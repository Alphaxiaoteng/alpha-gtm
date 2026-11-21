package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestSafeSnapshotExcludesCredentialAndProfileData(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	account, err := server.db.CreateAccount("xhs", "snapshot-account", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateBrowserProfile(account.ID, "/private/profile", 123, "en-US", "en-US", "UTC"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateCredential(account.ID, "xhs", "oauth", "do-not-export-ref", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateContent("article", "snapshot content", "", "body", "[]", "draft"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/export/snapshot", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{"do-not-export-ref", "/private/profile", "credentials", "browser_profiles", "login_sessions", "schedules"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "snapshot-account") || !strings.Contains(body, "snapshot content") {
		t.Fatalf("snapshot missed safe business data: %s", body)
	}
}

func TestScheduleRoutesAreNotRegistered(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/schedules", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	if response.Code != http.StatusNotFound {
		t.Fatalf("schedule API status=%d, want 404", response.Code)
	}
}
