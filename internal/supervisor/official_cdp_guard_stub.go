//go:build !windows

package supervisor

func isOfficialCDPTarget(addr string) bool { return false }
