//go:build unix

package terminal

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestKillProcessGroupTreatsESRCHAsExited(t *testing.T) {
	original := signalProcessGroup
	signalProcessGroup = func(int, syscall.Signal) error { return syscall.ESRCH }
	defer func() { signalProcessGroup = original }()

	if err := killProcessGroup(&os.Process{Pid: 987654}); err != nil {
		t.Fatalf("ESRCH should be treated as exited: %v", err)
	}
}

func TestStopAllReportsProcessGroupFailure(t *testing.T) {
	original := signalProcessGroup
	signalProcessGroup = func(int, syscall.Signal) error { return syscall.EPERM }
	defer func() { signalProcessGroup = original }()

	m := NewManager(t.TempDir(), t.TempDir())
	m.sessions["blocked"] = &Session{
		ID:    "blocked",
		Alive: true,
		cmd:   &exec.Cmd{Process: &os.Process{Pid: 987655}},
	}
	err := m.StopAll()
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("StopAll error = %v, want EPERM", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("failed session remained registered after StopAll")
	}
}
