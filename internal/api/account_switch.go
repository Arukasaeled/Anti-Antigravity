package api

import (
	"encoding/json"
	"fmt"
	"github.com/2ag/2ag/internal/patcher"
	"github.com/2ag/2ag/internal/supervisor"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type switchJob struct {
	Running          bool                           `json:"running"`
	Email            string                         `json:"email"`
	Success          bool                           `json:"success"`
	Message          string                         `json:"message"`
	FinishedAt       string                         `json:"finished_at,omitempty"`
	Result           supervisor.AccountSwitchResult `json:"result"`
	Transaction      supervisor.AccountTransaction  `json:"transaction"`
	UserMessage      string                         `json:"user_message,omitempty"`
	UserMessageEN    string                         `json:"user_message_en,omitempty"`
	TechnicalDetails string                         `json:"technical_details,omitempty"`
}

var accountSwitchJob struct {
	sync.Mutex
	state switchJob
}

func (s *Server) startAccountSwitch(w http.ResponseWriter, email string) {
	accountSwitchJob.Lock()
	if accountSwitchJob.state.Running {
		accountSwitchJob.Unlock()
		http.Error(w, "已有切号正在进行", 409)
		return
	}
	accountSwitchJob.state = switchJob{Running: true, Email: email, Message: "正在保存登录并重启原形态宿主；接下来验证原生刷新"}
	state := accountSwitchJob.state
	accountSwitchJob.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(state)
	mode := s.currentRuntimeMode()
	go func() {
		result, err := supervisor.SwitchAccountTransactional(email, mode)
		message := result.Message
		if err != nil {
			message = err.Error()
		}
		userMessage, userMessageEN := switchUserMessage(result, err)
		state := switchJob{Email: email, Success: err == nil, Message: message, FinishedAt: time.Now().Format(time.RFC3339), Result: result, Transaction: supervisor.CurrentAccountTransaction(), UserMessage: userMessage, UserMessageEN: userMessageEN}
		if err != nil {
			state.TechnicalDetails = message
		}
		if err := persistSwitchReceipt(state); err != nil {
			log.Printf("[2ag] 保存切号回执失败: %v", err)
			state.Message += "；回执保存失败，重启后无法回看本次结果"
		}
		accountSwitchJob.Lock()
		accountSwitchJob.state = state
		accountSwitchJob.Unlock()
	}()
}

func persistSwitchReceipt(state switchJob) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".2ag", "workspace")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".account-switch-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, "account-switch.json")); err != nil {
		return fmt.Errorf("切号元数据保存失败: %w", err)
	}
	return nil
}

func (s *Server) handleAccountSwitchStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	accountSwitchJob.Lock()
	state := accountSwitchJob.state
	accountSwitchJob.Unlock()
	if state.Email == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if raw, err := os.ReadFile(filepath.Join(home, ".2ag", "workspace", "account-switch.json")); err == nil {
				json.Unmarshal(raw, &state)
			}
		}
	}
	if state.Running {
		state.Transaction = supervisor.CurrentAccountTransaction()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(state)
}

func switchUserMessage(result supervisor.AccountSwitchResult, err error) (string, string) {
	if err == nil {
		return "原生登录已核验", "Native sign-in verified"
	}
	zh, en := "无法切换账号。请查看账号健康状态。", "Couldn't switch accounts. Check Account Health."
	if result.RolledBack {
		if result.RollbackVerified {
			zh += " 原账号已恢复并核验。"
			en += " Your previous account was restored and verified."
		} else {
			zh += " 恢复尚未确认，请重启 Manager 继续恢复。"
			en += " Recovery isn't verified. Restart Manager to continue recovery."
		}
	}
	if strings.Contains(err.Error(), "ineligible") {
		reasonZH, reasonEN := "Antigravity 原生服务拒绝了此账号的使用资格。", "Antigravity rejected this account's eligibility."
		if strings.Contains(strings.ToLower(err.Error()), "location") {
			reasonZH = "Antigravity 原生服务提示：此账号当前地区暂不支持使用。"
			reasonEN = "Antigravity reports that this account isn't supported in its current location."
		}
		zh = reasonZH + strings.TrimPrefix(zh, "无法切换账号。请查看账号健康状态。")
		en = reasonEN + strings.TrimPrefix(en, "Couldn't switch accounts. Check Account Health.")
	}
	return zh, en
}

func (s *Server) handleAccountHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	health := supervisor.AccountHealthSnapshot(r.URL.Query().Get("email"))
	accountSwitchJob.Lock()
	receipt := accountSwitchJob.state
	accountSwitchJob.Unlock()
	if receipt.Email == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if raw, err := os.ReadFile(filepath.Join(home, ".2ag", "workspace", "account-switch.json")); err == nil {
				_ = json.Unmarshal(raw, &receipt)
			}
		}
	}
	health["last_switch"] = receipt
	json.NewEncoder(w).Encode(health)
}

func (s *Server) handleAccountHealthUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	fmt.Fprint(w, "window.TwoAgAccountHealth = "+patcher.AccountHealthSource+";")
}

func (s *Server) handleTraceOpenFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		http.Error(w, "invalid file path", 400)
		return
	}
	err := supervisor.OpenTraceFile(body.Path)
	w.Header().Set("Content-Type", "application/json")
	message := ""
	if err != nil {
		w.WriteHeader(409)
		message = err.Error()
	}
	json.NewEncoder(w).Encode(map[string]any{"success": err == nil, "message": message})
}
