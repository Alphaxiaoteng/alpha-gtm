package logincheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	cdptarget "github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

func requireRealBrowserFixture(t *testing.T) {
	t.Helper()
	if os.Getenv("ALPHA_NEXUS_REAL_BROWSER_SMOKE") != "1" {
		t.Skip("real Chromium fixture is opt-in so interrupted unit tests cannot block account browsers")
	}
}

func TestClassifyEvidenceNeverTurnsMissingEvidenceIntoLogout(t *testing.T) {
	tests := []struct {
		name      string
		cookies   []string
		snapshot  domSnapshot
		wantState string
		wantCode  string
	}{
		{name: "reviewed cookie", cookies: []string{"web_session"}, wantState: AuthLoggedIn, wantCode: ReasonVerifiedCookie},
		{name: "profile DOM", snapshot: domSnapshot{Name: "creator", AvatarSelector: "img.user_avatar", IdentityVerified: true}, wantState: AuthLoggedIn, wantCode: ReasonVerifiedProfileDOM},
		{name: "reviewed session DOM", snapshot: domSnapshot{SessionVerified: true}, wantState: AuthLoggedIn, wantCode: ReasonVerifiedSessionDOM},
		{name: "explicit login page", snapshot: domSnapshot{LoginRequired: true}, wantState: AuthLoggedOut, wantCode: ReasonLoginRequiredPage},
		{name: "expired page", snapshot: domSnapshot{SessionExpired: true}, wantState: AuthLoggedOut, wantCode: ReasonSessionExpiredPage},
		{name: "checkpoint is not logout", snapshot: domSnapshot{CheckpointRequired: true}, wantState: AuthUnknown, wantCode: ReasonCheckpointRequired},
		{name: "checkpoint overrides retained profile", cookies: []string{"web_session"}, snapshot: domSnapshot{Name: "stale name", AvatarSelector: "img.user_avatar", IdentityVerified: true, CheckpointRequired: true}, wantState: AuthUnknown, wantCode: ReasonCheckpointRequired},
		{name: "checkpoint wins over login and expiry", cookies: []string{"web_session"}, snapshot: domSnapshot{LoginRequired: true, SessionExpired: true, CheckpointRequired: true}, wantState: AuthUnknown, wantCode: ReasonCheckpointRequired},
		{name: "missing selectors is unknown", snapshot: domSnapshot{}, wantState: AuthUnknown, wantCode: ReasonProfileSelectorMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyEvidence("xhs", test.cookies, test.snapshot)
			if got.AuthState != test.wantState || got.ReasonCode != test.wantCode {
				t.Fatalf("got state=%q reason=%q, want state=%q reason=%q", got.AuthState, got.ReasonCode, test.wantState, test.wantCode)
			}
		})
	}
}

func TestConflictingVerifiedIdentityAndLoginEvidenceNeverLogsOut(t *testing.T) {
	for _, platform := range SupportedObservedPlatforms() {
		for _, conflict := range []struct {
			name     string
			snapshot domSnapshot
		}{
			{
				name: "visible login component",
				snapshot: domSnapshot{
					Name: "verified creator", AvatarSelector: "img", IdentityVerified: true,
					LoginRequired: true,
				},
			},
			{
				name: "visible expired component",
				snapshot: domSnapshot{
					Name: "verified creator", AvatarSelector: "img", IdentityVerified: true,
					SessionExpired: true,
				},
			},
		} {
			t.Run(platform+"/"+conflict.name, func(t *testing.T) {
				got := classifyEvidence(platform, nil, conflict.snapshot)
				if got.AuthState == AuthLoggedOut {
					t.Fatalf("conflicting evidence was persisted as logout: %+v", got)
				}
				if got.AuthState != AuthUnknown || got.ReasonCode != ReasonManualVerifyRequired || got.EvidenceCode != EvidenceMissing {
					t.Fatalf("conflicting evidence=%+v", got)
				}
			})
		}
	}
}

func TestUnpairedIdentityCannotUpdateAccountEvenWhenCookieIsValid(t *testing.T) {
	got := classifyEvidence("xhs", []string{"web_session"}, domSnapshot{
		Name: "content-area-author", AvatarSelector: "img.user_avatar",
	})
	if got.AuthState != AuthLoggedIn || got.ReasonCode != ReasonVerifiedCookie {
		t.Fatalf("observation=%+v", got)
	}
	if got.Name != "" || got.AccountIdentifier != "" {
		t.Fatalf("unverified identity escaped provenance boundary: %+v", got)
	}
}

func TestXHSVerifiedIdentifierIsStoredWithoutPresentationPrefix(t *testing.T) {
	for _, raw := range []string{"小红书账号: AgentCreator", "小红书号：AgentCreator"} {
		got := classifyEvidence("xhs", nil, domSnapshot{
			Name: "极客小腾", AccountIdentifier: raw, IdentityVerified: true,
			PageURL: "https://creator.xiaohongshu.com/new/home",
		})
		if got.AuthState != AuthLoggedIn || got.AccountIdentifier != "AgentCreator" {
			t.Fatalf("raw=%q observation=%+v", raw, got)
		}
	}
}

func TestLoggedInEvidenceWinsOverStaleLoginTab(t *testing.T) {
	observations := []Observation{
		{AuthState: AuthLoggedOut, ReasonCode: ReasonLoginRequiredPage},
		{AuthState: AuthLoggedIn, ReasonCode: ReasonVerifiedCookie, Name: "真实账号"},
	}
	got := mergeObservations(observations)
	if got.AuthState != AuthLoggedIn || got.Name != "真实账号" {
		t.Fatalf("merged=%+v", got)
	}
}

func TestMetaBusinessSuiteSelectedAssetOverridesDirectInstagramLogin(t *testing.T) {
	rule := profileRules["instagram"]
	if rule.RequiredSessionProofs != 1 || len(rule.SessionProofSelectors) != 2 {
		t.Fatalf("Meta selected-asset proof is incomplete: %+v", rule)
	}

	businessSuite := classifyEvidence("instagram", nil, domSnapshot{
		SessionProofCount: 1,
		PageURL:           "https://business.facebook.com/latest/composer/",
	})
	directInstagramLogin := classifyEvidence("instagram", nil, domSnapshot{
		LoginRequired: true,
		PageURL:       "https://www.instagram.com/accounts/login/",
	})
	got := mergeObservations([]Observation{directInstagramLogin, businessSuite})
	if got.AuthState != AuthLoggedIn || got.ReasonCode != ReasonVerifiedSessionDOM ||
		got.PageOrigin != "https://business.facebook.com/latest/composer/" {
		t.Fatalf("Business Suite selected asset did not remain the Meta session truth: %+v", got)
	}
}

func TestMetaBusinessSuiteRequiresOneReviewedAssetProof(t *testing.T) {
	got := classifyEvidence("instagram", nil, domSnapshot{
		SessionProofCount: 0,
		PageURL:           "https://business.facebook.com/latest/composer/",
	})
	if got.AuthState != AuthUnknown || got.ReasonCode != ReasonProfileSelectorMissing {
		t.Fatalf("missing Meta asset was incorrectly accepted as a reviewed session: %+v", got)
	}
}

func TestMetaLoginDetectionDoesNotTreatEveryFormAsLogout(t *testing.T) {
	for _, code := range []string{"meta", "facebook", "instagram"} {
		for _, selector := range profileRules[code].LoginContainers {
			if strings.TrimSpace(selector) == "form" {
				t.Fatalf("%s login detection still accepts a generic form: %+v", code, profileRules[code])
			}
		}
	}
}

func TestMetaGenericBusinessFormIsNotLogoutEvidence(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	script, err := profileObservationScript(profileRules["meta"])
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		form          string
		loginRequired bool
	}{
		{name: "ordinary business form", form: `<form><input name="title"><button>登录后发布设置</button></form>`},
		{name: "reviewed login form", form: `<form><input name="email"><button>Log in</button></form>`, loginRequired: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			html := `<!doctype html><html><body>` + test.form + `</body></html>`
			var snapshot domSnapshot
			if err := chromedp.Run(browserCtx,
				chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(html)),
				chromedp.WaitReady("body"),
				chromedp.Evaluate(script, &snapshot),
			); err != nil {
				t.Fatal(err)
			}
			if snapshot.LoginRequired != test.loginRequired {
				t.Fatalf("loginRequired=%v, want %v: %+v", snapshot.LoginRequired, test.loginRequired, snapshot)
			}
		})
	}
}

func TestUnifiedMetaAccountAcceptsReviewedBusinessSessionCookie(t *testing.T) {
	got := classifyEvidence("meta", []string{"c_user"}, domSnapshot{
		PageURL: "https://business.facebook.com/latest/home/",
	})
	if got.AuthState != AuthLoggedIn || got.ReasonCode != ReasonVerifiedCookie ||
		got.EvidenceCode != EvidenceReviewedCookie {
		t.Fatalf("unified Meta cookie evidence was lost: %+v", got)
	}
}

func TestUnknownEvidenceKeepsSafePageOriginForDiagnostics(t *testing.T) {
	got := mergeObservations([]Observation{{
		AuthState:    AuthUnknown,
		ReasonCode:   ReasonProfileSelectorMissing,
		EvidenceCode: EvidenceMissing,
		PageOrigin:   "https://mp.weixin.qq.com/cgi-bin/home",
	}})
	if got.PageOrigin != "https://mp.weixin.qq.com/cgi-bin/home" {
		t.Fatalf("safe page origin was lost: %+v", got)
	}
}

func TestIncompleteInspectionKeepsOnlySafePageOrigin(t *testing.T) {
	got := mergeInspectedObservations([]Observation{{
		AuthState:         AuthLoggedOut,
		ReasonCode:        ReasonLoginRequiredPage,
		EvidenceCode:      EvidenceExplicitLoginDOM,
		Name:              "must not survive",
		AccountIdentifier: "must-not-survive",
		PageOrigin:        "https://creator.douyin.com/login",
		AvatarPNG:         []byte{1, 2, 3},
	}}, true)
	if got.AuthState != AuthUnknown || got.ReasonCode != ReasonManualVerifyRequired {
		t.Fatalf("incomplete observation was not downgraded: %+v", got)
	}
	if got.PageOrigin != "https://creator.douyin.com/login" {
		t.Fatalf("safe page origin was lost: %+v", got)
	}
	if got.Name != "" || got.AccountIdentifier != "" || len(got.AvatarPNG) != 0 {
		t.Fatalf("unconfirmed identity escaped incomplete observation: %+v", got)
	}
}

func TestCheckpointWinsOverOtherCreatorTabs(t *testing.T) {
	checkpoint := Observation{AuthState: AuthUnknown, ReasonCode: ReasonCheckpointRequired, EvidenceCode: EvidenceCheckpointDOM}
	login := Observation{AuthState: AuthLoggedOut, ReasonCode: ReasonLoginRequiredPage, EvidenceCode: EvidenceExplicitLoginDOM}
	if got := mergeObservations([]Observation{checkpoint, login}); got.ReasonCode != ReasonCheckpointRequired || got.AuthState != AuthUnknown {
		t.Fatalf("merged checkpoint/login=%+v", got)
	}
	verified := Observation{AuthState: AuthLoggedIn, ReasonCode: ReasonVerifiedProfileDOM, EvidenceCode: EvidenceReviewedProfileDOM}
	if got := mergeObservations([]Observation{checkpoint, verified}); got.AuthState != AuthUnknown || got.ReasonCode != ReasonCheckpointRequired {
		t.Fatalf("merged checkpoint/verified=%+v", got)
	}
}

func TestSelectPlatformTargetsUsesOnlyExistingReviewedHosts(t *testing.T) {
	targets := []*cdptarget.Info{
		{Type: "page", URL: "about:blank", TargetID: "blank"},
		{Type: "service_worker", URL: "https://creator.douyin.com/sw.js", TargetID: "worker"},
		{Type: "page", URL: "https://evil.example/?next=creator.douyin.com", TargetID: "evil"},
		{Type: "page", URL: "https://www.douyin.com/user/1", TargetID: "public"},
		{Type: "page", URL: "https://creator.douyin.com/creator-micro/home", TargetID: "creator"},
	}
	got := selectPlatformTargets(targets, profileRules["douyin"].Hosts, profileRules["douyin"].LoginPaths)
	if len(got) != 1 || got[0].TargetID != "creator" {
		t.Fatalf("targets=%+v", got)
	}
}

func TestCSDNEditorIsAReviewedExistingTarget(t *testing.T) {
	targets := []*cdptarget.Info{
		{Type: "page", URL: "https://editor.csdn.net/md/", TargetID: "editor"},
		{Type: "page", URL: "https://blog.csdn.net/cpt1024", TargetID: "public"},
	}
	got := selectPlatformTargets(targets, profileRules["csdn"].Hosts, profileRules["csdn"].LoginPaths)
	if len(got) != 1 || got[0].TargetID != "editor" {
		t.Fatalf("targets=%+v", got)
	}
}

func TestCreatorHomeIsPrioritizedBeforeFourStaleLoginTargets(t *testing.T) {
	targets := []*cdptarget.Info{
		{Type: "page", URL: "https://creator.douyin.com/login?stale=1", TargetID: "login-1"},
		{Type: "page", URL: "https://creator.douyin.com/login/qr", TargetID: "login-2"},
		{Type: "page", URL: "https://creator.douyin.com/login/phone", TargetID: "login-3"},
		{Type: "page", URL: "https://creator.douyin.com/creator-micro/home", TargetID: "creator-home"},
	}
	selected := selectPlatformTargets(targets, profileRules["douyin"].Hosts, profileRules["douyin"].LoginPaths)
	bounded := boundedPlatformTargets(selected)
	if len(bounded) != 4 || bounded[0].TargetID != "creator-home" {
		t.Fatalf("prioritized targets=%+v", bounded)
	}
}

func TestPublicContentHostsCannotSupplyAccountIdentity(t *testing.T) {
	targets := []*cdptarget.Info{
		{Type: "page", URL: "https://www.xiaohongshu.com/explore/1", TargetID: "xhs-public"},
		{Type: "page", URL: "https://www.douyin.com/user/1", TargetID: "douyin-public"},
		{Type: "page", URL: "https://www.kuaishou.com/profile/1", TargetID: "kuaishou-public"},
	}
	for _, code := range []string{"xhs", "douyin", "kuaishou"} {
		if got := selectPlatformTargets(targets, profileRules[code].Hosts, profileRules[code].LoginPaths); len(got) != 0 {
			t.Fatalf("platform %s accepted public identity target: %+v", code, got)
		}
	}
}

func TestSnapshotURLMustRemainOnReviewedCreatorHost(t *testing.T) {
	hosts := profileRules["xhs"].Hosts
	for _, accepted := range []string{
		"https://creator.xiaohongshu.com/creator/home",
		"https://sub.creator.xiaohongshu.com/creator/home",
	} {
		if !pageURLMatchesReviewedHosts(accepted, hosts) {
			t.Fatalf("reviewed URL rejected: %s", accepted)
		}
	}
	for _, rejected := range []string{
		"https://www.xiaohongshu.com/explore/1",
		"https://creator.xiaohongshu.com.evil.example/creator/home",
		"javascript:alert(1)",
	} {
		if pageURLMatchesReviewedHosts(rejected, hosts) {
			t.Fatalf("unreviewed URL accepted: %s", rejected)
		}
	}
}

func TestPlatformTargetInspectionHasHardBound(t *testing.T) {
	targets := []*cdptarget.Info{{TargetID: "1"}, {TargetID: "2"}, {TargetID: "3"}, {TargetID: "4"}, {TargetID: "5"}, {TargetID: "6"}, {TargetID: "7"}}
	got := boundedPlatformTargets(targets)
	if len(got) != 6 || got[5].TargetID != "6" {
		t.Fatalf("bounded targets=%+v", got)
	}
}

func TestDOMProbeBoundsToTwoReviewedTargets(t *testing.T) {
	targets := []*cdptarget.Info{{TargetID: "home"}, {TargetID: "editor"}, {TargetID: "stale-login"}}
	got := boundedDOMProbeTargets(targets)
	if len(got) != 2 || got[0].TargetID != "home" || got[1].TargetID != "editor" {
		t.Fatalf("bounded DOM probe targets=%+v", got)
	}
}

func TestLimitedDOMProbePreservesIncompleteNegativeEvidence(t *testing.T) {
	targets := []*cdptarget.Info{{TargetID: "stale-login"}, {TargetID: "unknown-home"}, {TargetID: "creator"}}
	items, incomplete, err := inspectPlatformTargetsWithLimit(targets, domProbeTargetLimit, func(targetID cdptarget.ID) (*Observation, error) {
		if targetID == "creator" {
			t.Fatalf("bounded probe inspected target %q", targetID)
		}
		if targetID == "stale-login" {
			return &Observation{AuthState: AuthLoggedOut, ReasonCode: ReasonLoginRequiredPage, EvidenceCode: EvidenceExplicitLoginDOM}, nil
		}
		return &Observation{AuthState: AuthUnknown, ReasonCode: ReasonProfileSelectorMissing, EvidenceCode: EvidenceMissing}, nil
	})
	if err != nil || !incomplete {
		t.Fatalf("items=%+v incomplete=%v err=%v", items, incomplete, err)
	}
	got := mergeInspectedObservations(items, incomplete)
	if got.AuthState != AuthUnknown || got.ReasonCode != ReasonManualVerifyRequired {
		t.Fatalf("truncated negative observation=%+v", got)
	}
}

func TestLimitedDOMProbeAcceptsPositiveReviewedEvidence(t *testing.T) {
	targets := []*cdptarget.Info{{TargetID: "current"}, {TargetID: "other-editor"}, {TargetID: "third"}}
	items, incomplete, err := inspectPlatformTargetsWithLimit(targets, domProbeTargetLimit, func(targetID cdptarget.ID) (*Observation, error) {
		if targetID == "third" {
			t.Fatalf("bounded probe inspected target %q", targetID)
		}
		if targetID == "current" {
			return &Observation{AuthState: AuthLoggedIn, ReasonCode: ReasonVerifiedProfileDOM, EvidenceCode: EvidenceReviewedProfileDOM, Name: "creator"}, nil
		}
		return &Observation{AuthState: AuthUnknown, ReasonCode: ReasonProfileSelectorMissing, EvidenceCode: EvidenceMissing}, nil
	})
	if err != nil || !incomplete {
		t.Fatalf("items=%+v incomplete=%v err=%v", items, incomplete, err)
	}
	got := mergeInspectedObservations(items, incomplete)
	if got.AuthState != AuthLoggedIn || got.Name != "creator" {
		t.Fatalf("truncated positive observation=%+v", got)
	}
}

func TestFourthCreatorTargetIsInspectedAfterThreeLoginOverlays(t *testing.T) {
	targets := []*cdptarget.Info{{TargetID: "1"}, {TargetID: "2"}, {TargetID: "3"}, {TargetID: "4"}}
	called := make([]string, 0, len(targets))
	items, incomplete, err := inspectPlatformTargets(targets, func(targetID cdptarget.ID) (*Observation, error) {
		called = append(called, string(targetID))
		if targetID == "4" {
			return &Observation{AuthState: AuthLoggedIn, ReasonCode: ReasonVerifiedProfileDOM, EvidenceCode: EvidenceReviewedProfileDOM, Name: "creator"}, nil
		}
		return &Observation{AuthState: AuthLoggedOut, ReasonCode: ReasonLoginRequiredPage, EvidenceCode: EvidenceExplicitLoginDOM}, nil
	})
	if err != nil || incomplete || strings.Join(called, ",") != "1,2,3,4" {
		t.Fatalf("inspection called=%v incomplete=%v err=%v", called, incomplete, err)
	}
	if got := mergeInspectedObservations(items, incomplete); got.AuthState != AuthLoggedIn || got.Name != "creator" {
		t.Fatalf("fourth creator login evidence was lost: %+v", got)
	}
}

func TestIncompleteNegativeInspectionDowngradesToUnknown(t *testing.T) {
	items := []Observation{{
		AuthState: AuthLoggedOut, ReasonCode: ReasonLoginRequiredPage, EvidenceCode: EvidenceExplicitLoginDOM,
	}}
	got := mergeInspectedObservations(items, true)
	if got.AuthState != AuthUnknown || got.ReasonCode != ReasonManualVerifyRequired || got.EvidenceCode != EvidenceMissing {
		t.Fatalf("incomplete negative inspection was persisted: %+v", got)
	}
}

func TestSanitizePageOriginDropsQueryFragmentAndCredentials(t *testing.T) {
	got := sanitizePageOrigin("https://user:pass@creator.douyin.com/creator-micro/home?token=secret#frag")
	if got != "https://creator.douyin.com/creator-micro/home" {
		t.Fatalf("origin=%q", got)
	}
	if got := sanitizePageOrigin("javascript:alert(1)"); got != "" {
		t.Fatalf("unsafe origin=%q", got)
	}
}

func TestSixPlatformRulesHaveSpecificProfileAndAvatarSelectors(t *testing.T) {
	for _, code := range SupportedObservedPlatforms() {
		rule, ok := profileRules[code]
		if !ok || len(rule.Hosts) == 0 || len(rule.AccountRootSelectors) == 0 || len(rule.NameSelectors) == 0 || len(rule.AvatarSelectors) == 0 || len(rule.LoginContainers) == 0 || len(rule.LoginTexts) == 0 || len(rule.DialogSelectors) == 0 {
			t.Fatalf("platform %s has incomplete reviewed observation rule: %+v", code, rule)
		}
	}
	if profileRules["douyin"].NameSelectors[0] != "[class^='name-']" || profileRules["douyin"].AvatarSelectors[0] != "[class^='avatar-'] img" {
		t.Fatal("douyin live-reviewed selectors lost priority")
	}
	if !strings.Contains(strings.Join(profileRules["douyin"].SessionContainers, "\x00"), "button") || !strings.Contains(strings.Join(profileRules["douyin"].SessionTexts, "\x00"), "作品发布") {
		t.Fatal("douyin reviewed composer session evidence is incomplete")
	}
	if profileRules["wechat_mp"].AvatarSelectors[0] != "img.weui-desktop-account__img" {
		t.Fatal("wechat_mp live-reviewed avatar selector lost priority")
	}
	if strings.Join(profileRules["wechat_video"].AvatarResourceHostSuffixes, "\x00") != "wx.qlogo.cn" {
		t.Fatalf("wechat_video must load its reviewed original avatar resource: %+v", profileRules["wechat_video"])
	}
	if len(profileRules["wechat_video"].SessionAvatarSelectors) == 0 ||
		profileRules["wechat_video"].SessionAvatarSelectors[0] != ".account-info img.avatar" {
		t.Fatalf("wechat_video must prefer its stable lower-left account avatar: %+v", profileRules["wechat_video"])
	}
	if strings.Join(profileRules["wechat_mp"].SessionContainers, "\x00") != ".new-creation__menu-item\x00a[href*='/cgi-bin/appmsg']" || strings.Join(profileRules["wechat_mp"].SessionTexts, "\x00") != "文章" {
		t.Fatal("wechat_mp reviewed creator-home session evidence is incomplete")
	}
	if profileRules["kuaishou"].NameSelectors[0] != ".user-info-dpd" || profileRules["kuaishou"].AvatarSelectors[0] != ".user-info-avatar img" {
		t.Fatal("kuaishou live-reviewed selectors lost priority")
	}
	if !strings.Contains(strings.Join(profileRules["kuaishou"].LoginContainers, "\x00"), "a.login") || !strings.Contains(strings.Join(profileRules["kuaishou"].LoginTexts, "\x00"), "立即登录") {
		t.Fatal("kuaishou creator-console logout evidence is incomplete")
	}
	exact := map[string]struct {
		names, avatars []string
		roots          []string
	}{
		"xhs":          {[]string{".account-name", ".name-box"}, []string{".avatar img", "img.user_avatar"}, []string{".base", ".user-info"}},
		"douyin":       {[]string{"[class^='name-']"}, []string{"[class^='avatar-'] img"}, []string{"[class^='container-']"}},
		"wechat_mp":    {[]string{".weui-desktop_name"}, []string{"img.weui-desktop-account__img"}, []string{".weui-desktop-person_info"}},
		"wechat_video": {[]string{".finder-nickname"}, []string{"img[alt='视频号头像']"}, []string{".finder-info-container"}},
		"kuaishou":     {[]string{".user-info-dpd"}, []string{".user-info-avatar img"}, []string{".user-info-dpd", ".user-info"}},
		"csdn":         {[]string{".info-box .name"}, []string{".avatar-box img"}, []string{".home-exp-user-card__head", ".user-info-box"}},
	}
	for code, want := range exact {
		rule := profileRules[code]
		if strings.Join(rule.NameSelectors, "\x00") != strings.Join(want.names, "\x00") ||
			strings.Join(rule.AvatarSelectors, "\x00") != strings.Join(want.avatars, "\x00") ||
			strings.Join(rule.AccountRootSelectors, "\x00") != strings.Join(want.roots, "\x00") {
			t.Fatalf("platform %s identity provenance is not exact: %+v", code, rule)
		}
	}
	if rule := profileRules["douyin"]; !rule.RequireIdentifier || rule.AccountRootMaxHeight <= 0 || rule.IdentityMaxCenterDistance <= 0 {
		t.Fatalf("douyin identity root constraints are incomplete: %+v", rule)
	}
	if rule := profileRules["xhs"]; !rule.RequireIdentifier || strings.Join(rule.IdentifierSelectors, "\x00") != ".others.description-text > div:first-child" {
		t.Fatalf("xhs home-card identifier provenance is incomplete: %+v", rule)
	}
	if rule := profileRules["csdn"]; !rule.RequireIdentifier || rule.IdentifierLinkHostSuffix != ".blog.csdn.net" || rule.IdentifierLinkPathHost != "blog.csdn.net" || len(rule.IdentifierLinkSelectors) != 1 {
		t.Fatalf("csdn public handle provenance is incomplete: %+v", rule)
	}
	if strings.Join(profileRules["csdn"].AvatarResourceHostSuffixes, "\x00") != "i-avatar.csdnimg.cn" {
		t.Fatalf("CSDN must load its reviewed original avatar instead of persisting the cropped header element: %+v", profileRules["csdn"])
	}
	if strings.Contains(strings.Join(profileRules["csdn"].LoginContainers, "\x00"), "[class*='passport']") {
		t.Fatal("csdn login detection must not treat every passport-related element as a live login panel")
	}
}

func TestCurrentVideoPlatformProfilesIncludeExplicitLoginAndIdentityBoundaries(t *testing.T) {
	for _, code := range []string{"facebook", "instagram", "tiktok", "youtube"} {
		rule, ok := profileRules[code]
		if !ok {
			t.Fatalf("platform %s is not monitored after account creation", code)
		}
		if len(rule.Hosts) == 0 || len(rule.AccountRootSelectors) == 0 || len(rule.NameSelectors) == 0 ||
			len(rule.AvatarSelectors) == 0 || len(rule.LoginContainers) == 0 || len(rule.LoginTexts) == 0 {
			t.Fatalf("platform %s has incomplete reviewed boundaries: %+v", code, rule)
		}
	}
	instagram := profileRules["instagram"]
	if !strings.Contains(strings.Join(instagram.LoginPaths, "\x00"), "/accounts/login") ||
		!strings.Contains(strings.Join(instagram.LoginContainers, "\x00"), "input[name='username']") {
		t.Fatalf("Instagram login page cannot override stale cookie evidence: %+v", instagram)
	}
	if instagram.Hosts[0] != "business.facebook.com" ||
		instagram.AccountRootSelectors[0] != "header section" ||
		!strings.Contains(strings.Join(instagram.SessionAvatarSelectors, "\x00"), "img[alt='Facebook']") ||
		!strings.Contains(strings.Join(instagram.SessionAvatarSelectors, "\x00"), "img[alt='Instagram']") {
		t.Fatalf("Instagram must use the shared reviewed Meta account-switcher identity: %+v", instagram)
	}
	if !strings.Contains(strings.Join(instagram.AvatarResourceHostSuffixes, "\x00"), ".fbcdn.net") {
		t.Fatalf("Instagram must load the reviewed Meta avatar resource instead of screenshotting its platform badge overlay: %+v", instagram)
	}
	facebook := profileRules["facebook"]
	if facebook.Hosts[0] != "business.facebook.com" ||
		!strings.Contains(strings.Join(facebook.SessionAvatarSelectors, "\x00"), "img[alt='Facebook']") ||
		!strings.Contains(strings.Join(facebook.SessionAvatarSelectors, "\x00"), "img[alt='Instagram']") ||
		strings.Join(facebook.SessionAvatarSelectors, "\x00") != strings.Join(instagram.SessionAvatarSelectors, "\x00") {
		t.Fatalf("Facebook and Instagram must share one reviewed Meta login identity: facebook=%+v instagram=%+v", facebook, instagram)
	}
	youtube := profileRules["youtube"]
	if youtube.AccountRootSelectors[0] != "ytcp-navigation-drawer #entity" ||
		youtube.NameSelectors[0] != "#entity-name" ||
		youtube.AvatarSelectors[0] != "#avatar img" {
		t.Fatalf("YouTube Studio identity must use the reviewed navigation identity card: %+v", youtube)
	}
	if !strings.Contains(strings.Join(youtube.AccountRootSelectors, "\x00"), "ytcp-navigation-drawer") ||
		!strings.Contains(strings.Join(youtube.AccountRootSelectors, "\x00"), "body") ||
		!strings.Contains(strings.Join(youtube.PrefixedNameSelectors, "\x00"), "div") ||
		!strings.Contains(strings.Join(youtube.NamePrefixes, "\x00"), "Your channel") {
		t.Fatalf("YouTube Studio current drawer label fallback is missing: %+v", youtube)
	}
	tiktok := profileRules["tiktok"]
	if !strings.Contains(strings.Join(tiktok.AccountRootSelectors, "\x00"), "body") ||
		!strings.Contains(strings.Join(tiktok.HiddenNameSelectors, "\x00"), "a[href^='/@']") ||
		!strings.Contains(strings.Join(tiktok.AvatarSelectors, "\x00"), "img[src*='tiktokx-cropcenter']") {
		t.Fatalf("TikTok Studio current profile-card fallback is missing: %+v", tiktok)
	}
	if !strings.Contains(strings.Join(tiktok.SessionAvatarSelectors, "\x00"), "[data-tt='Header_NewHeader_Clickable']") ||
		!strings.Contains(strings.Join(tiktok.AvatarResourceHostSuffixes, "\x00"), ".tiktokcdn-us.com") {
		t.Fatalf("TikTok must load its stable reviewed profile avatar resource: %+v", tiktok)
	}
}

func TestReviewedSessionAvatarDoesNotRequireWeakPageIdentity(t *testing.T) {
	requireRealBrowserFixture(t)
	tests := []struct {
		platform string
		html     string
	}{
		{
			platform: "douyin",
			html: `<header>
				<div class="dropdown-wrapper-current avatar-wrapper-current" style="width:74px;height:32px">
					<div class="avatar-current" style="width:32px;height:32px;background-image:linear-gradient(#087ea4,#087ea4)"></div>
				</div>
			</header><button style="display:block">作品发布</button>`,
		},
		{
			platform: "instagram",
			html: `<div role="combobox" style="display:block;width:196px;height:48px">
				<div style="display:inline-block;width:32px;height:32px">
					<div><img class="account-avatar" width="24" height="24" alt="" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24'%3E%3Crect width='24' height='24' fill='%23087ea4'/%3E%3C/svg%3E"></div>
					<div><img width="12" height="12" alt="Instagram" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12'/%3E"></div>
				</div>
				<div style="display:inline-block;width:32px;height:32px">
					<div><img class="account-avatar" width="24" height="24" alt="" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24'%3E%3Crect width='24' height='24' fill='%23b3261e'/%3E%3C/svg%3E"></div>
					<div><img width="12" height="12" alt="Facebook" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12'/%3E"></div>
				</div>
			</div><nav style="display:block">首页 内容</nav>`,
		},
		{
			platform: "wechat_video",
			html: `<div class="finder-ui-desktop-menu" style="display:block">内容管理</div>
				<div class="account-info" style="display:block;width:180px;height:48px">
					<img class="avatar" width="32" height="32"
						src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='32' height='32'%3E%3Crect width='32' height='32' fill='%230087a8'/%3E%3C/svg%3E">
				</div>`,
		},
	}
	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
			allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
			defer allocatorCancel()
			browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
			defer browserCancel()
			page := `<!doctype html><html><body>` + test.html + `</body></html>`
			if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(page)), chromedp.WaitReady("body")); err != nil {
				t.Skipf("isolated Chromium fixture unavailable: %v", err)
			}
			script, err := profileObservationScript(profileRules[test.platform])
			if err != nil {
				t.Fatal(err)
			}
			var snapshot domSnapshot
			if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
				t.Fatal(err)
			}
			if !snapshot.SessionVerified || snapshot.IdentityVerified || snapshot.AvatarSelector == "" || snapshot.AvatarIndex < 0 {
				t.Fatalf("%s reviewed session avatar was not selected safely: %+v", test.platform, snapshot)
			}
		})
	}
}

func TestCurrentGlobalCreatorIdentityFixtures(t *testing.T) {
	requireRealBrowserFixture(t)
	tests := []struct {
		platform   string
		name       string
		identifier string
		html       string
	}{
		{
			platform: "youtube",
			name:     "阿爾法玩家α",
			html: `<youtube-studio-shell id="studio" style="display:block;width:260px;height:420px"></youtube-studio-shell>
				<script>
					document.querySelector("#studio").attachShadow({mode:"open"}).innerHTML =
						'<ytcp-navigation-drawer style="display:block;width:260px;height:420px">' +
						'<div id="entity" style="display:block;width:220px;height:120px">' +
						'<div id="avatar"><img alt="channel avatar" src="data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%2248%22 height=%2248%22/%3E" style="width:48px;height:48px"></div>' +
						'<div aria-label="Your channel 阿爾法玩家α" style="display:block;width:180px;height:24px"></div></div><div>Analytics</div></ytcp-navigation-drawer>';
				</script>`,
		},
		{
			platform:   "tiktok",
			name:       "agame911",
			identifier: "agame911",
			html: `<img src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48'/%3E#tiktokx-cropcenter" style="width:48px;height:48px">
				<a href="/@agame911" style="display:none">微信Lavine00ds（江浙沪线下搭讪）</a>
				<button style="display:block">上传</button>`,
		},
		{
			platform: "instagram",
			name:     "AlphaIntelligence",
			html: `<header><section style="display:block;width:220px;height:80px">
					<div role="heading" style="display:block">AlphaIntelligence</div>
					<img alt="profile picture" width="48" height="48" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48'/%3E">
				</section></header>`,
		},
	}
	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
			allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
			defer allocatorCancel()
			browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
			defer browserCancel()
			page := `<!doctype html><html><body>` + test.html + `</body></html>`
			if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(page)), chromedp.WaitReady("body")); err != nil {
				t.Skipf("isolated Chromium fixture unavailable: %v", err)
			}
			script, err := profileObservationScript(profileRules[test.platform])
			if err != nil {
				t.Fatal(err)
			}
			var snapshot domSnapshot
			if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
				t.Fatal(err)
			}
			if !snapshot.IdentityVerified || snapshot.Name != test.name || snapshot.AccountIdentifier != test.identifier || snapshot.AvatarSelector == "" {
				var debug any
				_ = chromedp.Run(browserCtx, chromedp.Evaluate(`({
					body: document.body.getBoundingClientRect().toJSON(),
					links: Array.from(document.querySelectorAll("a[href^='/@']")).map(el => ({text: el.textContent, rect: el.getBoundingClientRect().toJSON()})),
					images: Array.from(document.querySelectorAll("img")).map(el => ({src: el.getAttribute("src"), rect: el.getBoundingClientRect().toJSON()})),
					headings: Array.from(document.querySelectorAll("h1,h2")).map(el => ({text: el.textContent, rect: el.getBoundingClientRect().toJSON()}))
				})`, &debug))
				t.Fatalf("%s current identity fixture was not verified: %+v debug=%+v", test.platform, snapshot, debug)
			}
		})
	}
}

func TestTikTokProfileRuleUsesProfileHandleAsIdentifier(t *testing.T) {
	rule := profileRules["tiktok"]
	if !strings.Contains(strings.Join(rule.IdentifierLinkSelectors, "\x00"), "a[href^='/@']") {
		t.Fatalf("TikTok identifier must come from the profile handle link: %+v", rule)
	}
	if rule.IdentifierLinkPathHost != "www.tiktok.com" {
		t.Fatalf("TikTok identifier link host=%q, want www.tiktok.com", rule.IdentifierLinkPathHost)
	}
	script, err := profileObservationScript(rule)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "profileHandleFromHref") || !strings.Contains(script, "^\\/@([A-Za-z0-9._-]{1,80})") {
		t.Fatal("TikTok observation script must normalize the @handle from the profile URL")
	}
}

func TestChromeErrorIsPlatformUnavailableInsteadOfMissingTab(t *testing.T) {
	got := unreviewedSnapshotObservation("chrome-error://chromewebdata/")
	if got.AuthState != AuthUnknown || got.ReasonCode != ReasonPlatformUnreachable || got.EvidenceCode != EvidenceMissing {
		t.Fatalf("chrome error observation=%+v", got)
	}
	other := unreviewedSnapshotObservation("https://evil.example/")
	if other.ReasonCode != ReasonPlatformTabMissing {
		t.Fatalf("unreviewed page observation=%+v", other)
	}
}

func TestAvatarResourceURLRequiresReviewedHTTPSHost(t *testing.T) {
	allowed := []string{".douyinpic.com"}
	tests := []struct {
		raw  string
		want bool
	}{
		{raw: "https://p3.douyinpic.com/avatar.jpeg?signature=local", want: true},
		{raw: "https://douyinpic.com/avatar.jpeg", want: true},
		{raw: "https://evil-douyinpic.com/avatar.jpeg", want: false},
		{raw: "http://p3.douyinpic.com/avatar.jpeg", want: false},
		{raw: "https://p3.douyinpic.com:8443/avatar.jpeg", want: false},
		{raw: "https://user@p3.douyinpic.com/avatar.jpeg", want: false},
	}
	for _, test := range tests {
		if got := avatarResourceURLAllowed(test.raw, allowed); got != test.want {
			t.Fatalf("avatarResourceURLAllowed(%q)=%v, want %v", test.raw, got, test.want)
		}
	}
}

func TestWechatVideoAvatarResourceURLAllowsOnlyReviewedHTTPSHost(t *testing.T) {
	allowed := profileRules["wechat_video"].AvatarResourceHostSuffixes
	tests := []struct {
		raw  string
		want bool
	}{
		{raw: "https://wx.qlogo.cn/mmhead/example/132", want: true},
		{raw: "https://sub.wx.qlogo.cn/mmhead/example/132", want: false},
		{raw: "https://evil-wx.qlogo.cn/mmhead/example/132", want: false},
		{raw: "http://wx.qlogo.cn/mmhead/example/132", want: false},
		{raw: "https://wx.qlogo.cn:8443/mmhead/example/132", want: false},
		{raw: "https://user@wx.qlogo.cn/mmhead/example/132", want: false},
	}
	for _, test := range tests {
		if got := avatarResourceURLAllowed(test.raw, allowed); got != test.want {
			t.Fatalf("avatarResourceURLAllowed(%q)=%v, want %v", test.raw, got, test.want)
		}
	}
}

func TestTikTokAvatarResourceURLAllowsOnlyReviewedCDN(t *testing.T) {
	allowed := profileRules["tiktok"].AvatarResourceHostSuffixes
	tests := []struct {
		raw  string
		want bool
	}{
		{raw: "https://p16-common-sign.tiktokcdn-us.com/avatar.jpeg?signature=local", want: true},
		{raw: "https://tiktokcdn-us.com/avatar.jpeg", want: true},
		{raw: "https://evil-tiktokcdn-us.com/avatar.jpeg", want: false},
		{raw: "http://p16-common-sign.tiktokcdn-us.com/avatar.jpeg", want: false},
		{raw: "https://p16-common-sign.tiktokcdn-us.com:8443/avatar.jpeg", want: false},
		{raw: "https://user@p16-common-sign.tiktokcdn-us.com/avatar.jpeg", want: false},
	}
	for _, test := range tests {
		if got := avatarResourceURLAllowed(test.raw, allowed); got != test.want {
			t.Fatalf("avatarResourceURLAllowed(%q)=%v, want %v", test.raw, got, test.want)
		}
	}
}

func TestVideoAvatarScreenshotWaitsForFullyLoadedOpaqueImage(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()

	page := `<!doctype html><html><body>
		<div class="finder-info-container" style="display:block;width:240px;height:100px">
			<div class="finder-nickname" style="display:block">动态社交C</div>
			<img alt="视频号头像" width="48" height="48" style="opacity:.25"
				src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48'%3E%3Crect width='48' height='48' fill='%230087a8'/%3E%3C/svg%3E">
		</div>
	</body></html>`
	if err := chromedp.Run(browserCtx,
		chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(page)),
		chromedp.WaitReady("img[alt='视频号头像']"),
	); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["wechat_video"])
	if err != nil {
		t.Fatal(err)
	}
	var fading domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &fading)); err != nil {
		t.Fatal(err)
	}
	if !fading.IdentityVerified || fading.AvatarResourceURL == "" || fading.AvatarScreenshotReady {
		t.Fatalf("fading video avatar must wait before element screenshot: %+v", fading)
	}

	var ready domSnapshot
	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate(`document.querySelector("img[alt='视频号头像']").style.opacity = "1"`, nil),
		chromedp.Evaluate(script, &ready),
	); err != nil {
		t.Fatal(err)
	}
	if !ready.AvatarScreenshotReady {
		t.Fatalf("fully loaded opaque video avatar should permit screenshot fallback: %+v", ready)
	}
}

func TestEqualPriorityLoggedInObservationCannotOverwriteStrongerIdentity(t *testing.T) {
	strong := Observation{
		AuthState: AuthLoggedIn, ReasonCode: ReasonVerifiedProfileDOM,
		Name: "verified creator", AccountIdentifier: "creator-id", AvatarPNG: []byte{1, 2, 3},
	}
	weak := Observation{AuthState: AuthLoggedIn, ReasonCode: ReasonVerifiedProfileDOM, Name: "weak"}
	got := mergeObservations([]Observation{strong, weak})
	if got.Name != strong.Name || got.AccountIdentifier != strong.AccountIdentifier || len(got.AvatarPNG) != len(strong.AvatarPNG) {
		t.Fatalf("strong identity was overwritten: %+v", got)
	}

	cookieOnly := Observation{AuthState: AuthLoggedIn, ReasonCode: ReasonVerifiedCookie}
	got = mergeObservations([]Observation{strong, cookieOnly})
	if got.Name != strong.Name || got.ReasonCode != ReasonVerifiedProfileDOM {
		t.Fatalf("cookie-only evidence replaced profile identity: %+v", got)
	}
}

func TestLoginEvidenceScriptNeverScansWholeBodyText(t *testing.T) {
	script, err := profileObservationScript(profileRules["wechat_video"])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"document.body", "body.includes", "body.innerText"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("login evidence script contains body-wide scan %q", forbidden)
		}
	}
	for _, required := range []string{"visibleContainerIncludes(rule.LoginContainers", "visibleContainerIncludes(rule.DialogSelectors"} {
		if !strings.Contains(script, required) {
			t.Fatalf("login evidence script missing scoped evidence %q", required)
		}
	}
	// Fixture contract: prose elsewhere on the page containing “扫码登录” or
	// “请重新登录” is never inspected, so it cannot become logout evidence.
}

func TestKuaishouCreatorLandingDetectsExplicitLoginPrompt(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><body><a class="login" style="display:block;width:160px;height:40px">立即登录</a></body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["kuaishou"])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
		t.Fatal(err)
	}
	classified := classifyEvidence("kuaishou", nil, snapshot)
	if !snapshot.LoginRequired || classified.AuthState != AuthLoggedOut || classified.ReasonCode != ReasonLoginRequiredPage {
		t.Fatalf("kuaishou login prompt was not classified as logout: snapshot=%+v classified=%+v", snapshot, classified)
	}
}

func TestProfileObservationScriptRequiresIdentityInsideReviewedRoot(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><body>
<section><div class="finder-nickname">content-area-author</div><img alt="视频号头像" width="48" height="48"></section>
<header class="finder-info-container"><div class="finder-nickname">real-creator</div></header>
</body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["wechat_video"])
	if err != nil {
		t.Fatal(err)
	}
	var unpaired domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &unpaired)); err != nil {
		t.Fatal(err)
	}
	if unpaired.IdentityVerified || unpaired.Name != "" || unpaired.AvatarSelector != "" {
		t.Fatalf("identity escaped reviewed root: %+v", unpaired)
	}
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`(() => {
  const image = document.createElement('img');
  image.alt = '视频号头像';
  image.width = 48;
  image.height = 48;
  image.src = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48'%3E%3Crect width='48' height='48' fill='%23087ea4'/%3E%3C/svg%3E";
  document.querySelector('.finder-info-container').appendChild(image);
})()`, nil)); err != nil {
		t.Fatal(err)
	}
	var paired domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &paired)); err != nil {
		t.Fatal(err)
	}
	if !paired.IdentityVerified || paired.Name != "real-creator" || paired.AvatarSelector != "img[alt='视频号头像']" {
		t.Fatalf("reviewed root identity was not paired: %+v", paired)
	}
}

func TestReviewedIdentityPairRejectsCrossRegionSPARoot(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><head><style>
#root { position: relative; width: 1200px; height: 900px; }
header { position: absolute; inset: 0 0 auto 0; height: 80px; }
main { position: absolute; inset: 420px 0 auto 0; height: 400px; }
</style></head><body><div id="root">
<header><img class="user_avatar" alt="账号头像" width="48" height="48"
 src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48'%3E%3Crect width='48' height='48' fill='%23087ea4'/%3E%3C/svg%3E"></header>
<main><div class="name-box">content-area-author</div></main>
</div></body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["xhs"])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
		t.Fatal(err)
	}
	if snapshot.IdentityVerified || snapshot.Name != "" || snapshot.AvatarSelector != "" {
		t.Fatalf("cross-region SPA elements were paired through #root: %+v", snapshot)
	}
}

func TestXHSHomeCardPairsReviewedIdentifierWithIdentity(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><body>
	<section class="base" style="display:flex;gap:16px;width:640px;height:122px">
  <div class="avatar"><img alt="账号头像" width="72" height="72"
   src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='72' height='72'%3E%3Crect width='72' height='72' fill='%23087ea4'/%3E%3C/svg%3E"></div>
  <div class="text">
    <div class="account-name">XHS creator</div>
    <div class="others description-text"><div>小红书账号: AgentCreator</div><div></div><div>还没有简介</div></div>
  </div>
</section></body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["xhs"])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
		t.Fatal(err)
	}
	if !snapshot.IdentityVerified || snapshot.Name != "XHS creator" || snapshot.AccountIdentifier != "小红书账号: AgentCreator" || snapshot.AvatarSelector != ".avatar img" {
		t.Fatalf("xhs reviewed home-card identity was not paired: %+v", snapshot)
	}
}

func TestDouyinIdentityRootRequiresReviewedIdentifier(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><body>
<div class="container-live" style="display:flex;align-items:center;gap:12px;width:600px;height:70px">
  <div class="avatar-live"><img alt="账号头像" width="48" height="48"
   src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48'%3E%3Crect width='48' height='48' fill='%23087ea4'/%3E%3C/svg%3E"></div>
  <div class="name-live">real-douyin-creator</div>
</div></body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["douyin"])
	if err != nil {
		t.Fatal(err)
	}
	var withoutIdentifier domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &withoutIdentifier)); err != nil {
		t.Fatal(err)
	}
	if withoutIdentifier.IdentityVerified {
		t.Fatalf("douyin identity without reviewed identifier was accepted: %+v", withoutIdentifier)
	}
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`(() => {
  const identifier = document.createElement('div');
  identifier.className = 'unique_id-live';
  identifier.textContent = 'douyin-id-123';
  document.querySelector('.container-live').appendChild(identifier);
})()`, nil)); err != nil {
		t.Fatal(err)
	}
	var complete domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &complete)); err != nil {
		t.Fatal(err)
	}
	if !complete.IdentityVerified || complete.Name != "real-douyin-creator" || complete.AccountIdentifier != "douyin-id-123" {
		t.Fatalf("complete douyin reviewed identity was rejected: %+v", complete)
	}
}

func TestCSDNIdentityUsesReviewedBlogSubdomainHandle(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><body>
<section class="user-info-box" style="display:flex;gap:16px;width:640px;height:122px">
  <a class="avatar-box"><img alt="账号头像" width="70" height="70"
   src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='70' height='70'%3E%3Crect width='70' height='70' fill='%23087ea4'/%3E%3C/svg%3E"></a>
  <div class="info-box"><p class="name">CSDN creator</p></div>
</section></body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["csdn"])
	if err != nil {
		t.Fatal(err)
	}
	var withoutHandle domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &withoutHandle)); err != nil {
		t.Fatal(err)
	}
	if withoutHandle.IdentityVerified {
		t.Fatalf("csdn identity without reviewed public handle was accepted: %+v", withoutHandle)
	}
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`document.querySelector('.avatar-box').href = 'https://cpt1024.blog.csdn.net/'`, nil)); err != nil {
		t.Fatal(err)
	}
	var complete domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &complete)); err != nil {
		t.Fatal(err)
	}
	if !complete.IdentityVerified || complete.Name != "CSDN creator" || complete.AccountIdentifier != "cpt1024" {
		t.Fatalf("complete csdn reviewed identity was rejected: %+v", complete)
	}
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`document.querySelector('.avatar-box').href = 'https://blog.csdn.net/unrelated/extra'`, nil)); err != nil {
		t.Fatal(err)
	}
	var unrelated domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &unrelated)); err != nil {
		t.Fatal(err)
	}
	if unrelated.IdentityVerified {
		t.Fatalf("non-handle csdn link was accepted: %+v", unrelated)
	}
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`document.querySelector('.avatar-box').href = 'https://blog.csdn.net/cpt1024'`, nil)); err != nil {
		t.Fatal(err)
	}
	var canonicalPath domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &canonicalPath)); err != nil {
		t.Fatal(err)
	}
	if !canonicalPath.IdentityVerified || canonicalPath.AccountIdentifier != "cpt1024" {
		t.Fatalf("canonical csdn path handle was rejected: %+v", canonicalPath)
	}
}

func TestCSDNHomeCardUsesReviewedProfileDOM(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><body>
<div class="home-exp-user-card__head" style="display:flex;gap:16px;width:422px;height:58px">
  <a class="avatar-box" href="https://cpt1024.blog.csdn.net/"><img alt="账号头像" width="54" height="54"
   src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='54' height='54'%3E%3Crect width='54' height='54' fill='%23087ea4'/%3E%3C/svg%3E"></a>
  <div class="info-box"><div class="name-box"><p class="name">CSDN creator</p></div></div>
</div></body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["csdn"])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
		t.Fatal(err)
	}
	if !snapshot.IdentityVerified || snapshot.Name != "CSDN creator" || snapshot.AccountIdentifier != "cpt1024" {
		t.Fatalf("current csdn home card identity was rejected: %+v", snapshot)
	}
}

func TestCSDNEditorUsesReviewedSessionDOMWithoutInventingIdentity(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	html := `<!doctype html><html><body>
<header class="article-bar__user-box" style="display:block;width:120px;height:80px">
  <div class="user-option-box"><a>博文管理</a><a>我的博客</a><a>退出</a></div>
</header></body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(html)), chromedp.WaitReady("body")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	script, err := profileObservationScript(profileRules["csdn"])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot domSnapshot
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
		t.Fatal(err)
	}
	if !snapshot.SessionVerified || snapshot.IdentityVerified || snapshot.Name != "" || snapshot.AccountIdentifier != "" {
		t.Fatalf("csdn editor session evidence invented account identity: %+v", snapshot)
	}
	classified := classifyEvidence("csdn", nil, snapshot)
	if classified.AuthState != AuthLoggedIn || classified.ReasonCode != ReasonVerifiedSessionDOM || classified.EvidenceCode != EvidenceReviewedSessionDOM {
		t.Fatalf("csdn editor session evidence was rejected: %+v", classified)
	}
}

func TestCreatorEditorsUseReviewedSessionDOMWithoutInventingIdentity(t *testing.T) {
	requireRealBrowserFixture(t)
	tests := []struct {
		platform string
		html     string
	}{
		{
			platform: "douyin",
			html:     `<nav class="douyin-creator-master-layout-sider-children" style="display:block;width:240px;height:400px">作品管理</nav>`,
		},
		{
			platform: "wechat_video",
			html:     `<ul class="finder-ui-desktop-menu finder-ui-desktop-menu_global" style="display:block;width:240px;height:400px"><li>内容管理</li></ul>`,
		},
		{
			platform: "wechat_mp",
			html:     `<a href="https://mp.weixin.qq.com/cgi-bin/appmsg?action=edit" style="display:block;width:160px;height:48px">文章</a>`,
		},
	}
	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
			allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
			defer allocatorCancel()
			browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
			defer browserCancel()
			page := `<!doctype html><html><body>` + test.html + `</body></html>`
			if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(page)), chromedp.WaitReady("body")); err != nil {
				t.Skipf("isolated Chromium fixture unavailable: %v", err)
			}
			script, err := profileObservationScript(profileRules[test.platform])
			if err != nil {
				t.Fatal(err)
			}
			var snapshot domSnapshot
			if err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &snapshot)); err != nil {
				t.Fatal(err)
			}
			if !snapshot.SessionVerified || snapshot.IdentityVerified || snapshot.Name != "" || snapshot.AccountIdentifier != "" {
				t.Fatalf("%s creator session evidence is invalid: %+v", test.platform, snapshot)
			}
			classified := classifyEvidence(test.platform, nil, snapshot)
			if classified.AuthState != AuthLoggedIn || classified.ReasonCode != ReasonVerifiedSessionDOM || classified.EvidenceCode != EvidenceReviewedSessionDOM {
				t.Fatalf("%s creator session evidence was rejected: %+v", test.platform, classified)
			}
		})
	}
}

func TestCallerCancellationIsAppliedOnlyToActions(t *testing.T) {
	callerCtx, callerCancel := context.WithCancel(context.Background())
	ownerCtx, ownerCancel := context.WithCancel(context.Background())
	defer ownerCancel()
	actionCtx, actionCancel := actionContext(ownerCtx, callerCtx, time.Minute)
	defer actionCancel()
	callerCancel()
	select {
	case <-actionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not stop CDP action")
	}
	select {
	case <-ownerCtx.Done():
		t.Fatal("caller cancellation propagated to existing target ownership")
	default:
	}
}

func TestCallerCancellationDoesNotCloseIsolatedExistingTarget(t *testing.T) {
	requireRealBrowserFixture(t)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(t.TempDir()))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html,<title>owned-fixture</title>")); err != nil {
		t.Skipf("isolated Chromium fixture unavailable: %v", err)
	}
	owner := chromedp.FromContext(browserCtx)
	if owner == nil || owner.Target == nil || owner.Target.TargetID == "" {
		t.Fatal("isolated fixture target was not created")
	}
	targetID := owner.Target.TargetID

	tabCtx, tabCancel := chromedp.NewContext(browserCtx, chromedp.WithTargetID(targetID))
	callerCtx, callerCancel := context.WithCancel(context.Background())
	actionCtx, actionCancel := actionContext(tabCtx, callerCtx, 10*time.Second)
	attached := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			close(attached)
			<-ctx.Done()
			return ctx.Err()
		}))
	}()
	select {
	case <-attached:
	case <-time.After(5 * time.Second):
		t.Fatal("isolated existing target was not attached")
	}
	callerCancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled target action did not stop")
	}
	actionCancel()
	detachExistingTarget(tabCtx, tabCancel)

	targets, err := chromedp.Targets(browserCtx)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range targets {
		if info.TargetID == targetID {
			return
		}
	}
	t.Fatal("caller cancellation closed the existing target")
}

func TestObservePassiveRemoteAllocatorSmokeAndCancellationKeepsTarget(t *testing.T) {
	requireRealBrowserFixture(t)
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<main><div class="name-box">unpaired-content-author</div></main>
<div class="base">
  <div class="account-name">remote-fixture-creator</div>
  <div class="avatar"><img alt="账号头像" width="48" height="48"
   src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48'%3E%3Crect width='48' height='48' fill='%23087ea4'/%3E%3C/svg%3E">
  </div>
  <div class="others description-text"><div>小红书账号: remote-fixture-id</div><div></div><div>fixture</div></div>
</div>
</body></html>`))
	}))
	defer fixture.Close()
	fixtureURL, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal(err)
	}

	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	debugPort := portListener.Addr().(*net.TCPAddr).Port
	_ = portListener.Close()
	if debugPort >= 15101 && debugPort <= 15105 {
		t.Fatal("isolated test selected a protected live CDP port")
	}

	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(t.TempDir()),
		chromedp.Flag("remote-debugging-address", "127.0.0.1"),
		chromedp.Flag("remote-debugging-port", strconv.Itoa(debugPort)),
		chromedp.Flag("host-resolver-rules", "MAP creator.xiaohongshu.com 127.0.0.1"),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("proxy-server", "direct://"),
		chromedp.Flag("proxy-bypass-list", "*"),
	)
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	creatorURL := "https://creator.xiaohongshu.com:" + fixtureURL.Port() + "/creator/home"
	if err := chromedp.Run(browserCtx, chromedp.Navigate(creatorURL), chromedp.WaitReady("body")); err != nil {
		t.Fatalf("start isolated remote fixture: %v", err)
	}
	owner := chromedp.FromContext(browserCtx)
	if owner == nil || owner.Target == nil {
		t.Fatal("isolated remote fixture has no target")
	}
	targetID := owner.Target.TargetID

	observed, err := ObservePassive(context.Background(), "xhs", debugPort)
	if err != nil {
		t.Fatalf("ObservePassive remote smoke: %v", err)
	}
	if observed.AuthState != AuthLoggedIn || observed.Name != "remote-fixture-creator" || len(observed.AvatarPNG) == 0 {
		t.Fatalf("observation=%+v avatarBytes=%d", observed, len(observed.AvatarPNG))
	}

	quick, err := ObserveDOMPassive(context.Background(), "xhs", debugPort)
	if err != nil {
		t.Fatalf("ObserveDOMPassive remote smoke: %v", err)
	}
	if quick.AuthState != AuthLoggedIn || quick.Name != "remote-fixture-creator" || len(quick.AvatarPNG) != 0 {
		t.Fatalf("quick observation=%+v avatarBytes=%d", quick, len(quick.AvatarPNG))
	}

	// Slow only this isolated fixture's selector calls so cancellation occurs
	// after the RemoteAllocator has attached to the existing target.
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`(() => {
  const original = document.querySelectorAll.bind(document);
  document.querySelectorAll = (selector) => {
    const until = Date.now() + 120;
    while (Date.now() < until) {}
    return original(selector);
  };
})()`, nil)); err != nil {
		t.Fatal(err)
	}
	requestCtx, requestCancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, observeErr := ObservePassive(requestCtx, "xhs", debugPort)
		done <- observeErr
	}()
	time.Sleep(150 * time.Millisecond)
	requestCancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled ObservePassive did not return")
	}

	targets, err := chromedp.Targets(browserCtx)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range targets {
		if info.TargetID == targetID {
			return
		}
	}
	t.Fatal("ObservePassive cancellation closed the existing target")
}

func TestPassiveObserverSourceCannotNavigateCreateOrCloseTargets(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("passive.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, forbidden := range []string{"chromedp.Navigate", "target.CreateTarget", "target.CloseTarget", "page.Navigate", "NewRemoteAllocator(ctx"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("passive observer must not contain %s", forbidden)
		}
	}
	if !strings.Contains(text, "NewRemoteAllocator(ownershipCtx") || !strings.Contains(text, `Target.TargetID = ""`) {
		t.Fatal("passive observer must own remote allocation independently and clear target ownership before cancel")
	}
	if !strings.Contains(text, "chromedp.NewContext(allocCtx)") || strings.Contains(text, "context.WithCancel(allocCtx)") {
		t.Fatal("passive observer must initialize the remote browser through a chromedp context")
	}
}
