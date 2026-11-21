package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/skilllib"
)

func TestSocialSkillCatalogExposesOnlyReviewedWorkflows(t *testing.T) {
	for _, name := range []string{"skill.list", "skill.get", "skill.run"} {
		if _, ok := Default.Get(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}

	result, err := Default.Call(context.Background(), &Runtime{}, "skill.list", nil)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("skill.list result=%+v err=%v", result, err)
	}
	if result.Data["count"] != 8 {
		t.Fatalf("unexpected catalog: %+v", result.Data)
	}

	result, err = Default.Call(context.Background(), &Runtime{}, "skill.get", map[string]interface{}{"skill_id": "social.account-check"})
	if err != nil || result == nil || !result.OK || result.Data["skill_id"] != "social.account-check" {
		t.Fatalf("skill.get result=%+v err=%v", result, err)
	}
}

func TestSocialPublishSkillsExposeOnlyPublicApprovalGatedTools(t *testing.T) {
	oneClick, ok := socialSkillByID("one-click-publish")
	if !ok || oneClick.RuntimeRef != "" || !oneClick.RequiresLocalUI || oneClick.FinalPublishAction ||
		strings.Join(oneClick.AllowedTools, ",") != "social.publish-capabilities,publish.plan.freeze,publish.plan.get,publish.plan.list,publish.plan.prepare" {
		t.Fatalf("one-click skill=%+v exists=%v", oneClick, ok)
	}
	for _, forbidden := range []string{"publish.plan.submit", "social.publish-submit-approved"} {
		for _, allowed := range oneClick.AllowedTools {
			if allowed == forbidden {
				t.Fatalf("one-click skill must not include final publish tool %q: %+v", forbidden, oneClick)
			}
		}
	}
	prepare, ok := socialSkillByID("social.publish-prepare")
	if !ok || strings.Join(prepare.AllowedTools, ",") != "social.publish-prepare" || prepare.FinalPublishAction {
		t.Fatalf("prepare skill=%+v exists=%v", prepare, ok)
	}
	for _, id := range []string{"video-publisher", "multipost"} {
		skill, exists := socialSkillByID(id)
		if !exists || skill.RuntimeRef != "social.publish-prepare" || !skill.RequiresLocalUI || skill.FinalPublishAction ||
			strings.Join(skill.AllowedTools, ",") != "social.publish-prepare" {
			t.Fatalf("native prepare skill=%+v exists=%v", skill, exists)
		}
	}
	submit, ok := socialSkillByID("social.publish-submit-approved")
	if !ok || strings.Join(submit.AllowedTools, ",") != "social.publish-submit-approved" || !submit.RequiresLocalUI || !submit.FinalPublishAction {
		t.Fatalf("submit skill=%+v exists=%v", submit, ok)
	}
}

func TestSocialMaterialsSkillRuntimeRefIsCallable(t *testing.T) {
	definition, ok := Default.Get("social.materials-list")
	if !ok {
		t.Fatal("social.materials-list is not registered")
	}
	if definition.SideEffect != "read" || !definition.AgentVisible || definition.AgentRisk != AgentRiskL0 {
		t.Fatalf("unsafe materials skill definition: %+v", definition)
	}
}

func TestStructuredSocialToolsAreAgentVisibleAndEgoBacked(t *testing.T) {
	for _, name := range []string{"social.account-check", "social.materials-list", "social.metrics.collect", "social.metrics.collect-batch", "social.metrics.overview", "social.publish-capabilities"} {
		definition, ok := Default.Get(name)
		if !ok || !definition.AgentVisible {
			t.Fatalf("definition %s=%+v exists=%v", name, definition, ok)
		}
	}
	for _, name := range []string{"social.publish-prepare", "social.publish-submit-approved"} {
		definition, ok := Default.Get(name)
		if !ok || definition.AgentVisible {
			t.Fatalf("non-Agent publish wrapper %s=%+v exists=%v", name, definition, ok)
		}
	}

	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "主账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 51, "task-51", "identity-1/account-1/xhs"); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()

	result, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir, EgoRunner: egoToolRunner{}}, "social.account-check", map[string]interface{}{"account_id": account.ID})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	observation, ok := result.Data["observation"].(*egolite.AccountObservation)
	if !ok || observation.Verified || observation.EvidenceCode != "EGO_SNAPSHOT_UNVERIFIED" {
		t.Fatalf("observation=%+v", result.Data["observation"])
	}
}

func TestSocialAccountCheckPreservesSafeSpaceOwnershipError(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "主账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 52, "task-52", "identity-1/account-1/xhs"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Call(context.Background(), &Runtime{
		DataDir:   dataDir,
		EgoRunner: socialMetricsErrorRunner{err: errors.New("EGO_SPACE_USER_CONTROLLED")},
	}, "social.account-check", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("safe account-check error must be returned in the Registry result: %v", err)
	}
	if result == nil || result.OK || result.Error != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("result=%+v, want explicit safe ownership error", result)
	}
}

func TestSocialAccountCheckGeneralizesUnknownErrorWithoutLeakingStderr(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "主账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 53, "task-53", "identity-1/account-1/xhs"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Call(context.Background(), &Runtime{
		DataDir:   dataDir,
		EgoRunner: socialMetricsErrorRunner{err: errors.New("stderr: private browser output")},
	}, "social.account-check", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("unknown account-check error must not escape Registry: %v", err)
	}
	if result == nil || result.OK || result.Error != "EGO_ACCOUNT_CHECK_FAILED" {
		t.Fatalf("result=%+v, want generic account-check error", result)
	}
	if strings.Contains(result.Error, "private browser output") {
		t.Fatalf("result leaked stderr: %+v", result)
	}
}

type socialMetricsRunner struct{}

func (socialMetricsRunner) Run(_ context.Context, program string, result any) error {
	payload := map[string]any{"done": true}
	if strings.Contains(program, "const metricResult =") {
		payload = map[string]any{
			"state": "observed", "rangeVerified": true, "evidenceCode": "REVIEWED_ANALYTICS_DOM",
			"pageOrigin": "https://creator.douyin.com/creator-micro/data-center",
			"metrics":    map[string]any{"views": 900, "likes": 60, "comments": 8, "shares": 5, "favorites": 7, "posts": 2},
			"posts":      []any{}, "matchedLabels": []string{"播放量"}, "handoffDone": true,
		}
		if strings.Contains(program, `"code":"kuaishou"`) {
			payload["evidenceCode"] = "REVIEWED_ANALYTICS_RESOURCE"
			payload["pageOrigin"] = "https://cp.kuaishou.com/profile"
			payload["resourceDiagnostics"] = []map[string]any{{
				"resource":   "https://cp.kuaishou.com/rest/cp/creator/analysis/pc/home/author/overview",
				"rangeHints": []string{"resource_explicit_7_day_bucket"},
				"numericCandidates": map[string]any{
					"reviewed.seven.view_count": 900,
					"reviewed.seven.like_count": 60,
				},
			}}
		}
	}
	raw, _ := json.Marshal(payload)
	return json.Unmarshal(raw, result)
}

type socialMetricsErrorRunner struct {
	err error
}

func (runner socialMetricsErrorRunner) Run(_ context.Context, _ string, _ any) error {
	return runner.err
}

func TestSocialMetricsBatchPreservesSafeRuntimeFailureAndFailsToolCall(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("失效采集浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "tool-metrics-runtime-failure",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 75, "metrics-runtime-failure", "alpha-nexus/metrics-runtime-failure"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	runtime := &Runtime{DataDir: dataDir, EgoRunner: socialMetricsErrorRunner{err: errors.New("EGO_SPACE_BINDING_MISSING")}}
	result, err := Default.Call(context.Background(), runtime, "social.metrics.collect-batch", map[string]interface{}{"account_ids": []interface{}{float64(account.ID)}})
	if err != nil {
		t.Fatalf("structured batch failure must remain readable: %v", err)
	}
	if result == nil || result.OK || result.Error != "EGO_METRICS_BATCH_FAILED" {
		t.Fatalf("batch failure must fail the tool call: %+v", result)
	}
	rows, ok := result.Data["accounts"].([]map[string]interface{})
	if !ok || len(rows) != 1 || rows[0]["error_code"] != "EGO_SPACE_BINDING_MISSING" {
		t.Fatalf("safe runtime error must be preserved: %T %+v", result.Data["accounts"], result.Data["accounts"])
	}
}

func TestSocialMetricsRegistryCollectsReviewedSnapshotAndReadsOverview(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("采集浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "tool-metrics-login",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 71, "metrics-tool", "alpha-nexus/metrics-tool"); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	runtime := &Runtime{DataDir: dataDir, EgoRunner: socialMetricsRunner{}, MaxConcurrentSpaces: 3}
	collected, err := Default.Call(context.Background(), runtime, "social.metrics.collect-batch", map[string]interface{}{"account_ids": []interface{}{float64(account.ID)}})
	if err != nil || collected == nil || !collected.OK {
		t.Fatalf("collected=%+v err=%v", collected, err)
	}
	rows, ok := collected.Data["accounts"].([]map[string]interface{})
	if !ok || len(rows) != 1 || rows[0]["snapshot"] == nil {
		t.Fatalf("batch rows must carry the persisted snapshot like single collect: %T %+v", collected.Data["accounts"], collected.Data["accounts"])
	}
	overview, err := Default.Call(context.Background(), runtime, "social.metrics.overview", nil)
	if err != nil || overview == nil || !overview.OK {
		t.Fatalf("overview=%+v err=%v", overview, err)
	}
	metrics, ok := overview.Data["overview"].(*db.CreatorMetricOverview)
	if !ok || len(metrics.Latest) != 1 || metrics.Latest[0].Views != 900 {
		t.Fatalf("metrics=%T %+v", overview.Data["overview"], overview.Data["overview"])
	}
}

func TestSocialMetricsRegistryPersistsOneExplicitAllPlatformBatch(t *testing.T) {
	definition, ok := Default.Get("social.metrics.collect-batch")
	if !ok || !strings.Contains(definition.Description, "100") || !strings.Contains(definition.AgentImpact, "all explicitly selected") {
		t.Fatalf("all-platform batch contract is stale: %+v", definition)
	}
	tooMany := make([]interface{}, 101)
	for index := range tooMany {
		tooMany[index] = float64(index + 1)
	}
	if result, err := Default.Call(context.Background(), nil, "social.metrics.collect-batch", map[string]interface{}{"account_ids": tooMany}); err == nil || result == nil || result.Error != "SKILL_INPUT_INVALID" {
		t.Fatalf("101-account batch must fail closed: result=%+v err=%v", result, err)
	}

	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("全平台采集浏览器")
	if err != nil {
		t.Fatal(err)
	}
	platforms := []string{"douyin", "wechat_video", "kuaishou", "xhs", "tiktok", "youtube", "meta", "wechat_mp", "csdn", "bilibili"}
	accountIDs := make([]interface{}, 0, len(platforms))
	for _, platform := range platforms {
		account, err := database.CreateAccountForIdentity(identity.ID, platform, platform, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
			AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
			ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
			ObservedAt: "2026-08-01T00:00:00Z", TraceID: "tool-all-platform-" + platform,
		}); err != nil {
			t.Fatal(err)
		}
		accountIDs = append(accountIDs, float64(account.ID))
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 74, "metrics-all-platform", "alpha-nexus/metrics-all-platform"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	runtime := &Runtime{DataDir: dataDir, EgoRunner: socialMetricsRunner{}, MaxConcurrentSpaces: 3}
	collected, err := Default.Call(context.Background(), runtime, "social.metrics.collect-batch", map[string]interface{}{"account_ids": accountIDs})
	if err != nil || collected == nil || !collected.OK || collected.Data["all_observed"] != false {
		t.Fatalf("collected=%+v err=%v", collected, err)
	}
	rows, ok := collected.Data["accounts"].([]map[string]interface{})
	if !ok || len(rows) != len(platforms) {
		t.Fatalf("accounts=%T %+v", collected.Data["accounts"], collected.Data["accounts"])
	}
	seenRows := map[string]bool{}
	for _, row := range rows {
		platform, _ := row["platform_code"].(string)
		if platform == "bilibili" {
			if row["state"] != "manual_takeover" || row["error_code"] != "EGO_METRICS_MANUAL_HANDOFF" || row["range_verified"] == true {
				t.Fatalf("manual platform row=%+v", row)
			}
			seenRows[platform] = true
			continue
		}
		if row["state"] != "observed" || row["range_verified"] != true {
			t.Fatalf("row=%+v", row)
		}
		seenRows[platform] = true
	}
	for _, platform := range platforms {
		if !seenRows[platform] {
			t.Fatalf("missing collected platform %s: %+v", platform, rows)
		}
	}

	overviewResult, err := Default.Call(context.Background(), runtime, "social.metrics.overview", nil)
	if err != nil || overviewResult == nil || !overviewResult.OK {
		t.Fatalf("overview=%+v err=%v", overviewResult, err)
	}
	overview, ok := overviewResult.Data["overview"].(*db.CreatorMetricOverview)
	if !ok || len(overview.Latest) != len(platforms)-1 || len(overview.Collections) != len(platforms) {
		t.Fatalf("overview=%T %+v", overviewResult.Data["overview"], overviewResult.Data["overview"])
	}
}

func TestSocialMetricsPreferencesAndHistoryStayBehindRegistry(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{DataDir: dataDir}

	readResult, err := Default.Call(context.Background(), runtime, "social.metrics.preferences", nil)
	if err != nil || readResult == nil || !readResult.OK {
		t.Fatalf("read preferences=%+v err=%v", readResult, err)
	}
	preference, ok := readResult.Data["preferences"].(*db.CreatorMetricPreference)
	if !ok || preference.HistoryDays != 30 || preference.RetentionDays != 90 {
		t.Fatalf("default preferences=%T %+v", readResult.Data["preferences"], readResult.Data["preferences"])
	}

	updated, err := Default.Call(context.Background(), runtime, "social.metrics.preferences.update", map[string]interface{}{
		"history_days": float64(90), "retention_days": float64(180),
	})
	if err != nil || updated == nil || !updated.OK {
		t.Fatalf("update preferences=%+v err=%v", updated, err)
	}
	overview, err := Default.Call(context.Background(), runtime, "social.metrics.overview", map[string]interface{}{"history_days": float64(90)})
	if err != nil || overview == nil || !overview.OK {
		t.Fatalf("90-day overview=%+v err=%v", overview, err)
	}
	metrics, ok := overview.Data["overview"].(*db.CreatorMetricOverview)
	if !ok || metrics.WindowDays != 90 {
		t.Fatalf("overview=%T %+v", overview.Data["overview"], overview.Data["overview"])
	}
}

func TestSocialPublishCapabilitiesExposeAllContractsWithoutClaimingSubmit(t *testing.T) {
	result, err := Default.Call(context.Background(), &Runtime{}, "social.publish-capabilities", nil)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	contracts, ok := result.Data["contracts"].([]map[string]interface{})
	if !ok || len(contracts) != 41 {
		t.Fatalf("contracts=%T %+v", result.Data["contracts"], result.Data["contracts"])
	}
	expected := map[string]egopublish.Capability{}
	firstRelease := map[string]bool{}
	for _, manifest := range egopublish.EgoLiteManifests() {
		for _, capability := range manifest.Capabilities {
			expected[manifest.PlatformCode+"/"+capability.ContentType] = capability
			firstRelease[manifest.PlatformCode+"/"+capability.ContentType] = manifest.FirstRelease
		}
	}
	seen := map[string]map[string]interface{}{}
	for _, contract := range contracts {
		code, _ := contract["platform_code"].(string)
		contentType, _ := contract["content_type"].(string)
		key := code + "/" + contentType
		capability, exists := expected[key]
		if code == "" || contentType == "" || !exists ||
			contract["prepare_mode"] != capability.PrepareMode ||
			contract["prepare_adapter_supported"] != capability.PrepareAdapterSupported ||
			contract["submit_adapter_supported"] != capability.SubmitAdapterSupported ||
			contract["verify_adapter_supported"] != capability.VerifyAdapterSupported ||
			contract["first_release"] != firstRelease[key] {
			t.Fatalf("invalid contract=%+v", contract)
		}
		if contract["submit_verified"] == true || contract["verify_verified"] == true {
			t.Fatalf("unverified submit or verification promoted=%+v", contract)
		}
		seen[key] = contract
	}
	for _, key := range []string{"xhs/graph_text", "xhs/video", "wechat_mp/article", "youtube/video", "acfun/article", "toutiao/video", "x/graph_text", "zhihu/video"} {
		if _, ok := seen[key]; !ok {
			t.Fatalf("missing contract %s", key)
		}
	}
	tiktokPhoto, ok := seen["tiktok/graph_text"]
	if !ok || tiktokPhoto["prepare_mode"] != egopublish.PrepareManualHandoff ||
		tiktokPhoto["prepare_adapter_supported"] != false ||
		tiktokPhoto["submit_adapter_supported"] != false || tiktokPhoto["verify_adapter_supported"] != false ||
		tiktokPhoto["submit_verified"] != false || tiktokPhoto["verify_verified"] != false ||
		tiktokPhoto["first_release"] != true {
		t.Fatalf("TikTok Photo Post must remain a first-release manual handoff: %+v", tiktokPhoto)
	}
}

func TestSocialWorkflowCatalogSeparatesVerifiedAutomationFromManualHandoff(t *testing.T) {
	result, err := Default.Call(context.Background(), &Runtime{}, "social.workflow.catalog", nil)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	workflows, ok := result.Data["platforms"].([]map[string]interface{})
	if !ok || len(workflows) < 10 {
		t.Fatalf("workflows=%T %+v", result.Data["platforms"], result.Data["platforms"])
	}
	byCode := map[string]map[string]interface{}{}
	for _, workflow := range workflows {
		byCode[workflow["platform_code"].(string)] = workflow
		if workflow["publish_workflow"] == "published" {
			t.Fatalf("workflow must never claim published: %+v", workflow)
		}
	}
	if byCode["douyin"]["data_workflow"] != "automated_snapshot" || byCode["bilibili"]["data_workflow"] != "manual_handoff" {
		t.Fatalf("unexpected data workflows: douyin=%+v bilibili=%+v", byCode["douyin"], byCode["bilibili"])
	}
}

func TestSocialSkillCatalogUsesUnifiedLocalLibraryWhenAvailable(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	library, err := skilllib.New(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Create(context.Background(), skilllib.CreateInput{
		ID: "local-helper", Name: "本地助手", Description: "处理本地提供的文本。",
		Instructions: "只处理用户明确提供的文本。",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Default.Call(context.Background(), &Runtime{Skills: library}, "skill.list", nil)
	if err != nil || result == nil || !result.OK || result.Data["count"] != 13 {
		t.Fatalf("unified catalog result=%+v err=%v", result, err)
	}
	items, ok := result.Data["skills"].([]skilllib.Skill)
	if !ok {
		t.Fatalf("skills=%T", result.Data["skills"])
	}
	refs := map[string]string{}
	for _, item := range items {
		refs[item.ID] = item.RuntimeRef
	}
	if _, exists := refs["social-content-workflow"]; !exists || refs["one-click-publish"] != "" || refs["social-metrics-collect"] != "social.metrics.collect-batch" ||
		refs["social-publish-prepare"] != "" || refs["social-publish-submit-approved"] != "" || refs["video-publisher"] != "" || refs["multipost"] != "" {
		t.Fatalf("built-in Registry refs=%+v", refs)
	}
}

func TestSocialAccountCheckSkillRunsOnlyExplicitAccountChecks(t *testing.T) {
	registry := NewRegistry()
	called := []int{}
	registry.Register(Def{Name: "social.account-check", SideEffect: "browser", Handler: func(_ context.Context, _ *Runtime, args map[string]interface{}) (*Result, error) {
		called = append(called, argInt(args, "account_id", 0))
		return okResult("checked", map[string]interface{}{"account_id": argInt(args, "account_id", 0)}), nil
	}})

	result, err := runSocialSkill(context.Background(), registry, &Runtime{}, map[string]interface{}{
		"skill_id": "social.account-check", "account_ids": []interface{}{float64(2), float64(3)},
	})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("skill result=%+v err=%v", result, err)
	}
	if len(called) != 2 || called[0] != 2 || called[1] != 3 {
		t.Fatalf("account checks=%v", called)
	}
}

func TestSocialPublishSkillsCannotRunThroughGenericSkillWrapper(t *testing.T) {
	registry := NewRegistry()
	called := false
	registry.Register(Def{Name: "social.publish-prepare", SideEffect: "external_publish", Handler: func(_ context.Context, _ *Runtime, _ map[string]interface{}) (*Result, error) {
		called = true
		return okResult("prepared", nil), nil
	}})
	for _, skillID := range []string{"social.publish-prepare", "video-publisher", "multipost", "social.publish-submit-approved"} {
		result, err := runSocialSkill(context.Background(), registry, &Runtime{TrustedLocalUI: true}, map[string]interface{}{"skill_id": skillID})
		if err == nil || result == nil || result.OK || result.Error != "SKILL_ORCHESTRATION_REQUIRED" || called {
			t.Fatalf("skill %s result=%+v err=%v called=%v", skillID, result, err, called)
		}
	}
}

func TestOneClickPublishSkillCannotRunAsGenericMacro(t *testing.T) {
	registry := NewRegistry()
	calls := 0
	registry.Register(Def{Name: "publish.plan.submit", SideEffect: "external_publish", Handler: func(_ context.Context, _ *Runtime, _ map[string]interface{}) (*Result, error) {
		calls++
		return okResult("submitted", nil), nil
	}})

	result, err := runSocialSkill(context.Background(), registry, &Runtime{}, map[string]interface{}{"skill_id": "one-click-publish"})
	if err == nil || result == nil || result.OK || result.Error != "SKILL_ORCHESTRATION_REQUIRED" || calls != 0 {
		t.Fatalf("one-click generic run result=%+v err=%v submit_calls=%d", result, err, calls)
	}
}

func TestSocialPublishRegistryKeepsPrepareAndSubmitCallbacksSeparated(t *testing.T) {
	args := map[string]interface{}{
		"plan_id": float64(7), "snapshot_hash": "snapshot", "approval_id": "approval", "nonce": "nonce",
	}
	prepareCalls, submitCalls := 0, 0
	runtime := &Runtime{
		PreparePublishPlan: func(context.Context, int64, string, string, string) (*Result, error) {
			prepareCalls++
			return okResult("prepared", nil), nil
		},
		SubmitPublishPlan: func(context.Context, int64, string, string, string) (*Result, error) {
			submitCalls++
			return okResult("submitted", nil), nil
		},
	}
	result, err := Default.Call(context.Background(), runtime, "social.publish-prepare", args)
	if err == nil || result == nil || result.Error != "TRUSTED_LOCAL_UI_REQUIRED" || prepareCalls != 0 || submitCalls != 0 {
		t.Fatalf("untrusted prepare result=%+v err=%v prepare=%d submit=%d", result, err, prepareCalls, submitCalls)
	}
	runtime.TrustedLocalUI = true
	result, err = Default.Call(context.Background(), runtime, "social.publish-prepare", args)
	if err != nil || result == nil || !result.OK || prepareCalls != 1 || submitCalls != 0 {
		t.Fatalf("trusted prepare result=%+v err=%v prepare=%d submit=%d", result, err, prepareCalls, submitCalls)
	}
	runtime.TrustedLocalUI = false
	result, err = Default.Call(context.Background(), runtime, "social.publish-submit-approved", args)
	if err == nil || result == nil || result.Error != "TRUSTED_LOCAL_UI_REQUIRED" || prepareCalls != 1 || submitCalls != 0 {
		t.Fatalf("untrusted submit result=%+v err=%v prepare=%d submit=%d", result, err, prepareCalls, submitCalls)
	}
	runtime.TrustedLocalUI = true
	result, err = Default.Call(context.Background(), runtime, "social.publish-submit-approved", args)
	if err != nil || result == nil || !result.OK || prepareCalls != 1 || submitCalls != 1 {
		t.Fatalf("trusted submit result=%+v err=%v prepare=%d submit=%d", result, err, prepareCalls, submitCalls)
	}
}

// Single-account and batch results must agree on how "no scoped engagement"
// is expressed; an explicit zero is still a value.
func TestCreatorMetricRowsUseOneShapeForScopedEngagement(t *testing.T) {
	zero := int64(0)
	withZero := creatorMetricCollectionFields(&egolite.CreatorMetricCollectResult{
		AccountID: 4, PlatformCode: "xhs", ScopedEngagement: &zero,
	})
	if value, ok := withZero["scoped_engagement"]; !ok || value != int64(0) {
		t.Fatalf("explicit zero must be kept: %+v", withZero)
	}
	missing := creatorMetricCollectionFields(&egolite.CreatorMetricCollectResult{
		AccountID: 4, PlatformCode: "xhs",
	})
	if _, ok := missing["scoped_engagement"]; ok {
		t.Fatalf("missing scoped engagement must stay absent, not null: %+v", missing)
	}
	failed := creatorMetricFailureRow(nil, 9, "xhs", errors.New("EGO_SPACE_BINDING_MISSING"))
	if failed["state"] != "unknown" || failed["stage"] != "SPACE_BINDING" || failed["ok"] != false {
		t.Fatalf("early failure must keep the shared row shape: %+v", failed)
	}
}

// A runtime failure must still tell the caller which account and stage failed,
// and why, without echoing captured output.
func TestSocialMetricsCollectFailureKeepsAccountStageAndSafeDetail(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("采集失败浏览器")
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
		ObservedAt: "2026-08-01T00:00:00Z", TraceID: "tool-metrics-single-failure",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 76, "metrics-single-failure", "alpha-nexus/metrics-single-failure"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	runner := &socialMetricsProgramErrorRunner{err: errors.New("database is locked (5) (SQLITE_BUSY)")}
	result, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir, EgoRunner: runner}, "social.metrics.collect", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("structured collection failure must remain readable: %v", err)
	}
	if result == nil || result.OK || result.Error != "EGO_METRICS_COLLECTION_FAILED" {
		t.Fatalf("result=%+v, want a generic collection failure", result)
	}
	if result.Data["account_id"] != account.ID || result.Data["stage"] != "RUNTIME_EXECUTION" {
		t.Fatalf("failure data lost the account or stage: %+v", result.Data)
	}
	if result.Data["run_error"] != "SQLITE_BUSY" {
		t.Fatalf("run_error=%v, want SQLITE_BUSY", result.Data["run_error"])
	}
}

type socialMetricsProgramErrorRunner struct {
	db  *db.DB
	err error
}

func (runner *socialMetricsProgramErrorRunner) BindProgramRunnerDB(database *db.DB) {
	runner.db = database
}

func (runner *socialMetricsProgramErrorRunner) Run(_ context.Context, program string, result any) error {
	if strings.Contains(program, "alpha-space-probe") {
		listing := []map[string]any{}
		if runner.db != nil {
			for identityID := int64(1); identityID <= 8; identityID++ {
				if space, spaceErr := runner.db.GetEgoSpaceByIdentity(identityID); spaceErr == nil && space != nil {
					listing = append(listing, map[string]any{"id": space.SpaceID, "ownership": space.Ownership})
				}
			}
		}
		raw, _ := json.Marshal(map[string]any{"spaces": listing})
		return json.Unmarshal(raw, result)
	}
	if strings.Contains(program, "const metricResult =") {
		return runner.err
	}
	return nil
}

// Runtime codes that already describe a page, profile or output problem must
// reach the caller unchanged; only genuinely unknown errors are generalized.
func TestSocialMetricsErrorCodesKeepSafeRuntimeDetail(t *testing.T) {
	for _, code := range []string{"EGO_OUTPUT_LIMIT", "EGO_PROFILE_NOT_FOUND", "EGO_PROFILE_BINDING_UNAVAILABLE", "EGO_LOGIN_QR_PLATFORM_MISMATCH"} {
		if got := safeSocialMetricsErrorCode(errors.New(code)); got != code {
			t.Fatalf("code %s was generalized to %s", code, got)
		}
	}
	if got := safeSocialMetricsErrorCode(errors.New("stderr: private browser output")); got != "EGO_METRICS_COLLECTION_FAILED" {
		t.Fatalf("unknown error must stay generic, got %s", got)
	}
}
