package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/tools"
)

type publishSafetyRunner struct {
	programs      []string
	prepareResult map[string]any
	submitResult  map[string]any
	managerResult map[string]any
	submitErr     error
}

func (runner *publishSafetyRunner) Run(_ context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	if strings.Contains(program, "const submitResults = []") && runner.submitErr != nil {
		return runner.submitErr
	}
	payload := map[string]any{
		"id": 71, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1",
		"ownership": "agent", "done": true,
	}
	if strings.Contains(program, "const boundSpace =") {
		payload = map[string]any{
			"id": 71, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1",
			"ownership": "agent", "done": true,
		}
	}
	if strings.Contains(program, "const preparationResults = []") {
		payload = map[string]any{"done": true, "completionAttempted": true, "completionDone": true, "results": []map[string]any{{
			"platformCode": "douyin", "status": "awaiting_review",
		}}}
		if runner.prepareResult != nil {
			payload = runner.prepareResult
		}
	}
	if strings.Contains(program, "const submitResults = []") && runner.submitResult != nil {
		payload = runner.submitResult
	}
	if strings.Contains(program, "const managerInspection = await js") && runner.managerResult != nil {
		payload = runner.managerResult
	}
	encoded, _ := json.Marshal(payload)
	return json.Unmarshal(encoded, result)
}

type publishSafetyFixture struct {
	database *db.DB
	service  *publishplan.Service
	server   *Server
	runner   *publishSafetyRunner
	plan     *publishplan.Plan
}

func newPublishSafetyFixture(t *testing.T, publishAt string, runner *publishSafetyRunner) *publishSafetyFixture {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.GetEgoIdentity(1)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 71, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "冻结标题", "", "冻结正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-image.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media, err := database.CreateMediaAsset(
		content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBytes)),
		hex.EncodeToString(digest[:]), 1080, 1440, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	service := &publishplan.Service{DB: database}
	plan, err := service.Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID}, PublishAt: publishAt,
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		db: database, cfg: config.Config{MaxConcurrentSpaces: 1},
		egoSpaces: egolite.NewSpaceService(database, runner),
	}
	return &publishSafetyFixture{database: database, service: service, server: server, runner: runner, plan: plan}
}

func (fixture *publishSafetyFixture) prepare(t *testing.T) {
	t.Helper()
	grant, err := fixture.service.ApproveForAction(
		fixture.plan.ID, fixture.plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.server.preparePublishPlan(
		context.Background(), fixture.plan.ID, fixture.plan.SnapshotHash, grant.ID, grant.Nonce,
	)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("prepare result=%+v err=%v", result, err)
	}
}

func (fixture *publishSafetyFixture) submit(t *testing.T) (*db.PublishJob, error) {
	t.Helper()
	grant, err := fixture.service.ApproveForAction(
		fixture.plan.ID, fixture.plan.SnapshotHash, "test-session", publishplan.PublishPlanActionSubmit, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, submitErr := fixture.server.submitPublishPlan(
		context.Background(), fixture.plan.ID, fixture.plan.SnapshotHash, grant.ID, grant.Nonce,
	)
	plan, err := fixture.service.Get(fixture.plan.ID)
	if err != nil || len(plan.TargetStates) != 1 || plan.TargetStates[0].JobID <= 0 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	job, err := fixture.database.GetPublishJob(plan.TargetStates[0].JobID)
	if err != nil {
		t.Fatal(err)
	}
	return job, submitErr
}

func (fixture *publishSafetyFixture) acceptanceSubmit(t *testing.T) (*tools.Result, *db.PublishJob, error) {
	t.Helper()
	grant, err := fixture.service.ApproveForAction(
		fixture.plan.ID, fixture.plan.SnapshotHash, "test-session", publishplan.PublishPlanActionAcceptanceSubmit, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, submitErr := fixture.server.submitAcceptancePublishPlan(
		context.Background(), fixture.plan.ID, fixture.plan.SnapshotHash, grant.ID, grant.Nonce,
	)
	plan, err := fixture.service.Get(fixture.plan.ID)
	if err != nil || len(plan.TargetStates) != 1 || plan.TargetStates[0].JobID <= 0 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	job, err := fixture.database.GetPublishJob(plan.TargetStates[0].JobID)
	if err != nil {
		t.Fatal(err)
	}
	return result, job, submitErr
}

func TestEgoNonMinuteScheduledPublishFailsClosedBeforeOpeningComposer(t *testing.T) {
	runner := &publishSafetyRunner{}
	invalidPublishAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Minute).Add(time.Second).Format(time.RFC3339)
	fixture := newPublishSafetyFixture(t, invalidPublishAt, runner)
	grant, err := fixture.service.ApproveForAction(
		fixture.plan.ID, fixture.plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.server.preparePublishPlan(
		context.Background(), fixture.plan.ID, fixture.plan.SnapshotHash, grant.ID, grant.Nonce,
	)
	if err == nil || result == nil || result.OK || result.Error != "EGO_PREPARE_PARTIAL_FAILED" {
		t.Fatalf("scheduled prepare result=%+v err=%v", result, err)
	}
	targets, ok := result.Data["targets"].([]egoPublishTargetResult)
	if !ok || len(targets) != 1 || targets[0].ErrorCode != "EGO_PUBLISH_SCHEDULE_INVALID" {
		t.Fatalf("scheduled targets=%#v", result.Data["targets"])
	}
	for _, program := range runner.programs {
		if strings.Contains(program, "const preparationResults = []") {
			t.Fatalf("scheduled target opened an immediate-publish composer: %s", program)
		}
	}
}

func TestEgoSubmitVerifiedGatePreventsReservationsAndRuntimeClicks(t *testing.T) {
	runner := &publishSafetyRunner{submitResult: map[string]any{"done": true}}
	fixture := newPublishSafetyFixture(t, "", runner)
	fixture.prepare(t)
	job, submitErr := fixture.submit(t)
	if submitErr == nil || job.Mode != "prepare" || job.SubmitAttemptedAt != "" || job.SubmitOutcome != "" || job.ErrorCode != "" {
		t.Fatalf("unverified submit gate job=%+v err=%v", job, submitErr)
	}
	for _, program := range runner.programs {
		if strings.Contains(program, "const submitResults = []") || strings.Contains(program, "submit approved publish") {
			t.Fatalf("unverified capability reached final submit runtime: %s", program)
		}
	}
}

func TestEgoPrepareLoginChallengePersistsLoginRequiredAndNotification(t *testing.T) {
	runner := &publishSafetyRunner{prepareResult: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{
			"platformCode": "douyin", "status": "failed", "errorCode": "LOGIN_REQUIRED",
		}},
	}}
	fixture := newPublishSafetyFixture(t, "", runner)
	grant, err := fixture.service.ApproveForAction(
		fixture.plan.ID, fixture.plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, prepareErr := fixture.server.preparePublishPlan(
		context.Background(), fixture.plan.ID, fixture.plan.SnapshotHash, grant.ID, grant.Nonce,
	)
	if prepareErr == nil || result == nil || result.Error != "EGO_PREPARE_PARTIAL_FAILED" {
		t.Fatalf("result=%+v err=%v", result, prepareErr)
	}
	targets, ok := result.Data["targets"].([]egoPublishTargetResult)
	if !ok || len(targets) != 1 || targets[0].Status != "login_required" || targets[0].ErrorCode != "LOGIN_REQUIRED" {
		t.Fatalf("targets=%#v", result.Data["targets"])
	}
	plan, err := fixture.service.Get(fixture.plan.ID)
	if err != nil || plan.Status != "login_required" || len(plan.TargetStates) != 1 || plan.TargetStates[0].Status != "login_required" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	var notifications int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM notifications WHERE type='publish.login_required' AND related_type='publish_plan' AND related_id=?`, fixture.plan.ID).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("login notifications=%d", notifications)
	}
}

func TestEgoAcceptanceSubmitPersistsCurrentSQLiteScheduledEvidence(t *testing.T) {
	publishAt := "2030-08-15T11:48:00Z"
	runner := &publishSafetyRunner{
		submitResult: map[string]any{
			"done": true, "completionAttempted": true, "completionDone": true,
			"results": []map[string]any{{
				"platformCode": "douyin", "status": "submitted_pending_verification", "clicked": true,
			}},
		},
		managerResult: map[string]any{
			"done": true, "completionAttempted": true, "completionDone": true,
			"location":         "https://creator.douyin.com/creator-micro/content/manage",
			"inspectionStatus": "inspected", "matchingTitle": true,
			"publishedState": false, "scheduledState": true, "observedStatus": "定时发布",
			"effectiveScheduledAt": publishAt,
			"screenshotBase64":     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
		},
	}
	fixture := newPublishSafetyFixture(t, publishAt, runner)
	fixture.prepare(t)
	result, job, submitErr := fixture.acceptanceSubmit(t)
	if submitErr != nil || result == nil || !result.OK || job.Status != "scheduled_verified" || job.SubmitOutcome != "scheduled_verified" {
		t.Fatalf("result=%+v job=%+v err=%v", result, job, submitErr)
	}
	records, err := fixture.database.ListPublishContentRecords(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].PlanID != fixture.plan.ID || records[0].RemoteState != "scheduled" || records[0].ScheduledAt != publishAt {
		t.Fatalf("current-project records=%+v", records)
	}
	for _, program := range runner.programs {
		if strings.Contains(program, "alpha-nexus/app.db") || strings.Contains(program, ".alpha-nexus/") {
			t.Fatalf("acceptance path referenced a legacy Alpha-Nexus store: %s", program)
		}
	}
}

func TestEgoSubmitVerifiedGatePrecedesMediaReservation(t *testing.T) {
	runner := &publishSafetyRunner{}
	fixture := newPublishSafetyFixture(t, "", runner)
	fixture.prepare(t)
	if err := os.WriteFile(fixture.plan.Snapshot.Media[0].FilePath, []byte("changed-after-prepare"), 0o600); err != nil {
		t.Fatal(err)
	}
	grant, err := fixture.service.ApproveForAction(
		fixture.plan.ID, fixture.plan.SnapshotHash, "test-session", publishplan.PublishPlanActionSubmit, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, submitErr := fixture.server.submitPublishPlan(
		context.Background(), fixture.plan.ID, fixture.plan.SnapshotHash, grant.ID, grant.Nonce,
	)
	if submitErr == nil || result == nil || result.Error != "EGO_SUBMIT_PARTIAL_FAILED" {
		t.Fatalf("result=%+v err=%v", result, submitErr)
	}
	targets, ok := result.Data["targets"].([]egoPublishTargetResult)
	if !ok || len(targets) != 1 || targets[0].Status != "failed" || targets[0].ErrorCode != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("targets=%#v", result.Data["targets"])
	}
	for _, program := range runner.programs {
		if strings.Contains(program, "const submitResults = []") {
			t.Fatalf("changed media reached submit runtime: %s", program)
		}
	}
	var submitJobs int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=? AND mode='submit'`, fixture.plan.ID).Scan(&submitJobs); err != nil {
		t.Fatal(err)
	}
	if submitJobs != 0 {
		t.Fatalf("changed media reserved %d submit job(s)", submitJobs)
	}
}
