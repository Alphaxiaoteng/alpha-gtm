package agents

import (
	"context"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/llm"
)

type FastAgentStreamHandler func(eventType string, data any) error

type FastAgentRequest struct {
	SessionID string
	WorkDir   string
	Message   string
}

const MinimalSystemPrompt = `You are a high-efficiency AI coding and task assistant running directly in the user's workspace.
Answer concisely and accurately.

You have access to 4 minimalist primitive tools:
1. read_file(path, start_line?, end_line?): Read file content with optional line slicing.
2. write_file(path, content): Create or overwrite a file.
3. replace_file(path, old_text, new_text): Precision text replacement in a file.
4. run_command(command): Run a shell command in workspace directory.

Rules:
- Operate with minimal steps.
- Prioritize high accuracy over verbose explanation.
`

// RunFastAgent executes the agent loop with streaming feedback.
func RunFastAgent(ctx context.Context, req FastAgentRequest, onEvent FastAgentStreamHandler) (string, error) {
	if strings.TrimSpace(req.Message) == "" {
		return "", fmt.Errorf("message is required")
	}

	cfg := llm.ResolveDoubaoConfig()
	if cfg.APIKey == "" {
		return "", fmt.Errorf("AGENT_API_NOT_CONFIGURED")
	}

	// 1. Record & Load History via Session Ledger
	DefaultLedger.Append(req.SessionID, "user", req.Message)
	history := DefaultLedger.GetHistory(req.SessionID)

	var messages []map[string]string
	messages = append(messages, map[string]string{"role": "system", "content": MinimalSystemPrompt})

	for _, m := range history {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}

	// 2. Smart Reasoning Auto Evaluation
	enableReasoning := ShouldEnableReasoning(req.Message, "auto")
	opts := llm.StreamOptions{EnableReasoning: enableReasoning}

	// 3. LLM Stream Call
	reply, err := llm.CompleteStreamWithOptions(ctx, cfg, messages, opts, func(token string) error {
		if onEvent != nil {
			return onEvent("token", map[string]string{"token": token})
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	// 3. Record Assistant Reply
	DefaultLedger.Append(req.SessionID, "assistant", reply)

	if onEvent != nil {
		_ = onEvent("done", map[string]any{"ok": true, "reply": reply})
	}

	return reply, nil
}
