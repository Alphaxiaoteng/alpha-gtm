//go:build darwin

package agenthub

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureProcess gives each provider invocation its own process group. A
// cancelled run terminates the full group, not only the CLI parent process.
func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
