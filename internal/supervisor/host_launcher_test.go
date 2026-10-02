package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectDefaultAntigravityPathPriorities(t *testing.T) {
	// Create a temporary mock directory structure
	tmpDir, err := os.MkdirTemp("", "2ag-launcher-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mockAppDir := filepath.Join(tmpDir, "app")
	if err := os.MkdirAll(mockAppDir, 0755); err != nil {
		t.Fatalf("failed to create mock app dir: %v", err)
	}

	mockExe := filepath.Join(mockAppDir, "Antigravity.exe")
	if err := os.WriteFile(mockExe, []byte("mock binary"), 0755); err != nil {
		t.Fatalf("failed to create mock exe: %v", err)
	}

	// Test with current directory set to tmpDir
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(tmpDir)

	path := detectDefaultAntigravityPath()
	if path == "" {
		t.Fatalf("expected to detect mock Antigravity.exe in cwd/app, got empty")
	}
	t.Logf("Detected Antigravity path: %s", path)
	if filepath.Base(path) != "Antigravity.exe" {
		t.Errorf("expected executable name Antigravity.exe, got %s", filepath.Base(path))
	}
}

func TestIDEProtectionGuard(t *testing.T) {
	myPID := os.Getpid()
	if !IsProtectedIDEProcess(myPID) {
		t.Errorf("expected current process PID %d to be protected, but got false", myPID)
	}

	err := killPIDSafely(myPID)
	if err == nil {
		t.Errorf("expected killPIDSafely on current process to fail with protection error, got nil")
	}

	// 验证探针不会将受保护的 IDE 误认为 2Ag 托管宿主
	status := ProbeRealHost()
	if status.PID > 0 && IsProtectedIDEProcess(status.PID) {
		t.Errorf("ProbeRealHost returned protected PID %d, which should have been excluded", status.PID)
	}
}
