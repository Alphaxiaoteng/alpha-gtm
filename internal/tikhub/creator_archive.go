package tikhub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type creatorFolderMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	Platform      string `json:"platform"`
	UserID        string `json:"user_id"`
	Name          string `json:"name,omitempty"`
	Note          string `json:"note,omitempty"`
	LookbackDays  int    `json:"lookback_days"`
	PageSize      int    `json:"page_size"`
	MaxPages      int    `json:"max_pages"`
	SortType      int    `json:"sort_type"`
	UpdatedAt     string `json:"updated_at"`
}

func CreatorFolder(dataDir, platform, userID string) string {
	sum := sha256.Sum256([]byte(platform + "\x00" + userID))
	prefix := safeCreatorSegment(userID)
	if len(prefix) > 42 {
		prefix = prefix[:42]
	}
	return filepath.Join(dataDir, "trend-radar", "creators", platform, prefix+"__"+hex.EncodeToString(sum[:6]))
}

// EnsureCreatorFolder creates the stable per-creator folder and updates only
// its small configuration index. Raw API snapshots remain append-only.
func EnsureCreatorFolder(dataDir string, user WatchUser) (string, error) {
	applyWatchDefaults(&user)
	root := CreatorFolder(dataDir, user.Platform, user.UserID)
	snapshots := filepath.Join(root, "snapshots")
	if err := os.MkdirAll(snapshots, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(snapshots, 0o700); err != nil {
		return "", err
	}
	metadata := creatorFolderMetadata{
		SchemaVersion: 1, Platform: user.Platform, UserID: user.UserID,
		Name: user.Name, Note: user.Note, LookbackDays: user.LookbackDays,
		PageSize: user.PageSize, MaxPages: user.MaxPages, SortType: user.SortType,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeOwnerOnlyAtomic(root, "creator.json", raw); err != nil {
		return "", err
	}
	return root, nil
}

// ArchiveCreatorSnapshot adds a hard link beneath the monitored creator. The
// canonical call snapshot stays under tikhub/calls and is never moved.
func ArchiveCreatorSnapshot(dataDir, rawPath string, snapshot CallSnapshot) (string, error) {
	userID := creatorIDFromParameters(snapshot.Parameters)
	if userID == "" || (snapshot.Capability != "creator_profile" && snapshot.Capability != "creator_posts") {
		return "", nil
	}
	root := CreatorFolder(dataDir, snapshot.Platform, userID)
	snapshots := filepath.Join(root, "snapshots")
	if err := os.MkdirAll(snapshots, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(snapshots, 0o700); err != nil {
		return "", err
	}
	name := strconv.FormatInt(snapshot.CallID, 10) + "_" + snapshot.Capability + ".json"
	target := filepath.Join(snapshots, name)
	if err := os.Link(rawPath, target); err != nil {
		return "", err
	}
	dir, err := os.Open(snapshots)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return "", err
	}
	return root, nil
}

func creatorIDFromParameters(parameters map[string]any) string {
	for _, name := range []string{"sec_user_id", "user_id", "username"} {
		value := strings.TrimSpace(fmt.Sprint(parameters[name]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func safeCreatorSegment(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "creator"
	}
	return builder.String()
}

func writeOwnerOnlyAtomic(dir, name string, raw []byte) error {
	temp, err := os.CreateTemp(dir, "."+name+"-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, filepath.Join(dir, name))
}
