package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/sidecar"
	"alpha-nexus-egolite/internal/ui"
)

var (
	Version                     = "0.2.0"
	BuildDate                   = "unknown"
	GitCommit                   = "unknown"
	errSidecarDescriptorCleanup = stderrors.New("SIDECAR_DESCRIPTOR_CLEANUP_FAILED")
	// activeCfg is set in PersistentPreRun so openQuickDB respects --data-dir.
	activeCfg config.Config
)

func NewRootCmd() *cobra.Command {
	var cfg config.Config

	root := &cobra.Command{
		Use:   "alpha-nexus-egolite",
		Short: "Alpha-Nexus EgoLite local social media publishing tool",
		Long: `Alpha-Nexus EgoLite: local social media publishing through isolated ego Spaces.
CLI is the primary interface for AI/Agent task dispatching.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			activeCfg = cfg
			cmdName := cmd.Name()
			if cmdName == "init" || cmdName == "version" || cmdName == "help" {
				return nil
			}
			// Load persisted config from config.json
			dataDir, err := cfg.ResolvedDataDir()
			if err == nil {
				configPath := filepath.Join(dataDir, "config.json")
				if _, err := os.Stat(configPath); err == nil {
					if fileData, err := os.ReadFile(configPath); err == nil {
						var configData struct {
							EgoBrowserCommand   string `json:"ego_browser_command"`
							MaxConcurrentSpaces int    `json:"max_concurrent_spaces"`
						}
						if err := json.Unmarshal(fileData, &configData); err == nil {
							if cfg.EgoBrowserCommand == "" && configData.EgoBrowserCommand != "" {
								cfg.EgoBrowserCommand = configData.EgoBrowserCommand
							}
							if configData.MaxConcurrentSpaces > 0 {
								cfg.MaxConcurrentSpaces = configData.MaxConcurrentSpaces
							}
						}
					}
				}
			}
			if cfg.EgoBrowserCommand == "" {
				cfg.EgoBrowserCommand = "ego-browser"
			}
			activeCfg = cfg
			return nil
		},
	}

	root.PersistentFlags().StringVar(&cfg.DataDir, "data-dir", config.DefaultDataDir, "Data directory path")
	root.PersistentFlags().StringVar(&cfg.EgoBrowserCommand, "ego-browser-command", "ego-browser", "ego-browser CLI command")
	root.PersistentFlags().IntVar(&cfg.MaxConcurrentSpaces, "max-concurrent-spaces", 1, "Maximum concurrent ego Spaces")
	root.PersistentFlags().IntVar(&cfg.UIPort, "ui-port", config.DefaultUIPort, "Local UI port")

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize local data directory and SQLite schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, cfg)
		},
	}

	platformCmd := &cobra.Command{
		Use:   "platform",
		Short: "List and manage platforms",
	}
	platformListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all supported platforms",
		RunE: func(cmd *cobra.Command, args []string) error {
			runPlatformList(cmd)
			return nil
		},
	}
	platformCmd.AddCommand(platformListCmd)

	accountCmd := &cobra.Command{
		Use:   "account",
		Short: "Manage accounts",
	}
	accountAddCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new account",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			name, _ := cmd.Flags().GetString("name")
			remark, _ := cmd.Flags().GetString("remark")
			group, _ := cmd.Flags().GetString("group")
			identityID, _ := cmd.Flags().GetInt64("identity-id")
			if platform == "" {
				return fmt.Errorf("--platform is required")
			}
			if name == "" {
				name = platform + "-" + randomSuffix()
			}
			return runAccountAdd(cmd, cfg, identityID, platform, name, remark, group)
		},
	}
	accountAddCmd.Flags().StringP("platform", "P", "", "Built-in platform code (see `platform list`)")
	accountAddCmd.Flags().StringP("name", "n", "", "Account display name (auto-generated if omitted)")
	accountAddCmd.Flags().StringP("remark", "r", "", "Remark/note")
	accountAddCmd.Flags().StringP("group", "g", "", "Account group (brand/region/product)")
	accountAddCmd.Flags().Int64("identity-id", 1, "Logical Ego identity ID")
	accountAddCmd.MarkFlagRequired("platform")

	accountListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			group, _ := cmd.Flags().GetString("group")
			return runAccountList(cmd, cfg, platform, group)
		},
	}
	accountListCmd.Flags().StringP("platform", "P", "", "Filter by platform code")
	accountListCmd.Flags().StringP("group", "g", "", "Filter by account group")

	accountCheckCmd := &cobra.Command{
		Use:   "check",
		Short: "Read a sanitized snapshot from the account's Ego Space",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id == 0 {
				return fmt.Errorf("--id is required")
			}
			return runAccountCheck(cmd, cfg, id)
		},
	}
	accountCheckCmd.Flags().Int64P("id", "i", 0, "Account ID")
	accountCheckCmd.MarkFlagRequired("id")

	accountLoginHandoffCmd := &cobra.Command{
		Use:   "login-handoff",
		Short: "Open the account's Ego Space and hand it to the user for login",
		Long: `Create or reuse the account's isolated Ego Space, open the platform login URL,
and transfer control to the user. Passwords, cookies, QR codes and 2FA are never read.

Typical flow:
  alpha-nexus-egolite account add --identity-id 1 --platform xhs --name my-xhs
  alpha-nexus-egolite account login-handoff --id <ID>
  # complete login in the delegated EgoLite Space
  alpha-nexus-egolite account check --id <ID>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id == 0 {
				return fmt.Errorf("%s: --id is required", errors.INVALID_ACCOUNT_ID)
			}
			return runAccountLoginHandoff(cmd, cfg, id)
		},
	}
	accountLoginHandoffCmd.Flags().Int64P("id", "i", 0, "Account ID")
	accountLoginHandoffCmd.MarkFlagRequired("id")

	accountSetGroupCmd := &cobra.Command{
		Use:   "set-group",
		Short: "Set account group (brand/region/product)",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			group, _ := cmd.Flags().GetString("group")
			if id <= 0 {
				return fmt.Errorf("--id is required")
			}
			d, _, err := openDB(cmd, cfg)
			if err != nil {
				return err
			}
			defer d.Close()
			if _, err := d.GetAccount(id); err != nil {
				return err
			}
			if err := d.UpdateAccountGroup(id, group); err != nil {
				return err
			}
			cmd.Printf("Account #%d group set to %q\n", id, group)
			return nil
		},
	}
	accountSetGroupCmd.Flags().Int64P("id", "i", 0, "Account ID")
	accountSetGroupCmd.Flags().StringP("group", "g", "", "Group name (empty to clear)")
	_ = accountSetGroupCmd.MarkFlagRequired("id")

	accountCmd.AddCommand(accountAddCmd, accountListCmd, accountCheckCmd, accountLoginHandoffCmd, accountSetGroupCmd)

	uiCmd := &cobra.Command{
		Use:   "ui",
		Short: "Local web UI console",
	}
	uiServeCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start local web UI console",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUIServe(cmd, cfg)
		},
	}
	uiServeCmd.Flags().Int("port", cfg.UIPort, "UI port")
	uiServeCmd.Flags().String("sidecar-descriptor", "", "Private desktop sidecar descriptor path")
	uiCmd.AddCommand(uiServeCmd)

	root.AddCommand(initCmd)
	root.AddCommand(platformCmd)
	root.AddCommand(newEgoCommand(&cfg))
	root.AddCommand(newKnowledgeCommand(&cfg))
	root.AddCommand(accountCmd)
	root.AddCommand(uiCmd)

	addContentCmd(root)
	addStudioScheduleHandoffCmd(root)
	addTargetCmd(root)
	addEgoPublishCmd(root)
	addBatchCmd(root)
	addJobsRecordsLogsCmds(root)
	addImportCmd(root)
	addAccountDisableNoteCmd(root)
	addAgentPushCmds(root)
	addLLMCmds(root)
	addCodingCmds(root)
	addAuthCmd(root)
	addTikHubCmd(root)
	addRadarCmd(root)
	addFeishuCmd(root)
	addToolsCmd(root)
	addFeedbackCmd(root)
	addMobileCmd(root)

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show version and build information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("alpha-nexus-egolite v%s\n", Version)
			cmd.Printf("  commit:    %s\n", GitCommit)
			cmd.Printf("  built:     %s\n", BuildDate)
			cmd.Printf("  platform:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	})

	return root
}

func runInit(cmd *cobra.Command, cfg config.Config) error {
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return fmt.Errorf("%s: %w", errors.INVALID_DATA_DIR, err)
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	d, err := db.Open(dataDir)
	if err != nil {
		return err
	}
	defer d.Close()

	if err := d.Migrate(); err != nil {
		return fmt.Errorf("%s: %w", errors.DB_MIGRATION_FAILED, err)
	}

	mediaDir := dataDir + "/media"
	screenshotsDir := dataDir + "/screenshots"
	logsDir := dataDir + "/logs"

	for _, dir := range []string{mediaDir, screenshotsDir, logsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	cmd.Printf("Initialized Alpha-Nexus EgoLite at %s\n", dataDir)
	cmd.Printf("  Database:     %s/app.db\n", dataDir)
	cmd.Printf("  Media:        %s\n", mediaDir)
	cmd.Printf("  Screenshots:  %s\n", screenshotsDir)
	cmd.Printf("  Logs:         %s\n", logsDir)
	return nil
}

func runPlatformList(cmd *cobra.Command) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CODE\tNAME\tPUBLISH")
	for _, p := range platforms.EgoLiteAll() {
		publish := "no"
		if p.SupportsPublish {
			publish = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Code, p.Name, publish)
	}
	w.Flush()
}

func openDB(cmd *cobra.Command, cfg config.Config) (*db.DB, string, error) {
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", errors.INVALID_DATA_DIR, err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		return nil, "", err
	}
	if err := d.Migrate(); err != nil {
		d.Close()
		return nil, "", fmt.Errorf("%s: %w", errors.DB_MIGRATION_FAILED, err)
	}
	return d, dataDir, nil
}

// runBrowserStart / runBrowserStatus / runBrowserStop (BrowserManager) removed —
// use runBrowser*Facade in browser_commands.go (T-B01).

func runAccountAdd(cmd *cobra.Command, cfg config.Config, identityID int64, platformCode, name, remark, group string) error {
	platformAllowed := false
	for _, platform := range platforms.EgoLiteAll() {
		if platform.Code == platformCode {
			platformAllowed = true
			break
		}
	}
	if !platformAllowed {
		return fmt.Errorf("%s: platform %q is not available in EgoLite", errors.PLATFORM_NOT_FOUND, platformCode)
	}

	d, _, err := openDB(cmd, cfg)
	if err != nil {
		return err
	}
	defer d.Close()

	if identityID <= 0 {
		return fmt.Errorf("INVALID_IDENTITY_ID")
	}
	if _, err := d.GetEgoIdentity(identityID); err != nil {
		return fmt.Errorf("EGO_IDENTITY_NOT_FOUND")
	}
	account, err := d.CreateAccountForIdentity(identityID, platformCode, name, remark)
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	if group != "" {
		if err := d.UpdateAccountGroup(account.ID, group); err != nil {
			return fmt.Errorf("set group: %w", err)
		}
		account.AccountGroup = group
	}

	cmd.Printf("Account created\n")
	cmd.Printf("  ID:       %d\n", account.ID)
	cmd.Printf("  Identity: %d\n", account.IdentityID)
	cmd.Printf("  Platform: %s\n", account.PlatformCode)
	cmd.Printf("  Name:     %s\n", account.Name)
	cmd.Printf("  State:    %s\n", account.LoginState)
	if group != "" {
		cmd.Printf("  Group:    %s\n", group)
	}
	if remark != "" {
		cmd.Printf("  Remark:   %s\n", remark)
	}
	return nil
}

func runAccountList(cmd *cobra.Command, cfg config.Config, platformFilter, groupFilter string) error {
	d, _, err := openDB(cmd, cfg)
	if err != nil {
		return err
	}
	defer d.Close()

	var accounts []db.Account
	switch {
	case groupFilter != "" && platformFilter != "":
		all, e := d.ListAccountsByGroup(groupFilter)
		if e != nil {
			return e
		}
		for _, a := range all {
			if a.PlatformCode == platformFilter {
				accounts = append(accounts, a)
			}
		}
	case groupFilter != "":
		accounts, err = d.ListAccountsByGroup(groupFilter)
	case platformFilter != "":
		accounts, err = d.ListAccountsByPlatform(platformFilter)
	default:
		accounts, err = d.ListAccounts()
	}
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tPLATFORM\tGROUP\tNAME\tSTATE\tACTIVE\tREMARK")
	for _, a := range accounts {
		remark := a.Remark
		if len(remark) > 20 {
			remark = remark[:17] + "..."
		}
		g := a.AccountGroup
		if g == "" {
			g = "-"
		}
		active := "yes"
		if !a.Enabled {
			active = "no"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.PlatformCode, g, a.Name, a.LoginState, active, remark)
	}
	w.Flush()
	if len(accounts) == 0 {
		cmd.Println("No accounts found. Use `alpha-nexus account add --platform <code>` to add one.")
	}
	return nil
}

func runAccountCheck(cmd *cobra.Command, cfg config.Config, id int64) error {
	d, _, err := openDB(cmd, cfg)
	if err != nil {
		return err
	}
	defer d.Close()

	account, err := d.GetAccount(id)
	if err != nil {
		return err
	}
	observation, err := egolite.NewSpaceService(
		d,
		egolite.NewRuntime(cfg.EgoBrowserCommand, 0),
	).ObserveAccount(cmd.Context(), account.ID)
	if err != nil {
		return fmt.Errorf("EGO_ACCOUNT_CHECK_FAILED: %w", err)
	}

	cmd.Printf("Account #%d\n", account.ID)
	cmd.Printf("  Platform: %s\n", account.PlatformCode)
	cmd.Printf("  Name:     %s\n", account.Name)
	cmd.Printf("  State:    %s\n", account.LoginState)
	if account.LoginTime != "" {
		cmd.Printf("  Last login: %s\n", account.LoginTime)
	}
	if account.LastCheckAt != "" {
		cmd.Printf("  Last check: %s\n", account.LastCheckAt)
	}
	if account.Remark != "" {
		cmd.Printf("  Remark:   %s\n", account.Remark)
	}
	cmd.Printf("  Space:    %d\n", observation.SpaceID)
	cmd.Printf("  Snapshot: %t (%d bytes)\n", observation.SnapshotAvailable, observation.SnapshotLength)
	cmd.Printf("  Evidence: %s\n", observation.EvidenceCode)
	cmd.Println("  Verified: false (creator identity requires reviewed platform evidence)")
	return nil
}

func runUIServe(cmd *cobra.Command, cfg config.Config) (returnErr error) {
	applyUIServePort(cmd, &cfg)
	if err := validateUIServeDataDir(cfg); err != nil {
		return err
	}
	descriptorPath, _ := cmd.Flags().GetString("sidecar-descriptor")
	var instanceLock *sidecar.InstanceLock
	var err error
	if descriptorPath != "" {
		instanceLock, err = sidecar.AcquireInstanceLock(descriptorPath)
		if err != nil {
			return err
		}
		defer instanceLock.Close()
	}
	server, err := ui.NewServer(cfg)
	if err != nil {
		return err
	}
	defer server.Close()

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.UIPort))
	if err != nil {
		return err
	}
	serveOptions := ui.ServeOptions{Version: Version}
	if descriptorPath != "" {
		serveOptions.TransportToken, err = sidecar.NewSecret()
		if err != nil {
			_ = listener.Close()
			return err
		}
		serveOptions.InstanceNonce, err = sidecar.NewSecret()
		if err != nil {
			_ = listener.Close()
			return err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		descriptor := sidecar.Descriptor{
			SchemaVersion: sidecar.SchemaVersion,
			PID:           os.Getpid(),
			Port:          port,
			StartedAt:     time.Now().UTC(),
			Version:       Version,
			Auth:          serveOptions.TransportToken,
			Nonce:         serveOptions.InstanceNonce,
		}
		if err := sidecar.WriteDescriptor(descriptorPath, descriptor); err != nil {
			_ = listener.Close()
			return err
		}
		defer func() {
			returnErr = joinSidecarDescriptorCleanup(
				returnErr,
				sidecar.RemoveDescriptor(descriptorPath, serveOptions.InstanceNonce),
			)
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener, serveOptions) }()

	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
		cmd.Println("\nUI server stopped.")
		return nil
	}
}

func validateUIServeDataDir(cfg config.Config) error {
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return err
	}
	marker, err := os.Stat(filepath.Join(dataDir, "prepare-dev.json"))
	if err == nil && marker.Mode().IsRegular() {
		return fmt.Errorf("UI_DATA_DIR_IS_BUILD_CACHE: refusing to use the development artifact cache as the SQLite source")
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect UI data directory: %w", err)
	}
	return nil
}

func joinSidecarDescriptorCleanup(runErr, cleanupErr error) error {
	if cleanupErr == nil {
		return runErr
	}
	classified := sidecarDescriptorCleanupError{cause: cleanupErr}
	return stderrors.Join(runErr, classified)
}

type sidecarDescriptorCleanupError struct {
	cause error
}

func (sidecarDescriptorCleanupError) Error() string {
	return errSidecarDescriptorCleanup.Error()
}

func (e sidecarDescriptorCleanupError) Unwrap() []error {
	return []error{errSidecarDescriptorCleanup, e.cause}
}

func applyUIServePort(cmd *cobra.Command, cfg *config.Config) {
	if !cmd.Flags().Changed("port") {
		return
	}
	port, _ := cmd.Flags().GetInt("port")
	cfg.UIPort = port
}

func randomSuffix() string {
	rand.Seed(time.Now().UnixNano())
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
