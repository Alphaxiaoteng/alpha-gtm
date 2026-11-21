package ui

import (
	"testing"
	"time"
)

// 键必须与参数顺序无关，否则同样参数会因为 map 遍历顺序不同而漏命中。
func TestDataAPICacheKeyIsOrderIndependent(t *testing.T) {
	first := dataAPICacheKey("douyin.creator_posts", map[string]any{
		"sec_user_id": "abc", "cursor": float64(0), "count": float64(20),
	})
	second := dataAPICacheKey("douyin.creator_posts", map[string]any{
		"count": float64(20), "sec_user_id": "abc", "cursor": float64(0),
	})
	if first != second {
		t.Errorf("key must not depend on map order:\n%q\n%q", first, second)
	}
}

func TestDataAPICacheKeyDiffersByEndpoint(t *testing.T) {
	params := map[string]any{"keyword": "男性情感"}
	if dataAPICacheKey("douyin.video_search", params) == dataAPICacheKey("xhs.note_search", params) {
		t.Error("different endpoints must not share a key")
	}
}

func TestDataAPICacheKeyDiffersByValue(t *testing.T) {
	a := dataAPICacheKey("douyin.video_search", map[string]any{"keyword": "AI"})
	b := dataAPICacheKey("douyin.video_search", map[string]any{"keyword": "AI 落地"})
	if a == b {
		t.Error("different parameter values must not share a key")
	}
}

func TestDataAPICacheTTLByCapability(t *testing.T) {
	if dataAPICacheTTL("keyword_search") != 6*time.Hour {
		t.Error("search results should only be reused for 6h")
	}
	if dataAPICacheTTL("creator_posts") != time.Hour {
		t.Error("post lists change often; 1h")
	}
	if dataAPICacheTTL("creator_profile") != 24*time.Hour {
		t.Error("profiles barely change; 24h")
	}
	if dataAPICacheTTL("unknown_capability") != 0 {
		t.Error("unknown capabilities must never be reused")
	}
}

func TestDataAPICacheHitAndExpiry(t *testing.T) {
	cache := newDataAPICache()
	params := map[string]any{"keyword": "男性情感"}
	now := time.Now()
	cache.store("douyin.video_search", params, dataAPICacheEntry{CallID: 42, RawPath: "/x.json"}, now)

	if entry, hit := cache.lookup("douyin.video_search", params, now.Add(time.Minute)); !hit || entry.CallID != 42 {
		t.Fatalf("expected a hit within the window, got %+v hit=%v", entry, hit)
	}
	// 超过 6 小时后必须失效。
	if _, hit := cache.lookup("douyin.video_search", params, now.Add(7*time.Hour)); hit {
		t.Error("entry must expire after the capability TTL")
	}
}

func TestDataAPICacheNilSafe(t *testing.T) {
	var cache *dataAPICache
	if _, hit := cache.lookup("x", nil, time.Now()); hit {
		t.Error("nil cache must never hit")
	}
	cache.store("x", nil, dataAPICacheEntry{}, time.Now()) // 不应 panic
}
