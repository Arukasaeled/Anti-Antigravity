package patcher

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/2ag/2ag/assets"
)

const (
	DefaultWallpaperPath   = `C:/Users/user/Pictures/E78978FCDE46412C17F90AAF7813EC8D.jpg`
	WallpaperServerAddress = "127.0.0.1:18082"
	WallpaperServerURL     = "http://127.0.0.1:18082/bg.jpg"
	CDPInjectionAttempts   = 5
	CDPInjectionInterval   = 800 * time.Millisecond
)

type CDPInjector struct {
	Address        string
	HTTPClient     *http.Client
	ProxyURL       string
	Initial        HubConfig
	Lifetime       context.Context
	BridgeHandlers map[string]BridgeHandler

	wallpaperMu     sync.Mutex
	wallpaperPath   string
	wallpaperURL    string
	wallpaperServer *http.Server
	bridgeMu        sync.Mutex
	bridge          *CDPBridge
	bridgeTarget    string
}

type cdpTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func NewCDPInjector(port int) *CDPInjector {
	if port <= 0 {
		port = 9222
	}
	return &CDPInjector{
		Address:    "127.0.0.1:" + strconv.Itoa(port),
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	}
}

func normalizeWallpaperPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultWallpaperPath
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve wallpaper %q: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat wallpaper %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("wallpaper path %q is a directory", path)
	}
	return path, nil
}

func (i *CDPInjector) ensureWallpaperServer(ctx context.Context, wallpaperPath string) error {
	path, err := normalizeWallpaperPath(wallpaperPath)
	if err != nil {
		return err
	}

	i.wallpaperMu.Lock()
	defer i.wallpaperMu.Unlock()
	if i.wallpaperServer != nil {
		if i.wallpaperPath != path {
			return fmt.Errorf("wallpaper server already serves %q", i.wallpaperPath)
		}
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/logo.png", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if r.Method == http.MethodGet {
			_, _ = w.Write(assets.LogoPNG)
		}
	})
	mux.HandleFunc("/bg.jpg", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		i.wallpaperMu.Lock()
		currentPath := i.wallpaperPath
		i.wallpaperMu.Unlock()
		http.ServeFile(w, r, currentPath)
	})
	server := &http.Server{
		Addr:    WallpaperServerAddress,
		Handler: mux,
	}
	listener, err := net.Listen("tcp", WallpaperServerAddress)
	if err != nil {
		// A second launcher instance may already own the preferred port. Fall
		// back to an ephemeral loopback port so injection remains available.
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("start wallpaper server on %s or an ephemeral port: %w", WallpaperServerAddress, err)
		}
	}
	i.wallpaperPath = path
	i.wallpaperURL = "http://" + listener.Addr().String() + "/bg.jpg"
	i.wallpaperServer = server
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[2ag] wallpaper server stopped: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		i.wallpaperMu.Lock()
		if i.wallpaperServer == server {
			i.wallpaperServer = nil
			i.wallpaperPath = ""
			i.wallpaperURL = ""
		}
		i.wallpaperMu.Unlock()
	}()
	return nil
}

// SetWallpaperPath changes the file served by the loopback image endpoint.
// It is intentionally limited to an existing regular file so the renderer
// never receives an arbitrary filesystem read through the HTTP route.
func (i *CDPInjector) SetWallpaperPath(path string) error {
	resolved, err := normalizeWallpaperPath(path)
	if err != nil {
		return err
	}
	i.wallpaperMu.Lock()
	defer i.wallpaperMu.Unlock()
	if i.wallpaperServer == nil {
		return errors.New("wallpaper server is not running")
	}
	i.wallpaperPath = resolved
	return nil
}

// Inject waits for CDP, opens a page target, and reapplies the Dream Skin
// closure five times at 800 ms intervals while the SPA finishes rendering.
func (i *CDPInjector) Inject(ctx context.Context, wallpaperPath string, blur int, opacity float64, modalOpacity float64) error {
	if i == nil {
		return errors.New("CDP injector is nil")
	}
	if blur < 0 || blur > 100 {
		return errors.New("blur must be between 0 and 100")
	}
	if opacity < 0 || opacity > 1 {
		return errors.New("opacity must be between 0 and 1")
	}
	if err := i.ensureWallpaperServer(ctx, wallpaperPath); err != nil {
		return err
	}
	target, err := i.findTarget(ctx)
	if err != nil {
		return err
	}
	conn, reader, err := openWebSocket(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	if len(i.BridgeHandlers) > 0 {
		i.ensureBridge(ctx, target.WebSocketDebuggerURL)
	}
	i.wallpaperMu.Lock()
	wallpaperURL := i.wallpaperURL
	i.wallpaperMu.Unlock()
	expression := buildInjectionExpression(wallpaperURL, blur, opacity, modalOpacity, i.ProxyURL, i.Initial)
	var lastErr error
	if err := installOnNewDocument(conn, reader, 1, expression); err != nil {
		lastErr = fmt.Errorf("register CDP document hook: %w", err)
		log.Printf("[2ag] %v; continuing with Runtime.evaluate", lastErr)
	}
	injected := false
	for attempt := 1; attempt <= CDPInjectionAttempts; attempt++ {
		if err := waitContext(ctx, CDPInjectionInterval); err != nil {
			return err
		}
		if err := evaluate(conn, reader, attempt+1, expression); err != nil {
			lastErr = fmt.Errorf("CDP injection attempt %d/%d: %w", attempt, CDPInjectionAttempts, err)
			continue
		}
		injected = true
	}
	if injected {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return nil
}

func (i *CDPInjector) ensureBridge(ctx context.Context, websocketURL string) {
	i.bridgeMu.Lock()
	defer i.bridgeMu.Unlock()
	if i.bridge != nil && i.bridgeTarget == websocketURL {
		return
	}
	if i.bridge != nil {
		_ = i.bridge.Close()
		i.bridge = nil
		i.bridgeTarget = ""
	}
	bridgeContext := i.Lifetime
	if bridgeContext == nil {
		bridgeContext = ctx
	}
	bridge, err := StartCDPBridge(bridgeContext, websocketURL, i.BridgeHandlers)
	if err != nil {
		log.Printf("[2ag] CDP IPC bridge unavailable: %v", err)
		return
	}
	i.bridge = bridge
	i.bridgeTarget = websocketURL
}

func (i *CDPInjector) WaitAndInject(ctx context.Context, wallpaperPath string, blur int, opacity float64, modalOpacity float64, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	// Keep the wallpaper server tied to the launcher, not the CDP deadline.
	if err := i.ensureWallpaperServer(ctx, wallpaperPath); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for {
		if err := i.Inject(waitCtx, wallpaperPath, blur, opacity, modalOpacity); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && lastErr != nil {
				return fmt.Errorf("wait for CDP injection: %w", lastErr)
			}
			return waitCtx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (i *CDPInjector) findTarget(ctx context.Context) (cdpTarget, error) {
	client := i.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+i.Address+"/json/list", nil)
	if err != nil {
		return cdpTarget{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return cdpTarget{}, fmt.Errorf("query CDP targets: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return cdpTarget{}, fmt.Errorf("query CDP targets: HTTP %s", response.Status)
	}
	var targets []cdpTarget
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return cdpTarget{}, fmt.Errorf("decode CDP targets: %w", err)
	}
	for _, target := range targets {
		if target.WebSocketDebuggerURL != "" && target.Type == "page" && !strings.HasPrefix(strings.ToLower(target.URL), "devtools://") {
			return target, nil
		}
	}
	return cdpTarget{}, errors.New("Antigravity loopback application page is not ready")
}

func buildInjectionExpression(wallpaperURL string, blur int, opacity float64, modalOpacity float64, proxyURL string, initial HubConfig) string {
	initial.Wallpaper = wallpaperURL
	if strings.HasSuffix(wallpaperURL, "/bg.jpg") {
		initial.LogoURL = strings.TrimSuffix(wallpaperURL, "/bg.jpg") + "/logo.png"
	}
	initial.Blur = blur
	initial.Opacity = opacity
	initial.ModalOpacity = modalOpacity
	initial.ProxyURL = proxyURL
	if initial.Preset == "" {
		initial.Preset = "Dark Dream"
	}
	if initial.Plugins == nil {
		initial.Plugins = make(map[string]any)
	}
	expression, err := BuildHubExpression(initial)
	if err != nil {
		return `(() => { throw new Error("2Ag hub configuration could not be encoded"); })()`
	}
	return expression
}

func installOnNewDocument(conn net.Conn, reader *bufio.Reader, id int, expression string) error {
	request := map[string]any{
		"id":     id,
		"method": "Page.addScriptToEvaluateOnNewDocument",
		"params": map[string]any{"source": expression},
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode document hook: %w", err)
	}
	if err := writeWebSocketFrame(conn, payload); err != nil {
		return fmt.Errorf("send document hook: %w", err)
	}
	for {
		frame, opcode, err := readWebSocketFrame(reader)
		if err != nil {
			return fmt.Errorf("read document hook response: %w", err)
		}
		if opcode == 9 {
			if err := writeWebSocketControlFrame(conn, 10, frame); err != nil {
				return fmt.Errorf("reply to CDP ping: %w", err)
			}
			continue
		}
		if opcode != 1 {
			continue
		}
		var response struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(frame, &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("Page.addScriptToEvaluateOnNewDocument: %s", response.Error.Message)
		}
		return nil
	}
}

func evaluate(conn net.Conn, reader *bufio.Reader, id int, expression string) error {
	request := map[string]any{
		"id":     id,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expression,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode CDP request: %w", err)
	}
	if err := writeWebSocketFrame(conn, payload); err != nil {
		return fmt.Errorf("send CDP request: %w", err)
	}
	for {
		frame, opcode, err := readWebSocketFrame(reader)
		if err != nil {
			return fmt.Errorf("read CDP response: %w", err)
		}
		if opcode == 9 {
			if err := writeWebSocketControlFrame(conn, 10, frame); err != nil {
				return fmt.Errorf("reply to CDP ping: %w", err)
			}
			continue
		}
		if opcode != 1 {
			continue
		}
		var response struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				ExceptionDetails json.RawMessage `json:"exceptionDetails"`
			} `json:"result"`
		}
		if err := json.Unmarshal(frame, &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("Runtime.evaluate: %s", response.Error.Message)
		}
		if len(response.Result.ExceptionDetails) != 0 && string(response.Result.ExceptionDetails) != "null" {
			var details struct {
				Text      string `json:"text"`
				Exception *struct {
					Description string `json:"description"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(response.Result.ExceptionDetails, &details); err != nil {
				log.Printf("[2ag] Runtime.evaluate exceptionDetails: %s", string(response.Result.ExceptionDetails))
				return fmt.Errorf("Runtime.evaluate raised an exception (decode exceptionDetails: %w)", err)
			}
			description := ""
			if details.Exception != nil {
				description = details.Exception.Description
			}
			log.Printf("[2ag] Runtime.evaluate exception: text=%q exception.description=%q", details.Text, description)
			if details.Text == "" && description == "" {
				return fmt.Errorf("Runtime.evaluate raised an exception: %s", string(response.Result.ExceptionDetails))
			}
			return fmt.Errorf("Runtime.evaluate raised an exception: text=%s; exception.description=%s", details.Text, description)
		}
		return nil
	}
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func openWebSocket(ctx context.Context, rawURL string) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CDP websocket URL: %w", err)
	}
	if u.Host == "" || u.Path == "" {
		return nil, nil, errors.New("CDP websocket URL is incomplete")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, nil, fmt.Errorf("connect CDP websocket: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("create websocket key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", u.RequestURI(), u.Host, key)
	if _, err := io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("handshake CDP websocket: %w", err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read CDP websocket handshake: %w", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, fmt.Errorf("CDP websocket handshake returned %s", response.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, reader, nil
}

func writeWebSocketFrame(conn net.Conn, payload []byte) error {
	return writeWebSocketControlFrame(conn, 1, payload)
}

func writeWebSocketControlFrame(conn net.Conn, opcode byte, payload []byte) error {
	var header bytes.Buffer
	header.WriteByte(0x80 | opcode)
	length := len(payload)
	if length < 126 {
		header.WriteByte(byte(length) | 0x80)
	} else if length <= 65535 {
		header.WriteByte(126 | 0x80)
		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(length))
		header.Write(size[:])
	} else {
		header.WriteByte(127 | 0x80)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(length))
		header.Write(size[:])
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header.Write(mask)
	masked := make([]byte, len(payload))
	for index := range payload {
		masked[index] = payload[index] ^ mask[index%4]
	}
	if _, err := conn.Write(header.Bytes()); err != nil {
		return err
	}
	_, err := conn.Write(masked)
	return err
}

func readWebSocketFrame(reader *bufio.Reader) ([]byte, byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}
	if first&0x0f == 8 {
		return nil, 0, errors.New("CDP websocket closed")
	}
	length := uint64(second & 0x7f)
	if length == 126 {
		var size [2]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return nil, 0, err
		}
		length = uint64(binary.BigEndian.Uint16(size[:]))
	} else if length == 127 {
		var size [8]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return nil, 0, err
		}
		length = binary.BigEndian.Uint64(size[:])
	}
	if length > 16<<20 {
		return nil, 0, errors.New("CDP websocket frame is too large")
	}
	masked := second&0x80 != 0
	mask := make([]byte, 4)
	if masked {
		if _, err := io.ReadFull(reader, mask); err != nil {
			return nil, 0, err
		}
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, 0, err
	}
	if masked {
		for index := range payload {
			payload[index] ^= mask[index%4]
		}
	}
	return payload, first & 0x0f, nil
}
