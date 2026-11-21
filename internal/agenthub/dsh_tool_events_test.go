package agenthub

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

// The dsh runtime executes tools with its own harness and reports them as
// tool/call + tool/result session events. The runner must forward them as
// tool.observed events so the run loop can persist the conversation timeline;
// dropping them would leave every dsh conversation's tool history empty.
func TestDshRunnerForwardsToolEvents(t *testing.T) {
	var mu sync.Mutex
	var events []ProviderEvent
	runner := dshTestRunner("", new(int), &mu)
	defer runner.CloseProcesses()

	fake := strings.Replace(
		dshPersistentFake,
		"process.stdout.write(JSON.stringify({jsonrpc: '2.0', method: 'session.status', params: {sessionId, status: 'idle'}}) + '\\n');",
		`process.stdout.write(JSON.stringify({jsonrpc: '2.0', method: 'session.event', params: {sessionId, event: {type: 'tool/call', data: {callId: 'call_ext_1', name: 'bash', arguments: '{"command":"echo hi"}'}}}}) + '\n');
		 process.stdout.write(JSON.stringify({jsonrpc: '2.0', method: 'session.event', params: {sessionId, event: {type: 'tool/result', data: {message: {toolCallId: 'call_ext_1', name: 'bash', isError: false, content: [{type: 'text', text: 'hi'}]}}}}}) + '\n');
		 process.stdout.write(JSON.stringify({jsonrpc: '2.0', method: 'session.status', params: {sessionId, status: 'idle'}}) + '\n');`,
		1,
	)
	runner.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "node", "-e", fake)
	}

	_, err := runner.Run(context.Background(), RunRequest{
		WorkDir: t.TempDir(), Message: "运行 bash", RunID: "run-tool-events", ModelID: "deepseek-v4.1-flash",
	}, func(event ProviderEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}

	observed := 0
	var callPayload, resultPayload map[string]any
	for _, event := range events {
		if event.Type != "tool.observed" {
			continue
		}
		observed++
		if event.Payload["status"] == "running" {
			callPayload = event.Payload
		} else {
			resultPayload = event.Payload
		}
	}
	if observed != 2 {
		t.Fatalf("tool.observed events %d, want 2", observed)
	}
	if callPayload["tool_name"] != "bash" || callPayload["call_id"] != "call_ext_1" {
		t.Fatalf("call payload missing name/call_id: %v", callPayload)
	}
	if callPayload["params_json"] == "" {
		t.Fatal("call payload must carry params_json for the timeline")
	}
	if resultPayload["status"] != "completed" {
		t.Fatalf("result status %v, want completed", resultPayload["status"])
	}
	if result, ok := resultPayload["result_json"].(string); !ok || !strings.Contains(result, "hi") {
		t.Fatalf("result payload must carry the tool output: %v", resultPayload["result_json"])
	}
}

// The run-loop read model: recordObservedToolCall persists one provider-executed
// tool call into agent_tool_calls with a stable, replay-safe id.
func TestServiceRecordsObservedToolCalls(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	registry, err := DefaultRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(context.Background(), ProviderDsh, "test", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(context.Background(), conversation.ID, ProviderDsh, "", "test", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"call_id": "call_ext_9", "tool_name": "bash", "params_json": `{"command":"echo hi"}`,
		"status": "completed", "result_json": `{"text":"hi"}`, "created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := service.recordObservedToolCall(context.Background(), run, payload); err != nil {
		t.Fatal(err)
	}

	rows, err := store.ListConversationToolCalls(context.Background(), conversation.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("tool call rows %d, want 1", len(rows))
	}
	if rows[0].ToolName != "bash" || rows[0].Status != "completed" {
		t.Fatalf("row mismatch: %+v", rows[0])
	}
}

// tool/result events arrive without a tool name; they must settle the
// existing row (status + result) instead of being rejected.
func TestServiceRecordsObservedToolCallResultWithoutName(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	registry, err := DefaultRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(context.Background(), ProviderDsh, "test", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(context.Background(), conversation.ID, ProviderDsh, "", "test", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Format(time.RFC3339Nano)
	if err := service.recordObservedToolCall(context.Background(), run, map[string]any{
		"call_id": "call_x", "tool_name": "bash", "params_json": `{"command":"echo hi"}`,
		"status": "running", "created_at": created,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.recordObservedToolCall(context.Background(), run, map[string]any{
		"call_id": "call_x", "status": "completed", "result_json": `{"text":"hi"}`, "created_at": created,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListConversationToolCalls(context.Background(), conversation.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows %d, want 1 (result must upsert, not insert)", len(rows))
	}
	if rows[0].ToolName != "bash" || rows[0].Status != "completed" {
		t.Fatalf("settled row mismatch: %+v", rows[0])
	}
}

// A payload without a call id or tool name must be rejected, not silently
// written with an empty identity.
func TestServiceRecordsObservedToolCallRejectsIncomplete(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	registry, err := DefaultRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(context.Background(), ProviderDsh, "test", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(context.Background(), conversation.ID, ProviderDsh, "", "test", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.recordObservedToolCall(context.Background(), run, map[string]any{"tool_name": "bash"}); err == nil {
		t.Fatal("missing call_id must be rejected")
	}
	if err := service.recordObservedToolCall(context.Background(), run, map[string]any{"call_id": "x", "status": "running"}); err == nil {
		t.Fatal("missing tool_name on the initial observation must be rejected")
	}
}
