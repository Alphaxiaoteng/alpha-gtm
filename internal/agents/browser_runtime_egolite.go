//go:build !legacy_chromium

package agents

import (
	"context"
	"fmt"
)

// PageSnapshot and BrowserAction remain as compatibility data types for old
// callers. The EgoLite product never attaches to CDP and never executes them.
type PageSnapshot struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type BrowserAction struct {
	Op       string `json:"op"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
	MS       int    `json:"ms,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// BrowserSession is deliberately inert in the default EgoLite build.
type BrowserSession struct {
	CDPURL string
}

func (s *BrowserSession) Snapshot(_ context.Context) (*PageSnapshot, error) {
	return nil, fmt.Errorf("EGO_RUNTIME_REQUIRED")
}

func BrowserAgent(_ context.Context, ac *Context, _ *BrowserSession, _ string, _ string) (*Result, error) {
	err := fmt.Errorf("EGO_RUNTIME_REQUIRED")
	traceID := ""
	if ac != nil {
		traceID = string(ac.TraceID)
	}
	return &Result{
		Agent:   "BrowserAgent",
		OK:      false,
		Output:  "legacy CDP BrowserAgent is disabled; use reviewed EgoLite Registry tools",
		TraceID: traceID,
	}, err
}

type AIChatProvider string

const (
	AIChatChatGPT AIChatProvider = "chatgpt"
	AIChatClaude  AIChatProvider = "claude"
	AIChatGemini  AIChatProvider = "gemini"
	AIChatCustom  AIChatProvider = "custom"
)

type AIChatRequest struct {
	CDPURL        string
	Provider      AIChatProvider
	URL           string
	Message       string
	InputSelector string
	SendSelector  string
	WaitMS        int
}

type AIChatResult struct {
	OK       bool   `json:"ok"`
	URL      string `json:"url"`
	Provider string `json:"provider"`
	Step     string `json:"step"`
	Message  string `json:"message"`
	Error    string `json:"error,omitempty"`
}

func AIBrowserChat(_ context.Context, req AIChatRequest) (*AIChatResult, error) {
	err := fmt.Errorf("EGO_RUNTIME_REQUIRED")
	return &AIChatResult{
		Provider: string(req.Provider),
		URL:      req.URL,
		Step:     "disabled",
		Message:  "legacy CDP AI chat is disabled in Alpha-Nexus EgoLite",
		Error:    "EGO_RUNTIME_REQUIRED",
	}, err
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
