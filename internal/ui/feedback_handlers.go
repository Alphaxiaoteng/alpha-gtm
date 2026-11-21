package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/feedback"
)

type feedbackSubmitBody struct {
	Content       string `json:"content"`
	CurrentModule string `json:"current_module"`
}

type feedbackListResponse struct {
	Configured bool                 `json:"configured"`
	Items      []db.FeedbackReceipt `json:"items"`
	SyncError  string               `json:"sync_error,omitempty"`
}

func (s *Server) handleAPIFeedback(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIFeedbackList(w, r)
	case http.MethodPost:
		s.handleAPIFeedbackSubmit(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIFeedbackSubmit(w http.ResponseWriter, r *http.Request) {
	if s.feedbackClient == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{
			"error_code": "FEEDBACK_NOT_CONFIGURED",
		})
		return
	}
	configured, configErr := s.feedbackClient.Configured()
	if configErr != nil || !configured {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{
			"error_code": "FEEDBACK_NOT_CONFIGURED",
		})
		return
	}
	var body feedbackSubmitBody
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error_code": "FEEDBACK_INVALID_JSON"})
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	body.CurrentModule = strings.TrimSpace(body.CurrentModule)
	contentLength := utf8.RuneCountInString(body.Content)
	if contentLength < 2 || contentLength > 2000 || utf8.RuneCountInString(body.CurrentModule) > 64 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error_code": "FEEDBACK_INVALID_INPUT"})
		return
	}
	s.serverMu.Lock()
	version := s.health.Version
	s.serverMu.Unlock()
	submitRequest := feedback.SubmitRequest{
		Content:         body.Content,
		AppVersion:      version,
		CurrentModule:   body.CurrentModule,
		OperatingSystem: runtime.GOOS,
		Channel:         "electron",
	}
	requestHash := feedbackSubmissionRequestHash(body.Content, body.CurrentModule)
	prepared, err := feedback.PrepareSubmitRequest(submitRequest)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error_code": "FEEDBACK_LOCAL_STORE_FAILED"})
		return
	}
	intent, err := s.db.GetOrCreateFeedbackSubmissionIntent(db.FeedbackSubmissionIntent{
		RequestHash:    requestHash,
		ClientToken:    prepared.ClientToken,
		FeedbackNumber: prepared.FeedbackNumber,
		ReadToken:      prepared.ReadToken,
	})
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error_code": "FEEDBACK_LOCAL_STORE_FAILED"})
		return
	}
	submitRequest.ClientToken = intent.ClientToken
	submitRequest.FeedbackNumber = intent.FeedbackNumber
	submitRequest.ReadToken = intent.ReadToken
	ctx, cancel := contextWithTimeout(r, 10*time.Second)
	defer cancel()
	receipt, err := s.feedbackClient.Submit(ctx, submitRequest)
	if err != nil {
		status, code := feedbackHTTPError(err)
		writeJSONStatus(w, status, map[string]string{"error_code": code})
		return
	}
	item := db.FeedbackReceipt{
		FeedbackID:     receipt.FeedbackID,
		ReadToken:      intent.ReadToken,
		Content:        body.Content,
		Status:         receipt.Status,
		Category:       receipt.Category,
		DeveloperReply: receipt.Reply,
		CreatedAt:      receipt.CreatedAt,
		UpdatedAt:      receipt.UpdatedAt,
		LastSyncedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.CompleteFeedbackSubmission(requestHash, item); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error_code": "FEEDBACK_LOCAL_STORE_FAILED"})
		return
	}
	writeJSONStatus(w, http.StatusCreated, item)
}

func feedbackSubmissionRequestHash(content, currentModule string) string {
	payload, _ := json.Marshal([2]string{strings.TrimSpace(content), strings.TrimSpace(currentModule)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func feedbackHTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, feedback.ErrNotConfigured),
		errors.Is(err, feedback.ErrKeychainUnavailable),
		errors.Is(err, feedback.ErrKeychainReadFailed):
		return http.StatusServiceUnavailable, "FEEDBACK_NOT_CONFIGURED"
	case errors.Is(err, feedback.ErrSchemaMismatch):
		return http.StatusBadGateway, "FEEDBACK_SCHEMA_MISMATCH"
	case errors.Is(err, feedback.ErrInvalidInput):
		return http.StatusBadRequest, "FEEDBACK_INVALID_INPUT"
	default:
		return http.StatusBadGateway, "FEEDBACK_REMOTE_UNAVAILABLE"
	}
}

func (s *Server) handleAPIFeedbackList(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.ListFeedbackReceipts(100)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error_code": "FEEDBACK_LOCAL_READ_FAILED"})
		return
	}
	configured := false
	if s.feedbackClient != nil {
		configured, _ = s.feedbackClient.Configured()
	}
	response := feedbackListResponse{Configured: configured, Items: items}
	if r.URL.Query().Get("refresh") == "1" && configured && len(items) > 0 {
		credentials := make([]feedback.Credential, 0, len(items))
		for _, item := range items {
			credentials = append(credentials, feedback.Credential{
				FeedbackID: item.FeedbackID,
				ReadToken:  item.ReadToken,
			})
		}
		ctx, cancel := contextWithTimeout(r, 10*time.Second)
		statuses, syncErr := s.feedbackClient.Status(ctx, credentials)
		cancel()
		if syncErr != nil {
			response.SyncError = "FEEDBACK_SYNC_UNAVAILABLE"
		} else {
			syncedAt := time.Now().UTC().Format(time.RFC3339)
			for _, status := range statuses {
				if err := s.db.UpdateFeedbackReceiptStatus(
					status.FeedbackID, status.Status, status.Category, status.Reply, status.UpdatedAt, syncedAt,
				); err != nil {
					continue
				}
			}
			items, err = s.db.ListFeedbackReceipts(100)
			if err == nil {
				response.Items = items
			}
		}
	}
	writeJSON(w, response)
}

func contextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), timeout)
}
