package ui

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// handleAPIOperationEvents exposes the safe Agent/Tool activity timeline. It
// is read-only and never returns tool args, CDP payloads, URLs, or secrets.
func (s *Server) handleAPIOperationEvents(w http.ResponseWriter, r *http.Request) {
	limit := 80
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
			return
		}
		limit = parsed
	}
	events, err := s.db.ListOperationEvents(limit)
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "OPERATION_EVENT_LIST_FAILED")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"events": events, "count": len(events)})
}
