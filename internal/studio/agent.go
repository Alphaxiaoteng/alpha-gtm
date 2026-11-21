// Package studio 提供本地素材、脚本和一次性剪辑方案能力。
package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/llm"
)

const (
	defaultClipMS        = 60_000
	minClipMS            = 5_000
	maxClipMS            = 180_000
	maxScriptPromptRunes = 8_000
)

// ClipPlan 是 Agent 给出的可审阅剪辑范围；执行必须由用户再次点击触发。
type ClipPlan struct {
	StartMS int    `json:"start_ms"`
	EndMS   int    `json:"end_ms"`
	Title   string `json:"title"`
	Reason  string `json:"reason"`
}

// DoubaoAgent only uses the fixed Doubao API contract. It produces a plan;
// writing a video remains a separate user-confirmed local ffmpeg action.
type DoubaoAgent struct {
	complete func(context.Context, string, string) (string, error)
}

func NewDoubaoAgent() *DoubaoAgent {
	return &DoubaoAgent{complete: func(ctx context.Context, system, prompt string) (string, error) {
		gateway := llm.NewDoubaoGateway(90 * time.Second)
		if gateway == nil {
			return "", fmt.Errorf("DOUBAO_API_NOT_CONFIGURED")
		}
		return gateway.Complete(ctx, system, prompt)
	}}
}

// NewDoubaoAgentWithCompletion keeps unit tests offline while exercising the
// same strict plan validation as the fixed production gateway.
func NewDoubaoAgentWithCompletion(complete func(context.Context, string, string) (string, error)) *DoubaoAgent {
	return &DoubaoAgent{complete: complete}
}

func normalizeTargetMS(value int) (int, error) {
	if value == 0 {
		return defaultClipMS, nil
	}
	if value < minClipMS || value > maxClipMS {
		return 0, fmt.Errorf("clip target duration must be %d-%d ms", minClipMS, maxClipMS)
	}
	return value, nil
}

// Plan 根据脚本和媒体元数据生成一个可审阅的剪辑范围；失败时不伪造本地方案。
func (a *DoubaoAgent) Plan(ctx context.Context, media db.MediaLibraryItem, script db.StudioScript, targetMS int) (*ClipPlan, error) {
	if a == nil || a.complete == nil {
		return nil, fmt.Errorf("DOUBAO_API_NOT_CONFIGURED")
	}
	if media.ID <= 0 || !strings.HasPrefix(strings.ToLower(media.Mime), "video/") {
		return nil, fmt.Errorf("CLIP_SOURCE_MUST_BE_VIDEO")
	}
	if script.ID <= 0 || strings.TrimSpace(script.Body) == "" {
		return nil, fmt.Errorf("CLIP_SCRIPT_REQUIRED")
	}
	targetMS, err := normalizeTargetMS(targetMS)
	if err != nil {
		return nil, err
	}
	body := []rune(strings.TrimSpace(script.Body))
	if len(body) > maxScriptPromptRunes {
		body = body[:maxScriptPromptRunes]
	}
	system := "你是视频粗剪规划 Agent。只输出一个 JSON 对象，不要 Markdown。JSON 字段必须为 start_ms、end_ms、title、reason。start_ms/end_ms 是整数毫秒；片段时长不得超过 target_duration_ms；不能编造素材时长外的范围。"
	prompt := fmt.Sprintf("媒体：mime=%s，宽=%d，高=%d，duration_ms=%d。\n目标时长上限：%d。\n脚本标题：%s\n脚本：\n%s",
		media.Mime, media.Width, media.Height, media.DurationMS, targetMS, script.Title, string(body))
	raw, err := a.complete(ctx, system, prompt)
	if err != nil {
		return nil, fmt.Errorf("DOUBAO_API_UNAVAILABLE: %w", err)
	}
	plan, err := parseClipPlan(raw, media.DurationMS, targetMS)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func parseClipPlan(raw string, sourceDurationMS, targetMS int) (*ClipPlan, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("DOUBAO_API_INVALID_PLAN: expected JSON object")
	}
	var plan ClipPlan
	if err := json.Unmarshal([]byte(raw[start:end+1]), &plan); err != nil {
		return nil, fmt.Errorf("DOUBAO_API_INVALID_PLAN: %w", err)
	}
	if plan.StartMS < 0 || plan.EndMS <= plan.StartMS {
		return nil, fmt.Errorf("DOUBAO_API_INVALID_PLAN: invalid range")
	}
	if plan.EndMS-plan.StartMS > targetMS {
		return nil, fmt.Errorf("DOUBAO_API_INVALID_PLAN: clip exceeds target duration")
	}
	if sourceDurationMS > 0 && plan.EndMS > sourceDurationMS {
		return nil, fmt.Errorf("DOUBAO_API_INVALID_PLAN: clip exceeds source duration")
	}
	plan.Title = strings.TrimSpace(plan.Title)
	plan.Reason = strings.TrimSpace(plan.Reason)
	if plan.Title == "" || plan.Reason == "" || len([]rune(plan.Title)) > 120 || len([]rune(plan.Reason)) > 500 {
		return nil, fmt.Errorf("DOUBAO_API_INVALID_PLAN: title and reason are required")
	}
	return &plan, nil
}
