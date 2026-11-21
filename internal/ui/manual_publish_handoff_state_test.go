package ui

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/publishplan"
)

func TestUnsupportedPublishPlanFailsWithoutTakeoverAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	identity, err := database.GetEgoIdentity(1)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "zhihu", "知乎合同测试账号", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		database.Close()
		t.Fatal(err)
	}
	space, err := database.CreateEgoSpaceForIdentity(identity.ID, 701, "task-701", "manual-zhihu-space")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}

	content, err := database.CreateContent("article", "知乎合同标题", "", "知乎合同正文", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	planService := &publishplan.Service{DB: database}
	plan, err := planService.Freeze(publishplan.FreezeInput{
		ContentID:   content.ID,
		MediaMode:   "article",
		Concurrency: 1,
		Targets:     []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	grant, err := planService.ApproveForAction(plan.ID, plan.SnapshotHash, "manual-handoff-test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}

	runner := &egoHandlerRunner{}
	server := &Server{
		db:        database,
		cfg:       config.Config{MaxConcurrentSpaces: 1},
		egoSpaces: egolite.NewSpaceService(database, runner),
	}
	result, err := server.preparePublishPlan(context.Background(), plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err == nil || result == nil || result.OK || result.Error != "EGO_PREPARE_PARTIAL_FAILED" {
		t.Fatalf("prepare result=%+v err=%v", result, err)
	}
	reloaded, err := planService.Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != "failed" || len(reloaded.TargetStates) != 1 {
		t.Fatalf("plan=%+v, want failed with one target", reloaded)
	}
	target := reloaded.TargetStates[0]
	if target.PlatformCode != "zhihu" || target.Status != "failed" || target.JobID != 0 || target.ErrorCode != "EGO_PUBLISH_AUTOMATION_UNSUPPORTED" {
		t.Fatalf("target=%+v, want unsupported failure with no job", target)
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=?`, plan.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("publish_jobs=%d, want 0", jobs)
	}
	if _, err := planService.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, publishplan.PublishPlanActionPrepare); err == nil {
		t.Fatal("prepare approval was reusable after manual handoff")
	}
	prepareProgram := ""
	for _, program := range runner.programs {
		// The generated worker contains a guarded delegated-space takeover branch
		// for other ownership states. This unsupported target must still stop
		// before its composer or final submit path; the runtime error/result above
		// proves that branch was not crossed for this agent-owned Space.
		for _, forbidden := range []string{"handOffTaskSpace", "submit approved publish"} {
			if strings.Contains(program, forbidden) {
				t.Fatalf("unsupported Zhihu prepare contains forbidden operation %q:\n%s", forbidden, program)
			}
		}
		if strings.Contains(program, "const preparationResults = []") {
			prepareProgram = program
		}
	}
	manualGuard := strings.Index(prepareProgram, `if (target.prepareMode === "manual_handoff")`)
	composerOpen := strings.Index(prepareProgram, "await openOrReuseTab")
	if prepareProgram == "" || manualGuard < 0 || composerOpen < 0 || manualGuard > composerOpen {
		t.Fatalf("unsupported prepare did not fail before composer open")
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(); err != nil {
		t.Fatal(err)
	}
	restartedPlan, err := (&publishplan.Service{DB: reopened}).Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restartedPlan.Status != "failed" || len(restartedPlan.TargetStates) != 1 ||
		restartedPlan.TargetStates[0].Status != "failed" || restartedPlan.TargetStates[0].JobID != 0 {
		t.Fatalf("restarted plan=%+v, want persisted unsupported failure", restartedPlan)
	}
	restartedSpace, err := reopened.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restartedSpace.SpaceID != space.SpaceID || restartedSpace.Ownership != "agent" || restartedSpace.State != "ready" {
		t.Fatalf("restarted Space=%+v, want agent/ready", restartedSpace)
	}
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=?`, plan.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("restarted publish_jobs=%d, want 0", jobs)
	}
	var consumedAt sql.NullString
	if err := reopened.QueryRow(`SELECT consumed_at FROM approvals WHERE id=?`, grant.ID).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if !consumedAt.Valid || consumedAt.String == "" {
		t.Fatalf("restarted approval consumed_at=%q, want consumed", consumedAt.String)
	}
}
