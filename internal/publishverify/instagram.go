// Package publishverify turns independently observed creator-backend state
// into the only authoritative published transition.
//go:build legacy_chromium

package publishverify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishplan"
)

const (
	DefaultInstagramPollInterval        = 20 * time.Second
	DefaultInstagramVerificationTimeout = 10 * time.Minute

	VerificationPublished      = "published"
	VerificationPending        = "verification_pending"
	VerificationManualTakeover = "manual_takeover"
)

var ErrInstagramBackendDOMUnverified = errors.New("instagram creator-backend DOM adapter is not verified")

// BrowserStatusProvider deliberately exposes only BrowserFacade.Status.
// Verification never starts a second browser or attaches to an arbitrary port.
type BrowserStatusProvider interface {
	Status(accountID int64) (*browser.AccountStatus, error)
}

// InstagramBackendObserver preserves the original platform API while using
// the shared creator-backend observation boundary.
type InstagramBackendObserver = CreatorBackendObserver

// UnverifiedInstagramBackendObserver is the production-safe fallback until a
// live Meta Business Suite DOM fixture has been reviewed.
type UnverifiedInstagramBackendObserver struct{}

func (UnverifiedInstagramBackendObserver) Observe(context.Context, string, InstagramExpectation) (InstagramBackendSnapshot, error) {
	return InstagramBackendSnapshot{}, ErrInstagramBackendDOMUnverified
}

// Instagram aliases preserve the original API while sharing the strict
// platform-explicit matching core used by all ten launch platforms.
type InstagramExpectation = CreatorBackendExpectation
type InstagramBackendItem = CreatorBackendItem
type InstagramBackendSnapshot = CreatorBackendSnapshot

type InstagramVerificationResult struct {
	State          string
	ErrorCode      string
	Attempts       int
	ScreenshotPath string
	Record         *db.PublishRecord
}

type InstagramVerifier struct {
	DB           *db.DB
	Browsers     BrowserStatusProvider
	Observer     InstagramBackendObserver
	PollInterval time.Duration
	Timeout      time.Duration
	Now          func() time.Time
	Sleep        func(context.Context, time.Duration) error
}

// ParseInstagramBackendSnapshot parses the narrow, non-secret observation
// contract produced by a reviewed DOM adapter. It does not infer page state
// from broad body text.
func ParseInstagramBackendSnapshot(raw, screenshotPNG []byte) (InstagramBackendSnapshot, error) {
	return parseCreatorBackendSnapshot("instagram", raw, screenshotPNG, true)
}

// MatchInstagramBackend requires one exact target, content, media, time,
// public-link and explicit Published/Public match. Shared launch captions
// cannot collide because a reviewed media or cover hash is also mandatory.
func MatchInstagramBackend(expected InstagramExpectation, snapshot InstagramBackendSnapshot) (InstagramBackendItem, bool) {
	if expected.PlatformCode == "" {
		expected.PlatformCode = "instagram"
	}
	if expected.ContentType == "" {
		expected.ContentType = "graph_text"
	}
	if snapshot.PlatformCode == "" {
		snapshot.PlatformCode = "instagram"
	}
	snapshot.Items = append([]CreatorBackendItem(nil), snapshot.Items...)
	for index := range snapshot.Items {
		if snapshot.Items[index].ContentType == "" {
			snapshot.Items[index].ContentType = expected.ContentType
		}
	}
	return MatchCreatorBackend(expected, snapshot)
}

func (v *InstagramVerifier) Verify(ctx context.Context, jobID int64) (*InstagramVerificationResult, error) {
	if v == nil || v.DB == nil || v.Browsers == nil || jobID <= 0 {
		return nil, fmt.Errorf("instagram verifier requires database, BrowserFacade status, and job id")
	}
	expected, err := v.expectation(jobID)
	if err != nil {
		return nil, err
	}
	status, statusErr := v.Browsers.Status(expected.AccountID)
	cdpURL, cdpErr := publishguard.LiveCDPURL(expected.AccountID, status, statusErr)
	if cdpErr != nil {
		return &InstagramVerificationResult{
			State:     VerificationManualTakeover,
			ErrorCode: "INSTAGRAM_BACKEND_BROWSER_UNAVAILABLE",
		}, nil
	}
	observer := v.Observer
	if observer == nil {
		observer = UnverifiedInstagramBackendObserver{}
	}
	interval := v.PollInterval
	if interval <= 0 {
		interval = DefaultInstagramPollInterval
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = DefaultInstagramVerificationTimeout
	}
	now := v.now()
	deadline := now.Add(timeout)
	expected.PublishedBefore = deadline
	attempts := 0
	lastCode := ""
	for {
		attempts++
		snapshot, observeErr := observer.Observe(ctx, cdpURL, expected)
		if errors.Is(observeErr, ErrInstagramBackendDOMUnverified) {
			return &InstagramVerificationResult{
				State: VerificationManualTakeover, ErrorCode: "INSTAGRAM_BACKEND_DOM_UNVERIFIED", Attempts: attempts,
			}, nil
		}
		if observeErr != nil {
			lastCode = "INSTAGRAM_BACKEND_OBSERVE_FAILED"
		} else if item, matched := MatchInstagramBackend(expected, snapshot); matched {
			screenshotPath, saveErr := saveInstagramEvidenceScreenshot(v.DB.DataDir, jobID, snapshot.ScreenshotPNG)
			if saveErr != nil {
				return nil, saveErr
			}
			record, recordErr := v.DB.RecordCreatorBackendPublished(jobID, db.CreatorBackendEvidence{
				ObservedStatus: item.Status,
				PostURL:        item.PostURL,
				ExternalPostID: item.ExternalPostID,
				ScreenshotPath: screenshotPath,
			})
			if recordErr != nil {
				return nil, recordErr
			}
			return &InstagramVerificationResult{
				State: VerificationPublished, Attempts: attempts, ScreenshotPath: screenshotPath, Record: record,
			}, nil
		}
		current := v.now()
		if !current.Before(deadline) {
			if lastCode == "" {
				lastCode = "INSTAGRAM_PUBLISHED_EVIDENCE_NOT_FOUND"
			}
			return &InstagramVerificationResult{
				State: VerificationPending, ErrorCode: lastCode, Attempts: attempts,
			}, nil
		}
		wait := interval
		if remaining := deadline.Sub(current); wait > remaining {
			wait = remaining
		}
		if err := v.sleep(ctx, wait); err != nil {
			return &InstagramVerificationResult{
				State: VerificationPending, ErrorCode: "INSTAGRAM_VERIFICATION_INTERRUPTED", Attempts: attempts,
			}, err
		}
	}
}

func (v *InstagramVerifier) expectation(jobID int64) (InstagramExpectation, error) {
	job, err := v.DB.GetPublishJob(jobID)
	if err != nil {
		return InstagramExpectation{}, err
	}
	if job.PlatformCode != "instagram" || job.PlanID <= 0 || job.PlanTargetID <= 0 ||
		job.Mode != "submit" || strings.TrimSpace(job.SubmitAttemptedAt) == "" {
		return InstagramExpectation{}, fmt.Errorf("instagram verification requires a submitted frozen Instagram job")
	}
	switch job.Status {
	case "awaiting_review", "manual_takeover", "failed":
	default:
		return InstagramExpectation{}, fmt.Errorf("instagram verification job is not awaiting creator-backend evidence")
	}
	submittedAt, err := time.Parse(time.RFC3339Nano, job.SubmitAttemptedAt)
	if err != nil {
		submittedAt, err = time.Parse(time.RFC3339, job.SubmitAttemptedAt)
	}
	if err != nil {
		return InstagramExpectation{}, fmt.Errorf("instagram submit time is invalid")
	}
	plan, err := (&publishplan.Service{DB: v.DB}).Get(job.PlanID)
	if err != nil {
		return InstagramExpectation{}, err
	}
	if plan.SnapshotHash != job.SnapshotHash || plan.ContentID != job.ContentID {
		return InstagramExpectation{}, fmt.Errorf("instagram verification frozen snapshot does not match job")
	}
	var target *publishplan.TargetSnapshot
	for index := range plan.Snapshot.Targets {
		candidate := &plan.Snapshot.Targets[index]
		if candidate.AccountID == job.AccountID && candidate.AssetID == job.AssetID && candidate.PlatformCode == "instagram" {
			if target != nil {
				return InstagramExpectation{}, fmt.Errorf("instagram verification frozen target is ambiguous")
			}
			target = candidate
		}
	}
	if target == nil {
		return InstagramExpectation{}, fmt.Errorf("instagram verification frozen target is missing")
	}
	asset, err := v.DB.GetAccountAsset(job.AssetID)
	if err != nil {
		return InstagramExpectation{}, err
	}
	if err := publishguard.ValidateTargetOwnership(v.DB, asset, "instagram"); err != nil ||
		asset.AccountID != job.AccountID ||
		strings.TrimSpace(asset.Name) == "" ||
		strings.TrimSpace(asset.ExternalID) == "" {
		return InstagramExpectation{}, fmt.Errorf("instagram verification target identity is invalid")
	}
	hashes := make([]string, 0, len(plan.Snapshot.Media))
	for _, item := range plan.Snapshot.Media {
		if validSHA256(item.SHA256) {
			hashes = append(hashes, strings.ToLower(item.SHA256))
		}
	}
	if len(hashes) == 0 {
		return InstagramExpectation{}, fmt.Errorf("instagram verification frozen media hashes are missing")
	}
	return InstagramExpectation{
		JobID: job.ID, AccountID: job.AccountID, AssetID: job.AssetID,
		PlatformCode: "instagram", ContentType: job.ContentType,
		TargetName: asset.Name, TargetExternalID: asset.ExternalID,
		Title: target.Title, Caption: target.Caption, Body: target.Body,
		MediaSHA256: hashes, SubmittedAt: submittedAt.UTC(),
	}, nil
}

func (v *InstagramVerifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

func (v *InstagramVerifier) sleep(ctx context.Context, duration time.Duration) error {
	if v.Sleep != nil {
		return v.Sleep(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func saveInstagramEvidenceScreenshot(dataDir string, jobID int64, screenshot []byte) (string, error) {
	if strings.TrimSpace(dataDir) == "" || jobID <= 0 || !validPNG(screenshot) {
		return "", fmt.Errorf("valid private Instagram evidence screenshot is required")
	}
	digest := sha256.Sum256(screenshot)
	directory := filepath.Join(dataDir, "screenshots")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create Instagram evidence directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("restrict Instagram evidence directory: %w", err)
	}
	destination := filepath.Join(directory, fmt.Sprintf("instagram-job-%d-%s.png", jobID, hex.EncodeToString(digest[:8])))
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("Instagram evidence screenshot path is not a regular file")
		}
		existing, readErr := os.ReadFile(destination)
		if readErr != nil {
			return "", readErr
		}
		if bytes.Equal(existing, screenshot) {
			if chmodErr := os.Chmod(destination, 0o600); chmodErr != nil {
				return "", chmodErr
			}
			return destination, nil
		}
		return "", fmt.Errorf("Instagram evidence screenshot identity conflict")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temp, err := os.CreateTemp(directory, ".instagram-evidence-*")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return "", err
	}
	if _, err := io.Copy(temp, bytes.NewReader(screenshot)); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tempPath, destination); err != nil {
		if existing, readErr := os.ReadFile(destination); readErr == nil && bytes.Equal(existing, screenshot) {
			return destination, nil
		}
		return "", err
	}
	return destination, nil
}
