//go:build legacy_chromium

package meta

import (
	"context"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestComposerEntryURLsInstagramVsFacebook(t *testing.T) {
	ig := ComposerEntryURLs("instagram", "graph_image")
	if len(ig) == 0 || !strings.Contains(ig[0], "instagram") {
		t.Fatalf("ig feed should prefer instagram_composer, got %v", ig)
	}
	igReel := ComposerEntryURLs("instagram", "reel")
	if len(igReel) == 0 || !strings.Contains(igReel[0], "reel") {
		t.Fatalf("ig reel should prefer reels composer, got %v", igReel)
	}
	fb := ComposerEntryURLs("facebook", "graph_text")
	if len(fb) == 0 || !strings.Contains(fb[0], "composer") {
		t.Fatalf("fb should open composer, got %v", fb)
	}
	// Facebook reel path
	fbReel := ComposerEntryURLs("facebook", "facebook_reel")
	joined := strings.Join(fbReel, " ")
	if !strings.Contains(joined, "reel") {
		t.Fatalf("fb reel urls should mention reel: %v", fbReel)
	}
}

func TestMediaRequired(t *testing.T) {
	if !mediaRequired("instagram", "image") {
		t.Fatal("ig image requires media")
	}
	if !mediaRequired("instagram", "reel") {
		t.Fatal("ig reel requires media")
	}
	if !mediaRequired("instagram", "graph_text") {
		t.Fatal("project graph_text always contains images")
	}
	if !mediaRequired("facebook", "photo") {
		t.Fatal("fb photo requires media")
	}
	if !mediaRequired("facebook", "graph_text") {
		t.Fatal("project graph_text always contains images")
	}
}

func TestEnsureFailureDiagnosticsSetsErrorCode(t *testing.T) {
	r := ensureFailureDiagnostics(&platforms.PublishResult{
		Status: "manual_takeover",
		Step:   StepReviewOrSubmit,
	})
	if r.ErrorCode != "AWAITING_HUMAN" {
		t.Fatalf("error_code=%q", r.ErrorCode)
	}
	// Does not overwrite existing codes
	r2 := ensureFailureDiagnostics(&platforms.PublishResult{
		Status:    "manual_takeover",
		ErrorCode: "PUBLISH_RESULT_UNKNOWN",
	})
	if r2.ErrorCode != "PUBLISH_RESULT_UNKNOWN" {
		t.Fatalf("should keep code, got %q", r2.ErrorCode)
	}
	// Success paths untouched
	ok := ensureFailureDiagnostics(&platforms.PublishResult{Status: "published"})
	if ok.ErrorCode != "" {
		t.Fatalf("published should not invent error_code: %q", ok.ErrorCode)
	}
}

func TestNeverPublishWithoutSuccessSignalContract(t *testing.T) {
	// Contract: AutoSubmit path returns published ONLY via clearSuccess branch.
	// Unit-level: unknown outcome must be manual_takeover + error_code.
	res := ensureFailureDiagnostics(&platforms.PublishResult{
		Status:     "manual_takeover",
		Step:       StepReviewOrSubmit,
		ErrorCode:  "PUBLISH_RESULT_UNKNOWN",
		Screenshot: "/tmp/fake.png",
		Message:    "success not confirmed",
	})
	if res.Status == "published" {
		t.Fatal("must not publish without clear success")
	}
	if res.ErrorCode == "" || res.Screenshot == "" {
		t.Fatalf("G2 AC3: failure path needs error_code+screenshot path when available: %+v", res)
	}
}

func TestPublishSuccessEvidenceMustBeNewAfterClick(t *testing.T) {
	oldToast := "Your post has been shared"
	if hasNewPublishSuccessEvidence([]string{oldToast}, []string{oldToast}) {
		t.Fatal("a pre-existing success toast must not prove this click succeeded")
	}
	if !hasNewPublishSuccessEvidence([]string{oldToast}, []string{oldToast, "Reel published successfully"}) {
		t.Fatal("a new exact success signal should be accepted")
	}
	if hasNewPublishSuccessEvidence(nil, []string{"Published posts", "Schedule post", "Draft saved"}) {
		t.Fatal("generic page text must not become publish evidence")
	}
	if !hasNewPublishSuccessEvidence(nil, []string{"作品发布成功"}) {
		t.Fatal("new Chinese success signal should be accepted")
	}
}

func TestDryRunInstagramReelListsUploadStep(t *testing.T) {
	p := &Publisher{platform: "instagram"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:   1,
		AssetID:     2,
		Caption:     "reel cap",
		AssetType:   "instagram_account",
		ContentType: "reel",
		MediaPath:   "/tmp/reel.mp4",
		AutoSubmit:  true,
		DryRun:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
	if !strings.Contains(res.Message, StepUploadMedia) {
		t.Fatalf("reel dry-run should plan upload: %s", res.Message)
	}
}
