// Package datalink implements one-time, read-only imports from local data files.
package datalink

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	appdb "alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
	_ "modernc.org/sqlite"
)

const (
	maxSourceBytes  = int64(512 << 20)
	maxSnapshotRows = 50_000
	maxSnapshotSize = int64(32 << 20)
)

var canonicalFields = map[string]string{
	"date": "date", "shop": "text", "platform": "text", "product": "text", "sku": "text",
	"content_id": "text", "impressions": "number", "clicks": "number", "visitors": "number",
	"orders": "number", "gmv": "number", "refunds": "number", "ad_spend": "number",
}

type Source struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	FileSize     int64  `json:"file_size"`
	SHA256       string `json:"sha256"`
	Status       string `json:"status"`
	ErrorCode    string `json:"error_code"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	filePath     string
}

type Column struct {
	Name         string `json:"name"`
	DeclaredType string `json:"declared_type"`
	NotNull      bool   `json:"not_null"`
	PrimaryKey   bool   `json:"primary_key"`
}

type Object struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Columns []Column `json:"columns"`
}

type InspectRequest struct {
	ObjectName string
	Query      string
	Limit      int
}

type Inspection struct {
	Source            Source                   `json:"source"`
	Objects           []Object                 `json:"objects"`
	Preview           []map[string]interface{} `json:"preview"`
	PreviewColumns    []string                 `json:"preview_columns"`
	SchemaFingerprint string                   `json:"schema_fingerprint"`
	SuggestedMapping  map[string]string        `json:"suggested_mapping"`
}

type CollectRequest struct {
	ObjectName    string
	Query         string
	Mapping       map[string]string
	MaxRows       int
	MaxBytes      int64
	TimeoutSecond int
}

type Preflight struct {
	SchemaFingerprint string            `json:"schema_fingerprint"`
	EstimatedRows     int64             `json:"estimated_rows"`
	Columns           []string          `json:"columns"`
	SuggestedMapping  map[string]string `json:"suggested_mapping"`
	Warnings          []string          `json:"warnings"`
}

type QualityReport struct {
	Missing      map[string]int `json:"missing"`
	TypeMismatch map[string]int `json:"type_mismatch"`
	Negative     map[string]int `json:"negative"`
	Duplicates   int            `json:"duplicates"`
	DateMin      string         `json:"date_min,omitempty"`
	DateMax      string         `json:"date_max,omitempty"`
	Issues       []string       `json:"issues"`
}

type Snapshot struct {
	ID                  int64             `json:"id"`
	SourceID            int64             `json:"source_id"`
	SourceName          string            `json:"source_name"`
	ObjectName          string            `json:"object_name"`
	SchemaFingerprint   string            `json:"schema_fingerprint"`
	Mapping             map[string]string `json:"mapping"`
	Status              string            `json:"status"`
	RowCount            int               `json:"row_count"`
	ByteCount           int64             `json:"byte_count"`
	Checksum            string            `json:"checksum"`
	DataPath            string            `json:"data_path"`
	ReviewPath          string            `json:"review_path"`
	ErrorCode           string            `json:"error_code"`
	Quality             QualityReport     `json:"quality"`
	Truncated           bool              `json:"truncated"`
	KnowledgeBaseID     int64             `json:"knowledge_base_id"`
	KnowledgeDocumentID int64             `json:"knowledge_document_id"`
	ConfirmedAt         string            `json:"confirmed_at"`
	CollectedAt         string            `json:"collected_at"`
}

type Service struct {
	database *appdb.DB
}

func NewService(database *appdb.DB) *Service { return &Service{database: database} }
func (s *Service) DataDir() string           { return s.database.DataDir }

func (s *Service) RegisterSQLite(ctx context.Context, name string, input io.Reader) (*Source, error) {
	if s == nil || s.database == nil || input == nil {
		return nil, fmt.Errorf("DATA_SOURCE_INVALID")
	}
	name = filepath.Base(strings.TrimSpace(name))
	ext := strings.ToLower(filepath.Ext(name))
	if name == "" || name == "." || (ext != ".db" && ext != ".sqlite" && ext != ".sqlite3") {
		return nil, fmt.Errorf("DATA_SOURCE_TYPE_UNSUPPORTED")
	}
	dir := filepath.Join(s.database.DataDir, "data-link", "sources")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_STORAGE_FAILED")
	}
	temp, err := os.CreateTemp(dir, "incoming-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_STORAGE_FAILED")
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(input, maxSourceBytes+1))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil || written <= 0 || written > maxSourceBytes {
		return nil, fmt.Errorf("DATA_SOURCE_SIZE_INVALID")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	storedPath := filepath.Join(dir, digest+ext)
	created := false
	if _, err := os.Stat(storedPath); os.IsNotExist(err) {
		if err := os.Rename(tempPath, storedPath); err != nil {
			return nil, fmt.Errorf("DATA_SOURCE_STORAGE_FAILED")
		}
		created = true
		if err := os.Chmod(storedPath, 0600); err != nil {
			return nil, fmt.Errorf("DATA_SOURCE_STORAGE_FAILED")
		}
	}
	conn, err := openSQLite(storedPath)
	if err != nil {
		if created {
			_ = os.Remove(storedPath)
		}
		return nil, fmt.Errorf("DATA_SOURCE_SQLITE_INVALID")
	}
	var quickCheck string
	err = conn.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&quickCheck)
	_ = conn.Close()
	if err != nil || quickCheck != "ok" {
		if created {
			_ = os.Remove(storedPath)
		}
		return nil, fmt.Errorf("DATA_SOURCE_SQLITE_INVALID")
	}
	rel, _ := filepath.Rel(s.database.DataDir, storedPath)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.database.ExecContext(ctx, `INSERT INTO data_link_sources
		(kind,name,original_name,file_path,file_size,sha256,status,error_code,created_at,updated_at)
		VALUES ('sqlite',?,?,?,?,?,'ready','',?,?)
		ON CONFLICT(kind,sha256) DO UPDATE SET name=excluded.name,original_name=excluded.original_name,
		file_size=excluded.file_size,status='ready',error_code='',updated_at=excluded.updated_at`,
		strings.TrimSuffix(name, ext), name, filepath.ToSlash(rel), written, digest, now, now)
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_SAVE_FAILED")
	}
	return s.sourceBySHA(ctx, digest)
}

func (s *Service) List(ctx context.Context) ([]Source, []Snapshot, error) {
	sources, err := s.listSources(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.database.QueryContext(ctx, `SELECT snapshot.id,snapshot.source_id,source.name,snapshot.object_name,
		snapshot.schema_fingerprint,snapshot.mapping_json,snapshot.status,snapshot.row_count,snapshot.byte_count,
		snapshot.checksum,snapshot.data_path,snapshot.review_path,snapshot.error_code,snapshot.quality_json,
		snapshot.truncated,COALESCE(snapshot.knowledge_base_id,0),COALESCE(snapshot.knowledge_document_id,0),snapshot.confirmed_at,snapshot.collected_at
		FROM data_link_snapshots snapshot JOIN data_link_sources source ON source.id=snapshot.source_id
		ORDER BY snapshot.id DESC LIMIT 50`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var snapshots []Snapshot
	for rows.Next() {
		item, err := scanSnapshot(rows)
		if err != nil {
			return nil, nil, err
		}
		snapshots = append(snapshots, *item)
	}
	return sources, snapshots, rows.Err()
}

func (s *Service) Inspect(ctx context.Context, sourceID int64, request InspectRequest) (*Inspection, error) {
	source, path, conn, err := s.openSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = path
	objects, fingerprint, err := catalog(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_INSPECT_FAILED")
	}
	result := &Inspection{Source: *source, Objects: objects, SchemaFingerprint: fingerprint, SuggestedMapping: suggestMapping(objects, request.ObjectName)}
	if strings.TrimSpace(request.ObjectName) == "" && strings.TrimSpace(request.Query) == "" {
		return result, nil
	}
	query, err := selectionQuery(objects, request.ObjectName, request.Query, bounded(request.Limit, 20, 1, 100)+1)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := conn.QueryContext(queryCtx, query)
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_QUERY_FAILED")
	}
	result.PreviewColumns, result.Preview, _, _, err = readRows(rows, bounded(request.Limit, 20, 1, 100), 2<<20, nil)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Preflight(ctx context.Context, sourceID int64, request CollectRequest) (*Preflight, error) {
	_, _, conn, err := s.openSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	objects, fingerprint, err := catalog(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_INSPECT_FAILED")
	}
	query, err := selectionQuery(objects, request.ObjectName, request.Query, 0)
	if err != nil {
		return nil, err
	}
	countCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var estimated int64
	if err := conn.QueryRowContext(countCtx, `SELECT COUNT(*) FROM (`+query+`)`).Scan(&estimated); err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_PREFLIGHT_FAILED")
	}
	rows, err := conn.QueryContext(ctx, `SELECT * FROM (`+query+`) LIMIT 1`)
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_PREFLIGHT_FAILED")
	}
	columns, err := rows.Columns()
	_ = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_PREFLIGHT_FAILED")
	}
	if err := validateMapping(columns, request.Mapping); err != nil {
		return nil, err
	}
	warnings := []string{}
	maxRows := bounded(request.MaxRows, 10_000, 1, maxSnapshotRows)
	if estimated > int64(maxRows) {
		warnings = append(warnings, fmt.Sprintf("结果约 %d 行，本次最多采集 %d 行", estimated, maxRows))
	}
	return &Preflight{SchemaFingerprint: fingerprint, EstimatedRows: estimated, Columns: columns, SuggestedMapping: suggestColumns(columns), Warnings: warnings}, nil
}

func (s *Service) Collect(ctx context.Context, sourceID int64, request CollectRequest) (*Snapshot, error) {
	source, _, conn, err := s.openSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	objects, fingerprint, err := catalog(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("DATA_SOURCE_INSPECT_FAILED")
	}
	maxRows := bounded(request.MaxRows, 10_000, 1, maxSnapshotRows)
	maxBytes := request.MaxBytes
	if maxBytes <= 0 || maxBytes > maxSnapshotSize {
		maxBytes = maxSnapshotSize
	}
	query, err := selectionQuery(objects, request.ObjectName, request.Query, maxRows+1)
	if err != nil {
		return nil, err
	}
	queryHash := sha256.Sum256([]byte(strings.TrimSpace(request.Query)))
	mappingJSON, _ := json.Marshal(request.Mapping)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.database.ExecContext(ctx, `INSERT INTO data_link_snapshots
		(source_id,object_name,query_sha256,schema_fingerprint,mapping_json,status,created_at,updated_at)
		VALUES (?,?,?,?,?,'running',?,?)`, sourceID, strings.TrimSpace(request.ObjectName), hex.EncodeToString(queryHash[:]), fingerprint, string(mappingJSON), now, now)
	if err != nil {
		return nil, fmt.Errorf("DATA_SNAPSHOT_CREATE_FAILED")
	}
	snapshotID, _ := result.LastInsertId()
	fail := func(code string) (*Snapshot, error) {
		_, _ = s.database.Exec(`UPDATE data_link_snapshots SET status='failed',error_code=?,updated_at=? WHERE id=?`, code, time.Now().UTC().Format(time.RFC3339Nano), snapshotID)
		return nil, fmt.Errorf("%s", code)
	}

	timeout := time.Duration(bounded(request.TimeoutSecond, 8, 1, 30)) * time.Second
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, err := conn.QueryContext(queryCtx, query)
	if err != nil {
		return fail("DATA_SOURCE_QUERY_FAILED")
	}
	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return fail("DATA_SOURCE_QUERY_FAILED")
	}
	if err := validateMapping(columns, request.Mapping); err != nil {
		_ = rows.Close()
		return fail(err.Error())
	}
	dir := filepath.Join(s.database.DataDir, "data-link", "snapshots")
	if err := os.MkdirAll(dir, 0700); err != nil {
		_ = rows.Close()
		return fail("DATA_SNAPSHOT_STORAGE_FAILED")
	}
	temp, err := os.CreateTemp(dir, fmt.Sprintf("snapshot-%d-*", snapshotID))
	if err != nil {
		_ = rows.Close()
		return fail("DATA_SNAPSHOT_STORAGE_FAILED")
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	counter := &countWriter{}
	encoder := json.NewEncoder(io.MultiWriter(temp, hash, counter))
	analyzer := newQualityAnalyzer(request.Mapping)
	values := make([]interface{}, len(columns))
	pointers := make([]interface{}, len(columns))
	for index := range values {
		pointers[index] = &values[index]
	}
	rowCount := 0
	truncated := false
	for rows.Next() {
		if rowCount >= maxRows {
			truncated = true
			break
		}
		if err := rows.Scan(pointers...); err != nil {
			_ = rows.Close()
			_ = temp.Close()
			return fail("DATA_SOURCE_SCAN_FAILED")
		}
		raw := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			raw[column] = normalizeValue(values[index])
		}
		mapped := map[string]interface{}{}
		for canonical, column := range request.Mapping {
			mapped[canonical] = raw[column]
		}
		analyzer.Add(raw, mapped)
		if err := encoder.Encode(map[string]interface{}{"raw": raw, "mapped": mapped}); err != nil {
			_ = rows.Close()
			_ = temp.Close()
			return fail("DATA_SNAPSHOT_STORAGE_FAILED")
		}
		rowCount++
		if counter.n > maxBytes {
			_ = rows.Close()
			_ = temp.Close()
			return fail("DATA_SNAPSHOT_BYTE_LIMIT")
		}
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if closeErr := temp.Close(); rowsErr != nil || closeErr != nil {
		if queryCtx.Err() != nil {
			return fail("DATA_SOURCE_QUERY_TIMEOUT")
		}
		return fail("DATA_SOURCE_QUERY_FAILED")
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	finalPath := filepath.Join(dir, fmt.Sprintf("snapshot-%d-%s.ndjson", snapshotID, checksum[:12]))
	if err := os.Rename(tempPath, finalPath); err != nil {
		return fail("DATA_SNAPSHOT_STORAGE_FAILED")
	}
	_ = os.Chmod(finalPath, 0600)
	quality := analyzer.Report()
	qualityJSON, _ := json.Marshal(quality)
	relData, _ := filepath.Rel(s.database.DataDir, finalPath)
	review := reviewMarkdown(snapshotID, source, request.ObjectName, fingerprint, checksum, rowCount, counter.n, truncated, request.Mapping, quality, now)
	reviewPath := filepath.Join(dir, fmt.Sprintf("snapshot-%d-review.md", snapshotID))
	if err := os.WriteFile(reviewPath, []byte(review), 0600); err != nil {
		return fail("DATA_SNAPSHOT_STORAGE_FAILED")
	}
	relReview, _ := filepath.Rel(s.database.DataDir, reviewPath)
	_, err = s.database.ExecContext(ctx, `UPDATE data_link_snapshots SET status='completed',row_count=?,byte_count=?,checksum=?,
		data_path=?,review_path=?,quality_json=?,truncated=?,collected_at=?,updated_at=? WHERE id=?`,
		rowCount, counter.n, checksum, filepath.ToSlash(relData), filepath.ToSlash(relReview), string(qualityJSON), boolInt(truncated), now, now, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("DATA_SNAPSHOT_SAVE_FAILED")
	}
	return s.GetSnapshot(ctx, snapshotID)
}

func (s *Service) ConfirmReview(ctx context.Context, snapshotID, baseID int64, confirm bool) (*Snapshot, error) {
	if !confirm {
		return nil, fmt.Errorf("CONFIRMATION_REQUIRED")
	}
	snapshot, err := s.GetSnapshot(ctx, snapshotID)
	if err != nil || snapshot.Status != "completed" || snapshot.ReviewPath == "" || baseID <= 0 {
		return nil, fmt.Errorf("DATA_REVIEW_INVALID")
	}
	path, err := s.resolve(snapshot.ReviewPath, filepath.Join(s.database.DataDir, "data-link", "snapshots"))
	if err != nil {
		return nil, fmt.Errorf("DATA_REVIEW_INVALID")
	}
	name := fmt.Sprintf("sql-link/snapshot-%d-%s.md", snapshot.ID, snapshot.Checksum[:12])
	result, err := knowledge.NewImporter(s.database).Import(ctx, knowledge.ImportRequest{BaseID: baseID, Files: []knowledge.InputFile{{Name: name, Path: path}}})
	if err != nil || len(result.Items) != 1 || result.Items[0].DocumentID <= 0 || result.Failed > 0 {
		return nil, fmt.Errorf("DATA_REVIEW_KNOWLEDGE_FAILED")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.database.ExecContext(ctx, `UPDATE data_link_snapshots SET knowledge_base_id=?,knowledge_document_id=?,confirmed_at=?,updated_at=? WHERE id=?`,
		baseID, result.Items[0].DocumentID, now, now, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("DATA_REVIEW_SAVE_FAILED")
	}
	return s.GetSnapshot(ctx, snapshotID)
}

func (s *Service) GetSnapshot(ctx context.Context, id int64) (*Snapshot, error) {
	row := s.database.QueryRowContext(ctx, `SELECT snapshot.id,snapshot.source_id,source.name,snapshot.object_name,
		snapshot.schema_fingerprint,snapshot.mapping_json,snapshot.status,snapshot.row_count,snapshot.byte_count,
		snapshot.checksum,snapshot.data_path,snapshot.review_path,snapshot.error_code,snapshot.quality_json,
		snapshot.truncated,COALESCE(snapshot.knowledge_base_id,0),COALESCE(snapshot.knowledge_document_id,0),snapshot.confirmed_at,snapshot.collected_at
		FROM data_link_snapshots snapshot JOIN data_link_sources source ON source.id=snapshot.source_id WHERE snapshot.id=?`, id)
	return scanSnapshot(row)
}

type scanner interface{ Scan(...interface{}) error }

func scanSnapshot(row scanner) (*Snapshot, error) {
	var item Snapshot
	var mappingJSON, qualityJSON string
	var truncated int
	if err := row.Scan(&item.ID, &item.SourceID, &item.SourceName, &item.ObjectName, &item.SchemaFingerprint,
		&mappingJSON, &item.Status, &item.RowCount, &item.ByteCount, &item.Checksum, &item.DataPath,
		&item.ReviewPath, &item.ErrorCode, &qualityJSON, &truncated, &item.KnowledgeBaseID,
		&item.KnowledgeDocumentID, &item.ConfirmedAt, &item.CollectedAt); err != nil {
		return nil, err
	}
	item.Truncated = truncated == 1
	item.Mapping = map[string]string{}
	item.Quality = QualityReport{Missing: map[string]int{}, TypeMismatch: map[string]int{}, Negative: map[string]int{}}
	_ = json.Unmarshal([]byte(mappingJSON), &item.Mapping)
	_ = json.Unmarshal([]byte(qualityJSON), &item.Quality)
	return &item, nil
}

func (s *Service) listSources(ctx context.Context) ([]Source, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT id,kind,name,original_name,file_path,file_size,sha256,status,error_code,created_at,updated_at FROM data_link_sources ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Source
	for rows.Next() {
		var item Source
		if err := rows.Scan(&item.ID, &item.Kind, &item.Name, &item.OriginalName, &item.filePath, &item.FileSize, &item.SHA256, &item.Status, &item.ErrorCode, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) sourceBySHA(ctx context.Context, digest string) (*Source, error) {
	row := s.database.QueryRowContext(ctx, `SELECT id,kind,name,original_name,file_path,file_size,sha256,status,error_code,created_at,updated_at FROM data_link_sources WHERE kind='sqlite' AND sha256=?`, digest)
	var item Source
	if err := row.Scan(&item.ID, &item.Kind, &item.Name, &item.OriginalName, &item.filePath, &item.FileSize, &item.SHA256, &item.Status, &item.ErrorCode, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) openSource(ctx context.Context, id int64) (*Source, string, *sql.DB, error) {
	row := s.database.QueryRowContext(ctx, `SELECT id,kind,name,original_name,file_path,file_size,sha256,status,error_code,created_at,updated_at FROM data_link_sources WHERE id=? AND status='ready'`, id)
	var source Source
	if err := row.Scan(&source.ID, &source.Kind, &source.Name, &source.OriginalName, &source.filePath, &source.FileSize, &source.SHA256, &source.Status, &source.ErrorCode, &source.CreatedAt, &source.UpdatedAt); err != nil {
		return nil, "", nil, fmt.Errorf("DATA_SOURCE_NOT_FOUND")
	}
	path, err := s.resolve(source.filePath, filepath.Join(s.database.DataDir, "data-link", "sources"))
	if err != nil {
		return nil, "", nil, fmt.Errorf("DATA_SOURCE_PATH_INVALID")
	}
	conn, err := openSQLite(path)
	if err != nil {
		return nil, "", nil, fmt.Errorf("DATA_SOURCE_OPEN_FAILED")
	}
	return &source, path, conn, nil
}

func (s *Service) resolve(relative, root string) (string, error) {
	path := filepath.Join(s.database.DataDir, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path")
	}
	return path, nil
}

func openSQLite(path string) (*sql.DB, error) {
	dsn := &url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Set("immutable", "1")
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout(2000)")
	dsn.RawQuery = query.Encode()
	conn, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func catalog(ctx context.Context, conn *sql.DB) ([]Object, string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT name,type FROM sqlite_schema WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var objects []Object
	for rows.Next() {
		var object Object
		if err := rows.Scan(&object.Name, &object.Type); err != nil {
			return nil, "", err
		}
		objects = append(objects, object)
	}
	if err := rows.Close(); err != nil {
		return nil, "", err
	}
	for index := range objects {
		columns, err := conn.QueryContext(ctx, `PRAGMA table_xinfo(`+quoteIdentifier(objects[index].Name)+`)`)
		if err != nil {
			return nil, "", err
		}
		for columns.Next() {
			var cid, notNull, primaryKey, hidden int
			var name, declaredType string
			var defaultValue interface{}
			if err := columns.Scan(&cid, &name, &declaredType, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
				_ = columns.Close()
				return nil, "", err
			}
			if hidden == 0 {
				objects[index].Columns = append(objects[index].Columns, Column{Name: name, DeclaredType: declaredType, NotNull: notNull == 1, PrimaryKey: primaryKey > 0})
			}
		}
		if err := columns.Close(); err != nil {
			return nil, "", err
		}
	}
	canonical, _ := json.Marshal(objects)
	digest := sha256.Sum256(canonical)
	return objects, hex.EncodeToString(digest[:]), nil
}

func selectionQuery(objects []Object, objectName, query string, limit int) (string, error) {
	query = strings.TrimSpace(query)
	objectName = strings.TrimSpace(objectName)
	if query != "" {
		if err := ValidateReadOnlySQL(query); err != nil {
			return "", err
		}
		if limit > 0 {
			return `SELECT * FROM (` + query + `) LIMIT ` + strconv.Itoa(limit), nil
		}
		return query, nil
	}
	if objectName == "" {
		return "", fmt.Errorf("DATA_SOURCE_SELECTION_REQUIRED")
	}
	found := false
	for _, object := range objects {
		if object.Name == objectName {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("DATA_SOURCE_OBJECT_NOT_FOUND")
	}
	result := `SELECT * FROM ` + quoteIdentifier(objectName)
	if limit > 0 {
		result += ` LIMIT ` + strconv.Itoa(limit)
	}
	return result, nil
}

func ValidateReadOnlySQL(query string) error {
	if strings.TrimSpace(query) == "" || len(query) > 64<<10 || strings.Contains(query, ";") {
		return fmt.Errorf("DATA_SOURCE_SQL_UNSAFE")
	}
	clean, ok := sqlWords(query)
	if !ok || len(clean) == 0 || (clean[0] != "SELECT" && clean[0] != "WITH") {
		return fmt.Errorf("DATA_SOURCE_SQL_UNSAFE")
	}
	blocked := map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "CREATE": true, "DROP": true, "ALTER": true, "PRAGMA": true, "ATTACH": true, "DETACH": true, "VACUUM": true, "REINDEX": true, "ANALYZE": true}
	for _, word := range clean {
		if blocked[word] {
			return fmt.Errorf("DATA_SOURCE_SQL_UNSAFE")
		}
	}
	return nil
}

func sqlWords(query string) ([]string, bool) {
	var words []string
	for i := 0; i < len(query); {
		if i+1 < len(query) && query[i:i+2] == "--" {
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(query) && query[i:i+2] == "/*" {
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += end + 4
			continue
		}
		if query[i] == '\'' || query[i] == '"' || query[i] == '`' {
			quote := query[i]
			i++
			for i < len(query) {
				if query[i] == quote {
					if i+1 < len(query) && query[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if unicode.IsLetter(rune(query[i])) || query[i] == '_' {
			start := i
			for i < len(query) && (unicode.IsLetter(rune(query[i])) || unicode.IsDigit(rune(query[i])) || query[i] == '_') {
				i++
			}
			words = append(words, strings.ToUpper(query[start:i]))
			continue
		}
		i++
	}
	return words, true
}

func readRows(rows *sql.Rows, maxRows int, maxBytes int64, analyzer *qualityAnalyzer) ([]string, []map[string]interface{}, int64, bool, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, 0, false, err
	}
	values := make([]interface{}, len(columns))
	pointers := make([]interface{}, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	var result []map[string]interface{}
	var bytes int64
	truncated := false
	for rows.Next() {
		if len(result) >= maxRows {
			truncated = true
			break
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, nil, bytes, false, err
		}
		item := map[string]interface{}{}
		for i, column := range columns {
			item[column] = normalizeValue(values[i])
		}
		encoded, _ := json.Marshal(item)
		bytes += int64(len(encoded))
		if bytes > maxBytes {
			return nil, nil, bytes, false, fmt.Errorf("DATA_SNAPSHOT_BYTE_LIMIT")
		}
		result = append(result, item)
	}
	return columns, result, bytes, truncated, rows.Err()
}

func normalizeValue(value interface{}) interface{} {
	if body, ok := value.([]byte); ok {
		return string(body)
	}
	if stamp, ok := value.(time.Time); ok {
		return stamp.UTC().Format(time.RFC3339Nano)
	}
	return value
}
func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func bounded(value, fallback, min, max int) int {
	if value == 0 {
		value = fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func validateMapping(columns []string, mapping map[string]string) error {
	known := map[string]bool{}
	for _, column := range columns {
		known[column] = true
	}
	for canonical, column := range mapping {
		if _, ok := canonicalFields[canonical]; !ok || !known[column] {
			return fmt.Errorf("DATA_FIELD_MAPPING_INVALID")
		}
	}
	return nil
}

func suggestMapping(objects []Object, selected string) map[string]string {
	for _, object := range objects {
		if object.Name == selected {
			names := make([]string, len(object.Columns))
			for i := range object.Columns {
				names[i] = object.Columns[i].Name
			}
			return suggestColumns(names)
		}
	}
	return map[string]string{}
}

func suggestColumns(columns []string) map[string]string {
	aliases := map[string][]string{
		"date": {"date", "day", "日期", "时间"}, "shop": {"shop", "store", "店铺"}, "platform": {"platform", "channel", "平台"},
		"product": {"product", "product_name", "商品"}, "sku": {"sku", "sku_id"}, "content_id": {"content_id", "post_id", "内容id"},
		"impressions": {"impressions", "exposure", "曝光"}, "clicks": {"clicks", "点击"}, "visitors": {"visitors", "uv", "访客"},
		"orders": {"orders", "order_count", "订单"}, "gmv": {"gmv", "sales", "成交金额"}, "refunds": {"refunds", "refund", "退款"}, "ad_spend": {"ad_spend", "spend", "广告消耗"},
	}
	result := map[string]string{}
	for _, column := range columns {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(column), " ", "_"))
		for canonical, names := range aliases {
			for _, name := range names {
				if normalized == name {
					result[canonical] = column
				}
			}
		}
	}
	return result
}

type qualityAnalyzer struct {
	mapping map[string]string
	report  QualityReport
	seen    map[string]bool
	dates   []time.Time
	numeric map[string]*numberStats
}
type numberStats struct {
	count    int
	sum, max float64
}

func newQualityAnalyzer(mapping map[string]string) *qualityAnalyzer {
	return &qualityAnalyzer{mapping: mapping, report: QualityReport{Missing: map[string]int{}, TypeMismatch: map[string]int{}, Negative: map[string]int{}}, seen: map[string]bool{}, numeric: map[string]*numberStats{}}
}
func (a *qualityAnalyzer) Add(raw, mapped map[string]interface{}) {
	encoded, _ := json.Marshal(raw)
	digest := sha256.Sum256(encoded)
	key := string(digest[:])
	if a.seen[key] {
		a.report.Duplicates++
	} else {
		a.seen[key] = true
	}
	for canonical := range a.mapping {
		value := mapped[canonical]
		if value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			a.report.Missing[canonical]++
			continue
		}
		switch canonicalFields[canonical] {
		case "date":
			stamp, ok := parseDate(value)
			if !ok {
				a.report.TypeMismatch[canonical]++
			} else {
				a.dates = append(a.dates, stamp)
			}
		case "number":
			number, ok := asNumber(value)
			if !ok {
				a.report.TypeMismatch[canonical]++
				continue
			}
			if number < 0 {
				a.report.Negative[canonical]++
			}
			stats := a.numeric[canonical]
			if stats == nil {
				stats = &numberStats{}
				a.numeric[canonical] = stats
			}
			stats.count++
			stats.sum += number
			if stats.count == 1 || number > stats.max {
				stats.max = number
			}
		}
	}
}
func (a *qualityAnalyzer) Report() QualityReport {
	for field, count := range a.report.Missing {
		if count > 0 {
			a.report.Issues = append(a.report.Issues, fmt.Sprintf("%s 缺失 %d 行", field, count))
		}
	}
	for field, count := range a.report.TypeMismatch {
		if count > 0 {
			a.report.Issues = append(a.report.Issues, fmt.Sprintf("%s 类型不一致 %d 行", field, count))
		}
	}
	for field, count := range a.report.Negative {
		if count > 0 {
			a.report.Issues = append(a.report.Issues, fmt.Sprintf("%s 出现 %d 个负值", field, count))
		}
	}
	if a.report.Duplicates > 0 {
		a.report.Issues = append(a.report.Issues, fmt.Sprintf("发现 %d 行完全重复", a.report.Duplicates))
	}
	if len(a.dates) > 0 {
		sort.Slice(a.dates, func(i, j int) bool { return a.dates[i].Before(a.dates[j]) })
		a.report.DateMin = a.dates[0].Format("2006-01-02")
		a.report.DateMax = a.dates[len(a.dates)-1].Format("2006-01-02")
	}
	for field, stats := range a.numeric {
		if stats.count > 1 && stats.max > 1000 && stats.max > (stats.sum/float64(stats.count))*100 {
			a.report.Issues = append(a.report.Issues, field+" 存在极端异常值")
		}
	}
	sort.Strings(a.report.Issues)
	return a.report
}
func parseDate(value interface{}) (time.Time, bool) {
	text := strings.TrimSpace(fmt.Sprint(value))
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02", "2006/01/02", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
func asNumber(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case int64:
		return float64(typed), true
	case int:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		number, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
		return number, err == nil
	}
}

type countWriter struct{ n int64 }

func (w *countWriter) Write(body []byte) (int, error) { w.n += int64(len(body)); return len(body), nil }

func reviewMarkdown(id int64, source *Source, objectName, fingerprint, checksum string, rows int, bytes int64, truncated bool, mapping map[string]string, quality QualityReport, collectedAt string) string {
	mappingJSON, _ := json.MarshalIndent(mapping, "", "  ")
	qualityJSON, _ := json.MarshalIndent(quality, "", "  ")
	return fmt.Sprintf("# SQL Link 全流程复盘\n\n来源引用：[SQL:%d:%s]\n\n## 采集证据\n\n- 数据源：%s（SQLite，SHA-256 `%s`）\n- 表/视图：%s\n- Schema 指纹：`%s`\n- 采集时间：%s\n- 行数：%d\n- 数据字节：%d\n- 快照校验和：`%s`\n- 达到限制：%t\n\n## 字段映射\n\n```json\n%s\n```\n\n## 数据质量\n\n```json\n%s\n```\n", id, checksum[:12], source.OriginalName, source.SHA256, objectName, fingerprint, collectedAt, rows, bytes, checksum, truncated, mappingJSON, qualityJSON)
}
