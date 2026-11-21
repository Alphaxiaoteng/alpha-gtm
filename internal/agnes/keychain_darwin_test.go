//go:build darwin

package agnes

import (
	"slices"
	"strings"
	"testing"
)

func TestDarwinKeychainUsesAbsoluteSystemSecurityBinary(t *testing.T) {
	if securityCommandPath != "/usr/bin/security" {
		t.Fatalf("security command path=%q", securityCommandPath)
	}
}

func TestDarwinKeychainPassesSecretOnlyThroughStdin(t *testing.T) {
	previous := runSecurityCommand
	var gotArgs []string
	var gotStdin string
	runSecurityCommand = func(args []string, stdin string) ([]byte, error) {
		gotArgs = slices.Clone(args)
		gotStdin = stdin
		return nil, nil
	}
	t.Cleanup(func() { runSecurityCommand = previous })

	if err := saveKeychainKeyImpl("synthetic-agnes-key"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(gotArgs, "synthetic-agnes-key") || strings.Contains(strings.Join(gotArgs, " "), "synthetic-agnes-key") {
		t.Fatalf("secret leaked into process arguments: %v", gotArgs)
	}
	if gotStdin != keychainInput("synthetic-agnes-key") || strings.Count(gotStdin, "synthetic-agnes-key") != 2 {
		t.Fatalf("unexpected stdin framing: %q", gotStdin)
	}
	if !slices.Contains(gotArgs, keychainService) || !slices.Contains(gotArgs, keychainAccount) {
		t.Fatalf("wrong keychain target: %v", gotArgs)
	}
}

func TestDarwinKeychainLoadsWithoutPassingInput(t *testing.T) {
	previous := runSecurityCommand
	runSecurityCommand = func(args []string, stdin string) ([]byte, error) {
		if stdin != "" {
			t.Fatalf("load stdin=%q", stdin)
		}
		if slices.Contains(args, "synthetic-agnes-key") {
			t.Fatalf("unexpected secret in args: %v", args)
		}
		return []byte("synthetic-agnes-key\n"), nil
	}
	t.Cleanup(func() { runSecurityCommand = previous })

	key, err := loadKeychainKeyImpl()
	if err != nil || key != "synthetic-agnes-key" {
		t.Fatalf("key=%q error=%v", key, err)
	}
}

func TestDarwinConfiguredCheckDoesNotRequestPassword(t *testing.T) {
	previous := runSecurityCommand
	runSecurityCommand = func(args []string, stdin string) ([]byte, error) {
		if stdin != "" || slices.Contains(args, "-w") || slices.Contains(args, "-g") {
			t.Fatalf("metadata check may not request secret: args=%v stdin=%q", args, stdin)
		}
		return []byte("synthetic metadata only"), nil
	}
	t.Cleanup(func() { runSecurityCommand = previous })

	configured, err := keychainKeyConfiguredImpl()
	if err != nil || !configured {
		t.Fatalf("configured=%v error=%v", configured, err)
	}
}
