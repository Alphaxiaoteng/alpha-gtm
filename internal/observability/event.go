// Package observability defines the process diagnostic event contract.
// SQLite job/audit events remain business truth; this package only emits
// allowlisted, non-sensitive diagnostic fields.
package observability

import (
	"context"
	"log/slog"
	"strings"
)

const SchemaVersion = 1

var stableErrorCodes = map[string]struct{}{
	"ACCOUNT_BROWSER_NOT_RUNNING": {}, "ACCOUNT_DISABLED": {}, "ACCOUNT_NOT_FOUND": {}, "ACCOUNT_STATE_UPDATE_FAILED": {}, "ALREADY_RUNNING": {},
	"ACTION_UNSUPPORTED": {}, "ACTIVE_PAGE_NOT_FOUND": {}, "API_CONNECTOR_UNAVAILABLE": {}, "API_CREDENTIAL_REQUIRED": {}, "API_PUBLISH_FAILED": {}, "ASSET_NOT_FOUND": {},
	"AUTH_FAILED": {}, "AUTH_INVALID": {}, "AUTH_REQUIRED": {}, "AWAITING_HUMAN": {},
	"BROWSER_CDP_REQUIRED": {}, "BROWSER_CRASH": {}, "BROWSER_NOT_FOUND": {}, "BROWSER_NOT_RUNNING": {}, "PUBLISH_BROWSER_DEGRADED": {},
	"BUSY_BY_JOB": {}, "BUSY_BY_UI_SESSION": {}, "CANCELLED": {}, "CDP_CONNECT": {}, "CDP_CONNECT_FAILED": {}, "CDP_NOT_READY": {}, "CDP_PAGE_LIST_FAILED": {}, "CDP_TARGET_CLOSED": {}, "CDP_URL_INVALID": {},
	"CONTENT_INVALID": {}, "CONTENT_NOT_FOUND": {}, "CONTENT_TYPE_INVALID": {}, "CONTENT_TYPE_UNSUPPORTED": {}, "CONTENT_UNKNOWN_FIELD": {}, "DB_MIGRATION_FAILED": {}, "EVENT_PERSIST_FAILED": {}, "FOCUS_FAILED": {},
	"COMPOSER_NAVIGATION_FAILED": {}, "COMPOSER_UNCONFIRMED": {},
	"CLIENT_RUNTIME_ERROR": {}, "DIAGNOSTIC_SNAPSHOT_FAILED": {}, "HTTP_CLIENT_ERROR": {}, "HTTP_SERVER_ERROR": {},
	"FORBIDDEN": {}, "INTERNAL_ERROR": {}, "INVALID_ACCOUNT_ID": {}, "INVALID_DATA_DIR": {},
	"INVALID_STATUS": {}, "INVALID_STEPS": {}, "JOB_CLAIM_FAILED": {}, "JOB_CREATE": {}, "JOB_NOT_CLAIMABLE": {}, "JOB_PERSIST_FAILED": {},
	"INPUT_TOO_LARGE": {}, "INPUT_UNKNOWN_FIELD": {}, "LEASE_EXPIRED": {}, "LOGIN_CHECK_FAILED": {}, "LOGIN_REQUIRED": {}, "MAX_CONCURRENT_BROWSERS": {},
	"MEDIA_FILE_INVALID": {}, "MEDIA_FILE_UNAVAILABLE": {}, "MEDIA_HASH_CHANGED": {}, "MEDIA_INVALID": {}, "MEDIA_ITEM_INVALID": {}, "MEDIA_ITEM_UNKNOWN_FIELD": {}, "MEDIA_REQUIRED": {}, "MEDIA_UPLOAD_UNCONFIRMED": {},
	"META_COVER_UPLOAD_UNVERIFIED": {}, "META_NATIVE_SCHEDULE_UNVERIFIED": {},
	"MISSING_PARAM": {}, "NO_PORT": {}, "NO_PORT_AVAILABLE": {}, "NO_PUBLISHER": {},
	"OBSERVABILITY_EMIT_FAILED": {}, "OPENCODE_NOT_FOUND": {}, "PERMANENT_FAILED": {}, "PLATFORM_NOT_FOUND": {},
	"PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1": {}, "PLATFORM_UNSUPPORTED": {}, "PLAYWRIGHT_RUNTIME_UNAVAILABLE": {}, "PLAYWRIGHT_WORKER_FAILED": {}, "PROFILE_BUSY": {}, "PROFILE_LOCKED": {},
	"PROFILE_NOT_FOUND": {}, "PUBLISH_FAILED": {}, "PUBLISH_NIL_RESULT": {},
	"PLATFORM_VERIFICATION_REQUIRED": {}, "PUBLISH_CLICK_OUTCOME_UNKNOWN": {}, "PUBLISH_RESULT_UNKNOWN": {}, "PUBLISH_SUCCESS_UNVERIFIED": {}, "RECORD_PERSIST_FAILED": {}, "RETRYABLE_FAILED": {},
	"FORM_REVIEW_REQUIRED": {}, "NODE_RUNTIME_UNAVAILABLE": {}, "SCHEMA_VERSION_UNSUPPORTED": {}, "SETTINGS_INVALID": {}, "SETTINGS_UNKNOWN_FIELD": {}, "SETTINGS_UNSUPPORTED": {},
	"EGO_METRICS_BATCH_FAILED": {}, "EGO_METRICS_COLLECTION_FAILED": {}, "EGO_RUNTIME_EXECUTION_FAILED": {}, "EGO_SPACE_BINDING_MISSING": {}, "EGO_SPACE_NOT_FOUND": {}, "EGO_SPACE_REBIND_FAILED": {}, "EGO_SPACE_INACTIVE": {},
	"SUBMIT_APPROVAL_REQUIRED": {}, "SUBMIT_BUTTON_NOT_FOUND": {}, "SUBMIT_FAILED": {}, "SUBMIT_FLAG_INVALID": {}, "TAGS_INVALID": {}, "TIMEOUT_INVALID": {},
	"TARGET_SELECTION_UNCONFIRMED": {},
	"WORKER_ACTION_INVALID":        {}, "WORKER_ACTION_TIMEOUT": {}, "WORKER_CANCELLED": {}, "WORKER_EXECUTION_FAILED": {}, "WORKER_FAILED": {}, "WORKER_FORM_PREPARE_FAILED": {}, "WORKER_INPUT_INVALID": {}, "WORKER_OUTPUT_INVALID": {},
	"WORKER_RUNTIME_TYPE_ERROR": {}, "WORKER_SESSION_FAILED": {}, "WORKER_START_FAILED": {}, "WORKER_TARGET_PAGE_FAILED": {}, "TERMINAL_LOCALHOST_ONLY": {}, "TIMEOUT": {}, "TOOL_NOT_ALLOWED": {},
	"TOOL_NOT_FOUND": {}, "TOOL_NOT_IMPLEMENTED": {}, "TRANSPORT_AUTH_REQUIRED": {},
	"UNAUTHORIZED": {}, "VALIDATION_FAILED": {},
}

// Event contains only fields approved for diagnostic logs. It intentionally has
// no message, args, arbitrary attributes, content, URL, or filesystem path.
type Event struct {
	Level           slog.Level
	Component       string
	Name            string
	TraceID         string
	RequestID       string
	TaskID          string
	AccountID       int64
	Platform        string
	JobID           int64
	PublishRecordID int64
	BatchID         int64
	Attempt         int
	Step            string
	Status          string
	ErrorCode       string
	ErrorClass      string
	Retryable       bool
	DurationMS      int64
	HTTPStatus      int
	AppVersion      string
	SidecarVersion  string
	PID             int
}

// Correlation carries non-secret identifiers through context.Context.
type Correlation struct {
	RequestID       string
	TaskID          string
	AccountID       int64
	Platform        string
	JobID           int64
	PublishRecordID int64
	BatchID         int64
}

type correlationKey struct{}

func WithCorrelation(ctx context.Context, c Correlation) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := CorrelationFromContext(ctx)
	if c.RequestID != "" {
		merged.RequestID = c.RequestID
	}
	if c.TaskID != "" {
		merged.TaskID = c.TaskID
	}
	if c.AccountID > 0 {
		merged.AccountID = c.AccountID
	}
	if c.Platform != "" {
		merged.Platform = c.Platform
	}
	if c.JobID > 0 {
		merged.JobID = c.JobID
	}
	if c.PublishRecordID > 0 {
		merged.PublishRecordID = c.PublishRecordID
	}
	if c.BatchID > 0 {
		merged.BatchID = c.BatchID
	}
	return context.WithValue(ctx, correlationKey{}, merged)
}

func CorrelationFromContext(ctx context.Context) Correlation {
	if ctx == nil {
		return Correlation{}
	}
	c, _ := ctx.Value(correlationKey{}).(Correlation)
	return c
}

// StableErrorCode extracts a machine code without returning raw error text.
func StableErrorCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if i := strings.IndexAny(raw, ": "); i >= 0 {
		raw = raw[:i]
	}
	if len(raw) < 2 || len(raw) > 64 {
		return "INTERNAL_ERROR"
	}
	for _, r := range raw {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return "INTERNAL_ERROR"
		}
	}
	if _, ok := stableErrorCodes[raw]; ok {
		return raw
	}
	return "INTERNAL_ERROR"
}

func ErrorClass(code string) string {
	switch {
	case code == "":
		return ""
	case strings.Contains(code, "TIMEOUT"):
		return "timeout"
	case strings.Contains(code, "CANCEL"):
		return "cancelled"
	case strings.HasPrefix(code, "MISSING_") || strings.HasPrefix(code, "INVALID_"):
		return "validation"
	case strings.Contains(code, "AUTH") || strings.Contains(code, "FORBIDDEN"):
		return "authorization"
	case strings.HasPrefix(code, "TOOL_"):
		return "tool"
	case code == "INTERNAL_ERROR":
		return "internal"
	default:
		return "domain"
	}
}

func safeToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return ""
		}
	}
	return value
}
