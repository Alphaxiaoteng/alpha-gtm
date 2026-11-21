package radar

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type LegacyTrendRadarImportResult struct {
	ImportedCalls int    `json:"imported_calls"`
	ImportedFiles int    `json:"imported_files"`
	BackupDir     string `json:"backup_dir"`
}

type legacyTikHubCall struct {
	ID              int64  `json:"id"`
	TraceID         string `json:"trace_id"`
	PlatformCode    string `json:"platform_code"`
	EndpointID      string `json:"endpoint_id"`
	Capability      string `json:"capability"`
	Method          string `json:"method"`
	Path            string `json:"path"`
	ParametersJSON  string `json:"parameters_json"`
	Status          string `json:"status"`
	ErrorCode       string `json:"error_code"`
	ProviderCode    string `json:"provider_code"`
	ProviderMessage string `json:"provider_message"`
	ItemCount       int64  `json:"item_count"`
	ResponseBytes   int64  `json:"response_bytes"`
	RawPath         string `json:"raw_path"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at"`
	DurationMS      int64  `json:"duration_ms"`
	CreatedAt       string `json:"created_at"`
}

var legacyTrendRadarPaths = []string{
	filepath.Join("tikhub", "calls"),
	filepath.Join("tikhub", "covers"),
	filepath.Join("tikhub", "trend_radar.json"),
	filepath.Join("tikhub", "watchlist.json"),
	filepath.Join("trend-radar", "schedules.json"),
	filepath.Join("trend-radar", "rss"),
	filepath.Join("trend-radar", "video-search"),
	filepath.Join("trend-radar", "creators"),
	filepath.Join("trend-radar", "video-deconstruct"),
}

// ImportLegacyTrendRadar replaces only Trend Radar assets and the matching
// TikHub call index. Browser profiles, provider settings, credentials, worker
// binaries, logs and locks are deliberately outside the allowlist.
func ImportLegacyTrendRadar(ctx context.Context, sourceDataDir, targetDataDir string, targetDB *sql.DB) (LegacyTrendRadarImportResult, error) {
	var result LegacyTrendRadarImportResult
	if targetDB == nil {
		return result, fmt.Errorf("target database is required")
	}
	sourceRoot, targetRoot, err := validateLegacyImportRoots(sourceDataDir, targetDataDir)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	sourceDB, err := openImmutableSQLite(filepath.Join(sourceRoot, "app.db"))
	if err != nil {
		return result, fmt.Errorf("open legacy database: %w", err)
	}
	defer sourceDB.Close()
	if err := quickCheckSQLite(ctx, sourceDB); err != nil {
		return result, fmt.Errorf("check legacy database: %w", err)
	}
	calls, err := loadLegacyTikHubCalls(ctx, sourceDB)
	if err != nil {
		return result, fmt.Errorf("read legacy TikHub calls: %w", err)
	}
	for index := range calls {
		calls[index].RawPath, err = rewriteLegacyRawPath(sourceRoot, targetRoot, calls[index].RawPath)
		if err != nil {
			return result, fmt.Errorf("call %d: %w", calls[index].ID, err)
		}
	}

	stageRoot, err := os.MkdirTemp(targetRoot, ".trend-radar-import-")
	if err != nil {
		return result, fmt.Errorf("create trend import stage: %w", err)
	}
	defer os.RemoveAll(stageRoot)
	if err := os.Chmod(stageRoot, 0o700); err != nil {
		return result, err
	}
	for _, relative := range legacyTrendRadarPaths[:4] {
		if err := copyLegacyRadarPath(ctx, sourceRoot, stageRoot, relative, &result.ImportedFiles); err != nil {
			return result, err
		}
	}
	if err := copyLegacyTrendTree(ctx, sourceRoot, stageRoot, &result.ImportedFiles); err != nil {
		return result, err
	}

	backupRoot := filepath.Join(targetRoot, "backups", "trend-radar-migration-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return result, fmt.Errorf("create trend backup: %w", err)
	}
	if err := os.Chmod(backupRoot, 0o700); err != nil {
		return result, err
	}
	result.BackupDir = backupRoot
	existingCalls, err := loadLegacyTikHubCalls(ctx, targetDB)
	if err != nil {
		return result, fmt.Errorf("read current TikHub calls: %w", err)
	}
	if err := writePrivateJSON(filepath.Join(backupRoot, "tikhub_api_calls.json"), existingCalls); err != nil {
		return result, fmt.Errorf("backup current TikHub calls: %w", err)
	}

	tx, err := targetDB.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin trend import: %w", err)
	}
	defer tx.Rollback()
	if err := replaceTikHubCalls(ctx, tx, calls); err != nil {
		return result, err
	}
	moved, err := installLegacyRadarFiles(stageRoot, targetRoot, backupRoot)
	if err != nil {
		restoreLegacyRadarFiles(targetRoot, backupRoot, moved)
		return result, err
	}
	if err := tx.Commit(); err != nil {
		restoreLegacyRadarFiles(targetRoot, backupRoot, moved)
		return result, fmt.Errorf("commit trend import: %w", err)
	}
	result.ImportedCalls = len(calls)
	return result, nil
}

func validateLegacyImportRoots(sourceDataDir, targetDataDir string) (string, string, error) {
	sourceRoot, err := filepath.Abs(strings.TrimSpace(sourceDataDir))
	if err != nil || strings.TrimSpace(sourceDataDir) == "" {
		return "", "", fmt.Errorf("invalid source data directory")
	}
	targetRoot, err := filepath.Abs(strings.TrimSpace(targetDataDir))
	if err != nil || strings.TrimSpace(targetDataDir) == "" {
		return "", "", fmt.Errorf("invalid target data directory")
	}
	if sourceRoot == targetRoot {
		return "", "", fmt.Errorf("source and target data directories must differ")
	}
	info, err := os.Lstat(sourceRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("legacy source data directory is not a regular directory")
	}
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		return "", "", fmt.Errorf("create target data directory: %w", err)
	}
	return sourceRoot, targetRoot, nil
}

func openImmutableSQLite(path string) (*sql.DB, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("legacy app.db is not a regular file")
	}
	dsn := &url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("immutable", "1")
	query.Set("mode", "ro")
	dsn.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func quickCheckSQLite(ctx context.Context, database *sql.DB) error {
	var state string
	if err := database.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&state); err != nil {
		return err
	}
	if state != "ok" {
		return fmt.Errorf("quick_check failed")
	}
	return nil
}

func loadLegacyTikHubCalls(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]legacyTikHubCall, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT
		id, trace_id, platform_code, endpoint_id, capability, method, path, parameters_json,
		status, error_code, provider_code, provider_message, item_count, response_bytes,
		raw_path, started_at, finished_at, duration_ms, created_at
		FROM tikhub_api_calls ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	calls := make([]legacyTikHubCall, 0)
	for rows.Next() {
		var call legacyTikHubCall
		if err := rows.Scan(
			&call.ID, &call.TraceID, &call.PlatformCode, &call.EndpointID, &call.Capability,
			&call.Method, &call.Path, &call.ParametersJSON, &call.Status, &call.ErrorCode,
			&call.ProviderCode, &call.ProviderMessage, &call.ItemCount, &call.ResponseBytes,
			&call.RawPath, &call.StartedAt, &call.FinishedAt, &call.DurationMS, &call.CreatedAt,
		); err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, rows.Err()
}

func rewriteLegacyRawPath(sourceRoot, targetRoot, rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("invalid raw snapshot path")
	}
	relative, err := filepath.Rel(sourceRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("raw snapshot path is outside the legacy data directory")
	}
	allowedPrefix := filepath.Join("tikhub", "calls") + string(os.PathSeparator)
	if !strings.HasPrefix(relative, allowedPrefix) {
		return "", fmt.Errorf("raw snapshot path is outside the TikHub call archive")
	}
	if info, err := os.Lstat(absolute); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("raw snapshot is not a regular file")
	}
	return filepath.Join(targetRoot, relative), nil
}

func copyLegacyRadarPath(ctx context.Context, sourceRoot, stageRoot, relative string, count *int) error {
	source := filepath.Join(sourceRoot, relative)
	if _, err := os.Lstat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return copyLegacyRadarEntry(ctx, source, filepath.Join(stageRoot, relative), count)
}

func copyLegacyTrendTree(ctx context.Context, sourceRoot, stageRoot string, count *int) error {
	source := filepath.Join(sourceRoot, "trend-radar")
	if _, err := os.Lstat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		top := strings.Split(relative, string(os.PathSeparator))[0]
		if top == "bin" || top == "logs" || strings.HasSuffix(top, ".lock") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link is not allowed in trend data: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() || strings.HasSuffix(entry.Name(), ".tmp") {
			return nil
		}
		target := filepath.Join(stageRoot, "trend-radar", relative)
		if err := copyPrivateFile(path, target); err != nil {
			return err
		}
		*count++
		return nil
	})
}

func copyLegacyRadarEntry(ctx context.Context, source, target string, count *int) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symbolic link is not allowed in trend data: %s", source)
	}
	if info.Mode().IsRegular() {
		if err := copyPrivateFile(source, target); err != nil {
			return err
		}
		*count++
		return nil
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link is not allowed in trend data: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if err := copyPrivateFile(path, filepath.Join(target, relative)); err != nil {
			return err
		}
		*count++
		return nil
	})
}

func copyPrivateFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("trend source is not a regular file: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writePrivateJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func replaceTikHubCalls(ctx context.Context, tx *sql.Tx, calls []legacyTikHubCall) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM tikhub_api_calls`); err != nil {
		return fmt.Errorf("clear current TikHub calls: %w", err)
	}
	for _, call := range calls {
		_, err := tx.ExecContext(ctx, `INSERT INTO tikhub_api_calls (
			id, trace_id, platform_code, endpoint_id, capability, method, path, parameters_json,
			status, error_code, provider_code, provider_message, item_count, response_bytes,
			raw_path, started_at, finished_at, duration_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			call.ID, call.TraceID, call.PlatformCode, call.EndpointID, call.Capability, call.Method,
			call.Path, call.ParametersJSON, call.Status, call.ErrorCode, call.ProviderCode,
			call.ProviderMessage, call.ItemCount, call.ResponseBytes, call.RawPath, call.StartedAt,
			call.FinishedAt, call.DurationMS, call.CreatedAt)
		if err != nil {
			return fmt.Errorf("import TikHub call %d: %w", call.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name = 'tikhub_api_calls'`); err != nil {
		return fmt.Errorf("reset TikHub call sequence: %w", err)
	}
	if len(calls) > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sqlite_sequence(name, seq) SELECT 'tikhub_api_calls', MAX(id) FROM tikhub_api_calls`); err != nil {
			return fmt.Errorf("set TikHub call sequence: %w", err)
		}
	}
	return nil
}

func installLegacyRadarFiles(stageRoot, targetRoot, backupRoot string) ([]string, error) {
	moved := make([]string, 0, len(legacyTrendRadarPaths))
	for _, relative := range legacyTrendRadarPaths {
		// Record the path before either rename so a failure after the backup
		// move still restores the previous destination.
		moved = append(moved, relative)
		target := filepath.Join(targetRoot, relative)
		backup := filepath.Join(backupRoot, relative)
		if _, err := os.Lstat(target); err == nil {
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				return moved, err
			}
			if err := os.Rename(target, backup); err != nil {
				return moved, fmt.Errorf("backup current trend path %s: %w", relative, err)
			}
		} else if !os.IsNotExist(err) {
			return moved, err
		}
		staged := filepath.Join(stageRoot, relative)
		if _, err := os.Lstat(staged); err == nil {
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return moved, err
			}
			if err := os.Rename(staged, target); err != nil {
				return moved, fmt.Errorf("install legacy trend path %s: %w", relative, err)
			}
		} else if !os.IsNotExist(err) {
			return moved, err
		}
	}
	return moved, nil
}

func restoreLegacyRadarFiles(targetRoot, backupRoot string, moved []string) {
	for index := len(moved) - 1; index >= 0; index-- {
		relative := moved[index]
		target := filepath.Join(targetRoot, relative)
		backup := filepath.Join(backupRoot, relative)
		_ = os.RemoveAll(target)
		if _, err := os.Lstat(backup); err == nil {
			_ = os.MkdirAll(filepath.Dir(target), 0o700)
			_ = os.Rename(backup, target)
		}
	}
}
