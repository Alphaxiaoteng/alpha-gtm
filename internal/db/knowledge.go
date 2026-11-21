package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var knowledgeSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type KnowledgeBase struct {
	ID              int64               `json:"id"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	IsDefault       bool                `json:"is_default"`
	TopK            int                 `json:"top_k"`
	MaxContextChars int                 `json:"max_context_chars"`
	DocumentCount   int                 `json:"document_count"`
	ChunkCount      int                 `json:"chunk_count"`
	LatestImport    *KnowledgeImportRun `json:"latest_import,omitempty"`
	CreatedAt       string              `json:"created_at"`
	UpdatedAt       string              `json:"updated_at"`
}

type KnowledgeImportRun struct {
	ID            int64  `json:"id"`
	BaseID        int64  `json:"base_id"`
	Status        string `json:"status"`
	TotalFiles    int    `json:"total_files"`
	ImportedFiles int    `json:"imported_files"`
	SkippedFiles  int    `json:"skipped_files"`
	FailedFiles   int    `json:"failed_files"`
	CreatedAt     string `json:"created_at"`
	CompletedAt   string `json:"completed_at"`
}

type KnowledgeDocumentInput struct {
	BaseID           int64
	ImportRunID      int64
	SourceName       string
	SourceType       string
	SHA256           string
	Status           string
	ErrorCode        string
	ErrorMessage     string
	OriginalPath     string
	NormalizedPath   string
	SourceSizeBytes  int64
	SourceModifiedNS int64
}

type KnowledgeDocument struct {
	ID               int64  `json:"id"`
	BaseID           int64  `json:"base_id"`
	ImportRunID      int64  `json:"import_run_id"`
	SourceName       string `json:"source_name"`
	SourceType       string `json:"source_type"`
	SHA256           string `json:"sha256"`
	Status           string `json:"status"`
	ErrorCode        string `json:"error_code"`
	ErrorMessage     string `json:"error_message"`
	OriginalPath     string `json:"original_path"`
	NormalizedPath   string `json:"normalized_path"`
	SourceSizeBytes  int64  `json:"-"`
	SourceModifiedNS int64  `json:"-"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type KnowledgeChunkInput struct {
	Ordinal     int
	HeadingPath string
	Content     string
}

type KnowledgeSearchResult struct {
	ChunkID     int64   `json:"chunk_id"`
	BaseID      int64   `json:"base_id"`
	BaseName    string  `json:"base_name"`
	DocumentID  int64   `json:"document_id"`
	DocumentSHA string  `json:"-"`
	SourceName  string  `json:"source_name"`
	HeadingPath string  `json:"heading_path"`
	Content     string  `json:"content"`
	Score       float64 `json:"score"`
}

type KnowledgeScope struct {
	BaseID       int64
	SourcePrefix string
}

func (db *DB) CreateKnowledgeBase(name, description string) (*KnowledgeBase, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || utf8.RuneCountInString(name) > 120 || utf8.RuneCountInString(description) > 2000 {
		return nil, fmt.Errorf("invalid knowledge base")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`INSERT INTO knowledge_bases
		(name,description,is_default,top_k,max_context_chars,created_at,updated_at)
		VALUES (?,?,CASE WHEN NOT EXISTS(SELECT 1 FROM knowledge_bases) THEN 1 ELSE 0 END,6,12000,?,?)`,
		name, description, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.GetKnowledgeBase(id)
}

func (db *DB) GetKnowledgeBase(id int64) (*KnowledgeBase, error) {
	item, err := scanKnowledgeBase(db.QueryRow(knowledgeBaseSummaryQuery+` WHERE b.id=?`, id))
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (db *DB) ListKnowledgeBases() ([]KnowledgeBase, error) {
	rows, err := db.Query(knowledgeBaseSummaryQuery + ` ORDER BY b.is_default DESC,b.updated_at DESC,b.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]KnowledgeBase, 0)
	for rows.Next() {
		item, err := scanKnowledgeBase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

const knowledgeBaseSummaryQuery = `SELECT b.id,b.name,b.description,b.is_default,b.top_k,b.max_context_chars,
	b.created_at,b.updated_at,
	(SELECT COUNT(*) FROM knowledge_documents d WHERE d.base_id=b.id AND d.status='ready'),
	(SELECT COUNT(*) FROM knowledge_chunks c JOIN knowledge_documents d ON d.id=c.document_id WHERE c.base_id=b.id AND d.status='ready'),
	r.id,r.base_id,r.status,r.total_files,r.imported_files,r.skipped_files,r.failed_files,r.created_at,r.completed_at
	FROM knowledge_bases b
	LEFT JOIN knowledge_import_runs r ON r.id=(SELECT id FROM knowledge_import_runs WHERE base_id=b.id ORDER BY id DESC LIMIT 1)`

type knowledgeBaseScanner interface {
	Scan(dest ...any) error
}

func scanKnowledgeBase(scanner knowledgeBaseScanner) (*KnowledgeBase, error) {
	var item KnowledgeBase
	var isDefault int
	var latestID, latestBaseID, latestTotal, latestImported, latestSkipped, latestFailed sql.NullInt64
	var latestStatus, latestCreatedAt, latestCompletedAt sql.NullString
	if err := scanner.Scan(&item.ID, &item.Name, &item.Description, &isDefault, &item.TopK,
		&item.MaxContextChars, &item.CreatedAt, &item.UpdatedAt, &item.DocumentCount, &item.ChunkCount,
		&latestID, &latestBaseID, &latestStatus, &latestTotal, &latestImported, &latestSkipped,
		&latestFailed, &latestCreatedAt, &latestCompletedAt); err != nil {
		return nil, err
	}
	item.IsDefault = isDefault == 1
	if latestID.Valid {
		item.LatestImport = &KnowledgeImportRun{
			ID: latestID.Int64, BaseID: latestBaseID.Int64, Status: latestStatus.String,
			TotalFiles: int(latestTotal.Int64), ImportedFiles: int(latestImported.Int64),
			SkippedFiles: int(latestSkipped.Int64), FailedFiles: int(latestFailed.Int64),
			CreatedAt: latestCreatedAt.String, CompletedAt: latestCompletedAt.String,
		}
	}
	return &item, nil
}

func (db *DB) UpdateKnowledgeBase(id int64, name, description string, topK, maxContextChars int) (*KnowledgeBase, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if id <= 0 || name == "" || utf8.RuneCountInString(name) > 120 ||
		utf8.RuneCountInString(description) > 2000 || topK < 1 || topK > 12 ||
		maxContextChars < 1000 || maxContextChars > 12000 {
		return nil, fmt.Errorf("invalid knowledge base")
	}
	result, err := db.Exec(`UPDATE knowledge_bases SET name=?,description=?,top_k=?,max_context_chars=?,updated_at=?
		WHERE id=?`, name, description, topK, maxContextChars, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, sql.ErrNoRows
	}
	return db.GetKnowledgeBase(id)
}

func (db *DB) DeleteKnowledgeBase(id int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid knowledge base")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM knowledge_chunks_fts WHERE base_id=?`, id); err != nil {
		return err
	}
	result, err := tx.Exec(`DELETE FROM knowledge_bases WHERE id=?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return sql.ErrNoRows
	}
	// ponytail: content-addressed files may be shared by another base. Keep them
	// local until an explicit orphan collector can archive them safely.
	return tx.Commit()
}

func (db *DB) CreateKnowledgeImportRun(baseID int64) (*KnowledgeImportRun, error) {
	if baseID <= 0 {
		return nil, fmt.Errorf("invalid knowledge base")
	}
	result, err := db.Exec(`INSERT INTO knowledge_import_runs(base_id,status,created_at) VALUES (?,'running',?)`,
		baseID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.GetKnowledgeImportRun(id)
}

func (db *DB) CompleteKnowledgeImportRun(id, total, imported, skipped, failed int64) error {
	if id <= 0 || total < 0 || imported < 0 || skipped < 0 || failed < 0 || imported+skipped+failed != total {
		return fmt.Errorf("invalid knowledge import result")
	}
	status := "completed"
	if failed > 0 {
		status = "completed_with_errors"
	}
	result, err := db.Exec(`UPDATE knowledge_import_runs SET status=?,total_files=?,imported_files=?,
		skipped_files=?,failed_files=?,completed_at=? WHERE id=? AND status='running'`,
		status, total, imported, skipped, failed, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) InterruptKnowledgeImportRun(id, total, imported, skipped, failed int64) error {
	if id <= 0 || total < 0 || imported < 0 || skipped < 0 || failed < 0 || imported+skipped+failed != total {
		return fmt.Errorf("invalid knowledge import result")
	}
	result, err := db.Exec(`UPDATE knowledge_import_runs SET status='interrupted',total_files=?,imported_files=?,
		skipped_files=?,failed_files=?,completed_at=? WHERE id=? AND status='running'`,
		total, imported, skipped, failed, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) UpdateKnowledgeImportRunProgress(id, total, imported, skipped, failed int64) error {
	if id <= 0 || total < 0 || imported < 0 || skipped < 0 || failed < 0 || imported+skipped+failed != total {
		return fmt.Errorf("invalid knowledge import progress")
	}
	result, err := db.Exec(`UPDATE knowledge_import_runs SET total_files=?,imported_files=?,
		skipped_files=?,failed_files=? WHERE id=? AND status='running'`,
		total, imported, skipped, failed, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) GetKnowledgeImportRun(id int64) (*KnowledgeImportRun, error) {
	var item KnowledgeImportRun
	err := db.QueryRow(`SELECT id,base_id,status,total_files,imported_files,skipped_files,failed_files,
		created_at,completed_at FROM knowledge_import_runs WHERE id=?`, id).
		Scan(&item.ID, &item.BaseID, &item.Status, &item.TotalFiles, &item.ImportedFiles,
			&item.SkippedFiles, &item.FailedFiles, &item.CreatedAt, &item.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (db *DB) UpsertKnowledgeDocument(input KnowledgeDocumentInput) (*KnowledgeDocument, bool, error) {
	input.SourceName = strings.TrimSpace(input.SourceName)
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	input.Status = strings.TrimSpace(input.Status)
	if input.BaseID <= 0 || input.SourceName == "" || input.SourceType == "" ||
		!knowledgeSHA256Pattern.MatchString(input.SHA256) || input.Status == "" {
		return nil, false, fmt.Errorf("invalid knowledge document")
	}
	var existingID int64
	err := db.QueryRow(`SELECT id FROM knowledge_documents WHERE base_id=? AND sha256=?`, input.BaseID, input.SHA256).Scan(&existingID)
	if err == nil {
		item, getErr := db.GetKnowledgeDocument(existingID)
		return item, false, getErr
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var runID any
	if input.ImportRunID > 0 {
		runID = input.ImportRunID
	}
	result, err := db.Exec(`INSERT INTO knowledge_documents
		(base_id,import_run_id,source_name,source_type,sha256,status,error_code,error_message,original_path,normalized_path,source_size_bytes,source_modified_ns,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		input.BaseID, runID, input.SourceName, input.SourceType, input.SHA256, input.Status,
		strings.TrimSpace(input.ErrorCode), strings.TrimSpace(input.ErrorMessage),
		strings.TrimSpace(input.OriginalPath), strings.TrimSpace(input.NormalizedPath), input.SourceSizeBytes, input.SourceModifiedNS, now, now)
	if err != nil {
		return nil, false, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, false, err
	}
	item, err := db.GetKnowledgeDocument(id)
	return item, true, err
}

func (db *DB) GetKnowledgeDocument(id int64) (*KnowledgeDocument, error) {
	var item KnowledgeDocument
	var runID sql.NullInt64
	err := db.QueryRow(`SELECT id,base_id,import_run_id,source_name,source_type,sha256,status,error_code,
		error_message,original_path,normalized_path,source_size_bytes,source_modified_ns,created_at,updated_at FROM knowledge_documents WHERE id=?`, id).
		Scan(&item.ID, &item.BaseID, &runID, &item.SourceName, &item.SourceType, &item.SHA256,
			&item.Status, &item.ErrorCode, &item.ErrorMessage, &item.OriginalPath, &item.NormalizedPath,
			&item.SourceSizeBytes, &item.SourceModifiedNS, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if runID.Valid {
		item.ImportRunID = runID.Int64
	}
	return &item, nil
}

func (db *DB) ListKnowledgeDocuments(baseID int64) ([]KnowledgeDocument, error) {
	if baseID <= 0 {
		return nil, fmt.Errorf("invalid knowledge base")
	}
	rows, err := db.Query(`SELECT id,base_id,import_run_id,source_name,source_type,sha256,status,error_code,
		error_message,original_path,normalized_path,source_size_bytes,source_modified_ns,created_at,updated_at
		FROM knowledge_documents WHERE base_id=? AND status<>'superseded' ORDER BY updated_at DESC,id DESC`, baseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]KnowledgeDocument, 0)
	for rows.Next() {
		var item KnowledgeDocument
		var runID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.BaseID, &runID, &item.SourceName, &item.SourceType,
			&item.SHA256, &item.Status, &item.ErrorCode, &item.ErrorMessage, &item.OriginalPath,
			&item.NormalizedPath, &item.SourceSizeBytes, &item.SourceModifiedNS, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if runID.Valid {
			item.ImportRunID = runID.Int64
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) FindKnowledgeDocumentBySourceMetadata(baseID int64, sourceName string, sizeBytes, modifiedNS int64) (*KnowledgeDocument, error) {
	sourceName = strings.TrimSpace(filepath.ToSlash(sourceName))
	if baseID <= 0 || sourceName == "" || sizeBytes <= 0 || modifiedNS <= 0 {
		return nil, nil
	}
	var id int64
	err := db.QueryRow(`SELECT id FROM knowledge_documents
		WHERE base_id=? AND source_name=? AND source_size_bytes=? AND source_modified_ns=?
		ORDER BY id DESC LIMIT 1`, baseID, sourceName, sizeBytes, modifiedNS).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return db.GetKnowledgeDocument(id)
}

func (db *DB) FindReadyKnowledgeDocumentBySourceName(baseID int64, sourceName string) (*KnowledgeDocument, error) {
	sourceName = strings.TrimSpace(filepath.ToSlash(sourceName))
	if baseID <= 0 || sourceName == "" {
		return nil, nil
	}
	var id int64
	err := db.QueryRow(`SELECT id FROM knowledge_documents
		WHERE base_id=? AND source_name=? AND status='ready' ORDER BY id DESC LIMIT 1`, baseID, sourceName).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return db.GetKnowledgeDocument(id)
}

func (db *DB) UpdateKnowledgeDocumentSourceMetadata(id, runID int64, sourceName string, sizeBytes, modifiedNS int64) error {
	sourceName = strings.TrimSpace(filepath.ToSlash(sourceName))
	if id <= 0 || sourceName == "" || sizeBytes <= 0 || modifiedNS <= 0 {
		return fmt.Errorf("invalid knowledge source metadata")
	}
	var importRun any
	if runID > 0 {
		importRun = runID
	}
	result, err := db.Exec(`UPDATE knowledge_documents SET import_run_id=COALESCE(?,import_run_id),
		source_name=?,source_size_bytes=?,source_modified_ns=?,updated_at=? WHERE id=?`,
		importRun, sourceName, sizeBytes, modifiedNS, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) ReplaceKnowledgeChunks(documentID int64, chunks []KnowledgeChunkInput) error {
	if err := validateKnowledgeChunks(documentID, chunks); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var baseID int64
	if err := tx.QueryRow(`SELECT base_id FROM knowledge_documents WHERE id=?`, documentID).Scan(&baseID); err != nil {
		return err
	}
	if err := replaceKnowledgeChunksTx(tx, baseID, documentID, chunks); err != nil {
		return err
	}
	return tx.Commit()
}

// FinalizeKnowledgeDocument makes a successfully parsed source version current
// only after its chunks and FTS rows are durable. A failed replacement therefore
// leaves the previous ready version available for local retrieval.
func (db *DB) FinalizeKnowledgeDocument(documentID int64, chunks []KnowledgeChunkInput) (bool, error) {
	if err := validateKnowledgeChunks(documentID, chunks); err != nil {
		return false, err
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var baseID int64
	var sourceModifiedNS int64
	var sourceName, status string
	if err := tx.QueryRow(`SELECT base_id,source_name,source_modified_ns,status FROM knowledge_documents WHERE id=?`, documentID).
		Scan(&baseID, &sourceName, &sourceModifiedNS, &status); err != nil {
		return false, err
	}
	if status != "pending" && status != "ready" {
		return false, fmt.Errorf("knowledge document is not finalizable")
	}
	var currentID, currentModifiedNS int64
	err = tx.QueryRow(`SELECT id,source_modified_ns FROM knowledge_documents
		WHERE base_id=? AND source_name=? AND status='ready' AND id<>?
		ORDER BY source_modified_ns DESC,id DESC LIMIT 1`, baseID, sourceName, documentID).
		Scan(&currentID, &currentModifiedNS)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil && knowledgeSourceVersionNewer(currentID, currentModifiedNS, documentID, sourceModifiedNS) {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(`DELETE FROM knowledge_chunks_fts WHERE document_id=?`, documentID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM knowledge_chunks WHERE document_id=?`, documentID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`UPDATE knowledge_documents SET status='superseded',error_code='stale_source_version',error_message='',updated_at=? WHERE id=?`, now, documentID); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := replaceKnowledgeChunksTx(tx, baseID, documentID, chunks); err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`DELETE FROM knowledge_chunks_fts WHERE document_id IN (
		SELECT id FROM knowledge_documents WHERE base_id=? AND source_name=? AND status='ready' AND id<>?
	)`, baseID, sourceName, documentID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE knowledge_documents SET status='superseded',updated_at=?
		WHERE base_id=? AND source_name=? AND status='ready' AND id<>?`, now, baseID, sourceName, documentID); err != nil {
		return false, err
	}
	result, err := tx.Exec(`UPDATE knowledge_documents SET status='ready',error_code='',error_message='',updated_at=? WHERE id=?`, now, documentID)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return false, sql.ErrNoRows
	}
	if _, err := tx.Exec(`UPDATE knowledge_bases SET updated_at=? WHERE id=?`, now, baseID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func knowledgeSourceVersionNewer(leftID, leftModifiedNS, rightID, rightModifiedNS int64) bool {
	if leftModifiedNS != rightModifiedNS {
		return leftModifiedNS > rightModifiedNS
	}
	return leftID > rightID
}

func validateKnowledgeChunks(documentID int64, chunks []KnowledgeChunkInput) error {
	if documentID <= 0 || len(chunks) == 0 {
		return fmt.Errorf("knowledge chunks required")
	}
	for index, chunk := range chunks {
		if chunk.Ordinal != index || strings.TrimSpace(chunk.Content) == "" || utf8.RuneCountInString(chunk.Content) > 16000 {
			return fmt.Errorf("invalid knowledge chunk")
		}
	}
	return nil
}

func replaceKnowledgeChunksTx(tx *sql.Tx, baseID, documentID int64, chunks []KnowledgeChunkInput) error {
	if _, err := tx.Exec(`DELETE FROM knowledge_chunks_fts WHERE document_id=?`, documentID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM knowledge_chunks WHERE document_id=?`, documentID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, chunk := range chunks {
		heading := strings.TrimSpace(chunk.HeadingPath)
		content := strings.TrimSpace(chunk.Content)
		result, err := tx.Exec(`INSERT INTO knowledge_chunks
			(base_id,document_id,ordinal,heading_path,content,char_count,created_at)
			VALUES (?,?,?,?,?,?,?)`, baseID, documentID, chunk.Ordinal, heading, content, utf8.RuneCountInString(content), now)
		if err != nil {
			return err
		}
		chunkID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO knowledge_chunks_fts
			(chunk_id,base_id,document_id,heading_path,content) VALUES (?,?,?,?,?)`,
			chunkID, baseID, documentID, heading, content); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) SearchKnowledgeChunks(baseIDs []int64, query string, limit int) ([]KnowledgeSearchResult, error) {
	return db.searchKnowledgeChunks(baseIDs, nil, query, limit)
}

func (db *DB) searchKnowledgeChunks(baseIDs []int64, scopes []KnowledgeScope, query string, limit int) ([]KnowledgeSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []KnowledgeSearchResult{}, nil
	}
	if limit <= 0 || limit > 12 {
		limit = 6
	}
	terms := strings.Fields(query)
	for _, term := range terms {
		if utf8.RuneCountInString(term) < 3 {
			results, err := db.searchKnowledgeChunksByTerms(baseIDs, scopes, terms, limit, " AND ")
			if err != nil || len(results) > 0 || len(terms) == 1 {
				return results, err
			}
			return db.searchKnowledgeChunksByTerms(baseIDs, scopes, terms, limit, " OR ")
		}
	}
	ftsTerms := make([]string, 0, len(terms))
	for _, term := range terms {
		ftsTerms = append(ftsTerms, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	ftsQuery := strings.Join(ftsTerms, " AND ")
	results, err := db.searchKnowledgeChunksFTS(baseIDs, scopes, ftsQuery, limit)
	if err != nil || len(results) > 0 {
		return results, err
	}
	if len(terms) > 1 {
		results, err = db.searchKnowledgeChunksFTS(baseIDs, scopes, strings.Join(ftsTerms, " OR "), limit)
		if err != nil || len(results) > 0 {
			return results, err
		}
	}
	fallbackTerms := knowledgeCJKBigramTerms(query, 24)
	if len(fallbackTerms) == 0 {
		return results, nil
	}
	candidateLimit := limit * 2
	if candidateLimit > 12 {
		candidateLimit = 12
	}
	candidates, fallbackErr := db.searchKnowledgeChunksByTerms(baseIDs, scopes, fallbackTerms, candidateLimit, " OR ")
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	results = results[:0]
	for _, candidate := range candidates {
		// LIKE fallback scores are the negative number of matching terms.
		// Require two matches so one generic bigram cannot create a result.
		if candidate.Score > -2 {
			continue
		}
		results = append(results, candidate)
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

// SearchKnowledgeChunksScoped applies folder scopes in SQL before ranking and
// limiting candidates, so an unrelated high-ranked folder cannot hide a valid
// result from the selected source prefix.
func (db *DB) SearchKnowledgeChunksScoped(baseIDs []int64, scopes []KnowledgeScope, query string, limit int) ([]KnowledgeSearchResult, error) {
	return db.searchKnowledgeChunks(baseIDs, scopes, query, limit)
}

// Long natural Chinese questions are often entered without spaces. SQLite's
// unicode tokenizer can treat the whole question as an exact phrase, so an
// empty exact result gets one bounded bigram pass. This stays lexical and
// local while preserving meaningful terms such as “短视频”“钩子”“分镜”.
func knowledgeCJKBigramTerms(query string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	stop := map[string]struct{}{
		"如何": {}, "怎么": {}, "一个": {}, "这个": {}, "以及": {},
		"通过": {}, "进行": {}, "可以": {}, "需要": {}, "基于": {}, "使用": {},
	}
	terms := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	run := make([]rune, 0, 16)
	flush := func() bool {
		for index := 0; index+1 < len(run); index++ {
			term := string(run[index : index+2])
			if _, ignored := stop[term]; ignored {
				continue
			}
			if _, exists := seen[term]; exists {
				continue
			}
			seen[term] = struct{}{}
			terms = append(terms, term)
			if len(terms) == limit {
				return true
			}
		}
		run = run[:0]
		return false
	}
	for _, character := range query {
		if unicode.Is(unicode.Han, character) {
			run = append(run, character)
			continue
		}
		if flush() {
			return terms
		}
	}
	flush()
	return terms
}

func (db *DB) searchKnowledgeChunksFTS(baseIDs []int64, scopes []KnowledgeScope, ftsQuery string, limit int) ([]KnowledgeSearchResult, error) {
	filter, filterArgs, err := knowledgeSearchFilter(baseIDs, scopes)
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, len(filterArgs)+2)
	args = append(args, ftsQuery)
	args = append(args, filterArgs...)
	args = append(args, limit)
	rows, err := db.Query(`SELECT c.id,c.base_id,b.name,c.document_id,d.sha256,d.source_name,c.heading_path,c.content,
		bm25(knowledge_chunks_fts,0.0,0.0,0.0,4.0,1.0)
		FROM knowledge_chunks_fts
		JOIN knowledge_chunks c ON c.id=CAST(knowledge_chunks_fts.chunk_id AS INTEGER)
		JOIN knowledge_documents d ON d.id=c.document_id
		JOIN knowledge_bases b ON b.id=c.base_id
		WHERE knowledge_chunks_fts MATCH ? AND d.status='ready'`+filter+`
		ORDER BY bm25(knowledge_chunks_fts,0.0,0.0,0.0,4.0,1.0),c.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]KnowledgeSearchResult, 0)
	for rows.Next() {
		var item KnowledgeSearchResult
		if err := rows.Scan(&item.ChunkID, &item.BaseID, &item.BaseName, &item.DocumentID, &item.DocumentSHA,
			&item.SourceName, &item.HeadingPath, &item.Content, &item.Score); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (db *DB) searchKnowledgeChunksByTerms(baseIDs []int64, scopes []KnowledgeScope, terms []string, limit int, joiner string) ([]KnowledgeSearchResult, error) {
	if joiner != " AND " && joiner != " OR " {
		return nil, fmt.Errorf("invalid knowledge search joiner")
	}
	clauses := make([]string, 0, len(terms))
	scoreClauses := make([]string, 0, len(terms))
	scoreArgs := make([]any, 0, len(terms)*2)
	whereArgs := make([]any, 0, len(terms)*2)
	for _, term := range terms {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
		pattern := "%" + escaped + "%"
		clause := `(c.heading_path LIKE ? ESCAPE '\' OR c.content LIKE ? ESCAPE '\')`
		clauses = append(clauses, clause)
		scoreClauses = append(scoreClauses, `CASE WHEN `+clause+` THEN 1 ELSE 0 END`)
		scoreArgs = append(scoreArgs, pattern, pattern)
		whereArgs = append(whereArgs, pattern, pattern)
	}
	filter, filterArgs, err := knowledgeSearchFilter(baseIDs, scopes)
	if err != nil {
		return nil, err
	}
	args := append(scoreArgs, whereArgs...)
	args = append(args, filterArgs...)
	args = append(args, limit)
	rows, err := db.Query(`SELECT c.id,c.base_id,b.name,c.document_id,d.sha256,d.source_name,c.heading_path,c.content,
		-1.0 * (`+strings.Join(scoreClauses, " + ")+`) AS relevance
		FROM knowledge_chunks c
		JOIN knowledge_documents d ON d.id=c.document_id
		JOIN knowledge_bases b ON b.id=c.base_id
		WHERE d.status='ready' AND (`+strings.Join(clauses, joiner)+`)`+filter+`
		ORDER BY relevance,c.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]KnowledgeSearchResult, 0)
	for rows.Next() {
		var item KnowledgeSearchResult
		if err := rows.Scan(&item.ChunkID, &item.BaseID, &item.BaseName, &item.DocumentID, &item.DocumentSHA,
			&item.SourceName, &item.HeadingPath, &item.Content, &item.Score); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func knowledgeSearchFilter(baseIDs []int64, scopes []KnowledgeScope) (string, []any, error) {
	parts := make([]string, 0, 2)
	args := make([]any, 0, len(baseIDs)+len(scopes)*3)
	if len(baseIDs) > 0 {
		placeholders := make([]string, 0, len(baseIDs))
		for _, id := range baseIDs {
			if id <= 0 {
				return "", nil, fmt.Errorf("invalid knowledge base id")
			}
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		parts = append(parts, `c.base_id IN (`+strings.Join(placeholders, ",")+`)`)
	}
	if len(scopes) > 0 {
		scopeParts := make([]string, 0, len(scopes))
		for _, scope := range scopes {
			if scope.BaseID <= 0 {
				return "", nil, fmt.Errorf("invalid knowledge scope")
			}
			prefix := strings.Trim(strings.ReplaceAll(scope.SourcePrefix, "\\", "/"), "/")
			if prefix == "" {
				scopeParts = append(scopeParts, `c.base_id=?`)
				args = append(args, scope.BaseID)
				continue
			}
			escapedPrefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
			scopeParts = append(scopeParts, `(c.base_id=? AND (d.source_name=? OR d.source_name LIKE ? ESCAPE '\'))`)
			args = append(args, scope.BaseID, prefix, escapedPrefix+"/%")
		}
		parts = append(parts, `(`+strings.Join(scopeParts, " OR ")+`)`)
	}
	if len(parts) == 0 {
		return "", args, nil
	}
	return ` AND ` + strings.Join(parts, " AND "), args, nil
}
