package selectors

import (
	"strings"
	"testing"
)

func TestEmbeddedCatalogHasLoginContracts(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"xhs", "douyin", "wechat_video", "wechat_mp", "kuaishou", "facebook", "instagram", "youtube", "tiktok", "x", "toutiao", "omtencent", "weishi", "bilibili", "acfun", "baijiahao", "sina", "jianshu", "zhihu", "juejin", "csdn", "pdd"} {
		platform, ok := catalog.Platform(code)
		if !ok || len(platform.URLs) == 0 || len(platform.LoginIndicators) == 0 {
			t.Fatalf("missing login contract for %s: %+v", code, platform)
		}
		if _, err := catalog.LoginScreenScript(code); err != nil {
			t.Fatalf("script for %s: %v", code, err)
		}
	}
}

func TestCookieLoginMatchUsesNamesOnly(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		platform string
		name     string
	}{
		{platform: "xhs", name: "WEB_SESSION"},
		{platform: "xhs", name: "galaxy_creator_session_id"},
		{platform: "xhs", name: "access-token-creator.xiaohongshu.com"},
		{platform: "kuaishou", name: "kuaishou.web.cp.api_st"},
		{platform: "kuaishou", name: "kwssectoken"},
	} {
		if !catalog.CookiesIndicateLogin(tc.platform, []string{tc.name}) {
			t.Fatalf("expected %s cookie name %q to match", tc.platform, tc.name)
		}
	}
	if catalog.CookiesIndicateLogin("xhs", []string{"other"}) {
		t.Fatal("unexpected cookie match")
	}
}

func TestMetaAliasSharesUnifiedLoginContract(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := catalog.Platform("meta")
	if !ok || len(meta.URLs) == 0 || len(meta.LoginIndicators) == 0 {
		t.Fatalf("unified Meta login contract missing: %+v", meta)
	}
	for _, cookieName := range []string{"c_user", "xs"} {
		if !catalog.CookiesIndicateLogin("meta", []string{cookieName}) {
			t.Fatalf("unified Meta account did not accept reviewed cookie %q", cookieName)
		}
	}
	if catalog.CookiesIndicateLogin("meta", []string{"sessionid"}) {
		t.Fatal("an Instagram-only cookie must not prove the shared Business Suite session")
	}
}

func TestKuaishouCatalogIncludesCurrentCreatorHost(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	urls := catalog.URLs("kuaishou")
	want := "https://cp.kuaishou.com"
	for _, got := range urls {
		if got == want {
			return
		}
	}
	t.Fatalf("kuaishou URLs=%v, want %q", urls, want)
}

func TestLoginCookieEvidenceAvailability(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.HasLoginCookieEvidence("xhs") {
		t.Fatal("xhs should have an exact cookie-name contract")
	}
	if catalog.HasLoginCookieEvidence("bilibili") {
		t.Fatal("bilibili must remain manual verification until exact evidence is authored")
	}
}

func TestCatalogHasIndependentVersionAndConservativeXHSSettingOperations(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != 1 || catalog.CatalogVersion != 3 {
		t.Fatalf("catalog versions=%+v", catalog)
	}

	operations := []string{
		"publish.settings.visibility.public",
		"publish.settings.visibility.private",
		"publish.settings.comment_enabled",
		"publish.settings.declare_original",
	}
	for _, operation := range operations {
		rules := catalog.OperationRules(" XHS ", " "+operation+" ")
		if len(rules) == 0 {
			t.Fatalf("missing xhs operation %q", operation)
		}
		for _, rule := range rules {
			if strings.TrimSpace(rule.Query) == "" && strings.TrimSpace(rule.TextContains) == "" {
				t.Fatalf("empty rule for %q: %+v", operation, rule)
			}
		}
	}

	xhs, ok := catalog.Platform("xhs")
	if !ok {
		t.Fatal("xhs catalog missing")
	}
	for operation := range xhs.Operations {
		lower := strings.ToLower(operation)
		if strings.Contains(lower, "submit") || strings.Contains(lower, "success") {
			t.Fatalf("settings-only catalog must not claim submit/success operation: %q", operation)
		}
	}
}

func TestOperationRulesReturnsDefensiveCopy(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	rules := catalog.OperationRules("xhs", "publish.settings.visibility.public")
	if len(rules) == 0 {
		t.Fatal("operation rules missing")
	}
	original := rules[0]
	rules[0] = Rule{Query: "mutated"}
	again := catalog.OperationRules("xhs", "publish.settings.visibility.public")
	if len(again) == 0 || again[0] != original {
		t.Fatalf("catalog rules were mutated: before=%+v after=%+v", original, again)
	}
	if got := catalog.OperationRules("xhs", "missing"); got != nil {
		t.Fatalf("missing operation=%+v, want nil", got)
	}
}
