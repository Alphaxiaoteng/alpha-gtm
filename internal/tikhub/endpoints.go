package tikhub

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const officialOpenAPIURL = "https://api.tikhub.io/"

// EndpointParameter is the small, fixed form schema exposed by the local
// workbench. It deliberately models only the parameters useful to Nexus.
type EndpointParameter struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
}

// EndpointDefinition is an allowlisted TikHub operation. Callers choose an ID;
// they never provide a URL or HTTP method.
type EndpointDefinition struct {
	ID          string              `json:"id"`
	Platform    string              `json:"platform"`
	Name        string              `json:"name"`
	Capability  string              `json:"capability"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	DocsURL     string              `json:"docs_url"`
	CostHint    string              `json:"cost_hint,omitempty"`
	FixedParams map[string]any      `json:"fixed_params,omitempty"`
	Params      []EndpointParameter `json:"params,omitempty"`
}

func stringParam(name, label string, required bool, placeholder string) EndpointParameter {
	return EndpointParameter{Name: name, Label: label, Type: "string", Required: required, Placeholder: placeholder}
}

func selectParam(name, label, defaultValue, help string, options ...string) EndpointParameter {
	return EndpointParameter{
		Name: name, Label: label, Type: "select", Default: defaultValue,
		Options: options, Help: help,
	}
}

// CuratedEndpoints contains only the official operations used by creator and
// niche monitoring. ponytail: add an operation only when a product workflow
// actually needs it.
func CuratedEndpoints() []EndpointDefinition {
	return []EndpointDefinition{
		{
			ID: "douyin.video_search", Platform: "douyin", Name: "视频关键词搜索 V2", Capability: "keyword_search",
			Method: http.MethodPost, Path: "/api/v1/douyin/search/fetch_video_search_v2", DocsURL: "https://docs.tikhub.io/370212780e0",
			CostHint: "官方基础价 $0.01 / 次（2026-07-25 查询）",
			FixedParams: map[string]any{
				"cursor": 0, "search_id": "", "backtrace": "",
			},
			Params: []EndpointParameter{
				stringParam("keyword", "关键词", true, "例如：AI 工具"),
				selectParam("publish_time", "发布时间", "7", "0 不限；1 一天；7 一周；180 半年", "0", "1", "7", "180"),
				selectParam("sort_type", "排序", "2", "0 综合；1 最多点赞；2 最新发布", "0", "1", "2"),
				selectParam("filter_duration", "视频时长", "0", "0 不限；0-1 一分钟内；1-5 一至五分钟；5-10000 五分钟以上", "0", "0-1", "1-5", "5-10000"),
				selectParam("content_type", "内容类型", "0", "0 不限；1 视频；2 图片；3 文章", "0", "1", "2", "3"),
			},
		},
		{
			ID: "douyin.video_detail", Platform: "douyin", Name: "作品详情", Capability: "content_detail",
			Method: http.MethodGet, Path: "/api/v1/douyin/app/v3/fetch_one_video_v3", DocsURL: "https://docs.tikhub.io/406098636e0",
			Params: []EndpointParameter{stringParam("aweme_id", "作品 ID", true, "aweme_id")},
		},
		{
			ID: "douyin.video_by_share_url", Platform: "douyin", Name: "分享链接作品详情", Capability: "content_detail",
			Method: http.MethodGet, Path: "/api/v1/douyin/app/v3/fetch_one_video_by_share_url", DocsURL: "https://docs.tikhub.io/186826220e0",
			Params: []EndpointParameter{stringParam("share_url", "规范分享链接", true, "https://v.douyin.com/…/")},
		},
		{
			ID: "douyin.creator_profile", Platform: "douyin", Name: "博主资料", Capability: "creator_profile",
			Method: http.MethodGet, Path: "/api/v1/douyin/app/v3/handler_user_profile", DocsURL: "https://docs.tikhub.io/186826222e0",
			Params: []EndpointParameter{stringParam("sec_user_id", "博主 sec_user_id", true, "sec_user_id")},
		},
		{
			ID: "douyin.creator_posts", Platform: "douyin", Name: "博主作品", Capability: "creator_posts",
			Method: http.MethodGet, Path: "/api/v1/douyin/app/v3/fetch_user_post_videos", DocsURL: "https://docs.tikhub.io/186826223e0",
			Params: []EndpointParameter{
				stringParam("sec_user_id", "博主 sec_user_id", true, "sec_user_id"),
				{Name: "max_cursor", Label: "翻页游标", Type: "integer", Default: int64(0), Help: "第一页为 0；下一页使用上次响应的 max_cursor"},
				selectParam("count", "每页作品数", "20", "官方上限 20，建议保持 20", "20"),
				selectParam("sort_type", "作品排序", "0", "0 最新；1 最热", "0", "1"),
			},
		},
		{
			ID: "xhs.note_search", Platform: "xhs", Name: "笔记关键词搜索", Capability: "keyword_search",
			Method: http.MethodGet, Path: "/api/v1/xiaohongshu/app_v2/search_notes", DocsURL: "https://docs.tikhub.io/420136398e0",
			FixedParams: map[string]any{"page": 1},
			Params:      []EndpointParameter{stringParam("keyword", "关键词", true, "例如：AI 工具")},
		},
		{
			ID: "xhs.image_detail", Platform: "xhs", Name: "图文笔记详情", Capability: "content_detail",
			Method: http.MethodGet, Path: "/api/v1/xiaohongshu/app_v2/get_image_note_detail", DocsURL: "https://docs.tikhub.io/420136391e0",
			Params: []EndpointParameter{stringParam("note_id", "笔记 ID", true, "note_id")},
		},
		{
			ID: "xhs.video_detail", Platform: "xhs", Name: "视频笔记详情", Capability: "content_detail",
			Method: http.MethodGet, Path: "/api/v1/xiaohongshu/app_v2/get_video_note_detail", DocsURL: "https://docs.tikhub.io/420136392e0",
			Params: []EndpointParameter{stringParam("note_id", "笔记 ID", true, "note_id")},
		},
		{
			ID: "xhs.creator_profile", Platform: "xhs", Name: "博主资料", Capability: "creator_profile",
			Method: http.MethodGet, Path: "/api/v1/xiaohongshu/app_v2/get_user_info", DocsURL: officialOpenAPIURL,
			Params: []EndpointParameter{stringParam("user_id", "博主 user_id", true, "user_id")},
		},
		{
			ID: "xhs.creator_posts", Platform: "xhs", Name: "博主笔记", Capability: "creator_posts",
			Method: http.MethodGet, Path: "/api/v1/xiaohongshu/app_v2/get_user_posted_notes", DocsURL: "https://docs.tikhub.io/420136396e0",
			FixedParams: map[string]any{"cursor": ""},
			Params:      []EndpointParameter{stringParam("user_id", "博主 user_id", true, "user_id")},
		},
		{
			ID: "kuaishou.search", Platform: "kuaishou", Name: "综合关键词搜索", Capability: "keyword_search",
			Method: http.MethodGet, Path: "/api/v1/kuaishou/app/search_comprehensive", DocsURL: officialOpenAPIURL,
			FixedParams: map[string]any{"pcursor": "", "sort_type": "all", "publish_time": "all", "duration": "all"},
			Params:      []EndpointParameter{stringParam("keyword", "关键词", true, "例如：AI 工具")},
		},
		{
			ID: "kuaishou.video_detail", Platform: "kuaishou", Name: "作品详情", Capability: "content_detail",
			Method: http.MethodGet, Path: "/api/v1/kuaishou/web/fetch_one_video_v2", DocsURL: "https://docs.tikhub.io/188317970e0",
			Params: []EndpointParameter{stringParam("photo_id", "作品 photo_id", true, "photo_id")},
		},
		{
			ID: "kuaishou.creator_profile", Platform: "kuaishou", Name: "博主资料", Capability: "creator_profile",
			Method: http.MethodGet, Path: "/api/v1/kuaishou/web/fetch_user_info", DocsURL: "https://docs.tikhub.io/343506806e0",
			Params: []EndpointParameter{stringParam("user_id", "博主 user_id", true, "user_id")},
		},
		{
			ID: "kuaishou.creator_posts", Platform: "kuaishou", Name: "博主作品", Capability: "creator_posts",
			Method: http.MethodGet, Path: "/api/v1/kuaishou/app/fetch_user_post_v2", DocsURL: officialOpenAPIURL,
			FixedParams: map[string]any{"pcursor": "", "sort": "latest"},
			Params:      []EndpointParameter{stringParam("user_id", "博主 user_id", true, "user_id")},
		},
		{
			ID: "wechat_video.keyword_search", Platform: "wechat_video", Name: "视频号关键词搜索", Capability: "keyword_search",
			Method: http.MethodPost, Path: "/api/v1/wechat_search/v2/fetch_search", DocsURL: "https://docs.tikhub.io/472974860e0",
			CostHint: "官方标价约 $0.01 / 次",
			FixedParams: map[string]any{
				"business_type": "video", "raw": true, "sort": "default", "publish_time": "all", "cursor": "",
			},
			Params: []EndpointParameter{stringParam("keyword", "关键词", true, "例如：AI 工具")},
		},
		{
			ID: "wechat_video.video_detail", Platform: "wechat_video", Name: "视频详情", Capability: "content_detail",
			Method: http.MethodPost, Path: "/api/v1/wechat_channels/v2/fetch_video_detail", DocsURL: "https://docs.tikhub.io/472974842e0",
			FixedParams: map[string]any{"raw": true},
			Params:      []EndpointParameter{stringParam("object_id", "作品 object_id", true, "保持为字符串")},
		},
		{
			ID: "wechat_video.creator_profile", Platform: "wechat_video", Name: "视频号资料", Capability: "creator_profile",
			Method: http.MethodPost, Path: "/api/v1/wechat_channels/v2/fetch_channel_info", DocsURL: "https://docs.tikhub.io/472974839e0",
			FixedParams: map[string]any{"raw": true},
			Params:      []EndpointParameter{stringParam("username", "finder username", true, "v2_…@finder")},
		},
		{
			ID: "wechat_video.creator_posts", Platform: "wechat_video", Name: "视频号作品", Capability: "creator_posts",
			Method: http.MethodPost, Path: "/api/v1/wechat_channels/v2/fetch_user_videos", DocsURL: "https://docs.tikhub.io/472974841e0",
			FixedParams: map[string]any{"raw": true, "last_buffer": ""},
			Params:      []EndpointParameter{stringParam("username", "finder username", true, "v2_…@finder")},
		},
		{
			ID: "wechat_mp.keyword_search", Platform: "wechat_mp", Name: "公众号文章搜索", Capability: "keyword_search",
			Method: http.MethodPost, Path: "/api/v1/wechat_search/v2/fetch_search", DocsURL: "https://docs.tikhub.io/472974860e0",
			CostHint: "官方标价约 $0.01 / 次",
			FixedParams: map[string]any{
				"business_type": "article", "raw": true, "sort": "default", "publish_time": "all", "cursor": "",
			},
			Params: []EndpointParameter{stringParam("keyword", "关键词", true, "例如：AI 工具")},
		},
		{
			ID: "wechat_mp.article_detail", Platform: "wechat_mp", Name: "文章详情", Capability: "content_detail",
			Method: http.MethodPost, Path: "/api/v1/wechat_mp/v2/fetch_article_detail", DocsURL: "https://docs.tikhub.io/472974851e0",
			FixedParams: map[string]any{"raw": true},
			Params:      []EndpointParameter{stringParam("url", "文章链接", true, "https://mp.weixin.qq.com/s/…")},
		},
		{
			ID: "wechat_mp.creator_profile", Platform: "wechat_mp", Name: "公众号资料", Capability: "creator_profile",
			Method: http.MethodPost, Path: "/api/v1/wechat_mp/v2/fetch_account_profile", DocsURL: "https://docs.tikhub.io/472974857e0",
			FixedParams: map[string]any{"raw": true},
			Params:      []EndpointParameter{stringParam("username", "公众号 username", true, "gh_…")},
		},
		{
			ID: "wechat_mp.creator_posts", Platform: "wechat_mp", Name: "公众号文章列表", Capability: "creator_posts",
			Method: http.MethodPost, Path: "/api/v1/wechat_mp/v2/fetch_account_articles", DocsURL: "https://docs.tikhub.io/472974858e0",
			FixedParams: map[string]any{"raw": true, "page_size": 20, "offset": ""},
			Params:      []EndpointParameter{stringParam("username", "公众号 username", true, "gh_…")},
		},
		{
			ID: "youtube.video_search", Platform: "youtube", Name: "视频关键词搜索 V2", Capability: "keyword_search",
			Method: http.MethodGet, Path: "/api/v1/youtube/web_v2/get_general_search_v2", DocsURL: "https://docs.tikhub.io/431829299e0",
			FixedParams: map[string]any{"type": "video", "sort_by": "relevance"},
			Params:      []EndpointParameter{stringParam("keyword", "关键词", true, "例如：AI Agent")},
		},
		{
			ID: "youtube.video_detail", Platform: "youtube", Name: "视频详情", Capability: "content_detail",
			Method: http.MethodGet, Path: "/api/v1/youtube/web_v2/get_video_info", DocsURL: "https://docs.tikhub.io/419083081e0",
			FixedParams: map[string]any{"language_code": "zh-CN"},
			Params:      []EndpointParameter{stringParam("video_id", "视频 ID", true, "YouTube video_id")},
		},
		{
			ID: "youtube.video_captions", Platform: "youtube", Name: "视频字幕", Capability: "content_transcript",
			Method: http.MethodGet, Path: "/api/v1/youtube/web_v2/get_video_captions", DocsURL: "https://docs.tikhub.io/431829301e0",
			FixedParams: map[string]any{"format": "txt"},
			Params: []EndpointParameter{
				stringParam("video_id", "视频 ID", true, "YouTube video_id"),
				stringParam("language_code", "字幕语言", false, "例如：zh-Hans；留空先取字幕列表"),
			},
		},
		{
			ID: "x.keyword_search", Platform: "x", Name: "关键词搜索", Capability: "keyword_search",
			Method: http.MethodGet, Path: "/api/v1/twitter/web/fetch_search_timeline", DocsURL: "https://docs.tikhub.io/215701673e0",
			FixedParams: map[string]any{"search_type": "Top"},
			Params:      []EndpointParameter{stringParam("keyword", "关键词", true, "例如：AI Agent")},
		},
		{
			ID: "x.post_detail", Platform: "x", Name: "帖子详情", Capability: "content_detail",
			Method: http.MethodGet, Path: "/api/v1/twitter/web/fetch_tweet_detail", DocsURL: "https://docs.tikhub.io/191321709e0",
			Params: []EndpointParameter{stringParam("tweet_id", "帖子 ID", true, "X status ID")},
		},
		// TikTok / Instagram 目前只在关键词搜索这一档接线；作品详情与博主接口还没有，
		// 趋势雷达的赛道平台白名单也还没有这两个平台（见 docs/TIKHUB.md 的平台表）。
		// 路径与参数已对照 api.tikhub.io 的 OpenAPI 规格核实（2026-11-21），不是按命名猜的。
		{
			ID: "tiktok.video_search", Platform: "tiktok", Name: "视频关键词搜索", Capability: "keyword_search",
			Method: http.MethodGet, Path: "/api/v1/tiktok/app/v3/fetch_video_search_result",
			DocsURL: "https://api.tikhub.io/#/TikTok-App-V3-API/fetch_video_search_result_api_v1_tiktok_app_v3_fetch_video_search_result_get",
			// 与其它搜索端点同一口径：只暴露关键词，翻页与地区走固定参数。
			FixedParams: map[string]any{"offset": 0, "count": 20, "region": "US"},
			Params:      []EndpointParameter{stringParam("keyword", "关键词", true, "例如：truck brake pad")},
		},
		{
			ID: "instagram.general_search", Platform: "instagram", Name: "综合关键词搜索", Capability: "keyword_search",
			Method: http.MethodGet, Path: "/api/v1/instagram/v2/general_search",
			DocsURL: "https://api.tikhub.io/#/Instagram-V2-API/general_search_api_v1_instagram_v2_general_search_get",
			Params:  []EndpointParameter{stringParam("keyword", "关键词", true, "例如：brake lining")},
		},
	}
}

// EstimateCallCostUSD extracts a per-call price from the locally curated
// CostHint. It never guesses: an endpoint without a documented price returns
// false so callers can say "price not registered" instead of inventing one.
func EstimateCallCostUSD(endpoint EndpointDefinition) (float64, bool) {
	match := costHintPattern.FindStringSubmatch(endpoint.CostHint)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

var costHintPattern = regexp.MustCompile(`\$\s*([0-9]+(?:\.[0-9]+)?)`)

func FindEndpoint(id string) (EndpointDefinition, bool) {
	id = strings.TrimSpace(id)
	for _, endpoint := range CuratedEndpoints() {
		if endpoint.ID == id {
			return endpoint, true
		}
	}
	return EndpointDefinition{}, false
}

func PrepareEndpointParams(endpoint EndpointDefinition, input map[string]any) (map[string]any, error) {
	prepared := make(map[string]any, len(endpoint.Params)+len(endpoint.FixedParams))
	for name, value := range endpoint.FixedParams {
		prepared[name] = value
	}
	allowed := make(map[string]EndpointParameter, len(endpoint.Params))
	for _, parameter := range endpoint.Params {
		allowed[parameter.Name] = parameter
	}
	for name, value := range input {
		parameter, ok := allowed[name]
		if !ok {
			return nil, fmt.Errorf("unsupported parameter %q", name)
		}
		normalized, err := normalizeEndpointValue(parameter, value)
		if err != nil {
			return nil, err
		}
		prepared[name] = normalized
	}
	for _, parameter := range endpoint.Params {
		if _, ok := prepared[parameter.Name]; ok {
			continue
		}
		if parameter.Default != nil {
			prepared[parameter.Name] = parameter.Default
			continue
		}
		if parameter.Required {
			return nil, fmt.Errorf("parameter %q is required", parameter.Name)
		}
	}
	switch endpoint.ID {
	case DouyinVideoDetailEndpoint:
		videoID := strings.TrimSpace(fmt.Sprint(prepared["aweme_id"]))
		if !douyinVideoIDPattern.MatchString(videoID) {
			return nil, fmt.Errorf("parameter %q must be a 16-21 digit Douyin video ID", "aweme_id")
		}
	case DouyinVideoShareEndpoint:
		parsed, err := ParseDouyinVideoInput(strings.TrimSpace(fmt.Sprint(prepared["share_url"])))
		if err != nil || parsed.EndpointID != DouyinVideoShareEndpoint {
			return nil, fmt.Errorf("parameter %q must be a valid v.douyin.com HTTPS share URL", "share_url")
		}
		prepared["share_url"] = parsed.NormalizedURL
	}
	return prepared, nil
}

func normalizeEndpointValue(parameter EndpointParameter, value any) (any, error) {
	switch parameter.Type {
	case "string", "select":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("parameter %q must be a string", parameter.Name)
		}
		text = strings.TrimSpace(text)
		if parameter.Required && text == "" {
			return nil, fmt.Errorf("parameter %q is required", parameter.Name)
		}
		if len(text) > 8192 {
			return nil, fmt.Errorf("parameter %q is too long", parameter.Name)
		}
		if parameter.Type == "select" && !containsString(parameter.Options, text) {
			return nil, fmt.Errorf("parameter %q has an unsupported value", parameter.Name)
		}
		return text, nil
	case "integer":
		number, ok := endpointInteger(value)
		if !ok {
			return nil, fmt.Errorf("parameter %q must be an integer", parameter.Name)
		}
		return number, nil
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("parameter %q must be a boolean", parameter.Name)
		}
		return boolean, nil
	default:
		return nil, fmt.Errorf("parameter %q has an unsupported schema", parameter.Name)
	}
}

func endpointInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.Trunc(number) == number && number >= math.MinInt64 && number <= math.MaxInt64 {
			return int64(number), true
		}
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil
	}
	return 0, false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// CallEndpoint executes one fixed definition after validating its form data.
func (c *Client) CallEndpoint(ctx context.Context, endpoint EndpointDefinition, input map[string]any) (map[string]interface{}, error) {
	params, err := PrepareEndpointParams(endpoint, input)
	if err != nil {
		return nil, err
	}
	return c.CallPreparedEndpoint(ctx, endpoint, params)
}

// CallPreparedEndpoint executes parameters that were already validated by
// PrepareEndpointParams. It exists so the UI can persist the exact final
// parameter envelope before the network request starts.
func (c *Client) CallPreparedEndpoint(ctx context.Context, endpoint EndpointDefinition, params map[string]any) (map[string]interface{}, error) {
	switch endpoint.Method {
	case http.MethodGet:
		query := make(url.Values, len(params))
		for name, value := range params {
			switch typed := value.(type) {
			case bool:
				query.Set(name, strconv.FormatBool(typed))
			default:
				query.Set(name, fmt.Sprint(value))
			}
		}
		return c.get(ctx, endpoint.Path, query)
	case http.MethodPost:
		return c.postJSON(ctx, endpoint.Path, params)
	default:
		return nil, fmt.Errorf("unsupported TikHub method")
	}
}
