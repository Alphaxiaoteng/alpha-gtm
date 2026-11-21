//go:build !darwin

package llm

import "errors"

func loadCustomKeychainKeyImpl() string {
	return ""
}

func saveCustomKeychainKeyImpl(string) error {
	return errors.New("custom LLM secure storage is unavailable")
}
