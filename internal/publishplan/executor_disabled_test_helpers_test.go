//go:build !legacy_chromium

package publishplan

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func approvePlanForTest(t *testing.T, service *Service, plan *Plan) {
	t.Helper()
	grant, err := service.Approve(plan.ID, plan.SnapshotHash, "desktop", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConsumeApproval(plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce); err != nil {
		t.Fatal(err)
	}
}

func markPlanAccountLoggedIn(t *testing.T, database *db.DB, accountID int64) {
	t.Helper()
	if err := database.UpdateAccountLoginState(accountID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
}

func newSubmitPlanFixture(t *testing.T) (*db.DB, *Plan, any) {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "Meta title", "Meta caption", "Meta body", `["meta"]`, "draft")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "meta.png")
	const payload = "meta-image"
	if err := os.WriteFile(imagePath, []byte(payload), 0o600); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	image, err := database.CreateMediaAsset(content.ID, "image", imagePath, imagePath, "image/png", int64(len(payload)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("instagram", "Meta operator", "")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	asset, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "AlphaIntelligence", "ig-public-1", "", 0, "")
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	service := &Service{DB: database}
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID,
		MediaMode: "graph_text",
		MediaIDs:  []int64{image.ID},
		Targets:   []TargetInput{{AccountID: account.ID, AssetID: asset.ID}},
	})
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	return database, plan, nil
}
