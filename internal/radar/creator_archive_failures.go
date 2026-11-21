package radar

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

// CreatorArchiveFailuresSchemaVersion is the on-disk ledger schema version. It
// only changes when existing fields change meaning.
const CreatorArchiveFailuresSchemaVersion = 1

// CreatorArchiveFailureLedger is the on-disk record of the assets that failed to
// download during the last media pass. It is kept beside index.json instead of
// inside it so the manifest stays a pure asset index.
type CreatorArchiveFailureLedger struct {
	SchemaVersion int                     `json:"schema_version"`
	Platform      string                  `json:"platform"`
	UserID        string                  `json:"user_id"`
	UpdatedAt     string                  `json:"updated_at"`
	Failures      []CreatorArchiveFailure `json:"failures"`
}

// CreatorArchiveFailure names one asset that could not be archived.
type CreatorArchiveFailure struct {
	AwemeID     string `json:"aweme_id"`
	Kind        string `json:"kind"`
	Reason      string `json:"reason"`
	AttemptedAt string `json:"attempted_at"`
	// Title and CoverPath borrow the post identity from the same archive run so
	// the UI can show which post failed without the user resolving the id. They
	// stay empty when the failure belongs to a post outside this run's list.
	Title     string `json:"title,omitempty"`
	CoverPath string `json:"cover_path,omitempty"`
}

func creatorArchiveFailuresPath(dataDir, platform, userID string) string {
	return filepath.Join(tikhub.CreatorFolder(dataDir, platform, userID), "archive-failures.json")
}

// LoadCreatorArchiveFailures reads the failure ledger for one creator. The bool
// reports whether a usable ledger exists; a missing or corrupt file is a normal
// "no failures recorded" state, never an error, so the UI can render from it
// without a startup failure path.
func LoadCreatorArchiveFailures(dataDir, platform, userID string) ([]CreatorArchiveFailure, bool) {
	raw, err := os.ReadFile(creatorArchiveFailuresPath(dataDir, platform, userID))
	if err != nil {
		return nil, false
	}
	var ledger CreatorArchiveFailureLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		return nil, false
	}
	if ledger.Failures == nil {
		return []CreatorArchiveFailure{}, true
	}
	return ledger.Failures, true
}

// persistCreatorArchiveFailures applies one archive run's outcome. A run without
// the media stage leaves prior history untouched; a run with the media stage
// replaces the ledger wholesale, because an asset that succeeded this time is no
// longer a failure.
func persistCreatorArchiveFailures(
	dataDir, platform, userID string,
	mediaRan bool,
	result tikhub.CreatorMediaResult,
	posts []tikhub.CreatorPostMedia,
) error {
	if !mediaRan {
		return nil
	}
	return saveCreatorArchiveFailures(dataDir, platform, userID, failuresFromMediaResult(result.Failures, posts))
}

// creatorPostIdentity indexes this run's post list by aweme id. The media pass
// only reports ids, so the ledger borrows title and cover path from the post
// list SyncCreatorAll already holds instead of making a second network call.
func creatorPostIdentity(posts []tikhub.CreatorPostMedia) map[string]tikhub.CreatorPostMedia {
	if len(posts) == 0 {
		return nil
	}
	index := make(map[string]tikhub.CreatorPostMedia, len(posts))
	for _, post := range posts {
		if post.AwemeID == "" {
			continue
		}
		index[post.AwemeID] = post
	}
	return index
}

func failuresFromMediaResult(items []tikhub.CreatorMediaFailure, posts []tikhub.CreatorPostMedia) []CreatorArchiveFailure {
	if len(items) == 0 {
		return nil
	}
	attemptedAt := time.Now().UTC().Format(time.RFC3339)
	identity := creatorPostIdentity(posts)
	seen := make(map[string]bool, len(items))
	out := make([]CreatorArchiveFailure, 0, len(items))
	for _, item := range items {
		awemeID := item.AwemeID
		if awemeID == "" || item.Kind == "" {
			continue
		}
		key := awemeID + "\x00" + item.Kind
		if seen[key] {
			continue
		}
		seen[key] = true
		post := identity[awemeID]
		out = append(out, CreatorArchiveFailure{
			AwemeID:     awemeID,
			Kind:        item.Kind,
			Reason:      item.Reason,
			AttemptedAt: attemptedAt,
			Title:       post.Title,
			CoverPath:   post.CoverPath,
		})
	}
	// Downloads settle concurrently, so the write path sorts for a stable file
	// across runs.
	return out
}

// saveCreatorArchiveFailures replaces the ledger atomically; an empty list
// removes the file so a fully successful archive leaves no stale artifact.
func saveCreatorArchiveFailures(dataDir, platform, userID string, failures []CreatorArchiveFailure) error {
	target := creatorArchiveFailuresPath(dataDir, platform, userID)
	if len(failures) == 0 {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	root := tikhub.CreatorFolder(dataDir, platform, userID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	sorted := make([]CreatorArchiveFailure, len(failures))
	copy(sorted, failures)
	sort.SliceStable(sorted, func(left, right int) bool {
		if sorted[left].AwemeID != sorted[right].AwemeID {
			return sorted[left].AwemeID < sorted[right].AwemeID
		}
		return sorted[left].Kind < sorted[right].Kind
	})
	ledger := CreatorArchiveFailureLedger{
		SchemaVersion: CreatorArchiveFailuresSchemaVersion,
		Platform:      platform,
		UserID:        userID,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Failures:      sorted,
	}
	raw, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(root, ".archive-failures-*.tmp")
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
