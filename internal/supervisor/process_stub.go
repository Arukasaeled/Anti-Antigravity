//go:build !windows

package supervisor

import (
	"context"
	"errors"

	"github.com/2ag/2ag/internal/config"
)

type HostOptions struct {
	Executable string
	Args       []string
	Dir        string
	Env        map[string]string
}

type ManagedProcess struct{}

func (p *ManagedProcess) PID() int { return 0 }

func Start(context.Context, HostOptions) (*ManagedProcess, error) {
	return nil, errors.New("Antigravity supervisor is supported on Windows only")
}

func Launch(ctx context.Context, opts HostOptions) (*ManagedProcess, error) {
	return Start(ctx, opts)
}

func (p *ManagedProcess) Wait() error {
	return errors.New("Antigravity supervisor is supported on Windows only")
}
func (p *ManagedProcess) Stop() error { return nil }
func (p *ManagedProcess) Close()      {}
func StartConfiguredSidecars(context.Context, *ManagedProcess, []config.Plugin, string) error {
	return errors.New("Antigravity supervisor is supported on Windows only")
}
func Run(context.Context, HostOptions) error {
	return errors.New("Antigravity supervisor is supported on Windows only")
}
