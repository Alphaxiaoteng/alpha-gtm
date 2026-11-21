package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/platforms"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	DataDir string
}

func Open(dataDir string) (*DB, error) {
	dbPath := filepath.Join(dataDir, "app.db")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.Chmod(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("restrict data dir permissions: %w", err)
	}
	dsn := &url.URL{Scheme: "file", Path: dbPath}
	query := dsn.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	// 事务一律以 IMMEDIATE 开始。默认的 deferred 事务只先拿读锁，等第一条写
	// 语句才升级；SQLite 在「升级时发现别人持有写锁」时不走 busy_timeout，
	// 直接返回 SQLITE_BUSY。迁移恰好是先读 PRAGMA 再写 DDL，所以两个进程同时
	// 启动时后一个必定失败——实测 4 个进程同时 Open+Migrate 同一个库，3 个以
	// `database is locked (5)` 退出。IMMEDIATE 在事务开头就取写锁，
	// busy_timeout 正常生效，后启动的进程会排队等待。
	query.Add("_txlock", "immediate")
	dsn.RawQuery = query.Encode()
	conn, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := os.Chmod(dbPath, 0600); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("restrict database permissions: %w", err)
	}
	// Configure pragmas to prevent SQLITE_BUSY database is locked error
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	// 切 WAL 需要独占锁，多个进程同时打开同一个库时后到的会拿到 SQLITE_BUSY。
	// 已经处于 WAL 的库不需要（也不该）再切一次：先读当前模式，只有确实不是
	// WAL 时才设置，并对并发竞争做有限重试。
	if err := ensureWAL(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set journal_mode WAL: %w", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}
	// WAL + synchronous=NORMAL is the standard durable-enough write profile:
	// a commit no longer waits for an fsync, and WAL still protects against
	// process crashes. cache_size is negative to mean KiB (20 MB), and
	// temp_store=MEMORY keeps sort/group temp tables off disk.
	if _, err := conn.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	if _, err := conn.Exec("PRAGMA cache_size = -20000;"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set cache_size: %w", err)
	}
	if _, err := conn.Exec("PRAGMA temp_store = MEMORY;"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set temp_store: %w", err)
	}
	database := &DB{DB: conn, DataDir: dataDir}
	// 全量 PRAGMA quick_check 在 124 MB 库上要 3.3 s，而连接池只有 1 条连接，
	// 整段启动都会被它卡住。这里只做根页探测（schema_version + sqlite_master，
	// 读 1~2 页）：非 SQLite 文件或 root page 损坏会立刻失败，深层损坏留给
	// SQLite 自身的读写错误和诊断接口（internal/db/diagnostics.go）。
	var schemaVersion int
	if err := conn.QueryRow("PRAGMA schema_version").Scan(&schemaVersion); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sqlite schema probe: %w", err)
	}
	var objectCount int
	if err := conn.QueryRow("SELECT count(*) FROM sqlite_master").Scan(&objectCount); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sqlite catalog probe: %w", err)
	}
	return database, nil
}

// VerifyIntegrity runs the full PRAGMA quick_check on its own read-only
// connection, so a 124 MB library cannot stall startup and cannot occupy the
// single main connection. Callers run it after the server is already serving.
func (db *DB) VerifyIntegrity(ctx context.Context) error {
	if db == nil || db.DB == nil {
		return fmt.Errorf("sqlite integrity: database is closed")
	}
	dbPath := filepath.Join(db.DataDir, "app.db")
	dsn := &url.URL{Scheme: "file", Path: dbPath}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "busy_timeout(10000)")
	query.Add("_pragma", "query_only(1)")
	dsn.RawQuery = query.Encode()
	conn, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return fmt.Errorf("sqlite integrity: %w", err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	var result string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&result); err != nil {
		return fmt.Errorf("sqlite integrity: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("sqlite integrity failed")
	}
	return nil
}

// OpenReadOnly opens an existing application database without initializing or
// validating it. Callers that use this path must perform their own narrow
// schema check before reading. The connection is deliberately query-only so
// a read-only tool cannot accidentally mutate the asset database.
func OpenReadOnly(dataDir string) (*DB, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("open sqlite read-only: data directory is required")
	}
	dbPath := filepath.Join(dataDir, "app.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("open sqlite read-only: app.db is missing")
		}
		return nil, fmt.Errorf("open sqlite read-only: app.db is unavailable")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open sqlite read-only: app.db is not a regular file")
	}

	dsn := &url.URL{Scheme: "file", Path: dbPath}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "query_only(1)")
	dsn.RawQuery = query.Encode()
	conn, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite read-only: database unavailable")
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open sqlite read-only: database unavailable")
	}
	return &DB{DB: conn, DataDir: dataDir}, nil
}

// QuickCheck rejects a damaged local asset database before migrations or
// browser work can make the failure harder to diagnose.
func (db *DB) QuickCheck() error {
	var result string
	if err := db.QueryRow("PRAGMA quick_check(1)").Scan(&result); err != nil {
		return fmt.Errorf("sqlite quick_check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("sqlite quick_check failed")
	}
	return nil
}

// schemaVersion 标记「建表 + 补列 + 建索引 + 建触发器」这批幂等 DDL 的版本，
// 存在 SQLite 的 PRAGMA user_version 里。新建或升级过结构的库会把版本号写在
// 迁移事务内，之后每次启动都跳过这几百条 DDL 与 78 次 PRAGMA table_info。
//
// 每次改动 migrations / schemaVirtualTables / additions / schemaIndexes /
// schemaTriggers，都必须把 schemaVersion +1——否则已升过级的库会跳过新结构。
//
// 数据回填语句刻意**不**跳过：它们是 `UPDATE ... WHERE 条件不满足` 的幂等清洗，
// 每次执行保证老库里的脏数据在任何一次启动后都被收敛，删掉反而会留下已升级
// 库永远修不了的历史数据。
const schemaVersion = 1

// ensureWAL 让数据库处于 WAL 模式。已经是的直接返回，避免在并发打开时无谓
// 争抢独占锁；确实需要切换时，切换本身可能与另一个正在启动的进程冲突，所以
// 做有限次重试——busy_timeout 对 journal_mode 切换不生效。
func ensureWAL(conn *sql.DB) error {
	var deadline = time.Now().Add(5 * time.Second)
	for {
		var mode string
		if err := conn.QueryRow("PRAGMA journal_mode;").Scan(&mode); err == nil &&
			strings.EqualFold(mode, "wal") {
			return nil
		}
		_, err := conn.Exec("PRAGMA journal_mode = WAL;")
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "SQLITE_BUSY") || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (db *DB) Migrate() error {
	// 版本号相同不代表结构完整：已有库可能被外部工具改过、也可能是测试手工
	// 造出来的老结构。所以版本一致时再做一次轻量哨兵检查（一条 sqlite_master
	// 计数 + 一次 PRAGMA），都对得上才跳过 DDL，否则照常重放。
	var currentVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&currentVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()
	if currentVersion != schemaVersion || !schemaLooksCurrent(tx) {
		if err := migrateSchemaDDL(tx); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("stamp schema version: %w", err)
	}
	return db.migrateData(tx)
}

// schemaLooksCurrent 是「版本号对得上」之后的二次确认，只花几次查询。
// 哨兵挑的是最近几轮迁移才引入、且一定会被 CREATE 出来的对象：缺任何一个
// 就说明这个库没真正跑过当前 DDL（被外部工具改过，或整张表被重建成了旧版）。
//
// 这里刻意不追求「完整校验」——完整校验等于重放一遍 DDL。它防的是最常见的
// 两种漂移：表被删、索引被删；以及老版本 CHECK 约束残留。
func schemaLooksCurrent(tx *sql.Tx) bool {
	var tables int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'
		AND name IN ('creator_metric_snapshots','radar_capture_calls','agent_bots')`).Scan(&tables); err != nil {
		return false
	}
	if tables != 3 {
		return false
	}
	var readySourceIndex int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index'
		AND name='idx_knowledge_documents_ready_source'`).Scan(&readySourceIndex); err != nil {
		return false
	}
	if readySourceIndex != 1 {
		return false
	}
	// manual_takeover 是后加的枚举值；老表定义里没有它时，下面这条 PRAGMA
	// 拿到的 sql 里就查不到这个词，必须重放迁移把 CHECK 换掉。
	var collectionStates string
	if err := tx.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master
		WHERE type='table' AND name='creator_metric_collection_states'`).Scan(&collectionStates); err != nil {
		return false
	}
	return strings.Contains(collectionStates, "manual_takeover")
}

// migrateSchemaDDL 是 Migrate 里所有「结构与常量 DDL」的部分，幂等且可跳过。
func migrateSchemaDDL(tx *sql.Tx) error {
	for _, stmt := range migrations {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	for _, stmt := range schemaVirtualTables {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("create virtual table: %w", err)
		}
	}
	additions := []struct {
		table, column, definition string
	}{
		{"accounts", "identity_id", "INTEGER NOT NULL DEFAULT 1"},
		{"ego_identities", "profile_id", "TEXT NOT NULL DEFAULT ''"},
		{"ego_identities", "profile_name", "TEXT NOT NULL DEFAULT ''"},
		{"ego_identities", "avatar", "TEXT NOT NULL DEFAULT ''"},
		{"browser_profiles", "fingerprint_config", "TEXT NOT NULL DEFAULT ''"},
		{"browser_profiles", "last_pid", "INTEGER"},
		{"browser_profiles", "proxy_server", "TEXT NOT NULL DEFAULT ''"},
		{"browser_profiles", "is_proxy_custom", "INTEGER NOT NULL DEFAULT 0"},
		{"accounts", "account_group", "TEXT NOT NULL DEFAULT ''"},
		{"accounts", "enabled", "INTEGER NOT NULL DEFAULT 1"},
		{"accounts", "avatar_source", "TEXT NOT NULL DEFAULT 'platform_fallback'"},
		{"accounts", "avatar_sha256", "TEXT NOT NULL DEFAULT ''"},
		{"accounts", "avatar_refreshed_at", "TEXT NOT NULL DEFAULT ''"},
		{"accounts", "login_mode", "TEXT NOT NULL DEFAULT 'unknown'"},
		{"accounts", "auth_method", "TEXT NOT NULL DEFAULT 'unknown'"},
		{"accounts", "last_focused_at", "TEXT NOT NULL DEFAULT ''"},
		{"account_login_events", "avatar_error_code", "TEXT NOT NULL DEFAULT ''"},
		{"browser_events", "reason_code", "TEXT NOT NULL DEFAULT ''"},
		{"browser_events", "exit_code", "INTEGER"},
		{"browser_events", "term_signal", "INTEGER"},
		{"publish_jobs", "trace_id", "TEXT"},
		{"publish_jobs", "idempotency_key", "TEXT"},
		{"publish_jobs", "cancel_requested_at", "TEXT"},
		{"publish_jobs", "plan_id", "INTEGER"},
		{"publish_jobs", "plan_target_id", "INTEGER"},
		{"publish_jobs", "snapshot_hash", "TEXT NOT NULL DEFAULT ''"},
		{"publish_jobs", "submit_attempted_at", "TEXT NOT NULL DEFAULT ''"},
		{"publish_jobs", "submit_outcome", "TEXT NOT NULL DEFAULT ''"},
		{"publish_plans", "approved_action", "TEXT NOT NULL DEFAULT ''"},
		{"publish_plans", "studio_handoff_id", "INTEGER REFERENCES studio_schedule_handoffs(id) ON DELETE RESTRICT"},
		{"job_events", "trace_id", "TEXT"},
		{"job_events", "request_id", "TEXT"},
		{"job_events", "task_id", "TEXT"},
		{"job_events", "duration_ms", "INTEGER NOT NULL DEFAULT 0"},
		{"job_events", "error_code", "TEXT"},
		{"job_events", "error_class", "TEXT"},
		{"job_events", "retryable", "INTEGER NOT NULL DEFAULT 0"},
		{"job_events", "schema_version", "INTEGER NOT NULL DEFAULT 1"},
		{"task_queue", "lease_owner", "TEXT"},
		{"task_queue", "lease_expires_at", "TEXT"},
		{"task_queue", "cancel_requested_at", "TEXT"},
		{"generation_jobs", "mode", "TEXT NOT NULL DEFAULT ''"},
		{"generation_jobs", "references_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"generation_jobs", "request_id", "TEXT NOT NULL DEFAULT ''"},
		{"generation_jobs", "request_hash", "TEXT NOT NULL DEFAULT ''"},
		{"generation_jobs", "output_size", "TEXT NOT NULL DEFAULT ''"},
		{"generation_jobs", "duration_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"media_assets", "library_id", "INTEGER REFERENCES media_library(id) ON DELETE RESTRICT"},
		{"media_assets", "role", "TEXT NOT NULL DEFAULT 'content'"},
		{"media_assets", "position", "INTEGER NOT NULL DEFAULT 0"},
		{"contents", "planned_at", "TEXT NOT NULL DEFAULT ''"},
		{"contents", "revision", "INTEGER NOT NULL DEFAULT 1"},
		{"media_library", "purpose", "TEXT NOT NULL DEFAULT 'shared' CHECK (purpose IN ('shared','video','graph_text','article'))"},
		{"studio_scripts", "content_type", "TEXT NOT NULL DEFAULT 'video' CHECK (content_type IN ('video','graph_text','article'))"},
		{"agent_bots", "pinned", "INTEGER NOT NULL DEFAULT 0"},
		{"agent_bots", "is_primary", "INTEGER NOT NULL DEFAULT 0"},
		{"agent_bots", "group_name", "TEXT NOT NULL DEFAULT ''"},
		{"agent_bots", "muted", "INTEGER NOT NULL DEFAULT 0"},
		{"agent_conversations", "last_read_at", "TEXT NOT NULL DEFAULT ''"},
		{"agent_conversations", "model_id", "TEXT NOT NULL DEFAULT ''"},
		{"agent_conversations", "bot_id", "TEXT NOT NULL DEFAULT ''"},
		{"agent_conversations", "workspace_mode", "TEXT NOT NULL DEFAULT 'media' CHECK (workspace_mode IN ('media','shop'))"},
		{"agent_runs", "model_id", "TEXT NOT NULL DEFAULT ''"},
		{"agent_messages", "provider_id", "TEXT NOT NULL DEFAULT ''"},
		{"agent_messages", "run_id", "TEXT NOT NULL DEFAULT ''"},
		{"agent_provider_configs", "executable_path", "TEXT NOT NULL DEFAULT ''"},
		{"agent_provider_configs", "launch_mode", "TEXT NOT NULL DEFAULT ''"},
		{"account_tags", "position", "INTEGER NOT NULL DEFAULT 0"},
		{"creator_metric_snapshots", "followers_verified", "INTEGER NOT NULL DEFAULT 0 CHECK (followers_verified IN (0,1))"},
		{"knowledge_documents", "source_size_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"knowledge_documents", "source_modified_ns", "INTEGER NOT NULL DEFAULT 0"},
		{"radar_works", "published_at", "TEXT NOT NULL DEFAULT ''"},
		{"feishu_sync", "content_hash", "TEXT NOT NULL DEFAULT ''"},
		{"radar_capture_calls", "ad_count", "INTEGER NOT NULL DEFAULT 0"},
		{"radar_capture_calls", "guide_count", "INTEGER NOT NULL DEFAULT 0"},
		{"radar_capture_calls", "unreadable_count", "INTEGER NOT NULL DEFAULT 0"},
		{"radar_capture_runs", "skipped", "INTEGER NOT NULL DEFAULT 0"},
		{"radar_capture_runs", "last_retry_at", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, addition := range additions {
		if err := ensureColumn(tx, addition.table, addition.column, addition.definition); err != nil {
			return fmt.Errorf("migrate %s.%s: %w", addition.table, addition.column, err)
		}
	}
	if err := ensureCreatorMetricManualHandoffState(tx); err != nil {
		return fmt.Errorf("migrate creator metric manual handoff state: %w", err)
	}
	if err := ensureShopListingPlatformSchema(tx); err != nil {
		return fmt.Errorf("migrate shop listing platforms: %w", err)
	}
	if err := ensurePublishPlanTargetsAssetUniqueness(tx); err != nil {
		return fmt.Errorf("migrate publish plan targets: %w", err)
	}
	if err := ensureEgoMatrixSchema(tx); err != nil {
		return fmt.Errorf("migrate ego matrix spaces: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_knowledge_documents_source_versions
		ON knowledge_documents(base_id,source_name,source_modified_ns DESC,id DESC)`); err != nil {
		return fmt.Errorf("index knowledge source versions: %w", err)
	}
	// 必须在建 idx_knowledge_documents_ready_source 之前收敛同源多版本，
	// 否则老库里并存的 ready 行会让唯一索引直接建失败。
	if err := reconcileKnowledgeLatestSources(tx); err != nil {
		return fmt.Errorf("reconcile latest knowledge sources: %w", err)
	}
	for _, stmt := range schemaIndexes {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}
	// These evidence triggers have intentional transition updates. Recreate
	// them so existing local databases receive the same immutable submit and
	// creator-backend verification gates as a fresh database.
	for _, trigger := range []string{
		"trg_publish_jobs_submit_marker_immutable",
		"trg_publish_records_verified_insert",
	} {
		if _, err := tx.Exec(`DROP TRIGGER IF EXISTS ` + trigger); err != nil {
			return fmt.Errorf("replace %s trigger: %w", trigger, err)
		}
	}
	for _, stmt := range schemaTriggers {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("create integrity trigger: %w", err)
		}
	}
	return nil
}

// migrateData 是每次都跑的数据收敛部分：回填与清洗，全部带 WHERE 条件。
func (db *DB) migrateData(tx *sql.Tx) error {
	if _, err := tx.Exec(`UPDATE generation_jobs SET mode=CASE kind WHEN 'video' THEN 'text_to_video' ELSE 'text_to_image' END WHERE mode=''`); err != nil {
		return fmt.Errorf("backfill generation job mode: %w", err)
	}
	if _, err := tx.Exec(`UPDATE generation_jobs SET output_size=CASE kind WHEN 'video' THEN '3:2' ELSE '1:1' END WHERE output_size=''`); err != nil {
		return fmt.Errorf("backfill generation job output size: %w", err)
	}
	if _, err := tx.Exec(`UPDATE generation_jobs SET duration_seconds=5 WHERE kind='video' AND duration_seconds=0`); err != nil {
		return fmt.Errorf("backfill generation job duration: %w", err)
	}
	if _, err := tx.Exec(`UPDATE agent_messages SET provider_id=COALESCE((SELECT provider_id FROM agent_conversations WHERE agent_conversations.id=agent_messages.conversation_id),'') WHERE provider_id=''`); err != nil {
		return fmt.Errorf("backfill agent message provider: %w", err)
	}
	if _, err := tx.Exec(`UPDATE agent_conversations SET workspace_mode='media' WHERE workspace_mode='' OR workspace_mode IS NULL`); err != nil {
		return fmt.Errorf("backfill agent workspace mode: %w", err)
	}
	if _, err := tx.Exec(`UPDATE account_tags AS current
		SET position=(SELECT COUNT(*) FROM account_tags AS prior
			WHERE prior.name COLLATE NOCASE < current.name COLLATE NOCASE
				OR (prior.name = current.name COLLATE NOCASE AND prior.id < current.id))
		WHERE (SELECT COUNT(*) FROM account_tags) > 1
			AND (SELECT COUNT(DISTINCT position) FROM account_tags)=1`); err != nil {
		return fmt.Errorf("backfill account tag positions: %w", err)
	}
	if _, err := tx.Exec(`UPDATE media_library SET purpose='video' WHERE purpose='shared' AND lower(COALESCE(mime,'')) LIKE 'video/%'`); err != nil {
		return fmt.Errorf("backfill media library purpose: %w", err)
	}
	if err := scrubLegacyGenerationReferenceURLs(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE accounts SET avatar = '' WHERE avatar LIKE '%qrcode%' OR avatar LIKE '%qr-code%' OR avatar LIKE 'data:image%'`); err != nil {
		return fmt.Errorf("clean leaked QR avatars: %w", err)
	}
	// Legacy remote avatar URLs may contain signed query/fragment material.
	// Keep only the public path until a reviewed DOM snapshot replaces them.
	if _, err := tx.Exec(`UPDATE accounts SET avatar = CASE
		WHEN instr(avatar,'?') > 0 THEN substr(avatar,1,instr(avatar,'?')-1)
		WHEN instr(avatar,'#') > 0 THEN substr(avatar,1,instr(avatar,'#')-1)
		ELSE avatar END
		WHERE avatar LIKE 'http://%' OR avatar LIKE 'https://%'`); err != nil {
		return fmt.Errorf("sanitize legacy remote avatars: %w", err)
	}
	if _, err := tx.Exec(`UPDATE accounts SET
		avatar_source = CASE
			WHEN avatar LIKE 'account-avatars/%' THEN 'observed'
			WHEN COALESCE(avatar,'') <> '' THEN 'legacy_remote'
			ELSE 'platform_fallback' END,
		avatar_sha256 = CASE WHEN avatar LIKE 'account-avatars/%' THEN avatar_sha256 ELSE '' END`); err != nil {
		return fmt.Errorf("normalize avatar provenance: %w", err)
	}
	if err := seedPlatforms(tx); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := consolidateLegacyMetaAccounts(tx, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO ego_identities (id,name,enabled,sort,created_at,updated_at) VALUES (1,'浏览器1',1,0,?,?)`, now, now); err != nil {
		return fmt.Errorf("seed default ego identity: %w", err)
	}
	if err := normalizeLegacyAutomaticIdentityNames(tx, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO ego_session_monitor (id,updated_at) VALUES (1,?)`, now); err != nil {
		return fmt.Errorf("seed ego session monitor: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO metrics_collection_schedule (id,updated_at) VALUES (1,?)`, now); err != nil {
		return fmt.Errorf("seed metrics collection schedule: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO creator_metric_preferences (id,history_days,retention_days,updated_at) VALUES (1,30,90,?)`, now); err != nil {
		return fmt.Errorf("seed creator metric preferences: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO notification_settings(key, value, updated_at) VALUES ('reminder_interval_hours', '12', ?)`, now); err != nil {
		return fmt.Errorf("seed notification settings: %w", err)
	}
	// Page-wide DOM text does not bind a follower number to an account metric
	// card or to the selected seven-day range. A follower-only legacy snapshot
	// has no usable metric evidence; mixed snapshots keep their measured 7-day
	// metrics but drop the unbound follower field.
	if _, err := tx.Exec(`UPDATE creator_metric_collection_states SET
		state='unknown',error_code='METRICS_DOM_UNBOUND',evidence_code='',range_verified=0,post_count=0,updated_at=?
		WHERE EXISTS (
			SELECT 1 FROM creator_metric_snapshots snapshot
			WHERE snapshot.account_id=creator_metric_collection_states.account_id
			  AND snapshot.evidence_code='REVIEWED_ANALYTICS_DOM' AND snapshot.followers>0
			  AND snapshot.views+snapshot.likes+snapshot.comments+snapshot.shares+snapshot.favorites+snapshot.posts=0
		)`, now); err != nil {
		return fmt.Errorf("invalidate unbound creator DOM collection state: %w", err)
	}
	if _, err := tx.Exec(`UPDATE creator_metric_snapshots SET
		range_verified=0,followers=0,evidence_code='INVALIDATED_DOM_FOLLOWERS',followers_verified=0,updated_at=?
		WHERE evidence_code='REVIEWED_ANALYTICS_DOM' AND followers>0
		  AND views+likes+comments+shares+favorites+posts=0`, now); err != nil {
		return fmt.Errorf("invalidate unbound creator DOM follower snapshots: %w", err)
	}
	if _, err := tx.Exec(`UPDATE creator_metric_snapshots SET followers=0,followers_verified=0,updated_at=?
		WHERE evidence_code='INVALIDATED_DOM_FOLLOWERS' AND followers>0`, now); err != nil {
		return fmt.Errorf("scrub invalidated creator DOM follower values: %w", err)
	}
	if _, err := tx.Exec(`UPDATE creator_metric_snapshots SET followers=0,followers_verified=0,updated_at=?
		WHERE evidence_code='REVIEWED_ANALYTICS_DOM' AND followers>0
		  AND views+likes+comments+shares+favorites+posts>0`, now); err != nil {
		return fmt.Errorf("drop unbound creator DOM follower fields: %w", err)
	}
	// Kuaishou creator pages include promotion/reward thresholds whose labels
	// overlap with account analytics. Historical page-wide DOM observations are
	// therefore not account-bound evidence. Keep the rows for audit, but remove
	// every metric and exclude them from dashboards.
	if _, err := tx.Exec(`UPDATE creator_metric_collection_states SET
		state='unknown',error_code='METRICS_DOM_UNBOUND',evidence_code='',range_verified=0,post_count=0,updated_at=?
		WHERE EXISTS (
			SELECT 1 FROM creator_metric_snapshots snapshot
			WHERE snapshot.account_id=creator_metric_collection_states.account_id
			  AND snapshot.platform_code='kuaishou'
			  AND snapshot.evidence_code IN ('REVIEWED_ANALYTICS_DOM','INVALIDATED_DOM_FOLLOWERS')
		)`, now); err != nil {
		return fmt.Errorf("invalidate Kuaishou page-wide DOM collection state: %w", err)
	}
	if _, err := tx.Exec(`UPDATE creator_metric_snapshots SET
		range_verified=0,followers=0,views=0,likes=0,comments=0,shares=0,favorites=0,posts=0,
		evidence_code='INVALIDATED_KUAISHOU_DOM',followers_verified=0,updated_at=?
		WHERE platform_code='kuaishou'
		  AND evidence_code IN ('REVIEWED_ANALYTICS_DOM','INVALIDATED_DOM_FOLLOWERS')`, now); err != nil {
		return fmt.Errorf("invalidate Kuaishou page-wide DOM snapshots: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO creator_metric_collection_states
		(account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
		SELECT snapshot.account_id,snapshot.platform_code,'unknown','METRICS_DOM_UNBOUND','',0,0,snapshot.collected_at,?
		FROM creator_metric_snapshots snapshot
		JOIN (SELECT account_id,MAX(id) AS id FROM creator_metric_snapshots
			WHERE evidence_code='INVALIDATED_KUAISHOU_DOM' GROUP BY account_id) latest ON latest.id=snapshot.id`, now); err != nil {
		return fmt.Errorf("backfill invalidated Kuaishou DOM collection states: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO creator_metric_collection_states
		(account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
		SELECT snapshot.account_id,snapshot.platform_code,'unknown','METRICS_DOM_UNBOUND','',0,0,snapshot.collected_at,?
		FROM creator_metric_snapshots snapshot
		JOIN (SELECT account_id,MAX(id) AS id FROM creator_metric_snapshots
			WHERE evidence_code='INVALIDATED_DOM_FOLLOWERS' GROUP BY account_id) latest ON latest.id=snapshot.id`, now); err != nil {
		return fmt.Errorf("backfill invalidated creator DOM collection states: %w", err)
	}
	// Old resource-backed snapshots did not persist the sanitized origin/path,
	// seven-day range hint, or numeric field names. Keep the row for audit but
	// do not treat an untraceable zero or metric as verified evidence.
	if _, err := tx.Exec(`UPDATE creator_metric_collection_states SET
		state='unknown',error_code='METRICS_RESOURCE_PROVENANCE_MISSING',evidence_code='',range_verified=0,post_count=0,updated_at=?
		WHERE EXISTS (
			SELECT 1 FROM creator_metric_snapshots snapshot
			WHERE snapshot.account_id=creator_metric_collection_states.account_id
			  AND snapshot.evidence_code='REVIEWED_ANALYTICS_RESOURCE'
			  AND (json_valid(snapshot.raw_json)=0
			    OR COALESCE(json_type(snapshot.raw_json,'$.resource_diagnostics'),'') <> 'array'
			    OR json_array_length(snapshot.raw_json,'$.resource_diagnostics')=0)
		)`, now); err != nil {
		return fmt.Errorf("invalidate creator resource collection state without provenance: %w", err)
	}
	if _, err := tx.Exec(`UPDATE creator_metric_snapshots SET
		range_verified=0,evidence_code='INVALIDATED_RESOURCE_PROVENANCE',updated_at=?
		WHERE evidence_code='REVIEWED_ANALYTICS_RESOURCE'
		  AND (json_valid(raw_json)=0
		    OR COALESCE(json_type(raw_json,'$.resource_diagnostics'),'') <> 'array'
		    OR json_array_length(raw_json,'$.resource_diagnostics')=0)`, now); err != nil {
		return fmt.Errorf("invalidate creator resource snapshots without provenance: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO creator_metric_collection_states
		(account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
		SELECT snapshot.account_id,snapshot.platform_code,'unknown','METRICS_RESOURCE_PROVENANCE_MISSING','',0,0,snapshot.collected_at,?
		FROM creator_metric_snapshots snapshot
		JOIN (SELECT account_id,MAX(id) AS id FROM creator_metric_snapshots
			WHERE evidence_code='INVALIDATED_RESOURCE_PROVENANCE' GROUP BY account_id) latest ON latest.id=snapshot.id`, now); err != nil {
		return fmt.Errorf("backfill invalidated creator resource collection states: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO creator_metric_collection_states
		(account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
		SELECT snapshot.account_id,snapshot.platform_code,'observed','',snapshot.evidence_code,
			snapshot.range_verified,snapshot.posts,snapshot.collected_at,?
		FROM creator_metric_snapshots snapshot
		JOIN (SELECT account_id,MAX(id) AS id FROM creator_metric_snapshots
			WHERE range_verified=1 GROUP BY account_id) latest ON latest.id=snapshot.id`, now); err != nil {
		return fmt.Errorf("backfill creator metric collection states: %w", err)
	}
	for _, kind := range []string{"feishu", "dingtalk", "webhook", "email"} {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO notification_channels(kind, updated_at) VALUES (?, ?)`, kind, now); err != nil {
			return fmt.Errorf("seed notification channel %s: %w", kind, err)
		}
	}
	// A browser-login account is itself a safe draft target unless a platform
	// discovery flow later creates more specific assets (for example Meta Pages).
	// This is metadata only; it never creates or copies a browser session.
	if _, err := tx.Exec(`INSERT INTO account_assets (account_id, platform_code, asset_type, name, external_id, avatar, enabled, last_seen_at, created_at, updated_at)
		SELECT a.id, a.platform_code, 'browser_account', a.name, '', '', 1, '', ?, ?
		FROM accounts a
		WHERE NOT EXISTS (SELECT 1 FROM account_assets aa WHERE aa.account_id=a.id)`, now, now); err != nil {
		return fmt.Errorf("backfill browser account assets: %w", err)
	}
	if _, err := tx.Exec(`UPDATE account_assets AS asset SET
		platform_code=(SELECT account.platform_code FROM accounts account WHERE account.id=asset.account_id),
		name=(SELECT account.name FROM accounts account WHERE account.id=asset.account_id),
		external_id=(SELECT COALESCE(account.account_identifier,'') FROM accounts account WHERE account.id=asset.account_id),
		avatar=(SELECT COALESCE(account.avatar,'') FROM accounts account WHERE account.id=asset.account_id),
		last_seen_at=CASE
			WHEN (SELECT COALESCE(account.last_check_at,'') FROM accounts account WHERE account.id=asset.account_id)<>''
			THEN (SELECT account.last_check_at FROM accounts account WHERE account.id=asset.account_id)
			ELSE asset.last_seen_at END,
		updated_at=?
		WHERE asset.asset_type='browser_account' AND EXISTS (
			SELECT 1 FROM accounts account WHERE account.id=asset.account_id AND (
				asset.platform_code<>account.platform_code OR asset.name<>account.name OR
				COALESCE(asset.external_id,'')<>COALESCE(account.account_identifier,'') OR
				COALESCE(asset.avatar,'')<>COALESCE(account.avatar,'') OR
				(COALESCE(account.last_check_at,'')<>'' AND COALESCE(asset.last_seen_at,'')<>account.last_check_at)
			)
		)`, now); err != nil {
		return fmt.Errorf("reconcile browser account assets: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	// Let SQLite refresh its planner statistics after schema changes. It is a
	// no-op when nothing changed and never blocks on a big table.
	if _, err := db.Exec("PRAGMA optimize;"); err != nil {
		return fmt.Errorf("optimize database: %w", err)
	}
	return nil
}

func ensureCreatorMetricManualHandoffState(tx *sql.Tx) error {
	var definition string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='creator_metric_collection_states'`).Scan(&definition); err != nil {
		return err
	}
	if strings.Contains(definition, "manual_takeover") {
		return nil
	}
	if _, err := tx.Exec(`ALTER TABLE creator_metric_collection_states RENAME TO creator_metric_collection_states_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(schemaCreatorMetricCollectionStates); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO creator_metric_collection_states
		(account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
		SELECT account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at
		FROM creator_metric_collection_states_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE creator_metric_collection_states_legacy`); err != nil {
		return err
	}
	return nil
}

func reconcileKnowledgeLatestSources(tx *sql.Tx) error {
	staleReady := `SELECT older.id FROM knowledge_documents older
		WHERE older.status='ready' AND EXISTS (
			SELECT 1 FROM knowledge_documents newer
			WHERE newer.base_id=older.base_id AND newer.source_name=older.source_name
				AND newer.status='ready' AND (
					newer.source_modified_ns>older.source_modified_ns OR
					(newer.source_modified_ns=older.source_modified_ns AND newer.id>older.id)
				)
		)`
	// 先取一遍再决定要不要动。knowledge_chunks_fts 的 document_id 没有索引，
	// DELETE ... WHERE document_id IN (子查询) 会全扫 FTS 表；而绝大多数启动
	// 根本没有 stale 文档（实测命中 0 行却要扫 19040 行、耗时 331ms）。
	rows, err := tx.Query(staleReady)
	if err != nil {
		return err
	}
	staleIDs := make([]int64, 0, 8)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		staleIDs = append(staleIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(staleIDs) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(staleIDs)), ",")
	args := make([]interface{}, len(staleIDs))
	for index, id := range staleIDs {
		args[index] = id
	}
	if _, err := tx.Exec(`DELETE FROM knowledge_chunks_fts WHERE document_id IN (`+placeholders+`)`, args...); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE knowledge_documents SET status='superseded',updated_at=CURRENT_TIMESTAMP
		WHERE id IN (`+placeholders+`)`, args...)
	return err
}

func ensureShopListingPlatformSchema(tx *sql.Tx) error {
	var definition string
	if err := tx.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='shop_listing_plans'`).Scan(&definition); err != nil {
		return err
	}
	if strings.Contains(definition, "'aliexpress'") {
		return nil
	}
	const legacyPlans = "shop_listing_plans_platform_legacy"
	const legacyItems = "shop_listing_items_platform_legacy"
	if _, err := tx.Exec(`ALTER TABLE shop_listing_plans RENAME TO ` + legacyPlans); err != nil {
		return err
	}
	if _, err := tx.Exec(schemaShopListingPlans); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO shop_listing_plans (
		id, identity_id, account_id, platform_code, snapshot_json, snapshot_hash,
		idempotency_key, status, concurrency, error_code, created_at, frozen_at, updated_at
	) SELECT id, identity_id, account_id, platform_code, snapshot_json, snapshot_hash,
		idempotency_key, status, concurrency, error_code, created_at, frozen_at, updated_at
		FROM ` + legacyPlans); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE shop_listing_items RENAME TO ` + legacyItems); err != nil {
		return err
	}
	if _, err := tx.Exec(schemaShopListingItems); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO shop_listing_items (
		id, plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at
	) SELECT id, plan_id, ordinal, payload_json, payload_hash, status, error_code, created_at, updated_at
		FROM ` + legacyItems); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE ` + legacyItems); err != nil {
		return err
	}
	_, err := tx.Exec(`DROP TABLE ` + legacyPlans)
	return err
}

func consolidateLegacyMetaAccounts(exec sqlExecer, now string) error {
	if _, err := exec.Exec(`INSERT INTO accounts (
		identity_id,platform_code,kind,name,login_state,sort,remark,enabled,source,created_at,updated_at
	)
	SELECT identity_id,'meta','browser_login','Meta',
		CASE
			WHEN MAX(CASE WHEN login_state='logged_in' THEN 1 ELSE 0 END)=1 THEN 'logged_in'
			WHEN MAX(CASE WHEN login_state='logged_out' THEN 1 ELSE 0 END)=1 THEN 'logged_out'
			ELSE 'unknown'
		END,
		MIN(sort),'',1,'meta_consolidation',?,?
	FROM accounts
	WHERE enabled=1 AND platform_code IN ('facebook','instagram')
	GROUP BY identity_id
	ON CONFLICT(identity_id,platform_code) DO UPDATE SET
		enabled=1,
		sort=MIN(accounts.sort,excluded.sort),
		login_state=CASE WHEN accounts.login_state='logged_in' OR excluded.login_state='logged_in' THEN 'logged_in' ELSE excluded.login_state END,
		updated_at=excluded.updated_at`, now, now); err != nil {
		return fmt.Errorf("consolidate Meta login account: %w", err)
	}
	if _, err := exec.Exec(`INSERT INTO account_assets (
		account_id,platform_code,asset_type,name,external_id,avatar,enabled,last_seen_at,created_at,updated_at
	)
	SELECT account.id,'meta','browser_account',account.name,'','',1,'',?,?
	FROM accounts account
	WHERE account.platform_code='meta'
		AND NOT EXISTS (SELECT 1 FROM account_assets asset WHERE asset.account_id=account.id AND asset.asset_type='browser_account')`, now, now); err != nil {
		return fmt.Errorf("seed Meta login asset: %w", err)
	}
	if _, err := exec.Exec(`UPDATE account_assets
		SET account_id=(SELECT meta.id FROM accounts meta WHERE meta.identity_id=(
			SELECT legacy.identity_id FROM accounts legacy WHERE legacy.id=account_assets.account_id
		) AND meta.platform_code='meta'), updated_at=?
		WHERE asset_type IN ('facebook_page','instagram_account')
			AND account_id IN (SELECT id FROM accounts WHERE platform_code IN ('facebook','instagram'))`, now); err != nil {
		return fmt.Errorf("move Meta publish assets: %w", err)
	}
	if _, err := exec.Exec(`UPDATE accounts SET enabled=0,updated_at=?
		WHERE enabled=1 AND platform_code IN ('facebook','instagram')
			AND EXISTS (SELECT 1 FROM accounts meta WHERE meta.identity_id=accounts.identity_id AND meta.platform_code='meta')`, now); err != nil {
		return fmt.Errorf("disable split Meta accounts: %w", err)
	}
	return nil
}

func normalizeLegacyAutomaticIdentityNames(tx *sql.Tx, now string) error {
	rows, err := tx.Query(`SELECT id,name FROM ego_identities ORDER BY sort,id`)
	if err != nil {
		return fmt.Errorf("scan legacy automatic browser names: %w", err)
	}
	type identityName struct {
		id   int64
		name string
	}
	identities := make([]identityName, 0)
	for rows.Next() {
		var identity identityName
		if err := rows.Scan(&identity.id, &identity.name); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan legacy automatic browser name: %w", err)
		}
		identities = append(identities, identity)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy automatic browser names: %w", err)
	}
	for index, identity := range identities {
		if !isLegacyAutomaticIdentityName(identity.name) {
			continue
		}
		if _, err := tx.Exec(`UPDATE ego_identities SET name=?,updated_at=? WHERE id=?`, browserName(index+1), now, identity.id); err != nil {
			return fmt.Errorf("rename legacy automatic browser: %w", err)
		}
	}
	return nil
}

func scrubLegacyGenerationReferenceURLs(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, references_json FROM generation_jobs WHERE instr(references_json, '"public_url"') > 0`)
	if err != nil {
		return fmt.Errorf("scan legacy generation references: %w", err)
	}
	type pendingUpdate struct {
		id   int64
		json string
	}
	updates := make([]pendingUpdate, 0)
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan legacy generation reference row: %w", err)
		}
		var references []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &references); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scrub legacy generation references for job %d: invalid JSON", id)
		}
		changed := false
		for _, reference := range references {
			if _, exists := reference["public_url"]; exists {
				delete(reference, "public_url")
				changed = true
			}
		}
		if !changed {
			continue
		}
		if references == nil {
			references = []map[string]json.RawMessage{}
		}
		encoded, err := json.Marshal(references)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("scrub legacy generation references for job %d: encode failed", id)
		}
		updates = append(updates, pendingUpdate{id: id, json: string(encoded)})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy generation reference scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan legacy generation references: %w", err)
	}
	for _, update := range updates {
		if _, err := tx.Exec(`UPDATE generation_jobs SET references_json=? WHERE id=?`, update.json, update.id); err != nil {
			return fmt.Errorf("scrub legacy generation references for job %d: update failed", update.id)
		}
	}
	return nil
}

func ensureColumn(tx *sql.Tx, table, column, definition string) error {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func ensurePublishPlanTargetsAssetUniqueness(tx *sql.Tx) error {
	var schema string
	if err := tx.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='publish_plan_targets'`).Scan(&schema); err != nil {
		return err
	}
	normalized := strings.NewReplacer(" ", "", "\n", "", "\t", "").Replace(strings.ToLower(schema))
	if !strings.Contains(normalized, "unique(plan_id,account_id)") {
		return nil
	}
	const legacyTable = "publish_plan_targets_account_unique"
	if _, err := tx.Exec(`ALTER TABLE publish_plan_targets RENAME TO ` + legacyTable); err != nil {
		return err
	}
	if _, err := tx.Exec(schemaPublishPlanTargets); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO publish_plan_targets (
			id, plan_id, account_id, asset_id, platform_code, variant_id, settings_json,
			status, publish_job_id, error_code, created_at, updated_at
		)
		SELECT id, plan_id, account_id, asset_id, platform_code, variant_id, settings_json,
			status, publish_job_id, error_code, created_at, updated_at
		FROM ` + legacyTable); err != nil {
		return err
	}
	_, err := tx.Exec(`DROP TABLE ` + legacyTable)
	return err
}

func ensureEgoMatrixSchema(tx *sql.Tx) error {
	var identityColumnCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('ego_spaces') WHERE name='identity_id'`).Scan(&identityColumnCount); err != nil {
		return err
	}
	if identityColumnCount > 0 {
		return nil
	}
	var accountColumnCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('ego_spaces') WHERE name='account_id'`).Scan(&accountColumnCount); err != nil {
		return err
	}
	if accountColumnCount == 0 {
		return fmt.Errorf("ego_spaces has no ownership column")
	}
	const legacyTable = "ego_spaces_account_owned"
	if _, err := tx.Exec(`ALTER TABLE ego_spaces RENAME TO ` + legacyTable); err != nil {
		return err
	}
	if _, err := tx.Exec(schemaEgoSpaces); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO ego_spaces (
			id,identity_id,space_id,task_id,name,ownership,state,control_epoch,active_job_id,
			last_seen_at,error_code,created_at,updated_at
		)
		SELECT legacy.id,accounts.identity_id,legacy.space_id,legacy.task_id,legacy.name,
			legacy.ownership,legacy.state,legacy.control_epoch,legacy.active_job_id,
			legacy.last_seen_at,legacy.error_code,legacy.created_at,legacy.updated_at
		FROM ` + legacyTable + ` legacy
		JOIN accounts ON accounts.id=legacy.account_id
		WHERE legacy.id=(
			SELECT MIN(candidate.id)
			FROM ` + legacyTable + ` candidate
			JOIN accounts candidate_account ON candidate_account.id=candidate.account_id
			WHERE candidate_account.identity_id=accounts.identity_id
		)`); err != nil {
		return err
	}
	_, err := tx.Exec(`DROP TABLE ` + legacyTable)
	return err
}

func (db *DB) SeedPlatforms() error {
	return seedPlatforms(db)
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func seedPlatforms(exec sqlExecer) error {
	now := time.Now().UTC().Format(time.RFC3339)
	stored := platforms.All()
	if meta, err := platforms.Find("meta"); err == nil {
		// Meta is one login identity backed by Facebook and Instagram publish
		// assets. Keep it available to account storage without duplicating the
		// two targets in the user-facing platform directory.
		stored = append(stored, meta)
	}
	for _, p := range stored {
		_, err := exec.Exec(`INSERT INTO platforms (
				code, name, icon, login_url, manager_url,
				supports_article, supports_graph_text, supports_video, sort,
				source, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'system', ?, ?)
			ON CONFLICT(code) DO UPDATE SET
				name=excluded.name,
				icon=excluded.icon,
				login_url=excluded.login_url,
				manager_url=excluded.manager_url,
				supports_article=excluded.supports_article,
				supports_graph_text=excluded.supports_graph_text,
				supports_video=excluded.supports_video,
				sort=excluded.sort,
				updated_at=excluded.updated_at`,
			p.Code, p.Name, p.Icon, p.LoginURL, p.ManagerURL, p.SupportsArticle, p.SupportsGraphText, p.SupportsVideo, p.Sort, now, now)
		if err != nil {
			return fmt.Errorf("seed platform %s: %w", p.Code, err)
		}
	}
	return nil
}

var migrations = []string{
	schemaPlatforms,
	schemaEgoIdentities,
	schemaAccounts,
	schemaShopListingPlans,
	schemaShopListingItems,
	schemaEgoSpaces,
	schemaEgoBookmarkImports,
	schemaEgoSessionMonitor,
	schemaAccountTags,
	schemaAccountTagLinks,
	schemaProxyEndpoints,
	schemaBrowserProfiles,
	schemaAccountAssets,
	schemaPlatformSettings,
	schemaContents,
	schemaShopContents,
	schemaShopContentMedia,
	schemaMediaAssets,
	schemaPublishJobs,
	schemaJobEvents,
	schemaBatchRuns,
	schemaBatchItems,
	schemaPublishRecords,
	schemaPublishContentRecords,
	schemaRecordInfos,
	schemaNotifications,
	schemaNotificationSettings,
	schemaNotificationChannels,
	schemaNotificationDeliveries,
	schemaLoginSessions,
	schemaAccountLoginEvents,
	schemaBrowserRuntimeInstances,
	schemaProfileLeases,
	schemaCDPPortLeases,
	schemaBrowserOperationLeases,
	schemaBrowserEvents,
	schemaAuditEvents,
	schemaOperationEvents,
	schemaTikHubAPICalls,
	schemaTaskQueue,
	schemaPostMetrics,
	schemaCreatorMetricSnapshots,
	schemaCreatorPostMetricSnapshots,
	schemaCreatorMetricCollectionStates,
	schemaCreatorMetricPreferences,
	schemaContentVersions,
	schemaContentVariants,
	schemaStudioScheduleHandoffs,
	schemaPublishPlans,
	schemaPublishPlanTargets,
	schemaApprovals,
	schemaCredentials,
	schemaMediaLibrary,
	schemaGenerationJobs,
	schemaAIImageProjects,
	schemaStudioScripts,
	schemaClipJobs,
	schemaAppUsers,
	schemaAgentConversations,
	schemaAgentBots,
	schemaAgentWorkspaceState,
	schemaMetricsCollectionSchedule,
	schemaAgentMessages,
	schemaAgentRuns,
	schemaAgentDelegations,
	schemaFeishuSync,
	schemaAgentEvents,
	schemaAgentProviderSessions,
	schemaAgentApprovals,
	schemaAgentProviderSelections,
	schemaAgentProviderCache,
	schemaAgentProviderModels,
	schemaAgentModelValidationBatches,
	schemaAgentProviderConfigs,
	schemaAgentSettings,
	schemaAgentToolCalls,
	schemaAgentKnowledgeTurns,
	schemaCreationWorkflows,
	schemaSkills,
	schemaFeedbackSubmissionIntents,
	schemaFeedbackReceipts,
	schemaKnowledgeBases,
	schemaKnowledgeImportRuns,
	schemaKnowledgeDocuments,
	schemaKnowledgeChunks,
	schemaShopKnowledgeBases,
	schemaShopKnowledgeImportRuns,
	schemaShopKnowledgeDocuments,
	schemaShopKnowledgeChunks,
	schemaContentKnowledgeCitations,
	schemaDataLinkSources,
	schemaDataLinkSnapshots,
	schemaCreationModelRoles,
	schemaCreationSessions,
	schemaCreationRuns,
	schemaFeishuContentExport,
	schemaFeishuContentRecordLinks,
	schemaRadarPoolTerms,
	schemaRadarCaptureRuns,
	schemaRadarCaptureCalls,
	schemaRadarWorks,
	schemaRadarObservations,
}

var schemaVirtualTables = []string{
	`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_chunks_fts USING fts5(
		chunk_id UNINDEXED,
		base_id UNINDEXED,
		document_id UNINDEXED,
		heading_path,
		content,
		tokenize='trigram'
	)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS shop_knowledge_chunks_fts USING fts5(
		chunk_id UNINDEXED,
		base_id UNINDEXED,
		document_id UNINDEXED,
		heading_path,
		content,
		tokenize='trigram'
	)`,
}

// schemaFeishuContentExport holds the single Feishu content-library binding:
// which Base and tables collected content is mirrored into, plus the last run.
const schemaFeishuContentExport = `CREATE TABLE IF NOT EXISTS feishu_content_export (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
	base_token TEXT NOT NULL DEFAULT '',
	base_url TEXT NOT NULL DEFAULT '',
	post_table_id TEXT NOT NULL DEFAULT '',
	metrics_table_id TEXT NOT NULL DEFAULT '',
	last_export_at TEXT NOT NULL DEFAULT '',
	last_result_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

// schemaFeishuContentRecordLinks remembers the Feishu record id of each
// exported business key so a later run updates instead of duplicating rows.
const schemaFeishuContentRecordLinks = `CREATE TABLE IF NOT EXISTS feishu_content_record_links (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	table_id TEXT NOT NULL,
	business_key TEXT NOT NULL,
	record_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(table_id, business_key)
)`

// The Trend Radar store keeps what used to be recomputed from raw provider
// JSON on every read: the pool vocabulary, each paid run, each call, each work
// and each metric observation. Attribution becomes a join instead of a string
// match, and "this work appeared how many times" becomes a count.
const schemaRadarPoolTerms = `CREATE TABLE IF NOT EXISTS radar_pool_terms (
	pool_id TEXT NOT NULL,
	pool_name TEXT NOT NULL DEFAULT '',
	term TEXT NOT NULL,
	normalized TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (pool_id, normalized)
)`

const schemaRadarCaptureRuns = `CREATE TABLE IF NOT EXISTS radar_capture_runs (
	id TEXT PRIMARY KEY,
	pool_id TEXT NOT NULL DEFAULT '',
	pool_name TEXT NOT NULL DEFAULT '',
	platform TEXT NOT NULL DEFAULT '',
	endpoint_id TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL DEFAULT '',
	finished_at TEXT NOT NULL DEFAULT '',
	last_retry_at TEXT NOT NULL DEFAULT '',
	terms_json TEXT NOT NULL DEFAULT '[]',
	item_cap INTEGER NOT NULL DEFAULT 0,
	max_calls INTEGER NOT NULL DEFAULT 0,
	estimated_cost_usd REAL NOT NULL DEFAULT 0,
	cost_basis TEXT NOT NULL DEFAULT '',
	total_calls INTEGER NOT NULL DEFAULT 0,
	succeeded INTEGER NOT NULL DEFAULT 0,
	failed INTEGER NOT NULL DEFAULT 0,
	skipped INTEGER NOT NULL DEFAULT 0,
	kept INTEGER NOT NULL DEFAULT 0,
	items INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
)`

const schemaRadarCaptureCalls = `CREATE TABLE IF NOT EXISTS radar_capture_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id TEXT NOT NULL DEFAULT '',
	call_id INTEGER NOT NULL DEFAULT 0,
	term TEXT NOT NULL DEFAULT '',
	platform TEXT NOT NULL DEFAULT '',
	endpoint_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	error_code TEXT NOT NULL DEFAULT '',
	error_message TEXT NOT NULL DEFAULT '',
	item_count INTEGER NOT NULL DEFAULT 0,
	kept_count INTEGER NOT NULL DEFAULT 0,
	ad_count INTEGER NOT NULL DEFAULT 0,
	guide_count INTEGER NOT NULL DEFAULT 0,
	unreadable_count INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	raw_path TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
)`

const schemaRadarWorks = `CREATE TABLE IF NOT EXISTS radar_works (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	platform TEXT NOT NULL,
	work_id TEXT NOT NULL,
	pool_id TEXT NOT NULL DEFAULT '',
	pool_name TEXT NOT NULL DEFAULT '',
	term TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	url TEXT NOT NULL DEFAULT '',
	published_at TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	last_seen_at TEXT NOT NULL,
	seen_count INTEGER NOT NULL DEFAULT 1,
	UNIQUE(platform, work_id)
)`

const schemaRadarObservations = `CREATE TABLE IF NOT EXISTS radar_observations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	work_pk INTEGER NOT NULL REFERENCES radar_works(id) ON DELETE CASCADE,
	platform TEXT NOT NULL DEFAULT '',
	work_id TEXT NOT NULL DEFAULT '',
	pool_id TEXT NOT NULL DEFAULT '',
	pool_name TEXT NOT NULL DEFAULT '',
	term TEXT NOT NULL DEFAULT '',
	call_id INTEGER NOT NULL DEFAULT 0,
	run_id TEXT NOT NULL DEFAULT '',
	play INTEGER NOT NULL DEFAULT 0,
	likes INTEGER NOT NULL DEFAULT 0,
	collects INTEGER NOT NULL DEFAULT 0,
	shares INTEGER NOT NULL DEFAULT 0,
	comments INTEGER NOT NULL DEFAULT 0,
	observed_at TEXT NOT NULL,
	UNIQUE(work_pk, call_id, observed_at)
)`

var schemaIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_feishu_sync_module ON feishu_sync(module, synced_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_delegations_parent ON agent_delegations(parent_conversation_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_delegations_status ON agent_delegations(status, updated_at)`,
	// One in-flight handoff per (transcript, bot): the DB is the last line of
	// defense when two requests race past the app-level check.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_delegations_running ON agent_delegations(parent_conversation_id, to_bot_id) WHERE status='running'`,
	`CREATE INDEX IF NOT EXISTS idx_radar_observations_work ON radar_observations(work_pk, observed_at)`,
	`CREATE INDEX IF NOT EXISTS idx_radar_observations_term ON radar_observations(pool_id, term)`,
	`CREATE INDEX IF NOT EXISTS idx_radar_calls_run ON radar_capture_calls(run_id)`,
	`CREATE INDEX IF NOT EXISTS idx_radar_works_pool ON radar_works(pool_id, first_seen_at)`,
	`CREATE INDEX IF NOT EXISTS idx_radar_works_published ON radar_works(published_at)`,
	`CREATE INDEX IF NOT EXISTS idx_accounts_platform ON accounts(platform_code)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_identity_platform ON accounts(identity_id, platform_code)`,
	`CREATE INDEX IF NOT EXISTS idx_accounts_identity ON accounts(identity_id, sort, id)`,
	`CREATE INDEX IF NOT EXISTS idx_ego_spaces_state ON ego_spaces(state, id)`,
	`CREATE INDEX IF NOT EXISTS idx_account_tag_links_tag ON account_tag_links(tag_id, account_id)`,
	`CREATE INDEX IF NOT EXISTS idx_browser_profiles_account ON browser_profiles(account_id)`,
	`CREATE INDEX IF NOT EXISTS idx_browser_profiles_proxy ON browser_profiles(proxy_id)`,
	`CREATE INDEX IF NOT EXISTS idx_proxy_endpoints_enabled ON proxy_endpoints(enabled, id)`,
	`CREATE INDEX IF NOT EXISTS idx_account_assets_account ON account_assets(account_id)`,
	`CREATE INDEX IF NOT EXISTS idx_account_assets_platform ON account_assets(platform_code)`,
	`CREATE INDEX IF NOT EXISTS idx_platform_settings_platform ON platform_settings(platform_code)`,
	`CREATE INDEX IF NOT EXISTS idx_media_assets_content ON media_assets(content_id)`,
	`CREATE INDEX IF NOT EXISTS idx_shop_contents_planned ON shop_contents(planned_at, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_shop_content_media_content ON shop_content_media(content_id, role, position, id)`,
	`CREATE INDEX IF NOT EXISTS idx_media_assets_library ON media_assets(library_id)`,
	`CREATE INDEX IF NOT EXISTS idx_media_library_purpose ON media_library(purpose, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_shop_knowledge_documents_base ON shop_knowledge_documents(base_id, updated_at DESC, id DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_shop_knowledge_documents_base_sha ON shop_knowledge_documents(base_id, sha256)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_shop_knowledge_chunks_document_ordinal ON shop_knowledge_chunks(document_id, ordinal)`,
	`CREATE INDEX IF NOT EXISTS idx_shop_knowledge_import_runs_base ON shop_knowledge_import_runs(base_id, created_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_studio_scripts_type ON studio_scripts(content_type, updated_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_batch_items_batch ON batch_items(batch_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_jobs_batch ON publish_jobs(batch_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_jobs_content ON publish_jobs(content_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_jobs_account ON publish_jobs(account_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_jobs_platform ON publish_jobs(platform_code)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_jobs_trace ON publish_jobs(trace_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_jobs_plan_target ON publish_jobs(plan_id, plan_target_id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_publish_jobs_idempotency ON publish_jobs(idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key <> ''`,
	`CREATE INDEX IF NOT EXISTS idx_job_events_job ON job_events(job_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_job_events_trace ON job_events(trace_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_records_job ON publish_records(job_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_records_content ON publish_records(content_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_records_account ON publish_records(account_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_records_platform ON publish_records(platform_code)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_publish_content_records_job ON publish_content_records(job_id)`,
	`CREATE INDEX IF NOT EXISTS idx_publish_content_records_state ON publish_content_records(remote_state, updated_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_feedback_receipts_created ON feedback_receipts(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_record_infos_record ON record_infos(publish_record_id)`,
	`CREATE INDEX IF NOT EXISTS idx_record_infos_account ON record_infos(account_id)`,
	`CREATE INDEX IF NOT EXISTS idx_record_infos_platform ON record_infos(platform_code)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_events_trace ON audit_events(trace_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_operation_events_trace ON operation_events(trace_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_operation_events_account ON operation_events(account_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_tikhub_api_calls_platform_created ON tikhub_api_calls(platform_code, created_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_tikhub_api_calls_endpoint_created ON tikhub_api_calls(endpoint_id, created_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_task_queue_ready ON task_queue(status, run_after, priority, id)`,
	`CREATE INDEX IF NOT EXISTS idx_task_queue_lease ON task_queue(status, lease_expires_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_creator_metric_snapshots_date ON creator_metric_snapshots(snapshot_date DESC, account_id)`,
	`CREATE INDEX IF NOT EXISTS idx_creator_metric_snapshots_platform ON creator_metric_snapshots(platform_code, snapshot_date DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_creator_post_metric_snapshots_snapshot ON creator_post_metric_snapshots(snapshot_id, views DESC, id)`,
	`CREATE INDEX IF NOT EXISTS idx_creator_post_metric_snapshots_account ON creator_post_metric_snapshots(account_id, platform_code, published_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_creator_metric_collection_states_platform ON creator_metric_collection_states(platform_code, collected_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_cdp_port_leases_expiry ON cdp_port_leases(expires_at, cdp_port)`,
	`CREATE INDEX IF NOT EXISTS idx_browser_operation_leases_expiry ON browser_operation_leases(expires_at, profile_id)`,
	`CREATE INDEX IF NOT EXISTS idx_login_sessions_account_status ON login_sessions(account_id, status, id)`,
	`CREATE INDEX IF NOT EXISTS idx_account_login_events_account_observed ON account_login_events(account_id, observed_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_notification_deliveries_account ON notification_deliveries(account_id, kind, status, created_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_clip_jobs_created ON clip_jobs(created_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_clip_jobs_source_media ON clip_jobs(source_media_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_generation_jobs_recoverable ON generation_jobs(status, created_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_generation_jobs_recent ON generation_jobs(created_at DESC, id DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_generation_jobs_request_id ON generation_jobs(request_id) WHERE request_id <> ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_content_variants_platform_default ON content_variants(content_id, platform_code) WHERE account_id IS NULL`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_content_variants_platform_account ON content_variants(content_id, platform_code, account_id) WHERE account_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_publish_plan_targets_plan ON publish_plan_targets(plan_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_studio_schedule_handoffs_workflow ON studio_schedule_handoffs(workflow_id, updated_at DESC, id DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_studio_schedule_handoffs_ready_workflow ON studio_schedule_handoffs(workflow_id) WHERE status='ready'`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_publish_plans_studio_handoff ON publish_plans(studio_handoff_id) WHERE studio_handoff_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_approvals_expiry ON approvals(expires_at, consumed_at)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_conversations_updated ON agent_conversations(updated_at DESC, id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_conversations_provider_updated ON agent_conversations(provider_id, updated_at DESC, id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_conversations_workspace_mode_updated ON agent_conversations(workspace_mode, updated_at DESC, id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_conversations_bot_updated ON agent_conversations(bot_id, updated_at DESC, id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_bots_pinned ON agent_bots(pinned, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_bots_group ON agent_bots(group_name)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_messages_conversation ON agent_messages(conversation_id, created_at, id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_runs_active_conversation ON agent_runs(conversation_id) WHERE status='running'`,
	`CREATE INDEX IF NOT EXISTS idx_agent_events_run_seq ON agent_events(run_id, seq)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_approvals_run_status ON agent_approvals(run_id, status)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_hash ON agent_tool_calls(run_id, params_hash)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_status ON agent_tool_calls(status, expires_at)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_knowledge_turns_conversation ON agent_knowledge_turns(conversation_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_creation_workflows_updated ON creation_workflows(updated_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_skills_enabled ON skills(enabled, status, id)`,
	`CREATE INDEX IF NOT EXISTS idx_knowledge_documents_base ON knowledge_documents(base_id, updated_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_knowledge_documents_source_versions ON knowledge_documents(base_id, source_name, source_modified_ns DESC, id DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_documents_base_sha ON knowledge_documents(base_id, sha256)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_documents_ready_source ON knowledge_documents(base_id, source_name) WHERE status='ready'`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_chunks_document_ordinal ON knowledge_chunks(document_id, ordinal)`,
	`CREATE INDEX IF NOT EXISTS idx_knowledge_import_runs_base ON knowledge_import_runs(base_id, created_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_content_knowledge_citations_content ON content_knowledge_citations(content_id, ordinal)`,
	`CREATE INDEX IF NOT EXISTS idx_data_link_sources_updated ON data_link_sources(updated_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_data_link_snapshots_source ON data_link_snapshots(source_id, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_data_link_snapshots_status ON data_link_snapshots(status, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_creation_runs_session ON creation_runs(session_id, created_at, id)`,
}

// Additive triggers preserve relational guarantees when upgrading legacy
// databases whose original CREATE TABLE statements did not include FKs.
// SQLite serializes the duplicate-record trigger with the insert itself, so it
// also closes the SELECT-then-INSERT race across processes.
var schemaTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS trg_media_assets_library_insert
	BEFORE INSERT ON media_assets
	WHEN NEW.library_id IS NULL
	  OR NOT EXISTS (SELECT 1 FROM media_library WHERE id=NEW.library_id)
	BEGIN SELECT RAISE(ABORT, 'media_assets library link required'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_media_assets_library_update
	BEFORE UPDATE OF library_id ON media_assets
	WHEN NEW.library_id IS NULL
	  OR NOT EXISTS (SELECT 1 FROM media_library WHERE id=NEW.library_id)
	BEGIN SELECT RAISE(ABORT, 'media_assets library link required'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_accounts_platform_insert
	BEFORE INSERT ON accounts
	WHEN NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
	BEGIN SELECT RAISE(ABORT, 'accounts platform foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_accounts_platform_update
	BEFORE UPDATE OF platform_code ON accounts
	WHEN NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
	BEGIN SELECT RAISE(ABORT, 'accounts platform foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_refs_insert
	BEFORE INSERT ON publish_jobs
	WHEN NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
	  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
	  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
	  OR (NEW.batch_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM batch_runs WHERE id=NEW.batch_id))
	BEGIN SELECT RAISE(ABORT, 'publish_jobs foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_refs_update
	BEFORE UPDATE OF batch_id, content_id, account_id, platform_code ON publish_jobs
	WHEN NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
	  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
	  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
	  OR (NEW.batch_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM batch_runs WHERE id=NEW.batch_id))
	BEGIN SELECT RAISE(ABORT, 'publish_jobs foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_frozen_link_insert
	BEFORE INSERT ON publish_jobs
	WHEN (
		(COALESCE(NEW.plan_id,0)=0 OR COALESCE(NEW.plan_target_id,0)=0 OR COALESCE(NEW.snapshot_hash,'')='')
		AND NOT (COALESCE(NEW.plan_id,0)=0 AND COALESCE(NEW.plan_target_id,0)=0 AND COALESCE(NEW.snapshot_hash,'')='')
	) OR (
		COALESCE(NEW.plan_id,0)>0 AND NOT EXISTS (
			SELECT 1 FROM publish_plans AS plan
			JOIN publish_plan_targets AS target ON target.plan_id=plan.id
			WHERE plan.id=NEW.plan_id AND target.id=NEW.plan_target_id
			  AND plan.content_id=NEW.content_id AND plan.snapshot_hash=NEW.snapshot_hash
			  AND target.account_id=NEW.account_id
			  AND target.asset_id=COALESCE(NEW.asset_id,0)
			  AND target.platform_code=NEW.platform_code
		)
	)
	BEGIN SELECT RAISE(ABORT, 'publish_jobs frozen snapshot link'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_frozen_link_immutable
	BEFORE UPDATE OF plan_id, plan_target_id, snapshot_hash ON publish_jobs
	WHEN COALESCE(OLD.plan_id,0)<>COALESCE(NEW.plan_id,0)
	  OR COALESCE(OLD.plan_target_id,0)<>COALESCE(NEW.plan_target_id,0)
	  OR COALESCE(OLD.snapshot_hash,'')<>COALESCE(NEW.snapshot_hash,'')
	BEGIN SELECT RAISE(ABORT, 'publish_jobs frozen snapshot link is immutable'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_submit_marker_immutable
	BEFORE UPDATE OF submit_attempted_at ON publish_jobs
	WHEN COALESCE(OLD.submit_attempted_at,'')<>''
	  AND COALESCE(NEW.submit_attempted_at,'')<>COALESCE(OLD.submit_attempted_at,'')
	  AND NOT (
		OLD.mode='submit' AND NEW.mode='submit'
		AND OLD.status='running' AND NEW.status='manual_takeover'
		AND OLD.current_step='submit_approved' AND NEW.current_step='submit_not_clicked'
		AND OLD.submit_outcome='attempted' AND NEW.submit_outcome='not_clicked'
		AND COALESCE(NEW.submit_attempted_at,'')=''
	  )
	BEGIN SELECT RAISE(ABORT, 'publish_jobs submit marker is immutable'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_plans_snapshot_immutable
	BEFORE UPDATE OF content_id, media_mode, snapshot_json, snapshot_hash, concurrency ON publish_plans
	WHEN OLD.content_id<>NEW.content_id
	  OR OLD.media_mode<>NEW.media_mode
	  OR OLD.snapshot_json<>NEW.snapshot_json
	  OR OLD.snapshot_hash<>NEW.snapshot_hash
	  OR OLD.concurrency<>NEW.concurrency
	BEGIN SELECT RAISE(ABORT, 'publish plan snapshot is immutable'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_job_events_job_insert
	BEFORE INSERT ON job_events
	WHEN NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id)
	BEGIN SELECT RAISE(ABORT, 'job_events job foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_job_events_job_update
	BEFORE UPDATE OF job_id ON job_events
	WHEN NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id)
	BEGIN SELECT RAISE(ABORT, 'job_events job foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_records_refs_insert
	BEFORE INSERT ON publish_records
	WHEN (NEW.job_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id))
	  OR NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
	  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
	  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
	BEGIN SELECT RAISE(ABORT, 'publish_records foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_records_refs_update
	BEFORE UPDATE OF job_id, content_id, account_id, platform_code ON publish_records
	WHEN (NEW.job_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id))
	  OR NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
	  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
	  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
	BEGIN SELECT RAISE(ABORT, 'publish_records foreign key'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_records_unique_job
	BEFORE INSERT ON publish_records
	WHEN NEW.job_id IS NOT NULL AND EXISTS (SELECT 1 FROM publish_records WHERE job_id=NEW.job_id)
	BEGIN SELECT RAISE(ABORT, 'duplicate publish record job'); END`,
	`CREATE TRIGGER IF NOT EXISTS trg_publish_records_verified_insert
	BEFORE INSERT ON publish_records
	WHEN NEW.job_id IS NULL
	  OR NEW.status<>'published'
	  OR NOT EXISTS (
		SELECT 1 FROM publish_jobs
		WHERE id=NEW.job_id AND status IN ('published','published_verified')
		  AND current_step='creator_backend_verified'
		  AND submit_outcome='verified_published'
	  )
	BEGIN SELECT RAISE(ABORT, 'publish record requires creator backend evidence'); END`,
}

const schemaPlatforms = `CREATE TABLE IF NOT EXISTS platforms (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  icon TEXT,
  login_url TEXT,
  manager_url TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  supports_article INTEGER NOT NULL DEFAULT 0,
  supports_graph_text INTEGER NOT NULL DEFAULT 0,
  supports_video INTEGER NOT NULL DEFAULT 0,
  sort INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT 'system',
  source_platform_id INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaKnowledgeBases = `CREATE TABLE IF NOT EXISTS knowledge_bases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  top_k INTEGER NOT NULL DEFAULT 6,
  max_context_chars INTEGER NOT NULL DEFAULT 12000,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaKnowledgeImportRuns = `CREATE TABLE IF NOT EXISTS knowledge_import_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'running',
  total_files INTEGER NOT NULL DEFAULT 0,
  imported_files INTEGER NOT NULL DEFAULT 0,
  skipped_files INTEGER NOT NULL DEFAULT 0,
  failed_files INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT ''
);`

const schemaKnowledgeDocuments = `CREATE TABLE IF NOT EXISTS knowledge_documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  import_run_id INTEGER REFERENCES knowledge_import_runs(id) ON DELETE SET NULL,
  source_name TEXT NOT NULL,
  source_type TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  status TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  original_path TEXT NOT NULL DEFAULT '',
  normalized_path TEXT NOT NULL DEFAULT '',
  source_size_bytes INTEGER NOT NULL DEFAULT 0,
  source_modified_ns INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaKnowledgeChunks = `CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  document_id INTEGER NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  heading_path TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  char_count INTEGER NOT NULL,
  created_at TEXT NOT NULL
);`

const schemaShopKnowledgeBases = `CREATE TABLE IF NOT EXISTS shop_knowledge_bases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  top_k INTEGER NOT NULL DEFAULT 6,
  max_context_chars INTEGER NOT NULL DEFAULT 12000,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaShopKnowledgeImportRuns = `CREATE TABLE IF NOT EXISTS shop_knowledge_import_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES shop_knowledge_bases(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'running',
  total_files INTEGER NOT NULL DEFAULT 0,
  imported_files INTEGER NOT NULL DEFAULT 0,
  skipped_files INTEGER NOT NULL DEFAULT 0,
  failed_files INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT ''
);`

const schemaShopKnowledgeDocuments = `CREATE TABLE IF NOT EXISTS shop_knowledge_documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES shop_knowledge_bases(id) ON DELETE CASCADE,
  import_run_id INTEGER REFERENCES shop_knowledge_import_runs(id) ON DELETE SET NULL,
  source_name TEXT NOT NULL,
  source_type TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  status TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  original_path TEXT NOT NULL DEFAULT '',
  normalized_path TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaShopKnowledgeChunks = `CREATE TABLE IF NOT EXISTS shop_knowledge_chunks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES shop_knowledge_bases(id) ON DELETE CASCADE,
  document_id INTEGER NOT NULL REFERENCES shop_knowledge_documents(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  heading_path TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  char_count INTEGER NOT NULL,
  created_at TEXT NOT NULL
);`

const schemaContentKnowledgeCitations = `CREATE TABLE IF NOT EXISTS content_knowledge_citations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  chunk_id INTEGER NOT NULL CHECK (chunk_id > 0),
  base_id INTEGER NOT NULL CHECK (base_id > 0),
  document_sha256 TEXT NOT NULL CHECK (length(document_sha256) = 64),
  source_name TEXT NOT NULL,
  heading_path TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(content_id, ordinal),
  UNIQUE(content_id, chunk_id, document_sha256)
);`

const schemaDataLinkSources = `CREATE TABLE IF NOT EXISTS data_link_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL CHECK (kind IN ('sqlite')),
  name TEXT NOT NULL,
  original_name TEXT NOT NULL,
  file_path TEXT NOT NULL,
  file_size INTEGER NOT NULL CHECK (file_size > 0),
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  status TEXT NOT NULL CHECK (status IN ('ready','failed')),
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(kind, sha256)
);`

const schemaDataLinkSnapshots = `CREATE TABLE IF NOT EXISTS data_link_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES data_link_sources(id) ON DELETE RESTRICT,
  object_name TEXT NOT NULL DEFAULT '',
  query_sha256 TEXT NOT NULL CHECK (length(query_sha256) = 64),
  schema_fingerprint TEXT NOT NULL CHECK (length(schema_fingerprint) = 64),
  mapping_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL CHECK (status IN ('running','completed','failed')),
  row_count INTEGER NOT NULL DEFAULT 0,
  byte_count INTEGER NOT NULL DEFAULT 0,
  checksum TEXT NOT NULL DEFAULT '',
  data_path TEXT NOT NULL DEFAULT '',
  review_path TEXT NOT NULL DEFAULT '',
  quality_json TEXT NOT NULL DEFAULT '{}',
  truncated INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0,1)),
  error_code TEXT NOT NULL DEFAULT '',
  knowledge_base_id INTEGER REFERENCES knowledge_bases(id) ON DELETE SET NULL,
  knowledge_document_id INTEGER REFERENCES knowledge_documents(id) ON DELETE SET NULL,
  confirmed_at TEXT NOT NULL DEFAULT '',
  collected_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaCreationModelRoles = `CREATE TABLE IF NOT EXISTS creation_model_roles (
  role TEXT PRIMARY KEY CHECK (role IN ('conversation','copywriter','reviewer')),
  provider_id TEXT NOT NULL,
  model_id TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaCreationSessions = `CREATE TABLE IF NOT EXISTS creation_sessions (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  topic_brief TEXT NOT NULL DEFAULT '',
  style TEXT NOT NULL DEFAULT '',
  platform TEXT NOT NULL DEFAULT '',
  sop_stage TEXT NOT NULL DEFAULT 'topic' CHECK (sop_stage IN ('topic','evidence','copy','script','review','save')),
  knowledge_base_ids_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaCreationRuns = `CREATE TABLE IF NOT EXISTS creation_runs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES creation_sessions(id) ON DELETE CASCADE,
  action TEXT NOT NULL CHECK (action IN ('topic','draft','review','revise')),
  provider_id TEXT NOT NULL DEFAULT '',
  model_id TEXT NOT NULL DEFAULT '',
  used_fallback INTEGER NOT NULL DEFAULT 0 CHECK (used_fallback IN (0,1)),
  prompt_summary TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL,
  citations_json TEXT NOT NULL DEFAULT '{}',
  quality_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);`

const schemaEgoIdentities = `CREATE TABLE IF NOT EXISTS ego_identities (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort INTEGER NOT NULL DEFAULT 0,
  profile_id TEXT NOT NULL DEFAULT '',
  profile_name TEXT NOT NULL DEFAULT '',
  avatar TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaAccounts = `CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  identity_id INTEGER NOT NULL DEFAULT 1,
  platform_code TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'browser_login',
  name TEXT NOT NULL,
  avatar TEXT,
  host TEXT,
  account_identifier TEXT,
  manager_addr TEXT,
  login_state TEXT NOT NULL DEFAULT 'unknown',
  login_time TEXT,
  last_check_at TEXT,
  last_focused_at TEXT NOT NULL DEFAULT '',
  sort INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL DEFAULT 'manual',
  source_account_id INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (identity_id) REFERENCES ego_identities(id) ON DELETE RESTRICT,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaShopListingPlans = `CREATE TABLE IF NOT EXISTS shop_listing_plans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  identity_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL CHECK (platform_code IN ('douyin_shop','taobao','tiktok_shop','shopee','shopify','amazon_seller','jd_shop','pinduoduo_shop','xhs_shop','kuaishou_shop','wechat_shop','alibaba_1688','aliexpress')),
  snapshot_json TEXT NOT NULL,
  snapshot_hash TEXT NOT NULL CHECK (length(snapshot_hash) = 64),
  idempotency_key TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL CHECK (status IN ('frozen','dry_run_ok','prepare_approved','manual_takeover')),
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK (concurrency = 1),
  error_code TEXT NOT NULL DEFAULT '' CHECK (error_code = '' OR (length(error_code) BETWEEN 1 AND 64 AND substr(error_code,1,1) GLOB '[A-Z]' AND error_code NOT GLOB '*[^A-Z_]*')),
  created_at TEXT NOT NULL,
  frozen_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (identity_id) REFERENCES ego_identities(id) ON DELETE RESTRICT,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT
);`

const schemaShopListingItems = `CREATE TABLE IF NOT EXISTS shop_listing_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id INTEGER NOT NULL,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  payload_json TEXT NOT NULL,
  payload_hash TEXT NOT NULL CHECK (length(payload_hash) = 64),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','manual_takeover')),
  error_code TEXT NOT NULL DEFAULT '' CHECK (error_code = '' OR (length(error_code) BETWEEN 1 AND 64 AND substr(error_code,1,1) GLOB '[A-Z]' AND error_code NOT GLOB '*[^A-Z_]*')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (plan_id) REFERENCES shop_listing_plans(id) ON DELETE CASCADE,
  UNIQUE (plan_id, ordinal)
);`

const schemaEgoSpaces = `CREATE TABLE IF NOT EXISTS ego_spaces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  identity_id INTEGER NOT NULL UNIQUE,
  space_id INTEGER NOT NULL UNIQUE,
  task_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  ownership TEXT NOT NULL DEFAULT 'agent',
  state TEXT NOT NULL DEFAULT 'ready',
  control_epoch INTEGER NOT NULL DEFAULT 1,
  active_job_id INTEGER,
  last_seen_at TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (identity_id) REFERENCES ego_identities(id) ON DELETE CASCADE,
  FOREIGN KEY (active_job_id) REFERENCES publish_jobs(id) ON DELETE SET NULL
);`

const schemaEgoBookmarkImports = `CREATE TABLE IF NOT EXISTS ego_bookmark_imports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  version INTEGER NOT NULL UNIQUE,
  status TEXT NOT NULL,
  mode TEXT NOT NULL,
  path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaEgoSessionMonitor = `CREATE TABLE IF NOT EXISTS ego_session_monitor (
  id INTEGER PRIMARY KEY CHECK (id=1),
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  interval_minutes INTEGER NOT NULL DEFAULT 60 CHECK (interval_minutes BETWEEN 1 AND 1440),
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK (concurrency=1),
  state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle','running')),
  last_started_at TEXT NOT NULL DEFAULT '',
  last_finished_at TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_result_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);`

const schemaAccountTags = `CREATE TABLE IF NOT EXISTS account_tags (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  color TEXT NOT NULL,
  position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaAccountTagLinks = `CREATE TABLE IF NOT EXISTS account_tag_links (
  account_id INTEGER NOT NULL,
  tag_id INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (account_id, tag_id),
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (tag_id) REFERENCES account_tags(id) ON DELETE CASCADE
);`

const schemaAccountLoginEvents = `CREATE TABLE IF NOT EXISTS account_login_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  auth_state TEXT NOT NULL CHECK (auth_state IN ('logged_in', 'logged_out', 'unknown')),
  persisted_login_state TEXT NOT NULL CHECK (persisted_login_state IN ('logged_in', 'logged_out', 'unknown')),
  browser_state TEXT NOT NULL,
  login_mode TEXT NOT NULL DEFAULT 'unknown',
  auth_method TEXT NOT NULL DEFAULT 'unknown',
  reason_code TEXT NOT NULL,
  evidence_code TEXT NOT NULL,
  page_origin TEXT NOT NULL DEFAULT '',
  profile_name TEXT NOT NULL DEFAULT '',
  account_identifier TEXT NOT NULL DEFAULT '',
  avatar_source TEXT NOT NULL DEFAULT 'platform_fallback',
  avatar_sha256 TEXT NOT NULL DEFAULT '',
  avatar_error_code TEXT NOT NULL DEFAULT '',
  profile_changed INTEGER NOT NULL DEFAULT 0,
  trace_id TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);`

const schemaBrowserProfiles = `CREATE TABLE IF NOT EXISTS browser_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  profile_dir TEXT NOT NULL,
  browser_type TEXT NOT NULL DEFAULT 'fingerprint-chromium',
  fingerprint_seed INTEGER NOT NULL,
  lang TEXT NOT NULL DEFAULT 'en-US',
  accept_lang TEXT NOT NULL DEFAULT 'en-US,en',
  timezone TEXT NOT NULL DEFAULT 'America/Los_Angeles',
  proxy_id INTEGER,
  cookie_encrypted TEXT,
  cookie_import_state TEXT NOT NULL DEFAULT 'none',
  last_cdp_port INTEGER,
  last_cdp_url TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (proxy_id) REFERENCES proxy_endpoints(id) ON DELETE RESTRICT
);`

// Proxy endpoints intentionally contain no authentication material. A future
// credential integration must use an OS keychain reference rather than SQLite.
const schemaProxyEndpoints = `CREATE TABLE IF NOT EXISTS proxy_endpoints (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  server TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaAccountAssets = `CREATE TABLE IF NOT EXISTS account_assets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  asset_type TEXT NOT NULL,
  name TEXT NOT NULL,
  external_id TEXT,
  avatar TEXT,
  connected_asset_id INTEGER,
  connected_asset_name TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_seen_at TEXT,
  raw_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaPlatformSettings = `CREATE TABLE IF NOT EXISTS platform_settings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  platform_code TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  settings_json TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  is_default INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT 'manual',
  source_setting_id INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaContents = `CREATE TABLE IF NOT EXISTS contents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  title TEXT,
  caption TEXT,
  body TEXT,
  tags_json TEXT,
  status TEXT NOT NULL DEFAULT 'draft',
  publish_count INTEGER NOT NULL DEFAULT 0,
  raw_publish_data TEXT,
  planned_at TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL DEFAULT 'manual',
  source_content_id INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaShopContents = `CREATE TABLE IF NOT EXISTS shop_contents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL CHECK (type IN ('article','graph_text','video')),
  title TEXT NOT NULL DEFAULT '',
  caption TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
  planned_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaShopContentMedia = `CREATE TABLE IF NOT EXISTS shop_content_media (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL REFERENCES shop_contents(id) ON DELETE CASCADE,
  library_id INTEGER NOT NULL REFERENCES media_library(id) ON DELETE RESTRICT,
  role TEXT NOT NULL DEFAULT 'content' CHECK (role IN ('content','cover')),
  position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
  created_at TEXT NOT NULL,
  UNIQUE(content_id, library_id, role)
);`

const schemaMediaAssets = `CREATE TABLE IF NOT EXISTS media_assets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER,
	library_id INTEGER,
  role TEXT NOT NULL DEFAULT 'content' CHECK (role IN ('content','cover')),
  position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
  kind TEXT NOT NULL,
  file_path TEXT NOT NULL,
  original_path TEXT,
  mime_type TEXT,
  size_bytes INTEGER,
  sha256 TEXT,
  width INTEGER,
  height INTEGER,
  duration_ms INTEGER,
  source TEXT NOT NULL DEFAULT 'manual',
  created_at TEXT NOT NULL,
  FOREIGN KEY (content_id) REFERENCES contents(id),
	FOREIGN KEY (library_id) REFERENCES media_library(id) ON DELETE RESTRICT
);`

const schemaPublishJobs = `CREATE TABLE IF NOT EXISTS publish_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER,
  content_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER,
  platform_code TEXT NOT NULL,
  content_type TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'browser',
  status TEXT NOT NULL,
  current_step TEXT,
  settings_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  retry_count INTEGER NOT NULL DEFAULT 0,
  max_retries INTEGER NOT NULL DEFAULT 2,
  error_code TEXT,
  error_message TEXT,
  trace_id TEXT,
  idempotency_key TEXT,
  cancel_requested_at TEXT,
  plan_id INTEGER,
  plan_target_id INTEGER,
  snapshot_hash TEXT NOT NULL DEFAULT '',
  submit_attempted_at TEXT NOT NULL DEFAULT '',
  submit_outcome TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (batch_id) REFERENCES batch_runs(id),
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code),
  FOREIGN KEY (plan_id) REFERENCES publish_plans(id),
  FOREIGN KEY (plan_target_id) REFERENCES publish_plan_targets(id)
);`

const schemaJobEvents = `CREATE TABLE IF NOT EXISTS job_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER NOT NULL,
  level TEXT NOT NULL,
  step TEXT,
  event_type TEXT NOT NULL,
  message TEXT,
  screenshot_path TEXT,
  dom_snapshot_path TEXT,
  raw_json TEXT,
  trace_id TEXT,
  request_id TEXT,
  task_id TEXT,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  error_code TEXT,
  error_class TEXT,
  retryable INTEGER NOT NULL DEFAULT 0,
  schema_version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  FOREIGN KEY (job_id) REFERENCES publish_jobs(id)
);`

const schemaBatchRuns = `CREATE TABLE IF NOT EXISTS batch_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  operation_type TEXT NOT NULL,
  platform_code TEXT,
  status TEXT NOT NULL,
  total_items INTEGER NOT NULL DEFAULT 0,
  pending_count INTEGER NOT NULL DEFAULT 0,
  running_count INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  skipped_count INTEGER NOT NULL DEFAULT 0,
  concurrency INTEGER NOT NULL DEFAULT 1,
  settings_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaBatchItems = `CREATE TABLE IF NOT EXISTS batch_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  item_type TEXT NOT NULL,
  item_id INTEGER,
  account_id INTEGER,
  asset_id INTEGER,
  content_id INTEGER,
  job_id INTEGER,
  status TEXT NOT NULL,
  current_step TEXT,
  error_code TEXT,
  error_message TEXT,
  screenshot_path TEXT,
  dom_snapshot_path TEXT,
  raw_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (batch_id) REFERENCES batch_runs(id)
);`

const schemaPublishRecords = `CREATE TABLE IF NOT EXISTS publish_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER,
  content_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER,
  platform_code TEXT NOT NULL,
  content_type TEXT NOT NULL,
  status TEXT NOT NULL,
  post_url TEXT,
  external_post_id TEXT,
  publish_data TEXT,
  published_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (job_id) REFERENCES publish_jobs(id),
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaPublishContentRecords = `CREATE TABLE IF NOT EXISTS publish_content_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER NOT NULL,
  plan_id INTEGER NOT NULL,
  plan_target_id INTEGER NOT NULL,
  content_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  content_type TEXT NOT NULL,
  account_name TEXT NOT NULL,
  title TEXT NOT NULL,
  cover_path TEXT NOT NULL DEFAULT '',
  remote_state TEXT NOT NULL CHECK (remote_state IN ('scheduled','published','delete_pending','delete_failed','deleted')),
  scheduled_at TEXT NOT NULL DEFAULT '',
  post_url TEXT NOT NULL DEFAULT '',
  external_post_id TEXT NOT NULL DEFAULT '',
  evidence_path TEXT NOT NULL DEFAULT '',
  delete_evidence_path TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  verified_at TEXT NOT NULL,
  deleted_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (job_id) REFERENCES publish_jobs(id),
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaRecordInfos = `CREATE TABLE IF NOT EXISTS record_infos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  publish_record_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER,
  platform_code TEXT NOT NULL,
  link TEXT,
  read_num INTEGER,
  comment_num INTEGER,
  share_num INTEGER,
  collect_num INTEGER,
  like_num INTEGER,
  success INTEGER NOT NULL DEFAULT 0,
  setting TEXT,
  reason TEXT,
  since TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (publish_record_id) REFERENCES publish_records(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaNotifications = `CREATE TABLE IF NOT EXISTS notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  level TEXT NOT NULL DEFAULT 'info',
  title TEXT NOT NULL,
  body TEXT,
  read INTEGER NOT NULL DEFAULT 0,
  related_type TEXT,
  related_id INTEGER,
  created_at TEXT NOT NULL
);`

const schemaNotificationSettings = `CREATE TABLE IF NOT EXISTS notification_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaNotificationChannels = `CREATE TABLE IF NOT EXISTS notification_channels (
  kind TEXT PRIMARY KEY CHECK (kind IN ('feishu','dingtalk','webhook','email')),
  enabled INTEGER NOT NULL DEFAULT 0,
  smtp_host TEXT NOT NULL DEFAULT '',
  smtp_port INTEGER NOT NULL DEFAULT 0,
  smtp_security TEXT NOT NULL DEFAULT '',
  smtp_username TEXT NOT NULL DEFAULT '',
  email_from TEXT NOT NULL DEFAULT '',
  email_to_json TEXT NOT NULL DEFAULT '[]',
  updated_at TEXT NOT NULL
);`

const schemaNotificationDeliveries = `CREATE TABLE IF NOT EXISTS notification_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  event_type TEXT NOT NULL,
  account_id INTEGER,
  status TEXT NOT NULL CHECK (status IN ('pending','delivered','failed')),
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  delivered_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL
);`

const schemaBrowserRuntimeInstances = `CREATE TABLE IF NOT EXISTS browser_runtime_instances (
  profile_id    TEXT PRIMARY KEY,
  account_id    INTEGER,
  pid           INTEGER NOT NULL,
  cdp_port      INTEGER NOT NULL,
  cdp_url       TEXT NOT NULL,
  state         TEXT NOT NULL DEFAULT 'starting',
  active_job_id INTEGER,
  lease_owner   TEXT,
  started_at    TEXT NOT NULL,
  last_seen_at  TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);`

const schemaProfileLeases = `CREATE TABLE IF NOT EXISTS profile_leases (
  profile_id    TEXT PRIMARY KEY,
  holder_type   TEXT NOT NULL,
  holder_id     TEXT NOT NULL,
  acquired_at   TEXT NOT NULL,
  expires_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);`

// schemaCDPPortLeases closes the cross-process gap between selecting a free
// loopback port and persisting browser_runtime_instances after process start.
// A reservation is intentionally short lived; the runtime row takes over as
// the durable ownership record once launch registration succeeds.
const schemaCDPPortLeases = `CREATE TABLE IF NOT EXISTS cdp_port_leases (
  cdp_port      INTEGER PRIMARY KEY,
  profile_id    TEXT NOT NULL UNIQUE,
  owner         TEXT NOT NULL,
  acquired_at   TEXT NOT NULL,
  expires_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);`

// schemaBrowserOperationLeases serializes every active browser action for a
// profile. It is intentionally separate from lifecycle profile_leases: a
// running browser may exist while exactly one Fleet/agent action owns it.
const schemaBrowserOperationLeases = `CREATE TABLE IF NOT EXISTS browser_operation_leases (
  profile_id    TEXT PRIMARY KEY,
  operation     TEXT NOT NULL,
  owner         TEXT NOT NULL,
  acquired_at   TEXT NOT NULL,
  expires_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);`

const schemaBrowserEvents = `CREATE TABLE IF NOT EXISTS browser_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  profile_id    TEXT NOT NULL,
  event_type    TEXT NOT NULL,
  message       TEXT,
  reason_code   TEXT NOT NULL DEFAULT '',
  exit_code     INTEGER,
  term_signal   INTEGER,
  pid           INTEGER,
  cdp_port      INTEGER,
  job_id        INTEGER,
  created_at    TEXT NOT NULL
);`

const schemaAuditEvents = `CREATE TABLE IF NOT EXISTS audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id TEXT,
  level TEXT NOT NULL DEFAULT 'info',
  category TEXT,
  message TEXT,
  related_type TEXT,
  related_id INTEGER,
  raw_json TEXT,
  created_at TEXT NOT NULL
);`

// schemaOperationEvents stores the user-visible, allowlisted timeline for
// Agent and Tool Registry activity. It deliberately has no args, URLs,
// browser protocol payloads, credentials, or filesystem paths.
const schemaOperationEvents = `CREATE TABLE IF NOT EXISTS operation_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id      TEXT NOT NULL,
  request_id    TEXT,
  task_id       TEXT,
  account_id    INTEGER,
  platform_code TEXT,
  component     TEXT NOT NULL,
  event_type    TEXT NOT NULL,
  step          TEXT,
  status        TEXT,
  level         TEXT NOT NULL DEFAULT 'info',
  error_code    TEXT,
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL
);`

const schemaTikHubAPICalls = `CREATE TABLE IF NOT EXISTS tikhub_api_calls (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id        TEXT NOT NULL,
  platform_code   TEXT NOT NULL,
  endpoint_id     TEXT NOT NULL,
  capability      TEXT NOT NULL,
  method          TEXT NOT NULL,
  path            TEXT NOT NULL,
  parameters_json TEXT NOT NULL DEFAULT '{}',
  status          TEXT NOT NULL DEFAULT 'running',
  error_code      TEXT NOT NULL DEFAULT '',
  provider_code   TEXT NOT NULL DEFAULT '',
  provider_message TEXT NOT NULL DEFAULT '',
  item_count      INTEGER NOT NULL DEFAULT 0,
  response_bytes  INTEGER NOT NULL DEFAULT 0,
  raw_path        TEXT NOT NULL DEFAULT '',
  started_at      TEXT NOT NULL,
  finished_at     TEXT NOT NULL DEFAULT '',
  duration_ms     INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL
);`

const schemaTaskQueue = `CREATE TABLE IF NOT EXISTS task_queue (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  payload_json TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  priority INTEGER NOT NULL DEFAULT 0,
  run_after TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  trace_id TEXT,
	lease_owner TEXT,
	lease_expires_at TEXT,
	cancel_requested_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaLoginSessions = `CREATE TABLE IF NOT EXISTS login_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  status TEXT NOT NULL,
  error_code TEXT,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  expires_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);`

const schemaPostMetrics = `CREATE TABLE IF NOT EXISTS post_metrics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  publish_record_id INTEGER,
  account_id INTEGER,
  platform_code TEXT,
  content_id INTEGER,
  views INTEGER NOT NULL DEFAULT 0,
  likes INTEGER NOT NULL DEFAULT 0,
  comments INTEGER NOT NULL DEFAULT 0,
  shares INTEGER NOT NULL DEFAULT 0,
  favorites INTEGER NOT NULL DEFAULT 0,
  raw_json TEXT,
  collected_at TEXT,
  created_at TEXT NOT NULL
);`

const schemaCreatorMetricSnapshots = `CREATE TABLE IF NOT EXISTS creator_metric_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  snapshot_date TEXT NOT NULL,
  window_days INTEGER NOT NULL DEFAULT 7 CHECK (window_days BETWEEN 1 AND 31),
  range_verified INTEGER NOT NULL DEFAULT 0 CHECK (range_verified IN (0,1)),
  followers INTEGER NOT NULL DEFAULT 0 CHECK (followers >= 0),
  followers_verified INTEGER NOT NULL DEFAULT 0 CHECK (followers_verified IN (0,1)),
  views INTEGER NOT NULL DEFAULT 0 CHECK (views >= 0),
  likes INTEGER NOT NULL DEFAULT 0 CHECK (likes >= 0),
  comments INTEGER NOT NULL DEFAULT 0 CHECK (comments >= 0),
  shares INTEGER NOT NULL DEFAULT 0 CHECK (shares >= 0),
  favorites INTEGER NOT NULL DEFAULT 0 CHECK (favorites >= 0),
  posts INTEGER NOT NULL DEFAULT 0 CHECK (posts >= 0),
  evidence_code TEXT NOT NULL,
  raw_json TEXT NOT NULL DEFAULT '{}',
  collected_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(account_id, snapshot_date, window_days),
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaCreatorPostMetricSnapshots = `CREATE TABLE IF NOT EXISTS creator_post_metric_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  snapshot_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  post_key TEXT NOT NULL,
  external_post_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  published_at TEXT NOT NULL DEFAULT '',
  post_url TEXT NOT NULL DEFAULT '',
  views INTEGER NOT NULL DEFAULT 0 CHECK (views >= 0),
  likes INTEGER NOT NULL DEFAULT 0 CHECK (likes >= 0),
  comments INTEGER NOT NULL DEFAULT 0 CHECK (comments >= 0),
  shares INTEGER NOT NULL DEFAULT 0 CHECK (shares >= 0),
  favorites INTEGER NOT NULL DEFAULT 0 CHECK (favorites >= 0),
  raw_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE(snapshot_id, post_key),
  FOREIGN KEY (snapshot_id) REFERENCES creator_metric_snapshots(id) ON DELETE CASCADE,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaCreatorMetricCollectionStates = `CREATE TABLE IF NOT EXISTS creator_metric_collection_states (
  account_id INTEGER PRIMARY KEY,
  platform_code TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('observed','skipped','unknown','manual_takeover')),
  error_code TEXT NOT NULL DEFAULT '',
  evidence_code TEXT NOT NULL DEFAULT '',
  range_verified INTEGER NOT NULL DEFAULT 0 CHECK (range_verified IN (0,1)),
  post_count INTEGER NOT NULL DEFAULT 0 CHECK (post_count >= 0),
  collected_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);`

const schemaCreatorMetricPreferences = `CREATE TABLE IF NOT EXISTS creator_metric_preferences (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  history_days INTEGER NOT NULL DEFAULT 30 CHECK (history_days IN (7,30,90)),
  retention_days INTEGER NOT NULL DEFAULT 90 CHECK (retention_days IN (30,90,180,365)),
  updated_at TEXT NOT NULL
);`

const schemaContentVersions = `CREATE TABLE IF NOT EXISTS content_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL,
  lang TEXT,
  title TEXT,
  caption TEXT,
  body TEXT,
  tags_json TEXT,
  created_at TEXT NOT NULL
);`

const schemaContentVariants = `CREATE TABLE IF NOT EXISTS content_variants (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  account_id INTEGER,
  title TEXT NOT NULL DEFAULT '',
  caption TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  revision INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL DEFAULT 'manual',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code),
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);`

const schemaPublishPlans = `CREATE TABLE IF NOT EXISTS publish_plans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL,
  media_mode TEXT NOT NULL,
  snapshot_json TEXT NOT NULL,
  snapshot_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  concurrency INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  frozen_at TEXT NOT NULL,
  approved_at TEXT,
  approved_action TEXT NOT NULL DEFAULT '',
  studio_handoff_id INTEGER REFERENCES studio_schedule_handoffs(id) ON DELETE RESTRICT,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (content_id) REFERENCES contents(id)
);`

const schemaStudioScheduleHandoffs = `CREATE TABLE IF NOT EXISTS studio_schedule_handoffs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workflow_id TEXT NOT NULL,
  content_id INTEGER NOT NULL,
  script_id INTEGER,
  revision INTEGER NOT NULL,
  media_mode TEXT NOT NULL CHECK (media_mode IN ('article','graph_text','video')),
  media_ids_json TEXT NOT NULL,
  cover_media_id INTEGER,
  native_video_cover INTEGER NOT NULL DEFAULT 0 CHECK (native_video_cover IN (0,1)),
  canonical_hash TEXT NOT NULL,
  planned_at_utc TEXT NOT NULL,
  timezone TEXT NOT NULL,
  original_local_time TEXT NOT NULL,
  intent_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('ready','frozen','superseded','cancelled','expired')),
  frozen_plan_id INTEGER,
  snapshot_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workflow_id) REFERENCES creation_workflows(id) ON DELETE RESTRICT,
  FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE RESTRICT,
  FOREIGN KEY (script_id) REFERENCES studio_scripts(id) ON DELETE RESTRICT,
  FOREIGN KEY (frozen_plan_id) REFERENCES publish_plans(id) ON DELETE RESTRICT
);`

const schemaPublishPlanTargets = `CREATE TABLE IF NOT EXISTS publish_plan_targets (
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
  FOREIGN KEY (plan_id) REFERENCES publish_plans(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (asset_id) REFERENCES account_assets(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code),
  FOREIGN KEY (variant_id) REFERENCES content_variants(id),
  FOREIGN KEY (publish_job_id) REFERENCES publish_jobs(id),
  UNIQUE (plan_id, asset_id)
);`

const schemaApprovals = `CREATE TABLE IF NOT EXISTS approvals (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  skill_version TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL,
  args_hash TEXT NOT NULL,
  capability TEXT NOT NULL,
  target_resource TEXT NOT NULL,
  cost_limit INTEGER NOT NULL DEFAULT 0,
  expires_at TEXT NOT NULL,
  nonce_hash TEXT NOT NULL,
  single_use INTEGER NOT NULL DEFAULT 1,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);`

const schemaCredentials = `CREATE TABLE IF NOT EXISTS credentials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER,
  platform_code TEXT,
  kind TEXT NOT NULL,
  ref_key TEXT NOT NULL,
  meta_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaMediaLibrary = `CREATE TABLE IF NOT EXISTS media_library (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  file_path TEXT NOT NULL,
  sha256 TEXT UNIQUE,
  size_bytes INTEGER,
  mime TEXT,
  width INTEGER,
  height INTEGER,
  duration_ms INTEGER,
  probe_json TEXT,
  purpose TEXT NOT NULL DEFAULT 'shared' CHECK (purpose IN ('shared','video','graph_text','article')),
  created_at TEXT NOT NULL
);`

const schemaGenerationJobs = `CREATE TABLE IF NOT EXISTS generation_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id TEXT NOT NULL DEFAULT '',
	request_hash TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('image', 'video')),
  mode TEXT NOT NULL CHECK (mode IN ('text_to_image', 'image_to_image', 'text_to_video', 'image_to_video')),
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  output_size TEXT NOT NULL DEFAULT '',
  duration_seconds INTEGER NOT NULL DEFAULT 0,
  prompt TEXT NOT NULL,
  prompt_hash TEXT NOT NULL CHECK (length(prompt_hash) = 64),
  references_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
  external_task_id TEXT NOT NULL DEFAULT '',
  external_video_id TEXT NOT NULL DEFAULT '',
  output_media_id INTEGER,
  progress INTEGER NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  FOREIGN KEY (output_media_id) REFERENCES media_library(id) ON DELETE RESTRICT
);`

const schemaAIImageProjects = `CREATE TABLE IF NOT EXISTS ai_image_projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  workspace_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaStudioScripts = `CREATE TABLE IF NOT EXISTS studio_scripts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT 'video' CHECK (content_type IN ('video','graph_text','article')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaClipJobs = `CREATE TABLE IF NOT EXISTS clip_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_media_id INTEGER NOT NULL,
  script_id INTEGER,
  source_path TEXT NOT NULL,
  output_path TEXT,
  start_ms INTEGER NOT NULL,
  end_ms INTEGER NOT NULL,
  plan_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'planned',
  error_code TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY (source_media_id) REFERENCES media_library(id),
  FOREIGN KEY (script_id) REFERENCES studio_scripts(id) ON DELETE RESTRICT
);`

// App tool users (local/intranet login). Not platform social accounts.
const schemaAppUsers = `CREATE TABLE IF NOT EXISTS app_users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'admin',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

// Agent Hub persists only the user-visible conversation state and event
// envelopes. Provider credentials, raw CLI transcripts, browser payloads and
// source paths deliberately do not belong in this schema.
const schemaAgentWorkspaceState = `CREATE TABLE IF NOT EXISTS agent_workspace_state (
  workspace_mode TEXT PRIMARY KEY CHECK (workspace_mode IN ('media','shop')),
  current_conversation_id TEXT REFERENCES agent_conversations(id) ON DELETE SET NULL,
  draft TEXT NOT NULL DEFAULT '',
  last_module TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);`

const schemaAgentConversations = `CREATE TABLE IF NOT EXISTS agent_conversations (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  provider_id TEXT NOT NULL,
	model_id TEXT NOT NULL DEFAULT '',
  bot_id TEXT NOT NULL DEFAULT '',
  workspace_mode TEXT NOT NULL DEFAULT 'media' CHECK (workspace_mode IN ('media','shop')),
  context_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_read_at TEXT NOT NULL DEFAULT ''
);`

// agent_bots stores Agent Hub personas only. Instructions are prompt text; the
// table never holds credentials, provider paths, DOM or tool grants, and
// capabilities_json is reserved for a future read-only bot surface.
const schemaAgentBots = `CREATE TABLE IF NOT EXISTS agent_bots (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  avatar TEXT NOT NULL DEFAULT '',
  color TEXT NOT NULL DEFAULT '',
  effort TEXT NOT NULL DEFAULT '',
  kickoff TEXT NOT NULL DEFAULT '',
  capabilities_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  pinned INTEGER NOT NULL DEFAULT 0,
  is_primary INTEGER NOT NULL DEFAULT 0,
  group_name TEXT NOT NULL DEFAULT '',
  muted INTEGER NOT NULL DEFAULT 0
);`

const schemaMetricsCollectionSchedule = `CREATE TABLE IF NOT EXISTS metrics_collection_schedule (
  id INTEGER PRIMARY KEY CHECK (id=1),
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
  interval_hours INTEGER NOT NULL DEFAULT 24 CHECK (interval_hours BETWEEN 1 AND 168),
  state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle','running')),
  last_started_at TEXT NOT NULL DEFAULT '',
  last_finished_at TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_result_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);`

const schemaAgentMessages = `CREATE TABLE IF NOT EXISTS agent_messages (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL DEFAULT '',
  run_id TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL CHECK (role IN ('user','assistant','system')),
  content TEXT NOT NULL,
  context_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);`

const schemaAgentRuns = `CREATE TABLE IF NOT EXISTS agent_runs (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL,
	model_id TEXT NOT NULL DEFAULT '',
  provider_session_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('running','completed','failed','cancelled','interrupted')),
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

// A delegation is one Bot handing a bounded task to another Bot. Both sides are
// real conversations/runs, so the audit trail is the same one the UI already
// reads; this table only records the handoff and its returned result.
// feishu_sync maps one local row to its Feishu Base record so repeated runs
// update instead of duplicating. Tokens and app secrets are never stored here.
const schemaFeishuSync = `CREATE TABLE IF NOT EXISTS feishu_sync (
  module TEXT NOT NULL,
  local_key TEXT NOT NULL,
  table_id TEXT NOT NULL,
  record_id TEXT NOT NULL,
  content_hash TEXT NOT NULL DEFAULT '',
  synced_at TEXT NOT NULL,
  PRIMARY KEY (module, local_key)
);`

const schemaAgentDelegations = `CREATE TABLE IF NOT EXISTS agent_delegations (
  id TEXT PRIMARY KEY,
  parent_conversation_id TEXT NOT NULL,
  from_bot_id TEXT NOT NULL DEFAULT '',
  to_bot_id TEXT NOT NULL,
  child_conversation_id TEXT NOT NULL,
  child_run_id TEXT NOT NULL,
  task TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running','completed','failed','cancelled')),
  result TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaAgentEvents = `CREATE TABLE IF NOT EXISTS agent_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  timestamp TEXT NOT NULL,
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  UNIQUE(run_id, seq)
);`

const schemaAgentProviderSessions = `CREATE TABLE IF NOT EXISTS agent_provider_sessions (
  conversation_id TEXT NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (conversation_id, provider_id)
);`

const schemaAgentApprovals = `CREATE TABLE IF NOT EXISTS agent_approvals (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,
  params_json TEXT NOT NULL,
  params_hash TEXT NOT NULL,
  impact_preview TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','approved','rejected','expired','cancelled')),
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  resolved_at TEXT NOT NULL DEFAULT ''
);`

const schemaAgentProviderSelections = `CREATE TABLE IF NOT EXISTS agent_provider_selections (
  provider_id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL DEFAULT '',
  model_id TEXT NOT NULL DEFAULT '',
  draft TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);`

const schemaAgentProviderCache = `CREATE TABLE IF NOT EXISTS agent_provider_cache (
  provider_id TEXT PRIMARY KEY,
  state TEXT NOT NULL,
  available INTEGER NOT NULL DEFAULT 0,
  ready INTEGER NOT NULL DEFAULT 0,
  version TEXT NOT NULL DEFAULT '',
  models_json TEXT NOT NULL DEFAULT '[]',
  reason TEXT NOT NULL DEFAULT '',
  checked_at TEXT NOT NULL DEFAULT '',
  model_expires_at TEXT NOT NULL DEFAULT ''
);`

const schemaAgentProviderModels = `CREATE TABLE IF NOT EXISTS agent_provider_models (
  provider_id TEXT NOT NULL,
  model_id TEXT NOT NULL,
  provider_version TEXT NOT NULL DEFAULT '',
  verified_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  PRIMARY KEY (provider_id, model_id)
);`

const schemaAgentModelValidationBatches = `CREATE TABLE IF NOT EXISTS agent_model_validation_batches (
  provider_id TEXT PRIMARY KEY,
  provider_version TEXT NOT NULL DEFAULT '',
  remaining_models_json TEXT NOT NULL DEFAULT '[]',
  total INTEGER NOT NULL DEFAULT 0,
  completed INTEGER NOT NULL DEFAULT 0,
  passed INTEGER NOT NULL DEFAULT 0,
  failures_json TEXT NOT NULL DEFAULT '[]',
  updated_at TEXT NOT NULL
);`

const schemaAgentProviderConfigs = `CREATE TABLE IF NOT EXISTS agent_provider_configs (
  provider_id TEXT PRIMARY KEY,
  executable_path TEXT NOT NULL DEFAULT '',
  launch_mode TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  added_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaAgentSettings = `CREATE TABLE IF NOT EXISTS agent_settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL
);`

const schemaAgentToolCalls = `CREATE TABLE IF NOT EXISTS agent_tool_calls (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  workspace TEXT NOT NULL,
  risk_level TEXT NOT NULL CHECK (risk_level IN ('L0','L1','L2','L3')),
  params_json TEXT NOT NULL,
  params_hash TEXT NOT NULL,
  impact_preview TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('proposed','pending','approved','rejected','running','completed','failed','expired','cancelled')),
  result_json TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  consumed_at TEXT NOT NULL DEFAULT ''
);`

const schemaAgentKnowledgeTurns = `CREATE TABLE IF NOT EXISTS agent_knowledge_turns (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  conversation_id TEXT NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL UNIQUE REFERENCES agent_runs(id) ON DELETE CASCADE,
  query TEXT NOT NULL DEFAULT '',
  scopes_json TEXT NOT NULL DEFAULT '[]',
  citations_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL
);`

const schemaCreationWorkflows = `CREATE TABLE IF NOT EXISTS creation_workflows (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL UNIQUE REFERENCES agent_conversations(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  platform TEXT NOT NULL DEFAULT '',
  style TEXT NOT NULL DEFAULT '',
  stage TEXT NOT NULL DEFAULT 'topic' CHECK (stage IN ('topic','copy','script','review')),
  knowledge_scopes_json TEXT NOT NULL DEFAULT '[]',
  accepted_artifacts_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

// Skill package content lives under the private local data directory. SQLite
// remains the catalog and enablement truth; arbitrary scripts are never stored
// in, or executed from, this table.
const schemaSkills = `CREATE TABLE IF NOT EXISTS skills (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL CHECK (source IN ('system','local')),
  status TEXT NOT NULL CHECK (status IN ('reviewed','ready','review_required')),
  runtime TEXT NOT NULL CHECK (runtime IN ('instructions','registry')),
  runtime_ref TEXT NOT NULL DEFAULT '',
  directory TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  has_scripts INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaFeedbackReceipts = `CREATE TABLE IF NOT EXISTS feedback_receipts (
  feedback_id TEXT PRIMARY KEY,
  read_token TEXT NOT NULL,
  content TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT '待处理' CHECK (status IN ('待处理','处理中','已完成','不采纳')),
  category TEXT NOT NULL DEFAULT '未分类',
  developer_reply TEXT NOT NULL DEFAULT '',
  remote_created_at TEXT NOT NULL DEFAULT '',
  remote_updated_at TEXT NOT NULL DEFAULT '',
  last_synced_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const schemaFeedbackSubmissionIntents = `CREATE TABLE IF NOT EXISTS feedback_submission_intents (
  request_hash TEXT PRIMARY KEY,
  client_token TEXT NOT NULL,
  feedback_number TEXT NOT NULL,
  read_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`
