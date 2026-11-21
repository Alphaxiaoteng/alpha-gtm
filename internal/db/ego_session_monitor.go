package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrEgoSessionMonitorRunning = errors.New("ego session monitor is already running")

type EgoSessionMonitorSettings struct {
	Enabled         bool            `json:"enabled"`
	IntervalMinutes int             `json:"interval_minutes"`
	Concurrency     int             `json:"concurrency"`
	State           string          `json:"state"`
	LastStartedAt   string          `json:"last_started_at"`
	LastFinishedAt  string          `json:"last_finished_at"`
	LastErrorCode   string          `json:"last_error_code"`
	LastResult      json.RawMessage `json:"last_result"`
	UpdatedAt       string          `json:"updated_at"`
}

func (db *DB) EgoSessionMonitorSettings() (*EgoSessionMonitorSettings, error) {
	var settings EgoSessionMonitorSettings
	var enabled int
	var result string
	err := db.QueryRow(`SELECT enabled, interval_minutes, concurrency, state,
		last_started_at, last_finished_at, last_error_code, last_result_json, updated_at
		FROM ego_session_monitor WHERE id=1`).
		Scan(&enabled, &settings.IntervalMinutes, &settings.Concurrency, &settings.State,
			&settings.LastStartedAt, &settings.LastFinishedAt, &settings.LastErrorCode, &result, &settings.UpdatedAt)
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

func (db *DB) UpdateEgoSessionMonitor(enabled bool, intervalMinutes int) (*EgoSessionMonitorSettings, error) {
	if intervalMinutes < 1 || intervalMinutes > 1440 {
		return nil, fmt.Errorf("session monitor interval must be between 1 and 1440 minutes")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE ego_session_monitor SET enabled=?, interval_minutes=?, updated_at=? WHERE id=1`,
		boolInt(enabled), intervalMinutes, now); err != nil {
		return nil, err
	}
	return db.EgoSessionMonitorSettings()
}

func (db *DB) BeginEgoSessionMonitorRun(now time.Time) (*EgoSessionMonitorSettings, error) {
	nowText := now.UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE ego_session_monitor SET state='running', last_started_at=?,
		last_error_code='', updated_at=? WHERE id=1 AND state='idle'`, nowText, nowText)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrEgoSessionMonitorRunning
	}
	return db.EgoSessionMonitorSettings()
}

func (db *DB) FinishEgoSessionMonitorRun(now time.Time, errorCode string, result any) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode session monitor result: %w", err)
	}
	nowText := now.UTC().Format(time.RFC3339)
	update, err := db.Exec(`UPDATE ego_session_monitor SET state='idle', last_finished_at=?,
		last_error_code=?, last_result_json=?, updated_at=? WHERE id=1 AND state='running'`,
		nowText, errorCode, string(encoded), nowText)
	if err != nil {
		return err
	}
	changed, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("session monitor run is not active")
	}
	return nil
}

func (db *DB) RecoverEgoSessionMonitorRun(now time.Time) error {
	nowText := now.UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE ego_session_monitor SET state='idle', last_finished_at=?,
		last_error_code='SESSION_MONITOR_INTERRUPTED', updated_at=? WHERE id=1 AND state='running'`,
		nowText, nowText)
	return err
}
