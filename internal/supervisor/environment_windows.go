//go:build windows

package supervisor

func detectEnvironmentVersion(executable string) string { return fileProductVersion(executable) }
