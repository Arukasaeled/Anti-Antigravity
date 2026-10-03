package api

import (
	"encoding/json"
	"fmt"
	"github.com/2ag/2ag/internal/supervisor"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type switchJob struct {
	Running    bool                           `json:"running"`
	Email      string                         `json:"email"`
	Success    bool                           `json:"success"`
	Message    string                         `json:"message"`
	FinishedAt string                         `json:"finished_at,omitempty"`
	Result     supervisor.AccountSwitchResult `json:"result"`
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
		state := switchJob{Email: email, Success: err == nil, Message: message, FinishedAt: time.Now().Format(time.RFC3339), Result: result}
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(state)
}
