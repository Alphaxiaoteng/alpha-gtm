//go:build darwin

package feedback

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestKeychainSaveKeepsBundleOutOfProcessArguments(t *testing.T) {
	previous := runFeedbackSecurityCommand
	t.Cleanup(func() { runFeedbackSecurityCommand = previous })

	var args []string
	var stdin string
	runFeedbackSecurityCommand = func(gotArgs []string, gotStdin string) ([]byte, error) {
		args = append([]string(nil), gotArgs...)
		stdin = gotStdin
		return nil, nil
	}
	store := NewKeychainStore()
	config := Config{
		AppID:     "cli_test_app",
		AppSecret: "synthetic-test-secret",
		WikiToken: "wikcn_test",
		TableID:   "tbl_test",
	}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(args, " ")
	if strings.Contains(joined, config.AppSecret) || strings.Contains(joined, config.AppID) {
		t.Fatalf("configuration leaked into process arguments: %v", args)
	}
	if !strings.Contains(joined, "alpha-nexus.feedback.feishu") ||
		!containsFeedbackArg(args, "add-generic-password") ||
		!containsFeedbackArg(args, "-w") {
		t.Fatalf("unexpected keychain arguments: %v", args)
	}
	if !strings.Contains(stdin, config.AppSecret) || !strings.Contains(stdin, config.WikiToken) {
		t.Fatal("configuration was not supplied through keychain stdin")
	}
}

func TestKeychainConfiguredDoesNotReadSecret(t *testing.T) {
	previous := runFeedbackSecurityCommand
	t.Cleanup(func() { runFeedbackSecurityCommand = previous })

	runFeedbackSecurityCommand = func(args []string, stdin string) ([]byte, error) {
		if containsFeedbackArg(args, "-w") || stdin != "" {
			t.Fatal("configured check requested secret material")
		}
		return nil, nil
	}
	configured, err := NewKeychainStore().Configured()
	if err != nil || !configured {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
}

func TestKeychainLoadDecodesStrictBundleAndMapsMissingItem(t *testing.T) {
	previous := runFeedbackSecurityCommand
	t.Cleanup(func() { runFeedbackSecurityCommand = previous })

	runFeedbackSecurityCommand = func(args []string, _ string) ([]byte, error) {
		if !containsFeedbackArg(args, "find-generic-password") || !containsFeedbackArg(args, "-w") {
			t.Fatalf("unexpected load arguments: %v", args)
		}
		return []byte(`{"app_id":"cli_test_app","app_secret":"synthetic-test-secret","base_token":"bascn_test","table_id":"tbl_test"}`), nil
	}
	config, err := NewKeychainStore().Load()
	if err != nil || config.BaseToken != "bascn_test" || config.WikiToken != "" {
		t.Fatalf("config=%+v err=%v", config, err)
	}

	runFeedbackSecurityCommand = func([]string, string) ([]byte, error) {
		return nil, &exec.ExitError{ProcessState: nil, Stderr: nil}
	}
	// A synthetic ExitError without ProcessState cannot report exit code 44,
	// so it must fail closed as a read error rather than masquerading as absent.
	if _, err := NewKeychainStore().Load(); !errors.Is(err, ErrKeychainReadFailed) {
		t.Fatalf("err=%v", err)
	}

	missingErr := exec.Command("/bin/sh", "-c", "exit 44").Run()
	runFeedbackSecurityCommand = func([]string, string) ([]byte, error) {
		return nil, missingErr
	}
	if _, err := NewKeychainStore().Load(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("missing keychain item err=%v", err)
	}
}

func TestKeychainLoadNeverLeaksInvalidBundleInError(t *testing.T) {
	previous := runFeedbackSecurityCommand
	t.Cleanup(func() { runFeedbackSecurityCommand = previous })

	const secret = "synthetic-invalid-secret"
	runFeedbackSecurityCommand = func([]string, string) ([]byte, error) {
		return []byte(`{"app_id":"id","app_secret":"` + secret + `","wiki_token":"wiki","table_id":"table","unknown":true}`), nil
	}
	_, err := NewKeychainStore().Load()
	if !errors.Is(err, ErrKeychainReadFailed) {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("invalid keychain bundle leaked through error text")
	}
}

func containsFeedbackArg(args []string, wanted string) bool {
	for _, arg := range args {
		if arg == wanted {
			return true
		}
	}
	return false
}
