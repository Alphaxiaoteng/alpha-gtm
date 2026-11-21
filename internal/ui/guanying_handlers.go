package ui

import (
	"encoding/json"
	"net/http"

	"alpha-nexus-egolite/internal/guanying"
)

func (s *Server) handleAPIGuanyingOverview(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "METHOD_NOT_ALLOWED"})
		return
	}
	overview, err := guanying.GetOverview(r.Context(), s.db.DB)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "GUANYING_OVERVIEW_FAILED", "message": err.Error()})
		return
	}
	writeJSON(w, overview)
}

func (s *Server) handleAPIGuanyingMatchQuote(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "METHOD_NOT_ALLOWED"})
		return
	}
	var req guanying.MatchRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_JSON"})
		return
	}
	resp, err := guanying.MatchAndQuote(r.Context(), s.db.DB, req)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "GUANYING_MATCH_FAILED", "message": err.Error()})
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleAPIGuanyingLeads(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		overview, err := guanying.GetOverview(r.Context(), s.db.DB)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"leads": overview.Leads})
	case http.MethodPost:
		var req guanying.SubmitLeadRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_JSON"})
			return
		}
		lead, err := guanying.SubmitLead(r.Context(), s.db.DB, req)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_LEAD", "message": err.Error()})
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"lead": lead})
	default:
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "METHOD_NOT_ALLOWED"})
	}
}

func (s *Server) handleAPIGuanyingSEOPages(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		overview, err := guanying.GetOverview(r.Context(), s.db.DB)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"seo_pages": overview.SEOPages})
	case http.MethodPost:
		var req guanying.CreateSEOPageRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_JSON"})
			return
		}
		page, err := guanying.CreateSEOPage(r.Context(), s.db.DB, req)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SEO_PAGE_FAILED", "message": err.Error()})
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"seo_page": page})
	default:
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "METHOD_NOT_ALLOWED"})
	}
}
