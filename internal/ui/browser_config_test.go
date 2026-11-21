package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestNewServerUsesEgoLiteAsOnlyBrowserRuntime(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if server.cfg.BrowserEngine != "ego-lite" {
		t.Fatalf("browser engine=%q", server.cfg.BrowserEngine)
	}
	if server.facade != nil || server.toolRuntime.Fleet != nil {
		t.Fatal("EgoLite server initialized legacy BrowserFacade or fleet runtime")
	}
}

func TestAccountAddCreatesNoLegacyBrowserProfile(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/accounts/add", bytes.NewBufferString(`{"identityId":1,"platform":"wechat_mp","name":"wechat"}`))
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	accounts, err := server.db.ListAccounts()
	if err != nil || len(accounts) != 1 || accounts[0].IdentityID != 1 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	if profile, err := server.db.GetBrowserProfileByAccount(accounts[0].ID); err == nil {
		t.Fatalf("legacy browser profile was created: %+v", profile)
	}
}

func TestLegacyBrowserConfigurationRoutesAreUnavailable(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/browser/config"},
		{http.MethodPost, "/api/browser/profiles"},
		{http.MethodPost, "/api/accounts/set-engine"},
		{http.MethodPost, "/api/browser/ai-chat"},
		{http.MethodPost, "/api/config/browsers"},
		{http.MethodPost, "/api/config/update"},
		{http.MethodGet, "/api/login-sessions"},
		{http.MethodGet, "/api/fleet/instances"},
		{http.MethodGet, "/api/fleet/runs"},
		{http.MethodPost, "/api/accounts/1/assets/discover"},
		{http.MethodPost, "/api/data/collect"},
		{http.MethodPost, "/api/batch/publish"},
		{http.MethodPost, "/api/publish-plans/1/inspect"},
		{http.MethodPost, "/api/publish-plans/1/inspect-composer"},
		{http.MethodPost, "/api/publish-plans/1/inspect-manager"},
		{http.MethodPost, "/api/publish-plans/1/resume-topics"},
		{http.MethodPost, "/api/publish-plans/1/reveal-settings"},
	} {
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, httptest.NewRequest(route.method, route.path, bytes.NewBufferString(`{}`)))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", route.method, route.path, response.Code, response.Body.String())
		}
	}
}
