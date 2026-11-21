package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/agentcfg"
	"alpha-nexus-egolite/internal/agents"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/terminal"
)

func TestCodingStatusDoesNotExposeUnverifiedModels(t *testing.T) {
	previous := resolveCodingLLMConfig
	resolveCodingLLMConfig = func() llm.Config { return llm.Config{Source: "keychain-missing"} }
	t.Cleanup(func() { resolveCodingLLMConfig = previous })
	dataDir := t.TempDir()
	if err := agentcfg.Save(dataDir, &agentcfg.Config{
		Models: []agentcfg.ModelEntry{{ID: "proxy/example", Label: "Example", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/coding/status", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Model         string `json:"model"`
		APIConfigured bool   `json:"api_configured"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "" {
		t.Fatalf("model=%q", payload.Model)
	}
	if strings.Contains(recorder.Body.String(), "model_list") || strings.Contains(recorder.Body.String(), `"models"`) {
		t.Fatalf("coding status must not expose a model list: %s", recorder.Body.String())
	}
	if payload.APIConfigured {
		t.Fatalf("api_configured=%v", payload.APIConfigured)
	}
}

func TestCodingStatusReportsConfiguredDoubaoWithoutSecrets(t *testing.T) {
	previous := resolveCodingLLMConfig
	resolveCodingLLMConfig = func() llm.Config {
		return llm.Config{BaseURL: llm.DoubaoBaseURL, APIKey: "synthetic-key", Model: llm.DoubaoModelID, Source: "keychain"}
	}
	t.Cleanup(func() { resolveCodingLLMConfig = previous })
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/coding/status", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["api_configured"] != true {
		t.Fatalf("api_configured=%v", payload["api_configured"])
	}
	encoded := recorder.Body.String()
	if strings.Contains(encoded, "synthetic-key") || strings.Contains(encoded, llm.DoubaoBaseURL) || strings.Contains(encoded, "keychain") {
		t.Fatalf("status leaked configuration: %s", encoded)
	}
}

func TestAgentModelsAPIReportsKeychainConfigurationWithoutEchoingCredential(t *testing.T) {
	previous := resolveCodingLLMConfig
	resolveCodingLLMConfig = func() llm.Config {
		return llm.Config{APIKey: "synthetic-existing-key", Source: "keychain"}
	}
	t.Cleanup(func() { resolveCodingLLMConfig = previous })
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/models", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		APIConfigured bool `json:"api_configured"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.APIConfigured {
		t.Fatal("Keychain-backed credential was reported as unconfigured")
	}
	if strings.Contains(recorder.Body.String(), "synthetic-existing-key") {
		t.Fatal("Agent model status echoed a credential")
	}
}

func TestAgentModelsAPIMigratesLegacyVerifiedDoubaoModel(t *testing.T) {
	previousResolve := resolveCodingLLMConfig
	previousSave := saveCodingLLMAPIKey
	resolveCodingLLMConfig = func() llm.Config { return llm.Config{} }
	savedKey := ""
	saveCodingLLMAPIKey = func(value string) error {
		savedKey = value
		return nil
	}
	t.Cleanup(func() {
		resolveCodingLLMConfig = previousResolve
		saveCodingLLMAPIKey = previousSave
	})
	server, err := NewServer(config.Config{
		DataDir:          t.TempDir(),
		UIPort:           0,
		AgentAPIKey:      "synthetic-legacy-key",
		AgentModel:       agentcfg.DoubaoModelID,
		AgentAPIVerified: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/models", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		APIConfigured bool                  `json:"api_configured"`
		Models        []agentcfg.ModelEntry `json:"models"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if savedKey != "synthetic-legacy-key" {
		t.Fatal("legacy verified credential was not migrated to secure storage")
	}
	if !payload.APIConfigured || len(payload.Models) != 1 || payload.Models[0].ID != agentcfg.DoubaoModelID {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestAgentModelsAPISavesOnlyModelsThatPassArkResponsesValidation(t *testing.T) {
	previousSave := saveCodingLLMAPIKey
	previousValidate := validateCodingLLMModel
	events := make([]string, 0, 5)
	saveCodingLLMAPIKey = func(key string) error {
		events = append(events, "save:"+key)
		return nil
	}
	validateCodingLLMModel = func(_ context.Context, key, model string) error {
		events = append(events, "validate:"+model)
		if key != "synthetic-new-key" {
			t.Fatalf("validator key=%q", key)
		}
		switch model {
		case agentcfg.DoubaoSeed21TurboModelID, agentcfg.DoubaoModelID:
			return nil
		default:
			return errors.New("provider detail that must not leak")
		}
	}
	t.Cleanup(func() {
		saveCodingLLMAPIKey = previousSave
		validateCodingLLMModel = previousValidate
	})

	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	const key = "synthetic-new-key"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/agent/models", strings.NewReader(`{
		"api_key":"`+key+`",
		"default_model":"`+agentcfg.DoubaoModelID+`",
		"default_effort":"high"
	}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(events) != 5 || events[4] != "save:"+key {
		t.Fatalf("validation must finish before Keychain save: events=%q", events)
	}
	if strings.Contains(recorder.Body.String(), key) {
		t.Fatalf("response leaked key: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "provider detail") {
		t.Fatalf("response leaked provider error: %s", recorder.Body.String())
	}
	var payload struct {
		agentcfg.Config
		KeyUpdated bool `json:"key_updated"`
		Validation struct {
			Tested int `json:"tested"`
			Passed int `json:"passed"`
			Failed []struct {
				ID        string `json:"id"`
				ErrorCode string `json:"error_code"`
			} `json:"failed"`
		} `json:"validation"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.KeyUpdated || payload.Validation.Tested != 4 || payload.Validation.Passed != 2 || len(payload.Validation.Failed) != 2 {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Engine != "doubao_api" || payload.DefaultModel != agentcfg.DoubaoModelID || payload.DefaultEffort != "high" {
		t.Fatalf("config=%+v", payload.Config)
	}
	if len(payload.Models) != 2 || payload.Models[0].ID != agentcfg.DoubaoSeed21TurboModelID || payload.Models[1].ID != agentcfg.DoubaoModelID {
		t.Fatalf("enabled models=%+v", payload.Models)
	}
	loaded, err := agentcfg.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 2 || loaded.Models[0].ID != agentcfg.DoubaoSeed21TurboModelID || loaded.Models[1].ID != agentcfg.DoubaoModelID {
		t.Fatalf("persisted=%+v", loaded)
	}
}

func TestAgentModelsAPIDoesNotSaveKeyOrConfigWhenEveryModelFails(t *testing.T) {
	previousSave := saveCodingLLMAPIKey
	previousValidate := validateCodingLLMModel
	saved := false
	saveCodingLLMAPIKey = func(string) error {
		saved = true
		return nil
	}
	validateCodingLLMModel = func(context.Context, string, string) error {
		return errors.New("synthetic provider failure")
	}
	t.Cleanup(func() {
		saveCodingLLMAPIKey = previousSave
		validateCodingLLMModel = previousValidate
	})

	dataDir := t.TempDir()
	before := &agentcfg.Config{
		DefaultModel: agentcfg.DoubaoModelID,
		Models: []agentcfg.ModelEntry{{
			ID: agentcfg.DoubaoModelID, Enabled: true, ValidatedAt: "2026-07-26T10:00:00Z",
		}},
	}
	if err := agentcfg.Save(dataDir, before); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/agent/models", strings.NewReader(`{
		"api_key":"synthetic-new-key",
		"models":["`+agentcfg.DoubaoSeed21ProModelID+`"]
	}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if saved {
		t.Fatal("failed validation must not save the Keychain credential")
	}
	if strings.Contains(recorder.Body.String(), "synthetic") {
		t.Fatalf("response leaked protected data: %s", recorder.Body.String())
	}
	after, err := agentcfg.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Models) != 1 || after.Models[0].ID != agentcfg.DoubaoModelID {
		t.Fatalf("existing config changed after failed validation: %+v", after)
	}
}

func TestAgentModelsAPISavesVerifiedCustomProviderForOpenCode(t *testing.T) {
	previousResolve := resolveCustomCodingLLMAPIKey
	previousSave := saveCustomCodingLLMAPIKey
	previousValidate := validateCustomCodingLLMModel
	var savedKey string
	resolveCustomCodingLLMAPIKey = func() string { return savedKey }
	saveCustomCodingLLMAPIKey = func(value string) error {
		savedKey = value
		return nil
	}
	validateCustomCodingLLMModel = func(_ context.Context, key, baseURL, protocol, model string) error {
		if key != "synthetic-custom-key" ||
			baseURL != "https://gateway.example.test/v1" ||
			protocol != agentcfg.ProtocolChatCompletions ||
			model != "vendor/coder-model" {
			t.Fatalf("custom validation=%q %q %q %q", key, baseURL, protocol, model)
		}
		return nil
	}
	t.Cleanup(func() {
		resolveCustomCodingLLMAPIKey = previousResolve
		saveCustomCodingLLMAPIKey = previousSave
		validateCustomCodingLLMModel = previousValidate
	})

	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	server.agentHub = nil

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/agent/models", strings.NewReader(`{
		"provider_type":"custom",
		"provider_name":"My Gateway",
		"base_url":"https://gateway.example.test/v1",
		"protocol":"chat_completions",
		"api_key":"synthetic-custom-key",
		"models":["vendor/coder-model"],
		"default_model":"vendor/coder-model",
		"default_effort":"high"
	}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "synthetic-custom-key") {
		t.Fatalf("response leaked key: %s", recorder.Body.String())
	}
	loaded, err := agentcfg.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProviderType != agentcfg.ProviderTypeCustom ||
		loaded.ProviderName != "My Gateway" ||
		loaded.BaseURL != "https://gateway.example.test/v1" ||
		loaded.Protocol != agentcfg.ProtocolChatCompletions ||
		len(loaded.Models) != 1 ||
		loaded.Models[0].ID != "vendor/coder-model" {
		t.Fatalf("custom config=%+v", loaded)
	}
}

func TestAgentModelsAPIReusesExistingKeychainCredentialWithoutEchoOrRewrite(t *testing.T) {
	previousResolve := resolveCodingLLMConfig
	previousSave := saveCodingLLMAPIKey
	previousValidate := validateCodingLLMModel
	resolveCodingLLMConfig = func() llm.Config {
		return llm.Config{APIKey: "synthetic-existing-key", Source: "keychain"}
	}
	saved := false
	saveCodingLLMAPIKey = func(string) error {
		saved = true
		return nil
	}
	validateCodingLLMModel = func(_ context.Context, key, model string) error {
		if key != "synthetic-existing-key" || model != agentcfg.DoubaoModelID {
			t.Fatalf("validation key/model=%q/%q", key, model)
		}
		return nil
	}
	t.Cleanup(func() {
		resolveCodingLLMConfig = previousResolve
		saveCodingLLMAPIKey = previousSave
		validateCodingLLMModel = previousValidate
	})

	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/agent/models", strings.NewReader(`{
		"models":["`+agentcfg.DoubaoModelID+`"]
	}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if saved {
		t.Fatal("an unchanged Keychain credential must not be rewritten")
	}
	if strings.Contains(recorder.Body.String(), "synthetic-existing-key") {
		t.Fatalf("response leaked key: %s", recorder.Body.String())
	}
	var payload codingModelsUpdateResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.KeyUpdated || len(payload.Models) != 1 || payload.Models[0].ID != agentcfg.DoubaoModelID {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestAgentModelsAPIRejectsUnknownModelsBeforeCallingProvider(t *testing.T) {
	previousValidate := validateCodingLLMModel
	called := false
	validateCodingLLMModel = func(context.Context, string, string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { validateCodingLLMModel = previousValidate })
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/agent/models", strings.NewReader(`{
		"api_key":"synthetic-key",
		"models":["invented/model"]
	}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatal("unknown model must not reach provider validation")
	}
}

func TestAgentModelsAPIMutationsRejectCrossSiteOriginsBeforeReadingCredentials(t *testing.T) {
	previousResolve := resolveCodingLLMConfig
	previousValidate := validateCodingLLMModel
	resolved := false
	validated := false
	resolveCodingLLMConfig = func() llm.Config {
		resolved = true
		return llm.Config{APIKey: "must-not-be-read"}
	}
	validateCodingLLMModel = func(context.Context, string, string) error {
		validated = true
		return nil
	}
	t.Cleanup(func() {
		resolveCodingLLMConfig = previousResolve
		validateCodingLLMModel = previousValidate
	})

	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, "/api/agent/models", strings.NewReader(`{
			"models":["`+agentcfg.DoubaoModelID+`"]
		}`))
		request.RemoteAddr = "127.0.0.1:41000"
		request.Host = "127.0.0.1:17321"
		request.Header.Set("Origin", "https://attacker.invalid")
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("method=%s status=%d body=%s", method, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "ARK_MODEL_ORIGIN_DENIED") {
			t.Fatalf("method=%s body=%s", method, recorder.Body.String())
		}
	}
	if resolved || validated {
		t.Fatalf("cross-site request reached credentials/provider: resolved=%v validated=%v", resolved, validated)
	}
}

func TestAgentModelsAPIAllowsTransportAuthenticatedElectronDevOrigin(t *testing.T) {
	previousSave := saveCodingLLMAPIKey
	previousValidate := validateCodingLLMModel
	saveCodingLLMAPIKey = func(string) error { return nil }
	validateCodingLLMModel = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() {
		saveCodingLLMAPIKey = previousSave
		validateCodingLLMModel = previousValidate
	})

	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	token := strings.Repeat("a", 64)
	handler := server.withTransportAuth(server.mux, token)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/agent/models", strings.NewReader(`{
		"api_key":"synthetic-new-key",
		"models":["`+agentcfg.DoubaoModelID+`"]
	}`))
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:43210"
	request.Header.Set("Origin", "http://127.0.0.1:5176")
	request.Header.Set(TransportTokenHeader, token)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCodingChatToolsModePersistsGuardedToolResult(t *testing.T) {
	dataDir := t.TempDir()
	if err := agentcfg.Save(dataDir, &agentcfg.Config{
		DefaultModel: agentcfg.DoubaoModelID,
		Models: []agentcfg.ModelEntry{{
			ID:          agentcfg.DoubaoModelID,
			Enabled:     true,
			ValidatedAt: "2026-07-26T10:00:00Z",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	session, err := terminal.CreateChatSession(dataDir, "", "doubao_api", agentcfg.DoubaoModelID, "medium")
	if err != nil {
		t.Fatal(err)
	}
	attachment, _, err := terminal.SaveSessionAttach(dataDir, session.ID, "sample.png", []byte("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"session_id": session.ID,
		"mode":       "tools",
		"message":    "整理这个素材",
		"model":      "local/",
		"files":      []string{attachment},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/coding/chat", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:17321"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		OK     bool   `json:"ok"`
		Engine string `json:"engine"`
		Error  string `json:"error"`
		Reply  string `json:"reply"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.OK || payload.Engine != "llm_tools" || payload.Error != "LLM_GATEWAY_UNAVAILABLE" || payload.Reply == "" {
		t.Fatalf("payload=%+v", payload)
	}
	persisted, err := terminal.LoadChatSession(dataDir, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Messages) != 2 || persisted.Messages[0].Role != "user" || len(persisted.Messages[0].Files) != 1 || persisted.Messages[0].Files[0] != attachment || persisted.Messages[1].Role != "assistant" || persisted.Messages[1].Engine != "llm_tools" || !persisted.Messages[1].Error || persisted.Messages[1].Text != payload.Reply {
		t.Fatalf("messages=%+v", persisted.Messages)
	}
}

func TestCodingToolAttachmentPathsOnlyAllowCurrentSessionUploads(t *testing.T) {
	dataDir := t.TempDir()
	session, err := terminal.CreateChatSession(dataDir, "", "opencode", "", "")
	if err != nil {
		t.Fatal(err)
	}
	attachment, _, err := terminal.SaveSessionAttach(dataDir, session.ID, "sample.png", []byte("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := codingToolAttachmentPaths(session, []string{attachment, attachment})
	if err != nil || len(allowed) != 1 {
		t.Fatalf("allowed=%q err=%v", allowed, err)
	}
	if goal := codingToolGoal("导入附件", allowed); goal == "导入附件" || !containsAll(goal, allowed[0], "media.import") {
		t.Fatalf("goal=%q", goal)
	}

	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := codingToolAttachmentPaths(session, []string{outside}); err == nil {
		t.Fatal("outside path must be rejected")
	}
	if err := os.Symlink(outside, filepath.Join(session.Dir, "attaches", "escape.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := codingToolAttachmentPaths(session, []string{filepath.Join(session.Dir, "attaches", "escape.png")}); err == nil {
		t.Fatal("symlink escaping session attachments must be rejected")
	}
}

func TestCodingWorksUploadIndexesOnlyMediaInStudio(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	if err := writer.WriteField("folder", "launch"); err != nil {
		t.Fatal(err)
	}
	video, err := writer.CreateFormFile("files", "launch.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := video.Write([]byte("local-video-fixture")); err != nil {
		t.Fatal(err)
	}
	caption, err := writer.CreateFormFile("files", "caption.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caption.Write([]byte("本地发布文案")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/coding/works", &form)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Count        int      `json:"count"`
		IndexedMedia int      `json:"indexed_media"`
		Saved        []string `json:"saved"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 2 || payload.IndexedMedia != 1 || len(payload.Saved) != 2 {
		t.Fatalf("payload=%+v", payload)
	}
	items, err := server.db.ListMediaLibrary(10)
	if err != nil || len(items) != 1 || filepath.Base(items[0].FilePath) != "launch.mp4" || items[0].Mime != "video/mp4" {
		t.Fatalf("media=%+v err=%v", items, err)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestCodingToolsReplyUsesChineseStatusAndStepSummary(t *testing.T) {
	got := codingToolsReply(&agents.SchemeRunResult{
		Message: "all steps ok",
		Steps: []map[string]interface{}{
			{"tool": "account.list", "ok": true},
			{"tool": "publish.run", "executed": false},
		},
	})
	want := "本地工具已完成\naccount.list：已完成\npublish.run：未执行（需单独确认）"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestCodingModelUsesOnlySavedEnabledModels(t *testing.T) {
	cfg := &agentcfg.Config{
		DefaultModel: agentcfg.DoubaoSeed21ProModelID,
		Models: []agentcfg.ModelEntry{
			{ID: agentcfg.DoubaoSeed21ProModelID, Enabled: true, ValidatedAt: "2026-07-26T10:00:00Z"},
			{ID: agentcfg.DoubaoSeed21TurboModelID, Enabled: true, ValidatedAt: "2026-07-26T10:00:00Z"},
		},
	}
	if got := codingModelForMode("chat", agentcfg.DoubaoSeed21TurboModelID, cfg); got != agentcfg.DoubaoSeed21TurboModelID {
		t.Fatalf("enabled request=%q", got)
	}
	if got := codingModelForMode("chat", "invented/model", cfg); got != agentcfg.DoubaoSeed21ProModelID {
		t.Fatalf("unknown request=%q", got)
	}
	if got := codingModelForMode("chat", "", cfg); got != agentcfg.DoubaoSeed21ProModelID {
		t.Fatalf("default=%q", got)
	}
	if got := codingModelForMode("chat", agentcfg.DoubaoModelID, &agentcfg.Config{}); got != "" {
		t.Fatalf("unverified fallback=%q", got)
	}
}
