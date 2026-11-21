//go:build darwin

package notify

import (
	"strings"
	"testing"
)

func TestKeychainStoresSecretOnlyThroughStdin(t *testing.T) {
	previous := runSecurityCommand
	defer func() { runSecurityCommand = previous }()
	const secret = "synthetic-notification-secret"
	var args []string
	var stdin string
	runSecurityCommand = func(gotArgs []string, gotStdin string) ([]byte, error) {
		args = append([]string(nil), gotArgs...)
		stdin = gotStdin
		return nil, nil
	}
	if err := keychainSave("feishu", secret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(args, " "), secret) || !strings.Contains(stdin, secret) {
		t.Fatal("notification secret was not isolated to keychain stdin")
	}
	if !strings.Contains(strings.Join(args, " "), "alpha-nexus.notify.feishu") {
		t.Fatalf("unexpected keychain service args: %v", args)
	}
}

func TestKeychainConfiguredCheckNeverRequestsSecretValue(t *testing.T) {
	previous := runSecurityCommand
	defer func() { runSecurityCommand = previous }()
	runSecurityCommand = func(args []string, stdin string) ([]byte, error) {
		if containsArg(args, "-w") || stdin != "" {
			t.Fatal("configured check requested secret value")
		}
		return nil, nil
	}
	configured, err := keychainConfigured("email")
	if err != nil || !configured {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
}

func containsArg(args []string, wanted string) bool {
	for _, arg := range args {
		if arg == wanted {
			return true
		}
	}
	return false
}
