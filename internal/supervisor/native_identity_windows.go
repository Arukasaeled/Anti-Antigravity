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
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type nativeHostIdentity struct {
	Email         string
	Available     bool
	Authenticated bool
	Source        string
}

type identityRPCConnection struct {
	Process HostProcess
	Port    int
	CSRF    string
}

var identityCSRFArgument = regexp.MustCompile(`(?:^|\s)--csrf_token(?:=|\s+)([^\s]+)`)

// This nonce stays inside the reader. Never expose command lines, response
// bodies, authentication tokens or the nonce in status, errors or logs.
func identityProcessCSRF(pid int) string {
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return ""
	}
	defer procCloseHandle.Call(handle)
	buffer := make([]byte, 16384)
	var size uint32
	query := syscall.NewLazyDLL("ntdll.dll").NewProc("NtQueryInformationProcess")
	status, _, _ := query.Call(handle, 60, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&size)))
	if status != 0 {
		return ""
	}
	type unicodeString struct {
		Length        uint16
		MaximumLength uint16
		Buffer        *uint16
	}
	value := (*unicodeString)(unsafe.Pointer(&buffer[0]))
	start, pointer := uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(value.Buffer))
	if value.Buffer == nil || value.Length == 0 || value.Length%2 != 0 || pointer < start || pointer+uintptr(value.Length) > start+uintptr(len(buffer)) {
		return ""
	}
	command := syscall.UTF16ToString(unsafe.Slice(value.Buffer, int(value.Length)/2))
	match := identityCSRFArgument.FindStringSubmatch(command)
	if len(match) != 2 {
		return ""
	}
	return strings.Trim(match[1], `"`)
}

// Discover only Language Servers descended from the exact authorized host.
// A second official instance can never supply identity for this process.
func identityRPCConnections(host HostProcess) []identityRPCConnection {
	if !sameHostProcess(host, processIdentity(host.PID)) || !CanManageHostPID(host.PID) {
		return nil
	}
	handle, _, _ := procCreateToolhelp32Snap.Call(th32csSnapProcess, 0)
	if handle == 0 || handle == uintptr(syscall.InvalidHandle) {
		return nil
	}
	defer procCloseHandle.Call(handle)
	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))
	parents, servers := map[int]int{}, map[int]bool{}
	more, _, _ := procProcess32FirstW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	for more != 0 {
		pid := int(entry.ProcessID)
		parents[pid] = int(entry.ParentProcessID)
		if strings.Contains(strings.ToLower(syscall.UTF16ToString(entry.ExeFile[:])), "language_server") {
			servers[pid] = true
		}
		more, _, _ = procProcess32NextW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	}
	children := map[int]HostProcess{}
	for pid := range servers {
		current := pid
		for depth := 0; depth < 64 && current > 0; depth++ {
			if current == host.PID {
				child := processIdentity(pid)
				if child.Started != "" && child.Started >= host.Started {
					children[pid] = child
				}
				break
			}
			next := parents[current]
			if next == current {
				break
			}
			current = next
		}
	}
	getTable := syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")
	var size uint32
	getTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, 2, 3, 0) // AF_INET, OWNER_PID_LISTENER
	if size < 4 || size > 32<<20 {
		return nil
	}
	buffer := make([]byte, size)
	status, _, _ := getTable.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0, 2, 3, 0)
	if status != 0 || size < 4 || int(size) > len(buffer) {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(buffer[:4]))
	if count > (int(size)-4)/24 {
		return nil
	}
	connections := []identityRPCConnection{}
	for i := 0; i < count; i++ {
		row := buffer[4+i*24 : 4+(i+1)*24]
		pid := int(binary.LittleEndian.Uint32(row[20:24]))
		child, exists := children[pid]
		address := net.IP(row[4:8])
		if !exists || (!address.IsLoopback() && !address.IsUnspecified()) || !sameHostProcess(child, processIdentity(pid)) {
			continue
		}
		port := int(binary.BigEndian.Uint16(row[8:10]))
		if port > 0 {
			connections = append(connections, identityRPCConnection{Process: child, Port: port, CSRF: identityProcessCSRF(pid)})
		}
	}
	return connections
}

func identityUnary(ctx context.Context, client *http.Client, connection identityRPCConnection, method string, out any) error {
	if !sameHostProcess(connection.Process, processIdentity(connection.Process.PID)) {
		return fmt.Errorf("native identity service changed")
	}
	payload := []byte(`{}`)
	body := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(body[1:5], uint32(len(payload)))
	copy(body[5:], payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("https://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/%s", connection.Port, method), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("native identity request unavailable")
	}
	request.Header.Set("Content-Type", "application/grpc-web+json")
	request.Header.Set("X-Grpc-Web", "1")
	if connection.CSRF != "" {
		request.Header.Set("X-Codeium-CSRF-Token", connection.CSRF)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("native identity service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("native identity HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || !sameHostProcess(connection.Process, processIdentity(connection.Process.PID)) {
		return fmt.Errorf("native identity response unavailable")
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '{' {
		if json.Unmarshal(trimmed, out) == nil {
			return nil
		}
		return fmt.Errorf("native identity response malformed")
	}
	var message []byte
	for len(raw) >= 5 {
		length := int(binary.BigEndian.Uint32(raw[1:5]))
		if length > len(raw)-5 {
			return fmt.Errorf("native identity response truncated")
		}
		if raw[0] == 0 {
			message = raw[5 : 5+length]
		} else if raw[0] == 128 {
			for _, line := range strings.Split(string(raw[5:5+length]), "\r\n") {
				if strings.HasPrefix(line, "grpc-status:") && strings.TrimSpace(strings.TrimPrefix(line, "grpc-status:")) != "0" {
					return fmt.Errorf("native identity RPC rejected")
				}
			}
		} else {
			return fmt.Errorf("native identity response unsupported")
		}
		raw = raw[5+length:]
	}
	if len(raw) != 0 || len(message) == 0 || json.Unmarshal(message, out) != nil {
		return fmt.Errorf("native identity response malformed")
	}
	return nil
}

func readNativeHostIdentity(ctx context.Context, host HostProcess) nativeHostIdentity {
	state := nativeHostIdentity{Source: "Antigravity LanguageServer/GetUserStatus + HasAuthToken"}
	// The self-signed TLS convention is restricted to discovered local child
	// processes. No proxy, redirect, remote URL or credential mutation is used.
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, connection := range identityRPCConnections(host) {
		if ctx.Err() != nil || !sameHostProcess(host, processIdentity(host.PID)) {
			break
		}
		var user struct {
			Status struct {
				Email string `json:"email"`
			} `json:"userStatus"`
		}
		var auth struct {
			HasToken bool `json:"hasToken"`
		}
		attemptCtx, cancel := context.WithTimeout(ctx, time.Second)
		userErr := identityUnary(attemptCtx, client, connection, "GetUserStatus", &user)
		var authErr error
		if userErr == nil {
			authErr = identityUnary(attemptCtx, client, connection, "HasAuthToken", &auth)
		}
		cancel()
		if userErr != nil || authErr != nil {
			continue
		}
		if !sameHostProcess(host, processIdentity(host.PID)) || !CanManageHostPID(host.PID) {
			return state
		}
		state.Available, state.Authenticated = true, auth.HasToken
		email := strings.TrimSpace(user.Status.Email)
		if len(email) <= 320 && strings.Contains(email, "@") && !strings.ContainsAny(email, "\r\n\t ") {
			state.Email = email
		}
		return state
	}
	return state
}
