//go:build !darwin

package feedback

func keychainConfigConfigured() (bool, error) {
	return false, ErrKeychainUnavailable
}

func loadKeychainConfig() (string, error) {
	return "", ErrKeychainUnavailable
}

func saveKeychainConfig(string) error {
	return ErrKeychainUnavailable
}
