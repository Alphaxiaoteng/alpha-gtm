package ui

import (
	"log/slog"
	"net/http"
)

// handleAPIDshCatalog exposes the DSH provider/model catalog 1:1 so the
// settings page can show what the local dsh runtime actually offers. Read-only:
// the file belongs to DSH and this endpoint never writes it.
func (s *Server) handleAPIDshCatalog(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "AGENT_DSH_LOCAL_ONLY", "local requests only")
		return
	}
	if s.agentHub == nil {
		writeDataAPIError(w, http.StatusServiceUnavailable, "AGENT_HUB_UNAVAILABLE", "agent hub unavailable")
		return
	}
	catalog, err := s.agentHub.DshCatalog()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "AGENT_DSH_CATALOG_FAILED", "read dsh catalog failed")
		return
	}
	// A malformed patch file is reported in the payload; the page still gets a
	// stable shape so settings can render what remains usable.
	if catalog.Warning != "" {
		slog.Warn("dsh catalog partially unreadable", "warning", catalog.Warning)
	}
	writeJSON(w, catalog)
}
