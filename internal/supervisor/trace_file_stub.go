//go:build !windows

package supervisor

import "fmt"

func OpenTraceFile(string) error {
	return fmt.Errorf("Opening files requires the Windows enhanced host")
}
