package egopublish

import (
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestEgoLiteManifestsCoverCatalogAndKeepStatusUnverified(t *testing.T) {
	manifests := EgoLiteManifests()
	catalog := platforms.EgoLiteAll()
	if len(manifests) != 22 {
		t.Fatalf("manifests=%d, want 22", len(manifests))
	}
	shop := map[string]bool{
		"douyin_shop": true, "taobao": true, "tiktok_shop": true,
		"shopee": true, "shopify": true, "amazon_seller": true,
		"qianchuan": true, "jd_shop": true, "pinduoduo_shop": true,
		"xhs_shop": true, "kuaishou_shop": true, "wechat_shop": true,
		"alibaba_1688": true, "aliexpress": true, "xianyu": true,
	}
	seen := map[string]bool{}
	for _, manifest := range manifests {
		if manifest.PlatformCode == "" || manifest.CreatorURL == "" || len(manifest.Capabilities) == 0 {
			t.Fatalf("incomplete manifest: %+v", manifest)
		}
		if seen[manifest.PlatformCode] {
			t.Fatalf("duplicate manifest: %s", manifest.PlatformCode)
		}
		if shop[manifest.PlatformCode] {
			t.Fatalf("shop platform must not have a publish manifest: %s", manifest.PlatformCode)
		}
		if manifest.Status != "unverified" {
			t.Fatalf("manifest status=%q, want unverified: %+v", manifest.Status, manifest)
		}
		for _, capability := range manifest.Capabilities {
			if capability.ContentType == "" || capability.ComposerURL == "" || capability.PrepareMode == "" {
				t.Fatalf("incomplete capability: %s %+v", manifest.PlatformCode, capability)
			}
			if capability.PrepareMode != PrepareAutomated && capability.PrepareMode != PrepareManualHandoff {
				t.Fatalf("unexpected prepare mode: %s %+v", manifest.PlatformCode, capability)
			}
			if capability.PrepareMode == PrepareAutomated && !capability.PrepareAdapterSupported {
				t.Fatalf("automated prepare must declare a local adapter: %s %+v", manifest.PlatformCode, capability)
			}
			if capability.PrepareMode == PrepareManualHandoff && capability.PrepareAdapterSupported {
				t.Fatalf("manual handoff must not claim an automated prepare adapter: %s %+v", manifest.PlatformCode, capability)
			}
			if capability.PrepareMode == PrepareManualHandoff && (capability.SubmitAdapterSupported || capability.VerifyAdapterSupported || capability.SubmitVerified || capability.VerifyVerified) {
				t.Fatalf("manual handoff must not expose submit/verify: %s %+v", manifest.PlatformCode, capability)
			}
			if capability.SubmitVerified || capability.VerifyVerified {
				t.Fatalf("live submit/verify must remain fail-closed without independent acceptance evidence: %s %+v", manifest.PlatformCode, capability)
			}
			if capability.NativeScheduleVerified {
				t.Fatalf("native scheduling must remain fail-closed without independent acceptance evidence: %s %+v", manifest.PlatformCode, capability)
			}
		}
		seen[manifest.PlatformCode] = true
	}
	for _, platform := range catalog {
		if !platform.SupportsArticle && !platform.SupportsGraphText && !platform.SupportsVideo {
			if seen[platform.Code] {
				t.Fatalf("login-only platform %s must not have a publish manifest", platform.Code)
			}
			continue
		}
		if platform.Code == "meta" {
			if !seen["facebook"] || !seen["instagram"] {
				t.Fatal("Meta login container must expose Facebook and Instagram publish manifests")
			}
			continue
		}
		if !seen[platform.Code] {
			t.Fatalf("catalog platform %s has no manifest", platform.Code)
		}
	}
}

func TestNativeScheduleCapabilitiesAreManifestScoped(t *testing.T) {
	for _, test := range []struct {
		platform, contentType string
		wantAdapter           bool
	}{
		{"douyin", "graph_text", true}, {"douyin", "video", true},
		{"kuaishou", "graph_text", true}, {"kuaishou", "video", true},
		{"tiktok", "video", true}, {"youtube", "video", true},
		{"wechat_video", "video", true}, {"facebook", "graph_text", true}, {"facebook", "video", true},
		{"xhs", "graph_text", false}, {"instagram", "video", false}, {"wechat_mp", "article", false}, {"csdn", "article", false}, {"tiktok", "graph_text", false},
	} {
		t.Run(test.platform+"/"+test.contentType, func(t *testing.T) {
			capability, ok := CapabilityFor(test.platform, test.contentType)
			if !ok || capability.NativeScheduleAdapterSupported != test.wantAdapter {
				t.Fatalf("capability=%+v ok=%v", capability, ok)
			}
			if capability.NativeScheduleMinLeadMinutes < 0 {
				t.Fatalf("invalid native schedule lead: %+v", capability)
			}
		})
	}
}

func TestFirstReleaseTenPlatformMatrixHasSixteenDistinctCapabilities(t *testing.T) {
	count := 0
	firstReleasePlatforms := 0
	for _, manifest := range EgoLiteManifests() {
		if !manifest.FirstRelease {
			continue
		}
		firstReleasePlatforms++
		count += len(manifest.Capabilities)
	}
	if firstReleasePlatforms != 10 {
		t.Fatalf("first-release platforms=%d, want 10", firstReleasePlatforms)
	}
	if count != 16 {
		t.Fatalf("first-release content contracts=%d, want 16", count)
	}

	tiktok, ok := ManifestFor("tiktok")
	if !ok || tiktok.PrepareModeFor("graph_text") != PrepareManualHandoff {
		t.Fatalf("TikTok Photo Post must remain manual handoff: %+v exists=%v", tiktok, ok)
	}
	if PrepareAdapterSupportedFor("tiktok", "graph_text") || SubmitAdapterSupportedFor("tiktok", "graph_text") || VerifyAdapterSupportedFor("tiktok", "graph_text") || SubmitVerifiedFor("tiktok", "graph_text") || VerifyVerifiedFor("tiktok", "graph_text") {
		t.Fatal("TikTok Photo Post must not expose any automated adapter")
	}
	bilibili, ok := ManifestFor("bilibili")
	if !ok || bilibili.PrepareModeFor("video") != PrepareManualHandoff || bilibili.FirstRelease {
		t.Fatalf("Bilibili video must not be an automated first-release capability: %+v exists=%v", bilibili, ok)
	}
}

func TestPrepareAdapterSupportIsExplicitAndMatchesModes(t *testing.T) {
	automated := 0
	manual := 0
	for _, manifest := range EgoLiteManifests() {
		for _, capability := range manifest.Capabilities {
			got := PrepareAdapterSupportedFor(manifest.PlatformCode, capability.ContentType)
			if got != capability.PrepareAdapterSupported {
				t.Fatalf("%s/%s helper=%t, capability=%t", manifest.PlatformCode, capability.ContentType, got, capability.PrepareAdapterSupported)
			}
			switch capability.PrepareMode {
			case PrepareAutomated:
				automated++
				if !got {
					t.Fatalf("%s/%s automated prepare is missing an adapter", manifest.PlatformCode, capability.ContentType)
				}
			case PrepareManualHandoff:
				manual++
				if got {
					t.Fatalf("%s/%s manual handoff exposes an adapter", manifest.PlatformCode, capability.ContentType)
				}
			}
		}
	}
	if automated != 15 || manual != 26 {
		t.Fatalf("automated/manual=%d/%d, want 15/26", automated, manual)
	}
	if PrepareAdapterSupportedFor("missing", "video") {
		t.Fatal("unknown platform must not expose a prepare adapter")
	}
}

func TestWechatMPArticleStaticSubmitAdapterRemainsUnverifiedWithoutManagerDOMContract(t *testing.T) {
	if !SubmitAdapterSupportedFor("wechat_mp", "article") {
		t.Fatal("WeChat Official Account article must expose its reviewed static submit adapter")
	}
	if !VerifyAdapterSupportedFor("wechat_mp", "article") {
		t.Fatal("WeChat Official Account article must expose its reviewed read-only manager verifier")
	}
	if SubmitVerifiedFor("wechat_mp", "article") || VerifyVerifiedFor("wechat_mp", "article") {
		t.Fatal("WeChat Official Account live submit and verify must remain fail-closed pending real account evidence")
	}
}

func TestUnreviewedMetaAdaptersKeepSubmitAndVerificationFailClosed(t *testing.T) {
	tests := []struct {
		platform    string
		contentType string
	}{
		{platform: "facebook", contentType: "video"},
		{platform: "instagram", contentType: "graph_text"},
		{platform: "instagram", contentType: "video"},
	}
	for _, test := range tests {
		t.Run(test.platform+"/"+test.contentType, func(t *testing.T) {
			if SubmitAdapterSupportedFor(test.platform, test.contentType) || VerifyAdapterSupportedFor(test.platform, test.contentType) {
				t.Fatalf("%s/%s must not expose unreviewed submit or manager DOM adapters", test.platform, test.contentType)
			}
			if SubmitVerifiedFor(test.platform, test.contentType) || VerifyVerifiedFor(test.platform, test.contentType) {
				t.Fatalf("%s/%s must remain PENDING VERIFICATION without creator-backend evidence", test.platform, test.contentType)
			}
		})
	}
}

func TestManifestContentTypesMatchPlatformCapabilities(t *testing.T) {
	contractCount := 0
	for _, manifest := range EgoLiteManifests() {
		platform, err := platforms.Find(manifest.PlatformCode)
		if err != nil {
			t.Fatalf("platform %s: %v", manifest.PlatformCode, err)
		}
		want := make([]string, 0, 3)
		if platform.SupportsArticle {
			want = append(want, "article")
		}
		if platform.SupportsGraphText {
			want = append(want, "graph_text")
		}
		if platform.SupportsVideo {
			want = append(want, "video")
		}
		if manifest.PlatformCode == "tiktok" {
			want = []string{"graph_text", "video"}
		}
		got := manifest.ContentTypes()
		if len(got) != len(want) {
			t.Fatalf("%s content types=%v, want %v", manifest.PlatformCode, got, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("%s content types=%v, want %v", manifest.PlatformCode, got, want)
			}
		}
		contractCount += len(got)
	}
	if contractCount != 41 {
		t.Fatalf("content contracts=%d, want 41", contractCount)
	}
	for _, code := range []string{"meta", "nonexistent"} {
		if _, ok := ManifestFor(code); ok {
			t.Fatalf("non-publish target %s unexpectedly has a manifest", code)
		}
	}
}

func TestComposerURLForCoversAllEgoLiteContentContracts(t *testing.T) {
	tests := map[string]string{
		"douyin/graph_text":    "https://creator.douyin.com/creator-micro/content/upload?default-tab=3",
		"douyin/video":         "https://creator.douyin.com/creator-micro/content/post/video",
		"wechat_video/video":   "https://channels.weixin.qq.com/platform/post/create",
		"kuaishou/graph_text":  "https://cp.kuaishou.com/article/publish/video",
		"kuaishou/video":       "https://cp.kuaishou.com/article/publish/video",
		"xhs/graph_text":       "https://creator.xiaohongshu.com/publish/publish?from=menu&target=image",
		"xhs/video":            "https://creator.xiaohongshu.com/publish/publish?from=menu&target=video",
		"tiktok/graph_text":    "https://www.tiktok.com/tiktokstudio",
		"tiktok/video":         "https://www.tiktok.com/tiktokstudio/upload",
		"youtube/video":        "https://studio.youtube.com/",
		"instagram/graph_text": "https://business.facebook.com/latest/composer/?asset_id=meta-page-1",
		"instagram/video":      "https://business.facebook.com/latest/instagram_reels_composer?asset_id=meta-page-1",
		"facebook/graph_text":  "https://business.facebook.com/latest/composer/?asset_id=meta-page-1",
		"facebook/video":       "https://business.facebook.com/latest/reels_composer?asset_id=meta-page-1",
		"wechat_mp/article":    "https://mp.weixin.qq.com/", "csdn/article": "https://editor.csdn.net/md/",
		"acfun/article": "https://member.acfun.cn", "acfun/video": "https://member.acfun.cn",
		"baijiahao/article": "https://baijiahao.baidu.com", "baijiahao/graph_text": "https://baijiahao.baidu.com", "baijiahao/video": "https://baijiahao.baidu.com",
		"bilibili/article": "https://member.bilibili.com/platform/home", "bilibili/video": "https://member.bilibili.com/platform/upload/video/frame",
		"jianshu/article": "https://www.jianshu.com", "juejin/article": "https://juejin.cn/creator", "juejin/graph_text": "https://juejin.cn/creator",
		"omtencent/article": "https://om.qq.com", "omtencent/video": "https://om.qq.com", "pdd/video": "https://mms.pinduoduo.com",
		"sina/article": "https://weibo.com", "sina/graph_text": "https://weibo.com", "sina/video": "https://weibo.com",
		"toutiao/article": "https://mp.toutiao.com", "toutiao/graph_text": "https://mp.toutiao.com", "toutiao/video": "https://mp.toutiao.com",
		"weishi/video": "https://weishi.qq.com", "x/graph_text": "https://x.com", "x/video": "https://x.com",
		"zhihu/article": "https://www.zhihu.com/creator", "zhihu/graph_text": "https://www.zhihu.com/creator", "zhihu/video": "https://www.zhihu.com/creator",
	}
	if len(tests) != 41 {
		t.Fatalf("content contract test cases=%d, want 41", len(tests))
	}
	for key, want := range tests {
		parts := strings.Split(key, "/")
		got, ok := ComposerURLFor(parts[0], parts[1], "meta-page-1")
		if !ok || got != want {
			t.Errorf("%s composer=(%q,%v), want %q", key, got, ok, want)
		}
	}
	for _, invalid := range [][2]string{{"nonexistent", "video"}, {"youtube", "article"}, {"xhs", "article"}, {"x", "article"}} {
		if got, ok := ComposerURLFor(invalid[0], invalid[1], ""); ok || got != "" {
			t.Errorf("invalid contract %v resolved to %q", invalid, got)
		}
	}
	for _, metaContract := range [][2]string{{"instagram", "graph_text"}, {"facebook", "video"}} {
		if got, ok := ComposerURLFor(metaContract[0], metaContract[1], ""); ok || got != "" {
			t.Errorf("Meta contract without asset ID resolved to %q", got)
		}
	}
}

func TestManualHandoffURLsResolveFromCapabilities(t *testing.T) {
	manualContracts := 0
	for _, manifest := range EgoLiteManifests() {
		for _, capability := range manifest.Capabilities {
			if capability.PrepareMode != PrepareManualHandoff {
				continue
			}
			manualContracts++
			if got, ok := ComposerURLFor(manifest.PlatformCode, capability.ContentType, ""); !ok || got != capability.ComposerURL {
				t.Errorf("%s/%s resolved=(%q,%v), want capability URL %q", manifest.PlatformCode, capability.ContentType, got, ok, capability.ComposerURL)
			}
		}
	}
	if manualContracts != 26 {
		t.Fatalf("manual handoff contracts=%d, want 26", manualContracts)
	}
}
