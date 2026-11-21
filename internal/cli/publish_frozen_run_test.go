//go:build legacy_chromium

package cli

import (
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/publishplan"
)

func TestFreezeInputFromPlanKeepsReviewedTargetAndUsesRelativeSchedule(t *testing.T) {
	publishAt := time.Date(2026, 7, 30, 13, 45, 59, 0, time.UTC)
	source := &publishplan.Plan{
		ID: 242, ContentID: 7, MediaMode: "video",
		Snapshot: publishplan.Snapshot{
			Media: []publishplan.MediaSnapshot{
				{ID: 9, Role: "video"},
				{ID: 10, Role: "cover"},
			},
			Targets: []publishplan.TargetSnapshot{{
				AccountID: 5, AssetID: 5, VariantID: 20,
				AccountPlatformCode: "wechat_video", PlatformCode: "wechat_video",
				SettingsJSON: `{"visibility":"public"}`,
			}},
		},
	}
	input, err := freezeInputFromPlan(source, publishAt)
	if err != nil {
		t.Fatal(err)
	}
	if input.ContentID != 7 || input.MediaMode != "video" ||
		len(input.MediaIDs) != 1 || input.MediaIDs[0] != 9 || input.CoverMediaID != 10 ||
		input.PublishAt != "2026-07-30T13:45:00Z" || len(input.Targets) != 1 ||
		input.Targets[0].AccountID != 5 || input.Targets[0].AssetID != 0 ||
		input.Targets[0].VariantID != 20 || input.Targets[0].SettingsJSON != `{"visibility":"public"}` {
		t.Fatalf("input=%+v", input)
	}
}

func TestFreezeInputFromArticlePlanKeepsTheFrozenCoverInArticleMedia(t *testing.T) {
	source := &publishplan.Plan{
		ID: 246, ContentID: 8, MediaMode: "article",
		Snapshot: publishplan.Snapshot{
			Media: []publishplan.MediaSnapshot{{ID: 11, Role: "cover"}},
			Targets: []publishplan.TargetSnapshot{{
				AccountID: 3, AssetID: 3,
				AccountPlatformCode: "wechat_mp", PlatformCode: "wechat_mp",
				SettingsJSON: `{"content_source":"none"}`,
			}},
		},
	}
	input, err := freezeInputFromPlan(source, time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(input.MediaIDs) != 1 || input.MediaIDs[0] != 11 || input.CoverMediaID != 0 || input.PublishAt != "2026-07-30T14:00:00Z" {
		t.Fatalf("article media was not preserved: %+v", input)
	}
}

func TestFreezeInputFromPlanPreservesNativeVideoCover(t *testing.T) {
	source := &publishplan.Plan{
		ID: 279, ContentID: 7, MediaMode: "video",
		Snapshot: publishplan.Snapshot{
			Media: []publishplan.MediaSnapshot{{ID: 9, Role: "video"}},
			Targets: []publishplan.TargetSnapshot{{
				AccountID: 1, AssetID: 1,
				AccountPlatformCode: "xhs", PlatformCode: "xhs",
			}},
		},
	}
	input, err := freezeInputFromPlan(source, time.Date(2026, 7, 31, 3, 14, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !input.NativeVideoCover || input.CoverMediaID != 0 || len(input.MediaIDs) != 1 || input.MediaIDs[0] != 9 {
		t.Fatalf("native video cover was not preserved: %+v", input)
	}
}

func TestFrozenRunRequiresExplicitSubmitConfirmationBeforeOpeningDatabase(t *testing.T) {
	command := newPublishFrozenRunCmd()
	command.SetArgs([]string{"--from-plan", "242"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "PUBLISH_SUBMIT_CONFIRMATION_REQUIRED") {
		t.Fatalf("err=%v", err)
	}
}

func TestFrozenSubmitRequiresExplicitSubmitConfirmationBeforeOpeningDatabase(t *testing.T) {
	command := newPublishFrozenSubmitCmd()
	command.SetArgs([]string{"--plan", "245"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "PUBLISH_SUBMIT_CONFIRMATION_REQUIRED") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveNotPublishedRequiresExplicitConfirmationBeforeOpeningDatabase(t *testing.T) {
	command := newPublishResolveNotPublishedCmd()
	command.SetArgs([]string{"--job", "441", "--evidence", "/tmp/evidence.png"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "PUBLISH_ABSENT_RESOLUTION_CONFIRMATION_REQUIRED") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveScheduledRequiresExplicitConfirmationBeforeOpeningDatabase(t *testing.T) {
	command := newPublishResolveScheduledCmd()
	command.SetArgs([]string{
		"--job", "471",
		"--evidence", "/tmp/scheduled.png",
		"--cancelled-evidence", "/tmp/cancelled.png",
	})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "PUBLISH_SCHEDULED_RESOLUTION_CONFIRMATION_REQUIRED") {
		t.Fatalf("err=%v", err)
	}
}
