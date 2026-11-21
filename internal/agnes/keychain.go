package agnes

import (
	"errors"
	"strings"
	"unicode"
)

const maxAPIKeyBytes = 4096

var (
	loadKeychainKey       = loadKeychainKeyImpl
	saveKeychainKey       = saveKeychainKeyImpl
	keychainKeyConfigured = keychainKeyConfiguredImpl
)

// APIKeyConfigured checks secure-store metadata without returning the secret.
func APIKeyConfigured() (bool, error) {
	configured, err := keychainKeyConfigured()
	if err != nil {
		return false, preserveOrMapError(err, CodeKeychainReadFailed)
	}
	return configured, nil
}

// SaveAPIKey stores the key only in platform secure storage.
func SaveAPIKey(apiKey string) error {
	key, err := normalizeKey(apiKey)
	if err != nil {
		return err
	}
	if err := saveKeychainKey(key); err != nil {
		return preserveOrMapError(err, CodeKeychainWriteFailed)
	}
	return nil
}

// LoadAPIKey reads the key only from platform secure storage.
func LoadAPIKey() (string, error) {
	key, err := loadKeychainKey()
	if err != nil {
		return "", preserveOrMapError(err, CodeKeychainReadFailed)
	}
	key, err = normalizeKey(key)
	if err != nil {
		if strings.TrimSpace(key) == "" {
			return "", newError(CodeKeyMissing, 0, false, nil)
		}
		return "", newError(CodeInvalidKey, 0, false, nil)
	}
	return key, nil
}

func normalizeKey(apiKey string) (string, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" || len(key) > maxAPIKeyBytes {
		return "", newError(CodeInvalidKey, 0, false, nil)
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return "", newError(CodeInvalidKey, 0, false, nil)
		}
	}
	return key, nil
}

func preserveOrMapError(err error, fallback ErrorCode) error {
	var coded *Error
	if errors.As(err, &coded) {
		return err
	}
	return newError(fallback, 0, false, err)
}

func keychainInput(key string) string {
	// macOS security reads the new value and confirmation from stdin.
	return key + "\n" + key + "\n"
}
