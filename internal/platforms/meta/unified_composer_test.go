//go:build legacy_chromium

package meta

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestMetaUnifiedComposerURLUsesPagePublicIDWithoutBusinessID(t *testing.T) {
	facebook, ok := metaUnifiedComposerURL("facebook", platforms.PublishRequest{
		AssetExternalID: "1241410172381837",
	})
	if !ok || facebook != "https://business.facebook.com/latest/composer/?asset_id=1241410172381837" {
		t.Fatalf("facebook composer=%q ok=%v", facebook, ok)
	}
	instagram, ok := metaUnifiedComposerURL("instagram", platforms.PublishRequest{
		AssetExternalID:          "17841416733781085",
		ConnectedAssetExternalID: "1241410172381837",
	})
	if !ok || instagram != facebook {
		t.Fatalf("instagram composer=%q ok=%v", instagram, ok)
	}
	if strings.Contains(instagram, "business_id") {
		t.Fatalf("composer must not invent a business id: %q", instagram)
	}
	if _, ok := metaUnifiedComposerURL("instagram", platforms.PublishRequest{
		AssetExternalID: "17841416733781085",
	}); ok {
		t.Fatal("unlinked Instagram target must fail closed")
	}
}

func TestMetaUnifiedComposerSingleTargetScriptDeselectsFacebookForInstagram(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"instagram",
		"chenpaiteng",
		"17841416733781085",
		"1241410172381837",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("expected a verified Instagram target script")
	}
	lowerScript := strings.ToLower(script)
	for _, forbidden := range []string{"publish", "share", "发布", "分享", "document.body"} {
		if strings.Contains(lowerScript, strings.ToLower(forbidden)) {
			t.Fatalf("target selector must not reach a final publish control (%q): %s", forbidden, script)
		}
	}
	result := runUnifiedComposerFixture(t, script)
	if result.ComboboxText != "chenpaiteng" {
		t.Fatalf("combobox=%q steps=%+v", result.ComboboxText, result.Steps)
	}
	assertOnlyMetaFixtureTargetSelected(t, result.Options, "chenpaiteng")
	assertMetaPreferenceOptionUntouched(t, result.Options)
	if len(result.Steps) < 2 || result.Steps[0].State != "changed" || result.Steps[len(result.Steps)-1].State != "confirmed" {
		t.Fatalf("steps=%+v", result.Steps)
	}
}

func TestMetaUnifiedComposerSingleTargetScriptMatchesLiveRootlessIconIdentity(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"instagram",
		"chenpaiteng",
		"17841416733781085",
		"1241410172381837",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("expected a verified Instagram target script")
	}
	result := runUnifiedComposerFixtureMode(t, script, "live-rootless-no-ids")
	if result.ComboboxText != "chenpaiteng" {
		t.Fatalf("combobox=%q steps=%+v", result.ComboboxText, result.Steps)
	}
	assertOnlyMetaFixtureTargetSelected(t, result.Options, "chenpaiteng")
	if len(result.Steps) < 2 || result.Steps[0].State != "changed" || result.Steps[len(result.Steps)-1].State != "confirmed" {
		t.Fatalf("steps=%+v", result.Steps)
	}
}

func TestMetaUnifiedComposerSingleTargetScriptDeselectsInstagramForFacebook(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"facebook",
		"AlphaIntelligence",
		"1241410172381837",
		"1241410172381837",
		"chenpaiteng",
		"17841416733781085",
	)
	if !ok {
		t.Fatal("expected a verified Facebook target script")
	}
	result := runUnifiedComposerFixture(t, script)
	if result.ComboboxText != "AlphaIntelligence" {
		t.Fatalf("combobox=%q steps=%+v", result.ComboboxText, result.Steps)
	}
	assertOnlyMetaFixtureTargetSelected(t, result.Options, "AlphaIntelligence")
	assertMetaPreferenceOptionUntouched(t, result.Options)
}

func TestMetaUnifiedComposerSingleTargetScriptFailsClosedOnUnknownOption(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"instagram",
		"not-present",
		"17841416733781085",
		"1241410172381837",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("public identity should build a script")
	}
	result := runUnifiedComposerFixture(t, script)
	if len(result.Steps) == 0 || result.Steps[0].State != "target_missing" {
		t.Fatalf("steps=%+v", result.Steps)
	}
	for _, step := range result.Steps {
		if step.State == "confirmed" {
			t.Fatalf("unknown target must not confirm: %+v", result.Steps)
		}
	}
}

func TestMetaUnifiedComposerSingleTargetScriptFailsClosedOnThirdSelectedAsset(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"instagram",
		"chenpaiteng",
		"17841416733781085",
		"1241410172381837",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("expected a verified Instagram target script")
	}
	result := runUnifiedComposerFixtureMode(t, script, "unknown-selected")
	if len(result.Steps) == 0 || result.Steps[0].State != "unexpected_target_option" {
		t.Fatalf("steps=%+v", result.Steps)
	}
	for _, option := range result.Options {
		if option.Clicks != 0 {
			t.Fatalf("unsafe target state must not be changed or confirmed: %+v", result.Options)
		}
	}
}

func TestMetaUnifiedComposerSingleTargetScriptFailsClosedOnThirdUnselectedAsset(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"instagram",
		"chenpaiteng",
		"17841416733781085",
		"1241410172381837",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("expected a verified Instagram target script")
	}
	result := runUnifiedComposerFixtureMode(t, script, "unknown-unselected")
	if len(result.Steps) == 0 || result.Steps[0].State != "unexpected_target_option" {
		t.Fatalf("steps=%+v", result.Steps)
	}
	for _, option := range result.Options {
		if option.Clicks != 0 {
			t.Fatalf("unknown target must fail before changing any selection: %+v", result.Options)
		}
	}
}

func TestMetaUnifiedComposerRequiresPublicIDsForBothConnectedOptions(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"instagram",
		"chenpaiteng",
		"17841416733781085",
		"1241410172381837",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("expected a frozen connected target script")
	}
	for _, mode := range []string{"wrong-target-id", "wrong-connected-id", "missing-target-id", "missing-connected-id"} {
		result := runUnifiedComposerFixtureMode(t, script, mode)
		if len(result.Steps) == 0 || result.Steps[0].State == "confirmed" {
			t.Fatalf("mode=%s must fail closed: %+v", mode, result.Steps)
		}
		for _, option := range result.Options {
			if option.Clicks != 0 {
				t.Fatalf("mode=%s changed an identity-mismatched option: %+v", mode, result.Options)
			}
		}
	}
}

func TestMetaUnifiedComposerOpensDelayedListboxOnlyOnce(t *testing.T) {
	script, ok := metaUnifiedTargetScript(
		"instagram",
		"chenpaiteng",
		"17841416733781085",
		"1241410172381837",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("expected a frozen connected target script")
	}
	result := runUnifiedComposerFixtureMode(t, script, "delayed-listbox")
	if result.ComboboxClicks != 1 || result.Steps[len(result.Steps)-1].State != "confirmed" {
		t.Fatalf("delayed chooser clicks=%d steps=%+v", result.ComboboxClicks, result.Steps)
	}
}

func TestMetaSubmitScriptReadsExactTargetAtClickEdge(t *testing.T) {
	script, ok := metaSubmitScript("instagram", platforms.PublishRequest{
		ContentType:              "graph_text",
		Caption:                  "正文",
		Media:                    []platforms.PublishMedia{{Path: "/tmp/image.png", Kind: "image", Role: "content"}},
		AssetName:                "chenpaiteng",
		AssetExternalID:          "17841416733781085",
		ConnectedAssetName:       "AlphaIntelligence",
		ConnectedAssetExternalID: "1241410172381837",
	})
	if !ok {
		t.Fatal("expected submit script for a frozen Meta target")
	}
	targetOnly := runMetaSubmitFixture(t, script, "target-only")
	if targetOnly.Result.State != "clicked" || !targetOnly.Result.TargetOnly || targetOnly.PublishClicks != 1 {
		t.Fatalf("target-only result=%+v", targetOnly)
	}
	for _, mode := range []string{
		"target-changed",
		"unknown-selected",
		"wrong-target-icon",
		"wrong-connected-icon",
		"missing-target-icon",
		"missing-connected-icon",
		"compliance-exposed",
		"caption-changed",
		"media-preview-missing",
		"media-processing",
		"input-only",
		"double-final",
		"outside-final",
		"duplicate-target",
		"duplicate-editor",
		"body-only",
		"wrong-content-control",
	} {
		result := runMetaSubmitFixture(t, script, mode)
		if result.Result.State == "clicked" || result.PublishClicks != 0 {
			t.Fatalf("mode=%s must fail closed at click edge: %+v", mode, result)
		}
	}
	delayed := runMetaSubmitFixture(t, script, "delayed-listbox")
	if delayed.Result.State != "clicked" || delayed.PublishClicks != 1 || delayed.ComboboxClicks != 1 {
		t.Fatalf("delayed click-edge chooser=%+v", delayed)
	}
}

func TestMetaSubmitUsesExactVideoFinalControlInRootlessScope(t *testing.T) {
	script, ok := metaSubmitScript("instagram", platforms.PublishRequest{
		ContentType:              "video",
		Caption:                  "正文",
		Media:                    []platforms.PublishMedia{{Path: "/tmp/video.mp4", Kind: "video", Role: "video"}},
		AssetName:                "chenpaiteng",
		AssetExternalID:          "17841416733781085",
		ConnectedAssetName:       "AlphaIntelligence",
		ConnectedAssetExternalID: "1241410172381837",
	})
	if !ok {
		t.Fatal("expected video submit script for a frozen Meta target")
	}
	ready := runMetaSubmitFixture(t, script, "video-target-only")
	if ready.Result.State != "clicked" || ready.PublishClicks != 1 {
		t.Fatalf("rootless video submit result=%+v", ready)
	}
	wrong := runMetaSubmitFixture(t, script, "video-wrong-content-control")
	if wrong.Result.State == "clicked" || wrong.PublishClicks != 0 {
		t.Fatalf("video must not click a graph publish control: %+v", wrong)
	}
}

func TestMetaFacebookSubmitRechecksComplianceAtClickEdge(t *testing.T) {
	script, ok := metaSubmitScript("facebook", platforms.PublishRequest{
		ContentType:              "graph_text",
		Caption:                  "正文",
		Media:                    []platforms.PublishMedia{{Path: "/tmp/image.png", Kind: "image", Role: "content"}},
		AssetName:                "AlphaIntelligence",
		AssetExternalID:          "1241410172381837",
		ConnectedAssetName:       "chenpaiteng",
		ConnectedAssetExternalID: "17841416733781085",
	})
	if !ok {
		t.Fatal("expected Facebook submit script for frozen connected identities")
	}
	public := runMetaSubmitFixture(t, script, "facebook-public")
	if public.Result.State != "clicked" || public.PublishClicks != 1 {
		t.Fatalf("public compliance result=%+v", public)
	}
	private := runMetaSubmitFixture(t, script, "facebook-private")
	if private.Result.State != "compliance_changed" || private.PublishClicks != 0 {
		t.Fatalf("private compliance must fail at click edge: %+v", private)
	}
}

func TestMetaUnifiedTargetScriptRequiresNumericPublicIdentity(t *testing.T) {
	for _, test := range []struct {
		platform, name, targetID, pageID, connectedName, connectedID string
	}{
		{platform: "instagram", name: "chenpaiteng", targetID: "", pageID: "1241410172381837", connectedName: "AlphaIntelligence", connectedID: "1241410172381837"},
		{platform: "instagram", name: "chenpaiteng", targetID: "ig-id", pageID: "1241410172381837", connectedName: "AlphaIntelligence", connectedID: "1241410172381837"},
		{platform: "instagram", name: "chenpaiteng", targetID: "17841416733781085", pageID: "", connectedName: "AlphaIntelligence", connectedID: "1241410172381837"},
		{platform: "instagram", name: "chenpaiteng", targetID: "17841416733781085", pageID: "1241410172381837", connectedName: "", connectedID: "1241410172381837"},
		{platform: "instagram", name: "chenpaiteng", targetID: "17841416733781085", pageID: "1241410172381837", connectedName: "AlphaIntelligence", connectedID: ""},
		{platform: "facebook", name: "AlphaIntelligence", targetID: "1241410172381837", pageID: "999", connectedName: "chenpaiteng", connectedID: "17841416733781085"},
	} {
		if _, ok := metaUnifiedTargetScript(test.platform, test.name, test.targetID, test.pageID, test.connectedName, test.connectedID); ok {
			t.Fatalf("unexpected script for %+v", test)
		}
	}
}

type unifiedComposerFixtureResult struct {
	Steps          []metaUnifiedTargetState `json:"steps"`
	ComboboxText   string                   `json:"comboboxText"`
	ComboboxClicks int                      `json:"comboboxClicks"`
	Options        []struct {
		Name     string `json:"name"`
		Selected string `json:"selected"`
		Clicks   int    `json:"clicks"`
	} `json:"options"`
}

func runUnifiedComposerFixture(t *testing.T, script string) unifiedComposerFixtureResult {
	return runUnifiedComposerFixtureMode(t, script, "")
}

func runUnifiedComposerFixtureMode(t *testing.T, script, mode string) unifiedComposerFixtureResult {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required by the publish worker test contract: %v", err)
	}
	args := []string{filepath.Join("testdata", "unified_composer_fixture.cjs")}
	if mode != "" {
		args = append(args, mode)
	}
	command := exec.Command(node, args...)
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run unified composer fixture: %v\n%s", err, output)
	}
	var result unifiedComposerFixtureResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode fixture: %v\n%s", err, output)
	}
	return result
}

type metaSubmitFixtureResult struct {
	Result         metaSubmitState `json:"result"`
	PublishClicks  int             `json:"publishClicks"`
	ComboboxClicks int             `json:"comboboxClicks"`
}

func runMetaSubmitFixture(t *testing.T, script, mode string) metaSubmitFixtureResult {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required by the publish worker test contract: %v", err)
	}
	command := exec.Command(node, filepath.Join("testdata", "meta_submit_fixture.cjs"), mode)
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Meta submit fixture: %v\n%s", err, output)
	}
	var result metaSubmitFixtureResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode Meta submit fixture: %v\n%s", err, output)
	}
	return result
}

func assertOnlyMetaFixtureTargetSelected(t *testing.T, options []struct {
	Name     string `json:"name"`
	Selected string `json:"selected"`
	Clicks   int    `json:"clicks"`
}, target string) {
	t.Helper()
	for _, option := range options {
		if option.Name == "保存偏好设置" {
			continue
		}
		want := "false"
		if option.Name == target {
			want = "true"
		}
		if option.Selected != want {
			t.Fatalf("option %q selected=%q want=%q", option.Name, option.Selected, want)
		}
	}
}

func assertMetaPreferenceOptionUntouched(t *testing.T, options []struct {
	Name     string `json:"name"`
	Selected string `json:"selected"`
	Clicks   int    `json:"clicks"`
}) {
	t.Helper()
	for _, option := range options {
		if option.Name != "保存偏好设置" {
			continue
		}
		if option.Selected != "true" || option.Clicks != 0 {
			t.Fatalf("non-asset preference option was changed: %+v", option)
		}
		return
	}
	t.Fatal("preference option missing from fixture")
}
