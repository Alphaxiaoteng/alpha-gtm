//go:build legacy_chromium

// Package meta implements Meta Business Suite helpers and IG/FB publishers.
package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/platforms"
)

// Named pipeline steps for Meta G2 publish (stable for logs, job_events, tests).
const (
	StepPreflight      = "preflight"
	StepOpenSuite      = "open_suite"
	StepSelectTarget   = "select_target"
	StepSelectComposer = "select_composer"
	StepUploadMedia    = "upload_media"
	StepFillCaption    = "fill_caption"
	StepCompliance     = "compliance"
	StepSchedule       = "schedule"
	StepReviewOrSubmit = "review_or_submit"
	StepResult         = "result"
	StepBrowserAttach  = "browser_attached"
	metaSessionTimeout = 3 * time.Minute
)

func init() {
	platforms.RegisterPublisher(&Publisher{platform: "instagram"})
	platforms.RegisterPublisher(&Publisher{platform: "facebook"})
}

// Publisher handles Instagram / Facebook via Business Suite (browser).
type Publisher struct {
	platform string
}

func (p *Publisher) Platform() string { return p.platform }

func (p *Publisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}

// FlowPlanner builds the ordered Meta publish step list (no CDP; unit-testable).
type FlowPlanner struct {
	HasMedia   bool
	AutoSubmit bool
}

// Steps returns the multi-step pipeline names in execution order.
// open_suite → select_composer → [upload_media] → fill_caption → review_or_submit → result
func (f FlowPlanner) Steps() []string {
	steps := []string{StepOpenSuite, StepSelectTarget, StepSelectComposer}
	if f.HasMedia {
		steps = append(steps, StepUploadMedia)
	}
	steps = append(steps, StepFillCaption, StepReviewOrSubmit, StepResult)
	return steps
}

// DefaultFlow returns a FlowPlanner for the given request.
func DefaultFlow(req platforms.PublishRequest) FlowPlanner {
	mediaPlan, err := buildMetaMediaPlan(req)
	return FlowPlanner{
		HasMedia:   err == nil && (len(mediaPlan.UploadPaths) > 0 || mediaPlan.CoverPath != ""),
		AutoSubmit: req.AutoSubmit,
	}
}

type metaMediaPlan struct {
	UploadPaths []string
	CoverPath   string
}

// buildMetaMediaPlan consumes the immutable structured media snapshot. The
// legacy MediaPath remains a fallback for older CLI jobs only.
func buildMetaMediaPlan(req platforms.PublishRequest) (metaMediaPlan, error) {
	if len(req.Media) == 0 {
		if path := strings.TrimSpace(req.MediaPath); path != "" {
			return metaMediaPlan{UploadPaths: []string{path}}, nil
		}
		return metaMediaPlan{}, nil
	}
	items := append([]platforms.PublishMedia(nil), req.Media...)
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].Position < items[right].Position
	})
	plan := metaMediaPlan{}
	seen := make(map[string]struct{}, len(items))
	videoCount := 0
	contentCount := 0
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			return metaMediaPlan{}, fmt.Errorf("structured media path required")
		}
		if _, exists := seen[path]; exists {
			return metaMediaPlan{}, fmt.Errorf("structured media paths must be unique")
		}
		seen[path] = struct{}{}
		switch item.Role {
		case "content":
			if item.Kind != "image" {
				return metaMediaPlan{}, fmt.Errorf("Meta content media must be an image")
			}
			contentCount++
			plan.UploadPaths = append(plan.UploadPaths, path)
		case "video":
			if item.Kind != "video" {
				return metaMediaPlan{}, fmt.Errorf("Meta video media must be a video")
			}
			videoCount++
			plan.UploadPaths = append(plan.UploadPaths, path)
		case "cover":
			if item.Kind != "image" || plan.CoverPath != "" {
				return metaMediaPlan{}, fmt.Errorf("Meta accepts at most one image cover")
			}
			plan.CoverPath = path
		default:
			return metaMediaPlan{}, fmt.Errorf("unsupported Meta media role %q", item.Role)
		}
	}
	if videoCount > 1 || (videoCount > 0 && contentCount > 0) {
		return metaMediaPlan{}, fmt.Errorf("Meta media must be either ordered images or one video")
	}
	if plan.CoverPath != "" && videoCount != 1 {
		return metaMediaPlan{}, fmt.Errorf("Meta cover requires one video")
	}
	return plan, nil
}

func mediaUploadFailureResult(platform, contentType string, plan metaMediaPlan, attached bool, screenshot string) *platforms.PublishResult {
	if len(plan.UploadPaths) == 0 {
		if !mediaRequired(platform, contentType) {
			return nil
		}
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "manual_takeover",
			Step:         StepUploadMedia,
			ErrorCode:    "MEDIA_REQUIRED",
			ErrorMessage: fmt.Sprintf("%s content type %q requires media", platform, contentType),
			Screenshot:   screenshot,
			Message:      "Required Meta media was not available; publish was not clicked",
		})
	}
	if attached {
		return nil
	}
	return ensureFailureDiagnostics(&platforms.PublishResult{
		Status:       "manual_takeover",
		Step:         StepUploadMedia,
		ErrorCode:    "MEDIA_UPLOAD_UNCONFIRMED",
		ErrorMessage: "Meta did not confirm the selected media upload",
		Screenshot:   screenshot,
		Message:      "Media upload could not be confirmed; publish was not clicked",
	})
}

func (p *Publisher) Validate(req platforms.PublishRequest) error {
	if req.ContentID <= 0 {
		return fmt.Errorf("content_id required")
	}
	if req.AssetID <= 0 {
		return fmt.Errorf("asset_id required")
	}
	if req.Caption == "" && req.Title == "" && req.Body == "" {
		return fmt.Errorf("content has empty title/caption/body")
	}
	if _, _, err := metaCaption("", req.TagsJSON); err != nil {
		return err
	}
	if _, err := buildMetaMediaPlan(req); err != nil {
		return err
	}
	switch p.platform {
	case "instagram":
		if req.AssetType != "" && req.AssetType != "instagram_account" {
			return fmt.Errorf("instagram requires asset_type instagram_account, got %q", req.AssetType)
		}
	case "facebook":
		if req.AssetType != "" && req.AssetType != "facebook_page" {
			return fmt.Errorf("facebook requires asset_type facebook_page, got %q", req.AssetType)
		}
	}
	if !req.DryRun && (strings.TrimSpace(req.AssetName) == "" || strings.TrimSpace(req.AssetExternalID) == "") {
		return fmt.Errorf("live prepare requires a target name and public target id")
	}
	return nil
}

func (p *Publisher) Publish(ctx context.Context, req platforms.PublishRequest) (*platforms.PublishResult, error) {
	if err := p.Validate(req); err != nil {
		return &platforms.PublishResult{
			Status:       "permanent_failed",
			Step:         StepPreflight,
			ErrorCode:    "VALIDATION_FAILED",
			ErrorMessage: err.Error(),
		}, err
	}

	mediaPlan, _ := buildMetaMediaPlan(req)
	if strings.TrimSpace(req.PublishAt) != "" {
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "manual_takeover",
			Step:         StepSchedule,
			ErrorCode:    "META_NATIVE_SCHEDULE_UNVERIFIED",
			ErrorMessage: "Meta native scheduling has not been verified",
			Message:      "Scheduled time was preserved, but immediate publish was blocked",
		}), nil
	}
	caption, metaTags, _ := metaCaption(firstNonEmpty(req.Caption, req.Body, req.Title), req.TagsJSON)
	complianceRequired, complianceEvidence, complianceErr := validateMetaFrozenCompliance(req.ComplianceJSON, caption)
	if complianceErr != nil {
		return &platforms.PublishResult{
			Status:       "permanent_failed",
			Step:         StepPreflight,
			ErrorCode:    "META_COMPLIANCE_INVALID",
			ErrorMessage: complianceErr.Error(),
			Message:      "Frozen Meta compliance did not match the approved formal policy; no browser control was touched",
		}, complianceErr
	}
	if req.DryRun {
		msg := fmt.Sprintf("dry-run ok: %s → asset %q (#%d) steps=%v",
			p.platform, req.AssetName, req.AssetID, DefaultFlow(req).Steps())
		if len(mediaPlan.UploadPaths) > 0 {
			msg += fmt.Sprintf(" media=%d", len(mediaPlan.UploadPaths))
		}
		if req.AutoSubmit {
			msg += " auto_submit=true"
		}
		if len(metaTags) > 0 {
			msg += fmt.Sprintf(" caption_hashtags=%d", len(metaTags))
		}
		return &platforms.PublishResult{
			Status:   "dry_run_ok",
			Step:     StepPreflight,
			Message:  msg,
			Evidence: mergeMetaEvidence(metaTagEvidence(metaTags), complianceEvidence),
		}, nil
	}
	if req.AutoSubmit && !complianceRequired {
		err := fmt.Errorf("META_COMPLIANCE_REQUIRED")
		return &platforms.PublishResult{
			Status:       "permanent_failed",
			Step:         StepPreflight,
			ErrorCode:    "META_COMPLIANCE_REQUIRED",
			ErrorMessage: err.Error(),
			Message:      "Meta live submit requires the complete frozen compliance policy; no browser control was touched",
		}, err
	}

	if strings.TrimSpace(req.CDPURL) == "" {
		return &platforms.PublishResult{
			Status:       "retryable_failed",
			Step:         StepBrowserAttach,
			ErrorCode:    string(errors.CDP_CONNECT),
			ErrorMessage: "CDP URL empty; start browser for account first",
		}, fmt.Errorf("%s: empty cdp url", errors.CDP_CONNECT)
	}

	session, err := attachCDP(ctx, req.CDPURL)
	if err != nil {
		return &platforms.PublishResult{
			Status:       "retryable_failed",
			Step:         StepBrowserAttach,
			ErrorCode:    string(errors.CDP_CONNECT),
			ErrorMessage: err.Error(),
		}, err
	}
	defer session.cancel()

	// ---- multi-step pipeline ----
	// open_suite → select_composer → upload_media → fill_caption → review_or_submit → result

	composerURL, ok := metaUnifiedComposerURL(p.platform, req)
	if !ok {
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "manual_takeover",
			Step:         StepSelectTarget,
			ErrorCode:    "TARGET_SELECTION_UNCONFIRMED",
			ErrorMessage: "Meta unified composer requires a verified Page public id",
			Message:      "Meta target identity was incomplete; publish was not clicked",
		}), nil
	}

	// 1) open_suite — both Page and connected Instagram publishing use the
	// Business Suite unified composer. The asset_id is always the verified Page
	// public ID; no business_id is guessed or persisted.
	if err := chromedp.Run(session.ctx,
		chromedp.Navigate(composerURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		shot := captureScreenshotBestEffort(session.ctx, StepOpenSuite+"_fail")
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "retryable_failed",
			Step:         StepOpenSuite,
			ErrorCode:    string(errors.CDP_CONNECT),
			ErrorMessage: err.Error(),
			Screenshot:   shot,
		}), err
	}
	if tryContinueMetaSession(session.ctx) {
		_ = chromedp.Run(session.ctx, chromedp.Sleep(1500*time.Millisecond))
	}

	// Every live preparation must prove the intended Page / Instagram account
	// before opening a composer. Even with auto-submit disabled, filling the
	// currently active wrong target is unsafe.
	if !trySelectMetaUnifiedTarget(session.ctx, p.platform, req) {
		shot := captureScreenshotBestEffort(session.ctx, StepSelectTarget+"_unconfirmed")
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "awaiting_review",
			Step:         StepSelectTarget,
			ErrorCode:    "TARGET_SELECTION_UNCONFIRMED",
			ErrorMessage: "Business Suite did not confirm exactly one selected publishing location",
			Screenshot:   shot,
			Message:      "The unified composer did not retain the requested single target; publish was not clicked",
		}), nil
	}

	// 3) select_composer — the target selector and editor must belong to the
	// same unified composer. Do not navigate to an Instagram tab or a second
	// composer after the single-target readback.
	composerOpened := pollMetaComposerConfirmation(
		func() bool { return runMetaComposerReadyScript(session.ctx) },
		func() { _ = chromedp.Run(session.ctx, chromedp.Sleep(500*time.Millisecond)) },
	)
	if !composerOpened {
		shot := captureScreenshotBestEffort(session.ctx, StepSelectComposer+"_unconfirmed")
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "manual_takeover",
			Step:         StepSelectComposer,
			ErrorCode:    "COMPOSER_UNCONFIRMED",
			ErrorMessage: "Meta did not expose a visible composer",
			Screenshot:   shot,
			Message:      "Create control was attempted, but the composer could not be confirmed; publish was not clicked",
		}), nil
	}
	composerShot := captureScreenshotBestEffort(session.ctx, StepSelectComposer+"_confirmed")
	if complianceRequired {
		liveEvidence, confirmed := observeMetaLiveCompliance(session.ctx, p.platform)
		if !confirmed {
			shot := captureScreenshotBestEffort(session.ctx, StepCompliance+"_unconfirmed")
			if shot == "" {
				shot = composerShot
			}
			return ensureFailureDiagnostics(&platforms.PublishResult{
				Status:       "manual_takeover",
				Step:         StepCompliance,
				ErrorCode:    "META_COMPLIANCE_STATE_UNCONFIRMED",
				ErrorMessage: "Meta live compliance controls did not match the frozen formal policy",
				Screenshot:   shot,
				Message:      "Compliance readback was unknown or unsafe; upload and publish were not attempted",
				Evidence:     complianceEvidence,
			}), nil
		}
		complianceEvidence = mergeMetaEvidence(complianceEvidence, liveEvidence)
	}

	// 4) upload_media. Structured roles are resolved before touching the page;
	// an unconfirmed upload stops here and can never fall through to submit.
	mediaAttached := false
	if len(mediaPlan.UploadPaths) > 0 {
		for _, path := range mediaPlan.UploadPaths {
			if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
				shot := captureScreenshotBestEffort(session.ctx, StepUploadMedia+"_missing")
				return ensureFailureDiagnostics(&platforms.PublishResult{
					Status:       "manual_takeover",
					Step:         StepUploadMedia,
					ErrorCode:    "MEDIA_FILE_UNAVAILABLE",
					ErrorMessage: "Meta media file is unavailable",
					Screenshot:   shot,
					Message:      "Media file was unavailable; publish was not clicked",
				}), nil
			}
		}
		mediaAttached = tryAttachMediaPaths(session.ctx, mediaPlan.UploadPaths)
		if !mediaAttached {
			shot := captureScreenshotBestEffort(session.ctx, StepUploadMedia+"_fail")
			if shot == "" {
				shot = composerShot
			}
			return mediaUploadFailureResult(p.platform, req.ContentType, mediaPlan, false, shot), nil
		}
	} else if failure := mediaUploadFailureResult(p.platform, req.ContentType, mediaPlan, false, ""); failure != nil {
		shot := captureScreenshotBestEffort(session.ctx, StepUploadMedia+"_required")
		failure.Screenshot = shot
		return failure, nil
	}
	coverMode := ""
	if mediaPlan.CoverPath != "" {
		coverAttached, coverStage := tryAttachMetaCover(session.ctx, mediaPlan.CoverPath)
		if !coverAttached {
			firstFrameFallback := coverStage == "tab" &&
				strings.Contains(strings.ToLower(filepath.Base(mediaPlan.CoverPath)), "first-frame") &&
				waitForMetaBooleanScript(session.ctx, metaDefaultReelCoverReadyScript(), time.Second)
			if !firstFrameFallback {
				shot := captureScreenshotBestEffort(session.ctx, StepUploadMedia+"_cover_"+coverStage)
				return ensureFailureDiagnostics(&platforms.PublishResult{
					Status:       "manual_takeover",
					Step:         StepUploadMedia,
					ErrorCode:    "META_COVER_UPLOAD_UNCONFIRMED",
					ErrorMessage: "Meta did not confirm the selected Reels cover at " + coverStage,
					Screenshot:   shot,
					Message:      "Video was attached, but its cover could not be confirmed; publish was not clicked",
				}), nil
			}
			coverMode = "platform_generated_frame"
		} else {
			coverMode = "custom_upload"
		}
	}

	// 5) fill_caption
	captionFilled := tryFillCaption(session.ctx, caption)
	if !captionFilled && strings.TrimSpace(caption) != "" {
		shot := captureScreenshotBestEffort(session.ctx, StepFillCaption+"_fail")
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "manual_takeover",
			Step:         StepFillCaption,
			ErrorCode:    "CAPTION_FILL_UNCONFIRMED",
			ErrorMessage: "Meta did not retain the typed caption",
			Screenshot:   shot,
			Message:      "Caption entry could not be confirmed; publish was not clicked",
		}), nil
	}

	// 6) review_or_submit (+ checkpoint screenshot)
	if isMetaVideoContent(req.ContentType) && !advanceMetaReelToFinalReview(session.ctx) {
		shot := captureScreenshotBestEffort(session.ctx, StepReviewOrSubmit+"_reel_step_fail")
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "manual_takeover",
			Step:         StepReviewOrSubmit,
			ErrorCode:    "META_REEL_REVIEW_UNCONFIRMED",
			ErrorMessage: "Meta Reels did not reach its final Share step",
			Screenshot:   shot,
			Message:      "Reels media and caption were prepared, but final review was not confirmed; Share was not clicked",
			Evidence:     metaPreparationEvidence(metaTags, coverMode, complianceEvidence),
		}), nil
	}
	if !waitForMetaPublishReady(session.ctx, req.ContentType, 15*time.Second) {
		shot := captureScreenshotBestEffort(session.ctx, StepReviewOrSubmit+"_disabled")
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:       "manual_takeover",
			Step:         StepReviewOrSubmit,
			ErrorCode:    "FINAL_CONTROL_NOT_READY",
			ErrorMessage: "Meta final publish control did not become enabled",
			Screenshot:   shot,
			Message:      "Composer fields were prepared, but final readiness was not confirmed; publish was not clicked",
			Evidence:     metaPreparationEvidence(metaTags, coverMode, complianceEvidence),
		}), nil
	}
	shot := captureScreenshotBestEffort(session.ctx, "meta_"+p.platform+"_"+StepReviewOrSubmit)

	if req.AutoSubmit {
		clicked, clearSuccess, targetOnly := tryClickPublish(session.ctx, p.platform, req)
		if !targetOnly {
			failShot := captureScreenshotBestEffort(session.ctx, StepReviewOrSubmit+"_target_changed")
			if failShot == "" {
				failShot = shot
			}
			return ensureFailureDiagnostics(&platforms.PublishResult{
				Status:       "manual_takeover",
				Step:         StepReviewOrSubmit,
				ErrorCode:    "TARGET_SELECTION_CHANGED",
				ErrorMessage: "Meta publishing destinations changed after preparation",
				Screenshot:   failShot,
				Message:      "Exact target-only readback failed at the final click edge; publish was not clicked",
				Evidence:     metaPreparationEvidence(metaTags, coverMode, complianceEvidence),
			}), nil
		}
		if clearSuccess {
			// 7) result — confirmed only with explicit success toast/copy
			finalShot := captureScreenshotBestEffort(session.ctx, StepResult+"_ok")
			if finalShot == "" {
				finalShot = shot
			}
			return &platforms.PublishResult{
				Status:     "published",
				Step:       StepResult,
				Screenshot: finalShot,
				Message:    fmt.Sprintf("%s publish confirmed after submit click", p.platform),
				Evidence:   metaPreparationEvidence(metaTags, coverMode, complianceEvidence),
			}, nil
		}
		// Uncertain outcome → never fake published
		failShot := captureScreenshotBestEffort(session.ctx, StepReviewOrSubmit+"_unknown")
		if failShot == "" {
			failShot = shot
		}
		msg := fmt.Sprintf("Business Suite opened (%s); auto-submit attempted (clicked=%v) but success not confirmed — manual takeover",
			p.platform, clicked)
		msg = appendPrepNotes(msg, composerOpened, captionFilled, mediaAttached, strings.Join(mediaPlan.UploadPaths, ","))
		return ensureFailureDiagnostics(&platforms.PublishResult{
			Status:     "manual_takeover",
			Step:       StepReviewOrSubmit,
			Screenshot: failShot,
			Message:    msg,
			ErrorCode:  "PUBLISH_RESULT_UNKNOWN",
			Evidence:   metaPreparationEvidence(metaTags, coverMode, complianceEvidence),
		}), nil
	}

	// AutoSubmit=false: stop before submit for review / human finish.
	status := "manual_takeover"
	step := StepOpenSuite
	msg := fmt.Sprintf("Business Suite opened (%s); complete publish manually (auto-submit off)", p.platform)
	if composerOpened || captionFilled || mediaAttached {
		status = "awaiting_review"
		step = StepReviewOrSubmit
		msg = fmt.Sprintf("Composer prepared best-effort (%s); awaiting review (auto-submit off)", p.platform)
	}
	msg = appendPrepNotes(msg, composerOpened, captionFilled, mediaAttached, strings.Join(mediaPlan.UploadPaths, ","))

	return ensureFailureDiagnostics(&platforms.PublishResult{
		Status:     status,
		Step:       step,
		Screenshot: shot,
		Message:    msg,
		ErrorCode:  "AWAITING_HUMAN",
		Evidence:   metaPreparationEvidence(metaTags, coverMode, complianceEvidence),
	}), nil
}

// ensureFailureDiagnostics guarantees error_code on non-success outcomes (G2 AC3).
// Does not invent screenshots — caller must capture when CDP is available.
func ensureFailureDiagnostics(res *platforms.PublishResult) *platforms.PublishResult {
	if res == nil {
		return &platforms.PublishResult{
			Status:    "permanent_failed",
			Step:      StepResult,
			ErrorCode: "PUBLISH_NIL_RESULT",
		}
	}
	switch res.Status {
	case "published", "dry_run_ok":
		return res
	}
	if strings.TrimSpace(res.ErrorCode) == "" {
		switch res.Status {
		case "manual_takeover", "awaiting_review":
			res.ErrorCode = "AWAITING_HUMAN"
		case "retryable_failed":
			res.ErrorCode = "RETRYABLE_FAILED"
		default:
			res.ErrorCode = "PUBLISH_FAILED"
		}
	}
	return res
}

// mediaRequired reports content types that cannot publish without a local file.
func mediaRequired(platform, contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if platform == "instagram" {
		switch {
		case strings.Contains(ct, "reel"), strings.Contains(ct, "video"),
			strings.Contains(ct, "image"), strings.Contains(ct, "photo"),
			strings.Contains(ct, "carousel"), ct == "graph_text":
			return true
		}
	}
	if platform == "facebook" {
		switch {
		case strings.Contains(ct, "reel"), strings.Contains(ct, "video"),
			strings.Contains(ct, "image"), strings.Contains(ct, "photo"), ct == "graph_text":
			return true
		}
	}
	return false
}

// metaCaption keeps Meta's official hashtag model explicit: Facebook and
// Instagram use hashtags inside the caption, not a separate selected topic
// token. This must never be reported as platform_topics.
func metaCaption(base, rawTags string) (string, []string, error) {
	base = strings.TrimSpace(base)
	if strings.TrimSpace(rawTags) == "" {
		rawTags = "[]"
	}
	var values []string
	if err := json.Unmarshal([]byte(rawTags), &values); err != nil {
		return "", nil, fmt.Errorf("tags must be a JSON string array")
	}

	existing := metaHashtags(base)
	added := make([]string, 0, len(values))
	for _, raw := range values {
		tag := strings.Trim(strings.TrimSpace(raw), "#")
		if tag == "" {
			continue
		}
		for _, char := range tag {
			if metaTagSeparator(char) {
				return "", nil, fmt.Errorf("invalid Meta hashtag %q", raw)
			}
		}
		key := strings.ToLower(tag)
		if _, ok := existing[key]; ok {
			continue
		}
		existing[key] = struct{}{}
		added = append(added, tag)
	}
	if len(added) == 0 {
		return base, nil, nil
	}
	hashtagLine := make([]string, len(added))
	for index, tag := range added {
		hashtagLine[index] = "#" + tag
	}
	if base == "" {
		return strings.Join(hashtagLine, " "), added, nil
	}
	return base + "\n\n" + strings.Join(hashtagLine, " "), added, nil
}

func metaHashtags(text string) map[string]struct{} {
	result := map[string]struct{}{}
	runes := []rune(text)
	for index := 0; index < len(runes); index++ {
		if runes[index] != '#' {
			continue
		}
		start := index + 1
		end := start
		for end < len(runes) && !metaTagSeparator(runes[end]) {
			end++
		}
		if end > start {
			result[strings.ToLower(string(runes[start:end]))] = struct{}{}
		}
		index = end - 1
	}
	return result
}

func metaTagSeparator(char rune) bool {
	return char == '#' || unicode.IsSpace(char) ||
		strings.ContainsRune(",，;；、。.!！?？:：()[]{}<>《》“”\"'‘’…/\\|", char)
}

func metaTagEvidence(tags []string) map[string]interface{} {
	if len(tags) == 0 {
		return nil
	}
	return map[string]interface{}{
		"tagsAppliedAs": "caption_hashtags",
		"hashtags":      tags,
	}
}

func metaPreparationEvidence(tags []string, coverMode string, compliance ...map[string]interface{}) map[string]interface{} {
	evidence := mergeMetaEvidence(metaTagEvidence(tags))
	for _, group := range compliance {
		evidence = mergeMetaEvidence(evidence, group)
	}
	if coverMode != "" {
		if evidence == nil {
			evidence = make(map[string]interface{})
		}
		evidence["coverAppliedAs"] = coverMode
	}
	if len(evidence) == 0 {
		return nil
	}
	return evidence
}

// ComposerEntryURLs returns Business Suite URLs tried for the platform/content type.
// Exported for unit tests (stable G2 contract).
func ComposerEntryURLs(platform, contentType string) []string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	isReel := strings.Contains(ct, "reel") || strings.Contains(ct, "video")
	switch platform {
	case "instagram":
		if isReel {
			return []string{
				"https://business.facebook.com/latest/reels_composer",
				"https://business.facebook.com/latest/instagram_reels_composer",
				"https://business.facebook.com/latest/composer",
				"https://business.facebook.com/latest/home",
			}
		}
		return []string{
			"https://business.facebook.com/latest/instagram_composer",
			"https://business.facebook.com/latest/composer",
			"https://business.facebook.com/latest/home",
			"https://business.facebook.com/latest/content_calendar",
		}
	default: // facebook page
		if isReel {
			return []string{
				"https://business.facebook.com/latest/reels_composer",
				"https://business.facebook.com/latest/composer",
				"https://business.facebook.com/latest/home",
			}
		}
		return []string{
			"https://business.facebook.com/latest/composer",
			"https://business.facebook.com/latest/home",
			"https://business.facebook.com/latest/content_calendar",
		}
	}
}

func metaTargetHomeURL(platform string, req platforms.PublishRequest) string {
	externalID := strings.TrimSpace(req.AssetExternalID)
	if platform == "instagram" {
		externalID = strings.TrimSpace(req.ConnectedAssetExternalID)
	}
	if externalID == "" {
		return "https://business.facebook.com/"
	}
	return "https://business.facebook.com/latest/home?asset_id=" + url.QueryEscape(externalID)
}

func metaUnifiedComposerURL(platform string, req platforms.PublishRequest) (string, bool) {
	targetID := strings.TrimSpace(req.AssetExternalID)
	pageID := targetID
	switch platform {
	case "facebook":
		if !validMetaAssetID(targetID) {
			return "", false
		}
	case "instagram":
		pageID = strings.TrimSpace(req.ConnectedAssetExternalID)
		if !validMetaAssetID(targetID) || !validMetaAssetID(pageID) {
			return "", false
		}
	default:
		return "", false
	}
	return "https://business.facebook.com/latest/composer/?asset_id=" + url.QueryEscape(pageID), true
}

func tryContinueMetaSession(ctx context.Context) bool {
	_ = chromedp.Run(ctx, chromedp.Sleep(750*time.Millisecond))
	for attempt := 0; attempt < 10; attempt++ {
		var clicked bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(metaLoginContinueScript(), &clicked)); err == nil && clicked {
			return true
		}
		_ = chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond))
	}
	return false
}

func metaLoginContinueScript() string {
	return `(() => {
  const labels = ['用 facebook 继续', 'continue with facebook'];
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  const controls = Array.from(document.querySelectorAll('button, a, [role="button"]'));
  const control = controls.find(node => {
    if (!visible(node) || node.disabled || node.getAttribute('aria-disabled') === 'true') return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || '', node.getAttribute('title') || ''].map(normalize);
    return values.some(value => labels.includes(value));
  });
  if (!control) return false;
  control.setAttribute('data-alpha-nexus-meta-login', 'true');
  try { control.click(); return true; } catch (_) { return false; }
})()`
}

func appendPrepNotes(msg string, _ bool, caption, media bool, mediaPath string) string {
	if caption {
		msg += "; caption fill attempted"
	}
	if media {
		msg += "; media attach attempted"
	} else if strings.TrimSpace(mediaPath) != "" {
		msg += fmt.Sprintf("; media path set but not attached (%q)", mediaPath)
	}
	return msg
}

type cdpSession struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func attachCDP(parent context.Context, cdpURL string) (*cdpSession, error) {
	if parent == nil {
		parent = context.Background()
	}
	targetID, err := metaBusinessTargetID(parent, cdpURL)
	if err != nil {
		return nil, err
	}
	// The BrowserFacade owns this browser and tab. Caller cancellation applies
	// only to actions; target ownership is cleared before chromedp cleanup so
	// prepare/discovery never close the user's persistent Business Suite tab.
	ownershipCtx, ownershipCancel := context.WithCancel(context.Background())
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ownershipCtx, cdpURL)
	taskCtx, cancelTask := chromedp.NewContext(allocCtx, chromedp.WithTargetID(targetID))
	actionCtx, cancelAction := context.WithTimeout(taskCtx, metaSessionTimeout)
	stopCaller := context.AfterFunc(parent, cancelAction)

	cancel := func() {
		stopCaller()
		cancelAction()
		if chromedpContext := chromedp.FromContext(taskCtx); chromedpContext != nil && chromedpContext.Target != nil {
			chromedpContext.Target.TargetID = ""
		}
		cancelTask()
		cancelAlloc()
		ownershipCancel()
	}

	return &cdpSession{ctx: actionCtx, cancel: cancel}, nil
}

func tryOpenComposer(ctx context.Context, platform, contentType string) bool {
	if clickMetaCreateControl(ctx, platform, contentType) {
		return true
	}
	// Prefer known Business Suite entry points, but a successful navigation is
	// not proof: Meta may redirect those URLs back to home.
	for _, url := range ComposerEntryURLs(platform, contentType) {
		err := chromedp.Run(ctx,
			chromedp.Navigate(url),
			chromedp.WaitReady("body", chromedp.ByQuery),
		)
		if err == nil {
			_ = chromedp.Run(ctx, chromedp.Sleep(1500*time.Millisecond))
			if runMetaComposerReadyScript(ctx) {
				return true
			}
			if clickMetaCreateControl(ctx, platform, contentType) {
				return true
			}
		}
	}
	return false
}

func clickMetaCreateControl(ctx context.Context, platform, contentType string) bool {
	var clicked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(metaCreateControlScript(platform, contentType), &clicked)); err != nil || !clicked {
		return false
	}
	return pollMetaComposerConfirmation(
		func() bool { return runMetaComposerReadyScript(ctx) },
		func() { _ = chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)) },
	)
}

func pollMetaComposerConfirmation(check func() bool, wait func()) bool {
	const attempts = 16
	for attempt := 0; attempt < attempts; attempt++ {
		if check() {
			return true
		}
		if attempt+1 < attempts {
			wait()
		}
	}
	return false
}

func runMetaComposerReadyScript(ctx context.Context) bool {
	var ready bool
	return chromedp.Run(ctx, chromedp.Evaluate(metaComposerReadyScript(), &ready)) == nil && ready
}

// metaComposerScopeResolverJS resolves the rootless Business Suite composer
// from the two live-observed semantic anchors. BODY/HTML are deliberately not
// accepted as a scope because global media and action controls are ambiguous.
const metaComposerScopeResolverJS = `
  const resolveComposerScope = () => {
    const targetComboboxes = Array.from(document.querySelectorAll('[role="combobox"]')).filter(node =>
      visible(node) &&
      node.getAttribute('contenteditable') !== 'true' &&
      !['INPUT', 'TEXTAREA'].includes(String(node.tagName || '').toUpperCase())
    );
    const editors = Array.from(document.querySelectorAll('[contenteditable="true"]')).filter(visible);
    if (targetComboboxes.length !== 1 || editors.length !== 1) return null;
    const editor = editors[0];
    for (let scope = targetComboboxes[0].parentElement; scope; scope = scope.parentElement) {
      const tag = String(scope.tagName || '').toUpperCase();
      if (tag === 'BODY' || tag === 'HTML') return null;
      if (visible(scope) && scope.contains && scope.contains(editor)) return scope;
    }
    return null;
  };`

func metaComposerReadyScript() string {
	return `(() => {
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
` + metaComposerScopeResolverJS + `
  return resolveComposerScope() !== null;
})()`
}

func metaCreateControlScript(platform, contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	labels := []string{"创建帖子", "create post"}
	if (platform == "instagram" || platform == "facebook") &&
		(strings.Contains(ct, "video") || strings.Contains(ct, "reel")) {
		labels = []string{"创建 reels", "create reels", "create reel", "创建视频", "create video"}
	}
	raw, _ := json.Marshal(labels)
	return fmt.Sprintf(`(() => {
  const labels = %s;
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  const controls = Array.from(document.querySelectorAll('button, a, [role="button"]'));
  const control = controls.find(node => {
    if (!visible(node) || node.disabled || node.getAttribute('aria-disabled') === 'true') return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || '', node.getAttribute('title') || ''].map(normalize);
    return values.some(value => labels.includes(value));
  });
  if (!control) return false;
  try { control.click(); return true; } catch (_) { return false; }
	})()`, raw)
}

type metaUnifiedTargetState struct {
	State    string   `json:"state"`
	Target   string   `json:"target"`
	Selected []string `json:"selected"`
	Combobox string   `json:"combobox"`
}

func trySelectMetaUnifiedTarget(ctx context.Context, platform string, req platforms.PublishRequest) bool {
	composerURL, ok := metaUnifiedComposerURL(platform, req)
	if !ok {
		return false
	}
	parsed, err := url.Parse(composerURL)
	if err != nil {
		return false
	}
	script, ok := metaUnifiedTargetScript(
		platform,
		req.AssetName,
		req.AssetExternalID,
		parsed.Query().Get("asset_id"),
		req.ConnectedAssetName,
		req.ConnectedAssetExternalID,
	)
	if !ok {
		return false
	}

	const attempts = 16
	for attempt := 0; attempt < attempts; attempt++ {
		var state metaUnifiedTargetState
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &state)); err != nil {
			return false
		}
		switch state.State {
		case "confirmed":
			return true
		case "opened", "changed", "loading", "combobox_mismatch":
			if attempt+1 < attempts {
				_ = chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond))
				continue
			}
		default:
			return false
		}
	}
	return false
}

// metaUnifiedTargetScript enforces one and only one publishing location in
// Meta's unified composer. It only accepts the live-observed accessibility
// contract: one visible non-editor combobox, one visible listbox, exact option
// text, aria-selected readback, and exact combobox text after the change.
func metaUnifiedTargetScript(platform, targetName, targetExternalID, pageExternalID, connectedName, connectedExternalID string) (string, bool) {
	platform = strings.TrimSpace(platform)
	targetName = strings.TrimSpace(targetName)
	targetExternalID = strings.TrimSpace(targetExternalID)
	pageExternalID = strings.TrimSpace(pageExternalID)
	connectedName = strings.TrimSpace(connectedName)
	connectedExternalID = strings.TrimSpace(connectedExternalID)
	if targetName == "" || connectedName == "" || strings.EqualFold(targetName, connectedName) ||
		!validMetaAssetID(targetExternalID) || !validMetaAssetID(pageExternalID) ||
		!validMetaAssetID(connectedExternalID) {
		return "", false
	}
	switch platform {
	case "facebook":
		if targetExternalID != pageExternalID {
			return "", false
		}
	case "instagram":
		if connectedExternalID != pageExternalID {
			return "", false
		}
	default:
		return "", false
	}
	rawTarget, err := json.Marshal(targetName)
	if err != nil {
		return "", false
	}
	rawTargetID, err := json.Marshal(targetExternalID)
	if err != nil {
		return "", false
	}
	rawPageID, err := json.Marshal(pageExternalID)
	if err != nil {
		return "", false
	}
	rawConnected, err := json.Marshal(connectedName)
	if err != nil {
		return "", false
	}
	rawConnectedID, err := json.Marshal(connectedExternalID)
	if err != nil {
		return "", false
	}
	rawPlatform, err := json.Marshal(platform)
	if err != nil {
		return "", false
	}
	connectedPlatform := "instagram"
	if platform == "instagram" {
		connectedPlatform = "facebook"
	}
	rawConnectedPlatform, err := json.Marshal(connectedPlatform)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf(`(() => {
	  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
	  const exactText = node => normalize(node && (node.innerText || node.textContent || ''));
	  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
	  const identityFields = node => {
	    const names = node && node.getAttributeNames ? node.getAttributeNames() : [];
	    const identityNames = new Set([
	      'data-asset-id', 'data-page-id', 'data-instagram-id', 'data-account-id',
	      'asset_id', 'page_id', 'instagram_id', 'account_id'
	    ]);
	    return [
	      node && node.getAttribute && node.getAttribute('href'),
	      ...names.filter(name => identityNames.has(String(name).toLowerCase())).map(name => node.getAttribute(name))
	    ].filter(value => String(value || '').trim());
	  };
	  const matchesIdentity = (node, expectedID, expectedPlatform) => {
	    const numericTokens = identityFields(node).flatMap(value => String(value || '').split(/[^0-9]+/).filter(Boolean));
	    if (numericTokens.length > 0) return numericTokens.includes(expectedID);
	    const knownPlatforms = new Set(['facebook', 'instagram']);
	    const icons = Array.from(node.querySelectorAll('img[alt]'))
	      .filter(visible)
	      .map(image => normalize(image.getAttribute('alt')))
	      .filter(label => knownPlatforms.has(label));
	    return icons.length === 1 && icons[0] === expectedPlatform;
	  };
	  const target = normalize(%s);
	  const connected = normalize(%s);
	  const targetID = %s;
	  const pageID = %s;
	  const connectedID = %s;
	  const targetPlatform = %s;
	  const connectedPlatform = %s;
	  const result = (state, selected = [], combobox = '') => ({ state, target: %s, targetID, pageID, selected, combobox });
	  if (location.hostname !== 'business.facebook.com' || !location.pathname.startsWith('/latest/composer')) return result('wrong_surface');
	  let currentPageID = '';
	  try { currentPageID = new URL(location.href).searchParams.get('asset_id') || ''; } catch (_) {}
	  if (currentPageID !== pageID) return result('wrong_asset');

	  const comboboxes = Array.from(document.querySelectorAll('[role="combobox"]')).filter(node =>
	    visible(node) &&
	    node.getAttribute('contenteditable') !== 'true' &&
	    !['INPUT', 'TEXTAREA'].includes(String(node.tagName || '').toUpperCase()) &&
	    (
	      exactText(node) === target ||
	      exactText(node) === connected ||
	      exactText(node).includes(target) ||
	      exactText(node).includes(connected)
	    )
  );
  if (comboboxes.length === 0) return result('loading');
  if (comboboxes.length !== 1) return result('ambiguous_combobox');
  const combobox = comboboxes[0];

	  const listboxes = Array.from(document.querySelectorAll('[role="listbox"]')).filter(visible);
	  if (listboxes.length === 0) {
	    if (combobox.disabled || combobox.getAttribute('aria-disabled') === 'true') return result('combobox_disabled');
	    if (combobox.getAttribute('data-alpha-nexus-target-opened') === 'true') return result('loading', [], exactText(combobox));
	    combobox.setAttribute('data-alpha-nexus-target-opened', 'true');
	    try { combobox.click(); return result('opened', [], exactText(combobox)); } catch (_) { return result('open_failed'); }
	  }
  if (listboxes.length !== 1) return result('ambiguous_listbox');

  const allOptions = Array.from(listboxes[0].querySelectorAll('[role="option"]')).filter(visible);
  const preferenceLabels = new Set(['保存偏好设置', 'save preference', 'save preferences']);
  const assetOptions = allOptions.filter(option => !preferenceLabels.has(exactText(option)));
	  const targetOptions = assetOptions.filter(option =>
	    exactText(option) === target && matchesIdentity(option, targetID, targetPlatform)
	  );
	  if (targetOptions.length !== 1) return result('target_missing');
	  const connectedOptions = assetOptions.filter(option =>
	    exactText(option) === connected && matchesIdentity(option, connectedID, connectedPlatform)
	  );
  if (connectedOptions.length !== 1) return result('connected_target_missing');
  const options = [targetOptions[0], connectedOptions[0]];
  if (assetOptions.some(option => !options.includes(option))) return result('unexpected_target_option');
  if (assetOptions.some(option => !['true', 'false'].includes(option.getAttribute('aria-selected')))) return result('selection_unknown');

  const targetOption = targetOptions[0];
  const selectedOptions = assetOptions.filter(option => option.getAttribute('aria-selected') === 'true');
  const selectedNames = selectedOptions.map(option => String(option.innerText || option.textContent || '').trim());
  if (selectedOptions.some(option => !options.includes(option))) return result('unknown_selected_target', selectedNames, exactText(combobox));
  const selectedOther = selectedOptions.find(option => option !== targetOption);
  if (selectedOther) {
    try { selectedOther.click(); return result('changed', selectedNames, exactText(combobox)); } catch (_) { return result('change_failed', selectedNames); }
  }
  if (targetOption.getAttribute('aria-selected') !== 'true') {
    try { targetOption.click(); return result('changed', selectedNames, exactText(combobox)); } catch (_) { return result('change_failed', selectedNames); }
  }
  if (selectedOptions.length !== 1) return result('multiple_selected', selectedNames, exactText(combobox));
  if (exactText(combobox) !== target) return result('combobox_mismatch', selectedNames, exactText(combobox));
  return result('confirmed', selectedNames, exactText(combobox));
	})()`, rawTarget, rawConnected, rawTargetID, rawPageID, rawConnectedID, rawPlatform, rawConnectedPlatform, rawTarget), true
}

// tryConfirmMetaTarget only returns true when the currently visible Business
// Suite UI marks the requested public target as selected. It intentionally
// does not return page text or DOM snapshots to callers, keeping browser data
// local while making the auto-submit gate deterministic.
func tryConfirmMetaTarget(ctx context.Context, platform, targetName, externalID, connectedName, connectedExternalID string) bool {
	if strings.TrimSpace(targetName) == "" || strings.TrimSpace(externalID) == "" {
		return false
	}
	if pollMetaTargetConfirmation(
		func() bool {
			return runMetaTargetScript(ctx, targetName, externalID, "selected") ||
				(platform == "instagram" && runMetaConnectedTargetScript(ctx, targetName, externalID, connectedName, connectedExternalID))
		},
		func() { _ = chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)) },
	) {
		return true
	}
	if !runMetaTargetScript(ctx, targetName, externalID, "open") {
		return false
	}
	_ = chromedp.Run(ctx, chromedp.Sleep(750*time.Millisecond))
	if !runMetaTargetScript(ctx, targetName, externalID, "choose") {
		return false
	}
	_ = chromedp.Run(ctx, chromedp.Sleep(750*time.Millisecond))
	if runMetaTargetScript(ctx, targetName, externalID, "selected") {
		return true
	}
	return platform == "instagram" &&
		runMetaConnectedTargetScript(ctx, targetName, externalID, connectedName, connectedExternalID)
}

func pollMetaTargetConfirmation(check func() bool, wait func()) bool {
	const attempts = 12
	for attempt := 0; attempt < attempts; attempt++ {
		if check() {
			return true
		}
		if attempt+1 < attempts {
			wait()
		}
	}
	return false
}

func runMetaTargetScript(ctx context.Context, targetName, externalID, action string) bool {
	script, ok := metaTargetScript(targetName, externalID, action)
	if !ok {
		return false
	}
	var result bool
	return chromedp.Run(ctx, chromedp.Evaluate(script, &result)) == nil && result
}

// metaTargetScript uses exact name + public identifier matching. The selector
// deliberately excludes arbitrary page text so a navigation label or an old
// post title cannot be mistaken for the publishing target.
func metaTargetScript(targetName, externalID, action string) (string, bool) {
	name := strings.TrimSpace(targetName)
	id := strings.TrimSpace(externalID)
	if name == "" || id == "" {
		return "", false
	}
	encodedName, err := json.Marshal(name)
	if err != nil {
		return "", false
	}
	encodedID, err := json.Marshal(id)
	if err != nil {
		return "", false
	}
	const shared = `
  const normalize = value => String(value || '').trim().toLowerCase().replace(/\s+/g, ' ');
  const targetName = normalize(%s);
  const targetID = normalize(%s);
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  const values = node => [node.innerText || node.textContent || '', node.getAttribute('aria-label') || '', node.getAttribute('title') || '', node.getAttribute('href') || '', node.id || '', ...node.getAttributeNames().map(name => node.getAttribute(name) || '')];
  const matchesTarget = node => {
    if (!visible(node)) return false;
    const fields = values(node).map(normalize);
    const exactName = fields.some(value => value === targetName);
	  const exactID = fields.some(value => value.includes(targetID));
	  return exactName && exactID;
	};`
	preamble := fmt.Sprintf(shared, encodedName, encodedID)
	switch action {
	case "selected":
		return fmt.Sprintf(`(() => {%s
  const selected = Array.from(document.querySelectorAll('[aria-current="page"], [aria-current="true"], [aria-selected="true"], [aria-checked="true"]'));
  if (selected.some(matchesTarget)) return true;
  let currentAssetID = '';
  try { currentAssetID = normalize(new URL(location.href).searchParams.get('asset_id')); } catch (_) {}
  if (currentAssetID !== targetID) return false;
  const identity = Array.from(document.querySelectorAll('h1, h2, [role="heading"], header *, [role="banner"] *, [role="navigation"] *, button *, a[href] *'));
  return identity.some(node => visible(node) && values(node).map(normalize).some(value => value === targetName));
})()`, preamble), true
	case "open":
		return fmt.Sprintf(`(() => {%s
  const hints = ['page', 'account', 'instagram', 'switch', 'business', '主页', '账号', '帐户', '切换'];
  const controls = Array.from(document.querySelectorAll('button, [role="button"], [aria-haspopup="menu"], [aria-haspopup="listbox"]'));
  const control = controls.find(node => {
    if (!visible(node) || node.disabled || node.getAttribute('aria-disabled') === 'true') return false;
    const label = values(node).map(normalize).join(' ');
    return hints.some(hint => label.includes(hint));
  });
  if (!control) return false;
  try { control.click(); return true; } catch (_) { return false; }
})()`, preamble), true
	case "choose":
		return fmt.Sprintf(`(() => {%s
  const candidates = Array.from(document.querySelectorAll('[role="option"], [role="menuitem"], [role="treeitem"], button, [role="button"], a[href]'));
  const candidate = candidates.find(matchesTarget);
  if (!candidate || candidate.disabled || candidate.getAttribute('aria-disabled') === 'true') return false;
  try { candidate.click(); return true; } catch (_) { return false; }
})()`, preamble), true
	default:
		return "", false
	}
}

func runMetaConnectedTargetScript(ctx context.Context, targetName, externalID, connectedName, connectedExternalID string) bool {
	script, ok := metaConnectedTargetScript(targetName, externalID, connectedName, connectedExternalID)
	if !ok {
		return false
	}
	var result bool
	return chromedp.Run(ctx, chromedp.Evaluate(script, &result)) == nil && result
}

// metaConnectedTargetScript confirms an Instagram target through the exact
// Facebook Page relationship discovered from Meta's own bootstrap data. The
// visible Page name is not a gate because Business Suite truncates it; the
// exact asset_id plus the visible Instagram association are the stable proof.
// It is intentionally unavailable without all four public identity fields.
func metaConnectedTargetScript(targetName, externalID, connectedName, connectedExternalID string) (string, bool) {
	values := []string{
		strings.TrimSpace(targetName),
		strings.TrimSpace(externalID),
		strings.TrimSpace(connectedName),
		strings.TrimSpace(connectedExternalID),
	}
	for _, value := range values {
		if value == "" {
			return "", false
		}
	}
	encoded := make([][]byte, len(values))
	for index, value := range values {
		item, err := json.Marshal(value)
		if err != nil {
			return "", false
		}
		encoded[index] = item
	}
	return fmt.Sprintf(`(() => {
  const normalize = value => String(value || '').trim().toLowerCase().replace(/\s+/g, ' ');
  const instagramName = normalize(%s);
  const instagramID = normalize(%s);
  const connectedPageName = normalize(%s);
  const connectedPageID = normalize(%s);
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  const values = node => [node.innerText || node.textContent || '', node.getAttribute('aria-label') || '', node.getAttribute('title') || '', node.getAttribute('href') || '', ...node.getAttributeNames().map(name => node.getAttribute(name) || '')].map(normalize);
  let currentAssetID = '';
	  try { currentAssetID = normalize(new URL(location.href).searchParams.get('asset_id')); } catch (_) {}
	  if (currentAssetID !== connectedPageID) return false;
  const instagram = Array.from(document.querySelectorAll('a[href*="instagram.com"], a, button, [role="link"], [aria-label*="Instagram"], [aria-label*="instagram"], [title*="Instagram"], [title*="instagram"]'));
  const explicitAssociationLabels = ['instagram', '前往 instagram', 'go to instagram', 'view instagram'];
  return instagram.some(node => {
    if (!visible(node)) return false;
    const fields = values(node);
    return fields.some(value => explicitAssociationLabels.includes(value) || value.includes(instagramName) || value.includes(instagramID));
  });
})()`, encoded[0], encoded[1], encoded[2], encoded[3]), true
}

func tryAttachMediaPaths(ctx context.Context, mediaPaths []string) bool {
	if len(mediaPaths) == 0 {
		return false
	}
	attachmentTimeout := metaMediaAttachmentTimeout(mediaPaths)
	var marked bool
	if !waitForMetaBooleanScript(ctx, metaMediaControlScript(), 30*time.Second) {
		return false
	}
	if setMetaChooserFiles(ctx, `[data-alpha-nexus-upload-control="true"]`, mediaPaths) &&
		waitForMetaMediaAttachment(ctx, mediaPaths, attachmentTimeout) {
		return true
	}

	// Some older Business Suite variants keep a persistent file input instead
	// of emitting a transient chooser. Retain that narrow fallback.
	_ = chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond))
	if err := chromedp.Run(ctx, chromedp.Evaluate(metaMarkMediaInputScript(), &marked)); err != nil || !marked {
		return false
	}
	for _, selector := range []string{
		`input[data-alpha-nexus-upload-target="true"]`,
		`[role="dialog"] input[type="file"]`,
		`form input[type="file"]`,
	} {
		err := chromedp.Run(ctx,
			chromedp.SetUploadFiles(selector, mediaPaths, chromedp.ByQuery),
			chromedp.Sleep(2*time.Second),
		)
		if err != nil {
			continue
		}
		// input.files only proves that CDP assigned local files. The upload
		// is complete only after the same composer exposes stable previews.
		if waitForMetaMediaAttachment(ctx, mediaPaths, attachmentTimeout) {
			return true
		}
	}
	return false
}

func metaMediaAttachmentTimeout(mediaPaths []string) time.Duration {
	for _, mediaPath := range mediaPaths {
		switch strings.ToLower(filepath.Ext(mediaPath)) {
		case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".flv":
			// ponytail: Meta processes even tiny Reels asynchronously; keep one
			// bounded native-evidence wait instead of adding a job-side poller.
			return 90 * time.Second
		}
	}
	return 20 * time.Second
}

func setMetaChooserFiles(ctx context.Context, controlSelector string, paths []string) bool {
	chooserEvents := make(chan *page.EventFileChooserOpened, 1)
	chromedp.ListenTarget(ctx, func(event any) {
		chooser, ok := event.(*page.EventFileChooserOpened)
		if !ok {
			return
		}
		select {
		case chooserEvents <- chooser:
		default:
		}
	})
	intercept := chromedp.ActionFunc(func(ctx context.Context) error {
		return page.SetInterceptFileChooserDialog(true).Do(ctx)
	})
	stopIntercept := chromedp.ActionFunc(func(ctx context.Context) error {
		return page.SetInterceptFileChooserDialog(false).Do(ctx)
	})
	if err := chromedp.Run(ctx, intercept,
		chromedp.Click(controlSelector, chromedp.ByQuery),
	); err != nil {
		_ = chromedp.Run(ctx, stopIntercept)
		return false
	}
	chooser, ok := waitForMetaFileChooser(chooserEvents, 3*time.Second)
	if !ok {
		_ = chromedp.Run(ctx, stopIntercept)
		return false
	}
	setFiles := chromedp.ActionFunc(func(ctx context.Context) error {
		return dom.SetFileInputFiles(paths).
			WithBackendNodeID(chooser.BackendNodeID).
			Do(ctx)
	})
	return chromedp.Run(ctx, setFiles, stopIntercept) == nil
}

func waitForMetaFileChooser(events <-chan *page.EventFileChooserOpened, timeout time.Duration) (*page.EventFileChooserOpened, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case event := <-events:
		if event == nil || event.BackendNodeID == 0 {
			return nil, false
		}
		return event, true
	case <-timer.C:
		return nil, false
	}
}

func waitForMetaMediaAttachment(ctx context.Context, mediaPaths []string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var confirmed bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(metaMediaAttachedScript(mediaPaths), &confirmed)); err == nil && confirmed {
			return true
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
			return false
		}
	}
	return false
}

func metaMediaControlScript() string {
	return `(() => {
  const labels = ['添加照片/视频', '添加照片或视频', '添加视频', 'add photo/video', 'add photos/videos', 'add photo or video', 'add video'];
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  document.querySelectorAll('[data-alpha-nexus-upload-target]').forEach(node => node.removeAttribute('data-alpha-nexus-upload-target'));
  window.__alphaNexusFileInputCount = document.querySelectorAll('input[type="file"]').length;
  const controls = Array.from(document.querySelectorAll('button, [role="button"], label'));
  const control = controls.find(node => {
    if (!visible(node) || node.disabled || node.getAttribute('aria-disabled') === 'true') return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || '', node.getAttribute('title') || ''].map(normalize);
    return values.some(value => labels.includes(value));
  });
  if (!control) return false;
  control.setAttribute('data-alpha-nexus-upload-control', 'true');
  return true;
})()`
}

func metaMarkMediaInputScript() string {
	return `(() => {
  const inputs = Array.from(document.querySelectorAll('input[type="file"]'));
  if (!inputs.length) return false;
  const before = Number(window.__alphaNexusFileInputCount || 0);
  const containers = Array.from(document.querySelectorAll('[role="dialog"], form')).filter(node => node.getClientRects().length > 0);
  const contextual = inputs.filter(input => containers.some(container => container.contains(input)));
  const candidate = inputs.length > before ? inputs[inputs.length - 1] : (contextual[contextual.length - 1] || (inputs.length === 1 ? inputs[0] : null));
  if (!candidate) return false;
  candidate.setAttribute('data-alpha-nexus-upload-target', 'true');
  return true;
})()`
}

func metaMediaInputConfirmedScript(selector string, expected int) string {
	rawSelector, _ := json.Marshal(selector)
	return fmt.Sprintf(`(() => {
  const selector = %s;
  const expected = %d;
  return Array.from(document.querySelectorAll(selector)).some(input => input.files && input.files.length >= expected);
})()`, rawSelector, expected)
}

func metaMediaAttachedScript(mediaPaths []string) string {
	names := make([]string, 0, len(mediaPaths))
	video := false
	for _, path := range mediaPaths {
		names = append(names, filepath.Base(path))
		switch strings.ToLower(filepath.Ext(path)) {
		case ".mp4", ".mov", ".m4v", ".webm":
			video = true
		}
	}
	rawNames, _ := json.Marshal(names)
	return fmt.Sprintf(`(() => {
	  const names = %s;
	  const video = %t;
	  const expected = names.length;
		  const labels = ['移除照片', '移除视频', '删除', 'remove photo', 'remove video', 'delete'];
		  const pendingLabels = ['上传中', '处理中', 'uploading', 'processing'];
		  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
		  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
%s
		  const root = resolveComposerScope();
		  if (!root) return false;
		  const controls = Array.from(root.querySelectorAll('button, [role="button"]')).filter(node => {
	    if (!visible(node)) return false;
	    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || '', node.getAttribute('title') || ''].map(normalize);
	    return values.some(value => labels.includes(value));
	  });
	  const previews = Array.from(root.querySelectorAll(
	    'img, video, [data-testid*="media_preview"], [data-testid*="preview"]'
	  )).filter(visible);
	  const pending = Array.from(root.querySelectorAll(
	    '[role="progressbar"], [aria-busy="true"], [data-testid*="progress"], [data-testid*="processing"]'
	  )).some(visible);
	  const leafTexts = new Set(Array.from(root.querySelectorAll('div, span'))
	    .filter(node => visible(node) && node.children.length === 0)
	    .map(node => normalize(node.textContent || ''))
	    .filter(Boolean));
	  if (pending || Array.from(leafTexts).some(text => pendingLabels.some(label => text.includes(label)))) return false;
	  if (controls.length < expected || previews.length < expected) return false;
	  if (!video) return true;
		  return controls.length >= expected &&
		    names.every(name => leafTexts.has(normalize(name))) &&
		    leafTexts.has('100%%');
		})()`, rawNames, video, metaComposerScopeResolverJS)
}

func tryAttachMetaCover(ctx context.Context, coverPath string) (bool, string) {
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(metaScrollComposerToEndScript(), nil),
		chromedp.Sleep(750*time.Millisecond),
	)
	if !waitForMetaBooleanScript(ctx, metaCoverTabScript(), 15*time.Second) {
		return false, "tab"
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-alpha-nexus-cover-tab="true"]`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil || !waitForMetaBooleanScript(ctx, metaCoverUploadControlScript(), 5*time.Second) {
		return false, "control"
	}
	if !setMetaChooserFiles(ctx, `[data-alpha-nexus-cover-upload="true"]`, []string{coverPath}) {
		return false, "chooser"
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var confirmed bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(metaCoverAttachedScript(), &confirmed)); err == nil && confirmed {
			return true, ""
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
			return false, "confirmation"
		}
	}
	return false, "confirmation"
}

func metaScrollComposerToEndScript() string {
	return `(() => {
  const nodes = Array.from(new Set([document.scrollingElement, ...document.querySelectorAll('div, main, section')])).filter(node => {
    if (!node || node.getClientRects().length === 0) return false;
    const rect = node.getBoundingClientRect();
    return rect.width >= 400 && node.scrollHeight > node.clientHeight + 100;
  });
  if (!nodes.length) return false;
  nodes.forEach(node => { node.scrollTop = node.scrollHeight; });
  return true;
})()`
}

func waitForMetaBooleanScript(ctx context.Context, script string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var confirmed bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &confirmed)); err == nil && confirmed {
			return true
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
			return false
		}
	}
	return false
}

func metaCoverTabScript() string {
	return `(() => {
  const labels = ['上传图片', 'upload image'];
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  document.querySelectorAll('[data-alpha-nexus-cover-tab]').forEach(node => node.removeAttribute('data-alpha-nexus-cover-tab'));
  const tab = Array.from(document.querySelectorAll('button, [role="button"]')).find(node => {
    if (!visible(node) || node.disabled || node.getAttribute('aria-disabled') === 'true') return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || ''].map(normalize);
    return values.some(value => labels.includes(value));
  });
  if (!tab) return false;
  tab.setAttribute('data-alpha-nexus-cover-tab', 'true');
  return true;
})()`
}

func metaCoverUploadControlScript() string {
	return `(() => {
  const labels = ['上传图片', 'upload image'];
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  document.querySelectorAll('[data-alpha-nexus-cover-upload]').forEach(node => node.removeAttribute('data-alpha-nexus-cover-upload'));
  const control = Array.from(document.querySelectorAll('a, [role="link"]')).find(node => {
    if (!visible(node) || node.getAttribute('aria-disabled') === 'true') return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || ''].map(normalize);
    return values.some(value => labels.includes(value));
  });
  if (!control) return false;
  control.setAttribute('data-alpha-nexus-cover-upload', 'true');
  return true;
})()`
}

func metaCoverAttachedScript() string {
	return `(() => {
  const labels = ['更换图片', 'replace image'];
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  return Array.from(document.querySelectorAll('a, button, [role="button"], [role="link"]')).some(node => {
    if (!visible(node)) return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || ''].map(normalize);
    return values.some(value => labels.includes(value));
  });
})()`
}

func metaDefaultReelCoverReadyScript() string {
	return `(() => {
  const headings = ['缩略图', 'thumbnail'];
  const nextLabels = ['下一页', 'next'];
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  const hasHeading = Array.from(document.querySelectorAll('h1, h2, h3, h4, [role="heading"]')).some(node =>
    visible(node) && headings.includes(normalize(node.innerText || node.textContent || ''))
  );
  if (!hasHeading) return false;
  return Array.from(document.querySelectorAll('button, [role="button"]')).some(node => {
    if (!visible(node) || node.disabled || node.getAttribute('aria-disabled') === 'true' || node.closest('[role="list"]')) return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || ''].map(normalize);
    return values.some(value => nextLabels.includes(value));
  });
})()`
}

func tryFillCaption(ctx context.Context, caption string) bool {
	if strings.TrimSpace(caption) == "" {
		return false
	}
	var editorFound bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(metaCaptionEditorScript(), &editorFound)); err != nil || !editorFound {
		return false
	}
	const selector = `[data-alpha-nexus-caption-editor="true"]`
	if err := chromedp.Run(ctx,
		chromedp.Focus(selector, chromedp.ByQuery),
		chromedp.KeyEvent("a", chromedp.KeyModifiers(input.ModifierMeta)),
		chromedp.KeyEvent(kb.Backspace),
		chromedp.SendKeys(selector, caption, chromedp.ByQuery),
		chromedp.Sleep(750*time.Millisecond),
	); err != nil {
		return false
	}
	var confirmed bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(metaCaptionMatchesScript(caption), &confirmed)); err != nil {
		return false
	}
	return confirmed
}

func metaCaptionEditorScript() string {
	return `(() => {
  if (!location.pathname.includes('composer')) return false;
  document.querySelectorAll('[data-alpha-nexus-caption-editor]').forEach(node => node.removeAttribute('data-alpha-nexus-caption-editor'));
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  const labels = ['在对话框中输入内容', '为帖子添加文字', 'write', 'post text', 'caption'];
  const candidates = Array.from(document.querySelectorAll('[contenteditable="true"]'));
  const editor = candidates.find(node => {
    if (!visible(node)) return false;
    const aria = String(node.getAttribute('aria-label') || '').toLowerCase();
    return node.getAttribute('role') === 'combobox' || labels.some(label => aria.includes(label));
  });
  if (!editor) return false;
  editor.setAttribute('data-alpha-nexus-caption-editor', 'true');
  return true;
})()`
}

func metaCaptionMatchesScript(caption string) string {
	rawCaption, _ := json.Marshal(caption)
	return fmt.Sprintf(`(() => {
  const expected = %s;
  const editor = document.querySelector('[data-alpha-nexus-caption-editor="true"]');
  if (!editor) return false;
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').replace(/\s+/g, ' ').trim();
  return normalize(editor.innerText || editor.textContent || '') === normalize(expected);
})()`, rawCaption)
}

func metaFinalControlLabels(contentType string) []string {
	labels := []string{"publish", "发布"}
	if isMetaVideoContent(contentType) {
		labels = []string{"share", "分享"}
	}
	return labels
}

func metaPublishReadyScript(contentType string) string {
	labels := metaFinalControlLabels(contentType)
	rawLabels, _ := json.Marshal(labels)
	return fmt.Sprintf(`(() => {
		  if (!location.pathname.includes('composer')) return false;
		  const exact = new Set(%s);
		  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
		  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
%s
		  const root = resolveComposerScope();
		  if (!root) return false;
		  const controls = Array.from(root.querySelectorAll('button, [role="button"]')).filter(button => {
		    if (!visible(button) || button.disabled || button.getAttribute('aria-disabled') === 'true' || button.closest('[role="list"]')) return false;
		    const values = [button.innerText || button.textContent || '', button.getAttribute('aria-label') || ''].map(normalize);
		    return values.some(value => exact.has(value));
		  });
		  return controls.length === 1;
		})()`, rawLabels, metaComposerScopeResolverJS)
}

func waitForMetaPublishReady(ctx context.Context, contentType string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var ready bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(metaPublishReadyScript(contentType), &ready)); err == nil && ready {
			return true
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
			return false
		}
	}
	return false
}

func isMetaVideoContent(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.Contains(contentType, "video") || strings.Contains(contentType, "reel")
}

func metaReelNextControlScript() string {
	return `(() => {
  const labels = ['下一页', 'next'];
  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
  document.querySelectorAll('[data-alpha-nexus-reel-next]').forEach(node => node.removeAttribute('data-alpha-nexus-reel-next'));
  const control = Array.from(document.querySelectorAll('button, [role="button"]')).find(node => {
    if (!visible(node) || node.disabled || node.getAttribute('aria-disabled') === 'true' || node.closest('[role="list"]')) return false;
    const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || ''].map(normalize);
    return values.some(value => labels.includes(value));
  });
  if (!control) return false;
  control.setAttribute('data-alpha-nexus-reel-next', 'true');
  return true;
})()`
}

func advanceMetaReelToFinalReview(ctx context.Context) bool {
	for range 2 {
		if waitForMetaPublishReady(ctx, "video", 500*time.Millisecond) {
			return true
		}
		var marked bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(metaReelNextControlScript(), &marked)); err != nil || !marked {
			return false
		}
		if err := chromedp.Run(ctx,
			chromedp.Click(`[data-alpha-nexus-reel-next="true"]`, chromedp.ByQuery),
			chromedp.Sleep(time.Second),
		); err != nil {
			return false
		}
	}
	return waitForMetaPublishReady(ctx, "video", 10*time.Second)
}

type metaSubmitState struct {
	State      string `json:"state"`
	TargetOnly bool   `json:"targetOnly"`
}

func metaSubmitScript(platform string, req platforms.PublishRequest) (string, bool) {
	targetName := strings.TrimSpace(req.AssetName)
	targetID := strings.TrimSpace(req.AssetExternalID)
	connectedName := strings.TrimSpace(req.ConnectedAssetName)
	connectedID := strings.TrimSpace(req.ConnectedAssetExternalID)
	pageID := targetID
	if platform == "instagram" {
		pageID = connectedID
	} else if platform != "facebook" {
		return "", false
	}
	if targetName == "" || connectedName == "" || strings.EqualFold(targetName, connectedName) ||
		!validMetaAssetID(targetID) || !validMetaAssetID(pageID) || !validMetaAssetID(connectedID) {
		return "", false
	}
	if platform == "instagram" && connectedID != pageID {
		return "", false
	}
	caption, _, captionErr := metaCaption(firstNonEmpty(req.Caption, req.Body, req.Title), req.TagsJSON)
	mediaPlan, mediaErr := buildMetaMediaPlan(req)
	if captionErr != nil || mediaErr != nil || strings.TrimSpace(caption) == "" ||
		(len(mediaPlan.UploadPaths) == 0 && mediaRequired(platform, req.ContentType)) {
		return "", false
	}
	rawTarget, _ := json.Marshal(targetName)
	rawConnected, _ := json.Marshal(connectedName)
	rawTargetID, _ := json.Marshal(targetID)
	rawPageID, _ := json.Marshal(pageID)
	rawConnectedID, _ := json.Marshal(connectedID)
	targetPlatform := platform
	connectedPlatform := "instagram"
	if platform == "instagram" {
		connectedPlatform = "facebook"
	}
	rawTargetPlatform, _ := json.Marshal(targetPlatform)
	rawConnectedPlatform, _ := json.Marshal(connectedPlatform)
	rawFinalLabels, _ := json.Marshal(metaFinalControlLabels(req.ContentType))
	surfaceGuard := metaSubmitSurfaceGuard(caption, mediaPlan, isMetaVideoContent(req.ContentType))
	complianceGuard, ok := metaSubmitComplianceGuard(platform)
	if !ok {
		return "", false
	}
	return fmt.Sprintf(`(() => {
		  const normalize = value => String(value || '').replace(/[\u200B-\u200D\uFEFF]/g, '').trim().toLowerCase().replace(/\s+/g, ' ');
		  const exactText = node => normalize(node && (node.innerText || node.textContent || ''));
		  const visible = node => !!node && node.getClientRects().length > 0 && node.getAttribute('aria-hidden') !== 'true';
%s
		  const identityFields = node => {
		    const names = node && node.getAttributeNames ? node.getAttributeNames() : [];
		    const identityNames = new Set([
		      'data-asset-id', 'data-page-id', 'data-instagram-id', 'data-account-id',
		      'asset_id', 'page_id', 'instagram_id', 'account_id'
		    ]);
		    return [
		      node && node.getAttribute && node.getAttribute('href'),
		      ...names.filter(name => identityNames.has(String(name).toLowerCase())).map(name => node.getAttribute(name))
		    ].filter(value => String(value || '').trim());
		  };
		  const matchesIdentity = (node, expectedID, expectedPlatform) => {
		    const numericTokens = identityFields(node).flatMap(value => String(value || '').split(/[^0-9]+/).filter(Boolean));
		    if (numericTokens.length > 0) return numericTokens.includes(expectedID);
		    const knownPlatforms = new Set(['facebook', 'instagram']);
		    const icons = Array.from(node.querySelectorAll('img[alt]'))
		      .filter(visible)
		      .map(image => normalize(image.getAttribute('alt')))
		      .filter(label => knownPlatforms.has(label));
		    return icons.length === 1 && icons[0] === expectedPlatform;
		  };
		  const target = normalize(%s);
		  const connected = normalize(%s);
		  const targetID = %s;
		  const pageID = %s;
		  const connectedID = %s;
		  const targetPlatform = %s;
		  const connectedPlatform = %s;
		  const result = (state, targetOnly = false) => ({ state, targetOnly, target: %s, targetID, pageID });
		  if (location.hostname !== 'business.facebook.com' || !location.pathname.startsWith('/latest/composer')) return result('wrong_surface');
		  let currentPageID = '';
		  try { currentPageID = new URL(location.href).searchParams.get('asset_id') || ''; } catch (_) {}
		  if (currentPageID !== pageID) return result('wrong_asset');

		  const root = resolveComposerScope();
		  if (!root) return result('composer_scope_unconfirmed');
		  const comboboxes = Array.from(root.querySelectorAll('[role="combobox"]')).filter(node =>
		    visible(node) &&
	    node.getAttribute('contenteditable') !== 'true' &&
	    !['INPUT', 'TEXTAREA'].includes(String(node.tagName || '').toUpperCase())
  );
  if (comboboxes.length !== 1) return result(comboboxes.length === 0 ? 'loading' : 'ambiguous_combobox');
  const combobox = comboboxes[0];
	  const listboxes = Array.from(document.querySelectorAll('[role="listbox"]')).filter(visible);
	  if (listboxes.length === 0) {
	    if (combobox.disabled || combobox.getAttribute('aria-disabled') === 'true') return result('combobox_disabled');
	    if (combobox.getAttribute('data-alpha-nexus-submit-target-opened') === 'true') return result('loading');
	    combobox.setAttribute('data-alpha-nexus-submit-target-opened', 'true');
	    try { combobox.click(); return result('opened'); } catch (_) { return result('open_failed'); }
	  }
  if (listboxes.length !== 1) return result('ambiguous_listbox');

		  const allOptions = Array.from(listboxes[0].querySelectorAll('[role="option"]')).filter(visible);
		  const preferenceLabels = new Set(['保存偏好设置', 'save preference', 'save preferences']);
		  const assetOptions = allOptions.filter(option => !preferenceLabels.has(exactText(option)));
		  const targetOptions = assetOptions.filter(option =>
		    exactText(option) === target && matchesIdentity(option, targetID, targetPlatform)
		  );
		  const connectedOptions = assetOptions.filter(option =>
		    exactText(option) === connected && matchesIdentity(option, connectedID, connectedPlatform)
		  );
		  if (targetOptions.length !== 1 || connectedOptions.length !== 1) return result('known_targets_missing');
		  const knownOptions = new Set([targetOptions[0], connectedOptions[0]]);
		  if (assetOptions.some(option => !knownOptions.has(option))) return result('unexpected_target_option');
		  if (assetOptions.some(option => !['true', 'false'].includes(option.getAttribute('aria-selected')))) return result('selection_unknown');
	  const selected = assetOptions.filter(option => option.getAttribute('aria-selected') === 'true');
	  if (selected.length !== 1 || selected[0] !== targetOptions[0] || exactText(combobox) !== target) return result('target_changed');

	  %s
	  %s
	  const exact = new Set(%s);
	  const finalControls = Array.from(root.querySelectorAll('button, [role="button"]')).filter(button => {
	    if (!visible(button) || button.disabled || button.getAttribute('aria-disabled') === 'true' || button.closest && button.closest('[role="list"]')) return false;
	    const values = [button.getAttribute('aria-label') || '', button.innerText || button.textContent || ''].map(normalize);
	    return values.some(value => exact.has(value));
		  });
		  if (finalControls.length !== 1) return result(finalControls.length === 0 ? 'final_control_missing' : 'ambiguous_final_control', true);
		  try { finalControls[0].click(); return result('clicked', true); } catch (_) { return result('click_failed', true); }
		})()`,
		metaComposerScopeResolverJS,
		rawTarget,
		rawConnected,
		rawTargetID,
		rawPageID,
		rawConnectedID,
		rawTargetPlatform,
		rawConnectedPlatform,
		rawTarget,
		surfaceGuard,
		complianceGuard,
		rawFinalLabels,
	), true
}

func metaSubmitSurfaceGuard(caption string, plan metaMediaPlan, video bool) string {
	rawCaption, _ := json.Marshal(caption)
	names := make([]string, 0, len(plan.UploadPaths))
	for _, mediaPath := range plan.UploadPaths {
		names = append(names, filepath.Base(mediaPath))
	}
	rawNames, _ := json.Marshal(names)
	return fmt.Sprintf(`const expectedCaption = normalize(%s);
	  const editors = Array.from(root.querySelectorAll('[data-alpha-nexus-caption-editor="true"]')).filter(visible);
	  if (editors.length !== 1 || normalize(editors[0].innerText || editors[0].textContent || '') !== expectedCaption) {
	    return result('composer_surface_unready', true);
	  }
	  const expectedMediaNames = %s;
	  const expectedMedia = expectedMediaNames.length;
	  if (expectedMedia > 0) {
	    const mediaLabels = ['移除照片', '移除视频', '删除', 'remove photo', 'remove video', 'delete'];
	    const mediaControls = Array.from(root.querySelectorAll('button, [role="button"]')).filter(node => {
	      if (!visible(node)) return false;
	      const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || '', node.getAttribute('title') || ''].map(normalize);
	      return values.some(value => mediaLabels.includes(value));
	    });
	    const previews = Array.from(root.querySelectorAll(
	      'img, video, [data-testid*="media_preview"], [data-testid*="preview"]'
	    )).filter(visible);
	    const pending = Array.from(root.querySelectorAll(
	      '[role="progressbar"], [aria-busy="true"], [data-testid*="progress"], [data-testid*="processing"]'
	    )).some(visible);
	    const leafTexts = new Set(Array.from(root.querySelectorAll('div, span'))
	      .filter(node => visible(node) && node.children.length === 0)
	      .map(node => normalize(node.textContent || '')).filter(Boolean));
	    if (pending || mediaControls.length < expectedMedia || previews.length < expectedMedia) {
	      return result('composer_surface_unready', true);
	    }
	    if (%t && (!expectedMediaNames.every(name => leafTexts.has(normalize(name))) || !leafTexts.has('100%%'))) {
	      return result('composer_surface_unready', true);
	    }
	  }
	  if (%t) {
	    const coverLabels = new Set(['更换图片', 'replace image']);
	    const coverReady = Array.from(root.querySelectorAll('a, button, [role="button"], [role="link"]')).some(node => {
	      if (!visible(node)) return false;
	      const values = [node.innerText || node.textContent || '', node.getAttribute('aria-label') || ''].map(normalize);
	      return values.some(value => coverLabels.has(value));
	    });
	    if (!coverReady) return result('composer_surface_unready', true);
	  }`, rawCaption, rawNames, video, plan.CoverPath != "")
}

func metaSubmitComplianceGuard(platform string) (string, bool) {
	switch platform {
	case "instagram":
		return `const complianceControls = Array.from(root.querySelectorAll(
	    '[role="switch"], [role="radio"], [role="checkbox"], input[type="radio"]'
	  )).filter(visible);
	  if (complianceControls.length !== 0) return result('compliance_changed', true);`, true
	case "facebook":
		return `const contextText = node => {
	    const candidates = [node, node && node.closest ? node.closest('label') : null, node && node.parentElement];
	    return normalize(Array.from(new Set(candidates.filter(Boolean))).map(candidate =>
	      candidate.innerText || candidate.textContent || candidate.getAttribute('aria-label') || candidate.getAttribute('title') || ''
	    ).join(' '));
	  };
	  const checked = node => {
	    const aria = node.getAttribute('aria-checked');
	    if (aria === 'true' || aria === 'false') return aria;
	    if (typeof node.checked === 'boolean') return node.checked ? 'true' : 'false';
	    return 'unknown';
	  };
	  const complianceControls = Array.from(root.querySelectorAll(
	    '[role="switch"], [role="radio"], [role="checkbox"], input[type="radio"]'
	  )).filter(visible);
	  const ad = complianceControls.filter(node => node.getAttribute('role') === 'switch' &&
	    (contextText(node).includes('创建为广告帖') || contextText(node).includes('create as ad post')));
	  const radios = complianceControls.filter(node => node.getAttribute('role') === 'radio' || String(node.tagName || '').toUpperCase() === 'INPUT');
	  const publicOptions = radios.filter(node => {
	    const text = contextText(node);
	    return (text.includes('公开') && text.includes('任何人都能看到')) ||
	      (text.includes('public') && (text.includes('anyone') || text.includes('everyone')));
	  });
	  const specificOptions = radios.filter(node => {
	    const text = contextText(node);
	    return text.includes('特定分享对象') || text.includes('specific audience');
	  });
	  if (ad.length !== 1 || publicOptions.length !== 1 || specificOptions.length !== 1 ||
	      checked(ad[0]) !== 'false' || checked(publicOptions[0]) !== 'true' || checked(specificOptions[0]) !== 'false') {
	    return result('compliance_changed', true);
	  }
	  const expectedCompliance = new Set([ad[0], publicOptions[0], specificOptions[0]]);
	  if (complianceControls.some(node => !expectedCompliance.has(node))) return result('compliance_changed', true);`, true
	default:
		return "", false
	}
}

func tryClickPublish(ctx context.Context, platform string, req platforms.PublishRequest) (clicked bool, clearSuccess bool, targetOnly bool) {
	// Snapshot transient evidence before clicking so an old toast cannot prove
	// that this publish attempt succeeded.
	before := publishEvidenceTexts(ctx)
	script, ok := metaSubmitScript(platform, req)
	if !ok {
		return false, false, false
	}
	var state metaSubmitState
	for attempt := 0; attempt < 8; attempt++ {
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &state)); err != nil {
			return false, false, false
		}
		if state.State == "clicked" {
			break
		}
		if state.State != "opened" && state.State != "loading" {
			return false, false, state.TargetOnly
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(250*time.Millisecond)); err != nil {
			return false, false, false
		}
	}
	if state.State != "clicked" {
		return false, false, state.TargetOnly
	}
	// Give Business Suite time to show toast / navigate.
	_ = chromedp.Run(ctx, chromedp.Sleep(3*time.Second))
	after := publishEvidenceTexts(ctx)
	// User rule: NEVER fake published without a new, explicit success signal.
	return true, hasNewPublishSuccessEvidence(before, after), true
}

const publishEvidenceTextJS = `(() => {
  const regions = Array.from(document.querySelectorAll('[role="status"], [role="alert"], [data-testid*="toast"], [aria-live="polite"], [aria-live="assertive"]'));
  return regions
    .map((el) => ((el.innerText || el.textContent || '') + '').replace(/\s+/g, ' ').trim())
    .filter((text) => text.length > 0);
})()`

func publishEvidenceTexts(ctx context.Context) []string {
	var texts []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(publishEvidenceTextJS, &texts)); err != nil {
		return nil
	}
	return texts
}

func hasNewPublishSuccessEvidence(before, after []string) bool {
	seen := make(map[string]struct{}, len(before))
	for _, text := range before {
		seen[normalizePublishEvidence(text)] = struct{}{}
	}
	for _, text := range after {
		normalized := normalizePublishEvidence(text)
		if normalized == "" {
			continue
		}
		if _, existed := seen[normalized]; existed {
			continue
		}
		if isPublishSuccessEvidence(normalized) {
			return true
		}
	}
	return false
}

func normalizePublishEvidence(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func isPublishSuccessEvidence(text string) bool {
	for _, phrase := range []string{
		"your post has been shared", "your reel has been shared",
		"published successfully", "post published", "reel published",
		"is now live", "shared to feed", "发布成功", "已发布", "已分享",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// CaptureScreenshot writes a PNG under the screenshots dir (best-effort).
// Exported for tests / callers that need an explicit capture helper name.
func CaptureScreenshot(ctx context.Context, tag string) string {
	return captureScreenshotBestEffort(ctx, tag)
}

func captureScreenshotBestEffort(ctx context.Context, tag string) string {
	dir, err := config.DefaultConfig().ScreenshotsDir()
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	name := fmt.Sprintf("%s_%d.png", sanitizeFileTag(tag), time.Now().UnixNano())
	path := filepath.Join(dir, name)
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil || len(buf) == 0 {
		return ""
	}
	// Screenshots may contain account names, post drafts, or platform notices.
	// Keep the evidence private to the local user, like diagnostic JSONL files.
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return ""
	}
	return path
}

func sanitizeFileTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "shot"
	}
	var b strings.Builder
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
