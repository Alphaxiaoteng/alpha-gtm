package tikhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func TestExtractContentCoverURLsUsesFirstGalleryImageAndSkipsAvatar(t *testing.T) {
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"note": map[string]interface{}{
						"id":    "6a0691460000000007011f48",
						"title": "AI 工具",
						"images_list": []interface{}{
							map[string]interface{}{"url": "https://sns-na-i11.xhscdn.com/cover.webp"},
							map[string]interface{}{"url": "https://sns-na-i11.xhscdn.com/second.webp"},
						},
						"user": map[string]interface{}{"images": "https://sns-avatar-qc.xhscdn.com/avatar.jpg"},
					},
				},
			},
		},
	}
	got := ExtractContentCoverURLs(response)
	if len(got) != 1 || got[0] != "https://sns-na-i11.xhscdn.com/cover.webp" {
		t.Fatalf("covers=%#v", got)
	}
}

func TestCacheContentCoversStoresOwnerOnlyImage(t *testing.T) {
	image := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00\x00\x00\x00!")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write(image)
	}))
	defer server.Close()

	response := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"id": "item-1", "title": "测试封面",
				"cover": map[string]interface{}{"url": server.URL + "/cover"},
			},
		},
	}
	dataDir := t.TempDir()
	result := cacheContentCovers(context.Background(), dataDir, response, server.Client(), func(*url.URL) bool { return true })
	if result.Discovered != 1 || result.Cached != 1 || result.Failed != 0 {
		t.Fatalf("result=%#v", result)
	}
	path := CachedCoverPath(dataDir, server.URL+"/cover")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(image) {
		t.Fatal("cached cover bytes changed")
	}

	second := cacheContentCovers(context.Background(), dataDir, response, server.Client(), func(*url.URL) bool { return true })
	if second.Existing != 1 || second.Cached != 0 {
		t.Fatalf("second=%#v", second)
	}
}

func TestExtractContentCoverURLsSupportsYouTubeAndXMedia(t *testing.T) {
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"videos": []interface{}{
				map[string]interface{}{
					"video_id": "yt-1",
					"title":    "AI 工具",
					"thumbnails": []interface{}{
						map[string]interface{}{"url": "https://i.ytimg.com/vi/yt-1/hqdefault.jpg"},
					},
				},
			},
			"tweets": []interface{}{
				map[string]interface{}{
					"tweet_id": "1900000000000000000",
					"text":     "AI Agent",
					"media": map[string]interface{}{
						"video": []interface{}{
							map[string]interface{}{"media_url_https": "https://pbs.twimg.com/media/example.jpg"},
						},
					},
				},
			},
		},
	}
	got := ExtractContentCoverURLs(response)
	if len(got) != 2 || got[0] != "https://i.ytimg.com/vi/yt-1/hqdefault.jpg" || got[1] != "https://pbs.twimg.com/media/example.jpg" {
		t.Fatalf("covers=%#v", got)
	}
	if !allowedCoverURL(mustCoverURL(t, "https://i.ytimg.com/vi/id/hqdefault.jpg")) {
		t.Fatal("YouTube thumbnail host rejected")
	}
	if !allowedCoverURL(mustCoverURL(t, "https://pbs.twimg.com/media/id.jpg")) {
		t.Fatal("X media host rejected")
	}
	if allowedCoverURL(mustCoverURL(t, "https://example.com/cover.jpg")) {
		t.Fatal("untrusted cover host accepted")
	}
}

func mustCoverURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
