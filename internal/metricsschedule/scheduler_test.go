package metricsschedule

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

func scheduleTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database
}

func seedScheduleAccount(t *testing.T, database *db.DB, identity int64, platform, loginState string) int64 {
	t.Helper()
	account, err := database.CreateAccountForIdentity(identity, platform, platform+"-acct", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET login_state=? WHERE id=?`, loginState, account.ID); err != nil {
		t.Fatal(err)
	}
	return account.ID
}

func TestSchedulerRunDisabledIsNoop(t *testing.T) {
	database := scheduleTestDB(t)
	var calls []int64
	scheduler := New(database, func(ctx context.Context, accountID int64) (string, error) {
		calls = append(calls, accountID)
		return "collected", nil
	})
	if _, err := scheduler.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("disabled schedule collected %v", calls)
	}
}

func TestSchedulerRunCollectsEligibleAndCountsResults(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(true, 24); err != nil {
		t.Fatal(err)
	}
	// identity 1 is seeded by migrate; a second identity holds the rest.
	identity2, err := database.CreateEgoIdentity("浏览器2")
	if err != nil {
		t.Fatal(err)
	}
	collectedAccount := seedScheduleAccount(t, database, 1, "douyin", "logged_in")
	skippedAccount := seedScheduleAccount(t, database, identity2.ID, "douyin", "logged_in")
	unsupportedAccount := seedScheduleAccount(t, database, identity2.ID, "bilibili", "logged_in")
	_ = unsupportedAccount

	var mu sync.Mutex
	var calls []int64
	states := map[int64]string{
		collectedAccount: "observed",
		skippedAccount:   "manual_takeover",
	}
	scheduler := New(database, func(ctx context.Context, accountID int64) (string, error) {
		mu.Lock()
		calls = append(calls, accountID)
		mu.Unlock()
		if state, ok := states[accountID]; ok {
			return state, nil
		}
		return "", fmt.Errorf("EGO_BROWSER_UNAVAILABLE")
	})
	result, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("collect calls=%v, want only the two supported-platform accounts", calls)
	}
	// bilibili is gated inside Run and counted as skipped; the collected
	// account reports the runtime's "observed" success state.
	if result.Eligible != 3 || result.Collected != 1 || result.Skipped != 2 || result.Failed != 0 {
		t.Fatalf("result=%+v", result)
	}
	settings, err := database.MetricsCollectionScheduleSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.State != "idle" || settings.LastFinishedAt == "" {
		t.Fatalf("run state not finished: %+v", settings)
	}
}

func TestSchedulerRunSkipsNotYetDue(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(true, 24); err != nil {
		t.Fatal(err)
	}
	// Simulate a finished run moments ago.
	fixedNow := time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)
	if _, err := database.BeginMetricsCollectionRun(fixedNow); err != nil {
		t.Fatal(err)
	}
	if err := database.FinishMetricsCollectionRun(fixedNow, "", db.MetricsCollectionResult{Eligible: 1, Collected: 1}); err != nil {
		t.Fatal(err)
	}
	seedScheduleAccount(t, database, 1, "douyin", "logged_in")
	var calls []int64
	scheduler := New(database, func(ctx context.Context, accountID int64) (string, error) {
		calls = append(calls, accountID)
		return "collected", nil
	})
	scheduler.now = func() time.Time { return fixedNow.Add(30 * time.Minute) }
	if _, err := scheduler.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("not-yet-due schedule collected %v", calls)
	}
	scheduler.now = func() time.Time { return fixedNow.Add(25 * time.Hour) }
	if _, err := scheduler.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 {
		t.Fatal("due schedule must collect")
	}
}

// The platform catalog gate stays in the scheduler: bilibili has no reviewed
// evidence catalog and must never reach the browser workflow.
func TestSchedulerPlatformGateMatchesCreatorCatalog(t *testing.T) {
	if egolite.SupportsCreatorMetrics("douyin") != true || egolite.SupportsCreatorMetrics("bilibili") != false {
		t.Fatal("creator metrics catalog drifted")
	}
}

func TestSchedulerRunCallsAfterRunHookOnceWithFinalCounters(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(true, 24); err != nil {
		t.Fatal(err)
	}
	account := seedScheduleAccount(t, database, 1, "douyin", "logged_in")
	scheduler := New(database, func(ctx context.Context, accountID int64) (string, error) {
		return "observed", nil
	})
	var hookCalls int
	var seen db.MetricsCollectionResult
	scheduler.SetAfterRun(func(ctx context.Context, result db.MetricsCollectionResult) error {
		hookCalls++
		seen = result
		return nil
	})
	if _, err := scheduler.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 1 {
		t.Fatalf("after-run hook calls=%d, want 1", hookCalls)
	}
	if seen.Collected != 1 || seen.Eligible != 1 {
		t.Fatalf("hook saw stale counters: %+v", seen)
	}
	if _, err := scheduler.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 1 {
		t.Fatalf("a not-due sweep must not run the hook again: calls=%d", hookCalls)
	}
	_ = account
}

func TestSchedulerRunSurfacesAfterRunHookFailureWithoutLosingCounters(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(true, 24); err != nil {
		t.Fatal(err)
	}
	seedScheduleAccount(t, database, 1, "douyin", "logged_in")
	scheduler := New(database, func(ctx context.Context, accountID int64) (string, error) {
		return "observed", nil
	})
	scheduler.SetAfterRun(func(context.Context, db.MetricsCollectionResult) error {
		return fmt.Errorf("feishu export failed")
	})
	result, err := scheduler.Run(context.Background())
	if err == nil {
		t.Fatal("hook failure must be reported")
	}
	if result == nil || result.Collected != 1 {
		t.Fatalf("collection counters lost: result=%+v", result)
	}
}

// 进程崩在 sweep 中间会把 state 留在 'running'，而 Begin 只翻 idle 行。没有恢复
// 路径的话该调度此后每次都以 ErrMetricsCollectionRunning 失败，再也不采集。
func TestSchedulerRecoverStaleRunResetsCrashedRow(t *testing.T) {
	database := scheduleTestDB(t)
	started := time.Now().UTC().Add(-80 * time.Hour) // 3 个 24h interval 之前
	settings, err := database.BeginMetricsCollectionRun(started)
	if err != nil {
		t.Fatal(err)
	}
	if settings.State != "running" {
		t.Fatalf("setup state=%q", settings.State)
	}
	scheduler := New(database, func(context.Context, int64) (string, error) {
		return "observed", nil
	})
	if err := scheduler.recoverStaleRun(settings, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var state, errorCode string
	if err := database.QueryRow(`SELECT state, last_error_code FROM metrics_collection_schedule WHERE id=1`).
		Scan(&state, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != "idle" {
		t.Fatalf("state=%q, a crashed run must be released", state)
	}
	// 恢复必须留下原因，不能伪装成一次正常完成。
	if errorCode != staleRunErrorCode {
		t.Fatalf("last_error_code=%q want %q", errorCode, staleRunErrorCode)
	}
	// 幂等：行已经不是 running，再调一次不能出错也不再改状态。
	if err := scheduler.recoverStaleRun(settings, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

// 还没超过三个 interval 的 running 行可能属于正在跑的 sweep，不能被抢。
func TestSchedulerRecoverStaleRunKeepsLiveRow(t *testing.T) {
	database := scheduleTestDB(t)
	settings, err := database.BeginMetricsCollectionRun(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	scheduler := New(database, func(context.Context, int64) (string, error) {
		return "observed", nil
	})
	if err := scheduler.recoverStaleRun(settings, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	state, err := database.MetricsCollectionScheduleSettings()
	if err != nil {
		t.Fatal(err)
	}
	if state.State != "running" {
		t.Fatalf("state=%q, a live sweep must not be reset", state.State)
	}
}

// 进程内正在跑的 sweep 不能被恢复逻辑抢走：行只在 Begin/Finish 刷新，长 sweep
// 看上去可能已经"过期"。
func TestSchedulerRecoverStaleRunSkipsSweepInFlight(t *testing.T) {
	database := scheduleTestDB(t)
	settings, err := database.BeginMetricsCollectionRun(time.Now().UTC().Add(-80 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := New(database, func(context.Context, int64) (string, error) {
		return "observed", nil
	})
	scheduler.markSweepInFlight(true)
	if err := scheduler.recoverStaleRun(settings, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	current, err := database.MetricsCollectionScheduleSettings()
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "running" {
		t.Fatalf("state=%q, an in-process sweep must keep its row", current.State)
	}
}

// 端到端：卡住的行被释放之后，同一次 Run 必须真的把账号采掉。
func TestSchedulerRunUnblocksStaleRunningRow(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(true, 24); err != nil {
		t.Fatal(err)
	}
	if _, err := database.BeginMetricsCollectionRun(time.Now().UTC().Add(-80 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	seedScheduleAccount(t, database, 1, "douyin", "logged_in")
	var calls int
	scheduler := New(database, func(context.Context, int64) (string, error) {
		calls++
		return "observed", nil
	})
	result, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("a stale running row must not block the sweep: %v", err)
	}
	if calls != 1 || result.Collected != 1 {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	settings, err := database.MetricsCollectionScheduleSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.State != "idle" || settings.LastFinishedAt == "" {
		t.Fatalf("run state not finished: %+v", settings)
	}
}

// 立刻失败的 sweep 不能 0 秒紧循环，也不能把错误吞掉。
func TestSchedulerStartReportsFailureAndBacksOff(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(true, 24); err != nil {
		t.Fatal(err)
	}
	// 一行仍然 running（远未到三个 interval）会让每次 Run 立刻失败。
	if _, err := database.BeginMetricsCollectionRun(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var delays []time.Duration
	var failures []error
	scheduler := New(database, func(context.Context, int64) (string, error) {
		t.Error("a wedged schedule must not start a browser collection")
		return "", nil
	})
	scheduler.onRunError = func(err error) {
		mu.Lock()
		failures = append(failures, err)
		mu.Unlock()
	}
	scheduler.after = func(delay time.Duration) <-chan time.Time {
		mu.Lock()
		delays = append(delays, delay)
		count := len(delays)
		mu.Unlock()
		if count >= 3 {
			cancel()
		}
		ready := make(chan time.Time, 1)
		ready <- time.Now()
		return ready
	}
	var waitGroup sync.WaitGroup
	scheduler.Start(ctx, &waitGroup)
	stopped := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("scheduler loop did not stop")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(failures) == 0 {
		t.Fatal("the loop swallowed a failed sweep")
	}
	if !errors.Is(failures[0], db.ErrMetricsCollectionRunning) {
		t.Fatalf("failure=%v", failures[0])
	}
	// delays[0] 是下一个 due 时间；失败之后必须出现接近 minRunGap 的退避。
	backoff := time.Duration(0)
	for _, delay := range delays[1:] {
		if delay > 0 && delay <= minRunGap {
			backoff = delay
			break
		}
	}
	if backoff < minRunGap/2 {
		t.Fatalf("delays=%v, want a ~%s backoff after the failure", delays, minRunGap)
	}
}

// 「立即采集」必须真的采，而不是被 24 小时间隔挡回一个全 0 的空结果。
func TestSchedulerRunNowIgnoresCooldown(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(true, 24); err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)
	if _, err := database.BeginMetricsCollectionRun(fixedNow); err != nil {
		t.Fatal(err)
	}
	if err := database.FinishMetricsCollectionRun(fixedNow, "", db.MetricsCollectionResult{Eligible: 1, Collected: 1}); err != nil {
		t.Fatal(err)
	}
	seedScheduleAccount(t, database, 1, "douyin", "logged_in")
	var calls []int64
	scheduler := New(database, func(ctx context.Context, accountID int64) (string, error) {
		calls = append(calls, accountID)
		return "observed", nil
	})
	scheduler.now = func() time.Time { return fixedNow.Add(30 * time.Minute) }
	result, err := scheduler.RunNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 || result == nil || result.Collected == 0 {
		t.Fatalf("manual run must collect despite the cooldown: calls=%v result=%+v", calls, result)
	}
}

// 用户把定时采集关掉之后，手动按钮也不该绕过这个开关去起浏览器。
func TestSchedulerRunNowRespectsDisabledSwitch(t *testing.T) {
	database := scheduleTestDB(t)
	if _, err := database.UpdateMetricsCollectionSchedule(false, 24); err != nil {
		t.Fatal(err)
	}
	seedScheduleAccount(t, database, 1, "douyin", "logged_in")
	calls := 0
	scheduler := New(database, func(ctx context.Context, accountID int64) (string, error) {
		calls++
		return "collected", nil
	})
	result, err := scheduler.RunNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("disabled schedule must not collect, got %d calls", calls)
	}
	if result == nil || result.Eligible != 0 || result.Collected != 0 {
		t.Fatalf("disabled schedule result=%+v", result)
	}
}
