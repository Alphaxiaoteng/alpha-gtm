//go:build legacy_chromium

package publishplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/logincheck"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishworker"
	"alpha-nexus-egolite/internal/trace"
)

type BrowserStatusProvider interface {
	Status(accountID int64) (*browser.AccountStatus, error)
}

type BrowserStarter interface {
	BrowserStatusProvider
	StartAccount(accountID int64, opts browser.StartOptions) (*browser.Instance, error)
}

// BrowserStopper is deliberately separate from BrowserStarter so prepare can
// still use a status-only facade. A submitted target is stopped only after
// durable creator-backend evidence says it is published.
type BrowserStopper interface {
	StopAccount(accountID int64) error
}

// LoginProbe reads only reviewed DOM evidence from an existing platform tab.
// It exists so tests can deterministically model a session expiring between
// plan approval and worker execution.
type LoginProbe func(context.Context, string, int) (*logincheck.Observation, error)

type Executor struct {
	DB           *db.DB
	Browsers     BrowserStatusProvider
	Worker       platforms.PlaywrightWorker
	PublisherFor func(string) (platforms.Publisher, error)
	LoginProbe   LoginProbe
	// VerifyCreatorBackend is the platform-neutral verification hook. It is
	// invoked only after the Worker recorded a durable actual final click.
	// Legacy platform hooks remain for compatibility with existing observers.
	VerifyCreatorBackend CreatorBackendVerifyFunc
	VerifyInstagram      CreatorBackendVerifyFunc
	VerifyXHS            CreatorBackendVerifyFunc
	Now                  func() time.Time
}

const (
	publishLeaseRetryDelay = 250 * time.Millisecond
	publishLeaseRetries    = 8

	publishPlanStatusPersistError = "PUBLISH_PLAN_STATUS_PERSIST_FAILED"
	publishPlanTargetPersistError = "PUBLISH_PLAN_TARGET_PERSIST_FAILED"
)

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Executor) acquirePublishLease(ctx context.Context, profileID, owner string) (bool, error) {
	for attempt := 0; attempt <= publishLeaseRetries; attempt++ {
		acquired, err := e.DB.AcquireBrowserOperationLease(profileID, "publish", owner, e.now(), 10*time.Minute)
		if err != nil || acquired || attempt == publishLeaseRetries {
			return acquired, err
		}
		// The fast DOM monitor owns the same profile briefly. A user-triggered
		// prepare waits up to two seconds instead of failing on that transient
		// collision; persistent leases still fail closed below.
		timer := time.NewTimer(publishLeaseRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
	return false, nil
}

func (e *Executor) probeExplicitLogout(ctx context.Context, platformCode string, cdpPort int) bool {
	if cdpPort <= 0 {
		return false
	}
	probe := e.LoginProbe
	if probe == nil {
		probe = logincheck.ObservePassive
	}
	// One bounded passive probe closes the approval → worker TOCTOU window.
	// Composer pages do not always retain the creator-home identity DOM, so the
	// explicit user action may also inspect reviewed cookie names (never values).
	// It never navigates or mutates the browser.
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	observation, err := probe(probeCtx, platformCode, cdpPort)
	// An incomplete composer/home DOM is unknown, not logout. Only explicit
	// reviewed logout evidence can override the persisted logged-in account.
	return err == nil && observation != nil && observation.AuthState == logincheck.AuthLoggedOut
}

func (e *Executor) publisherFor(platformCode string) (platforms.Publisher, error) {
	if e.PublisherFor != nil {
		return e.PublisherFor(platformCode)
	}
	if platformCode == "facebook" || platformCode == "instagram" {
		return platforms.GetPublisher(platformCode)
	}
	return platforms.NewPlaywrightPublisher(platformCode, e.Worker)
}

func (e *Executor) markPlan(id int64, from, to string) error {
	result, err := e.DB.Exec(`UPDATE publish_plans SET status=?, updated_at=? WHERE id=? AND status=?`, to, e.now().Format(time.RFC3339Nano), id, from)
	if err != nil {
		return fmt.Errorf("%s: %w", publishPlanStatusPersistError, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", publishPlanStatusPersistError, err)
	}
	if changed != 1 {
		var current string
		if queryErr := e.DB.QueryRow(`SELECT status FROM publish_plans WHERE id=?`, id).Scan(&current); queryErr == nil && current == to {
			return nil
		}
		return fmt.Errorf("PUBLISH_PLAN_STATUS_CONFLICT")
	}
	return nil
}

func (e *Executor) applyCreatorBackendVerification(ctx context.Context, platformCode string, row TargetRun) (TargetRun, bool) {
	verify := e.VerifyCreatorBackend
	errorPrefix := strings.ToUpper(strings.TrimSpace(platformCode))
	if verify == nil {
		switch platformCode {
		case "instagram":
			verify = e.VerifyInstagram
			errorPrefix = "INSTAGRAM"
		case "xhs":
			verify = e.VerifyXHS
			errorPrefix = "XHS"
		default:
			return row, false
		}
	}
	if verify == nil || row.JobID <= 0 {
		return row, false
	}
	job, err := e.DB.GetPublishJob(row.JobID)
	if err != nil || strings.TrimSpace(job.SubmitAttemptedAt) == "" {
		return row, false
	}
	verification, verifyErr := verify(ctx, row.JobID)
	if verifyErr != nil {
		row.Status = string(publish.StatusSubmittedPendingVerification)
		row.Step = "creator_backend_verification"
		row.ErrorCode = errorPrefix + "_BACKEND_VERIFY_FAILED"
		return row, true
	}
	switch verification.Status {
	case string(publish.StatusPublished), string(publish.StatusPublishedVerified):
		persisted, persistErr := e.DB.GetPublishJob(row.JobID)
		if persistErr == nil && (persisted.Status == "published" || persisted.Status == "published_verified") &&
			persisted.CurrentStep == "creator_backend_verified" &&
			persisted.SubmitOutcome == "verified_published" {
			var records int
			if countErr := e.DB.QueryRow(`SELECT COUNT(*) FROM publish_records
				WHERE job_id=? AND status='published'`, row.JobID).Scan(&records); countErr == nil && records == 1 {
				row.Status = string(publish.StatusPublishedVerified)
				row.Step = "creator_backend_verified"
				row.ErrorCode = ""
				return row, true
			}
		}
		row.Status = string(publish.StatusSubmittedPendingVerification)
		row.Step = "creator_backend_verification"
		row.ErrorCode = errorPrefix + "_PUBLISHED_EVIDENCE_NOT_PERSISTED"
	default:
		row.Status = string(publish.StatusSubmittedPendingVerification)
		row.Step = verification.Step
		if strings.TrimSpace(row.Step) == "" {
			row.Step = "creator_backend_verification"
		}
		row.ErrorCode = verification.ErrorCode
		if strings.TrimSpace(row.ErrorCode) == "" {
			row.ErrorCode = errorPrefix + "_PUBLISHED_EVIDENCE_NOT_FOUND"
		}
	}
	return row, true
}

func (e *Executor) updateTarget(planID, assetID, jobID int64, status, code string) error {
	var persistedJobID interface{}
	if jobID > 0 {
		persistedJobID = jobID
	}
	result, err := e.DB.Exec(`UPDATE publish_plan_targets SET publish_job_id=?, status=?, error_code=?, updated_at=? WHERE plan_id=? AND asset_id=?`,
		persistedJobID, status, code, e.now().Format(time.RFC3339Nano), planID, assetID)
	if err != nil {
		return fmt.Errorf("%s: %w", publishPlanTargetPersistError, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", publishPlanTargetPersistError, err)
	}
	if changed != 1 {
		return fmt.Errorf("PUBLISH_PLAN_TARGET_STATUS_CONFLICT")
	}
	return nil
}

func (e *Executor) failUntrackedJob(jobID int64) {
	if jobID <= 0 {
		return
	}
	// A job is created pending before its plan-target link can be persisted.
	// If that link fails, never leave an orphaned pending job that another
	// executor could mistake for runnable work. A DB-wide outage can still
	// prevent this best-effort terminal mark, but the caller receives the
	// persistence error and never reports a successful prepare.
	_ = e.DB.UpdatePublishJob(jobID, "failed", "plan_target_persist", publishPlanTargetPersistError, "publish plan target status could not persist", 0)
}

func (e *Executor) closePublishedBrowser(stopper BrowserStopper, row *TargetRun, traceID string) {
	if stopper == nil || row == nil || row.JobID <= 0 ||
		(row.Status != string(publish.StatusPublished) && row.Status != string(publish.StatusPublishedVerified)) {
		return
	}
	if err := stopper.StopAccount(row.AccountID); err != nil {
		row.BrowserCloseError = "PUBLISH_BROWSER_CLOSE_FAILED"
		_, _ = e.DB.CreateJobEventDetailed(db.JobEventInput{
			JobID: row.JobID, Level: "warn", Step: "browser_close", Type: "job.browser_close_failed",
			Message: "creator backend verified publication; browser close failed", TraceID: traceID,
			ErrorCode: row.BrowserCloseError,
		})
		return
	}
	row.BrowserClosed = true
	_, _ = e.DB.CreateJobEventDetailed(db.JobEventInput{
		JobID: row.JobID, Level: "info", Step: "browser_close", Type: "job.browser_closed",
		Message: "creator backend verified publication; browser closed", TraceID: traceID,
	})
}

// closeUnsubmittedBrowser releases the background browser after a failed
// attempt that never reached the platform's final control. Once submission is
// attempted, the browser stays available for result verification rather than
// risking a duplicate post on a later retry.
func (e *Executor) closeUnsubmittedBrowser(stopper BrowserStopper, row *TargetRun, traceID string) {
	if stopper == nil || row == nil || row.JobID <= 0 || row.Status == string(publish.StatusPublished) {
		return
	}
	job, err := e.DB.GetPublishJob(row.JobID)
	if err != nil || strings.TrimSpace(job.SubmitAttemptedAt) != "" {
		return
	}
	if err := stopper.StopAccount(row.AccountID); err != nil {
		row.BrowserCloseError = "PUBLISH_BROWSER_CLOSE_FAILED"
		_, _ = e.DB.CreateJobEventDetailed(db.JobEventInput{
			JobID: row.JobID, Level: "warn", Step: "browser_close", Type: "job.browser_close_failed",
			Message: "publish did not reach final submission; browser close failed", TraceID: traceID,
			ErrorCode: row.BrowserCloseError,
		})
		return
	}
	row.BrowserClosed = true
	_, _ = e.DB.CreateJobEventDetailed(db.JobEventInput{
		JobID: row.JobID, Level: "info", Step: "browser_close", Type: "job.browser_closed",
		Message: "publish did not reach final submission; browser closed", TraceID: traceID,
	})
}

func (e *Executor) Prepare(ctx context.Context, planID int64) (*Execution, error) {
	return e.run(ctx, planID, false)
}

func (e *Executor) Submit(ctx context.Context, planID int64) (*Execution, error) {
	return e.run(ctx, planID, true)
}

// Inspect reads one existing platform tab through the same leased Facade and
// reviewed Worker used by publishing. It is intentionally fail-closed: the
// account must already be running, logged in, and bound to exactly one target.
func (e *Executor) Inspect(ctx context.Context, planID int64) (*Inspection, error) {
	return e.inspect(ctx, planID, "inspect")
}

// InspectComposer opens only a platform's reviewed direct composer URL in the
// current account tab. It is a diagnostic operation: no field is filled, no
// media is selected, and no publish job is created.
func (e *Executor) InspectComposer(ctx context.Context, planID int64) (*Inspection, error) {
	return e.inspect(ctx, planID, "inspect_composer")
}

// InspectManager opens only the reviewed platform-native content manager in
// the current account tab and returns sanitized status evidence.
func (e *Executor) InspectManager(ctx context.Context, planID int64) (*Inspection, error) {
	return e.inspect(ctx, planID, "inspect_manager")
}

func (e *Executor) DeleteContentRecord(ctx context.Context, recordID int64) (*publishworker.Output, error) {
	if e == nil || e.DB == nil || e.Browsers == nil || e.Worker == nil {
		return nil, fmt.Errorf("PUBLISH_PLAN_EXECUTOR_UNAVAILABLE")
	}
	starter, canStart := e.Browsers.(BrowserStarter)
	stopper, canStop := e.Browsers.(BrowserStopper)
	if !canStart || !canStop {
		return nil, fmt.Errorf("PUBLISH_BROWSER_LIFECYCLE_UNAVAILABLE")
	}
	record, err := e.DB.GetPublishContentRecord(recordID)
	if err != nil {
		return nil, err
	}
	if record.PlatformCode != "youtube" || (record.RemoteState != "scheduled" && record.RemoteState != "delete_pending" && record.RemoteState != "delete_failed") {
		return nil, fmt.Errorf("PUBLISH_CONTENT_DELETE_UNSUPPORTED")
	}
	account, err := e.DB.GetAccount(record.AccountID)
	if err != nil || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
		return nil, fmt.Errorf("PUBLISH_ACCOUNT_NOT_LOGGED_IN")
	}
	profileID := browser.ProfileIDForAccount(record.AccountID)
	owner := fmt.Sprintf("publish-delete:%d:%s", record.ID, strings.ReplaceAll(string(trace.New()), "_", "-"))
	acquired, err := e.acquirePublishLease(ctx, profileID, owner)
	if err != nil || !acquired {
		return nil, fmt.Errorf("BROWSER_OPERATION_BUSY")
	}
	defer e.DB.ReleaseBrowserOperationLease(profileID, owner)
	status, statusErr := e.Browsers.Status(record.AccountID)
	cdpURL, cdpErr := publishguard.LiveCDPURL(record.AccountID, status, statusErr)
	if cdpErr != nil {
		if _, startErr := starter.StartAccount(record.AccountID, browser.StartOptions{
			HolderType: browser.HolderJob, HolderID: owner, SkipFocus: true,
		}); startErr != nil {
			return nil, fmt.Errorf("PUBLISH_BROWSER_START_FAILED")
		}
		status, statusErr = e.Browsers.Status(record.AccountID)
		cdpURL, cdpErr = publishguard.LiveCDPURL(record.AccountID, status, statusErr)
	}
	if cdpErr != nil {
		return nil, fmt.Errorf("PUBLISH_BROWSER_NOT_RUNNING")
	}
	content := publishworker.Content{Type: record.ContentType, Title: record.Title}
	inspection, err := e.Worker.Run(ctx, publishworker.Input{
		SchemaVersion: 1, Action: "inspect_manager", Platform: record.PlatformCode,
		CDPURL: cdpURL, Content: content, PublishAt: record.ScheduledAt,
		Settings: map[string]interface{}{}, TimeoutMS: 30000,
	})
	if err != nil || inspection == nil || inspection.Status != "inspected" ||
		inspection.Evidence["matchingTitle"] != true || inspection.Evidence["scheduledState"] != true {
		return inspection, fmt.Errorf("PUBLISH_CONTENT_DELETE_TARGET_UNVERIFIED")
	}
	output, err := e.Worker.Run(ctx, publishworker.Input{
		SchemaVersion: 1, Action: "delete_content", Platform: record.PlatformCode,
		CDPURL: cdpURL, Content: content, Settings: map[string]interface{}{},
		AllowDelete: true, TimeoutMS: 30000,
	})
	if err != nil || output == nil || output.Status != "deleted" {
		return output, fmt.Errorf("PUBLISH_CONTENT_DELETE_FAILED")
	}
	sourcePath, _ := output.Evidence["screenshotPath"].(string)
	body, err := os.ReadFile(sourcePath)
	if err != nil || len(body) == 0 {
		return output, fmt.Errorf("PUBLISH_CONTENT_DELETE_EVIDENCE_MISSING")
	}
	evidenceDir := filepath.Join(e.DB.DataDir, "screenshots", "publish")
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		return output, err
	}
	privatePath := filepath.Join(evidenceDir, fmt.Sprintf("publish-content-%d-deleted.png", record.ID))
	if err := os.WriteFile(privatePath, body, 0o600); err != nil {
		return output, err
	}
	if err := e.DB.MarkPublishContentDeleted(record.ID, privatePath); err != nil {
		return output, err
	}
	if err := stopper.StopAccount(record.AccountID); err != nil {
		output.Evidence["browserCloseError"] = "BROWSER_CLOSE_FAILED"
	}
	return output, nil
}

// ResumeTopics repairs only native topics in an already-uploaded editor. It
// never navigates, selects media, or submits, so a stopped upload is not
// replayed merely to correct hashtags.
func (e *Executor) ResumeTopics(ctx context.Context, planID int64) (*Inspection, error) {
	return e.inspect(ctx, planID, "resume_topics")
}

// RevealSettings opens only the reviewed creator-console settings disclosure
// for the current frozen target, then returns the same sanitized inspection.
// It never fills, uploads, creates a job, or submits.
func (e *Executor) RevealSettings(ctx context.Context, planID int64) (*Inspection, error) {
	return e.inspect(ctx, planID, "inspect_settings")
}

func (e *Executor) inspect(ctx context.Context, planID int64, action string) (*Inspection, error) {
	if e == nil || e.DB == nil || e.Browsers == nil || e.Worker == nil {
		return nil, fmt.Errorf("PUBLISH_PLAN_EXECUTOR_UNAVAILABLE")
	}
	if action != "inspect" && action != "inspect_settings" && action != "inspect_composer" && action != "inspect_manager" && action != "resume_topics" {
		return nil, fmt.Errorf("PUBLISH_PLAN_INSPECTION_FAILED")
	}
	plan, err := (&Service{DB: e.DB}).Get(planID)
	if err != nil {
		return nil, err
	}
	if len(plan.Snapshot.Targets) != 1 {
		return nil, fmt.Errorf("PUBLISH_PLAN_INSPECTION_TARGET_REQUIRED")
	}
	if err := validateFrozenTargetIdentity(e.DB, plan); err != nil {
		return nil, err
	}
	target := plan.Snapshot.Targets[0]
	account, err := e.DB.GetAccount(target.AccountID)
	if err != nil || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
		return nil, fmt.Errorf("PUBLISH_ACCOUNT_NOT_LOGGED_IN")
	}
	profileID := browser.ProfileIDForAccount(target.AccountID)
	owner := fmt.Sprintf("publish-inspect:%d:%d:%s", plan.ID, target.AccountID, strings.ReplaceAll(string(trace.New()), "_", "-"))
	acquired, err := e.acquirePublishLease(ctx, profileID, owner)
	if err != nil || !acquired {
		return nil, fmt.Errorf("BROWSER_OPERATION_BUSY")
	}
	defer e.DB.ReleaseBrowserOperationLease(profileID, owner)
	status, statusErr := e.Browsers.Status(target.AccountID)
	cdpURL, cdpErr := publishguard.LiveCDPURL(target.AccountID, status, statusErr)
	if cdpErr != nil {
		return nil, fmt.Errorf("PUBLISH_BROWSER_NOT_RUNNING")
	}
	if e.probeExplicitLogout(ctx, account.PlatformCode, status.CDPPort) {
		return nil, fmt.Errorf("PUBLISH_ACCOUNT_SESSION_LOGGED_OUT")
	}
	tags := []string{}
	media := []publishworker.Media{}
	if action == "resume_topics" && json.Unmarshal([]byte(target.TagsJSON), &tags) != nil {
		return nil, fmt.Errorf("TAGS_INVALID")
	}
	if action == "resume_topics" {
		for _, item := range plan.Snapshot.Media {
			media = append(media, publishworker.Media{Path: item.FilePath, Kind: item.Kind, Role: item.Role, Position: item.Position, SHA256: item.SHA256})
		}
	}
	output, runErr := e.Worker.Run(ctx, publishworker.Input{
		SchemaVersion: 1, Action: action, Platform: target.PlatformCode, CDPURL: cdpURL,
		Content: publishworker.Content{Type: plan.MediaMode, Title: target.Title, Caption: target.Caption, Body: target.Body, Tags: tags},
		Media:   media, Settings: map[string]interface{}{}, PublishAt: target.PublishAt, TimeoutMS: 30000,
	})
	if output != nil && strings.TrimSpace(output.ErrorCode) != "" {
		return nil, fmt.Errorf("%s", strings.TrimSpace(output.ErrorCode))
	}
	if code := publishworker.ErrorCode(runErr); code != "" {
		return nil, fmt.Errorf("%s", code)
	}
	if runErr != nil || output == nil ||
		(action != "resume_topics" && action != "inspect_manager" && output.Status != "inspected") ||
		(action == "inspect_manager" && output.Status != "inspected") ||
		(action == "resume_topics" && output.Status != "resumed") {
		return nil, fmt.Errorf("PUBLISH_PLAN_INSPECTION_FAILED")
	}
	if action == "inspect_manager" {
		output.Evidence["inspectionStatus"] = output.Status
		output.Evidence["inspectionStep"] = output.Step
		if output.ErrorCode != "" {
			output.Evidence["inspectionErrorCode"] = output.ErrorCode
		}
	}
	if action == "resume_topics" {
		output.Evidence["resumeStatus"] = output.Status
		output.Evidence["resumeStep"] = output.Step
		if output.ErrorCode != "" {
			output.Evidence["resumeErrorCode"] = output.ErrorCode
		}
	}
	var jobID int64
	for _, state := range plan.TargetStates {
		if state.AccountID == target.AccountID && state.AssetID == target.AssetID {
			jobID = state.JobID
			break
		}
	}
	return &Inspection{
		PlanID: plan.ID, JobID: jobID, AccountID: target.AccountID, AssetID: target.AssetID, PlatformCode: target.PlatformCode,
		Location: output.Location, Evidence: output.Evidence,
	}, nil
}

func (e *Executor) run(ctx context.Context, planID int64, autoSubmit bool) (*Execution, error) {
	if e == nil || e.DB == nil || e.Browsers == nil || e.Worker == nil {
		return nil, fmt.Errorf("PUBLISH_PLAN_EXECUTOR_UNAVAILABLE")
	}
	starter, canStartBrowser := e.Browsers.(BrowserStarter)
	stopper, canStopBrowser := e.Browsers.(BrowserStopper)
	if autoSubmit && (!canStartBrowser || !canStopBrowser) {
		return nil, fmt.Errorf("PUBLISH_BROWSER_LIFECYCLE_UNAVAILABLE")
	}
	plan, err := (&Service{DB: e.DB}).Get(planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != StatusApproved {
		return nil, fmt.Errorf("PUBLISH_PLAN_APPROVAL_REQUIRED")
	}
	requiredAction := PublishPlanActionPrepare
	if autoSubmit {
		requiredAction = PublishPlanActionSubmit
	}
	if plan.ApprovedAction != requiredAction {
		return nil, fmt.Errorf("PUBLISH_PLAN_APPROVAL_REQUIRED")
	}
	if err := validateFrozenTargetIdentity(e.DB, plan); err != nil {
		return nil, err
	}
	verifiedTargets := make(map[int64]bool, len(plan.Snapshot.Targets))
	scheduledTargets := make(map[int64]bool, len(plan.Snapshot.Targets))
	unknownTargets := make(map[int64]bool, len(plan.Snapshot.Targets))
	for _, target := range plan.Snapshot.Targets {
		var published int
		if err := e.DB.QueryRow(`SELECT COUNT(*) FROM publish_records
			WHERE content_id=? AND account_id=? AND COALESCE(asset_id,0)=?
			  AND platform_code=? AND content_type=? AND status='published'`,
			plan.ContentID, target.AccountID, target.AssetID, target.PlatformCode, plan.MediaMode,
		).Scan(&published); err != nil {
			return nil, err
		}
		verifiedTargets[target.AssetID] = published > 0
		if published == 0 {
			var scheduled int
			if err := e.DB.QueryRow(`SELECT COUNT(*) FROM publish_jobs
				WHERE content_id=? AND account_id=? AND COALESCE(asset_id,0)=?
				  AND platform_code=? AND content_type=?
				  AND submit_outcome='scheduled_verified'`,
				plan.ContentID, target.AccountID, target.AssetID, target.PlatformCode, plan.MediaMode,
			).Scan(&scheduled); err != nil {
				return nil, err
			}
			scheduledTargets[target.AssetID] = scheduled > 0
			var unknown int
			if err := e.DB.QueryRow(`SELECT COUNT(*) FROM publish_jobs AS job
				WHERE job.content_id=? AND job.account_id=? AND COALESCE(job.asset_id,0)=?
				  AND job.platform_code=? AND job.content_type=?
				  AND COALESCE(job.submit_attempted_at,'')<>''
				  AND COALESCE(job.submit_outcome,'')<>'verified_not_published'
				  AND COALESCE(job.submit_outcome,'')<>'scheduled_verified'
				  AND NOT EXISTS (
				    SELECT 1 FROM publish_records AS record
				    WHERE record.job_id=job.id AND record.status='published'
				  )`,
				plan.ContentID, target.AccountID, target.AssetID, target.PlatformCode, plan.MediaMode,
			).Scan(&unknown); err != nil {
				return nil, err
			}
			unknownTargets[target.AssetID] = unknown > 0
		}
	}
	if err := e.markPlan(plan.ID, StatusApproved, "running"); err != nil {
		return nil, err
	}
	traceID := string(trace.New())
	execution := &Execution{PlanID: plan.ID, Status: "running", Trace: traceID, Targets: make([]TargetRun, 0, len(plan.Snapshot.Targets))}
	failed := 0
	awaitingVerification := 0
	resultUnknown := 0
	for _, target := range plan.Snapshot.Targets {
		if ctx.Err() != nil {
			failed++
			execution.Targets = append(execution.Targets, TargetRun{AccountID: target.AccountID, AssetID: target.AssetID, PlatformCode: target.PlatformCode, Status: "cancelled", ErrorCode: "PUBLISH_PLAN_CANCELLED"})
			break
		}
		// Keep preflight ahead of durable job creation. A rejected target is
		// represented by its plan target, while every created publish job is
		// immediately handed to Runner and can no longer remain pending here.
		row := TargetRun{AccountID: target.AccountID, AssetID: target.AssetID, PlatformCode: target.PlatformCode, Status: "running"}
		if verifiedTargets[target.AssetID] {
			row.Status, row.Step = string(publish.StatusPublishedVerified), "already_published"
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, ""); err != nil {
				return nil, err
			}
			continue
		}
		if scheduledTargets[target.AssetID] {
			row.Status, row.Step = "scheduled_verified", "already_scheduled_verified"
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, ""); err != nil {
				return nil, err
			}
			continue
		}
		if unknownTargets[target.AssetID] {
			row.Status, row.Step, row.ErrorCode = "result_unknown", "submission_unknown", "PUBLISH_CLICK_OUTCOME_UNKNOWN"
			awaitingVerification++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		if !autoSubmit && preparedTargetSubmitReady(e.DB, plan, target) {
			row.Status, row.Step = string(publish.StatusAwaitingReview), "already_prepared"
			if err := e.DB.QueryRow(`SELECT job.id
				FROM publish_plan_targets AS target
				JOIN publish_jobs AS job ON job.plan_id=target.plan_id AND job.plan_target_id=target.id
				WHERE target.plan_id=? AND target.asset_id=?
				  AND job.snapshot_hash=? AND job.content_id=? AND job.content_type=?
				  AND job.account_id=? AND COALESCE(job.asset_id,0)=?
				  AND job.platform_code=? AND COALESCE(job.settings_json,'')=?
				  AND job.mode=? AND job.status=? AND job.current_step IN ('ready_to_submit','review_or_submit')
				  AND COALESCE(job.submit_attempted_at,'')=''
				ORDER BY job.id DESC LIMIT 1`,
				plan.ID, target.AssetID, plan.SnapshotHash, plan.ContentID, plan.MediaMode,
				target.AccountID, target.AssetID, target.PlatformCode, target.SettingsJSON,
				publish.ModePrepare, StatusAwaitingReview,
			).Scan(&row.JobID); err != nil {
				return nil, err
			}
			execution.Targets = append(execution.Targets, row)
			continue
		}
		if autoSubmit && !preparedTargetSubmitReady(e.DB, plan, target) {
			row.Step = "not_ready_to_submit"
			if err := e.DB.QueryRow(`SELECT status, COALESCE(error_code,'') FROM publish_plan_targets
				WHERE plan_id=? AND asset_id=?`, plan.ID, target.AssetID).Scan(&row.Status, &row.ErrorCode); err != nil {
				return nil, err
			}
			if row.Status == "failed" {
				failed++
			} else if row.Status == "manual_takeover" {
				// Historical records may still contain this state. Do not
				// propagate it into a fresh execution result.
				row.Status = "failed"
				if row.ErrorCode == "PLATFORM_VERIFICATION_REQUIRED" || row.ErrorCode == "PUBLISH_ACCOUNT_NOT_LOGGED_IN" || row.ErrorCode == "LOGIN_REQUIRED" {
					row.ErrorCode = "LOGIN_REQUIRED"
				} else if row.ErrorCode == "" {
					row.ErrorCode = "PUBLISH_AUTOMATION_UNSUPPORTED"
				}
				failed++
			}
			execution.Targets = append(execution.Targets, row)
			continue
		}
		account, accountErr := e.DB.GetAccount(target.AccountID)
		if accountErr != nil || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
			row.Status, row.Step, row.ErrorCode = "failed", "login_preflight", "PUBLISH_ACCOUNT_NOT_LOGGED_IN"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		profileID := browser.ProfileIDForAccount(target.AccountID)
		owner := fmt.Sprintf("publish-plan:%d:%d:%s", plan.ID, target.AccountID, strings.ReplaceAll(traceID, "_", "-"))
		acquired, leaseErr := e.acquirePublishLease(ctx, profileID, owner)
		if leaseErr != nil || !acquired {
			row.Status, row.Step, row.ErrorCode = "failed", "browser_lease", "BROWSER_OPERATION_BUSY"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		status, statusErr := e.Browsers.Status(target.AccountID)
		cdpURL, cdpErr := publishguard.LiveCDPURL(target.AccountID, status, statusErr)
		// A prepared-only submit may reuse only a page that was already alive
		// before this execution began. Starting a fresh browser below has no
		// prepared composer and must use the normal frozen submit flow instead.
		reusedRunningBrowser := cdpErr == nil
		if cdpErr != nil && canStartBrowser {
			instance, startErr := starter.StartAccount(target.AccountID, browser.StartOptions{
				HolderType: browser.HolderJob,
				HolderID:   owner,
				// One-key publish is a background task. Keep the user's active
				// application focused even when this account browser must start.
				SkipFocus: true,
			})
			if startErr != nil && instance == nil {
				_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
				row.Status, row.Step, row.ErrorCode = "failed", "browser_start", "PUBLISH_BROWSER_START_FAILED"
				failed++
				execution.Targets = append(execution.Targets, row)
				if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
					return nil, err
				}
				continue
			}
			status, statusErr = e.Browsers.Status(target.AccountID)
			cdpURL, cdpErr = publishguard.LiveCDPURL(target.AccountID, status, statusErr)
		}
		if cdpErr != nil {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "browser_preflight", "PUBLISH_BROWSER_NOT_RUNNING"
			if errors.Is(cdpErr, publishguard.ErrBrowserDegraded) {
				row.ErrorCode = "PUBLISH_BROWSER_DEGRADED"
			}
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		if e.probeExplicitLogout(ctx, account.PlatformCode, status.CDPPort) {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "live_login_preflight", "PUBLISH_ACCOUNT_SESSION_LOGGED_OUT"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		if _, timeErr := normalizePublishTime(target.PublishAt, e.now()); timeErr != nil {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "publish_time_preflight", "PUBLISH_TIME_INVALID"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		publisher, publisherErr := e.publisherFor(target.PlatformCode)
		if publisherErr != nil {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "publisher_preflight", "PLAYWRIGHT_PUBLISHER_UNAVAILABLE"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		complianceJSON, complianceErr := frozenComplianceJSON(target.ComplianceFields)
		if complianceErr != nil {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "compliance_preflight", "PUBLISH_FROZEN_COMPLIANCE_INVALID"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		content := &db.Content{ID: plan.ContentID, Type: plan.MediaMode, Title: target.Title, Caption: target.Caption, Body: target.Body, TagsJSON: target.TagsJSON}
		asset, assetErr := e.DB.GetAccountAsset(target.AssetID)
		if assetErr != nil {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "target_preflight", "PUBLISH_TARGET_NOT_FOUND"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		action := publish.ModePrepare
		if autoSubmit {
			action = publish.ModeSubmit
		}
		var planTargetID int64
		if targetErr := e.DB.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=? AND asset_id=?`,
			plan.ID, target.AssetID).Scan(&planTargetID); targetErr != nil {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "job_create", "PUBLISH_PLAN_TARGET_NOT_FOUND"
			failed++
			execution.Targets = append(execution.Targets, row)
			continue
		}
		// Once prepare reaches an enabled final control, submit must use that
		// same live page. Re-preparing can make an already-uploaded file input
		// disappear and can also duplicate platform-side draft work.
		submitPreparedOnly := false
		if autoSubmit {
			var ready int
			readyErr := e.DB.QueryRow(`SELECT COUNT(*) FROM publish_jobs
				WHERE plan_id=? AND plan_target_id=? AND snapshot_hash=? AND mode='prepare'
				  AND status='awaiting_review' AND current_step IN ('ready_to_submit','review_or_submit')
				  AND COALESCE(submit_attempted_at,'')=''`,
				plan.ID, planTargetID, plan.SnapshotHash,
			).Scan(&ready)
			if readyErr != nil {
				_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
				row.Status, row.Step, row.ErrorCode = "failed", "job_create", "PUBLISH_JOB_CREATE_FAILED"
				failed++
				execution.Targets = append(execution.Targets, row)
				continue
			}
			submitPreparedOnly = ready == 1 && reusedRunningBrowser
		}
		idempotencyKey := fmt.Sprintf("plan:%d:account:%d:asset:%d:%s:%s",
			plan.ID, target.AccountID, target.AssetID, action, plan.SnapshotHash)
		if action == publish.ModePrepare {
			var priorAttempts int
			if countErr := e.DB.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=? AND plan_target_id=? AND mode=?`,
				plan.ID, planTargetID, publish.ModePrepare).Scan(&priorAttempts); countErr != nil {
				_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
				row.Status, row.Step, row.ErrorCode = "failed", "job_create", "PUBLISH_JOB_CREATE_FAILED"
				failed++
				execution.Targets = append(execution.Targets, row)
				if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
					return nil, err
				}
				continue
			}
			if priorAttempts > 0 {
				idempotencyKey += fmt.Sprintf(":attempt:%d", priorAttempts+1)
			}
		} else {
			// Only a prior submit with no trusted final click can receive a new
			// idempotency key. Clicked or outcome-unknown jobs remain immutable
			// and must be verified instead of replayed.
			var preflightRetries int
			if countErr := e.DB.QueryRow(`SELECT COUNT(*) FROM publish_jobs
				WHERE plan_id=? AND plan_target_id=? AND mode=?
				  AND status IN ('failed','manual_takeover')
				  AND COALESCE(submit_attempted_at,'')=''`,
				plan.ID, planTargetID, publish.ModeSubmit).Scan(&preflightRetries); countErr != nil {
				_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
				row.Status, row.Step, row.ErrorCode = "failed", "job_create", "PUBLISH_JOB_CREATE_FAILED"
				failed++
				execution.Targets = append(execution.Targets, row)
				if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
					return nil, err
				}
				continue
			}
			if preflightRetries > 0 {
				idempotencyKey += fmt.Sprintf(":unsubmitted-retry:%d", preflightRetries+1)
			}
		}
		job, created, jobErr := e.DB.CreatePublishPlanJobWithIntentResult(plan.ID, planTargetID, plan.SnapshotHash,
			0, plan.ContentID, target.AccountID, target.AssetID, target.PlatformCode, plan.MediaMode,
			idempotencyKey, action, target.SettingsJSON)
		if jobErr != nil || !created {
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			row.Status, row.Step, row.ErrorCode = "failed", "job_create", "PUBLISH_JOB_CREATE_FAILED"
			failed++
			execution.Targets = append(execution.Targets, row)
			if err := e.updateTarget(plan.ID, target.AssetID, 0, row.Status, row.ErrorCode); err != nil {
				return nil, err
			}
			continue
		}
		row.JobID = job.ID
		if err := e.updateTarget(plan.ID, target.AssetID, job.ID, "running", ""); err != nil {
			e.failUntrackedJob(job.ID)
			_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
			return nil, err
		}
		result, runErr := (&publish.Runner{Store: e.DB}).Run(ctx, publish.RunInput{
			Job: job, Content: content, Asset: asset, CDPURL: cdpURL, DryRun: false, AutoSubmit: autoSubmit,
			Media: planMedia(plan.Snapshot), Publisher: publisher, TraceID: traceID, PublishAt: target.PublishAt,
			SubmitPreparedOnly: submitPreparedOnly,
			ComplianceJSON:     complianceJSON,
		})
		if result != nil {
			row.Status, row.Step = string(result.Status), result.Step
			if result.Result != nil {
				row.ErrorCode = result.Result.ErrorCode
			}
		}
		if row.Status == "manual_takeover" {
			// Defensive compatibility for a stale Runner. A trusted click is
			// irreversible and becomes result_unknown; a login challenge is
			// the sole interaction-required state; every other handoff fails.
			clicked := false
			if result != nil && result.Result != nil {
				clicked, _ = result.Result.Evidence["submitClicked"].(bool)
			}
			if clicked {
				row.Status = string(publish.StatusResultUnknown)
				row.ErrorCode = "PUBLISH_RESULT_UNKNOWN"
			} else if row.ErrorCode == "PLATFORM_VERIFICATION_REQUIRED" || row.ErrorCode == "PUBLISH_ACCOUNT_NOT_LOGGED_IN" || row.ErrorCode == "LOGIN_REQUIRED" {
				row.Status = string(publish.StatusLoginRequired)
				row.ErrorCode = "LOGIN_REQUIRED"
			} else {
				row.Status = "failed"
				if row.ErrorCode == "" {
					row.ErrorCode = "PUBLISH_AUTOMATION_UNSUPPORTED"
				}
			}
		}
		verificationAttempted := false
		if autoSubmit && row.ErrorCode != "PLATFORM_VERIFICATION_REQUIRED" {
			row, verificationAttempted = e.applyCreatorBackendVerification(ctx, target.PlatformCode, row)
		}
		_ = e.DB.ReleaseBrowserOperationLease(profileID, owner)
		if autoSubmit && (row.Status == string(publish.StatusPublished) || row.Status == string(publish.StatusPublishedVerified)) {
			e.closePublishedBrowser(stopper, &row, traceID)
		}
		if (runErr != nil && !verificationAttempted && row.Status != string(publish.StatusPublished)) || row.Status == "" {
			row.Status = "failed"
			if row.ErrorCode == "" {
				if autoSubmit {
					row.ErrorCode = "PUBLISH_SUBMIT_FAILED"
				} else {
					row.ErrorCode = "PUBLISH_PREPARE_FAILED"
				}
			}
			failed++
		} else if autoSubmit && row.Status == string(publish.StatusSubmittedPendingVerification) {
			awaitingVerification++
		} else if autoSubmit && row.Status == "result_unknown" {
			resultUnknown++
		}
		if row.Status == "failed" && canStopBrowser {
			e.closeUnsubmittedBrowser(stopper, &row, traceID)
		}
		execution.Targets = append(execution.Targets, row)
		if err := e.updateTarget(plan.ID, target.AssetID, job.ID, row.Status, row.ErrorCode); err != nil {
			return nil, err
		}
	}
	allPublished := len(execution.Targets) == len(plan.Snapshot.Targets) && len(execution.Targets) > 0
	for _, target := range execution.Targets {
		if target.Status != string(publish.StatusPublished) && target.Status != string(publish.StatusPublishedVerified) {
			allPublished = false
			break
		}
	}
	if allPublished {
		execution.Status = "published_verified"
	} else if autoSubmit {
		execution.Status = "partial_failed"
	} else {
		execution.Status = "awaiting_review"
	}
	if resultUnknown > 0 {
		execution.Status = "result_unknown"
	} else if failed > 0 {
		execution.Status = "partial_failed"
	} else if awaitingVerification > 0 {
		execution.Status = "awaiting_verification"
	}
	if err := e.markPlan(plan.ID, "running", execution.Status); err != nil {
		return nil, err
	}
	return execution, nil
}
