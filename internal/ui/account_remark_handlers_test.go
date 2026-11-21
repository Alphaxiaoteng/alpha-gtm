package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountRemarkAPITrimsUpdatesAndClearsRemark(t *testing.T) {
	database, _ := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database, mux: http.NewServeMux()}
	server.routes()

	update := httptest.NewRecorder()
	server.mux.ServeHTTP(update, httptest.NewRequest(http.MethodPatch, "/api/accounts/"+jsonNumber(account.ID)+"/remark", bytes.NewBufferString(`{"remark":"  主账号  "}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.Remark != "主账号" {
		t.Fatalf("remark=%q err=%v", updated.Remark, err)
	}

	tooLong := httptest.NewRecorder()
	server.mux.ServeHTTP(tooLong, httptest.NewRequest(http.MethodPatch, "/api/accounts/"+jsonNumber(account.ID)+"/remark", bytes.NewBufferString(`{"remark":"`+strings.Repeat("长", 81)+`"}`)))
	if tooLong.Code != http.StatusBadRequest {
		t.Fatalf("long remark status=%d body=%s", tooLong.Code, tooLong.Body.String())
	}

	clear := httptest.NewRecorder()
	server.mux.ServeHTTP(clear, httptest.NewRequest(http.MethodPatch, "/api/accounts/"+jsonNumber(account.ID)+"/remark", bytes.NewBufferString(`{"remark":" "}`)))
	if clear.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", clear.Code, clear.Body.String())
	}
	updated, err = database.GetAccount(account.ID)
	if err != nil || updated.Remark != "" {
		t.Fatalf("cleared remark=%q err=%v", updated.Remark, err)
	}
}
