//go:build legacy_chromium

package ui

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/sessionbackup"
)

func TestSessionBackupListExposesOnlySafeMetadata(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	account, err := server.db.CreateAccount("xhs", "backup-account", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.sessionBackups.SaveLocal(account.ID, sessionbackup.Snapshot{
		Version: sessionbackup.FormatVersion, CreatedAt: time.Now().UTC(),
		Account: sessionbackup.Account{PlatformCode: "xhs", Name: "backup-account"},
		Cookies: []sessionbackup.Cookie{{Name: "session", Value: "do-not-return-this-cookie", Domain: ".xiaohongshu.com", Path: "/"}},
	}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/session-backups", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"do-not-return-this-cookie", "profile_dir", "cookies", "local-key"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("backup list leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestSessionImportRejectsWrongPassphraseWithoutLeakingArchive(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	archive, err := server.sessionBackups.EncryptPortable(sessionbackup.Snapshot{
		Version: sessionbackup.FormatVersion, CreatedAt: time.Now().UTC(),
		Account: sessionbackup.Account{PlatformCode: "xhs", Name: "source"},
		Cookies: []sessionbackup.Cookie{{Name: "session", Value: "never-leak-import-cookie", Domain: ".xiaohongshu.com", Path: "/"}},
	}, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("archive", "account.anxsession")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("passphrase", "wrong passphrase here"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/accounts/session-import", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "never-leak-import-cookie") {
		t.Fatalf("import error leaked session data: %s", response.Body.String())
	}
}

func TestSessionImportExistingRejectsPlatformMismatchWithoutCreatingDuplicate(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	account, err := server.db.CreateAccount("xhs", "existing-target", "")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := server.sessionBackups.EncryptPortable(sessionbackup.Snapshot{
		Version: sessionbackup.FormatVersion, CreatedAt: time.Now().UTC(),
		Account: sessionbackup.Account{PlatformCode: "douyin", Name: "source"},
		Cookies: []sessionbackup.Cookie{{Name: "session", Value: "never-leak-mismatch-cookie", Domain: ".douyin.com", Path: "/"}},
	}, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("archive", "account.anxsession")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("passphrase", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/session-import", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "never-leak-mismatch-cookie") {
		t.Fatalf("mismatch response leaked session data: %s", response.Body.String())
	}
	accounts, err := server.db.ListAccounts()
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
}

func TestApplyImportedSessionProfilePreservesDirectoryAndUsesPortableFingerprint(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	account, err := server.db.CreateAccount("xhs", "existing-target", "")
	if err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	profile, err := server.db.CreateBrowserProfileEngine(account.ID, profileDir, "chromium", 1001, "en-US", "en-US,en", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.applyImportedSessionProfile(account.ID, sessionbackup.Profile{
		BrowserType: "fingerprint-chromium", FingerprintSeed: 9009, FingerprintConfig: `{"region":"beijing"}`,
		Lang: "zh-CN", AcceptLang: "zh-CN,zh", Timezone: "Asia/Shanghai",
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := server.db.GetBrowserProfileByAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != profile.ID || updated.ProfileDir != profileDir || updated.BrowserType != "fingerprint-chromium" ||
		updated.FingerprintSeed != 9009 || updated.FingerprintConfig != `{"region":"beijing"}` ||
		updated.Lang != "zh-CN" || updated.AcceptLang != "zh-CN,zh" || updated.Timezone != "Asia/Shanghai" {
		t.Fatalf("profile=%+v", updated)
	}
}
