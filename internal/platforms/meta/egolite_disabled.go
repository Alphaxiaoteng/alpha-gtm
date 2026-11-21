//go:build !legacy_chromium

// Package meta keeps only a fail-closed compatibility adapter in the EgoLite
// product. Meta login, discovery, preparation and submission are performed by
// reviewed Ego Space tools rather than the historical CDP publisher.
package meta

import (
	"context"
	"fmt"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
)

type Publisher struct {
	platform string
}

func (publisher *Publisher) Platform() string { return publisher.platform }

func (publisher *Publisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}

func (publisher *Publisher) Validate(request platforms.PublishRequest) error {
	if request.PlatformCode != publisher.platform {
		return fmt.Errorf("META_PLATFORM_MISMATCH")
	}
	return nil
}

func (publisher *Publisher) Publish(_ context.Context, request platforms.PublishRequest) (*platforms.PublishResult, error) {
	if err := publisher.Validate(request); err != nil {
		return nil, err
	}
	if request.DryRun {
		return &platforms.PublishResult{
			Status:  "dry_run_ok",
			Step:    "dry_run",
			Message: "legacy Meta request validated without opening a browser",
		}, nil
	}
	err := fmt.Errorf("PUBLISH_LEGACY_CHROMIUM_DISABLED")
	return &platforms.PublishResult{
		Status:       "retryable_failed",
		Step:         "ego_runtime_required",
		ErrorCode:    "PUBLISH_LEGACY_CHROMIUM_DISABLED",
		ErrorMessage: err.Error(),
	}, err
}

type DiscoverResult struct {
	FacebookPages     []string `json:"facebook_pages"`
	InstagramAccounts []string `json:"instagram_accounts"`
	Errors            []string `json:"errors,omitempty"`
	Success           bool     `json:"success"`
}

func Discover(_ context.Context, _ string, _ int64, _ *db.DB) (*DiscoverResult, error) {
	return nil, fmt.Errorf("EGO_RUNTIME_REQUIRED")
}

func init() {
	platforms.RegisterPublisher(&Publisher{platform: "instagram"})
	platforms.RegisterPublisher(&Publisher{platform: "facebook"})
}
