//go:build !darwin

package llm

import "errors"

func loadDoubaoKeychainKeyImpl() string {
	return ""
}

func saveDoubaoKeychainKeyImpl(string) error {
	return errors.New("当前系统不支持 macOS 钥匙串")
}
