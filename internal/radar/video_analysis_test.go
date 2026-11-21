package radar

import (
	"context"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/tikhub"
)

func TestVideoAnalysisPromptForbidsUnsourcedIndustryBenchmarks(t *testing.T) {
	prompt := videoAnalysisSystemPrompt()
	for _, rule := range []string{
		"不得引用输入中未提供的行业基准",
		"不得声称高于或低于平台常见水平",
	} {
		if !strings.Contains(prompt, rule) {
			t.Fatalf("video analysis prompt is missing evidence rule %q", rule)
		}
	}
}

func TestAnalyzeVideoUsesConfiguredModelAndReusesSuccessfulVideoID(t *testing.T) {
	dataDir := t.TempDir()
	snapshot := videoAnalysisFixture(15)
	calls := 0
	complete := func(_ context.Context, system, user string) (string, error) {
		calls++
		if system == "" || user == "" {
			t.Fatal("model prompt is empty")
		}
		return `{
			"summary":"基于标题与公开指标的分析",
			"viral_potential":"收藏和分享可作为传播信号，但播放量缺失",
			"hook":"未验证",
			"evidence":["点赞 2035","收藏 852"],
			"structure":["完整口播结构未验证"],
			"audience":"关注情感与个人成长的人群，属于推断",
			"emotional_drivers":["认知冲突，未验证"],
			"topic_suggestions":["亲密关系中的认知误区"],
			"analysis_prompts":[{"title":"证据审计","text":"区分事实与推断"}],
			"limitations":["未提供字幕，无法分析完整口播"]
		}`, nil
	}
	first, reused, err := AnalyzeVideo(context.Background(), dataDir, snapshot, "doubao", "model-1", complete, false)
	if err != nil {
		t.Fatal(err)
	}
	if reused || first.Status != "succeeded" || calls != 1 {
		t.Fatalf("first=%#v reused=%v calls=%d", first, reused, calls)
	}
	secondSnapshot := videoAnalysisFixture(16)
	second, reused, err := AnalyzeVideo(context.Background(), dataDir, secondSnapshot, "doubao", "model-1", complete, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reused || second.SourceCallID != 15 || calls != 1 {
		t.Fatalf("second=%#v reused=%v calls=%d", second, reused, calls)
	}
}

func TestAnalyzeVideoRejectsUnstructuredModelOutput(t *testing.T) {
	analysis, _, err := AnalyzeVideo(
		context.Background(),
		t.TempDir(),
		videoAnalysisFixture(20),
		"doubao",
		"model-1",
		func(context.Context, string, string) (string, error) { return "这不是 JSON", nil },
		false,
	)
	if err == nil || analysis.Status != "failed" || analysis.ErrorCode != "MODEL_OUTPUT_INVALID" {
		t.Fatalf("analysis=%#v err=%v", analysis, err)
	}
}

func TestAnalyzeVideoFallsBackOnceAndStoresActualExecutor(t *testing.T) {
	primaryCalls, fallbackCalls := 0, 0
	fallback := VideoAnalysisExecutor{
		Route: "settings_llm_fallback", Provider: "custom", Model: "fallback-model",
		Complete: func(context.Context, string, string) (string, error) {
			fallbackCalls++
			return `{
				"summary":"备用模型基于标题与公开指标完成分析",
				"viral_potential":"收藏和分享是可观察信号",
				"hook":"未验证",
				"evidence":["点赞 2035","收藏 852"],
				"structure":["完整结构未验证"],
				"audience":"情感成长受众，属于推断",
				"emotional_drivers":["认知冲突，未验证"],
				"topic_suggestions":["亲密关系中的认知误区"],
				"analysis_prompts":[{"title":"证据审计","text":"区分事实与推断"}],
				"limitations":["没有字幕"]
			}`, nil
		},
	}
	analysis, reused, err := AnalyzeVideoWithFallback(
		context.Background(),
		t.TempDir(),
		videoAnalysisFixture(21),
		VideoAnalysisExecutor{
			Route: "agent", Provider: "opencode", Model: "agent-model",
			Complete: func(context.Context, string, string) (string, error) {
				primaryCalls++
				return `{"structure":"wrong type"}`, nil
			},
		},
		&fallback,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reused || primaryCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("reused=%v primary=%d fallback=%d", reused, primaryCalls, fallbackCalls)
	}
	if analysis.Status != "succeeded" || analysis.Route != "settings_llm_fallback" ||
		analysis.Provider != "custom" || analysis.Model != "fallback-model" {
		t.Fatalf("analysis=%#v", analysis)
	}
	if len(analysis.Attempts) != 2 ||
		analysis.Attempts[0].ErrorCode != "MODEL_OUTPUT_INVALID" ||
		analysis.Attempts[1].Status != "succeeded" {
		t.Fatalf("attempts=%#v", analysis.Attempts)
	}
}

func TestAnalyzeVideoDoesNotFallbackAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fallbackCalls := 0
	fallback := VideoAnalysisExecutor{
		Route: "settings_llm_fallback", Provider: "custom", Model: "fallback-model",
		Complete: func(context.Context, string, string) (string, error) {
			fallbackCalls++
			return "{}", nil
		},
	}
	analysis, _, err := AnalyzeVideoWithFallback(
		ctx,
		t.TempDir(),
		videoAnalysisFixture(22),
		VideoAnalysisExecutor{
			Route: "agent", Provider: "opencode", Model: "agent-model",
			Complete: func(ctx context.Context, _, _ string) (string, error) {
				return "", ctx.Err()
			},
			ShouldFallback: func(error) bool { return true },
		},
		&fallback,
		false,
	)
	if err == nil || fallbackCalls != 0 || len(analysis.Attempts) != 1 {
		t.Fatalf("analysis=%#v err=%v fallback=%d", analysis, err, fallbackCalls)
	}
}

func TestExtractVideoEvidenceFromSearchSnapshot(t *testing.T) {
	searchSnapshot := tikhub.CallSnapshot{
		SchemaVersion: 1,
		CallID:        30,
		Platform:      "douyin",
		EndpointID:    "douyin.video_search",
		Capability:    "keyword_search",
		Parameters:    map[string]any{"keyword": "AI"},
		Response: map[string]interface{}{
			"data": map[string]interface{}{
				"business_data": []interface{}{
					map[string]interface{}{
						"data": map[string]interface{}{
							"aweme_info": map[string]interface{}{
								"aweme_id": "7699455064305306920",
								"desc":     "AI漫剧教程 #AI #AI漫剧",
								"duration": float64(120000),
								"author":   map[string]interface{}{"nickname": "AI创作者A"},
								"statistics": map[string]interface{}{
									"digg_count": float64(1000), "collect_count": float64(500),
								},
								"text_extra": []interface{}{map[string]interface{}{"hashtag_name": "AI漫剧"}},
							},
						},
					},
					map[string]interface{}{
						"data": map[string]interface{}{
							"aweme_info": map[string]interface{}{
								"aweme_id": "7699488712627177146",
								"desc":     "AI短剧制作 #短剧",
								"duration": float64(60000),
								"author":   map[string]interface{}{"nickname": "AI创作者B"},
								"statistics": map[string]interface{}{
									"digg_count": float64(800), "collect_count": float64(300),
								},
							},
						},
					},
				},
			},
		},
	}

	// 1. Without targetVideoID, extracts first valid aweme
	firstEvidence, err := ExtractVideoEvidence(searchSnapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if firstEvidence.VideoID != "7699455064305306920" || firstEvidence.Creator != "AI创作者A" {
		t.Fatalf("unexpected first evidence: %#v", firstEvidence)
	}

	// 2. With targetVideoID for second video
	secondEvidence, err := ExtractVideoEvidenceForTarget(searchSnapshot, "7699488712627177146")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondEvidence.VideoID != "7699488712627177146" || secondEvidence.Creator != "AI创作者B" {
		t.Fatalf("unexpected second evidence: %#v", secondEvidence)
	}

	// 3. With non-existent video ID
	_, err = ExtractVideoEvidenceForTarget(searchSnapshot, "9999999999999999999")
	if err == nil {
		t.Fatal("expected error for non-existent video ID, got nil")
	}
}

func videoAnalysisFixture(callID int64) tikhub.CallSnapshot {
	return tikhub.CallSnapshot{
		SchemaVersion: 1,
		CallID:        callID,
		Platform:      "douyin",
		EndpointID:    tikhub.DouyinVideoDetailEndpoint,
		Capability:    "content_detail",
		Parameters:    map[string]any{"aweme_id": "7666283984318131889"},
		Response: map[string]interface{}{"data": map[string]interface{}{"aweme_detail": map[string]interface{}{
			"aweme_id": "7666283984318131889",
			"desc":     "#火种情感 #认知",
			"duration": float64(251076),
			"author":   map[string]interface{}{"nickname": "样本博主"},
			"statistics": map[string]interface{}{
				"digg_count": float64(2035), "collect_count": float64(852), "share_count": float64(206),
			},
			"text_extra": []interface{}{map[string]interface{}{"hashtag_name": "火种情感"}},
		}}},
	}
}
