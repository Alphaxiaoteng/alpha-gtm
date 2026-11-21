//go:build legacy_chromium

package publishverify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/url"
	"strings"
	"time"
)

// ErrCreatorBackendDOMUnverified is returned until a platform-specific
// observer has been reviewed against that platform's real content manager.
// A fixture proves matching behavior only; it never makes a DOM adapter live.
var ErrCreatorBackendDOMUnverified = errors.New("creator-backend DOM adapter is not verified")

type CreatorBackendExpectation struct {
	JobID            int64
	AccountID        int64
	AssetID          int64
	PlatformCode     string
	ContentType      string
	TargetName       string
	TargetExternalID string
	Title            string
	Caption          string
	Body             string
	MediaSHA256      []string
	FrozenMedia      []CreatorBackendMedia
	SubmitTargetID   string
	SubmittedAt      time.Time
	PublishedBefore  time.Time
}

// CreatorBackendMedia is the immutable local-media portion needed by
// platform observers that must compare a rendered backend thumbnail with the
// frozen source. Paths never leave the local verifier.
type CreatorBackendMedia struct {
	Kind     string
	Role     string
	FilePath string
	SHA256   string
}

type CreatorBackendItem struct {
	ContentType     string `json:"content_type,omitempty"`
	Status          string `json:"status"`
	Title           string `json:"title"`
	Caption         string `json:"caption"`
	Body            string `json:"body,omitempty"`
	ContentSHA256   string `json:"content_sha256,omitempty"`
	MediaSHA256     string `json:"media_sha256"`
	ThumbnailSHA256 string `json:"thumbnail_sha256"`
	PublishedAt     string `json:"published_at"`
	PostURL         string `json:"post_url"`
	ExternalPostID  string `json:"external_post_id"`
}

type CreatorBackendSnapshot struct {
	PlatformCode     string               `json:"platform_code,omitempty"`
	ManagerURL       string               `json:"manager_url"`
	TargetName       string               `json:"target_name"`
	TargetExternalID string               `json:"target_external_id"`
	Items            []CreatorBackendItem `json:"items"`
	ScreenshotPNG    []byte               `json:"-"`
}

// CreatorBackendObserver is deliberately only a data boundary. A platform
// must supply a separately reviewed live DOM adapter before Observe may return
// a snapshot. Unknown DOM remains fail-closed.
type CreatorBackendObserver interface {
	Observe(context.Context, string, CreatorBackendExpectation) (CreatorBackendSnapshot, error)
}

// UnverifiedCreatorBackendObserver is the safe default for every platform
// without a reviewed content-manager DOM adapter.
type UnverifiedCreatorBackendObserver struct {
	PlatformCode string
}

func (o UnverifiedCreatorBackendObserver) Observe(_ context.Context, _ string, expected CreatorBackendExpectation) (CreatorBackendSnapshot, error) {
	code := strings.TrimSpace(o.PlatformCode)
	if code == "" {
		code = strings.TrimSpace(expected.PlatformCode)
	}
	if _, ok := creatorBackendPlatformContracts()[code]; !ok {
		return CreatorBackendSnapshot{}, fmt.Errorf("%w: unsupported platform %q", ErrCreatorBackendDOMUnverified, code)
	}
	if expected.PlatformCode != "" && expected.PlatformCode != code {
		return CreatorBackendSnapshot{}, fmt.Errorf("%w: expected platform %q does not match observer %q", ErrCreatorBackendDOMUnverified, expected.PlatformCode, code)
	}
	return CreatorBackendSnapshot{}, fmt.Errorf("%w: %s", ErrCreatorBackendDOMUnverified, code)
}

type creatorBackendPlatformContract struct {
	managerHosts map[string]struct{}
	publicHosts  map[string]struct{}
	contentTypes map[string]struct{}
}

// creatorBackendPlatformContracts is intentionally the same ten-platform
// scope as the frozen αgamer launch matrix. It is evidence validation, not a
// claim that any platform has a live DOM observer.
func creatorBackendPlatformContracts() map[string]creatorBackendPlatformContract {
	set := func(values ...string) map[string]struct{} {
		result := make(map[string]struct{}, len(values))
		for _, value := range values {
			result[value] = struct{}{}
		}
		return result
	}
	return map[string]creatorBackendPlatformContract{
		"instagram": {
			managerHosts: set("business.facebook.com"),
			publicHosts:  set("instagram.com", "www.instagram.com"),
			contentTypes: set("graph_text", "video"),
		},
		"facebook": {
			managerHosts: set("business.facebook.com"),
			publicHosts:  set("facebook.com", "www.facebook.com"),
			contentTypes: set("graph_text", "video"),
		},
		"xhs": {
			managerHosts: set("creator.xiaohongshu.com"),
			publicHosts:  set("xiaohongshu.com", "www.xiaohongshu.com"),
			contentTypes: set("graph_text", "video"),
		},
		"douyin": {
			managerHosts: set("creator.douyin.com"),
			publicHosts:  set("douyin.com", "www.douyin.com"),
			contentTypes: set("graph_text", "video"),
		},
		"kuaishou": {
			managerHosts: set("cp.kuaishou.com"),
			publicHosts:  set("kuaishou.com", "www.kuaishou.com"),
			contentTypes: set("graph_text", "video"),
		},
		"wechat_video": {
			managerHosts: set("channels.weixin.qq.com"),
			publicHosts:  set("channels.weixin.qq.com"),
			contentTypes: set("video"),
		},
		"wechat_mp": {
			managerHosts: set("mp.weixin.qq.com"),
			publicHosts:  set("mp.weixin.qq.com"),
			contentTypes: set("article"),
		},
		"csdn": {
			managerHosts: set("mp.csdn.net"),
			publicHosts:  set("blog.csdn.net"),
			contentTypes: set("article"),
		},
		"youtube": {
			managerHosts: set("studio.youtube.com"),
			publicHosts:  set("youtube.com", "www.youtube.com", "youtu.be"),
			contentTypes: set("video"),
		},
		"tiktok": {
			managerHosts: set("tiktok.com", "www.tiktok.com"),
			publicHosts:  set("tiktok.com", "www.tiktok.com"),
			contentTypes: set("graph_text", "video"),
		},
	}
}

// SupportsCreatorBackendPlatform reports whether the frozen launch contract
// has a bounded creator-manager and public-link contract for this platform.
// It does not claim that a reviewed live DOM observer is available.
func SupportsCreatorBackendPlatform(platformCode string) bool {
	_, ok := creatorBackendPlatformContracts()[strings.TrimSpace(platformCode)]
	return ok
}

// ParseCreatorBackendSnapshot parses only a platform-explicit, bounded
// observation produced by a reviewed adapter. It does not inspect broad page
// text and it rejects cross-platform snapshots.
func ParseCreatorBackendSnapshot(platformCode string, raw, screenshotPNG []byte) (CreatorBackendSnapshot, error) {
	return parseCreatorBackendSnapshot(platformCode, raw, screenshotPNG, false)
}

func parseCreatorBackendSnapshot(platformCode string, raw, screenshotPNG []byte, allowLegacyInstagram bool) (CreatorBackendSnapshot, error) {
	platformCode = strings.TrimSpace(platformCode)
	contract, ok := creatorBackendPlatformContracts()[platformCode]
	if !ok {
		return CreatorBackendSnapshot{}, fmt.Errorf("creator backend platform %q is unsupported", platformCode)
	}
	if len(raw) == 0 || len(raw) > 1<<20 {
		return CreatorBackendSnapshot{}, fmt.Errorf("%s backend snapshot must be 1 byte to 1 MiB", platformCode)
	}
	var snapshot CreatorBackendSnapshot
	if allowLegacyInstagram {
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return CreatorBackendSnapshot{}, fmt.Errorf("decode %s backend snapshot: %w", platformCode, err)
		}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&snapshot); err != nil {
			return CreatorBackendSnapshot{}, fmt.Errorf("decode %s backend snapshot: %w", platformCode, err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return CreatorBackendSnapshot{}, fmt.Errorf("decode %s backend snapshot: trailing data", platformCode)
		}
	}
	if snapshot.PlatformCode == "" && allowLegacyInstagram && platformCode == "instagram" {
		snapshot.PlatformCode = "instagram"
	}
	if snapshot.PlatformCode != platformCode {
		return CreatorBackendSnapshot{}, fmt.Errorf("%s backend snapshot platform does not match", platformCode)
	}
	if !validCreatorManagerURL(contract, snapshot.ManagerURL) {
		return CreatorBackendSnapshot{}, fmt.Errorf("%s backend snapshot manager URL is invalid", platformCode)
	}
	if strings.TrimSpace(snapshot.TargetName) == "" || strings.TrimSpace(snapshot.TargetExternalID) == "" {
		return CreatorBackendSnapshot{}, fmt.Errorf("%s backend snapshot target identity is required", platformCode)
	}
	if len(snapshot.Items) > 100 {
		return CreatorBackendSnapshot{}, fmt.Errorf("%s backend snapshot has too many items", platformCode)
	}
	for index := range snapshot.Items {
		item := &snapshot.Items[index]
		if item.ContentType == "" && allowLegacyInstagram && platformCode == "instagram" {
			// Legacy Instagram snapshots did not carry a content type. The
			// compatibility matcher binds it to the frozen expectation.
		} else if _, ok := contract.contentTypes[item.ContentType]; !ok {
			return CreatorBackendSnapshot{}, fmt.Errorf("%s backend item content type is invalid", platformCode)
		}
		if strings.TrimSpace(item.Status) == "" {
			return CreatorBackendSnapshot{}, fmt.Errorf("%s backend item status is required", platformCode)
		}
		if item.PublishedAt != "" {
			if _, err := time.Parse(time.RFC3339, item.PublishedAt); err != nil {
				return CreatorBackendSnapshot{}, fmt.Errorf("%s backend item published_at is invalid", platformCode)
			}
		}
		for _, digest := range []string{item.ContentSHA256, item.MediaSHA256, item.ThumbnailSHA256} {
			if digest != "" && !validSHA256(digest) {
				return CreatorBackendSnapshot{}, fmt.Errorf("%s backend item hash is invalid", platformCode)
			}
		}
	}
	if !validPNG(screenshotPNG) {
		return CreatorBackendSnapshot{}, fmt.Errorf("%s backend screenshot must be PNG", platformCode)
	}
	snapshot.ScreenshotPNG = append([]byte(nil), screenshotPNG...)
	return snapshot, nil
}

// MatchCreatorBackend requires a single platform/content form, exact target,
// exact title/body fingerprint, reviewed media hash, bounded publish time,
// explicit Published status, stable public identity, and PNG evidence.
func MatchCreatorBackend(expected CreatorBackendExpectation, snapshot CreatorBackendSnapshot) (CreatorBackendItem, bool) {
	contract, ok := creatorBackendPlatformContracts()[expected.PlatformCode]
	if !ok || snapshot.PlatformCode != expected.PlatformCode {
		return CreatorBackendItem{}, false
	}
	if _, ok := contract.contentTypes[expected.ContentType]; !ok ||
		!validCreatorManagerURL(contract, snapshot.ManagerURL) ||
		!sameText(snapshot.TargetName, expected.TargetName) ||
		strings.TrimSpace(snapshot.TargetExternalID) != strings.TrimSpace(expected.TargetExternalID) ||
		!validPNG(snapshot.ScreenshotPNG) {
		return CreatorBackendItem{}, false
	}
	expectedHashes := make(map[string]struct{}, len(expected.MediaSHA256))
	for _, value := range expected.MediaSHA256 {
		value = strings.ToLower(strings.TrimSpace(value))
		if validSHA256(value) {
			expectedHashes[value] = struct{}{}
		}
	}
	if len(expectedHashes) == 0 || !validCreatorContentExpectation(expected) {
		return CreatorBackendItem{}, false
	}
	for _, item := range snapshot.Items {
		if item.ContentType != expected.ContentType ||
			!explicitPublishedStatus(item.Status) ||
			!matchingCreatorBackendContent(expected, item) ||
			!matchingCreatorBackendMedia(expectedHashes, item) ||
			!withinCreatorBackendPublishWindow(expected, item.PublishedAt) ||
			!validCreatorPublicPostIdentity(expected.PlatformCode, contract, item.PostURL, item.ExternalPostID) {
			continue
		}
		return item, true
	}
	return CreatorBackendItem{}, false
}

func validCreatorContentExpectation(expected CreatorBackendExpectation) bool {
	return normalizeText(expected.Title) != "" || normalizeText(expected.Caption) != "" || normalizeText(expected.Body) != ""
}

func matchingCreatorBackendContent(expected CreatorBackendExpectation, item CreatorBackendItem) bool {
	if normalizeText(item.Title) != "" && normalizeText(expected.Title) != "" && !sameText(expected.Title, item.Title) {
		return false
	}
	hasObservedBody := normalizeText(item.Caption) != "" || normalizeText(item.Body) != ""
	for _, pair := range [][2]string{
		{expected.Caption, item.Caption},
		{expected.Caption, item.Body},
		{expected.Body, item.Body},
		{expected.Body, item.Caption},
	} {
		if sameText(pair[0], pair[1]) {
			return true
		}
	}
	contentHash := strings.ToLower(strings.TrimSpace(item.ContentSHA256))
	if validSHA256(contentHash) {
		for _, value := range []string{expected.Title, expected.Caption, expected.Body} {
			if normalizeText(value) != "" && normalizedTextSHA256(value) == contentHash {
				return true
			}
		}
	}
	// Some content managers expose only a title. Once any body text or body
	// fingerprint is exposed it must match and cannot fall back to title-only.
	return !hasObservedBody && contentHash == "" && sameText(expected.Title, item.Title)
}

func matchingCreatorBackendMedia(expected map[string]struct{}, item CreatorBackendItem) bool {
	for _, value := range []string{item.MediaSHA256, item.ThumbnailSHA256} {
		if _, ok := expected[strings.ToLower(strings.TrimSpace(value))]; ok {
			return true
		}
	}
	return false
}

func withinCreatorBackendPublishWindow(expected CreatorBackendExpectation, raw string) bool {
	publishedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil || expected.SubmittedAt.IsZero() || expected.PublishedBefore.IsZero() {
		return false
	}
	notBefore := expected.SubmittedAt.Add(-2 * time.Minute)
	return !publishedAt.Before(notBefore) && !publishedAt.After(expected.PublishedBefore)
}

func explicitPublishedStatus(value string) bool {
	value = strings.TrimSpace(value)
	return value == "Published" || value == "Public" || value == "已发布" || value == "已发表"
}

func validCreatorManagerURL(contract creatorBackendPlatformContract, raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Hostname() == "" || parsed.Port() != "" || parsed.Fragment != "" {
		return false
	}
	_, ok := contract.managerHosts[strings.ToLower(parsed.Hostname())]
	return ok
}

func validCreatorPublicPostIdentity(platformCode string, contract creatorBackendPlatformContract, rawURL, externalPostID string) bool {
	externalPostID = strings.Trim(strings.TrimSpace(externalPostID), "/")
	if externalPostID == "" || len([]rune(externalPostID)) > 256 || strings.ContainsAny(externalPostID, "\r\n") {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if _, ok := contract.publicHosts[strings.ToLower(parsed.Hostname())]; !ok {
		return false
	}
	rawSegments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	segments := make([]string, 0, len(rawSegments))
	for _, rawSegment := range rawSegments {
		segment, err := url.PathUnescape(rawSegment)
		if err != nil {
			return false
		}
		segments = append(segments, segment)
	}
	if platformCode == "instagram" {
		return len(segments) == 2 &&
			(segments[0] == "p" || segments[0] == "reel") &&
			segments[1] == externalPostID
	}
	for _, segment := range segments {
		if segment == externalPostID {
			return true
		}
	}
	return false
}

func sameText(left, right string) bool {
	return normalizeText(left) != "" && normalizeText(left) == normalizeText(right)
}

func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizedTextSHA256(value string) string {
	digest := sha256.Sum256([]byte(normalizeText(value)))
	return hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPNG(value []byte) bool {
	if len(value) <= 8 || !bytes.Equal(value[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return false
	}
	_, err := png.DecodeConfig(bytes.NewReader(value))
	return err == nil
}
