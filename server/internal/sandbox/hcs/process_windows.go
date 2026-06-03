//go:build windows

package hcs

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type launcherProcess struct {
	cmd  *exec.Cmd
	job  windows.Handle
	done chan error
	mu   sync.Mutex
}

func startLauncherProcess(cmd *exec.Cmd) (*launcherProcess, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}

	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(process)

	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = cmd.Process.Kill()
		_ = windows.CloseHandle(job)
		return nil, err
	}

	proc := &launcherProcess{cmd: cmd, job: job, done: make(chan error, 1)}
	go func() {
		proc.done <- cmd.Wait()
	}()
	return proc, nil
}

func (p *launcherProcess) stop(timeout time.Duration) error {
	p.mu.Lock()
	job := p.job
	cmd := p.cmd
	p.mu.Unlock()

	select {
	case err := <-p.done:
		_ = p.closeJob()
		return err
	default:
	}

	if cmd != nil && cmd.Process != nil {
		// The launcher registers Ctrl+C handling and SIGTERM. Sending Ctrl+Break
		// to its process group gives it a chance to run its normal VM cleanup path.
		_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-p.done:
		_ = p.closeJob()
		return err
	case <-timer.C:
	}

	if job != 0 {
		if err := windows.TerminateJobObject(job, 1); err != nil {
			return fmt.Errorf("failed to terminate HCS launcher job: %w", err)
		}
	} else if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill HCS launcher: %w", err)
		}
	}

	select {
	case err := <-p.done:
		_ = p.closeJob()
		return err
	case <-time.After(5 * time.Second):
		_ = p.closeJob()
		return fmt.Errorf("timed out waiting for HCS launcher to exit after hard termination")
	}
}

func (p *launcherProcess) closeJob() error {
	p.mu.Lock()
	job := p.job
	p.job = 0
	p.mu.Unlock()
	if job == 0 {
		return nil
	}
	return windows.CloseHandle(job)
}
