package db_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func createControllablePublishJob(t *testing.T, database *db.DB, suffix string) *db.PublishJob {
	return createControllablePublishJobWithMode(t, database, suffix, "browser")
}

func createControllablePublishJobWithMode(t *testing.T, database *db.DB, suffix, mode string) *db.PublishJob {
	t.Helper()
	account, err := database.CreateAccount("xhs", "job-control-"+suffix, "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "job control", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	job, err := database.CreatePublishJobWithIntent(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, "control:"+suffix+":0001", mode, "")
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestRequestPublishJobCancelPersistsImmediateAndRunningCancellation(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	pending := createControllablePublishJob(t, database, "pending")
	action, cancelled, err := database.RequestPublishJobCancel(pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if action != "cancelled" || cancelled.Status != "cancelled" || cancelled.CancelRequestedAt == "" {
		t.Fatalf("action=%q job=%+v", action, cancelled)
	}
	if requested, err := database.IsPublishJobCancelRequested(pending.ID); err != nil || !requested {
		t.Fatalf("requested=%v err=%v", requested, err)
	}
	if _, _, err := database.RequestPublishJobCancel(pending.ID); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("repeat cancel error=%v; want conflict", err)
	}

	running := createControllablePublishJob(t, database, "running")
	if claimed, err := database.ClaimPublishJob(running.ID, "trace-running"); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	action, cancelling, err := database.RequestPublishJobCancel(running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if action != "cancel_requested" || cancelling.Status != "running" || cancelling.CancelRequestedAt == "" {
		t.Fatalf("action=%q job=%+v", action, cancelling)
	}

	if _, _, err := database.RequestPublishJobCancel(running.ID + 9999); !errors.Is(err, db.ErrPublishJobNotFound) {
		t.Fatalf("missing cancel error=%v; want not found", err)
	}
}

func TestFinalizePublishJobAtomicallyPrefersPersistedCancellation(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job := createControllablePublishJob(t, database, "finalize-cancel")
	if claimed, err := database.ClaimPublishJob(job.ID, "trace-finalize"); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if action, _, err := database.RequestPublishJobCancel(job.ID); err != nil || action != "cancel_requested" {
		t.Fatalf("action=%q err=%v", action, err)
	}
	cancelled, err := database.FinalizePublishJob(job.ID, "success", "submitted", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("finalizer did not report persisted cancellation")
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil || updated.Status != "cancelled" || updated.CurrentStep != "cancelled" {
		t.Fatalf("job=%+v err=%v", updated, err)
	}

	completed := createControllablePublishJob(t, database, "finalize-first")
	if claimed, err := database.ClaimPublishJob(completed.ID, "trace-finalize-first"); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if cancelled, err := database.FinalizePublishJob(completed.ID, "success", "submitted", "", "", 0); err != nil || cancelled {
		t.Fatalf("cancelled=%v err=%v", cancelled, err)
	}
	if _, _, err := database.RequestPublishJobCancel(completed.ID); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("post-finalize cancel error=%v; want conflict", err)
	}
	if requested, err := database.IsPublishJobCancelRequested(completed.ID); err != nil || requested {
		t.Fatalf("post-finalize requested=%v err=%v", requested, err)
	}
}

func TestRetryPublishJobAtomicallyQueuesOnlyRetryableFailure(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	job, _, _, _ := createFrozenLinkedJob(t, database, "retryable", "submit")
	if err := database.UpdatePublishJob(job.ID, "failed", "browser_attached", "CDP_CONNECT", "safe failure", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "error", Step: "browser_attached", Type: "job.retryable_failed",
		Message: "safe failure", ErrorCode: "CDP_CONNECT", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if retryable, err := database.CanRetryPublishJob(job.ID); err != nil || !retryable {
		t.Fatalf("retryable=%v err=%v", retryable, err)
	}

	queued, task, err := database.RetryPublishJob(job.ID, "retry:trace:0001")
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != "pending" || queued.RetryCount != 1 || queued.ErrorCode != "" || queued.CancelRequestedAt != "" {
		t.Fatalf("queued job=%+v", queued)
	}
	if task == nil || task.Kind != "publish" || task.Status != "pending" {
		t.Fatalf("queue task=%+v", task)
	}
	var payload struct {
		ExistingJobID int64 `json:"existing_job_id"`
		DryRun        bool  `json:"dry_run"`
		AutoSubmit    bool  `json:"auto_submit"`
	}
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil || payload.ExistingJobID != job.ID || payload.DryRun || !payload.AutoSubmit {
		t.Fatalf("payload=%q decoded=%+v err=%v", task.PayloadJSON, payload, err)
	}
	events, err := database.ListJobEventsByJob(job.ID)
	if err != nil || len(events) != 2 || events[1].EventType != "job.retry_queued" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if _, _, err := database.RetryPublishJob(job.ID, "retry:trace:0002"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("duplicate retry error=%v; want conflict", err)
	}

	permanent := createControllablePublishJob(t, database, "permanent")
	if err := database.UpdatePublishJob(permanent.ID, "failed", "preflight", "VALIDATION_FAILED", "safe failure", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{JobID: permanent.ID, Level: "error", Type: "job.permanent_failed", Retryable: false}); err != nil {
		t.Fatal(err)
	}
	if retryable, err := database.CanRetryPublishJob(permanent.ID); err != nil || retryable {
		t.Fatalf("permanent retryable=%v err=%v", retryable, err)
	}
	if _, _, err := database.RetryPublishJob(permanent.ID, "retry:trace:0003"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("permanent retry error=%v; want conflict", err)
	}

	unfrozen := createControllablePublishJobWithMode(t, database, "unfrozen-live", "submit")
	if err := database.UpdatePublishJob(unfrozen.ID, "failed", "browser_attached", "CDP_CONNECT", "safe failure", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{
		JobID: unfrozen.ID, Level: "error", Type: "job.retryable_failed",
		ErrorCode: "CDP_CONNECT", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if retryable, err := database.CanRetryPublishJob(unfrozen.ID); err != nil || retryable {
		t.Fatalf("unfrozen live job retryable=%v err=%v", retryable, err)
	}
	if _, _, err := database.RetryPublishJob(unfrozen.ID, "retry:unfrozen"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("unfrozen live retry err=%v", err)
	}
}

func TestPublishJobSettingsSnapshotIsImmutableAndIdempotent(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job := createControllablePublishJob(t, database, "settings-snapshot")
	const snapshot = `{"snapshot_version":1}`
	if err := database.SetPublishJobSettings(job.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := database.SetPublishJobSettings(job.ID, snapshot); err != nil {
		t.Fatalf("same snapshot must be idempotent: %v", err)
	}
	if err := database.SetPublishJobSettings(job.ID, `{"snapshot_version":2}`); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("snapshot mutation err=%v", err)
	}
	if claimed, err := database.ClaimPublishJob(job.ID, "settings-running"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.SetPublishJobSettings(job.ID, snapshot); err != nil {
		t.Fatalf("same terminal input should remain idempotent: %v", err)
	}
}

func TestPublishJobExecutionModeIsWriteOnceAndIdempotent(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job := createControllablePublishJobWithMode(t, database, "execution-mode", "prepare")
	if err := database.ValidatePublishJobMode(job.ID, "prepare"); err != nil {
		t.Fatalf("same mode must be idempotent: %v", err)
	}
	if err := database.ValidatePublishJobMode(job.ID, "submit"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("pending mode upgrade err=%v", err)
	}
	if claimed, err := database.ClaimPublishJob(job.ID, "mode-running"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.ValidatePublishJobMode(job.ID, "submit"); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("running mode upgrade err=%v", err)
	}
	if err := database.ValidatePublishJobMode(job.ID, "prepare"); err != nil {
		t.Fatalf("same running mode should remain idempotent: %v", err)
	}
}

func TestCreatePublishJobIntentAtomicallyBindsIdempotentReplay(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "intent-race", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "intent race", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}

	const key = "intent-race:0001"
	const settings = `{"snapshot_version":1}`
	type result struct {
		mode string
		job  *db.PublishJob
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, mode := range []string{"prepare", "submit"} {
		go func(requestedMode string) {
			<-start
			job, createErr := database.CreatePublishJobWithIntent(
				0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, key, requestedMode, settings,
			)
			results <- result{mode: requestedMode, job: job, err: createErr}
		}(mode)
	}
	close(start)
	var winner result
	winners, conflicts := 0, 0
	for index := 0; index < 2; index++ {
		outcome := <-results
		switch {
		case outcome.err == nil:
			winners++
			winner = outcome
		case errors.Is(outcome.err, db.ErrPublishJobConflict):
			conflicts++
		default:
			t.Fatalf("unexpected create error: %v", outcome.err)
		}
	}
	if winners != 1 || conflicts != 1 || winner.job == nil || winner.job.Mode != winner.mode {
		t.Fatalf("winners=%d conflicts=%d winner=%+v", winners, conflicts, winner)
	}
	replay, err := database.CreatePublishJobWithIntent(
		0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, key, winner.mode, settings,
	)
	if err != nil || replay.ID != winner.job.ID {
		t.Fatalf("same replay job=%+v err=%v", replay, err)
	}
	if _, err := database.CreatePublishJobWithIntent(
		0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, key, winner.mode, `{"snapshot_version":2}`,
	); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("settings replay conflict err=%v", err)
	}
}

func TestPreparePublishJobResumeAllowsOnlyOneConcurrentWinner(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "resume-race", "prepare")
	if err := database.UpdatePublishJob(job.ID, "manual_takeover", "open_creator", "AWAITING_HUMAN", "manual review required", 0); err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := database.PreparePublishJobResume(job.ID, "resume-race")
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, db.ErrPublishJobConflict):
			conflicts++
		default:
			t.Fatalf("unexpected resume error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil || updated.Status != "pending" || updated.RetryCount != 1 {
		t.Fatalf("job=%+v err=%v", updated, err)
	}
	events, err := database.ListJobEventsByJob(job.ID)
	if err != nil || len(events) != 1 || events[0].EventType != "job.resume_requested" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestBatchItemResumeAndClaimUseCompareAndSwap(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	run, err := database.CreateBatchRun("publish", "xhs", "running", 1, 1, `{"dry_run":true}`)
	if err != nil {
		t.Fatal(err)
	}
	item, err := database.CreateBatchItem(run.ID, "publish", 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	claimResults := make(chan struct {
		claimed bool
		err     error
	}, 2)
	for index := 0; index < 2; index++ {
		go func() {
			claimed, claimErr := database.ClaimPendingBatchItem(item.ID, "publish")
			claimResults <- struct {
				claimed bool
				err     error
			}{claimed: claimed, err: claimErr}
		}()
	}
	winners := 0
	for index := 0; index < 2; index++ {
		result := <-claimResults
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.claimed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners=%d", winners)
	}
	if _, err := database.ResetBatchItemForResume(item.ID, "running"); err == nil {
		t.Fatal("running item must not be reset automatically")
	}
	if err := database.UpdateBatchItemStatus(item.ID, "failed", "publish", "TEST", "retry"); err != nil {
		t.Fatal(err)
	}

	resetResults := make(chan struct {
		reset bool
		err   error
	}, 2)
	for index := 0; index < 2; index++ {
		go func() {
			reset, resetErr := database.ResetBatchItemForResume(item.ID, "failed")
			resetResults <- struct {
				reset bool
				err   error
			}{reset: reset, err: resetErr}
		}()
	}
	resetWinners := 0
	for index := 0; index < 2; index++ {
		result := <-resetResults
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.reset {
			resetWinners++
		}
	}
	if resetWinners != 1 {
		t.Fatalf("reset winners=%d", resetWinners)
	}
	items, err := database.ListBatchItems(run.ID)
	if err != nil || len(items) != 1 || items[0].Status != "pending" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
