package egopublish

import (
	"net/url"
	"strings"
)

const (
	PrepareAutomated     = "automated"
	PrepareManualHandoff = "manual_handoff"

	ScheduleLocalExecutionForbidden  = "EGO_SCHEDULE_LOCAL_EXECUTION_FORBIDDEN"
	ScheduleTimezoneInvalid          = "EGO_SCHEDULE_TIMEZONE_INVALID"
	ScheduleDSTAmbiguous             = "EGO_SCHEDULE_DST_AMBIGUOUS"
	ScheduleTimeInvalid              = "EGO_SCHEDULE_TIME_INVALID"
	ScheduleWorkflowNotReview        = "EGO_SCHEDULE_WORKFLOW_NOT_REVIEW"
	ScheduleHandoffSuperseded        = "EGO_SCHEDULE_HANDOFF_SUPERSEDED"
	ScheduleAccountSessionUnverified = "EGO_SCHEDULE_ACCOUNT_SESSION_UNVERIFIED"
	ScheduleReadbackMismatch         = "EGO_SCHEDULE_READBACK_MISMATCH"
	ScheduleResultUnknown            = "EGO_SCHEDULE_RESULT_UNKNOWN"
)

// Capability is the single source of truth for one platform/content-type
// boundary. Adapter support says that local code exists. Verified support is a
// separate, fail-closed live-acceptance gate: it is the only capability that
// may allow a final click or promote creator-manager evidence to success.
type Capability struct {
	ContentType                    string `json:"content_type"`
	ComposerURL                    string `json:"composer_url"`
	PrepareMode                    string `json:"prepare_mode"`
	PrepareAdapterSupported        bool   `json:"prepare_adapter_supported"`
	SubmitAdapterSupported         bool   `json:"submit_adapter_supported"`
	VerifyAdapterSupported         bool   `json:"verify_adapter_supported"`
	SubmitVerified                 bool   `json:"submit_verified"`
	VerifyVerified                 bool   `json:"verify_verified"`
	NativeScheduleAdapterSupported bool   `json:"native_schedule_adapter_supported"`
	NativeScheduleVerified         bool   `json:"native_schedule_verified"`
	NativeScheduleMinLeadMinutes   int    `json:"native_schedule_min_lead_minutes"`
}

type Manifest struct {
	PlatformCode string       `json:"platform_code"`
	Name         string       `json:"name"`
	CreatorURL   string       `json:"creator_url"`
	FirstRelease bool         `json:"first_release"`
	Capabilities []Capability `json:"capabilities"`
	Status       string       `json:"status"`
}

func automated(contentType, composerURL string, submitAdapterSupported, verifyAdapterSupported bool) Capability {
	return Capability{
		ContentType: contentType, ComposerURL: composerURL, PrepareMode: PrepareAutomated,
		PrepareAdapterSupported: true,
		SubmitAdapterSupported:  submitAdapterSupported, VerifyAdapterSupported: verifyAdapterSupported,
	}
}

func manual(contentType, handoffURL string) Capability {
	return Capability{ContentType: contentType, ComposerURL: handoffURL, PrepareMode: PrepareManualHandoff}
}

func nativeSchedule(capability Capability, leadMinutes int) Capability {
	capability.NativeScheduleAdapterSupported = true
	capability.NativeScheduleMinLeadMinutes = leadMinutes
	return capability
}

// The first ten manifests are the launch matrix. Extra catalog platforms stay
// explicitly manual; none may inflate the launch matrix count or inherit an
// automated submit/verification boundary.
var egoLiteManifests = []Manifest{
	{PlatformCode: "douyin", Name: "抖音", CreatorURL: "https://creator.douyin.com", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		nativeSchedule(automated("graph_text", "https://creator.douyin.com/creator-micro/content/upload?default-tab=3", true, true), 15),
		nativeSchedule(automated("video", "https://creator.douyin.com/creator-micro/content/post/video", true, true), 15),
	}},
	{PlatformCode: "wechat_video", Name: "视频号", CreatorURL: "https://channels.weixin.qq.com", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		nativeSchedule(automated("video", "https://channels.weixin.qq.com/platform/post/create", true, true), 15),
	}},
	{PlatformCode: "kuaishou", Name: "快手", CreatorURL: "https://cp.kuaishou.com", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		nativeSchedule(automated("graph_text", "https://cp.kuaishou.com/article/publish/video", true, true), 15),
		nativeSchedule(automated("video", "https://cp.kuaishou.com/article/publish/video", true, true), 15),
	}},
	{PlatformCode: "xhs", Name: "小红书", CreatorURL: "https://creator.xiaohongshu.com", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		automated("graph_text", "https://creator.xiaohongshu.com/publish/publish?from=menu&target=image", false, false),
		automated("video", "https://creator.xiaohongshu.com/publish/publish?from=menu&target=video", false, false),
	}},
	{PlatformCode: "tiktok", Name: "TikTok", CreatorURL: "https://www.tiktok.com/tiktokstudio", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		manual("graph_text", "https://www.tiktok.com/tiktokstudio"),
		nativeSchedule(automated("video", "https://www.tiktok.com/tiktokstudio/upload", true, true), 15),
	}},
	{PlatformCode: "youtube", Name: "YouTube", CreatorURL: "https://studio.youtube.com", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		nativeSchedule(automated("video", "https://studio.youtube.com/", true, true), 15),
	}},
	{PlatformCode: "instagram", Name: "Instagram", CreatorURL: "https://business.facebook.com", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		automated("graph_text", "https://business.facebook.com/latest/composer/", false, false),
		automated("video", "https://business.facebook.com/latest/instagram_reels_composer", false, false),
	}},
	{PlatformCode: "facebook", Name: "Facebook", CreatorURL: "https://business.facebook.com", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		nativeSchedule(automated("graph_text", "https://business.facebook.com/latest/composer/", true, true), 20),
		nativeSchedule(automated("video", "https://business.facebook.com/latest/reels_composer", false, false), 20),
	}},
	{PlatformCode: "wechat_mp", Name: "微信公众号", CreatorURL: "https://mp.weixin.qq.com/", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		automated("article", "https://mp.weixin.qq.com/", true, true),
	}},
	{PlatformCode: "csdn", Name: "CSDN", CreatorURL: "https://mp.csdn.net", FirstRelease: true, Status: "unverified", Capabilities: []Capability{
		automated("article", "https://editor.csdn.net/md/", true, true),
	}},
	{PlatformCode: "acfun", Name: "AcFun", CreatorURL: "https://member.acfun.cn", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://member.acfun.cn"), manual("video", "https://member.acfun.cn"),
	}},
	{PlatformCode: "baijiahao", Name: "百家号", CreatorURL: "https://baijiahao.baidu.com", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://baijiahao.baidu.com"), manual("graph_text", "https://baijiahao.baidu.com"), manual("video", "https://baijiahao.baidu.com"),
	}},
	{PlatformCode: "bilibili", Name: "哔哩哔哩", CreatorURL: "https://member.bilibili.com/platform/home", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://member.bilibili.com/platform/home"), manual("video", "https://member.bilibili.com/platform/upload/video/frame"),
	}},
	{PlatformCode: "jianshu", Name: "简书", CreatorURL: "https://www.jianshu.com", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://www.jianshu.com"),
	}},
	{PlatformCode: "juejin", Name: "掘金", CreatorURL: "https://juejin.cn/creator", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://juejin.cn/creator"), manual("graph_text", "https://juejin.cn/creator"),
	}},
	{PlatformCode: "omtencent", Name: "腾讯内容开放平台", CreatorURL: "https://om.qq.com", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://om.qq.com"), manual("video", "https://om.qq.com"),
	}},
	{PlatformCode: "pdd", Name: "拼多多", CreatorURL: "https://mms.pinduoduo.com", Status: "unverified", Capabilities: []Capability{
		manual("video", "https://mms.pinduoduo.com"),
	}},
	{PlatformCode: "sina", Name: "新浪微博", CreatorURL: "https://weibo.com", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://weibo.com"), manual("graph_text", "https://weibo.com"), manual("video", "https://weibo.com"),
	}},
	{PlatformCode: "toutiao", Name: "今日头条", CreatorURL: "https://mp.toutiao.com", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://mp.toutiao.com"), manual("graph_text", "https://mp.toutiao.com"), manual("video", "https://mp.toutiao.com"),
	}},
	{PlatformCode: "weishi", Name: "微视", CreatorURL: "https://weishi.qq.com", Status: "unverified", Capabilities: []Capability{
		manual("video", "https://weishi.qq.com"),
	}},
	{PlatformCode: "x", Name: "X", CreatorURL: "https://x.com", Status: "unverified", Capabilities: []Capability{
		manual("graph_text", "https://x.com"), manual("video", "https://x.com"),
	}},
	{PlatformCode: "zhihu", Name: "知乎", CreatorURL: "https://www.zhihu.com/creator", Status: "unverified", Capabilities: []Capability{
		manual("article", "https://www.zhihu.com/creator"), manual("graph_text", "https://www.zhihu.com/creator"), manual("video", "https://www.zhihu.com/creator"),
	}},
}

func EgoLiteManifests() []Manifest {
	result := make([]Manifest, len(egoLiteManifests))
	for index, manifest := range egoLiteManifests {
		result[index] = cloneManifest(manifest)
	}
	return result
}

func ManifestFor(platformCode string) (Manifest, bool) {
	for _, manifest := range egoLiteManifests {
		if manifest.PlatformCode == platformCode {
			return cloneManifest(manifest), true
		}
	}
	return Manifest{}, false
}

func (manifest Manifest) CapabilityFor(contentType string) (Capability, bool) {
	for _, capability := range manifest.Capabilities {
		if capability.ContentType == contentType {
			return capability, true
		}
	}
	return Capability{}, false
}

func CapabilityFor(platformCode, contentType string) (Capability, bool) {
	manifest, ok := ManifestFor(platformCode)
	if !ok {
		return Capability{}, false
	}
	return manifest.CapabilityFor(contentType)
}

func (manifest Manifest) ContentTypes() []string {
	result := make([]string, 0, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		result = append(result, capability.ContentType)
	}
	return result
}

func (manifest Manifest) PrepareModeFor(contentType string) string {
	capability, ok := manifest.CapabilityFor(contentType)
	if !ok {
		return ""
	}
	return capability.PrepareMode
}

// PrepareAdapterSupportedFor distinguishes a local prefill adapter from a
// manual composer handoff. It says nothing about a final publish click or
// creator-backend acceptance.
func PrepareAdapterSupportedFor(platformCode, contentType string) bool {
	capability, ok := CapabilityFor(platformCode, contentType)
	return ok && capability.PrepareAdapterSupported
}

func SubmitAdapterSupportedFor(platformCode, contentType string) bool {
	capability, ok := CapabilityFor(platformCode, contentType)
	return ok && capability.SubmitAdapterSupported
}

func VerifyAdapterSupportedFor(platformCode, contentType string) bool {
	capability, ok := CapabilityFor(platformCode, contentType)
	return ok && capability.VerifyAdapterSupported
}

// SubmitVerifiedFor is deliberately distinct from adapter availability. It is
// the final-click gate and remains false until the exact live platform/content
// contract has independent creator-backend acceptance evidence.
func SubmitVerifiedFor(platformCode, contentType string) bool {
	capability, ok := CapabilityFor(platformCode, contentType)
	return ok && capability.SubmitVerified
}

// VerifyVerifiedFor is the only gate permitted to persist a manager inspection
// as published_verified or scheduled_verified.
func VerifyVerifiedFor(platformCode, contentType string) bool {
	capability, ok := CapabilityFor(platformCode, contentType)
	return ok && capability.VerifyVerified
}

func NativeScheduleAdapterSupportedFor(platformCode, contentType string) bool {
	capability, ok := CapabilityFor(platformCode, contentType)
	return ok && capability.NativeScheduleAdapterSupported
}

func NativeScheduleVerifiedFor(platformCode, contentType string) bool {
	capability, ok := CapabilityFor(platformCode, contentType)
	return ok && capability.NativeScheduleVerified
}

func NativeScheduleMinLeadMinutesFor(platformCode, contentType string) int {
	capability, ok := CapabilityFor(platformCode, contentType)
	if !ok || !capability.NativeScheduleAdapterSupported || capability.NativeScheduleMinLeadMinutes < 1 {
		return 0
	}
	return capability.NativeScheduleMinLeadMinutes
}

// NativeScheduleSupportedFor is the ordinary workflow gate. An adapter alone
// never authorizes schedule use until independent evidence marks the exact
// platform/content contract as verified.
func NativeScheduleSupportedFor(platformCode, contentType string) bool {
	return NativeScheduleAdapterSupportedFor(platformCode, contentType) && NativeScheduleVerifiedFor(platformCode, contentType)
}

func ComposerURLFor(platformCode, contentType, assetExternalID string) (string, bool) {
	capability, ok := CapabilityFor(platformCode, contentType)
	if !ok || capability.ComposerURL == "" {
		return "", false
	}
	composerURL := capability.ComposerURL
	if platformCode == "instagram" || platformCode == "facebook" {
		if strings.TrimSpace(assetExternalID) == "" {
			return "", false
		}
		parsed, err := url.Parse(composerURL)
		if err != nil {
			return "", false
		}
		query := parsed.Query()
		query.Set("asset_id", assetExternalID)
		parsed.RawQuery = query.Encode()
		composerURL = parsed.String()
	}
	return composerURL, true
}

func cloneManifest(manifest Manifest) Manifest {
	manifest.Capabilities = append([]Capability(nil), manifest.Capabilities...)
	return manifest
}
