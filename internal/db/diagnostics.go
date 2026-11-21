package db

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
)

// DiagnosticSnapshot is a deliberately payload-free view of local health and
// recent lifecycle events. It contains IDs and reviewed codes, never content,
// URLs, filesystem paths, profile names, account names, or raw messages.
type DiagnosticSnapshot struct {
	SchemaVersion int                      `json:"schema_version"`
	GeneratedAt   string                   `json:"generated_at"`
	Database      DiagnosticDatabase       `json:"database"`
	Counts        DiagnosticCounts         `json:"counts"`
	Operations    []OperationEvent         `json:"operation_events"`
	Jobs          []DiagnosticJobEvent     `json:"job_events"`
	Browsers      []DiagnosticBrowserEvent `json:"browser_events"`
	Accounts      []DiagnosticAccountEvent `json:"account_login_events"`
	Audits        []DiagnosticAuditEvent   `json:"audit_events"`
	AgentRuns     []DiagnosticAgentRun     `json:"agent_runs"`
}

type DiagnosticDatabase struct {
	QuickCheck    string `json:"quick_check"`
	JournalMode   string `json:"journal_mode"`
	ForeignKeys   bool   `json:"foreign_keys"`
	UserVersion   int64  `json:"user_version"`
	PageCount     int64  `json:"page_count"`
	FreelistCount int64  `json:"freelist_count"`
}

type DiagnosticCounts struct {
	AccountsTotal           int64 `json:"accounts_total"`
	AccountsLoggedIn        int64 `json:"accounts_logged_in"`
	BrowserInstancesRunning int64 `json:"browser_instances_running"`
	PublishJobsActive       int64 `json:"publish_jobs_active"`
	PublishJobsFailed       int64 `json:"publish_jobs_failed"`
	QueueActive             int64 `json:"queue_active"`
	QueueFailed             int64 `json:"queue_failed"`
	AgentRunsActive         int64 `json:"agent_runs_active"`
	AgentRunsFailed         int64 `json:"agent_runs_failed"`
	NotificationsUnread     int64 `json:"notifications_unread"`
	OperationEventsTotal    int64 `json:"operation_events_total"`
	BrowserEventsTotal      int64 `json:"browser_events_total"`
	AccountEventsTotal      int64 `json:"account_events_total"`
	JobEventsTotal          int64 `json:"job_events_total"`
	AuditEventsTotal        int64 `json:"audit_events_total"`
}

type DiagnosticJobEvent struct {
	ID            int64  `json:"id"`
	JobID         int64  `json:"job_id"`
	Level         string `json:"level"`
	Step          string `json:"step,omitempty"`
	EventType     string `json:"event_type"`
	TraceID       string `json:"trace_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
	ErrorCode     string `json:"error_code,omitempty"`
	ErrorClass    string `json:"error_class,omitempty"`
	Retryable     bool   `json:"retryable"`
	SchemaVersion int    `json:"schema_version"`
	CreatedAt     string `json:"created_at"`
}

type DiagnosticBrowserEvent struct {
	ID         int64  `json:"id"`
	ProfileRef string `json:"profile_ref"`
	EventType  string `json:"event_type"`
	ReasonCode string `json:"reason_code,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	TermSignal *int   `json:"term_signal,omitempty"`
	PID        int    `json:"pid,omitempty"`
	CDPPort    int    `json:"cdp_port,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type DiagnosticAccountEvent struct {
	ID                  int64  `json:"id"`
	AccountID           int64  `json:"account_id"`
	AuthState           string `json:"auth_state"`
	PersistedLoginState string `json:"persisted_login_state"`
	BrowserState        string `json:"browser_state"`
	LoginMode           string `json:"login_mode"`
	AuthMethod          string `json:"auth_method"`
	ReasonCode          string `json:"reason_code"`
	EvidenceCode        string `json:"evidence_code"`
	AvatarSource        string `json:"avatar_source"`
	AvatarErrorCode     string `json:"avatar_error_code,omitempty"`
	ProfileChanged      bool   `json:"profile_changed"`
	ObservedAt          string `json:"observed_at"`
}

type DiagnosticAuditEvent struct {
	ID          int64  `json:"id"`
	TraceID     string `json:"trace_id,omitempty"`
	Level       string `json:"level"`
	Category    string `json:"category,omitempty"`
	RelatedType string `json:"related_type,omitempty"`
	RelatedID   int64  `json:"related_id,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type DiagnosticAgentRun struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func (db *DB) BuildDiagnosticSnapshot(limit int) (*DiagnosticSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	snapshot := &DiagnosticSnapshot{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Database:      DiagnosticDatabase{QuickCheck: "ok"},
	}
	if err := db.QuickCheck(); err != nil {
		return nil, err
	}
	var foreignKeys int
	for query, target := range map[string]any{
		"PRAGMA journal_mode":   &snapshot.Database.JournalMode,
		"PRAGMA foreign_keys":   &foreignKeys,
		"PRAGMA user_version":   &snapshot.Database.UserVersion,
		"PRAGMA page_count":     &snapshot.Database.PageCount,
		"PRAGMA freelist_count": &snapshot.Database.FreelistCount,
	} {
		if err := db.QueryRow(query).Scan(target); err != nil {
			return nil, fmt.Errorf("read diagnostic database pragma: %w", err)
		}
	}
	snapshot.Database.ForeignKeys = foreignKeys == 1

	counts := []struct {
		query  string
		target *int64
	}{
		{`SELECT COUNT(*) FROM accounts`, &snapshot.Counts.AccountsTotal},
		{`SELECT COUNT(*) FROM accounts WHERE login_state='logged_in'`, &snapshot.Counts.AccountsLoggedIn},
		{`SELECT COUNT(*) FROM browser_runtime_instances WHERE state NOT IN ('stopped','crashed')`, &snapshot.Counts.BrowserInstancesRunning},
		{`SELECT COUNT(*) FROM publish_jobs WHERE status IN ('pending','running','retrying')`, &snapshot.Counts.PublishJobsActive},
		{`SELECT COUNT(*) FROM publish_jobs WHERE status='failed'`, &snapshot.Counts.PublishJobsFailed},
		{`SELECT COUNT(*) FROM task_queue WHERE status IN ('pending','running')`, &snapshot.Counts.QueueActive},
		{`SELECT COUNT(*) FROM task_queue WHERE status='failed'`, &snapshot.Counts.QueueFailed},
		{`SELECT COUNT(*) FROM agent_runs WHERE status='running'`, &snapshot.Counts.AgentRunsActive},
		{`SELECT COUNT(*) FROM agent_runs WHERE status='failed'`, &snapshot.Counts.AgentRunsFailed},
		{`SELECT COUNT(*) FROM notifications WHERE read=0`, &snapshot.Counts.NotificationsUnread},
		{`SELECT COUNT(*) FROM operation_events`, &snapshot.Counts.OperationEventsTotal},
		{`SELECT COUNT(*) FROM browser_events`, &snapshot.Counts.BrowserEventsTotal},
		{`SELECT COUNT(*) FROM account_login_events`, &snapshot.Counts.AccountEventsTotal},
		{`SELECT COUNT(*) FROM job_events`, &snapshot.Counts.JobEventsTotal},
		{`SELECT COUNT(*) FROM audit_events`, &snapshot.Counts.AuditEventsTotal},
	}
	for _, count := range counts {
		if err := db.QueryRow(count.query).Scan(count.target); err != nil {
			return nil, fmt.Errorf("read diagnostic count: %w", err)
		}
	}

	operations, err := db.ListOperationEvents(limit)
	if err != nil {
		return nil, err
	}
	for index := range operations {
		sanitizeDiagnosticOperation(&operations[index])
	}
	snapshot.Operations = operations
	if snapshot.Jobs, err = db.listDiagnosticJobEvents(limit); err != nil {
		return nil, err
	}
	if snapshot.Browsers, err = db.listDiagnosticBrowserEvents(limit); err != nil {
		return nil, err
	}
	if snapshot.Accounts, err = db.listDiagnosticAccountEvents(limit); err != nil {
		return nil, err
	}
	if snapshot.Audits, err = db.listDiagnosticAuditEvents(limit); err != nil {
		return nil, err
	}
	if snapshot.AgentRuns, err = db.listDiagnosticAgentRuns(limit); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (db *DB) listDiagnosticJobEvents(limit int) ([]DiagnosticJobEvent, error) {
	rows, err := db.Query(`SELECT id,job_id,level,COALESCE(step,''),event_type,COALESCE(trace_id,''),
		COALESCE(request_id,''),COALESCE(task_id,''),duration_ms,COALESCE(error_code,''),
		COALESCE(error_class,''),retryable,schema_version,created_at
		FROM job_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DiagnosticJobEvent, 0)
	for rows.Next() {
		var event DiagnosticJobEvent
		var retryable int
		if err := rows.Scan(&event.ID, &event.JobID, &event.Level, &event.Step, &event.EventType, &event.TraceID,
			&event.RequestID, &event.TaskID, &event.DurationMS, &event.ErrorCode, &event.ErrorClass,
			&retryable, &event.SchemaVersion, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.Retryable = retryable == 1
		event.Level = diagnosticToken(event.Level)
		event.Step = diagnosticToken(event.Step)
		event.EventType = diagnosticToken(event.EventType)
		event.TraceID = diagnosticToken(event.TraceID)
		event.RequestID = diagnosticToken(event.RequestID)
		event.TaskID = diagnosticToken(event.TaskID)
		event.ErrorCode = diagnosticToken(event.ErrorCode)
		event.ErrorClass = diagnosticToken(event.ErrorClass)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (db *DB) listDiagnosticBrowserEvents(limit int) ([]DiagnosticBrowserEvent, error) {
	rows, err := db.Query(`SELECT id,profile_id,event_type,COALESCE(reason_code,''),exit_code,term_signal,
		COALESCE(pid,0),COALESCE(cdp_port,0),created_at FROM browser_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DiagnosticBrowserEvent, 0)
	for rows.Next() {
		var event DiagnosticBrowserEvent
		var profileID string
		var exitCode, termSignal sql.NullInt64
		if err := rows.Scan(&event.ID, &profileID, &event.EventType, &event.ReasonCode, &exitCode,
			&termSignal, &event.PID, &event.CDPPort, &event.CreatedAt); err != nil {
			return nil, err
		}
		hash := sha256.Sum256([]byte(profileID))
		event.ProfileRef = fmt.Sprintf("%x", hash[:6])
		event.EventType = diagnosticToken(event.EventType)
		event.ReasonCode = diagnosticToken(event.ReasonCode)
		if exitCode.Valid {
			value := int(exitCode.Int64)
			event.ExitCode = &value
		}
		if termSignal.Valid {
			value := int(termSignal.Int64)
			event.TermSignal = &value
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (db *DB) listDiagnosticAccountEvents(limit int) ([]DiagnosticAccountEvent, error) {
	rows, err := db.Query(`SELECT id,account_id,auth_state,persisted_login_state,browser_state,login_mode,
		auth_method,reason_code,evidence_code,avatar_source,COALESCE(avatar_error_code,''),
		profile_changed,observed_at FROM account_login_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DiagnosticAccountEvent, 0)
	for rows.Next() {
		var event DiagnosticAccountEvent
		var changed int
		if err := rows.Scan(&event.ID, &event.AccountID, &event.AuthState, &event.PersistedLoginState,
			&event.BrowserState, &event.LoginMode, &event.AuthMethod, &event.ReasonCode, &event.EvidenceCode,
			&event.AvatarSource, &event.AvatarErrorCode, &changed, &event.ObservedAt); err != nil {
			return nil, err
		}
		event.ProfileChanged = changed == 1
		event.AuthState = diagnosticToken(event.AuthState)
		event.PersistedLoginState = diagnosticToken(event.PersistedLoginState)
		event.BrowserState = diagnosticToken(event.BrowserState)
		event.LoginMode = diagnosticToken(event.LoginMode)
		event.AuthMethod = diagnosticToken(event.AuthMethod)
		event.ReasonCode = diagnosticToken(event.ReasonCode)
		event.EvidenceCode = diagnosticToken(event.EvidenceCode)
		event.AvatarSource = diagnosticToken(event.AvatarSource)
		event.AvatarErrorCode = diagnosticToken(event.AvatarErrorCode)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (db *DB) listDiagnosticAuditEvents(limit int) ([]DiagnosticAuditEvent, error) {
	rows, err := db.Query(`SELECT id,COALESCE(trace_id,''),level,COALESCE(category,''),
		COALESCE(related_type,''),COALESCE(related_id,0),created_at
		FROM audit_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DiagnosticAuditEvent, 0)
	for rows.Next() {
		var event DiagnosticAuditEvent
		if err := rows.Scan(&event.ID, &event.TraceID, &event.Level, &event.Category,
			&event.RelatedType, &event.RelatedID, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.TraceID = diagnosticToken(event.TraceID)
		event.Level = diagnosticToken(event.Level)
		event.Category = diagnosticToken(event.Category)
		event.RelatedType = diagnosticToken(event.RelatedType)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (db *DB) listDiagnosticAgentRuns(limit int) ([]DiagnosticAgentRun, error) {
	rows, err := db.Query(`SELECT id,provider_id,status,COALESCE(error_code,''),created_at,updated_at
		FROM agent_runs ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DiagnosticAgentRun, 0)
	for rows.Next() {
		var run DiagnosticAgentRun
		if err := rows.Scan(&run.ID, &run.ProviderID, &run.Status, &run.ErrorCode, &run.CreatedAt, &run.UpdatedAt); err != nil {
			return nil, err
		}
		run.ID = diagnosticToken(run.ID)
		run.ProviderID = diagnosticToken(run.ProviderID)
		run.Status = diagnosticToken(run.Status)
		run.ErrorCode = diagnosticToken(run.ErrorCode)
		out = append(out, run)
	}
	return out, rows.Err()
}

func sanitizeDiagnosticOperation(event *OperationEvent) {
	event.TraceID = diagnosticToken(event.TraceID)
	event.RequestID = diagnosticToken(event.RequestID)
	event.TaskID = diagnosticToken(event.TaskID)
	event.PlatformCode = diagnosticToken(event.PlatformCode)
	event.Component = diagnosticToken(event.Component)
	event.EventType = diagnosticToken(event.EventType)
	event.Step = diagnosticToken(event.Step)
	event.Status = diagnosticToken(event.Status)
	event.Level = diagnosticToken(event.Level)
	event.ErrorCode = diagnosticToken(event.ErrorCode)
}

func diagnosticToken(value string) string {
	if value == "" {
		return ""
	}
	if validOperationEventToken(value) {
		return value
	}
	return "redacted"
}
