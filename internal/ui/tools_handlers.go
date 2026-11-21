package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/tools"
	"alpha-nexus-egolite/internal/trace"
)

const maxToolsCallBodyBytes = 1 << 20

type toolsCallBody struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// handleAPIToolsSchema exposes the same tool schema used by CLI and MCP.
func (s *Server) handleAPIToolsSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeToolsError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	schema, err := tools.Default.SchemaJSON()
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(schema)
}

// handleAPIToolsCall is the HTTP adapter over the canonical tools.Registry.
// It intentionally does not implement business logic or create a second tool
// catalog: CLI, MCP, UI, and Electron all reach the same Registry contract.
func (s *Server) handleAPIToolsCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeToolsError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxToolsCallBodyBytes)
	defer r.Body.Close()
	var body toolsCallBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeToolsError(w, http.StatusBadRequest, "MISSING_PARAM")
		return
	}
	requestID := string(trace.New())
	ctx := trace.WithID(r.Context(), trace.ID(requestID))
	ctx = observability.WithCorrelation(ctx, observability.Correlation{RequestID: requestID})
	result, _ := tools.Default.Call(ctx, s.toolRuntime, body.Name, body.Args)
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"schema_version": 1,
		"request_id":     requestID,
		"result":         result,
	})
}

func writeToolsError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"errorCode": code})
}
