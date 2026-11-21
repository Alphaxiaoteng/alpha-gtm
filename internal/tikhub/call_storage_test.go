package tikhub

import (
	"os"
	"testing"
)

// A paid provider response must never be dropped just because a snapshot file
// with the same call id already exists (call ids repeat after a DB restore).
func TestSaveCallSnapshotSurvivesNameCollision(t *testing.T) {
	dataDir := t.TempDir()
	first := CallSnapshot{CallID: 176, TraceID: "traceaaaaaaaa", Platform: "douyin",
		EndpointID: "douyin.video_search", Capability: "keyword_search",
		Method: "GET", Path: "/api/v1/douyin/app/v3/fetch_video_search_result",
		Parameters: map[string]any{"keyword": "男性情感"},
		Response:   map[string]interface{}{"data": map[string]interface{}{"items": []interface{}{}}},
	}
	path1, _, err := SaveCallSnapshot(dataDir, first)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	second := first
	second.TraceID = "tracebbbbbbbb"
	path2, _, err := SaveCallSnapshot(dataDir, second)
	if err != nil {
		t.Fatalf("second save on collision: %v", err)
	}
	if path1 == path2 {
		t.Fatalf("expected a distinct path on collision, both were %s", path1)
	}
	for _, path := range []string{path1, path2} {
		if info, statErr := os.Stat(path); statErr != nil || info.Size() == 0 {
			t.Fatalf("snapshot %s not written: %v", path, statErr)
		}
	}
}
