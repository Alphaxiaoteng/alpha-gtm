package observability

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Diagnostic event files are named per process: events-<pid>.jsonl, plus
// rotation siblings events-<pid>.jsonl.<n>. The in-process rotating writer
// bounds a single process's output, but nothing bounded the number of files a
// machine accumulates across restarts: one file per PID, forever.
//
// PruneEventFiles is the missing garbage collector. It removes event files that
// belong to processes which are no longer alive, and files older than MaxAge,
// while leaving anything the caller declares live untouched.

// PruneEventFileResult reports what a prune pass removed.
type PruneEventFileResult struct {
	Scanned int
	Removed int
	Kept    int
	Bytes   int64
	// Errors holds the per-file failures. A prune pass never aborts on one bad
	// file: an unreadable entry must not block cleaning the rest.
	Errors []error
}

type PruneEventFileOptions struct {
	// KeepPIDs are live process ids whose files must survive regardless of age.
	KeepPIDs map[int]struct{}
	// MaxAge removes files older than this even when their PID is unknown.
	// Zero disables the age rule and prunes on PID liveness alone.
	MaxAge time.Duration
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time
}

const eventFilePrefix = "events-"
const eventFileSuffix = ".jsonl"

// PruneEventFiles deletes stale diagnostic event files from dir.
//
// It only ever considers files matching events-<digits>.jsonl or
// events-<digits>.jsonl.<digits>; every other entry in the directory is left
// alone, so a shared directory such as logs/ keeps unrelated artifacts.
func PruneEventFiles(dir string, opts PruneEventFileOptions) (PruneEventFileResult, error) {
	var result PruneEventFileResult
	if dir == "" {
		return result, fmt.Errorf("event file directory required")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("read event file directory: %w", err)
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	cutoff := time.Time{}
	if opts.MaxAge > 0 {
		cutoff = now().Add(-opts.MaxAge)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		pid, ok := eventFilePID(entry.Name())
		if !ok {
			continue
		}
		result.Scanned++
		info, infoErr := entry.Info()
		if infoErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("stat %s: %w", entry.Name(), infoErr))
			continue
		}
		if _, live := opts.KeepPIDs[pid]; live {
			result.Kept++
			continue
		}
		// With MaxAge the age rule is the only rule: a recent file is kept even
		// when its PID is unknown. Without MaxAge, a file whose owner is gone is
		// only removed once it is at least a minute old, so a process that has
		// just started and not yet been recorded as live cannot lose its file.
		var expired bool
		if !cutoff.IsZero() {
			expired = info.ModTime().Before(cutoff)
		} else {
			expired = info.ModTime().Before(now().Add(-time.Minute))
		}
		if !expired {
			result.Kept++
			continue
		}
		if removeErr := os.Remove(filepath.Join(dir, entry.Name())); removeErr != nil {
			if os.IsNotExist(removeErr) {
				continue
			}
			result.Errors = append(result.Errors, fmt.Errorf("remove %s: %w", entry.Name(), removeErr))
			continue
		}
		result.Removed++
		result.Bytes += info.Size()
	}
	return result, nil
}

// eventFilePID extracts the owning process id from an event log file name and
// reports whether the name follows the managed convention.
func eventFilePID(name string) (int, bool) {
	if !strings.HasPrefix(name, eventFilePrefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(name, eventFilePrefix)
	// Drop rotation suffix: events-<pid>.jsonl.3 -> events-<pid>.jsonl
	if idx := strings.Index(rest, eventFileSuffix+"."); idx >= 0 {
		suffixDigits := rest[idx+len(eventFileSuffix)+1:]
		if suffixDigits == "" || !allDigits(suffixDigits) {
			return 0, false
		}
		rest = rest[:idx+len(eventFileSuffix)]
	}
	if !strings.HasSuffix(rest, eventFileSuffix) {
		return 0, false
	}
	pidText := strings.TrimSuffix(rest, eventFileSuffix)
	if pidText == "" || !allDigits(pidText) {
		return 0, false
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// EventLogStats summarises the managed event files in a directory without
// deleting anything, so a settings page can show real usage.
type EventLogSummary struct {
	Files   int
	Bytes   int64
	Oldest  time.Time
	Newest  time.Time
	NewestN string
}

func ReadEventLogSummary(dir string) (EventLogSummary, error) {
	var stats EventLogSummary
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, fmt.Errorf("read event file directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if _, ok := eventFilePID(entry.Name()); !ok {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		stats.Files++
		stats.Bytes += info.Size()
		if stats.Oldest.IsZero() || info.ModTime().Before(stats.Oldest) {
			stats.Oldest = info.ModTime()
		}
		if info.ModTime().After(stats.Newest) {
			stats.Newest = info.ModTime()
			stats.NewestN = entry.Name()
		}
	}
	return stats, nil
}
