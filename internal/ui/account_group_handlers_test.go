package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestAccountGroupAPIUpdatesAndClearsExplicitMetaPairing(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("instagram", "meta operator", "")
	if err != nil {
		t.Fatal(err)
	}

	update := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPatch, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/group", bytes.NewBufferString(body))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		return recorder
	}
	updated := update(`{"group":"  Alpha Meta  "}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", updated.Code, updated.Body.String())
	}
	var response struct {
		AccountID int64  `json:"accountId"`
		Group     string `json:"group"`
	}
	if err := json.NewDecoder(updated.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.AccountID != account.ID || response.Group != "Alpha Meta" {
		t.Fatalf("response=%+v", response)
	}
	persisted, err := server.db.GetAccount(account.ID)
	if err != nil || persisted.AccountGroup != "Alpha Meta" {
		t.Fatalf("account=%+v err=%v", persisted, err)
	}
	if cleared := update(`{"group":""}`); cleared.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", cleared.Code, cleared.Body.String())
	}
	persisted, _ = server.db.GetAccount(account.ID)
	if persisted.AccountGroup != "" {
		t.Fatalf("group was not cleared: %+v", persisted)
	}
}

func TestAccountGroupAPIRejectsOversizedValue(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("facebook", "meta operator", "")
	if err != nil {
		t.Fatal(err)
	}
	value := bytes.Repeat([]byte("界"), 81)
	body, _ := json.Marshal(map[string]string{"group": string(value)})
	request := httptest.NewRequest(http.MethodPatch, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/group", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
