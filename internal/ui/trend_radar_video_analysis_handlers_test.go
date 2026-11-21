package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tikhub"
)

type videoAnalysisAgentRunner struct {
	calls    *int
	response string
}

const videoAnalysisResponse = `{
	"summary":"仅基于标题标签与公开指标的分析",
	"viral_potential":"收藏与转发是可观察传播信号",
	"hook":"未验证",
	"evidence":["点赞 2035","收藏 852"],
	"structure":["口播结构未验证"],
	"audience":"情感成长受众，属于推断",
	"emotional_drivers":["认知冲突，未验证"],
	"topic_suggestions":["亲密关系中的认知误区"],
	"analysis_prompts":[{"title":"证据审计","text":"逐项区分事实与推断"}],
	"limitations":["没有字幕，只分析标题、标签和公开指标"]
}`

func (r videoAnalysisAgentRunner) Run(
	_ context.Context,
	request agenthub.RunRequest,
	emit func(agenthub.ProviderEvent),
) (string, error) {
	(*r.calls)++
	if !request.Ephemeral || request.BrokerEndpoint != "" || request.BrokerToken != "" || request.SessionID != "" {
		return "", context.Canceled
	}
	emit(agenthub.ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": r.response}})
	return "", nil
}

func TestVideoAnalysisHandlerCallsModelOnceAndReusesVideoID(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	first := saveVideoAnalysisCallFixture(t, database, dataDir, 1)
	second := saveVideoAnalysisCallFixture(t, database, dataDir, 2)
	modelCalls := 0
	fallbackCalls := 0
	const model = "opencode/test-model"
	store := agenthub.NewStore(database)
	if err := store.SaveProviderProbe(context.Background(), agenthub.ProviderProbe{
		ProviderID: agenthub.ProviderOpenCode, State: agenthub.ProviderStateReady,
		Available: true, Ready: true, Version: "test-version",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveVerifiedModel(context.Background(), agenthub.ProviderOpenCode, "test-version", model); err != nil {
		t.Fatal(err)
	}
	registry, err := agenthub.NewRegistry(agenthub.Provider{
		Descriptor: agenthub.ProviderDescriptor{ID: agenthub.ProviderOpenCode, Label: "OpenCode"},
		Probe: func(context.Context) agenthub.ProviderProbe {
			return agenthub.ProviderProbe{ProviderID: agenthub.ProviderOpenCode, State: agenthub.ProviderStateReady, Available: true, Ready: true}
		},
		Runner: videoAnalysisAgentRunner{calls: &modelCalls, response: videoAnalysisResponse},
	})
	if err != nil {
		t.Fatal(err)
	}
	hub, err := agenthub.NewService(store, registry, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Shutdown(context.Background())
	if _, err := hub.SaveProviderSelection(context.Background(), agenthub.ProviderSelection{
		ProviderID: agenthub.ProviderOpenCode, ModelID: model,
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		cfg: config.Config{
			DataDir: dataDir, AgentAPIProvider: "custom", AgentAPIBaseURL: "https://example.invalid/v1",
			AgentAPIKey: "synthetic-key", AgentModel: "fallback-model", AgentAPIVerified: true,
		},
		db: database, agentHub: hub,
	}
	server.completeAI = func(context.Context, string, string) (string, error) {
		fallbackCalls++
		return videoAnalysisResponse, nil
	}
	for index, call := range []*db.TikHubAPICall{first, second} {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/trend-radar/video-search/1/analyze",
			bytes.NewBufferString(`{"confirm":true}`),
		)
		request.SetPathValue("call_id", strconv.FormatInt(call.ID, 10))
		recorder := httptest.NewRecorder()
		server.handleAPITrendRadarVideoAnalysis(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status=%d body=%s", index, recorder.Code, recorder.Body.String())
		}
		var result struct {
			Reused bool `json:"reused"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Reused != (index == 1) {
			t.Fatalf("request %d reused=%v", index, result.Reused)
		}
	}
	if modelCalls != 1 {
		t.Fatalf("model calls=%d want=1", modelCalls)
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls=%d want=0", fallbackCalls)
	}
}

func TestVideoAnalysisHandlerFallsBackToVerifiedSettingsLLMWithoutTikHubRecall(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	call := saveVideoAnalysisCallFixture(t, database, dataDir, 1)
	fallbackCalls := 0
	server := &Server{
		cfg: config.Config{
			DataDir: dataDir, AgentAPIProvider: "custom", AgentAPIBaseURL: "https://example.invalid/v1",
			AgentAPIKey: "synthetic-key", AgentModel: "fallback-model", AgentAPIVerified: true,
		},
		db: database,
	}
	server.completeAI = func(context.Context, string, string) (string, error) {
		fallbackCalls++
		return videoAnalysisResponse, nil
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/trend-radar/video-search/1/analyze",
		bytes.NewBufferString(`{"confirm":true}`),
	)
	request.SetPathValue("call_id", strconv.FormatInt(call.ID, 10))
	recorder := httptest.NewRecorder()
	server.handleAPITrendRadarVideoAnalysis(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Analysis struct {
			Route    string `json:"route"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
			Attempts []struct {
				Route  string `json:"route"`
				Status string `json:"status"`
			} `json:"attempts"`
		} `json:"analysis"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if fallbackCalls != 1 || result.Analysis.Route != "settings_llm_fallback" ||
		result.Analysis.Provider != "custom" || result.Analysis.Model != "fallback-model" {
		t.Fatalf("fallback=%d analysis=%+v", fallbackCalls, result.Analysis)
	}
	if len(result.Analysis.Attempts) != 2 ||
		result.Analysis.Attempts[0].Route != "agent" ||
		result.Analysis.Attempts[1].Status != "succeeded" {
		t.Fatalf("attempts=%+v", result.Analysis.Attempts)
	}
}

func saveVideoAnalysisCallFixture(t *testing.T, database *db.DB, dataDir string, sequence int64) *db.TikHubAPICall {
	t.Helper()
	call, err := database.CreateTikHubAPICall(db.TikHubAPICallInput{
		TraceID: "trace-test", PlatformCode: "douyin",
		EndpointID: tikhub.DouyinVideoDetailEndpoint, Capability: "content_detail",
		Method: "GET", Path: "/api/v1/douyin/web/fetch_one_video",
		ParametersJSON: `{"aweme_id":"7666283984318131889"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tikhub.CallSnapshot{
		SchemaVersion: 1, CallID: call.ID, TraceID: call.TraceID,
		Platform: "douyin", EndpointID: tikhub.DouyinVideoDetailEndpoint,
		Capability: "content_detail", Method: "GET", Path: call.Path,
		Parameters: map[string]any{"aweme_id": "7666283984318131889"},
		Response: map[string]interface{}{"data": map[string]interface{}{"aweme_detail": map[string]interface{}{
			"aweme_id": "7666283984318131889", "desc": "#火种情感 #认知",
			"author": map[string]interface{}{"nickname": "样本博主"},
			"statistics": map[string]interface{}{
				"digg_count": float64(2035), "collect_count": float64(852),
			},
		}}},
	}
	path, size, err := tikhub.SaveCallSnapshot(dataDir, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	finished, err := database.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
		Status: "succeeded", RawPath: path, ResponseBytes: size, ItemCount: sequence,
	})
	if err != nil {
		t.Fatal(err)
	}
	return finished
}
