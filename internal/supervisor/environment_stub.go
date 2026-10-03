//go:build !windows

package supervisor

func detectEnvironmentVersion(string) string { return "" }
