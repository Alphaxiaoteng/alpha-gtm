package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/skilllib"
)

func localSkillRequest(method, target string, body []byte) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:17321"
	return request
}

func TestSkillLibraryRoutesCreateEnableAndReadLocalSkill(t *testing.T) {
	server := testAgentHubServer(t)

	listed := httptest.NewRecorder()
	server.ServeHTTP(listed, localSkillRequest(http.MethodGet, "/api/skills", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "skill-creator") {
		t.Fatalf("list=%d body=%s", listed.Code, listed.Body.String())
	}

	created := httptest.NewRecorder()
	server.ServeHTTP(created, localSkillRequest(http.MethodPost, "/api/skills", []byte(`{
		"id":"local-caption-check","name":"文案检查","description":"检查未验证事实。",
		"instructions":"列出所有没有来源的事实。"
	}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var skill skilllib.Skill
	if err := json.Unmarshal(created.Body.Bytes(), &skill); err != nil || skill.Enabled {
		t.Fatalf("skill=%+v err=%v", skill, err)
	}

	enabled := httptest.NewRecorder()
	server.ServeHTTP(enabled, localSkillRequest(http.MethodPut, "/api/skills/local-caption-check/enabled", []byte(`{"enabled":true}`)))
	if enabled.Code != http.StatusOK || !strings.Contains(enabled.Body.String(), `"enabled":true`) {
		t.Fatalf("enable=%d body=%s", enabled.Code, enabled.Body.String())
	}

	detail := httptest.NewRecorder()
	server.ServeHTTP(detail, localSkillRequest(http.MethodGet, "/api/skills/local-caption-check", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "列出所有没有来源的事实") {
		t.Fatalf("detail=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestSkillLibraryImportAndAutomationReviewGate(t *testing.T) {
	server := testAgentHubServer(t)
	markdown := "---\nname: imported-draft\ndescription: This skill should be used when checking a draft.\n---\n\nCheck only supplied text."
	body, _ := json.Marshal(map[string]string{"content": markdown})
	imported := httptest.NewRecorder()
	server.ServeHTTP(imported, localSkillRequest(http.MethodPost, "/api/skills/import", body))
	if imported.Code != http.StatusCreated || !strings.Contains(imported.Body.String(), "imported-draft") {
		t.Fatalf("import=%d body=%s", imported.Code, imported.Body.String())
	}

	automation := httptest.NewRecorder()
	server.ServeHTTP(automation, localSkillRequest(http.MethodPost, "/api/skills", []byte(`{
		"id":"fixed-browser-flow","name":"固定浏览器流程","description":"固定表单自动化。",
		"instructions":"运行审核后的固定脚本。","automation":true
	}`)))
	if automation.Code != http.StatusCreated || !strings.Contains(automation.Body.String(), skilllib.StatusReviewRequired) {
		t.Fatalf("automation=%d body=%s", automation.Code, automation.Body.String())
	}

	enabled := httptest.NewRecorder()
	server.ServeHTTP(enabled, localSkillRequest(http.MethodPut, "/api/skills/fixed-browser-flow/enabled", []byte(`{"enabled":true}`)))
	if enabled.Code != http.StatusConflict {
		t.Fatalf("enable=%d body=%s", enabled.Code, enabled.Body.String())
	}
}
