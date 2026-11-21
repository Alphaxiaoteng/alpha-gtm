package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/trace"
)

// newAgentGateway 让回归测试显式使用离线回退，避免测试误发付费请求。
var newAgentGateway = llm.NewDoubaoGateway

func completeWithDoubao(ctx context.Context, system, user string) (string, error) {
	gateway := newAgentGateway(6 * time.Second)
	if gateway == nil {
		return "", fmt.Errorf("DOUBAO_API_NOT_CONFIGURED")
	}
	return gateway.Complete(ctx, system, user)
}

// Tool whitelist (AgentPush §7).
type Tool string

const (
	ToolLLMGenerate         Tool = "llm.generate"
	ToolFilesRead           Tool = "files.read"
	ToolFilesWrite          Tool = "files.write"
	ToolMediaTranscode      Tool = "media.transcode"
	ToolBrowserAttach       Tool = "browser.attach"
	ToolBrowserSnapshot     Tool = "browser.snapshot"
	ToolBrowserAction       Tool = "browser.action"
	ToolBrowserUpload       Tool = "browser.upload"
	ToolAPIRequest          Tool = "api.request"
	ToolTaskUpdate          Tool = "task.update"
	ToolTaskRequireApproval Tool = "task.require_approval"
)

type ApprovalMode string

const (
	ApprovalAlwaysReview      ApprovalMode = "always_review"
	ApprovalTrustedAutoSubmit ApprovalMode = "trusted_auto_submit"
)

// Context bounds an agent run.
type Context struct {
	TaskID       string
	AccountID    string
	Platform     string
	ApprovalMode ApprovalMode
	AllowedTools map[Tool]bool
	MaxSteps     int
	TimeoutMs    int
	TraceID      trace.ID
	Steps        int
}

func (c *Context) Allow(t Tool) bool {
	if c.AllowedTools == nil {
		return true
	}
	return c.AllowedTools[t]
}

func (c *Context) Step(name string) error {
	c.Steps++
	if c.MaxSteps > 0 && c.Steps > c.MaxSteps {
		return fmt.Errorf("max steps exceeded (%d)", c.MaxSteps)
	}
	trace.Record(c.TraceID, "agent.step", map[string]string{"name": name, "task": c.TaskID})
	return nil
}

// Result is generic agent output.
type Result struct {
	Agent   string            `json:"agent"`
	OK      bool              `json:"ok"`
	Output  string            `json:"output"`
	Data    map[string]string `json:"data,omitempty"`
	TraceID string            `json:"trace_id"`
}

// ContentAgent: titles, captions, tags, translation (AP-503).
func ContentAgent(ctx context.Context, ac *Context, draft, lang string) (*Result, error) {
	if err := ac.Step("content.generate"); err != nil {
		return nil, err
	}
	if !ac.Allow(ToolLLMGenerate) {
		return &Result{Agent: "ContentAgent", OK: false, Output: "llm.generate not allowed", TraceID: string(ac.TraceID)}, nil
	}
	sys := "You are ContentAgent for social posts. Produce title, caption, tags, CTA. Language: " + lang
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := completeWithDoubao(cctx, sys, draft)
	if err != nil || strings.TrimSpace(out) == "" {
		out = draft
	}
	return &Result{
		Agent:   "ContentAgent",
		OK:      true,
		Output:  out,
		Data:    map[string]string{"lang": lang},
		TraceID: string(ac.TraceID),
	}, nil
}

// AdaptAgent: platform payload compression (AP-504).
func AdaptAgent(ctx context.Context, ac *Context, caption, platform string) (*Result, error) {
	if err := ac.Step("adapt.platform"); err != nil {
		return nil, err
	}
	limit := 2200
	switch platform {
	case "x", "twitter":
		limit = 280
	case "xhs":
		limit = 1000
	case "douyin", "kuaishou", "wechat_video":
		limit = 500
	}
	body := caption
	if len([]rune(body)) > limit {
		r := []rune(body)
		body = string(r[:limit-1]) + "…"
	}
	sys := fmt.Sprintf("Adapt caption for platform %s max %d chars. Keep hashtags.", platform, limit)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := completeWithDoubao(cctx, sys, body)
	if err != nil || out == "" || strings.HasPrefix(out, "[local-template]") {
		out = body
	}
	return &Result{
		Agent:   "AdaptAgent",
		OK:      true,
		Output:  out,
		Data:    map[string]string{"platform": platform, "max_chars": fmt.Sprintf("%d", limit)},
		TraceID: string(ac.TraceID),
	}, nil
}

// PublishAgent is deliberately non-executing. It has no content, account, asset,
// or idempotency-key inputs, so it must never fabricate a publish job state.
// Use publish.run (via ToolAgent) or the publish CLI for an executable request.
func PublishAgent(ctx context.Context, ac *Context, summary string) (*Result, error) {
	if err := ac.Step("publish.plan"); err != nil {
		return nil, err
	}
	return &Result{
		Agent:  "PublishAgent",
		OK:     false,
		Output: "not executed: PublishAgent only received a summary; use publish.run or alpha-nexus publish with content, target, and idempotency key",
		Data: map[string]string{
			"status":     "not_executed",
			"error_code": "PUBLISH_AGENT_REQUIRES_EXECUTABLE_JOB",
			"next":       "publish.run",
			"summary":    summary,
		},
		TraceID: string(ac.TraceID),
	}, nil
}

// MonitorAgent AP-506.
func MonitorAgent(ctx context.Context, ac *Context, loginState, lastError string) (*Result, error) {
	if err := ac.Step("monitor.check"); err != nil {
		return nil, err
	}
	alerts := []string{}
	if loginState == "" || loginState == "unknown" || loginState == "logged_out" {
		alerts = append(alerts, "LOGIN_REQUIRED")
	}
	if lastError != "" {
		alerts = append(alerts, "LAST_ERROR:"+lastError)
	}
	ok := len(alerts) == 0
	return &Result{
		Agent:   "MonitorAgent",
		OK:      ok,
		Output:  strings.Join(alerts, "; "),
		Data:    map[string]string{"checked_at": time.Now().UTC().Format(time.RFC3339)},
		TraceID: string(ac.TraceID),
	}, nil
}

// AnalyticsAgent AP-507.
func AnalyticsAgent(ctx context.Context, ac *Context, metricsJSON string) (*Result, error) {
	if err := ac.Step("analytics.insight"); err != nil {
		return nil, err
	}
	sys := "Summarize social metrics and give 3 optimization tips."
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := completeWithDoubao(cctx, sys, metricsJSON)
	if err != nil || strings.TrimSpace(out) == "" {
		out = metricsJSON
	}
	return &Result{Agent: "AnalyticsAgent", OK: true, Output: out, TraceID: string(ac.TraceID)}, nil
}

func DefaultTools() map[Tool]bool {
	return map[Tool]bool{
		ToolLLMGenerate: true, ToolFilesRead: true, ToolTaskUpdate: true,
		ToolTaskRequireApproval: true, ToolBrowserAttach: true, ToolBrowserSnapshot: true,
	}
}
