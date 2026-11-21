package llm

import (
	"errors"
	"strings"
)

var loadCustomKeychainKey = loadCustomKeychainKeyImpl
var saveCustomKeychainKey = saveCustomKeychainKeyImpl

func ResolveCustomAPIKey() string {
	return strings.TrimSpace(loadCustomKeychainKey())
}

func SaveCustomAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("自定义 LLM API 密钥不能为空")
	}
	return saveCustomKeychainKey(key)
}
