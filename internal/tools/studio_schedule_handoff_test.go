package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egopublish"
)

func TestStudioScheduleHandoffToolsExposeOnlyEditorialHandoffAndFreeze(t *testing.T) {
	RegisterCatalog()
	for _, want := range []struct {
		name       string
		sideEffect string
		risk       AgentRisk
	}{
		{"studio.schedule-handoff.create", "write", AgentRiskL2},
		{"studio.schedule-handoff.get", "read", AgentRiskL0},
		{"studio.schedule-handoff.list", "read", AgentRiskL0},
		{"studio.schedule-handoff.cancel", "write", AgentRiskL2},
		{"publish.plan.freeze-handoff", "write", AgentRiskL3},
	} {
		definition, ok := Default.Get(want.name)
		if !ok || definition.SideEffect != want.sideEffect || !definition.AgentVisible || definition.AgentRisk != want.risk {
			t.Fatalf("%s definition=%+v exists=%v", want.name, definition, ok)
		}
	}
	for _, forbidden := range []string{
		"studio.schedule-handoff.execute", "studio.schedule-handoff.run", "publish.plan.schedule", "publish.plan.execute-at",
	} {
		if _, ok := Default.Get(forbidden); ok {
			t.Fatalf("local scheduling tool must not exist: %s", forbidden)
		}
	}
}

func TestFreezeHandoffRegistryRejectsCallerControlledContentAndTime(t *testing.T) {
	result, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.plan.freeze-handoff", map[string]interface{}{
		"handoff_id": float64(1), "targets": []interface{}{}, "content_id": float64(7), "publish_at": "2030-08-15T11:48:00Z",
	})
	if err == nil || result == nil || result.Error != "VALIDATION_FAILED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestFreezeHandoffRegistryRequiresStrictSerialConcurrency(t *testing.T) {
	result, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.plan.freeze-handoff", map[string]interface{}{
		"handoff_id": float64(1), "targets": []interface{}{map[string]interface{}{"account_id": float64(1)}}, "concurrency": float64(2),
	})
	if err == nil || result == nil || result.Error != "VALIDATION_FAILED" || !strings.Contains(err.Error(), "studio schedule handoff concurrency must be 1") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestStudioScheduleToolErrorPreservesCreationGuards(t *testing.T) {
	for _, code := range []string{egopublish.ScheduleWorkflowNotReview, egopublish.ScheduleTimeInvalid} {
		if got := studioScheduleToolError(fmt.Errorf("%s", code)); got != code {
			t.Fatalf("code=%s got=%s", code, got)
		}
	}
}

func TestFreezeHandoffRegistryPreservesUnverifiedNativeScheduleCodeWithoutSpace(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES ('registry-studio','test','test','{}',?,?)`, now, now); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{ID: "registry-studio", ConversationID: "registry-studio", Title: "Registry Studio", Stage: "review"})
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed article", "", "Reviewed body", `["reviewed"]`, "draft")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	handoff, created, err := database.CreateStudioScheduleHandoff(context.Background(), db.StudioScheduleHandoffInput{WorkflowID: workflow.ID, ContentID: content.ID, OriginalLocalTime: "2030-08-15T19:48", Timezone: "Asia/Shanghai"})
	if err != nil || !created || handoff.Status != db.StudioScheduleHandoffReady {
		_ = database.Close()
		t.Fatalf("handoff=%+v created=%v err=%v", handoff, created, err)
	}
	account, err := database.CreateAccount("csdn", "registry-csdn", "")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir}, "publish.plan.freeze-handoff", map[string]interface{}{
		"handoff_id": float64(handoff.ID), "targets": []interface{}{map[string]interface{}{"account_id": float64(account.ID)}},
	})
	if err == nil || result == nil || result.Error != "EGO_PUBLISH_SCHEDULE_UNVERIFIED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	database, err = db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	persisted, err := database.GetStudioScheduleHandoff(context.Background(), handoff.ID)
	if err != nil || persisted.Status != db.StudioScheduleHandoffReady {
		t.Fatalf("handoff=%+v err=%v", persisted, err)
	}
	var plans, spaces int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_plans`).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM ego_spaces`).Scan(&spaces); err != nil {
		t.Fatal(err)
	}
	if plans != 0 || spaces != 0 {
		t.Fatalf("plans=%d spaces=%d, expected fail-closed without browser work", plans, spaces)
	}
}
