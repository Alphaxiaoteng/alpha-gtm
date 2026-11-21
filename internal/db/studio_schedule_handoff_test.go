package db_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func TestStudioScheduleHandoffPersistsOneReadyRevisionIdempotently(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES ('conversation','test','test','{}',?,?)`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{
		ID: "studio-schedule-workflow", ConversationID: "conversation", Title: "Studio schedule", Stage: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed title", "", "Reviewed body", `["reviewed"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	input := db.StudioScheduleHandoffInput{
		WorkflowID: workflow.ID, ContentID: content.ID,
		OriginalLocalTime: "2030-08-15T19:48", Timezone: "Asia/Shanghai",
	}
	first, created, err := database.CreateStudioScheduleHandoff(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !created || first.Status != db.StudioScheduleHandoffReady || first.Revision != 1 || first.PlannedAtUTC != "2030-08-15T11:48:00Z" || first.CanonicalHash == "" || first.IntentHash == "" {
		t.Fatalf("first=%+v created=%v", first, created)
	}
	second, created, err := database.CreateStudioScheduleHandoff(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("idempotent handoff first=%+v second=%+v created=%v", first, second, created)
	}
	items, err := database.ListStudioScheduleHandoffs(context.Background(), 10)
	if err != nil || len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err := database.ReplaceContent(content.ID, "article", "Changed title", "", "Reviewed body", `["reviewed"]`, "draft"); err != nil {
		t.Fatal(err)
	}
	var rawStatus, rawUpdatedAt string
	if err := database.QueryRow(`SELECT status,updated_at FROM studio_schedule_handoffs WHERE id=?`, first.ID).Scan(&rawStatus, &rawUpdatedAt); err != nil {
		t.Fatal(err)
	}
	stale, err := database.GetStudioScheduleHandoff(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != db.StudioScheduleHandoffSuperseded {
		t.Fatalf("stale=%+v", stale)
	}
	items, err = database.ListStudioScheduleHandoffs(context.Background(), 10)
	if err != nil || len(items) != 1 || items[0].Status != db.StudioScheduleHandoffSuperseded {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	var persistedStatus, persistedUpdatedAt string
	if err := database.QueryRow(`SELECT status,updated_at FROM studio_schedule_handoffs WHERE id=?`, first.ID).Scan(&persistedStatus, &persistedUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if persistedStatus != rawStatus || persistedUpdatedAt != rawUpdatedAt {
		t.Fatalf("read changed persisted handoff: before status=%q updated_at=%q, after status=%q updated_at=%q", rawStatus, rawUpdatedAt, persistedStatus, persistedUpdatedAt)
	}
	if _, err := database.Exec(`UPDATE studio_schedule_handoffs SET planned_at_utc=? WHERE id=?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), first.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status,updated_at FROM studio_schedule_handoffs WHERE id=?`, first.ID).Scan(&rawStatus, &rawUpdatedAt); err != nil {
		t.Fatal(err)
	}
	expired, err := database.GetStudioScheduleHandoff(context.Background(), first.ID)
	if err != nil || expired.Status != db.StudioScheduleHandoffExpired {
		t.Fatalf("expired=%+v err=%v", expired, err)
	}
	items, err = database.ListStudioScheduleHandoffs(context.Background(), 10)
	if err != nil || len(items) != 1 || items[0].Status != db.StudioScheduleHandoffExpired {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err := database.QueryRow(`SELECT status,updated_at FROM studio_schedule_handoffs WHERE id=?`, first.ID).Scan(&persistedStatus, &persistedUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if persistedStatus != rawStatus || persistedUpdatedAt != rawUpdatedAt {
		t.Fatalf("read changed persisted expired handoff: before status=%q updated_at=%q, after status=%q updated_at=%q", rawStatus, rawUpdatedAt, persistedStatus, persistedUpdatedAt)
	}
}

func TestStudioScheduleHandoffConcurrentCreateReplaysOneReadyIntent(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	second, err := db.Open(database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES ('concurrent-handoff','test','test','{}',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{
		ID: "concurrent-handoff", ConversationID: "concurrent-handoff", Title: "Concurrent handoff", Stage: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed title", "", "Reviewed body", `["reviewed"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	input := db.StudioScheduleHandoffInput{
		WorkflowID: workflow.ID, ContentID: content.ID, OriginalLocalTime: "2030-08-15T19:48", Timezone: "Asia/Shanghai",
	}
	type createResult struct {
		handoff *db.StudioScheduleHandoff
		created bool
		err     error
	}
	results := make(chan createResult, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for _, handle := range []*db.DB{database, second} {
		group.Add(1)
		go func(handle *db.DB) {
			defer group.Done()
			<-start
			handoff, created, err := handle.CreateStudioScheduleHandoff(context.Background(), input)
			results <- createResult{handoff: handoff, created: created, err: err}
		}(handle)
	}
	close(start)
	group.Wait()
	close(results)

	var got []createResult
	for result := range results {
		if result.err != nil || result.handoff == nil {
			t.Fatalf("concurrent create result=%+v", result)
		}
		got = append(got, result)
	}
	if len(got) != 2 || got[0].handoff.ID != got[1].handoff.ID {
		t.Fatalf("concurrent handoffs=%+v", got)
	}
	created := 0
	for _, result := range got {
		if result.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created count=%d, results=%+v", created, got)
	}
	var ready, total int
	if err := database.QueryRow(`SELECT COUNT(*) FROM studio_schedule_handoffs WHERE workflow_id=? AND status='ready'`, workflow.ID).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM studio_schedule_handoffs WHERE workflow_id=?`, workflow.ID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if ready != 1 || total != 1 {
		t.Fatalf("ready=%d total=%d, want exactly one ready handoff", ready, total)
	}
}

func TestStudioScheduleHandoffRejectsInvalidAndAmbiguousWallTime(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES ('conversation','test','test','{}',?,?)`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{
		ID: "studio-schedule-time", ConversationID: "conversation", Title: "Studio schedule", Stage: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed title", "", "Reviewed body", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, local, zone, want string
	}{
		{"invalid zone", "2030-08-15T19:48", "Not/AZone", "EGO_SCHEDULE_TIMEZONE_INVALID"},
		{"seconds", "2030-08-15T19:48:01", "Asia/Shanghai", "EGO_SCHEDULE_TIMEZONE_INVALID"},
		{"spring gap", "2030-03-10T02:30", "America/New_York", "EGO_SCHEDULE_TIMEZONE_INVALID"},
		{"fall overlap", "2030-11-03T01:30", "America/New_York", "EGO_SCHEDULE_DST_AMBIGUOUS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := database.CreateStudioScheduleHandoff(context.Background(), db.StudioScheduleHandoffInput{
				WorkflowID: workflow.ID, ContentID: content.ID, OriginalLocalTime: test.local, Timezone: test.zone,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want %s", err, test.want)
			}
		})
	}
}

func TestStudioScheduleHandoffRequiresReviewedWorkflowAndCreatesNoRow(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed title", "", "Reviewed body", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"topic", "copy", "script"} {
		t.Run(stage, func(t *testing.T) {
			conversationID := "conversation-" + stage
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES (?,?, 'test','{}',?,?)`, conversationID, stage, now, now); err != nil {
				t.Fatal(err)
			}
			workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{
				ID: "studio-schedule-" + stage, ConversationID: conversationID, Title: "Studio schedule", Stage: stage,
			})
			if err != nil {
				t.Fatal(err)
			}
			handoff, created, err := database.CreateStudioScheduleHandoff(context.Background(), db.StudioScheduleHandoffInput{
				WorkflowID: workflow.ID, ContentID: content.ID, OriginalLocalTime: "2030-08-15T19:48", Timezone: "Asia/Shanghai",
			})
			if err == nil || !strings.Contains(err.Error(), "EGO_SCHEDULE_WORKFLOW_NOT_REVIEW") || handoff != nil || created {
				t.Fatalf("handoff=%+v created=%v err=%v", handoff, created, err)
			}
			var count int
			if err := database.QueryRow(`SELECT COUNT(*) FROM studio_schedule_handoffs WHERE workflow_id=?`, workflow.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("non-review workflow created %d handoff rows", count)
			}
		})
	}
}

func TestStudioScheduleHandoffRejectsPastOrCurrentWallTimeAndCreatesNoRow(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES ('conversation-current','test','test','{}',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{
		ID: "studio-schedule-current", ConversationID: "conversation-current", Title: "Studio schedule", Stage: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed title", "", "Reviewed body", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		at   time.Time
	}{
		{name: "current", at: time.Now()},
		{name: "past", at: time.Now().Add(-2 * time.Minute)},
	} {
		t.Run(test.name, func(t *testing.T) {
			handoff, created, err := database.CreateStudioScheduleHandoff(context.Background(), db.StudioScheduleHandoffInput{
				WorkflowID: workflow.ID, ContentID: content.ID, OriginalLocalTime: test.at.In(location).Format("2006-01-02T15:04"), Timezone: "Asia/Shanghai",
			})
			if err == nil || !strings.Contains(err.Error(), "EGO_SCHEDULE_TIME_INVALID") || handoff != nil || created {
				t.Fatalf("handoff=%+v created=%v err=%v", handoff, created, err)
			}
			var count int
			if err := database.QueryRow(`SELECT COUNT(*) FROM studio_schedule_handoffs WHERE workflow_id=?`, workflow.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("%s wall time created %d handoff rows", test.name, count)
			}
		})
	}
}
