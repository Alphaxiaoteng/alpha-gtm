package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

func TestCreationDraftRequiresManualConfirmationForLocalRevise(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/creation/drafts", bytes.NewBufferString(`{
		"type":"video","title":"待核验脚本","caption":"文案","body":"脚本正文","tags_json":"[]",
		"review_result":"本地审核结论：revise\n- 请人工核验事实","manual_review_confirmed":false
	}`)))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "manual review confirmation required") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var count int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM contents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("blocked creation draft persisted count=%d err=%v", count, err)
	}
}

func TestCreationDraftPersistsAuditedLocalDraftAfterManualConfirmation(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/creation/drafts", bytes.NewBufferString(`{
		"type":"video","title":"人工核验脚本","caption":"克制文案","body":"## 选题证据\n真实来源\n\n## 脚本\n正文","tags_json":"[\"创作工作台\"]",
		"review_result":"本地审核结论：revise\n- 已提示逐条核验","manual_review_confirmed":true
	}`)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Content            db.Content `json:"content"`
		ReviewDecision     string     `json:"review_decision"`
		ReviewSnapshotHash string     `json:"review_snapshot_hash"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Content.Status != "draft" || response.ReviewDecision != "revise" || len(response.ReviewSnapshotHash) != 64 {
		t.Fatalf("response=%+v", response)
	}
	stored, err := server.db.GetContent(response.Content.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"## 审核记录", "人工确认：是", "审核快照 SHA-256：" + response.ReviewSnapshotHash} {
		if !strings.Contains(stored.Body, expected) {
			t.Fatalf("stored body missing %q: %s", expected, stored.Body)
		}
	}
	if stored.Status != "draft" {
		t.Fatalf("creation commit escaped draft status: %+v", stored)
	}
}

func TestCreationDraftRejectsAmbiguousReview(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/creation/drafts", bytes.NewBufferString(`{
		"type":"video","title":"含糊审核","body":"正文","tags_json":"[]",
		"review_result":"审核结论：pass\n另一个检查建议 revise","manual_review_confirmed":true
	}`)))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "review decision is ambiguous") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
