package supervisor

import (
	"testing"
)

func TestCleanSessionTitle(t *testing.T) {
	raw := "<USER_REQUEST>\n# 开发 2ag Windows 增强套件 MVP\n\n> 任务目标...\n</USER_REQUEST>"
	title := CleanSessionTitle(raw)
	if title != "开发 2ag Windows 增强套件 MVP" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestScanLocalSessions(t *testing.T) {
	result := ScanLocalSessions()
	t.Logf("Total sessions scanned: %d", result.Total)
	for i, s := range result.Sessions {
		t.Logf("[%d] ID: %s | Title: %s | Updated: %s | Turns: %d", i+1, s.ID, s.Title, s.UpdatedAt, s.Turns)
	}
	if result.Total == 0 {
		t.Log("Note: No sessions found, but scanner executed successfully")
	}
}
