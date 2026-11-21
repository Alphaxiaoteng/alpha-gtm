package tikhub

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFakeSnapshot(t *testing.T, dataDir string, callID int64) string {
	t.Helper()
	dir := filepath.Join(dataDir, "tikhub", "calls", "2026-11-21")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "snapshot.json")
	body := `{"response":{"ok":true,"items":[1,2,3]}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// 同一端点同一参数在窗口内必须命中，且写进磁盘的索引能被另一次查询读到——
// 这正是 UI 进程与 radar worker 进程之间复用付费结果的前提。
func TestReusableCallRoundTripsAcrossLookups(t *testing.T) {
	dataDir := t.TempDir()
	rawPath := writeFakeSnapshot(t, dataDir, 7)
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	params := map[string]any{"sec_user_id": "abc", "cursor": float64(0)}

	RecordReusableCall(dataDir, "douyin.creator_posts", params,
		ReuseEntry{CallID: 7, RawPath: rawPath}, now)

	entry, hit := LookupReusableCall(dataDir, "douyin.creator_posts", params, "creator_posts", now.Add(30*time.Minute))
	if !hit {
		t.Fatal("same endpoint and params inside the window must reuse")
	}
	if entry.CallID != 7 || entry.RawPath != rawPath {
		t.Fatalf("entry=%+v", entry)
	}
	response, err := ReadSnapshotResponse(dataDir, entry.RawPath)
	if err != nil || response == nil {
		t.Fatalf("snapshot replay failed: %v", err)
	}
}

// 参数顺序不该影响命中：map 遍历顺序变化是 Go 的常态。
func TestReuseKeyIsStableAcrossParamOrder(t *testing.T) {
	first := ReuseKey("xhs.user_posts", map[string]any{"a": "1", "b": "2"})
	second := ReuseKey("xhs.user_posts", map[string]any{"b": "2", "a": "1"})
	if first != second {
		t.Fatalf("key changed with param order: %q vs %q", first, second)
	}
	if first == ReuseKey("xhs.user_posts", map[string]any{"a": "1", "b": "3"}) {
		t.Fatal("different params must produce different keys")
	}
}

// 窗口过期后必须重新调用，否则界面会永远看到同一份旧数据。
func TestReusableCallExpiresAfterWindow(t *testing.T) {
	dataDir := t.TempDir()
	rawPath := writeFakeSnapshot(t, dataDir, 9)
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	params := map[string]any{"keyword": "ai"}
	RecordReusableCall(dataDir, "douyin.video_search", params,
		ReuseEntry{CallID: 9, RawPath: rawPath}, now)

	if _, hit := LookupReusableCall(dataDir, "douyin.video_search", params, "keyword_search", now.Add(5*time.Hour)); !hit {
		t.Fatal("inside the 6h window must still reuse")
	}
	if _, hit := LookupReusableCall(dataDir, "douyin.video_search", params, "keyword_search", now.Add(7*time.Hour)); hit {
		t.Fatal("past the window must not reuse")
	}
}

// 未知能力不复用：宁可多花一次，也不要给出可能过期的数据。
func TestUnknownCapabilityNeverReuses(t *testing.T) {
	dataDir := t.TempDir()
	rawPath := writeFakeSnapshot(t, dataDir, 3)
	now := time.Now()
	params := map[string]any{"id": "1"}
	RecordReusableCall(dataDir, "some.unknown_capability", params,
		ReuseEntry{CallID: 3, RawPath: rawPath}, now)
	if _, hit := LookupReusableCall(dataDir, "some.unknown_capability", params, "unknown_capability", now); hit {
		t.Fatal("unknown capability must not be reused")
	}
}

// 快照文件被清理掉之后不能回放一个空响应。
func TestLookupSkipsEntryWhoseSnapshotIsGone(t *testing.T) {
	dataDir := t.TempDir()
	rawPath := writeFakeSnapshot(t, dataDir, 11)
	now := time.Now()
	params := map[string]any{"kw": "x"}
	RecordReusableCall(dataDir, "douyin.creator_posts", params,
		ReuseEntry{CallID: 11, RawPath: rawPath}, now)
	if err := os.Remove(rawPath); err != nil {
		t.Fatal(err)
	}
	if _, hit := LookupReusableCall(dataDir, "douyin.creator_posts", params, "creator_posts", now); hit {
		t.Fatal("a missing snapshot must not be reused")
	}
}

// 索引文件损坏不该让调用失败：缓存是省钱手段，不是正确性依赖。
func TestLookupSurvivesCorruptIndex(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "tikhub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "tikhub", "reuse-index.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit := LookupReusableCall(dataDir, "douyin.creator_posts", map[string]any{"a": "1"}, "creator_posts", time.Now()); hit {
		t.Fatal("corrupt index must read as a miss")
	}
	// 写一遍能自愈。
	rawPath := writeFakeSnapshot(t, dataDir, 12)
	RecordReusableCall(dataDir, "douyin.creator_posts", map[string]any{"a": "1"},
		ReuseEntry{CallID: 12, RawPath: rawPath}, time.Now())
	if _, hit := LookupReusableCall(dataDir, "douyin.creator_posts", map[string]any{"a": "1"}, "creator_posts", time.Now()); !hit {
		t.Fatal("recording after a corrupt index must repair it")
	}
}
