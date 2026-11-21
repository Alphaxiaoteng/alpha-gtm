package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
)

func writeRadarFixture(t *testing.T, root, relative string, value any) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func radarTestConfig() tikhub.RadarConfig {
	return tikhub.RadarConfig{Niches: []tikhub.RadarNiche{
		{
			ID: "tools", Name: "工具观察", Enabled: true,
			SearchTerms: []string{"AI工具"}, Platforms: []string{"xhs"},
		},
		{
			ID: "relationships", Name: "关系观察", Enabled: true,
			SearchTerms: []string{"亲密关系"}, Platforms: []string{"douyin"},
		},
	}}
}

func TestBuildTrendRadarSummaryGroupsSearchSnapshotsByNiche(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarTestConfig()); err != nil {
		t.Fatal(err)
	}
	coverSource := "https://sns-na-i11.xhscdn.com/cover.webp"
	writeRadarFixture(t, dataDir, filepath.Join("tikhub/covers", tikhub.CachedCoverName(coverSource)), []byte("cached-cover"))
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-25/1_xhs_note_search.json", map[string]any{
		"schema_version": 1,
		"call_id":        1,
		"platform":       "xhs",
		"endpoint_id":    "xhs.note_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "AI工具"},
		"fetched_at":     "2026-07-25T03:04:05Z",
		"response": map[string]any{
			"data": map[string]any{"items": []any{
				map[string]any{
					"note_id": "ai-1", "title": "三个 AI 工具提高内容效率", "nickname": "效率研究所", "liked_count": 8600,
					"images_list": []any{map[string]any{"url": coverSource}},
				},
			}},
		},
	})
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-25/2_douyin_video_search.json", map[string]any{
		"schema_version": 1,
		"call_id":        2,
		"platform":       "douyin",
		"endpoint_id":    "douyin.video_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "亲密关系"},
		"fetched_at":     "2026-07-25T03:05:00Z",
		"response": map[string]any{
			"data": []any{
				map[string]any{"aweme_id": "emotion-1", "desc": "亲密关系里最容易忽略的边界", "nickname": "关系观察室", "digg_count": 5200},
			},
		},
	})

	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 7, 25, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotState != "ready" || len(got.Niches) != 2 || len(got.Signals) != 2 {
		t.Fatalf("unexpected summary=%#v", got)
	}
	if got.Niches[0].ID != "tools" || got.Niches[0].ResultCount != 1 || got.Niches[1].ID != "relationships" || got.Niches[1].ResultCount != 1 {
		t.Fatalf("niches=%#v", got.Niches)
	}
	if got.Signals[0].NicheID == got.Signals[1].NicheID {
		t.Fatalf("signals were not grouped by niche: %#v", got.Signals)
	}
	foundLocalCover := false
	for _, signal := range got.Signals {
		if signal.Platform == "xhs" && signal.CoverURL == "/api/trend-radar/covers/"+tikhub.CachedCoverName(coverSource) {
			foundLocalCover = true
		}
	}
	if !foundLocalCover {
		t.Fatalf("local cover missing: %#v", got.Signals)
	}
}

func TestBuildTrendRadarSummaryDoesNotTruncateCollectedSignals(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarTestConfig()); err != nil {
		t.Fatal(err)
	}
	items := make([]any, 150)
	for index := range items {
		items[index] = map[string]any{
			"note_id": fmt.Sprintf("note-%03d", index),
			"title":   fmt.Sprintf("AI 工具样本 %03d", index),
		}
	}
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-25/150_xhs_note_search.json", map[string]any{
		"schema_version": 1,
		"call_id":        150,
		"platform":       "xhs",
		"endpoint_id":    "xhs.note_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "AI工具"},
		"fetched_at":     "2026-07-25T03:04:05Z",
		"response":       map[string]any{"items": items},
	})
	got, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != len(items) {
		t.Fatalf("signals=%d want=%d", len(got.Signals), len(items))
	}
}

func TestBuildTrendRadarSummaryKeepsUnconfiguredHistory(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, tikhub.RadarConfig{Niches: []tikhub.RadarNiche{}}); err != nil {
		t.Fatal(err)
	}
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-25/1_xhs_note_search.json", map[string]any{
		"schema_version": 1,
		"call_id":        1,
		"platform":       "xhs",
		"endpoint_id":    "xhs.note_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "过去的搜索词"},
		"fetched_at":     "2026-07-25T03:04:05Z",
		"response": map[string]any{"items": []any{
			map[string]any{"note_id": "history-1", "title": "仍然可以查看的旧内容"},
		}},
	})

	got, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 1 || got.Signals[0].NicheID != archivedRadarNicheID {
		t.Fatalf("historical signals were hidden: %#v", got.Signals)
	}
	if len(got.Niches) != 1 || got.Niches[0].State != "archived" || got.Niches[0].Name != "历史记录" {
		t.Fatalf("archived niche=%#v", got.Niches)
	}
}

func TestBuildTrendRadarSummaryIgnoresRanksAndComments(t *testing.T) {
	dataDir := t.TempDir()
	writeRadarFixture(t, dataDir, "tikhub/ranks/2026-07-25/video.json", map[string]any{
		"items": []any{map[string]any{"title": "不应出现的榜单"}},
	})
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-25/3_xhs_comments.json", map[string]any{
		"schema_version": 1,
		"call_id":        3,
		"platform":       "xhs",
		"endpoint_id":    "xhs.comments",
		"capability":     "comments",
		"parameters":     map[string]any{"keyword": "AI工具"},
		"fetched_at":     "2026-07-25T03:04:05Z",
		"response":       map[string]any{"items": []any{map[string]any{"title": "不应出现的评论"}}},
	})
	got, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 0 || got.SnapshotState != "empty" {
		t.Fatalf("legacy ranks/comments leaked into monitoring: %#v", got)
	}
}

func TestCreatorMonitoringSupportsDouyinAndXHSFromSavedCalls(t *testing.T) {
	dataDir := t.TempDir()
	writeRadarFixture(t, dataDir, "tikhub/watchlist.json", map[string]any{
		"users": []any{
			map[string]any{"platform": "xhs", "user_id": "xhs-1", "name": "小红书样本"},
			map[string]any{"platform": "douyin", "user_id": "dy-1", "name": "抖音样本"},
		},
	})
	for index, fixture := range []struct {
		platform, endpoint, key, id string
	}{
		{"xhs", "xhs.creator_posts", "user_id", "xhs-1"},
		{"douyin", "douyin.creator_posts", "sec_user_id", "dy-1"},
	} {
		writeRadarFixture(t, dataDir, filepath.Join("tikhub/calls/2026-07-25", fixture.platform+".json"), map[string]any{
			"schema_version": 1,
			"call_id":        index + 10,
			"platform":       fixture.platform,
			"endpoint_id":    fixture.endpoint,
			"capability":     "creator_posts",
			"parameters":     map[string]any{fixture.key: fixture.id},
			"fetched_at":     "2026-07-25T03:04:05Z",
			"response":       map[string]any{"items": []any{map[string]any{"title": "作品"}}},
		})
	}
	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Creators) != 2 || got.Creators[0].State != "ready" || got.Creators[1].State != "ready" {
		t.Fatalf("creators=%#v", got.Creators)
	}
	for _, creator := range got.Creators {
		if creator.FolderPath == "" {
			t.Fatalf("creator folder missing: %#v", creator)
		}
		if info, err := os.Stat(creator.FolderPath); err != nil || !info.IsDir() {
			t.Fatalf("creator folder unavailable: path=%q err=%v", creator.FolderPath, err)
		}
	}
}

func TestCreatorFolderCombinesPagesAndFiltersToConfiguredLookback(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	user := tikhub.WatchUser{
		Platform: "douyin", UserID: "dy-1", Name: "抖音样本",
		LookbackDays: 30, PageSize: 20, MaxPages: 3, SortType: 0,
	}
	snapshots := []tikhub.CallSnapshot{
		{
			Platform: "douyin", Capability: "creator_posts", FetchedAt: "2026-07-26T12:00:02Z",
			Parameters: map[string]any{"sec_user_id": "dy-1", "max_cursor": 123},
			Response: map[string]interface{}{"data": map[string]interface{}{"aweme_list": []interface{}{
				map[string]interface{}{"aweme_id": "old", "desc": "四十天前", "create_time": now.AddDate(0, 0, -40).Unix()},
			}}},
		},
		{
			Platform: "douyin", Capability: "creator_posts", FetchedAt: "2026-07-26T12:00:01Z",
			Parameters: map[string]any{"sec_user_id": "dy-1", "max_cursor": 0},
			Response: map[string]interface{}{"data": map[string]interface{}{"aweme_list": []interface{}{
				map[string]interface{}{"aweme_id": "recent", "desc": "十天前", "create_time": now.AddDate(0, 0, -10).Unix()},
			}}},
		},
	}
	creator := radarCreatorFromWatch(user, snapshots, nil, now, dataDir)
	if creator.LastPages != 2 || creator.LastCount != 1 || len(creator.LatestPosts) != 1 {
		t.Fatalf("creator paging/filter=%#v", creator)
	}
	if creator.LatestPosts[0].ID != "recent" {
		t.Fatalf("unexpected posts=%#v", creator.LatestPosts)
	}
}

// 博主级自动更新开关必须出现在 summary 里，否则 UI 只能显示一个猜的状态。
func TestCreatorSummaryExposesAutoArchiveSwitch(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	off := radarCreatorFromWatch(tikhub.WatchUser{Platform: "douyin", UserID: "dy-off"}, nil, nil, now, dataDir)
	if off.AutoArchive {
		t.Fatal("a creator without the switch must report auto_archive=false")
	}

	on := radarCreatorFromWatch(
		tikhub.WatchUser{Platform: "douyin", UserID: "dy-on", AutoArchive: true}, nil, nil, now, dataDir,
	)
	if !on.AutoArchive {
		t.Fatal("an opted-in creator must report auto_archive=true")
	}
	encoded, err := json.Marshal(on)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"auto_archive":true`) {
		t.Fatalf("summary JSON is missing the switch: %s", encoded)
	}
}

func TestTrendRadarKeepsExactVideoLookupsSeparateFromKeywordSignals(t *testing.T) {
	dataDir := t.TempDir()
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-26/video-detail.json", map[string]any{
		"schema_version": 1,
		"call_id":        50,
		"platform":       "douyin",
		"endpoint_id":    tikhub.DouyinVideoDetailEndpoint,
		"capability":     "content_detail",
		"parameters":     map[string]any{"aweme_id": "7666283984318131889"},
		"fetched_at":     "2026-07-26T03:04:05Z",
		"response": map[string]any{"data": map[string]any{"aweme_detail": map[string]any{
			"aweme_id": "7666283984318131889", "desc": "精确视频结果",
			"author": map[string]any{"nickname": "样本博主"},
		}}},
	})
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-26/video-detail-again.json", map[string]any{
		"schema_version": 1,
		"call_id":        51,
		"platform":       "douyin",
		"endpoint_id":    tikhub.DouyinVideoDetailEndpoint,
		"capability":     "content_detail",
		"parameters":     map[string]any{"aweme_id": "7666283984318131889"},
		"fetched_at":     "2026-07-26T04:05:06Z",
		"response": map[string]any{"data": map[string]any{"aweme_detail": map[string]any{
			"aweme_id": "7666283984318131889", "desc": "精确视频结果",
			"author": map[string]any{"nickname": "样本博主"},
		}}},
	})
	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 0 || len(got.VideoLookups) != 1 {
		t.Fatalf("summary=%#v", got)
	}
	if got.VideoLookups[0].CallID != 50 || got.VideoLookups[0].VideoID != "7666283984318131889" ||
		got.VideoLookups[0].Title != "精确视频结果" || got.VideoLookups[0].Occurrences != 2 {
		t.Fatalf("lookup=%#v", got.VideoLookups[0])
	}
}

func TestVideoSearchParseHandlerFiltersShareTextWithoutCallingProvider(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/trend-radar/video-search/parse", bytes.NewBufferString(
		`{"input":"8.76 火种情感 https://v.douyin.com/eer9qWKqopE/ 复制此链接"}`,
	))
	recorder := httptest.NewRecorder()
	(&Server{cfg: config.Config{DataDir: t.TempDir()}}).handleAPITrendRadarVideoSearchParse(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got tikhub.ParsedDouyinVideoInput
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.EndpointID != tikhub.DouyinVideoShareEndpoint || got.NormalizedURL != "https://v.douyin.com/eer9qWKqopE/" {
		t.Fatalf("parsed=%#v", got)
	}
}

func TestVideoSearchParseHandlerReturnsExistingVideoWithoutProviderCall(t *testing.T) {
	dataDir := t.TempDir()
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-26/video-detail.json", map[string]any{
		"schema_version": 1,
		"call_id":        50,
		"platform":       "douyin",
		"endpoint_id":    tikhub.DouyinVideoDetailEndpoint,
		"capability":     "content_detail",
		"parameters":     map[string]any{"aweme_id": "7666283984318131889"},
		"fetched_at":     "2026-07-26T03:04:05Z",
		"response": map[string]any{"data": map[string]any{"aweme_detail": map[string]any{
			"aweme_id": "7666283984318131889", "desc": "精确视频结果",
		}}},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/trend-radar/video-search/parse", bytes.NewBufferString(
		`{"input":"https://www.douyin.com/user/example?modal_id=7666283984318131889"}`,
	))
	recorder := httptest.NewRecorder()
	(&Server{cfg: config.Config{DataDir: dataDir}}).handleAPITrendRadarVideoSearchParse(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got struct {
		ExistingCallID         int64  `json:"existing_call_id"`
		ExistingVideoID        string `json:"existing_video_id"`
		ExistingAnalysisStatus string `json:"existing_analysis_status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ExistingCallID != 50 || got.ExistingVideoID != "7666283984318131889" || got.ExistingAnalysisStatus != "pending" {
		t.Fatalf("existing=%#v", got)
	}
}

func TestNormalizeRadarSignalSupportsDouyinYouTubeAndXTextAndCovers(t *testing.T) {
	dataDir := t.TempDir()
	niche := trendRadarNiche{ID: "ai", Name: "AI赛道"}
	fixtures := []struct {
		platform, cover, wantTitle, wantText, wantCreator, wantURL string
		item                                                       map[string]interface{}
	}{
		{
			platform: "douyin", cover: "https://p3-sign.douyincdn.com/cover.jpeg",
			wantTitle: "三个 AI 工具让内容效率翻倍", wantText: "三个 AI 工具让内容效率翻倍", wantCreator: "效率研究所",
			wantURL: "https://www.douyin.com/video/7520000000000000001",
			item: map[string]interface{}{
				"aweme_id": "7520000000000000001", "desc": "三个 AI 工具让内容效率翻倍",
				"author": map[string]interface{}{"nickname": "效率研究所"},
				"video":  map[string]interface{}{"cover": map[string]interface{}{"url_list": []interface{}{"https://p3-sign.douyincdn.com/cover.jpeg"}}},
			},
		},
		{
			platform: "youtube", cover: "https://i.ytimg.com/vi/abc123/hqdefault.jpg",
			wantTitle: "Best AI Agents", wantText: "A practical walkthrough of local AI agents.", wantCreator: "Agent Lab",
			wantURL: "https://www.youtube.com/watch?v=abc123",
			item: map[string]interface{}{
				"video_id": "abc123", "title": "Best AI Agents", "description_snippet": "A practical walkthrough of local AI agents.",
				"author":     "Agent Lab",
				"thumbnails": []interface{}{map[string]interface{}{"url": "https://i.ytimg.com/vi/abc123/hqdefault.jpg"}},
				"view_count": "1,200 views",
			},
		},
		{
			platform: "x", cover: "https://pbs.twimg.com/media/example.jpg",
			wantTitle: "AI Agent workflow with local-first storage", wantText: "AI Agent workflow with local-first storage", wantCreator: "Builder",
			wantURL: "https://x.com/i/web/status/1900000000000000000",
			item: map[string]interface{}{
				"tweet_id": "1900000000000000000",
				"text":     "AI Agent workflow with local-first storage",
				"media": map[string]interface{}{"video": []interface{}{
					map[string]interface{}{"media_url_https": "https://pbs.twimg.com/media/example.jpg"},
				}},
				"user_info": map[string]interface{}{"name": "Builder"},
			},
		},
	}
	for _, fixture := range fixtures {
		writeRadarFixture(t, dataDir, filepath.Join("tikhub/covers", tikhub.CachedCoverName(fixture.cover)), []byte("cached"))
		got := normalizeRadarSignal(niche, "AI工具", fixture.platform, "2026-07-25T03:04:05Z", dataDir, fixture.item)
		if got.Title != fixture.wantTitle || got.ContentText != fixture.wantText || got.ContentSource != "provider_text" ||
			got.Creator != fixture.wantCreator || got.URL != fixture.wantURL ||
			got.CoverURL != "/api/trend-radar/covers/"+tikhub.CachedCoverName(fixture.cover) {
			t.Fatalf("%s signal=%#v", fixture.platform, got)
		}
		if fixture.platform == "youtube" && got.Play != 1200 {
			t.Fatalf("YouTube formatted view count was not normalized: %#v", got)
		}
	}
}

func TestNormalizeRadarSignalExtractsMetricsIncludingAwemeInfoAndComments(t *testing.T) {
	dataDir := t.TempDir()
	niche := trendRadarNiche{ID: "ai", Name: "AI赛道"}
	item := map[string]interface{}{
		"aweme_id": "7520000000000000002",
		"desc":     "测试互动数据",
		"aweme_info": map[string]interface{}{
			"statistics": map[string]interface{}{
				"play_count":    int64(10000),
				"digg_count":    int64(2000),
				"collect_count": int64(300),
				"share_count":   int64(400),
				"comment_count": int64(500),
			},
		},
	}
	got := normalizeRadarSignal(niche, "AI", "douyin", "2026-07-25T03:04:05Z", dataDir, item)
	if got.Play != 10000 || got.Likes != 2000 || got.Collects != 300 || got.Shares != 400 || got.Comments != 500 {
		t.Fatalf("unexpected metrics in signal: %#v", got)
	}
}

func TestBuildTrendRadarSummaryPrefersSavedYouTubeCaption(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, tikhub.RadarConfig{Niches: []tikhub.RadarNiche{{
		ID: "ai", Name: "AI赛道", Enabled: true, SearchTerms: []string{"AI工具"}, Platforms: []string{"youtube"},
	}}}); err != nil {
		t.Fatal(err)
	}
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-25/1_youtube_video_search.json", map[string]any{
		"schema_version": 1, "call_id": 1, "platform": "youtube",
		"endpoint_id": "youtube.video_search", "capability": "keyword_search",
		"parameters": map[string]any{"keyword": "AI工具"}, "fetched_at": "2026-07-25T03:04:05Z",
		"response": map[string]any{"data": map[string]any{"videos": []any{
			map[string]any{"video_id": "yt-1", "title": "AI 视频", "description_snippet": "搜索摘要"},
		}}},
	})
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-07-25/2_youtube_video_captions.json", map[string]any{
		"schema_version": 1, "call_id": 2, "platform": "youtube",
		"endpoint_id": "youtube.video_captions", "capability": "content_transcript",
		"parameters": map[string]any{"video_id": "yt-1", "language_code": "zh"}, "fetched_at": "2026-07-25T03:05:05Z",
		"response": map[string]any{"data": map[string]any{
			"video_id": "yt-1", "language_code": "zh", "format": "txt", "content": "完整字幕正文",
		}},
	})
	got, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 1 || got.Signals[0].ContentText != "完整字幕正文" || got.Signals[0].ContentSource != "platform_caption" {
		t.Fatalf("signals=%#v", got.Signals)
	}
}

// TestNeverSyncedCreatorMarshalsLatestPostsAsArray guards the JSON contract the
// dashboard relies on: a creator added but not yet fetched has no posts, and the
// field must serialize as [] rather than null. The frontend renders
// creator.latest_posts.length directly, so a null would crash the whole page.
func TestNeverSyncedCreatorMarshalsLatestPostsAsArray(t *testing.T) {
	creator := radarCreatorFromWatch(
		tikhub.WatchUser{Platform: "xhs", UserID: "never-synced-user", Name: "新博主"},
		nil, nil, time.Now(), t.TempDir(),
	)
	encoded, err := json.Marshal(creator)
	if err != nil {
		t.Fatalf("marshal creator: %v", err)
	}
	if !strings.Contains(string(encoded), `"latest_posts":[]`) {
		t.Fatalf("latest_posts must marshal as an empty array, got %s", encoded)
	}
}

// TestCreatorArchiveEndpointGuardsInputs pins the safety contract of the
// full-archive endpoint: it spends TikHub credit and writes media to disk, so it
// must reject cross-site callers, non-POST methods, and creators that are not on
// the local watchlist.
func TestCreatorArchiveEndpointGuardsInputs(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	path := "/api/trend-radar/creators/douyin/some-user/archive"

	// 只注册了 POST；Go mux 对未匹配方法返回 404，因此这里只要求非 2xx。
	getRecorder := callDataAPI(t, server, http.MethodGet, path, nil)
	if getRecorder.Code < 400 {
		t.Fatalf("GET status=%d want a 4xx rejection", getRecorder.Code)
	}

	crossSite := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "https://evil.example.com")
	server.ServeHTTP(crossSite, request)
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site status=%d want 403", crossSite.Code)
	}

	// 空名单：必须报 404，而不是先花钱调接口。
	notFound := callDataAPI(t, server, http.MethodPost, path, map[string]any{})
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("unknown creator status=%d body=%s", notFound.Code, notFound.Body.String())
	}

	// 不支持小红书全量归档。
	badPlatform := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/creators/xhs/some-user/archive", map[string]any{})
	if badPlatform.Code != http.StatusBadRequest {
		t.Fatalf("xhs status=%d want 400", badPlatform.Code)
	}
}

// TestCreatorArchiveFailuresEndpointReadsLedger covers the on-disk failure ledger
// the UI uses to list and retry assets that did not download. A missing ledger is
// "no failures", not an error, so the endpoint must answer with an empty array.
func TestCreatorArchiveFailuresEndpointReadsLedger(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	const (
		platform = "douyin"
		userID   = "failures-user"
	)
	path := "/api/trend-radar/creators/" + platform + "/" + userID + "/archive-failures"

	empty := callDataAPI(t, server, http.MethodGet, path, nil)
	if empty.Code != http.StatusOK {
		t.Fatalf("no-ledger status=%d body=%s", empty.Code, empty.Body.String())
	}
	var emptyPayload struct {
		OK       bool                          `json:"ok"`
		Total    int                           `json:"total"`
		Failures []radar.CreatorArchiveFailure `json:"failures"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyPayload); err != nil {
		t.Fatalf("decode empty payload: %v body=%s", err, empty.Body.String())
	}
	if !emptyPayload.OK || emptyPayload.Total != 0 || len(emptyPayload.Failures) != 0 {
		t.Fatalf("no-ledger payload=%#v", emptyPayload)
	}

	folder := tikhub.CreatorFolder(dataDir, platform, userID)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	ledger := `{"schema_version":1,"platform":"douyin","user_id":"failures-user",
		"updated_at":"2026-11-05T10:00:00Z","failures":[
		{"aweme_id":"7660464546108533873","kind":"cover","reason":"unexpected status 403","attempted_at":"2026-11-05T10:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(folder, "archive-failures.json"), []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded := callDataAPI(t, server, http.MethodGet, path, nil)
	if loaded.Code != http.StatusOK {
		t.Fatalf("ledger status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	var payload struct {
		OK       bool                          `json:"ok"`
		Total    int                           `json:"total"`
		Failures []radar.CreatorArchiveFailure `json:"failures"`
	}
	if err := json.Unmarshal(loaded.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode ledger payload: %v body=%s", err, loaded.Body.String())
	}
	if payload.Total != 1 || len(payload.Failures) != 1 {
		t.Fatalf("ledger payload=%#v", payload)
	}
	if got := payload.Failures[0]; got.AwemeID != "7660464546108533873" || got.Kind != "cover" || got.Reason == "" {
		t.Fatalf("failure=%#v", got)
	}
}

func TestCreatorMediaEndpointServesFilesAndRejectsTraversals(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	platform := "douyin"
	userID := "user-media-1"
	folder := tikhub.CreatorFolder(dataDir, platform, userID)
	coverDir := filepath.Join(folder, "media", "covers")
	audioDir := filepath.Join(folder, "media", "audio")
	if err := os.MkdirAll(coverDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(audioDir, 0o700); err != nil {
		t.Fatal(err)
	}

	jpegBytes := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00fakejpegdata")
	if err := os.WriteFile(filepath.Join(coverDir, "123.jpg"), jpegBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	audioBytes := []byte("ID3\x03\x00\x00\x00\x00\x00\x00fakemp3data1234567890")
	if err := os.WriteFile(filepath.Join(audioDir, "123.mp3"), audioBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. 正常封面读取
	req := httptest.NewRequest(http.MethodGet, "/api/trend-radar/creators/douyin/"+userID+"/media/covers/123.jpg", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cover status=%d want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("cover content-type=%q want image/jpeg", ct)
	}
	if w.Body.String() != string(jpegBytes) {
		t.Fatalf("cover body mismatch")
	}

	// 2. 音频读取与 Range 请求（流式试听与拖动快进）
	rangeReq := httptest.NewRequest(http.MethodGet, "/api/trend-radar/creators/douyin/"+userID+"/media/audio/123.mp3", nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	wRange := httptest.NewRecorder()
	server.ServeHTTP(wRange, rangeReq)
	if wRange.Code != http.StatusPartialContent {
		t.Fatalf("audio range status=%d want 206", wRange.Code)
	}
	if ct := wRange.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("audio content-type=%q want audio/mpeg", ct)
	}
	if wRange.Body.String() != string(audioBytes[:10]) {
		t.Fatalf("audio range body mismatch: got %q", wRange.Body.String())
	}

	// 3. 越权与路径穿越必须被拒绝 (404)
	traversalReq := httptest.NewRequest(http.MethodGet, "/api/trend-radar/creators/douyin/"+userID+"/media/covers/..%2f..%2findex.json", nil)
	wTrav := httptest.NewRecorder()
	server.ServeHTTP(wTrav, traversalReq)
	if wTrav.Code != http.StatusNotFound {
		t.Fatalf("traversal status=%d want 404", wTrav.Code)
	}

	// 4. 不支持的 kind 必须返回 404
	badKindReq := httptest.NewRequest(http.MethodGet, "/api/trend-radar/creators/douyin/"+userID+"/media/other/123.jpg", nil)
	wBadKind := httptest.NewRecorder()
	server.ServeHTTP(wBadKind, badKindReq)
	if wBadKind.Code != http.StatusNotFound {
		t.Fatalf("bad kind status=%d want 404", wBadKind.Code)
	}
}

// TestCreatorAutoArchiveEndpointTogglesWatchlistSwitch pins the switch contract:
// only same-origin PUTs for a Douyin creator already on the watchlist may write,
// and the stored value must round-trip through the watchlist file.
func TestCreatorAutoArchiveEndpointTogglesWatchlistSwitch(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	const userID = "auto-toggle-user"
	if err := tikhub.AddWatch(dataDir, "douyin", userID, "自动归档博主", ""); err != nil {
		t.Fatal(err)
	}
	path := "/api/trend-radar/creators/douyin/" + userID + "/auto-archive"

	// 跨站必须 403，且不得写入。
	crossSite := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"enabled":true}`))
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "https://evil.example.com")
	server.ServeHTTP(crossSite, request)
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site status=%d want 403", crossSite.Code)
	}
	users, err := tikhub.LoadWatchlist(dataDir)
	if err != nil || len(users) != 1 || users[0].AutoArchive {
		t.Fatalf("cross-site request must not toggle: %#v err=%v", users, err)
	}

	enabled := callDataAPI(t, server, http.MethodPut, path, map[string]any{"enabled": true})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", enabled.Code, enabled.Body.String())
	}
	var enabledPayload struct {
		OK      bool `json:"ok"`
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(enabled.Body.Bytes(), &enabledPayload); err != nil {
		t.Fatalf("decode enable payload: %v body=%s", err, enabled.Body.String())
	}
	if !enabledPayload.OK || !enabledPayload.Enabled {
		t.Fatalf("enable payload=%#v", enabledPayload)
	}
	users, err = tikhub.LoadWatchlist(dataDir)
	if err != nil || len(users) != 1 || !users[0].AutoArchive {
		t.Fatalf("enabled switch did not persist: %#v err=%v", users, err)
	}

	disabled := callDataAPI(t, server, http.MethodPut, path, map[string]any{"enabled": false})
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disabled.Code, disabled.Body.String())
	}
	var disabledPayload struct {
		OK      bool `json:"ok"`
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(disabled.Body.Bytes(), &disabledPayload); err != nil {
		t.Fatalf("decode disable payload: %v body=%s", err, disabled.Body.String())
	}
	if !disabledPayload.OK || disabledPayload.Enabled {
		t.Fatalf("disable payload=%#v", disabledPayload)
	}

	if rec := callDataAPI(t, server, http.MethodPut,
		"/api/trend-radar/creators/xhs/"+userID+"/auto-archive", map[string]any{"enabled": true}); rec.Code != http.StatusBadRequest {
		t.Fatalf("xhs status=%d body=%s want 400", rec.Code, rec.Body.String())
	}
	if rec := callDataAPI(t, server, http.MethodPut,
		"/api/trend-radar/creators/douyin/missing-user/auto-archive", map[string]any{"enabled": true}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown creator status=%d body=%s want 404", rec.Code, rec.Body.String())
	}
	if rec := callDataAPI(t, server, http.MethodPut, path, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status=%d body=%s want 400", rec.Code, rec.Body.String())
	}
}

func writeRadarKeywordFixture(t *testing.T, dataDir, keyword, platform, callName string) {
	t.Helper()
	writeRadarFixture(t, dataDir, filepath.Join("tikhub/calls/2026-07-25", callName), map[string]any{
		"schema_version": 1,
		"call_id":        len(keyword)*100 + len(platform),
		"platform":       platform,
		"endpoint_id":    platform + ".keyword_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": keyword},
		"fetched_at":     "2026-07-25T03:04:05Z",
		"response": map[string]any{
			"data": map[string]any{"items": []any{
				map[string]any{"title": keyword + " 观测样本", "nickname": "样本博主"},
			}},
		},
	})
}

func TestTrendRadarAssignKeywordMovesUnassignedTermsIntoNewNiche(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarTestConfig()); err != nil {
		t.Fatal(err)
	}
	writeRadarKeywordFixture(t, dataDir, "私有化部署", "douyin", "1_douyin_keyword_search.json")
	writeRadarKeywordFixture(t, dataDir, "API账单", "xhs", "2_xhs_keyword_search.json")
	writeRadarKeywordFixture(t, dataDir, "AI省钱", "wechat_video", "3_wechat_video_keyword_search.json")
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	before, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var archivedBefore []string
	for _, niche := range before.Niches {
		if niche.ID == archivedRadarNicheID {
			archivedBefore = append(archivedBefore, niche.SearchTerms...)
		}
	}
	if len(archivedBefore) != 3 {
		t.Fatalf("expected 3 unassigned terms before assignment, got %#v", archivedBefore)
	}

	response := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/assign-keyword", map[string]any{
		"terms": []string{"私有化部署", "API账单", "AI省钱"},
		"new_niche": map[string]any{
			"name": "AI 降本池", "description": "本地部署与 API 成本",
		},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("assign status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		OK            bool     `json:"ok"`
		AssignedTerms []string `json:"assigned_terms"`
		TargetNicheID string   `json:"target_niche_id"`
		TargetNiche   string   `json:"target_niche_name"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode assign payload: %v body=%s", err, response.Body.String())
	}
	if !payload.OK || len(payload.AssignedTerms) != 3 || payload.TargetNicheID == "" || payload.TargetNiche != "AI 降本池" {
		t.Fatalf("assign payload=%#v", payload)
	}

	after, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	termNiche := map[string]string{}
	for _, niche := range after.Niches {
		for _, term := range niche.SearchTerms {
			termNiche[term] = niche.ID
		}
	}
	for _, term := range []string{"私有化部署", "API账单", "AI省钱"} {
		if termNiche[term] != payload.TargetNicheID {
			t.Fatalf("term %q still belongs to %q, want %q (niches=%#v)", term, termNiche[term], payload.TargetNicheID, after.Niches)
		}
	}
	if termNiche["AI工具"] != "tools" || termNiche["亲密关系"] != "relationships" {
		t.Fatalf("configured terms moved unexpectedly: %#v", termNiche)
	}
	for _, niche := range after.Niches {
		if niche.ID == archivedRadarNicheID {
			t.Fatalf("archived niche still holds terms after assignment: %#v", niche.SearchTerms)
		}
	}

	saved, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, niche := range saved.Niches {
		if niche.ID == payload.TargetNicheID {
			found = niche.Enabled && len(niche.SearchTerms) == 3 && len(niche.Platforms) > 0
		}
	}
	if !found {
		t.Fatalf("saved config missing enabled target niche: %#v", saved.Niches)
	}
}

func TestTrendRadarAssignKeywordRejectsInvalidTargets(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarTestConfig()); err != nil {
		t.Fatal(err)
	}
	writeRadarKeywordFixture(t, dataDir, "私有化部署", "douyin", "1_douyin_keyword_search.json")
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	missingTarget := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/assign-keyword", map[string]any{
		"terms": []string{"私有化部署"},
	})
	if missingTarget.Code != http.StatusBadRequest || !strings.Contains(missingTarget.Body.String(), "invalid_target") {
		t.Fatalf("missing target status=%d body=%s", missingTarget.Code, missingTarget.Body.String())
	}

	bothTargets := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/assign-keyword", map[string]any{
		"terms": []string{"私有化部署"}, "target_niche_id": "tools",
		"new_niche": map[string]any{"name": "新池"},
	})
	if bothTargets.Code != http.StatusBadRequest || !strings.Contains(bothTargets.Body.String(), "invalid_target") {
		t.Fatalf("both targets status=%d body=%s", bothTargets.Code, bothTargets.Body.String())
	}

	unknownTarget := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/assign-keyword", map[string]any{
		"terms": []string{"私有化部署"}, "target_niche_id": "missing-pool",
	})
	if unknownTarget.Code != http.StatusBadRequest || !strings.Contains(unknownTarget.Body.String(), "niche_not_found") {
		t.Fatalf("unknown target status=%d body=%s", unknownTarget.Code, unknownTarget.Body.String())
	}

	configuredTerm := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/assign-keyword", map[string]any{
		"terms": []string{"AI工具"}, "target_niche_id": "relationships",
	})
	if configuredTerm.Code != http.StatusBadRequest || !strings.Contains(configuredTerm.Body.String(), "invalid_terms") {
		t.Fatalf("configured term status=%d body=%s", configuredTerm.Code, configuredTerm.Body.String())
	}
}
