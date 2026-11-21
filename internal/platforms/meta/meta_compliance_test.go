//go:build legacy_chromium

package meta

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestValidateMetaFrozenComplianceRequiresLockedFormalPolicy(t *testing.T) {
	raw := formalMetaComplianceJSON(t)
	required, evidence, err := validateMetaFrozenCompliance(raw, "前言\n"+metaFormalAIDisclosure+"\n#标签")
	if err != nil || !required {
		t.Fatalf("required=%v evidence=%+v err=%v", required, evidence, err)
	}
	if evidence["ai_disclosure"] != "exact_caption_text" ||
		evidence["retention"] != "keep" ||
		evidence["frozen_policy"] != "validated" {
		t.Fatalf("evidence=%+v", evidence)
	}
}

func TestMetaFacebookComplianceScriptReadsAdOffAndPublicPrivacy(t *testing.T) {
	script := metaFacebookComplianceReadbackScript()
	if strings.Contains(script, ".click()") || strings.Contains(script, "document.body") {
		t.Fatalf("compliance readback must be passive: %s", script)
	}
	state := runMetaComplianceFixture(t, script, "facebook_public")
	if !state.OK || state.State != "verified" || state.CommercialContent != "false" || state.Visibility != "public" {
		t.Fatalf("state=%+v", state)
	}
	for _, mode := range []string{"facebook_ad_true", "facebook_private"} {
		if state := runMetaComplianceFixture(t, script, mode); state.OK {
			t.Fatalf("mode=%s must fail closed: %+v", mode, state)
		}
	}
	for _, mode := range []string{"duplicate_target", "duplicate_editor", "body_only"} {
		if state := runMetaComplianceFixture(t, script, mode); state.OK {
			t.Fatalf("unsafe rootless scope mode=%s must fail closed: %+v", mode, state)
		}
	}
}

func TestMetaInstagramComplianceScriptOnlyAcceptsNoExposedControls(t *testing.T) {
	script := metaInstagramComplianceAbsenceScript()
	if strings.Contains(script, ".click()") || strings.Contains(script, "document.body") {
		t.Fatalf("Instagram observation must be passive: %s", script)
	}
	state := runMetaComplianceFixture(t, script, "instagram_empty")
	if !state.OK || state.State != "not_exposed_in_composer" {
		t.Fatalf("state=%+v", state)
	}
	if state := runMetaComplianceFixture(t, script, "facebook_public"); state.OK {
		t.Fatalf("newly exposed controls must require review: %+v", state)
	}
	for _, mode := range []string{
		"composer_missing",
		"instagram_unknown_control",
		"duplicate_target",
		"duplicate_editor",
		"body_only",
	} {
		if state := runMetaComplianceFixture(t, script, mode); state.OK {
			t.Fatalf("mode=%s must fail closed: %+v", mode, state)
		}
	}
}

func TestMetaLiveComplianceEvidenceDoesNotClaimUnexposedInstagramControlsWereApplied(t *testing.T) {
	evidence, ok := metaLiveComplianceEvidence("instagram", metaLiveComplianceState{
		OK:                true,
		State:             "not_exposed_in_composer",
		CommercialContent: "not_exposed_in_composer",
		Visibility:        "not_exposed_in_composer",
	})
	if !ok ||
		evidence["ai_generated_control"] != "not_exposed_in_composer" ||
		evidence["commercial_content_control"] != "not_exposed_in_composer" ||
		evidence["visibility_control"] != "not_exposed_in_composer" ||
		evidence["comments_control"] != "not_exposed_in_composer" ||
		evidence["visibility_verification"] != "public_professional_asset_and_post_publish_public_url_required" {
		t.Fatalf("evidence=%+v ok=%v", evidence, ok)
	}
}

func TestPublishRejectsInvalidFrozenComplianceBeforeBrowserAttach(t *testing.T) {
	publisher := &Publisher{platform: "facebook"}
	result, err := publisher.Publish(context.Background(), platforms.PublishRequest{
		ContentID:       1,
		AssetID:         2,
		AssetType:       "facebook_page",
		AssetName:       "AlphaIntelligence",
		AssetExternalID: "1241410172381837",
		Caption:         "missing the frozen exact disclosure",
		ComplianceJSON:  formalMetaComplianceJSON(t),
	})
	if err == nil || result == nil || result.Status != "permanent_failed" ||
		result.Step != StepPreflight || result.ErrorCode != "META_COMPLIANCE_INVALID" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPublishValidFrozenComplianceContinuesToBrowserPreflight(t *testing.T) {
	publisher := &Publisher{platform: "facebook"}
	result, err := publisher.Publish(context.Background(), platforms.PublishRequest{
		ContentID:       1,
		AssetID:         2,
		AssetType:       "facebook_page",
		AssetName:       "AlphaIntelligence",
		AssetExternalID: "1241410172381837",
		Caption:         metaFormalAIDisclosure,
		ComplianceJSON:  formalMetaComplianceJSON(t),
	})
	if err == nil || result == nil || result.Step != StepBrowserAttach {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPublishAutoSubmitRequiresFrozenComplianceBeforeBrowserAttach(t *testing.T) {
	publisher := &Publisher{platform: "facebook"}
	result, err := publisher.Publish(context.Background(), platforms.PublishRequest{
		ContentID:       1,
		AssetID:         2,
		AssetType:       "facebook_page",
		AssetName:       "AlphaIntelligence",
		AssetExternalID: "1241410172381837",
		Caption:         "legacy caption",
		AutoSubmit:      true,
	})
	if err == nil || result == nil || result.Status != "permanent_failed" ||
		result.Step != StepPreflight || result.ErrorCode != "META_COMPLIANCE_REQUIRED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPublishPrepareAllowsLegacyEmptyCompliance(t *testing.T) {
	publisher := &Publisher{platform: "facebook"}
	result, err := publisher.Publish(context.Background(), platforms.PublishRequest{
		ContentID:       1,
		AssetID:         2,
		AssetType:       "facebook_page",
		AssetName:       "AlphaIntelligence",
		AssetExternalID: "1241410172381837",
		Caption:         "legacy caption",
	})
	if err == nil || result == nil || result.Step != StepBrowserAttach {
		t.Fatalf("legacy prepare should reach browser preflight: result=%+v err=%v", result, err)
	}
}

func TestValidateMetaFrozenComplianceKeepsLegacyEmptyInputCompatible(t *testing.T) {
	required, evidence, err := validateMetaFrozenCompliance("", "legacy caption")
	if err != nil || required || evidence != nil {
		t.Fatalf("required=%v evidence=%+v err=%v", required, evidence, err)
	}
}

func TestValidateMetaFrozenComplianceFailsClosedBeforeLiveControls(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func([]map[string]any) []map[string]any
		caption string
	}{
		{
			name: "missing disclosure field",
			mutate: func(fields []map[string]any) []map[string]any {
				return fields[1:]
			},
			caption: metaFormalAIDisclosure,
		},
		{
			name: "ai false",
			mutate: func(fields []map[string]any) []map[string]any {
				fields[0]["value"] = false
				return fields
			},
			caption: metaFormalAIDisclosure,
		},
		{
			name: "commercial true",
			mutate: func(fields []map[string]any) []map[string]any {
				fields[2]["value"] = true
				return fields
			},
			caption: metaFormalAIDisclosure,
		},
		{
			name: "private",
			mutate: func(fields []map[string]any) []map[string]any {
				fields[3]["value"] = "private"
				return fields
			},
			caption: metaFormalAIDisclosure,
		},
		{
			name: "comments disabled",
			mutate: func(fields []map[string]any) []map[string]any {
				fields[4]["value"] = false
				return fields
			},
			caption: metaFormalAIDisclosure,
		},
		{
			name: "delete retention",
			mutate: func(fields []map[string]any) []map[string]any {
				fields[9]["value"] = "delete"
				return fields
			},
			caption: metaFormalAIDisclosure,
		},
		{
			name:    "caption missing exact disclosure",
			mutate:  func(fields []map[string]any) []map[string]any { return fields },
			caption: "AI 生成，但不是冻结的 exact 披露",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.mutate(formalMetaComplianceFields()))
			if err != nil {
				t.Fatal(err)
			}
			if required, _, err := validateMetaFrozenCompliance(string(encoded), test.caption); err == nil || required {
				t.Fatalf("required=%v err=%v", required, err)
			}
		})
	}
}

func TestValidateMetaFrozenComplianceRejectsMalformedOrDuplicateFields(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`[{"policy_key":"ai_generated","value":true,"status":"manual"},{"policy_key":"ai_generated","value":true,"status":"manual"}]`,
		`[{"policy_key":"unknown","value":true,"status":"manual"}]`,
	} {
		if required, _, err := validateMetaFrozenCompliance(raw, metaFormalAIDisclosure); err == nil || required {
			t.Fatalf("raw=%s required=%v err=%v", raw, required, err)
		}
	}
}

func formalMetaComplianceJSON(t *testing.T) string {
	t.Helper()
	encoded, err := json.Marshal(formalMetaComplianceFields())
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func formalMetaComplianceFields() []map[string]any {
	return []map[string]any{
		{"policy_key": "ai_generated", "platform_key": "ai_generated", "value": true, "status": "manual"},
		{"policy_key": "ai_disclosure", "value": metaFormalAIDisclosure, "status": "applied"},
		{"policy_key": "commercial_content", "platform_key": "commercial_content", "value": false, "status": "manual"},
		{"policy_key": "visibility", "platform_key": "visibility", "value": "public", "status": "manual"},
		{"policy_key": "comments", "platform_key": "comment_enabled", "value": true, "status": "manual"},
		{"policy_key": "download", "value": false, "status": "unsupported"},
		{"policy_key": "duet", "value": false, "status": "unsupported"},
		{"policy_key": "stitch", "value": false, "status": "unsupported"},
		{"policy_key": "youtube_audience", "value": "not_made_for_kids", "status": "unsupported"},
		{"policy_key": "retention", "value": "keep", "status": "applied"},
	}
}

func runMetaComplianceFixture(t *testing.T, script, mode string) metaLiveComplianceState {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required by the publish worker test contract: %v", err)
	}
	command := exec.Command(node, filepath.Join("testdata", "meta_compliance_fixture.cjs"), mode)
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Meta compliance fixture: %v\n%s", err, output)
	}
	var state metaLiveComplianceState
	if err := json.Unmarshal(output, &state); err != nil {
		t.Fatalf("decode Meta compliance fixture: %v\n%s", err, output)
	}
	return state
}
