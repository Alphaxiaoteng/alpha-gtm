package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/agenthub"
)

func TestAgentBotMemoryRoutesRoundTrip(t *testing.T) {
	server := testAgentHubServer(t)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"记忆面板"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create bot=%d body=%s", created.Code, created.Body.String())
	}
	var createBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	id := createBody.Bot.ID

	empty := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/"+id+"/memory", "")
	if empty.Code != http.StatusOK {
		t.Fatalf("get memory=%d body=%s", empty.Code, empty.Body.String())
	}
	if body := decodeBotMemoryBody(t, empty.Body.Bytes()); body != "" {
		t.Fatalf("fresh bot memory = %q", body)
	}

	content := "# Bot 记忆：记忆面板\n\n## 用户偏好\n- 短句，不要标题党\n"
	put := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+id+"/memory",
		`{"memory":`+quoteJSON(t, content)+`}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put memory=%d body=%s", put.Code, put.Body.String())
	}
	if body := decodeBotMemoryBody(t, put.Body.Bytes()); body != content {
		t.Fatalf("put memory body = %q", body)
	}

	got := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/"+id+"/memory", "")
	if body := decodeBotMemoryBody(t, got.Body.Bytes()); body != content {
		t.Fatalf("get memory=%d body=%q", got.Code, body)
	}

	cleared := agentBotRequest(t, server, http.MethodDelete, "/api/agent-bots/"+id+"/memory", "")
	if cleared.Code != http.StatusOK {
		t.Fatalf("delete memory=%d body=%s", cleared.Code, cleared.Body.String())
	}
	if body := decodeBotMemoryBody(t, cleared.Body.Bytes()); body != "" {
		t.Fatalf("cleared memory = %q", body)
	}
	after := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/"+id+"/memory", "")
	if body := decodeBotMemoryBody(t, after.Body.Bytes()); body != "" {
		t.Fatalf("memory after clear = %q", body)
	}
}

func TestAgentBotMemoryRequiresExistingBotAndSameOrigin(t *testing.T) {
	server := testAgentHubServer(t)

	missing := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/bot_missing/memory", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing bot memory=%d body=%s", missing.Code, missing.Body.String())
	}

	crossSite := agentBotCrossSiteRequest(t, http.MethodGet, "/api/agent-bots/bot_x/memory")
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site memory=%d body=%s", crossSite.Code, crossSite.Body.String())
	}
}

func TestAgentRunPromptCarriesBotMemory(t *testing.T) {
	runner := &agentBotCaptureRunner{requests: make(chan agenthub.RunRequest, 1)}
	server := testAgentHubServerWithRunner(t, runner)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"注入验证"}`)
	var createBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	put := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+createBody.Bot.ID+"/memory",
		`{"memory":"# Bot 记忆：注入验证\n\n## 品牌语气\n- 禁用词：yyds\n"}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put memory=%d body=%s", put.Code, put.Body.String())
	}

	conversation := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","bot_id":"`+createBody.Bot.ID+`","context":{}}`)
	if conversation.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", conversation.Code, conversation.Body.String())
	}
	var conversationBody agenthub.Conversation
	if err := json.Unmarshal(conversation.Body.Bytes(), &conversationBody); err != nil {
		t.Fatal(err)
	}
	run := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations/"+conversationBody.ID+"/runs",
		`{"model_id":"gpt-5.6-sol","message":"写一条文案","context":{}}`)
	if run.Code != http.StatusAccepted {
		t.Fatalf("run=%d body=%s", run.Code, run.Body.String())
	}
	request := <-runner.requests
	if !strings.Contains(request.Message, "【Bot 记忆】") || !strings.Contains(request.Message, "禁用词：yyds") {
		t.Fatalf("memory missing from provider prompt: %q", request.Message)
	}
}

func decodeBotMemoryBody(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		Memory *string `json:"memory"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode memory body: %v raw=%s", err, raw)
	}
	if body.Memory == nil {
		t.Fatalf("memory field missing: %s", raw)
	}
	return *body.Memory
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func agentBotCrossSiteRequest(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(nil))
	request.RemoteAddr = "127.0.0.1:17321"
	request.Header.Set("Origin", "http://evil.example.com")
	recorder := httptest.NewRecorder()
	server := testAgentHubServer(t)
	server.ServeHTTP(recorder, request)
	return recorder
}
