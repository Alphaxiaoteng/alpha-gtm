package copyhumanize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRewritesTypicalMachineCopy(t *testing.T) {
	input := "在当今快速发展的时代，众所周知，内容为王。我们不仅提升效率，而且降低成本。综上所述，这就是答案。"

	result, err := ApplyDefault(input)
	if err != nil {
		t.Fatalf("ApplyDefault: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected the text to change")
	}
	for _, banned := range []string{"在当今", "众所周知", "不仅", "综上所述"} {
		if strings.Contains(result.Text, banned) {
			t.Errorf("%q should be gone, got %q", banned, result.Text)
		}
	}
	if !strings.Contains(result.Text, "内容为王") {
		t.Errorf("substantive text must survive, got %q", result.Text)
	}
}

func TestApplyReportsEveryRuleThatFired(t *testing.T) {
	result, err := ApplyDefault("赋能团队，打造闭环，抓住抓手。")
	if err != nil {
		t.Fatalf("ApplyDefault: %v", err)
	}
	fired := map[string]bool{}
	for _, match := range result.Matches {
		fired[match.RuleID] = true
		if match.Why == "" {
			t.Errorf("rule %q must explain itself", match.RuleID)
		}
		if match.Count < 1 {
			t.Errorf("rule %q count = %d", match.RuleID, match.Count)
		}
	}
	for _, want := range []string{"drop-buzzword-enable", "drop-buzzword-closed-loop", "drop-buzzword-grip"} {
		if !fired[want] {
			t.Errorf("rule %q should have fired; got %v", want, fired)
		}
	}
}

func TestApplyIsIdempotentOnCleanText(t *testing.T) {
	clean := "这条视频的剪辑从四十分钟压到十二分钟。"
	result, err := ApplyDefault(clean)
	if err != nil {
		t.Fatalf("ApplyDefault: %v", err)
	}
	if result.Changed {
		t.Errorf("clean copy should be untouched, got %q", result.Text)
	}
	if len(result.Matches) != 0 {
		t.Errorf("no rule should fire, got %+v", result.Matches)
	}
}

func TestApplyIsStableAcrossRuns(t *testing.T) {
	input := "首先，我们要赋能业务。其次，形成闭环。"
	first, err := ApplyDefault(input)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := ApplyDefault(input)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.Text != second.Text {
		t.Errorf("non-deterministic rewrite: %q vs %q", first.Text, second.Text)
	}
}

func TestDisabledRuleDoesNothing(t *testing.T) {
	// 一条启用、一条禁用：禁用的那条不能生效。
	rules := []Rule{
		{ID: "on", Label: "启用", Why: "保留", Enabled: true, Pattern: "保留", Replace: "留住"},
		{ID: "off", Label: "禁用", Why: "不该跑", Enabled: false, Pattern: "测试", Replace: "X"},
	}
	result, err := Apply("这是一段测试文本，需要保留", rules)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.Contains(result.Text, "X") {
		t.Errorf("disabled rule must not run, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "留住") {
		t.Errorf("enabled rule should have run, got %q", result.Text)
	}
}

func TestCustomRuleRuns(t *testing.T) {
	rules := []Rule{{
		ID: "brand-term", Label: "品牌词", Why: "统一写法", Enabled: true,
		Pattern: "阿尔法", Replace: "Alpha",
	}}
	result, err := Apply("阿尔法工具很好用", rules)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Text != "Alpha工具很好用" {
		t.Errorf("got %q", result.Text)
	}
}

func TestInvalidPatternIsRejected(t *testing.T) {
	rules := []Rule{{ID: "bad", Enabled: true, Pattern: "([", Replace: ""}}
	if _, err := Apply("x", rules); err == nil {
		t.Fatal("invalid regex should fail")
	}
	if err := Validate(rules); err == nil {
		t.Fatal("Validate should reject an invalid pattern")
	}
}

func TestNoEnabledRulesIsAnError(t *testing.T) {
	if _, err := Apply("x", []Rule{{ID: "a", Enabled: false, Pattern: "x"}}); err == nil {
		t.Fatal("no enabled rules should be an error, not a silent no-op")
	}
}

func TestLoadRulesFallsBackToDefaultsWhenAbsent(t *testing.T) {
	rules, err := LoadRules(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected default rules")
	}
}

func TestLoadRulesRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadRules(path); err == nil {
		t.Fatal("malformed file should be reported, not silently ignored")
	}
}

func TestSavedRulesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	rules := []Rule{{
		ID: "only", Label: "唯一", Why: "测试往返", Enabled: true,
		Pattern: "旧", Replace: "新",
	}}
	if err := SaveRules(path, rules); err != nil {
		t.Fatalf("SaveRules: %v", err)
	}
	loaded, err := LoadRules(path)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "only" {
		t.Fatalf("round trip lost data: %+v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %v, want 0600", perm)
	}
}

func TestSavedRulesOmitCompiledState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := SaveRules(path, DefaultRules()); err != nil {
		t.Fatalf("SaveRules: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded []map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, rule := range decoded {
		if _, leaked := rule["compiled"]; leaked {
			t.Error("compiled regexp must not be serialised")
		}
	}
}

func TestDefaultRulesHaveUniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, rule := range DefaultRules() {
		if seen[rule.ID] {
			t.Errorf("duplicate rule id %q", rule.ID)
		}
		seen[rule.ID] = true
		if rule.Label == "" || rule.Why == "" {
			t.Errorf("rule %q needs a label and a reason", rule.ID)
		}
	}
}

func TestDefaultRulesCompile(t *testing.T) {
	if err := Validate(DefaultRules()); err != nil {
		t.Fatalf("shipped rules must compile: %v", err)
	}
}

// 删除整句后不能留下行首标点：这是编辑器最容易忽略的副作用。
func TestNoLeadingPunctuationAfterDeletion(t *testing.T) {
	result, err := ApplyDefault("你是否也在为选题发愁？——其实很简单——从数据出发。")
	if err != nil {
		t.Fatalf("ApplyDefault: %v", err)
	}
	if strings.HasPrefix(result.Text, "，") || strings.HasPrefix(result.Text, ",") {
		t.Errorf("text must not start with punctuation, got %q", result.Text)
	}
	if strings.Contains(result.Text, "——") {
		t.Errorf("em dashes should be folded, got %q", result.Text)
	}
}

// 问句只在行首才算开场。带引子的问句要保留引子。
func TestQuestionOpenerRequiresLineStart(t *testing.T) {
	result, err := ApplyDefault("今天想聊聊一个话题：你是否也遇到过这种情况？我把它拆成三步。")
	if err != nil {
		t.Fatalf("ApplyDefault: %v", err)
	}
	if !strings.Contains(result.Text, "今天想聊聊一个话题") {
		t.Errorf("a real lead-in must survive, got %q", result.Text)
	}
	for _, match := range result.Matches {
		if match.RuleID == "drop-question-opener" {
			t.Error("mid-line question must not be treated as an opener")
		}
	}
}

// 中文列表常用冒号开头，不能被当成删句残留。
func TestLeadingColonIsNotStripped(t *testing.T) {
	rules := []Rule{{ID: "trim-leading-punct", Label: "清理行首标点", Why: "测试", Enabled: true,
		Pattern: `(?m)^[，,、；;]\s*`, Replace: ""}}
	result, err := Apply("清单：\n：冒号开头的行", rules)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.Contains(result.Text, "：冒号开头的行") {
		t.Errorf("leading colon must survive, got %q", result.Text)
	}
}
