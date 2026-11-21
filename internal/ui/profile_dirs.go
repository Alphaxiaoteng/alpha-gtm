package ui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// createFreshProfileDir never adopts an existing directory. A leftover profile
// may contain a valid session, so a new account must receive a newly-created,
// empty path even if its deterministic base name already exists.
func createFreshProfileDir(dataDir string, accountID int64) (string, error) {
	root := filepath.Join(dataDir, "profiles")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", err
	}

	base := filepath.Join(root, hashProfileID(accountID))
	for attempt := 0; attempt < 32; attempt++ {
		candidate := base
		if attempt > 0 {
			suffix := make([]byte, 4)
			if _, err := rand.Read(suffix); err != nil {
				return "", err
			}
			candidate += "-" + hex.EncodeToString(suffix)
		}
		if err := os.Mkdir(candidate, 0o700); err == nil {
			return candidate, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate a fresh browser profile directory")
}

func removeStoredProfileDir(dataDir, profileDir string) error {
	root, err := filepath.Abs(filepath.Join(dataDir, "profiles"))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(profileDir)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return fmt.Errorf("profile path is outside the managed profile root")
	}
	if len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("profile path is outside the managed profile root")
	}
	return os.RemoveAll(target)
}
