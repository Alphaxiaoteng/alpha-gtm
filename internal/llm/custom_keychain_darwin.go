//go:build darwin

package llm

import (
	"os/exec"
	"strings"
)

const (
	customKeychainService = "alpha-nexus.custom-llm.api_key"
	customKeychainAccount = "alpha-nexus"
)

func loadCustomKeychainKeyImpl() string {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-a", customKeychainAccount,
		"-s", customKeychainService,
		"-w",
	).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func saveCustomKeychainKeyImpl(key string) error {
	command := exec.Command(
		"security", "add-generic-password",
		"-a", customKeychainAccount,
		"-s", customKeychainService,
		"-U",
		"-w",
	)
	command.Stdin = strings.NewReader(key + "\n" + key + "\n")
	return command.Run()
}
