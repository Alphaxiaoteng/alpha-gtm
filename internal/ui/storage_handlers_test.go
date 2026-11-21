package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDatabaseSnapshotPathsExcludesLiveDatabase(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"app.db", "app.db-wal", "app.db-shm",
		"app.db.bak-1", "app.db.bak-before-scope-fix.db",
		"app.before-dom-metric-fix.db",
		"config.json", "alpha-nexus.db",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	paths := databaseSnapshotPaths(dir)
	if len(paths) != 3 {
		t.Fatalf("got %d snapshots, want 3: %v", len(paths), paths)
	}
	for _, path := range paths {
		base := filepath.Base(path)
		if base == "app.db" || base == "app.db-wal" || base == "app.db-shm" {
			t.Errorf("live database file %q must never be treated as a snapshot", base)
		}
	}
}

func TestPruneDatabaseSnapshotsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "app.db.bak-old")
	recent := filepath.Join(dir, "app.db.bak-recent")
	for i, path := range []string{old, recent} {
		if err := os.WriteFile(path, []byte("database-bytes"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		stamp := time.Now().Add(time.Duration(-24*(len([]string{old, recent})-i)) * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		if err := os.WriteFile(path+"-wal", []byte("wal"), 0o600); err != nil {
			t.Fatalf("write wal: %v", err)
		}
	}
	live := filepath.Join(dir, "app.db")
	if err := os.WriteFile(live, []byte("live"), 0o600); err != nil {
		t.Fatalf("write live: %v", err)
	}

	removed, bytes, err := pruneDatabaseSnapshots(dir, 1)
	if err != nil {
		t.Fatalf("pruneDatabaseSnapshots: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2 (snapshot + its wal)", removed)
	}
	if bytes <= 0 {
		t.Errorf("bytes = %d, want > 0", bytes)
	}
	if _, statErr := os.Stat(old); !os.IsNotExist(statErr) {
		t.Errorf("oldest snapshot should be gone, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(old + "-wal"); !os.IsNotExist(statErr) {
		t.Errorf("oldest snapshot wal should be gone")
	}
	if _, statErr := os.Stat(recent); statErr != nil {
		t.Errorf("newest snapshot should survive: %v", statErr)
	}
	if _, statErr := os.Stat(live); statErr != nil {
		t.Errorf("live database must survive: %v", statErr)
	}
}

func TestPruneDatabaseSnapshotsNoOpWhenWithinLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.db.bak-1"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	removed, bytes, err := pruneDatabaseSnapshots(dir, 1)
	if err != nil {
		t.Fatalf("pruneDatabaseSnapshots: %v", err)
	}
	if removed != 0 || bytes != 0 {
		t.Errorf("removed=%d bytes=%d, want 0/0", removed, bytes)
	}
}

func TestDirectoryUsageCountsFilesAndBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "b.txt"), []byte("123"), 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}

	files, bytes := directoryUsage(dir)
	if files != 2 {
		t.Errorf("files = %d, want 2", files)
	}
	if bytes != 8 {
		t.Errorf("bytes = %d, want 8", bytes)
	}
}

func TestDirectoryUsageMissingDirectoryIsZero(t *testing.T) {
	files, bytes := directoryUsage(filepath.Join(t.TempDir(), "absent"))
	if files != 0 || bytes != 0 {
		t.Errorf("files=%d bytes=%d, want 0/0", files, bytes)
	}
}

func TestPruneDirectoryContentsSkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), []byte("png"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "keepme"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	removed, bytes := pruneDirectoryContents(dir)
	if removed != 1 || bytes != 3 {
		t.Errorf("removed=%d bytes=%d, want 1/3", removed, bytes)
	}
	if _, err := os.Stat(filepath.Join(dir, "keepme")); err != nil {
		t.Errorf("subdirectory must survive: %v", err)
	}
}

// 截图写在子目录里，只扫顶层等于什么都没删。
func TestPruneDirectoryContentsRecursesIntoSubdirs(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "publish-inspections")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.png"), []byte("aaa"), 0o600); err != nil {
		t.Fatalf("write top: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "deep.png"), []byte("bbbb"), 0o600); err != nil {
		t.Fatalf("write deep: %v", err)
	}

	removed, bytes := pruneDirectoryContents(dir)
	if removed != 2 {
		t.Errorf("removed = %d, want 2 (top + nested)", removed)
	}
	if bytes != 7 {
		t.Errorf("bytes = %d, want 7", bytes)
	}
	if _, err := os.Stat(filepath.Join(nested, "deep.png")); !os.IsNotExist(err) {
		t.Error("nested file should be gone")
	}
}

// 子目录里的符号链接仍不能被跟随。
func TestPruneDirectoryContentsDoesNotFollowNestedSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	sub := filepath.Join(root, "desktop")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(sub, "link.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	pruneDirectoryContents(root)
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("target outside must survive: %v", err)
	}
}

func TestRemoveEmptySubdirsKeepsNonEmpty(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	full := filepath.Join(dir, "full")
	for _, d := range []string{empty, full} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(full, "a.png"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	removeEmptySubdirs(dir)
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Error("empty subdir should be removed")
	}
	if _, err := os.Stat(full); err != nil {
		t.Errorf("non-empty subdir must survive: %v", err)
	}
}
