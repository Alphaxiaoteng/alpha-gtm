package publishplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/db"
)

func TestAlphaGamerAuthorFitsWeChatEightCharacterLimit(t *testing.T) {
	if alphaGamerAuthor != "αgamer" || utf8.RuneCountInString(alphaGamerAuthor) > 8 {
		t.Fatalf("wechat author=%q runes=%d", alphaGamerAuthor, utf8.RuneCountInString(alphaGamerAuthor))
	}
}

func TestAlphaGamerCompliancePolicyIsTheLockedFormalPolicy(t *testing.T) {
	got := AlphaGamerCompliancePolicy()
	want := CompliancePolicy{
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("policy=%+v, want %+v", got, want)
	}
	if err := validateCompliancePolicy(got); err != nil {
		t.Fatal(err)
	}

	invalid := got
	invalid.Download = true
	if err := validateCompliancePolicy(invalid); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("invalid locked policy err=%v", err)
	}
}

func TestBuildTargetComplianceUsesDeterministicHonestPlatformMappings(t *testing.T) {
	policy := AlphaGamerCompliancePolicy()
	body := "intro\n" + AlphaGamerAIDisclosure + "\nend"
	tests := []struct {
		platform   string
		mediaMode  string
		wantKeys   map[string]string
		supplement map[string]any
	}{
		{"instagram", "graph_text", map[string]string{"ai_generated": "", "commercial_content": "commercial_content", "comments": "comment_enabled"}, nil},
		{"facebook", "video", map[string]string{"ai_generated": "", "commercial_content": "commercial_content", "comments": "comment_enabled"}, nil},
		{"xhs", "graph_text", map[string]string{"ai_generated": "", "comments": "comment_enabled", "download": "allow_copy", "duet": "allow_duet"}, map[string]any{"declare_original": false}},
		{"douyin", "video", map[string]string{"ai_generated": "", "comments": "comment_enabled", "download": "allow_download"}, nil},
		{"kuaishou", "video", map[string]string{"ai_generated": "", "comments": "comment_enabled", "download": "allow_download", "duet": "allow_duet"}, map[string]any{"same_city": false}},
		{"wechat_video", "video", map[string]string{"ai_generated": "", "comments": ""}, map[string]any{"declare_original": false}},
		{"wechat_mp", "article", map[string]string{"ai_generated": "", "comments": ""}, map[string]any{"author": alphaGamerAuthor, "declare_original": false, "allow_reprint": false, "publish_type": "publish"}},
		{"csdn", "article", map[string]string{"ai_generated": "", "visibility": "visibility"}, map[string]any{"article_type": "original", "backup_gitcode": false, "minimum_content_tags": 3}},
		{"youtube", "video", map[string]string{"youtube_audience": "audience", "comments": "allow_comments"}, map[string]any{"altered_content": false, "playlist": ""}},
		{"tiktok", "video", map[string]string{"ai_generated": "", "download": "allow_download", "stitch": "allow_stitch"}, nil},
	}
	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			fields, err := buildTargetCompliance(test.platform, test.mediaMode, body, policy)
			if err != nil {
				t.Fatal(err)
			}
			wantOrder := []string{"ai_generated", "ai_disclosure", "commercial_content", "visibility", "comments", "download", "duet", "stitch", "youtube_audience", "retention"}
			if len(fields) < len(wantOrder) {
				t.Fatalf("fields=%+v", fields)
			}
			for index, key := range wantOrder {
				if fields[index].PolicyKey != key {
					t.Fatalf("field order[%d]=%q, want %q", index, fields[index].PolicyKey, key)
				}
			}
			for policyKey, platformKey := range test.wantKeys {
				var got ComplianceField
				for _, field := range fields {
					if field.PolicyKey == policyKey {
						got = field
						break
					}
				}
				if got.PlatformKey != platformKey {
					t.Fatalf("%s mapping=%+v, want platform key %q", policyKey, got, platformKey)
				}
				if platformKey != "" && got.Status != ComplianceStatusManual {
					t.Fatalf("%s browser control must remain manual until live state verification: %+v", policyKey, got)
				}
				if platformKey == "" && policyKey != "ai_disclosure" && policyKey != "retention" && got.Status != ComplianceStatusUnsupported {
					t.Fatalf("%s unmapped platform control must be unsupported: %+v", policyKey, got)
				}
			}
			if fields[1].Status != ComplianceStatusApplied || fields[9].Status != ComplianceStatusApplied {
				t.Fatalf("frozen disclosure and local retention invariants are missing: %+v", fields)
			}
			for key, value := range test.supplement {
				var got *ComplianceField
				for index := range fields {
					if fields[index].PolicyKey == key {
						got = &fields[index]
						break
					}
				}
				if got == nil || !reflect.DeepEqual(got.Value, value) {
					t.Fatalf("supplement %q=%+v, want %#v", key, got, value)
				}
				if key == "minimum_content_tags" {
					if got.Status != ComplianceStatusApplied {
						t.Fatalf("validated CSDN tag count status=%+v", got)
					}
				} else if got.Status != ComplianceStatusManual {
					t.Fatalf("supplemental browser field must remain manual: %+v", got)
				}
			}
		})
	}
}

func TestBuildTargetComplianceRequiresExactDisclosureForAIContent(t *testing.T) {
	_, err := buildTargetCompliance("instagram", "graph_text", "AI generated", AlphaGamerCompliancePolicy())
	if err == nil || !strings.Contains(err.Error(), "exact AI disclosure") {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyComplianceSettingsMergesPlatformValuesAndRejectsConflicts(t *testing.T) {
	policy := AlphaGamerCompliancePolicy()
	got, err := applyComplianceSettings("xhs", `{}`, policy)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(got), &values); err != nil {
		t.Fatal(err)
	}
	if values["content_source"] != "none" || values["visibility"] != "public" ||
		values["comment_enabled"] != true || values["declare_original"] != false ||
		values["allow_duet"] != false || values["allow_copy"] != false {
		t.Fatalf("mapped settings=%s", got)
	}
	if _, err := applyComplianceSettings("xhs", `{"visibility":"private"}`, policy); err == nil {
		t.Fatal("explicit setting conflicting with frozen compliance policy was accepted")
	}
	for platform, settings := range map[string]string{
		"xhs":      `{"allow_copy":true}`,
		"douyin":   `{"self_declaration":"ai"}`,
		"kuaishou": `{"comment_enabled":false}`,
		"tiktok":   `{"allow_download":true}`,
	} {
		if _, err := applyComplianceSettings(platform, settings, policy); err == nil {
			t.Fatalf("%s accepted settings conflicting with frozen compliance policy", platform)
		}
	}

	youtube, err := applyComplianceSettings("youtube", `{}`, policy)
	if err != nil {
		t.Fatal(err)
	}
	if youtube != `{"audience":"not_made_for_kids","visibility":"public","playlist":"","allow_comments":true,"altered_content":false}` {
		t.Fatalf("youtube settings=%s", youtube)
	}
}

func TestApplyComplianceSettingsProducesExactTenPlatformContracts(t *testing.T) {
	policy := AlphaGamerCompliancePolicy()
	want := map[string]string{
		"instagram":    `{}`,
		"facebook":     `{}`,
		"xhs":          `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}`,
		"douyin":       `{"activity":"","collection":"","extension_type":"none","location":"","hotspot":"","allow_download":false,"visibility":"public","music":"","comment_enabled":true,"self_declaration":"none"}`,
		"kuaishou":     `{"link_applet":"","declaration":"none","collection":"","location":"","allow_duet":false,"allow_download":false,"same_city":false,"visibility":"public","music":"","comment_enabled":true}`,
		"wechat_video": `{"location":"","collection":"","link_type":"none","link_address":"","activity":"","declare_original":false,"music":""}`,
		"wechat_mp":    `{"author":"αgamer","original_link":"","comment_enabled":false,"declare_original":false,"allow_reprint":false,"publish_type":"publish","collection":"","content_source":"none"}`,
		"csdn":         `{"collection":"","article_type":"original","original_link":"","backup_gitcode":false,"visibility":"public","activity":"","topic":""}`,
		"youtube":      `{"audience":"not_made_for_kids","visibility":"public","playlist":"","allow_comments":true,"altered_content":false}`,
		"tiktok":       `{"visibility":"public","allow_comment":true,"allow_duet":false,"allow_stitch":false,"ai_generated":false,"allow_download":false}`,
	}
	for code, expected := range want {
		t.Run(code, func(t *testing.T) {
			got, err := applyComplianceSettings(code, `{}`, policy)
			if err != nil || got != expected {
				t.Fatalf("settings=(%s,%v), want %s", got, err, expected)
			}
		})
	}
}

func TestFreezeV2PinsCompliancePolicyMappingsAndSettings(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent(
		"graph_text",
		"αgamer｜阿尔法玩家，正式入驻",
		"",
		"你好，这里是 αgamer｜阿尔法玩家。\n"+AlphaGamerAIDisclosure,
		`["αgamer","阿尔法玩家","游戏内容"]`,
		"draft",
	)
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "cover.png")
	const payload = "formal-cover"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	image, err := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)), hex.EncodeToString(digest[:]), 1080, 1440, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "formal-xhs", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	policy := AlphaGamerCompliancePolicy()

	plan, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID},
		CompliancePolicy: &policy,
		Targets:          []TargetInput{{AccountID: account.ID, SettingsJSON: `{}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Snapshot.Version != PublishPlanSnapshotVersion || plan.Snapshot.CompliancePolicy == nil ||
		!reflect.DeepEqual(*plan.Snapshot.CompliancePolicy, policy) {
		t.Fatalf("snapshot=%+v", plan.Snapshot)
	}
	policy.Download = true
	if plan.Snapshot.CompliancePolicy.Download {
		t.Fatal("frozen compliance policy retained the caller-owned pointer")
	}
	if len(plan.Snapshot.Targets[0].ComplianceFields) < 10 {
		t.Fatalf("target compliance=%+v", plan.Snapshot.Targets[0].ComplianceFields)
	}
	if got := plan.Snapshot.Targets[0].SettingsJSON; got != `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}` {
		t.Fatalf("settings=%s", got)
	}
	persisted, err := (&Service{DB: database}).Get(plan.ID)
	if err != nil || persisted.Snapshot.Version != PublishPlanSnapshotVersion || persisted.Snapshot.CompliancePolicy == nil {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
}

func TestFreezeAIComplianceRejectsMissingDisclosureAndInsufficientCSDNTags(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "formal-csdn", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	policy := AlphaGamerCompliancePolicy()

	for name, test := range map[string]struct {
		body string
		tags string
		want string
	}{
		"disclosure": {body: "generic intro", tags: `["one","two","three"]`, want: "exact AI disclosure"},
		"tags":       {body: AlphaGamerAIDisclosure, tags: `["one","two"]`, want: "at least three"},
	} {
		t.Run(name, func(t *testing.T) {
			content, createErr := database.CreateContent("article", name, "", test.body, test.tags, "draft")
			if createErr != nil {
				t.Fatal(createErr)
			}
			_, freezeErr := (&Service{DB: database}).Freeze(FreezeInput{
				ContentID: content.ID, MediaMode: "article", CompliancePolicy: &policy,
				Targets: []TargetInput{{AccountID: account.ID, SettingsJSON: `{}`}},
			})
			if freezeErr == nil || !strings.Contains(freezeErr.Error(), test.want) {
				t.Fatalf("err=%v, want %q", freezeErr, test.want)
			}
		})
	}
}
