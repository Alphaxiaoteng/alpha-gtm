//go:build !legacy_chromium

package ui

import (
	"errors"
	"go/build"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestDefaultBuildExcludesLegacyChromiumSessionAndObserver(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range pkg.Imports {
		if imported == "alpha-nexus-egolite/internal/logincheck" ||
			imported == "alpha-nexus-egolite/internal/sessionbackup" ||
			imported == "github.com/chromedp/chromedp" ||
			strings.HasPrefix(imported, "github.com/chromedp/cdproto") {
			t.Fatalf("default internal/ui build imports retired runtime %q", imported)
		}
	}
	for _, name := range pkg.GoFiles {
		switch name {
		case "account_observer.go", "account_observer_handlers.go", "account_session_handlers.go", "verify.go":
			t.Fatalf("default internal/ui build includes legacy file %q", name)
		}
	}

	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if server.toolRuntime.Database != server.db {
		t.Fatal("long-lived UI Registry runtime does not reuse the startup-checked database")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "session-backups")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("default server initialized retired session-backups directory: %v", err)
	}

	for _, endpoint := range []string{
		"/api/accounts/1/session-backups",
		"/api/accounts/session-import",
		"/api/accounts/session-backup",
		"/api/accounts/1/observe",
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("retired endpoint %q remains callable: status=%d", endpoint, response.Code)
		}
	}
}
