package platforms_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestXHSSettingsSchema(t *testing.T) {
	schema, ok := platforms.SettingsSchemaFor(" XHS ")
	if !ok {
		t.Fatal("xhs settings schema not found")
	}
	if schema.PlatformCode != "xhs" || schema.Version != 6 {
		t.Fatalf("schema identity=%+v", schema)
	}
	wantKeys := []string{"location", "collection", "group", "declare_original", "content_source", "related_media", "visibility", "comment_enabled", "allow_duet", "allow_copy"}
	if len(schema.Fields) != len(wantKeys) {
		t.Fatalf("fields=%+v", schema.Fields)
	}
	for index, key := range wantKeys {
		if schema.Fields[index].Key != key {
			t.Fatalf("field[%d]=%q, want %q", index, schema.Fields[index].Key, key)
		}
	}
}

func TestCoreCreatorSettingsSchemasNormalizeOnlyDeclaredFields(t *testing.T) {
	tests := []struct {
		code     string
		wantKeys []string
		valid    string
		want     string
	}{
		{
			code:     "douyin",
			wantKeys: []string{"activity", "collection", "extension_type", "location", "hotspot", "allow_download", "visibility", "music", "comment_enabled", "friend_mention", "self_declaration", "video_chapters"},
			valid:    `{"visibility":"private","location":"上海","comment_enabled":false,"self_declaration":"ai"}`,
			want:     `{"activity":"","collection":"","extension_type":"none","location":"上海","hotspot":"","allow_download":true,"visibility":"private","music":"","comment_enabled":false,"self_declaration":"ai"}`,
		},
		{
			code:     "wechat_mp",
			wantKeys: []string{"author", "original_link", "comment_enabled", "declare_original", "allow_reprint", "publish_type", "collection", "content_source"},
			valid:    `{"author":"Alpha Nexus","publish_type":"publish"}`,
			want:     `{"author":"Alpha Nexus","original_link":"","comment_enabled":false,"declare_original":false,"allow_reprint":false,"publish_type":"publish","collection":"","content_source":"none"}`,
		},
		{
			code:     "csdn",
			wantKeys: []string{"collection", "article_type", "original_link", "backup_gitcode", "visibility", "activity", "topic"},
			valid:    `{"article_type":"repost","original_link":"https://example.com/source"}`,
			want:     `{"collection":"","article_type":"repost","original_link":"https://example.com/source","backup_gitcode":false,"visibility":"public","activity":"","topic":""}`,
		},
		{
			code:     "wechat_video",
			wantKeys: []string{"location", "collection", "link_type", "link_address", "activity", "declare_original", "music"},
			valid:    `{"location":"杭州","declare_original":true}`,
			want:     `{"location":"杭州","collection":"","link_type":"none","link_address":"","activity":"","declare_original":true,"music":""}`,
		},
		{
			code:     "kuaishou",
			wantKeys: []string{"link_applet", "declaration", "collection", "location", "allow_duet", "allow_download", "same_city", "visibility", "music", "comment_enabled"},
			valid:    `{"visibility":"private","allow_download":false,"location":"杭州","comment_enabled":false}`,
			want:     `{"link_applet":"","declaration":"none","collection":"","location":"杭州","allow_duet":true,"allow_download":false,"same_city":true,"visibility":"private","music":"","comment_enabled":false}`,
		},
		{
			code:     "youtube",
			wantKeys: []string{"audience", "visibility", "playlist", "allow_comments", "altered_content"},
			valid:    `{"audience":"not_made_for_kids","visibility":"unlisted","playlist":"Alpha Nexus"}`,
			want:     `{"audience":"not_made_for_kids","visibility":"unlisted","playlist":"Alpha Nexus","allow_comments":true,"altered_content":false}`,
		},
		{
			code:     "tiktok",
			wantKeys: []string{"visibility", "allow_comment", "allow_duet", "allow_stitch", "ai_generated", "allow_download"},
			valid:    `{"visibility":"private","allow_duet":false,"allow_download":false}`,
			want:     `{"visibility":"private","allow_comment":true,"allow_duet":false,"allow_stitch":true,"ai_generated":false,"allow_download":false}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			schema, ok := platforms.SettingsSchemaFor(tt.code)
			if !ok || schema.PlatformCode != tt.code || len(schema.Fields) != len(tt.wantKeys) {
				t.Fatalf("schema=%+v ok=%v", schema, ok)
			}
			if schema.Verification == "" {
				t.Fatalf("%s schema has no verification state: %+v", tt.code, schema)
			}
			if schema.Verification == "console_verified" && schema.VerifiedAt == "" {
				t.Fatalf("%s verified schema has no verification date: %+v", tt.code, schema)
			}
			for index, key := range tt.wantKeys {
				if schema.Fields[index].Key != key {
					t.Fatalf("field[%d]=%q, want %q", index, schema.Fields[index].Key, key)
				}
			}
			got, err := platforms.NormalizeSettings(tt.code, tt.valid)
			if err != nil || got != tt.want {
				t.Fatalf("NormalizeSettings()=(%s,%v), want %s", got, err, tt.want)
			}
			if _, err := platforms.NormalizeSettings(tt.code, `{"unknown":true}`); err == nil {
				t.Fatal("unknown setting accepted")
			}
		})
	}
}

func TestYouTubeFormalDefaultsArePublicAndNotMadeForKids(t *testing.T) {
	schema, ok := platforms.SettingsSchemaFor("youtube")
	if !ok || schema.Version != 2 {
		t.Fatalf("youtube schema=%+v ok=%v", schema, ok)
	}
	got, err := platforms.NormalizeSettings("youtube", `{}`)
	const want = `{"audience":"not_made_for_kids","visibility":"public","playlist":"","allow_comments":true,"altered_content":false}`
	if err != nil || got != want {
		t.Fatalf("NormalizeSettings(youtube empty)=(%s,%v), want %s", got, err, want)
	}
}

func TestFormalTenPlatformSchemasExposeUnknownControlsAsManualUnverified(t *testing.T) {
	requested := map[string][]string{
		"instagram":    {"visibility", "comment_enabled", "commercial_content", "ai_generated"},
		"facebook":     {"visibility", "comment_enabled", "commercial_content", "ai_generated"},
		"xhs":          {"content_source", "declare_original", "visibility", "comment_enabled", "allow_duet", "allow_copy"},
		"douyin":       {"self_declaration", "visibility", "comment_enabled", "allow_download"},
		"kuaishou":     {"declaration", "visibility", "comment_enabled", "allow_download", "allow_duet", "same_city"},
		"wechat_video": {"declare_original"},
		"wechat_mp":    {"author", "content_source", "comment_enabled", "declare_original", "allow_reprint", "publish_type"},
		"csdn":         {"article_type", "visibility", "backup_gitcode"},
		"youtube":      {"audience", "visibility", "allow_comments", "altered_content"},
		"tiktok":       {"ai_generated", "visibility", "allow_comment", "allow_duet", "allow_stitch", "allow_download"},
	}
	for code, keys := range requested {
		schema, ok := platforms.SettingsSchemaFor(code)
		if !ok {
			t.Fatalf("%s schema missing", code)
		}
		fields := make(map[string]platforms.SettingField, len(schema.Fields))
		for _, field := range schema.Fields {
			fields[field.Key] = field
			if field.Type == "manual" && field.Verification != "unverified" {
				t.Fatalf("%s manual field %q verification=%q", code, field.Key, field.Verification)
			}
		}
		for _, key := range keys {
			if _, exists := fields[key]; !exists {
				t.Fatalf("%s requested compliance field %q missing", code, key)
			}
		}
	}

	for _, code := range []string{"instagram", "facebook"} {
		normalized, err := platforms.NormalizeSettings(code, `{}`)
		if err != nil || normalized != `{}` {
			t.Fatalf("%s normalized=(%s,%v)", code, normalized, err)
		}
		if _, err := platforms.NormalizeSettings(code, `{"visibility":"public"}`); err == nil {
			t.Fatalf("%s accepted an unverified browser control as automatically applied", code)
		}
	}
}

func TestPublishSettingsSchemasExposeEvidenceFreshness(t *testing.T) {
	for _, code := range []string{"xhs", "douyin", "kuaishou", "wechat_mp", "csdn", "wechat_video"} {
		schema, ok := platforms.SettingsSchemaFor(code)
		if !ok {
			t.Fatalf("%s schema missing", code)
		}
		switch code {
		case "kuaishou", "wechat_video":
			if schema.Verification != "contract_verified" || schema.VerifiedAt != "2026-07-25" {
				t.Fatalf("%s secondary contract evidence metadata=%+v", code, schema)
			}
		default:
			if schema.Verification != "console_verified" || schema.VerifiedAt != "2026-07-25" {
				t.Fatalf("%s schema evidence metadata=%+v", code, schema)
			}
		}
	}
}

func TestCorePublishSettingsOnlyPersistVerifiedOptionalPreferences(t *testing.T) {
	for _, code := range []string{"xhs", "douyin", "kuaishou", "wechat_video"} {
		schema, ok := platforms.SettingsSchemaFor(code)
		if !ok {
			t.Fatalf("%s schema missing", code)
		}
		for _, field := range schema.Fields {
			if field.Required {
				t.Fatalf("%s setting %q must stay optional; title and content requirements belong to the content contract", code, field.Key)
			}
			if field.Type == "manual" {
				if field.Remember {
					t.Fatalf("%s manual setting %q must not be saved as an applied value", code, field.Key)
				}
				continue
			}
			if !field.Remember {
				t.Fatalf("%s optional setting %q must explicitly remember the previous selection", code, field.Key)
			}
			if field.Type == "string" && field.Default != "" {
				t.Fatalf("%s dynamic setting %q has unverified magic default %#v", code, field.Key, field.Default)
			}
		}
	}
}

func TestPublishSettingsSchemaKeepsUnreviewedWorkerControlsFailClosed(t *testing.T) {
	schema, ok := platforms.SettingsSchemaFor("douyin")
	if !ok {
		t.Fatal("douyin schema missing")
	}
	manual := map[string]bool{}
	fields := map[string]platforms.SettingField{}
	for _, field := range schema.Fields {
		fields[field.Key] = field
		if field.Type == "manual" {
			manual[field.Key] = true
		}
		if field.Type != "manual" && !field.Remember {
			t.Fatalf("optional automatic field %q must remember the previous account selection", field.Key)
		}
	}
	for _, key := range []string{"friend_mention", "video_chapters"} {
		if !manual[key] {
			t.Fatalf("official field %q must remain visible without claiming automatic support", key)
		}
	}
	for _, key := range []string{"comment_enabled", "self_declaration"} {
		field := fields[key]
		if field.Type == "manual" || field.Verification != "unverified" {
			t.Fatalf("frozen compliance field %q must be typed but remain live-unverified: %+v", key, field)
		}
	}
}

func TestNormalizeXHSSettingsDefaultsAndStableOrder(t *testing.T) {
	want := `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}`
	for _, raw := range []string{"", `{}`, `{ "declare_original": false }`} {
		got, err := platforms.NormalizeSettings("xhs", raw)
		if err != nil {
			t.Fatalf("NormalizeSettings(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("NormalizeSettings(%q)=%s, want %s", raw, got, want)
		}
	}

	got, err := platforms.NormalizeSettings(" XHS ", `{"declare_original":true,"visibility":"private","comment_enabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"visibility":"private","comment_enabled":false,"declare_original":true,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}`
	if got != want {
		t.Fatalf("normalized=%s, want %s", got, want)
	}

	got, err = platforms.NormalizeSettings("xhs", `{"location":"上海市徐汇区"}`)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"上海市徐汇区","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}`
	if got != want {
		t.Fatalf("normalized location=%s, want %s", got, want)
	}
}

func TestNormalizeXHSSettingsRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"unknown field":     `{"unknown":"Alpha"}`,
		"location newline":  "{\"location\":\"上海\\n徐汇\"}",
		"location too long": `{"location":"` + strings.Repeat("a", 129) + `"}`,
		"wrong enum":        `{"visibility":"followers"}`,
		"enum wrong type":   `{"visibility":true}`,
		"boolean string":    `{"comment_enabled":"true"}`,
		"boolean null":      `{"declare_original":null}`,
		"allow duet string": `{"allow_duet":"false"}`,
		"allow copy null":   `{"allow_copy":null}`,
		"top level array":   `[]`,
		"top level null":    `null`,
		"trailing document": `{} {}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := platforms.NormalizeSettings("xhs", raw); err == nil {
				t.Fatalf("accepted invalid settings: %s", raw)
			}
		})
	}
	if _, err := platforms.NormalizeSettings("bilibili", `{}`); err == nil {
		t.Fatal("platform without schema was accepted")
	}
}

func TestBuildAndParsePublishSettingsSnapshot(t *testing.T) {
	raw, err := platforms.BuildPublishSettingsSnapshot("xhs", 42, `{"visibility":"private"}`, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"snapshot_version":1,"platform_code":"xhs","template_id":42,"settings_schema_version":6,"selector_catalog_version":7,"adapter_version":3,"settings":{"visibility":"private","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}}`
	if raw != want {
		t.Fatalf("snapshot=%s, want %s", raw, want)
	}

	snapshot, err := platforms.ParsePublishSettingsSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PlatformCode != "xhs" || snapshot.TemplateID != 42 || snapshot.SettingsSchemaVersion != 6 || snapshot.SelectorCatalogVersion != 7 || snapshot.AdapterVersion != 3 {
		t.Fatalf("snapshot identity=%+v", snapshot)
	}
	if string(snapshot.Settings) != `{"visibility":"private","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}` {
		t.Fatalf("settings=%s", snapshot.Settings)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil || len(decoded) != 7 {
		t.Fatalf("snapshot JSON shape invalid: keys=%d err=%v", len(decoded), err)
	}
}

func TestPublishSettingsSnapshotRejectsInvalidEnvelope(t *testing.T) {
	valid := `{"snapshot_version":1,"platform_code":"xhs","template_id":0,"settings_schema_version":2,"selector_catalog_version":1,"adapter_version":1,"settings":{}}`
	tests := map[string]string{
		"unknown envelope field": strings.Replace(valid, `"settings":{}`, `"extra":true,"settings":{}`, 1),
		"snapshot version":       strings.Replace(valid, `"snapshot_version":1`, `"snapshot_version":2`, 1),
		"schema version":         strings.Replace(valid, `"settings_schema_version":2`, `"settings_schema_version":99`, 1),
		"catalog version":        strings.Replace(valid, `"selector_catalog_version":1`, `"selector_catalog_version":0`, 1),
		"adapter version":        strings.Replace(valid, `"adapter_version":1`, `"adapter_version":0`, 1),
		"negative template":      strings.Replace(valid, `"template_id":0`, `"template_id":-1`, 1),
		"wrong platform":         strings.Replace(valid, `"platform_code":"xhs"`, `"platform_code":"bilibili"`, 1),
		"invalid settings":       strings.Replace(valid, `"settings":{}`, `"settings":{"visibility":"followers"}`, 1),
		"trailing document":      valid + `{}`,
	}

	legacy := strings.Replace(valid, `"settings_schema_version":2`, `"settings_schema_version":1`, 1)
	if _, err := platforms.ParsePublishSettingsSnapshot(legacy); err != nil {
		t.Fatalf("legacy XHS V1 snapshot rejected: %v", err)
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := platforms.ParsePublishSettingsSnapshot(raw); err == nil {
				t.Fatalf("accepted invalid snapshot: %s", raw)
			}
		})
	}

	for name, args := range map[string]struct {
		templateID     int64
		catalogVersion int
		adapterVersion int
	}{
		"negative template": {-1, 1, 1},
		"catalog version":   {0, 0, 1},
		"adapter version":   {0, 1, 0},
	} {
		t.Run("build "+name, func(t *testing.T) {
			if _, err := platforms.BuildPublishSettingsSnapshot("xhs", args.templateID, `{}`, args.catalogVersion, args.adapterVersion); err == nil {
				t.Fatal("BuildPublishSettingsSnapshot accepted invalid metadata")
			}
		})
	}
}

func TestParsePublishSettingsSnapshotKeepsEarlierComplianceSchemasReadable(t *testing.T) {
	tests := []struct {
		platform string
		version  int
		want     string
	}{
		{"xhs", 5, `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}`},
		{"douyin", 3, `{"activity":"","collection":"","extension_type":"none","location":"","hotspot":"","allow_download":true,"visibility":"public","music":"","comment_enabled":true,"self_declaration":"none"}`},
		{"kuaishou", 2, `{"link_applet":"","declaration":"none","collection":"","location":"","allow_duet":true,"allow_download":true,"same_city":true,"visibility":"public","music":"","comment_enabled":true}`},
		{"tiktok", 1, `{"visibility":"private","allow_comment":true,"allow_duet":true,"allow_stitch":true,"ai_generated":false,"allow_download":true}`},
	}
	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			raw := fmt.Sprintf(
				`{"snapshot_version":1,"platform_code":%q,"template_id":0,"settings_schema_version":%d,"selector_catalog_version":1,"adapter_version":1,"settings":{}}`,
				test.platform, test.version,
			)
			snapshot, err := platforms.ParsePublishSettingsSnapshot(raw)
			if err != nil || string(snapshot.Settings) != test.want {
				t.Fatalf("snapshot=%+v err=%v", snapshot, err)
			}
		})
	}
}
