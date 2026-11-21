package feedback

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
)

var (
	ErrInvalidConfig       = errors.New("feedback configuration is invalid")
	ErrNotConfigured       = errors.New("feedback is not configured")
	ErrKeychainUnavailable = errors.New("feedback keychain is unavailable")
	ErrKeychainReadFailed  = errors.New("feedback keychain read failed")
	ErrKeychainWriteFailed = errors.New("feedback keychain write failed")
)

type Config struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
	WikiToken string `json:"wiki_token,omitempty"`
	BaseToken string `json:"base_token,omitempty"`
	TableID   string `json:"table_id"`
}

type ConfigStore interface {
	Load() (Config, error)
	Save(Config) error
	Configured() (bool, error)
}

type keychainStore struct{}

func NewKeychainStore() ConfigStore { return keychainStore{} }

func (config Config) Validate() error {
	_, err := normalizeConfig(config)
	return err
}

func (keychainStore) Load() (Config, error) {
	raw, err := loadKeychainConfig()
	if err != nil {
		return Config{}, err
	}
	config, err := decodeConfig(raw)
	if err != nil {
		return Config{}, ErrKeychainReadFailed
	}
	return config, nil
}

func (keychainStore) Save(config Config) error {
	raw, err := encodeConfig(config)
	if err != nil {
		return err
	}
	return saveKeychainConfig(raw)
}

func (keychainStore) Configured() (bool, error) {
	return keychainConfigConfigured()
}

func encodeConfig(config Config) (string, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", ErrInvalidConfig
	}
	return string(data), nil
}

func decodeConfig(raw string) (Config, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, ErrInvalidConfig
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, ErrInvalidConfig
	}
	return normalizeConfig(config)
}

func normalizeConfig(config Config) (Config, error) {
	config.AppID = strings.TrimSpace(config.AppID)
	config.AppSecret = strings.TrimSpace(config.AppSecret)
	config.WikiToken = strings.TrimSpace(config.WikiToken)
	config.BaseToken = strings.TrimSpace(config.BaseToken)
	config.TableID = strings.TrimSpace(config.TableID)

	if !validConfigValue(config.AppID, 256) ||
		!validConfigValue(config.AppSecret, 4096) ||
		!validConfigValue(config.TableID, 256) ||
		!strings.HasPrefix(config.AppID, "cli_") ||
		!strings.HasPrefix(config.TableID, "tbl") {
		return Config{}, ErrInvalidConfig
	}
	hasWiki := config.WikiToken != ""
	hasBase := config.BaseToken != ""
	if hasWiki == hasBase {
		return Config{}, ErrInvalidConfig
	}
	if hasWiki && !validConfigValue(config.WikiToken, 1024) {
		return Config{}, ErrInvalidConfig
	}
	if hasBase && !validConfigValue(config.BaseToken, 1024) {
		return Config{}, ErrInvalidConfig
	}
	return config, nil
}

func validConfigValue(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func keychainInput(value string) string {
	// `security -w` reads the new value and its confirmation from stdin.
	return value + "\n" + value + "\n"
}
