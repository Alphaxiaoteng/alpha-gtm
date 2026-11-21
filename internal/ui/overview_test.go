package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
)

func TestOverviewCountsDurableExternalBrowserRuntime(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	account, err := server.db.CreateAccount("xhs", "overview-external-runtime", "")
	if err != nil {
		t.Fatal(err)
	}

	process := exec.Command("sleep", "30")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	})

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := server.db.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, lease_owner, started_at, last_seen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ready', 'external-owner', ?, ?, ?)`,
		browser.ProfileIDForAccount(account.ID), account.ID, process.Process.Pid,
		15991, "http://127.0.0.1:15991", now, now, now); err != nil {
		t.Fatal(err)
	}
	if server.facade != nil {
		t.Fatal("EgoLite overview initialized the legacy BrowserFacade")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Running int `json:"running"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Running != 0 {
		t.Fatalf("legacy runtime leaked into EgoLite overview: running=%d", body.Running)
	}
}
