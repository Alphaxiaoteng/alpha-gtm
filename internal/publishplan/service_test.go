package publishplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/platforms"
)

func TestLegacySnapshotRequiresRefreezeBeforeApproval(t *testing.T) {
	database, plan, _ := newSubmitPlanFixture(t)
	defer database.Close()
	legacy := plan.Snapshot
	legacy.Version = PublishPlanSnapshotVersion - 1
	snapshotJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(snapshotJSON)
	snapshotHash := hex.EncodeToString(digest[:])
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := database.Exec(`INSERT INTO publish_plans
		(content_id, media_mode, snapshot_json, snapshot_hash, status, concurrency, created_at, frozen_at, updated_at)
		VALUES (?, ?, ?, ?, 'frozen', 1, ?, ?, ?)`,
		plan.ContentID, plan.MediaMode, string(snapshotJSON), snapshotHash, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	legacyPlanID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	target := legacy.Targets[0]
	if _, err := database.Exec(`INSERT INTO publish_plan_targets
		(plan_id, account_id, asset_id, platform_code, settings_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`,
		legacyPlanID, target.AccountID, target.AssetID, target.PlatformCode, target.SettingsJSON, now, now); err != nil {
		t.Fatal(err)
	}

	grant, err := (&Service{DB: database}).ApproveForAction(
		legacyPlanID, snapshotHash, "desktop", PublishPlanActionPrepare, 5*time.Minute,
	)
	if err == nil || !strings.Contains(err.Error(), "PUBLISH_PLAN_REFREEZE_REQUIRED") || grant != nil {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
}

func TestNormalizedSettingsForMediaModeOmitsOtherContentTypeFields(t *testing.T) {
	got, err := platforms.NormalizeSettingsForContentType(
		"douyin",
		"video",
		`{"visibility":"public","music":"","comment_enabled":true,"self_declaration":"none"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]interface{}
	if err := json.Unmarshal([]byte(got), &values); err != nil {
		t.Fatal(err)
	}
	if _, ok := values["music"]; ok {
		t.Fatalf("video settings retained graph-text-only music: %s", got)
	}
	if values["visibility"] != "public" || values["comment_enabled"] != true || values["self_declaration"] != "none" {
		t.Fatalf("shared settings changed: %s", got)
	}
}

func TestFreezeAutoGeneratesVideoCoverWhenContentHasNoImage(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("video", "video title", "", "video body", `["video"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(t.TempDir(), "source.mp4")
	const videoPayload = "video-fixture"
	if err := os.WriteFile(videoPath, []byte(videoPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	videoDigest := sha256.Sum256([]byte(videoPayload))
	video, err := database.CreateMediaAsset(content.ID, "video", videoPath, videoPath, "video/mp4", int64(len(videoPayload)), hex.EncodeToString(videoDigest[:]), 1080, 1920, 1000)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	extractorCalls := 0
	service := &Service{
		DB: database,
		CoverExtractor: func(source, output string) (*media.FileMeta, error) {
			extractorCalls++
			if source != videoPath {
				t.Fatalf("source=%s", source)
			}
			const coverPayload = "jpeg-cover"
			if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(output, []byte(coverPayload), 0o600); err != nil {
				return nil, err
			}
			coverDigest := sha256.Sum256([]byte(coverPayload))
			return &media.FileMeta{
				Path: output, SHA256: hex.EncodeToString(coverDigest[:]), SizeBytes: int64(len(coverPayload)),
				MimeGuess: "image/jpeg", Width: 1080, Height: 1920,
			}, nil
		},
	}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "video", MediaIDs: []int64{video.ID},
		Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if extractorCalls != 1 || len(plan.Snapshot.Media) != 2 || plan.Snapshot.Media[0].Role != "video" || plan.Snapshot.Media[1].Role != "cover" || plan.Snapshot.Media[1].Kind != "image" {
		t.Fatalf("calls=%d media=%+v", extractorCalls, plan.Snapshot.Media)
	}
	nativePlan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "video", MediaIDs: []int64{video.ID},
		NativeVideoCover: true,
		Targets:          []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if extractorCalls != 1 || len(nativePlan.Snapshot.Media) != 1 || nativePlan.Snapshot.Media[0].Role != "video" {
		t.Fatalf("native cover calls=%d media=%+v", extractorCalls, nativePlan.Snapshot.Media)
	}
	if _, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "video", MediaIDs: []int64{video.ID},
		CoverMediaID: plan.Snapshot.Media[1].ID, NativeVideoCover: true,
		Targets: []TargetInput{{AccountID: account.ID}},
	}); err == nil {
		t.Fatal("explicit and native video covers must be mutually exclusive")
	}
}

func TestFreezePreservesExplicitArticleCoverRole(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "article cover", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	content, err := database.CreateContent("article", "article title", "", "article body", `["article-tag"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(t.TempDir(), "article-cover.png")
	const coverPayload = "explicit-article-cover"
	if err := os.WriteFile(coverPath, []byte(coverPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(coverPayload))
	library, err := database.UpsertMediaLibrary(
		coverPath, hex.EncodeToString(digest[:]), int64(len(coverPayload)),
		"image/png", 1280, 720, 0, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	cover, err := database.CreateMediaAssetFromLibraryWithRole(content.ID, library.ID, "cover", 0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "article", MediaIDs: []int64{cover.ID},
		Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Snapshot.Media) != 1 || plan.Snapshot.Media[0].Role != "cover" {
		t.Fatalf("article media=%+v", plan.Snapshot.Media)
	}
}

func TestFreezePublishAtOverridesEditorialPlannedAtWithoutMutatingContent(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("wechat_video", "scheduled video", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	content, err := database.CreateContent("video", "title", "", "body", `["scheduled"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("video"))
	video, err := database.CreateMediaAsset(
		content.ID, "video", mediaPath, mediaPath, "video/mp4", 5,
		hex.EncodeToString(digest[:]), 1080, 1920, 10_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(coverPath, []byte("cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	coverDigest := sha256.Sum256([]byte("cover"))
	cover, err := database.CreateMediaAsset(
		content.ID, "image", coverPath, coverPath, "image/png", 5,
		hex.EncodeToString(coverDigest[:]), 1080, 1440, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	publishAt := now.Add(24 * time.Hour).Format(time.RFC3339)
	plan, err := (&Service{DB: database, Now: func() time.Time { return now }}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "video", MediaIDs: []int64{video.ID}, CoverMediaID: cover.ID,
		PublishAt: publishAt, Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Snapshot.Content.PlannedAt != publishAt || plan.Snapshot.Targets[0].PublishAt != publishAt {
		t.Fatalf("content=%+v target=%+v", plan.Snapshot.Content, plan.Snapshot.Targets[0])
	}
	reloaded, err := database.GetContent(content.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.PlannedAt != "" {
		t.Fatalf("editorial planned_at mutated: %q", reloaded.PlannedAt)
	}
}

func TestFreezeMakesArticleImmediateAndRejectsUnsupportedOrRushedScheduling(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "immediate article", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	service := &Service{DB: database, Now: func() time.Time { return now }}

	content, err := database.CreateContent("article", "immediate title", "", "immediate body", `["immediate"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Snapshot.Content.PlannedAt != "" || plan.Snapshot.Targets[0].PublishAt != "" {
		t.Fatalf("planned snapshot=%+v target=%+v", plan.Snapshot.Content, plan.Snapshot.Targets[0])
	}
	reloaded, err := service.Get(plan.ID)
	if err != nil || len(reloaded.TargetStates) != 1 || reloaded.TargetStates[0].Status != "pending" || reloaded.TargetStates[0].SubmitAttempted {
		t.Fatalf("target states=%+v err=%v", reloaded.TargetStates, err)
	}

	for name, plannedAt := range map[string]string{
		"past":     now.Add(-time.Minute).Format(time.RFC3339),
		"too soon": now.Add(14*time.Minute + 59*time.Second).Format(time.RFC3339),
	} {
		t.Run(name, func(t *testing.T) {
			candidate, createErr := database.CreateContentWithPlannedAt(
				"article", name, "", "body", `["scheduled"]`, "draft", plannedAt,
			)
			if createErr != nil {
				t.Fatal(createErr)
			}
			_, freezeErr := service.Freeze(FreezeInput{
				ContentID: candidate.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}},
			})
			if freezeErr == nil || !strings.Contains(freezeErr.Error(), "15 minutes") {
				t.Fatalf("freeze err=%v", freezeErr)
			}
		})
	}

	future, err := database.CreateContentWithPlannedAt(
		"article", "future", "", "body", `["scheduled"]`, "draft", now.Add(2*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Freeze(FreezeInput{ContentID: future.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}}); err == nil || !strings.Contains(err.Error(), "EGO_PUBLISH_SCHEDULE_UNVERIFIED") {
		t.Fatalf("future CSDN scheduling err=%v", err)
	}
}

func TestFreezePreservesOrderedMediaVariantsAndSingleUseApproval(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "base title", "base caption", "base body", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaIDs := make([]int64, 0, 2)
	for index, body := range []string{"first-image", "second-image"} {
		path := filepath.Join(t.TempDir(), body+".png")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(body))
		asset, err := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", int64(len(body)), hex.EncodeToString(digest[:]), 100+index, 100+index, 0)
		if err != nil {
			t.Fatal(err)
		}
		mediaIDs = append(mediaIDs, asset.ID)
	}
	xhs, _ := database.CreateAccount("xhs", "xhs", "")
	secondXHS, _ := database.CreateAccount("xhs", "xhs-second", "")
	markPlanAccountLoggedIn(t, database, xhs.ID)
	markPlanAccountLoggedIn(t, database, secondXHS.ID)
	variant, err := database.UpsertContentVariant(db.ContentVariantInput{
		ContentID: content.ID, PlatformCode: "xhs", Title: "xhs title", Caption: "xhs caption", Body: "xhs body", TagsJSON: `["xhs"]`, Source: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database, Now: func() time.Time { return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC) }}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{mediaIDs[1], mediaIDs[0]},
		Targets: []TargetInput{
			{AccountID: secondXHS.ID, SettingsJSON: `{}`},
			{AccountID: xhs.ID, VariantID: variant.ID, SettingsJSON: `{"visibility":"public","comment_enabled":true,"declare_original":false}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusFrozen || plan.SnapshotHash == "" || plan.Snapshot.Concurrency != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET snapshot_json='{}' WHERE id=?`, plan.ID); err == nil {
		t.Fatal("frozen publish plan snapshot was mutable")
	}
	if got := []int64{plan.Snapshot.Media[0].ID, plan.Snapshot.Media[1].ID}; got[0] != mediaIDs[1] || got[1] != mediaIDs[0] {
		t.Fatalf("media order=%v", got)
	}
	if plan.Snapshot.Targets[1].Title != "xhs title" || plan.Snapshot.Targets[1].VariantID != variant.ID {
		t.Fatalf("variant snapshot=%+v", plan.Snapshot.Targets[1])
	}
	if err := database.ReplaceContent(content.ID, "graph_text", "changed later", "changed", "changed", `[]`, "draft"); err != nil {
		t.Fatal(err)
	}
	persisted, err := service.Get(plan.ID)
	if err != nil || persisted.Snapshot.Content.Title != "base title" {
		t.Fatalf("frozen source changed: plan=%+v err=%v", persisted, err)
	}
	if _, err := service.Approve(plan.ID, "wrong-hash", "desktop", 5*time.Minute); err == nil {
		t.Fatal("approval accepted a different snapshot hash")
	}
	grant, err := service.Approve(plan.ID, plan.SnapshotHash, "desktop", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var approvalArgsHash string
	if err := database.QueryRow(`SELECT args_hash FROM approvals WHERE id=?`, grant.ID).Scan(&approvalArgsHash); err != nil {
		t.Fatal(err)
	}
	if len(approvalArgsHash) != 64 || approvalArgsHash == plan.SnapshotHash {
		t.Fatalf("approval args hash does not independently bind account/media/settings: %q", approvalArgsHash)
	}
	consumed, err := service.ConsumeApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err != nil || consumed.Status != StatusApproved {
		t.Fatalf("consume plan=%+v err=%v", consumed, err)
	}
	if _, err := service.ConsumeApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce); err == nil {
		t.Fatal("approval replay was accepted")
	}
}

func TestPublishPlanApprovalIsBoundToPrepareOrSubmitAction(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("graph_text", "title", "caption", "body", `[]`, "draft")
	account, _ := database.CreateAccount("xhs", "xhs", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("image"))
	media, _ := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", 5, hex.EncodeToString(digest[:]), 100, 100, 0)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID},
		Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionPrepare); err == nil {
		t.Fatal("submit approval was accepted for prepare")
	}
	consumed, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit)
	if err != nil || consumed.Status != StatusApproved {
		t.Fatalf("submit consume plan=%+v err=%v", consumed, err)
	}
	if _, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", "unknown", 5*time.Minute); err == nil {
		t.Fatal("unknown approval action was accepted")
	}
}

func TestPublishPlanAcceptanceApprovalIsSingleTargetAndSeparatelyBound(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("graph_text", "title", "caption", "body", `[]`, "draft")
	account, _ := database.CreateAccount("douyin", "douyin", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("image"))
	media, _ := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", 5, hex.EncodeToString(digest[:]), 100, 100, 0)
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID}, Concurrency: 1,
		Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "desktop", PublishPlanActionAcceptanceSubmit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionSubmit); err == nil {
		t.Fatal("acceptance approval was accepted for normal submit")
	}
	consumed, err := service.ConsumeApprovalForAction(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce, PublishPlanActionAcceptanceSubmit)
	if err != nil || consumed.Status != StatusApproved {
		t.Fatalf("acceptance consume plan=%+v err=%v", consumed, err)
	}
}

func TestFreezeFailsClosedForInvalidMediaAndDuplicateTarget(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("graph_text", "title", "caption", "body", `[]`, "draft")
	account, _ := database.CreateAccount("xhs", "xhs", "")
	markPlanAccountLoggedIn(t, database, account.ID)
	service := &Service{DB: database}
	if _, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{9999}, Targets: []TargetInput{{AccountID: account.ID}}}); err == nil {
		t.Fatal("unknown media was accepted")
	}
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("video"))
	video, _ := database.CreateMediaAsset(content.ID, "video", path, path, "video/mp4", 5, hex.EncodeToString(digest[:]), 0, 0, 1000)
	if _, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{video.ID}, Targets: []TargetInput{{AccountID: account.ID}}}); err == nil {
		t.Fatal("video was accepted for graph_text")
	}
	if _, err := service.Freeze(FreezeInput{ContentID: content.ID, MediaMode: "video", MediaIDs: []int64{video.ID}, Targets: []TargetInput{{AccountID: account.ID}, {AccountID: account.ID}}}); err == nil {
		t.Fatal("duplicate account target was accepted")
	}
	article, _ := database.CreateContent("article", "title", "", "body", `[]`, "draft")
	if _, err := service.Freeze(FreezeInput{ContentID: article.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}}); err == nil {
		t.Fatal("article plan accepted a video-platform target")
	}
	csdn, _ := database.CreateAccount("csdn", "csdn", "")
	markPlanAccountLoggedIn(t, database, csdn.ID)
	if _, err := service.Freeze(FreezeInput{ContentID: article.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: csdn.ID}}}); err == nil {
		t.Fatal("CSDN article plan accepted an empty required tag list")
	}
	imagePath := filepath.Join(t.TempDir(), "article-image.png")
	const imagePayload = "article-image"
	if err := os.WriteFile(imagePath, []byte(imagePayload), 0o600); err != nil {
		t.Fatal(err)
	}
	imageDigest := sha256.Sum256([]byte(imagePayload))
	image, _ := database.CreateMediaAsset(article.ID, "image", imagePath, imagePath, "image/png", int64(len(imagePayload)), hex.EncodeToString(imageDigest[:]), 100, 100, 0)
	if _, err := service.Freeze(FreezeInput{ContentID: article.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID}, Targets: []TargetInput{{AccountID: account.ID}}}); err == nil {
		t.Fatal("article content accepted a different media mode")
	}
}

func TestFreezeRequiresLoggedInAccount(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Service{DB: database}).Freeze(FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}}}); err == nil || err.Error() != "account must be logged in before preparing a publish plan" {
		t.Fatalf("err=%v", err)
	}
}

func TestFreezeRejectsConcurrencyAboveActiveSpaceLimit(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "title", "", "body", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "article", Concurrency: 4,
		Targets: []TargetInput{{AccountID: 1}},
	})
	if err == nil || err.Error() != "publish plan concurrency must be between 1 and 3" {
		t.Fatalf("err=%v", err)
	}
}

func TestFreezePinsControlledTargetToBrowserAccountAsset(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 || assets[0].AssetType != "browser_account" {
		t.Fatalf("default assets=%+v err=%v", assets, err)
	}
	browserAssetID := assets[0].ID
	if _, err := database.CreateAccountAsset(account.ID, "csdn", "csdn_column", "extra generic target", "", "", 0, ""); err != nil {
		t.Fatal(err)
	}

	plan, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Snapshot.Targets) != 1 || plan.Snapshot.Targets[0].AssetID != browserAssetID {
		t.Fatalf("controlled target=%+v, want browser asset %d", plan.Snapshot.Targets, browserAssetID)
	}
}

func TestFreezePinsMetaTargetToExplicitConfirmedAsset(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "Meta title", "Meta caption", "Meta body", `["meta"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "meta.png")
	const payload = "meta-image"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	image, err := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("meta", "Meta operator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	target, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "AlphaIntelligence", "ig-public-1", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID},
		Targets: []TargetInput{{AccountID: account.ID, AssetID: target.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Snapshot.Targets) != 1 || plan.Snapshot.Targets[0].AssetID != target.ID || plan.Snapshot.Targets[0].PlatformCode != "instagram" {
		t.Fatalf("targets=%+v", plan.Snapshot.Targets)
	}

	unconfirmed, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Unconfirmed", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID},
		Targets: []TargetInput{{AccountID: account.ID, AssetID: unconfirmed.ID}},
	}); err == nil {
		t.Fatal("unconfirmed Meta target was accepted")
	}
}

func TestFreezePinsFacebookAndInstagramTargetsOwnedByOneMetaAccount(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "Meta title", "", "Meta body", `["meta"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "meta-pair.png")
	const payload = "meta-pair-image"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	image, err := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("meta", "Meta operator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	facebook, err := database.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "page-public-1", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	instagram, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Alpha IG", "ig-public-1", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID},
		Targets: []TargetInput{
			{AccountID: account.ID, AssetID: facebook.ID},
			{AccountID: account.ID, AssetID: instagram.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Snapshot.Targets) != 2 {
		t.Fatalf("targets=%+v", plan.Snapshot.Targets)
	}
	if plan.Snapshot.Targets[0].AssetID != facebook.ID || plan.Snapshot.Targets[0].PlatformCode != "facebook" {
		t.Fatalf("facebook target=%+v", plan.Snapshot.Targets[0])
	}
	if plan.Snapshot.Targets[1].AssetID != instagram.ID || plan.Snapshot.Targets[1].PlatformCode != "instagram" {
		t.Fatalf("instagram target=%+v", plan.Snapshot.Targets[1])
	}
	var persisted int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != 2 {
		t.Fatalf("persisted targets=%d", persisted)
	}
}

func TestFreezeCapturesCompleteTargetIdentity(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "Meta identity", "", "Meta body", `["meta"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "meta-identity.png")
	const payload = "meta-identity-image"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	image, err := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("meta", "Meta operator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	instagram, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Alpha IG", "ig-public-1", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	facebook, err := database.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "page-public-1", "", instagram.ID, instagram.Name)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := (&Service{DB: database}).Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{image.ID},
		Targets: []TargetInput{{AccountID: account.ID, AssetID: facebook.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	target := plan.Snapshot.Targets[0]
	if target.AccountPlatformCode != account.PlatformCode ||
		target.AssetType != facebook.AssetType || target.AssetName != facebook.Name ||
		target.AssetExternalID != facebook.ExternalID ||
		target.AssetEnabled != facebook.Enabled ||
		target.ConnectedAssetID != instagram.ID ||
		target.ConnectedAssetPlatformCode != instagram.PlatformCode ||
		target.ConnectedAssetType != instagram.AssetType ||
		target.ConnectedAssetName != instagram.Name ||
		target.ConnectedAssetExternalID != instagram.ExternalID ||
		target.ConnectedAssetEnabled != instagram.Enabled {
		t.Fatalf("target identity=%+v", target)
	}
}

func TestFreezeReusesAnUnchangedFrozenSnapshot(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("article", "title", "", "body", `["article-tag"]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	service := &Service{DB: database}
	input := FreezeInput{ContentID: content.ID, MediaMode: "article", Targets: []TargetInput{{AccountID: account.ID, SettingsJSON: `{}`}}}
	first, err := service.Freeze(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Freeze(input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.SnapshotHash != first.SnapshotHash || second.Status != StatusFrozen {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_plans WHERE snapshot_hash=? AND status=?`, first.SnapshotHash, StatusFrozen).Scan(&count); err != nil || count != 1 {
		t.Fatalf("frozen plan count=%d err=%v", count, err)
	}
}

func TestListSummariesReturnsSafeNewestFirstPlanMetadata(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent(
		"article",
		"αgamer｜阿尔法玩家，正式入驻",
		"",
		"article body that must not be listed",
		`["alpha-gamer"]`,
		"draft",
	)
	if err != nil {
		t.Fatal(err)
	}
	wechat, _ := database.CreateAccount("wechat_mp", "wechat", "")
	csdn, _ := database.CreateAccount("csdn", "csdn", "")
	markPlanAccountLoggedIn(t, database, wechat.ID)
	markPlanAccountLoggedIn(t, database, csdn.ID)
	service := &Service{DB: database}
	first, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "article",
		Targets: []TargetInput{{AccountID: wechat.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "article",
		Targets: []TargetInput{{AccountID: csdn.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	summaries, err := service.ListSummaries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].ID != second.ID || summaries[1].ID != first.ID {
		t.Fatalf("summaries=%+v", summaries)
	}
	if summaries[0].ContentID != content.ID ||
		summaries[0].ContentTitle != "αgamer｜阿尔法玩家，正式入驻" ||
		summaries[0].MediaMode != "article" ||
		summaries[0].TargetCount != 1 ||
		len(summaries[0].PlatformCodes) != 1 ||
		summaries[0].PlatformCodes[0] != "csdn" {
		t.Fatalf("newest summary=%+v", summaries[0])
	}
}

func TestSupportsNativeScheduleKeepsArticlePlatformsImmediateUntilVerified(t *testing.T) {
	if SupportsNativeSchedule("wechat_mp", "article") {
		t.Fatal("WeChat MP article scheduling must remain disabled until runtime and manager verification are reviewed")
	}
	if SupportsNativeSchedule("csdn", "article") {
		t.Fatal("CSDN article must remain immediate")
	}
}

func TestFreezeSupportsReviewedArticleAndVideoPlatformGroups(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database}

	article, _ := database.CreateContent("article", "article title", "source caption", "article body", `["article-tag"]`, "draft")
	wechatMP, _ := database.CreateAccount("wechat_mp", "wechat article", "")
	csdn, _ := database.CreateAccount("csdn", "csdn article", "")
	markPlanAccountLoggedIn(t, database, wechatMP.ID)
	markPlanAccountLoggedIn(t, database, csdn.ID)
	wechatVariant, _ := database.UpsertContentVariant(db.ContentVariantInput{ContentID: article.ID, PlatformCode: "wechat_mp", Title: "wechat title", Body: "wechat body", TagsJSON: `["wechat-tag"]`})
	csdnVariant, _ := database.UpsertContentVariant(db.ContentVariantInput{ContentID: article.ID, PlatformCode: "csdn", Title: "csdn title", Body: "csdn body", TagsJSON: `["csdn-tag"]`})
	articlePlan, err := service.Freeze(FreezeInput{
		ContentID: article.ID,
		MediaMode: "article",
		Targets:   []TargetInput{{AccountID: wechatMP.ID, VariantID: wechatVariant.ID, SettingsJSON: `{}`}, {AccountID: csdn.ID, VariantID: csdnVariant.ID, SettingsJSON: `{}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{articlePlan.Snapshot.Targets[0].PlatformCode, articlePlan.Snapshot.Targets[1].PlatformCode}; got[0] != "wechat_mp" || got[1] != "csdn" {
		t.Fatalf("article targets=%v", got)
	}
	if first, second := articlePlan.Snapshot.Targets[0], articlePlan.Snapshot.Targets[1]; first.Title != "wechat title" || first.Body != "wechat body" || first.Caption != "" || first.TagsJSON != `["wechat-tag"]` || second.Title != "csdn title" || second.Body != "csdn body" || second.Caption != "" || second.TagsJSON != `["csdn-tag"]` {
		t.Fatalf("article variants not independent: first=%+v second=%+v", first, second)
	}

	video, _ := database.CreateContent("video", "video title", "source caption", "video body", `["video-tag"]`, "draft")
	path := filepath.Join(t.TempDir(), "reviewed-video.mp4")
	const payload = "reviewed-video"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	media, _ := database.CreateMediaAsset(video.ID, "video", path, path, "video/mp4", int64(len(payload)), hex.EncodeToString(digest[:]), 1080, 1920, 1000)
	coverPath := filepath.Join(t.TempDir(), "reviewed-video-cover.jpg")
	const coverPayload = "reviewed-video-cover"
	if err := os.WriteFile(coverPath, []byte(coverPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	coverDigest := sha256.Sum256([]byte(coverPayload))
	cover, _ := database.CreateMediaAsset(video.ID, "image", coverPath, coverPath, "image/jpeg", int64(len(coverPayload)), hex.EncodeToString(coverDigest[:]), 1080, 1920, 0)
	targets := make([]TargetInput, 0, 4)
	var douyinID int64
	for _, code := range []string{"xhs", "douyin", "kuaishou", "wechat_video"} {
		account, createErr := database.CreateAccount(code, code+" video", "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		markPlanAccountLoggedIn(t, database, account.ID)
		targets = append(targets, TargetInput{AccountID: account.ID, SettingsJSON: `{}`})
		if code == "douyin" {
			douyinID = account.ID
		}
	}
	videoPlan, err := service.Freeze(FreezeInput{ContentID: video.ID, MediaMode: "video", MediaIDs: []int64{media.ID}, CoverMediaID: cover.ID, Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	if len(videoPlan.Snapshot.Targets) != 4 || len(videoPlan.Snapshot.Media) != 2 || videoPlan.Snapshot.Media[0].Role != "video" || videoPlan.Snapshot.Media[1].Role != "cover" {
		t.Fatalf("video plan=%+v", videoPlan.Snapshot)
	}
	if videoPlan.Snapshot.Targets[1].SettingsJSON != `{"activity":"","allow_download":true,"collection":"","comment_enabled":true,"extension_type":"none","hotspot":"","location":"","self_declaration":"none","visibility":"public"}` {
		t.Fatalf("douyin defaults=%s", videoPlan.Snapshot.Targets[1].SettingsJSON)
	}
	if _, err := service.Freeze(FreezeInput{ContentID: video.ID, MediaMode: "video", MediaIDs: []int64{media.ID}, CoverMediaID: cover.ID, Targets: []TargetInput{{AccountID: douyinID, SettingsJSON: `{"location":"上海","visibility":"public"}`}}}); err != nil {
		t.Fatalf("reviewed douyin settings rejected: %v", err)
	}
}

func TestDouyinGraphTextUsesReviewedPrepareContract(t *testing.T) {
	if !supportsMediaMode("douyin", "graph_text") {
		t.Fatal("douyin graph-text must be accepted after the official creator entry is verified")
	}
}

func TestKuaishouGraphTextUsesReviewedPrepareContract(t *testing.T) {
	if !supportsMediaMode("kuaishou", "graph_text") {
		t.Fatal("kuaishou graph-text must be accepted after the official creator entry is verified")
	}
}

func TestTikTokAndYouTubeUseControlledPrepareContracts(t *testing.T) {
	for _, test := range []struct {
		platform string
		mode     string
	}{
		{platform: "youtube", mode: "video"},
		{platform: "tiktok", mode: "video"},
	} {
		if !supportedPlanPlatforms[test.platform] || !supportsMediaMode(test.platform, test.mode) {
			t.Fatalf("%s %s must be accepted by the controlled publish plan", test.platform, test.mode)
		}
	}
	if supportsMediaMode("youtube", "graph_text") || supportsMediaMode("youtube", "article") {
		t.Fatal("YouTube must not be exposed as graph-text or article publishing")
	}
	if !supportsMediaMode("tiktok", "graph_text") {
		t.Fatal("TikTok Photo Post must remain an explicit manual-handoff publish contract")
	}
}
