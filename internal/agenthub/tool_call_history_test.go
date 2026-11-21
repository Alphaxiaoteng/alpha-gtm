package agenthub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func seedToolCall(t *testing.T, service *Service, run *Run, call ToolCall, params string) {
	t.Helper()
	if err := service.store.CreateToolCall(context.Background(), run, &call, params, false); err != nil {
		t.Fatalf("create tool call: %v", err)
	}
	// CreateToolCall does not persist error_code (a finished call writes it via
	// MarkToolCall); set it directly so the read path is what is under test.
	if call.ErrorCode != "" {
		if _, err := service.store.db.Exec(`UPDATE agent_tool_calls SET error_code=? WHERE id=?`, call.ErrorCode, call.ID); err != nil {
			t.Fatalf("set error code: %v", err)
		}
	}
}

func TestConversationToolCallsRedactsAndOrders(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	ctx := context.Background()
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "hello", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}

	earlier := "2026-11-20T10:00:00.000000Z"
	later := "2026-11-20T10:00:02.500000Z"
	seedToolCall(t, service, run, ToolCall{
		ID: "tool_older", ToolName: "account.list", Status: "completed",
		RiskLevel: "L0", ImpactPreview: "读取账号", CreatedAt: earlier, UpdatedAt: later,
	}, `{"workspace":"media","access_token":"secret-value","limit":30}`)
	seedToolCall(t, service, run, ToolCall{
		ID: "tool_newer", ToolName: "radar.capture", Status: "failed",
		RiskLevel: "L2", ImpactPreview: "抓取失败", ErrorCode: "TIKHUB_RATE_LIMIT",
		CreatedAt: later, UpdatedAt: "2026-11-20T10:00:09.000000Z",
	}, `{"query":"fde","nested":{"cookie":"abc"}}`)

	calls, err := service.ConversationToolCalls(ctx, conversation.ID, 50)
	if err != nil {
		t.Fatalf("conversation tool calls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if calls[0].ID != "tool_newer" {
		t.Fatalf("newest first: got %q", calls[0].ID)
	}
	if calls[0].ErrorCode != "TIKHUB_RATE_LIMIT" {
		t.Fatalf("error code lost: %+v", calls[0])
	}

	params, ok := calls[0].Params.(map[string]any)
	if !ok {
		t.Fatalf("params should decode to an object: %#v", calls[0].Params)
	}
	nested, ok := params["nested"].(map[string]any)
	if !ok || nested["cookie"] != "***" {
		t.Fatalf("nested secret not redacted: %#v", params)
	}

	older, ok := calls[1].Params.(map[string]any)
	if !ok {
		t.Fatalf("older params: %#v", calls[1].Params)
	}
	if older["access_token"] != "***" {
		t.Fatalf("token not redacted: %#v", older)
	}
	if calls[1].DurationMS != 2500 {
		t.Fatalf("duration = %d, want 2500", calls[1].DurationMS)
	}
	if calls[0].DurationMS != 6500 {
		t.Fatalf("failed call duration = %d, want 6500", calls[0].DurationMS)
	}
}

func TestConversationToolCallsCapsPayload(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	ctx := context.Background()
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "hello", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	huge := `{"body":"` + strings.Repeat("x", 6000) + `"}`
	seedToolCall(t, service, run, ToolCall{
		ID: "tool_big", ToolName: "content.create", Status: "completed", RiskLevel: "L1",
		CreatedAt: "2026-11-20T10:00:00.000000Z", UpdatedAt: "2026-11-20T10:00:01.000000Z",
	}, huge)

	calls, err := service.ConversationToolCalls(ctx, conversation.ID, 10)
	if err != nil || len(calls) != 1 {
		t.Fatalf("calls = %d err = %v", len(calls), err)
	}
	text, ok := calls[0].Params.(string)
	if !ok {
		t.Fatalf("oversized params should degrade to a string: %T", calls[0].Params)
	}
	if len(text) > toolPayloadLimit+8 || !strings.HasSuffix(text, "…") {
		t.Fatalf("params not truncated: len=%d", len(text))
	}
}

func TestConversationToolCallsEmptyConversation(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	calls, err := service.ConversationToolCalls(context.Background(), "conv_missing", 50)
	if err != nil {
		t.Fatalf("missing conversation must not error: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %d, want 0", len(calls))
	}
}

func TestSanitizeToolPayloadRedactsEmbeddedJSONString(t *testing.T) {
	// Providers often keep the tool result as a JSON string inside the result
	// object; the token there must be masked like any other.
	raw := `{"body":"{\"access_token\":\"sk-live-123\",\"note\":\"ok\"}"}`
	got, ok := sanitizeToolPayload(raw).(map[string]any)
	if !ok {
		t.Fatalf("payload should decode to an object: %#v", sanitizeToolPayload(raw))
	}
	body, ok := got["body"].(string)
	if !ok {
		t.Fatalf("body should stay a string: %#v", got["body"])
	}
	if strings.Contains(body, "sk-live-123") {
		t.Fatalf("token in embedded JSON leaked: %s", body)
	}
	if !strings.Contains(body, "***") {
		t.Fatalf("embedded token should be masked: %s", body)
	}
}

func TestSanitizeToolPayloadMasksMalformedSecrets(t *testing.T) {
	raw := `{"api_key":"sk-broken","body":"` + strings.Repeat("x", 20)
	got, ok := sanitizeToolPayload(raw).(string)
	if !ok {
		t.Fatalf("malformed payload should degrade to a string: %T", sanitizeToolPayload(raw))
	}
	if strings.Contains(got, "sk-broken") {
		t.Fatalf("malformed payload leaked a secret: %s", got)
	}
}

func TestSanitizeToolPayloadMasksDeepAndVariantKeys(t *testing.T) {
	raw := `{"a":{"b":{"c":{"d":{"e":{"f":{"g":{"passwd":"hunter2","credential":"abc"}}}}}}}}`
	encoded, err := json.Marshal(sanitizeToolPayload(raw))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "hunter2") || strings.Contains(text, "abc") {
		t.Fatalf("deep secret leaked: %s", text)
	}
}

func TestTruncateToolTextKeepsUTF8(t *testing.T) {
	// "中" is 3 bytes, so a byte cut at 2000 lands mid-rune.
	text := strings.Repeat("中", 900)
	got := truncateToolText(text)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated text is not valid UTF-8: %q", got[len(got)-6:])
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated text should end with an ellipsis")
	}
}

func TestConversationToolCallsSortsByInstant(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	ctx := context.Background()
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "hello", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	// RFC3339Nano trims trailing zeros: ".5Z" sorts after ".5000001Z" as text
	// but is earlier in time.
	seedToolCall(t, service, run, ToolCall{
		ID: "tool_late", ToolName: "account.list", Status: "completed", RiskLevel: "L0",
		CreatedAt: "2026-11-20T10:00:00.5000001Z", UpdatedAt: "2026-11-20T10:00:01Z",
	}, `{}`)
	seedToolCall(t, service, run, ToolCall{
		ID: "tool_early", ToolName: "account.list", Status: "completed", RiskLevel: "L0",
		CreatedAt: "2026-11-20T10:00:00.5Z", UpdatedAt: "2026-11-20T10:00:01Z",
	}, `{}`)

	calls, err := service.ConversationToolCalls(ctx, conversation.ID, 10)
	if err != nil || len(calls) != 2 {
		t.Fatalf("calls=%d err=%v", len(calls), err)
	}
	if calls[0].ID != "tool_late" {
		t.Fatalf("newest first: got %q", calls[0].ID)
	}
}

func TestSanitizeToolPayloadKeepsMalformed(t *testing.T) {
	raw := `{"unterminated":`
	got := sanitizeToolPayload(raw)
	if got != raw {
		t.Fatalf("malformed payload should be visible: %#v", got)
	}
	if sanitizeToolPayload("  ") != nil {
		t.Fatal("blank payload should be nil")
	}
	_ = json.Valid([]byte("null"))
	_ = time.Now
}
