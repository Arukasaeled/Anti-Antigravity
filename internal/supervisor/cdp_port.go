package supervisor

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultCDPPort 是 2Ag 为 Antigravity 宿主预留的默认 CDP 调试端口，
// 仅在 DevToolsActivePort 动态解析全部失败时作为兜底。
const DefaultCDPPort = 28472

// devToolsActivePortFile 是 Chromium/Electron 在 user-data-dir 下写出的调试端点探针文件：
// 第一行为实际监听的端口，第二行为 /devtools/browser/<id> 路径。
const devToolsActivePortFile = "DevToolsActivePort"

// cdpProbeTimeout 是候选端口存活探测的超时；本机回环，100ms 足够区分"在听"与"没人听"。
const cdpProbeTimeout = 100 * time.Millisecond

// ReadCDPPortFromFile 读取 DevToolsActivePort 文件的第一行并解析为端口号。
// 文件不存在、不可读或第一行不是合法端口时一律返回 0（调用方据此继续下一个候选）。
func ReadCDPPortFromFile(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	firstLine := strings.TrimSpace(strings.SplitN(normalized, "\n", 2)[0])
	port, err := strconv.Atoi(firstLine)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

// cdpPortCandidates 按优先级列出 DevToolsActivePort 的候选路径：
//  1. %APPDATA%\Antigravity\DevToolsActivePort  —— 用户手动启动的宿主
//  2. %APPDATA%\antigravity\DevToolsActivePort  —— 大小写变体目录
//  3. %USERPROFILE%\.2ag\profiles\*\DevToolsActivePort —— 2Ag 各账号沙箱拉起的宿主
func cdpPortCandidates() []string {
	var out []string
	environment := DetectAntigravityEnvironment()
	for _, root := range append([]string{environment.UserDataRoot}, environment.ProfileRoots...) {
		if root != "" {
			out = append(out, filepath.Join(root, devToolsActivePortFile))
		}
	}
	return out
}

// probeCDPPortAlive 对 127.0.0.1:port 做一次极短 TCP 探测，
// 用于剔除已退出实例遗留在 DevToolsActivePort 里的陈旧端口。
func probeCDPPortAlive(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), cdpProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ResolveCDPPort 动态解析宿主真实的 CDP 调试端口。
// 优先读取 DevToolsActivePort 文件第一行并对候选端口做存活探测
// （避免读到已退出实例留下的陈旧端口）；全部失败时回退 DefaultCDPPort。
func ResolveCDPPort() int {
	for _, path := range cdpPortCandidates() {
		port := ReadCDPPortFromFile(path)
		if probeCDPPortAlive(port) {
			return port
		}
	}
	return DefaultCDPPort
}

// CDPAddrForPort 将端口号格式化为可直接连接的 CDP 地址（127.0.0.1:<port>）。
func CDPAddrForPort(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// ResolveCDPAddr 返回可直接用于 HTTP/WebSocket 连接的 CDP 地址（127.0.0.1:<动态端口>）。
func ResolveCDPAddr() string {
	return CDPAddrForPort(ResolveCDPPort())
}

// ReserveCDPPort 向系统申请一个当前空闲的回环端口，供宿主 --remote-debugging-port 使用。
// 与 cmd/2ag/runner.go 的 reserveCDPPort 同源思路；申请失败时回退 DefaultCDPPort。
func ReserveCDPPort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("[2ag] 动态预留 CDP 端口失败，回退默认端口 %d: %v", DefaultCDPPort, err)
		return DefaultCDPPort
	}
	port := 0
	if addr, ok := l.Addr().(*net.TCPAddr); ok {
		port = addr.Port
	}
	_ = l.Close()
	if port <= 0 {
		return DefaultCDPPort
	}
	return port
}
