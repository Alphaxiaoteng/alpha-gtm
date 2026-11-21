package shoplisting

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/db"
)

const (
	SnapshotVersion          = 1
	StatusFrozen             = "frozen"
	StatusDryRunOK           = "dry_run_ok"
	StatusPrepareApproved    = "prepare_approved"
	StatusManualTakeover     = "manual_takeover"
	itemStatusPending        = "pending"
	itemStatusManualTakeover = "manual_takeover"

	prepareApprovalToolName = "shop.listing.prepare"
	approvalCapability      = "external_write"
	defaultApprovalTTL      = 5 * time.Minute
	maxApprovalTTL          = 30 * time.Minute
	defaultListLimit        = 20
	maxListLimit            = 100

	maxItems               = 100
	maxTitleRunes          = 200
	maxDescriptionRunes    = 20000
	maxVendorRunes         = 200
	maxBrandRunes          = 200
	maxCategoryRunes       = 200
	maxSKUrun              = 128
	maxVariantTitleRunes   = 200
	maxMedia               = 20
	maxVariants            = 100
	maxOptions             = 32
	maxPriceMinor          = int64(99999999999999)
	maxInventory           = int64(1000000000)
	maxWeightGrams         = int64(1000000000)
	maxIdempotencyKeyRunes = 128
)

var (
	ErrIdempotencyConflict = errors.New("SHOP_LISTING_IDEMPOTENCY_CONFLICT")
	sha256Pattern          = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	currencyPattern        = regexp.MustCompile(`^[A-Z]{3}$`)
	errorCodePattern       = regexp.MustCompile(`^[A-Z][A-Z_]{0,63}$`)
)

var supportedPlatforms = map[string]struct{}{
	"douyin_shop":    {},
	"taobao":         {},
	"tiktok_shop":    {},
	"shopee":         {},
	"shopify":        {},
	"amazon_seller":  {},
	"jd_shop":        {},
	"pinduoduo_shop": {},
	"xhs_shop":       {},
	"kuaishou_shop":  {},
	"wechat_shop":    {},
	"alibaba_1688":   {},
	"aliexpress":     {},
}

// Service owns the local freeze/dry-run and approval contract. It deliberately
// has no browser, Registry, network, or UI dependency.
type Service struct {
	DB  *db.DB
	Now func() time.Time
}

type FreezeInput struct {
	IdentityID     int64       `json:"identity_id"`
	AccountID      int64       `json:"account_id"`
	PlatformCode   string      `json:"platform_code"`
	IdempotencyKey string      `json:"idempotency_key"`
	Concurrency    int         `json:"concurrency,omitempty"`
	Items          []ItemInput `json:"items"`
}

type ItemInput struct {
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Vendor         string         `json:"vendor,omitempty"`
	Brand          string         `json:"brand,omitempty"`
	Category       string         `json:"category"`
	Currency       string         `json:"currency"`
	PriceMinor     int64          `json:"price_minor"`
	CompareAtMinor *int64         `json:"compare_at_minor,omitempty"`
	Inventory      int64          `json:"inventory"`
	WeightGrams    int64          `json:"weight_grams"`
	Media          []MediaInput   `json:"media,omitempty"`
	Variants       []VariantInput `json:"variants,omitempty"`
}

type MediaInput struct {
	FilePath string `json:"file_path"`
	SHA256   string `json:"sha256"`
	Role     string `json:"role"`
}

type VariantInput struct {
	SKU        string            `json:"sku"`
	Title      string            `json:"title,omitempty"`
	PriceMinor int64             `json:"price_minor"`
	Inventory  int64             `json:"inventory"`
	Options    map[string]string `json:"options,omitempty"`
}

type Snapshot struct {
	Version      int         `json:"version"`
	IdentityID   int64       `json:"identity_id"`
	AccountID    int64       `json:"account_id"`
	PlatformCode string      `json:"platform_code"`
	Concurrency  int         `json:"concurrency"`
	Items        []ItemInput `json:"items"`
}

type DryRunResult struct {
	Snapshot     Snapshot `json:"snapshot"`
	SnapshotJSON string   `json:"snapshot_json"`
	SnapshotHash string   `json:"snapshot_hash"`
}

type ApprovalGrant struct {
	ID        string `json:"approval_id"`
	Nonce     string `json:"nonce"`
	ExpiresAt string `json:"expires_at"`
}

type Plan struct {
	ID             int64        `json:"id"`
	IdentityID     int64        `json:"identity_id"`
	AccountID      int64        `json:"account_id"`
	PlatformCode   string       `json:"platform_code"`
	IdempotencyKey string       `json:"idempotency_key"`
	SnapshotJSON   string       `json:"snapshot_json"`
	SnapshotHash   string       `json:"snapshot_hash"`
	Status         string       `json:"status"`
	Concurrency    int          `json:"concurrency"`
	CreatedAt      string       `json:"created_at"`
	FrozenAt       string       `json:"frozen_at"`
	UpdatedAt      string       `json:"updated_at"`
	ErrorCode      string       `json:"error_code"`
	Items          []ItemRecord `json:"items"`
}

// PlanSummary is the restart-safe, redacted listing surface. It deliberately
// contains neither the idempotency key nor any snapshot/item payload.
type PlanSummary struct {
	PlanID       int64  `json:"plan_id"`
	IdentityID   int64  `json:"identity_id"`
	AccountID    int64  `json:"account_id"`
	PlatformCode string `json:"platform_code"`
	SnapshotHash string `json:"snapshot_hash"`
	Status       string `json:"status"`
	Concurrency  int    `json:"concurrency"`
	ItemCount    int    `json:"item_count"`
	CreatedAt    string `json:"created_at"`
	FrozenAt     string `json:"frozen_at"`
	UpdatedAt    string `json:"updated_at"`
	ErrorCode    string `json:"error_code"`
}

type ItemRecord struct {
	ID          int64  `json:"id"`
	PlanID      int64  `json:"plan_id"`
	Ordinal     int    `json:"ordinal"`
	PayloadJSON string `json:"payload_json"`
	PayloadHash string `json:"payload_hash"`
	Status      string `json:"status"`
	ErrorCode   string `json:"error_code"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Service) DryRun(input FreezeInput) (*DryRunResult, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("shop listing database is required")
	}
	normalized, err := s.validate(input)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

// Freeze validates and persists one immutable local product snapshot. A
// replay with the same idempotency key and snapshot returns the original plan;
// reusing the key for another snapshot fails closed.
func (s *Service) Freeze(input FreezeInput) (*Plan, bool, error) {
	input.PlatformCode = strings.TrimSpace(input.PlatformCode)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	run, err := s.DryRun(input)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	nowText := now.Format(time.RFC3339Nano)

	tx, err := s.DB.Begin()
	if err != nil {
		return nil, false, fmt.Errorf("begin shop listing freeze: %w", err)
	}
	defer tx.Rollback()

	var existingID int64
	var existingHash string
	err = tx.QueryRow(`SELECT id, snapshot_hash FROM shop_listing_plans WHERE idempotency_key=?`, input.IdempotencyKey).Scan(&existingID, &existingHash)
	switch {
	case err == nil:
		if existingHash != run.SnapshotHash {
			return nil, false, fmt.Errorf("%w: idempotency key already binds another snapshot", ErrIdempotencyConflict)
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit shop listing replay: %w", err)
		}
		plan, err := s.Get(existingID)
		return plan, false, err
	case err != sql.ErrNoRows:
		return nil, false, fmt.Errorf("lookup shop listing idempotency key: %w", err)
	}

	result, err := tx.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		input.IdentityID, input.AccountID, input.PlatformCode, run.SnapshotJSON, run.SnapshotHash,
		input.IdempotencyKey, StatusDryRunOK, nowText, nowText, nowText)
	if err != nil {
		var existing int64
		var existingSnapshotHash string
		if lookupErr := tx.QueryRow(`SELECT id, snapshot_hash FROM shop_listing_plans WHERE idempotency_key=?`, input.IdempotencyKey).Scan(&existing, &existingSnapshotHash); lookupErr == nil {
			if existingSnapshotHash != run.SnapshotHash {
				return nil, false, fmt.Errorf("%w: idempotency key already binds another snapshot", ErrIdempotencyConflict)
			}
			if err := tx.Rollback(); err != nil {
				return nil, false, fmt.Errorf("rollback shop listing replay: %w", err)
			}
			plan, getErr := s.Get(existing)
			return plan, false, getErr
		}
		return nil, false, fmt.Errorf("insert shop listing plan: %w", err)
	}
	planID, err := result.LastInsertId()
	if err != nil {
		return nil, false, fmt.Errorf("read shop listing plan id: %w", err)
	}
	for ordinal, item := range run.Snapshot.Items {
		payload, err := json.Marshal(item)
		if err != nil {
			return nil, false, fmt.Errorf("encode shop listing item %d: %w", ordinal, err)
		}
		itemDigest := sha256.Sum256(payload)
		itemHash := hex.EncodeToString(itemDigest[:])
		if _, err := tx.Exec(`INSERT INTO shop_listing_items
			(plan_id, ordinal, payload_json, payload_hash, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, planID, ordinal, string(payload), itemHash, itemStatusPending, nowText, nowText); err != nil {
			return nil, false, fmt.Errorf("insert shop listing item %d: %w", ordinal, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit shop listing freeze: %w", err)
	}
	plan, err := s.Get(planID)
	return plan, true, err
}

func (s *Service) Get(id int64) (*Plan, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("shop listing database is required")
	}
	return loadPlan(s.DB, id)
}

// ListSummaries returns only the persisted plan metadata needed to recover a
// local Shop workflow after restart. The query intentionally never selects
// snapshot_json, idempotency_key, or any item payload.
func (s *Service) ListSummaries(limit int) ([]PlanSummary, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("shop listing database is required")
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	rows, err := s.DB.Query(`SELECT p.id, p.identity_id, p.account_id, p.platform_code,
		p.snapshot_hash, p.status, p.concurrency,
		(SELECT COUNT(*) FROM shop_listing_items i WHERE i.plan_id=p.id),
		p.created_at, p.frozen_at, p.updated_at, p.error_code
		FROM shop_listing_plans p ORDER BY p.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make([]PlanSummary, 0, limit)
	for rows.Next() {
		var summary PlanSummary
		if err := rows.Scan(
			&summary.PlanID, &summary.IdentityID, &summary.AccountID, &summary.PlatformCode,
			&summary.SnapshotHash, &summary.Status, &summary.Concurrency, &summary.ItemCount,
			&summary.CreatedAt, &summary.FrozenAt, &summary.UpdatedAt, &summary.ErrorCode,
		); err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summaries, nil
}

type shopListingQuerier interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}

func loadPlan(queryer shopListingQuerier, id int64) (*Plan, error) {
	if id <= 0 {
		return nil, errors.New("shop listing plan id is required")
	}
	var plan Plan
	err := queryer.QueryRow(`SELECT id, identity_id, account_id, platform_code, idempotency_key,
		snapshot_json, snapshot_hash, status, concurrency, created_at, frozen_at, updated_at, error_code
		FROM shop_listing_plans WHERE id=?`, id).Scan(
		&plan.ID, &plan.IdentityID, &plan.AccountID, &plan.PlatformCode, &plan.IdempotencyKey,
		&plan.SnapshotJSON, &plan.SnapshotHash, &plan.Status, &plan.Concurrency,
		&plan.CreatedAt, &plan.FrozenAt, &plan.UpdatedAt, &plan.ErrorCode)
	if err == sql.ErrNoRows {
		return nil, errors.New("shop listing plan not found")
	}
	if err != nil {
		return nil, err
	}
	rows, err := queryer.Query(`SELECT id, plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at
		FROM shop_listing_items WHERE plan_id=? ORDER BY ordinal`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ItemRecord
		if err := rows.Scan(&item.ID, &item.PlanID, &item.Ordinal, &item.PayloadJSON, &item.PayloadHash, &item.Status, &item.ErrorCode, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		plan.Items = append(plan.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := validatePlanIntegrity(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func validatePlanIntegrity(plan *Plan) error {
	if plan == nil {
		return errors.New("SHOP_LISTING_PLAN_REQUIRED")
	}
	digest := sha256.Sum256([]byte(plan.SnapshotJSON))
	if !constantTimeEqual(plan.SnapshotHash, hex.EncodeToString(digest[:])) {
		return errors.New("SHOP_LISTING_SNAPSHOT_INTEGRITY_FAILED")
	}
	if plan.Concurrency != 1 {
		return errors.New("SHOP_LISTING_CONCURRENCY_INVALID")
	}
	if plan.ErrorCode != "" && !errorCodePattern.MatchString(plan.ErrorCode) {
		return errors.New("SHOP_LISTING_ERROR_CODE_INVALID")
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(plan.SnapshotJSON), &snapshot); err != nil {
		return errors.New("SHOP_LISTING_SNAPSHOT_INVALID")
	}
	canonical, err := json.Marshal(snapshot)
	if err != nil || !constantTimeEqual(string(canonical), plan.SnapshotJSON) {
		return errors.New("SHOP_LISTING_SNAPSHOT_NOT_CANONICAL")
	}
	if snapshot.Version != SnapshotVersion || snapshot.IdentityID != plan.IdentityID ||
		snapshot.AccountID != plan.AccountID || snapshot.PlatformCode != plan.PlatformCode ||
		snapshot.Concurrency != 1 || len(snapshot.Items) == 0 || len(snapshot.Items) != len(plan.Items) {
		return errors.New("SHOP_LISTING_SNAPSHOT_TARGET_MISMATCH")
	}
	for index, item := range plan.Items {
		if item.PlanID != plan.ID || item.Ordinal != index || item.ErrorCode != "" && !errorCodePattern.MatchString(item.ErrorCode) {
			return fmt.Errorf("SHOP_LISTING_ITEM_%d_INVALID", index)
		}
		itemDigest := sha256.Sum256([]byte(item.PayloadJSON))
		if !constantTimeEqual(item.PayloadHash, hex.EncodeToString(itemDigest[:])) {
			return fmt.Errorf("shop listing item %d integrity check failed", item.ID)
		}
		var payload ItemInput
		if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("SHOP_LISTING_ITEM_%d_INVALID", index)
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil || !constantTimeEqual(string(payloadJSON), item.PayloadJSON) {
			return fmt.Errorf("SHOP_LISTING_ITEM_%d_NOT_CANONICAL", index)
		}
		expectedJSON, err := json.Marshal(snapshot.Items[index])
		if err != nil || !constantTimeEqual(string(expectedJSON), item.PayloadJSON) {
			return fmt.Errorf("SHOP_LISTING_ITEM_%d_SNAPSHOT_MISMATCH", index)
		}
	}
	return nil
}

func prepareApprovalArgsHash(plan *Plan) (string, error) {
	if plan == nil {
		return "", errors.New("SHOP_LISTING_PLAN_REQUIRED")
	}
	type binding struct {
		PlanID       int64  `json:"plan_id"`
		SnapshotHash string `json:"snapshot_hash"`
		IdentityID   int64  `json:"identity_id"`
		AccountID    int64  `json:"account_id"`
		PlatformCode string `json:"platform_code"`
	}
	encoded, err := json.Marshal(binding{
		PlanID: plan.ID, SnapshotHash: plan.SnapshotHash, IdentityID: plan.IdentityID,
		AccountID: plan.AccountID, PlatformCode: plan.PlatformCode,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Service) ApprovePrepare(planID int64, snapshotHash, sessionID string, ttl time.Duration) (*ApprovalGrant, error) {
	if s == nil || s.DB == nil || planID <= 0 {
		return nil, errors.New("SHOP_LISTING_PREPARE_APPROVAL_REQUIRED")
	}
	plan, err := s.Get(planID)
	if err != nil {
		return nil, err
	}
	if err := validatePlanIntegrity(plan); err != nil {
		return nil, err
	}
	if !constantTimeEqual(plan.SnapshotHash, strings.TrimSpace(snapshotHash)) {
		return nil, errors.New("SHOP_LISTING_SNAPSHOT_CHANGED")
	}
	if plan.Status != StatusDryRunOK {
		return nil, errors.New("SHOP_LISTING_PLAN_NOT_DRY_RUN_OK")
	}
	if err := s.validateTarget(plan.IdentityID, plan.AccountID, plan.PlatformCode); err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = defaultApprovalTTL
	}
	if ttl > maxApprovalTTL {
		return nil, errors.New("SHOP_LISTING_APPROVAL_TTL_TOO_LONG")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "desktop-local"
	}
	argsHash, err := prepareApprovalArgsHash(plan)
	if err != nil {
		return nil, err
	}
	idPart, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	nonce, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	nonceDigest := sha256.Sum256([]byte(nonce))
	now := s.now()
	expiresAt := now.Add(ttl).Format(time.RFC3339Nano)
	id := "shop-approval-" + idPart
	_, err = s.DB.Exec(`INSERT INTO approvals
		(id, session_id, tool_name, args_hash, capability, target_resource, expires_at, nonce_hash, single_use, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		id, sessionID, prepareApprovalToolName, argsHash, approvalCapability,
		fmt.Sprintf("shop_listing_plan:%d", plan.ID), expiresAt,
		hex.EncodeToString(nonceDigest[:]), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &ApprovalGrant{ID: id, Nonce: nonce, ExpiresAt: expiresAt}, nil
}

func (s *Service) ConsumePrepareApproval(planID int64, snapshotHash, approvalID, nonce string) (*Plan, error) {
	if s == nil || s.DB == nil || planID <= 0 || strings.TrimSpace(approvalID) == "" || nonce == "" {
		return nil, errors.New("SHOP_LISTING_PREPARE_APPROVAL_REQUIRED")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin shop listing approval: %w", err)
	}
	defer tx.Rollback()
	plan, err := loadPlan(tx, planID)
	if err != nil {
		return nil, err
	}
	if err := validatePlanIntegrity(plan); err != nil {
		return nil, err
	}
	if err := validateTargetQuery(tx, plan.IdentityID, plan.AccountID, plan.PlatformCode); err != nil {
		return nil, err
	}
	providedHash := strings.TrimSpace(snapshotHash)
	if !constantTimeEqual(plan.SnapshotHash, providedHash) {
		return nil, errors.New("SHOP_LISTING_SNAPSHOT_CHANGED")
	}
	if plan.Status != StatusDryRunOK {
		return nil, errors.New("SHOP_LISTING_PLAN_NOT_DRY_RUN_OK")
	}
	expectedArgsHash, err := prepareApprovalArgsHash(plan)
	if err != nil {
		return nil, err
	}
	var toolName, storedArgsHash, targetResource, expiresAt, storedNonceHash string
	var singleUse int
	var consumedAt sql.NullString
	if err := tx.QueryRow(`SELECT tool_name, args_hash, target_resource, expires_at, nonce_hash, single_use, consumed_at
		FROM approvals WHERE id=?`, approvalID).
		Scan(&toolName, &storedArgsHash, &targetResource, &expiresAt, &storedNonceHash, &singleUse, &consumedAt); err != nil {
		return nil, errors.New("SHOP_LISTING_PREPARE_APPROVAL_INVALID")
	}
	parsedExpiry, parseErr := time.Parse(time.RFC3339Nano, expiresAt)
	now := s.now()
	expiryOK := parseErr == nil && now.Before(parsedExpiry)
	nonceDigest := sha256.Sum256([]byte(nonce))
	toolOK := constantTimeEqual(toolName, prepareApprovalToolName)
	targetOK := constantTimeEqual(targetResource, fmt.Sprintf("shop_listing_plan:%d", planID))
	argsOK := constantTimeEqual(storedArgsHash, expectedArgsHash)
	nonceOK := constantTimeEqual(storedNonceHash, hex.EncodeToString(nonceDigest[:]))
	if !expiryOK || singleUse != 1 || consumedAt.Valid || !toolOK || !targetOK || !argsOK || !nonceOK {
		return nil, errors.New("SHOP_LISTING_PREPARE_APPROVAL_INVALID")
	}
	updatedAt := now.Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE approvals SET consumed_at=? WHERE id=? AND single_use=1 AND consumed_at IS NULL`, updatedAt, approvalID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return nil, errors.New("SHOP_LISTING_PREPARE_APPROVAL_REPLAY")
	}
	result, err = tx.Exec(`UPDATE shop_listing_plans SET status=?, updated_at=?
		WHERE id=? AND status=? AND snapshot_hash=?`, StatusPrepareApproved, updatedAt, planID, StatusDryRunOK, providedHash)
	if err != nil {
		return nil, err
	}
	changed, err = result.RowsAffected()
	if err != nil || changed != 1 {
		return nil, errors.New("SHOP_LISTING_PREPARE_APPROVAL_CONFLICT")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(planID)
}

func (s *Service) MarkManualTakeover(planID int64, snapshotHash, errorCode string) (*Plan, error) {
	if s == nil || s.DB == nil || planID <= 0 {
		return nil, errors.New("SHOP_LISTING_MANUAL_TAKEOVER_REQUIRED")
	}
	errorCode = strings.TrimSpace(errorCode)
	if !errorCodePattern.MatchString(errorCode) {
		return nil, errors.New("SHOP_LISTING_ERROR_CODE_INVALID")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin shop listing manual takeover: %w", err)
	}
	defer tx.Rollback()
	plan, err := loadPlan(tx, planID)
	if err != nil {
		return nil, err
	}
	if err := validatePlanIntegrity(plan); err != nil {
		return nil, err
	}
	if err := validateTargetQuery(tx, plan.IdentityID, plan.AccountID, plan.PlatformCode); err != nil {
		return nil, err
	}
	if !constantTimeEqual(plan.SnapshotHash, strings.TrimSpace(snapshotHash)) {
		return nil, errors.New("SHOP_LISTING_SNAPSHOT_CHANGED")
	}
	if plan.Status != StatusDryRunOK && plan.Status != StatusPrepareApproved {
		return nil, errors.New("SHOP_LISTING_PLAN_NOT_READY_FOR_MANUAL_TAKEOVER")
	}
	now := s.now().Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE shop_listing_plans SET status=?, error_code=?, updated_at=?
		WHERE id=? AND status IN (?,?) AND snapshot_hash=?`, StatusManualTakeover, errorCode, now,
		planID, StatusDryRunOK, StatusPrepareApproved, plan.SnapshotHash)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return nil, errors.New("SHOP_LISTING_MANUAL_TAKEOVER_CONFLICT")
	}
	result, err = tx.Exec(`UPDATE shop_listing_items SET status=?, error_code=?, updated_at=? WHERE plan_id=?`,
		itemStatusManualTakeover, errorCode, now, planID)
	if err != nil {
		return nil, err
	}
	itemChanged, err := result.RowsAffected()
	if err != nil || itemChanged != int64(len(plan.Items)) {
		return nil, errors.New("SHOP_LISTING_ITEM_STATE_CONFLICT")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(planID)
}

func (s *Service) validate(input FreezeInput) (*DryRunResult, error) {
	input.PlatformCode = strings.TrimSpace(input.PlatformCode)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdentityID <= 0 || input.AccountID <= 0 {
		return nil, errors.New("SHOP_LISTING_IDENTITY_ACCOUNT_REQUIRED")
	}
	if _, ok := supportedPlatforms[input.PlatformCode]; !ok {
		return nil, errors.New("SHOP_LISTING_PLATFORM_UNSUPPORTED")
	}
	if input.IdempotencyKey == "" || !utf8.ValidString(input.IdempotencyKey) || utf8.RuneCountInString(input.IdempotencyKey) > maxIdempotencyKeyRunes || strings.IndexFunc(input.IdempotencyKey, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return nil, errors.New("SHOP_LISTING_IDEMPOTENCY_KEY_INVALID")
	}
	if len(input.Items) == 0 || len(input.Items) > maxItems {
		return nil, errors.New("SHOP_LISTING_ITEMS_OUT_OF_BOUNDS")
	}
	if input.Concurrency != 0 && input.Concurrency != 1 {
		return nil, errors.New("SHOP_LISTING_CONCURRENCY_INVALID")
	}
	if err := s.validateTarget(input.IdentityID, input.AccountID, input.PlatformCode); err != nil {
		return nil, err
	}
	items := make([]ItemInput, len(input.Items))
	for i, item := range input.Items {
		normalized, err := normalizeItem(item)
		if err != nil {
			return nil, fmt.Errorf("SHOP_LISTING_ITEM_%d_INVALID: %w", i, err)
		}
		items[i] = normalized
	}
	snapshot := Snapshot{
		Version: SnapshotVersion, IdentityID: input.IdentityID, AccountID: input.AccountID,
		PlatformCode: input.PlatformCode, Concurrency: 1, Items: items,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode shop listing snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return &DryRunResult{Snapshot: snapshot, SnapshotJSON: string(encoded), SnapshotHash: hex.EncodeToString(digest[:])}, nil
}

func (s *Service) validateTarget(identityID, accountID int64, platformCode string) error {
	return validateTargetQuery(s.DB, identityID, accountID, platformCode)
}

func validateTargetQuery(queryer shopListingQuerier, identityID, accountID int64, platformCode string) error {
	var actualIdentity int64
	var actualPlatform string
	err := queryer.QueryRow(`SELECT identity_id, platform_code FROM accounts WHERE id=?`, accountID).Scan(&actualIdentity, &actualPlatform)
	if err == sql.ErrNoRows {
		return errors.New("SHOP_LISTING_ACCOUNT_NOT_FOUND")
	}
	if err != nil {
		return fmt.Errorf("validate shop listing account: %w", err)
	}
	if actualIdentity != identityID {
		return errors.New("SHOP_LISTING_ACCOUNT_IDENTITY_MISMATCH")
	}
	if actualPlatform != platformCode {
		return errors.New("SHOP_LISTING_ACCOUNT_PLATFORM_MISMATCH")
	}
	var identityExists int
	if err := queryer.QueryRow(`SELECT COUNT(*) FROM ego_identities WHERE id=?`, identityID).Scan(&identityExists); err != nil {
		return fmt.Errorf("validate shop listing identity: %w", err)
	}
	if identityExists != 1 {
		return errors.New("SHOP_LISTING_IDENTITY_NOT_FOUND")
	}
	var platformExists int
	if err := queryer.QueryRow(`SELECT COUNT(*) FROM platforms WHERE code=?`, platformCode).Scan(&platformExists); err != nil {
		return fmt.Errorf("validate shop listing platform: %w", err)
	}
	if platformExists != 1 {
		return errors.New("SHOP_LISTING_PLATFORM_NOT_FOUND")
	}
	return nil
}

func normalizeItem(item ItemInput) (ItemInput, error) {
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.Vendor = strings.TrimSpace(item.Vendor)
	item.Brand = strings.TrimSpace(item.Brand)
	item.Category = strings.TrimSpace(item.Category)
	if err := requiredText(item.Title, maxTitleRunes, "title"); err != nil {
		return ItemInput{}, err
	}
	if !utf8.ValidString(item.Description) || utf8.RuneCountInString(item.Description) > maxDescriptionRunes {
		return ItemInput{}, errors.New("description is invalid or too long")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"vendor", item.Vendor, maxVendorRunes}, {"brand", item.Brand, maxBrandRunes}, {"category", item.Category, maxCategoryRunes},
	} {
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit {
			return ItemInput{}, fmt.Errorf("%s is too long or invalid", field.name)
		}
	}
	if !currencyPattern.MatchString(item.Currency) {
		return ItemInput{}, errors.New("currency must be three uppercase letters")
	}
	if err := validNonNegativeBound(item.PriceMinor, maxPriceMinor, "price_minor"); err != nil {
		return ItemInput{}, err
	}
	if item.CompareAtMinor != nil {
		if err := validNonNegativeBound(*item.CompareAtMinor, maxPriceMinor, "compare_at_minor"); err != nil {
			return ItemInput{}, err
		}
		if *item.CompareAtMinor < item.PriceMinor {
			return ItemInput{}, errors.New("compare_at_minor cannot be below price_minor")
		}
	}
	if err := validNonNegativeBound(item.Inventory, maxInventory, "inventory"); err != nil {
		return ItemInput{}, err
	}
	if err := validNonNegativeBound(item.WeightGrams, maxWeightGrams, "weight_grams"); err != nil {
		return ItemInput{}, err
	}
	if len(item.Media) > maxMedia {
		return ItemInput{}, errors.New("too many media files")
	}
	for i := range item.Media {
		media, err := normalizeMedia(item.Media[i])
		if err != nil {
			return ItemInput{}, fmt.Errorf("media %d: %w", i, err)
		}
		item.Media[i] = media
	}
	if len(item.Variants) > maxVariants {
		return ItemInput{}, errors.New("too many variants")
	}
	seenSKU := make(map[string]struct{}, len(item.Variants))
	for i := range item.Variants {
		variant, err := normalizeVariant(item.Variants[i])
		if err != nil {
			return ItemInput{}, fmt.Errorf("variant %d: %w", i, err)
		}
		key := strings.ToUpper(variant.SKU)
		if _, exists := seenSKU[key]; exists {
			return ItemInput{}, errors.New("variant SKU must be unique")
		}
		seenSKU[key] = struct{}{}
		item.Variants[i] = variant
	}
	return item, nil
}

func normalizeMedia(media MediaInput) (MediaInput, error) {
	media.FilePath = filepath.Clean(strings.TrimSpace(media.FilePath))
	media.SHA256 = strings.ToLower(strings.TrimSpace(media.SHA256))
	media.Role = strings.TrimSpace(media.Role)
	if !filepath.IsAbs(media.FilePath) {
		return MediaInput{}, errors.New("file_path must be absolute")
	}
	if !sha256Pattern.MatchString(media.SHA256) {
		return MediaInput{}, errors.New("sha256 must be 64 hexadecimal characters")
	}
	if media.Role == "" || !utf8.ValidString(media.Role) || utf8.RuneCountInString(media.Role) > 64 {
		return MediaInput{}, errors.New("role is invalid")
	}
	info, err := os.Stat(media.FilePath)
	if err != nil {
		return MediaInput{}, fmt.Errorf("file_path is not readable: %w", err)
	}
	if info.IsDir() {
		return MediaInput{}, errors.New("file_path must reference a file")
	}
	file, err := os.Open(media.FilePath)
	if err != nil {
		return MediaInput{}, fmt.Errorf("open media: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return MediaInput{}, fmt.Errorf("hash media: %w", err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != media.SHA256 {
		return MediaInput{}, errors.New("media sha256 does not match file")
	}
	return media, nil
}

func normalizeVariant(variant VariantInput) (VariantInput, error) {
	variant.SKU = strings.TrimSpace(variant.SKU)
	variant.Title = strings.TrimSpace(variant.Title)
	if err := requiredText(variant.SKU, maxSKUrun, "sku"); err != nil {
		return VariantInput{}, err
	}
	if !utf8.ValidString(variant.Title) || utf8.RuneCountInString(variant.Title) > maxVariantTitleRunes {
		return VariantInput{}, errors.New("variant title is too long or invalid")
	}
	if err := validNonNegativeBound(variant.PriceMinor, maxPriceMinor, "variant price_minor"); err != nil {
		return VariantInput{}, err
	}
	if err := validNonNegativeBound(variant.Inventory, maxInventory, "variant inventory"); err != nil {
		return VariantInput{}, err
	}
	if len(variant.Options) > maxOptions {
		return VariantInput{}, errors.New("too many variant options")
	}
	if len(variant.Options) > 0 {
		normalized := make(map[string]string, len(variant.Options))
		for key, value := range variant.Options {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || !utf8.ValidString(key) || utf8.RuneCountInString(key) > 64 || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 200 {
				return VariantInput{}, errors.New("variant option is invalid")
			}
			normalized[key] = value
		}
		variant.Options = normalized
	}
	return variant, nil
}

func requiredText(value string, max int, field string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return fmt.Errorf("%s is required or too long", field)
	}
	return nil
}

func validNonNegativeBound(value, max int64, field string) error {
	if value < 0 || value > max {
		return fmt.Errorf("%s is out of bounds", field)
	}
	return nil
}
