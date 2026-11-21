package radar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

// CreatorArchiveProgress reports the coarse state of one creator archive run.
// The file lives inside the creator folder so a restarted UI can resume display.
type CreatorArchiveProgress struct {
	Phase              string `json:"phase"`
	PostsFetched       int    `json:"posts_fetched"`
	PostsTotalEstimate int    `json:"posts_total_estimate"`
	CoversDone         int    `json:"covers_done"`
	CoversTotal        int    `json:"covers_total"`
	AudioDone          int    `json:"audio_done"`
	AudioTotal         int    `json:"audio_total"`
	Error              string `json:"error,omitempty"`
	StartedAt          string `json:"started_at"`
	UpdatedAt          string `json:"updated_at"`
}

func creatorArchiveProgressPath(dataDir, platform, userID string) string {
	return filepath.Join(tikhub.CreatorFolder(dataDir, platform, userID), "archive-progress.json")
}

func LoadCreatorArchiveProgress(dataDir, platform, userID string) (CreatorArchiveProgress, bool) {
	raw, err := os.ReadFile(creatorArchiveProgressPath(dataDir, platform, userID))
	if err != nil {
		return CreatorArchiveProgress{}, false
	}
	var progress CreatorArchiveProgress
	if err := json.Unmarshal(raw, &progress); err != nil {
		return CreatorArchiveProgress{}, false
	}
	return progress, true
}

func SaveCreatorArchiveProgress(dataDir, platform, userID string, progress CreatorArchiveProgress) error {
	progress.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return err
	}
	root := tikhub.CreatorFolder(dataDir, platform, userID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	target := creatorArchiveProgressPath(dataDir, platform, userID)
	temp, err := os.CreateTemp(root, ".archive-progress-*.tmp")
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
	return os.Rename(tempPath, target)
}

func ClearCreatorArchiveProgress(dataDir, platform, userID string) {
	_ = os.Remove(creatorArchiveProgressPath(dataDir, platform, userID))
}
