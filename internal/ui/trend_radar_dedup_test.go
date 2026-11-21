package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

// P2-1 去重键统一回归。同一作品跨轮只占一行且 Occurrences 递增；
// 同名不同作品ID 各自独立计数；榜单/聚合/导出三处同一键。

func radarDedupTestConfig() tikhub.RadarConfig {
	return tikhub.RadarConfig{Niches: []tikhub.RadarNiche{{
		ID: "relationships", Name: "关系观察", Enabled: true,
		SearchTerms: []string{"亲密关系"}, Platforms: []string{"douyin"},
	}}}
}

func writeDouyinSearchCall(t *testing.T, dataDir string, callID int, fetchedAt string, items []map[string]any) {
	t.Helper()
	payload := make([]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, item)
	}
	writeRadarFixture(t, dataDir, "tikhub/calls/"+fetchedAt[:10]+"/"+
		itoaRadarCallID(callID)+"_douyin_video_search.json", map[string]any{
		"schema_version": 1,
		"call_id":        callID,
		"platform":       "douyin",
		"endpoint_id":    "douyin.video_search",
		"capability":     "keyword_search",
		"parameters":     map[string]any{"keyword": "亲密关系"},
		"fetched_at":     fetchedAt,
		"response":       map[string]any{"data": payload},
	})
}

func itoaRadarCallID(id int) string {
	if id < 10 {
		return "0" + string(rune('0'+id))
	}
	return string(rune('0'+id/10)) + string(rune('0'+id%10))
}

func douyinItem(awemeID, desc, shareURL string) map[string]any {
	item := map[string]any{"aweme_id": awemeID, "desc": desc, "nickname": "作者"}
	if shareURL != "" {
		item["share_url"] = shareURL
	}
	return item
}

// 场景1：同一作品跨两轮（share_url 查询参数不同）→ 一行、Occurrences=2。
func TestRadarDedupMergesSameWorkAcrossRuns(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarDedupTestConfig()); err != nil {
		t.Fatal(err)
	}
	writeDouyinSearchCall(t, dataDir, 1, "2026-07-25T03:05:00Z", []map[string]any{
		douyinItem("W1", "同名作品甲", "https://www.iesdouyin.com/share/video/W1/?from=share&utm=1"),
	})
	writeDouyinSearchCall(t, dataDir, 2, "2026-07-26T03:05:00Z", []map[string]any{
		douyinItem("W1", "同名作品甲", "https://www.iesdouyin.com/share/video/W1/?from=copy&ts=999"),
	})

	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 7, 26, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 1 {
		t.Fatalf("signals rows = %d, want 1 (same aweme_id across two runs): %#v", len(got.Signals), got.Signals)
	}
	if got.Signals[0].VideoID != "W1" || got.Signals[0].Occurrences != 2 {
		t.Fatalf("merged row = %#v, want video_id=W1 occurrences=2", got.Signals[0])
	}
}

// 场景2：同名不同作品（不同作品ID）各自独立计数、不合并。
func TestRadarDedupKeepsSameTitleDifferentWorkIDsApart(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarDedupTestConfig()); err != nil {
		t.Fatal(err)
	}
	writeDouyinSearchCall(t, dataDir, 1, "2026-07-26T03:05:00Z", []map[string]any{
		douyinItem("W1", "同名作品甲", "https://www.iesdouyin.com/share/video/W1/?from=share"),
		douyinItem("W2", "同名作品甲", "https://www.iesdouyin.com/share/video/W2/?from=share"),
	})

	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 7, 26, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 2 {
		t.Fatalf("signals rows = %d, want 2 (same title, different ids): %#v", len(got.Signals), got.Signals)
	}
	byID := map[string]int{}
	for _, signal := range got.Signals {
		byID[signal.VideoID] = signal.Occurrences
	}
	if byID["W1"] != 1 || byID["W2"] != 1 {
		t.Fatalf("occurrences by id = %#v, want each 1", byID)
	}
}

// 场景3：无作品ID 的回退口径——URL 去查询参数后合并；无ID无URL 的条目在候选层
// 就不算作品（避免评论/推荐词杂项污染榜单），键函数的标题回退是最后防线。
func TestRadarDedupFallbackNormalizedURLThenTitle(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveRadarConfig(dataDir, radarDedupTestConfig()); err != nil {
		t.Fatal(err)
	}
	// 两条无 aweme_id、URL 查询参数不同 → 规范化后合并为一行。
	writeDouyinSearchCall(t, dataDir, 1, "2026-07-25T03:05:00Z", []map[string]any{
		map[string]any{"desc": "无ID作品", "nickname": "作者", "share_url": "https://www.iesdouyin.com/share/video/N1/?from=share"},
	})
	writeDouyinSearchCall(t, dataDir, 2, "2026-07-26T03:05:00Z", []map[string]any{
		map[string]any{"desc": "无ID作品", "nickname": "作者", "share_url": "https://www.iesdouyin.com/share/video/N1/?from=copy&ts=1"},
	})
	// 两条无 ID 无 URL、同标题 → 候选层拦截，不进榜单。
	writeDouyinSearchCall(t, dataDir, 3, "2026-07-26T04:05:00Z", []map[string]any{
		map[string]any{"desc": "只有标题的作品", "nickname": "作者"},
	})
	writeDouyinSearchCall(t, dataDir, 4, "2026-07-26T05:05:00Z", []map[string]any{
		map[string]any{"desc": "只有标题的作品", "nickname": "作者"},
	})

	got, err := buildTrendRadarSummary(dataDir, time.Date(2026, 7, 26, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 1 {
		t.Fatalf("signals rows = %d, want 1 (url fallback merged; title-only items stay out): %#v", len(got.Signals), got.Signals)
	}
	if got.Signals[0].VideoID != "" || got.Signals[0].Occurrences != 2 {
		t.Fatalf("url-fallback row = %#v, want video_id empty occurrences=2", got.Signals[0])
	}
	key := radarWorkIdentity(got.Signals[0].VideoID, got.Signals[0].URL, got.Signals[0].Title)
	if key != "https://www.iesdouyin.com/share/video/N1/" {
		t.Fatalf("url fallback key = %q, want query params stripped", key)
	}
}

// 键函数三级口径单独回归：作品ID → 规范化URL → 小写标题。
func TestRadarWorkIdentityResolutionOrder(t *testing.T) {
	if got := radarWorkIdentity("W1", "https://a.example/x?p=1", "标题"); got != "W1" {
		t.Fatalf("id wins: %q", got)
	}
	if got := radarWorkIdentity("", "https://A.Example/x?p=1#f", "标题"); got != "https://a.example/x" {
		t.Fatalf("url fallback: %q", got)
	}
	if got := radarWorkIdentity("  ", "", " Mixed CASE 标题 "); got != "mixed case 标题" {
		t.Fatalf("title fallback: %q", got)
	}
	if got := radarWorkDedupKey("douyin", "W1", "", ""); got != "douyin|W1" {
		t.Fatalf("platform scoping: %q", got)
	}
}

// 场景4：榜单/聚合/导出三处同一键——导出 Occurrences 与页面聚合行一致。
func TestRadarExportUsesSameDedupKeyAsBoard(t *testing.T) {
	server, dataDir := newRadarRunTestServer(t)
	if _, err := tikhub.SaveRadarConfig(dataDir, radarDedupTestConfig()); err != nil {
		t.Fatal(err)
	}
	writeDouyinSearchCall(t, dataDir, 1, "2026-07-25T03:05:00Z", []map[string]any{
		douyinItem("W1", "同名作品甲", "https://www.iesdouyin.com/share/video/W1/?from=share&utm=1"),
	})
	writeDouyinSearchCall(t, dataDir, 2, "2026-07-26T03:05:00Z", []map[string]any{
		douyinItem("W1", "同名作品甲", "https://www.iesdouyin.com/share/video/W1/?from=copy&ts=999"),
	})

	backfill := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/store/backfill", map[string]any{})
	if backfill.Code != http.StatusOK {
		t.Fatalf("backfill status=%d body=%s", backfill.Code, backfill.Body.String())
	}
	// inserted 的语义是「新观测」，两轮各记一条观测；关键是作品只有一行、seen_count=2。
	seenCounts, err := server.db.RadarWorkSeenCounts()
	if err != nil {
		t.Fatal(err)
	}
	if seenCounts["douyin|W1"] != 2 {
		t.Fatalf("store seen counts = %#v, want douyin|W1 -> 2", seenCounts)
	}
	if len(seenCounts) != 1 {
		t.Fatalf("store work rows = %d, want 1: %#v", len(seenCounts), seenCounts)
	}

	summary := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/summary", nil)
	if summary.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", summary.Code, summary.Body.String())
	}
	var summaryPayload struct {
		Signals []struct {
			VideoID     string `json:"video_id"`
			Occurrences int    `json:"occurrences"`
		} `json:"signals"`
	}
	if err := json.Unmarshal(summary.Body.Bytes(), &summaryPayload); err != nil {
		t.Fatalf("decode summary: %v body=%s", err, summary.Body.String())
	}
	boardOccurrences := 0
	for _, signal := range summaryPayload.Signals {
		if signal.VideoID == "W1" {
			boardOccurrences = signal.Occurrences
		}
	}
	if len(summaryPayload.Signals) != 1 || boardOccurrences != 2 {
		t.Fatalf("board signals=%d occurrences=%d, want 1/2", len(summaryPayload.Signals), boardOccurrences)
	}

	export := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/export?niche_id=relationships&format=csv", nil)
	if export.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", export.Code, export.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(export.Body.String(), "\uFEFF")), "\n")
	if len(lines) != 2 {
		t.Fatalf("csv rows = %d, want header + 1: %s", len(lines), export.Body.String())
	}
	fields := strings.Split(lines[1], ",")
	if len(fields) < 17 {
		t.Fatalf("csv row has %d fields: %s", len(fields), lines[1])
	}
	if workID := strings.TrimSpace(fields[16]); workID != "W1" {
		t.Fatalf("csv work id = %q, want W1", workID)
	}
	if occurrences := strings.TrimSpace(fields[13]); occurrences != "2" {
		t.Fatalf("csv occurrences = %q, want 2 (same key as board)", occurrences)
	}
}
