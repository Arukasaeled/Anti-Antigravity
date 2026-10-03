//go:build windows

package supervisor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Official mode has no debug port. Read its child's local RPC service instead
// of enabling CDP or treating a credential write as authenticated UI state.
type nativeRPCConnection struct {
	Port int
	CSRF string
}

// The host's per-process CSRF nonce is kept in memory and never logged.
func nativeProcessCommandLine(pid int) string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	query := syscall.NewLazyDLL("ntdll.dll").NewProc("NtQueryInformationProcess")
	buffer := make([]byte, 16384)
	var length uint32
	status, _, _ := query.Call(h, 60, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&length)))
	if status != 0 {
		return ""
	}
	type unicodeString struct {
		Length        uint16
		MaximumLength uint16
		Buffer        *uint16
	}
	text := (*unicodeString)(unsafe.Pointer(&buffer[0]))
	if text.Buffer == nil || text.Length == 0 || int(text.Length) > len(buffer)-16 {
		return ""
	}
	start := uintptr(unsafe.Pointer(&buffer[0]))
	ptr := uintptr(unsafe.Pointer(text.Buffer))
	if ptr < start || ptr+uintptr(text.Length) > start+uintptr(len(buffer)) {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice(text.Buffer, int(text.Length)/2))
}

func nativeProcessCSRF(pid int) string {
	command := nativeProcessCommandLine(pid)
	match := regexp.MustCompile(`--csrf_token(?:=|\s+)([^\s]+)`).FindStringSubmatch(command)
	if len(match) > 1 {
		return strings.Trim(match[1], `"`)
	}
	return ""
}

func nativeLSConnections() []nativeRPCConnection {
	root := GetManagedHostPID()
	if root <= 0 {
		return nil
	}
	if !strings.HasSuffix(strings.ToLower(getProcessExePath(root)), `\antigravity.exe`) {
		return nil
	}
	h, _, _ := procCreateToolhelp32Snap.Call(th32csSnapProcess, 0)
	if h == 0 || h == uintptr(syscall.InvalidHandle) {
		return nil
	}
	defer procCloseHandle.Call(h)
	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, _ := procProcess32FirstW.Call(h, uintptr(unsafe.Pointer(&entry)))
	pids := map[int]string{}
	for ok != 0 {
		if int(entry.ParentProcessID) == root && strings.Contains(strings.ToLower(syscall.UTF16ToString(entry.ExeFile[:])), "language_server") {
			pids[int(entry.ProcessID)] = nativeProcessCSRF(int(entry.ProcessID))
		}
		ok, _, _ = procProcess32NextW.Call(h, uintptr(unsafe.Pointer(&entry)))
	}
	if len(pids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netstat", "-ano", "-p", "tcp")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var ports []nativeRPCConnection
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[3] != "LISTENING" {
			continue
		}
		pid, _ := strconv.Atoi(fields[4])
		csrf, exists := pids[pid]
		if !exists {
			continue
		}
		address, portString, e := net.SplitHostPort(fields[1])
		if e != nil || (address != "127.0.0.1" && address != "::1" && address != "0.0.0.0") {
			continue
		}
		port, _ := strconv.Atoi(portString)
		if port > 0 {
			ports = append(ports, nativeRPCConnection{Port: port, CSRF: csrf})
		}
	}
	return ports
}

func nativeUnary(connection nativeRPCConnection, method string, out any) error {
	// Only a discovered local child is contacted. TLS self-signing is the
	// host's loopback convention; this transport cannot be used for remote URLs.
	payload := []byte(`{}`)
	body := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(body[1:5], uint32(len(payload)))
	copy(body[5:], payload)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("https://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/%s", connection.Port, method), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/grpc-web+json")
	req.Header.Set("X-Grpc-Web", "1")
	if connection.CSRF != "" {
		req.Header.Set("X-Codeium-CSRF-Token", connection.CSRF)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("local RPC HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(raw) > 0 && raw[0] == '{' {
		return json.Unmarshal(raw, out)
	}
	for len(raw) >= 5 {
		length := int(binary.BigEndian.Uint32(raw[1:5]))
		if length > len(raw)-5 {
			return fmt.Errorf("truncated native RPC")
		}
		if raw[0] == 0 {
			return json.Unmarshal(raw[5:5+length], out)
		}
		if raw[0] == 128 {
			return fmt.Errorf("native RPC returned a trailer without a message")
		}
		raw = raw[5+length:]
	}
	return fmt.Errorf("no native RPC message")
}

func readNativeAuthRPC() NativeAuthState {
	state := NativeAuthState{Source: "Antigravity LanguageServer/GetAuthStatus"}
	for _, connection := range nativeLSConnections() {
		var result struct {
			AuthResult struct {
				Valid        bool            `json:"hasValidAuth"`
				Message      string          `json:"uiMessage"`
				Ineligible   json.RawMessage `json:"ineligible"`
				GeneralError json.RawMessage `json:"generalError"`
				Verification json.RawMessage `json:"verificationRequired"`
				TOS          json.RawMessage `json:"tosViolation"`
			} `json:"authResult"`
		}
		if nativeUnary(connection, "GetAuthStatus", &result) != nil {
			continue
		}
		state.Available = true
		state.Valid = result.AuthResult.Valid
		state.Message = result.AuthResult.Message
		for _, detail := range []struct {
			Name  string
			Value json.RawMessage
		}{{"ineligible", result.AuthResult.Ineligible}, {"generalError", result.AuthResult.GeneralError}, {"verificationRequired", result.AuthResult.Verification}, {"tosViolation", result.AuthResult.TOS}} {
			if len(detail.Value) > 0 && string(detail.Value) != "null" {
				state.Failure = detail.Name
				break
			}
		}
		if !state.Valid && state.Failure == "" && state.Message != "" {
			state.Failure = "native-auth"
		}
		{
			var user struct {
				Status struct {
					Email string `json:"email"`
				} `json:"userStatus"`
			}
			if nativeUnary(connection, "GetUserStatus", &user) == nil {
				state.Email = user.Status.Email
			}
			var token struct {
				HasToken bool `json:"hasToken"`
			}
			if nativeUnary(connection, "HasAuthToken", &token) == nil {
				state.Authenticated = token.HasToken && state.Email != ""
			}
		}
		return state
	}
	return state
}
