package radar

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tikhub"
)

// CreatorAutoArchiveInterval is the minimum gap between two automatic passes
// for the same creator. The background worker ticks every minute, so a shorter
// window would spend TikHub credit 1440 times a day per creator.
const CreatorAutoArchiveInterval = 24 * time.Hour

// CreatorAutoArchiveMaxPages keeps the automatic pass incremental: Douyin
// returns 20 posts per page, so two pages (~40 latest posts) are enough to meet
// already-archived work. Media downloads reuse the manual archive path, where
// existing files are skipped.
const CreatorAutoArchiveMaxPages = 2

// CreatorAutoArchiveProgressFreshFor mirrors the UI's crash-remnant rule: an
// archive-progress.json updated within three minutes means a manual archive is
// still running, so the worker must not compete for the same creator folder.
const CreatorAutoArchiveProgressFreshFor = 3 * time.Minute

// creatorAutoArchiveTimeout bounds one automatic pass so a stuck media download
// cannot hold the worker lock for its whole 30-minute stale window.
const creatorAutoArchiveTimeout = 10 * time.Minute

// CreatorAutoArchiveState is the worker-owned record of the last automatic
// archive attempt for one creator. It lives inside the creator folder instead of
// watchlist.json for two reasons: the worker never has to write the watchlist
// (so it cannot clobber a concurrent UI edit), and manual archives keep their own
// progress/index files, so an automatic pass can never be mistaken for a manual
// one or vice versa.
type CreatorAutoArchiveState struct {
	SchemaVersion int    `json:"schema_version"`
	Platform      string `json:"platform"`
	UserID        string `json:"user_id"`
	LastAttemptAt string `json:"last_attempt_at"`
	LastStatus    string `json:"last_status,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	LastNewPosts  int    `json:"last_new_posts,omitempty"`
}

// LastAttempt parses LastAttemptAt, returning the zero time when the value is
// absent or corrupt so the creator simply becomes due again.
func (state CreatorAutoArchiveState) LastAttempt() time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, state.LastAttemptAt); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func creatorAutoArchiveStatePath(dataDir, platform, userID string) string {
	return filepath.Join(tikhub.CreatorFolder(dataDir, platform, userID), "auto-archive.json")
}

func LoadCreatorAutoArchiveState(dataDir, platform, userID string) (CreatorAutoArchiveState, bool) {
	raw, err := os.ReadFile(creatorAutoArchiveStatePath(dataDir, platform, userID))
	if err != nil {
		return CreatorAutoArchiveState{}, false
	}
	var state CreatorAutoArchiveState
	if err := json.Unmarshal(raw, &state); err != nil {
		return CreatorAutoArchiveState{}, false
	}
	return state, true
}

func SaveCreatorAutoArchiveState(dataDir string, state CreatorAutoArchiveState) error {
	state.SchemaVersion = 1
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	root := tikhub.CreatorFolder(dataDir, state.Platform, state.UserID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	target := creatorAutoArchiveStatePath(dataDir, state.Platform, state.UserID)
	temp, err := os.CreateTemp(root, ".auto-archive-*.tmp")
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

// CreatorAutoArchiveCandidate is one watchlist entry plus the time of its last
// automatic attempt (zero when it has never run).
type CreatorAutoArchiveCandidate struct {
	User          tikhub.WatchUser
	LastAttemptAt time.Time
}

// supportsCreatorAutoArchive is the single platform gate for creator archival:
// the selection below, the worker loop, and creator_sync.go's endpoint keys all
// read it, so enabling a platform here enables the whole chain at once. Adding
// a platform without a verified profile and posts endpoint would only produce
// failed passes, so the list stays explicit.
func supportsCreatorAutoArchive(platform string) bool {
	switch platform {
	case "douyin", "xhs":
		return true
	default:
		return false
	}
}

// SelectCreatorAutoArchiveCreators is a pure decision: it keeps only creators
// whose switch is on, whose platform supports incremental archival, and whose
// last automatic attempt is at least CreatorAutoArchiveInterval before now.
// No disk or network access, so tests can pin the cadence without a worker run.
func SelectCreatorAutoArchiveCreators(candidates []CreatorAutoArchiveCandidate, now time.Time) []tikhub.WatchUser {
	selected := make([]tikhub.WatchUser, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.User.AutoArchive || !supportsCreatorAutoArchive(candidate.User.Platform) {
			continue
		}
		if !candidate.LastAttemptAt.IsZero() && now.Sub(candidate.LastAttemptAt) < CreatorAutoArchiveInterval {
			continue
		}
		selected = append(selected, candidate.User)
	}
	return selected
}

// CreatorArchiveInProgress reports whether a manual/UI archive currently owns
// this creator folder. A progress file older than the freshness window is a
// crash remnant and must not block the worker forever.
func CreatorArchiveInProgress(dataDir, platform, userID string, now time.Time) bool {
	progress, ok := LoadCreatorArchiveProgress(dataDir, platform, userID)
	if !ok {
		return false
	}
	updated, err := time.Parse(time.RFC3339Nano, progress.UpdatedAt)
	if err != nil {
		updated, err = time.Parse(time.RFC3339, progress.UpdatedAt)
	}
	if err != nil {
		return false
	}
	return now.Sub(updated) <= CreatorAutoArchiveProgressFreshFor
}

// runDueCreatorAutoArchives runs the opted-in creators serially (concurrency is
// fixed at 1 by construction) after the keyword schedules of the same tick.
// A creator failure is recorded and the loop continues; only an unreadable
// watchlist aborts the pass.
func runDueCreatorAutoArchives(ctx context.Context, database *db.DB, dataDir string, now time.Time, summary *RunSummary) error {
	users, err := tikhub.LoadWatchlist(dataDir)
	if err != nil {
		return fmt.Errorf("load creator watchlist: %w", err)
	}
	candidates := make([]CreatorAutoArchiveCandidate, 0, len(users))
	for _, user := range users {
		if !user.AutoArchive || !supportsCreatorAutoArchive(user.Platform) {
			continue
		}
		candidate := CreatorAutoArchiveCandidate{User: user}
		if state, ok := LoadCreatorAutoArchiveState(dataDir, user.Platform, user.UserID); ok {
			candidate.LastAttemptAt = state.LastAttempt()
		}
		candidates = append(candidates, candidate)
	}

	selected := SelectCreatorAutoArchiveCreators(candidates, now)
	summary.CreatorsDue = len(selected)
	for _, user := range selected {
		if ctx.Err() != nil {
			return nil
		}
		if CreatorArchiveInProgress(dataDir, user.Platform, user.UserID, now) {
			summary.CreatorsSkipped++
			log.Printf("radar auto-archive %s/%s skipped: manual archive in progress", user.Platform, user.UserID)
			continue
		}

		runCtx, cancel := context.WithTimeout(ctx, creatorAutoArchiveTimeout)
		result, runErr := SyncCreatorAll(runCtx, database, dataDir, user, CreatorSyncOptions{
			MaxPages: CreatorAutoArchiveMaxPages,
			Media:    true,
			// 非空回调让 SyncCreatorAll 写入/清理 archive-progress.json，UI 因此
			// 能看到自动归档进度，同时下一次 tick 也能识别出正在运行的归档。
			OnProgress: func(string, int, int) {},
		})
		cancel()
		summary.CreatorCallIDs = append(summary.CreatorCallIDs, result.CallIDs...)

		state := CreatorAutoArchiveState{
			Platform: user.Platform, UserID: user.UserID,
			LastAttemptAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if runErr != nil {
			// 失败也记录时间：否则同一博主会每分钟重试一次，而重试可能已经
			// 花了额度。「每 24 小时最多自动跑一次」对失败同样成立。
			state.LastStatus = "failed"
			state.LastError = trimAutoArchiveError(runErr.Error())
			summary.CreatorsFailed++
			summary.CreatorErrors = append(summary.CreatorErrors,
				fmt.Sprintf("%s/%s: %s", user.Platform, user.UserID, state.LastError))
			log.Printf("radar auto-archive %s/%s failed: %v", user.Platform, user.UserID, runErr)
		} else {
			state.LastStatus = "succeeded"
			state.LastNewPosts = result.NewPosts
			summary.CreatorsSucceeded++
			log.Printf("radar auto-archive %s/%s done: pages=%d posts=%d new=%d stopped=%s",
				user.Platform, user.UserID, result.Pages, result.Posts, result.NewPosts, result.Stopped)
		}
		if saveErr := SaveCreatorAutoArchiveState(dataDir, state); saveErr != nil {
			summary.CreatorErrors = append(summary.CreatorErrors,
				fmt.Sprintf("%s/%s: state write failed: %v", user.Platform, user.UserID, saveErr))
			log.Printf("radar auto-archive %s/%s state write failed: %v", user.Platform, user.UserID, saveErr)
		}
	}
	return nil
}

// trimAutoArchiveError keeps the persisted error short; stdout/stderr logs keep
// the full message.
func trimAutoArchiveError(message string) string {
	message = strings.TrimSpace(message)
	const limit = 300
	runes := []rune(message)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return message
}
