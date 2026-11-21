package db_test

import (
	"reflect"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestCreationSessionPersistsAttachmentsAndRuns(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	session, err := database.CreateCreationSession(db.CreationSessionInput{
		Title:            "杭州露营新手避坑",
		TopicBrief:       "新手如何选择营地",
		Style:            "真实、具体",
		Platform:         "xhs",
		SOPStage:         "topic",
		KnowledgeBaseIDs: []int64{3, 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID == "" {
		t.Fatal("creation session id is empty")
	}
	if !reflect.DeepEqual(session.KnowledgeBaseIDs, []int64{3, 8}) {
		t.Fatalf("session attachments=%v", session.KnowledgeBaseIDs)
	}

	run, err := database.AppendCreationRun(db.CreationRunInput{
		SessionID:     session.ID,
		Action:        "draft",
		ProviderID:    "custom",
		ModelID:       "kimi-k2",
		Result:        "一份可审阅的草稿",
		CitationsJSON: `[{"chunk_id":12}]`,
		QualityJSON:   `{"decision":"revise"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.ID == "" {
		t.Fatal("creation run id is empty")
	}

	got, err := database.GetCreationSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.KnowledgeBaseIDs, []int64{3, 8}) {
		t.Fatalf("persisted attachments=%v", got.KnowledgeBaseIDs)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("persisted runs=%+v", got.Runs)
	}
	if got.Runs[0].Action != "draft" || got.Runs[0].ModelID != "kimi-k2" || got.Runs[0].QualityJSON != `{"decision":"revise"}` {
		t.Fatalf("persisted run=%+v", got.Runs[0])
	}
}

func TestCreationModelRoleReplacesOnlyKnownRoles(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.PutCreationModelRole(db.CreationModelRole{Role: "copywriter", ProviderID: "custom", ModelID: "kimi-k2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.PutCreationModelRole(db.CreationModelRole{Role: "copywriter", ProviderID: "custom", ModelID: "gemini-3.6-flash"}); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetCreationModelRole("copywriter")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != "custom" || got.ModelID != "gemini-3.6-flash" {
		t.Fatalf("role=%+v", got)
	}
	if _, err := database.PutCreationModelRole(db.CreationModelRole{Role: "unknown", ProviderID: "custom", ModelID: "kimi-k2"}); err == nil {
		t.Fatal("unknown role was accepted")
	}
}
