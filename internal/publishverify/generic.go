//go:build legacy_chromium

package publishverify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishplan"
)

const (
	DefaultCreatorBackendPollInterval = 20 * time.Second
	DefaultCreatorBackendTimeout      = 10 * time.Minute
)

// GenericVerifier turns a reviewed, platform-specific manager observation
// into the durable published transition. It deliberately has no DOM logic:
// platforms without an injected reviewed observer stop at manual takeover.
type GenericVerifier struct {
	DB           *db.DB
	Browsers     BrowserStatusProvider
	PlatformCode string
	Observer     CreatorBackendObserver
	PollInterval time.Duration
	Timeout      time.Duration
	Now          func() time.Time
	Sleep        func(context.Context, time.Duration) error
}

type GenericVerificationResult struct {
	State          string
	ErrorCode      string
	Attempts       int
	ScreenshotPath string
	Record         *db.PublishRecord
}

func (v *GenericVerifier) Verify(ctx context.Context, jobID int64) (*GenericVerificationResult, error) {
	if v == nil || v.DB == nil || v.Browsers == nil || jobID <= 0 {
		return nil, fmt.Errorf("creator-backend verifier requires database, BrowserFacade status, and job id")
	}
	expected, err := v.expectation(jobID)
	if err != nil {
		return nil, err
	}
	status, statusErr := v.Browsers.Status(expected.AccountID)
	cdpURL, cdpErr := publishguard.LiveCDPURL(expected.AccountID, status, statusErr)
	if cdpErr != nil {
		return &GenericVerificationResult{State: VerificationManualTakeover, ErrorCode: v.code("BACKEND_BROWSER_UNAVAILABLE")}, nil
	}
	observer := v.Observer
	if observer == nil {
		observer = UnverifiedCreatorBackendObserver{PlatformCode: expected.PlatformCode}
	}
	interval := v.PollInterval
	if interval <= 0 {
		interval = DefaultCreatorBackendPollInterval
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = DefaultCreatorBackendTimeout
	}
	deadline := v.now().Add(timeout)
	expected.PublishedBefore = deadline
	lastCode := ""
	for attempts := 1; ; attempts++ {
		snapshot, observeErr := observer.Observe(ctx, cdpURL, expected)
		switch {
		case errors.Is(observeErr, ErrCreatorBackendDOMUnverified):
			return &GenericVerificationResult{State: VerificationManualTakeover, ErrorCode: v.code("BACKEND_DOM_UNVERIFIED"), Attempts: attempts}, nil
		case observeErr != nil:
			lastCode = v.code("BACKEND_OBSERVE_FAILED")
		default:
			if item, matched := matchUniqueCreatorBackend(expected, snapshot); matched {
				screenshotPath, saveErr := saveCreatorBackendEvidenceScreenshot(v.DB.DataDir, expected.PlatformCode, jobID, snapshot.ScreenshotPNG)
				if saveErr != nil {
					return nil, saveErr
				}
				record, recordErr := v.DB.RecordCreatorBackendPublished(jobID, db.CreatorBackendEvidence{
					ObservedStatus: item.Status, PostURL: item.PostURL, ExternalPostID: item.ExternalPostID, ScreenshotPath: screenshotPath,
				})
				if recordErr != nil {
					return nil, recordErr
				}
				return &GenericVerificationResult{State: VerificationPublished, Attempts: attempts, ScreenshotPath: screenshotPath, Record: record}, nil
			}
		}
		now := v.now()
		if !now.Before(deadline) {
			if lastCode == "" {
				lastCode = v.code("PUBLISHED_EVIDENCE_NOT_FOUND")
			}
			return &GenericVerificationResult{State: VerificationPending, ErrorCode: lastCode, Attempts: attempts}, nil
		}
		wait := interval
		if remaining := deadline.Sub(now); wait > remaining {
			wait = remaining
		}
		if sleepErr := v.sleep(ctx, wait); sleepErr != nil {
			return &GenericVerificationResult{State: VerificationPending, ErrorCode: v.code("VERIFICATION_INTERRUPTED"), Attempts: attempts}, sleepErr
		}
	}
}

func (v *GenericVerifier) expectation(jobID int64) (CreatorBackendExpectation, error) {
	job, err := v.DB.GetPublishJob(jobID)
	if err != nil {
		return CreatorBackendExpectation{}, err
	}
	platformCode := strings.TrimSpace(v.PlatformCode)
	if _, ok := creatorBackendPlatformContracts()[platformCode]; !ok || job.PlatformCode != platformCode ||
		job.PlanID <= 0 || job.PlanTargetID <= 0 || job.Mode != "submit" || strings.TrimSpace(job.SubmitAttemptedAt) == "" {
		return CreatorBackendExpectation{}, fmt.Errorf("creator-backend verification requires a submitted frozen %s job", platformCode)
	}
	switch job.Status {
	case "awaiting_review", "manual_takeover", "failed":
	default:
		return CreatorBackendExpectation{}, fmt.Errorf("creator-backend verification job is not awaiting evidence")
	}
	attemptedAt, err := parseSubmitTime(job.SubmitAttemptedAt)
	if err != nil {
		return CreatorBackendExpectation{}, fmt.Errorf("creator-backend submit time is invalid")
	}
	clickedAt, submitTargetID, err := verifiedXHSSubmitClick(v.DB, job, attemptedAt)
	if err != nil {
		return CreatorBackendExpectation{}, err
	}
	plan, err := (&publishplan.Service{DB: v.DB}).Get(job.PlanID)
	if err != nil || plan.SnapshotHash != job.SnapshotHash || plan.ContentID != job.ContentID || plan.MediaMode != job.ContentType {
		return CreatorBackendExpectation{}, fmt.Errorf("creator-backend frozen snapshot does not match job")
	}
	var target *publishplan.TargetSnapshot
	for index := range plan.Snapshot.Targets {
		candidate := &plan.Snapshot.Targets[index]
		if candidate.AccountID == job.AccountID && candidate.AssetID == job.AssetID && candidate.PlatformCode == platformCode {
			if target != nil {
				return CreatorBackendExpectation{}, fmt.Errorf("creator-backend frozen target is ambiguous")
			}
			target = candidate
		}
	}
	if target == nil {
		return CreatorBackendExpectation{}, fmt.Errorf("creator-backend frozen target is missing")
	}
	asset, err := v.DB.GetAccountAsset(job.AssetID)
	if err != nil || publishguard.ValidateTargetOwnership(v.DB, asset, platformCode) != nil ||
		asset.AccountID != job.AccountID || asset.AssetType != target.AssetType || asset.Name != target.AssetName ||
		asset.ExternalID != target.AssetExternalID || !asset.Enabled || strings.TrimSpace(target.AssetName) == "" || strings.TrimSpace(target.AssetExternalID) == "" {
		return CreatorBackendExpectation{}, fmt.Errorf("creator-backend target identity is invalid")
	}
	hashes := make([]string, 0, len(plan.Snapshot.Media))
	media := make([]CreatorBackendMedia, 0, len(plan.Snapshot.Media))
	for _, item := range plan.Snapshot.Media {
		digest := strings.ToLower(strings.TrimSpace(item.SHA256))
		if !validSHA256(digest) {
			return CreatorBackendExpectation{}, fmt.Errorf("creator-backend frozen media hash is invalid")
		}
		hashes = append(hashes, digest)
		media = append(media, CreatorBackendMedia{Kind: item.Kind, Role: item.Role, FilePath: item.FilePath, SHA256: digest})
	}
	if len(hashes) == 0 {
		return CreatorBackendExpectation{}, fmt.Errorf("creator-backend requires frozen media")
	}
	return CreatorBackendExpectation{
		JobID: job.ID, AccountID: job.AccountID, AssetID: job.AssetID,
		PlatformCode: platformCode, ContentType: job.ContentType,
		TargetName: target.AssetName, TargetExternalID: target.AssetExternalID,
		Title: target.Title, Caption: target.Caption, Body: target.Body,
		MediaSHA256: hashes, FrozenMedia: media, SubmitTargetID: submitTargetID, SubmittedAt: clickedAt.UTC(),
	}, nil
}

func (v *GenericVerifier) code(suffix string) string {
	return strings.ToUpper(strings.TrimSpace(v.PlatformCode)) + "_" + suffix
}

func (v *GenericVerifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

func (v *GenericVerifier) sleep(ctx context.Context, duration time.Duration) error {
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

func matchUniqueCreatorBackend(expected CreatorBackendExpectation, snapshot CreatorBackendSnapshot) (CreatorBackendItem, bool) {
	var matched CreatorBackendItem
	found := false
	for _, item := range snapshot.Items {
		candidate := snapshot
		candidate.Items = []CreatorBackendItem{item}
		current, ok := MatchCreatorBackend(expected, candidate)
		if !ok {
			continue
		}
		if found {
			return CreatorBackendItem{}, false
		}
		matched, found = current, true
	}
	return matched, found
}
