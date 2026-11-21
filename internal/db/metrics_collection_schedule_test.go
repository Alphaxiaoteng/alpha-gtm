package db

import (
	"testing"
	"time"
)

func testScheduleDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestMetricsCollectionScheduleDefaultsAndRoundTrip(t *testing.T) {
	database := testScheduleDB(t)
	settings, err := database.MetricsCollectionScheduleSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Enabled {
		t.Fatal("auto collection must default to disabled")
	}
	if settings.IntervalHours != 24 {
		t.Fatalf("interval hours %d, want 24", settings.IntervalHours)
	}
	if settings.LastFinishedAt != "" || settings.LastStartedAt != "" {
		t.Fatalf("fresh schedule carries run state: %+v", settings)
	}
	updated, err := database.UpdateMetricsCollectionSchedule(true, 12)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled || updated.IntervalHours != 12 {
		t.Fatalf("update lost: %+v", updated)
	}
	if _, err := database.UpdateMetricsCollectionSchedule(true, 0); err == nil {
		t.Fatal("interval below 1 must be rejected")
	}
	if _, err := database.UpdateMetricsCollectionSchedule(true, 169); err == nil {
		t.Fatal("interval above 168 must be rejected")
	}
}

func TestMetricsCollectionScheduleRunLifecycle(t *testing.T) {
	database := testScheduleDB(t)
	now := time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)
	if _, err := database.BeginMetricsCollectionRun(now); err != nil {
		t.Fatal(err)
	}
	if err := database.FinishMetricsCollectionRun(now.Add(3*time.Minute), "", MetricsCollectionResult{Eligible: 5, Collected: 3, Skipped: 2, Failed: 0}); err != nil {
		t.Fatal(err)
	}
	settings, err := database.MetricsCollectionScheduleSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.State != "idle" || settings.LastFinishedAt == "" {
		t.Fatalf("run lifecycle not persisted: %+v", settings)
	}
	// A second begin must succeed (state back to idle) — the monitor runs
	// serialized per process, the DB flag only guards stale crash recovery.
	if _, err := database.BeginMetricsCollectionRun(now.Add(time.Hour)); err != nil {
		t.Fatalf("second begin rejected: %v", err)
	}
}

func TestEligibleCreatorMetricAccountsFiltersStateAndFreshness(t *testing.T) {
	database := testScheduleDB(t)
	now := time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)
	identity2, err := database.CreateEgoIdentity("浏览器2")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(identity int64, platform, loginState string) int64 {
		account, err := database.CreateAccountForIdentity(identity, platform, platform+"-acct", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE accounts SET login_state=? WHERE id=?`, loginState, account.ID); err != nil {
			t.Fatal(err)
		}
		return account.ID
	}
	fresh := mk(1, "douyin", "logged_in")
	stale := mk(identity2.ID, "douyin", "logged_in")
	_ = mk(identity2.ID, "xhs", "logged_out")
	unsupported := mk(identity2.ID, "bilibili", "logged_in")
	disabled := mk(identity2.ID, "kuaishou", "logged_in")
	if _, err := database.Exec(`UPDATE accounts SET enabled=0 WHERE id=?`, disabled); err != nil {
		t.Fatal(err)
	}
	dated := func(accountID int64, platform, date string) {
		if _, err := database.Exec(`INSERT INTO creator_metric_snapshots (account_id, platform_code, snapshot_date, window_days, range_verified, followers, views, likes, comments, shares, favorites, posts, evidence_code, raw_json, collected_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, accountID, platform, date, 7, 1, 1, 2, 3, 4, 5, 6, 0, "REVIEWED_ANALYTICS_DOM", "{}", date+"T00:00:00Z", date+"T00:00:00Z", date+"T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	dated(fresh, "douyin", "2026-09-14")
	dated(stale, "douyin", "2026-09-10")

	candidates, err := database.StaleLoggedInAccounts(now, 24)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]string{}
	for _, candidate := range candidates {
		got[candidate.ID] = candidate.PlatformCode
	}
	// fresh skipped (today snapshot), logged-out skipped, disabled skipped;
	// stale and never-collected remain. bilibili stays eligible at the DB
	// layer — platform filtering belongs to the scheduler.
	if len(candidates) != 2 {
		t.Fatalf("candidates=%+v, want exactly stale + unsupported", candidates)
	}
	if _, ok := got[fresh]; ok {
		t.Fatalf("fresh account %d must be skipped", fresh)
	}
	if _, ok := got[stale]; !ok {
		t.Fatalf("stale account %d missing", stale)
	}
	if _, ok := got[unsupported]; !ok {
		t.Fatalf("never-collected account %d missing", unsupported)
	}
}
