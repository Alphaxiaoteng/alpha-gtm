//go:build !unix

package terminal

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func sysProcAttrNewGroup() *syscall.SysProcAttr {
	return nil
}

func killProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill process: %w", err)
	}
	return nil
}
