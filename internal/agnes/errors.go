package agnes

import "errors"

// ErrorCode is safe to persist and return to callers. Provider response bodies
// and credential material are deliberately excluded from errors.
type ErrorCode string

const (
	CodeInvalidInput             ErrorCode = "AGNES_INVALID_INPUT"
	CodeInvalidKey               ErrorCode = "AGNES_INVALID_KEY"
	CodeUnsupportedModel         ErrorCode = "AGNES_UNSUPPORTED_MODEL"
	CodeKeyMissing               ErrorCode = "AGNES_KEY_MISSING"
	CodeKeychainUnavailable      ErrorCode = "AGNES_KEYCHAIN_UNAVAILABLE"
	CodeKeychainReadFailed       ErrorCode = "AGNES_KEYCHAIN_READ_FAILED"
	CodeKeychainWriteFailed      ErrorCode = "AGNES_KEYCHAIN_WRITE_FAILED"
	CodeRequestFailed            ErrorCode = "AGNES_REQUEST_FAILED"
	CodeProviderRejected         ErrorCode = "AGNES_PROVIDER_REJECTED"
	CodeAuthFailed               ErrorCode = "AGNES_AUTH_FAILED"
	CodeQuotaExhausted           ErrorCode = "AGNES_QUOTA_EXHAUSTED"
	CodeRateLimited              ErrorCode = "AGNES_RATE_LIMITED"
	CodeProviderUnavailable      ErrorCode = "AGNES_PROVIDER_UNAVAILABLE"
	CodeResponseTooLarge         ErrorCode = "AGNES_RESPONSE_TOO_LARGE"
	CodeInvalidResponse          ErrorCode = "AGNES_INVALID_RESPONSE"
	CodeInvalidImageArtifact     ErrorCode = "AGNES_INVALID_IMAGE_ARTIFACT"
	CodeVideoFailed              ErrorCode = "AGNES_VIDEO_FAILED"
	CodeDownloadRejected         ErrorCode = "AGNES_VIDEO_DOWNLOAD_REJECTED"
	CodeDownloadRedirectRejected ErrorCode = "AGNES_VIDEO_REDIRECT_REJECTED"
	CodeDownloadFailed           ErrorCode = "AGNES_VIDEO_DOWNLOAD_FAILED"
	CodeInternal                 ErrorCode = "AGNES_INTERNAL_ERROR"
)

// Error carries only stable, non-sensitive metadata. Error() intentionally
// returns the code alone so upstream logging cannot leak provider bodies.
type Error struct {
	Code       ErrorCode
	HTTPStatus int
	Retryable  bool
	cause      error
}

func (e *Error) Error() string {
	if e == nil || e.Code == "" {
		return string(CodeInternal)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && e.Code == other.Code
}

func newError(code ErrorCode, status int, retryable bool, cause error) error {
	return &Error{Code: code, HTTPStatus: status, Retryable: retryable, cause: cause}
}

// CodeOf extracts a stable code and never derives one from raw error text.
func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var coded *Error
	if errors.As(err, &coded) && coded.Code != "" {
		return coded.Code
	}
	return CodeInternal
}
