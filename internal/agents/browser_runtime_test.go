//go:build legacy_chromium

package agents

import (
	"context"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/trace"
)

func TestBrowserAgentNoCDP(t *testing.T) {
	ac := &Context{MaxSteps: 2, AllowedTools: DefaultTools(), TraceID: trace.New(), TaskID: "t"}
	res, err := BrowserAgent(context.Background(), ac, &BrowserSession{CDPURL: ""}, "open https://example.com", "")
	if err == nil && res != nil && res.OK {
		t.Fatal("expected failure without CDP")
	}
}

func TestPlanNextFallbackNavigate(t *testing.T) {
	// 明确禁用远程模型，验证 URL 的离线接管路径。
	previous := newBrowserPlannerGateway
	newBrowserPlannerGateway = func(time.Duration) *llm.Gateway { return nil }
	t.Cleanup(func() { newBrowserPlannerGateway = previous })

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	act, _, _ := planNext(ctx, "https://example.com", &PageSnapshot{URL: "about:blank", Title: "", Text: ""}, 1)
	if act.Op != "navigate" && act.Op != "takeover" {
		t.Fatalf("unexpected op %s", act.Op)
	}
}
