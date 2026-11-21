package turbopush

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"alpha-nexus-egolite/internal/db"
)

type SourceKind string

const (
	SourceLiveDB          SourceKind = "live-db"
	SourceEncryptedExport SourceKind = "encrypted-export"
	SourceUnsupported     SourceKind = "unsupported"
)

var (
	ErrEncryptedExport   = errors.New("Turbo Push encrypted export is not importable")
	ErrUnsupportedSource = errors.New("unsupported Turbo Push source")
)

const (
	sqliteHeader = "SQLite format 3\x00"
	sourcePrefix = "turbopush"
)

// platformMap contains only semantically equivalent Alpha-Nexus platforms.
// Unsupported platforms are reported and skipped rather than coerced into a
// different network.
var platformMap = map[string]string{
	"acfun":        "acfun",
	"baijiahao":    "baijiahao",
	"bilibili":     "bilibili",
	"csdn":         "csdn",
	"douyin":       "douyin",
	"facebook":     "facebook",
	"instagram":    "instagram",
	"jianshu":      "jianshu",
	"juejin":       "juejin",
	"kuaishou":     "kuaishou",
	"omtencent":    "omtencent",
	"pdd":          "pdd",
	"sina":         "sina",
	"tiktok":       "tiktok",
	"toutiao":      "toutiao",
	"toutiaohao":   "toutiao",
	"twitter":      "x",
	"wechat":       "wechat_mp",
	"wechat-mp":    "wechat_mp",
	"wechat-video": "wechat_video",
	"wechat_mp":    "wechat_mp",
	"wechat_video": "wechat_video",
	"weishi":       "weishi",
	"x":            "x",
	"xhs":          "xhs",
	"xiaohongshu":  "xhs",
	"youtube":      "youtube",
	"zhihu":        "zhihu",
}

var requiredSourceSchema = map[string][]string{
	"platforms":       {"id", "plat_type"},
	"accounts":        {"id", "name", "platform_id"},
	"plat_sets":       {"id", "name", "description", "platform_id", "enable", "default"},
	"articles":        {"id", "publish_data"},
	"publish_records": {"id", "content_id"},
}

type ImportResult struct {
	Platforms      int
	Accounts       int
	Settings       int
	Contents       int
	Records        int
	Media          int
	Skipped        int
	SkippedReasons []string
	Errors         []string
}

// DetectSource identifies supported live data.db files without reading row
// values. A .tp export is always fail-closed: Alpha-Nexus does not decrypt it.
func DetectSource(sourcePath string) (SourceKind, error) {
	resolvedPath, info, err := resolveSourcePath(sourcePath)
	if err != nil {
		return SourceUnsupported, fmt.Errorf("inspect source: %w", err)
	}
	if strings.EqualFold(filepath.Ext(resolvedPath), ".tp") {
		return SourceEncryptedExport, nil
	}
	if info.IsDir() {
		return SourceUnsupported, nil
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return SourceUnsupported, fmt.Errorf("open source header: %w", err)
	}
	header := make([]byte, len(sqliteHeader))
	n, readErr := io.ReadFull(file, header)
	closeErr := file.Close()
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		return SourceUnsupported, fmt.Errorf("read source header: %w", readErr)
	}
	if closeErr != nil {
		return SourceUnsupported, fmt.Errorf("close source header: %w", closeErr)
	}
	if n != len(sqliteHeader) || string(header) != sqliteHeader {
		return SourceUnsupported, nil
	}

	source, err := openReadOnly(resolvedPath)
	if err != nil {
		return SourceUnsupported, err
	}
	defer source.Close()
	valid, err := hasRequiredSchema(source)
	if err != nil {
		return SourceUnsupported, fmt.Errorf("inspect source schema: %w", err)
	}
	if !valid {
		return SourceUnsupported, nil
	}
	return SourceLiveDB, nil
}

func resolveSourcePath(sourcePath string) (string, os.FileInfo, error) {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", nil, err
	}
	if !info.IsDir() {
		return sourcePath, info, nil
	}
	candidate := filepath.Join(sourcePath, "data.db")
	candidateInfo, err := os.Stat(candidate)
	if errors.Is(err, os.ErrNotExist) {
		return sourcePath, info, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !candidateInfo.Mode().IsRegular() {
		return sourcePath, info, nil
	}
	return candidate, candidateInfo, nil
}

func openReadOnly(sourcePath string) (*sql.DB, error) {
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	u := &url.URL{Scheme: "file", Path: absPath}
	query := u.Query()
	query.Set("mode", "ro")
	u.RawQuery = query.Encode()
	source, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open source database: %w", err)
	}
	if err := source.Ping(); err != nil {
		source.Close()
		return nil, fmt.Errorf("ping source database: %w", err)
	}
	return source, nil
}

func hasRequiredSchema(source *sql.DB) (bool, error) {
	for table, requiredColumns := range requiredSourceSchema {
		var exists int
		if err := source.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return false, err
		}
		if exists != 1 {
			return false, nil
		}
		rows, err := source.Query(`PRAGMA table_info(` + quoteIdentifier(table) + `)`)
		if err != nil {
			return false, err
		}
		columns := make(map[string]bool)
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, columnType string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				return false, err
			}
			columns[name] = true
		}
		if err := rows.Close(); err != nil {
			return false, err
		}
		if err := rows.Err(); err != nil {
			return false, err
		}
		for _, column := range requiredColumns {
			if !columns[column] {
				return false, nil
			}
		}
	}
	return true, nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func Import(sourcePath string, targetDB *db.DB, dataDir string) (*ImportResult, error) {
	result := &ImportResult{}
	if targetDB == nil {
		return result, fmt.Errorf("target database is required")
	}

	kind, err := DetectSource(sourcePath)
	if err != nil {
		return fail(result, "detect source", err)
	}
	switch kind {
	case SourceEncryptedExport:
		return fail(result, "detect source", ErrEncryptedExport)
	case SourceUnsupported:
		return fail(result, "detect source", ErrUnsupportedSource)
	case SourceLiveDB:
	default:
		return fail(result, "detect source", ErrUnsupportedSource)
	}

	resolvedPath, _, err := resolveSourcePath(sourcePath)
	if err != nil {
		return fail(result, "resolve source", err)
	}
	sourceID, err := sourceNamespace(resolvedPath)
	if err != nil {
		return fail(result, "identify source", err)
	}
	source, err := openReadOnly(resolvedPath)
	if err != nil {
		return fail(result, "open source", err)
	}
	defer source.Close()

	ctx := context.Background()
	sourceTx, err := source.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fail(result, "begin source snapshot", err)
	}
	defer sourceTx.Rollback()
	targetTx, err := targetDB.BeginTx(ctx, nil)
	if err != nil {
		return fail(result, "begin target transaction", err)
	}
	defer targetTx.Rollback()
	committed := false
	defer func() {
		if !committed {
			result.Platforms = 0
			result.Accounts = 0
			result.Settings = 0
			result.Contents = 0
			result.Records = 0
			result.Media = 0
		}
	}()

	platforms, err := loadPlatformMap(sourceTx, targetTx, result)
	if err != nil {
		return fail(result, "import platforms", err)
	}
	if err := importAccounts(sourceTx, targetTx, dataDir, sourceID, platforms, result); err != nil {
		return fail(result, "import accounts", err)
	}
	if err := importPlatSets(sourceTx, targetTx, sourceID, platforms, result); err != nil {
		return fail(result, "import platform settings", err)
	}
	if err := importArticles(sourceTx, targetTx, sourceID, result); err != nil {
		return fail(result, "import contents", err)
	}
	if err := importPublishRecords(sourceTx, targetTx, sourceID, result); err != nil {
		return fail(result, "import publish records", err)
	}

	if err := sourceTx.Commit(); err != nil {
		return fail(result, "finish source snapshot", err)
	}
	if err := targetTx.Commit(); err != nil {
		return fail(result, "commit target transaction", err)
	}
	committed = true
	return result, nil
}

func sourceNamespace(resolvedPath string) (string, error) {
	canonical, err := filepath.EvalSymlinks(resolvedPath)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absPath)))
	return fmt.Sprintf("%s:%x", sourcePrefix, digest[:8]), nil
}

func fail(result *ImportResult, stage string, err error) (*ImportResult, error) {
	message := fmt.Sprintf("%s: %v", stage, err)
	result.Errors = append(result.Errors, message)
	return result, fmt.Errorf("%s: %w", stage, err)
}

func skip(result *ImportResult, reason string) {
	result.Skipped++
	result.SkippedReasons = append(result.SkippedReasons, reason)
}

func loadPlatformMap(source *sql.Tx, target *sql.Tx, result *ImportResult) (map[int64]string, error) {
	rows, err := source.Query(`SELECT id, COALESCE(plat_type, '') FROM platforms ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mappedPlatforms := make(map[int64]string)
	for rows.Next() {
		var id int64
		var sourceCode string
		if err := rows.Scan(&id, &sourceCode); err != nil {
			return nil, fmt.Errorf("platform row: %w", err)
		}
		normalized := strings.ToLower(strings.TrimSpace(sourceCode))
		mappedCode, ok := platformMap[normalized]
		if !ok {
			skip(result, fmt.Sprintf("platform %d skipped: unsupported type %q", id, normalized))
			continue
		}
		var exists int
		if err := target.QueryRow(`SELECT COUNT(*) FROM platforms WHERE code=?`, mappedCode).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check target platform %q: %w", mappedCode, err)
		}
		if exists != 1 {
			return nil, fmt.Errorf("target platform %q is not seeded", mappedCode)
		}
		mappedPlatforms[id] = mappedCode
		result.Platforms++
	}
	return mappedPlatforms, rows.Err()
}

func importAccounts(source *sql.Tx, target *sql.Tx, _ string, sourceID string, platforms map[int64]string, result *ImportResult) error {
	// Only stable identity metadata is read. Avatar/free-text fields are excluded
	// because they can carry signed URLs, QR payloads, or pasted credentials.
	rows, err := source.Query(`SELECT id, COALESCE(name, ''), platform_id FROM accounts ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	identityID, err := importIdentity(target, sourceID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, platformID int64
		var name string
		if err := rows.Scan(&id, &name, &platformID); err != nil {
			return fmt.Errorf("account %d: %w", id, err)
		}
		platformCode, ok := platforms[platformID]
		if !ok {
			skip(result, fmt.Sprintf("account %d skipped: source platform %d is unsupported", id, platformID))
			continue
		}
		if strings.TrimSpace(name) == "" {
			name = platformCode + " account"
		}
		now := time.Now().UTC().Format(time.RFC3339)

		var accountID int64
		err := target.QueryRow(`SELECT id FROM accounts WHERE source=? AND source_account_id=? ORDER BY id LIMIT 1`, sourceID, id).Scan(&accountID)
		switch {
		case err == nil:
			// Imported rows become Alpha-owned after their first insertion. A
			// repeated import must not reset local edits or authenticated state.
		case errors.Is(err, sql.ErrNoRows):
			insert, err := target.Exec(`INSERT INTO accounts (identity_id, platform_code, kind, name, avatar, login_state, sort, remark, source, source_account_id, created_at, updated_at) VALUES (?, ?, 'browser_login', ?, '', 'unknown', 0, '', ?, ?, ?, ?)`,
				identityID, platformCode, name, sourceID, id, now, now)
			if err != nil {
				return fmt.Errorf("insert account %d: %w", id, err)
			}
			accountID, err = insert.LastInsertId()
			if err != nil {
				return fmt.Errorf("account %d id: %w", id, err)
			}
		default:
			return fmt.Errorf("lookup account %d: %w", id, err)
		}

		var assetCount int
		if err := target.QueryRow(`SELECT COUNT(*) FROM account_assets WHERE account_id=? AND asset_type='browser_account'`, accountID).Scan(&assetCount); err != nil {
			return fmt.Errorf("lookup account asset %d: %w", id, err)
		}
		if assetCount == 0 {
			if _, err := target.Exec(`INSERT INTO account_assets (
					account_id,platform_code,asset_type,name,external_id,avatar,enabled,last_seen_at,created_at,updated_at
				) VALUES (?,?,'browser_account',?,'','',1,'',?,?)`,
				accountID, platformCode, name, now, now); err != nil {
				return fmt.Errorf("insert account asset %d: %w", id, err)
			}
		}
		result.Accounts++
	}
	return rows.Err()
}

func importIdentity(target *sql.Tx, sourceID string) (int64, error) {
	var identityID int64
	err := target.QueryRow(`SELECT identity_id FROM accounts WHERE source=? ORDER BY id LIMIT 1`, sourceID).Scan(&identityID)
	if err == nil {
		return identityID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	suffix := strings.TrimPrefix(sourceID, sourcePrefix+":")
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	name := "导入矩阵 " + suffix
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := target.Exec(`INSERT INTO ego_identities(name,enabled,sort,created_at,updated_at)
		VALUES (?,1,(SELECT COALESCE(MAX(sort),-1)+1 FROM ego_identities),?,?)`, name, now, now)
	if err != nil {
		return 0, fmt.Errorf("create import identity: %w", err)
	}
	identityID, err = result.LastInsertId()
	return identityID, err
}

func importPlatSets(source *sql.Tx, target *sql.Tx, sourceID string, platforms map[int64]string, result *ImportResult) error {
	// Source setting JSON is deliberately not selected: platform configuration
	// may embed API keys, proxy credentials, or device data. Metadata remains useful.
	rows, err := source.Query("SELECT id, COALESCE(name, ''), COALESCE(description, ''), platform_id, COALESCE(enable, 0), COALESCE(`default`, 0) FROM plat_sets ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, platformID int64
		var name, description string
		var enabled, isDefault int
		if err := rows.Scan(&id, &name, &description, &platformID, &enabled, &isDefault); err != nil {
			return fmt.Errorf("platform setting %d: %w", id, err)
		}
		platformCode, ok := platforms[platformID]
		if !ok {
			skip(result, fmt.Sprintf("platform setting %d skipped: source platform %d is unsupported", id, platformID))
			continue
		}
		const safeSettings = "{}"
		now := time.Now().UTC().Format(time.RFC3339)
		var targetID int64
		err = target.QueryRow(`SELECT id FROM platform_settings WHERE source=? AND source_setting_id=? ORDER BY id LIMIT 1`, sourceID, id).Scan(&targetID)
		switch {
		case err == nil:
			// Keep locally edited Alpha configuration authoritative on reimport.
		case errors.Is(err, sql.ErrNoRows):
			_, err = target.Exec(`INSERT INTO platform_settings (platform_code, name, description, settings_json, enabled, is_default, source, source_setting_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				platformCode, name, description, safeSettings, enabled, isDefault, sourceID, id, now, now)
		default:
			return fmt.Errorf("lookup platform setting %d: %w", id, err)
		}
		if err != nil {
			return fmt.Errorf("write platform setting %d: %w", id, err)
		}
		result.Settings++
	}
	return rows.Err()
}

func importArticles(source *sql.Tx, target *sql.Tx, sourceID string, result *ImportResult) error {
	rows, err := source.Query(`SELECT id, COALESCE(publish_data, '') FROM articles ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var publishDataJSON string
		if err := rows.Scan(&id, &publishDataJSON); err != nil {
			return fmt.Errorf("content %d: %w", id, err)
		}
		content, err := decodeContent(publishDataJSON)
		if err != nil {
			skip(result, fmt.Sprintf("content %d skipped: invalid publish_data JSON", id))
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		var targetID int64
		err = target.QueryRow(`SELECT id FROM contents WHERE source=? AND source_content_id=? ORDER BY id LIMIT 1`, sourceID, id).Scan(&targetID)
		switch {
		case err == nil:
			// Published/local content state belongs to Alpha after first import.
		case errors.Is(err, sql.ErrNoRows):
			_, err = target.Exec(`INSERT INTO contents (type, title, caption, body, tags_json, status, publish_count, raw_publish_data, source, source_content_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'draft', 0, NULL, ?, ?, ?, ?)`,
				content.contentType, content.title, content.caption, content.body, content.tagsJSON, sourceID, id, now, now)
		default:
			return fmt.Errorf("lookup content %d: %w", id, err)
		}
		if err != nil {
			return fmt.Errorf("write content %d: %w", id, err)
		}
		result.Contents++
	}
	return rows.Err()
}

type decodedContent struct {
	contentType string
	title       string
	caption     string
	body        string
	tagsJSON    string
}

func decodeContent(value string) (decodedContent, error) {
	var publishData map[string]any
	if err := json.Unmarshal([]byte(value), &publishData); err != nil {
		return decodedContent{}, err
	}
	content := decodedContent{contentType: "graph_text", tagsJSON: "[]"}
	content.title, _ = publishData["title"].(string)
	content.body, _ = publishData["content"].(string)
	content.caption, _ = publishData["caption"].(string)
	if content.caption == "" {
		content.caption, _ = publishData["desc"].(string)
	}
	if content.caption == "" {
		content.caption, _ = publishData["description"].(string)
	}
	if content.caption == "" {
		content.caption = content.body
	}
	if publishType, ok := publishData["publish_type"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(publishType)) {
		case "video", "reel":
			content.contentType = "reel"
		case "article":
			content.contentType = "article"
		}
	}
	if tags, ok := publishData["tags"].([]any); ok {
		filtered := make([]string, 0, len(tags))
		for _, tag := range tags {
			if value, ok := tag.(string); ok {
				filtered = append(filtered, value)
			}
		}
		encoded, err := json.Marshal(filtered)
		if err != nil {
			return decodedContent{}, err
		}
		content.tagsJSON = string(encoded)
	}
	return content, nil
}

func importPublishRecords(source *sql.Tx, target *sql.Tx, sourceID string, result *ImportResult) error {
	// The observed Turbo Push record row does not provide a safe account/platform
	// relation. Importing with zero or guessed foreign keys would fabricate
	// history, so D1 reports these rows and leaves them untouched.
	rows, err := source.Query(`SELECT id, content_id FROM publish_records ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, sourceContentID int64
		if err := rows.Scan(&id, &sourceContentID); err != nil {
			return fmt.Errorf("publish record %d: %w", id, err)
		}
		var contentID int64
		err := target.QueryRow(`SELECT id FROM contents WHERE source=? AND source_content_id=? ORDER BY id LIMIT 1`, sourceID, sourceContentID).Scan(&contentID)
		if errors.Is(err, sql.ErrNoRows) {
			skip(result, fmt.Sprintf("publish_record %d skipped: source content %d is unresolved", id, sourceContentID))
			continue
		}
		if err != nil {
			return fmt.Errorf("resolve publish record %d content: %w", id, err)
		}
		skip(result, fmt.Sprintf("publish_record %d skipped: account and platform relation is unavailable", id))
	}
	return rows.Err()
}
