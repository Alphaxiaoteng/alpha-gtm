//go:build !darwin

package agents

import (
	"errors"
	"runtime"
)

func openCodeSandboxStatus() error {
	return errors.New("native filesystem sandbox unavailable on " + runtime.GOOS)
}

func openCodeSandboxCommand(_ string, _ []string, _ *openCodeSession) (string, []string, error) {
	return "", nil, openCodeSandboxStatus()
}
