package tikhub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCuratedEndpointsKeepOnlyTrendRadarCore(t *testing.T) {
	endpoints := CuratedEndpoints()
	if len(endpoints) < 24 || len(endpoints) > 28 {
		t.Fatalf("curated endpoint count=%d, want focused catalog", len(endpoints))
	}
	requiredPlatforms := map[string]map[string]bool{
		"douyin":       {},
		"xhs":          {},
		"kuaishou":     {},
		"wechat_video": {},
		"wechat_mp":    {},
		"youtube":      {},
		"x":            {},
	}
	for _, endpoint := range endpoints {
		if endpoint.ID == "" || endpoint.Name == "" || endpoint.Method == "" || endpoint.Path == "" || endpoint.DocsURL == "" {
			t.Fatalf("incomplete endpoint: %#v", endpoint)
		}
		capabilities, ok := requiredPlatforms[endpoint.Platform]
		if !ok {
			t.Fatalf("unexpected platform %q", endpoint.Platform)
		}
		capabilities[endpoint.Capability] = true
		if endpoint.Capability == "daily_rank" || endpoint.Capability == "comments" {
			t.Fatalf("monitoring catalog retained %s endpoint %q", endpoint.Capability, endpoint.ID)
		}
		if endpoint.Path == "/api/v1/douyin/app/v3/fetch_video_search_result" {
			t.Fatal("stale Douyin search endpoint retained")
		}
		if endpoint.Capability == "keyword_search" && endpoint.ID != "douyin.video_search" && (len(endpoint.Params) != 1 || endpoint.Params[0].Name != "keyword") {
			t.Fatalf("%s exposes non-essential search params: %#v", endpoint.ID, endpoint.Params)
		}
	}
	for platform, capabilities := range requiredPlatforms {
		if platform == "youtube" || platform == "x" {
			for _, capability := range []string{"keyword_search", "content_detail"} {
				if !capabilities[capability] {
					t.Fatalf("%s missing %s", platform, capability)
				}
			}
			continue
		}
		for _, capability := range []string{"keyword_search", "content_detail", "creator_profile", "creator_posts"} {
			if !capabilities[capability] {
				t.Fatalf("%s missing %s", platform, capability)
			}
		}
	}
	douyinSearch, ok := FindEndpoint("douyin.video_search")
	if !ok || douyinSearch.Path != "/api/v1/douyin/search/fetch_video_search_v2" || douyinSearch.Method != http.MethodPost {
		t.Fatalf("unexpected Douyin search definition: %#v", douyinSearch)
	}
	if douyinSearch.CostHint != "官方基础价 $0.01 / 次（2026-07-25 查询）" {
		t.Fatalf("Douyin official price hint missing: %q", douyinSearch.CostHint)
	}
	prepared, err := PrepareEndpointParams(douyinSearch, map[string]any{"keyword": "AI工具"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["publish_time"] != "7" || prepared["sort_type"] != "2" || prepared["filter_duration"] != "0" || prepared["content_type"] != "0" {
		t.Fatalf("unexpected Douyin defaults: %#v", prepared)
	}
	if _, err := PrepareEndpointParams(douyinSearch, map[string]any{"keyword": "AI工具", "publish_time": "30"}); err == nil {
		t.Fatal("unsupported Douyin publish_time should fail")
	}
	douyinDetail, ok := FindEndpoint(DouyinVideoDetailEndpoint)
	if !ok || douyinDetail.Path != "/api/v1/douyin/app/v3/fetch_one_video_v3" {
		t.Fatalf("unexpected Douyin detail definition: %#v", douyinDetail)
	}
	if _, err := PrepareEndpointParams(douyinDetail, map[string]any{"aweme_id": "not-an-id"}); err == nil {
		t.Fatal("invalid Douyin video ID should fail")
	}
	douyinShare, ok := FindEndpoint(DouyinVideoShareEndpoint)
	if !ok || douyinShare.Path != "/api/v1/douyin/app/v3/fetch_one_video_by_share_url" {
		t.Fatalf("unexpected Douyin share definition: %#v", douyinShare)
	}
	prepared, err = PrepareEndpointParams(douyinShare, map[string]any{"share_url": "https://v.douyin.com/eer9qWKqopE/"})
	if err != nil || prepared["share_url"] != "https://v.douyin.com/eer9qWKqopE/" {
		t.Fatalf("Douyin share params=%#v err=%v", prepared, err)
	}
	if _, err := PrepareEndpointParams(douyinShare, map[string]any{"share_url": "https://example.com/video"}); err == nil {
		t.Fatal("non-Douyin share URL should fail")
	}
	douyinCreatorPosts, ok := FindEndpoint("douyin.creator_posts")
	if !ok {
		t.Fatal("missing Douyin creator posts endpoint")
	}
	prepared, err = PrepareEndpointParams(douyinCreatorPosts, map[string]any{
		"sec_user_id": "creator-1", "max_cursor": json.Number("123"), "count": "20", "sort_type": "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["max_cursor"] != int64(123) || prepared["count"] != "20" || prepared["sort_type"] != "0" {
		t.Fatalf("unexpected creator paging params: %#v", prepared)
	}
	if _, err := PrepareEndpointParams(douyinCreatorPosts, map[string]any{"sec_user_id": "creator-1", "count": "50"}); err == nil {
		t.Fatal("creator page size above official limit should fail")
	}
	youtubeSearch, ok := FindEndpoint("youtube.video_search")
	if !ok || youtubeSearch.Path != "/api/v1/youtube/web_v2/get_general_search_v2" || youtubeSearch.Method != http.MethodGet {
		t.Fatalf("unexpected YouTube search definition: %#v", youtubeSearch)
	}
	youtubeCaptions, ok := FindEndpoint("youtube.video_captions")
	if !ok || youtubeCaptions.Path != "/api/v1/youtube/web_v2/get_video_captions" || youtubeCaptions.Capability != "content_transcript" {
		t.Fatalf("unexpected YouTube captions definition: %#v", youtubeCaptions)
	}
	xSearch, ok := FindEndpoint("x.keyword_search")
	if !ok || xSearch.Path != "/api/v1/twitter/web/fetch_search_timeline" || xSearch.Method != http.MethodGet {
		t.Fatalf("unexpected X search definition: %#v", xSearch)
	}
}

func TestYouTubeAndXEndpointParametersStayFocused(t *testing.T) {
	youtubeSearch, _ := FindEndpoint("youtube.video_search")
	prepared, err := PrepareEndpointParams(youtubeSearch, map[string]any{"keyword": "AI Agent"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["keyword"] != "AI Agent" || prepared["type"] != "video" || prepared["sort_by"] != "relevance" {
		t.Fatalf("unexpected YouTube params: %#v", prepared)
	}
	if _, err := PrepareEndpointParams(youtubeSearch, map[string]any{"keyword": "AI", "api_key": "no"}); err == nil {
		t.Fatal("unknown YouTube parameter was accepted")
	}

	xSearch, _ := FindEndpoint("x.keyword_search")
	prepared, err = PrepareEndpointParams(xSearch, map[string]any{"keyword": "AI Agent"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["keyword"] != "AI Agent" || prepared["search_type"] != "Top" {
		t.Fatalf("unexpected X params: %#v", prepared)
	}
	if _, err := PrepareEndpointParams(xSearch, map[string]any{"keyword": "AI", "cursor": "hidden"}); err == nil {
		t.Fatal("unknown X cursor override was accepted")
	}
}

func TestPrepareEndpointParamsRejectsUnknownAndAppliesFixedValues(t *testing.T) {
	endpoint, ok := FindEndpoint("wechat_video.keyword_search")
	if !ok {
		t.Fatal("wechat video search endpoint missing")
	}
	prepared, err := PrepareEndpointParams(endpoint, map[string]any{"keyword": "人工智能"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["business_type"] != "video" || prepared["keyword"] != "人工智能" {
		t.Fatalf("unexpected prepared params: %#v", prepared)
	}
	if prepared["sort"] != "default" || prepared["publish_time"] != "all" || prepared["cursor"] != "" {
		t.Fatalf("stable search defaults missing: %#v", prepared)
	}
	if _, err := PrepareEndpointParams(endpoint, map[string]any{"keyword": "AI", "business_type": "article"}); err == nil {
		t.Fatal("fixed business_type override was accepted")
	}
	if _, err := PrepareEndpointParams(endpoint, map[string]any{"keyword": "AI", "token": "secret"}); err == nil {
		t.Fatal("unknown parameter was accepted")
	}
	if _, err := PrepareEndpointParams(endpoint, map[string]any{"keyword": "AI", "cursor": "hidden"}); err == nil {
		t.Fatal("internal pagination parameter was exposed")
	}
	if _, err := PrepareEndpointParams(endpoint, nil); err == nil {
		t.Fatal("missing required keyword was accepted")
	}
}

func TestCallEndpointPreservesLargeIntegerTokens(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/wechat_search/v2/fetch_search" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"results":[{"object_id":14941130915890399732}]}}`))
	}))
	defer server.Close()

	endpoint, ok := FindEndpoint("wechat_video.keyword_search")
	if !ok {
		t.Fatal("wechat video search endpoint missing")
	}
	client := New("synthetic-token")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	response, err := client.CallEndpoint(context.Background(), endpoint, map[string]any{"keyword": "人工智能"})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["business_type"] != "video" {
		t.Fatalf("fixed business_type missing: %#v", gotBody)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "14941130915890399732") {
		t.Fatalf("large integer lost precision: %s", raw)
	}
}
