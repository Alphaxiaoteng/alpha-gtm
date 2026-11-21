package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/tools"
	"alpha-nexus-egolite/internal/trace"
)

func TestToolAgentPropagatesTraceAndTaskToRegistry(t *testing.T) {
	var out bytes.Buffer
	emitter := observability.NewJSONEmitter(&out, observability.Options{Service: "test"})
	ac := &Context{TaskID: "task-observe", TraceID: trace.ID("trace-agent"), MaxSteps: 2}
	report, err := ToolAgent(context.Background(), ac, ToolAgentInput{
		Steps:       []ToolAgentStep{{Tool: "account.list"}},
		StopOnError: true,
		Runtime:     &tools.Runtime{DataDir: t.TempDir(), Events: emitter},
	})
	if err != nil || report == nil || !report.OK {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("events=%d\n%s", len(lines), out.String())
	}
	var event map[string]any
	if err := json.Unmarshal(lines[1], &event); err != nil {
		t.Fatalf("decode event: %v\n%s", err, out.String())
	}
	if event["trace_id"] != "trace-agent" || event["task_id"] != "task-observe" {
		t.Fatalf("correlation=%v", event)
	}
}

func TestToolAgentDoesNotRetryStudioFileWrites(t *testing.T) {
	var out bytes.Buffer
	emitter := observability.NewJSONEmitter(&out, observability.Options{Service: "test"})
	report, err := ToolAgent(context.Background(), &Context{TaskID: "studio-write", MaxSteps: 2, TraceID: trace.ID("trace-studio-write")}, ToolAgentInput{
		Steps:       []ToolAgentStep{{Tool: "studio.clip.execute", Args: map[string]interface{}{"clip_job_id": 1, "confirm": false}}},
		MaxRetries:  3,
		StopOnError: true,
		Runtime:     &tools.Runtime{DataDir: t.TempDir(), Events: emitter},
	})
	if err != nil || report == nil || report.OK {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("studio write tool calls=%d, want one start/terminal pair: %s", len(lines), out.String())
	}
}
