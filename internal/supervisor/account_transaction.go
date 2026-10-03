package supervisor

import (
	"sync"
	"time"
)

// A shared observation of the real transaction, including legacy/relay callers.
// This is not a second switch engine. Credential locking remains authoritative.
type AccountTransaction struct {
	Stage     string             `json:"stage"`
	Target    string             `json:"target,omitempty"`
	StartedAt string             `json:"started_at,omitempty"`
	UpdatedAt string             `json:"updated_at,omitempty"`
	History   []TransactionStage `json:"history,omitempty"`
}
type TransactionStage struct {
	Stage string `json:"stage"`
	At    string `json:"at"`
}

var accountTransaction struct {
	sync.RWMutex
	state AccountTransaction
}

func transactionStage(stage, target string) {
	accountTransaction.Lock()
	defer accountTransaction.Unlock()
	now := time.Now().Format(time.RFC3339Nano)
	if stage == "BackingUp" {
		accountTransaction.state = AccountTransaction{Target: target, StartedAt: now}
	}
	accountTransaction.state.Stage = stage
	accountTransaction.state.UpdatedAt = now
	accountTransaction.state.History = append(accountTransaction.state.History, TransactionStage{stage, now})
	if len(accountTransaction.state.History) > 24 {
		accountTransaction.state.History = accountTransaction.state.History[len(accountTransaction.state.History)-24:]
	}
}
func CurrentAccountTransaction() AccountTransaction {
	accountTransaction.RLock()
	defer accountTransaction.RUnlock()
	s := accountTransaction.state
	s.History = append([]TransactionStage(nil), s.History...)
	if s.Stage == "" {
		s.Stage = "Idle"
	}
	return s
}
