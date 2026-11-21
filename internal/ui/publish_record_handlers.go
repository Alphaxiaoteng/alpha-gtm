package ui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tools"
)

func (s *Server) handleAPIPublishRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	records, err := s.db.ListPublishRecords(100)
	if err != nil {
		http.Error(w, "list publish records failed", http.StatusInternalServerError)
		return
	}
	if records == nil {
		records = make([]db.PublishRecord, 0)
	}
	writeJSON(w, records)
}

func (s *Server) handleAPIPublishContentRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	records, err := s.db.ListPublishContentRecords(100)
	if err != nil {
		http.Error(w, "list publish content records failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, records)
}

func (s *Server) handleAPIPublishContentRecordCover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "publish content record id required", http.StatusBadRequest)
		return
	}
	record, err := s.db.GetPublishContentRecord(id)
	if err != nil || record.CoverPath == "" {
		http.Error(w, "cover unavailable", http.StatusNotFound)
		return
	}
	path, err := validateLocalMediaPath(record.CoverPath)
	if err != nil {
		http.Error(w, "cover unavailable", http.StatusNotFound)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "cover unavailable", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "cover unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleAPIPublishContentRecordDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	var request struct {
		ConfirmPermanentDelete bool `json:"confirmPermanentDelete"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || !request.ConfirmPermanentDelete {
		writeToolsError(w, http.StatusBadRequest, "DELETE_CONFIRMATION_REQUIRED")
		return
	}
	record, err := s.db.GetPublishContentRecord(id)
	if err != nil {
		writeToolsError(w, http.StatusNotFound, "PUBLISH_CONTENT_RECORD_NOT_FOUND")
		return
	}
	deletableScheduled := (record.PlatformCode == "douyin" || record.PlatformCode == "youtube" ||
		record.PlatformCode == "tiktok" || record.PlatformCode == "kuaishou" || record.PlatformCode == "wechat_video" ||
		record.PlatformCode == "facebook") && record.RemoteState == "scheduled"
	deletablePublishedKuaishou := record.PlatformCode == "kuaishou" && record.RemoteState == "published" &&
		strings.TrimSpace(record.PostURL) != "" && strings.TrimSpace(record.ExternalPostID) != ""
	if !deletableScheduled && !deletablePublishedKuaishou {
		writeToolsError(w, http.StatusConflict, "PUBLISH_CONTENT_DELETE_UNSUPPORTED")
		return
	}
	if err := s.db.MarkPublishContentDeletePending(id); err != nil {
		writeToolsError(w, http.StatusConflict, "PUBLISH_CONTENT_NOT_DELETABLE")
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	result, _ := tools.Default.Call(r.Context(), localRuntime, "publish.content.delete", map[string]interface{}{
		"record_id": id, "confirm_permanent_delete": true,
	})
	if result == nil || !result.OK {
		code := "PUBLISH_CONTENT_DELETE_FAILED"
		if result != nil && result.Error != "" {
			code = result.Error
		}
		_ = s.db.MarkPublishContentDeleteFailed(id, code)
		writeToolsError(w, http.StatusConflict, code)
		return
	}
	writeJSON(w, map[string]interface{}{"deleted": true})
}

func (s *Server) handleAPIManualPublishJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.db.ListManualPublishJobs(100)
	if err != nil {
		http.Error(w, "list manual publish jobs failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, jobs)
}

// handleAPIPublishJobConfirm is a compatibility endpoint. A local user
// attestation is non-authoritative and can never create a publish record.
func (s *Server) handleAPIPublishJobConfirm(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || jobID <= 0 {
		http.Error(w, "invalid publish job id", http.StatusBadRequest)
		return
	}
	var request struct {
		PostURL        string `json:"postUrl"`
		ExternalPostID string `json:"externalPostId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = s.db.ConfirmManualPublish(jobID, request.PostURL, request.ExternalPostID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrPublishJobNotFound):
			http.Error(w, "publish job not found", http.StatusNotFound)
		case errors.Is(err, db.ErrPublishJobConflict):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	http.Error(w, "creator-backend evidence is required", http.StatusConflict)
}

// handleAPIPublishJobVerifyPublished triggers a read-only re-verification of
// one already-submitted supported job. Evidence is collected only by the
// server's reviewed observer; callers cannot supply status, URLs, or files.
func (s *Server) handleAPIPublishJobVerifyPublished(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || jobID <= 0 {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	defer r.Body.Close()
	if body, readErr := io.ReadAll(r.Body); readErr != nil || len(body) != 0 {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}

	before, err := s.db.GetPublishJob(jobID)
	if err != nil {
		if errors.Is(err, db.ErrPublishJobNotFound) {
			writeToolsError(w, http.StatusNotFound, "PUBLISH_JOB_NOT_FOUND")
			return
		}
		writeToolsError(w, http.StatusInternalServerError, "PUBLISH_JOB_READ_FAILED")
		return
	}
	if !egoCreatorBackendPlatformSupported(before.PlatformCode) ||
		before.Mode != "submit" || before.SubmitAttemptedAt == "" {
		writeToolsError(w, http.StatusConflict, "PUBLISH_VERIFICATION_NOT_ALLOWED")
		return
	}

	verification, err := s.verifyCreatorBackend(r.Context(), jobID)
	if err != nil {
		writeToolsError(w, http.StatusConflict, strings.ToUpper(before.PlatformCode)+"_BACKEND_VERIFY_FAILED")
		return
	}
	after, err := s.db.GetPublishJob(jobID)
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "PUBLISH_JOB_READ_FAILED")
		return
	}
	if after.SubmitAttemptedAt != before.SubmitAttemptedAt {
		writeToolsError(w, http.StatusInternalServerError, "PUBLISH_SUBMIT_MARKER_CHANGED")
		return
	}

	status := http.StatusAccepted
	if verification.Status == "published" {
		status = http.StatusOK
	}
	if verification.Status == "published" || verification.Status == "scheduled" {
		var exists int
		body := before.PlatformCode + " · " + verification.Status
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM notifications WHERE type='publish.completed' AND related_type='publish_job' AND related_id=? AND body=?)`, jobID, body).Scan(&exists); err != nil || exists == 0 {
			_, _ = s.db.CreateNotification("publish.completed", "info", "发布已核验", body, "publish_job", jobID)
		}
	}
	writeJSONStatus(w, status, map[string]string{
		"status":     verification.Status,
		"step":       verification.Step,
		"error_code": verification.ErrorCode,
	})
}

func egoCreatorBackendPlatformSupported(platformCode string) bool {
	switch strings.TrimSpace(platformCode) {
	case "instagram", "facebook", "xhs", "douyin", "kuaishou", "wechat_video", "wechat_mp", "csdn", "youtube", "tiktok":
		return true
	default:
		return false
	}
}
