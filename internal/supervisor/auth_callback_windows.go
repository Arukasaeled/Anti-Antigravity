//go:build windows

package supervisor

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

// Electron registers only its executable by default. Without the profile flag,
// an OAuth URI starts a separate default-profile instance of the copied host.
// Route callbacks to the exact running account profile, preserving %1 literally.
func bindEnhancedAuthCallback(exe, profile string, pid, cdpPort int) error {
	if strings.ContainsAny(exe+profile, "\"\r\n") {
		return fmt.Errorf("invalid callback path")
	}
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\antigravity\shell\open\command`, registry.QUERY_VALUE)
		if err == nil {
			command, _, _ := key.GetStringValue("")
			key.Close()
			if strings.HasPrefix(strings.ToLower(command), strings.ToLower(`"`+exe+`"`)) && probeCDPPortAlive(cdpPort) {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if GetManagedHostPID() != pid {
		return fmt.Errorf("callback owner changed")
	}
	if !ready {
		return fmt.Errorf("宿主回调注册尚未就绪，未覆盖其他注册项")
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\antigravity\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue("", `"`+exe+`" "--user-data-dir=`+profile+`" "%1"`)
}
