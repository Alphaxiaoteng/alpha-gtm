package platforms

import (
	"context"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/selectors"
)

// xhsPublisher is deliberately a verified handoff adapter. It validates the
// immutable settings contract and opens the exact creator console, but never
// claims that dynamic controls or submission succeeded without live evidence.
type xhsPublisher struct{}

func (p *xhsPublisher) Platform() string { return "xhs" }

func (p *xhsPublisher) Strategy() PublishStrategy { return StrategyBrowser }

func (p *xhsPublisher) Validate(req PublishRequest) error {
	if strings.TrimSpace(req.Caption) == "" {
		return fmt.Errorf("caption required")
	}
	if strings.TrimSpace(req.SettingsJSON) == "" {
		return nil
	}
	snapshot, err := ParsePublishSettingsSnapshot(req.SettingsJSON)
	if err != nil {
		return err
	}
	if snapshot.PlatformCode != "xhs" || snapshot.AdapterVersion != XHSBrowserAdapterVersion {
		return fmt.Errorf("unsupported xhs publish settings snapshot")
	}
	catalog, err := selectors.Default()
	if err != nil {
		return err
	}
	if snapshot.SelectorCatalogVersion != catalog.CatalogVersion {
		return fmt.Errorf("xhs selector catalog version mismatch")
	}
	return nil
}

func (p *xhsPublisher) Publish(ctx context.Context, req PublishRequest) (*PublishResult, error) {
	if err := p.Validate(req); err != nil {
		return &PublishResult{
			Status: "permanent_failed", Step: "preflight", ErrorCode: "VALIDATION_FAILED", ErrorMessage: err.Error(),
		}, err
	}
	if req.DryRun {
		return &PublishResult{
			Status: "dry_run_ok", Step: "preflight", Message: "dry-run ok: xhs settings snapshot validated",
		}, nil
	}
	if strings.TrimSpace(req.CDPURL) == "" {
		return &PublishResult{
			Status: "retryable_failed", Step: "browser_attached", ErrorCode: string(errors.CDP_CONNECT),
			ErrorMessage: "CDP URL empty; start browser for account first",
		}, fmt.Errorf("%s: empty cdp url", errors.CDP_CONNECT)
	}
	if err := OpenURL(ctx, req.CDPURL, creatorURLXHS); err != nil {
		return &PublishResult{
			Status: "retryable_failed", Step: "browser_attached", ErrorCode: string(errors.CDP_CONNECT), ErrorMessage: err.Error(),
		}, err
	}
	return &PublishResult{
		Status: "manual_takeover", Step: "open_creator",
		Message: "小红书创作后台已打开；模板设置已记录为任务快照，请人工确认后发布",
	}, nil
}
