package publishplan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publishguard"
)

const (
	StatusFrozen             = "frozen"
	StatusApproved           = "approved"
	StatusAwaitingReview     = "awaiting_review"
	PublishPlanActionPrepare = "prepare"
	PublishPlanActionSubmit  = "submit"
	// PublishPlanActionAcceptanceSubmit is a local, single-target bootstrap
	// approval used to collect the first real adapter acceptance evidence. It
	// remains distinct from ordinary submit approval and never changes the
	// global verified capability flags by itself.
	PublishPlanActionAcceptanceSubmit = "acceptance_submit"
	PublishPlanSnapshotVersion        = 3
	minPublishLead                    = 15 * time.Minute
)

var supportedPlanPlatforms = func() map[string]bool {
	result := map[string]bool{"meta": true}
	for _, manifest := range egopublish.EgoLiteManifests() {
		result[manifest.PlatformCode] = true
	}
	return result
}()

func supportsMediaMode(platformCode, mediaMode string) bool {
	manifest, ok := egopublish.ManifestFor(platformCode)
	if !ok {
		return false
	}
	for _, contentType := range manifest.ContentTypes() {
		if contentType == mediaMode {
			return true
		}
	}
	return false
}

func SupportsNativeSchedule(platformCode, mediaMode string) bool {
	return egopublish.NativeScheduleSupportedFor(platformCode, mediaMode)
}

type Service struct {
	DB             *db.DB
	Now            func() time.Time
	CoverExtractor func(string, string) (*media.FileMeta, error)
}

type FreezeInput struct {
	ContentID        int64             `json:"content_id"`
	MediaMode        string            `json:"media_mode"`
	MediaIDs         []int64           `json:"media_ids"`
	CoverMediaID     int64             `json:"cover_media_id,omitempty"`
	NativeVideoCover bool              `json:"native_video_cover,omitempty"`
	PublishAt        string            `json:"publish_at,omitempty"`
	CompliancePolicy *CompliancePolicy `json:"compliance_policy,omitempty"`
	Concurrency      int               `json:"concurrency,omitempty"`
	Targets          []TargetInput     `json:"targets"`
	StudioHandoffID  int64             `json:"-"`
}

type FreezeHandoffInput struct {
	HandoffID        int64             `json:"handoff_id"`
	Targets          []TargetInput     `json:"targets"`
	CompliancePolicy *CompliancePolicy `json:"compliance_policy,omitempty"`
	Concurrency      int               `json:"concurrency,omitempty"`
}

type TargetInput struct {
	AccountID    int64  `json:"account_id"`
	AssetID      int64  `json:"asset_id,omitempty"`
	VariantID    int64  `json:"variant_id,omitempty"`
	SettingsJSON string `json:"settings_json,omitempty"`
}

type ContentSnapshot struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Caption   string `json:"caption"`
	Body      string `json:"body"`
	TagsJSON  string `json:"tags_json"`
	PlannedAt string `json:"planned_at,omitempty"`
}

type MediaSnapshot struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	FilePath   string `json:"file_path"`
	MIMEType   string `json:"mime_type"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int    `json:"duration_ms"`
	Position   int    `json:"position"`
	Role       string `json:"role"`
}

type TargetSnapshot struct {
	AccountID                  int64             `json:"account_id"`
	AccountPlatformCode        string            `json:"account_platform_code"`
	AssetID                    int64             `json:"asset_id"`
	PlatformCode               string            `json:"platform_code"`
	AssetType                  string            `json:"asset_type"`
	AssetName                  string            `json:"asset_name"`
	AssetExternalID            string            `json:"asset_external_id"`
	AssetEnabled               bool              `json:"asset_enabled"`
	ConnectedAssetID           int64             `json:"connected_asset_id,omitempty"`
	ConnectedAssetPlatformCode string            `json:"connected_asset_platform_code,omitempty"`
	ConnectedAssetType         string            `json:"connected_asset_type,omitempty"`
	ConnectedAssetName         string            `json:"connected_asset_name,omitempty"`
	ConnectedAssetExternalID   string            `json:"connected_asset_external_id,omitempty"`
	ConnectedAssetEnabled      bool              `json:"connected_asset_enabled,omitempty"`
	VariantID                  int64             `json:"variant_id,omitempty"`
	Title                      string            `json:"title"`
	Caption                    string            `json:"caption"`
	Body                       string            `json:"body"`
	TagsJSON                   string            `json:"tags_json"`
	SettingsJSON               string            `json:"settings_json"`
	ComplianceFields           []ComplianceField `json:"compliance_fields,omitempty"`
	PublishAt                  string            `json:"publish_at,omitempty"`
}

type Snapshot struct {
	Version          int               `json:"version"`
	Content          ContentSnapshot   `json:"content"`
	MediaMode        string            `json:"media_mode"`
	Media            []MediaSnapshot   `json:"media"`
	Targets          []TargetSnapshot  `json:"targets"`
	CompliancePolicy *CompliancePolicy `json:"compliance_policy,omitempty"`
	Concurrency      int               `json:"concurrency"`
}

type Plan struct {
	ID              int64         `json:"id"`
	ContentID       int64         `json:"contentId"`
	MediaMode       string        `json:"mediaMode"`
	Snapshot        Snapshot      `json:"snapshot"`
	SnapshotHash    string        `json:"snapshotHash"`
	Status          string        `json:"status"`
	Concurrency     int           `json:"concurrency"`
	CreatedAt       string        `json:"createdAt"`
	FrozenAt        string        `json:"frozenAt"`
	ApprovedAt      string        `json:"approvedAt,omitempty"`
	ApprovedAction  string        `json:"approvedAction,omitempty"`
	StudioHandoffID int64         `json:"studioHandoffId,omitempty"`
	TargetStates    []TargetState `json:"targetStates,omitempty"`
}

// TargetState is the compact, non-sensitive execution state shown after a
// one-key attempt. The immutable snapshot remains the source for content.
type TargetState struct {
	AccountID       int64  `json:"accountId"`
	AssetID         int64  `json:"assetId"`
	PlatformCode    string `json:"platformCode"`
	JobID           int64  `json:"jobId,omitempty"`
	Status          string `json:"status"`
	ErrorCode       string `json:"errorCode,omitempty"`
	SubmitAttempted bool   `json:"submitAttempted"`
}

// PlanSummary is the safe list surface used by the local UI. Frozen bodies,
// settings, media paths, hashes, and account identifiers remain available only
// from the explicit single-plan review endpoint.
type PlanSummary struct {
	ID            int64    `json:"id"`
	ContentID     int64    `json:"contentId"`
	ContentTitle  string   `json:"contentTitle"`
	MediaMode     string   `json:"mediaMode"`
	Status        string   `json:"status"`
	TargetCount   int      `json:"targetCount"`
	PlatformCodes []string `json:"platformCodes"`
	CreatedAt     string   `json:"createdAt"`
}

type ApprovalGrant struct {
	ID        string `json:"approvalId"`
	Nonce     string `json:"nonce"`
	ExpiresAt string `json:"expiresAt"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func normalizePublishTime(raw string, now time.Time) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", fmt.Errorf("planned publish time must be RFC3339")
	}
	value = value.UTC()
	if value.Before(now.UTC().Add(minPublishLead)) {
		return "", fmt.Errorf("planned publish time must be at least 15 minutes in the future")
	}
	return value.Format(time.RFC3339), nil
}

func canonicalTags(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "[]"
	}
	var tags []interface{}
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return "", fmt.Errorf("tags must be a JSON array")
	}
	encoded, _ := json.Marshal(tags)
	return string(encoded), nil
}

func validateRequiredTargetFields(target TargetSnapshot) error {
	if strings.TrimSpace(target.Title) == "" || strings.TrimSpace(target.Body+target.Caption) == "" {
		return fmt.Errorf("target title and body are required")
	}
	if target.PlatformCode != "csdn" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(target.TagsJSON), &tags); err != nil {
		return fmt.Errorf("CSDN tags must be a string array")
	}
	for _, tag := range tags {
		if strings.Trim(strings.TrimSpace(tag), "#") != "" {
			return nil
		}
	}
	return fmt.Errorf("CSDN requires at least one article tag")
}

func hashFile(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, fmt.Errorf("media file unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", 0, fmt.Errorf("media path must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("media file unavailable")
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", 0, fmt.Errorf("media file could not be hashed")
	}
	return hex.EncodeToString(digest.Sum(nil)), info.Size(), nil
}

func mediaSnapshot(asset db.MediaAsset, position int, role string) (MediaSnapshot, error) {
	actualHash, actualSize, err := hashFile(asset.FilePath)
	if err != nil {
		return MediaSnapshot{}, err
	}
	if asset.SHA256 == "" || !strings.EqualFold(asset.SHA256, actualHash) {
		return MediaSnapshot{}, fmt.Errorf("media hash changed since import")
	}
	return MediaSnapshot{
		ID: asset.ID, Kind: asset.Kind, FilePath: asset.FilePath, MIMEType: asset.MimeType,
		SHA256: strings.ToLower(actualHash), SizeBytes: actualSize, Width: asset.Width, Height: asset.Height,
		DurationMS: asset.DurationMS, Position: position, Role: role,
	}, nil
}

func (s *Service) ensureVideoCover(contentID int64, input FreezeInput) (FreezeInput, error) {
	if input.MediaMode != "video" {
		if input.NativeVideoCover {
			return input, fmt.Errorf("native video cover requires video mode")
		}
		return input, nil
	}
	if input.NativeVideoCover && input.CoverMediaID > 0 {
		return input, fmt.Errorf("explicit and native video covers are mutually exclusive")
	}
	if input.NativeVideoCover || input.CoverMediaID > 0 {
		return input, nil
	}
	assets, err := s.DB.ListMediaByContent(contentID)
	if err != nil {
		return input, err
	}
	for _, asset := range assets {
		if asset.Kind == "image" {
			input.CoverMediaID = asset.ID
			return input, nil
		}
	}
	if len(input.MediaIDs) != 1 {
		return input, fmt.Errorf("video cover requires exactly one selected video")
	}
	var video db.MediaAsset
	for _, asset := range assets {
		if asset.ID == input.MediaIDs[0] {
			video = asset
			break
		}
	}
	if video.ID == 0 || video.Kind != "video" {
		return input, fmt.Errorf("video cover source is unavailable")
	}
	videoSnapshot, err := mediaSnapshot(video, 0, "video")
	if err != nil {
		return input, err
	}
	outputDir := filepath.Join(s.DB.DataDir, "media", "generated-covers")
	outputPath := filepath.Join(outputDir, videoSnapshot.SHA256[:24]+"-first-frame.jpg")
	var meta *media.FileMeta
	if info, statErr := os.Stat(outputPath); statErr == nil && info.Mode().IsRegular() {
		meta, err = media.Probe(outputPath)
	} else {
		extractor := s.CoverExtractor
		if extractor == nil {
			extractor = media.ExtractFirstFrame
		}
		meta, err = extractor(video.FilePath, outputPath)
	}
	if err != nil || meta == nil || meta.SizeBytes <= 0 || !strings.HasPrefix(strings.ToLower(meta.MimeGuess), "image/") {
		if err == nil {
			err = fmt.Errorf("generated cover is invalid")
		}
		return input, fmt.Errorf("video cover could not be generated: %w", err)
	}
	cover, err := s.DB.CreateMediaAsset(
		contentID, "image", meta.Path, video.FilePath, meta.MimeGuess,
		meta.SizeBytes, meta.SHA256, meta.Width, meta.Height, 0,
	)
	if err != nil {
		return input, fmt.Errorf("index generated video cover: %w", err)
	}
	input.CoverMediaID = cover.ID
	return input, nil
}

func (s *Service) selectedMedia(contentID int64, input FreezeInput) ([]MediaSnapshot, error) {
	assets, err := s.DB.ListMediaByContent(contentID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]db.MediaAsset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	seen := make(map[int64]bool)
	selected := make([]MediaSnapshot, 0, len(input.MediaIDs)+1)
	for position, id := range input.MediaIDs {
		if id <= 0 || seen[id] {
			return nil, fmt.Errorf("media ids must be unique positive values")
		}
		seen[id] = true
		asset, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("media does not belong to content")
		}
		role := "content"
		if input.MediaMode == "video" {
			role = "video"
		} else if input.MediaMode == "article" && asset.Role == "cover" {
			role = "cover"
		}
		snapshot, err := mediaSnapshot(asset, position, role)
		if err != nil {
			return nil, err
		}
		selected = append(selected, snapshot)
	}
	switch input.MediaMode {
	case "graph_text":
		if len(selected) < 1 || len(selected) > 9 {
			return nil, fmt.Errorf("graph_text requires 1-9 images")
		}
		for _, item := range selected {
			if item.Kind != "image" {
				return nil, fmt.Errorf("graph_text accepts images only")
			}
		}
		if input.CoverMediaID != 0 {
			return nil, fmt.Errorf("graph_text cover is the first ordered image")
		}
	case "article":
		if len(selected) > 9 {
			return nil, fmt.Errorf("article accepts at most 9 images")
		}
		for _, item := range selected {
			if item.Kind != "image" {
				return nil, fmt.Errorf("article accepts images only")
			}
		}
	case "video":
		if len(selected) != 1 || selected[0].Kind != "video" {
			return nil, fmt.Errorf("video mode requires exactly one video")
		}
		if input.CoverMediaID > 0 {
			if seen[input.CoverMediaID] {
				return nil, fmt.Errorf("video cover must be a separate image")
			}
			cover, ok := byID[input.CoverMediaID]
			if !ok || cover.Kind != "image" {
				return nil, fmt.Errorf("video cover must be an image belonging to content")
			}
			snapshot, err := mediaSnapshot(cover, 1, "cover")
			if err != nil {
				return nil, err
			}
			selected = append(selected, snapshot)
		}
	default:
		return nil, fmt.Errorf("media mode must be graph_text, video, or article")
	}
	return selected, nil
}

func normalizedSettings(platformCode, raw string) (string, error) {
	if _, ok := platforms.SettingsSchemaFor(platformCode); ok {
		return platforms.NormalizeSettings(platformCode, raw)
	}
	if strings.TrimSpace(raw) == "" {
		return "{}", nil
	}
	var value map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil || len(value) != 0 {
		return "", fmt.Errorf("platform has no reviewed settings fields")
	}
	return "{}", nil
}

func normalizePlanSettings(platformCode, mediaMode, raw string) (string, error) {
	if _, ok := platforms.SettingsSchemaFor(platformCode); ok {
		return platforms.NormalizeSettingsForContentType(platformCode, mediaMode, raw)
	}
	return normalizedSettings(platformCode, raw)
}

func (s *Service) targetSnapshot(content *db.Content, mediaMode string, policy *CompliancePolicy, input TargetInput) (TargetSnapshot, error) {
	account, err := s.DB.GetAccount(input.AccountID)
	if err != nil || !account.Enabled || !supportedPlanPlatforms[account.PlatformCode] {
		return TargetSnapshot{}, fmt.Errorf("enabled supported account is required")
	}
	if account.LoginState != db.AccountAuthLoggedIn {
		return TargetSnapshot{}, fmt.Errorf("account must be logged in before preparing a publish plan")
	}
	assets, err := s.DB.ListAccountAssetsByAccount(account.ID)
	if err != nil {
		return TargetSnapshot{}, err
	}
	var selected *db.AccountAsset
	if input.AssetID > 0 {
		for index := range assets {
			if assets[index].ID == input.AssetID {
				selected = &assets[index]
				break
			}
		}
		if selected == nil || publishguard.ValidateTargetOwnership(s.DB, selected, selected.PlatformCode) != nil {
			return TargetSnapshot{}, fmt.Errorf("selected publish target does not belong to the account")
		}
		if selected.PlatformCode != "facebook" && selected.PlatformCode != "instagram" {
			return TargetSnapshot{}, fmt.Errorf("explicit asset selection is supported for Meta targets only")
		}
		if strings.TrimSpace(selected.Name) == "" || strings.TrimSpace(selected.ExternalID) == "" {
			return TargetSnapshot{}, fmt.Errorf("Meta publish target name and public ID must be confirmed")
		}
	} else {
		for index := range assets {
			asset := &assets[index]
			if asset.Enabled && asset.PlatformCode == account.PlatformCode && asset.AssetType == "browser_account" {
				selected = asset
				break
			}
		}
	}
	if selected == nil {
		return TargetSnapshot{}, fmt.Errorf("account has no enabled browser target")
	}
	if !selected.Enabled {
		return TargetSnapshot{}, fmt.Errorf("publish target must be enabled")
	}
	var connected *db.AccountAsset
	if selected.ConnectedAssetID > 0 {
		connected, err = s.DB.GetAccountAsset(selected.ConnectedAssetID)
		if err != nil || connected.AccountID != account.ID || !connected.Enabled || selected.ConnectedAssetName != connected.Name {
			return TargetSnapshot{}, fmt.Errorf("connected publish target identity is inconsistent")
		}
	}
	tagsJSON, err := canonicalTags(content.TagsJSON)
	if err != nil {
		return TargetSnapshot{}, err
	}
	target := TargetSnapshot{
		AccountID: account.ID, AccountPlatformCode: account.PlatformCode,
		AssetID: selected.ID, PlatformCode: selected.PlatformCode,
		AssetType: selected.AssetType, AssetName: selected.Name, AssetExternalID: selected.ExternalID, AssetEnabled: selected.Enabled,
		Title: content.Title, Caption: content.Caption, Body: content.Body, TagsJSON: tagsJSON,
	}
	if connected != nil {
		target.ConnectedAssetID = connected.ID
		target.ConnectedAssetPlatformCode = connected.PlatformCode
		target.ConnectedAssetType = connected.AssetType
		target.ConnectedAssetName = connected.Name
		target.ConnectedAssetExternalID = connected.ExternalID
		target.ConnectedAssetEnabled = connected.Enabled
	}
	if input.VariantID > 0 {
		variant, err := s.DB.GetContentVariant(input.VariantID)
		if err != nil || variant.ContentID != content.ID || variant.PlatformCode != target.PlatformCode || (variant.AccountID != 0 && variant.AccountID != account.ID) {
			return TargetSnapshot{}, fmt.Errorf("content variant does not match target")
		}
		target.VariantID = variant.ID
		target.Title = variant.Title
		target.Caption = variant.Caption
		target.Body = variant.Body
		target.TagsJSON = variant.TagsJSON
	}
	if err := validateRequiredTargetFields(target); err != nil {
		return TargetSnapshot{}, err
	}
	if policy == nil {
		target.SettingsJSON, err = normalizePlanSettings(target.PlatformCode, mediaMode, input.SettingsJSON)
		return target, err
	}
	target.SettingsJSON, err = applyComplianceSettings(target.PlatformCode, input.SettingsJSON, *policy)
	if err != nil {
		return TargetSnapshot{}, err
	}
	target.SettingsJSON, err = normalizePlanSettings(target.PlatformCode, mediaMode, target.SettingsJSON)
	if err != nil {
		return TargetSnapshot{}, err
	}
	target.ComplianceFields, err = buildTargetCompliance(
		target.PlatformCode,
		mediaMode,
		strings.Join([]string{target.Title, target.Caption, target.Body}, "\n"),
		*policy,
	)
	if err != nil {
		return TargetSnapshot{}, err
	}
	if err := validateComplianceTarget(target, *policy); err != nil {
		return TargetSnapshot{}, err
	}
	return target, nil
}

func (s *Service) frozenPlanBySnapshot(snapshotHash string) (*Plan, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM publish_plans WHERE snapshot_hash=? AND status=? ORDER BY id DESC LIMIT 1`, snapshotHash, StatusFrozen).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

func (s *Service) Freeze(input FreezeInput) (*Plan, error) {
	if s == nil || s.DB == nil || input.ContentID <= 0 || len(input.Targets) == 0 || len(input.Targets) > 100 {
		return nil, fmt.Errorf("content and 1-100 targets are required")
	}
	content, err := s.DB.GetContent(input.ContentID)
	if err != nil {
		return nil, err
	}
	plannedAt := strings.TrimSpace(input.PublishAt)
	if plannedAt == "" {
		plannedAt = content.PlannedAt
	}
	plannedAt, err = normalizePublishTime(plannedAt, s.now())
	if err != nil {
		return nil, err
	}
	if content.Type != input.MediaMode {
		return nil, fmt.Errorf("content type must match media mode")
	}
	if input.Concurrency <= 0 {
		input.Concurrency = 1
	}
	if input.Concurrency > 3 {
		return nil, fmt.Errorf("publish plan concurrency must be between 1 and 3")
	}
	var frozenPolicy *CompliancePolicy
	if input.CompliancePolicy != nil {
		if err := validateCompliancePolicy(*input.CompliancePolicy); err != nil {
			return nil, err
		}
		copy := *input.CompliancePolicy
		frozenPolicy = &copy
	}
	input, err = s.ensureVideoCover(content.ID, input)
	if err != nil {
		return nil, err
	}
	media, err := s.selectedMedia(content.ID, input)
	if err != nil {
		return nil, err
	}
	targetInputs := append([]TargetInput(nil), input.Targets...)
	seenAssets := make(map[int64]bool, len(targetInputs))
	targets := make([]TargetSnapshot, 0, len(targetInputs))
	for _, targetInput := range targetInputs {
		if targetInput.AccountID <= 0 {
			return nil, fmt.Errorf("target account ids must be positive values")
		}
		target, err := s.targetSnapshot(content, input.MediaMode, frozenPolicy, targetInput)
		if err != nil {
			return nil, err
		}
		if seenAssets[target.AssetID] {
			return nil, fmt.Errorf("publish target assets must be unique")
		}
		seenAssets[target.AssetID] = true
		if !supportsMediaMode(target.PlatformCode, input.MediaMode) {
			return nil, fmt.Errorf("platform does not support the selected media mode")
		}
		if plannedAt != "" {
			if !egopublish.NativeScheduleAdapterSupportedFor(target.PlatformCode, input.MediaMode) ||
				(input.StudioHandoffID > 0 && !egopublish.NativeScheduleVerifiedFor(target.PlatformCode, input.MediaMode)) {
				return nil, fmt.Errorf("EGO_PUBLISH_SCHEDULE_UNVERIFIED")
			}
			leadMinutes := egopublish.NativeScheduleMinLeadMinutesFor(target.PlatformCode, input.MediaMode)
			publishAt, parseErr := time.Parse(time.RFC3339, plannedAt)
			if parseErr != nil || leadMinutes < 1 || publishAt.Before(s.now().Add(time.Duration(leadMinutes)*time.Minute)) {
				return nil, fmt.Errorf("planned publish time must meet the platform lead time")
			}
		}
		target.PublishAt = plannedAt
		targets = append(targets, target)
	}
	tagsJSON, err := canonicalTags(content.TagsJSON)
	if err != nil {
		return nil, err
	}
	snapshot := Snapshot{
		Version: PublishPlanSnapshotVersion, Content: ContentSnapshot{ID: content.ID, Type: content.Type, Title: content.Title, Caption: content.Caption, Body: content.Body, TagsJSON: tagsJSON, PlannedAt: plannedAt},
		MediaMode: input.MediaMode, Media: media, Targets: targets, CompliancePolicy: frozenPolicy, Concurrency: input.Concurrency,
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(snapshotJSON)
	snapshotHash := hex.EncodeToString(digest[:])
	// A user may return from the review step without changing anything, or the
	// desktop UI may retry the same request after a transport hiccup. Reuse the
	// exact frozen snapshot so one intent cannot leave duplicate plans behind.
	if input.StudioHandoffID == 0 {
		if existing, err := s.frozenPlanBySnapshot(snapshotHash); err != nil {
			return nil, err
		} else if existing != nil {
			return existing, nil
		}
	}
	nowTime := s.now()
	now := nowTime.Format(time.RFC3339Nano)
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if input.StudioHandoffID > 0 {
		handoff, validateErr := s.DB.ValidateStudioScheduleHandoffTx(context.Background(), tx, input.StudioHandoffID, nowTime)
		if validateErr != nil {
			return nil, validateErr
		}
		if handoff.ContentID != content.ID || handoff.MediaMode != input.MediaMode || handoff.PlannedAtUTC != plannedAt || handoff.CoverMediaID != input.CoverMediaID || handoff.NativeVideoCover != input.NativeVideoCover || len(handoff.MediaIDs) != len(input.MediaIDs) {
			return nil, fmt.Errorf("%s", egopublish.ScheduleHandoffSuperseded)
		}
		for index := range handoff.MediaIDs {
			if handoff.MediaIDs[index] != input.MediaIDs[index] {
				return nil, fmt.Errorf("%s", egopublish.ScheduleHandoffSuperseded)
			}
		}
	}
	var handoffID any
	if input.StudioHandoffID > 0 {
		handoffID = input.StudioHandoffID
	}
	result, err := tx.Exec(`INSERT INTO publish_plans (content_id, media_mode, snapshot_json, snapshot_hash, status, concurrency, studio_handoff_id, created_at, frozen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, content.ID, input.MediaMode, string(snapshotJSON), snapshotHash, StatusFrozen, input.Concurrency, handoffID, now, now, now)
	if err != nil {
		return nil, err
	}
	planID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		var variantID interface{}
		if target.VariantID > 0 {
			variantID = target.VariantID
		}
		if _, err := tx.Exec(`INSERT INTO publish_plan_targets (plan_id, account_id, asset_id, platform_code, variant_id, settings_json, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, planID, target.AccountID, target.AssetID, target.PlatformCode, variantID, target.SettingsJSON, now, now); err != nil {
			return nil, err
		}
	}
	if input.StudioHandoffID > 0 {
		if err := s.DB.FreezeStudioScheduleHandoffTx(context.Background(), tx, input.StudioHandoffID, planID, snapshotHash, nowTime); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Plan{ID: planID, ContentID: content.ID, MediaMode: input.MediaMode, Snapshot: snapshot, SnapshotHash: snapshotHash, Status: StatusFrozen, Concurrency: input.Concurrency, StudioHandoffID: input.StudioHandoffID, CreatedAt: now, FrozenAt: now}, nil
}

// FreezeHandoff turns a ready Studio editorial handoff into one immutable
// publish plan. It deliberately accepts account targets only; the reviewed
// content, ordered media, mode, and wall-clock time all come from the handoff.
func (s *Service) FreezeHandoff(input FreezeHandoffInput) (*Plan, error) {
	if s == nil || s.DB == nil || input.HandoffID <= 0 {
		return nil, fmt.Errorf("studio schedule handoff id is required")
	}
	if input.Concurrency < 0 || input.Concurrency > 1 {
		return nil, fmt.Errorf("studio schedule handoff concurrency must be 1")
	}
	input.Concurrency = 1
	handoff, err := s.DB.GetStudioScheduleHandoff(context.Background(), input.HandoffID)
	if err != nil {
		return nil, err
	}
	if handoff.Status != db.StudioScheduleHandoffReady {
		return nil, fmt.Errorf("%s", egopublish.ScheduleHandoffSuperseded)
	}
	for _, target := range input.Targets {
		account, accountErr := s.DB.GetAccount(target.AccountID)
		if accountErr != nil || account.LoginState != db.AccountAuthLoggedIn {
			return nil, fmt.Errorf("%s", egopublish.ScheduleAccountSessionUnverified)
		}
	}
	return s.Freeze(FreezeInput{
		ContentID: handoff.ContentID, MediaMode: handoff.MediaMode, MediaIDs: handoff.MediaIDs, CoverMediaID: handoff.CoverMediaID,
		NativeVideoCover: handoff.NativeVideoCover, PublishAt: handoff.PlannedAtUTC, CompliancePolicy: input.CompliancePolicy,
		Concurrency: input.Concurrency, Targets: input.Targets, StudioHandoffID: handoff.ID,
	})
}

func (s *Service) Get(id int64) (*Plan, error) {
	if s == nil || s.DB == nil || id <= 0 {
		return nil, fmt.Errorf("publish plan id is required")
	}
	var plan Plan
	var snapshotJSON string
	var approvedAt sql.NullString
	err := s.DB.QueryRow(`SELECT id, content_id, media_mode, snapshot_json, snapshot_hash, status, concurrency, COALESCE(studio_handoff_id,0), created_at, frozen_at, approved_at, COALESCE(approved_action,'')
		FROM publish_plans WHERE id=?`, id).
		Scan(&plan.ID, &plan.ContentID, &plan.MediaMode, &snapshotJSON, &plan.SnapshotHash, &plan.Status, &plan.Concurrency, &plan.StudioHandoffID, &plan.CreatedAt, &plan.FrozenAt, &approvedAt, &plan.ApprovedAction)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("publish plan not found")
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(snapshotJSON), &plan.Snapshot); err != nil {
		return nil, fmt.Errorf("decode frozen publish plan: %w", err)
	}
	digest := sha256.Sum256([]byte(snapshotJSON))
	if subtle.ConstantTimeCompare([]byte(plan.SnapshotHash), []byte(hex.EncodeToString(digest[:]))) != 1 {
		return nil, fmt.Errorf("frozen publish plan snapshot integrity check failed")
	}
	if plan.Snapshot.Content.ID != plan.ContentID || plan.Snapshot.MediaMode != plan.MediaMode || plan.Snapshot.Concurrency != plan.Concurrency {
		return nil, fmt.Errorf("frozen publish plan metadata does not match its snapshot")
	}
	if approvedAt.Valid {
		plan.ApprovedAt = approvedAt.String
	}
	rows, err := s.DB.Query(`SELECT target.account_id, target.asset_id, target.platform_code, COALESCE(target.publish_job_id,0), target.status, COALESCE(target.error_code,''), COALESCE(job.submit_attempted_at,'')
		FROM publish_plan_targets AS target LEFT JOIN publish_jobs AS job ON job.id=target.publish_job_id
		WHERE target.plan_id=? ORDER BY target.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var target TargetState
		var submitAttemptedAt string
		if err := rows.Scan(&target.AccountID, &target.AssetID, &target.PlatformCode, &target.JobID, &target.Status, &target.ErrorCode, &submitAttemptedAt); err != nil {
			return nil, err
		}
		target.SubmitAttempted = strings.TrimSpace(submitAttemptedAt) != ""
		plan.TargetStates = append(plan.TargetStates, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *Service) ListSummaries(limit int) ([]PlanSummary, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("publish plan database is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id FROM publish_plans ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	summaries := make([]PlanSummary, 0, len(ids))
	for _, id := range ids {
		plan, err := s.Get(id)
		if err != nil {
			return nil, err
		}
		platformCodes := make([]string, 0, len(plan.Snapshot.Targets))
		seenPlatforms := make(map[string]bool, len(plan.Snapshot.Targets))
		for _, target := range plan.Snapshot.Targets {
			if target.PlatformCode == "" || seenPlatforms[target.PlatformCode] {
				continue
			}
			seenPlatforms[target.PlatformCode] = true
			platformCodes = append(platformCodes, target.PlatformCode)
		}
		summaries = append(summaries, PlanSummary{
			ID: plan.ID, ContentID: plan.ContentID, ContentTitle: plan.Snapshot.Content.Title,
			MediaMode: plan.MediaMode, Status: plan.Status, TargetCount: len(plan.Snapshot.Targets),
			PlatformCodes: platformCodes, CreatedAt: plan.CreatedAt,
		})
	}
	return summaries, nil
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func publishPlanToolForAction(action string) (string, error) {
	switch strings.TrimSpace(action) {
	case PublishPlanActionPrepare:
		return "publish.plan.prepare", nil
	case PublishPlanActionSubmit:
		return "publish.plan.submit", nil
	case PublishPlanActionAcceptanceSubmit:
		return "publish.plan.acceptance_submit", nil
	default:
		return "", fmt.Errorf("unsupported publish plan action")
	}
}

func isSubmitAction(action string) bool {
	action = strings.TrimSpace(action)
	return action == PublishPlanActionSubmit || action == PublishPlanActionAcceptanceSubmit
}

func validateAcceptanceSubmitPlan(plan *Plan) error {
	if plan == nil || len(plan.Snapshot.Targets) != 1 || plan.Concurrency != 1 {
		return fmt.Errorf("PUBLISH_ACCEPTANCE_SINGLE_TARGET_REQUIRED")
	}
	target := plan.Snapshot.Targets[0]
	if !egopublish.PrepareAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode) ||
		!egopublish.SubmitAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode) ||
		!egopublish.VerifyAdapterSupportedFor(target.PlatformCode, plan.Snapshot.MediaMode) {
		return fmt.Errorf("EGO_PUBLISH_ACCEPTANCE_UNAVAILABLE")
	}
	return nil
}

func approvalBindingHash(plan *Plan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("publish plan is required")
	}
	type targetBinding struct {
		AccountID    int64  `json:"account_id"`
		AssetID      int64  `json:"asset_id"`
		PlatformCode string `json:"platform_code"`
		SettingsJSON string `json:"settings_json"`
	}
	type binding struct {
		PlanID       int64           `json:"plan_id"`
		SnapshotHash string          `json:"snapshot_hash"`
		MediaHashes  []string        `json:"media_hashes"`
		Targets      []targetBinding `json:"targets"`
	}
	value := binding{
		PlanID:       plan.ID,
		SnapshotHash: plan.SnapshotHash,
		MediaHashes:  make([]string, 0, len(plan.Snapshot.Media)),
		Targets:      make([]targetBinding, 0, len(plan.Snapshot.Targets)),
	}
	for _, item := range plan.Snapshot.Media {
		value.MediaHashes = append(value.MediaHashes, item.SHA256)
	}
	for _, target := range plan.Snapshot.Targets {
		value.Targets = append(value.Targets, targetBinding{
			AccountID: target.AccountID, AssetID: target.AssetID,
			PlatformCode: target.PlatformCode, SettingsJSON: target.SettingsJSON,
		})
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Service) Approve(planID int64, snapshotHash, sessionID string, ttl time.Duration) (*ApprovalGrant, error) {
	return s.ApproveForAction(planID, snapshotHash, sessionID, PublishPlanActionPrepare, ttl)
}

type approvalQueryer interface {
	QueryRow(string, ...any) *sql.Row
}

func preparedTargetSubmitReady(queryer approvalQueryer, plan *Plan, target TargetSnapshot) bool {
	if queryer == nil || plan == nil {
		return false
	}
	var ready int
	err := queryer.QueryRow(`SELECT COUNT(DISTINCT target.id)
		FROM publish_plan_targets AS target
		JOIN publish_jobs AS job ON job.plan_id=target.plan_id AND job.plan_target_id=target.id
		WHERE target.plan_id=? AND target.account_id=? AND target.asset_id=? AND target.platform_code=? AND target.settings_json=?
		  AND job.snapshot_hash=? AND job.content_id=? AND job.content_type=?
		  AND job.account_id=target.account_id AND COALESCE(job.asset_id,0)=target.asset_id
		  AND job.platform_code=target.platform_code AND COALESCE(job.settings_json,'')=target.settings_json
		  AND job.mode=? AND job.status=? AND job.current_step IN ('ready_to_submit','review_or_submit')
		  AND COALESCE(job.submit_attempted_at,'')=''
		  AND (target.status=? OR (target.status='failed' AND target.error_code='PUBLISH_JOB_CREATE_FAILED') OR EXISTS (
			SELECT 1 FROM publish_jobs AS failed_submit
			WHERE failed_submit.id=target.publish_job_id AND failed_submit.mode=?
			  AND failed_submit.status='failed' AND failed_submit.error_code='AUTO_SUBMIT_NOT_SUPPORTED'
			  AND failed_submit.snapshot_hash=? AND COALESCE(failed_submit.submit_attempted_at,'')=''
		  ) OR EXISTS (
			SELECT 1 FROM publish_jobs AS unsubmitted_retry
			WHERE unsubmitted_retry.id=target.publish_job_id
			  AND unsubmitted_retry.plan_id=target.plan_id AND unsubmitted_retry.plan_target_id=target.id
			  AND unsubmitted_retry.mode=? AND unsubmitted_retry.status IN ('failed','manual_takeover')
			  AND unsubmitted_retry.snapshot_hash=? AND COALESCE(unsubmitted_retry.submit_attempted_at,'')=''
		  ) OR (target.status='failed' AND target.error_code='PUBLISH_JOB_CREATE_FAILED' AND EXISTS (
			SELECT 1 FROM publish_jobs AS failed_submit
			WHERE failed_submit.plan_id=target.plan_id AND failed_submit.plan_target_id=target.id AND failed_submit.mode=?
			  AND failed_submit.status IN ('failed','manual_takeover')
			  AND failed_submit.snapshot_hash=? AND COALESCE(failed_submit.submit_attempted_at,'')=''
		  )))`,
		plan.ID, target.AccountID, target.AssetID, target.PlatformCode, target.SettingsJSON,
		plan.SnapshotHash, plan.ContentID, plan.MediaMode,
		PublishPlanActionPrepare, StatusAwaitingReview, StatusAwaitingReview,
		PublishPlanActionSubmit, plan.SnapshotHash,
		PublishPlanActionSubmit, plan.SnapshotHash,
		PublishPlanActionSubmit, plan.SnapshotHash,
	).Scan(&ready)
	return err == nil && ready > 0
}

func preparedSubmitCounts(queryer approvalQueryer, plan *Plan) (int, int) {
	if queryer == nil || plan == nil || len(plan.Snapshot.Targets) == 0 {
		return 0, 0
	}
	ready := 0
	for _, target := range plan.Snapshot.Targets {
		if preparedTargetSubmitReady(queryer, plan, target) {
			ready++
		}
	}
	return ready, len(plan.Snapshot.Targets)
}

func preparedSubmitReady(queryer approvalQueryer, plan *Plan) bool {
	ready, targets := preparedSubmitCounts(queryer, plan)
	return targets > 0 && ready == targets
}

func preparedSubmitAvailable(queryer approvalQueryer, plan *Plan) bool {
	ready, _ := preparedSubmitCounts(queryer, plan)
	return ready > 0
}

func prepareRetryAllowed(queryer approvalQueryer, plan *Plan, status string) bool {
	if queryer == nil || plan == nil {
		return false
	}
	switch status {
	case StatusAwaitingReview, "manual_takeover", "partial_failed", "partial_published", "queued":
	default:
		return false
	}
	if preparedSubmitReady(queryer, plan) {
		return false
	}
	var mismatched int
	err := queryer.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN snapshot_hash<>? THEN 1 ELSE 0 END),0)
		FROM publish_jobs WHERE plan_id=?`,
		plan.SnapshotHash, plan.ID,
	).Scan(&mismatched)
	if err != nil || mismatched != 0 {
		return false
	}
	var queued, active int
	if err := queryer.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN status='queued' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status IN ('awaiting_review','manual_takeover','submitting','submitted_pending_verification','result_unknown') THEN 1 ELSE 0 END),0)
		FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&queued, &active); err != nil {
		return false
	}
	// A queued group may only be prepared after every target currently open in
	// the identity's bounded Space has reached a terminal recorded state.
	if queued > 0 {
		return active == 0
	}
	// A submitted/unknown target does not block another frozen target that
	// never reached the final control. Executor skips the submitted target by
	// exact content/account/asset/form identity and retries only this safe set.
	var retryable int
	err = queryer.QueryRow(`SELECT COUNT(*)
		FROM publish_plan_targets AS target
		WHERE target.plan_id=?
		  AND target.status IN ('awaiting_review','manual_takeover','failed','cancelled')
		  AND NOT EXISTS (
			SELECT 1 FROM publish_jobs AS attempted
			WHERE attempted.plan_id=target.plan_id AND attempted.plan_target_id=target.id
			  AND COALESCE(attempted.submit_attempted_at,'')<>''
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM publish_jobs AS ready
			WHERE ready.plan_id=target.plan_id AND ready.plan_target_id=target.id
			  AND ready.snapshot_hash=? AND ready.mode='prepare' AND ready.status='awaiting_review'
			  AND ready.current_step IN ('ready_to_submit','review_or_submit')
			  AND COALESCE(ready.submit_attempted_at,'')=''
		  )
		  AND (target.error_code='PUBLISH_JOB_CREATE_FAILED' OR EXISTS (
			SELECT 1 FROM publish_jobs AS prior
			WHERE prior.plan_id=target.plan_id AND prior.plan_target_id=target.id
			  AND prior.snapshot_hash=? AND COALESCE(prior.submit_attempted_at,'')=''
		  ))`,
		plan.ID, plan.SnapshotHash, plan.SnapshotHash,
	).Scan(&retryable)
	return err == nil && retryable > 0
}

func (s *Service) ApproveForAction(planID int64, snapshotHash, sessionID, action string, ttl time.Duration) (*ApprovalGrant, error) {
	toolName, err := publishPlanToolForAction(action)
	if err != nil {
		return nil, err
	}
	plan, err := s.Get(planID)
	if err != nil {
		return nil, err
	}
	if action == PublishPlanActionAcceptanceSubmit {
		if err := validateAcceptanceSubmitPlan(plan); err != nil {
			return nil, err
		}
	}
	if err := validateFrozenTargetIdentity(s.DB, plan); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(plan.SnapshotHash), []byte(strings.TrimSpace(snapshotHash))) != 1 {
		return nil, fmt.Errorf("publish plan snapshot changed or is not frozen")
	}
	if plan.Status != StatusFrozen {
		retryPrepare := action == PublishPlanActionPrepare && prepareRetryAllowed(s.DB, plan, plan.Status)
		submitState := plan.Status == StatusAwaitingReview || plan.Status == "partial_failed" || plan.Status == "manual_takeover"
		if !retryPrepare && (!isSubmitAction(action) || !submitState || !preparedSubmitAvailable(s.DB, plan)) {
			if isSubmitAction(action) && submitState {
				return nil, fmt.Errorf("PUBLISH_PLAN_NOT_READY_TO_SUBMIT")
			}
			return nil, fmt.Errorf("publish plan snapshot changed or is not frozen")
		}
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if ttl > 30*time.Minute {
		return nil, fmt.Errorf("approval TTL exceeds 30 minutes")
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "desktop-local"
	}
	idPart, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	nonce, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	nonceDigest := sha256.Sum256([]byte(nonce))
	argsHash, err := approvalBindingHash(plan)
	if err != nil {
		return nil, err
	}
	now := s.now()
	expiresAt := now.Add(ttl).Format(time.RFC3339Nano)
	id := "approval-" + idPart
	_, err = s.DB.Exec(`INSERT INTO approvals (id, session_id, tool_name, args_hash, capability, target_resource, expires_at, nonce_hash, single_use, created_at)
		VALUES (?, ?, ?, ?, 'external_write', ?, ?, ?, 1, ?)`,
		id, sessionID, toolName, argsHash, fmt.Sprintf("publish_plan:%d", plan.ID), expiresAt, hex.EncodeToString(nonceDigest[:]), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &ApprovalGrant{ID: id, Nonce: nonce, ExpiresAt: expiresAt}, nil
}

func (s *Service) ConsumeApproval(planID int64, snapshotHash, approvalID, nonce string) (*Plan, error) {
	return s.ConsumeApprovalForAction(planID, snapshotHash, approvalID, nonce, PublishPlanActionPrepare)
}

func (s *Service) ConsumeApprovalForAction(planID int64, snapshotHash, approvalID, nonce, action string) (*Plan, error) {
	if s == nil || s.DB == nil || planID <= 0 || approvalID == "" || nonce == "" {
		return nil, fmt.Errorf("publish plan approval is required")
	}
	expectedToolName, err := publishPlanToolForAction(action)
	if err != nil {
		return nil, err
	}
	plan, err := s.Get(planID)
	if err != nil {
		return nil, err
	}
	if action == PublishPlanActionAcceptanceSubmit {
		if err := validateAcceptanceSubmitPlan(plan); err != nil {
			return nil, err
		}
	}
	if err := validateFrozenTargetIdentity(s.DB, plan); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(plan.SnapshotHash), []byte(strings.TrimSpace(snapshotHash))) != 1 {
		return nil, fmt.Errorf("publish plan snapshot changed")
	}
	expectedArgsHash, err := approvalBindingHash(plan)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var toolName, storedArgsHash, targetResource, expiresAt, storedNonceHash string
	var singleUse int
	var consumedAt sql.NullString
	if err := tx.QueryRow(`SELECT tool_name, args_hash, target_resource, expires_at, nonce_hash, single_use, consumed_at FROM approvals WHERE id=?`, approvalID).
		Scan(&toolName, &storedArgsHash, &targetResource, &expiresAt, &storedNonceHash, &singleUse, &consumedAt); err != nil {
		return nil, fmt.Errorf("approval not found")
	}
	parsedExpiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !s.now().Before(parsedExpiry) || singleUse != 1 || consumedAt.Valid || toolName != expectedToolName || targetResource != fmt.Sprintf("publish_plan:%d", planID) || storedArgsHash != expectedArgsHash {
		return nil, fmt.Errorf("approval is expired, consumed, or bound to another request")
	}
	nonceDigest := sha256.Sum256([]byte(nonce))
	if subtle.ConstantTimeCompare([]byte(storedNonceHash), []byte(hex.EncodeToString(nonceDigest[:]))) != 1 {
		return nil, fmt.Errorf("approval nonce is invalid")
	}
	var persistedHash, status string
	if err := tx.QueryRow(`SELECT snapshot_hash, status FROM publish_plans WHERE id=?`, planID).Scan(&persistedHash, &status); err != nil || persistedHash != snapshotHash {
		return nil, fmt.Errorf("publish plan snapshot changed or is not frozen")
	}
	if status != StatusFrozen {
		retryPrepare := action == PublishPlanActionPrepare && prepareRetryAllowed(tx, plan, status)
		submitState := status == StatusAwaitingReview || status == "partial_failed" || status == "manual_takeover"
		if !retryPrepare && (!isSubmitAction(action) || !submitState || !preparedSubmitAvailable(tx, plan)) {
			if isSubmitAction(action) && submitState {
				return nil, fmt.Errorf("PUBLISH_PLAN_NOT_READY_TO_SUBMIT")
			}
			return nil, fmt.Errorf("publish plan snapshot changed or is not frozen")
		}
	}
	now := s.now().Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE approvals SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, now, approvalID)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, fmt.Errorf("approval was already consumed")
	}
	result, err = tx.Exec(`UPDATE publish_plans SET status=?, approved_action=?, approved_at=?, updated_at=? WHERE id=? AND status=? AND snapshot_hash=?`,
		StatusApproved, action, now, now, planID, status, snapshotHash)
	if err != nil {
		return nil, err
	}
	changed, _ = result.RowsAffected()
	if changed != 1 {
		return nil, fmt.Errorf("publish plan approval conflict")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(planID)
}
