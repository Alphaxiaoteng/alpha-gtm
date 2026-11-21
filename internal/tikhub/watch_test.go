package tikhub

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestWatchlistAutoArchiveDefaultsFalseForLegacyJSON pins the no-migration
// promise: a watchlist written before the switch existed must load without an
// error and must keep auto archive off, so no TikHub credit is spent by default.
func TestWatchlistAutoArchiveDefaultsFalseForLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "tikhub")
	if err := os.MkdirAll(watchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"users":[{"platform":"douyin","user_id":"legacy-user","name":"老数据","added_at":"2026-01-02T03:04:05Z"}]}`
	if err := os.WriteFile(filepath.Join(watchDir, "watchlist.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	users, err := LoadWatchlist(dir)
	if err != nil {
		t.Fatalf("legacy watchlist must load: %v", err)
	}
	if len(users) != 1 || users[0].AutoArchive {
		t.Fatalf("legacy auto_archive must default to false: %#v", users)
	}

	if err := SaveWatchlist(dir, users); err != nil {
		t.Fatalf("save legacy watchlist: %v", err)
	}
	reloaded, err := LoadWatchlist(dir)
	if err != nil || len(reloaded) != 1 || reloaded[0].AutoArchive {
		t.Fatalf("reloaded auto_archive must stay false: %#v err=%v", reloaded, err)
	}
}

func TestWatchAutoArchiveRoundTripAndPreservedByUpdate(t *testing.T) {
	dir := t.TempDir()
	if err := AddWatch(dir, "douyin", "auto-user", "自动归档博主", ""); err != nil {
		t.Fatal(err)
	}

	user, err := SetWatchAutoArchive(dir, "douyin", "auto-user", true)
	if err != nil {
		t.Fatalf("enable auto archive: %v", err)
	}
	if !user.AutoArchive {
		t.Fatalf("returned user must report enabled: %#v", user)
	}
	users, err := LoadWatchlist(dir)
	if err != nil || len(users) != 1 || !users[0].AutoArchive {
		t.Fatalf("enabled switch did not persist: %#v err=%v", users, err)
	}

	// 抓取参数表单不带开关字段；UpdateWatch 必须保留已有值。
	if err := UpdateWatch(dir, WatchUser{
		Platform: "douyin", UserID: "auto-user", Name: "改名", LookbackDays: 7,
		PageSize: 20, MaxPages: 2, SortType: 1,
	}); err != nil {
		t.Fatalf("update options: %v", err)
	}
	users, err = LoadWatchlist(dir)
	if err != nil || len(users) != 1 || !users[0].AutoArchive {
		t.Fatalf("UpdateWatch dropped auto_archive: %#v err=%v", users, err)
	}
	if users[0].Name != "改名" || users[0].MaxPages != 2 {
		t.Fatalf("option edit was not applied: %#v", users[0])
	}

	if _, err := SetWatchAutoArchive(dir, "douyin", "auto-user", false); err != nil {
		t.Fatalf("disable auto archive: %v", err)
	}
	users, err = LoadWatchlist(dir)
	if err != nil || len(users) != 1 || users[0].AutoArchive {
		t.Fatalf("disabled switch did not persist: %#v err=%v", users, err)
	}

	if _, err := SetWatchAutoArchive(dir, "douyin", "missing-user", true); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unknown creator must report ErrNotExist, got %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "tikhub", "watchlist.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("watchlist mode: %v %v", info, err)
	}
}
