package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ContentVariantInput struct {
	ContentID    int64
	PlatformCode string
	AccountID    int64
	Title        string
	Caption      string
	Body         string
	TagsJSON     string
	Source       string
}

type ContentVariant struct {
	ID           int64  `json:"id"`
	ContentID    int64  `json:"contentId"`
	PlatformCode string `json:"platformCode"`
	AccountID    int64  `json:"accountId,omitempty"`
	Title        string `json:"title"`
	Caption      string `json:"caption"`
	Body         string `json:"body"`
	TagsJSON     string `json:"tagsJson"`
	Revision     int    `json:"revision"`
	Source       string `json:"source"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

func normalizeVariantInput(input ContentVariantInput) (ContentVariantInput, error) {
	input.PlatformCode = strings.TrimSpace(input.PlatformCode)
	input.Source = strings.TrimSpace(input.Source)
	if input.Source == "" {
		input.Source = "manual"
	}
	if input.ContentID <= 0 || input.PlatformCode == "" || input.AccountID < 0 {
		return input, fmt.Errorf("content, platform, and optional account are required")
	}
	if len(input.Title)+len(input.Caption)+len(input.Body)+len(input.TagsJSON) > 1<<20 {
		return input, fmt.Errorf("content variant exceeds 1 MiB")
	}
	if strings.TrimSpace(input.TagsJSON) == "" {
		input.TagsJSON = "[]"
	}
	var tags []interface{}
	if err := json.Unmarshal([]byte(input.TagsJSON), &tags); err != nil {
		return input, fmt.Errorf("content variant tags must be a JSON array")
	}
	canonicalTags, _ := json.Marshal(tags)
	input.TagsJSON = string(canonicalTags)
	return input, nil
}

func (db *DB) UpsertContentVariant(input ContentVariantInput) (*ContentVariant, error) {
	input, err := normalizeVariantInput(input)
	if err != nil {
		return nil, err
	}
	if _, err := db.GetContent(input.ContentID); err != nil {
		return nil, err
	}
	if _, err := db.GetPlatform(input.PlatformCode); err != nil {
		return nil, err
	}
	if input.AccountID > 0 {
		account, err := db.GetAccount(input.AccountID)
		if err != nil {
			return nil, err
		}
		if account.PlatformCode != input.PlatformCode {
			return nil, fmt.Errorf("variant account platform mismatch")
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var existingID int64
	if input.AccountID > 0 {
		err = db.QueryRow(`SELECT id FROM content_variants WHERE content_id=? AND platform_code=? AND account_id=?`, input.ContentID, input.PlatformCode, input.AccountID).Scan(&existingID)
	} else {
		err = db.QueryRow(`SELECT id FROM content_variants WHERE content_id=? AND platform_code=? AND account_id IS NULL`, input.ContentID, input.PlatformCode).Scan(&existingID)
	}
	switch err {
	case nil:
		_, err = db.Exec(`UPDATE content_variants SET title=?, caption=?, body=?, tags_json=?, source=?, revision=revision+1, updated_at=? WHERE id=?`, input.Title, input.Caption, input.Body, input.TagsJSON, input.Source, now, existingID)
	case sql.ErrNoRows:
		var accountID interface{}
		if input.AccountID > 0 {
			accountID = input.AccountID
		}
		result, insertErr := db.Exec(`INSERT INTO content_variants (content_id, platform_code, account_id, title, caption, body, tags_json, revision, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`, input.ContentID, input.PlatformCode, accountID, input.Title, input.Caption, input.Body, input.TagsJSON, input.Source, now, now)
		if insertErr != nil {
			return nil, insertErr
		}
		existingID, err = result.LastInsertId()
		if err == nil {
			err = nil
		}
	default:
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return db.GetContentVariant(existingID)
}

func (db *DB) GetContentVariant(id int64) (*ContentVariant, error) {
	var variant ContentVariant
	var accountID sql.NullInt64
	err := db.QueryRow(`SELECT id, content_id, platform_code, account_id, title, caption, body, tags_json, revision, source, created_at, updated_at
		FROM content_variants WHERE id=?`, id).
		Scan(&variant.ID, &variant.ContentID, &variant.PlatformCode, &accountID, &variant.Title, &variant.Caption, &variant.Body, &variant.TagsJSON, &variant.Revision, &variant.Source, &variant.CreatedAt, &variant.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("content variant %d not found", id)
	}
	if err != nil {
		return nil, err
	}
	if accountID.Valid {
		variant.AccountID = accountID.Int64
	}
	return &variant, nil
}
