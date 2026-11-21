package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestAgentAPIConfigIsSavedOnlyAfterTheSelectedModelReplies(t *testing.T) {
	var providerStatus = http.StatusUnauthorized
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if providerStatus != http.StatusOK {
			http.Error(w, "synthetic failure", providerStatus)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "verified-model" {
			http.Error(w, "wrong model", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer provider.Close()

	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	body := map[string]string{
		"agent_api_provider": "custom",
		"agent_api_base_url": provider.URL,
		"agent_api_key":      "synthetic-key",
		"agent_model":        "verified-model",
	}
	raw, _ := json.Marshal(body)
	failed := httptest.NewRecorder()
	server.handleAPIAgentConfig(failed, httptest.NewRequest(http.MethodPost, "/api/ai/config/agent", bytes.NewReader(raw)))
	if failed.Code != http.StatusUnprocessableEntity {
		t.Fatalf("failed save status=%d body=%s", failed.Code, failed.Body.String())
	}
	if server.cfg.AgentModel != "" || server.cfg.AgentAPIVerified {
		t.Fatalf("failed model was saved: model=%q verified=%v", server.cfg.AgentModel, server.cfg.AgentAPIVerified)
	}

	providerStatus = http.StatusOK
	saved := httptest.NewRecorder()
	server.handleAPIAgentConfig(saved, httptest.NewRequest(http.MethodPost, "/api/ai/config/agent", bytes.NewReader(raw)))
	if saved.Code != http.StatusOK {
		t.Fatalf("verified save status=%d body=%s", saved.Code, saved.Body.String())
	}
	if server.cfg.AgentModel != "verified-model" || !server.cfg.AgentAPIVerified {
		t.Fatalf("verified model not saved: model=%q verified=%v", server.cfg.AgentModel, server.cfg.AgentAPIVerified)
	}
}

func TestAgentAITestDoesNotTreatHTTP404AsSuccess(t *testing.T) {
	provider := httptest.NewServer(http.NotFoundHandler())
	defer provider.Close()
	server := &Server{cfg: config.Config{}}
	body, _ := json.Marshal(map[string]string{
		"category": "agent",
		"provider": "custom",
		"base_url": provider.URL,
		"api_key":  "synthetic-key",
		"model":    "missing-model",
	})
	recorder := httptest.NewRecorder()
	server.handleAPIAITest(recorder, httptest.NewRequest(http.MethodPost, "/api/ai/test", bytes.NewReader(body)))
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"ok":false`)) {
		t.Fatalf("404 was accepted: %s", recorder.Body.String())
	}
}

func TestAIConfigUpdateDoesNotClearVerifiedFallbackWhenCredentialIsOmitted(t *testing.T) {
	server := &Server{cfg: config.Config{
		DataDir: t.TempDir(), AgentAPIProvider: "custom",
		AgentAPIBaseURL: "https://example.invalid/v1", AgentAPIKey: "existing-key",
		AgentModel: "verified-model", AgentAPIVerified: true,
	}}
	raw, _ := json.Marshal(AIConfigPayload{
		AgentAPIProvider: "custom", AgentAPIBaseURL: "https://example.invalid/v1",
		AgentModel: "verified-model",
	})
	recorder := httptest.NewRecorder()
	server.handleAPIAIConfig(recorder, httptest.NewRequest(http.MethodPost, "/api/ai/config", bytes.NewReader(raw)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if server.cfg.AgentAPIKey != "existing-key" || !server.cfg.AgentAPIVerified {
		t.Fatalf("fallback credential was cleared or invalidated")
	}
}

func TestAIConfigReportsSecureMediaCredentialWithoutEchoingKey(t *testing.T) {
	server := &Server{
		cfg: config.Config{
			ImageAPIProvider: "Agnes AI",
			VideoAPIProvider: "Agnes AI",
		},
		agnesAPIKeyConfigured: func() (bool, error) { return true, nil },
	}
	recorder := httptest.NewRecorder()
	server.handleAPIAIConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/ai/config", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		ImageConfigured bool   `json:"image_api_configured"`
		VideoConfigured bool   `json:"video_api_configured"`
		ImageAPIKey     string `json:"image_api_key"`
		VideoAPIKey     string `json:"video_api_key"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.ImageConfigured || !payload.VideoConfigured {
		t.Fatalf("configured image=%v video=%v", payload.ImageConfigured, payload.VideoConfigured)
	}
	if payload.ImageAPIKey != "" || payload.VideoAPIKey != "" {
		t.Fatal("secure-store credentials must never be echoed")
	}
}

func TestAIConfigMigratesLegacyAgnesCredentialToSecureStore(t *testing.T) {
	saved := ""
	server := &Server{
		cfg: config.Config{
			DataDir:          t.TempDir(),
			MediaAPIProvider: "Agnes AI",
			MediaAPIKey:      "synthetic-legacy-agnes-key",
		},
		agnesAPIKeyConfigured: func() (bool, error) { return false, nil },
		agnesSaveAPIKey: func(value string) error {
			saved = value
			return nil
		},
	}
	recorder := httptest.NewRecorder()
	server.handleAPIAIConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/ai/config", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if saved != "synthetic-legacy-agnes-key" {
		t.Fatal("legacy Agnes credential was not migrated")
	}
	var payload struct {
		ImageConfigured bool `json:"image_api_configured"`
		VideoConfigured bool `json:"video_api_configured"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.ImageConfigured || !payload.VideoConfigured {
		t.Fatalf("configured image=%v video=%v", payload.ImageConfigured, payload.VideoConfigured)
	}
}
