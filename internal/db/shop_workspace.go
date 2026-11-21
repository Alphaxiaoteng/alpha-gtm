package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Shop content and knowledge intentionally use their own tables. Browser
// identities, accounts, Spaces, and media-library files remain shared assets.
func (db *DB) CreateShopContent(contentType, title, caption, body, tagsJSON, status, plannedAt string) (*Content, error) {
	if !validContentType(contentType) || !validContentStatus(status) {
		return nil, fmt.Errorf("invalid content type or status")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`INSERT INTO shop_contents(type,title,caption,body,tags_json,status,planned_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, contentType, title, caption, body, tagsJSON, status, plannedAt, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.GetShopContent(id)
}

func (db *DB) GetShopContent(id int64) (*Content, error) {
	if id <= 0 {
		return nil, fmt.Errorf("shop content id must be positive")
	}
	var item Content
	err := db.QueryRow(`SELECT c.id,c.type,c.title,c.caption,c.body,c.tags_json,c.status,0,c.planned_at,
		COALESCE(m.id,0),COALESCE(CASE WHEN l.mime LIKE 'video/%' THEN 'video' ELSE 'image' END,''),COALESCE(l.mime,'')
		FROM shop_contents c
		LEFT JOIN shop_content_media m ON m.id=(SELECT id FROM shop_content_media WHERE content_id=c.id ORDER BY CASE WHEN role='cover' THEN 0 ELSE 1 END,position,id LIMIT 1)
		LEFT JOIN media_library l ON l.id=m.library_id WHERE c.id=?`, id).
		Scan(&item.ID, &item.Type, &item.Title, &item.Caption, &item.Body, &item.TagsJSON, &item.Status,
			&item.PublishCount, &item.PlannedAt, &item.CoverMediaID, &item.CoverKind, &item.CoverMimeType)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("shop content %d not found", id)
	}
	return &item, err
}

func (db *DB) ListShopContents() ([]Content, error) {
	rows, err := db.Query(`SELECT c.id,c.type,c.title,c.caption,c.body,c.tags_json,c.status,0,c.planned_at,
		COALESCE(m.id,0),COALESCE(CASE WHEN l.mime LIKE 'video/%' THEN 'video' ELSE 'image' END,''),COALESCE(l.mime,'')
		FROM shop_contents c
		LEFT JOIN shop_content_media m ON m.id=(SELECT id FROM shop_content_media WHERE content_id=c.id ORDER BY CASE WHEN role='cover' THEN 0 ELSE 1 END,position,id LIMIT 1)
		LEFT JOIN media_library l ON l.id=m.library_id
		ORDER BY CASE WHEN c.planned_at='' THEN 1 ELSE 0 END,c.planned_at,c.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Content, 0)
	for rows.Next() {
		var item Content
		if err := rows.Scan(&item.ID, &item.Type, &item.Title, &item.Caption, &item.Body, &item.TagsJSON, &item.Status,
			&item.PublishCount, &item.PlannedAt, &item.CoverMediaID, &item.CoverKind, &item.CoverMimeType); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) ReplaceShopContent(id int64, contentType, title, caption, body, tagsJSON, status, plannedAt string) error {
	if id <= 0 || !validContentType(contentType) || !validContentStatus(status) {
		return fmt.Errorf("invalid shop content")
	}
	result, err := db.Exec(`UPDATE shop_contents SET type=?,title=?,caption=?,body=?,tags_json=?,status=?,planned_at=?,updated_at=? WHERE id=?`,
		contentType, title, caption, body, tagsJSON, status, plannedAt, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("shop content %d not found", id)
	}
	return nil
}

func (db *DB) DeleteShopContent(id int64) error {
	result, err := db.Exec(`DELETE FROM shop_contents WHERE id=?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("shop content %d not found", id)
	}
	return nil
}

func (db *DB) AttachShopContentMedia(contentID, libraryID int64, role string, position int) (*MediaAsset, error) {
	if contentID <= 0 || libraryID <= 0 || (role != "content" && role != "cover") || position < 0 {
		return nil, fmt.Errorf("invalid shop content media")
	}
	if _, err := db.GetShopContent(contentID); err != nil {
		return nil, err
	}
	if _, err := db.GetMediaLibraryItem(libraryID); err != nil {
		return nil, err
	}
	result, err := db.Exec(`INSERT INTO shop_content_media(content_id,library_id,role,position,created_at) VALUES (?,?,?,?,?)
		ON CONFLICT(content_id,library_id,role) DO UPDATE SET position=excluded.position`,
		contentID, libraryID, role, position, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	if id == 0 {
		// UPSERT 命中 DO UPDATE 时 LastInsertId 为 0，回查是正常路径；
		// 回查本身失败就不能继续——用一个 id=0 去读详情只会得到误导性的错误。
		if err := db.QueryRow(`SELECT id FROM shop_content_media WHERE content_id=? AND library_id=? AND role=?`, contentID, libraryID, role).Scan(&id); err != nil {
			return nil, fmt.Errorf("resolve shop content media id: %w", err)
		}
	}
	return db.GetShopContentMedia(id)
}

func (db *DB) GetShopContentMedia(id int64) (*MediaAsset, error) {
	var item MediaAsset
	err := db.QueryRow(`SELECT m.id,m.content_id,m.library_id,m.role,m.position,
		CASE WHEN l.mime LIKE 'video/%' THEN 'video' ELSE 'image' END,l.file_path,'',l.mime,l.size_bytes,l.sha256,l.width,l.height,l.duration_ms
		FROM shop_content_media m JOIN media_library l ON l.id=m.library_id WHERE m.id=?`, id).
		Scan(&item.ID, &item.ContentID, &item.LibraryID, &item.Role, &item.Position, &item.Kind, &item.FilePath,
			&item.OriginalPath, &item.MimeType, &item.SizeBytes, &item.SHA256, &item.Width, &item.Height, &item.DurationMS)
	return &item, err
}

func (db *DB) ListShopContentMedia(contentID int64) ([]MediaAsset, error) {
	rows, err := db.Query(`SELECT m.id,m.content_id,m.library_id,m.role,m.position,
		CASE WHEN l.mime LIKE 'video/%' THEN 'video' ELSE 'image' END,l.file_path,'',l.mime,l.size_bytes,l.sha256,l.width,l.height,l.duration_ms
		FROM shop_content_media m JOIN media_library l ON l.id=m.library_id WHERE m.content_id=? ORDER BY m.role,m.position,m.id`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]MediaAsset, 0)
	for rows.Next() {
		var item MediaAsset
		if err := rows.Scan(&item.ID, &item.ContentID, &item.LibraryID, &item.Role, &item.Position, &item.Kind, &item.FilePath,
			&item.OriginalPath, &item.MimeType, &item.SizeBytes, &item.SHA256, &item.Width, &item.Height, &item.DurationMS); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) CreateShopKnowledgeBase(name, description string) (*KnowledgeBase, error) {
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if name == "" || utf8.RuneCountInString(name) > 120 || utf8.RuneCountInString(description) > 2000 {
		return nil, fmt.Errorf("invalid shop knowledge base")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`INSERT INTO shop_knowledge_bases(name,description,is_default,top_k,max_context_chars,created_at,updated_at)
		VALUES (?,?,CASE WHEN NOT EXISTS(SELECT 1 FROM shop_knowledge_bases) THEN 1 ELSE 0 END,6,12000,?,?)`, name, description, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return db.GetShopKnowledgeBase(id)
}

const shopKnowledgeBaseSummaryQuery = `SELECT b.id,b.name,b.description,b.is_default,b.top_k,b.max_context_chars,b.created_at,b.updated_at,
	(SELECT COUNT(*) FROM shop_knowledge_documents d WHERE d.base_id=b.id AND d.status='ready'),
	(SELECT COUNT(*) FROM shop_knowledge_chunks c JOIN shop_knowledge_documents d ON d.id=c.document_id WHERE c.base_id=b.id AND d.status='ready'),
	r.id,r.base_id,r.status,r.total_files,r.imported_files,r.skipped_files,r.failed_files,r.created_at,r.completed_at
	FROM shop_knowledge_bases b LEFT JOIN shop_knowledge_import_runs r ON r.id=(SELECT id FROM shop_knowledge_import_runs WHERE base_id=b.id ORDER BY id DESC LIMIT 1)`

func (db *DB) GetShopKnowledgeBase(id int64) (*KnowledgeBase, error) {
	return scanKnowledgeBase(db.QueryRow(shopKnowledgeBaseSummaryQuery+` WHERE b.id=?`, id))
}

func (db *DB) ListShopKnowledgeBases() ([]KnowledgeBase, error) {
	rows, err := db.Query(shopKnowledgeBaseSummaryQuery + ` ORDER BY b.is_default DESC,b.updated_at DESC,b.id DESC`)
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

func (db *DB) ListShopKnowledgeDocuments(baseID int64) ([]KnowledgeDocument, error) {
	rows, err := db.Query(`SELECT id,base_id,COALESCE(import_run_id,0),source_name,source_type,sha256,status,error_code,error_message,original_path,normalized_path,created_at,updated_at
		FROM shop_knowledge_documents WHERE base_id=? ORDER BY updated_at DESC,id DESC`, baseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]KnowledgeDocument, 0)
	for rows.Next() {
		var item KnowledgeDocument
		if err := rows.Scan(&item.ID, &item.BaseID, &item.ImportRunID, &item.SourceName, &item.SourceType, &item.SHA256,
			&item.Status, &item.ErrorCode, &item.ErrorMessage, &item.OriginalPath, &item.NormalizedPath, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) CreateShopKnowledgeImportRun(baseID int64) (*KnowledgeImportRun, error) {
	if _, err := db.GetShopKnowledgeBase(baseID); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`INSERT INTO shop_knowledge_import_runs(base_id,status,created_at) VALUES (?,'running',?)`, baseID, now)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &KnowledgeImportRun{ID: id, BaseID: baseID, Status: "running", CreatedAt: now}, nil
}

func (db *DB) CompleteShopKnowledgeImportRun(id int64, total, imported, skipped, failed int) error {
	status := "completed"
	if failed > 0 {
		status = "completed_with_errors"
	}
	_, err := db.Exec(`UPDATE shop_knowledge_import_runs SET status=?,total_files=?,imported_files=?,skipped_files=?,failed_files=?,completed_at=? WHERE id=? AND status='running'`,
		status, total, imported, skipped, failed, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (db *DB) ImportShopKnowledgeDocument(baseID int64, sourceName, sourceType, sha, originalPath, normalizedPath string, chunks []KnowledgeChunkInput) (*KnowledgeDocument, bool, error) {
	if baseID <= 0 || strings.TrimSpace(sourceName) == "" || !knowledgeSHA256Pattern.MatchString(sha) || len(chunks) == 0 {
		return nil, false, fmt.Errorf("invalid shop knowledge document")
	}
	var existingID int64
	if err := db.QueryRow(`SELECT id FROM shop_knowledge_documents WHERE base_id=? AND sha256=?`, baseID, sha).Scan(&existingID); err == nil {
		items, listErr := db.ListShopKnowledgeDocuments(baseID)
		for index := range items {
			if items[index].ID == existingID {
				return &items[index], false, listErr
			}
		}
		return nil, false, listErr
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`INSERT INTO shop_knowledge_documents(base_id,source_name,source_type,sha256,status,original_path,normalized_path,created_at,updated_at)
		VALUES (?,?,?,?,'ready',?,?,?,?)`, baseID, sourceName, sourceType, sha, originalPath, normalizedPath, now, now)
	if err != nil {
		return nil, false, err
	}
	documentID, _ := result.LastInsertId()
	for index, chunk := range chunks {
		if chunk.Ordinal != index || strings.TrimSpace(chunk.Content) == "" {
			return nil, false, fmt.Errorf("invalid shop knowledge chunk")
		}
		chunkResult, err := tx.Exec(`INSERT INTO shop_knowledge_chunks(base_id,document_id,ordinal,heading_path,content,char_count,created_at) VALUES (?,?,?,?,?,?,?)`,
			baseID, documentID, index, strings.TrimSpace(chunk.HeadingPath), strings.TrimSpace(chunk.Content), utf8.RuneCountInString(chunk.Content), now)
		if err != nil {
			return nil, false, err
		}
		chunkID, _ := chunkResult.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO shop_knowledge_chunks_fts(chunk_id,base_id,document_id,heading_path,content) VALUES (?,?,?,?,?)`,
			chunkID, baseID, documentID, strings.TrimSpace(chunk.HeadingPath), strings.TrimSpace(chunk.Content)); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Exec(`UPDATE shop_knowledge_bases SET updated_at=? WHERE id=?`, now, baseID); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	items, err := db.ListShopKnowledgeDocuments(baseID)
	for index := range items {
		if items[index].ID == documentID {
			return &items[index], true, err
		}
	}
	return nil, true, err
}

func (db *DB) SearchShopKnowledgeChunks(baseIDs []int64, query string, limit int) ([]KnowledgeSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []KnowledgeSearchResult{}, nil
	}
	if limit <= 0 || limit > 12 {
		limit = 6
	}
	where, args := `d.status='ready' AND (instr(c.heading_path,?)>0 OR instr(c.content,?)>0)`, []any{query, query}
	if len(baseIDs) > 0 {
		placeholders := make([]string, len(baseIDs))
		for index, id := range baseIDs {
			placeholders[index] = "?"
			args = append(args, id)
		}
		where += ` AND c.base_id IN (` + strings.Join(placeholders, ",") + `)`
	}
	args = append(args, limit)
	rows, err := db.Query(`SELECT c.id,c.base_id,b.name,c.document_id,d.sha256,d.source_name,c.heading_path,c.content,0
		FROM shop_knowledge_chunks c JOIN shop_knowledge_documents d ON d.id=c.document_id JOIN shop_knowledge_bases b ON b.id=c.base_id
		WHERE `+where+` ORDER BY c.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]KnowledgeSearchResult, 0)
	for rows.Next() {
		var item KnowledgeSearchResult
		if err := rows.Scan(&item.ChunkID, &item.BaseID, &item.BaseName, &item.DocumentID, &item.DocumentSHA, &item.SourceName, &item.HeadingPath, &item.Content, &item.Score); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
