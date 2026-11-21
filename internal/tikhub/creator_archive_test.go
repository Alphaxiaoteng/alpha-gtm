package tikhub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreatorArchiveUsesStableOwnerOnlyFolderAndHardLinksSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	user := WatchUser{
		Platform: "douyin", UserID: "creator-1", Name: "样本博主",
		LookbackDays: 30, PageSize: 20, MaxPages: 3,
	}
	root, err := EnsureCreatorFolder(dataDir, user)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("creator folder mode=%v", info.Mode().Perm())
	}
	rawPath := filepath.Join(dataDir, "raw.json")
	if err := os.WriteFile(rawPath, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	archiveRoot, err := ArchiveCreatorSnapshot(dataDir, rawPath, CallSnapshot{
		CallID: 42, Platform: "douyin", Capability: "creator_posts",
		Parameters: map[string]any{"sec_user_id": "creator-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if archiveRoot != root {
		t.Fatalf("archive root=%q want=%q", archiveRoot, root)
	}
	target := filepath.Join(root, "snapshots", "42_creator_posts.json")
	rawInfo, err := os.Stat(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(rawInfo, targetInfo) {
		t.Fatal("creator snapshot should be a hard link to canonical raw call")
	}
}

func TestWatchDefaultsAndOptionsPersist(t *testing.T) {
	dataDir := t.TempDir()
	if err := AddWatch(dataDir, "douyin", "creator-1", "样本", ""); err != nil {
		t.Fatal(err)
	}
	users, err := LoadWatchlist(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].LookbackDays != 30 || users[0].PageSize != 20 || users[0].MaxPages != 3 {
		t.Fatalf("defaults=%#v", users)
	}
	users[0].LookbackDays = 14
	users[0].MaxPages = 5
	users[0].SortType = 1
	if err := UpdateWatch(dataDir, users[0]); err != nil {
		t.Fatal(err)
	}
	updated, err := LoadWatchlist(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].LookbackDays != 14 || updated[0].MaxPages != 5 || updated[0].SortType != 1 {
		t.Fatalf("updated=%#v", updated[0])
	}
}

func TestVideoLookupArchiveHardLinksIntoIndependentFolder(t *testing.T) {
	dataDir := t.TempDir()
	rawPath := filepath.Join(dataDir, "raw-video.json")
	if err := os.WriteFile(rawPath, []byte(`{"video":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := ArchiveVideoLookupSnapshot(dataDir, rawPath, CallSnapshot{
		CallID: 60, Platform: "douyin", Capability: "content_detail",
		Parameters: map[string]any{"aweme_id": "7666283984318131889"},
	})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "60_7666283984318131889.json")
	rawInfo, err := os.Stat(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(rawInfo, targetInfo) {
		t.Fatal("video lookup should be a hard link to canonical raw call")
	}
}
