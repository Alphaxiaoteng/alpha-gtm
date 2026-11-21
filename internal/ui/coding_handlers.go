package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agentcfg"
	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/agents"
	"alpha-nexus-egolite/internal/agentws"
	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/terminal"
	"alpha-nexus-egolite/internal/trace"
)

// resolveCodingLLMConfig 是包内测试替换点；状态接口只返回钥匙串密钥是否存在。
var resolveCodingLLMConfig = llm.ResolveDoubaoConfig
var saveCodingLLMAPIKey = llm.SaveDoubaoAPIKey
var resolveCustomCodingLLMAPIKey = llm.ResolveCustomAPIKey
var saveCustomCodingLLMAPIKey = llm.SaveCustomAPIKey
var validateCodingLLMModel = func(ctx context.Context, apiKey, model string) error {
	return llm.ValidateArkResponsesModel(ctx, apiKey, model, 12*time.Second)
}
var validateCustomCodingLLMModel = func(ctx context.Context, apiKey, baseURL, protocol, model string) error {
	if protocol == agentcfg.ProtocolResponses {
		return llm.ValidateOpenAIResponsesModel(ctx, baseURL, apiKey, model, 12*time.Second)
	}
	gateway := &llm.Gateway{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  apiKey,
		Model:   model,
		Client:  llm.NewDoubaoHTTPClient(12 * time.Second),
		Strict:  true,
	}
	_, err := gateway.Complete(ctx, "Reply with only OK.", "Connection test")
	return err
}

type codingChatBody struct {
	SessionID      string   `json:"session_id"`
	Engine         string   `json:"engine"`
	Message        string   `json:"message"`
	WorkDir        string   `json:"work_dir"`
	Continue       bool     `json:"continue"`
	Model          string   `json:"model"`
	Effort         string   `json:"effort"`
	Thinking       bool     `json:"thinking"`
	PermissionMode string   `json:"permission_mode"`
	Files          []string `json:"files"` // absolute paths already on disk
	Mode           string   `json:"mode"`  // chat | tools
}

// POST /api/coding/chat  { session_id, engine, message, model, effort, files[], continue }
func (s *Server) handleAPICodingChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body codingChatBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	body.Mode = strings.ToLower(strings.TrimSpace(body.Mode))
	if body.Mode == "" {
		body.Mode = "chat"
	}
	if body.Mode != "chat" && body.Mode != "tools" {
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}
	dir, _ := s.cfg.ResolvedDataDir()
	ws, err := agentws.Ensure(dir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_ = terminal.EnsureSessionLayout(dir)
	acfg, _ := agentcfg.Load(dir)

	body.Engine = strings.ToLower(strings.TrimSpace(body.Engine))
	if body.Engine == "" {
		if acfg != nil && acfg.Engine != "" {
			body.Engine = acfg.Engine
		} else {
			body.Engine = "doubao_api"
		}
	}
	body.Model = codingModelForMode(body.Mode, body.Model, acfg)
	if body.Effort == "" && acfg != nil {
		body.Effort = acfg.DefaultEffort
	}

	// Session isolation: each session = own cwd for CLI continue (no speed loss)
	workDir := ws
	cont := body.Continue
	var sess *terminal.ChatSession
	if body.SessionID != "" {
		sess, err = terminal.LoadChatSession(dir, body.SessionID)
		if err != nil {
			http.Error(w, "session not found", 404)
			return
		}
		workDir = sess.Dir
		if body.Engine == "" {
			body.Engine = sess.Engine
		}
		if body.Model == "" {
			body.Model = sess.Model
		}
		if body.Effort == "" {
			body.Effort = sess.Effort
		}
		// continue if session already has turns
		if len(sess.Messages) > 0 {
			cont = true
		}
		_, _ = terminal.UpdateSessionPrefs(dir, sess.ID, body.Engine, body.Model, body.Effort, "")
	}
	if body.Mode == "tools" {
		if sess == nil {
			http.Error(w, "session_id required for tools mode", http.StatusBadRequest)
			return
		}
		res := s.runCodingTools(r.Context(), dir, sess, body)
		s.writeCodingChatResult(w, dir, sess, res)
		return
	}

	// Build history BEFORE appending current user (for multi-turn)
	var history []terminal.ChatTurn
	if sess != nil {
		for _, m := range sess.Messages {
			if m.Role == "user" || m.Role == "assistant" {
				history = append(history, terminal.ChatTurn{Role: m.Role, Content: m.Text})
			}
		}
	}

	// Persist user message
	if sess != nil {
		_, _ = terminal.AppendChatMessage(dir, sess.ID, terminal.ChatMsg{
			Role: "user", Text: body.Message, Files: body.Files,
			Engine: body.Engine, Model: body.Model, Effort: body.Effort,
		})
	}

	// Multi-turn via the configured Doubao-compatible API.
	_ = cont
	res, err := terminal.ChatWithHistory(r.Context(), terminal.ChatRequest{
		Engine:       body.Engine,
		Message:      body.Message,
		WorkDir:      workDir,
		DataDir:      dir,
		Model:        body.Model,
		Effort:       body.Effort,
		Thinking:     body.Thinking,
		Files:        body.Files,
		ExtraAddDirs: []string{ws, agentws.WorksPath(dir), agentws.AutomationsPath(dir)},
		Timeout:      2 * time.Minute,
	}, history)
	if res == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error": errString(err), "session_id": body.SessionID,
		})
		return
	}

	s.writeCodingChatResult(w, dir, sess, res)
}

func codingModelForMode(_ string, reqModel string, cfg *agentcfg.Config) string {
	if candidate, ok := agentcfg.CandidateModel(reqModel); ok && agentcfg.ModelEnabled(cfg, candidate.ID) {
		return candidate.ID
	}
	if cfg != nil && agentcfg.ModelEnabled(cfg, cfg.DefaultModel) {
		return cfg.DefaultModel
	}
	return ""
}

// codingToolAttachmentPaths resolves only regular files rooted in this chat
// session's attaches/ directory. The tool model never receives arbitrary
// client-provided filesystem paths.
func codingToolAttachmentPaths(sess *terminal.ChatSession, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if sess == nil || strings.TrimSpace(sess.Dir) == "" {
		return nil, fmt.Errorf("TOOL_AGENT_ATTACHMENT_INVALID")
	}
	sessionRoot, err := filepath.EvalSymlinks(sess.Dir)
	if err != nil {
		return nil, fmt.Errorf("TOOL_AGENT_ATTACHMENT_INVALID")
	}
	attachmentsRoot, err := filepath.EvalSymlinks(filepath.Join(sess.Dir, "attaches"))
	attachmentsRelative, relativeErr := filepath.Rel(sessionRoot, attachmentsRoot)
	if err != nil || relativeErr != nil || attachmentsRelative != "attaches" {
		return nil, fmt.Errorf("TOOL_AGENT_ATTACHMENT_INVALID")
	}
	seen := make(map[string]struct{}, len(files))
	allowed := make([]string, 0, len(files))
	for _, file := range files {
		absolute, err := filepath.Abs(strings.TrimSpace(file))
		if err != nil {
			return nil, fmt.Errorf("TOOL_AGENT_ATTACHMENT_INVALID")
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil || !pathInside(attachmentsRoot, resolved) {
			return nil, fmt.Errorf("TOOL_AGENT_ATTACHMENT_INVALID")
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("TOOL_AGENT_ATTACHMENT_INVALID")
		}
		if _, exists := seen[resolved]; !exists {
			seen[resolved] = struct{}{}
			allowed = append(allowed, resolved)
		}
	}
	return allowed, nil
}

func pathInside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func codingToolGoal(message string, attachments []string) string {
	if len(attachments) == 0 {
		return message
	}
	return strings.TrimSpace(message) + "\n\n本次用户明确上传到当前会话的本地附件。若任务涉及整理或导入素材，只可对下列路径调用 media.import；不得访问其他文件：\n- " + strings.Join(attachments, "\n- ")
}

// runCodingTools executes only the Tool Agent's confirmation-gated local plan.
// It deliberately shares the chat session so outcomes remain auditable in UI.
func (s *Server) runCodingTools(ctx context.Context, dataDir string, sess *terminal.ChatSession, body codingChatBody) *terminal.ChatResult {
	_, _ = terminal.AppendChatMessage(dataDir, sess.ID, terminal.ChatMsg{
		Role: "user", Text: body.Message, Files: body.Files,
		Engine: "llm_tools", Model: body.Model, Effort: body.Effort,
	})
	attachments, err := codingToolAttachmentPaths(sess, body.Files)
	if err != nil {
		return &terminal.ChatResult{
			OK: false, Engine: "llm_tools", Model: body.Model, Effort: body.Effort,
			Error: "TOOL_AGENT_ATTACHMENT_INVALID",
			Reply: "工具协同只接受当前会话中已上传的常规附件；未执行任何工具。",
		}
	}
	result, runErr := agents.RunScheme(ctx, agents.SchemeRunRequest{
		Scheme:                  agents.SchemeLLMTools,
		Goal:                    codingToolGoal(body.Message, attachments),
		DryRun:                  false,
		Model:                   body.Model,
		DataDir:                 dataDir,
		Runtime:                 s.toolRuntime,
		RestrictMediaImport:     true,
		AllowedMediaImportPaths: attachments,
	})
	if result == nil {
		return &terminal.ChatResult{
			OK: false, Engine: "llm_tools", Model: body.Model, Effort: body.Effort,
			Error: "TOOL_AGENT_UNAVAILABLE", Reply: "本地工具协同未能启动；未执行任何工具。",
		}
	}
	errCode := result.Error
	if errCode == "" && runErr != nil {
		errCode = "TOOL_AGENT_FAILED"
	}
	return &terminal.ChatResult{
		OK:       result.OK,
		Engine:   "llm_tools",
		Model:    body.Model,
		Effort:   body.Effort,
		Reply:    codingToolsReply(result),
		Duration: result.Duration,
		Error:    errCode,
	}
}

func codingToolsReply(result *agents.SchemeRunResult) string {
	if result == nil {
		return "本地工具协同未能启动；未执行任何工具。"
	}
	message := strings.TrimSpace(result.Message)
	switch message {
	case "all steps ok":
		message = "本地工具已完成"
	case "finished with errors":
		message = "本地工具执行未完成"
	case "llm_tools loop finished":
		message = "本地工具协同已结束"
	}
	lines := []string{message}
	for _, step := range result.Steps {
		tool, _ := step["tool"].(string)
		if tool == "" {
			continue
		}
		if executed, exists := step["executed"].(bool); exists && !executed {
			lines = append(lines, tool+"：未执行（需单独确认）")
			continue
		}
		if ok, exists := step["ok"].(bool); exists {
			if ok {
				lines = append(lines, tool+"：已完成")
			} else {
				lines = append(lines, tool+"：未完成")
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Server) writeCodingChatResult(w http.ResponseWriter, dataDir string, sess *terminal.ChatSession, res *terminal.ChatResult) {
	if sess != nil && res != nil {
		_, _ = terminal.AppendChatMessage(dataDir, sess.ID, terminal.ChatMsg{
			Role: "assistant", Text: res.Reply, Engine: res.Engine,
			Model: res.Model, Effort: res.Effort, Duration: res.Duration,
			Error: !res.OK,
		})
		if s2, e := terminal.LoadChatSession(dataDir, sess.ID); e == nil {
			resMap := map[string]any{}
			b, _ := json.Marshal(res)
			_ = json.Unmarshal(b, &resMap)
			resMap["session_id"] = s2.ID
			resMap["session_title"] = s2.Title
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resMap)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// GET /api/coding/status
func (s *Server) handleAPICodingStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir, _ := s.cfg.ResolvedDataDir()
	ws, _ := agentws.Ensure(dir)
	_ = terminal.EnsureSessionLayout(dir)
	acfg, _ := agentcfg.Load(dir)
	if acfg == nil {
		acfg = &agentcfg.Config{Engine: "doubao_api", DefaultEffort: "medium"}
	}
	llmCfg := resolveCodingLLMConfig()
	apiConfigured := strings.TrimSpace(llmCfg.APIKey) != ""
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"work_dir":       ws,
		"workspace":      ws,
		"automations":    agentws.AutomationsPath(dir),
		"works":          agentws.WorksPath(dir),
		"mode":           "chat",
		"sandbox":        true,
		"engine":         acfg.Engine,
		"model":          acfg.DefaultModel,
		"default_effort": acfg.DefaultEffort,
		"api_configured": apiConfigured,
		"note":           "仅使用固定直连的豆包 API；密钥只保存在 macOS 钥匙串",
		"efforts": []map[string]string{
			{"id": "low", "label": "快速"},
			{"id": "medium", "label": "均衡"},
			{"id": "high", "label": "深度思考"},
			{"id": "max", "label": "最强"},
		},
	})
}

type codingModelValidationFailure struct {
	ID        string `json:"id"`
	ErrorCode string `json:"error_code"`
}

type codingModelValidationSummary struct {
	Tested int                            `json:"tested"`
	Passed int                            `json:"passed"`
	Failed []codingModelValidationFailure `json:"failed"`
}

type codingModelsUpdateResponse struct {
	*agentcfg.Config
	KeyUpdated    bool                         `json:"key_updated"`
	APIConfigured bool                         `json:"api_configured"`
	Validation    codingModelValidationSummary `json:"validation"`
}

func (s *Server) loadAgentModelConfig(dir string) (*agentcfg.Config, bool, error) {
	cfg, err := agentcfg.Load(dir)
	if err != nil {
		return nil, false, err
	}
	keychainConfigured := strings.TrimSpace(resolveCodingLLMConfig().APIKey) != ""
	if cfg.ProviderType == agentcfg.ProviderTypeCustom {
		keychainConfigured = strings.TrimSpace(resolveCustomCodingLLMAPIKey()) != ""
	}
	if len(cfg.Models) > 0 {
		return cfg, keychainConfigured, nil
	}
	if cfg.ProviderType == agentcfg.ProviderTypeCustom {
		return cfg, keychainConfigured, nil
	}

	s.mu.Lock()
	legacyVerified := s.cfg.AgentAPIVerified
	legacyModel := strings.TrimSpace(s.cfg.AgentModel)
	legacyKey := strings.TrimSpace(s.cfg.AgentAPIKey)
	s.mu.Unlock()
	candidate, knownModel := agentcfg.CandidateModel(legacyModel)
	if !legacyVerified || !knownModel {
		return cfg, keychainConfigured, nil
	}
	if !keychainConfigured {
		if legacyKey == "" || saveCodingLLMAPIKey(legacyKey) != nil {
			return cfg, false, nil
		}
		keychainConfigured = true
	}
	candidate.Enabled = true
	candidate.ValidatedAt = time.Now().UTC().Format(time.RFC3339)
	cfg.DefaultModel = candidate.ID
	cfg.Models = []agentcfg.ModelEntry{candidate}
	if err := agentcfg.Save(dir, cfg); err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

// GET/PUT /api/agent/models — verified Volcano Ark Responses model preferences.
func (s *Server) handleAPIAgentModels(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if (r.Method == http.MethodPut || r.Method == http.MethodPost) &&
		!transportAuthenticatedRequest(r) && !sameOriginLoopbackRequest(r) {
		writeCodingModelError(w, http.StatusForbidden, "ARK_MODEL_ORIGIN_DENIED", nil)
		return
	}
	dir, _ := s.cfg.ResolvedDataDir()
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		cfg, apiConfigured, err := s.loadAgentModelConfig(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(codingModelsUpdateResponse{
			Config: cfg, APIConfigured: apiConfigured,
		})
	case http.MethodPut, http.MethodPost:
		var body struct {
			APIKey        string   `json:"api_key"`
			ProviderType  string   `json:"provider_type"`
			ProviderName  string   `json:"provider_name"`
			BaseURL       string   `json:"base_url"`
			Protocol      string   `json:"protocol"`
			Models        []string `json:"models"`
			DefaultModel  string   `json:"default_model"`
			DefaultEffort string   `json:"default_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		current, _, err := s.loadAgentModelConfig(dir)
		if err != nil {
			writeCodingModelError(w, http.StatusInternalServerError, "LLM_MODEL_CONFIG_LOAD_FAILED", nil)
			return
		}
		providerType := strings.ToLower(strings.TrimSpace(body.ProviderType))
		if providerType == "" {
			providerType = current.ProviderType
		}
		if providerType != agentcfg.ProviderTypeCustom {
			providerType = agentcfg.ProviderTypeVolcanoArk
		}
		apiKey := strings.TrimSpace(body.APIKey)
		keyUpdated := apiKey != ""
		if apiKey == "" {
			if providerType == agentcfg.ProviderTypeCustom {
				apiKey = strings.TrimSpace(resolveCustomCodingLLMAPIKey())
			} else {
				apiKey = strings.TrimSpace(resolveCodingLLMConfig().APIKey)
			}
			if apiKey == "" {
				writeCodingModelError(w, http.StatusBadRequest, "LLM_API_KEY_REQUIRED", nil)
				return
			}
		}

		var modelIDs []string
		var ok bool
		if providerType == agentcfg.ProviderTypeCustom {
			modelIDs, ok = normalizeCustomCodingModelCandidates(body.Models)
			body.BaseURL = strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
			body.ProviderName = strings.TrimSpace(body.ProviderName)
			body.Protocol = strings.ToLower(strings.TrimSpace(body.Protocol))
			if body.ProviderName == "" {
				body.ProviderName = "自定义 LLM"
			}
			if len(body.ProviderName) > 80 ||
				!validDirectAPIBaseURL(body.BaseURL) ||
				(body.Protocol != agentcfg.ProtocolResponses && body.Protocol != agentcfg.ProtocolChatCompletions) {
				ok = false
			}
		} else {
			modelIDs, ok = normalizeCodingModelCandidates(body.Models)
		}
		if !ok {
			writeCodingModelError(w, http.StatusBadRequest, "LLM_CONFIG_INVALID", nil)
			return
		}
		summary := codingModelValidationSummary{Tested: len(modelIDs)}
		passed := make([]agentcfg.ModelEntry, 0, len(modelIDs))
		for _, modelID := range modelIDs {
			var validationErr error
			if providerType == agentcfg.ProviderTypeCustom {
				validationErr = validateCustomCodingLLMModel(
					r.Context(), apiKey, body.BaseURL, body.Protocol, modelID,
				)
			} else {
				validationErr = validateCodingLLMModel(r.Context(), apiKey, modelID)
			}
			if validationErr != nil {
				summary.Failed = append(summary.Failed, codingModelValidationFailure{
					ID:        modelID,
					ErrorCode: codingModelValidationErrorCode(validationErr),
				})
				continue
			}
			candidate, known := agentcfg.CandidateModel(modelID)
			if providerType == agentcfg.ProviderTypeCustom || !known {
				candidate = agentcfg.ModelEntry{ID: modelID, Label: modelID, Source: providerType}
			}
			candidate.Enabled = true
			candidate.ValidatedAt = time.Now().UTC().Format(time.RFC3339)
			passed = append(passed, candidate)
		}
		summary.Passed = len(passed)
		if len(passed) == 0 {
			writeCodingModelError(w, http.StatusUnprocessableEntity, "LLM_MODEL_VALIDATION_FAILED", &summary)
			return
		}
		// The one-time key remains in memory during validation and reaches
		// persistent storage only after at least one real provider call passes.
		if keyUpdated {
			var saveErr error
			if providerType == agentcfg.ProviderTypeCustom {
				saveErr = saveCustomCodingLLMAPIKey(apiKey)
			} else {
				saveErr = saveCodingLLMAPIKey(apiKey)
			}
			if saveErr != nil {
				writeCodingModelError(w, http.StatusBadRequest, "LLM_KEYCHAIN_SAVE_FAILED", &summary)
				return
			}
		}
		effort := normalizeCodingEffort(body.DefaultEffort)
		cfg := &agentcfg.Config{
			ProviderType:  providerType,
			ProviderName:  body.ProviderName,
			BaseURL:       body.BaseURL,
			Protocol:      body.Protocol,
			Engine:        "doubao_api",
			DefaultModel:  body.DefaultModel,
			DefaultEffort: effort,
			Models:        passed,
		}
		if err := agentcfg.Save(dir, cfg); err != nil {
			writeCodingModelError(w, http.StatusInternalServerError, "LLM_MODEL_CONFIG_SAVE_FAILED", &summary)
			return
		}
		if s.agentHub != nil {
			_ = s.agentHub.BeginProbe(agenthub.ProviderCustom)
		}
		_ = json.NewEncoder(w).Encode(codingModelsUpdateResponse{
			Config: cfg, KeyUpdated: keyUpdated, APIConfigured: true, Validation: summary,
		})
	case http.MethodPatch:
		// Kept as a harmless compatibility refresh; there is no provider discovery.
		cfg, err := agentcfg.Load(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(cfg)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func normalizeCustomCodingModelCandidates(requested []string) ([]string, bool) {
	if len(requested) == 0 || len(requested) > 32 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(requested))
	normalized := make([]string, 0, len(requested))
	for _, id := range requested {
		id = strings.TrimSpace(id)
		if id == "" || agenthub.ValidateModelID(id) != nil || len(id) > 96 {
			return nil, false
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized, len(normalized) > 0
}

func (s *Server) openCodeLLMConfig(dataDir string) agenthub.OpenCodeLLMConfig {
	cfg, err := agentcfg.Load(dataDir)
	if err == nil && len(cfg.Models) > 0 {
		apiKey := strings.TrimSpace(resolveCodingLLMConfig().APIKey)
		baseURL := llm.DoubaoBaseURL
		name := "火山方舟"
		providerPackage := "@ai-sdk/openai"
		if cfg.ProviderType == agentcfg.ProviderTypeCustom {
			apiKey = strings.TrimSpace(resolveCustomCodingLLMAPIKey())
			baseURL = cfg.BaseURL
			name = cfg.ProviderName
			if cfg.Protocol == agentcfg.ProtocolChatCompletions {
				providerPackage = "@ai-sdk/openai-compatible"
			}
		}
		models := make(map[string]string, len(cfg.Models))
		for _, model := range cfg.Models {
			models[agenthub.OpenCodeLLMModelID(model.ID)] = model.ID
		}
		return agenthub.OpenCodeLLMConfig{
			ProviderID: "alpha-nexus-llm",
			Name:       name,
			BaseURL:    baseURL,
			Package:    providerPackage,
			APIKey:     apiKey,
			Models:     models,
		}
	}

	s.mu.Lock()
	verified := s.cfg.AgentAPIVerified
	name := strings.TrimSpace(s.cfg.AgentAPIProvider)
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.AgentAPIBaseURL), "/")
	apiKey := strings.TrimSpace(s.cfg.AgentAPIKey)
	model := strings.TrimSpace(s.cfg.AgentModel)
	s.mu.Unlock()
	if !verified || !validDirectAPIBaseURL(baseURL) || apiKey == "" || agenthub.ValidateModelID(model) != nil || model == "" {
		return agenthub.OpenCodeLLMConfig{}
	}
	if name == "" {
		name = "自定义 LLM"
	}
	return agenthub.OpenCodeLLMConfig{
		ProviderID: "alpha-nexus-llm",
		Name:       name,
		BaseURL:    baseURL,
		Package:    "@ai-sdk/openai-compatible",
		APIKey:     apiKey,
		Models: map[string]string{
			agenthub.OpenCodeLLMModelID(model): model,
		},
	}
}

func normalizeCodingModelCandidates(requested []string) ([]string, bool) {
	if len(requested) == 0 {
		return agentcfg.CandidateModelIDs(), true
	}
	seen := make(map[string]struct{}, len(requested))
	normalized := make([]string, 0, len(requested))
	for _, id := range requested {
		candidate, ok := agentcfg.CandidateModel(id)
		if !ok {
			return nil, false
		}
		if _, exists := seen[candidate.ID]; exists {
			continue
		}
		seen[candidate.ID] = struct{}{}
		normalized = append(normalized, candidate.ID)
	}
	if len(normalized) == 0 {
		return nil, false
	}
	return normalized, true
}

func normalizeCodingEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "max":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "medium"
	}
}

func codingModelValidationErrorCode(err error) string {
	var responseErr *llm.ArkResponsesError
	if !errors.As(err, &responseErr) {
		return "ARK_MODEL_UNAVAILABLE"
	}
	switch {
	case responseErr.StatusCode == http.StatusUnauthorized || responseErr.StatusCode == http.StatusForbidden:
		return "ARK_AUTH_FAILED"
	case responseErr.StatusCode == http.StatusNotFound:
		return "ARK_MODEL_UNAVAILABLE"
	case responseErr.StatusCode == http.StatusTooManyRequests:
		return "ARK_RATE_LIMITED"
	case responseErr.Code == llm.ArkResponsesTransportFailed:
		return "ARK_SERVICE_UNAVAILABLE"
	case responseErr.Code == llm.ArkResponsesInvalidResponse || responseErr.Code == llm.ArkResponsesEmptyOutput:
		return "ARK_INVALID_RESPONSE"
	case responseErr.Code == llm.ArkResponsesInvalidRequest:
		return "ARK_MODEL_INVALID"
	default:
		return "ARK_MODEL_UNAVAILABLE"
	}
}

func writeCodingModelError(w http.ResponseWriter, status int, code string, summary *codingModelValidationSummary) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]any{"ok": false, "error_code": code}
	if summary != nil {
		payload["validation"] = summary
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// GET/POST /api/coding/sessions  |  GET/DELETE /api/coding/sessions?id=
func (s *Server) handleAPICodingSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	dir, _ := s.cfg.ResolvedDataDir()
	_ = terminal.EnsureSessionLayout(dir)
	w.Header().Set("Content-Type", "application/json")

	id := strings.TrimSpace(r.URL.Query().Get("id"))

	switch r.Method {
	case http.MethodGet:
		if id != "" {
			sess, err := terminal.LoadChatSession(dir, id)
			if err != nil {
				http.Error(w, "not found", 404)
				return
			}
			_ = json.NewEncoder(w).Encode(sess)
			return
		}
		list, err := terminal.ListChatSessions(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": list})
	case http.MethodPost:
		var body struct {
			Title  string `json:"title"`
			Engine string `json:"engine"`
			Model  string `json:"model"`
			Effort string `json:"effort"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sess, err := terminal.CreateChatSession(dir, body.Title, body.Engine, body.Model, body.Effort)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(sess)
	case http.MethodDelete:
		if id == "" {
			http.Error(w, "id required", 400)
			return
		}
		if err := terminal.DeleteChatSession(dir, id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /api/coding/upload  multipart: session_id + file
func (s *Server) handleAPICodingUpload(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "bad multipart", 400)
		return
	}
	sessionID := strings.TrimSpace(r.FormValue("session_id"))
	if sessionID == "" {
		http.Error(w, "session_id required", 400)
		return
	}
	dir, _ := s.cfg.ResolvedDataDir()
	if _, err := terminal.LoadChatSession(dir, sessionID); err != nil {
		http.Error(w, "session not found", 404)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", 400)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	abs, rel, err := terminal.SaveSessionAttach(dir, sessionID, hdr.Filename, data)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "path": abs, "rel": rel, "name": hdr.Filename, "size": len(data),
	})
}

// GET /api/coding/works — list batch works folders
// POST multipart to works/_inbox or named folder
func (s *Server) handleAPICodingWorks(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	dir, _ := s.cfg.ResolvedDataDir()
	_ = terminal.EnsureSessionLayout(dir)
	works := agentws.WorksPath(dir)

	switch r.Method {
	case http.MethodGet:
		entries, _ := os.ReadDir(works)
		type item struct {
			Name  string   `json:"name"`
			Path  string   `json:"path"`
			Files []string `json:"files"`
		}
		var list []item
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(works, e.Name())
			var files []string
			_ = filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(p, path)
				files = append(files, rel)
				return nil
			})
			list = append(list, item{Name: e.Name(), Path: p, Files: files})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"works_dir": works,
			"items":     list,
			"note":      "每个子文件夹 = 一条作品（video + caption.txt）",
		})
	case http.MethodPost:
		if err := r.ParseMultipartForm(256 << 20); err != nil {
			http.Error(w, "bad multipart", 400)
			return
		}
		folder := strings.TrimSpace(r.FormValue("folder"))
		if folder == "" {
			folder = "_inbox"
		}
		folder = filepath.Base(folder)
		destDir := filepath.Join(works, folder)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var saved []string
		for _, fhs := range r.MultipartForm.File["files"] {
			f, err := fhs.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(io.LimitReader(f, 256<<20))
			_ = f.Close()
			name := filepath.Base(fhs.Filename)
			target := filepath.Join(destDir, name)
			if err := os.WriteFile(target, data, 0o644); err == nil {
				saved = append(saved, target)
			}
		}
		// also accept single "file"
		if len(saved) == 0 {
			file, hdr, err := r.FormFile("file")
			if err == nil {
				defer file.Close()
				data, _ := io.ReadAll(io.LimitReader(file, 256<<20))
				target := filepath.Join(destDir, filepath.Base(hdr.Filename))
				if err := os.WriteFile(target, data, 0o644); err == nil {
					saved = append(saved, target)
				}
			}
		}
		indexedMedia := 0
		for _, path := range saved {
			validated, err := media.ValidateLocalPath(path)
			if err != nil {
				continue // caption/text files remain valid works inputs, but are not Studio media.
			}
			meta, err := media.Probe(validated)
			if err != nil {
				continue
			}
			item, err := s.db.UpsertMediaLibrary(meta.Path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON)
			if err != nil {
				continue
			}
			indexedMedia++
			_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "studio", "media indexed from works upload", "media_library", item.ID, "")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "folder": destDir, "saved": saved, "count": len(saved), "indexed_media": indexedMedia,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /api/browser/ai-chat
func (s *Server) handleAPIBrowserAIChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AccountID int64  `json:"account_id"`
		CDPURL    string `json:"cdp_url"`
		Provider  string `json:"provider"`
		URL       string `json:"url"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.CDPURL) != "" {
		http.Error(w, "CDP_URL_NOT_ALLOWED", http.StatusBadRequest)
		return
	}
	if body.AccountID <= 0 {
		http.Error(w, "account_id required", http.StatusBadRequest)
		return
	}
	if s.facade == nil {
		http.Error(w, "browser facade is unavailable", http.StatusInternalServerError)
		return
	}
	if _, err := s.db.GetAccount(body.AccountID); err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	release, err := s.acquireBrowserOperationLease(body.AccountID, "ai_chat", uiBrowserAIChatLeaseTTL)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	defer release()
	status, statusErr := s.facade.Status(body.AccountID)
	cdp, cdpErr := publishguard.LiveCDPURL(body.AccountID, status, statusErr)
	if cdpErr != nil {
		http.Error(w, cdpErr.Error(), http.StatusConflict)
		return
	}
	res, err := runAIBrowserChat(r.Context(), cdp, body.Provider, body.URL, body.Message)
	w.Header().Set("Content-Type", "application/json")
	if res == nil {
		w.WriteHeader(500)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": errString(err)})
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func runAIBrowserChat(ctx context.Context, cdp, provider, url, message string) (*agents.AIChatResult, error) {
	p := agents.AIChatProvider(strings.ToLower(strings.TrimSpace(provider)))
	if p == "" {
		p = agents.AIChatChatGPT
	}
	return agents.AIBrowserChat(ctx, agents.AIChatRequest{
		CDPURL:   cdp,
		Provider: p,
		URL:      url,
		Message:  message,
	})
}
