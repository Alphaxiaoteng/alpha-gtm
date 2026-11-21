package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agnes"
	"alpha-nexus-egolite/internal/llm"
)

type AIConfigPayload struct {
	// Agent & LLM Models
	AgentAPIProvider string `json:"agent_api_provider"`
	AgentAPIBaseURL  string `json:"agent_api_base_url"`
	AgentAPIKey      string `json:"agent_api_key"`
	AgentModel       string `json:"agent_model"`
	AgentAPIVerified bool   `json:"agent_api_verified"`
	ThinkingMode     string `json:"thinking_mode"`
	CodingModel      string `json:"coding_model"`
	PlanModel        string `json:"plan_model"`

	// Image Models
	ImageAPIProvider string `json:"image_api_provider"`
	ImageAPIBaseURL  string `json:"image_api_base_url"`
	ImageAPIKey      string `json:"image_api_key"`
	ImageConfigured  bool   `json:"image_api_configured"`
	ImageModel       string `json:"image_model"`

	// Video Models
	VideoAPIProvider string `json:"video_api_provider"`
	VideoAPIBaseURL  string `json:"video_api_base_url"`
	VideoAPIKey      string `json:"video_api_key"`
	VideoConfigured  bool   `json:"video_api_configured"`
	VideoModel       string `json:"video_model"`

	// Legacy Media Models
	MediaAPIProvider string `json:"media_api_provider"`
	MediaAPIBaseURL  string `json:"media_api_base_url"`
	MediaAPIKey      string `json:"media_api_key"`
}

func (s *Server) handleAPIAIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.getAIConfig(w, r)
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		s.updateAIConfig(w, r)
		return
	}
	http.Error(w, "GET, POST or PUT only", 405)
}

func (s *Server) getAIConfig(w http.ResponseWriter, _ *http.Request) {
	secureMediaConfigured := s.secureMediaCredentialConfigured()
	s.mu.Lock()
	defer s.mu.Unlock()

	payload := AIConfigPayload{
		AgentAPIProvider: s.cfg.AgentAPIProvider,
		AgentAPIBaseURL:  s.cfg.AgentAPIBaseURL,
		AgentAPIKey:      maskSecretKey(s.cfg.AgentAPIKey),
		AgentModel:       s.cfg.AgentModel,
		AgentAPIVerified: s.cfg.AgentAPIVerified,
		ThinkingMode:     s.cfg.ThinkingMode,
		CodingModel:      s.cfg.CodingModel,
		PlanModel:        s.cfg.PlanModel,

		ImageAPIProvider: s.cfg.ImageAPIProvider,
		ImageAPIBaseURL:  s.cfg.ImageAPIBaseURL,
		ImageAPIKey:      maskSecretKey(s.cfg.ImageAPIKey),
		ImageConfigured:  secureMediaConfigured || strings.TrimSpace(s.cfg.ImageAPIKey) != "",
		ImageModel:       s.cfg.AIImageModel,

		VideoAPIProvider: s.cfg.VideoAPIProvider,
		VideoAPIBaseURL:  s.cfg.VideoAPIBaseURL,
		VideoAPIKey:      maskSecretKey(s.cfg.VideoAPIKey),
		VideoConfigured:  secureMediaConfigured || strings.TrimSpace(s.cfg.VideoAPIKey) != "",
		VideoModel:       s.cfg.AIVideoModel,

		MediaAPIProvider: s.cfg.MediaAPIProvider,
		MediaAPIBaseURL:  s.cfg.MediaAPIBaseURL,
		MediaAPIKey:      maskSecretKey(s.cfg.MediaAPIKey),
	}
	if payload.AgentModel == "" {
		payload.AgentModel = "gpt-4o"
	}
	if payload.ThinkingMode == "" {
		payload.ThinkingMode = "auto"
	}
	if payload.CodingModel == "" {
		payload.CodingModel = "claude-3-7-sonnet"
	}
	if payload.PlanModel == "" {
		payload.PlanModel = "o3-mini"
	}
	if payload.ImageModel == "" {
		payload.ImageModel = "agnes-image-2.1-flash"
	}
	if payload.VideoModel == "" {
		payload.VideoModel = "agnes-video-v2.0"
	}
	if payload.ImageAPIProvider == "" {
		payload.ImageAPIProvider = "agnes"
	}
	if payload.VideoAPIProvider == "" {
		payload.VideoAPIProvider = "agnes"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

func (s *Server) secureMediaCredentialConfigured() bool {
	if s.agnesAPIKeyConfigured != nil {
		if configured, err := s.agnesAPIKeyConfigured(); err == nil && configured {
			return true
		}
	}
	if s.agnesSaveAPIKey == nil {
		return false
	}

	s.mu.Lock()
	keys := make([]string, 0, 3)
	addLegacy := func(provider, key string) {
		provider = strings.ToLower(strings.TrimSpace(provider))
		key = strings.TrimSpace(key)
		if key == "" || (provider != "" && provider != "agnes" && provider != "agnes ai") {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}
	addLegacy(s.cfg.ImageAPIProvider, s.cfg.ImageAPIKey)
	addLegacy(s.cfg.VideoAPIProvider, s.cfg.VideoAPIKey)
	addLegacy(s.cfg.MediaAPIProvider, s.cfg.MediaAPIKey)
	s.mu.Unlock()
	if len(keys) != 1 || s.agnesSaveAPIKey(keys[0]) != nil {
		return false
	}

	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.updatePersistedConfigLocked(dataDir, func(existing map[string]any) {
		delete(existing, "image_api_key")
		delete(existing, "video_api_key")
		delete(existing, "media_api_key")
	})
	s.cfg.ImageAPIKey = ""
	s.cfg.VideoAPIKey = ""
	s.cfg.MediaAPIKey = ""
	return true
}

func (s *Server) updateAIConfig(w http.ResponseWriter, r *http.Request) {
	var body AIConfigPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	agentKey := body.AgentAPIKey
	if strings.TrimSpace(agentKey) == "" || strings.Contains(agentKey, "****") {
		agentKey = s.cfg.AgentAPIKey
	}
	imageKey := body.ImageAPIKey
	if strings.Contains(imageKey, "****") {
		imageKey = s.cfg.ImageAPIKey
	}
	videoKey := body.VideoAPIKey
	if strings.Contains(videoKey, "****") {
		videoKey = s.cfg.VideoAPIKey
	}
	agentChanged := strings.TrimSpace(body.AgentAPIProvider) != strings.TrimSpace(s.cfg.AgentAPIProvider) ||
		strings.TrimRight(strings.TrimSpace(body.AgentAPIBaseURL), "/") != strings.TrimRight(strings.TrimSpace(s.cfg.AgentAPIBaseURL), "/") ||
		strings.TrimSpace(body.AgentModel) != strings.TrimSpace(s.cfg.AgentModel) ||
		agentKey != s.cfg.AgentAPIKey
	agentVerified := s.cfg.AgentAPIVerified && !agentChanged

	if _, err := s.updatePersistedConfigLocked(dataDir, func(existing map[string]any) {
		existing["agent_api_provider"] = body.AgentAPIProvider
		existing["agent_api_base_url"] = body.AgentAPIBaseURL
		if agentKey != "" {
			existing["agent_api_key"] = agentKey
		}
		existing["agent_model"] = body.AgentModel
		existing["agent_api_verified"] = agentVerified
		if body.ThinkingMode != "" {
			existing["thinking_mode"] = body.ThinkingMode
		}
		existing["coding_model"] = body.CodingModel
		existing["plan_model"] = body.PlanModel

		existing["image_api_provider"] = body.ImageAPIProvider
		existing["image_api_base_url"] = body.ImageAPIBaseURL
		if imageKey != "" {
			existing["image_api_key"] = imageKey
		}
		existing["ai_image_model"] = body.ImageModel

		existing["video_api_provider"] = body.VideoAPIProvider
		existing["video_api_base_url"] = body.VideoAPIBaseURL
		if videoKey != "" {
			existing["video_api_key"] = videoKey
		}
		existing["ai_video_model"] = body.VideoModel
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	s.cfg.AgentAPIProvider = body.AgentAPIProvider
	s.cfg.AgentAPIBaseURL = body.AgentAPIBaseURL
	if agentKey != "" {
		s.cfg.AgentAPIKey = agentKey
	}
	s.cfg.AgentModel = body.AgentModel
	s.cfg.AgentAPIVerified = agentVerified
	if body.ThinkingMode != "" {
		s.cfg.ThinkingMode = body.ThinkingMode
	}
	s.cfg.CodingModel = body.CodingModel
	s.cfg.PlanModel = body.PlanModel

	s.cfg.ImageAPIProvider = body.ImageAPIProvider
	s.cfg.ImageAPIBaseURL = body.ImageAPIBaseURL
	if imageKey != "" {
		s.cfg.ImageAPIKey = imageKey
	}
	s.cfg.AIImageModel = body.ImageModel

	s.cfg.VideoAPIProvider = body.VideoAPIProvider
	s.cfg.VideoAPIBaseURL = body.VideoAPIBaseURL
	if videoKey != "" {
		s.cfg.VideoAPIKey = videoKey
	}
	s.cfg.AIVideoModel = body.VideoModel

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": "AI 配置已更新"})
}

func (s *Server) handleAPIAgentConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider     string `json:"agent_api_provider"`
		BaseURL      string `json:"agent_api_base_url"`
		APIKey       string `json:"agent_api_key"`
		Model        string `json:"agent_model"`
		ThinkingMode string `json:"thinking_mode"`
		CodingModel  string `json:"coding_model"`
		PlanModel    string `json:"plan_model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.Provider = strings.TrimSpace(body.Provider)
	body.BaseURL = resolveAIBaseURL(body.Provider, body.BaseURL)
	body.Model = strings.TrimSpace(body.Model)
	apiKey := strings.TrimSpace(body.APIKey)
	s.mu.Lock()
	if strings.Contains(apiKey, "****") {
		apiKey = s.cfg.AgentAPIKey
	}
	s.mu.Unlock()
	if body.Model == "" || apiKey == "" || !validDirectAPIBaseURL(body.BaseURL) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": "API 地址、API Key 和模型不能为空",
		})
		return
	}
	start := time.Now()
	if err := testAgentModel(r.Context(), body.BaseURL, apiKey, body.Model); err != nil {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{
			"ok": false, "latency_ms": time.Since(start).Milliseconds(),
			"error": "模型测试失败，未保存",
		})
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, "resolve data directory failed", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.updatePersistedConfigLocked(dataDir, func(existing map[string]any) {
		existing["agent_api_provider"] = body.Provider
		existing["agent_api_base_url"] = body.BaseURL
		existing["agent_api_key"] = apiKey
		existing["agent_model"] = body.Model
		existing["agent_api_verified"] = true
		if body.ThinkingMode != "" {
			existing["thinking_mode"] = body.ThinkingMode
		}
		if body.CodingModel != "" {
			existing["coding_model"] = body.CodingModel
		}
		if body.PlanModel != "" {
			existing["plan_model"] = body.PlanModel
		}
	}); err != nil {
		http.Error(w, "save Agent API configuration failed", http.StatusInternalServerError)
		return
	}
	s.cfg.AgentAPIProvider = body.Provider
	s.cfg.AgentAPIBaseURL = body.BaseURL
	s.cfg.AgentAPIKey = apiKey
	s.cfg.AgentModel = body.Model
	s.cfg.AgentAPIVerified = true
	if body.ThinkingMode != "" {
		s.cfg.ThinkingMode = body.ThinkingMode
	}
	if body.CodingModel != "" {
		s.cfg.CodingModel = body.CodingModel
	}
	if body.PlanModel != "" {
		s.cfg.PlanModel = body.PlanModel
	}
	writeJSON(w, map[string]any{
		"ok": true, "verified": true, "latency_ms": time.Since(start).Milliseconds(),
		"message": "模型测试通过并已添加",
	})
}

func (s *Server) handleAPIAITest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		Category string `json:"category"` // "agent", "image", or "video"
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if strings.Contains(apiKey, "****") {
		switch req.Category {
		case "image":
			apiKey = s.cfg.ImageAPIKey
		case "video":
			apiKey = s.cfg.VideoAPIKey
		default:
			apiKey = s.cfg.AgentAPIKey
		}
	}

	baseURL := resolveAIBaseURL(req.Provider, req.BaseURL)
	if req.Category == "agent" {
		start := time.Now()
		err := testAgentModel(r.Context(), baseURL, apiKey, strings.TrimSpace(req.Model))
		result := map[string]any{"ok": err == nil, "latency_ms": time.Since(start).Milliseconds()}
		if err == nil {
			result["message"] = "模型调用成功"
		} else {
			result["error"] = "模型调用失败"
		}
		writeJSON(w, result)
		return
	}

	start := time.Now()
	client := llm.NewDoubaoHTTPClient(8 * time.Second)

	pingURL := baseURL + "/models"
	httpReq, err := http.NewRequestWithContext(r.Context(), "GET", pingURL, nil)
	if err != nil {
		httpReq, _ = http.NewRequestWithContext(r.Context(), "GET", baseURL, nil)
	}
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(httpReq)
	latency := time.Since(start).Milliseconds()

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":         false,
			"latency_ms": latency,
			"error":      fmt.Sprintf("连接失败: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          true,
			"status_code": resp.StatusCode,
			"latency_ms":  latency,
			"message":     fmt.Sprintf("联通成功！节点正常 (HTTP 200, 耗时 %dms)", latency),
		})
		return
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          false,
			"status_code": resp.StatusCode,
			"latency_ms":  latency,
			"error":       fmt.Sprintf("节点连通，但 API Key 认证失败 (HTTP %d, 耗时 %dms)", resp.StatusCode, latency),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          false,
		"status_code": resp.StatusCode,
		"latency_ms":  latency,
		"error":       fmt.Sprintf("节点测试失败 (HTTP %d, 耗时 %dms)", resp.StatusCode, latency),
	})
}

func resolveAIBaseURL(provider, baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL != "" {
		return baseURL
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "volcano", "火山方舟":
		return "https://ark.cn-beijing.volces.com/api/v3"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "anthropic", "claude":
		return "https://api.anthropic.com/v1"
	case "gemini", "google gemini":
		return "https://generativelanguage.googleapis.com/v1beta"
	case "ollama":
		return "http://127.0.0.1:11434/v1"
	case "qwen", "阿里云百炼":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	case "siliconflow", "硅基流动":
		return "https://api.siliconflow.cn/v1"
	case "modelscope", "魔搭社区":
		return "https://api-inference.modelscope.cn/v1"
	case "agnes", "agnes ai":
		return agnes.APIBaseURL
	default:
		return "https://api.openai.com/v1"
	}
}

func validDirectAPIBaseURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.User == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func testAgentModel(ctx context.Context, baseURL, apiKey, model string) error {
	if !validDirectAPIBaseURL(baseURL) || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(model) == "" {
		return fmt.Errorf("invalid Agent API configuration")
	}
	gateway := &llm.Gateway{
		BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model,
		Client: llm.NewDoubaoHTTPClient(12 * time.Second), Strict: true,
	}
	testCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	_, err := gateway.Complete(testCtx, "Reply with only OK.", "Connection test")
	return err
}

func maskSecretKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
