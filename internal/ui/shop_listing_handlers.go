package ui

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/shoplisting"
	"alpha-nexus-egolite/internal/tools"
)

const shopListingPrepareApprovalTTL = 5 * time.Minute

type shopListingApprovalResponse struct {
	ApprovalID string `json:"approvalId"`
	Nonce      string `json:"nonce"`
	ExpiresAt  string `json:"expiresAt"`
}

func (s *Server) handleAPIShopListingPrepareApproval(w http.ResponseWriter, r *http.Request) {
	planID, ok := publishPlanID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	var body struct {
		SnapshotHash string `json:"snapshotHash"`
	}
	if !decodePublishPlanJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.SnapshotHash) == "" {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}

	sessionID := s.healthSnapshot().Nonce
	if sessionID == "" {
		sessionID = "desktop-local"
	}
	grant, err := (&shoplisting.Service{DB: s.db}).ApprovePrepare(
		planID, body.SnapshotHash, sessionID, shopListingPrepareApprovalTTL,
	)
	if err != nil {
		writeToolsError(w, http.StatusConflict, shopListingStableErrorCode(err, "SHOP_LISTING_PREPARE_APPROVAL_REJECTED"))
		return
	}
	writeJSON(w, shopListingApprovalResponse{
		ApprovalID: grant.ID,
		Nonce:      grant.Nonce,
		ExpiresAt:  grant.ExpiresAt,
	})
}

func (s *Server) handleAPIShopListingPrepare(w http.ResponseWriter, r *http.Request) {
	planID, ok := publishPlanID(r)
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
	if strings.TrimSpace(body.SnapshotHash) == "" || strings.TrimSpace(body.ApprovalID) == "" || strings.TrimSpace(body.Nonce) == "" {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if s.toolRuntime == nil {
		writeToolsError(w, http.StatusInternalServerError, "SHOP_LISTING_RUNTIME_UNAVAILABLE")
		return
	}

	localRuntime := s.toolRuntime.WithTrustedLocalUI(true)
	executionCtx, cancel := publishPlanExecutionContext()
	defer cancel()
	result, callErr := tools.Default.Call(executionCtx, localRuntime, "shop.listing.prepare", map[string]interface{}{
		"input": map[string]interface{}{
			"plan_id": planID, "snapshot_hash": body.SnapshotHash,
			"approval_id": body.ApprovalID, "nonce": body.Nonce,
		},
	})
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if !result.OK || callErr != nil {
		code := shopListingStableErrorString(result.Error, shopListingStableErrorCode(callErr, "SHOP_LISTING_PREPARE_FAILED"))
		status := http.StatusConflict
		if code == "VALIDATION_FAILED" || code == "SHOP_LISTING_PREPARE_INPUT_INVALID" || code == "TRUSTED_LOCAL_UI_REQUIRED" {
			status = http.StatusBadRequest
		} else if code == "TOOL_NOT_FOUND" || code == "TOOL_NOT_IMPLEMENTED" || code == "SHOP_LISTING_RUNTIME_UNAVAILABLE" {
			status = http.StatusInternalServerError
		}
		writeToolsError(w, status, code)
		return
	}
	writeJSON(w, result.Data)
}

// PrepareShopListing is the trusted Registry callback for one approved local
// shop-listing plan. It only opens the manager for review; it never submits.
func (s *Server) PrepareShopListing(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*tools.Result, error) {
	service := &shoplisting.Service{DB: s.db}
	plan, err := service.ConsumePrepareApproval(planID, snapshotHash, approvalID, nonce)
	if err != nil {
		return shopListingPrepareFailure("SHOP_LISTING_PREPARE_APPROVAL_REJECTED", err)
	}
	if s.egoSpaces == nil {
		err := errors.New("EGO_RUNTIME_UNAVAILABLE")
		return markShopListingFailedHandoff(service, plan, snapshotHash, "EGO_RUNTIME_UNAVAILABLE", err)
	}

	space, handoffErr := s.egoSpaces.OpenManagerAndHandoff(ctx, plan.AccountID)
	if handoffErr != nil {
		code := shopListingStableErrorCode(handoffErr, "EGO_MANAGER_HANDOFF_FAILED")
		if code == "EGO_SPACE_USER_CONTROLLED" {
			return s.markShopListingManualTakeover(service, plan, snapshotHash, code, nil)
		}
		return markShopListingFailedHandoff(service, plan, snapshotHash, code, handoffErr)
	}
	if space == nil {
		err := errors.New("EGO_HANDOFF_FAILED")
		return markShopListingFailedHandoff(service, plan, snapshotHash, "EGO_HANDOFF_FAILED", err)
	}
	return s.markShopListingManualTakeover(service, plan, snapshotHash, "SHOP_LISTING_FORM_REVIEW_REQUIRED", space)
}

func markShopListingFailedHandoff(service *shoplisting.Service, plan *shoplisting.Plan, snapshotHash, errorCode string, cause error) (*tools.Result, error) {
	if _, err := service.MarkManualTakeover(plan.ID, snapshotHash, errorCode); err != nil {
		return shopListingPrepareFailure(shopListingStableErrorCode(err, "SHOP_LISTING_MANUAL_TAKEOVER_FAILED"), err)
	}
	return shopListingPrepareFailure(errorCode, cause)
}

func (s *Server) markShopListingManualTakeover(service *shoplisting.Service, plan *shoplisting.Plan, snapshotHash, errorCode string, space *db.EgoSpace) (*tools.Result, error) {
	manualPlan, err := service.MarkManualTakeover(plan.ID, snapshotHash, errorCode)
	if err != nil {
		return shopListingPrepareFailure(shopListingStableErrorCode(err, "SHOP_LISTING_MANUAL_TAKEOVER_FAILED"), err)
	}
	if space == nil && s.db != nil {
		space, _ = s.db.GetEgoSpaceByAccount(plan.AccountID)
	}
	return &tools.Result{
		OK:      true,
		Tool:    "shop.listing.prepare",
		Message: "商品后台已交给用户复核",
		Data:    shopListingPrepareSummary(manualPlan, space),
	}, nil
}

func shopListingPrepareSummary(plan *shoplisting.Plan, space *db.EgoSpace) map[string]interface{} {
	spaceSummary := interface{}(nil)
	if space != nil {
		spaceSummary = map[string]interface{}{
			"id":        space.ID,
			"space_id":  space.SpaceID,
			"ownership": space.Ownership,
			"state":     space.State,
		}
	}
	return map[string]interface{}{
		"plan": map[string]interface{}{
			"id":            plan.ID,
			"snapshot_hash": plan.SnapshotHash,
			"status":        plan.Status,
		},
		"account":    map[string]interface{}{"id": plan.AccountID},
		"identity":   map[string]interface{}{"id": plan.IdentityID},
		"platform":   plan.PlatformCode,
		"space":      spaceSummary,
		"status":     plan.Status,
		"state":      plan.Status,
		"error_code": plan.ErrorCode,
	}
}

func shopListingPrepareFailure(code string, err error) (*tools.Result, error) {
	if strings.TrimSpace(code) == "" {
		code = "SHOP_LISTING_PREPARE_FAILED"
	}
	if err == nil {
		err = errors.New(code)
	}
	return &tools.Result{OK: false, Tool: "shop.listing.prepare", Error: code}, err
}

func shopListingStableErrorCode(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return shopListingStableErrorString(err.Error(), fallback)
}

func shopListingStableErrorString(raw, fallback string) string {
	code := strings.TrimSpace(raw)
	if separator := strings.IndexAny(code, ": \t\r\n"); separator >= 0 {
		code = code[:separator]
	}
	if len(code) == 0 || len(code) > 80 || code[0] < 'A' || code[0] > 'Z' || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
		return fallback
	}
	return code
}
