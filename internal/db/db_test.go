package db_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
)

func setupDB(t *testing.T) (*db.DB, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "alpha-nexus-test-*")
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(dir)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	return d, func() {
		d.Close()
		os.RemoveAll(dir)
	}
}

func TestOpenAndMigrate(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()

	if err := d.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}
}

func TestUpdateAccountAssetConnectionValidatesOwnerAndIdentity(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("meta", "Meta operator", "")
	if err != nil {
		t.Fatal(err)
	}
	facebook, err := d.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "111111111", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	instagram, err := d.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Alpha IG", "222222222", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateAccountAssetConnection(facebook.ID, instagram.ID, "instagram", "instagram_account", instagram.Name, instagram.ExternalID); err != nil {
		t.Fatal(err)
	}
	connected, err := d.GetAccountAsset(facebook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if connected.ConnectedAssetID != instagram.ID || connected.ConnectedAssetName != instagram.Name {
		t.Fatalf("connection=%+v", connected)
	}
	if err := d.UpdateAccountAssetConnection(facebook.ID, instagram.ID, "instagram", "instagram_account", "Wrong name", instagram.ExternalID); err == nil {
		t.Fatal("connection accepted mismatched target name")
	}
	if err := d.UpdateAccountAssetConnection(facebook.ID, instagram.ID, "facebook", "instagram_account", instagram.Name, instagram.ExternalID); err == nil {
		t.Fatal("connection accepted mismatched target platform")
	}
	if err := d.UpdateAccountAssetConnection(facebook.ID, instagram.ID, "instagram", "facebook_page", instagram.Name, instagram.ExternalID); err == nil {
		t.Fatal("connection accepted mismatched target type")
	}
	if err := d.UpdateAccountAssetConnection(facebook.ID, instagram.ID, "instagram", "instagram_account", instagram.Name, "999999999"); err == nil {
		t.Fatal("connection accepted mismatched target external ID")
	}
	otherAccount, err := d.CreateAccount("meta", "Other Meta operator", "")
	if err != nil {
		t.Fatal(err)
	}
	otherInstagram, err := d.CreateAccountAsset(otherAccount.ID, "instagram", "instagram_account", instagram.Name, instagram.ExternalID, "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateAccountAssetConnection(facebook.ID, otherInstagram.ID, "instagram", "instagram_account", otherInstagram.Name, otherInstagram.ExternalID); err == nil {
		t.Fatal("connection accepted target from another owner")
	}
	wrongSource, err := d.CreateAccountAsset(account.ID, "csdn", "csdn_column", "Wrong source", "333333333", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateAccountAssetConnection(wrongSource.ID, instagram.ID, "instagram", "instagram_account", instagram.Name, instagram.ExternalID); err == nil {
		t.Fatal("connection accepted a non-Meta source asset")
	}
	unchanged, err := d.GetAccountAsset(facebook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ConnectedAssetID != instagram.ID || unchanged.ConnectedAssetName != instagram.Name {
		t.Fatalf("failed validation mutated connection=%+v", unchanged)
	}
}

func TestQuickCheckReportsHealthyDatabase(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()

	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := d.QuickCheck(); err != nil {
		t.Fatalf("QuickCheck() failed for healthy database: %v", err)
	}
}

func TestOpenReadOnlyReadsHealthyMigratedDatabase(t *testing.T) {
	dataDir := t.TempDir()
	writable, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Migrate(); err != nil {
		writable.Close()
		t.Fatal(err)
	}
	if _, err := writable.Exec(`CREATE TABLE readonly_fixture (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		writable.Close()
		t.Fatal(err)
	}
	if _, err := writable.Exec(`INSERT INTO readonly_fixture(id, value) VALUES (1, 'healthy')`); err != nil {
		writable.Close()
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	readonly, err := db.OpenReadOnly(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	if readonly.DataDir != dataDir {
		t.Fatalf("DataDir=%q, want %q", readonly.DataDir, dataDir)
	}
	var value string
	if err := readonly.QueryRow(`SELECT value FROM readonly_fixture WHERE id=1`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "healthy" {
		t.Fatalf("fixture value=%q", value)
	}
	var foreignKeys, queryOnly, busyTimeout int
	if err := readonly.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := readonly.QueryRow(`PRAGMA query_only`).Scan(&queryOnly); err != nil {
		t.Fatal(err)
	}
	if err := readonly.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || queryOnly != 1 || busyTimeout != 5000 {
		t.Fatalf("read-only pragmas foreign_keys=%d query_only=%d busy_timeout=%d", foreignKeys, queryOnly, busyTimeout)
	}
}

func TestOpenReadOnlyRejectsWrites(t *testing.T) {
	dataDir := t.TempDir()
	writable, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Migrate(); err != nil {
		writable.Close()
		t.Fatal(err)
	}
	if _, err := writable.Exec(`CREATE TABLE readonly_write_fixture (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		writable.Close()
		t.Fatal(err)
	}
	if _, err := writable.Exec(`INSERT INTO readonly_write_fixture(id, value) VALUES (1, 'before')`); err != nil {
		writable.Close()
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	readonly, err := db.OpenReadOnly(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	if _, err := readonly.Exec(`INSERT INTO readonly_write_fixture(id, value) VALUES (2, 'insert')`); err == nil {
		t.Fatal("read-only INSERT succeeded")
	}
	if _, err := readonly.Exec(`UPDATE readonly_write_fixture SET value='updated' WHERE id=1`); err == nil {
		t.Fatal("read-only UPDATE succeeded")
	}
	var value string
	if err := readonly.QueryRow(`SELECT value FROM readonly_write_fixture WHERE id=1`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "before" {
		t.Fatalf("fixture value=%q after rejected writes", value)
	}
}

func TestOpenReadOnlyDoesNotCreateMissingDatabase(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing-data-dir")
	if _, err := db.OpenReadOnly(dataDir); err == nil {
		t.Fatal("OpenReadOnly accepted a missing database")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "app.db")); !os.IsNotExist(err) {
		t.Fatalf("app.db stat error=%v, want not exist", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir stat error=%v, want not exist", err)
	}
}

func TestOpenReadOnlyPreservesWritableOpenInitialization(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "writable-data-dir")
	writable, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer writable.Close()
	if err := writable.QuickCheck(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "app.db")); err != nil {
		t.Fatalf("writable Open did not initialize app.db: %v", err)
	}
}

func TestOpenRejectsCorruptedSQLiteBeforeUse(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE corrupt_fixture (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 100; index++ {
		if _, err := database.Exec(`INSERT INTO corrupt_fixture(body) VALUES (?)`, strings.Repeat("x", 1024)); err != nil {
			t.Fatal(err)
		}
	}
	var pageSize, rootPage int64
	if err := database.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT rootpage FROM sqlite_master WHERE name='corrupt_fixture'`).Scan(&rootPage); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	appDB := filepath.Join(dataDir, "app.db")
	file, err := os.OpenFile(appDB, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt(bytes.Repeat([]byte{0xff}, 128), (rootPage-1)*pageSize)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("corrupt fixture write=%v close=%v", writeErr, closeErr)
	}

	// 启动不再同步做全量 quick_check（124 MB 库要 3.3 s），但损坏必须仍能被
	// 检测到：独立连接上的 VerifyIntegrity / QuickCheck 必须报错。
	reopened, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("open must not block on integrity: %v", err)
	}
	defer reopened.Close()
	if err := reopened.VerifyIntegrity(context.Background()); err == nil {
		t.Fatal("corrupted SQLite database was accepted")
	}
}

func TestMigrationIdempotent(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()

	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	if err := d.Migrate(); err != nil {
		t.Fatalf("second Migrate() should be idempotent: %v", err)
	}
}

func TestSetAccountEnabledSynchronizesTargets(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "disabled-account", "")
	if err != nil {
		t.Fatal(err)
	}
	target, err := d.CreateAccountAsset(account.ID, "xhs", "xhs_account", "extra-target", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetAccountEnabled(account.ID, false); err != nil {
		t.Fatal(err)
	}
	updated, err := d.GetAccount(account.ID)
	if err != nil || updated.Enabled {
		t.Fatalf("account=%+v err=%v", updated, err)
	}
	for _, id := range []int64{target.ID} {
		asset, err := d.GetAccountAsset(id)
		if err != nil || asset.Enabled {
			t.Fatalf("asset=%+v err=%v", asset, err)
		}
	}
	createdWhileDisabled, err := d.CreateAccountAsset(account.ID, "xhs", "xhs_account", "new-target", "", "", 0, "")
	if err != nil || createdWhileDisabled.Enabled {
		t.Fatalf("asset=%+v err=%v", createdWhileDisabled, err)
	}
	if err := d.SetAccountEnabled(account.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{target.ID, createdWhileDisabled.ID} {
		asset, err := d.GetAccountAsset(id)
		if err != nil || !asset.Enabled {
			t.Fatalf("asset=%+v err=%v", asset, err)
		}
	}
}

func TestSeedPlatforms(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()

	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	rows, err := d.Query("SELECT code, name FROM platforms WHERE code <> 'meta' ORDER BY sort")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var count int
	expected := platforms.All()

	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err != nil {
			t.Fatal(err)
		}
		if count >= len(expected) {
			t.Fatalf("unexpected extra platform: %s", code)
		}
		if code != expected[count].Code || name != expected[count].Name {
			t.Errorf("row %d: got (%q, %q), want (%q, %q)", count, code, name, expected[count].Code, expected[count].Name)
		}
		count++
	}
	if count != len(expected) {
		t.Errorf("got %d platforms, want %d", count, len(expected))
	}
	var metaName string
	if err := d.QueryRow("SELECT name FROM platforms WHERE code='meta'").Scan(&metaName); err != nil || metaName != "Meta" {
		t.Fatalf("stored Meta login alias name=%q err=%v", metaName, err)
	}
}

func TestSeedPlatformsRefreshesSystemCatalogWithoutChangingUserEnablement(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()

	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE platforms
		SET name='stale', login_url='https://stale.invalid', manager_url='https://stale.invalid',
			supports_article=1, supports_graph_text=0, supports_video=0, sort=999,
			enabled=0, source_platform_id=77
		WHERE code='instagram'`); err != nil {
		t.Fatal(err)
	}

	if err := d.SeedPlatforms(); err != nil {
		t.Fatal(err)
	}

	var name, loginURL, managerURL string
	var article, graphText, video, sort, enabled int
	var sourcePlatformID int64
	if err := d.QueryRow(`SELECT name, login_url, manager_url, supports_article,
			supports_graph_text, supports_video, sort, enabled, source_platform_id
			FROM platforms WHERE code='instagram'`).
		Scan(&name, &loginURL, &managerURL, &article, &graphText, &video, &sort, &enabled, &sourcePlatformID); err != nil {
		t.Fatal(err)
	}
	catalog, err := platforms.Find("instagram")
	if err != nil {
		t.Fatal(err)
	}
	if name != catalog.Name || loginURL != catalog.LoginURL || managerURL != catalog.ManagerURL ||
		article != 0 || graphText != 1 || video != 1 || sort != catalog.Sort {
		t.Fatalf("system catalog was not refreshed: name=%q login=%q manager=%q capabilities=%d/%d/%d sort=%d",
			name, loginURL, managerURL, article, graphText, video, sort)
	}
	if enabled != 0 || sourcePlatformID != 77 {
		t.Fatalf("user-owned state changed: enabled=%d source_platform_id=%d", enabled, sourcePlatformID)
	}
}

func TestCoreTablesExist(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()

	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	expectedTables := []string{
		"platforms", "accounts", "browser_profiles", "account_assets",
		"platform_settings", "contents", "media_assets", "publish_jobs",
		"job_events", "batch_runs", "batch_items", "publish_records",
		"record_infos", "notifications",
		"browser_runtime_instances", "profile_leases", "cdp_port_leases", "browser_operation_leases", "browser_events",
		"audit_events", "task_queue", "post_metrics",
		"content_versions", "credentials", "media_library", "studio_scripts", "clip_jobs",
	}

	for _, table := range expectedTables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := d.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
			if err != nil {
				t.Errorf("table %q not found: %v", table, err)
			}
		})
	}
}

func TestFreshSchemaExcludesScheduling(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schedule_entries'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("fresh schema created schedule_entries")
	}
	if columnExists(t, d, "publish_jobs", "scheduled_at") {
		t.Fatal("fresh schema created publish_jobs.scheduled_at")
	}
}

func TestMigratePreservesLegacyScheduleData(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if _, err := d.Exec(`CREATE TABLE schedule_entries (id INTEGER PRIMARY KEY, name TEXT NOT NULL, payload_json TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO schedule_entries (id, name, payload_json) VALUES (1, 'legacy', '{"tool":"publish.run"}')`); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	var name string
	if err := d.QueryRow(`SELECT name FROM schedule_entries WHERE id=1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "legacy" {
		t.Fatalf("legacy schedule name=%q", name)
	}
}

// TestRuntimeTablesExist asserts multi-browser runtime schema columns (T-E01).
func TestRuntimeTablesExist(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()

	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	// browser_runtime_instances required columns
	runtimeCols := []string{
		"profile_id", "account_id", "pid", "cdp_port", "cdp_url",
		"state", "active_job_id", "lease_owner", "started_at", "last_seen_at", "updated_at",
	}
	for _, col := range runtimeCols {
		var name string
		err := d.QueryRow(
			`SELECT name FROM pragma_table_info('browser_runtime_instances') WHERE name = ?`, col,
		).Scan(&name)
		if err != nil {
			t.Errorf("browser_runtime_instances.%s missing: %v", col, err)
		}
	}

	// profile_leases required columns
	leaseCols := []string{
		"profile_id", "holder_type", "holder_id", "acquired_at", "expires_at", "updated_at",
	}
	for _, col := range leaseCols {
		var name string
		err := d.QueryRow(
			`SELECT name FROM pragma_table_info('profile_leases') WHERE name = ?`, col,
		).Scan(&name)
		if err != nil {
			t.Errorf("profile_leases.%s missing: %v", col, err)
		}
	}

	// smoke insert/delete (Facade-ready empty runtime)
	_, err := d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES ('smoke-profile', 1, 1, 15101, 'http://127.0.0.1:15101', 'ready', datetime('now'), datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	_, err = d.Exec(`INSERT INTO profile_leases
		(profile_id, holder_type, holder_id, acquired_at, expires_at, updated_at)
		VALUES ('smoke-profile', 'cli', 'smoke', datetime('now'), datetime('now', '+30 seconds'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert lease: %v", err)
	}
	d.Exec(`DELETE FROM profile_leases WHERE profile_id = 'smoke-profile'`)
	d.Exec(`DELETE FROM browser_runtime_instances WHERE profile_id = 'smoke-profile'`)
}

func TestDataDirCreated(t *testing.T) {
	dir, err := os.MkdirTemp("", "alpha-nexus-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "app.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// DB not created yet; good, we just opened
	}

	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("db file should exist after Open")
	}
}

func TestOpenEnablesForeignKeysAndRestrictsPermissions(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	var enabled int
	if err := d.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("foreign_keys=%d, want 1", enabled)
	}
	d.SetConnMaxLifetime(time.Nanosecond)
	time.Sleep(time.Millisecond)
	if err := d.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("foreign_keys after reconnect=%d, want 1", enabled)
	}
	if _, err := d.Exec(`INSERT INTO browser_profiles (account_id, profile_dir, browser_type, fingerprint_seed, created_at, updated_at) VALUES (999999, 'invalid', 'chromium', 1, 'now', 'now')`); err == nil {
		t.Fatal("foreign key accepted an unknown account")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(d.DataDir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("data dir mode=%#o, want 0700", got)
		}
		info, err = os.Stat(filepath.Join(d.DataDir, "app.db"))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("app.db mode=%#o, want 0600", got)
		}
	}
}

func TestDeleteAccountRemovesOwnedSetupRowsAtomically(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "delete-fresh", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateLoginSession(account.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteAccount(account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetAccount(account.ID); err == nil {
		t.Fatal("account still exists")
	}
	for table := range map[string]struct{}{"account_assets": {}, "browser_profiles": {}, "login_sessions": {}} {
		var count int
		if err := d.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE account_id=?", account.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s rows remain: %d", table, count)
		}
	}
}

func TestDeleteAccountWithPublishHistoryRollsBackOwnedRows(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "delete-history", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := d.CreateContent("graph_text", "history", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := d.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	if _, err := d.CreatePublishJobWithIntent(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, "", "prepare", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteAccount(account.ID); err == nil {
		t.Fatal("account with publish history was deleted")
	}
	if _, err := d.GetAccount(account.ID); err != nil {
		t.Fatalf("account delete was not rolled back: %v", err)
	}
	remainingAssets, err := d.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(remainingAssets) != 1 {
		t.Fatalf("account assets were not rolled back: assets=%+v err=%v", remainingAssets, err)
	}
}

func TestPublishJobAndRecordIdempotency(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "idempotency", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := d.CreateContent("graph_text", "", "", "", "", "draft")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := d.CreateBatchRun("publish", "xhs", "pending", 1, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := d.CreatePublishJob(batch.ID, content.ID, account.ID, 0, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.CreatePublishJob(batch.ID, content.ID, account.ID, 0, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.IdempotencyKey == "" {
		t.Fatalf("jobs not idempotent: first=%+v second=%+v", first, second)
	}
	directKey := "request:publish-20260716-0001"
	directFirst, err := d.CreatePublishJobWithIdempotencyKey(0, content.ID, account.ID, 0, "xhs", content.Type, directKey)
	if err != nil {
		t.Fatal(err)
	}
	directSecond, err := d.CreatePublishJobWithIdempotencyKey(0, content.ID, account.ID, 0, "xhs", content.Type, directKey)
	if err != nil {
		t.Fatal(err)
	}
	if directFirst.ID != directSecond.ID || directFirst.IdempotencyKey != directKey {
		t.Fatalf("direct key was not idempotent: first=%+v second=%+v", directFirst, directSecond)
	}
	if _, err := d.CreatePublishJobWithIdempotencyKey(0, content.ID, account.ID, 0, "xhs", content.Type, "secret key with spaces"); err == nil {
		t.Fatal("unsafe idempotency key accepted")
	}
	if _, err := d.CreatePublishJobWithIdempotencyKey(0, content.ID, account.ID, 0, "instagram", content.Type, directKey); err == nil {
		t.Fatal("conflicting request reused the idempotency key")
	}
	if _, err := d.CreatePublishRecord(first.ID, content.ID, account.ID, 0, "xhs", content.Type, "published", "", ""); !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("unverified publish record error=%v; want conflict", err)
	}
	var recordCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, first.ID).Scan(&recordCount); err != nil || recordCount != 0 {
		t.Fatalf("unverified record count=%d err=%v", recordCount, err)
	}
	if err := d.UpdatePublishJob(first.ID+9999, "failed", "test", "TEST", "", 0); err == nil {
		t.Fatal("missing publish job update returned nil")
	}

	// A job can be acquired by exactly one executor.
	var wg sync.WaitGroup
	claims := make(chan bool, 2)
	errs := make(chan error, 2)
	for _, traceID := range []string{"trace-a", "trace-b"} {
		wg.Add(1)
		go func(traceID string) {
			defer wg.Done()
			claimed, err := d.ClaimPublishJob(first.ID, traceID)
			claims <- claimed
			errs <- err
		}(traceID)
	}
	wg.Wait()
	close(claims)
	close(errs)
	claimedCount := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for claimed := range claims {
		if claimed {
			claimedCount++
		}
	}
	if claimedCount != 1 {
		t.Fatalf("atomic claims=%d, want exactly 1", claimedCount)
	}

	legacy, err := d.CreatePublishJobWithIdempotencyKey(0, content.ID, account.ID, 0, "xhs", content.Type, "legacy:waiting-schedule")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdatePublishJob(legacy.ID, "waiting_schedule", "legacy", "", "", 0); err != nil {
		t.Fatal(err)
	}
	claimed, err := d.ClaimPublishJob(legacy.ID, "trace-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("legacy waiting_schedule job was claimable")
	}
}

func TestCreatePublishJobWithIntentResultMarksReplay(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "intent-replay", "")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := d.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := d.CreateContent("graph_text", "replay", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := d.CreatePublishJobWithIntentResult(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, "intent-replay:0001", "dry_run", `{"snapshot_version":1}`)
	if err != nil || !created {
		t.Fatalf("first job=%+v created=%v err=%v", first, created, err)
	}
	second, created, err := d.CreatePublishJobWithIntentResult(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, "intent-replay:0001", "dry_run", `{"snapshot_version":1}`)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("replay job=%+v created=%v err=%v", second, created, err)
	}
}

func TestJobEventAdditiveSchemaAndRoundTrip(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}

	wantCols := []string{
		"trace_id", "request_id", "task_id", "duration_ms",
		"error_code", "error_class", "retryable", "schema_version", "raw_json",
	}
	for _, col := range wantCols {
		var name string
		if err := d.QueryRow(`SELECT name FROM pragma_table_info('job_events') WHERE name = ?`, col).Scan(&name); err != nil {
			t.Fatalf("job_events.%s missing: %v", col, err)
		}
	}

	account, err := d.CreateAccount("xhs", "event-test", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := d.CreateContent("graph_text", "", "", "", "", "draft")
	if err != nil {
		t.Fatal(err)
	}
	job, err := d.CreatePublishJob(0, content.ID, account.ID, 0, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}

	created, err := d.CreateJobEventDetailed(db.JobEventInput{
		JobID:      job.ID,
		Level:      "error",
		Step:       "submit",
		Type:       "publish.failed",
		Message:    "sanitized",
		TraceID:    "trace-1",
		RequestID:  "request-1",
		TaskID:     "task-1",
		DurationMS: 42,
		ErrorCode:  "SUBMIT_FAILED",
		ErrorClass: "platform",
		Retryable:  true,
		RawJSON:    `{"safe":true}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.SchemaVersion != 1 {
		t.Fatalf("schema version=%d", created.SchemaVersion)
	}
	events, err := d.ListJobEventsByJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	replay, err := d.ListJobEventsAfter(0, 10)
	if err != nil || len(replay) != 1 || replay[0].ID != created.ID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if replay, err := d.ListJobEventsAfter(created.ID, 10); err != nil || len(replay) != 0 {
		t.Fatalf("cursor replay=%+v err=%v", replay, err)
	}
	if _, err := d.ListJobEventsAfter(-1, 10); err == nil {
		t.Fatal("negative replay cursor accepted")
	}
	got := events[0]
	if got.TraceID != "trace-1" || got.RequestID != "request-1" || got.TaskID != "task-1" || got.DurationMS != 42 || got.ErrorCode != "SUBMIT_FAILED" || got.ErrorClass != "platform" || !got.Retryable || got.RawJSON != `{"safe":true}` {
		t.Fatalf("event round trip mismatch: %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || string(encoded) == `null` || containsJSONKey(encoded, "RawJSON") || containsJSONKey(encoded, "raw_json") {
		t.Fatalf("raw_json escaped API serialization: %s", encoded)
	}

	invalidEvents := []db.JobEventInput{
		{JobID: job.ID, Level: "fatal", Type: "bad.level"},
		{JobID: job.ID, Level: "info", Type: "bad type"},
		{JobID: job.ID, Level: "info", Type: "bad.duration", DurationMS: -1},
		{JobID: job.ID, Level: "error", Type: "bad.retry", Retryable: true},
		{JobID: job.ID, Level: "info", Type: "bad.class", ErrorClass: "secret"},
		{JobID: job.ID, Level: "info", Type: "bad.raw", RawJSON: `{`},
	}
	for _, input := range invalidEvents {
		if _, err := d.CreateJobEventDetailed(input); err == nil {
			t.Fatalf("invalid event accepted: %+v", input)
		}
	}

	var indexName string
	if err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_job_events_trace'`).Scan(&indexName); err != nil {
		t.Fatalf("idx_job_events_trace missing: %v", err)
	}
}

func TestTaskQueueLeaseRenewReclaimAndCancel(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	first, err := d.EnqueueTask("publish", `{}`, 1, "", "trace-queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Second).Truncate(time.Second)
	claimed, err := d.ClaimNextTaskWithLease(now, "worker-a", time.Minute)
	if err != nil || claimed == nil || claimed.ID != first.ID || claimed.Attempts != 1 || claimed.LeaseOwner != "worker-a" {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	if other, err := d.ClaimNextTaskWithLease(now, "worker-b", time.Minute); err != nil || other != nil {
		t.Fatalf("concurrent lease claim=%+v err=%v", other, err)
	}
	if renewed, err := d.RenewTaskLease(first.ID, "worker-a", now.Add(30*time.Second), time.Minute); err != nil || !renewed {
		t.Fatalf("renewed=%v err=%v", renewed, err)
	}
	if renewed, err := d.RenewTaskLease(first.ID, "worker-b", now, time.Minute); err != nil || renewed {
		t.Fatalf("wrong owner renewed=%v err=%v", renewed, err)
	}
	if cancelled, err := d.RequestTaskCancel(first.ID); err != nil || !cancelled {
		t.Fatalf("cancelled=%v err=%v", cancelled, err)
	}
	if completed, err := d.CompleteLeasedTask(first.ID, "worker-a", "done", "", 1); err != nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	items, err := d.ListTaskQueue(10)
	if err != nil || len(items) != 1 || items[0].Status != "cancelled" || items[0].LeaseOwner != "" {
		t.Fatalf("items=%+v err=%v", items, err)
	}

	second, err := d.EnqueueTask("publish", `{}`, 1, "", "trace-reclaim")
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := d.ClaimNextTaskWithLease(now, "worker-a", time.Second); err != nil || claimed == nil || claimed.ID != second.ID {
		t.Fatalf("first reclaim lease=%+v err=%v", claimed, err)
	}
	reclaimed, err := d.ClaimNextTaskWithLease(now.Add(2*time.Second), "worker-b", time.Minute)
	if err != nil || reclaimed == nil || reclaimed.ID != second.ID || reclaimed.Attempts != 2 || reclaimed.LeaseOwner != "worker-b" {
		t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
	}
	if _, err := d.ClaimNextTaskWithLease(now, "bad owner", time.Minute); err == nil {
		t.Fatal("unsafe lease owner accepted")
	}
}

func TestNotificationsReadLifecycle(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	first, err := d.CreateNotification("publish.completed", "info", "发布完成", "publish completed", "publish_job", 12)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateNotification("publish.failed", "error", "发布失败", "publish failed", "publish_job", 13); err != nil {
		t.Fatal(err)
	}
	unread, err := d.ListNotifications(10, true)
	if err != nil || len(unread) != 2 || unread[0].Read {
		t.Fatalf("unread=%+v err=%v", unread, err)
	}
	if changed, err := d.MarkNotificationRead(first.ID); err != nil || !changed {
		t.Fatalf("mark changed=%v err=%v", changed, err)
	}
	if changed, err := d.MarkNotificationRead(first.ID); err != nil || changed {
		t.Fatalf("second mark changed=%v err=%v", changed, err)
	}
	if changed, err := d.MarkAllNotificationsRead(); err != nil || changed != 1 {
		t.Fatalf("mark all changed=%d err=%v", changed, err)
	}
	items, err := d.ListNotifications(10, false)
	if err != nil || len(items) != 2 || !items[0].Read || !items[1].Read {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestContentEditorLifecycle(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := d.CreateContent("article", "原标题", "摘要", "正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ReplaceContent(content.ID, "article", "新标题", "", "", `[]`, "archived"); err != nil {
		t.Fatal(err)
	}
	updated, err := d.GetContent(content.ID)
	if err != nil || updated.Title != "新标题" || updated.Caption != "" || updated.Body != "" || updated.Status != "archived" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	clone, err := d.CloneContent(content.ID)
	if err != nil || clone.ID == content.ID || clone.Status != "draft" || clone.Title != "新标题（副本）" {
		t.Fatalf("clone=%+v err=%v", clone, err)
	}
	if err := d.DeleteContent(content.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetContent(content.ID); err == nil {
		t.Fatal("deleted content remained readable")
	}
}

func TestContentPlannedAtIsLocalEditorialMetadata(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	const plannedAt = "2026-08-01T09:30:00Z"
	content, err := d.CreateContentWithPlannedAt("article", "排期文章", "", "正文", "[]", "draft", plannedAt)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := d.GetContent(content.ID)
	if err != nil || loaded.PlannedAt != plannedAt {
		t.Fatalf("content=%+v err=%v", loaded, err)
	}
	if err := d.ReplaceContentWithPlannedAt(content.ID, "article", "排期文章", "", "正文", "[]", "draft", ""); err != nil {
		t.Fatal(err)
	}
	loaded, err = d.GetContent(content.ID)
	if err != nil || loaded.PlannedAt != "" {
		t.Fatalf("cleared content=%+v err=%v", loaded, err)
	}
	if columnExists(t, d, "publish_jobs", "scheduled_at") {
		t.Fatal("content planning must not create publish job scheduling")
	}
}

func TestPlatformTemplateLifecycleAndSecretGuard(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	first, err := d.CreatePlatformSetting("xhs", "默认图文", "公开设置", `{"visibility":"public"}`, true, true)
	if err != nil || first.ID == 0 || !first.IsDefault || first.Source != "manual" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := d.CreatePlatformSetting("xhs", "备用", "", `{"comment_enabled":false}`, false, true)
	if err != nil || !second.IsDefault {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	updatedFirst, err := d.GetPlatformSetting(first.ID)
	if err != nil || updatedFirst.IsDefault {
		t.Fatalf("updated first=%+v err=%v", updatedFirst, err)
	}
	if err := d.ReplacePlatformSetting(second.ID, "xhs", "备用图文", "说明", `{"comment_enabled":true}`, false, false); err != nil {
		t.Fatal(err)
	}
	updatedSecond, err := d.GetPlatformSetting(second.ID)
	if err != nil || updatedSecond.Name != "备用图文" || updatedSecond.Enabled || updatedSecond.IsDefault {
		t.Fatalf("updated second=%+v err=%v", updatedSecond, err)
	}
	if _, err := d.CreatePlatformSetting("xhs", "unsafe", "", `{"nested":{"cookie":"value"}}`, true, false); err == nil {
		t.Fatal("credential-shaped template settings were accepted")
	}
	if err := d.DeletePlatformSetting(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetPlatformSetting(first.ID); err == nil {
		t.Fatal("deleted template remained readable")
	}
}

func TestLoginSessionLifecycleDoesNotStoreSecrets(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "login-session", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := d.CreateLoginSession(account.ID)
	if err != nil || first.Status != "opening_browser" || first.ExpiresAt == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	startedAt, err := time.Parse(time.RFC3339, first.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt, err := time.Parse(time.RFC3339, first.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if expiresAt.Sub(startedAt) < 55*time.Minute {
		t.Fatalf("login sync window=%s, want at least 55m", expiresAt.Sub(startedAt))
	}
	second, err := d.CreateLoginSession(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := d.ListLoginSessions(10)
	if err != nil || len(sessions) != 2 || sessions[0].ID != second.ID || sessions[1].Status != "cancelled" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	if err := d.UpdateLoginSession(second.ID, "verified", ""); err != nil {
		t.Fatal(err)
	}
	active, err := d.ActiveLoginSession(account.ID)
	if err != nil || active != nil {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}

func TestProxyEndpointLifecycleAndBinding(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "pooled-proxy", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateBrowserProfile(account.ID, t.TempDir(), 1, "en-US", "en-US", "UTC"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateProxyEndpoint("unsafe", "socks5://user:password@127.0.0.1:1080", true); err == nil {
		t.Fatal("credential proxy endpoint accepted")
	}
	endpoint, err := d.CreateProxyEndpoint("local", "socks5://127.0.0.1:1080", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.BindProxyEndpoint(account.ID, endpoint.ID); err != nil {
		t.Fatal(err)
	}
	profile, err := d.GetBrowserProfileByAccount(account.ID)
	if err != nil || !profile.ProxyID.Valid || profile.ProxyID.Int64 != endpoint.ID || profile.ProxyServer != endpoint.Server {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
	if err := d.DeleteProxyEndpoint(endpoint.ID); err == nil {
		t.Fatal("bound endpoint deleted")
	}
	updated, err := d.UpdateProxyEndpoint(endpoint.ID, "local-rotated", "http://127.0.0.1:8080", true)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Server != "http://127.0.0.1:8080" {
		t.Fatalf("updated=%+v", updated)
	}
	profile, err = d.GetBrowserProfileByAccount(account.ID)
	if err != nil || profile.ProxyServer != updated.Server {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
	if err := d.BindProxyEndpoint(account.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteProxyEndpoint(endpoint.ID); err != nil {
		t.Fatal(err)
	}
}

func TestManualPublishConfirmationCannotCreateAuthoritativeRecord(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "manual-confirm", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := d.CreateContent("graph_text", "manual", "caption", "", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	job, err := d.CreatePublishJob(0, content.ID, account.ID, 0, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdatePublishJob(job.ID, "manual_takeover", "open_creator", "AWAITING_HUMAN", "handoff", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ConfirmManualPublish(job.ID, "", ""); err == nil {
		t.Fatal("manual publish confirmation without platform evidence was accepted")
	}
	if _, err := d.ConfirmManualPublish(job.ID, "https://www.xiaohongshu.com/explore/a?token=no", "note-1"); err == nil {
		t.Fatal("post URL query was accepted")
	}
	record, err := d.ConfirmManualPublish(job.ID, "https://www.xiaohongshu.com/explore/a", "note-1")
	if record != nil || !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("manual attestation record=%+v err=%v", record, err)
	}
	pendingJob, err := d.GetPublishJob(job.ID)
	if err != nil || pendingJob.Status != "manual_takeover" || pendingJob.CurrentStep != "open_creator" {
		t.Fatalf("job=%+v err=%v", pendingJob, err)
	}
	var records int
	if err := d.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
	manual, err := d.ListManualPublishJobs(10)
	if err != nil || len(manual) != 1 || manual[0].ID != job.ID {
		t.Fatalf("manual=%+v err=%v", manual, err)
	}
}

func TestAwaitingReviewCanBeListedButNotUserConfirmed(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := d.CreateAccount("xhs", "review-confirm", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := d.CreateContent("graph_text", "review", "caption", "", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	job, err := d.CreatePublishJob(0, content.ID, account.ID, 0, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdatePublishJob(job.ID, "awaiting_review", "review", "", "", 0); err != nil {
		t.Fatal(err)
	}

	pending, err := d.ListManualPublishJobs(10)
	if err != nil || len(pending) != 1 || pending[0].ID != job.ID {
		t.Fatalf("pending confirmations=%+v err=%v", pending, err)
	}
	record, err := d.ConfirmManualPublish(job.ID, "", "review-note-1")
	if record != nil || !errors.Is(err, db.ErrPublishJobConflict) {
		t.Fatalf("manual attestation record=%+v err=%v", record, err)
	}
	var stored int
	if err := d.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("stored publish records=%d err=%v", stored, err)
	}
}

func TestMigrateUpgradesLegacyJobEventTables(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	legacy := []string{
		`CREATE TABLE publish_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, batch_id INTEGER, content_id INTEGER NOT NULL,
			account_id INTEGER NOT NULL, asset_id INTEGER, platform_code TEXT NOT NULL,
			content_type TEXT NOT NULL, mode TEXT NOT NULL, status TEXT NOT NULL, current_step TEXT,
			settings_json TEXT, scheduled_at TEXT, started_at TEXT, finished_at TEXT,
			retry_count INTEGER NOT NULL DEFAULT 0, max_retries INTEGER NOT NULL DEFAULT 2,
			error_code TEXT, error_message TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE job_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, job_id INTEGER NOT NULL, level TEXT NOT NULL,
			step TEXT, event_type TEXT NOT NULL, message TEXT, screenshot_path TEXT,
			dom_snapshot_path TEXT, raw_json TEXT, created_at TEXT NOT NULL
		)`,
	}
	for _, statement := range legacy {
		if _, err := d.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	for table, columns := range map[string][]string{
		"publish_jobs": {"trace_id", "idempotency_key", "cancel_requested_at"},
		"job_events":   {"trace_id", "request_id", "task_id", "duration_ms", "error_code", "error_class", "retryable", "schema_version"},
	} {
		for _, column := range columns {
			if !columnExists(t, d, table, column) {
				t.Fatalf("legacy migration missing %s.%s", table, column)
			}
		}
	}
	if _, err := d.Exec(`INSERT INTO job_events (job_id, level, event_type, created_at) VALUES (999999, 'info', 'orphan.test', CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("legacy schema accepted orphan job event after migration")
	}
}

func containsJSONKey(encoded []byte, key string) bool {
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return false
	}
	_, ok := object[key]
	return ok
}

func TestMigrateRollsBackAdditiveChangesOnIndexFailure(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if _, err := d.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE browser_profiles (
		id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL, profile_dir TEXT NOT NULL,
		browser_type TEXT NOT NULL, fingerprint_seed INTEGER NOT NULL, lang TEXT NOT NULL,
		accept_lang TEXT NOT NULL, timezone TEXT NOT NULL, proxy_id INTEGER, cookie_encrypted TEXT,
		cookie_import_state TEXT NOT NULL, last_cdp_port INTEGER, last_cdp_url TEXT,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(); err == nil {
		t.Fatal("expected migration failure for incompatible legacy accounts table")
	}
	if columnExists(t, d, "browser_profiles", "fingerprint_config") {
		t.Fatal("additive column survived failed migration")
	}
	var platforms int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='platforms'`).Scan(&platforms); err != nil {
		t.Fatal(err)
	}
	if platforms != 0 {
		t.Fatal("new tables survived failed migration")
	}
}

func TestMigrateRekeysPublishPlanTargetsFromAccountToAsset(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if _, err := d.Exec(`CREATE TABLE publish_plan_targets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		plan_id INTEGER NOT NULL,
		account_id INTEGER NOT NULL,
		asset_id INTEGER NOT NULL,
		platform_code TEXT NOT NULL,
		variant_id INTEGER,
		settings_json TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL DEFAULT 'pending',
		publish_job_id INTEGER,
		error_code TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (plan_id, account_id)
	)`); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := d.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='publish_plan_targets'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	normalized := strings.NewReplacer(" ", "", "\n", "", "\t", "").Replace(strings.ToLower(schema))
	if !strings.Contains(normalized, "unique(plan_id,asset_id)") || strings.Contains(normalized, "unique(plan_id,account_id)") {
		t.Fatalf("publish_plan_targets schema=%s", schema)
	}
}

func columnExists(t *testing.T, d *db.DB, table, column string) bool {
	t.Helper()
	var found int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&found); err != nil {
		t.Fatal(err)
	}
	return found == 1
}

func TestOpenUsesFastDurablePragmas(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// WAL + NORMAL is the write profile this app relies on: a commit does not
	// fsync, but WAL still protects against process crashes. cache_size is KiB
	// when negative, and temp_store=2 is MEMORY.
	for pragma, want := range map[string]string{
		"synchronous":  "1",
		"cache_size":   "-20000",
		"temp_store":   "2",
		"journal_mode": "wal",
	} {
		var value string
		if err := database.QueryRow("PRAGMA " + pragma).Scan(&value); err != nil {
			t.Fatalf("read pragma %s: %v", pragma, err)
		}
		if value != want {
			t.Fatalf("pragma %s=%q, want %q", pragma, value, want)
		}
	}
}

// 多个进程/协程同时打开同一个数据目录时必须都能完成迁移。默认的 deferred
// 事务在读锁升级写锁时不走 busy_timeout，直接返回 SQLITE_BUSY——实测 4 个
// 并发里有 3 个失败。DSN 里的 _txlock=immediate 让事务开头就取写锁。
func TestMigrateIsSafeUnderConcurrentOpen(t *testing.T) {
	dir := t.TempDir()
	const workers = 4
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			database, err := db.Open(dir)
			if err != nil {
				errs[slot] = err
				return
			}
			defer database.Close()
			errs[slot] = database.Migrate()
		}(index)
	}
	wg.Wait()
	for slot, err := range errs {
		if err != nil {
			t.Fatalf("worker %d failed: %v", slot, err)
		}
	}
}
