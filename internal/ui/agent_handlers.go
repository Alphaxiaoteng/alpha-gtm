package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agents"
	"alpha-nexus-egolite/internal/publishguard"
)

const (
	openCodeNotFound            = "not_found"
	openCodeAvailableUnverified = "available_unverified"
	openCodeVerifiedReadOnly    = "verified_read_only"
	openCodeVersionMismatch     = "version_mismatch"
)

type agentSchemeAvailability struct {
	agents.SchemeInfo
	Ready        bool   `json:"ready"`
	ReadyNote    string `json:"ready_note,omitempty"`
	Availability string `json:"availability,omitempty"`
}

func enrichAgentSchemes(list []agents.SchemeInfo, openCodeBinary string) []agentSchemeAvailability {
	out := make([]agentSchemeAvailability, 0, len(list))
	for _, info := range list {
		enriched := agentSchemeAvailability{SchemeInfo: info, Ready: true}
		switch info.ID {
		case agents.SchemeOpenCode:
			// Binary discovery proves only that a file is present. There is no
			// persisted real MCP smoke evidence yet, so this endpoint must never
			// promote OpenCode to verified_read_only automatically.
			enriched.Ready = false
			if openCodeBinary == "" {
				enriched.Availability = openCodeNotFound
				enriched.ReadyNote = "opencode not on PATH"
			} else {
				enriched.Availability = openCodeAvailableUnverified
				enriched.ReadyNote = "opencode binary found; real read-only smoke unverified"
			}
		case agents.SchemeBrowser:
			enriched.ReadyNote = "needs running CDP / account browser for live"
		case agents.SchemeLLMTools:
			enriched.ReadyNote = "uses AGENTPUSH_LLM_* / ~/.alpha-nexus/llm.json (DashScope/OpenAI); gateway failure returns without executing tools"
		case agents.SchemeNative, agents.SchemeMCPExport:
			enriched.ReadyNote = "always available"
		}
		out = append(out, enriched)
	}
	return out
}

func openCodeAvailabilityForBinary(openCodeBinary string) string {
	if openCodeBinary == "" {
		return openCodeNotFound
	}
	return openCodeAvailableUnverified
}

func (s *Server) handleAPIAgentSchemes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	oc := agents.ResolveOpenCodeBinary("")
	out := enrichAgentSchemes(agents.ListSchemes(), oc)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"schemes":               out,
		"opencode":              oc,
		"opencode_availability": openCodeAvailabilityForBinary(oc),
		"server_time":           time.Now().UTC().Format(time.RFC3339),
	})
}

type schemeRunBody struct {
	Scheme    string `json:"scheme"`
	Goal      string `json:"goal"`
	DryRun    *bool  `json:"dry_run"`
	AccountID int64  `json:"account_id"`
	CDPURL    string `json:"cdp_url"`
	MaxSteps  int    `json:"max_steps"`
	Model     string `json:"model"`
}

func (s *Server) handleAPIAgentSchemeRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body schemeRunBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Scheme == "" {
		http.Error(w, "scheme required", http.StatusBadRequest)
		return
	}
	dry := true
	if body.DryRun != nil {
		dry = *body.DryRun
	}
	if agents.SchemeID(body.Scheme) == agents.SchemeBrowser {
		if strings.TrimSpace(body.CDPURL) != "" {
			http.Error(w, "CDP_URL_NOT_ALLOWED", http.StatusBadRequest)
			return
		}
		if !dry && body.AccountID <= 0 {
			http.Error(w, "account_id required", http.StatusBadRequest)
			return
		}
	}
	dir, _ := s.cfg.ResolvedDataDir()
	req := agents.SchemeRunRequest{
		Scheme:    agents.SchemeID(body.Scheme),
		Goal:      body.Goal,
		DryRun:    dry,
		AccountID: body.AccountID,
		CDPURL:    body.CDPURL,
		MaxSteps:  body.MaxSteps,
		Model:     body.Model,
		DataDir:   dir,
		Runtime:   s.toolRuntime,
	}
	// cap UI browser live loop
	if req.MaxSteps <= 0 {
		req.MaxSteps = 4
	}
	if req.MaxSteps > 12 {
		req.MaxSteps = 12
	}
	if req.Scheme == agents.SchemeBrowser && req.AccountID > 0 {
		release, err := s.acquireBrowserOperationLease(req.AccountID, "agent_browser", uiBrowserAgentLeaseTTL)
		if err != nil {
			writeFacadeError(w, err)
			return
		}
		defer release()
		if s.facade == nil {
			http.Error(w, "browser facade is unavailable", http.StatusInternalServerError)
			return
		}
		status, statusErr := s.facade.Status(req.AccountID)
		if cdpURL, cdpErr := publishguard.LiveCDPURL(req.AccountID, status, statusErr); cdpErr == nil {
			// Trusted UI adapter passes the endpoint in-process. It is never
			// returned by tools.Registry or accepted from the HTTP caller.
			req.CDPURL = cdpURL
		}
	}

	ctx := r.Context()
	res, err := agents.RunScheme(ctx, req)
	if res == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "error": errString(err),
		})
		return
	}
	status := http.StatusOK
	if !res.OK && err != nil {
		status = http.StatusOK // still return envelope for UI buttons
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(res)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
