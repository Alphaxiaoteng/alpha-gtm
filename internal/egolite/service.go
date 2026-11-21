package egolite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/trace"
)

type ProgramRunner interface {
	Run(context.Context, string, any) error
}

const loginQRRefreshInterval = 45 * time.Second

type SpaceService struct {
	db      *db.DB
	runtime ProgramRunner
	// ensureMu serializes "does this identity already own a Space" with "create
	// one". Two callers that both read "no Space" would each ask the runtime for
	// a Space, and the loser's Space stays orphaned in EgoLite.
	ensureMu sync.Mutex
}

type metaPublishTarget struct {
	FacebookPage     db.AccountAsset
	InstagramAccount db.AccountAsset
}

func (service *SpaceService) selectMetaAsset(accountID int64, platformCode, assetType, requiredCode, ambiguousCode string) (db.AccountAsset, error) {
	assets, err := service.db.ListAccountAssetsByAccount(accountID)
	if err != nil {
		return db.AccountAsset{}, err
	}
	matches := make([]db.AccountAsset, 0, 1)
	for _, asset := range assets {
		if asset.PlatformCode == platformCode && asset.AssetType == assetType {
			matches = append(matches, asset)
		}
	}
	if len(matches) == 0 {
		return db.AccountAsset{}, fmt.Errorf("%s", requiredCode)
	}
	if len(matches) != 1 {
		return db.AccountAsset{}, fmt.Errorf("%s", ambiguousCode)
	}
	if !matches[0].Enabled {
		return db.AccountAsset{}, fmt.Errorf("EGO_META_%s_ASSET_DISABLED", strings.ToUpper(platformCode))
	}
	return matches[0], nil
}

func validateStoredMetaAsset(asset db.AccountAsset, prefix string) error {
	if strings.TrimSpace(asset.Name) == "" {
		return fmt.Errorf("%s_ASSET_NAME_REQUIRED", prefix)
	}
	if strings.TrimSpace(asset.ExternalID) == "" {
		return fmt.Errorf("%s_ASSET_ID_REQUIRED", prefix)
	}
	if !isPureMetaExternalID(asset.ExternalID) {
		return fmt.Errorf("%s_ASSET_ID_INVALID", prefix)
	}
	return nil
}

func validateRequestedMetaAsset(asset db.AccountAsset, requestedName, requestedID, prefix string) error {
	if err := validateStoredMetaAsset(asset, prefix); err != nil {
		return err
	}
	requestedName = strings.TrimSpace(requestedName)
	requestedID = strings.TrimSpace(requestedID)
	if requestedName == "" {
		return fmt.Errorf("%s_ASSET_NAME_REQUIRED", prefix)
	}
	if requestedID == "" {
		return fmt.Errorf("%s_ASSET_ID_REQUIRED", prefix)
	}
	if !isPureMetaExternalID(requestedID) {
		return fmt.Errorf("%s_ASSET_ID_INVALID", prefix)
	}
	if requestedName != strings.TrimSpace(asset.Name) {
		return fmt.Errorf("%s_ASSET_NAME_MISMATCH", prefix)
	}
	if requestedID != strings.TrimSpace(asset.ExternalID) {
		return fmt.Errorf("%s_ASSET_ID_MISMATCH", prefix)
	}
	return nil
}

func (service *SpaceService) validateMetaPublishTarget(account db.Account, page PublishPage) (metaPublishTarget, error) {
	if account.PlatformCode != "meta" || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
		return metaPublishTarget{}, fmt.Errorf("EGO_META_ACCOUNT_UNAVAILABLE")
	}
	facebookPage, err := service.selectMetaAsset(account.ID, "facebook", "facebook_page", "EGO_META_FACEBOOK_ASSET_REQUIRED", "EGO_META_FACEBOOK_ASSET_AMBIGUOUS")
	if err != nil {
		return metaPublishTarget{}, err
	}
	if err := validateStoredMetaAsset(facebookPage, "EGO_META_FACEBOOK"); err != nil {
		return metaPublishTarget{}, err
	}
	if page.PlatformCode == "facebook" {
		if err := validateRequestedMetaAsset(facebookPage, page.AssetName, page.AssetExternalID, "EGO_META_FACEBOOK"); err != nil {
			return metaPublishTarget{}, err
		}
		return metaPublishTarget{FacebookPage: facebookPage}, nil
	}

	instagramAccount, err := service.selectMetaAsset(account.ID, "instagram", "instagram_account", "EGO_META_INSTAGRAM_ASSET_REQUIRED", "EGO_META_INSTAGRAM_ASSET_AMBIGUOUS")
	if err != nil {
		return metaPublishTarget{}, err
	}
	if err := validateRequestedMetaAsset(instagramAccount, page.AssetName, page.AssetExternalID, "EGO_META_INSTAGRAM"); err != nil {
		return metaPublishTarget{}, err
	}

	instagramConnected, err := service.connectedMetaAsset(instagramAccount, account.ID)
	if err != nil {
		return metaPublishTarget{}, err
	}
	facebookConnected, err := service.connectedMetaAsset(facebookPage, account.ID)
	if err != nil {
		return metaPublishTarget{}, err
	}
	if instagramConnected == nil && facebookConnected == nil {
		return metaPublishTarget{}, fmt.Errorf("EGO_META_INSTAGRAM_CONNECTION_REQUIRED")
	}
	if instagramConnected == nil || facebookConnected == nil ||
		instagramConnected.ID != facebookPage.ID || facebookConnected.ID != instagramAccount.ID {
		return metaPublishTarget{}, fmt.Errorf("EGO_META_INSTAGRAM_CONNECTION_NOT_BIDIRECTIONAL")
	}
	if strings.TrimSpace(instagramAccount.ConnectedAssetName) != strings.TrimSpace(facebookPage.Name) ||
		strings.TrimSpace(facebookPage.ConnectedAssetName) != strings.TrimSpace(instagramAccount.Name) {
		return metaPublishTarget{}, fmt.Errorf("EGO_META_INSTAGRAM_CONNECTION_IDENTITY_MISMATCH")
	}
	return metaPublishTarget{FacebookPage: facebookPage, InstagramAccount: instagramAccount}, nil
}

func (service *SpaceService) connectedMetaAsset(source db.AccountAsset, accountID int64) (*db.AccountAsset, error) {
	if source.ConnectedAssetID == 0 {
		return nil, nil
	}
	connected, err := service.db.GetAccountAsset(source.ConnectedAssetID)
	if err != nil || connected.AccountID != accountID {
		return nil, fmt.Errorf("EGO_META_INSTAGRAM_CONNECTION_ACCOUNT_MISMATCH")
	}
	return connected, nil
}

func selectPublishAccount(accounts []db.Account, platformCode string) (db.Account, bool) {
	if platformCode == "facebook" || platformCode == "instagram" {
		var selected db.Account
		matches := 0
		for _, account := range accounts {
			if account.PlatformCode == "meta" && account.Enabled && account.LoginState == db.AccountAuthLoggedIn {
				selected = account
				matches++
			}
		}
		return selected, matches == 1
	}
	var selected db.Account
	found := false
	for _, account := range accounts {
		if account.PlatformCode == platformCode {
			selected = account
			found = true
		}
	}
	return selected, found
}

type spaceResult struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"taskId"`
	Name      string `json:"name"`
	Ownership string `json:"ownership"`
	Done      bool   `json:"done"`
	Skipped   string `json:"skipped"`
}

type AccountObservation struct {
	AccountID         int64  `json:"account_id"`
	SpaceID           int64  `json:"space_id"`
	SnapshotAvailable bool   `json:"snapshot_available"`
	SnapshotLength    int    `json:"snapshot_length"`
	Verified          bool   `json:"verified"`
	EvidenceCode      string `json:"evidence_code"`
}

type LoginBatch struct {
	IdentityID            int64                               `json:"identity_id"`
	SpaceID               int64                               `json:"space_id"`
	AccountIDs            []int64                             `json:"account_ids"`
	PlatformCodes         []string                            `json:"platform_codes"`
	PreparedPlatformCodes []string                            `json:"prepared_platform_codes,omitempty"`
	PlatformStatuses      map[string]string                   `json:"platform_statuses,omitempty"`
	PlatformErrors        map[string]string                   `json:"platform_errors,omitempty"`
	PlatformClicked       map[string]bool                     `json:"platform_clicked,omitempty"`
	PlatformDiagnostics   map[string]PublishPrepareDiagnostic `json:"platform_diagnostics,omitempty"`
	State                 string                              `json:"state"`
}

const maxPublishPrepareDiagnosticBytes = 4096

func sanitizeDiagnosticText(value string, limit int, tail bool) string {
	value = strings.Map(func(character rune) rune {
		if character == '\u200b' || character == '\ufeff' {
			return -1
		}
		if character == '\u00a0' {
			return ' '
		}
		return character
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if tail {
		return string(runes[len(runes)-limit:])
	}
	return string(runes[:limit])
}

func validDiagnosticTag(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func sanitizePublishPrepareDiagnostic(platformCode, errorCode string, input *PublishPrepareDiagnostic) *PublishPrepareDiagnostic {
	if platformCode != "douyin" || !strings.HasPrefix(errorCode, "EGO_DOUYIN_TOPIC_") || input == nil {
		return nil
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > maxPublishPrepareDiagnosticBytes || len(input.Nodes) > 12 || len(input.Topics) > 4 {
		return nil
	}
	switch input.Branch {
	case "trigger", "suggestion_click", "verified_space":
	default:
		return nil
	}
	switch input.Stage {
	case "trigger", "suggestion", "precondition", "native_commit", "readback":
	default:
		return nil
	}
	result := &PublishPrepareDiagnostic{
		Branch: input.Branch, Stage: input.Stage,
		BodyTail: sanitizeDiagnosticText(input.BodyTail, 80, true),
		Topics:   make([]string, 0, len(input.Topics)),
		Nodes:    make([]PublishPrepareDiagnosticNode, 0, len(input.Nodes)),
	}
	for _, topic := range input.Topics {
		result.Topics = append(result.Topics, sanitizeDiagnosticText(topic, 80, false))
	}
	for _, node := range input.Nodes {
		tag := strings.ToLower(strings.TrimSpace(node.Tag))
		if !validDiagnosticTag(tag) {
			return nil
		}
		result.Nodes = append(result.Nodes, PublishPrepareDiagnosticNode{
			Tag:  tag,
			Text: sanitizeDiagnosticText(node.Text, 80, false),
			Attributes: PublishPrepareDiagnosticAttributes{
				DataMention:       sanitizeDiagnosticText(node.Attributes.DataMention, 80, false),
				DataTopicSelected: sanitizeDiagnosticText(node.Attributes.DataTopicSelected, 80, false),
				DataTopic:         sanitizeDiagnosticText(node.Attributes.DataTopic, 80, false),
				ContentEditable:   sanitizeDiagnosticText(node.Attributes.ContentEditable, 80, false),
			},
		})
	}
	bounded, err := json.Marshal(result)
	if err != nil || len(bounded) > maxPublishPrepareDiagnosticBytes {
		return nil
	}
	return result
}

type BrowserOpenResult struct {
	IdentityID    int64    `json:"identity_id"`
	SpaceID       int64    `json:"space_id"`
	PlatformCodes []string `json:"platform_codes"`
	Total         int      `json:"total"`
	Remaining     int      `json:"remaining"`
	NextOffset    int      `json:"next_offset"`
	State         string   `json:"state"`
	Reused        bool     `json:"reused"`
}

type LoginVerification struct {
	IdentityID int64        `json:"identity_id"`
	State      string       `json:"state"`
	Accounts   []db.Account `json:"accounts"`
}

type metaAssetObservation struct {
	PlatformCode          string `json:"platformCode"`
	Name                  string `json:"name"`
	Avatar                string `json:"avatar"`
	AvatarBase64          string `json:"avatarBase64"`
	ExternalID            string `json:"externalID"`
	ConnectedPlatformCode string `json:"connectedPlatformCode"`
}

type LoginQR struct {
	AccountID    int64  `json:"account_id"`
	PlatformCode string `json:"platform_code"`
	State        string `json:"state"`
	ReasonCode   string `json:"reason_code,omitempty"`
	ImageDataURL string `json:"image_data_url,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	PageOrigin   string `json:"page_origin,omitempty"`
}

type PublishPage struct {
	PlatformCode    string
	ContentType     string
	AssetExternalID string
	// AssetName is optional for legacy callers; when present it must exactly
	// match the formal account asset selected from the logged-in container.
	AssetName    string
	PublishAt    string
	SettingsJSON string
	Title        string
	Caption      string
	Body         string
	TagsJSON     string
	Media        []PublishMedia
}

func scheduledPublishTargetSupported(platformCode, contentType string) bool {
	return egopublish.NativeScheduleAdapterSupportedFor(platformCode, contentType)
}

func validateScheduledPublishAt(platformCode, contentType, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if !scheduledPublishTargetSupported(platformCode, contentType) {
		return fmt.Errorf("EGO_PUBLISH_SCHEDULE_UNVERIFIED")
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	leadMinutes := egopublish.NativeScheduleMinLeadMinutesFor(platformCode, contentType)
	minimum := time.Now().Add(time.Duration(leadMinutes) * time.Minute)
	if platformCode == "facebook" {
		minimum = time.Now().Truncate(time.Minute).Add(time.Duration(leadMinutes) * time.Minute)
	}
	if err != nil || parsed.Format(time.RFC3339) != raw || parsed.Second() != 0 || parsed.Nanosecond() != 0 || leadMinutes < 1 || parsed.Before(minimum) {
		return fmt.Errorf("EGO_PUBLISH_SCHEDULE_INVALID")
	}
	if platformCode == "facebook" {
		minute := time.Now().Truncate(time.Minute)
		if parsed.Before(minute.Add(20*time.Minute)) || parsed.After(minute.Add(29*24*time.Hour)) {
			return fmt.Errorf("EGO_PUBLISH_SCHEDULE_INVALID")
		}
	}
	return nil
}

// ValidateScheduledPublishAt exposes the single reviewed scheduling contract
// to thin API adapters so they cannot reject a target the EgoLite worker accepts.
func ValidateScheduledPublishAt(platformCode, contentType, raw string) error {
	return validateScheduledPublishAt(strings.TrimSpace(platformCode), strings.TrimSpace(contentType), strings.TrimSpace(raw))
}

// validateMetaPublishMedia keeps the immutable input shape aligned with the
// Business Suite composer. Graph posts preserve the caller's image order;
// video composers accept exactly one source video and at most one optional
// image cover, never an ambiguous mixed upload.
func validateMetaPublishMedia(contentType string, media []PublishMedia) error {
	switch contentType {
	case "graph_text":
		if len(media) == 0 {
			return fmt.Errorf("EGO_PUBLISH_MEDIA_REQUIRED")
		}
		for _, item := range media {
			if item.Kind != "image" {
				return fmt.Errorf("EGO_META_MEDIA_INVALID")
			}
		}
		return nil
	case "video":
		if len(media) == 0 {
			return fmt.Errorf("EGO_PUBLISH_MEDIA_REQUIRED")
		}
		if len(media) > 2 || media[0].Kind != "video" || media[0].Role != "video" {
			return fmt.Errorf("EGO_META_MEDIA_INVALID")
		}
		if len(media) == 2 && (media[1].Kind != "image" || media[1].Role != "cover") {
			return fmt.Errorf("EGO_META_MEDIA_INVALID")
		}
		return nil
	default:
		return fmt.Errorf("EGO_META_MEDIA_INVALID")
	}
}

// validateDouyinPublishMedia keeps the frozen input shape within the reviewed
// creator image and video composer contracts. Graph posts may carry ordered
// images; the video composer accepts one source video only.
func validateDouyinPublishMedia(contentType string, media []PublishMedia) error {
	if len(media) == 0 {
		return fmt.Errorf("EGO_PUBLISH_MEDIA_REQUIRED")
	}
	switch contentType {
	case "graph_text":
		if len(media) > 30 {
			return fmt.Errorf("EGO_DOUYIN_MEDIA_INVALID")
		}
		for _, item := range media {
			if item.Kind != "image" {
				return fmt.Errorf("EGO_DOUYIN_MEDIA_INVALID")
			}
		}
	case "video":
		if len(media) != 1 || media[0].Kind != "video" {
			return fmt.Errorf("EGO_DOUYIN_MEDIA_INVALID")
		}
	default:
		return fmt.Errorf("EGO_DOUYIN_MEDIA_INVALID")
	}
	return nil
}

func normalizeYouTubeSettings(contentType, settingsJSON string) (platforms.YouTubeSettings, error) {
	normalized, err := platforms.NormalizeSettingsForContentType("youtube", contentType, settingsJSON)
	if err != nil {
		return platforms.YouTubeSettings{}, fmt.Errorf("EGO_YOUTUBE_SETTINGS_INVALID")
	}
	var settings platforms.YouTubeSettings
	if err := json.Unmarshal([]byte(normalized), &settings); err != nil ||
		(settings.Audience != "not_made_for_kids" && settings.Audience != "made_for_kids") ||
		(settings.Visibility != "private" && settings.Visibility != "unlisted" && settings.Visibility != "public") {
		return platforms.YouTubeSettings{}, fmt.Errorf("EGO_YOUTUBE_SETTINGS_INVALID")
	}
	return settings, nil
}

func normalizeTikTokSettings(contentType, settingsJSON string) (platforms.TikTokSettings, error) {
	normalized, err := platforms.NormalizeSettingsForContentType("tiktok", contentType, settingsJSON)
	if err != nil {
		return platforms.TikTokSettings{}, fmt.Errorf("EGO_TIKTOK_SETTINGS_INVALID")
	}
	var settings platforms.TikTokSettings
	if err := json.Unmarshal([]byte(normalized), &settings); err != nil ||
		(settings.Visibility != "public" && settings.Visibility != "friends" && settings.Visibility != "private") {
		return platforms.TikTokSettings{}, fmt.Errorf("EGO_TIKTOK_SETTINGS_INVALID")
	}
	return settings, nil
}

func validateYouTubePublishMedia(media []PublishMedia) error {
	videoCount := 0
	coverCount := 0
	for _, item := range media {
		switch item.Role {
		case "video":
			if item.Kind != "video" {
				return fmt.Errorf("EGO_YOUTUBE_MEDIA_INVALID")
			}
			videoCount++
		case "cover":
			if item.Kind != "image" {
				return fmt.Errorf("EGO_YOUTUBE_MEDIA_INVALID")
			}
			coverCount++
		default:
			return fmt.Errorf("EGO_YOUTUBE_MEDIA_INVALID")
		}
	}
	if videoCount != 1 || coverCount > 1 {
		return fmt.Errorf("EGO_YOUTUBE_MEDIA_INVALID")
	}
	return nil
}

func validateTikTokPublishMedia(media []PublishMedia) error {
	videoCount := 0
	coverCount := 0
	for _, item := range media {
		switch item.Role {
		case "video":
			if item.Kind != "video" {
				return fmt.Errorf("EGO_TIKTOK_MEDIA_INVALID")
			}
			videoCount++
		case "cover":
			if item.Kind != "image" {
				return fmt.Errorf("EGO_TIKTOK_MEDIA_INVALID")
			}
			coverCount++
		default:
			return fmt.Errorf("EGO_TIKTOK_MEDIA_INVALID")
		}
	}
	if videoCount != 1 || coverCount > 1 {
		return fmt.Errorf("EGO_TIKTOK_MEDIA_INVALID")
	}
	return nil
}

// validateBilibiliPublishMedia deliberately accepts only one video. Bilibili's
// automatic cover and declaration controls have not yet been reviewed in an
// EgoLite Space, so callers must not silently apply them during prepare.
func validateBilibiliPublishMedia(media []PublishMedia) error {
	if len(media) != 1 || media[0].Kind != "video" || media[0].Role != "video" {
		return fmt.Errorf("EGO_BILIBILI_MEDIA_INVALID")
	}
	return nil
}

func validateWechatVideoPublishMedia(media []PublishMedia) error {
	if len(media) != 1 || media[0].Kind != "video" || media[0].Role != "video" {
		return fmt.Errorf("EGO_WECHAT_VIDEO_MEDIA_INVALID")
	}
	return nil
}

func validateWechatVideoSettings(settingsJSON string) error {
	if strings.TrimSpace(settingsJSON) == "" || strings.TrimSpace(settingsJSON) == "{}" {
		return nil
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil || settings == nil {
		return fmt.Errorf("EGO_WECHAT_VIDEO_SETTINGS_INVALID")
	}
	for key, raw := range settings {
		switch key {
		case "location", "collection", "link_address", "activity", "music":
			var value string
			if err := json.Unmarshal(raw, &value); err != nil || value != "" {
				return fmt.Errorf("EGO_WECHAT_VIDEO_SETTINGS_UNSUPPORTED")
			}
		case "link_type":
			var value string
			if err := json.Unmarshal(raw, &value); err != nil || value != "none" {
				return fmt.Errorf("EGO_WECHAT_VIDEO_SETTINGS_UNSUPPORTED")
			}
		case "declare_original":
			var value bool
			if err := json.Unmarshal(raw, &value); err != nil || value {
				return fmt.Errorf("EGO_WECHAT_VIDEO_SETTINGS_UNSUPPORTED")
			}
		default:
			// Do not invent support for fields that the current schema does not
			// expose (including video_annotation).
			return fmt.Errorf("EGO_WECHAT_VIDEO_SETTINGS_UNSUPPORTED")
		}
	}
	return nil
}

func normalizeWechatMPSettings(settingsJSON string) (platforms.WechatMPSettings, error) {
	settings := platforms.WechatMPSettings{PublishType: "publish", ContentSource: "none"}
	if strings.TrimSpace(settingsJSON) == "" || strings.TrimSpace(settingsJSON) == "{}" {
		return settings, nil
	}
	var rawSettings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &rawSettings); err != nil || rawSettings == nil {
		return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_INVALID")
	}
	for key, raw := range rawSettings {
		switch key {
		case "author":
			if err := json.Unmarshal(raw, &settings.Author); err != nil || utf8.RuneCountInString(settings.Author) > 64 || !utf8.ValidString(settings.Author) {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_INVALID")
			}
			for _, r := range settings.Author {
				if unicode.IsControl(r) {
					return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_INVALID")
				}
			}
		case "original_link":
			if err := json.Unmarshal(raw, &settings.OriginalLink); err != nil || settings.OriginalLink != "" {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
			}
		case "comment_enabled":
			if err := json.Unmarshal(raw, &settings.CommentEnabled); err != nil || settings.CommentEnabled {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
			}
		case "declare_original":
			if err := json.Unmarshal(raw, &settings.DeclareOriginal); err != nil || settings.DeclareOriginal {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
			}
		case "allow_reprint":
			if err := json.Unmarshal(raw, &settings.AllowReprint); err != nil || settings.AllowReprint {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
			}
		case "publish_type":
			if err := json.Unmarshal(raw, &settings.PublishType); err != nil || settings.PublishType != "publish" {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
			}
		case "collection":
			if err := json.Unmarshal(raw, &settings.Collection); err != nil || settings.Collection != "" {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
			}
		case "content_source":
			if err := json.Unmarshal(raw, &settings.ContentSource); err != nil || settings.ContentSource != "none" {
				return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
			}
		default:
			return platforms.WechatMPSettings{}, fmt.Errorf("EGO_WECHAT_MP_SETTINGS_UNSUPPORTED")
		}
	}
	return settings, nil
}

func validateWechatMPPublishMedia(media []PublishMedia) error {
	if len(media) != 1 || media[0].Kind != "image" || (media[0].Role != "content" && media[0].Role != "cover") {
		return fmt.Errorf("EGO_WECHAT_MP_MEDIA_INVALID")
	}
	return nil
}

func validateXHSPublishMedia(contentType string, media []PublishMedia) error {
	switch contentType {
	case "graph_text":
		if len(media) < 1 || len(media) > 18 {
			return fmt.Errorf("EGO_XHS_MEDIA_INVALID")
		}
		for _, item := range media {
			if item.Kind != "image" || item.Role != "image" {
				return fmt.Errorf("EGO_XHS_MEDIA_INVALID")
			}
		}
	case "video":
		if len(media) != 1 || media[0].Kind != "video" || media[0].Role != "video" {
			return fmt.Errorf("EGO_XHS_MEDIA_INVALID")
		}
	default:
		return fmt.Errorf("EGO_XHS_CONTENT_TYPE_UNSUPPORTED")
	}
	return nil
}

func validateXHSSettings(settingsJSON string) error {
	settings, err := platforms.ParseXHSSettings(settingsJSON)
	if err != nil {
		return fmt.Errorf("EGO_XHS_SETTINGS_UNVERIFIED")
	}
	if settings != (platforms.XHSSettings{Visibility: "public", CommentEnabled: true, ContentSource: "none"}) {
		return fmt.Errorf("EGO_XHS_SETTINGS_UNVERIFIED")
	}
	return nil
}

type PublishManagerInspection struct {
	AccountID            int64  `json:"account_id"`
	PlatformCode         string `json:"platform_code"`
	Location             string `json:"location,omitempty"`
	InspectionStatus     string `json:"inspection_status"`
	MatchingTitle        bool   `json:"matching_title"`
	PublishedState       bool   `json:"published_state"`
	ScheduledState       bool   `json:"scheduled_state"`
	EffectiveScheduledAt string `json:"effective_scheduled_at,omitempty"`
	ObservedStatus       string `json:"observed_status,omitempty"`
	PostURL              string `json:"post_url,omitempty"`
	ExternalPostID       string `json:"external_post_id,omitempty"`
	ScreenshotPath       string `json:"-"`
}

// PublishContentDeletion is returned only after the exact scheduled manager
// row is no longer present and a post-delete screenshot has been persisted.
type PublishContentDeletion struct {
	RecordID       int64  `json:"record_id"`
	AccountID      int64  `json:"account_id"`
	PlatformCode   string `json:"platform_code"`
	Deleted        bool   `json:"deleted"`
	ScreenshotPath string `json:"-"`
}

type publishManagerRuntimeResult struct {
	Done                 bool   `json:"done"`
	CompletionAttempted  bool   `json:"completionAttempted"`
	CompletionDone       bool   `json:"completionDone"`
	Location             string `json:"location"`
	InspectionStatus     string `json:"inspectionStatus"`
	MatchingTitle        bool   `json:"matchingTitle"`
	PublishedState       bool   `json:"publishedState"`
	ScheduledState       bool   `json:"scheduledState"`
	EffectiveScheduledAt string `json:"effectiveScheduledAt"`
	ObservedStatus       string `json:"observedStatus"`
	PostURL              string `json:"postURL"`
	ExternalPostID       string `json:"externalPostID"`
	ScreenshotBase64     string `json:"screenshotBase64"`
}

type publishContentDeleteRuntimeResult struct {
	Done                    bool   `json:"done"`
	Deleted                 bool   `json:"deleted"`
	MatchingTitle           bool   `json:"matchingTitle"`
	MatchingScheduledState  bool   `json:"matchingScheduledState"`
	MatchingScheduledMinute bool   `json:"matchingScheduledMinute"`
	RowCandidateCount       int    `json:"rowCandidateCount"`
	DeleteMenuCount         int    `json:"deleteMenuCount"`
	ConfirmButtonCount      int    `json:"confirmButtonCount"`
	FinalClickAttempted     bool   `json:"finalClickAttempted"`
	ErrorCode               string `json:"errorCode"`
	ScreenshotBase64        string `json:"screenshotBase64"`
}

func NewSpaceService(database *db.DB, runtime ProgramRunner) *SpaceService {
	pinProgramRunnerDB(database, runtime)
	return &SpaceService{db: database, runtime: runtime}
}

func (service *SpaceService) bindIdentitySpace(ctx context.Context, profileID, profileName, taskName string) (spaceResult, error) {
	profileIDJSON, _ := json.Marshal(profileID)
	profileNameJSON, _ := json.Marshal(profileName)
	taskNameJSON, _ := json.Marshal(taskName)
	program := fmt.Sprintf(`const profileResult = await listProfiles()
const profiles = Array.isArray(profileResult)
  ? profileResult
  : (Array.isArray(profileResult?.profiles) ? profileResult.profiles : [])
const normalizeProfileName = (value) => String(value ?? "").replace(/\s+/g, "").trim().toLowerCase()
const expectedProfileName = normalizeProfileName(%s)
const expectedProfileID = %s
const profile = expectedProfileID
  ? profiles.find((item) => String(item.id ?? "") === String(expectedProfileID))
  : profiles.find((item) => {
  const norm = normalizeProfileName(item.name)
  if (norm === expectedProfileName) return true
  if (norm.startsWith(expectedProfileName + "(") || norm.startsWith(expectedProfileName + "（")) return true
  const numMatch = expectedProfileName.match(/\d+/)
  if (numMatch) {
    const num = numMatch[0]
    const idLower = String(item.id ?? "").toLowerCase()
    if (idLower === "profile " + num || idLower === "profile-" + num || idLower === num) return true
    if (norm === "浏览器" + num || norm === "profile" + num || norm.startsWith("浏览器" + num) || norm.startsWith("profile" + num)) return true
  }
  return false
  })
if (!profile?.id) throw new Error("EGO_PROFILE_NOT_FOUND")
const taskName = %s
const spaces = await listTaskSpaces()
const boundSpace = spaces.find((item) =>
  String(item.profileId ?? "") === String(profile.id) &&
  (String(item.name ?? "") === taskName || String(item.taskId ?? "") === taskName)
)
let task = boundSpace
if (!task) {
  if (typeof ego === "undefined" || typeof ego.createTaskSpace !== "function") {
    throw new Error("EGO_PROFILE_BINDING_UNAVAILABLE")
  }
  task = await ego.createTaskSpace(taskName, profile.id)
}
cliLog(%q + JSON.stringify({
  id: task.id,
  taskId: String(task.taskId ?? task.id),
  name: task.name,
  ownership: task.ownership
}))
`, profileNameJSON, profileIDJSON, taskNameJSON, resultPrefix)
	var result spaceResult
	if err := service.runtime.Run(ctx, program, &result); err != nil {
		return spaceResult{}, err
	}
	if result.ID < 0 || result.TaskID == "" {
		return spaceResult{}, fmt.Errorf("EGO_SPACE_RESULT_INVALID")
	}
	return result, nil
}

func (service *SpaceService) EnsureIdentitySpace(ctx context.Context, identityID int64) (*db.EgoSpace, error) {
	if service == nil || service.db == nil || service.runtime == nil || identityID <= 0 {
		return nil, fmt.Errorf("EGO_SPACE_INPUT_INVALID")
	}
	identity, err := service.db.GetEgoIdentity(identityID)
	if err != nil {
		return nil, err
	}
	taskName := fmt.Sprintf("alpha-nexus/matrix-%d", identityID)
	// The read below and the create it may trigger are one decision. Holding the
	// lock across the runtime bind keeps two concurrent callers from both
	// deciding "no Space yet" and leaving one of the two Spaces orphaned.
	service.ensureMu.Lock()
	defer service.ensureMu.Unlock()
	existing, err := service.db.GetEgoSpaceByIdentity(identityID)
	if err == nil {
		if strings.HasPrefix(existing.Name, "alpha-nexus/matrix-") {
			taskName = existing.Name
		}
		result, err := service.bindIdentitySpace(ctx, identity.ProfileID, identity.Name, taskName)
		if err != nil {
			return nil, err
		}
		if result.ID == existing.SpaceID && result.TaskID == existing.TaskID && existing.Name == taskName {
			if result.Ownership == "user" || result.Ownership == "agentDelegatedToUser" {
				return service.db.UpdateEgoSpaceStateForIdentity(identityID, "agentDelegatedToUser", "manual_takeover", ErrSpaceUserControlled)
			}
			return existing, nil
		}
		return service.db.RebindEgoSpaceForIdentity(identityID, result.ID, result.TaskID, taskName)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	result, err := service.bindIdentitySpace(ctx, identity.ProfileID, identity.Name, taskName)
	if err != nil {
		return nil, err
	}
	return service.db.CreateEgoSpaceForIdentity(identityID, result.ID, result.TaskID, taskName)
}

func (service *SpaceService) EnsureAccountSpace(ctx context.Context, accountID int64) (*db.EgoSpace, error) {
	if service == nil || service.db == nil || accountID <= 0 {
		return nil, fmt.Errorf("EGO_SPACE_INPUT_INVALID")
	}
	account, err := service.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	return service.EnsureIdentitySpace(ctx, account.IdentityID)
}

func (service *SpaceService) OpenLoginAndHandoff(ctx context.Context, accountID int64) (*db.EgoSpace, error) {
	account, err := service.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	if _, err := service.OpenLoginBatch(ctx, account.IdentityID, []string{account.PlatformCode}); err != nil {
		return nil, err
	}
	return service.db.GetEgoSpaceByIdentity(account.IdentityID)
}

// OpenLoginQR opens or re-inspects one platform login page and returns only a
// cropped PNG held in memory. The QR is never written to Alpha-Nexus storage.
func (service *SpaceService) OpenLoginQR(ctx context.Context, accountID int64, open bool) (*LoginQR, error) {
	if service == nil || service.db == nil || service.runtime == nil || accountID <= 0 {
		return nil, fmt.Errorf("EGO_LOGIN_QR_INPUT_INVALID")
	}
	account, err := service.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	if !account.Enabled {
		return nil, fmt.Errorf("EGO_LOGIN_QR_ACCOUNT_DISABLED")
	}
	platform, err := platforms.Find(account.PlatformCode)
	if err != nil {
		return nil, err
	}
	targetURL := accountManagerURL(*account, platform)
	if targetURL == "" {
		return nil, fmt.Errorf("EGO_PLATFORM_LOGIN_UNAVAILABLE")
	}
	space, err := service.EnsureIdentitySpace(ctx, account.IdentityID)
	if err != nil {
		return nil, err
	}
	// The generated program releases the Space on every exit it can see. A crash,
	// cancel or kill never reaches that code, so the claim is released again here
	// and a Space cannot stay agent-occupied because a QR read died.
	spaceReleased := false
	defer func() {
		if spaceReleased {
			return
		}
		service.releaseSpaceAfterFailure(ctx, space.SpaceID)
	}()
	urlJSON, _ := json.Marshal(targetURL)
	platformCodeJSON, _ := json.Marshal(account.PlatformCode)
	loginRuleJSON, _ := json.Marshal(platformReviewedLoginRule(account.PlatformCode))
	strictLoginEvidenceJSON, _ := json.Marshal(platformRequiresReviewedLoginEvidence(account.PlatformCode))
	var selection string
	var navigation string
	if open {
		selection = fmt.Sprintf(`const spaces = await listTaskSpaces()
const currentSpace = spaces.find((item) => Number(item.id) === %d)
if (!currentSpace) throw new Error(%s)
if (currentSpace && (currentSpace.ownership === "agentDelegatedToUser" || currentSpace.ownership === "user")) {
  await takeOverTaskSpace(%d)
} else {
  await useOrCreateTaskSpace(%d)
}
`, space.SpaceID, jsErrorLiteral(ErrSpaceBindingMissing), space.SpaceID, space.SpaceID)
		navigation = fmt.Sprintf(`const staleTabs = await listTabs()
while (staleTabs.length > 1) {
  const stale = staleTabs.shift()
  await closeTab(stale.targetId ?? stale.id)
}
await openOrReuseTab(%s, { wait: true, timeout: 30 })
await gotoAndWait(%s, { timeout: 30 })
`, urlJSON, urlJSON)
	} else {
		selection = fmt.Sprintf(`const spaces = await listTaskSpaces()
const currentSpace = spaces.find((item) => Number(item.id) === %d)
if (!currentSpace) throw new Error(%s)
if (currentSpace.ownership === "agentDelegatedToUser" || currentSpace.ownership === "user") {
  await takeOverTaskSpace(%d)
} else {
  await useOrCreateTaskSpace(%d)
}
`, space.SpaceID, jsErrorLiteral(ErrSpaceBindingMissing), space.SpaceID, space.SpaceID)
		hostsJSON, _ := json.Marshal(platformOriginHosts(account.PlatformCode))
		suffixesJSON, _ := json.Marshal(platformOriginSuffixes(account.PlatformCode))
		navigation = fmt.Sprintf(`const allowedHosts = %s
const allowedSuffixes = %s
const matchingTabs = (await listTabs()).filter((tab) => {
  try {
    const host = new URL(String(tab.url ?? "")).hostname.toLowerCase().replace(/\.$/, "")
    return allowedHosts.includes(host) || allowedSuffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
  } catch { return false }
})
const matchingTab = matchingTabs[matchingTabs.length - 1]
const matchingTargetId = String(matchingTab?.targetId ?? matchingTab?.id ?? "")
if (!matchingTargetId) throw new Error("EGO_LOGIN_QR_PLATFORM_TAB_MISSING")
await switchTab(matchingTargetId)
const activeTab = await currentTab()
const activeTargetId = String(activeTab?.targetId ?? activeTab?.id ?? "")
if (activeTargetId !== matchingTargetId) throw new Error("EGO_LOGIN_QR_PLATFORM_TAB_MISMATCH")
`, string(hostsJSON), string(suffixesJSON))
	}
	// Everything that can claim, navigate or throw runs inside one try block so
	// the Space is handed back on every exit, not only the happy path. A throw
	// used to skip the handoff and leave the Space agent-occupied forever.
	program := fmt.Sprintf(`let loginQRFailure = null
const loginQR = { state: "manual_takeover", imageBase64: "", imageMime: "", pageOrigin: "" }
try {
%s%sconst platformCode = %s
const loginRule = %s
const strictLoginEvidence = %s
const matchesReviewedDashboard = (rule, pageURL, bodyText) => {
  if (!rule?.host) return false
  try {
    const parsed = new URL(pageURL)
    const host = parsed.hostname.toLowerCase().replace(/\.$/, "")
    return host === rule.host &&
      (!rule.pathPrefixes?.length || rule.pathPrefixes.some((prefix) => parsed.pathname.startsWith(prefix))) &&
      (rule.requiredText || []).every((token) => bodyText.includes(token))
  } catch { return false }
}
const clickResult = await js(String.raw`+"`"+`(() => {
  const visible = (element) => {
    const rect = element.getBoundingClientRect()
    const style = getComputedStyle(element)
    return rect.width > 0 && rect.height > 0 && style.display !== "none" && style.visibility !== "hidden"
  }
  const expiryPattern = /二维码\s*(?:已)?\s*(?:失效|过期)|点击\s*(?:刷新|重试)|刷新二维码|QR\s*(?:code\s*)?expired|refresh\s*QR/i
  const findExpiredControl = () => {
    const candidates = [...document.querySelectorAll(
      'button,a,[role="button"],[role="link"],[class*="expired" i],[class*="refresh" i],[class*="qrcode" i],[class*="qr-code" i],[class*="mask" i]'
    )]
    return candidates.find((element) => {
      const text = (element.textContent || "").replace(/\s+/g, " ").trim()
      return visible(element) && text.length <= 80 && expiryPattern.test(text)
    }) || null
  }
  const refreshExpiredQR = () => {
    const expired = findExpiredControl()
    if (!expired) return false
    const clickable = expired.closest('button,a,[role="button"],[role="link"]') || expired
    clickable.click()
    return true
  }
  const refreshed = refreshExpiredQR()
  clearInterval(window.__alphaNexusQRRefreshTimer)
  const refreshDeadline = Date.now() + 15 * 60 * 1000
  window.__alphaNexusQRRefreshTimer = setInterval(() => {
    if (Date.now() >= refreshDeadline) {
      clearInterval(window.__alphaNexusQRRefreshTimer)
      return
    }
    refreshExpiredQR()
  }, 3000)
  if (refreshed) return { clicked: true, refreshed: true }
  const choices = [...document.querySelectorAll('button,[role="button"],[role="tab"],a')]
  const match = choices.find((element) => visible(element) &&
    /^(扫码登录|二维码登录|扫码|QR\s*(code)?|Use QR code)$/i.test((element.textContent || "").trim()))
  if (!match) return { clicked: false, refreshed: false }
  match.click()
  return { clicked: true, refreshed: false }
})()`+"`"+`)
if (clickResult?.clicked) await new Promise((resolve) => setTimeout(resolve, clickResult.refreshed ? 1200 : 700))
const page = await pageInfo()
const snapshot = JSON.stringify(await snapshotText())
const observed = await js(String.raw`+"`"+`(() => {
  const visible = (element) => {
    const rect = element.getBoundingClientRect()
    const style = getComputedStyle(element)
    return rect.width > 0 && rect.height > 0 && style.display !== "none" &&
      style.visibility !== "hidden" && Number(style.opacity || "1") > 0
  }
  const selectors = [
    'canvas', 'img[src^="data:image"]', 'img[src*="qr" i]',
    '[class*="qrcode" i]', '[class*="qr-code" i]', '[class~="qr"]',
    '[id*="qrcode" i]', '[aria-label*="二维码"]', '[aria-label*="QR" i]'
  ]
	  const candidates = [...new Set(selectors.flatMap((selector) => [...document.querySelectorAll(selector)]))]
	    .filter(visible)
	    .map((element) => {
      const rect = element.getBoundingClientRect()
      const descriptor = [
        element.id, String(element.className || ""), element.getAttribute("aria-label") || "",
        element.getAttribute("alt") || "", element.getAttribute("src") || ""
      ].join(" ")
      const ratio = rect.width / Math.max(rect.height, 1)
      let score = ratio >= 0.72 && ratio <= 1.28 ? 2 : 0
      if (/qr|qrcode|二维码/i.test(descriptor)) score += 4
      if (element.tagName === "CANVAS") score += 2
      if (element.tagName === "IMG" && /^data:image/i.test(element.getAttribute("src") || "")) score += 1
	      return { element, rect, score }
    })
    .filter((item) => item.score >= 3 && item.rect.width >= 96 && item.rect.height >= 96 &&
      item.rect.width <= 720 && item.rect.height <= 720)
    .sort((left, right) => right.score - left.score || (right.rect.width * right.rect.height) - (left.rect.width * left.rect.height))
	  const best = candidates[0]
  const bodyText = (document.body?.innerText || "").slice(0, 12000)
  const dashboard = /内容管理|作品管理|数据中心|发布作品|创作中心|creator dashboard|channel dashboard/i.test(bodyText)
  const loginForm = Boolean(document.querySelector('input[type="password"],input[autocomplete="one-time-code"]')) ||
    /登录|sign in|log in|扫码|二维码/i.test(bodyText)
	  if (!best) return { clip: null, imageData: "", bodyText, dashboard, loginForm }
	  best.element.scrollIntoView({ block: "center", inline: "center" })
	  const rect = best.element.getBoundingClientRect()
	  let imageData = ""
	  try {
	    if (best.element.tagName === "CANVAS" && typeof best.element.toDataURL === "function") {
	      imageData = best.element.toDataURL("image/png")
	    } else if (best.element.tagName === "IMG" && /^data:image\/(?:png|jpe?g);base64,/i.test(best.element.src || "")) {
	      imageData = best.element.src
	    }
	  } catch {}
	  const padding = 12
	  const x = Math.max(0, rect.x - padding)
	  const y = Math.max(0, rect.y - padding)
	  const width = Math.min(window.innerWidth - x, rect.width + padding * 2)
	  const height = Math.min(window.innerHeight - y, rect.height + padding * 2)
	  if (width <= 0 || height <= 0) return { clip: null, imageData, bodyText, dashboard, loginForm }
	  return {
	    clip: {
	      x,
	      y,
	      width,
	      height,
      scale: 1
    },
	    imageData,
	    bodyText,
	    dashboard,
    loginForm
  }
})()`+"`"+`)
loginQR.pageOrigin = typeof page?.url === "string" ? page.url : %s
if (loginRule?.host) {
  observed.dashboard = matchesReviewedDashboard(loginRule, loginQR.pageOrigin, String(observed?.bodyText || ""))
}
if (strictLoginEvidence && !loginRule?.host) {
  observed.dashboard = false
}
const directQR = String(observed?.imageData || "").match(/^data:image\/(png|jpe?g);base64,([A-Za-z0-9+/=]+)$/i)
const loginConfirmed = observed?.dashboard && (loginRule?.host || !observed?.loginForm) &&
  !/captcha|验证码|安全验证|security verification/i.test(snapshot)
let capturedQR = null
if (!directQR && observed?.clip) {
  try {
    const screenshot = await cdp("Page.captureScreenshot", {
      format: "png",
      fromSurface: true,
      captureBeyondViewport: false,
      clip: observed.clip
    })
    if (typeof screenshot?.data === "string" && screenshot.data) capturedQR = screenshot.data
  } catch {}
}
let embeddedQR = null
if (!directQR && !capturedQR && !loginConfirmed) {
  const findEmbeddedQR = async () => {
    const documentTree = await cdp("DOM.getDocument", { depth: -1, pierce: true })
    const candidates = []
    const retryNodeIds = []
    const collectImages = (node, ancestors = [], parent = null) => {
      if (!node || typeof node !== "object") return
      const attrs = {}
      for (let index = 0; index < (node.attributes || []).length; index += 2) {
        attrs[node.attributes[index]] = node.attributes[index + 1]
      }
      if (parent?.backendNodeId && /二维码失效|点击重试|QR\s*(?:code\s*)?expired|refresh\s*QR/i.test(String(node.nodeValue || ""))) {
        retryNodeIds.push(parent.backendNodeId)
      }
      const descriptor = [...ancestors, attrs.id || "", attrs.class || "", attrs.alt || ""].join(" ")
      const match = node.nodeName === "IMG" && String(attrs.src || "").match(
        /^data:image\/(png|jpe?g);base64,([A-Za-z0-9+/=]+)$/i
      )
      let trustedWechatFrame = false
      if (platformCode === "wechat_mp" && node.nodeName === "IFRAME") {
        try {
          const source = new URL(String(attrs.src || ""), loginQR.pageOrigin || %s)
          trustedWechatFrame = source.hostname === "open.weixin.qq.com" && source.pathname === "/connect/qrconnect"
        } catch {}
      }
      if ((match && /qr|qrcode|二维码|scan|login-code|wechat|applets/i.test(descriptor)) || trustedWechatFrame) {
        candidates.push({
          nodeId: node.nodeId,
          backendNodeId: node.backendNodeId,
          mime: match ? match[1].toLowerCase() : "png",
          data: match ? match[2] : "",
          kind: trustedWechatFrame ? "wechat_qr_frame" : "data_image",
          descriptor
        })
      }
      const nextAncestors = [...ancestors.slice(-7), attrs.id || "", attrs.class || ""]
      for (const child of node.children || []) collectImages(child, nextAncestors, node)
      if (node.contentDocument) collectImages(node.contentDocument, nextAncestors, node)
      for (const shadow of node.shadowRoots || []) collectImages(shadow, nextAncestors, node)
    }
    collectImages(documentTree?.root)
    const measured = []
    for (const candidate of candidates) {
      if ((candidate.data && candidate.data.length > 700000) || (!candidate.data && candidate.kind !== "wechat_qr_frame")) continue
      try {
        const nodeRef = candidate.backendNodeId ?
          { backendNodeId: candidate.backendNodeId } : { nodeId: candidate.nodeId }
        const box = (await cdp("DOM.getBoxModel", nodeRef))?.model
        const width = Number(box?.width || 0), height = Number(box?.height || 0)
        const ratio = width / Math.max(height, 1)
        const approvedShape = candidate.kind === "wechat_qr_frame"
          ? width >= 180 && height >= 120 && width <= 720 && height <= 720 && ratio >= 0.72 && ratio <= 2.5
          : width >= 96 && height >= 96 && width <= 720 && height <= 720 && ratio >= 0.72 && ratio <= 1.28
        const quad = box?.border || box?.content || []
        const xs = quad.filter((_, index) => index %% 2 === 0), ys = quad.filter((_, index) => index %% 2 === 1)
        const x = Math.min(...xs), y = Math.min(...ys)
        const clip = xs.length === 4 && ys.length === 4 && Number.isFinite(x) && Number.isFinite(y)
          ? { x: Math.max(0, x), y: Math.max(0, y), width, height, scale: 1 }
          : null
        if (box && approvedShape && (candidate.data || clip)) {
          measured.push({ ...candidate, clip, area: width * height })
        }
      } catch {}
    }
    return {
      qr: measured.sort((left, right) => right.area - left.area || right.data.length - left.data.length)[0] || null,
      retryBackendNodeId: retryNodeIds[0] || 0
    }
  }
	  let refreshedExpiredQR = false
	  try {
	    for (let attempt = 0; attempt < 25 && !embeddedQR; attempt++) {
	      const found = await findEmbeddedQR()
	      embeddedQR = found.qr
	      if (!embeddedQR && !refreshedExpiredQR && found.retryBackendNodeId) {
	        try {
	          const resolved = await cdp("DOM.resolveNode", { backendNodeId: found.retryBackendNodeId })
	          await cdp("Runtime.callFunctionOn", {
	            objectId: resolved?.object?.objectId,
	            functionDeclaration: "function(){ this.click(); }",
	            returnByValue: true
	          })
	          refreshedExpiredQR = true
	        } catch {}
	      }
	      if (!embeddedQR && attempt < 24) await new Promise((resolve) => setTimeout(resolve, 300))
	    }
	  } catch {}
	  if (!capturedQR && embeddedQR?.clip && !embeddedQR?.data) {
	    try {
	      const screenshot = await cdp("Page.captureScreenshot", {
	        format: "png", fromSurface: true, captureBeyondViewport: true, clip: embeddedQR.clip
	      })
	      if (typeof screenshot?.data === "string" && screenshot.data) capturedQR = screenshot.data
	    } catch {}
	  }
}
if (directQR) {
	  loginQR.state = "qr_ready"
	  loginQR.imageBase64 = directQR[2]
	  loginQR.imageMime = directQR[1].toLowerCase() === "png" ? "image/png" : "image/jpeg"
	} else if (capturedQR) {
	  loginQR.state = "qr_ready"
	  loginQR.imageBase64 = capturedQR
	  loginQR.imageMime = "image/png"
} else if (embeddedQR) {
  loginQR.state = "qr_ready"
  loginQR.imageBase64 = embeddedQR.data
  loginQR.imageMime = embeddedQR.mime === "png" ? "image/png" : "image/jpeg"
} else if (loginConfirmed) {
  loginQR.state = "logged_in"
}
} catch (loginQRError) {
  loginQRFailure = loginQRError
}
// The release runs whatever happened above. A release that itself throws keeps
// the original failure as the reported cause instead of masking it.
let qrHandoff = null
try {
  qrHandoff = await handOffTaskSpace(%d)
} catch (handoffError) {
  if (!loginQRFailure) loginQRFailure = handoffError
}
if (loginQRFailure) throw loginQRFailure
if (!qrHandoff?.done) throw new Error(%s)
cliLog(%q + JSON.stringify(loginQR))
`, selection, navigation, platformCodeJSON, string(loginRuleJSON), string(strictLoginEvidenceJSON), urlJSON, urlJSON, space.SpaceID, jsErrorLiteral(ErrHandoffFailed), resultPrefix)
	var runtimeResult struct {
		State       string `json:"state"`
		ImageBase64 string `json:"imageBase64"`
		ImageMime   string `json:"imageMime"`
		PageOrigin  string `json:"pageOrigin"`
	}
	if err := service.runtime.Run(ctx, program, &runtimeResult); err != nil {
		if err.Error() == ErrSpaceUserControlled {
			_, _ = service.db.UpdateEgoSpaceStateForIdentity(
				account.IdentityID, "agentDelegatedToUser", "manual_takeover", "USER_CONTROLLED",
			)
		}
		return nil, err
	}
	// A nil error proves the program reached its own release, so the deferred
	// best-effort release is not repeated here.
	spaceReleased = true
	if !matchesPlatformOrigin(account.PlatformCode, runtimeResult.PageOrigin) {
		return nil, fmt.Errorf("EGO_LOGIN_QR_PLATFORM_MISMATCH")
	}
	result := &LoginQR{
		AccountID: account.ID, PlatformCode: account.PlatformCode,
		State: runtimeResult.State, PageOrigin: safePageOrigin(runtimeResult.PageOrigin),
	}
	switch runtimeResult.State {
	case "qr_ready":
		raw, err := base64.StdEncoding.DecodeString(runtimeResult.ImageBase64)
		mime := strings.ToLower(strings.TrimSpace(runtimeResult.ImageMime))
		if mime == "" {
			mime = "image/png"
		}
		validImage := mime == "image/png" && len(raw) >= 8 && string(raw[:8]) == "\x89PNG\r\n\x1a\n"
		if mime == "image/jpeg" || mime == "image/jpg" {
			mime = "image/jpeg"
			validImage = len(raw) >= 3 && string(raw[:3]) == "\xff\xd8\xff"
		}
		if err != nil || len(raw) < 8 || len(raw) > 512<<10 || !validImage {
			return nil, fmt.Errorf("EGO_LOGIN_QR_IMAGE_INVALID")
		}
		result.ImageDataURL = "data:" + mime + ";base64," + runtimeResult.ImageBase64
		result.ExpiresAt = time.Now().UTC().Add(loginQRRefreshInterval).Format(time.RFC3339)
		if _, err := service.db.UpdateEgoSpaceStateForIdentity(
			account.IdentityID, "agentDelegatedToUser", "manual_takeover", "",
		); err != nil {
			return nil, err
		}
	case "logged_in":
		if _, err := service.db.UpdateEgoSpaceStateForIdentity(
			account.IdentityID, "agentDelegatedToUser", "manual_takeover", "",
		); err != nil {
			return nil, err
		}
		return result, nil
	case "manual_takeover", "":
		if _, err := service.db.UpdateEgoSpaceStateForIdentity(
			account.IdentityID, "agentDelegatedToUser", "manual_takeover", "QR_UNAVAILABLE",
		); err != nil {
			return nil, err
		}
		result.State = "manual_takeover"
		result.ReasonCode = "QR_UNAVAILABLE"
	default:
		return nil, fmt.Errorf("EGO_LOGIN_QR_RESULT_INVALID")
	}
	return result, nil
}

// releaseSpaceAfterFailure is the last line of defence against a Space that is
// stuck under agent control. The generated login programs already release inside
// their own failure path, but a crash, cancel or kill never reaches it.
//
// It is deliberately best effort: the caller's own error is the report, so this
// call cannot change the returned error and a detached, short-lived context
// keeps an already cancelled request from skipping the release.
func (service *SpaceService) releaseSpaceAfterFailure(ctx context.Context, spaceID int64) {
	if service == nil || service.runtime == nil || spaceID <= 0 {
		return
	}
	defer func() { _ = recover() }()
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const stuckSpace = spaces.find((item) => Number(item.id) === %d)
let released = Boolean(stuckSpace) && (stuckSpace.ownership === "user" || stuckSpace.ownership === "agentDelegatedToUser")
if (stuckSpace && stuckSpace.ownership === "agent") {
  await useOrCreateTaskSpace(%d)
  const result = await handOffTaskSpace(%d)
  released = Boolean(result?.done)
}
cliLog(%q + JSON.stringify({ released }))
`, spaceID, spaceID, spaceID, resultPrefix)
	var result struct {
		Released bool `json:"released"`
	}
	_ = service.runtime.Run(releaseCtx, program, &result)
}

// ReleaseLoginQR ends Electron-side QR polling and returns the Space to the
// user without creating, navigating, or closing any tab.
func (service *SpaceService) ReleaseLoginQR(ctx context.Context, accountID int64) (*LoginQR, error) {
	if service == nil || service.db == nil || service.runtime == nil || accountID <= 0 {
		return nil, fmt.Errorf("EGO_LOGIN_QR_INPUT_INVALID")
	}
	account, err := service.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	space, err := service.db.GetEgoSpaceByIdentity(account.IdentityID)
	if err != nil {
		return nil, err
	}
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const currentSpace = spaces.find((item) => Number(item.id) === %d)
let done = Boolean(currentSpace)
if (currentSpace && currentSpace.ownership !== "user" && currentSpace.ownership !== "agentDelegatedToUser") {
  await useOrCreateTaskSpace(%d)
  const result = await handOffTaskSpace(%d)
  done = Boolean(result?.done)
}
cliLog(%q + JSON.stringify({ done }))
`, space.SpaceID, space.SpaceID, space.SpaceID, resultPrefix)
	var result spaceResult
	if err := service.runtime.Run(ctx, program, &result); err != nil {
		return nil, err
	}
	if !result.Done {
		return nil, fmt.Errorf(ErrHandoffFailed)
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(
		account.IdentityID, "agentDelegatedToUser", "manual_takeover", "",
	); err != nil {
		return nil, err
	}
	return &LoginQR{AccountID: account.ID, PlatformCode: account.PlatformCode, State: "manual_takeover"}, nil
}

func (service *SpaceService) OpenManagerAndHandoff(ctx context.Context, accountID int64) (*db.EgoSpace, error) {
	if service == nil || service.db == nil || service.runtime == nil || accountID <= 0 {
		return nil, fmt.Errorf("EGO_SPACE_INPUT_INVALID")
	}
	account, err := service.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	platform, err := platforms.Find(account.PlatformCode)
	if err != nil {
		return nil, err
	}
	if platform.ManagerURL == "" {
		return nil, fmt.Errorf("EGO_PLATFORM_MANAGER_UNAVAILABLE")
	}
	space, err := service.existingBindingSpace(account.IdentityID)
	if err != nil {
		return nil, err
	}
	urlJSON, _ := json.Marshal(platform.ManagerURL)
	// One guarded program: it proves the Space still exists and is not under
	// manual control before opening anything, and hands control back at the end.
	// A vanished Space is never re-bound here, so no tab opens in a new context.
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const managerSpace = spaces.find((item) => Number(item.id) === %d)
if (!managerSpace) throw new Error(%s)
if (managerSpace.ownership === "user" || managerSpace.ownership === "agentDelegatedToUser") {
  throw new Error(%s)
}
await useOrCreateTaskSpace(%d)
await openOrReuseTab(%s, { wait: true, timeout: 30 })
const result = await handOffTaskSpace(%d)
cliLog(%q + JSON.stringify(result))
	`, space.SpaceID, jsErrorLiteral(ErrSpaceNotFound), jsErrorLiteral(ErrSpaceUserControlled), space.SpaceID, urlJSON, space.SpaceID, resultPrefix)
	var result spaceResult
	if err := service.runtime.Run(ctx, program, &result); err != nil {
		if strings.TrimSpace(err.Error()) == ErrSpaceUserControlled {
			if _, stateErr := service.db.UpdateEgoSpaceStateForIdentity(
				account.IdentityID, "agentDelegatedToUser", "manual_takeover", ErrSpaceUserControlled,
			); stateErr != nil {
				return nil, stateErr
			}
		}
		return nil, err
	}
	if !result.Done {
		return nil, fmt.Errorf(ErrHandoffFailed)
	}
	return service.db.UpdateEgoSpaceStateForIdentity(account.IdentityID, "agentDelegatedToUser", "manual_takeover", "")
}

func (service *SpaceService) ResumeAfterConfirmation(ctx context.Context, accountID int64) (*db.EgoSpace, error) {
	if service == nil || service.db == nil || service.runtime == nil {
		return nil, fmt.Errorf("EGO_SPACE_INPUT_INVALID")
	}
	account, err := service.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	space, err := service.db.GetEgoSpaceByIdentity(account.IdentityID)
	if err != nil {
		return nil, err
	}
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const boundSpace = spaces.find((item) => Number(item.id) === %d)
if (!boundSpace) throw new Error(%s)
if (boundSpace.ownership === "user" || boundSpace.ownership === "agentDelegatedToUser") {
  await takeOverTaskSpace(%d)
} else if (boundSpace.ownership !== "agent") {
  await claimTaskSpace(%d)
}
const claimedSpaces = await listTaskSpaces()
const claimedSpace = claimedSpaces.find((item) => Number(item.id) === %d)
const ownership = String(claimedSpace?.ownership ?? "")
cliLog(%q + JSON.stringify({
  id: Number(claimedSpace?.id ?? 0),
  taskId: String(claimedSpace?.taskId ?? ""),
  name: String(claimedSpace?.name ?? ""),
  ownership,
  done: Boolean(claimedSpace && ownership === "agent")
}))
`, space.SpaceID, jsErrorLiteral(ErrSpaceBindingMissing), space.SpaceID, space.SpaceID, space.SpaceID, resultPrefix)
	var result spaceResult
	if err := service.runtime.Run(ctx, program, &result); err != nil {
		return nil, err
	}
	if !result.Done || result.ID != space.SpaceID || result.Ownership != "agent" {
		return nil, fmt.Errorf("EGO_RESUME_FAILED")
	}
	return service.db.UpdateEgoSpaceStateForIdentity(account.IdentityID, "agent", "ready", "")
}

func (service *SpaceService) ObserveAccount(ctx context.Context, accountID int64) (*AccountObservation, error) {
	if service == nil || service.db == nil || service.runtime == nil || accountID <= 0 {
		return nil, fmt.Errorf("EGO_SPACE_INPUT_INVALID")
	}
	space, err := service.db.GetEgoSpaceByAccount(accountID)
	if err != nil {
		return nil, err
	}
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const boundSpace = spaces.find((item) => Number(item.id) === %d)
if (!boundSpace) throw new Error(%s)
if (boundSpace.ownership === "user" || boundSpace.ownership === "agentDelegatedToUser") {
  throw new Error(%s)
}
await useOrCreateTaskSpace(%d)
const snapshot = await snapshotText({ interactive: true })
cliLog(%q + JSON.stringify({
  available: snapshot != null,
  length: JSON.stringify(snapshot ?? "").length
}))
`, space.SpaceID, jsErrorLiteral(ErrSpaceBindingMissing), jsErrorLiteral(ErrSpaceUserControlled), space.SpaceID, resultPrefix)
	var result struct {
		Available bool `json:"available"`
		Length    int  `json:"length"`
	}
	if err := service.runtime.Run(ctx, program, &result); err != nil {
		return nil, err
	}
	return &AccountObservation{
		AccountID: accountID, SpaceID: space.SpaceID,
		SnapshotAvailable: result.Available, SnapshotLength: result.Length,
		Verified: false, EvidenceCode: "EGO_SNAPSHOT_UNVERIFIED",
	}, nil
}

// CheckAccountSession performs one conservative read-only login check in an
// existing Space. The enabled monitor is the user's recurring authorization
// to reclaim a delegated Space for this one call; control is always returned.
func (service *SpaceService) CheckAccountSession(ctx context.Context, account db.Account) (SessionCheck, error) {
	if service == nil || service.db == nil || service.runtime == nil || account.ID <= 0 || account.IdentityID <= 0 {
		return SessionCheck{}, fmt.Errorf("EGO_SESSION_CHECK_INPUT_INVALID")
	}
	space, err := service.db.GetEgoSpaceByIdentity(account.IdentityID)
	if err != nil {
		return SessionCheck{}, err
	}
	platform, err := platforms.Find(account.PlatformCode)
	if err != nil {
		return SessionCheck{}, err
	}
	targetURL := accountManagerURL(account, platform)
	if account.PlatformCode == "xhs" {
		// The generic creator root redirected this authenticated Space to the
		// login shell; use the reviewed read-only dashboard entry for checks.
		targetURL = "https://creator.xiaohongshu.com/new/home"
	} else if account.PlatformCode == "wechat_video" {
		targetURL = "https://channels.weixin.qq.com/platform"
	} else if account.PlatformCode == "wechat_mp" {
		targetURL = "https://mp.weixin.qq.com/cgi-bin/home"
	}
	if targetURL == "" {
		return SessionCheck{}, fmt.Errorf("EGO_PLATFORM_MANAGER_UNAVAILABLE")
	}
	urlJSON, _ := json.Marshal(targetURL)
	platformCodeJSON, _ := json.Marshal(account.PlatformCode)
	accountIdentifierJSON, _ := json.Marshal(account.AccountIdentifier)
	loginRuleJSON, _ := json.Marshal(platformReviewedLoginRule(account.PlatformCode))
	strictLoginEvidenceJSON, _ := json.Marshal(platformRequiresReviewedLoginEvidence(account.PlatformCode))
	originSuffixesJSON, _ := json.Marshal(platformOriginSuffixes(account.PlatformCode))
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const currentSpace = spaces.find((item) => Number(item.id) === %d)
let sessionCheck = {
  authState: "unknown",
  reasonCode: "PLATFORM_UNREACHABLE",
  evidenceCode: "EVIDENCE_MISSING",
  pageOrigin: ""
}
const platformCode = %s
const storedAccountIdentifier = %s
const loginRule = %s
const strictLoginEvidence = %s
const targetURL = %s
const targetSuffixes = %s
const matchesReviewedDashboard = (rule, pageURL, bodyText) => {
  if (!rule?.host) return false
  try {
    const parsed = new URL(pageURL)
    const host = parsed.hostname.toLowerCase().replace(/\.$/, "")
    return host === rule.host &&
      (!rule.pathPrefixes?.length || rule.pathPrefixes.some((prefix) => parsed.pathname.startsWith(prefix))) &&
      (rule.requiredText || []).every((token) => bodyText.includes(token))
  } catch { return false }
}
const reclaimedFromUser = Boolean(currentSpace &&
  (currentSpace.ownership === "agentDelegatedToUser" || currentSpace.ownership === "user"))
if (currentSpace && (currentSpace.ownership === "agent" || !currentSpace.ownership || reclaimedFromUser)) {
  if (reclaimedFromUser) await takeOverTaskSpace(%d)
  else await useOrCreateTaskSpace(%d)
  const beforeTabs = await listTabs()
  const beforeIds = new Set(beforeTabs.map((item) => String(item.targetId ?? item.id ?? "")))
  let temporaryTargetId = ""
  try {
    const targetHost = new URL(targetURL).hostname.toLowerCase().replace(/\.$/, "")
    const matchingTabs = (await listTabs()).filter((tab) => {
      try {
        const host = new URL(String(tab.url || "")).hostname.toLowerCase().replace(/\.$/, "")
        return host === targetHost || targetSuffixes.some((suffix) => host === suffix || host.endsWith("." + suffix))
      } catch { return false }
    })
    const matchingTab = [...matchingTabs].reverse().find((tab) => {
      try { return new URL(String(tab.url || "")).hostname.toLowerCase().replace(/\.$/, "") === targetHost } catch { return false }
    }) || matchingTabs[matchingTabs.length - 1]
    if (matchingTab) {
      await switchTab(matchingTab.targetId || matchingTab.id)
    } else {
      const opened = await openOrReuseTab(targetURL, { wait: true, timeout: 30 })
      const openedTargetId = String(opened?.targetId ?? opened?.id ?? "")
      if (openedTargetId && !beforeIds.has(openedTargetId)) temporaryTargetId = openedTargetId
    }
    try { await waitForNetworkIdle({ timeout: 8 }) } catch {}
    await new Promise((resolve) => setTimeout(resolve, 1200))
    const page = await pageInfo()
    const snapshot = JSON.stringify(await snapshotText())
    const dom = await js(String.raw`+"`"+`(() => {
      const text = (selector) => (document.querySelector(selector)?.textContent || "").trim()
      const attr = (selector, name) => document.querySelector(selector)?.getAttribute(name) || ""
      const isVisible = (element) => {
        if (!element || element.hidden || element.getAttribute('aria-hidden') === 'true') return false
        const style = getComputedStyle(element)
        return style.display !== 'none' && style.visibility !== 'hidden' && Number(style.opacity || 1) > 0 &&
          element.getClientRects().length > 0
      }
      const loginControls = [...document.querySelectorAll('button, a, input[type="submit"], [role="button"]')]
      const qrNodes = [...document.querySelectorAll('[class*="qr" i], [id*="qr" i], img[alt*="qr" i], img[alt*="二维码"]')]
      return {
        name: text('[data-testid*="profile"]') || text('[class*="profile-name"]') ||
          text('[class*="user-name"]') || text('[class*="username"]') ||
          attr('meta[property="profile:username"]', 'content'),
		douyinName: text('[class*="account-trigger"] [class*="name"]'),
        identifier: attr('[data-user-id]', 'data-user-id') ||
          attr('[data-account-id]', 'data-account-id') || attr('[data-uid]', 'data-uid'),
		csdnProfileHref: attr('[class*="user"] a[href*="blog.csdn.net/"], [class*="profile"] a[href*="blog.csdn.net/"]', 'href'),
		wechatMPName: text('.acount_box-nickname') || text('.weui-desktop_name'),
        avatar: attr('img.weui-desktop-account__img, img[class*="avatar"], [class*="avatar"] img, img[alt*="avatar" i], img[alt*="头像"]', 'src') ||
          [...document.querySelectorAll('img')].map((image) => image.currentSrc || image.src || "")
			.find((src) => /aweme-avatar|i-avatar\.csdnimg\.cn|sns-avatar|xhscdn[^/]*(?:\/|%%2f).*avatar|\/uhead\/|yt3\.ggpht|tiktokcdn|scontent[^/]*\.fbcdn|wx\.qlogo\.cn/i.test(src)) || "",
        bodyText: (document.body?.innerText || "").slice(0, 12000),
        hasVisiblePassword: [...document.querySelectorAll('input[type="password"]')].some(isVisible),
        hasVisibleLoginControl: loginControls.some((element) => isVisible(element) &&
          /登录|sign in|log in|继续|下一步/i.test((element.innerText || element.value || element.getAttribute('aria-label') || '').trim())),
        hasVisibleQR: qrNodes.some(isVisible)
      }
    })()`+"`"+`)
    const pageURL = typeof page?.url === "string" ? page.url : targetURL
    const reviewedDashboard = matchesReviewedDashboard(loginRule, pageURL, dom.bodyText)
    if (platformCode === "douyin") {
	  dom.name = dom.douyinName ||
	    dom.bodyText.match(/创作服务\s*\n([^\n]{1,80})\s*\n抖音号/)?.[1]?.trim() || dom.name
      dom.identifier = dom.identifier || dom.bodyText.match(/抖音号[：:]\s*([^\s]+)/)?.[1] || ""
	    } else if (platformCode === "csdn") {
	      // The CSDN creator sidebar's menu titles are not account identities.
	      // Only use the title from a validated public profile URL.
	      dom.name = ""
	      try {
        const storedProfilePath = /^[A-Za-z0-9_-]{2,80}$/.test(storedAccountIdentifier) ? storedAccountIdentifier : ""
        const profileURL = new URL(dom.csdnProfileHref || (storedProfilePath ? "https://blog.csdn.net/" + encodeURIComponent(storedProfilePath) : ""), pageURL)
        const profilePath = profileURL.pathname.split("/").filter(Boolean)[0] || ""
        if (profileURL.protocol !== "https:" || profileURL.hostname !== "blog.csdn.net" || !/^[A-Za-z0-9_-]{2,80}$/.test(profilePath)) {
          throw new Error("invalid_csdn_profile_url")
        }
        dom.identifier = profilePath
        try {
          const profileHTML = String(await serverFetch(profileURL.href))
          const publicTitle = (profileHTML.match(/<title[^>]*>([^<]{1,240})<\/title>/i)?.[1] || "")
            .replace(/&amp;/g, "&").replace(/&quot;/g, '"').replace(/&#39;|&apos;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">")
            .replace(/-CSDN博客$/, "").trim()
	          if (publicTitle) dom.name = publicTitle
	        } catch {}
	      } catch {}
	      dom.name = dom.name || ""
    } else if (platformCode === "wechat_video") {
	  const videoNameCandidate = dom.bodyText.match(/通知中心\s*\n([^\n]{1,80})/)?.[1]?.trim() || ""
	  const videoName = videoNameCandidate && !/^\d+$/.test(videoNameCandidate) ? videoNameCandidate : ""
      dom.identifier = dom.identifier || dom.bodyText.match(/视频号ID:\s*\n?([^\s]+)/)?.[1] || storedAccountIdentifier || ""
	  dom.name = videoName || dom.identifier || ""
	} else if (platformCode === "wechat_mp") {
	  dom.name = dom.wechatMPName || dom.name
    } else if (platformCode === "kuaishou") {
      dom.name = dom.name || dom.bodyText.match(/^\s*\d+\s*\n([^\n]{1,80})\s*\n发布作品/m)?.[1]?.trim() || ""
    } else if (platformCode === "xhs") {
      dom.name = dom.name || dom.bodyText.match(/创作服务平台\s*\n([^\n]{1,80})\s*\n发布笔记/)?.[1]?.trim() || ""
      dom.identifier = dom.identifier || dom.bodyText.match(/小红书账号[：:]\s*([^\s]+)/)?.[1] || ""
    } else if (platformCode === "tiktok") {
      dom.identifier = dom.identifier || dom.bodyText.match(/返回 TikTok\s*\n([^\n]{1,80})/)?.[1]?.trim() || storedAccountIdentifier || ""
	  dom.name = dom.identifier || ""
    } else if (platformCode === "youtube") {
      dom.name = dom.name || dom.bodyText.match(/Your channel\s*\n([^\n]{1,120})/)?.[1]?.trim() || ""
      dom.identifier = dom.identifier || pageURL.match(/\/channel\/([^/?#]+)/)?.[1] || ""
    } else if (platformCode === "meta") {
      dom.name = dom.name || dom.bodyText.match(/添加封面照片\s*\n([^\n]{1,120})\s*\n\d+\s*\n位粉丝/)?.[1]?.trim() || ""
      try { dom.identifier = dom.identifier || new URL(pageURL).searchParams.get("asset_id") || "" } catch {}
    }
    const challenge = /captcha|验证码|安全验证|扫码验证|security verification/i.test(snapshot)
    const loginURLHint = /\/login|signin|passport|\/auth(?:\/|$)/i.test(pageURL)
    const loginTextHint = /登录|sign in|log in|手机号|扫码|email|phone number/i.test(snapshot)
    const visibleCredentialSurface = dom.hasVisiblePassword || dom.hasVisibleQR || (loginURLHint && dom.hasVisibleLoginControl)
    const explicitLogin = visibleCredentialSurface && loginTextHint
    // Same contract as the verify program: a visible credential surface is
    // definitive even when the login page text mentions 验证码/扫码验证.
    const authState = explicitLogin ? "logged_out" :
      challenge ? "manual_takeover" :
      reviewedDashboard ? "logged_in" :
      (!strictLoginEvidence && (dom.name || dom.identifier)) ? "logged_in" : "unknown"
    sessionCheck = {
      authState,
      name: dom.name,
      identifier: dom.identifier,
      avatar: dom.avatar,
      reasonCode: challenge ? "CHECKPOINT_REQUIRED" :
        authState === "logged_in" ? "VERIFIED_PROFILE_DOM" :
        explicitLogin ? "LOGIN_REQUIRED_PAGE" : "PROFILE_SELECTOR_MISSING",
      evidenceCode: challenge ? "CHECKPOINT_DOM" :
        authState === "logged_in" ? "REVIEWED_PROFILE_DOM" :
        explicitLogin ? "EXPLICIT_LOGIN_DOM" : "EVIDENCE_MISSING",
      pageOrigin: pageURL
    }
  } finally {
    if (temporaryTargetId) await closeTab(temporaryTargetId)
    if (reclaimedFromUser) {
      const handoff = await handOffTaskSpace(%d)
      if (!handoff?.done) throw new Error(%s)
    }
  }
} else if (currentSpace) {
  sessionCheck.reasonCode = "BROWSER_OPERATION_BUSY"
  sessionCheck.evidenceCode = "OPERATION_LEASE_BUSY"
}
cliLog(%q + JSON.stringify(sessionCheck))
`, space.SpaceID, platformCodeJSON, accountIdentifierJSON, string(loginRuleJSON), string(strictLoginEvidenceJSON), urlJSON, string(originSuffixesJSON), space.SpaceID, space.SpaceID, space.SpaceID, jsErrorLiteral(ErrHandoffFailed), resultPrefix)
	var runtimeResult struct {
		AuthState    string `json:"authState"`
		Name         string `json:"name"`
		Identifier   string `json:"identifier"`
		Avatar       string `json:"avatar"`
		ReasonCode   string `json:"reasonCode"`
		EvidenceCode string `json:"evidenceCode"`
		PageOrigin   string `json:"pageOrigin"`
	}
	if err := service.runtime.Run(ctx, program, &runtimeResult); err != nil {
		return SessionCheck{}, err
	}
	return SessionCheck{
		AuthState: runtimeResult.AuthState, Name: runtimeResult.Name,
		AccountIdentifier: runtimeResult.Identifier, ReasonCode: runtimeResult.ReasonCode,
		EvidenceCode: runtimeResult.EvidenceCode, PageOrigin: runtimeResult.PageOrigin,
		AvatarURL: runtimeResult.Avatar,
	}, nil
}

func (service *SpaceService) OpenLoginBatch(ctx context.Context, identityID int64, platformCodes []string) (*LoginBatch, error) {
	if len(platformCodes) == 0 {
		return nil, fmt.Errorf("EGO_LOGIN_BATCH_EMPTY")
	}
	accounts, err := service.db.ListAccountsByIdentity(identityID)
	if err != nil {
		return nil, err
	}
	byPlatform := make(map[string]db.Account, len(accounts))
	for _, account := range accounts {
		if account.Enabled {
			byPlatform[account.PlatformCode] = account
		}
	}
	selectedCodes := make([]string, 0, 3)
	accountIDs := make([]int64, 0, 3)
	urls := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	for _, rawCode := range platformCodes {
		code := strings.TrimSpace(rawCode)
		if code == "" {
			continue
		}
		if _, duplicate := seen[code]; duplicate {
			continue
		}
		account, ok := byPlatform[code]
		if !ok {
			return nil, fmt.Errorf("EGO_PLATFORM_NOT_CONFIGURED")
		}
		platform, err := platforms.Find(code)
		if err != nil {
			return nil, err
		}
		targetURL := accountManagerURL(account, platform)
		if targetURL == "" {
			return nil, fmt.Errorf("EGO_PLATFORM_MANAGER_UNAVAILABLE")
		}
		seen[code] = struct{}{}
		selectedCodes = append(selectedCodes, code)
		accountIDs = append(accountIDs, account.ID)
		urls = append(urls, targetURL)
		if len(selectedCodes) == 3 {
			break
		}
	}
	if len(selectedCodes) == 0 {
		return nil, fmt.Errorf("EGO_LOGIN_BATCH_EMPTY")
	}
	space, err := service.EnsureIdentitySpace(ctx, identityID)
	if err != nil {
		return nil, err
	}
	spaceReleased := false
	defer func() {
		if spaceReleased {
			return
		}
		service.releaseSpaceAfterFailure(ctx, space.SpaceID)
	}()
	var program strings.Builder
	fmt.Fprintf(&program, `const spaces = await listTaskSpaces()
let loginBatchFailure = null
try {
const existingSpace = spaces.find((item) => Number(item.id) === %d)
if (!existingSpace) throw new Error(%s)
if (existingSpace.ownership === "agentDelegatedToUser" || existingSpace.ownership === "user") {
	await takeOverTaskSpace(%d)
} else {
  await useOrCreateTaskSpace(%d)
}
`, space.SpaceID, jsErrorLiteral(ErrSpaceBindingMissing), space.SpaceID, space.SpaceID)
	appendSpaceSafeTabReplacement(&program, urls)
	appendLoginBatchReleaseTail(&program, space.SpaceID)
	var result spaceResult
	if err := service.runtime.Run(ctx, program.String(), &result); err != nil {
		return nil, err
	}
	spaceReleased = true
	if !result.Done {
		return nil, fmt.Errorf(ErrHandoffFailed)
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(identityID, "agentDelegatedToUser", "manual_takeover", ""); err != nil {
		return nil, err
	}
	return &LoginBatch{
		IdentityID: identityID, SpaceID: space.SpaceID, AccountIDs: accountIDs,
		PlatformCodes: selectedCodes, State: "manual_takeover",
	}, nil
}

// appendLoginBatchReleaseTail closes a login program with one unconditional
// release: the body's failure is remembered, the Space is handed back whatever
// happened, and only then is the runtime payload emitted. Without it any throw
// between the claim and the last line left the Space agent-occupied forever.
func appendLoginBatchReleaseTail(program *strings.Builder, spaceID int64) {
	fmt.Fprintf(program, `} catch (error) {
  loginBatchFailure = error
}
// A release that throws must not mask the body failure that caused it.
let loginBatchHandoff = null
try {
  loginBatchHandoff = await handOffTaskSpace(%d)
} catch (handoffError) {
  if (!loginBatchFailure) loginBatchFailure = handoffError
}
if (loginBatchFailure) throw loginBatchFailure
if (!loginBatchHandoff?.done) throw new Error(%s)
cliLog(%q + JSON.stringify(loginBatchHandoff))
`, spaceID, jsErrorLiteral(ErrHandoffFailed), resultPrefix)
}

// OpenLoggedInPlatforms reuses an identity's existing Space whenever one is
// already bound. The local UI action is the user's explicit authorization to
// claim a handed-off Space for this one operation; control is handed back as
// soon as the requested creator pages are open.
func (service *SpaceService) OpenLoggedInPlatforms(ctx context.Context, identityID int64, offset int) (*BrowserOpenResult, error) {
	return service.OpenLoggedInPlatformsForCodes(ctx, identityID, offset, nil)
}

// OpenLoggedInPlatformsForCodes is the mode-aware compatibility entry point.
// A non-empty request is validated against the identity's enabled accounts and
// then intersected with the existing ordered logged-in platform list.
func (service *SpaceService) OpenLoggedInPlatformsForCodes(ctx context.Context, identityID int64, offset int, requestedCodes []string) (*BrowserOpenResult, error) {
	if service == nil || service.db == nil || service.runtime == nil || identityID <= 0 || offset < 0 {
		return nil, fmt.Errorf("EGO_BROWSER_OPEN_INPUT_INVALID")
	}
	accounts, err := service.db.ListAccountsByIdentity(identityID)
	if err != nil {
		return nil, err
	}
	byPlatform := make(map[string]db.Account, len(accounts))
	configuredByPlatform := make(map[string]db.Account, len(accounts))
	for _, account := range accounts {
		if account.Enabled {
			configuredByPlatform[account.PlatformCode] = account
		}
		if account.Enabled && account.LoginState == db.AccountAuthLoggedIn {
			byPlatform[account.PlatformCode] = account
		}
	}
	requested := make(map[string]struct{}, len(requestedCodes))
	for _, rawCode := range requestedCodes {
		code := strings.TrimSpace(rawCode)
		if code == "" {
			return nil, fmt.Errorf("EGO_PLATFORM_NOT_CONFIGURED")
		}
		if _, duplicate := requested[code]; duplicate {
			continue
		}
		if _, findErr := platforms.Find(code); findErr != nil {
			return nil, fmt.Errorf("EGO_PLATFORM_NOT_CONFIGURED")
		}
		if _, configured := configuredByPlatform[code]; !configured {
			return nil, fmt.Errorf("EGO_PLATFORM_NOT_CONFIGURED")
		}
		requested[code] = struct{}{}
	}
	if len(requestedCodes) > 0 && len(requested) == 0 {
		return nil, fmt.Errorf("EGO_PLATFORM_NOT_CONFIGURED")
	}
	orderedCodes := []string{
		"douyin", "wechat_video", "kuaishou", "xhs", "tiktok",
		"youtube", "meta", "wechat_mp", "csdn",
		"acfun", "baijiahao", "bilibili", "jianshu", "juejin", "omtencent",
		"pdd", "sina", "toutiao", "weishi", "x", "zhihu",
		"douyin_shop", "taobao", "tiktok_shop", "shopee", "shopify", "amazon_seller",
		"qianchuan", "jd_shop", "pinduoduo_shop", "xhs_shop", "kuaishou_shop", "wechat_shop",
		"alibaba_1688", "aliexpress", "xianyu",
	}
	loggedInCodes := make([]string, 0, len(byPlatform))
	for _, code := range orderedCodes {
		if len(requested) > 0 {
			if _, ok := requested[code]; !ok {
				continue
			}
		}
		if _, ok := byPlatform[code]; ok {
			loggedInCodes = append(loggedInCodes, code)
		}
	}
	if len(loggedInCodes) == 0 {
		for _, code := range orderedCodes {
			if len(requested) > 0 {
				if _, ok := requested[code]; !ok {
					continue
				}
			}
			if _, ok := configuredByPlatform[code]; ok {
				loggedInCodes = append(loggedInCodes, code)
			}
		}
	}
	if len(loggedInCodes) == 0 {
		return nil, fmt.Errorf("EGO_PLATFORM_NOT_CONFIGURED")
	}
	if offset >= len(loggedInCodes) {
		offset = 0
	}
	end := offset + 3
	if end > len(loggedInCodes) {
		end = len(loggedInCodes)
	}
	selectedCodes := loggedInCodes[offset:end]
	urls := make([]string, 0, len(selectedCodes))
	for _, code := range selectedCodes {
		platform, findErr := platforms.Find(code)
		if findErr != nil {
			return nil, findErr
		}
		account, ok := byPlatform[code]
		if !ok {
			account = configuredByPlatform[code]
		}
		managerURL := accountManagerURL(account, platform)
		if managerURL == "" {
			return nil, fmt.Errorf("EGO_PLATFORM_MANAGER_UNAVAILABLE")
		}
		urls = append(urls, managerURL)
	}

	_, existingErr := service.db.GetEgoSpaceByIdentity(identityID)
	reused := existingErr == nil
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return nil, existingErr
	}
	space, err := service.EnsureIdentitySpace(ctx, identityID)
	if err != nil {
		return nil, err
	}
	spaceReleased := false
	defer func() {
		if spaceReleased {
			return
		}
		service.releaseSpaceAfterFailure(ctx, space.SpaceID)
	}()

	var program strings.Builder
	fmt.Fprintf(&program, `const spaces = await listTaskSpaces()
let loginBatchFailure = null
try {
const existingSpace = spaces.find((item) => Number(item.id) === %d)
if (!existingSpace) throw new Error(%s)
if (existingSpace.ownership === "agentDelegatedToUser" || existingSpace.ownership === "user") {
	await takeOverTaskSpace(%d)
} else {
  await useOrCreateTaskSpace(%d)
}
`, space.SpaceID, jsErrorLiteral(ErrSpaceBindingMissing), space.SpaceID, space.SpaceID)
	appendSpaceSafeTabReplacement(&program, urls)
	appendLoginBatchReleaseTail(&program, space.SpaceID)
	var runtimeResult spaceResult
	if err := service.runtime.Run(ctx, program.String(), &runtimeResult); err != nil {
		return nil, err
	}
	spaceReleased = true
	if !runtimeResult.Done {
		return nil, fmt.Errorf(ErrHandoffFailed)
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(identityID, "agentDelegatedToUser", "manual_takeover", ""); err != nil {
		return nil, err
	}
	remaining := len(loggedInCodes) - end
	nextOffset := end
	if remaining == 0 {
		nextOffset = 0
	}
	return &BrowserOpenResult{
		IdentityID: identityID, SpaceID: space.SpaceID,
		PlatformCodes: append([]string(nil), selectedCodes...),
		Total:         len(loggedInCodes), Remaining: remaining, NextOffset: nextOffset,
		State: "manual_takeover", Reused: reused,
	}, nil
}

func (service *SpaceService) OpenPublishBatchAndHandoff(ctx context.Context, identityID int64, pages []PublishPage) (*LoginBatch, error) {
	// A prepared composer must remain available for the later submit-bound
	// operation. Serializing to one target prevents repeated prepare calls from
	// accumulating composer tabs in one persistent matrix Space.
	if service == nil || service.db == nil || service.runtime == nil || identityID <= 0 || len(pages) != 1 {
		return nil, fmt.Errorf("EGO_PUBLISH_BATCH_INVALID")
	}
	accounts, err := service.db.ListAccountsByIdentity(identityID)
	if err != nil {
		return nil, err
	}
	accountIDs := make([]int64, 0, len(pages))
	platformCodes := make([]string, 0, len(pages))
	runtimeTargets := make([]publishRuntimeTarget, 0, len(pages))
	unsupportedPrepare := make(map[string]bool)
	seen := make(map[string]struct{}, len(pages))
	for _, page := range pages {
		if err := validateScheduledPublishAt(page.PlatformCode, page.ContentType, page.PublishAt); err != nil {
			return nil, err
		}
		if page.PlatformCode == "wechat_video" && page.ContentType != "video" {
			return nil, fmt.Errorf("EGO_WECHAT_VIDEO_CONTENT_TYPE_UNSUPPORTED")
		}
		if _, duplicate := seen[page.PlatformCode]; duplicate {
			return nil, fmt.Errorf("EGO_PUBLISH_BATCH_INVALID")
		}
		if !platforms.CanPublish(page.PlatformCode) {
			return nil, fmt.Errorf("EGO_PUBLISH_PLATFORM_DISABLED")
		}
		if page.PlatformCode == "wechat_mp" && page.ContentType != "article" {
			return nil, fmt.Errorf("EGO_WECHAT_MP_CONTENT_TYPE_UNSUPPORTED")
		}
		account, ok := selectPublishAccount(accounts, page.PlatformCode)
		if !ok || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
			if page.PlatformCode == "facebook" || page.PlatformCode == "instagram" {
				return nil, fmt.Errorf("EGO_META_ACCOUNT_UNAVAILABLE")
			}
			return nil, fmt.Errorf("EGO_PUBLISH_ACCOUNT_UNAVAILABLE")
		}
		metaTarget := metaPublishTarget{}
		composerAssetExternalID := page.AssetExternalID
		if page.PlatformCode == "facebook" || page.PlatformCode == "instagram" {
			metaTarget, err = service.validateMetaPublishTarget(account, page)
			if err != nil {
				return nil, err
			}
			if page.PlatformCode == "instagram" {
				// Business Suite's unified Instagram composer is entered through
				// the connected Facebook Page. The Instagram public ID remains a
				// separately verified target in the runtime payload.
				composerAssetExternalID = metaTarget.FacebookPage.ExternalID
			}
		}
		capability, capabilityOK := egopublish.CapabilityFor(page.PlatformCode, page.ContentType)
		composerURL, composerOK := egopublish.ComposerURLFor(page.PlatformCode, page.ContentType, composerAssetExternalID)
		if !capabilityOK || !composerOK {
			return nil, fmt.Errorf("EGO_PUBLISH_COMPOSER_UNAVAILABLE")
		}
		if capability.PrepareMode == egopublish.PrepareAutomated && !capability.PrepareAdapterSupported {
			return nil, fmt.Errorf("EGO_PUBLISH_PREPARE_UNAVAILABLE")
		}
		if capability.PrepareMode != egopublish.PrepareAutomated {
			unsupportedPrepare[page.PlatformCode] = true
		}
		seen[page.PlatformCode] = struct{}{}
		accountIDs = append(accountIDs, account.ID)
		platformCodes = append(platformCodes, page.PlatformCode)
		tags, tagsErr := parseFrozenTags(page.TagsJSON)
		if tagsErr != nil {
			return nil, tagsErr
		}
		if page.PlatformCode == "xhs" {
			if err := validateXHSSettings(page.SettingsJSON); err != nil {
				return nil, err
			}
			if len(tags) > 3 {
				return nil, fmt.Errorf("EGO_XHS_TAGS_INVALID")
			}
			if err := validateXHSPublishMedia(page.ContentType, page.Media); err != nil {
				return nil, err
			}
			if strings.TrimSpace(page.Title) == "" || (strings.TrimSpace(page.Body) == "" && strings.TrimSpace(page.Caption) == "") {
				return nil, fmt.Errorf("EGO_PUBLISH_CONTENT_INVALID")
			}
		}
		if page.PlatformCode == "bilibili" && page.ContentType == "video" {
			if err := validateBilibiliPublishMedia(page.Media); err != nil {
				return nil, err
			}
			if len(tags) != 0 {
				return nil, fmt.Errorf("EGO_BILIBILI_TAGS_UNSUPPORTED")
			}
		}
		wechatMPSettings := platforms.WechatMPSettings{PublishType: "publish", ContentSource: "none"}
		if page.PlatformCode == "wechat_mp" {
			wechatMPSettings, err = normalizeWechatMPSettings(page.SettingsJSON)
			if err != nil {
				return nil, err
			}
			if err := validateWechatMPPublishMedia(page.Media); err != nil {
				return nil, err
			}
			if len(tags) != 0 {
				return nil, fmt.Errorf("EGO_WECHAT_MP_TAGS_UNSUPPORTED")
			}
		}
		if page.PlatformCode == "douyin" {
			if err := validateDouyinPublishMedia(page.ContentType, page.Media); err != nil {
				return nil, err
			}
		}
		if err := validateFrozenPublishMedia(page.Media); err != nil {
			return nil, err
		}
		if page.PlatformCode == "facebook" || page.PlatformCode == "instagram" {
			if err := validateMetaPublishMedia(page.ContentType, page.Media); err != nil {
				return nil, err
			}
		}
		if page.PlatformCode == "wechat_video" {
			if err := validateWechatVideoPublishMedia(page.Media); err != nil {
				return nil, err
			}
			if err := validateWechatVideoSettings(page.SettingsJSON); err != nil {
				return nil, err
			}
			if len(tags) > 3 {
				return nil, fmt.Errorf("EGO_WECHAT_VIDEO_TAGS_INVALID")
			}
		}
		youtubeSettings := platforms.YouTubeSettings{}
		tikTokSettings := platforms.TikTokSettings{}
		if page.PlatformCode == "youtube" {
			youtubeSettings, err = normalizeYouTubeSettings(page.ContentType, page.SettingsJSON)
			if err != nil {
				return nil, err
			}
			if err := validateYouTubePublishMedia(page.Media); err != nil {
				return nil, err
			}
		} else if page.PlatformCode == "tiktok" && capability.PrepareMode == egopublish.PrepareAutomated {
			tikTokSettings, err = normalizeTikTokSettings(page.ContentType, page.SettingsJSON)
			if err != nil {
				return nil, err
			}
			if err := validateTikTokPublishMedia(page.Media); err != nil {
				return nil, err
			}
		}
		if capability.PrepareMode == egopublish.PrepareAutomated {
			if (page.PlatformCode != "tiktok" && page.PlatformCode != "facebook" && page.PlatformCode != "instagram" && strings.TrimSpace(page.Title) == "") ||
				(page.PlatformCode == "bilibili" && (page.ContentType != "video" || (strings.TrimSpace(page.Body) == "" && strings.TrimSpace(page.Caption) == ""))) ||
				(page.PlatformCode == "csdn" && strings.TrimSpace(page.Body) == "") ||
				((page.PlatformCode == "kuaishou" || page.PlatformCode == "xhs" || page.PlatformCode == "tiktok" || page.PlatformCode == "wechat_video" || page.PlatformCode == "facebook" || page.PlatformCode == "instagram") && strings.TrimSpace(page.Body) == "" && strings.TrimSpace(page.Caption) == "") ||
				(page.PlatformCode == "wechat_mp" && strings.TrimSpace(page.Body) == "") {
				return nil, fmt.Errorf("EGO_PUBLISH_CONTENT_INVALID")
			}
			if page.PlatformCode == "kuaishou" {
				switch len(page.Media) {
				case 0:
					return nil, fmt.Errorf("EGO_PUBLISH_MEDIA_REQUIRED")
				case 1:
				default:
					return nil, fmt.Errorf("EGO_PREPARE_MULTI_MEDIA_UNVERIFIED")
				}
			}
		}
		runtimeTargets = append(runtimeTargets, publishRuntimeTarget{
			PlatformCode: page.PlatformCode, ContentType: page.ContentType, PrepareMode: capability.PrepareMode, ComposerURL: composerURL,
			MetaPageID: metaTarget.FacebookPage.ExternalID, MetaPageName: metaTarget.FacebookPage.Name,
			MetaInstagramID: metaTarget.InstagramAccount.ExternalID, MetaInstagramName: metaTarget.InstagramAccount.Name,
			PublishAt: page.PublishAt,
			Audience:  youtubeSettings.Audience, Visibility: youtubeSettings.Visibility,
			Playlist: youtubeSettings.Playlist, AllowComments: youtubeSettings.AllowComments,
			AlteredContent: youtubeSettings.AlteredContent,
			AllowComment:   tikTokSettings.AllowComment, AllowDuet: tikTokSettings.AllowDuet,
			AllowStitch: tikTokSettings.AllowStitch, AIGenerated: tikTokSettings.AIGenerated,
			AllowDownload: tikTokSettings.AllowDownload,
			Author:        wechatMPSettings.Author, CommentEnabled: wechatMPSettings.CommentEnabled,
			DeclareOriginal: wechatMPSettings.DeclareOriginal, AllowReprint: wechatMPSettings.AllowReprint,
			PublishType: wechatMPSettings.PublishType, Collection: wechatMPSettings.Collection,
			ContentSource: wechatMPSettings.ContentSource,
			Title:         page.Title, Caption: page.Caption, Body: page.Body, Tags: tags,
			Media: append([]PublishMedia(nil), page.Media...),
		})
	}
	space, err := service.EnsureIdentitySpace(ctx, identityID)
	if err != nil {
		return nil, err
	}
	program := buildEgoPublishPrepareProgram(space.SpaceID, runtimeTargets)
	var result publishRuntimeResult
	if err := service.runtime.Run(ctx, program, &result); err != nil {
		if strings.TrimSpace(err.Error()) == ErrSpaceUserControlled {
			platformErrors := make(map[string]string, len(platformCodes))
			for _, platformCode := range platformCodes {
				platformErrors[platformCode] = ErrSpaceUserControlled
			}
			if _, stateErr := service.db.UpdateEgoSpaceStateForIdentity(identityID, "user", "ready", ErrSpaceUserControlled); stateErr != nil {
				return nil, stateErr
			}
			return &LoginBatch{
				IdentityID: identityID, SpaceID: space.SpaceID, AccountIDs: accountIDs,
				PlatformCodes: platformCodes, PlatformErrors: platformErrors, State: "failed",
			}, nil
		}
		return nil, err
	}
	if !result.Done {
		return nil, fmt.Errorf(ErrPublishRuntimeIncomplete)
	}
	completion, completionErr := service.runPublishCompletion(ctx, space.SpaceID)
	delegatedToUser := (completionErr != nil && strings.TrimSpace(completionErr.Error()) == ErrSpaceUserControlled) ||
		(completionErr == nil && !completion.Done && strings.TrimSpace(completion.Skipped) == "user-owned")
	if (completionErr != nil && !delegatedToUser) || (!completion.Done && !delegatedToUser) {
		return nil, fmt.Errorf(ErrPublishSpaceReleaseFailed)
	}
	completionOwnership := "agent"
	if delegatedToUser {
		completionOwnership = "agentDelegatedToUser"
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(identityID, completionOwnership, "ready", ""); err != nil {
		return nil, err
	}
	prepared := make([]string, 0, len(result.Results))
	errorsByPlatform := make(map[string]string)
	diagnosticsByPlatform := make(map[string]PublishPrepareDiagnostic)
	for _, targetResult := range result.Results {
		if targetResult.Status == "awaiting_review" {
			prepared = append(prepared, targetResult.PlatformCode)
		}
		if targetResult.ErrorCode != "" {
			errorsByPlatform[targetResult.PlatformCode] = targetResult.ErrorCode
		}
		if diagnostic := sanitizePublishPrepareDiagnostic(targetResult.PlatformCode, targetResult.ErrorCode, targetResult.Diagnostic); diagnostic != nil {
			diagnosticsByPlatform[targetResult.PlatformCode] = *diagnostic
		}
	}
	preparedSet := make(map[string]bool, len(prepared))
	for _, platformCode := range prepared {
		preparedSet[platformCode] = true
	}
	for _, platformCode := range platformCodes {
		if preparedSet[platformCode] || errorsByPlatform[platformCode] != "" {
			continue
		}
		if unsupportedPrepare[platformCode] {
			errorsByPlatform[platformCode] = "EGO_PUBLISH_AUTOMATION_UNSUPPORTED"
		} else {
			errorsByPlatform[platformCode] = "EGO_PREPARE_RESULT_INVALID"
		}
	}
	state := "failed"
	if len(prepared) == len(platformCodes) {
		state = "awaiting_review"
	} else if len(prepared) > 0 {
		state = "partial_failed"
	}
	return &LoginBatch{
		IdentityID: identityID, SpaceID: space.SpaceID, AccountIDs: accountIDs,
		PlatformCodes: platformCodes, PreparedPlatformCodes: prepared,
		PlatformErrors: errorsByPlatform, PlatformDiagnostics: diagnosticsByPlatform, State: state,
	}, nil
}

// Publish error codes shared by the publish runtime and its callers. They are
// declared once so a code change is compiler-checked instead of drifting
// across raw string literals.
const (
	ErrPublishSpaceReleaseFailed = "EGO_PUBLISH_SPACE_RELEASE_FAILED"
	ErrPublishRuntimeIncomplete  = "EGO_PUBLISH_RUNTIME_INCOMPLETE"
	ErrSpaceUserControlled       = "EGO_SPACE_USER_CONTROLLED"
	ErrSpaceBindingMissing       = "EGO_SPACE_BINDING_MISSING"
	// ErrSpaceNotFound means the bound Space no longer exists in the EgoLite
	// runtime; the operator has to re-bind, so it is reported separately from a
	// local binding row that was never written.
	ErrSpaceNotFound = "EGO_SPACE_NOT_FOUND"
	// ErrSpaceRebindFailed means the Space was gone and rebuilding it failed;
	// the binding row is left untouched so the operator decides what to do.
	ErrSpaceRebindFailed = "EGO_SPACE_REBIND_FAILED"
	ErrHandoffFailed     = "EGO_HANDOFF_FAILED"
)

// mapSpaceCreateError keeps contract errors a caller can act on (missing
// profile, profile already bound, Space under manual control) and reports any
// other rebuild failure as ErrSpaceRebindFailed. The raw runtime error is
// never swallowed into "missing binding", which would misdiagnose the cause.
func mapSpaceCreateError(err error) error {
	if err == nil {
		return nil
	}
	code := strings.TrimSpace(err.Error())
	// The binding-missing code is raised while rebuilding here, so it means the
	// rebuild did not produce a usable Space, not that a binding never existed.
	if code == ErrSpaceBindingMissing || code == ErrSpaceNotFound {
		return fmt.Errorf("%s", ErrSpaceRebindFailed)
	}
	for _, known := range []string{
		ErrSpaceUserControlled,
		"EGO_PROFILE_NOT_FOUND", "EGO_PROFILE_BINDING_UNAVAILABLE", "EGO_INPUT_INVALID",
		"EGO_PROFILE_TAKEN", "EGO_RUNTIME_TIMEOUT", "EGO_RUNTIME_UNAVAILABLE", "EGO_RUNTIME_CANCELLED",
	} {
		if code == known {
			return fmt.Errorf("%s", known)
		}
	}
	return fmt.Errorf("%s", ErrSpaceRebindFailed)
}

// jsErrorLiteral renders a JS `throw new Error(<code>)` argument for one of the
// error codes above, so generated runtime programs and the Go callers that
// inspect them cannot drift apart on spelling.
func jsErrorLiteral(code string) string {
	encoded, _ := json.Marshal(code)
	return string(encoded)
}

// spaceRebindTimeout bounds the automatic re-bind so a stuck runtime cannot
// hang a collection for longer than the account's own timeout budget.
const spaceRebindTimeout = 60 * time.Second

// rebindMissingRuntimeSpace checks that the persisted binding still exists in
// the EgoLite runtime before a caller sends a program that addresses it by id.
// When the Space is gone (e.g. the browser was reset or the Space was deleted
// outside the app) the identity is re-bound through EnsureIdentitySpace
// instead of failing with EGO_SPACE_BINDING_MISSING on every later call.
// Network or runtime failures fail closed: the caller gets the original error
// and nothing is re-bound.
func (service *SpaceService) rebindMissingRuntimeSpace(ctx context.Context, identityID int64, space *db.EgoSpace) (*db.EgoSpace, error) {
	if space == nil || space.SpaceID <= 0 {
		return nil, fmt.Errorf(ErrSpaceBindingMissing)
	}
	live, known, err := service.probeRuntimeSpace(ctx, space.SpaceID)
	if err != nil {
		return nil, err
	}
	if live {
		return space, nil
	}
	if !known {
		// The runtime answered without a Space listing, so "not live" is not
		// evidence of disappearance. Keep the binding and let the caller's own
		// guarded program report what the runtime actually knows; rebuilding on
		// a half-read answer would create a second Space for the same identity.
		return space, nil
	}
	// A Space the operator took over by hand is never rebuilt on their behalf:
	// that would silently end the takeover. Report it so they resume explicitly.
	if space.Ownership == "user" || space.Ownership == "agentDelegatedToUser" || space.State == "manual_takeover" {
		return nil, fmt.Errorf(ErrSpaceUserControlled)
	}
	rebuiltCtx, cancel := context.WithTimeout(ctx, spaceRebindTimeout)
	defer cancel()
	rebuilt, err := service.EnsureIdentitySpace(rebuiltCtx, identityID)
	if err != nil {
		return nil, mapSpaceCreateError(err)
	}
	if rebuilt == nil || rebuilt.SpaceID <= 0 || rebuilt.SpaceID == space.SpaceID {
		return nil, fmt.Errorf(ErrSpaceRebindFailed)
	}
	if live, err := service.spaceExistsInRuntime(rebuiltCtx, rebuilt.SpaceID); err != nil {
		return nil, err
	} else if !live {
		return nil, fmt.Errorf(ErrSpaceRebindFailed)
	}
	return rebuilt, nil
}

// existingBindingSpace reads the persisted binding without touching the
// runtime. It is the pre-flight for write actions: a missing row is reported
// before any runtime work, and a vanished Space is caught by the guarded
// program itself so no write is ever re-bound into a new context.
func (service *SpaceService) existingBindingSpace(identityID int64) (*db.EgoSpace, error) {
	if service == nil || service.db == nil || identityID <= 0 {
		return nil, fmt.Errorf("EGO_SPACE_INPUT_INVALID")
	}
	space, err := service.db.GetEgoSpaceByIdentity(identityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf(ErrSpaceBindingMissing)
		}
		return nil, err
	}
	if space == nil || space.SpaceID <= 0 {
		return nil, fmt.Errorf(ErrSpaceBindingMissing)
	}
	return space, nil
}

// EnsureRuntimeSpaceBinding is the caller-facing pre-flight for read-only
// entries (creator metrics, manager handoff, social metrics): it makes sure a
// binding row exists (re-binding a vanished Space before the caller starts its
// own work) and returns the Space the caller should address. It reads
// listTaskSpaces once and never wraps the caller's program. Write actions must
// use requireLiveRuntimeSpace instead.
func (service *SpaceService) EnsureRuntimeSpaceBinding(ctx context.Context, identityID int64) (*db.EgoSpace, error) {
	space, err := service.db.GetEgoSpaceByIdentity(identityID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		rebuiltCtx, cancel := context.WithTimeout(ctx, spaceRebindTimeout)
		defer cancel()
		rebuilt, rebuildErr := service.EnsureIdentitySpace(rebuiltCtx, identityID)
		if rebuildErr != nil {
			return nil, mapSpaceCreateError(rebuildErr)
		}
		if rebuilt == nil || rebuilt.SpaceID <= 0 {
			return nil, fmt.Errorf(ErrSpaceRebindFailed)
		}
		return rebuilt, nil
	}
	return service.rebindMissingRuntimeSpace(ctx, identityID, space)
}

// spaceExistsInRuntime re-reads listTaskSpaces and reports whether spaceID is
// among them. Any runtime or output failure is returned as-is so callers fail
// closed instead of guessing.
func (service *SpaceService) spaceExistsInRuntime(ctx context.Context, spaceID int64) (bool, error) {
	exists, known, err := service.probeRuntimeSpace(ctx, spaceID)
	if err != nil {
		return false, err
	}
	return exists && known, nil
}

// probeRuntimeSpace reports whether spaceID is listed and whether the runtime
// answered with an actual listing. A program that returns no `spaces` array is
// not a listing: the JSON field stays nil when the key is absent and becomes an
// empty slice when the runtime really said "no Spaces".
func (service *SpaceService) probeRuntimeSpace(ctx context.Context, spaceID int64) (exists bool, known bool, err error) {
	var listed struct {
		Spaces []struct {
			ID int64 `json:"id"`
		} `json:"spaces"`
	}
	// The runtime only returns a payload on a line prefixed with resultPrefix;
	// without it the probe reads as invalid output and every Space looks gone.
	probe := fmt.Sprintf("const spaces = await listTaskSpaces()\ncliLog(%q + JSON.stringify({ spaces, probe: \"alpha-space-probe\" }))", resultPrefix)
	if err := service.runtime.Run(ctx, probe, &listed); err != nil {
		return false, false, err
	}
	if listed.Spaces == nil {
		return false, false, nil
	}
	for _, item := range listed.Spaces {
		if item.ID == spaceID {
			return true, true, nil
		}
	}
	return false, true, nil
}

func (service *SpaceService) runPublishCompletion(ctx context.Context, spaceID int64) (spaceResult, error) {
	if service == nil || service.runtime == nil || spaceID < 0 {
		return spaceResult{}, fmt.Errorf(ErrPublishSpaceReleaseFailed)
	}
	var result spaceResult
	if err := service.runtime.Run(ctx, buildEgoPublishCompletionProgram(spaceID), &result); err != nil {
		return result, err
	}
	return result, nil
}

// restoreResumedSpaceClaim hands back a Space that ResumeAfterConfirmation
// reclaimed for a run that then never answered. Leaving the row at
// agent/ready would report a live claim that no agent holds any more, so the
// Space is released best effort and persisted as delegated with the failure.
func (service *SpaceService) restoreResumedSpaceClaim(ctx context.Context, identityID int64) {
	if service == nil || service.db == nil || identityID <= 0 {
		return
	}
	if space, err := service.db.GetEgoSpaceByIdentity(identityID); err == nil && space != nil {
		service.releaseSpaceAfterFailure(ctx, space.SpaceID)
	}
	_, _ = service.db.UpdateEgoSpaceStateForIdentity(
		identityID, "agentDelegatedToUser", "manual_takeover", ErrPublishSpaceReleaseFailed,
	)
}

func (service *SpaceService) validateSubmitPublishBatch(identityID int64, pages []PublishPage, acceptance bool) ([]publishRuntimeTarget, []int64, []string, error) {
	if service == nil || service.db == nil || identityID <= 0 || len(pages) == 0 || len(pages) > 3 {
		return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_BATCH_INVALID")
	}
	accounts, err := service.db.ListAccountsByIdentity(identityID)
	if err != nil {
		return nil, nil, nil, err
	}
	runtimeTargets := make([]publishRuntimeTarget, 0, len(pages))
	accountIDs := make([]int64, 0, len(pages))
	platformCodes := make([]string, 0, len(pages))
	seen := make(map[string]struct{}, len(pages))
	for _, page := range pages {
		if err := validateScheduledPublishAt(page.PlatformCode, page.ContentType, page.PublishAt); err != nil {
			return nil, nil, nil, err
		}
		if (page.PlatformCode == "kuaishou" || page.PlatformCode == "wechat_video" || page.PlatformCode == "facebook") && strings.TrimSpace(page.PublishAt) == "" {
			return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_SCHEDULE_REQUIRED")
		}
		if _, duplicate := seen[page.PlatformCode]; duplicate {
			return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_BATCH_INVALID")
		}
		capability, capabilityOK := egopublish.CapabilityFor(page.PlatformCode, page.ContentType)
		if !capabilityOK {
			return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_SUBMIT_UNVERIFIED")
		}
		account, ok := selectPublishAccount(accounts, page.PlatformCode)
		if !ok || !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
			return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_ACCOUNT_UNAVAILABLE")
		}
		tags, tagsErr := parseFrozenTags(page.TagsJSON)
		if tagsErr != nil {
			return nil, nil, nil, tagsErr
		}
		if page.PlatformCode == "douyin" {
			if err := validateDouyinPublishMedia(page.ContentType, page.Media); err != nil {
				return nil, nil, nil, err
			}
		}
		if err := validateFrozenPublishMedia(page.Media); err != nil {
			return nil, nil, nil, err
		}
		if (page.PlatformCode != "facebook" && strings.TrimSpace(page.Title) == "") ||
			(page.PlatformCode == "facebook" && strings.TrimSpace(page.Caption) == "" && strings.TrimSpace(page.Body) == "") ||
			((page.PlatformCode == "csdn" || page.PlatformCode == "wechat_mp") && strings.TrimSpace(page.Body) == "") {
			return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_CONTENT_INVALID")
		}
		youtubeSettings := platforms.YouTubeSettings{}
		tikTokSettings := platforms.TikTokSettings{}
		wechatMPSettings := platforms.WechatMPSettings{PublishType: "publish", ContentSource: "none"}
		metaTarget := metaPublishTarget{}
		switch page.PlatformCode {
		case "csdn":
			if len(page.Media) > 0 {
				return nil, nil, nil, fmt.Errorf("EGO_CSDN_COVER_REQUIRES_REVIEW")
			}
			if len(tags) == 0 {
				return nil, nil, nil, fmt.Errorf("EGO_CSDN_TAG_REQUIRED")
			}
		case "youtube":
			youtubeSettings, err = normalizeYouTubeSettings(page.ContentType, page.SettingsJSON)
			if err != nil {
				return nil, nil, nil, err
			}
			if err := validateYouTubePublishMedia(page.Media); err != nil {
				return nil, nil, nil, err
			}
		case "tiktok":
			tikTokSettings, err = normalizeTikTokSettings(page.ContentType, page.SettingsJSON)
			if err != nil {
				return nil, nil, nil, err
			}
			if err := validateTikTokPublishMedia(page.Media); err != nil {
				return nil, nil, nil, err
			}
		case "kuaishou":
			if len(page.Media) != 1 {
				if len(page.Media) == 0 {
					return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_MEDIA_REQUIRED")
				}
				return nil, nil, nil, fmt.Errorf("EGO_PREPARE_MULTI_MEDIA_UNVERIFIED")
			}
		case "wechat_video":
			if err := validateWechatVideoPublishMedia(page.Media); err != nil {
				return nil, nil, nil, err
			}
			if err := validateWechatVideoSettings(page.SettingsJSON); err != nil {
				return nil, nil, nil, err
			}
			if len(tags) > 3 {
				return nil, nil, nil, fmt.Errorf("EGO_WECHAT_VIDEO_TAGS_INVALID")
			}
		case "wechat_mp":
			wechatMPSettings, err = normalizeWechatMPSettings(page.SettingsJSON)
			if err != nil {
				return nil, nil, nil, err
			}
			if err := validateWechatMPPublishMedia(page.Media); err != nil {
				return nil, nil, nil, err
			}
		case "facebook", "instagram":
			metaTarget, err = service.validateMetaPublishTarget(account, page)
			if err != nil {
				return nil, nil, nil, err
			}
			if err := validateMetaPublishMedia(page.ContentType, page.Media); err != nil {
				return nil, nil, nil, err
			}
		}
		composerAssetExternalID := page.AssetExternalID
		if page.PlatformCode == "instagram" {
			// Instagram is composed through its connected Facebook Page. The
			// Instagram asset remains in the frozen target for the pair readback.
			composerAssetExternalID = metaTarget.FacebookPage.ExternalID
		}
		composerURL, ok := egopublish.ComposerURLFor(page.PlatformCode, page.ContentType, composerAssetExternalID)
		if !ok {
			return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_COMPOSER_UNAVAILABLE")
		}
		if !capability.SubmitVerified {
			if !acceptance {
				return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_SUBMIT_UNVERIFIED")
			}
			if !capability.PrepareAdapterSupported || !capability.SubmitAdapterSupported || !capability.VerifyAdapterSupported {
				return nil, nil, nil, fmt.Errorf("EGO_PUBLISH_ACCEPTANCE_UNAVAILABLE")
			}
		}
		runtimeTargets = append(runtimeTargets, publishRuntimeTarget{
			PlatformCode: page.PlatformCode, ContentType: page.ContentType, ComposerURL: composerURL,
			PublishAt: page.PublishAt,
			Audience:  youtubeSettings.Audience, Visibility: youtubeSettings.Visibility,
			Playlist: youtubeSettings.Playlist, AllowComments: youtubeSettings.AllowComments,
			AlteredContent: youtubeSettings.AlteredContent,
			AllowComment:   tikTokSettings.AllowComment, AllowDuet: tikTokSettings.AllowDuet,
			AllowStitch: tikTokSettings.AllowStitch, AIGenerated: tikTokSettings.AIGenerated,
			AllowDownload: tikTokSettings.AllowDownload,
			Author:        wechatMPSettings.Author, CommentEnabled: wechatMPSettings.CommentEnabled,
			DeclareOriginal: wechatMPSettings.DeclareOriginal, AllowReprint: wechatMPSettings.AllowReprint,
			PublishType: wechatMPSettings.PublishType, Collection: wechatMPSettings.Collection,
			ContentSource: wechatMPSettings.ContentSource,
			MetaPageID:    metaTarget.FacebookPage.ExternalID, MetaPageName: metaTarget.FacebookPage.Name,
			MetaInstagramID: metaTarget.InstagramAccount.ExternalID, MetaInstagramName: metaTarget.InstagramAccount.Name,
			Title: page.Title, Caption: page.Caption, Body: page.Body, Tags: tags,
			Media: append([]PublishMedia(nil), page.Media...),
		})
		seen[page.PlatformCode] = struct{}{}
		accountIDs = append(accountIDs, account.ID)
		platformCodes = append(platformCodes, page.PlatformCode)
	}
	return runtimeTargets, accountIDs, platformCodes, nil
}

// ValidateSubmitPublishBatch performs deterministic frozen-input checks
// without acquiring a Space or reserving a final-click attempt. Submit still
// repeats the same checks immediately before runtime execution to prevent
// time-of-check/time-of-use changes.
func (service *SpaceService) ValidateSubmitPublishBatch(identityID int64, pages []PublishPage) error {
	// A final-click result is keyed by the reviewed target. Keeping this runtime
	// call to one target prevents two same-platform targets from collapsing into
	// the historical platform-code result map. The caller queues targets on the
	// same bound Space, so this does not create extra Spaces or parallel clicks.
	if len(pages) != 1 {
		return fmt.Errorf("EGO_PUBLISH_SINGLE_TARGET_REQUIRED")
	}
	_, _, _, err := service.validateSubmitPublishBatch(identityID, pages, false)
	return err
}

// ValidateAcceptanceSubmitPublishBatch is the fail-closed bootstrap path used
// to collect the first real submit and creator-manager evidence for one exact
// platform/content contract. It never widens the public verified capability:
// all three reviewed adapters must already exist and the caller must still use
// a single-target, submit-bound local approval.
func (service *SpaceService) ValidateAcceptanceSubmitPublishBatch(identityID int64, pages []PublishPage) error {
	if len(pages) != 1 {
		return fmt.Errorf("EGO_PUBLISH_SINGLE_TARGET_REQUIRED")
	}
	_, _, _, err := service.validateSubmitPublishBatch(identityID, pages, true)
	return err
}

// SubmitPublishBatchAndHandoff performs one submit-bound click per frozen
// reviewed platform target. It never retries an attempted click: absence of explicit
// creator-backend evidence is returned as result_unknown.
func (service *SpaceService) SubmitPublishBatchAndHandoff(ctx context.Context, identityID int64, pages []PublishPage) (*LoginBatch, error) {
	return service.submitPublishBatchAndHandoff(ctx, identityID, pages, false)
}

// SubmitAcceptancePublishBatchAndHandoff executes one explicit live-acceptance
// target through the same reviewed final-click program. It is intentionally a
// separate method so ordinary publishing remains gated by SubmitVerified.
func (service *SpaceService) SubmitAcceptancePublishBatchAndHandoff(ctx context.Context, identityID int64, pages []PublishPage) (*LoginBatch, error) {
	return service.submitPublishBatchAndHandoff(ctx, identityID, pages, true)
}

func (service *SpaceService) submitPublishBatchAndHandoff(ctx context.Context, identityID int64, pages []PublishPage, acceptance bool) (*LoginBatch, error) {
	if service == nil || service.runtime == nil {
		return nil, fmt.Errorf("EGO_PUBLISH_BATCH_INVALID")
	}
	if len(pages) != 1 {
		return nil, fmt.Errorf("EGO_PUBLISH_SINGLE_TARGET_REQUIRED")
	}
	runtimeTargets, accountIDs, platformCodes, err := service.validateSubmitPublishBatch(identityID, pages, acceptance)
	if err != nil {
		return nil, err
	}
	scheduledPlatform := make(map[string]bool, len(runtimeTargets))
	for _, target := range runtimeTargets {
		scheduledPlatform[target.PlatformCode] = strings.TrimSpace(target.PublishAt) != ""
	}
	resumedClaim := false
	if existing, existingErr := service.db.GetEgoSpaceByIdentity(identityID); existingErr == nil &&
		(existing.Ownership == "user" || existing.Ownership == "agentDelegatedToUser") {
		// The submit-bound approval authorizes reclaiming the already reviewed
		// Space for this one frozen batch. This is runtime control transfer, not
		// a publish task that the user must finish manually.
		if _, resumeErr := service.ResumeAfterConfirmation(ctx, accountIDs[0]); resumeErr != nil {
			return nil, resumeErr
		}
		resumedClaim = true
	}
	// The reclaim above already persisted an agent claim. If the work that
	// follows dies the row keeps claiming a live agent Space, which reads as
	// "still running" everywhere and blocks the next task, so the claim is
	// handed back together with the failure. Only a run that never answered
	// triggers this; every later branch records its own state.
	submitAnswered := false
	defer func() {
		if !resumedClaim || submitAnswered {
			return
		}
		service.restoreResumedSpaceClaim(ctx, identityID)
	}()
	space, err := service.EnsureIdentitySpace(ctx, identityID)
	if err != nil {
		return nil, err
	}
	var runtimeResult publishRuntimeResult
	if err := service.runtime.Run(ctx, buildEgoPublishSubmitProgram(space.SpaceID, runtimeTargets), &runtimeResult); err != nil {
		return nil, err
	}
	submitAnswered = true
	if !runtimeResult.Done {
		return nil, fmt.Errorf(ErrHandoffFailed)
	}
	// The submit program reports its click result first; Space completion is
	// always a separate dedicated runtime call so a failed release is
	// distinguishable from an unevaluated result.
	completion, completionErr := service.runPublishCompletion(ctx, space.SpaceID)
	runtimeResult.CompletionAttempted = true
	runtimeResult.CompletionDone = completionErr == nil && completion.Done
	statuses := make(map[string]string, len(platformCodes))
	errorsByPlatform := make(map[string]string, len(platformCodes))
	clickedByPlatform := make(map[string]bool, len(platformCodes))
	requestedPlatforms := make(map[string]struct{}, len(platformCodes))
	for _, platformCode := range platformCodes {
		requestedPlatforms[platformCode] = struct{}{}
	}
	validSubmitResultStatus := func(status string) bool {
		switch strings.TrimSpace(status) {
		case "failed", "submitted_pending_verification", "result_unknown":
			return true
		default:
			return false
		}
	}
	normalizedResults := make(map[string]publishRuntimeTargetResult, len(platformCodes))
	seenResults := make(map[string]struct{}, len(runtimeResult.Results))
	validResultSet := runtimeResult.Done && len(runtimeResult.Results) == len(platformCodes)
	for _, targetResult := range runtimeResult.Results {
		platformCode := strings.TrimSpace(targetResult.PlatformCode)
		targetResult.Status = strings.TrimSpace(targetResult.Status)
		targetResult.PlatformCode = platformCode
		_, requested := requestedPlatforms[platformCode]
		_, duplicate := seenResults[platformCode]
		if !requested || duplicate || !validSubmitResultStatus(targetResult.Status) {
			validResultSet = false
		}
		if requested && targetResult.Clicked != nil {
			if previous, exists := clickedByPlatform[platformCode]; !exists || *targetResult.Clicked {
				clickedByPlatform[platformCode] = *targetResult.Clicked || previous
			}
		}
		if requested && !duplicate {
			normalizedResults[platformCode] = targetResult
		}
		seenResults[platformCode] = struct{}{}
	}
	if len(seenResults) != len(platformCodes) {
		validResultSet = false
	}
	provenUnclicked := validResultSet
	for _, platformCode := range platformCodes {
		targetResult, exists := normalizedResults[platformCode]
		if !exists || targetResult.Clicked == nil || *targetResult.Clicked || targetResult.Status != "failed" {
			provenUnclicked = false
		}
	}
	unknownErrorCode := func(platformCode string) string {
		if scheduledPlatform[platformCode] {
			return egopublish.ScheduleResultUnknown
		}
		return "EGO_SUBMIT_RESULT_UNKNOWN"
	}
	unknownBatch := func(persist bool) (*LoginBatch, error) {
		for _, platformCode := range platformCodes {
			statuses[platformCode] = "result_unknown"
			errorsByPlatform[platformCode] = unknownErrorCode(platformCode)
		}
		batch := &LoginBatch{
			IdentityID: identityID, SpaceID: space.SpaceID, AccountIDs: accountIDs,
			PlatformCodes: platformCodes, PlatformStatuses: statuses,
			PlatformErrors: errorsByPlatform, PlatformClicked: clickedByPlatform, State: "result_unknown",
		}
		if persist {
			if _, stateErr := service.db.UpdateEgoSpaceStateForIdentity(identityID, "agentDelegatedToUser", "result_unknown", ErrPublishSpaceReleaseFailed); stateErr != nil {
				return nil, stateErr
			}
		}
		return batch, nil
	}
	completionSucceeded := runtimeResult.CompletionAttempted && runtimeResult.CompletionDone
	if !completionSucceeded {
		if !provenUnclicked {
			return unknownBatch(true)
		}
		return nil, fmt.Errorf(ErrPublishSpaceReleaseFailed)
	}
	if !validResultSet {
		if _, stateErr := service.db.UpdateEgoSpaceStateForIdentity(identityID, "agent", "ready", ""); stateErr != nil {
			return nil, stateErr
		}
		return unknownBatch(false)
	}
	for _, platformCode := range platformCodes {
		targetResult := normalizedResults[platformCode]
		status := strings.TrimSpace(targetResult.Status)
		if targetResult.Clicked == nil || (*targetResult.Clicked && status != "submitted_pending_verification" && status != "result_unknown") {
			status = "result_unknown"
		}
		statuses[platformCode] = status
		if targetResult.ErrorCode != "" {
			errorCode := targetResult.ErrorCode
			if scheduledPlatform[platformCode] && errorCode == "EGO_SUBMIT_RESULT_UNKNOWN" {
				errorCode = egopublish.ScheduleResultUnknown
			}
			errorsByPlatform[platformCode] = errorCode
		} else if status == "result_unknown" {
			errorsByPlatform[platformCode] = unknownErrorCode(platformCode)
		}
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(identityID, "agent", "ready", ""); err != nil {
		return nil, err
	}
	state := "failed"
	pendingCount := 0
	failedCount := 0
	unknown := false
	for _, platformCode := range platformCodes {
		switch statuses[platformCode] {
		case "submitted_pending_verification":
			pendingCount++
		case "result_unknown":
			unknown = true
		default:
			failedCount++
		}
	}
	if unknown {
		state = "result_unknown"
	} else if pendingCount == len(platformCodes) {
		state = "submitted_pending_verification"
	} else if pendingCount > 0 && failedCount > 0 {
		state = "partial_failed"
	}
	return &LoginBatch{
		IdentityID: identityID, SpaceID: space.SpaceID, AccountIDs: accountIDs,
		PlatformCodes: platformCodes, PlatformStatuses: statuses,
		PlatformErrors: errorsByPlatform, PlatformClicked: clickedByPlatform, State: state,
	}, nil
}

// InspectPublishManager performs a read-only, title-bound verification in the
// existing account Space. When frozenPublishAt is supplied, it is the
// immutable schedule from the frozen plan; the manager DOM must independently
// show the same local minute before scheduled evidence is accepted. The
// variadic form keeps legacy immediate-publish callers source compatible while
// allowing the plan verifier to pass its frozen target time.
func (service *SpaceService) InspectPublishManager(ctx context.Context, accountID int64, expectedTitle string, frozenPublishAt ...string) (*PublishManagerInspection, error) {
	if service == nil || service.db == nil || service.runtime == nil || accountID <= 0 || strings.TrimSpace(expectedTitle) == "" {
		return nil, fmt.Errorf("EGO_PUBLISH_INSPECTION_INVALID")
	}
	if len(frozenPublishAt) > 1 {
		return nil, fmt.Errorf("EGO_PUBLISH_INSPECTION_INVALID")
	}
	requestedPublishAt := ""
	if len(frozenPublishAt) == 1 {
		requestedPublishAt = strings.TrimSpace(frozenPublishAt[0])
	}
	return service.inspectPublishManagerTarget(ctx, accountID, "", expectedTitle, requestedPublishAt)
}

// InspectPublishManagerTarget performs the same read-only manager inspection
// with an explicit publish target. Facebook targets are backed by a logged-in
// Meta account and its formal Page asset; the manager adapter remains
// fail-closed until its URL and DOM contract are reviewed.
func (service *SpaceService) InspectPublishManagerTarget(ctx context.Context, accountID int64, platformCode, expectedMarker, frozenPublishAt string) (*PublishManagerInspection, error) {
	platformCode = strings.TrimSpace(platformCode)
	if platformCode == "" {
		return nil, fmt.Errorf("EGO_PUBLISH_INSPECTION_INVALID")
	}
	return service.inspectPublishManagerTarget(ctx, accountID, platformCode, expectedMarker, strings.TrimSpace(frozenPublishAt))
}

func (service *SpaceService) inspectPublishManagerTarget(ctx context.Context, accountID int64, platformCode, expectedMarker, requestedPublishAt string) (*PublishManagerInspection, error) {
	if service == nil || service.db == nil || service.runtime == nil || accountID <= 0 || strings.TrimSpace(expectedMarker) == "" {
		return nil, fmt.Errorf("EGO_PUBLISH_INSPECTION_INVALID")
	}
	account, err := service.db.GetAccount(accountID)
	if err != nil || !account.Enabled || account.IdentityID <= 0 || account.LoginState != db.AccountAuthLoggedIn {
		return nil, fmt.Errorf("EGO_PUBLISH_ACCOUNT_UNAVAILABLE")
	}
	inspectionPlatformCode := strings.TrimSpace(platformCode)
	if inspectionPlatformCode == "" {
		inspectionPlatformCode = account.PlatformCode
	}
	var facebookPage db.AccountAsset
	if inspectionPlatformCode == "facebook" {
		if account.PlatformCode != "meta" {
			return nil, fmt.Errorf("EGO_META_ACCOUNT_UNAVAILABLE")
		}
		selectedPage, assetErr := service.selectMetaAsset(account.ID, "facebook", "facebook_page", "EGO_META_FACEBOOK_ASSET_REQUIRED", "EGO_META_FACEBOOK_ASSET_AMBIGUOUS")
		if assetErr != nil {
			return nil, assetErr
		}
		facebookPage = selectedPage
		if assetErr := validateStoredMetaAsset(facebookPage, "EGO_META_FACEBOOK"); assetErr != nil {
			return nil, assetErr
		}
	}
	requestedLocalMinute := ""
	if requestedPublishAt != "" {
		var scheduleErr error
		requestedLocalMinute, scheduleErr = scheduledManagerLocalMinute(inspectionPlatformCode, requestedPublishAt)
		if scheduleErr != nil {
			return nil, fmt.Errorf("EGO_PUBLISH_SCHEDULE_INVALID")
		}
	}
	managerURL, hostSuffix, rowSelectors, ok := publishManagerInspectionConfig(inspectionPlatformCode)
	if !ok {
		return nil, fmt.Errorf("EGO_PUBLISH_VERIFY_ADAPTER_UNVERIFIED")
	}
	if inspectionPlatformCode == "facebook" {
		managerURL = "https://business.facebook.com/latest/planner/?asset_id=" + url.QueryEscape(strings.TrimSpace(facebookPage.ExternalID))
	}
	space, err := service.EnsureIdentitySpace(ctx, account.IdentityID)
	if err != nil {
		return nil, err
	}
	managerJSON, _ := json.Marshal(managerURL)
	hostSuffixJSON, _ := json.Marshal(hostSuffix)
	rowSelectorsJSON, _ := json.Marshal(rowSelectors)
	titleJSON, _ := json.Marshal(strings.TrimSpace(expectedMarker))
	platformJSON, _ := json.Marshal(inspectionPlatformCode)
	inspectionScriptJSON, _ := json.Marshal(publishManagerInspectionScript(
		inspectionPlatformCode, expectedMarker, hostSuffix, rowSelectors, requestedPublishAt, requestedLocalMinute,
		facebookPage.ExternalID, facebookPage.Name,
	))
	youtubeNavigationScriptJSON, _ := json.Marshal(youtubeManagerNavigationScript())
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const existingSpace = spaces.find((item) => Number(item.id) === %d)
if (!existingSpace) throw new Error(%s)
if (existingSpace.ownership === "agentDelegatedToUser" || existingSpace.ownership === "user") {
  throw new Error(%s)
}
await useOrCreateTaskSpace(%d)
const managerURL = %s
const hostSuffix = %s
const expectedTitle = %s
const rowSelectors = %s
const platformCode = %s
const beforeTabs = await listTabs()
const beforeIDs = new Set(beforeTabs.map((tab) => String(tab.targetId ?? tab.id ?? "")))
const prior = await currentTab()
const priorID = String(prior?.targetId ?? prior?.id ?? "")
await openOrReuseTab(managerURL, { wait: true, timeout: 30 })
await new Promise((resolve) => setTimeout(resolve, 1600))
if (platformCode === "youtube") {
  const contentNavigation = await js(%s)
  if (contentNavigation?.clicked) await new Promise((resolve) => setTimeout(resolve, 1600))
}
const manager = await currentTab()
const managerID = String(manager?.targetId ?? manager?.id ?? "")
const managerInspection = await js(%s)
let screenshotBase64 = ""
if (managerInspection.matchingTitle && (managerInspection.publishedState || managerInspection.scheduledState)) {
  const screenshot = await cdp("Page.captureScreenshot", { format: "png", fromSurface: true, captureBeyondViewport: false })
  if (typeof screenshot?.data === "string") screenshotBase64 = screenshot.data
}
if (priorID && managerID && priorID !== managerID) {
  if (!beforeIDs.has(managerID)) await closeTab(managerID)
  await switchTab(priorID)
}
let completionAttempted = false
let completionDone = false
try {
  completionAttempted = true
  const completed = await completeTaskSpace(%d, { keep: true })
  completionDone = completed?.done === true
} catch {
  completionDone = false
}
cliLog(%q + JSON.stringify({ done: true, ...managerInspection, screenshotBase64, completionAttempted, completionDone }))
`, space.SpaceID, jsErrorLiteral(ErrSpaceBindingMissing), jsErrorLiteral(ErrSpaceUserControlled), space.SpaceID, managerJSON, hostSuffixJSON, titleJSON, rowSelectorsJSON, platformJSON, youtubeNavigationScriptJSON, inspectionScriptJSON, space.SpaceID, resultPrefix)

	var runtimeResult publishManagerRuntimeResult
	if err := service.runtime.Run(ctx, program, &runtimeResult); err != nil {
		return nil, err
	}
	if !runtimeResult.CompletionAttempted || !runtimeResult.CompletionDone {
		_, _ = service.db.UpdateEgoSpaceStateForIdentity(account.IdentityID, "agentDelegatedToUser", "verification_failed", ErrPublishSpaceReleaseFailed)
		return nil, fmt.Errorf(ErrPublishSpaceReleaseFailed)
	}
	if !runtimeResult.Done {
		return nil, fmt.Errorf(ErrPublishRuntimeIncomplete)
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(account.IdentityID, "agent", "ready", ""); err != nil {
		return nil, err
	}
	if runtimeResult.PublishedState {
		observedStatus := strings.TrimSpace(runtimeResult.ObservedStatus)
		validPublishedStatus := observedStatus == "已发布" || observedStatus == "已发表" || observedStatus == "发布成功" || observedStatus == "发表成功" || observedStatus == "群发成功" || observedStatus == "公开" || observedStatus == "Published" || observedStatus == "Public" || observedStatus == "published"
		if runtimeResult.InspectionStatus != "inspected" || !runtimeResult.MatchingTitle || !validPublishedStatus {
			return nil, fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		if err := validatePublishedManagerRuntimeResult(inspectionPlatformCode, runtimeResult); err != nil {
			return nil, err
		}
	}
	if runtimeResult.PublishedState && runtimeResult.ScheduledState {
		return nil, fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
	}
	if runtimeResult.ScheduledState {
		if (inspectionPlatformCode != "douyin" && inspectionPlatformCode != "youtube" && inspectionPlatformCode != "tiktok" && inspectionPlatformCode != "kuaishou" && inspectionPlatformCode != "wechat_video" && inspectionPlatformCode != "wechat_mp" && inspectionPlatformCode != "facebook") || requestedPublishAt == "" ||
			runtimeResult.InspectionStatus != "inspected" || !runtimeResult.MatchingTitle ||
			!validScheduledManagerStatus(inspectionPlatformCode, runtimeResult.ObservedStatus) {
			return nil, fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		if err := validateScheduledManagerRuntimeResult(inspectionPlatformCode, requestedPublishAt, runtimeResult); err != nil {
			return nil, err
		}
	}
	inspection := &PublishManagerInspection{
		AccountID: account.ID, PlatformCode: inspectionPlatformCode,
		Location: runtimeResult.Location, InspectionStatus: runtimeResult.InspectionStatus,
		MatchingTitle: runtimeResult.MatchingTitle, PublishedState: runtimeResult.PublishedState,
		ScheduledState: runtimeResult.ScheduledState, EffectiveScheduledAt: runtimeResult.EffectiveScheduledAt,
		ObservedStatus: runtimeResult.ObservedStatus, PostURL: runtimeResult.PostURL,
		ExternalPostID: runtimeResult.ExternalPostID,
	}
	if inspection.MatchingTitle && (inspection.PublishedState || inspection.ScheduledState) {
		path, screenshotErr := service.persistPublishInspectionScreenshot(account.ID, runtimeResult.ScreenshotBase64)
		if screenshotErr != nil {
			return nil, screenshotErr
		}
		inspection.ScreenshotPath = path
	}
	return inspection, nil
}

// DeleteScheduledPublishContent performs the one reviewed destructive action
// supported by the EgoLite mainline: removing one independently recorded
// scheduled row. It reuses the bound Space, refuses user ownership, and
// returns success only after the exact row disappears from the manager DOM.
func (service *SpaceService) DeleteScheduledPublishContent(ctx context.Context, record db.PublishContentRecord) (*PublishContentDeletion, error) {
	if service == nil || service.db == nil || service.runtime == nil || record.ID <= 0 || record.AccountID <= 0 {
		return nil, fmt.Errorf("EGO_REMOTE_DELETE_INVALID")
	}
	platformCode := strings.TrimSpace(record.PlatformCode)
	if platformCode != "douyin" && platformCode != "youtube" && platformCode != "tiktok" &&
		platformCode != "kuaishou" && platformCode != "wechat_video" && platformCode != "facebook" {
		return nil, fmt.Errorf("EGO_REMOTE_DELETE_UNSUPPORTED")
	}
	if strings.TrimSpace(record.RemoteState) != "delete_pending" {
		return nil, fmt.Errorf("EGO_REMOTE_DELETE_STATE_INVALID")
	}
	title := strings.TrimSpace(record.Title)
	if title == "" {
		return nil, fmt.Errorf("EGO_REMOTE_DELETE_INVALID")
	}
	scheduledLocalMinute := ""
	scheduledDelete := strings.TrimSpace(record.ScheduledAt) != ""
	if platformCode == "kuaishou" {
		if err := validateKuaishouPublishedIdentity(record.PostURL, record.ExternalPostID); err == nil {
			scheduledDelete = false
		} else if !scheduledDelete {
			return nil, fmt.Errorf("EGO_REMOTE_DELETE_IDENTITY_INVALID")
		}
	}
	if scheduledDelete {
		scheduledAt, err := time.Parse(time.RFC3339, strings.TrimSpace(record.ScheduledAt))
		if err != nil || scheduledAt.Second() != 0 || scheduledAt.Nanosecond() != 0 {
			return nil, fmt.Errorf("EGO_REMOTE_DELETE_SCHEDULE_INVALID")
		}
		var scheduleErr error
		scheduledLocalMinute, scheduleErr = scheduledManagerLocalMinute(platformCode, scheduledAt.Format(time.RFC3339))
		if scheduleErr != nil {
			return nil, fmt.Errorf("EGO_REMOTE_DELETE_SCHEDULE_INVALID")
		}
	}
	account, err := service.db.GetAccount(record.AccountID)
	expectedAccountPlatform := platformCode
	if platformCode == "facebook" {
		expectedAccountPlatform = "meta"
	}
	if err != nil || !account.Enabled || account.IdentityID <= 0 ||
		account.PlatformCode != expectedAccountPlatform || account.LoginState != db.AccountAuthLoggedIn {
		return nil, fmt.Errorf("EGO_PUBLISH_ACCOUNT_UNAVAILABLE")
	}
	var facebookPage db.AccountAsset
	if platformCode == "facebook" {
		facebookPage, err = service.selectMetaAsset(account.ID, "facebook", "facebook_page", "EGO_META_FACEBOOK_ASSET_REQUIRED", "EGO_META_FACEBOOK_ASSET_AMBIGUOUS")
		if err != nil {
			return nil, err
		}
		if err := validateStoredMetaAsset(facebookPage, "EGO_META_FACEBOOK"); err != nil {
			return nil, err
		}
	}
	// Delete is a write action: the bound Space is read (never re-bound) and the
	// guarded program below reports a vanished Space instead of clicking.
	space, err := service.existingBindingSpace(account.IdentityID)
	if err != nil {
		return nil, err
	}
	managerURL := ""
	deleteScript := ""
	switch platformCode {
	case "douyin":
		managerURL = "https://creator.douyin.com/creator-micro/content/manage"
		deleteScript = douyinScheduledDeleteScript(title, scheduledLocalMinute)
	case "youtube":
		managerURL = "https://studio.youtube.com/"
		deleteScript = youtubeScheduledDeleteScript(title, scheduledLocalMinute)
	case "tiktok":
		managerURL = "https://www.tiktok.com/tiktokstudio/content"
		deleteScript = tiktokScheduledDeleteScript(title, scheduledLocalMinute)
	case "wechat_video":
		managerURL = "https://channels.weixin.qq.com/platform/post/list"
		deleteScript = wechatVideoScheduledDeleteScript(title, scheduledLocalMinute)
	case "kuaishou":
		managerURL = "https://cp.kuaishou.com/article/manage/video"
		if scheduledDelete {
			deleteScript = kuaishouScheduledDeleteScript(title, scheduledLocalMinute)
		} else {
			deleteScript = kuaishouPublishedDeleteScript(title, record.PostURL, record.ExternalPostID)
		}
	case "facebook":
		managerURL = "https://business.facebook.com/latest/planner/?asset_id=" + url.QueryEscape(strings.TrimSpace(facebookPage.ExternalID))
		deleteScript = facebookScheduledDeleteScript(title, scheduledLocalMinute, facebookPage.ExternalID, facebookPage.Name)
	}
	managerJSON, _ := json.Marshal(managerURL)
	deleteScriptJSON, _ := json.Marshal(deleteScript)
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const boundSpaceID = %d
const existingSpace = spaces.find((item) => Number(item.id) === boundSpaceID)
// A vanished Space is never re-bound here: a final click must not land in a new context.
if (!existingSpace) throw new Error(%s)
if (existingSpace.ownership === "agentDelegatedToUser" || existingSpace.ownership === "user") {
  throw new Error(%s)
}
await useOrCreateTaskSpace(%d)
const managerURL = %s
let deletionResult = { deleted: false, errorCode: "EGO_REMOTE_DELETE_RUNTIME_FAILED" }
let screenshotBase64 = ""
try {
  await openOrReuseTab(managerURL, { wait: true, timeout: 30 })
  await new Promise((resolve) => setTimeout(resolve, 1600))
  deletionResult = await js(%s)
  if (deletionResult?.deleted === true) {
    const screenshot = await cdp("Page.captureScreenshot", { format: "png", fromSurface: true, captureBeyondViewport: false })
    if (typeof screenshot?.data === "string") screenshotBase64 = screenshot.data
  }
} catch {
  deletionResult = { deleted: false, errorCode: "EGO_REMOTE_DELETE_RESULT_UNKNOWN", finalClickAttempted: Boolean(deletionResult?.finalClickAttempted) }
}
const handoff = await handOffTaskSpace(%d)
cliLog(%q + JSON.stringify({ done: Boolean(handoff?.done), ...deletionResult, screenshotBase64 }))
`, space.SpaceID, jsErrorLiteral(ErrSpaceNotFound), jsErrorLiteral(ErrSpaceUserControlled), space.SpaceID, managerJSON, deleteScriptJSON, space.SpaceID, resultPrefix)

	var runtimeResult publishContentDeleteRuntimeResult
	if err := service.runtime.Run(ctx, program, &runtimeResult); err != nil {
		switch err.Error() {
		case ErrSpaceUserControlled, ErrSpaceBindingMissing, ErrSpaceNotFound:
			return nil, err
		}
		return nil, fmt.Errorf("EGO_REMOTE_DELETE_RESULT_UNKNOWN")
	}
	if !runtimeResult.Done {
		return nil, fmt.Errorf(ErrHandoffFailed)
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(account.IdentityID, "agentDelegatedToUser", "manual_takeover", ""); err != nil {
		return nil, err
	}
	if !runtimeResult.Deleted {
		if runtimeResult.FinalClickAttempted {
			return nil, fmt.Errorf("EGO_REMOTE_DELETE_RESULT_UNKNOWN")
		}
		return nil, fmt.Errorf("%s", safePublishContentDeleteErrorCode(runtimeResult.ErrorCode))
	}
	if !runtimeResult.MatchingTitle ||
		(scheduledDelete && (!runtimeResult.MatchingScheduledState || !runtimeResult.MatchingScheduledMinute)) || runtimeResult.RowCandidateCount != 1 ||
		runtimeResult.DeleteMenuCount != 1 || runtimeResult.ConfirmButtonCount != 1 ||
		!runtimeResult.FinalClickAttempted {
		return nil, fmt.Errorf("EGO_REMOTE_DELETE_RESULT_INVALID")
	}
	screenshotPath, err := service.persistPublishInspectionScreenshot(account.ID, runtimeResult.ScreenshotBase64)
	if err != nil {
		return nil, err
	}
	return &PublishContentDeletion{
		RecordID: record.ID, AccountID: account.ID, PlatformCode: platformCode,
		Deleted: true, ScreenshotPath: screenshotPath,
	}, nil
}

func safePublishContentDeleteErrorCode(value string) string {
	switch strings.TrimSpace(value) {
	case "EGO_REMOTE_DELETE_NOT_FOUND", "EGO_REMOTE_DELETE_AMBIGUOUS",
		"EGO_REMOTE_DELETE_EVIDENCE_MISMATCH", "EGO_REMOTE_DELETE_ACTION_AMBIGUOUS",
		"EGO_REMOTE_DELETE_RUNTIME_FAILED", "EGO_REMOTE_DELETE_RESULT_UNKNOWN":
		return strings.TrimSpace(value)
	default:
		return "EGO_REMOTE_DELETE_RESULT_UNKNOWN"
	}
}

func publishManagerInspectionConfig(platformCode string) (managerURL, hostSuffix, rowSelectors string, ok bool) {
	switch platformCode {
	case "douyin":
		return "https://creator.douyin.com/creator-micro/content/manage", "douyin.com", "tr,[class*='content-card'],[class*='work-card'],[class*='video-card'],[class*='item-card']", true
	case "csdn":
		return "https://mp.csdn.net/mp_blog/manage/article", "csdn.net", "tr,[class*='article-item'],[class*='article-list-item'],[class*='blog-item']", true
	case "tiktok":
		return "https://www.tiktok.com/tiktokstudio/content", "tiktok.com", "article,tr,li,[role='row'],[data-e2e*='post'],[data-e2e*='content'],[class*='post-item'],[class*='content-item']", true
	case "kuaishou":
		return "https://cp.kuaishou.com/article/manage/video", "kuaishou.com", "article,li,[role='row'],[class*='work-item'],[class*='photo-item'],[class*='list-item']", true
	case "wechat_video":
		return "https://channels.weixin.qq.com/platform/post/list", "channels.weixin.qq.com", "article,li,[role='row'],[class*='list-item'],[class*='video-item']", true
	case "wechat_mp":
		return "https://mp.weixin.qq.com/cgi-bin/appmsg?t=media/appmsg_list2&type=9&action=list_card", "mp.weixin.qq.com", ".weui-desktop-masonry-list .weui-desktop-publish__cover__title,.weui-desktop-masonry-list .weui-desktop-publish__cover", true
	case "facebook":
		return "https://business.facebook.com/latest/planner/", "facebook.com", "article,li,[role='row'],[role='gridcell'],[data-testid*='planner'],[class*='planner'],[class*='scheduled']", true
	case "youtube":
		return "https://studio.youtube.com/", "youtube.com", "ytcp-video-row,tr,article", true
	default:
		return "", "", "", false
	}
}

func youtubeManagerNavigationScript() string {
	return `(() => {
  const normalize = (value) => String(value || '').normalize('NFKC').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  let locationSafe = ''
  try {
    const parsed = new URL(location.href)
    locationSafe = parsed.origin + parsed.pathname
  } catch {}
  if (/\/videos(?:\/|$)/u.test(location.pathname)) {
    return { onContent: true, clicked: false, location: locationSafe }
  }
  const candidates = [...document.querySelectorAll(
    'a[href],button,[role="link"],[role="button"],[role="tab"],ytcp-navigation-drawer-item',
  )].filter(visible)
  const entries = candidates.filter((candidate) => {
    const text = normalize(candidate.innerText || candidate.textContent)
    if (text !== 'Content' && text !== '内容') return false
    const descriptor = [
      candidate.getAttribute('href') || '', candidate.getAttribute('aria-label') || '',
      candidate.getAttribute('title') || '', text,
    ].join(' ')
    return !/edit|upload|publish|schedule|编辑|上传|发布|定时/iu.test(descriptor)
  })
  if (entries.length !== 1) return { onContent: false, clicked: false, location: locationSafe }
  const clickable = entries[0].closest('a[href],button,[role="link"],[role="button"],[role="tab"]') || entries[0]
  clickable.click()
  return { onContent: false, clicked: true, location: locationSafe }
})()`
}

func scheduledManagerLocalMinute(platformCode, raw string) (string, error) {
	requestedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil || requestedAt.Second() != 0 || requestedAt.Nanosecond() != 0 {
		return "", fmt.Errorf("invalid scheduled time")
	}
	if platformCode == "tiktok" {
		const step = 5 * time.Minute
		if remainder := requestedAt.UnixNano() % int64(step); remainder != 0 {
			requestedAt = requestedAt.Add(time.Duration(int64(step) - remainder))
		}
	}
	china := time.FixedZone("Asia/Shanghai", 8*60*60)
	return requestedAt.In(china).Format("2006-01-02 15:04"), nil
}

func publishManagerInspectionScript(platformCode, expectedTitle, hostSuffix, rowSelectors, expectedPublishAt, requestedLocalMinute, facebookPageID, facebookPageName string) string {
	expectedJSON, _ := json.Marshal(strings.TrimSpace(expectedTitle))
	if (platformCode == "douyin" || platformCode == "youtube" || platformCode == "tiktok" || platformCode == "kuaishou" || platformCode == "wechat_video" || platformCode == "facebook") && strings.TrimSpace(expectedPublishAt) != "" {
		publishAtJSON, _ := json.Marshal(strings.TrimSpace(expectedPublishAt))
		localMinuteJSON, _ := json.Marshal(strings.TrimSpace(requestedLocalMinute))
		rowSelectorsJSON, _ := json.Marshal(rowSelectors)
		hostSuffixJSON, _ := json.Marshal(hostSuffix)
		scheduledLabels := "['定时发布', '定时发布中', '待发布', 'Scheduled']"
		if platformCode == "youtube" {
			scheduledLabels = "['Scheduled', '定时发布', '定时发布中', '定时发表', '待发布']"
		} else if platformCode == "tiktok" {
			scheduledLabels = "['Scheduled', '定时发布', '待发布']"
		} else if platformCode == "kuaishou" {
			scheduledLabels = "['定时发布', '定时发布中', '待发布']"
		} else if platformCode == "wechat_video" {
			scheduledLabels = "['定时发表', '定时发布', '定时发布中', '待发布']"
		} else if platformCode == "facebook" {
			scheduledLabels = "['Scheduled', '已排期', '定时发布', '待发布']"
		}
		facebookPageIDJSON, _ := json.Marshal(strings.TrimSpace(facebookPageID))
		facebookPageNameJSON, _ := json.Marshal(strings.TrimSpace(facebookPageName))
		sourceRoots := "[document]"
		if platformCode == "wechat_video" {
			sourceRoots = `[
    document,
    ...[...document.querySelectorAll('iframe[src]')]
      .filter((frame) => {
        try { return new URL(frame.getAttribute('src'), location.href).href === 'https://channels.weixin.qq.com/micro/content/post/list' } catch { return false }
      })
      .map((frame) => frame.contentDocument)
      .filter(Boolean),
  ]`
		}
		return `(() => {
  const expected = ` + string(expectedJSON) + `
  const requestedPublishAt = ` + string(publishAtJSON) + `
  const requestedLocalMinute = ` + string(localMinuteJSON) + `
  const youtubeScheduleMinute = requestedLocalMinute
  const tiktokScheduleMinute = requestedLocalMinute
  const kuaishouScheduleMinute = requestedLocalMinute
  const wechatVideoScheduleMinute = requestedLocalMinute
  const selectors = ` + string(rowSelectorsJSON) + `
	  const suffix = ` + string(hostSuffixJSON) + `
	  const facebookPageID = ` + string(facebookPageIDJSON) + `
	  const facebookPageName = ` + string(facebookPageNameJSON) + `
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const locationSafe = (() => {
    try {
      const parsed = new URL(location.href)
      return parsed.origin + parsed.pathname
    } catch { return '' }
  })()
	  const empty = (matchingTitle = false, rawRowCandidateCount = 0, rowCandidateCount = 0) => ({
    location: locationSafe, inspectionStatus: 'observed', matchingTitle,
    publishedState: false, scheduledState: false, effectiveScheduledAt: '',
    observedStatus: '', postURL: '', externalPostID: '',
	    rawRowCandidateCount, rowCandidateCount,
	  })
	  if (facebookPageID || facebookPageName) {
	    const params = new URLSearchParams(location.search)
	    const pageCandidates = [...document.querySelectorAll("[role='combobox'],[aria-selected='true'],[aria-current='true'],[data-page-id],[data-asset-id]")]
	      .filter(visible).filter((node) => normalize(node.innerText || node.textContent || node.getAttribute('aria-label')) === normalize(facebookPageName))
	    if (location.hostname !== 'business.facebook.com' || params.get('asset_id') !== facebookPageID || pageCandidates.length !== 1) return empty()
	  }
	  const hasExactTitle = (node) => [...node.querySelectorAll('*')]
    .filter(visible)
    .some((candidate) => normalize(candidate.innerText || candidate.textContent) === normalize(expected))
    || normalize(node.innerText || node.textContent).split(/\r?\n/).map(normalize).includes(normalize(expected))
	  const sourceRoots = ` + sourceRoots + `
	  const candidateRows = sourceRoots.flatMap((root) => [...root.querySelectorAll(selectors)])
    .filter((node) => visible(node) && hasExactTitle(node))
  const rows = candidateRows.filter((row) => !candidateRows.some((other) => other !== row && row.contains(other)))
  if (rows.length !== 1) return empty(rows.length > 0, candidateRows.length, rows.length)
  const row = rows[0]
  const visibleLabels = [row, ...row.querySelectorAll('*')].filter(visible)
    .map((node) => normalize(node.innerText || node.getAttribute('aria-label') || node.textContent))
  const scheduledLabels = ` + scheduledLabels + `
  const publishedLabels = ['已发布', '发布成功', '公开', 'Published', 'Public', 'published']
  const scheduledStatus = scheduledLabels.find((label) => visibleLabels.includes(label)) || ''
  const publishedStatus = publishedLabels.find((label) => visibleLabels.includes(label)) || ''
  if (scheduledStatus && publishedStatus) return empty(true, candidateRows.length, rows.length)
  const localMinute = (year, month, day, hour, minute) => {
    const date = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day), Number(hour) - 8, Number(minute), 0))
    if (!Number.isFinite(date.getTime())) return ''
    return date.toISOString()
  }
  const matchingEffectiveScheduledAt = (rawText) => {
    const text = normalize(rawText)
    const fullDate = /(?:^|[^\d])(\d{4})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(?:[T ]+)\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    const shortDate = /(?:^|[^\d])(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    const requested = requestedLocalMinute
    let match
    while ((match = fullDate.exec(text)) !== null) {
      const local = String(match[1]).padStart(4, '0') + '-' + String(match[2]).padStart(2, '0') + '-' + String(match[3]).padStart(2, '0') + ' ' + String(match[4]).padStart(2, '0') + ':' + match[5]
      if (local === requested) return localMinute(match[1], match[2], match[3], match[4], match[5])
    }
    while ((match = shortDate.exec(text)) !== null) {
      const local = requested.slice(0, 4) + '-' + String(match[1]).padStart(2, '0') + '-' + String(match[2]).padStart(2, '0') + ' ' + String(match[3]).padStart(2, '0') + ':' + match[4]
      if (local === requested) return localMinute(requested.slice(0, 4), match[1], match[2], match[3], match[4])
    }
    return ''
  }
  const publicLinks = new Map()
  const inspectPublicLink = (raw) => {
    let candidate
    try { candidate = new URL(raw, location.href) } catch { return }
    const host = candidate.hostname.toLowerCase()
    if (candidate.protocol !== 'https:' || !(host === suffix || host.endsWith('.' + suffix))) return
    const postURL = candidate.origin + candidate.pathname
    const externalPostID = candidate.pathname.split('/').filter(Boolean).pop() || ''
    if (externalPostID) publicLinks.set(postURL, externalPostID)
  }
  for (const anchor of row.querySelectorAll('a[href]')) inspectPublicLink(anchor.getAttribute('href'))
  if (scheduledStatus) {
    const effectiveScheduledAt = matchingEffectiveScheduledAt(row.innerText || row.textContent)
    if (!effectiveScheduledAt) return {
      location: locationSafe, inspectionStatus: 'observed', matchingTitle: true,
      publishedState: false, scheduledState: false, effectiveScheduledAt: '',
      observedStatus: scheduledStatus, postURL: '', externalPostID: '',
      rawRowCandidateCount: candidateRows.length, rowCandidateCount: rows.length,
    }
    return {
      location: locationSafe, inspectionStatus: 'inspected', matchingTitle: true,
      publishedState: false, scheduledState: true, effectiveScheduledAt,
      observedStatus: scheduledStatus, postURL: '', externalPostID: '',
      rawRowCandidateCount: candidateRows.length, rowCandidateCount: rows.length,
    }
  }
  const publicEntries = [...publicLinks.entries()]
  const publishedState = Boolean(publishedStatus && publicEntries.length === 1)
  const [postURL, externalPostID] = publishedState ? publicEntries[0] : ['', '']
  return {
    location: locationSafe, inspectionStatus: publishedState ? 'inspected' : 'observed',
    matchingTitle: true, publishedState, scheduledState: false, effectiveScheduledAt: '',
    observedStatus: publishedState ? publishedStatus : '', postURL, externalPostID,
    rawRowCandidateCount: candidateRows.length, rowCandidateCount: rows.length,
  }
})()`
	}
	if platformCode == "tiktok" {
		return `(() => {
  const expected = ` + string(expectedJSON) + `
  const normalize = (value) => String(value || '').normalize('NFKC').replace(/[｜|]/gu, '|').replace(/\s+/gu, '').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const locationSafe = (() => {
    try {
      const parsed = new URL(location.href)
      return parsed.origin + parsed.pathname
    } catch { return '' }
  })()
  const empty = (matchingTitle = false, rawRowCandidateCount = 0, rowCandidateCount = 0) => ({
    location: locationSafe, inspectionStatus: 'observed', matchingTitle,
    publishedState: false, observedStatus: '', postURL: '', externalPostID: '',
    publicVisibility: false, rawRowCandidateCount, rowCandidateCount,
    linkCandidateCount: 0,
  })
  const body = document.body
  if (!body || !expected) return empty()
  const semantic = [...body.querySelectorAll(
    "article,tr,li,[role='row'],[data-e2e*='post'],[data-e2e*='content'],[class*='post-item'],[class*='content-item']",
  )].filter((node) => visible(node) && normalize(node.innerText || node.textContent).includes(normalize(expected)))
  const inferredRows = new Set()
  const textNodes = [...body.querySelectorAll('h1,h2,h3,p,span,div')]
    .filter((node) => visible(node) && normalize(node.innerText || node.textContent).includes(normalize(expected)))
    .sort((left, right) => normalize(left.innerText || left.textContent).length - normalize(right.innerText || right.textContent).length)
    .slice(0, 20)
  for (const textNode of textNodes) {
    let ancestor = textNode
    for (let depth = 0; ancestor && ancestor !== body && depth < 12; depth += 1) {
      const hasMedia = Boolean(ancestor.querySelector('img,video,canvas'))
      const actionCount = ancestor.querySelectorAll('a[href],button,[role="button"]').length
      const rowLike = ancestor.matches('article,tr,li,[role="row"],[class*="post-item"],[class*="content-item"]')
      if ((hasMedia || rowLike) && actionCount > 0 && normalize(ancestor.innerText || ancestor.textContent).includes(normalize(expected))) {
        inferredRows.add(ancestor)
        break
      }
      ancestor = ancestor.parentElement
    }
  }
  const rawRows = [...(inferredRows.size > 0 ? inferredRows : new Set(semantic))]
  const visibilityLabels = new Set(['所有人', 'Everyone', 'Public', '公开', '仅自己', 'Onlyme', 'Private', '私享'])
  const rowsWithVisibility = rawRows.filter((candidate) => [...candidate.querySelectorAll('*')]
    .filter(visible)
    .some((node) => visibilityLabels.has(normalize(node.innerText || node.getAttribute('aria-label')))))
  const scopedRows = rowsWithVisibility.length > 0 ? rowsWithVisibility : rawRows
  const rows = scopedRows.filter((candidate) => !scopedRows.some((other) => other !== candidate && candidate.contains(other)))
  if (rows.length !== 1) return empty(rawRows.length > 0, rawRows.length, rows.length)
  const row = rows[0]
  const labels = [...row.querySelectorAll('*')].filter(visible)
    .map((node) => normalize(node.innerText || node.getAttribute('aria-label')))
  const publicVisibility = labels.some((label) => ['所有人', 'Everyone', 'Public', '公开'].includes(label))
  const privateVisibility = labels.some((label) => ['仅自己', 'Onlyme', 'Private', '私享'].includes(label))
  const publicLinks = new Map()
  const inspectPublicLink = (raw) => {
    let candidate
    try { candidate = new URL(raw, 'https://www.tiktok.com') } catch { return }
    if (candidate.protocol !== 'https:' || candidate.hostname !== 'www.tiktok.com') return
    const match = candidate.pathname.match(/^\/@([A-Za-z0-9._-]{2,64})\/video\/([0-9]{10,32})\/?$/u)
    if (!match) return
    const url = 'https://www.tiktok.com/@' + match[1] + '/video/' + match[2]
    publicLinks.set(url, match[2])
  }
  for (const anchor of row.querySelectorAll('a[href]')) inspectPublicLink(anchor.getAttribute('href'))
  for (const node of [row, ...row.querySelectorAll('*')]) {
    for (const attribute of node.attributes || []) {
      if (attribute.name === 'href' || !/\/@[A-Za-z0-9._-]{2,64}\/video\/[0-9]{10,32}/u.test(attribute.value)) continue
      inspectPublicLink(attribute.value)
    }
  }
  const publicEntries = [...publicLinks.entries()]
  const publishedState = publicVisibility && !privateVisibility && publicEntries.length === 1
  const [postURL, externalPostID] = publishedState ? publicEntries[0] : ['', '']
  return {
    location: locationSafe, inspectionStatus: publishedState ? 'inspected' : 'observed',
    matchingTitle: true, publishedState, observedStatus: publishedState ? 'Public' : '',
    postURL, externalPostID, publicVisibility,
    rawRowCandidateCount: rawRows.length, rowCandidateCount: rows.length,
    linkCandidateCount: publicEntries.length,
  }
})()`
	}
	if platformCode == "youtube" {
		return `(() => {
  const expected = ` + string(expectedJSON) + `
  const expectedTitles = new Set([
    expected,
    expected.replaceAll('｜', '|'),
    expected.replace(/\s*\|\s*/gu, ' | '),
  ].map((value) => String(value || '').normalize('NFKC').replace(/\s+/gu, ' ').trim()).filter(Boolean))
  const normalize = (value) => String(value || '').normalize('NFKC').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const locationSafe = (() => {
    try {
      const parsed = new URL(location.href)
      return parsed.origin + parsed.pathname
    } catch { return '' }
  })()
  const rows = [...document.querySelectorAll('ytcp-video-row,tr,article')]
    .filter((row) => visible(row))
    .filter((row) => [...row.querySelectorAll('#video-title,[id="video-title"],a[title],h1,h2')]
      .filter(visible)
      .some((control) => [control.innerText, control.getAttribute('title')]
        .some((value) => expectedTitles.has(normalize(value)))))
  const uniqueRows = rows.filter((row) => !rows.some((other) => other !== row && row.contains(other)))
  if (uniqueRows.length !== 1) {
    return {
      location: locationSafe, inspectionStatus: 'observed', matchingTitle: uniqueRows.length > 0,
      publishedState: false, observedStatus: '', postURL: '', externalPostID: '',
    }
  }
  const row = uniqueRows[0]
  const exactVisible = (labels) => labels.find((label) => [...row.querySelectorAll('*')]
    .filter(visible)
    .some((node) => normalize(node.innerText || node.textContent) === label)) || ''
  const scheduledStatus = exactVisible(['Scheduled', '定时发布', '定时发布中', '定时发表', '待发布'])
  if (scheduledStatus) {
    return {
      location: locationSafe, inspectionStatus: 'observed', matchingTitle: true,
      publishedState: false, observedStatus: '', postURL: '', externalPostID: '',
    }
  }
  const publicStatus = exactVisible(['Public', 'Published', '公开', '已发布'])
  const editIDs = new Set()
  for (const anchor of row.querySelectorAll('a[href]')) {
    let reviewed
    try { reviewed = new URL(anchor.getAttribute('href'), location.href) } catch { continue }
    if (reviewed.protocol !== 'https:' || reviewed.hostname !== 'studio.youtube.com') continue
    const match = reviewed.pathname.match(/^\/video\/([A-Za-z0-9_-]{6,64})\/edit$/u)
    if (match) editIDs.add(match[1])
  }
  const publishedState = Boolean(publicStatus && editIDs.size === 1)
  const externalPostID = publishedState ? [...editIDs][0] : ''
  return {
    location: locationSafe, inspectionStatus: publishedState ? 'inspected' : 'observed',
    matchingTitle: true, publishedState,
    observedStatus: publishedState ? publicStatus : '',
    postURL: publishedState ? 'https://www.youtube.com/shorts/' + externalPostID : '',
    externalPostID,
  }
})()`
	}
	if platformCode == "wechat_mp" {
		publishAtJSON, _ := json.Marshal(strings.TrimSpace(expectedPublishAt))
		localMinuteJSON, _ := json.Marshal(strings.TrimSpace(requestedLocalMinute))
		rowSelectorsJSON, _ := json.Marshal(rowSelectors)
		return `(() => {
  const expected = ` + string(expectedJSON) + `
  const requestedPublishAt = ` + string(publishAtJSON) + `
  const requestedLocalMinute = ` + string(localMinuteJSON) + `
  const wechatMPScheduleMinute = requestedLocalMinute
  const selectors = ` + string(rowSelectorsJSON) + `
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const locationSafe = (() => {
    try {
      const parsed = new URL(location.href)
      return parsed.origin + parsed.pathname
    } catch { return '' }
  })()
  const empty = (matchingTitle = false, rawRowCandidateCount = 0, rowCandidateCount = 0) => ({
    location: locationSafe, inspectionStatus: 'observed', matchingTitle,
    publishedState: false, scheduledState: false, effectiveScheduledAt: '',
    observedStatus: '', postURL: '', externalPostID: '',
    rawRowCandidateCount, rowCandidateCount,
  })
  const hasExactTitle = (node) => {
    const expectedTitle = normalize(expected)
    if (!expectedTitle) return false
    const lines = normalize(node.innerText || node.textContent || '').split(/\r?\n/gu).map(normalize)
    if (lines.includes(expectedTitle)) return true
    return [node, ...node.querySelectorAll('*')].filter(visible)
      .some((candidate) => normalize(candidate.innerText || candidate.textContent || '') === expectedTitle)
  }
  const candidateRows = [...document.querySelectorAll(selectors)]
    .filter((node) => visible(node) && hasExactTitle(node))
    .map((node) => {
      if (!node.matches('.weui-desktop-publish__cover__title')) return node
      return node.closest('.weui-desktop-publish__cover') || node
    })
  const uniqueCandidateRows = [...new Set(candidateRows)]
  const rows = uniqueCandidateRows.filter((row) => !uniqueCandidateRows.some((other) => other !== row && row.contains(other)))
  if (rows.length !== 1) return empty(rows.length > 0, uniqueCandidateRows.length, rows.length)
  const row = rows[0]
  const visibleLabels = [row, ...row.querySelectorAll('*')].filter(visible)
    .map((node) => normalize(node.innerText || node.getAttribute('aria-label') || node.textContent || ''))
  const scheduledLabels = ['定时发表', '定时发布', '定时发布中', '待发布', 'Scheduled']
  const publishedLabels = ['已发表', '已发布', '发表成功', '发布成功', '群发成功', 'Published', 'Public']
  const scheduledStatus = scheduledLabels.find((label) => visibleLabels.includes(label)) || ''
  const publishedStatus = publishedLabels.find((label) => visibleLabels.includes(label)) || ''
  if (scheduledStatus && publishedStatus) return empty(true, uniqueCandidateRows.length, rows.length)
  const localMinute = (year, month, day, hour, minute) => {
    const date = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day), Number(hour) - 8, Number(minute), 0))
    if (!Number.isFinite(date.getTime())) return ''
    return date.toISOString()
  }
  const matchingEffectiveScheduledAt = (rawText) => {
    const text = normalize(rawText)
    const requested = wechatMPScheduleMinute
    if (!requested) return ''
    const fullDate = /(?:^|[^\d])(\d{4})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(?:[T ]+)\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    const shortDate = /(?:^|[^\d])(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    let match
    while ((match = fullDate.exec(text)) !== null) {
      const local = String(match[1]).padStart(4, '0') + '-' + String(match[2]).padStart(2, '0') + '-' + String(match[3]).padStart(2, '0') + ' ' + String(match[4]).padStart(2, '0') + ':' + match[5]
      if (local === requested) return localMinute(match[1], match[2], match[3], match[4], match[5])
    }
    while ((match = shortDate.exec(text)) !== null) {
      const local = requested.slice(0, 4) + '-' + String(match[1]).padStart(2, '0') + '-' + String(match[2]).padStart(2, '0') + ' ' + String(match[3]).padStart(2, '0') + ':' + match[4]
      if (local === requested) return localMinute(requested.slice(0, 4), match[1], match[2], match[3], match[4])
    }
    return ''
  }
  if (scheduledStatus) {
    const effectiveScheduledAt = matchingEffectiveScheduledAt(row.innerText || row.textContent || '')
    if (!effectiveScheduledAt) return {
      location: locationSafe, inspectionStatus: 'observed', matchingTitle: true,
      publishedState: false, scheduledState: false, effectiveScheduledAt: '',
      observedStatus: scheduledStatus, postURL: '', externalPostID: '',
      rawRowCandidateCount: uniqueCandidateRows.length, rowCandidateCount: rows.length,
    }
    return {
      location: locationSafe, inspectionStatus: 'inspected', matchingTitle: true,
      publishedState: false, scheduledState: true, effectiveScheduledAt,
      observedStatus: scheduledStatus, postURL: '', externalPostID: '',
      rawRowCandidateCount: uniqueCandidateRows.length, rowCandidateCount: rows.length,
    }
  }
  const publicLinks = new Map()
  const inspectPublicLink = (raw) => {
    let candidate
    try { candidate = new URL(raw, location.href) } catch { return }
    if (candidate.protocol !== 'https:' || candidate.hostname.toLowerCase() !== 'mp.weixin.qq.com') return
    if (candidate.pathname !== '/s' && !candidate.pathname.startsWith('/s/')) return
    const parts = candidate.pathname.split('/').filter(Boolean)
    const externalPostID = parts.length === 2 && parts[0] === 's'
      ? parts[1] : String(candidate.searchParams.get('sn') || '').trim()
    if (!/^[A-Za-z0-9_-]{1,128}$/u.test(externalPostID)) return
    const allowedParams = ['__biz', 'mid', 'idx', 'sn']
    const safeQuery = new URLSearchParams()
    for (const name of allowedParams) {
      const value = candidate.searchParams.get(name)
      if (value) safeQuery.set(name, value)
    }
    const postURL = candidate.origin + candidate.pathname + (safeQuery.toString() ? '?' + safeQuery.toString() : '')
    publicLinks.set(postURL, externalPostID)
  }
  for (const anchor of row.querySelectorAll('a[href]')) inspectPublicLink(anchor.getAttribute('href'))
  const publicEntries = [...publicLinks.entries()]
  const publishedState = Boolean(publishedStatus && publicEntries.length === 1)
  const [postURL, externalPostID] = publishedState ? publicEntries[0] : ['', '']
  return {
    location: locationSafe, inspectionStatus: publishedState ? 'inspected' : 'observed',
    matchingTitle: true, publishedState, scheduledState: false, effectiveScheduledAt: '',
    observedStatus: publishedState ? publishedStatus : '', postURL, externalPostID,
    rawRowCandidateCount: uniqueCandidateRows.length, rowCandidateCount: rows.length,
  }
})()`
	}
	hostSuffixJSON, _ := json.Marshal(hostSuffix)
	rowSelectorsJSON, _ := json.Marshal(rowSelectors)
	return `(() => {
  const expected = ` + string(expectedJSON) + `
  const selectors = ` + string(rowSelectorsJSON) + `
  const suffix = ` + string(hostSuffixJSON) + `
  const normalize = (value) => String(value || '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0
  }
  const hasExactTitle = (node) => String(node.innerText || node.textContent || '')
    .split(/\r?\n/).map(normalize).some((line) => line === expected)
  let rows = [...document.querySelectorAll(selectors)].filter((node) => visible(node) && hasExactTitle(node))
  rows = rows.filter((row) => !rows.some((other) => other !== row && row.contains(other)))
  const row = rows.length === 1 ? rows[0] : null
  const statusLabels = ['已发布', '公开', 'Published', 'Public']
  let observedStatus = ''
  if (row) {
    const lines = String(row.innerText || row.textContent || '').split(/\r?\n/).map(normalize)
    observedStatus = statusLabels.find((label) => lines.includes(label)) || ''
  }
  let postURL = '', externalPostID = ''
  if (row) {
    for (const link of row.querySelectorAll('a[href]')) {
      try {
        const parsed = new URL(link.href, location.href)
        const host = parsed.hostname.toLowerCase()
        if (parsed.protocol === 'https:' && (host === suffix || host.endsWith('.' + suffix))) {
          postURL = parsed.origin + parsed.pathname
          externalPostID = parsed.pathname.split('/').filter(Boolean).pop() || ''
          break
        }
      } catch {}
    }
  }
  let locationSafe = ''
  try {
    const parsed = new URL(location.href)
    locationSafe = parsed.origin + parsed.pathname
  } catch {}
  return {
    location: locationSafe, inspectionStatus: row && observedStatus ? 'inspected' : 'observed',
    matchingTitle: Boolean(row), publishedState: Boolean(row && observedStatus), observedStatus,
    postURL, externalPostID,
  }
})()`
}

func douyinScheduledDeleteScript(expectedTitle, scheduledLocalMinute string) string {
	titleJSON, _ := json.Marshal(strings.TrimSpace(expectedTitle))
	minuteJSON, _ := json.Marshal(strings.TrimSpace(scheduledLocalMinute))
	return `(async () => {
  const expectedTitle = ` + string(titleJSON) + `
  const expectedMinute = ` + string(minuteJSON) + `
  const rowSelectors = "tr,[class*='content-card'],[class*='work-card'],[class*='video-card'],[class*='item-card']"
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const wait = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds))
  const exactText = (node) => normalize(
    node?.getAttribute?.('aria-label') || node?.getAttribute?.('title') || node?.innerText || node?.textContent,
  )
  const hasExactTitle = (node) => [node, ...node.querySelectorAll('*')]
    .filter(visible)
    .some((candidate) => exactText(candidate) === expectedTitle)
    || normalize(node.innerText || node.textContent).split(/\r?\n/gu).map(normalize).includes(expectedTitle)
  const titleRows = () => {
    const candidates = [...document.querySelectorAll(rowSelectors)]
      .filter((node) => visible(node) && hasExactTitle(node))
    return candidates.filter((row) => !candidates.some((other) => other !== row && row.contains(other)))
  }
  const scheduledLabels = new Set(['定时发布', '定时发布中', '待发布', 'Scheduled'])
  const hasScheduledState = (row) => [row, ...row.querySelectorAll('*')]
    .filter(visible).some((node) => scheduledLabels.has(exactText(node)))
  const hasScheduledMinute = (row) => {
    const text = normalize(row.innerText || row.textContent)
    const fullDate = /(?:^|[^\d])(\d{4})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(?:[T ]+)\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    const shortDate = /(?:^|[^\d])(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    let match
    while ((match = fullDate.exec(text)) !== null) {
      const minute = String(match[1]).padStart(4, '0') + '-' + String(match[2]).padStart(2, '0') + '-'
        + String(match[3]).padStart(2, '0') + ' ' + String(match[4]).padStart(2, '0') + ':' + match[5]
      if (minute === expectedMinute) return true
    }
    while ((match = shortDate.exec(text)) !== null) {
      const minute = expectedMinute.slice(0, 4) + '-' + String(match[1]).padStart(2, '0') + '-'
        + String(match[2]).padStart(2, '0') + ' ' + String(match[3]).padStart(2, '0') + ':' + match[4]
      if (minute === expectedMinute) return true
    }
    return false
  }
  const base = {
    deleted: false, matchingTitle: false, matchingScheduledState: false,
    matchingScheduledMinute: false, rowCandidateCount: 0, deleteMenuCount: 0,
    confirmButtonCount: 0, finalClickAttempted: false,
  }
  const rows = titleRows()
  if (rows.length === 0) return { ...base, errorCode: 'EGO_REMOTE_DELETE_NOT_FOUND' }
  if (rows.length !== 1) return {
    ...base, matchingTitle: true, rowCandidateCount: rows.length,
    errorCode: 'EGO_REMOTE_DELETE_AMBIGUOUS',
  }
  const row = rows[0]
  const matchingScheduledState = hasScheduledState(row)
  const matchingScheduledMinute = hasScheduledMinute(row)
  const evidence = {
    ...base, matchingTitle: true, matchingScheduledState, matchingScheduledMinute,
    rowCandidateCount: 1,
  }
  if (!matchingScheduledState || !matchingScheduledMinute) {
    return { ...evidence, errorCode: 'EGO_REMOTE_DELETE_EVIDENCE_MISMATCH' }
  }
  const menuLabels = new Set(['更多', '更多操作', '操作', 'More', 'More actions', 'Actions', '…', '...'])
  const menuControls = [...row.querySelectorAll('button,[role="button"]')]
    .filter((node) => visible(node) && menuLabels.has(exactText(node)))
  if (menuControls.length !== 1) {
    return { ...evidence, deleteMenuCount: menuControls.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  }
  menuControls[0].click()
  await wait(350)
  const deleteLabels = new Set(['删除', '删除作品', '删除视频', 'Delete'])
  const deleteItems = [...document.querySelectorAll(
    '[role="menuitem"],button,[role="button"],li,[class*="menu-item"],[class*="dropdown-item"]',
  )].filter((node) => visible(node) && deleteLabels.has(exactText(node)))
    .filter((node) => !node.closest('[aria-modal="true"],[role="dialog"],[class*="modal"],[class*="dialog"]'))
  if (deleteItems.length !== 1) {
    return { ...evidence, deleteMenuCount: deleteItems.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  }
  deleteItems[0].click()
  await wait(350)
  const dialogCandidates = [...document.querySelectorAll(
    '[aria-modal="true"],[role="dialog"],[class*="modal"],[class*="dialog"]',
  )].filter(visible).filter((node) => {
    const text = normalize(node.innerText || node.textContent)
    return /删除|Delete/iu.test(text)
  })
  const dialogs = dialogCandidates.filter((dialog) => !dialogCandidates.some(
    (other) => other !== dialog && dialog.contains(other),
  ))
  if (dialogs.length !== 1) {
    return { ...evidence, deleteMenuCount: 1, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  }
  const confirmLabels = new Set(['确认删除', '确定删除', '删除', 'Delete', 'Confirm delete'])
  const confirmButtons = [...dialogs[0].querySelectorAll('button,[role="button"]')]
    .filter((node) => visible(node) && confirmLabels.has(exactText(node)))
  if (confirmButtons.length !== 1) {
    return {
      ...evidence, deleteMenuCount: 1, confirmButtonCount: confirmButtons.length,
      errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
    }
  }
  confirmButtons[0].click()
  const clicked = {
    ...evidence, deleteMenuCount: 1, confirmButtonCount: 1, finalClickAttempted: true,
  }
  for (let attempt = 0; attempt < 10; attempt += 1) {
    await wait(500)
    if (titleRows().length === 0) return { ...clicked, deleted: true, errorCode: '' }
  }
  return { ...clicked, errorCode: 'EGO_REMOTE_DELETE_RESULT_UNKNOWN' }
})()`
}

func youtubeScheduledDeleteScript(expectedTitle, scheduledLocalMinute string) string {
	titleJSON, _ := json.Marshal(strings.TrimSpace(expectedTitle))
	minuteJSON, _ := json.Marshal(strings.TrimSpace(scheduledLocalMinute))
	return `(async () => {
  const expectedTitle = ` + string(titleJSON) + `
  const expectedMinute = ` + string(minuteJSON) + `
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const expectedTitles = new Set([
    expectedTitle, expectedTitle.replaceAll('｜', '|'), expectedTitle.replace(/\s*\|\s*/gu, ' | '),
  ].map(normalize).filter(Boolean))
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const wait = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds))
  const exactText = (node) => normalize(
    node?.getAttribute?.('aria-label') || node?.getAttribute?.('title') || node?.innerText || node?.textContent,
  )
  const base = {
    deleted: false, matchingTitle: false, matchingScheduledState: false,
    matchingScheduledMinute: false, rowCandidateCount: 0, deleteMenuCount: 0,
    confirmButtonCount: 0, finalClickAttempted: false,
  }
  if (!/\/videos(?:\/|$)/u.test(location.pathname)) {
    const entries = [...document.querySelectorAll(
      'a[href],button,[role="link"],[role="button"],[role="tab"],ytcp-navigation-drawer-item',
    )].filter(visible).filter((node) => {
      const label = exactText(node)
      return label === 'Content' || label === '内容'
    })
    const uniqueEntries = entries.filter((node) => !entries.some((other) => other !== node && node.contains(other)))
    if (uniqueEntries.length !== 1) return { ...base, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
    const entry = uniqueEntries[0].closest('a[href],button,[role="link"],[role="button"],[role="tab"]') || uniqueEntries[0]
    entry.click()
    await wait(1000)
  }
  const hasExactTitle = (row) => [...row.querySelectorAll('#video-title,[id="video-title"],a[title],h1,h2')]
    .filter(visible).some((node) => [node.innerText, node.getAttribute('title')]
      .some((value) => expectedTitles.has(normalize(value))))
  const titleRows = () => [...document.querySelectorAll('ytcp-video-row')]
    .filter((row) => visible(row) && hasExactTitle(row))
  const scheduledLabels = new Set(['Scheduled', '定时发布', '定时发布中', '定时发表', '待发布'])
  const hasScheduledState = (row) => [row, ...row.querySelectorAll('*')]
    .filter(visible).some((node) => scheduledLabels.has(exactText(node)))
  const hasScheduledMinute = (row) => {
    const text = normalize(row.innerText || row.textContent)
    const fullDate = /(?:^|[^\d])(\d{4})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(?:[T ]+)\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    const shortDate = /(?:^|[^\d])(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?\s*(\d{1,2})\s*[:：]\s*(\d{2})(?::\d{2})?/gu
    let match
    while ((match = fullDate.exec(text)) !== null) {
      const minute = String(match[1]).padStart(4, '0') + '-' + String(match[2]).padStart(2, '0') + '-'
        + String(match[3]).padStart(2, '0') + ' ' + String(match[4]).padStart(2, '0') + ':' + match[5]
      if (minute === expectedMinute) return true
    }
    while ((match = shortDate.exec(text)) !== null) {
      const minute = expectedMinute.slice(0, 4) + '-' + String(match[1]).padStart(2, '0') + '-'
        + String(match[2]).padStart(2, '0') + ' ' + String(match[3]).padStart(2, '0') + ':' + match[4]
      if (minute === expectedMinute) return true
    }
    return false
  }
  const rows = titleRows()
  if (rows.length === 0) return { ...base, errorCode: 'EGO_REMOTE_DELETE_NOT_FOUND' }
  if (rows.length !== 1) return {
    ...base, matchingTitle: true, rowCandidateCount: rows.length,
    errorCode: 'EGO_REMOTE_DELETE_AMBIGUOUS',
  }
  const row = rows[0]
  const matchingScheduledState = hasScheduledState(row)
  const matchingScheduledMinute = hasScheduledMinute(row)
  const evidence = {
    ...base, matchingTitle: true, matchingScheduledState, matchingScheduledMinute,
    rowCandidateCount: 1,
  }
  if (!matchingScheduledState || !matchingScheduledMinute) {
    return { ...evidence, errorCode: 'EGO_REMOTE_DELETE_EVIDENCE_MISMATCH' }
  }
  const menuSelectors = [
    "button[aria-label='Options']", "button[aria-label='More actions']",
    "button[aria-label='更多操作']", '#overflow-menu',
  ]
  const menuControls = [...new Set(menuSelectors.flatMap((selector) => [...row.querySelectorAll(selector)]))]
    .filter(visible)
  if (menuControls.length !== 1) {
    return { ...evidence, deleteMenuCount: menuControls.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  }
  menuControls[0].click()
  await wait(350)
  const deleteLabels = new Set(['Delete forever', '永久删除'])
  const deleteItems = [...document.querySelectorAll('[role="menuitem"],tp-yt-paper-item,ytcp-text-menu-item')]
    .filter((node) => visible(node) && deleteLabels.has(exactText(node)))
  if (deleteItems.length !== 1) {
    return { ...evidence, deleteMenuCount: deleteItems.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  }
  deleteItems[0].click()
  await wait(350)
  const dialogCandidates = [...document.querySelectorAll('ytcp-dialog,[role="dialog"],[aria-modal="true"]')]
    .filter(visible).filter((dialog) => {
      const text = normalize(dialog.innerText || dialog.textContent)
      return text.includes('Delete forever') || text.includes('永久删除')
    })
  const dialogs = dialogCandidates.filter((dialog) => !dialogCandidates.some(
    (other) => other !== dialog && dialog.contains(other),
  ))
  if (dialogs.length !== 1) {
    return { ...evidence, deleteMenuCount: 1, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  }
  const dialog = dialogs[0]
  const checkboxes = [...dialog.querySelectorAll('input[type="checkbox"],[role="checkbox"]')].filter(visible)
  if (checkboxes.length > 1) {
    return { ...evidence, deleteMenuCount: 1, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  }
  if (checkboxes.length === 1) {
    const checkbox = checkboxes[0]
    const checked = () => checkbox.checked === true || checkbox.getAttribute('aria-checked') === 'true'
    if (!checked()) checkbox.click()
    if (!checked()) return { ...evidence, deleteMenuCount: 1, errorCode: 'EGO_REMOTE_DELETE_EVIDENCE_MISMATCH' }
  }
  const confirmButtons = [...dialog.querySelectorAll('button,[role="button"],ytcp-button')]
    .filter((node) => visible(node) && deleteLabels.has(exactText(node)))
  if (confirmButtons.length !== 1) {
    return {
      ...evidence, deleteMenuCount: 1, confirmButtonCount: confirmButtons.length,
      errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
    }
  }
  confirmButtons[0].click()
  const clicked = {
    ...evidence, deleteMenuCount: 1, confirmButtonCount: 1, finalClickAttempted: true,
  }
  for (let attempt = 0; attempt < 12; attempt += 1) {
    await wait(500)
    const deletedToast = [...document.querySelectorAll('[role="alert"],ytcp-toast,ytcp-snackbar')]
      .filter(visible).some((node) => {
        const text = normalize(node.innerText || node.textContent)
        return text.includes('Content deleted') || text.includes('内容已删除')
      })
    if (titleRows().length === 0 || deletedToast) return { ...clicked, deleted: true, errorCode: '' }
  }
  return { ...clicked, errorCode: 'EGO_REMOTE_DELETE_RESULT_UNKNOWN' }
})()`
}

func wechatVideoScheduledDeleteScript(expectedTitle, scheduledLocalMinute string) string {
	titleJSON, _ := json.Marshal(strings.TrimSpace(expectedTitle))
	minuteJSON, _ := json.Marshal(strings.TrimSpace(scheduledLocalMinute))
	return `(async () => {
  const expectedTitle = ` + string(titleJSON) + `
  const scheduledLocalMinute = ` + string(minuteJSON) + `
  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const exactText = (node) => normalize(
    node?.getAttribute?.('aria-label') || node?.getAttribute?.('title') || node?.innerText || node?.textContent,
  )
  const exactTitle = (node) => normalize(node.innerText || node.textContent)
    .split(/\r?\n/u).map(normalize).includes(normalize(expectedTitle))
  const matchingMinute = (node) => {
    const text = normalize(node.innerText || node.textContent)
    const [date, time] = scheduledLocalMinute.split(' ')
    const [year, month, day] = date.split('-')
    return text.includes(scheduledLocalMinute)
      || text.includes(year + '-' + month + '-' + day + ' ' + time)
      || text.includes(month + '/' + day + ' ' + time)
      || text.includes(month + '-' + day + ' ' + time)
      || text.includes(month + '月' + day + '日 ' + time)
  }
  const scheduledLabels = new Set(['定时发表', '定时发布', '待发布', 'Scheduled'])
  const scheduled = (node) => [node, ...node.querySelectorAll('*')]
    .filter(visible).some((candidate) => scheduledLabels.has(exactText(candidate)))
  const rowsForTarget = () => {
    if (location.hostname !== 'channels.weixin.qq.com') return []
    const candidates = [...document.querySelectorAll(
      "article,li,[role='row'],[class*='list-item'],[class*='video-item']",
    )].filter((node) => visible(node) && exactTitle(node) && matchingMinute(node) && scheduled(node))
    return candidates.filter((row) => !candidates.some((other) => other !== row && row.contains(other)))
  }
  const rows = rowsForTarget()
  const evidence = {
    matchingTitle: rows.length > 0,
    matchingScheduledState: rows.length > 0,
    matchingScheduledMinute: rows.length > 0,
    rowCandidateCount: rows.length,
    deleteMenuCount: 0,
    confirmButtonCount: 0,
    finalClickAttempted: false,
  }
  if (rows.length === 0) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_NOT_FOUND' }
  if (rows.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_AMBIGUOUS' }
  const row = rows[0]
  const actionLabels = new Set(['更多', '更多操作', '操作', 'More', 'More actions', '…', '...'])
  const actions = [...row.querySelectorAll('button,[role="button"],[aria-haspopup="menu"]')]
    .filter(visible).filter((node) => actionLabels.has(exactText(node))
      || /more|menu|ellipsis|更多/iu.test([node.getAttribute('data-testid'), node.className].join(' ')))
  if (actions.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  actions[0].click()
  await wait(350)
  const deleteLabels = new Set(['删除视频', '删除作品', '删除', 'Delete'])
  const deleteItems = [...document.querySelectorAll(
    '[role="menuitem"],button,[role="button"],li,[class*="menu-item"],[class*="dropdown-item"]',
  )].filter(visible).filter((node) => deleteLabels.has(exactText(node)))
    .filter((node) => !node.closest('[role="dialog"],[role="alertdialog"],[aria-modal="true"]'))
  if (deleteItems.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: deleteItems.length,
    errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  deleteItems[0].click()
  await wait(350)
  const dialogCandidates = [...document.querySelectorAll(
    '[role="dialog"],[role="alertdialog"],[aria-modal="true"],[class*="modal"],[class*="dialog"]',
  )].filter(visible).filter((dialog) => /删除|Delete/iu.test(normalize(dialog.innerText || dialog.textContent)))
  const dialogs = dialogCandidates.filter((dialog) => !dialogCandidates.some(
    (other) => other !== dialog && dialog.contains(other),
  ))
  if (dialogs.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: 1,
    errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  const confirmLabels = new Set(['确认删除', '确定删除', '删除', 'Delete', 'Confirm delete'])
  const confirmButtons = [...dialogs[0].querySelectorAll('button,[role="button"]')]
    .filter(visible).filter((node) => confirmLabels.has(exactText(node)))
  if (confirmButtons.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: 1, confirmButtonCount: confirmButtons.length,
    errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  confirmButtons[0].click()
  const clicked = { ...evidence, deleteMenuCount: 1, confirmButtonCount: 1, finalClickAttempted: true }
  for (let attempt = 0; attempt < 12; attempt += 1) {
    await wait(500)
    const deletedToast = [...document.querySelectorAll(
      '[role="alert"],[role="status"],[class*="toast"],[class*="message"]',
    )].filter(visible).some((node) => /删除成功|已删除|deleted/iu.test(normalize(node.innerText || node.textContent)))
    if (rowsForTarget().length === 0 || deletedToast) return { ...clicked, deleted: true, errorCode: '' }
  }
  return { ...clicked, deleted: false, errorCode: 'EGO_REMOTE_DELETE_RESULT_UNKNOWN' }
})()`
}

func facebookScheduledDeleteScript(expectedMarker, scheduledLocalMinute, pageID, pageName string) string {
	markerJSON, _ := json.Marshal(strings.TrimSpace(expectedMarker))
	minuteJSON, _ := json.Marshal(strings.TrimSpace(scheduledLocalMinute))
	pageIDJSON, _ := json.Marshal(strings.TrimSpace(pageID))
	pageNameJSON, _ := json.Marshal(strings.TrimSpace(pageName))
	return `(async () => {
  const expectedMarker = ` + string(markerJSON) + `
  const scheduledLocalMinute = ` + string(minuteJSON) + `
  const expectedPageID = ` + string(pageIDJSON) + `
  const expectedPageName = ` + string(pageNameJSON) + `
  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const evidence = (overrides = {}) => ({
    matchingTitle: false, matchingScheduledState: false, matchingScheduledMinute: false,
    rowCandidateCount: 0, deleteMenuCount: 0, confirmButtonCount: 0,
    finalClickAttempted: false, deleted: false, ...overrides,
  })
  const params = new URLSearchParams(location.search)
  const pageCandidates = [...document.querySelectorAll(
    "[role='combobox'],[aria-selected='true'],[aria-current='true'],[data-page-id],[data-asset-id]",
  )].filter(visible).filter((node) => normalize(
    node.innerText || node.textContent || node.getAttribute('aria-label'),
  ) === normalize(expectedPageName))
  if (location.hostname !== 'business.facebook.com' || params.get('asset_id') !== expectedPageID || pageCandidates.length !== 1) {
    return evidence({ errorCode: 'EGO_REMOTE_DELETE_EVIDENCE_MISMATCH' })
  }
  const exactMarker = (node) => normalize(node.innerText || node.textContent)
    .split(/\r?\n/u).map(normalize).includes(normalize(expectedMarker))
  const matchingMinute = (node) => {
    const text = normalize(node.innerText || node.textContent)
    const [date, time] = scheduledLocalMinute.split(' ')
    const [year, month, day] = date.split('-')
    return text.includes(scheduledLocalMinute)
      || text.includes(year + '-' + month + '-' + day + ' ' + time)
      || text.includes(month + '/' + day + ' ' + time)
      || text.includes(month + '-' + day + ' ' + time)
      || text.includes(month + '月' + day + '日 ' + time)
  }
  const scheduled = (node) => [node, ...node.querySelectorAll('*')].filter(visible)
    .map((candidate) => normalize(candidate.innerText || candidate.getAttribute('aria-label') || candidate.textContent))
    .some((label) => ['Scheduled', '已排期', '定时发布', '待发布'].includes(label))
  const rowsForTarget = () => {
    const candidates = [...document.querySelectorAll(
      "article,li,[role='row'],[role='gridcell'],[data-testid*='planner'],[class*='planner'],[class*='scheduled']",
    )].filter((node) => visible(node) && exactMarker(node) && matchingMinute(node) && scheduled(node))
    return candidates.filter((row) => !candidates.some((other) => other !== row && row.contains(other)))
  }
  const rows = rowsForTarget()
  const matched = evidence({
    matchingTitle: rows.length > 0, matchingScheduledState: rows.length > 0,
    matchingScheduledMinute: rows.length > 0, rowCandidateCount: rows.length,
  })
  if (rows.length === 0) return { ...matched, errorCode: 'EGO_REMOTE_DELETE_NOT_FOUND' }
  if (rows.length !== 1) return { ...matched, errorCode: 'EGO_REMOTE_DELETE_AMBIGUOUS' }
  const row = rows[0]
  const actionLabels = new Set(['More', 'More actions', 'Actions', '更多', '更多操作', '操作', '…', '...'])
  const actions = [...row.querySelectorAll('button,[role="button"],[aria-haspopup="menu"]')]
    .filter(visible).filter((node) => actionLabels.has(normalize(
      node.getAttribute('aria-label') || node.getAttribute('title') || node.innerText || node.textContent,
    )))
  if (actions.length !== 1) return { ...matched, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  actions[0].click()
  await wait(350)
  const deleteLabels = new Set(['Delete post', 'Delete', '删除帖子', '删除'])
  const deleteItems = [...document.querySelectorAll('[role="menuitem"],button,[role="button"],li')]
    .filter(visible).filter((node) => deleteLabels.has(normalize(node.innerText || node.textContent)))
  if (deleteItems.length !== 1) return {
    ...matched, deleteMenuCount: deleteItems.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  deleteItems[0].click()
  await wait(350)
  const dialogs = [...document.querySelectorAll('[role="dialog"],[role="alertdialog"],[aria-modal="true"]')]
    .filter(visible).filter((dialog) => /Delete post|Delete|删除帖子|删除/u.test(normalize(dialog.innerText || dialog.textContent)))
    .filter((dialog, _, values) => !values.some((other) => other !== dialog && dialog.contains(other)))
  if (dialogs.length !== 1) return {
    ...matched, deleteMenuCount: 1, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  const confirmButtons = [...dialogs[0].querySelectorAll('button,[role="button"]')]
    .filter(visible).filter((node) => deleteLabels.has(normalize(node.innerText || node.textContent)))
  if (confirmButtons.length !== 1) return {
    ...matched, deleteMenuCount: 1, confirmButtonCount: confirmButtons.length,
    errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  confirmButtons[0].click()
  const clicked = { ...matched, deleteMenuCount: 1, confirmButtonCount: 1, finalClickAttempted: true }
  for (let attempt = 0; attempt < 12; attempt += 1) {
    await wait(500)
    const deletedToast = [...document.querySelectorAll('[role="alert"],[role="status"],[class*="toast"],[class*="message"]')]
      .filter(visible).some((node) => /Post deleted|Deleted|帖子已删除|删除成功|已删除/iu.test(normalize(node.innerText || node.textContent)))
    if (rowsForTarget().length === 0 || deletedToast) return { ...clicked, deleted: true, errorCode: '' }
  }
  return { ...clicked, errorCode: 'EGO_REMOTE_DELETE_RESULT_UNKNOWN' }
})()`
}

func tiktokScheduledDeleteScript(expectedTitle, scheduledLocalMinute string) string {
	titleJSON, _ := json.Marshal(strings.TrimSpace(expectedTitle))
	minuteJSON, _ := json.Marshal(strings.TrimSpace(scheduledLocalMinute))
	return `(async () => {
  const expectedTitle = ` + string(titleJSON) + `
  const scheduledLocalMinute = ` + string(minuteJSON) + `
  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const exactTitle = (node) => normalize(node.innerText || node.textContent)
    .split(/\r?\n/u).map(normalize).includes(normalize(expectedTitle))
  const matchingMinute = (node) => {
    const text = normalize(node.innerText || node.textContent)
    const [date, time] = scheduledLocalMinute.split(' ')
    const [year, month, day] = date.split('-')
    return text.includes(scheduledLocalMinute)
      || text.includes(year + '-' + month + '-' + day + ' ' + time)
      || text.includes(month + '/' + day + ' ' + time)
      || text.includes(month + '-' + day + ' ' + time)
      || text.includes(month + '月' + day + '日 ' + time)
  }
  const scheduled = (node) => [node, ...node.querySelectorAll('*')].filter(visible)
    .map((candidate) => normalize(candidate.innerText || candidate.getAttribute('aria-label') || candidate.textContent))
    .some((label) => ['Scheduled', '定时发布', '待发布'].includes(label))
  const allRows = () => [...document.querySelectorAll(
    "article,tr,li,[role='row'],[data-e2e*='post'],[data-e2e*='content'],[class*='post-item'],[class*='content-item']",
  )].filter((node) => visible(node) && exactTitle(node) && matchingMinute(node) && scheduled(node))
  const leafRows = () => {
    const rows = allRows()
    return rows.filter((row) => !rows.some((other) => other !== row && row.contains(other)))
  }
  const rows = leafRows()
  const evidence = {
    matchingTitle: rows.length > 0,
    matchingScheduledState: rows.length > 0,
    matchingScheduledMinute: rows.length > 0,
    rowCandidateCount: rows.length,
    deleteMenuCount: 0,
    confirmButtonCount: 0,
    finalClickAttempted: false,
  }
  if (rows.length === 0) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_NOT_FOUND' }
  if (rows.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_AMBIGUOUS' }
  const row = rows[0]
  const actions = [...row.querySelectorAll('button,[role="button"]')].filter(visible).filter((node) => {
    const label = normalize(node.getAttribute('aria-label') || node.getAttribute('title') || node.innerText || node.textContent)
    return ['More', 'More actions', '更多', '更多操作', '…', '...'].includes(label)
      || /more|menu|ellipsis|更多/iu.test([node.getAttribute('data-e2e'), node.className].join(' '))
  })
  if (actions.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  actions[0].click()
  await wait(300)
  const deleteItems = [...document.querySelectorAll('[role="menuitem"],button,[role="button"],li')]
    .filter(visible).filter((node) => ['Delete', '删除'].includes(normalize(node.innerText || node.textContent)))
  if (deleteItems.length !== 1) return { ...evidence, deleted: false, deleteMenuCount: deleteItems.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  deleteItems[0].click()
  await wait(300)
  const confirmButtons = [...document.querySelectorAll('[role="dialog"] button,[role="alertdialog"] button,button')]
    .filter(visible).filter((node) => ['Delete', '删除', 'Confirm', '确认'].includes(normalize(node.innerText || node.textContent)))
  if (confirmButtons.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: 1,
    confirmButtonCount: confirmButtons.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  confirmButtons[0].click()
  const clicked = {
    ...evidence, deleteMenuCount: 1, confirmButtonCount: 1, finalClickAttempted: true,
  }
  for (let attempt = 0; attempt < 12; attempt += 1) {
    await wait(500)
    const deletedToast = [...document.querySelectorAll('[role="alert"],[role="status"],[class*="toast"],[class*="Toast"]')]
      .filter(visible).some((node) => /deleted|删除成功|已删除/iu.test(normalize(node.innerText || node.textContent)))
    if (leafRows().length === 0 || deletedToast) return { ...clicked, deleted: true, errorCode: '' }
  }
  return { ...clicked, deleted: false, errorCode: 'EGO_REMOTE_DELETE_RESULT_UNKNOWN' }
})()`
}

func kuaishouScheduledDeleteScript(expectedTitle, scheduledLocalMinute string) string {
	titleJSON, _ := json.Marshal(strings.TrimSpace(expectedTitle))
	minuteJSON, _ := json.Marshal(strings.TrimSpace(scheduledLocalMinute))
	return `(async () => {
  const expectedTitle = ` + string(titleJSON) + `
  const scheduledLocalMinute = ` + string(minuteJSON) + `
  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const exactText = (node) => normalize(
    node?.getAttribute?.('aria-label') || node?.getAttribute?.('title') || node?.innerText || node?.textContent,
  )
  const exactTitle = (node) => normalize(node.innerText || node.textContent)
    .split(/\r?\n/u).map(normalize).includes(normalize(expectedTitle))
  const matchingMinute = (node) => {
    const text = normalize(node.innerText || node.textContent)
    const [date, time] = scheduledLocalMinute.split(' ')
    const [year, month, day] = date.split('-')
    return text.includes(scheduledLocalMinute)
      || text.includes(year + '-' + month + '-' + day + ' ' + time)
      || text.includes(month + '/' + day + ' ' + time)
      || text.includes(month + '-' + day + ' ' + time)
      || text.includes(month + '月' + day + '日 ' + time)
  }
  const scheduledLabels = new Set(['定时发布', '定时发布中', '待发布', 'Scheduled'])
  const scheduled = (node) => [node, ...node.querySelectorAll('*')]
    .filter(visible).some((candidate) => scheduledLabels.has(exactText(candidate)))
  const rowsForTarget = () => {
    if (location.hostname !== 'cp.kuaishou.com') return []
    const candidates = [...document.querySelectorAll(
      "article,li,[role='row'],[class*='work-item'],[class*='photo-item'],[class*='list-item']",
    )].filter((node) => visible(node) && exactTitle(node) && matchingMinute(node) && scheduled(node))
    return candidates.filter((row) => !candidates.some((other) => other !== row && row.contains(other)))
  }
  const rows = rowsForTarget()
  const evidence = {
    matchingTitle: rows.length > 0, matchingScheduledState: rows.length > 0,
    matchingScheduledMinute: rows.length > 0, rowCandidateCount: rows.length,
    deleteMenuCount: 0, confirmButtonCount: 0, finalClickAttempted: false,
  }
  if (rows.length === 0) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_NOT_FOUND' }
  if (rows.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_AMBIGUOUS' }
  const row = rows[0]
  const actionLabels = new Set(['更多', '更多操作', '操作', 'More', 'More actions', '…', '...'])
  const actions = [...row.querySelectorAll('button,[role="button"],[aria-haspopup="menu"]')]
    .filter(visible).filter((node) => actionLabels.has(exactText(node))
      || /more|menu|ellipsis|更多/iu.test([node.getAttribute('data-testid'), node.className].join(' ')))
  if (actions.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  actions[0].click()
  await wait(350)
  const deleteLabels = new Set(['删除作品', '删除视频', '删除', 'Delete'])
  const deleteItems = [...document.querySelectorAll(
    '[role="menuitem"],button,[role="button"],li,[class*="menu-item"],[class*="dropdown-item"]',
  )].filter(visible).filter((node) => deleteLabels.has(exactText(node)))
    .filter((node) => !node.closest('[role="dialog"],[role="alertdialog"],[aria-modal="true"]'))
  if (deleteItems.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: deleteItems.length,
    errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  deleteItems[0].click()
  await wait(350)
  const dialogCandidates = [...document.querySelectorAll(
    '[role="dialog"],[role="alertdialog"],[aria-modal="true"],[class*="modal"],[class*="dialog"]',
  )].filter(visible).filter((dialog) => /删除|Delete/iu.test(normalize(dialog.innerText || dialog.textContent)))
  const dialogs = dialogCandidates.filter((dialog) => !dialogCandidates.some(
    (other) => other !== dialog && dialog.contains(other),
  ))
  if (dialogs.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: 1,
    errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  const confirmLabels = new Set(['确认删除', '确定删除', '删除', 'Delete', 'Confirm delete'])
  const confirmButtons = [...dialogs[0].querySelectorAll('button,[role="button"]')]
    .filter(visible).filter((node) => confirmLabels.has(exactText(node)))
  if (confirmButtons.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: 1, confirmButtonCount: confirmButtons.length,
    errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  confirmButtons[0].click()
  const clicked = { ...evidence, deleteMenuCount: 1, confirmButtonCount: 1, finalClickAttempted: true }
  for (let attempt = 0; attempt < 12; attempt += 1) {
    await wait(500)
    const deletedToast = [...document.querySelectorAll(
      '[role="alert"],[role="status"],[class*="toast"],[class*="message"]',
    )].filter(visible).some((node) => /删除成功|已删除|deleted/iu.test(normalize(node.innerText || node.textContent)))
    if (rowsForTarget().length === 0 || deletedToast) return { ...clicked, deleted: true, errorCode: '' }
  }
  return { ...clicked, deleted: false, errorCode: 'EGO_REMOTE_DELETE_RESULT_UNKNOWN' }
})()`
}

func kuaishouPublishedDeleteScript(expectedTitle, expectedPostURL, externalPostID string) string {
	titleJSON, _ := json.Marshal(strings.TrimSpace(expectedTitle))
	postURLJSON, _ := json.Marshal(strings.TrimSpace(expectedPostURL))
	externalIDJSON, _ := json.Marshal(strings.TrimSpace(externalPostID))
	return `(async () => {
  const expectedTitle = ` + string(titleJSON) + `
  const expectedPostURL = ` + string(postURLJSON) + `
  const externalPostID = ` + string(externalIDJSON) + `
  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
  const normalize = (value) => String(value || '').normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/gu, '').replace(/\s+/gu, ' ').trim()
  const visible = (node) => {
    if (!node) return false
    const style = getComputedStyle(node)
    const rect = node.getBoundingClientRect()
    return style.display !== 'none' && style.visibility !== 'hidden'
      && Number(style.opacity || 1) > 0 && rect.width > 0 && rect.height > 0
  }
  const canonicalLink = (raw) => {
    try {
      const parsed = new URL(raw, location.href)
      const match = parsed.pathname.match(/^\/short-video\/([A-Za-z0-9_-]{6,64})\/?$/u)
      if (parsed.protocol !== 'https:' || parsed.hostname !== 'www.kuaishou.com' || parsed.search || parsed.hash || !match) return ''
      return 'https://www.kuaishou.com/short-video/' + match[1]
    } catch { return '' }
  }
  const exactMarker = (node) => normalize(node.innerText || node.textContent)
    .split(/\r?\n/u).map(normalize).includes(normalize(expectedTitle))
  const exactIdentity = (node) => [...node.querySelectorAll('a[href]')]
    .map((anchor) => canonicalLink(anchor.getAttribute('href')))
    .filter(Boolean)
    .filter((value, index, values) => values.indexOf(value) === index)
  const rowsForTarget = () => {
    const candidates = [...document.querySelectorAll(
      "article,li,[role='row'],[class*='work-item'],[class*='photo-item'],[class*='list-item']",
    )].filter((node) => visible(node) && exactMarker(node))
      .filter((node) => {
        const links = exactIdentity(node)
        return links.length === 1 && links[0] === expectedPostURL && links[0].endsWith('/' + externalPostID)
      })
    return candidates.filter((row) => !candidates.some((other) => other !== row && row.contains(other)))
  }
  const rows = rowsForTarget()
  const evidence = {
    matchingTitle: rows.length > 0,
    matchingScheduledState: false,
    matchingScheduledMinute: false,
    rowCandidateCount: rows.length,
    deleteMenuCount: 0,
    confirmButtonCount: 0,
    finalClickAttempted: false,
  }
  if (rows.length === 0) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_NOT_FOUND' }
  if (rows.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_AMBIGUOUS' }
  const row = rows[0]
  const actions = [...row.querySelectorAll('button,[role="button"],a,[tabindex="0"]')]
    .filter(visible).filter((node) => {
      const label = normalize(node.getAttribute('aria-label') || node.getAttribute('title') || node.innerText || node.textContent)
      return ['更多', '更多操作', '…', '...', 'More', 'More actions', '删除'].includes(label)
        || /more|menu|ellipsis|更多/iu.test([node.getAttribute('data-testid'), node.className].join(' '))
    })
  if (actions.length !== 1) return { ...evidence, deleted: false, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  actions[0].click()
  await wait(300)
  const deleteItems = [...document.querySelectorAll('[role="menuitem"],button,[role="button"],li')]
    .filter(visible).filter((node) => ['删除', 'Delete'].includes(normalize(node.innerText || node.textContent)))
  if (deleteItems.length !== 1) return { ...evidence, deleted: false, deleteMenuCount: deleteItems.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS' }
  deleteItems[0].click()
  await wait(300)
  const confirmButtons = [...document.querySelectorAll('[role="dialog"] button,[role="alertdialog"] button,button')]
    .filter(visible).filter((node) => ['确认删除', '删除', '确定', 'Delete', 'Confirm'].includes(normalize(node.innerText || node.textContent)))
  if (confirmButtons.length !== 1) return {
    ...evidence, deleted: false, deleteMenuCount: 1,
    confirmButtonCount: confirmButtons.length, errorCode: 'EGO_REMOTE_DELETE_ACTION_AMBIGUOUS',
  }
  confirmButtons[0].click()
  const clicked = { ...evidence, deleteMenuCount: 1, confirmButtonCount: 1, finalClickAttempted: true }
  for (let attempt = 0; attempt < 12; attempt += 1) {
    await wait(500)
    const deletedToast = [...document.querySelectorAll('[role="alert"],[role="status"],[class*="toast"],[class*="message"]')]
      .filter(visible).some((node) => /删除成功|已删除|deleted/iu.test(normalize(node.innerText || node.textContent)))
    if (rowsForTarget().length === 0 || deletedToast) return { ...clicked, deleted: true, errorCode: '' }
  }
  return { ...clicked, deleted: false, errorCode: 'EGO_REMOTE_DELETE_RESULT_UNKNOWN' }
})()`
}

func validScheduledManagerStatus(platformCode, status string) bool {
	switch strings.TrimSpace(status) {
	case "定时发布", "定时发布中", "待发布", "Scheduled":
		return platformCode == "douyin" || platformCode == "youtube" || platformCode == "tiktok" || platformCode == "kuaishou" || platformCode == "wechat_video" || platformCode == "wechat_mp" || platformCode == "facebook"
	case "已排期":
		return platformCode == "facebook"
	case "定时发表":
		return platformCode == "youtube" || platformCode == "wechat_video" || platformCode == "wechat_mp"
	default:
		return false
	}
}

func validateScheduledManagerRuntimeResult(platformCode, requestedPublishAt string, result publishManagerRuntimeResult) error {
	expectedLocalMinute, err := scheduledManagerLocalMinute(platformCode, requestedPublishAt)
	if err != nil {
		return fmt.Errorf(egopublish.ScheduleReadbackMismatch)
	}
	effectiveAt, err := time.Parse(time.RFC3339, strings.TrimSpace(result.EffectiveScheduledAt))
	if err != nil || strings.TrimSpace(result.EffectiveScheduledAt) == "" {
		return fmt.Errorf(egopublish.ScheduleReadbackMismatch)
	}
	china := time.FixedZone("Asia/Shanghai", 8*60*60)
	if effectiveAt.In(china).Format("2006-01-02 15:04") != expectedLocalMinute {
		return fmt.Errorf(egopublish.ScheduleReadbackMismatch)
	}
	return nil
}

func validatePublishedManagerRuntimeResult(platformCode string, result publishManagerRuntimeResult) error {
	if platformCode == "wechat_mp" {
		parsed, err := url.Parse(strings.TrimSpace(result.PostURL))
		if err != nil || parsed.Scheme != "https" || strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")) != "mp.weixin.qq.com" || parsed.User != nil || parsed.Fragment != "" {
			return fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		externalPostID := ""
		switch {
		case len(parts) == 2 && parts[0] == "s":
			externalPostID = parts[1]
		case parsed.Path == "/s":
			externalPostID = strings.TrimSpace(parsed.Query().Get("sn"))
		}
		if externalPostID == "" || externalPostID != strings.TrimSpace(result.ExternalPostID) || !isWechatMPExternalPostID(externalPostID) {
			return fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		return nil
	}
	if platformCode == "kuaishou" {
		parsed, err := url.Parse(result.PostURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "www.kuaishou.com" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) != 2 || parts[0] != "short-video" || parts[1] != result.ExternalPostID || !isKuaishouExternalPostID(result.ExternalPostID) {
			return fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		return nil
	}
	if platformCode == "tiktok" {
		parsed, err := url.Parse(result.PostURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "www.tiktok.com" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) != 3 || !strings.HasPrefix(parts[0], "@") || parts[1] != "video" ||
			!isTikTokHandle(strings.TrimPrefix(parts[0], "@")) || parts[2] != result.ExternalPostID ||
			!isTikTokExternalPostID(result.ExternalPostID) {
			return fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		return nil
	}
	if platformCode == "youtube" {
		if !isYouTubeExternalPostID(result.ExternalPostID) || result.PostURL != "https://www.youtube.com/shorts/"+result.ExternalPostID {
			return fmt.Errorf("EGO_PUBLISH_INSPECTION_RESULT_INVALID")
		}
		return nil
	}
	return nil
}

func isWechatMPExternalPostID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func isKuaishouExternalPostID(value string) bool {
	if len(value) < 6 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validateKuaishouPublishedIdentity(postURL, externalPostID string) error {
	postURL = strings.TrimSpace(postURL)
	externalPostID = strings.TrimSpace(externalPostID)
	if !isKuaishouExternalPostID(externalPostID) || postURL != "https://www.kuaishou.com/short-video/"+externalPostID {
		return fmt.Errorf("invalid Kuaishou published identity")
	}
	return nil
}

func isTikTokExternalPostID(value string) bool {
	if len(value) < 10 || len(value) > 32 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func isTikTokHandle(value string) bool {
	if len(value) < 2 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') &&
			(char < '0' || char > '9') && char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func isYouTubeExternalPostID(value string) bool {
	if len(value) < 6 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func (service *SpaceService) persistPublishInspectionScreenshot(accountID int64, encoded string) (string, error) {
	if service == nil || service.db == nil || accountID <= 0 || strings.TrimSpace(encoded) == "" {
		return "", fmt.Errorf("EGO_PUBLISH_EVIDENCE_INVALID")
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(payload) < 8 || len(payload) > 768*1024 || !bytes.Equal(payload[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return "", fmt.Errorf("EGO_PUBLISH_EVIDENCE_INVALID")
	}
	directory := filepath.Join(service.db.DataDir, "screenshots", "publish-inspections")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("EGO_PUBLISH_EVIDENCE_WRITE_FAILED")
	}
	path := filepath.Join(directory, fmt.Sprintf("account-%d-%d.png", accountID, time.Now().UTC().UnixNano()))
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", fmt.Errorf("EGO_PUBLISH_EVIDENCE_WRITE_FAILED")
	}
	return path, nil
}

func appendSpaceSafeTabReplacement(program *strings.Builder, urls []string) {
	if len(urls) == 0 {
		return
	}
	program.WriteString(`const staleTabs = await listTabs()
while (staleTabs.length > 1) {
  const stale = staleTabs.shift()
  await closeTab(stale.targetId ?? stale.id)
}
`)
	firstURL, _ := json.Marshal(urls[0])
	fmt.Fprintf(program, `const anchor = staleTabs.shift()
const anchorId = String(anchor?.targetId ?? anchor?.id ?? "")
await openOrReuseTab(%s, { wait: true, timeout: 30 })
await new Promise((resolve) => setTimeout(resolve, 800))
const afterFirstTabs = await listTabs()
if (anchorId && afterFirstTabs.length > 1 && afterFirstTabs.some((tab) => String(tab.targetId ?? tab.id ?? "") === anchorId)) {
  await closeTab(anchorId)
}
`, firstURL)
	for _, targetURL := range urls[1:] {
		urlJSON, _ := json.Marshal(targetURL)
		fmt.Fprintf(program, "await openOrReuseTab(%s, { wait: true, timeout: 30 })\nawait new Promise((resolve) => setTimeout(resolve, 800))\n", urlJSON)
	}
}

func (service *SpaceService) VerifyLoginBatch(ctx context.Context, identityID int64, platformCodes []string) (*LoginVerification, error) {
	if service == nil || service.db == nil || service.runtime == nil || identityID <= 0 || len(platformCodes) == 0 {
		return nil, fmt.Errorf("EGO_LOGIN_VERIFY_INPUT_INVALID")
	}
	space, err := service.EnsureIdentitySpace(ctx, identityID)
	if err != nil {
		return nil, err
	}
	accounts, err := service.db.ListAccountsByIdentity(identityID)
	if err != nil {
		return nil, err
	}
	byPlatform := make(map[string]db.Account, len(accounts))
	for _, account := range accounts {
		byPlatform[account.PlatformCode] = account
	}
	type target struct {
		PlatformCode        string             `json:"platformCode"`
		URL                 string             `json:"url"`
		AccountIdentifier   string             `json:"accountIdentifier,omitempty"`
		Rule                *reviewedLoginRule `json:"rule,omitempty"`
		Suffixes            []string           `json:"suffixes"`
		StrictLoginEvidence bool               `json:"strictLoginEvidence"`
	}
	targets := make([]target, 0, 3)
	seen := make(map[string]struct{}, 3)
	for _, rawCode := range platformCodes {
		code := strings.TrimSpace(rawCode)
		if _, duplicate := seen[code]; duplicate {
			continue
		}
		account, ok := byPlatform[code]
		if !ok || !account.Enabled {
			return nil, fmt.Errorf("EGO_PLATFORM_NOT_CONFIGURED")
		}
		platform, err := platforms.Find(code)
		if err != nil {
			return nil, err
		}
		targetURL := accountManagerURL(account, platform)
		targets = append(targets, target{
			PlatformCode: code, URL: targetURL, AccountIdentifier: account.AccountIdentifier, Rule: platformReviewedLoginRule(code),
			Suffixes: platformOriginSuffixes(code), StrictLoginEvidence: platformRequiresReviewedLoginEvidence(code),
		})
		seen[code] = struct{}{}
		if len(targets) == 3 {
			break
		}
	}
	targetsJSON, _ := json.Marshal(targets)
	program := fmt.Sprintf(`await takeOverTaskSpace(%d)
const targets = %s
const profiles = []
const matchesReviewedDashboard = (rule, pageURL, bodyText) => {
  if (!rule?.host) return false
  try {
    const parsed = new URL(pageURL)
    const host = parsed.hostname.toLowerCase().replace(/\.$/, "")
    return host === rule.host &&
      (!rule.pathPrefixes?.length || rule.pathPrefixes.some((prefix) => parsed.pathname.startsWith(prefix))) &&
      (rule.requiredText || []).every((token) => bodyText.includes(token))
  } catch { return false }
}
for (const target of targets) {
  const targetHost = new URL(target.url).hostname.toLowerCase().replace(/\.$/, "")
  const matchingTabs = (await listTabs()).filter((tab) => {
    try {
      const host = new URL(String(tab.url || "")).hostname.toLowerCase().replace(/\.$/, "")
      return host === targetHost || (target.suffixes || []).some((suffix) => host === suffix || host.endsWith("." + suffix))
    } catch { return false }
  })
  const matchingTab = [...matchingTabs].reverse().find((tab) => {
    try { return new URL(String(tab.url || "")).hostname.toLowerCase().replace(/\.$/, "") === targetHost } catch { return false }
  }) || matchingTabs[matchingTabs.length - 1]
  if (matchingTab) {
    await switchTab(matchingTab.targetId || matchingTab.id)
  } else {
    await openOrReuseTab(target.url, { wait: true, timeout: 30 })
  }
  const page = await pageInfo()
  const dom = await js(String.raw`+"`"+`(() => {
    const text = (selector) => (document.querySelector(selector)?.textContent || "").trim()
    const attr = (selector, name) => document.querySelector(selector)?.getAttribute(name) || ""
    const isVisible = (element) => {
      if (!element || element.hidden || element.getAttribute('aria-hidden') === 'true') return false
      const style = getComputedStyle(element)
      return style.display !== 'none' && style.visibility !== 'hidden' && Number(style.opacity || 1) > 0 &&
        element.getClientRects().length > 0
    }
    const loginControls = [...document.querySelectorAll('button, a, input[type="submit"], [role="button"]')]
    const qrNodes = [...document.querySelectorAll('[class*="qr" i], [id*="qr" i], img[alt*="qr" i], img[alt*="二维码"]')]
    const name = text('[data-testid*="profile"]') || text('[class*="profile-name"]') ||
      text('[class*="user-name"]') || text('[class*="username"]') ||
      attr('meta[property="profile:username"]', 'content')
    const identifier = attr('[data-user-id]', 'data-user-id') ||
      attr('[data-account-id]', 'data-account-id') || attr('[data-uid]', 'data-uid')
	const images = [...document.querySelectorAll('img')]
	const visibleAvatar = (image) => {
	  const rect = image.getBoundingClientRect()
	  return rect.width >= 16 && rect.height >= 16 && rect.width <= 512 && rect.height <= 512
	}
	const knownAvatarSource = (image) => /aweme-avatar|i-avatar\.csdnimg\.cn|sns-avatar|xhscdn[^/]*(?:\/|%%2f).*avatar|\/uhead\/|yt3\.ggpht|tiktokcdn|scontent[^/]*\.fbcdn|wx\.qlogo\.cn/i.test(image.currentSrc || image.src || "")
	const avatarElement = images.find((image) => visibleAvatar(image) && (image.matches('img[class*="avatar"]') || image.matches('img[alt*="avatar" i]') || image.matches('img[alt*="头像"]') || image.closest('[class*="avatar"]'))) ||
	  images.find((image) => visibleAvatar(image) && knownAvatarSource(image)) || images.find(knownAvatarSource)
	const youtubeAvatarElement = document.querySelector('ytcp-navigation-drawer img.thumbnail, ytcp-navigation-drawer yt-img-shadow#avatar img, yt-img-shadow#avatar img')
	const wechatMPAvatarElement = document.querySelector('img.weui-desktop-account__img, .weui-desktop-account__img img, img[src*="wx.qlogo.cn"]')
	const avatar = avatarElement?.currentSrc || avatarElement?.src || ""
	const avatarRect = avatarElement?.getBoundingClientRect()
	const avatarClip = avatarRect && avatarRect.width >= 16 && avatarRect.height >= 16 && avatarRect.width <= 512 && avatarRect.height <= 512
	  ? { x: avatarRect.left + window.scrollX, y: avatarRect.top + window.scrollY, width: avatarRect.width, height: avatarRect.height }
	  : null
	const youtubeAvatarRect = youtubeAvatarElement?.getBoundingClientRect()
	const youtubeAvatarClip = youtubeAvatarRect && youtubeAvatarRect.width >= 16 && youtubeAvatarRect.height >= 16
	  ? { x: youtubeAvatarRect.left + window.scrollX, y: youtubeAvatarRect.top + window.scrollY, width: youtubeAvatarRect.width, height: youtubeAvatarRect.height }
	  : null
	const wechatMPAvatarRect = wechatMPAvatarElement?.getBoundingClientRect()
	const wechatMPAvatarClip = wechatMPAvatarRect && wechatMPAvatarRect.width >= 16 && wechatMPAvatarRect.height >= 16
	  ? { x: wechatMPAvatarRect.left + window.scrollX, y: wechatMPAvatarRect.top + window.scrollY, width: wechatMPAvatarRect.width, height: wechatMPAvatarRect.height }
	  : null
	const metaAssetCandidates = (() => {
	  const numeric = (value) => { const text = String(value || "").trim(); return /^\d+$/.test(text) && /[1-9]/.test(text) ? text : "" }
	  const nodesFor = (image) => { const nodes = [image]; let current = image.parentElement; for (let depth = 0; depth < 5 && current; depth++, current = current.parentElement) nodes.push(current); return nodes }
	  const contextFor = (image, platformCode) => nodesFor(image).some((node) => {
	    const text = [...node.attributes].filter((attribute) => !/^(href|src|srcset)$/i.test(attribute.name)).map((attribute) => attribute.name + "=" + attribute.value).join(" ") + " " + (node.innerText || node.textContent || "")
	    return platformCode === "facebook" ? /facebook|page/i.test(text) : /instagram|instagram account|\big\b/i.test(text)
	  })
	  const explicitIDFor = (image, platformCode) => {
	    const context = contextFor(image, platformCode)
	    const attributeKeys = platformCode === "facebook"
	      ? ["data_page_id", "page_id", "data_facebook_page_id", "data_asset_id", "asset_id"]
	      : ["data_instagram_account_id", "instagram_account_id", "data_instagram_id", "instagram_id", "data_ig_id", "ig_id", "data_asset_id", "asset_id"]
	    for (const node of nodesFor(image)) {
	      for (const attribute of node.attributes) {
	        const key = attribute.name.toLowerCase().replace(/-/g, "_")
	        if (!attributeKeys.includes(key) || (key.endsWith("asset_id") && !context)) continue
	        const value = numeric(attribute.value)
	        if (value) return value
	      }
	      for (const raw of [node.getAttribute("href"), node.getAttribute("data-href"), node.getAttribute("data-url")]) {
	        if (!raw) continue
	        try {
	          const parsed = new URL(raw, location.href)
	          const keys = platformCode === "facebook" ? ["page_id", "asset_id"] : ["instagram_account_id", "instagram_id", "ig_id", "asset_id"]
	          for (const key of keys) {
	            if (key === "asset_id" && !context) continue
	            const value = numeric(parsed.searchParams.get(key))
	            if (value) return value
	          }
	        } catch {}
	      }
	    }
	    return ""
	  }
	  const connectedFor = (image, platformCode) => {
	    for (const node of nodesFor(image)) {
	      for (const attribute of node.attributes) {
	        const key = attribute.name.toLowerCase().replace(/-/g, "_")
	        if (key !== "data_connected_platform_code" && key !== "connected_platform_code") continue
	        const value = String(attribute.value || "").trim().toLowerCase()
	        if ((value === "facebook" || value === "instagram") && value !== platformCode) return value
	      }
	    }
	    return ""
	  }
	  return images.map((image, index) => {
	    if (!visibleAvatar(image) || !/scontent[^/]*\.(?:fbcdn|cdninstagram)\.net/i.test(image.currentSrc || image.src || "")) return null
	    const nextBrand = images.slice(index + 1, index + 3).map((candidate) => candidate.alt || "").find((alt) => alt === "Facebook" || alt === "Instagram") || ""
		const platformCode = nextBrand === "Facebook" ? "facebook" : nextBrand === "Instagram" ? "instagram" : ""
		if (!platformCode) return null
		const externalID = explicitIDFor(image, platformCode)
		const connectedPlatformCode = connectedFor(image, platformCode)
		image.setAttribute('data-alpha-meta-avatar', platformCode)
		const rect = image.getBoundingClientRect()
		return { platformCode, name: "", avatar: image.currentSrc || image.src || "", externalID, connectedPlatformCode, avatarClip: { x: rect.left + window.scrollX, y: rect.top + window.scrollY, width: rect.width, height: rect.height } }
	  }).filter(Boolean).filter((candidate, index, all) => all.findIndex((item) => item.platformCode === candidate.platformCode) === index)
})()
    return {
	  name, identifier, avatar, wechatMPName: text('.acount_box-nickname') || text('.weui-desktop_name'),
	  csdnProfileHref: attr('[class*="user"] a[href*="blog.csdn.net/"], [class*="profile"] a[href*="blog.csdn.net/"]', 'href'),
	  douyinName: text('[class*="account-trigger"] [class*="name"]'),
	  avatarClip, youtubeAvatar: youtubeAvatarElement?.currentSrc || youtubeAvatarElement?.src || "", youtubeAvatarClip,
	  wechatMPAvatar: wechatMPAvatarElement?.currentSrc || wechatMPAvatarElement?.src || "", wechatMPAvatarClip, metaAssetCandidates,
      bodyText: (document.body?.innerText || "").slice(0, 12000),
      hasVisiblePassword: [...document.querySelectorAll('input[type="password"]')].some(isVisible),
      hasVisibleLoginControl: loginControls.some((element) => isVisible(element) &&
        /登录|sign in|log in|继续|下一步/i.test((element.innerText || element.value || element.getAttribute('aria-label') || '').trim())),
      hasVisibleQR: qrNodes.some(isVisible)
    }
  })()`+"`"+`)
  const snapshot = dom.bodyText
  const pageURL = typeof page?.url === "string" ? page.url : target.url
  const reviewedDashboard = matchesReviewedDashboard(target.rule, pageURL, dom.bodyText)
  if (target.platformCode === "douyin") {
	  dom.name = dom.douyinName ||
	    dom.bodyText.match(/创作服务\s*\n([^\n]{1,80})\s*\n抖音号/)?.[1]?.trim() || dom.name
    dom.identifier = dom.identifier || dom.bodyText.match(/抖音号[：:]\s*([^\s]+)/)?.[1] || ""
	  } else if (target.platformCode === "csdn") {
	    // The CSDN creator sidebar's menu titles are not account identities.
	    // Only use the title from a validated public profile URL.
	    dom.name = ""
	    try {
      const storedProfilePath = /^[A-Za-z0-9_-]{2,80}$/.test(target.accountIdentifier || "") ? target.accountIdentifier : ""
      const profileURL = new URL(dom.csdnProfileHref || (storedProfilePath ? "https://blog.csdn.net/" + encodeURIComponent(storedProfilePath) : ""), pageURL)
      const profilePath = profileURL.pathname.split("/").filter(Boolean)[0] || ""
      if (profileURL.protocol !== "https:" || profileURL.hostname !== "blog.csdn.net" || !/^[A-Za-z0-9_-]{2,80}$/.test(profilePath)) {
        throw new Error("invalid_csdn_profile_url")
      }
      dom.identifier = profilePath
      try {
        const profileHTML = String(await serverFetch(profileURL.href))
        const publicTitle = (profileHTML.match(/<title[^>]*>([^<]{1,240})<\/title>/i)?.[1] || "")
          .replace(/&amp;/g, "&").replace(/&quot;/g, '"').replace(/&#39;|&apos;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">")
          .replace(/-CSDN博客$/, "").trim()
	        if (publicTitle) dom.name = publicTitle
	      } catch {}
	    } catch {}
	    dom.name = dom.name || ""
  } else if (target.platformCode === "wechat_video") {
	const videoNameCandidate = dom.bodyText.match(/通知中心\s*\n([^\n]{1,80})/)?.[1]?.trim() || ""
	const videoName = videoNameCandidate && !/^\d+$/.test(videoNameCandidate) ? videoNameCandidate : ""
    dom.identifier = dom.identifier || dom.bodyText.match(/视频号ID:\s*\n?([^\s]+)/)?.[1] || target.accountIdentifier || ""
	dom.name = videoName || dom.identifier || ""
	} else if (target.platformCode === "wechat_mp") {
	  dom.name = dom.wechatMPName || dom.name
	  if (dom.wechatMPAvatarClip) {
	    dom.avatar = dom.wechatMPAvatar || dom.avatar
	    dom.avatarClip = dom.wechatMPAvatarClip
	  }
  } else if (target.platformCode === "kuaishou") {
    dom.name = dom.name || dom.bodyText.match(/^\s*\d+\s*\n([^\n]{1,80})\s*\n发布作品/m)?.[1]?.trim() || ""
  } else if (target.platformCode === "xhs") {
    dom.name = dom.name || dom.bodyText.match(/创作服务平台\s*\n([^\n]{1,80})\s*\n发布笔记/)?.[1]?.trim() || ""
    dom.identifier = dom.identifier || dom.bodyText.match(/小红书账号[：:]\s*([^\s]+)/)?.[1] || ""
  } else if (target.platformCode === "tiktok") {
    dom.identifier = dom.identifier || dom.bodyText.match(/返回 TikTok\s*\n([^\n]{1,80})/)?.[1]?.trim() || target.accountIdentifier || ""
	dom.name = dom.identifier || ""
  } else if (target.platformCode === "youtube") {
    dom.name = dom.name || dom.bodyText.match(/Your channel\s*\n([^\n]{1,120})/)?.[1]?.trim() || ""
    dom.identifier = dom.identifier || pageURL.match(/\/channel\/([^/?#]+)/)?.[1] || ""
	if (dom.youtubeAvatarClip) {
	  dom.avatar = dom.youtubeAvatar || dom.avatar
	  dom.avatarClip = dom.youtubeAvatarClip
	}
  } else if (target.platformCode === "meta") {
    dom.name = dom.name || dom.bodyText.match(/添加封面照片\s*\n([^\n]{1,120})\s*\n\d+\s*\n位粉丝/)?.[1]?.trim() || ""
    try { dom.identifier = dom.identifier || new URL(pageURL).searchParams.get("asset_id") || "" } catch {}
  }
  const challenge = /captcha|验证码|安全验证|扫码验证|security verification/i.test(snapshot)
  const loginURLHint = /\/login|signin|passport|\/auth(?:\/|$)/i.test(pageURL)
  const loginTextHint = /登录|sign in|log in|手机号|扫码|email|phone number/i.test(snapshot)
  const visibleCredentialSurface = dom.hasVisiblePassword || dom.hasVisibleQR || (loginURLHint && dom.hasVisibleLoginControl)
  const explicitLogin = visibleCredentialSurface && loginTextHint
  // A visible credential surface is definitive: platform login pages carry
  // 扫码验证/验证码 copy, so challenge text must never outrank them.
  const authState = explicitLogin ? "logged_out" :
    challenge ? "manual_takeover" :
    reviewedDashboard ? "logged_in" :
    (!target.strictLoginEvidence && (dom.name || dom.identifier)) ? "logged_in" : "unknown"
	let avatarBase64 = ""
	if (authState === "logged_in" && dom.avatarClip) {
	  try {
	    const captured = await cdp("Page.captureScreenshot", {
	      format: "png", captureBeyondViewport: true,
	      clip: { ...dom.avatarClip, scale: 1 }
	    })
	    avatarBase64 = String(captured?.data || "")
	  } catch {}
	}
	const metaAssets = []
	if (authState === "logged_in" && target.platformCode === "meta") {
	  for (const candidate of dom.metaAssetCandidates || []) {
	    try {
	      const isolated = await js("(() => { const source = document.querySelector('img[data-alpha-meta-avatar=\\\"" + candidate.platformCode + "\\\"]'); if (!source) return null; document.getElementById('alpha-meta-avatar-capture')?.remove(); const stage = document.createElement('div'); stage.id = 'alpha-meta-avatar-capture'; Object.assign(stage.style, { position: 'fixed', left: '0', top: '0', width: '128px', height: '128px', zIndex: '2147483647', overflow: 'hidden' }); const clone = source.cloneNode(true); Object.assign(clone.style, { width: '128px', height: '128px', maxWidth: 'none', maxHeight: 'none', objectFit: 'cover', border: '0', borderRadius: '0', margin: '0', padding: '0' }); stage.appendChild(clone); document.documentElement.appendChild(stage); return { x: 0, y: 0, width: 128, height: 128 }; })()")
	      if (!isolated) continue
	      const captured = await cdp("Page.captureScreenshot", {
	        format: "png", captureBeyondViewport: true,
	        clip: { ...isolated, scale: 1 }
	      })
	      metaAssets.push({ platformCode: candidate.platformCode, name: candidate.name || dom.name, avatar: candidate.avatar, externalID: candidate.externalID || "", connectedPlatformCode: candidate.connectedPlatformCode || "", avatarBase64: String(captured?.data || "") })
	    } catch {} finally { try { await js("document.getElementById('alpha-meta-avatar-capture')?.remove()") } catch {} }
	  }
	}
  profiles.push({
    platformCode: target.platformCode,
    authState,
    name: dom.name,
    identifier: dom.identifier,
    avatar: dom.avatar,
	avatarBase64,
	metaAssets,
    pageOrigin: pageURL
  })
}
const controlAction = profiles.some((profile) =>
  profile?.authState === "logged_out" || profile?.authState === "manual_takeover"
) ? "handoff" : "complete"
let controlAttempted = false
let controlDone = false
try {
  controlAttempted = true
  if (controlAction === "handoff") {
    const handoff = await handOffTaskSpace(%d)
    controlDone = handoff?.done === true
  } else {
    const completed = await completeTaskSpace(%d, { keep: true })
    controlDone = completed?.done === true
  }
} catch {
  controlDone = false
}
cliLog(%q + JSON.stringify({ profiles, controlAction, controlAttempted, controlDone }))
`, space.SpaceID, string(targetsJSON), space.SpaceID, space.SpaceID, resultPrefix)
	var runtimeResult struct {
		Profiles []struct {
			PlatformCode string                 `json:"platformCode"`
			AuthState    string                 `json:"authState"`
			Name         string                 `json:"name"`
			Identifier   string                 `json:"identifier"`
			Avatar       string                 `json:"avatar"`
			AvatarBase64 string                 `json:"avatarBase64"`
			MetaAssets   []metaAssetObservation `json:"metaAssets"`
			PageOrigin   string                 `json:"pageOrigin"`
		} `json:"profiles"`
		ControlAction    string `json:"controlAction"`
		ControlAttempted bool   `json:"controlAttempted"`
		ControlDone      bool   `json:"controlDone"`
	}
	if err := service.runtime.Run(ctx, program, &runtimeResult); err != nil {
		return nil, err
	}
	manualTakeover := false
	loggedOut := false
	for _, profile := range runtimeResult.Profiles {
		switch profile.AuthState {
		case db.AccountAuthLoggedOut:
			loggedOut = true
		case "manual_takeover":
			manualTakeover = true
		}
	}
	expectedControlAction := "complete"
	if manualTakeover || loggedOut {
		expectedControlAction = "handoff"
	}
	if !runtimeResult.ControlAttempted || !runtimeResult.ControlDone || runtimeResult.ControlAction != expectedControlAction {
		return nil, fmt.Errorf("EGO_LOGIN_CONTROL_FAILED")
	}
	verifiedAccounts := make([]db.Account, 0, len(runtimeResult.Profiles))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, profile := range runtimeResult.Profiles {
		account, ok := byPlatform[profile.PlatformCode]
		if !ok {
			continue
		}
		authState := db.AccountAuthUnknown
		reasonCode := db.LoginReasonProfileSelectorMissing
		evidenceCode := db.LoginEvidenceMissing
		switch profile.AuthState {
		case db.AccountAuthLoggedIn:
			authState = db.AccountAuthLoggedIn
			reasonCode = db.LoginReasonVerifiedProfileDOM
			evidenceCode = db.LoginEvidenceReviewedProfileDOM
		case db.AccountAuthLoggedOut:
			authState = db.AccountAuthLoggedOut
			reasonCode = db.LoginReasonLoginRequiredPage
			evidenceCode = db.LoginEvidenceExplicitLoginDOM
		case "manual_takeover":
			manualTakeover = true
			reasonCode = db.LoginReasonCheckpointRequired
			evidenceCode = db.LoginEvidenceCheckpointDOM
		}
		avatarSource := db.AvatarSourcePlatformFallback
		avatarRef := ""
		avatarSHA256 := ""
		avatarErrorCode := ""
		if authState == db.AccountAuthLoggedIn && profile.AvatarBase64 != "" {
			if storedRef, storedSHA, avatarErr := persistObservedAvatarPNG(service.db.DataDir, account.ID, profile.AvatarBase64); avatarErr == nil {
				avatarSource = db.AvatarSourceObserved
				avatarRef = storedRef
				avatarSHA256 = storedSHA
			} else {
				avatarErrorCode = "AVATAR_CAPTURE_INVALID"
			}
		}
		if _, err := service.db.RecordAccountLoginObservation(db.AccountLoginObservation{
			AccountID: account.ID, AuthState: authState, BrowserState: "ready",
			LoginMode: db.LoginModeBrowserProfileSession, AuthMethod: db.AuthMethodUnknown,
			ReasonCode: reasonCode, EvidenceCode: evidenceCode,
			PageOrigin: safePageOrigin(profile.PageOrigin), Name: profile.Name,
			AccountIdentifier: profile.Identifier, AvatarSource: avatarSource,
			AvatarRef: avatarRef, AvatarSHA256: avatarSHA256, AvatarErrorCode: avatarErrorCode,
			ObservedAt: now, TraceID: string(trace.New()),
		}); err != nil {
			return nil, err
		}
		if authState == db.AccountAuthLoggedIn {
			if managerAddr := safePlatformManagerAddress(profile.PlatformCode, profile.PageOrigin); managerAddr != "" {
				if err := service.db.UpdateAccountManagerAddr(account.ID, managerAddr); err != nil {
					return nil, err
				}
			}
		}
		if authState == db.AccountAuthLoggedIn && avatarSource != db.AvatarSourceObserved {
			if avatarRef := SafeAvatarURL(profile.Avatar); avatarRef != "" {
				if err := service.db.UpdateAccountAvatarFallback(account.ID, avatarRef, now); err != nil {
					return nil, err
				}
			}
		}
		if authState == db.AccountAuthLoggedIn && profile.PlatformCode == "meta" {
			if err := service.persistMetaAssetAvatars(account.ID, profile.Name, profile.MetaAssets); err != nil {
				return nil, err
			}
		}
		updated, err := service.db.GetAccount(account.ID)
		if err != nil {
			return nil, err
		}
		verifiedAccounts = append(verifiedAccounts, *updated)
	}
	state := "ready"
	ownership := "agent"
	errorCode := ""
	if manualTakeover {
		state = "manual_takeover"
		ownership = "agentDelegatedToUser"
		errorCode = "CHECKPOINT_REQUIRED"
	} else if loggedOut {
		state = "manual_takeover"
		ownership = "agentDelegatedToUser"
		errorCode = "LOGIN_REQUIRED"
	}
	if _, err := service.db.UpdateEgoSpaceStateForIdentity(identityID, ownership, state, errorCode); err != nil {
		return nil, err
	}
	return &LoginVerification{IdentityID: identityID, State: state, Accounts: verifiedAccounts}, nil
}

func isPureMetaExternalID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	hasNonZero := false
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
		if r != '0' {
			hasNonZero = true
		}
	}
	return hasNonZero
}

func (service *SpaceService) persistMetaAssetAvatars(accountID int64, fallbackName string, profiles []metaAssetObservation) error {
	existing, err := service.db.ListAccountAssetsByAccount(accountID)
	if err != nil {
		return err
	}
	byPlatform := make(map[string]db.AccountAsset, len(existing))
	for _, asset := range existing {
		if asset.AssetType != "browser_account" {
			if _, duplicate := byPlatform[asset.PlatformCode]; duplicate {
				return fmt.Errorf("EGO_META_ASSET_AMBIGUOUS")
			}
			byPlatform[asset.PlatformCode] = asset
		}
	}
	observed := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		if profile.PlatformCode != "facebook" && profile.PlatformCode != "instagram" {
			return fmt.Errorf("EGO_META_ASSET_PLATFORM_INVALID")
		}
		if _, duplicate := observed[profile.PlatformCode]; duplicate {
			return fmt.Errorf("EGO_META_ASSET_AMBIGUOUS")
		}
		observed[profile.PlatformCode] = struct{}{}
		profile.ExternalID = strings.TrimSpace(profile.ExternalID)
		if !isPureMetaExternalID(profile.ExternalID) {
			return fmt.Errorf("EGO_META_ASSET_ID_INVALID")
		}
		connected := strings.TrimSpace(profile.ConnectedPlatformCode)
		if connected != "" && (connected == profile.PlatformCode || (connected != "facebook" && connected != "instagram")) {
			return fmt.Errorf("EGO_META_ASSET_CONNECTION_INVALID")
		}
		if asset, ok := byPlatform[profile.PlatformCode]; ok && asset.ExternalID != "" && profile.ExternalID != "" && asset.ExternalID != profile.ExternalID {
			return fmt.Errorf("EGO_META_ASSET_ID_CONFLICT")
		}
	}
	if fb, ok := profileByMetaPlatform(profiles, "facebook"); ok {
		if ig, igOK := profileByMetaPlatform(profiles, "instagram"); igOK && ((fb.ConnectedPlatformCode != "" && fb.ConnectedPlatformCode != "instagram") || (ig.ConnectedPlatformCode != "" && ig.ConnectedPlatformCode != "facebook")) {
			return fmt.Errorf("EGO_META_ASSET_CONNECTION_INVALID")
		}
	}
	for _, profile := range profiles {
		assetType := ""
		switch profile.PlatformCode {
		case "facebook":
			assetType = "facebook_page"
		case "instagram":
			assetType = "instagram_account"
		default:
			continue
		}
		asset, ok := byPlatform[profile.PlatformCode]
		if !ok {
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				name = strings.TrimSpace(fallbackName)
			}
			created, createErr := service.db.CreateAccountAsset(accountID, profile.PlatformCode, assetType, name, strings.TrimSpace(profile.ExternalID), "", 0, "")
			if createErr != nil {
				return createErr
			}
			asset = *created
			byPlatform[profile.PlatformCode] = asset
		} else if asset.ExternalID == "" && strings.TrimSpace(profile.ExternalID) != "" {
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				name = asset.Name
			}
			updated, updateErr := service.db.ConfirmAccountAssetIdentity(asset.ID, accountID, name, strings.TrimSpace(profile.ExternalID))
			if updateErr != nil {
				return updateErr
			}
			asset = *updated
			byPlatform[profile.PlatformCode] = asset
		}
		if profile.AvatarBase64 == "" {
			continue
		}
		avatarRef, _, persistErr := persistObservedAssetAvatarPNG(service.db.DataDir, asset.ID, profile.AvatarBase64)
		if persistErr != nil {
			continue
		}
		if err := service.db.UpdateAccountAssetAvatar(asset.ID, avatarRef); err != nil {
			return err
		}
	}
	if len(profiles) == 2 {
		facebook, facebookOK := byPlatform["facebook"]
		instagram, instagramOK := byPlatform["instagram"]
		if facebookOK && instagramOK && isPureMetaExternalID(facebook.ExternalID) && facebook.ExternalID != "" && isPureMetaExternalID(instagram.ExternalID) && instagram.ExternalID != "" {
			if err := service.db.UpdateAccountAssetConnection(facebook.ID, instagram.ID, "instagram", "instagram_account", instagram.Name, instagram.ExternalID); err != nil {
				return err
			}
			if err := service.db.UpdateAccountAssetConnection(instagram.ID, facebook.ID, "facebook", "facebook_page", facebook.Name, facebook.ExternalID); err != nil {
				return err
			}
		}
	}
	return nil
}

func profileByMetaPlatform(profiles []metaAssetObservation, platformCode string) (metaAssetObservation, bool) {
	for _, profile := range profiles {
		if profile.PlatformCode == platformCode {
			return profile, true
		}
	}
	return metaAssetObservation{}, false
}

// SafeAvatarURL keeps only a non-secret public HTTPS reference suitable for
// Electron display. Query strings, fragments, credentials, and local targets
// are never persisted.
func SafeAvatarURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil &&
		(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if len(parsed.String()) > 2048 {
		return ""
	}
	return parsed.String()
}

func safePageOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func accountManagerURL(account db.Account, platform platforms.Platform) string {
	if stored := safePlatformManagerAddress(account.PlatformCode, account.ManagerAddr); stored != "" {
		return stored
	}
	if platform.ManagerURL != "" {
		return platform.ManagerURL
	}
	return platform.LoginURL
}

func safePlatformManagerAddress(platformCode, raw string) string {
	if platformCode != "meta" {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")) != "business.facebook.com" || parsed.User != nil {
		return ""
	}
	if parsed.Path != "/latest/home" && parsed.Path != "/latest/home/" {
		return ""
	}
	cleanID := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 32 {
			return ""
		}
		for _, character := range value {
			if character < '0' || character > '9' {
				return ""
			}
		}
		return value
	}
	query := url.Values{}
	if value := cleanID(parsed.Query().Get("asset_id")); value != "" {
		query.Set("asset_id", value)
	}
	if value := cleanID(parsed.Query().Get("business_id")); value != "" {
		query.Set("business_id", value)
	}
	if len(query) == 0 {
		return ""
	}
	parsed.User = nil
	parsed.Host = "business.facebook.com"
	parsed.Path = "/latest/home"
	parsed.RawPath = ""
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func matchesPlatformOrigin(platformCode, raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	for _, allowedHost := range platformOriginHosts(platformCode) {
		if host == allowedHost {
			return true
		}
	}
	for _, suffix := range platformOriginSuffixes(platformCode) {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func platformOriginHosts(platformCode string) []string {
	if platformCode == "alibaba_1688" {
		return []string{"login.taobao.com"}
	}
	return []string{}
}

type reviewedLoginRule struct {
	Host         string   `json:"host"`
	PathPrefixes []string `json:"pathPrefixes"`
	RequiredText []string `json:"requiredText"`
}

// platformReviewedLoginRule contains only login evidence observed on real
// creator dashboards. A missing rule deliberately falls back to conservative
// profile-DOM detection instead of treating a generic page as authenticated.
func platformReviewedLoginRule(platformCode string) *reviewedLoginRule {
	switch platformCode {
	case "douyin":
		return &reviewedLoginRule{
			Host: "creator.douyin.com", PathPrefixes: []string{"/creator-micro/"},
			RequiredText: []string{"作品发布", "内容管理", "数据中心"},
		}
	case "csdn":
		return &reviewedLoginRule{
			Host: "mp.csdn.net", PathPrefixes: []string{"/"},
			RequiredText: []string{"创作中心", "内容管理", "作品数据"},
		}
	case "wechat_video":
		return &reviewedLoginRule{
			Host: "channels.weixin.qq.com", PathPrefixes: []string{"/platform"},
			RequiredText: []string{"内容管理", "数据中心", "视频号ID:"},
		}
	case "wechat_mp":
		return &reviewedLoginRule{
			Host: "mp.weixin.qq.com", PathPrefixes: []string{"/cgi-bin/"},
			RequiredText: []string{"内容管理", "互动管理", "数据分析", "设置与开发"},
		}
	case "kuaishou":
		return &reviewedLoginRule{
			Host: "cp.kuaishou.com", PathPrefixes: []string{"/profile"},
			RequiredText: []string{"发布作品", "内容管理", "数据中心"},
		}
	case "xhs":
		return &reviewedLoginRule{
			Host: "creator.xiaohongshu.com", PathPrefixes: []string{"/new/"},
			RequiredText: []string{"笔记管理", "数据看板", "小红书账号:"},
		}
	case "xhs_shop":
		return &reviewedLoginRule{
			Host: "ark.xiaohongshu.com", PathPrefixes: []string{"/app-system/"},
			RequiredText: []string{"店铺管理", "商品管理", "订单管理"},
		}
	case "xianyu":
		return &reviewedLoginRule{
			Host: "www.goofish.com", PathPrefixes: []string{"/personal"},
			RequiredText: []string{"我卖出的", "我买到的"},
		}
	case "tiktok":
		return &reviewedLoginRule{
			Host: "www.tiktok.com", PathPrefixes: []string{"/tiktokstudio"},
			RequiredText: []string{"作品", "数据分析", "创作者学院"},
		}
	case "youtube":
		return &reviewedLoginRule{
			Host: "studio.youtube.com", PathPrefixes: []string{"/channel/"},
			RequiredText: []string{"Channel dashboard", "Content", "Analytics"},
		}
	case "meta":
		return &reviewedLoginRule{
			Host: "business.facebook.com", PathPrefixes: []string{"/latest/"},
			RequiredText: []string{"创建帖子", "帖子和 Reels", "前往 Instagram"},
		}
	default:
		return nil
	}
}

// E-commerce consoles are available for isolated login management before a
// creator-specific profile rule has been reviewed. Generic profile-looking DOM
// must not be promoted to a successful login for these platforms.
func platformRequiresReviewedLoginEvidence(platformCode string) bool {
	switch platformCode {
	case "douyin_shop", "taobao", "tiktok_shop", "shopee", "shopify", "amazon_seller",
		"qianchuan", "jd_shop", "pinduoduo_shop", "xhs_shop", "kuaishou_shop", "wechat_shop",
		"alibaba_1688", "aliexpress", "xianyu":
		return true
	default:
		return false
	}
}

func platformOriginSuffixes(platformCode string) []string {
	allowed := map[string][]string{
		"douyin":         {"douyin.com"},
		"wechat_video":   {"weixin.qq.com", "qq.com"},
		"kuaishou":       {"kuaishou.com"},
		"xhs":            {"xiaohongshu.com"},
		"tiktok":         {"tiktok.com"},
		"youtube":        {"youtube.com", "google.com"},
		"meta":           {"business.facebook.com", "facebook.com", "meta.com"},
		"instagram":      {"instagram.com", "facebook.com", "meta.com"},
		"facebook":       {"facebook.com", "meta.com"},
		"wechat_mp":      {"weixin.qq.com"},
		"csdn":           {"csdn.net"},
		"acfun":          {"acfun.cn"},
		"baijiahao":      {"baidu.com"},
		"bilibili":       {"bilibili.com"},
		"jianshu":        {"jianshu.com"},
		"juejin":         {"juejin.cn"},
		"omtencent":      {"qq.com"},
		"pdd":            {"pinduoduo.com"},
		"sina":           {"weibo.com"},
		"toutiao":        {"toutiao.com"},
		"weishi":         {"qq.com"},
		"x":              {"x.com"},
		"zhihu":          {"zhihu.com"},
		"douyin_shop":    {"jinritemai.com"},
		"taobao":         {"taobao.com"},
		"tiktok_shop":    {"tiktokglobalshop.com"},
		"shopee":         {"shopee.cn", "shopee.com"},
		"shopify":        {"shopify.com", "myshopify.com"},
		"amazon_seller":  {"amazon.com"},
		"qianchuan":      {"jinritemai.com"},
		"jd_shop":        {"shop.jd.com", "jd.com"},
		"pinduoduo_shop": {"pinduoduo.com"},
		"xhs_shop":       {"xiaohongshu.com"},
		"kuaishou_shop":  {"kwaixiaodian.com"},
		"wechat_shop":    {"weixin.qq.com", "qq.com"},
		"alibaba_1688":   {"1688.com"},
		"aliexpress":     {"aliexpress.com"},
		"xianyu":         {"goofish.com", "taobao.com"},
	}
	return allowed[platformCode]
}

// pinProgramRunnerDB lets a test double answer the binding pre-flight with the
// Spaces in the database it is driven with. Only test doubles opt in through
// BindProgramRunnerDB, so production runners never hold a DB handle.
func pinProgramRunnerDB(database *db.DB, runtime ProgramRunner) {
	if binder, ok := runtime.(interface{ BindProgramRunnerDB(*db.DB) }); ok {
		binder.BindProgramRunnerDB(database)
	}
}
