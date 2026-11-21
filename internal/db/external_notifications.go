package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const DefaultNotificationReminderHours = 12

type NotificationChannel struct {
	Kind         string   `json:"kind"`
	Enabled      bool     `json:"enabled"`
	SMTPHost     string   `json:"smtp_host,omitempty"`
	SMTPPort     int      `json:"smtp_port,omitempty"`
	SMTPSecurity string   `json:"smtp_security,omitempty"`
	SMTPUsername string   `json:"smtp_username,omitempty"`
	EmailFrom    string   `json:"email_from,omitempty"`
	EmailTo      []string `json:"email_to,omitempty"`
	UpdatedAt    string   `json:"updated_at"`
}

type NotificationDelivery struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	EventType   string `json:"event_type"`
	AccountID   int64  `json:"account_id,omitempty"`
	Status      string `json:"status"`
	ErrorCode   string `json:"error_code,omitempty"`
	CreatedAt   string `json:"created_at"`
	DeliveredAt string `json:"delivered_at,omitempty"`
}

func validNotificationChannelKind(kind string) bool {
	switch kind {
	case "feishu", "dingtalk", "webhook", "email":
		return true
	default:
		return false
	}
}

func (db *DB) ListNotificationChannels() ([]NotificationChannel, error) {
	rows, err := db.Query(`SELECT kind, enabled, smtp_host, smtp_port, smtp_security, smtp_username, email_from, email_to_json, updated_at
		FROM notification_channels ORDER BY CASE kind WHEN 'feishu' THEN 1 WHEN 'dingtalk' THEN 2 WHEN 'webhook' THEN 3 ELSE 4 END`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	channels := make([]NotificationChannel, 0, 4)
	for rows.Next() {
		channel, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, *channel)
	}
	return channels, rows.Err()
}

func (db *DB) GetNotificationChannel(kind string) (*NotificationChannel, error) {
	if !validNotificationChannelKind(kind) {
		return nil, fmt.Errorf("invalid notification channel kind")
	}
	return scanNotificationChannel(db.QueryRow(`SELECT kind, enabled, smtp_host, smtp_port, smtp_security, smtp_username, email_from, email_to_json, updated_at
		FROM notification_channels WHERE kind=?`, kind))
}

type notificationChannelScanner interface {
	Scan(...any) error
}

func scanNotificationChannel(scanner notificationChannelScanner) (*NotificationChannel, error) {
	var channel NotificationChannel
	var enabled int
	var recipientsJSON string
	if err := scanner.Scan(&channel.Kind, &enabled, &channel.SMTPHost, &channel.SMTPPort, &channel.SMTPSecurity, &channel.SMTPUsername, &channel.EmailFrom, &recipientsJSON, &channel.UpdatedAt); err != nil {
		return nil, err
	}
	channel.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(recipientsJSON), &channel.EmailTo); err != nil {
		return nil, fmt.Errorf("decode notification recipients: %w", err)
	}
	if channel.EmailTo == nil {
		channel.EmailTo = []string{}
	}
	return &channel, nil
}

func (db *DB) SaveNotificationChannel(channel NotificationChannel) (*NotificationChannel, error) {
	channel.Kind = strings.TrimSpace(channel.Kind)
	if !validNotificationChannelKind(channel.Kind) {
		return nil, fmt.Errorf("invalid notification channel kind")
	}
	if channel.EmailTo == nil {
		channel.EmailTo = []string{}
	}
	recipientsJSON, err := json.Marshal(channel.EmailTo)
	if err != nil {
		return nil, fmt.Errorf("encode notification recipients: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO notification_channels(kind, enabled, smtp_host, smtp_port, smtp_security, smtp_username, email_from, email_to_json, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(kind) DO UPDATE SET enabled=excluded.enabled, smtp_host=excluded.smtp_host, smtp_port=excluded.smtp_port,
			smtp_security=excluded.smtp_security, smtp_username=excluded.smtp_username, email_from=excluded.email_from,
			email_to_json=excluded.email_to_json, updated_at=excluded.updated_at`,
		channel.Kind, boolInt(channel.Enabled), channel.SMTPHost, channel.SMTPPort, channel.SMTPSecurity,
		channel.SMTPUsername, channel.EmailFrom, string(recipientsJSON), now)
	if err != nil {
		return nil, err
	}
	return db.GetNotificationChannel(channel.Kind)
}

func (db *DB) NotificationReminderHours() (int, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM notification_settings WHERE key='reminder_interval_hours'`).Scan(&value)
	if err == sql.ErrNoRows {
		return DefaultNotificationReminderHours, nil
	}
	if err != nil {
		return 0, err
	}
	hours, err := strconv.Atoi(value)
	if err != nil || (hours != 12 && hours != 24) {
		return 0, fmt.Errorf("invalid persisted notification reminder interval")
	}
	return hours, nil
}

func (db *DB) SetNotificationReminderHours(hours int) error {
	if hours != 12 && hours != 24 {
		return fmt.Errorf("notification reminder interval must be 12 or 24 hours")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO notification_settings(key, value, updated_at) VALUES ('reminder_interval_hours', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, strconv.Itoa(hours), now)
	return err
}

func (db *DB) CreateNotificationDelivery(kind, eventType string, accountID int64) (*NotificationDelivery, error) {
	if !validNotificationChannelKind(kind) || strings.TrimSpace(eventType) == "" {
		return nil, fmt.Errorf("invalid notification delivery")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO notification_deliveries(kind, event_type, account_id, status, error_code, created_at, delivered_at)
		VALUES (?,?,?,'pending','',?,'')`, kind, eventType, nullableID(accountID), now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &NotificationDelivery{ID: id, Kind: kind, EventType: eventType, AccountID: accountID, Status: "pending", CreatedAt: now}, nil
}

func (db *DB) CompleteNotificationDelivery(id int64, errorCode string) error {
	if id <= 0 {
		return fmt.Errorf("notification delivery id must be positive")
	}
	status := "delivered"
	deliveredAt := time.Now().UTC().Format(time.RFC3339)
	if errorCode != "" {
		status = "failed"
		deliveredAt = ""
	}
	res, err := db.Exec(`UPDATE notification_deliveries SET status=?, error_code=?, delivered_at=? WHERE id=? AND status='pending'`, status, errorCode, deliveredAt, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("notification delivery is not pending")
	}
	return nil
}

func (db *DB) LatestAccountNotificationAttempt(accountID int64, kind string) (time.Time, bool, error) {
	if accountID <= 0 || !validNotificationChannelKind(kind) {
		return time.Time{}, false, fmt.Errorf("invalid notification delivery lookup")
	}
	var createdAt string
	err := db.QueryRow(`SELECT created_at FROM notification_deliveries
		WHERE account_id=? AND kind=? AND event_type IN ('account_offline','account_offline_reminder')
		ORDER BY created_at DESC, id DESC LIMIT 1`, accountID, kind).Scan(&createdAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse notification delivery time: %w", err)
	}
	return parsed, true, nil
}

func (db *DB) GetNotificationDelivery(id int64) (*NotificationDelivery, error) {
	var delivery NotificationDelivery
	var accountID sql.NullInt64
	err := db.QueryRow(`SELECT id, kind, event_type, account_id, status, error_code, created_at, delivered_at FROM notification_deliveries WHERE id=?`, id).
		Scan(&delivery.ID, &delivery.Kind, &delivery.EventType, &accountID, &delivery.Status, &delivery.ErrorCode, &delivery.CreatedAt, &delivery.DeliveredAt)
	if err != nil {
		return nil, err
	}
	if accountID.Valid {
		delivery.AccountID = accountID.Int64
	}
	return &delivery, nil
}
