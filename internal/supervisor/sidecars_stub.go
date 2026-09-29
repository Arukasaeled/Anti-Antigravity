//go:build !windows

package supervisor

import (
	"context"
	"errors"

	"github.com/2ag/2ag/internal/config"
)

type SidecarStatus struct{}
type SidecarManager struct{}

func NewSidecarManager(context.Context, *ManagedProcess, []config.Plugin, string, int) (*SidecarManager, error) {
	return nil, errors.New("sidecar supervisor is supported on Windows only")
}
func (m *SidecarManager) Address() string { return "" }
func (m *SidecarManager) Start(string) error {
	return errors.New("sidecar supervisor is supported on Windows only")
}
func (m *SidecarManager) Stop(string) error {
	return errors.New("sidecar supervisor is supported on Windows only")
}
func (m *SidecarManager) StopAll()                {}
func (m *SidecarManager) Status() []SidecarStatus { return nil }
