//go:build !windows

package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopServiceKillsEntireProcessGroup(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	pidDir := filepath.Join(workspaceRoot, "pids")
	t.Setenv("HOME", homeDir)

	servicesDir := filepath.Join(workspaceRoot, ServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	serviceID := "group-stop-test"
	scriptPath := filepath.Join(servicesDir, serviceID+".sh")
	// Use #!/bin/sh and POSIX-only constructs so the test works on macOS
	// (system bash is 3.2, which lacks $BASHPID; $PPID is POSIX-standard).
	// For the top-level script $$ is the PID.  For subshells we can't use $$
	// (it gives the parent's PID), so we spawn a child sh whose $PPID is the
	// bash subshell's PID.
	script := fmt.Sprintf(`#!/bin/sh
set -eu
pid_dir=%q

printf '%%s\n' "$$" > "$pid_dir/parent.pid"

(
  sh -c 'printf "%%s\n" "$PPID"' > "$pid_dir/child.pid"
  (
    sh -c 'printf "%%s\n" "$PPID"' > "$pid_dir/grandchild.pid"
    exec tail -f /dev/null
  ) &
  wait
) &
wait
`, pidDir)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	mgr := NewManager()
	if _, code, err := mgr.StartService(workspaceRoot, serviceID); err != nil {
		t.Fatalf("StartService() failed: code=%q err=%v", code, err)
	}

	parentPID := waitForPIDFile(t, filepath.Join(pidDir, "parent.pid"))
	childPID := waitForPIDFile(t, filepath.Join(pidDir, "child.pid"))
	grandchildPID := waitForPIDFile(t, filepath.Join(pidDir, "grandchild.pid"))

	waitForProcessesAlive(t, parentPID, childPID, grandchildPID)

	t.Cleanup(func() {
		_ = killProcessGroup(parentPID, syscall.SIGKILL)
		for _, pid := range []int{parentPID, childPID, grandchildPID} {
			if processExists(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	_, unsubscribe, closeCh := mgr.Subscribe(serviceID)
	defer unsubscribe()
	if closeCh == nil {
		t.Fatal("Subscribe() returned nil close channel")
	}

	if code, err := mgr.StopService(serviceID); err != nil {
		t.Fatalf("StopService() failed: code=%q err=%v", code, err)
	}

	select {
	case <-closeCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for service to stop")
	}

	svc, err := mgr.GetService(workspaceRoot, serviceID)
	if err != nil {
		t.Fatalf("GetService() failed: %v", err)
	}
	if svc == nil {
		t.Fatal("GetService() returned nil service")
	}
	if svc.Status != "stopped" {
		t.Fatalf("service status = %q, want stopped", svc.Status)
	}

	waitForProcessesGone(t, parentPID, childPID, grandchildPID)
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()

	var pid int
	waitForCondition(t, 5*time.Second, func() (bool, string, error) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return false, "waiting for pid file", nil
		}
		if err != nil {
			return false, "", err
		}

		parsed, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return false, "", err
		}
		if parsed <= 0 {
			return false, fmt.Sprintf("pid %d is not positive", parsed), nil
		}

		pid = parsed
		return true, "", nil
	})

	return pid
}

func waitForProcessesAlive(t *testing.T, pids ...int) {
	t.Helper()

	waitForCondition(t, 5*time.Second, func() (bool, string, error) {
		var dead []string
		for _, pid := range pids {
			if !processExists(pid) {
				dead = append(dead, strconv.Itoa(pid))
			}
		}
		if len(dead) > 0 {
			return false, fmt.Sprintf("processes not alive yet: %s", strings.Join(dead, ", ")), nil
		}
		return true, "", nil
	})
}

func waitForProcessesGone(t *testing.T, pids ...int) {
	t.Helper()

	waitForCondition(t, 5*time.Second, func() (bool, string, error) {
		var alive []string
		for _, pid := range pids {
			if processExists(pid) {
				alive = append(alive, strconv.Itoa(pid))
			}
		}
		if len(alive) > 0 {
			return false, fmt.Sprintf("processes still alive: %s", strings.Join(alive, ", ")), nil
		}
		return true, "", nil
	})
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() (bool, string, error)) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastReason string
	for time.Now().Before(deadline) {
		ok, reason, err := check()
		if err != nil {
			t.Fatalf("condition check failed: %v", err)
		}
		if ok {
			return
		}
		if reason != "" {
			lastReason = reason
		}
		time.Sleep(25 * time.Millisecond)
	}

	if lastReason == "" {
		lastReason = "condition was not satisfied"
	}
	t.Fatal(lastReason)
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
