package db_test

import (
	"testing"

	"alpha-nexus-egolite/internal/db"
)

// A replayed (older) capture must not move last_seen_at backwards, and it must
// fill published_at only when empty — a later capture without the field cannot
// erase a real publish time.
func TestRecordRadarCapturePublishTimeAndLastSeen(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	first := "2026-11-19T10:00:00Z"
	older := "2026-11-18T10:00:00Z"
	later := "2026-11-20T10:00:00Z"
	published := "2026-11-17T08:20:43Z"

	record := func(observedAt, publishedAt string) {
		t.Helper()
		if _, err := database.RecordRadarCapture(db.RadarCapture{
			Platform: "xhs", WorkID: "note_1", PoolID: "ai", PoolName: "AI 赛道",
			Term: "FDE", Author: "作者", Title: "标题", URL: "https://example.com/n/1",
			PublishedAt: publishedAt, ObservedAt: observedAt,
		}); err != nil {
			t.Fatalf("record capture: %v", err)
		}
	}

	record(first, published)
	record(older, "")
	record(later, "")

	var lastSeen, publishedAt, author string
	if err := database.QueryRow(`SELECT last_seen_at, published_at, author FROM radar_works WHERE platform='xhs' AND work_id='note_1'`).
		Scan(&lastSeen, &publishedAt, &author); err != nil {
		t.Fatal(err)
	}
	if lastSeen != later {
		t.Fatalf("last_seen_at = %q, want the newest observation %q", lastSeen, later)
	}
	if publishedAt != published {
		t.Fatalf("published_at = %q, want %q preserved", publishedAt, published)
	}
	if author != "作者" {
		t.Fatalf("author = %q, want the first non-empty value kept", author)
	}
}
