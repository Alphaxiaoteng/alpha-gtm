package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/publishplan"
)

type publishPlanContractFixture struct {
	accountID int64
	assetID   int64
}

func TestPublishPlanFreezeViaRegistryAcceptsAllEgoLiteContracts(t *testing.T) {
	fixtures, contentIDs, mediaIDs, dataDir := makePublishPlanContractFixtures(t)
	runtime := &Runtime{DataDir: dataDir}

	contractCount := 0
	planIDs := make([]int64, 0, 40)
	for _, manifest := range egopublish.EgoLiteManifests() {
		for _, capability := range manifest.Capabilities {
			contentType := capability.ContentType
			contractCount++
			fixture := fixtures[manifest.PlatformCode]
			target := map[string]interface{}{"account_id": fixture.accountID}
			if manifest.PlatformCode == "facebook" || manifest.PlatformCode == "instagram" {
				target["asset_id"] = fixture.assetID
			}
			result, err := Default.Call(context.Background(), runtime, "publish.plan.freeze", map[string]interface{}{
				"content_id": contentIDs[contentType],
				"media_mode": contentType,
				"media_ids":  mediaIDs[contentType],
				"targets":    []map[string]interface{}{target},
			})
			if err != nil || result == nil || !result.OK {
				t.Errorf("freeze %s/%s: result=%+v err=%v", manifest.PlatformCode, contentType, result, err)
				continue
			}
			plan, ok := result.Data["plan"].(*publishplan.Plan)
			if !ok || plan == nil {
				t.Errorf("freeze %s/%s returned unexpected plan=%T", manifest.PlatformCode, contentType, result.Data["plan"])
				continue
			}
			if plan.Concurrency != 1 || plan.Snapshot.Concurrency != 1 {
				t.Errorf("freeze %s/%s concurrency=(plan:%d snapshot:%d), want 1", manifest.PlatformCode, contentType, plan.Concurrency, plan.Snapshot.Concurrency)
			}
			if plan.SnapshotHash == "" || plan.MediaMode != contentType || len(plan.Snapshot.Targets) != 1 || plan.Snapshot.Targets[0].PlatformCode != manifest.PlatformCode {
				t.Errorf("freeze %s/%s snapshot identity invalid: hash=%q mode=%q targets=%+v", manifest.PlatformCode, contentType, plan.SnapshotHash, plan.MediaMode, plan.Snapshot.Targets)
			}
			assertPublishPlanContractMedia(t, manifest.PlatformCode, contentType, plan.Snapshot.Media)

			// Registry get opens and migrates a fresh SQLite handle, proving the
			// frozen snapshot survives the same close/reopen boundary used by the UI.
			recoveredResult, recoveredErr := Default.Call(context.Background(), runtime, "publish.plan.get", map[string]interface{}{"plan_id": plan.ID})
			if recoveredErr != nil || recoveredResult == nil || !recoveredResult.OK {
				t.Errorf("recover %s/%s plan %d: result=%+v err=%v", manifest.PlatformCode, contentType, plan.ID, recoveredResult, recoveredErr)
				continue
			}
			recovered, ok := recoveredResult.Data["plan"].(*publishplan.Plan)
			if !ok || recovered == nil {
				t.Errorf("recover %s/%s returned unexpected plan=%T", manifest.PlatformCode, contentType, recoveredResult.Data["plan"])
				continue
			}
			if recovered.SnapshotHash != plan.SnapshotHash || recovered.MediaMode != contentType || recovered.Concurrency != 1 || recovered.Snapshot.Targets[0].PlatformCode != manifest.PlatformCode {
				t.Errorf("recover %s/%s changed frozen identity: before=%+v after=%+v", manifest.PlatformCode, contentType, plan, recovered)
			}
			planIDs = append(planIDs, plan.ID)
		}
	}

	if contractCount != 41 {
		t.Fatalf("manifest contract count=%d, want 41", contractCount)
	}
	if len(planIDs) != contractCount {
		t.Fatalf("frozen plan count=%d, want %d", len(planIDs), contractCount)
	}

	listResult, listErr := Default.Call(context.Background(), runtime, "publish.plan.list", map[string]interface{}{"limit": 100})
	if listErr != nil || listResult == nil || !listResult.OK {
		t.Fatalf("list frozen contracts: result=%+v err=%v", listResult, listErr)
	}
	summaries, ok := listResult.Data["plans"].([]publishplan.PlanSummary)
	if !ok || len(summaries) != contractCount {
		t.Fatalf("list frozen contracts=%T/%d, want []publishplan.PlanSummary/%d", listResult.Data["plans"], len(summaries), contractCount)
	}
}

func TestPublishPlanFreezeViaRegistryRejectsUnknownPlatformAndContentType(t *testing.T) {
	fixtures, contentIDs, mediaIDs, dataDir := makePublishPlanContractFixtures(t)
	runtime := &Runtime{DataDir: dataDir}

	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	// custom is a valid stored platform, but it is intentionally outside the
	// EgoLite publish manifest and must not become freeze-capable by accident.
	unknown, err := database.CreateAccount("custom", "unsupported platform", "")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(unknown.ID, db.AccountAuthLoggedIn); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	unknownPlatform, unknownPlatformErr := Default.Call(context.Background(), runtime, "publish.plan.freeze", map[string]interface{}{
		"content_id": contentIDs["article"],
		"media_mode": "article",
		"media_ids":  mediaIDs["article"],
		"targets":    []map[string]interface{}{{"account_id": unknown.ID}},
	})
	if unknownPlatformErr == nil || unknownPlatform == nil || unknownPlatform.OK {
		t.Fatalf("unknown platform was accepted: result=%+v err=%v", unknownPlatform, unknownPlatformErr)
	}

	valid := fixtures["xhs"]
	invalidType, invalidTypeErr := Default.Call(context.Background(), runtime, "publish.plan.freeze", map[string]interface{}{
		"content_id": contentIDs["graph_text"],
		"media_mode": "not-a-content-type",
		"media_ids":  mediaIDs["graph_text"],
		"targets":    []map[string]interface{}{{"account_id": valid.accountID}},
	})
	if invalidTypeErr == nil || invalidType == nil || invalidType.OK {
		t.Fatalf("unknown content type was accepted: result=%+v err=%v", invalidType, invalidTypeErr)
	}
}

func makePublishPlanContractFixtures(t *testing.T) (map[string]publishPlanContractFixture, map[string]int64, map[string][]int64, string) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}

	article, err := database.CreateContent("article", "contract article", "article caption", "article body", `["contract"]`, "draft")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	graphText, err := database.CreateContent("graph_text", "contract graph", "graph caption", "graph body", `["contract"]`, "draft")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	video, err := database.CreateContent("video", "contract video", "video caption", "video body", `["contract"]`, "draft")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}

	imagePath := writePublishPlanFixtureFile(t, dataDir, "contract.png", "contract-image")
	imageHash := sha256Hex([]byte("contract-image"))
	image, err := database.CreateMediaAsset(graphText.ID, "image", imagePath, imagePath, "image/png", int64(len("contract-image")), imageHash, 1200, 900, 0)
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	videoPath := writePublishPlanFixtureFile(t, dataDir, "contract.mp4", "contract-video")
	videoHash := sha256Hex([]byte("contract-video"))
	videoAsset, err := database.CreateMediaAsset(video.ID, "video", videoPath, videoPath, "video/mp4", int64(len("contract-video")), videoHash, 1080, 1920, 1000)
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	coverPath := writePublishPlanFixtureFile(t, dataDir, "contract-cover.jpg", "contract-cover")
	coverHash := sha256Hex([]byte("contract-cover"))
	_, err = database.CreateMediaAsset(video.ID, "image", coverPath, coverPath, "image/jpeg", int64(len("contract-cover")), coverHash, 1080, 1920, 0)
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}

	fixtures := make(map[string]publishPlanContractFixture)
	metaAccount, err := database.CreateAccount("meta", "contract meta", "")
	if err != nil {
		_ = database.Close()
		t.Fatalf("create Meta account: %v", err)
	}
	if err := database.UpdateAccountLoginState(metaAccount.ID, db.AccountAuthLoggedIn); err != nil {
		_ = database.Close()
		t.Fatalf("login Meta account: %v", err)
	}
	for _, manifest := range egopublish.EgoLiteManifests() {
		if manifest.PlatformCode == "facebook" || manifest.PlatformCode == "instagram" {
			assetType := "facebook_page"
			if manifest.PlatformCode == "instagram" {
				assetType = "instagram_account"
			}
			asset, err := database.CreateAccountAsset(metaAccount.ID, manifest.PlatformCode, assetType, "contract "+manifest.PlatformCode+" asset", manifest.PlatformCode+"-asset-001", "", 0, "")
			if err != nil {
				_ = database.Close()
				t.Fatalf("create %s Meta asset: %v", manifest.PlatformCode, err)
			}
			fixtures[manifest.PlatformCode] = publishPlanContractFixture{accountID: metaAccount.ID, assetID: asset.ID}
			continue
		}
		account, err := database.CreateAccount(manifest.PlatformCode, "contract "+manifest.PlatformCode, "")
		if err != nil {
			_ = database.Close()
			t.Fatalf("create %s account: %v", manifest.PlatformCode, err)
		}
		if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
			_ = database.Close()
			t.Fatalf("login %s account: %v", manifest.PlatformCode, err)
		}
		persisted, err := database.GetAccount(account.ID)
		if err != nil || persisted.PlatformCode != manifest.PlatformCode || !persisted.Enabled || persisted.LoginState != db.AccountAuthLoggedIn {
			_ = database.Close()
			t.Fatalf("invalid %s fixture account: account=%+v err=%v", manifest.PlatformCode, persisted, err)
		}
		fixtures[manifest.PlatformCode] = publishPlanContractFixture{accountID: account.ID}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	return fixtures,
		map[string]int64{"article": article.ID, "graph_text": graphText.ID, "video": video.ID},
		map[string][]int64{"article": {}, "graph_text": {image.ID}, "video": {videoAsset.ID}},
		dataDir
}

func writePublishPlanFixtureFile(t *testing.T, dataDir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dataDir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sha256Hex(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func assertPublishPlanContractMedia(t *testing.T, platformCode, contentType string, media []publishplan.MediaSnapshot) {
	t.Helper()
	wantKind := ""
	wantCount := 0
	switch contentType {
	case "article":
		wantCount = 0
	case "graph_text":
		wantKind, wantCount = "image", 1
	case "video":
		wantCount = 2
	default:
		t.Fatalf("unexpected manifest content type %q for %s", contentType, platformCode)
	}
	if len(media) != wantCount {
		t.Errorf("freeze %s/%s media count=%d, want %d: %+v", platformCode, contentType, len(media), wantCount, media)
		return
	}
	if contentType == "graph_text" && (media[0].Kind != wantKind || media[0].Role != "content") {
		t.Errorf("freeze %s/%s media=%+v, want image content", platformCode, contentType, media)
	}
	if contentType == "video" && (media[0].Kind != "video" || media[0].Role != "video" || media[1].Kind != "image" || media[1].Role != "cover") {
		t.Errorf("freeze %s/%s media=%+v, want video+cover", platformCode, contentType, media)
	}
}
