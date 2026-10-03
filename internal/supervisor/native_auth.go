package supervisor

// Only public login status is exposed, never OAuth material.
type NativeAuthState struct {
	Available bool   `json:"available"`
	Valid     bool   `json:"valid"`
	Source    string `json:"source"`
	Failure   string `json:"failure,omitempty"`
	Message   string `json:"message,omitempty"`
	Email     string `json:"email,omitempty"`
}
