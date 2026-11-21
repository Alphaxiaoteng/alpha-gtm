package platforms_test

import (
	"context"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
	_ "alpha-nexus-egolite/internal/platforms/meta"
	"alpha-nexus-egolite/internal/selectors"
)

func TestAllReturnsTargetPlatformCatalog(t *testing.T) {
	all := platforms.All()
	if len(all) != 38 {
		t.Fatalf("All() returned %d platforms, want 38", len(all))
	}
}

func TestFindExisting(t *testing.T) {
	p, err := platforms.Find("instagram")
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "instagram" {
		t.Errorf("Code = %q, want %q", p.Code, "instagram")
	}
	if !p.SupportsPublish {
		t.Error("instagram should support publish")
	}
}

func TestFindMetaLoginAliasWithoutExpandingPlatformDirectory(t *testing.T) {
	p, err := platforms.Find("meta")
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "meta" || p.Name != "Meta" ||
		p.LoginURL != "https://business.facebook.com/latest/home" ||
		p.ManagerURL != "https://business.facebook.com/latest/home" {
		t.Fatalf("meta=%+v", p)
	}
	if p.SupportsArticle {
		t.Fatal("Meta login container must expose only the graph-text/video asset capabilities")
	}
	readiness := platforms.Readiness("meta")
	if !readiness.CanCreateJob || !readiness.CanAttemptAutoSubmit || readiness.Mode != "browser_automation" {
		t.Fatalf("meta readiness=%+v", readiness)
	}
	if len(platforms.All()) != 38 {
		t.Fatalf("Meta login alias must not duplicate Facebook and Instagram in the platform directory")
	}
}

// ecommercePublishable are the commerce consoles that may create a publish job
// and open their creator surface for a manual handoff. They must never
// auto-submit, because no commerce adapter has a verified success signal.
var ecommercePublishable = []string{"alibaba_1688", "aliexpress", "xianyu"}

func TestEcommerceHandoffConsolesNeverAutoSubmit(t *testing.T) {
	for _, code := range ecommercePublishable {
		readiness := platforms.Readiness(code)
		if readiness.Mode != "manual_handoff" || !readiness.CanCreateJob || readiness.CanAttemptAutoSubmit {
			t.Fatalf("%s readiness=%+v, want manual_handoff job=true auto=false", code, readiness)
		}
	}
}

func TestEcommerceConsolesAreLoginOnly(t *testing.T) {
	for _, code := range []string{
		"douyin_shop", "taobao", "tiktok_shop", "shopee", "shopify", "amazon_seller",
		"qianchuan", "jd_shop", "pinduoduo_shop", "xhs_shop", "kuaishou_shop", "wechat_shop",
	} {
		platform, err := platforms.Find(code)
		if err != nil {
			t.Fatal(err)
		}
		if platform.LoginURL == "" || platform.ManagerURL == "" || platform.SupportsPublish ||
			platform.SupportsArticle || platform.SupportsGraphText || platform.SupportsVideo {
			t.Fatalf("%s must be a login-only console: %+v", code, platform)
		}
		readiness := platforms.Readiness(code)
		if readiness.Mode != "unavailable" || readiness.CanCreateJob || readiness.CanAttemptAutoSubmit {
			t.Fatalf("%s publish readiness=%+v", code, readiness)
		}
	}
}

func TestFindXHSExposesPrepareOnlyPublishPath(t *testing.T) {
	p, err := platforms.Find("xhs")
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "xhs" {
		t.Errorf("Code = %q, want %q", p.Code, "xhs")
	}
	if !p.SupportsPublish || !p.SupportsGraphText || !p.SupportsVideo {
		t.Fatalf("xhs must expose graph-text/video prepare capabilities: %+v", p)
	}
	readiness := platforms.Readiness("xhs")
	if !readiness.CanCreateJob || readiness.CanAttemptAutoSubmit || readiness.Mode != "browser_automation" {
		t.Fatalf("xhs publish readiness=%+v", readiness)
	}
	for _, phrase := range []string{"最终提交仍需独立审批", "submit adapter 未验证", "不代表自动发布成功"} {
		if !strings.Contains(readiness.Summary, phrase) {
			t.Fatalf("xhs readiness summary missing %q: %s", phrase, readiness.Summary)
		}
	}
}

func TestKuaishouReadinessAllowsFrozenSingleTargetSubmit(t *testing.T) {
	readiness := platforms.Readiness("kuaishou")
	if readiness.Mode != "browser_automation" || !readiness.CanCreateJob || !readiness.CanAttemptAutoSubmit {
		t.Fatalf("kuaishou readiness=%+v", readiness)
	}
}

func TestReviewedContentTypesMatchCurrentWorkers(t *testing.T) {
	expected := map[string]struct {
		article bool
		graph   bool
		video   bool
	}{
		"facebook": {graph: true, video: true},
		"xhs":      {graph: true, video: true},
		"douyin":   {graph: true, video: true},
		"kuaishou": {graph: true, video: true},
		"csdn":     {article: true},
	}
	for code, want := range expected {
		platform, err := platforms.Find(code)
		if err != nil {
			t.Fatal(err)
		}
		if platform.SupportsArticle != want.article || platform.SupportsGraphText != want.graph || platform.SupportsVideo != want.video {
			t.Errorf("%s capabilities=(article=%v graph=%v video=%v), want %+v", code, platform.SupportsArticle, platform.SupportsGraphText, platform.SupportsVideo, want)
		}
	}
}

func TestFindMissing(t *testing.T) {
	_, err := platforms.Find("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent platform")
	}
}

func TestPublishTargetAssetTypes(t *testing.T) {
	tests := []struct {
		platform string
		asset    string
		allowed  bool
	}{
		{"facebook", "facebook_page", true},
		{"facebook", "browser_account", false},
		{"instagram", "instagram_account", true},
		{"instagram", "facebook_page", false},
		{"xhs", "browser_account", true},
	}
	for _, tt := range tests {
		if got := platforms.IsPublishTargetAssetType(tt.platform, tt.asset); got != tt.allowed {
			t.Errorf("IsPublishTargetAssetType(%q, %q)=%v, want %v", tt.platform, tt.asset, got, tt.allowed)
		}
	}
}

func TestCodes(t *testing.T) {
	codes := platforms.Codes()
	expected := []string{
		"acfun", "alibaba_1688", "aliexpress", "amazon_seller", "baijiahao", "bilibili",
		"csdn", "custom", "douyin", "douyin_shop", "facebook", "instagram", "jd_shop",
		"jianshu", "juejin", "kuaishou", "kuaishou_shop", "omtencent", "pdd",
		"pinduoduo_shop", "qianchuan", "shopee", "shopify", "sina", "taobao", "tiktok",
		"tiktok_shop", "toutiao", "wechat_mp", "wechat_shop", "wechat_video", "weishi",
		"x", "xhs", "xhs_shop", "xianyu", "youtube", "zhihu",
	}
	if len(codes) != len(expected) {
		t.Fatalf("Codes() returned %d codes, want %d: %v", len(codes), len(expected), codes)
	}
	for i, c := range expected {
		if codes[i] != c {
			t.Errorf("Codes()[%d] = %q, want %q", i, codes[i], c)
		}
	}
}

func TestGenericManualPlatformsHaveHandoffPublisher(t *testing.T) {
	p, err := platforms.Find("bilibili")
	if err != nil {
		t.Fatal(err)
	}
	if p.ManagerURL == "" || !platforms.CanPublish("bilibili") {
		t.Fatal("platform with a creator URL must have a manual handoff path")
	}
	if platforms.CanPublish("custom") {
		t.Fatal("custom platform without a creator URL must remain unavailable")
	}
}

func TestLiteCodes(t *testing.T) {
	lite := platforms.LiteCodes()
	expected := []string{"xhs", "douyin", "wechat_video", "kuaishou"}
	if len(lite) != len(expected) {
		t.Fatalf("LiteCodes() returned %d, want %d", len(lite), len(expected))
	}
}

func TestPublishCodes(t *testing.T) {
	pub := platforms.PublishCodes()
	expected := map[string]bool{
		"acfun": true, "baijiahao": true, "bilibili": true, "csdn": true,
		"douyin": true, "facebook": true, "instagram": true, "jianshu": true,
		"juejin": true, "kuaishou": true, "omtencent": true, "pdd": true,
		"sina": true, "tiktok": true, "toutiao": true, "wechat_mp": true,
		"wechat_video": true, "weishi": true, "x": true, "xhs": true,
		"youtube": true, "zhihu": true,
		"alibaba_1688": true, "aliexpress": true, "xianyu": true,
	}
	if len(pub) != len(expected) {
		t.Fatalf("PublishCodes() returned %d, want %d: %v", len(pub), len(expected), pub)
	}
	for _, c := range pub {
		if !expected[c] {
			t.Errorf("unexpected PublishCode %q", c)
		}
	}
	for code := range expected {
		if !platforms.CanPublish(code) {
			t.Errorf("CanPublish(%q) = false, want true", code)
		}
	}
	if platforms.CanPublish("nonexistent") {
		t.Error("CanPublish(nonexistent) should be false")
	}
}

func TestPlatformFieldValues(t *testing.T) {
	tests := []struct {
		code string
		name string
	}{
		{"instagram", "Instagram"},
		{"facebook", "Facebook"},
		{"xhs", "小红书"},
		{"douyin", "抖音"},
		{"wechat_video", "视频号"},
		{"kuaishou", "快手"},
		{"wechat_mp", "微信公众号"},
		{"youtube", "YouTube"},
		{"tiktok", "TikTok"},
		{"x", "X"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			p, err := platforms.Find(tt.code)
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != tt.name {
				t.Errorf("Name = %q, want %q", p.Name, tt.name)
			}
			if !p.SupportsPublish {
				t.Errorf("%s SupportsPublish = false, want true", tt.code)
			}
		})
	}
}

func TestWeChatMPResumesThroughTheOfficialEntry(t *testing.T) {
	p, err := platforms.Find("wechat_mp")
	if err != nil {
		t.Fatal(err)
	}
	const loginPage = "https://mp.weixin.qq.com/cgi-bin/loginpage?url=%2Fcgi-bin%2Fhome"
	const managerEntry = "https://mp.weixin.qq.com/"
	if p.LoginURL != loginPage || p.ManagerURL != managerEntry {
		t.Fatalf("wechat_mp URLs = login %q manager %q, want login %q manager %q", p.LoginURL, p.ManagerURL, loginPage, managerEntry)
	}
}

func TestRegisteredPublishers(t *testing.T) {
	// non-meta publishers register in platforms init; meta needs its package import.
	for _, code := range []string{"douyin", "xhs", "wechat_video", "kuaishou", "wechat_mp", "youtube", "tiktok", "x", "bilibili", "zhihu", "toutiao"} {
		p, err := platforms.GetPublisher(code)
		if err != nil {
			t.Fatalf("GetPublisher(%q): %v", code, err)
		}
		if p.Platform() != code {
			t.Errorf("Platform() = %q, want %q", p.Platform(), code)
		}
	}
}

func TestGenericManualPublisherDryRun(t *testing.T) {
	p, err := platforms.GetPublisher("bilibili")
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{Title: "manual handoff", DryRun: true})
	if err != nil || res.Status != "dry_run_ok" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestReadinessIsConservative(t *testing.T) {
	manual := platforms.Readiness("bilibili")
	if !manual.CanCreateJob || manual.CanAttemptAutoSubmit || manual.Mode != "manual_handoff" {
		t.Fatalf("manual=%+v", manual)
	}
	meta := platforms.Readiness("instagram")
	if !meta.CanCreateJob || !meta.CanAttemptAutoSubmit || meta.Mode != "browser_automation" {
		t.Fatalf("meta=%+v", meta)
	}
	for _, code := range []string{"douyin", "wechat_video", "wechat_mp", "youtube", "tiktok", "csdn"} {
		controlled := platforms.Readiness(code)
		if !controlled.CanCreateJob || !controlled.CanAttemptAutoSubmit || controlled.Mode != "browser_automation" {
			t.Fatalf("%s readiness=%+v", code, controlled)
		}
		if !strings.Contains(controlled.Summary, "单目标冻结计划") ||
			!strings.Contains(controlled.Summary, "submit-bound") ||
			!strings.Contains(controlled.Summary, "fail-closed") {
			t.Fatalf("%s readiness must state the controlled-submit boundary: %+v", code, controlled)
		}
	}
	xAPI := platforms.Readiness("x")
	if !xAPI.CanCreateJob || xAPI.CanAttemptAutoSubmit || xAPI.Mode != "api_credentials" {
		t.Fatalf("x readiness=%+v", xAPI)
	}
	missing := platforms.Readiness("custom")
	if missing.CanCreateJob || missing.Mode != "unavailable" {
		t.Fatalf("missing=%+v", missing)
	}
}

func TestPublishCapabilityMatrixCoversEveryDeclaredPath(t *testing.T) {
	for _, code := range platforms.PublishCodes() {
		t.Run(code, func(t *testing.T) {
			platform, err := platforms.Find(code)
			if err != nil {
				t.Fatal(err)
			}
			if !platform.SupportsPublish {
				t.Fatalf("%s is in PublishCodes but SupportsPublish=false", code)
			}
			readiness := platforms.Readiness(code)
			if !readiness.CanCreateJob || readiness.Mode == "unavailable" {
				t.Fatalf("readiness=%+v", readiness)
			}
			publisher, err := platforms.GetPublisher(code)
			if err != nil {
				t.Fatal(err)
			}
			result, err := publisher.Publish(context.Background(), platforms.PublishRequest{
				PlatformCode: code, ContentID: 1, AssetID: 1,
				Title: "matrix", Caption: "matrix", ContentType: "graph_text", DryRun: true,
			})
			if err != nil || result == nil || result.Status != "dry_run_ok" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	custom, err := platforms.Find("custom")
	if err != nil {
		t.Fatal(err)
	}
	if custom.SupportsPublish || platforms.Readiness("custom").CanCreateJob {
		t.Fatalf("custom must stay unavailable: %+v", custom)
	}
	if _, err := platforms.GetPublisher("custom"); err == nil {
		t.Fatal("custom unexpectedly has a publisher")
	}
}

func TestBrowserPublisherDryRun(t *testing.T) {
	p, err := platforms.GetPublisher("douyin")
	if err != nil {
		t.Fatal(err)
	}
	if p.Strategy() != platforms.StrategyBrowser {
		t.Fatalf("strategy=%s", p.Strategy())
	}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		Caption: "hello",
		DryRun:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
}

func TestCoreBrowserPublisherValidatesSettingsSnapshotOffline(t *testing.T) {
	catalog, err := selectors.Default()
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := platforms.GetPublisher("douyin")
	if err != nil {
		t.Fatal(err)
	}
	adapterVersion, ok := platforms.PublishSettingsAdapterVersion("douyin")
	if !ok {
		t.Fatal("douyin settings adapter version missing")
	}
	snapshot, err := platforms.BuildPublishSettingsSnapshot("douyin", 0, `{"location":"上海"}`, catalog.CatalogVersion, adapterVersion)
	if err != nil {
		t.Fatal(err)
	}
	result, err := publisher.Publish(context.Background(), platforms.PublishRequest{
		PlatformCode: "douyin", Caption: "offline settings validation", DryRun: true, SettingsJSON: snapshot,
	})
	if err != nil || result == nil || result.Status != "dry_run_ok" {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	wrongVersion, err := platforms.BuildPublishSettingsSnapshot("douyin", 0, `{}`, catalog.CatalogVersion, adapterVersion+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Validate(platforms.PublishRequest{Caption: "offline settings validation", SettingsJSON: wrongVersion}); err == nil {
		t.Fatal("publisher accepted a snapshot for another adapter version")
	}
}

func TestBrowserPublisherRequiresCaption(t *testing.T) {
	p, err := platforms.GetPublisher("xhs")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Publish(context.Background(), platforms.PublishRequest{DryRun: true})
	if err == nil {
		t.Fatal("expected caption validation error")
	}
}

func TestAPIPublisherLiveRequiresNativeConnector(t *testing.T) {
	p, err := platforms.GetPublisher("youtube")
	if err != nil {
		t.Fatal(err)
	}
	if p.Strategy() != platforms.StrategyAPI {
		t.Fatalf("strategy=%s", p.Strategy())
	}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		Title:  "vid",
		DryRun: false,
	})
	if err == nil {
		t.Fatal("expected API_CONNECTOR_UNAVAILABLE error")
	}
	if res == nil || res.ErrorCode != "API_CONNECTOR_UNAVAILABLE" {
		t.Fatalf("error code=%v", res)
	}
	if res.Status != "permanent_failed" {
		t.Fatalf("status=%s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "native API connector is unavailable") {
		t.Fatalf("message=%q", res.ErrorMessage)
	}
}

func TestAPIPublisherDryRun(t *testing.T) {
	p, err := platforms.GetPublisher("tiktok")
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		Caption: "clip",
		DryRun:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
}

func TestWechatMPHybrid(t *testing.T) {
	p, err := platforms.GetPublisher("wechat_mp")
	if err != nil {
		t.Fatal(err)
	}
	if p.Strategy() != platforms.StrategyHybrid {
		t.Fatalf("strategy=%s", p.Strategy())
	}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
}

func TestAPIPublisherLiveFailsClosedDespiteLegacyEnvironment(t *testing.T) {
	t.Setenv("AGENTPUSH_SIMULATE_PUBLISH", "1")
	t.Setenv("AGENTPUSH_API_WEBHOOK", "://must-not-be-called")
	for _, code := range []string{"youtube", "tiktok", "x"} {
		t.Run(code, func(t *testing.T) {
			p, err := platforms.GetPublisher(code)
			if err != nil {
				t.Fatal(err)
			}
			res, err := p.Publish(context.Background(), platforms.PublishRequest{
				Title:         "live attempt",
				DryRun:        false,
				CredentialRef: "must-not-leave-process",
			})
			if err == nil {
				t.Fatal("expected unavailable native connector error")
			}
			if res == nil || res.Status == "published" || res.ErrorCode != "API_CONNECTOR_UNAVAILABLE" {
				t.Fatalf("result=%+v err=%v", res, err)
			}
			if res.ExternalID != "" || res.PostURL != "" {
				t.Fatalf("live failure invented publish evidence: %+v", res)
			}
		})
	}
}
