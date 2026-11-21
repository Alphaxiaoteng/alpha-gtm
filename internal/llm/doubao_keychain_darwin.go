//go:build darwin

package llm

import (
	"errors"
	"os/exec"
	"strings"
)

const (
	doubaoKeychainService = "alpha-nexus.doubao.api_key"
	doubaoKeychainAccount = "alpha-nexus"
)

func loadDoubaoKeychainKeyImpl() string {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-a", doubaoKeychainAccount,
		"-s", doubaoKeychainService,
		"-w",
	).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func saveDoubaoKeychainKeyImpl(key string) error {
	// With -w as the last option, security reads the password from stdin rather
	// than exposing it in process arguments. Its output is deliberately ignored.
	cmd := exec.Command(
		"security", "add-generic-password",
		"-U",
		"-a", doubaoKeychainAccount,
		"-s", doubaoKeychainService,
		"-w",
	)
	cmd.Stdin = strings.NewReader(doubaoKeychainInput(key))
	if err := cmd.Run(); err != nil {
		return errors.New("无法更新 macOS 钥匙串中的豆包 API 密钥")
	}
	return nil
}
