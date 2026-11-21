//go:build legacy_chromium

package browser

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
)

// State 枚举浏览器实例状态。
type State string

const (
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateBusy     State = "busy"
	StateDegraded State = "degraded"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed"
	StateStopped  State = "stopped"
)

const (
	browserEventStoppedByUser        = "stopped_by_user"
	browserEventProcessExited        = "process_exited"
	browserEventProcessMissing       = "process_missing_after_restart"
	browserEventCrashed              = "crashed"
	browserEventSupervisorDetached   = "supervisor_detached"
	browserEventSupervisorShutdown   = "supervisor_shutdown"
	browserEventFocusFailed          = "focus_failed"
	browserEventFocusRecovered       = "focus_recovered"
	browserEventFocusOperationFailed = "focus_operation_failed"
	browserEventFocusSucceeded       = "focus_succeeded"
	browserEventHeartbeatFailed      = "heartbeat_failed"
	browserEventHeartbeatHealthy     = "heartbeat_recovered"
	browserEventLeaseLost            = "lease_lost"
	browserEventLeaseRecovered       = "lease_recovered"
	browserEventDegradedUnknown      = "degraded_unknown"
)

const (
	browserReasonUserStop                   = "USER_STOP"
	browserReasonProcessExitClean           = "PROCESS_EXIT_CLEAN"
	browserReasonProcessExitNonzero         = "PROCESS_EXIT_NONZERO"
	browserReasonProcessSignaled            = "PROCESS_SIGNALED"
	browserReasonProcessMissingAfterRestart = "PROCESS_MISSING_AFTER_RESTART"
	browserReasonSupervisorDetached         = "SUPERVISOR_DETACHED"
	browserReasonSupervisorShutdown         = "SUPERVISOR_SHUTDOWN"
	browserReasonLeaseLost                  = "LEASE_LOST"
	browserReasonWaitResultUnknown          = "WAIT_RESULT_UNKNOWN"
)

type degradationCause string

const (
	degradationCauseFocus     degradationCause = "focus"
	degradationCauseHeartbeat degradationCause = "heartbeat"
	degradationCauseLease     degradationCause = "lease"
	degradationCauseUnknown   degradationCause = "unknown"
)

// Instance 代表一个运行中的浏览器实例。
type Instance struct {
	ProfileID   string
	AccountID   int64
	PID         int
	CDPPort     int
	CDPURL      string
	State       State
	StartedAt   time.Time
	LastSeenAt  time.Time
	ActiveJobID *int64
	LeaseOwner  string
	cmd         *exec.Cmd
	cancel      context.CancelFunc // 用于 cancel cmd context
	done        chan struct{}      // closed by the single cmd.Wait owner
	stopEvent   string             // stable terminal cause selected before signaling
	exitDetail  string             // stable exit code/signal only; never raw stderr
}

// LaunchConfig 包含启动浏览器所需的全部参数。
type LaunchConfig struct {
	ProfileID       string
	AccountID       int64
	UserDataDir     string
	FingerprintSeed int
	Platform        string
	PlatformVersion string
	Brand           string
	BrandVersion    string
	HWConcurrency   int
	GPUVendor       string
	GPURenderer     string
	Lang            string
	AcceptLang      string
	Timezone        string
	BinPath         string
	// Engine: chromium | ego-lite | browseros | fingerprint-chromium
	Engine        string
	EgoLitePath   string // optional override for ego-lite binary
	BrowserOSPath string // optional override for BrowserOS binary
	LoginURL      string
	PreferredPort int // 优先使用的端口，0 则自动分配
	ProxyServer   string
	WindowBounds  *DisplayBounds
	// Background keeps a non-activated automation window responsive without
	// changing focus or sharing the profile with a second browser instance.
	Background bool
	// HolderType / HolderID: lease 持有者；空则默认 HolderCLI + supervisor-pid-<pid>
	HolderType LeaseHolderType
	HolderID   string
}

func sanitizedBrowserEnvironment(environ []string) []string {
	// Browsers need the normal process environment (PATH/HOME/locale), but never
	// application credentials. Keep this deliberately name-based and broad; add
	// an explicit safe allowlist only if a browser runtime later proves it needs
	// an environment variable containing one of these credential markers.
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		upper := strings.ToUpper(strings.TrimSpace(name))
		if !ok || upper == "" {
			continue
		}
		if strings.Contains(upper, "KEY") ||
			strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "CREDENTIAL") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func (c LaunchConfig) args(cdpPort int) []string {
	engine := NormalizeEngine(c.Engine)
	args := SecurityBaseArgs(cdpPort, c.UserDataDir)
	args = append(args, "--start-maximized")
	if c.Background {
		args = append(args,
			"--disable-background-timer-throttling",
			"--disable-renderer-backgrounding",
			"--disable-backgrounding-occluded-windows",
		)
	}
	if bounds := c.WindowBounds; bounds != nil && bounds.Width > 0 && bounds.Height > 0 {
		args = append(args,
			fmt.Sprintf("--window-position=%d,%d", bounds.X, bounds.Y),
			fmt.Sprintf("--window-size=%d,%d", bounds.Width, bounds.Height),
		)
	}

	// fingerprint-chromium only: per-account seed + platform spoof
	if EngineSupportsFingerprintFlags(engine) {
		if c.FingerprintSeed > 0 {
			args = append(args, fmt.Sprintf("--fingerprint=%d", c.FingerprintSeed))
		}
		if c.Platform != "" {
			args = append(args, fmt.Sprintf("--fingerprint-platform=%s", c.Platform))
		}
		if c.PlatformVersion != "" {
			args = append(args, fmt.Sprintf("--fingerprint-platform-version=%s", c.PlatformVersion))
		}
		if c.Brand != "" {
			args = append(args, fmt.Sprintf("--fingerprint-brand=%s", c.Brand))
		}
		if c.BrandVersion != "" {
			args = append(args, fmt.Sprintf("--fingerprint-brand-version=%s", c.BrandVersion))
		}
		if c.HWConcurrency > 0 {
			args = append(args, fmt.Sprintf("--fingerprint-hardware-concurrency=%d", c.HWConcurrency))
		}
		if c.GPUVendor != "" {
			args = append(args, fmt.Sprintf("--fingerprint-gpu-vendor=%s", c.GPUVendor))
		}
		if c.GPURenderer != "" {
			args = append(args, fmt.Sprintf("--fingerprint-gpu-renderer=%s", c.GPURenderer))
		}
	}

	lang := c.Lang
	if lang == "" {
		lang = "en-US"
	}
	args = append(args, fmt.Sprintf("--lang=%s", lang))
	acceptLang := c.AcceptLang
	if acceptLang == "" {
		acceptLang = "en-US,en"
	}
	args = append(args, fmt.Sprintf("--accept-lang=%s", acceptLang))
	tz := c.Timezone
	if tz == "" {
		tz = "America/Los_Angeles"
	}
	// fingerprint-chromium understands --timezone=; ego-lite/Chromium use env TZ primarily
	if EngineSupportsFingerprintFlags(engine) {
		args = append(args, fmt.Sprintf("--timezone=%s", tz))
	}

	if c.ProxyServer != "" {
		args = append(args, fmt.Sprintf("--proxy-server=%s", c.ProxyServer))
	}
	if c.LoginURL != "" {
		args = append(args, c.LoginURL)
	}
	return args
}

// RuntimeSnapshot 用于序列化 runtime.json 供 UI 轮询。
type RuntimeSnapshot struct {
	UpdatedAt string            `json:"updated_at"`
	Instances []InstanceSummary `json:"instances"`
}

// InstanceSummary 是 Instance 的 JSON 友好版本。
type InstanceSummary struct {
	ProfileID   string `json:"profile_id"`
	AccountID   int64  `json:"account_id"`
	Platform    string `json:"platform,omitempty"`
	State       string `json:"state"`
	CDPPort     int    `json:"cdp_port"`
	CDPURL      string `json:"cdp_url"`
	PID         int    `json:"pid"`
	StartedAt   string `json:"started_at"`
	LastSeenAt  string `json:"last_seen_at"`
	LeaseOwner  string `json:"lease_owner,omitempty"`
	ActiveJobID *int64 `json:"active_job_id,omitempty"`
}

// Supervisor 管理多个浏览器实例的生命周期。
// ponytail: 从 ui/server.go 的 processes map 逻辑提取而来，不是从零构建。
// 对外统一入口请用 BrowserFacade；Supervisor 保留为 Facade 内部实现。
type Supervisor struct {
	mu          sync.Mutex
	instances   map[string]*Instance // profileID -> Instance
	db          *db.DB
	ports       *PortAllocator
	leases      *LeaseManager
	browserPath string
	dataDir     string
	ctx         context.Context
	cancel      context.CancelFunc
}

func newLaunchLeaseOwner(holderID string) (string, error) {
	generation := make([]byte, 16)
	if _, err := rand.Read(generation); err != nil {
		return "", fmt.Errorf("generate browser lease owner: %w", err)
	}
	return holderID + ":launch:" + hex.EncodeToString(generation), nil
}

func runtimeStartedAt(inst *Instance) string {
	return inst.StartedAt.UTC().Format(time.RFC3339Nano)
}

// NewSupervisor 创建 Supervisor 并启动 heartbeat loop。
func NewSupervisor(database *db.DB, browserPath, dataDir string) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Supervisor{
		instances:   make(map[string]*Instance),
		db:          database,
		ports:       NewPortAllocator(database),
		leases:      NewLeaseManager(database),
		browserPath: browserPath,
		dataDir:     dataDir,
		ctx:         ctx,
		cancel:      cancel,
	}
	// 启动时从 SQLite 恢复已崩溃的实例（清理 stale 记录）
	s.cleanStaleInstances()
	go s.heartbeatLoop(ctx)
	return s
}

// Open 启动一个浏览器实例。
// Deprecated path for external callers: prefer BrowserFacade.StartAccount.
func (s *Supervisor) Open(cfg LaunchConfig) (*Instance, error) {
	s.mu.Lock()
	if existing, ok := s.instances[cfg.ProfileID]; ok {
		s.mu.Unlock()
		if existing.State != StateCrashed && existing.State != StateStopped {
			return existing, fmt.Errorf("%s: profile %s", errors.ALREADY_RUNNING, cfg.ProfileID)
		}
		// crashed/stopped 的清理掉重启
		s.cleanInstance(cfg.ProfileID)
		s.mu.Lock()
	}
	s.mu.Unlock()
	if err := s.ensureRuntimeLaunchAvailable(cfg.ProfileID); err != nil {
		return nil, err
	}

	engine := NormalizeEngine(cfg.Engine)
	binPath, err := ResolveBinary(engine, cfg.BinPath, cfg.EgoLitePath, cfg.BrowserOSPath, s.browserPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errors.BROWSER_NOT_FOUND, err)
	}

	holderType := cfg.HolderType
	if holderType == "" {
		holderType = HolderCLI
	}
	holderID := cfg.HolderID
	if holderID == "" {
		holderID = fmt.Sprintf("supervisor-pid-%d", os.Getpid())
	}
	leaseOwner, err := newLaunchLeaseOwner(holderID)
	if err != nil {
		return nil, err
	}

	// Every launch gets a unique lease generation. Reusing the caller's logical
	// holder ID would make concurrent starts look like a harmless renewal and
	// let delayed cleanup release a replacement browser's lease.
	if err := s.leases.Acquire(cfg.ProfileID, holderType, leaseOwner, 30*time.Second); err != nil {
		return nil, err
	}

	// Reserve keeps the chosen port exclusive across application processes until
	// the starting runtime row is durable. The runtime row then becomes the
	// long-lived ownership record, so release is safe on every return path.
	portReservation, err := s.ports.Reserve(cfg.ProfileID, cfg.PreferredPort)
	if err != nil {
		s.leases.Release(cfg.ProfileID, leaseOwner)
		return nil, err
	}
	defer func() { _ = portReservation.Release() }()
	port := portReservation.Port

	cdpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// profile 目录：0700 隔离敏感 cookie/local storage
	if err := os.MkdirAll(cfg.UserDataDir, 0o700); err != nil {
		s.leases.Release(cfg.ProfileID, leaseOwner)
		return nil, fmt.Errorf("%s: create profile dir: %w", errors.INVALID_DATA_DIR, err)
	}
	_ = os.Chmod(cfg.UserDataDir, 0o700)

	// 启动进程
	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, binPath, cfg.args(port)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = sanitizedBrowserEnvironment(os.Environ())
	if !EngineSupportsFingerprintFlags(engine) && cfg.Timezone != "" {
		cmd.Env = append(cmd.Env, "TZ="+cfg.Timezone)
	}

	// 记事件：starting
	s.logEvent(cfg.ProfileID, "started", fmt.Sprintf("Starting engine=%s bin=%s port=%d", engine, filepath.Base(binPath), port), 0, port)

	if err := cmd.Start(); err != nil {
		cmdCancel()
		s.leases.Release(cfg.ProfileID, leaseOwner)
		return nil, fmt.Errorf("%s: start browser: %w", errors.BROWSER_NOT_FOUND, err)
	}

	now := time.Now().UTC()
	inst := &Instance{
		ProfileID:  cfg.ProfileID,
		AccountID:  cfg.AccountID,
		PID:        cmd.Process.Pid,
		CDPPort:    port,
		CDPURL:     cdpURL,
		State:      StateStarting,
		StartedAt:  now,
		LastSeenAt: now,
		LeaseOwner: leaseOwner,
		cmd:        cmd,
		cancel:     cmdCancel,
		done:       make(chan struct{}),
	}

	s.mu.Lock()
	s.instances[cfg.ProfileID] = inst
	s.mu.Unlock()

	// Exactly one goroutine owns cmd.Wait for the process lifetime. Close only
	// signals the process and waits on inst.done; it never calls Wait again.
	go func() {
		waitErr := cmd.Wait()
		s.finishProcessExit(inst, waitErr)
	}()

	// Persist the starting identity before waiting for CDP so every exit path
	// has an exact runtime row to remove before releasing the profile lease.
	if persistErr := s.upsertRuntimeInstance(inst); persistErr != nil {
		closeErr := s.closeInstance(inst)
		if closeErr == nil {
			// The failed INSERT may have left no runtime row for closeInstance to
			// match. The generation token makes this direct release ABA-safe.
			_ = s.leases.Release(cfg.ProfileID, inst.LeaseOwner)
			return nil, fmt.Errorf("persist starting browser runtime: %w", persistErr)
		}
		return nil, fmt.Errorf("persist starting browser runtime: %w; stop failed: %v", persistErr, closeErr)
	}
	select {
	case <-inst.done:
		_ = s.closeInstance(inst)
		return nil, fmt.Errorf("%s: browser process exited during runtime registration", errors.BROWSER_CRASH)
	default:
	}

	// 等待 CDP ready，但子进程已退出时必须立即返回，不能继续等待并误报 CDP 超时。
	if err := waitCDPReadyOrExit(cdpURL, 30*time.Second, inst.done); err != nil {
		select {
		case <-inst.done:
			s.mu.Lock()
			exitDetail := inst.exitDetail
			s.mu.Unlock()
			if exitDetail != "" {
				return nil, fmt.Errorf("%s: browser process exited before CDP was ready (%s)", errors.BROWSER_CRASH, exitDetail)
			}
			return nil, fmt.Errorf("%s: browser process exited before CDP was ready", errors.BROWSER_CRASH)
		default:
		}
		// CDP 超时，kill 进程并清理
		if reserveErr := s.reserveRuntimeStop(cfg.ProfileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner); reserveErr != nil {
			return nil, fmt.Errorf("%s: %w; stop reservation failed: %v", errors.CDP_NOT_READY, err, reserveErr)
		}
		if closeErr := s.closeInstance(inst); closeErr != nil {
			return nil, fmt.Errorf("%s: %w; stop failed: %v", errors.CDP_NOT_READY, err, closeErr)
		}
		return nil, fmt.Errorf("%s: %w", errors.CDP_NOT_READY, err)
	}

	inst.State = StateReady
	inst.LastSeenAt = time.Now().UTC()
	// The launch flag handles the normal path. CDP enforces the native window
	// state for Chromium builds that restore an older profile's saved bounds.
	_ = maximizeCDPWindow(cdpURL)

	// 写 SQLite runtime 表；若 ready 状态无法持久化，立即结束该代进程，
	// 绝不返回一个无法 Stop/Claim/审计的游离浏览器。
	if persistErr := s.upsertRuntimeInstance(inst); persistErr != nil {
		closeErr := s.closeInstance(inst)
		if closeErr == nil {
			_ = s.leases.Release(cfg.ProfileID, inst.LeaseOwner)
			return nil, fmt.Errorf("persist ready browser runtime: %w", persistErr)
		}
		return nil, fmt.Errorf("persist ready browser runtime: %w; stop failed: %v", persistErr, closeErr)
	}
	// 更新 browser_profiles.last_cdp_port
	s.db.Exec(`UPDATE browser_profiles SET last_cdp_port = ?, last_cdp_url = ?, last_pid = ?, updated_at = ? WHERE account_id = ?`,
		port, cdpURL, inst.PID, time.Now().UTC().Format(time.RFC3339), cfg.AccountID)

	// 写 runtime.json
	s.writeRuntimeJSON()

	// 记事件：ready
	s.logEvent(cfg.ProfileID, "ready", fmt.Sprintf("CDP ready pid=%d port=%d", inst.PID, port), inst.PID, port)

	return inst, nil
}

// Close 停止指定 profile 的浏览器实例。
func (s *Supervisor) Close(profileID string) error {
	s.mu.Lock()
	inst, ok := s.instances[profileID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("%s: %s not running", errors.PROFILE_NOT_FOUND, profileID)
	}
	if err := s.reserveRuntimeStop(profileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner); err != nil {
		return err
	}
	s.setStopEvent(inst, browserEventStoppedByUser)
	if err := s.closeInstance(inst); err != nil {
		return err
	}
	// done acknowledges Wait before its SQLite cleanup. Persist the explicit
	// cause synchronously so callers can observe it when Close returns.
	s.logEvent(inst.ProfileID, browserEventStoppedByUser, "", inst.PID, inst.CDPPort)
	return nil
}

// Focus 将指定 profile 的浏览器窗口置前。
func (s *Supervisor) Focus(profileID string) error {
	return s.focus(profileID, "")
}

// FocusWithInitialPage is for an explicit user open/login request only. It
// creates the configured page only when this profile's browser has no pages.
func (s *Supervisor) FocusWithInitialPage(profileID, initialURL string) error {
	return s.focus(profileID, initialURL)
}

func (s *Supervisor) focus(profileID, initialURL string) error {
	s.mu.Lock()
	inst, ok := s.instances[profileID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("%s: %s not running", errors.PROFILE_NOT_FOUND, profileID)
	}

	// Maximize through the already-authorized local CDP session first. macOS may
	// deny or time out an Accessibility focus request; that must not leave the
	// browser at a small restored profile size.
	if err := ensureCDPWindowForFocus(inst.CDPURL, initialURL); err != nil {
		s.recordFocusOperationFailure(inst.ProfileID, inst.PID, inst.CDPPort)
		return fmt.Errorf("maximize browser window: %w", err)
	}
	if err := FocusByPID(inst.PID); err != nil {
		s.recordFocusOperationFailure(inst.ProfileID, inst.PID, inst.CDPPort)
		return err
	}
	if _, err := s.recoverRuntimeDegradation(inst.ProfileID, inst.PID, inst.CDPPort, runtimeStartedAt(inst), inst.LeaseOwner, degradationCauseFocus); err != nil {
		s.logEvent(inst.ProfileID, "runtime_persist_failed", err.Error(), inst.PID, inst.CDPPort)
	}
	s.recordFocusSucceeded(inst.AccountID, inst.ProfileID, inst.PID, inst.CDPPort)
	return nil
}

// recordFocusOperationFailure records a transient UI operation failure without
// changing durable browser health. A healthy CDP session remains usable even
// when macOS refuses a single attempt to bring its window to the foreground.
func (s *Supervisor) recordFocusOperationFailure(profileID string, pid, cdpPort int) {
	s.logEvent(profileID, browserEventFocusOperationFailed, "", pid, cdpPort)
}

// recordFocusSucceeded atomically records the last account switched to and its
// safe lifecycle event. It stores no page URL, profile path, or cookie data.
func (s *Supervisor) recordFocusSucceeded(accountID int64, profileID string, pid, cdpPort int) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err == nil {
		var result sql.Result
		result, err = tx.Exec(`UPDATE accounts SET last_focused_at=?, updated_at=? WHERE id=?`, now, now, accountID)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected != 1 {
				err = fmt.Errorf("account focus target not found")
			}
		}
		if err == nil {
			_, err = tx.Exec(`INSERT INTO browser_events
				(profile_id, event_type, message, reason_code, pid, cdp_port, created_at)
				VALUES (?, ?, '', '', ?, ?, ?)`,
				profileID, browserEventFocusSucceeded, pid, cdpPort, now)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
	if err != nil {
		s.logEvent(profileID, "runtime_persist_failed", "account focus state", pid, cdpPort)
	}
}

// Reconcile 对齐内存实例与 SQLite runtime：清理死 PID、僵尸端口与过期 lease。
func (s *Supervisor) Reconcile() {
	s.reconcileAll()
	s.reconcileDBOrphans()
	_ = s.leases.CleanExpired()
}

// CountRunning 返回当前仍存活（内存 + 外部 DB runtime 活 PID）的实例数。
func (s *Supervisor) CountRunning() int {
	s.mu.Lock()
	n := 0
	for _, inst := range s.instances {
		if inst.State != StateStopped && inst.State != StateCrashed {
			n++
		}
	}
	// profileIDs already in memory
	inMem := make(map[string]struct{}, len(s.instances))
	for id := range s.instances {
		inMem[id] = struct{}{}
	}
	s.mu.Unlock()

	rows, err := s.db.Query(`SELECT profile_id, pid FROM browser_runtime_instances`)
	if err != nil {
		return n
	}
	defer rows.Close()
	for rows.Next() {
		var profileID string
		var pid int
		if err := rows.Scan(&profileID, &pid); err != nil {
			continue
		}
		if _, ok := inMem[profileID]; ok {
			continue
		}
		if pidAlive(pid) {
			n++
		}
	}
	return n
}

// MarkDegraded 将运行中实例标记为 degraded（Focus/login 降级路径）。
func (s *Supervisor) MarkDegraded(profileID string, reason string) {
	s.mu.Lock()
	inst, ok := s.instances[profileID]
	s.mu.Unlock()
	if !ok || inst.State == StateStopped || inst.State == StateCrashed {
		return
	}
	cause := degradationCauseUnknown
	if errors.Is(reason, errors.FOCUS_FAILED) {
		cause = degradationCauseFocus
	}
	if err := s.markDegradedCause(inst, cause, reason); err != nil {
		// A failed cause event must never make the runtime look healthy.
		s.mu.Lock()
		if current, exists := s.instances[profileID]; exists && current.PID == inst.PID && current.StartedAt.Equal(inst.StartedAt) {
			current.State = StateDegraded
		}
		s.mu.Unlock()
		_ = s.persistRuntimeState(inst.ProfileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner, StateDegraded)
		s.logEvent(profileID, "runtime_persist_failed", err.Error(), inst.PID, inst.CDPPort)
	}
}

func degradationEventType(cause degradationCause, recovered bool) (string, bool) {
	switch cause {
	case degradationCauseFocus:
		if recovered {
			return browserEventFocusRecovered, true
		}
		return browserEventFocusFailed, true
	case degradationCauseHeartbeat:
		if recovered {
			return browserEventHeartbeatHealthy, true
		}
		return browserEventHeartbeatFailed, true
	case degradationCauseLease:
		if recovered {
			return browserEventLeaseRecovered, true
		}
		return browserEventLeaseLost, true
	case degradationCauseUnknown:
		if recovered {
			return "", false
		}
		return browserEventDegradedUnknown, true
	default:
		return "", false
	}
}

// activeDegradationCauses rebuilds cause state from paired stable events. This
// remains valid after a service restart, when a live runtime is no longer in
// this Supervisor's in-memory map.
func (s *Supervisor) activeDegradationCauses(profileID string, pid int, startedAt string) (map[degradationCause]bool, error) {
	started, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return nil, fmt.Errorf("parse browser runtime start: %w", err)
	}
	rows, err := s.db.Query(`SELECT event_type, COALESCE(message,'') FROM browser_events
		WHERE profile_id=? AND pid=? AND created_at>=? ORDER BY id`,
		profileID, pid, started.UTC().Truncate(time.Second).Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	causes := make(map[degradationCause]bool)
	for rows.Next() {
		var eventType, message string
		if err := rows.Scan(&eventType, &message); err != nil {
			return nil, err
		}
		switch eventType {
		case browserEventFocusFailed:
			causes[degradationCauseFocus] = true
		case browserEventFocusRecovered:
			delete(causes, degradationCauseFocus)
		case browserEventHeartbeatFailed:
			causes[degradationCauseHeartbeat] = true
		case browserEventHeartbeatHealthy:
			delete(causes, degradationCauseHeartbeat)
		case browserEventLeaseLost:
			causes[degradationCauseLease] = true
		case browserEventLeaseRecovered:
			delete(causes, degradationCauseLease)
		case browserEventDegradedUnknown:
			causes[degradationCauseUnknown] = true
		case "degraded":
			// Compatibility for pre-cause rows. The message remains local and is
			// never used for browser exit classification or returned to callers.
			if errors.Is(message, errors.FOCUS_FAILED) {
				causes[degradationCauseFocus] = true
			} else {
				causes[degradationCauseUnknown] = true
			}
		}
	}
	return causes, rows.Err()
}

func (s *Supervisor) markDegradedCause(inst *Instance, cause degradationCause, detail string) error {
	if inst == nil {
		return fmt.Errorf("browser instance is required")
	}
	return s.markRuntimeDegradedCause(inst.ProfileID, inst.PID, inst.CDPPort, runtimeStartedAt(inst), inst.LeaseOwner, cause, detail)
}

func (s *Supervisor) markRuntimeDegradedCause(profileID string, pid, cdpPort int, startedAt, leaseOwner string, cause degradationCause, detail string) error {
	causes, err := s.activeDegradationCauses(profileID, pid, startedAt)
	if err != nil {
		return err
	}
	if !causes[cause] {
		eventType, ok := degradationEventType(cause, false)
		if !ok {
			return fmt.Errorf("unknown degradation cause %q", cause)
		}
		if err := s.insertEvent(profileID, eventType, detail, pid, cdpPort); err != nil {
			return err
		}
	}
	return s.persistRuntimeState(profileID, pid, startedAt, leaseOwner, StateDegraded)
}

// recoverRuntimeDegradation clears exactly one active cause and changes the
// runtime to ready only after no other cause remains.
func (s *Supervisor) recoverRuntimeDegradation(profileID string, pid, cdpPort int, startedAt, leaseOwner string, cause degradationCause) (State, error) {
	causes, err := s.activeDegradationCauses(profileID, pid, startedAt)
	if err != nil {
		return StateDegraded, err
	}
	if !causes[cause] {
		var state string
		err := s.db.QueryRow(`SELECT state FROM browser_runtime_instances
			WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=?`,
			profileID, pid, startedAt, leaseOwner).Scan(&state)
		if err == sql.ErrNoRows {
			return StateStopped, nil
		}
		if err != nil {
			return StateDegraded, err
		}
		return State(state), nil
	}
	eventType, ok := degradationEventType(cause, true)
	if !ok {
		return StateDegraded, fmt.Errorf("degradation cause %q cannot be recovered", cause)
	}
	if err := s.insertEvent(profileID, eventType, "", pid, cdpPort); err != nil {
		return StateDegraded, err
	}
	causes, err = s.activeDegradationCauses(profileID, pid, startedAt)
	if err != nil {
		return StateDegraded, err
	}
	state := StateReady
	if len(causes) > 0 {
		state = StateDegraded
	}
	if err := s.persistRuntimeState(profileID, pid, startedAt, leaseOwner, state); err != nil {
		return StateDegraded, err
	}
	return state, nil
}

func (s *Supervisor) persistRuntimeState(profileID string, pid int, startedAt, leaseOwner string, state State) error {
	result, err := s.db.Exec(`UPDATE browser_runtime_instances SET state=?, updated_at=?
		WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=?`,
		state, time.Now().UTC().Format(time.RFC3339), profileID, pid, startedAt, leaseOwner)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	s.mu.Lock()
	current, inMemory := s.instances[profileID]
	if inMemory && current.PID == pid && runtimeStartedAt(current) == startedAt && current.LeaseOwner == leaseOwner {
		current.State = state
	} else {
		inMemory = false
	}
	s.mu.Unlock()
	if affected == 0 && !inMemory {
		return fmt.Errorf("browser runtime identity changed while updating degradation state")
	}
	if inMemory {
		s.writeRuntimeJSON()
	}
	return nil
}

// List 返回所有运行中的实例。
func (s *Supervisor) List() []*Instance {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		result = append(result, inst)
	}
	return result
}

// Get 按 profileID 查找实例。
func (s *Supervisor) Get(profileID string) (*Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[profileID]
	return inst, ok
}

// Health 检查实例健康状态。
func (s *Supervisor) Health(profileID string) (State, error) {
	s.mu.Lock()
	inst, ok := s.instances[profileID]
	s.mu.Unlock()

	if !ok {
		return StateStopped, fmt.Errorf("PROFILE_NOT_FOUND: %s", profileID)
	}

	if !pidAlive(inst.PID) {
		return StateCrashed, nil
	}
	if !cdpAlive(inst.CDPURL) {
		return StateDegraded, nil
	}
	return StateReady, nil
}

// Detach stops only this process's heartbeat. Browser processes and durable
// runtime rows intentionally remain alive so a UI/backend restart cannot erase
// account continuity. Explicit account Stop remains the only normal close path.
func (s *Supervisor) Detach() {
	if s != nil && s.cancel != nil {
		s.cancel()
	}
	if s == nil {
		return
	}
	s.mu.Lock()
	instances := make([]*Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		instances = append(instances, inst)
	}
	s.mu.Unlock()
	for _, inst := range instances {
		s.logEvent(inst.ProfileID, browserEventSupervisorDetached, "", inst.PID, inst.CDPPort)
	}
}

// Shutdown closes all instances owned by this Supervisor. Application/service
// teardown must use Detach; tests and explicit whole-runtime owners may use
// Shutdown when terminating every managed browser is actually intended.
func (s *Supervisor) Shutdown() {
	if s != nil && s.cancel != nil {
		s.cancel()
	}

	s.mu.Lock()
	profiles := make([]string, 0, len(s.instances))
	for pid := range s.instances {
		profiles = append(profiles, pid)
	}
	s.mu.Unlock()

	for _, profileID := range profiles {
		s.mu.Lock()
		inst := s.instances[profileID]
		s.mu.Unlock()
		if inst != nil {
			if err := s.reserveRuntimeStop(inst.ProfileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner); err == nil {
				// Persist intent before signalling. The following process_exited/crashed
				// event records the observed OS result without losing who requested it.
				s.logEvent(inst.ProfileID, browserEventSupervisorShutdown, "", inst.PID, inst.CDPPort)
				s.setStopEvent(inst, browserEventProcessExited)
				_ = s.closeInstance(inst)
			}
		}
	}
}

// Leases 返回 LeaseManager 供外部使用。
func (s *Supervisor) Leases() *LeaseManager {
	return s.leases
}

// --- 内部方法 ---

// removeTrackedInstance deletes only the exact in-memory process identity.
// A PID may be reused after an old process exits, so PID alone is not enough.
func (s *Supervisor) setStopEvent(inst *Instance, eventType string) {
	if inst == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.instances[inst.ProfileID]
	if ok && current.PID == inst.PID && current.StartedAt.Equal(inst.StartedAt) {
		current.stopEvent = eventType
	}
}

// finishProcessExit acknowledges Wait immediately. Cleanup and audit writes may
// block on SQLite, but Close must never signal a PID again after Wait completed.
func (s *Supervisor) finishProcessExit(inst *Instance, waitErr error) {
	exitDetail := stableProcessExitMessage(waitErr)
	s.mu.Lock()
	inst.exitDetail = exitDetail
	s.mu.Unlock()
	close(inst.done)
	s.mu.Lock()
	eventType := inst.stopEvent
	s.mu.Unlock()
	if eventType == "" {
		if waitErr != nil {
			eventType = browserEventCrashed
		} else {
			eventType = browserEventProcessExited
		}
	}
	s.removeTrackedInstance(inst)
	if removed, removeErr := s.removeRuntimeInstance(inst.ProfileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner); removeErr == nil && removed {
		_ = s.leases.Release(inst.ProfileID, inst.LeaseOwner)
	}
	s.writeRuntimeJSON()
	s.logEvent(inst.ProfileID, eventType, exitDetail, inst.PID, inst.CDPPort)
}

// stableProcessExitMessage keeps diagnostics useful without persisting raw
// browser stderr, command arguments, URLs, or profile data.
func stableProcessExitMessage(waitErr error) string {
	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		return ""
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return fmt.Sprintf("signal=%d", status.Signal())
	}
	return fmt.Sprintf("exit_code=%d", exitErr.ExitCode())
}

func (s *Supervisor) removeTrackedInstance(inst *Instance) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.instances[inst.ProfileID]
	if !ok || current.PID != inst.PID || !current.StartedAt.Equal(inst.StartedAt) {
		return false
	}
	delete(s.instances, inst.ProfileID)
	return true
}

func (s *Supervisor) closeInstance(inst *Instance) error {
	if inst.cmd != nil && inst.cmd.Process != nil {
		select {
		case <-inst.done:
			// The single Wait owner has already observed exit. Never signal a PID
			// that the OS may have reused for another process.
		default:
			if err := gracefulCloseCDP(inst.CDPURL); err == nil {
				select {
				case <-inst.done:
					break
				case <-time.After(2 * time.Second):
				}
			}
			select {
			case <-inst.done:
				break
			default:
				signal := func(sig syscall.Signal) error {
					if groupErr := syscall.Kill(-inst.cmd.Process.Pid, sig); groupErr != nil {
						if pidErr := syscall.Kill(inst.cmd.Process.Pid, sig); pidErr != nil && pidErr != syscall.ESRCH {
							return pidErr
						}
					}
					return nil
				}
				if err := signal(syscall.SIGTERM); err != nil {
					return fmt.Errorf("signal browser process %d: %w", inst.PID, err)
				}
				select {
				case <-inst.done:
				case <-time.After(5 * time.Second):
					if err := signal(syscall.SIGKILL); err != nil {
						return fmt.Errorf("kill browser process %d: %w", inst.PID, err)
					}
					select {
					case <-inst.done:
					case <-time.After(2 * time.Second):
						return fmt.Errorf("browser process %d did not exit after SIGKILL", inst.PID)
					}
				}
			}
		}
	}
	if inst.cancel != nil {
		inst.cancel()
	}

	s.removeTrackedInstance(inst)

	removed, err := s.removeRuntimeInstance(inst.ProfileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner)
	if err != nil {
		return fmt.Errorf("remove stopped browser runtime: %w", err)
	}
	if removed {
		if err := s.leases.Release(inst.ProfileID, inst.LeaseOwner); err != nil {
			return fmt.Errorf("release stopped browser lease: %w", err)
		}
	}
	s.writeRuntimeJSON()
	return nil
}

func (s *Supervisor) cleanInstance(profileID string) {
	s.mu.Lock()
	inst, ok := s.instances[profileID]
	if ok {
		delete(s.instances, profileID)
	}
	s.mu.Unlock()

	if ok && inst.cancel != nil {
		inst.cancel()
	}
	if ok {
		if removed, err := s.removeRuntimeInstance(profileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner); err == nil && removed {
			_ = s.leases.Release(profileID, inst.LeaseOwner)
		}
	}
}

func (s *Supervisor) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileAll()
			s.reconcileExternalRuntimes()
		}
	}
}

func (s *Supervisor) reconcileAll() {
	s.mu.Lock()
	snapshot := make([]*Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		snapshot = append(snapshot, inst)
	}
	s.mu.Unlock()

	changed := false
	for _, inst := range snapshot {
		if inst.State == StateStopped || inst.State == StateCrashed {
			continue
		}

		alive := pidAlive(inst.PID)
		cdpOK := alive && cdpAlive(inst.CDPURL)

		if !alive {
			// cmd.Wait is the sole owner of terminal classification for processes
			// launched by this Supervisor. Reconcile must not race it, invent a
			// crash reason, or persist raw process detail.
			continue
		} else if renewErr := s.leases.Renew(inst.ProfileID, inst.LeaseOwner, 30*time.Second); renewErr != nil {
			if errors.Is(renewErr.Error(), errors.PROFILE_LOCKED) {
				// A different holder now owns the profile. Fence and terminate only
				// this exact process identity; replacement runtime/lease rows survive.
				_, _ = s.db.Exec(`UPDATE browser_runtime_instances SET state='stopping', updated_at=?
					WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=?`,
					time.Now().UTC().Format(time.RFC3339), inst.ProfileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner)
				s.setStopEvent(inst, browserEventLeaseLost)
				if closeErr := s.closeInstance(inst); closeErr != nil {
					s.logEvent(inst.ProfileID, "runtime_persist_failed", closeErr.Error(), inst.PID, inst.CDPPort)
				}
				s.logEvent(inst.ProfileID, browserEventLeaseLost, "", inst.PID, inst.CDPPort)
				changed = true
				continue
			}
			if err := s.markDegradedCause(inst, degradationCauseLease, renewErr.Error()); err != nil {
				s.logEvent(inst.ProfileID, "runtime_persist_failed", err.Error(), inst.PID, inst.CDPPort)
			}
			changed = true
		} else {
			if _, err := s.recoverRuntimeDegradation(inst.ProfileID, inst.PID, inst.CDPPort, runtimeStartedAt(inst), inst.LeaseOwner, degradationCauseLease); err != nil {
				s.logEvent(inst.ProfileID, "runtime_persist_failed", err.Error(), inst.PID, inst.CDPPort)
			}
			if !cdpOK {
				if err := s.markDegradedCause(inst, degradationCauseHeartbeat, "CDP not responding"); err != nil {
					s.logEvent(inst.ProfileID, "runtime_persist_failed", err.Error(), inst.PID, inst.CDPPort)
				}
				changed = true
				continue
			}
			if _, err := s.recoverRuntimeDegradation(inst.ProfileID, inst.PID, inst.CDPPort, runtimeStartedAt(inst), inst.LeaseOwner, degradationCauseHeartbeat); err != nil {
				s.logEvent(inst.ProfileID, "runtime_persist_failed", err.Error(), inst.PID, inst.CDPPort)
			}
			s.mu.Lock()
			inst.LastSeenAt = time.Now().UTC()
			s.mu.Unlock()
			// 更新 SQLite last_seen_at
			s.db.Exec(`UPDATE browser_runtime_instances SET last_seen_at = ?, updated_at = ?
				WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=?`,
				time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
				inst.ProfileID, inst.PID, runtimeStartedAt(inst), inst.LeaseOwner)
		}
	}

	if changed {
		s.writeRuntimeJSON()
	}
}

func (s *Supervisor) upsertRuntimeInstance(inst *Instance) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO browser_runtime_instances
		 (profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id) DO UPDATE SET
			account_id=excluded.account_id,
			pid=excluded.pid,
			cdp_port=excluded.cdp_port,
			cdp_url=excluded.cdp_url,
			state=CASE
				WHEN browser_runtime_instances.state='stopping'
					AND browser_runtime_instances.pid=excluded.pid
					AND browser_runtime_instances.started_at=excluded.started_at
					AND COALESCE(browser_runtime_instances.lease_owner,'')=COALESCE(excluded.lease_owner,'')
				THEN 'stopping'
				ELSE excluded.state
			END,
			lease_owner=excluded.lease_owner,
			started_at=excluded.started_at,
			last_seen_at=excluded.last_seen_at,
			updated_at=excluded.updated_at`,
		inst.ProfileID, inst.AccountID, inst.PID, inst.CDPPort, inst.CDPURL,
		string(inst.State), inst.LeaseOwner, runtimeStartedAt(inst), now, now,
	)
	return err
}

// removeRuntimeInstance clears only an idle/terminal runtime. A running publish
// owner is retained for Runner heartbeat recovery even if the browser crashes.
func (s *Supervisor) removeRuntimeInstance(profileID string, pid int, startedAt, leaseOwner string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM browser_runtime_instances
		WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=? AND NOT EXISTS (
			SELECT 1 FROM publish_jobs
			WHERE id=browser_runtime_instances.active_job_id AND status='running'
		)`, profileID, pid, startedAt, leaseOwner)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

// reserveRuntimeStop is the explicit-stop linearization point. SQLite
// serializes this state transition against ClaimBrowserRuntimeJob: stop either
// reserves the exact idle runtime, or observes the running owner and fails
// without signaling the process. The row remains until process exit is proven.
func (s *Supervisor) reserveRuntimeStop(profileID string, pid int, startedAt, leaseOwner string) error {
	res, err := s.db.Exec(`UPDATE browser_runtime_instances
		SET state='stopping', updated_at=?
		WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=? AND state<>'stopping'
		AND NOT EXISTS (
			SELECT 1 FROM publish_jobs
			WHERE id=browser_runtime_instances.active_job_id AND status='running'
		)`, time.Now().UTC().Format(time.RFC3339), profileID, pid, startedAt, leaseOwner)
	if err != nil {
		return fmt.Errorf("reserve browser runtime stop: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reserve browser runtime stop rows affected: %w", err)
	}
	if changed == 1 {
		return nil
	}
	var currentPID int
	var currentStartedAt, currentLeaseOwner, currentState string
	var activeJobID int64
	var runningOwner int
	err = s.db.QueryRow(`SELECT pid, started_at, COALESCE(lease_owner,''), state, COALESCE(active_job_id,0),
		CASE WHEN EXISTS (SELECT 1 FROM publish_jobs
			WHERE id=browser_runtime_instances.active_job_id AND status='running') THEN 1 ELSE 0 END
		FROM browser_runtime_instances WHERE profile_id=?`, profileID).
		Scan(&currentPID, &currentStartedAt, &currentLeaseOwner, &currentState, &activeJobID, &runningOwner)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reserve browser runtime stop: %w", err)
	}
	if currentPID != pid || currentStartedAt != startedAt || currentLeaseOwner != leaseOwner {
		return fmt.Errorf("%s: browser runtime changed while stopping profile %s", errors.PROFILE_LOCKED, profileID)
	}
	if runningOwner == 1 {
		return fmt.Errorf("%s: profile %s is owned by publish job %d", errors.BUSY_BY_JOB, profileID, activeJobID)
	}
	if currentState == "stopping" {
		return nil
	}
	return fmt.Errorf("reserve browser runtime stop: runtime state %s could not be reserved", currentState)
}

// ensureRuntimeLaunchAvailable prevents an expired logical lease from allowing
// a second browser to open the same user-data-dir while the old PID or publish
// owner is still live. The unique lease acquired immediately afterwards closes
// the cross-process race between this check and process start.
func (s *Supervisor) ensureRuntimeLaunchAvailable(profileID string) error {
	var pid int
	var startedAt, leaseOwner string
	var activeJobID int64
	var runningOwner int
	err := s.db.QueryRow(`SELECT pid, started_at, COALESCE(lease_owner,''), COALESCE(active_job_id,0),
		CASE WHEN EXISTS (SELECT 1 FROM publish_jobs
			WHERE id=browser_runtime_instances.active_job_id AND status='running') THEN 1 ELSE 0 END
		FROM browser_runtime_instances WHERE profile_id=?`, profileID).
		Scan(&pid, &startedAt, &leaseOwner, &activeJobID, &runningOwner)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check browser runtime before launch: %w", err)
	}
	if runningOwner == 1 {
		return fmt.Errorf("%s: profile %s is owned by publish job %d", errors.BUSY_BY_JOB, profileID, activeJobID)
	}
	if pidAlive(pid) {
		return fmt.Errorf("%s: profile %s has live runtime pid %d", errors.ALREADY_RUNNING, profileID, pid)
	}
	removed, err := s.removeRuntimeInstance(profileID, pid, startedAt, leaseOwner)
	if err != nil {
		return fmt.Errorf("clean dead browser runtime before launch: %w", err)
	}
	if !removed {
		return fmt.Errorf("%s: browser runtime changed while launching profile %s", errors.PROFILE_LOCKED, profileID)
	}
	if leaseOwner != "" {
		if err := s.leases.Release(profileID, leaseOwner); err != nil {
			return fmt.Errorf("release dead browser lease before launch: %w", err)
		}
	}
	return nil
}

func (s *Supervisor) cleanStaleInstances() {
	// 启动时只清死进程/僵尸端口，保留仍存活的外部实例 runtime 行（占位端口）。
	s.reconcileDBOrphans()
	_ = s.leases.CleanExpired()
}

// reconcileDBOrphans 扫描 SQLite runtime 表：死 PID 行删除并释放 lease/端口；活 PID 保留占位。
func (s *Supervisor) reconcileDBOrphans() {
	s.reconcileExternalRuntimes()
}

// reconcileExternalRuntimes health-checks durable runtimes that survived a
// backend restart. It never owns or signals those processes: only exact SQLite
// state, paired heartbeat events, and conservative exit evidence are updated.
func (s *Supervisor) reconcileExternalRuntimes() {
	rows, err := s.db.Query(`SELECT profile_id, pid, cdp_port, cdp_url, COALESCE(lease_owner,''), started_at, state FROM browser_runtime_instances`)
	if err != nil {
		return
	}
	defer rows.Close()

	type orphan struct {
		profileID  string
		pid        int
		cdpPort    int
		cdpURL     string
		leaseOwner string
		startedAt  string
		state      string
	}
	var external []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.profileID, &o.pid, &o.cdpPort, &o.cdpURL, &o.leaseOwner, &o.startedAt, &o.state); err != nil {
			continue
		}
		s.mu.Lock()
		_, inMem := s.instances[o.profileID]
		s.mu.Unlock()
		if inMem {
			continue // reconcileAll 负责内存实例
		}
		external = append(external, o)
	}

	changed := false
	for _, o := range external {
		if !pidAlive(o.pid) {
			// A restarted backend has no cmd.Wait status, so "crashed" would be an
			// invention (the user may simply have closed the window). Fence the exact
			// row and record only the conservative process-exited fact.
			marked, markErr := s.db.Exec(`UPDATE browser_runtime_instances SET state='stopped', updated_at=?
				WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=? AND state<>'stopped'`,
				time.Now().UTC().Format(time.RFC3339), o.profileID, o.pid, o.startedAt, o.leaseOwner)
			if markErr != nil {
				continue
			}
			markedCount, _ := marked.RowsAffected()
			removed, removeErr := s.removeRuntimeInstance(o.profileID, o.pid, o.startedAt, o.leaseOwner)
			if removeErr != nil {
				continue
			}
			if removed && o.leaseOwner != "" && s.leases != nil {
				_ = s.leases.Release(o.profileID, o.leaseOwner)
			}
			if markedCount == 1 {
				s.logEvent(o.profileID, browserEventProcessMissing, "", o.pid, o.cdpPort)
			}
			changed = changed || removed || markedCount == 1
			continue
		}
		if o.state == "stopping" || o.state == string(StateStopped) || o.state == string(StateCrashed) {
			// A surviving process behind a terminal fence is deliberately left
			// untouched. This observer does not own it and must not resurrect it.
			continue
		}

		if !cdpAlive(o.cdpURL) {
			if err := s.markRuntimeDegradedCause(o.profileID, o.pid, o.cdpPort, o.startedAt, o.leaseOwner, degradationCauseHeartbeat, ""); err == nil {
				changed = true
			}
			continue
		}
		_, _ = s.recoverRuntimeDegradation(o.profileID, o.pid, o.cdpPort, o.startedAt, o.leaseOwner, degradationCauseHeartbeat)
		now := time.Now().UTC().Format(time.RFC3339)
		_, _ = s.db.Exec(`UPDATE browser_runtime_instances SET last_seen_at=?, updated_at=?
			WHERE profile_id=? AND pid=? AND started_at=? AND COALESCE(lease_owner,'')=?`,
			now, now, o.profileID, o.pid, o.startedAt, o.leaseOwner)
	}
	if changed {
		s.writeRuntimeJSON()
	}
}

func (s *Supervisor) writeRuntimeJSON() {
	s.mu.Lock()
	snap := RuntimeSnapshot{
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Instances: make([]InstanceSummary, 0, len(s.instances)),
	}
	for _, inst := range s.instances {
		if inst.State == StateStopped || inst.State == StateCrashed {
			continue
		}
		snap.Instances = append(snap.Instances, InstanceSummary{
			ProfileID:   inst.ProfileID,
			AccountID:   inst.AccountID,
			State:       string(inst.State),
			CDPPort:     inst.CDPPort,
			CDPURL:      inst.CDPURL,
			PID:         inst.PID,
			StartedAt:   runtimeStartedAt(inst),
			LastSeenAt:  inst.LastSeenAt.Format(time.RFC3339),
			LeaseOwner:  inst.LeaseOwner,
			ActiveJobID: inst.ActiveJobID,
		})
	}
	s.mu.Unlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}

	path := s.dataDir + "/runtime.json"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	os.Rename(tmp, path)
}

func (s *Supervisor) logEvent(profileID, eventType, message string, pid, cdpPort int) {
	_ = s.insertEvent(profileID, eventType, message, pid, cdpPort)
}

func (s *Supervisor) insertEvent(profileID, eventType, message string, pid, cdpPort int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	reasonCode, exitCode, termSignal := browserEventDiagnostics(eventType, message)
	_, err := s.db.Exec(
		`INSERT INTO browser_events
		 (profile_id, event_type, message, reason_code, exit_code, term_signal, pid, cdp_port, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profileID, eventType, message, reasonCode, exitCode, termSignal, pid, cdpPort, now,
	)
	return err
}

// browserEventDiagnostics converts only internally generated, fixed-format
// process results into fields safe for the local UI. Free-form messages stay
// in SQLite for local debugging and are never projected through this contract.
func browserEventDiagnostics(eventType, message string) (string, any, any) {
	switch eventType {
	case browserEventStoppedByUser:
		return browserReasonUserStop, nil, nil
	case browserEventProcessMissing:
		return browserReasonProcessMissingAfterRestart, nil, nil
	case browserEventSupervisorDetached:
		return browserReasonSupervisorDetached, nil, nil
	case browserEventSupervisorShutdown:
		return browserReasonSupervisorShutdown, nil, nil
	case browserEventLeaseLost:
		return browserReasonLeaseLost, nil, nil
	case browserEventProcessExited, browserEventCrashed:
		if message == "" && eventType == browserEventProcessExited {
			return browserReasonProcessExitClean, 0, nil
		}
		if strings.HasPrefix(message, "exit_code=") {
			value, err := strconv.Atoi(strings.TrimPrefix(message, "exit_code="))
			if err == nil && strconv.Itoa(value) == strings.TrimPrefix(message, "exit_code=") {
				if value == 0 {
					return browserReasonProcessExitClean, value, nil
				}
				return browserReasonProcessExitNonzero, value, nil
			}
		}
		if strings.HasPrefix(message, "signal=") {
			value, err := strconv.Atoi(strings.TrimPrefix(message, "signal="))
			if err == nil && value > 0 && strconv.Itoa(value) == strings.TrimPrefix(message, "signal=") {
				return browserReasonProcessSignaled, nil, value
			}
		}
		return browserReasonWaitResultUnknown, nil, nil
	default:
		return "", nil, nil
	}
}

func (s *Supervisor) SetBrowserPath(path string) {
	s.mu.Lock()
	s.browserPath = path
	s.mu.Unlock()
}

// ArrangeWindows 将所有运行中的浏览器实例按网格平铺排列在桌面上。
func (s *Supervisor) ArrangeWindows(layoutType string) error {
	s.mu.Lock()
	var active []*Instance
	for _, inst := range s.instances {
		if inst.State == StateReady || inst.State == StateBusy || inst.State == StateDegraded {
			if pidAlive(inst.PID) {
				active = append(active, inst)
			}
		}
	}
	s.mu.Unlock()

	N := len(active)
	if N == 0 {
		return nil
	}

	// 简单冒泡排序（按启动时间排序），保证排列顺序的一致性
	for i := 0; i < len(active); i++ {
		for j := i + 1; j < len(active); j++ {
			if active[i].StartedAt.After(active[j].StartedAt) {
				active[i], active[j] = active[j], active[i]
			}
		}
	}

	// 获取显示器屏幕分辨率
	screenWidth, screenHeight, err := GetScreenResolution()
	if err != nil {
		screenWidth, screenHeight = 1920, 1080 // 默认降级分辨率
	}

	// 计算网格行列数
	var cols, rows int
	if N <= 1 {
		cols, rows = 1, 1
	} else if N <= 2 {
		cols, rows = 2, 1
	} else if N <= 4 {
		cols, rows = 2, 2
	} else if N <= 6 {
		cols, rows = 3, 2
	} else if N <= 9 {
		cols, rows = 3, 3
	} else if N <= 12 {
		cols, rows = 4, 3
	} else {
		// ponytail: 极简的开根号网格划分
		c := 1
		for c*c < N {
			c++
		}
		cols = c
		rows = (N + c - 1) / c
	}

	// 针对 macOS 留出顶部菜单栏 (25px) 与底部 Dock 栏 (80px) 空间
	yOffset := 25
	dockOffset := 80
	usableWidth := screenWidth
	usableHeight := screenHeight - yOffset - dockOffset
	if usableHeight <= 0 {
		usableHeight = screenHeight
		yOffset = 0
	}

	cellWidth := usableWidth / cols
	cellHeight := usableHeight / rows

	for idx, inst := range active {
		col := idx % cols
		row := idx / cols
		x := col * cellWidth
		y := yOffset + row*cellHeight

		// 辅助功能权限缺失时 osascript 会失败；立即返回，不把每个窗口
		// 的等待累积成一次无响应的布局请求。
		if err := ResizeAndMoveByPID(inst.PID, x, y, cellWidth, cellHeight); err != nil {
			return fmt.Errorf("arrange browser window %d: %w", inst.PID, err)
		}
	}

	return nil
}

// --- 工具函数 ---

func waitCDPReadyOrExit(cdpURL string, timeout time.Duration, exited <-chan struct{}) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		resp, err := http.Get(cdpURL + "/json/version")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-exited:
			return fmt.Errorf("browser process exited before CDP was ready")
		case <-deadline.C:
			return fmt.Errorf("CDP not ready after %s", timeout)
		case <-ticker.C:
		}
	}
}

func pidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func cdpAlive(cdpURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(cdpURL + "/json/version")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func writeBrowserOSConfig(userDataDir string, cdpPort, serverPort, extPort int) {
	dotDir := filepath.Join(userDataDir, ".browseros")
	_ = os.MkdirAll(dotDir, 0o755)
	configPath := filepath.Join(dotDir, "server_config.json")

	var data map[string]interface{}
	if bytes, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(bytes, &data)
	}
	if data == nil {
		data = make(map[string]interface{})
	}

	dirs, ok := data["directories"].(map[string]interface{})
	if !ok {
		dirs = make(map[string]interface{})
		data["directories"] = dirs
	}
	dirs["execution"] = dotDir
	if dirs["resources"] == nil || dirs["resources"] == "" {
		dirs["resources"] = "/Applications/BrowserOS.app/Contents/Resources/BrowserOSServer/default/resources"
	}

	ports, ok := data["ports"].(map[string]interface{})
	if !ok {
		ports = make(map[string]interface{})
		data["ports"] = ports
	}
	ports["cdp"] = cdpPort
	ports["server"] = serverPort
	ports["extension"] = extPort
	if ports["proxy"] == nil {
		ports["proxy"] = cdpPort - 100
	}

	if finalBytes, err := json.MarshalIndent(data, "", "  "); err == nil {
		_ = os.WriteFile(configPath, finalBytes, 0644)
	}
}
