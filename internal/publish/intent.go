package publish

import (
	"strings"

	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/selectors"
)

const (
	ModeDryRun  = "dry_run"
	ModePrepare = "prepare"
	ModeSubmit  = "submit"
)

// ExecutionMode is the durable publish intent. It prevents a retry from
// changing a rehearsal or review-only run into an automatic submit.
func ExecutionMode(dryRun, autoSubmit bool) string {
	if dryRun {
		return ModeDryRun
	}
	if autoSubmit {
		return ModeSubmit
	}
	return ModePrepare
}

// IntentFromMode accepts legacy mode values as review-only for safe upgrades.
func IntentFromMode(mode string) (dryRun, autoSubmit bool) {
	switch mode {
	case ModeDryRun:
		return true, false
	case ModeSubmit, "auto_submit":
		return false, true
	default:
		return false, false
	}
}

// DefaultSettingsSnapshot returns the deterministic, non-secret snapshot for
// a new job when no platform template was selected.
func DefaultSettingsSnapshot(platformCode string) (string, error) {
	if strings.ToLower(strings.TrimSpace(platformCode)) != "xhs" {
		return "", nil
	}
	catalog, err := selectors.Default()
	if err != nil {
		return "", err
	}
	return platforms.BuildPublishSettingsSnapshot(
		"xhs", 0, "{}", catalog.CatalogVersion, platforms.XHSBrowserAdapterVersion,
	)
}
