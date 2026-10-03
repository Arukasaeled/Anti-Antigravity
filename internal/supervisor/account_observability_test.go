//go:build windows

package supervisor

import "testing"

func TestRootSelectionAcrossManagerRestart(t *testing.T) {
	if got := chooseHostRoot(0, 0, 100, false); got != 100 {
		t.Fatal("unregistered frozen root must be found")
	}
	if got := chooseHostRoot(100, 100, 25, true); got != 100 {
		t.Fatal("smaller Electron child cannot replace managed root")
	}
	if got := chooseHostRoot(0, 100, 25, true); got != 0 {
		t.Fatal("child cannot bootstrap root discovery")
	}
	if got := chooseHostRoot(50, 100, 100, false); got != 100 {
		t.Fatal("managed root must take priority")
	}
}

func TestHealthRejectsCrossAccountNativeState(t *testing.T) {
	state := healthNativeForOwner(NativeAuthState{Available: true, Valid: true, Email: "a@example.invalid"}, "b@example.invalid")
	if state.Valid || state.Failure != "identity-mismatch" {
		t.Fatal("native state cannot be attributed to another account")
	}
	state = healthNativeForOwner(NativeAuthState{Available: true, Valid: true, Email: "A@example.invalid"}, "a@example.invalid")
	if !state.Valid {
		t.Fatal("email case should not invalidate matching identity")
	}
}
