package platforms

import (
	"context"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/errors"
)

// Creator / admin URLs used when opening browser for manual takeover.
const (
	creatorURLDouyin      = "https://creator.douyin.com"
	creatorURLXHS         = "https://creator.xiaohongshu.com"
	creatorURLWechatVideo = "https://channels.weixin.qq.com"
	creatorURLKuaishou    = "https://cp.kuaishou.com"
	creatorURLWechatMP    = "https://mp.weixin.qq.com"
)

func init() {
	// Browser strategy (Lite creators): open creator URL → manual_takeover.
	RegisterPublisher(&browserPublisher{code: "douyin", creatorURL: creatorURLDouyin})
	RegisterPublisher(&xhsPublisher{})
	RegisterPublisher(&browserPublisher{code: "wechat_video", creatorURL: creatorURLWechatVideo})
	RegisterPublisher(&browserPublisher{code: "kuaishou", creatorURL: creatorURLKuaishou})

	// Hybrid: open MP admin → manual_takeover.
	RegisterPublisher(&browserPublisher{code: "wechat_mp", creatorURL: creatorURLWechatMP, hybrid: true})

	// Legacy direct adapter strategy: dry-run only until native connectors are
	// implemented. Canonical YouTube/TikTok plan execution is selected by
	// ExecutionPublisher and uses the reviewed browser worker; X stays here.
	RegisterPublisher(&apiPublisher{code: "youtube"})
	RegisterPublisher(&apiPublisher{code: "tiktok"})
	RegisterPublisher(&apiPublisher{code: "x"})

	// Every built-in creator with a known management URL gets a functional
	// handoff path. It opens the account's already-running browser at the
	// creator console and records manual_takeover; it never clicks Publish.
	// Meta remains reserved for its dedicated browser publisher, registered by
	// the meta package's init function.
	known := map[string]bool{
		"douyin": true, "xhs": true, "wechat_video": true, "kuaishou": true,
		"wechat_mp": true, "youtube": true, "tiktok": true, "x": true,
		"instagram": true, "facebook": true,
	}
	for _, platform := range All() {
		if known[platform.Code] || strings.TrimSpace(platform.ManagerURL) == "" {
			continue
		}
		RegisterPublisher(&browserPublisher{
			code: platform.Code, creatorURL: platform.ManagerURL, manualOnly: true,
		})
	}
}

// browserPublisher handles browser (and hybrid) open-creator → manual_takeover.
type browserPublisher struct {
	code       string
	creatorURL string
	hybrid     bool
	manualOnly bool
}

func (p *browserPublisher) Platform() string { return p.code }

func (p *browserPublisher) Strategy() PublishStrategy {
	if p.hybrid {
		return StrategyHybrid
	}
	return StrategyBrowser
}

func (p *browserPublisher) Validate(req PublishRequest) error {
	if _, schemaExists := SettingsSchemaFor(p.code); schemaExists && strings.TrimSpace(req.SettingsJSON) != "" {
		snapshot, err := ParsePublishSettingsSnapshot(req.SettingsJSON)
		if err != nil {
			return err
		}
		adapterVersion, _ := PublishSettingsAdapterVersion(p.code)
		if snapshot.PlatformCode != p.code || snapshot.AdapterVersion != adapterVersion {
			return fmt.Errorf("unsupported %s publish settings snapshot", p.code)
		}
	}
	if p.manualOnly {
		if strings.TrimSpace(req.Title) == "" && strings.TrimSpace(req.Caption) == "" && strings.TrimSpace(req.Body) == "" {
			return fmt.Errorf("title, caption, or body required")
		}
		return nil
	}
	// Hybrid dry-run is lenient on content fields; browser requires caption.
	if p.hybrid {
		return nil
	}
	if strings.TrimSpace(req.Caption) == "" {
		return fmt.Errorf("caption required")
	}
	return nil
}

func (p *browserPublisher) Publish(ctx context.Context, req PublishRequest) (*PublishResult, error) {
	if err := p.Validate(req); err != nil {
		return &PublishResult{
			Status:       "permanent_failed",
			Step:         "preflight",
			ErrorCode:    "VALIDATION_FAILED",
			ErrorMessage: err.Error(),
		}, err
	}

	if req.DryRun {
		return &PublishResult{
			Status:  "dry_run_ok",
			Step:    "preflight",
			Message: fmt.Sprintf("dry-run ok: %s (strategy=%s)", p.code, p.Strategy()),
		}, nil
	}

	if strings.TrimSpace(req.CDPURL) == "" {
		return &PublishResult{
			Status:       "retryable_failed",
			Step:         "browser_attached",
			ErrorCode:    string(errors.CDP_CONNECT),
			ErrorMessage: "CDP URL empty; start browser for account first",
		}, fmt.Errorf("%s: empty cdp url", errors.CDP_CONNECT)
	}

	if err := OpenURL(ctx, req.CDPURL, p.creatorURL); err != nil {
		return &PublishResult{
			Status:       "retryable_failed",
			Step:         "browser_attached",
			ErrorCode:    string(errors.CDP_CONNECT),
			ErrorMessage: err.Error(),
		}, err
	}

	return &PublishResult{
		Status:  "manual_takeover",
		Step:    "open_creator",
		Message: manualHandoffMessage(p.creatorURL, p.manualOnly, req.AutoSubmit),
	}, nil
}

func manualHandoffMessage(creatorURL string, manualOnly, autoSubmit bool) string {
	if manualOnly && autoSubmit {
		return fmt.Sprintf("%s opened; automatic submit is unavailable for this platform, complete publish manually", creatorURL)
	}
	return fmt.Sprintf("%s opened; complete publish manually", creatorURL)
}

// apiPublisher reserves the native API surface for YouTube / TikTok / X.
// It always fails closed for live calls. YouTube/TikTok's separately reviewed
// browser path is available only through ExecutionPublisher; X has no such path.
type apiPublisher struct {
	code string
}

func (p *apiPublisher) Platform() string { return p.code }

func (p *apiPublisher) Strategy() PublishStrategy { return StrategyAPI }

func (p *apiPublisher) Validate(req PublishRequest) error {
	if strings.TrimSpace(req.Caption) == "" && strings.TrimSpace(req.Title) == "" {
		return fmt.Errorf("caption or title required")
	}
	return nil
}

func (p *apiPublisher) Publish(_ context.Context, req PublishRequest) (*PublishResult, error) {
	if err := p.Validate(req); err != nil {
		return &PublishResult{
			Status:       "permanent_failed",
			Step:         "preflight",
			ErrorCode:    "VALIDATION_FAILED",
			ErrorMessage: err.Error(),
		}, err
	}

	if req.DryRun {
		return &PublishResult{
			Status:  "dry_run_ok",
			Step:    "preflight",
			Message: fmt.Sprintf("dry-run ok: %s (api; credentials not checked)", p.code),
		}, nil
	}

	msg := fmt.Sprintf("native API connector is unavailable for platform %q", p.code)
	code := string(errors.API_CONNECTOR_UNAVAILABLE)
	return &PublishResult{
		Status:       "permanent_failed",
		Step:         "preflight",
		ErrorCode:    code,
		ErrorMessage: msg,
	}, fmt.Errorf("%s: %s", code, msg)
}
