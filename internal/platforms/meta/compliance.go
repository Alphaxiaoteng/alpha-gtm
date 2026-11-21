//go:build legacy_chromium

package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chromedp/chromedp"
)

const metaFormalAIDisclosure = "本条封面与动态视觉由 AI 生成，仅用于品牌视觉展示，不是真实游戏画面，也不包含商业合作。"

type metaFrozenComplianceField struct {
	PolicyKey   string          `json:"policy_key"`
	PlatformKey string          `json:"platform_key,omitempty"`
	Value       json.RawMessage `json:"value"`
	Status      string          `json:"status"`
	Reason      string          `json:"reason,omitempty"`
}

type metaExpectedComplianceField struct {
	PlatformKey string
	Value       any
	Status      string
}

type metaLiveComplianceState struct {
	OK                bool   `json:"ok"`
	State             string `json:"state"`
	CommercialContent string `json:"commercialContent"`
	Visibility        string `json:"visibility"`
}

func validateMetaFrozenCompliance(raw, caption string) (bool, map[string]interface{}, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var fields []metaFrozenComplianceField
	if err := decoder.Decode(&fields); err != nil {
		return false, nil, fmt.Errorf("META_COMPLIANCE_INVALID")
	}
	if err := ensureMetaComplianceJSONEOF(decoder); err != nil {
		return false, nil, err
	}

	expected := map[string]metaExpectedComplianceField{
		"ai_generated":       {PlatformKey: "ai_generated", Value: true, Status: "manual"},
		"ai_disclosure":      {Value: metaFormalAIDisclosure, Status: "applied"},
		"commercial_content": {PlatformKey: "commercial_content", Value: false, Status: "manual"},
		"visibility":         {PlatformKey: "visibility", Value: "public", Status: "manual"},
		"comments":           {PlatformKey: "comment_enabled", Value: true, Status: "manual"},
		"download":           {Value: false, Status: "unsupported"},
		"duet":               {Value: false, Status: "unsupported"},
		"stitch":             {Value: false, Status: "unsupported"},
		"youtube_audience":   {Value: "not_made_for_kids", Status: "unsupported"},
		"retention":          {Value: "keep", Status: "applied"},
	}
	if len(fields) != len(expected) {
		return false, nil, fmt.Errorf("META_COMPLIANCE_INVALID")
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		want, ok := expected[field.PolicyKey]
		if !ok {
			return false, nil, fmt.Errorf("META_COMPLIANCE_INVALID")
		}
		if _, duplicate := seen[field.PolicyKey]; duplicate {
			return false, nil, fmt.Errorf("META_COMPLIANCE_INVALID")
		}
		seen[field.PolicyKey] = struct{}{}
		if field.PlatformKey != want.PlatformKey || field.Status != want.Status ||
			!metaComplianceValueMatches(field.Value, want.Value) {
			return false, nil, fmt.Errorf("META_COMPLIANCE_INVALID")
		}
	}
	if !strings.Contains(caption, metaFormalAIDisclosure) {
		return false, nil, fmt.Errorf("META_COMPLIANCE_DISCLOSURE_MISSING")
	}
	return true, map[string]interface{}{
		"frozen_policy": "validated",
		"ai_disclosure": "exact_caption_text",
		"retention":     "keep",
	}, nil
}

func ensureMetaComplianceJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("META_COMPLIANCE_INVALID")
	}
	return nil
}

func metaComplianceValueMatches(raw json.RawMessage, expected any) bool {
	switch want := expected.(type) {
	case bool:
		var got bool
		return json.Unmarshal(raw, &got) == nil && got == want &&
			(strings.TrimSpace(string(raw)) == "true" || strings.TrimSpace(string(raw)) == "false")
	case string:
		var got string
		return json.Unmarshal(raw, &got) == nil && got == want
	default:
		return false
	}
}

func mergeMetaEvidence(groups ...map[string]interface{}) map[string]interface{} {
	var merged map[string]interface{}
	for _, group := range groups {
		for key, value := range group {
			if merged == nil {
				merged = make(map[string]interface{})
			}
			merged[key] = value
		}
	}
	return merged
}

func metaFacebookComplianceReadbackScript() string {
	return `(() => {
		  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
		  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
		  const fail = state => ({ ok: false, state, commercialContent: 'unverified', visibility: 'unverified' });
		  if (location.hostname !== 'business.facebook.com' || !location.pathname.startsWith('/latest/composer')) return fail('wrong_surface');
` + metaComposerScopeResolverJS + `
		  const root = resolveComposerScope();
		  if (!root) return fail('composer_scope_unconfirmed');
		  const contextText = node => {
    const candidates = [node, node && node.closest ? node.closest('label') : null, node && node.parentElement];
    return normalize(Array.from(new Set(candidates.filter(Boolean))).map(candidate =>
      candidate.innerText || candidate.textContent || candidate.getAttribute('aria-label') || candidate.getAttribute('title') || ''
    ).join(' '));
  };
  const checked = node => {
    const aria = node.getAttribute('aria-checked');
    if (aria === 'true' || aria === 'false') return aria;
    if (typeof node.checked === 'boolean') return node.checked ? 'true' : 'false';
    return 'unknown';
  };
	  const allControls = Array.from(root.querySelectorAll(
	    '[role="switch"], [role="radio"], [role="checkbox"], input[type="radio"]'
	  )).filter(visible);
	  const adSwitches = allControls.filter(node => {
	    if (node.getAttribute('role') !== 'switch') return false;
	    if (!visible(node)) return false;
	    const text = contextText(node);
	    return text.includes('创建为广告帖') || text.includes('create as ad post');
  });
  if (adSwitches.length !== 1) return fail('commercial_control_unconfirmed');
  if (checked(adSwitches[0]) !== 'false') return fail('commercial_content_not_false');

	  const radios = allControls.filter(node => node.getAttribute('role') === 'radio' || String(node.tagName || '').toUpperCase() === 'INPUT');
  const publicRadios = radios.filter(node => {
    const text = contextText(node);
    return (text.includes('公开') && text.includes('任何人都能看到')) ||
      (text.includes('public') && (text.includes('anyone') || text.includes('everyone')));
  });
  const specificRadios = radios.filter(node => {
    const text = contextText(node);
    return text.includes('特定分享对象') || text.includes('specific audience');
  });
  if (publicRadios.length !== 1 || specificRadios.length !== 1) return fail('privacy_controls_unconfirmed');
  if (checked(publicRadios[0]) !== 'true' || checked(specificRadios[0]) !== 'false') return fail('visibility_not_public');
	  const expectedControls = new Set([adSwitches[0], publicRadios[0], specificRadios[0]]);
	  if (allControls.some(node => !expectedControls.has(node))) return fail('unsupported_control_exposed');
	  return { ok: true, state: 'verified', commercialContent: 'false', visibility: 'public' };
	})()`
}

func metaInstagramComplianceAbsenceScript() string {
	return `(() => {
		  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
		  const fail = state => ({ ok: false, state, commercialContent: 'unverified', visibility: 'unverified' });
		  if (location.hostname !== 'business.facebook.com' || !location.pathname.startsWith('/latest/composer')) return fail('wrong_surface');
` + metaComposerScopeResolverJS + `
		  const root = resolveComposerScope();
		  if (!root) return fail('composer_scope_unconfirmed');
		  const exposed = Array.from(root.querySelectorAll(
		    '[role="switch"], [role="radio"], [role="checkbox"], input[type="radio"]'
	  )).some(visible);
	  if (exposed) return fail('unexpected_compliance_control');
	  return { ok: true, state: 'not_exposed_in_composer', commercialContent: 'not_exposed_in_composer', visibility: 'not_exposed_in_composer' };
	})()`
}

func observeMetaLiveCompliance(ctx context.Context, platform string) (map[string]interface{}, bool) {
	var script string
	switch platform {
	case "facebook":
		script = metaFacebookComplianceReadbackScript()
	case "instagram":
		script = metaInstagramComplianceAbsenceScript()
	default:
		return nil, false
	}
	var state metaLiveComplianceState
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &state)); err != nil || !state.OK {
		return nil, false
	}
	return metaLiveComplianceEvidence(platform, state)
}

func metaLiveComplianceEvidence(platform string, state metaLiveComplianceState) (map[string]interface{}, bool) {
	base := map[string]interface{}{
		"ai_generated_control": "not_exposed_in_composer",
		"comments_control":     "not_exposed_in_composer",
	}
	switch platform {
	case "instagram":
		if !state.OK || state.State != "not_exposed_in_composer" ||
			state.CommercialContent != "not_exposed_in_composer" ||
			state.Visibility != "not_exposed_in_composer" {
			return nil, false
		}
		base["commercial_content_control"] = "not_exposed_in_composer"
		base["visibility_control"] = "not_exposed_in_composer"
		base["visibility_verification"] = "public_professional_asset_and_post_publish_public_url_required"
	case "facebook":
		if !state.OK || state.State != "verified" ||
			state.CommercialContent != "false" || state.Visibility != "public" {
			return nil, false
		}
		base["commercial_content_control"] = "verified_false"
		base["visibility_control"] = "verified_public"
		base["visibility_verification"] = "post_publish_public_url_required"
	default:
		return nil, false
	}
	return base, true
}
