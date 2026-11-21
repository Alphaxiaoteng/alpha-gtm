//go:build !darwin

package agnes

import "testing"

func TestNonDarwinKeychainFailsClosed(t *testing.T) {
	if configured, err := keychainKeyConfiguredImpl(); configured || CodeOf(err) != CodeKeychainUnavailable {
		t.Fatalf("configured=%v code=%s error=%v", configured, CodeOf(err), err)
	}
	if _, err := loadKeychainKeyImpl(); CodeOf(err) != CodeKeychainUnavailable {
		t.Fatalf("load code=%s error=%v", CodeOf(err), err)
	}
	if err := saveKeychainKeyImpl("synthetic-agnes-key"); CodeOf(err) != CodeKeychainUnavailable {
		t.Fatalf("save code=%s error=%v", CodeOf(err), err)
	}
}
