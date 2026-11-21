package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

func (s *Server) handleAPIGuanyingSiteMaintenance(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		overview, err := guanying.InspectBrandSite(r.Context(), s.db.DB)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SITE_INSPECT_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, overview)
	case http.MethodPost:
		var req guanying.CreateSiteLogRequest
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req)
		logItem, err := guanying.AddSiteMaintenanceLog(r.Context(), s.db.DB, req)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SITE_LOG_FAILED", "message": err.Error()})
			return
		}
		overview, err := guanying.InspectBrandSite(r.Context(), s.db.DB)
		if err != nil {
			writeJSONStatus(w, http.StatusCreated, map[string]any{"log": logItem})
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"log": logItem, "overview": overview})
	default:
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "METHOD_NOT_ALLOWED"})
	}
}

func (s *Server) handleAPIGuanyingSEOAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		targetURL := strings.TrimSpace(r.URL.Query().Get("url"))
		live := r.URL.Query().Get("live") == "1" || r.URL.Query().Get("live") == "true"
		overview, err := guanying.AuditKooyingOfficialSite(r.Context(), targetURL, live)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SEO_AUDIT_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, overview)
	case http.MethodPost:
		var req struct {
			URL  string `json:"url"`
			Live bool   `json:"live"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req)
		targetURL := strings.TrimSpace(req.URL)
		if targetURL == "" {
			targetURL = "https://www.kooying.com/"
		}
		overview, err := guanying.AuditKooyingOfficialSite(r.Context(), targetURL, true)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SEO_AUDIT_FAILED", "message": err.Error()})
			return
		}
		if overview != nil && overview.LiveProbeError == "" && len(overview.Pages) > 0 {
			summary := fmt.Sprintf("对官网真实目标 %s 完成实时抓取与 SEO/GEO 深度诊断：综合得分 %d/100（预期优化后 %d/100），发现高危 %d 项、警告 %d 项，Sitemap 含 %d 个 URL。",
				targetURL, overview.OverallScore, overview.ProjectedScore, overview.CriticalCount, overview.WarningCount, overview.SitemapURLCount)
			_, _ = guanying.AddSiteMaintenanceLog(r.Context(), s.db.DB, guanying.CreateSiteLogRequest{
				Category:    "seo_geo",
				Title:       fmt.Sprintf("官网实时 SEO 抓取诊断：%s", targetURL),
				TargetPath:  targetURL,
				PageURL:     targetURL,
				Summary:     summary,
				OperatorBot: "bot_gy_seo_builder",
				Status:      "verified",
			})
		}
		writeJSON(w, overview)
	default:
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "METHOD_NOT_ALLOWED"})
	}
}
