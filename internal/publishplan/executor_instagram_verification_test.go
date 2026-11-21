//go:build legacy_chromium

package publishplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
)

func TestExecutorInstagramSubmitOnlyReportsPublishedAfterPersistedBackendEvidence(t *testing.T) {
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
		VerifyInstagram: func(_ context.Context, jobID int64) (CreatorBackendVerification, error) {
			directory := filepath.Join(database.DataDir, "screenshots")
			if err := os.MkdirAll(directory, 0o700); err != nil {
				return CreatorBackendVerification{}, err
			}
			screenshot := filepath.Join(directory, "instagram-published.png")
			if err := os.WriteFile(screenshot, []byte("creator backend evidence"), 0o600); err != nil {
				return CreatorBackendVerification{}, err
			}
			if _, err := database.RecordCreatorBackendPublished(jobID, db.CreatorBackendEvidence{
				ObservedStatus: "Published",
				PostURL:        "https://www.instagram.com/p/DExecutorVerified/",
				ExternalPostID: "DExecutorVerified",
				ScreenshotPath: screenshot,
			}); err != nil {
				return CreatorBackendVerification{}, err
			}
			return CreatorBackendVerification{Status: "published", Step: "creator_backend_verified"}, nil
		},
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "published_verified" || len(execution.Targets) != 1 ||
		execution.Targets[0].Status != "published_verified" ||
		execution.Targets[0].Step != "creator_backend_verified" {
		t.Fatalf("execution=%+v", execution)
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records
		WHERE job_id=? AND status='published'`, execution.Targets[0].JobID).Scan(&records); err != nil || records != 1 {
		t.Fatalf("records=%d err=%v", records, err)
	}
	if browsers.startCalls != 1 || browsers.stopCalls != 1 || browsers.running || !execution.Targets[0].BrowserClosed {
		t.Fatalf("start=%d stop=%d running=%v", browsers.startCalls, browsers.stopCalls, browsers.running)
	}
}

func TestExecutorKeepsPublishedWhenBrowserCloseFailsAndPersistsDiagnostic(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	prepareAndApproveSubmitPlanForTest(t, database, plan, browsers)
	browsers.stopErr = fmt.Errorf("stop failed")
	publisher := &submitPlanPublisher{status: string(publish.StatusPublished)}
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(platformCode string) (platforms.Publisher, error) {
			publisher.platform = platformCode
			return publisher, nil
		},
		VerifyInstagram: func(_ context.Context, jobID int64) (CreatorBackendVerification, error) {
			directory := filepath.Join(database.DataDir, "screenshots")
			if err := os.MkdirAll(directory, 0o700); err != nil {
				return CreatorBackendVerification{}, err
			}
			screenshot := filepath.Join(directory, "instagram-published.png")
			if err := os.WriteFile(screenshot, []byte("creator backend evidence"), 0o600); err != nil {
				return CreatorBackendVerification{}, err
			}
			if _, err := database.RecordCreatorBackendPublished(jobID, db.CreatorBackendEvidence{
				ObservedStatus: "Published", PostURL: "https://www.instagram.com/p/DExecutorCloseFailure/",
				ExternalPostID: "DExecutorCloseFailure", ScreenshotPath: screenshot,
			}); err != nil {
				return CreatorBackendVerification{}, err
			}
			return CreatorBackendVerification{Status: "published", Step: "creator_backend_verified"}, nil
		},
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	target := execution.Targets[0]
	if execution.Status != "published_verified" || target.Status != "published_verified" || target.BrowserClosed || target.BrowserCloseError != "PUBLISH_BROWSER_CLOSE_FAILED" {
		t.Fatalf("execution=%+v", execution)
	}
	var events int
	if err := database.QueryRow(`SELECT COUNT(*) FROM job_events WHERE job_id=? AND event_type='job.browser_close_failed'`, target.JobID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("close failure events=%d err=%v", events, err)
	}
}

func TestExecutorInstagramSubmitKeepsTimeoutAndUnknownUnpublished(t *testing.T) {
	cases := []struct {
		name string
		step string
		code string
	}{
		{name: "timeout", step: "verification_pending", code: "INSTAGRAM_PUBLISHED_EVIDENCE_NOT_FOUND"},
		{name: "unknown DOM", step: "creator_backend_verification", code: "INSTAGRAM_BACKEND_DOM_UNVERIFIED"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
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
				VerifyInstagram: func(context.Context, int64) (CreatorBackendVerification, error) {
					return CreatorBackendVerification{
						Status: "awaiting_review", Step: test.step, ErrorCode: test.code,
					}, nil
				},
			}

			execution, err := executor.Submit(context.Background(), plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if execution.Status != "awaiting_verification" || len(execution.Targets) != 1 {
				t.Fatalf("execution=%+v", execution)
			}
			target := execution.Targets[0]
			if target.Status != "submitted_pending_verification" || target.Step != test.step || target.ErrorCode != test.code {
				t.Fatalf("target=%+v", target)
			}
			var records int
			if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, target.JobID).Scan(&records); err != nil || records != 0 {
				t.Fatalf("records=%d err=%v", records, err)
			}
			if browsers.stopCalls != 0 || !browsers.running {
				t.Fatalf("stop=%d running=%v", browsers.stopCalls, browsers.running)
			}
		})
	}
}

func TestExecutorRejectsVerifierClaimWithoutPersistedBackendEvidence(t *testing.T) {
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
		VerifyInstagram: func(context.Context, int64) (CreatorBackendVerification, error) {
			return CreatorBackendVerification{Status: "published", Step: "creator_backend_verified"}, nil
		},
	}

	execution, err := executor.Submit(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	target := execution.Targets[0]
	if execution.Status != "awaiting_verification" ||
		target.Status != "submitted_pending_verification" ||
		target.ErrorCode != "INSTAGRAM_PUBLISHED_EVIDENCE_NOT_PERSISTED" {
		t.Fatalf("execution=%+v", execution)
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, target.JobID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
}

func TestExecutorRejectsXHSPublishBeforeCreatorBackendVerification(t *testing.T) {
	database, plan, browsers := newXHSSubmitPlanFixture(t)
	defer database.Close()
	xhsCalls := 0
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		VerifyCreatorBackend: func(_ context.Context, jobID int64) (CreatorBackendVerification, error) {
			xhsCalls++
			return CreatorBackendVerification{Status: "published"}, nil
		},
	}
	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution == nil || execution.Status != "partial_failed" || len(execution.Targets) != 1 {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if target := execution.Targets[0]; target.Status != "failed" || target.ErrorCode != "PUBLISH_PREPARE_FAILED" {
		t.Fatalf("target=%+v", target)
	}
	if xhsCalls != 0 {
		t.Fatalf("read-only XHS reached creator-backend verification %d times", xhsCalls)
	}
}

type xhsPreparePublisher struct{}

func (xhsPreparePublisher) Platform() string { return "xhs" }
func (xhsPreparePublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (xhsPreparePublisher) Validate(platforms.PublishRequest) error { return nil }
func (xhsPreparePublisher) Publish(context.Context, platforms.PublishRequest) (*platforms.PublishResult, error) {
	return &platforms.PublishResult{
		Status: string(publish.StatusAwaitingReview), Step: "review_or_submit", ErrorCode: "AWAITING_HUMAN",
	}, nil
}

func prepareAndApproveXHSSubmitPlanForTest(t *testing.T, database *db.DB, plan *Plan, browsers *submitPlanBrowsers) {
	t.Helper()
	executor := &Executor{
		DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
		PublisherFor: func(string) (platforms.Publisher, error) { return xhsPreparePublisher{}, nil },
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
	if _, err := service.ConsumeApprovalForAction(
		plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit,
	); err != nil {
		t.Fatal(err)
	}
	browsers.startCalls = 0
	browsers.stopCalls = 0
	browsers.startOpts = nil
}

func newXHSSubmitPlanFixture(t *testing.T) (*db.DB, *Plan, *submitPlanBrowsers) {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "XHS title", "XHS caption", "XHS body", `["xhs"]`, "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "xhs.png")
	const payload = "xhs-image"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		database.Close()
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	imageAsset, err := database.CreateMediaAsset(
		content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)),
		hex.EncodeToString(digest[:]), 100, 100, 0,
	)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "XHS operator", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	if _, err := database.Exec(`UPDATE account_assets SET name='极客小腾', external_id='alpha-gamer' WHERE id=?`, assets[0].ID); err != nil {
		database.Close()
		t.Fatal(err)
	}
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{imageAsset.ID},
		Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	return database, plan, &submitPlanBrowsers{database: database}
}
