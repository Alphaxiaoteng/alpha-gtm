package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/tools"
)

func TestAccountTagAPIUsesRegistryAndReturnsTagsOnAccounts(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database, mux: http.NewServeMux(), toolRuntime: &tools.Runtime{DataDir: dataDir}}
	server.routes()

	create := httptest.NewRecorder()
	server.mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/account-tags", bytes.NewBufferString(`{"name":"文章","color":"#8B5CF6"}`)))
	if create.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var tag struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &tag); err != nil || tag.ID <= 0 {
		t.Fatalf("tag=%+v err=%v", tag, err)
	}
	createSecond := httptest.NewRecorder()
	server.mux.ServeHTTP(createSecond, httptest.NewRequest(http.MethodPost, "/api/account-tags", bytes.NewBufferString(`{"name":"优先","color":"#0EA5E9"}`)))
	if createSecond.Code != http.StatusOK {
		t.Fatalf("create second status=%d body=%s", createSecond.Code, createSecond.Body.String())
	}
	var secondTag struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(createSecond.Body.Bytes(), &secondTag); err != nil || secondTag.ID <= 0 {
		t.Fatalf("second tag=%+v err=%v", secondTag, err)
	}
	reorder := httptest.NewRecorder()
	server.mux.ServeHTTP(reorder, httptest.NewRequest(http.MethodPut, "/api/account-tags/order", bytes.NewBufferString(
		`{"tagIds":[`+jsonNumber(secondTag.ID)+`,`+jsonNumber(tag.ID)+`]}`,
	)))
	if reorder.Code != http.StatusOK {
		t.Fatalf("reorder status=%d body=%s", reorder.Code, reorder.Body.String())
	}
	list := httptest.NewRecorder()
	server.mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/account-tags", nil))
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(`"position":0`)) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	bulk := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"accountIds":[` + jsonNumber(account.ID) + `],"tagIds":[` + jsonNumber(tag.ID) + `],"mode":"append"}`)
	server.mux.ServeHTTP(bulk, httptest.NewRequest(http.MethodPost, "/api/account-tags/bulk", body))
	if bulk.Code != http.StatusOK {
		t.Fatalf("bulk status=%d body=%s", bulk.Code, bulk.Body.String())
	}

	accounts := httptest.NewRecorder()
	server.mux.ServeHTTP(accounts, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	if accounts.Code != http.StatusOK || !bytes.Contains(accounts.Body.Bytes(), []byte(`"tags":[{"id":`)) {
		t.Fatalf("accounts status=%d body=%s", accounts.Code, accounts.Body.String())
	}

	invalid := httptest.NewRecorder()
	server.mux.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/account-tags", bytes.NewBufferString(`{"name":"x","color":"#112233","unknown":true}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func jsonNumber(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
