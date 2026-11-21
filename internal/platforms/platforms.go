package platforms

import (
	"fmt"
	"sort"
)

const KuaishouLoginURL = "https://passport.kuaishou.com/pc/account/login/?sid=kuaishou.web.cp.api&callback=https%3A%2F%2Fcp.kuaishou.com%2Frest%2Finfra%2Fsts%3FfollowUrl%3Dhttps%253A%252F%252Fcp.kuaishou.com%252Fprofile%26setRootDomain%3Dtrue"

// Category groups platforms by primary content form so the account library can
// be filtered without hardcoding code lists in the UI.
const (
	CategoryMultimedia = "multimedia"
	CategoryArticle    = "article"
	CategoryEcommerce  = "ecommerce"
)

// platformCategories maps every builtin code to its primary form. Anything
// absent (custom, meta alias) keeps an empty category and renders as "other".
var platformCategories = map[string]string{
	"instagram": CategoryMultimedia, "facebook": CategoryMultimedia,
	"xhs": CategoryMultimedia, "douyin": CategoryMultimedia,
	"wechat_video": CategoryMultimedia, "kuaishou": CategoryMultimedia,
	"youtube": CategoryMultimedia, "tiktok": CategoryMultimedia,
	"x": CategoryMultimedia, "weishi": CategoryMultimedia,
	"bilibili": CategoryMultimedia, "acfun": CategoryMultimedia,
	"sina":      CategoryMultimedia,
	"wechat_mp": CategoryArticle, "toutiao": CategoryArticle,
	"omtencent": CategoryArticle, "baijiahao": CategoryArticle,
	"jianshu": CategoryArticle, "zhihu": CategoryArticle,
	"juejin": CategoryArticle, "csdn": CategoryArticle,
	"douyin_shop": CategoryEcommerce, "taobao": CategoryEcommerce,
	"tiktok_shop": CategoryEcommerce, "shopee": CategoryEcommerce,
	"shopify": CategoryEcommerce, "amazon_seller": CategoryEcommerce,
	"qianchuan": CategoryEcommerce, "jd_shop": CategoryEcommerce,
	"pinduoduo_shop": CategoryEcommerce, "xhs_shop": CategoryEcommerce,
	"kuaishou_shop": CategoryEcommerce, "wechat_shop": CategoryEcommerce,
	"alibaba_1688": CategoryEcommerce, "aliexpress": CategoryEcommerce,
	"xianyu": CategoryEcommerce, "pdd": CategoryEcommerce,
}

var metaLoginAlias = Platform{
	Code: "meta", Name: "Meta", Icon: "Meta",
	LoginURL: "https://business.facebook.com/latest/home", ManagerURL: "https://business.facebook.com/latest/home",
	SupportsGraphText: true, SupportsVideo: true,
}

type Platform struct {
	Code              string
	Name              string
	Icon              string
	LoginURL          string
	ManagerURL        string
	SupportsArticle   bool
	SupportsGraphText bool
	SupportsVideo     bool
	Sort              int
	SupportsPublish   bool
	Category          string
}

// PublishReadiness describes the operational boundary shown to users before a
// job is created. It is intentionally conservative: a handoff is useful, but
// is not evidence that a platform can be published automatically.
type PublishReadiness struct {
	Mode                 string
	CanCreateJob         bool
	CanAttemptAutoSubmit bool
	Summary              string
}

// RequiredPublishAssetType declares the target type that a platform adapter
// needs before it can create a usable publish job. Most browser-handoff
// platforms publish from the account container itself; Meta is different
// because its adapters require a selected Page or Instagram account.
func RequiredPublishAssetType(code string) (string, bool) {
	switch code {
	case "facebook":
		return "facebook_page", true
	case "instagram":
		return "instagram_account", true
	default:
		return "", false
	}
}

// IsPublishTargetAssetType keeps the Meta target constraint at every entry
// point while preserving the existing generic account asset model elsewhere.
func IsPublishTargetAssetType(platformCode, assetType string) bool {
	required, constrained := RequiredPublishAssetType(platformCode)
	return !constrained || assetType == required
}

func Readiness(code string) PublishReadiness {
	if code == "meta" {
		return PublishReadiness{Mode: "browser_automation", CanCreateJob: true, CanAttemptAutoSubmit: true, Summary: "Meta 登录容器通过已确认的 Facebook Page 或 Instagram 资产创建发布任务。"}
	}
	if !CanPublish(code) {
		return PublishReadiness{Mode: "unavailable", Summary: "该平台没有可用的创作后台路径，不能创建发布任务。"}
	}
	switch code {
	case "instagram", "facebook":
		return PublishReadiness{Mode: "browser_automation", CanCreateJob: true, CanAttemptAutoSubmit: true, Summary: "Meta 浏览器流程可尝试自动提交；无明确成功信号时会转人工确认。"}
	case "xhs":
		return PublishReadiness{
			Mode:                 "browser_automation",
			CanCreateJob:         true,
			CanAttemptAutoSubmit: false,
			Summary:              "小红书仅支持自动准备发布计划；最终提交仍需独立审批，当前 submit adapter 未验证，不代表自动发布成功。",
		}
	case "douyin", "wechat_video", "wechat_mp", "youtube", "tiktok", "kuaishou", "csdn":
		return PublishReadiness{
			Mode:                 "browser_automation",
			CanCreateJob:         true,
			CanAttemptAutoSubmit: true,
			Summary:              "受控浏览器流程仅允许单目标冻结计划经 submit-bound 独立审批点击一次；直接或未冻结 live 请求保持 fail-closed，点击后仍须创作者后台核验。",
		}
	case "x":
		return PublishReadiness{Mode: "api_credentials", CanCreateJob: true, Summary: "原生 API Connector 尚未接入；当前只允许演练，不发送内容、媒体路径或凭证引用。"}
	default:
		return PublishReadiness{Mode: "manual_handoff", CanCreateJob: true, Summary: "将打开创作后台并转人工接管；不会自动点击发布。"}
	}
}

// Turbo Push's 20 formal platforms, custom HTTP, and Alpha's Meta pair. A
// platform appearing here means it can be managed and logged into; publishing
// is enabled only after a native/API adapter has a contract and fixtures.
var builtinPlatforms = []Platform{
	{Code: "instagram", Name: "Instagram", Icon: "📷", LoginURL: "https://www.instagram.com", ManagerURL: "https://business.facebook.com", SupportsArticle: false, SupportsGraphText: true, SupportsVideo: true, Sort: 1, SupportsPublish: true},
	{Code: "facebook", Name: "Facebook", Icon: "👍", LoginURL: "https://www.facebook.com", ManagerURL: "https://business.facebook.com", SupportsArticle: false, SupportsGraphText: true, SupportsVideo: true, Sort: 2, SupportsPublish: true},
	{Code: "xhs", Name: "小红书", Icon: "📕", LoginURL: "https://www.xiaohongshu.com", ManagerURL: "https://creator.xiaohongshu.com", SupportsArticle: false, SupportsGraphText: true, SupportsVideo: true, Sort: 3, SupportsPublish: true},
	{Code: "douyin", Name: "抖音", Icon: "🎵", LoginURL: "https://www.douyin.com", ManagerURL: "https://creator.douyin.com", SupportsArticle: false, SupportsGraphText: true, SupportsVideo: true, Sort: 4, SupportsPublish: true},
	{Code: "wechat_video", Name: "视频号", Icon: "💬", LoginURL: "https://channels.weixin.qq.com", ManagerURL: "https://channels.weixin.qq.com", SupportsArticle: false, SupportsGraphText: false, SupportsVideo: true, Sort: 5, SupportsPublish: true},
	{Code: "kuaishou", Name: "快手", Icon: "🎬", LoginURL: KuaishouLoginURL, ManagerURL: "https://cp.kuaishou.com", SupportsArticle: false, SupportsGraphText: true, SupportsVideo: true, Sort: 6, SupportsPublish: true},
	{Code: "wechat_mp", Name: "微信公众号", Icon: "📰", LoginURL: "https://mp.weixin.qq.com/cgi-bin/loginpage?url=%2Fcgi-bin%2Fhome", ManagerURL: "https://mp.weixin.qq.com/", SupportsArticle: true, SupportsGraphText: false, SupportsVideo: false, Sort: 7, SupportsPublish: true},
	{Code: "youtube", Name: "YouTube", Icon: "▶️", LoginURL: "https://www.youtube.com", ManagerURL: "https://studio.youtube.com", SupportsArticle: false, SupportsGraphText: false, SupportsVideo: true, Sort: 8, SupportsPublish: true},
	{Code: "tiktok", Name: "TikTok", Icon: "🎞️", LoginURL: "https://www.tiktok.com", ManagerURL: "https://www.tiktok.com/tiktokstudio", SupportsArticle: false, SupportsGraphText: false, SupportsVideo: true, Sort: 9, SupportsPublish: true},
	{Code: "x", Name: "X", Icon: "𝕏", LoginURL: "https://x.com", ManagerURL: "https://x.com", SupportsArticle: false, SupportsGraphText: true, SupportsVideo: true, Sort: 10, SupportsPublish: true},
	{Code: "toutiao", Name: "今日头条", Icon: "📰", LoginURL: "https://www.toutiao.com", ManagerURL: "https://mp.toutiao.com", SupportsArticle: true, SupportsGraphText: true, SupportsVideo: true, Sort: 11},
	{Code: "omtencent", Name: "腾讯内容开放平台", Icon: "企鹅", LoginURL: "https://om.qq.com", ManagerURL: "https://om.qq.com", SupportsArticle: true, SupportsGraphText: false, SupportsVideo: true, Sort: 12},
	{Code: "weishi", Name: "微视", Icon: "🎞️", LoginURL: "https://weishi.qq.com", ManagerURL: "https://weishi.qq.com", SupportsArticle: false, SupportsGraphText: false, SupportsVideo: true, Sort: 13},
	{Code: "bilibili", Name: "哔哩哔哩", Icon: "📺", LoginURL: "https://www.bilibili.com", ManagerURL: "https://member.bilibili.com/platform/home", SupportsArticle: true, SupportsGraphText: false, SupportsVideo: true, Sort: 14},
	{Code: "acfun", Name: "AcFun", Icon: "🅰️", LoginURL: "https://www.acfun.cn", ManagerURL: "https://member.acfun.cn", SupportsArticle: true, SupportsGraphText: false, SupportsVideo: true, Sort: 15},
	{Code: "baijiahao", Name: "百家号", Icon: "百", LoginURL: "https://baijiahao.baidu.com", ManagerURL: "https://baijiahao.baidu.com", SupportsArticle: true, SupportsGraphText: true, SupportsVideo: true, Sort: 16},
	{Code: "sina", Name: "新浪微博", Icon: "微博", LoginURL: "https://weibo.com", ManagerURL: "https://weibo.com", SupportsArticle: true, SupportsGraphText: true, SupportsVideo: true, Sort: 17},
	{Code: "jianshu", Name: "简书", Icon: "简", LoginURL: "https://www.jianshu.com", ManagerURL: "https://www.jianshu.com", SupportsArticle: true, SupportsGraphText: false, SupportsVideo: false, Sort: 18},
	{Code: "zhihu", Name: "知乎", Icon: "知", LoginURL: "https://www.zhihu.com", ManagerURL: "https://www.zhihu.com/creator", SupportsArticle: true, SupportsGraphText: true, SupportsVideo: true, Sort: 19},
	{Code: "juejin", Name: "掘金", Icon: "掘", LoginURL: "https://juejin.cn", ManagerURL: "https://juejin.cn/creator", SupportsArticle: true, SupportsGraphText: true, SupportsVideo: false, Sort: 20},
	{Code: "csdn", Name: "CSDN", Icon: "C", LoginURL: "https://www.csdn.net", ManagerURL: "https://mp.csdn.net", SupportsArticle: true, SupportsGraphText: false, SupportsVideo: false, Sort: 21},
	{Code: "pdd", Name: "拼多多", Icon: "拼", LoginURL: "https://mms.pinduoduo.com", ManagerURL: "https://mms.pinduoduo.com", SupportsArticle: false, SupportsGraphText: false, SupportsVideo: true, Sort: 22},
	{Code: "custom", Name: "自定义平台", Icon: "⚙️", SupportsArticle: true, SupportsGraphText: true, SupportsVideo: true, Sort: 23},
	{Code: "douyin_shop", Name: "抖音电商", Icon: "抖店", LoginURL: "https://fxg.jinritemai.com/", ManagerURL: "https://fxg.jinritemai.com/", Sort: 24},
	{Code: "taobao", Name: "淘宝", Icon: "淘宝", LoginURL: "https://myseller.taobao.com/home.htm", ManagerURL: "https://myseller.taobao.com/home.htm", Sort: 25},
	{Code: "tiktok_shop", Name: "TikTok Shop", Icon: "TTS", LoginURL: "https://seller.tiktokglobalshop.com/account/login", ManagerURL: "https://seller.tiktokglobalshop.com/", Sort: 26},
	{Code: "shopee", Name: "Shopee", Icon: "S", LoginURL: "https://seller.shopee.cn/", ManagerURL: "https://seller.shopee.cn/", Sort: 27},
	{Code: "shopify", Name: "Shopify", Icon: "S", LoginURL: "https://www.shopify.com/login", ManagerURL: "https://admin.shopify.com/", Sort: 28},
	{Code: "amazon_seller", Name: "Amazon Seller", Icon: "a", LoginURL: "https://sellercentral.amazon.com/", ManagerURL: "https://sellercentral.amazon.com/", Sort: 29},
	{Code: "qianchuan", Name: "巨量千川", Icon: "千", LoginURL: "https://qianchuan.jinritemai.com/", ManagerURL: "https://qianchuan.jinritemai.com/", Sort: 30},
	{Code: "jd_shop", Name: "京东店铺", Icon: "京", LoginURL: "https://login.shop.jd.com/", ManagerURL: "https://seller-v10.shop.jd.com/", Sort: 31},
	{Code: "pinduoduo_shop", Name: "拼多多店铺", Icon: "拼", LoginURL: "https://mms.pinduoduo.com/", ManagerURL: "https://mms.pinduoduo.com/", Sort: 32},
	{Code: "xhs_shop", Name: "小红书店铺", Icon: "红", LoginURL: "https://ark.xiaohongshu.com/merchant/login?from=xhsweb", ManagerURL: "https://ark.xiaohongshu.com/ark", Sort: 33},
	{Code: "kuaishou_shop", Name: "快手小店", Icon: "快", LoginURL: "https://login.kwaixiaodian.com/", ManagerURL: "https://s.kwaixiaodian.com/", Sort: 34},
	{Code: "wechat_shop", Name: "微信小店", Icon: "微", LoginURL: "https://channels.weixin.qq.com/shop", ManagerURL: "https://channels.weixin.qq.com/shop", Sort: 35},
	{Code: "alibaba_1688", Name: "1688", Icon: "1688", LoginURL: "https://work.1688.com/", ManagerURL: "https://work.1688.com/", Sort: 36},
	{Code: "aliexpress", Name: "AliExpress", Icon: "AE", LoginURL: "https://gsp.aliexpress.com/", ManagerURL: "https://gsp.aliexpress.com/", Sort: 37},
	{Code: "xianyu", Name: "闲鱼", Icon: "闲", LoginURL: "https://www.goofish.com/personal", ManagerURL: "https://www.goofish.com/personal", Sort: 38},
}

func All() []Platform {
	result := make([]Platform, len(builtinPlatforms))
	copy(result, builtinPlatforms)
	for index := range result {
		result[index].SupportsPublish = CanPublish(result[index].Code)
		if result[index].Category == "" {
			result[index].Category = platformCategories[result[index].Code]
		}
	}
	return result
}

// EgoLiteAll is the first-release product catalog. Older adapter code remains
// compiled for migration fixtures but is not offered for new Ego identities.
func EgoLiteAll() []Platform {
	enabled := map[string]bool{
		"xhs": true, "douyin": true,
		"kuaishou": true, "tiktok": true, "wechat_video": true,
		"youtube": true, "wechat_mp": true, "csdn": true,
		"acfun": true, "baijiahao": true, "bilibili": true,
		"jianshu": true, "juejin": true, "omtencent": true, "pdd": true,
		"sina": true, "toutiao": true, "weishi": true, "x": true, "zhihu": true,
		"douyin_shop": true, "taobao": true, "tiktok_shop": true,
		"shopee": true, "shopify": true, "amazon_seller": true,
		"qianchuan": true, "jd_shop": true, "pinduoduo_shop": true,
		"xhs_shop": true, "kuaishou_shop": true, "wechat_shop": true,
		"alibaba_1688": true, "aliexpress": true, "xianyu": true,
	}
	meta := metaLoginAlias
	meta.SupportsPublish = true
	meta.Category = CategoryMultimedia
	result := make([]Platform, 0, len(enabled)+1)
	result = append(result, meta)
	for _, platform := range builtinPlatforms {
		if !enabled[platform.Code] {
			continue
		}
		platform.SupportsPublish = CanPublish(platform.Code)
		if platform.Category == "" {
			platform.Category = platformCategories[platform.Code]
		}
		if platform.Code == "xhs" {
			// SupportsPublish is the legacy direct-publisher flag. XHS is
			// available through Readiness as prepare-only until submit is verified.
			platform.SupportsPublish = false
		}
		result = append(result, platform)
	}
	return result
}

func Find(code string) (Platform, error) {
	if code == metaLoginAlias.Code {
		return metaLoginAlias, nil
	}
	for _, p := range builtinPlatforms {
		if p.Code == code {
			p.SupportsPublish = CanPublish(p.Code)
			return p, nil
		}
	}
	return Platform{}, fmt.Errorf("platform %q not found", code)
}

func Codes() []string {
	codes := make([]string, 0, len(builtinPlatforms))
	for _, p := range builtinPlatforms {
		codes = append(codes, p.Code)
	}
	sort.Strings(codes)
	return codes
}

func LiteCodes() []string {
	return []string{"xhs", "douyin", "wechat_video", "kuaishou"}
}

// PublishCodes returns every target with a supported publish path. Most use a
// safe creator-console handoff; only adapters with verified success signals may
// ever transition to published automatically.
func PublishCodes() []string {
	return []string{
		"acfun", "baijiahao", "bilibili", "csdn", "douyin", "facebook",
		"instagram", "jianshu", "juejin", "kuaishou", "omtencent", "pdd",
		"sina", "tiktok", "toutiao", "wechat_mp", "wechat_video", "weishi",
		"x", "xhs", "youtube", "zhihu",
		"alibaba_1688", "aliexpress", "xianyu",
	}
}

func CanPublish(code string) bool {
	for _, c := range PublishCodes() {
		if c == code {
			return true
		}
	}
	return false
}
