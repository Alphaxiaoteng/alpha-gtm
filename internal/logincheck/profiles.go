package logincheck

import "sort"

type platformProfileRule struct {
	Hosts                      []string
	AccountRootSelectors       []string
	AccountRootMaxHeight       float64
	IdentityMaxCenterDistance  float64
	RequireIdentifier          bool
	NameSelectors              []string
	PrefixedNameSelectors      []string
	HiddenNameSelectors        []string
	NamePrefixes               []string
	AvatarSelectors            []string
	AvatarElementSelectors     []string
	SessionAvatarSelectors     []string
	SessionAvatarElements      []string
	SessionProofSelectors      []string
	RequiredSessionProofs      int
	AvatarResourceHostSuffixes []string
	IdentifierSelectors        []string
	IdentifierLinkSelectors    []string
	IdentifierLinkHostSuffix   string
	IdentifierLinkPathHost     string
	SessionContainers          []string
	SessionTexts               []string
	LoginPaths                 []string
	LoginContainers            []string
	LoginTexts                 []string
	DialogSelectors            []string
	ExpiredTexts               []string
	CheckpointTexts            []string
}

func metaBusinessSuiteProfileRule(platformBadge string) platformProfileRule {
	assetSelector := "[role='combobox'] div:has(> div > img[alt='" + platformBadge + "']) > div > img[alt='']"
	return platformProfileRule{
		Hosts:                      []string{"business.facebook.com", "www.instagram.com", "www.facebook.com"},
		AccountRootSelectors:       []string{"header section"},
		NameSelectors:              []string{"[data-testid='user-name']", "a[aria-label*='profile']", "a[aria-label*='个人资料']", "h1", "[role='heading']"},
		AvatarSelectors:            []string{"img[alt*='profile picture']", "img[alt*='头像']", "img"},
		SessionAvatarSelectors:     []string{assetSelector},
		SessionProofSelectors:      []string{assetSelector},
		AvatarResourceHostSuffixes: []string{".fbcdn.net", ".cdninstagram.com"},
		SessionContainers:          []string{"nav", "[role='navigation']"},
		SessionTexts:               []string{"Home", "首页", "Content", "内容", "Search", "搜索"},
		LoginPaths:                 []string{"/login", "/accounts/login", "/accounts/emailsignup"},
		LoginContainers:            []string{"form:has(input[name='email'])", "form:has(input[name='username'])"},
		LoginTexts:                 []string{"Log in", "登录"},
		DialogSelectors:            []string{"[role='dialog']", "[class*='dialog']", "[class*='modal']"},
	}
}

func combinedMetaBusinessSuiteProfileRule() platformProfileRule {
	rule := metaBusinessSuiteProfileRule("Facebook")
	instagramSelector := "[role='combobox'] div:has(> div > img[alt='Instagram']) > div > img[alt='']"
	rule.SessionAvatarSelectors = append(rule.SessionAvatarSelectors, instagramSelector)
	rule.SessionProofSelectors = append(rule.SessionProofSelectors, instagramSelector)
	// Business Suite shows only the selected destination in a single-target
	// composer, so either reviewed Meta asset badge is sufficient.
	rule.RequiredSessionProofs = 1
	return rule
}

// profileRules are intentionally narrow, reviewed selectors for the six
// creator consoles currently supported by account observation. Generic header
// text and arbitrary small images are excluded because they previously saved
// platform logos and login QR codes as account identity.
var profileRules = map[string]platformProfileRule{
	"xhs": {
		Hosts:                []string{"creator.xiaohongshu.com"},
		AccountRootSelectors: []string{".base", ".user-info"},
		RequireIdentifier:    true,
		NameSelectors:        []string{".account-name", ".name-box"},
		AvatarSelectors:      []string{".avatar img", "img.user_avatar"},
		IdentifierSelectors:  []string{".others.description-text > div:first-child"},
		LoginPaths:           []string{"/login"},
		LoginContainers:      []string{".login-container", "[class*='login-container']"},
		LoginTexts:           []string{"扫码登录", "手机号登录"},
		DialogSelectors:      []string{"[role='dialog']", "[class*='dialog']", "[class*='modal']"},
	},
	"douyin": {
		Hosts:                      []string{"creator.douyin.com"},
		AccountRootSelectors:       []string{"[class^='container-']"},
		AccountRootMaxHeight:       160,
		IdentityMaxCenterDistance:  180,
		RequireIdentifier:          true,
		NameSelectors:              []string{"[class^='name-']"},
		AvatarSelectors:            []string{"[class^='avatar-'] img"},
		SessionAvatarElements:      []string{"[class*='avatar-wrapper-'] [class^='avatar-']"},
		AvatarResourceHostSuffixes: []string{".douyinpic.com"},
		IdentifierSelectors:        []string{"[class^='unique_id-']"},
		SessionContainers:          []string{".douyin-creator-master-layout-sider-children", "button"},
		SessionTexts:               []string{"作品管理", "作品发布"},
		LoginPaths:                 []string{"/login"},
		LoginContainers:            []string{"[class*='login-container']", "[class^='login-']"},
		LoginTexts:                 []string{"扫码登录", "手机号登录", "验证码登录"},
		DialogSelectors:            []string{"[role='dialog']", "[class*='dialog']", "[class*='modal']"},
	},
	"wechat_mp": {
		Hosts:                []string{"mp.weixin.qq.com"},
		AccountRootSelectors: []string{".weui-desktop-person_info"},
		NameSelectors:        []string{".weui-desktop_name"},
		AvatarSelectors:      []string{"img.weui-desktop-account__img"},
		SessionContainers:    []string{".new-creation__menu-item", "a[href*='/cgi-bin/appmsg']"},
		SessionTexts:         []string{"文章"},
		LoginPaths:           []string{"/cgi-bin/loginpage", "/login"},
		LoginContainers:      []string{".login__type__container", ".login_panel", "[class*='login__']"},
		LoginTexts:           []string{"使用微信扫一扫登录", "微信扫码登录"},
		DialogSelectors:      []string{"[role='dialog']", ".weui-desktop-dialog", "[class*='dialog']"},
	},
	"wechat_video": {
		Hosts:                      []string{"channels.weixin.qq.com"},
		AccountRootSelectors:       []string{".finder-info-container"},
		NameSelectors:              []string{".finder-nickname"},
		AvatarSelectors:            []string{"img[alt='视频号头像']"},
		SessionAvatarSelectors:     []string{".account-info img.avatar"},
		AvatarResourceHostSuffixes: []string{"wx.qlogo.cn"},
		SessionContainers:          []string{".finder-ui-desktop-menu"},
		SessionTexts:               []string{"内容管理"},
		LoginPaths:                 []string{"/login"},
		LoginContainers:            []string{"[class*='login-container']", "[class*='qrcode']"},
		LoginTexts:                 []string{"请使用微信扫码登录", "微信扫码登录"},
		DialogSelectors:            []string{"[role='dialog']", "[class*='dialog']", "[class*='modal']"},
	},
	"kuaishou": {
		Hosts:                []string{"cp.kuaishou.com", "creator.kuaishou.com"},
		AccountRootSelectors: []string{".user-info-dpd", ".user-info"},
		NameSelectors:        []string{".user-info-dpd"},
		AvatarSelectors:      []string{".user-info-avatar img"},
		LoginPaths:           []string{"/login"},
		LoginContainers:      []string{"a.login", ".login-container", "[class*='login-container']"},
		LoginTexts:           []string{"立即登录", "扫码登录", "手机号登录"},
		DialogSelectors:      []string{"[role='dialog']", "[class*='dialog']", "[class*='modal']"},
	},
	"csdn": {
		Hosts: []string{"mp.csdn.net", "editor.csdn.net", "passport.csdn.net"},
		// Current CSDN creator home exposes the paired profile controls in this
		// compact card; retain the older root for editor pages.
		// ponytail: add future variants only after a read-only DOM review.
		AccountRootSelectors:       []string{".home-exp-user-card__head", ".user-info-box"},
		AccountRootMaxHeight:       180,
		IdentityMaxCenterDistance:  260,
		RequireIdentifier:          true,
		NameSelectors:              []string{".info-box .name"},
		AvatarSelectors:            []string{".avatar-box img"},
		AvatarResourceHostSuffixes: []string{"i-avatar.csdnimg.cn"},
		IdentifierLinkSelectors:    []string{"a.avatar-box[href]"},
		IdentifierLinkHostSuffix:   ".blog.csdn.net",
		IdentifierLinkPathHost:     "blog.csdn.net",
		SessionContainers:          []string{".article-bar__user-box"},
		SessionTexts:               []string{"退出"},
		LoginPaths:                 []string{"/login"},
		LoginContainers:            []string{".login-box", ".main-login", "#passportbox"},
		LoginTexts:                 []string{"账号登录", "扫码登录"},
		DialogSelectors:            []string{"[role='dialog']", "[class*='dialog']", "[class*='modal']"},
	},
	// Facebook and Instagram are assets under one Meta login identity. Prefer
	// the Facebook portrait because Instagram may expose Meta's grey default
	// silhouette when that asset has no dedicated picture.
	"facebook":  combinedMetaBusinessSuiteProfileRule(),
	"instagram": combinedMetaBusinessSuiteProfileRule(),
	"meta":      combinedMetaBusinessSuiteProfileRule(),
	"tiktok": {
		Hosts:                     []string{"www.tiktok.com"},
		AccountRootSelectors:      []string{"[data-e2e='profile-icon']", "[class*='UserInfo']", "[class*='user-info']", "body"},
		IdentityMaxCenterDistance: 240,
		NameSelectors:             []string{"[data-e2e='profile-username']", "[class*='UserName']", "[class*='user-name']"},
		HiddenNameSelectors:       []string{"a[href^='/@']"},
		IdentifierLinkSelectors:   []string{"a[href^='/@']"},
		IdentifierLinkPathHost:    "www.tiktok.com",
		AvatarSelectors: []string{
			"img[data-tt='components_Avatar_AvatarImg']",
			"img[data-e2e*='avatar']",
			"img[src*='tiktokx-cropcenter']",
		},
		SessionAvatarSelectors: []string{
			"[data-tt='Header_NewHeader_Clickable'] img[data-tt='components_Avatar_AvatarImg']",
			"[data-tt='NewHome_UserInfo_FlexRow'] img[data-tt='components_Avatar_AvatarImg']",
		},
		AvatarResourceHostSuffixes: []string{".tiktokcdn-us.com"},
		SessionContainers:          []string{"[data-e2e='creator-center-sidebar']", "nav", "button"},
		SessionTexts:               []string{"Manage posts", "管理帖子", "Analytics", "数据分析", "上传"},
		LoginPaths:                 []string{"/login"},
		LoginContainers:            []string{"[data-e2e*='login']", "form"},
		LoginTexts:                 []string{"Log in", "登录"},
		DialogSelectors:            []string{"[role='dialog']", "[class*='dialog']", "[class*='modal']"},
	},
	"youtube": {
		Hosts:                     []string{"studio.youtube.com"},
		AccountRootSelectors:      []string{"ytcp-navigation-drawer #entity", "ytcp-navigation-drawer #account-info", "#account-info", "ytcp-navigation-drawer", "body"},
		IdentityMaxCenterDistance: 280,
		NameSelectors:             []string{"#entity-name", "#channel-name", ".channel-name"},
		PrefixedNameSelectors:     []string{"[aria-label^='Your channel']", "[aria-label^='您的频道']", "[aria-label^='你的频道']", ":scope", "div"},
		NamePrefixes:              []string{"Your channel", "您的频道", "你的频道"},
		AvatarSelectors:           []string{"#avatar img", "ytcp-navigation-drawer #avatar img", "#avatar.ytcp-channel-header-avatar img", "ytcp-entity-page-header #avatar img", "yt-img-shadow img", "img.avatar", "ytcp-navigation-drawer img", "img"},
		SessionContainers:         []string{"ytcp-navigation-drawer"},
		SessionTexts:              []string{"Channel content", "频道内容", "Analytics", "数据分析"},
		LoginPaths:                []string{"/signin"},
		LoginContainers:           []string{"a[href*='ServiceLogin']", "form"},
		LoginTexts:                []string{"Sign in", "登录"},
		DialogSelectors:           []string{"[role='dialog']", "tp-yt-paper-dialog"},
	},
}

var commonExpiredTexts = []string{"登录已过期", "会话已过期", "请重新登录"}
var commonCheckpointTexts = []string{"安全验证", "验证身份", "账号异常", "登录环境异常"}

func SupportedObservedPlatforms() []string {
	result := make([]string, 0, len(profileRules))
	for code := range profileRules {
		result = append(result, code)
	}
	sort.Strings(result)
	return result
}
