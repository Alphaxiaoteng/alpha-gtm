package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/batch"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/trace"
	// Register meta publishers.
	_ "alpha-nexus-egolite/internal/platforms/meta"
)

// Store abstracts job persistence used by Runner.
type Store interface {
	ClaimPublishJob(id int64, traceID string) (bool, error)
	ValidatePublishJobMode(id int64, mode string) error
	ClaimBrowserRuntimeJob(accountID, jobID int64) (bool, error)
	VerifyBrowserRuntimeJob(accountID, jobID int64) (bool, error)
	ReleaseBrowserRuntimeJob(accountID, jobID int64) (bool, error)
	TouchPublishJob(id int64) (bool, error)
	SetPublishJobSettings(id int64, settingsJSON string) error
	MarkPublishSubmitAttempt(id int64) error
	SetPublishSubmitOutcome(id int64, outcome string) error
	UpdatePublishJob(id int64, status, currentStep, errorCode, errorMessage string, retryCount int) error
	CreateJobEventDetailed(db.JobEventInput) (*db.JobEvent, error)
}

// Runner executes one publish job through the state machine + platform adapter.
type Runner struct {
	Store Store
}

type notificationStore interface {
	CreateNotification(kind, level, title, body, relatedType string, relatedID int64) (*db.Notification, error)
}

type cancellationStore interface {
	IsPublishJobCancelRequested(id int64) (bool, error)
}

type finalizationStore interface {
	FinalizePublishJob(id int64, status, currentStep, errorCode, errorMessage string, retryCount int) (bool, error)
}

type accountOwnershipStore interface {
	GetAccount(id int64) (*db.Account, error)
}

type accountAssetLookupStore interface {
	GetAccountAsset(id int64) (*db.AccountAsset, error)
}

const (
	cancelPollInterval     = 200 * time.Millisecond
	publishHeartbeatPeriod = 15 * time.Second
)

// notify is deliberately best-effort: publish job_events remain the durable
// execution truth even if a presentation notification cannot be stored.
func (r *Runner) notify(kind, level, title, body string, jobID int64) {
	if store, ok := r.Store.(notificationStore); ok {
		_, _ = store.CreateNotification(kind, level, title, body, "publish_job", jobID)
	}
}

// RunInput is assembled by CLI/UI.
type RunInput struct {
	Job        *db.PublishJob
	Content    *db.Content
	Asset      *db.AccountAsset
	CDPURL     string
	DryRun     bool
	AutoSubmit bool
	// SubmitPreparedOnly preserves the page prepared by the immediately
	// preceding review-only run. It is never accepted without AutoSubmit.
	SubmitPreparedOnly bool
	MediaPath          string
	Media              []platforms.PublishMedia
	Publisher          platforms.Publisher
	EmitNDJSON         bool
	CredentialRef      string // API platforms: vault/keychain ref only
	TraceID            string
	RequestID          string
	TaskID             string
	PublishAt          string
	ComplianceJSON     string
}

// RunResult is the terminal outcome.
type RunResult struct {
	Status       Status
	Step         string
	Message      string
	Result       *platforms.PublishResult
	WarningCodes []string
}

func (r *Runner) emit(enabled bool, event map[string]interface{}) {
	if enabled {
		batch.EmitNDJSON(event)
	}
}

func (r *Runner) set(jobID int64, st Status, step, code, msg string, retry int) error {
	return r.Store.UpdatePublishJob(jobID, databaseStatus(st), step, code, msg, retry)
}

// databaseStatus maps machine states onto the persisted compatibility states.
func databaseStatus(st Status) string {
	dbStatus := string(st)
	switch st {
	case StatusPublished, StatusDryRunOK:
		dbStatus = "success"
	case StatusRetryableFailed, StatusPermanentFailed:
		dbStatus = "failed"
	case StatusManualTakeover:
		dbStatus = "manual_takeover"
	case StatusAwaitingReview:
		dbStatus = "awaiting_review"
	case StatusQueued, StatusPreflight, StatusBrowserAttached, StatusAPIAuthenticated,
		StatusAssetProcessing, StatusAssetUploaded, StatusMetadataFilled, StatusSubmitting:
		dbStatus = "running"
	}
	return dbStatus
}

func (r *Runner) persistJob(jobID int64, st Status, step, code, msg string, retry int) error {
	if err := r.set(jobID, st, step, code, msg, retry); err != nil {
		return fmt.Errorf("JOB_PERSIST_FAILED: %w", err)
	}
	return nil
}

func (r *Runner) persistEvent(event db.JobEventInput) error {
	if _, err := r.Store.CreateJobEventDetailed(event); err != nil {
		return fmt.Errorf("EVENT_PERSIST_FAILED: %w", err)
	}
	return nil
}

func safePublishMessage(st Status, code string) string {
	switch st {
	case StatusPublished:
		return "publish completed"
	case StatusSubmittedPendingVerification:
		return "submission accepted; creator backend verification pending"
	case StatusLoginRequired:
		return "publish requires login"
	case StatusResultUnknown:
		return "publish result requires creator backend verification"
	case StatusDryRunOK:
		return "dry run completed"
	case StatusManualTakeover, StatusAwaitingReview:
		if code == "PLATFORM_VERIFICATION_REQUIRED" {
			return "publish stopped because the platform requires human verification"
		}
		if code == "PUBLISH_VERIFICATION_REQUIRED" {
			return "creator backend verification required"
		}
		return "manual review required"
	case StatusCancelled:
		return "publish cancelled"
	default:
		if code == "" {
			code = "PUBLISH_FAILED"
		}
		return fmt.Sprintf("publish failed (%s)", code)
	}
}

func isLoginRequiredPublishError(code string) bool {
	switch strings.TrimSpace(code) {
	case "LOGIN_REQUIRED", "PLATFORM_VERIFICATION_REQUIRED",
		"PUBLISH_ACCOUNT_NOT_LOGGED_IN", "PUBLISH_ACCOUNT_SESSION_LOGGED_OUT":
		return true
	default:
		return false
	}
}

func evidenceBool(evidence map[string]interface{}, key string) bool {
	value, ok := evidence[key].(bool)
	return ok && value
}

// compactPublishEvidence persists only stable field identifiers. Worker page
// inspection can include drafts, DOM text, and large screenshot metadata;
// those stay in the local worker boundary and must never make job events fail
// to persist or leak through diagnostics.
func compactPublishEvidence(evidence map[string]interface{}) string {
	if len(evidence) == 0 {
		return ""
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return ""
	}
	var fields struct {
		MissingRequiredControls                 []string        `json:"missingRequiredControls"`
		UnverifiedFields                        []string        `json:"unverifiedFields"`
		SettingsApplied                         []string        `json:"settingsApplied"`
		SettingsVerified                        []string        `json:"settingsVerified"`
		SettingsNotPresent                      []string        `json:"settingsNotPresent"`
		ArticleCoverSelectionVerified           *bool           `json:"articleCoverSelectionVerified"`
		ArticleCoverPreviewCommitted            *bool           `json:"articleCoverPreviewCommitted"`
		ArticleCoverNativeUploadObserved        *bool           `json:"articleCoverNativeUploadObserved"`
		ArticleCoverNativeUploadStatuses        []int           `json:"articleCoverNativeUploadStatuses"`
		SubmitAttempted                         *bool           `json:"submitAttempted"`
		SubmitClicked                           *bool           `json:"submitClicked"`
		SubmitClickedAt                         string          `json:"submitClickedAt"`
		SubmitTargetID                          string          `json:"submitTargetId"`
		CriticalRevalidation                    map[string]bool `json:"criticalRevalidation"`
		ReviewedComposerLandmarks               map[string]bool `json:"reviewedComposerLandmarks"`
		EntryClicked                            *bool           `json:"entryClicked"`
		EntryFallbackUsed                       *bool           `json:"entryFallbackUsed"`
		IntermediateReached                     *bool           `json:"intermediateReached"`
		ContentImagesUploadedSelectionVerified  *bool           `json:"contentImagesUploadedSelectionVerified"`
		ContentImagesUploadedPreviewObserved    *bool           `json:"contentImagesUploadedPreviewObserved"`
		ContentImagesUploadedProgressCleared    *bool           `json:"contentImagesUploadedProgressCleared"`
		ContentImagesUploadedErrorFree          *bool           `json:"contentImagesUploadedErrorFree"`
		ContentImagesUploadedScopeVerified      *bool           `json:"contentImagesUploadedScopeVerified"`
		ContentImagesUploadedCompletionStable   *bool           `json:"contentImagesUploadedCompletionStable"`
		ContentImagesUploadedBaselineMediaCount *int            `json:"contentImagesUploadedBaselineMediaCount"`
		ContentImagesUploadedPreviewMediaCount  *int            `json:"contentImagesUploadedPreviewMediaCount"`
		ContentImagesUploadedPreviewIncrement   *int            `json:"contentImagesUploadedPreviewIncrement"`
		ContentImagesUploadedExpectedItemCount  *int            `json:"contentImagesUploadedExpectedItemCount"`
		VideoUploadedSelectionVerified          *bool           `json:"videoUploadedSelectionVerified"`
		VideoUploadedPreviewObserved            *bool           `json:"videoUploadedPreviewObserved"`
		VideoUploadedProgressCleared            *bool           `json:"videoUploadedProgressCleared"`
		VideoUploadedErrorFree                  *bool           `json:"videoUploadedErrorFree"`
		VideoUploadedScopeVerified              *bool           `json:"videoUploadedScopeVerified"`
		VideoUploadedCompletionStable           *bool           `json:"videoUploadedCompletionStable"`
		VideoUploadedBaselineMediaCount         *int            `json:"videoUploadedBaselineMediaCount"`
		VideoUploadedPreviewMediaCount          *int            `json:"videoUploadedPreviewMediaCount"`
		VideoUploadedPreviewIncrement           *int            `json:"videoUploadedPreviewIncrement"`
		VideoUploadedExpectedItemCount          *int            `json:"videoUploadedExpectedItemCount"`
	}
	if json.Unmarshal(encoded, &fields) != nil {
		return ""
	}
	output := map[string]interface{}{}
	for key, values := range map[string][]string{
		"missingRequiredControls": fields.MissingRequiredControls,
		"unverifiedFields":        fields.UnverifiedFields,
		"settingsApplied":         fields.SettingsApplied,
		"settingsVerified":        fields.SettingsVerified,
		"settingsNotPresent":      fields.SettingsNotPresent,
	} {
		if safe := compactEvidenceTokens(values); len(safe) > 0 {
			output[key] = safe
		}
	}
	if fields.ArticleCoverSelectionVerified != nil {
		output["articleCoverSelectionVerified"] = *fields.ArticleCoverSelectionVerified
	}
	if fields.ArticleCoverPreviewCommitted != nil {
		output["articleCoverPreviewCommitted"] = *fields.ArticleCoverPreviewCommitted
	}
	if fields.ArticleCoverNativeUploadObserved != nil {
		output["articleCoverNativeUploadObserved"] = *fields.ArticleCoverNativeUploadObserved
	}
	if statuses := compactHTTPStatuses(fields.ArticleCoverNativeUploadStatuses); len(statuses) > 0 {
		output["articleCoverNativeUploadStatuses"] = statuses
	}
	if fields.SubmitAttempted != nil {
		output["submitAttempted"] = *fields.SubmitAttempted
	}
	if fields.SubmitClicked != nil {
		output["submitClicked"] = *fields.SubmitClicked
	}
	if fields.SubmitClicked != nil && *fields.SubmitClicked && validSubmitClickEvidence(fields.SubmitClickedAt, fields.SubmitTargetID) {
		output["submitClickedAt"] = fields.SubmitClickedAt
		output["submitTargetId"] = fields.SubmitTargetID
	}
	if critical := compactEvidenceBoolMap(fields.CriticalRevalidation); len(critical) > 0 {
		output["criticalRevalidation"] = critical
	}
	if landmarks := compactEvidenceBoolMap(fields.ReviewedComposerLandmarks); len(landmarks) > 0 {
		output["reviewedComposerLandmarks"] = landmarks
	}
	for key, value := range map[string]*bool{
		"entryClicked":        fields.EntryClicked,
		"entryFallbackUsed":   fields.EntryFallbackUsed,
		"intermediateReached": fields.IntermediateReached,
	} {
		if value != nil {
			output[key] = *value
		}
	}
	for key, value := range map[string]*bool{
		"contentImagesUploadedSelectionVerified": fields.ContentImagesUploadedSelectionVerified,
		"contentImagesUploadedPreviewObserved":   fields.ContentImagesUploadedPreviewObserved,
		"contentImagesUploadedProgressCleared":   fields.ContentImagesUploadedProgressCleared,
		"contentImagesUploadedErrorFree":         fields.ContentImagesUploadedErrorFree,
		"contentImagesUploadedScopeVerified":     fields.ContentImagesUploadedScopeVerified,
		"contentImagesUploadedCompletionStable":  fields.ContentImagesUploadedCompletionStable,
		"videoUploadedSelectionVerified":         fields.VideoUploadedSelectionVerified,
		"videoUploadedPreviewObserved":           fields.VideoUploadedPreviewObserved,
		"videoUploadedProgressCleared":           fields.VideoUploadedProgressCleared,
		"videoUploadedErrorFree":                 fields.VideoUploadedErrorFree,
		"videoUploadedScopeVerified":             fields.VideoUploadedScopeVerified,
		"videoUploadedCompletionStable":          fields.VideoUploadedCompletionStable,
	} {
		if value != nil {
			output[key] = *value
		}
	}
	for key, value := range map[string]*int{
		"contentImagesUploadedBaselineMediaCount": fields.ContentImagesUploadedBaselineMediaCount,
		"contentImagesUploadedPreviewMediaCount":  fields.ContentImagesUploadedPreviewMediaCount,
		"contentImagesUploadedPreviewIncrement":   fields.ContentImagesUploadedPreviewIncrement,
		"contentImagesUploadedExpectedItemCount":  fields.ContentImagesUploadedExpectedItemCount,
		"videoUploadedBaselineMediaCount":         fields.VideoUploadedBaselineMediaCount,
		"videoUploadedPreviewMediaCount":          fields.VideoUploadedPreviewMediaCount,
		"videoUploadedPreviewIncrement":           fields.VideoUploadedPreviewIncrement,
		"videoUploadedExpectedItemCount":          fields.VideoUploadedExpectedItemCount,
	} {
		if value != nil && *value >= 0 && *value <= 9 {
			output[key] = *value
		}
	}
	if len(output) == 0 {
		return ""
	}
	encoded, err = json.Marshal(output)
	if err != nil || len(encoded) > 8<<10 {
		return ""
	}
	return string(encoded)
}

func compactEvidenceTokens(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 96 || seen[value] || !safeEvidenceToken(value) {
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

func compactHTTPStatuses(values []int) []int {
	result := make([]int, 0, len(values))
	seen := make(map[int]bool, len(values))
	for _, value := range values {
		if value < 100 || value > 599 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) == 3 {
			break
		}
	}
	return result
}

func safeEvidenceToken(value string) bool {
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func compactEvidenceBoolMap(values map[string]bool) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]bool, len(values))
	for key, value := range values {
		if safeEvidenceToken(key) {
			result[key] = value
		}
		if len(result) == 20 {
			break
		}
	}
	return result
}

func validSubmitClickEvidence(clickedAt, targetID string) bool {
	if _, err := time.Parse(time.RFC3339Nano, clickedAt); err != nil || len(targetID) != 32 {
		return false
	}
	for _, char := range targetID {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

func (r *Runner) persistEarlyFailure(jobID int64, step, code, message, traceID, requestID, taskID string, started time.Time, retryable bool) error {
	status := StatusPermanentFailed
	eventType := "job.permanent_failed"
	if retryable {
		status = StatusRetryableFailed
		eventType = "job.retryable_failed"
	}
	if err := r.persistJob(jobID, status, step, code, message, 0); err != nil {
		return err
	}
	return r.persistEvent(db.JobEventInput{
		JobID: jobID, Level: "error", Step: step, Type: eventType,
		Message: message, TraceID: traceID, RequestID: requestID, TaskID: taskID,
		DurationMS: time.Since(started).Milliseconds(), ErrorCode: code,
		ErrorClass: observability.ErrorClass(code), Retryable: retryable,
	})
}

func (r *Runner) persistCancellation(job *db.PublishJob, traceID, requestID, taskID string, started time.Time) (*RunResult, error) {
	const step = "cancelled"
	message := safePublishMessage(StatusCancelled, "")
	if err := r.persistJob(job.ID, StatusCancelled, step, "", "", job.RetryCount); err != nil {
		return &RunResult{Status: StatusCancelled, Step: step, Message: message}, err
	}
	return r.persistCancellationEvent(job, traceID, requestID, taskID, started)
}

func (r *Runner) persistCancellationEvent(job *db.PublishJob, traceID, requestID, taskID string, started time.Time) (*RunResult, error) {
	const step = "cancelled"
	message := safePublishMessage(StatusCancelled, "")
	if err := r.persistEvent(db.JobEventInput{
		JobID: job.ID, Level: "info", Step: step, Type: "job.cancelled",
		Message: message, TraceID: traceID, RequestID: requestID, TaskID: taskID,
		DurationMS: time.Since(started).Milliseconds(),
	}); err != nil {
		return &RunResult{Status: StatusCancelled, Step: step, Message: message}, err
	}
	return &RunResult{Status: StatusCancelled, Step: step, Message: message}, nil
}

// watchCancellation exists only for one Run call. A persisted cancellation
// marker cancels the adapter context; stop always terminates the watcher.
func (r *Runner) watchCancellation(ctx context.Context, jobID int64) (context.Context, func() bool, func()) {
	store, ok := r.Store.(cancellationStore)
	if !ok {
		return ctx, func() bool { return false }, func() {}
	}
	watchedCtx, cancel := context.WithCancel(ctx)
	requested := make(chan struct{})
	stopCh := make(chan struct{})
	var requestOnce sync.Once
	markRequested := func() {
		requestOnce.Do(func() {
			close(requested)
			cancel()
		})
	}
	check := func() bool {
		select {
		case <-requested:
			return true
		default:
		}
		value, err := store.IsPublishJobCancelRequested(jobID)
		if err == nil && value {
			markRequested()
			return true
		}
		return false
	}
	_ = check()
	go func() {
		ticker := time.NewTicker(cancelPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-watchedCtx.Done():
				return
			case <-ticker.C:
				if check() {
					return
				}
			}
		}
	}()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			close(stopCh)
			cancel()
		})
	}
	return watchedCtx, check, stop
}

// watchExecutionLease keeps a running job recoverable after process crashes.
// Losing durable ownership cancels the adapter context and fails closed.
func (r *Runner) watchExecutionLease(ctx context.Context, jobID, accountID int64, verifyBrowserRuntime bool) (context.Context, func()) {
	watchedCtx, cancel := context.WithCancel(ctx)
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(publishHeartbeatPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-watchedCtx.Done():
				return
			case <-ticker.C:
				alive, err := r.Store.TouchPublishJob(jobID)
				if err != nil || !alive {
					cancel()
					return
				}
				if verifyBrowserRuntime {
					owned, ownershipErr := r.Store.VerifyBrowserRuntimeJob(accountID, jobID)
					if ownershipErr != nil || !owned {
						cancel()
						return
					}
				}
			}
		}
	}()
	var stopOnce sync.Once
	return watchedCtx, func() {
		stopOnce.Do(func() {
			close(stopCh)
			cancel()
		})
	}
}

// Run executes the pipeline.
func (r *Runner) Run(ctx context.Context, in RunInput) (*RunResult, error) {
	if r.Store == nil {
		return nil, fmt.Errorf("publish store is required")
	}
	if in.Job == nil || in.Content == nil || in.Asset == nil {
		return nil, fmt.Errorf("job, content, and asset are required")
	}
	job := in.Job
	started := time.Now()
	mode := ExecutionMode(in.DryRun, in.AutoSubmit)
	if err := r.Store.ValidatePublishJobMode(job.ID, mode); err != nil {
		return nil, fmt.Errorf("JOB_MODE_MISMATCH: %w", err)
	}
	if job.PlatformCode == "xhs" && strings.TrimSpace(job.SettingsJSON) == "" {
		snapshot, err := DefaultSettingsSnapshot("xhs")
		if err != nil {
			return nil, fmt.Errorf("JOB_SETTINGS_INVALID: %w", err)
		}
		if err := r.Store.SetPublishJobSettings(job.ID, snapshot); err != nil {
			return nil, fmt.Errorf("JOB_SETTINGS_PERSIST_FAILED: %w", err)
		}
		job.SettingsJSON = snapshot
	}
	job.Mode = mode
	traceID := in.TraceID
	if traceID == "" {
		traceID = job.TraceID
	}
	if traceID != "" {
		ctx = trace.WithID(ctx, trace.ID(traceID))
	}
	var ensuredTrace trace.ID
	ctx, ensuredTrace = trace.Ensure(ctx)
	if traceID == "" {
		traceID = string(ensuredTrace)
	}
	ctx = observability.WithCorrelation(ctx, observability.Correlation{
		RequestID: in.RequestID, TaskID: in.TaskID, JobID: job.ID, BatchID: job.BatchID,
	})
	claimed, err := r.Store.ClaimPublishJob(job.ID, traceID)
	if err != nil {
		return nil, fmt.Errorf("JOB_CLAIM_FAILED: %w", err)
	}
	if !claimed {
		message := "publish job is already running or terminal"
		return &RunResult{Status: StatusPermanentFailed, Step: "claim", Message: message}, fmt.Errorf("JOB_NOT_CLAIMABLE: %s", message)
	}
	runCtx, cancelRequested, stopCancelWatch := r.watchCancellation(ctx, job.ID)
	defer stopCancelWatch()
	if cancelRequested() {
		return r.persistCancellation(job, traceID, in.RequestID, in.TaskID, started)
	}
	if accountStore, ok := r.Store.(accountOwnershipStore); ok {
		account, accountErr := accountStore.GetAccount(in.Asset.AccountID)
		if targetErr := publishguard.ValidateTargetOwner(account, in.Asset, job.PlatformCode); accountErr != nil || targetErr != nil {
			code := publishguard.ErrTargetAccountNotFound.Error()
			if targetErr != nil {
				code = targetErr.Error()
			}
			message := "target asset does not belong to a compatible account platform"
			out := &RunResult{Status: StatusPermanentFailed, Step: "target_preflight", Message: message}
			if err := r.persistEarlyFailure(job.ID, "target_preflight", code, message, traceID, in.RequestID, in.TaskID, started, false); err != nil {
				return out, err
			}
			r.notify("publish.failed", "error", "发布失败", message, job.ID)
			return out, fmt.Errorf("%s: %s", code, message)
		}
	}

	if !platforms.CanPublish(in.Job.PlatformCode) {
		msg := "platform publishing is not supported"
		out := &RunResult{Status: StatusPermanentFailed, Step: "preflight", Message: msg}
		code := string(errors.PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1)
		if err := r.persistEarlyFailure(job.ID, "preflight", code, msg, traceID, in.RequestID, in.TaskID, started, false); err != nil {
			return out, err
		}
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.failed", "jobId": job.ID, "errorCode": code})
		r.notify("publish.failed", "error", "发布失败", msg, job.ID)
		return out, fmt.Errorf("%s: %s", code, msg)
	}
	if in.AutoSubmit && !platforms.Readiness(job.PlatformCode).CanAttemptAutoSubmit {
		const code = "AUTO_SUBMIT_NOT_SUPPORTED"
		message := "platform requires its declared manual or API flow"
		out := &RunResult{Status: StatusPermanentFailed, Step: "preflight", Message: message}
		if err := r.persistEarlyFailure(job.ID, "preflight", code, message, traceID, in.RequestID, in.TaskID, started, false); err != nil {
			return out, err
		}
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.failed", "jobId": job.ID, "errorCode": code})
		r.notify("publish.failed", "error", "发布失败", message, job.ID)
		return out, fmt.Errorf("%s: %s", code, message)
	}
	if in.AutoSubmit && (job.PlanID <= 0 || job.PlanTargetID <= 0 || len(job.SnapshotHash) != 64) {
		const code = "PUBLISH_FROZEN_SNAPSHOT_REQUIRED"
		message := "real submit requires one reviewed frozen publish-plan target"
		out := &RunResult{Status: StatusPermanentFailed, Step: "preflight", Message: message}
		if err := r.persistEarlyFailure(job.ID, "preflight", code, message, traceID, in.RequestID, in.TaskID, started, false); err != nil {
			return out, err
		}
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.failed", "jobId": job.ID, "errorCode": code})
		return out, fmt.Errorf("%s: %s", code, message)
	}

	pub := in.Publisher
	if pub == nil {
		pub, err = platforms.ExecutionPublisher(job.PlatformCode)
	}
	if err != nil || pub == nil || pub.Platform() != job.PlatformCode {
		const code = "NO_PUBLISHER"
		message := "platform publisher is unavailable"
		out := &RunResult{Status: StatusPermanentFailed, Step: "preflight", Message: message}
		if persistErr := r.persistEarlyFailure(job.ID, "preflight", code, message, traceID, in.RequestID, in.TaskID, started, false); persistErr != nil {
			return out, persistErr
		}
		r.notify("publish.failed", "error", "发布失败", message, job.ID)
		return out, fmt.Errorf("%s: %s", code, message)
	}

	verifyBrowserRuntime := !in.DryRun && pub.Strategy() != platforms.StrategyAPI
	retainBrowserRuntime := false
	if verifyBrowserRuntime {
		claimedRuntime, claimErr := r.Store.ClaimBrowserRuntimeJob(in.Asset.AccountID, job.ID)
		if claimErr != nil {
			const code = "PUBLISH_BROWSER_LEASE_FAILED"
			message := "target account browser lease could not be persisted"
			out := &RunResult{Status: StatusRetryableFailed, Step: "browser_lease", Message: message}
			if persistErr := r.persistEarlyFailure(job.ID, "browser_lease", code, message, traceID, in.RequestID, in.TaskID, started, true); persistErr != nil {
				return out, persistErr
			}
			r.notify("publish.failed", "error", "发布失败", message, job.ID)
			return out, fmt.Errorf("%s", code)
		}
		if !claimedRuntime {
			code := string(errors.BUSY_BY_JOB)
			message := "target account browser is busy with another publish job"
			out := &RunResult{Status: StatusRetryableFailed, Step: "browser_lease", Message: message}
			if persistErr := r.persistEarlyFailure(job.ID, "browser_lease", code, message, traceID, in.RequestID, in.TaskID, started, true); persistErr != nil {
				return out, persistErr
			}
			r.notify("publish.failed", "error", "发布失败", message, job.ID)
			return out, fmt.Errorf("%s: %s", code, message)
		}
		defer func() {
			if !retainBrowserRuntime {
				_, _ = r.Store.ReleaseBrowserRuntimeJob(in.Asset.AccountID, job.ID)
			}
		}()
	}
	runCtx, stopExecutionLease := r.watchExecutionLease(runCtx, job.ID, in.Asset.AccountID, verifyBrowserRuntime)
	defer stopExecutionLease()

	// queued → preflight
	st := StatusQueued
	st, err = Transition(st, StatusPreflight)
	if err != nil {
		return nil, err
	}
	if err := r.persistJob(job.ID, st, "preflight", "", "", 0); err != nil {
		return nil, err
	}
	if err := r.persistEvent(db.JobEventInput{
		JobID: job.ID, Level: "info", Step: "preflight", Type: "job.step",
		Message: "preflight started", TraceID: traceID, RequestID: in.RequestID, TaskID: in.TaskID,
	}); err != nil {
		out := &RunResult{Status: StatusPermanentFailed, Step: "preflight", Message: "preflight event persistence failed"}
		if stateErr := r.persistJob(job.ID, StatusPermanentFailed, "preflight", "EVENT_PERSIST_FAILED", out.Message, 0); stateErr != nil {
			return out, fmt.Errorf("%v; %w", err, stateErr)
		}
		return out, err
	}
	r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.step", "jobId": job.ID, "step": "preflight"})

	connectedAssetName := in.Asset.ConnectedAssetName
	connectedAssetExternalID := ""
	if in.Asset.ConnectedAssetID > 0 {
		if store, ok := r.Store.(accountAssetLookupStore); ok {
			if connected, lookupErr := store.GetAccountAsset(in.Asset.ConnectedAssetID); lookupErr == nil &&
				connected.AccountID == in.Asset.AccountID {
				connectedAssetName = connected.Name
				connectedAssetExternalID = connected.ExternalID
			}
		}
	}
	req := platforms.PublishRequest{
		JobID:                    job.ID,
		PlatformCode:             job.PlatformCode,
		ContentID:                in.Content.ID,
		ContentType:              in.Content.Type,
		Title:                    in.Content.Title,
		Caption:                  in.Content.Caption,
		Body:                     in.Content.Body,
		TagsJSON:                 in.Content.TagsJSON,
		MediaPath:                in.MediaPath,
		Media:                    in.Media,
		AccountID:                in.Asset.AccountID,
		AssetID:                  in.Asset.ID,
		AssetName:                in.Asset.Name,
		AssetType:                in.Asset.AssetType,
		AssetExternalID:          in.Asset.ExternalID,
		ConnectedAssetName:       connectedAssetName,
		ConnectedAssetExternalID: connectedAssetExternalID,
		CDPURL:                   in.CDPURL,
		DryRun:                   in.DryRun,
		AutoSubmit:               in.AutoSubmit,
		SubmitPreparedOnly:       in.SubmitPreparedOnly,
		SettingsJSON:             job.SettingsJSON,
		ComplianceJSON:           in.ComplianceJSON,
		PublishAt:                in.PublishAt,
		CredentialRef:            in.CredentialRef,
	}

	submitAttempted := false
	res, pubErr := pub.Publish(runCtx, req)
	if cancelRequested() {
		return r.persistCancellation(job, traceID, in.RequestID, in.TaskID, started)
	}
	if res == nil {
		res = &platforms.PublishResult{
			Status:       string(StatusPermanentFailed),
			Step:         "publish",
			ErrorCode:    "PUBLISH_NIL_RESULT",
			ErrorMessage: "publish failed (PUBLISH_NIL_RESULT)",
		}
	}

	next := Status(res.Status)
	if next == "" {
		if pubErr != nil {
			next = StatusRetryableFailed
		} else {
			next = StatusPermanentFailed
		}
	}

	// Allow transition from preflight to adapter outcome, or via common mid-path nodes.
	if CanTransition(StatusPreflight, next) {
		st = next
	} else if CanTransition(StatusBrowserAttached, next) {
		st = next
	} else if CanTransition(StatusMetadataFilled, next) {
		st = next
	} else if CanTransition(StatusSubmitting, next) {
		st = next
	} else {
		// Coerce unknown statuses into failure for safety.
		if pubErr != nil {
			st = StatusRetryableFailed
		} else {
			st = StatusPermanentFailed
		}
		res.ErrorCode = "INVALID_STATUS"
	}
	// The Worker only sets submitClicked after observing a trusted click on the
	// exact reviewed final control. A stopped revalidation must not burn the
	// one-click gate or keep a browser window open.
	if in.AutoSubmit && evidenceBool(res.Evidence, "submitClicked") {
		if err := r.Store.MarkPublishSubmitAttempt(job.ID); err != nil {
			const code = "PUBLISH_SUBMIT_MARK_FAILED"
			message := "final submit click result requires verification"
			out := &RunResult{Status: StatusResultUnknown, Step: "submit_marker", Message: message, Result: res}
			if persistErr := r.persistEarlyFailure(job.ID, "submit_marker", code, message, traceID, in.RequestID, in.TaskID, started, false); persistErr != nil {
				return out, persistErr
			}
			return out, fmt.Errorf("%s: %w", code, err)
		}
		submitAttempted = true
		// A real final click is irreversible; keep the browser lease until the
		// creator backend verifies its outcome.
		retainBrowserRuntime = verifyBrowserRuntime
	}
	// Platform adapters may still return the historical manual_takeover state.
	// Publishing must never persist a new manual task: before the trusted click
	// it is a stable failure (or login requirement); after the click it is an
	// irreversible unknown result and must not be retried automatically.
	if st == StatusManualTakeover {
		if submitAttempted {
			st = StatusResultUnknown
			res.ErrorCode = "PUBLISH_RESULT_UNKNOWN"
		} else if isLoginRequiredPublishError(res.ErrorCode) {
			st = StatusLoginRequired
			res.ErrorCode = "LOGIN_REQUIRED"
		} else {
			st = StatusPermanentFailed
			res.ErrorCode = "PUBLISH_AUTOMATION_UNSUPPORTED"
		}
		res.Status = string(st)
	}
	// A trusted final click is not publication evidence. Its only automatic
	// successor is creator-backend reconciliation; it must never create a new
	// manual-review task. A no-click review result is a deterministic failure
	// (or login block) and remains safe to retry only through the frozen plan.
	if in.AutoSubmit && (st == StatusPublished || st == StatusAwaitingReview) {
		if submitAttempted {
			st = StatusSubmittedPendingVerification
			res.Status = string(st)
			res.Step = "creator_backend_verification"
			res.ErrorCode = ""
			res.ErrorMessage = ""
		} else if isLoginRequiredPublishError(res.ErrorCode) {
			st = StatusLoginRequired
			res.Status = string(st)
			res.ErrorCode = "LOGIN_REQUIRED"
		} else {
			st = StatusPermanentFailed
			res.Status = string(st)
			res.ErrorCode = "PUBLISH_AUTOMATION_UNSUPPORTED"
		}
	}

	// G2 AC3: non-success outcomes must carry error_code when adapter omitted it.
	errCode := res.ErrorCode
	if errCode == "" && st != StatusPublished && st != StatusDryRunOK && st != StatusSubmittedPendingVerification {
		switch st {
		case StatusManualTakeover, StatusAwaitingReview:
			errCode = "AWAITING_HUMAN"
		case StatusLoginRequired:
			errCode = "LOGIN_REQUIRED"
		case StatusResultUnknown:
			errCode = "PUBLISH_RESULT_UNKNOWN"
		case StatusRetryableFailed:
			errCode = "RETRYABLE_FAILED"
		case StatusPermanentFailed:
			errCode = "PERMANENT_FAILED"
		default:
			errCode = "PUBLISH_FAILED"
		}
	}
	if errCode != "" {
		errCode = observability.StableErrorCode(errCode)
		if errCode == "" {
			errCode = "INTERNAL_ERROR"
		}
	}
	submitOutcome := ""
	if submitAttempted {
		switch st {
		case StatusPublished, StatusSubmittedPendingVerification, StatusAwaitingReview, StatusManualTakeover:
			submitOutcome = "verification_required"
		case StatusRetryableFailed:
			submitOutcome = "unknown"
		case StatusResultUnknown:
			submitOutcome = "unknown"
		default:
			submitOutcome = "failed"
		}
	}
	// Legacy prepare-only calls can still return published. They are not
	// publish success; leave them in the pre-submit review state. Auto-submit
	// reaches submitted_pending_verification above instead.
	if st == StatusPublished {
		st = StatusAwaitingReview
		res.Status = string(st)
		res.Step = "creator_backend_verification"
		res.ErrorCode = "PUBLISH_VERIFICATION_REQUIRED"
		errCode = res.ErrorCode
		res.ErrorMessage = "creator backend verification required"
		submitOutcome = "verification_required"
	}
	if submitOutcome != "" {
		if outcomeErr := r.Store.SetPublishSubmitOutcome(job.ID, submitOutcome); outcomeErr != nil {
			return &RunResult{Status: st, Step: res.Step, Result: res}, fmt.Errorf("PUBLISH_SUBMIT_OUTCOME_PERSIST_FAILED: %w", outcomeErr)
		}
	}
	res.ErrorCode = errCode
	message := safePublishMessage(st, errCode)
	res.Message = message
	if errCode != "" {
		res.ErrorMessage = message
	} else {
		res.ErrorMessage = ""
	}
	out := &RunResult{Status: st, Step: res.Step, Message: message, Result: res}
	var persistErr error
	usedAtomicFinalizer := false
	if store, ok := r.Store.(finalizationStore); ok {
		usedAtomicFinalizer = true
		var cancelled bool
		cancelled, persistErr = store.FinalizePublishJob(job.ID, databaseStatus(st), res.Step, errCode, res.ErrorMessage, job.RetryCount)
		if persistErr == nil && cancelled {
			return r.persistCancellationEvent(job, traceID, in.RequestID, in.TaskID, started)
		}
	} else {
		persistErr = r.persistJob(job.ID, st, res.Step, errCode, res.ErrorMessage, job.RetryCount)
	}
	if persistErr != nil {
		if usedAtomicFinalizer {
			return out, fmt.Errorf("JOB_FINALIZE_FAILED: %w", persistErr)
		}
		if st != StatusPublished {
			return out, persistErr
		}
		out.WarningCodes = append(out.WarningCodes, "JOB_PERSIST_FAILED")
	}
	level := "info"
	eventType := "job." + string(st)
	eventCode := errCode
	eventClass := observability.ErrorClass(errCode)
	eventMessage := message
	if st == StatusRetryableFailed || st == StatusPermanentFailed {
		level = "error"
	} else if st == StatusResultUnknown {
		level = "warn"
	}
	if st == StatusPublished && len(out.WarningCodes) > 0 {
		level = "warn"
		eventType = "job.published_with_warning"
		eventCode = out.WarningCodes[0]
		eventClass = "persistence"
		eventMessage = "publish completed with persistence warning"
	}
	// Screenshot path (when adapter captured one) is persisted on job_events.
	retryable := st == StatusRetryableFailed
	rawEvidence := compactPublishEvidence(res.Evidence)
	if err := r.persistEvent(db.JobEventInput{
		JobID:          job.ID,
		Level:          level,
		Step:           res.Step,
		Type:           eventType,
		Message:        eventMessage,
		ScreenshotPath: res.Screenshot,
		RawJSON:        rawEvidence,
		TraceID:        traceID,
		RequestID:      in.RequestID,
		TaskID:         in.TaskID,
		DurationMS:     time.Since(started).Milliseconds(),
		ErrorCode:      eventCode,
		ErrorClass:     eventClass,
		Retryable:      retryable,
	}); err != nil {
		if st != StatusPublished {
			return out, err
		}
		out.WarningCodes = append(out.WarningCodes, "EVENT_PERSIST_FAILED")
	}

	if st == StatusPublished {
		if len(out.WarningCodes) > 0 {
			out.Message = fmt.Sprintf("%s [persistence warning: %v]", message, out.WarningCodes)
			r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.published_with_warning", "jobId": job.ID, "status": string(st), "warningCodes": out.WarningCodes})
		} else {
			r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.success", "jobId": job.ID, "status": string(st)})
		}
	} else if st == StatusDryRunOK {
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.success", "jobId": job.ID, "status": string(st)})
	} else if st == StatusSubmittedPendingVerification {
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.verification_pending", "jobId": job.ID, "step": res.Step, "status": string(st)})
	} else if st == StatusLoginRequired {
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.login_required", "jobId": job.ID, "step": res.Step, "status": string(st)})
	} else if st == StatusManualTakeover || st == StatusAwaitingReview {
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.manual_takeover", "jobId": job.ID, "step": res.Step, "status": string(st)})
	} else if st == StatusResultUnknown {
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.result_unknown", "jobId": job.ID, "step": res.Step, "status": string(st)})
	} else {
		r.emit(in.EmitNDJSON, map[string]interface{}{"event": "job.failed", "jobId": job.ID, "errorCode": res.ErrorCode, "step": res.Step})
	}

	if st == StatusLoginRequired {
		r.notify("publish.login_required", "warn", "需要登录", message, job.ID)
	} else if st == StatusPermanentFailed || st == StatusRetryableFailed {
		if errCode == "LOGIN_REQUIRED" || errCode == "PLATFORM_VERIFICATION_REQUIRED" {
			r.notify("publish.login_required", "warn", "需要登录", message, job.ID)
		} else {
			r.notify("publish.failed", "error", "发布失败", message, job.ID)
		}
		return out, fmt.Errorf("%s: %s", res.ErrorCode, message)
	}
	if st == StatusResultUnknown {
		r.notify("publish.result_unknown", "warn", "发布结果待核验", message, job.ID)
	}
	if st == StatusPublished {
		r.notify("publish.completed", "info", "发布完成", message, job.ID)
	}
	if pubErr != nil && !IsTerminal(st) {
		return out, fmt.Errorf("%s: publish adapter failed", observability.StableErrorCode(res.ErrorCode))
	}
	return out, nil
}
