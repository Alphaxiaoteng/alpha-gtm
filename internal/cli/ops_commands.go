//go:build legacy_chromium

package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/batch"
	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/xhs"
)

// addOpsCmd: CDP background collect + auto publish orchestration.
func addOpsCmd(root *cobra.Command) {
	ops := &cobra.Command{
		Use:   "ops",
		Short: "Background CDP ops: data collect + auto publish",
		Long: `Orchestrates fingerprint browser via BrowserFacade + CDP scrapers.

Inspired by project/xhs push/collect workflow:
  1) start browser for account (optional --start)
  2) scrape creator dashboard over CDP
  3) save to data-dir/xhs_data/YYYY-MM-DD.json
  4) optional publish preparation

Examples:
  alpha-nexus ops collect --account-id 1 --start
  alpha-nexus ops collect-all --platform xhs --group brand-a --start
  alpha-nexus ops auto-publish -P facebook -c 1 -t 2 --account-id 3 --start --dry-run
`,
	}

	collect := &cobra.Command{
		Use:   "collect",
		Short: "Collect creator notes/metrics for one account via CDP",
		RunE: func(cmd *cobra.Command, args []string) error {
			accountID, _ := cmd.Flags().GetInt64("account-id")
			start, _ := cmd.Flags().GetBool("start")
			if accountID <= 0 {
				return fmt.Errorf("--account-id is required")
			}
			return runOpsCollect(cmd, accountID, start)
		},
	}
	collect.Flags().Int64("account-id", 0, "Account ID")
	collect.Flags().Bool("start", false, "Start browser via Facade if not running")
	_ = collect.MarkFlagRequired("account-id")

	collectAll := &cobra.Command{
		Use:   "collect-all",
		Short: "Collect all accounts (optional platform/group filter)",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			group, _ := cmd.Flags().GetString("group")
			start, _ := cmd.Flags().GetBool("start")
			if platform == "" {
				platform = "xhs"
			}
			return runOpsCollectAll(cmd, platform, group, start)
		},
	}
	collectAll.Flags().StringP("platform", "P", "xhs", "Platform code (default xhs)")
	collectAll.Flags().StringP("group", "g", "", "Filter account_group")
	collectAll.Flags().Bool("start", false, "Start browsers if needed")

	autoPub := &cobra.Command{
		Use:   "auto-publish",
		Short: "Start browser if needed, then prepare via CDP pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			contentID, _ := cmd.Flags().GetInt64("content")
			targetID, _ := cmd.Flags().GetInt64("target")
			accountID, _ := cmd.Flags().GetInt64("account-id")
			start, _ := cmd.Flags().GetBool("start")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			autoSubmit, _ := cmd.Flags().GetBool("auto-submit")
			idempotencyKey, _ := cmd.Flags().GetString("idempotency-key")
			if platform == "" || contentID == 0 || targetID == 0 {
				return fmt.Errorf("--platform, --content, --target required")
			}
			return runOpsAutoPublish(cmd, platform, contentID, targetID, accountID, start, dryRun, autoSubmit, idempotencyKey)
		},
	}
	autoPub.Flags().StringP("platform", "P", "", "Platform")
	autoPub.Flags().Int64P("content", "c", 0, "Content ID")
	autoPub.Flags().Int64P("target", "t", 0, "Target asset ID")
	autoPub.Flags().Int64("account-id", 0, "Account ID (default: asset.account_id)")
	autoPub.Flags().Bool("start", true, "Start browser if not running")
	autoPub.Flags().Bool("dry-run", false, "Dry-run publish")
	autoPub.Flags().Bool("auto-submit", false, "Direct ops rejects this; final click requires publish.plan.submit approval")
	autoPub.Flags().String("idempotency-key", "", "Stable request key (required for live publish)")
	_ = autoPub.MarkFlagRequired("platform")
	_ = autoPub.MarkFlagRequired("content")
	_ = autoPub.MarkFlagRequired("target")

	ops.AddCommand(collect, collectAll, autoPub)
	root.AddCommand(ops)
}

func openOpsFacade() (*browser.BrowserFacade, *db.DB, string, error) {
	cfg := activeCfg
	if cfg.DataDir == "" {
		cfg = config.DefaultConfig()
	}
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return nil, nil, "", err
	}
	d, err := db.Open(dataDir)
	if err != nil {
		return nil, nil, "", err
	}
	if err := d.Migrate(); err != nil {
		d.Close()
		return nil, nil, "", err
	}
	facade := browser.NewFacadeFromConfig(d, cfg, dataDir)
	return facade, d, dataDir, nil
}

func ensureBrowserRunning(cmd *cobra.Command, facade *browser.BrowserFacade, accountID int64, start bool) (cdpURL string, err error) {
	st, err := facade.Status(accountID)
	if err != nil {
		return "", err
	}
	if st.Running && (st.CDPURL != "" || st.CDPPort > 0) {
		if st.CDPURL != "" {
			return st.CDPURL, nil
		}
		return fmt.Sprintf("http://127.0.0.1:%d", st.CDPPort), nil
	}
	if !start {
		return "", fmt.Errorf("browser not running for account %d (pass --start)", accountID)
	}
	batch.EmitNDJSON(map[string]interface{}{"event": "browser.starting", "accountId": accountID})
	cmd.Printf("Starting browser for account #%d...\n", accountID)
	inst, err := facade.StartAccount(accountID, browser.StartOptions{
		HolderType: browser.HolderCLI,
		HolderID:   fmt.Sprintf("ops-%d", accountID),
		SkipFocus:  true,
	})
	if err != nil {
		// already running is ok
		st2, e2 := facade.Status(accountID)
		if e2 == nil && st2.Running {
			if st2.CDPURL != "" {
				return st2.CDPURL, nil
			}
			if st2.CDPPort > 0 {
				return fmt.Sprintf("http://127.0.0.1:%d", st2.CDPPort), nil
			}
		}
		return "", err
	}
	// brief wait for CDP
	time.Sleep(2 * time.Second)
	if inst != nil && inst.CDPURL != "" {
		return inst.CDPURL, nil
	}
	st3, _ := facade.Status(accountID)
	if st3 != nil && st3.CDPURL != "" {
		return st3.CDPURL, nil
	}
	if st3 != nil && st3.CDPPort > 0 {
		return fmt.Sprintf("http://127.0.0.1:%d", st3.CDPPort), nil
	}
	return "", fmt.Errorf("browser started but CDP URL unknown")
}

func runOpsCollect(cmd *cobra.Command, accountID int64, start bool) error {
	facade, d, dataDir, err := openOpsFacade()
	if err != nil {
		return err
	}
	defer d.Close()
	defer facade.Shutdown()

	acc, err := d.GetAccount(accountID)
	if err != nil {
		return err
	}
	if acc.PlatformCode != "xhs" {
		cmd.Printf("Warning: platform is %s; scraper targets XHS creator dashboard\n", acc.PlatformCode)
	}

	batch.EmitNDJSON(map[string]interface{}{"event": "collect.started", "accountId": accountID, "platform": acc.PlatformCode})
	cdpURL, err := ensureBrowserRunning(cmd, facade, accountID, start)
	if err != nil {
		batch.EmitNDJSON(map[string]interface{}{"event": "collect.failed", "accountId": accountID, "error": err.Error()})
		return err
	}
	cmd.Printf("CDP %s → scraping notes...\n", cdpURL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	data, err := xhs.ScrapeNotes(ctx, cdpURL)
	if err != nil {
		batch.EmitNDJSON(map[string]interface{}{"event": "collect.failed", "accountId": accountID, "error": err.Error()})
		return fmt.Errorf("scrape: %w", err)
	}
	// prefer DB account name when scrape name empty
	if data.Account == "" || data.Account == "unknown" {
		data.Account = acc.Name
	}

	xhsDir := filepath.Join(dataDir, "xhs_data")
	day := &xhs.DayData{Date: time.Now().Format("2006-01-02"), Accounts: []xhs.AccountData{*data}}
	if err := xhs.SaveToFile(day, xhsDir); err != nil {
		return err
	}
	_, _ = d.CreateAuditEvent("", "info", "collect", fmt.Sprintf("xhs notes=%d account=%s", data.NotesCount, data.Account), "account", accountID, "")

	batch.EmitNDJSON(map[string]interface{}{
		"event": "collect.success", "accountId": accountID,
		"notes": data.NotesCount, "name": data.Account, "file": filepath.Join(xhsDir, day.Date+".json"),
	})
	cmd.Printf("Collected %d notes for %q → %s/%s.json\n", data.NotesCount, data.Account, xhsDir, day.Date)
	for i, n := range data.Notes {
		if i >= 5 {
			break
		}
		cmd.Printf("  - %s  views=%s likes=%s\n", truncateRunes(n.Title, 36), n.ViewCount, n.LikeCount)
	}
	return nil
}

func runOpsCollectAll(cmd *cobra.Command, platform, group string, start bool) error {
	facade, d, dataDir, err := openOpsFacade()
	if err != nil {
		return err
	}
	defer d.Close()
	defer facade.Shutdown()

	var accounts []db.Account
	if group != "" {
		accounts, err = d.ListAccountsByGroup(group)
	} else {
		accounts, err = d.ListAccountsByPlatform(platform)
	}
	if err != nil {
		return err
	}
	var filtered []db.Account
	for _, a := range accounts {
		if platform != "" && a.PlatformCode != platform {
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) == 0 {
		cmd.Println("No matching accounts.")
		return nil
	}

	batch.EmitNDJSON(map[string]interface{}{"event": "collect_all.started", "total": len(filtered), "platform": platform, "group": group})
	ok, fail := 0, 0
	for _, a := range filtered {
		cmd.Printf("\n== account #%d %s ==\n", a.ID, a.Name)
		if err := runOpsCollectOne(cmd, facade, d, dataDir, a.ID, start); err != nil {
			cmd.Printf("FAIL: %v\n", err)
			fail++
			continue
		}
		ok++
	}
	batch.EmitNDJSON(map[string]interface{}{"event": "collect_all.finished", "success": ok, "failed": fail})
	cmd.Printf("\nCollect-all done: success=%d failed=%d\n", ok, fail)
	return nil
}

func runOpsCollectOne(cmd *cobra.Command, facade *browser.BrowserFacade, d *db.DB, dataDir string, accountID int64, start bool) error {
	acc, err := d.GetAccount(accountID)
	if err != nil {
		return err
	}
	cdpURL, err := ensureBrowserRunning(cmd, facade, accountID, start)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	data, err := xhs.ScrapeNotes(ctx, cdpURL)
	if err != nil {
		return err
	}
	if data.Account == "" || data.Account == "unknown" {
		data.Account = acc.Name
	}
	xhsDir := filepath.Join(dataDir, "xhs_data")
	day := &xhs.DayData{Date: time.Now().Format("2006-01-02"), Accounts: []xhs.AccountData{*data}}
	if err := xhs.SaveToFile(day, xhsDir); err != nil {
		return err
	}
	cmd.Printf("OK notes=%d file=%s\n", data.NotesCount, filepath.Join(xhsDir, day.Date+".json"))
	return nil
}

func runOpsAutoPublish(cmd *cobra.Command, platform string, contentID, targetID, accountID int64, start, dryRun, autoSubmit bool, idempotencyKey string) error {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !dryRun && idempotencyKey == "" {
		return fmt.Errorf("--idempotency-key is required for live publish")
	}
	readiness := platforms.Readiness(platform)
	if !readiness.CanCreateJob {
		return fmt.Errorf("PLATFORM_PUBLISH_UNAVAILABLE: platform %q has no publish path", platform)
	}
	if autoSubmit && !readiness.CanAttemptAutoSubmit {
		return fmt.Errorf("AUTO_SUBMIT_NOT_SUPPORTED: platform %q requires its declared manual/API flow", platform)
	}
	if autoSubmit {
		return fmt.Errorf("PUBLISH_FROZEN_SNAPSHOT_REQUIRED: real submit must use one approved single-target publish plan")
	}
	facade, d, dataDir, err := openOpsFacade()
	if err != nil {
		return err
	}
	defer d.Close()
	defer facade.Shutdown()

	asset, err := d.GetAccountAsset(targetID)
	if err != nil {
		return err
	}
	if err := publishguard.ValidateTargetOwnership(d, asset, platform); err != nil {
		return err
	}
	if accountID > 0 && accountID != asset.AccountID {
		return publishguard.ErrBrowserAccountMismatch
	}
	accountID = asset.AccountID
	content, err := d.GetContent(contentID)
	if err != nil {
		return err
	}
	mediaList, mediaErr := d.ListMediaByContent(content.ID)
	mediaPath, publishMedia, mediaErr := publishguard.PublishMediaPayload(mediaList, mediaErr, content.Type)
	if mediaErr != nil {
		return mediaErr
	}

	cdpURL := ""
	if !dryRun {
		_, err = ensureBrowserRunning(cmd, facade, accountID, start)
		if err != nil {
			return publishguard.ErrBrowserNotRunning
		}
		status, statusErr := facade.Status(asset.AccountID)
		cdpURL, statusErr = publishguard.LiveCDPURL(asset.AccountID, status, statusErr)
		if statusErr != nil {
			return statusErr
		}
		cmd.Printf("CDP ready: %s\n", cdpURL)
	}

	settingsJSON, err := publish.DefaultSettingsSnapshot(platform)
	if err != nil {
		return err
	}
	job, err := d.CreatePublishJobWithIntent(
		0, content.ID, asset.AccountID, asset.ID, platform, content.Type, idempotencyKey,
		publish.ExecutionMode(dryRun, autoSubmit), settingsJSON,
	)
	if err != nil {
		return err
	}
	batch.EmitNDJSON(map[string]interface{}{"event": "ops.auto_publish.started", "jobId": job.ID, "accountId": accountID, "dryRun": dryRun})

	runner := &publish.Runner{Store: d}
	res, runErr := runner.Run(context.Background(), publish.RunInput{
		Job: job, Content: content, Asset: asset,
		CDPURL: cdpURL, DryRun: dryRun, AutoSubmit: autoSubmit, EmitNDJSON: true,
		MediaPath: mediaPath, Media: publishMedia,
	})
	_ = dataDir
	if res != nil {
		cmd.Printf("Result: %s step=%s\n", res.Status, res.Step)
		if res.Message != "" {
			cmd.Printf("  %s\n", res.Message)
		}
	}
	if runErr != nil {
		return runErr
	}
	return nil
}
