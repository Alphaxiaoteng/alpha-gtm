package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// handleAPIAccountGroup stores an explicit operator-defined account group.
// Meta pairing only consumes a non-empty exact group; it never guesses from a
// nickname, avatar, or platform ID.
func (s *Server) handleAPIAccountGroup(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	var body struct {
		Group string `json:"group"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid account group request", http.StatusBadRequest)
		return
	}
	group := strings.TrimSpace(body.Group)
	if utf8.RuneCountInString(group) > 80 {
		http.Error(w, "account group must not exceed 80 characters", http.StatusBadRequest)
		return
	}
	if err := s.db.UpdateAccountGroup(accountID, group); err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"accountId": accountID, "group": group})
}
