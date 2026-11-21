package ui

import "net/http"

func (s *Server) handleAPILocalDashboardSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	summary, err := s.db.GetLocalDashboardSummary()
	if err != nil {
		http.Error(w, "load dashboard summary failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, summary)
}
