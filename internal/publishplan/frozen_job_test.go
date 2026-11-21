package publishplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestFrozenInputForJobIgnoresLiveContentMutationAndRejectsMediaReplacement(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent(
		"graph_text",
		"frozen title",
		"frozen caption\n"+AlphaGamerAIDisclosure,
		"frozen body",
		`[]`,
		"draft",
	)
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen.png")
	const mediaBody = "frozen-media"
	if err := os.WriteFile(mediaPath, []byte(mediaBody), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(mediaBody))
	media, err := database.CreateMediaAsset(content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBody)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "frozen retry", "")
	if err != nil {
		t.Fatal(err)
	}
	markPlanAccountLoggedIn(t, database, account.ID)
	service := &Service{DB: database}
	policy := AlphaGamerCompliancePolicy()
	plan, err := service.Freeze(FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID},
		CompliancePolicy: &policy,
		Targets:          []TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePlanForTest(t, service, plan)
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	target := plan.Snapshot.Targets[0]
	job, created, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash,
		0, content.ID, target.AccountID, target.AssetID, target.PlatformCode, plan.MediaMode,
		"frozen-retry:0001", "prepare", target.SettingsJSON,
	)
	if err != nil || !created {
		t.Fatalf("job=%+v created=%v err=%v", job, created, err)
	}
	if err := database.ReplaceContent(content.ID, "graph_text", "mutated title", "mutated caption", "mutated body", `[]`, "draft"); err != nil {
		t.Fatal(err)
	}
	frozen, err := service.FrozenInputForJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Content.Title != "frozen title" ||
		frozen.Content.Caption != "frozen caption\n"+AlphaGamerAIDisclosure ||
		frozen.MediaPath != mediaPath {
		t.Fatalf("frozen input=%+v", frozen)
	}
	wantCompliance, err := json.Marshal(plan.Snapshot.Targets[0].ComplianceFields)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.ComplianceJSON != string(wantCompliance) {
		t.Fatalf("frozen compliance=%s want=%s", frozen.ComplianceJSON, wantCompliance)
	}
	if _, err := database.Exec(`UPDATE account_assets SET name='drifted target' WHERE id=?`, target.AssetID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FrozenInputForJob(job); err == nil || !strings.Contains(err.Error(), "PUBLISH_TARGET_SNAPSHOT_MISMATCH") {
		t.Fatalf("drifted target error=%v", err)
	}
	if _, err := database.Exec(`UPDATE account_assets SET name=? WHERE id=?`, target.AssetName, target.AssetID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("replaced-media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FrozenInputForJob(job); err == nil || !strings.Contains(err.Error(), "PUBLISH_FROZEN_MEDIA_MISMATCH") {
		t.Fatalf("replaced media error=%v", err)
	}
}
