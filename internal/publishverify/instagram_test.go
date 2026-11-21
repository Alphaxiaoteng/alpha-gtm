//go:build legacy_chromium

package publishverify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

const (
	testTitle   = "αgamer｜阿尔法玩家，正式入驻"
	testCaption = "你好，这里是 αgamer｜阿尔法玩家。\n本条封面与动态视觉由 AI 生成。"
	testAsset   = "chenpaiteng"
	testAssetID = "17841416733781085"
	testPostID  = "DExample01"
	testMedia   = "2bb09bbfa4307ce3b6bf5d3e74800d0c43270b05a64347469cbb80014ab0f45e"
	testCover   = "1a1b84869bbcab7b0354b24dda5409c25b4ffe252bd46663926a381187cdce37"
)

type fakeBrowserStatus struct {
	status *browser.AccountStatus
	err    error
}

func (f fakeBrowserStatus) Status(int64) (*browser.AccountStatus, error) {
	return f.status, f.err
}

type sequenceObserver struct {
	snapshots []InstagramBackendSnapshot
	err       error
	calls     int
}

func (o *sequenceObserver) Observe(_ context.Context, _ string, _ InstagramExpectation) (InstagramBackendSnapshot, error) {
	o.calls++
	if o.err != nil {
		return InstagramBackendSnapshot{}, o.err
	}
	index := o.calls - 1
	if index >= len(o.snapshots) {
		index = len(o.snapshots) - 1
	}
	return o.snapshots[index], nil
}

type concurrentObserver struct {
	snapshot InstagramBackendSnapshot
	entered  chan struct{}
	release  chan struct{}
}

func (o concurrentObserver) Observe(ctx context.Context, _ string, _ InstagramExpectation) (InstagramBackendSnapshot, error) {
	select {
	case o.entered <- struct{}{}:
	case <-ctx.Done():
		return InstagramBackendSnapshot{}, ctx.Err()
	}
	select {
	case <-o.release:
		return o.snapshot, nil
	case <-ctx.Done():
		return InstagramBackendSnapshot{}, ctx.Err()
	}
}

func pngFixture() []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, 1, 1))
	canvas.Set(0, 0, color.RGBA{R: 42, G: 21, B: 84, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		panic(err)
	}
	return output.Bytes()
}

func publishedSnapshot(publishedAt time.Time) InstagramBackendSnapshot {
	return InstagramBackendSnapshot{
		ManagerURL:       "https://business.facebook.com/latest/content_calendar",
		TargetName:       testAsset,
		TargetExternalID: testAssetID,
		Items: []InstagramBackendItem{{
			Status:          "Published",
			Title:           testTitle,
			Caption:         testCaption,
			MediaSHA256:     testMedia,
			ThumbnailSHA256: testCover,
			PublishedAt:     publishedAt.UTC().Format(time.RFC3339),
			PostURL:         "https://www.instagram.com/p/" + testPostID + "/",
			ExternalPostID:  testPostID,
		}},
		ScreenshotPNG: pngFixture(),
	}
}

func TestParseInstagramBackendSnapshotFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "instagram_backend_published.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseInstagramBackendSnapshot(raw, pngFixture())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TargetName != testAsset || snapshot.TargetExternalID != testAssetID || len(snapshot.Items) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot.Items[0].Status != "Published" || snapshot.Items[0].ExternalPostID != testPostID {
		t.Fatalf("item=%+v", snapshot.Items[0])
	}
}

func TestMatchInstagramBackendRequiresTargetContentMediaTimeAndPublishedStatus(t *testing.T) {
	submitted := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	expected := InstagramExpectation{
		AccountID:        9,
		AssetID:          11,
		TargetName:       testAsset,
		TargetExternalID: testAssetID,
		Title:            testTitle,
		Caption:          testCaption,
		MediaSHA256:      []string{testMedia, testCover},
		SubmittedAt:      submitted,
		PublishedBefore:  submitted.Add(DefaultInstagramVerificationTimeout),
	}
	snapshot := publishedSnapshot(submitted.Add(time.Minute))
	item, ok := MatchInstagramBackend(expected, snapshot)
	if !ok || item.ExternalPostID != testPostID {
		t.Fatalf("matched=%v item=%+v", ok, item)
	}

	cases := map[string]func(*InstagramBackendSnapshot){
		"wrong target": func(value *InstagramBackendSnapshot) { value.TargetExternalID = "other" },
		"processing":   func(value *InstagramBackendSnapshot) { value.Items[0].Status = "Processing" },
		"old post": func(value *InstagramBackendSnapshot) {
			value.Items[0].PublishedAt = submitted.Add(-3 * time.Minute).Format(time.RFC3339)
		},
		"wrong content": func(value *InstagramBackendSnapshot) { value.Items[0].Caption = "unrelated post" },
		"wrong media": func(value *InstagramBackendSnapshot) {
			value.Items[0].MediaSHA256 = strings.Repeat("a", 64)
			value.Items[0].ThumbnailSHA256 = strings.Repeat("b", 64)
		},
		"wrong URL identity": func(value *InstagramBackendSnapshot) {
			value.Items[0].PostURL = "https://www.instagram.com/p/OtherPost/"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			copySnapshot := snapshot
			copySnapshot.Items = append([]InstagramBackendItem(nil), snapshot.Items...)
			mutate(&copySnapshot)
			if item, ok := MatchInstagramBackend(expected, copySnapshot); ok {
				t.Fatalf("unexpected match: %+v", item)
			}
		})
	}
}

func TestInstagramVerificationDefaultsAreTwentySecondsAndTenMinutes(t *testing.T) {
	if DefaultInstagramPollInterval != 20*time.Second {
		t.Fatalf("poll interval=%s", DefaultInstagramPollInterval)
	}
	if DefaultInstagramVerificationTimeout != 10*time.Minute {
		t.Fatalf("timeout=%s", DefaultInstagramVerificationTimeout)
	}
}

func TestInstagramVerifierPollsThenRecordsIndependentEvidence(t *testing.T) {
	database, job, submitted := setupInstagramVerificationJob(t)
	defer database.Close()

	processing := publishedSnapshot(submitted.Add(time.Minute))
	processing.Items[0].Status = "Processing"
	published := publishedSnapshot(submitted.Add(2 * time.Minute))
	observer := &sequenceObserver{snapshots: []InstagramBackendSnapshot{processing, published}}
	now := submitted
	verifier := &InstagramVerifier{
		DB: database,
		Browsers: fakeBrowserStatus{status: &browser.AccountStatus{
			AccountID: job.AccountID,
			Running:   true,
			CDPPort:   15123,
			CDPURL:    "http://127.0.0.1:15123",
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
	if result.State != VerificationPublished || result.Record == nil || result.Record.ExternalPostID != testPostID {
		t.Fatalf("result=%+v", result)
	}
	if observer.calls != 2 || result.Attempts != 2 {
		t.Fatalf("calls=%d attempts=%d", observer.calls, result.Attempts)
	}
	if !strings.HasPrefix(result.ScreenshotPath, filepath.Join(database.DataDir, "screenshots")+string(os.PathSeparator)) {
		t.Fatalf("screenshot path escaped private dir: %q", result.ScreenshotPath)
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

func TestInstagramVerifierConcurrentReverificationCreatesAtMostOneRecordAndPreservesSubmitMarker(t *testing.T) {
	database, job, submitted := setupInstagramVerificationJob(t)
	defer database.Close()
	before := job.SubmitAttemptedAt
	observer := concurrentObserver{
		snapshot: publishedSnapshot(submitted.Add(time.Minute)),
		entered:  make(chan struct{}, 2),
		release:  make(chan struct{}),
	}
	verifier := &InstagramVerifier{
		DB: database,
		Browsers: fakeBrowserStatus{status: &browser.AccountStatus{
			AccountID: job.AccountID,
			Running:   true,
			CDPPort:   15123,
			CDPURL:    "http://127.0.0.1:15123",
		}},
		Observer: observer,
		Now:      func() time.Time { return submitted },
	}
	type outcome struct {
		result *InstagramVerificationResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, err := verifier.Verify(context.Background(), job.ID)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	for range 2 {
		select {
		case <-observer.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent verifier did not reach observer")
		}
	}
	close(observer.release)
	for range 2 {
		value := <-outcomes
		if value.err != nil || value.result == nil || value.result.State != VerificationPublished {
			t.Fatalf("outcome=%+v", value)
		}
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 1 {
		t.Fatalf("records=%d err=%v", records, err)
	}
	after, err := database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before == "" || after.SubmitAttemptedAt != before {
		t.Fatalf("submit marker before=%q after=%q", before, after.SubmitAttemptedAt)
	}
}

func TestInstagramVerifierFailsClosedWhenDOMAdapterIsUnverified(t *testing.T) {
	database, job, submitted := setupInstagramVerificationJob(t)
	defer database.Close()
	verifier := &InstagramVerifier{
		DB: database,
		Browsers: fakeBrowserStatus{status: &browser.AccountStatus{
			AccountID: job.AccountID,
			Running:   true,
			CDPPort:   15123,
			CDPURL:    "http://127.0.0.1:15123",
		}},
		Now: func() time.Time { return submitted },
	}
	result, err := verifier.Verify(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != VerificationManualTakeover || result.ErrorCode != "INSTAGRAM_BACKEND_DOM_UNVERIFIED" {
		t.Fatalf("result=%+v", result)
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
}

func TestInstagramVerifierTimesOutWithoutPublishedMatch(t *testing.T) {
	database, job, submitted := setupInstagramVerificationJob(t)
	defer database.Close()
	processing := publishedSnapshot(submitted.Add(time.Minute))
	processing.Items[0].Status = "Processing"
	observer := &sequenceObserver{snapshots: []InstagramBackendSnapshot{processing}}
	now := submitted
	verifier := &InstagramVerifier{
		DB:           database,
		Browsers:     fakeBrowserStatus{status: &browser.AccountStatus{AccountID: job.AccountID, Running: true, CDPPort: 15123, CDPURL: "http://127.0.0.1:15123"}},
		Observer:     observer,
		PollInterval: 20 * time.Second,
		Timeout:      40 * time.Second,
		Now:          func() time.Time { return now },
		Sleep: func(_ context.Context, duration time.Duration) error {
			now = now.Add(duration)
			return nil
		},
	}
	result, err := verifier.Verify(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != VerificationPending || result.Attempts != 3 || observer.calls != 3 {
		t.Fatalf("result=%+v calls=%d", result, observer.calls)
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
}

func setupInstagramVerificationJob(t *testing.T) (*db.DB, *db.PublishJob, time.Time) {
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
	account, err := database.CreateAccount("instagram", "Meta login", "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := database.CreateAccountAsset(
		account.ID, "instagram", "instagram_account", testAsset, testAssetID, "", 0, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", testTitle, testCaption, testCaption, `["αgamer"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := publishplan.Snapshot{
		Version: publishplan.PublishPlanSnapshotVersion,
		Content: publishplan.ContentSnapshot{
			ID: content.ID, Type: content.Type, Title: testTitle, Caption: testCaption,
			Body: testCaption, TagsJSON: `["αgamer"]`,
		},
		MediaMode: "graph_text",
		Media: []publishplan.MediaSnapshot{
			{ID: 1, Kind: "image", SHA256: testMedia, Role: "content", Position: 0},
			{ID: 2, Kind: "image", SHA256: testCover, Role: "cover", Position: 1},
		},
		Targets: []publishplan.TargetSnapshot{{
			AccountID: account.ID, AccountPlatformCode: account.PlatformCode,
			AssetID: asset.ID, PlatformCode: "instagram",
			AssetType: asset.AssetType, AssetName: asset.Name, AssetExternalID: asset.ExternalID, AssetEnabled: asset.Enabled,
			Title: testTitle, Caption: testCaption, Body: testCaption,
			TagsJSON: `["αgamer"]`, SettingsJSON: `{"visibility":"public"}`,
		}},
		Concurrency: 1,
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
		VALUES (?, ?, ?, 'instagram', '{"visibility":"public"}', 'pending', ?, ?)`,
		planID, account.ID, asset.ID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := targetResult.LastInsertId()
	prepareJob, created, err := database.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash, 0, content.ID, account.ID, asset.ID,
		"instagram", "graph_text", fmt.Sprintf("instagram:prepare:%d", content.ID),
		"prepare", `{"visibility":"public"}`,
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
		"instagram", "graph_text", fmt.Sprintf("instagram:verify:%d", content.ID),
		"submit", `{"visibility":"public"}`,
	)
	if err != nil || !created {
		t.Fatalf("job=%+v created=%v err=%v", job, created, err)
	}
	if claimed, err := database.ClaimPublishJob(job.ID, "instagram-verify"); err != nil || !claimed {
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
	job, err = database.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	submittedAt, err := time.Parse(time.RFC3339Nano, job.SubmitAttemptedAt)
	if err != nil {
		t.Fatal(err)
	}
	return database, job, submittedAt.UTC()
}
