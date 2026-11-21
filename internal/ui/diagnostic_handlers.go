package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/observability"
)

const diagnosticSnapshotRetention = 10

func (s *Server) handleAPIDiagnosticSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.db.BuildDiagnosticSnapshot(100)
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "DIAGNOSTIC_SNAPSHOT_FAILED")
		return
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "DIAGNOSTIC_SNAPSHOT_FAILED")
		return
	}
	data = append(data, '\n')
	logsDir, err := s.diagnosticLogsDir()
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "DIAGNOSTIC_SNAPSHOT_FAILED")
		return
	}
	name := fmt.Sprintf("diagnostic-%s.json", time.Now().UTC().Format("20060102T150405.000000000Z"))
	path := filepath.Join(logsDir, name)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		writeToolsError(w, http.StatusInternalServerError, "DIAGNOSTIC_SNAPSHOT_FAILED")
		return
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		writeToolsError(w, http.StatusInternalServerError, "DIAGNOSTIC_SNAPSHOT_FAILED")
		return
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		writeToolsError(w, http.StatusInternalServerError, "DIAGNOSTIC_SNAPSHOT_FAILED")
		return
	}
	if err := pruneDiagnosticSnapshots(logsDir, diagnosticSnapshotRetention); err != nil && s.toolRuntime != nil && s.toolRuntime.Events != nil {
		_ = s.toolRuntime.Events.Emit(r.Context(), observability.Event{
			Level: slog.LevelWarn, Component: "diagnostics", Name: "snapshot.retention_failed",
			Step: "prune", Status: "failed", ErrorCode: "DIAGNOSTIC_SNAPSHOT_FAILED",
		})
	}
	writeJSON(w, map[string]interface{}{
		"ok":           true,
		"path":         path,
		"generated_at": snapshot.GeneratedAt,
		"counts":       snapshot.Counts,
	})
}

func (s *Server) handleAPIDiagnosticLogDirectory(w http.ResponseWriter, _ *http.Request) {
	logsDir, err := s.diagnosticLogsDir()
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "DIAGNOSTIC_LOG_DIRECTORY_FAILED")
		return
	}
	writeJSON(w, map[string]any{"path": logsDir})
}

func (s *Server) diagnosticLogsDir() (string, error) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(logsDir, 0o700); err != nil {
		return "", err
	}
	return logsDir, nil
}

func pruneDiagnosticSnapshots(logsDir string, keep int) error {
	if keep < 1 {
		return nil
	}
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasPrefix(name, "diagnostic-") && strings.HasSuffix(name, ".json") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names[:max(0, len(names)-keep)] {
		if err := os.Remove(filepath.Join(logsDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleAPIClientDiagnostic(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind        string `json:"kind"`
		Fingerprint string `json:"fingerprint"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	kind := strings.TrimSpace(body.Kind)
	switch kind {
	case "window_error", "unhandled_rejection", "react_error":
	default:
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	fingerprint := strings.ToLower(strings.TrimSpace(body.Fingerprint))
	decoded, err := hex.DecodeString(fingerprint)
	if err != nil || len(decoded) != sha256.Size {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if s.toolRuntime != nil && s.toolRuntime.Events != nil {
		_ = s.toolRuntime.Events.Emit(r.Context(), observability.Event{
			Level: slog.LevelError, Component: "ui.client", Name: "runtime.error",
			TaskID: "client-" + fingerprint[:12], Step: kind, Status: "failed",
			ErrorCode: "CLIENT_RUNTIME_ERROR", ErrorClass: "renderer",
		})
	}
	w.WriteHeader(http.StatusNoContent)
}
