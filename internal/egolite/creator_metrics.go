package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"alpha-nexus-egolite/internal/db"
)

type creatorMetricPlatform struct {
	Code          string   `json:"code"`
	Host          string   `json:"host"`
	Suffixes      []string `json:"suffixes"`
	Candidates    []string `json:"candidates"`
	MaxCandidates int      `json:"maxCandidates,omitempty"`
	Markers       []string `json:"markers"`
	Resources     []string `json:"resources"`
}

var creatorMetricPlatformCatalog = map[string]creatorMetricPlatform{
	"douyin": {
		Code: "douyin", Host: "creator.douyin.com", Suffixes: []string{"douyin.com"},
		MaxCandidates: 1,
		Candidates: []string{
			"https://creator.douyin.com/creator-micro/data-center/data-overview",
			"https://creator.douyin.com/creator-micro/data-center",
			"https://creator.douyin.com/creator-micro/content/manage",
		},
		Markers: []string{"数据中心", "作品数据", "核心数据", "数据概览", "内容管理", "作品管理"},
		Resources: []string{
			"/janus/douyin/creator/data/author/upgrade/",
			"/janus/douyin/creator/pc/work_list",
		},
	},
	"xhs": {
		Code: "xhs", Host: "creator.xiaohongshu.com", Suffixes: []string{"xiaohongshu.com"},
		Candidates: []string{
			"https://creator.xiaohongshu.com/statistics/account/v2",
			"https://creator.xiaohongshu.com/statistics/data-analysis",
			"https://creator.xiaohongshu.com/new/note-manager",
		},
		Markers: []string{"数据看板", "数据分析", "账号数据", "笔记数据", "发布笔记", "笔记管理"},
		Resources: []string{
			"/api/galaxy/v2/creator/datacenter/livedata/overview",
			"/api/galaxy/creator/data/note_detail_new",
			"/api/galaxy/creator/datacenter/note/analyze/list",
			"/api/galaxy/v2/creator/datacenter/audience/view/periods",
		},
	},
	"wechat_video": {
		Code: "wechat_video", Host: "channels.weixin.qq.com", Suffixes: []string{"weixin.qq.com", "qq.com"},
		Candidates: []string{
			"https://channels.weixin.qq.com/micro/statistic/statistics/overview",
			"https://channels.weixin.qq.com/micro/statistic/statistics",
			"https://channels.weixin.qq.com/platform/statistics/overview",
			"https://channels.weixin.qq.com/platform/statistics",
			"https://channels.weixin.qq.com/platform/post/list",
		},
		Markers: []string{"数据中心", "核心数据", "作品数据", "数据概览", "作品管理", "发表记录"},
		Resources: []string{
			"/cgi-bin/mmfinderassistant-bin/helper/helper_mmdata",
			"/cgi-bin/mmfinderassistant-bin/helper/hepler_merlin_mmdata",
			"/micro/statistic/cgi-bin/mmfinderassistant-bin/helper/helper_mmdata",
			"/micro/statistic/cgi-bin/mmfinderassistant-bin/helper/hepler_merlin_mmdata",
			"/micro/content/cgi-bin/mmfinderassistant-bin/helper/helper_mmdata",
			"/micro/content/cgi-bin/mmfinderassistant-bin/helper/hepler_merlin_mmdata",
			"/micro/content/cgi-bin/mmfinderassistant-bin/post/post_list",
		},
	},
	"kuaishou": {
		Code: "kuaishou", Host: "cp.kuaishou.com", Suffixes: []string{"kuaishou.com"},
		Candidates: []string{
			"https://cp.kuaishou.com/profile",
			"https://cp.kuaishou.com/profile/data-center",
			"https://cp.kuaishou.com/profile/analysis",
		},
		Markers: []string{"数据中心", "作品数据", "核心数据", "数据概览"},
		Resources: []string{
			"/rest/cp/creator/analysis/pc/home/photo/list",
			"/rest/cp/creator/analysis/pc/home/author/overview",
		},
	},
	"tiktok": {
		Code: "tiktok", Host: "www.tiktok.com", Suffixes: []string{"tiktok.com"},
		Candidates: []string{
			"https://www.tiktok.com/tiktokstudio/analytics",
			"https://www.tiktok.com/tiktokstudio/content",
		},
		Markers: []string{"Analytics", "Overview", "Content", "Key metrics", "数据分析", "内容"},
	},
	"youtube": {
		Code: "youtube", Host: "studio.youtube.com", Suffixes: []string{"youtube.com", "google.com"},
		MaxCandidates: 1,
		Candidates:    []string{"https://studio.youtube.com"},
		Markers:       []string{"Channel analytics", "Analytics", "Overview", "Content", "频道分析", "数据分析"},
		Resources: []string{
			"/youtubei/v1/creator/get_channel_dashboard",
			"/youtubei/v1/creator/list_creator_videos",
			"/youtubei/v1/creator/get_creator_videos",
		},
	},
	"meta": {
		Code: "meta", Host: "business.facebook.com", Suffixes: []string{"facebook.com", "meta.com"},
		MaxCandidates: 1,
		Candidates: []string{
			"https://business.facebook.com/latest/insights/overview",
			"https://business.facebook.com/latest/home",
		},
		Markers: []string{"Insights", "Overview", "Results", "内容", "成效", "覆盖人数", "互动"},
	},
	"wechat_mp": {
		Code: "wechat_mp", Host: "mp.weixin.qq.com", Suffixes: []string{"weixin.qq.com"},
		Candidates: []string{
			"https://mp.weixin.qq.com/misc/appmsganalysis",
			"https://mp.weixin.qq.com/misc/useranalysis",
			"https://mp.weixin.qq.com/",
		},
		Markers: []string{"数据分析", "用户分析", "内容分析", "图文分析", "昨日关键指标"},
	},
	"csdn": {
		Code: "csdn", Host: "mp.csdn.net", Suffixes: []string{"csdn.net"},
		Candidates: []string{"https://mp.csdn.net/"},
		Markers:    []string{"作品数据", "粉丝数据", "一周小结", "总阅读量", "昨日"},
	},
}

func creatorMetricPlatformForAccount(platform creatorMetricPlatform, account db.Account) creatorMetricPlatform {
	platform.Candidates = append([]string(nil), platform.Candidates...)
	switch platform.Code {
	case "youtube":
		identifier := strings.TrimSpace(account.AccountIdentifier)
		if strings.HasPrefix(identifier, "UC") && len(identifier) >= 10 && len(identifier) <= 64 {
			platform.Candidates = append([]string{"https://studio.youtube.com/channel/" + url.PathEscape(identifier) + "/analytics/tab-overview/period-week"}, platform.Candidates...)
		}
	case "meta":
		if manager := safePlatformManagerAddress("meta", account.ManagerAddr); manager != "" {
			parsed, err := url.Parse(manager)
			if err == nil {
				parsed.Path = "/latest/insights/overview"
				platform.Candidates = append([]string{parsed.String(), manager}, platform.Candidates...)
			}
		}
	}
	return platform
}

const creatorMetricsDOMExpression = `(() => {
  const normalize = (value) => String(value || "").replace(/\s+/g, " ").trim()
  const platformSuffixes = __ALPHA_SUFFIXES__
  const platformResources = __ALPHA_RESOURCES__
  const bodyText = normalize(document.body?.innerText || "").slice(0, 60000)
  const parseNumber = (value) => {
    const match = normalize(value).replace(/,/g, "").match(/(-?\d+(?:\.\d+)?)\s*(亿|万|w|k|m)?/i)
    if (!match) return null
    let number = Number(match[1])
    const unit = (match[2] || "").toLowerCase()
    if (unit === "亿") number *= 100000000
    if (unit === "万" || unit === "w") number *= 10000
    if (unit === "k") number *= 1000
    if (unit === "m") number *= 1000000
    return Number.isFinite(number) && number >= 0 ? Math.round(number) : null
  }
  // Keep the exact displayed value beside every parsed number. A metric that
  // cannot quote the page text it came from is not evidence, and a missing
  // "新增互动" must stay null instead of becoming 0.
  const metricIn = (text, labelsForMetric) => {
    for (const label of labelsForMetric) {
      const escaped = label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
      const after = text.match(new RegExp(escaped + "\\s*[:：]?\\s*([0-9][0-9,.]*(?:\\.\\d+)?\\s*(?:亿|万|w|k|m)?)", "i"))
      if (after) return { value: parseNumber(after[1]), label, raw: normalize(after[1]), context: normalize(after[0]).slice(0, 80) }
      const before = text.match(new RegExp("([0-9][0-9,.]*(?:\\.\\d+)?\\s*(?:亿|万|w|k|m)?)\\s*" + escaped, "i"))
      if (before) return { value: parseNumber(before[1]), label, raw: normalize(before[1]), context: normalize(before[0]).slice(0, 80) }
    }
    return { value: null, label: "", raw: "", context: "" }
  }
  const definitions = {
    views: ["视频播放量", "作品播放量", "笔记浏览量", "总播放次数", "有效播放次数", "播放次数", "观看人数", "观看次数", "观看数", "曝光次数", "播放量", "观看量", "浏览量", "曝光量", "Views", "Impressions"],
    likes: ["点赞数", "获赞数", "点赞量", "获赞量", "点赞", "Likes"],
    comments: ["评论数", "评论量", "评论", "Comments"],
    shares: ["分享数", "转发数", "分享量", "转发量", "分享", "Shares"],
    favorites: ["收藏数", "收藏量", "收藏", "Saves"],
    posts: ["发布视频数", "发布笔记数", "发布作品", "作品数", "发布内容", "Videos", "Published videos"]
  }
  const collectMetrics = (text) => {
    const values = {}, labels = [], evidence = {}
    for (const [key, labelsForMetric] of Object.entries(definitions)) {
      const found = metricIn(text, labelsForMetric)
      if (found.value === null) continue
      values[key] = found.value
      labels.push(found.label)
      evidence[key] = { label: found.label, value_text: found.raw, context: found.context }
    }
    return { values, labels, evidence }
  }
  const pageScan = collectMetrics(bodyText)
  let metrics = pageScan.values
  let matchedLabels = pageScan.labels
  let metricEvidence = pageScan.evidence
  // A page can show a seven-day summary card next to all-time account totals.
  // Reading the first matching label from the whole page mixes the two: on the
  // CSDN home the all-time "收藏数" and the per-article rows sit beside a
  // "近7日创作表现" card. Bind the numbers to the smallest container that
  // carries both the 7-day label and an "新增" metric, and never claim a
  // window for a page-wide scan.
  const platformCode = "__ALPHA_PLATFORM_CODE__"
  const sevenDayCard = (() => {
    let best = null
    for (const node of document.querySelectorAll("section,article,div")) {
      const text = normalize(node.innerText || "")
      if (!text || text.length > 1200) continue
      if (!/(?:近|过去|最近)\s*7\s*(?:天|日)/.test(text)) continue
      if (!/新增(?:阅读|播放|观看)/.test(text)) continue
      if (!best || text.length < best.text.length) best = { text }
    }
    return best
  })()
  let scopedEngagement = null
  let scopedEvidence = null
  let metricScope = "page"
  let scopeSource = ""
  let scopeTextLength = 0
  if (sevenDayCard) {
    const readScoped = (pattern, fallbackLabel) => {
      const match = sevenDayCard.text.match(new RegExp(pattern + "\\s*[:：]?\\s*([0-9][0-9,.]*(?:\\.\\d+)?\\s*(?:亿|万|w|k|m)?)", "i"))
      if (!match) return null
      return { value: parseNumber(match[1]), label: fallbackLabel, raw: normalize(match[1]), context: normalize(match[0]).slice(0, 80) }
    }
    const scopedViews = readScoped("新增(?:阅读|播放|观看)", "新增阅读")
    const scopedInteraction = readScoped("新增互动", "新增互动")
    if (scopedViews !== null) {
      metrics = { views: scopedViews.value }
      matchedLabels = [scopedViews.label]
      metricEvidence = { views: { label: scopedViews.label, value_text: scopedViews.raw, context: scopedViews.context } }
      metricScope = "window"
      scopeSource = "seven_day_card"
      scopeTextLength = sevenDayCard.text.length
      scopedEngagement = scopedInteraction === null ? null : scopedInteraction.value
      const asEvidence = (item) => item === null ? null : { label: item.label, value_text: item.raw, context: item.context }
      scopedEvidence = { views: asEvidence(scopedViews), engagement: asEvidence(scopedInteraction) }
    }
  }
  // Some dashboards scope the whole visible panel with a "近7日" tab instead of
  // one summary card. Bind to the smallest panel that holds the range label and
  // at least two metric labels, so the numbers cannot come from an all-time card
  // elsewhere on the page. CSDN mixes both kinds of card on one page, so it only
  // trusts the explicit "新增" card above.
  if (metricScope !== "window" && platformCode !== "csdn") {
    const periodPanel = (() => {
      let best = null
      const metricWords = /(?:播放量|观看数|观看次数|观看人数|浏览量|曝光量|点赞|评论|收藏|分享|转发)/g
      for (const node of document.querySelectorAll("section,article,div,main")) {
        const text = normalize(node.innerText || "")
        if (text.length < 12 || text.length > 800) continue
        if (!/(?:近|过去|最近)\s*7\s*(?:天|日)|Last\s*7\s*days/i.test(text)) continue
        const words = text.match(metricWords) || []
        if (words.length < 2) continue
        if (!best || text.length < best.text.length) best = { text }
      }
      return best
    })()
    if (periodPanel) {
      const scoped = collectMetrics(periodPanel.text)
      if (Object.keys(scoped.values).length >= 2) {
        metrics = scoped.values
        matchedLabels = scoped.labels
        metricEvidence = scoped.evidence
        metricScope = "window"
        scopeSource = "page_period_panel"
        scopeTextLength = periodPanel.text.length
      }
    }
  }
  // A tab strip can scope the whole data area without living inside the same
  // container as the numbers. Only an actually selected, visible 7-day control
  // counts; an inactive "近7日" tab proves nothing.
  if (metricScope !== "window" && platformCode !== "csdn") {
    const selectedSevenDay = [...document.querySelectorAll('[aria-selected="true"],[class*="active"],[class*="selected"]')]
      .filter((node) => {
        const style = getComputedStyle(node), rect = node.getBoundingClientRect()
        return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0
      })
      .some((node) => /^(?:近|过去|最近)\s*7\s*(?:天|日)$|^Last\s*7\s*days$/i.test(normalize(node.getAttribute("aria-label") || node.innerText || node.textContent || "")))
    if (selectedSevenDay && Object.keys(metrics).length > 0) {
      metricScope = "window"
      scopeSource = "selected_seven_day_control"
      scopeTextLength = bodyText.length
    }
  }
  const rangeHints = [...new Set(bodyText.match(/(?:近|过去|最近)\s*\d{1,2}\s*(?:天|日)|Last\s*\d{1,2}\s*days/ig) || [])]
    .map(normalize).slice(0, 12)
  const rangeControls = [...document.querySelectorAll('button,[role="button"],[role="combobox"],[aria-haspopup="listbox"]')]
    .map((node) => normalize(node.getAttribute('aria-label') || node.innerText || node.textContent || ''))
    .filter((value) => /(?:近|过去|最近)\s*\d{1,2}\s*(?:天|日)|Last\s*\d{1,2}\s*days|date range/i.test(value))
    .slice(0, 12)
  // A page-wide "近7日" hint only says a control exists somewhere on the page.
  // CSDN's home shows one while the captured numbers come from all-time cards,
  // so a CSDN page-wide scan is never a verified window.
  const rangeVerified = metricScope === "window" ||
    (platformCode !== "csdn" && rangeHints.some((value) => /(?:近|过去|最近)\s*7\s*(?:天|日)|Last\s*7\s*days/i.test(value)))
  const matchedMarkers = __ALPHA_MARKERS__.filter((marker) => bodyText.includes(marker))
  const reviewedMarker = matchedMarkers.length > 0
  const labelHints = [...new Set(bodyText.match(/[\u4e00-\u9fffA-Za-z]{0,12}(?:播放|观看|浏览|曝光|点赞|评论|分享|转发|收藏|粉丝|作品)[\u4e00-\u9fffA-Za-z]{0,12}/g) || [])].slice(0, 30)
  const routeHints = []
  for (const node of document.querySelectorAll('[href],[data-href],[data-url],[data-path],[data-route],[to]')) {
    for (const attribute of ["href", "data-href", "data-url", "data-path", "data-route", "to"]) {
      const raw = node.getAttribute(attribute)
      if (!raw) continue
      try {
        const parsed = new URL(raw, location.href)
        if (parsed.protocol !== "https:" || parsed.hostname !== location.hostname) continue
        const route = parsed.origin + parsed.pathname + parsed.hash
        if (route.length <= 240 && /(data|analysis|stat|overview|content|work|video|note|post|manage|center|dashboard)/i.test(route)) routeHints.push(route)
      } catch {}
    }
  }
  const resourceHints = []
  const resourceCandidates = []
  const isStaticResourcePath = (pathname) => /\.(?:js|css|map|png|jpe?g|gif|webp|svg|ico|woff2?|ttf|otf)$/i.test(pathname)
  const hasSecretLikePathSegment = (pathname) => pathname.split("/").some((segment) => /(?:token|cookie|secret|auth|ticket|signature|credential|session|password|nonce|key)/i.test(segment))
  const isWeChatMPHost = (() => {
    try { return new URL(location.href).hostname.toLowerCase() === "mp.weixin.qq.com" } catch { return false }
  })()
  const isReviewedWeChatMPPerformancePath = (pathname) => {
    if (!isWeChatMPHost || isStaticResourcePath(pathname) || hasSecretLikePathSegment(pathname)) return false
    return ["/cgi-bin/", "/misc/", "/datacube/"].some((prefix) => pathname.startsWith(prefix))
  }
  for (const entry of performance.getEntriesByType("resource").slice(-500)) {
    try {
      const parsed = new URL(String(entry?.name || ""), location.href)
      const host = parsed.hostname.toLowerCase().replace(/\.$/, "")
      const samePlatform = platformSuffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
      const route = parsed.origin + parsed.pathname
      const genericPerformancePath = /(api|data|analysis|stat|metric|overview|content|work|video|note|post|creator|account|dashboard|insight|trend)/i.test(parsed.pathname)
      const performancePath = isWeChatMPHost ? isReviewedWeChatMPPerformancePath(parsed.pathname) : genericPerformancePath
      if (parsed.protocol === "https:" && samePlatform && route.length <= 240 &&
          !isStaticResourcePath(parsed.pathname) && performancePath) {
        resourceHints.push(route)
      }
      if (parsed.protocol === "https:" && samePlatform && platformResources.includes(parsed.pathname)) {
        resourceCandidates.push(String(entry.name || ""))
      }
    } catch {}
  }
  const visibleLoginInput = [...document.querySelectorAll('input[type="password"],input[autocomplete="one-time-code"]')]
    .some((node) => {
      const style = getComputedStyle(node), rect = node.getBoundingClientRect()
      return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0
    })
  const loginPrompt = /扫码登录|验证码登录|密码登录|Sign in|Log in/i.test(bodyText.slice(0, 3000))
  const loginForm = visibleLoginInput && loginPrompt
  const rows = [...document.querySelectorAll('tr,.note,[class*="work-item"],[class*="content-item"],[class*="video-item"],[class*="note-item"]')]
  const posts = []
  const seen = new Set()
  for (const row of rows) {
    if (posts.length >= 50) break
    const text = normalize(row.innerText || row.textContent || "")
    if (text.length < 8 || text.length > 1600) continue
    const link = row.querySelector('a[href]')
    let postURL = ""
    let externalPostId = ""
    try {
      const parsed = new URL(link?.href || "", location.href)
      if (parsed.protocol === "https:") {
        postURL = parsed.origin + parsed.pathname
        externalPostId = parsed.pathname.split("/").filter(Boolean).pop() || ""
      }
    } catch {}
    const lines = String(row.innerText || row.textContent || "").split("\n").map(normalize).filter(Boolean)
    const explicitTitle = normalize(row.querySelector(".title")?.innerText || row.querySelector('[class*="title"]')?.innerText || "")
    const title = explicitTitle || lines.find((line) => line.length >= 2 && line.length <= 120 && !/^\d+(?:[,.]\d+)*(?:万|亿|w|k|m)?$/i.test(line)) || ""
    if (!title) continue
    const rowMetric = (labels) => {
      for (const label of labels) {
        const index = text.indexOf(label)
        if (index < 0) continue
        const value = parseNumber(text.slice(index + label.length, index + label.length + 32))
        if (value !== null) return value
      }
      return 0
    }
    const publishedAt = normalize(row.querySelector(".time")?.innerText || "") || lines.find((line) => /20\d{2}[-/.年]\d{1,2}[-/.月]\d{1,2}/.test(line)) || ""
    const dateMatch = publishedAt.match(/(20\d{2})[-/.年](\d{1,2})[-/.月](\d{1,2})/)
    if (!dateMatch) continue
    const publishedDate = new Date(Number(dateMatch[1]), Number(dateMatch[2]) - 1, Number(dateMatch[3]))
    const ageDays = (Date.now() - publishedDate.getTime()) / 86400000
    if (!Number.isFinite(ageDays) || ageDays < -1 || ageDays > 8) continue
    const key = externalPostId || title + "\n" + publishedAt
    if (seen.has(key)) continue
    const classicMetrics = [...row.querySelectorAll(".icon_list .icon span:last-child, .icon_list span")]
      .map((node) => parseNumber(node.innerText || node.textContent || "")).filter((value) => value !== null)
    const post = {
      externalPostId, title, publishedAt, postURL,
      views: classicMetrics[0] ?? rowMetric(definitions.views),
      comments: classicMetrics[1] ?? rowMetric(definitions.comments),
      likes: classicMetrics[2] ?? rowMetric(definitions.likes),
      favorites: classicMetrics[3] ?? rowMetric(definitions.favorites),
      shares: classicMetrics[4] ?? rowMetric(definitions.shares)
    }
    if (!Object.values(post).some((value) => typeof value === "number" && value > 0)) continue
    seen.add(key)
    posts.push(post)
  }
  const contentRangeVerified = posts.length > 0
  return {
    metrics, matchedLabels, matchedMarkers, labelHints,
    routeHints: [...new Set(routeHints)].slice(0, 40),
    resourceHints: [...new Set(resourceHints)].slice(0, 40),
    resourceCandidates: [...new Set(resourceCandidates)].slice(0, 40),
    rangeVerified, rangeHints, rangeControls, reviewedMarker, loginForm, posts,
    contentRangeVerified, metricScope, scopedEngagement, scopedEvidence,
    metricEvidence, scopeSource, scopeTextLength,
    pageTitle: normalize(document.title).slice(0, 120), textLength: bodyText.length,
    iframeCount: document.querySelectorAll("iframe").length
  }
})()`

type CreatorMetricCollectResult struct {
	AccountID           int64                             `json:"account_id"`
	PlatformCode        string                            `json:"platform_code"`
	State               string                            `json:"state"`
	RangeVerified       bool                              `json:"range_verified"`
	MetricScope         string                            `json:"metric_scope,omitempty"`
	ScopedEngagement    *int64                            `json:"scoped_engagement,omitempty"`
	ScopeSource         string                            `json:"scope_source,omitempty"`
	MetricEvidence      map[string]CreatorMetricEvidence  `json:"metric_evidence,omitempty"`
	ScopedEvidence      *CreatorMetricScopedEvidence      `json:"scoped_evidence,omitempty"`
	EvidenceCode        string                            `json:"evidence_code,omitempty"`
	ErrorCode           string                            `json:"error_code,omitempty"`
	Stage               string                            `json:"stage,omitempty"`
	PageOrigin          string                            `json:"page_origin,omitempty"`
	PageTitle           string                            `json:"page_title,omitempty"`
	MatchedLabels       []string                          `json:"matched_labels,omitempty"`
	MatchedMarkers      []string                          `json:"matched_markers,omitempty"`
	LabelHints          []string                          `json:"label_hints,omitempty"`
	RangeHints          []string                          `json:"range_hints,omitempty"`
	RangeControls       []string                          `json:"range_controls,omitempty"`
	RouteHints          []string                          `json:"route_hints,omitempty"`
	ResourceHints       []string                          `json:"resource_hints,omitempty"`
	ResourceDiagnostics []CreatorMetricResourceDiagnostic `json:"resource_diagnostics,omitempty"`
	MetricCandidates    map[string]int64                  `json:"metric_candidates,omitempty"`
	TextLength          int                               `json:"text_length,omitempty"`
	IframeCount         int                               `json:"iframe_count,omitempty"`
	Attempts            []CreatorMetricAttempt            `json:"attempts,omitempty"`
	Snapshot            *db.CreatorMetricSnapshot         `json:"snapshot,omitempty"`
	PostCount           int                               `json:"post_count"`
	// RunError carries the runtime's own failure text when the collection
	// program could not finish, so callers can tell a page problem from a
	// launcher problem instead of only seeing a generic error code.
	RunError string `json:"run_error,omitempty"`
}

type CreatorMetricEvidence struct {
	Label     string `json:"label"`
	ValueText string `json:"value_text"`
	Context   string `json:"context"`
}

type CreatorMetricScopedEvidence struct {
	Views      *CreatorMetricEvidence `json:"views,omitempty"`
	Engagement *CreatorMetricEvidence `json:"engagement,omitempty"`
}

type CreatorMetricAttempt struct {
	Candidate      string   `json:"candidate"`
	Actual         string   `json:"actual"`
	PageTitle      string   `json:"page_title,omitempty"`
	MatchedMarkers []string `json:"matched_markers,omitempty"`
	MatchedLabels  []string `json:"matched_labels,omitempty"`
	TextLength     int      `json:"text_length,omitempty"`
	IframeCount    int      `json:"iframe_count,omitempty"`
}

type CreatorMetricResourceDiagnostic struct {
	Resource          string           `json:"resource"`
	NumericCandidates map[string]int64 `json:"numeric_candidates,omitempty"`
	RangeHints        []string         `json:"range_hints,omitempty"`
	ShapeHints        []string         `json:"shape_hints,omitempty"`
}

type CreatorMetricsService struct {
	db      *db.DB
	runtime ProgramRunner
	now     func() time.Time
	// spaceBinding owns the runtime membership check for the bound Space.
	spaceBinding *SpaceService
}

const creatorMetricsAccountTimeout = 35 * time.Second
const creatorMetricsCleanupTimeout = 5 * time.Second

func creatorMetricsCleanupProgram(spaceID int64) string {
	return fmt.Sprintf(`let cleanupHandoffDone = false
try {
  await takeOverTaskSpace(%d)
  try {
    await js("(() => { for (const media of document.querySelectorAll('video, audio')) { try { media.pause() } catch {} } return true })()")
  } catch {}
} finally {
  try {
    const handoff = await handOffTaskSpace(%d)
    cleanupHandoffDone = Boolean(handoff?.done)
  } catch {}
}
cliLog(%q + JSON.stringify({ cleanupHandoffDone }))
`, spaceID, spaceID, resultPrefix)
}

func (service *CreatorMetricsService) cleanupTaskSpace(spaceID int64) {
	if service == nil || service.runtime == nil || spaceID < 0 {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), creatorMetricsCleanupTimeout)
	defer cancel()
	var observed struct {
		CleanupHandoffDone bool `json:"cleanupHandoffDone"`
	}
	_ = service.runtime.Run(cleanupCtx, creatorMetricsCleanupProgram(spaceID), &observed)
}

func NewCreatorMetricsService(database *db.DB, runtime ProgramRunner) *CreatorMetricsService {
	return &CreatorMetricsService{db: database, runtime: runtime, now: time.Now, spaceBinding: NewSpaceService(database, runtime)}
}

func creatorMetricSnapshotDate(collectedAt time.Time) string {
	return collectedAt.Format("2006-01-02")
}

// SupportsCreatorMetrics reports whether this platform has a reviewed,
// automatic seven-day creator-backend extractor. Other managed platforms are
// intentionally represented by a persisted manual handoff, not guessed DOM.
func SupportsCreatorMetrics(platformCode string) bool {
	_, ok := creatorMetricPlatformCatalog[strings.TrimSpace(platformCode)]
	return ok
}

// SafeCreatorMetricErrorDetail reduces a collection error to something that can
// cross the API boundary. Runtime errors are already stable codes; local
// database contention gets a short label. Anything else is withheld because it
// may contain paths, page text or other captured output.
func SafeCreatorMetricErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "EGO_") {
		return text
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "database is locked"), strings.Contains(lower, "sqlite_busy"):
		return "SQLITE_BUSY"
	case strings.Contains(lower, "database or disk is full"), strings.Contains(lower, "sqlite_full"):
		return "SQLITE_FULL"
	case strings.Contains(lower, "disk i/o error"), strings.Contains(lower, "sqlite_ioerr"):
		return "SQLITE_IOERR"
	default:
		return ""
	}
}

func (service *CreatorMetricsService) Collect(ctx context.Context, accountID int64) (*CreatorMetricCollectResult, error) {
	if service == nil || service.db == nil || service.runtime == nil || accountID <= 0 {
		return nil, fmt.Errorf("EGO_METRICS_INPUT_INVALID")
	}
	account, err := service.db.GetAccount(accountID)
	if err != nil || !account.Enabled {
		return nil, fmt.Errorf("EGO_METRICS_ACCOUNT_UNAVAILABLE")
	}
	if account.LoginState != db.AccountAuthLoggedIn {
		result := &CreatorMetricCollectResult{AccountID: account.ID, PlatformCode: account.PlatformCode, State: "skipped", ErrorCode: "ACCOUNT_NOT_LOGGED_IN"}
		if err := service.persistCollectionState(result, service.now()); err != nil {
			return nil, err
		}
		return result, nil
	}
	duplicate, err := service.isDuplicateAccountBinding(*account)
	if err != nil {
		return nil, err
	}
	if duplicate {
		result := &CreatorMetricCollectResult{AccountID: account.ID, PlatformCode: account.PlatformCode, State: "skipped", ErrorCode: "DUPLICATE_ACCOUNT_BINDING"}
		if err := service.persistCollectionState(result, service.now()); err != nil {
			return nil, err
		}
		return result, nil
	}
	platform, ok := creatorMetricPlatformCatalog[account.PlatformCode]
	if !ok {
		result := &CreatorMetricCollectResult{AccountID: account.ID, PlatformCode: account.PlatformCode, State: "manual_takeover", Stage: "MANUAL_ANALYTICS", ErrorCode: "EGO_METRICS_MANUAL_HANDOFF"}
		if err := service.persistCollectionState(result, service.now()); err != nil {
			return nil, err
		}
		return result, nil
	}
	platform = creatorMetricPlatformForAccount(platform, *account)
	// The persisted binding can outlive the Space itself: read it and, when the
	// Space is gone from the runtime, re-bind instead of failing every later
	// collection with EGO_SPACE_BINDING_MISSING.
	space, err := service.spaceBinding.EnsureRuntimeSpaceBinding(ctx, account.IdentityID)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, creatorMetricsAccountTimeout)
	defer cancel()
	platformJSON, _ := json.Marshal(platform)
	markersJSON, _ := json.Marshal(platform.Markers)
	suffixesJSON, _ := json.Marshal(platform.Suffixes)
	resourcesJSON, _ := json.Marshal(platform.Resources)
	domExpression := strings.Replace(creatorMetricsDOMExpression, "__ALPHA_MARKERS__", string(markersJSON), 1)
	domExpression = strings.Replace(domExpression, "__ALPHA_SUFFIXES__", string(suffixesJSON), 1)
	domExpression = strings.Replace(domExpression, "__ALPHA_RESOURCES__", string(resourcesJSON), 1)
	platformCodeJSON, _ := json.Marshal(account.PlatformCode)
	domExpression = strings.Replace(domExpression, "__ALPHA_PLATFORM_CODE__", strings.Trim(string(platformCodeJSON), `"`), 1)
	domExpressionJSON, _ := json.Marshal(domExpression)
	program := fmt.Sprintf(`await takeOverTaskSpace(%d)
const platform = %s
const domExpression = %s
const metricResult = {
  state: "unknown", rangeVerified: false, evidenceCode: "", errorCode: "",
  stage: "INITIALIZE", pageOrigin: "", pageTitle: "", metrics: {}, posts: [],
  matchedLabels: [], matchedMarkers: [], labelHints: [], rangeHints: [], rangeControls: [], routeHints: [], resourceHints: [], resourceDiagnostics: [], metricCandidates: {},
  textLength: 0, iframeCount: 0, attempts: [], handoffDone: false,
  metricScope: "page", scopedEngagement: null, scopedEvidence: null,
  metricEvidence: {}, scopeSource: ""
}

let originalURL = ""
const privateResourceURLs = []
const privateResourceResponses = []
const metricDeadlineAt = Date.now() + 28000
const remainingMetricBudget = () => metricDeadlineAt - Date.now()
const boundedBrowserFetch = async (raw) => {
  const timeout = Math.min(4000, Math.max(0, remainingMetricBudget() - 500))
  if (timeout < 500) throw new Error("metric_time_budget_exhausted")
  return await Promise.race([
    browserFetch(raw, { method: "GET" }),
    new Promise((_, reject) => setTimeout(() => reject(new Error("metric_fetch_timeout")), timeout))
  ])
}
try {
  await cdp("Network.enable", { maxTotalBufferSize: 8388608, maxResourceBufferSize: 2097152 })
} catch {}
const safeOrigin = (raw) => {
  try {
    const parsed = new URL(String(raw || ""))
    return parsed.protocol === "https:" ? parsed.origin + parsed.pathname : ""
  } catch { return "" }
}
const normalizeText = (value) => String(value || "").replace(/\s+/g, " ").trim()
const parseMetricNumber = (value) => {
  const match = normalizeText(value).replace(/,/g, "").match(/(-?\d+(?:\.\d+)?)\s*(亿|万|w|k|m)?/i)
  if (!match) return null
  let number = Number(match[1])
  const unit = (match[2] || "").toLowerCase()
  if (unit === "亿") number *= 100000000
  if (unit === "万" || unit === "w") number *= 10000
  if (unit === "k") number *= 1000
  if (unit === "m") number *= 1000000
  return Number.isFinite(number) && number >= 0 ? Math.round(number) : null
}
const metricDefinitions = {
  views: ["视频播放量", "作品播放量", "笔记浏览量", "总播放次数", "有效播放次数", "播放次数", "观看人数", "观看次数", "观看数", "曝光次数", "播放量", "观看量", "浏览量", "曝光量", "Views", "Impressions"],
  likes: ["点赞数", "获赞数", "点赞量", "获赞量", "点赞", "Likes"], comments: ["评论数", "评论量", "评论", "Comments"],
  shares: ["分享数", "转发数", "分享量", "转发量", "分享", "Shares"], favorites: ["收藏数", "收藏量", "收藏", "Saves"],
  posts: ["发布视频数", "发布笔记数", "发布作品", "作品数", "发布内容", "Videos", "Published videos"]
}
const extractMetrics = (raw) => {
  const text = normalizeText(raw).slice(0, 60000)
  const metrics = {}
  const labels = []
  for (const [key, candidates] of Object.entries(metricDefinitions)) {
    for (const label of candidates) {
      const escaped = label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
      const after = text.match(new RegExp(escaped + "\\s*[:：]?\\s*([0-9][0-9,.]*(?:\\.\\d+)?\\s*(?:亿|万|w|k|m)?)", "i"))
      const before = text.match(new RegExp("([0-9][0-9,.]*(?:\\.\\d+)?\\s*(?:亿|万|w|k|m)?)\\s*" + escaped, "i"))
      const value = parseMetricNumber((after || before || [])[1])
      if (value !== null) { metrics[key] = value; labels.push(label); break }
    }
  }
  return { text, metrics, labels }
}
const extractSafeRoutes = (raw, baseURL) => {
  const routes = []
  const values = String(raw || "").match(/https?:\/\/[^\s"'<>\\]+/g) || []
  for (const value of values.slice(0, 300)) {
    try {
      const parsed = new URL(value, baseURL)
      const host = parsed.hostname.toLowerCase().replace(/\.$/, "")
      const samePlatform = host === platform.host || platform.suffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
      const route = parsed.origin + parsed.pathname + parsed.hash
      if (samePlatform && route.length <= 240 && /(data|analysis|stat|overview|content|work|video|note|post|manage|center|dashboard)/i.test(route)) routes.push(route)
    } catch {}
  }
  return [...new Set(routes)].slice(0, 40)
}
const collectCapturedResourceResponses = async () => {
  if (!Array.isArray(platform.resources) || platform.resources.length === 0) return
  let drained = []
  try {
    const value = await drainEvents()
    drained = Array.isArray(value) ? value : Array.isArray(value?.events) ? value.events : []
  } catch { return }
  const allowedPaths = new Set(platform.resources)
  for (const event of drained.slice(-1000)) {
    const method = String(event?.method || event?.type || "")
    if (method !== "Network.responseReceived" && !method.endsWith(".responseReceived")) continue
    const params = event?.params && typeof event.params === "object" ? event.params : event
    const response = params?.response && typeof params.response === "object" ? params.response : {}
    const responseURL = String(response.url || "")
    let parsedURL
    try { parsedURL = new URL(responseURL) } catch { continue }
    const host = parsedURL.hostname.toLowerCase().replace(/\.$/, "")
    const samePlatform = host === platform.host || platform.suffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
    const status = Number(response.status || 0)
    if (parsedURL.protocol !== "https:" || !samePlatform || !allowedPaths.has(parsedURL.pathname) || status < 200 || status >= 400) continue
    const requestId = String(params?.requestId || "")
    if (!requestId) continue
    try {
      const captured = await cdp("Network.getResponseBody", { requestId })
      if (captured?.base64Encoded) continue
      const body = String(captured?.body || "")
      if (!body || body.length > 1048576) continue
      privateResourceURLs.push(responseURL)
      privateResourceResponses.push({ url: responseURL, body })
      if (privateResourceResponses.length > 24) privateResourceResponses.splice(0, privateResourceResponses.length - 24)
    } catch {}
  }
}
const collectResourceDiagnostics = async (seedURLs = [], capturedResponses = []) => {
  if (!Array.isArray(platform.resources) || platform.resources.length === 0) return []
  const pageURLs = await js(String.raw`+"`"+`performance.getEntriesByType("resource")
    .map((entry) => String(entry?.name || "")).filter(Boolean).slice(-500)`+"`"+`)
  const capturedURLs = capturedResponses.map((item) => String(item?.url || "")).filter(Boolean)
  const rawURLs = [...seedURLs, ...pageURLs, ...capturedURLs].slice(-800)
  const allowedPaths = new Set(platform.resources)
  const seen = new Set()
  const diagnostics = []
  const metricKey = /^(?:view_count|play_count|exposure_count|impression_count|like_count|digg_count|comment_count|share_count|collect_count|favorite_count|favorited_count|total_favorited|fan_count|fans_count|follower_count|followers_count|publish_count|post_count|note_count|work_count|video_count|vv|pv|uv|观看(?:数|量)?|播放(?:数|量)?|浏览(?:数|量)?|曝光(?:数|量)?|点赞(?:数|量)?|评论(?:数|量)?|分享(?:数|量)?|转发(?:数|量)?|收藏(?:数|量)?|粉丝(?:数|量)?|作品数)$/i
  const parseRangeValue = (value) => {
    const raw = String(value || "").trim()
    if (/^\d{13}$/.test(raw)) return Number(raw)
    if (/^\d{10}$/.test(raw)) return Number(raw) * 1000
    const parsed = Date.parse(raw)
    return Number.isFinite(parsed) ? parsed : null
  }
  for (const raw of [...rawURLs].reverse()) {
    if (diagnostics.length >= 4 || remainingMetricBudget() < 1500) break
    let parsedURL
    try { parsedURL = new URL(raw) } catch { continue }
    const host = parsedURL.hostname.toLowerCase().replace(/\.$/, "")
    const samePlatform = host === platform.host || platform.suffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
    if (parsedURL.protocol !== "https:" || !samePlatform || !allowedPaths.has(parsedURL.pathname) || seen.has(parsedURL.pathname)) continue
    seen.add(parsedURL.pathname)
    try {
      const captured = [...capturedResponses].reverse().find((item) => String(item?.url || "") === raw)
      const responseBody = captured?.body || await boundedBrowserFetch(raw)
      const firstSerialized = typeof responseBody === "string" ? responseBody : JSON.stringify(responseBody)
      if (!firstSerialized || firstSerialized.length > 1048576) continue
      let payload = typeof responseBody === "string" ? JSON.parse(responseBody) : responseBody
      if (platform.code === "douyin" && parsedURL.pathname === "/janus/douyin/creator/pc/work_list" && Array.isArray(payload?.aweme_list)) {
        const mergedItems = []
        const seenItems = new Set()
        const appendUniqueItems = (items) => {
          for (const item of items) {
            if (mergedItems.length >= 250) break
            const externalID = String(item?.aweme_id || "").trim()
            const fallbackID = String(item?.create_time || "") + "|" + String(item?.desc || "").trim()
            const key = externalID ? "id:" + externalID : fallbackID !== "|" ? "fallback:" + fallbackID : ""
            if (key && seenItems.has(key)) continue
            if (key) seenItems.add(key)
            mergedItems.push(item)
          }
        }
        appendUniqueItems(payload.aweme_list)
        let hasMore = payload?.has_more === true || Number(payload?.has_more || 0) > 0
        let cursor = String(payload?.max_cursor || "").trim()
        const seenCursors = new Set(cursor ? [cursor] : [])
        for (let pageIndex = 1; pageIndex < 5 && hasMore && cursor && remainingMetricBudget() > 1500; pageIndex += 1) {
          const nextURL = new URL(raw)
          nextURL.searchParams.set("max_cursor", cursor)
          await new Promise((resolve) => setTimeout(resolve, 600))
          const nextBody = await boundedBrowserFetch(nextURL.toString())
          const nextSerialized = typeof nextBody === "string" ? nextBody : JSON.stringify(nextBody)
          if (!nextSerialized || nextSerialized.length > 1048576) break
          const nextPayload = typeof nextBody === "string" ? JSON.parse(nextBody) : nextBody
          if (!Array.isArray(nextPayload?.aweme_list)) break
          appendUniqueItems(nextPayload.aweme_list)
          hasMore = nextPayload?.has_more === true || Number(nextPayload?.has_more || 0) > 0
          const nextCursor = String(nextPayload?.max_cursor || "").trim()
          if (hasMore && (!nextCursor || seenCursors.has(nextCursor))) break
          cursor = nextCursor
          if (cursor) seenCursors.add(cursor)
        }
        payload = { ...payload, aweme_list: mergedItems, has_more: hasMore, max_cursor: cursor }
      }
      const serialized = JSON.stringify(payload)
      if (!serialized || serialized.length > 5242880) continue
      const numericCandidates = {}
	  const shapeHints = []
	  const shapeKey = /^(?:create_time|createTime|publish_time|publishTime|published_at|publishedAt|release_time|releaseTime|has_more|hasMore|cursor|max_cursor|maxCursor)$/
	  const parseNestedJSON = (value) => {
		if (typeof value !== "string" || value.length > 1048576) return null
		try {
			const parsed = JSON.parse(value)
			return parsed !== null && typeof parsed === "object" ? parsed : null
		} catch {}
		return null
	  }
	  const collectShape = (value, prefix) => {
		if (!value || typeof value !== "object" || Array.isArray(value)) return
		for (const key of Object.keys(value).slice(0, 120)) {
		  if (shapeKey.test(key)) shapeHints.push(prefix ? prefix + "." + key : key)
		}
	  }
	  const collectMetricShape = (value, prefix, depth) => {
		if (depth > 5 || value == null || shapeHints.length >= 40) return
		if (typeof value === "string") {
			const nested = parseNestedJSON(value)
			if (nested) collectMetricShape(nested, prefix, depth + 1)
			return
		}
		if (Array.isArray(value)) {
		  if (value.length > 0) collectMetricShape(value[0], prefix + "[]", depth + 1)
		  return
		}
		if (typeof value !== "object") return
		for (const [key, item] of Object.entries(value).slice(0, 120)) {
		  if (!/^[A-Za-z0-9_\-一-鿿]{1,80}$/.test(key) || /token|cookie|secret|auth|ticket|signature|credential/i.test(key)) continue
		  const path = prefix ? prefix + "." + key : key
		  if (/data|metric|stat|view|play|like|comment|share|collect|favorite|fan|follow|time|date|range|period|list|item|post|video|work|count|cursor|page|pv|uv|vv/i.test(key)) shapeHints.push(path)
		  collectMetricShape(item, path, depth + 1)
		  if (shapeHints.length >= 40) break
		}
	  }
	  collectShape(payload, "")
	  collectMetricShape(payload, "", 0)
	  for (const key of parsedURL.searchParams.keys()) {
		if (/^(?:cursor|max_cursor|maxCursor|count|limit|page|page_size)$/i.test(key)) shapeHints.push("query." + key)
	  }
	  for (const listKey of ["aweme_list", "items"]) {
		const rows = payload?.[listKey]
		if (Array.isArray(rows) && rows.length > 0) collectShape(rows[0], listKey + "[]")
	  }
      const walk = (value, path, depth) => {
        if (depth > 8 || Object.keys(numericCandidates).length >= 60 || value == null) return
        const leafKey = path.split(".").pop()?.replace(/\[\]$/g, "") || ""
		if ((typeof value === "number" || typeof value === "string") && metricKey.test(leafKey)) {
		  const parsed = typeof value === "number" ? value : parseMetricNumber(value)
		  if (Number.isFinite(parsed) && parsed >= 0) numericCandidates[path.slice(0, 180)] = Math.round(parsed)
		  if (Number.isFinite(parsed) && parsed >= 0) return
		}
		if (typeof value === "string") {
		  const nested = parseNestedJSON(value)
		  if (nested) walk(nested, path, depth + 1)
		  return
		}
        if (Array.isArray(value)) {
          for (const item of value.slice(0, 30)) walk(item, path + "[]", depth + 1)
          return
        }
        if (typeof value === "object") {
          for (const [key, item] of Object.entries(value).slice(0, 120)) {
            if (!/^[A-Za-z0-9_\-\u4e00-\u9fff]{1,80}$/.test(key)) continue
            walk(item, path ? path + "." + key : key, depth + 1)
          }
        }
      }
	      walk(payload, "", 0)
	      if (platform.code === "kuaishou" && parsedURL.pathname === "/rest/cp/creator/analysis/pc/home/author/overview") {
	        const basicData = Array.isArray(payload?.data?.basicData)
	          ? payload.data.basicData
	          : Array.isArray(payload?.basicData) ? payload.basicData : []
	        const metricLeafByLabel = [
	          [/播放|观看|浏览|曝光/, "view_count"],
	          [/点赞|获赞/, "like_count"],
	          [/评论/, "comment_count"],
	          [/分享|转发/, "share_count"],
	          [/收藏/, "collect_count"]
	        ]
	        for (const item of basicData.slice(0, 30)) {
	          const label = normalizeText(item?.name || "")
	          const mapping = metricLeafByLabel.find(([pattern]) => pattern.test(label))
	          const value = parseMetricNumber(item?.sumCount)
	          if (!mapping || value === null) continue
	          numericCandidates["reviewed.seven." + mapping[1]] = value
	        }
	      }
	      const rangeHints = []
      if (/(?:近|过去|最近)\s*7\s*(?:天|日)|Last\s*7\s*days/i.test(serialized)) rangeHints.push("explicit_7_day_marker")
      if (Object.keys(numericCandidates).some((key) => /(?:^|\.)seven(?:\.|$)/i.test(key))) {
        rangeHints.push("resource_explicit_7_day_bucket")
      }
      const starts = [], ends = []
      for (const [key, value] of parsedURL.searchParams.entries()) {
        const parsed = parseRangeValue(value)
        if (parsed === null) continue
        if (/(?:start|begin|from)/i.test(key)) starts.push(parsed)
        if (/(?:end|to)/i.test(key)) ends.push(parsed)
      }
      if (starts.some((start) => ends.some((end) => end >= start && (end - start) / 86400000 >= 5.5 && (end - start) / 86400000 <= 7.5))) {
        rangeHints.push("query_7_day_range")
      }
      const postCandidates = []
	      if (platform.code === "douyin" && parsedURL.pathname === "/janus/douyin/creator/pc/work_list" && Array.isArray(payload?.aweme_list)) {
        for (const item of payload.aweme_list.slice(0, 250)) {
          const publishedMs = Number(item?.create_time || 0) * 1000
          if (!Number.isFinite(publishedMs) || publishedMs <= 0) continue
          const ageDays = (Date.now() - publishedMs) / 86400000
          if (!Number.isFinite(ageDays) || ageDays < -1) continue
          if (ageDays > 7.999) continue
          const statistics = item?.statistics || {}
          const metricValue = (key) => {
            const parsed = parseMetricNumber(statistics[key])
            return parsed === null ? 0 : parsed
          }
          const post = {
            externalPostId: String(item?.aweme_id || "").slice(0, 120),
            title: normalizeText(item?.desc || "").slice(0, 240),
            publishedAt: new Date(publishedMs).toISOString(),
            postURL: safeOrigin(item?.share_url || ""),
            views: metricValue("play_count"), likes: metricValue("digg_count"),
            comments: metricValue("comment_count"), shares: metricValue("share_count"),
            favorites: metricValue("collect_count")
          }
          if (post.externalPostId || post.title) {
            postCandidates.push(post)
          }
		        }
		        const hasMore = payload?.has_more === true || Number(payload?.has_more || 0) > 0
		const publishedTimes = payload.aweme_list
		  .map((item) => Number(item?.create_time || 0) * 1000)
		  .filter((value) => Number.isFinite(value) && value > 0)
		const chronological = publishedTimes.every((value, index) => index === 0 || publishedTimes[index - 1] >= value)
		const reachedBeforeWindow = publishedTimes.some((value) => (Date.now() - value) / 86400000 > 7.999)
		if (!hasMore || (chronological && reachedBeforeWindow)) rangeHints.push("resource_complete_7_day_window")
		      } else if (platform.code === "kuaishou" && parsedURL.pathname === "/rest/cp/creator/analysis/pc/home/photo/list") {
		        const photoList = payload?.data?.photoList || payload?.photoList || {}
		        const photoItems = Array.isArray(photoList?.photoItems) ? photoList.photoItems : []
		        const metricValue = (value) => {
		          const parsed = parseMetricNumber(value)
		          return parsed === null ? 0 : parsed
		        }
		        for (const item of photoItems.slice(0, 250)) {
		          const publishedMs = parseRangeValue(item?.publishTime)
		          if (!Number.isFinite(publishedMs) || publishedMs <= 0) continue
		          const ageDays = (Date.now() - publishedMs) / 86400000
		          if (!Number.isFinite(ageDays) || ageDays < -1 || ageDays > 7.999) continue
		          const post = {
		            externalPostId: String(item?.photoId || "").slice(0, 120),
		            title: normalizeText(item?.title || "").slice(0, 240),
		            publishedAt: new Date(publishedMs).toISOString(),
		            postURL: "",
		            views: metricValue(item?.playCount), likes: metricValue(item?.likeCount),
		            comments: metricValue(item?.commentCount), shares: metricValue(item?.shareCount),
		            favorites: metricValue(item?.collectCount)
		          }
		          if (post.externalPostId || post.title) postCandidates.push(post)
		        }
		        const publishedTimes = photoItems
		          .map((item) => parseRangeValue(item?.publishTime))
		          .filter((value) => Number.isFinite(value) && value > 0)
		        const chronological = publishedTimes.every((value, index) => index === 0 || publishedTimes[index - 1] >= value)
		        const reachedBeforeWindow = publishedTimes.some((value) => (Date.now() - value) / 86400000 > 7.999)
		        const totalCount = Number(photoList?.totalCount)
		        const coversAll = Number.isFinite(totalCount) && totalCount >= 0 && totalCount <= photoItems.length
		        if (coversAll || (chronological && reachedBeforeWindow)) rangeHints.push("resource_complete_7_day_window")
		      } else if (platform.code === "youtube" && parsedURL.pathname === "/youtubei/v1/creator/get_creator_videos") {
		        const videos = Array.isArray(payload?.videos) ? payload.videos : []
		        const metricValue = (value) => {
		          const parsed = parseMetricNumber(value)
		          return parsed === null ? 0 : parsed
		        }
		        for (const item of videos.slice(0, 250)) {
		          const publishedSeconds = Number(item?.timePublishedSeconds || 0)
		          const publishedMs = Number.isFinite(publishedSeconds) ? publishedSeconds * 1000 : 0
		          const ageDays = (Date.now() - publishedMs) / 86400000
		          if (!Number.isFinite(publishedMs) || publishedMs <= 0 || !Number.isFinite(ageDays) || ageDays < -1 || ageDays > 7.999) continue
		          const post = {
		            externalPostId: String(item?.videoId || "").slice(0, 120),
		            title: normalizeText(item?.title || item?.videoTitle || "").slice(0, 240),
		            publishedAt: new Date(publishedMs).toISOString(),
		            postURL: "",
		            views: metricValue(item?.metrics?.viewCount), likes: metricValue(item?.metrics?.likeCount),
		            comments: metricValue(item?.metrics?.commentCount), shares: 0, favorites: 0
		          }
		          if (post.externalPostId || post.title) postCandidates.push(post)
		        }
		        const publishedTimes = videos
		          .map((item) => Number(item?.timePublishedSeconds || 0) * 1000)
		          .filter((value) => Number.isFinite(value) && value > 0)
		        const chronological = publishedTimes.every((value, index) => index === 0 || publishedTimes[index - 1] >= value)
		        const reachedBeforeWindow = publishedTimes.some((value) => (Date.now() - value) / 86400000 > 7.999)
		        const hasMore = payload?.hasMore === true || Boolean(payload?.continuationToken || payload?.nextPageToken)
		        if (!hasMore || (chronological && reachedBeforeWindow)) rangeHints.push("resource_complete_7_day_window")
		      }
      diagnostics.push({
        resource: parsedURL.origin + parsedURL.pathname,
        numericCandidates,
        rangeHints: [...new Set(rangeHints)],
		shapeHints: [...new Set(shapeHints)].slice(0, 20),
        postCandidates
      })
    } catch {}
  }
  return diagnostics
}
const switchYouTubeToSevenDays = async () => {
  const switched = await js("(() => {"+
    "const normalize=(value)=>String(value||'').replace(/\\s+/g,' ').trim();"+
    "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.display!=='none'&&style.visibility!=='hidden'&&rect.width>0&&rect.height>0};"+
    "const exactTarget=(wanted)=>{const matches=[...document.querySelectorAll('*')].filter((node)=>visible(node)&&normalize(node.innerText||node.textContent)===wanted);const leaves=matches.filter((node)=>!matches.some((other)=>other!==node&&node.contains(other)));const targets=[...new Set(leaves.map((node)=>node.closest('button,[role=button],ytcp-button,ytcp-dropdown-trigger,[aria-haspopup],[tabindex]')||node))];if(targets.length!==1)return null;return targets[0]};"+
    "const current=exactTarget('Last 28 days');if(!current)return false;current.dispatchEvent(new MouseEvent('click',{bubbles:true,composed:true}));return true;"+
  "})()")
  if (!switched) return false
  await new Promise((resolve) => setTimeout(resolve, 250))
  return await js("(() => {"+
    "const normalize=(value)=>String(value||'').replace(/\\s+/g,' ').trim();"+
    "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.display!=='none'&&style.visibility!=='hidden'&&rect.width>0&&rect.height>0};"+
    "const matches=[...document.querySelectorAll('*')].filter((node)=>visible(node)&&normalize(node.innerText||node.textContent)==='Last 7 days');const leaves=matches.filter((node)=>!matches.some((other)=>other!==node&&node.contains(other)));const targets=[...new Set(leaves.map((node)=>node.closest('button,[role=button],ytcp-button,ytcp-dropdown-trigger,[role=option],[role=menuitem],[tabindex]')||node))];if(targets.length!==1)return false;targets[0].dispatchEvent(new MouseEvent('click',{bubbles:true,composed:true}));return true;"+
  "})()")
}
const switchMetaToSevenDays = async () => {
  const opened = await js("(() => {"+
    "const normalize=(value)=>String(value||'').replace(/\\s+/g,' ').trim();"+
    "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.display!=='none'&&style.visibility!=='hidden'&&rect.width>0&&rect.height>0};"+
    "const targets=[...document.querySelectorAll('button,[role=button],[role=combobox],[aria-haspopup=listbox]')].filter((node)=>{const text=normalize([node.getAttribute('aria-label'),node.innerText,node.textContent].join(' '));return visible(node)&&/^过去\\s*28\\s*天(?:[：:]|$)/.test(text)});if(targets.length!==1)return false;targets[0].dispatchEvent(new MouseEvent('click',{bubbles:true,composed:true}));return true;"+
  "})()")
  if (!opened) return false
  await new Promise((resolve) => setTimeout(resolve, 250))
  return await js("(() => {"+
    "const normalize=(value)=>String(value||'').replace(/\\s+/g,' ').trim();"+
    "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.display!=='none'&&style.visibility!=='hidden'&&rect.width>0&&rect.height>0};"+
    "const matches=[...document.querySelectorAll('*')].filter((node)=>visible(node)&&/^过去\\s*7\\s*天(?:[：:]|$)/.test(normalize(node.innerText||node.textContent)));const leaves=matches.filter((node)=>!matches.some((other)=>other!==node&&node.contains(other)));const targets=[...new Set(leaves.map((node)=>node.closest('button,[role=button],[role=option],[role=menuitem],[role=menuitemradio],[tabindex]')||node))];if(targets.length!==1)return false;targets[0].dispatchEvent(new MouseEvent('click',{bubbles:true,composed:true}));return true;"+
  "})()")
}
try {
  metricResult.stage = "READ_ORIGINAL_PAGE"
  const originalPage = await pageInfo()
  originalURL = safeOrigin(originalPage?.url)
const candidateQueue = [...platform.candidates]
const maxCandidateAttempts = Math.max(1, Math.min(3, Number(platform.maxCandidates || 3)))
const seenCandidates = new Set()
  for (let candidateIndex = 0; candidateIndex < candidateQueue.length && candidateIndex < maxCandidateAttempts && Date.now() < metricDeadlineAt; candidateIndex += 1) {
	const candidate = candidateQueue[candidateIndex]
	if (seenCandidates.has(candidate)) continue
	seenCandidates.add(candidate)
    if (remainingMetricBudget() < 12000) {
      metricResult.state = "skipped"
      metricResult.errorCode = "EGO_METRICS_TIMEOUT"
      metricResult.stage = "TIME_BUDGET_EXHAUSTED"
      break
    }
    metricResult.stage = "NAVIGATE_ANALYTICS"
    await gotoAndWait(candidate, { wait: true, timeout: 8 })
    try { await waitForNetworkIdle({ timeout: 3 }) } catch {}
    await new Promise((resolve) => setTimeout(resolve, 600))
    await collectCapturedResourceResponses()
    metricResult.stage = "READ_PAGE_INFO"
    const page = await pageInfo()
	const pageURL = String(page?.url || candidate)
	let hostMatches = false
	try {
	  const host = new URL(pageURL).hostname.toLowerCase().replace(/\.$/, "")
	  hostMatches = host === platform.host || platform.suffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
	} catch {}
    metricResult.stage = "READ_DOM"
    let dom = await js(domExpression)
    privateResourceURLs.push(...(Array.isArray(dom.resourceCandidates) ? dom.resourceCandidates : []))
    delete dom.resourceCandidates
    const youtubeNeedsSevenDays = platform.code === "youtube" && !dom.rangeVerified && (dom.rangeHints || []).includes("Last 28 days")
    const metaNeedsSevenDays = platform.code === "meta" && !dom.rangeVerified && (dom.rangeHints || []).some((value) => /^过去\s*28\s*天/.test(String(value || "")))
    if ((youtubeNeedsSevenDays || metaNeedsSevenDays) && remainingMetricBudget() > 5000) {
      metricResult.stage = "SELECT_SEVEN_DAY_RANGE"
      const switched = youtubeNeedsSevenDays ? await switchYouTubeToSevenDays() : await switchMetaToSevenDays()
      if (switched) {
        try { await waitForNetworkIdle({ timeout: 3 }) } catch {}
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (switched || metaNeedsSevenDays) {
        dom = await js(domExpression)
        privateResourceURLs.push(...(Array.isArray(dom.resourceCandidates) ? dom.resourceCandidates : []))
        delete dom.resourceCandidates
      }
    }
    metricResult.stage = "VALIDATE_EVIDENCE"
    if (Object.keys(dom.metrics || {}).length === 0 && Number(dom.iframeCount || 0) > 0) {
      try {
        metricResult.stage = "READ_FRAME_DOM"
        const frameTree = await cdp("Page.getFrameTree")
        const frames = []
        const collectFrames = (node) => {
          if (!node || typeof node !== "object") return
          if (node.frame) frames.push(node.frame)
          for (const child of node.childFrames || []) collectFrames(child)
        }
        collectFrames(frameTree?.frameTree)
        for (const frame of frames.slice(1, 12)) {
          if (!String(frame?.url || "").startsWith("https://")) continue
          try {
            const world = await cdp("Page.createIsolatedWorld", {
              frameId: frame.id, worldName: "alpha-nexus-metrics-readonly", grantUniveralAccess: false
            })
            const evaluated = await cdp("Runtime.evaluate", {
              expression: domExpression, contextId: world?.executionContextId,
              returnByValue: true, awaitPromise: true
            })
            const frameDOM = evaluated?.result?.value
            if (!frameDOM || typeof frameDOM !== "object") continue
            privateResourceURLs.push(...(Array.isArray(frameDOM.resourceCandidates) ? frameDOM.resourceCandidates : []))
            delete frameDOM.resourceCandidates
            for (const [key, value] of Object.entries(frameDOM.metrics || {})) {
              if (!(key in dom.metrics) && Number.isFinite(Number(value))) dom.metrics[key] = Number(value)
            }
            dom.matchedLabels = [...new Set([...(dom.matchedLabels || []), ...(frameDOM.matchedLabels || [])])]
            dom.matchedMarkers = [...new Set([...(dom.matchedMarkers || []), ...(frameDOM.matchedMarkers || [])])]
            dom.labelHints = [...new Set([...(dom.labelHints || []), ...(frameDOM.labelHints || [])])].slice(0, 30)
			dom.rangeHints = [...new Set([...(dom.rangeHints || []), ...(frameDOM.rangeHints || [])])].slice(0, 12)
            dom.routeHints = [...new Set([...(dom.routeHints || []), ...(frameDOM.routeHints || [])])].slice(0, 40)
            dom.resourceHints = [...new Set([...(dom.resourceHints || []), ...(frameDOM.resourceHints || [])])].slice(0, 40)
            dom.rangeVerified = Boolean(dom.rangeVerified || frameDOM.rangeVerified || frameDOM.contentRangeVerified)
            dom.reviewedMarker = Boolean(dom.reviewedMarker || frameDOM.reviewedMarker)
            dom.posts = [...(dom.posts || []), ...(frameDOM.posts || [])].slice(0, 50)
            dom.textLength = Math.max(Number(dom.textLength || 0), Number(frameDOM.textLength || 0))
          } catch {}
        }
      } catch {}
      metricResult.stage = "VALIDATE_EVIDENCE"
    }
    if (Object.keys(dom.metrics || {}).length === 0 && Number(dom.iframeCount || 0) > 0) {
      try {
        metricResult.stage = "READ_ACCESSIBILITY_SNAPSHOT"
        const snapshotRaw = JSON.stringify(await snapshotText())
        const snapshot = extractMetrics(snapshotRaw)
        const snapshotMarkers = platform.markers.filter((marker) => snapshot.text.includes(marker))
        if (Object.keys(snapshot.metrics).length > 0) {
          dom.metrics = snapshot.metrics
          dom.matchedLabels = snapshot.labels
        }
        dom.matchedMarkers = [...new Set([...(dom.matchedMarkers || []), ...snapshotMarkers])]
        dom.labelHints = [...new Set([...(dom.labelHints || []), ...(snapshot.text.match(/[\u4e00-\u9fffA-Za-z]{0,12}(?:播放|观看|浏览|曝光|点赞|评论|分享|转发|收藏|粉丝|作品)[\u4e00-\u9fffA-Za-z]{0,12}/g) || [])])].slice(0, 30)
        dom.routeHints = [...new Set([...(dom.routeHints || []), ...extractSafeRoutes(snapshotRaw, candidate)])].slice(0, 40)
        dom.reviewedMarker = dom.matchedMarkers.length > 0
        dom.rangeVerified = Boolean(dom.rangeVerified || /近\s*7\s*天|过去\s*7\s*天|最近\s*7\s*天|Last\s*7\s*days/i.test(snapshot.text))
        dom.textLength = Math.max(Number(dom.textLength || 0), snapshot.text.length)
      } catch {}
      metricResult.stage = "VALIDATE_EVIDENCE"
    }
    if (Object.keys(dom.metrics || {}).length === 0 && Number(dom.iframeCount || 0) > 0) {
      try {
        metricResult.stage = "READ_PIERCED_DOM"
        const documentTree = await cdp("DOM.getDocument", { depth: -1, pierce: true })
        const textParts = []
        const routeHints = []
        const collectText = (node) => {
          if (!node || typeof node !== "object" || textParts.length >= 12000) return
          if (["SCRIPT", "STYLE", "NOSCRIPT"].includes(String(node.nodeName || "").toUpperCase())) return
          if (node.nodeName === "#text" && node.nodeValue) textParts.push(String(node.nodeValue))
          const attributes = node.attributes || []
          const attributeMap = {}
          for (let index = 0; index < attributes.length; index += 2) {
            const key = String(attributes[index] || "").toLowerCase()
            attributeMap[key] = String(attributes[index + 1] || "")
            if (["aria-label", "title", "alt", "value"].includes(key)) textParts.push(String(attributes[index + 1] || ""))
          }
          if (String(node.nodeName || "").toUpperCase() === "A" && attributeMap.href) {
            try {
              const parsed = new URL(attributeMap.href, candidate)
              const host = parsed.hostname.toLowerCase().replace(/\.$/, "")
              const samePlatform = host === platform.host || platform.suffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
              const route = parsed.origin + parsed.pathname + parsed.hash
              if (samePlatform && route.length <= 240 && /(data|analysis|stat|overview|content|work|video|note)/i.test(route)) routeHints.push(route)
            } catch {}
          }
          for (const child of node.children || []) collectText(child)
          if (node.contentDocument) collectText(node.contentDocument)
          for (const shadow of node.shadowRoots || []) collectText(shadow)
        }
        collectText(documentTree?.root)
        const pierced = extractMetrics(textParts.join(" "))
        const piercedMarkers = platform.markers.filter((marker) => pierced.text.includes(marker))
        if (Object.keys(pierced.metrics).length > 0) {
          dom.metrics = pierced.metrics
          dom.matchedLabels = pierced.labels
        }
        dom.matchedMarkers = [...new Set([...(dom.matchedMarkers || []), ...piercedMarkers])]
        dom.labelHints = [...new Set([...(dom.labelHints || []), ...(pierced.text.match(/[\u4e00-\u9fffA-Za-z]{0,12}(?:播放|观看|浏览|曝光|点赞|评论|分享|转发|收藏|粉丝|作品)[\u4e00-\u9fffA-Za-z]{0,12}/g) || [])])].slice(0, 30)
        dom.routeHints = [...new Set([...(dom.routeHints || []), ...routeHints])].slice(0, 40)
        dom.reviewedMarker = dom.matchedMarkers.length > 0
        dom.rangeVerified = Boolean(dom.rangeVerified || /(?:近|过去|最近)\s*7\s*(?:天|日)|Last\s*7\s*days/i.test(pierced.text))
        dom.textLength = Math.max(Number(dom.textLength || 0), pierced.text.length)
      } catch {}
      metricResult.stage = "VALIDATE_EVIDENCE"
    }
    const skeletonAnalyticsPage = hostMatches && dom.reviewedMarker && Object.keys(dom.metrics || {}).length === 0 && Number(dom.textLength || 0) < 1200
    if (skeletonAnalyticsPage && remainingMetricBudget() > 2500) {
      metricResult.stage = "WAIT_FOR_ANALYTICS"
      await new Promise((resolve) => setTimeout(resolve, 1200))
      await collectCapturedResourceResponses()
      dom = await js(domExpression)
      privateResourceURLs.push(...(Array.isArray(dom.resourceCandidates) ? dom.resourceCandidates : []))
      delete dom.resourceCandidates
      metricResult.stage = "VALIDATE_EVIDENCE"
    }
    metricResult.pageTitle = String(dom.pageTitle || "").slice(0, 120)
    metricResult.matchedLabels = [...new Set([...metricResult.matchedLabels, ...(Array.isArray(dom.matchedLabels) ? dom.matchedLabels : [])])].slice(0, 20)
    metricResult.matchedMarkers = [...new Set([...metricResult.matchedMarkers, ...(Array.isArray(dom.matchedMarkers) ? dom.matchedMarkers : [])])].slice(0, 20)
    metricResult.labelHints = [...new Set([...metricResult.labelHints, ...(Array.isArray(dom.labelHints) ? dom.labelHints : [])])].slice(0, 30)
			metricResult.rangeHints = [...new Set([...metricResult.rangeHints, ...(Array.isArray(dom.rangeHints) ? dom.rangeHints : [])])].slice(0, 12)
	metricResult.rangeControls = [...new Set([...metricResult.rangeControls, ...(Array.isArray(dom.rangeControls) ? dom.rangeControls : [])])].slice(0, 12)
    metricResult.routeHints = [...new Set([...metricResult.routeHints, ...(Array.isArray(dom.routeHints) ? dom.routeHints : [])])].slice(0, 40)
	metricResult.resourceHints = [...new Set([...metricResult.resourceHints, ...(Array.isArray(dom.resourceHints) ? dom.resourceHints : [])])].slice(0, 40)
	for (const route of Array.isArray(dom.routeHints) ? dom.routeHints : []) {
	  try {
		const parsed = new URL(route)
		const host = parsed.hostname.toLowerCase().replace(/\.$/, "")
		const samePlatform = host === platform.host || platform.suffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
		if (parsed.protocol === "https:" && samePlatform && /(data|analysis|stat|overview|insight|dashboard)/i.test(parsed.pathname + parsed.hash) && !seenCandidates.has(parsed.href)) {
		  candidateQueue.push(parsed.href)
		}
	  } catch {}
	}
    metricResult.metricCandidates = dom.metrics && typeof dom.metrics === "object" ? dom.metrics : {}
    metricResult.textLength = Number(dom.textLength || 0)
    metricResult.iframeCount = Number(dom.iframeCount || 0)
    metricResult.pageOrigin = safeOrigin(pageURL)
	metricResult.attempts.push({
	  candidate: safeOrigin(candidate), actual: metricResult.pageOrigin,
	  pageTitle: String(dom.pageTitle || "").slice(0, 120),
	  matchedMarkers: Array.isArray(dom.matchedMarkers) ? dom.matchedMarkers.slice(0, 20) : [],
	  matchedLabels: Array.isArray(dom.matchedLabels) ? dom.matchedLabels.slice(0, 20) : [],
	  textLength: Number(dom.textLength || 0), iframeCount: Number(dom.iframeCount || 0)
	})
    if (/(data|analysis|stat|overview|content|work|video|note|post|manage|center|dashboard)/i.test(metricResult.pageOrigin)) {
      metricResult.routeHints = [...new Set([...metricResult.routeHints, metricResult.pageOrigin])].slice(0, 40)
    }
    if (dom.loginForm || /\/login|signin|passport|\/auth(?:\/|$)/i.test(pageURL)) {
      metricResult.state = "logged_out"
      metricResult.errorCode = "LOGIN_REQUIRED"
      break
    }
    // A verified window can legitimately contain zeros (a dormant account).
    // Requiring a positive value hid those accounts entirely; a parsed label is
    // the evidence, the value itself may be 0.
    const hasMeasuredMetric = Object.keys(dom.metrics || {}).length > 0
	const analyticsRangeVerified = Boolean(dom.rangeVerified)
	const contentRangeVerified = Boolean(dom.contentRangeVerified && dom.posts.length > 0)
    if (platform.code !== "kuaishou" && hostMatches && dom.reviewedMarker && ((analyticsRangeVerified && hasMeasuredMetric) || contentRangeVerified)) {
      metricResult.state = "observed"
	  metricResult.errorCode = ""
	  metricResult.rangeVerified = true
      if (analyticsRangeVerified && hasMeasuredMetric) {
        metricResult.evidenceCode = "REVIEWED_ANALYTICS_DOM"
        metricResult.metrics = dom.metrics
        metricResult.metricScope = dom.metricScope === "window" ? "window" : "page"
        metricResult.scopeSource = String(dom.scopeSource || "")
        if (dom.scopedEngagement !== null && dom.scopedEngagement !== undefined && Number.isFinite(Number(dom.scopedEngagement))) metricResult.scopedEngagement = Number(dom.scopedEngagement)
        metricResult.metricEvidence = dom.metricEvidence && typeof dom.metricEvidence === "object" ? dom.metricEvidence : {}
        metricResult.scopedEvidence = dom.scopedEvidence || null
      } else {
        metricResult.evidenceCode = "REVIEWED_CONTENT_DOM"
        metricResult.metrics = {
          posts: dom.posts.length,
          views: dom.posts.reduce((sum, item) => sum + Number(item.views || 0), 0),
          likes: dom.posts.reduce((sum, item) => sum + Number(item.likes || 0), 0),
          comments: dom.posts.reduce((sum, item) => sum + Number(item.comments || 0), 0),
          shares: dom.posts.reduce((sum, item) => sum + Number(item.shares || 0), 0),
          favorites: dom.posts.reduce((sum, item) => sum + Number(item.favorites || 0), 0)
        }
      }
      metricResult.posts = dom.posts
      metricResult.matchedLabels = dom.matchedLabels
      metricResult.stage = "COMPLETE"
      break
    }
    metricResult.errorCode = hostMatches ? "METRICS_DOM_UNRECOGNIZED" : "METRICS_PAGE_REDIRECTED"
  }
  if (metricResult.state === "unknown" && remainingMetricBudget() >= 1500) {
    metricResult.stage = "READ_RESOURCE_DIAGNOSTICS"
    await collectCapturedResourceResponses()
    const diagnostics = await collectResourceDiagnostics([...new Set(privateResourceURLs)].slice(-200), privateResourceResponses)
    metricResult.resourceDiagnostics = diagnostics.map(({ resource, numericCandidates, rangeHints, shapeHints }) => ({
      resource, numericCandidates, rangeHints, shapeHints
    }))
    const completePosts = diagnostics
      .filter((item) => item.rangeHints?.includes("resource_complete_7_day_window"))
      .flatMap((item) => item.postCandidates || []).slice(0, 250)
    const sevenDayEntries = diagnostics.flatMap((item) => Object.entries(item.numericCandidates || {}))
      .filter(([key]) => /(?:^|\.)seven(?:\.|$)/i.test(key))
	    if (completePosts.length > 0) {
      metricResult.posts = completePosts
      metricResult.rangeVerified = true
      metricResult.metrics = {
        posts: completePosts.length,
        views: completePosts.reduce((sum, item) => sum + Number(item.views || 0), 0),
        likes: completePosts.reduce((sum, item) => sum + Number(item.likes || 0), 0),
        comments: completePosts.reduce((sum, item) => sum + Number(item.comments || 0), 0),
        shares: completePosts.reduce((sum, item) => sum + Number(item.shares || 0), 0),
        favorites: completePosts.reduce((sum, item) => sum + Number(item.favorites || 0), 0)
      }
      const followerValues = diagnostics.flatMap((item) => Object.entries(item.numericCandidates || {}))
        .filter(([key]) => /(?:^|\.)followers?_count$|(?:^|\.)fans?_count$/i.test(key))
        .map(([, value]) => Number(value)).filter((value) => Number.isFinite(value) && value >= 0)
      if (followerValues.length > 0) metricResult.metrics.followers = Math.max(...followerValues)
      metricResult.state = "observed"
      metricResult.errorCode = ""
	      metricResult.evidenceCode = "REVIEWED_CONTENT_RESOURCE"
	      metricResult.stage = "COMPLETE"
	    } else if (["douyin", "kuaishou"].includes(platform.code) && diagnostics.some((item) =>
	      item.rangeHints?.includes("resource_complete_7_day_window") && (item.postCandidates || []).length === 0)) {
	      metricResult.posts = []
	      metricResult.metrics = { posts: 0, views: 0, likes: 0, comments: 0, shares: 0, favorites: 0 }
	      metricResult.rangeVerified = true
	      metricResult.state = "observed"
	      metricResult.errorCode = ""
	      metricResult.evidenceCode = "REVIEWED_CONTENT_RESOURCE_EMPTY"
	      metricResult.stage = "COMPLETE"
	    } else if (sevenDayEntries.length > 0 && metricResult.matchedMarkers.length > 0) {
      const metrics = {}
      const metricMap = {
        view_count: "views", play_count: "views", like_count: "likes", digg_count: "likes",
        comment_count: "comments", share_count: "shares", collect_count: "favorites", favorite_count: "favorites",
        post_count: "posts", note_count: "posts", work_count: "posts", video_count: "posts"
      }
      for (const [key, value] of sevenDayEntries) {
        const leaf = key.split(".").pop()?.replace(/\[\]$/g, "") || ""
        const metric = metricMap[leaf]
        if (!metric) continue
        metrics[metric] = Math.max(Number(metrics[metric] || 0), Number(value || 0))
      }
      if (Object.keys(metrics).length > 0) {
        metricResult.metrics = metrics
        metricResult.rangeVerified = true
        metricResult.state = "observed"
        metricResult.errorCode = ""
        metricResult.evidenceCode = "REVIEWED_ANALYTICS_RESOURCE"
        metricResult.stage = "COMPLETE"
      } else {
        metricResult.stage = "VALIDATE_EVIDENCE"
      }
    } else {
      metricResult.stage = "VALIDATE_EVIDENCE"
    }
  }
  if (metricResult.state === "unknown" && remainingMetricBudget() < 1500) {
    metricResult.state = "skipped"
    metricResult.errorCode = "EGO_METRICS_TIMEOUT"
    metricResult.stage = "TIME_BUDGET_EXHAUSTED"
  }
} catch (error) {
  metricResult.state = "unknown"
  metricResult.errorCode = "METRICS_" + metricResult.stage + "_FAILED"
} finally {
  if (originalURL && originalURL !== metricResult.pageOrigin) {
    try {
      await gotoAndWait(originalURL, { wait: true, timeout: 4 })
      await new Promise((resolve) => setTimeout(resolve, 250))
    } catch {}
  }
  try {
    await js("(() => { for (const media of document.querySelectorAll('video, audio')) { try { media.pause() } catch {} } return true })()")
  } catch {}
  try {
    const handoff = await handOffTaskSpace(%d)
    metricResult.handoffDone = Boolean(handoff?.done)
  } catch {}
}
cliLog(%q + JSON.stringify(metricResult))
`, space.SpaceID, string(platformJSON), string(domExpressionJSON), space.SpaceID, resultPrefix)
	var observed struct {
		State               string                           `json:"state"`
		RangeVerified       bool                             `json:"rangeVerified"`
		MetricScope         string                           `json:"metricScope"`
		ScopedEngagement    *int64                           `json:"scopedEngagement"`
		ScopeSource         string                           `json:"scopeSource"`
		MetricEvidence      map[string]CreatorMetricEvidence `json:"metricEvidence"`
		ScopedEvidence      *CreatorMetricScopedEvidence     `json:"scopedEvidence"`
		EvidenceCode        string                           `json:"evidenceCode"`
		ErrorCode           string                           `json:"errorCode"`
		Stage               string                           `json:"stage"`
		PageOrigin          string                           `json:"pageOrigin"`
		PageTitle           string                           `json:"pageTitle"`
		MatchedLabels       []string                         `json:"matchedLabels"`
		MatchedMarkers      []string                         `json:"matchedMarkers"`
		LabelHints          []string                         `json:"labelHints"`
		RangeHints          []string                         `json:"rangeHints"`
		RangeControls       []string                         `json:"rangeControls"`
		RouteHints          []string                         `json:"routeHints"`
		ResourceHints       []string                         `json:"resourceHints"`
		ResourceDiagnostics []struct {
			Resource          string           `json:"resource"`
			NumericCandidates map[string]int64 `json:"numericCandidates"`
			RangeHints        []string         `json:"rangeHints"`
			ShapeHints        []string         `json:"shapeHints"`
		} `json:"resourceDiagnostics"`
		TextLength       int              `json:"textLength"`
		IframeCount      int              `json:"iframeCount"`
		Metrics          map[string]int64 `json:"metrics"`
		MetricCandidates map[string]int64 `json:"metricCandidates"`
		Posts            []struct {
			ExternalPostID string `json:"externalPostId"`
			Title          string `json:"title"`
			PublishedAt    string `json:"publishedAt"`
			PostURL        string `json:"postURL"`
			Views          int64  `json:"views"`
			Likes          int64  `json:"likes"`
			Comments       int64  `json:"comments"`
			Shares         int64  `json:"shares"`
			Favorites      int64  `json:"favorites"`
		} `json:"posts"`
		Attempts []struct {
			Candidate      string   `json:"candidate"`
			Actual         string   `json:"actual"`
			PageTitle      string   `json:"pageTitle"`
			MatchedMarkers []string `json:"matchedMarkers"`
			MatchedLabels  []string `json:"matchedLabels"`
			TextLength     int      `json:"textLength"`
			IframeCount    int      `json:"iframeCount"`
		} `json:"attempts"`
		HandoffDone bool `json:"handoffDone"`
	}
	if err := service.runtime.Run(runCtx, program, &observed); err != nil {
		if err.Error() != "EGO_SPACE_USER_CONTROLLED" {
			service.cleanupTaskSpace(space.SpaceID)
		}
		if ctx.Err() == nil && (runCtx.Err() == context.DeadlineExceeded || err.Error() == "EGO_RUNTIME_TIMEOUT") {
			result := &CreatorMetricCollectResult{AccountID: account.ID, PlatformCode: account.PlatformCode, State: "skipped", Stage: "TIME_BUDGET_EXHAUSTED", ErrorCode: "EGO_METRICS_TIMEOUT"}
			if persistErr := service.persistCollectionState(result, service.now()); persistErr != nil {
				return nil, persistErr
			}
			return result, nil
		}
		if err.Error() == "EGO_SPACE_USER_CONTROLLED" {
			_, _ = service.db.UpdateEgoSpaceStateForIdentity(
				account.IdentityID, "agentDelegatedToUser", "manual_takeover", "USER_CONTROLLED",
			)
			result := &CreatorMetricCollectResult{AccountID: account.ID, PlatformCode: account.PlatformCode, State: "skipped", Stage: "USER_CONTROLLED", ErrorCode: "EGO_SPACE_USER_CONTROLLED"}
			if persistErr := service.persistCollectionState(result, service.now()); persistErr != nil {
				return nil, persistErr
			}
			return result, nil
		}
		// Keep the account and the runtime's own failure text on the way out:
		// a bare error code leaves the caller unable to tell a slow page from a
		// broken launcher. The caller still receives the error, so this stays a
		// failure, not a collection result.
		failure := &CreatorMetricCollectResult{
			AccountID: account.ID, PlatformCode: account.PlatformCode,
			State: "unknown", Stage: "RUNTIME_EXECUTION",
			ErrorCode: "EGO_METRICS_COLLECTION_FAILED", RunError: SafeCreatorMetricErrorDetail(err),
		}
		return failure, err
	}
	if !observed.HandoffDone {
		service.cleanupTaskSpace(space.SpaceID)
		return nil, fmt.Errorf("EGO_HANDOFF_FAILED")
	}
	result := &CreatorMetricCollectResult{
		AccountID: account.ID, PlatformCode: account.PlatformCode, State: observed.State,
		RangeVerified: observed.RangeVerified, EvidenceCode: observed.EvidenceCode,
		MetricScope: observed.MetricScope, ScopedEngagement: observed.ScopedEngagement,
		ScopeSource: observed.ScopeSource, MetricEvidence: observed.MetricEvidence, ScopedEvidence: observed.ScopedEvidence,
		ErrorCode: observed.ErrorCode, PageOrigin: safePageOrigin(observed.PageOrigin), PostCount: len(observed.Posts),
		Stage: observed.Stage, PageTitle: strings.TrimSpace(observed.PageTitle), MatchedLabels: observed.MatchedLabels,
		MatchedMarkers: observed.MatchedMarkers, TextLength: observed.TextLength, IframeCount: observed.IframeCount,
		LabelHints: observed.LabelHints, RangeHints: observed.RangeHints, RangeControls: observed.RangeControls, RouteHints: safeCreatorMetricRouteHints(observed.RouteHints, platform), MetricCandidates: observed.MetricCandidates,
		ResourceHints: safeCreatorMetricRouteHints(observed.ResourceHints, platform),
	}
	if observed.State == "logged_out" {
		result.State = "skipped"
		result.RangeVerified = false
		result.EvidenceCode = ""
		result.PostCount = 0
	} else if observed.State != "observed" && observed.State != "unknown" && observed.State != "skipped" {
		result.State = "unknown"
		result.RangeVerified = false
		result.EvidenceCode = ""
		result.PostCount = 0
		if result.ErrorCode == "" {
			result.ErrorCode = "METRICS_RESULT_INVALID"
		}
	}
	completeDouyinWindow := false
	for _, diagnostic := range observed.ResourceDiagnostics {
		resource, ok := safeCreatorMetricResource(diagnostic.Resource, platform)
		if !ok {
			continue
		}
		for _, hint := range diagnostic.RangeHints {
			if hint == "resource_complete_7_day_window" {
				completeDouyinWindow = true
			}
		}
		sanitized := CreatorMetricResourceDiagnostic{
			Resource: resource, NumericCandidates: safeCreatorMetricCandidates(diagnostic.NumericCandidates),
			RangeHints: safeCreatorMetricRangeHints(diagnostic.RangeHints), ShapeHints: safeCreatorMetricShapeHints(diagnostic.ShapeHints),
		}
		if observed.EvidenceCode == "REVIEWED_ANALYTICS_RESOURCE" &&
			(len(sanitized.NumericCandidates) == 0 || len(sanitized.RangeHints) == 0) {
			continue
		}
		result.ResourceDiagnostics = append(result.ResourceDiagnostics, sanitized)
		if len(result.ResourceDiagnostics) == 8 {
			break
		}
	}
	if observed.State == "observed" && observed.EvidenceCode == "REVIEWED_ANALYTICS_RESOURCE" && len(result.ResourceDiagnostics) == 0 {
		observed.State = "unknown"
		observed.RangeVerified = false
		observed.EvidenceCode = ""
		observed.ErrorCode = "METRICS_RESOURCE_PROVENANCE_MISSING"
		result.State = observed.State
		result.RangeVerified = observed.RangeVerified
		result.EvidenceCode = observed.EvidenceCode
		result.ErrorCode = observed.ErrorCode
		result.PostCount = 0
	}
	if account.PlatformCode == "douyin" && observed.State != "observed" && completeDouyinWindow {
		observed.State = "observed"
		observed.RangeVerified = true
		observed.EvidenceCode = "REVIEWED_CONTENT_RESOURCE_EMPTY"
		observed.ErrorCode = ""
		observed.Stage = "COMPLETE"
		if observed.Metrics == nil {
			observed.Metrics = map[string]int64{}
		}
		result.State = observed.State
		result.RangeVerified = observed.RangeVerified
		result.EvidenceCode = observed.EvidenceCode
		result.ErrorCode = observed.ErrorCode
		result.Stage = observed.Stage
	}
	for _, attempt := range observed.Attempts {
		candidate, candidateOK := safeCreatorMetricPage(attempt.Candidate, platform)
		actual, actualOK := safeCreatorMetricPage(attempt.Actual, platform)
		if !candidateOK || !actualOK {
			continue
		}
		result.Attempts = append(result.Attempts, CreatorMetricAttempt{
			Candidate: candidate, Actual: actual, PageTitle: strings.TrimSpace(attempt.PageTitle),
			MatchedMarkers: attempt.MatchedMarkers, MatchedLabels: attempt.MatchedLabels,
			TextLength: attempt.TextLength, IframeCount: attempt.IframeCount,
		})
		if len(result.Attempts) == len(platform.Candidates) {
			break
		}
	}
	if observed.State != "observed" {
		result.PostCount = 0
		if err := service.persistCollectionState(result, service.now()); err != nil {
			return nil, err
		}
		return result, nil
	}
	// Kuaishou pages contain promotion and reward copy that resembles account
	// analytics (for example a generic "1000 plays" threshold). Page-wide DOM
	// text cannot bind those values to the selected account and seven-day range.
	// Only the reviewed Kuaishou resource contracts may persist metrics.
	if account.PlatformCode == "kuaishou" && observed.EvidenceCode == "REVIEWED_ANALYTICS_DOM" {
		result.State = "unknown"
		result.RangeVerified = false
		result.EvidenceCode = ""
		result.ErrorCode = "METRICS_DOM_UNBOUND"
		result.PostCount = 0
		if err := service.persistCollectionState(result, service.now()); err != nil {
			return nil, err
		}
		return result, nil
	}
	if observed.EvidenceCode == "REVIEWED_ANALYTICS_DOM" {
		delete(observed.Metrics, "followers")
		if len(observed.Metrics) == 0 && len(observed.Posts) == 0 {
			result.State = "unknown"
			result.RangeVerified = false
			result.EvidenceCode = ""
			result.ErrorCode = "METRICS_DOM_UNBOUND"
			result.PostCount = 0
			if err := service.persistCollectionState(result, service.now()); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
	if !observed.RangeVerified {
		result.State = "unknown"
		result.ErrorCode = "METRICS_RANGE_UNVERIFIED"
		result.EvidenceCode = ""
		result.PostCount = 0
		if err := service.persistCollectionState(result, service.now()); err != nil {
			return nil, err
		}
		return result, nil
	}
	rawEvidence := map[string]any{
		"matched_labels":  observed.MatchedLabels,
		"range_verified":  observed.RangeVerified,
		"metric_scope":    observed.MetricScope,
		"scope_source":    observed.ScopeSource,
		"metric_evidence": observed.MetricEvidence,
		"post_count":      len(observed.Posts),
	}
	if observed.ScopedEngagement != nil {
		rawEvidence["scoped_engagement"] = *observed.ScopedEngagement
	}
	if observed.ScopedEvidence != nil {
		rawEvidence["scoped_evidence"] = observed.ScopedEvidence
	}
	if strings.Contains(observed.EvidenceCode, "_RESOURCE") {
		resourceEvidence := make([]map[string]any, 0, len(result.ResourceDiagnostics))
		for _, diagnostic := range result.ResourceDiagnostics {
			numericKeys := make([]string, 0, len(diagnostic.NumericCandidates))
			for key := range diagnostic.NumericCandidates {
				numericKeys = append(numericKeys, key)
			}
			sort.Strings(numericKeys)
			resourceEvidence = append(resourceEvidence, map[string]any{
				"resource": diagnostic.Resource, "range_hints": diagnostic.RangeHints, "numeric_keys": numericKeys,
			})
		}
		rawEvidence["resource_diagnostics"] = resourceEvidence
	}
	rawJSON, _ := json.Marshal(rawEvidence)
	postInputs := make([]db.CreatorPostMetricInput, 0, len(observed.Posts))
	postSource := `{"source":"reviewed_dom"}`
	if observed.EvidenceCode == "REVIEWED_CONTENT_RESOURCE" {
		postSource = `{"source":"reviewed_resource"}`
	}
	for _, post := range observed.Posts {
		postURL, postURLAllowed := safeCreatorMetricPage(post.PostURL, platform)
		if !postURLAllowed {
			postURL = ""
		}
		postInputs = append(postInputs, db.CreatorPostMetricInput{
			ExternalPostID: strings.TrimSpace(post.ExternalPostID), Title: strings.TrimSpace(post.Title),
			PublishedAt: strings.TrimSpace(post.PublishedAt), PostURL: postURL,
			Views: post.Views, Likes: post.Likes, Comments: post.Comments, Shares: post.Shares,
			Favorites: post.Favorites, RawJSON: postSource,
		})
	}
	now := service.now()
	followersVerified := observed.EvidenceCode == "REVIEWED_ANALYTICS_RESOURCE" &&
		creatorMetricResourceHasFollowerField(result.ResourceDiagnostics)
	postCount := observed.Metrics["posts"]
	if (observed.EvidenceCode == "REVIEWED_CONTENT_DOM" || observed.EvidenceCode == "REVIEWED_CONTENT_RESOURCE") && postCount == 0 {
		postCount = int64(len(postInputs))
	}
	snapshot, err := service.db.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: account.PlatformCode, SnapshotDate: creatorMetricSnapshotDate(now),
		WindowDays: 7, RangeVerified: observed.RangeVerified, Followers: observed.Metrics["followers"],
		FollowersVerified: followersVerified,
		Views:             observed.Metrics["views"], Likes: observed.Metrics["likes"], Comments: observed.Metrics["comments"],
		Shares: observed.Metrics["shares"], Favorites: observed.Metrics["favorites"], Posts: postCount,
		EvidenceCode: observed.EvidenceCode, RawJSON: string(rawJSON), CollectedAt: now.UTC().Format(time.RFC3339),
	}, postInputs)
	if err != nil {
		return nil, err
	}
	result.Snapshot = snapshot
	result.PostCount = len(postInputs)
	if err := service.persistCollectionState(result, now); err != nil {
		return nil, err
	}
	return result, nil
}

func creatorMetricResourceHasFollowerField(diagnostics []CreatorMetricResourceDiagnostic) bool {
	for _, diagnostic := range diagnostics {
		for key := range diagnostic.NumericCandidates {
			leaf := strings.ToLower(strings.TrimSuffix(key[strings.LastIndex(key, ".")+1:], "[]"))
			switch leaf {
			case "follower_count", "followers_count", "fan_count", "fans_count":
				return true
			}
		}
	}
	return false
}

func (service *CreatorMetricsService) isDuplicateAccountBinding(account db.Account) (bool, error) {
	identifier := strings.TrimSpace(account.AccountIdentifier)
	if identifier == "" {
		return false, nil
	}
	accounts, err := service.db.ListAccountsByPlatform(account.PlatformCode)
	if err != nil {
		return false, err
	}
	for _, candidate := range accounts {
		if candidate.ID == account.ID || !candidate.Enabled || candidate.LoginState != db.AccountAuthLoggedIn ||
			candidate.IdentityID >= account.IdentityID || !strings.EqualFold(strings.TrimSpace(candidate.AccountIdentifier), identifier) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func (service *CreatorMetricsService) persistCollectionState(result *CreatorMetricCollectResult, collectedAt time.Time) error {
	if result == nil {
		return fmt.Errorf("EGO_METRICS_RESULT_INVALID")
	}
	_, err := service.db.UpsertCreatorMetricCollectionState(db.CreatorMetricCollectionStateInput{
		AccountID: result.AccountID, PlatformCode: result.PlatformCode, State: result.State,
		ErrorCode: result.ErrorCode, EvidenceCode: result.EvidenceCode,
		RangeVerified: result.RangeVerified, PostCount: result.PostCount,
		CollectedAt: collectedAt.UTC().Format(time.RFC3339),
	})
	return err
}

func safeCreatorMetricRouteHints(values []string, platform creatorMetricPlatform) []string {
	result := make([]string, 0, min(len(values), 40))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		clean, hostMatches := safeCreatorMetricPage(value, platform)
		parsed, _ := url.Parse(clean)
		path := strings.ToLower(parsed.Path + parsed.Fragment)
		if !hostMatches || !strings.ContainsAny(path, "abcdefghijklmnopqrstuvwxyz") {
			continue
		}
		matched := false
		for _, keyword := range []string{"data", "analysis", "stat", "overview", "content", "work", "video", "note", "post", "manage", "center", "dashboard"} {
			if strings.Contains(path, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if _, duplicate := seen[clean]; duplicate {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
		if len(result) == 40 {
			break
		}
	}
	return result
}

func safeCreatorMetricResource(value string, platform creatorMetricPlatform) (string, bool) {
	clean, hostMatches := safeCreatorMetricPage(value, platform)
	if !hostMatches {
		return "", false
	}
	parsed, err := url.Parse(clean)
	if err != nil {
		return "", false
	}
	for _, allowedPath := range platform.Resources {
		if parsed.Path == allowedPath {
			return clean, true
		}
	}
	return "", false
}

func safeCreatorMetricCandidates(values map[string]int64) map[string]int64 {
	result := make(map[string]int64, min(len(values), 60))
	allowedLeaves := map[string]struct{}{
		"view_count": {}, "play_count": {}, "exposure_count": {}, "impression_count": {},
		"like_count": {}, "digg_count": {}, "comment_count": {}, "share_count": {},
		"collect_count": {}, "favorite_count": {}, "favorited_count": {}, "total_favorited": {},
		"fan_count": {}, "fans_count": {}, "follower_count": {}, "followers_count": {},
		"publish_count": {}, "post_count": {}, "note_count": {}, "work_count": {}, "video_count": {},
		"vv": {}, "pv": {}, "uv": {},
	}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if value < 0 || len(key) == 0 || len(key) > 180 {
			continue
		}
		valid := true
		for _, char := range key {
			if unicode.IsLetter(char) || unicode.IsDigit(char) || strings.ContainsRune("._-[]", char) {
				continue
			}
			valid = false
			break
		}
		if !valid {
			continue
		}
		leaf := strings.TrimSuffix(key[strings.LastIndex(key, ".")+1:], "[]")
		if _, allowed := allowedLeaves[strings.ToLower(leaf)]; !allowed && !isChineseCreatorMetricLeaf(leaf) {
			continue
		}
		result[key] = value
		if len(result) == 60 {
			break
		}
	}
	return result
}

func isChineseCreatorMetricLeaf(value string) bool {
	for _, prefix := range []string{"观看", "播放", "浏览", "曝光", "点赞", "评论", "分享", "转发", "收藏", "粉丝"} {
		if value == prefix || value == prefix+"数" || value == prefix+"量" {
			return true
		}
	}
	return value == "作品数"
}

func safeCreatorMetricRangeHints(values []string) []string {
	result := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, value := range values {
		if value != "explicit_7_day_marker" && value != "query_7_day_range" &&
			value != "resource_complete_7_day_window" && value != "resource_explicit_7_day_bucket" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func safeCreatorMetricShapeHints(values []string) []string {
	allowedLeaves := map[string]struct{}{
		"create_time": {}, "createTime": {}, "publish_time": {}, "publishTime": {},
		"published_at": {}, "publishedAt": {}, "release_time": {}, "releaseTime": {},
		"has_more": {}, "hasMore": {}, "cursor": {}, "max_cursor": {}, "maxCursor": {},
		"count": {}, "limit": {}, "page": {}, "page_size": {},
	}
	result := make([]string, 0, min(len(values), 20))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 180 {
			continue
		}
		valid := true
		for _, character := range value {
			if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("._-[]", character) {
				continue
			}
			valid = false
			break
		}
		leaf := strings.TrimSuffix(value[strings.LastIndex(value, ".")+1:], "[]")
		if !valid {
			continue
		}
		if _, allowed := allowedLeaves[leaf]; !allowed {
			lower := strings.ToLower(leaf)
			if strings.Contains(lower, "token") || strings.Contains(lower, "cookie") || strings.Contains(lower, "secret") ||
				strings.Contains(lower, "auth") || strings.Contains(lower, "ticket") || strings.Contains(lower, "signature") ||
				strings.Contains(lower, "credential") {
				continue
			}
			relevant := false
			for _, keyword := range []string{"data", "metric", "stat", "view", "play", "like", "comment", "share", "collect", "favorite", "fan", "follow", "time", "date", "range", "period", "list", "item", "post", "video", "work", "count", "cursor", "page", "pv", "uv", "vv"} {
				if strings.Contains(lower, keyword) {
					relevant = true
					break
				}
			}
			if !relevant {
				continue
			}
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == 20 {
			break
		}
	}
	return result
}

func safeCreatorMetricPage(value string, platform creatorMetricPlatform) (string, bool) {
	clean := safePageOrigin(value)
	parsed, err := url.Parse(clean)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == platform.Host {
		return clean, true
	}
	for _, suffix := range platform.Suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return clean, true
		}
	}
	return "", false
}
