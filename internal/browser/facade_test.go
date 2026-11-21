//go:build legacy_chromium

package browser

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
	"github.com/gorilla/websocket"
)

func setupFacade(t *testing.T) (*BrowserFacade, *db.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	f := NewFacade(database, FacadeConfig{
		BrowserPath:           "/nonexistent/fingerprint-chromium",
		DataDir:               dataDir,
		MaxConcurrentBrowsers: 2,
		displayProvider: func() ([]DisplayBounds, error) {
			return []DisplayBounds{{X: 0, Y: 0, Width: 1920, Height: 1080}}, nil
		},
	})
	t.Cleanup(func() { f.Shutdown() })
	return f, database, dataDir
}

func seedAccount(t *testing.T, database *db.DB, dataDir string, name, platform string, seed int) int64 {
	t.Helper()
	acct, err := database.CreateAccount(platform, name, "")
	if err != nil {
		t.Fatal(err)
	}
	profileDir := filepath.Join(dataDir, "profiles", fmt.Sprintf("acct-%d", acct.ID))
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}
	_, err = database.CreateBrowserProfile(acct.ID, profileDir, seed, "en-US", "en-US,en", "America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	// preferred port + fingerprint_config for LaunchConfig consistency
	now := time.Now().UTC().Format(time.RFC3339)
	fc := `{"platform":"MacIntel","platformVersion":"14.0","brand":"Chrome","brandVersion":"120.0.0.0","hardwareConcurrency":8,"gpuVendor":"Apple","gpuRenderer":"Apple M1"}`
	_, err = database.Exec(
		`UPDATE browser_profiles SET last_cdp_port = ?, fingerprint_config = ?, proxy_server = ?, is_proxy_custom = 1, updated_at = ? WHERE account_id = ?`,
		15110, fc, "socks5://127.0.0.1:1080", now, acct.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	return acct.ID
}

func TestFacade_EmptyAccountID(t *testing.T) {
	f, _, _ := setupFacade(t)

	if _, err := f.StartAccount(0, StartOptions{}); err == nil || !strings.Contains(err.Error(), string(errors.INVALID_ACCOUNT_ID)) {
		t.Fatalf("StartAccount(0): want INVALID_ACCOUNT_ID, got %v", err)
	}
	if err := f.StopAccount(0); err == nil || !strings.Contains(err.Error(), string(errors.INVALID_ACCOUNT_ID)) {
		t.Fatalf("StopAccount(0): want INVALID_ACCOUNT_ID, got %v", err)
	}
	if _, err := f.Status(-1); err == nil || !strings.Contains(err.Error(), string(errors.INVALID_ACCOUNT_ID)) {
		t.Fatalf("Status(-1): want INVALID_ACCOUNT_ID, got %v", err)
	}
	if err := f.Focus(0); err == nil || !strings.Contains(err.Error(), string(errors.INVALID_ACCOUNT_ID)) {
		t.Fatalf("Focus(0): want INVALID_ACCOUNT_ID, got %v", err)
	}
	if _, err := f.LaunchConfigFromProfile(0); err == nil || !strings.Contains(err.Error(), string(errors.INVALID_ACCOUNT_ID)) {
		t.Fatalf("LaunchConfigFromProfile(0): want INVALID_ACCOUNT_ID, got %v", err)
	}
}

func TestAutomationStartOptionsNeverRequestFocus(t *testing.T) {
	for _, holder := range []LeaseHolderType{HolderJob, HolderBatch} {
		if (StartOptions{HolderType: holder}).shouldFocus() {
			t.Fatalf("%s automation requested focus", holder)
		}
	}
	if (StartOptions{HolderType: HolderUI}).shouldFocus() != true {
		t.Fatal("explicit UI start should retain focus behavior")
	}
	if (StartOptions{HolderType: HolderUI, SkipFocus: true}).shouldFocus() {
		t.Fatal("SkipFocus must override explicit UI focus")
	}
}

func TestFacade_UnknownAccount(t *testing.T) {
	f, _, _ := setupFacade(t)

	if _, err := f.StartAccount(99999, StartOptions{}); err == nil || !strings.Contains(err.Error(), string(errors.ACCOUNT_NOT_FOUND)) {
		t.Fatalf("StartAccount unknown: want ACCOUNT_NOT_FOUND, got %v", err)
	}
	if _, err := f.Status(99999); err == nil || !strings.Contains(err.Error(), string(errors.ACCOUNT_NOT_FOUND)) {
		t.Fatalf("Status unknown: want ACCOUNT_NOT_FOUND, got %v", err)
	}
	if _, err := f.LaunchConfigFromProfile(99999); err == nil || !strings.Contains(err.Error(), string(errors.ACCOUNT_NOT_FOUND)) {
		t.Fatalf("LaunchConfigFromProfile unknown: want ACCOUNT_NOT_FOUND, got %v", err)
	}
}

func TestFacade_DisabledAccountCannotBuildLaunchConfig(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	id := seedAccount(t, database, dataDir, "disabled", "instagram", 42)
	if err := database.SetAccountEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.LaunchConfigFromProfile(id); err == nil || !strings.Contains(err.Error(), string(errors.ACCOUNT_DISABLED)) {
		t.Fatalf("LaunchConfigFromProfile disabled account: %v", err)
	}
}

func TestFacade_StopIdempotent(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	id := seedAccount(t, database, dataDir, "stop-idem", "instagram", 4242)

	// 从未启动
	if err := f.StopAccount(id); err != nil {
		t.Fatalf("first stop (not running): %v", err)
	}
	// 重复 stop
	if err := f.StopAccount(id); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestFacade_StopRejectsRunningPublishOwner(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	accountID := seedAccount(t, database, dataDir, "stop-busy", "xhs", 4243)
	content, err := database.CreateContent("graph_text", "busy", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(accountID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	job, err := database.CreatePublishJobWithIntent(0, content.ID, accountID, assets[0].ID, "xhs", content.Type, "", "prepare", "")
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(job.ID, "stop-busy"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	profileID := ProfileIDForAccount(accountID)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, active_job_id, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 99999998, 15131, 'http://127.0.0.1:15131', 'ready', ?, 'external-owner', ?, ?, ?)`,
		profileID, accountID, job.ID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := f.sup.leases.Acquire(profileID, HolderJob, "external-owner", time.Minute); err != nil {
		t.Fatal(err)
	}

	if err := f.StopAccount(accountID); err == nil || !strings.Contains(err.Error(), string(errors.BUSY_BY_JOB)) {
		t.Fatalf("stop must reject running owner: %v", err)
	}
	var activeJobID int64
	if err := database.QueryRow(`SELECT active_job_id FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&activeJobID); err != nil {
		t.Fatal(err)
	}
	if activeJobID != job.ID {
		t.Fatalf("active job changed: got=%d want=%d", activeJobID, job.ID)
	}
	if lease, ok := f.sup.leases.Get(profileID); !ok || lease.HolderID != "external-owner" {
		t.Fatalf("publish owner's lease was removed: lease=%+v ok=%v", lease, ok)
	}
}

func TestFacade_StopWithoutRuntimePreservesUnknownLease(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	accountID := seedAccount(t, database, dataDir, "stop-unknown-lease", "xhs", 4244)
	profileID := ProfileIDForAccount(accountID)
	if err := f.sup.leases.Acquire(profileID, HolderUI, "another-starter", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := f.StopAccount(accountID); err != nil {
		t.Fatal(err)
	}
	if lease, ok := f.sup.leases.Get(profileID); !ok || lease.HolderID != "another-starter" {
		t.Fatalf("unknown lease was removed: lease=%+v ok=%v", lease, ok)
	}
}

func TestFacade_StopFailsClosedWhenRuntimeDeleteFails(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	accountID := seedAccount(t, database, dataDir, "stop-db-error", "xhs", 4245)
	profileID := ProfileIDForAccount(accountID)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 99999995, 15135, 'http://127.0.0.1:15135', 'ready', 'db-error-owner', ?, ?, ?)`,
		profileID, accountID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := f.sup.leases.Acquire(profileID, HolderUI, "db-error-owner", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(fmt.Sprintf(`CREATE TRIGGER block_runtime_delete BEFORE DELETE ON browser_runtime_instances
		WHEN OLD.profile_id=%q BEGIN SELECT RAISE(ABORT, 'blocked runtime delete'); END`, profileID)); err != nil {
		t.Fatal(err)
	}
	if err := f.StopAccount(accountID); err == nil || !strings.Contains(err.Error(), "blocked runtime delete") {
		t.Fatalf("stop did not fail closed: %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("runtime count=%d err=%v", count, err)
	}
	if lease, ok := f.sup.leases.Get(profileID); !ok || lease.HolderID != "db-error-owner" {
		t.Fatalf("lease released after stop gate error: lease=%+v ok=%v", lease, ok)
	}
}

func TestFacade_StopDeadExternalCleansExactRuntimeAndLease(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	accountID := seedAccount(t, database, dataDir, "stop-dead-external", "xhs", 4246)
	profileID := ProfileIDForAccount(accountID)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 99999994, 15136, 'http://127.0.0.1:15136', 'ready', 'dead-external-owner', ?, ?, ?)`,
		profileID, accountID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := f.sup.leases.Acquire(profileID, HolderUI, "dead-external-owner", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := f.StopAccount(accountID); err != nil {
		t.Fatal(err)
	}
	var runtimeCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&runtimeCount); err != nil || runtimeCount != 0 {
		t.Fatalf("runtime count=%d err=%v", runtimeCount, err)
	}
	if lease, ok := f.sup.leases.Get(profileID); ok {
		t.Fatalf("stopped runtime lease remains: %+v", lease)
	}
	var eventType string
	if err := database.QueryRow(`SELECT event_type FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, profileID).Scan(&eventType); err != nil || eventType != browserEventStoppedByUser {
		t.Fatalf("explicit stop event_type=%q err=%v", eventType, err)
	}
}

func TestFacadeStopExternalRequestsGracefulBrowserCloseBeforeSignalFallback(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	accountID := seedAccount(t, database, dataDir, "stop-graceful-external", "xhs", 4247)
	profileID := ProfileIDForAccount(accountID)
	process := exec.Command("sleep", "60")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	processDone := make(chan struct{})
	go func() {
		_ = process.Wait()
		close(processDone)
	}()
	t.Cleanup(func() {
		_ = process.Process.Kill()
		select {
		case <-processDone:
		case <-time.After(time.Second):
		}
	})
	requests := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/test",
		})
	})
	mux.HandleFunc("/devtools/browser/test", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		var command struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := conn.ReadJSON(&command); err != nil {
			t.Errorf("read command: %v", err)
			return
		}
		requests <- command.Method
		_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]any{}})
		_ = process.Process.Signal(syscall.SIGTERM)
	})
	server = httptest.NewServer(mux)
	defer server.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, ?, 15141, ?, 'ready', ?, ?, ?)`,
		profileID, accountID, process.Process.Pid, server.URL, now, now, now); err != nil {
		t.Fatal(err)
	}

	if err := f.StopAccount(accountID); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-requests:
		if method != "Browser.close" {
			t.Fatalf("method=%q", method)
		}
	default:
		t.Fatal("StopAccount skipped the graceful Browser.close request")
	}
}

func TestFacade_LaunchConfigFromProfile_Consistent(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	id := seedAccount(t, database, dataDir, "cfg-acct", "instagram", 12345)

	cfg1, err := f.LaunchConfigFromProfile(id)
	if err != nil {
		t.Fatal(err)
	}
	cfg2, err := f.LaunchConfigFromProfile(id)
	if err != nil {
		t.Fatal(err)
	}

	if cfg1.ProfileID != ProfileIDForAccount(id) {
		t.Fatalf("ProfileID: got %s", cfg1.ProfileID)
	}
	if cfg1.FingerprintSeed != 12345 || cfg2.FingerprintSeed != 12345 {
		t.Fatalf("seed mismatch: %d / %d", cfg1.FingerprintSeed, cfg2.FingerprintSeed)
	}
	if cfg1.PreferredPort != 15110 || cfg2.PreferredPort != 15110 {
		t.Fatalf("preferred port: %d / %d", cfg1.PreferredPort, cfg2.PreferredPort)
	}
	if cfg1.ProxyServer != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy: %s", cfg1.ProxyServer)
	}
	if cfg1.Platform != "MacIntel" || cfg1.Brand != "Chrome" || cfg1.HWConcurrency != 8 {
		t.Fatalf("fingerprint fields incomplete: %+v", cfg1)
	}
	if cfg1.LoginURL == "" {
		t.Fatal("LoginURL should be filled from platform")
	}
	// 两次启动参数一致（除动态端口外 —— PreferredPort 相同）
	if cfg1.UserDataDir != cfg2.UserDataDir ||
		cfg1.Lang != cfg2.Lang ||
		cfg1.LoginURL != cfg2.LoginURL ||
		cfg1.ProxyServer != cfg2.ProxyServer ||
		cfg1.Platform != cfg2.Platform ||
		cfg1.BrandVersion != cfg2.BrandVersion {
		t.Fatalf("LaunchConfig not stable across calls:\n1=%+v\n2=%+v", cfg1, cfg2)
	}

	// args 除端口外稳定
	args1 := cfg1.args(15110)
	args2 := cfg2.args(15110)
	if len(args1) != len(args2) {
		t.Fatalf("args length %d vs %d", len(args1), len(args2))
	}
	for i := range args1 {
		if args1[i] != args2[i] {
			t.Fatalf("args[%d]: %q vs %q", i, args1[i], args2[i])
		}
	}
	// CDP 仅 127.0.0.1
	joined := strings.Join(args1, " ")
	if !strings.Contains(joined, "--remote-debugging-address=127.0.0.1") {
		t.Fatal("missing remote-debugging-address=127.0.0.1")
	}
	if !strings.Contains(joined, "--remote-allow-origins=*") {
		t.Fatal("missing local CDP WebSocket origin allowance")
	}
}

func TestFacade_MaxConcurrentBrowsers(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	f.SetMaxConcurrentBrowsers(2)

	id1 := seedAccount(t, database, dataDir, "c1", "instagram", 1)
	id2 := seedAccount(t, database, dataDir, "c2", "instagram", 2)
	id3 := seedAccount(t, database, dataDir, "c3", "instagram", 3)

	// 注入假运行实例占满并发（不启真浏览器）
	now := time.Now().UTC().Format(time.RFC3339)
	for i, id := range []int64{id1, id2} {
		pid := os.Getpid() // 用当前测试进程模拟存活 PID
		port := 15101 + i
		profileID := ProfileIDForAccount(id)
		_, err := database.Exec(
			`INSERT INTO browser_runtime_instances
			 (profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 'ready', 'test', ?, ?, ?)`,
			profileID, id, pid, port, fmt.Sprintf("http://127.0.0.1:%d", port), now, now, now,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if n := f.Supervisor().CountRunning(); n < 2 {
		t.Fatalf("CountRunning want >=2, got %d", n)
	}

	_, err := f.StartAccount(id3, StartOptions{SkipFocus: true})
	if err == nil {
		// 若真启动了会泄漏，立刻停
		_ = f.StopAccount(id3)
		t.Fatal("expected MAX_CONCURRENT_BROWSERS")
	}
	if !strings.Contains(err.Error(), string(errors.MAX_CONCURRENT_BROWSERS)) {
		t.Fatalf("want MAX_CONCURRENT_BROWSERS, got %v", err)
	}
}

func TestFacade_LeaseConflict(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	id := seedAccount(t, database, dataDir, "lease-acct", "facebook", 99)

	if err := f.AcquireLease(id, HolderUI, "ui-session-1", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	err := f.AcquireLease(id, HolderCLI, "cli-other", 30*time.Second)
	if err == nil {
		t.Fatal("expected PROFILE_LOCKED")
	}
	if !strings.Contains(err.Error(), string(errors.PROFILE_LOCKED)) {
		t.Fatalf("want PROFILE_LOCKED, got %v", err)
	}
	if err := f.ReleaseLease(id, "ui-session-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.AcquireLease(id, HolderCLI, "cli-other", 30*time.Second); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestFacade_ReconcileDeadPID(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	id := seedAccount(t, database, dataDir, "dead-pid", "douyin", 7)
	profileID := ProfileIDForAccount(id)

	now := time.Now().UTC().Format(time.RFC3339)
	// 使用极大且不存在的 PID
	deadPID := 99999999
	_, err := database.Exec(
		`INSERT INTO browser_runtime_instances
		 (profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		 VALUES (?, ?, ?, 15155, 'http://127.0.0.1:15155', 'ready', 'holder-x', ?, ?, ?)`,
		profileID, id, deadPID, now, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	// 配套 lease
	_ = f.Supervisor().Leases().Acquire(profileID, HolderCLI, "holder-x", time.Hour)

	// 强制写入后，PID 不存活
	if pidAlive(deadPID) {
		t.Skip("deadPID unexpectedly alive on this system")
	}

	f.Reconcile()

	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id = ?`, profileID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("runtime row should be cleaned, count=%d", n)
	}
	if _, ok := f.Supervisor().Leases().Get(profileID); ok {
		t.Fatal("lease should be released after Reconcile")
	}

	// 端口应可再分配 15155
	port, err := NewPortAllocator(database).Acquire("other-profile", 15155)
	if err != nil {
		t.Fatal(err)
	}
	if port != 15155 {
		t.Fatalf("expected freed preferred port 15155, got %d", port)
	}
}

func TestFacade_ListRunningEmpty(t *testing.T) {
	f, _, _ := setupFacade(t)
	list := f.ListRunning()
	if len(list) != 0 {
		t.Fatalf("expected empty, got %d", len(list))
	}
}

func TestFacade_StatusStopped(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	id := seedAccount(t, database, dataDir, "st-acct", "xhs", 3)
	st, err := f.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Running || st.State != StateStopped {
		t.Fatalf("want stopped, got %+v", st)
	}
	if st.ProfileID != ProfileIDForAccount(id) {
		t.Fatalf("profile id: %s", st.ProfileID)
	}
}

func TestFacade_StartWithoutBrowserPath(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	f.SetBrowserPath("")
	id := seedAccount(t, database, dataDir, "no-bin", "kuaishou", 11)

	_, err := f.StartAccount(id, StartOptions{SkipFocus: true})
	if err == nil {
		_ = f.StopAccount(id)
		t.Fatal("expected error without browser path")
	}
	if !strings.Contains(err.Error(), string(errors.BROWSER_NOT_FOUND)) {
		t.Fatalf("want BROWSER_NOT_FOUND, got %v", err)
	}
}

func TestFacadeStartReturnsDurableRuntimeAfterBackendRestart(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	accountID := seedAccount(t, database, dataDir, "external-restart", "xhs", 778)
	cdp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer cdp.Close()
	parsed, err := url.Parse(cdp.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	profileID := ProfileIDForAccount(accountID)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ready', 'previous-backend', ?, ?, ?)`,
		profileID, accountID, os.Getpid(), port, cdp.URL, now, now, now); err != nil {
		t.Fatal(err)
	}

	inst, startErr := f.StartAccount(accountID, StartOptions{SkipFocus: true})
	if startErr == nil || !strings.Contains(startErr.Error(), string(errors.ALREADY_RUNNING)) {
		t.Fatalf("start error=%v", startErr)
	}
	if inst == nil || inst.ProfileID != profileID || inst.PID != os.Getpid() || inst.CDPPort != port || inst.State != StateReady {
		t.Fatalf("durable runtime was not returned: %+v", inst)
	}
}

func TestFacadeDetachPreservesRuntimeForNextBackend(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	fakeBin := writeFakeBrowser(t, dataDir)
	f.SetBrowserPath(fakeBin)
	accountID := seedAccount(t, database, dataDir, "detach-restart", "instagram", 779)
	if _, err := database.Exec(`UPDATE browser_profiles SET last_cdp_port=0 WHERE account_id=?`, accountID); err != nil {
		t.Fatal(err)
	}
	inst, err := f.StartAccount(accountID, StartOptions{HolderType: HolderUI, HolderID: "first-backend", SkipFocus: true})
	if err != nil {
		t.Fatal(err)
	}

	f.Detach()
	if !pidAlive(inst.PID) {
		t.Fatal("detaching the backend stopped the account browser")
	}
	var runtimeCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id=? AND pid=?`, inst.ProfileID, inst.PID).Scan(&runtimeCount); err != nil || runtimeCount != 1 {
		t.Fatalf("durable runtime count=%d err=%v", runtimeCount, err)
	}

	next := NewFacade(database, FacadeConfig{BrowserPath: fakeBin, DataDir: dataDir, MaxConcurrentBrowsers: 2})
	defer next.Shutdown()
	status, err := next.Status(accountID)
	if err != nil || status == nil || !status.Running || status.PID != inst.PID {
		t.Fatalf("next backend did not recover runtime: status=%+v err=%v", status, err)
	}
	if err := next.StopAccount(accountID); err != nil {
		t.Fatal(err)
	}
}

// TestFacade_StartSuccessWithFakeBrowser 用假 CDP 服务模拟真浏览器，覆盖 Start 成功路径。
func TestFacade_StartSuccessWithFakeBrowser(t *testing.T) {
	f, database, dataDir := setupFacade(t)
	f.SetMaxConcurrentBrowsers(5)

	fakeBin := writeFakeBrowser(t, dataDir)
	f.SetBrowserPath(fakeBin)

	id := seedAccount(t, database, dataDir, "fake-ok", "instagram", 777)
	// 清 preferred 避免端口冲突
	database.Exec(`UPDATE browser_profiles SET last_cdp_port = 0 WHERE account_id = ?`, id)

	inst, err := f.StartAccount(id, StartOptions{
		HolderType: HolderUI,
		HolderID:   "test-ui",
		SkipFocus:  true, // CI 无 GUI
	})
	if err != nil {
		t.Fatalf("StartAccount: %v", err)
	}
	if inst.CDPPort < PortMin || inst.CDPPort > PortMax {
		t.Fatalf("port out of range: %d", inst.CDPPort)
	}
	if inst.State != StateReady && inst.State != StateDegraded {
		t.Fatalf("state: %s", inst.State)
	}

	list := f.ListRunning()
	if len(list) != 1 {
		t.Fatalf("ListRunning: %d", len(list))
	}

	st, err := f.Status(id)
	if err != nil || !st.Running {
		t.Fatalf("Status: %+v err=%v", st, err)
	}

	// 重复 stop 幂等
	if err := f.StopAccount(id); err != nil {
		t.Fatal(err)
	}
	var exitEvent string
	if err := database.QueryRow(`SELECT event_type FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, ProfileIDForAccount(id)).Scan(&exitEvent); err != nil || exitEvent != browserEventStoppedByUser {
		t.Fatalf("explicit in-process stop event_type=%q err=%v", exitEvent, err)
	}
	if err := f.StopAccount(id); err != nil {
		t.Fatal(err)
	}
	st2, _ := f.Status(id)
	if st2.Running {
		t.Fatal("should not be running after stop")
	}
}

func writeFakeBrowser(t *testing.T, dataDir string) string {
	t.Helper()
	// 解析 --remote-debugging-port=N，在 127.0.0.1:N 上提供 /json/version
	script := `#!/usr/bin/env bash
set -e
PORT=""
for a in "$@"; do
  case "$a" in
    --remote-debugging-port=*) PORT="${a#--remote-debugging-port=}" ;;
  esac
done
if [ -z "$PORT" ]; then
  echo "no port" >&2
  exit 1
fi
# Python 极简 HTTP（标准库），响应 CDP version
python3 - "$PORT" <<'PY'
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port = int(sys.argv[1])

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b'[]' if self.path == "/json/list" else b'{"Browser":"fake","Protocol-Version":"1.3"}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *args):
        pass

HTTPServer(("127.0.0.1", port), H).serve_forever()
PY
`
	path := filepath.Join(dataDir, "fake-browser.sh")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
