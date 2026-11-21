package cli

import (
	"encoding/json"
	"fmt"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/feishu"
	"github.com/spf13/cobra"
)

// addFeishuCmd wires the Feishu content library: the collected content mirror
// into one Base, driven through the official lark-cli.
func addFeishuCmd(root *cobra.Command) {
	feishuCmd := &cobra.Command{
		Use:   "feishu",
		Short: "Mirror collected content into a Feishu Base (多维表格)",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show lark-cli identity and the bound content Base",
		RunE: func(cmd *cobra.Command, args []string) error {
			database := openQuickDB()
			defer database.Close()
			settings, err := database.FeishuContentExportSettings()
			if err != nil {
				return err
			}
			out := map[string]any{"export": settings, "configured": settings.Configured()}
			client := feishu.NewClient()
			if identity, identityErr := client.Status(cmd.Context()); identityErr == nil {
				out["cli"] = identity
			} else {
				out["cli_error"] = identityErr.Error()
			}
			encoded, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return err
			}
			cmd.Println(string(encoded))
			return nil
		},
	}

	var initName string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create the content Base (作品库 + 账号日报) and bind it",
		RunE: func(cmd *cobra.Command, args []string) error {
			database := openQuickDB()
			defer database.Close()
			base, err := feishu.NewExporter(database, feishu.NewClient()).Configure(cmd.Context(), initName)
			if err != nil {
				return err
			}
			cmd.Printf("Base created: %s\n", base.URL)
			cmd.Printf("base_token=%s post_table=%s metrics_table=%s\n", base.Token, base.PostTableID, base.MetricsTableID)
			cmd.Println("The content mirror is enabled; every scheduled collection now updates this Base.")
			return nil
		},
	}
	initCmd.Flags().StringVar(&initName, "name", "Alpha-Nexus 内容库", "Base name")

	var exportDays int
	var exportDryRun bool
	var exportLimit int
	var contentDir string
	var contentDryRun bool
	pushContentCmd := &cobra.Command{
		Use:   "push-content",
		Short: "Push finished drafts (成稿方案-*.md) into the Feishu 稿件库 table",
		RunE: func(cmd *cobra.Command, args []string) error {
			database := openQuickDB()
			defer database.Close()
			exporter := feishu.NewExporter(database, feishu.NewClient())
			result, err := exporter.PushContentDir(cmd.Context(), contentDir, contentDryRun)
			if err != nil {
				return err
			}
			encoded, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			cmd.Println(string(encoded))
			return nil
		},
	}
	pushContentCmd.Flags().StringVar(&contentDir, "dir", "/Users/albert/Documents/project/output/content-base", "Directory with 成稿方案-*.md drafts")
	pushContentCmd.Flags().BoolVar(&contentDryRun, "dry-run", false, "Print what would be written without calling Feishu")

	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Mirror collected content into the bound Base now",
		Long: `Mirror collected content into the bound Base.
With --dry-run nothing is sent to Feishu: the command prints the exact rows and
field values a real run would write, so the mapping can be reviewed first.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			database := openQuickDB()
			defer database.Close()
			exporter := feishu.NewExporter(database, feishu.NewClient())
			if exportDryRun {
				preview, err := exporter.Preview(exportDays, exportLimit)
				if err != nil {
					return err
				}
				encoded, err := json.MarshalIndent(preview, "", "  ")
				if err != nil {
					return err
				}
				cmd.Println(string(encoded))
				return nil
			}
			result, err := exporter.Run(cmd.Context(), exportDays)
			if err != nil {
				return err
			}
			encoded, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			cmd.Println(string(encoded))
			return nil
		},
	}
	exportCmd.Flags().IntVar(&exportDays, "days", feishu.DefaultExportDays, "How many days back to mirror")
	exportCmd.Flags().BoolVar(&exportDryRun, "dry-run", false, "Print the rows that would be written and send nothing")
	exportCmd.Flags().IntVar(&exportLimit, "limit", 20, "Max rows per table shown by --dry-run")

	enableCmd := &cobra.Command{
		Use:   "enable",
		Short: "Turn the content mirror on",
		RunE:  setFeishuEnabled(true),
	}
	disableCmd := &cobra.Command{
		Use:   "disable",
		Short: "Turn the content mirror off",
		RunE:  setFeishuEnabled(false),
	}

	feishuCmd.AddCommand(statusCmd, initCmd, exportCmd, pushContentCmd, enableCmd, disableCmd)
	root.AddCommand(feishuCmd)
}

func setFeishuEnabled(enabled bool) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		database := openQuickDB()
		defer database.Close()
		if err := database.SetFeishuContentExportEnabled(enabled); err != nil {
			return err
		}
		settings, err := database.FeishuContentExportSettings()
		if err != nil {
			return err
		}
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		cmd.Printf("Content mirror %s: %s\n", state, describeFeishuBase(settings))
		return nil
	}
}

func describeFeishuBase(settings db.FeishuContentExport) string {
	if !settings.Configured() {
		return "no Base bound yet (run: feishu init)"
	}
	return fmt.Sprintf("%s", settings.BaseURL)
}
