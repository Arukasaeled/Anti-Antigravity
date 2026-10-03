//go:build windows

package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func readNativeAuthAt(addr string) NativeAuthState {
	state := NativeAuthState{Source: "Antigravity LanguageServer/GetAuthStatus"}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/json/list")
	if err != nil {
		return state
	}
	defer resp.Body.Close()
	var targets []cdpTarget
	if json.NewDecoder(resp.Body).Decode(&targets) != nil {
		return state
	}
	for _, target := range targets {
		if !IsRealWorkbenchTarget(target) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, reader, err := openCDPWebSocket(ctx, target.WebSocketDebuggerURL)
		if err != nil {
			cancel()
			continue
		}
		raw, err := evaluateCDPString(conn, reader, 1, `(async()=>{const c=window.Wj?.lsClient;if(!c?.getAuthStatus)return JSON.stringify({available:false});const r=(await c.getAuthStatus({})).authResult;if(!r)return JSON.stringify({available:false});let email='';if(r.hasValidAuth&&c.getUserStatus)email=(await c.getUserStatus({})).userStatus?.email||'';return JSON.stringify({available:true,valid:r.hasValidAuth===true,email,failure:r.failureDetails?.case||'',message:(r.uiMessage||'').slice(0,500)})})()`)
		conn.Close()
		cancel()
		if err == nil {
			json.Unmarshal([]byte(raw), &state)
			return state
		}
	}
	return state
}

func ReadNativeAuthState() NativeAuthState {
	if IsOfficialRuntime() {
		return readNativeAuthRPC()
	}
	state := readNativeAuthAt(ResolveCDPAddr())
	if state.Available {
		return state
	}
	return readNativeAuthRPC()
}

// Expired archives may bootstrap native refresh, but do not count as successful
// switches until the host reports valid auth and has renewed its credential.
func waitForNativeAccount(email string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	last := NativeAuthState{}
	var lastRaw []byte
	for time.Now().Before(deadline) {
		raw, err := readAntigravityCredentialRaw()
		lastRaw = raw
		if err != nil {
			return nil, err
		}
		if owner := emailFromCredentialPayload(raw); owner != "" && !strings.EqualFold(owner, email) {
			return nil, fmt.Errorf("原生宿主凭据归属改变，拒绝提交切号")
		}
		last = ReadNativeAuthState()
		if last.Available {
			if last.Failure != "" {
				return nil, fmt.Errorf("原生 Antigravity 登录被拒绝（%s）：%s", last.Failure, last.Message)
			}
			// Refresh grants need not issue a new ID token. Require the complete
			// archive, a current access token and server-confirmed identity instead.
			if nativeCredentialReady(raw, last, email) {
				return raw, nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	_, fresh := credentialUsableEmail(lastRaw)
	return nil, fmt.Errorf("原生 Antigravity 登录/刷新未确认（RPC 可读=%v，原生登录有效=%v，完整新登录=%v）；未提交切号", last.Available, last.Valid, fresh)
}
