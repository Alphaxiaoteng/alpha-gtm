//go:build !darwin

package agnes

func keychainKeyConfiguredImpl() (bool, error) {
	return false, newError(CodeKeychainUnavailable, 0, false, nil)
}

func loadKeychainKeyImpl() (string, error) {
	return "", newError(CodeKeychainUnavailable, 0, false, nil)
}

func saveKeychainKeyImpl(string) error {
	return newError(CodeKeychainUnavailable, 0, false, nil)
}
