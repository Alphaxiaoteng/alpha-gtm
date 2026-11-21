package publishplan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
)

func validFrozenSnapshotHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	return strings.Trim(value, "0123456789abcdef") == ""
}

// FrozenJobInput is reconstructed only from the immutable publish-plan
// snapshot. Live content rows are deliberately not consulted during retry.
type FrozenJobInput struct {
	Content        *db.Content
	Asset          *db.AccountAsset
	MediaPath      string
	Media          []platforms.PublishMedia
	PublishAt      string
	ComplianceJSON string
}

func frozenComplianceJSON(fields []ComplianceField) (string, error) {
	if len(fields) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("PUBLISH_FROZEN_COMPLIANCE_INVALID")
	}
	return string(encoded), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

// FrozenInputForJob verifies every persisted link before returning reviewed
// content and media. Any plan mutation or on-disk media replacement fails
// closed instead of silently changing a retry.
func (s *Service) FrozenInputForJob(job *db.PublishJob) (*FrozenJobInput, error) {
	if s == nil || s.DB == nil || job == nil || job.PlanID <= 0 || job.PlanTargetID <= 0 || !validFrozenSnapshotHash(job.SnapshotHash) {
		return nil, fmt.Errorf("PUBLISH_FROZEN_SNAPSHOT_REQUIRED")
	}
	plan, err := s.Get(job.PlanID)
	if err != nil || plan.SnapshotHash != job.SnapshotHash {
		return nil, fmt.Errorf("PUBLISH_FROZEN_SNAPSHOT_MISMATCH")
	}
	if err := validateFrozenTargetIdentity(s.DB, plan); err != nil {
		return nil, err
	}
	var persistedJobID int64
	var accountID, assetID int64
	var platformCode, settingsJSON string
	err = s.DB.QueryRow(`SELECT COALESCE(publish_job_id,0), account_id, asset_id, platform_code, settings_json
		FROM publish_plan_targets WHERE id=? AND plan_id=?`,
		job.PlanTargetID, job.PlanID).
		Scan(&persistedJobID, &accountID, &assetID, &platformCode, &settingsJSON)
	if err != nil || persistedJobID != job.ID || accountID != job.AccountID || assetID != job.AssetID ||
		platformCode != job.PlatformCode || settingsJSON != job.SettingsJSON {
		return nil, fmt.Errorf("PUBLISH_FROZEN_TARGET_MISMATCH")
	}
	var target *TargetSnapshot
	for index := range plan.Snapshot.Targets {
		candidate := &plan.Snapshot.Targets[index]
		if candidate.AccountID == job.AccountID && candidate.AssetID == job.AssetID && candidate.PlatformCode == job.PlatformCode {
			target = candidate
			break
		}
	}
	if target == nil || plan.ContentID != job.ContentID || plan.MediaMode != job.ContentType || target.SettingsJSON != job.SettingsJSON {
		return nil, fmt.Errorf("PUBLISH_FROZEN_JOB_MISMATCH")
	}
	complianceJSON, err := frozenComplianceJSON(target.ComplianceFields)
	if err != nil {
		return nil, err
	}
	asset, err := s.DB.GetAccountAsset(job.AssetID)
	if err != nil || asset.AccountID != job.AccountID || asset.PlatformCode != job.PlatformCode {
		return nil, fmt.Errorf("PUBLISH_FROZEN_TARGET_UNAVAILABLE")
	}
	publishMedia := planMedia(plan.Snapshot)
	mediaPath := ""
	for index := range plan.Snapshot.Media {
		item := plan.Snapshot.Media[index]
		actualHash, hashErr := fileSHA256(item.FilePath)
		if hashErr != nil || actualHash != item.SHA256 {
			return nil, fmt.Errorf("PUBLISH_FROZEN_MEDIA_MISMATCH")
		}
		if mediaPath == "" && item.Role != "cover" {
			mediaPath = item.FilePath
		}
	}
	content := &db.Content{
		ID: job.ContentID, Type: plan.MediaMode, Title: target.Title,
		Caption: target.Caption, Body: target.Body, TagsJSON: target.TagsJSON,
	}
	return &FrozenJobInput{
		Content: content, Asset: asset, MediaPath: mediaPath,
		Media: publishMedia, PublishAt: target.PublishAt, ComplianceJSON: complianceJSON,
	}, nil
}
