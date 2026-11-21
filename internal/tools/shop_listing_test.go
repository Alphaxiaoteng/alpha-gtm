package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestShopListingCatalogRegistersOnlyLocalPhase0Tools(t *testing.T) {
	RegisterCatalog()
	for _, want := range []struct {
		name       string
		sideEffect string
		risk       AgentRisk
	}{
		{"shop.listing.dry-run", "read", AgentRiskL1},
		{"shop.listing.freeze", "write", AgentRiskL2},
		{"shop.listing.get", "read", AgentRiskL0},
		{"shop.listing.list", "read", AgentRiskL0},
		{"shop.listing.prepare", "browser", AgentRiskL3},
	} {
		definition, ok := Default.Get(want.name)
		if !ok {
			t.Fatalf("missing %s", want.name)
		}
		if definition.SideEffect != want.sideEffect || !definition.AgentVisible || definition.AgentRisk != want.risk {
			t.Fatalf("definition=%+v", definition)
		}
		if len(definition.Params) != 1 || definition.Params[0].Name != "input" || !definition.Params[0].Required || definition.Params[0].Type != "object" {
			t.Fatalf("input schema=%+v", definition.Params)
		}
	}
	for _, name := range []string{
		"shop.listing.submit", "shop.listing.submit-approved", "shop.listing.verify",
	} {
		if _, ok := Default.Get(name); ok {
			t.Fatalf("placeholder must not be registered: %s", name)
		}
	}
}

func TestShopListingPrepareRequiresTrustedLocalUI(t *testing.T) {
	args := map[string]interface{}{"input": map[string]interface{}{
		"plan_id": int64(17), "snapshot_hash": "snapshot-hash", "approval_id": "approval-id", "nonce": "nonce",
	}}
	calls := 0
	runtime := &Runtime{PrepareShopListing: func(context.Context, int64, string, string, string) (*Result, error) {
		calls++
		return okResult("prepared", nil), nil
	}}
	result, err := Default.Call(context.Background(), runtime, "shop.listing.prepare", args)
	if err == nil || result == nil || result.Error != "TRUSTED_LOCAL_UI_REQUIRED" || calls != 0 {
		t.Fatalf("untrusted result=%+v err=%v calls=%d", result, err, calls)
	}

	runtime.TrustedLocalUI = true
	result, err = Default.Call(context.Background(), runtime, "shop.listing.prepare", args)
	if err != nil || result == nil || !result.OK || calls != 1 {
		t.Fatalf("trusted result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestShopListingPrepareForwardsTrustedCallbackExactlyOnce(t *testing.T) {
	ctxKey := struct{}{}
	wantCtx := context.WithValue(context.Background(), ctxKey, "shop-listing")
	wantPlanID := int64(42)
	wantHash, wantApprovalID, wantNonce := "hash-42", "approval-42", "nonce-42"
	calls := 0
	runtime := &Runtime{
		TrustedLocalUI: true,
		PrepareShopListing: func(ctx context.Context, planID int64, snapshotHash, approvalID, nonce string) (*Result, error) {
			calls++
			if ctx.Value(ctxKey) != "shop-listing" || planID != wantPlanID || snapshotHash != wantHash || approvalID != wantApprovalID || nonce != wantNonce {
				t.Fatalf("callback args ctx=%v plan=%d hash=%q approval=%q nonce=%q", ctx, planID, snapshotHash, approvalID, nonce)
			}
			return okResult("prepared", map[string]interface{}{"plan_id": planID}), nil
		},
	}
	result, err := Default.Call(wantCtx, runtime, "shop.listing.prepare", map[string]interface{}{"input": map[string]interface{}{
		"plan_id": wantPlanID, "snapshot_hash": wantHash, "approval_id": wantApprovalID, "nonce": wantNonce,
	}})
	if err != nil || result == nil || !result.OK || calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestShopListingPrepareRejectsUnknownAndMissingInput(t *testing.T) {
	runtime := &Runtime{
		TrustedLocalUI: true,
		PrepareShopListing: func(context.Context, int64, string, string, string) (*Result, error) {
			t.Fatal("prepare callback must not run for invalid input")
			return nil, nil
		},
	}
	cases := []map[string]interface{}{
		{"plan_id": int64(17), "snapshot_hash": "snapshot-hash", "approval_id": "approval-id", "nonce": "nonce", "unknown": true},
		{"plan_id": int64(17), "snapshot_hash": "snapshot-hash", "approval_id": "approval-id"},
		{"plan_id": int64(17), "snapshot_hash": "snapshot-hash", "nonce": "nonce"},
		{"plan_id": int64(17), "approval_id": "approval-id", "nonce": "nonce"},
		{"snapshot_hash": "snapshot-hash", "approval_id": "approval-id", "nonce": "nonce"},
	}
	for _, input := range cases {
		result, err := Default.Call(context.Background(), runtime, "shop.listing.prepare", map[string]interface{}{"input": input})
		if err == nil || result == nil || result.Error != "SHOP_LISTING_PREPARE_INPUT_INVALID" {
			t.Fatalf("input=%+v result=%+v err=%v", input, result, err)
		}
	}
}

func TestWithTrustedLocalUICopiesPrepareShopListing(t *testing.T) {
	calls := 0
	runtime := &Runtime{PrepareShopListing: func(context.Context, int64, string, string, string) (*Result, error) {
		calls++
		return okResult("prepared", nil), nil
	}}
	trusted := runtime.WithTrustedLocalUI(true)
	if trusted == nil || !trusted.TrustedLocalUI || trusted.PrepareShopListing == nil {
		t.Fatalf("trusted runtime did not copy prepare callback: %+v", trusted)
	}
	if _, err := trusted.PrepareShopListing(context.Background(), 1, "hash", "approval", "nonce"); err != nil || calls != 1 {
		t.Fatalf("copied callback err=%v calls=%d", err, calls)
	}
}

func TestShopListingToolsRunAgainstTemporaryDatabaseAndRedactResults(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(1, "shopify", "测试店铺", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "product-image.bin")
	mediaBytes := []byte("shop listing test media")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		database.Close()
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	mediaHash := hex.EncodeToString(digest[:])
	input := shopListingToolInput(account.ID, mediaPath, mediaHash)
	runtime := &Runtime{DataDir: dataDir}

	dryRun, err := Default.Call(context.Background(), runtime, "shop.listing.dry-run", map[string]interface{}{"input": input})
	if err != nil || dryRun == nil || !dryRun.OK {
		database.Close()
		t.Fatalf("dry-run result=%+v err=%v", dryRun, err)
	}
	assertShopListingResultIsRedacted(t, dryRun, mediaPath)
	var planCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM shop_listing_plans`).Scan(&planCount); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if planCount != 0 {
		database.Close()
		t.Fatalf("dry-run created %d plans", planCount)
	}
	if dryRun.Data["snapshot_hash"] == nil || dryRun.Data["snapshot"] == nil || dryRun.Data["item_count"] != 1 {
		database.Close()
		t.Fatalf("dry-run summary=%+v", dryRun.Data)
	}

	first, err := Default.Call(context.Background(), runtime, "shop.listing.freeze", map[string]interface{}{"input": input})
	if err != nil || first == nil || !first.OK {
		database.Close()
		t.Fatalf("freeze result=%+v err=%v", first, err)
	}
	assertShopListingResultIsRedacted(t, first, mediaPath)
	planID := shopListingResultInt64(t, first.Data["plan_id"])
	if first.Data["created"] != true || first.Data["replayed"] != false || first.Data["status"] == "" {
		database.Close()
		t.Fatalf("first freeze summary=%+v", first.Data)
	}

	replay, err := Default.Call(context.Background(), runtime, "shop.listing.freeze", map[string]interface{}{"input": input})
	if err != nil || replay == nil || !replay.OK {
		database.Close()
		t.Fatalf("replay result=%+v err=%v", replay, err)
	}
	if replay.Data["created"] != false || replay.Data["replayed"] != true || shopListingResultInt64(t, replay.Data["plan_id"]) != planID {
		database.Close()
		t.Fatalf("replay summary=%+v", replay.Data)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM shop_listing_plans`).Scan(&planCount); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if planCount != 1 {
		database.Close()
		t.Fatalf("replay created extra plan: %d", planCount)
	}

	got, err := Default.Call(context.Background(), runtime, "shop.listing.get", map[string]interface{}{"input": map[string]interface{}{"plan_id": planID}})
	if err != nil || got == nil || !got.OK {
		database.Close()
		t.Fatalf("get result=%+v err=%v", got, err)
	}
	assertShopListingResultIsRedacted(t, got, mediaPath)
	encoded, _ := json.Marshal(got.Data)
	if !strings.Contains(string(encoded), `"status":"pending"`) {
		database.Close()
		t.Fatalf("get omitted item status: %s", encoded)
	}
	assertShopListingSummaryResultIsRedacted(t, got, mediaPath)
	list, err := Default.Call(context.Background(), runtime, "shop.listing.list", map[string]interface{}{"input": map[string]interface{}{}})
	if err != nil || list == nil || !list.OK {
		database.Close()
		t.Fatalf("list result=%+v err=%v", list, err)
	}
	assertShopListingSummaryResultIsRedacted(t, list, mediaPath)
	if list.Data["count"] != 1 {
		database.Close()
		t.Fatalf("list count=%+v", list.Data)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestShopListingListRejectsInvalidLimitAndUnknownFields(t *testing.T) {
	runtime := &Runtime{DataDir: t.TempDir()}
	cases := []map[string]interface{}{
		{"unknown": true},
		{"limit": nil},
		{"limit": float64(0)},
		{"limit": float64(-1)},
		{"limit": float64(101)},
		{"limit": 1.5},
	}
	for _, input := range cases {
		result, err := Default.Call(context.Background(), runtime, "shop.listing.list", map[string]interface{}{"input": input})
		if err == nil || result == nil || result.Error != "SHOP_LISTING_LIST_INPUT_INVALID" {
			t.Fatalf("input=%+v result=%+v err=%v", input, result, err)
		}
	}
	result, err := Default.Call(context.Background(), runtime, "shop.listing.list", map[string]interface{}{"input": map[string]interface{}{"limit": float64(1)}})
	if err != nil || result == nil || !result.OK || result.Data["count"] != 0 {
		t.Fatalf("valid limit result=%+v err=%v", result, err)
	}
}

func TestShopListingListInitializesEmptyDataDir(t *testing.T) {
	dataDir := t.TempDir()
	result, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir}, "shop.listing.list", map[string]interface{}{"input": map[string]interface{}{}})
	if err != nil || result == nil || !result.OK || result.Data["count"] != 0 {
		t.Fatalf("empty data dir result=%+v err=%v", result, err)
	}
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if ready, err := shopListingReadSchemaReady(database); err != nil || !ready {
		t.Fatalf("empty data dir schema ready=%v err=%v", ready, err)
	}
}

func TestShopListingReadPathUsesReadOnlyDatabaseForReadySchema(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE shop_listing_readonly_fixture (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_readonly_fixture(id, value) VALUES (1, 'before')`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	readonly, failure, err := openShopListingReadOnlyToolDB(&Runtime{DataDir: dataDir})
	if err != nil || failure != nil || readonly == nil {
		t.Fatalf("readonly database=%v failure=%+v err=%v", readonly, failure, err)
	}
	defer readonly.Close()
	var queryOnly int
	if err := readonly.QueryRow(`PRAGMA query_only`).Scan(&queryOnly); err != nil {
		t.Fatal(err)
	}
	if queryOnly != 1 {
		t.Fatalf("query_only=%d, want 1", queryOnly)
	}
	if _, err := readonly.Exec(`UPDATE shop_listing_readonly_fixture SET value='after' WHERE id=1`); err == nil {
		t.Fatal("Shop read path returned a writable database")
	}
}

func TestShopListingReadPathFailsClosedForIncompleteSchema(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`CREATE TABLE shop_listing_plans (
		id INTEGER PRIMARY KEY,
		identity_id INTEGER NOT NULL,
		account_id INTEGER NOT NULL,
		platform_code TEXT NOT NULL CHECK (platform_code IN ('aliexpress')),
		snapshot_json TEXT NOT NULL,
		snapshot_hash TEXT NOT NULL,
		idempotency_key TEXT NOT NULL,
		status TEXT NOT NULL,
		concurrency INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		frozen_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	_, err = database.Exec(`CREATE TABLE shop_listing_items (
		id INTEGER PRIMARY KEY,
		plan_id INTEGER NOT NULL,
		ordinal INTEGER NOT NULL,
		payload_json TEXT NOT NULL,
		payload_hash TEXT NOT NULL,
		status TEXT NOT NULL,
		error_code TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir}, "shop.listing.list", map[string]interface{}{"input": map[string]interface{}{}})
	if err == nil || result == nil || result.Error != "SHOP_LISTING_DB_SCHEMA_INVALID" || err.Error() != "SHOP_LISTING_DB_SCHEMA_INVALID" {
		t.Fatalf("incomplete schema result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Error, dataDir) || strings.Contains(err.Error(), dataDir) {
		t.Fatalf("schema error leaked data directory: result=%+v err=%v", result, err)
	}
}

func TestShopListingToolsRejectUnknownFieldsMissingRuntimeAndKeepStableErrors(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(1, "shopify", "测试店铺", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	input := shopListingToolInput(account.ID, "", "")
	input["unknown_field"] = "reject me"
	result, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir}, "shop.listing.dry-run", map[string]interface{}{"input": input})
	if err == nil || result == nil || result.Error != "SHOP_LISTING_INPUT_INVALID" {
		t.Fatalf("unknown field result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Error, "unknown_field") {
		t.Fatal("stable error leaked decoder detail")
	}

	delete(input, "unknown_field")
	result, err = Default.Call(context.Background(), &Runtime{}, "shop.listing.dry-run", map[string]interface{}{"input": input})
	if err == nil || result == nil || result.Error != "SHOP_LISTING_RUNTIME_REQUIRED" {
		t.Fatalf("missing runtime result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Error, dataDir) {
		t.Fatal("stable error leaked data directory")
	}
}

func shopListingToolInput(accountID int64, mediaPath, mediaHash string) map[string]interface{} {
	item := map[string]interface{}{
		"title": "测试商品", "description": "本地 Registry 测试商品", "category": "测试",
		"currency": "CNY", "price_minor": int64(1299), "inventory": int64(3), "weight_grams": int64(100),
		"variants": []interface{}{map[string]interface{}{"sku": "SKU-001", "price_minor": int64(1299), "inventory": int64(3)}},
	}
	if mediaPath != "" {
		item["media"] = []interface{}{map[string]interface{}{"file_path": mediaPath, "sha256": mediaHash, "role": "main"}}
	}
	return map[string]interface{}{
		"identity_id": int64(1), "account_id": accountID, "platform_code": "shopify",
		"idempotency_key": "registry-shop-listing-001", "items": []interface{}{item},
	}
}

func assertShopListingResultIsRedacted(t *testing.T, result *Result, mediaPath string) {
	t.Helper()
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"file_path", "snapshot_json", "payload_json", mediaPath} {
		if forbidden != "" && strings.Contains(text, forbidden) {
			t.Fatalf("result leaked %q: %s", forbidden, text)
		}
	}
}

func assertShopListingSummaryResultIsRedacted(t *testing.T, result *Result, mediaPath string) {
	t.Helper()
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"idempotency_key", "snapshot_json", "payload_json", "file_path", mediaPath, "测试商品", "本地 Registry 测试商品"} {
		if forbidden != "" && strings.Contains(text, forbidden) {
			t.Fatalf("summary result leaked %q: %s", forbidden, text)
		}
	}
}

func shopListingResultInt64(t *testing.T, value interface{}) int64 {
	t.Helper()
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		t.Fatalf("expected integer result, got %T", value)
		return 0
	}
}
