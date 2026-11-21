package db

import (
	"fmt"
	"strings"
	"time"
)

const (
	FeedbackStatusPending    = "待处理"
	FeedbackStatusProcessing = "处理中"
	FeedbackStatusDone       = "已完成"
	FeedbackStatusRejected   = "不采纳"
)

type FeedbackReceipt struct {
	FeedbackID     string `json:"feedback_id"`
	ReadToken      string `json:"-"`
	Content        string `json:"content"`
	Status         string `json:"status"`
	Category       string `json:"category"`
	DeveloperReply string `json:"developer_reply"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastSyncedAt   string `json:"last_synced_at"`
}

type FeedbackSubmissionIntent struct {
	RequestHash    string
	ClientToken    string
	FeedbackNumber string
	ReadToken      string
}

func validFeedbackStatus(status string) bool {
	switch status {
	case FeedbackStatusPending, FeedbackStatusProcessing, FeedbackStatusDone, FeedbackStatusRejected:
		return true
	default:
		return false
	}
}

func (db *DB) CreateFeedbackReceipt(receipt FeedbackReceipt) error {
	receipt.FeedbackID = strings.TrimSpace(receipt.FeedbackID)
	receipt.ReadToken = strings.TrimSpace(receipt.ReadToken)
	receipt.Content = strings.TrimSpace(receipt.Content)
	if receipt.FeedbackID == "" || receipt.ReadToken == "" || receipt.Content == "" {
		return fmt.Errorf("feedback receipt id, query credential, and content are required")
	}
	if receipt.Status == "" {
		receipt.Status = FeedbackStatusPending
	}
	if !validFeedbackStatus(receipt.Status) {
		return fmt.Errorf("invalid feedback status")
	}
	if receipt.Category == "" {
		receipt.Category = "未分类"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if receipt.CreatedAt == "" {
		receipt.CreatedAt = now
	}
	if receipt.UpdatedAt == "" {
		receipt.UpdatedAt = receipt.CreatedAt
	}
	_, err := db.Exec(`
		INSERT INTO feedback_receipts (
			feedback_id, read_token, content, status, category, developer_reply,
			remote_created_at, remote_updated_at, last_synced_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, receipt.FeedbackID, receipt.ReadToken, receipt.Content, receipt.Status, receipt.Category,
		receipt.DeveloperReply, receipt.CreatedAt, receipt.UpdatedAt, receipt.LastSyncedAt, now, now)
	return err
}

func (db *DB) GetOrCreateFeedbackSubmissionIntent(intent FeedbackSubmissionIntent) (FeedbackSubmissionIntent, error) {
	intent.RequestHash = strings.TrimSpace(intent.RequestHash)
	intent.ClientToken = strings.TrimSpace(intent.ClientToken)
	intent.FeedbackNumber = strings.TrimSpace(intent.FeedbackNumber)
	intent.ReadToken = strings.TrimSpace(intent.ReadToken)
	if intent.RequestHash == "" || intent.ClientToken == "" || intent.FeedbackNumber == "" || intent.ReadToken == "" {
		return FeedbackSubmissionIntent{}, fmt.Errorf("feedback submission intent is incomplete")
	}
	tx, err := db.Begin()
	if err != nil {
		return FeedbackSubmissionIntent{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
		INSERT INTO feedback_submission_intents (
			request_hash, client_token, feedback_number, read_token, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(request_hash) DO NOTHING
	`, intent.RequestHash, intent.ClientToken, intent.FeedbackNumber, intent.ReadToken, now, now); err != nil {
		return FeedbackSubmissionIntent{}, err
	}
	var stored FeedbackSubmissionIntent
	if err := tx.QueryRow(`
		SELECT request_hash, client_token, feedback_number, read_token
		FROM feedback_submission_intents
		WHERE request_hash=?
	`, intent.RequestHash).Scan(
		&stored.RequestHash, &stored.ClientToken, &stored.FeedbackNumber, &stored.ReadToken,
	); err != nil {
		return FeedbackSubmissionIntent{}, err
	}
	if err := tx.Commit(); err != nil {
		return FeedbackSubmissionIntent{}, err
	}
	return stored, nil
}

// CompleteFeedbackSubmission atomically stores the remote receipt and clears
// the durable retry intent. A failed transaction leaves the stable Feishu
// client_token and query credential available for the next identical retry.
func (db *DB) CompleteFeedbackSubmission(requestHash string, receipt FeedbackReceipt) error {
	requestHash = strings.TrimSpace(requestHash)
	receipt.FeedbackID = strings.TrimSpace(receipt.FeedbackID)
	receipt.ReadToken = strings.TrimSpace(receipt.ReadToken)
	receipt.Content = strings.TrimSpace(receipt.Content)
	if requestHash == "" || receipt.FeedbackID == "" || receipt.ReadToken == "" || receipt.Content == "" {
		return fmt.Errorf("feedback completion is incomplete")
	}
	if receipt.Status == "" {
		receipt.Status = FeedbackStatusPending
	}
	if !validFeedbackStatus(receipt.Status) {
		return fmt.Errorf("invalid feedback status")
	}
	if receipt.Category == "" {
		receipt.Category = "未分类"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if receipt.CreatedAt == "" {
		receipt.CreatedAt = now
	}
	if receipt.UpdatedAt == "" {
		receipt.UpdatedAt = receipt.CreatedAt
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expectedReadToken string
	if err := tx.QueryRow(`
		SELECT read_token
		FROM feedback_submission_intents
		WHERE request_hash=?
	`, requestHash).Scan(&expectedReadToken); err != nil {
		return err
	}
	if expectedReadToken != receipt.ReadToken {
		return fmt.Errorf("feedback query credential does not match submission intent")
	}
	if _, err := tx.Exec(`
		INSERT INTO feedback_receipts (
			feedback_id, read_token, content, status, category, developer_reply,
			remote_created_at, remote_updated_at, last_synced_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, receipt.FeedbackID, receipt.ReadToken, receipt.Content, receipt.Status, receipt.Category,
		receipt.DeveloperReply, receipt.CreatedAt, receipt.UpdatedAt, receipt.LastSyncedAt, now, now); err != nil {
		return err
	}
	result, err := tx.Exec(`DELETE FROM feedback_submission_intents WHERE request_hash=?`, requestHash)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("feedback submission intent not found")
	}
	return tx.Commit()
}

func (db *DB) ListFeedbackReceipts(limit int) ([]FeedbackReceipt, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := db.Query(`
		SELECT feedback_id, read_token, content, status, category, developer_reply,
		       remote_created_at, remote_updated_at, last_synced_at
		FROM feedback_receipts
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]FeedbackReceipt, 0)
	for rows.Next() {
		var item FeedbackReceipt
		if err := rows.Scan(
			&item.FeedbackID, &item.ReadToken, &item.Content, &item.Status, &item.Category,
			&item.DeveloperReply, &item.CreatedAt, &item.UpdatedAt, &item.LastSyncedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) UpdateFeedbackReceiptStatus(feedbackID, status, category, reply, remoteUpdatedAt, syncedAt string) error {
	if strings.TrimSpace(feedbackID) == "" || !validFeedbackStatus(status) {
		return fmt.Errorf("invalid feedback status update")
	}
	if category == "" {
		category = "未分类"
	}
	if syncedAt == "" {
		syncedAt = time.Now().UTC().Format(time.RFC3339)
	}
	result, err := db.Exec(`
		UPDATE feedback_receipts
		SET status=?, category=?, developer_reply=?, remote_updated_at=?,
		    last_synced_at=?, updated_at=?
		WHERE feedback_id=?
	`, status, category, reply, remoteUpdatedAt, syncedAt, syncedAt, feedbackID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("feedback receipt not found")
	}
	return nil
}
