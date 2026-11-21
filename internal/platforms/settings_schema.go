package platforms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	// PublishSettingsSnapshotVersion versions the immutable job envelope, not a
	// platform's setting fields or selector catalog.
	PublishSettingsSnapshotVersion   = 1
	MetaSettingsSchemaVersion        = 1
	XHSSettingsSchemaVersion         = 6
	DouyinSettingsSchemaVersion      = 4
	WechatMPSettingsSchemaVersion    = 1
	CSDNSettingsSchemaVersion        = 1
	WechatVideoSettingsSchemaVersion = 3
	KuaishouSettingsSchemaVersion    = 3
	YouTubeSettingsSchemaVersion     = 2
	TikTokSettingsSchemaVersion      = 2
	XHSBrowserAdapterVersion         = 1
	CoreBrowserHandoffAdapterVersion = 1
)

// SettingOption is one allowed value for an enum field.
type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// SettingField is the small renderer-neutral contract needed by the desktop UI.
// "manual" keeps an official platform field visible without claiming the
// current browser worker can fill it.
type SettingField struct {
	Key          string          `json:"key"`
	Label        string          `json:"label"`
	Type         string          `json:"type"`
	Required     bool            `json:"required"`
	Remember     bool            `json:"remember"`
	Default      any             `json:"default"`
	Description  string          `json:"description,omitempty"`
	Verification string          `json:"verification,omitempty"`
	ContentTypes []string        `json:"content_types,omitempty"`
	Options      []SettingOption `json:"options,omitempty"`
}

// SettingsSchema describes the public, non-secret settings accepted for one
// platform. Absence of a schema means the platform has no supported local
// preference contract.
type SettingsSchema struct {
	PlatformCode string         `json:"platform_code"`
	Version      int            `json:"version"`
	Verification string         `json:"verification"`
	VerifiedAt   string         `json:"verified_at,omitempty"`
	Fields       []SettingField `json:"fields"`
}

type XHSSettings struct {
	Visibility      string `json:"visibility"`
	CommentEnabled  bool   `json:"comment_enabled"`
	DeclareOriginal bool   `json:"declare_original"`
	Location        string `json:"location"`
	Collection      string `json:"collection"`
	Group           string `json:"group"`
	ContentSource   string `json:"content_source"`
	RelatedMedia    string `json:"related_media"`
	AllowDuet       bool   `json:"allow_duet"`
	AllowCopy       bool   `json:"allow_copy"`
}

type DouyinSettings struct {
	Activity        string `json:"activity"`
	Collection      string `json:"collection"`
	ExtensionType   string `json:"extension_type"`
	Location        string `json:"location"`
	Hotspot         string `json:"hotspot"`
	AllowDownload   bool   `json:"allow_download"`
	Visibility      string `json:"visibility"`
	Music           string `json:"music"`
	CommentEnabled  bool   `json:"comment_enabled"`
	SelfDeclaration string `json:"self_declaration"`
}

type WechatMPSettings struct {
	Author          string `json:"author"`
	OriginalLink    string `json:"original_link"`
	CommentEnabled  bool   `json:"comment_enabled"`
	DeclareOriginal bool   `json:"declare_original"`
	AllowReprint    bool   `json:"allow_reprint"`
	PublishType     string `json:"publish_type"`
	Collection      string `json:"collection"`
	ContentSource   string `json:"content_source"`
}

type CSDNSettings struct {
	Collection    string `json:"collection"`
	ArticleType   string `json:"article_type"`
	OriginalLink  string `json:"original_link"`
	BackupGitCode bool   `json:"backup_gitcode"`
	Visibility    string `json:"visibility"`
	Activity      string `json:"activity"`
	Topic         string `json:"topic"`
}

type WechatVideoSettings struct {
	Location        string `json:"location"`
	Collection      string `json:"collection"`
	LinkType        string `json:"link_type"`
	LinkAddress     string `json:"link_address"`
	Activity        string `json:"activity"`
	DeclareOriginal bool   `json:"declare_original"`
	Music           string `json:"music"`
}

type KuaishouSettings struct {
	LinkApplet     string `json:"link_applet"`
	Declaration    string `json:"declaration"`
	Collection     string `json:"collection"`
	Location       string `json:"location"`
	AllowDuet      bool   `json:"allow_duet"`
	AllowDownload  bool   `json:"allow_download"`
	SameCity       bool   `json:"same_city"`
	Visibility     string `json:"visibility"`
	Music          string `json:"music"`
	CommentEnabled bool   `json:"comment_enabled"`
}

type YouTubeSettings struct {
	Audience       string `json:"audience"`
	Visibility     string `json:"visibility"`
	Playlist       string `json:"playlist"`
	AllowComments  bool   `json:"allow_comments"`
	AlteredContent bool   `json:"altered_content"`
}

type TikTokSettings struct {
	Visibility    string `json:"visibility"`
	AllowComment  bool   `json:"allow_comment"`
	AllowDuet     bool   `json:"allow_duet"`
	AllowStitch   bool   `json:"allow_stitch"`
	AIGenerated   bool   `json:"ai_generated"`
	AllowDownload bool   `json:"allow_download"`
}

// PublishSettingsSnapshot is the immutable settings envelope persisted with a
// publish job. Catalog and adapter versions are supplied by their owning layers.
type PublishSettingsSnapshot struct {
	SnapshotVersion        int             `json:"snapshot_version"`
	PlatformCode           string          `json:"platform_code"`
	TemplateID             int64           `json:"template_id"`
	SettingsSchemaVersion  int             `json:"settings_schema_version"`
	SelectorCatalogVersion int             `json:"selector_catalog_version"`
	AdapterVersion         int             `json:"adapter_version"`
	Settings               json.RawMessage `json:"settings"`
}

// PublishSettingsAdapterVersion declares the adapter contract expected by a
// settings snapshot. Creator handoff adapters record preferences but do not
// attempt to apply them to a third-party page.
func PublishSettingsAdapterVersion(platformCode string) (int, bool) {
	switch normalizePlatformCode(platformCode) {
	case "xhs":
		return XHSBrowserAdapterVersion, true
	case "instagram", "facebook", "douyin", "wechat_mp", "csdn", "wechat_video", "kuaishou", "youtube", "tiktok":
		return CoreBrowserHandoffAdapterVersion, true
	default:
		return 0, false
	}
}

// SettingsSchemaFor returns a fresh schema value so callers cannot mutate a
// package-global registry. Core creator preferences are persisted for human
// handoff; browser adapters do not claim they applied them until separately
// verified against the creator console.
func SettingsSchemaFor(platformCode string) (SettingsSchema, bool) {
	switch normalizePlatformCode(platformCode) {
	case "instagram", "facebook":
		code := normalizePlatformCode(platformCode)
		return SettingsSchema{
			PlatformCode: code,
			Version:      MetaSettingsSchemaVersion,
			Verification: "unverified",
			Fields: []SettingField{
				{Key: "visibility", Label: "可见范围", Type: "manual", Verification: "unverified", Description: "发布前读取并确认创作者后台的实际公开状态"},
				{Key: "comment_enabled", Label: "允许评论", Type: "manual", Verification: "unverified", Description: "发布前读取并确认创作者后台的实际评论状态"},
				{Key: "commercial_content", Label: "商业合作内容", Type: "manual", Verification: "unverified", Description: "发布前读取并确认商业合作开关为关闭"},
				{Key: "ai_generated", Label: "AI 生成内容", Type: "manual", Verification: "unverified", Description: "页面存在原生 AI 标识时人工开启；不存在时以正文披露为准"},
			},
		}, true
	case "xhs":
		return SettingsSchema{
			PlatformCode: "xhs",
			Version:      XHSSettingsSchemaVersion,
			Verification: "console_verified",
			VerifiedAt:   "2026-07-25",
			Fields: []SettingField{
				{Key: "location", Label: "位置", Type: "string", Remember: true, Default: ""},
				{Key: "collection", Label: "合集", Type: "string", Remember: true, Default: ""},
				{Key: "group", Label: "关联群聊", Type: "string", Remember: true, Default: ""},
				{Key: "declare_original", Label: "声明原创", Type: "boolean", Remember: true, Default: false},
				{
					Key: "content_source", Label: "创作来源", Type: "enum", Remember: true, Default: "none",
					Options: []SettingOption{
						{Value: "none", Label: "不声明"}, {Value: "fiction", Label: "虚构演绎仅供娱乐"},
						{Value: "ai", Label: "笔记含 AI 合成内容"}, {Value: "self_disclosed", Label: "已在正文中自主标注"},
						{Value: "original", Label: "自主拍摄"}, {Value: "reprint", Label: "来源转载"},
					},
				},
				{Key: "related_media", Label: "关联媒体", Type: "string", Remember: true, Default: ""},
				{
					Key: "visibility", Label: "可见范围", Type: "enum", Remember: true, Default: "public",
					Options: []SettingOption{{Value: "public", Label: "公开"}, {Value: "friends", Label: "互关好友"}, {Value: "private", Label: "仅自己"}},
				},
				{Key: "comment_enabled", Label: "允许评论", Type: "boolean", Remember: true, Default: true},
				{Key: "allow_duet", Label: "允许合拍", Type: "boolean", Remember: true, Default: false},
				{Key: "allow_copy", Label: "允许正文复制", Type: "boolean", Remember: true, Default: false},
			},
		}, true
	case "douyin":
		return SettingsSchema{
			PlatformCode: "douyin", Version: DouyinSettingsSchemaVersion,
			Verification: "console_verified", VerifiedAt: "2026-07-25",
			Fields: []SettingField{
				{Key: "activity", Label: "官方活动", Type: "string", Remember: true, Default: "", Description: "输入活动名称，多个活动用 / 分隔"},
				{Key: "collection", Label: "添加合集", Type: "string", Remember: true, Default: ""},
				{Key: "extension_type", Label: "添加标签", Type: "enum", Remember: true, Default: "none", Options: []SettingOption{
					{Value: "none", Label: "不添加"}, {Value: "location", Label: "位置"}, {Value: "film_tv", Label: "影视演艺"},
					{Value: "mini_program", Label: "小程序"}, {Value: "mark_objects", Label: "标记万物"},
				}},
				{Key: "location", Label: "位置", Type: "string", Remember: true, Default: "", Description: "选择位置标签时使用"},
				{Key: "hotspot", Label: "关联热点", Type: "string", Remember: true, Default: ""},
				{Key: "allow_download", Label: "保存权限", Type: "boolean", Remember: true, Default: true},
				{Key: "visibility", Label: "谁可以看", Type: "enum", Remember: true, Default: "public", Options: []SettingOption{{Value: "public", Label: "公开"}, {Value: "friends", Label: "好友可见"}, {Value: "private", Label: "仅自己可见"}}},
				{Key: "music", Label: "音乐", Type: "string", Remember: true, Default: "", ContentTypes: []string{"graph_text"}, Description: "仅图文发布生效"},
				{Key: "comment_enabled", Label: "允许评论", Type: "boolean", Remember: true, Default: true, Verification: "unverified", Description: "冻结意图；当前选择器未验证，Worker 必须 fail-closed"},
				{Key: "friend_mention", Label: "@好友", Type: "manual", Verification: "unverified", Description: "官方页面存在该字段；当前需在平台页面选择"},
				{Key: "self_declaration", Label: "自主声明", Type: "enum", Remember: true, Default: "none", Verification: "unverified", Description: "冻结意图；当前选择器未验证，Worker 必须 fail-closed", Options: []SettingOption{
					{Value: "none", Label: "不声明"}, {Value: "ai", Label: "AI 生成"},
				}},
				{Key: "video_chapters", Label: "视频章节", Type: "manual", Verification: "unverified", ContentTypes: []string{"video"}, Description: "结构化章节需在平台页面确认"},
			},
		}, true
	case "wechat_mp":
		return SettingsSchema{
			PlatformCode: "wechat_mp", Version: WechatMPSettingsSchemaVersion,
			Verification: "console_verified", VerifiedAt: "2026-07-25",
			Fields: []SettingField{
				{Key: "author", Label: "作者", Type: "string", Default: ""},
				{Key: "original_link", Label: "原文链接", Type: "string", Default: ""},
				{Key: "comment_enabled", Label: "开启留言", Type: "boolean", Default: false},
				{Key: "declare_original", Label: "声明原创", Type: "boolean", Default: false},
				{Key: "allow_reprint", Label: "允许快捷转载", Type: "boolean", Default: false},
				{Key: "publish_type", Label: "发表类型", Type: "enum", Default: "publish", Options: []SettingOption{{Value: "publish", Label: "发布"}}},
				{Key: "collection", Label: "合集", Type: "string", Default: ""},
				{Key: "content_source", Label: "内容来源声明", Type: "enum", Default: "none", Options: []SettingOption{
					{Value: "none", Label: "不声明"}, {Value: "ai", Label: "内容由 AI 生成"},
					{Value: "official_media", Label: "素材来源官方媒体/网络新闻"}, {Value: "fiction", Label: "内容剧情演绎仅供娱乐"},
					{Value: "opinion", Label: "个人观点仅供参考"}, {Value: "health", Label: "健康医疗分享仅供参考"},
					{Value: "investment", Label: "投资观点仅供参考"},
				}},
			},
		}, true
	case "csdn":
		return SettingsSchema{
			PlatformCode: "csdn", Version: CSDNSettingsSchemaVersion,
			Verification: "console_verified", VerifiedAt: "2026-07-25",
			Fields: []SettingField{
				{Key: "collection", Label: "分类专栏", Type: "string", Default: ""},
				{Key: "article_type", Label: "文章类型", Type: "enum", Default: "original", Options: []SettingOption{{Value: "original", Label: "原创"}, {Value: "repost", Label: "转载"}, {Value: "translation", Label: "翻译"}}},
				{Key: "original_link", Label: "原文链接", Type: "string", Default: ""},
				{Key: "backup_gitcode", Label: "同时备份到 GitCode", Type: "boolean", Default: false},
				{Key: "visibility", Label: "可见范围", Type: "enum", Default: "public", Options: []SettingOption{{Value: "public", Label: "全部可见"}, {Value: "private", Label: "仅自己可见"}, {Value: "followers", Label: "粉丝可见"}, {Value: "vip", Label: "VIP 可见"}}},
				{Key: "activity", Label: "参与活动", Type: "string", Default: ""},
				{Key: "topic", Label: "话题", Type: "string", Default: ""},
			},
		}, true
	case "wechat_video":
		return SettingsSchema{
			PlatformCode: "wechat_video", Version: WechatVideoSettingsSchemaVersion,
			Verification: "contract_verified", VerifiedAt: "2026-07-25",
			Fields: []SettingField{
				{Key: "location", Label: "地理位置", Type: "string", Remember: true, Default: "", Description: "留空不显示"},
				{Key: "collection", Label: "合集", Type: "string", Remember: true, Default: ""},
				{Key: "link_type", Label: "链接类型", Type: "enum", Remember: true, Default: "none", Options: []SettingOption{{Value: "none", Label: "不设置"}, {Value: "article", Label: "公众号文章"}, {Value: "red_packet", Label: "红包封面"}}},
				{Key: "link_address", Label: "链接地址", Type: "string", Remember: true, Default: "", Description: "选择链接类型后生效"},
				{Key: "activity", Label: "参与活动", Type: "string", Remember: true, Default: ""},
				{Key: "declare_original", Label: "声明原创", Type: "boolean", Remember: true, Default: false, ContentTypes: []string{"video"}},
				{Key: "music", Label: "音乐", Type: "string", Remember: true, Default: "", ContentTypes: []string{"graph_text"}, Description: "仅图文发布生效"},
			},
		}, true
	case "kuaishou":
		return SettingsSchema{
			PlatformCode: "kuaishou", Version: KuaishouSettingsSchemaVersion,
			Verification: "contract_verified", VerifiedAt: "2026-07-25",
			Fields: []SettingField{
				{Key: "link_applet", Label: "关联小程序", Type: "string", Remember: true, Default: "", Description: "填写快手 App 小程序页面链接"},
				{Key: "declaration", Label: "作品声明", Type: "enum", Remember: true, Default: "none", Options: []SettingOption{
					{Value: "none", Label: "不声明"}, {Value: "ai", Label: "内容为 AI 生成"}, {Value: "fiction", Label: "演绎情节仅供娱乐"},
					{Value: "opinion", Label: "个人观点仅供参考"}, {Value: "network", Label: "素材来源于网络"},
				}},
				{Key: "collection", Label: "合集", Type: "string", Remember: true, Default: ""},
				{Key: "location", Label: "位置", Type: "string", Remember: true, Default: ""},
				{Key: "allow_duet", Label: "允许别人跟我拍同框", Type: "boolean", Remember: true, Default: true},
				{Key: "allow_download", Label: "允许下载此作品", Type: "boolean", Remember: true, Default: true},
				{Key: "same_city", Label: "作品展示在同城页", Type: "boolean", Remember: true, Default: true},
				{Key: "visibility", Label: "谁可以看", Type: "enum", Remember: true, Default: "public", Options: []SettingOption{{Value: "public", Label: "公开"}, {Value: "friends", Label: "好友"}, {Value: "private", Label: "仅自己"}}},
				{Key: "music", Label: "音乐", Type: "string", Remember: true, Default: "", ContentTypes: []string{"graph_text"}, Description: "仅图文发布生效"},
				{Key: "comment_enabled", Label: "允许评论", Type: "boolean", Remember: true, Default: true, Verification: "unverified", Description: "冻结意图；当前选择器未验证，Worker 必须 fail-closed"},
			},
		}, true
	case "youtube":
		return SettingsSchema{
			PlatformCode: "youtube", Version: YouTubeSettingsSchemaVersion,
			Verification: "contract_verified", VerifiedAt: "2026-07-26",
			Fields: []SettingField{
				{
					Key: "audience", Label: "受众", Type: "enum", Required: true, Remember: true, Default: "not_made_for_kids",
					Description: "默认不是面向儿童的内容；确认页仍可修改。",
					Options: []SettingOption{
						{Value: "", Label: "请选择"},
						{Value: "not_made_for_kids", Label: "不是面向儿童的内容"},
						{Value: "made_for_kids", Label: "是面向儿童的内容"},
					},
				},
				{Key: "visibility", Label: "可见范围", Type: "enum", Remember: true, Default: "public", Options: []SettingOption{
					{Value: "private", Label: "私享"}, {Value: "unlisted", Label: "不公开列出"}, {Value: "public", Label: "公开"},
				}},
				{Key: "playlist", Label: "播放列表", Type: "string", Remember: true, Default: ""},
				{Key: "allow_comments", Label: "允许评论", Type: "boolean", Remember: true, Default: true},
				{Key: "altered_content", Label: "含逼真合成或篡改内容", Type: "boolean", Remember: true, Default: false},
			},
		}, true
	case "tiktok":
		return SettingsSchema{
			PlatformCode: "tiktok", Version: TikTokSettingsSchemaVersion,
			Verification: "contract_verified", VerifiedAt: "2026-07-28",
			Fields: []SettingField{
				{Key: "visibility", Label: "谁可以观看", Type: "enum", Remember: true, Default: "private", Options: []SettingOption{
					{Value: "public", Label: "所有人"}, {Value: "friends", Label: "好友"}, {Value: "private", Label: "仅自己"},
				}},
				{Key: "allow_comment", Label: "允许评论", Type: "boolean", Remember: true, Default: true},
				{Key: "allow_duet", Label: "允许合拍", Type: "boolean", Remember: true, Default: true, ContentTypes: []string{"video"}},
				{Key: "allow_stitch", Label: "允许拼接", Type: "boolean", Remember: true, Default: true, ContentTypes: []string{"video"}},
				{Key: "ai_generated", Label: "AI 生成内容", Type: "boolean", Remember: true, Default: false},
				{Key: "allow_download", Label: "允许下载", Type: "boolean", Remember: true, Default: true, Description: "当前 Studio 未展示下载开关时，Worker 通过完整发布页地标确认其缺席"},
			},
		}, true
	default:
		return SettingsSchema{}, false
	}
}

// ParseXHSSettings validates exact JSON types and rejects unknown fields. Empty
// input is treated as an empty object so callers receive explicit defaults.
func ParseXHSSettings(settingsJSON string) (XHSSettings, error) {
	settings := XHSSettings{
		Visibility:     "public",
		CommentEnabled: true,
		ContentSource:  "none",
	}
	raw := strings.TrimSpace(settingsJSON)
	if raw == "" {
		raw = "{}"
	}

	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return XHSSettings{}, fmt.Errorf("xhs settings must be a JSON object: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return XHSSettings{}, fmt.Errorf("xhs settings must contain one JSON object: %w", err)
	}
	values, ok := decoded.(map[string]any)
	if !ok || values == nil {
		return XHSSettings{}, fmt.Errorf("xhs settings must be a JSON object")
	}

	for key, value := range values {
		switch key {
		case "visibility":
			visibility, ok := value.(string)
			if !ok {
				return XHSSettings{}, fmt.Errorf("xhs setting %q must be a string", key)
			}
			switch visibility {
			case "public", "friends", "private":
				settings.Visibility = visibility
			default:
				return XHSSettings{}, fmt.Errorf("xhs setting %q must be public or private", key)
			}
		case "comment_enabled":
			enabled, ok := value.(bool)
			if !ok {
				return XHSSettings{}, fmt.Errorf("xhs setting %q must be a boolean", key)
			}
			settings.CommentEnabled = enabled
		case "declare_original", "allow_duet", "allow_copy":
			booleanValue, ok := value.(bool)
			if !ok {
				return XHSSettings{}, fmt.Errorf("xhs setting %q must be a boolean", key)
			}
			switch key {
			case "declare_original":
				settings.DeclareOriginal = booleanValue
			case "allow_duet":
				settings.AllowDuet = booleanValue
			default:
				settings.AllowCopy = booleanValue
			}
		case "location":
			location, ok := value.(string)
			if !ok {
				return XHSSettings{}, fmt.Errorf("xhs setting %q must be a string", key)
			}
			location = strings.TrimSpace(location)
			if utf8.RuneCountInString(location) > 128 || strings.ContainsAny(location, "\r\n\x00") {
				return XHSSettings{}, fmt.Errorf("xhs setting %q must be at most 128 characters without control lines", key)
			}
			settings.Location = location
		case "collection", "group", "related_media":
			text, err := normalizeTextSetting("xhs", key, value)
			if err != nil {
				return XHSSettings{}, err
			}
			switch key {
			case "collection":
				settings.Collection = text
			case "group":
				settings.Group = text
			default:
				settings.RelatedMedia = text
			}
		case "content_source":
			source, ok := value.(string)
			if !ok || !stringIn(source, "none", "fiction", "ai", "self_disclosed", "original", "reprint") {
				return XHSSettings{}, fmt.Errorf("xhs setting %q has an invalid value", key)
			}
			settings.ContentSource = source
		default:
			return XHSSettings{}, fmt.Errorf("unknown xhs setting %q", key)
		}
	}
	return settings, nil
}

// parseCoreSettings keeps the newer handoff schemas strict without changing
// the established XHS normalization order or compatibility surface.
func parseCoreSettings(platformCode, settingsJSON string) (map[string]any, error) {
	schema, ok := SettingsSchemaFor(platformCode)
	if !ok {
		return nil, fmt.Errorf("platform %q has no settings schema", strings.TrimSpace(platformCode))
	}
	raw := strings.TrimSpace(settingsJSON)
	if raw == "" {
		raw = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%s settings must be a JSON object: %w", schema.PlatformCode, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%s settings must contain one JSON object: %w", schema.PlatformCode, err)
	}
	provided, ok := decoded.(map[string]any)
	if !ok || provided == nil {
		return nil, fmt.Errorf("%s settings must be a JSON object", schema.PlatformCode)
	}
	fields := make(map[string]SettingField, len(schema.Fields))
	for _, field := range schema.Fields {
		if field.Type == "manual" {
			continue
		}
		fields[field.Key] = field
	}
	for key := range provided {
		if _, known := fields[key]; !known {
			return nil, fmt.Errorf("unknown %s setting %q", schema.PlatformCode, key)
		}
	}
	values := make(map[string]any, len(schema.Fields))
	for _, field := range schema.Fields {
		if field.Type == "manual" {
			continue
		}
		value, present := provided[field.Key]
		if !present {
			value = field.Default
		}
		switch field.Type {
		case "boolean":
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("%s setting %q must be a boolean", schema.PlatformCode, field.Key)
			}
		case "string":
			text, err := normalizeTextSetting(schema.PlatformCode, field.Key, value)
			if err != nil {
				return nil, err
			}
			if field.Required && text == "" {
				return nil, fmt.Errorf("%s setting %q is required", schema.PlatformCode, field.Key)
			}
			value = text
		case "enum":
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s setting %q must be a string", schema.PlatformCode, field.Key)
			}
			valid := false
			for _, option := range field.Options {
				if option.Value == text {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("%s setting %q has an invalid value", schema.PlatformCode, field.Key)
			}
		default:
			return nil, fmt.Errorf("unsupported %s setting type %q", schema.PlatformCode, field.Type)
		}
		values[field.Key] = value
	}
	return values, nil
}

// NormalizeSettings validates a platform setting object and returns stable JSON.
func NormalizeSettings(platformCode, settingsJSON string) (string, error) {
	switch normalizePlatformCode(platformCode) {
	case "instagram", "facebook":
		values, err := parseCoreSettings(normalizePlatformCode(platformCode), settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return "", fmt.Errorf("encode normalized Meta settings: %w", err)
		}
		return string(encoded), nil
	case "xhs":
		settings, err := ParseXHSSettings(settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(settings)
		if err != nil {
			return "", fmt.Errorf("encode normalized xhs settings: %w", err)
		}
		return string(encoded), nil
	case "douyin":
		values, err := parseCoreSettings("douyin", settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(DouyinSettings{
			Activity: values["activity"].(string), Collection: values["collection"].(string),
			ExtensionType: values["extension_type"].(string), Location: values["location"].(string),
			Hotspot: values["hotspot"].(string), AllowDownload: values["allow_download"].(bool),
			Visibility: values["visibility"].(string), Music: values["music"].(string),
			CommentEnabled: values["comment_enabled"].(bool), SelfDeclaration: values["self_declaration"].(string),
		})
		if err != nil {
			return "", fmt.Errorf("encode normalized douyin settings: %w", err)
		}
		return string(encoded), nil
	case "wechat_mp":
		values, err := parseCoreSettings("wechat_mp", settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(WechatMPSettings{
			Author: values["author"].(string), OriginalLink: values["original_link"].(string),
			CommentEnabled: values["comment_enabled"].(bool), DeclareOriginal: values["declare_original"].(bool),
			AllowReprint: values["allow_reprint"].(bool), PublishType: values["publish_type"].(string),
			Collection: values["collection"].(string), ContentSource: values["content_source"].(string),
		})
		if err != nil {
			return "", fmt.Errorf("encode normalized wechat_mp settings: %w", err)
		}
		return string(encoded), nil
	case "csdn":
		values, err := parseCoreSettings("csdn", settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(CSDNSettings{
			Collection: values["collection"].(string), ArticleType: values["article_type"].(string),
			OriginalLink: values["original_link"].(string), BackupGitCode: values["backup_gitcode"].(bool),
			Visibility: values["visibility"].(string), Activity: values["activity"].(string), Topic: values["topic"].(string),
		})
		if err != nil {
			return "", fmt.Errorf("encode normalized csdn settings: %w", err)
		}
		return string(encoded), nil
	case "wechat_video":
		values, err := parseCoreSettings("wechat_video", settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(WechatVideoSettings{
			Location: values["location"].(string), Collection: values["collection"].(string),
			LinkType: values["link_type"].(string), LinkAddress: values["link_address"].(string),
			Activity: values["activity"].(string), DeclareOriginal: values["declare_original"].(bool),
			Music: values["music"].(string),
		})
		if err != nil {
			return "", fmt.Errorf("encode normalized wechat_video settings: %w", err)
		}
		return string(encoded), nil
	case "kuaishou":
		values, err := parseCoreSettings("kuaishou", settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(KuaishouSettings{
			LinkApplet: values["link_applet"].(string), Declaration: values["declaration"].(string),
			Collection: values["collection"].(string), Location: values["location"].(string),
			AllowDuet: values["allow_duet"].(bool), AllowDownload: values["allow_download"].(bool),
			SameCity: values["same_city"].(bool), Visibility: values["visibility"].(string),
			Music: values["music"].(string), CommentEnabled: values["comment_enabled"].(bool),
		})
		if err != nil {
			return "", fmt.Errorf("encode normalized kuaishou settings: %w", err)
		}
		return string(encoded), nil
	case "youtube":
		values, err := parseCoreSettings("youtube", settingsJSON)
		if err != nil {
			return "", err
		}
		if values["audience"].(string) == "" {
			return "", fmt.Errorf("youtube setting %q is required", "audience")
		}
		encoded, err := json.Marshal(YouTubeSettings{
			Audience: values["audience"].(string), Visibility: values["visibility"].(string),
			Playlist: values["playlist"].(string), AllowComments: values["allow_comments"].(bool),
			AlteredContent: values["altered_content"].(bool),
		})
		if err != nil {
			return "", fmt.Errorf("encode normalized youtube settings: %w", err)
		}
		return string(encoded), nil
	case "tiktok":
		values, err := parseCoreSettings("tiktok", settingsJSON)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(TikTokSettings{
			Visibility: values["visibility"].(string), AllowComment: values["allow_comment"].(bool),
			AllowDuet: values["allow_duet"].(bool), AllowStitch: values["allow_stitch"].(bool),
			AIGenerated: values["ai_generated"].(bool), AllowDownload: values["allow_download"].(bool),
		})
		if err != nil {
			return "", fmt.Errorf("encode normalized tiktok settings: %w", err)
		}
		return string(encoded), nil
	default:
		return "", fmt.Errorf("platform %q has no settings schema", strings.TrimSpace(platformCode))
	}
}

func normalizeTextSetting(platformCode, key string, value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s setting %q must be a string", platformCode, key)
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > 128 || strings.ContainsAny(text, "\r\n\x00") {
		return "", fmt.Errorf("%s setting %q must be at most 128 characters without control lines", platformCode, key)
	}
	return text, nil
}

func stringIn(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// NormalizeSettingsForContentType keeps shared settings and removes fields
// explicitly scoped to a different media type before a plan reaches a worker.
func NormalizeSettingsForContentType(platformCode, contentType, settingsJSON string) (string, error) {
	normalized, err := NormalizeSettings(platformCode, settingsJSON)
	if err != nil {
		return "", err
	}
	return filterNormalizedSettingsForContentType(platformCode, contentType, normalized)
}

func filterNormalizedSettingsForContentType(platformCode, contentType, normalized string) (string, error) {
	schema, ok := SettingsSchemaFor(platformCode)
	if !ok {
		return normalized, nil
	}
	var values map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &values); err != nil {
		return "", fmt.Errorf("decode normalized platform settings: %w", err)
	}
	changed := false
	for _, field := range schema.Fields {
		if len(field.ContentTypes) == 0 {
			continue
		}
		supported := false
		for _, reviewedType := range field.ContentTypes {
			if reviewedType == contentType {
				supported = true
				break
			}
		}
		if !supported {
			if _, exists := values[field.Key]; exists {
				delete(values, field.Key)
				changed = true
			}
		}
	}
	if !changed {
		return normalized, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode content-specific platform settings: %w", err)
	}
	return string(encoded), nil
}

// BuildPublishSettingsSnapshot validates settings and emits stable JSON. It does
// not infer catalog or adapter versions because those values belong to their
// respective runtime owners.
func BuildPublishSettingsSnapshot(platformCode string, templateID int64, settingsJSON string, selectorCatalogVersion, adapterVersion int) (string, error) {
	if templateID < 0 {
		return "", fmt.Errorf("template id must not be negative")
	}
	if selectorCatalogVersion <= 0 {
		return "", fmt.Errorf("selector catalog version must be positive")
	}
	if adapterVersion <= 0 {
		return "", fmt.Errorf("adapter version must be positive")
	}
	code := normalizePlatformCode(platformCode)
	schema, ok := SettingsSchemaFor(code)
	if !ok {
		return "", fmt.Errorf("platform %q has no settings schema", strings.TrimSpace(platformCode))
	}
	normalized, err := NormalizeSettings(code, settingsJSON)
	if err != nil {
		return "", err
	}
	snapshot := PublishSettingsSnapshot{
		SnapshotVersion:        PublishSettingsSnapshotVersion,
		PlatformCode:           code,
		TemplateID:             templateID,
		SettingsSchemaVersion:  schema.Version,
		SelectorCatalogVersion: selectorCatalogVersion,
		AdapterVersion:         adapterVersion,
		Settings:               json.RawMessage(normalized),
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode publish settings snapshot: %w", err)
	}
	return string(encoded), nil
}

// ParsePublishSettingsSnapshot strictly validates an immutable job envelope and
// returns its settings in normalized order.
func ParsePublishSettingsSnapshot(snapshotJSON string) (*PublishSettingsSnapshot, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(snapshotJSON)))
	decoder.DisallowUnknownFields()
	var snapshot PublishSettingsSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("invalid publish settings snapshot: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("publish settings snapshot must contain one JSON object: %w", err)
	}
	if snapshot.SnapshotVersion != PublishSettingsSnapshotVersion {
		return nil, fmt.Errorf("unsupported publish settings snapshot version %d", snapshot.SnapshotVersion)
	}
	if snapshot.TemplateID < 0 {
		return nil, fmt.Errorf("template id must not be negative")
	}
	if snapshot.SelectorCatalogVersion <= 0 {
		return nil, fmt.Errorf("selector catalog version must be positive")
	}
	if snapshot.AdapterVersion <= 0 {
		return nil, fmt.Errorf("adapter version must be positive")
	}
	code := normalizePlatformCode(snapshot.PlatformCode)
	schema, ok := SettingsSchemaFor(code)
	if !ok {
		return nil, fmt.Errorf("platform %q has no settings schema", snapshot.PlatformCode)
	}
	if snapshot.PlatformCode != code {
		return nil, fmt.Errorf("platform code must be normalized as %q", code)
	}
	// Earlier snapshots remain readable; current normalization preserves their
	// explicit values and supplies only the newly versioned safe defaults.
	legacySchema := (code == "xhs" && snapshot.SettingsSchemaVersion >= 1 && snapshot.SettingsSchemaVersion <= 5) ||
		(code == "wechat_video" && snapshot.SettingsSchemaVersion == 2) ||
		(code == "douyin" && snapshot.SettingsSchemaVersion >= 1 && snapshot.SettingsSchemaVersion <= 3) ||
		(code == "kuaishou" && snapshot.SettingsSchemaVersion >= 1 && snapshot.SettingsSchemaVersion <= 2) ||
		(code == "tiktok" && snapshot.SettingsSchemaVersion == 1)
	if snapshot.SettingsSchemaVersion != schema.Version && !legacySchema {
		return nil, fmt.Errorf("unsupported %s settings schema version %d", code, snapshot.SettingsSchemaVersion)
	}
	if len(bytes.TrimSpace(snapshot.Settings)) == 0 || bytes.Equal(bytes.TrimSpace(snapshot.Settings), []byte("null")) {
		return nil, fmt.Errorf("publish settings snapshot requires settings object")
	}
	normalized, err := NormalizeSettings(code, string(snapshot.Settings))
	if err != nil {
		return nil, err
	}
	snapshot.Settings = json.RawMessage(normalized)
	return &snapshot, nil
}

func normalizePlatformCode(platformCode string) string {
	return strings.ToLower(strings.TrimSpace(platformCode))
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
