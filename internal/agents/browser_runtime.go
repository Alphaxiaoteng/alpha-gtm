//go:build legacy_chromium

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/trace"
)

// newBrowserPlannerGateway 使浏览器规划测试可明确阻断远程模型请求。
var newBrowserPlannerGateway = llm.NewDoubaoGateway

// PageSnapshot is a compact DOM/text view for the LLM (not full HTML dump).
type PageSnapshot struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"` // truncated visible text
}

// BrowserAction is one restricted action the BrowserAgent may emit.
type BrowserAction struct {
	Op       string `json:"op"` // snapshot | navigate | click | type | wait | done | takeover
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
	MS       int    `json:"ms,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// BrowserSession talks to an already-running CDP endpoint (fingerprint-chromium).
type BrowserSession struct {
	CDPURL string
}

func (s *BrowserSession) withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc, error) {
	if strings.TrimSpace(s.CDPURL) == "" {
		return nil, nil, fmt.Errorf("empty CDP URL")
	}
	alloc, cancelAlloc := chromedp.NewRemoteAllocator(parent, s.CDPURL)
	ctx, cancelCtx := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, d)
	cancel := func() {
		cancelTimeout()
		cancelCtx()
		cancelAlloc()
	}
	return ctx, cancel, nil
}

func (s *BrowserSession) Snapshot(parent context.Context) (*PageSnapshot, error) {
	ctx, cancel, err := s.withTimeout(parent, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()

	var url, title, text string
	err = chromedp.Run(ctx,
		chromedp.Location(&url),
		chromedp.Title(&title),
		chromedp.Evaluate(`(() => {
  const t = (document.body && document.body.innerText) ? document.body.innerText : '';
  return t.replace(/\s+/g, ' ').trim().slice(0, 2500);
})()`, &text),
	)
	if err != nil {
		return nil, err
	}
	return &PageSnapshot{URL: url, Title: title, Text: text}, nil
}

func (s *BrowserSession) Navigate(parent context.Context, url string) error {
	ctx, cancel, err := s.withTimeout(parent, 45*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	return chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
	)
}

func (s *BrowserSession) Click(parent context.Context, selector string) error {
	ctx, cancel, err := s.withTimeout(parent, 20*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	return chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	)
}

func (s *BrowserSession) Type(parent context.Context, selector, text string) error {
	ctx, cancel, err := s.withTimeout(parent, 20*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	return chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

func (s *BrowserSession) Screenshot(parent context.Context, dir, tag string) (string, error) {
	ctx, cancel, err := s.withTimeout(parent, 20*time.Second)
	if err != nil {
		return "", err
	}
	defer cancel()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("agent-browser-%s-%d.png", tag, time.Now().Unix()))
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// planNext asks LLM for a single BrowserAction JSON given goal + snapshot.
func planNext(ctx context.Context, goal string, snap *PageSnapshot, step int) (*BrowserAction, string, error) {
	sys := `You control a browser via restricted tools. Reply with ONE JSON object only, no markdown:
{"op":"navigate|click|type|wait|snapshot|done|takeover","url":"...","selector":"css","text":"...","ms":500,"reason":"..."}
Rules:
- Prefer takeover if login/captcha/2FA/unclear.
- click/type need a CSS selector.
- done when goal appears achieved.
- wait ms between 200 and 5000.
- Never invent credentials.`
	user := fmt.Sprintf("step=%d\ngoal=%s\nurl=%s\ntitle=%s\ntext=%s",
		step, goal, snap.URL, snap.Title, snap.Text)
	// Bound LLM wait so offline/hanging proxies fall through to local URL heuristics quickly.
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	raw := ""
	if gateway := newBrowserPlannerGateway(5 * time.Second); gateway != nil {
		raw, _ = gateway.Complete(cctx, sys, user)
	}
	raw = strings.TrimSpace(raw)
	// extract JSON object if model wraps text
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var act BrowserAction
	if err := json.Unmarshal([]byte(raw), &act); err != nil {
		// fallback when LLM offline / non-JSON: navigate if goal is a URL
		g := strings.TrimSpace(goal)
		if strings.HasPrefix(g, "http://") || strings.HasPrefix(g, "https://") {
			// re-navigate until page leaves about:blank or max steps
			if step <= 2 || strings.HasPrefix(snap.URL, "about:") || snap.URL == "" {
				return &BrowserAction{Op: "navigate", URL: g, Reason: "local fallback navigate"}, raw, nil
			}
			return &BrowserAction{Op: "done", Reason: "local fallback: goal URL reached or navigated", URL: g}, raw, nil
		}
		return &BrowserAction{Op: "takeover", Reason: "LLM parse failed; need human"}, raw, nil
	}
	act.Op = strings.ToLower(strings.TrimSpace(act.Op))
	return &act, raw, nil
}

// BrowserAgent runs a bounded AI loop on an existing CDP session (PRD PublishAgent browser tools).
// Does NOT start browsers; attach via CDPURL from Facade/status.
func BrowserAgent(ctx context.Context, ac *Context, sess *BrowserSession, goal, screenshotDir string) (*Result, error) {
	if ac == nil {
		ac = &Context{MaxSteps: 8, AllowedTools: DefaultTools(), TraceID: trace.New()}
	}
	if ac.MaxSteps <= 0 {
		ac.MaxSteps = 8
	}
	if !ac.Allow(ToolBrowserAttach) && !ac.Allow(ToolBrowserSnapshot) {
		return &Result{Agent: "BrowserAgent", OK: false, Output: "browser tools not allowed", TraceID: string(ac.TraceID)}, nil
	}

	var log []string
	var lastShot string

	for step := 1; step <= ac.MaxSteps; step++ {
		if err := ac.Step(fmt.Sprintf("browser.step.%d", step)); err != nil {
			return &Result{Agent: "BrowserAgent", OK: false, Output: err.Error(), TraceID: string(ac.TraceID)}, err
		}
		if !ac.Allow(ToolBrowserSnapshot) {
			return &Result{Agent: "BrowserAgent", OK: false, Output: "browser.snapshot not allowed", TraceID: string(ac.TraceID)}, nil
		}
		snap, err := sess.Snapshot(ctx)
		if err != nil {
			log = append(log, fmt.Sprintf("snapshot_error: %v", err))
			return &Result{
				Agent: "BrowserAgent", OK: false, Output: strings.Join(log, "\n"),
				Data:    map[string]string{"error": err.Error(), "status": "retryable_failed"},
				TraceID: string(ac.TraceID),
			}, err
		}
		log = append(log, fmt.Sprintf("snap url=%s title=%s", snap.URL, snap.Title))

		act, raw, _ := planNext(ctx, goal, snap, step)
		log = append(log, fmt.Sprintf("plan op=%s reason=%s raw=%s", act.Op, act.Reason, truncate(raw, 200)))

		switch act.Op {
		case "done":
			if screenshotDir != "" {
				if p, err := sess.Screenshot(ctx, screenshotDir, "done"); err == nil {
					lastShot = p
				}
			}
			return &Result{
				Agent: "BrowserAgent", OK: true, Output: strings.Join(log, "\n"),
				Data:    map[string]string{"status": "done", "url": snap.URL, "screenshot": lastShot, "reason": act.Reason},
				TraceID: string(ac.TraceID),
			}, nil
		case "takeover":
			if screenshotDir != "" {
				if p, err := sess.Screenshot(ctx, screenshotDir, "takeover"); err == nil {
					lastShot = p
				}
			}
			return &Result{
				Agent: "BrowserAgent", OK: true, Output: strings.Join(log, "\n"),
				Data:    map[string]string{"status": "manual_takeover", "url": snap.URL, "screenshot": lastShot, "reason": act.Reason},
				TraceID: string(ac.TraceID),
			}, nil
		case "navigate":
			if !ac.Allow(ToolBrowserAction) {
				return failTool(ac, log, "browser.action not allowed")
			}
			if act.URL == "" {
				log = append(log, "navigate missing url → takeover")
				return &Result{Agent: "BrowserAgent", OK: true, Output: strings.Join(log, "\n"),
					Data: map[string]string{"status": "manual_takeover", "reason": "missing url"}, TraceID: string(ac.TraceID)}, nil
			}
			if err := sess.Navigate(ctx, act.URL); err != nil {
				log = append(log, "navigate_error: "+err.Error())
			} else {
				// settle after navigation
				select {
				case <-ctx.Done():
					return &Result{Agent: "BrowserAgent", OK: false, Output: strings.Join(log, "\n"), TraceID: string(ac.TraceID)}, ctx.Err()
				case <-time.After(800 * time.Millisecond):
				}
				log = append(log, "navigate_ok: "+act.URL)
			}
		case "click":
			if !ac.Allow(ToolBrowserAction) {
				return failTool(ac, log, "browser.action not allowed")
			}
			if act.Selector == "" {
				return &Result{Agent: "BrowserAgent", OK: true, Output: strings.Join(log, "\n"),
					Data: map[string]string{"status": "manual_takeover", "reason": "missing selector"}, TraceID: string(ac.TraceID)}, nil
			}
			if err := sess.Click(ctx, act.Selector); err != nil {
				log = append(log, "click_error: "+err.Error())
			}
		case "type":
			if !ac.Allow(ToolBrowserAction) {
				return failTool(ac, log, "browser.action not allowed")
			}
			if act.Selector == "" {
				return &Result{Agent: "BrowserAgent", OK: true, Output: strings.Join(log, "\n"),
					Data: map[string]string{"status": "manual_takeover", "reason": "missing selector"}, TraceID: string(ac.TraceID)}, nil
			}
			if err := sess.Type(ctx, act.Selector, act.Text); err != nil {
				log = append(log, "type_error: "+err.Error())
			}
		case "wait", "snapshot":
			ms := act.MS
			if ms < 200 {
				ms = 500
			}
			if ms > 5000 {
				ms = 5000
			}
			select {
			case <-ctx.Done():
				return &Result{Agent: "BrowserAgent", OK: false, Output: strings.Join(log, "\n"), TraceID: string(ac.TraceID)}, ctx.Err()
			case <-time.After(time.Duration(ms) * time.Millisecond):
			}
		default:
			log = append(log, "unknown op → takeover")
			return &Result{Agent: "BrowserAgent", OK: true, Output: strings.Join(log, "\n"),
				Data: map[string]string{"status": "manual_takeover", "reason": "unknown op " + act.Op}, TraceID: string(ac.TraceID)}, nil
		}
	}

	if screenshotDir != "" {
		if p, err := sess.Screenshot(ctx, screenshotDir, "maxsteps"); err == nil {
			lastShot = p
		}
	}
	return &Result{
		Agent: "BrowserAgent", OK: true, Output: strings.Join(log, "\n"),
		Data:    map[string]string{"status": "manual_takeover", "reason": "max steps", "screenshot": lastShot},
		TraceID: string(ac.TraceID),
	}, nil
}

func failTool(ac *Context, log []string, msg string) (*Result, error) {
	log = append(log, msg)
	return &Result{Agent: "BrowserAgent", OK: false, Output: strings.Join(log, "\n"), TraceID: string(ac.TraceID)}, fmt.Errorf("%s", msg)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
