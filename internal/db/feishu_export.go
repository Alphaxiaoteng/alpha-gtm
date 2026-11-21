package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FeishuContentExport is the single binding between this app and one Feishu
// Base that mirrors collected content. A zero row means "not configured yet";
// callers still get a usable struct so the UI can render defaults.
type FeishuContentExport struct {
	Enabled        bool   `json:"enabled"`
	BaseToken      string `json:"base_token"`
	BaseURL        string `json:"base_url"`
	PostTableID    string `json:"post_table_id"`
	MetricsTableID string `json:"metrics_table_id"`
	LastExportAt   string `json:"last_export_at"`
	LastResultJSON string `json:"last_result_json"`
	UpdatedAt      string `json:"updated_at"`
}

// Configured reports whether a Base and both tables are bound.
func (settings FeishuContentExport) Configured() bool {
	return strings.TrimSpace(settings.BaseToken) != "" &&
		strings.TrimSpace(settings.PostTableID) != "" &&
		strings.TrimSpace(settings.MetricsTableID) != ""
}

// FeishuContentExportSettings reads the binding row. A missing row returns an
// empty, disabled struct rather than an error: an unconfigured app is normal.
func (db *DB) FeishuContentExportSettings() (FeishuContentExport, error) {
	var settings FeishuContentExport
	var enabled int
	err := db.QueryRow(`SELECT enabled,base_token,base_url,post_table_id,metrics_table_id,
		last_export_at,last_result_json,updated_at FROM feishu_content_export WHERE id=1`).Scan(
		&enabled, &settings.BaseToken, &settings.BaseURL, &settings.PostTableID,
		&settings.MetricsTableID, &settings.LastExportAt, &settings.LastResultJSON, &settings.UpdatedAt)
	if err == sql.ErrNoRows {
		return FeishuContentExport{}, nil
	}
	if err != nil {
		return FeishuContentExport{}, err
	}
	settings.Enabled = enabled == 1
	return settings, nil
}

// SaveFeishuContentExport writes the binding row, preserving fields the caller
// did not set by reading the current row first.
func (db *DB) SaveFeishuContentExport(settings FeishuContentExport) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := 0
	if settings.Enabled {
		enabled = 1
	}
	if strings.TrimSpace(settings.LastResultJSON) == "" {
		settings.LastResultJSON = "{}"
	}
	_, err := db.Exec(`INSERT INTO feishu_content_export
		(id,enabled,base_token,base_url,post_table_id,metrics_table_id,last_export_at,last_result_json,created_at,updated_at)
		VALUES (1,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			enabled=excluded.enabled, base_token=excluded.base_token, base_url=excluded.base_url,
			post_table_id=excluded.post_table_id, metrics_table_id=excluded.metrics_table_id,
			last_export_at=excluded.last_export_at, last_result_json=excluded.last_result_json,
			updated_at=excluded.updated_at`,
		enabled, strings.TrimSpace(settings.BaseToken), strings.TrimSpace(settings.BaseURL),
		strings.TrimSpace(settings.PostTableID), strings.TrimSpace(settings.MetricsTableID),
		settings.LastExportAt, settings.LastResultJSON, now, now)
	return err
}

// SetFeishuContentExportEnabled flips the mirror without touching the binding,
// so pausing the export never loses which Base it writes to.
func (db *DB) SetFeishuContentExportEnabled(enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE feishu_content_export SET enabled=?, updated_at=? WHERE id=1`, value, now)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("feishu content export is not configured")
	}
	return nil
}

// FeishuRecordLink returns the Feishu record id previously written for one
// business key, or "" when this key has not been exported yet.
func (db *DB) FeishuRecordLink(tableID, businessKey string) (string, error) {
	var recordID string
	err := db.QueryRow(`SELECT record_id FROM feishu_content_record_links
		WHERE table_id=? AND business_key=?`, tableID, businessKey).Scan(&recordID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return recordID, nil
}

// SaveFeishuRecordLink records (or replaces) the Feishu record id for a key.
func (db *DB) SaveFeishuRecordLink(tableID, businessKey, recordID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO feishu_content_record_links
		(table_id,business_key,record_id,updated_at) VALUES (?,?,?,?)
		ON CONFLICT(table_id, business_key) DO UPDATE SET
			record_id=excluded.record_id, updated_at=excluded.updated_at`,
		tableID, businessKey, recordID, now)
	return err
}

// FeishuPostRow is one collected work ready to be mirrored into Feishu.
type FeishuPostRow struct {
	BusinessKey  string
	PlatformCode string
	AccountName  string
	Title        string
	PublishedAt  string
	PostURL      string
	Views        int64
	Likes        int64
	Comments     int64
	Shares       int64
	Favorites    int64
	SnapshotDate string
}

// FeishuAccountMetricRow is one account's daily metric window.
type FeishuAccountMetricRow struct {
	BusinessKey   string
	PlatformCode  string
	AccountName   string
	SnapshotDate  string
	WindowDays    int
	RangeVerified bool
	Followers     int64
	Views         int64
	Likes         int64
	Comments      int64
	Shares        int64
	Favorites     int64
	Posts         int64
}

// ListFeishuPostRows returns the newest snapshot's posts per account, so the
// mirror shows current numbers instead of every historical snapshot.
func (db *DB) ListFeishuPostRows(sinceSnapshotDate string, limit int) ([]FeishuPostRow, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := db.Query(`SELECT
			p.platform_code, a.name, COALESCE(p.title,''), COALESCE(p.published_at,''),
			COALESCE(p.post_url,''), p.views, p.likes, p.comments, p.shares, p.favorites,
			s.snapshot_date, p.account_id, p.post_key
		FROM creator_post_metric_snapshots p
		JOIN creator_metric_snapshots s ON s.id = p.snapshot_id
		JOIN accounts a ON a.id = p.account_id
		WHERE s.snapshot_date >= ?
		ORDER BY s.snapshot_date DESC, p.views DESC, p.id DESC
		LIMIT ?`, sinceSnapshotDate, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FeishuPostRow, 0, 64)
	for rows.Next() {
		var row FeishuPostRow
		var accountID int64
		var postKey string
		if err := rows.Scan(&row.PlatformCode, &row.AccountName, &row.Title, &row.PublishedAt,
			&row.PostURL, &row.Views, &row.Likes, &row.Comments, &row.Shares, &row.Favorites,
			&row.SnapshotDate, &accountID, &postKey); err != nil {
			return nil, err
		}
		row.BusinessKey = fmt.Sprintf("post:%s:%d:%s", row.PlatformCode, accountID, postKey)
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListFeishuAccountMetricRows returns account metric windows from the given
// snapshot date onward.
func (db *DB) ListFeishuAccountMetricRows(sinceSnapshotDate string, limit int) ([]FeishuAccountMetricRow, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := db.Query(`SELECT
			s.platform_code, a.name, s.snapshot_date, s.window_days, s.range_verified,
			s.followers, s.views, s.likes, s.comments, s.shares, s.favorites, s.posts,
			s.account_id
		FROM creator_metric_snapshots s
		JOIN accounts a ON a.id = s.account_id
		WHERE s.snapshot_date >= ?
		ORDER BY s.snapshot_date DESC, s.id DESC
		LIMIT ?`, sinceSnapshotDate, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FeishuAccountMetricRow, 0, 32)
	for rows.Next() {
		var row FeishuAccountMetricRow
		var verified int
		var accountID int64
		if err := rows.Scan(&row.PlatformCode, &row.AccountName, &row.SnapshotDate, &row.WindowDays,
			&verified, &row.Followers, &row.Views, &row.Likes, &row.Comments, &row.Shares,
			&row.Favorites, &row.Posts, &accountID); err != nil {
			return nil, err
		}
		row.RangeVerified = verified == 1
		row.BusinessKey = fmt.Sprintf("acct:%d:%s:%d", accountID, row.SnapshotDate, row.WindowDays)
		out = append(out, row)
	}
	return out, rows.Err()
}
