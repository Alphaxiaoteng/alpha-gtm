//go:build legacy_chromium

package browser

import (
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/platforms"
)

// Space represents an isolated browser context for a profile/task (best-scheme integration).
// This enables handoff/resume, tab snapshot control, and Agent ownership switching while respecting existing lease/CDP contracts.
type Space struct {
	ID           string `json:"id"`   // UUID v4
	Name         string `json:"name"` // User-visible name
	ProfileID    string `json:"profile_id"`
	Kind         string `json:"kind"`      // "default" | "task"
	Ownership    string `json:"ownership"` // "user" | "agent"
	State        string `json:"state"`     // "active" | "handoff" | "completed"
	ControlEpoch int    `json:"control_epoch"`
	CreatedAt    int64  `json:"created_at"`
	// future: lastTabTarget, snapshot refs, etc.
}

// SpaceManager manages multiple Spaces with handoff, tab isolation, and snapshot actions.
type SpaceManager struct {
	facade *BrowserFacade
	db     *db.DB
	// socket handler for NDJSON RPC (local only)
}

// DefaultMaxConcurrentBrowsers 与 CDP 端口池大小一致（15101–15600 → 500）。
const DefaultMaxConcurrentBrowsers = PortMax - PortMin + 1

// FacadeConfig 构造 BrowserFacade 的可选配置。
type FacadeConfig struct {
	BrowserPath           string // fingerprint-chromium / default binary
	EgoLitePath           string // optional ego-lite binary
	BrowserOSPath         string // optional BrowserOS binary
	DefaultEngine         string // chromium | ego-lite | browseros | fingerprint-chromium
	DataDir               string
	MaxConcurrentBrowsers int // 0 = DefaultMaxConcurrentBrowsers；超限拒绝（不排队）
	displayProvider       func() ([]DisplayBounds, error)
}

// StartOptions 控制 StartAccount 的 lease / focus 行为。
type StartOptions struct {
	HolderType LeaseHolderType
	HolderID   string
	// SkipFocus: true 时不尝试窗口置前（自动化批量场景）。
	SkipFocus bool
}

func (o StartOptions) shouldFocus() bool {
	if o.SkipFocus {
		return false
	}
	// Jobs and batches are automation. They may place and control windows, but
	// never activate them or steal the user's current keyboard focus.
	return o.HolderType != HolderJob && o.HolderType != HolderBatch
}

// AccountStatus 是单账号浏览器运行态快照。
type AccountStatus struct {
	AccountID  int64  `json:"account_id"`
	ProfileID  string `json:"profile_id"`
	Running    bool   `json:"running"`
	State      State  `json:"state"`
	PID        int    `json:"pid,omitempty"`
	CDPPort    int    `json:"cdp_port,omitempty"`
	CDPURL     string `json:"cdp_url,omitempty"`
	LeaseOwner string `json:"lease_owner,omitempty"`
}

// BrowserFacade 是 CLI / UI / batch 共用的唯一浏览器生命周期入口。
// 内部走 Supervisor + LeaseManager + PortAllocator，禁止第二套启动实现。
//
// 公开 API:
//   - StartAccount / StopAccount / Status / ListRunning / Focus
//   - AcquireLease / ReleaseLease
//   - LaunchConfigFromProfile
//   - Reconcile
//   - SetMaxConcurrentBrowsers / MaxConcurrentBrowsers
//
// 废弃路径: 直接调用 Supervisor.Open / BrowserManager.Start（请改走本 Facade）。
type BrowserFacade struct {
	sup *Supervisor
	db  *db.DB
	cfg FacadeConfig
}

// NewFacade 创建 BrowserFacade 并启动 Supervisor heartbeat。
func NewFacade(database *db.DB, cfg FacadeConfig) *BrowserFacade {
	if cfg.MaxConcurrentBrowsers <= 0 {
		cfg.MaxConcurrentBrowsers = DefaultMaxConcurrentBrowsers
	}
	if cfg.displayProvider == nil {
		cfg.displayProvider = GetAvailableDisplays
	}
	sup := NewSupervisor(database, cfg.BrowserPath, cfg.DataDir)
	return &BrowserFacade{
		sup: sup,
		db:  database,
		cfg: cfg,
	}
}

// NewFacadeFromConfig 从应用 Config 构造 Facade。
func NewFacadeFromConfig(database *db.DB, cfg config.Config, dataDir string) *BrowserFacade {
	elPath := cfg.EgoLitePath
	if elPath == "" {
		elPath = DetectEgoLite()
	}
	return NewFacade(database, FacadeConfig{
		BrowserPath:   cfg.BrowserPath,
		EgoLitePath:   elPath,
		DefaultEngine: cfg.BrowserEngine,
		DataDir:       dataDir,
	})
}

// Supervisor 返回内部 Supervisor（供 ArrangeWindows 等高级能力；业务启停请用 Facade）。
func (f *BrowserFacade) Supervisor() *Supervisor {
	return f.sup
}

// Shutdown 关闭全部实例与 heartbeat。
func (f *BrowserFacade) Shutdown() {
	f.sup.Shutdown()
}

// CreateSpace creates a new isolated Space for a profile. Uses existing lease/DB for profile association.
// This is the best-scheme entry point for agent execution control.
func (f *BrowserFacade) CreateSpace(name, profileID, kind, ownership string) (*Space, error) {
	if err := validateAccountIDForSpace(profileID); err != nil { // reuse validation
		return nil, err
	}
	// TODO: persist to DB (spaces table), start associated browser if needed, increment epoch
	s := &Space{
		ID:           fmt.Sprintf("sp_%d", time.Now().UnixNano()),
		Name:         name,
		ProfileID:    profileID,
		Kind:         kind,
		Ownership:    ownership,
		State:        "active",
		ControlEpoch: 1,
		CreatedAt:    time.Now().UnixMilli(),
	}
	// future: f.db.CreateSpace(s) + Supervisor integration
	return s, nil
}

// validateAccountIDForSpace simple reuse of existing validator (can be extended).
func validateAccountIDForSpace(profileID string) error {
	if profileID == "" {
		return fmt.Errorf("%s: profile_id must be provided", errors.INVALID_ACCOUNT_ID)
	}
	return nil
}

// SpaceControl provides handoff/resume and tab snapshot actions for a Space (NDJSON RPC ready).
// Uses local socket for safe, audited control switching.
type SpaceControl struct {
	facade     *BrowserFacade
	socketPath string
	// future: active spaces map, snapshot cache, event emitter
}

// NewSpaceControl creates a control instance for a Space (will be extended with socket handler).
func (f *BrowserFacade) NewSpaceControl(spaceID string) *SpaceControl {
	return &SpaceControl{
		facade:     f,
		socketPath: "/tmp/alpha-nexus-agent.sock", // local only, per protocol
	}
}

// Handoff transfers ownership to user (increment epoch, mark state).
// Enforces manual takeover per AGENTS.md contract.
func (sc *SpaceControl) Handoff(spaceID string) error {
	// TODO: epoch +1, DB update (add spaces table if missing), emit event, prevent pending ops
	// Current: basic mock success + manual takeover enforcement
	return fmt.Errorf("%s: handoff initiated - user takeover required", errors.MANUAL_TAKEOVER)
}

// Resume brings back to agent control.
func (sc *SpaceControl) Resume(spaceID string) error {
	// TODO: restore epoch, re-focus if needed, socket response
	return fmt.Errorf("%s: resume requested - user confirmation pending", errors.MANUAL_TAKEOVER)
}

// StartSocketServer starts the NDJSON socket server for SpaceControl (best-scheme local-only protocol).
// Called from main CLI / UI init. Supports concurrent Space handoff/resume.
func (f *BrowserFacade) StartSocketServer() error {
	// Minimal NDJSON socket handler for SpaceControl (local-only).
	// TODO: implement real listener, Space lookup, epoch validation, lease integration.
	// Uses local socket only, no public exposure.
	return fmt.Errorf("socket server stub - implement in follow-up")
}

// Detach stops local supervision without terminating account browsers. The
// durable runtime rows let the next backend process recover and focus them.
func (f *BrowserFacade) Detach() {
	if f != nil && f.sup != nil {
		f.sup.Detach()
	}
}

// SetBrowserPath 更新 fingerprint-chromium 二进制路径。
func (f *BrowserFacade) SetBrowserPath(path string) {
	f.cfg.BrowserPath = path
	f.sup.SetBrowserPath(path)
}

// SetEgoLitePath updates the dedicated ego-lite binary used by profiles
// whose engine is ego-lite. Existing profiles keep their own engine choice.
func (f *BrowserFacade) SetEgoLitePath(path string) {
	f.cfg.EgoLitePath = path
}

// SetBrowserOSPath updates the dedicated BrowserOS binary used by profiles
// whose engine is browseros. Existing profiles keep their own engine choice.
func (f *BrowserFacade) SetBrowserOSPath(path string) {
	f.cfg.BrowserOSPath = path
}

// SetDefaultEngine updates the engine used for profiles created after this
// setting is saved. Existing profiles remain their stored browser_type.
func (f *BrowserFacade) SetDefaultEngine(engine string) {
	f.cfg.DefaultEngine = NormalizeEngine(engine)
}

// SetMaxConcurrentBrowsers 设置并发上限；n<=0 时恢复默认端口池大小。
// 策略: 超限明确拒绝（MAX_CONCURRENT_BROWSERS），不排队。
func (f *BrowserFacade) SetMaxConcurrentBrowsers(n int) {
	if n <= 0 {
		n = DefaultMaxConcurrentBrowsers
	}
	f.cfg.MaxConcurrentBrowsers = n
}

// MaxConcurrentBrowsers 返回当前并发上限。
func (f *BrowserFacade) MaxConcurrentBrowsers() int {
	return f.cfg.MaxConcurrentBrowsers
}

// ProfileIDForAccount 账号 → 稳定 profile_id（acct-<id>）。
func ProfileIDForAccount(accountID int64) string {
	return fmt.Sprintf("acct-%d", accountID)
}

// validateAccountID 空 / 非正 account_id。
func validateAccountID(accountID int64) error {
	if accountID <= 0 {
		return fmt.Errorf("%s: account_id must be positive", errors.INVALID_ACCOUNT_ID)
	}
	return nil
}

// LaunchConfigFromProfile 从 DB browser_profiles + platforms 完整复原启动参数。
// 填充: 指纹、代理、login_url、preferred port、user-data-dir。
func (f *BrowserFacade) LaunchConfigFromProfile(accountID int64) (LaunchConfig, error) {
	if err := validateAccountID(accountID); err != nil {
		return LaunchConfig{}, err
	}

	account, err := f.db.GetAccount(accountID)
	if err != nil {
		return LaunchConfig{}, fmt.Errorf("%s: %w", errors.ACCOUNT_NOT_FOUND, err)
	}
	if !account.Enabled {
		return LaunchConfig{}, fmt.Errorf("%s: account %d is disabled", errors.ACCOUNT_DISABLED, accountID)
	}

	bp, err := f.db.GetBrowserProfileByAccount(accountID)
	if err != nil {
		return LaunchConfig{}, fmt.Errorf("%s: browser profile for account %d: %w", errors.PROFILE_NOT_FOUND, accountID, err)
	}

	profileDir := bp.ProfileDir
	if profileDir == "" {
		profileDir = filepath.Join(f.cfg.DataDir, "profiles", ProfileIDForAccount(accountID))
	}

	// 登录/管理页：优先 ManagerURL，其次 LoginURL
	loginURL := ""
	if plat, err := platforms.Find(account.PlatformCode); err == nil {
		if account.PlatformCode == "kuaishou" && account.LoginState != db.AccountAuthLoggedIn {
			loginURL = plat.LoginURL
		} else {
			loginURL = plat.ManagerURL
		}
		if loginURL == "" {
			loginURL = plat.LoginURL
		}
	}
	if loginURL == "" {
		// 尝试 DB platforms 表
		if p, err := f.db.GetPlatform(account.PlatformCode); err == nil {
			if account.PlatformCode == "kuaishou" && account.LoginState != db.AccountAuthLoggedIn {
				loginURL = p.LoginURL
			} else {
				loginURL = p.ManagerURL
			}
			if loginURL == "" {
				loginURL = p.LoginURL
			}
		}
	}

	proxyServer := f.resolveProxy(accountID, bp)

	engine := NormalizeEngine(bp.BrowserType)
	// ponytail: ego-lite was retired from the product UI. Keep old profile data
	// intact, but always launch it through the default Chromium engine.
	if engine == EngineEgoLite {
		engine = EngineChromium
	}
	if bp.BrowserType == "" {
		if de := NormalizeEngine(f.cfg.DefaultEngine); de != "" {
			engine = de
		}
	}

	binPath := f.cfg.BrowserPath
	switch engine {
	case EngineEgoLite:
		if f.cfg.EgoLitePath != "" {
			binPath = f.cfg.EgoLitePath
		} else if strings.Contains(strings.ToLower(f.cfg.BrowserPath), "ego") {
			binPath = f.cfg.BrowserPath
		} else {
			binPath = ""
		}
	case EngineBrowserOS:
		if f.cfg.BrowserOSPath != "" {
			binPath = f.cfg.BrowserOSPath
		} else if strings.Contains(strings.ToLower(f.cfg.BrowserPath), "browseros") {
			binPath = f.cfg.BrowserPath
		} else {
			binPath = ""
		}
	}

	cfg := LaunchConfig{
		ProfileID:       ProfileIDForAccount(accountID),
		AccountID:       accountID,
		UserDataDir:     profileDir,
		FingerprintSeed: bp.FingerprintSeed,
		Lang:            bp.Lang,
		AcceptLang:      bp.AcceptLang,
		Timezone:        bp.Timezone,
		Engine:          engine,
		EgoLitePath:     f.cfg.EgoLitePath,
		BrowserOSPath:   f.cfg.BrowserOSPath,
		BinPath:         binPath,
		LoginURL:        loginURL,
		PreferredPort:   bp.LastCDPPort,
		ProxyServer:     proxyServer,
	}

	if bp.FingerprintConfig != "" {
		var fc struct {
			Platform        string `json:"platform"`
			PlatformVersion string `json:"platformVersion"`
			Brand           string `json:"brand"`
			BrandVersion    string `json:"brandVersion"`
			HWConcurrency   int    `json:"hardwareConcurrency"`
			GPUVendor       string `json:"gpuVendor"`
			GPURenderer     string `json:"gpuRenderer"`
		}
		if err := json.Unmarshal([]byte(bp.FingerprintConfig), &fc); err == nil {
			cfg.Platform = fc.Platform
			cfg.PlatformVersion = fc.PlatformVersion
			cfg.Brand = fc.Brand
			cfg.BrandVersion = fc.BrandVersion
			cfg.HWConcurrency = fc.HWConcurrency
			cfg.GPUVendor = fc.GPUVendor
			cfg.GPURenderer = fc.GPURenderer
		}
	}

	return cfg, nil
}

// resolveProxy 只消费代理池绑定后写入 profile 的快照。没有绑定即直连。
func (f *BrowserFacade) resolveProxy(_ int64, bp *db.BrowserProfile) string {
	if bp == nil {
		return ""
	}
	return strings.TrimSpace(bp.ProxyServer)
}

// StartAccount 启动账号浏览器：LaunchConfigFromProfile → 并发校验 → Supervisor.Open → Focus。
// login_url 通过 LaunchConfig 作为启动 URL 传入（导航意图）；Focus 失败只记录操作事件。
func (f *BrowserFacade) StartAccount(accountID int64, opts StartOptions) (*Instance, error) {
	if err := validateAccountID(accountID); err != nil {
		return nil, err
	}

	// 启动前对齐死进程，避免假占端口/并发名额
	f.Reconcile()

	cfg, err := f.LaunchConfigFromProfile(accountID)
	if err != nil {
		return nil, err
	}

	shouldFocus := opts.shouldFocus()
	cfg.Background = opts.SkipFocus

	// 已在跑则直接返回（显式人工入口的幂等成功路径附带 Focus）
	if inst, ok := f.sup.Get(cfg.ProfileID); ok {
		if inst.State != StateCrashed && inst.State != StateStopped {
			if shouldFocus {
				_ = f.sup.Focus(cfg.ProfileID)
			}
			return inst, fmt.Errorf("%s: account %d profile %s", errors.ALREADY_RUNNING, accountID, cfg.ProfileID)
		}
	}
	// After a backend restart the browser remains alive by design, but it is no
	// longer present in this process's in-memory Supervisor map. Return the
	// durable runtime snapshot as the same idempotent ALREADY_RUNNING success so
	// UI callers can focus/open it without attempting a duplicate profile launch.
	if status, statusErr := f.Status(accountID); statusErr == nil && status.Running &&
		status.State != StateStopping && status.State != StateStopped && status.State != StateCrashed {
		if shouldFocus {
			_ = f.Focus(accountID)
		}
		return &Instance{
			ProfileID: status.ProfileID, AccountID: status.AccountID,
			PID: status.PID, CDPPort: status.CDPPort, CDPURL: status.CDPURL,
			State: status.State, LastSeenAt: time.Now().UTC(), LeaseOwner: status.LeaseOwner,
		}, fmt.Errorf("%s: account %d profile %s", errors.ALREADY_RUNNING, accountID, cfg.ProfileID)
	}

	running := f.sup.CountRunning()
	if running >= f.cfg.MaxConcurrentBrowsers {
		return nil, fmt.Errorf("%s: limit %d (running %d)", errors.MAX_CONCURRENT_BROWSERS, f.cfg.MaxConcurrentBrowsers, running)
	}

	holderType := opts.HolderType
	if holderType == "" {
		holderType = HolderCLI
	}
	holderID := opts.HolderID
	if holderID == "" {
		holderID = fmt.Sprintf("facade-pid-%d", os.Getpid())
	}
	cfg.HolderType = holderType
	cfg.HolderID = holderID
	if displays, displayErr := f.cfg.displayProvider(); displayErr == nil {
		if display, ok := preferredSecondaryDisplay(displays); ok {
			cfg.WindowBounds = &display
		}
	}
	if cfg.BinPath == "" {
		cfg.BinPath = f.cfg.BrowserPath
	}

	inst, err := f.sup.Open(cfg)
	if err != nil {
		return nil, err
	}
	// Chromium can ignore a positional launch URL when it restores a fresh
	// profile window, leaving a single about:blank tab. This is an explicit
	// account launch, so replacing that sole blank tab is safe; existing pages
	// are deliberately untouched by navigateFreshBlankPage.
	if cfg.LoginURL != "" {
		if _, navigationErr := navigateFreshBlankPage(inst.CDPURL, cfg.LoginURL); navigationErr != nil {
			_ = f.StopAccount(accountID)
			return nil, fmt.Errorf("%s: open platform page: %w", errors.CDP_NOT_READY, navigationErr)
		}
	}

	// Focus is a convenience operation. Its failure must not make a healthy CDP
	// session look unavailable or turn a logged-in account into a false warning.
	if shouldFocus {
		_ = f.sup.Focus(cfg.ProfileID)
	}

	return inst, nil
}

// StopAccount 停止账号浏览器。未知/未运行/重复 stop 均幂等成功。
func (f *BrowserFacade) StopAccount(accountID int64) error {
	if err := validateAccountID(accountID); err != nil {
		return err
	}
	profileID := ProfileIDForAccount(accountID)

	// 未在本进程运行：清可能残留的 runtime/lease（幂等）
	if _, ok := f.sup.Get(profileID); !ok {
		// 尝试按 DB runtime 杀外部 PID（若存在且存活）
		var pid, cdpPort int
		var cdpURL, leaseOwner, startedAt string
		err := f.db.QueryRow(
			`SELECT pid, cdp_port, cdp_url, COALESCE(lease_owner,''), started_at FROM browser_runtime_instances WHERE profile_id = ?`,
			profileID,
		).Scan(&pid, &cdpPort, &cdpURL, &leaseOwner, &startedAt)
		if err == sql.ErrNoRows {
			// Unknown/repeated stop is an exact no-op. An unknown lease owner is
			// left to TTL cleanup instead of deleting another starter's lease.
			return nil
		}
		if err != nil {
			return fmt.Errorf("read browser runtime before stop: %w", err)
		}
		if err := f.sup.reserveRuntimeStop(profileID, pid, startedAt, leaseOwner); err != nil {
			return err
		}
		signal := func(sig syscall.Signal) error {
			if groupErr := syscall.Kill(-pid, sig); groupErr != nil {
				if pidErr := syscall.Kill(pid, sig); pidErr != nil && pidErr != syscall.ESRCH {
					return pidErr
				}
			}
			return nil
		}
		waitExit := func(timeout time.Duration) bool {
			deadline := time.Now().Add(timeout)
			for pidAlive(pid) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			return !pidAlive(pid)
		}
		if pidAlive(pid) {
			gracefullyClosed := false
			if err := gracefulCloseCDP(cdpURL); err == nil {
				gracefullyClosed = waitExit(2 * time.Second)
			}
			if !gracefullyClosed {
				if err := signal(syscall.SIGTERM); err != nil {
					return fmt.Errorf("stop browser process %d: %w", pid, err)
				}
				if !waitExit(5 * time.Second) {
					if err := signal(syscall.SIGKILL); err != nil {
						return fmt.Errorf("kill browser process %d: %w", pid, err)
					}
					if !waitExit(2 * time.Second) {
						return fmt.Errorf("browser process %d did not exit after SIGKILL", pid)
					}
				}
			}
		}
		removed, removeErr := f.sup.removeRuntimeInstance(profileID, pid, startedAt, leaseOwner)
		if removeErr != nil {
			return fmt.Errorf("remove stopped browser runtime: %w", removeErr)
		}
		if !removed {
			var currentPID int
			lookupErr := f.db.QueryRow(`SELECT pid FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&currentPID)
			if lookupErr == nil {
				return fmt.Errorf("%s: browser runtime changed to pid %d while stopping profile %s", errors.PROFILE_LOCKED, currentPID, profileID)
			}
			if lookupErr != sql.ErrNoRows {
				return fmt.Errorf("verify stopped browser runtime: %w", lookupErr)
			}
		}
		f.sup.logEvent(profileID, browserEventStoppedByUser, "", pid, cdpPort)
		if removed && leaseOwner != "" {
			if err := f.sup.leases.Release(profileID, leaseOwner); err != nil {
				return fmt.Errorf("release stopped browser lease: %w", err)
			}
		}
		f.sup.writeRuntimeJSON()
		return nil
	}

	if err := f.sup.Close(profileID); err != nil {
		if errors.Is(err.Error(), errors.PROFILE_NOT_FOUND) {
			return nil
		}
		return err
	}
	return nil
}

// Status 查询账号浏览器状态。
func (f *BrowserFacade) Status(accountID int64) (*AccountStatus, error) {
	if err := validateAccountID(accountID); err != nil {
		return nil, err
	}
	// 可选：账号必须存在
	if _, err := f.db.GetAccount(accountID); err != nil {
		return nil, fmt.Errorf("%s: %w", errors.ACCOUNT_NOT_FOUND, err)
	}

	profileID := ProfileIDForAccount(accountID)
	st := &AccountStatus{
		AccountID: accountID,
		ProfileID: profileID,
		Running:   false,
		State:     StateStopped,
	}

	if inst, ok := f.sup.Get(profileID); ok {
		if inst.AccountID != accountID {
			return nil, fmt.Errorf("browser runtime account mismatch")
		}
		st.Running = inst.State != StateStopped && inst.State != StateCrashed
		st.State = f.recoverHealthyRuntimeDegradation(inst.ProfileID, inst.PID, inst.CDPPort, runtimeStartedAt(inst), inst.LeaseOwner, inst.ActiveJobID != nil, inst.State, inst.CDPURL)
		st.PID = inst.PID
		st.CDPPort = inst.CDPPort
		st.CDPURL = inst.CDPURL
		st.LeaseOwner = inst.LeaseOwner
		return st, nil
	}

	// 外部 runtime 行
	var pid, port int
	var cdpURL, leaseOwner, state, startedAt string
	var activeJobID sql.NullInt64
	err := f.db.QueryRow(
		`SELECT pid, cdp_port, cdp_url, COALESCE(lease_owner,''), state, started_at, active_job_id
		 FROM browser_runtime_instances WHERE profile_id = ? AND account_id = ?`,
		profileID, accountID,
	).Scan(&pid, &port, &cdpURL, &leaseOwner, &state, &startedAt, &activeJobID)
	if err == nil && pidAlive(pid) {
		st.Running = true
		st.State = State(state)
		if st.State == "" {
			st.State = StateReady
		}
		st.PID = pid
		st.CDPPort = port
		st.CDPURL = cdpURL
		st.LeaseOwner = leaseOwner
		st.State = f.recoverHealthyRuntimeDegradation(profileID, pid, port, startedAt, leaseOwner, activeJobID.Valid, st.State, cdpURL)
	}
	return st, nil
}

func (f *BrowserFacade) recoverHealthyRuntimeDegradation(profileID string, pid, port int, startedAt, leaseOwner string, activeJob bool, state State, cdpURL string) State {
	if state != StateDegraded || activeJob || !cdpAlive(cdpURL) {
		return state
	}
	for _, cause := range []degradationCause{degradationCauseHeartbeat, degradationCauseFocus} {
		recovered, err := f.sup.recoverRuntimeDegradation(profileID, pid, port, startedAt, leaseOwner, cause)
		if err != nil {
			f.sup.logEvent(profileID, "runtime_persist_failed", err.Error(), pid, port)
			return state
		}
		state = recovered
	}
	return state
}

// ListRunning 列出本 Supervisor 管理的运行中实例。
func (f *BrowserFacade) ListRunning() []*Instance {
	f.Reconcile()
	return f.sup.List()
}

// Focus 将账号浏览器窗口置前。
func (f *BrowserFacade) Focus(accountID int64) error {
	if err := validateAccountID(accountID); err != nil {
		return err
	}
	profileID := ProfileIDForAccount(accountID)
	if _, ok := f.sup.Get(profileID); ok {
		if err := f.sup.Focus(profileID); err != nil {
			if !stderrors.Is(err, errNoCDPPageTarget) {
				return fmt.Errorf("%s: %w", errors.FOCUS_FAILED, err)
			}
			cfg, cfgErr := f.LaunchConfigFromProfile(accountID)
			if cfgErr != nil {
				return fmt.Errorf("%s: %w", errors.FOCUS_FAILED, cfgErr)
			}
			if err := f.sup.FocusWithInitialPage(profileID, cfg.LoginURL); err != nil {
				return fmt.Errorf("%s: %w", errors.FOCUS_FAILED, err)
			}
			return nil
		}
		return nil
	}

	// A backend upgrade must not require closing user-owned browsers. After a
	// restart, live processes remain represented by the durable runtime row even
	// though this Supervisor did not launch them.
	status, err := f.Status(accountID)
	if err != nil {
		return fmt.Errorf("%s: %w", errors.FOCUS_FAILED, err)
	}
	if !status.Running || status.PID <= 0 {
		return fmt.Errorf("%s: %s not running", errors.FOCUS_FAILED, profileID)
	}
	if err := ensureCDPWindowForFocus(status.CDPURL, ""); err != nil {
		f.sup.recordFocusOperationFailure(profileID, status.PID, status.CDPPort)
		if !stderrors.Is(err, errNoCDPPageTarget) {
			return fmt.Errorf("%s: %w", errors.FOCUS_FAILED, err)
		}
		cfg, cfgErr := f.LaunchConfigFromProfile(accountID)
		if cfgErr != nil {
			return fmt.Errorf("%s: %w", errors.FOCUS_FAILED, cfgErr)
		}
		if err := ensureCDPWindowForFocus(status.CDPURL, cfg.LoginURL); err != nil {
			return fmt.Errorf("%s: maximize browser window: %w", errors.FOCUS_FAILED, err)
		}
	}
	if err := FocusByPID(status.PID); err != nil {
		f.sup.recordFocusOperationFailure(profileID, status.PID, status.CDPPort)
		return fmt.Errorf("%s: %w", errors.FOCUS_FAILED, err)
	}
	var currentPID, currentPort int
	var startedAt, leaseOwner string
	if err := f.db.QueryRow(`SELECT pid, cdp_port, started_at, COALESCE(lease_owner,'')
		FROM browser_runtime_instances WHERE profile_id=?`, profileID).
		Scan(&currentPID, &currentPort, &startedAt, &leaseOwner); err == nil && currentPID == status.PID {
		if _, recoverErr := f.sup.recoverRuntimeDegradation(profileID, currentPID, currentPort, startedAt, leaseOwner, degradationCauseFocus); recoverErr != nil {
			f.sup.logEvent(profileID, "runtime_persist_failed", recoverErr.Error(), currentPID, currentPort)
		}
	}
	f.sup.recordFocusSucceeded(accountID, profileID, status.PID, status.CDPPort)
	return nil
}

// AcquireLease 为账号 profile 获取租约。
func (f *BrowserFacade) AcquireLease(accountID int64, holderType LeaseHolderType, holderID string, ttl time.Duration) error {
	if err := validateAccountID(accountID); err != nil {
		return err
	}
	if holderType == "" {
		holderType = HolderCLI
	}
	if holderID == "" {
		return fmt.Errorf("holderID required")
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return f.sup.leases.Acquire(ProfileIDForAccount(accountID), holderType, holderID, ttl)
}

// ReleaseLease 释放账号 profile 租约。
func (f *BrowserFacade) ReleaseLease(accountID int64, holderID string) error {
	if err := validateAccountID(accountID); err != nil {
		return err
	}
	return f.sup.leases.Release(ProfileIDForAccount(accountID), holderID)
}

// Reconcile 清理死 PID / 僵尸端口 / 过期 lease，对齐 runtime 表。
func (f *BrowserFacade) Reconcile() {
	f.sup.Reconcile()
}
