package egolite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/egopublish"
)

func TestAutomatedPublishContractsUseManifestAndStopAtHandoff(t *testing.T) {
	contracts := make([]struct {
		platformCode string
		contentType  string
	}, 0, 15)
	for _, manifest := range egopublish.EgoLiteManifests() {
		for _, capability := range manifest.Capabilities {
			if capability.PrepareMode != egopublish.PrepareAutomated {
				continue
			}
			contracts = append(contracts, struct {
				platformCode string
				contentType  string
			}{manifest.PlatformCode, capability.ContentType})
		}
	}
	if len(contracts) != 15 {
		t.Fatalf("automated manifest contracts=%d, want 15", len(contracts))
	}

	for _, contract := range contracts {
		contract := contract
		t.Run(contract.platformCode+"/"+contract.contentType, func(t *testing.T) {
			database, identity, page, expectedComposer := automatedPublishContractFixture(t, contract.platformCode, contract.contentType)
			runner := &fakeProgramRunner{publishPrepare: map[string]any{
				"done":    true,
				"results": []map[string]any{{"platformCode": contract.platformCode, "status": "awaiting_review"}},
			}}
			service := egolite.NewSpaceService(database, runner)

			batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
			if err != nil {
				t.Fatal(err)
			}
			if batch.State != "awaiting_review" {
				t.Fatalf("batch state=%q, want awaiting_review: %+v", batch.State, batch)
			}
			if len(batch.PreparedPlatformCodes) != 1 || batch.PreparedPlatformCodes[0] != contract.platformCode || len(batch.PlatformErrors) != 0 {
				t.Fatalf("batch=%+v", batch)
			}
			if len(runner.calls) < 3 {
				t.Fatalf("runtime calls=%d, want space binding, prepare, and completion", len(runner.calls))
			}

			program := runner.calls[len(runner.calls)-2]
			assertJavaScriptSyntax(t, program)
			composerJSON, err := json.Marshal(expectedComposer)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{
				"const preparationResults = []",
				contract.platformCode,
				strings.Trim(string(composerJSON), `"`),
				"awaiting_review",
			} {
				if !strings.Contains(program, required) {
					t.Fatalf("automated prepare program missing %q: %s", required, program)
				}
			}
			for _, forbidden := range []string{
				"const submitResults = []",
				"submit approved publish",
				"click(finalSelector",
				"click(publishSelector",
			} {
				if strings.Contains(program, forbidden) {
					t.Fatalf("automated prepare crossed final publish boundary %q: %s", forbidden, program)
				}
			}
			if !strings.Contains(program, `if (existingSpace.ownership === "user")`) ||
				!strings.Contains(program, `throw new Error("EGO_SPACE_USER_CONTROLLED")`) {
				t.Fatalf("automated prepare lost the user-controlled fail-closed guard: %s", program)
			}

			space, err := database.GetEgoSpaceByIdentity(identity.ID)
			if err != nil {
				t.Fatal(err)
			}
			if space.Ownership != "agent" || space.State != "ready" {
				t.Fatalf("space=%+v, want retained agent/ready Space", space)
			}
			completion := runner.calls[len(runner.calls)-1]
			if !strings.Contains(completion, "completeTaskSpace(") || !strings.Contains(completion, "{ keep: true }") ||
				strings.Contains(completion, "handOffTaskSpace(") || strings.Contains(completion, "takeOverTaskSpace(") {
				t.Fatalf("publish completion must retain tabs without user handoff: %s", completion)
			}
			var jobs, records int
			if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records`).Scan(&records); err != nil {
				t.Fatal(err)
			}
			if jobs != 0 || records != 0 {
				t.Fatalf("prepare created persistent publish state: jobs=%d records=%d", jobs, records)
			}
		})
	}
}

func TestPublishPrepareSerializesOneTargetBeforeOpeningEgoLite(t *testing.T) {
	database, identity, first, _ := automatedPublishContractFixture(t, "douyin", "graph_text")
	_, _, second, _ := automatedPublishContractFixture(t, "csdn", "article")
	runner := &fakeProgramRunner{}
	_, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(
		context.Background(), identity.ID, []egolite.PublishPage{first, second},
	)
	if err == nil || err.Error() != "EGO_PUBLISH_BATCH_INVALID" {
		t.Fatalf("err=%v, want EGO_PUBLISH_BATCH_INVALID", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("multi-target prepare opened EgoLite: calls=%d", len(runner.calls))
	}
}

func automatedPublishContractFixture(t *testing.T, platformCode, contentType string) (*db.DB, *db.EgoIdentity, egolite.PublishPage, string) {
	t.Helper()
	page := egolite.PublishPage{
		PlatformCode: platformCode,
		ContentType:  contentType,
		Title:        "自动合约标题",
		Caption:      "自动合约说明",
		Body:         "自动合约正文",
		TagsJSON:     `[]`,
	}

	var database *db.DB
	var identity *db.EgoIdentity
	var expectedComposer string
	if platformCode == "facebook" || platformCode == "instagram" {
		instagramID := ""
		if platformCode == "instagram" {
			instagramID = "17841416733781085"
		}
		var pageAsset, instagramAsset *db.AccountAsset
		var meta *db.Account
		database, identity, meta, pageAsset, instagramAsset = newMetaPublishFixture(t, instagramID)
		if platformCode == "instagram" {
			if err := database.UpdateAccountAssetConnection(instagramAsset.ID, pageAsset.ID, "facebook", "facebook_page", pageAsset.Name, pageAsset.ExternalID); err != nil {
				t.Fatal(err)
			}
			if err := database.UpdateAccountAssetConnection(pageAsset.ID, instagramAsset.ID, "instagram", "instagram_account", instagramAsset.Name, instagramAsset.ExternalID); err != nil {
				t.Fatal(err)
			}
			page.AssetExternalID = instagramAsset.ExternalID
			page.AssetName = instagramAsset.Name
			expectedComposer, _ = egopublish.ComposerURLFor(platformCode, contentType, pageAsset.ExternalID)
		} else {
			page.AssetExternalID = pageAsset.ExternalID
			page.AssetName = pageAsset.Name
			expectedComposer, _ = egopublish.ComposerURLFor(platformCode, contentType, pageAsset.ExternalID)
		}
		_ = meta
	} else {
		database = openServiceDB(t)
		var err error
		identity, err = database.CreateEgoIdentity("自动合约浏览器")
		if err != nil {
			t.Fatal(err)
		}
		account, err := database.CreateAccountForIdentity(identity.ID, platformCode, platformCode, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
			t.Fatal(err)
		}
		expectedComposer, _ = egopublish.ComposerURLFor(platformCode, contentType, "")
	}

	switch {
	case platformCode == "youtube":
		page.SettingsJSON = `{"audience":"not_made_for_kids","visibility":"unlisted","playlist":"AlphaNexus","allow_comments":false,"altered_content":true}`
	case platformCode == "tiktok":
		page.SettingsJSON = `{"visibility":"friends","allow_comment":false,"allow_duet":true,"allow_stitch":true,"ai_generated":true,"allow_download":false}`
	case platformCode == "wechat_video":
		page.SettingsJSON = `{"link_type":"none","declare_original":false}`
	case platformCode == "wechat_mp":
		page.SettingsJSON = `{"author":"Alpha Nexus","original_link":"","comment_enabled":false,"declare_original":false,"allow_reprint":false,"publish_type":"publish","collection":"","content_source":"none"}`
	}
	if platformCode == "wechat_mp" {
		page.TagsJSON = ""
	}

	switch contentType {
	case "video":
		page.Media = []egolite.PublishMedia{automatedPublishMedia(t, "video", "video")}
		if platformCode == "youtube" || platformCode == "tiktok" {
			page.Media = append(page.Media, automatedPublishMedia(t, "image", "cover"))
		}
	case "graph_text":
		role := "body"
		if platformCode == "xhs" {
			role = "image"
		}
		page.Media = []egolite.PublishMedia{automatedPublishMedia(t, "image", role)}
	case "article":
		if platformCode == "wechat_mp" {
			page.Media = []egolite.PublishMedia{automatedPublishMedia(t, "image", "content")}
		}
	}
	return database, identity, page, expectedComposer
}

func automatedPublishMedia(t *testing.T, kind, role string) egolite.PublishMedia {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("automated-%s-%s.bin", kind, role))
	payload := []byte("automated-publish-contract-" + kind + "-" + role)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	return egolite.PublishMedia{FilePath: path, SHA256: hex.EncodeToString(digest[:]), Kind: kind, Role: role}
}
