package supervisor

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
	"sort"
	"strings"
	"time"

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/patcher"
)

type cdpTarget struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// IsRealWorkbenchTarget 判断 Target 是否为真实工作台页面，严格只抓取真实的渲染视窗
func IsRealWorkbenchTarget(t cdpTarget) bool {
	// 1. 严格只抓取 page 类型的渲染目标
	if t.Type != "page" || t.WebSocketDebuggerURL == "" {
		return false
	}

	// 2. 严格拦截无效协议与开发工具
	if strings.HasPrefix(t.URL, "devtools://") ||
		strings.HasPrefix(t.URL, "chrome://") ||
		t.URL == "about:blank" {
		return false
	}

	// 3. 拦截 1 秒即销毁的临时启动遮罩
	if strings.HasPrefix(t.URL, "data:text/html") ||
		strings.Contains(t.URL, "Loading%20Antigravity") ||
		strings.Contains(strings.ToLower(t.Title), "loading antigravity") {
		return false
	}

	// 4. 黄金特征判定：
	// Antigravity 的真实主工作台 URL 必为本地高位端口 https://127.0.0.1:xxxxx/ 或 vscode-file://
	// 此时绝对不能检查 Title 是否为空！Title 为空也必须判定为真实工作台！
	if strings.HasPrefix(t.URL, "https://127.0.0.1:") ||
		strings.HasPrefix(t.URL, "http://127.0.0.1:") ||
		strings.Contains(t.URL, "workbench") ||
		strings.HasPrefix(t.URL, "vscode-file://") {
		return true
	}

	// 5. 兜底：如果 URL 或 Title 显式包含 antigravity 关键字
	lowerURL := strings.ToLower(t.URL)
	lowerTitle := strings.ToLower(t.Title)
	if strings.Contains(lowerURL, "antigravity") || strings.Contains(lowerTitle, "antigravity") {
		return true
	}

	return false
}

func targetScore(t cdpTarget) int {
	score := 50
	lowerTitle := strings.ToLower(t.Title)
	lowerURL := strings.ToLower(t.URL)
	if strings.Contains(lowerTitle, "anti-antigravity") {
		score = 120
	} else if strings.Contains(lowerTitle, "antigravity") {
		score = 110
	} else if strings.Contains(lowerURL, "workbench") || strings.Contains(lowerURL, "workbench.html") {
		score = 100
	} else if strings.Contains(lowerURL, "/c/") || strings.Contains(lowerURL, "section=") {
		score = 95
	} else if strings.HasPrefix(lowerURL, "https://127.0.0.1:") || strings.HasPrefix(lowerURL, "http://127.0.0.1:") {
		score = 90
	} else if strings.HasPrefix(lowerURL, "vscode-file://") {
		score = 80
	}
	return score
}

// tryEnableBrowserAutoAttach 尝试连接 Browser 目标并激活 Target.setAutoAttach
func tryEnableBrowserAutoAttach(ctx context.Context, targetAddr string) {
	client := &http.Client{Timeout: 1 * time.Second}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+targetAddr+"/json/version", nil)
			if err != nil {
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK {
				continue
			}

			var verInfo struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			if err := json.Unmarshal(data, &verInfo); err != nil || verInfo.WebSocketDebuggerURL == "" {
				continue
			}

			conn, reader, err := openCDPWebSocket(ctx, verInfo.WebSocketDebuggerURL)
			if err != nil {
				continue
			}
			defer conn.Close()

			autoAttachReq := map[string]any{
				"id":     1001,
				"method": "Target.setAutoAttach",
				"params": map[string]any{
					"autoAttach":             true,
					"waitForDebuggerOnStart": false,
					"flatten":                true,
				},
			}
			if err := sendCDPCommand(conn, reader, 1001, autoAttachReq); err == nil {
				log.Printf("[2ag] 已成功向 Browser 目标发送 Target.setAutoAttach")
			}
			return
		}
	}
}

// ResolveWallpaperDataURL 将本地图片文件解析并编码为 data:image/...;base64,... 格式
// 彻底解决 Chromium/Electron 对 https:// 页面加载 file:/// 协议的同源安全拦截
func ResolveWallpaperDataURL(wallpaperPath string) string {
	if strings.TrimSpace(wallpaperPath) == "" {
		wallpaperPath = config.DefaultWallpaperPath
	}
	cleanPath := strings.TrimPrefix(wallpaperPath, "file:///")
	cleanPath = filepath.Clean(cleanPath)

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if cleanPath != filepath.Clean(config.DefaultWallpaperPath) {
			data, err = os.ReadFile(config.DefaultWallpaperPath)
		}
	}
	if err == nil && len(data) > 0 {
		ext := strings.ToLower(filepath.Ext(cleanPath))
		mimeType := "image/jpeg"
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".webp":
			mimeType = "image/webp"
		case ".gif":
			mimeType = "image/gif"
		}
		return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	}
	return wallpaperPath
}

// WatchAndInjectCDP 监听 CDP 端口并在目标页面就绪时物理注入 anti-Antigravity 补丁
func WatchAndInjectCDP(targetAddr string, timeout time.Duration) error {
	if targetAddr == "" {
		targetAddr = ResolveCDPAddr()
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Printf("[2ag] 正在监听 CDP 端口 %s 准备物理注入 anti-Antigravity (超时: %v)...", targetAddr, timeout)

	cfgPath, err := config.DefaultPath()
	var cfg config.Config
	if err == nil {
		cfg, _ = config.Load(cfgPath)
	}
	wpPath := cfg.WallpaperPath
	if wpPath == "" {
		wpPath = config.DefaultWallpaperPath
	}
	wpDataURL := ResolveWallpaperDataURL(wpPath)

	blur := cfg.Blur
	if blur <= 0 {
		blur = 28
	}
	opacity := cfg.Opacity
	if opacity <= 0 {
		opacity = 0.6
	}

	hubConfig := patcher.HubConfig{
		Language:      "zh-CN",
		Preset:        "Dark Dream",
		Blur:          blur,
		Opacity:       opacity,
		Wallpaper:     wpDataURL,
		WallpaperPath: wpPath,
		GravityBoost:  map[string]any{"centered_width": true, "enable_devtools": true, "paste_plaintext_fix": true},
	}
	expression, err := patcher.BuildHubExpression(hubConfig)
	if err != nil {
		return fmt.Errorf("构建注入脚本失败: %w", err)
	}

	// 异步向 Browser 目标发送 Target.setAutoAttach，确保主窗口打开及后续窗口能被监听
	go tryEnableBrowserAutoAttach(ctx, targetAddr)

	client := &http.Client{Timeout: 1 * time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	injected := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[2ag] CDP 自动注入监听超时 (%s)", targetAddr)
			return ctx.Err()
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+targetAddr+"/json", nil)
			if err != nil {
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK {
				continue
			}

			var targets []cdpTarget
			if err := json.Unmarshal(data, &targets); err != nil {
				continue
			}

			var validTargets []cdpTarget
			for _, t := range targets {
				if IsRealWorkbenchTarget(t) {
					validTargets = append(validTargets, t)
				}
			}

			if len(validTargets) == 0 {
				continue
			}

			// 优先匹配 Title 包含 Anti-antigravity、Antigravity 的主渲染视窗
			sort.Slice(validTargets, func(i, j int) bool {
				return targetScore(validTargets[i]) > targetScore(validTargets[j])
			})

			injectedCount := 0
			for _, t := range validTargets {
				key := t.ID
				if key == "" {
					key = t.WebSocketDebuggerURL
				}
				if injected[key] {
					continue
				}

				log.Printf("[2ag] 捕获到 Antigravity 真实工作台页面 (Title: %q, URL: %s, Score: %d)，执行全量广播注入...", t.Title, t.URL, targetScore(t))

				if err := performCDPInject(ctx, t.WebSocketDebuggerURL, expression); err != nil {
					log.Printf("[2ag] 针对页面 %q 的 CDP 注入尝试暂时失败: %v; 持续重试...", t.Title, err)
					continue
				}

				injected[key] = true
				injectedCount++
			}

			if injectedCount > 0 {
				log.Printf("[2ag] 宿主物理变身 anti-Antigravity 成功！(已对全部 %d 个真实可视视窗完成全量广播注入，880px 黄金居中与 Dream-Skin 壁纸引擎已生效)", injectedCount)
				go maintainCDPInjection(targetAddr, expression)
				return nil
			}
		}
	}
}

func watchAndInjectCDP(targetAddr string, timeout time.Duration) {
	_ = WatchAndInjectCDP(targetAddr, timeout)
}

// maintainCDPInjection 启动常驻保活巡检协程：当宿主发生 SPA 路由跳转或页面重绘导致补丁丢失时，自动重新激活
func maintainCDPInjection(targetAddr string, expression string) {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	probeExpr := `Boolean(document.getElementById('2ag-dream-skin-bg') && document.getElementById('2ag-hud-root'))`

	for range ticker.C {
		req, err := http.NewRequest(http.MethodGet, "http://"+targetAddr+"/json", nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}

		var targets []cdpTarget
		if err := json.Unmarshal(data, &targets); err != nil {
			continue
		}

		for _, t := range targets {
			if !IsRealWorkbenchTarget(t) {
				continue
			}

			conn, reader, err := openCDPWebSocket(context.Background(), t.WebSocketDebuggerURL)
			if err != nil {
				continue
			}

			needsInject := true
			evalReq := map[string]any{
				"id":     9999,
				"method": "Runtime.evaluate",
				"params": map[string]any{
					"expression":    probeExpr,
					"returnByValue": true,
				},
			}
			if err := sendCDPCommand(conn, reader, 9999, evalReq); err == nil {
				line, rerr := reader.ReadBytes('\n')
				if rerr == nil {
					var res struct {
						Result struct {
							Result struct {
								Value bool `json:"value"`
							} `json:"result"`
						} `json:"result"`
					}
					if json.Unmarshal(line, &res) == nil && res.Result.Result.Value {
						needsInject = false
					}
				}
			}
			_ = conn.Close()

			if needsInject {
				log.Printf("[2ag] 检测到工作台视窗 (Title: %q, URL: %s) 需重新激活 2Ag 补丁，执行热注入...", t.Title, t.URL)
				_ = performCDPInject(context.Background(), t.WebSocketDebuggerURL, expression)
			}
		}
	}
}

func performCDPInject(ctx context.Context, wsURL string, expression string) error {
	conn, reader, err := openCDPWebSocket(ctx, wsURL)
	if err != nil {
		return fmt.Errorf("建立 WebSocket 连接失败: %w", err)
	}
	defer conn.Close()

	// 1. 发送 Page.enable
	pageEnableReq := map[string]any{
		"id":     1,
		"method": "Page.enable",
	}
	if err := sendCDPCommand(conn, reader, 1, pageEnableReq); err != nil {
		log.Printf("[2ag] Page.enable: %v", err)
	}

	// 2. 发送 Page.addScriptToEvaluateOnNewDocument 确保 SPA 页面刷新后持续生效
	addScriptReq := map[string]any{
		"id":     2,
		"method": "Page.addScriptToEvaluateOnNewDocument",
		"params": map[string]any{
			"source": expression,
		},
	}
	if err := sendCDPCommand(conn, reader, 2, addScriptReq); err != nil {
		log.Printf("[2ag] Page.addScriptToEvaluateOnNewDocument: %v", err)
	}

	// 3. 立即执行 Runtime.evaluate 让当前已有 DOM 窗口直接完成 880px 居中排版与 HUD 呈现
	evalReq := map[string]any{
		"id":     3,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expression,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	}
	if err := sendCDPCommand(conn, reader, 3, evalReq); err != nil {
		return fmt.Errorf("Runtime.evaluate 失败: %w", err)
	}

	// 4. 自动通过 CDP Runtime.evaluate 探针读取宿主内存中的真实登录邮箱
	if realEmail, err := evaluateCDPString(conn, reader, 10, HostAccountProbeExpr); err == nil && realEmail != "" && strings.Contains(realEmail, "@") {
		log.Printf("[2ag] 通过 CDP Runtime.evaluate 探针成功识别宿主真实登录账号: %s", realEmail)
		SetActiveAccount(realEmail)
	}

	// 5. 短暂延迟后再 evaluate 两次，稳固抗击 SPA 挂载重刷与状态水合
	go func() {
		for i := 4; i <= 6; i++ {
			time.Sleep(800 * time.Millisecond)
			retryConn, retryReader, err := openCDPWebSocket(context.Background(), wsURL)
			if err != nil {
				return
			}
			retryEval := map[string]any{
				"id":     i,
				"method": "Runtime.evaluate",
				"params": map[string]any{
					"expression":    expression,
					"awaitPromise":  true,
					"returnByValue": true,
				},
			}
			_ = sendCDPCommand(retryConn, retryReader, i, retryEval)
			if realEmail, err := evaluateCDPString(retryConn, retryReader, i+10, HostAccountProbeExpr); err == nil && realEmail != "" && strings.Contains(realEmail, "@") {
				SetActiveAccount(realEmail)
			}
			_ = retryConn.Close()
		}
	}()

	return nil
}

func openCDPWebSocket(ctx context.Context, rawURL string) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("解析 CDP URL 失败: %w", err)
	}
	if u.Host == "" || u.Path == "" {
		return nil, nil, errors.New("CDP URL 不完整")
	}

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 CDP 端口失败: %w", err)
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("生成 WebSocket Key 失败: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", u.RequestURI(), u.Host, key)
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("发送 WebSocket 握手请求失败: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("读取 WebSocket 握手响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, fmt.Errorf("WebSocket 握手状态码异常: %s", resp.Status)
	}
	return conn, reader, nil
}

func sendCDPCommand(conn net.Conn, reader *bufio.Reader, id int, command map[string]any) error {
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	if err := writeCDPFrame(conn, 1, payload); err != nil {
		return err
	}
	for {
		frame, opcode, err := readCDPFrame(reader)
		if err != nil {
			return err
		}
		if opcode == 9 { // Ping
			_ = writeCDPFrame(conn, 10, frame) // Pong
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
			return fmt.Errorf("CDP 错误: %s", response.Error.Message)
		}
		return nil
	}
}

func writeCDPFrame(conn net.Conn, opcode byte, payload []byte) error {
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
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := conn.Write(header.Bytes()); err != nil {
		return err
	}
	_, err := conn.Write(masked)
	return err
}

func readCDPFrame(reader *bufio.Reader) ([]byte, byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}
	if first&0x0f == 8 {
		return nil, 0, errors.New("WebSocket 连接已关闭")
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
		return nil, 0, errors.New("CDP Frame 过大")
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
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, first & 0x0f, nil
}

// HostAccountProbeExpr 探测宿主内存与 DOM 中的真实登录邮箱
const HostAccountProbeExpr = `(() => {
	try {
		for (let i = 0; i < localStorage.length; i++) {
			const k = localStorage.key(i);
			const v = localStorage.getItem(k);
			if (v && v.includes('@')) {
				const m = v.match(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/);
				if (m) return m[0];
			}
		}
	} catch(e) {}
	try {
		for (let i = 0; i < sessionStorage.length; i++) {
			const k = sessionStorage.key(i);
			const v = sessionStorage.getItem(k);
			if (v && v.includes('@')) {
				const m = v.match(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/);
				if (m) return m[0];
			}
		}
	} catch(e) {}
	try {
		const nodes = document.querySelectorAll('[aria-label*="@"], [title*="@"], [data-email], [class*="account"], [class*="profile"], [class*="user"]');
		for (const n of nodes) {
			const text = (n.getAttribute('aria-label') || '') + ' ' + (n.getAttribute('title') || '') + ' ' + (n.getAttribute('data-email') || '') + ' ' + (n.innerText || '');
			const m = text.match(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/);
			if (m) return m[0];
		}
	} catch(e) {}
	return "";
})()`

// QueryHostEmailViaCDP 自动通过 CDP Runtime.evaluate 探针读取宿主内存中的真实登录邮箱
func QueryHostEmailViaCDP(timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://" + ResolveCDPAddr() + "/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var targets []cdpTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return "", err
	}
	var wsURL string
	bestScore := -1
	for _, t := range targets {
		if IsRealWorkbenchTarget(t) {
			score := targetScore(t)
			if score > bestScore {
				bestScore = score
				wsURL = t.WebSocketDebuggerURL
			}
		}
	}
	if wsURL == "" {
		for _, t := range targets {
			if t.Type == "page" && t.WebSocketDebuggerURL != "" && !strings.HasPrefix(strings.ToLower(t.URL), "devtools://") {
				wsURL = t.WebSocketDebuggerURL
				break
			}
		}
	}
	if wsURL == "" {
		return "", errors.New("未找到宿主页面调试端点")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, reader, err := openCDPWebSocket(ctx, wsURL)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	val, err := evaluateCDPString(conn, reader, 99, HostAccountProbeExpr)
	if err != nil {
		return "", err
	}
	val = strings.TrimSpace(val)
	if strings.Contains(val, "@") {
		return val, nil
	}
	return "", errors.New("宿主内存中未探测到有效登录邮箱")
}

func evaluateCDPString(conn net.Conn, reader *bufio.Reader, id int, expr string) (string, error) {
	command := map[string]any{
		"id":     id,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expr,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	if err := writeCDPFrame(conn, 1, payload); err != nil {
		return "", err
	}
	for {
		frame, opcode, err := readCDPFrame(reader)
		if err != nil {
			return "", err
		}
		if opcode == 9 { // Ping
			_ = writeCDPFrame(conn, 10, frame) // Pong
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
				Result struct {
					Type  string `json:"type"`
					Value any    `json:"value"`
				} `json:"result"`
			} `json:"result"`
		}
		if err := json.Unmarshal(frame, &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return "", fmt.Errorf("CDP 错误: %s", response.Error.Message)
		}
		if str, ok := response.Result.Result.Value.(string); ok {
			return str, nil
		}
		return "", nil
	}
}

