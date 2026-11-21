package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

func TestEgoIdentityPlatformsExpectedCodesHTTPProtectsCurrentConfiguration(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	identity, err := server.db.CreateEgoIdentity("HTTP 冲突保护矩阵")
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := server.db.ConfigureIdentityPlatforms(identity.ID, []string{"douyin"})
	if err != nil {
		t.Fatal(err)
	}
	original := accounts[0]
	if err := server.db.UpdateAccountLoginState(original.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}

	update := httptest.NewRecorder()
	server.mux.ServeHTTP(update, httptest.NewRequest(http.MethodPut,
		"/api/ego/identities/"+jsonNumber(identity.ID)+"/platforms",
		bytes.NewBufferString(`{"platform_codes":["douyin","youtube"],"expected_platform_codes":["douyin"]}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("current expected update status=%d body=%s", update.Code, update.Body.String())
	}

	stale := httptest.NewRecorder()
	server.mux.ServeHTTP(stale, httptest.NewRequest(http.MethodPut,
		"/api/ego/identities/"+jsonNumber(identity.ID)+"/platforms",
		bytes.NewBufferString(`{"platform_codes":["xhs"],"expected_platform_codes":["douyin"]}`)))
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"EGO_PLATFORM_CONFIGURATION_CONFLICT"`) {
		t.Fatalf("stale update status=%d body=%s", stale.Code, stale.Body.String())
	}

	all, err := server.db.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	var current *db.Account
	enabled := map[string]bool{}
	for index := range all {
		account := &all[index]
		enabled[account.PlatformCode] = account.Enabled
		if account.PlatformCode == "douyin" {
			current = account
		}
	}
	if current == nil || current.ID != original.ID || current.LoginState != db.AccountAuthLoggedIn || !enabled["douyin"] || !enabled["youtube"] || enabled["xhs"] {
		t.Fatalf("conflict changed accounts: %+v", all)
	}
}

func TestEgoIdentityPlatformsExpectedCodesHTTPAcceptsEmptyAndRejectsInvalid(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	identity, err := server.db.CreateEgoIdentity("HTTP 空集合矩阵")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/ego/identities/" + jsonNumber(identity.ID) + "/platforms"
	for _, body := range []string{
		`{"platform_codes":[],"expected_platform_codes":[]}`,
		`{"platform_codes":["douyin"]}`,
	} {
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body)))
		if response.Code != http.StatusOK {
			t.Fatalf("valid body=%s status=%d body=%s", body, response.Code, response.Body.String())
		}
	}
	for _, body := range []string{
		`{}`,
		`{"platform_codes":null}`,
		`{"platform_codes":["douyin"],"expected_platform_codes":null}`,
		`{"platform_codes":["douyin"],"expected_platform_codes":["douyin","douyin"]}`,
		`{"platform_codes":["douyin","douyin"]}`,
	} {
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid body=%s status=%d body=%s", body, response.Code, response.Body.String())
		}
		accounts, err := server.db.ListAccountsByIdentity(identity.ID)
		if err != nil || len(accounts) != 1 || accounts[0].PlatformCode != "douyin" || !accounts[0].Enabled {
			t.Fatalf("invalid body=%s changed accounts=%+v err=%v", body, accounts, err)
		}
	}
}
