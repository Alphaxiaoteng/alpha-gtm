package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/trace"
)

func (s *Server) handleAPITrendRadarVideoAnalysis(w http.ResponseWriter, r *http.Request) {
	callID, err := strconv.ParseInt(r.PathValue("call_id"), 10, 64)
	if err != nil || callID <= 0 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_call_id", "视频记录 ID 无效")
		return
	}
	var body struct {
		Confirm bool   `json:"confirm"`
		Force   bool   `json:"force"`
		VideoID string `json:"video_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "拆解请求格式不正确")
		return
	}
	if !body.Confirm {
		writeDataAPIError(w, http.StatusBadRequest, "confirmation_required", "请明确确认后调用 Agent 或备用 LLM 拆解")
		return
	}
	targetVideoID := strings.TrimSpace(body.VideoID)
	if targetVideoID == "" {
		targetVideoID = strings.TrimSpace(r.URL.Query().Get("video_id"))
	}
	call, err := s.db.GetTikHubAPICall(callID)
	if err != nil || call.Status != "succeeded" || call.PlatformCode != "douyin" || (call.Capability != "content_detail" && call.Capability != "keyword_search") {
		writeDataAPIError(w, http.StatusNotFound, "video_snapshot_not_found", "找不到可拆解的抖音视频记录")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	snapshot, err := loadVideoAnalysisSnapshot(dataDir, call)
	if err != nil {
		writeDataAPIError(w, http.StatusUnprocessableEntity, "video_snapshot_invalid", "视频原始记录无法用于拆解")
		return
	}

	s.videoAnalysisMu.Lock()
	defer s.videoAnalysisMu.Unlock()
	evidence, evidenceErr := radar.ExtractVideoEvidenceForTarget(snapshot, targetVideoID)
	if evidenceErr != nil {
		writeDataAPIError(w, http.StatusUnprocessableEntity, "video_snapshot_invalid", "视频原始记录无法用于拆解")
		return
	}
	if targetVideoID == "" {
		targetVideoID = evidence.VideoID
	}
	var selection *agenthub.ProviderSelection
	var selectionErr error
	if s.agentHub == nil {
		selectionErr = agenthub.ErrProviderConnect
	} else {
		selection, selectionErr = s.agentHub.TextTaskSelection(r.Context())
	}
	provider := "agent-hub"
	model := ""
	if selection != nil {
		provider = string(selection.ProviderID)
		model = selection.ModelID
	}
	s.mu.Lock()
	fallbackProvider := strings.TrimSpace(s.cfg.AgentAPIProvider)
	fallbackModel := strings.TrimSpace(s.cfg.AgentModel)
	fallbackConfigured := s.cfg.AgentAPIVerified &&
		strings.TrimSpace(s.cfg.AgentAPIBaseURL) != "" &&
		strings.TrimSpace(s.cfg.AgentAPIKey) != "" &&
		fallbackModel != "" &&
		s.completeAI != nil
	s.mu.Unlock()
	if fallbackProvider == "" {
		fallbackProvider = "settings-llm"
	}
	var agentRunID string
	analysisContext, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	primary := radar.VideoAnalysisExecutor{
		Route: "agent", Provider: provider, Model: model,
		TaskID:         func() string { return agentRunID },
		ShouldFallback: agentAnalysisAllowsFallback,
		Complete: func(ctx context.Context, system, user string) (string, error) {
			if selectionErr != nil {
				return "", selectionErr
			}
			agentContext, cancelAgent := context.WithTimeout(ctx, 90*time.Second)
			defer cancelAgent()
			result, err := s.agentHub.CompleteTextTask(
				agentContext,
				*selection,
				"趋势雷达 · 视频拆解",
				system+"\n\nInput evidence JSON\n"+user,
				agenthub.ContextSnapshot{
					Workspace: "trend-radar",
					Page:      "video-analysis",
					Selections: []agenthub.ContextSelection{{
						ID: evidence.VideoID, Name: "抖音视频 " + evidence.VideoID,
					}},
				},
			)
			agentRunID = result.RunID
			return result.Answer, err
		},
	}
	var fallback *radar.VideoAnalysisExecutor
	if fallbackConfigured {
		fallback = &radar.VideoAnalysisExecutor{
			Route: "settings_llm_fallback", Provider: fallbackProvider, Model: fallbackModel,
			Complete: s.completeAI,
		}
	}
	analysis, reused, analyzeErr := radar.AnalyzeVideoWithFallbackForTarget(
		analysisContext,
		dataDir,
		snapshot,
		targetVideoID,
		primary,
		fallback,
		body.Force,
	)
	for index, attempt := range analysis.Attempts {
		level := "info"
		if attempt.Status != "succeeded" {
			level = "error"
		}
		_, _ = s.db.CreateOperationEvent(db.OperationEventInput{
			TraceID: string(trace.New()), TaskID: attempt.TaskID, PlatformCode: "douyin",
			Component: map[string]string{"agent": "agent", "settings_llm_fallback": "llm"}[attempt.Route],
			EventType: "video_analysis_attempt", Step: []string{"primary", "fallback"}[min(index, 1)],
			Status: attempt.Status, Level: level, ErrorCode: attempt.ErrorCode, DurationMS: attempt.DurationMS,
		})
	}
	status, level, errorCode := "succeeded", "info", ""
	if reused {
		status = "reused"
	}
	if analyzeErr != nil {
		status, level, errorCode = "failed", "error", analysis.ErrorCode
	}
	_, _ = s.db.CreateOperationEvent(db.OperationEventInput{
		TraceID: string(trace.New()), PlatformCode: "douyin", Component: "trend-radar",
		EventType: "video_analysis", Step: analysis.Route,
		Status: status, Level: level, ErrorCode: errorCode, DurationMS: analysis.DurationMS,
	})
	if analyzeErr != nil {
		writeDataAPIError(w, http.StatusBadGateway, errorCode, analysis.ErrorMessage)
		return
	}
	writeJSON(w, map[string]any{"analysis": analysis, "reused": reused})
}

func agentAnalysisAllowsFallback(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, agenthub.ErrProviderNotFound) ||
		errors.Is(err, agenthub.ErrProviderConnect) ||
		errors.Is(err, agenthub.ErrProviderExecutable) ||
		errors.Is(err, agenthub.ErrInvalidModel) ||
		errors.Is(err, agenthub.ErrModelNotVerified) ||
		errors.Is(err, agenthub.ErrRunActive) {
		return true
	}
	code := strings.TrimSpace(err.Error())
	if code == "AGENT_PROVIDER_CANCELLED" {
		return false
	}
	return strings.HasPrefix(code, "AGENT_PROVIDER_") ||
		code == "AGENT_OUTPUT_TOO_LARGE" ||
		code == "AGENT_OUTPUT_EMPTY"
}

func loadVideoAnalysisSnapshot(dataDir string, call *db.TikHubAPICall) (tikhub.CallSnapshot, error) {
	root, err := filepath.Abs(filepath.Join(dataDir, "tikhub", "calls"))
	if err != nil {
		return tikhub.CallSnapshot{}, err
	}
	target, err := filepath.Abs(call.RawPath)
	if err != nil || (target != root && !strings.HasPrefix(target, root+string(os.PathSeparator))) {
		return tikhub.CallSnapshot{}, os.ErrPermission
	}
	var snapshot tikhub.CallSnapshot
	if err := readRadarJSON(target, &snapshot); err != nil {
		return tikhub.CallSnapshot{}, err
	}
	if snapshot.CallID != call.ID || snapshot.Platform != "douyin" || (snapshot.Capability != "content_detail" && snapshot.Capability != "keyword_search") {
		return tikhub.CallSnapshot{}, os.ErrInvalid
	}
	return snapshot, nil
}
