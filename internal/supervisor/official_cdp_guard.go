//go:build windows

package supervisor

import (
	"net"
	"strconv"
)

func isOfficialCDPTarget(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	officialPort, _, err := officialDevToolsActivePort()
	return err == nil && port == strconv.Itoa(officialPort) && ProbeOfficialHostPresence().Running
}
