package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// 持久 CDP 会话。
//
// 为什么必须有这一层：Page.addScriptToEvaluateOnNewDocument 注册的脚本是「会话绑定」的。
// 实测（scripts/audit/probe_persist.js，同一台机器同一次运行内四组对照）：
//
//	A1 同会话·首次导航   mark=1
//	A2 同会话·二次导航   mark=1
//	B  会话关闭后导航    mark=0   ← 注册随连接一起消失
//	C1 新会话重新注册    mark=1
//	C2 该会话也关闭后    mark=0   ← 结论稳定复现
//
// 历史实现 performCDPInject 在注册完 Page.addScriptToEvaluateOnNewDocument 之后
// 紧跟着 defer conn.Close()，等于把刚签好的「跳转自愈契约」当场撕毁：
// 宿主内部每一次路由跳转/刷新都会回到无补丁状态，只能靠 3s 巡检事后补救。
// 这就是「补丁被冲刷消失、用户最终看到的是原版」的根因。
//
// 本层把「脚本已注册在一条活着的连接上」变成一个被持续维护的不变式：
// 会话常驻并保活，只有连接真的失效时才重建（重建时会重新注册）。
const (
	cdpSessionKeepaliveInterval = 4 * time.Second
	cdpSessionIOTimeout         = 6 * time.Second
	cdpSessionEvalID            = 9001
	cdpSessionScriptID          = 2
)

var errCDPSessionClosed = errors.New("CDP 持久会话已关闭")

var cdpSessionRegistry = struct {
	sync.Mutex
	sessions map[string]*persistentCDPSession
}{sessions: make(map[string]*persistentCDPSession)}

// 补丁保活巡检的去重登记表：同一个 CDP 地址只允许存在一条巡检协程。
var (
	maintainMu    sync.Mutex
	maintainLoops = make(map[string]struct{})
)

var (
	cdpDaemonOnce   sync.Once
	cdpDaemonCtx    context.Context
	cdpDaemonCancel context.CancelFunc
)

// cdpDaemonContext 是所有 CDP 长驻组件（会话保活、补丁保活巡检）共用的生命周期锚点。
// 它刻意不绑定任何单次注入调用传入的短超时 ctx —— 那些 ctx 几秒后就 Done 了，
// 若把保活挂上去，会话会在注入刚完成时立刻取消自己。
// 收束点：cmd/2ag/manager.go 在 os.Exit 之前显式调用 CloseAllCDPSessions()
// （os.Exit 不执行 defer，不能依赖 defer 收尾）。
func cdpDaemonContext() context.Context {
	cdpDaemonOnce.Do(func() {
		cdpDaemonCtx, cdpDaemonCancel = context.WithCancel(context.Background())
	})
	return cdpDaemonCtx
}

// CloseAllCDPSessions 取消全部长驻 CDP 组件并关闭持久会话。幂等。
func CloseAllCDPSessions() {
	_ = cdpDaemonContext() // 确保 cancel 已初始化
	if cdpDaemonCancel != nil {
		cdpDaemonCancel()
	}

	cdpSessionRegistry.Lock()
	stale := make([]*persistentCDPSession, 0, len(cdpSessionRegistry.sessions))
	for _, s := range cdpSessionRegistry.sessions {
		stale = append(stale, s)
	}
	cdpSessionRegistry.sessions = make(map[string]*persistentCDPSession)
	cdpSessionRegistry.Unlock()

	for _, s := range stale {
		s.shutdown()
	}
}

type persistentCDPSession struct {
	wsURL  string
	conn   net.Conn
	reader *bufio.Reader

	mu     sync.Mutex
	closed bool

	// 已注册的补丁版本。换版（热重载读到新的 injected_hub.js）时必须先摘掉旧注册，
	// 否则新文档会把新旧两份补丁各跑一遍 —— 双份监听、双份中枢。
	scriptRegistered bool
	scriptSource     string
	scriptIdentifier string

	cancel context.CancelFunc
}

// acquirePersistentCDPSession 返回一个仍然存活的持久会话，必要时新建并把补丁注册上去。
// 注意：调用方传入的 ctx 只用于本次建连拨号，会话本身的寿命不跟它走。
func acquirePersistentCDPSession(ctx context.Context, wsURL string, expression string) (*persistentCDPSession, error) {
	cdpSessionRegistry.Lock()
	existing, ok := cdpSessionRegistry.sessions[wsURL]
	cdpSessionRegistry.Unlock()
	if ok {
		if err := existing.ensureScript(expression); err == nil {
			return existing, nil
		}
		// 会话虽在注册表里但已不可用（半开连接等）：摘掉它，走重建路径。
		existing.detach()
	}

	conn, reader, err := openCDPWebSocket(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("建立 CDP 持久连接失败: %w", err)
	}

	sessionCtx, cancel := context.WithCancel(cdpDaemonContext())
	s := &persistentCDPSession{wsURL: wsURL, conn: conn, reader: reader, cancel: cancel}

	// 顺序要点：必须先 Page.enable 打开 Page 域，否则 addScriptToEvaluateOnNewDocument
	// 会被 CDP 以 "Page.enable wasn't called" 拒绝，而注册会静默失败 ——
	// 那正是「以为注册上了、其实什么都没发生」的来源。
	if err := s.exchange(1, map[string]any{"id": 1, "method": "Page.enable"}); err != nil {
		log.Printf("[2ag] Page.enable: %v", err)
	}

	if err := s.ensureScript(expression); err != nil {
		s.shutdown()
		return nil, fmt.Errorf("注册跳转自愈契约失败: %w", err)
	}

	cdpSessionRegistry.Lock()
	if raced, ok := cdpSessionRegistry.sessions[wsURL]; ok {
		cdpSessionRegistry.Unlock()
		s.shutdown() // 并发下别的调用已先建好同 wsURL 的会话，丢弃自己这份
		_ = raced.ensureScript(expression)
		return raced, nil
	}
	cdpSessionRegistry.sessions[wsURL] = s
	cdpSessionRegistry.Unlock()

	go s.keepalive(sessionCtx)
	log.Printf("[2ag] 已建立 CDP 持久会话（补丁 %d 字节已在会话上注册，跳转自愈契约生效）: %s", len(expression), wsURL)
	return s, nil
}

// lookupLiveSession 取出指定目标的持久会话（可能正在失效，调用方需自行处理交换错误）。
func lookupLiveSession(wsURL string) *persistentCDPSession {
	cdpSessionRegistry.Lock()
	defer cdpSessionRegistry.Unlock()
	return cdpSessionRegistry.sessions[wsURL]
}

// ensureScript 让会话上「恰好存在一份」当前版本的补丁注册。
func (s *persistentCDPSession) ensureScript(expression string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errCDPSessionClosed
	}
	if s.scriptRegistered && s.scriptSource == expression {
		return nil
	}
	if s.scriptIdentifier != "" {
		if _, err := s.commandLocked(3, map[string]any{
			"id":     3,
			"method": "Page.removeScriptToEvaluateOnNewDocument",
			"params": map[string]any{"identifier": s.scriptIdentifier},
		}); err != nil {
			// 摘旧版失败不致命（新注册仍会生效），但必须留痕：否则热重载会
			// 悄悄留下两份补丁同跑，表现为双份监听、双份中枢。
			log.Printf("[2ag] 警告: 摘除旧版补丁注册失败（可能导致新旧两份补丁同跑）: %v", err)
		}
		s.scriptIdentifier = ""
		s.scriptRegistered = false
		s.scriptSource = ""
	}

	raw, err := s.commandLocked(cdpSessionScriptID, map[string]any{
		"id":     cdpSessionScriptID,
		"method": "Page.addScriptToEvaluateOnNewDocument",
		"params": map[string]any{"source": expression},
	})
	if err != nil {
		return err
	}
	var parsed struct {
		Identifier string `json:"identifier"`
	}
	_ = json.Unmarshal(raw, &parsed)
	s.scriptIdentifier = parsed.Identifier
	s.scriptRegistered = true
	s.scriptSource = expression
	if s.scriptIdentifier == "" {
		// 极端情况：拿不到 identifier，后续无法定向摘除注册。
		log.Printf("[2ag] 警告: addScriptToEvaluateOnNewDocument 未返回 identifier，该会话上的旧版注册将无法定向摘除")
	}
	return nil
}

// exchange 在会话上执行一条 CDP 命令（串行化，避免多协程交叉读帧）。
func (s *persistentCDPSession) exchange(id int, command map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exchangeLocked(id, command)
}

func (s *persistentCDPSession) exchangeLocked(id int, command map[string]any) error {
	if s.closed {
		return errCDPSessionClosed
	}
	if err := s.conn.SetDeadline(time.Now().Add(cdpSessionIOTimeout)); err != nil {
		return err
	}
	defer s.conn.SetDeadline(time.Time{})
	return sendCDPCommand(s.conn, s.reader, id, command)
}

// commandLocked 执行命令并返回其 result 负载（需要读取 identifier 之类的返回值时使用）。
// 调用方必须已持有 s.mu。
func (s *persistentCDPSession) commandLocked(id int, command map[string]any) (json.RawMessage, error) {
	if s.closed {
		return nil, errCDPSessionClosed
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	if err := s.conn.SetDeadline(time.Now().Add(cdpSessionIOTimeout)); err != nil {
		return nil, err
	}
	defer s.conn.SetDeadline(time.Time{})
	if err := writeCDPFrame(s.conn, 1, payload); err != nil {
		return nil, err
	}
	for {
		frame, opcode, err := readCDPFrame(s.reader)
		if err != nil {
			return nil, err
		}
		if opcode == 9 { // Ping
			_ = writeCDPFrame(s.conn, 10, frame) // Pong
			continue
		}
		if opcode != 1 {
			continue
		}
		var response struct {
			ID     int             `json:"id"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(frame, &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("CDP 错误: %s", response.Error.Message)
		}
		return response.Result, nil
	}
}

// evaluate 在当前文档上即时执行补丁源码。
func (s *persistentCDPSession) evaluate(id int, expression string) error {
	return s.exchange(id, map[string]any{
		"id":     id,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expression,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	})
}

// evalRaw 在当前文档上执行表达式并返回其值（按值传递）。
func (s *persistentCDPSession) evalRaw(id int, expression string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := s.commandLocked(id, map[string]any{
		"id":     id,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expression,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Result.Value, nil
}

// evalString 在当前文档上执行表达式并只取字符串结果。
func (s *persistentCDPSession) evalString(id int, expression string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errCDPSessionClosed
	}
	if err := s.conn.SetDeadline(time.Now().Add(cdpSessionIOTimeout)); err != nil {
		return "", err
	}
	defer s.conn.SetDeadline(time.Time{})
	return evaluateCDPString(s.conn, s.reader, id, expression)
}

// keepalive 周期性地在会话上打一次极廉价的往返，确认连接与 Page 域都还活着。
// 这是有界的：ctx 结束（CloseAllCDPSessions）或会话失效时协程立即退出，不会堆积。
func (s *persistentCDPSession) keepalive(ctx context.Context) {
	ticker := time.NewTicker(cdpSessionKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			err := s.exchange(cdpSessionEvalID, map[string]any{
				"id":     cdpSessionEvalID,
				"method": "Runtime.evaluate",
				"params": map[string]any{"expression": "1", "returnByValue": true},
			})
			if err != nil {
				log.Printf("[2ag] CDP 持久会话失效（%v），已注销并交由下一次注入重建: %s", err, s.wsURL)
				s.detach()
				return
			}
		}
	}
}

// detach 把会话从注册表摘除并关闭，使后续注入能够重建一条健康的会话。
func (s *persistentCDPSession) detach() {
	cdpSessionRegistry.Lock()
	if cdpSessionRegistry.sessions[s.wsURL] == s {
		delete(cdpSessionRegistry.sessions, s.wsURL)
	}
	cdpSessionRegistry.Unlock()
	s.shutdown()
}

func (s *persistentCDPSession) shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	_ = s.conn.Close()
}
