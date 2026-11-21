//go:build darwin

package agnes

import (
	"errors"
	"os/exec"
	"strings"
)

const (
	keychainService     = "alpha-nexus.agnes.api_key"
	keychainAccount     = "alpha-nexus"
	securityCommandPath = "/usr/bin/security"
)

type securityCommandRunner func(args []string, stdin string) ([]byte, error)

var runSecurityCommand securityCommandRunner = func(args []string, stdin string) ([]byte, error) {
	cmd := exec.Command(securityCommandPath, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.Output()
}

func keychainKeyConfiguredImpl() (bool, error) {
	// Without -w or -g, security checks item metadata and never returns the
	// password value. Output is discarded.
	_, err := runSecurityCommand([]string{
		"find-generic-password",
		"-a", keychainAccount,
		"-s", keychainService,
	}, "")
	if err == nil {
		return true, nil
	}
	if keychainItemNotFound(err) {
		return false, nil
	}
	return false, newError(CodeKeychainReadFailed, 0, false, err)
}

func loadKeychainKeyImpl() (string, error) {
	out, err := runSecurityCommand([]string{
		"find-generic-password",
		"-a", keychainAccount,
		"-s", keychainService,
		"-w",
	}, "")
	if err != nil {
		if keychainItemNotFound(err) {
			return "", newError(CodeKeyMissing, 0, false, nil)
		}
		return "", newError(CodeKeychainReadFailed, 0, false, err)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", newError(CodeKeyMissing, 0, false, nil)
	}
	return key, nil
}

func keychainItemNotFound(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 44
}

func saveKeychainKeyImpl(key string) error {
	// The key is never a process argument. With -w last, security consumes it
	// from stdin; command output is intentionally discarded.
	_, err := runSecurityCommand([]string{
		"add-generic-password",
		"-U",
		"-a", keychainAccount,
		"-s", keychainService,
		"-w",
	}, keychainInput(key))
	if err != nil {
		return newError(CodeKeychainWriteFailed, 0, false, err)
	}
	return nil
}
