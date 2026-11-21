package skilllib

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func testLibrary(t *testing.T) *Library {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	library, err := New(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func TestLibrarySeedsReviewedSkillsAndPersistsLocalSkill(t *testing.T) {
	library := testLibrary(t)
	items, err := library.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 4 || items[0].ID == "" {
		t.Fatalf("seeded skills=%+v", items)
	}
	creator, err := library.Get(context.Background(), "skill-creator")
	if err != nil || creator.Status != StatusReviewed || !creator.Enabled {
		t.Fatalf("creator=%+v err=%v", creator, err)
	}

	created, err := library.Create(context.Background(), CreateInput{
		ID: "xhs-draft-helper", Name: "小红书草稿助手",
		Description:  "根据已核实素材整理小红书草稿。",
		Instructions: "先核对素材来源，再生成草稿。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Enabled || created.Status != StatusReady || created.Source != SourceLocal {
		t.Fatalf("created=%+v", created)
	}
	raw, err := os.ReadFile(filepath.Join(library.Root(), created.ID, "SKILL.md"))
	if err != nil || !strings.Contains(string(raw), "name: xhs-draft-helper") {
		t.Fatalf("skill file=%q err=%v", raw, err)
	}
}

func TestLibrarySeedsPublishSkillsAsReviewedInstructions(t *testing.T) {
	library := testLibrary(t)
	items, err := library.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 12 {
		t.Fatalf("system skill count=%d, want 12", len(items))
	}

	byID := make(map[string]Skill, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	for _, id := range []string{"video-publisher", "multipost", "social-publish-prepare", "social-publish-submit-approved"} {
		skill, ok := byID[id]
		if !ok {
			t.Fatalf("missing native skill %q: %+v", id, byID)
		}
		if skill.Source != SourceSystem || skill.Status != StatusReviewed || skill.Runtime != RuntimeInstructions ||
			skill.RuntimeRef != "" || !skill.Enabled || skill.HasScripts {
			t.Fatalf("native skill=%+v", skill)
		}
	}
	oneClick, ok := byID["one-click-publish"]
	if !ok {
		t.Fatalf("missing one-click-publish: %+v", byID)
	}
	if oneClick.Source != SourceSystem || oneClick.Status != StatusReviewed || oneClick.Runtime != RuntimeInstructions ||
		oneClick.RuntimeRef != "" || !oneClick.Enabled || oneClick.HasScripts {
		t.Fatalf("one-click skill=%+v", oneClick)
	}
	oneClickWithInstructions, err := library.Get(context.Background(), "one-click-publish")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(oneClickWithInstructions.Instructions, "social.publish-capabilities") ||
		!strings.Contains(oneClickWithInstructions.Instructions, "publish.plan.freeze") ||
		!strings.Contains(oneClickWithInstructions.Instructions, "publish.plan.prepare") ||
		!strings.Contains(oneClickWithInstructions.Instructions, "Never call publish.plan.submit") {
		t.Fatalf("one-click instructions missing workflow boundaries: %q", oneClickWithInstructions.Instructions)
	}
}

func TestLibraryEnableResolveAndAutomationReviewGate(t *testing.T) {
	library := testLibrary(t)
	created, err := library.Create(context.Background(), CreateInput{
		ID: "caption-check", Name: "文案检查", Description: "检查文案中的未验证事实。",
		Instructions: "标记每一项没有来源的事实。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.SetEnabled(context.Background(), created.ID, true); err != nil {
		t.Fatal(err)
	}
	expanded, refs, err := library.ExpandPrompt(context.Background(), "请用 /caption-check 检查这个文案，再 @caption-check 复核")
	if err != nil || len(refs) != 1 || refs[0] != created.ID {
		t.Fatalf("refs=%v err=%v", refs, err)
	}
	if !strings.Contains(expanded, "标记每一项没有来源的事实") || !strings.Contains(expanded, "请用 /caption-check") {
		t.Fatalf("expanded=%s", expanded)
	}
	inputs, err := library.ResolveSkillInputs(context.Background(), "请用 /caption-check 检查")
	if err != nil || len(inputs) != 1 || inputs[0]["name"] != "caption-check" || inputs[0]["path"] != filepath.Join(library.Root(), "caption-check", "SKILL.md") {
		t.Fatalf("structured inputs=%v err=%v", inputs, err)
	}

	automation, err := library.Create(context.Background(), CreateInput{
		ID: "browser-form-fill", Name: "表单预填", Description: "自动化填写固定表单。",
		Instructions: "调用审核后的固定脚本填写表单。", Automation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if automation.Status != StatusReviewRequired {
		t.Fatalf("automation=%+v", automation)
	}
	if _, err := library.SetEnabled(context.Background(), automation.ID, true); err == nil {
		t.Fatal("automation skill enabled before review")
	}
}

func TestLibraryImportsStandardSkillMarkdown(t *testing.T) {
	library := testLibrary(t)
	imported, err := library.Import(context.Background(), `---
name: imported-helper
description: This skill should be used when checking a supplied draft.
---

# Imported helper

Check only the supplied draft and cite every issue.
`)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID != "imported-helper" || imported.Enabled || imported.Status != StatusReady {
		t.Fatalf("imported=%+v", imported)
	}
}

func TestLibraryStartupUpdatesOnlyBuiltInSkillsAndPreservesEnabledChoice(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	library, err := New(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.SetEnabled(context.Background(), "ego-browser", false); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE skills SET name='stale',description='stale' WHERE id='ego-browser'`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library.Root(), "ego-browser", "SKILL.md"), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	custom, err := library.Create(context.Background(), CreateInput{
		ID: "local-style", Name: "本地风格", Description: "用户自己的写作风格。", Instructions: "保留用户自己的内容。",
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(database, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	builtin, err := reloaded.Get(context.Background(), "ego-browser")
	if err != nil || builtin.Name != "ego-browser 1.2.3" || builtin.Enabled || strings.Contains(builtin.Instructions, "stale") {
		t.Fatalf("builtin=%+v err=%v", builtin, err)
	}
	local, err := reloaded.Get(context.Background(), custom.ID)
	if err != nil || local.Instructions != "保留用户自己的内容。" {
		t.Fatalf("local=%+v err=%v", local, err)
	}
}
