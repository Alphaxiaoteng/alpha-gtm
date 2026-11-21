//go:build !darwin

package notify

func keychainConfigured(string) (bool, error) { return false, newError(CodeKeychainUnavailable) }
func keychainLoad(string) (string, error)     { return "", newError(CodeKeychainUnavailable) }
func keychainSave(string, string) error       { return newError(CodeKeychainUnavailable) }
