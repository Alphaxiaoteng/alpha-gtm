package db_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func createFrozenLinkedJob(t *testing.T, database *db.DB, suffix, mode string) (*db.PublishJob, int64, int64, string) {
	t.Helper()
	account, err := database.CreateAccount("xhs", "frozen-"+suffix, "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "frozen title", "frozen caption", "frozen body", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	snapshotDigest := sha256.Sum256([]byte(`{}`))
	snapshotHash := fmt.Sprintf("%x", snapshotDigest[:])
	planResult, err := database.Exec(`INSERT INTO publish_plans
		(content_id, media_mode, snapshot_json, snapshot_hash, status, approved_action, concurrency, created_at, frozen_at, updated_at)
		VALUES (?, 'graph_text', '{}', ?, 'approved', 'prepare', 1, ?, ?, ?)`,
		content.ID, snapshotHash, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	planID, err := planResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	targetResult, err := database.Exec(`INSERT INTO publish_plan_targets
		(plan_id, account_id, asset_id, platform_code, settings_json, status, created_at, updated_at)
		VALUES (?, ?, ?, 'xhs', '{"visibility":"public"}', 'pending', ?, ?)`,
		planID, account.ID, assets[0].ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := targetResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "submit" {
		prepareJob, created, err := database.CreatePublishPlanJobWithIntentResult(
			planID, targetID, snapshotHash,
			0, content.ID, account.ID, assets[0].ID, "xhs", content.Type,
			"frozen:"+suffix+":prepare", "prepare", `{"visibility":"public"}`,
		)
		if err != nil || !created {
			t.Fatalf("prepare job=%+v created=%v err=%v", prepareJob, created, err)
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
	}
	job, created, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash,
		0, content.ID, account.ID, assets[0].ID, "xhs", content.Type,
		"frozen:"+suffix+":0001", mode, `{"visibility":"public"}`,
	)
	if err != nil || !created {
		t.Fatalf("job=%+v created=%v err=%v", job, created, err)
	}
	return job, planID, targetID, snapshotHash
}

func TestPublishPlanJobLinkIsImmutableAndPartOfIdempotentIntent(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, planID, targetID, snapshotHash := createFrozenLinkedJob(t, database, "immutable-link", "prepare")
	if job.PlanID != planID || job.PlanTargetID != targetID || job.SnapshotHash != snapshotHash {
		t.Fatalf("frozen link missing: %+v", job)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET snapshot_hash=? WHERE id=?`, strings.Repeat("b", 64), job.ID); err == nil {
		t.Fatal("frozen publish job snapshot link was mutable")
	}
	if _, _, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID+999, snapshotHash,
		0, job.ContentID, job.AccountID, job.AssetID, job.PlatformCode, job.ContentType,
		job.IdempotencyKey, job.Mode, job.SettingsJSON,
	); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("idempotency key accepted a different frozen target: %v", err)
	}
}

func TestCancelledUnsubmittedPrepareCanBeRecreatedFromFrozenSnapshot(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	first, planID, targetID, snapshotHash := createFrozenLinkedJob(t, database, "cancelled-prepare", "prepare")
	if _, err := database.Exec(`UPDATE publish_jobs SET status='cancelled', current_step='cancelled' WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET status='cancelled' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	second, created, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash,
		0, first.ContentID, first.AccountID, first.AssetID, first.PlatformCode, first.ContentType,
		"frozen:cancelled-prepare:0002", "prepare", first.SettingsJSON,
	)
	if err != nil || !created || second.ID == first.ID {
		t.Fatalf("second=%+v created=%v err=%v", second, created, err)
	}
	var targetJobID int64
	if err := database.QueryRow(`SELECT publish_job_id FROM publish_plan_targets WHERE id=?`, targetID).Scan(&targetJobID); err != nil || targetJobID != second.ID {
		t.Fatalf("target job=%d want=%d err=%v", targetJobID, second.ID, err)
	}
}

func TestSubmitAttemptIsSingleUseAndDisablesBlindRetry(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "submit-once", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "submit-once"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET submit_attempted_at='' WHERE id=?`, job.ID); err == nil {
		t.Fatal("durable submit marker was cleared")
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("second submit attempt err=%v", err)
	}
	if _, err := database.FinalizePublishJob(job.ID, "failed", "submit", "PUBLISH_RESULT_UNKNOWN", "publish outcome needs verification", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "error", Step: "submit", Type: "job.retryable_failed",
		Message: "outcome unknown", ErrorCode: "PUBLISH_RESULT_UNKNOWN", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if retryable, err := database.CanRetryPublishJob(job.ID); err != nil || retryable {
		t.Fatalf("submit-attempted job retryable=%v err=%v", retryable, err)
	}
	if _, _, err := database.RetryPublishJob(job.ID, "blind-retry"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("blind retry err=%v", err)
	}
	if _, err := database.PreparePublishJobResume(job.ID, "blind-resume"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("blind resume err=%v", err)
	}
}

func TestExplicitUnclickedSubmitReleaseIsAtomicAndRecoverable(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, targetID, _ := createFrozenLinkedJob(t, database, "submit-not-clicked", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "submit-not-clicked"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.UpdatePublishJob(job.ID, "running", "submit_approved", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.ReleasePublishSubmitReservation(job.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "manual_takeover" || updated.CurrentStep != "submit_not_clicked" ||
		updated.SubmitAttemptedAt != "" || updated.SubmitOutcome != "not_clicked" {
		t.Fatalf("released job is not recoverable atomically: %+v", updated)
	}
	var targetStatus string
	if err := database.QueryRow(`SELECT status FROM publish_plan_targets WHERE id=?`, targetID).Scan(&targetStatus); err != nil {
		t.Fatal(err)
	}
	if targetStatus != "manual_takeover" {
		t.Fatalf("released target status=%q", targetStatus)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("released job bypassed a new reviewed submit job: %v", err)
	}
}

func TestUnknownSubmitOutcomeCannotReleaseReservation(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "submit-unknown", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "submit-unknown"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.UpdatePublishJob(job.ID, "running", "submit_approved", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.SetPublishSubmitOutcome(job.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := database.ReleasePublishSubmitReservation(job.ID); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("unknown outcome reservation was released: %v", err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SubmitAttemptedAt == "" || updated.SubmitOutcome != "unknown" {
		t.Fatalf("unknown outcome lost durable click gate: %+v", updated)
	}
}

func TestResultUnknownSubmitAttemptCannotAutoRetry(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "result-unknown-no-retry", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "result-unknown-no-retry"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdatePublishJob(job.ID, "result_unknown", "creator_backend_verification", "PUBLISH_RESULT_UNKNOWN", "", 0); err != nil {
		t.Fatal(err)
	}
	if retryable, err := database.CanRetryPublishJob(job.ID); err != nil || retryable {
		t.Fatalf("result_unknown retryable=%v err=%v", retryable, err)
	}
	if _, _, err := database.RetryPublishJob(job.ID, "result-unknown-retry"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("result_unknown retry err=%v", err)
	}
	if _, err := database.PreparePublishJobResume(job.ID, "result-unknown-resume"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("result_unknown resume err=%v", err)
	}
}

func TestCreatorBackendNegativeEvidenceResolvesAttemptWithoutClearingMarker(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "verified-absent", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "verified-absent"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join(t.TempDir(), "manager.png")
	evidence := []byte("creator-manager-negative-evidence")
	if err := os.WriteFile(evidencePath, evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(evidence)
	if err := database.ResolvePublishSubmitNotPublished(job.ID, evidencePath, fmt.Sprintf("%x", digest[:])); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "failed" || updated.CurrentStep != "creator_backend_verified_absent" ||
		updated.ErrorCode != "CREATOR_BACKEND_NOT_FOUND" ||
		updated.SubmitAttemptedAt == "" || updated.SubmitOutcome != "verified_not_published" {
		t.Fatalf("updated=%+v", updated)
	}
	events, err := database.ListJobEventsByJob(job.ID)
	if err != nil || len(events) != 1 ||
		events[0].EventType != "job.creator_backend_not_published" ||
		events[0].ScreenshotPath != evidencePath {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestCreatorBackendScheduledEvidenceResolvesTestAndKeepsItOutOfPublishRecords(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, planID, targetID, _ := createFrozenLinkedJob(t, database, "scheduled-verified", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "scheduled-verified"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join(t.TempDir(), "scheduled.png")
	cancelledPath := filepath.Join(t.TempDir(), "cancelled.png")
	evidence := []byte("creator-manager-scheduled-evidence")
	cancelled := []byte("creator-manager-cancelled-evidence")
	if err := os.WriteFile(evidencePath, evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cancelledPath, cancelled, 0o600); err != nil {
		t.Fatal(err)
	}
	evidenceDigest := sha256.Sum256(evidence)
	cancelledDigest := sha256.Sum256(cancelled)
	if err := database.ResolvePublishSubmitScheduled(
		job.ID,
		evidencePath, fmt.Sprintf("%x", evidenceDigest[:]),
		cancelledPath, fmt.Sprintf("%x", cancelledDigest[:]),
	); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "success" || updated.CurrentStep != "creator_backend_scheduled_verified" ||
		updated.ErrorCode != "" || updated.SubmitAttemptedAt == "" ||
		updated.SubmitOutcome != "scheduled_verified" {
		t.Fatalf("updated=%+v", updated)
	}
	var targetStatus, planStatus string
	if err := database.QueryRow(`SELECT status FROM publish_plan_targets WHERE id=?`, targetID).Scan(&targetStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status FROM publish_plans WHERE id=?`, planID).Scan(&planStatus); err != nil {
		t.Fatal(err)
	}
	if targetStatus != "scheduled_verified" || planStatus != "scheduled_verified" {
		t.Fatalf("target=%q plan=%q", targetStatus, planStatus)
	}
	var published int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&published); err != nil || published != 0 {
		t.Fatalf("published=%d err=%v", published, err)
	}
	events, err := database.ListJobEventsByJob(job.ID)
	if err != nil || len(events) != 1 ||
		events[0].EventType != "job.creator_backend_scheduled_verified" ||
		events[0].ScreenshotPath != evidencePath {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestSubmitAttemptCannotOvertakeDurableCancellation(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "cancel-before-submit", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "cancel-before-submit"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if action, _, err := database.RequestPublishJobCancel(job.ID); err != nil || action != "cancel_requested" {
		t.Fatalf("cancel action=%q err=%v", action, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("submit gate overtook cancellation: %v", err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil || updated.SubmitAttemptedAt != "" {
		t.Fatalf("cancelled job has submit marker: job=%+v err=%v", updated, err)
	}
}

func TestSubmitAttemptRequiresFrozenPlanBinding(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job := createControllablePublishJobWithMode(t, database, "unfrozen-submit", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "unfrozen-submit"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("unfrozen submit reached irreversible gate: %v", err)
	}
}

func TestPublishRecordRequiresIndependentCreatorBackendEvidence(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, planID, _, _ := createFrozenLinkedJob(t, database, "creator-evidence", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "creator-evidence"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 12345, 15101, 'http://127.0.0.1:15101', 'ready', ?, ?, ?)`,
		"creator-evidence-runtime", job.AccountID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if claimed, err := database.ClaimBrowserRuntimeJob(job.AccountID, job.ID); err != nil || !claimed {
		t.Fatalf("claim runtime=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.FinalizePublishJob(job.ID, "awaiting_review", "creator_backend_verification", "PUBLISH_VERIFICATION_REQUIRED", "creator backend verification required", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreatePublishRecord(job.ID, job.ContentID, job.AccountID, job.AssetID, job.PlatformCode, job.ContentType, "published", "https://www.xiaohongshu.com/explore/note-1", "note-1"); err == nil {
		t.Fatal("unverified adapter result created a publish record")
	}
	if _, err := database.RecordCreatorBackendPublished(job.ID, db.CreatorBackendEvidence{
		ObservedStatus: "Published",
		PostURL:        "https://www.xiaohongshu.com/explore/note-1",
		ExternalPostID: "note-1",
	}); err == nil {
		t.Fatal("creator evidence without screenshot was accepted")
	}
	screenshotDir := filepath.Join(database.DataDir, "screenshots")
	if err := os.MkdirAll(screenshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	screenshotPath := filepath.Join(screenshotDir, "published-note-1.png")
	if err := os.WriteFile(screenshotPath, []byte("reviewed creator backend screenshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence := db.CreatorBackendEvidence{
		ObservedStatus: "Published",
		PostURL:        "https://www.xiaohongshu.com/explore/note-1",
		ExternalPostID: "note-1",
		ScreenshotPath: screenshotPath,
	}
	if _, err := database.RecordCreatorBackendPublished(job.ID, evidence); err == nil {
		t.Fatal("XHS creator evidence without durable actual-click evidence was accepted")
	}
	refreshed, err := database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	clickedAt, err := time.Parse(time.RFC3339Nano, refreshed.SubmitAttemptedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "info", Step: "creator_backend_verification", Type: "job.awaiting_review",
		RawJSON: fmt.Sprintf(`{"submitAttempted":true,"submitClicked":true,"submitClickedAt":%q,"submitTargetId":"A4D362607FC76673DA07AE900173A0F3"}`,
			clickedAt.Add(time.Second).Format(time.RFC3339Nano)),
	}); err != nil {
		t.Fatal(err)
	}
	record, err := database.RecordCreatorBackendPublished(job.ID, db.CreatorBackendEvidence{
		ObservedStatus: "Published",
		PostURL:        "https://www.xiaohongshu.com/explore/note-1",
		ExternalPostID: "note-1",
		ScreenshotPath: screenshotPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "published" || record.Source != "creator_backend" || record.PostURL == "" || record.ExternalPostID != "note-1" {
		t.Fatalf("record=%+v", record)
	}
	contentRecords, err := database.ListPublishContentRecords(10)
	if err != nil || len(contentRecords) != 1 {
		t.Fatalf("content records=%+v err=%v", contentRecords, err)
	}
	if contentRecords[0].JobID != job.ID ||
		contentRecords[0].RemoteState != "published" ||
		contentRecords[0].PostURL != record.PostURL ||
		contentRecords[0].ExternalPostID != record.ExternalPostID ||
		contentRecords[0].EvidencePath == "" {
		t.Fatalf("content record=%+v", contentRecords[0])
	}
	replayed, err := database.RecordCreatorBackendPublished(job.ID, db.CreatorBackendEvidence{
		ObservedStatus: "Published",
		PostURL:        "https://www.xiaohongshu.com/explore/note-1",
		ExternalPostID: "note-1",
		ScreenshotPath: screenshotPath,
	})
	if err != nil || replayed.ID != record.ID {
		t.Fatalf("verified evidence replay=%+v err=%v", replayed, err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil || updated.Status != "published" || updated.CurrentStep != "creator_backend_verified" || updated.SubmitOutcome != "verified_published" {
		t.Fatalf("job=%+v err=%v", updated, err)
	}
	var planStatus, targetStatus string
	if err := database.QueryRow(`SELECT status FROM publish_plans WHERE id=?`, planID).Scan(&planStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status FROM publish_plan_targets WHERE publish_job_id=?`, job.ID).Scan(&targetStatus); err != nil {
		t.Fatal(err)
	}
	if planStatus != "published" || targetStatus != "published" {
		t.Fatalf("plan=%q target=%q", planStatus, targetStatus)
	}
	if owned, err := database.VerifyBrowserRuntimeJob(job.AccountID, job.ID); err != nil || owned {
		t.Fatalf("verified publication must release runtime ownership: owned=%v err=%v", owned, err)
	}
	events, err := database.ListJobEventsByJob(job.ID)
	if err != nil || len(events) != 2 ||
		events[1].EventType != "job.creator_backend_verified" || events[1].ScreenshotPath == "" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	var operationEvent db.OperationEvent
	if err := database.QueryRow(`SELECT trace_id, account_id, platform_code, component, event_type, step, status, level
		FROM operation_events WHERE event_type='publish.creator_backend_verified' AND account_id=?`, job.AccountID).
		Scan(&operationEvent.TraceID, &operationEvent.AccountID, &operationEvent.PlatformCode, &operationEvent.Component,
			&operationEvent.EventType, &operationEvent.Step, &operationEvent.Status, &operationEvent.Level); err != nil {
		t.Fatalf("published operation event: %v", err)
	}
	if operationEvent.PlatformCode != job.PlatformCode || operationEvent.Step != "published_verified" || operationEvent.Status != "published" {
		t.Fatalf("operationEvent=%+v", operationEvent)
	}
}

func TestCreatorBackendPublishedEvidenceAtomicallyResolvesPostClickStates(t *testing.T) {
	for _, initialStatus := range []string{"submitted_pending_verification", "result_unknown"} {
		t.Run(initialStatus, func(t *testing.T) {
			database, cleanup := setupDB(t)
			defer cleanup()
			if err := database.Migrate(); err != nil {
				t.Fatal(err)
			}
			job, planID, targetID, _ := createFrozenLinkedJob(t, database, "creator-state-"+initialStatus, "submit")
			if claimed, err := database.ClaimPublishJob(job.ID, "creator-state-"+initialStatus); err != nil || !claimed {
				t.Fatalf("claim=%v err=%v", claimed, err)
			}
			if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
				t.Fatal(err)
			}
			if err := database.UpdatePublishJob(job.ID, initialStatus, "creator_backend_verification", "", "", 0); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE publish_plan_targets SET status=? WHERE id=?`, initialStatus, targetID); err != nil {
				t.Fatal(err)
			}
			screenshotDir := filepath.Join(database.DataDir, "screenshots")
			if err := os.MkdirAll(screenshotDir, 0o700); err != nil {
				t.Fatal(err)
			}
			screenshotPath := filepath.Join(screenshotDir, "published-"+initialStatus+".png")
			if err := os.WriteFile(screenshotPath, []byte("creator backend published evidence"), 0o600); err != nil {
				t.Fatal(err)
			}
			refreshed, err := database.GetPublishJob(job.ID)
			if err != nil {
				t.Fatal(err)
			}
			clickedAt, err := time.Parse(time.RFC3339Nano, refreshed.SubmitAttemptedAt)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateJobEventDetailed(db.JobEventInput{
				JobID: job.ID, Level: "info", Step: "submit", Type: "job.submit_clicked",
				RawJSON: fmt.Sprintf(`{"submitAttempted":true,"submitClicked":true,"submitClickedAt":%q,"submitTargetId":"A4D362607FC76673DA07AE900173A0F3"}`,
					clickedAt.Add(time.Second).Format(time.RFC3339Nano)),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.RecordCreatorBackendPublished(job.ID, db.CreatorBackendEvidence{
				ObservedStatus: "Published",
				PostURL:        "https://www.xiaohongshu.com/explore/creator-state-" + initialStatus,
				ExternalPostID: "creator-state-" + initialStatus,
				ScreenshotPath: screenshotPath,
			}); err != nil {
				t.Fatal(err)
			}
			updated, err := database.GetPublishJob(job.ID)
			if err != nil || updated.Status != "published_verified" || updated.SubmitAttemptedAt == "" || updated.SubmitOutcome != "verified_published" {
				t.Fatalf("job=%+v err=%v", updated, err)
			}
			var targetStatus, planStatus string
			if err := database.QueryRow(`SELECT status FROM publish_plan_targets WHERE id=?`, targetID).Scan(&targetStatus); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow(`SELECT status FROM publish_plans WHERE id=?`, planID).Scan(&planStatus); err != nil {
				t.Fatal(err)
			}
			if targetStatus != "published_verified" || planStatus != "published_verified" {
				t.Fatalf("target=%q plan=%q", targetStatus, planStatus)
			}
		})
	}
}

func TestCreatorBackendScheduledEvidenceAtomicallyResolvesPostClickStates(t *testing.T) {
	for _, initialStatus := range []string{"submitted_pending_verification", "result_unknown"} {
		t.Run(initialStatus, func(t *testing.T) {
			database, cleanup := setupDB(t)
			defer cleanup()
			if err := database.Migrate(); err != nil {
				t.Fatal(err)
			}
			job, planID, targetID, _ := createFrozenLinkedJob(t, database, "scheduled-state-"+initialStatus, "submit")
			if claimed, err := database.ClaimPublishJob(job.ID, "scheduled-state-"+initialStatus); err != nil || !claimed {
				t.Fatalf("claim=%v err=%v", claimed, err)
			}
			if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
				t.Fatal(err)
			}
			if err := database.UpdatePublishJob(job.ID, initialStatus, "creator_backend_verification", "", "", 0); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE publish_plan_targets SET status=? WHERE id=?`, initialStatus, targetID); err != nil {
				t.Fatal(err)
			}
			screenshotDir := filepath.Join(database.DataDir, "screenshots")
			if err := os.MkdirAll(screenshotDir, 0o700); err != nil {
				t.Fatal(err)
			}
			screenshotPath := filepath.Join(screenshotDir, "scheduled-"+initialStatus+".png")
			if err := os.WriteFile(screenshotPath, []byte("creator backend scheduled evidence"), 0o600); err != nil {
				t.Fatal(err)
			}
			record, err := database.RecordCreatorBackendScheduled(
				job.ID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339), screenshotPath,
			)
			if err != nil {
				t.Fatal(err)
			}
			if record.RemoteState != "scheduled" || record.EvidencePath == "" {
				t.Fatalf("record=%+v", record)
			}
			updated, err := database.GetPublishJob(job.ID)
			if err != nil || updated.Status != "scheduled_verified" || updated.SubmitAttemptedAt == "" || updated.SubmitOutcome != "scheduled_verified" {
				t.Fatalf("job=%+v err=%v", updated, err)
			}
			var targetStatus, planStatus string
			if err := database.QueryRow(`SELECT status FROM publish_plan_targets WHERE id=?`, targetID).Scan(&targetStatus); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow(`SELECT status FROM publish_plans WHERE id=?`, planID).Scan(&planStatus); err != nil {
				t.Fatal(err)
			}
			if targetStatus != "scheduled_verified" || planStatus != "scheduled_verified" {
				t.Fatalf("target=%q plan=%q", targetStatus, planStatus)
			}
			var published int
			if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&published); err != nil || published != 0 {
				t.Fatalf("published=%d err=%v", published, err)
			}
		})
	}
}

func TestCreatorBackendScheduledEvidenceRequiresPostClickVerificationState(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "scheduled-state-gate", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "scheduled-state-gate"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	screenshotDir := filepath.Join(database.DataDir, "screenshots")
	if err := os.MkdirAll(screenshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	screenshotPath := filepath.Join(screenshotDir, "scheduled-gate.png")
	if err := os.WriteFile(screenshotPath, []byte("creator backend scheduled evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordCreatorBackendScheduled(job.ID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339), screenshotPath); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("scheduled evidence bypassed verification state: %v", err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil || updated.Status != "running" || updated.SubmitOutcome != "attempted" {
		t.Fatalf("job=%+v err=%v", updated, err)
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_content_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("scheduled records=%d err=%v", records, err)
	}
}
