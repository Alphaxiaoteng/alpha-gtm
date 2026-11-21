package ui

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestLegacyChromiumAccountImportCopiesMetadataButNotLoginState(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE accounts (
		id INTEGER PRIMARY KEY,
		platform_code TEXT NOT NULL,
		name TEXT NOT NULL,
		account_identifier TEXT,
		login_state TEXT NOT NULL,
		enabled INTEGER NOT NULL
	);
	INSERT INTO accounts(platform_code,name,account_identifier,login_state,enabled) VALUES
		('xhs','旧小红书','xhs-88','logged_in',1),
		('douyin','旧抖音','dy-99','logged_in',1)`); err != nil {
		t.Fatal(err)
	}
	_ = legacy.Close()

	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.legacyAccountsPath = legacyPath

	preview := httptest.NewRecorder()
	server.ServeHTTP(preview, httptest.NewRequest(http.MethodGet, "/api/ego/legacy-accounts", nil))
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"count":2`) ||
		!strings.Contains(preview.Body.String(), `"historical_login_state":"logged_in"`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	for _, forbidden := range []string{"cookie", "profile_dir", "storage"} {
		if strings.Contains(strings.ToLower(preview.Body.String()), forbidden) {
			t.Fatalf("preview leaked forbidden field %q: %s", forbidden, preview.Body.String())
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/ego/legacy-accounts/import", nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"imported":2`) {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	accounts, err := server.db.ListAccounts()
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	for _, account := range accounts {
		if account.LoginState != "unknown" {
			t.Fatalf("legacy login state was trusted: %+v", account)
		}
		if account.AccountIdentifier == "" || !strings.Contains(account.Remark, "需在 EgoLite 重新登录") {
			t.Fatalf("metadata was not imported safely: %+v", account)
		}
	}
}
