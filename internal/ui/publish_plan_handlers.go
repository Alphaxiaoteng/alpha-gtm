package ui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/tools"
)

const maxPublishPlanBodyBytes = 1 << 20

const publishPlanExecutionTimeout = 10 * time.Minute

type egoPublishTargetResult struct {
	AccountID  int64                             `json:"account_id"`
	Platform   string                            `json:"platform"`
	Status     string                            `json:"status"`
	ErrorCode  string                            `json:"error_code,omitempty"`
	JobID      int64                             `json:"job_id,omitempty"`
	Diagnostic *egolite.PublishPrepareDiagnostic `json:"diagnostic,omitempty"`
}

type publishPlanExecutionTarget struct {
	AccountID    int64                             `json:"accountId"`
	PlatformCode string                            `json:"platformCode"`
	JobID        int64                             `json:"jobId"`
	Status       string                            `json:"status"`
	Step         string                            `json:"step"`
	ErrorCode    string                            `json:"errorCode,omitempty"`
	Diagnostic   *egolite.PublishPrepareDiagnostic `json:"diagnostic,omitempty"`
}

type publishPlanExecutionResponse struct {
	PlanID  int64                        `json:"planId"`
	Status  string                       `json:"status"`
	Trace   string                       `json:"trace"`
	Targets []publishPlanExecutionTarget `json:"targets"`
}

func publishPlanExecution(planID int64, status string, targets []egoPublishTargetResult) publishPlanExecutionResponse {
	executionTargets := make([]publishPlanExecutionTarget, 0, len(targets))
	for _, target := range targets {
		step := "review_required"
		switch target.Status {
		case "awaiting_review":
			step = "ready_to_submit"
		case "submitted_pending_verification", "result_unknown":
			step = "creator_backend_verification"
		case "login_required":
			step = "login_required"
		case "manual_takeover":
			step = "manual_takeover"
		case "skipped":
			step = "account_check"
		case "failed":
			step = "failed"
		case "queued":
			step = "queued"
		}
		executionTargets = append(executionTargets, publishPlanExecutionTarget{
			AccountID: target.AccountID, PlatformCode: target.Platform, JobID: target.JobID,
			Status: target.Status, Step: step, ErrorCode: target.ErrorCode,
			Diagnostic: target.Diagnostic,
		})
	}
	return publishPlanExecutionResponse{PlanID: planID, Status: status, Targets: executionTargets}
}

func publishPlanTargetKey(accountID, assetID int64, platformCode string) string {
	return fmt.Sprintf("%d:%d:%s", accountID, assetID, platformCode)
}

func publishPlanTargetStates(plan *publishplan.Plan) map[string]publishplan.TargetState {
	states := make(map[string]publishplan.TargetState)
	if plan == nil {
		return states
	}
	for _, state := range plan.TargetStates {
		states[publishPlanTargetKey(state.AccountID, state.AssetID, state.PlatformCode)] = state
	}
	return states
}

func publishPlanTargetResult(target publishplan.TargetSnapshot, state publishplan.TargetState) egoPublishTargetResult {
	return egoPublishTargetResult{
		AccountID: target.AccountID, Platform: target.PlatformCode,
		Status: state.Status, ErrorCode: state.ErrorCode, JobID: state.JobID,
	}
}

// publishPlanExecutionContext outlives an Electron renderer reload or a local
// HTTP disconnect. A one-key action remains bounded and retains the existing
// frozen plan, browser lease, and single-click guards.
func publishPlanExecutionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), publishPlanExecutionTimeout)
}

func decodePublishPlanJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxPublishPlanBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return false
	}
	return true
}

func (s *Server) callPublishPlanTool(w http.ResponseWriter, r *http.Request, name string, args map[string]interface{}) *tools.Result {
	result, _ := tools.Default.Call(r.Context(), s.toolRuntime, name, args)
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return nil
	}
	if !result.OK {
		status := http.StatusBadRequest
		if result.Error == "PUBLISH_PLAN_NOT_FOUND" {
			status = http.StatusNotFound
		}
		writeToolsError(w, status, result.Error)
		return nil
	}
	return result
}

func (s *Server) handleAPIPublishPlanCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ContentID        int64                         `json:"contentId"`
		MediaMode        string                        `json:"mediaMode"`
		MediaIDs         []int64                       `json:"mediaIds"`
		CoverMediaID     int64                         `json:"coverMediaId"`
		CompliancePolicy *publishplan.CompliancePolicy `json:"compliancePolicy"`
		Concurrency      int                           `json:"concurrency"`
		Targets          []publishplan.TargetInput     `json:"targets"`
	}
	if !decodePublishPlanJSON(w, r, &body) {
		return
	}
	// The plan detail endpoint returns its resolved browser-account asset for
	// every platform. That is evidence, not an explicit target choice: only
	// Meta assets may be selected by ID. Reusing a non-Meta snapshot in the UI
	// must therefore let the control plane resolve the current browser asset.
	for index := range body.Targets {
		target := &body.Targets[index]
		if target.AssetID <= 0 {
			continue
		}
		asset, err := s.db.GetAccountAsset(target.AssetID)
		if err == nil && asset.AccountID == target.AccountID && asset.AssetType == "browser_account" && asset.PlatformCode != "facebook" && asset.PlatformCode != "instagram" {
			target.AssetID = 0
		}
	}
	result := s.callPublishPlanTool(w, r, "publish.plan.freeze", map[string]interface{}{
		"content_id": body.ContentID, "media_mode": body.MediaMode, "media_ids": body.MediaIDs,
		"cover_media_id": body.CoverMediaID, "compliance_policy": body.CompliancePolicy, "concurrency": body.Concurrency, "targets": body.Targets,
	})
	if result != nil {
		writeJSON(w, result.Data["plan"])
	}
}

func (s *Server) handleAPIPublishPlanList(w http.ResponseWriter, _ *http.Request) {
	plans, err := (&publishplan.Service{DB: s.db}).ListSummaries(100)
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "PUBLISH_PLAN_LIST_FAILED")
		return
	}
	writeJSON(w, plans)
}

func publishPlanID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) handleAPIPublishPlanGet(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	result := s.callPublishPlanTool(w, r, "publish.plan.get", map[string]interface{}{"plan_id": id})
	if result != nil {
		writeJSON(w, result.Data["plan"])
	}
}

func (s *Server) handleAPIPublishPlanInspect(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	result, _ := tools.Default.Call(r.Context(), localRuntime, "publish.plan.inspect", map[string]interface{}{"plan_id": id})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		status := http.StatusConflict
		if result.Error == "TRUSTED_LOCAL_UI_REQUIRED" || result.Error == "VALIDATION_FAILED" {
			status = http.StatusBadRequest
		}
		writeToolsError(w, status, result.Error)
		return
	}
	writeJSON(w, result.Data["inspection"])
}

func (s *Server) handleAPIPublishPlanInspectComposer(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	result, _ := tools.Default.Call(r.Context(), localRuntime, "publish.plan.inspect_composer", map[string]interface{}{"plan_id": id})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		status := http.StatusConflict
		if result.Error == "TRUSTED_LOCAL_UI_REQUIRED" || result.Error == "VALIDATION_FAILED" {
			status = http.StatusBadRequest
		}
		writeToolsError(w, status, result.Error)
		return
	}
	writeJSON(w, result.Data["inspection"])
}

func (s *Server) handleAPIPublishPlanInspectManager(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	result, _ := tools.Default.Call(r.Context(), localRuntime, "publish.plan.inspect_manager", map[string]interface{}{"plan_id": id})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		writeToolsError(w, http.StatusConflict, result.Error)
		return
	}
	writeJSON(w, result.Data["inspection"])
}

// handleAPIPublishPlanVerifyManager is the only public creator-manager
// inspection route in the EgoLite edition. It is read-only at the platform:
// it may record independently observed evidence, but never retries submission.
func (s *Server) handleAPIPublishPlanVerifyManager(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	var accountID int64
	if raw := strings.TrimSpace(r.URL.Query().Get("account_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
			return
		}
		accountID = parsed
	}
	platformCode := strings.TrimSpace(r.URL.Query().Get("platform"))
	executionCtx, cancel := publishPlanExecutionContext()
	defer cancel()
	result, _ := s.inspectPublishPlanManagerTarget(executionCtx, id, accountID, platformCode)
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		writeToolsError(w, http.StatusConflict, result.Error)
		return
	}
	writeJSON(w, result.Data)
}

func (s *Server) handleAPIPublishPlanResumeTopics(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	result, _ := tools.Default.Call(r.Context(), localRuntime, "publish.plan.resume_topics", map[string]interface{}{"plan_id": id})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		status := http.StatusConflict
		if result.Error == "TRUSTED_LOCAL_UI_REQUIRED" || result.Error == "VALIDATION_FAILED" {
			status = http.StatusBadRequest
		}
		writeToolsError(w, status, result.Error)
		return
	}
	writeJSON(w, result.Data["inspection"])
}

func (s *Server) handleAPIPublishPlanRevealSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	result, _ := tools.Default.Call(r.Context(), localRuntime, "publish.plan.reveal_settings", map[string]interface{}{"plan_id": id})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		status := http.StatusConflict
		if result.Error == "TRUSTED_LOCAL_UI_REQUIRED" || result.Error == "VALIDATION_FAILED" {
			status = http.StatusBadRequest
		}
		writeToolsError(w, status, result.Error)
		return
	}
	writeJSON(w, result.Data["inspection"])
}

func (s *Server) handleAPIPublishPlanApprove(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	var body struct {
		SnapshotHash string `json:"snapshotHash"`
		Action       string `json:"action"`
	}
	if !decodePublishPlanJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.SnapshotHash) == "" {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	action := strings.TrimSpace(body.Action)
	if action == "" {
		action = publishplan.PublishPlanActionPrepare
	}
	if action != publishplan.PublishPlanActionPrepare && action != publishplan.PublishPlanActionSubmit && action != publishplan.PublishPlanActionAcceptanceSubmit {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	sessionID := s.healthSnapshot().Nonce
	if sessionID == "" {
		sessionID = "desktop-local"
	}
	grant, err := (&publishplan.Service{DB: s.db}).ApproveForAction(id, body.SnapshotHash, sessionID, action, 5*time.Minute)
	if err != nil {
		writeToolsError(w, http.StatusConflict, "PUBLISH_PLAN_APPROVAL_REJECTED")
		return
	}
	writeJSON(w, grant)
}

func (s *Server) preparePublishPlan(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*tools.Result, error) {
	if _, err := (&publishplan.Service{DB: s.db}).ConsumeApprovalForAction(planID, snapshotHash, approvalID, nonce, publishplan.PublishPlanActionPrepare); err != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.prepare", Error: "PUBLISH_PLAN_APPROVAL_REJECTED"}, err
	}
	plan, err := (&publishplan.Service{DB: s.db}).Get(planID)
	if err != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.prepare", Error: "PUBLISH_PLAN_PREPARE_FAILED"}, err
	}
	if s.egoSpaces == nil {
		return &tools.Result{OK: false, Tool: "publish.plan.prepare", Error: "EGO_RUNTIME_UNAVAILABLE"}, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	targets := make([]egoPublishTargetResult, len(plan.Snapshot.Targets))
	states := publishPlanTargetStates(plan)
	type matrixBatch struct {
		identityID int64
		spaceID    int64
		pages      []egolite.PublishPage
		indices    []int
	}
	// Prepare one target per scheduler job. The scheduler remains globally
	// serial, so a shared matrix Space never runs two platforms at once and an
	// invalid target cannot fail its otherwise valid neighbours.
	groups := make(map[int]*matrixBatch)
	order := make([]int, 0, len(plan.Snapshot.Targets))
	for index, target := range plan.Snapshot.Targets {
		state := states[publishPlanTargetKey(target.AccountID, target.AssetID, target.PlatformCode)]
		targets[index] = publishPlanTargetResult(target, state)
		// Existing prepared, submitted, or manually-owned targets must not be
		// reopened while this identity still has them in its bounded Space.
		if state.Status != "pending" && state.Status != "queued" && state.Status != "login_required" {
			continue
		}
		if scheduleErr := egolite.ValidateScheduledPublishAt(target.PlatformCode, plan.Snapshot.MediaMode, target.PublishAt); scheduleErr != nil {
			targets[index].Status = "failed"
			targets[index].ErrorCode = scheduleErr.Error()
			continue
		}
		account, accountErr := s.db.GetAccount(target.AccountID)
		if accountErr != nil || account.IdentityID <= 0 {
			targets[index].Status = "failed"
			targets[index].ErrorCode = "EGO_ACCOUNT_INVALID"
			continue
		}
		if account.LoginState != db.AccountAuthLoggedIn {
			targets[index].Status = "login_required"
			targets[index].ErrorCode = "LOGIN_REQUIRED"
			continue
		}
		group := &matrixBatch{identityID: account.IdentityID}
		groups[index] = group
		order = append(order, index)
		frozenMedia := make([]egolite.PublishMedia, 0, len(plan.Snapshot.Media))
		for _, item := range plan.Snapshot.Media {
			frozenMedia = append(frozenMedia, egolite.PublishMedia{
				FilePath: item.FilePath, SHA256: item.SHA256, Kind: item.Kind, Role: item.Role,
			})
		}
		group.pages = append(group.pages, egolite.PublishPage{
			PlatformCode: target.PlatformCode, ContentType: plan.Snapshot.MediaMode,
			AssetExternalID: target.AssetExternalID,
			PublishAt:       target.PublishAt,
			SettingsJSON:    target.SettingsJSON,
			Title:           target.Title, Caption: target.Caption, Body: target.Body,
			TagsJSON: target.TagsJSON, Media: frozenMedia,
		})
		group.indices = append(group.indices, index)
	}
	jobs := make([]egolite.SpaceJob, 0, len(groups))
	for _, groupID := range order {
		group := groups[groupID]
		if len(group.pages) == 0 {
			continue
		}
		identityID := group.identityID
		firstAccountID := targets[group.indices[0]].AccountID
		space, ensureErr := s.egoSpaces.EnsureAccountSpace(ctx, firstAccountID)
		if ensureErr != nil {
			for _, index := range group.indices {
				targets[index].Status = "failed"
				targets[index].ErrorCode = "EGO_SPACE_ENSURE_FAILED"
			}
			continue
		}
		if space.Ownership == "user" {
			_, _ = s.db.UpdateEgoSpaceStateForIdentity(identityID, "agentDelegatedToUser", "manual_takeover", "EGO_SPACE_USER_CONTROLLED")
			for _, index := range group.indices {
				targets[index].Status = "failed"
				targets[index].ErrorCode = "EGO_SPACE_USER_CONTROLLED"
			}
			continue
		}
		group.spaceID = space.SpaceID
		batch := group
		jobs = append(jobs, egolite.SpaceJob{
			ID: identityID, SpaceID: space.SpaceID,
			Run: func(jobContext context.Context) error {
				opened, handoffErr := s.egoSpaces.OpenPublishBatchAndHandoff(jobContext, batch.identityID, batch.pages)
				prepared := make(map[string]bool)
				platformErrors := make(map[string]string)
				platformDiagnostics := make(map[string]egolite.PublishPrepareDiagnostic)
				if opened != nil {
					for _, code := range opened.PreparedPlatformCodes {
						prepared[code] = true
					}
					platformErrors = opened.PlatformErrors
					platformDiagnostics = opened.PlatformDiagnostics
				}
				for _, index := range batch.indices {
					if diagnostic, ok := platformDiagnostics[targets[index].Platform]; ok {
						diagnosticCopy := diagnostic
						targets[index].Diagnostic = &diagnosticCopy
					}
					if handoffErr != nil {
						if strings.TrimSpace(handoffErr.Error()) == "EGO_SPACE_USER_CONTROLLED" {
							targets[index].Status = "failed"
							targets[index].ErrorCode = "EGO_SPACE_USER_CONTROLLED"
						} else {
							targets[index].Status = "failed"
							targets[index].ErrorCode = "EGO_MANAGER_HANDOFF_FAILED"
						}
					} else if prepared[targets[index].Platform] {
						frozenTarget := plan.Snapshot.Targets[index]
						var planTargetID int64
						lookupErr := s.db.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=? AND account_id=? AND asset_id=? AND platform_code=?`,
							planID, frozenTarget.AccountID, frozenTarget.AssetID, frozenTarget.PlatformCode).Scan(&planTargetID)
						prepareJob, created, createErr := s.db.CreatePublishPlanJobWithIntentResult(
							planID, planTargetID, plan.SnapshotHash, 0, plan.ContentID,
							frozenTarget.AccountID, frozenTarget.AssetID, frozenTarget.PlatformCode, plan.MediaMode,
							"", publishplan.PublishPlanActionPrepare, frozenTarget.SettingsJSON,
						)
						if lookupErr != nil || createErr != nil || !created || prepareJob == nil ||
							s.db.UpdatePublishJob(prepareJob.ID, "awaiting_review", "ready_to_submit", "", "", 0) != nil {
							targets[index].Status = "failed"
							targets[index].ErrorCode = "PUBLISH_JOB_CREATE_FAILED"
						} else {
							targets[index].Status = "awaiting_review"
							targets[index].ErrorCode = ""
							targets[index].JobID = prepareJob.ID
						}
					} else {
						targets[index].ErrorCode = platformErrors[targets[index].Platform]
						if targets[index].ErrorCode == "" {
							targets[index].ErrorCode = "EGO_PUBLISH_AUTOMATION_UNSUPPORTED"
						}
						if targets[index].ErrorCode == "LOGIN_REQUIRED" {
							targets[index].Status = "login_required"
						} else {
							targets[index].Status = "failed"
						}
					}
				}
				return handoffErr
			},
		})
	}
	concurrency := 1
	results := egolite.NewSpaceScheduler(concurrency).Run(ctx, jobs)
	failed := false
	loginRequired := false
	queued := false
	activeReview := false
	for _, target := range targets {
		if target.Status == "failed" {
			failed = true
		}
		if target.Status == "login_required" {
			loginRequired = true
		}
		if target.Status == "queued" {
			queued = true
		}
		if target.Status == "awaiting_review" {
			activeReview = true
		}
	}
	// 这些写库失败必须被发现：target 状态只改了内存，库里的仍是旧值，
	// 页面刷新就会回退，而调用方拿到的是「成功」。
	var persistErrs []string
	for index, target := range targets {
		frozenTarget := plan.Snapshot.Targets[index]
		if _, execErr := s.db.Exec(`UPDATE publish_plan_targets SET status=?,error_code=?,updated_at=? WHERE plan_id=? AND account_id=? AND asset_id=? AND platform_code=?`,
			target.Status, target.ErrorCode, time.Now().UTC().Format(time.RFC3339), planID,
			frozenTarget.AccountID, frozenTarget.AssetID, frozenTarget.PlatformCode); execErr != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("target %s: %v", frozenTarget.PlatformCode, execErr))
		}
		s.notifyPublishPlanTarget(planID, target)
	}
	planStatus := "awaiting_review"
	if (failed || loginRequired) && activeReview {
		planStatus = "partial_failed"
	} else if failed {
		planStatus = "failed"
	} else if loginRequired {
		planStatus = "login_required"
	} else if queued {
		planStatus = "queued"
	}
	if _, execErr := s.db.Exec(`UPDATE publish_plans SET status=? WHERE id=?`, planStatus, planID); execErr != nil {
		persistErrs = append(persistErrs, fmt.Sprintf("plan: %v", execErr))
	}
	if failed || loginRequired {
		return &tools.Result{OK: false, Tool: "publish.plan.prepare", Error: "EGO_PREPARE_PARTIAL_FAILED", Data: map[string]interface{}{"targets": targets, "scheduler": results, "execution": publishPlanExecution(planID, planStatus, targets)}}, fmt.Errorf("EGO_PREPARE_PARTIAL_FAILED")
	}
	if len(persistErrs) > 0 {
		return &tools.Result{OK: false, Tool: "publish.plan.prepare", Error: "EGO_PREPARE_PERSIST_FAILED",
				Data: map[string]interface{}{"targets": targets, "scheduler": results,
					"execution": publishPlanExecution(planID, planStatus, targets), "persist_errors": persistErrs}},
			fmt.Errorf("EGO_PREPARE_PERSIST_FAILED")
	}
	message := "creator forms prepared automatically and are ready for the one-time submit approval"
	if queued {
		message = "current creator composers opened; remaining accounts are queued until this group is resolved"
	}
	return &tools.Result{OK: true, Tool: "publish.plan.prepare", Message: message, Data: map[string]interface{}{"targets": targets, "scheduler": results, "execution": publishPlanExecution(planID, planStatus, targets)}}, nil
}

// PrepareApprovedPublishPlan exposes the same frozen-plan executor to the CLI.
func (s *Server) PrepareApprovedPublishPlan(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*tools.Result, error) {
	return s.preparePublishPlan(ctx, planID, snapshotHash, approvalID, nonce)
}

func egoSubmitPreflightErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	if len(code) <= 80 && strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
		return code
	}
	return "EGO_SUBMIT_PREFLIGHT_FAILED"
}

func (s *Server) notifyPublishPlanTarget(planID int64, target egoPublishTargetResult) {
	kind, level, title := "", "", ""
	switch {
	case target.ErrorCode == "LOGIN_REQUIRED":
		kind, level, title = "publish.login_required", "warn", "需要登录"
	case target.Status == "result_unknown":
		kind, level, title = "publish.result_unknown", "warn", "发布结果待核验"
	case target.Status == "failed":
		kind, level, title = "publish.failed", "error", "平台异常"
	case target.Status == "published_verified" || target.Status == "scheduled_verified" || target.Status == "published":
		kind, level, title = "publish.completed", "info", "发布已核验"
	default:
		return
	}
	relatedType, relatedID := "publish_plan", planID
	if target.JobID > 0 {
		relatedType, relatedID = "publish_job", target.JobID
	}
	body := target.Platform
	if target.ErrorCode != "" {
		body += " · " + target.ErrorCode
	}
	var exists int
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM notifications WHERE type=? AND related_type=? AND related_id=? AND title=? AND body=?)`,
		kind, relatedType, relatedID, title, body).Scan(&exists); err == nil && exists == 1 {
		return
	}
	_, _ = s.db.CreateNotification(kind, level, title, body, relatedType, relatedID)
}

func (s *Server) submitPublishPlan(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*tools.Result, error) {
	return s.submitPublishPlanForAction(ctx, planID, snapshotHash, approvalID, nonce, publishplan.PublishPlanActionSubmit)
}

func (s *Server) submitAcceptancePublishPlan(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*tools.Result, error) {
	return s.submitPublishPlanForAction(ctx, planID, snapshotHash, approvalID, nonce, publishplan.PublishPlanActionAcceptanceSubmit)
}

func (s *Server) submitPublishPlanForAction(ctx context.Context, planID int64, snapshotHash, approvalID, nonce, action string) (*tools.Result, error) {
	acceptance := action == publishplan.PublishPlanActionAcceptanceSubmit
	toolName := "publish.plan.submit"
	if acceptance {
		toolName = "publish.plan.acceptance_submit"
	}
	if _, err := (&publishplan.Service{DB: s.db}).ConsumeApprovalForAction(planID, snapshotHash, approvalID, nonce, action); err != nil {
		return &tools.Result{OK: false, Tool: toolName, Error: "PUBLISH_PLAN_APPROVAL_REJECTED"}, err
	}
	plan, err := (&publishplan.Service{DB: s.db}).Get(planID)
	if err != nil {
		return &tools.Result{OK: false, Tool: toolName, Error: "PUBLISH_PLAN_SUBMIT_FAILED"}, err
	}
	if s.egoSpaces == nil {
		return &tools.Result{OK: false, Tool: toolName, Error: "EGO_RUNTIME_UNAVAILABLE"}, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	targets := make([]egoPublishTargetResult, len(plan.Snapshot.Targets))
	states := publishPlanTargetStates(plan)
	type matrixBatch struct {
		identityID int64
		pages      []egolite.PublishPage
		indices    []int
	}
	// Final submission deliberately creates one scheduler job per frozen target.
	// The scheduler is globally serial and each job reuses the account's bound
	// Space. This keeps the result/evidence identity unambiguous even when one
	// matrix identity owns multiple targets for the same platform.
	groups := make(map[int]*matrixBatch)
	order := make([]int, 0)
	now := time.Now().UTC().Format(time.RFC3339)
	for index, target := range plan.Snapshot.Targets {
		state := states[publishPlanTargetKey(target.AccountID, target.AssetID, target.PlatformCode)]
		targets[index] = publishPlanTargetResult(target, state)
		if state.Status != "awaiting_review" && state.Status != "manual_takeover" {
			continue
		}
		if scheduleErr := egolite.ValidateScheduledPublishAt(target.PlatformCode, plan.Snapshot.MediaMode, target.PublishAt); scheduleErr != nil {
			targets[index].Status = "failed"
			targets[index].ErrorCode = scheduleErr.Error()
			continue
		}
		adapterReady := egopublish.PrepareAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode) &&
			egopublish.SubmitAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode) &&
			egopublish.VerifyAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode)
		if (!acceptance && !egopublish.SubmitVerifiedFor(target.PlatformCode, plan.Snapshot.MediaMode)) || (acceptance && !adapterReady) {
			targets[index].Status = "failed"
			if acceptance {
				targets[index].ErrorCode = "EGO_PUBLISH_ACCEPTANCE_UNAVAILABLE"
			} else {
				targets[index].ErrorCode = "EGO_PUBLISH_SUBMIT_UNVERIFIED"
			}
			continue
		}
		var currentStatus string
		var safelyUnclicked int
		stateErr := s.db.QueryRow(`SELECT target.status, EXISTS (
			SELECT 1 FROM publish_jobs AS job
			WHERE job.id=target.publish_job_id AND job.plan_id=target.plan_id AND job.plan_target_id=target.id
			  AND job.mode='submit' AND job.status='manual_takeover'
			  AND COALESCE(job.submit_attempted_at,'')='' AND job.submit_outcome='not_clicked'
		) FROM publish_plan_targets AS target WHERE target.plan_id=? AND target.account_id=?`,
			planID, target.AccountID).Scan(&currentStatus, &safelyUnclicked)
		if stateErr != nil || (currentStatus != "awaiting_review" && !(currentStatus == "manual_takeover" && safelyUnclicked == 1)) {
			targets[index].Status = "failed"
			targets[index].ErrorCode = "EGO_PUBLISH_PLAN_NOT_PREPARED"
			continue
		}
		account, accountErr := s.db.GetAccount(target.AccountID)
		if accountErr != nil || account.IdentityID <= 0 || account.LoginState != db.AccountAuthLoggedIn {
			targets[index].Status = "login_required"
			targets[index].ErrorCode = "LOGIN_REQUIRED"
			continue
		}
		group := &matrixBatch{identityID: account.IdentityID}
		groups[index] = group
		order = append(order, index)
		frozenMedia := make([]egolite.PublishMedia, 0, len(plan.Snapshot.Media))
		for _, item := range plan.Snapshot.Media {
			frozenMedia = append(frozenMedia, egolite.PublishMedia{FilePath: item.FilePath, SHA256: item.SHA256, Kind: item.Kind, Role: item.Role})
		}
		group.pages = append(group.pages, egolite.PublishPage{
			PlatformCode: target.PlatformCode, ContentType: plan.Snapshot.MediaMode,
			AssetExternalID: target.AssetExternalID, AssetName: target.AssetName, Title: target.Title,
			PublishAt: target.PublishAt, Caption: target.Caption, Body: target.Body, TagsJSON: target.TagsJSON, Media: frozenMedia,
		})
		group.indices = append(group.indices, index)
	}
	jobs := make([]egolite.SpaceJob, 0, len(order))
	for _, groupID := range order {
		batch := groups[groupID]
		identityID := batch.identityID
		preflightIndices := make([]int, 0, len(batch.indices))
		preflightPages := make([]egolite.PublishPage, 0, len(batch.pages))
		for pageIndex, index := range batch.indices {
			var preflightErr error
			if acceptance {
				preflightErr = s.egoSpaces.ValidateAcceptanceSubmitPublishBatch(identityID, []egolite.PublishPage{batch.pages[pageIndex]})
			} else {
				preflightErr = s.egoSpaces.ValidateSubmitPublishBatch(identityID, []egolite.PublishPage{batch.pages[pageIndex]})
			}
			if preflightErr != nil {
				targets[index].Status = "failed"
				targets[index].ErrorCode = egoSubmitPreflightErrorCode(preflightErr)
				continue
			}
			preflightIndices = append(preflightIndices, index)
			preflightPages = append(preflightPages, batch.pages[pageIndex])
		}
		if len(preflightPages) == 0 {
			continue
		}
		space, ensureErr := s.egoSpaces.EnsureAccountSpace(ctx, targets[preflightIndices[0]].AccountID)
		if ensureErr != nil {
			for _, index := range preflightIndices {
				targets[index].Status = "failed"
				targets[index].ErrorCode = "EGO_SPACE_ENSURE_FAILED"
			}
			continue
		}
		readyIndices := make([]int, 0, len(preflightIndices))
		readyPages := make([]egolite.PublishPage, 0, len(preflightPages))
		readyJobIDs := make([]int64, 0, len(preflightPages))
		for pageIndex, index := range preflightIndices {
			frozenTarget := plan.Snapshot.Targets[index]
			var planTargetID int64
			lookupErr := s.db.QueryRow(`SELECT id FROM publish_plan_targets AS target
				WHERE plan_id=? AND account_id=? AND asset_id=? AND platform_code=?
				  AND (status='awaiting_review' OR (status='manual_takeover' AND EXISTS (
					SELECT 1 FROM publish_jobs AS job
					WHERE job.id=target.publish_job_id AND job.plan_id=target.plan_id AND job.plan_target_id=target.id
					  AND job.mode='submit' AND job.status='manual_takeover'
					  AND COALESCE(job.submit_attempted_at,'')='' AND job.submit_outcome='not_clicked'
				  )))`,
				planID, frozenTarget.AccountID, frozenTarget.AssetID, frozenTarget.PlatformCode).Scan(&planTargetID)
			if lookupErr != nil {
				targets[index].Status = "failed"
				targets[index].ErrorCode = "EGO_SUBMIT_STATE_CONFLICT"
				continue
			}
			submitJob, created, createErr := s.db.CreatePublishPlanJobWithIntentResult(
				planID, planTargetID, plan.SnapshotHash, 0, plan.ContentID,
				frozenTarget.AccountID, frozenTarget.AssetID, frozenTarget.PlatformCode, plan.MediaMode,
				"", publishplan.PublishPlanActionSubmit, frozenTarget.SettingsJSON,
			)
			if createErr != nil || !created || submitJob == nil {
				targets[index].Status = "failed"
				targets[index].ErrorCode = "EGO_SUBMIT_STATE_CONFLICT"
				continue
			}
			if err := s.db.UpdatePublishJob(submitJob.ID, "running", "submit_approved", "", "", 0); err != nil {
				_ = s.db.UpdatePublishJob(submitJob.ID, "failed", "submit_job_start", "EGO_SUBMIT_STATE_CONFLICT", "", 0)
				targets[index].Status = "failed"
				targets[index].ErrorCode = "EGO_SUBMIT_STATE_CONFLICT"
				continue
			}
			if err := s.db.MarkPublishSubmitAttempt(submitJob.ID); err != nil {
				_ = s.db.UpdatePublishJob(submitJob.ID, "failed", "submit_attempt_gate", "EGO_SUBMIT_STATE_CONFLICT", "", 0)
				targets[index].Status = "failed"
				targets[index].ErrorCode = "EGO_SUBMIT_STATE_CONFLICT"
				continue
			}
			targets[index].JobID = submitJob.ID
			changed, updateErr := s.db.Exec(`UPDATE publish_plan_targets SET status='submitting',error_code='',updated_at=? WHERE id=? AND plan_id=? AND publish_job_id=? AND status='pending'`,
				now, planTargetID, planID, submitJob.ID)
			if updateErr != nil {
				_ = s.db.SetPublishSubmitOutcome(submitJob.ID, "unknown")
				_ = s.db.UpdatePublishJob(submitJob.ID, "result_unknown", "creator_backend_verification", "EGO_SUBMIT_STATE_CONFLICT", "", 0)
				targets[index].Status = "result_unknown"
				targets[index].ErrorCode = "EGO_SUBMIT_STATE_CONFLICT"
				continue
			}
			rows, rowsErr := changed.RowsAffected()
			if rowsErr != nil || rows != 1 {
				_ = s.db.SetPublishSubmitOutcome(submitJob.ID, "unknown")
				_ = s.db.UpdatePublishJob(submitJob.ID, "result_unknown", "creator_backend_verification", "EGO_SUBMIT_STATE_CONFLICT", "", 0)
				targets[index].Status = "result_unknown"
				targets[index].ErrorCode = "EGO_SUBMIT_STATE_CONFLICT"
				continue
			}
			readyIndices = append(readyIndices, index)
			readyPages = append(readyPages, preflightPages[pageIndex])
			readyJobIDs = append(readyJobIDs, submitJob.ID)
		}
		if len(readyPages) == 0 {
			continue
		}
		jobIndices := readyIndices
		jobPages := readyPages
		jobIDs := readyJobIDs
		jobs = append(jobs, egolite.SpaceJob{ID: identityID, SpaceID: space.SpaceID, Run: func(jobContext context.Context) error {
			var execution *egolite.LoginBatch
			var submitErr error
			if acceptance {
				execution, submitErr = s.egoSpaces.SubmitAcceptancePublishBatchAndHandoff(jobContext, identityID, jobPages)
			} else {
				execution, submitErr = s.egoSpaces.SubmitPublishBatchAndHandoff(jobContext, identityID, jobPages)
			}
			for jobIndex, index := range jobIndices {
				jobID := jobIDs[jobIndex]
				if submitErr != nil || execution == nil {
					targets[index].Status = "result_unknown"
					targets[index].ErrorCode = "EGO_SUBMIT_RESULT_UNKNOWN"
					_ = s.db.SetPublishSubmitOutcome(jobID, "unknown")
					_ = s.db.UpdatePublishJob(jobID, "result_unknown", "creator_backend_verification", "EGO_SUBMIT_RESULT_UNKNOWN", "", 0)
					continue
				}
				status := execution.PlatformStatuses[targets[index].Platform]
				clicked, clickReported := execution.PlatformClicked[targets[index].Platform]
				switch status {
				case "submitted_pending_verification", "result_unknown", "failed":
					targets[index].Status = status
				default:
					targets[index].Status = "result_unknown"
				}
				targets[index].ErrorCode = execution.PlatformErrors[targets[index].Platform]
				if targets[index].Status == "result_unknown" && targets[index].ErrorCode == "" {
					targets[index].ErrorCode = "EGO_SUBMIT_RESULT_UNKNOWN"
				}
				if clickReported && !clicked {
					targets[index].Status = "failed"
					if targets[index].ErrorCode == "" {
						targets[index].ErrorCode = "EGO_SUBMIT_NOT_CLICKED"
					}
					if releaseErr := s.db.ReleasePublishSubmitReservation(jobID); releaseErr != nil {
						targets[index].Status = "result_unknown"
						targets[index].ErrorCode = "EGO_SUBMIT_RESULT_UNKNOWN"
						_ = s.db.SetPublishSubmitOutcome(jobID, "unknown")
						_ = s.db.UpdatePublishJob(jobID, "result_unknown", "creator_backend_verification", targets[index].ErrorCode, "", 0)
					} else {
						step := "submit_not_clicked"
						if targets[index].ErrorCode == "LOGIN_REQUIRED" {
							step = "login_required"
						}
						_ = s.db.UpdatePublishJob(jobID, "failed", step, targets[index].ErrorCode, "", 0)
					}
				} else if !clickReported && targets[index].Status == "submitted_pending_verification" {
					targets[index].Status = "result_unknown"
					targets[index].ErrorCode = "EGO_SUBMIT_RESULT_UNKNOWN"
					_ = s.db.SetPublishSubmitOutcome(jobID, "unknown")
					_ = s.db.UpdatePublishJob(jobID, "result_unknown", "creator_backend_verification", targets[index].ErrorCode, "", 0)
				} else if !clickReported {
					targets[index].Status = "result_unknown"
					targets[index].ErrorCode = "EGO_SUBMIT_RESULT_UNKNOWN"
					_ = s.db.SetPublishSubmitOutcome(jobID, "unknown")
					_ = s.db.UpdatePublishJob(jobID, "result_unknown", "creator_backend_verification", targets[index].ErrorCode, "", 0)
				} else if targets[index].Status == "submitted_pending_verification" {
					_ = s.db.SetPublishSubmitOutcome(jobID, "verification_required")
					_ = s.db.UpdatePublishJob(jobID, "submitted_pending_verification", "creator_backend_verification", "", "", 0)
				} else if clicked {
					targets[index].Status = "result_unknown"
					if targets[index].ErrorCode == "" {
						targets[index].ErrorCode = "EGO_SUBMIT_RESULT_UNKNOWN"
					}
					_ = s.db.SetPublishSubmitOutcome(jobID, "unknown")
					_ = s.db.UpdatePublishJob(jobID, "result_unknown", "creator_backend_verification", targets[index].ErrorCode, "", 0)
				} else {
					_ = s.db.UpdatePublishJob(jobID, "failed", "submit_not_clicked", targets[index].ErrorCode, "", 0)
				}
				frozenTarget := plan.Snapshot.Targets[index]
				// 闭包内无法把错误直接抛给调用方，记日志保证失败可追溯；
				// 最终一致性由外层汇总循环再写一遍状态兜底。
				if _, execErr := s.db.Exec(`UPDATE publish_plan_targets SET status=?,error_code=?,updated_at=? WHERE plan_id=? AND account_id=? AND asset_id=? AND platform_code=?`,
					targets[index].Status, targets[index].ErrorCode, time.Now().UTC().Format(time.RFC3339), planID,
					frozenTarget.AccountID, frozenTarget.AssetID, frozenTarget.PlatformCode); execErr != nil {
					slog.Error("publish plan target persist failed", "plan_id", planID,
						"platform", frozenTarget.PlatformCode, "error", execErr)
				}
				if (acceptance || egopublish.VerifyVerifiedFor(targets[index].Platform, plan.MediaMode)) &&
					(targets[index].Status == "submitted_pending_verification" || targets[index].Status == "result_unknown") {
					verification, verifyErr := s.verifyCreatorBackendForAction(jobContext, jobID, acceptance)
					if verifyErr == nil && verification.Status == "published" {
						targets[index].Status, targets[index].ErrorCode = "published_verified", ""
					} else if verifyErr == nil && verification.Status == "scheduled" {
						targets[index].Status, targets[index].ErrorCode = "scheduled_verified", ""
					}
				}
			}
			return submitErr
		}})
	}
	concurrency := 1
	schedulerResults := egolite.NewSpaceScheduler(concurrency).Run(ctx, jobs)
	resultUnknown := false
	failed := false
	loginRequired := false
	pendingVerification := false
	verifiedCount := 0
	var persistErrs []string
	for index := range targets {
		target := &targets[index]
		if target.Status == "" {
			target.Status = "failed"
			target.ErrorCode = "EGO_PUBLISH_SUBMIT_UNVERIFIED"
		}
		if target.Status == "result_unknown" {
			resultUnknown = true
		} else if target.Status == "login_required" {
			loginRequired = true
		} else if target.Status == "submitted_pending_verification" {
			pendingVerification = true
		} else if target.Status == "published_verified" || target.Status == "scheduled_verified" || target.Status == "published" {
			verifiedCount++
		} else if target.Status == "failed" {
			failed = true
		}
		frozenTarget := plan.Snapshot.Targets[index]
		if _, execErr := s.db.Exec(`UPDATE publish_plan_targets SET status=?,error_code=?,updated_at=? WHERE plan_id=? AND account_id=? AND asset_id=? AND platform_code=?`,
			target.Status, target.ErrorCode, time.Now().UTC().Format(time.RFC3339), planID,
			frozenTarget.AccountID, frozenTarget.AssetID, frozenTarget.PlatformCode); execErr != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("target %s: %v", frozenTarget.PlatformCode, execErr))
		}
		s.notifyPublishPlanTarget(planID, *target)
	}
	planStatus := "submitted_pending_verification"
	if verifiedCount == len(targets) && verifiedCount > 0 {
		planStatus = "completed"
	} else if resultUnknown {
		planStatus = "result_unknown"
	} else if (failed || loginRequired) && (pendingVerification || verifiedCount > 0) {
		planStatus = "partial_failed"
	} else if loginRequired && !failed {
		planStatus = "login_required"
	} else if failed || !pendingVerification {
		planStatus = "failed"
	}
	if _, execErr := s.db.Exec(`UPDATE publish_plans SET status=? WHERE id=?`, planStatus, planID); execErr != nil {
		persistErrs = append(persistErrs, fmt.Sprintf("plan: %v", execErr))
	}
	data := map[string]interface{}{"targets": targets, "scheduler": schedulerResults, "execution": publishPlanExecution(planID, planStatus, targets)}
	// 发布结果落库失败比任何单平台结果都严重：内存里显示的状态刷新后就没了，
	// 而用户会以为发布已记录。这里优先报出来。
	if len(persistErrs) > 0 {
		data["persist_errors"] = persistErrs
		return &tools.Result{OK: false, Tool: toolName, Error: "EGO_SUBMIT_PERSIST_FAILED", Data: data}, fmt.Errorf("EGO_SUBMIT_PERSIST_FAILED")
	}
	if resultUnknown {
		return &tools.Result{OK: false, Tool: toolName, Error: "EGO_SUBMIT_RESULT_UNKNOWN", Data: data}, fmt.Errorf("EGO_SUBMIT_RESULT_UNKNOWN")
	}
	if failed || loginRequired {
		return &tools.Result{OK: false, Tool: toolName, Error: "EGO_SUBMIT_PARTIAL_FAILED", Data: data}, fmt.Errorf("EGO_SUBMIT_PARTIAL_FAILED")
	}
	return &tools.Result{OK: true, Tool: toolName, Message: "platform acknowledged the click; independent creator-backend verification is still required", Data: data}, nil
}

// SubmitApprovedPublishPlan exposes the submit-bound, single-use executor to the CLI.
func (s *Server) SubmitApprovedPublishPlan(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*tools.Result, error) {
	return s.submitPublishPlan(ctx, planID, snapshotHash, approvalID, nonce)
}

// SubmitAcceptancePublishPlan exposes the separately bound, single-target
// live-acceptance executor to the local CLI. It is intentionally not part of
// the Agent-visible Registry surface.
func (s *Server) SubmitAcceptancePublishPlan(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*tools.Result, error) {
	return s.submitAcceptancePublishPlan(ctx, planID, snapshotHash, approvalID, nonce)
}

func (s *Server) inspectPublishPlan(ctx context.Context, planID int64) (*tools.Result, error) {
	inspection, err := s.inspectEgoPublishPlan(ctx, planID, false)
	if err != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.inspect", Error: publishPlanInspectionErrorCode(err)}, err
	}
	return &tools.Result{OK: true, Tool: "publish.plan.inspect", Message: "Ego Space observed without changing the page", Data: map[string]interface{}{"inspection": inspection}}, nil
}

func (s *Server) inspectPublishPlanComposer(ctx context.Context, planID int64) (*tools.Result, error) {
	inspection, err := s.inspectEgoPublishPlan(ctx, planID, true)
	if err != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.inspect_composer", Error: publishPlanInspectionErrorCode(err)}, err
	}
	return &tools.Result{OK: true, Tool: "publish.plan.inspect_composer", Message: "creator composer inspected in EgoLite without submitting", Data: map[string]interface{}{"inspection": inspection}}, nil
}

func (s *Server) inspectPublishPlanManager(ctx context.Context, planID int64) (*tools.Result, error) {
	return s.inspectPublishPlanManagerTarget(ctx, planID, 0, "")
}

func (s *Server) inspectPublishPlanManagerTarget(ctx context.Context, planID, accountID int64, platformCode string) (*tools.Result, error) {
	inspection, err := s.inspectEgoPublishManagerTarget(ctx, planID, accountID, platformCode)
	if err != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.inspect_manager", Error: publishPlanInspectionErrorCode(err)}, err
	}
	records, recordErr := publishplan.PersistManagerInspection(s.db, planID, inspection)
	if recordErr != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.inspect_manager", Error: "PUBLISH_CONTENT_RECORD_FAILED"}, recordErr
	}
	safeInspection := *inspection
	safeInspection.Evidence = make(map[string]interface{}, len(inspection.Evidence))
	for key, value := range inspection.Evidence {
		if key != "screenshotPath" {
			safeInspection.Evidence[key] = value
		}
	}
	data := map[string]interface{}{"inspection": &safeInspection}
	if records.PublishRecord != nil {
		data["publish_record"] = records.PublishRecord
	}
	if records.ContentRecord != nil {
		data["content_record"] = records.ContentRecord
	}
	return &tools.Result{OK: true, Tool: "publish.plan.inspect_manager", Message: "creator manager inspected", Data: data}, nil
}

func (s *Server) inspectEgoPublishManagerPlan(ctx context.Context, planID int64) (*publishplan.Inspection, error) {
	return s.inspectEgoPublishManagerTarget(ctx, planID, 0, "")
}

// inspectEgoPublishManagerTarget verifies exactly one frozen target. A batch
// plan therefore remains verifiable target-by-target without turning a
// creator-manager search into a broad, ambiguous scan.
func (s *Server) inspectEgoPublishManagerTarget(ctx context.Context, planID, accountID int64, platformCode string) (*publishplan.Inspection, error) {
	return s.inspectEgoPublishManagerTargetWithGate(ctx, planID, accountID, platformCode, false)
}

func (s *Server) inspectEgoPublishManagerTargetWithGate(ctx context.Context, planID, accountID int64, platformCode string, acceptance bool) (*publishplan.Inspection, error) {
	if s == nil || s.db == nil || s.egoSpaces == nil {
		return nil, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	plan, err := (&publishplan.Service{DB: s.db}).Get(planID)
	if err != nil {
		return nil, err
	}
	if len(plan.Snapshot.Targets) != 1 && accountID <= 0 {
		return nil, fmt.Errorf("PUBLISH_PLAN_INSPECTION_TARGET_REQUIRED")
	}
	var target publishplan.TargetSnapshot
	matched := 0
	for _, candidate := range plan.Snapshot.Targets {
		if accountID > 0 && candidate.AccountID != accountID {
			continue
		}
		if strings.TrimSpace(platformCode) != "" && candidate.PlatformCode != strings.TrimSpace(platformCode) {
			continue
		}
		target = candidate
		matched++
	}
	if matched != 1 {
		return nil, fmt.Errorf("PUBLISH_PLAN_INSPECTION_TARGET_REQUIRED")
	}
	account, err := s.db.GetAccount(target.AccountID)
	if err != nil || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
		return nil, fmt.Errorf("PUBLISH_ACCOUNT_NOT_LOGGED_IN")
	}
	var jobID int64
	verificationReady := false
	for _, state := range plan.TargetStates {
		if state.AccountID != target.AccountID || state.AssetID != target.AssetID || state.PlatformCode != target.PlatformCode {
			continue
		}
		jobID = state.JobID
		verificationReady = state.Status == "submitted_pending_verification" || state.Status == "result_unknown"
		break
	}
	if jobID <= 0 || !verificationReady {
		return nil, fmt.Errorf("PUBLISH_PLAN_VERIFICATION_NOT_READY")
	}
	if !egopublish.VerifyVerifiedFor(target.PlatformCode, plan.Snapshot.MediaMode) {
		adapterReady := egopublish.PrepareAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode) &&
			egopublish.SubmitAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode) &&
			egopublish.VerifyAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode)
		if !acceptance || !adapterReady {
			if acceptance {
				return nil, fmt.Errorf("EGO_PUBLISH_ACCEPTANCE_UNAVAILABLE")
			}
			return nil, fmt.Errorf("EGO_PUBLISH_VERIFY_UNVERIFIED")
		}
	}
	// target.PublishAt comes from the immutable plan snapshot. Pass it through
	// to the manager verifier so a caller cannot substitute a different time at
	// verification time.
	manager, err := s.egoSpaces.InspectPublishManagerTarget(ctx, target.AccountID, target.PlatformCode, publishManagerMarker(target), target.PublishAt)
	if err != nil {
		return nil, err
	}
	evidence := map[string]interface{}{
		"runtime": "ego-lite", "platform": target.PlatformCode,
		"inspectionStatus": manager.InspectionStatus,
		"matchingTitle":    manager.MatchingTitle, "publishedState": manager.PublishedState,
		"scheduledState": manager.ScheduledState, "effectiveScheduledAt": manager.EffectiveScheduledAt,
		"observedStatus": manager.ObservedStatus, "postURL": manager.PostURL,
		"externalPostID": manager.ExternalPostID,
	}
	if manager.ScreenshotPath != "" {
		evidence["screenshotPath"] = manager.ScreenshotPath
	}
	return &publishplan.Inspection{
		PlanID: plan.ID, JobID: jobID, AccountID: target.AccountID,
		AssetID: target.AssetID, PlatformCode: target.PlatformCode,
		Location: manager.Location, Evidence: evidence,
	}, nil
}

func publishManagerMarker(target publishplan.TargetSnapshot) string {
	if (target.PlatformCode != "tiktok" && target.PlatformCode != "kuaishou" && target.PlatformCode != "wechat_video" && target.PlatformCode != "facebook") || strings.TrimSpace(target.PublishAt) == "" {
		return strings.TrimSpace(target.Title)
	}
	for _, value := range []string{target.Caption, target.Body, target.Title} {
		for _, line := range strings.Split(value, "\n") {
			if marker := strings.TrimSpace(line); marker != "" {
				return marker
			}
		}
	}
	return ""
}

func (s *Server) recordPublishedManagerInspection(inspection *publishplan.Inspection) (*db.PublishRecord, error) {
	if s == nil || s.db == nil || inspection == nil {
		return nil, fmt.Errorf("complete creator manager evidence is required")
	}
	records, err := publishplan.PersistManagerInspection(s.db, inspection.PlanID, inspection)
	if err != nil || records.PublishRecord == nil {
		return nil, err
	}
	return records.PublishRecord, nil
}

func (s *Server) deletePublishContent(ctx context.Context, recordID int64) (*tools.Result, error) {
	if s == nil || s.db == nil || s.egoSpaces == nil || recordID <= 0 {
		err := fmt.Errorf("EGO_REMOTE_DELETE_INVALID")
		return &tools.Result{OK: false, Tool: "publish.content.delete", Error: err.Error()}, err
	}
	record, err := s.db.GetPublishContentRecord(recordID)
	if err != nil {
		return &tools.Result{OK: false, Tool: "publish.content.delete", Error: "PUBLISH_CONTENT_RECORD_NOT_FOUND"}, err
	}
	deleted, err := s.egoSpaces.DeleteScheduledPublishContent(ctx, *record)
	if err != nil {
		code := publishContentDeleteErrorCode(err)
		return &tools.Result{OK: false, Tool: "publish.content.delete", Error: code,
			Message: "remote scheduled content was not independently verified as deleted"}, err
	}
	if deleted == nil || !deleted.Deleted || strings.TrimSpace(deleted.ScreenshotPath) == "" {
		err := fmt.Errorf("EGO_REMOTE_DELETE_RESULT_INVALID")
		return &tools.Result{OK: false, Tool: "publish.content.delete", Error: err.Error()}, err
	}
	if err := s.db.MarkPublishContentDeleted(record.ID, deleted.ScreenshotPath); err != nil {
		return &tools.Result{OK: false, Tool: "publish.content.delete", Error: "PUBLISH_CONTENT_DELETE_RECORD_FAILED"}, err
	}
	if strings.TrimSpace(record.ScheduledAt) != "" {
		publishedEvidence, readErr := os.ReadFile(record.EvidencePath)
		if readErr != nil {
			return &tools.Result{OK: false, Tool: "publish.content.delete", Error: "PUBLISH_SUBMIT_SCHEDULE_RESOLVE_FAILED"}, readErr
		}
		deletedEvidence, readErr := os.ReadFile(deleted.ScreenshotPath)
		if readErr != nil {
			return &tools.Result{OK: false, Tool: "publish.content.delete", Error: "PUBLISH_SUBMIT_SCHEDULE_RESOLVE_FAILED"}, readErr
		}
		publishedDigest := sha256.Sum256(publishedEvidence)
		deletedDigest := sha256.Sum256(deletedEvidence)
		if err := s.db.ResolvePublishSubmitScheduled(
			record.JobID, record.EvidencePath, fmt.Sprintf("%x", publishedDigest[:]),
			deleted.ScreenshotPath, fmt.Sprintf("%x", deletedDigest[:]),
		); err != nil {
			return &tools.Result{OK: false, Tool: "publish.content.delete", Error: "PUBLISH_SUBMIT_SCHEDULE_RESOLVE_FAILED"}, err
		}
	}
	return &tools.Result{OK: true, Tool: "publish.content.delete", Message: "scheduled content deleted and verified",
		Data: map[string]interface{}{"record_id": record.ID, "deleted": true}}, nil
}

func publishContentDeleteErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	if len(code) > 0 && len(code) <= 96 && strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
		return code
	}
	return "EGO_REMOTE_DELETE_RESULT_UNKNOWN"
}

func (s *Server) revealPublishPlanSettings(ctx context.Context, planID int64) (*tools.Result, error) {
	inspection, err := s.inspectEgoPublishPlan(ctx, planID, true)
	if err != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.reveal_settings", Error: publishPlanInspectionErrorCode(err)}, err
	}
	return &tools.Result{OK: true, Tool: "publish.plan.reveal_settings", Message: "creator manager handed off in EgoLite; settings remain user-controlled", Data: map[string]interface{}{"inspection": inspection}}, nil
}

func (s *Server) resumePublishPlanTopics(ctx context.Context, planID int64) (*tools.Result, error) {
	inspection, err := s.inspectEgoPublishPlan(ctx, planID, true)
	if err != nil {
		return &tools.Result{OK: false, Tool: "publish.plan.resume_topics", Error: publishPlanInspectionErrorCode(err)}, err
	}
	return &tools.Result{OK: true, Tool: "publish.plan.resume_topics", Message: "creator manager handed off in EgoLite; topic repair is manual until verified", Data: map[string]interface{}{"inspection": inspection}}, nil
}

func (s *Server) inspectEgoPublishPlan(ctx context.Context, planID int64, handoff bool) (*publishplan.Inspection, error) {
	if s == nil || s.db == nil || s.egoSpaces == nil {
		return nil, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	plan, err := (&publishplan.Service{DB: s.db}).Get(planID)
	if err != nil {
		return nil, err
	}
	if len(plan.Snapshot.Targets) != 1 {
		return nil, fmt.Errorf("PUBLISH_PLAN_INSPECTION_TARGET_REQUIRED")
	}
	if handoff {
		return nil, fmt.Errorf("EGO_PUBLISH_MANUAL_TAKEOVER_DISABLED")
	}
	target := plan.Snapshot.Targets[0]
	account, err := s.db.GetAccount(target.AccountID)
	if err != nil || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
		return nil, fmt.Errorf("PUBLISH_ACCOUNT_NOT_LOGGED_IN")
	}
	evidence := map[string]interface{}{
		"runtime": "ego-lite", "platform": target.PlatformCode,
		"verified": false, "evidenceCode": "EGO_SNAPSHOT_UNVERIFIED",
	}
	observation, err := s.egoSpaces.ObserveAccount(ctx, target.AccountID)
	if err != nil {
		return nil, err
	}
	evidence["spaceId"] = observation.SpaceID
	evidence["snapshotAvailable"] = observation.SnapshotAvailable
	evidence["snapshotLength"] = observation.SnapshotLength
	evidence["inspectionStatus"] = "observed"
	evidence["inspectionStep"] = "ego_snapshot"
	var jobID int64
	for _, state := range plan.TargetStates {
		if state.AccountID == target.AccountID && state.AssetID == target.AssetID {
			jobID = state.JobID
			break
		}
	}
	return &publishplan.Inspection{
		PlanID: plan.ID, JobID: jobID, AccountID: target.AccountID,
		AssetID: target.AssetID, PlatformCode: target.PlatformCode, Evidence: evidence,
	}, nil
}

func publishPlanInspectionErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	switch code {
	case "PUBLISH_PLAN_NOT_FOUND", "PUBLISH_PLAN_INSPECTION_TARGET_REQUIRED", "PUBLISH_TARGET_SNAPSHOT_MISMATCH",
		"PUBLISH_ACCOUNT_NOT_LOGGED_IN", "PUBLISH_BROWSER_NOT_RUNNING", "PUBLISH_ACCOUNT_SESSION_LOGGED_OUT", "EGO_RUNTIME_UNAVAILABLE",
		"EGO_PUBLISH_MANUAL_TAKEOVER_DISABLED",
		"BROWSER_OPERATION_BUSY", "ACTIVE_PAGE_NOT_FOUND", "WORKER_SESSION_FAILED", "WORKER_START_FAILED",
		"PLAYWRIGHT_RUNTIME_UNAVAILABLE", "NODE_RUNTIME_UNAVAILABLE", "WORKER_CANCELLED":
		return code
	default:
		// Worker errors cross the process boundary as an uppercase, bounded code;
		// retain it for actionable UI diagnostics without exposing stderr or page text.
		if len(code) <= 80 && strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
			return code
		}
		return "PUBLISH_PLAN_INSPECTION_FAILED"
	}
}

// verifyCreatorBackend is the single post-click verification dispatcher. It
// never accepts caller-provided evidence and has no publish side effect. The
// EgoLite edition must not fall back to BrowserFacade/chromedp observers.
func (s *Server) verifyCreatorBackend(ctx context.Context, jobID int64) (publishplan.CreatorBackendVerification, error) {
	return s.verifyCreatorBackendForAction(ctx, jobID, false)
}

func (s *Server) verifyCreatorBackendForAction(ctx context.Context, jobID int64, acceptance bool) (publishplan.CreatorBackendVerification, error) {
	job, err := s.db.GetPublishJob(jobID)
	if err != nil {
		return publishplan.CreatorBackendVerification{}, err
	}
	if egopublish.VerifyVerifiedFor(job.PlatformCode, job.ContentType) || acceptance {
		if job.PlanID <= 0 {
			return publishplan.CreatorBackendVerification{}, fmt.Errorf("PUBLISH_PLAN_VERIFICATION_NOT_READY")
		}
		inspection, inspectErr := s.inspectEgoPublishManagerTargetWithGate(ctx, job.PlanID, job.AccountID, job.PlatformCode, acceptance)
		if inspectErr != nil {
			return publishplan.CreatorBackendVerification{}, inspectErr
		}
		if inspection.JobID != job.ID {
			return publishplan.CreatorBackendVerification{}, fmt.Errorf("PUBLISH_JOB_PLAN_MISMATCH")
		}
		records, persistErr := publishplan.PersistManagerInspection(s.db, job.PlanID, inspection)
		if persistErr != nil {
			return publishplan.CreatorBackendVerification{}, persistErr
		}
		if records.PublishRecord != nil {
			return publishplan.CreatorBackendVerification{
				Status: "published", Step: "creator_backend_verified",
			}, nil
		}
		if records.ContentRecord != nil {
			return publishplan.CreatorBackendVerification{
				Status: "scheduled", Step: "creator_backend_scheduled",
			}, nil
		}
		return publishplan.CreatorBackendVerification{
			Status: "awaiting_review", Step: "creator_backend_verification",
			ErrorCode: strings.ToUpper(job.PlatformCode) + "_EGO_BACKEND_EVIDENCE_MISSING",
		}, nil
	}
	return publishplan.CreatorBackendVerification{
		Status: "awaiting_review", Step: "creator_backend_verification",
		ErrorCode: "EGO_PUBLISH_VERIFY_UNVERIFIED",
	}, nil
}

func (s *Server) handleAPIPublishPlanPrepare(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	var body struct {
		SnapshotHash string `json:"snapshotHash"`
		ApprovalID   string `json:"approvalId"`
		Nonce        string `json:"nonce"`
	}
	if !decodePublishPlanJSON(w, r, &body) {
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	executionCtx, cancel := publishPlanExecutionContext()
	defer cancel()
	result, _ := tools.Default.Call(executionCtx, localRuntime, "publish.plan.prepare", map[string]interface{}{
		"plan_id": id, "snapshot_hash": body.SnapshotHash, "approval_id": body.ApprovalID, "nonce": body.Nonce,
	})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		if execution := result.Data["execution"]; execution != nil {
			writeJSON(w, execution)
			return
		}
		status := http.StatusConflict
		if result.Error == "TRUSTED_LOCAL_UI_REQUIRED" || result.Error == "VALIDATION_FAILED" {
			status = http.StatusBadRequest
		}
		writeToolsError(w, status, result.Error)
		return
	}
	writeJSON(w, result.Data["execution"])
}

func (s *Server) handleAPIPublishPlanSubmit(w http.ResponseWriter, r *http.Request) {
	id, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	var body struct {
		SnapshotHash string `json:"snapshotHash"`
		ApprovalID   string `json:"approvalId"`
		Nonce        string `json:"nonce"`
	}
	if !decodePublishPlanJSON(w, r, &body) {
		return
	}
	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	executionCtx, cancel := publishPlanExecutionContext()
	defer cancel()
	result, _ := tools.Default.Call(executionCtx, localRuntime, "publish.plan.submit", map[string]interface{}{
		"plan_id": id, "snapshot_hash": body.SnapshotHash, "approval_id": body.ApprovalID, "nonce": body.Nonce,
	})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK {
		if execution := result.Data["execution"]; execution != nil {
			writeJSON(w, execution)
			return
		}
		status := http.StatusConflict
		if result.Error == "TRUSTED_LOCAL_UI_REQUIRED" || result.Error == "VALIDATION_FAILED" {
			status = http.StatusBadRequest
		}
		writeToolsError(w, status, result.Error)
		return
	}
	writeJSON(w, result.Data["execution"])
}
