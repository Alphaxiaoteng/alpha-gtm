package publishplan

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"alpha-nexus-egolite/internal/platforms"
)

const (
	AlphaGamerAIDisclosure = "本条封面与动态视觉由 AI 生成，仅用于品牌视觉展示，不是真实游戏画面，也不包含商业合作。"
	alphaGamerAuthor       = "αgamer"

	ComplianceStatusApplied     = "applied"
	ComplianceStatusManual      = "manual"
	ComplianceStatusUnsupported = "unsupported"
)

// CompliancePolicy is frozen with a publish plan. A browser control is never
// considered applied merely because the requested value equals a platform
// default; the live worker must read and verify the page state independently.
type CompliancePolicy struct {
	AIGenerated       bool   `json:"ai_generated"`
	AIDisclosure      string `json:"ai_disclosure"`
	CommercialContent bool   `json:"commercial_content"`
	Visibility        string `json:"visibility"`
	Comments          bool   `json:"comments"`
	Download          bool   `json:"download"`
	Duet              bool   `json:"duet"`
	Stitch            bool   `json:"stitch"`
	YouTubeAudience   string `json:"youtube_audience"`
	Retention         string `json:"retention"`
}

// ComplianceField records the deterministic platform mapping without claiming
// an unobserved third-party page state. "applied" is reserved for invariants
// already proven by the frozen local snapshot.
type ComplianceField struct {
	PolicyKey   string `json:"policy_key"`
	PlatformKey string `json:"platform_key,omitempty"`
	Value       any    `json:"value"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

func AlphaGamerCompliancePolicy() CompliancePolicy {
	return CompliancePolicy{
		AIGenerated:       true,
		AIDisclosure:      AlphaGamerAIDisclosure,
		CommercialContent: false,
		Visibility:        "public",
		Comments:          true,
		Download:          false,
		Duet:              false,
		Stitch:            false,
		YouTubeAudience:   "not_made_for_kids",
		Retention:         "keep",
	}
}

func validateCompliancePolicy(policy CompliancePolicy) error {
	if policy.AIGenerated {
		if !reflect.DeepEqual(policy, AlphaGamerCompliancePolicy()) {
			return fmt.Errorf("AI-generated publish compliance policy must match the locked formal policy")
		}
		return nil
	}
	if strings.TrimSpace(policy.AIDisclosure) != "" {
		return fmt.Errorf("AI disclosure requires ai_generated=true")
	}
	if policy.Visibility != "public" {
		return fmt.Errorf("compliance visibility must be public")
	}
	if policy.YouTubeAudience != "" && policy.YouTubeAudience != "not_made_for_kids" {
		return fmt.Errorf("unsupported YouTube audience policy")
	}
	if policy.Retention != "keep" {
		return fmt.Errorf("compliance retention must be keep")
	}
	return nil
}

type complianceMapping struct {
	aiGenerated       string
	commercialContent string
	visibility        string
	comments          string
	download          string
	duet              string
	stitch            string
	youtubeAudience   string
}

func platformComplianceMapping(platformCode, mediaMode string) (complianceMapping, bool) {
	switch platformCode {
	case "instagram", "facebook":
		return complianceMapping{
			commercialContent: "commercial_content", visibility: "visibility",
			comments: "comment_enabled",
		}, true
	case "xhs":
		return complianceMapping{
			visibility: "visibility", comments: "comment_enabled",
			download: "allow_copy", duet: "allow_duet",
		}, true
	case "douyin":
		return complianceMapping{visibility: "visibility", comments: "comment_enabled", download: "allow_download"}, true
	case "kuaishou":
		return complianceMapping{visibility: "visibility", comments: "comment_enabled", download: "allow_download", duet: "allow_duet"}, true
	case "wechat_video":
		return complianceMapping{}, true
	case "wechat_mp":
		// The reviewed EgoLite article adapter currently supports the publish
		// path with comments disabled. Keep the broader policy intent visible as
		// manual evidence instead of freezing an unsupported live toggle.
		return complianceMapping{}, true
	case "csdn":
		return complianceMapping{visibility: "visibility"}, true
	case "youtube":
		return complianceMapping{visibility: "visibility", comments: "allow_comments", youtubeAudience: "audience"}, true
	case "tiktok":
		mapping := complianceMapping{visibility: "visibility", comments: "allow_comment", download: "allow_download"}
		if mediaMode == "video" {
			mapping.duet = "allow_duet"
			mapping.stitch = "allow_stitch"
		}
		return mapping, true
	default:
		return complianceMapping{}, false
	}
}

func browserComplianceField(policyKey, platformKey string, value any) ComplianceField {
	if platformKey == "" {
		return ComplianceField{
			PolicyKey: policyKey, Value: value, Status: ComplianceStatusUnsupported,
			Reason: "no reviewed creator-console control for this platform and content type",
		}
	}
	return ComplianceField{
		PolicyKey: policyKey, PlatformKey: platformKey, Value: value, Status: ComplianceStatusManual,
		Reason: "frozen intent; read, set, and verify the live creator-console state before submit",
	}
}

func supplementalComplianceFields(platformCode string) []ComplianceField {
	manual := func(key string, value any) ComplianceField {
		return browserComplianceField(key, key, value)
	}
	switch platformCode {
	case "xhs":
		return []ComplianceField{manual("declare_original", false)}
	case "kuaishou":
		return []ComplianceField{manual("same_city", false)}
	case "wechat_video":
		return []ComplianceField{manual("declare_original", false)}
	case "wechat_mp":
		return []ComplianceField{
			manual("author", alphaGamerAuthor),
			manual("declare_original", false),
			manual("allow_reprint", false),
			manual("publish_type", "publish"),
		}
	case "csdn":
		return []ComplianceField{
			manual("article_type", "original"),
			manual("backup_gitcode", false),
			{
				PolicyKey: "minimum_content_tags", Value: 3, Status: ComplianceStatusApplied,
				Reason: "at least three distinct non-empty tags are present in the frozen target",
			},
		}
	case "youtube":
		return []ComplianceField{
			manual("altered_content", false),
			manual("playlist", ""),
		}
	default:
		return nil
	}
}

func buildTargetCompliance(platformCode, mediaMode, frozenText string, policy CompliancePolicy) ([]ComplianceField, error) {
	if err := validateCompliancePolicy(policy); err != nil {
		return nil, err
	}
	mapping, ok := platformComplianceMapping(platformCode, mediaMode)
	if !ok {
		return nil, fmt.Errorf("platform has no reviewed compliance mapping")
	}
	if policy.AIGenerated && !strings.Contains(frozenText, policy.AIDisclosure) {
		return nil, fmt.Errorf("AI-generated content must include the exact AI disclosure in frozen text")
	}

	fields := []ComplianceField{
		browserComplianceField("ai_generated", mapping.aiGenerated, policy.AIGenerated),
		{
			PolicyKey: "ai_disclosure", Value: policy.AIDisclosure, Status: ComplianceStatusApplied,
			Reason: "exact disclosure is present in the frozen target text",
		},
		browserComplianceField("commercial_content", mapping.commercialContent, policy.CommercialContent),
		browserComplianceField("visibility", mapping.visibility, policy.Visibility),
		browserComplianceField("comments", mapping.comments, policy.Comments),
		browserComplianceField("download", mapping.download, policy.Download),
		browserComplianceField("duet", mapping.duet, policy.Duet),
		browserComplianceField("stitch", mapping.stitch, policy.Stitch),
		browserComplianceField("youtube_audience", mapping.youtubeAudience, policy.YouTubeAudience),
		{
			PolicyKey: "retention", Value: policy.Retention, Status: ComplianceStatusApplied,
			Reason: "local frozen plan is configured to keep the published post",
		},
	}
	fields = append(fields, supplementalComplianceFields(platformCode)...)
	return fields, nil
}

func validateComplianceTarget(target TargetSnapshot, policy CompliancePolicy) error {
	if !policy.AIGenerated || target.PlatformCode != "csdn" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(target.TagsJSON), &tags); err != nil {
		return fmt.Errorf("CSDN tags must be a string array")
	}
	unique := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		normalized := strings.ToLower(strings.Trim(strings.TrimSpace(tag), "#"))
		if normalized != "" {
			unique[normalized] = struct{}{}
		}
	}
	if len(unique) < 3 {
		return fmt.Errorf("CSDN formal AI content requires at least three content tags")
	}
	return nil
}

func complianceSettingsOverrides(platformCode string, policy CompliancePolicy) map[string]any {
	switch platformCode {
	case "xhs":
		return map[string]any{
			"content_source": "none", "declare_original": false,
			"visibility": policy.Visibility, "comment_enabled": policy.Comments,
			"allow_duet": policy.Duet, "allow_copy": policy.Download,
		}
	case "douyin":
		return map[string]any{
			"self_declaration": "none", "comment_enabled": policy.Comments,
			"allow_download": policy.Download, "visibility": policy.Visibility,
		}
	case "kuaishou":
		return map[string]any{
			"declaration": "none", "allow_duet": policy.Duet, "allow_download": policy.Download,
			"same_city": false, "visibility": policy.Visibility, "comment_enabled": policy.Comments,
		}
	case "wechat_video":
		return map[string]any{"declare_original": false}
	case "wechat_mp":
		return map[string]any{
			"author": alphaGamerAuthor, "original_link": "", "comment_enabled": false,
			"declare_original": false, "allow_reprint": false, "publish_type": "publish",
			"collection": "", "content_source": "none",
		}
	case "csdn":
		return map[string]any{
			"collection": "", "article_type": "original", "original_link": "",
			"backup_gitcode": false, "visibility": policy.Visibility, "activity": "", "topic": "",
		}
	case "youtube":
		return map[string]any{
			"audience": policy.YouTubeAudience, "visibility": policy.Visibility, "playlist": "",
			"allow_comments": policy.Comments, "altered_content": false,
		}
	case "tiktok":
		return map[string]any{
			"visibility": policy.Visibility, "allow_comment": policy.Comments,
			"allow_duet": policy.Duet, "allow_stitch": policy.Stitch, "ai_generated": false,
			"allow_download": policy.Download,
		}
	default:
		return nil
	}
}

func decodeSettingsObject(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(raw), &values); err != nil || values == nil {
		return nil, fmt.Errorf("settings must be a JSON object")
	}
	return values, nil
}

func applyComplianceSettings(platformCode, requestedJSON string, policy CompliancePolicy) (string, error) {
	if err := validateCompliancePolicy(policy); err != nil {
		return "", err
	}
	requested, err := decodeSettingsObject(requestedJSON)
	if err != nil {
		return "", err
	}
	for key, value := range complianceSettingsOverrides(platformCode, policy) {
		if requestedValue, exists := requested[key]; exists && !reflect.DeepEqual(requestedValue, value) {
			return "", fmt.Errorf("%s setting %q conflicts with frozen compliance policy", platformCode, key)
		}
		requested[key] = value
	}
	encoded, err := json.Marshal(requested)
	if err != nil {
		return "", fmt.Errorf("encode compliance settings: %w", err)
	}
	if _, ok := platforms.SettingsSchemaFor(platformCode); !ok {
		return string(encoded), nil
	}
	return platforms.NormalizeSettings(platformCode, string(encoded))
}
