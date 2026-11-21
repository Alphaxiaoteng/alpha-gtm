package egolite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

type creatorMetricsRunner struct {
	db              *db.DB
	bindErr         error
	program         string
	payload         map[string]any
	spaceID         int64
	rebindID        int64
	probeCount      int
	membershipReads int
	programs        []string
	programErr      error
}

type creatorMetricsTimeoutRunner struct {
	db       *db.DB
	deadline time.Time
	programs []string
}

type creatorMetricsUserControlledRunner struct{}

func (runner *creatorMetricsTimeoutRunner) Run(ctx context.Context, program string, result any) error {
	if handled, err := bindingProbeAnswer(runner.db, program, result); handled {
		return err
	}
	runner.programs = append(runner.programs, program)
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("EGO_RUNTIME_TIMEOUT")
	}
	runner.deadline = deadline
	return fmt.Errorf("EGO_RUNTIME_TIMEOUT")
}

func (creatorMetricsUserControlledRunner) Run(_ context.Context, program string, result any) error {
	if handled, err := bindingProbeAnswer(nil, program, result); handled {
		return err
	}
	return fmt.Errorf("EGO_SPACE_USER_CONTROLLED")
}

type creatorMetricsCleanupRunner struct {
	db           *db.DB
	spaceID      int64
	firstErr     error
	firstPayload map[string]any
	programs     []string
	deadlines    []time.Time
}

func (runner *creatorMetricsCleanupRunner) Run(ctx context.Context, program string, result any) error {
	if strings.Contains(program, "JSON.stringify({ spaces })") {
		raw, _ := json.Marshal(map[string]any{"spaces": []any{map[string]any{"id": runner.spaceID, "ownership": "agent"}}})
		return json.Unmarshal(raw, result)
	}
	runner.programs = append(runner.programs, program)
	if deadline, ok := ctx.Deadline(); ok {
		runner.deadlines = append(runner.deadlines, deadline)
	}
	if len(runner.programs) == 1 {
		if runner.firstPayload != nil {
			raw, _ := json.Marshal(runner.firstPayload)
			if err := json.Unmarshal(raw, result); err != nil {
				return err
			}
		}
		return runner.firstErr
	}
	return json.Unmarshal([]byte(`{"cleanupHandoffDone":true}`), result)
}

func isBareSpaceProbe(program string) bool {
	return strings.Contains(program, "alpha-space-probe")
}

func (runner *creatorMetricsRunner) Run(_ context.Context, program string, result any) error {
	if strings.Contains(program, "const profileResult = await listProfiles()") {
		runner.programs = append(runner.programs, program)
		if runner.bindErr != nil {
			return runner.bindErr
		}
		// The re-bind reports the Space id the runtime actually created.
		payload := map[string]any{
			"id": runner.rebindID, "taskId": "alpha-nexus/matrix-1",
			"name": "alpha-nexus/matrix-1", "ownership": "agent",
		}
		raw, _ := json.Marshal(payload)
		return json.Unmarshal(raw, result)
	}
	// Scripted vanished-Space mode: the first membership read reports the Space
	// as gone, and once the rebuild happened the new Space is listed.
	if runner.rebindID > 0 && isBareSpaceProbe(program) {
		runner.programs = append(runner.programs, program)
		runner.membershipReads++
		listing := []any{}
		if runner.membershipReads > 1 {
			listing = append(listing, map[string]any{"id": runner.rebindID, "ownership": "agent"})
		}
		raw, _ := json.Marshal(map[string]any{"spaces": listing})
		return json.Unmarshal(raw, result)
	}
	if handled, err := bindingProbeAnswer(runner.db, program, result); handled {
		return err
	}
	runner.programs = append(runner.programs, program)
	runner.probeCount = len(runner.programs) - 1
	runner.program = program
	if runner.programErr != nil {
		return runner.programErr
	}
	raw, _ := json.Marshal(runner.payload)
	return json.Unmarshal(raw, result)
}

func TestCreatorMetricsUsesReviewedEgoProgramPersistsSnapshotAndHandsBack(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("指标浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-login",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 31, "metrics-space", "alpha-nexus/metrics"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 31, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_DOM",
		"pageOrigin":    "https://creator.douyin.com/creator-micro/data-center?token=redacted",
		"metrics":       map[string]any{"views": 3200, "likes": 180, "comments": 30, "shares": 20, "favorites": 40, "posts": 3},
		"posts":         []map[string]any{{"externalPostId": "video-1", "title": "真实作品", "postURL": "https://evil.example/video/video-1?token=secret", "views": 2600}},
		"matchedLabels": []string{"播放量", "点赞量"},
		"routeHints": []string{
			"https://creator.douyin.com/creator-micro/data-center?token=redacted",
			"https://evil.example/data-center?token=secret",
		},
		"resourceDiagnostics": []map[string]any{
			{
				"resource":          "https://creator.douyin.com/janus/douyin/creator/data/author/upgrade/?token=redacted",
				"numericCandidates": map[string]any{"data.play_count": 3200, "unsafe key!": 9},
				"rangeHints":        []string{"query_7_day_range", "raw_secret"},
				"shapeHints":        []string{"cursor", "query.count", "data.metric_list", "aweme_list[].create_time", "aweme_list[].unsafe key", "token.value"},
			},
			{"resource": "https://evil.example/api/data", "numericCandidates": map[string]any{"views": 99}},
		},
		"attempts": []map[string]any{{
			"candidate": "https://creator.douyin.com/creator-micro/data-center?token=redacted",
			"actual":    "https://creator.douyin.com/creator-micro/content/manage?token=redacted",
			"pageTitle": "抖音创作者中心", "matchedMarkers": []string{"内容管理"},
		}},
		"handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "observed" || result.Snapshot == nil || result.Snapshot.Views != 3200 || result.PostCount != 1 {
		t.Fatalf("result=%+v", result)
	}
	overview, overviewErr := database.CreatorMetricOverview(7, time.Now())
	if overviewErr != nil || len(overview.TopPosts) != 1 || overview.TopPosts[0].PostURL != "" {
		t.Fatalf("cross-platform post URL was retained: overview=%+v err=%v", overview, overviewErr)
	}
	if len(result.RouteHints) != 1 || result.RouteHints[0] != "https://creator.douyin.com/creator-micro/data-center" {
		t.Fatalf("route hints must be same-platform and query-free: %+v", result.RouteHints)
	}
	if len(result.Attempts) != 1 || result.Attempts[0].Actual != "https://creator.douyin.com/creator-micro/content/manage" {
		t.Fatalf("attempts must retain only sanitized request and landing routes: %+v", result.Attempts)
	}
	if len(result.ResourceDiagnostics) != 1 ||
		result.ResourceDiagnostics[0].Resource != "https://creator.douyin.com/janus/douyin/creator/data/author/upgrade/" ||
		result.ResourceDiagnostics[0].NumericCandidates["data.play_count"] != 3200 ||
		len(result.ResourceDiagnostics[0].NumericCandidates) != 1 ||
		strings.Join(result.ResourceDiagnostics[0].RangeHints, ",") != "query_7_day_range" ||
		strings.Join(result.ResourceDiagnostics[0].ShapeHints, ",") != "cursor,query.count,data.metric_list,aweme_list[].create_time" {
		t.Fatalf("resource diagnostics were not allowlisted and sanitized: %+v", result.ResourceDiagnostics)
	}
	for _, required := range []string{"await takeOverTaskSpace(31)", "await handOffTaskSpace(31)", "const metricResult =", "REVIEWED_ANALYTICS_DOM", "gotoAndWait(originalURL", "extractSafeRoutes", "data-route", "waitForNetworkIdle", "视频播放量", "发布笔记数", `browserFetch(raw, { method: "GET" })`, "firstSerialized.length > 1048576", "nextSerialized.length > 1048576", "pageIndex < 5", `searchParams.set("max_cursor"`, "setTimeout(resolve, 600)", "const seenCursors = new Set", "const seenItems = new Set", "slice(0, 250)", `cdp("Network.enable"`, "drainEvents()", `cdp("Network.getResponseBody"`, "const privateResourceResponses = []", "capturedResponses = []", "body.length > 1048576"} {
		if !strings.Contains(runner.program, required) {
			t.Fatalf("collector program missing %q", required)
		}
	}
	for _, required := range []string{"const metricDeadlineAt = Date.now() + 28000", "Date.now() < metricDeadlineAt", "timeout: 8", "timeout: 3", "EGO_METRICS_TIMEOUT"} {
		if !strings.Contains(runner.program, required) {
			t.Fatalf("collector program missing bounded-runtime guard %q", required)
		}
	}
	for _, forbidden := range []string{"document.cookie", "localStorage", "sessionStorage", "uploadFile", "click(", "oldestAgeDays() < 7.999", "metricResult.privateResourceResponses", "metricResult.resourceBodies"} {
		if strings.Contains(runner.program, forbidden) {
			t.Fatalf("collector program contains forbidden %q", forbidden)
		}
	}
	for _, placeholder := range []string{"__ALPHA_MARKERS__", "__ALPHA_SUFFIXES__", "__ALPHA_RESOURCES__"} {
		if strings.Contains(runner.program, placeholder) {
			t.Fatalf("collector program did not bind %s into the page expression", placeholder)
		}
	}
	if node, lookupErr := exec.LookPath("node"); lookupErr == nil {
		path := filepath.Join(t.TempDir(), "creator-metrics.mjs")
		if err := os.WriteFile(path, []byte(runner.program), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
			t.Fatalf("generated JavaScript is invalid: %v\n%s", err, output)
		}
	}
}

func TestCreatorMetricsParsesNestedJSONStringInCapturedAuthenticatedResponseWithoutGETReplayOrRawOutput(t *testing.T) {
	node, lookupErr := exec.LookPath("node")
	if lookupErr != nil {
		t.Skip("node is required for the generated EgoLite program contract")
	}
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("视频号响应采集")
	account, _ := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-network-response",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 91, "metrics-network-response", "alpha-nexus/metrics-network-response"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 91, payload: map[string]any{
		"state": "unknown", "rangeVerified": false, "errorCode": "METRICS_DOM_UNRECOGNIZED", "handoffDone": true,
	}}
	if _, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}

	const resourceURL = "https://channels.weixin.qq.com/cgi-bin/mmfinderassistant-bin/helper/helper_mmdata"
	const responseBody = `{"data":"{\"seven\":{\"view_count\":123,\"like_count\":7},\"opaque\":\"do-not-persist-response\",\"invalid\":\"{not-json\"}"}`
	wrapper := `
let currentURL = "https://channels.weixin.qq.com/platform/statistics/overview"
let networkDrained = false
globalThis.setTimeout = (callback) => { queueMicrotask(callback); return 0 }
globalThis.takeOverTaskSpace = async () => ({ done: true })
globalThis.handOffTaskSpace = async () => ({ done: true })
globalThis.gotoAndWait = async (url) => { currentURL = String(url) }
globalThis.waitForNetworkIdle = async () => {}
globalThis.pageInfo = async () => ({ url: currentURL })
globalThis.snapshotText = async () => ({})
globalThis.js = async (expression) => {
  if (String(expression).trim().startsWith('performance.getEntriesByType("resource")')) return []
  return {
    metrics: {}, posts: [], matchedLabels: [], matchedMarkers: ["数据中心"], labelHints: [],
    routeHints: [], resourceHints: [], resourceCandidates: [], reviewedMarker: true,
    rangeVerified: false, contentRangeVerified: false, iframeCount: 0, textLength: 8,
    pageTitle: "视频号助手", loginForm: false
  }
}
globalThis.drainEvents = async () => {
  if (networkDrained) return []
  networkDrained = true
  return [{
    method: "Network.responseReceived",
    params: { requestId: "authenticated-post-1", response: { url: "` + resourceURL + `", status: 200 } }
  }]
}
globalThis.cdp = async (method) => {
  if (method === "Network.enable") return {}
  if (method === "Network.getResponseBody") return {
    base64Encoded: false,
    body: ` + strconv.Quote(responseBody) + `
  }
  return {}
}
globalThis.browserFetch = async () => { throw new Error("unexpected_get_replay") }
globalThis.cliLog = (value) => process.stdout.write(String(value) + "\n")
` + runner.program
	path := filepath.Join(t.TempDir(), "creator-metrics-network-response.mjs")
	if err := os.WriteFile(path, []byte(wrapper), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(node, path).CombinedOutput()
	if err != nil {
		t.Fatalf("generated collector failed: %v\n%s", err, output)
	}
	line := string(output)
	if !strings.Contains(line, `"state":"observed"`) || !strings.Contains(line, `"rangeVerified":true`) ||
		!strings.Contains(line, `"evidenceCode":"REVIEWED_ANALYTICS_RESOURCE"`) ||
		!strings.Contains(line, `"views":123`) || !strings.Contains(line, `"likes":7`) ||
		!strings.Contains(line, `data.seven.view_count`) {
		t.Fatalf("captured response was not parsed: %s", line)
	}
	if strings.Contains(line, "do-not-persist-response") || strings.Contains(line, "unexpected_get_replay") {
		t.Fatalf("collector replayed GET or exposed a raw response: %s", line)
	}
}

func TestCreatorMetricsReReadsMetaDOMAfterUnsuccessfulSevenDaySwitch(t *testing.T) {
	if _, lookupErr := exec.LookPath("node"); lookupErr != nil {
		t.Skip("node is required for the generated EgoLite program contract")
	}
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("Meta 日期范围诊断")
	account, _ := database.CreateAccountForIdentity(identity.ID, "meta", "Meta", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-meta-range-diagnostic",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 93, "metrics-meta-range-diagnostic", "alpha-nexus/metrics-meta-range-diagnostic"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 93, payload: map[string]any{
		"state": "unknown", "errorCode": "METRICS_DOM_UNRECOGNIZED", "handoffDone": true,
	}}
	if _, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	wrapper := `
let currentURL = "https://business.facebook.com/latest/insights/overview"
let domReads = 0
globalThis.setTimeout = (callback) => { queueMicrotask(callback); return 0 }
globalThis.takeOverTaskSpace = async () => ({ done: true })
globalThis.handOffTaskSpace = async () => ({ done: true })
globalThis.gotoAndWait = async (url) => { currentURL = String(url) }
globalThis.waitForNetworkIdle = async () => {}
globalThis.pageInfo = async () => ({ url: currentURL })
globalThis.snapshotText = async () => ({})
globalThis.js = async (expression) => {
  const source = String(expression)
  if (source.trim().startsWith('performance.getEntriesByType("resource")')) return []
  if (source.includes('过去') && source.includes('28') && source.includes('targets=')) return false
  domReads += 1
  return {
    metrics: {}, posts: [], matchedLabels: [], matchedMarkers: ["Insights"], labelHints: [],
    routeHints: [], resourceHints: [], resourceCandidates: [], reviewedMarker: true,
    rangeVerified: false, rangeHints: ["过去 28 天"],
    rangeControls: domReads === 1 ? ["过去 28 天"] : ["过去 28 天", "过去 7 天"],
    contentRangeVerified: false, iframeCount: 0, textLength: 1200,
    pageTitle: "Meta Insights", loginForm: false
  }
}
globalThis.drainEvents = async () => []
globalThis.cdp = async () => ({})
globalThis.browserFetch = async () => { throw new Error("unexpected_get_replay") }
globalThis.cliLog = (value) => process.stdout.write(String(value) + "\n")
` + runner.program
	path := filepath.Join(t.TempDir(), "creator-metrics-meta-range.mjs")
	if err := os.WriteFile(path, []byte(wrapper), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("node", path).CombinedOutput()
	if err != nil {
		t.Fatalf("generated Meta collector failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"rangeControls":["过去 28 天","过去 7 天"]`) {
		t.Fatalf("Meta DOM was not re-read after the failed switch attempt: %s", output)
	}
}

func TestCreatorMetricsParsesReviewedKuaishouSevenDayOverviewResource(t *testing.T) {
	line := runKuaishouCreatorMetricsProgram(t,
		"https://cp.kuaishou.com/rest/cp/creator/analysis/pc/home/author/overview",
		`{"currentTime":1785628800000,"data":{"basicData":[`+
			`{"name":"播放量","sumCount":1234},{"name":"点赞量","sumCount":56},`+
			`{"name":"评论量","sumCount":7},{"name":"分享量","sumCount":8},`+
			`{"name":"收藏量","sumCount":9}]}}`)
	for _, fragment := range []string{
		`"state":"observed"`, `"rangeVerified":true`,
		`"evidenceCode":"REVIEWED_ANALYTICS_RESOURCE"`,
		`"views":1234`, `"likes":56`, `"comments":7`, `"shares":8`, `"favorites":9`,
	} {
		if !strings.Contains(line, fragment) {
			t.Fatalf("Kuaishou seven-day overview missing %q: %s", fragment, line)
		}
	}
}

func TestCreatorMetricsParsesReviewedKuaishouCompleteContentWindow(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-24 * time.Hour).UnixMilli()
	old := now.Add(-9 * 24 * time.Hour).UnixMilli()
	body := fmt.Sprintf(`{"currentTime":%d,"data":{"photoList":{"totalCount":2,"photoItems":[`+
		`{"photoId":"recent-1","title":"近七天作品","publishTime":%d,"playCount":210,"likeCount":12,"commentCount":3,"shareCount":2,"collectCount":4},`+
		`{"photoId":"old-1","title":"窗口外作品","publishTime":%d,"playCount":900,"likeCount":90,"commentCount":30,"shareCount":20,"collectCount":40}`+
		`]}}}`, now.UnixMilli(), recent, old)
	line := runKuaishouCreatorMetricsProgram(t,
		"https://cp.kuaishou.com/rest/cp/creator/analysis/pc/home/photo/list", body)
	for _, fragment := range []string{
		`"state":"observed"`, `"rangeVerified":true`,
		`"evidenceCode":"REVIEWED_CONTENT_RESOURCE"`,
		`"posts":1`, `"views":210`, `"likes":12`, `"comments":3`, `"shares":2`, `"favorites":4`,
		`"externalPostId":"recent-1"`,
	} {
		if !strings.Contains(line, fragment) {
			t.Fatalf("Kuaishou content window missing %q: %s", fragment, line)
		}
	}
	if strings.Contains(line, "old-1") || strings.Contains(line, "窗口外作品") {
		t.Fatalf("Kuaishou content window retained an older post: %s", line)
	}
}

func TestCreatorMetricsAcceptsReviewedKuaishouEmptyContentWindow(t *testing.T) {
	line := runKuaishouCreatorMetricsProgram(t,
		"https://cp.kuaishou.com/rest/cp/creator/analysis/pc/home/photo/list",
		`{"currentTime":1785628800000,"data":{"photoList":{"totalCount":0,"photoItems":[]}}}`)
	for _, fragment := range []string{
		`"state":"observed"`, `"rangeVerified":true`,
		`"evidenceCode":"REVIEWED_CONTENT_RESOURCE_EMPTY"`, `"posts":0`,
	} {
		if !strings.Contains(line, fragment) {
			t.Fatalf("Kuaishou empty content window missing %q: %s", fragment, line)
		}
	}
}

func runKuaishouCreatorMetricsProgram(t *testing.T, resourceURL, responseBody string) string {
	t.Helper()
	if _, lookupErr := exec.LookPath("node"); lookupErr != nil {
		t.Skip("node is required for the generated EgoLite program contract")
	}
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("快手资源采集")
	account, _ := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-kuaishou-resource",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 92, "metrics-kuaishou-resource", "alpha-nexus/metrics-kuaishou-resource"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 92, payload: map[string]any{
		"state": "unknown", "rangeVerified": false, "errorCode": "METRICS_DOM_UNRECOGNIZED", "handoffDone": true,
	}}
	if _, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	wrapper := `
let currentURL = "https://cp.kuaishou.com/profile"
let networkDrained = false
globalThis.setTimeout = (callback) => { queueMicrotask(callback); return 0 }
globalThis.takeOverTaskSpace = async () => ({ done: true })
globalThis.handOffTaskSpace = async () => ({ done: true })
globalThis.gotoAndWait = async (url) => { currentURL = String(url) }
globalThis.waitForNetworkIdle = async () => {}
globalThis.pageInfo = async () => ({ url: currentURL })
globalThis.snapshotText = async () => ({})
globalThis.js = async (expression) => {
  if (String(expression).trim().startsWith('performance.getEntriesByType("resource")')) return []
  return {
    metrics: {}, posts: [], matchedLabels: [], matchedMarkers: ["数据概览"], labelHints: [],
    routeHints: [], resourceHints: [], resourceCandidates: [], reviewedMarker: true,
    rangeVerified: false, contentRangeVerified: false, iframeCount: 0, textLength: 8,
    pageTitle: "快手创作者服务平台", loginForm: false
  }
}
globalThis.drainEvents = async () => {
  if (networkDrained) return []
  networkDrained = true
  return [{
    method: "Network.responseReceived",
    params: { requestId: "kuaishou-authenticated-post", response: { url: ` + strconv.Quote(resourceURL) + `, status: 200 } }
  }]
}
globalThis.cdp = async (method) => {
  if (method === "Network.enable") return {}
  if (method === "Network.getResponseBody") return { base64Encoded: false, body: ` + strconv.Quote(responseBody) + ` }
  return {}
}
globalThis.browserFetch = async () => { throw new Error("unexpected_get_replay") }
globalThis.cliLog = (value) => process.stdout.write(String(value) + "\n")
` + runner.program
	path := filepath.Join(t.TempDir(), "creator-metrics-kuaishou-resource.mjs")
	if err := os.WriteFile(path, []byte(wrapper), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("node", path).CombinedOutput()
	if err != nil {
		t.Fatalf("generated Kuaishou collector failed: %v\n%s", err, output)
	}
	line := string(output)
	if strings.Contains(line, "unexpected_get_replay") || strings.Contains(line, responseBody) {
		t.Fatalf("Kuaishou collector replayed GET or exposed a raw response: %s", line)
	}
	return line
}

func TestCreatorMetricsSkipsLoggedOutAccountWithoutRunningEgo(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("未登录指标浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	runner := &creatorMetricsRunner{}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil || result.State != "skipped" || result.ErrorCode != "ACCOUNT_NOT_LOGGED_IN" || runner.program != "" {
		t.Fatalf("result=%+v err=%v program=%q", result, err, runner.program)
	}
}

func TestCreatorMetricsBindsReviewedCandidatesForExtendedPlatforms(t *testing.T) {
	tests := []struct {
		platform      string
		identifier    string
		manager       string
		wantFragments []string
	}{
		{platform: "douyin", wantFragments: []string{"https://creator.douyin.com/creator-micro/data-center/data-overview"}},
		{platform: "xhs", wantFragments: []string{`"candidates":["https://creator.xiaohongshu.com/statistics/account/v2","https://creator.xiaohongshu.com/statistics/data-analysis"`}},
		{platform: "wechat_video", wantFragments: []string{"https://channels.weixin.qq.com/micro/statistic/statistics/overview"}},
		{platform: "kuaishou", wantFragments: []string{
			"https://cp.kuaishou.com/profile",
			"/rest/cp/creator/analysis/pc/home/photo/list",
			"/rest/cp/creator/analysis/pc/home/author/overview",
		}},
		{platform: "tiktok", wantFragments: []string{"https://www.tiktok.com/tiktokstudio/analytics"}},
		{platform: "youtube", identifier: "UCt3-HqDX2QiTp31CnDe-xvg", wantFragments: []string{"studio.youtube.com/channel/UCt3-HqDX2QiTp31CnDe-xvg/analytics/tab-overview/period-week", `candidateIndex < maxCandidateAttempts`}},
		{platform: "meta", manager: "https://business.facebook.com/latest/home?business_id=1708507717147261&asset_id=1241410172381837", wantFragments: []string{"business.facebook.com/latest/insights/overview", "1708507717147261", "1241410172381837"}},
		{platform: "wechat_mp", wantFragments: []string{"https://mp.weixin.qq.com/", "用户分析"}},
		{platform: "csdn", identifier: "weixin_49486457", wantFragments: []string{"https://mp.csdn.net/", "一周小结"}},
	}
	for index, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			database := openServiceDB(t)
			identity, _ := database.CreateEgoIdentity("扩展平台指标浏览器")
			account, _ := database.CreateAccountForIdentity(identity.ID, test.platform, test.platform, "")
			if _, err := database.Exec(`UPDATE accounts SET account_identifier=? WHERE id=?`, test.identifier, account.ID); err != nil {
				t.Fatal(err)
			}
			if test.manager != "" {
				if err := database.UpdateAccountManagerAddr(account.ID, test.manager); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
				AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
				ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
				ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-candidate-" + test.platform,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, int64(120+index), "metrics-candidate-"+test.platform, "alpha-nexus/metrics-candidate-"+test.platform); err != nil {
				t.Fatal(err)
			}
			runner := &creatorMetricsRunner{spaceID: int64(120 + index), payload: map[string]any{
				"state": "unknown", "rangeVerified": false, "errorCode": "METRICS_DOM_UNRECOGNIZED", "handoffDone": true,
			}}
			if _, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID); err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.wantFragments {
				if !strings.Contains(runner.program, fragment) {
					t.Fatalf("%s collector missing %q", test.platform, fragment)
				}
			}
		})
	}
}

func TestCreatorMetricsNormalizesRuntimeLoginPageToSkipped(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("采集时掉线浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-runtime-logout",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 35, "metrics-runtime-logout", "alpha-nexus/metrics-runtime-logout"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 35, payload: map[string]any{
		"state": "logged_out", "errorCode": "LOGIN_REQUIRED", "handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil || result.State != "skipped" || result.ErrorCode != "LOGIN_REQUIRED" || result.PostCount != 0 {
		t.Fatalf("runtime login page was not normalized: result=%+v err=%v", result, err)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil || len(overview.Collections) != 1 || overview.Collections[0].State != "skipped" || overview.Collections[0].PostCount != 0 {
		t.Fatalf("normalized collection state was not persisted: overview=%+v err=%v", overview, err)
	}
}

func TestCreatorMetricsPersistsExplicitSevenDayZeroResourceSnapshot(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("零值指标浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-zero-login",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 33, "metrics-zero", "alpha-nexus/metrics-zero"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 33, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_RESOURCE",
		"metrics": map[string]any{"followers": 0, "views": 0, "likes": 0, "comments": 0, "shares": 0, "favorites": 0},
		"resourceDiagnostics": []map[string]any{{
			"resource":          "https://creator.xiaohongshu.com/api/galaxy/v2/creator/datacenter/livedata/overview?authorization=do-not-store",
			"numericCandidates": map[string]any{"data.seven.follower_count": 0, "data.seven.view_count": 0, "unsafe key!": 9},
			"rangeHints":        []string{"resource_explicit_7_day_bucket", "do-not-store"},
		}},
		"handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "observed" || result.Snapshot == nil || !result.Snapshot.FollowersVerified || result.Snapshot.EvidenceCode != "REVIEWED_ANALYTICS_RESOURCE" {
		t.Fatalf("explicit seven-day zero snapshot was not persisted: %+v", result)
	}
	var rawJSON string
	if err := database.QueryRow(`SELECT raw_json FROM creator_metric_snapshots WHERE id=?`, result.Snapshot.ID).Scan(&rawJSON); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"resource_diagnostics"`,
		`"resource":"https://creator.xiaohongshu.com/api/galaxy/v2/creator/datacenter/livedata/overview"`,
		`"range_hints":["resource_explicit_7_day_bucket"]`,
		`"numeric_keys":["data.seven.follower_count","data.seven.view_count"]`,
	} {
		if !strings.Contains(rawJSON, expected) {
			t.Fatalf("persisted resource provenance missing %q: %s", expected, rawJSON)
		}
	}
	for _, forbidden := range []string{"authorization", "do-not-store", "unsafe key!", "numeric_candidates"} {
		if strings.Contains(rawJSON, forbidden) {
			t.Fatalf("persisted resource provenance leaked %q: %s", forbidden, rawJSON)
		}
	}
}

func TestCreatorMetricsRejectsAnalyticsResourceWithoutSanitizedProvenance(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("无来源指标浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-resource-without-provenance",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 133, "metrics-no-provenance", "alpha-nexus/metrics-no-provenance"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 133, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_RESOURCE",
		"metrics": map[string]any{"views": 1}, "handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "unknown" || result.ErrorCode != "METRICS_RESOURCE_PROVENANCE_MISSING" || result.Snapshot != nil {
		t.Fatalf("resource metric without provenance must remain unknown: %+v", result)
	}
}

func TestCreatorMetricsRejectsAnalyticsResourceWithIncompleteSanitizedProvenance(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("不完整来源指标浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-resource-incomplete-provenance",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 134, "metrics-incomplete-provenance", "alpha-nexus/metrics-incomplete-provenance"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 134, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_RESOURCE",
		"metrics": map[string]any{"views": 9},
		"resourceDiagnostics": []map[string]any{{
			"resource":          "https://creator.xiaohongshu.com/api/galaxy/v2/creator/datacenter/livedata/overview",
			"numericCandidates": map[string]any{"data.view_count": 9},
			"rangeHints":        []string{},
		}},
		"handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("incomplete provenance must normalize to unknown, got error: %v", err)
	}
	if result.State != "unknown" || result.ErrorCode != "METRICS_RESOURCE_PROVENANCE_MISSING" || result.Snapshot != nil {
		t.Fatalf("incomplete resource provenance must remain unknown: %+v", result)
	}
	var snapshotCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM creator_metric_snapshots WHERE account_id=?`, account.ID).Scan(&snapshotCount); err != nil {
		t.Fatal(err)
	}
	if snapshotCount != 0 {
		t.Fatalf("incomplete provenance invented %d snapshots", snapshotCount)
	}
}

func TestCreatorMetricsPersistsZeroForCompleteDouyinResourceWindow(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("抖音完整零窗口")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-douyin-zero-login",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 34, "metrics-douyin-zero", "alpha-nexus/metrics-douyin-zero"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 34, payload: map[string]any{
		"state": "unknown", "rangeVerified": false, "errorCode": "METRICS_DOM_UNRECOGNIZED",
		"resourceDiagnostics": []map[string]any{{
			"resource":   "https://creator.douyin.com/janus/douyin/creator/pc/work_list",
			"rangeHints": []string{"resource_complete_7_day_window"},
		}},
		"handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "observed" || !result.RangeVerified || result.Snapshot == nil ||
		result.Snapshot.Posts != 0 || result.Snapshot.Views != 0 || result.Snapshot.FollowersVerified ||
		result.Snapshot.EvidenceCode != "REVIEWED_CONTENT_RESOURCE_EMPTY" {
		t.Fatalf("complete empty Douyin window was not persisted as a reliable zero snapshot: %+v", result)
	}
}

func TestCreatorMetricsPersistsZeroInteractionDouyinPost(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("抖音零互动作品")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-douyin-zero-post-login",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 36, "metrics-douyin-zero-post", "alpha-nexus/metrics-douyin-zero-post"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 36, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_CONTENT_RESOURCE",
		"metrics": map[string]any{"posts": 1, "views": 0, "likes": 0, "comments": 0, "shares": 0, "favorites": 0},
		"posts": []map[string]any{{
			"externalPostId": "zero-post", "title": "刚发布的作品", "publishedAt": "2026-08-01T01:00:00Z",
			"views": 0, "likes": 0, "comments": 0, "shares": 0, "favorites": 0,
		}},
		"handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "observed" || !result.RangeVerified || result.Snapshot == nil || result.Snapshot.Posts != 1 || result.PostCount != 1 {
		t.Fatalf("zero-interaction Douyin post was dropped: %+v", result)
	}
}

func TestCreatorMetricsRejectsObservedPayloadWithoutVerifiedSevenDayRange(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("未验证范围浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-login",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 32, "metrics-space", "alpha-nexus/metrics"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 32, payload: map[string]any{
		"state": "observed", "rangeVerified": false, "evidenceCode": "REVIEWED_ANALYTICS_DOM",
		"metrics": map[string]any{"likes": 3}, "posts": []map[string]any{{"externalPostId": "unverified", "title": "未验证作品", "views": 3}}, "handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "unknown" || result.ErrorCode != "METRICS_RANGE_UNVERIFIED" || result.Snapshot != nil || result.PostCount != 0 {
		t.Fatalf("unverified range must not persist: %+v", result)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil || len(overview.Collections) != 1 || overview.Collections[0].State != "unknown" || overview.Collections[0].ErrorCode != "METRICS_RANGE_UNVERIFIED" || overview.Collections[0].PostCount != 0 {
		t.Fatalf("unknown collection state was not retained: overview=%+v err=%v", overview, err)
	}
}

func TestCreatorMetricsAcceptsVerifiedZeroValueWindow(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("小红书沉默账号")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "极客小腾", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-zero-window",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 34, "metrics-space", "alpha-nexus/metrics"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 34, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_DOM",
		"metrics": map[string]any{"views": 0}, "matchedLabels": []string{"观看数"},
		"matchedMarkers": []string{"数据看板"}, "handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "observed" || result.Snapshot == nil || result.Snapshot.Views != 0 {
		t.Fatalf("a verified window of zeros is data, got %+v", result)
	}
}

func TestCreatorMetricsPersistsPageEvidenceAndKeepsMissingScopedEngagementNull(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("证据留存浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "极客小腾", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-evidence",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 35, "metrics-space", "alpha-nexus/metrics"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 35, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_DOM",
		"metricScope": "page", "metrics": map[string]any{"views": 0}, "matchedLabels": []string{"观看数"},
		"metricEvidence": map[string]any{
			"views": map[string]any{"label": "观看数", "value_text": "0", "context": "观看数 0"},
		},
		"matchedMarkers": []string{"数据看板"}, "handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "observed" || result.Snapshot == nil {
		t.Fatalf("evidence-backed page metric should persist: %+v", result)
	}
	if result.ScopedEngagement != nil {
		t.Fatalf("a page without 新增互动 must not invent scoped engagement: %v", *result.ScopedEngagement)
	}
	var rawJSON string
	if err := database.QueryRow(`SELECT raw_json FROM creator_metric_snapshots WHERE id=?`, result.Snapshot.ID).Scan(&rawJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawJSON, `"metric_evidence"`) || !strings.Contains(rawJSON, `"value_text":"0"`) {
		t.Fatalf("snapshot must keep the displayed page value: %s", rawJSON)
	}
	if strings.Contains(rawJSON, `"scoped_engagement"`) {
		t.Fatalf("missing scoped engagement must be omitted, not written as 0: %s", rawJSON)
	}
}

func TestCreatorMetricsRejectsUnboundPageWideAnalyticsDOMMetrics(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("快手误匹配浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-unbound-dom",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 33, "metrics-space", "alpha-nexus/metrics"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 33, payload: map[string]any{
		"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_DOM",
		"metrics": map[string]any{"followers": 124000, "views": 1000, "likes": 12}, "matchedLabels": []string{"粉丝", "播放量", "点赞量"},
		"matchedMarkers": []string{"数据中心"}, "handoffDone": true,
	}}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "unknown" || result.ErrorCode != "METRICS_DOM_UNBOUND" || result.Snapshot != nil {
		t.Fatalf("page-wide DOM metric must not persist: %+v", result)
	}
	var snapshotCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM creator_metric_snapshots WHERE account_id=?`, account.ID).Scan(&snapshotCount); err != nil {
		t.Fatal(err)
	}
	if snapshotCount != 0 {
		t.Fatalf("unbound DOM metric invented %d snapshots", snapshotCount)
	}
}

func TestCreatorMetricsPersistsVerifiedSevenDaySnapshotForEachNonDouyinPlatform(t *testing.T) {
	fixtures := []struct {
		platform  string
		name      string
		spaceID   int64
		views     int64
		likes     int64
		comments  int64
		shares    int64
		favorites int64
		posts     int64
		host      string
	}{
		{platform: "wechat_video", name: "视频号", spaceID: 71, views: 1240, likes: 82, comments: 9, shares: 4, posts: 3, host: "channels.weixin.qq.com"},
		{platform: "kuaishou", name: "快手", spaceID: 72, views: 24000, likes: 320, comments: 21, shares: 11, posts: 6, host: "cp.kuaishou.com"},
		{platform: "xhs", name: "小红书", spaceID: 73, views: 980, likes: 75, comments: 12, favorites: 44, posts: 5, host: "creator.xiaohongshu.com"},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.platform, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity(fixture.name + "指标浏览器")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, fixture.platform, fixture.name, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
				AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
				ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
				ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-" + fixture.platform,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, fixture.spaceID, "metrics-"+fixture.platform, "alpha-nexus/metrics-"+fixture.platform); err != nil {
				t.Fatal(err)
			}

			expectedEvidence := "REVIEWED_ANALYTICS_DOM"
			payload := map[string]any{
				"state": "observed", "rangeVerified": true, "evidenceCode": expectedEvidence,
				"pageOrigin": "https://" + fixture.host + "/analytics?token=redacted",
				"metrics": map[string]any{
					"views": fixture.views, "likes": fixture.likes, "comments": fixture.comments,
					"shares": fixture.shares, "favorites": fixture.favorites, "posts": fixture.posts,
				},
				"matchedLabels": []string{"播放量"}, "matchedMarkers": []string{"数据中心"}, "handoffDone": true,
			}
			if fixture.platform == "kuaishou" {
				expectedEvidence = "REVIEWED_ANALYTICS_RESOURCE"
				payload["evidenceCode"] = expectedEvidence
				payload["resourceDiagnostics"] = []map[string]any{{
					"resource":   "https://cp.kuaishou.com/rest/cp/creator/analysis/pc/home/author/overview",
					"rangeHints": []string{"resource_explicit_7_day_bucket"},
					"numericCandidates": map[string]any{
						"reviewed.seven.view_count": fixture.views,
						"reviewed.seven.like_count": fixture.likes,
					},
				}}
			}
			runner := &creatorMetricsRunner{spaceID: 33, payload: payload}
			result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.State != "observed" || !result.RangeVerified || result.Snapshot == nil {
				t.Fatalf("verified fixture was not persisted: %+v", result)
			}
			if result.Snapshot.PlatformCode != fixture.platform || result.Snapshot.AccountID != account.ID ||
				result.Snapshot.Views != fixture.views || result.Snapshot.Likes != fixture.likes ||
				result.Snapshot.Comments != fixture.comments || result.Snapshot.Shares != fixture.shares ||
				result.Snapshot.Favorites != fixture.favorites || result.Snapshot.Posts != fixture.posts {
				t.Fatalf("snapshot crossed account/platform boundary: %+v", result.Snapshot)
			}

			var persistedPlatform, persistedEvidence string
			var persistedAccountID, persistedViews, persistedPosts int64
			var rangeVerified bool
			if err := database.QueryRow(`SELECT account_id,platform_code,range_verified,views,posts,evidence_code
				FROM creator_metric_snapshots WHERE id=?`, result.Snapshot.ID).Scan(
				&persistedAccountID, &persistedPlatform, &rangeVerified, &persistedViews, &persistedPosts, &persistedEvidence,
			); err != nil {
				t.Fatal(err)
			}
			if persistedAccountID != account.ID || persistedPlatform != fixture.platform || !rangeVerified ||
				persistedViews != fixture.views || persistedPosts != fixture.posts || persistedEvidence != expectedEvidence {
				t.Fatalf("persisted row account=%d platform=%q range=%v views=%d posts=%d evidence=%q",
					persistedAccountID, persistedPlatform, rangeVerified, persistedViews, persistedPosts, persistedEvidence)
			}
			if !strings.Contains(runner.program, fmt.Sprintf("await takeOverTaskSpace(%d)", fixture.spaceID)) ||
				!strings.Contains(runner.program, `"code":"`+fixture.platform+`"`) {
				t.Fatalf("collector was not bound to account space/platform: %s", runner.program)
			}
		})
	}
}

func TestCreatorMetricsUnknownDoesNotCreateSnapshotForEachNonDouyinPlatform(t *testing.T) {
	for index, platform := range []string{"wechat_video", "kuaishou", "xhs"} {
		platform := platform
		t.Run(platform, func(t *testing.T) {
			database := openServiceDB(t)
			identity, _ := database.CreateEgoIdentity("未知指标-" + platform)
			account, _ := database.CreateAccountForIdentity(identity.ID, platform, platform, "")
			if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
				AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
				ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
				ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-unknown-" + platform,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, int64(80+index), "metrics-unknown-"+platform, "alpha-nexus/metrics-unknown-"+platform); err != nil {
				t.Fatal(err)
			}
			runner := &creatorMetricsRunner{spaceID: 33, payload: map[string]any{
				"state": "unknown", "rangeVerified": false, "errorCode": "METRICS_DOM_UNRECOGNIZED", "handoffDone": true,
			}}
			result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
			if err != nil || result.State != "unknown" || result.Snapshot != nil || result.PostCount != 0 {
				t.Fatalf("unknown result=%+v err=%v", result, err)
			}
			var snapshotCount int
			if err := database.QueryRow(`SELECT COUNT(*) FROM creator_metric_snapshots WHERE account_id=?`, account.ID).Scan(&snapshotCount); err != nil {
				t.Fatal(err)
			}
			if snapshotCount != 0 {
				t.Fatalf("unknown collection invented %d snapshots", snapshotCount)
			}
		})
	}
}

// Without a binding row the collection attempts exactly one re-bind. A failed
// rebuild is reported as such, and the collection program itself never runs.
func TestCreatorMetricsRequiresBoundIdentitySpaceBeforeRuntime(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("无 Space 指标浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-missing-space",
	}); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{
		spaceID: 33, bindErr: fmt.Errorf("EGO_PROFILE_NOT_FOUND"),
		payload: map[string]any{"state": "observed", "rangeVerified": true, "handoffDone": true},
	}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err == nil || err.Error() != "EGO_PROFILE_NOT_FOUND" || result != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if runner.program != "" {
		t.Fatalf("collection ran without a bound identity Space: %q", runner.program)
	}
	for _, program := range runner.programs {
		if !strings.Contains(program, "const profileResult = await listProfiles()") {
			t.Fatalf("only the re-bind may reach the runtime: %v", runner.programs)
		}
	}
	if space, spaceErr := database.GetEgoSpaceByIdentity(identity.ID); spaceErr == nil && space != nil {
		t.Fatalf("a failed re-bind must not write a binding: %+v", space)
	}
}

// A rebuild that fails for a reason the caller cannot act on is reported as a
// re-bind failure, never as "no binding" and never as a successful collection.
func TestCreatorMetricsReportsRebindFailureWhenSpaceCannotBeRebuilt(t *testing.T) {
	database := openServiceDB(t)
	identity, _ := database.CreateEgoIdentity("空间重建失败的指标浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-rebind-failure",
	}); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 33, bindErr: fmt.Errorf("EGO_RUNTIME_EXECUTION_FAILED")}
	if _, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID); err == nil || err.Error() != "EGO_SPACE_REBIND_FAILED" {
		t.Fatalf("err=%v, want EGO_SPACE_REBIND_FAILED", err)
	}
}

// A binding whose Space vanished from the runtime is re-bound, and the
// collection of the same account then runs against the new Space.
func TestCreatorMetricsRebindsVanishedSpaceAndCollects(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("空间已失效的指标浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-vanished-space",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 50, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 50, payload: map[string]any{
		"state": "unknown", "rangeVerified": false, "errorCode": "METRICS_DOM_UNRECOGNIZED", "handoffDone": true,
	}}
	// The runtime no longer knows Space 50; the first re-bind call creates 77.
	runner.rebindID = 77
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err != nil || result == nil || result.State != "unknown" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil || space.SpaceID != 77 {
		t.Fatalf("binding was not re-bound to the rebuilt Space: %+v err=%v", space, err)
	}
	if !strings.Contains(runner.program, "await takeOverTaskSpace(77)") {
		t.Fatalf("collection did not target the rebuilt Space: %q", runner.program)
	}
}

// A runtime failure must still identify the account and the stage it failed
// at; before this, callers only saw a generic code and could not tell a page
// problem from a launcher problem.
func TestCreatorMetricsKeepsAccountAndStageWhenRuntimeFails(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("运行失败的指标浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-runtime-failure",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 61, "metrics-failure", "alpha-nexus/metrics-failure"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 61, programErr: fmt.Errorf("EGO_RUNTIME_EXECUTION_FAILED")}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err == nil || err.Error() != "EGO_RUNTIME_EXECUTION_FAILED" {
		t.Fatalf("err=%v, want the runtime failure to be returned", err)
	}
	if result == nil {
		t.Fatal("a failed collection must still report the account it was collecting")
	}
	if result.AccountID != account.ID || result.PlatformCode != "xhs" || result.Stage != "RUNTIME_EXECUTION" {
		t.Fatalf("result=%+v, want the account and failure stage", result)
	}
	if result.RunError != "EGO_RUNTIME_EXECUTION_FAILED" {
		t.Fatalf("run_error=%q, want the stable runtime code", result.RunError)
	}
}

// Raw captured output is never safe to echo back: keep the account and stage,
// but withhold the message itself.
func TestCreatorMetricsWithholdsUnsafeRuntimeOutput(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("输出不可回传的指标浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "metrics-unsafe-output",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 62, "metrics-unsafe", "alpha-nexus/metrics-unsafe"); err != nil {
		t.Fatal(err)
	}
	runner := &creatorMetricsRunner{spaceID: 62, programErr: fmt.Errorf("stderr: 私密页面内容 /Users/albert/secret")}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(context.Background(), account.ID)
	if err == nil || result == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.RunError != "" {
		t.Fatalf("run_error must withhold raw output, got %q", result.RunError)
	}
	if result.AccountID != account.ID || result.Stage != "RUNTIME_EXECUTION" {
		t.Fatalf("result=%+v, want the account and failure stage", result)
	}
}
