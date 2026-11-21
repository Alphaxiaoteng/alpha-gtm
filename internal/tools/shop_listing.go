package tools

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/shoplisting"
)

// registerShopListingTools exposes the local Phase 0 contract and a trusted
// browser preparation handoff. Submission and verification are not cataloged.
func registerShopListingTools(registry *Registry) {
	registry.Register(Def{
		Name: "shop.listing.dry-run", Category: "shop", SideEffect: "read",
		Description:  "只读取本地商品素材并计算校验哈希，不写入数据库、不打开浏览器",
		Params:       []Param{{Name: "input", Type: "object", Required: true, Description: "商品冻结输入对象"}},
		AgentVisible: true, AgentWorkspace: "shop", AgentRisk: AgentRiskL1,
		AgentImpact:      "读取明确商品输入和本地素材校验哈希，不持久化、不打开浏览器",
		AgentIdempotency: "snapshot-hash",
		Handler:          handleShopListingDryRun,
	})
	registry.Register(Def{
		Name: "shop.listing.freeze", Category: "shop", SideEffect: "write",
		Description:  "显式持久化经过校验的本地商品快照；不打开浏览器、不上架",
		Params:       []Param{{Name: "input", Type: "object", Required: true, Description: "商品冻结输入对象"}},
		AgentVisible: true, AgentWorkspace: "shop", AgentRisk: AgentRiskL2,
		AgentImpact:      "将商品快照写入本地数据库，保持幂等并发为 1",
		AgentIdempotency: "idempotency-key-and-snapshot",
		Handler:          handleShopListingFreeze,
	})
	registry.Register(Def{
		Name: "shop.listing.get", Category: "shop", SideEffect: "read",
		Description:  "读取本地商品计划摘要和条目状态，不返回商品载荷或本地路径",
		Params:       []Param{{Name: "input", Type: "object", Required: true, Description: "{plan_id}"}},
		AgentVisible: true, AgentWorkspace: "shop", AgentRisk: AgentRiskL0,
		AgentImpact:      "读取本地商品计划摘要和条目状态",
		AgentIdempotency: "read",
		Handler:          handleShopListingGet,
	})
	registry.Register(Def{
		Name: "shop.listing.list", Category: "shop", SideEffect: "read",
		Description:  "列出可恢复的本地商品计划脱敏摘要，不返回商品载荷或审批凭证",
		Params:       []Param{{Name: "input", Type: "object", Required: true, Description: "{} 或 {limit: 1-100}"}},
		AgentVisible: true, AgentWorkspace: "shop", AgentRisk: AgentRiskL0,
		AgentImpact:      "读取本地商品计划的状态、目标和条目数量",
		AgentIdempotency: "read",
		Handler:          handleShopListingList,
	})
	registry.Register(Def{
		Name: "shop.listing.prepare", Category: "shop", SideEffect: "browser",
		Description:  "在受控商品后台准备冻结计划，不执行最终提交",
		Params:       []Param{{Name: "input", Type: "object", Required: true, Description: "{plan_id,snapshot_hash,approval_id,nonce}"}},
		AgentVisible: true, AgentWorkspace: "shop", AgentRisk: AgentRiskL3,
		AgentImpact:      "在受控商品后台准备冻结计划，不执行最终提交",
		AgentIdempotency: "single-use-domain-approval",
		Handler:          handleShopListingPrepare,
	})
}

type shopListingGetInput struct {
	PlanID int64 `json:"plan_id"`
}

type shopListingListInput struct {
	Limit int
}

type shopListingPrepareInput struct {
	PlanID       int64  `json:"plan_id"`
	SnapshotHash string `json:"snapshot_hash"`
	ApprovalID   string `json:"approval_id"`
	Nonce        string `json:"nonce"`
}

func handleShopListingPrepare(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	if runtime == nil || !runtime.TrustedLocalUI || runtime.PrepareShopListing == nil {
		err := errors.New("TRUSTED_LOCAL_UI_REQUIRED")
		return failResult("TRUSTED_LOCAL_UI_REQUIRED", "需要受信任的本地 UI"), err
	}
	input, err := decodeShopListingPrepareInput(args)
	if err != nil {
		err = errors.New("SHOP_LISTING_PREPARE_INPUT_INVALID")
		return failResult("SHOP_LISTING_PREPARE_INPUT_INVALID", "商品准备输入无效"), err
	}
	return runtime.PrepareShopListing(ctx, input.PlanID, input.SnapshotHash, input.ApprovalID, input.Nonce)
}

func handleShopListingDryRun(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeShopListingFreezeInput(args)
	if err != nil {
		return failResult("SHOP_LISTING_INPUT_INVALID", "商品输入无效"), err
	}
	database, failure, err := openShopListingToolDB(runtime)
	if failure != nil {
		return failure, err
	}
	defer database.Close()

	run, err := (&shoplisting.Service{DB: database}).DryRun(input)
	if err != nil {
		return failResult(shopListingErrorCode(err, "SHOP_LISTING_DRY_RUN_FAILED"), "商品本地校验失败"), err
	}
	return okResult("商品本地校验完成", map[string]interface{}{
		"snapshot_hash": run.SnapshotHash,
		"snapshot":      shopListingSnapshotSummary(run.Snapshot),
		"item_count":    len(run.Snapshot.Items),
	}), nil
}

func handleShopListingFreeze(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeShopListingFreezeInput(args)
	if err != nil {
		return failResult("SHOP_LISTING_INPUT_INVALID", "商品输入无效"), err
	}
	database, failure, err := openShopListingToolDB(runtime)
	if failure != nil {
		return failure, err
	}
	defer database.Close()

	plan, created, err := (&shoplisting.Service{DB: database}).Freeze(input)
	if err != nil {
		return failResult(shopListingErrorCode(err, "SHOP_LISTING_FREEZE_FAILED"), "商品快照持久化失败"), err
	}
	return okResult("商品快照已保存", map[string]interface{}{
		"plan_id":       plan.ID,
		"snapshot_hash": plan.SnapshotHash,
		"status":        plan.Status,
		"created":       created,
		"replayed":      !created,
		"item_count":    len(plan.Items),
	}), nil
}

func handleShopListingGet(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeShopListingGetInput(args)
	if err != nil {
		return failResult("SHOP_LISTING_INPUT_INVALID", "计划输入无效"), err
	}
	if input.PlanID <= 0 {
		err := errors.New("SHOP_LISTING_PLAN_ID_REQUIRED")
		return failResult("SHOP_LISTING_PLAN_ID_REQUIRED", "plan_id 必须为正数"), err
	}
	database, failure, err := openShopListingReadOnlyToolDB(runtime)
	if failure != nil {
		return failure, err
	}
	defer database.Close()

	plan, err := (&shoplisting.Service{DB: database}).Get(input.PlanID)
	if err != nil {
		code := shopListingErrorCode(err, "SHOP_LISTING_GET_FAILED")
		return failResult(code, "商品计划读取失败"), errors.New(code)
	}
	items := make([]map[string]interface{}, 0, len(plan.Items))
	for _, item := range plan.Items {
		items = append(items, map[string]interface{}{
			"item_id": item.ID,
			"ordinal": item.Ordinal,
			"status":  item.Status,
		})
	}
	return okResult("商品计划摘要", map[string]interface{}{
		"plan": map[string]interface{}{
			"plan_id":       plan.ID,
			"identity_id":   plan.IdentityID,
			"account_id":    plan.AccountID,
			"platform_code": plan.PlatformCode,
			"snapshot_hash": plan.SnapshotHash,
			"status":        plan.Status,
			"concurrency":   plan.Concurrency,
			"created_at":    plan.CreatedAt,
			"frozen_at":     plan.FrozenAt,
			"updated_at":    plan.UpdatedAt,
			"error_code":    plan.ErrorCode,
		},
		"items":      items,
		"item_count": len(items),
	}), nil
}

func handleShopListingList(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeShopListingListInput(args)
	if err != nil {
		return failResult("SHOP_LISTING_LIST_INPUT_INVALID", "商品计划列表输入无效"), err
	}
	database, failure, err := openShopListingReadOnlyToolDB(runtime)
	if failure != nil {
		return failure, err
	}
	defer database.Close()

	plans, err := (&shoplisting.Service{DB: database}).ListSummaries(input.Limit)
	if err != nil {
		return failResult("SHOP_LISTING_LIST_FAILED", "商品计划列表读取失败"), errors.New("SHOP_LISTING_LIST_FAILED")
	}
	return okResult("商品计划列表", map[string]interface{}{
		"plans": plans,
		"count": len(plans),
	}), nil
}

func decodeShopListingFreezeInput(args map[string]interface{}) (shoplisting.FreezeInput, error) {
	var input shoplisting.FreezeInput
	payload, err := strictShopListingInputPayload(args)
	if err != nil {
		return input, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return input, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return input, errors.New("multiple JSON values")
		}
		return input, err
	}
	return input, nil
}

func decodeShopListingGetInput(args map[string]interface{}) (shopListingGetInput, error) {
	var input shopListingGetInput
	payload, err := strictShopListingInputPayload(args)
	if err != nil {
		return input, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return input, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return input, errors.New("multiple JSON values")
		}
		return input, err
	}
	return input, nil
}

func decodeShopListingListInput(args map[string]interface{}) (shopListingListInput, error) {
	var input shopListingListInput
	payload, err := strictShopListingInputPayload(args)
	if err != nil {
		return input, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return input, err
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return input, errors.New("input must be an object")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return input, errors.New("multiple JSON values")
		}
		return input, err
	}
	for key := range fields {
		if key != "limit" {
			return input, errors.New("unknown list input field")
		}
	}
	input.Limit = 20
	if rawLimit, ok := fields["limit"]; ok {
		if string(rawLimit) == "null" {
			return input, errors.New("limit is required when provided")
		}
		limitDecoder := json.NewDecoder(bytes.NewReader(rawLimit))
		if err := limitDecoder.Decode(&input.Limit); err != nil {
			return input, err
		}
		if err := limitDecoder.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				return input, errors.New("multiple limit values")
			}
			return input, err
		}
	}
	if input.Limit < 1 || input.Limit > 100 {
		return input, errors.New("limit must be between 1 and 100")
	}
	return input, nil
}

func decodeShopListingPrepareInput(args map[string]interface{}) (shopListingPrepareInput, error) {
	var input shopListingPrepareInput
	payload, err := strictShopListingInputPayload(args)
	if err != nil {
		return input, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return input, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return input, errors.New("multiple JSON values")
		}
		return input, err
	}
	if input.PlanID <= 0 || strings.TrimSpace(input.SnapshotHash) == "" ||
		strings.TrimSpace(input.ApprovalID) == "" || strings.TrimSpace(input.Nonce) == "" {
		return input, errors.New("required prepare field is missing")
	}
	return input, nil
}

func strictShopListingInputPayload(args map[string]interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, errors.New("input object is required")
	}
	payload, ok := args["input"]
	if !ok || payload == nil {
		return nil, errors.New("input object is required")
	}
	return payload, nil
}

func openShopListingToolDB(runtime *Runtime) (*db.DB, *Result, error) {
	if runtime == nil || strings.TrimSpace(runtime.DataDir) == "" {
		err := errors.New("SHOP_LISTING_RUNTIME_REQUIRED")
		return nil, failResult("SHOP_LISTING_RUNTIME_REQUIRED", "本地运行时不可用"), err
	}
	database, err := db.Open(runtime.DataDir)
	if err != nil {
		return nil, failResult("SHOP_LISTING_DB_OPEN_FAILED", "本地数据库不可用"), err
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		return nil, failResult("SHOP_LISTING_DB_MIGRATE_FAILED", "本地数据库初始化失败"), err
	}
	return database, nil, nil
}

// openShopListingReadOnlyToolDB opens the database for the redacted Get/List
// surfaces without running the application-wide migration chain when the two
// Shop tables already expose the columns those queries use. A new or partial
// data directory falls back to the normal migration path once, then must pass
// the same schema check before it is handed to the read service.
func openShopListingReadOnlyToolDB(runtime *Runtime) (*db.DB, *Result, error) {
	if runtime == nil || strings.TrimSpace(runtime.DataDir) == "" {
		err := errors.New("SHOP_LISTING_RUNTIME_REQUIRED")
		return nil, failResult("SHOP_LISTING_RUNTIME_REQUIRED", "本地运行时不可用"), err
	}
	_, statErr := os.Stat(filepath.Join(runtime.DataDir, "app.db"))
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, failResult("SHOP_LISTING_DB_OPEN_FAILED", "本地数据库不可用"), errors.New("SHOP_LISTING_DB_OPEN_FAILED")
	}
	if os.IsNotExist(statErr) {
		return openShopListingReadWriteToolDB(runtime)
	}

	database, err := db.OpenReadOnly(runtime.DataDir)
	if err != nil {
		return nil, failResult("SHOP_LISTING_DB_OPEN_FAILED", "本地数据库不可用"), errors.New("SHOP_LISTING_DB_OPEN_FAILED")
	}
	ready, err := shopListingReadSchemaReady(database)
	if err != nil {
		_ = database.Close()
		return nil, failResult("SHOP_LISTING_DB_SCHEMA_CHECK_FAILED", "本地数据库结构无法验证"), errors.New("SHOP_LISTING_DB_SCHEMA_CHECK_FAILED")
	}
	if ready {
		return database, nil, nil
	}
	_ = database.Close()
	return openShopListingReadWriteToolDB(runtime)
}

func openShopListingReadWriteToolDB(runtime *Runtime) (*db.DB, *Result, error) {
	database, err := db.Open(runtime.DataDir)
	if err != nil {
		return nil, failResult("SHOP_LISTING_DB_OPEN_FAILED", "本地数据库不可用"), errors.New("SHOP_LISTING_DB_OPEN_FAILED")
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		return nil, failResult("SHOP_LISTING_DB_MIGRATE_FAILED", "本地数据库初始化失败"), errors.New("SHOP_LISTING_DB_MIGRATE_FAILED")
	}
	ready, err := shopListingReadSchemaReady(database)
	if err != nil {
		_ = database.Close()
		return nil, failResult("SHOP_LISTING_DB_SCHEMA_CHECK_FAILED", "本地数据库结构无法验证"), errors.New("SHOP_LISTING_DB_SCHEMA_CHECK_FAILED")
	}
	if !ready {
		_ = database.Close()
		return nil, failResult("SHOP_LISTING_DB_SCHEMA_INVALID", "本地数据库结构不完整"), errors.New("SHOP_LISTING_DB_SCHEMA_INVALID")
	}
	return database, nil, nil
}

type shopListingReadTableSchema struct {
	name    string
	columns []string
}

var shopListingReadSchema = []shopListingReadTableSchema{
	{
		name: "shop_listing_plans",
		columns: []string{
			"id", "identity_id", "account_id", "platform_code", "idempotency_key",
			"snapshot_json", "snapshot_hash", "status", "concurrency", "created_at",
			"frozen_at", "updated_at", "error_code",
		},
	},
	{
		name: "shop_listing_items",
		columns: []string{
			"id", "plan_id", "ordinal", "payload_json", "payload_hash", "status",
			"error_code", "created_at", "updated_at",
		},
	},
}

func shopListingReadSchemaReady(database *db.DB) (bool, error) {
	if database == nil {
		return false, errors.New("database is required")
	}
	for _, table := range shopListingReadSchema {
		var tableCount int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table.name).Scan(&tableCount); err != nil {
			return false, err
		}
		if tableCount != 1 {
			return false, nil
		}
		columns, err := shopListingTableColumns(database, table.name)
		if err != nil {
			return false, err
		}
		for _, required := range table.columns {
			if _, ok := columns[required]; !ok {
				return false, nil
			}
		}
	}
	return true, nil
}

func shopListingTableColumns(database *db.DB, tableName string) (map[string]struct{}, error) {
	rows, err := database.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		return nil, err
	}
	columns := make(map[string]struct{})
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return nil, err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return columns, nil
}

func shopListingErrorCode(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	if errors.Is(err, shoplisting.ErrIdempotencyConflict) {
		return "SHOP_LISTING_IDEMPOTENCY_CONFLICT"
	}
	message := err.Error()
	if strings.HasPrefix(message, "SHOP_LISTING_") {
		if end := strings.IndexAny(message, ": \t\r\n"); end > 0 {
			return message[:end]
		}
		return message
	}
	if strings.Contains(strings.ToLower(message), "integrity") {
		return "SHOP_LISTING_INTEGRITY_FAILED"
	}
	return fallback
}

func shopListingSnapshotSummary(snapshot shoplisting.Snapshot) map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		itemSummary := map[string]interface{}{
			"title":        item.Title,
			"description":  item.Description,
			"vendor":       item.Vendor,
			"brand":        item.Brand,
			"category":     item.Category,
			"currency":     item.Currency,
			"price_minor":  item.PriceMinor,
			"inventory":    item.Inventory,
			"weight_grams": item.WeightGrams,
		}
		if item.CompareAtMinor != nil {
			itemSummary["compare_at_minor"] = *item.CompareAtMinor
		}
		if len(item.Media) > 0 {
			media := make([]map[string]interface{}, 0, len(item.Media))
			for _, asset := range item.Media {
				media = append(media, map[string]interface{}{"sha256": asset.SHA256, "role": asset.Role})
			}
			itemSummary["media"] = media
		}
		if len(item.Variants) > 0 {
			itemSummary["variants"] = item.Variants
		}
		items = append(items, itemSummary)
	}
	return map[string]interface{}{
		"version":       snapshot.Version,
		"identity_id":   snapshot.IdentityID,
		"account_id":    snapshot.AccountID,
		"platform_code": snapshot.PlatformCode,
		"concurrency":   snapshot.Concurrency,
		"items":         items,
	}
}
