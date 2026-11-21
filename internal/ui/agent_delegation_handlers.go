package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/agenthub"
)

// Delegations are the in-app "subagent" surface: one Bot hands a bounded task
// to another Bot. The child is a real conversation + run, so the response is
// the same shape the run views already use, plus the handoff metadata.
func (s *Server) handleAPIAgentDelegations(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		conversationID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
		if conversationID == "" {
			writeAgentHubJSON(w, http.StatusBadRequest, map[string]string{"error": "AGENT_CONVERSATION_REQUIRED"})
			return
		}
		delegations, err := s.agentHub.ListDelegations(r.Context(), conversationID)
		if err != nil {
			writeAgentDelegationError(w, err)
			return
		}
		writeAgentHubJSON(w, http.StatusOK, map[string]any{"delegations": delegations})
	case http.MethodPost:
		var body agenthub.DelegateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeAgentHubJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent delegation json"})
			return
		}
		delegation, err := s.agentHub.Delegate(r.Context(), body)
		if err != nil {
			writeAgentDelegationError(w, err)
			return
		}
		writeAgentHubJSON(w, http.StatusAccepted, delegation)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIAgentDelegation(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	delegation, err := s.agentHub.GetDelegation(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeAgentDelegationError(w, err)
		return
	}
	writeAgentHubJSON(w, http.StatusOK, delegation)
}

func (s *Server) handleAPIAgentDelegationCancel(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	delegation, err := s.agentHub.CancelDelegation(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeAgentDelegationError(w, err)
		return
	}
	writeAgentHubJSON(w, http.StatusOK, delegation)
}

func writeAgentDelegationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agenthub.ErrDelegationNotFound):
		writeAgentHubJSON(w, http.StatusNotFound, map[string]string{"error": "AGENT_DELEGATION_NOT_FOUND"})
	case errors.Is(err, agenthub.ErrBotNotFound):
		writeAgentHubJSON(w, http.StatusNotFound, map[string]string{"error": "AGENT_BOT_NOT_FOUND"})
	case errors.Is(err, agenthub.ErrDelegationSelf):
		writeAgentHubJSON(w, http.StatusBadRequest, map[string]string{"error": "AGENT_DELEGATION_SELF"})
	case errors.Is(err, agenthub.ErrDelegationBusy):
		writeAgentHubJSON(w, http.StatusConflict, map[string]string{"error": "AGENT_DELEGATION_BUSY"})
	case errors.Is(err, agenthub.ErrDelegationLimit):
		writeAgentHubJSON(w, http.StatusTooManyRequests, map[string]string{"error": "AGENT_DELEGATION_LIMIT"})
	default:
		writeAgentHubError(w, err)
	}
}
