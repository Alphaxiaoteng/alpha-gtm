package terminal

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agentws"
)

// ChatRequest is a single agent turn (non-interactive CLI).
// WorkDir is forced into the agent-workspace sandbox unless already inside it.
// Thin wrapper: no extra LLM hop — just exec claude/opencode with same flags.
type ChatRequest struct {
	Engine   string // opencode | claude
	Message  string
	WorkDir  string
	DataDir  string // for sandbox root
	Continue bool   // continue last session in workdir
	Timeout  time.Duration
	// SystemAppend is prepended as policy (sandbox + no app source edits).
	SystemAppend string
	// Model: claude alias (sonnet/opus/haiku) or opencode provider/model
	Model string
	// Effort / thinking: low|medium|high|xhigh|max (claude --effort; opencode --variant)
	Effort string
	// Thinking shows thinking blocks for opencode when true
	Thinking bool
	// PermissionMode for claude: default|acceptEdits|plan|auto|bypassPermissions|dontAsk
	PermissionMode string
	// Files: local absolute paths to attach (images etc.) — passed straight to CLI.
	Files []string
	// ExtraAddDirs: additional --add-dir for claude (e.g. works folder).
	ExtraAddDirs []string
}

// ChatResult is assistant text (+ optional raw for debug).
type ChatResult struct {
	OK       bool   `json:"ok"`
	Engine   string `json:"engine"`
	Binary   string `json:"binary"`
	WorkDir  string `json:"work_dir"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Reply    string `json:"reply"`
	Duration string `json:"duration"`
	Error    string `json:"error,omitempty"`
	RawTail  string `json:"raw_tail,omitempty"`
}

// Chat runs opencode run / claude -p and returns plain assistant text.
// This is the chat-box path — no interactive TUI / xterm session required.
func Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	_ = ctx
	start := time.Now()
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	// Claude was already removed from this legacy wrapper: every request was
	// normalized to OpenCode below. Keep that behavior without adding a new
	// Claude execution path, but reject before env lookup, filesystem access,
	// binary discovery, or exec. Only agents.OpenCodeBridge may launch OpenCode.
	if engine != "opencode" {
		engine = "opencode"
	}
	return &ChatResult{
		OK:       false,
		Engine:   engine,
		Duration: time.Since(start).Round(time.Millisecond).String(),
		Error:    ErrOpenCodeUnbrokeredDisabled.Error(),
	}, ErrOpenCodeUnbrokeredDisabled
}

func resolveSandboxWorkDir(dataDir, want string) (string, error) {
	return agentws.ResolveWorkDir(dataDir, want)
}

func sandboxPolicy(wd string) string {
	// Prefer pointing at parent agent-workspace when session subdir
	root := wd
	if base := filepath.Base(filepath.Dir(wd)); base == "sessions" {
		root = filepath.Dir(filepath.Dir(wd))
	}
	return fmt.Sprintf(`You are Alpha-Nexus Agent (CLI wrapped in a chat UI).
STRICT RULES:
1. Writable roots: session dir %s and parent workspace %s
2. Automations scripts: %s/automations/
3. Batch publish materials (videos/captions): %s/works/ — each subfolder is one work
4. Do NOT modify Alpha-Nexus application source (internal/, ui-web/, cmd/).
5. Call tools/MCP for accounts/browser/publish dry-run. Keep answers concise.`, wd, root, root, root)
}
