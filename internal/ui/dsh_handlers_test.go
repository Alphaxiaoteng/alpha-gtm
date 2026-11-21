package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestDshCatalogRequiresLoopback(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/dsh-catalog", nil)
	request.RemoteAddr = "203.0.113.9:45000"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDshCatalogShapeIsStable(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	recorder := callAIImageAPI(t, server, http.MethodGet, "/api/agent/dsh-catalog", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// The shape must stay stable even when ~/.dsh is absent, so the settings
	// page can render an honest "0 providers" state instead of failing.
	for _, key := range []string{"default_provider", "default_model", "providers", "model_ids"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing key %q in %s", key, recorder.Body.String())
		}
	}
}
