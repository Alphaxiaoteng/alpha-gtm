package ui

import "net/http"

func (s *Server) handleAPIMetricsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	summary, err := s.db.GetMetricSummary()
	if err != nil {
		http.Error(w, "read metrics failed", 500)
		return
	}
	writeJSON(w, summary)
}
func (s *Server) handleAPIMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items, err := s.db.ListPostMetrics(100)
	if err != nil {
		http.Error(w, "list metrics failed", 500)
		return
	}
	writeJSON(w, items)
}
