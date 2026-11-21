// Package publishguard keeps publish entry-point checks consistent before Runner is called.
package publishguard

import (
	"errors"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
)

var (
	ErrBrowserNotRunning           = errors.New("PUBLISH_BROWSER_NOT_RUNNING")
	ErrBrowserDegraded             = errors.New("PUBLISH_BROWSER_DEGRADED")
	ErrBrowserCDPUnavailable       = errors.New("PUBLISH_BROWSER_CDP_UNAVAILABLE")
	ErrBrowserCDPInvalid           = errors.New("PUBLISH_BROWSER_CDP_INVALID")
	ErrBrowserAccountMismatch      = errors.New("PUBLISH_BROWSER_ACCOUNT_MISMATCH")
	ErrTargetPlatformMismatch      = errors.New("PUBLISH_TARGET_PLATFORM_MISMATCH")
	ErrTargetAssetTypeInvalid      = errors.New("PUBLISH_TARGET_ASSET_TYPE_INVALID")
	ErrTargetDisabled              = errors.New("PUBLISH_TARGET_DISABLED")
	ErrTargetAccountNotFound       = errors.New("PUBLISH_TARGET_ACCOUNT_NOT_FOUND")
	ErrTargetOwnerMismatch         = errors.New("PUBLISH_TARGET_OWNER_MISMATCH")
	ErrTargetOwnerPlatformMismatch = errors.New("PUBLISH_TARGET_OWNER_PLATFORM_MISMATCH")
	ErrMediaLookupFailed           = errors.New("PUBLISH_MEDIA_LOOKUP_FAILED")
	ErrMultipleMediaUnsupported    = errors.New("PUBLISH_MULTIPLE_MEDIA_UNSUPPORTED")
)

func ValidateTargetPlatform(asset *db.AccountAsset, platform string) error {
	if asset == nil || strings.TrimSpace(platform) == "" || asset.PlatformCode != platform {
		return ErrTargetPlatformMismatch
	}
	// In-memory test fixtures may omit an ID; persisted targets always have one.
	if asset.ID > 0 && !asset.Enabled {
		return ErrTargetDisabled
	}
	if !platforms.IsPublishTargetAssetType(platform, asset.AssetType) {
		return ErrTargetAssetTypeInvalid
	}
	return nil
}

// ValidateTargetOwnership verifies that the publishing target belongs to a
// local account for the same platform. Meta Business Suite uses one login
// session for independently identified Facebook / Instagram publish targets.
func ValidateTargetOwnership(database *db.DB, asset *db.AccountAsset, platform string) error {
	if err := ValidateTargetPlatform(asset, platform); err != nil {
		return err
	}
	if database == nil {
		return ErrTargetAccountNotFound
	}
	account, err := database.GetAccount(asset.AccountID)
	if err != nil {
		return ErrTargetAccountNotFound
	}
	return ValidateTargetOwner(account, asset, platform)
}

// ValidateTargetOwner is the persistence-free half used by Runner when its
// Store can read the owning account for an older queued job.
func ValidateTargetOwner(account *db.Account, asset *db.AccountAsset, platform string) error {
	if err := ValidateTargetPlatform(asset, platform); err != nil {
		return err
	}
	if account == nil {
		return ErrTargetAccountNotFound
	}
	if account.ID != asset.AccountID {
		return ErrTargetOwnerMismatch
	}
	if !account.Enabled {
		return ErrTargetDisabled
	}
	if account.PlatformCode == platform {
		return nil
	}
	if (account.PlatformCode == "meta" || account.PlatformCode == "facebook" || account.PlatformCode == "instagram") &&
		(platform == "facebook" || platform == "instagram") {
		return nil
	}
	return ErrTargetOwnerPlatformMismatch
}

// SingleMediaPath preserves zero/one-media behavior and rejects silent truncation.
func SingleMediaPath(items []db.MediaAsset, lookupErr error) (string, error) {
	if lookupErr != nil {
		return "", ErrMediaLookupFailed
	}
	if len(items) > 1 {
		return "", ErrMultipleMediaUnsupported
	}
	if len(items) == 0 {
		return "", nil
	}
	if path := strings.TrimSpace(items[0].FilePath); path != "" {
		return path, nil
	}
	return strings.TrimSpace(items[0].OriginalPath), nil
}

// PublishMediaPayload preserves every reviewed content asset. Video content
// uses one video plus an optional image cover; graph-text/article content keeps
// images in order. Ambiguous mixes fail closed instead of silently picking one.
func PublishMediaPayload(items []db.MediaAsset, lookupErr error, contentType string) (string, []platforms.PublishMedia, error) {
	if lookupErr != nil {
		return "", nil, ErrMediaLookupFailed
	}
	if len(items) > 10 {
		return "", nil, ErrMultipleMediaUnsupported
	}
	result := make([]platforms.PublishMedia, 0, len(items))
	primary := ""
	videoCount, coverCount := 0, 0
	for index, item := range items {
		path := strings.TrimSpace(item.FilePath)
		if path == "" {
			path = strings.TrimSpace(item.OriginalPath)
		}
		if path == "" {
			return "", nil, ErrMediaLookupFailed
		}
		role, position := "content", index
		switch contentType {
		case "video":
			switch item.Kind {
			case "video":
				videoCount++
				if videoCount > 1 {
					return "", nil, ErrMultipleMediaUnsupported
				}
				role, position, primary = "video", 0, path
			case "image":
				coverCount++
				if coverCount > 1 {
					return "", nil, ErrMultipleMediaUnsupported
				}
				// Worker positions are unique across roles, not per role.
				role, position = "cover", 1
			default:
				return "", nil, ErrMultipleMediaUnsupported
			}
		case "graph_text", "article":
			if item.Kind != "image" {
				return "", nil, ErrMultipleMediaUnsupported
			}
			if primary == "" {
				primary = path
			}
		default:
			if len(items) > 1 {
				return "", nil, ErrMultipleMediaUnsupported
			}
			primary = path
		}
		result = append(result, platforms.PublishMedia{
			Path: path, Kind: item.Kind, Role: role, Position: position, SHA256: strings.ToLower(item.SHA256),
		})
	}
	if primary == "" && len(result) > 0 {
		primary = result[0].Path
	}
	return primary, result, nil
}

// FailPendingJob closes a retry worker's pending job when preflight rejects it.
func FailPendingJob(database *db.DB, job *db.PublishJob, step, traceID string, failure error, retryable bool) error {
	if database == nil || job == nil || failure == nil {
		return failure
	}
	code := failure.Error()
	if err := database.UpdatePublishJob(job.ID, "failed", step, code, code, job.RetryCount); err != nil {
		return fmt.Errorf("PUBLISH_PREFLIGHT_PERSIST_FAILED: %w", err)
	}
	eventType := "job.permanent_failed"
	if retryable {
		eventType = "job.retryable_failed"
	}
	if traceID == "" {
		traceID = job.TraceID
	}
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "error", Step: step, Type: eventType,
		Message: code, TraceID: traceID, ErrorCode: code, Retryable: retryable,
	}); err != nil {
		return fmt.Errorf("PUBLISH_PREFLIGHT_PERSIST_FAILED: %w", err)
	}
	return failure
}
