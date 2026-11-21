package db

import (
	"database/sql"
	"fmt"
	"time"
)

const maxContentKnowledgeCitations = 12

type ContentKnowledgeCitation struct {
	ContentID      int64  `json:"content_id"`
	Ordinal        int    `json:"ordinal"`
	ChunkID        int64  `json:"chunk_id"`
	BaseID         int64  `json:"base_id"`
	DocumentSHA256 string `json:"document_sha256"`
	SourceName     string `json:"source_name"`
	HeadingPath    string `json:"heading_path"`
	CreatedAt      string `json:"created_at"`
}

func normalizeContentKnowledgeChunkIDs(chunkIDs []int64) ([]int64, error) {
	if len(chunkIDs) > maxContentKnowledgeCitations {
		return nil, fmt.Errorf("too many knowledge citations")
	}
	result := make([]int64, 0, len(chunkIDs))
	seen := make(map[int64]struct{}, len(chunkIDs))
	for _, chunkID := range chunkIDs {
		if chunkID <= 0 {
			return nil, fmt.Errorf("invalid knowledge citation chunk id")
		}
		if _, exists := seen[chunkID]; exists {
			continue
		}
		seen[chunkID] = struct{}{}
		result = append(result, chunkID)
	}
	return result, nil
}

func resolveContentKnowledgeCitations(tx *sql.Tx, contentID int64, chunkIDs []int64) ([]ContentKnowledgeCitation, error) {
	normalized, err := normalizeContentKnowledgeChunkIDs(chunkIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	items := make([]ContentKnowledgeCitation, 0, len(normalized))
	for ordinal, chunkID := range normalized {
		item := ContentKnowledgeCitation{ContentID: contentID, Ordinal: ordinal, ChunkID: chunkID, CreatedAt: now}
		err := tx.QueryRow(`SELECT c.base_id,d.sha256,d.source_name,c.heading_path
			FROM knowledge_chunks c
			JOIN knowledge_documents d ON d.id=c.document_id
			WHERE c.id=? AND d.status='ready'`, chunkID).
			Scan(&item.BaseID, &item.DocumentSHA256, &item.SourceName, &item.HeadingPath)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("knowledge citation chunk is unavailable")
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func replaceContentKnowledgeCitationsTx(tx *sql.Tx, contentID int64, chunkIDs []int64) error {
	items, err := resolveContentKnowledgeCitations(tx, contentID, chunkIDs)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM content_knowledge_citations WHERE content_id=?`, contentID); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.Exec(`INSERT INTO content_knowledge_citations
			(content_id,ordinal,chunk_id,base_id,document_sha256,source_name,heading_path,created_at)
			VALUES (?,?,?,?,?,?,?,?)`, item.ContentID, item.Ordinal, item.ChunkID, item.BaseID,
			item.DocumentSHA256, item.SourceName, item.HeadingPath, item.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) CreateContentWithPlannedAtAndKnowledgeCitations(contentType, title, caption, body, tagsJSON, status, plannedAt string, chunkIDs []int64) (*Content, error) {
	if !validContentType(contentType) || !validContentStatus(status) {
		return nil, fmt.Errorf("invalid content type or status")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.Exec(`INSERT INTO contents (type,title,caption,body,tags_json,status,planned_at,source,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,'manual',?,?)`, contentType, title, caption, body, tagsJSON, status, plannedAt, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := replaceContentKnowledgeCitationsTx(tx, id, chunkIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Content{ID: id, Type: contentType, Title: title, Caption: caption, Body: body, TagsJSON: tagsJSON, Status: status, PlannedAt: plannedAt}, nil
}

func (db *DB) ReplaceContentWithPlannedAtAndKnowledgeCitations(id int64, contentType, title, caption, body, tagsJSON, status, plannedAt string, chunkIDs []int64) error {
	if id <= 0 {
		return fmt.Errorf("content id must be positive")
	}
	if !validContentType(contentType) || !validContentStatus(status) {
		return fmt.Errorf("invalid content type or status")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.Exec(`UPDATE contents SET type=?,title=?,caption=?,body=?,tags_json=?,status=?,planned_at=?,revision=revision+1,updated_at=? WHERE id=?`,
		contentType, title, caption, body, tagsJSON, status, plannedAt, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("content %d not found", id)
	}
	if err := replaceContentKnowledgeCitationsTx(tx, id, chunkIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ListContentKnowledgeCitations(contentID int64) ([]ContentKnowledgeCitation, error) {
	if contentID <= 0 {
		return nil, fmt.Errorf("content id must be positive")
	}
	rows, err := db.Query(`SELECT content_id,ordinal,chunk_id,base_id,document_sha256,source_name,heading_path,created_at
		FROM content_knowledge_citations WHERE content_id=? ORDER BY ordinal`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ContentKnowledgeCitation, 0)
	for rows.Next() {
		var item ContentKnowledgeCitation
		if err := rows.Scan(&item.ContentID, &item.Ordinal, &item.ChunkID, &item.BaseID, &item.DocumentSHA256,
			&item.SourceName, &item.HeadingPath, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
