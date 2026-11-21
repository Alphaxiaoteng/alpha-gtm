//go:build legacy_chromium

package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// AIChatProvider presets for opening an AI chat UI and sending a message via CDP.
type AIChatProvider string

const (
	AIChatChatGPT AIChatProvider = "chatgpt"
	AIChatClaude  AIChatProvider = "claude"
	AIChatGemini  AIChatProvider = "gemini"
	AIChatCustom  AIChatProvider = "custom"
)

// AIChatRequest drives "open AI window + type message + send".
type AIChatRequest struct {
	CDPURL   string
	Provider AIChatProvider
	// URL overrides provider default when set (or provider=custom).
	URL     string
	Message string
	// Optional CSS selectors (auto-filled from provider defaults).
	InputSelector string
	SendSelector  string // empty → press Enter
	WaitMS        int
}

// AIChatResult is a structured outcome for CLI/API.
type AIChatResult struct {
	OK       bool   `json:"ok"`
	URL      string `json:"url"`
	Provider string `json:"provider"`
	Step     string `json:"step"`
	Message  string `json:"message"`
	Error    string `json:"error,omitempty"`
}

func providerDefaults(p AIChatProvider) (url, input, send string) {
	switch p {
	case AIChatChatGPT:
		// ChatGPT composer — may change; best-effort
		return "https://chatgpt.com/",
			`#prompt-textarea, div[id="prompt-textarea"], textarea[data-id="root"], div.ProseMirror[contenteditable="true"]`,
			`button[data-testid="send-button"], button[aria-label*="Send"], button[aria-label*="发送"]`
	case AIChatClaude:
		return "https://claude.ai/new",
			`div[contenteditable="true"].ProseMirror, div[contenteditable="true"], textarea`,
			`button[aria-label*="Send"], button[aria-label*="发送"]`
	case AIChatGemini:
		return "https://gemini.google.com/app",
			`div.ql-editor[contenteditable="true"], rich-textarea textarea, div[contenteditable="true"]`,
			`button[aria-label*="Send"], button[aria-label*="发送"]`
	default:
		return "", `textarea, div[contenteditable="true"]`, ""
	}
}

// AIBrowserChat opens the AI site in an existing CDP browser and submits a message.
// This controls the AI chat window — not just a blank page screenshot loop.
func AIBrowserChat(ctx context.Context, req AIChatRequest) (*AIChatResult, error) {
	out := &AIChatResult{Provider: string(req.Provider), Message: req.Message}
	if strings.TrimSpace(req.CDPURL) == "" {
		out.Error = "cdp_url required"
		return out, fmt.Errorf("cdp_url required")
	}
	if strings.TrimSpace(req.Message) == "" {
		out.Error = "message required"
		return out, fmt.Errorf("message required")
	}
	p := req.Provider
	if p == "" {
		p = AIChatChatGPT
	}
	defURL, defInput, defSend := providerDefaults(p)
	url := strings.TrimSpace(req.URL)
	if url == "" {
		url = defURL
	}
	if url == "" {
		out.Error = "url required for custom provider"
		return out, fmt.Errorf("url required")
	}
	inputSel := req.InputSelector
	if inputSel == "" {
		inputSel = defInput
	}
	sendSel := req.SendSelector
	if sendSel == "" {
		sendSel = defSend
	}
	out.URL = url

	sess := &BrowserSession{CDPURL: req.CDPURL}
	out.Step = "navigate"
	if err := sess.Navigate(ctx, url); err != nil {
		out.Error = err.Error()
		return out, err
	}
	wait := req.WaitMS
	if wait < 500 {
		wait = 2500
	}
	select {
	case <-ctx.Done():
		out.Error = ctx.Err().Error()
		return out, ctx.Err()
	case <-time.After(time.Duration(wait) * time.Millisecond):
	}

	out.Step = "focus_input"
	// Try each comma-separated selector
	var lastErr error
	focused := false
	for _, sel := range splitSelectors(inputSel) {
		if err := sess.focusAndType(ctx, sel, req.Message); err == nil {
			focused = true
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if !focused {
		out.Error = fmt.Sprintf("input not found: %v", lastErr)
		out.Step = "input_failed"
		return out, lastErr
	}

	out.Step = "send"
	sent := false
	if sendSel != "" {
		for _, sel := range splitSelectors(sendSel) {
			if err := sess.Click(ctx, sel); err == nil {
				sent = true
				break
			}
		}
	}
	if !sent {
		// Enter key on focused element
		if err := sess.pressEnter(ctx); err != nil {
			out.Error = "send failed: " + err.Error()
			return out, err
		}
	}
	out.OK = true
	out.Step = "sent"
	out.Message = "message submitted to AI chat UI"
	return out, nil
}

func splitSelectors(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *BrowserSession) focusAndType(parent context.Context, selector, text string) error {
	ctx, cancel, err := s.withTimeout(parent, 25*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	return chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		// contenteditable: SendKeys after focus; clear may fail — ignore
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

func (s *BrowserSession) pressEnter(parent context.Context) error {
	ctx, cancel, err := s.withTimeout(parent, 10*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	// Keyboard Enter via JS
	return chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement && document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',code:'Enter',keyCode:13,which:13,bubbles:true}))`, nil))
}
