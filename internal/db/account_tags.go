package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AccountTagModeAppend  = "append"
	AccountTagModeRemove  = "remove"
	AccountTagModeReplace = "replace"
)

type AccountTag struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Color        string `json:"color"`
	Position     int    `json:"position"`
	AccountCount int    `json:"accountCount"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

func normalizeAccountTag(name, color string) (string, string, error) {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	color = strings.ToUpper(strings.TrimSpace(color))
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return "", "", fmt.Errorf("tag name must contain 1-40 characters")
	}
	if !validHexColor(color) {
		return "", "", fmt.Errorf("tag color must be #RRGGBB")
	}
	return name, color, nil
}

func validHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, char := range value[1:] {
		if !((char >= '0' && char <= '9') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

// UpsertAccountTag is idempotent by case-insensitive name. Reusing a name
// updates only its display color; account associations are preserved.
func (db *DB) UpsertAccountTag(name, color string) (*AccountTag, error) {
	name, color, err := normalizeAccountTag(name, color)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO account_tags (name, color, position, created_at, updated_at)
		VALUES (?, ?, (SELECT COALESCE(MAX(position) + 1, 0) FROM account_tags), ?, ?)
		ON CONFLICT(name) DO UPDATE SET color=excluded.color, updated_at=excluded.updated_at`, name, color, now, now); err != nil {
		return nil, err
	}
	var tag AccountTag
	if err := db.QueryRow(`SELECT id, name, color, position, created_at, updated_at FROM account_tags WHERE name=? COLLATE NOCASE`, name).
		Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Position, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
		return nil, err
	}
	return &tag, nil
}

func (db *DB) ListAccountTags() ([]AccountTag, error) {
	rows, err := db.Query(`SELECT t.id, t.name, t.color, t.position, COUNT(l.account_id), t.created_at, t.updated_at
		FROM account_tags t LEFT JOIN account_tag_links l ON l.tag_id=t.id
		GROUP BY t.id ORDER BY t.position, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([]AccountTag, 0)
	for rows.Next() {
		var tag AccountTag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Position, &tag.AccountCount, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (db *DB) ListAccountTagsByAccount() (map[int64][]AccountTag, error) {
	rows, err := db.Query(`SELECT l.account_id, t.id, t.name, t.color, t.position, t.created_at, t.updated_at,
		(SELECT COUNT(*) FROM account_tag_links count_links WHERE count_links.tag_id=t.id)
		FROM account_tag_links l JOIN account_tags t ON t.id=l.tag_id
		ORDER BY l.account_id, t.position, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64][]AccountTag)
	for rows.Next() {
		var accountID int64
		var tag AccountTag
		if err := rows.Scan(&accountID, &tag.ID, &tag.Name, &tag.Color, &tag.Position, &tag.CreatedAt, &tag.UpdatedAt, &tag.AccountCount); err != nil {
			return nil, err
		}
		result[accountID] = append(result[accountID], tag)
	}
	return result, rows.Err()
}

// ReorderAccountTags persists the complete user-visible tag order. Requiring
// the full set prevents a stale client from silently hiding or duplicating tags.
func (db *DB) ReorderAccountTags(tagIDs []int64) error {
	if len(tagIDs) > 50 {
		return fmt.Errorf("too many ids: maximum 50")
	}
	seen := make(map[int64]struct{}, len(tagIDs))
	for _, tagID := range tagIDs {
		if tagID <= 0 {
			return fmt.Errorf("ids must be positive")
		}
		if _, exists := seen[tagID]; exists {
			return fmt.Errorf("tag order must not contain duplicates")
		}
		seen[tagID] = struct{}{}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var storedCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM account_tags`).Scan(&storedCount); err != nil {
		return err
	}
	if storedCount != len(tagIDs) {
		return fmt.Errorf("tag order must contain every tag exactly once")
	}
	if count, err := countExistingIDs(tx, "account_tags", tagIDs); err != nil {
		return err
	} else if count != len(tagIDs) {
		return fmt.Errorf("one or more tags do not exist")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for position, tagID := range tagIDs {
		if _, err := tx.Exec(`UPDATE account_tags SET position=?, updated_at=? WHERE id=?`, position, now, tagID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func normalizePositiveIDs(values []int64, max int, allowEmpty bool) ([]int64, error) {
	if len(values) == 0 && !allowEmpty {
		return nil, fmt.Errorf("at least one id is required")
	}
	if len(values) > max {
		return nil, fmt.Errorf("too many ids: maximum %d", max)
	}
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, fmt.Errorf("ids must be positive")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func int64Args(values []int64) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func countExistingIDs(tx *sql.Tx, table string, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id IN (`+sqlPlaceholders(len(ids))+`)`, int64Args(ids)...).Scan(&count)
	return count, err
}

// BulkUpdateAccountTags changes every selected account in one transaction.
// Replace accepts an empty tag list to clear the selection deliberately.
func (db *DB) BulkUpdateAccountTags(accountIDs, tagIDs []int64, mode string) error {
	accountIDs, err := normalizePositiveIDs(accountIDs, 100, false)
	if err != nil {
		return err
	}
	tagIDs, err = normalizePositiveIDs(tagIDs, 50, true)
	if err != nil {
		return err
	}
	if mode != AccountTagModeAppend && mode != AccountTagModeRemove && mode != AccountTagModeReplace {
		return fmt.Errorf("tag update mode must be append, remove, or replace")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	accountCount, err := countExistingIDs(tx, "accounts", accountIDs)
	if err != nil || accountCount != len(accountIDs) {
		if err != nil {
			return err
		}
		return fmt.Errorf("one or more accounts do not exist")
	}
	tagCount, err := countExistingIDs(tx, "account_tags", tagIDs)
	if err != nil || tagCount != len(tagIDs) {
		if err != nil {
			return err
		}
		return fmt.Errorf("one or more tags do not exist")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, accountID := range accountIDs {
		if mode == AccountTagModeReplace {
			if _, err := tx.Exec(`DELETE FROM account_tag_links WHERE account_id=?`, accountID); err != nil {
				return err
			}
		}
		for _, tagID := range tagIDs {
			switch mode {
			case AccountTagModeAppend, AccountTagModeReplace:
				if _, err := tx.Exec(`INSERT OR IGNORE INTO account_tag_links (account_id, tag_id, created_at) VALUES (?, ?, ?)`, accountID, tagID, now); err != nil {
					return err
				}
			case AccountTagModeRemove:
				if _, err := tx.Exec(`DELETE FROM account_tag_links WHERE account_id=? AND tag_id=?`, accountID, tagID); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// ListAccountsByTagIDs returns the union of accounts matching any selected
// tag. Each account appears once, including same-platform multi-account rows.
func (db *DB) ListAccountsByTagIDs(tagIDs []int64) ([]Account, error) {
	tagIDs, err := normalizePositiveIDs(tagIDs, 50, false)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT DISTINCT a.id FROM accounts a
		JOIN account_tag_links l ON l.account_id=a.id
		WHERE l.tag_id IN (`+sqlPlaceholders(len(tagIDs))+`)
		ORDER BY a.platform_code, a.sort, a.id`, int64Args(tagIDs)...)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(ids))
	for _, id := range ids {
		account, err := db.GetAccount(id)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *account)
	}
	return accounts, nil
}
