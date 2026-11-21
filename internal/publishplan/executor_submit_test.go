//go:build legacy_chromium

package publishplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/logincheck"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
)

type submitPlanBrowsers struct {
	database   *db.DB
	running    bool
	startCalls int
	stopCalls  int
	startOpts  []browser.StartOptions
	stopErr    error
}

func (b *submitPlanBrowsers) Status(accountID int64) (*browser.AccountStatus, error) {
	status := &browser.AccountStatus{
		AccountID: accountID,
		ProfileID: browser.ProfileIDForAccount(accountID),
	}
	if b.running {
		status.Running = true
		status.State = browser.StateReady
		status.CDPPort = 15106
		status.CDPURL = "http://127.0.0.1:15106/"
	}
	return status, nil
}

func (b *submitPlanBrowsers) StartAccount(accountID int64, opts browser.StartOptions) (*browser.Instance, error) {
	b.startCalls++
	b.startOpts = append(b.startOpts, opts)
	b.running = true
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := b.database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 12345, 15106, 'http://127.0.0.1:15106/', 'ready', ?, ?, ?)`,
		browser.ProfileIDForAccount(accountID), accountID, now, now, now)
	if err != nil {
		return nil, err
	}
	return &browser.Instance{
		AccountID: accountID,
		ProfileID: browser.ProfileIDForAccount(accountID),
		State:     browser.StateReady,
		CDPPort:   15106,
		CDPURL:    "http://127.0.0.1:15106/",
	}, nil
}

func (b *submitPlanBrowsers) StopAccount(accountID int64) error {
	b.stopCalls++
	if b.stopErr != nil {
		return b.stopErr
	}
	b.running = false
	_, err := b.database.Exec(`DELETE FROM browser_runtime_instances WHERE account_id=?`, accountID)
	return err
}

type submitPlanPublisher struct {
	platform string
	status   string
	statuses []string
	requests []platforms.PublishRequest
}

type verificationThenSuccessPublisher struct {
	requests []platforms.PublishRequest
}

func (p *verificationThenSuccessPublisher) Platform() string {
	return "instagram"
}

func (p *verificationThenSuccessPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}

func (p *verificationThenSuccessPublisher) Validate(platforms.PublishRequest) error {
	return nil
}

func (p *verificationThenSuccessPublisher) Publish(_ context.Context, request platforms.PublishRequest) (*platforms.PublishResult, error) {
	p.requests = append(p.requests, request)
	if len(p.requests) == 1 {
		return &platforms.PublishResult{
			Status:    string(publish.StatusManualTakeover),
			Step:      "platform_verification_required",
			ErrorCode: "PLATFORM_VERIFICATION_REQUIRED",
		}, nil
	}
	return &platforms.PublishResult{
		Status: string(publish.StatusPublished),
		Step:   "success_verified",
		Evidence: map[string]interface{}{
			"submitAttempted": true,
			"submitClicked":   true,
			"submitClickedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"submitTargetId":  "A4D362607FC76673DA07AE900173A0F3",
		},
	}, nil
}

func (p *submitPlanPublisher) Platform() string {
	if p.platform != "" {
		return p.platform
	}
	return "instagram"
}
func (p *submitPlanPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *submitPlanPublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *submitPlanPublisher) Publish(_ context.Context, request platforms.PublishRequest) (*platforms.PublishResult, error) {
	status := p.status
	if len(p.requests) < len(p.statuses) {
		status = p.statuses[len(p.requests)]
	}
	p.requests = append(p.requests, request)
	step := "manual_takeover"
	code := "AWAITING_HUMAN"
	if status == string(publish.StatusPublished) {
		step = "success_verified"
		code = ""
	}
	result := &platforms.PublishResult{Status: status, Step: step, ErrorCode: code}
	if request.AutoSubmit && status == string(publish.StatusPublished) {
		result.Evidence = map[string]interface{}{
			"submitAttempted": true,
			"submitClicked":   true,
			"submitClickedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"submitTargetId":  "A4D362607FC76673DA07AE900173A0F3",
		}
	}
	return result, nil
}

func newSubmitPlanFixture(t *testing.T) (*db.DB, *Plan, *submitPlanBrowsers) {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "Meta title", "Meta caption", "Meta body", `["meta"]`, "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "meta.png")
	const payload = "meta-image"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		database.Close()
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	image, err := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("instagram", "Meta operator", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	asset, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "AlphaIntelligence", "ig-public-1", "", 0, "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID,
		MediaMode: "graph_text",
		MediaIDs:  []int64{image.ID},
		Targets:   []TargetInput{{AccountID: account.ID, AssetID: asset.ID}},
	})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	return database, plan, &submitPlanBrowsers{database: database}
}

func prepareAndApproveSubmitPlanForTest(t *testing.T, database *db.DB, plan *Plan, browsers *submitPlanBrowsers) {
	t.Helper()
	preparePublisher := &prepareThenSubmitPublisher{platform: plan.Snapshot.Targets[0].PlatformCode}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return preparePublisher, nil },
	}
	if execution, err := executor.Prepare(context.Background(), plan.ID); err != nil ||
		execution == nil || execution.Status != StatusAwaitingReview {
		t.Fatalf("prepare execution=%+v err=%v", execution, err)
	}
	service := &Service{DB: database}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	// Submit lifecycle tests start from a stopped account browser. Prepare now
	// deliberately leaves its reviewed page open for the user to take over.
	if browsers.running {
		if err := browsers.StopAccount(plan.Snapshot.Targets[0].AccountID); err != nil {
			t.Fatal(err)
		}
	}
	browsers.startCalls = 0
	browsers.stopCalls = 0
	browsers.startOpts = nil
}

func TestExecutorSkipsTargetWithVerifiedPublishedRecordBeforeStartingBrowser(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	target := plan.Snapshot.Targets[0]
	job, err := database.CreatePublishJobWithIntent(
		0, plan.ContentID, target.AccountID, target.AssetID, target.PlatformCode,
		plan.MediaMode, "verified-existing-target", publish.ModeSubmit, target.SettingsJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`UPDATE publish_jobs
		SET status='published', current_step='creator_backend_verified',
		    submit_attempted_at=?, submit_outcome='verified_published'
		WHERE id=?`, now, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreatePublishRecord(
		job.ID, plan.ContentID, target.AccountID, target.AssetID,
		target.PlatformCode, plan.MediaMode, "published",
		"https://example.com/published", "published-1",
	); err != nil {
		t.Fatal(err)
	}
	publisher := &submitPlanPublisher{platform: target.PlatformCode, status: string(publish.StatusAwaitingReview)}
	execution, err := (&Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}).Prepare(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "published" || len(execution.Targets) != 1 ||
		execution.Targets[0].Status != "published" || execution.Targets[0].Step != "already_published" {
		t.Fatalf("execution=%+v", execution)
	}
	if browsers.startCalls != 0 || len(publisher.requests) != 0 {
		t.Fatalf("completed target started browser=%d requests=%d", browsers.startCalls, len(publisher.requests))
	}
}

func TestExecutorDoesNotReplayEquivalentUnknownSubmission(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	target := plan.Snapshot.Targets[0]
	job, err := database.CreatePublishJobWithIntent(
		0, plan.ContentID, target.AccountID, target.AssetID, target.PlatformCode,
		plan.MediaMode, "unknown-existing-target", publish.ModeSubmit, target.SettingsJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs
		SET status='awaiting_review', current_step='submission_unknown',
		    submit_attempted_at=?, submit_outcome='unknown'
		WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), job.ID); err != nil {
		t.Fatal(err)
	}
	publisher := &submitPlanPublisher{platform: target.PlatformCode, status: string(publish.StatusAwaitingReview)}
	execution, err := (&Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}).Prepare(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "awaiting_verification" || len(execution.Targets) != 1 ||
		execution.Targets[0].Status != "awaiting_review" ||
		execution.Targets[0].Step != "submission_unknown" ||
		execution.Targets[0].ErrorCode != "PUBLISH_CLICK_OUTCOME_UNKNOWN" {
		t.Fatalf("execution=%+v", execution)
	}
	if browsers.startCalls != 0 || len(publisher.requests) != 0 {
		t.Fatalf("unknown target started browser=%d requests=%d", browsers.startCalls, len(publisher.requests))
	}
}

func TestExecutorAllowsFreshPlanAfterCreatorBackendVerifiedPriorAttemptAbsent(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	target := plan.Snapshot.Targets[0]
	job, err := database.CreatePublishJobWithIntent(
		0, plan.ContentID, target.AccountID, target.AssetID, target.PlatformCode,
		plan.MediaMode, "verified-absent-target", publish.ModeSubmit, target.SettingsJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs
		SET status='failed', current_step='creator_backend_verified_absent',
		    error_code='CREATOR_BACKEND_NOT_FOUND',
		    submit_attempted_at=?, submit_outcome='verified_not_published'
		WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), job.ID); err != nil {
		t.Fatal(err)
	}
	publisher := &submitPlanPublisher{platform: target.PlatformCode, status: string(publish.StatusAwaitingReview)}
	execution, err := (&Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}).Prepare(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "awaiting_review" || len(execution.Targets) != 1 ||
		execution.Targets[0].JobID == 0 ||
		execution.Targets[0].Step == "submission_unknown" {
		t.Fatalf("execution=%+v", execution)
	}
	if browsers.startCalls != 1 || len(publisher.requests) != 1 {
		t.Fatalf("verified-absent target did not start a fresh prepare: browser=%d requests=%d", browsers.startCalls, len(publisher.requests))
	}
}

func newCSDNSubmitPlanFixture(t *testing.T) (*db.DB, *Plan, *submitPlanBrowsers) {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "CSDN title", "", "CSDN body", `["article-tag"]`, "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "CSDN creator", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID,
		MediaMode: "article",
		Targets:   []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	return database, plan, &submitPlanBrowsers{database: database}
}

func TestExecutorCSDNSubmitRepreparesWhenReviewedPageIsClosed(t *testing.T) {
	database, plan, browsers := newCSDNSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	publisher := &submitPlanPublisher{platform: "csdn", status: string(publish.StatusManualTakeover)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution.Status != "manual_takeover" || len(publisher.requests) != 1 {
		t.Fatalf("execution=%+v requests=%+v err=%v", execution, publisher.requests, err)
	}
	request := publisher.requests[0]
	if !request.AutoSubmit || request.SubmitPreparedOnly {
		t.Fatalf("closed CSDN page must use full frozen submit: %+v", request)
	}
}

func TestExecutorCSDNSubmitRetriesOnlyPriorUnclickedFailure(t *testing.T) {
	database, plan, browsers := newCSDNSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database}
	target := plan.Snapshot.Targets[0]
	first, created, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, plan.ContentID, target.AccountID, target.AssetID,
		target.PlatformCode, plan.MediaMode, "plan:csdn:submit:first", publish.ModeSubmit, target.SettingsJSON,
	)
	if err != nil || !created || first == nil {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET status='manual_takeover', current_step='required_control_missing', error_code='FORM_REVIEW_REQUIRED' WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET publish_job_id=NULL, status='failed', error_code='PUBLISH_JOB_CREATE_FAILED' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='partial_failed' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}
	secondGrant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, secondGrant.ID, secondGrant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	publisher := &submitPlanPublisher{platform: "csdn", status: string(publish.StatusManualTakeover)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}
	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || len(execution.Targets) != 1 || execution.Targets[0].JobID == first.ID {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if len(publisher.requests) != 1 || publisher.requests[0].SubmitPreparedOnly {
		t.Fatalf("requests=%+v", publisher.requests)
	}
}

func TestExecutorCSDNSubmitRecoversLegacyCreateFailureWithoutSubmitJob(t *testing.T) {
	database, plan, browsers := newCSDNSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET publish_job_id=NULL, status='failed', error_code='PUBLISH_JOB_CREATE_FAILED' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='partial_failed' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	publisher := &submitPlanPublisher{platform: "csdn", status: string(publish.StatusManualTakeover)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}
	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || len(execution.Targets) != 1 || execution.Targets[0].JobID == 0 {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if len(publisher.requests) != 1 || publisher.requests[0].SubmitPreparedOnly {
		t.Fatalf("requests=%+v", publisher.requests)
	}
}

func TestExecutorSubmitStartsStoppedBrowserAndLeavesItOpenForBackendEvidence(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	publisher := &submitPlanPublisher{status: string(publish.StatusPublished)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(platformCode string) (platforms.Publisher, error) {
			publisher.platform = platformCode
			return publisher, nil
		},
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_verification" || len(execution.Targets) != 1 || execution.Targets[0].Status != "awaiting_review" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 1 || browsers.stopCalls != 0 || !browsers.running {
		t.Fatalf("start=%d stop=%d", browsers.startCalls, browsers.stopCalls)
	}
	if len(browsers.startOpts) != 1 || browsers.startOpts[0].HolderType != browser.HolderJob || !browsers.startOpts[0].SkipFocus {
		t.Fatalf("start options=%+v", browsers.startOpts)
	}
	if len(publisher.requests) != 1 || !publisher.requests[0].AutoSubmit || publisher.requests[0].DryRun {
		t.Fatalf("requests=%+v", publisher.requests)
	}
	job, err := database.GetPublishJob(execution.Targets[0].JobID)
	if err != nil || job.Mode != publish.ModeSubmit || job.Status != "awaiting_review" || job.SubmitAttemptedAt == "" || job.SubmitOutcome != "verification_required" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestExecutorSubmitDoesNotCloseBrowserBeforeBackendEvidence(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	browsers.stopErr = context.DeadlineExceeded
	publisher := &submitPlanPublisher{status: string(publish.StatusPublished)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(platformCode string) (platforms.Publisher, error) {
			publisher.platform = platformCode
			return publisher, nil
		},
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_verification" || len(execution.Targets) != 1 {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	target := execution.Targets[0]
	if target.Status != "awaiting_review" || target.Step != "creator_backend_verification" || target.ErrorCode != "PUBLISH_VERIFICATION_REQUIRED" {
		t.Fatalf("target=%+v", target)
	}
	if browsers.stopCalls != 0 || !browsers.running {
		t.Fatalf("stop=%d running=%v", browsers.stopCalls, browsers.running)
	}
}

func TestExecutorSubmitRequiresEveryFrozenTargetPrepared(t *testing.T) {
	database, original, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	accountID := original.Snapshot.Targets[0].AccountID
	facebook, err := database.CreateAccountAsset(accountID, "facebook", "facebook_page", "Alpha Page", "page-public-1", "", 0, "")
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
			{AccountID: accountID, AssetID: facebook.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	publisher := &submitPlanPublisher{status: string(publish.StatusPublished)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(platformCode string) (platforms.Publisher, error) {
			publisher.platform = platformCode
			return publisher, nil
		},
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), "PUBLISH_PLAN_APPROVAL_REQUIRED") || execution != nil {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 0 || browsers.stopCalls != 0 || len(publisher.requests) != 0 {
		t.Fatalf("multi-target submit mutated browser state: start=%d stop=%d requests=%d",
			browsers.startCalls, browsers.stopCalls, len(publisher.requests))
	}
}

func TestExecutorSubmitRunsReadyTargetAndSkipsFailedPrepareTarget(t *testing.T) {
	database, original, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	accountID := original.Snapshot.Targets[0].AccountID
	secondAccount, err := database.CreateAccount("instagram", "Meta backup operator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, secondAccount.ID)
	second, err := database.CreateAccountAsset(secondAccount.ID, "instagram", "instagram_account", "Alpha Backup", "ig-public-2", "", 0, "")
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
			{AccountID: secondAccount.ID, AssetID: second.ID},
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
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err != nil {
		t.Fatal(err)
	}
	publisher.requests = nil
	browsers.startCalls = 0
	browsers.running = true

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution == nil || len(execution.Targets) != 2 {
		t.Fatalf("submit=%+v err=%v", execution, err)
	}
	if len(publisher.requests) != 1 || !publisher.requests[0].AutoSubmit || publisher.requests[0].AssetID != original.Snapshot.Targets[0].AssetID {
		t.Fatalf("submit=%+v requests=%+v", execution, publisher.requests)
	}
	skipped := execution.Targets[1]
	if skipped.AssetID != second.ID || skipped.JobID != 0 || skipped.Step != "not_ready_to_submit" ||
		skipped.Status != "failed" || skipped.ErrorCode != "UPLOAD_FAILED" {
		t.Fatalf("skipped target=%+v", skipped)
	}

	retryGrant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionPrepare, 5*time.Minute)
	if err != nil {
		t.Fatalf("unsubmitted failed target should remain retryable beside an unknown target: %v", err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, retryGrant.ID, retryGrant.Nonce, PublishPlanActionPrepare); err != nil {
		t.Fatal(err)
	}
	publisher.requests = nil
	browsers.running = false
	retried, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || retried == nil || len(retried.Targets) != 2 {
		t.Fatalf("retry prepare=%+v err=%v", retried, err)
	}
	if len(publisher.requests) != 1 || publisher.requests[0].AssetID != second.ID || publisher.requests[0].AutoSubmit {
		t.Fatalf("retry=%+v requests=%+v", retried, publisher.requests)
	}
	if retried.Targets[0].Step != "submission_unknown" || retried.Targets[0].ErrorCode != "PUBLISH_CLICK_OUTCOME_UNKNOWN" {
		t.Fatalf("unknown target was not safely skipped: %+v", retried.Targets[0])
	}
}

func TestExecutorPrepareRetryReusesReadyTargetAndRunsOnlyFailedTarget(t *testing.T) {
	database, original, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	accountID := original.Snapshot.Targets[0].AccountID
	second, err := database.CreateAccountAsset(accountID, "instagram", "instagram_account", "Alpha Backup", "ig-public-2", "", 0, "")
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
	first, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || len(first.Targets) != 2 {
		t.Fatalf("first prepare=%+v err=%v", first, err)
	}
	readyJobID := first.Targets[0].JobID
	failedJobID := first.Targets[1].JobID
	if _, err := database.Exec(`UPDATE publish_jobs
		SET status='failed', current_step='media_upload', error_code='UPLOAD_FAILED'
		WHERE id=?`, failedJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets
		SET status='failed', error_code='UPLOAD_FAILED'
		WHERE plan_id=? AND asset_id=?`, plan.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='partial_failed' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionPrepare, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionPrepare); err != nil {
		t.Fatal(err)
	}
	publisher.requests = nil
	browsers.startCalls = 0

	retried, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || retried == nil || len(retried.Targets) != 2 {
		t.Fatalf("retry prepare=%+v err=%v", retried, err)
	}
	if len(publisher.requests) != 1 || publisher.requests[0].AssetID != second.ID || publisher.requests[0].AutoSubmit {
		t.Fatalf("retry=%+v requests=%+v", retried, publisher.requests)
	}
	reused := retried.Targets[0]
	if reused.AssetID != original.Snapshot.Targets[0].AssetID || reused.JobID != readyJobID ||
		reused.Status != "awaiting_review" || reused.Step != "already_prepared" {
		t.Fatalf("reused target=%+v", reused)
	}
	if retried.Targets[1].JobID == failedJobID {
		t.Fatalf("failed prepare job was replayed: %+v", retried.Targets[1])
	}
}

func TestExecutorPrepareStartsStoppedBrowserAndLeavesItOpenForReview(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	publisher := &submitPlanPublisher{status: string(publish.StatusAwaitingReview)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_review" || len(execution.Targets) != 1 || execution.Targets[0].Status != string(publish.StatusAwaitingReview) {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 1 || browsers.stopCalls != 0 || !browsers.running || len(publisher.requests) != 1 {
		t.Fatalf("start=%d stop=%d requests=%d", browsers.startCalls, browsers.stopCalls, len(publisher.requests))
	}
}

func TestExecutorPrepareLeavesBrowserOpenAfterManualTakeover(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	publisher := &submitPlanPublisher{status: string(publish.StatusManualTakeover)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "manual_takeover" || len(execution.Targets) != 1 || execution.Targets[0].Status != string(publish.StatusManualTakeover) {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 1 || browsers.stopCalls != 0 || !browsers.running {
		t.Fatalf("start=%d stop=%d running=%v", browsers.startCalls, browsers.stopCalls, browsers.running)
	}
}

func TestExecutorPrepareClosesBrowserAfterPublisherFailure(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	publisher := &submitPlanPublisher{status: string(publish.StatusPermanentFailed)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "partial_failed" || len(execution.Targets) != 1 || execution.Targets[0].Status != "failed" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if !execution.Targets[0].BrowserClosed || browsers.startCalls != 1 || browsers.stopCalls != 1 || browsers.running {
		t.Fatalf("start=%d stop=%d running=%v", browsers.startCalls, browsers.stopCalls, browsers.running)
	}
}

func TestExecutorPrepareNeverClosesBrowserThatWasAlreadyRunning(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	if _, err := browsers.StartAccount(plan.Snapshot.Targets[0].AccountID, browser.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	browsers.startCalls = 0
	browsers.startOpts = nil
	publisher := &submitPlanPublisher{status: string(publish.StatusAwaitingReview)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_review" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 0 || browsers.stopCalls != 0 || !browsers.running {
		t.Fatalf("start=%d stop=%d running=%v", browsers.startCalls, browsers.stopCalls, browsers.running)
	}
}

func TestExecutorSubmitLeavesBrowserOpenForManualTakeover(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	publisher := &submitPlanPublisher{status: string(publish.StatusManualTakeover)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution.Status != "manual_takeover" || len(execution.Targets) != 1 || execution.Targets[0].Status != "manual_takeover" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 1 || browsers.stopCalls != 0 || !browsers.running {
		t.Fatalf("start=%d stop=%d running=%v", browsers.startCalls, browsers.stopCalls, browsers.running)
	}
}

func TestExecutorSubmitContinuesAfterOneTargetRequiresPlatformVerification(t *testing.T) {
	database, original, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	secondAccount, err := database.CreateAccount("instagram", "Second operator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, secondAccount.ID)
	secondAsset, err := database.CreateAccountAsset(
		secondAccount.ID, "instagram", "instagram_account", "Second target", "ig-public-2", "", 0, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 12346, 15107, 'http://127.0.0.1:15107/', 'ready', ?, ?, ?)`,
		browser.ProfileIDForAccount(secondAccount.ID), secondAccount.ID, now, now, now); err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: original.ContentID,
		MediaMode: original.MediaMode,
		MediaIDs:  []int64{original.Snapshot.Media[0].ID},
		Targets: []TargetInput{
			{AccountID: original.Snapshot.Targets[0].AccountID, AssetID: original.Snapshot.Targets[0].AssetID},
			{AccountID: secondAccount.ID, AssetID: secondAsset.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)

	publisher := &verificationThenSuccessPublisher{}
	backendVerificationCalls := 0
	execution, err := (&Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
		VerifyCreatorBackend: func(context.Context, int64) (CreatorBackendVerification, error) {
			backendVerificationCalls++
			return CreatorBackendVerification{
				Status:    string(publish.StatusAwaitingReview),
				Step:      "creator_backend_verification",
				ErrorCode: "PUBLISH_VERIFICATION_REQUIRED",
			}, nil
		},
	}).Submit(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "partial_failed" || len(execution.Targets) != 2 || len(publisher.requests) != 2 {
		t.Fatalf("execution=%+v requests=%d", execution, len(publisher.requests))
	}
	if backendVerificationCalls != 1 {
		t.Fatalf("backend verification calls=%d, want only the non-challenged target", backendVerificationCalls)
	}
	if first := execution.Targets[0]; first.Status != string(publish.StatusManualTakeover) ||
		first.Step != "platform_verification_required" || first.ErrorCode != "PLATFORM_VERIFICATION_REQUIRED" {
		t.Fatalf("verification target=%+v", first)
	}
	if second := execution.Targets[1]; second.Status != string(publish.StatusAwaitingReview) ||
		second.Step != "creator_backend_verification" {
		t.Fatalf("next target did not continue: %+v", second)
	}
}

func TestExecutorSubmitClosesBrowserWhenPublisherFailsBeforeSubmission(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	publisher := &submitPlanPublisher{status: string(publish.StatusPermanentFailed)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution.Status != "partial_failed" || len(execution.Targets) != 1 || execution.Targets[0].Status != "failed" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 1 || browsers.stopCalls != 1 || browsers.running || !execution.Targets[0].BrowserClosed {
		t.Fatalf("start=%d stop=%d running=%v", browsers.startCalls, browsers.stopCalls, browsers.running)
	}
}

func TestExecutorSubmitDoesNotAggregateCancelledTargetAsPublished(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	publisher := &submitPlanPublisher{status: string(publish.StatusCancelled)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status == string(publish.StatusPublished) || len(execution.Targets) != 1 ||
		execution.Targets[0].Status != string(publish.StatusCancelled) {
		t.Fatalf("execution=%+v", execution)
	}
	var persisted string
	if err := database.QueryRow(`SELECT status FROM publish_plans WHERE id=?`, plan.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted == string(publish.StatusPublished) {
		t.Fatalf("cancelled submit persisted plan status=%q", persisted)
	}
}

func TestExecutorSubmitContinuesWhenLiveLoginEvidenceIsUnknown(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	publisher := &submitPlanPublisher{status: string(publish.StatusPublished)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{},
		LoginProbe: func(context.Context, string, int) (*logincheck.Observation, error) {
			return &logincheck.Observation{AuthState: logincheck.AuthUnknown}, nil
		},
		PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_verification" || len(execution.Targets) != 1 || execution.Targets[0].Status != "awaiting_review" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if browsers.startCalls != 1 || browsers.stopCalls != 0 || !browsers.running {
		t.Fatalf("start=%d stop=%d running=%v", browsers.startCalls, browsers.stopCalls, browsers.running)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("publisher did not run with non-logout evidence: %+v", publisher.requests)
	}
}
