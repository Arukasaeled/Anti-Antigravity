package supervisor

import "strings"

// Only public login status is exposed, never OAuth material.
type NativeAuthState struct {
	Available     bool   `json:"available"`
	Valid         bool   `json:"valid"`
	Authenticated bool   `json:"authenticated"`
	Source        string `json:"source"`
	Failure       string `json:"failure,omitempty"`
	Message       string `json:"message,omitempty"`
	Email         string `json:"email,omitempty"`
}

// Eligibility and session authentication are different facts. In 2.19.1 a
// consumer can execute Agent work while GetAuthStatus reports ineligible.
// Keep that result intact; accepting the identity requires HasAuthToken and
// GetUserStatus from the same managed LanguageServer, plus credential checks.
func nativeSessionReady(state NativeAuthState) bool {
	return state.Available && state.Email != "" && (state.Valid || (state.Authenticated && state.Failure == "ineligible"))
}

func terminalBrokerRejection(state NativeAuthState, expected string) bool {
	return state.Available && !nativeSessionReady(state) && state.Failure == "ineligible" && state.Email != "" && (expected == "" || strings.EqualFold(state.Email, expected))
}

// A cached status from the previous account or a transient network failure is
// not a definitive rejection of the target being bootstrapped.
func terminalNativeAccountRejection(state NativeAuthState, expected string) bool {
	if !state.Available || nativeSessionReady(state) || state.Email == "" || !strings.EqualFold(state.Email, expected) {
		return false
	}
	switch state.Failure {
	case "ineligible", "verificationRequired", "tosViolation":
		return true
	}
	return false
}
