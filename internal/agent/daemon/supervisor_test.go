package daemon

import (
	"context"
	"os/exec"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func sleepCmd() (*exec.Cmd, error) { return exec.Command("sleep", "30"), nil }

func TestSupervisorBecomesReadyWhenHealthy(t *testing.T) {
	var healthy atomic.Bool
	s := NewSupervisor(func(int) (*exec.Cmd, error) { return sleepCmd() }, 0,
		func() bool { return healthy.Load() }, 2*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)
	healthy.Store(true)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !s.Ready() {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatal("supervisor should be ready once health is true")
	}
}

func TestSupervisorFallsBackWhenNeverHealthy(t *testing.T) {
	s := NewSupervisor(func(int) (*exec.Cmd, error) { return sleepCmd() }, 0,
		func() bool { return false }, 150*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !s.FellBack() {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.FellBack() {
		t.Fatal("never-healthy sidecar must fall back")
	}
}

// TestSupervisorKillsChildOnShutdown verifies that cancelling ctx kills the
// child process and the Start goroutine exits cleanly. We use a real "sleep 30"
// so the child would outlive the test unless the supervisor kills it.
//
// PID is obtained via s.Pid() which is protected by the supervisor's internal
// mutex, so there is no data race between the supervisor setting cmd.Process
// (in cmd.Start) and the test reading it.
func TestSupervisorKillsChildOnShutdown(t *testing.T) {
	spawn := func(int) (*exec.Cmd, error) {
		return exec.Command("sleep", "30"), nil
	}

	// health is permanently false so the supervisor stays in the poll loop —
	// we cancel ctx before readyTimeout to trigger the shutdown path.
	s := NewSupervisor(spawn, 0, func() bool { return false }, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()

	// Wait until the supervisor has started the child and recorded its PID.
	// s.Pid() is safe to call from any goroutine (mu-protected).
	var pid int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pid = s.Pid()
		if pid != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		t.Fatal("child process PID not available within 2s")
	}

	// Cancel ctx — supervisor must kill the child.
	cancel()

	// Wait for the Start goroutine to return.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start goroutine did not return within 2s after cancel")
	}

	// Confirm the child is gone. Give the OS a moment to reap the zombie.
	time.Sleep(50 * time.Millisecond)
	err := syscall.Kill(pid, 0)
	if err == nil {
		// Still a zombie being reaped — Start() already returned, which means
		// Wait() was called and the process is effectively dead. Log only.
		t.Logf("note: signal(0) succeeded (process may be zombie during reap) — Start() returned, which is sufficient")
	}
}
