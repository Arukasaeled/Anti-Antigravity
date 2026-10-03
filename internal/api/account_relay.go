package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/supervisor"
)

const relayThreshold = 5

type relayMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type relayPulse struct {
	Busy         bool           `json:"busy"`
	Conversation string         `json:"conversation"`
	Title        string         `json:"title"`
	Pool         string         `json:"pool"`
	Model        string         `json:"model"`
	ProjectName  string         `json:"project_name"`
	ProjectPath  string         `json:"project_path"`
	Messages     []relayMessage `json:"messages"`
}

type relayCandidate struct {
	Email     string `json:"email"`
	Remaining int    `json:"remaining"`
}

type relayEvent struct {
	At      string `json:"at"`
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

type relayHandoff struct {
	ID           string `json:"id"`
	From         string `json:"from"`
	To           string `json:"to"`
	Conversation string `json:"conversation"`
	Title        string `json:"title"`
	Model        string `json:"model"`
	ProjectPath  string `json:"project_path"`
	Prompt       string `json:"prompt"`
	SendClaimed  bool   `json:"send_claimed"`
	ClaimID      string `json:"claim_id,omitempty"`
}

type relayState struct {
	Enabled           bool             `json:"enabled"`
	Pool              string           `json:"pool"`
	WorkspacePath     string           `json:"workspace_path"`
	TaskBrief         string           `json:"task_brief"`
	BriefConversation string           `json:"brief_conversation"`
	DispatchAt        int64            `json:"dispatch_at,omitempty"`
	Phase             string           `json:"phase"`
	Message           string           `json:"message"`
	From              string           `json:"from,omitempty"`
	To                string           `json:"to,omitempty"`
	CheckedAt         string           `json:"checked_at,omitempty"`
	LastSwitchAt      int64            `json:"last_switch_at,omitempty"`
	Candidates        []relayCandidate `json:"candidates"`
	History           []relayEvent     `json:"history"`
	Handoff           *relayHandoff    `json:"handoff,omitempty"`
}

// Pulses are real host DOM observations. No timer or synthetic task state
// starts a switch; while disabled, pulses do not even probe account quotas.
type accountRelay struct {
	mu        sync.Mutex
	state     relayState
	path      string
	pulse     relayPulse
	seen      time.Time
	lastCheck time.Time
	inFlight  bool
	revision  uint64
	failed    map[string]time.Time
}

func newAccountRelay() *accountRelay {
	r := &accountRelay{state: relayState{Pool: "auto", Phase: "off"}, failed: make(map[string]time.Time)}
	if home, err := os.UserHomeDir(); err == nil {
		r.path = filepath.Join(home, ".2ag", "workspace", "relay.json")
		if raw, err := os.ReadFile(r.path); err == nil {
			var saved relayState
			if json.Unmarshal(raw, &saved) == nil {
				r.state = saved
			}
		}
	}
	if r.state.Pool != "gemini" && r.state.Pool != "claude" {
		r.state.Pool = "auto"
	}
	if r.state.Handoff != nil {
		if r.state.Phase == "dispatching" || r.state.Phase == "needs-confirmation" {
			r.state.Handoff.SendClaimed = true
		}
		if r.state.Handoff.SendClaimed {
			r.state.Phase = "needs-confirmation"
		} else {
			r.state.Phase = "paused"
		}
		r.state.Message = "保留上次任务交接；请确认当前任务后再恢复"
	} else if r.state.Enabled {
		r.state.Phase = "idle"
	} else {
		r.state.Phase = "off"
	}
	return r
}

func (r *accountRelay) saveLocked() error {
	if r.path == "" {
		return fmt.Errorf("无法定位接力数据目录")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.path), ".relay-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), r.path)
}

func (r *accountRelay) recordLocked(phase, message string) {
	r.state.Phase, r.state.Message = phase, message
	r.state.History = append(r.state.History, relayEvent{At: time.Now().UTC().Format(time.RFC3339), Phase: phase, Message: message})
	if len(r.state.History) > 20 {
		r.state.History = r.state.History[len(r.state.History)-20:]
	}
}

func (r *accountRelay) snapshot() relayState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Phase == "dispatching" && r.state.DispatchAt > 0 && time.Now().UnixMilli()-r.state.DispatchAt > 30000 {
		r.recordLocked("needs-confirmation", "原生发送结果未确认，交接已保留；请检查会话后确认")
		_ = r.saveLocked()
	}
	out := r.state
	out.History = append([]relayEvent(nil), out.History...)
	out.Candidates = append([]relayCandidate(nil), out.Candidates...)
	// Context is served separately after checking the actual destination owner.
	if out.Handoff != nil {
		copy := *out.Handoff
		copy.Prompt = ""
		out.Handoff = &copy
	}
	return out
}

func (r *accountRelay) beginManualSwitch() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Phase == "switching" || r.state.Phase == "dispatching" {
		return fmt.Errorf("自动接力正在切号或续发，请等待当前操作完成")
	}
	r.revision++
	r.pulse.Busy = false
	if r.state.Handoff != nil {
		r.state.Phase = "paused"
	}
	return nil
}

func relayPool(acc supervisor.AccountInstance, pool string) supervisor.QuotaWindow {
	if pool == "claude" {
		return acc.ClaudePool
	}
	return acc.GeminiPool
}

func liveRelayPool(pool supervisor.QuotaWindow, now time.Time) bool {
	return pool.Available && !pool.Stale && strings.HasPrefix(pool.Source, "live") &&
		pool.UpdatedAt > 0 && now.UnixMilli()-pool.UpdatedAt >= 0 && now.UnixMilli()-pool.UpdatedAt < 90000
}

func relayPoolScore(pool supervisor.QuotaWindow) int {
	if pool.FiveHourPercent < pool.WeeklyPercent {
		return pool.FiveHourPercent
	}
	return pool.WeeklyPercent
}

func (r *accountRelay) pulseHost(p relayPulse, mode string) {
	r.mu.Lock()
	r.pulse, r.seen = p, time.Now()
	if !r.state.Enabled || r.inFlight || r.state.Handoff != nil || mode != config.RuntimeModeEnhanced {
		r.mu.Unlock()
		return
	}
	if !p.Busy || p.Conversation == "" || len(p.Messages) == 0 {
		r.state.Phase = "idle"
		r.mu.Unlock()
		return
	}
	if time.Since(r.lastCheck) < 20*time.Second || time.Now().UnixMilli()-r.state.LastSwitchAt < 120000 {
		r.mu.Unlock()
		return
	}
	r.inFlight, r.lastCheck = true, time.Now()
	revision := r.revision
	r.state.Phase = "checking"
	r.mu.Unlock()
	go r.evaluate(mode, revision)
}

func (r *accountRelay) evaluate(mode string, revision uint64) {
	defer func() { r.mu.Lock(); r.inFlight = false; r.mu.Unlock() }()
	owner, err := supervisor.ReadHostLoginEmail()
	if err != nil || owner == "" {
		r.block(revision, "无法确认当前登录账号")
		return
	}
	accounts := supervisor.ScanLocalAccounts()
	now := time.Now()
	r.mu.Lock()
	deferUnlock := true
	defer func() {
		if deferUnlock {
			r.mu.Unlock()
		}
	}()
	if !r.state.Enabled || revision != r.revision || !r.pulse.Busy || now.Sub(r.seen) > 40*time.Second {
		return
	}
	r.state.CheckedAt = now.UTC().Format(time.RFC3339)
	pool := r.state.Pool
	if pool == "auto" {
		pool = r.pulse.Pool
	}
	var active *supervisor.AccountInstance
	for i := range accounts {
		if strings.EqualFold(accounts[i].Email, owner) {
			active = &accounts[i]
			break
		}
	}
	if active == nil {
		r.state.Phase, r.state.Message = "blocked", "当前账号未登记"
		return
	}
	if pool != "gemini" && pool != "claude" {
		// Without an identifiable selected model, look for a genuinely low pool.
		pool = ""
		for _, name := range []string{"gemini", "claude"} {
			q := relayPool(*active, name)
			if liveRelayPool(q, now) && ((q.FiveHourKnown && q.FiveHourPercent <= relayThreshold) || (q.WeeklyKnown && q.WeeklyPercent <= relayThreshold)) {
				pool = name
				break
			}
		}
		if pool == "" {
			r.state.Phase, r.state.Message = "watching", "等待当前任务的实时低配额读数"
			return
		}
	}
	q := relayPool(*active, pool)
	if !liveRelayPool(q, now) {
		r.state.Phase, r.state.Message = "blocked", "配额非实时或已过期，不据此切号"
		return
	}
	low := (q.FiveHourKnown && q.FiveHourPercent <= relayThreshold) || (q.WeeklyKnown && q.WeeklyPercent <= relayThreshold)
	if !low {
		r.state.Phase, r.state.Message = "watching", "当前任务配额充足"
		return
	}
	brief := ""
	if r.state.BriefConversation == r.pulse.Conversation {
		brief = r.state.TaskBrief
	}
	hasInstructions := strings.TrimSpace(brief) != ""
	for _, message := range r.pulse.Messages {
		if message.Role == "user" && strings.TrimSpace(message.Text) != "" {
			hasInstructions = true
			break
		}
	}
	if !hasInstructions {
		r.state.Phase, r.state.Message = "needs-context", "没有加载到用户指令，请填写接力备忘后再继续"
		return
	}
	candidates := []relayCandidate{}
	for _, account := range accounts {
		if strings.EqualFold(account.Email, owner) || now.Before(r.failed[strings.ToLower(account.Email)]) {
			continue
		}
		p := relayPool(account, pool)
		if !liveRelayPool(p, now) || !p.FiveHourKnown || !p.WeeklyKnown || relayPoolScore(p) <= relayThreshold {
			continue
		}
		if !supervisor.AccountCredentialReady(account.Email) {
			continue
		}
		candidates = append(candidates, relayCandidate{Email: account.Email, Remaining: relayPoolScore(p)})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Remaining > candidates[j].Remaining })
	r.state.Candidates = candidates
	if len(candidates) == 0 {
		r.state.Phase, r.state.Message = "blocked", "没有实时配额高于 5% 且凭据可用的备选账号"
		return
	}
	p := r.pulse
	projectPath := r.state.WorkspacePath
	if projectPath == "" {
		projectPath = supervisor.RelayWorkspacePath(owner, p.ProjectPath, p.ProjectName)
	}
	if p.ProjectName != "" && projectPath == "" {
		r.state.Phase, r.state.Message = "needs-workspace", "未取得项目目录，请在接力设置中填写工作目录"
		return
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		r.state.Phase, r.state.Message = "blocked", "无法生成任务交接 ID"
		return
	}
	handoffID := hex.EncodeToString(id)
	handoff := &relayHandoff{ID: handoffID, From: owner, To: candidates[0].Email, Conversation: p.Conversation, Title: p.Title, Model: p.Model, ProjectPath: projectPath, Prompt: relayPrompt(p, projectPath, handoffID, brief)}
	r.state.Handoff, r.state.From, r.state.To = handoff, owner, handoff.To
	r.recordLocked("switching", "当前 "+pool+" 配额 ≤5%，正在接力至 "+handoff.To)
	if err := r.saveLocked(); err != nil {
		r.state.Handoff = nil
		r.state.Phase, r.state.Message = "blocked", "任务交接保存失败，未切号"
		return
	}
	conversation := p.Conversation
	r.mu.Unlock()
	deferUnlock = false
	result, switchErr := supervisor.SwitchAccountTransactional(handoff.To, mode, supervisor.AccountSwitchOptions{
		ExpectedOwner: owner, WorkspacePath: projectPath,
		BeforeStop: func() error {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.state.Enabled || revision != r.revision || !r.pulse.Busy || r.pulse.Conversation != conversation || time.Since(r.seen) > 40*time.Second {
				return fmt.Errorf("接力已关闭或原任务已改变，未切号")
			}
			return nil
		},
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if switchErr != nil {
		r.failed[strings.ToLower(handoff.To)] = time.Now().Add(10 * time.Minute)
		r.recordLocked("paused", switchErr.Error())
	} else if result.Verified && strings.EqualFold(result.VerifiedOwner, handoff.To) {
		r.state.LastSwitchAt = time.Now().UnixMilli()
		if r.state.Enabled && revision == r.revision {
			r.recordLocked("resume-ready", "切号完成，等待原生输入区接力")
		} else {
			r.recordLocked("paused", "切号完成；接力已暂停，交接内容已保留")
		}
	} else {
		r.recordLocked("paused", "未确认目标身份，保留交接内容")
	}
	if err := r.saveLocked(); err != nil {
		r.state.Phase, r.state.Message = "paused", "接力状态保存失败，请手动确认任务"
	}
}

func (r *accountRelay) block(revision uint64, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if revision == r.revision {
		r.state.Phase, r.state.Message = "blocked", message
	}
}

func relayPrompt(p relayPulse, path, id, brief string) string {
	var out strings.Builder
	out.WriteString("2Ag handoff ID: " + id + "\n")
	out.WriteString("Continue the same task after an account quota handoff. Preserve existing edits. Inspect the working directory and current results before resuming; do not redo completed work. Follow the original user's instructions below.\n")
	if strings.TrimSpace(brief) != "" {
		out.WriteString("User's retained task instructions:\n" + brief + "\n")
	}
	if path != "" {
		out.WriteString("Working directory: " + path + "\n")
	}
	if p.ProjectName != "" {
		out.WriteString("Project: " + p.ProjectName + "\n")
	}
	out.WriteString("Original conversation: " + p.Conversation + "\nCaptured loaded messages (local text, not an AI summary):\n")
	for _, message := range p.Messages {
		out.WriteString("\n[" + message.Role + "]\n" + message.Text + "\n")
	}
	return out.String()
}

func (s *Server) handleAccountRelay(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if req.Method == http.MethodGet {
		json.NewEncoder(w).Encode(s.relay.snapshot())
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input struct {
		Enabled           *bool   `json:"enabled"`
		Pool              string  `json:"pool"`
		WorkspacePath     *string `json:"workspace_path"`
		TaskBrief         *string `json:"task_brief"`
		BriefConversation string  `json:"brief_conversation"`
		Action            string  `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 24000)).Decode(&input); err != nil {
		http.Error(w, "invalid relay settings", 400)
		return
	}
	if input.TaskBrief != nil && len(*input.TaskBrief) > 8000 {
		http.Error(w, "task brief too large", 400)
		return
	}
	if input.TaskBrief != nil && strings.TrimSpace(*input.TaskBrief) != "" && (input.BriefConversation == "" || len(input.BriefConversation) > 256) {
		http.Error(w, "open the task conversation before saving its handoff note", 400)
		return
	}
	if input.Pool != "" && input.Pool != "auto" && input.Pool != "gemini" && input.Pool != "claude" {
		http.Error(w, "invalid quota pool", 400)
		return
	}
	if input.WorkspacePath != nil && strings.TrimSpace(*input.WorkspacePath) != "" {
		path := strings.TrimSpace(*input.WorkspacePath)
		info, err := os.Stat(path)
		if !filepath.IsAbs(path) || err != nil || !info.IsDir() {
			http.Error(w, "工作目录必须是存在的绝对目录", 400)
			return
		}
	}
	r := s.relay
	r.mu.Lock()
	before := r.state
	if input.Enabled != nil {
		r.state.Enabled = *input.Enabled
	}
	if input.Pool != "" {
		r.state.Pool = input.Pool
	}
	if input.WorkspacePath != nil {
		r.state.WorkspacePath = strings.TrimSpace(*input.WorkspacePath)
	}
	if input.TaskBrief != nil {
		r.state.TaskBrief, r.state.BriefConversation = *input.TaskBrief, input.BriefConversation
	}
	switch input.Action {
	case "", "retry", "dismiss", "confirm-running", "confirm-not-sent":
	default:
		r.state = before
		r.mu.Unlock()
		http.Error(w, "invalid relay action", 400)
		return
	}
	if r.inFlight && input.Action != "" {
		r.state = before
		r.mu.Unlock()
		http.Error(w, "接力正在进行，请等待当前切换完成", 409)
		return
	}
	if input.Action == "dismiss" {
		r.state.Handoff = nil
	}
	if input.Action == "confirm-running" || input.Action == "confirm-not-sent" {
		if r.state.Handoff == nil || !r.state.Handoff.SendClaimed {
			r.state = before
			r.mu.Unlock()
			http.Error(w, "no uncertain send to confirm", 409)
			return
		}
		if input.Action == "confirm-running" {
			r.state.Handoff = nil
			r.recordLocked("running", "用户确认原生任务已接收")
		} else {
			copy := *r.state.Handoff
			copy.SendClaimed = false
			r.state.Handoff = &copy
			input.Action = "retry"
		}
	}
	if input.Action == "retry" && r.state.Handoff != nil {
		if r.state.Handoff.SendClaimed {
			r.state = before
			r.mu.Unlock()
			http.Error(w, "原生发送状态待确认，不能重复发送", 409)
			return
		}
		owner, err := supervisor.ReadHostLoginEmail()
		if err != nil || !strings.EqualFold(owner, r.state.Handoff.To) {
			r.state = before
			r.mu.Unlock()
			http.Error(w, "当前登录账号不是交接目标，请先手动切到目标账号", 409)
			return
		}
		r.state.Phase = "resume-ready"
		r.state.Enabled = true
	} else if r.state.Handoff != nil {
		if r.state.Handoff.SendClaimed {
			r.state.Phase = "needs-confirmation"
		} else {
			r.state.Phase = "paused"
		}
	} else if input.Action != "confirm-running" {
		if r.state.Enabled {
			r.state.Phase = "idle"
		} else {
			r.state.Phase = "off"
		}
	}
	if err := r.saveLocked(); err != nil {
		r.state = before
		r.mu.Unlock()
		http.Error(w, "保存接力设置失败", 500)
		return
	}
	r.revision++
	r.mu.Unlock()
	json.NewEncoder(w).Encode(r.snapshot())
}

func (s *Server) handleRelayPulse(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var pulse relayPulse
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 300000)).Decode(&pulse); err != nil {
		http.Error(w, "invalid task observation", 400)
		return
	}
	if len(pulse.Messages) > 60 || len(pulse.Conversation) > 256 || len(pulse.Model) > 120 || len(pulse.ProjectPath) > 4096 || len(pulse.Title) > 512 {
		http.Error(w, "task observation too large", 400)
		return
	}
	size := 0
	for _, message := range pulse.Messages {
		size += len(message.Text)
	}
	if size > 96000 {
		http.Error(w, "loaded context exceeds relay limit; pin the essential context first", 400)
		return
	}
	s.relay.pulseHost(pulse, s.currentRuntimeMode())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.relay.snapshot())
}

func (s *Server) handleRelayHandoff(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	r := s.relay
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.Method == http.MethodGet {
		if r.state.Handoff == nil {
			json.NewEncoder(w).Encode(nil)
			return
		}
		// The local user may copy retained context even after a failed switch.
		json.NewEncoder(w).Encode(r.state.Handoff)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input struct {
		ID      string `json:"id"`
		Phase   string `json:"phase"`
		Message string `json:"message"`
		NotSent bool   `json:"not_sent"`
		ClaimID string `json:"claim_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, req.Body, 8192)).Decode(&input) != nil || r.state.Handoff == nil || input.ID != r.state.Handoff.ID {
		http.Error(w, "handoff is no longer current", 409)
		return
	}
	if input.Phase != "dispatching" && input.Phase != "running" && input.Phase != "resume-failed" && input.Phase != "needs-confirmation" {
		http.Error(w, "invalid handoff phase", 400)
		return
	}
	if input.Phase == "dispatching" {
		if len(input.ClaimID) < 8 || len(input.ClaimID) > 80 {
			http.Error(w, "claim id required", 400)
			return
		}
		owner, err := supervisor.ReadHostLoginEmail()
		if !r.state.Enabled || r.state.Phase != "resume-ready" || err != nil || !strings.EqualFold(owner, r.state.Handoff.To) {
			http.Error(w, "handoff destination is not ready", 409)
			return
		}
	} else if r.state.Phase != "dispatching" && !(input.Phase == "resume-failed" && r.state.Phase == "resume-ready") {
		http.Error(w, "handoff is not being dispatched", 409)
		return
	}
	if input.Phase != "dispatching" && (r.state.Handoff.ClaimID == "" || input.ClaimID != r.state.Handoff.ClaimID) {
		http.Error(w, "handoff claim belongs to another interface", 409)
		return
	}
	before := r.state
	copy := *r.state.Handoff
	r.state.Handoff = &copy
	if input.Phase == "dispatching" {
		r.state.Handoff.SendClaimed = true
		r.state.Handoff.ClaimID = input.ClaimID
		r.state.DispatchAt = time.Now().UnixMilli()
	}
	if input.Phase == "resume-failed" {
		if r.state.Handoff.SendClaimed && !input.NotSent {
			input.Phase = "needs-confirmation"
		} else {
			r.state.Handoff.SendClaimed = false
		}
	}
	r.recordLocked(input.Phase, input.Message)
	if input.Phase == "running" {
		r.state.Handoff = nil
	}
	if err := r.saveLocked(); err != nil {
		r.state = before
		http.Error(w, "saving handoff failed", 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
