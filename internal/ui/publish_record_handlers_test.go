package ui

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/publishplan"
)

func createUIFrozenSubmitJob(t *testing.T, server *Server, suffix string) *db.PublishJob {
	return createUIFrozenSubmitJobForPlatform(t, server, suffix, "instagram")
}

func createUIFrozenSubmitJobForPlatform(t *testing.T, server *Server, suffix, platformCode string) *db.PublishJob {
	t.Helper()
	accountPlatformCode := platformCode
	if platformCode == "facebook" {
		accountPlatformCode = "meta"
	}
	account, err := server.db.CreateAccount(accountPlatformCode, "ui-frozen-"+suffix, "")
	if err != nil {
		t.Fatal(err)
	}
	assetType := "instagram_account"
	if platformCode == "xhs" {
		assetType = "xhs_account"
	} else if platformCode == "facebook" {
		assetType = "facebook_page"
	}
	asset, err := server.db.CreateAccountAsset(
		account.ID, platformCode, assetType, "creator-"+suffix, "17841416733781085", "", 0, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	content, err := server.db.CreateContent("graph_text", "job", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(server.db.DataDir, "ui-frozen-"+suffix+".png")
	canvas := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 160, A: 255})
		}
	}
	var mediaBytes bytes.Buffer
	if err := png.Encode(&mediaBytes, canvas); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, mediaBytes.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	mediaDigest := sha256.Sum256(mediaBytes.Bytes())
	publishAt := ""
	if platformCode == "facebook" {
		publishAt = "2030-08-15T11:48:00Z"
	}
	snapshot := publishplan.Snapshot{
		Version: publishplan.PublishPlanSnapshotVersion,
		Content: publishplan.ContentSnapshot{
			ID: content.ID, Type: content.Type, Title: content.Title,
			Caption: content.Caption, Body: content.Body, TagsJSON: content.TagsJSON,
		},
		MediaMode: "graph_text",
		Media: []publishplan.MediaSnapshot{{
			ID: 1, Kind: "image", FilePath: mediaPath, MIMEType: "image/png",
			SHA256: fmt.Sprintf("%x", mediaDigest[:]), Role: "content",
		}},
		Targets: []publishplan.TargetSnapshot{{
			AccountID: account.ID, AccountPlatformCode: account.PlatformCode,
			AssetID: asset.ID, PlatformCode: platformCode, AssetType: asset.AssetType,
			AssetName: asset.Name, AssetExternalID: asset.ExternalID, AssetEnabled: asset.Enabled,
			Title: content.Title, Caption: content.Caption, Body: content.Body,
			TagsJSON: content.TagsJSON, SettingsJSON: `{}`, PublishAt: publishAt,
		}},
		Concurrency: 1,
	}
	snapshotBytes, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotJSON := string(snapshotBytes)
	digest := sha256.Sum256(snapshotBytes)
	snapshotHash := fmt.Sprintf("%x", digest[:])
	now := time.Now().UTC().Format(time.RFC3339Nano)
	planResult, err := server.db.Exec(`INSERT INTO publish_plans
		(content_id, media_mode, snapshot_json, snapshot_hash, status, approved_action, concurrency, created_at, frozen_at, updated_at)
		VALUES (?, 'graph_text', ?, ?, 'approved', 'prepare', 1, ?, ?, ?)`,
		content.ID, snapshotJSON, snapshotHash, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	planID, _ := planResult.LastInsertId()
	targetResult, err := server.db.Exec(`INSERT INTO publish_plan_targets
		(plan_id, account_id, asset_id, platform_code, settings_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, '{}', 'pending', ?, ?)`,
		planID, account.ID, asset.ID, platformCode, now, now)
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := targetResult.LastInsertId()
	prepareJob, created, err := server.db.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash, 0, content.ID, account.ID, asset.ID,
		platformCode, content.Type, "ui-frozen:"+suffix+":prepare", "prepare", `{}`,
	)
	if err != nil || !created {
		t.Fatalf("prepare job=%+v created=%v err=%v", prepareJob, created, err)
	}
	if _, err := server.db.Exec(`UPDATE publish_jobs SET status='awaiting_review', current_step='ready_to_submit' WHERE id=?`, prepareJob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.Exec(`UPDATE publish_plan_targets SET status='awaiting_review' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.Exec(`UPDATE publish_plans SET status='approved', approved_action='submit' WHERE id=?`, planID); err != nil {
		t.Fatal(err)
	}
	job, created, err := server.db.CreatePublishPlanJobWithIntentResult(
		planID, targetID, snapshotHash, 0, content.ID, account.ID, asset.ID,
		platformCode, content.Type, "ui-frozen:"+suffix+":0001", "submit", `{}`,
	)
	if err != nil || !created {
		t.Fatalf("job=%+v created=%v err=%v", job, created, err)
	}
	return job
}

func createUIScheduledDouyinContentRecord(t *testing.T, server *Server, suffix string) *db.PublishContentRecord {
	return createUIScheduledContentRecord(t, server, suffix, "douyin")
}

func createUIScheduledContentRecord(t *testing.T, server *Server, suffix, platformCode string) *db.PublishContentRecord {
	t.Helper()
	job := createUIFrozenSubmitJobForPlatform(t, server, suffix, platformCode)
	if claimed, err := server.db.ClaimPublishJob(job.ID, "scheduled-delete-"+suffix); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := server.db.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	evidenceDir := filepath.Join(server.db.DataDir, "screenshots", "publish")
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join(evidenceDir, "scheduled-delete-"+suffix+".png")
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 20, G: 20, B: 20, A: 255})
	var evidence bytes.Buffer
	if err := png.Encode(&evidence, canvas); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, evidence.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	scheduledAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339)
	record, err := server.db.RecordScheduledPublishContent(job.ID, scheduledAt, evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.GetAccount(job.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateEgoSpaceForIdentity(
		account.IdentityID, 96, "alpha-nexus/"+platformCode+"-delete-"+suffix, platformCode+" 删除",
	); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestPublishRecordsAPIEncodesEmptyListAsArray(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/publish-records", nil))
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestManualConfirmationAPIListsReviewButCannotClaimPublished(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	manual := createUIJob(t, server, "manual-confirm-list")
	if err := server.db.UpdatePublishJob(manual.ID, "manual_takeover", "open_creator", "AWAITING_HUMAN", "handoff", 0); err != nil {
		t.Fatal(err)
	}
	review := createUIJob(t, server, "review-confirm-list")
	if err := server.db.UpdatePublishJob(review.ID, "awaiting_review", "review", "", "", 0); err != nil {
		t.Fatal(err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/publish-jobs/manual", nil)
	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, listRequest)
	var pending []map[string]json.RawMessage
	if listResponse.Code != http.StatusOK || json.NewDecoder(listResponse.Body).Decode(&pending) != nil || len(pending) != 2 {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	statuses := make(map[int64]string, len(pending))
	for _, item := range pending {
		var id int64
		var status string
		if err := json.Unmarshal(item["id"], &id); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(item["status"], &status); err != nil {
			t.Fatalf("manual confirmation item has no status: %+v", item)
		}
		statuses[id] = status
	}
	if statuses[manual.ID] != "manual_takeover" || statuses[review.ID] != "awaiting_review" {
		t.Fatalf("statuses=%+v", statuses)
	}

	confirmRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/publish-jobs/%d/confirm", review.ID), bytes.NewBufferString(`{"externalPostId":"review-1"}`))
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmResponse := httptest.NewRecorder()
	server.ServeHTTP(confirmResponse, confirmRequest)
	if confirmResponse.Code != http.StatusConflict {
		t.Fatalf("confirm status=%d body=%s", confirmResponse.Code, confirmResponse.Body.String())
	}

	repeatRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/publish-jobs/%d/confirm", review.ID), bytes.NewBufferString(`{}`))
	repeatResponse := httptest.NewRecorder()
	server.ServeHTTP(repeatResponse, repeatRequest)
	if repeatResponse.Code != http.StatusConflict {
		t.Fatalf("manual attestation replay status=%d body=%s", repeatResponse.Code, repeatResponse.Body.String())
	}
	var count int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, review.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("publish record count=%d err=%v", count, err)
	}
}

func TestCreatorBackendVerificationAPIRejectsCallerSuppliedEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	runner := &egoHandlerRunner{}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	job := createUIFrozenSubmitJob(t, server, "creator-backend-evidence")
	if claimed, err := server.db.ClaimPublishJob(job.ID, "creator-backend-evidence"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := server.db.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.FinalizePublishJob(job.ID, "awaiting_review", "creator_backend_verification", "PUBLISH_VERIFICATION_REQUIRED", "creator backend verification required", 0); err != nil {
		t.Fatal(err)
	}
	screenshotDir := filepath.Join(server.db.DataDir, "screenshots")
	if err := os.MkdirAll(screenshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	screenshotPath := filepath.Join(screenshotDir, "creator-published.png")
	if err := os.WriteFile(screenshotPath, []byte("creator backend evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"observedStatus": "Published",
		"postUrl":        "https://example.com/posts/1", "externalPostId": "post-1",
		"screenshotPath": screenshotPath,
	})
	verified := httptest.NewRecorder()
	server.ServeHTTP(verified, httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-jobs/%d/verify-published", job.ID), bytes.NewReader(body)))
	if verified.Code != http.StatusBadRequest {
		t.Fatalf("caller evidence status=%d body=%s", verified.Code, verified.Body.String())
	}
	var records int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}

	before, err := server.db.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	reverify := httptest.NewRecorder()
	server.ServeHTTP(reverify, httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-jobs/%d/verify-published", job.ID), nil))
	if reverify.Code != http.StatusAccepted ||
		!bytes.Contains(reverify.Body.Bytes(), []byte(`"status":"awaiting_review"`)) ||
		!bytes.Contains(reverify.Body.Bytes(), []byte(`"error_code":"EGO_PUBLISH_VERIFY_UNVERIFIED"`)) {
		t.Fatalf("reverify status=%d body=%s", reverify.Code, reverify.Body.String())
	}
	if len(runner.programs) != 0 {
		t.Fatalf("unverified creator verification reached EgoLite runtime: programs=%d", len(runner.programs))
	}
	after, err := server.db.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.SubmitAttemptedAt == "" || after.SubmitAttemptedAt != before.SubmitAttemptedAt {
		t.Fatalf("submit marker before=%q after=%q", before.SubmitAttemptedAt, after.SubmitAttemptedAt)
	}
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("reverify records=%d err=%v", records, err)
	}
}

func TestCreatorBackendVerificationAPILeavesDouyinManagerEvidenceUnverified(t *testing.T) {
	testCreatorBackendVerificationAPILeavesManagerEvidenceUnverified(t,
		"douyin", "https://creator.douyin.com/creator-micro/content/manage",
		"已发布", "https://www.douyin.com/video/123", "123",
	)
}

func TestCreatorBackendVerificationAPILeavesCSDNManagerEvidenceUnverified(t *testing.T) {
	testCreatorBackendVerificationAPILeavesManagerEvidenceUnverified(t,
		"csdn", "https://mp.csdn.net/mp_blog/manage/article",
		"已发布", "https://blog.csdn.net/weixin_49486457/article/details/123", "123",
	)
}

func TestCreatorBackendVerificationAPILeavesTikTokManagerEvidenceUnverified(t *testing.T) {
	testCreatorBackendVerificationAPILeavesManagerEvidenceUnverified(t,
		"tiktok", "https://www.tiktok.com/tiktokstudio/content",
		"Public", "https://www.tiktok.com/@alpha_gamer/video/1234567890123", "1234567890123",
	)
}

func TestCreatorBackendVerificationAPILeavesYouTubeManagerEvidenceUnverified(t *testing.T) {
	testCreatorBackendVerificationAPILeavesManagerEvidenceUnverified(t,
		"youtube", "https://studio.youtube.com/",
		"Published", "https://www.youtube.com/shorts/xYz_12-AB", "xYz_12-AB",
	)
}

func TestCreatorBackendVerificationAPILeavesFacebookManagerEvidenceUnverified(t *testing.T) {
	testCreatorBackendVerificationAPILeavesManagerEvidenceUnverified(t,
		"facebook", "https://business.facebook.com/latest/planner/?asset_id=17841416733781085",
		"Scheduled", "", "",
	)
}

func testCreatorBackendVerificationAPILeavesManagerEvidenceUnverified(t *testing.T, platformCode, managerURL, observedStatus, postURL, externalPostID string) {
	t.Helper()
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	publishInspect := map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true, "location": managerURL,
		"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
		"observedStatus": observedStatus, "postURL": postURL, "externalPostID": externalPostID,
		"screenshotBase64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}
	if platformCode == "facebook" {
		publishInspect["publishedState"] = false
		publishInspect["scheduledState"] = true
		publishInspect["effectiveScheduledAt"] = "2030-08-15T11:48:00Z"
	}
	runner := &egoHandlerRunner{
		boundSpace: map[string]any{
			"id": 14, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1", "ownership": "user",
		},
		publishInspect: publishInspect,
	}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	owner := platformCode + "-manager-evidence"
	job := createUIFrozenSubmitJobForPlatform(t, server, owner, platformCode)
	account, err := server.db.GetAccount(job.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateEgoSpaceForIdentity(
		account.IdentityID, 14, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1",
	); err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdateAccountLoginState(job.AccountID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if claimed, err := server.db.ClaimPublishJob(job.ID, owner); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := server.db.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.FinalizePublishJob(
		job.ID, "awaiting_review", "creator_backend_verification",
		"EGO_SUBMIT_RESULT_UNKNOWN", "creator backend verification required", 0,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.Exec(`UPDATE publish_plan_targets SET status='result_unknown',publish_job_id=? WHERE id=?`, job.ID, job.PlanTargetID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.Exec(`UPDATE publish_plans SET status='result_unknown' WHERE id=?`, job.PlanID); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, fmt.Sprintf("/api/publish-jobs/%d/verify-published", job.ID), nil,
	))
	if response.Code != http.StatusAccepted ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"status":"awaiting_review"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"EGO_PUBLISH_VERIFY_UNVERIFIED"`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishJob(job.ID)
	if err != nil {
		t.Fatalf("job=%+v err=%v", after, err)
	}
	if after.Status != "awaiting_review" || after.SubmitOutcome != "attempted" {
		t.Fatalf("job=%+v", after)
	}
	var publishRecords, contentRecords int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&publishRecords); err != nil || publishRecords != 0 {
		t.Fatalf("publish records=%d err=%v", publishRecords, err)
	}
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM publish_content_records WHERE job_id=?`, job.ID).Scan(&contentRecords); err != nil || contentRecords != 0 {
		t.Fatalf("content records=%d err=%v", contentRecords, err)
	}
	if len(runner.programs) != 0 {
		t.Fatalf("unverified manager verification reached EgoLite runtime: programs=%d", len(runner.programs))
	}
}

func TestCreatorBackendVerificationAPIDispatchesSubmittedXHSJobWithoutCallerEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	job := createUIFrozenSubmitJobForPlatform(t, server, "xhs-backend-dispatch", "xhs")
	if claimed, err := server.db.ClaimPublishJob(job.ID, "xhs-backend-dispatch"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := server.db.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.FinalizePublishJob(
		job.ID, "awaiting_review", "creator_backend_verification",
		"PUBLISH_VERIFICATION_REQUIRED", "creator backend verification required", 0,
	); err != nil {
		t.Fatal(err)
	}
	before, err := server.db.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	submittedAt, err := time.Parse(time.RFC3339Nano, before.SubmitAttemptedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "info", Step: "creator_backend_verification", Type: "job.awaiting_review",
		RawJSON: fmt.Sprintf(
			`{"submitAttempted":true,"submitClicked":true,"submitClickedAt":%q,"submitTargetId":"A4D362607FC76673DA07AE900173A0F3"}`,
			submittedAt.Add(time.Second).Format(time.RFC3339Nano),
		),
	}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, fmt.Sprintf("/api/publish-jobs/%d/verify-published", job.ID), nil,
	))
	if response.Code != http.StatusAccepted ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"status":"awaiting_review"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"EGO_PUBLISH_VERIFY_UNVERIFIED"`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SubmitAttemptedAt != before.SubmitAttemptedAt {
		t.Fatalf("submit marker before=%q after=%q", before.SubmitAttemptedAt, after.SubmitAttemptedAt)
	}
	var records int
	if err := server.db.QueryRow(
		`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID,
	).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
}

func TestCreatorBackendVerificationAPIAcceptsSubmittedUnsupportedJobAndFailsClosedWithoutObserver(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	job := createUIFrozenSubmitJobForPlatform(t, server, "wechat-mp-backend-dispatch", "wechat_mp")
	if claimed, err := server.db.ClaimPublishJob(job.ID, "wechat-mp-backend-dispatch"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := server.db.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.FinalizePublishJob(
		job.ID, "awaiting_review", "creator_backend_verification",
		"PUBLISH_VERIFICATION_REQUIRED", "creator backend verification required", 0,
	); err != nil {
		t.Fatal(err)
	}
	before, err := server.db.GetPublishJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	submittedAt, err := time.Parse(time.RFC3339Nano, before.SubmitAttemptedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "info", Step: "creator_backend_verification", Type: "job.awaiting_review",
		RawJSON: fmt.Sprintf(
			`{"submitAttempted":true,"submitClicked":true,"submitClickedAt":%q,"submitTargetId":"A4D362607FC76673DA07AE900173A0F3"}`,
			submittedAt.Add(time.Second).Format(time.RFC3339Nano),
		),
	}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, fmt.Sprintf("/api/publish-jobs/%d/verify-published", job.ID), nil,
	))
	if response.Code != http.StatusAccepted ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"status":"awaiting_review"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"EGO_PUBLISH_VERIFY_UNVERIFIED"`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var records int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
}

func TestPublishContentDeleteAPIMarksDeletedOnlyAfterReviewedDouyinRuntimeEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledDouyinContentRecord(t, server, "reviewed")
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "deleted" || after.DeletedAt == "" || after.DeleteEvidencePath == "" {
		t.Fatalf("record=%+v", after)
	}
	deletePrograms := 0
	for _, program := range runner.programs {
		if strings.Contains(program, "let deletionResult =") {
			deletePrograms++
			if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
				t.Fatalf("unsafe delete program: %s", program)
			}
		}
	}
	if deletePrograms != 1 {
		t.Fatalf("delete program count=%d", deletePrograms)
	}
}

func TestPublishContentDeleteAPIMarksDeletedOnlyAfterReviewedYouTubeRuntimeEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledContentRecord(t, server, "youtube-reviewed", "youtube")
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "deleted" || after.DeletedAt == "" || after.DeleteEvidencePath == "" {
		t.Fatalf("record=%+v", after)
	}
	job, err := server.db.GetPublishJob(record.JobID)
	if err != nil || job.Status != "success" || job.SubmitOutcome != "scheduled_verified" || job.CurrentStep != "creator_backend_scheduled_verified" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	program := strings.Join(runner.programs, "\n")
	for _, required := range []string{"https://studio.youtube.com/", "Delete forever", "Content deleted"} {
		if !strings.Contains(program, required) {
			t.Fatalf("YouTube deletion program missing %q: %s", required, program)
		}
	}
}

func TestPublishContentDeleteAPIMarksDeletedOnlyAfterReviewedTikTokRuntimeEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledContentRecord(t, server, "tiktok-reviewed", "tiktok")
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "deleted" || after.DeletedAt == "" || after.DeleteEvidencePath == "" {
		t.Fatalf("record=%+v", after)
	}
	program := strings.Join(runner.programs, "\n")
	for _, required := range []string{"https://www.tiktok.com/tiktokstudio/content", "Scheduled", "Delete", "删除"} {
		if !strings.Contains(program, required) {
			t.Fatalf("TikTok deletion program missing %q: %s", required, program)
		}
	}
}

func TestPublishContentDeleteAPIMarksDeletedOnlyAfterReviewedFacebookRuntimeEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledContentRecord(t, server, "facebook-reviewed", "facebook")
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"matchingPageID": true, "matchingPageName": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "deleted" || after.DeletedAt == "" || after.DeleteEvidencePath == "" {
		t.Fatalf("record=%+v", after)
	}
	program := strings.Join(runner.programs, "\n")
	for _, required := range []string{
		"https://business.facebook.com/latest/planner/?asset_id=17841416733781085",
		"17841416733781085", "creator-facebook-reviewed", "EGO_REMOTE_DELETE_RESULT_UNKNOWN",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Facebook deletion program missing %q: %s", required, program)
		}
	}
}

func TestPublishContentDeleteAPIMarksDeletedOnlyAfterReviewedWechatVideoRuntimeEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledContentRecord(t, server, "wechat-video-reviewed", "wechat_video")
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "deleted" || after.DeletedAt == "" || after.DeleteEvidencePath == "" {
		t.Fatalf("record=%+v", after)
	}
	job, err := server.db.GetPublishJob(record.JobID)
	if err != nil || job.Status != "success" || job.SubmitOutcome != "scheduled_verified" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestPublishContentDeleteAPIMarksDeletedOnlyAfterReviewedKuaishouPublishedEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledContentRecord(t, server, "kuaishou-reviewed", "kuaishou")
	postURL := "https://www.kuaishou.com/short-video/3xqxmjkthzpckus"
	if _, err := server.db.Exec(`UPDATE publish_content_records SET remote_state='published', post_url=?, external_post_id=? WHERE id=?`,
		postURL, "3xqxmjkthzpckus", record.ID); err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": false, "matchingScheduledMinute": false,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "deleted" || after.DeletedAt == "" || after.DeleteEvidencePath == "" {
		t.Fatalf("record=%+v", after)
	}
	program := strings.Join(runner.programs, "\n")
	for _, required := range []string{"cp.kuaishou.com/article/manage/video", postURL, "3xqxmjkthzpckus", "确认删除"} {
		if !strings.Contains(program, required) {
			t.Fatalf("Kuaishou deletion program missing %q: %s", required, program)
		}
	}
}

func TestPublishContentDeleteAPIMarksDeletedOnlyAfterReviewedKuaishouScheduledEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledContentRecord(t, server, "kuaishou-scheduled-reviewed", "kuaishou")
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "deleted" || after.DeletedAt == "" || after.DeleteEvidencePath == "" {
		t.Fatalf("record=%+v", after)
	}
	job, err := server.db.GetPublishJob(record.JobID)
	if err != nil || job.Status != "success" || job.SubmitOutcome != "scheduled_verified" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestPublishContentDeleteAPIFailsClosedWithoutDisappearanceEvidence(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledDouyinContentRecord(t, server, "unknown")
	runner := &egoHandlerRunner{publishDelete: map[string]any{
		"done": true, "deleted": false, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true, "errorCode": "EGO_REMOTE_DELETE_RESULT_UNKNOWN",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	))
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("EGO_REMOTE_DELETE_RESULT_UNKNOWN")) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "delete_failed" || after.DeletedAt != "" || after.DeleteEvidencePath != "" ||
		after.ErrorCode != "EGO_REMOTE_DELETE_RESULT_UNKNOWN" {
		t.Fatalf("record=%+v", after)
	}
}

func TestPublishContentDeleteAPIRejectsUnsupportedStateBeforeRemoteMutation(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	record := createUIScheduledDouyinContentRecord(t, server, "published")
	if _, err := server.db.Exec(`UPDATE publish_content_records SET remote_state='published' WHERE id=?`, record.ID); err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{publishDelete: map[string]any{"done": true, "deleted": true}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/publish-content-records/%d/delete", record.ID),
		bytes.NewBufferString(`{"confirmPermanentDelete":true}`),
	))
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("PUBLISH_CONTENT_DELETE_UNSUPPORTED")) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := server.db.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RemoteState != "published" || len(runner.programs) != 0 {
		t.Fatalf("record=%+v programs=%d", after, len(runner.programs))
	}
}
