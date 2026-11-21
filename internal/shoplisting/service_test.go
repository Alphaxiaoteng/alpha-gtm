package shoplisting

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

var shopListingPlatformCodes = []string{
	"douyin_shop",
	"taobao",
	"tiktok_shop",
	"shopee",
	"shopify",
	"amazon_seller",
	"jd_shop",
	"pinduoduo_shop",
	"xhs_shop",
	"kuaishou_shop",
	"wechat_shop",
	"alibaba_1688",
	"aliexpress",
}

func TestShopListingFreezeReplayAndRecoveryForAllPlatforms(t *testing.T) {
	for _, platformCode := range shopListingPlatformCodes {
		t.Run(platformCode, func(t *testing.T) {
			database, dataDir := openShopListingDB(t)
			identity, err := database.GetEgoIdentity(1)
			if err != nil {
				database.Close()
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, platformCode, platformCode+" 店铺", "")
			if err != nil {
				database.Close()
				t.Fatal(err)
			}
			if account.IdentityID != identity.ID || account.PlatformCode != platformCode {
				database.Close()
				t.Fatalf("account=%+v", account)
			}

			input := validShopListingInputForPlatform(identity.ID, account.ID, platformCode, "shop-freeze-"+platformCode)
			service := &Service{DB: database}
			dryRun, err := service.DryRun(input)
			if err != nil {
				database.Close()
				t.Fatal(err)
			}
			if dryRun.Snapshot.PlatformCode != platformCode || dryRun.Snapshot.Concurrency != 1 {
				database.Close()
				t.Fatalf("dry-run snapshot=%+v", dryRun.Snapshot)
			}

			plan, created, err := service.Freeze(input)
			if err != nil {
				database.Close()
				t.Fatal(err)
			}
			if !created || plan.PlatformCode != platformCode || plan.Concurrency != 1 || plan.Status != StatusDryRunOK || len(plan.Items) != 1 {
				database.Close()
				t.Fatalf("created=%v plan=%+v", created, plan)
			}
			if plan.SnapshotHash != dryRun.SnapshotHash || plan.SnapshotJSON != dryRun.SnapshotJSON {
				database.Close()
				t.Fatal("freeze snapshot differs from dry-run snapshot")
			}

			replayed, replayCreated, err := service.Freeze(input)
			if err != nil {
				database.Close()
				t.Fatal(err)
			}
			if replayCreated || replayed.ID != plan.ID || replayed.SnapshotHash != plan.SnapshotHash {
				database.Close()
				t.Fatalf("replay created=%v replay=%+v first=%+v", replayCreated, replayed, plan)
			}
			var planCount int
			if err := database.QueryRow(`SELECT COUNT(*) FROM shop_listing_plans WHERE idempotency_key=?`, input.IdempotencyKey).Scan(&planCount); err != nil {
				database.Close()
				t.Fatal(err)
			}
			if planCount != 1 {
				database.Close()
				t.Fatalf("idempotent plan count=%d", planCount)
			}

			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := db.Open(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if err := reopened.Migrate(); err != nil {
				t.Fatal(err)
			}
			recovered, err := (&Service{DB: reopened}).Get(plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.IdentityID != identity.ID || recovered.AccountID != account.ID || recovered.PlatformCode != platformCode ||
				recovered.Concurrency != 1 || recovered.Status != StatusDryRunOK || recovered.SnapshotHash != plan.SnapshotHash || len(recovered.Items) != 1 {
				t.Fatalf("recovered=%+v", recovered)
			}
		})
	}
}

func TestFreezeDryRunPersistsCanonicalSnapshotAndItemHashes(t *testing.T) {
	database, dataDir := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath, mediaHash := shopListingMedia(t, "shop image")
	compareAt := int64(1599)
	input := FreezeInput{
		IdentityID: 1, AccountID: account.ID, PlatformCode: "shopify", IdempotencyKey: "shop-freeze-001",
		Items: []ItemInput{{
			Title: "Alpha 杯", Description: "本地 freeze 测试商品", Vendor: "Alpha", Brand: "Alpha Brand", Category: "家居",
			Currency: "CNY", PriceMinor: 1299, CompareAtMinor: &compareAt, Inventory: 8, WeightGrams: 320,
			Media:    []MediaInput{{FilePath: mediaPath, SHA256: mediaHash, Role: "主图"}},
			Variants: []VariantInput{{SKU: "ALPHA-BLACK", Title: "黑色", PriceMinor: 1299, Inventory: 8, Options: map[string]string{"颜色": "黑"}}},
		}, {
			Title: "Alpha 杯补充装", Description: "第二个商品 item", Category: "家居", Currency: "CNY",
			PriceMinor: 499, Inventory: 10, WeightGrams: 100,
		}},
	}

	run, err := (&Service{DB: database}).DryRun(input)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := database.QueryRow(`SELECT COUNT(*) FROM shop_listing_plans`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatalf("dry-run wrote %d plans", before)
	}
	canonical, err := json.Marshal(run.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if run.SnapshotJSON != string(canonical) {
		t.Fatalf("snapshot is not canonical JSON: %s", run.SnapshotJSON)
	}
	digest := sha256.Sum256(canonical)
	if run.SnapshotHash != hex.EncodeToString(digest[:]) {
		t.Fatalf("snapshot hash=%s", run.SnapshotHash)
	}

	service := &Service{DB: database, Now: func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }}
	plan, created, err := service.Freeze(input)
	if err != nil {
		t.Fatal(err)
	}
	if !created || plan.Status != StatusDryRunOK || plan.Concurrency != 1 || len(plan.Items) != 2 {
		t.Fatalf("created=%v plan=%+v", created, plan)
	}
	if plan.SnapshotHash != run.SnapshotHash || plan.SnapshotJSON != run.SnapshotJSON {
		t.Fatal("persisted snapshot differs from dry-run snapshot")
	}
	for _, item := range plan.Items {
		itemDigest := sha256.Sum256([]byte(item.PayloadJSON))
		if item.PayloadHash != hex.EncodeToString(itemDigest[:]) {
			t.Fatalf("item %d hash mismatch", item.ID)
		}
	}
	var itemCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM shop_listing_items WHERE plan_id=?`, plan.ID).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if itemCount != 2 {
		t.Fatalf("item count=%d", itemCount)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(); err != nil {
		t.Fatal(err)
	}
	recovered, err := (&Service{DB: reopened}).Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.SnapshotHash != plan.SnapshotHash || len(recovered.Items) != 2 {
		t.Fatalf("recovered=%+v", recovered)
	}
}

func TestFreezeIsIdempotentAndConflictsOnChangedSnapshot(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	input := validShopListingInput(account.ID)
	service := &Service{DB: database}
	first, created, err := service.Freeze(input)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first freeze was not created")
	}
	replay, created, err := service.Freeze(input)
	if err != nil {
		t.Fatal(err)
	}
	if created || replay.ID != first.ID || replay.SnapshotHash != first.SnapshotHash {
		t.Fatalf("replay created=%v replay=%+v first=%+v", created, replay, first)
	}

	changed := input
	changed.Items = append([]ItemInput(nil), input.Items...)
	changed.Items[0].Title = "changed"
	if _, _, err := service.Freeze(changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed snapshot error=%v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM shop_listing_plans WHERE idempotency_key=?`, input.IdempotencyKey).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("idempotent plan count=%d", count)
	}
}

func TestFreezeRejectsTargetMismatchAndConcurrencyOutsideOne(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	otherIdentity, err := database.CreateEgoIdentity("浏览器2")
	if err != nil {
		t.Fatal(err)
	}
	input := validShopListingInput(account.ID)
	input.IdentityID = otherIdentity.ID
	if _, err := (&Service{DB: database}).DryRun(input); err == nil || !strings.Contains(err.Error(), "IDENTITY_MISMATCH") {
		t.Fatalf("identity mismatch error=%v", err)
	}

	input = validShopListingInput(account.ID)
	input.PlatformCode = "taobao"
	if _, err := (&Service{DB: database}).DryRun(input); err == nil || !strings.Contains(err.Error(), "PLATFORM_MISMATCH") {
		t.Fatalf("platform mismatch error=%v", err)
	}

	input = validShopListingInput(account.ID)
	input.Concurrency = 2
	if _, err := (&Service{DB: database}).DryRun(input); err == nil || !strings.Contains(err.Error(), "CONCURRENCY_INVALID") {
		t.Fatalf("concurrency error=%v", err)
	}

	_, err = database.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (1, ?, 'shopify', '{}', ?, 'db-concurrency-001', 'dry_run_ok', 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, account.ID, strings.Repeat("a", 64))
	if err == nil {
		t.Fatal("database accepted concurrency=2")
	}
}

func TestShopListingRejectsQianchuan(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	input := validShopListingInput(account.ID)
	input.PlatformCode = "qianchuan"
	if _, err := (&Service{DB: database}).DryRun(input); err == nil || err.Error() != "SHOP_LISTING_PLATFORM_UNSUPPORTED" {
		t.Fatalf("qianchuan error=%v", err)
	}
}

func TestFreezeRejectsInvalidPriceSKUAndMediaDigest(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database}
	mediaPath, mediaHash := shopListingMedia(t, "validation image")
	withMedia := func() FreezeInput {
		input := validShopListingInput(account.ID)
		input.Items[0].Media = []MediaInput{{FilePath: mediaPath, SHA256: mediaHash, Role: "主图"}}
		return input
	}

	badPrice := withMedia()
	badPrice.Items[0].PriceMinor = -1
	if _, err := service.DryRun(badPrice); err == nil || !strings.Contains(err.Error(), "price_minor") {
		t.Fatalf("bad price error=%v", err)
	}

	duplicateSKU := withMedia()
	duplicateSKU.Items[0].Variants = append(duplicateSKU.Items[0].Variants, duplicateSKU.Items[0].Variants[0])
	if _, err := service.DryRun(duplicateSKU); err == nil || !strings.Contains(err.Error(), "SKU") {
		t.Fatalf("duplicate SKU error=%v", err)
	}

	relativeMedia := withMedia()
	relativeMedia.Items[0].Media[0].FilePath = "relative.jpg"
	if _, err := service.DryRun(relativeMedia); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative media error=%v", err)
	}

	badHash := withMedia()
	badHash.Items[0].Media[0].SHA256 = "not-a-sha256"
	if _, err := service.DryRun(badHash); err == nil || !strings.Contains(err.Error(), "64 hexadecimal") {
		t.Fatalf("bad hash format error=%v", err)
	}

	mismatch := withMedia()
	mismatch.Items[0].Media[0].SHA256 = strings.Repeat("0", 64)
	if _, err := service.DryRun(mismatch); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("bad hash digest error=%v", err)
	}
}

func TestShopListingSchemaHasIndependentTablesAndForeignKeys(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	for _, table := range []string{"shop_listing_plans", "shop_listing_items"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("missing %s", table)
		}
	}
	rows, err := database.Query(`PRAGMA foreign_key_list(shop_listing_plans)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seenIdentity, seenAccount := false, false
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatal(err)
		}
		if table == "ego_identities" && from == "identity_id" {
			seenIdentity = true
		}
		if table == "accounts" && from == "account_id" {
			seenAccount = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !seenIdentity || !seenAccount {
		t.Fatalf("plan foreign keys identity=%v account=%v", seenIdentity, seenAccount)
	}
}

func TestShopListingListSummariesRecoversNewestPlansAfterDatabaseReopen(t *testing.T) {
	database, dataDir := openShopListingDB(t)
	firstAccount, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	secondAccount, err := database.CreateAccountForIdentity(1, "taobao", "淘宝店铺", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	service := &Service{DB: database}
	first, _, err := service.Freeze(validShopListingInputForPlatform(1, firstAccount.ID, "shopify", "list-recovery-001"))
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	second, _, err := service.Freeze(validShopListingInputForPlatform(1, secondAccount.ID, "taobao", "list-recovery-002"))
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(); err != nil {
		t.Fatal(err)
	}

	summaries, err := (&Service{DB: reopened}).ListSummaries(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].PlanID != second.ID || summaries[1].PlanID != first.ID {
		t.Fatalf("summaries=%+v", summaries)
	}
	if summaries[0].PlatformCode != "taobao" || summaries[0].AccountID != secondAccount.ID ||
		summaries[0].ItemCount != 1 || summaries[0].Concurrency != 1 || summaries[0].Status != StatusDryRunOK {
		t.Fatalf("newest summary=%+v", summaries[0])
	}
	if summaries[1].PlatformCode != "shopify" || summaries[1].AccountID != firstAccount.ID || summaries[1].ItemCount != 1 {
		t.Fatalf("oldest summary=%+v", summaries[1])
	}
	encoded, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"idempotency_key", "snapshot_json", "payload_json", "测试商品", "描述"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, encoded)
		}
	}
	limited, err := (&Service{DB: reopened}).ListSummaries(1)
	if err != nil || len(limited) != 1 || limited[0].PlanID != second.ID {
		t.Fatalf("limited=%+v err=%v", limited, err)
	}
}

func TestPrepareApprovalBindsAndConsumesOnce(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	service := &Service{DB: database, Now: func() time.Time { return now }}
	input := validShopListingInput(account.ID)
	input.IdempotencyKey = "shop-approval-once-001"
	plan, _, err := service.Freeze(input)
	if err != nil {
		t.Fatal(err)
	}

	grant, err := service.ApprovePrepare(plan.ID, plan.SnapshotHash, "session-approval-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	expectedExpiry := now.Add(defaultApprovalTTL).Format(time.RFC3339Nano)
	if grant.ID == "" || grant.Nonce == "" || grant.ExpiresAt != expectedExpiry {
		t.Fatalf("grant=%+v", grant)
	}
	var sessionID, toolName, targetResource, argsHash, nonceHash, expiresAt string
	var singleUse int
	var consumedAt sql.NullString
	if err := database.QueryRow(`SELECT session_id, tool_name, target_resource, args_hash, nonce_hash, expires_at, single_use, consumed_at
		FROM approvals WHERE id=?`, grant.ID).Scan(&sessionID, &toolName, &targetResource, &argsHash, &nonceHash, &expiresAt, &singleUse, &consumedAt); err != nil {
		t.Fatal(err)
	}
	nonceDigest := sha256.Sum256([]byte(grant.Nonce))
	if sessionID != "session-approval-1" || toolName != prepareApprovalToolName ||
		targetResource != "shop_listing_plan:"+strconv.FormatInt(plan.ID, 10) || argsHash == "" ||
		nonceHash != hex.EncodeToString(nonceDigest[:]) || expiresAt != expectedExpiry || singleUse != 1 || consumedAt.Valid {
		t.Fatalf("approval row session=%q tool=%q target=%q args=%q nonce=%q expires=%q single=%d consumed=%v", sessionID, toolName, targetResource, argsHash, nonceHash, expiresAt, singleUse, consumedAt)
	}

	approved, err := service.ConsumePrepareApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != StatusPrepareApproved || approved.ErrorCode != "" || len(approved.Items) != 1 || approved.Items[0].Status != itemStatusPending {
		t.Fatalf("approved=%+v", approved)
	}
	if _, err := service.ConsumePrepareApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce); err == nil {
		t.Fatal("replayed prepare approval was accepted")
	}
	var consumedCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM approvals WHERE id=? AND consumed_at IS NOT NULL`, grant.ID).Scan(&consumedCount); err != nil {
		t.Fatal(err)
	}
	if consumedCount != 1 {
		t.Fatalf("consumed approvals=%d", consumedCount)
	}
}

func TestPrepareApprovalRejectsWrongBindingAndExpiryWithoutConsuming(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	service := &Service{DB: database, Now: func() time.Time { return now }}
	input := validShopListingInput(account.ID)
	input.IdempotencyKey = "shop-approval-binding-001"
	plan, _, err := service.Freeze(input)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.ApprovePrepare(plan.ID, plan.SnapshotHash, "session-binding-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	otherInput := validShopListingInput(account.ID)
	otherInput.IdempotencyKey = "shop-approval-binding-002"
	otherPlan, _, err := service.Freeze(otherInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumePrepareApproval(otherPlan.ID, otherPlan.SnapshotHash, grant.ID, grant.Nonce); err == nil {
		t.Fatal("approval was accepted for another plan")
	}
	if _, err := service.ConsumePrepareApproval(plan.ID, strings.Repeat("0", 64), grant.ID, grant.Nonce); err == nil {
		t.Fatal("approval was accepted for another snapshot hash")
	}
	if _, err := service.ConsumePrepareApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce+"wrong"); err == nil {
		t.Fatal("approval was accepted with another nonce")
	}
	var consumedAt sql.NullString
	if err := database.QueryRow(`SELECT consumed_at FROM approvals WHERE id=?`, grant.ID).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt.Valid {
		t.Fatal("invalid approval attempt consumed the grant")
	}

	now = now.Add(time.Minute)
	if _, err := service.ConsumePrepareApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce); err == nil {
		t.Fatal("expired approval was accepted")
	}
	if err := database.QueryRow(`SELECT consumed_at FROM approvals WHERE id=?`, grant.ID).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt.Valid {
		t.Fatal("expired approval was consumed")
	}
}

func TestManualTakeoverTransitionsPlanItemsAndSurvivesReopen(t *testing.T) {
	database, dataDir := openShopListingDB(t)
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	service := &Service{DB: database}
	input := validShopListingInput(account.ID)
	input.IdempotencyKey = "shop-manual-takeover-001"
	plan, _, err := service.Freeze(input)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	updated, err := service.MarkManualTakeover(plan.ID, plan.SnapshotHash, "SHOPIFY_PREPARE_UNREVIEWED")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if updated.Status != StatusManualTakeover || updated.ErrorCode != "SHOPIFY_PREPARE_UNREVIEWED" ||
		len(updated.Items) != 1 || updated.Items[0].Status != itemStatusManualTakeover || updated.Items[0].ErrorCode != updated.ErrorCode {
		database.Close()
		t.Fatalf("manual takeover=%+v", updated)
	}
	if _, err := service.MarkManualTakeover(plan.ID, plan.SnapshotHash, "lowercase"); err == nil {
		database.Close()
		t.Fatal("invalid error code was accepted")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(); err != nil {
		t.Fatal(err)
	}
	recovered, err := (&Service{DB: reopened}).Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusManualTakeover || recovered.ErrorCode != "SHOPIFY_PREPARE_UNREVIEWED" ||
		recovered.Items[0].Status != itemStatusManualTakeover || recovered.Items[0].ErrorCode != recovered.ErrorCode {
		t.Fatalf("recovered=%+v", recovered)
	}
}

func TestShopListingStatusAndErrorCodeChecks(t *testing.T) {
	database, _ := openShopListingDB(t)
	defer database.Close()
	account, err := database.CreateAccountForIdentity(1, "shopify", "Shopify 店铺", "")
	if err != nil {
		t.Fatal(err)
	}
	validPlanValues := func(status, errorCode, key string) error {
		_, err := database.Exec(`INSERT INTO shop_listing_plans
			(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, error_code, created_at, frozen_at, updated_at)
			VALUES (1, ?, 'shopify', '{}', ?, ?, ?, 1, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			account.ID, strings.Repeat("a", 64), key, status, errorCode)
		return err
	}
	for _, status := range []string{"dry_run_ok", "prepare_approved", "manual_takeover"} {
		if err := validPlanValues(status, "", "shop-check-"+status); err != nil {
			t.Fatalf("status %s rejected: %v", status, err)
		}
	}
	if err := validPlanValues("listed", "", "shop-check-listed"); err == nil {
		t.Fatal("database accepted unknown plan status")
	}
	if err := validPlanValues("dry_run_ok", "not-valid", "shop-check-error"); err == nil {
		t.Fatal("database accepted invalid error code")
	}
	plan, _, err := (&Service{DB: database}).Freeze(FreezeInput{
		IdentityID: 1, AccountID: account.ID, PlatformCode: "shopify", IdempotencyKey: "shop-check-items-001",
		Items: []ItemInput{{Title: "商品", Description: "描述", Category: "类目", Currency: "CNY", PriceMinor: 1, Inventory: 1, WeightGrams: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_items
		(plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at)
		VALUES (?, 99, '{}', ?, 'listed', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, plan.ID, strings.Repeat("a", 64)); err == nil {
		t.Fatal("database accepted unknown item status")
	}
}

func openShopListingDB(t *testing.T) (*db.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	return database, dataDir
}

func validShopListingInput(accountID int64) FreezeInput {
	return validShopListingInputForPlatform(1, accountID, "shopify", "shop-valid-001")
}

func validShopListingInputForPlatform(identityID, accountID int64, platformCode, idempotencyKey string) FreezeInput {
	return FreezeInput{
		IdentityID: identityID, AccountID: accountID, PlatformCode: platformCode, IdempotencyKey: idempotencyKey,
		Items: []ItemInput{{
			Title: "测试商品", Description: "描述", Category: "测试", Currency: "CNY", PriceMinor: 100,
			Inventory: 2, WeightGrams: 10, Variants: []VariantInput{{SKU: "SKU-001", PriceMinor: 100, Inventory: 2}},
		}},
	}
}

func shopListingMedia(t *testing.T, content string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "media.bin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(content))
	return path, hex.EncodeToString(digest[:])
}
