//go:build legacy_chromium

package browser_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/browser"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "unibuy-browser-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestProfileLockAcquireAndRelease(t *testing.T) {
	dir := setupTestDir(t)
	lock := browser.NewProfileLock(dir, "test-profile")

	if err := lock.Acquire(); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if !lock.IsLocked() {
		t.Error("lock should be held after Acquire")
	}

	lock2 := browser.NewProfileLock(dir, "test-profile")
	if err := lock2.Acquire(); err == nil {
		t.Error("second acquire should fail (lock held)")
		lock2.Release()
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	lock3 := browser.NewProfileLock(dir, "test-profile")
	if err := lock3.Acquire(); err != nil {
		t.Fatalf("acquire after release should succeed: %v", err)
	}
	lock3.Release()
}

func TestProfileLockReentrant(t *testing.T) {
	dir := setupTestDir(t)
	lock := browser.NewProfileLock(dir, "test-profile")
	if err := lock.Acquire(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Acquire(); err != nil {
		t.Fatalf("reentrant acquire should be no-op: %v", err)
	}
	lock.Release()
}

func TestBrowserStatusNotRunning(t *testing.T) {
	dir := setupTestDir(t)
	mgr := browser.NewManager("/bin/sh", dir, 29999)
	status, err := mgr.Status("test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Error("browser should not be running initially")
	}
}

func TestStartWithoutBrowserPath(t *testing.T) {
	dir := setupTestDir(t)
	mgr := browser.NewManager("", dir, 15101)
	ctx := context.Background()
	_, err := mgr.Start(ctx, "test")
	if err == nil {
		t.Fatal("expected error for empty browser path")
		mgr.Stop()
	}
}

func TestStartWithNonexistentBrowser(t *testing.T) {
	dir := setupTestDir(t)
	mgr := browser.NewManager("/nonexistent/browser", dir, 15102)
	ctx := context.Background()
	_, err := mgr.Start(ctx, "test")
	if err == nil {
		mgr.Stop()
		t.Fatal("expected error for nonexistent browser")
	}
}

func TestProfileDirCreated(t *testing.T) {
	dir := setupTestDir(t)
	profileName := "test-profile-dir"
	lock := browser.NewProfileLock(dir, profileName)
	if err := lock.Acquire(); err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	expected := filepath.Join(dir, "profiles", profileName)
	info, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("profile dir should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("profile path should be a directory")
	}
}
