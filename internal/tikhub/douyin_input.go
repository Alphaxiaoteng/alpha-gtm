package tikhub

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

const (
	DouyinVideoDetailEndpoint = "douyin.video_detail"
	DouyinVideoShareEndpoint  = "douyin.video_by_share_url"
	maxDouyinVideoInputRunes  = 4096
)

var (
	douyinVideoIDPattern = regexp.MustCompile(`^[0-9]{16,21}$`)
	douyinURLPattern     = regexp.MustCompile(`https?://[^\s]+`)
	douyinPathIDPattern  = regexp.MustCompile(`/(?:video|note|share/video)/([0-9]{16,21})(?:/|$)`)
)

type ParsedDouyinVideoInput struct {
	Kind          string         `json:"kind"`
	EndpointID    string         `json:"endpoint_id"`
	VideoID       string         `json:"video_id,omitempty"`
	ExtractedURL  string         `json:"extracted_url,omitempty"`
	NormalizedURL string         `json:"normalized_url,omitempty"`
	Params        map[string]any `json:"params"`
}

// ParseDouyinVideoInput accepts a pure video ID, canonical URL, short share
// URL, or a full copied Douyin share sentence. It never resolves the URL.
func ParseDouyinVideoInput(input string) (ParsedDouyinVideoInput, error) {
	text := strings.TrimSpace(input)
	if text == "" || len([]rune(text)) > maxDouyinVideoInputRunes {
		return ParsedDouyinVideoInput{}, fmt.Errorf("请输入有效的抖音视频 ID、链接或分享文本")
	}
	for _, char := range text {
		if unicode.IsControl(char) && char != '\n' && char != '\r' && char != '\t' {
			return ParsedDouyinVideoInput{}, fmt.Errorf("输入包含不支持的控制字符")
		}
	}
	if douyinVideoIDPattern.MatchString(text) {
		return parsedDouyinVideoID(text), nil
	}
	candidate := douyinURLPattern.FindString(text)
	candidate = strings.TrimRight(candidate, "，。！？；：、）】》)>]}'\"")
	if candidate == "" {
		return ParsedDouyinVideoInput{}, fmt.Errorf("没有找到有效的抖音视频 ID 或 HTTPS 链接")
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || (parsed.Port() != "" && parsed.Port() != "443") {
		return ParsedDouyinVideoInput{}, fmt.Errorf("只接受不含账号信息的 HTTPS 抖音链接")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !isAllowedDouyinHost(host) {
		return ParsedDouyinVideoInput{}, fmt.Errorf("只接受 douyin.com 或 iesdouyin.com 官方域名")
	}
	if videoID := strings.TrimSpace(parsed.Query().Get("modal_id")); douyinVideoIDPattern.MatchString(videoID) {
		return parsedDouyinVideoID(videoID), nil
	}
	if matches := douyinPathIDPattern.FindStringSubmatch(parsed.EscapedPath()); len(matches) == 2 {
		return parsedDouyinVideoID(matches[1]), nil
	}
	if host == "v.douyin.com" {
		normalized := "https://" + host + parsed.EscapedPath()
		return ParsedDouyinVideoInput{
			Kind: "share_url", EndpointID: DouyinVideoShareEndpoint,
			ExtractedURL: candidate, NormalizedURL: normalized,
			Params: map[string]any{"share_url": normalized},
		}, nil
	}
	return ParsedDouyinVideoInput{}, fmt.Errorf("该链接没有可提取的视频 ID；请使用视频页链接或 v.douyin.com 分享链接")
}

func parsedDouyinVideoID(videoID string) ParsedDouyinVideoInput {
	return ParsedDouyinVideoInput{
		Kind: "video_id", EndpointID: DouyinVideoDetailEndpoint, VideoID: videoID,
		NormalizedURL: "https://www.douyin.com/video/" + videoID,
		Params:        map[string]any{"aweme_id": videoID},
	}
}

func isAllowedDouyinHost(host string) bool {
	return host == "douyin.com" || strings.HasSuffix(host, ".douyin.com") ||
		host == "iesdouyin.com" || strings.HasSuffix(host, ".iesdouyin.com")
}
