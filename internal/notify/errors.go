package notify

import "errors"

type ErrorCode string

const (
	CodeInvalidConfig       ErrorCode = "NOTIFY_INVALID_CONFIG"
	CodeEndpointRejected    ErrorCode = "NOTIFY_ENDPOINT_REJECTED"
	CodeNotConfigured       ErrorCode = "NOTIFY_NOT_CONFIGURED"
	CodeKeychainUnavailable ErrorCode = "NOTIFY_KEYCHAIN_UNAVAILABLE"
	CodeKeychainReadFailed  ErrorCode = "NOTIFY_KEYCHAIN_READ_FAILED"
	CodeKeychainWriteFailed ErrorCode = "NOTIFY_KEYCHAIN_WRITE_FAILED"
	CodeRequestFailed       ErrorCode = "NOTIFY_REQUEST_FAILED"
	CodeProviderRejected    ErrorCode = "NOTIFY_PROVIDER_REJECTED"
	CodeSMTPFailed          ErrorCode = "NOTIFY_SMTP_FAILED"
	CodeQueueFull           ErrorCode = "NOTIFY_QUEUE_FULL"
	CodeInternal            ErrorCode = "NOTIFY_INTERNAL"
)

type codedError struct{ code ErrorCode }

func (err codedError) Error() string { return string(err.code) }

func newError(code ErrorCode) error { return codedError{code: code} }

func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var coded codedError
	if errors.As(err, &coded) {
		return coded.code
	}
	return CodeInternal
}
