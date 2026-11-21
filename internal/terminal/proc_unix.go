//go:build unix

package terminal

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var signalProcessGroup = syscall.Kill

func sysProcAttrNewGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	// negative PID = process group
	err := signalProcessGroup(-p.Pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return fmt.Errorf("signal process group: %w", err)
}
