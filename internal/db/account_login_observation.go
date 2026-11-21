package db

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	AccountAuthLoggedIn  = "logged_in"
	AccountAuthLoggedOut = "logged_out"
	AccountAuthUnknown   = "unknown"

	LoginModeBrowserProfileSession = "browser_profile_session"
	LoginModeManualBrowser         = "manual_browser"
	LoginModeUnknown               = "unknown"

	AuthMethodQR      = "qr"
	AuthMethodPhone   = "phone"
	AuthMethodOAuth   = "oauth"
	AuthMethodUnknown = "unknown"

	AvatarSourceObserved         = "observed"
	AvatarSourcePlatformFallback = "platform_fallback"
	AvatarSourceLegacyRemote     = "legacy_remote"
	AvatarErrorCaptureFailed     = "AVATAR_CAPTURE_FAILED"
	AvatarErrorPersistFailed     = "AVATAR_PERSIST_FAILED"

	LoginReasonVerifiedCookie         = "VERIFIED_COOKIE"
	LoginReasonVerifiedProfileDOM     = "VERIFIED_PROFILE_DOM"
	LoginReasonVerifiedSessionDOM     = "VERIFIED_SESSION_DOM"
	LoginReasonLoginRequiredPage      = "LOGIN_REQUIRED_PAGE"
	LoginReasonSessionExpiredPage     = "SESSION_EXPIRED_PAGE"
	LoginReasonCheckpointRequired     = "CHECKPOINT_REQUIRED"
	LoginReasonProfileSelectorMissing = "PROFILE_SELECTOR_MISSING"
	LoginReasonPlatformTabMissing     = "PLATFORM_TAB_MISSING"
	LoginReasonPlatformUnreachable    = "PLATFORM_UNREACHABLE"
	LoginReasonCDPUnreachable         = "CDP_UNREACHABLE"
	LoginReasonBrowserProcessExited   = "BROWSER_PROCESS_EXITED"
	LoginReasonManualVerifyRequired   = "MANUAL_VERIFY_REQUIRED"
	LoginReasonBrowserOperationBusy   = "BROWSER_OPERATION_BUSY"

	LoginEvidenceReviewedCookie         = "REVIEWED_COOKIE_NAME"
	LoginEvidenceReviewedProfileDOM     = "REVIEWED_PROFILE_DOM"
	LoginEvidenceReviewedSessionDOM     = "REVIEWED_SESSION_DOM"
	LoginEvidenceExplicitLoginDOM       = "EXPLICIT_LOGIN_DOM"
	LoginEvidenceExplicitExpiredDOM     = "EXPLICIT_EXPIRED_DOM"
	LoginEvidenceCheckpointDOM          = "CHECKPOINT_DOM"
	LoginEvidenceMissing                = "EVIDENCE_MISSING"
	LoginEvidenceOperationLeaseBusy     = "OPERATION_LEASE_BUSY"
	LoginEvidenceBrowserStoppedByUser   = "BROWSER_STOPPED_BY_USER"
	LoginEvidenceBrowserProcessExited   = "BROWSER_PROCESS_EXITED"
	LoginEvidenceBrowserCrashed         = "BROWSER_CRASHED"
	LoginEvidenceBrowserHeartbeatFailed = "BROWSER_HEARTBEAT_FAILED"
	LoginEvidenceBrowserLeaseLost       = "BROWSER_LEASE_LOST"
)

type AccountLoginObservation struct {
	AccountID         int64
	AuthState         string
	BrowserState      string
	LoginMode         string
	AuthMethod        string
	ReasonCode        string
	EvidenceCode      string
	PageOrigin        string
	Name              string
	AccountIdentifier string
	AvatarRef         string
	AvatarSource      string
	AvatarSHA256      string
	AvatarErrorCode   string
	ObservedAt        string
	TraceID           string
}

type AccountLoginEvent struct {
	ID                  int64  `json:"id"`
	AccountID           int64  `json:"accountId"`
	AuthState           string `json:"authState"`
	PersistedLoginState string `json:"persistedLoginState"`
	BrowserState        string `json:"browserState"`
	LoginMode           string `json:"loginMode"`
	AuthMethod          string `json:"authMethod"`
	ReasonCode          string `json:"reasonCode"`
	EvidenceCode        string `json:"evidenceCode"`
	PageOrigin          string `json:"pageOrigin,omitempty"`
	Name                string `json:"name,omitempty"`
	AccountIdentifier   string `json:"accountIdentifier,omitempty"`
	AvatarSource        string `json:"avatarSource"`
	AvatarSHA256        string `json:"avatarSha256,omitempty"`
	AvatarErrorCode     string `json:"avatarErrorCode,omitempty"`
	ProfileChanged      bool   `json:"profileChanged"`
	TraceID             string `json:"traceId"`
	ObservedAt          string `json:"observedAt"`
	CreatedAt           string `json:"createdAt"`
	EvidenceChanged     bool   `json:"-"`
}

// BrowserLifecycleEvent is the safe account-browser timeline. Raw event
// messages are deliberately omitted because process stderr and platform URLs
// are not part of the diagnostic contract.
type BrowserLifecycleEvent struct {
	ID         int64  `json:"id"`
	EventType  string `json:"eventType"`
	ReasonCode string `json:"reasonCode,omitempty"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	TermSignal *int   `json:"termSignal,omitempty"`
	PID        int    `json:"pid,omitempty"`
	CDPPort    int    `json:"cdpPort,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

var validBrowserLifecycleReasonCodes = map[string]bool{
	"":                              true,
	"USER_STOP":                     true,
	"PROCESS_EXIT_CLEAN":            true,
	"PROCESS_EXIT_NONZERO":          true,
	"PROCESS_SIGNALED":              true,
	"PROCESS_MISSING_AFTER_RESTART": true,
	"SUPERVISOR_DETACHED":           true,
	"SUPERVISOR_SHUTDOWN":           true,
	"LEASE_LOST":                    true,
	"WAIT_RESULT_UNKNOWN":           true,
}

var validAccountAuthStates = map[string]bool{AccountAuthLoggedIn: true, AccountAuthLoggedOut: true, AccountAuthUnknown: true}
var validLoginModes = map[string]bool{LoginModeBrowserProfileSession: true, LoginModeManualBrowser: true, LoginModeUnknown: true}
var validAuthMethods = map[string]bool{AuthMethodQR: true, AuthMethodPhone: true, AuthMethodOAuth: true, AuthMethodUnknown: true}
var validAvatarSources = map[string]bool{AvatarSourceObserved: true, AvatarSourcePlatformFallback: true, AvatarSourceLegacyRemote: true}
var validAvatarErrorCodes = map[string]bool{"": true, AvatarErrorCaptureFailed: true, AvatarErrorPersistFailed: true}
var validLoginReasons = map[string]bool{
	LoginReasonVerifiedCookie: true, LoginReasonVerifiedProfileDOM: true, LoginReasonVerifiedSessionDOM: true,
	LoginReasonLoginRequiredPage: true, LoginReasonSessionExpiredPage: true,
	LoginReasonCheckpointRequired: true, LoginReasonProfileSelectorMissing: true,
	LoginReasonPlatformTabMissing: true, LoginReasonPlatformUnreachable: true, LoginReasonCDPUnreachable: true,
	LoginReasonBrowserProcessExited: true, LoginReasonManualVerifyRequired: true,
	LoginReasonBrowserOperationBusy: true,
}
var validLoginEvidence = map[string]bool{
	LoginEvidenceReviewedCookie: true, LoginEvidenceReviewedProfileDOM: true, LoginEvidenceReviewedSessionDOM: true,
	LoginEvidenceExplicitLoginDOM: true, LoginEvidenceExplicitExpiredDOM: true,
	LoginEvidenceCheckpointDOM: true, LoginEvidenceMissing: true,
	LoginEvidenceOperationLeaseBusy:   true,
	LoginEvidenceBrowserStoppedByUser: true, LoginEvidenceBrowserProcessExited: true,
	LoginEvidenceBrowserCrashed: true, LoginEvidenceBrowserHeartbeatFailed: true,
	LoginEvidenceBrowserLeaseLost: true,
}

// LatestBrowserExitEvidence returns only an allowlisted lifecycle code. Event
// types, rather than free-form messages, are the authority for exit causes.
func (db *DB) LatestBrowserExitEvidence(profileID, observedAfter string) (string, error) {
	if strings.TrimSpace(profileID) == "" {
		return "", fmt.Errorf("profile id is required")
	}
	if _, err := time.Parse(time.RFC3339, observedAfter); err != nil {
		return "", fmt.Errorf("observed after must be RFC3339")
	}
	var eventType string
	err := db.QueryRow(`SELECT event_type FROM browser_events
		WHERE profile_id=? AND created_at>=?
		  AND event_type IN ('stopped_by_user','process_exited','process_missing_after_restart','supervisor_shutdown','crashed','heartbeat_failed','heartbeat_recovered','lease_lost','lease_recovered')
		ORDER BY created_at DESC, id DESC LIMIT 1`, profileID, observedAfter).Scan(&eventType)
	if err == sql.ErrNoRows {
		return LoginEvidenceMissing, nil
	}
	if err != nil {
		return "", err
	}
	switch eventType {
	case "stopped_by_user":
		return LoginEvidenceBrowserStoppedByUser, nil
	case "process_exited":
		return LoginEvidenceBrowserProcessExited, nil
	case "process_missing_after_restart", "supervisor_shutdown":
		return LoginEvidenceBrowserProcessExited, nil
	case "crashed":
		return LoginEvidenceBrowserCrashed, nil
	case "heartbeat_failed":
		return LoginEvidenceBrowserHeartbeatFailed, nil
	case "lease_lost":
		return LoginEvidenceBrowserLeaseLost, nil
	case "heartbeat_recovered", "lease_recovered":
		return LoginEvidenceMissing, nil
	}
	return LoginEvidenceMissing, nil
}

// ListBrowserLifecycleEvents returns only allowlisted lifecycle metadata. It
// never returns the browser_events.message column.
func (db *DB) ListBrowserLifecycleEvents(profileID string, limit int) ([]BrowserLifecycleEvent, error) {
	profileID = strings.TrimSpace(profileID)
	if !validBrowserLifecycleProfileID(profileID) {
		return nil, fmt.Errorf("invalid browser profile id")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := db.Query(`SELECT id, event_type, COALESCE(reason_code,''), exit_code, term_signal,
		COALESCE(pid,0), COALESCE(cdp_port,0), created_at
		FROM browser_events
		WHERE profile_id=? AND event_type IN (
			'started','ready','stopped','stopped_by_user','process_exited','process_missing_after_restart','crashed',
			'supervisor_detached','supervisor_shutdown',
			'focus_failed','focus_recovered','focus_operation_failed','focus_succeeded',
			'heartbeat_failed','heartbeat_recovered','lease_lost','lease_recovered','degraded_unknown'
		)
		ORDER BY id DESC LIMIT ?`, profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]BrowserLifecycleEvent, 0)
	for rows.Next() {
		var event BrowserLifecycleEvent
		var exitCode, termSignal sql.NullInt64
		if err := rows.Scan(&event.ID, &event.EventType, &event.ReasonCode, &exitCode, &termSignal, &event.PID, &event.CDPPort, &event.CreatedAt); err != nil {
			return nil, err
		}
		if !validBrowserLifecycleReasonCodes[event.ReasonCode] {
			event.ReasonCode = ""
			exitCode.Valid = false
			termSignal.Valid = false
		}
		if exitCode.Valid {
			value := int(exitCode.Int64)
			event.ExitCode = &value
		}
		if termSignal.Valid {
			value := int(termSignal.Int64)
			event.TermSignal = &value
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func validBrowserLifecycleProfileID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// RecordAccountLoginObservation atomically records non-secret evidence and
// updates the durable account state. Unknown evidence only advances
// last_check_at; it can never demote a previously verified account.
func (db *DB) RecordAccountLoginObservation(input AccountLoginObservation) (*AccountLoginEvent, error) {
	if err := validateAccountLoginObservation(&input); err != nil {
		return nil, err
	}
	input.Name = trustedObservedProfileName(input.Name)
	if input.ObservedAt == "" {
		input.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if input.LoginMode == "" {
		input.LoginMode = LoginModeUnknown
	}
	if input.AuthMethod == "" {
		input.AuthMethod = AuthMethodUnknown
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	account, err := scanAccount(tx.QueryRow(`SELECT `+accountSelectCols+` FROM accounts WHERE id=?`, input.AccountID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("account %d not found", input.AccountID)
	}
	if err != nil {
		return nil, err
	}

	name := account.Name
	identifier := account.AccountIdentifier
	avatarRef := account.Avatar
	avatarSource := account.AvatarSource
	avatarSHA := account.AvatarSHA256
	avatarRefreshedAt := account.AvatarRefreshedAt
	loginMode := account.LoginMode
	authMethod := account.AuthMethod
	profileChanged := false
	identityChanged := false
	if avatarSource == "" {
		if avatarRef == "" {
			avatarSource = AvatarSourcePlatformFallback
		} else {
			avatarSource = AvatarSourceLegacyRemote
		}
	}

	if input.AuthState == AccountAuthLoggedIn {
		if input.Name != "" && input.Name != name {
			name = input.Name
			profileChanged = true
			identityChanged = true
		}
		if input.AccountIdentifier != "" && input.AccountIdentifier != identifier {
			identifier = input.AccountIdentifier
			profileChanged = true
			identityChanged = true
		}
		hasFreshObservedAvatar := input.AvatarSource == AvatarSourceObserved && input.AvatarRef != ""
		if identityChanged && !hasFreshObservedAvatar {
			avatarRef = ""
			avatarSource = AvatarSourcePlatformFallback
			avatarSHA = ""
			avatarRefreshedAt = ""
		}
		if hasFreshObservedAvatar && (input.AvatarRef != avatarRef || input.AvatarSHA256 != avatarSHA || avatarSource != AvatarSourceObserved) {
			avatarRef = input.AvatarRef
			avatarSource = AvatarSourceObserved
			avatarSHA = input.AvatarSHA256
			avatarRefreshedAt = input.ObservedAt
			profileChanged = true
		}
		if input.LoginMode != LoginModeUnknown {
			loginMode = input.LoginMode
		}
		// The way a historical session was authenticated cannot be inferred
		// from an already-logged-in page. Keep unknown unless reliable login
		// completion code explicitly supplies qr/phone/oauth.
		if input.AuthMethod != AuthMethodUnknown {
			authMethod = input.AuthMethod
		}
	}

	persistedState := account.LoginState
	switch input.AuthState {
	case AccountAuthLoggedIn:
		persistedState = AccountAuthLoggedIn
	case AccountAuthLoggedOut:
		persistedState = AccountAuthLoggedOut
	}
	_, err = tx.Exec(`UPDATE accounts SET
		name=?, avatar=?, avatar_source=?, avatar_sha256=?, avatar_refreshed_at=?,
		account_identifier=?, login_state=?,
		login_time=CASE WHEN ?='logged_in' AND (login_state<>'logged_in' OR COALESCE(login_time,'')='') THEN ? ELSE login_time END,
		last_check_at=?, login_mode=?, auth_method=?, updated_at=? WHERE id=?`,
		name, avatarRef, avatarSource, avatarSHA, avatarRefreshedAt,
		identifier, persistedState, input.AuthState, input.ObservedAt,
		input.ObservedAt, loginMode, authMethod, input.ObservedAt, input.AccountID)
	if err != nil {
		return nil, err
	}
	if input.AuthState == AccountAuthLoggedIn {
		if _, err := tx.Exec(`UPDATE account_assets SET name=?, external_id=?, avatar=?, last_seen_at=?, updated_at=? WHERE account_id=? AND asset_type='browser_account'`, name, identifier, avatarRef, input.ObservedAt, input.ObservedAt, input.AccountID); err != nil {
			return nil, err
		}
	}

	candidate := AccountLoginEvent{
		AccountID: input.AccountID, AuthState: input.AuthState, PersistedLoginState: persistedState,
		BrowserState: input.BrowserState, LoginMode: loginMode, AuthMethod: authMethod,
		ReasonCode: input.ReasonCode, EvidenceCode: input.EvidenceCode, PageOrigin: input.PageOrigin,
		Name: name, AccountIdentifier: identifier, AvatarSource: avatarSource, AvatarSHA256: avatarSHA,
		AvatarErrorCode: input.AvatarErrorCode, ProfileChanged: profileChanged,
		TraceID: input.TraceID, ObservedAt: input.ObservedAt, CreatedAt: input.ObservedAt,
	}
	latest, err := latestAccountLoginEventTx(tx, input.AccountID)
	if err != nil {
		return nil, err
	}
	if latest != nil && sameAccountLoginEvidence(latest, &candidate) {
		if _, err := tx.Exec(`UPDATE account_login_events SET trace_id=?, observed_at=? WHERE id=?`, candidate.TraceID, candidate.ObservedAt, latest.ID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		current := *latest
		current.ProfileChanged = false
		current.TraceID = candidate.TraceID
		current.ObservedAt = candidate.ObservedAt
		current.EvidenceChanged = false
		return &current, nil
	}

	res, err := tx.Exec(`INSERT INTO account_login_events (
		account_id, auth_state, persisted_login_state, browser_state, login_mode, auth_method,
		reason_code, evidence_code, page_origin, profile_name, account_identifier,
			avatar_source, avatar_sha256, avatar_error_code, profile_changed, trace_id, observed_at, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		candidate.AccountID, candidate.AuthState, candidate.PersistedLoginState, candidate.BrowserState,
		candidate.LoginMode, candidate.AuthMethod, candidate.ReasonCode, candidate.EvidenceCode,
		candidate.PageOrigin, candidate.Name, candidate.AccountIdentifier, candidate.AvatarSource,
		candidate.AvatarSHA256, candidate.AvatarErrorCode, boolInt(candidate.ProfileChanged), candidate.TraceID,
		candidate.ObservedAt, candidate.CreatedAt)
	if err != nil {
		return nil, err
	}
	candidate.ID, err = res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	candidate.EvidenceChanged = true
	return &candidate, nil
}

func trustedObservedProfileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(value), ""))
	switch normalized {
	case "暂无个人简介", "暂无简介", "暂无昵称", "未设置", "未命名", "unknown", "anonymous":
		return ""
	default:
		return value
	}
}

// UpdateAccountAvatarFallback stores only a caller-sanitized public reference.
// Managed observed snapshots still take precedence when available.
func (db *DB) UpdateAccountAvatarFallback(accountID int64, avatarRef, observedAt string) error {
	avatarRef = strings.TrimSpace(avatarRef)
	if accountID <= 0 || avatarRef == "" {
		return fmt.Errorf("account id and avatar reference are required")
	}
	if observedAt == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339)
	}
	result, err := db.Exec(`UPDATE accounts SET avatar=?, avatar_source=?, avatar_sha256='',
		avatar_refreshed_at=?, updated_at=? WHERE id=? AND avatar_source<>?`,
		avatarRef, AvatarSourcePlatformFallback, observedAt, observedAt, accountID, AvatarSourceObserved)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE id=?`, accountID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("account %d not found", accountID)
		}
	}
	return nil
}

func (db *DB) ListAccountLoginEvents(accountID int64, limit int) ([]AccountLoginEvent, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("account id must be positive")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := db.Query(`SELECT id, account_id, auth_state, persisted_login_state, browser_state, login_mode, auth_method, reason_code, evidence_code, page_origin, profile_name, account_identifier, avatar_source, avatar_sha256, avatar_error_code, profile_changed, trace_id, observed_at, created_at FROM account_login_events WHERE account_id=? ORDER BY observed_at DESC, id DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AccountLoginEvent, 0)
	for rows.Next() {
		item, err := scanAccountLoginEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (db *DB) LatestAccountLoginEvent(accountID int64) (*AccountLoginEvent, error) {
	return latestAccountLoginEventScanner(db.QueryRow(`SELECT id, account_id, auth_state, persisted_login_state, browser_state, login_mode, auth_method, reason_code, evidence_code, page_origin, profile_name, account_identifier, avatar_source, avatar_sha256, avatar_error_code, profile_changed, trace_id, observed_at, created_at FROM account_login_events WHERE account_id=? ORDER BY observed_at DESC, id DESC LIMIT 1`, accountID))
}

func (db *DB) LatestAccountLoginEventForState(accountID int64, authState string) (*AccountLoginEvent, error) {
	if accountID <= 0 || (authState != AccountAuthLoggedIn && authState != AccountAuthLoggedOut) {
		return nil, fmt.Errorf("invalid account auth state")
	}
	return latestAccountLoginEventScanner(db.QueryRow(`SELECT id, account_id, auth_state, persisted_login_state, browser_state, login_mode, auth_method, reason_code, evidence_code, page_origin, profile_name, account_identifier, avatar_source, avatar_sha256, avatar_error_code, profile_changed, trace_id, observed_at, created_at
		FROM account_login_events WHERE account_id=? AND auth_state=? ORDER BY observed_at DESC, id DESC LIMIT 1`, accountID, authState))
}

func latestAccountLoginEventTx(tx *sql.Tx, accountID int64) (*AccountLoginEvent, error) {
	return latestAccountLoginEventScanner(tx.QueryRow(`SELECT id, account_id, auth_state, persisted_login_state, browser_state, login_mode, auth_method, reason_code, evidence_code, page_origin, profile_name, account_identifier, avatar_source, avatar_sha256, avatar_error_code, profile_changed, trace_id, observed_at, created_at FROM account_login_events WHERE account_id=? ORDER BY observed_at DESC, id DESC LIMIT 1`, accountID))
}

func latestAccountLoginEventScanner(row *sql.Row) (*AccountLoginEvent, error) {
	item, err := scanAccountLoginEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

func scanAccountLoginEvent(scanner interface{ Scan(...any) error }) (*AccountLoginEvent, error) {
	var item AccountLoginEvent
	var profileChanged int
	err := scanner.Scan(&item.ID, &item.AccountID, &item.AuthState, &item.PersistedLoginState,
		&item.BrowserState, &item.LoginMode, &item.AuthMethod, &item.ReasonCode, &item.EvidenceCode,
		&item.PageOrigin, &item.Name, &item.AccountIdentifier, &item.AvatarSource, &item.AvatarSHA256,
		&item.AvatarErrorCode, &profileChanged, &item.TraceID, &item.ObservedAt, &item.CreatedAt)
	item.ProfileChanged = profileChanged == 1
	return &item, err
}

func sameAccountLoginEvidence(a, b *AccountLoginEvent) bool {
	return a.AuthState == b.AuthState && a.PersistedLoginState == b.PersistedLoginState &&
		a.BrowserState == b.BrowserState && a.LoginMode == b.LoginMode && a.AuthMethod == b.AuthMethod &&
		a.ReasonCode == b.ReasonCode && a.EvidenceCode == b.EvidenceCode && a.PageOrigin == b.PageOrigin &&
		a.Name == b.Name && a.AccountIdentifier == b.AccountIdentifier && a.AvatarSource == b.AvatarSource &&
		a.AvatarSHA256 == b.AvatarSHA256 && a.AvatarErrorCode == b.AvatarErrorCode
}

func validateAccountLoginObservation(input *AccountLoginObservation) error {
	if input == nil || input.AccountID <= 0 {
		return fmt.Errorf("account id must be positive")
	}
	if !validAccountAuthStates[input.AuthState] {
		return fmt.Errorf("invalid account auth state %q", input.AuthState)
	}
	if input.LoginMode == "" {
		input.LoginMode = LoginModeUnknown
	}
	if input.AuthMethod == "" {
		input.AuthMethod = AuthMethodUnknown
	}
	if !validLoginModes[input.LoginMode] || !validAuthMethods[input.AuthMethod] {
		return fmt.Errorf("invalid login mode or auth method")
	}
	if !validLoginReasons[input.ReasonCode] || !validLoginEvidence[input.EvidenceCode] {
		return fmt.Errorf("invalid login reason or evidence code")
	}
	if !validAvatarErrorCodes[input.AvatarErrorCode] {
		return fmt.Errorf("invalid avatar error code %q", input.AvatarErrorCode)
	}
	if input.AuthState == AccountAuthLoggedOut {
		validExplicit := (input.ReasonCode == LoginReasonLoginRequiredPage && input.EvidenceCode == LoginEvidenceExplicitLoginDOM) ||
			(input.ReasonCode == LoginReasonSessionExpiredPage && input.EvidenceCode == LoginEvidenceExplicitExpiredDOM)
		if !validExplicit {
			return fmt.Errorf("logout requires explicit login or expiry evidence")
		}
	}
	input.Name = boundedObservationText(input.Name, 80)
	input.AccountIdentifier = boundedObservationText(input.AccountIdentifier, 120)
	input.BrowserState = boundedObservationText(input.BrowserState, 32)
	input.TraceID = boundedObservationText(input.TraceID, 128)
	if input.BrowserState == "" || input.TraceID == "" {
		return fmt.Errorf("browser state and trace id are required")
	}
	if input.PageOrigin != "" {
		parsed, err := url.Parse(input.PageOrigin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(input.PageOrigin) > 1024 {
			return fmt.Errorf("page origin must be a sanitized http(s) host/path")
		}
	}
	if input.AvatarSource == "" {
		input.AvatarSource = AvatarSourcePlatformFallback
	}
	if !validAvatarSources[input.AvatarSource] {
		return fmt.Errorf("invalid avatar source %q", input.AvatarSource)
	}
	if input.AvatarSource == AvatarSourceObserved {
		clean := path.Clean(strings.TrimSpace(input.AvatarRef))
		if clean == "." || strings.HasPrefix(clean, "../") || !strings.HasPrefix(clean, "account-avatars/") || clean != input.AvatarRef {
			return fmt.Errorf("observed avatar ref must be a managed relative path")
		}
		decoded, err := hex.DecodeString(input.AvatarSHA256)
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("observed avatar requires SHA-256")
		}
	}
	if input.ObservedAt != "" {
		if _, err := time.Parse(time.RFC3339, input.ObservedAt); err != nil {
			return fmt.Errorf("observed_at must be RFC3339")
		}
	}
	return nil
}

func boundedObservationText(value string, max int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > max {
		return ""
	}
	return value
}

// LatestAccountLoginEvents returns the newest observation per account in one
// pass. The account list used to call LatestAccountLoginEvent once per row,
// which on a 27-account page meant 27 extra queries every poll.
func (db *DB) LatestAccountLoginEvents() (map[int64]*AccountLoginEvent, error) {
	rows, err := db.Query(`SELECT id, account_id, auth_state, persisted_login_state, browser_state, login_mode,
		auth_method, reason_code, evidence_code, page_origin, profile_name, account_identifier, avatar_source,
		avatar_sha256, avatar_error_code, profile_changed, trace_id, observed_at, created_at
		FROM account_login_events e
		WHERE NOT EXISTS (
			SELECT 1 FROM account_login_events newer
			WHERE newer.account_id=e.account_id
			  AND (newer.observed_at>e.observed_at OR (newer.observed_at=e.observed_at AND newer.id>e.id))
		)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	latest := make(map[int64]*AccountLoginEvent, 32)
	for rows.Next() {
		item, err := scanAccountLoginEvent(rows)
		if err != nil {
			return nil, err
		}
		latest[item.AccountID] = item
	}
	return latest, rows.Err()
}
