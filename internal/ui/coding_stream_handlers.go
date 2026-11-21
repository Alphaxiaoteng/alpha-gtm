package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/agents"
	"alpha-nexus-egolite/internal/agentws"
)

type codingStreamBody struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	WorkDir   string `json:"work_dir"`
}

// POST /api/coding/chat/stream - Server-Sent Events (SSE) streaming endpoint for fast token feedback
func (s *Server) handleAPICodingChatStream(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var body codingStreamBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	dir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	agents.DefaultLedger.Init(dir)

	ws, err := agentws.Ensure(dir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	workDir := body.WorkDir
	if workDir == "" {
		workDir = ws
	}

	sessionID := body.SessionID
	if sessionID == "" {
		sessionID = "default"
	}

	// SSE Setup
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", 500)
		return
	}

	req := agents.FastAgentRequest{
		SessionID: sessionID,
		WorkDir:   workDir,
		Message:   strings.TrimSpace(body.Message),
	}

	_, agentErr := agents.RunFastAgent(r.Context(), req, func(eventType string, data any) error {
		payload, err := json.Marshal(data)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(payload))
		flusher.Flush()
		return nil
	})

	if agentErr != nil {
		errPayload, _ := json.Marshal(map[string]string{"error": agentErr.Error()})
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", string(errPayload))
		flusher.Flush()
	}
}
