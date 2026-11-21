package ui

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tools"
)

func TestStudioScheduleHandoffRoutesDelegateToRegistryAndKeepFreezeFailClosed(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	if _, err := database.Exec(`INSERT INTO agent_conversations(id,title,provider_id,context_json,created_at,updated_at) VALUES ('studio-handoff','test','test','{}',?,?)`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateCreationWorkflow(context.Background(), db.CreationWorkflowInput{ID: "studio-handoff", ConversationID: "studio-handoff", Title: "Studio handoff", Stage: "review"})
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "Reviewed content", "", "Reviewed body", `["reviewed"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "Studio CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database, mux: http.NewServeMux(), toolRuntime: &tools.Runtime{DataDir: dataDir}}
	server.routes()

	create := httptest.NewRecorder()
	server.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/studio/schedule-handoffs", bytes.NewBufferString(`{"workflowId":"`+workflow.ID+`","contentId":`+itoa(content.ID)+`,"originalLocalTime":"2030-08-15T19:48","timezone":"Asia/Shanghai"}`)))
	if create.Code != http.StatusCreated || !strings.Contains(create.Body.String(), `"status":"ready"`) {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}

	get := httptest.NewRecorder()
	server.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/studio/schedule-handoffs/1", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"workflow_id":"`+workflow.ID+`"`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}

	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/studio/schedule-handoffs?limit=20", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"content_id":`+itoa(content.ID)) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	freeze := httptest.NewRecorder()
	server.ServeHTTP(freeze, httptest.NewRequest(http.MethodPost, "/api/publish-plans/freeze-handoff", bytes.NewBufferString(`{"handoffId":1,"targets":[{"account_id":`+itoa(account.ID)+`}]}`)))
	if freeze.Code != http.StatusConflict || !strings.Contains(freeze.Body.String(), "EGO_PUBLISH_SCHEDULE_UNVERIFIED") {
		t.Fatalf("freeze status=%d body=%s", freeze.Code, freeze.Body.String())
	}
	var spaces int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ego_spaces`).Scan(&spaces); err != nil || spaces != 0 {
		t.Fatalf("spaces=%d err=%v", spaces, err)
	}

	cancel := httptest.NewRecorder()
	server.ServeHTTP(cancel, httptest.NewRequest(http.MethodPost, "/api/studio/schedule-handoffs/1/cancel", nil))
	if cancel.Code != http.StatusOK || !strings.Contains(cancel.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}
