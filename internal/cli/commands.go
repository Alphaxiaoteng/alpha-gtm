//go:build legacy_chromium

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/batch"
	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/importers/turbopush"
	"alpha-nexus-egolite/internal/logincheck"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/platforms/meta"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/ui"
	"alpha-nexus-egolite/internal/xhs"
)

// ---- XHS Data Collection ----

func addXhsCmd(root *cobra.Command) {
	xhsCmd := &cobra.Command{Use: "xhs", Short: "XHS (小红书) data operations"}

	xhsCollectCmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect notes from XHS creator dashboard via CDP (Facade account-id)",
		Long: `Prefer: ops collect --account-id N --start

Legacy: --port / --profile still accepted for fixed CDP port.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			accountID, _ := cmd.Flags().GetInt64("account-id")
			start, _ := cmd.Flags().GetBool("start")
			port, _ := cmd.Flags().GetInt("port")
			profile, _ := cmd.Flags().GetString("profile")
			if accountID > 0 {
				return runOpsCollect(cmd, accountID, start)
			}
			if port == 0 {
				port = 15101
			}
			return runXhsCollectLegacy(cmd, profile, port)
		},
	}
	xhsCollectCmd.Flags().Int64("account-id", 0, "Account ID (recommended)")
	xhsCollectCmd.Flags().Bool("start", false, "Start browser if needed (with --account-id)")
	xhsCollectCmd.Flags().Int("port", 15101, "Legacy CDP port")
	xhsCollectCmd.Flags().StringP("profile", "p", "", "Legacy profile name")

	xhsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List dates with collected XHS data",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runXhsList(cmd)
		},
	}

	xhsShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Show collected XHS data for a specific date",
		RunE: func(cmd *cobra.Command, args []string) error {
			date, _ := cmd.Flags().GetString("date")
			if date == "" {
				return fmt.Errorf("--date is required (YYYY-MM-DD)")
			}
			return runXhsShow(cmd, date)
		},
	}
	xhsShowCmd.Flags().StringP("date", "d", "", "Date to show (YYYY-MM-DD)")
	xhsShowCmd.MarkFlagRequired("date")

	xhsCmd.AddCommand(xhsCollectCmd, xhsListCmd, xhsShowCmd)
	root.AddCommand(xhsCmd)
}

func runXhsCollectLegacy(cmd *cobra.Command, profileName string, port int) error {
	dataDir, err := activeCfg.ResolvedDataDir()
	if err != nil {
		cfg := config.DefaultConfig()
		dataDir, err = cfg.ResolvedDataDir()
		if err != nil {
			return err
		}
	}

	// Check if browser is running (legacy BrowserManager status)
	mgr := browser.NewManager("", dataDir, port)
	if profileName != "" {
		status, err := mgr.Status(profileName)
		if err != nil || !status.Running {
			return fmt.Errorf("browser not running for profile %s. Start it first (or use --account-id --start)", profileName)
		}
	}

	cdpURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd.Printf("Collecting XHS notes from %s...\n", cdpURL)

	ctx := context.Background()
	accountData, err := xhs.ScrapeNotes(ctx, cdpURL)
	if err != nil {
		return fmt.Errorf("scrape notes: %w", err)
	}

	// Save to file
	xhsDataDir := filepath.Join(dataDir, "xhs_data")
	today := time.Now().Format("2006-01-02")
	dayData := &xhs.DayData{
		Date:     today,
		Accounts: []xhs.AccountData{*accountData},
	}

	if err := xhs.SaveToFile(dayData, xhsDataDir); err != nil {
		return fmt.Errorf("save data: %w", err)
	}

	cmd.Printf("Collection complete:\n")
	cmd.Printf("  Account: %s\n", accountData.Account)
	cmd.Printf("  Notes:   %d\n", accountData.NotesCount)
	cmd.Printf("  Saved to: %s/%s.json\n", xhsDataDir, today)

	if accountData.NotesCount > 0 {
		cmd.Printf("\nRecent notes:\n")
		for i, n := range accountData.Notes {
			if i >= 5 {
				break
			}
			title := n.Title
			if len(title) > 40 {
				title = title[:37] + "..."
			}
			cmd.Printf("  %s  %-42s views:%s comments:%s\n", n.PublishTime, title, n.ViewCount, n.CommentCount)
		}
	}

	return nil
}

func runXhsList(cmd *cobra.Command) error {
	dataDir, err := config.DefaultConfig().ResolvedDataDir()
	if err != nil {
		return err
	}
	xhsDataDir := filepath.Join(dataDir, "xhs_data")

	dates, err := xhs.ListDates(xhsDataDir)
	if err != nil {
		return fmt.Errorf("list dates: %w", err)
	}

	cmd.Printf("XHS data collection dates:\n")
	for _, d := range dates {
		data, err := xhs.LoadFromFile(d, xhsDataDir)
		if err != nil {
			continue
		}
		cmd.Printf("  %s  (%d accounts)\n", d, len(data.Accounts))
	}
	if len(dates) == 0 {
		cmd.Println("  No data collected yet.")
	}
	return nil
}

func runXhsShow(cmd *cobra.Command, date string) error {
	dataDir, err := config.DefaultConfig().ResolvedDataDir()
	if err != nil {
		return err
	}
	xhsDataDir := filepath.Join(dataDir, "xhs_data")

	data, err := xhs.LoadFromFile(date, xhsDataDir)
	if err != nil {
		return fmt.Errorf("load data for %s: %w", date, err)
	}

	cmd.Printf("XHS Data for %s:\n", date)
	for _, account := range data.Accounts {
		cmd.Printf("\nAccount: %s\n", account.Account)
		cmd.Printf("Crawled: %s\n", account.CrawledAt)
		cmd.Printf("Notes:   %d\n", account.NotesCount)

		if len(account.Notes) > 0 {
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "TIME\tTITLE\tVIEWS\tLIKES\tCOMMENTS\tCOLLECTS")
			for _, n := range account.Notes {
				title := n.Title
				if len(title) > 30 {
					title = title[:27] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", n.PublishTime, title, n.ViewCount, n.LikeCount, n.CommentCount, n.CollectCount)
			}
			w.Flush()
		}
	}
	return nil
}

// ---- Content ----

func addContentCmd(root *cobra.Command) {
	contentCmd := &cobra.Command{Use: "content", Short: "Manage content items"}
	contentCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new content item",
		RunE: func(cmd *cobra.Command, args []string) error {
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
	contentCreateCmd.Flags().StringP("type", "t", "", "Content type (reel/graph_text/article)")
	contentCreateCmd.Flags().StringP("title", "", "", "Content title")
	contentCreateCmd.Flags().StringP("caption", "c", "", "Caption text or file path with @ prefix")
	contentCreateCmd.Flags().StringP("body", "b", "", "Body text (for articles)")
	contentCreateCmd.Flags().StringP("tags", "", "", "Comma-separated tags")
	contentCreateCmd.MarkFlagRequired("type")

	contentListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all content items",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			contents, err := d.ListContents()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tTYPE\tTITLE\tSTATUS\tPUBLISHED")
			for _, c := range contents {
				title := c.Title
				if len(title) > 30 {
					title = title[:27] + "..."
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\n", c.ID, c.Type, title, c.Status, c.PublishCount)
			}
			w.Flush()
			if len(contents) == 0 {
				cmd.Println("No content found.")
			}
			return nil
		},
	}
	contentCmd.AddCommand(contentCreateCmd, contentListCmd)
	root.AddCommand(contentCmd)
}

func runContentCreate(cmd *cobra.Command, contentType, title, caption, body, tags string) error {
	if caption != "" && strings.HasPrefix(caption, "@") {
		data, err := os.ReadFile(caption[1:])
		if err != nil {
			return fmt.Errorf("read caption file: %w", err)
		}
		caption = string(data)
	}
	d := openQuickDB()
	defer d.Close()
	content, err := d.CreateContent(contentType, title, caption, body, tags, "draft")
	if err != nil {
		return err
	}
	cmd.Printf("Content created: #%d (%s)\n", content.ID, contentType)
	return nil
}

// ---- Meta ----

func addMetaCmd(root *cobra.Command) {
	metaCmd := &cobra.Command{Use: "meta", Short: "Meta Business Suite operations"}
	metaLoginCmd := &cobra.Command{
		Use:   "login",
		Short: "Open Meta Business Suite for manual login",
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, _ := cmd.Flags().GetString("profile")
			port, _ := cmd.Flags().GetInt("port")
			if profile == "" {
				return fmt.Errorf("--profile is required")
			}
			if port == 0 {
				port = 15101
			}
			return runMetaLogin(cmd, profile, port)
		},
	}
	metaLoginCmd.Flags().StringP("profile", "p", "", "Profile name")
	metaLoginCmd.Flags().Int("port", 15101, "CDP port")
	metaLoginCmd.MarkFlagRequired("profile")

	metaCheckCmd := &cobra.Command{
		Use:   "check",
		Short: "Check Meta Business Suite login state",
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, _ := cmd.Flags().GetString("profile")
			port, _ := cmd.Flags().GetInt("port")
			if profile == "" {
				return fmt.Errorf("--profile is required")
			}
			if port == 0 {
				port = 15101
			}
			return runMetaCheck(cmd, profile, port)
		},
	}
	metaCheckCmd.Flags().StringP("profile", "p", "", "Profile name")
	metaCheckCmd.Flags().Int("port", 15101, "CDP port")
	metaCheckCmd.MarkFlagRequired("profile")

	metaDiscoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover Facebook Pages and Instagram accounts from logged-in Meta session",
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, _ := cmd.Flags().GetString("profile")
			port, _ := cmd.Flags().GetInt("port")
			if profile == "" {
				return fmt.Errorf("--profile is required")
			}
			if port == 0 {
				port = 15101
			}
			return runMetaDiscover(cmd, profile, port)
		},
	}
	metaDiscoverCmd.Flags().StringP("profile", "p", "", "Profile name")
	metaDiscoverCmd.Flags().Int("port", 15101, "CDP port")
	metaDiscoverCmd.MarkFlagRequired("profile")

	metaCmd.AddCommand(metaLoginCmd, metaCheckCmd, metaDiscoverCmd)
	root.AddCommand(metaCmd)
}

func runMetaLogin(cmd *cobra.Command, profileName string, port int) error {
	dataDir, err := config.DefaultConfig().ResolvedDataDir()
	if err != nil {
		return err
	}
	mgr := browser.NewManager("", dataDir, port)
	status, err := mgr.Status(profileName)
	if err != nil || !status.Running {
		return fmt.Errorf("browser not running for profile %s. Start it first", profileName)
	}
	cmd.Printf("Meta Business Suite is open at https://business.facebook.com\n")
	cmd.Println("Please log in manually in the browser window.")
	return nil
}

func runMetaCheck(cmd *cobra.Command, profileName string, port int) error {
	dataDir, err := config.DefaultConfig().ResolvedDataDir()
	if err != nil {
		return err
	}
	mgr := browser.NewManager("", dataDir, port)
	status, err := mgr.Status(profileName)
	if err != nil || !status.Running {
		return fmt.Errorf("browser not running for profile %s", profileName)
	}
	cmd.Printf("Browser is RUNNING for profile %s (CDP: %s)\n", profileName, status.CDPURL)
	cmd.Println("Use `alpha-nexus account check --id <ID>` or UI to verify login state.")
	return nil
}

func runMetaDiscover(cmd *cobra.Command, profileName string, port int) error {
	dataDir, err := activeCfg.ResolvedDataDir()
	if err != nil {
		return err
	}

	mgr := browser.NewManager("", dataDir, port)
	status, err := mgr.Status(profileName)
	if err != nil || !status.Running {
		return fmt.Errorf("browser not running for profile %s. Start it first", profileName)
	}

	d := openQuickDB()
	defer d.Close()

	cdpURL := status.CDPURL
	if cdpURL == "" {
		return fmt.Errorf("browser CDP endpoint is unavailable for profile %s", profileName)
	}
	cmd.Printf("Discovering Meta assets from %s...\n", cdpURL)

	// Find account for this profile
	accounts, err := d.ListAccounts()
	if err != nil {
		return err
	}
	var accountID int64
	expectedProfileDir := filepath.Join(dataDir, "profiles", profileName)
	for _, a := range accounts {
		bp, err := d.GetBrowserProfileByAccount(a.ID)
		if err == nil && filepath.Clean(bp.ProfileDir) == filepath.Clean(expectedProfileDir) {
			accountID = a.ID
			break
		}
	}
	if accountID == 0 {
		return fmt.Errorf("profile %q is not bound to a Meta account", profileName)
	}

	ctx := context.Background()
	result, err := meta.Discover(ctx, cdpURL, accountID, d)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	cmd.Printf("Discovery complete:\n")
	cmd.Printf("  Facebook Pages:  %d\n", len(result.FacebookPages))
	for _, p := range result.FacebookPages {
		cmd.Printf("    - %s\n", p)
	}
	cmd.Printf("  Instagram Accounts: %d\n", len(result.InstagramAccounts))
	for _, ig := range result.InstagramAccounts {
		cmd.Printf("    - %s\n", ig)
	}
	if len(result.Errors) > 0 {
		cmd.Printf("  Errors: %d\n", len(result.Errors))
		for _, e := range result.Errors[:3] {
			cmd.Printf("    - %s\n", e)
		}
	}
	return nil
}

// ---- Target ----

func addTargetCmd(root *cobra.Command) {
	targetCmd := &cobra.Command{Use: "target", Short: "Manage publishing targets (account assets)"}

	targetAddCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a publishing target asset for an account",
		Long: `Manually register a publishing target (account_assets row).

Examples:
  alpha-nexus target add --account-id 1 --platform youtube --type youtube_channel --name my-ch
  alpha-nexus target add --account-id 2 --platform facebook --type facebook_page --name My Page
  alpha-nexus target add --account-id 3 --platform xhs --type xhs_account --name my-xhs

Meta assets can also come from meta discover; this command is for manual / non-Meta targets.`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			d := openQuickDB()
			defer d.Close()

			if _, err := d.GetAccount(accountID); err != nil {
				return err
			}

			asset, err := d.CreateAccountAsset(accountID, platformCode, assetType, name, externalID, "", 0, "")
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
	targetAddCmd.Flags().Int64("account-id", 0, "Account ID that owns this target")
	targetAddCmd.Flags().StringP("platform", "P", "", "Platform code (youtube/facebook/xhs/...)")
	targetAddCmd.Flags().String("type", "", "Asset type (youtube_channel/facebook_page/xhs_account/...)")
	targetAddCmd.Flags().StringP("name", "n", "", "Display name")
	targetAddCmd.Flags().String("external-id", "", "Optional platform external ID")
	_ = targetAddCmd.MarkFlagRequired("account-id")
	_ = targetAddCmd.MarkFlagRequired("platform")
	_ = targetAddCmd.MarkFlagRequired("type")
	_ = targetAddCmd.MarkFlagRequired("name")

	targetListCmd := &cobra.Command{
		Use:   "list",
		Short: "List publishing targets (optionally filter by platform or account)",
		RunE: func(cmd *cobra.Command, args []string) error {
			platformCode, _ := cmd.Flags().GetString("platform")
			accountID, _ := cmd.Flags().GetInt64("account-id")

			d := openQuickDB()
			defer d.Close()

			var assets []db.AccountAsset
			var err error
			switch {
			case accountID > 0 && platformCode != "":
				all, e := d.ListAccountAssetsByAccount(accountID)
				if e != nil {
					return e
				}
				for _, a := range all {
					if a.PlatformCode == platformCode {
						assets = append(assets, a)
					}
				}
			case accountID > 0:
				assets, err = d.ListAccountAssetsByAccount(accountID)
			case platformCode != "":
				assets, err = d.ListAccountAssetsByPlatform(platformCode)
			default:
				// List all platforms that have assets via accounts' assets.
				accounts, e := d.ListAccounts()
				if e != nil {
					return e
				}
				seen := map[int64]bool{}
				for _, acc := range accounts {
					list, e := d.ListAccountAssetsByAccount(acc.ID)
					if e != nil {
						return e
					}
					for _, a := range list {
						if !seen[a.ID] {
							seen[a.ID] = true
							assets = append(assets, a)
						}
					}
				}
			}
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tACCOUNT_ID\tPLATFORM\tTYPE\tNAME\tEXTERNAL_ID")
			for _, a := range assets {
				fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\n", a.ID, a.AccountID, a.PlatformCode, a.AssetType, a.Name, a.ExternalID)
			}
			w.Flush()
			if len(assets) == 0 {
				if platformCode != "" {
					cmd.Printf("No targets found for platform %s. Use `target add` or `meta discover`.\n", platformCode)
				} else if accountID > 0 {
					cmd.Printf("No targets found for account %d. Use `target add --account-id %d`.\n", accountID, accountID)
				} else {
					cmd.Println("No targets found. Use `target add` or `meta discover`.")
				}
			}
			return nil
		},
	}
	targetListCmd.Flags().StringP("platform", "P", "", "Filter by platform code (optional)")
	targetListCmd.Flags().Int64("account-id", 0, "Filter by account ID (optional)")

	targetCmd.AddCommand(targetAddCmd, targetListCmd)
	root.AddCommand(targetCmd)
}

// ---- Publish ----

func addPublishCmd(root *cobra.Command) {
	publishCmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish content to a social media platform",
		Long: `Create a publish job for any platform with a declared publish path.

Use --dry-run for offline preflight. Creator-console platforms open the target's
running browser and prepare reviewed fields without clicking publish. YouTube and
TikTok may click once only from a frozen single-target plan with a submit-bound
approval; direct CLI --auto-submit remains fail-closed. X remains dry-run only.
No platform is recorded published without creator-backend evidence.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			platformCode, _ := cmd.Flags().GetString("platform")
			contentID, _ := cmd.Flags().GetInt64("content")
			targetID, _ := cmd.Flags().GetInt64("target")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			autoSubmit, _ := cmd.Flags().GetBool("auto-submit")
			mode, _ := cmd.Flags().GetString("mode")
			approval, _ := cmd.Flags().GetString("approval")

			if platformCode == "" {
				return fmt.Errorf("--platform is required")
			}
			if contentID == 0 {
				return fmt.Errorf("--content is required")
			}
			if targetID == 0 {
				return fmt.Errorf("--target is required")
			}

			if !platforms.CanPublish(platformCode) {
				return fmt.Errorf("%s: platform %q does not support publishing in V1", errors.PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1, platformCode)
			}

			// PRD modes: draft_only | review | browser | api_first
			switch strings.ToLower(strings.TrimSpace(mode)) {
			case "", "browser":
				// default live browser path
			case "draft_only":
				dryRun = true
				autoSubmit = false
			case "review":
				autoSubmit = false
			case "api_first":
				// still uses the target account's running browser for now
			default:
				return fmt.Errorf("unknown --mode %q (draft_only|review|browser|api_first)", mode)
			}
			switch strings.ToLower(strings.TrimSpace(approval)) {
			case "", "none":
			case "always_review", "review":
				autoSubmit = false
			case "trusted", "trusted_auto_submit":
				// keep autoSubmit as user set (default false unless --auto-submit)
			default:
				return fmt.Errorf("unknown --approval %q (none|always_review|trusted)", approval)
			}

			idempotencyKey, _ := cmd.Flags().GetString("idempotency-key")
			return runPublish(cmd, platformCode, contentID, targetID, dryRun, autoSubmit, idempotencyKey)
		},
	}
	publishCmd.Flags().StringP("platform", "P", "", "Platform code")
	publishCmd.Flags().Int64P("content", "c", 0, "Content ID")
	publishCmd.Flags().Int64P("target", "t", 0, "Target asset ID")
	publishCmd.Flags().Bool("dry-run", false, "Validate only; do not attach browser or submit")
	publishCmd.Flags().Bool("auto-submit", false, "Direct CLI rejects this; final click requires publish.plan.submit approval")
	publishCmd.Flags().String("idempotency-key", "", "Stable request key; reuse only to safely resume the same publish request")
	publishCmd.Flags().String("mode", "browser", "Publish mode: draft_only|review|browser|api_first")
	publishCmd.Flags().String("approval", "none", "Approval: none|always_review|trusted")
	publishCmd.MarkFlagRequired("platform")
	publishCmd.MarkFlagRequired("content")
	publishCmd.MarkFlagRequired("target")

	publishResumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "Re-run a publish job from DB (manual_takeover or failed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID, _ := cmd.Flags().GetInt64("job")
			if jobID == 0 {
				return fmt.Errorf("--job is required")
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			autoSubmit, _ := cmd.Flags().GetBool("auto-submit")
			return runPublishResume(cmd, jobID, dryRun, autoSubmit)
		},
	}
	publishResumeCmd.Flags().Int64("job", 0, "Publish job ID")
	publishResumeCmd.Flags().Bool("dry-run", false, "Validate only on resume")
	publishResumeCmd.Flags().Bool("auto-submit", false, "Attempt submit on resume")
	_ = publishResumeCmd.MarkFlagRequired("job")
	publishCmd.AddCommand(
		publishResumeCmd,
		newPublishFrozenRunCmd(),
		newPublishFrozenSubmitCmd(),
		newPublishResolveNotPublishedCmd(),
		newPublishResolveScheduledCmd(),
	)

	root.AddCommand(publishCmd)
}

func runPublish(cmd *cobra.Command, platformCode string, contentID, targetID int64, dryRun, autoSubmit bool, idempotencyKey string) error {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !dryRun && idempotencyKey == "" {
		return fmt.Errorf("--idempotency-key is required for live publish")
	}
	readiness := platforms.Readiness(platformCode)
	if !readiness.CanCreateJob {
		return fmt.Errorf("PLATFORM_PUBLISH_UNAVAILABLE: platform %q has no publish path", platformCode)
	}
	if autoSubmit && !readiness.CanAttemptAutoSubmit {
		return fmt.Errorf("AUTO_SUBMIT_NOT_SUPPORTED: platform %q requires its declared manual/API flow", platformCode)
	}
	if autoSubmit {
		return fmt.Errorf("PUBLISH_FROZEN_SNAPSHOT_REQUIRED: real submit must use one approved single-target publish plan")
	}
	d := openQuickDB()
	defer d.Close()

	content, err := d.GetContent(contentID)
	if err != nil {
		return err
	}

	asset, err := d.GetAccountAsset(targetID)
	if err != nil {
		return err
	}
	if err := publishguard.ValidateTargetOwnership(d, asset, platformCode); err != nil {
		return err
	}

	mediaList, mediaErr := d.ListMediaByContent(content.ID)
	mediaPath, publishMedia, mediaErr := publishguard.PublishMediaPayload(mediaList, mediaErr, content.Type)
	if mediaErr != nil {
		return mediaErr
	}
	cdpURL := ""
	if !dryRun {
		cdpURL, err = resolvePublishCDPURLForPlatform(d, asset.AccountID, platformCode)
		if err != nil {
			return err
		}
	}

	settingsJSON, err := publish.DefaultSettingsSnapshot(platformCode)
	if err != nil {
		return err
	}
	job, err := d.CreatePublishJobWithIntent(
		0, content.ID, asset.AccountID, asset.ID, platformCode, content.Type, idempotencyKey,
		publish.ExecutionMode(dryRun, autoSubmit), settingsJSON,
	)
	if err != nil {
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{"event": "job.started", "jobId": job.ID, "dryRun": dryRun, "autoSubmit": autoSubmit})
	_, _ = d.CreateJobEvent(job.ID, "info", "init", "job.started",
		"publish job requested", "", "")

	cmd.Printf("Publish job created: #%d\n", job.ID)
	cmd.Printf("  Content:  #%d (%s)\n", content.ID, content.Type)
	cmd.Printf("  Target:   #%d (%s)\n", asset.ID, asset.Name)
	cmd.Printf("  Platform: %s\n", platformCode)
	if mediaPath != "" {
		cmd.Printf("  Media:    %s\n", mediaPath)
	}
	if dryRun {
		cmd.Println("  Mode:     dry-run")
	} else if autoSubmit {
		cmd.Println("  Mode:     auto-submit")
	} else {
		cmd.Println("  Mode:     prepare + review")
	}
	cmd.Println()

	credRef := ""
	if ref, refErr := d.GetCredentialRef(asset.AccountID, platformCode); refErr == nil {
		credRef = ref
	}

	runner := &publish.Runner{Store: d}
	result, runErr := runner.Run(context.Background(), publish.RunInput{
		Job:           job,
		Content:       content,
		Asset:         asset,
		CDPURL:        cdpURL,
		DryRun:        dryRun,
		AutoSubmit:    autoSubmit,
		MediaPath:     mediaPath,
		Media:         publishMedia,
		EmitNDJSON:    true,
		CredentialRef: credRef,
	})

	if result != nil {
		cmd.Printf("Result: %s (step=%s)\n", result.Status, result.Step)
		if result.Message != "" {
			cmd.Printf("  %s\n", result.Message)
		}
		if result.Result != nil && result.Result.Screenshot != "" {
			cmd.Printf("  Screenshot: %s\n", result.Result.Screenshot)
		}
	}
	return runErr
}

// ---- Batch ----

func addBatchCmd(root *cobra.Command) {
	batchCmd := &cobra.Command{Use: "batch", Short: "Batch operations"}

	batchAccountCheckCmd := &cobra.Command{
		Use:   "account-check",
		Short: "Batch check login states for accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			return runBatchAccountCheck(cmd, platform, concurrency)
		},
	}
	batchAccountCheckCmd.Flags().StringP("platform", "P", "all", "Platform code or 'all'")
	batchAccountCheckCmd.Flags().IntP("concurrency", "c", 1, "Concurrency")

	batchDiscoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Batch discover Meta targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			return runBatchDiscover(cmd, platform, concurrency)
		},
	}
	batchDiscoverCmd.Flags().StringP("platform", "P", "meta", "Platform")
	batchDiscoverCmd.Flags().IntP("concurrency", "c", 1, "Concurrency")

	batchContentImportCmd := &cobra.Command{
		Use:   "content-import",
		Short: "Batch import contents from CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			if filePath == "" {
				return fmt.Errorf("--file is required")
			}
			return runBatchContentImport(cmd, filePath)
		},
	}
	batchContentImportCmd.Flags().StringP("file", "f", "", "CSV file path")
	batchContentImportCmd.MarkFlagRequired("file")

	batchPublishCmd := &cobra.Command{
		Use:   "publish",
		Short: "Batch publish content from CSV",
		Long: `CSV columns (header optional): content_id,target_id
Optional third column: dry_run (true/false). Default dry-run from --dry-run flag.

Concurrency defaults to 1 (same profile must not parallel-publish).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			filePath, _ := cmd.Flags().GetString("content-file")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			resume, _ := cmd.Flags().GetBool("resume")
			batchID, _ := cmd.Flags().GetInt64("batch-id")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if !resume && filePath == "" {
				return fmt.Errorf("--content-file is required (or --resume with --batch-id)")
			}
			if platform == "" && !resume {
				return fmt.Errorf("--platform is required")
			}
			if platform != "" && !platforms.CanPublish(platform) {
				return fmt.Errorf("%s: platform %q does not support publishing", errors.PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1, platform)
			}
			return runBatchPublish(cmd, platform, filePath, concurrency, resume, batchID, dryRun)
		},
	}
	batchPublishCmd.Flags().StringP("platform", "P", "", "Platform code (all AgentPush platforms)")
	batchPublishCmd.Flags().StringP("content-file", "f", "", "CSV: content_id,target_id")
	batchPublishCmd.Flags().IntP("concurrency", "c", 1, "Concurrency (default 1)")
	batchPublishCmd.Flags().Bool("resume", false, "Resume pending/failed items in a batch")
	batchPublishCmd.Flags().Int64("batch-id", 0, "Batch id for --resume")
	batchPublishCmd.Flags().Bool("dry-run", true, "Default dry-run for safety; set --dry-run=false for live")
	// --platform required for new runs; --resume can infer from batch record.

	batchRetryCmd := &cobra.Command{
		Use:   "retry",
		Short: "Retry failed items in a batch",
		RunE: func(cmd *cobra.Command, args []string) error {
			batchID, _ := cmd.Flags().GetInt64("batch")
			failedOnly, _ := cmd.Flags().GetBool("failed-only")
			if batchID == 0 {
				return fmt.Errorf("--batch is required")
			}
			return runBatchRetry(cmd, batchID, failedOnly)
		},
	}
	batchRetryCmd.Flags().Int64P("batch", "b", 0, "Batch run ID")
	batchRetryCmd.Flags().Bool("failed-only", false, "Only retry failed items")
	batchRetryCmd.MarkFlagRequired("batch")

	batchResumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a cancelled/interrupted batch",
		RunE: func(cmd *cobra.Command, args []string) error {
			batchID, _ := cmd.Flags().GetInt64("batch")
			if batchID == 0 {
				return fmt.Errorf("--batch is required")
			}
			return runBatchResume(cmd, batchID)
		},
	}
	batchResumeCmd.Flags().Int64P("batch", "b", 0, "Batch run ID")
	batchResumeCmd.MarkFlagRequired("batch")

	batchStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show batch run status",
		RunE: func(cmd *cobra.Command, args []string) error {
			batchID, _ := cmd.Flags().GetInt64("batch")
			if batchID == 0 {
				return fmt.Errorf("--batch is required")
			}
			return runBatchStatus(cmd, batchID)
		},
	}
	batchStatusCmd.Flags().Int64P("batch", "b", 0, "Batch run ID")
	batchStatusCmd.MarkFlagRequired("batch")

	// ponytail: keep legacy command implementations compile-checked while their
	// Chromium/CDP entry points remain deliberately unreachable in EgoLite.
	_ = []*cobra.Command{batchAccountCheckCmd, batchDiscoverCmd, batchPublishCmd, batchRetryCmd, batchResumeCmd}
	batchCmd.AddCommand(batchContentImportCmd, batchStatusCmd)
	root.AddCommand(batchCmd)
}

// runBatchAccountCheck starts each account browser via Facade, verifies only
// reviewed cookie-name evidence, then stops browsers it started. Concurrency is bounded.
// Item-level error codes match internal/errors (T-B03).
func runBatchAccountCheck(cmd *cobra.Command, platformFilter string, concurrency int) error {
	if concurrency < 1 {
		concurrency = 1
	}

	cfg := config.DefaultConfig()
	if d, err := cmd.Flags().GetString("data-dir"); err == nil && d != "" {
		cfg.DataDir = d
	}
	if b, err := cmd.Flags().GetString("browser"); err == nil && b != "" {
		cfg.BrowserPath = b
	}
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	var accounts []db.Account
	if platformFilter == "all" {
		accounts, err = database.ListAccounts()
	} else {
		accounts, err = database.ListAccountsByPlatform(platformFilter)
	}
	if err != nil {
		return err
	}

	run, err := database.CreateBatchRun("account-check", platformFilter, "running", len(accounts), concurrency, "")
	if err != nil {
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event": "batch.started", "batchId": run.ID, "operation": "account-check",
		"total": len(accounts), "concurrency": concurrency,
	})

	type result struct {
		success bool
		code    string
	}
	results := make([]result, len(accounts))
	sem := make(chan struct{}, concurrency)

	// Sequential path when concurrency==1 keeps ordering simple; still uses Facade.
	// Parallel path for concurrency>1.
	runOne := func(i int, a db.Account) {
		item, err := database.CreateBatchItem(run.ID, "account_check", 0, a.ID, 0, 0, 0)
		if err != nil {
			results[i] = result{false, "INTERNAL"}
			return
		}
		batch.EmitNDJSON(map[string]interface{}{
			"event": "batch.item.started", "batchId": run.ID, "itemId": item.ID, "accountId": a.ID,
		})

		holderID := fmt.Sprintf("batch-%d-acct-%d", run.ID, a.ID)
		_, startErr := facade.StartAccount(a.ID, browser.StartOptions{
			HolderType: browser.HolderBatch,
			HolderID:   holderID,
			SkipFocus:  true,
		})
		startedHere := startErr == nil
		if startErr != nil {
			code := errorCodeOf(startErr)
			// ALREADY_RUNNING is OK — proceed to check
			if !errors.Is(startErr.Error(), errors.ALREADY_RUNNING) {
				_ = database.UpdateBatchItemStatus(item.ID, "failed", "start", code, startErr.Error())
				batch.EmitNDJSON(map[string]interface{}{
					"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID,
					"accountId": a.ID, "errorCode": code, "message": startErr.Error(),
				})
				results[i] = result{false, code}
				return
			}
		}
		if startedHere {
			defer func() {
				if stopErr := facade.StopAccount(a.ID); stopErr != nil {
					batch.EmitNDJSON(map[string]interface{}{
						"event": "batch.item.warning", "batchId": run.ID, "itemId": item.ID,
						"accountId": a.ID, "errorCode": errorCodeOf(stopErr), "message": stopErr.Error(),
					})
				}
			}()
		}

		status, statusErr := facade.Status(a.ID)
		if statusErr != nil || !status.Running || status.CDPPort <= 0 {
			code := "ACCOUNT_BROWSER_NOT_RUNNING"
			message := "account browser is unavailable for verification"
			if statusErr != nil {
				code = errorCodeOf(statusErr)
				message = statusErr.Error()
			}
			_ = database.UpdateBatchItemStatus(item.ID, "failed", "verify", code, message)
			results[i] = result{false, code}
			return
		}
		check, checkErr := logincheck.VerifyPassive(context.Background(), a.PlatformCode, status.CDPPort)
		if checkErr != nil || !check.LoggedIn {
			code := "LOGIN_NOT_VERIFIED"
			message := "reviewed login evidence was not found"
			if checkErr != nil {
				code = errorCodeOf(checkErr)
				message = checkErr.Error()
			}
			_ = database.UpdateBatchItemStatus(item.ID, "failed", "verify", code, message)
			batch.EmitNDJSON(map[string]interface{}{
				"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID,
				"accountId": a.ID, "errorCode": code, "message": message,
			})
			results[i] = result{false, code}
			return
		}
		if err := database.UpdateAccountLoginState(a.ID, "logged_in"); err != nil {
			_ = database.UpdateBatchItemStatus(item.ID, "failed", "verify", "ACCOUNT_STATE_UPDATE", err.Error())
			results[i] = result{false, "ACCOUNT_STATE_UPDATE"}
			return
		}
		_ = database.UpdateBatchItemStatus(item.ID, "success", "verified", "", "")
		batch.EmitNDJSON(map[string]interface{}{
			"event": "batch.item.success", "batchId": run.ID, "itemId": item.ID,
			"accountId": a.ID, "state": "logged_in",
		})

		results[i] = result{true, ""}
	}

	if concurrency == 1 {
		for i, a := range accounts {
			runOne(i, a)
		}
	} else {
		// Lightweight parallel without extra import churn: use channel + goroutines
		done := make(chan struct{}, len(accounts))
		for i, a := range accounts {
			sem <- struct{}{}
			go func(i int, a db.Account) {
				defer func() { <-sem; done <- struct{}{} }()
				runOne(i, a)
			}(i, a)
		}
		for range accounts {
			<-done
		}
	}

	success, failed := 0, 0
	for _, r := range results {
		if r.success {
			success++
		} else {
			failed++
		}
	}
	_ = database.UpdateBatchRunCounters(run.ID, 0, 0, success, failed, 0)
	batch.EmitNDJSON(map[string]interface{}{
		"event": "batch.finished", "batchId": run.ID, "success": success, "failed": failed,
	})
	cmd.Printf("Batch account-check #%d completed: %d success, %d failed\n", run.ID, success, failed)
	return nil
}

func runBatchDiscover(cmd *cobra.Command, platformFilter string, concurrency int) error {
	if concurrency < 1 {
		concurrency = 1
	}
	platformFilter = strings.ToLower(strings.TrimSpace(platformFilter))
	if platformFilter == "" {
		platformFilter = "meta"
	}
	if platformFilter != "meta" && platformFilter != "all" && platformFilter != "facebook" && platformFilter != "instagram" {
		return fmt.Errorf("BATCH_DISCOVER_UNSUPPORTED_PLATFORM: %s", platformFilter)
	}

	cfg := config.DefaultConfig()
	if dataDir, err := cmd.Flags().GetString("data-dir"); err == nil && dataDir != "" {
		cfg.DataDir = dataDir
	}
	if browserPath, err := cmd.Flags().GetString("browser"); err == nil && browserPath != "" {
		cfg.BrowserPath = browserPath
	}
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	var accounts []db.Account
	if platformFilter == "facebook" || platformFilter == "instagram" {
		accounts, err = database.ListAccountsByPlatform(platformFilter)
	} else {
		all, listErr := database.ListAccounts()
		if listErr != nil {
			return listErr
		}
		for _, account := range all {
			if account.PlatformCode == "meta" || account.PlatformCode == "facebook" || account.PlatformCode == "instagram" {
				accounts = append(accounts, account)
			}
		}
	}
	if err != nil {
		return err
	}

	run, err := database.CreateBatchRun("discover", platformFilter, "running", len(accounts), concurrency, "")
	if err != nil {
		return err
	}
	batch.EmitNDJSON(map[string]interface{}{
		"event": "batch.started", "batchId": run.ID, "operation": "discover",
		"total": len(accounts), "concurrency": concurrency,
	})

	type result struct {
		success bool
	}
	results := make([]result, len(accounts))
	sem := make(chan struct{}, concurrency)
	runOne := func(index int, account db.Account) {
		item, itemErr := database.CreateBatchItem(run.ID, "meta_discover", 0, account.ID, 0, 0, 0)
		if itemErr != nil {
			return
		}
		batch.EmitNDJSON(map[string]interface{}{
			"event": "batch.item.started", "batchId": run.ID, "itemId": item.ID, "accountId": account.ID,
		})
		status, statusErr := facade.Status(account.ID)
		if statusErr != nil || !status.Running || status.CDPURL == "" {
			code := "ACCOUNT_BROWSER_NOT_RUNNING"
			message := "start the account browser and finish Meta login before discovery"
			if statusErr != nil {
				code = errorCodeOf(statusErr)
				message = statusErr.Error()
			}
			_ = database.UpdateBatchItemStatus(item.ID, "failed", "browser", code, message)
			batch.EmitNDJSON(map[string]interface{}{
				"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID,
				"accountId": account.ID, "errorCode": code, "message": message,
			})
			return
		}
		discovery, discoverErr := meta.Discover(context.Background(), status.CDPURL, account.ID, database)
		if discoverErr != nil || !discovery.Success {
			code := "META_DISCOVER_FAILED"
			message := "Meta target discovery did not complete"
			if discoverErr != nil {
				code = errorCodeOf(discoverErr)
				message = discoverErr.Error()
			} else if len(discovery.Errors) > 0 {
				code = "META_DISCOVER_PARTIAL"
				message = strings.Join(discovery.Errors, "; ")
			}
			_ = database.UpdateBatchItemStatus(item.ID, "failed", "discover", code, message)
			batch.EmitNDJSON(map[string]interface{}{
				"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID,
				"accountId": account.ID, "errorCode": code, "message": message,
			})
			return
		}
		message := fmt.Sprintf("facebook_pages=%d instagram_accounts=%d", len(discovery.FacebookPages), len(discovery.InstagramAccounts))
		_ = database.UpdateBatchItemStatus(item.ID, "success", "discovered", "", message)
		batch.EmitNDJSON(map[string]interface{}{
			"event": "batch.item.success", "batchId": run.ID, "itemId": item.ID,
			"accountId": account.ID, "facebookPages": len(discovery.FacebookPages), "instagramAccounts": len(discovery.InstagramAccounts),
		})
		results[index].success = true
	}
	if concurrency == 1 {
		for index, account := range accounts {
			runOne(index, account)
		}
	} else {
		done := make(chan struct{}, len(accounts))
		for index, account := range accounts {
			sem <- struct{}{}
			go func(index int, account db.Account) {
				defer func() { <-sem; done <- struct{}{} }()
				runOne(index, account)
			}(index, account)
		}
		for range accounts {
			<-done
		}
	}

	success, failed := 0, 0
	for _, item := range results {
		if item.success {
			success++
		} else {
			failed++
		}
	}
	_ = database.UpdateBatchRunCounters(run.ID, 0, 0, success, failed, 0)
	batch.EmitNDJSON(map[string]interface{}{
		"event": "batch.finished", "batchId": run.ID, "success": success, "failed": failed,
	})
	cmd.Printf("Batch discover #%d completed: %d success, %d failed\n", run.ID, success, failed)
	return nil
}

func runBatchContentImport(cmd *cobra.Command, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	d := openQuickDB()
	defer d.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var contents []db.Content

	for scanner.Scan() {
		lineNum++
		if lineNum == 1 {
			continue // skip header
		}
		line := scanner.Text()
		fields := strings.Split(line, ",")
		if len(fields) < 3 {
			continue
		}
		contentType := strings.TrimSpace(fields[0])
		title := strings.TrimSpace(fields[1])
		caption := strings.TrimSpace(fields[2])
		body := ""
		tags := ""
		if len(fields) > 3 {
			body = strings.TrimSpace(fields[3])
		}
		if len(fields) > 4 {
			tags = strings.TrimSpace(fields[4])
		}
		c, err := d.CreateContent(contentType, title, caption, body, tags, "draft")
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}
		contents = append(contents, *c)
	}

	run, err := d.CreateBatchRun("content-import", "all", "completed", len(contents), 1, "")
	if err != nil {
		return err
	}

	for _, c := range contents {
		d.CreateBatchItem(run.ID, "content_import", c.ID, 0, 0, c.ID, 0)
	}

	cmd.Printf("Imported %d contents from %s (batch #%d)\n", len(contents), filePath, run.ID)
	return nil
}

// resetBatchPublishItemForResume is only called by an explicit publish
// --resume request. Manual handoffs are reset with an optimistic status
// predicate so a concurrently running item is never silently reclaimed.
func resetBatchPublishItemForResume(d *db.DB, itemID int64, fromStatus string) (bool, error) {
	fromStatus = strings.ToLower(strings.TrimSpace(fromStatus))
	if fromStatus == "manual_takeover" || fromStatus == "awaiting_review" {
		now := time.Now().UTC().Format(time.RFC3339)
		result, err := d.Exec(`UPDATE batch_items SET status='pending', current_step='', error_code='', error_message='', finished_at=NULL, updated_at=? WHERE id=? AND status=?`, now, itemID, fromStatus)
		if err != nil {
			return false, err
		}
		changed, err := result.RowsAffected()
		return changed == 1, err
	}
	return d.ResetBatchItemForResume(itemID, fromStatus)
}

func runBatchPublish(cmd *cobra.Command, platformCode, filePath string, concurrency int, resume bool, batchID int64, dryRun bool) error {
	d := openQuickDB()
	defer d.Close()

	if concurrency <= 0 {
		concurrency = 1
	}

	var run *db.BatchRun
	var err error

	if resume {
		if batchID <= 0 {
			// Find latest publish batch for platform (or any publish batch).
			runs, listErr := d.ListBatchRuns(100)
			if listErr != nil {
				return listErr
			}
			found := batch.FindLatestPublishBatch(runs, platformCode, filePath)
			if found == nil {
				return fmt.Errorf("--batch-id is required with --resume (no prior publish batch found)")
			}
			batchID = found.ID
		}
		run, err = d.GetBatchRun(batchID)
		if err != nil {
			return err
		}
		if platformCode != "" && platformCode != run.PlatformCode {
			return fmt.Errorf("batch resume cannot change stored platform %q", run.PlatformCode)
		}
		platformCode = run.PlatformCode
		var storedIntent struct {
			DryRun *bool `json:"dry_run"`
		}
		if err := json.Unmarshal([]byte(run.SettingsJSON), &storedIntent); err != nil || storedIntent.DryRun == nil {
			return fmt.Errorf("batch #%d has no valid stored publish intent", run.ID)
		}
		if dryRun != *storedIntent.DryRun {
			return fmt.Errorf("batch resume cannot change stored publish intent (dry_run=%v)", *storedIntent.DryRun)
		}
		dryRun = *storedIntent.DryRun
		// Reset failed/interrupted items to pending so the runner loop is uniform.
		all, listErr := d.ListBatchItems(run.ID)
		if listErr != nil {
			return listErr
		}
		resumable := batch.FilterResumableItems(all)
		if len(resumable) == 0 {
			cmd.Printf("Batch #%d has no pending/failed items to resume.\n", run.ID)
			return nil
		}
		for _, it := range resumable {
			status := strings.ToLower(strings.TrimSpace(it.Status))
			if status == "pending" {
				continue
			}
			if _, resetErr := resetBatchPublishItemForResume(d, it.ID, status); resetErr != nil {
				return fmt.Errorf("reset batch item %d for resume: %w", it.ID, resetErr)
			}
		}
		cmd.Printf("Resuming batch publish #%d (%d items)\n", run.ID, len(resumable))
		batch.EmitNDJSON(map[string]interface{}{
			"event": "batch.started", "batchId": run.ID, "operation": "publish",
			"total": len(resumable), "resume": true, "dryRun": dryRun,
		})
	} else {
		rows, err := parsePublishCSV(filePath)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("no rows in %s", filePath)
		}
		settings := fmt.Sprintf(`{"dry_run":%v,"content_file":%q}`, dryRun, filePath)
		run, err = d.CreateBatchRun("publish", platformCode, "running", len(rows), concurrency, settings)
		if err != nil {
			return err
		}
		for _, r := range rows {
			accountID := int64(0)
			if asset, aErr := d.GetAccountAsset(r.targetID); aErr == nil {
				accountID = asset.AccountID
			}
			if _, err := d.CreateBatchItem(run.ID, "publish", 0, accountID, r.targetID, r.contentID, 0); err != nil {
				return err
			}
		}
		batch.EmitNDJSON(map[string]interface{}{
			"event": "batch.started", "batchId": run.ID, "operation": "publish", "total": len(rows), "dryRun": dryRun,
		})
	}

	items, err := d.ListBatchItems(run.ID)
	if err != nil {
		return err
	}

	// Only process pending (after resume reset, failed became pending).
	toRun := make([]db.BatchItem, 0, len(items))
	for _, it := range items {
		if it.Status == "pending" {
			toRun = append(toRun, it)
		}
	}
	if len(toRun) == 0 {
		cmd.Printf("Batch #%d: nothing to run\n", run.ID)
		return nil
	}

	sem := make(chan struct{}, concurrency)
	var (
		mu            sync.Mutex
		success       int
		failed        int
		firstStateErr error
	)
	recordStateErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstStateErr == nil {
			firstStateErr = err
		}
		mu.Unlock()
	}
	updateItem := func(id int64, status, step, code, message string) bool {
		if err := d.UpdateBatchItemStatus(id, status, step, code, message); err != nil {
			recordStateErr(fmt.Errorf("update batch item %d to %s: %w", id, status, err))
			return false
		}
		return true
	}

	runOne := func(item db.BatchItem) {
		claimed, claimErr := d.ClaimPendingBatchItem(item.ID, "publish")
		if claimErr != nil {
			recordStateErr(fmt.Errorf("claim batch item %d: %w", item.ID, claimErr))
			return
		}
		if !claimed {
			return
		}
		batch.EmitNDJSON(map[string]interface{}{
			"event": "batch.item.started", "batchId": run.ID, "itemId": item.ID,
			"contentId": item.ContentID, "assetId": item.AssetID,
		})

		content, err := d.GetContent(item.ContentID)
		if err != nil {
			if !updateItem(item.ID, "failed", "load_content", "CONTENT_NOT_FOUND", err.Error()) {
				return
			}
			batch.EmitNDJSON(map[string]interface{}{"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID, "errorCode": "CONTENT_NOT_FOUND"})
			mu.Lock()
			failed++
			mu.Unlock()
			return
		}
		asset, err := d.GetAccountAsset(item.AssetID)
		if err != nil {
			if !updateItem(item.ID, "failed", "load_asset", "ASSET_NOT_FOUND", err.Error()) {
				return
			}
			batch.EmitNDJSON(map[string]interface{}{"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID, "errorCode": "ASSET_NOT_FOUND"})
			mu.Lock()
			failed++
			mu.Unlock()
			return
		}
		plat := platformCode
		if plat == "" {
			plat = asset.PlatformCode
		}
		if targetErr := publishguard.ValidateTargetOwnership(d, asset, plat); targetErr != nil {
			if !updateItem(item.ID, "failed", "target_preflight", targetErr.Error(), targetErr.Error()) {
				return
			}
			batch.EmitNDJSON(map[string]interface{}{"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID, "errorCode": targetErr.Error()})
			mu.Lock()
			failed++
			mu.Unlock()
			return
		}
		markReplayedOutcome := func(existingJob *db.PublishJob) bool {
			disposition := batch.ClassifyPublishStatus(existingJob.Status)
			message := "existing publish job already completed"
			if disposition.WaitingHuman {
				message = "existing publish job still requires human action"
			}
			if !updateItem(item.ID, disposition.ItemStatus, "already_recorded", "", message) {
				return false
			}
			batch.EmitNDJSON(map[string]interface{}{
				"event": disposition.Event, "batchId": run.ID, "itemId": item.ID,
				"jobId": existingJob.ID, "status": existingJob.Status, "replayed": true,
			})
			if disposition.Success {
				mu.Lock()
				success++
				mu.Unlock()
			}
			return true
		}

		var job *db.PublishJob
		var mediaPath, publishAt, complianceJSON string
		var publishMedia []platforms.PublishMedia
		frozenInput := false
		if item.JobID > 0 {
			job, err = d.GetPublishJob(item.JobID)
			if err == nil && (job.BatchID != run.ID || job.ContentID != content.ID || job.AccountID != asset.AccountID || job.AssetID != asset.ID || job.PlatformCode != plat) {
				err = fmt.Errorf("stored publish job target does not match batch item")
			}
			if err == nil {
				err = d.ValidatePublishJobMode(job.ID, publish.ExecutionMode(dryRun, false))
			}
			if err != nil {
				if !updateItem(item.ID, "failed", "load_job", "JOB_INTENT_MISMATCH", err.Error()) {
					return
				}
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			switch job.Status {
			case "success", "published", "manual_takeover", "awaiting_review":
				// Reconcile locally without requiring media or a running browser.
				// Human states remain handoffs and are never rewritten as success.
				markReplayedOutcome(job)
				return
			case "failed", "pending":
				// Preflight first; a failed job consumes retry budget only below.
			default:
				if !updateItem(item.ID, "failed", "resume_job", "JOB_NOT_RESUMABLE", "stored publish job is not claimable") {
					return
				}
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			if job.PlanID > 0 {
				frozen, frozenErr := (&publishplan.Service{DB: d}).FrozenInputForJob(job)
				if frozenErr != nil {
					if !updateItem(item.ID, "failed", "frozen_snapshot_preflight", "PUBLISH_FROZEN_SNAPSHOT_INVALID", frozenErr.Error()) {
						return
					}
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				content, asset = frozen.Content, frozen.Asset
				plat = job.PlatformCode
				mediaPath, publishMedia, publishAt, complianceJSON = frozen.MediaPath, frozen.Media, frozen.PublishAt, frozen.ComplianceJSON
				frozenInput = true
				if content.ID != item.ContentID || asset.ID != item.AssetID {
					if !updateItem(item.ID, "failed", "frozen_snapshot_preflight", "JOB_INTENT_MISMATCH", "frozen publish target does not match batch item") {
						return
					}
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				if targetErr := publishguard.ValidateTargetOwnership(d, asset, plat); targetErr != nil {
					if !updateItem(item.ID, "failed", "target_preflight", targetErr.Error(), targetErr.Error()) {
						return
					}
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
			}
		}

		if !frozenInput {
			mediaList, mediaErr := d.ListMediaByContent(content.ID)
			mediaPath, publishMedia, mediaErr = publishguard.PublishMediaPayload(mediaList, mediaErr, content.Type)
			if mediaErr != nil {
				if !updateItem(item.ID, "failed", "media_preflight", mediaErr.Error(), mediaErr.Error()) {
					return
				}
				batch.EmitNDJSON(map[string]interface{}{"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID, "errorCode": mediaErr.Error()})
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
		}
		cdpURL := ""
		if !dryRun {
			cdpURL, err = resolvePublishCDPURLForPlatform(d, asset.AccountID, plat)
			if err != nil {
				if !updateItem(item.ID, "failed", "browser_preflight", err.Error(), err.Error()) {
					return
				}
				batch.EmitNDJSON(map[string]interface{}{"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID, "errorCode": err.Error()})
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
		}

		if job == nil {
			settingsJSON, settingsErr := publish.DefaultSettingsSnapshot(plat)
			if settingsErr != nil {
				err = settingsErr
			} else {
				job, err = d.CreatePublishJobWithIntent(
					run.ID, content.ID, asset.AccountID, asset.ID, plat, content.Type, fmt.Sprintf("batch:%d:item:%d", run.ID, item.ID),
					publish.ExecutionMode(dryRun, false), settingsJSON,
				)
			}
		}
		if err != nil {
			if !updateItem(item.ID, "failed", "create_job", "JOB_CREATE", err.Error()) {
				return
			}
			mu.Lock()
			failed++
			mu.Unlock()
			return
		}
		if linkErr := d.UpdateBatchItemJob(item.ID, job.ID); linkErr != nil {
			recordStateErr(fmt.Errorf("link batch item %d to publish job %d: %w", item.ID, job.ID, linkErr))
			updateItem(item.ID, "failed", "link_job", "JOB_LINK", "publish job link could not be persisted")
			return
		}
		switch job.Status {
		case "success", "published", "manual_takeover", "awaiting_review":
			markReplayedOutcome(job)
			return
		case "failed":
			failedJobID := job.ID
			job, err = d.PreparePublishJobResume(job.ID, job.TraceID)
			if err != nil {
				if !updateItem(item.ID, "failed", "resume_job", "JOB_NOT_RESUMABLE", err.Error()) {
					return
				}
				batch.EmitNDJSON(map[string]interface{}{
					"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID,
					"jobId": failedJobID, "errorCode": "JOB_NOT_RESUMABLE",
				})
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
		case "pending":
			// New or crash-recovered job: Runner will claim it below.
		default:
			if !updateItem(item.ID, "failed", "resume_job", "JOB_NOT_RESUMABLE", "stored publish job is not claimable") {
				return
			}
			mu.Lock()
			failed++
			mu.Unlock()
			return
		}

		credRef := ""
		if ref, refErr := d.GetCredentialRef(asset.AccountID, plat); refErr == nil {
			credRef = ref
		}

		runner := &publish.Runner{Store: d}
		res, runErr := runner.Run(context.Background(), publish.RunInput{
			Job: job, Content: content, Asset: asset,
			CDPURL: cdpURL, DryRun: dryRun, MediaPath: mediaPath,
			Media:      publishMedia,
			EmitNDJSON: true, CredentialRef: credRef,
			PublishAt:      publishAt,
			ComplianceJSON: complianceJSON,
		})
		status := ""
		if res != nil {
			status = string(res.Status)
		}
		disposition := batch.ClassifyPublishStatus(status)
		ok := runErr == nil && res != nil && (disposition.Success || disposition.WaitingHuman ||
			disposition.PendingVerification || disposition.RequiresReconciliation)
		if !ok {
			msg := ""
			code := "PUBLISH_FAILED"
			step := "publish"
			if res != nil {
				msg = res.Message
				step = res.Step
				if res.Result != nil && res.Result.ErrorCode != "" {
					code = res.Result.ErrorCode
				}
			}
			if runErr != nil && msg == "" {
				msg = runErr.Error()
			}
			if !updateItem(item.ID, "failed", step, code, msg) {
				return
			}
			batch.EmitNDJSON(map[string]interface{}{"event": "batch.item.failed", "batchId": run.ID, "itemId": item.ID, "errorCode": code, "jobId": job.ID})
			mu.Lock()
			failed++
			mu.Unlock()
			return
		}
		if !updateItem(item.ID, disposition.ItemStatus, string(res.Status), "", res.Message) {
			return
		}
		batch.EmitNDJSON(map[string]interface{}{"event": disposition.Event, "batchId": run.ID, "itemId": item.ID, "jobId": job.ID, "status": string(res.Status)})
		if disposition.Success {
			mu.Lock()
			success++
			mu.Unlock()
		}
	}

	if concurrency == 1 {
		for _, item := range toRun {
			runOne(item)
		}
	} else {
		done := make(chan struct{}, len(toRun))
		for _, item := range toRun {
			sem <- struct{}{}
			go func(item db.BatchItem) {
				defer func() { <-sem; done <- struct{}{} }()
				runOne(item)
			}(item)
		}
		for range toRun {
			<-done
		}
	}

	// Recount from DB for accurate resume totals.
	items2, listErr := d.ListBatchItems(run.ID)
	if listErr != nil {
		return listErr
	}
	counts := batch.RecountPublishBatchItems(items2)
	success, failed = counts.Success, counts.Failed
	if counts.WaitingHuman {
		if err := d.UpdateBatchRunWaitingHuman(run.ID, counts.Pending, 0, counts.Success, counts.Failed, 0); err != nil {
			return err
		}
	} else if err := d.UpdateBatchRunCounters(run.ID, counts.Pending, 0, counts.Success, counts.Failed, 0); err != nil {
		return err
	}
	mu.Lock()
	stateErr := firstStateErr
	mu.Unlock()
	if stateErr != nil {
		return fmt.Errorf("batch state persistence failed: %w", stateErr)
	}

	if counts.WaitingHuman {
		batch.EmitNDJSON(map[string]interface{}{
			"event": batch.PublishBatchSummaryEvent(counts), "batchId": run.ID,
			"waiting": counts.WaitingCount, "waiting_human": counts.WaitingCount,
			"pending": counts.Pending, "success": success, "failed": failed,
		})
		cmd.Printf("Batch publish #%d: success=%d failed=%d waiting_human=%d (dry_run=%v resume=%v)\n", run.ID, success, failed, counts.WaitingCount, dryRun, resume)
	} else {
		batch.EmitNDJSON(map[string]interface{}{
			"event": batch.PublishBatchSummaryEvent(counts), "batchId": run.ID, "success": success, "failed": failed,
		})
		cmd.Printf("Batch publish #%d: success=%d failed=%d (dry_run=%v resume=%v)\n", run.ID, success, failed, dryRun, resume)
	}
	return nil
}

type publishCSVRow struct {
	contentID int64
	targetID  int64
}

func parsePublishCSV(path string) ([]publishCSVRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var rows []publishCSVRow
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		low := strings.ToLower(line)
		if lineNo == 1 && (strings.Contains(low, "content") || strings.Contains(low, "target") || strings.Contains(low, "platform")) {
			continue
		}
		parts := strings.Split(line, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) < 2 {
			return nil, fmt.Errorf("line %d: need content_id,target_id (or content_id,platform,target_id)", lineNo)
		}
		var cID, tID int64
		if _, err := fmt.Sscanf(parts[0], "%d", &cID); err != nil || cID <= 0 {
			return nil, fmt.Errorf("line %d: bad content_id", lineNo)
		}
		// Schema A: content_id,target_id
		// Schema B: content_id,platform,target_id[,account_id]
		if len(parts) >= 3 {
			// if col1 is numeric, treat as target_id (schema A with extra cols); else platform (schema B)
			if _, err := fmt.Sscanf(parts[1], "%d", &tID); err == nil && tID > 0 {
				// schema A
			} else if _, err := fmt.Sscanf(parts[2], "%d", &tID); err != nil || tID <= 0 {
				return nil, fmt.Errorf("line %d: bad target_id", lineNo)
			}
		} else if _, err := fmt.Sscanf(parts[1], "%d", &tID); err != nil || tID <= 0 {
			return nil, fmt.Errorf("line %d: bad target_id", lineNo)
		}
		rows = append(rows, publishCSVRow{contentID: cID, targetID: tID})
	}
	return rows, sc.Err()
}

// resolvePublishCDPURL accepts only the target account's running Facade session.
func resolvePublishCDPURL(d *db.DB, accountID int64) (string, error) {
	dataDir, _ := config.DefaultConfig().ResolvedDataDir()
	facade := browser.NewFacadeFromConfig(d, config.DefaultConfig(), dataDir)
	defer facade.Shutdown()
	status, statusErr := facade.Status(accountID)
	return publishguard.LiveCDPURL(accountID, status, statusErr)
}

func resolvePublishCDPURLForPlatform(d *db.DB, accountID int64, platformCode string) (string, error) {
	publisher, err := platforms.ExecutionPublisher(platformCode)
	if err != nil {
		return "", err
	}
	if publisher.Strategy() == platforms.StrategyAPI {
		return "", nil
	}
	return resolvePublishCDPURL(d, accountID)
}

func runPublishResume(cmd *cobra.Command, jobID int64, dryRun, autoSubmit bool) error {
	d := openQuickDB()
	defer d.Close()

	job, err := d.GetPublishJob(jobID)
	if err != nil {
		return err
	}
	if !batch.JobIsResumable(job.Status) {
		return fmt.Errorf("job #%d status %q is not resumable (need manual_takeover or failed)", job.ID, job.Status)
	}
	storedDryRun, storedAutoSubmit := publish.IntentFromMode(job.Mode)
	if cmd.Flags().Changed("auto-submit") && autoSubmit != storedAutoSubmit {
		return fmt.Errorf("resume cannot change stored publish intent; create a new publish job")
	}
	if cmd.Flags().Changed("dry-run") && dryRun != storedDryRun {
		return fmt.Errorf("resume cannot change stored publish intent; create a new publish job")
	}
	dryRun, autoSubmit = storedDryRun, storedAutoSubmit

	var content *db.Content
	var asset *db.AccountAsset
	var mediaPath, publishAt, complianceJSON string
	var publishMedia []platforms.PublishMedia
	if job.PlanID > 0 {
		frozen, frozenErr := (&publishplan.Service{DB: d}).FrozenInputForJob(job)
		if frozenErr != nil {
			return frozenErr
		}
		content, asset = frozen.Content, frozen.Asset
		mediaPath, publishMedia, publishAt, complianceJSON = frozen.MediaPath, frozen.Media, frozen.PublishAt, frozen.ComplianceJSON
	} else {
		content, err = d.GetContent(job.ContentID)
		if err != nil {
			return err
		}
		asset, err = d.GetAccountAsset(job.AssetID)
		if err != nil {
			return err
		}
		mediaList, mediaErr := d.ListMediaByContent(content.ID)
		mediaPath, publishMedia, mediaErr = publishguard.PublishMediaPayload(mediaList, mediaErr, content.Type)
		if mediaErr != nil {
			return mediaErr
		}
	}
	if err := publishguard.ValidateTargetOwnership(d, asset, job.PlatformCode); err != nil {
		return err
	}
	cdpURL := ""
	if !dryRun {
		cdpURL, err = resolvePublishCDPURLForPlatform(d, asset.AccountID, job.PlatformCode)
		if err != nil {
			return err
		}
	}

	priorStatus := job.Status
	job, err = d.PreparePublishJobResume(job.ID, job.TraceID)
	if err != nil {
		return err
	}
	retry := job.RetryCount

	batch.EmitNDJSON(map[string]interface{}{
		"event": "job.resume", "jobId": job.ID, "retry": retry, "priorStatus": priorStatus,
		"dryRun": dryRun, "autoSubmit": autoSubmit,
	})
	cmd.Printf("Resuming publish job #%d (retry=%d prior=%s)\n", job.ID, retry, priorStatus)
	cmd.Printf("  Content:  #%d (%s)\n", content.ID, content.Type)
	cmd.Printf("  Target:   #%d (%s)\n", asset.ID, asset.Name)
	cmd.Printf("  Platform: %s\n", job.PlatformCode)
	cmd.Println()

	credRef := ""
	if ref, refErr := d.GetCredentialRef(asset.AccountID, job.PlatformCode); refErr == nil {
		credRef = ref
	}

	runner := &publish.Runner{Store: d}
	result, runErr := runner.Run(context.Background(), publish.RunInput{
		Job:            job,
		Content:        content,
		Asset:          asset,
		CDPURL:         cdpURL,
		DryRun:         dryRun,
		AutoSubmit:     autoSubmit,
		MediaPath:      mediaPath,
		Media:          publishMedia,
		EmitNDJSON:     true,
		CredentialRef:  credRef,
		PublishAt:      publishAt,
		ComplianceJSON: complianceJSON,
	})

	if result != nil {
		cmd.Printf("Result: %s (step=%s)\n", result.Status, result.Step)
		if result.Message != "" {
			cmd.Printf("  %s\n", result.Message)
		}
		if result.Result != nil && result.Result.Screenshot != "" {
			cmd.Printf("  Screenshot: %s\n", result.Result.Screenshot)
		}
	}
	return runErr
}

func runBatchRetry(cmd *cobra.Command, batchID int64, failedOnly bool) error {
	d := openQuickDB()
	defer d.Close()

	run, err := d.GetBatchRun(batchID)
	if err != nil {
		return err
	}

	items, err := d.ListBatchItems(batchID)
	if err != nil {
		return err
	}

	retryCount := 0
	for _, item := range items {
		if failedOnly && item.Status != "failed" {
			continue
		}
		if !failedOnly && item.Status != "failed" && item.Status != "cancelled" {
			continue
		}
		if err := d.UpdateBatchItemStatus(item.ID, "pending", "", "", ""); err != nil {
			return fmt.Errorf("queue batch item %d for retry: %w", item.ID, err)
		}
		retryCount++
	}

	if retryCount == 0 {
		cmd.Println("No items to retry.")
		return nil
	}

	if err := d.UpdateBatchRunCounters(run.ID, retryCount, 0, run.SuccessCount, 0, 0); err != nil {
		return err
	}
	cmd.Printf("Queued %d items for retry in batch #%d\n", retryCount, batchID)
	return nil
}

func runBatchResume(cmd *cobra.Command, batchID int64) error {
	d := openQuickDB()
	defer d.Close()

	run, err := d.GetBatchRun(batchID)
	if err != nil {
		return err
	}

	items, err := d.ListBatchItems(batchID)
	if err != nil {
		return err
	}

	pendingCount := 0
	for _, item := range items {
		if item.Status == "pending" || item.Status == "cancelled" || item.Status == "running" {
			pendingCount++
		}
	}

	if pendingCount == 0 {
		cmd.Println("No pending items to resume.")
		return nil
	}

	d.UpdateBatchRunCounters(run.ID, pendingCount, 0, run.SuccessCount, run.FailedCount, run.SkippedCount)
	cmd.Printf("Resumed batch #%d with %d pending items\n", batchID, pendingCount)
	return nil
}

func runBatchStatus(cmd *cobra.Command, batchID int64) error {
	d := openQuickDB()
	defer d.Close()

	run, err := d.GetBatchRun(batchID)
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

	items, err := d.ListBatchItems(batchID)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		cmd.Printf("\nItems:\n")
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tERROR")
		for _, item := range items {
			errStr := item.ErrorCode
			if errStr == "" {
				errStr = "-"
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", item.ID, item.ItemType, item.Status, errStr)
		}
		w.Flush()
	}
	return nil
}

// ---- Jobs / Records / Logs ----

func addJobsRecordsLogsCmds(root *cobra.Command) {
	jobsCmd := &cobra.Command{Use: "jobs", Short: "List publish jobs"}
	jobsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent publish jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			jobs, err := d.ListPublishJobs(50)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tCONTENT\tACCOUNT\tPLATFORM\tSTATUS\tERROR")
			for _, j := range jobs {
				errStr := j.ErrorCode
				if errStr == "" {
					errStr = "-"
				}
				fmt.Fprintf(w, "%d\t%d\t%d\t%s\t%s\t%s\n", j.ID, j.ContentID, j.AccountID, j.PlatformCode, j.Status, errStr)
			}
			w.Flush()
			if len(jobs) == 0 {
				cmd.Println("No jobs found.")
			}
			return nil
		},
	}
	jobsCmd.AddCommand(jobsListCmd)
	root.AddCommand(jobsCmd)

	recordsCmd := &cobra.Command{Use: "records", Short: "List publish records"}
	recordsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent publish records",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			records, err := d.ListPublishRecords(50)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tCONTENT\tPLATFORM\tSTATUS\tURL")
			for _, r := range records {
				fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\n", r.ID, r.ContentID, r.PlatformCode, r.Status, r.PostURL)
			}
			w.Flush()
			if len(records) == 0 {
				cmd.Println("No records found.")
			}
			return nil
		},
	}
	recordsCmd.AddCommand(recordsListCmd)
	root.AddCommand(recordsCmd)

	logsCmd := &cobra.Command{Use: "logs", Short: "View job or batch logs"}
	logsShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Show events for a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID, _ := cmd.Flags().GetInt64("job")
			if jobID == 0 {
				return fmt.Errorf("--job is required")
			}
			d := openQuickDB()
			defer d.Close()
			events, err := d.ListJobEventsByJob(jobID)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tLEVEL\tSTEP\tTYPE\tMESSAGE")
			for _, e := range events {
				msg := e.Message
				if len(msg) > 50 {
					msg = msg[:47] + "..."
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", e.ID, e.Level, e.Step, e.EventType, msg)
			}
			w.Flush()
			if len(events) == 0 {
				cmd.Printf("No events for job #%d.\n", jobID)
			}
			return nil
		},
	}
	logsShowCmd.Flags().Int64P("job", "j", 0, "Job ID")
	logsShowCmd.MarkFlagRequired("job")
	logsCmd.AddCommand(logsShowCmd)
	root.AddCommand(logsCmd)
}

// ---- Import ----

func addImportCmd(root *cobra.Command) {
	importCmd := &cobra.Command{Use: "import", Short: "Import data from external sources"}
	turbopushCmd := &cobra.Command{
		Use:   "turbopush",
		Short: "Import data from TurboPush data.db",
		RunE: func(cmd *cobra.Command, args []string) error {
			source, _ := cmd.Flags().GetString("source")
			if source == "" {
				return fmt.Errorf("--source is required")
			}
			return runImportTurboPush(cmd, source)
		},
	}
	turbopushCmd.Flags().StringP("source", "s", "", "Path to TurboPush data.db")
	turbopushCmd.MarkFlagRequired("source")
	importCmd.AddCommand(turbopushCmd)
	root.AddCommand(importCmd)
}

func runImportTurboPush(cmd *cobra.Command, sourcePath string) error {
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}

	d := openQuickDB()
	defer d.Close()

	dataDir, _ := config.DefaultConfig().ResolvedDataDir()

	cmd.Printf("Importing from %s...\n", sourcePath)
	result, err := turbopush.Import(sourcePath, d, dataDir)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	cmd.Printf("Import complete:\n")
	cmd.Printf("  Accounts:  %d\n", result.Accounts)
	cmd.Printf("  Settings:  %d\n", result.Settings)
	cmd.Printf("  Contents:  %d\n", result.Contents)
	cmd.Printf("  Records:   %d\n", result.Records)
	cmd.Printf("  Skipped:   %d\n", result.Skipped)
	if len(result.SkippedReasons) > 0 {
		cmd.Printf("  Skip reasons:\n")
		limit := len(result.SkippedReasons)
		if limit > 5 {
			limit = 5
		}
		for _, reason := range result.SkippedReasons[:limit] {
			cmd.Printf("    - %s\n", reason)
		}
		if len(result.SkippedReasons) > limit {
			cmd.Printf("    ... and %d more\n", len(result.SkippedReasons)-limit)
		}
	}
	if len(result.Errors) > 0 {
		cmd.Printf("  Errors:    %d\n", len(result.Errors))
		for _, e := range result.Errors[:5] {
			cmd.Printf("    - %s\n", e)
		}
		if len(result.Errors) > 5 {
			cmd.Printf("    ... and %d more\n", len(result.Errors)-5)
		}
	}
	return nil
}

// ---- Account additional commands ----

func addAccountDisableNoteCmd(root *cobra.Command) {
	accountCmd := findCommand(root, "account")
	if accountCmd == nil {
		return
	}

	disableCmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable an account and all of its publishing targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id == 0 {
				return fmt.Errorf("--id is required")
			}
			d := openQuickDB()
			defer d.Close()
			if err := d.SetAccountEnabled(id, false); err != nil {
				return err
			}
			cmd.Printf("Account #%d disabled. Its browser cannot start and its targets cannot publish.\n", id)
			return nil
		},
	}
	disableCmd.Flags().Int64P("id", "i", 0, "Account ID")
	disableCmd.MarkFlagRequired("id")

	enableCmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable an account and all of its publishing targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id == 0 {
				return fmt.Errorf("--id is required")
			}
			d := openQuickDB()
			defer d.Close()
			if err := d.SetAccountEnabled(id, true); err != nil {
				return err
			}
			cmd.Printf("Account #%d enabled. Its browser and targets are available again.\n", id)
			return nil
		},
	}
	enableCmd.Flags().Int64P("id", "i", 0, "Account ID")
	enableCmd.MarkFlagRequired("id")

	noteCmd := &cobra.Command{
		Use:   "note",
		Short: "Update account remark",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			remark, _ := cmd.Flags().GetString("remark")
			if id == 0 {
				return fmt.Errorf("--id is required")
			}
			d := openQuickDB()
			defer d.Close()
			_, err := d.GetAccount(id)
			if err != nil {
				return err
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if _, err := d.Exec("UPDATE accounts SET remark = ?, updated_at = ? WHERE id = ?", remark, now, id); err != nil {
				return err
			}
			cmd.Printf("Account #%d remark updated.\n", id)
			return nil
		},
	}
	noteCmd.Flags().Int64P("id", "i", 0, "Account ID")
	noteCmd.MarkFlagRequired("id")
	noteCmd.Flags().StringP("remark", "r", "", "New remark text")

	accountCmd.AddCommand(disableCmd, enableCmd, noteCmd)

	// Add delete command
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an account",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id == 0 {
				return fmt.Errorf("--id is required")
			}

			// Stop running browser via Facade before any database/profile deletion.
			cfg := config.DefaultConfig()
			if d, err := cmd.Flags().GetString("data-dir"); err == nil && d != "" {
				cfg.DataDir = d
			}
			facade, database, dataDir, err := openFacade(cfg)
			if err != nil {
				return err
			}
			defer database.Close()
			defer facade.Shutdown()
			if err := facade.StopAccount(id); err != nil {
				return err
			}
			if err := database.DeleteAccount(id); err != nil {
				return err
			}

			// Clean up local profile directory
			if dataDir != "" {
				profileDir := filepath.Join(dataDir, "profiles", ui.ProfileDirName(id))
				if err := os.RemoveAll(profileDir); err != nil {
					return fmt.Errorf("account deleted but local profile cleanup failed")
				}
			}

			cmd.Printf("Account #%d deleted.\n", id)
			return nil
		},
	}
	deleteCmd.Flags().Int64P("id", "i", 0, "Account ID")
	deleteCmd.MarkFlagRequired("id")
	accountCmd.AddCommand(deleteCmd)
}

// ---- Helpers ----

func openQuickDB() *db.DB {
	cfg := activeCfg
	if cfg.DataDir == "" {
		cfg = config.DefaultConfig()
	}
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		exitErr(err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		exitErr(err)
	}
	if err := d.Migrate(); err != nil {
		d.Close()
		exitErr(err)
	}
	return d
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func findCommand(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Use == name || c.Name() == name {
			return c
		}
	}
	return nil
}
