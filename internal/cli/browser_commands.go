//go:build legacy_chromium

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/batch"
	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
)

// ---- Browser commands via BrowserFacade (T-B01) ----
//
// Canonical API: browser start|stop|status|list|focus with --account-id.
// --profile is a legacy alias that resolves to account_id when possible.
// All lifecycle ops go through browser.Facade; do not call BrowserManager
// or exec fingerprint-chromium directly from CLI.

// clashPortHash: same as server.go clashPort — stable account → port hash.
func clashPortHash(accountID int64, startPort int) int {
	h := uint64(accountID) * 2654435761
	h ^= h >> 16
	h *= 0x45d9f3b
	return startPort + int(h%1000)
}

// openFacade creates a short-lived BrowserFacade for a CLI command.
// Callers must Close the DB. Do NOT call Facade.Shutdown after start — that
// would kill browsers; CLI is short-lived and browsers outlive the process.
func openFacade(cfg config.Config) (*browser.BrowserFacade, *db.DB, string, error) {
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return nil, nil, "", fmt.Errorf("%s: %w", errors.INVALID_DATA_DIR, err)
	}
	database, err := db.Open(dataDir)
	if err != nil {
		return nil, nil, "", err
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		return nil, nil, "", fmt.Errorf("%s: %w", errors.DB_MIGRATION_FAILED, err)
	}
	facade := browser.NewFacadeFromConfig(database, cfg, dataDir)
	return facade, database, dataDir, nil
}

// resolveAccountID resolves --account-id or legacy --profile to a positive account ID.
func resolveAccountID(database *db.DB, accountID int64, profile string) (int64, error) {
	if accountID > 0 {
		if _, err := database.GetAccount(accountID); err != nil {
			return 0, fmt.Errorf("%s: account %d", errors.ACCOUNT_NOT_FOUND, accountID)
		}
		return accountID, nil
	}
	if profile == "" {
		return 0, fmt.Errorf("%s: --account-id is required (or legacy --profile)", errors.INVALID_ACCOUNT_ID)
	}

	// Numeric profile string → account id
	if n, err := strconv.ParseInt(profile, 10, 64); err == nil && n > 0 {
		if _, err := database.GetAccount(n); err == nil {
			return n, nil
		}
	}

	// acct-<id> form
	if strings.HasPrefix(profile, "acct-") {
		if n, err := strconv.ParseInt(strings.TrimPrefix(profile, "acct-"), 10, 64); err == nil && n > 0 {
			if _, err := database.GetAccount(n); err == nil {
				return n, nil
			}
		}
	}

	accounts, err := database.ListAccounts()
	if err != nil {
		return 0, err
	}
	for _, a := range accounts {
		bp, err := database.GetBrowserProfileByAccount(a.ID)
		if err != nil {
			continue
		}
		base := filepath.Base(bp.ProfileDir)
		if a.Name == profile || base == profile || bp.ProfileDir == profile {
			return a.ID, nil
		}
	}
	return 0, fmt.Errorf("%s: cannot resolve profile %q to account", errors.PROFILE_NOT_FOUND, profile)
}

// errorCodeOf extracts a stable errors package code from an error string ("CODE: detail").
func errorCodeOf(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	known := []errors.ErrorCode{
		errors.PROFILE_LOCKED,
		errors.NO_PORT,
		errors.NO_PORT_AVAILABLE,
		errors.BROWSER_CRASH,
		errors.CDP_CONNECT,
		errors.LEASE_EXPIRED,
		errors.BROWSER_NOT_FOUND,
		errors.PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1,
		errors.LOGIN_REQUIRED,
		errors.INVALID_DATA_DIR,
		errors.DB_MIGRATION_FAILED,
		errors.PLATFORM_NOT_FOUND,
		errors.PROFILE_NOT_FOUND,
		errors.PROFILE_BUSY,
		errors.BUSY_BY_JOB,
		errors.BUSY_BY_UI,
		errors.CDP_NOT_READY,
		errors.ALREADY_RUNNING,
		errors.ACCOUNT_NOT_FOUND,
		errors.ACCOUNT_DISABLED,
		errors.INVALID_ACCOUNT_ID,
		errors.MAX_CONCURRENT_BROWSERS,
		errors.FOCUS_FAILED,
	}
	for _, c := range known {
		if errors.Is(msg, c) {
			return string(c)
		}
	}
	if idx := strings.Index(msg, ":"); idx > 0 {
		tok := msg[:idx]
		if tok == strings.ToUpper(tok) && strings.Contains(tok, "_") {
			return tok
		}
	}
	return "INTERNAL"
}

func emitBrowserError(op string, accountID int64, err error) {
	batch.EmitNDJSON(map[string]interface{}{
		"event":     "browser.error",
		"op":        op,
		"accountId": accountID,
		"errorCode": errorCodeOf(err),
		"message":   err.Error(),
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})
}

func runBrowserStartFacade(cmd *cobra.Command, cfg config.Config, accountID int64, profile string) error {
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		emitBrowserError("start", accountID, err)
		return err
	}
	defer database.Close()

	id, err := resolveAccountID(database, accountID, profile)
	if err != nil {
		emitBrowserError("start", accountID, err)
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event":     "browser.starting",
		"accountId": id,
		"profileId": browser.ProfileIDForAccount(id),
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})

	holderID := fmt.Sprintf("cli-pid-%d", os.Getpid())
	inst, err := facade.StartAccount(id, browser.StartOptions{
		HolderType: browser.HolderCLI,
		HolderID:   holderID,
		// Browser-backed publish and inspection must not steal focus from the
		// user's active app. The account tab remains available to the Worker.
		SkipFocus: true,
	})
	if err != nil {
		// ALREADY_RUNNING still returns a usable instance — soft success
		if inst != nil && errors.Is(err.Error(), errors.ALREADY_RUNNING) {
			batch.EmitNDJSON(map[string]interface{}{
				"event":     "browser.already_running",
				"accountId": id,
				"profileId": inst.ProfileID,
				"cdpPort":   inst.CDPPort,
				"cdpUrl":    inst.CDPURL,
				"pid":       inst.PID,
				"state":     string(inst.State),
				"errorCode": string(errors.ALREADY_RUNNING),
				"ts":        time.Now().UTC().Format(time.RFC3339),
			})
			cmd.Printf("Browser already running for account #%d\n", id)
			cmd.Printf("  Profile:  %s\n", inst.ProfileID)
			cmd.Printf("  PID:      %d\n", inst.PID)
			cmd.Printf("  CDP URL:  %s\n", inst.CDPURL)
			cmd.Printf("  CDP Port: %d\n", inst.CDPPort)
			cmd.Printf("  State:    %s\n", inst.State)
			return nil
		}
		emitBrowserError("start", id, err)
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event":     "browser.started",
		"accountId": id,
		"profileId": inst.ProfileID,
		"cdpPort":   inst.CDPPort,
		"cdpUrl":    inst.CDPURL,
		"pid":       inst.PID,
		"state":     string(inst.State),
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})

	cmd.Printf("Browser started\n")
	cmd.Printf("  Account:  #%d\n", id)
	cmd.Printf("  Profile:  %s\n", inst.ProfileID)
	cmd.Printf("  PID:      %d\n", inst.PID)
	cmd.Printf("  CDP URL:  %s\n", inst.CDPURL)
	cmd.Printf("  CDP Port: %d\n", inst.CDPPort)
	cmd.Printf("  State:    %s\n", inst.State)
	return nil
}

func runBrowserStopFacade(cmd *cobra.Command, cfg config.Config, accountID int64, profile string, all bool) error {
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		emitBrowserError("stop", accountID, err)
		return err
	}
	defer database.Close()

	if all {
		// Stop every running account known to Facade / DB accounts.
		accounts, err := database.ListAccounts()
		if err != nil {
			return err
		}
		n := 0
		for _, a := range accounts {
			st, err := facade.Status(a.ID)
			if err != nil || st == nil || !st.Running {
				continue
			}
			if err := facade.StopAccount(a.ID); err != nil {
				emitBrowserError("stop", a.ID, err)
				continue
			}
			batch.EmitNDJSON(map[string]interface{}{
				"event":     "browser.stopped",
				"accountId": a.ID,
				"profileId": browser.ProfileIDForAccount(a.ID),
				"ts":        time.Now().UTC().Format(time.RFC3339),
			})
			n++
		}
		// Also close any in-process instances not covered
		for _, inst := range facade.ListRunning() {
			if inst.AccountID > 0 {
				_ = facade.StopAccount(inst.AccountID)
			}
		}
		cmd.Printf("Stopped %d browser instance(s)\n", n)
		return nil
	}

	id, err := resolveAccountID(database, accountID, profile)
	if err != nil {
		emitBrowserError("stop", accountID, err)
		return err
	}

	if err := facade.StopAccount(id); err != nil {
		emitBrowserError("stop", id, err)
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event":     "browser.stopped",
		"accountId": id,
		"profileId": browser.ProfileIDForAccount(id),
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})
	cmd.Printf("Browser stopped for account #%d\n", id)
	return nil
}

func runBrowserStatusFacade(cmd *cobra.Command, cfg config.Config, accountID int64, profile string) error {
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		emitBrowserError("status", accountID, err)
		return err
	}
	defer database.Close()

	// No selector: list all accounts' status as NDJSON lines + table
	if accountID <= 0 && profile == "" {
		accounts, err := database.ListAccounts()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "ACCOUNT\tPROFILE\tRUNNING\tSTATE\tPORT\tPID\tLEASE")
		for _, a := range accounts {
			st, err := facade.Status(a.ID)
			if err != nil {
				continue
			}
			batch.EmitNDJSON(map[string]interface{}{
				"event":      "browser.status",
				"accountId":  st.AccountID,
				"profileId":  st.ProfileID,
				"running":    st.Running,
				"state":      string(st.State),
				"cdpPort":    st.CDPPort,
				"cdpUrl":     st.CDPURL,
				"pid":        st.PID,
				"leaseOwner": st.LeaseOwner,
				"ts":         time.Now().UTC().Format(time.RFC3339),
			})
			fmt.Fprintf(w, "%d\t%s\t%v\t%s\t%d\t%d\t%s\n",
				st.AccountID, st.ProfileID, st.Running, st.State, st.CDPPort, st.PID, st.LeaseOwner)
		}
		w.Flush()
		if len(accounts) == 0 {
			cmd.Println("No accounts found.")
		}
		return nil
	}

	id, err := resolveAccountID(database, accountID, profile)
	if err != nil {
		emitBrowserError("status", accountID, err)
		return err
	}

	st, err := facade.Status(id)
	if err != nil {
		emitBrowserError("status", id, err)
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event":      "browser.status",
		"accountId":  st.AccountID,
		"profileId":  st.ProfileID,
		"running":    st.Running,
		"state":      string(st.State),
		"cdpPort":    st.CDPPort,
		"cdpUrl":     st.CDPURL,
		"pid":        st.PID,
		"leaseOwner": st.LeaseOwner,
		"ts":         time.Now().UTC().Format(time.RFC3339),
	})

	if st.Running {
		cmd.Printf("Browser is RUNNING\n")
		cmd.Printf("  Account:  #%d\n", st.AccountID)
		cmd.Printf("  Profile:  %s\n", st.ProfileID)
		cmd.Printf("  State:    %s\n", st.State)
		cmd.Printf("  PID:      %d\n", st.PID)
		cmd.Printf("  CDP URL:  %s\n", st.CDPURL)
		cmd.Printf("  CDP Port: %d\n", st.CDPPort)
		if st.LeaseOwner != "" {
			cmd.Printf("  Lease:    %s\n", st.LeaseOwner)
		}
	} else {
		cmd.Printf("Browser is NOT running\n")
		cmd.Printf("  Account:  #%d\n", st.AccountID)
		cmd.Printf("  Profile:  %s\n", st.ProfileID)
		cmd.Printf("  State:    %s\n", st.State)
	}
	return nil
}

func runBrowserListFacade(cmd *cobra.Command, cfg config.Config) error {
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		emitBrowserError("list", 0, err)
		return err
	}
	defer database.Close()

	// Prefer account Status (sees external runtime rows); merge with ListRunning.
	type row struct {
		AccountID  int64
		ProfileID  string
		Running    bool
		State      string
		CDPPort    int
		PID        int
		LeaseOwner string
	}
	byProfile := map[string]row{}

	accounts, err := database.ListAccounts()
	if err != nil {
		return err
	}
	for _, a := range accounts {
		st, err := facade.Status(a.ID)
		if err != nil || st == nil || !st.Running {
			continue
		}
		byProfile[st.ProfileID] = row{
			AccountID:  st.AccountID,
			ProfileID:  st.ProfileID,
			Running:    true,
			State:      string(st.State),
			CDPPort:    st.CDPPort,
			PID:        st.PID,
			LeaseOwner: st.LeaseOwner,
		}
	}
	for _, inst := range facade.ListRunning() {
		if _, ok := byProfile[inst.ProfileID]; ok {
			continue
		}
		if inst.State == browser.StateStopped || inst.State == browser.StateCrashed {
			continue
		}
		byProfile[inst.ProfileID] = row{
			AccountID:  inst.AccountID,
			ProfileID:  inst.ProfileID,
			Running:    true,
			State:      string(inst.State),
			CDPPort:    inst.CDPPort,
			PID:        inst.PID,
			LeaseOwner: inst.LeaseOwner,
		}
	}

	instances := make([]map[string]interface{}, 0, len(byProfile))
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tPROFILE\tSTATE\tPORT\tPID\tLEASE")
	for _, r := range byProfile {
		instances = append(instances, map[string]interface{}{
			"accountId":  r.AccountID,
			"profileId":  r.ProfileID,
			"state":      r.State,
			"cdpPort":    r.CDPPort,
			"pid":        r.PID,
			"leaseOwner": r.LeaseOwner,
		})
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%d\t%s\n",
			r.AccountID, r.ProfileID, r.State, r.CDPPort, r.PID, r.LeaseOwner)
	}
	w.Flush()

	batch.EmitNDJSON(map[string]interface{}{
		"event":     "browser.list",
		"count":     len(instances),
		"instances": instances,
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})

	if len(instances) == 0 {
		cmd.Println("No browser instances running.")
	}
	return nil
}

func runBrowserFocusFacade(cmd *cobra.Command, cfg config.Config, accountID int64, profile string) error {
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		emitBrowserError("focus", accountID, err)
		return err
	}
	defer database.Close()

	id, err := resolveAccountID(database, accountID, profile)
	if err != nil {
		emitBrowserError("focus", accountID, err)
		return err
	}

	if err := facade.Focus(id); err != nil {
		emitBrowserError("focus", id, err)
		return err
	}

	batch.EmitNDJSON(map[string]interface{}{
		"event":     "browser.focused",
		"accountId": id,
		"profileId": browser.ProfileIDForAccount(id),
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})
	cmd.Printf("Focused account #%d (%s)\n", id, browser.ProfileIDForAccount(id))
	return nil
}

// ---- Deprecated Supervisor-era helpers (open/close/health/tabs) ----
// open/close now delegate to Facade start/stop. health/tabs remain read-only.

// Deprecated: use browser start --account-id. Delegates to Facade.
func runBrowserOpen(cmd *cobra.Command, cfg config.Config, profileName string) error {
	cmd.Println("deprecated: prefer `browser start --account-id <id>` (open delegates to Facade)")
	return runBrowserStartFacade(cmd, cfg, 0, profileName)
}

// Deprecated: use browser stop --account-id. Delegates to Facade.
func runBrowserClose(cmd *cobra.Command, cfg config.Config, profileName string, all bool) error {
	cmd.Println("deprecated: prefer `browser stop --account-id <id>` (close delegates to Facade)")
	return runBrowserStopFacade(cmd, cfg, 0, profileName, all)
}

func runBrowserHealth(cmd *cobra.Command, cfg config.Config, profileName string) error {
	facade, database, _, err := openFacade(cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	id, err := resolveAccountID(database, 0, profileName)
	if err != nil {
		return err
	}
	st, err := facade.Status(id)
	if err != nil {
		return err
	}
	batch.EmitNDJSON(map[string]interface{}{
		"event":     "browser.health",
		"accountId": id,
		"profileId": st.ProfileID,
		"running":   st.Running,
		"state":     string(st.State),
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})
	cmd.Printf("Profile: %s (account #%d)\n", st.ProfileID, id)
	cmd.Printf("  Health: %s\n", st.State)
	cmd.Printf("  Running: %v\n", st.Running)
	return nil
}

func runBrowserTabs(cmd *cobra.Command, cfg config.Config) error {
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dataDir, "runtime.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cmd.Println("{\"updated_at\":\"\",\"instances\":[]}")
			return nil
		}
		return err
	}
	cmd.Print(string(data))
	return nil
}
