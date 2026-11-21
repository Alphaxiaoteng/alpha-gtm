package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/agenthub"
)

func TestAgentDelegationRoutesRoundTrip(t *testing.T) {
	server := testAgentHubServer(t)

	conversationResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","title":"主线程","context":{"page":"bot-chat"}}`)
	if conversationResponse.Code != http.StatusCreated {
		t.Fatalf("create conversation=%d body=%s", conversationResponse.Code, conversationResponse.Body.String())
	}
	var conversation agenthub.Conversation
	if err := json.Unmarshal(conversationResponse.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}

	botResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"开发","role":"工程"}`)
	if botResponse.Code != http.StatusCreated {
		t.Fatalf("create bot=%d body=%s", botResponse.Code, botResponse.Body.String())
	}
	var botBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(botResponse.Body.Bytes(), &botBody); err != nil {
		t.Fatal(err)
	}

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent/delegations",
		`{"parent_conversation_id":"`+conversation.ID+`","to_bot_id":"`+botBody.Bot.ID+`","task":"检查登录流程"}`)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create delegation=%d body=%s", created.Code, created.Body.String())
	}
	var delegation agenthub.Delegation
	if err := json.Unmarshal(created.Body.Bytes(), &delegation); err != nil {
		t.Fatal(err)
	}
	if delegation.Status != agenthub.DelegationRunning || delegation.ChildRunID == "" {
		t.Fatalf("unexpected delegation: %+v", delegation)
	}

	listed := agentBotRequest(t, server, http.MethodGet, "/api/agent/delegations?conversation_id="+conversation.ID, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list delegation=%d body=%s", listed.Code, listed.Body.String())
	}
	var listBody struct {
		Delegations []agenthub.Delegation `json:"delegations"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Delegations) != 1 || listBody.Delegations[0].ID != delegation.ID {
		t.Fatalf("list body=%s", listed.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		one := agentBotRequest(t, server, http.MethodGet, "/api/agent/delegations/"+delegation.ID, "")
		if one.Code != http.StatusOK {
			t.Fatalf("get delegation=%d body=%s", one.Code, one.Body.String())
		}
		if err := json.Unmarshal(one.Body.Bytes(), &delegation); err != nil {
			t.Fatal(err)
		}
		if delegation.Status != agenthub.DelegationRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delegation did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if delegation.Status != agenthub.DelegationCompleted || delegation.Result != "offline reply" {
		t.Fatalf("finished delegation=%+v", delegation)
	}
}

func TestAgentDelegationRejectsUnknownBot(t *testing.T) {
	server := testAgentHubServer(t)
	conversationResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","title":"主线程","context":{"page":"bot-chat"}}`)
	var conversation agenthub.Conversation
	if err := json.Unmarshal(conversationResponse.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}

	missing := agentBotRequest(t, server, http.MethodPost, "/api/agent/delegations",
		`{"parent_conversation_id":"`+conversation.ID+`","to_bot_id":"bot_missing","task":"x"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing bot=%d body=%s", missing.Code, missing.Body.String())
	}
	if !containsJSONError(missing.Body.Bytes(), "AGENT_BOT_NOT_FOUND") {
		t.Fatalf("missing bot body=%s", missing.Body.String())
	}

	botResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"开发"}`)
	var botBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(botResponse.Body.Bytes(), &botBody); err != nil {
		t.Fatal(err)
	}
	self := agentBotRequest(t, server, http.MethodPost, "/api/agent/delegations",
		`{"parent_conversation_id":"`+conversation.ID+`","from_bot_id":"`+botBody.Bot.ID+`","to_bot_id":"`+botBody.Bot.ID+`","task":"x"}`)
	if self.Code != http.StatusBadRequest {
		t.Fatalf("self delegation=%d body=%s", self.Code, self.Body.String())
	}
}

func containsJSONError(body []byte, want string) bool {
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload["error"] == want
}

type blockingDelegationRunner struct{ release chan struct{} }

func (r blockingDelegationRunner) Run(ctx context.Context, _ agenthub.RunRequest, _ func(agenthub.ProviderEvent)) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-r.release:
		return "done", nil
	}
}

func TestAgentDelegationRejectsDuplicateInFlight(t *testing.T) {
	runner := blockingDelegationRunner{release: make(chan struct{})}
	defer close(runner.release)
	server := testAgentHubServerWithRunner(t, runner)

	conversationResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","title":"主线程","context":{"page":"bot-chat"}}`)
	var conversation agenthub.Conversation
	if err := json.Unmarshal(conversationResponse.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}
	botResponse := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"开发"}`)
	var botBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(botResponse.Body.Bytes(), &botBody); err != nil {
		t.Fatal(err)
	}
	payload := `{"parent_conversation_id":"` + conversation.ID + `","to_bot_id":"` + botBody.Bot.ID + `","task":"第一件"}`
	if first := agentBotRequest(t, server, http.MethodPost, "/api/agent/delegations", payload); first.Code != http.StatusAccepted {
		t.Fatalf("first=%d body=%s", first.Code, first.Body.String())
	}
	second := agentBotRequest(t, server, http.MethodPost, "/api/agent/delegations", payload)
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate=%d body=%s", second.Code, second.Body.String())
	}
	if !containsJSONError(second.Body.Bytes(), "AGENT_DELEGATION_BUSY") {
		t.Fatalf("duplicate body=%s", second.Body.String())
	}
}
