//go:build !windows

package supervisor

func AccountHealthSnapshot(email string) map[string]any {
	return map[string]any{"account": email, "native": NativeAuthState{}, "transaction": CurrentAccountTransaction()}
}
