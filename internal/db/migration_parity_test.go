package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestSQLMigrationsCoverRuntimeSchema(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	applySQLMigrations(t, database)

	tablePattern := regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS\s+([a-z_]+)`)
	for _, statement := range migrations {
		match := tablePattern.FindStringSubmatch(statement)
		if len(match) != 2 {
			t.Fatalf("cannot parse runtime table statement: %.80s", statement)
		}
		assertSQLiteObject(t, database, "table", match[1])
	}
	indexPattern := regexp.MustCompile(`(?i)CREATE (?:UNIQUE )?INDEX IF NOT EXISTS\s+([a-z_]+)`)
	for _, statement := range schemaIndexes {
		match := indexPattern.FindStringSubmatch(statement)
		if len(match) != 2 {
			t.Fatalf("cannot parse runtime index statement: %.80s", statement)
		}
		assertSQLiteObject(t, database, "index", match[1])
	}
	triggerPattern := regexp.MustCompile(`(?i)CREATE TRIGGER IF NOT EXISTS\s+([a-z_]+)`)
	for _, statement := range schemaTriggers {
		match := triggerPattern.FindStringSubmatch(statement)
		if len(match) != 2 {
			t.Fatalf("cannot parse runtime trigger statement: %.80s", statement)
		}
		assertSQLiteObject(t, database, "trigger", match[1])
	}
}

func TestSQLMigrationColumnsCoverRuntimeMigrate(t *testing.T) {
	runtimeDB, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	if err := runtimeDB.Migrate(); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	applySQLMigrations(t, sqlDB)

	rows, err := runtimeDB.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		runtimeColumns := sqliteColumns(t, runtimeDB, table)
		sqlColumns := sqliteColumns(t, sqlDB, table)
		for name, runtimeType := range runtimeColumns {
			sqlType, exists := sqlColumns[name]
			if !exists {
				t.Errorf("SQL migrations missing runtime column %s.%s", table, name)
				continue
			}
			if sqlType != runtimeType {
				t.Errorf("SQL migration type drift %s.%s: sql=%s runtime=%s", table, name, sqlType, runtimeType)
			}
		}
	}
}

func TestSQLMigrationsSeedEveryBuiltInPlatform(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	applySQLMigrations(t, database)

	rows, err := database.Query(`SELECT code FROM platforms ORDER BY code`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, code)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	expected := platforms.Codes()
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("SQL platform seed drift:\nactual:   %v\nexpected: %v", actual, expected)
	}
}

func TestContentMediaLibraryMigrationPreservesLegacyRows(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var linkMigration string
	for _, path := range files {
		if filepath.Base(path) == "013_content_media_library_links.sql" {
			linkMigration = path
			break
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), err)
		}
	}
	if linkMigration == "" {
		t.Fatal("content media library migration not found")
	}
	contentResult, err := database.Exec(`INSERT INTO contents (type, title, status, source, created_at, updated_at) VALUES ('video', 'legacy', 'draft', 'manual', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatal(err)
	}
	contentID, _ := contentResult.LastInsertId()
	legacyResult, err := database.Exec(`INSERT INTO media_assets (content_id, kind, file_path, source, created_at) VALUES (?, 'video', '/tmp/legacy.mp4', 'manual', CURRENT_TIMESTAMP)`, contentID)
	if err != nil {
		t.Fatal(err)
	}
	legacyID, _ := legacyResult.LastInsertId()
	body, err := os.ReadFile(linkMigration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(string(body)); err != nil {
		t.Fatalf("apply %s: %v", filepath.Base(linkMigration), err)
	}
	var libraryID sql.NullInt64
	if err := database.QueryRow(`SELECT library_id FROM media_assets WHERE id=?`, legacyID).Scan(&libraryID); err != nil {
		t.Fatal(err)
	}
	if libraryID.Valid {
		t.Fatalf("legacy media unexpectedly linked to library %d", libraryID.Int64)
	}
	if _, err := database.Exec(`INSERT INTO media_assets (content_id, kind, file_path, source, created_at) VALUES (?, 'video', '/tmp/new-unlinked.mp4', 'manual', CURRENT_TIMESTAMP)`, contentID); err == nil {
		t.Fatal("migration allowed a new unlinked content media row")
	}
	assertSQLiteObject(t, database, "index", "idx_media_assets_library")
	assertSQLiteObject(t, database, "trigger", "trg_media_assets_library_insert")
	assertSQLiteObject(t, database, "trigger", "trg_media_assets_library_update")
	rows, err := database.Query(`PRAGMA foreign_key_list(media_assets)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	foundLibraryFK := false
	for rows.Next() {
		var id, sequence int
		var table, fromColumn, toColumn, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &sequence, &table, &fromColumn, &toColumn, &onUpdate, &onDelete, &match); err != nil {
			t.Fatal(err)
		}
		if table == "media_library" && fromColumn == "library_id" && toColumn == "id" && onDelete == "RESTRICT" {
			foundLibraryFK = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !foundLibraryFK {
		t.Fatal("media_assets.library_id foreign key with ON DELETE RESTRICT not found")
	}
}

func TestRuntimeMigratePreservesLegacyContentMediaRows(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, path := range files {
		if filepath.Base(path) == "013_content_media_library_links.sql" {
			break
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), err)
		}
	}
	contentResult, err := database.Exec(`INSERT INTO contents (type, title, status, source, created_at, updated_at) VALUES ('video', 'legacy-runtime', 'draft', 'manual', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatal(err)
	}
	contentID, _ := contentResult.LastInsertId()
	legacyResult, err := database.Exec(`INSERT INTO media_assets (content_id, kind, file_path, source, created_at) VALUES (?, 'video', '/tmp/legacy-runtime.mp4', 'manual', CURRENT_TIMESTAMP)`, contentID)
	if err != nil {
		t.Fatal(err)
	}
	legacyID, _ := legacyResult.LastInsertId()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var libraryID sql.NullInt64
	if err := database.QueryRow(`SELECT library_id FROM media_assets WHERE id=?`, legacyID).Scan(&libraryID); err != nil {
		t.Fatal(err)
	}
	if libraryID.Valid {
		t.Fatalf("runtime migration unexpectedly linked legacy media to %d", libraryID.Int64)
	}
	if _, err := database.Exec(`INSERT INTO media_assets (content_id, kind, file_path, source, created_at) VALUES (?, 'video', '/tmp/runtime-unlinked.mp4', 'manual', CURRENT_TIMESTAMP)`, contentID); err == nil {
		t.Fatal("runtime migration allowed a new unlinked content media row")
	}
	assertSQLiteObject(t, database, "index", "idx_media_assets_library")
	assertSQLiteObject(t, database, "trigger", "trg_media_assets_library_insert")
}

func TestShopListingPlatformMigrationPreservesPlansItemsAndConstraints(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	applySQLMigrationsThrough(t, database, "050_more_shop_login_platforms.sql")

	if _, err := database.Exec(`INSERT OR IGNORE INTO ego_identities (id, name, created_at, updated_at)
		VALUES (1, 'shop listing migration', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO accounts (id, identity_id, platform_code, name, created_at, updated_at)
		VALUES (1, 1, 'shopify', 'legacy shop', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	planValues := []any{int64(17), int64(1), int64(1), "shopify", `{"legacy":true}`, strings.Repeat("a", 64), "legacy-plan-001", "prepare_approved", int64(1), "LEGACY_REVIEW", "2026-08-10T01:02:03Z", "2026-08-10T01:02:03Z", "2026-08-10T01:02:03Z"}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(id, identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, error_code, created_at, frozen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, planValues...); err != nil {
		t.Fatal(err)
	}
	itemValues := []any{int64(23), int64(17), int64(4), `{"legacy_item":true}`, strings.Repeat("b", 64), "manual_takeover", "LEGACY_REVIEW", "2026-08-10T01:02:03Z", "2026-08-10T01:02:03Z"}
	if _, err := database.Exec(`INSERT INTO shop_listing_items
		(id, plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, itemValues...); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "051_expand_shop_listing_platforms.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(string(body)); err != nil {
		t.Fatalf("apply 051: %v", err)
	}

	var plan struct {
		ID, IdentityID, AccountID, Concurrency                                      int64
		PlatformCode, SnapshotJSON, SnapshotHash, IdempotencyKey, Status, ErrorCode string
		CreatedAt, FrozenAt, UpdatedAt                                              string
	}
	if err := database.QueryRow(`SELECT id, identity_id, account_id, platform_code, snapshot_json, snapshot_hash,
		idempotency_key, status, concurrency, error_code, created_at, frozen_at, updated_at
		FROM shop_listing_plans WHERE id=17`).Scan(
		&plan.ID, &plan.IdentityID, &plan.AccountID, &plan.PlatformCode, &plan.SnapshotJSON, &plan.SnapshotHash,
		&plan.IdempotencyKey, &plan.Status, &plan.Concurrency, &plan.ErrorCode, &plan.CreatedAt, &plan.FrozenAt, &plan.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if plan.ID != planValues[0] || plan.IdentityID != planValues[1] || plan.AccountID != planValues[2] ||
		plan.PlatformCode != planValues[3] || plan.SnapshotJSON != planValues[4] || plan.SnapshotHash != planValues[5] ||
		plan.IdempotencyKey != planValues[6] || plan.Status != planValues[7] || plan.Concurrency != planValues[8] ||
		plan.ErrorCode != planValues[9] || plan.CreatedAt != planValues[10] || plan.FrozenAt != planValues[11] || plan.UpdatedAt != planValues[12] {
		t.Fatalf("plan changed during migration: %+v", plan)
	}
	var item struct {
		ID, PlanID, Ordinal                                               int64
		PayloadJSON, PayloadHash, Status, ErrorCode, CreatedAt, UpdatedAt string
	}
	if err := database.QueryRow(`SELECT id, plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at
		FROM shop_listing_items WHERE id=23`).Scan(&item.ID, &item.PlanID, &item.Ordinal, &item.PayloadJSON, &item.PayloadHash, &item.Status, &item.ErrorCode, &item.CreatedAt, &item.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if item.ID != itemValues[0] || item.PlanID != itemValues[1] || item.Ordinal != itemValues[2] ||
		item.PayloadJSON != itemValues[3] || item.PayloadHash != itemValues[4] || item.Status != itemValues[5] ||
		item.ErrorCode != itemValues[6] || item.CreatedAt != itemValues[7] || item.UpdatedAt != itemValues[8] {
		t.Fatalf("item changed during migration: %+v", item)
	}

	var foreignKeys int
	if err := database.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d", foreignKeys)
	}
	assertShopListingForeignKey(t, database, "shop_listing_plans", "identity_id", "ego_identities", "RESTRICT")
	assertShopListingForeignKey(t, database, "shop_listing_plans", "account_id", "accounts", "RESTRICT")
	assertShopListingForeignKey(t, database, "shop_listing_items", "plan_id", "shop_listing_plans", "CASCADE")
	var integrity string
	if err := database.QueryRow(`PRAGMA quick_check(1)`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("quick_check=%q err=%v", integrity, err)
	}

	for _, platformCode := range []string{
		"douyin_shop", "taobao", "tiktok_shop", "shopee", "shopify", "amazon_seller",
		"jd_shop", "pinduoduo_shop", "xhs_shop", "kuaishou_shop", "wechat_shop", "alibaba_1688", "aliexpress",
	} {
		if _, err := database.Exec(`INSERT INTO shop_listing_plans
			(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
			VALUES (1, 1, ?, '{}', ?, ?, 'dry_run_ok', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			platformCode, strings.Repeat("c", 64), "platform-"+platformCode); err != nil {
			t.Fatalf("platform %s rejected: %v", platformCode, err)
		}
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (1, 1, 'qianchuan', '{}', ?, 'platform-qianchuan', 'dry_run_ok', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("d", 64)); err == nil {
		t.Fatal("qianchuan was accepted by shop_listing_plans")
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (1, 1, 'shopify', '{}', ?, 'invalid-status', 'listed', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("e", 64)); err == nil {
		t.Fatal("unknown plan status was accepted after migration")
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (1, 1, 'shopify', '{}', ?, 'invalid-concurrency', 'dry_run_ok', 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("f", 64)); err == nil {
		t.Fatal("concurrency=2 was accepted after migration")
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (1, 1, 'shopify', '{}', ?, 'duplicate-key', 'dry_run_ok', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("1", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (1, 1, 'shopify', '{}', ?, 'duplicate-key', 'dry_run_ok', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("2", 64)); err == nil {
		t.Fatal("plan idempotency uniqueness was lost")
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_items
		(plan_id, ordinal, payload_json, payload_hash, status, created_at, updated_at)
		VALUES (17, 4, '{}', ?, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("3", 64)); err == nil {
		t.Fatal("item ordinal uniqueness was lost")
	}
}

func TestRuntimeMigrateExpandsLegacyShopListingPlatformsWithoutLosingRows(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	applySQLMigrationsThrough(t, database, "050_more_shop_login_platforms.sql")

	if _, err := database.Exec(`INSERT OR IGNORE INTO ego_identities (id, name, created_at, updated_at)
		VALUES (1, 'runtime shop migration', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO accounts (id, identity_id, platform_code, name, created_at, updated_at)
		VALUES (1, 1, 'shopify', 'legacy shop', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(id, identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (17, 1, 1, 'shopify', '{}', ?, 'runtime-legacy-plan', 'dry_run_ok', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_items
		(id, plan_id, ordinal, payload_json, payload_hash, status, created_at, updated_at)
		VALUES (23, 17, 0, '{}', ?, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var planID, itemID int64
	if err := database.QueryRow(`SELECT id FROM shop_listing_plans WHERE id=17`).Scan(&planID); err != nil || planID != 17 {
		t.Fatalf("plan id=%d err=%v", planID, err)
	}
	if err := database.QueryRow(`SELECT id FROM shop_listing_items WHERE id=23 AND plan_id=17`).Scan(&itemID); err != nil || itemID != 23 {
		t.Fatalf("item id=%d err=%v", itemID, err)
	}
	if _, err := database.Exec(`INSERT INTO shop_listing_plans
		(identity_id, account_id, platform_code, snapshot_json, snapshot_hash, idempotency_key, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (1, 1, 'aliexpress', '{}', ?, 'runtime-new-platform', 'dry_run_ok', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, strings.Repeat("c", 64)); err != nil {
		t.Fatalf("expanded runtime schema rejected aliexpress: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("runtime migration is not idempotent: %v", err)
	}
}

func applySQLMigrationsThrough(t *testing.T, database *DB, lastFile string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), err)
		}
		if filepath.Base(path) == lastFile {
			return
		}
	}
	t.Fatalf("migration %s not found", lastFile)
}

func assertShopListingForeignKey(t *testing.T, database *DB, table, from, target, onDelete string) {
	t.Helper()
	rows, err := database.Query("PRAGMA foreign_key_list(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, seq int
		var referencedTable, referencedFrom, referencedTo, onUpdate, referencedDelete, match string
		if err := rows.Scan(&id, &seq, &referencedTable, &referencedFrom, &referencedTo, &onUpdate, &referencedDelete, &match); err != nil {
			t.Fatal(err)
		}
		if referencedTable == target && referencedFrom == from && referencedTo == "id" && referencedDelete == onDelete {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("missing %s.%s -> %s ON DELETE %s foreign key", table, from, target, onDelete)
}

func applySQLMigrations(t *testing.T, database *DB) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migration files: %v count=%d", err, len(files))
	}
	sort.Strings(files)
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), err)
		}
	}
}

func sqliteColumns(t *testing.T, database *DB, table string) map[string]string {
	t.Helper()
	rows, err := database.Query(fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]string{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = strings.ToUpper(strings.TrimSpace(dataType))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}

func assertSQLiteObject(t *testing.T, database *DB, kind, name string) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type=? AND name=?`, kind, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("SQL migrations missing runtime %s %s", kind, name)
	}
}
