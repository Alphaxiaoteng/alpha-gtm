package observability

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEventFile(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("{\"ts\":\"x\"}\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if age > 0 {
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("chtimes %s: %v", name, err)
		}
	}
	return path
}

func TestEventFilePID(t *testing.T) {
	cases := []struct {
		name string
		pid  int
		ok   bool
	}{
		{"events-1234.jsonl", 1234, true},
		{"events-1234.jsonl.3", 1234, true},
		{"events-1234.jsonl.10", 1234, true},
		{"events-.jsonl", 0, false},
		{"events-abc.jsonl", 0, false},
		{"events-1234.jsonl.x", 0, false},
		{"events-1234.log", 0, false},
		{"diagnostic-20260101T000000.000000000Z.json", 0, false},
		{"ui-serve.stdout.log", 0, false},
		{"auto-commit.log", 0, false},
		{"events-0.jsonl", 0, false},
	}
	for _, tc := range cases {
		pid, ok := eventFilePID(tc.name)
		if ok != tc.ok || pid != tc.pid {
			t.Errorf("eventFilePID(%q) = (%d, %v), want (%d, %v)", tc.name, pid, ok, tc.pid, tc.ok)
		}
	}
}

func TestPruneEventFilesRemovesDeadProcesses(t *testing.T) {
	dir := t.TempDir()
	dead := writeEventFile(t, dir, "events-999001.jsonl", 2*time.Hour)
	deadRotated := writeEventFile(t, dir, "events-999002.jsonl.2", 2*time.Hour)
	live := writeEventFile(t, dir, "events-424242.jsonl", 2*time.Hour)
	unrelated := writeEventFile(t, dir, "ui-serve.stdout.log", 48*time.Hour)

	result, err := PruneEventFiles(dir, PruneEventFileOptions{
		KeepPIDs: map[int]struct{}{424242: {}},
	})
	if err != nil {
		t.Fatalf("PruneEventFiles: %v", err)
	}
	if result.Removed != 2 {
		t.Errorf("Removed = %d, want 2", result.Removed)
	}
	if result.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0", result.Bytes)
	}
	for _, gone := range []string{dead, deadRotated} {
		if _, statErr := os.Stat(gone); !os.IsNotExist(statErr) {
			t.Errorf("%s should be gone, stat err = %v", gone, statErr)
		}
	}
	for _, kept := range []string{live, unrelated} {
		if _, statErr := os.Stat(kept); statErr != nil {
			t.Errorf("%s should survive: %v", kept, statErr)
		}
	}
}

func TestPruneEventFilesKeepsFreshFilesOfUnknownPIDs(t *testing.T) {
	dir := t.TempDir()
	fresh := writeEventFile(t, dir, "events-999003.jsonl", 0)

	result, err := PruneEventFiles(dir, PruneEventFileOptions{})
	if err != nil {
		t.Fatalf("PruneEventFiles: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("Removed = %d, want 0 (a just-started process must not lose its file)", result.Removed)
	}
	if _, statErr := os.Stat(fresh); statErr != nil {
		t.Errorf("fresh file should survive: %v", statErr)
	}
}

func TestPruneEventFilesHonoursMaxAge(t *testing.T) {
	dir := t.TempDir()
	old := writeEventFile(t, dir, "events-777001.jsonl", 30*24*time.Hour)
	recent := writeEventFile(t, dir, "events-777002.jsonl", 2*time.Hour)

	result, err := PruneEventFiles(dir, PruneEventFileOptions{
		MaxAge: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("PruneEventFiles: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1", result.Removed)
	}
	if _, statErr := os.Stat(old); !os.IsNotExist(statErr) {
		t.Errorf("aged file should be gone")
	}
	if _, statErr := os.Stat(recent); statErr != nil {
		t.Errorf("recent file should survive: %v", statErr)
	}
}

func TestPruneEventFilesMissingDirectoryIsNotAnError(t *testing.T) {
	result, err := PruneEventFiles(filepath.Join(t.TempDir(), "absent"), PruneEventFileOptions{})
	if err != nil {
		t.Fatalf("PruneEventFiles on missing dir: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("Removed = %d, want 0", result.Removed)
	}
}

func TestEventLogStatsCountsManagedFilesOnly(t *testing.T) {
	dir := t.TempDir()
	writeEventFile(t, dir, "events-5001.jsonl", 3*time.Hour)
	writeEventFile(t, dir, "events-5002.jsonl.1", 0)
	writeEventFile(t, dir, "auto-commit.log", 0)

	stats, err := ReadEventLogSummary(dir)
	if err != nil {
		t.Fatalf("ReadEventLogSummary: %v", err)
	}
	if stats.Files != 2 {
		t.Errorf("Files = %d, want 2", stats.Files)
	}
	if stats.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0", stats.Bytes)
	}
	if stats.Oldest.IsZero() || stats.Newest.IsZero() {
		t.Errorf("Oldest/Newest should be set: %+v", stats)
	}
	if stats.NewestN != "events-5002.jsonl.1" {
		t.Errorf("NewestN = %q, want events-5002.jsonl.1", stats.NewestN)
	}
}
