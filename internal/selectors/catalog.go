// Package selectors owns Alpha-Nexus' versioned browser automation catalog.
//
// It intentionally contains only self-authored, public DOM heuristics. It does
// not copy Turbo Push configuration and never stores values of cookies/tokens.
package selectors

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed catalog.v1.json
var embeddedCatalog []byte

type Rule struct {
	Query        string `json:"query,omitempty"`
	TextContains string `json:"text_contains,omitempty"`
}

type PlatformCatalog struct {
	URLs                []string          `json:"urls"`
	LoggedInCookieNames []string          `json:"logged_in_cookie_names"`
	LoginIndicators     []Rule            `json:"login_indicators"`
	Operations          map[string][]Rule `json:"operations"`
}

type Catalog struct {
	SchemaVersion  int                        `json:"schema_version"`
	CatalogVersion int                        `json:"catalog_version"`
	Platforms      map[string]PlatformCatalog `json:"platforms"`
}

var (
	defaultOnce sync.Once
	defaultCat  *Catalog
	defaultErr  error
)

// Default returns the embedded, validated catalog. A malformed catalog is a
// programming/deployment error; callers receive a nil catalog instead of a
// permissive partial configuration.
func Default() (*Catalog, error) {
	defaultOnce.Do(func() {
		var c Catalog
		if err := json.Unmarshal(embeddedCatalog, &c); err != nil {
			defaultErr = fmt.Errorf("decode selector catalog: %w", err)
			return
		}
		if err := c.Validate(); err != nil {
			defaultErr = err
			return
		}
		defaultCat = &c
	})
	return defaultCat, defaultErr
}

func (c *Catalog) Platform(code string) (PlatformCatalog, bool) {
	if c == nil {
		return PlatformCatalog{}, false
	}
	p, ok := c.Platforms[strings.ToLower(strings.TrimSpace(code))]
	return p, ok
}

func (c *Catalog) URLs(code string) []string {
	p, ok := c.Platform(code)
	if !ok {
		return nil
	}
	return append([]string(nil), p.URLs...)
}

// OperationRules returns a defensive copy of self-authored candidate rules.
// Rules identify controls only; their presence is not evidence that an action
// succeeded, and callers must verify resulting DOM state independently.
func (c *Catalog) OperationRules(code, operation string) []Rule {
	p, ok := c.Platform(code)
	if !ok {
		return nil
	}
	rules := p.Operations[strings.ToLower(strings.TrimSpace(operation))]
	if len(rules) == 0 {
		return nil
	}
	return append([]Rule(nil), rules...)
}

// CookiesIndicateLogin compares only cookie names, never values.
func (c *Catalog) CookiesIndicateLogin(code string, names []string) bool {
	p, ok := c.Platform(code)
	if !ok || len(p.LoggedInCookieNames) == 0 {
		return false
	}
	want := make(map[string]struct{}, len(p.LoggedInCookieNames))
	for _, name := range p.LoggedInCookieNames {
		want[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range names {
		if _, ok := want[strings.ToLower(name)]; ok {
			return true
		}
	}
	return false
}

// HasLoginCookieEvidence reports whether a platform has a reviewed, exact
// cookie-name contract. Platforms without one require manual verification.
func (c *Catalog) HasLoginCookieEvidence(code string) bool {
	p, ok := c.Platform(code)
	return ok && len(p.LoggedInCookieNames) > 0
}

// LoginScreenScript returns a read-only browser expression. The JSON encoded
// rules prevent unsafe JavaScript interpolation when the catalog evolves.
func (c *Catalog) LoginScreenScript(code string) (string, error) {
	p, ok := c.Platform(code)
	if !ok {
		return "", fmt.Errorf("selector catalog has no platform %q", code)
	}
	rules, err := json.Marshal(p.LoginIndicators)
	if err != nil {
		return "", fmt.Errorf("encode login indicators: %w", err)
	}
	return `(() => {
  try {
    const rules = ` + string(rules) + `;
    const bodyText = (document.body?.innerText || '').toLowerCase();
    const matched = rules.some((rule) => {
      if (rule.query && document.querySelector(rule.query)) return true;
      return Boolean(rule.text_contains && bodyText.includes(rule.text_contains.toLowerCase()));
    });
    return JSON.stringify({ isLoginScreen: matched ? "true" : "false" });
  } catch (_) {
    return JSON.stringify({ isLoginScreen: "false" });
  }
})()`, nil
}

func (c *Catalog) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("unsupported selector catalog schema version %d", c.SchemaVersion)
	}
	if c.CatalogVersion <= 0 {
		return fmt.Errorf("selector catalog version must be positive")
	}
	if len(c.Platforms) == 0 {
		return fmt.Errorf("selector catalog has no platforms")
	}
	for code, p := range c.Platforms {
		if strings.TrimSpace(code) == "" || len(p.URLs) == 0 {
			return fmt.Errorf("selector catalog platform %q requires URLs", code)
		}
		for _, rule := range p.LoginIndicators {
			if strings.TrimSpace(rule.Query) == "" && strings.TrimSpace(rule.TextContains) == "" {
				return fmt.Errorf("selector catalog platform %q has empty login rule", code)
			}
		}
		for operation, rules := range p.Operations {
			if strings.TrimSpace(operation) == "" || len(rules) == 0 {
				return fmt.Errorf("selector catalog platform %q has invalid operation %q", code, operation)
			}
			for _, rule := range rules {
				if strings.TrimSpace(rule.Query) == "" && strings.TrimSpace(rule.TextContains) == "" {
					return fmt.Errorf("selector catalog platform %q operation %q has empty rule", code, operation)
				}
			}
		}
	}
	return nil
}
