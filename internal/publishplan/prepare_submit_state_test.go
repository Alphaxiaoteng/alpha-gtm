//go:build legacy_chromium

package publishplan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
)

type prepareThenSubmitPublisher struct {
	requests []platforms.PublishRequest
	platform string
}

func (p *prepareThenSubmitPublisher) Platform() string {
	if p.platform != "" {
		return p.platform
	}
	return "instagram"
}

func (p *prepareThenSubmitPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}

type retryPreparePublisher struct {
	calls int
}

func (p *retryPreparePublisher) Platform() string { return "instagram" }

func (p *retryPreparePublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}

func (p *retryPreparePublisher) Validate(platforms.PublishRequest) error { return nil }

func (p *retryPreparePublisher) Publish(_ context.Context, request platforms.PublishRequest) (*platforms.PublishResult, error) {
	p.calls++
	if request.AutoSubmit {
		return nil, nil
	}
	if p.calls == 1 {
		return &platforms.PublishResult{
			Status: "awaiting_review", Step: "select_target", ErrorCode: "TARGET_SELECTION_UNCONFIRMED",
		}, nil
	}
	return &platforms.PublishResult{Status: "awaiting_review", Step: "review_or_submit"}, nil
}

func (p *prepareThenSubmitPublisher) Validate(platforms.PublishRequest) error { return nil }

func (p *prepareThenSubmitPublisher) Publish(_ context.Context, request platforms.PublishRequest) (*platforms.PublishResult, error) {
	p.requests = append(p.requests, request)
	if request.AutoSubmit {
		return &platforms.PublishResult{Status: string(publish.StatusPublished), Step: "success_verified", Evidence: map[string]interface{}{
			"submitAttempted": true,
			"submitClicked":   true,
			"submitClickedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"submitTargetId":  "A4D362607FC76673DA07AE900173A0F3",
		}}, nil
	}
	return &platforms.PublishResult{
		Status: string(publish.StatusAwaitingReview), Step: "review_or_submit", ErrorCode: "AWAITING_HUMAN",
	}, nil
}

func prepareReadySubmitFixture(t *testing.T) (*db.DB, *Plan, *submitPlanBrowsers, *Executor, *prepareThenSubmitPublisher, int64) {
	t.Helper()
	database, plan, browsers := newSubmitPlanFixture(t)
	publisher := &prepareThenSubmitPublisher{}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}
	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution == nil || execution.Status != "awaiting_review" || len(execution.Targets) != 1 {
		database.Close()
		t.Fatalf("prepare execution=%+v err=%v", execution, err)
	}
	return database, plan, browsers, executor, publisher, execution.Targets[0].JobID
}

func TestPrepareThenSubmitConsumesNewApprovalAndReplacesReadyPrepareJob(t *testing.T) {
	database, plan, _, executor, publisher, prepareJobID := prepareReadySubmitFixture(t)
	defer database.Close()

	service := &Service{DB: database}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit)
	if err != nil || approved.Status != StatusApproved {
		t.Fatalf("approved=%+v err=%v", approved, err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err == nil {
		t.Fatal("submit approval replay was accepted")
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution == nil || execution.Status != "awaiting_verification" || len(execution.Targets) != 1 {
		t.Fatalf("submit execution=%+v err=%v", execution, err)
	}
	if len(publisher.requests) != 2 || publisher.requests[0].AutoSubmit || !publisher.requests[1].AutoSubmit {
		t.Fatalf("publisher requests=%+v", publisher.requests)
	}
	if !publisher.requests[1].SubmitPreparedOnly {
		t.Fatalf("live prepared composer must be submitted without re-uploading: %+v", publisher.requests[1])
	}
	submitJobID := execution.Targets[0].JobID
	if submitJobID <= 0 || submitJobID == prepareJobID {
		t.Fatalf("prepare job=%d submit job=%d", prepareJobID, submitJobID)
	}
	var targetJobID int64
	if err := database.QueryRow(`SELECT publish_job_id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetJobID); err != nil || targetJobID != submitJobID {
		t.Fatalf("target job=%d want=%d err=%v", targetJobID, submitJobID, err)
	}
	prepareJob, err := database.GetPublishJob(prepareJobID)
	if err != nil || prepareJob.Mode != publish.ModePrepare || prepareJob.Status != "awaiting_review" ||
		prepareJob.CurrentStep != "review_or_submit" || prepareJob.SubmitAttemptedAt != "" {
		t.Fatalf("prepare job=%+v err=%v", prepareJob, err)
	}
	submitJob, err := database.GetPublishJob(submitJobID)
	if err != nil || submitJob.Mode != publish.ModeSubmit || submitJob.SubmitAttemptedAt == "" {
		t.Fatalf("submit job=%+v err=%v", submitJob, err)
	}
}

func TestPrepareCanReplayFrozenSnapshotAfterUnsubmittedReviewFailure(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	publisher := &retryPreparePublisher{}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	first, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || len(first.Targets) != 1 || first.Targets[0].ErrorCode != "TARGET_SELECTION_UNCONFIRMED" {
		t.Fatalf("first execution=%+v err=%v", first, err)
	}
	firstJobID := first.Targets[0].JobID
	service := &Service{DB: database}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionPrepare, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionPrepare); err != nil {
		t.Fatal(err)
	}

	second, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || len(second.Targets) != 1 || second.Targets[0].Step != "review_or_submit" {
		t.Fatalf("second execution=%+v err=%v", second, err)
	}
	if second.Targets[0].JobID <= 0 || second.Targets[0].JobID == firstJobID {
		t.Fatalf("first job=%d second job=%d", firstJobID, second.Targets[0].JobID)
	}
	var jobs, submitted int
	if err := database.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN COALESCE(submit_attempted_at,'')<>'' THEN 1 ELSE 0 END) FROM publish_jobs WHERE plan_id=?`, plan.ID).
		Scan(&jobs, &submitted); err != nil || jobs != 2 || submitted != 0 {
		t.Fatalf("jobs=%d submitted=%d err=%v", jobs, submitted, err)
	}
}

func TestPrepareCanReplayAfterPreflightSubmitFailureWithoutClick(t *testing.T) {
	database, plan, _, executor, _, prepareJobID := prepareReadySubmitFixture(t)
	defer database.Close()
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database}
	submitGrant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, submitGrant.ID, submitGrant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	target := plan.Snapshot.Targets[0]
	submitJob, created, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, plan.ContentID, target.AccountID, target.AssetID,
		target.PlatformCode, plan.MediaMode, fmt.Sprintf("plan:%d:account:%d:asset:%d:%s:%s",
			plan.ID, target.AccountID, target.AssetID, publish.ModeSubmit, plan.SnapshotHash), publish.ModeSubmit, target.SettingsJSON,
	)
	if err != nil || !created || submitJob == nil {
		t.Fatalf("submit job=%+v created=%v err=%v", submitJob, created, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET status='failed', current_step='preflight', error_code='AUTO_SUBMIT_NOT_SUPPORTED' WHERE id=?`, submitJob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET publish_job_id=NULL, status='failed', error_code='PUBLISH_JOB_CREATE_FAILED' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='partial_failed' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}

	submitRetryGrant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, submitRetryGrant.ID, submitRetryGrant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	submitted, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || submitted == nil || len(submitted.Targets) != 1 || submitted.Targets[0].JobID == submitJob.ID || submitted.Targets[0].JobID == prepareJobID {
		t.Fatalf("submitted=%+v err=%v", submitted, err)
	}
}

func TestSubmitCanRetryPreparedSnapshotAfterManualTakeoverWithoutTrustedClick(t *testing.T) {
	database, plan, _, executor, _, prepareJobID := prepareReadySubmitFixture(t)
	defer database.Close()
	service := &Service{DB: database}
	firstGrant, err := service.ApproveForAction(
		plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(
		plan.ID, plan.SnapshotHash, firstGrant.ID, firstGrant.Nonce, PublishPlanActionSubmit,
	); err != nil {
		t.Fatal(err)
	}
	target := plan.Snapshot.Targets[0]
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	firstSubmit, created, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, plan.ContentID, target.AccountID, target.AssetID,
		target.PlatformCode, plan.MediaMode, "manual-submit-without-click", publish.ModeSubmit, target.SettingsJSON,
	)
	if err != nil || !created {
		t.Fatalf("submit=%+v created=%v err=%v", firstSubmit, created, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs
		SET status='manual_takeover', current_step='submit_button_missing', error_code='SUBMIT_BUTTON_NOT_FOUND'
		WHERE id=?`, firstSubmit.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets
		SET status='manual_takeover', error_code='SUBMIT_BUTTON_NOT_FOUND', publish_job_id=?
		WHERE id=?`, firstSubmit.ID, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='manual_takeover' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}

	retryGrant, err := service.ApproveForAction(
		plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute,
	)
	if err != nil {
		t.Fatalf("unsubmitted manual takeover should remain retryable: %v", err)
	}
	if _, err := service.ConsumeApprovalForAction(
		plan.ID, plan.SnapshotHash, retryGrant.ID, retryGrant.Nonce, PublishPlanActionSubmit,
	); err != nil {
		t.Fatal(err)
	}
	retried, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || retried == nil || len(retried.Targets) != 1 ||
		retried.Targets[0].JobID <= 0 || retried.Targets[0].Status == "failed" ||
		retried.Targets[0].JobID == firstSubmit.ID || retried.Targets[0].JobID == prepareJobID {
		t.Fatalf("retried=%+v err=%v", retried, err)
	}
}

func TestPrepareApprovalCannotAuthorizeSubmit(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	publisher := &prepareThenSubmitPublisher{}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), "PUBLISH_PLAN_APPROVAL_REQUIRED") || execution != nil {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 0 || len(publisher.requests) != 0 {
		t.Fatalf("prepare approval reached submit: starts=%d requests=%d", browsers.startCalls, len(publisher.requests))
	}
}

func TestConsumedApprovalPersistsRequiredAction(t *testing.T) {
	database, plan, _ := newSubmitPlanFixture(t)
	defer database.Close()

	var approvedAction string
	if err := database.QueryRow(`SELECT approved_action FROM publish_plans WHERE id=?`, plan.ID).Scan(&approvedAction); err != nil {
		t.Fatal(err)
	}
	if approvedAction != PublishPlanActionPrepare {
		t.Fatalf("approved action=%q", approvedAction)
	}
}

func TestSubmitJobCreationRejectsSkippedPrepare(t *testing.T) {
	database, plan, _ := newSubmitPlanFixture(t)
	defer database.Close()
	target := plan.Snapshot.Targets[0]
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, plan.ContentID, target.AccountID, target.AssetID,
		target.PlatformCode, plan.MediaMode, "skipped-prepare-submit", publish.ModeSubmit, target.SettingsJSON,
	); err == nil {
		t.Fatal("submit job was created without a ready prepare job")
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=?`, plan.ID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs=%d err=%v", jobs, err)
	}
}

func TestSubmitApprovalRejectsPrepareJobThatIsNotReady(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *db.DB, int64)
	}{
		{
			name: "non ready step",
			mutate: func(t *testing.T, database *db.DB, jobID int64) {
				t.Helper()
				if _, err := database.Exec(`UPDATE publish_jobs SET current_step='metadata_filled' WHERE id=?`, jobID); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "manual takeover",
			mutate: func(t *testing.T, database *db.DB, jobID int64) {
				t.Helper()
				if _, err := database.Exec(`UPDATE publish_jobs SET status='manual_takeover' WHERE id=?`, jobID); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "failed",
			mutate: func(t *testing.T, database *db.DB, jobID int64) {
				t.Helper()
				if _, err := database.Exec(`UPDATE publish_jobs SET status='failed' WHERE id=?`, jobID); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "submit already attempted",
			mutate: func(t *testing.T, database *db.DB, jobID int64) {
				t.Helper()
				if _, err := database.Exec(`UPDATE publish_jobs SET submit_attempted_at='2026-07-26T00:00:00Z' WHERE id=?`, jobID); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, plan, _, _, _, prepareJobID := prepareReadySubmitFixture(t)
			defer database.Close()
			test.mutate(t, database, prepareJobID)

			_, err := (&Service{DB: database}).ApproveForAction(
				plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute,
			)
			if err == nil || !strings.Contains(err.Error(), "PUBLISH_PLAN_NOT_READY_TO_SUBMIT") {
				t.Fatalf("approval err=%v", err)
			}
		})
	}
}

func TestSubmitApprovalAllowsReadyTargetsWhenAnotherTargetPrepareFailed(t *testing.T) {
	database, original, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	accountID := original.Snapshot.Targets[0].AccountID
	second, err := database.CreateAccountAsset(accountID, "facebook", "facebook_page", "Alpha Page", "page-public-1", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: original.ContentID,
		MediaMode: original.MediaMode,
		MediaIDs:  []int64{original.Snapshot.Media[0].ID},
		Targets: []TargetInput{
			{AccountID: accountID, AssetID: original.Snapshot.Targets[0].AssetID},
			{AccountID: accountID, AssetID: second.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	publisher := &prepareThenSubmitPublisher{}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}
	prepared, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || len(prepared.Targets) != 2 {
		t.Fatalf("prepare=%+v err=%v", prepared, err)
	}
	var failedTargetID, failedJobID int64
	if err := database.QueryRow(`SELECT id, publish_job_id FROM publish_plan_targets WHERE plan_id=? AND asset_id=?`,
		plan.ID, second.ID).Scan(&failedTargetID, &failedJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs
		SET status='failed', current_step='media_upload', error_code='UPLOAD_FAILED'
		WHERE id=?`, failedJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets
		SET status='failed', error_code='UPLOAD_FAILED'
		WHERE id=?`, failedTargetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='partial_failed' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}

	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatalf("ready target should allow submit approval: %v", err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatalf("ready target should allow submit approval consumption: %v", err)
	}
}

func TestSubmitApprovalConsumeRechecksReadyPrepareJobAtomically(t *testing.T) {
	database, plan, _, _, _, prepareJobID := prepareReadySubmitFixture(t)
	defer database.Close()
	service := &Service{DB: database}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET current_step='manual_takeover' WHERE id=?`, prepareJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err == nil ||
		!strings.Contains(err.Error(), "PUBLISH_PLAN_NOT_READY_TO_SUBMIT") {
		t.Fatalf("consume err=%v", err)
	}
	var status string
	var consumed int
	if err := database.QueryRow(`SELECT status FROM publish_plans WHERE id=?`, plan.ID).Scan(&status); err != nil || status != "awaiting_review" {
		t.Fatalf("plan status=%q err=%v", status, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM approvals WHERE id=? AND consumed_at IS NOT NULL`, grant.ID).Scan(&consumed); err != nil || consumed != 0 {
		t.Fatalf("consumed=%d err=%v", consumed, err)
	}
}

func TestSubmitJobCreationRejectsPrepareJobThatChangedAfterApproval(t *testing.T) {
	database, plan, _, _, _, prepareJobID := prepareReadySubmitFixture(t)
	defer database.Close()
	service := &Service{DB: database}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET status='failed' WHERE id=?`, prepareJobID); err != nil {
		t.Fatal(err)
	}
	target := plan.Snapshot.Targets[0]
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, plan.ContentID, target.AccountID, target.AssetID,
		target.PlatformCode, plan.MediaMode, "changed-prepare-submit", publish.ModeSubmit, target.SettingsJSON,
	); err == nil {
		t.Fatal("submit job replaced a failed prepare job")
	}
	var targetJobID int64
	var jobCount int
	if err := database.QueryRow(`SELECT publish_job_id FROM publish_plan_targets WHERE id=?`, targetID).Scan(&targetJobID); err != nil || targetJobID != prepareJobID {
		t.Fatalf("target job=%d prepare=%d err=%v", targetJobID, prepareJobID, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=?`, plan.ID).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("jobs=%d err=%v", jobCount, err)
	}
}

func TestSubmitJobCreationReplacesReadyPrepareJob(t *testing.T) {
	database, plan, _, _, _, prepareJobID := prepareReadySubmitFixture(t)
	defer database.Close()
	service := &Service{DB: database}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	target := plan.Snapshot.Targets[0]
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	job, created, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, plan.ContentID, target.AccountID, target.AssetID,
		target.PlatformCode, plan.MediaMode, "ready-prepare-submit", publish.ModeSubmit, target.SettingsJSON,
	)
	if err != nil || !created || job == nil || job.ID == prepareJobID {
		t.Fatalf("job=%+v created=%v prepare=%d err=%v", job, created, prepareJobID, err)
	}
	var targetJobID int64
	if err := database.QueryRow(`SELECT publish_job_id FROM publish_plan_targets WHERE id=?`, targetID).Scan(&targetJobID); err != nil || targetJobID != job.ID {
		t.Fatalf("target job=%d want=%d err=%v", targetJobID, job.ID, err)
	}
}
