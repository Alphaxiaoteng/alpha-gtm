package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/config"
	platformpkg "alpha-nexus-egolite/internal/platforms"
)

func templateRequest(t *testing.T, server *Server, settingsJSON string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"platform_code": "xhs", "name": "XHS schema", "description": "safe",
		"settings_json": settingsJSON, "enabled": true, "is_default": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/templates", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func TestPlatformsAPIExposesCoreCreatorSettingsSchemas(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	request := httptest.NewRequest(http.MethodGet, "/api/platforms", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var items []struct {
		Code           string                      `json:"code"`
		SettingsSchema *platformpkg.SettingsSchema `json:"settingsSchema"`
	}
	if err := json.NewDecoder(response.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	wantVersions := map[string]int{
		"xhs":          platformpkg.XHSSettingsSchemaVersion,
		"douyin":       platformpkg.DouyinSettingsSchemaVersion,
		"wechat_mp":    platformpkg.WechatMPSettingsSchemaVersion,
		"csdn":         platformpkg.CSDNSettingsSchemaVersion,
		"wechat_video": platformpkg.WechatVideoSettingsSchemaVersion,
		"kuaishou":     platformpkg.KuaishouSettingsSchemaVersion,
	}
	for _, item := range items {
		switch item.Code {
		case "xhs", "douyin", "wechat_mp", "csdn", "wechat_video", "kuaishou":
			found[item.Code] = true
			wantVersion := wantVersions[item.Code]
			if item.SettingsSchema == nil || item.SettingsSchema.Version != wantVersion || len(item.SettingsSchema.Fields) == 0 {
				t.Fatalf("%s settings schema=%+v", item.Code, item.SettingsSchema)
			}
		case "bilibili":
			if item.SettingsSchema != nil {
				t.Fatalf("manual platform schema leaked: %+v", item.SettingsSchema)
			}
		}
	}
	for _, code := range []string{"xhs", "douyin", "wechat_mp", "csdn", "wechat_video", "kuaishou"} {
		if !found[code] {
			t.Fatalf("%s settings schema missing", code)
		}
	}
}

func TestXHSTemplateAPIValidatesAndNormalizesSchemaSettings(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	invalid := templateRequest(t, server, `{"visibility":"followers"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	valid := templateRequest(t, server, `{"visibility":"private","location":"上海市徐汇区"}`)
	if valid.Code != http.StatusCreated {
		t.Fatalf("valid status=%d body=%s", valid.Code, valid.Body.String())
	}
	var created struct {
		SettingsJSON string `json:"settings_json"`
	}
	if err := json.NewDecoder(valid.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	want := `{"visibility":"private","comment_enabled":true,"declare_original":false,"location":"上海市徐汇区","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}`
	if created.SettingsJSON != want {
		t.Fatalf("settings=%s, want %s", created.SettingsJSON, want)
	}
}
