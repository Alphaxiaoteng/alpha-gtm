package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/tools"
	"alpha-nexus-egolite/internal/trace"
)

// SchemeID identifies one pluggable agent integration path.
type SchemeID string

const (
	SchemeNative    SchemeID = "native"
	SchemeLLMTools  SchemeID = "llm_tools"
	SchemeOpenCode  SchemeID = "opencode"
	SchemeMCPExport SchemeID = "mcp_export"
	SchemeBrowser   SchemeID = "browser"
)

// SchemeInfo is catalog metadata for UI buttons / CLI list.
type SchemeInfo struct {
	ID           SchemeID `json:"id"`
	Name         string   `json:"name"`
	Tagline      string   `json:"tagline"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	Requires     []string `json:"requires"`
	Weight       string   `json:"weight"` // lightweight | medium | heavy
	Recommended  bool     `json:"recommended,omitempty"`
}

// SchemeRunRequest is the common input for all scheme runners.
type SchemeRunRequest struct {
	Scheme    SchemeID       `json:"scheme"`
	Goal      string         `json:"goal"`
	DryRun    bool           `json:"dry_run"` // default true for UI smoke tests
	AccountID int64          `json:"account_id,omitempty"`
	CDPURL    string         `json:"cdp_url,omitempty"`
	MaxSteps  int            `json:"max_steps,omitempty"`
	Model     string         `json:"model,omitempty"`
	OpenCode  string         `json:"opencode_bin,omitempty"` // path override
	DataDir   string         `json:"data_dir,omitempty"`
	Runtime   *tools.Runtime `json:"-"`
	// RestrictMediaImport limits media.import to the canonical paths supplied by
	// the caller. Chat tool mode uses it for files uploaded to that session.
	RestrictMediaImport     bool     `json:"-"`
	AllowedMediaImportPaths []string `json:"-"`
}

// SchemeRunResult is a stable envelope for CLI / HTTP / UI.
type SchemeRunResult struct {
	OK       bool                     `json:"ok"`
	Scheme   SchemeID                 `json:"scheme"`
	Goal     string                   `json:"goal"`
	DryRun   bool                     `json:"dry_run"`
	Message  string                   `json:"message"`
	TraceID  string                   `json:"trace_id"`
	Duration string                   `json:"duration"`
	Output   string                   `json:"output,omitempty"`
	Error    string                   `json:"error,omitempty"`
	Steps    []map[string]interface{} `json:"steps,omitempty"`
	Data     map[string]interface{}   `json:"data,omitempty"`
}

// ListSchemes returns the five integration paths as UI sub-buttons.
func ListSchemes() []SchemeInfo {
	return []SchemeInfo{
		{
			ID:      SchemeNative,
			Name:    "Native ToolAgent",
			Tagline: "确定性工具链 · 零 LLM 依赖",
			Description: "直接跑 tools.Registry 多步计划（查账号/Ego Space/TikHub 状态）。" +
				"最稳、最快、可预测；适合值班脚本与 Agent 编排底层。",
			Capabilities: []string{"查 SQLite 数据", "工具白名单执行", "重试 / stop-on-error", "NDJSON 友好"},
			Requires:     []string{"sqlite"},
			Weight:       "lightweight",
			Recommended:  true,
		},
		{
			ID:      SchemeLLMTools,
			Name:    "LLM + Tools Loop",
			Tagline: "模型规划 · Registry 执行",
			Description: "固定豆包 API 读取安全工具 schema，产出 tool_calls，" +
				"再由 ToolAgent 执行本地工作流；发布与外部网络仍走各自的明确确认入口。",
			Capabilities: []string{"自然语言目标", "动态选安全工具", "本地工作流", "无有效计划不执行"},
			Requires:     []string{"llm_gateway", "tools.registry"},
			Weight:       "medium",
		},
		{
			ID:      SchemeOpenCode,
			Name:    "OpenCode CLI Bridge",
			Tagline: "轻量外挂 · 160k★ 编码 Agent",
			Description: "subprocess 调用本机 opencode run（--format json）。" +
				"不把重型 Agent 嵌进 Go 进程；适合复杂推理，再通过 MCP/CLI 回控 Alpha-Nexus。",
			Capabilities: []string{"opencode run", "MCP 生态", "多模型 provider", "会话继续"},
			Requires:     []string{"opencode_binary"},
			Weight:       "lightweight",
		},
		{
			ID:      SchemeMCPExport,
			Name:    "MCP Tool Export",
			Tagline: "把本应用能力导出给任意 Agent",
			Description: "将 tools.Registry 导出为 MCP tools/list 风格 schema + opencode mcp 配置片段。" +
				"Claude / OpenCode / Cursor 等均可挂接，不绑定单一框架。",
			Capabilities: []string{"tools/list schema", "MCP config snippet", "Agent 工具接入"},
			Requires:     []string{"tools.registry"},
			Weight:       "lightweight",
		},
	}
}

// RunScheme dispatches to one of the four non-browser runners.
func RunScheme(ctx context.Context, req SchemeRunRequest) (*SchemeRunResult, error) {
	start := time.Now()
	if req.Scheme == "" {
		return nil, fmt.Errorf("scheme is required")
	}
	if strings.TrimSpace(req.Goal) == "" {
		req.Goal = defaultGoal(req.Scheme)
	}
	if req.MaxSteps <= 0 {
		req.MaxSteps = 6
	}
	if req.Runtime == nil {
		req.Runtime = &tools.Runtime{DataDir: req.DataDir}
	} else if req.DataDir != "" && req.Runtime.DataDir == "" {
		req.Runtime.DataDir = req.DataDir
	}

	tid := string(trace.New())
	var (
		res *SchemeRunResult
		err error
	)
	switch req.Scheme {
	case SchemeNative:
		res, err = runSchemeNative(ctx, req, tid)
	case SchemeLLMTools:
		res, err = runSchemeLLMTools(ctx, req, tid)
	case SchemeOpenCode:
		res, err = runSchemeOpenCode(ctx, req, tid)
	case SchemeMCPExport:
		res, err = runSchemeMCPExport(ctx, req, tid)
	default:
		return nil, fmt.Errorf("unknown scheme %q", req.Scheme)
	}
	if res == nil {
		res = &SchemeRunResult{OK: false, Scheme: req.Scheme, Goal: req.Goal, TraceID: tid, Error: "nil result"}
		if err != nil {
			res.Error = err.Error()
			res.Message = err.Error()
		}
	}
	res.Scheme = req.Scheme
	res.Goal = req.Goal
	res.DryRun = req.DryRun
	res.TraceID = tid
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res, err
}

func defaultGoal(id SchemeID) string {
	switch id {
	case SchemeNative:
		return "probe local data: list accounts + Ego Space status"
	case SchemeLLMTools:
		return "summarize how many accounts and Ego Spaces exist"
	case SchemeOpenCode:
		return "In one short paragraph, describe how an agent can inspect Alpha-Nexus EgoLite accounts without opening a browser."
	case SchemeMCPExport:
		return "export Alpha-Nexus tools as MCP schema"
	case SchemeBrowser:
		return "https://example.com"
	default:
		return "ping"
	}
}

// ---- runners ----

func runSchemeNative(ctx context.Context, req SchemeRunRequest, tid string) (*SchemeRunResult, error) {
	steps := []ToolAgentStep{
		{Tool: "account.list", Args: map[string]interface{}{}},
		{Tool: "ego.space.list", Args: map[string]interface{}{}},
	}
	if req.DryRun {
		// dry: only schema-level plan, still execute read-only tools (safe)
		// user asked to test — read tools are fine; label as smoke
	}
	in := ToolAgentInput{
		Goal:        req.Goal,
		Steps:       steps,
		MaxRetries:  1,
		StopOnError: false,
		Runtime:     req.Runtime,
	}
	ac := &Context{TaskID: "scheme-native", MaxSteps: 20, TraceID: trace.ID(tid)}
	rep, err := ToolAgent(ctx, ac, in)
	out := &SchemeRunResult{
		OK:      rep != nil && rep.OK,
		Message: "native ToolAgent executed read probes",
		Data: map[string]interface{}{
			"engine": "tools.Registry + ToolAgent",
			"mode":   "deterministic",
		},
	}
	if rep != nil {
		out.Steps = rep.Steps
		out.Message = rep.Message
		out.OK = rep.OK
		if b, e := jsonMarshalPretty(rep); e == nil {
			out.Output = string(b)
		}
	}
	if err != nil {
		out.Error = err.Error()
		out.OK = false
	}
	return out, err
}

func runSchemeLLMTools(ctx context.Context, req SchemeRunRequest, tid string) (*SchemeRunResult, error) {
	return LLMToolsAgent(ctx, req, tid)
}

func runSchemeOpenCode(ctx context.Context, req SchemeRunRequest, tid string) (*SchemeRunResult, error) {
	return OpenCodeBridge(ctx, req, tid)
}

func runSchemeMCPExport(ctx context.Context, req SchemeRunRequest, tid string) (*SchemeRunResult, error) {
	_ = ctx
	export, err := BuildMCPExport(req.DataDir)
	if err != nil {
		return &SchemeRunResult{OK: false, Message: "mcp export failed", Error: err.Error(), TraceID: tid}, err
	}
	msg := fmt.Sprintf("exported %d tools as MCP-compatible schema", export.ToolCount)
	if req.DryRun {
		msg += " (dry_run — no external MCP server started)"
	}
	return &SchemeRunResult{
		OK:      true,
		Message: msg,
		Data: map[string]interface{}{
			"tool_count":     export.ToolCount,
			"categories":     export.Categories,
			"mcp_config":     export.MCPConfigSnippet,
			"opencode_hint":  export.OpenCodeHint,
			"schema_preview": export.SchemaPreview,
			"schema_path":    export.SchemaPath,
		},
		Output: export.SchemaPreview,
	}, nil
}

func runSchemeBrowser(ctx context.Context, req SchemeRunRequest, tid string) (*SchemeRunResult, error) {
	cdp := strings.TrimSpace(req.CDPURL)
	if cdp == "" && req.AccountID <= 0 {
		// dry-friendly: no CDP → return readiness check, not hard fail for UI test
		if req.DryRun {
			return &SchemeRunResult{
				OK:      true,
				Message: "browser scheme ready (dry_run): provide an account_id with a running browser",
				Data: map[string]interface{}{
					"status": "ready_no_cdp",
					"hint":   "alpha-nexus-egolite browser start --account-id N && agent scheme run --id browser --account-id N --goal ...",
				},
			}, nil
		}
		return &SchemeRunResult{
			OK: false, Message: "account_id or cdp_url required",
			Error: "BROWSER_CDP_REQUIRED",
		}, fmt.Errorf("BROWSER_CDP_REQUIRED")
	}

	if cdp == "" {
		if req.DryRun {
			return &SchemeRunResult{
				OK:      true,
				Message: "no CDP endpoint yet; start browser for account first",
				Data:    map[string]interface{}{"status": "browser_not_running", "account_id": req.AccountID},
			}, nil
		}
		return &SchemeRunResult{OK: false, Error: "BROWSER_NOT_RUNNING", Message: "browser not running"}, fmt.Errorf("BROWSER_NOT_RUNNING")
	}

	if req.DryRun {
		// attach snapshot only (one read)
		sess := &BrowserSession{CDPURL: cdp}
		snap, err := sess.Snapshot(ctx)
		if err != nil {
			return &SchemeRunResult{
				OK: false, Message: "browser snapshot failed", Error: "BROWSER_SNAPSHOT_FAILED",
				Data: map[string]interface{}{"status": "retryable_failed"},
			}, fmt.Errorf("BROWSER_SNAPSHOT_FAILED")
		}
		return &SchemeRunResult{
			OK:      true,
			Message: "browser dry_run: snapshot ok, skipped action loop",
			Data: map[string]interface{}{
				"url":   snap.URL,
				"title": snap.Title,
				"text":  truncate(snap.Text, 400),
			},
		}, nil
	}

	shotDir := ""
	if req.DataDir != "" {
		shotDir = filepath.Join(req.DataDir, "screenshots")
	}
	ac := &Context{
		TaskID: "scheme-browser", MaxSteps: req.MaxSteps,
		AllowedTools: DefaultTools(), TraceID: trace.ID(tid),
	}
	ac.AllowedTools[ToolBrowserAction] = true
	ac.AllowedTools[ToolBrowserUpload] = true
	r, err := BrowserAgent(ctx, ac, &BrowserSession{CDPURL: cdp}, req.Goal, shotDir)
	out := &SchemeRunResult{OK: r != nil && r.OK, Message: "BrowserAgent finished"}
	if r != nil {
		out.OK = r.OK
		out.Output = r.Output
		out.Data = map[string]interface{}{}
		for k, v := range r.Data {
			out.Data[k] = v
		}
		out.Data["agent"] = r.Agent
		if !r.OK && r.Data != nil {
			out.Error = r.Data["error"]
		}
	}
	if err != nil {
		// Browser transport errors may embed the private loopback endpoint.
		out.Error = "BROWSER_AGENT_FAILED"
		out.Message = "BrowserAgent failed"
		out.Output = ""
		out.Data = map[string]interface{}{"agent": "BrowserAgent", "status": "retryable_failed"}
		return out, fmt.Errorf("BROWSER_AGENT_FAILED")
	}
	return out, err
}

func jsonMarshalPretty(v interface{}) ([]byte, error) {
	return marshalIndent(v)
}

// ResolveOpenCodeBinary finds opencode on PATH or common install paths.
func ResolveOpenCodeBinary(override string) string {
	if override != "" {
		if st, err := os.Stat(override); err == nil && !st.IsDir() {
			return override
		}
	}
	if p, err := exec.LookPath("opencode"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local/bin/opencode"),
		filepath.Join(home, ".opencode/bin/opencode"),
		"/usr/local/bin/opencode",
		"/opt/homebrew/bin/opencode",
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
