package ui

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	apperrors "alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/queue"
	"alpha-nexus-egolite/internal/trace"
)

type publishJobResponse struct {
	ID                int64  `json:"id"`
	BatchID           int64  `json:"batch_id"`
	PlanID            int64  `json:"plan_id"`
	PlanTargetID      int64  `json:"plan_target_id"`
	ContentID         int64  `json:"content_id"`
	AccountID         int64  `json:"account_id"`
	AssetID           int64  `json:"asset_id"`
	PlatformCode      string `json:"platform_code"`
	ContentType       string `json:"content_type"`
	Mode              string `json:"mode"`
	Status            string `json:"status"`
	CurrentStep       string `json:"current_step"`
	RetryCount        int    `json:"retry_count"`
	MaxRetries        int    `json:"max_retries"`
	ErrorCode         string `json:"error_code"`
	ErrorMessage      string `json:"error_message"`
	TraceID           string `json:"trace_id"`
	CancelRequestedAt string `json:"cancel_requested_at"`
	SnapshotHash      string `json:"snapshot_hash,omitempty"`
	SubmitAttempted   bool   `json:"submit_attempted"`
	SubmitOutcome     string `json:"submit_outcome,omitempty"`
	CreatedAt         string `json:"created_at"`
	CanCancel         bool   `json:"can_cancel"`
}

type publishJobEventResponse struct {
	ID               int64                 `json:"id"`
	JobID            int64                 `json:"job_id"`
	Level            string                `json:"level"`
	Step             string                `json:"step,omitempty"`
	EventType        string                `json:"event_type"`
	Message          string                `json:"message,omitempty"`
	TraceID          string                `json:"trace_id,omitempty"`
	RequestID        string                `json:"request_id,omitempty"`
	TaskID           string                `json:"task_id,omitempty"`
	DurationMS       int64                 `json:"duration_ms"`
	ErrorCode        string                `json:"error_code,omitempty"`
	ErrorClass       string                `json:"error_class,omitempty"`
	Retryable        bool                  `json:"retryable"`
	SchemaVersion    int                   `json:"schema_version"`
	ScreenshotSaved  bool                  `json:"screenshot_saved"`
	DOMSnapshotSaved bool                  `json:"dom_snapshot_saved"`
	Diagnostics      publishJobDiagnostics `json:"diagnostics,omitempty"`
	CreatedAt        string                `json:"created_at"`
}

// publishJobDiagnostics is an allowlist projection of Worker evidence. It is
// intentionally field-name-only: paths, DOM, captions, and raw event JSON
// remain local diagnostics and never enter the UI response.
type publishJobDiagnostics struct {
	MissingRequiredControls                 []string `json:"missing_required_controls,omitempty"`
	UnverifiedFields                        []string `json:"unverified_fields,omitempty"`
	SettingsApplied                         []string `json:"settings_applied,omitempty"`
	SettingsVerified                        []string `json:"settings_verified,omitempty"`
	SettingsNotPresent                      []string `json:"settings_not_present,omitempty"`
	ArticleCoverSelectionVerified           *bool    `json:"article_cover_selection_verified,omitempty"`
	ArticleCoverPreviewCommitted            *bool    `json:"article_cover_preview_committed,omitempty"`
	ContentImagesUploadedSelectionVerified  *bool    `json:"content_images_uploaded_selection_verified,omitempty"`
	ContentImagesUploadedPreviewObserved    *bool    `json:"content_images_uploaded_preview_observed,omitempty"`
	ContentImagesUploadedProgressCleared    *bool    `json:"content_images_uploaded_progress_cleared,omitempty"`
	ContentImagesUploadedErrorFree          *bool    `json:"content_images_uploaded_error_free,omitempty"`
	ContentImagesUploadedScopeVerified      *bool    `json:"content_images_uploaded_scope_verified,omitempty"`
	ContentImagesUploadedCompletionStable   *bool    `json:"content_images_uploaded_completion_stable,omitempty"`
	ContentImagesUploadedBaselineMediaCount *int     `json:"content_images_uploaded_baseline_media_count,omitempty"`
	ContentImagesUploadedPreviewMediaCount  *int     `json:"content_images_uploaded_preview_media_count,omitempty"`
	ContentImagesUploadedPreviewIncrement   *int     `json:"content_images_uploaded_preview_increment,omitempty"`
	ContentImagesUploadedExpectedItemCount  *int     `json:"content_images_uploaded_expected_item_count,omitempty"`
}

type publishJobDetailResponse struct {
	publishJobResponse
	CanRetry                bool                      `json:"can_retry"`
	RetryBlockedBySubmit    bool                      `json:"retry_blocked_by_submit"`
	CreatorBackendVerified  bool                      `json:"creator_backend_verified"`
	EvidenceScreenshotSaved bool                      `json:"evidence_screenshot_saved"`
	Events                  []publishJobEventResponse `json:"events"`
}

type publishJobActionResponse struct {
	Action      string                   `json:"action"`
	QueueTaskID int64                    `json:"queue_task_id,omitempty"`
	Job         publishJobDetailResponse `json:"job"`
}

func canCancelPublishJob(status string) bool {
	switch status {
	case "pending", "queued", "running", "manual_takeover", "awaiting_review":
		return true
	default:
		return false
	}
}

func newPublishJobResponse(job *db.PublishJob) publishJobResponse {
	return publishJobResponse{
		ID: job.ID, BatchID: job.BatchID, PlanID: job.PlanID, PlanTargetID: job.PlanTargetID,
		ContentID: job.ContentID, AccountID: job.AccountID,
		AssetID: job.AssetID, PlatformCode: job.PlatformCode, ContentType: job.ContentType,
		Mode: job.Mode, Status: job.Status, CurrentStep: job.CurrentStep,
		RetryCount: job.RetryCount, MaxRetries: job.MaxRetries, ErrorCode: job.ErrorCode,
		ErrorMessage: job.ErrorMessage, TraceID: job.TraceID,
		CancelRequestedAt: job.CancelRequestedAt, SnapshotHash: job.SnapshotHash,
		SubmitAttempted: job.SubmitAttemptedAt != "", SubmitOutcome: job.SubmitOutcome,
		CreatedAt: job.CreatedAt,
		CanCancel: canCancelPublishJob(job.Status) && job.CancelRequestedAt == "" && job.SubmitAttemptedAt == "",
	}
}

func newPublishJobEventResponse(event db.JobEvent) publishJobEventResponse {
	return publishJobEventResponse{
		ID: event.ID, JobID: event.JobID, Level: event.Level, Step: event.Step,
		EventType: event.EventType, Message: event.Message, TraceID: event.TraceID,
		RequestID: event.RequestID, TaskID: event.TaskID, DurationMS: event.DurationMS,
		ErrorCode: event.ErrorCode, ErrorClass: event.ErrorClass, Retryable: event.Retryable,
		SchemaVersion: event.SchemaVersion, ScreenshotSaved: event.ScreenshotPath != "",
		DOMSnapshotSaved: event.DOMSnapshotPath != "", Diagnostics: summarizePublishJobDiagnostics(event.RawJSON), CreatedAt: event.CreatedAt,
	}
}

func summarizePublishJobDiagnostics(raw string) publishJobDiagnostics {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return publishJobDiagnostics{}
	}
	var evidence map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &evidence) != nil {
		return publishJobDiagnostics{}
	}
	values := func(key string) []string {
		var source []string
		if json.Unmarshal(evidence[key], &source) != nil {
			return nil
		}
		result := make([]string, 0, len(source))
		seen := make(map[string]bool, len(source))
		for _, value := range source {
			value = strings.TrimSpace(value)
			if value == "" || len(value) > 96 || seen[value] || !safeDiagnosticToken(value) {
				continue
			}
			seen[value] = true
			result = append(result, value)
			if len(result) == 20 {
				break
			}
		}
		return result
	}
	boolean := func(key string) *bool {
		var value bool
		if json.Unmarshal(evidence[key], &value) != nil {
			return nil
		}
		return &value
	}
	count := func(key string) *int {
		var value int
		if json.Unmarshal(evidence[key], &value) != nil || value < 0 || value > 9 {
			return nil
		}
		return &value
	}
	return publishJobDiagnostics{
		MissingRequiredControls:                 values("missingRequiredControls"),
		UnverifiedFields:                        values("unverifiedFields"),
		SettingsApplied:                         values("settingsApplied"),
		SettingsVerified:                        values("settingsVerified"),
		SettingsNotPresent:                      values("settingsNotPresent"),
		ArticleCoverSelectionVerified:           boolean("articleCoverSelectionVerified"),
		ArticleCoverPreviewCommitted:            boolean("articleCoverPreviewCommitted"),
		ContentImagesUploadedSelectionVerified:  boolean("contentImagesUploadedSelectionVerified"),
		ContentImagesUploadedPreviewObserved:    boolean("contentImagesUploadedPreviewObserved"),
		ContentImagesUploadedProgressCleared:    boolean("contentImagesUploadedProgressCleared"),
		ContentImagesUploadedErrorFree:          boolean("contentImagesUploadedErrorFree"),
		ContentImagesUploadedScopeVerified:      boolean("contentImagesUploadedScopeVerified"),
		ContentImagesUploadedCompletionStable:   boolean("contentImagesUploadedCompletionStable"),
		ContentImagesUploadedBaselineMediaCount: count("contentImagesUploadedBaselineMediaCount"),
		ContentImagesUploadedPreviewMediaCount:  count("contentImagesUploadedPreviewMediaCount"),
		ContentImagesUploadedPreviewIncrement:   count("contentImagesUploadedPreviewIncrement"),
		ContentImagesUploadedExpectedItemCount:  count("contentImagesUploadedExpectedItemCount"),
	}
}

func safeDiagnosticToken(value string) bool {
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func newPublishJobEventResponses(events []db.JobEvent) []publishJobEventResponse {
	response := make([]publishJobEventResponse, 0, len(events))
	for _, event := range events {
		response = append(response, newPublishJobEventResponse(event))
	}
	return response
}

func (s *Server) newPublishJobDetail(job *db.PublishJob) (publishJobDetailResponse, error) {
	canRetry, err := s.db.CanRetryPublishJob(job.ID)
	if err != nil {
		return publishJobDetailResponse{}, err
	}
	events, err := s.db.ListJobEventsByJob(job.ID)
	if err != nil {
		return publishJobDetailResponse{}, err
	}
	redactedEvents := newPublishJobEventResponses(events)
	verified := false
	evidenceScreenshotSaved := false
	for _, event := range redactedEvents {
		if event.EventType == "job.creator_backend_verified" {
			verified = true
			evidenceScreenshotSaved = evidenceScreenshotSaved || event.ScreenshotSaved
		}
	}
	return publishJobDetailResponse{
		publishJobResponse: newPublishJobResponse(job), CanRetry: canRetry,
		RetryBlockedBySubmit:   job.SubmitAttemptedAt != "" && !canRetry,
		CreatorBackendVerified: verified, EvidenceScreenshotSaved: evidenceScreenshotSaved,
		Events: redactedEvents,
	}, nil
}

func parsePublishJobID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid publish job id")
	}
	return id, nil
}

func writePublishJobError(w http.ResponseWriter, err error) {
	switch {
	case stderrors.Is(err, db.ErrPublishJobNotFound):
		http.Error(w, "publish job not found", http.StatusNotFound)
	case stderrors.Is(err, db.ErrPublishJobConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, "publish job operation failed", http.StatusInternalServerError)
	}
}

func (s *Server) handleAPIJobDetail(w http.ResponseWriter, r *http.Request) {
	jobID, err := parsePublishJobID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job, err := s.db.GetPublishJob(jobID)
	if err != nil {
		writePublishJobError(w, err)
		return
	}
	detail, err := s.newPublishJobDetail(job)
	if err != nil {
		writePublishJobError(w, err)
		return
	}
	writeJSON(w, detail)
}

func (s *Server) handleAPIJobCancel(w http.ResponseWriter, r *http.Request) {
	jobID, err := parsePublishJobID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	action, job, err := s.db.RequestPublishJobCancel(jobID)
	if err != nil {
		writePublishJobError(w, err)
		return
	}
	detail, err := s.newPublishJobDetail(job)
	if err != nil {
		writePublishJobError(w, err)
		return
	}
	if action == "cancel_requested" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
	}
	writeJSON(w, publishJobActionResponse{Action: action, Job: detail})
}

func (s *Server) handleAPIJobRetry(w http.ResponseWriter, r *http.Request) {
	jobID, err := parsePublishJobID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	traceID := string(trace.New())
	job, task, err := s.db.RetryPublishJob(jobID, traceID)
	if err != nil {
		writePublishJobError(w, err)
		return
	}
	detail, err := s.newPublishJobDetail(job)
	if err != nil {
		writePublishJobError(w, err)
		return
	}
	s.startPublishQueueTask(task.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, publishJobActionResponse{Action: "queued", QueueTaskID: task.ID, Job: detail})
}

type retryPublishQueuePayload struct {
	ExistingJobID int64 `json:"existing_job_id"`
}

func (s *Server) startPublishQueueTask(taskID int64) {
	if s.batchCtx == nil || taskID <= 0 {
		return
	}
	s.batchWG.Add(1)
	go func() {
		defer s.batchWG.Done()
		worker := &queue.DBWorker{
			Store: s.db, Owner: fmt.Sprintf("ui-retry:%d:%d", os.Getpid(), taskID), LeaseTTL: time.Minute,
			Handler: s.runPublishQueueTask,
		}
		_ = worker.RunTask(s.batchCtx, taskID)
	}()
}

func (s *Server) runPublishQueueTask(ctx context.Context, task *db.TaskQueueItem) error {
	var payload retryPublishQueuePayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil || payload.ExistingJobID <= 0 {
		return fmt.Errorf("invalid retry publish payload")
	}
	job, err := s.db.GetPublishJob(payload.ExistingJobID)
	if err != nil {
		return err
	}
	if job.Status == "cancelled" {
		return nil
	}
	if job.Status != "pending" {
		return fmt.Errorf("publish job %d is not pending", job.ID)
	}
	var content *db.Content
	var asset *db.AccountAsset
	var mediaPath, publishAt, complianceJSON string
	var publishMedia []platforms.PublishMedia
	if job.PlanID > 0 {
		frozen, frozenErr := (&publishplan.Service{DB: s.db}).FrozenInputForJob(job)
		if frozenErr != nil {
			return publishguard.FailPendingJob(s.db, job, "frozen_snapshot_preflight", task.TraceID, frozenErr, false)
		}
		content, asset = frozen.Content, frozen.Asset
		mediaPath, publishMedia, publishAt, complianceJSON = frozen.MediaPath, frozen.Media, frozen.PublishAt, frozen.ComplianceJSON
	} else {
		content, err = s.db.GetContent(job.ContentID)
		if err != nil {
			return err
		}
		asset, err = s.publishJobAsset(job)
		if err != nil {
			return err
		}
		mediaList, mediaErr := s.db.ListMediaByContent(content.ID)
		mediaPath, publishMedia, mediaErr = publishguard.PublishMediaPayload(mediaList, mediaErr, content.Type)
		if mediaErr != nil {
			return publishguard.FailPendingJob(s.db, job, "media_preflight", task.TraceID, mediaErr, false)
		}
	}
	if targetErr := publishguard.ValidateTargetOwnership(s.db, asset, job.PlatformCode); targetErr != nil {
		return publishguard.FailPendingJob(s.db, job, "target_preflight", task.TraceID, targetErr, false)
	}
	dryRun, autoSubmit := publish.IntentFromMode(job.Mode)
	cdpURL := ""
	if !dryRun {
		if s.facade == nil {
			err := fmt.Errorf("EGO_SPACE_REQUIRED")
			return publishguard.FailPendingJob(s.db, job, "ego_space_preflight", task.TraceID, err, true)
		}
		release, leaseErr := s.acquireBrowserOperationLease(asset.AccountID, "publish", uiBrowserPublishLeaseTTL)
		if leaseErr != nil {
			if errorCodeOf(leaseErr) == string(apperrors.BROWSER_OPERATION_BUSY) {
				stableErr := fmt.Errorf("%s", apperrors.BROWSER_OPERATION_BUSY)
				return publishguard.FailPendingJob(s.db, job, "browser_operation_lease", task.TraceID, stableErr, true)
			}
			return leaseErr
		}
		defer release()
		status, statusErr := s.facade.Status(asset.AccountID)
		cdpURL, statusErr = publishguard.LiveCDPURL(asset.AccountID, status, statusErr)
		if statusErr != nil {
			return publishguard.FailPendingJob(s.db, job, "browser_preflight", task.TraceID, statusErr, true)
		}
	}
	credentialRef, _ := s.db.GetCredentialRef(asset.AccountID, job.PlatformCode)
	_, err = (&publish.Runner{Store: s.db}).Run(ctx, publish.RunInput{
		Job: job, Content: content, Asset: asset, CDPURL: cdpURL, DryRun: dryRun,
		AutoSubmit: autoSubmit, MediaPath: mediaPath, Media: publishMedia, CredentialRef: credentialRef,
		TraceID: task.TraceID, TaskID: strconv.FormatInt(task.ID, 10), PublishAt: publishAt,
		ComplianceJSON: complianceJSON,
	})
	return err
}

func (s *Server) publishJobAsset(job *db.PublishJob) (*db.AccountAsset, error) {
	if job.AssetID > 0 {
		return s.db.GetAccountAsset(job.AssetID)
	}
	assets, err := s.db.ListAccountAssetsByAccount(job.AccountID)
	if err != nil {
		return nil, err
	}
	for index := range assets {
		if assets[index].Enabled && assets[index].PlatformCode == job.PlatformCode {
			return &assets[index], nil
		}
	}
	return nil, fmt.Errorf("publish job account asset not found")
}
