package tikhub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// CreatorMediaOptions controls one media archival pass.
type CreatorMediaOptions struct {
	// Workers bounds concurrent downloads. Values <= 0 fall back to
	// DefaultCreatorMediaWorkers.
	Workers int
	Covers  bool
	Audio   bool
	// OnProgress receives (completed, total) after each item settles.
	OnProgress func(completed, total int)
}

// DefaultCreatorMediaWorkers keeps a creator sync fast without opening so many
// sockets that CDNs start throttling: covers and audio are separate passes.
const DefaultCreatorMediaWorkers = 8

// CreatorMediaResult summarizes one archival pass. Total counts download jobs
// (covers + audio), not posts, so Total == Covers + Audio + Skipped + Failed.
type CreatorMediaResult struct {
	Covers  int `json:"covers"`
	Audio   int `json:"audio"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
	Total   int `json:"total"`
	// Failures lists the assets that could not be archived, so callers can see
	// which posts still need attention instead of guessing from a count.
	Failures []CreatorMediaFailure `json:"failures,omitempty"`
}

// CreatorMediaFailure records one asset that failed to download.
type CreatorMediaFailure struct {
	AwemeID string `json:"aweme_id"`
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
}

// CreatorPostMedia is one post plus the media files archived beside it.
// Paths are relative to the creator folder so the folder stays movable.
type CreatorPostMedia struct {
	AwemeID    string `json:"aweme_id"`
	Title      string `json:"title,omitempty"`
	CreateTime int64  `json:"create_time,omitempty"`
	CoverURL   string `json:"cover_url,omitempty"`
	AudioURL   string `json:"audio_url,omitempty"`
	CoverPath  string `json:"cover_path,omitempty"`
	AudioPath  string `json:"audio_path,omitempty"`
	Play       int64  `json:"play,omitempty"`
	Likes      int64  `json:"likes,omitempty"`
	Comments   int64  `json:"comments,omitempty"`
	Collects   int64  `json:"collects,omitempty"`
	Shares     int64  `json:"shares,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// creatorPostList digs out the aweme list from a douyin.creator_posts response.
func creatorPostList(response map[string]interface{}) []map[string]interface{} {
	data, _ := response["data"].(map[string]interface{})
	if data == nil {
		return nil
	}
	raw, ok := data["aweme_list"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]interface{}); ok {
			out = append(out, entry)
		}
	}
	return out
}

func mapField(value interface{}, key string) map[string]interface{} {
	container, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	child, _ := container[key].(map[string]interface{})
	return child
}

// stringListField reads a nested {"<key>":{"url_list":[...]}} string list.
func stringListField(value interface{}, key string) []string {
	holder := mapField(value, key)
	if holder == nil {
		return nil
	}
	raw, ok := holder["url_list"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}

// pickImageURL prefers a widely viewable format. Douyin lists the HEIC variant
// first on most covers, but HEIC will not render in most browsers or editors, so
// the JPEG/PNG sibling is chosen when present.
func pickImageURL(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	for _, candidate := range candidates {
		if imageExtension(candidate) == ".jpg" || imageExtension(candidate) == ".png" || imageExtension(candidate) == ".webp" {
			return candidate
		}
	}
	return candidates[0]
}

func imageExtension(raw string) string {
	path := raw
	if index := strings.IndexAny(path, "?#"); index >= 0 {
		path = path[:index]
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpeg":
		return ".jpg"
	default:
		return ext
	}
}

// ExtractCreatorPostMedia returns one media record per post in a
// douyin.creator_posts response, preserving API order.
func ExtractCreatorPostMedia(response map[string]interface{}) []CreatorPostMedia {
	posts := creatorPostList(response)
	if len(posts) == 0 {
		return nil
	}
	out := make([]CreatorPostMedia, 0, len(posts))
	for _, post := range posts {
		awemeID := strings.TrimSpace(fmt.Sprint(post["aweme_id"]))
		if awemeID == "" || awemeID == "<nil>" {
			continue
		}
		video := post["video"]
		item := CreatorPostMedia{
			AwemeID: awemeID,
			Title:   strings.TrimSpace(fmt.Sprint(post["desc"])),
			CoverURL: pickImageURL(firstNonEmpty(
				stringListField(video, "cover"),
				stringListField(video, "origin_cover"),
				stringListField(video, "dynamic_cover"),
			)),
			AudioURL: firstURL(stringListField(post["music"], "play_url")),
		}
		if item.Title == "<nil>" {
			item.Title = ""
		}
		if raw, ok := post["create_time"]; ok {
			item.CreateTime = toInt64(raw)
		}
		if stats, ok := post["statistics"].(map[string]interface{}); ok {
			item.Play = toInt64(stats["play_count"])
			item.Likes = toInt64(stats["digg_count"])
			item.Comments = toInt64(stats["comment_count"])
			item.Collects = toInt64(stats["collect_count"])
			item.Shares = toInt64(stats["share_count"])
		}
		if videoMap, ok := post["video"].(map[string]interface{}); ok {
			item.DurationMS = toInt64(videoMap["duration"])
		}
		out = append(out, item)
	}
	return out
}

func firstNonEmpty(lists ...[]string) []string {
	for _, list := range lists {
		if len(list) > 0 {
			return list
		}
	}
	return nil
}

func firstURL(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

func toInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case json.Number:
		// Client.do decodes with UseNumber, so every numeric field arrives as
		// json.Number. Without this branch each counter fell into default and
		// was archived as 0, which looks like a real metric instead of a miss.
		if parsed, err := typed.Int64(); err == nil {
			return parsed
		}
		if parsed, err := typed.Float64(); err == nil {
			return int64(parsed)
		}
		return 0
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		var parsed int64
		_, _ = fmt.Sscan(typed, &parsed)
		return parsed
	default:
		return 0
	}
}

// allowedMediaURL extends the cover allowlist with the music CDN hosts Douyin
// uses for original-sound audio.
func allowedMediaURL(value *url.URL) bool {
	if allowedCoverURL(value) {
		return true
	}
	if value == nil || value.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(value.Hostname(), "."))
	for _, suffix := range []string{".douyinstatic.com", ".amemv.com"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	// Douyin bounces some audio to a load balancer that reports the origin it
	// proxied via a base64 real_host query parameter. Trusting the whole CDN
	// domain would widen the allowlist, so the redirect is accepted only when it
	// declares an origin we already trust.
	return isVerifiedCDNRedirect(value, host)
}

// cdnRedirectHosts are the load-balancing domains Douyin uses for media
// redirects. Membership alone is never sufficient; the real_host claim must
// also resolve to an already-allowed host.
var cdnRedirectHosts = []string{".bsgslb.cn"}

func isVerifiedCDNRedirect(value *url.URL, host string) bool {
	matched := false
	for _, suffix := range cdnRedirectHosts {
		if strings.HasSuffix(host, suffix) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	claimed := strings.TrimSpace(value.Query().Get("real_host"))
	if claimed == "" {
		return false
	}
	decoded, ok := decodeBase64Loose(claimed)
	if !ok {
		return false
	}
	origin := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(string(decoded)), "."))
	if origin == "" || strings.ContainsAny(origin, "/:@ ") {
		return false
	}
	return allowedCoverURL(&url.URL{Scheme: "https", Host: origin}) ||
		strings.HasSuffix(origin, ".douyinstatic.com") ||
		strings.HasSuffix(origin, ".amemv.com")
}

// decodeBase64Loose accepts the padded/unpadded and standard/URL-safe variants
// that CDNs mix freely.
func decodeBase64Loose(value string) ([]byte, bool) {
	for _, encoding := range []*base64.Encoding{
		base64.URLEncoding, base64.RawURLEncoding, base64.StdEncoding, base64.RawStdEncoding,
	} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, true
		}
	}
	return nil, false
}

// CreatorMediaDir returns the folder that holds downloaded media for one creator.
func CreatorMediaDir(dataDir, platform, userID string) string {
	return filepath.Join(CreatorFolder(dataDir, platform, userID), "media")
}

// mediaTargetPath builds "<dir>/<awemeID><ext>", falling back to a stable name
// when the CDN URL carries no usable extension.
func mediaTargetPath(dir, awemeID, sourceURL, fallbackExt string) string {
	ext := imageExtension(sourceURL)
	if fallbackExt == ".mp3" {
		ext = filepath.Ext(sourceURL)
		if index := strings.IndexAny(ext, "?#"); index >= 0 {
			ext = ext[:index]
		}
		ext = strings.ToLower(ext)
		if ext != ".mp3" && ext != ".m4a" && ext != ".aac" && ext != ".wav" {
			ext = fallbackExt
		}
	}
	if ext == "" {
		ext = fallbackExt
	}
	return filepath.Join(dir, safeCreatorSegment(awemeID)+ext)
}

// DownloadCreatorMedia archives covers and audio for the given posts into the
// creator folder. Existing files are reused, so re-running is cheap and the
// caller can treat this as "update".
func DownloadCreatorMedia(
	ctx context.Context,
	dataDir, platform, userID string,
	items []CreatorPostMedia,
	options CreatorMediaOptions,
) (CreatorMediaResult, error) {
	return downloadCreatorMedia(ctx, dataDir, platform, userID, items, options, newMediaClient(), allowedMediaURL)
}

func downloadCreatorMedia(
	ctx context.Context,
	dataDir, platform, userID string,
	items []CreatorPostMedia,
	options CreatorMediaOptions,
	client *http.Client,
	allow func(*url.URL) bool,
) (CreatorMediaResult, error) {
	result := CreatorMediaResult{Total: len(items)}
	if len(items) == 0 || dataDir == "" {
		return result, nil
	}
	if !options.Covers && !options.Audio {
		options.Covers = true
		options.Audio = true
	}
	workers := options.Workers
	if workers <= 0 {
		workers = DefaultCreatorMediaWorkers
	}
	root := CreatorMediaDir(dataDir, platform, userID)
	coverDir := filepath.Join(root, "covers")
	audioDir := filepath.Join(root, "audio")
	for _, dir := range []string{coverDir, audioDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return result, err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return result, err
		}
	}

	type job struct {
		sourceURL string
		target    string
		kind      string
		itemIndex int
	}
	jobs := make([]job, 0, len(items)*2)
	for index := range items {
		if options.Covers && items[index].CoverURL != "" {
			jobs = append(jobs, job{
				sourceURL: items[index].CoverURL,
				target:    mediaTargetPath(coverDir, items[index].AwemeID, items[index].CoverURL, ".jpg"),
				kind:      "cover",
				itemIndex: index,
			})
		}
		if options.Audio && items[index].AudioURL != "" {
			jobs = append(jobs, job{
				sourceURL: items[index].AudioURL,
				target:    mediaTargetPath(audioDir, items[index].AwemeID, items[index].AudioURL, ".mp3"),
				kind:      "audio",
				itemIndex: index,
			})
		}
	}

	var mu sync.Mutex
	var group sync.WaitGroup
	sem := make(chan struct{}, workers)
	completed := 0
	result.Total = len(jobs)

	for _, current := range jobs {
		current := current
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				result.Failed++
				result.Failures = append(result.Failures, CreatorMediaFailure{
					AwemeID: items[current.itemIndex].AwemeID,
					Kind:    current.kind,
					Reason:  "cancelled",
				})
				mu.Unlock()
				return
			}

			settle := func(outcome string, reason string) {
				mu.Lock()
				completed++
				switch outcome {
				case "cover":
					result.Covers++
				case "audio":
					result.Audio++
				case "skip":
					result.Skipped++
				default:
					result.Failed++
					if reason != "" {
						result.Failures = append(result.Failures, CreatorMediaFailure{
							AwemeID: items[current.itemIndex].AwemeID,
							Kind:    current.kind,
							Reason:  reason,
						})
					}
				}
				if options.OnProgress != nil {
					options.OnProgress(completed, len(jobs))
				}
				mu.Unlock()
			}

			if info, err := os.Stat(current.target); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				settle("skip", "")
				return
			}
			if err := downloadMediaFile(ctx, client, allow, current.sourceURL, filepath.Dir(current.target), current.target, current.kind); err != nil {
				settle("fail", err.Error())
				return
			}
			settle(current.kind, "")
		}()
	}
	group.Wait()

	// A cancelled or expired context left the loop above with the jobs that
	// settled before the deadline; the ones that never ran are absent from the
	// counters. Returning nil here made a partial archive look complete, so the
	// cancellation is handed back to the caller after the local paths below are
	// recorded.
	interrupted := ctx.Err()

	// Record the local paths so the folder is self-describing.
	mu.Lock()
	for index := range items {
		coverTarget := mediaTargetPath(coverDir, items[index].AwemeID, items[index].CoverURL, ".jpg")
		if _, err := os.Stat(coverTarget); err == nil {
			items[index].CoverPath = relativeToCreatorFolder(dataDir, platform, userID, coverTarget)
		}
		audioTarget := mediaTargetPath(audioDir, items[index].AwemeID, items[index].AudioURL, ".mp3")
		if _, err := os.Stat(audioTarget); err == nil {
			items[index].AudioPath = relativeToCreatorFolder(dataDir, platform, userID, audioTarget)
		}
	}
	mu.Unlock()
	if interrupted != nil {
		return result, interrupted
	}
	return result, nil
}

func relativeToCreatorFolder(dataDir, platform, userID, target string) string {
	root := CreatorFolder(dataDir, platform, userID)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}

// SortCreatorMediaNewestFirst orders posts by create time, newest first, with a
// stable tiebreak so repeated runs produce identical indexes.
func SortCreatorMediaNewestFirst(items []CreatorPostMedia) {
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].CreateTime != items[right].CreateTime {
			return items[left].CreateTime > items[right].CreateTime
		}
		return items[left].AwemeID > items[right].AwemeID
	})
}

// CreatorPostsPaging reports the next cursor and whether more pages exist.
// Douyin returns has_more as a number and max_cursor as a large integer, so both
// are read defensively rather than assumed.
func CreatorPostsPaging(response map[string]interface{}) (string, bool) {
	data, _ := response["data"].(map[string]interface{})
	if data == nil {
		return "", false
	}
	if !hasMoreFlag(data["has_more"]) {
		return "", false
	}
	cursor := cursorString(data["max_cursor"])
	if cursor == "" || cursor == "0" {
		return "", false
	}
	return cursor, true
}

// CreatorPostsHasMore reports whether a creator_posts response says more pages
// exist, for the platforms whose paging shape differs: douyin carries a
// page-level has_more under data, xhs carries it under data.data. It answers
// only that question, so a caller that cannot build the next request still
// stops with the truth instead of claiming the list ended.
func CreatorPostsHasMore(response map[string]interface{}, platform string) bool {
	data, _ := response["data"].(map[string]interface{})
	if data == nil {
		return false
	}
	if platform == "xhs" {
		inner, _ := data["data"].(map[string]interface{})
		if inner == nil {
			return false
		}
		return hasMoreFlag(inner["has_more"])
	}
	return hasMoreFlag(data["has_more"])
}

// hasMoreFlag reads the many shapes has_more arrives in: a bool, a 0/1 number,
// or the string form of either.
func hasMoreFlag(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case int64:
		return typed != 0
	case json.Number:
		return typed.String() != "0"
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	default:
		return false
	}
}

// cursorString renders a cursor without scientific notation. Douyin cursors are
// millisecond timestamps (~1.79e12), and fmt.Sprint on a float64 would emit
// "1.789638008e+12", which the API rejects and which silently breaks paging.
func cursorString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if typed != typed || typed == 0 {
			return ""
		}
		return strconv.FormatInt(int64(typed), 10)
	case float32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case string:
		text := strings.TrimSpace(typed)
		// Tolerate a numeric string that arrived in exponent form.
		if parsed, err := strconv.ParseFloat(text, 64); err == nil {
			return strconv.FormatInt(int64(parsed), 10)
		}
		return text
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "<nil>" {
			return ""
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil {
			return strconv.FormatInt(int64(parsed), 10)
		}
		return text
	}
}

// ApplyWatchDefaultsForSync fills unset watch options so a freshly added creator
// can be archived without the caller pre-populating every field.
func ApplyWatchDefaultsForSync(user *WatchUser) {
	applyWatchDefaults(user)
}
