// Package errors defines stable machine-readable error codes for CLI, HTTP API,
// batch items, and NDJSON. Consumers (browser / cli / ui) must reference these
// constants — do not invent parallel string literals.
package errors

// ErrorCode is a stable error code string shared across CLI NDJSON and HTTP API.
type ErrorCode string

// Canonical multi-browser / account error codes (T-E02).
//
//	PROFILE_LOCKED      profile flock or lease held by another holder
//	NO_PORT             no free CDP port (short form; batch item style)
//	NO_PORT_AVAILABLE   no free CDP port in pool 15101–15600 (verbose form)
//	BROWSER_CRASH       browser process died / Reconcile marked crashed
//	CDP_CONNECT         CDP connect or readiness failed
//	LEASE_EXPIRED       lease TTL elapsed; holder must re-acquire
//	BROWSER_NOT_FOUND   fingerprint-chromium binary path missing or invalid
const (
	// --- multi-browser core (required by multi-browser framework) ---
	PROFILE_LOCKED    ErrorCode = "PROFILE_LOCKED"
	NO_PORT           ErrorCode = "NO_PORT"
	NO_PORT_AVAILABLE ErrorCode = "NO_PORT_AVAILABLE"
	BROWSER_CRASH     ErrorCode = "BROWSER_CRASH"
	CDP_CONNECT       ErrorCode = "CDP_CONNECT"
	LEASE_EXPIRED     ErrorCode = "LEASE_EXPIRED"
	BROWSER_NOT_FOUND ErrorCode = "BROWSER_NOT_FOUND"

	// --- platform / data ---
	PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1 ErrorCode = "PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1"
	API_CONNECTOR_UNAVAILABLE            ErrorCode = "API_CONNECTOR_UNAVAILABLE"
	LOGIN_REQUIRED                       ErrorCode = "LOGIN_REQUIRED"
	INVALID_DATA_DIR                     ErrorCode = "INVALID_DATA_DIR"
	DB_MIGRATION_FAILED                  ErrorCode = "DB_MIGRATION_FAILED"
	PLATFORM_NOT_FOUND                   ErrorCode = "PLATFORM_NOT_FOUND"
	PROFILE_NOT_FOUND                    ErrorCode = "PROFILE_NOT_FOUND"

	// --- lease / concurrency variants ---
	// PROFILE_BUSY: lease held (same family as PROFILE_LOCKED; use PROFILE_LOCKED for user-facing).
	PROFILE_BUSY ErrorCode = "PROFILE_BUSY"
	BUSY_BY_JOB  ErrorCode = "BUSY_BY_JOB"
	BUSY_BY_UI   ErrorCode = "BUSY_BY_UI_SESSION"
	// BROWSER_OPERATION_BUSY fences start/stop/publish/collect actions that
	// target the same account profile across UI, Registry, and Fleet processes.
	BROWSER_OPERATION_BUSY ErrorCode = "BROWSER_OPERATION_BUSY"
	// BROWSER_OPERATION_LEASE_FAILED is an internal persistence failure, not a
	// contention signal; callers must fail closed instead of touching Facade.
	BROWSER_OPERATION_LEASE_FAILED ErrorCode = "BROWSER_OPERATION_LEASE_FAILED"

	// --- CDP / process variants ---
	CDP_NOT_READY   ErrorCode = "CDP_NOT_READY"
	ALREADY_RUNNING ErrorCode = "ALREADY_RUNNING"

	// PROCESS_CRASHED is a deprecated alias of BROWSER_CRASH (kept for old callers).
	PROCESS_CRASHED ErrorCode = "BROWSER_CRASH"

	// --- Facade validation / concurrency (T-A01..T-A05) ---
	ACCOUNT_NOT_FOUND           ErrorCode = "ACCOUNT_NOT_FOUND"
	ACCOUNT_DISABLED            ErrorCode = "ACCOUNT_DISABLED"
	ACCOUNT_BROWSER_NOT_RUNNING ErrorCode = "ACCOUNT_BROWSER_NOT_RUNNING"
	ACCOUNT_STATE_UPDATE_FAILED ErrorCode = "ACCOUNT_STATE_UPDATE_FAILED"
	INVALID_ACCOUNT_ID          ErrorCode = "INVALID_ACCOUNT_ID"
	LOGIN_CHECK_FAILED          ErrorCode = "LOGIN_CHECK_FAILED"
	MAX_CONCURRENT_BROWSERS     ErrorCode = "MAX_CONCURRENT_BROWSERS"
	FOCUS_FAILED                ErrorCode = "FOCUS_FAILED"
	MANUAL_TAKEOVER             ErrorCode = "MANUAL_TAKEOVER"
)

// AllMultiBrowserCodes is the frozen set for multi-browser framework acceptance.
var AllMultiBrowserCodes = []ErrorCode{
	PROFILE_LOCKED,
	NO_PORT,
	NO_PORT_AVAILABLE,
	BROWSER_CRASH,
	CDP_CONNECT,
	LEASE_EXPIRED,
	BROWSER_NOT_FOUND,
}

func (e ErrorCode) String() string {
	return string(e)
}

// Is reports whether errMsg contains the given code as a prefix or exact token.
// Useful when code is wrapped as "CODE: detail".
func Is(errMsg string, code ErrorCode) bool {
	if errMsg == "" || code == "" {
		return false
	}
	s := string(code)
	if errMsg == s {
		return true
	}
	// "CODE: ..." or "CODE ..."
	if len(errMsg) > len(s) && errMsg[:len(s)] == s {
		c := errMsg[len(s)]
		return c == ':' || c == ' '
	}
	return false
}
