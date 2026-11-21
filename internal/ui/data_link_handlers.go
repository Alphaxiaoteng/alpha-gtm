package ui

import (
	"io"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/datalink"
)

const maxDataLinkUploadBytes = int64(513 << 20)

// handleAPIDataLinkSQLiteSource is the narrow transport boundary for the
// browser file picker. It streams one selected file into controlled storage;
// every inspection and collection action remains a Registry tool call.
func (s *Server) handleAPIDataLinkSQLiteSource(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDataLinkUploadBytes)
	defer r.Body.Close()
	reader, err := r.MultipartReader()
	if err != nil {
		writeDataLinkUploadError(w, http.StatusBadRequest, "DATA_SOURCE_UPLOAD_INVALID")
		return
	}
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			writeDataLinkUploadError(w, http.StatusBadRequest, "DATA_SOURCE_UPLOAD_INVALID")
			return
		}
		if part.FormName() != "file" || strings.TrimSpace(part.FileName()) == "" {
			_ = part.Close()
			continue
		}
		source, registerErr := datalink.NewService(s.db).RegisterSQLite(r.Context(), part.FileName(), part)
		_ = part.Close()
		if registerErr != nil {
			code := strings.TrimSpace(registerErr.Error())
			status := http.StatusBadRequest
			if code == "DATA_SOURCE_STORAGE_FAILED" || code == "DATA_SOURCE_SAVE_FAILED" {
				status = http.StatusInternalServerError
			}
			writeDataLinkUploadError(w, status, code)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]interface{}{"source": source})
		return
	}
	writeDataLinkUploadError(w, http.StatusBadRequest, "DATA_SOURCE_FILE_REQUIRED")
}

func writeDataLinkUploadError(w http.ResponseWriter, status int, code string) {
	if !strings.HasPrefix(code, "DATA_SOURCE_") {
		code = "DATA_SOURCE_UPLOAD_FAILED"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error_code": code})
}
