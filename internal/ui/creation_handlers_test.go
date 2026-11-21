package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestCreationRoleRejectsUnverifiedModel(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/creation/model-roles/copywriter", bytes.NewBufferString(`{"provider_id":"custom","model_id":"kimi-k2"}`)))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "AGENT_MODEL_NOT_VERIFIED") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreationSessionRejectsUnknownJSONFields(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/creation/sessions", bytes.NewBufferString(`{"title":"露营避坑","unexpected":true}`)))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "CREATION_REQUEST_INVALID") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreationRunWithoutExternalPermissionPersistsLocalEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	base, err := server.db.CreateKnowledgeBase("露营资料", "")
	if err != nil {
		t.Fatal(err)
	}

	create := httptest.NewRecorder()
	baseID := strconv.FormatInt(base.ID, 10)
	server.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/creation/sessions", bytes.NewBufferString(`{"title":"露营避坑","knowledge_base_ids":[`+baseID+`]}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	if !strings.Contains(create.Body.String(), `"runs":[]`) {
		t.Fatalf("new session must expose an empty run list, body=%s", create.Body.String())
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(create.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}

	run := httptest.NewRecorder()
	server.ServeHTTP(run, httptest.NewRequest(http.MethodPost, "/api/creation/sessions/"+session.ID+"/runs", bytes.NewBufferString(`{"action":"draft","allow_external":false}`)))
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"source":"local"`) || !strings.Contains(run.Body.String(), `"decision":"revise"`) {
		t.Fatalf("run status=%d body=%s", run.Code, run.Body.String())
	}

	loaded := httptest.NewRecorder()
	server.ServeHTTP(loaded, httptest.NewRequest(http.MethodGet, "/api/creation/sessions/"+session.ID, nil))
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"action":"draft"`) || !strings.Contains(loaded.Body.String(), `"knowledge_base_ids":[`+baseID+`]`) {
		t.Fatalf("loaded status=%d body=%s", loaded.Code, loaded.Body.String())
	}
}
