package api

import (
	"errors"
	"github.com/2ag/2ag/internal/supervisor"
	"strings"
	"testing"
)

func TestSwitchMessageSeparatesFailureAndRecovery(t *testing.T) {
	r := supervisor.AccountSwitchResult{RolledBack: true, RollbackVerified: true}
	zh, en := switchUserMessage(r, errors.New("GetAuthStatus ineligible"))
	if !strings.Contains(zh, "原账号已恢复") || !strings.Contains(en, "restored and verified") {
		t.Fatal("verified rollback must be explicit")
	}
	r.RollbackVerified = false
	_, en = switchUserMessage(r, errors.New("native timeout"))
	if strings.Contains(en, "restored and verified") || !strings.Contains(en, "Restart Manager") {
		t.Fatal("unverified rollback cannot claim success")
	}
}
