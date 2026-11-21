package db

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestBrowserRuntimeJobClaimIsExclusiveAndReclaimsTerminalOwner(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	account, err := database.CreateAccount("xhs", "runtime-lease", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "title", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	first, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ready', ?, ?, ?)`,
		"acct-runtime-lease", account.ID, 12345, 17001, "http://127.0.0.1:17001", now, now, now); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(first.ID, "first"); err != nil || !claimed {
		t.Fatalf("claim first publish job=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, first.ID); err != nil || !claimed {
		t.Fatalf("claim first runtime=%v err=%v", claimed, err)
	}
	if owned, err := database.VerifyBrowserRuntimeJob(account.ID, first.ID); err != nil || !owned {
		t.Fatalf("verify first runtime=%v err=%v", owned, err)
	}
	if owned, err := database.VerifyBrowserRuntimeJob(account.ID, second.ID); err != nil || owned {
		t.Fatalf("wrong owner verification=%v err=%v", owned, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, second.ID); err != nil || claimed {
		t.Fatalf("second runtime claim while first runs=%v err=%v", claimed, err)
	}

	if err := database.UpdatePublishJob(first.ID, "success", "done", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(second.ID, "second"); err != nil || !claimed {
		t.Fatalf("claim second publish job=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, second.ID); err != nil || !claimed {
		t.Fatalf("terminal owner must be reclaimed: claimed=%v err=%v", claimed, err)
	}
	if released, err := database.ReleaseBrowserRuntimeJob(account.ID, first.ID); err != nil || released {
		t.Fatalf("late first release must not clear second owner: released=%v err=%v", released, err)
	}
	if released, err := database.ReleaseBrowserRuntimeJob(account.ID, second.ID); err != nil || !released {
		t.Fatalf("release second runtime=%v err=%v", released, err)
	}
	if owned, err := database.VerifyBrowserRuntimeJob(account.ID, second.ID); err != nil || owned {
		t.Fatalf("released owner verification=%v err=%v", owned, err)
	}
}

func TestReleaseStoppedCancelledPublishLeaseRequiresUnclickedCancelledJob(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "cancelled-lease", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "title", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	job, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	if action, _, err := database.RequestPublishJobCancel(job.ID); err != nil || action != "cancelled" {
		t.Fatalf("cancel action=%q err=%v", action, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'stopped', ?, ?, ?)`, "acct-cancelled-lease", account.ID, 12345, 17001, "http://127.0.0.1:17001", now, now, now); err != nil {
		t.Fatal(err)
	}
	if acquired, err := database.AcquireBrowserOperationLease("acct-cancelled-lease", "publish", "publish-plan:1:1:test", time.Now(), time.Minute); err != nil || !acquired {
		t.Fatalf("lease acquired=%v err=%v", acquired, err)
	}
	released, err := database.ReleaseStoppedCancelledPublishLease(account.ID)
	if err != nil || !released {
		t.Fatalf("released=%v err=%v", released, err)
	}
	if acquired, err := database.AcquireBrowserOperationLease("acct-cancelled-lease", "publish", "publish-plan:1:1:next", time.Now(), time.Minute); err != nil || !acquired {
		t.Fatalf("lease was not released: acquired=%v err=%v", acquired, err)
	}
}

func TestBrowserRuntimeJobClaimRecoversExpiredExecutor(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "expired-executor", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "title", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	owner, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	next, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 12345, 17001, 'http://127.0.0.1:17001', 'ready', ?, ?, ?)`,
		"acct-expired-executor", account.ID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(owner.ID, "owner"); err != nil || !claimed {
		t.Fatalf("claim owner=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, owner.ID); err != nil || !claimed {
		t.Fatalf("claim owner runtime=%v err=%v", claimed, err)
	}
	stale := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if _, err := database.Exec(`UPDATE publish_jobs SET updated_at=? WHERE id=?`, stale, owner.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(next.ID, "next"); err != nil || !claimed {
		t.Fatalf("claim next=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, next.ID); err != nil || !claimed {
		t.Fatalf("reclaim expired runtime=%v err=%v", claimed, err)
	}
	recovered, err := database.GetPublishJob(owner.ID)
	if err != nil || recovered.Status != "failed" || recovered.ErrorCode != "PUBLISH_EXECUTOR_LOST" {
		t.Fatalf("recovered owner=%+v err=%v", recovered, err)
	}
	events, err := database.ListJobEventsByJob(owner.ID)
	if err != nil || len(events) != 1 || !events[0].Retryable || events[0].EventType != "job.retryable_failed" {
		t.Fatalf("recovery events=%+v err=%v", events, err)
	}
	if released, err := database.ReleaseBrowserRuntimeJob(account.ID, next.ID); err != nil || !released {
		t.Fatalf("release next=%v err=%v", released, err)
	}
	if err := database.UpdatePublishJob(next.ID, "success", "done", "", "", 0); err != nil {
		t.Fatal(err)
	}

	cancelledOwner, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	afterCancel, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(cancelledOwner.ID, "cancelled-owner"); err != nil || !claimed {
		t.Fatalf("claim cancelled owner=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, cancelledOwner.ID); err != nil || !claimed {
		t.Fatalf("claim cancelled owner runtime=%v err=%v", claimed, err)
	}
	if action, _, err := database.RequestPublishJobCancel(cancelledOwner.ID); err != nil || action != "cancel_requested" {
		t.Fatalf("cancel action=%q err=%v", action, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET updated_at=? WHERE id=?`, stale, cancelledOwner.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(afterCancel.ID, "after-cancel"); err != nil || !claimed {
		t.Fatalf("claim after cancel=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, afterCancel.ID); err != nil || !claimed {
		t.Fatalf("reclaim cancelled runtime=%v err=%v", claimed, err)
	}
	cancelled, err := database.GetPublishJob(cancelledOwner.ID)
	if err != nil || cancelled.Status != "cancelled" || cancelled.ErrorCode != "" {
		t.Fatalf("cancelled recovery=%+v err=%v", cancelled, err)
	}
	cancelEvents, err := database.ListJobEventsByJob(cancelledOwner.ID)
	if err != nil || len(cancelEvents) != 2 || cancelEvents[1].EventType != "job.cancelled" || cancelEvents[1].Retryable {
		t.Fatalf("cancel recovery events=%+v err=%v", cancelEvents, err)
	}
}

func TestBrowserRuntimeJobClaimDoesNotReassignUnknownSubmittedOutcome(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "submitted-executor", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "title", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	snapshotDigest := sha256.Sum256([]byte(`{}`))
	snapshotHash := fmt.Sprintf("%x", snapshotDigest[:])
	planResult, err := database.Exec(`INSERT INTO publish_plans
		(content_id, media_mode, snapshot_json, snapshot_hash, status, approved_action, concurrency, created_at, frozen_at, updated_at)
		VALUES (?, 'graph_text', '{}', ?, 'approved', 'prepare', 1, ?, ?, ?)`,
		content.ID, snapshotHash, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	planID, _ := planResult.LastInsertId()
	targetResult, err := database.Exec(`INSERT INTO publish_plan_targets
		(plan_id, account_id, asset_id, platform_code, settings_json, status, created_at, updated_at)
		VALUES (?, ?, ?, 'xhs', '{}', 'pending', ?, ?)`,
		planID, account.ID, assets[0].ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := targetResult.LastInsertId()
	prepareJob, created, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash, 0, content.ID, account.ID, assets[0].ID,
		"xhs", content.Type, "submitted-owner-prepare", "prepare", `{}`,
	)
	if err != nil || !created {
		t.Fatalf("prepare=%+v created=%v err=%v", prepareJob, created, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET status='awaiting_review', current_step='ready_to_submit' WHERE id=?`, prepareJob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET status='awaiting_review' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='approved', approved_action='submit' WHERE id=?`, planID); err != nil {
		t.Fatal(err)
	}
	owner, created, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash, 0, content.ID, account.ID, assets[0].ID,
		"xhs", content.Type, "submitted-owner", "submit", `{}`,
	)
	if err != nil || !created {
		t.Fatalf("owner=%+v created=%v err=%v", owner, created, err)
	}
	next, err := database.CreatePublishJob(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 12345, 17001, 'http://127.0.0.1:17001', 'ready', ?, ?, ?)`,
		"acct-submitted-executor", account.ID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(owner.ID, "owner"); err != nil || !claimed {
		t.Fatalf("claim owner=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, owner.ID); err != nil || !claimed {
		t.Fatalf("claim owner runtime=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(owner.ID); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if _, err := database.Exec(`UPDATE publish_jobs SET updated_at=? WHERE id=?`, stale, owner.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimPublishJob(next.ID, "next"); err != nil || !claimed {
		t.Fatalf("claim next=%v err=%v", claimed, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, next.ID); err != nil || claimed {
		t.Fatalf("unknown submitted outcome must retain runtime ownership: claimed=%v err=%v", claimed, err)
	}
	recovered, err := database.GetPublishJob(owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "awaiting_review" || recovered.CurrentStep != "creator_backend_verification" ||
		recovered.ErrorCode != "PUBLISH_RESULT_UNKNOWN" || recovered.SubmitOutcome != "unknown" {
		t.Fatalf("unknown submitted outcome was not fail-closed: %+v", recovered)
	}
	if owned, err := database.VerifyBrowserRuntimeJob(account.ID, owner.ID); err != nil || !owned {
		t.Fatalf("submitted job must keep runtime ownership pending verification: owned=%v err=%v", owned, err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(account.ID, next.ID); err != nil || claimed {
		t.Fatalf("subsequent claim bypassed unresolved submit fence: claimed=%v err=%v", claimed, err)
	}
	events, err := database.ListJobEventsByJob(owner.ID)
	if err != nil || len(events) != 1 || events[0].Retryable || events[0].EventType != "job.publish_outcome_unknown" {
		t.Fatalf("unknown outcome events=%+v err=%v", events, err)
	}
}
