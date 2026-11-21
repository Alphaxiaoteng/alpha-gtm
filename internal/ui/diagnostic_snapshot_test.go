package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestDiagnosticSnapshotIs0600AndExcludesSensitivePayloads(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("xhs", "private-account-name", "private-remark")
	if err != nil {
		t.Fatal(err)
	}
	content, err := server.db.CreateContent("article", "private-title", "", "private-body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	job, err := server.db.CreatePublishJob(0, content.ID, account.ID, 0, "xhs", "article")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateJobEvent(job.ID, "error", "submit", "job.failed", "private-job-message", "/private/screenshot", "/private/dom"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.Exec(`INSERT INTO browser_events
		(profile_id,event_type,message,reason_code,pid,cdp_port,created_at)
		VALUES ('private/profile/path','crashed','private-browser-message','PROCESS_EXIT_NONZERO',123,15142,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.Exec(`INSERT INTO audit_events
		(trace_id,level,category,message,related_type,related_id,raw_json,created_at)
		VALUES ('trace-safe','error','account','private-audit-message','account',?,'{"token":"private-token"}',CURRENT_TIMESTAMP)`, account.ID); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/diagnostics/snapshot", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Path == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{
		"private-account-name", "private-remark", "private-title", "private-body",
		"private-job-message", "/private/screenshot", "/private/dom",
		"private/profile/path", "private-browser-message", "private-audit-message", "private-token",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostic snapshot leaked %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"quick_check", "job.failed", "PROCESS_EXIT_NONZERO", `"accounts_total": 1`} {
		if !strings.Contains(text, required) {
			t.Fatalf("diagnostic snapshot missed %q: %s", required, text)
		}
	}
}

func TestDiagnosticSnapshotsKeepOnlyRecentTen(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	logsDir := filepath.Join(dataDir, "logs")
	for index := 0; index < 12; index++ {
		path := filepath.Join(logsDir, "diagnostic-20000101T000000."+string(rune('a'+index))+"Z.json")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/diagnostics/snapshot", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	files, err := filepath.Glob(filepath.Join(logsDir, "diagnostic-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 10 {
		t.Fatalf("diagnostic snapshots=%d want=10 files=%v", len(files), files)
	}
}

func TestDiagnosticLogDirectoryIsCreatedWithPrivatePermissions(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/diagnostics/log-directory", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataDir, "logs")
	if result.Path != want {
		t.Fatalf("path=%q want=%q", result.Path, want)
	}
	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestClientRuntimeDiagnosticAcceptsOnlySafeFingerprint(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	emitter := &captureObserverEmitter{}
	server.toolRuntime.Events = emitter
	body := `{"kind":"unhandled_rejection","fingerprint":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/diagnostics/client-event", strings.NewReader(body)))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(emitter.events) != 2 {
		t.Fatalf("events=%+v", emitter.events)
	}
	event := emitter.events[0]
	if event.Component != "ui.client" || event.Name != "runtime.error" ||
		event.Step != "unhandled_rejection" || event.TaskID != "client-0123456789ab" ||
		event.ErrorCode != "CLIENT_RUNTIME_ERROR" {
		t.Fatalf("event=%+v", event)
	}

	badResponse := httptest.NewRecorder()
	badBody := `{"kind":"window_error","fingerprint":"private stack and URL"}`
	server.ServeHTTP(badResponse, httptest.NewRequest(http.MethodPost, "/api/diagnostics/client-event", strings.NewReader(badBody)))
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("bad status=%d body=%s", badResponse.Code, badResponse.Body.String())
	}
	if strings.Contains(strings.ToLower(fmt.Sprintf("%+v", emitter.events)), "private") {
		t.Fatalf("client diagnostic leaked raw detail: %+v", emitter.events)
	}
}
