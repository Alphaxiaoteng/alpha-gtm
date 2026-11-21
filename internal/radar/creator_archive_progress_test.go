package radar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreatorArchiveProgressRoundTrip(t *testing.T) {
	dir := t.TempDir()
	platform, userID := "douyin", "test-user"
	progress := CreatorArchiveProgress{
		Phase: "posts", PostsFetched: 40, PostsTotalEstimate: 40,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := SaveCreatorArchiveProgress(dir, platform, userID, progress); err != nil {
		t.Fatalf("save progress: %v", err)
	}
	loaded, ok := LoadCreatorArchiveProgress(dir, platform, userID)
	if !ok {
		t.Fatal("expected progress to load")
	}
	if loaded.Phase != "posts" || loaded.PostsFetched != 40 || loaded.UpdatedAt == "" {
		t.Fatalf("unexpected loaded progress: %+v", loaded)
	}
	ClearCreatorArchiveProgress(dir, platform, userID)
	if _, ok := LoadCreatorArchiveProgress(dir, platform, userID); ok {
		t.Fatal("expected progress to be cleared")
	}
	if _, err := os.Stat(filepath.Join(dir, "trend-radar", "creators", platform, userID, "archive-progress.json")); !os.IsNotExist(err) {
		t.Fatalf("expected progress file removed, stat err=%v", err)
	}
}
