//go:build !legacy_chromium

package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/importers/turbopush"
	"alpha-nexus-egolite/internal/platforms"
)

// This file is the default EgoLite CLI command surface. Chromium/CDP command
// implementations live behind the explicit legacy_chromium build tag.

func addContentCmd(root *cobra.Command) {
	contentCmd := &cobra.Command{Use: "content", Short: "Manage content items"}
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new content item",
		RunE: func(cmd *cobra.Command, _ []string) error {
			contentType, _ := cmd.Flags().GetString("type")
			title, _ := cmd.Flags().GetString("title")
			caption, _ := cmd.Flags().GetString("caption")
			body, _ := cmd.Flags().GetString("body")
			tags, _ := cmd.Flags().GetString("tags")
			if contentType == "" {
				return fmt.Errorf("--type is required (reel, graph_text, article)")
			}
			return runContentCreate(cmd, contentType, title, caption, body, tags)
		},
	}
	createCmd.Flags().StringP("type", "t", "", "Content type (reel/graph_text/article)")
	createCmd.Flags().String("title", "", "Content title")
	createCmd.Flags().StringP("caption", "c", "", "Caption text or file path with @ prefix")
	createCmd.Flags().StringP("body", "b", "", "Body text (for articles)")
	createCmd.Flags().String("tags", "", "Comma-separated tags")
	_ = createCmd.MarkFlagRequired("type")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all content items",
		RunE: func(cmd *cobra.Command, _ []string) error {
			database := openQuickDB()
			defer database.Close()
			contents, err := database.ListContents()
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(writer, "ID\tTYPE\tTITLE\tSTATUS\tPUBLISHED")
			for _, content := range contents {
				title := content.Title
				if len(title) > 30 {
					title = title[:27] + "..."
				}
				fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%d\n", content.ID, content.Type, title, content.Status, content.PublishCount)
			}
			_ = writer.Flush()
			if len(contents) == 0 {
				cmd.Println("No content found.")
			}
			return nil
		},
	}
	contentCmd.AddCommand(createCmd, listCmd)
	root.AddCommand(contentCmd)
}

func runContentCreate(cmd *cobra.Command, contentType, title, caption, body, tags string) error {
	if strings.HasPrefix(caption, "@") {
		data, err := os.ReadFile(caption[1:])
		if err != nil {
			return fmt.Errorf("read caption file: %w", err)
		}
		caption = string(data)
	}
	canonicalTags, err := canonicalContentTags(tags)
	if err != nil {
		return err
	}
	database := openQuickDB()
	defer database.Close()
	content, err := database.CreateContent(contentType, title, caption, body, canonicalTags, "draft")
	if err != nil {
		return err
	}
	cmd.Printf("Content created: #%d (%s)\n", content.ID, contentType)
	return nil
}

func canonicalContentTags(raw string) (string, error) {
	tags := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		if tag := strings.TrimSpace(part); tag != "" {
			tags = append(tags, tag)
		}
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		return "", fmt.Errorf("encode content tags: %w", err)
	}
	return string(encoded), nil
}

func addTargetCmd(root *cobra.Command) {
	targetCmd := &cobra.Command{Use: "target", Short: "Manage publishing targets (account assets)"}
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a publishing target asset for an account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			accountID, _ := cmd.Flags().GetInt64("account-id")
			platformCode, _ := cmd.Flags().GetString("platform")
			assetType, _ := cmd.Flags().GetString("type")
			name, _ := cmd.Flags().GetString("name")
			externalID, _ := cmd.Flags().GetString("external-id")
			if accountID <= 0 {
				return fmt.Errorf("--account-id is required")
			}
			if platformCode == "" {
				return fmt.Errorf("--platform is required")
			}
			if assetType == "" {
				return fmt.Errorf("--type is required")
			}
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if _, err := platforms.Find(platformCode); err != nil {
				return fmt.Errorf("%s: %w", errors.PLATFORM_NOT_FOUND, err)
			}
			database := openQuickDB()
			defer database.Close()
			if _, err := database.GetAccount(accountID); err != nil {
				return err
			}
			asset, err := database.CreateAccountAsset(accountID, platformCode, assetType, name, externalID, "", 0, "")
			if err != nil {
				return fmt.Errorf("create target: %w", err)
			}
			cmd.Printf("Target created: #%d\n", asset.ID)
			cmd.Printf("  Account:  #%d\n", asset.AccountID)
			cmd.Printf("  Platform: %s\n", asset.PlatformCode)
			cmd.Printf("  Type:     %s\n", asset.AssetType)
			cmd.Printf("  Name:     %s\n", asset.Name)
			if asset.ExternalID != "" {
				cmd.Printf("  External: %s\n", asset.ExternalID)
			}
			return nil
		},
	}
	addCmd.Flags().Int64("account-id", 0, "Account ID that owns this target")
	addCmd.Flags().StringP("platform", "P", "", "Platform code")
	addCmd.Flags().String("type", "", "Asset type")
	addCmd.Flags().StringP("name", "n", "", "Display name")
	addCmd.Flags().String("external-id", "", "Optional platform external ID")
	_ = addCmd.MarkFlagRequired("account-id")
	_ = addCmd.MarkFlagRequired("platform")
	_ = addCmd.MarkFlagRequired("type")
	_ = addCmd.MarkFlagRequired("name")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List publishing targets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			platformCode, _ := cmd.Flags().GetString("platform")
			accountID, _ := cmd.Flags().GetInt64("account-id")
			database := openQuickDB()
			defer database.Close()

			var assets []db.AccountAsset
			var err error
			switch {
			case accountID > 0:
				assets, err = database.ListAccountAssetsByAccount(accountID)
			case platformCode != "":
				assets, err = database.ListAccountAssetsByPlatform(platformCode)
			default:
				accounts, listErr := database.ListAccounts()
				if listErr != nil {
					return listErr
				}
				seen := make(map[int64]struct{})
				for _, account := range accounts {
					items, listErr := database.ListAccountAssetsByAccount(account.ID)
					if listErr != nil {
						return listErr
					}
					for _, asset := range items {
						if _, exists := seen[asset.ID]; !exists {
							seen[asset.ID] = struct{}{}
							assets = append(assets, asset)
						}
					}
				}
			}
			if err != nil {
				return err
			}
			if accountID > 0 && platformCode != "" {
				filtered := assets[:0]
				for _, asset := range assets {
					if asset.PlatformCode == platformCode {
						filtered = append(filtered, asset)
					}
				}
				assets = filtered
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(writer, "ID\tACCOUNT_ID\tPLATFORM\tTYPE\tNAME\tEXTERNAL_ID")
			for _, asset := range assets {
				fmt.Fprintf(writer, "%d\t%d\t%s\t%s\t%s\t%s\n", asset.ID, asset.AccountID, asset.PlatformCode, asset.AssetType, asset.Name, asset.ExternalID)
			}
			_ = writer.Flush()
			if len(assets) == 0 {
				cmd.Println("No targets found. Use `target add`.")
			}
			return nil
		},
	}
	listCmd.Flags().StringP("platform", "P", "", "Filter by platform code")
	listCmd.Flags().Int64("account-id", 0, "Filter by account ID")
	targetCmd.AddCommand(addCmd, listCmd)
	root.AddCommand(targetCmd)
}

func addBatchCmd(root *cobra.Command) {
	batchCmd := &cobra.Command{Use: "batch", Short: "Offline batch operations"}
	contentImportCmd := &cobra.Command{
		Use:   "content-import",
		Short: "Batch import contents from CSV",
		RunE: func(cmd *cobra.Command, _ []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			if filePath == "" {
				return fmt.Errorf("--file is required")
			}
			return runBatchContentImport(cmd, filePath)
		},
	}
	contentImportCmd.Flags().StringP("file", "f", "", "CSV file path")
	_ = contentImportCmd.MarkFlagRequired("file")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show batch run status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			batchID, _ := cmd.Flags().GetInt64("batch")
			if batchID == 0 {
				return fmt.Errorf("--batch is required")
			}
			return runBatchStatus(cmd, batchID)
		},
	}
	statusCmd.Flags().Int64P("batch", "b", 0, "Batch run ID")
	_ = statusCmd.MarkFlagRequired("batch")
	batchCmd.AddCommand(contentImportCmd, statusCmd)
	root.AddCommand(batchCmd)
}

func runBatchContentImport(cmd *cobra.Command, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()
	database := openQuickDB()
	defer database.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	var contents []db.Content
	for scanner.Scan() {
		lineNumber++
		if lineNumber == 1 {
			continue
		}
		fields := strings.Split(scanner.Text(), ",")
		if len(fields) < 3 {
			continue
		}
		body, tags := "", ""
		if len(fields) > 3 {
			body = strings.TrimSpace(fields[3])
		}
		if len(fields) > 4 {
			tags = strings.TrimSpace(fields[4])
		}
		content, err := database.CreateContent(
			strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1]), strings.TrimSpace(fields[2]),
			body, tags, "draft",
		)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
		contents = append(contents, *content)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	run, err := database.CreateBatchRun("content-import", "all", "completed", len(contents), 1, "")
	if err != nil {
		return err
	}
	for _, content := range contents {
		if _, err := database.CreateBatchItem(run.ID, "content_import", content.ID, 0, 0, content.ID, 0); err != nil {
			return err
		}
	}
	cmd.Printf("Imported %d contents from %s (batch #%d)\n", len(contents), filePath, run.ID)
	return nil
}

func runBatchStatus(cmd *cobra.Command, batchID int64) error {
	database := openQuickDB()
	defer database.Close()
	run, err := database.GetBatchRun(batchID)
	if err != nil {
		return err
	}
	cmd.Printf("Batch #%d\n", run.ID)
	cmd.Printf("  Operation:  %s\n", run.OperationType)
	cmd.Printf("  Platform:   %s\n", run.PlatformCode)
	cmd.Printf("  Status:     %s\n", run.Status)
	cmd.Printf("  Total:      %d\n", run.TotalItems)
	cmd.Printf("  Pending:    %d\n", run.PendingCount)
	cmd.Printf("  Running:    %d\n", run.RunningCount)
	cmd.Printf("  Success:    %d\n", run.SuccessCount)
	cmd.Printf("  Failed:     %d\n", run.FailedCount)
	cmd.Printf("  Skipped:    %d\n", run.SkippedCount)
	cmd.Printf("  Concurrency: %d\n", run.Concurrency)
	items, err := database.ListBatchItems(batchID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	cmd.Println("\nItems:")
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(writer, "ID\tTYPE\tSTATUS\tERROR")
	for _, item := range items {
		errorCode := item.ErrorCode
		if errorCode == "" {
			errorCode = "-"
		}
		fmt.Fprintf(writer, "%d\t%s\t%s\t%s\n", item.ID, item.ItemType, item.Status, errorCode)
	}
	return writer.Flush()
}

func addJobsRecordsLogsCmds(root *cobra.Command) {
	jobsCmd := &cobra.Command{Use: "jobs", Short: "List publish jobs"}
	jobsCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List recent publish jobs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			database := openQuickDB()
			defer database.Close()
			jobs, err := database.ListPublishJobs(50)
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(writer, "ID\tCONTENT\tACCOUNT\tPLATFORM\tSTATUS\tERROR")
			for _, job := range jobs {
				errorCode := job.ErrorCode
				if errorCode == "" {
					errorCode = "-"
				}
				fmt.Fprintf(writer, "%d\t%d\t%d\t%s\t%s\t%s\n", job.ID, job.ContentID, job.AccountID, job.PlatformCode, job.Status, errorCode)
			}
			return writer.Flush()
		},
	})
	root.AddCommand(jobsCmd)

	recordsCmd := &cobra.Command{Use: "records", Short: "List publish records"}
	recordsCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List recent publish records",
		RunE: func(cmd *cobra.Command, _ []string) error {
			database := openQuickDB()
			defer database.Close()
			records, err := database.ListPublishRecords(50)
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(writer, "ID\tCONTENT\tPLATFORM\tSTATUS\tURL")
			for _, record := range records {
				fmt.Fprintf(writer, "%d\t%d\t%s\t%s\t%s\n", record.ID, record.ContentID, record.PlatformCode, record.Status, record.PostURL)
			}
			return writer.Flush()
		},
	})
	root.AddCommand(recordsCmd)

	logsCmd := &cobra.Command{Use: "logs", Short: "View job logs"}
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show events for a job",
		RunE: func(cmd *cobra.Command, _ []string) error {
			jobID, _ := cmd.Flags().GetInt64("job")
			if jobID == 0 {
				return fmt.Errorf("--job is required")
			}
			database := openQuickDB()
			defer database.Close()
			events, err := database.ListJobEventsByJob(jobID)
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(writer, "ID\tLEVEL\tSTEP\tTYPE\tMESSAGE")
			for _, event := range events {
				message := event.Message
				if len(message) > 50 {
					message = message[:47] + "..."
				}
				fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\n", event.ID, event.Level, event.Step, event.EventType, message)
			}
			return writer.Flush()
		},
	}
	showCmd.Flags().Int64P("job", "j", 0, "Job ID")
	_ = showCmd.MarkFlagRequired("job")
	logsCmd.AddCommand(showCmd)
	root.AddCommand(logsCmd)
}

func addImportCmd(root *cobra.Command) {
	importCmd := &cobra.Command{Use: "import", Short: "Import data from external sources"}
	turbopushCmd := &cobra.Command{
		Use:   "turbopush",
		Short: "Import data from TurboPush data.db",
		RunE: func(cmd *cobra.Command, _ []string) error {
			source, _ := cmd.Flags().GetString("source")
			if source == "" {
				return fmt.Errorf("--source is required")
			}
			return runImportTurboPush(cmd, source)
		},
	}
	turbopushCmd.Flags().StringP("source", "s", "", "Path to TurboPush data.db")
	_ = turbopushCmd.MarkFlagRequired("source")
	importCmd.AddCommand(turbopushCmd)
	root.AddCommand(importCmd)
}

func runImportTurboPush(cmd *cobra.Command, sourcePath string) error {
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}
	database := openQuickDB()
	defer database.Close()
	dataDir, err := activeCfg.ResolvedDataDir()
	if err != nil {
		return err
	}
	cmd.Printf("Importing from %s...\n", sourcePath)
	result, err := turbopush.Import(sourcePath, database, dataDir)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	cmd.Printf("Import complete:\n  Accounts:  %d\n  Settings:  %d\n  Contents:  %d\n  Records:   %d\n  Skipped:   %d\n",
		result.Accounts, result.Settings, result.Contents, result.Records, result.Skipped)
	return nil
}

func addAccountDisableNoteCmd(root *cobra.Command) {
	accountCmd := findCommand(root, "account")
	if accountCmd == nil {
		return
	}
	setEnabledCommand := func(use string, enabled bool) *cobra.Command {
		command := &cobra.Command{
			Use: use,
			RunE: func(cmd *cobra.Command, _ []string) error {
				id, _ := cmd.Flags().GetInt64("id")
				if id == 0 {
					return fmt.Errorf("--id is required")
				}
				database := openQuickDB()
				defer database.Close()
				if err := database.SetAccountEnabled(id, enabled); err != nil {
					return err
				}
				cmd.Printf("Account #%d %s.\n", id, use+"d")
				return nil
			},
		}
		command.Flags().Int64P("id", "i", 0, "Account ID")
		_ = command.MarkFlagRequired("id")
		return command
	}
	noteCmd := &cobra.Command{
		Use:   "note",
		Short: "Update account remark",
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			remark, _ := cmd.Flags().GetString("remark")
			if id == 0 {
				return fmt.Errorf("--id is required")
			}
			database := openQuickDB()
			defer database.Close()
			if _, err := database.GetAccount(id); err != nil {
				return err
			}
			_, err := database.Exec("UPDATE accounts SET remark = ?, updated_at = ? WHERE id = ?", remark, time.Now().UTC().Format(time.RFC3339), id)
			return err
		},
	}
	noteCmd.Flags().Int64P("id", "i", 0, "Account ID")
	noteCmd.Flags().StringP("remark", "r", "", "New remark text")
	_ = noteCmd.MarkFlagRequired("id")
	accountCmd.AddCommand(setEnabledCommand("disable", false), setEnabledCommand("enable", true), noteCmd)
}

func openQuickDB() *db.DB {
	cfg := activeCfg
	if cfg.DataDir == "" {
		cfg = config.DefaultConfig()
	}
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		exitErr(err)
	}
	database, err := db.Open(dataDir)
	if err != nil {
		exitErr(err)
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		exitErr(err)
	}
	return database
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func findCommand(root *cobra.Command, name string) *cobra.Command {
	for _, command := range root.Commands() {
		if command.Use == name || command.Name() == name {
			return command
		}
	}
	return nil
}
