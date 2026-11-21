package egolite

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCreatorMetricSnapshotDateUsesTheLocalCalendarDay(t *testing.T) {
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	collectedAt := time.Date(2026, 8, 2, 0, 30, 0, 0, shanghai)
	if got := creatorMetricSnapshotDate(collectedAt); got != "2026-08-02" {
		t.Fatalf("snapshot date=%q, want local calendar day 2026-08-02", got)
	}
}

func TestCreatorMetricsDOMExpressionCompiles(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	program := "new Function('return ' + " + strconv.Quote(creatorMetricsDOMExpression) + ")\n"
	if output, err := exec.Command(node, "--input-type=module", "--eval", program).CombinedOutput(); err != nil {
		t.Fatalf("creator metrics DOM expression is invalid: %v\n%s", err, output)
	}
}

func TestCreatorMetricsRecognizesExplicitSevenDayVariants(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	for _, marker := range []string{"近7天", "最近 7 日", "过去7日", "Last 7 days"} {
		expression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", `[]`, 1)
		expression = strings.Replace(expression, "__ALPHA_SUFFIXES__", `[]`, 1)
		expression = strings.Replace(expression, "__ALPHA_RESOURCES__", `[]`, 1)
		script := `globalThis.document = {
  body: { innerText: ` + strconv.Quote(marker+" 数据概览 播放量 120") + ` },
  title: "metrics", querySelector: () => null, querySelectorAll: () => []
};
globalThis.location = { href: "https://example.com/metrics" };
process.stdout.write(JSON.stringify(` + expression + `));`
		output, runErr := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput()
		if runErr != nil {
			t.Fatalf("evaluate %q: %v\n%s", marker, runErr, output)
		}
		var result struct {
			RangeVerified bool `json:"rangeVerified"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("decode %q: %v\n%s", marker, err, output)
		}
		if !result.RangeVerified {
			t.Fatalf("explicit seven-day marker %q was not recognized", marker)
		}
	}
}

func TestCreatorMetricsBindsPagePeriodPanelInsteadOfAllTimeNumbers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	expression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_SUFFIXES__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_RESOURCES__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_PLATFORM_CODE__", `xhs`, 1)
	script := `const panel = { innerText: "近7日 观看数 5 点赞 0 收藏 0" };
globalThis.document = {
  body: { innerText: "账号累计 观看数 9999 点赞 800" },
  title: "metrics",
  querySelector: () => null,
  querySelectorAll: (selector) => selector === "section,article,div,main" ? [panel] : []
};
globalThis.location = { href: "https://creator.xiaohongshu.com/statistics/account/v2" };
globalThis.performance = { getEntriesByType: () => [] };
process.stdout.write(JSON.stringify(` + expression + `));`
	output, runErr := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput()
	if runErr != nil {
		t.Fatalf("evaluate panel: %v\n%s", runErr, output)
	}
	var result struct {
		Metrics       map[string]int64 `json:"metrics"`
		MetricScope   string           `json:"metricScope"`
		ScopeSource   string           `json:"scopeSource"`
		RangeVerified bool             `json:"rangeVerified"`
		Evidence      map[string]struct {
			ValueText string `json:"value_text"`
		} `json:"metricEvidence"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if result.Metrics["views"] != 5 || result.MetricScope != "window" || result.ScopeSource != "page_period_panel" || !result.RangeVerified {
		t.Fatalf("period panel was not bound: %+v", result)
	}
	if result.Evidence["views"].ValueText != "5" || result.Evidence["likes"].ValueText != "0" {
		t.Fatalf("panel values must quote the page text: %+v", result.Evidence)
	}
}

func TestCreatorMetricsUsesVisibleSelectedSevenDayControlAsScope(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	expression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_SUFFIXES__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_RESOURCES__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_PLATFORM_CODE__", `xhs`, 1)
	script := `const active = { innerText: "近7日", getAttribute: () => null, getBoundingClientRect: () => ({ width: 80, height: 24 }) };
globalThis.getComputedStyle = () => ({ display: "block", visibility: "visible" });
globalThis.document = {
  body: { innerText: "数据看板 近7日 近30日 观看数 0" },
  title: "metrics",
  querySelector: () => null,
  querySelectorAll: (selector) => selector.includes("aria-selected") ? [active] : []
};
globalThis.location = { href: "https://creator.xiaohongshu.com/statistics/account/v2" };
globalThis.performance = { getEntriesByType: () => [] };
process.stdout.write(JSON.stringify(` + expression + `));`
	output, runErr := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput()
	if runErr != nil {
		t.Fatalf("evaluate selected control: %v\n%s", runErr, output)
	}
	var result struct {
		MetricScope string `json:"metricScope"`
		ScopeSource string `json:"scopeSource"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if result.MetricScope != "window" || result.ScopeSource != "selected_seven_day_control" {
		t.Fatalf("selected 7-day control was not used as scope: %+v", result)
	}
}

func TestCreatorMetricsKeepsPageWideAnalyticsSeparateFromDatedContentRows(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	for _, required := range []string{
		"const analyticsRangeVerified = Boolean(dom.rangeVerified)",
		"const contentRangeVerified = Boolean(dom.contentRangeVerified && dom.posts.length > 0)",
		"analyticsRangeVerified && hasMeasuredMetric",
		`metricResult.evidenceCode = "REVIEWED_CONTENT_DOM"`,
		"views: dom.posts.reduce",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("creator metric DOM decision missing %q", required)
		}
	}
	if strings.Contains(program, "const verifiedRange = Boolean(dom.rangeVerified || dom.contentRangeVerified)") {
		t.Fatal("dated content rows still promote unrelated page-wide analytics to a verified seven-day snapshot")
	}
}

func TestCreatorMetricsUsesObservedWeChatVideoRoutes(t *testing.T) {
	platform := creatorMetricPlatformCatalog["wechat_video"]
	joined := strings.Join(platform.Candidates, "\n")
	for _, route := range []string{
		"https://channels.weixin.qq.com/micro/statistic/statistics/overview",
		"https://channels.weixin.qq.com/micro/statistic/statistics",
	} {
		if !strings.Contains(joined, route) {
			t.Fatalf("missing observed video-channel analytics route %q: %v", route, platform.Candidates)
		}
	}
}

func TestCreatorMetricsUsesObservedWeChatOfficialAccountAnalyticsRoutes(t *testing.T) {
	platform := creatorMetricPlatformCatalog["wechat_mp"]
	joined := strings.Join(platform.Candidates, "\n")
	for _, route := range []string{
		"https://mp.weixin.qq.com/misc/appmsganalysis",
		"https://mp.weixin.qq.com/misc/useranalysis",
	} {
		if !strings.Contains(joined, route) {
			t.Fatalf("missing observed official-account analytics route %q: %v", route, platform.Candidates)
		}
	}
}

func TestCreatorMetricsUsesObservedYouTubeStudioResourcePaths(t *testing.T) {
	platform := creatorMetricPlatformCatalog["youtube"]
	joined := strings.Join(platform.Resources, "\n")
	for _, resource := range []string{
		"/youtubei/v1/creator/get_channel_dashboard",
		"/youtubei/v1/creator/list_creator_videos",
	} {
		if !strings.Contains(joined, resource) {
			t.Fatalf("missing observed YouTube Studio resource path %q: %v", resource, platform.Resources)
		}
	}
}

func TestCreatorMetricsBuildsSevenDayPostsFromObservedYouTubeVideoResource(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	for _, required := range []string{
		`platform.code === "youtube" && parsedURL.pathname === "/youtubei/v1/creator/get_creator_videos"`,
		`payload?.videos`,
		`item?.timePublishedSeconds`,
		`item?.metrics?.viewCount`,
		`item?.metrics?.likeCount`,
		`item?.metrics?.commentCount`,
		`resource_complete_7_day_window`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("YouTube seven-day resource extractor missing %q", required)
		}
	}
}

func TestCreatorMetricsLimitsMetaToOneTrustedInsightsAttempt(t *testing.T) {
	platform := creatorMetricPlatformCatalog["meta"]
	if platform.MaxCandidates != 1 {
		t.Fatalf("Meta max candidates=%d, want one trusted insights attempt", platform.MaxCandidates)
	}
}

func TestCreatorMetricsLimitsYouTubeToTheBoundChannelAnalyticsAttempt(t *testing.T) {
	platform := creatorMetricPlatformCatalog["youtube"]
	if platform.MaxCandidates != 1 {
		t.Fatalf("YouTube max candidates=%d, want one channel-analytics attempt", platform.MaxCandidates)
	}
}

func TestCreatorMetricsLimitsDouyinToTheReviewedDataCenterAttempt(t *testing.T) {
	platform := creatorMetricPlatformCatalog["douyin"]
	if platform.MaxCandidates != 1 {
		t.Fatalf("Douyin max candidates=%d, want one reviewed data-center attempt", platform.MaxCandidates)
	}
}

func TestCreatorMetricsRetriesOnlyARecognizedSkeletonAnalyticsPage(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	for _, required := range []string{
		`const skeletonAnalyticsPage = hostMatches && dom.reviewedMarker && Object.keys(dom.metrics || {}).length === 0 && Number(dom.textLength || 0) < 1200`,
		`await new Promise((resolve) => setTimeout(resolve, 1200))`,
		`dom = await js(domExpression)`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("recognized analytics skeleton retry missing %q", required)
		}
	}
}

func TestCreatorMetricsOnlyChangesYouTubeAnalyticsToAnExplicitSevenDayOption(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	for _, required := range []string{
		`platform.code === "youtube"`,
		`metricResult.stage = "SELECT_SEVEN_DAY_RANGE"`,
		`Last 28 days`,
		`Last 7 days`,
		`dispatchEvent(new MouseEvent(`,
		`closest('button,[role=button],ytcp-button,ytcp-dropdown-trigger,[aria-haspopup],[tabindex]')`,
		`if(targets.length!==1)return false`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("YouTube seven-day selector missing %q", required)
		}
	}
}

func TestCreatorMetricsOnlyChangesMetaInsightsToAnExplicitSevenDayOption(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	for _, required := range []string{
		`platform.code === "meta"`,
		`metricResult.stage = "SELECT_SEVEN_DAY_RANGE"`,
		`过去\\s*28\\s*天`,
		`过去\\s*7\\s*天`,
		`switchMetaToSevenDays`,
		`document.querySelectorAll('button,[role=button],[role=combobox],[aria-haspopup=listbox]')`,
		`getAttribute('aria-label')`,
		`[node.getAttribute('aria-label'),node.innerText,node.textContent].join(' ')`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Meta seven-day selector missing %q", required)
		}
	}
}

func TestCreatorMetricsRequiresVisibleLoginEvidenceBeforeSkipping(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	for _, required := range []string{
		`const visibleLoginInput`,
		`const loginPrompt =`,
		`const loginForm = visibleLoginInput && loginPrompt`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("login evidence guard missing %q", required)
		}
	}
	if strings.Contains(program, `Boolean(document.querySelector('input[type="password"]`) {
		t.Fatal("a hidden password input must not by itself mark a creator backend logged out")
	}
}

func TestCreatorMetricsAcceptsOnlyChronologicalDouyinCoverageBoundary(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	for _, required := range []string{
		"const publishedTimes = payload.aweme_list",
		"const chronological = publishedTimes.every",
		"const reachedBeforeWindow = publishedTimes.some",
		`if (!hasMore || (chronological && reachedBeforeWindow)) rangeHints.push("resource_complete_7_day_window")`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Douyin seven-day coverage boundary missing %q", required)
		}
	}
}

func TestCreatorMetricsBoundsNestedResourceShapeDiagnostics(t *testing.T) {
	source, err := os.ReadFile("creator_metrics.go")
	if err != nil {
		t.Fatal(err)
	}
	program := string(source)
	if !strings.Contains(program, "if (depth > 5 || value == null || shapeHints.length >= 40) return") {
		t.Fatal("nested resource diagnostics must reach reviewed item fields while staying bounded")
	}
}

func TestCreatorMetricsUsesObservedWeChatVideoResourcePaths(t *testing.T) {
	platform := creatorMetricPlatformCatalog["wechat_video"]
	joined := strings.Join(platform.Resources, "\n")
	for _, resource := range []string{
		"/micro/statistic/cgi-bin/mmfinderassistant-bin/helper/helper_mmdata",
		"/micro/statistic/cgi-bin/mmfinderassistant-bin/helper/hepler_merlin_mmdata",
		"/micro/content/cgi-bin/mmfinderassistant-bin/post/post_list",
	} {
		if !strings.Contains(joined, resource) {
			t.Fatalf("missing observed video-channel resource path %q: %v", resource, platform.Resources)
		}
	}
}

func TestCreatorMetricsCollectsOnlySanitizedSamePlatformResourceHints(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	expression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_SUFFIXES__", `["xiaohongshu.com"]`, 1)
	expression = strings.Replace(expression, "__ALPHA_RESOURCES__", `["/api/sns/web/v1/data/overview"]`, 1)
	script := `globalThis.document = {
  body: { innerText: "数据看板" }, title: "metrics",
  querySelector: () => null, querySelectorAll: () => []
};
globalThis.location = { href: "https://creator.xiaohongshu.com/statistics/data-analysis" };
Object.defineProperty(globalThis, "performance", { value: { getEntriesByType: () => [
  { name: "https://edith.xiaohongshu.com/api/sns/web/v1/data/overview?token=secret" },
  { name: "https://evil.example/api/data?token=secret" },
  { name: "https://creator.xiaohongshu.com/assets/app.js" },
  { name: "https://res.xiaohongshu.com/statistic/app.js" }
] } });
process.stdout.write(JSON.stringify(` + expression + `));`
	output, runErr := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput()
	if runErr != nil {
		t.Fatalf("evaluate resource hints: %v\n%s", runErr, output)
	}
	var result struct {
		ResourceHints []string `json:"resourceHints"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode resource hints: %v\n%s", err, output)
	}
	want := []string{"https://edith.xiaohongshu.com/api/sns/web/v1/data/overview"}
	if strings.Join(result.ResourceHints, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resource hints=%v, want query-free same-platform paths %v", result.ResourceHints, want)
	}
}

func TestCreatorMetricsCollectsReviewedWeChatMPPerformancePathHintsOnly(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	expression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", `[]`, 1)
	expression = strings.Replace(expression, "__ALPHA_SUFFIXES__", `["weixin.qq.com"]`, 1)
	expression = strings.Replace(expression, "__ALPHA_RESOURCES__", `[]`, 1)
	script := `globalThis.document = {
  body: { innerText: "数据分析" }, title: "metrics",
  querySelector: () => null, querySelectorAll: () => []
};
globalThis.location = { href: "https://mp.weixin.qq.com/misc/useranalysis" };
Object.defineProperty(globalThis, "performance", { value: { getEntriesByType: () => [
  { name: "https://mp.weixin.qq.com/cgi-bin/report?token=secret" },
  { name: "https://mp.weixin.qq.com/misc/profile?from=menu" },
  { name: "https://mp.weixin.qq.com/datacube/summary?session=secret" },
  { name: "https://mp.weixin.qq.com/cgi-bin/app.js?cache=1" },
  { name: "https://mp.weixin.qq.com/cgi-bin/secret-token/metrics" },
  { name: "https://evil.example/cgi-bin/report?token=secret" }
] } });
process.stdout.write(JSON.stringify(` + expression + `));`
	output, runErr := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput()
	if runErr != nil {
		t.Fatalf("evaluate WeChat MP resource hints: %v\n%s", runErr, output)
	}
	var result struct {
		ResourceHints []string `json:"resourceHints"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode WeChat MP resource hints: %v\n%s", err, output)
	}
	want := []string{
		"https://mp.weixin.qq.com/cgi-bin/report",
		"https://mp.weixin.qq.com/misc/profile",
		"https://mp.weixin.qq.com/datacube/summary",
	}
	if strings.Join(result.ResourceHints, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resource hints=%v, want reviewed query-free WeChat MP paths %v", result.ResourceHints, want)
	}
}

func TestCreatorMetricsExtractsVerifiedSevenDayDOMForEachNonDouyinPlatform(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is unavailable")
	}
	fixtures := []struct {
		platform  string
		body      string
		wantViews int64
		wantLikes int64
		wantPosts int64
	}{
		{platform: "wechat_video", body: "数据中心 近7天 视频播放量 1,240 点赞量 82 评论量 9 分享量 4 发布视频数 3", wantViews: 1240, wantLikes: 82, wantPosts: 3},
		{platform: "kuaishou", body: "数据中心 最近7日 作品播放量 2.4万 获赞量 320 评论量 21 分享量 11 作品数 6", wantViews: 24000, wantLikes: 320, wantPosts: 6},
		{platform: "xhs", body: "数据看板 过去 7 天 笔记浏览量 980 点赞量 75 评论量 12 收藏量 44 发布笔记数 5", wantViews: 980, wantLikes: 75, wantPosts: 5},
		{platform: "youtube", body: "Channel analytics Last 7 days Views 1,240 Likes 82 Comments 9 Videos 3", wantViews: 1240, wantLikes: 82, wantPosts: 3},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.platform, func(t *testing.T) {
			platform := creatorMetricPlatformCatalog[fixture.platform]
			markers, _ := json.Marshal(platform.Markers)
			suffixes, _ := json.Marshal(platform.Suffixes)
			resources, _ := json.Marshal(platform.Resources)
			expression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", string(markers), 1)
			expression = strings.Replace(expression, "__ALPHA_SUFFIXES__", string(suffixes), 1)
			expression = strings.Replace(expression, "__ALPHA_RESOURCES__", string(resources), 1)
			script := `globalThis.document = {
  body: { innerText: ` + strconv.Quote(fixture.body) + ` },
  title: "metrics", querySelector: () => null, querySelectorAll: () => []
};
globalThis.location = { href: ` + strconv.Quote("https://"+platform.Host+"/metrics") + ` };
Object.defineProperty(globalThis, "performance", { value: { getEntriesByType: () => [] } });
process.stdout.write(JSON.stringify(` + expression + `));`
			output, runErr := exec.Command("node", "--input-type=module", "--eval", script).CombinedOutput()
			if runErr != nil {
				t.Fatalf("evaluate %s DOM fixture: %v\n%s", fixture.platform, runErr, output)
			}
			var result struct {
				RangeVerified  bool             `json:"rangeVerified"`
				RangeHints     []string         `json:"rangeHints"`
				ReviewedMarker bool             `json:"reviewedMarker"`
				Metrics        map[string]int64 `json:"metrics"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("decode %s DOM fixture: %v\n%s", fixture.platform, err, output)
			}
			if !result.RangeVerified || !result.ReviewedMarker || result.Metrics["views"] != fixture.wantViews ||
				result.Metrics["likes"] != fixture.wantLikes || result.Metrics["posts"] != fixture.wantPosts {
				t.Fatalf("result=%+v, want range+marker views=%d likes=%d posts=%d",
					result, fixture.wantViews, fixture.wantLikes, fixture.wantPosts)
			}
			if len(result.RangeHints) == 0 {
				t.Fatalf("result=%+v, want a sanitized visible time-range hint", result)
			}
		})
	}
}

// The CSDN home shows a "近7日创作表现" card next to all-time account totals
// (收藏数) and per-article counters. A page-wide label scan mixes them: it
// reported the all-time 收藏数 as a 7-day number and read "1点赞 13收藏" as
// likes=13. Only the seven-day card may feed a verified window.
func TestCreatorMetricsCSDNReadsSevenDayCardOnly(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	body := "近7日创作表现 新增阅读 503 作品获得的阅读次数 新增互动 0 收到的点赞、评论与收藏 " +
		"总阅读量 61.1w 粉丝数 11,387 收藏数 6,304 " +
		"近期文章 279阅读 0评论 1点赞 13收藏"
	card := "近7日创作表现\n\n新增阅读\n\n503\n\n作品获得的阅读次数\n\n新增互动\n\n0\n\n收到的点赞、评论与收藏"

	fixtures := []struct {
		name      string
		cardNode  string
		wantScope string
		wantRange bool
	}{
		{name: "with seven day card", cardNode: `[{ innerText: ` + strconv.Quote(card) + ` }]`, wantScope: "window", wantRange: true},
		{name: "without seven day card", cardNode: `[]`, wantScope: "page", wantRange: false},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			expression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", `["一周小结"]`, 1)
			expression = strings.Replace(expression, "__ALPHA_SUFFIXES__", `["csdn.net"]`, 1)
			expression = strings.Replace(expression, "__ALPHA_RESOURCES__", `[]`, 1)
			expression = strings.Replace(expression, "__ALPHA_PLATFORM_CODE__", "csdn", 1)
			script := `globalThis.document = {
  body: { innerText: ` + strconv.Quote(body) + ` },
  title: "首页-CSDN创作中心",
  querySelector: () => null,
  querySelectorAll: (selector) => selector === "section,article,div" ? ` + fixture.cardNode + ` : []
};
globalThis.location = { href: "https://mp.csdn.net/" };
Object.defineProperty(globalThis, "performance", { value: { getEntriesByType: () => [] } });
process.stdout.write(JSON.stringify(` + expression + `));`
			output, runErr := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput()
			if runErr != nil {
				t.Fatalf("evaluate CSDN DOM fixture: %v\n%s", runErr, output)
			}
			var result struct {
				RangeVerified    bool             `json:"rangeVerified"`
				MetricScope      string           `json:"metricScope"`
				ScopedEngagement *int64           `json:"scopedEngagement"`
				Metrics          map[string]int64 `json:"metrics"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("decode CSDN DOM fixture: %v\n%s", err, output)
			}
			if result.MetricScope != fixture.wantScope || result.RangeVerified != fixture.wantRange {
				t.Fatalf("scope=%q range=%v, want %q/%v (result=%+v)",
					result.MetricScope, result.RangeVerified, fixture.wantScope, fixture.wantRange, result)
			}
			if fixture.wantRange {
				if result.Metrics["views"] != 503 || result.ScopedEngagement == nil || *result.ScopedEngagement != 0 {
					t.Fatalf("metrics=%v engagement=%v, want views=503 engagement=0", result.Metrics, result.ScopedEngagement)
				}
				for _, key := range []string{"likes", "comments", "favorites"} {
					if _, exists := result.Metrics[key]; exists {
						t.Fatalf("metrics=%v, want no %s from all-time cards", result.Metrics, key)
					}
				}
				return
			}
			// Without the card the page-wide numbers are all-time, so CSDN must
			// never present them as a verified seven-day window.
			if result.Metrics["favorites"] == 0 {
				t.Fatalf("metrics=%v, want the page-wide scan to still see the all-time total for this assertion", result.Metrics)
			}
		})
	}
}
