package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/agenthub"
)

// railBody mirrors GET /api/agent-bots/rail.
type railBody struct {
	Bots        []agenthub.BotRailItem `json:"bots"`
	UnreadTotal int                    `json:"unread_total"`
}

func TestAgentBotRailAggregatesGroupMuteAndUnread(t *testing.T) {
	server := testAgentHubServer(t)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots",
		`{"name":"UI","group_name":" 开发团队 ","muted":true,"model":"`+agentBotTestModel+`"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var createBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Bot.GroupName != "开发团队" || !createBody.Bot.Muted {
		t.Fatalf("group/mute not stored: %+v", createBody.Bot)
	}

	conversation := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations",
		`{"provider_id":"codex","model_id":"gpt-5.6-sol","bot_id":"`+createBody.Bot.ID+`","context":{}}`)
	if conversation.Code != http.StatusCreated {
		t.Fatalf("conversation=%d body=%s", conversation.Code, conversation.Body.String())
	}
	var conversationBody agenthub.Conversation
	if err := json.Unmarshal(conversation.Body.Bytes(), &conversationBody); err != nil {
		t.Fatal(err)
	}

	readRail := func() railBody {
		t.Helper()
		recorder := agentBotRequest(t, server, http.MethodGet, "/api/agent-bots/rail", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("rail=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var body railBody
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		// The hub seeds a built-in main Agent row (and it sorts first as the
		// primary). This test asserts the rail projection of the persona it just
		// created, so drop the built-in row from the list under test.
		filtered := body.Bots[:0]
		for _, bot := range body.Bots {
			if bot.ID != agenthub.BuiltinAlphaAgentID {
				filtered = append(filtered, bot)
			}
		}
		body.Bots = filtered
		return body
	}

	// A fresh conversation has no messages, so nothing is unread.
	if body := readRail(); body.UnreadTotal != 0 || len(body.Bots) != 1 || body.Bots[0].Unread != 0 {
		t.Fatalf("empty rail = %+v", body)
	}

	// One inbound message is the whole unread contract: newer than last_read_at.
	messageAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := server.db.ExecContext(context.Background(),
		`INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at)
		 VALUES('msg_rail','`+conversationBody.ID+`','codex','','assistant','服务正忙。请稍后重试。','{}','`+messageAt+`')`); err != nil {
		t.Fatal(err)
	}
	body := readRail()
	if body.UnreadTotal != 1 || body.Bots[0].Unread != 1 {
		t.Fatalf("unread rail = %+v", body)
	}
	if body.Bots[0].LastMessage != "服务正忙。请稍后重试。" || body.Bots[0].LastMessageAt != messageAt {
		t.Fatalf("last message = %+v", body.Bots[0])
	}
	if body.Bots[0].GroupName != "开发团队" || !body.Bots[0].Muted {
		t.Fatalf("rail lost group/mute: %+v", body.Bots[0])
	}

	// Opening the conversation clears its badge.
	read := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations/"+conversationBody.ID+"/read", "")
	if read.Code != http.StatusOK {
		t.Fatalf("read=%d body=%s", read.Code, read.Body.String())
	}
	var readResult struct {
		Read      bool   `json:"read"`
		ReadAt    string `json:"last_read_at"`
		Untouched string `json:"conversation_id"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &readResult); err != nil {
		t.Fatal(err)
	}
	if !readResult.Read || readResult.ReadAt == "" || readResult.Untouched != conversationBody.ID {
		t.Fatalf("read response = %s", read.Body.String())
	}
	if body := readRail(); body.UnreadTotal != 0 || body.Bots[0].Unread != 0 {
		t.Fatalf("rail after read = %+v", body)
	}

	// The watermark is durable, not a frontend-only flag.
	var lastReadAt string
	if err := server.db.QueryRow(`SELECT last_read_at FROM agent_conversations WHERE id=?`, conversationBody.ID).Scan(&lastReadAt); err != nil {
		t.Fatal(err)
	}
	if lastReadAt != readResult.ReadAt {
		t.Fatalf("stored last_read_at = %q, want %q", lastReadAt, readResult.ReadAt)
	}
}

func TestAgentBotRailAndReadErrorPaths(t *testing.T) {
	server := testAgentHubServer(t)

	if missing := agentBotRequest(t, server, http.MethodPost, "/api/agent/conversations/conv_missing/read", ""); missing.Code != http.StatusNotFound {
		t.Fatalf("read missing conversation=%d body=%s", missing.Code, missing.Body.String())
	}
	if crossSite := agentBotCrossSiteRequest(t, http.MethodGet, "/api/agent-bots/rail"); crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site rail=%d body=%s", crossSite.Code, crossSite.Body.String())
	}
}

// TestAgentBotPatchGroupAndMute proves the settings form can rewrite both new
// fields without touching the rest of the persona.
func TestAgentBotPatchGroupAndMute(t *testing.T) {
	server := testAgentHubServer(t)

	created := agentBotRequest(t, server, http.MethodPost, "/api/agent-bots", `{"name":"UI","group_name":"开发团队","muted":true}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var createBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}

	patched := agentBotRequest(t, server, http.MethodPut, "/api/agent-bots/"+createBody.Bot.ID,
		`{"group_name":"新媒体团队","muted":false}`)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch=%d body=%s", patched.Code, patched.Body.String())
	}
	var patchBody struct {
		Bot agenthub.Bot `json:"bot"`
	}
	if err := json.Unmarshal(patched.Body.Bytes(), &patchBody); err != nil {
		t.Fatal(err)
	}
	if patchBody.Bot.GroupName != "新媒体团队" || patchBody.Bot.Muted {
		t.Fatalf("patched bot = %+v", patchBody.Bot)
	}
	if patchBody.Bot.Name != "UI" {
		t.Fatalf("patch dropped the name: %+v", patchBody.Bot)
	}
}
