package turbopush_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/importers/turbopush"
	_ "modernc.org/sqlite"
)

func openTarget(t *testing.T) *db.DB {
	t.Helper()
	target, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	if err := target.Migrate(); err != nil {
		t.Fatal(err)
	}
	return target
}

// createSource deliberately includes opaque free-form values. A safe importer
// must work from the narrow metadata allowlist without copying those values.
func createSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.db")
	source, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })

	statements := []string{
		`CREATE TABLE platforms (id INTEGER PRIMARY KEY, name TEXT, plat_type TEXT NOT NULL)`,
		`CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT, avatar TEXT, platform_id INTEGER NOT NULL, remark TEXT)`,
		"CREATE TABLE plat_sets (id INTEGER PRIMARY KEY, name TEXT, description TEXT, setting TEXT, platform_id INTEGER NOT NULL, enable INTEGER, `default` INTEGER)",
		`CREATE TABLE articles (id INTEGER PRIMARY KEY, publish_data TEXT, status INTEGER, publish_status INTEGER, publish_type INTEGER)`,
		`CREATE TABLE publish_records (id INTEGER PRIMARY KEY, content_id INTEGER)`,
		`INSERT INTO platforms (id, name, plat_type) VALUES
			(1, 'TikTok', 'tiktok'),
			(2, 'Toutiao', 'toutiaohao'),
			(3, 'Weishi', 'weishi'),
			(4, 'XHS', 'xiaohongshu')`,
		`INSERT INTO accounts (id, name, avatar, platform_id, remark) VALUES
			(11, 'TikTok account', '', 1, 'allowed remark'),
			(12, 'Toutiao account', '', 2, ''),
			(13, 'Weishi account', '', 3, ''),
			(14, 'XHS account', '', 4, '')`,
		"INSERT INTO plat_sets (id, name, description, setting, platform_id, enable, `default`) VALUES (21, 'TikTok defaults', '', '{\"api_key\":\"synthetic-sentinel\",\"location\":\"CN\"}', 1, 1, 1)",
		`INSERT INTO articles (id, publish_data, status, publish_status, publish_type) VALUES (31, '{"title":"Imported","content":"Body","desc":"Caption"}', 1, 1, 0)`,
		`INSERT INTO publish_records (id, content_id) VALUES (41, 31)`,
	}
	for _, statement := range statements {
		if _, err := source.Exec(statement); err != nil {
			t.Fatalf("source fixture: %v", err)
		}
	}
	return path
}

func count(t *testing.T, database *db.DB, query string, args ...any) int {
	t.Helper()
	var value int
	if err := database.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDetectSource(t *testing.T) {
	live := createSource(t)
	kind, err := turbopush.DetectSource(live)
	if err != nil {
		t.Fatal(err)
	}
	if kind != turbopush.SourceLiveDB {
		t.Fatalf("kind=%q want %q", kind, turbopush.SourceLiveDB)
	}
	kind, err = turbopush.DetectSource(filepath.Dir(live))
	if err != nil {
		t.Fatal(err)
	}
	if kind != turbopush.SourceLiveDB {
		t.Fatalf("directory kind=%q want %q", kind, turbopush.SourceLiveDB)
	}

	encrypted := filepath.Join(t.TempDir(), "backup.tp")
	if err := os.WriteFile(encrypted, []byte("opaque export"), 0600); err != nil {
		t.Fatal(err)
	}
	kind, err = turbopush.DetectSource(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if kind != turbopush.SourceEncryptedExport {
		t.Fatalf("kind=%q want %q", kind, turbopush.SourceEncryptedExport)
	}

	unsupported := filepath.Join(t.TempDir(), "other.db")
	other, err := sql.Open("sqlite", unsupported)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Exec(`CREATE TABLE something_else (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	_ = other.Close()
	kind, err = turbopush.DetectSource(unsupported)
	if err != nil {
		t.Fatal(err)
	}
	if kind != turbopush.SourceUnsupported {
		t.Fatalf("kind=%q want %q", kind, turbopush.SourceUnsupported)
	}
}

func TestImportRejectsEncryptedExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.tp")
	if err := os.WriteFile(path, []byte("opaque export"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := turbopush.Import(path, openTarget(t), t.TempDir())
	if !errors.Is(err, turbopush.ErrEncryptedExport) {
		t.Fatalf("error=%v want ErrEncryptedExport", err)
	}
}

func TestImportAllowlistMappingAndIdempotency(t *testing.T) {
	source := createSource(t)
	target := openTarget(t)
	dataDir := t.TempDir()

	first, err := turbopush.Import(filepath.Dir(source), target, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Accounts != 4 {
		t.Fatalf("accounts=%d want 4", first.Accounts)
	}
	if first.Records != 0 {
		t.Fatalf("records=%d want 0", first.Records)
	}
	if len(first.SkippedReasons) == 0 {
		t.Fatal("expected explicit skip reasons")
	}
	if !strings.Contains(strings.Join(first.SkippedReasons, "\n"), "publish_record 41") {
		t.Fatalf("missing publish record skip reason: %v", first.SkippedReasons)
	}

	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%'`); got != 4 {
		t.Fatalf("imported accounts=%d want 4", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%' AND platform_code='tiktok' AND source_account_id=11`); got != 1 {
		t.Fatalf("tiktok mapping count=%d want 1", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%' AND platform_code='toutiao' AND source_account_id=12`); got != 1 {
		t.Fatalf("toutiao mapping count=%d want 1", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%' AND platform_code='weishi' AND source_account_id=13`); got != 1 {
		t.Fatalf("weishi mapping count=%d want 1", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%' AND platform_code IN ('douyin','wechat_video')`); got != 0 {
		t.Fatalf("incorrect platform coercions imported=%d", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%' AND login_state <> 'unknown'`); got != 0 {
		t.Fatalf("inherited login states=%d", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM browser_profiles bp JOIN accounts a ON a.id=bp.account_id WHERE a.source LIKE 'turbopush:%' AND bp.cookie_encrypted IS NOT NULL`); got != 0 {
		t.Fatalf("imported cookie values=%d", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM platform_settings WHERE source LIKE 'turbopush:%' AND source_setting_id=21 AND platform_code='tiktok'`); got != 1 {
		t.Fatalf("settings=%d want 1", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM platform_settings WHERE source LIKE 'turbopush:%' AND settings_json <> '{}'`); got != 0 {
		t.Fatalf("unsafe source settings copied=%d", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM contents WHERE source LIKE 'turbopush:%' AND source_content_id=31 AND status='draft'`); got != 1 {
		t.Fatalf("contents=%d want 1 safe draft", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM contents WHERE source LIKE 'turbopush:%' AND raw_publish_data IS NOT NULL`); got != 0 {
		t.Fatalf("raw source payloads copied=%d", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM publish_records WHERE account_id=0 OR content_id=0 OR platform_code=''`); got != 0 {
		t.Fatalf("invalid publish records=%d", got)
	}

	if _, err := turbopush.Import(source, target, dataDir); err != nil {
		t.Fatal(err)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%'`); got != 4 {
		t.Fatalf("accounts after second import=%d want 4", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM browser_profiles bp JOIN accounts a ON a.id=bp.account_id WHERE a.source LIKE 'turbopush:%'`); got != 0 {
		t.Fatalf("legacy browser profiles after second import=%d want 0", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM platform_settings WHERE source LIKE 'turbopush:%'`); got != 1 {
		t.Fatalf("settings after second import=%d want 1", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM contents WHERE source LIKE 'turbopush:%'`); got != 1 {
		t.Fatalf("contents after second import=%d want 1", got)
	}
}

func TestReimportPreservesAlphaOwnedFields(t *testing.T) {
	source := createSource(t)
	target := openTarget(t)
	dataDir := t.TempDir()
	if _, err := turbopush.Import(source, target, dataDir); err != nil {
		t.Fatal(err)
	}

	var sourceID string
	if err := target.QueryRow(`SELECT source FROM accounts WHERE source_account_id=11`).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(`UPDATE accounts SET name=?, login_state=?, login_time=?, avatar=?, remark=? WHERE source=? AND source_account_id=?`,
		"Alpha local account", "logged_in", "2026-07-16T08:00:00Z", "local-avatar.png", "local remark", sourceID, 11); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(`UPDATE platform_settings SET name=?, settings_json=?, enabled=? WHERE source=? AND source_setting_id=?`,
		"Alpha local settings", `{"theme":"local"}`, 0, sourceID, 21); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(`UPDATE contents SET body=?, status=?, publish_count=? WHERE source=? AND source_content_id=?`,
		"Alpha local body", "published", 7, sourceID, 31); err != nil {
		t.Fatal(err)
	}

	if _, err := turbopush.Import(source, target, dataDir); err != nil {
		t.Fatal(err)
	}

	var accountName, loginState, loginTime, avatar, remark string
	if err := target.QueryRow(`SELECT name, login_state, login_time, avatar, remark FROM accounts WHERE source=? AND source_account_id=?`, sourceID, 11).
		Scan(&accountName, &loginState, &loginTime, &avatar, &remark); err != nil {
		t.Fatal(err)
	}
	if accountName != "Alpha local account" || loginState != "logged_in" || loginTime != "2026-07-16T08:00:00Z" || avatar != "local-avatar.png" || remark != "local remark" {
		t.Fatalf("account fields overwritten: name=%q state=%q time=%q avatar=%q remark=%q", accountName, loginState, loginTime, avatar, remark)
	}

	var settingsName, settingsJSON string
	var settingsEnabled int
	if err := target.QueryRow(`SELECT name, settings_json, enabled FROM platform_settings WHERE source=? AND source_setting_id=?`, sourceID, 21).
		Scan(&settingsName, &settingsJSON, &settingsEnabled); err != nil {
		t.Fatal(err)
	}
	if settingsName != "Alpha local settings" || settingsJSON != `{"theme":"local"}` || settingsEnabled != 0 {
		t.Fatalf("settings overwritten: name=%q json=%q enabled=%d", settingsName, settingsJSON, settingsEnabled)
	}

	var body, status string
	var publishCount int
	if err := target.QueryRow(`SELECT body, status, publish_count FROM contents WHERE source=? AND source_content_id=?`, sourceID, 31).
		Scan(&body, &status, &publishCount); err != nil {
		t.Fatal(err)
	}
	if body != "Alpha local body" || status != "published" || publishCount != 7 {
		t.Fatalf("content fields overwritten: body=%q status=%q publish_count=%d", body, status, publishCount)
	}

	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source=?`, sourceID); got != 4 {
		t.Fatalf("accounts after reimport=%d want 4", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM browser_profiles bp JOIN accounts a ON a.id=bp.account_id WHERE a.source=?`, sourceID); got != 0 {
		t.Fatalf("legacy browser profiles after reimport=%d want 0", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM platform_settings WHERE source=?`, sourceID); got != 1 {
		t.Fatalf("settings after reimport=%d want 1", got)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM contents WHERE source=?`, sourceID); got != 1 {
		t.Fatalf("contents after reimport=%d want 1", got)
	}
}

func TestImportRollsBackAllTargetWrites(t *testing.T) {
	source := createSource(t)
	target := openTarget(t)
	if _, err := target.Exec(`CREATE TRIGGER fail_turbopush_content BEFORE INSERT ON contents WHEN NEW.source LIKE 'turbopush:%' BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
		t.Fatal(err)
	}

	result, err := turbopush.Import(source, target, t.TempDir())
	if err == nil {
		t.Fatal("expected import failure")
	}
	if result == nil {
		t.Fatal("expected partial result for diagnostics")
	}
	if result.Accounts != 0 || result.Settings != 0 || result.Contents != 0 || result.Records != 0 {
		t.Fatalf("rolled-back result reported committed rows: %+v", result)
	}
	for _, table := range []string{"accounts", "platform_settings", "contents"} {
		if got := count(t, target, `SELECT COUNT(*) FROM `+table+` WHERE source LIKE 'turbopush:%'`); got != 0 {
			t.Fatalf("%s rows after rollback=%d", table, got)
		}
	}
}

func TestImportNamespacesIndependentSourceDatabases(t *testing.T) {
	firstSource := createSource(t)
	secondSource := createSource(t)
	target := openTarget(t)
	if _, err := turbopush.Import(firstSource, target, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := turbopush.Import(secondSource, target, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := count(t, target, `SELECT COUNT(*) FROM accounts WHERE source LIKE 'turbopush:%'`); got != 8 {
		t.Fatalf("independent sources collided: accounts=%d want 8", got)
	}
	if got := count(t, target, `SELECT COUNT(DISTINCT source) FROM accounts WHERE source LIKE 'turbopush:%'`); got != 2 {
		t.Fatalf("source namespaces=%d want 2", got)
	}
}
