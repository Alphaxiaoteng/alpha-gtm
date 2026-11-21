package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/feedback"
)

func TestFeedbackConfigureReadsSecretOnlyFromStdin(t *testing.T) {
	previous := saveFeedbackConfig
	t.Cleanup(func() { saveFeedbackConfig = previous })
	var saved feedback.Config
	saveFeedbackConfig = func(config feedback.Config) error {
		saved = config
		return nil
	}
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("synthetic-secret\n"))
	output := new(bytes.Buffer)
	command.SetOut(output)

	err := runFeedbackConfigure(
		command,
		"cli_test",
		"https://example.feishu.cn/wiki/wik_test?table=tbl_test&view=vew_test",
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if saved.AppID != "cli_test" || saved.AppSecret != "synthetic-secret" ||
		saved.WikiToken != "wik_test" || saved.TableID != "tbl_test" {
		t.Fatalf("saved=%+v", saved)
	}
	if strings.Contains(output.String(), saved.AppSecret) ||
		strings.Contains(output.String(), saved.AppID) {
		t.Fatalf("configuration leaked through output: %q", output.String())
	}
}

func TestFeedbackConfigureRequiresSecretStdinAndValidTarget(t *testing.T) {
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("synthetic-secret"))
	if err := runFeedbackConfigure(command, "cli_test", "https://example.feishu.cn/wiki/wik_test?table=tbl_test", false); err == nil {
		t.Fatal("expected explicit stdin flag")
	}
	for _, rawURL := range []string{
		"http://example.feishu.cn/wiki/wik_test?table=tbl_test",
		"https://attacker.example/wiki/wik_test?table=tbl_test",
		"https://example.feishu.cn/wiki/wik_test",
		"https://example.feishu.cn/docs/doc_test?table=tbl_test",
	} {
		if _, err := feedbackConfigFromURL("cli_test", "synthetic-secret", rawURL); !errors.Is(err, feedback.ErrInvalidConfig) {
			t.Fatalf("url=%q err=%v", rawURL, err)
		}
	}
}

func TestFeedbackConfigFromBaseURL(t *testing.T) {
	config, err := feedbackConfigFromURL(
		"cli_test",
		"synthetic-secret",
		"https://example.feishu.cn/base/bascn_test?table=tbl_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseToken != "bascn_test" || config.WikiToken != "" {
		t.Fatalf("config=%+v", config)
	}
}
