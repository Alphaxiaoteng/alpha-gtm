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
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/publishplan"
	"github.com/chromedp/cdproto/target"
)

const (
	testXHSName       = "极客小腾"
	testXHSExternalID = "alpha-gamer"
	testXHSNoteID     = "66a4f891000000001f012345"
	testXHSTargetID   = "A4D362607FC76673DA07AE900173A0F3"
)

type xhsSequenceObserver struct {
	snapshots []XHSBackendSnapshot
	err       error
	calls     int
}

func (o *xhsSequenceObserver) Observe(_ context.Context, _ string, _ XHSExpectation) (XHSBackendSnapshot, error) {
	o.calls++
	if o.err != nil {
		return XHSBackendSnapshot{}, o.err
	}
	index := o.calls - 1
	if index >= len(o.snapshots) {
		index = len(o.snapshots) - 1
	}
	return o.snapshots[index], nil
}

func TestXHSVerificationDefaultsAreTwentySecondsAndTenMinutes(t *testing.T) {
	if DefaultXHSPollInterval != 20*time.Second {
		t.Fatalf("poll interval=%s", DefaultXHSPollInterval)
	}
	if DefaultXHSVerificationTimeout != 10*time.Minute {
		t.Fatalf("timeout=%s", DefaultXHSVerificationTimeout)
	}
}

func TestParseXHSDataImpressionRequiresOneStableNoteIDAndAcceptsDOMContentType(t *testing.T) {
	noteID, contentType, err := parseXHSDataImpression(
		`{"note_id":"66a4f891000000001f012345","note_type":"normal"}`,
	)
	if err != nil || noteID != testXHSNoteID || contentType != "graph_text" {
		t.Fatalf("id=%q type=%q err=%v", noteID, contentType, err)
	}
	noteID, contentType, err = parseXHSDataImpression(
		`{"track":{"noteId":"66a4f891000000001f012345","noteType":"video"}}`,
	)
	if err != nil || noteID != testXHSNoteID || contentType != "video" {
		t.Fatalf("id=%q type=%q err=%v", noteID, contentType, err)
	}
	noteID, contentType, err = parseXHSDataImpression(
		`{"noteTarget":{"type":"NoteTarget","value":{"noteId":"66a4f891000000001f012345"}}}`,
	)
	if err != nil || noteID != testXHSNoteID || contentType != "" {
		t.Fatalf("live id=%q type=%q err=%v", noteID, contentType, err)
	}
	for name, raw := range map[string]string{
		"missing id":       `{"note_type":"normal"}`,
		"generic id":       `{"id":"66a4f891000000001f012345","note_type":"normal"}`,
		"ambiguous id":     `{"note_id":"66a4f891000000001f012345","nested":{"noteId":"77a4f891000000001f012345"}}`,
		"unknown type":     `{"note_id":"66a4f891000000001f012345","note_type":"live"}`,
		"non-json payload": `not-json`,
	} {
		t.Run(name, func(t *testing.T) {
			if noteID, contentType, err := parseXHSDataImpression(raw); err == nil || noteID != "" || contentType != "" {
				t.Fatalf("id=%q type=%q err=%v", noteID, contentType, err)
			}
		})
	}
}

func TestSelectXHSCreatorTargetRequiresOneReviewedCreatorPage(t *testing.T) {
	selected, err := selectXHSCreatorTarget([]*target.Info{
		{TargetID: "public", Type: "page", URL: "https://www.xiaohongshu.com/explore/1"},
		{TargetID: "creator", Type: "page", URL: "https://creator.xiaohongshu.com/publish/publish"},
		{TargetID: "evil", Type: "page", URL: "https://creator.xiaohongshu.com.evil.example/new/note-manager"},
	})
	if err != nil || selected != "creator" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
	selected, err = selectXHSCreatorTarget([]*target.Info{
		{TargetID: "composer", Type: "page", URL: "https://creator.xiaohongshu.com/publish/publish"},
		{TargetID: "manager", Type: "page", URL: xhsNoteManagerURL},
	})
	if err != nil || selected != "manager" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
	if selected, err := selectXHSCreatorTarget([]*target.Info{
		{TargetID: "one", Type: "page", URL: xhsNoteManagerURL},
		{TargetID: "two", Type: "page", URL: xhsNoteManagerURL},
	}); !errors.Is(err, ErrXHSBackendAmbiguous) || selected != "" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
	selected, err = selectXHSCreatorTargetForJob([]*target.Info{
		{TargetID: "manager", Type: "page", URL: xhsNoteManagerURL},
		{TargetID: testXHSTargetID, Type: "page", URL: "https://creator.xiaohongshu.com/publish/publish?target=image"},
	}, testXHSTargetID)
	if err != nil || selected != testXHSTargetID {
		t.Fatalf("job-bound selected=%q err=%v", selected, err)
	}
}

func TestParseXHSPublishedTimeRequiresMinutePrecision(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, location)
	for raw, want := range map[string]string{
		"发布于 2026-07-26 11:58": "2026-07-26T03:58:00Z",
		"2026年07月26日 11:59":    "2026-07-26T03:59:00Z",
		"今天 11:57":             "2026-07-26T03:57:00Z",
	} {
		parsed, err := parseXHSPublishedTime(raw, location, now)
		if err != nil || parsed.UTC().Format(time.RFC3339) != want {
			t.Fatalf("raw=%q parsed=%s err=%v", raw, parsed, err)
		}
	}
	for _, raw := range []string{"2026-07-26", "3分钟前", "刚刚", ""} {
		if parsed, err := parseXHSPublishedTime(raw, location, now); err == nil || !parsed.IsZero() {
			t.Fatalf("raw=%q parsed=%s err=%v", raw, parsed, err)
		}
	}
}

func TestXHSObserverRequiresReviewedHomeIdentityBeforePublishedCards(t *testing.T) {
	if !validXHSCreatorHomeURL(xhsCreatorHomeURL) ||
		validXHSCreatorHomeURL("https://creator.xiaohongshu.com/new/home?from=guess") ||
		validXHSCreatorHomeURL("https://creator.xiaohongshu.com.evil.example/new/home") {
		t.Fatal("creator home URL validation is not exact")
	}
	for _, required := range []string{
		".base",
		".account-name, .name-box",
		".others.description-text > div:first-child",
		"小红书账号",
	} {
		if !strings.Contains(xhsCollectIdentityJS, required) {
			t.Fatalf("identity collector is missing reviewed selector %q", required)
		}
	}
	if strings.Contains(xhsCollectIdentityJS, "document.body.innerText") {
		t.Fatal("identity collector must pair name and identifier inside one reviewed profile card")
	}
}

func TestXHSObserverScriptsRequireReviewedPublishedTabAndDataImpression(t *testing.T) {
	for name, script := range map[string]string{
		"mount":   xhsVisiblePublishedTabCountJS,
		"tab":     xhsEnsurePublishedTabJS,
		"confirm": xhsPublishedTabActiveJS,
		"collect": xhsCollectBackendJS,
	} {
		if !strings.Contains(script, "'已发布'") {
			t.Fatalf("%s script does not bind the Published tab", name)
		}
	}
	for _, required := range []string{
		".tab-item",
		"tab-item--active",
		".note-card[data-impression]",
		".note-card__title",
		".note-card__time",
		"img.content",
		".play_time",
		"data-alpha-nexus-xhs-verify",
	} {
		if !strings.Contains(xhsCollectBackendJS, required) {
			t.Fatalf("collector is missing reviewed selector %q", required)
		}
	}
	if strings.Contains(xhsCollectBackendJS, "document.body.innerText") {
		t.Fatal("collector must not infer identity or Published status from broad body text")
	}
}

func TestWaitForXHSPublishedTabMountHandlesDelayedUniqueTabAndAmbiguity(t *testing.T) {
	counts := []int{0, 0, 1}
	probes := 0
	sleeps := 0
	count, err := waitForXHSPublishedTabMount(
		context.Background(),
		time.Second,
		func(context.Context) (int, error) {
			count := counts[probes]
			probes++
			return count, nil
		},
		func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	)
	if err != nil || count != 1 || probes != 3 || sleeps != 2 {
		t.Fatalf("count=%d probes=%d sleeps=%d err=%v", count, probes, sleeps, err)
	}

	sleeps = 0
	count, err = waitForXHSPublishedTabMount(
		context.Background(),
		time.Second,
		func(context.Context) (int, error) { return 2, nil },
		func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	)
	if err != nil || count != 2 || sleeps != 0 {
		t.Fatalf("ambiguous count=%d sleeps=%d err=%v", count, sleeps, err)
	}
}

func TestSelectXHSBackendCardUsesFrozenTitleAndContentType(t *testing.T) {
	published := `{"index":{"type":"Index","value":{"channelTabName":"published"}},"noteTarget":{"type":"NoteTarget","value":{"noteId":"66a4f891000000001f012345"}}}`
	items := []xhsDOMItem{
		{Title: testTitle, ContentType: "graph_text", Marker: "graph-card", DataImpression: published},
		{Title: testTitle, ContentType: "video", Marker: "video-card", DataImpression: published},
	}
	graph, err := selectXHSBackendCard(items, testTitle, "graph_text")
	if err != nil || graph.Marker != "graph-card" {
		t.Fatalf("graph=%+v err=%v", graph, err)
	}
	video, err := selectXHSBackendCard(items, testTitle, "video")
	if err != nil || video.Marker != "video-card" {
		t.Fatalf("video=%+v err=%v", video, err)
	}
	if _, err := selectXHSBackendCard(append(items, items[0]), testTitle, "graph_text"); !errors.Is(err, ErrXHSBackendAmbiguous) {
		t.Fatalf("duplicate same-form title error=%v", err)
	}
	if _, err := selectXHSBackendCard(items, "missing", "video"); err != nil {
		t.Fatalf("absence must remain pollable, err=%v", err)
	}
	staleAllTab := items[0]
	staleAllTab.DataImpression = strings.Replace(published, `"published"`, `"all"`, 1)
	if card, err := selectXHSBackendCard([]xhsDOMItem{staleAllTab}, testTitle, "graph_text"); err != nil || card.Marker != "" {
		t.Fatalf("stale all-tab card must remain pollable, card=%+v err=%v", card, err)
	}
	if channel, err := parseXHSDataImpressionChannel(published); err != nil || channel != "published" {
		t.Fatalf("channel=%q err=%v", channel, err)
	}
}

func TestMatchXHSThumbnailRequiresOnePerceptualFrozenImageMatch(t *testing.T) {
	directory := t.TempDir()
	coverPath := filepath.Join(directory, "cover.png")
	writePatternPNG(t, coverPath, false, 108, 144)
	coverBytes, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(coverBytes)
	media := []CreatorBackendMedia{{
		Kind: "image", Role: "cover", FilePath: coverPath, SHA256: hex.EncodeToString(digest[:]),
	}}

	resized := pngPattern(t, false, 54, 72)
	matched, err := matchXHSThumbnail(resized, media)
	if err != nil || matched != media[0].SHA256 {
		t.Fatalf("matched=%q err=%v", matched, err)
	}
	if matched, err := matchXHSThumbnail(pngPattern(t, true, 54, 72), media); err == nil || matched != "" {
		t.Fatalf("unrelated matched=%q err=%v", matched, err)
	}
	portraitPath := filepath.Join(directory, "portrait.png")
	writePatternPNG(t, portraitPath, false, 90, 160)
	portraitBytes, err := os.ReadFile(portraitPath)
	if err != nil {
		t.Fatal(err)
	}
	portraitDigest := sha256.Sum256(portraitBytes)
	portraitMedia := []CreatorBackendMedia{{
		Kind: "image", Role: "cover", FilePath: portraitPath,
		SHA256: hex.EncodeToString(portraitDigest[:]),
	}}
	portraitImage, _, err := image.Decode(bytes.NewReader(portraitBytes))
	if err != nil {
		t.Fatal(err)
	}
	crop := centeredAspectCrop(portraitImage, image.Rect(0, 0, 60, 60))
	var croppedPNG bytes.Buffer
	if err := png.Encode(&croppedPNG, crop); err != nil {
		t.Fatal(err)
	}
	if matched, err := matchXHSThumbnail(croppedPNG.Bytes(), portraitMedia); err != nil ||
		matched != portraitMedia[0].SHA256 {
		t.Fatalf("center-cropped matched=%q err=%v", matched, err)
	}

	secondPath := filepath.Join(directory, "duplicate.png")
	if err := os.WriteFile(secondPath, coverBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	secondDigest := sha256.Sum256(coverBytes)
	ambiguous := append(media, CreatorBackendMedia{
		Kind: "image", Role: "content", FilePath: secondPath, SHA256: hex.EncodeToString(secondDigest[:]),
	})
	if matched, err := matchXHSThumbnail(resized, ambiguous); err == nil || matched != "" {
		t.Fatalf("ambiguous matched=%q err=%v", matched, err)
	}
}

func TestXHSVerifierPollsThenRecordsIndependentEvidence(t *testing.T) {
	database, job, submitted, coverHash := setupXHSVerificationJob(t, 1)
	defer database.Close()

	processing := xhsPublishedSnapshot(submitted.Add(time.Minute), coverHash)
	processing.Items[0].Status = "审核中"
	published := xhsPublishedSnapshot(submitted.Add(2*time.Minute), coverHash)
	observer := &xhsSequenceObserver{snapshots: []XHSBackendSnapshot{processing, published}}
	now := submitted
	verifier := &XHSVerifier{
		DB: database,
		Browsers: fakeBrowserStatus{status: &browser.AccountStatus{
			AccountID: job.AccountID, Running: true, CDPPort: 15124,
			CDPURL: "http://127.0.0.1:15124",
		}},
		Observer: observer,
		Now:      func() time.Time { return now },
		Sleep: func(_ context.Context, duration time.Duration) error {
			now = now.Add(duration)
			return nil
		},
	}
	result, err := verifier.Verify(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != VerificationPublished || result.Record == nil ||
		result.Record.ExternalPostID != testXHSNoteID || result.Attempts != 2 {
		t.Fatalf("result=%+v", result)
	}
	if observer.calls != 2 {
		t.Fatalf("observer calls=%d", observer.calls)
	}
	info, err := os.Stat(result.ScreenshotPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("screenshot info=%+v err=%v", info, err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil || updated.Status != "published" || updated.SubmitOutcome != "verified_published" {
		t.Fatalf("job=%+v err=%v", updated, err)
	}
}

func TestXHSVerifierRejectsNonSingleTargetFrozenPlanBeforeBrowserObservation(t *testing.T) {
	database, job, _, _ := setupXHSVerificationJob(t, 2)
	defer database.Close()
	observer := &xhsSequenceObserver{}
	verifier := &XHSVerifier{
		DB:       database,
		Browsers: fakeBrowserStatus{},
		Observer: observer,
	}
	if result, err := verifier.Verify(context.Background(), job.ID); err == nil || result != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if observer.calls != 0 {
		t.Fatalf("observer calls=%d", observer.calls)
	}
}

func TestXHSVerifierRequiresDurableActualClickEvidence(t *testing.T) {
	database, job, _, _ := setupXHSVerificationJob(t, 1)
	defer database.Close()
	if _, err := database.Exec(`UPDATE job_events SET raw_json='{}' WHERE job_id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if expectation, err := (&XHSVerifier{DB: database}).expectation(job.ID); err == nil || expectation.SubmitTargetID != "" {
		t.Fatalf("expectation=%+v err=%v", expectation, err)
	}
}

func TestMatchXHSBackendRejectsTitleOnlyDuplicateAndWrongMediaOrType(t *testing.T) {
	submitted := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	coverHash := strings.Repeat("a", 64)
	expected := XHSExpectation{
		PlatformCode: "xhs", ContentType: "graph_text",
		TargetName: testXHSName, TargetExternalID: testXHSExternalID,
		Title: testTitle, Caption: testCaption, Body: testCaption,
		MediaSHA256: []string{coverHash}, SubmittedAt: submitted,
		PublishedBefore: submitted.Add(DefaultXHSVerificationTimeout),
	}
	snapshot := xhsPublishedSnapshot(submitted.Add(time.Minute), coverHash)
	if item, ok := MatchXHSBackend(expected, snapshot); !ok || item.ExternalPostID != testXHSNoteID {
		t.Fatalf("matched=%v item=%+v", ok, item)
	}
	snapshot.Items = append(snapshot.Items, snapshot.Items[0])
	if item, ok := MatchXHSBackend(expected, snapshot); ok {
		t.Fatalf("duplicate title unexpectedly matched: %+v", item)
	}
	snapshot = xhsPublishedSnapshot(submitted.Add(time.Minute), coverHash)
	snapshot.Items[0].ThumbnailSHA256 = strings.Repeat("b", 64)
	if item, ok := MatchXHSBackend(expected, snapshot); ok {
		t.Fatalf("wrong media unexpectedly matched: %+v", item)
	}
	snapshot = xhsPublishedSnapshot(submitted.Add(time.Minute), coverHash)
	snapshot.Items[0].ContentType = "video"
	if item, ok := MatchXHSBackend(expected, snapshot); ok {
		t.Fatalf("wrong type unexpectedly matched: %+v", item)
	}
	snapshot = xhsPublishedSnapshot(submitted.Add(time.Minute), coverHash)
	snapshot.PlatformCode = "instagram"
	if item, ok := MatchXHSBackend(expected, snapshot); ok {
		t.Fatalf("cross-platform snapshot unexpectedly matched: %+v", item)
	}
}

func xhsPublishedSnapshot(publishedAt time.Time, coverHash string) XHSBackendSnapshot {
	return XHSBackendSnapshot{
		PlatformCode: "xhs",
		ManagerURL:   "https://creator.xiaohongshu.com/new/note-manager",
		TargetName:   testXHSName, TargetExternalID: testXHSExternalID,
		Items: []XHSBackendItem{{
			ContentType: "graph_text", Status: "已发布",
			Title: testTitle, Caption: testCaption,
			ThumbnailSHA256: coverHash,
			PublishedAt:     publishedAt.UTC().Format(time.RFC3339),
			PostURL:         "https://www.xiaohongshu.com/explore/" + testXHSNoteID,
			ExternalPostID:  testXHSNoteID,
		}},
		ScreenshotPNG: pngFixture(),
	}
}

func setupXHSVerificationJob(t *testing.T, targetCount int) (*db.DB, *db.PublishJob, time.Time, string) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "XHS login", "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := database.CreateAccountAsset(
		account.ID, "xhs", "xhs_account", testXHSName, testXHSExternalID, "", 0, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", testTitle, testCaption, testCaption, `["αgamer"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(dataDir, "cover.png")
	writePatternPNG(t, coverPath, false, 108, 144)
	coverBytes, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatal(err)
	}
	coverDigest := sha256.Sum256(coverBytes)
	coverHash := hex.EncodeToString(coverDigest[:])
	targets := []publishplan.TargetSnapshot{{
		AccountID: account.ID, AccountPlatformCode: account.PlatformCode,
		AssetID: asset.ID, PlatformCode: "xhs", AssetType: asset.AssetType,
		AssetName: asset.Name, AssetExternalID: asset.ExternalID, AssetEnabled: asset.Enabled,
		Title: testTitle, Caption: testCaption, Body: testCaption,
		TagsJSON: `["αgamer"]`, SettingsJSON: `{"visibility":"public","content_source":"ai"}`,
	}}
	if targetCount > 1 {
		targets = append(targets, targets[0])
	}
	snapshot := publishplan.Snapshot{
		Version: publishplan.PublishPlanSnapshotVersion,
		Content: publishplan.ContentSnapshot{
			ID: content.ID, Type: content.Type, Title: testTitle, Caption: testCaption,
			Body: testCaption, TagsJSON: `["αgamer"]`,
		},
		MediaMode: "graph_text",
		Media: []publishplan.MediaSnapshot{{
			ID: 1, Kind: "image", FilePath: coverPath, MIMEType: "image/png",
			SHA256: coverHash, Role: "cover", Position: 0,
		}},
		Targets: targets, Concurrency: 1,
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(snapshotJSON)
	snapshotHash := hex.EncodeToString(digest[:])
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	planResult, err := database.Exec(`INSERT INTO publish_plans
		(content_id, media_mode, snapshot_json, snapshot_hash, status, approved_action, concurrency, created_at, frozen_at, approved_at, updated_at)
		VALUES (?, 'graph_text', ?, ?, 'approved', 'prepare', 1, ?, ?, ?, ?)`,
		content.ID, string(snapshotJSON), snapshotHash, now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	planID, _ := planResult.LastInsertId()
	targetResult, err := database.Exec(`INSERT INTO publish_plan_targets
		(plan_id, account_id, asset_id, platform_code, settings_json, status, created_at, updated_at)
		VALUES (?, ?, ?, 'xhs', '{"visibility":"public","content_source":"ai"}', 'awaiting_review', ?, ?)`,
		planID, account.ID, asset.ID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := targetResult.LastInsertId()
	prepareJob, created, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash, 0, content.ID, account.ID, asset.ID,
		"xhs", "graph_text", fmt.Sprintf("xhs:prepare:%d", content.ID),
		"prepare", `{"visibility":"public","content_source":"ai"}`,
	)
	if err != nil || !created {
		t.Fatalf("prepare job=%+v created=%v err=%v", prepareJob, created, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET status='awaiting_review', current_step='ready_to_submit' WHERE id=?`, prepareJob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET status='awaiting_review' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='approved', approved_action='submit' WHERE id=?`, planID); err != nil {
		t.Fatal(err)
	}
	job, created, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash, 0, content.ID, account.ID, asset.ID,
		"xhs", "graph_text", fmt.Sprintf("xhs:verify:%d", content.ID),
		"submit", `{"visibility":"public","content_source":"ai"}`,
	)
	if err != nil || !created {
		t.Fatalf("job=%+v created=%v err=%v", job, created, err)
	}
	if claimed, err := database.ClaimPublishJob(job.ID, "xhs-verify"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.FinalizePublishJob(
		job.ID, "awaiting_review", "creator_backend_verification",
		"PUBLISH_VERIFICATION_REQUIRED", "creator backend verification required", 0,
	); err != nil {
		t.Fatal(err)
	}
	refreshed, err := database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	submittedAt, err := time.Parse(time.RFC3339Nano, refreshed.SubmitAttemptedAt)
	if err != nil {
		t.Fatal(err)
	}
	clickedAt := submittedAt.UTC().Add(30 * time.Second)
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "info", Step: "creator_backend_verification", Type: "job.awaiting_review",
		RawJSON: fmt.Sprintf(`{"submitAttempted":true,"submitClicked":true,"submitClickedAt":%q,"submitTargetId":%q}`,
			clickedAt.Format(time.RFC3339Nano), testXHSTargetID),
	}); err != nil {
		t.Fatal(err)
	}
	return database, refreshed, clickedAt, coverHash
}

func writePatternPNG(t *testing.T, path string, alternate bool, width, height int) {
	t.Helper()
	if err := os.WriteFile(path, pngPattern(t, alternate, width, height), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pngPattern(t *testing.T, alternate bool, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var pixel color.RGBA
			if alternate {
				pixel = color.RGBA{R: uint8(220 - x*100/width), G: uint8(180 - y*80/height), B: 40, A: 255}
			} else {
				pixel = color.RGBA{R: uint8(10 + x*80/width), G: uint8(20 + y*40/height), B: uint8(120 + x*100/width), A: 255}
			}
			canvas.Set(x, y, pixel)
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
