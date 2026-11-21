package ui

import (
	"encoding/json"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/feishu"
)

// Feishu is the shared workspace mirror. Local SQLite stays the source of
// truth; this endpoint pushes rows into the Base and records the mapping.
func (s *Server) handleAPIFeishuStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "FEISHU_METHOD_NOT_ALLOWED"})
		return
	}
	status := feishu.SyncStatusFor(r.Context(), s.db.DB, s.db.DataDir)
	writeJSON(w, status)
}

func (s *Server) handleAPIFeishuSync(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "FEISHU_METHOD_NOT_ALLOWED"})
		return
	}
	var body struct {
		Module string `json:"module"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
	module := strings.TrimSpace(body.Module)
	if module == "" {
		module = "all"
	}
	if !feishu.StartSync(s.db.DB, s.db.DataDir, module) {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "FEISHU_SYNC_RUNNING", "hint": "已有同步任务在跑，请稍后查看状态"})
		return
	}
	writeJSONStatus(w, http.StatusAccepted, feishu.SyncStatusFor(r.Context(), s.db.DB, s.db.DataDir))
}
