//go:build darwin

package feedback

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
)

const (
	feedbackKeychainService = "alpha-nexus.feedback.feishu"
	feedbackKeychainAccount = "alpha-nexus"
	feedbackSecurityPath    = "/usr/bin/security"
)

type feedbackSecurityCommandRunner func(args []string, stdin string) ([]byte, error)

var runFeedbackSecurityCommand feedbackSecurityCommandRunner = func(args []string, stdin string) ([]byte, error) {
	cmd := exec.Command(feedbackSecurityPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stderr = io.Discard
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.Output()
}

func keychainConfigConfigured() (bool, error) {
	_, err := runFeedbackSecurityCommand([]string{
		"find-generic-password",
		"-a", feedbackKeychainAccount,
		"-s", feedbackKeychainService,
	}, "")
	if err == nil {
		return true, nil
	}
	if feedbackKeychainItemNotFound(err) {
		return false, nil
	}
	return false, ErrKeychainReadFailed
}

func loadKeychainConfig() (string, error) {
	output, err := runFeedbackSecurityCommand([]string{
		"find-generic-password",
		"-a", feedbackKeychainAccount,
		"-s", feedbackKeychainService,
		"-w",
	}, "")
	if err != nil {
		if feedbackKeychainItemNotFound(err) {
			return "", ErrNotConfigured
		}
		return "", ErrKeychainReadFailed
	}
	return strings.TrimSpace(string(output)), nil
}

func saveKeychainConfig(raw string) error {
	_, err := runFeedbackSecurityCommand([]string{
		"add-generic-password",
		"-U",
		"-a", feedbackKeychainAccount,
		"-s", feedbackKeychainService,
		"-w",
	}, keychainInput(raw))
	if err != nil {
		return ErrKeychainWriteFailed
	}
	return nil
}

func feedbackKeychainItemNotFound(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) &&
		exitErr.ProcessState != nil &&
		exitErr.ExitCode() == 44
}
