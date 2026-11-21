//go:build legacy_chromium

package publishplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	"alpha-nexus-egolite/internal/publishworker"
)

type fakePlanBrowsers struct{}

func (fakePlanBrowsers) Status(accountID int64) (*browser.AccountStatus, error) {
	return &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), Running: true, State: browser.StateReady, CDPPort: 15106, CDPURL: "http://127.0.0.1:15106/"}, nil
}

type stoppedPlanBrowsers struct{}

func (stoppedPlanBrowsers) Status(accountID int64) (*browser.AccountStatus, error) {
	return &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID)}, nil
}

type degradedPlanBrowsers struct{}

func (degradedPlanBrowsers) Status(accountID int64) (*browser.AccountStatus, error) {
	return &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), Running: true, State: browser.StateDegraded, CDPPort: 15106, CDPURL: "http://127.0.0.1:15106/"}, nil
}

func insertPlanRuntime(t *testing.T, database *db.DB, accountID int64, port int) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ready', ?, ?, ?)`, browser.ProfileIDForAccount(accountID), accountID, 12000+accountID, port, "http://127.0.0.1:15106/", now, now, now); err != nil {
		t.Fatal(err)
	}
}

func approvePlanForTest(t *testing.T, service *Service, plan *Plan) {
	t.Helper()
	grant, err := service.Approve(plan.ID, plan.SnapshotHash, "desktop", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce); err != nil {
		t.Fatal(err)
	}
}

func newConnectedMetaPlanFixture(t *testing.T) (*db.DB, *Plan, *submitPlanBrowsers, *db.AccountAsset, *db.AccountAsset) {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "Meta identity", "", "Meta body", `["meta"]`, "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "meta-identity-drift.png")
	const payload = "meta-identity-drift"
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
	account, err := database.CreateAccount("meta", "Meta operator", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	connected, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Alpha IG", "ig-public-1", "", 0, "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	selected, err := database.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "page-public-1", "", connected.ID, connected.Name)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID},
		Targets: []TargetInput{{AccountID: account.ID, AssetID: selected.ID}},
	})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	return database, plan, &submitPlanBrowsers{database: database}, selected, connected
}

func TestExecutorRejectsFrozenTargetIdentityDriftBeforeBrowser(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *db.DB, *db.AccountAsset, *db.AccountAsset)
	}{
		{name: "asset type", mutate: func(t *testing.T, database *db.DB, selected, _ *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET asset_type='facebook_group' WHERE id=?`, selected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "asset name", mutate: func(t *testing.T, database *db.DB, selected, _ *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET name='Other Page' WHERE id=?`, selected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "asset external id", mutate: func(t *testing.T, database *db.DB, selected, _ *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET external_id='page-public-2' WHERE id=?`, selected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "asset enabled", mutate: func(t *testing.T, database *db.DB, selected, _ *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET enabled=0 WHERE id=?`, selected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "account platform", mutate: func(t *testing.T, database *db.DB, selected, _ *db.AccountAsset) {
			_, err := database.Exec(`UPDATE accounts SET platform_code='xhs' WHERE id=?`, selected.AccountID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "connected asset id", mutate: func(t *testing.T, database *db.DB, selected, _ *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET connected_asset_id=NULL WHERE id=?`, selected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "connected asset name", mutate: func(t *testing.T, database *db.DB, selected, _ *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET connected_asset_name='Other IG' WHERE id=?`, selected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "connected current name", mutate: func(t *testing.T, database *db.DB, _, connected *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET name='Renamed IG' WHERE id=?`, connected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "connected platform", mutate: func(t *testing.T, database *db.DB, _, connected *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET platform_code='xhs' WHERE id=?`, connected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "connected asset type", mutate: func(t *testing.T, database *db.DB, _, connected *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET asset_type='browser_account' WHERE id=?`, connected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "connected external id", mutate: func(t *testing.T, database *db.DB, _, connected *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET external_id='ig-public-2' WHERE id=?`, connected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "connected enabled", mutate: func(t *testing.T, database *db.DB, _, connected *db.AccountAsset) {
			_, err := database.Exec(`UPDATE account_assets SET enabled=0 WHERE id=?`, connected.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, plan, browsers, selected, connected := newConnectedMetaPlanFixture(t)
			defer database.Close()
			test.mutate(t, database, selected, connected)
			publisher := &submitPlanPublisher{status: string(publish.StatusAwaitingReview)}
			executor := &Executor{
				DB: database, Browsers: browsers, Worker: &fakePlanWorker{}, LoginProbe: loggedInPlanProbe,
				PublisherFor: func(string) (platforms.Publisher, error) { return publisher, nil },
			}

			execution, err := executor.Prepare(context.Background(), plan.ID)
			if err == nil || !strings.Contains(err.Error(), "PUBLISH_TARGET_SNAPSHOT_MISMATCH") || execution != nil {
				t.Fatalf("execution=%+v err=%v", execution, err)
			}
			if browsers.startCalls != 0 || len(publisher.requests) != 0 {
				t.Fatalf("drift reached browser: starts=%d requests=%d", browsers.startCalls, len(publisher.requests))
			}
			persisted, getErr := (&Service{DB: database}).Get(plan.ID)
			if getErr != nil || persisted.Status != StatusApproved {
				t.Fatalf("persisted=%+v err=%v", persisted, getErr)
			}
		})
	}
}

func markPlanAccountLoggedIn(t *testing.T, database *db.DB, accountID int64) {
	t.Helper()
	if err := database.UpdateAccountLoginState(accountID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
}

type fakePlanWorker struct {
	calls []publishworker.Input
}

type inspectPlanWorker struct {
	calls []publishworker.Input
}

func (w *inspectPlanWorker) Run(_ context.Context, input publishworker.Input) (*publishworker.Output, error) {
	w.calls = append(w.calls, input)
	return &publishworker.Output{
		SchemaVersion: 1, OK: true, Status: "inspected", Step: input.Action,
		Evidence: map[string]interface{}{"revealedSection": "发布设置"},
	}, nil
}

type fakeMetaPlanPublisher struct {
	requests []platforms.PublishRequest
}

func (p *fakeMetaPlanPublisher) Platform() string { return "instagram" }
func (p *fakeMetaPlanPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *fakeMetaPlanPublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *fakeMetaPlanPublisher) Publish(_ context.Context, request platforms.PublishRequest) (*platforms.PublishResult, error) {
	p.requests = append(p.requests, request)
	return &platforms.PublishResult{Status: "awaiting_review", Step: "review_or_submit", ErrorCode: "AWAITING_HUMAN"}, nil
}

func (w *fakePlanWorker) Run(_ context.Context, input publishworker.Input) (*publishworker.Output, error) {
	w.calls = append(w.calls, input)
	return &publishworker.Output{SchemaVersion: 1, OK: true, Status: "awaiting_review", Step: "ready_to_submit"}, nil
}

func TestExecutorRevealSettingsUsesExistingBrowserWithoutCreatingJob(t *testing.T) {
	database, plan, browsers := newSubmitPlanFixture(t)
	defer database.Close()
	browsers.running = true
	worker := &inspectPlanWorker{}
	executor := &Executor{DB: database, Browsers: browsers, Worker: worker, LoginProbe: loggedInPlanProbe}

	inspection, err := executor.RevealSettings(context.Background(), plan.ID)
	if err != nil || inspection == nil || inspection.Evidence["revealedSection"] != "发布设置" {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	if len(worker.calls) != 1 || worker.calls[0].Action != "inspect_settings" || browsers.startCalls != 0 {
		t.Fatalf("calls=%+v startCalls=%d", worker.calls, browsers.startCalls)
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs=%d err=%v", jobs, err)
	}
}

func TestExecutorInspectManagerPassesFrozenPublishTime(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "scheduled title", "", "scheduled body", `["alpha","gamer","launch"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("wechat_mp", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	publishAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	plan, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "article", PublishAt: publishAt,
		Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	insertPlanRuntime(t, database, account.ID, 15106)
	worker := &inspectPlanWorker{}
	executor := &Executor{DB: database, Browsers: fakePlanBrowsers{}, Worker: worker, LoginProbe: loggedInPlanProbe}
	if _, err := executor.InspectManager(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if len(worker.calls) != 1 || worker.calls[0].PublishAt != publishAt {
		t.Fatalf("worker calls=%+v", worker.calls)
	}
}

func loggedInPlanProbe(_ context.Context, _ string, _ int) (*logincheck.Observation, error) {
	return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn}, nil
}

func testExecutor(database *db.DB, browsers BrowserStatusProvider, worker *fakePlanWorker) *Executor {
	return &Executor{DB: database, Browsers: browsers, Worker: worker, LoginProbe: loggedInPlanProbe}
}

func TestExecutorPreparesApprovedTargetsSeriallyWithoutSubmit(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("article", "title", "caption", "body", `["article-tag"]`, "draft")
	account, _ := database.CreateAccount("csdn", "creator", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", MediaIDs: []int64{}, Targets: []TargetInput{{AccountID: account.ID, SettingsJSON: `{}`}}})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Approve(plan.ID, plan.SnapshotHash, "desktop", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 12345, 15106, 'http://127.0.0.1:15106/', 'ready', ?, ?, ?)`, browser.ProfileIDForAccount(account.ID), account.ID, now, now, now); err != nil {
		t.Fatal(err)
	}
	worker := &fakePlanWorker{}
	executor := testExecutor(database, fakePlanBrowsers{}, worker)
	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_review" || len(execution.Targets) != 1 || execution.Targets[0].Status != "awaiting_review" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if len(worker.calls) != 1 || worker.calls[0].Action != "prepare" || worker.calls[0].AllowSubmit {
		t.Fatalf("worker calls=%+v", worker.calls)
	}
	persisted, err := service.Get(plan.ID)
	if err != nil || persisted.Status != "awaiting_review" {
		t.Fatalf("plan=%+v err=%v", persisted, err)
	}
	var targetStatus string
	var jobID int64
	if err := database.QueryRow(`SELECT status, publish_job_id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetStatus, &jobID); err != nil || targetStatus != "awaiting_review" || jobID <= 0 {
		t.Fatalf("target status=%s job=%d err=%v", targetStatus, jobID, err)
	}
	job, err := database.GetPublishJob(jobID)
	if err != nil || job.Mode != "prepare" || job.Status != "awaiting_review" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records`).Scan(&records); err != nil || records != 0 {
		t.Fatalf("prepare created publish records: records=%d err=%v", records, err)
	}
	if _, err := executor.Prepare(context.Background(), plan.ID); err == nil || err.Error() != "PUBLISH_PLAN_APPROVAL_REQUIRED" {
		t.Fatalf("repeat prepare err=%v", err)
	}
	if len(worker.calls) != 1 {
		t.Fatalf("repeat prepare reran worker: calls=%d", len(worker.calls))
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("repeat prepare created jobs: jobs=%d err=%v", jobs, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records`).Scan(&records); err != nil || records != 0 {
		t.Fatalf("repeat prepare created publish records: records=%d err=%v", records, err)
	}
}

func TestExecutorRoutesFrozenComplianceThroughReviewedMetaPublisherWithoutSubmit(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent(
		"graph_text",
		"Meta title",
		"Meta caption\n"+AlphaGamerAIDisclosure,
		"Meta body",
		`["meta"]`,
		"draft",
	)
	imagePath := filepath.Join(t.TempDir(), "meta.png")
	const payload = "meta-image"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	image, _ := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)), hex.EncodeToString(digest[:]), 100, 100, 0)
	account, _ := database.CreateAccount("instagram", "Meta operator", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	target, _ := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "AlphaIntelligence", "ig-public-1", "", 0, "")
	insertPlanRuntime(t, database, account.ID, 15106)
	service := &Service{DB: database}
	policy := AlphaGamerCompliancePolicy()
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID},
		CompliancePolicy: &policy,
		Targets:          []TargetInput{{AccountID: account.ID, AssetID: target.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)

	metaPublisher := &fakeMetaPlanPublisher{}
	worker := &fakePlanWorker{}
	executor := testExecutor(database, fakePlanBrowsers{}, worker)
	executor.PublisherFor = func(platform string) (platforms.Publisher, error) {
		if platform != "instagram" {
			t.Fatalf("platform=%s", platform)
		}
		return metaPublisher, nil
	}
	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_review" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if len(metaPublisher.requests) != 1 || metaPublisher.requests[0].AutoSubmit || metaPublisher.requests[0].AssetID != target.ID {
		t.Fatalf("requests=%+v", metaPublisher.requests)
	}
	wantCompliance, err := json.Marshal(plan.Snapshot.Targets[0].ComplianceFields)
	if err != nil {
		t.Fatal(err)
	}
	if got := metaPublisher.requests[0].ComplianceJSON; got != string(wantCompliance) {
		t.Fatalf("compliance=%s want=%s", got, wantCompliance)
	}
	if len(worker.calls) != 0 {
		t.Fatalf("Meta plan used generic worker: %+v", worker.calls)
	}
}

func TestExecutorRevalidatesPlannedTimeImmediatelyBeforePlatformFill(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	frozenAt := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	content, err := database.CreateContentWithPlannedAt(
		"graph_text", "title", "", "body", `["graph-tag"]`, "draft",
		frozenAt.Add(30*time.Minute).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "scheduled.png")
	if err := os.WriteFile(imagePath, []byte("scheduled-image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("scheduled-image"))
	image, err := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len("scheduled-image")), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	insertPlanRuntime(t, database, account.ID, 15106)
	service := &Service{DB: database, Now: func() time.Time { return frozenAt }}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID}, Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	worker := &fakePlanWorker{}
	executor := testExecutor(database, fakePlanBrowsers{}, worker)
	executor.Now = func() time.Time { return frozenAt.Add(20 * time.Minute) }

	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(execution.Targets) != 1 || execution.Targets[0].ErrorCode != "PUBLISH_TIME_INVALID" {
		t.Fatalf("execution=%+v", execution)
	}
	if len(worker.calls) != 0 {
		t.Fatalf("worker calls=%d, want zero", len(worker.calls))
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs=%d err=%v", jobs, err)
	}
}

func TestExecutorFailsClosedWhenTargetStatusCannotPersist(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "title", "caption", "body", `["article-tag"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	insertPlanRuntime(t, database, account.ID, 15106)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	if _, err := database.Exec(`CREATE TRIGGER block_publish_plan_target_finish
		BEFORE UPDATE OF status ON publish_plan_targets WHEN NEW.status='awaiting_review'
		BEGIN SELECT RAISE(ABORT, 'publish plan target update blocked'); END`); err != nil {
		t.Fatal(err)
	}

	worker := &fakePlanWorker{}
	execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), publishPlanTargetPersistError) {
		t.Fatalf("err=%v, want %s", err, publishPlanTargetPersistError)
	}
	if execution != nil {
		t.Fatalf("target persistence failure must not return an execution: %+v", execution)
	}
	if len(worker.calls) != 1 {
		t.Fatalf("worker calls=%d, want one controlled prepare", len(worker.calls))
	}
	persisted, err := service.Get(plan.ID)
	if err != nil || persisted.Status != "running" {
		t.Fatalf("plan=%+v err=%v; failed persistence must not claim awaiting review", persisted, err)
	}
	var targetStatus string
	if err := database.QueryRow(`SELECT status FROM publish_plan_targets WHERE plan_id=? AND account_id=?`, plan.ID, account.ID).Scan(&targetStatus); err != nil || targetStatus != "running" {
		t.Fatalf("target status=%q err=%v", targetStatus, err)
	}
}

func TestExecutorFailsAJobWhenItsRunningTargetLinkCannotPersist(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "title", "caption", "body", `["article-tag"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	insertPlanRuntime(t, database, account.ID, 15106)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	if _, err := database.Exec(`CREATE TRIGGER block_publish_plan_target_link
		BEFORE UPDATE OF status ON publish_plan_targets WHEN NEW.status='running'
		BEGIN SELECT RAISE(ABORT, 'publish plan target link blocked'); END`); err != nil {
		t.Fatal(err)
	}

	worker := &fakePlanWorker{}
	execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), publishPlanTargetPersistError) {
		t.Fatalf("err=%v, want %s", err, publishPlanTargetPersistError)
	}
	if execution != nil || len(worker.calls) != 0 {
		t.Fatalf("execution=%+v worker calls=%d", execution, len(worker.calls))
	}
	var status, step, code string
	if err := database.QueryRow(`SELECT status, current_step, error_code FROM publish_jobs`).Scan(&status, &step, &code); err != nil || status != "failed" || step != "plan_target_persist" || code != publishPlanTargetPersistError {
		t.Fatalf("job status=%q step=%q code=%q err=%v", status, step, code, err)
	}
	var pending int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE status='pending'`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("pending jobs=%d err=%v", pending, err)
	}
}

func TestExecutorFailsClosedWhenFinalPlanStatusCannotPersist(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "title", "caption", "body", `["article-tag"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	insertPlanRuntime(t, database, account.ID, 15106)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	if _, err := database.Exec(`CREATE TRIGGER block_publish_plan_finish
		BEFORE UPDATE OF status ON publish_plans WHEN NEW.status='awaiting_review'
		BEGIN SELECT RAISE(ABORT, 'publish plan finish blocked'); END`); err != nil {
		t.Fatal(err)
	}

	worker := &fakePlanWorker{}
	execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), publishPlanStatusPersistError) {
		t.Fatalf("err=%v, want %s", err, publishPlanStatusPersistError)
	}
	if execution != nil {
		t.Fatalf("plan persistence failure must not return an execution: %+v", execution)
	}
	if len(worker.calls) != 1 {
		t.Fatalf("worker calls=%d, want one controlled prepare", len(worker.calls))
	}
	persisted, err := service.Get(plan.ID)
	if err != nil || persisted.Status != "running" {
		t.Fatalf("plan=%+v err=%v; failed persistence must not claim awaiting review", persisted, err)
	}
	var targetStatus string
	if err := database.QueryRow(`SELECT status FROM publish_plan_targets WHERE plan_id=? AND account_id=?`, plan.ID, account.ID).Scan(&targetStatus); err != nil || targetStatus != "awaiting_review" {
		t.Fatalf("target status=%q err=%v", targetStatus, err)
	}
}

func TestExecutorRejectsFrozenUnapprovedPlan(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	account, _ := database.CreateAccount("csdn", "creator", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	plan, err := (&Service{DB: database}).Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = testExecutor(database, fakePlanBrowsers{}, &fakePlanWorker{}).Prepare(context.Background(), plan.ID)
	if err == nil || err.Error() != "PUBLISH_PLAN_APPROVAL_REQUIRED" {
		t.Fatalf("err=%v", err)
	}
}

func TestExecutorRejectsAccountThatLoggedOutAfterPlanFreeze(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	account, _ := database.CreateAccount("csdn", "creator", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedOut); err != nil {
		t.Fatal(err)
	}
	worker := &fakePlanWorker{}
	execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "partial_failed" || len(execution.Targets) != 1 || execution.Targets[0].ErrorCode != "PUBLISH_ACCOUNT_NOT_LOGGED_IN" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if len(worker.calls) != 0 {
		t.Fatalf("worker ran for logged-out account: %+v", worker.calls)
	}
	var jobCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobCount); err != nil || jobCount != 0 {
		t.Fatalf("jobs=%d err=%v", jobCount, err)
	}
}

func TestExecutorContinuesToNextTargetAfterLoggedOutTarget(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	loggedOut, _ := database.CreateAccount("csdn", "logged-out", "")
	loggedIn, _ := database.CreateAccount("csdn", "logged-in", "")
	markPlanAccountLoggedIn(t, database, loggedOut.ID)
	markPlanAccountLoggedIn(t, database, loggedIn.ID)
	insertPlanRuntime(t, database, loggedIn.ID, 15106)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID,
		MediaMode: "article",
		Targets: []TargetInput{
			{AccountID: loggedOut.ID},
			{AccountID: loggedIn.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	if err := database.UpdateAccountLoginState(loggedOut.ID, db.AccountAuthLoggedOut); err != nil {
		t.Fatal(err)
	}
	worker := &fakePlanWorker{}
	execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "partial_failed" || len(execution.Targets) != 2 {
		t.Fatalf("execution=%+v", execution)
	}
	if execution.Targets[0].ErrorCode != "PUBLISH_ACCOUNT_NOT_LOGGED_IN" {
		t.Fatalf("first target=%+v", execution.Targets[0])
	}
	if execution.Targets[1].Status != "awaiting_review" || len(worker.calls) != 1 {
		t.Fatalf("second target=%+v worker calls=%d", execution.Targets[1], len(worker.calls))
	}
}

func TestExecutorRejectsLiveLogoutAfterPlanFreezeBeforeCreatingJob(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	account, _ := database.CreateAccount("csdn", "creator", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)

	probeCalls := 0
	worker := &fakePlanWorker{}
	executor := &Executor{
		DB: database, Browsers: fakePlanBrowsers{}, Worker: worker,
		LoginProbe: func(_ context.Context, platformCode string, cdpPort int) (*logincheck.Observation, error) {
			probeCalls++
			if platformCode != "csdn" || cdpPort != 15106 {
				t.Errorf("probe platform=%q port=%d", platformCode, cdpPort)
			}
			var activeLeases int
			if err := database.QueryRow(`SELECT COUNT(*) FROM browser_operation_leases WHERE profile_id=?`, browser.ProfileIDForAccount(account.ID)).Scan(&activeLeases); err != nil || activeLeases != 1 {
				t.Errorf("live probe must run under publish lease: leases=%d err=%v", activeLeases, err)
			}
			return &logincheck.Observation{AuthState: logincheck.AuthLoggedOut}, nil
		},
	}
	execution, err := executor.Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "partial_failed" || len(execution.Targets) != 1 || execution.Targets[0].ErrorCode != "PUBLISH_ACCOUNT_SESSION_LOGGED_OUT" {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	if probeCalls != 1 || len(worker.calls) != 0 {
		t.Fatalf("probe calls=%d worker calls=%+v", probeCalls, worker.calls)
	}
	var jobs, targetJobID int64
	var targetStatus, targetCode string
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs=%d err=%v", jobs, err)
	}
	if err := database.QueryRow(`SELECT status, error_code, COALESCE(publish_job_id,0) FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetStatus, &targetCode, &targetJobID); err != nil || targetStatus != "failed" || targetCode != "PUBLISH_ACCOUNT_SESSION_LOGGED_OUT" || targetJobID != 0 {
		t.Fatalf("target status=%s code=%s job=%d err=%v", targetStatus, targetCode, targetJobID, err)
	}
	acquired, err := database.AcquireBrowserOperationLease(browser.ProfileIDForAccount(account.ID), "test", "live-probe-verify", time.Now(), time.Minute)
	if err != nil || !acquired {
		t.Fatalf("live preflight leaked browser lease: acquired=%v err=%v", acquired, err)
	}
	if err := database.ReleaseBrowserOperationLease(browser.ProfileIDForAccount(account.ID), "live-probe-verify"); err != nil {
		t.Fatal(err)
	}
}

func TestExecutorWaitsForBriefObserverLeaseBeforePreparing(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	account, _ := database.CreateAccount("csdn", "creator", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	insertPlanRuntime(t, database, account.ID, 15106)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	owner := "observe-probe:short-lease"
	profileID := browser.ProfileIDForAccount(account.ID)
	acquired, err := database.AcquireBrowserOperationLease(profileID, "observe", owner, time.Now(), time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquire observer lease: acquired=%v err=%v", acquired, err)
	}
	time.AfterFunc(50*time.Millisecond, func() { _ = database.ReleaseBrowserOperationLease(profileID, owner) })
	worker := &fakePlanWorker{}
	execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
	if err != nil || execution.Status != "awaiting_review" || len(worker.calls) != 1 {
		t.Fatalf("execution=%+v calls=%d err=%v", execution, len(worker.calls), err)
	}
}

func TestExecutorPreflightFailuresDoNotLeavePendingJobs(t *testing.T) {
	tests := []struct {
		name         string
		wantCode     string
		browsers     BrowserStatusProvider
		prepareTest  func(*testing.T, *db.DB, *Plan, int64)
		publisherFor func(string) (platforms.Publisher, error)
	}{
		{
			name:     "browser_operation_busy",
			wantCode: "BROWSER_OPERATION_BUSY",
			browsers: fakePlanBrowsers{},
			prepareTest: func(t *testing.T, database *db.DB, _ *Plan, accountID int64) {
				t.Helper()
				acquired, err := database.AcquireBrowserOperationLease(browser.ProfileIDForAccount(accountID), "test", "executor-test-owner", time.Now(), time.Minute)
				if err != nil || !acquired {
					t.Fatalf("acquire blocking lease: acquired=%v err=%v", acquired, err)
				}
			},
		},
		{
			name:     "browser_not_running",
			wantCode: "PUBLISH_BROWSER_NOT_RUNNING",
			browsers: stoppedPlanBrowsers{},
		},
		{
			name:     "browser_degraded",
			wantCode: "PUBLISH_BROWSER_DEGRADED",
			browsers: degradedPlanBrowsers{},
		},
		{
			name:     "publisher_unavailable",
			wantCode: "PLAYWRIGHT_PUBLISHER_UNAVAILABLE",
			browsers: fakePlanBrowsers{},
			publisherFor: func(string) (platforms.Publisher, error) {
				return nil, fmt.Errorf("fixture publisher unavailable")
			},
		},
		{
			name:     "target_asset_missing",
			wantCode: "PUBLISH_TARGET_NOT_FOUND",
			browsers: fakePlanBrowsers{},
			prepareTest: func(t *testing.T, database *db.DB, plan *Plan, _ int64) {
				t.Helper()
				// Model an asset disappearing after the immutable plan is frozen.
				// Keep the snapshot and publish_plan_targets asset IDs aligned so
				// the executor can persist this target's precise failure even when
				// one Meta login owns both Facebook and Instagram targets.
				if _, err := database.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
					t.Fatal(err)
				}
				if _, err := database.Exec(`DELETE FROM account_assets WHERE id=?`, plan.Snapshot.Targets[0].AssetID); err != nil {
					t.Fatal(err)
				}
				if _, err := database.Exec(`PRAGMA foreign_keys=ON`); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, err := db.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := database.Migrate(); err != nil {
				t.Fatal(err)
			}
			content, err := database.CreateContent("article", "title", "caption", "body", `["article-tag"]`, "draft")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccount("csdn", "creator", "")
			if err != nil {
				t.Fatal(err)
			}
			markPlanAccountLoggedIn(t, database, account.ID)
			service := &Service{DB: database}
			plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}})
			if err != nil {
				t.Fatal(err)
			}
			approvePlanForTest(t, service, plan)
			if test.prepareTest != nil {
				test.prepareTest(t, database, plan, account.ID)
			}

			worker := &fakePlanWorker{}
			executor := testExecutor(database, test.browsers, worker)
			executor.PublisherFor = test.publisherFor
			execution, err := executor.Prepare(context.Background(), plan.ID)
			if test.name == "target_asset_missing" {
				if err == nil || !strings.Contains(err.Error(), "PUBLISH_TARGET_SNAPSHOT_MISMATCH") || execution != nil {
					t.Fatalf("execution=%+v err=%v", execution, err)
				}
				var totalJobs int
				if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&totalJobs); err != nil || totalJobs != 0 {
					t.Fatalf("jobs=%d err=%v", totalJobs, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if execution.Status != "partial_failed" || len(execution.Targets) != 1 || execution.Targets[0].Status != "failed" || execution.Targets[0].ErrorCode != test.wantCode {
				t.Fatalf("execution=%+v", execution)
			}
			if len(worker.calls) != 0 {
				t.Fatalf("worker called during rejected preflight: %+v", worker.calls)
			}
			var totalJobs, pendingJobs int
			if err := database.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0) FROM publish_jobs`).Scan(&totalJobs, &pendingJobs); err != nil {
				t.Fatal(err)
			}
			if totalJobs != 0 || pendingJobs != 0 {
				t.Fatalf("preflight created publish jobs: total=%d pending=%d", totalJobs, pendingJobs)
			}
			var targetStatus, targetCode string
			var targetJobID int64
			if err := database.QueryRow(`SELECT status, error_code, COALESCE(publish_job_id,0) FROM publish_plan_targets WHERE plan_id=? AND account_id=?`, plan.ID, account.ID).Scan(&targetStatus, &targetCode, &targetJobID); err != nil {
				t.Fatal(err)
			}
			if targetStatus != "failed" || targetCode != test.wantCode || targetJobID != 0 {
				t.Fatalf("target status=%s code=%s job=%d", targetStatus, targetCode, targetJobID)
			}
			persisted, err := service.Get(plan.ID)
			if err != nil || persisted.Status != "partial_failed" {
				t.Fatalf("plan=%+v err=%v", persisted, err)
			}
		})
	}
}

func TestExecutorPreparesReviewedArticleAndVideoGroupsWithoutSubmit(t *testing.T) {
	t.Run("wechat_mp_and_csdn_articles", func(t *testing.T) {
		database, err := db.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		if err := database.Migrate(); err != nil {
			t.Fatal(err)
		}
		content, _ := database.CreateContent("article", "article title", "", "article body", `["article-tag"]`, "draft")
		targets := make([]TargetInput, 0, 2)
		for index, code := range []string{"wechat_mp", "csdn"} {
			account, createErr := database.CreateAccount(code, code, "")
			if createErr != nil {
				t.Fatal(createErr)
			}
			markPlanAccountLoggedIn(t, database, account.ID)
			targets = append(targets, TargetInput{AccountID: account.ID, SettingsJSON: `{}`})
			insertPlanRuntime(t, database, account.ID, 15101+index)
		}
		service := &Service{DB: database}
		plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: targets})
		if err != nil {
			t.Fatal(err)
		}
		approvePlanForTest(t, service, plan)
		worker := &fakePlanWorker{}
		execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
		if err != nil || execution.Status != "awaiting_review" || len(worker.calls) != 2 {
			t.Fatalf("execution=%+v calls=%d err=%v", execution, len(worker.calls), err)
		}
		for index, platform := range []string{"wechat_mp", "csdn"} {
			call := worker.calls[index]
			if call.Platform != platform || call.Action != "prepare" || call.AllowSubmit || call.Content.Type != "article" || call.Content.Body != "article body" || call.PublishAt != "" {
				t.Fatalf("call[%d]=%+v", index, call)
			}
		}
	})

	t.Run("four_video_platforms", func(t *testing.T) {
		database, err := db.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		if err := database.Migrate(); err != nil {
			t.Fatal(err)
		}
		plannedAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
		content, _ := database.CreateContentWithPlannedAt("video", "video title", "", "video body", `["video-tag"]`, "draft", plannedAt)
		path := filepath.Join(t.TempDir(), "video.mp4")
		const payload = "video-fixture"
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(payload))
		media, _ := database.CreateMediaAsset(content.ID, "video", path, path, "video/mp4", int64(len(payload)), hex.EncodeToString(digest[:]), 1080, 1920, 1000)
		coverPath := filepath.Join(t.TempDir(), "video-cover.jpg")
		const coverPayload = "video-cover-fixture"
		if err := os.WriteFile(coverPath, []byte(coverPayload), 0o600); err != nil {
			t.Fatal(err)
		}
		coverDigest := sha256.Sum256([]byte(coverPayload))
		cover, _ := database.CreateMediaAsset(content.ID, "image", coverPath, coverPath, "image/jpeg", int64(len(coverPayload)), hex.EncodeToString(coverDigest[:]), 1080, 1920, 0)
		targets := make([]TargetInput, 0, 4)
		platformCodes := []string{"tiktok", "douyin", "kuaishou", "wechat_video"}
		for index, code := range platformCodes {
			account, createErr := database.CreateAccount(code, code, "")
			if createErr != nil {
				t.Fatal(createErr)
			}
			markPlanAccountLoggedIn(t, database, account.ID)
			targets = append(targets, TargetInput{AccountID: account.ID, SettingsJSON: `{}`})
			insertPlanRuntime(t, database, account.ID, 15101+index)
		}
		service := &Service{DB: database}
		plan, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "video", MediaIDs: []int64{media.ID}, CoverMediaID: cover.ID, Targets: targets})
		if err != nil {
			t.Fatal(err)
		}
		approvePlanForTest(t, service, plan)
		worker := &fakePlanWorker{}
		execution, err := testExecutor(database, fakePlanBrowsers{}, worker).Prepare(context.Background(), plan.ID)
		if err != nil || execution.Status != "awaiting_review" || len(worker.calls) != 4 {
			t.Fatalf("execution=%+v calls=%d err=%v", execution, len(worker.calls), err)
		}
		for index, platform := range platformCodes {
			call := worker.calls[index]
			if call.Platform != platform || call.Action != "prepare" || call.AllowSubmit || call.Content.Type != "video" || len(call.Media) != 2 || call.Media[0].Role != "video" || call.Media[1].Role != "cover" || call.PublishAt != plannedAt {
				t.Fatalf("call[%d]=%+v", index, call)
			}
		}
	})
}
