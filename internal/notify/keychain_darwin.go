//go:build darwin

package notify

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
)

const securityCommandPath = "/usr/bin/security"

type securityCommandRunner func(args []string, stdin string) ([]byte, error)

var runSecurityCommand securityCommandRunner = func(args []string, stdin string) ([]byte, error) {
	cmd := exec.Command(securityCommandPath, args...)
	// `security ... -w` prompts when the password is intentionally omitted
	// from argv. A new session makes that prompt consume the supplied stdin
	// instead of the app's terminal; never place a credential in argv or logs.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stderr = io.Discard
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.Output()
}

func keychainService(kind string) string { return "alpha-nexus.notify." + kind }

func keychainConfigured(kind string) (bool, error) {
	if !validKind(kind) {
		return false, newError(CodeInvalidConfig)
	}
	_, err := runSecurityCommand([]string{"find-generic-password", "-a", "alpha-nexus", "-s", keychainService(kind)}, "")
	if err == nil {
		return true, nil
	}
	if keychainItemNotFound(err) {
		return false, nil
	}
	return false, newError(CodeKeychainReadFailed)
}

func keychainLoad(kind string) (string, error) {
	if !validKind(kind) {
		return "", newError(CodeInvalidConfig)
	}
	out, err := runSecurityCommand([]string{"find-generic-password", "-a", "alpha-nexus", "-s", keychainService(kind), "-w"}, "")
	if err != nil {
		if keychainItemNotFound(err) {
			return "", newError(CodeNotConfigured)
		}
		return "", newError(CodeKeychainReadFailed)
	}
	value, err := normalizeSecret(kind, string(out))
	if err != nil {
		return "", newError(CodeKeychainReadFailed)
	}
	return value, nil
}

func keychainSave(kind, value string) error {
	normalized, err := normalizeSecret(kind, value)
	if err != nil {
		return err
	}
	_, err = runSecurityCommand([]string{"add-generic-password", "-U", "-a", "alpha-nexus", "-s", keychainService(kind), "-w"}, keychainInput(normalized))
	if err != nil {
		return newError(CodeKeychainWriteFailed)
	}
	return nil
}

func keychainItemNotFound(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 44
}
