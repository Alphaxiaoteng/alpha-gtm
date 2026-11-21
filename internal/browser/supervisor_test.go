//go:build legacy_chromium

package browser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSupervisorRemoveTrackedInstanceRejectsReusedPID(t *testing.T) {
	sup := &Supervisor{instances: make(map[string]*Instance)}
	old := &Instance{ProfileID: "acct-reused-pid", PID: 4242, StartedAt: time.Unix(100, 0).UTC()}
	replacement := &Instance{ProfileID: old.ProfileID, PID: old.PID, StartedAt: old.StartedAt.Add(time.Second)}
	sup.instances[old.ProfileID] = replacement

	if sup.removeTrackedInstance(old) {
		t.Fatal("old process identity removed replacement with reused PID")
	}
	if current := sup.instances[old.ProfileID]; current != replacement {
		t.Fatalf("replacement changed: current=%p replacement=%p", current, replacement)
	}
}

func TestProcessWaitAcknowledgementDoesNotWaitForSQLiteCleanup(t *testing.T) {
	database := setupTestDB(t)
	startedAt := time.Now().UTC()
	instance := &Instance{
		ProfileID: "acct-wait-cleanup", AccountID: 71, PID: 987654, CDPPort: 15191,
		CDPURL: "http://127.0.0.1:15191", State: StateReady, StartedAt: startedAt,
		LastSeenAt: startedAt, LeaseOwner: "wait-cleanup-owner", done: make(chan struct{}),
	}
	sup := &Supervisor{
		instances: map[string]*Instance{instance.ProfileID: instance}, db: database,
		leases: NewLeaseManager(database), dataDir: t.TempDir(),
	}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`UPDATE browser_runtime_instances SET updated_at=updated_at WHERE profile_id=?`, instance.ProfileID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		sup.finishProcessExit(instance, nil)
		close(finished)
	}()
	select {
	case <-instance.done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Wait acknowledgement was delayed by SQLite cleanup")
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not finish after SQLite lock release")
	}
}

func TestUnexpectedWaitErrorRecordsStableCrashWithoutDetail(t *testing.T) {
	database := setupTestDB(t)
	startedAt := time.Now().UTC()
	instance := &Instance{
		ProfileID: "acct-unexpected-exit", AccountID: 72, PID: 987655, CDPPort: 15193,
		CDPURL: "http://127.0.0.1:15193", State: StateReady, StartedAt: startedAt,
		LastSeenAt: startedAt, LeaseOwner: "unexpected-exit-owner", done: make(chan struct{}),
	}
	sup := &Supervisor{
		instances: map[string]*Instance{instance.ProfileID: instance}, db: database,
		leases: NewLeaseManager(database), dataDir: t.TempDir(),
	}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	sup.finishProcessExit(instance, fmt.Errorf("private process detail"))
	var eventType, message string
	if err := database.QueryRow(`SELECT event_type, COALESCE(message,'') FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, instance.ProfileID).Scan(&eventType, &message); err != nil {
		t.Fatal(err)
	}
	if eventType != "crashed" || message != "" {
		t.Fatalf("event_type=%q message=%q", eventType, message)
	}
}

func TestStableProcessExitMessageRecordsOnlyExitCode(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 23")
	err := cmd.Run()
	if got := stableProcessExitMessage(err); got != "exit_code=23" {
		t.Fatalf("stable exit detail=%q", got)
	}
	if got := stableProcessExitMessage(fmt.Errorf("private process detail")); got != "" {
		t.Fatalf("raw process detail leaked: %q", got)
	}
}

func TestBrowserEventDiagnosticsExposeOnlyStableProcessFields(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		message    string
		wantReason string
		wantExit   any
		wantSignal any
	}{
		{name: "clean", eventType: browserEventProcessExited, wantReason: browserReasonProcessExitClean, wantExit: 0},
		{name: "nonzero", eventType: browserEventCrashed, message: "exit_code=23", wantReason: browserReasonProcessExitNonzero, wantExit: 23},
		{name: "signal", eventType: browserEventCrashed, message: "signal=15", wantReason: browserReasonProcessSignaled, wantSignal: 15},
		{name: "missing after restart", eventType: browserEventProcessMissing, wantReason: browserReasonProcessMissingAfterRestart},
		{name: "free form never parsed", eventType: browserEventCrashed, message: "token=private exit_code=23", wantReason: browserReasonWaitResultUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reason, exitCode, signal := browserEventDiagnostics(test.eventType, test.message)
			if reason != test.wantReason || exitCode != test.wantExit || signal != test.wantSignal {
				t.Fatalf("reason=%q exit=%v signal=%v", reason, exitCode, signal)
			}
		})
	}
}

func TestWaitCDPReadyOrExitReturnsImmediatelyAfterProcessExit(t *testing.T) {
	exited := make(chan struct{})
	close(exited)
	started := time.Now()
	err := waitCDPReadyOrExit("http://127.0.0.1:1", time.Second, exited)
	if err == nil || !strings.Contains(err.Error(), "process exited") {
		t.Fatalf("wait error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("process exit was not observed promptly: %s", elapsed)
	}
}

func TestSupervisorOpenReturnsStableCrashImmediatelyWhenBrowserExits(t *testing.T) {
	database := setupTestDB(t)
	account, err := database.CreateAccount("xhs", "browser-exit-fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binPath := filepath.Join(dir, "browser-exit")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 23\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sup := NewSupervisor(database, binPath, dir)
	defer sup.Shutdown()
	started := time.Now()
	_, err = sup.Open(LaunchConfig{
		ProfileID: ProfileIDForAccount(account.ID), AccountID: account.ID,
		UserDataDir: filepath.Join(dir, "profile"), Engine: EngineChromium, BinPath: binPath,
	})
	if err == nil || !strings.Contains(err.Error(), string(errors.BROWSER_CRASH)) || !strings.Contains(err.Error(), "exit_code=23") {
		t.Fatalf("start error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("browser exit was reported too slowly: %s", elapsed)
	}
}

func TestReconcileAllLeavesOwnedTerminalClassificationToWait(t *testing.T) {
	database := setupTestDB(t)
	startedAt := time.Now().UTC().Add(-time.Second)
	instance := &Instance{
		ProfileID: "acct-owned-dead", AccountID: 73, PID: 99999991, CDPPort: 15194,
		CDPURL: "http://127.0.0.1:15194", State: StateReady, StartedAt: startedAt,
		LastSeenAt: startedAt, LeaseOwner: "owned-dead-owner", done: make(chan struct{}),
	}
	if pidAlive(instance.PID) {
		t.Skip("synthetic dead PID is unexpectedly alive")
	}
	sup := &Supervisor{
		instances: map[string]*Instance{instance.ProfileID: instance}, db: database,
		leases: NewLeaseManager(database), dataDir: t.TempDir(),
	}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}

	sup.reconcileAll()
	if current, ok := sup.Get(instance.ProfileID); !ok || current != instance {
		t.Fatal("heartbeat removed an owned process before its Wait owner classified exit")
	}
	var premature int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_events WHERE profile_id=? AND event_type IN ('crashed','process_exited')`, instance.ProfileID).Scan(&premature); err != nil || premature != 0 {
		t.Fatalf("heartbeat wrote terminal event before Wait: count=%d err=%v", premature, err)
	}

	sup.finishProcessExit(instance, nil)
	var eventType, message string
	if err := database.QueryRow(`SELECT event_type, COALESCE(message,'') FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, instance.ProfileID).Scan(&eventType, &message); err != nil {
		t.Fatal(err)
	}
	if eventType != browserEventProcessExited || message != "" {
		t.Fatalf("event_type=%q message=%q", eventType, message)
	}
}

func TestExternalRuntimeHeartbeatPersistsHealthWithoutOwningProcess(t *testing.T) {
	database := setupTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	startedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	profileID := "acct-external-healthy"
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, 74, ?, 15991, ?, 'degraded', 'external-owner', ?, ?, ?)`,
		profileID, os.Getpid(), server.URL, startedAt, startedAt, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO browser_events (profile_id, event_type, message, pid, cdp_port, created_at)
		VALUES (?, 'heartbeat_failed', '', ?, 15991, ?)`, profileID, os.Getpid(), time.Now().UTC().Add(-time.Second).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{instances: make(map[string]*Instance), db: database, leases: NewLeaseManager(database), dataDir: t.TempDir()}

	sup.reconcileExternalRuntimes()
	var state, lastSeen string
	if err := database.QueryRow(`SELECT state, last_seen_at FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&state, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if state != string(StateReady) || lastSeen == startedAt {
		t.Fatalf("state=%q last_seen_at=%q", state, lastSeen)
	}
	var recovered int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_events WHERE profile_id=? AND event_type='heartbeat_recovered'`, profileID).Scan(&recovered); err != nil || recovered != 1 {
		t.Fatalf("heartbeat recovery count=%d err=%v", recovered, err)
	}
	if !pidAlive(os.Getpid()) {
		t.Fatal("external reconcile signalled its unowned process")
	}
}

func TestExternalRuntimeHeartbeatPreservesStoppingFence(t *testing.T) {
	database := setupTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	startedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	profileID := "acct-external-stopping"
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, 76, ?, 15993, ?, 'stopping', 'external-stopping-owner', ?, ?, ?)`,
		profileID, os.Getpid(), server.URL, startedAt, startedAt, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO browser_events (profile_id, event_type, message, pid, cdp_port, created_at)
		VALUES (?, 'heartbeat_failed', '', ?, 15993, ?)`, profileID, os.Getpid(), time.Now().UTC().Add(-time.Second).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{instances: make(map[string]*Instance), db: database, leases: NewLeaseManager(database), dataDir: t.TempDir()}

	sup.reconcileExternalRuntimes()
	var state string
	if err := database.QueryRow(`SELECT state FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "stopping" {
		t.Fatalf("external heartbeat cleared stopping fence: state=%q", state)
	}
}

func TestExternalRuntimeDeadPIDUsesConservativeStableExit(t *testing.T) {
	database := setupTestDB(t)
	const deadPID = 99999992
	if pidAlive(deadPID) {
		t.Skip("synthetic dead PID is unexpectedly alive")
	}
	startedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	profileID := "acct-external-dead"
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, 75, ?, 15992, 'http://127.0.0.1:15992', 'ready', 'external-dead-owner', ?, ?, ?)`,
		profileID, deadPID, startedAt, startedAt, startedAt); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{instances: make(map[string]*Instance), db: database, leases: NewLeaseManager(database), dataDir: t.TempDir()}

	sup.reconcileExternalRuntimes()
	var remaining int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("remaining=%d err=%v", remaining, err)
	}
	var eventType, message, reasonCode string
	if err := database.QueryRow(`SELECT event_type, COALESCE(message,''), COALESCE(reason_code,'') FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, profileID).Scan(&eventType, &message, &reasonCode); err != nil {
		t.Fatal(err)
	}
	if eventType != browserEventProcessMissing || message != "" || reasonCode != browserReasonProcessMissingAfterRestart {
		t.Fatalf("unowned exit was over-classified: event_type=%q message=%q reason=%q", eventType, message, reasonCode)
	}
}

func TestExternalRuntimeHeartbeatPersistsCDPFailureWithoutOwningProcess(t *testing.T) {
	database := setupTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	cdpURL := server.URL
	server.Close()
	startedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	profileID := "acct-external-cdp-down"
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, 76, ?, 15993, ?, 'ready', 'external-cdp-owner', ?, ?, ?)`,
		profileID, os.Getpid(), cdpURL, startedAt, startedAt, startedAt); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{instances: make(map[string]*Instance), db: database, leases: NewLeaseManager(database), dataDir: t.TempDir()}

	sup.reconcileExternalRuntimes()
	var state string
	if err := database.QueryRow(`SELECT state FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&state); err != nil || state != string(StateDegraded) {
		t.Fatalf("state=%q err=%v", state, err)
	}
	var eventType, message string
	if err := database.QueryRow(`SELECT event_type, COALESCE(message,'') FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, profileID).Scan(&eventType, &message); err != nil {
		t.Fatal(err)
	}
	if eventType != browserEventHeartbeatFailed || message != "" {
		t.Fatalf("event_type=%q message=%q", eventType, message)
	}
	if !pidAlive(os.Getpid()) {
		t.Fatal("external CDP reconcile signalled its unowned process")
	}
}

func TestPortAllocator_Acquire(t *testing.T) {
	d := setupTestDB(t)
	pa := NewPortAllocator(d)

	// 获取第一个端口
	port1, err := pa.Acquire("profile-1", 0)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if port1 < PortMin || port1 > PortMax {
		t.Fatalf("port %d out of range [%d, %d]", port1, PortMin, PortMax)
	}

	// 优先使用当前机器上真实空闲的 preferred；开发中的账号浏览器可能
	// 正在占用 15101–15105，测试不能要求关闭用户会话。
	preferred := 0
	for port := PortMax; port >= PortMin; port-- {
		if isTCPFree(port) {
			preferred = port
			break
		}
	}
	if preferred == 0 {
		t.Skip("no free CDP port available for preferred-port assertion")
	}
	port2, err := pa.Acquire("profile-2", preferred)
	if err != nil {
		t.Fatalf("Acquire preferred: %v", err)
	}
	if port2 != preferred {
		t.Fatalf("expected preferred port %d, got %d", preferred, port2)
	}
}

func TestCDPPortPoolHasFiveHundredSlots(t *testing.T) {
	if got := PortMax - PortMin + 1; got != 500 {
		t.Fatalf("CDP port pool size=%d, want 500", got)
	}
}

func TestPortAllocator_PreferredOccupied(t *testing.T) {
	d := setupTestDB(t)
	pa := NewPortAllocator(d)

	// 模拟一个已占用端口
	d.Exec(`INSERT INTO browser_runtime_instances (profile_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES ('occupied', 9999, 15105, 'http://127.0.0.1:15105', 'ready', datetime('now'), datetime('now'), datetime('now'))`)

	port, err := pa.Acquire("profile-new", 15105)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 应该分配了非 15105 的端口
	if port == 15105 {
		t.Fatalf("should not have allocated occupied port 15105")
	}
}

func TestPortAllocatorReserveBlocksOtherLaunchUntilRelease(t *testing.T) {
	d := setupTestDB(t)
	pa := NewPortAllocator(d)
	first, err := pa.Reserve("profile-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	second, err := pa.Reserve("profile-b", first.Port)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if second.Port == first.Port {
		t.Fatalf("port %d was reserved twice", first.Port)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	third, err := pa.Reserve("profile-c", first.Port)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Release()
	if third.Port != first.Port {
		t.Fatalf("released preferred port = %d, want %d", third.Port, first.Port)
	}
}

func TestLeaseManager_AcquireRelease(t *testing.T) {
	d := setupTestDB(t)
	lm := NewLeaseManager(d)

	// 获取 lease
	err := lm.Acquire("profile-1", HolderCLI, "cli-1", 30*1e9) // 30s
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// 验证获取到了
	lease, ok := lm.Get("profile-1")
	if !ok {
		t.Fatal("Get: expected lease")
	}
	if lease.HolderID != "cli-1" {
		t.Fatalf("expected holder cli-1, got %s", lease.HolderID)
	}

	// 另一个 holder 应该被拒绝
	err = lm.Acquire("profile-1", HolderUI, "ui-1", 30*1e9)
	if err == nil {
		t.Fatal("expected error for double acquire")
	}

	// 释放
	err = lm.Release("profile-1", "cli-1")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}

	// 释放后应该空闲
	_, ok = lm.Get("profile-1")
	if ok {
		t.Fatal("expected lease to be released")
	}

	// 现在 ui-1 应该能获取
	err = lm.Acquire("profile-1", HolderUI, "ui-1", 30*1e9)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
}

func TestLeaseManager_SameHolderReacquire(t *testing.T) {
	d := setupTestDB(t)
	lm := NewLeaseManager(d)

	err := lm.Acquire("profile-1", HolderCLI, "cli-1", 30*1e9)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// 同一个 holder 再次获取应该成功（续租）
	err = lm.Acquire("profile-1", HolderCLI, "cli-1", 30*1e9)
	if err != nil {
		t.Fatalf("Reacquire by same holder: %v", err)
	}
}

func TestLaunchLeaseOwnerIsUniquePerGeneration(t *testing.T) {
	first, err := newLaunchLeaseOwner("ui-pid-42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := newLaunchLeaseOwner("ui-pid-42")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "ui-pid-42:launch:") || !strings.HasPrefix(second, "ui-pid-42:launch:") {
		t.Fatalf("invalid launch generations: first=%q second=%q", first, second)
	}
}

func TestLaunchLeaseOwnersFenceConcurrentSameLogicalHolder(t *testing.T) {
	d := setupTestDB(t)
	lm := NewLeaseManager(d)
	first, err := newLaunchLeaseOwner("ui-pid-42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := newLaunchLeaseOwner("ui-pid-42")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, owner := range []string{first, second} {
		go func(owner string) {
			<-start
			results <- lm.Acquire("profile-concurrent-launch", HolderUI, owner, time.Minute)
		}(owner)
	}
	close(start)
	winners, locked := 0, 0
	for range 2 {
		acquireErr := <-results
		if acquireErr == nil {
			winners++
		} else if strings.Contains(acquireErr.Error(), string(errors.PROFILE_LOCKED)) {
			locked++
		} else {
			t.Fatalf("unexpected acquire error: %v", acquireErr)
		}
	}
	if winners != 1 || locked != 1 {
		t.Fatalf("winners=%d locked=%d", winners, locked)
	}
}

func TestDelayedOldLaunchReleasePreservesReplacementLease(t *testing.T) {
	d := setupTestDB(t)
	lm := NewLeaseManager(d)
	oldOwner, err := newLaunchLeaseOwner("ui-pid-42")
	if err != nil {
		t.Fatal(err)
	}
	newOwner, err := newLaunchLeaseOwner("ui-pid-42")
	if err != nil {
		t.Fatal(err)
	}
	if err := lm.Acquire("profile-lease-aba", HolderUI, oldOwner, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE profile_leases SET expires_at=? WHERE profile_id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), "profile-lease-aba"); err != nil {
		t.Fatal(err)
	}
	if err := lm.Acquire("profile-lease-aba", HolderUI, newOwner, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := lm.Release("profile-lease-aba", oldOwner); err != nil {
		t.Fatal(err)
	}
	lease, ok := lm.Get("profile-lease-aba")
	if !ok || lease.HolderID != newOwner {
		t.Fatalf("old cleanup removed replacement lease: lease=%+v ok=%v", lease, ok)
	}
}

func TestLeaseManager_AcquireReclaimsRFC3339ExpiredRow(t *testing.T) {
	d := setupTestDB(t)
	lm := NewLeaseManager(d)
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO profile_leases
		(profile_id, holder_type, holder_id, acquired_at, expires_at, updated_at)
		VALUES ('profile-expired', 'cli', 'old-holder', ?, ?, ?)`,
		now.Add(-2*time.Minute).Format(time.RFC3339), now.Add(-time.Minute).Format(time.RFC3339), now.Add(-2*time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := lm.Acquire("profile-expired", HolderUI, "new-holder", time.Minute); err != nil {
		t.Fatalf("acquire after RFC3339 expiry: %v", err)
	}
	lease, ok := lm.Get("profile-expired")
	if !ok || lease.HolderID != "new-holder" {
		t.Fatalf("lease=%+v ok=%v", lease, ok)
	}
}

func TestLeaseManager_RenewRejectsLostOwnership(t *testing.T) {
	d := setupTestDB(t)
	lm := NewLeaseManager(d)
	if err := lm.Acquire("profile-lost", HolderCLI, "old-holder", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := lm.Release("profile-lost", "old-holder"); err != nil {
		t.Fatal(err)
	}
	if err := lm.Acquire("profile-lost", HolderUI, "new-holder", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := lm.Renew("profile-lost", "old-holder", time.Minute); err == nil || !strings.Contains(err.Error(), string(errors.PROFILE_LOCKED)) {
		t.Fatalf("lost owner renew error=%v", err)
	}
}

func TestSupervisorLeaseLossStopsOnlyOldProcessIdentity(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	startedAt := time.Now().UTC()
	inst := &Instance{
		ProfileID: "acct-lease-lost", AccountID: 88, PID: cmd.Process.Pid,
		CDPPort: 15138, CDPURL: "http://127.0.0.1:15138", State: StateReady,
		StartedAt: startedAt, LastSeenAt: startedAt, LeaseOwner: "old-holder",
		cmd: cmd, done: make(chan struct{}),
	}
	go func() {
		_ = cmd.Wait()
		close(inst.done)
	}()
	sup.mu.Lock()
	sup.instances[inst.ProfileID] = inst
	sup.mu.Unlock()
	if err := sup.upsertRuntimeInstance(inst); err != nil {
		t.Fatal(err)
	}
	if err := sup.leases.Acquire(inst.ProfileID, HolderUI, "new-holder", time.Minute); err != nil {
		t.Fatal(err)
	}

	sup.reconcileAll()
	if pidAlive(inst.PID) {
		t.Fatal("old process remained alive after lease ownership loss")
	}
	if lease, ok := sup.leases.Get(inst.ProfileID); !ok || lease.HolderID != "new-holder" {
		t.Fatalf("replacement lease changed: lease=%+v ok=%v", lease, ok)
	}
	if current, ok := sup.Get(inst.ProfileID); ok && current.PID == inst.PID {
		t.Fatalf("old instance remained in memory: %+v", current)
	}
}

func TestSupervisor_NewAndList(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()

	instances := sup.List()
	if len(instances) != 0 {
		t.Fatalf("expected 0 instances, got %d", len(instances))
	}
}

func TestSupervisorRuntimeRefreshPreservesActivePublishJob(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	instance := &Instance{
		ProfileID: "acct-7", AccountID: 7, PID: 12345, CDPPort: 15107,
		CDPURL: "http://127.0.0.1:15107", State: StateReady, LeaseOwner: "ui-7", StartedAt: time.Now().UTC(),
	}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE browser_runtime_instances SET active_job_id=77 WHERE profile_id=?`, instance.ProfileID); err != nil {
		t.Fatal(err)
	}
	sup.mu.Lock()
	sup.instances[instance.ProfileID] = instance
	sup.mu.Unlock()
	sup.MarkDegraded(instance.ProfileID, "test")
	var activeJobID int64
	if err := d.QueryRow(`SELECT active_job_id FROM browser_runtime_instances WHERE profile_id=?`, instance.ProfileID).Scan(&activeJobID); err != nil {
		t.Fatal(err)
	}
	if activeJobID != 77 {
		t.Fatalf("runtime refresh cleared active publish job: %d", activeJobID)
	}
}

func TestDegradedCauseRecoveryIsScopedAndSurvivesSupervisorRestart(t *testing.T) {
	database := setupTestDB(t)
	account, err := database.CreateAccount("xhs", "degraded fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC().Add(-time.Second)
	instance := &Instance{
		ProfileID: ProfileIDForAccount(account.ID), AccountID: account.ID, PID: os.Getpid(), CDPPort: 15190,
		CDPURL: "http://127.0.0.1:15190", State: StateReady, StartedAt: startedAt,
		LastSeenAt: startedAt, LeaseOwner: "degraded-test-owner",
	}
	first := &Supervisor{instances: map[string]*Instance{instance.ProfileID: instance}, db: database, dataDir: t.TempDir()}
	if err := first.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	if err := first.markDegradedCause(instance, degradationCauseFocus, "focus fixture"); err != nil {
		t.Fatal(err)
	}
	if err := first.markDegradedCause(instance, degradationCauseHeartbeat, "heartbeat fixture"); err != nil {
		t.Fatal(err)
	}
	if instance.State != StateDegraded {
		t.Fatalf("in-memory state was not degraded: %s", instance.State)
	}
	facade := &BrowserFacade{sup: first, db: database}
	status, err := facade.Status(account.ID)
	if err != nil || status.State != StateDegraded {
		t.Fatalf("in-memory facade status=%+v err=%v", status, err)
	}

	// A fresh Supervisor has no in-memory Instance. The paired browser_events
	// remain the cause source of truth for the live external runtime row.
	restarted := &Supervisor{instances: make(map[string]*Instance), db: database, dataDir: t.TempDir()}
	state, err := restarted.recoverRuntimeDegradation(
		instance.ProfileID, instance.PID, instance.CDPPort, runtimeStartedAt(instance), instance.LeaseOwner, degradationCauseHeartbeat,
	)
	if err != nil || state != StateDegraded {
		t.Fatalf("heartbeat recovery cleared another cause: state=%s err=%v", state, err)
	}
	causes, err := restarted.activeDegradationCauses(instance.ProfileID, instance.PID, runtimeStartedAt(instance))
	if err != nil || !causes[degradationCauseFocus] || causes[degradationCauseHeartbeat] {
		t.Fatalf("causes=%v err=%v", causes, err)
	}

	state, err = restarted.recoverRuntimeDegradation(
		instance.ProfileID, instance.PID, instance.CDPPort, runtimeStartedAt(instance), instance.LeaseOwner, degradationCauseFocus,
	)
	if err != nil || state != StateReady {
		t.Fatalf("last cause recovery did not restore ready: state=%s err=%v", state, err)
	}
	var persisted string
	if err := database.QueryRow(`SELECT state FROM browser_runtime_instances WHERE profile_id=?`, instance.ProfileID).Scan(&persisted); err != nil || persisted != string(StateReady) {
		t.Fatalf("persisted state=%q err=%v", persisted, err)
	}
}

func TestFocusOperationFailureDoesNotDegradeHealthyBrowser(t *testing.T) {
	database := setupTestDB(t)
	account, err := database.CreateAccount("csdn", "focus fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC().Add(-time.Second)
	instance := &Instance{
		ProfileID: ProfileIDForAccount(account.ID), AccountID: account.ID, PID: os.Getpid(), CDPPort: 15193,
		CDPURL: "http://127.0.0.1:15193", State: StateReady, StartedAt: startedAt,
		LastSeenAt: startedAt, LeaseOwner: "focus-operation-owner",
	}
	sup := &Supervisor{instances: map[string]*Instance{instance.ProfileID: instance}, db: database, dataDir: t.TempDir()}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	sup.recordFocusOperationFailure(instance.ProfileID, instance.PID, instance.CDPPort)
	if instance.State != StateReady {
		t.Fatalf("focus operation polluted runtime health: %s", instance.State)
	}
	var state, eventType string
	if err := database.QueryRow(`SELECT state FROM browser_runtime_instances WHERE profile_id=?`, instance.ProfileID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT event_type FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, instance.ProfileID).Scan(&eventType); err != nil {
		t.Fatal(err)
	}
	if state != string(StateReady) || eventType != browserEventFocusOperationFailed {
		t.Fatalf("state=%q event=%q", state, eventType)
	}
}

func TestRecordFocusSucceededPersistsLastFocusedAccountAndLifecycleEvent(t *testing.T) {
	database := setupTestDB(t)
	account, err := database.CreateAccount("csdn", "focus success fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{db: database}
	sup.recordFocusSucceeded(account.ID, ProfileIDForAccount(account.ID), os.Getpid(), 15192)

	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastFocusedAt == "" {
		t.Fatal("successful account switch did not persist last_focused_at")
	}
	var eventType string
	if err := database.QueryRow(`SELECT event_type FROM browser_events WHERE profile_id=? ORDER BY id DESC LIMIT 1`, ProfileIDForAccount(account.ID)).Scan(&eventType); err != nil {
		t.Fatal(err)
	}
	if eventType != browserEventFocusSucceeded {
		t.Fatalf("event_type=%q", eventType)
	}
}

func TestHealthyStatusRecoversLegacyFocusOnlyDegradation(t *testing.T) {
	database := setupTestDB(t)
	account, err := database.CreateAccount("csdn", "legacy focus fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	startedAt := time.Now().UTC().Add(-time.Second)
	instance := &Instance{
		ProfileID: ProfileIDForAccount(account.ID), AccountID: account.ID, PID: os.Getpid(), CDPPort: 15194,
		CDPURL: server.URL, State: StateReady, StartedAt: startedAt,
		LastSeenAt: startedAt, LeaseOwner: "legacy-focus-owner",
	}
	sup := &Supervisor{instances: map[string]*Instance{instance.ProfileID: instance}, db: database, dataDir: t.TempDir()}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	if err := sup.markDegradedCause(instance, degradationCauseFocus, "legacy focus fixture"); err != nil {
		t.Fatal(err)
	}
	facade := &BrowserFacade{sup: sup, db: database}
	status, err := facade.Status(account.ID)
	if err != nil || status.State != StateReady || instance.State != StateReady {
		t.Fatalf("focus-only state did not recover: status=%+v instance=%s err=%v", status, instance.State, err)
	}
	if err := sup.markDegradedCause(instance, degradationCauseUnknown, "unknown fixture"); err != nil {
		t.Fatal(err)
	}
	status, err = facade.Status(account.ID)
	if err != nil || status.State != StateDegraded {
		t.Fatalf("unknown degradation was incorrectly cleared: status=%+v err=%v", status, err)
	}
}

func TestAbsentRecoveryCauseDoesNotOverwriteBusyRuntime(t *testing.T) {
	database := setupTestDB(t)
	startedAt := time.Now().UTC().Add(-time.Second)
	instance := &Instance{
		ProfileID: "acct-busy-recovery", AccountID: 20, PID: os.Getpid(), CDPPort: 15192,
		CDPURL: "http://127.0.0.1:15192", State: StateBusy, StartedAt: startedAt,
		LastSeenAt: startedAt, LeaseOwner: "busy-owner",
	}
	sup := &Supervisor{instances: map[string]*Instance{instance.ProfileID: instance}, db: database, dataDir: t.TempDir()}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	state, err := sup.recoverRuntimeDegradation(instance.ProfileID, instance.PID, instance.CDPPort, runtimeStartedAt(instance), instance.LeaseOwner, degradationCauseHeartbeat)
	if err != nil || state != StateBusy || instance.State != StateBusy {
		t.Fatalf("state=%s inMemory=%s err=%v", state, instance.State, err)
	}
}

func TestSupervisorRuntimeRefreshCannotClearStoppingFence(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	instance := &Instance{
		ProfileID: "acct-stopping", AccountID: 8, PID: 12346, CDPPort: 15108,
		CDPURL: "http://127.0.0.1:15108", State: StateReady, LeaseOwner: "ui-8", StartedAt: time.Now().UTC(),
	}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE browser_runtime_instances SET state='stopping' WHERE profile_id=?`, instance.ProfileID); err != nil {
		t.Fatal(err)
	}
	if err := sup.upsertRuntimeInstance(instance); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := d.QueryRow(`SELECT state FROM browser_runtime_instances WHERE profile_id=?`, instance.ProfileID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "stopping" {
		t.Fatalf("runtime refresh cleared stopping fence: %q", state)
	}
	replacement := &Instance{
		ProfileID: instance.ProfileID, AccountID: instance.AccountID, PID: 22346, CDPPort: 15118,
		CDPURL: "http://127.0.0.1:15118", State: StateReady, LeaseOwner: "ui-8-new", StartedAt: instance.StartedAt.Add(time.Minute),
	}
	if err := sup.upsertRuntimeInstance(replacement); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := d.QueryRow(`SELECT pid, state FROM browser_runtime_instances WHERE profile_id=?`, instance.ProfileID).Scan(&pid, &state); err != nil {
		t.Fatal(err)
	}
	if pid != replacement.PID || state != string(StateReady) {
		t.Fatalf("replacement identity inherited old fence: pid=%d state=%q", pid, state)
	}
}

func TestSupervisorOldRuntimeIdentityCannotDeleteReplacement(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	const profileID = "acct-replaced"
	const newStartedAt = "2026-07-17T10:00:00Z"
	if _, err := d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, 7, 22222, 15134, 'http://127.0.0.1:15134', 'ready', ?, ?, ?)`,
		profileID, newStartedAt, newStartedAt, newStartedAt); err != nil {
		t.Fatal(err)
	}
	removed, err := sup.removeRuntimeInstance(profileID, 11111, "2026-07-17T09:00:00Z", "old-owner")
	if err != nil || removed {
		t.Fatalf("old cleanup removed replacement: removed=%v err=%v", removed, err)
	}
	if err := sup.reserveRuntimeStop(profileID, 11111, "2026-07-17T09:00:00Z", "old-owner"); err == nil || !strings.Contains(err.Error(), string(errors.PROFILE_LOCKED)) {
		t.Fatalf("old stop identity error=%v", err)
	}
	var pid int
	var startedAt string
	if err := d.QueryRow(`SELECT pid, started_at FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&pid, &startedAt); err != nil {
		t.Fatal(err)
	}
	if pid != 22222 || startedAt != newStartedAt {
		t.Fatalf("replacement changed: pid=%d started=%s", pid, startedAt)
	}
}

func TestSupervisorOldLeaseGenerationCannotDeleteSamePIDAndTimeReplacement(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	const profileID = "acct-generation-replaced"
	const startedAt = "2026-07-17T10:00:00.123456789Z"
	if _, err := d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, 7, 22222, 15134, 'http://127.0.0.1:15134', 'ready', 'new-generation', ?, ?, ?)`,
		profileID, startedAt, startedAt, startedAt); err != nil {
		t.Fatal(err)
	}
	removed, err := sup.removeRuntimeInstance(profileID, 22222, startedAt, "old-generation")
	if err != nil || removed {
		t.Fatalf("old generation removed replacement: removed=%v err=%v", removed, err)
	}
	if err := sup.reserveRuntimeStop(profileID, 22222, startedAt, "old-generation"); err == nil || !strings.Contains(err.Error(), string(errors.PROFILE_LOCKED)) {
		t.Fatalf("old generation stop error=%v", err)
	}
	var owner, state string
	if err := d.QueryRow(`SELECT lease_owner, state FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&owner, &state); err != nil {
		t.Fatal(err)
	}
	if owner != "new-generation" || state != "ready" {
		t.Fatalf("replacement changed: owner=%q state=%q", owner, state)
	}
}

func TestSupervisorLaunchRejectsLiveExternalRuntimeEvenWithoutLease(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	const profileID = "acct-external-live"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, 7, ?, 15134, 'http://127.0.0.1:15134', 'ready', 'expired-generation', ?, ?, ?)`,
		profileID, cmd.Process.Pid, now, now, now); err != nil {
		t.Fatal(err)
	}
	launchErr := sup.ensureRuntimeLaunchAvailable(profileID)
	if launchErr == nil || !strings.Contains(launchErr.Error(), string(errors.ALREADY_RUNNING)) {
		t.Fatalf("live external runtime launch error=%v", launchErr)
	}
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("runtime count=%d err=%v", count, err)
	}
}

func TestSupervisorUpsertRuntimeInstanceReturnsDatabaseError(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	if _, err := d.Exec(`CREATE TRIGGER block_runtime_insert BEFORE INSERT ON browser_runtime_instances
		BEGIN SELECT RAISE(ABORT, 'runtime insert blocked'); END`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	inst := &Instance{
		ProfileID: "acct-persist-error", AccountID: 9, PID: 99999, CDPPort: 15144,
		CDPURL: "http://127.0.0.1:15144", State: StateStarting,
		LeaseOwner: "generation", StartedAt: now, LastSeenAt: now,
	}
	if err := sup.upsertRuntimeInstance(inst); err == nil || !strings.Contains(err.Error(), "runtime insert blocked") {
		t.Fatalf("upsert error=%v", err)
	}
}

func TestSupervisorStopReservationBlocksPublishClaim(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	const profileID = "acct-stop-reserved"
	const startedAt = "2026-07-17T11:00:00Z"
	if _, err := d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, 77, 33333, 15137, 'http://127.0.0.1:15137', 'ready', ?, ?, ?)`,
		profileID, startedAt, startedAt, startedAt); err != nil {
		t.Fatal(err)
	}
	if err := sup.reserveRuntimeStop(profileID, 33333, startedAt, ""); err != nil {
		t.Fatal(err)
	}
	if claimed, err := d.ClaimBrowserRuntimeJob(77, 99); err != nil || claimed {
		t.Fatalf("claim after stop reservation=%v err=%v", claimed, err)
	}
	var state string
	if err := d.QueryRow(`SELECT state FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&state); err != nil || state != "stopping" {
		t.Fatalf("state=%q err=%v", state, err)
	}
}

func TestSupervisor_ReconcileDeadPID(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()

	deadPID := 99999998
	if pidAlive(deadPID) {
		t.Skip("unexpected live PID")
	}
	d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES ('acct-dead', ?, 15120, 'http://127.0.0.1:15120', 'ready', 'h1', datetime('now'), datetime('now'), datetime('now'))`, deadPID)
	_ = sup.Leases().Acquire("acct-dead", HolderCLI, "h1", 3600*1e9)

	sup.Reconcile()

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id = 'acct-dead'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected runtime cleaned, got %d", n)
	}
}

func TestSupervisor_ReconcilePreservesRunningPublishOwner(t *testing.T) {
	d := setupTestDB(t)
	sup := NewSupervisor(d, "/nonexistent/browser", t.TempDir())
	defer sup.Shutdown()
	account, err := d.CreateAccount("xhs", "dead-publish-runtime", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := d.CreateContent("graph_text", "title", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := d.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	job, err := d.CreatePublishJobWithIntent(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, "", "prepare", "")
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := d.ClaimPublishJob(job.ID, "dead-runtime"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	const profileID = "acct-dead-publish"
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, active_job_id, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 99999997, 15132, 'http://127.0.0.1:15132', 'ready', ?, 'dead-owner', ?, ?, ?)`,
		profileID, account.ID, job.ID, now, now, now); err != nil {
		t.Fatal(err)
	}

	sup.Reconcile()
	var activeJobID int64
	if err := d.QueryRow(`SELECT active_job_id FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&activeJobID); err != nil {
		t.Fatal(err)
	}
	if activeJobID != job.ID {
		t.Fatalf("running owner marker changed: got=%d want=%d", activeJobID, job.ID)
	}
	if err := d.UpdatePublishJob(job.ID, "success", "done", "", "", 0); err != nil {
		t.Fatal(err)
	}
	sup.Reconcile()
	var remaining int
	if err := d.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances WHERE profile_id=?`, profileID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("terminal owner runtime was not cleaned: %d", remaining)
	}
}
