package egolite

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
)

// persistObservedAvatarPNG stores only a bounded PNG crop produced by the
// already-bound Ego Space. It never fetches a remote avatar URL or reads
// browser profile data.
func persistObservedAvatarPNG(dataDir string, accountID int64, encoded string) (string, string, error) {
	return persistObservedAvatarPNGAt(dataDir, "account-avatars", "account", accountID, encoded)
}

func persistObservedAssetAvatarPNG(dataDir string, assetID int64, encoded string) (string, string, error) {
	return persistObservedAvatarPNGAt(dataDir, "account-asset-avatars", "asset", assetID, encoded)
}

func persistObservedAvatarPNGAt(dataDir, directoryName, filePrefix string, entityID int64, encoded string) (string, string, error) {
	if entityID <= 0 || dataDir == "" || encoded == "" {
		return "", "", fmt.Errorf("avatar input is incomplete")
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 || len(payload) > 2<<20 {
		return "", "", fmt.Errorf("avatar payload is invalid")
	}
	config, err := png.DecodeConfig(bytes.NewReader(payload))
	if err != nil || config.Width < 16 || config.Height < 16 || config.Width > 2048 || config.Height > 2048 {
		return "", "", fmt.Errorf("avatar PNG dimensions are invalid")
	}
	ratio := float64(config.Width) / float64(config.Height)
	if ratio < 0.65 || ratio > 1.55 {
		return "", "", fmt.Errorf("avatar PNG aspect ratio is invalid")
	}

	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve avatar root: %w", err)
	}
	directory := filepath.Join(root, directoryName)
	if info, statErr := os.Lstat(directory); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", "", fmt.Errorf("avatar directory is unsafe")
		}
	} else if os.IsNotExist(statErr) {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return "", "", fmt.Errorf("create avatar directory: %w", err)
		}
	} else {
		return "", "", fmt.Errorf("inspect avatar directory: %w", statErr)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", "", fmt.Errorf("restrict avatar directory: %w", err)
	}

	digest := sha256.Sum256(payload)
	sha := hex.EncodeToString(digest[:])
	name := fmt.Sprintf("%s-%d-%s.png", filePrefix, entityID, sha[:16])
	destination := filepath.Join(directory, name)
	if info, statErr := os.Lstat(destination); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("avatar target is unsafe")
		}
		return filepath.ToSlash(filepath.Join(directoryName, name)), sha, nil
	} else if !os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("inspect avatar target: %w", statErr)
	}

	temporary, err := os.CreateTemp(directory, ".avatar-*.tmp")
	if err != nil {
		return "", "", fmt.Errorf("create avatar temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", "", err
	}
	return filepath.ToSlash(filepath.Join(directoryName, name)), sha, nil
}
