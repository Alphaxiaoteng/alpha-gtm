//go:build legacy_chromium

package publishverify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishplan"
)

const (
	DefaultXHSPollInterval        = 20 * time.Second
	DefaultXHSVerificationTimeout = 10 * time.Minute
)

var (
	ErrXHSBackendDOMUnverified = errors.New("xhs creator-backend DOM is unverified")
	ErrXHSBackendAmbiguous     = errors.New("xhs creator-backend evidence is ambiguous")
	ErrXHSThumbnailUnverified  = errors.New("xhs creator-backend thumbnail does not uniquely match frozen media")
)

type XHSExpectation = CreatorBackendExpectation
type XHSBackendItem = CreatorBackendItem
type XHSBackendSnapshot = CreatorBackendSnapshot
type XHSBackendObserver = CreatorBackendObserver

type XHSVerificationResult struct {
	State          string
	ErrorCode      string
	Attempts       int
	ScreenshotPath string
	Record         *db.PublishRecord
}

type XHSVerifier struct {
	DB           *db.DB
	Browsers     BrowserStatusProvider
	Observer     XHSBackendObserver
	PollInterval time.Duration
	Timeout      time.Duration
	Now          func() time.Time
	Sleep        func(context.Context, time.Duration) error
}

// MatchXHSBackend additionally requires a unique matching note. A shared
// launch title is never enough to select one of two creator-backend cards.
func MatchXHSBackend(expected XHSExpectation, snapshot XHSBackendSnapshot) (XHSBackendItem, bool) {
	if expected.PlatformCode == "" {
		expected.PlatformCode = "xhs"
	}
	if expected.PlatformCode != "xhs" || snapshot.PlatformCode != "xhs" {
		return XHSBackendItem{}, false
	}
	matches := make([]CreatorBackendItem, 0, 1)
	for _, item := range snapshot.Items {
		candidate := snapshot
		candidate.Items = []CreatorBackendItem{item}
		if matched, ok := MatchCreatorBackend(expected, candidate); ok {
			matches = append(matches, matched)
		}
	}
	if len(matches) != 1 {
		return XHSBackendItem{}, false
	}
	return matches[0], true
}

func (v *XHSVerifier) Verify(ctx context.Context, jobID int64) (*XHSVerificationResult, error) {
	if v == nil || v.DB == nil || v.Browsers == nil || jobID <= 0 {
		return nil, fmt.Errorf("xhs verifier requires database, BrowserFacade status, and job id")
	}
	expected, err := v.expectation(jobID)
	if err != nil {
		return nil, err
	}
	status, statusErr := v.Browsers.Status(expected.AccountID)
	cdpURL, cdpErr := publishguard.LiveCDPURL(expected.AccountID, status, statusErr)
	if cdpErr != nil {
		return &XHSVerificationResult{
			State: VerificationManualTakeover, ErrorCode: "XHS_BACKEND_BROWSER_UNAVAILABLE",
		}, nil
	}
	observer := v.Observer
	if observer == nil {
		observer = ChromedpXHSBackendObserver{}
	}
	interval := v.PollInterval
	if interval <= 0 {
		interval = DefaultXHSPollInterval
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = DefaultXHSVerificationTimeout
	}
	deadline := v.now().Add(timeout)
	expected.PublishedBefore = deadline
	attempts := 0
	lastCode := ""
	for {
		attempts++
		snapshot, observeErr := observer.Observe(ctx, cdpURL, expected)
		switch {
		case errors.Is(observeErr, ErrXHSBackendDOMUnverified):
			return &XHSVerificationResult{
				State: VerificationManualTakeover, ErrorCode: "XHS_BACKEND_DOM_UNVERIFIED", Attempts: attempts,
			}, nil
		case errors.Is(observeErr, ErrXHSBackendAmbiguous):
			lastCode = "XHS_PUBLISHED_EVIDENCE_AMBIGUOUS"
		case errors.Is(observeErr, ErrXHSThumbnailUnverified):
			lastCode = "XHS_BACKEND_THUMBNAIL_UNVERIFIED"
		case observeErr != nil:
			lastCode = "XHS_BACKEND_OBSERVE_FAILED"
		default:
			if item, matched := MatchXHSBackend(expected, snapshot); matched {
				screenshotPath, saveErr := saveCreatorBackendEvidenceScreenshot(
					v.DB.DataDir, "xhs", jobID, snapshot.ScreenshotPNG,
				)
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
				return &XHSVerificationResult{
					State: VerificationPublished, Attempts: attempts,
					ScreenshotPath: screenshotPath, Record: record,
				}, nil
			}
		}
		current := v.now()
		if !current.Before(deadline) {
			if lastCode == "" {
				lastCode = "XHS_PUBLISHED_EVIDENCE_NOT_FOUND"
			}
			return &XHSVerificationResult{
				State: VerificationPending, ErrorCode: lastCode, Attempts: attempts,
			}, nil
		}
		wait := interval
		if remaining := deadline.Sub(current); wait > remaining {
			wait = remaining
		}
		if err := v.sleep(ctx, wait); err != nil {
			return &XHSVerificationResult{
				State: VerificationPending, ErrorCode: "XHS_VERIFICATION_INTERRUPTED", Attempts: attempts,
			}, err
		}
	}
}

func (v *XHSVerifier) expectation(jobID int64) (XHSExpectation, error) {
	job, err := v.DB.GetPublishJob(jobID)
	if err != nil {
		return XHSExpectation{}, err
	}
	if job.PlatformCode != "xhs" || job.PlanID <= 0 || job.PlanTargetID <= 0 ||
		job.Mode != "submit" || strings.TrimSpace(job.SubmitAttemptedAt) == "" {
		return XHSExpectation{}, fmt.Errorf("xhs verification requires a submitted frozen XHS job")
	}
	switch job.Status {
	case "awaiting_review", "manual_takeover", "failed":
	default:
		return XHSExpectation{}, fmt.Errorf("xhs verification job is not awaiting creator-backend evidence")
	}
	submittedAt, err := parseSubmitTime(job.SubmitAttemptedAt)
	if err != nil {
		return XHSExpectation{}, fmt.Errorf("xhs submit time is invalid")
	}
	clickedAt, submitTargetID, err := verifiedXHSSubmitClick(v.DB, job, submittedAt)
	if err != nil {
		return XHSExpectation{}, err
	}
	plan, err := (&publishplan.Service{DB: v.DB}).Get(job.PlanID)
	if err != nil {
		return XHSExpectation{}, err
	}
	if len(plan.Snapshot.Targets) != 1 {
		return XHSExpectation{}, fmt.Errorf("xhs verification requires one frozen target")
	}
	if plan.SnapshotHash != job.SnapshotHash || plan.ContentID != job.ContentID ||
		plan.MediaMode != job.ContentType {
		return XHSExpectation{}, fmt.Errorf("xhs verification frozen snapshot does not match job")
	}
	target := plan.Snapshot.Targets[0]
	if target.AccountID != job.AccountID || target.AssetID != job.AssetID ||
		target.PlatformCode != "xhs" || target.AccountPlatformCode != "xhs" {
		return XHSExpectation{}, fmt.Errorf("xhs verification frozen target does not match job")
	}
	asset, err := v.DB.GetAccountAsset(job.AssetID)
	if err != nil {
		return XHSExpectation{}, err
	}
	if err := publishguard.ValidateTargetOwnership(v.DB, asset, "xhs"); err != nil ||
		asset.AccountID != job.AccountID ||
		asset.AssetType != target.AssetType ||
		asset.Name != target.AssetName ||
		asset.ExternalID != target.AssetExternalID ||
		strings.TrimSpace(target.AssetName) == "" ||
		strings.TrimSpace(target.AssetExternalID) == "" {
		return XHSExpectation{}, fmt.Errorf("xhs verification target identity is invalid")
	}
	hashes := make([]string, 0, len(plan.Snapshot.Media))
	media := make([]CreatorBackendMedia, 0, len(plan.Snapshot.Media))
	imageCount := 0
	for _, item := range plan.Snapshot.Media {
		digest := strings.ToLower(strings.TrimSpace(item.SHA256))
		if !validSHA256(digest) {
			return XHSExpectation{}, fmt.Errorf("xhs verification frozen media hash is invalid")
		}
		hashes = append(hashes, digest)
		frozen := CreatorBackendMedia{
			Kind: item.Kind, Role: item.Role, FilePath: item.FilePath, SHA256: digest,
		}
		if item.Kind == "image" {
			if err := verifyFrozenLocalMedia(frozen); err != nil {
				return XHSExpectation{}, err
			}
			imageCount++
		}
		media = append(media, frozen)
	}
	if imageCount == 0 {
		return XHSExpectation{}, fmt.Errorf("xhs verification requires a frozen local cover or image")
	}
	return XHSExpectation{
		JobID: job.ID, AccountID: job.AccountID, AssetID: job.AssetID,
		PlatformCode: "xhs", ContentType: job.ContentType,
		TargetName: target.AssetName, TargetExternalID: target.AssetExternalID,
		Title: target.Title, Caption: target.Caption, Body: target.Body,
		MediaSHA256: hashes, FrozenMedia: media, SubmitTargetID: submitTargetID,
		SubmittedAt: clickedAt.UTC(),
	}, nil
}

func verifiedXHSSubmitClick(database *db.DB, job *db.PublishJob, attemptedAt time.Time) (time.Time, string, error) {
	events, err := database.ListJobEventsByJob(job.ID)
	if err != nil {
		return time.Time{}, "", err
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		if strings.TrimSpace(event.RawJSON) == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if len(event.RawJSON) > 64<<10 || json.Unmarshal([]byte(event.RawJSON), &raw) != nil {
			return time.Time{}, "", fmt.Errorf("xhs submit click evidence is invalid")
		}
		if _, present := raw["submitClicked"]; !present {
			continue
		}
		var evidence struct {
			SubmitAttempted bool   `json:"submitAttempted"`
			SubmitClicked   bool   `json:"submitClicked"`
			SubmitClickedAt string `json:"submitClickedAt"`
			SubmitTargetID  string `json:"submitTargetId"`
		}
		if json.Unmarshal([]byte(event.RawJSON), &evidence) != nil ||
			!evidence.SubmitAttempted || !evidence.SubmitClicked ||
			!validXHSTargetID(strings.TrimSpace(evidence.SubmitTargetID)) {
			return time.Time{}, "", fmt.Errorf("xhs verification requires durable actual-click evidence")
		}
		clickedAt, err := parseSubmitTime(strings.TrimSpace(evidence.SubmitClickedAt))
		if err != nil || clickedAt.Before(attemptedAt) || clickedAt.After(attemptedAt.Add(5*time.Minute)) {
			return time.Time{}, "", fmt.Errorf("xhs submit click time is invalid")
		}
		return clickedAt.UTC(), strings.TrimSpace(evidence.SubmitTargetID), nil
	}
	return time.Time{}, "", fmt.Errorf("xhs verification requires durable actual-click evidence")
}

func parseSubmitTime(raw string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return value, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func (v *XHSVerifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

func (v *XHSVerifier) sleep(ctx context.Context, duration time.Duration) error {
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

func parseXHSDataImpression(raw string) (string, string, error) {
	value, err := decodeXHSDataImpression(raw)
	if err != nil {
		return "", "", err
	}
	noteIDs := map[string]struct{}{}
	contentTypes := map[string]struct{}{}
	invalidContentType := false
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				normalizedKey := strings.ToLower(strings.TrimSpace(key))
				if text, ok := nested.(string); ok {
					text = strings.TrimSpace(text)
					switch normalizedKey {
					case "noteid", "note_id":
						if validXHSNoteID(text) {
							noteIDs[text] = struct{}{}
						}
					case "notetype", "note_type":
						if contentType := normalizeXHSContentType(text); contentType != "" {
							contentTypes[contentType] = struct{}{}
						} else {
							invalidContentType = true
						}
					}
				}
				visit(nested)
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		}
	}
	visit(value)
	if len(noteIDs) != 1 || len(contentTypes) > 1 || invalidContentType {
		return "", "", fmt.Errorf("xhs data-impression identity or content type is ambiguous")
	}
	var noteID, contentType string
	for value := range noteIDs {
		noteID = value
	}
	for value := range contentTypes {
		contentType = value
	}
	return noteID, contentType, nil
}

func decodeXHSDataImpression(raw string) (any, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, fmt.Errorf("xhs data-impression is missing or too large")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode xhs data-impression: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode xhs data-impression: trailing data")
	}
	return value, nil
}

func parseXHSDataImpressionChannel(raw string) (string, error) {
	value, err := decodeXHSDataImpression(raw)
	if err != nil {
		return "", err
	}
	root, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("xhs data-impression root is not an object")
	}
	index, ok := root["index"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("xhs data-impression index is missing")
	}
	indexValue, ok := index["value"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("xhs data-impression index value is missing")
	}
	channel, ok := indexValue["channelTabName"].(string)
	channel = strings.TrimSpace(channel)
	if !ok || channel == "" || len(channel) > 32 {
		return "", fmt.Errorf("xhs data-impression channel is invalid")
	}
	return channel, nil
}

func validXHSNoteID(value string) bool {
	if len(value) < 16 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func normalizeXHSContentType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "video", "视频":
		return "video"
	case "normal", "image", "images", "graphic", "图文":
		return "graph_text"
	default:
		return ""
	}
}

func verifyFrozenLocalMedia(media CreatorBackendMedia) error {
	path := strings.TrimSpace(media.FilePath)
	if media.Kind != "image" || !filepath.IsAbs(path) || !validSHA256(media.SHA256) {
		return fmt.Errorf("xhs verification frozen local image is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > 50<<20 {
		return fmt.Errorf("xhs verification frozen local image is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open xhs frozen image: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("hash xhs frozen image: %w", err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != strings.ToLower(media.SHA256) {
		return fmt.Errorf("xhs verification frozen local image hash changed")
	}
	return nil
}

func matchXHSThumbnail(thumbnailPNG []byte, media []CreatorBackendMedia) (string, error) {
	if !validPNG(thumbnailPNG) {
		return "", ErrXHSThumbnailUnverified
	}
	thumbnail, _, err := image.Decode(bytes.NewReader(thumbnailPNG))
	if err != nil {
		return "", ErrXHSThumbnailUnverified
	}
	thumbnailSignature, err := makeImageSignature(thumbnail)
	if err != nil {
		return "", ErrXHSThumbnailUnverified
	}
	matches := make([]string, 0, 1)
	for _, frozen := range media {
		if frozen.Kind != "image" {
			continue
		}
		if err := verifyFrozenLocalMedia(frozen); err != nil {
			return "", err
		}
		file, err := os.Open(frozen.FilePath)
		if err != nil {
			return "", err
		}
		source, _, decodeErr := image.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil || closeErr != nil {
			return "", ErrXHSThumbnailUnverified
		}
		sourceSignature, signatureErr := makeImageSignature(source)
		if signatureErr != nil {
			return "", ErrXHSThumbnailUnverified
		}
		matched := similarImageSignatures(sourceSignature, thumbnailSignature)
		if !matched {
			if cropped := centeredAspectCrop(source, thumbnail.Bounds()); cropped != nil {
				croppedSignature, croppedErr := makeImageSignature(cropped)
				if croppedErr != nil {
					return "", ErrXHSThumbnailUnverified
				}
				matched = similarImageSignatures(croppedSignature, thumbnailSignature)
			}
		}
		if matched {
			matches = append(matches, strings.ToLower(frozen.SHA256))
		}
	}
	if len(matches) != 1 {
		return "", ErrXHSThumbnailUnverified
	}
	return matches[0], nil
}

func centeredAspectCrop(source image.Image, target image.Rectangle) image.Image {
	sourceBounds := source.Bounds()
	if target.Dx() <= 0 || target.Dy() <= 0 {
		return nil
	}
	sourceRatio := float64(sourceBounds.Dx()) / float64(sourceBounds.Dy())
	targetRatio := float64(target.Dx()) / float64(target.Dy())
	if sourceRatio > targetRatio {
		width := int(float64(sourceBounds.Dy()) * targetRatio)
		if width <= 0 || width >= sourceBounds.Dx() {
			return nil
		}
		left := sourceBounds.Min.X + (sourceBounds.Dx()-width)/2
		return croppedImage{Image: source, bounds: image.Rect(left, sourceBounds.Min.Y, left+width, sourceBounds.Max.Y)}
	}
	height := int(float64(sourceBounds.Dx()) / targetRatio)
	if height <= 0 || height >= sourceBounds.Dy() {
		return nil
	}
	top := sourceBounds.Min.Y + (sourceBounds.Dy()-height)/2
	return croppedImage{Image: source, bounds: image.Rect(sourceBounds.Min.X, top, sourceBounds.Max.X, top+height)}
}

type croppedImage struct {
	image.Image
	bounds image.Rectangle
}

func (value croppedImage) Bounds() image.Rectangle {
	return value.bounds
}

type imageSignature struct {
	tiles [8 * 8 * 3]uint8
	dhash uint64
}

func makeImageSignature(source image.Image) (imageSignature, error) {
	bounds := source.Bounds()
	if bounds.Dx() < 8 || bounds.Dy() < 8 || bounds.Dx() > 20000 || bounds.Dy() > 20000 {
		return imageSignature{}, fmt.Errorf("image dimensions are outside verifier bounds")
	}
	var signature imageSignature
	for tileY := 0; tileY < 8; tileY++ {
		y0 := bounds.Min.Y + tileY*bounds.Dy()/8
		y1 := bounds.Min.Y + (tileY+1)*bounds.Dy()/8
		for tileX := 0; tileX < 8; tileX++ {
			x0 := bounds.Min.X + tileX*bounds.Dx()/8
			x1 := bounds.Min.X + (tileX+1)*bounds.Dx()/8
			var red, green, blue uint64
			var count uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, b, _ := source.At(x, y).RGBA()
					red += uint64(r >> 8)
					green += uint64(g >> 8)
					blue += uint64(b >> 8)
					count++
				}
			}
			if count == 0 {
				return imageSignature{}, fmt.Errorf("image tile is empty")
			}
			index := (tileY*8 + tileX) * 3
			signature.tiles[index] = uint8(red / count)
			signature.tiles[index+1] = uint8(green / count)
			signature.tiles[index+2] = uint8(blue / count)
		}
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			left := sampledLuma(source, bounds.Min.X+x*bounds.Dx()/9, bounds.Min.Y+y*bounds.Dy()/8)
			right := sampledLuma(source, bounds.Min.X+(x+1)*bounds.Dx()/9, bounds.Min.Y+y*bounds.Dy()/8)
			if left > right {
				signature.dhash |= 1 << uint(y*8+x)
			}
		}
	}
	return signature, nil
}

func sampledLuma(source image.Image, x, y int) uint32 {
	r, g, b, _ := source.At(x, y).RGBA()
	return 299*(r>>8) + 587*(g>>8) + 114*(b>>8)
}

func similarImageSignatures(left, right imageSignature) bool {
	var distance int
	for bits := left.dhash ^ right.dhash; bits != 0; bits &= bits - 1 {
		distance++
	}
	if distance > 8 {
		return false
	}
	var total int
	for index := range left.tiles {
		delta := int(left.tiles[index]) - int(right.tiles[index])
		if delta < 0 {
			delta = -delta
		}
		total += delta
	}
	return total/len(left.tiles) <= 18
}

func saveCreatorBackendEvidenceScreenshot(dataDir, platform string, jobID int64, screenshot []byte) (string, error) {
	platform = strings.TrimSpace(platform)
	if strings.TrimSpace(dataDir) == "" || platform == "" || jobID <= 0 || !validPNG(screenshot) {
		return "", fmt.Errorf("valid private creator-backend evidence screenshot is required")
	}
	digest := sha256.Sum256(screenshot)
	directory := filepath.Join(dataDir, "screenshots")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create creator-backend evidence directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("restrict creator-backend evidence directory: %w", err)
	}
	destination := filepath.Join(directory, fmt.Sprintf("%s-job-%d-%s.png", platform, jobID, hex.EncodeToString(digest[:8])))
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("creator-backend evidence path is not a regular file")
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
		return "", fmt.Errorf("creator-backend evidence identity conflict")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temp, err := os.CreateTemp(directory, "."+platform+"-evidence-*")
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
