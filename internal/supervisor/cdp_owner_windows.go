//go:build windows

package supervisor

import (
	"encoding/binary"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var procGetExtendedTCPTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")
var cdpArgument = regexp.MustCompile(`(?:^|\s)"?--remote-debugging-port(?:=|\s+)([0-9]+)`)

// TCP_TABLE_OWNER_PID_LISTENER, AF_INET. Read only socket ownership; no
// command line, OAuth callback, CSRF nonce or credential leaves this module.
func listeningPortOwners() map[int]int {
	var size uint32
	procGetExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, 2, 3, 0)
	owners := map[int]int{}
	if size < 4 || size > 32<<20 {
		return owners
	}
	buf := make([]byte, size)
	status, _, _ := procGetExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, 2, 3, 0)
	if status != 0 || size < 4 || int(size) > len(buf) {
		return owners
	}
	count := int(binary.LittleEndian.Uint32(buf[:4]))
	if count > (int(size)-4)/24 {
		return owners
	}
	for i := 0; i < count; i++ {
		row := buf[4+i*24 : 4+(i+1)*24]
		address := binary.LittleEndian.Uint32(row[4:8])
		if address != 0 && address != 0x0100007f { // 0.0.0.0 / 127.0.0.1
			continue
		}
		port := int(binary.BigEndian.Uint16(row[8:10]))
		owners[port] = int(binary.LittleEndian.Uint32(row[20:24]))
	}
	return owners
}

func managedCDPPort(root int, files []string) int {
	if !strings.EqualFold(filepath.Base(getProcessExePath(root)), "Antigravity.exe") {
		return 0
	}
	advertised := 0
	if match := cdpArgument.FindStringSubmatch(nativeProcessCommandLine(root)); len(match) == 2 {
		advertised, _ = strconv.Atoi(match[1])
	}
	var candidates []int
	for _, file := range files {
		candidates = append(candidates, ReadCDPPortFromFile(file))
	}
	owners := listeningPortOwners()
	return resolveCDPPortForHost(root, advertised, candidates, func(port int) int { return owners[port] })
}
