package notify

import "strings"

type SecretStore interface {
	Configured(kind string) (bool, error)
	Load(kind string) (string, error)
	Save(kind, value string) error
}

type keychainStore struct{}

func NewKeychainStore() SecretStore { return keychainStore{} }

func (keychainStore) Configured(kind string) (bool, error) { return keychainConfigured(kind) }
func (keychainStore) Load(kind string) (string, error)     { return keychainLoad(kind) }
func (keychainStore) Save(kind, value string) error        { return keychainSave(kind, value) }

func validKind(kind string) bool {
	switch kind {
	case "feishu", "dingtalk", "webhook", "email":
		return true
	default:
		return false
	}
}

func normalizeSecret(kind, value string) (string, error) {
	value = strings.TrimSpace(value)
	if !validKind(kind) || value == "" || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
		return "", newError(CodeInvalidConfig)
	}
	return value, nil
}

func keychainInput(value string) string { return value + "\n" + value + "\n" }
