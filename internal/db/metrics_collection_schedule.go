package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MetricsCollectionSchedule is the single-row auto-collection switch: the
// monitor persists its run state here the same way the session monitor does.
type MetricsCollectionSchedule struct {
	Enabled        bool            `json:"enabled"`
	IntervalHours  int             `json:"interval_hours"`
	State          string          `json:"state"`
	LastStartedAt  string          `json:"last_started_at"`
	LastFinishedAt string          `json:"last_finished_at"`
	LastResult     json.RawMessage `json:"last_result"`
	UpdatedAt      string          `json:"updated_at"`
}

// MetricsCollectionResult summarizes one scheduled sweep for the settings UI.
type MetricsCollectionResult struct {
	Eligible  int `json:"eligible"`
	Collected int `json:"collected"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

var ErrMetricsCollectionRunning = errors.New("metrics collection is already running")

func (db *DB) MetricsCollectionScheduleSettings() (*MetricsCollectionSchedule, error) {
	var settings MetricsCollectionSchedule
	var enabled int
	var result string
	err := db.QueryRow(`SELECT enabled, interval_hours, state,
		last_started_at, last_finished_at, last_result_json, updated_at
		FROM metrics_collection_schedule WHERE id=1`).
		Scan(&enabled, &settings.IntervalHours, &settings.State,
			&settings.LastStartedAt, &settings.LastFinishedAt, &result, &settings.UpdatedAt)
	if err != nil {
		return nil, err
	}
	settings.Enabled = enabled == 1
	settings.LastResult = json.RawMessage(result)
	if !json.Valid(settings.LastResult) {
		settings.LastResult = json.RawMessage(`{}`)
	}
	return &settings, nil
}

func (db *DB) UpdateMetricsCollectionSchedule(enabled bool, intervalHours int) (*MetricsCollectionSchedule, error) {
	if intervalHours < 1 || intervalHours > 168 {
		return nil, fmt.Errorf("metrics collection interval must be between 1 and 168 hours")
	}
	nowText := time.Now().UTC().Format(time.RFC3339)
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	if _, err := db.Exec(`UPDATE metrics_collection_schedule SET enabled=?, interval_hours=?, updated_at=? WHERE id=1`,
		enabledInt, intervalHours, nowText); err != nil {
		return nil, err
	}
	return db.MetricsCollectionScheduleSettings()
}

func (db *DB) BeginMetricsCollectionRun(now time.Time) (*MetricsCollectionSchedule, error) {
	nowText := now.UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE metrics_collection_schedule SET state='running', last_started_at=?,
		updated_at=? WHERE id=1 AND state='idle'`, nowText, nowText)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrMetricsCollectionRunning
	}
	return db.MetricsCollectionScheduleSettings()
}

func (db *DB) FinishMetricsCollectionRun(now time.Time, errorCode string, result MetricsCollectionResult) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	nowText := now.UTC().Format(time.RFC3339)
	_, err = db.Exec(`UPDATE metrics_collection_schedule SET state='idle', last_finished_at=?,
		last_error_code=?, last_result_json=?, updated_at=? WHERE id=1 AND state='running'`,
		nowText, errorCode, string(encoded), nowText)
	return err
}

// MetricsCollectionCandidate is one enabled logged-in account with its
// newest snapshot date; the scheduler applies the platform catalog filter
// (egolite.SupportsCreatorMetrics) on top.
type MetricsCollectionCandidate struct {
	ID           int64
	PlatformCode string
	LatestDate   string
}

// StaleLoggedInAccounts returns enabled logged-in accounts whose newest
// creator-metric snapshot is older than maxAgeHours (never-collected
// accounts are always included).
func (db *DB) StaleLoggedInAccounts(now time.Time, maxAgeHours int) ([]MetricsCollectionCandidate, error) {
	rows, err := db.Query(`SELECT id, platform_code, login_state,
		COALESCE((SELECT MAX(snapshot_date) FROM creator_metric_snapshots s WHERE s.account_id = a.id), '')
		FROM accounts a WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cutoff := now.Add(-time.Duration(maxAgeHours) * time.Hour).Format("2006-01-02")
	candidates := make([]MetricsCollectionCandidate, 0)
	for rows.Next() {
		var id int64
		var platformCode, loginState, latestDate string
		if err := rows.Scan(&id, &platformCode, &loginState, &latestDate); err != nil {
			return nil, err
		}
		if loginState != AccountAuthLoggedIn {
			continue
		}
		if latestDate != "" && latestDate >= cutoff {
			continue
		}
		candidates = append(candidates, MetricsCollectionCandidate{ID: id, PlatformCode: platformCode, LatestDate: latestDate})
	}
	return candidates, rows.Err()
}
