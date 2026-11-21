package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agentcfg"
	"alpha-nexus-egolite/internal/llm"
)

type arkResponsesCompleter interface {
	Complete(context.Context, string) (string, error)
}

type arkResponsesFactory func(string, time.Duration) (arkResponsesCompleter, error)

// newArkResponsesClient is replaceable only by package tests. Production uses
// the fixed Ark Responses endpoint and the existing macOS Keychain credential.
var newArkResponsesClient arkResponsesFactory = func(model string, timeout time.Duration) (arkResponsesCompleter, error) {
	return llm.NewArkResponsesClient(model, timeout)
}

// ChatTurn is one multi-turn message sent as part of the single Responses input.
type ChatTurn struct {
	Role    string `json:"role"` // user | assistant | system
	Content string `json:"content"`
}

// ChatWithHistory calls Volcano Ark Responses directly with a model that has
// already passed the real probe and is enabled in agentcfg. There is no model
// fallback and no config.json/env credential lookup.
func ChatWithHistory(ctx context.Context, req ChatRequest, history []ChatTurn) (*ChatResult, error) {
	start := time.Now()
	engine := "doubao_api"
	binary := "agent-api"
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return &ChatResult{OK: false, Engine: engine, Error: "message required"}, fmt.Errorf("message required")
	}

	model, err := verifiedArkModel(req.DataDir, req.Model)
	if err != nil {
		return &ChatResult{
			OK: false, Engine: engine, Binary: binary, Error: "AGENT_MODEL_NOT_VERIFIED",
			Reply:    "所选豆包模型尚未测试成功，未启用。请先在设置中测试成功后重试。",
			Duration: time.Since(start).Round(time.Millisecond).String(),
		}, fmt.Errorf("AGENT_MODEL_NOT_VERIFIED")
	}

	dataDir := req.DataDir
	wd, err := resolveSandboxWorkDir(dataDir, req.WorkDir)
	if err != nil {
		return &ChatResult{OK: false, Engine: engine, Model: model, Error: err.Error()}, err
	}
	policy := strings.TrimSpace(req.SystemAppend)
	if policy == "" {
		policy = sandboxPolicy(wd)
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	client, err := newArkResponsesClient(model, timeout)
	if err != nil {
		reply, code := arkResponsesFailure(err)
		return &ChatResult{
			OK: false, Engine: engine, Binary: binary, Model: model, WorkDir: wd,
			Error: code, Reply: reply,
			Duration: time.Since(start).Round(time.Millisecond).String(),
		}, fmt.Errorf("%s", code)
	}

	input := buildArkResponsesInput(policy, history, msg, req.Files)
	reply, err := client.Complete(ctx, input)
	if err != nil {
		message, code := arkResponsesFailure(err)
		return &ChatResult{
			OK: false, Engine: engine, Binary: binary, Model: model, WorkDir: wd,
			Error: code, Reply: message,
			Duration: time.Since(start).Round(time.Millisecond).String(),
		}, fmt.Errorf("%s", code)
	}
	reply = strings.TrimSpace(reply)
	if message, code, known := presentGatewayFailure(reply); known {
		return &ChatResult{
			OK: false, Engine: engine, Binary: binary, WorkDir: wd,
			Model: model, Effort: req.Effort, Reply: message, Error: code,
			Duration: time.Since(start).Round(time.Millisecond).String(),
		}, fmt.Errorf("%s", code)
	}
	if reply == "" {
		message, code := arkResponsesFailure(&llm.ArkResponsesError{Code: llm.ArkResponsesEmptyOutput})
		return &ChatResult{
			OK: false, Engine: engine, Binary: binary, Model: model, WorkDir: wd,
			Error: code, Reply: message,
			Duration: time.Since(start).Round(time.Millisecond).String(),
		}, fmt.Errorf("%s", code)
	}
	return &ChatResult{
		OK: true, Engine: engine, Binary: binary, WorkDir: wd,
		Model: model, Effort: req.Effort, Reply: reply,
		Duration: time.Since(start).Round(time.Millisecond).String(),
	}, nil
}

func verifiedArkModel(dataDir, requested string) (string, error) {
	cfg, err := agentcfg.Load(dataDir)
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(requested)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	candidate, ok := agentcfg.CandidateModel(model)
	if !ok || !agentcfg.ModelEnabled(cfg, candidate.ID) {
		return "", fmt.Errorf("model is not verified")
	}
	return candidate.ID, nil
}

func buildArkResponsesInput(policy string, history []ChatTurn, message string, files []string) string {
	var input strings.Builder
	input.WriteString("[System]\n")
	input.WriteString(strings.TrimSpace(policy))
	input.WriteString("\n\n[Conversation]")
	for _, turn := range history {
		content := strings.TrimSpace(turn.Content)
		if content == "" {
			continue
		}
		label := ""
		switch turn.Role {
		case "user":
			label = "User"
		case "assistant":
			label = "Assistant"
		case "system":
			label = "System note"
		default:
			continue
		}
		input.WriteString("\n\n")
		input.WriteString(label)
		input.WriteString(":\n")
		input.WriteString(content)
	}
	input.WriteString("\n\n[Current user]\n")
	input.WriteString(strings.TrimSpace(message))
	if len(files) > 0 {
		input.WriteString("\n\n[Attached files]")
		for _, file := range files {
			if file = strings.TrimSpace(file); file != "" {
				input.WriteString("\n- ")
				input.WriteString(filepath.Base(file))
			}
		}
	}
	return input.String()
}

func arkResponsesFailure(err error) (reply, code string) {
	var responseErr *llm.ArkResponsesError
	if !errors.As(err, &responseErr) {
		return "豆包 API 返回错误，模型没有执行。请检查本机 API 配置后重试。", "DOUBAO_API_ERROR"
	}
	switch responseErr.Code {
	case llm.ArkResponsesNotConfigured:
		return "Agent API 尚未配置或密钥无效。请在设置中配置有效 API 密钥后重试。", "AGENT_API_NOT_CONFIGURED"
	case llm.ArkResponsesTransportFailed:
		return "豆包 API 暂时不可达，模型没有执行。请检查网络与本机 API 配置后重试。", "DOUBAO_API_UNREACHABLE"
	case llm.ArkResponsesHTTPFailed:
		return doubaoHTTPFailure(responseErr.StatusCode)
	default:
		return "豆包 API 返回错误，模型没有执行。请检查本机 API 配置后重试。", "DOUBAO_API_ERROR"
	}
}

// presentGatewayFailure converts a common JSON error envelope into safe,
// actionable UI text. Never show a provider payload as an assistant answer.
func presentGatewayFailure(raw string) (message, code string, known bool) {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &envelope) != nil || envelope.Error.Message == "" {
		return "", "", false
	}
	if message, unavailable := gatewayUnavailableMessage(envelope.Error.Message); unavailable {
		return message, "DOUBAO_API_UNREACHABLE", true
	}
	return "豆包 API 返回错误，模型没有执行。请检查本机 API 配置后重试。", "DOUBAO_API_ERROR", true
}

func gatewayUnavailableMessage(raw string) (string, bool) {
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "connection refused") || strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "no such host") || strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "timed out") {
		return "豆包 API 暂时不可达，模型没有执行。请检查网络与本机 API 配置后重试。", true
	}
	return "", false
}

func doubaoHTTPFailure(status int) (reply, code string) {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "豆包 API 鉴权失败，模型没有执行。请检查本机 API 密钥后重试。", "DOUBAO_API_AUTH_FAILED"
	case http.StatusTooManyRequests:
		return "豆包 API 当前限流，模型没有执行。请稍后重试。", "DOUBAO_API_RATE_LIMITED"
	default:
		if status >= http.StatusInternalServerError {
			return "豆包 API 暂时不可用，模型没有执行。请稍后重试。", "DOUBAO_API_UNAVAILABLE"
		}
		return "豆包 API 返回错误，模型没有执行。请检查本机 API 配置后重试。", "DOUBAO_API_ERROR"
	}
}
