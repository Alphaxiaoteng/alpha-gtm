package copyhumanize

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Match records one rule that changed the text, so the editor can see exactly
// what was rewritten instead of trusting an opaque result.
type Match struct {
	RuleID  string `json:"rule_id"`
	Label   string `json:"label"`
	Why     string `json:"why"`
	Count   int    `json:"count"`
	Example string `json:"example"`
}

// Result is the rewrite plus its audit trail.
type Result struct {
	Text     string  `json:"text"`
	Original string  `json:"original"`
	Matches  []Match `json:"matches"`
	// Changed reports whether any rule fired.
	Changed bool `json:"changed"`
}

// compile validates and compiles every enabled rule.
func compile(rules []Rule) ([]Rule, error) {
	out := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if strings.TrimSpace(rule.Pattern) == "" {
			return nil, fmt.Errorf("rule %q has an empty pattern", rule.ID)
		}
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("rule %q pattern: %w", rule.ID, err)
		}
		rule.compiled = re
		out = append(out, rule)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no enabled rules")
	}
	return out, nil
}

// Apply rewrites text with the given rules.
//
// Each rule runs once, in order: a later rule sees the earlier rule's output but
// no rule loops, so the result is deterministic and every change can be traced
// to one rule.
func Apply(text string, rules []Rule) (Result, error) {
	compiled, err := compile(rules)
	if err != nil {
		return Result{}, err
	}
	result := Result{Original: text, Text: text}
	for _, rule := range compiled {
		matches := rule.compiled.FindAllString(result.Text, -1)
		if len(matches) == 0 {
			continue
		}
		next := rule.compiled.ReplaceAllString(result.Text, rule.Replace)
		if next == result.Text {
			continue
		}
		result.Matches = append(result.Matches, Match{
			RuleID: rule.ID, Label: rule.Label, Why: rule.Why,
			Count: len(matches), Example: truncate(matches[0], 60),
		})
		result.Text = next
	}
	result.Text = strings.TrimSpace(result.Text)
	result.Changed = result.Text != strings.TrimSpace(result.Original)
	return result, nil
}

// ApplyDefault rewrites with the shipped rule pack.
func ApplyDefault(text string) (Result, error) { return Apply(text, DefaultRules()) }

func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

// LoadRules reads a rule file. An absent file yields the defaults, so a fresh
// install works without setup; an unreadable or malformed file is an error the
// caller can surface rather than silently falling back.
func LoadRules(path string) ([]Rule, error) {
	if strings.TrimSpace(path) == "" {
		return DefaultRules(), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultRules(), nil
		}
		return nil, fmt.Errorf("read copy rules: %w", err)
	}
	var rules []Rule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("parse copy rules: %w", err)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("copy rules file is empty")
	}
	return rules, nil
}

// SaveRules writes rules as indented JSON, creating the file 0600 because rule
// text may quote the operator's own drafting.
func SaveRules(path string, rules []Rule) error {
	clean := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		rule.compiled = nil
		clean = append(clean, rule)
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return fmt.Errorf("encode copy rules: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write copy rules: %w", err)
	}
	return nil
}

// Validate compiles every enabled rule without touching text, so the editor can
// reject a bad pattern before it reaches a document.
func Validate(rules []Rule) error {
	_, err := compile(rules)
	return err
}

// RuleIDs returns the stable identifiers in order, for tests and diagnostics.
func RuleIDs(rules []Rule) []string {
	ids := make([]string, 0, len(rules))
	for _, rule := range rules {
		ids = append(ids, rule.ID)
	}
	sort.Strings(ids)
	return ids
}
