package supervisor

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件用真实套接字验证 CDP 长驻组件的生命周期，回答两个只能靠实测才能确认的问题：
//
//  1. 反复注入（初次注入 / 热重载 / 保活巡检补注入）是否会不断堆积持久会话与保活协程？
//  2. CloseAllCDPSessions() 之后，保活协程与巡检协程是否真的全部退出，而不是留在后台？
//
// 这两个问题不能靠读代码断言：会话注册表、keepalive、runMaintainLoop 都是长驻的，
// 「看起来有去重」与「实测不增长」是两件事。测试替身刻意复用生产代码里的
// readCDPFrame / writeCDPFrame，从而与真实 CDP 走同一套掩码与分帧规则；
// 正因如此它只证明 2Ag 自身的生命周期逻辑，不代表与 Chrome 的互通性
// （后者由真机运行与 scripts/audit/probe_persist.js 负责）。

// countGoroutinesContaining 统计当前所有协程栈里出现 frame 的次数。
// 用栈帧名而不是协程总数，是为了只盯住本包的长驻组件：测试进程中
// http.Server 等基础设施自身也有协程，总数对比会被它们干扰。
func countGoroutinesContaining(frame string) int {
	size := 1 << 20
	for {
		buf := make([]byte, size)
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), frame)
		}
		size *= 2
		if size > 1<<26 {
			return -1
		}
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时（%v）：%s", timeout, desc)
}

// fakeCDPServer 是一个最小但忠实的 CDP 端点：HTTP 上提供 /json 目标清单，
// websocket 上按方法名回包，并记录收到的命令序列与连接生死。
type fakeCDPServer struct {
	t    *testing.T
	ln   net.Listener
	addr string // host:port
	wsURL string

	mu       sync.Mutex
	conns    int      // 已接受的 websocket 连接数
	closed   int      // 已被客户端关闭的连接数
	cmds     []string // 收到的 method 序列（跨连接累计）
	scriptN  int      // addScriptToEvaluateOnNewDocument 的调用次数（用于造 id）
	rawConns []net.Conn
	srv      *http.Server
}

func newFakeCDPServer(t *testing.T) *fakeCDPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听测试端口失败: %v", err)
	}
	f := &fakeCDPServer{t: t, ln: ln, addr: ln.Addr().String()}
	f.wsURL = "ws://" + f.addr + "/devtools/page/FAKE"
	f.srv = &http.Server{Handler: f}
	go func() { _ = f.srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = f.srv.Close()
		f.mu.Lock()
		for _, c := range f.rawConns {
			_ = c.Close()
		}
		f.mu.Unlock()
	})
	return f
}

func (f *fakeCDPServer) snapshot() (conns, closed int, cmds []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(f.cmds))
	copy(cp, f.cmds)
	return f.conns, f.closed, cp
}

func (f *fakeCDPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/json":
		payload := []map[string]any{{
			"id":                   "FAKE",
			"title":                "2Ag Lifecycle Fixture",
			"type":                 "page",
			"url":                  "https://127.0.0.1:51999/",
			"webSocketDebuggerUrl": f.wsURL,
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	case "/devtools/page/FAKE":
		f.handleWebSocket(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeCDPServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}

	accept := base64.StdEncoding.EncodeToString(sha1Sum(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	fmt.Fprintf(brw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err := brw.Flush(); err != nil {
		_ = conn.Close()
		return
	}

	f.mu.Lock()
	f.conns++
	f.rawConns = append(f.rawConns, conn)
	f.mu.Unlock()

	// 帧编解码直接复用生产实现，保证与服务端同一套掩码/长度规则。
	reader := brw.Reader
	for {
		frame, opcode, err := readCDPFrame(reader)
		if err != nil {
			f.mu.Lock()
			f.closed++
			f.mu.Unlock()
			_ = conn.Close()
			return
		}
		if opcode == 9 { // Ping
			_ = writeCDPFrame(conn, 10, frame)
			continue
		}
		if opcode != 1 {
			continue
		}
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(frame, &req); err != nil {
			continue
		}
		f.mu.Lock()
		f.cmds = append(f.cmds, req.Method)
		f.mu.Unlock()

		resp := f.respond(req.ID, req.Method, req.Params)
		payload, _ := json.Marshal(resp)
		if err := writeCDPFrame(conn, 1, payload); err != nil {
			f.mu.Lock()
			f.closed++
			f.mu.Unlock()
			_ = conn.Close()
			return
		}
	}
}

func sha1Sum(s string) []byte {
	sum := sha1.Sum([]byte(s))
	return sum[:]
}

func (f *fakeCDPServer) respond(id int, method string, params map[string]any) map[string]any {
	switch method {
	case "Page.addScriptToEvaluateOnNewDocument":
		f.mu.Lock()
		f.scriptN++
		ident := fmt.Sprintf("script-%d", f.scriptN)
		f.mu.Unlock()
		return map[string]any{"id": id, "result": map[string]any{"identifier": ident}}
	case "Runtime.evaluate":
		expr, _ := params["expression"].(string)
		switch {
		case expr == "1": // 保活探针
			return map[string]any{"id": id, "result": map[string]any{
				"result": map[string]any{"type": "number", "value": 1}}}
		case expr == HostAccountProbeExpr:
			// 故意返回空串：本测试只关心生命周期，不希望在测试进程里
			// 触发 SetActiveAccount 改写全局主控账号状态。
			return map[string]any{"id": id, "result": map[string]any{
				"result": map[string]any{"type": "string", "value": ""}}}
		case strings.Contains(expr, "twoag-injected-root"): // 补丁存活探针
			return map[string]any{"id": id, "result": map[string]any{
				"result": map[string]any{"type": "boolean", "value": true}}}
		default: // 补丁源码本体
			return map[string]any{"id": id, "result": map[string]any{
				"result": map[string]any{"type": "undefined"}}}
		}
	default:
		return map[string]any{"id": id, "result": map[string]any{}}
	}
}

// TestSessionLifecycleDoesNotLeak 是本轮验收条件「无协程泄露」的直接证据。
func TestSessionLifecycleDoesNotLeak(t *testing.T) {
	// 栈帧里方法名带指针括号：(*persistentCDPSession).keepalive，
	// 所以匹配串必须含 ")." 这一段，否则永远匹配不到而让断言假通过。
	const (
		keepaliveFrame = "persistentCDPSession).keepalive"
		maintainFrame  = "runMaintainLoop"
	)

	f := newFakeCDPServer(t)
	exprV1 := "/* patch v1 */ window.__2ag_test = 1;"
	exprV2 := "/* patch v2 */ window.__2ag_test = 2;"

	if base := countGoroutinesContaining(keepaliveFrame); base != 0 {
		t.Fatalf("测试起点不干净：已存在 %d 个会话保活协程", base)
	}
	if base := countGoroutinesContaining(maintainFrame); base != 0 {
		t.Fatalf("测试起点不干净：已存在 %d 个保活巡检协程", base)
	}

	ctx := cdpDaemonContext()
	if err := performCDPInject(ctx, f.wsURL, exprV1); err != nil {
		t.Fatalf("首次注入失败: %v", err)
	}

	if got := len(cdpSessionRegistry.sessions); got != 1 {
		t.Fatalf("首次注入后应恰有 1 条持久会话，实际 %d 条", got)
	}
	conns, _, _ := f.snapshot()
	if conns != 1 {
		t.Fatalf("首次注入应只建立 1 条 websocket 连接，实际 %d 条", conns)
	}
	afterFirst := countGoroutinesContaining(keepaliveFrame)
	if afterFirst < 1 {
		// 防止假通过：若栈帧名写错，countGoroutinesContaining 恒为 0，
		// 后面「收束后归零」会在什么都没测的情况下成立。
		t.Fatalf("首次注入后未观测到会话保活协程栈帧，栈帧匹配串可能有误（%q）", keepaliveFrame)
	}

	// 关键断言：重复注入不得让会话与协程增长。
	// 调用方可能来自初次注入、HotReloadCDP 与 3 秒保活巡检三条路径，
	// 若 acquirePersistentCDPSession 的去重失效，这里就会线性膨胀。
	for i := 0; i < 4; i++ {
		if err := performCDPInject(ctx, f.wsURL, exprV1); err != nil {
			t.Fatalf("第 %d 次重复注入失败: %v", i+2, err)
		}
	}
	if got := len(cdpSessionRegistry.sessions); got != 1 {
		t.Fatalf("重复注入后会话数增长到 %d 条（应为 1）", got)
	}
	conns, _, _ = f.snapshot()
	if conns != 1 {
		t.Fatalf("重复注入后 websocket 连接增长到 %d 条（应为 1）", conns)
	}
	if got := countGoroutinesContaining(keepaliveFrame); got != afterFirst {
		t.Fatalf("保活协程随注入次数增长: %d → %d", afterFirst, got)
	}

	// 巡检协程同样必须去重：初次注入与保活巡检都会试图拉起它。
	maintainCDPInjection(f.addr, exprV1)
	maintainCDPInjection(f.addr, exprV1)
	maintainCDPInjection(f.addr, exprV1)
	waitForCondition(t, 2*time.Second, "保活巡检协程启动", func() bool {
		return countGoroutinesContaining(maintainFrame) >= 1
	})
	maintainMu.Lock()
	loops := len(maintainLoops)
	maintainMu.Unlock()
	if loops != 1 {
		t.Fatalf("同一 CDP 地址应只有 1 条巡检协程，实际登记 %d 条", loops)
	}
	if got := countGoroutinesContaining(maintainFrame); got != 1 {
		t.Fatalf("同一 CDP 地址的巡检协程栈应只有 1 份，实际 %d 份", got)
	}

	// 热重载换版：必须先在会话上摘掉旧注册再注册新版，
	// 否则新旧两份补丁会同跑（双份监听、双份中枢）。
	_, _, before := f.snapshot()
	if err := performCDPInject(ctx, f.wsURL, exprV2); err != nil {
		t.Fatalf("热重载注入失败: %v", err)
	}
	_, _, after := f.snapshot()
	if len(after) < len(before)+2 {
		t.Fatalf("换版时未见摘旧注册+注册新版两步，命令序列增量: %v", after[len(before):])
	}
	tail := after[len(before):]
	if tail[0] != "Page.removeScriptToEvaluateOnNewDocument" || tail[1] != "Page.addScriptToEvaluateOnNewDocument" {
		t.Fatalf("换版命令顺序错误，应为先摘后注册，实际: %v", tail)
	}
	session := lookupLiveSession(f.wsURL)
	if session == nil {
		t.Fatal("换版后持久会话不应消失")
	}
	if session.scriptIdentifier != "script-2" {
		t.Fatalf("换版后应记录新 identifier(script-2)，实际 %q", session.scriptIdentifier)
	}

	// 收束：CloseAllCDPSessions 必须让全部长驻组件退出。
	maintainMu.Lock()
	loopsBefore := len(maintainLoops)
	maintainMu.Unlock()
	if loopsBefore != 1 {
		t.Fatalf("收束前应恰有 1 条巡检登记，实际 %d 条", loopsBefore)
	}
	CloseAllCDPSessions()

	if got := len(cdpSessionRegistry.sessions); got != 0 {
		t.Fatalf("收束后会话注册表应为空，实际残留 %d 条", got)
	}
	waitForCondition(t, 3*time.Second, "保活协程退出", func() bool {
		return countGoroutinesContaining(keepaliveFrame) == 0
	})
	waitForCondition(t, 3*time.Second, "巡检协程退出", func() bool {
		return countGoroutinesContaining(maintainFrame) == 0
	})
	waitForCondition(t, 3*time.Second, "巡检登记表清空", func() bool {
		maintainMu.Lock()
		defer maintainMu.Unlock()
		return len(maintainLoops) == 0
	})

	// 连接也要真的关掉，否则对端会一直挂在 FIN_WAIT 上。
	waitForCondition(t, 3*time.Second, "服务端观察到连接关闭", func() bool {
		conns, closed, _ := f.snapshot()
		return closed >= conns && conns > 0
	})

	// 幂等性：二次收束不得 panic 或重新制造残留。
	CloseAllCDPSessions()
	if got := countGoroutinesContaining(keepaliveFrame); got != 0 {
		t.Fatalf("二次收束后仍残留 %d 个保活协程", got)
	}

	t.Logf("生命周期验证通过：注入 5 次保持 1 条会话 / 1 条保活协程 / 1 条巡检协程，收束后全部归零")
}

// TestEnsureScriptSwapsRegistrationOnVersionChange 用 net.Pipe 直接驱动会话，
// 精确验证「同版本不重复注册、换版本先摘后注册」这一热重载前提。
func TestEnsureScriptSwapsRegistrationOnVersionChange(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var mu sync.Mutex
	var cmds []string
	scriptN := 0

	go func() {
		reader := bufio.NewReader(serverConn)
		for {
			frame, opcode, err := readCDPFrame(reader)
			if err != nil {
				return
			}
			if opcode != 1 {
				continue
			}
			var req struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(frame, &req); err != nil {
				continue
			}
			mu.Lock()
			cmds = append(cmds, req.Method)
			var resp map[string]any
			if req.Method == "Page.addScriptToEvaluateOnNewDocument" {
				scriptN++
				resp = map[string]any{"id": req.ID, "result": map[string]any{
					"identifier": fmt.Sprintf("script-%d", scriptN)}}
			} else {
				resp = map[string]any{"id": req.ID, "result": map[string]any{}}
			}
			mu.Unlock()
			payload, _ := json.Marshal(resp)
			if err := writeCDPFrame(serverConn, 1, payload); err != nil {
				return
			}
		}
	}()

	s := &persistentCDPSession{
		wsURL:  "ws://fake/session",
		conn:   clientConn,
		reader: bufio.NewReader(clientConn),
	}

	cmdSnapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]string, len(cmds))
		copy(cp, cmds)
		return cp
	}

	if err := s.ensureScript("patch-v1"); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	first := cmdSnapshot()
	if len(first) != 1 || first[0] != "Page.addScriptToEvaluateOnNewDocument" {
		t.Fatalf("首次注册命令序列异常: %v", first)
	}
	if s.scriptIdentifier != "script-1" || !s.scriptRegistered {
		t.Fatalf("首次注册后状态异常: identifier=%q registered=%v", s.scriptIdentifier, s.scriptRegistered)
	}

	// 同版本重复调用必须零流量：保活巡检每 3 秒都会走这条路。
	if err := s.ensureScript("patch-v1"); err != nil {
		t.Fatalf("同版本重复注册失败: %v", err)
	}
	if got := len(cmdSnapshot()); got != 1 {
		t.Fatalf("同版本重复注册产生了额外 CDP 流量，命令数 %d（应为 1）", got)
	}

	// 换版本：必须先摘旧注册。
	if err := s.ensureScript("patch-v2"); err != nil {
		t.Fatalf("换版注册失败: %v", err)
	}
	all := cmdSnapshot()
	if len(all) != 3 {
		t.Fatalf("换版应恰好产生 2 条命令，实际总数 %d: %v", len(all), all)
	}
	if all[1] != "Page.removeScriptToEvaluateOnNewDocument" {
		t.Fatalf("换版时第一步应为摘除旧注册，实际 %q", all[1])
	}
	if all[2] != "Page.addScriptToEvaluateOnNewDocument" {
		t.Fatalf("换版时第二步应为注册新版，实际 %q", all[2])
	}
	if s.scriptIdentifier != "script-2" || s.scriptSource != "patch-v2" {
		t.Fatalf("换版后状态异常: identifier=%q source=%q", s.scriptIdentifier, s.scriptSource)
	}

	// 关闭后的会话必须拒绝一切交互，而不是写到一条半开的连接上。
	s.shutdown()
	if err := s.ensureScript("patch-v3"); err != errCDPSessionClosed {
		t.Fatalf("已关闭会话应返回 errCDPSessionClosed，实际 %v", err)
	}
	if err := s.exchange(77, map[string]any{"id": 77, "method": "Page.enable"}); err != errCDPSessionClosed {
		t.Fatalf("已关闭会话的 exchange 应返回 errCDPSessionClosed，实际 %v", err)
	}
}

// 保证测试替身与生产代码共用同一套分帧规则时，读写是对称的。
func TestFakeServerFrameRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	payload := []byte(`{"id":1,"method":"Page.enable"}`)
	go func() {
		_ = writeCDPFrame(a, 1, payload)
	}()
	got, opcode, err := readCDPFrame(bufio.NewReader(b))
	if err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	if opcode != 1 || string(got) != string(payload) {
		t.Fatalf("帧往返不一致: opcode=%d payload=%q", opcode, got)
	}
	// 大于 125 字节的负载走 126 长度分支，容易被掩码逻辑写错。
	long := make([]byte, 300)
	for i := range long {
		long[i] = byte('a' + i%26)
	}
	go func() {
		_ = writeCDPFrame(a, 1, long)
	}()
	got, opcode, err = readCDPFrame(bufio.NewReader(b))
	if err != nil {
		t.Fatalf("长帧读失败: %v", err)
	}
	if opcode != 1 || len(got) != len(long) || string(got) != string(long) {
		t.Fatalf("长帧往返不一致: opcode=%d len=%d", opcode, len(got))
	}
}
