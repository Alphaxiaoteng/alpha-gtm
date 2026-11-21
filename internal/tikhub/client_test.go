package tikhub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractRankItemsNested(t *testing.T) {
	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"data": map[string]interface{}{
				"objs": []interface{}{
					map[string]interface{}{"item_title": "a"},
					map[string]interface{}{"item_title": "b"},
				},
			},
		},
	}
	items := ExtractRankItems(resp)
	if len(items) != 2 {
		t.Fatalf("got %d", len(items))
	}
}

func TestRankBoardNames(t *testing.T) {
	if _, ok := RankBoards["video"]; !ok {
		t.Fatal("missing video board")
	}
}

func TestWatchlistAddRemove(t *testing.T) {
	dir := t.TempDir()
	if err := AddWatch(dir, "xhs", "u1", "A", ""); err != nil {
		t.Fatal(err)
	}
	users, err := LoadWatchlist(dir)
	if err != nil || len(users) != 1 {
		t.Fatalf("load: %v n=%d", err, len(users))
	}
	if err := RemoveWatch(dir, "xhs", "u1"); err != nil {
		t.Fatal(err)
	}
	users, _ = LoadWatchlist(dir)
	if len(users) != 0 {
		t.Fatalf("want empty after remove, got %d", len(users))
	}
}

func TestWriteRankMarkdown(t *testing.T) {
	dir := t.TempDir()
	items := []map[string]interface{}{
		{"item_title": "hello", "nick_name": "bob", "item_id": "123", "play_cnt": float64(1000)},
	}
	fp, err := WriteRankMarkdown(dir, "2026-07-12", "video", items, "now")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(b), "hello", "bob", "123") {
		t.Fatalf("md missing fields: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "tikhub", "ranks", "2026-07-12", "video.md")); err != nil {
		t.Fatal(err)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
