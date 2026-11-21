package tikhub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxCoverBytes = 8 << 20

type CoverCacheResult struct {
	Discovered int `json:"discovered"`
	Cached     int `json:"cached"`
	Existing   int `json:"existing"`
	Failed     int `json:"failed"`
}

// ExtractContentCoverURLs finds content covers without collecting author
// avatars. For gallery posts, only the first image is the card cover.
func ExtractContentCoverURLs(response map[string]interface{}) []string {
	result := make([]string, 0, 32)
	seen := map[string]bool{}
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		parsed, err := url.Parse(candidate)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || seen[candidate] {
			return
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	var collectPreferredURL func(any, int)
	collectPreferredURL = func(value any, depth int) {
		if depth > 6 {
			return
		}
		switch typed := value.(type) {
		case string:
			add(typed)
		case []interface{}:
			if len(typed) > 0 {
				collectPreferredURL(typed[0], depth+1)
			}
		case map[string]interface{}:
			for _, key := range []string{"url", "url_default", "url_pre", "url_list", "src", "media_url_https", "preview_image_url"} {
				if child, ok := typed[key]; ok {
					before := len(result)
					collectPreferredURL(child, depth+1)
					if len(result) > before {
						return
					}
				}
			}
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child := typed[key]
				before := len(result)
				collectPreferredURL(child, depth+1)
				if len(result) > before {
					return
				}
			}
		}
	}
	var walk func(any, int)
	walk = func(value any, depth int) {
		if depth > 12 {
			return
		}
		switch typed := value.(type) {
		case []interface{}:
			for _, child := range typed {
				walk(child, depth+1)
			}
		case map[string]interface{}:
			if contentMapHasIdentity(typed) {
				if images, ok := typed["images_list"].([]interface{}); ok && len(images) > 0 {
					collectPreferredURL(images[0], 0)
				}
				if images, ok := typed["image_list"].([]interface{}); ok && len(images) > 0 {
					collectPreferredURL(images[0], 0)
				}
				for _, key := range []string{"cover", "cover_url", "cover_image", "thumbnail", "thumbnails", "poster", "media_url_https", "preview_image_url"} {
					if child, ok := typed[key]; ok {
						collectPreferredURL(child, 0)
					}
				}
				for _, key := range []string{"video", "aweme_info", "photo", "media", "legacy"} {
					if child, ok := typed[key]; ok {
						collectContentMediaCover(child, collectPreferredURL)
					}
				}
			}
			visited := map[string]bool{}
			for _, key := range []string{"business_data", "data", "items", "list", "videos", "aweme_list", "notes", "tweets"} {
				if child, ok := typed[key]; ok {
					visited[key] = true
					walk(child, depth+1)
				}
			}
			keys := make([]string, 0, len(typed))
			for key := range typed {
				if !visited[key] {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(typed[key], depth+1)
			}
		}
	}
	walk(response, 0)
	return result
}

func collectContentMediaCover(value any, collect func(any, int)) {
	if items, ok := value.([]interface{}); ok {
		for _, item := range items {
			collectContentMediaCover(item, collect)
		}
		return
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	for _, key := range []string{"cover", "origin_cover", "dynamic_cover", "thumbnail", "thumbnails", "poster", "media_url_https", "preview_image_url"} {
		if child, ok := object[key]; ok {
			collect(child, 0)
		}
	}
	if video, ok := object["video"]; ok {
		collectContentMediaCover(video, collect)
	}
	if photo, ok := object["photo"]; ok {
		collectContentMediaCover(photo, collect)
	}
	for _, key := range []string{"extended_entities", "entities", "media"} {
		switch child := object[key].(type) {
		case map[string]interface{}:
			collectContentMediaCover(child, collect)
		case []interface{}:
			for _, item := range child {
				collectContentMediaCover(item, collect)
			}
		}
	}
}

func contentMapHasIdentity(value map[string]interface{}) bool {
	for _, key := range []string{
		"id", "note_id", "aweme_id", "photo_id", "object_id", "video_id", "rest_id", "tweet_id",
		"title", "desc", "description", "description_snippet", "display_title", "full_text", "text",
	} {
		if item, ok := value[key]; ok && strings.TrimSpace(fmt.Sprint(item)) != "" {
			return true
		}
	}
	return false
}

func CachedCoverName(sourceURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceURL)))
	return hex.EncodeToString(sum[:]) + ".img"
}

func CachedCoverPath(dataDir, sourceURL string) string {
	return filepath.Join(dataDir, "tikhub", "covers", CachedCoverName(sourceURL))
}

// CacheContentCovers is an explicit-call side effect. There is no scheduler and
// no retention task; cached covers remain in the local data directory.
func CacheContentCovers(ctx context.Context, dataDir string, response map[string]interface{}) CoverCacheResult {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !allowedCoverURL(request.URL) {
				return fmt.Errorf("cover redirect host is not allowed")
			}
			return nil
		},
	}
	return cacheContentCovers(ctx, dataDir, response, client, allowedCoverURL)
}

func cacheContentCovers(
	ctx context.Context,
	dataDir string,
	response map[string]interface{},
	client *http.Client,
	allow func(*url.URL) bool,
) CoverCacheResult {
	urls := ExtractContentCoverURLs(response)
	result := CoverCacheResult{Discovered: len(urls)}
	if dataDir == "" || len(urls) == 0 {
		return result
	}
	dir := filepath.Join(dataDir, "tikhub", "covers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		result.Failed = len(urls)
		return result
	}
	_ = os.Chmod(dir, 0o700)

	var mu sync.Mutex
	var group sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, sourceURL := range urls {
		sourceURL := sourceURL
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				result.Failed++
				mu.Unlock()
				return
			}
			target := CachedCoverPath(dataDir, sourceURL)
			if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				mu.Lock()
				result.Existing++
				mu.Unlock()
				return
			}
			if err := downloadCover(ctx, client, allow, sourceURL, dir, target); err != nil {
				mu.Lock()
				result.Failed++
				mu.Unlock()
				return
			}
			mu.Lock()
			result.Cached++
			mu.Unlock()
		}()
	}
	group.Wait()
	return result
}

func downloadCover(ctx context.Context, client *http.Client, allow func(*url.URL) bool, sourceURL, dir, target string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil || !allow(parsed) {
		return fmt.Errorf("cover URL is not allowed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("cover returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxCoverBytes+1))
	if err != nil {
		return err
	}
	if len(raw) == 0 || len(raw) > maxCoverBytes {
		return fmt.Errorf("cover size is invalid")
	}
	contentType := http.DetectContentType(raw)
	if !allowedImageType(contentType) {
		return fmt.Errorf("cover content type %q is not allowed", contentType)
	}
	temp, err := os.CreateTemp(dir, ".cover-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, target)
}

func allowedCoverURL(value *url.URL) bool {
	if value == nil || value.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(value.Hostname(), "."))
	for _, suffix := range []string{
		".xhscdn.com", ".douyincdn.com", ".douyinpic.com", ".byteimg.com", ".kscdn.com",
		".qpic.cn", ".qlogo.cn", ".weixin.qq.com",
		".ytimg.com", ".ggpht.com", ".googleusercontent.com", ".twimg.com",
	} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func allowedImageType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "image/avif":
		return true
	default:
		return false
	}
}
