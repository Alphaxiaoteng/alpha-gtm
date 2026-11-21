package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/agenthub"
)

const agentBotTestModel = "codex/gpt-5.6-sol"

func agentBotRequest(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, path, reader)
	request.RemoteAddr = "127.0.0.1:17321"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestAgentBotRoutesCRUDRoundTrip(t *testing.T) {
	server := testAgentHubServer(t)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots",
		`{"name":"  资料管家 ","role":"研究助理","instructions":"只引用证据","model":"`+agentBotTestModel+`","avatar":"books","color":"#4F7CFF","effort":"high","kickoff":"读资料"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var createBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Bot.ID == "" || createBody.Bot.Name != "资料管家" {
		t.Fatalf("unexpected created bot: %+v", createBody.Bot)
	}
	if createBody.Bot.Capabilities == nil || len(createBody.Bot.Capabilities) != 0 {
		t.Fatalf("capabilities must serialize as []: %s", created.Body.String())
	}

	listed := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list=%d body=%s", listed.Code, listed.Body.String())
	}
	var listBody struct {
		Bots []agenthub.Bot `json:"bots"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	// The hub seeds one built-in main Agent row on startup; the CRUD contract
	// here is about the persona this test just created, so exclude that row.
	userBots := 0
	var createdFound bool
	for _, bot := range listBody.Bots {
		if bot.ID == agenthub.BuiltinAlphaAgentID {
			continue
		}
		userBots++
		createdFound = createdFound || bot.ID == createBody.Bot.ID
	}
	if userBots != 1 || !createdFound {
		t.Fatalf("list body=%s", listed.Body.String())
	}

	updated := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+createBody.Bot.ID, `{"role":"内容策划","effort":""}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update=%d body=%s", updated.Code, updated.Body.String())
	}
	var updateBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &updateBody); err != nil {
		t.Fatal(err)
	}
	if updateBody.Bot.Role != "内容策划" || updateBody.Bot.Effort != "" || updateBody.Bot.Name != "资料管家" {
		t.Fatalf("partial update body=%s", updated.Body.String())
	}

	removed := agentBotRequest(t, server, http.MethodDelete, "/api/agent-bots/"+createBody.Bot.ID, "")
	if removed.Code != http.StatusOK {
		t.Fatalf("delete=%d body=%s", removed.Code, removed.Body.String())
	}
	if again := agentBotRequest(t, server, http.MethodDelete, "/api/agent-bots/"+createBody.Bot.ID, ""); again.Code != http.StatusNotFound {
		t.Fatalf("second delete=%d body=%s", again.Code, again.Body.String())
	}
}

func TestAgentBotRoutesRejectInvalidModelAndName(t *testing.T) {
	server := testAgentHubServer(t)

	badModel := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"A","model":"gpt-5.6-sol"}`)
	if badModel.Code != http.StatusBadRequest || !strings.Contains(badModel.Body.String(), "AGENT_MODEL_INVALID") {
		t.Fatalf("bad model=%d body=%s", badModel.Code, badModel.Body.String())
	}

	badName := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"   "}`)
	if badName.Code != http.StatusBadRequest || !strings.Contains(badName.Body.String(), "AGENT_BOT_NAME_REQUIRED") {
		t.Fatalf("bad name=%d body=%s", badName.Code, badName.Body.String())
	}

	missing := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/bot_missing", `{"role":"x"}`)
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "AGENT_BOT_NOT_FOUND") {
		t.Fatalf("missing bot=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestAgentBotRoutesRejectCrossSite(t *testing.T) {
	server := testAgentHubServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/agent-bots", ""},
		{http.MethodPost, "/api/agent-bots", `{"name":"A"}`},
		{http.MethodPut, "/api/agent-bots/bot_x", `{"role":"x"}`},
		{http.MethodDelete, "/api/agent-bots/bot_x", ""},
	} {
		request := httptest.NewRequest(tc.method, tc.path, bytes.NewReader([]byte(tc.body)))
		request.RemoteAddr = "127.0.0.1:17321"
		request.Header.Set("Origin", "http://evil.example.com")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s cross-site=%d body=%s", tc.method, tc.path, recorder.Code, recorder.Body.String())
		}
	}
}

type agentBotCaptureRunner struct{ requests chan agenthub.RunRequest }

func (runner *agentBotCaptureRunner) Run(_ context.Context, request agenthub.RunRequest, emit func(agenthub.ProviderEvent)) (string, error) {
	runner.requests <- request
	emit(agenthub.ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": "bot reply"}})
	return "test-thread-bot", nil
}

func TestAgentConversationBindsBotAndInjectsInstructions(t *testing.T) {
	runner := &agentBotCaptureRunner{requests: make(chan agenthub.RunRequest, 1)}
	server := testAgentHubServerWithRunner(t, runner)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots",
		`{"name":"资料管家","instructions":"只能引用带 [KB:id] 的证据"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create bot=%d body=%s", created.Code, created.Body.String())
	}
	var botBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &botBody); err != nil {
		t.Fatal(err)
	}

	conversation := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","bot_id":"`+botBody.Bot.ID+`","context":{}}`)
	if conversation.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", conversation.Code, conversation.Body.String())
	}
	var conversationBody agenthub.Conversation
	if err := json.Unmarshal(conversation.Body.Bytes(), &conversationBody); err != nil {
		t.Fatal(err)
	}
	if conversationBody.BotID != botBody.Bot.ID {
		t.Fatalf("conversation bot_id=%q body=%s", conversationBody.BotID, conversation.Body.String())
	}

	dangling := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","bot_id":"bot_missing","context":{}}`)
	if dangling.Code != http.StatusNotFound {
		t.Fatalf("dangling bot=%d body=%s", dangling.Code, dangling.Body.String())
	}

	run := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations/"+conversationBody.ID+"/runs",
		`{"model_id":"gpt-5.6-sol","message":"你好","context":{}}`)
	if run.Code != http.StatusAccepted {
		t.Fatalf("run=%d body=%s", run.Code, run.Body.String())
	}
	request := <-runner.requests
	if !strings.Contains(request.Instructions, "只能引用带 [KB:id] 的证据") ||
		!strings.Contains(request.Message, "只能引用带 [KB:id] 的证据") {
		t.Fatalf("bot instructions missing from run: %+v", request)
	}

	deleted := agentBotRequest(t, server, http.MethodDelete, "/api/agent-bots/"+botBody.Bot.ID, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete bot=%d body=%s", deleted.Code, deleted.Body.String())
	}
	after := agentBotRequest(t, server, http.MethodGet, "/api/agent/conversations/"+conversationBody.ID, "")
	if after.Code != http.StatusOK {
		t.Fatalf("conversation after bot delete=%d body=%s", after.Code, after.Body.String())
	}
	var afterBody agenthub.Conversation
	if err := json.Unmarshal(after.Body.Bytes(), &afterBody); err != nil {
		t.Fatal(err)
	}
	if afterBody.BotID != "" {
		t.Fatalf("conversation still bound to deleted bot: %q", afterBody.BotID)
	}
}

func TestAgentBotGetByID(t *testing.T) {
	server := testAgentHubServer(t)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"单查测试","role":"测试"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var body struct {
		Bot struct {
			ID string `json:"id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Bot.ID == "" {
		t.Fatalf("missing id: %s", created.Body.String())
	}

	got := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/"+body.Bot.ID, "")
	if got.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	var single struct {
		Bot struct {
			Name string `json:"name"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &single); err != nil {
		t.Fatal(err)
	}
	if single.Bot.Name != "单查测试" {
		t.Fatalf("name = %q", single.Bot.Name)
	}

	missing := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/bot_nope", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestAgentBotPinEndpoint(t *testing.T) {
	server := testAgentHubServer(t)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"置顶候选"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var botBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &botBody); err != nil {
		t.Fatal(err)
	}
	if botBody.Bot.Pinned {
		t.Fatalf("new bot is pinned by default: %+v", botBody.Bot)
	}

	pinned := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+botBody.Bot.ID+"/pin", `{"pinned":true}`)
	if pinned.Code != http.StatusOK {
		t.Fatalf("pin=%d body=%s", pinned.Code, pinned.Body.String())
	}
	var pinnedBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(pinned.Body.Bytes(), &pinnedBody); err != nil {
		t.Fatal(err)
	}
	if !pinnedBody.Bot.Pinned {
		t.Fatalf("pin not stored: %s", pinned.Body.String())
	}

	// The body must say which way; an empty object is a client bug, not "unpin".
	missingFlag := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+botBody.Bot.ID+"/pin", `{}`)
	if missingFlag.Code != http.StatusBadRequest {
		t.Fatalf("missing pinned flag=%d body=%s", missingFlag.Code, missingFlag.Body.String())
	}

	unpinned := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+botBody.Bot.ID+"/pin", `{"pinned":false}`)
	if unpinned.Code != http.StatusOK {
		t.Fatalf("unpin=%d body=%s", unpinned.Code, unpinned.Body.String())
	}
	var unpinnedBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(unpinned.Body.Bytes(), &unpinnedBody); err != nil {
		t.Fatal(err)
	}
	if unpinnedBody.Bot.Pinned {
		t.Fatalf("unpin not stored: %s", unpinned.Body.String())
	}

	missing := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/bot_missing/pin", `{"pinned":true}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing bot pin=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestAgentBotPrimaryEndpointDemotesTheRest(t *testing.T) {
	server := testAgentHubServer(t)

	ids := make([]string, 0, 2)
	for _, name := range []string{"一号", "二号"} {
		created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"`+name+`"}`)
		if created.Code != http.StatusCreated {
			t.Fatalf("create %s=%d body=%s", name, created.Code, created.Body.String())
		}
		var body struct {
			Bot agenthub.Bot `json:"bot"`
		}
		if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, body.Bot.ID)
	}

	for _, id := range ids {
		promoted := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+id+"/primary", "")
		if promoted.Code != http.StatusOK {
			t.Fatalf("primary=%d body=%s", promoted.Code, promoted.Body.String())
		}
		var body struct {
			Bot agenthub.Bot `json:"bot"`
		}
		if err := json.Unmarshal(promoted.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if !body.Bot.IsPrimary || body.Bot.ID != id {
			t.Fatalf("primary response = %+v", body.Bot)
		}
	}

	listed := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list=%d body=%s", listed.Code, listed.Body.String())
	}
	var listBody struct {
		Bots []agenthub.Bot `json:"bots"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	primaries := 0
	for _, bot := range listBody.Bots {
		if bot.IsPrimary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Fatalf("primary count = %d in %s", primaries, listed.Body.String())
	}
	// The surviving primary is the last one promoted, and it leads the list.
	if listBody.Bots[0].ID != ids[1] || !listBody.Bots[0].IsPrimary {
		t.Fatalf("list head = %+v", listBody.Bots[0])
	}

	missing := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/bot_missing/primary", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing bot primary=%d body=%s", missing.Code, missing.Body.String())
	}
}

// Creating a conversation for a persona opens it with the kickoff line, and a
// rebind must not append a second copy.
func TestAgentConversationSeedsBotKickoff(t *testing.T) {
	server := testAgentHubServer(t)
	const kickoff = "今天想聊什么选题？"

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots",
		`{"name":"选题助手","kickoff":"`+kickoff+`"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create bot=%d body=%s", created.Code, created.Body.String())
	}
	var botBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &botBody); err != nil {
		t.Fatal(err)
	}

	conversation := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","bot_id":"`+botBody.Bot.ID+`","context":{}}`)
	if conversation.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", conversation.Code, conversation.Body.String())
	}
	var body agenthub.Conversation
	if err := json.Unmarshal(conversation.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("messages = %d, want only the kickoff: %s", len(body.Messages), conversation.Body.String())
	}
	if body.Messages[0].Role != "assistant" || body.Messages[0].Content != kickoff {
		t.Fatalf("first message = %+v", body.Messages[0])
	}

	// Re-binding the same persona through PATCH must not duplicate the greeting.
	rebound := agentBotRequest(t, server, http.MethodPatch, "/api/agent/conversations/"+body.ID,
		`{"bot_id":"`+botBody.Bot.ID+`"}`)
	if rebound.Code != http.StatusOK {
		t.Fatalf("rebind=%d body=%s", rebound.Code, rebound.Body.String())
	}
	after := agentBotRequest(t, server, http.MethodGet, "/api/agent/conversations/"+body.ID, "")
	if after.Code != http.StatusOK {
		t.Fatalf("get conversation=%d body=%s", after.Code, after.Body.String())
	}
	var afterBody agenthub.Conversation
	if err := json.Unmarshal(after.Body.Bytes(), &afterBody); err != nil {
		t.Fatal(err)
	}
	if len(afterBody.Messages) != 1 {
		t.Fatalf("kickoff duplicated after rebind: %d messages", len(afterBody.Messages))
	}
}

func TestAgentConversationsReadAllClearsRailUnread(t *testing.T) {
	server := testAgentHubServer(t)
	botResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"开发"}`)
	var botBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(botResponse.Body.Bytes(), &botBody); err != nil {
		t.Fatal(err)
	}
	conversationResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","bot_id":"`+botBody.Bot.ID+`","title":"开发","context":{"page":"bot-chat"}}`)
	var conversation agenthub.Conversation
	if err := json.Unmarshal(conversationResponse.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}
	if conversationResponse.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", conversationResponse.Code, conversationResponse.Body.String())
	}
	run := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations/"+conversation.ID+"/runs",
		`{"model_id":"gpt-5.6-sol","message":"你好","context":{"page":"bot-chat"}}`)
	if run.Code != http.StatusAccepted {
		t.Fatalf("run=%d body=%s", run.Code, run.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rail := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/rail", "")
		var railBody struct {
			Bots []agenthub.BotRailItem `json:"bots"`
		}
		if err := json.Unmarshal(rail.Body.Bytes(), &railBody); err != nil {
			t.Fatal(err)
		}
		var unread int
		for _, item := range railBody.Bots {
			if item.ID == botBody.Bot.ID {
				unread = item.Unread
			}
		}
		if unread > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rail never reported unread: %s", rail.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	readAll := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations/read-all",
		`{"bot_id":"`+botBody.Bot.ID+`"}`)
	if readAll.Code != http.StatusOK {
		t.Fatalf("read-all=%d body=%s", readAll.Code, readAll.Body.String())
	}
	rail := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/rail", "")
	var railBody struct {
		Bots []agenthub.BotRailItem `json:"bots"`
	}
	if err := json.Unmarshal(rail.Body.Bytes(), &railBody); err != nil {
		t.Fatal(err)
	}
	for _, item := range railBody.Bots {
		if item.ID == botBody.Bot.ID && item.Unread != 0 {
			t.Fatalf("after read-all unread=%d, want 0", item.Unread)
		}
	}
}
