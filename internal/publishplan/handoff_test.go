package publishplan

import (
	"context"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func TestFreezeHandoffFailsClosedUntilNativeScheduleIsVerified(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES ('conversation','test','test','{}',?,?)`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{
		ID: "freeze-handoff", ConversationID: "conversation", Title: "Freeze handoff", Stage: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed title", "", "Reviewed body", `["reviewed"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	handoff, _, err := database.CreateStudioScheduleHandoff(context.Background(), db.StudioScheduleHandoffInput{
		WorkflowID: workflow.ID, ContentID: content.ID, OriginalLocalTime: "2030-08-15T19:48", Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Service{DB: database, Now: func() time.Time { return time.Date(2030, 8, 15, 10, 0, 0, 0, time.UTC) }}).FreezeHandoff(FreezeHandoffInput{
		HandoffID: handoff.ID, Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err == nil || !strings.Contains(err.Error(), "EGO_PUBLISH_SCHEDULE_UNVERIFIED") {
		t.Fatalf("err=%v", err)
	}
	reloaded, err := database.GetStudioScheduleHandoff(context.Background(), handoff.ID)
	if err != nil || reloaded.Status != db.StudioScheduleHandoffReady || reloaded.FrozenPlanID != 0 {
		t.Fatalf("handoff=%+v err=%v", reloaded, err)
	}
}

func TestFreezeHandoffRequiresStrictSerialConcurrency(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	service := &Service{DB: database}
	for _, concurrency := range []int{-1, 2, 3} {
		plan, err := service.FreezeHandoff(FreezeHandoffInput{HandoffID: 1, Concurrency: concurrency, Targets: []TargetInput{{AccountID: 1}}})
		if err == nil || !strings.Contains(err.Error(), "studio schedule handoff concurrency must be 1") || plan != nil {
			t.Fatalf("concurrency=%d plan=%+v err=%v", concurrency, plan, err)
		}
	}
}
