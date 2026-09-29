//go:build windows

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/2ag/2ag/internal/config"
)

const (
	JobObjectExtendedLimitInformationClass = 9
	jobObjectLimitKillOnJobClose           = 0x00002000
	processSetQuota                        = 0x0100
	processTerminate                       = 0x0001
	processQueryLimitedInformation         = 0x1000
)

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation  jobObjectBasicLimitInformation
	IoInfo                 ioCounters
	ProcessMemoryLimit     uintptr
	JobMemoryLimit         uintptr
	PeakProcessMemoryLimit uintptr
	PeakJobMemoryLimit     uintptr
}

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
)

type HostOptions struct {
	Executable string
	Args       []string
	Dir        string
	Env        map[string]string
}

type ManagedProcess struct {
	cmd       *exec.Cmd
	job       syscall.Handle
	proc      syscall.Handle
	sidecars  []*exec.Cmd
	sidecarMu sync.Mutex
	closeOnce sync.Once
}

// PID returns the operating-system process identifier of the managed host.
// It is exposed to the in-app diagnostics panel without exposing the command
// or Job Object handles themselves.
func (p *ManagedProcess) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func Start(ctx context.Context, opts HostOptions) (*ManagedProcess, error) {
	if opts.Executable == "" {
		return nil, errors.New("host executable is empty")
	}
	args := append([]string(nil), opts.Args...)
	job, err := createKillOnCloseJob()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, opts.Executable, args...)
	cmd.Dir = opts.Dir
	cmd.Env = mergedEnvironment(opts.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		closeHandle(job)
		return nil, fmt.Errorf("start Antigravity: %w", err)
	}
	proc, err := openProcess(syscall.Handle(cmd.Process.Pid))
	if err != nil {
		closeHandle(job)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("open host process: %w", err)
	}
	if err := assignProcess(job, proc); err != nil {
		closeHandle(proc)
		closeHandle(job)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("assign host to job: %w", err)
	}
	return &ManagedProcess{cmd: cmd, job: job, proc: proc}, nil
}

func mergedEnvironment(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return nil
	}
	environment := os.Environ()
	for name, value := range overrides {
		prefix := name + "="
		replaced := false
		for index, entry := range environment {
			if strings.HasPrefix(entry, prefix) {
				environment[index] = prefix + value
				replaced = true
				break
			}
		}
		if !replaced {
			environment = append(environment, prefix+value)
		}
	}
	return environment
}

func Launch(ctx context.Context, opts HostOptions) (*ManagedProcess, error) {
	return Start(ctx, opts)
}

func (p *ManagedProcess) Wait() error {
	err := p.cmd.Wait()
	p.Close()
	return err
}

func (p *ManagedProcess) Stop() error {
	p.Close()
	if p.cmd.ProcessState == nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}

func (p *ManagedProcess) Close() {
	p.closeOnce.Do(func() {
		closeHandle(p.proc)
		closeHandle(p.job)
	})
}

func StartConfiguredSidecars(ctx context.Context, p *ManagedProcess, plugins []config.Plugin, baseDir string) error {
	if p == nil {
		return errors.New("managed process is nil")
	}
	for _, plugin := range plugins {
		if !plugin.Enabled {
			continue
		}
		executable := plugin.Executable
		if !filepath.IsAbs(executable) {
			executable = filepath.Join(baseDir, executable)
		}
		cmd := exec.CommandContext(ctx, executable, plugin.Args...)
		cmd.Dir = baseDir
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
		if err := cmd.Start(); err != nil {
			log.Printf("[2ag] sidecar %q failed to start: %v", plugin.Name, err)
			continue
		}
		handle, err := openProcess(syscall.Handle(cmd.Process.Pid))
		if err != nil {
			log.Printf("[2ag] sidecar %q process handle failed: %v", plugin.Name, err)
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			continue
		}
		if err := assignProcess(p.job, handle); err != nil {
			closeHandle(handle)
			log.Printf("[2ag] sidecar %q was not assigned to Job Object: %v", plugin.Name, err)
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			continue
		}
		closeHandle(handle)
		p.sidecarMu.Lock()
		p.sidecars = append(p.sidecars, cmd)
		p.sidecarMu.Unlock()
	}
	return nil
}

func Run(ctx context.Context, opts HostOptions) error {
	managed, err := Start(ctx, opts)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- managed.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = managed.Stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return ctx.Err()
	}
}

func createKillOnCloseJob() (syscall.Handle, error) {
	r1, _, callErr := procCreateJobObjectW.Call(0, 0)
	if r1 == 0 {
		return 0, winError(callErr, "CreateJobObjectW")
	}
	job := syscall.Handle(r1)
	info := jobObjectExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r1, _, callErr = procSetInformationJobObject.Call(uintptr(job), JobObjectExtendedLimitInformationClass, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r1 == 0 {
		closeHandle(job)
		return 0, winError(callErr, "SetInformationJobObject")
	}
	return job, nil
}

func openProcess(pid syscall.Handle) (syscall.Handle, error) {
	r1, _, callErr := procOpenProcess.Call(processSetQuota|processTerminate|processQueryLimitedInformation, 0, uintptr(pid))
	if r1 == 0 {
		return 0, winError(callErr, "OpenProcess")
	}
	return syscall.Handle(r1), nil
}

func assignProcess(job, process syscall.Handle) error {
	r1, _, callErr := procAssignProcessToJobObject.Call(uintptr(job), uintptr(process))
	if r1 == 0 {
		return winError(callErr, "AssignProcessToJobObject")
	}
	return nil
}

func closeHandle(handle syscall.Handle) {
	if handle != 0 {
		_, _, _ = procCloseHandle.Call(uintptr(handle))
	}
}

func winError(callErr error, operation string) error {
	if callErr == nil || callErr == syscall.Errno(0) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, callErr)
}
