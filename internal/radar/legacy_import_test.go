package radar

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

const legacyTikHubCallsSchema = `
CREATE TABLE tikhub_api_calls (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id TEXT NOT NULL,
  platform_code TEXT NOT NULL,
  endpoint_id TEXT NOT NULL,
  capability TEXT NOT NULL,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  parameters_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'running',
  error_code TEXT NOT NULL DEFAULT '',
  provider_code TEXT NOT NULL DEFAULT '',
  provider_message TEXT NOT NULL DEFAULT '',
  item_count INTEGER NOT NULL DEFAULT 0,
  response_bytes INTEGER NOT NULL DEFAULT 0,
  raw_path TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);`

func TestImportLegacyTrendRadarReplacesDataAndRewritesPaths(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	writeRadarTestFile(t, filepath.Join(sourceDir, "tikhub", "trend_radar.json"), `{"version":1,"niches":[{"id":"old-project"}]}`)
	writeRadarTestFile(t, filepath.Join(sourceDir, "tikhub", "watchlist.json"), `{"users":[]}`)
	writeRadarTestFile(t, filepath.Join(sourceDir, "tikhub", "calls", "2026-07-25", "1_xhs.json"), `{"call_id":1}`)
	writeRadarTestFile(t, filepath.Join(sourceDir, "tikhub", "covers", "cover.img"), "image")
	writeRadarTestFile(t, filepath.Join(sourceDir, "trend-radar", "video-search", "1.json"), `{"call_id":1}`)
	writeRadarTestFile(t, filepath.Join(sourceDir, "trend-radar", "bin", "worker"), "must-not-copy")
	writeRadarTestFile(t, filepath.Join(sourceDir, "trend-radar", "logs", "worker.log"), "must-not-copy")
	writeRadarTestFile(t, filepath.Join(sourceDir, "trend-radar", "worker.lock", "owner"), "must-not-copy")

	sourceDB := openRadarTestDB(t, filepath.Join(sourceDir, "app.db"))
	insertRadarTestCall(t, sourceDB, 1, "xhs", filepath.Join(sourceDir, "tikhub", "calls", "2026-07-25", "1_xhs.json"))
	insertRadarTestCall(t, sourceDB, 2, "douyin", "")
	sourceDB.Close()

	writeRadarTestFile(t, filepath.Join(targetDir, "tikhub", "trend_radar.json"), `{"version":1,"niches":[{"id":"new-project"}]}`)
	writeRadarTestFile(t, filepath.Join(targetDir, "trend-radar", "video-search", "stale.json"), `{"stale":true}`)
	targetDB := openRadarTestDB(t, filepath.Join(targetDir, "app.db"))
	defer targetDB.Close()
	insertRadarTestCall(t, targetDB, 99, "youtube", filepath.Join(targetDir, "tikhub", "calls", "stale.json"))

	result, err := ImportLegacyTrendRadar(context.Background(), sourceDir, targetDir, targetDB)
	if err != nil {
		t.Fatalf("ImportLegacyTrendRadar() error = %v", err)
	}
	if result.ImportedCalls != 2 {
		t.Fatalf("ImportedCalls = %d, want 2", result.ImportedCalls)
	}
	if result.ImportedFiles != 5 {
		t.Fatalf("ImportedFiles = %d, want 5", result.ImportedFiles)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "trend-radar", "video-search", "stale.json")); !os.IsNotExist(err) {
		t.Fatalf("stale trend file was not removed: %v", err)
	}
	for _, forbidden := range []string{
		filepath.Join(targetDir, "trend-radar", "bin"),
		filepath.Join(targetDir, "trend-radar", "logs"),
		filepath.Join(targetDir, "trend-radar", "worker.lock"),
	} {
		if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
			t.Fatalf("forbidden runtime path copied: %s", forbidden)
		}
	}
	raw, err := os.ReadFile(filepath.Join(targetDir, "tikhub", "trend_radar.json"))
	if err != nil || !strings.Contains(string(raw), "old-project") {
		t.Fatalf("legacy config not installed: raw=%q err=%v", raw, err)
	}
	backupRaw, err := os.ReadFile(filepath.Join(result.BackupDir, "tikhub", "trend_radar.json"))
	if err != nil || !strings.Contains(string(backupRaw), "new-project") {
		t.Fatalf("target config backup missing: raw=%q err=%v", backupRaw, err)
	}
	if _, err := os.Stat(filepath.Join(result.BackupDir, "tikhub_api_calls.json")); err != nil {
		t.Fatalf("database backup missing: %v", err)
	}

	rows, err := targetDB.Query(`SELECT id, raw_path FROM tikhub_api_calls ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id int64
		var rawPath string
		if err := rows.Scan(&id, &rawPath); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{string(rune('0' + id)), rawPath}, "|"))
	}
	wantFirst := "1|" + filepath.Join(targetDir, "tikhub", "calls", "2026-07-25", "1_xhs.json")
	if len(got) != 2 || got[0] != wantFirst || got[1] != "2|" {
		t.Fatalf("imported rows = %#v, want [%q %q]", got, wantFirst, "2|")
	}

	second, err := ImportLegacyTrendRadar(context.Background(), sourceDir, targetDir, targetDB)
	if err != nil {
		t.Fatalf("second import error = %v", err)
	}
	if second.ImportedCalls != 2 {
		t.Fatalf("second ImportedCalls = %d, want 2", second.ImportedCalls)
	}
	var count int
	if err := targetDB.QueryRow(`SELECT COUNT(*) FROM tikhub_api_calls`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rows after repeated import = %d, err=%v", count, err)
	}
}

func TestImportLegacyTrendRadarRejectsSymlink(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	writeRadarTestFile(t, outside, `{"private":true}`)
	if err := os.MkdirAll(filepath.Join(sourceDir, "tikhub", "calls"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(sourceDir, "tikhub", "calls", "leak.json")); err != nil {
		t.Fatal(err)
	}
	sourceDB := openRadarTestDB(t, filepath.Join(sourceDir, "app.db"))
	sourceDB.Close()
	targetDB := openRadarTestDB(t, filepath.Join(targetDir, "app.db"))
	defer targetDB.Close()

	if _, err := ImportLegacyTrendRadar(context.Background(), sourceDir, targetDir, targetDB); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error = %v, want symbolic link rejection", err)
	}
}

func openRadarTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(legacyTikHubCallsSchema); err != nil {
		database.Close()
		t.Fatal(err)
	}
	return database
}

func insertRadarTestCall(t *testing.T, database *sql.DB, id int64, platform, rawPath string) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO tikhub_api_calls (
		id, trace_id, platform_code, endpoint_id, capability, method, path, parameters_json,
		status, error_code, provider_code, provider_message, item_count, response_bytes,
		raw_path, started_at, finished_at, duration_ms, created_at
	) VALUES (?, ?, ?, ?, ?, 'GET', '/test', '{}', 'succeeded', '', '', '', 1, 10, ?, ?, ?, 5, ?)`,
		id, "trace", platform, platform+".search", "keyword_search", rawPath,
		"2026-07-25T00:00:00Z", "2026-07-25T00:00:01Z", "2026-07-25T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
}

func writeRadarTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
