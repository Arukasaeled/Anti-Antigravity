package supervisor

import "strings"

// Only public login status is exposed, never OAuth material.
type NativeAuthState struct {
	Available bool   `json:"available"`
	Valid     bool   `json:"valid"`
	Source    string `json:"source"`
	Failure   string `json:"failure,omitempty"`
	Message   string `json:"message,omitempty"`
	Email     string `json:"email,omitempty"`
}

func terminalBrokerRejection(state NativeAuthState, expected string) bool {
	return state.Available && !state.Valid && state.Failure == "ineligible" && state.Email != "" && (expected == "" || strings.EqualFold(state.Email, expected))
}

// A cached status from the previous account or a transient network failure is
// not a definitive rejection of the target being bootstrapped.
func terminalNativeAccountRejection(state NativeAuthState, expected string) bool {
	if !state.Available || state.Valid || state.Email == "" || !strings.EqualFold(state.Email, expected) {
		return false
	}
	switch state.Failure {
	case "ineligible", "verificationRequired", "tosViolation":
		return true
	}
	return false
}
