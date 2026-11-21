package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/agenthub"
)

func TestCreationWorkflowUsesSharedAgentConversationAndExplicitAdvance(t *testing.T) {
	server := testAgentHubServer(t)
	if _, err := server.agentHub.SaveProviderSelection(context.Background(), agenthub.ProviderSelection{ProviderID: agenthub.ProviderCodex, ModelID: "gpt-5.6-sol"}); err != nil {
		t.Fatal(err)
	}

	created := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/creation/workflows", bytes.NewBufferString(`{"title":"露营访谈","stage":"topic"}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var view creationWorkflowView
	if err := json.Unmarshal(created.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Workflow == nil || view.Conversation == nil || view.Workflow.ConversationID != view.Conversation.ID {
		t.Fatalf("workflow is not bound to the shared conversation: %s", created.Body.String())
	}

	run := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/creation/workflows/"+view.Workflow.ID+"/runs", strings.NewReader(`{"stage":"topic","message":"我想做一条真实的露营避坑视频"}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(run, request)
	if run.Code != http.StatusOK {
		t.Fatalf("run=%d body=%s", run.Code, run.Body.String())
	}
	var runResponse struct {
		Run agenthub.Run `json:"run"`
	}
	if err := json.Unmarshal(run.Body.Bytes(), &runResponse); err != nil {
		t.Fatal(err)
	}
	waitForAgentHubRun(t, server, runResponse.Run.ID)

	advanced := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/creation/workflows/"+view.Workflow.ID+"/advance", strings.NewReader(`{"from_stage":"topic","artifact":"一个可核验的露营避坑选题"}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(advanced, request)
	if advanced.Code != http.StatusOK || !strings.Contains(advanced.Body.String(), `"stage":"copy"`) || !strings.Contains(advanced.Body.String(), `"topic":"一个可核验的露营避坑选题"`) {
		t.Fatalf("advance=%d body=%s", advanced.Code, advanced.Body.String())
	}

	loaded := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/creation/workflows/"+view.Workflow.ID, nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(loaded, request)
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"id":"`+view.Conversation.ID+`"`) {
		t.Fatalf("load=%d body=%s", loaded.Code, loaded.Body.String())
	}
}

func TestCreationWorkflowRequiresCurrentVerifiedModel(t *testing.T) {
	server := testAgentHubServer(t)
	if _, err := server.agentHub.SaveProviderSelection(context.Background(), agenthub.ProviderSelection{ProviderID: agenthub.ProviderCodex}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/creation/workflows", strings.NewReader(`{"title":"没有模型也不能伪造"}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "CREATION_MODEL_UNAVAILABLE") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
