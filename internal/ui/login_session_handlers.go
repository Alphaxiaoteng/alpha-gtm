package ui

import (
	"net/http"
	"strconv"
)

func (s *Server) handleAPILoginSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessions, err := s.db.ListLoginSessions(100)
	if err != nil {
		http.Error(w, "list login sessions failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func (s *Server) handleAPIAccountLoginStart(w http.ResponseWriter, r *http.Request) {
	id, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	session, err := s.db.CreateLoginSession(id)
	if err != nil {
		http.Error(w, "create login session failed", http.StatusBadRequest)
		return
	}
	browser, err := s.startAccountBrowser(id)
	if err != nil {
		_ = s.db.UpdateLoginSession(session.ID, "failed", "BROWSER_START_FAILED")
		writeFacadeError(w, err)
		return
	}
	_ = s.db.UpdateLoginSession(session.ID, "awaiting_login", "")
	session.Status = "awaiting_login"
	writeJSON(w, map[string]any{"session": session, "browser": browser})
}

func (s *Server) handleAPILoginSessionCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "login session id required", http.StatusBadRequest)
		return
	}
	sessions, err := s.db.ListLoginSessions(200)
	if err != nil {
		http.Error(w, "read login session failed", http.StatusInternalServerError)
		return
	}
	var accountID int64
	for _, item := range sessions {
		if item.ID == id {
			accountID = item.AccountID
			break
		}
	}
	if accountID == 0 {
		http.Error(w, "login session not found", http.StatusNotFound)
		return
	}
	if s.facade != nil {
		if err := s.stopAccountBrowser(accountID); err != nil {
			writeFacadeError(w, err)
			return
		}
	}
	if err := s.db.UpdateLoginSession(id, "cancelled", "CANCELLED_BY_USER"); err != nil {
		http.Error(w, "login session is not active", http.StatusConflict)
		return
	}
	writeJSON(w, map[string]bool{"cancelled": true})
}
