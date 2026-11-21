package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (s *Server) handleAPIAccountRemark(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	var body struct {
		Remark string `json:"remark"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid account remark request", http.StatusBadRequest)
		return
	}
	remark := strings.TrimSpace(body.Remark)
	if utf8.RuneCountInString(remark) > 80 {
		http.Error(w, "account remark must not exceed 80 characters", http.StatusBadRequest)
		return
	}
	if err := s.db.UpdateAccountRemark(accountID, remark); err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"accountId": accountID, "remark": remark})
}
