package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/internal/supervisor"
)

// 这组端到端 API 测试刻意**不依赖任何具体账号**。
//
// 它验的是「路由通不通、回执形状对不对」，与「本机装了哪个账号」无关：
//   - 会话列表 / 导出 / host status / dashboard / takeover 方法限制：都是只读 + 形状断言；
//   - 设为主控：需要本地真的存在至少一个账号才能验「存在性闸门」放行，
//     所以这里先从 ScanLocalAccounts() 里取一个**真实存在的**邮箱，
//     取不到就跳过那一段（而不是拿一个写死的邮箱去撞 404）。
//
// 为什么必须去掉写死的邮箱：那条 404 只在原作者机器上不会出现 ——
// 别的用户跑这条测试时，它必然失败，而失败原因看起来像「代码坏了」。
// 测试的前提条件必须由测试自己发现，不能预设成某台机器的状态。
func TestAPISessionsAndHost(t *testing.T) {
	bus := core.NewEventBus()
	cfg := config.Config{}
	sm := core.NewStateMachine(cfg, "2ag.json", bus)
	s := NewServer(sm, bus)

	// 1. GET /api/v1/sessions
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/sessions, got %d", w.Code)
	}
	var sessionsRes supervisor.SessionsResult
	if err := json.NewDecoder(w.Body).Decode(&sessionsRes); err != nil {
		t.Fatalf("failed to decode sessions response: %v", err)
	}
	t.Logf("API returned %d sessions", sessionsRes.Total)

	// 2. GET /api/v1/sessions/export?id=...（有会话才验；内容不做断言，
	//    它来自用户自己的数据，测试不该假设里面有特定文本）
	if sessionsRes.Total > 0 {
		first := sessionsRes.Sessions[0]
		reqExport := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/export?id="+first.ID, nil)
		wExport := httptest.NewRecorder()
		s.mux.ServeHTTP(wExport, reqExport)
		if wExport.Code != http.StatusOK {
			t.Errorf("expected 200 OK for sessions/export, got %d", wExport.Code)
		} else if wExport.Body.Len() == 0 {
			t.Error("export 返回 200 但内容为空")
		} else {
			t.Logf("Session export ok, content length: %d", wExport.Body.Len())
		}
	}

	// 3. GET /api/v1/host/status
	reqHost := httptest.NewRequest(http.MethodGet, "/api/v1/host/status", nil)
	wHost := httptest.NewRecorder()
	s.mux.ServeHTTP(wHost, reqHost)
	if wHost.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/host/status, got %d", wHost.Code)
	}

	// 4. GET /api/v1/dashboard
	reqDash := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	wDash := httptest.NewRecorder()
	s.mux.ServeHTTP(wDash, reqDash)
	if wDash.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/dashboard, got %d", wDash.Code)
	}
	var dashRes struct {
		ActiveAccount supervisor.AccountInstance `json:"active_account"`
	}
	if err := json.NewDecoder(wDash.Body).Decode(&dashRes); err != nil {
		t.Fatalf("failed to decode dashboard response: %v", err)
	}
	t.Logf("Dashboard active account: %q", dashRes.ActiveAccount.Email)

	// 5. 设为主控：存在性闸门的正例与反例。
	//
	// 反例（幽灵邮箱）必须 404 —— 这一条不依赖本机状态，永远可验。
	ghostBody := `{"email":"definitely-not-a-real-account@example.invalid"}`
	reqGhost := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/primary", strings.NewReader(ghostBody))
	wGhost := httptest.NewRecorder()
	s.mux.ServeHTTP(wGhost, reqGhost)
	if wGhost.Code != http.StatusNotFound {
		t.Errorf("不存在的账号必须被拒（404），实际 %d", wGhost.Code)
	}
	// 空邮箱必须 400
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/primary", strings.NewReader(`{"email":""}`))
	wEmpty := httptest.NewRecorder()
	s.mux.ServeHTTP(wEmpty, reqEmpty)
	if wEmpty.Code != http.StatusBadRequest {
		t.Errorf("空邮箱必须被拒（400），实际 %d", wEmpty.Code)
	}

	// 正例：从本机真实账号里挑一个。
	accounts := supervisor.ScanLocalAccounts()
	if len(accounts) == 0 {
		t.Log("本机没有任何本地账号，跳过「设为主控」正例")
	} else {
		target := accounts[0].Email
		// 记下当前主控，测完还原 —— 这条测试会写 ~/.2ag/active_account.txt，
		// 不还原就等于把用户的选中项改掉了。
		before := supervisor.GetActiveAccountEmail()
		reqSwitch := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/primary",
			strings.NewReader(`{"email":`+jsonString(target)+`}`))
		wSwitch := httptest.NewRecorder()
		s.mux.ServeHTTP(wSwitch, reqSwitch)
		if wSwitch.Code != http.StatusOK {
			t.Errorf("已存在的账号应被接受（200），实际 %d", wSwitch.Code)
		} else {
			reqActive := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/active", nil)
			wActive := httptest.NewRecorder()
			s.mux.ServeHTTP(wActive, reqActive)
			if wActive.Code != http.StatusOK {
				t.Errorf("expected 200 OK for /api/v1/accounts/active, got %d", wActive.Code)
			} else {
				var activeRes struct {
					Email string `json:"email"`
				}
				if err := json.NewDecoder(wActive.Body).Decode(&activeRes); err != nil {
					t.Fatalf("failed to decode active account: %v", err)
				}
				if !strings.EqualFold(activeRes.Email, target) {
					t.Errorf("切换后主控应为 %q，实际 %q", target, activeRes.Email)
				}
			}
			// 还原主控标记
			if before != "" && !strings.EqualFold(before, target) {
				supervisor.SetActiveAccount(before)
			}
		}
	}

	// 6. /api/v1/host/takeover 方法限制
	reqTakeoverGet := httptest.NewRequest(http.MethodGet, "/api/v1/host/takeover", nil)
	wTakeoverGet := httptest.NewRecorder()
	s.mux.ServeHTTP(wTakeoverGet, reqTakeoverGet)
	if wTakeoverGet.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed for GET /api/v1/host/takeover, got %d", wTakeoverGet.Code)
	}
}

// jsonString 把一个字符串安全地放进 JSON 字面量里（邮箱可能含引号，虽然实际不会）。
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
