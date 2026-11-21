package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
)

// 这两份 fixture 照 2026-11-13 的真实响应形态写：小红书一次搜索 20 条 =
// 17 条 model_type=note + 3 张 model_type=ads 广告卡（第 1、7、13 条，0 基），
// 广告卡里有 ads_id、ads.ad_tag 和一张推广笔记，第 1、7 张还带 CTA 按钮；
// 抖音一次搜索 8 条 = 7 张作品卡 + 1 张 douyin.doctype_related_search 引导卡。

func radarXHSCardFixtureResponse() map[string]interface{} {
	adsIndexes := map[int]string{1: "5645674721", 7: "6728918829", 13: "7180533959"}
	adNoteIDs := map[int]string{
		1: "69f1d0dc0000000035030123", 7: "69aa9ac5000000001a01e167", 13: "6aec1471000000001a02b6ee",
	}
	ctaTitles := map[int]string{1: "立即预约", 7: "立即咨询"}
	items := make([]any, 0, 20)
	for index := range 20 {
		if adsID, isAd := adsIndexes[index]; isAd {
			ads := map[string]any{
				"model_type": "ads",
				"ad_tag":     "Ads",
				"ads_id":     adsID,
				"extra_json": "{\"ads_engagebar_info\":{}}",
				"note": map[string]any{
					"id": adNoteIDs[index], "title": "推广笔记 " + adsID, "user": map[string]any{"nickname": "广告主"},
					"liked_count": 3080, "collected_count": 2998,
				},
			}
			if cta, hasCTA := ctaTitles[index]; hasCTA {
				ads["widgets_fallback"] = map[string]any{
					"ads_engage_bar": map[string]any{
						"dynamic_card_info": map[string]any{"title": cta, "subtitle": "了解更多"},
					},
				}
			}
			items = append(items, map[string]any{"model_type": "ads", "ads": ads, "mix_track_id": "ADS__" + adsID})
			continue
		}
		items = append(items, map[string]any{
			"model_type": "note",
			"note": map[string]any{
				"id":    fmt.Sprintf("6aba7ac5000000001a024d%02x", index),
				"title": "自然笔记 " + strconv.Itoa(index),
				"user":  map[string]any{"nickname": "作者"}, "liked_count": 120,
			},
		})
	}
	return map[string]interface{}{
		"data": map[string]interface{}{
			"data": map[string]interface{}{"items": items},
		},
	}
}

func radarDouyinCardFixtureResponse() map[string]interface{} {
	entries := make([]any, 0, 8)
	for index := range 7 {
		awemeID := "770044278730512076" + strconv.Itoa(index)
		entries = append(entries, map[string]any{
			"card_id": "douyin.doctype_aweme_video.video_search_video_new_double",
			"type":    1,
			"data": map[string]any{
				"card_id": "douyin.doctype_aweme_video.video_search_video_new_double",
				"aweme_info": map[string]any{
					"aweme_id":      awemeID,
					"desc":          "真实作品 " + awemeID,
					"commerce_info": map[string]any{"ad_type": 0, "is_ad": false},
				},
			},
		})
	}
	entries = append(entries, map[string]any{
		"card_id": "douyin.doctype_related_search.video_search_realted_word_new_double",
		"type":    66668,
		"data": map[string]any{
			"card_id": "douyin.doctype_related_search.video_search_realted_word_new_double",
			"data":    map[string]any{"rs_extra_info": map[string]any{"query_id": "7706045619072324883"}},
		},
	})
	return map[string]interface{}{
		"data": map[string]interface{}{"business_data": entries},
	}
}

func TestRadarClassifyResultCardsSplitsXHSAdsFromWorks(t *testing.T) {
	tally := radarClassifyResultCards("xhs", radarXHSCardFixtureResponse())
	if !tally.Found {
		t.Fatal("xhs 结果列表形态应当被识别")
	}
	if tally.Entries != 20 || tally.Works != 17 || tally.Ads != 3 || tally.Guides != 0 || tally.Unreadable != 0 {
		t.Fatalf("逐条账不对: %#v", tally)
	}
	if tally.Works+tally.Ads+tally.Guides+tally.Unreadable != tally.Entries {
		t.Fatalf("四个数字相加必须等于列表条数: %#v", tally)
	}
	if len(tally.Samples) != 3 {
		t.Fatalf("三张广告卡都要留下依据: %#v", tally.Samples)
	}
	adsIDs := map[string]bool{}
	for _, sample := range tally.Samples {
		if sample.Kind != radarCardKindAd || sample.Marker != "model_type=ads" {
			t.Fatalf("广告卡依据不对: %#v", sample)
		}
		if sample.Title == "" {
			t.Fatalf("广告卡自带标题要记下来: %#v", sample)
		}
		adsIDs[sample.AdID] = true
	}
	for _, want := range []string{"5645674721", "6728918829", "7180533959"} {
		if !adsIDs[want] {
			t.Fatalf("缺广告位 id %s: %#v", want, tally.Samples)
		}
	}
}

func TestRadarClassifyResultCardsCountsDouyinGuideCard(t *testing.T) {
	tally := radarClassifyResultCards("douyin", radarDouyinCardFixtureResponse())
	if !tally.Found {
		t.Fatal("抖音结果列表形态应当被识别")
	}
	if tally.Entries != 8 || tally.Works != 7 || tally.Ads != 0 || tally.Guides != 1 || tally.Unreadable != 0 {
		t.Fatalf("逐条账不对: %#v", tally)
	}
	if len(tally.Samples) != 1 || tally.Samples[0].Kind != radarCardKindGuide {
		t.Fatalf("引导卡依据不对: %#v", tally.Samples)
	}
	if tally.Samples[0].Marker != "card_id=douyin.doctype_related_search.video_search_realted_word_new_double" {
		t.Fatalf("引导卡 marker 要写原始 card_id: %#v", tally.Samples[0])
	}
}

func TestRadarClassifyResultCardsCountsPromotedVideoByIsAd(t *testing.T) {
	// 本轮真实样本里 is_ad 全是 false，这条按平台自己的字段判：采到 true 的
	// 那一天它要算广告卡，而不是算作品。
	response := radarDouyinCardFixtureResponse()
	entries := response["data"].(map[string]interface{})["business_data"].([]any)
	promoted := entries[0].(map[string]any)
	promoted["data"].(map[string]any)["aweme_info"].(map[string]any)["commerce_info"] = map[string]any{
		"ad_type": 1, "is_ad": true, "ad_id": "ad-9001",
	}
	tally := radarClassifyResultCards("douyin", response)
	if tally.Entries != 8 || tally.Works != 6 || tally.Ads != 1 || tally.Guides != 1 || tally.Unreadable != 0 {
		t.Fatalf("is_ad=true 应计 1 张广告卡: %#v", tally)
	}
	if tally.Samples[0].Kind != radarCardKindAd || tally.Samples[0].AdID != "ad-9001" {
		t.Fatalf("广告卡依据不对: %#v", tally.Samples)
	}
	if tally.Samples[0].Marker != "data.aweme_info.commerce_info.is_ad=true" {
		t.Fatalf("广告卡 marker 要写命中路径: %#v", tally.Samples[0])
	}
}

func TestRadarCandidateItemsSkipAdCardsAndCTAs(t *testing.T) {
	items := radarCandidateItems("xhs", radarXHSCardFixtureResponse())
	if len(items) != 17 {
		t.Fatalf("广告卡与 CTA 都不该进候选，want 17 got %d", len(items))
	}
	for _, item := range items {
		if radarString(item, "model_type") == "ads" {
			t.Fatalf("广告卡子树漏进候选: %#v", item)
		}
		title := radarString(item, "title", "display_title")
		if title == "立即预约" || title == "立即咨询" {
			t.Fatalf("CTA 按钮漏进候选: %#v", item)
		}
		if id := radarString(item, "id"); id == "69f1d0dc0000000035030123" || id == "69aa9ac5000000001a01e167" || id == "6aec1471000000001a02b6ee" {
			t.Fatalf("广告卡的推广笔记漏进候选: %#v", item)
		}
	}
}

func TestBuildTrendRadarSummaryCountsCardsWithoutPollutingSignals(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarTestConfig()); err != nil {
		t.Fatal(err)
	}
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-11-13/216_xhs_note_search.json", map[string]any{
		"schema_version": 1,
		"call_id":        216,
		"platform":       "xhs",
		"endpoint_id":    "xhs.note_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "AI工具"},
		"fetched_at":     "2026-11-13T02:03:12Z",
		"response":       radarXHSCardFixtureResponse(),
	})
	writeRadarFixture(t, dataDir, "tikhub/calls/2026-11-13/217_douyin_video_search.json", map[string]any{
		"schema_version": 1,
		"call_id":        217,
		"platform":       "douyin",
		"endpoint_id":    "douyin.video_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "亲密关系"},
		"fetched_at":     "2026-11-13T02:04:00Z",
		"response":       radarDouyinCardFixtureResponse(),
	})

	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.AdCards != 3 || got.GuideCards != 1 || got.Unreadable != 0 {
		t.Fatalf("卡计数不对: ads=%d guides=%d unreadable=%d", got.AdCards, got.GuideCards, got.Unreadable)
	}
	if len(got.Signals) != 24 {
		t.Fatalf("榜单只该有 17 条小红书笔记 + 7 条抖音作品: got %d", len(got.Signals))
	}
	for _, signal := range got.Signals {
		for _, adNoteID := range []string{"69f1d0dc0000000035030123", "69aa9ac5000000001a01e167", "6aec1471000000001a02b6ee"} {
			if signal.VideoID == adNoteID {
				t.Fatalf("广告卡的推广笔记进了榜单: %#v", signal)
			}
		}
	}
}

func TestRadarClassifyResultCardsUnknownShapeReportsNotFound(t *testing.T) {
	tally := radarClassifyResultCards("xhs", map[string]interface{}{"data": map[string]interface{}{"items": "not-a-list"}})
	if tally.Found {
		t.Fatalf("形态不认识时必须 Found=false: %#v", tally)
	}
	if tally.Ads != 0 || tally.Guides != 0 || tally.Unreadable != 0 {
		t.Fatalf("形态不认识时数字不作数: %#v", tally)
	}

	// summary 遇到不认识的形态：三个计数保持不动，不假装 0 条读不到。
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarTestConfig()); err != nil {
		t.Fatal(err)
	}
	writeRadarFixture(t, dataDir, filepath.Join("tikhub", "calls", "2026-11-13", "300_xhs_note_search.json"), map[string]any{
		"schema_version": 1,
		"call_id":        300,
		"platform":       "xhs",
		"endpoint_id":    "xhs.note_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "AI工具"},
		"fetched_at":     "2026-11-13T02:05:00Z",
		"response":       map[string]any{"data": map[string]any{"items": "not-a-list"}},
	})
	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.AdCards != 0 || got.GuideCards != 0 || got.Unreadable != 0 {
		t.Fatalf("不认识的形态不该写进计数: ads=%d guides=%d unreadable=%d", got.AdCards, got.GuideCards, got.Unreadable)
	}
}

func TestRadarNicheRunRecordsAdCountsFromRealCardShape(t *testing.T) {
	server, _ := newRadarRunTestServer(t)
	server.tikhubCallEndpoint = func(ctx context.Context, _ tikhub.EndpointDefinition, _ map[string]any) (map[string]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return radarXHSCardFixtureResponse(), nil
	}

	response := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "xhs", "terms": []string{"词一"}, "confirm": true,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", response.Code, response.Body.String())
	}
	var run radar.Run
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	// 抓回 20 条里 3 张广告卡单列；卡不算读不到，也不占保留名额的解释。
	if run.Items != 20 || run.AdCards != 3 || run.GuideCards != 0 || run.Unreadable != 0 {
		t.Fatalf("run 卡计数不对: items=%d ads=%d guides=%d unreadable=%d", run.Items, run.AdCards, run.GuideCards, run.Unreadable)
	}
	if len(run.Calls) != 1 || run.Calls[0].AdCount != 3 || run.Calls[0].UnreadableCount != 0 {
		t.Fatalf("调用明细缺卡计数: %+v", run.Calls)
	}
	if run.Kept != tikhub.RadarXHSItemCap {
		t.Fatalf("保留名额仍是平台上限: kept=%d", run.Kept)
	}

	// 落库：抓取记录接口优先读 SQLite，读到的必须是同一次调用的同一组数。
	runsResponse := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/runs?niche_id=niche-test", nil)
	if runsResponse.Code != http.StatusOK {
		t.Fatalf("runs status=%d body=%s", runsResponse.Code, runsResponse.Body.String())
	}
	var runsPayload struct {
		Runs []radar.Run `json:"runs"`
	}
	if err := json.Unmarshal(runsResponse.Body.Bytes(), &runsPayload); err != nil {
		t.Fatal(err)
	}
	stored := false
	for _, storedRun := range runsPayload.Runs {
		if storedRun.ID != run.ID {
			continue
		}
		stored = true
		if storedRun.AdCards != 3 || len(storedRun.Calls) != 1 || storedRun.Calls[0].AdCount != 3 {
			t.Fatalf("SQLite 里的卡计数不对: %+v", storedRun)
		}
	}
	if !stored {
		t.Fatalf("run %s 没落到 SQLite", run.ID)
	}

	// 榜单：广告卡的推广笔记不进榜单，summary 里 3 张广告卡单列。
	summaryResponse := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/summary", nil)
	if summaryResponse.Code != http.StatusOK {
		t.Fatalf("summary status=%d", summaryResponse.Code)
	}
	var summary trendRadarSummary
	if err := json.Unmarshal(summaryResponse.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.AdCards != 3 || summary.GuideCards != 0 || summary.Unreadable != 0 {
		t.Fatalf("summary 卡计数不对: ads=%d guides=%d unreadable=%d", summary.AdCards, summary.GuideCards, summary.Unreadable)
	}
	if len(summary.Signals) != tikhub.RadarXHSItemCap {
		t.Fatalf("榜单该是 17 条真笔记截到保留名额 %d，got %d", tikhub.RadarXHSItemCap, len(summary.Signals))
	}
	for _, signal := range summary.Signals {
		if radarString(map[string]interface{}{"id": signal.VideoID}, "id") == "69f1d0dc0000000035030123" {
			t.Fatalf("广告卡的推广笔记进了榜单: %#v", signal)
		}
	}
}
