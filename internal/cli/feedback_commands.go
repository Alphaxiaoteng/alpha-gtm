package cli

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/feedback"
)

const maxFeedbackSecretBytes = 4096

var saveFeedbackConfig = func(config feedback.Config) error {
	return feedback.NewKeychainStore().Save(config)
}

func addFeedbackCmd(root *cobra.Command) {
	feedbackCmd := &cobra.Command{
		Use:   "feedback",
		Short: "Configure the local Feishu feedback integration",
	}
	configureCmd := &cobra.Command{
		Use:   "configure",
		Short: "Store Feishu feedback credentials in the macOS Keychain",
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, _ := cmd.Flags().GetString("app-id")
			targetURL, _ := cmd.Flags().GetString("target-url")
			secretFromStdin, _ := cmd.Flags().GetBool("app-secret-stdin")
			return runFeedbackConfigure(cmd, appID, targetURL, secretFromStdin)
		},
	}
	configureCmd.Flags().String("app-id", "", "Feishu custom app ID")
	configureCmd.Flags().String("target-url", "", "Feishu Wiki or Base table URL")
	configureCmd.Flags().Bool("app-secret-stdin", false, "read the App Secret from stdin")
	_ = configureCmd.MarkFlagRequired("app-id")
	_ = configureCmd.MarkFlagRequired("target-url")
	feedbackCmd.AddCommand(configureCmd)
	root.AddCommand(feedbackCmd)
}

func runFeedbackConfigure(
	cmd *cobra.Command,
	appID, targetURL string,
	secretFromStdin bool,
) error {
	if !secretFromStdin {
		return fmt.Errorf("--app-secret-stdin is required")
	}
	secret, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxFeedbackSecretBytes+1))
	if err != nil || len(secret) == 0 || len(secret) > maxFeedbackSecretBytes {
		return feedback.ErrInvalidConfig
	}
	config, err := feedbackConfigFromURL(appID, strings.TrimSpace(string(secret)), targetURL)
	if err != nil {
		return err
	}
	if err := saveFeedbackConfig(config); err != nil {
		return err
	}
	cmd.Println("Feishu feedback is configured in the macOS Keychain.")
	return nil
}

func feedbackConfigFromURL(appID, appSecret, rawURL string) (feedback.Config, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return feedback.Config{}, feedback.ErrInvalidConfig
	}
	host := strings.ToLower(parsed.Hostname())
	if !isFeishuHost(host) {
		return feedback.Config{}, feedback.ErrInvalidConfig
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return feedback.Config{}, feedback.ErrInvalidConfig
	}
	config := feedback.Config{
		AppID:     appID,
		AppSecret: appSecret,
		TableID:   parsed.Query().Get("table"),
	}
	switch parts[len(parts)-2] {
	case "wiki":
		config.WikiToken = parts[len(parts)-1]
	case "base", "bitable":
		config.BaseToken = parts[len(parts)-1]
	default:
		return feedback.Config{}, feedback.ErrInvalidConfig
	}
	if !strings.HasPrefix(config.TableID, "tbl") {
		return feedback.Config{}, feedback.ErrInvalidConfig
	}
	if err := config.Validate(); err != nil {
		return feedback.Config{}, err
	}
	return config, nil
}

func isFeishuHost(host string) bool {
	for _, suffix := range []string{"feishu.cn", "larksuite.com", "larkoffice.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
