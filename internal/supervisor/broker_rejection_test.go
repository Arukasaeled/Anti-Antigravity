package supervisor

import "testing"

func TestNativeRejectionRequiresTargetAndTerminalFailure(t *testing.T) {
	state := NativeAuthState{Available: true, Email: "target@example.com", Failure: "generalError", Message: "network EOF"}
	if terminalNativeAccountRejection(state, state.Email) {
		t.Fatal("temporary network failure must retain the native verification window")
	}
	state.Failure = "ineligible"
	if !terminalNativeAccountRejection(state, state.Email) || terminalNativeAccountRejection(state, "previous@example.com") {
		t.Fatal("terminal rejection must match the actual target identity")
	}
	state.Email = ""
	if terminalNativeAccountRejection(state, "target@example.com") {
		t.Fatal("unattributed cached status must not reject the target")
	}
}

func TestBrokerReportsNativeEligibilityWithoutWaitingForToken(t *testing.T) {
	s := NativeAuthState{Available: true, Failure: "ineligible", Email: "target@example.com"}
	if !terminalBrokerRejection(s, "target@example.com") {
		t.Fatal("native rejection must not become a ten-minute placeholder timeout")
	}
	if terminalBrokerRejection(s, "other@example.com") {
		t.Fatal("another account's cached failure must not reject the requested login")
	}
	s.Email = ""
	if terminalBrokerRejection(s, "") {
		t.Fatal("unknown identity must not become a terminal account diagnosis")
	}
	s.Email = "target@example.com"
	s.Failure = "generalError"
	if terminalBrokerRejection(s, "target@example.com") {
		t.Fatal("transient network errors must remain retryable")
	}
}
