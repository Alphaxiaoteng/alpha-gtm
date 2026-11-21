package ui

import (
	"encoding/json"
	"net/http"
	"testing"

	"alpha-nexus-egolite/internal/agenthub"
)

func TestAgentConversationToolCallsRoute(t *testing.T) {
	server := testAgentHubServer(t)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","title":"工具时间轴","context":{"page":"bot-chat"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", created.Code, created.Body.String())
	}
	var conversation agenthub.Conversation
	if err := json.Unmarshal(created.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}

	listed := agentBotRequest(t, server, http.MethodGet,
		"/api/agent/conversations/"+conversation.ID+"/tool-calls?limit=10", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("tool calls=%d body=%s", listed.Code, listed.Body.String())
	}
	var body struct {
		ToolCalls []agenthub.ToolCallRecord `json:"tool_calls"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ToolCalls == nil {
		t.Fatal("empty conversation must return an array, not null")
	}
	if len(body.ToolCalls) != 0 {
		t.Fatalf("tool calls = %d, want 0", len(body.ToolCalls))
	}

	// An unknown conversation is an empty timeline, not a 404: the drawer opens
	// before the first tool ever runs.
	missing := agentBotRequest(t, server, http.MethodGet, "/api/agent/conversations/conv_missing/tool-calls", "")
	if missing.Code != http.StatusOK {
		t.Fatalf("missing conversation=%d body=%s", missing.Code, missing.Body.String())
	}

	// The route is registered for GET only; a write attempt must not succeed.
	post := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations/"+conversation.ID+"/tool-calls", "{}")
	if post.Code != http.StatusMethodNotAllowed && post.Code != http.StatusNotFound {
		t.Fatalf("POST tool calls=%d, want 404/405", post.Code)
	}
}
