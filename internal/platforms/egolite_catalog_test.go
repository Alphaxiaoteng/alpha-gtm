package platforms_test

import (
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestEgoLiteCatalogUsesOneMetaLoginContainer(t *testing.T) {
	want := map[string]map[string]bool{
		"meta":           {"graph_text": true, "video": true},
		"xhs":            {"graph_text": true, "video": true},
		"douyin":         {"graph_text": true, "video": true},
		"kuaishou":       {"graph_text": true, "video": true},
		"tiktok":         {"video": true},
		"wechat_video":   {"video": true},
		"youtube":        {"video": true},
		"wechat_mp":      {"article": true},
		"csdn":           {"article": true},
		"acfun":          {"article": true, "video": true},
		"baijiahao":      {"article": true, "graph_text": true, "video": true},
		"bilibili":       {"article": true, "video": true},
		"jianshu":        {"article": true},
		"juejin":         {"article": true, "graph_text": true},
		"omtencent":      {"article": true, "video": true},
		"pdd":            {"video": true},
		"sina":           {"article": true, "graph_text": true, "video": true},
		"toutiao":        {"article": true, "graph_text": true, "video": true},
		"weishi":         {"video": true},
		"x":              {"graph_text": true, "video": true},
		"zhihu":          {"article": true, "graph_text": true, "video": true},
		"douyin_shop":    {},
		"taobao":         {},
		"tiktok_shop":    {},
		"shopee":         {},
		"shopify":        {},
		"amazon_seller":  {},
		"qianchuan":      {},
		"jd_shop":        {},
		"pinduoduo_shop": {},
		"xhs_shop":       {},
		"kuaishou_shop":  {},
		"wechat_shop":    {},
		"alibaba_1688":   {},
		"aliexpress":     {},
		"xianyu":         {},
	}

	catalog := platforms.EgoLiteAll()
	const wantCatalogSize = 36
	if len(want) != wantCatalogSize {
		t.Fatalf("test expectation has %d platforms, want %d", len(want), wantCatalogSize)
	}
	if len(catalog) != wantCatalogSize {
		t.Fatalf("EgoLite catalog has %d platforms, want %d", len(catalog), wantCatalogSize)
	}

	shapes := 0
	seen := make(map[string]bool, len(catalog))
	for _, platform := range catalog {
		expected, ok := want[platform.Code]
		if !ok {
			t.Fatalf("unexpected EgoLite platform %q", platform.Code)
		}
		if seen[platform.Code] {
			t.Fatalf("duplicate EgoLite platform %q", platform.Code)
		}
		seen[platform.Code] = true
		got := map[string]bool{
			"article":    platform.SupportsArticle,
			"graph_text": platform.SupportsGraphText,
			"video":      platform.SupportsVideo,
		}
		for shape, enabled := range got {
			if enabled != expected[shape] {
				t.Fatalf("%s shape %s=%v, want %v", platform.Code, shape, enabled, expected[shape])
			}
			if enabled {
				shapes++
			}
		}
		loginOnly := map[string]bool{
			"xhs": true, "douyin_shop": true, "taobao": true,
			"tiktok_shop": true, "shopee": true, "shopify": true, "amazon_seller": true,
			"qianchuan": true, "jd_shop": true, "pinduoduo_shop": true,
			"xhs_shop": true, "kuaishou_shop": true, "wechat_shop": true,
		}
		if platform.ManagerURL == "" || (!loginOnly[platform.Code] && !platform.SupportsPublish) {
			t.Fatalf("%s is not a usable first-release target: %+v", platform.Code, platform)
		}
		if loginOnly[platform.Code] && platform.SupportsPublish {
			t.Fatalf("%s must remain login-only: %+v", platform.Code, platform)
		}
		if platform.Code == "xhs" {
			readiness := platforms.Readiness(platform.Code)
			if readiness.Mode != "browser_automation" || !readiness.CanCreateJob || readiness.CanAttemptAutoSubmit {
				t.Fatalf("xhs readiness=%+v", readiness)
			}
		}
	}
	for code := range want {
		if !seen[code] {
			t.Fatalf("expected EgoLite platform %q is missing", code)
		}
	}
	if shapes != 38 {
		t.Fatalf("EgoLite catalog has %d content shapes, want 38", shapes)
	}
}
