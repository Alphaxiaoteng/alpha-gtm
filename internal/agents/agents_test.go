package agents

import (
	"context"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/trace"
)

func disableAgentGateway(t *testing.T) {
	t.Helper()
	previous := newAgentGateway
	newAgentGateway = func(time.Duration) *llm.Gateway { return nil }
	t.Cleanup(func() { newAgentGateway = previous })
}

func TestAdaptAndMonitor(t *testing.T) {
	disableAgentGateway(t)
	ac := &Context{TaskID: "t1", TraceID: trace.New(), MaxSteps: 10, AllowedTools: DefaultTools()}
	r, err := AdaptAgent(context.Background(), ac, "hello world from agentpush", "x")
	if err != nil || !r.OK || r.Output == "" {
		t.Fatalf("%+v %v", r, err)
	}
	r2, err := MonitorAgent(context.Background(), ac, "logged_out", "")
	if err != nil || r2.OK {
		t.Fatalf("expected login alert %+v %v", r2, err)
	}
}

func TestPublishAgentNeverFabricatesJobState(t *testing.T) {
	for _, mode := range []ApprovalMode{ApprovalAlwaysReview, ApprovalTrustedAutoSubmit} {
		t.Run(string(mode), func(t *testing.T) {
			ac := &Context{
				TaskID:       "publish-plan",
				ApprovalMode: mode,
				MaxSteps:     2,
				AllowedTools: DefaultTools(),
				TraceID:      trace.New(),
			}
			result, err := PublishAgent(context.Background(), ac, "publish the launch post")
			if err != nil {
				t.Fatal(err)
			}
			if result.OK || result.Data["status"] != "not_executed" || result.Data["error_code"] != "PUBLISH_AGENT_REQUIRES_EXECUTABLE_JOB" {
				t.Fatalf("result=%+v", result)
			}
			for _, forbidden := range []string{"queued", "awaiting_review", "trusted_auto_submit"} {
				if strings.Contains(result.Output, forbidden) {
					t.Fatalf("output must not claim %q: %q", forbidden, result.Output)
				}
			}
		})
	}
}
