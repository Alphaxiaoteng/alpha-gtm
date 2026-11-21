package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestConfigUpdateRemovesLegacyScheduledCollection(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "auto_collect": true,
  "collect_interval_seconds": 3600,
  "proxy_mode": "api",
  "clash_start_port": 10801,
  "proxy_api_url": "https://example.invalid/proxies"
}`), 0600); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/config/update", bytes.NewBufferString(`{"browserPath":""}`))
	recorder := httptest.NewRecorder()
	server.handleAPIConfigUpdate(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var persisted map[string]any
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"auto_collect", "collect_interval_seconds"} {
		if _, exists := persisted[key]; exists {
			t.Fatalf("legacy scheduled setting %q was retained: %#v", key, persisted)
		}
	}
	for _, key := range []string{"proxy_mode", "clash_start_port", "proxy_api_url"} {
		if _, exists := persisted[key]; !exists {
			t.Fatalf("inert legacy proxy setting %q was destructively removed: %#v", key, persisted)
		}
	}
}

func TestServerDoesNotStartBackgroundLoginWatchers(t *testing.T) {
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"monitorBrowserTabs",
		"time.NewTicker(5 * time.Second)",
		"go s.afterBrowserStart",
	} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("background login watcher must stay removed: %s", forbidden)
		}
	}
}

func TestLegacyProxyControlRoutesAreNotRegistered(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for _, path := range []string{"/api/proxy/test", "/api/accounts/set-proxy"} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("legacy proxy route %s status=%d want 404", path, recorder.Code)
		}
	}
}
