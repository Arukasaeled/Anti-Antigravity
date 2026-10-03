//go:build !windows

package supervisor

import "fmt"

func readNativeUsageRows(path, query string, visit func(int64, []byte) error) error {
	return fmt.Errorf("native conversation SQLite reader is available on Windows")
}
