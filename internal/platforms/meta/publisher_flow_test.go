//go:build legacy_chromium

package meta

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/platforms"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
)

func TestFlowPlannerStepOrder(t *testing.T) {
	noMedia := FlowPlanner{HasMedia: false, AutoSubmit: false}.Steps()
	wantNoMedia := []string{
		StepOpenSuite, StepSelectTarget, StepSelectComposer, StepFillCaption, StepReviewOrSubmit, StepResult,
	}
	if len(noMedia) != len(wantNoMedia) {
		t.Fatalf("steps=%v want %v", noMedia, wantNoMedia)
	}
	for i := range wantNoMedia {
		if noMedia[i] != wantNoMedia[i] {
			t.Fatalf("step[%d]=%s want %s", i, noMedia[i], wantNoMedia[i])
		}
	}

	withMedia := FlowPlanner{HasMedia: true, AutoSubmit: true}.Steps()
	wantMedia := []string{
		StepOpenSuite, StepSelectTarget, StepSelectComposer, StepUploadMedia, StepFillCaption, StepReviewOrSubmit, StepResult,
	}
	if len(withMedia) != len(wantMedia) {
		t.Fatalf("media steps=%v want %v", withMedia, wantMedia)
	}
	for i := range wantMedia {
		if withMedia[i] != wantMedia[i] {
			t.Fatalf("step[%d]=%s want %s", i, withMedia[i], wantMedia[i])
		}
	}
}

func TestLivePrepareRequiresPublicTargetIdentity(t *testing.T) {
	p := &Publisher{platform: "facebook"}
	if err := p.Validate(platforms.PublishRequest{
		ContentID: 1, AssetID: 2, Caption: "hello", AssetType: "facebook_page",
	}); err == nil {
		t.Fatal("live prepare without a target identity must be rejected")
	}
	if err := p.Validate(platforms.PublishRequest{
		ContentID: 1, AssetID: 2, Caption: "hello", AssetType: "facebook_page", AssetName: "Alpha Page", AssetExternalID: "123456789012345",
	}); err != nil {
		t.Fatalf("identified live target rejected: %v", err)
	}
	if err := p.Validate(platforms.PublishRequest{
		ContentID: 1, AssetID: 2, Caption: "hello", AssetType: "facebook_page", DryRun: true,
	}); err != nil {
		t.Fatalf("dry-run should not require a confirmed live identity: %v", err)
	}
}

func TestMetaTargetScriptRequiresExactPublicIdentity(t *testing.T) {
	if _, ok := metaTargetScript("Alpha Page", "", "selected"); ok {
		t.Fatal("missing public target id must not build a selection script")
	}
	script, ok := metaTargetScript("Alpha Page", "page-123", "choose")
	if !ok || !strings.Contains(script, `"Alpha Page"`) || !strings.Contains(script, `"page-123"`) {
		t.Fatalf("script=%q ok=%v", script, ok)
	}
	if strings.Contains(script, "document.body.innerText") || !strings.Contains(script, "[role=\"option\"]") {
		t.Fatalf("selection script is too broad: %s", script)
	}
}

func TestMetaConnectedTargetScriptRequiresVerifiedPageAndInstagramAssociation(t *testing.T) {
	if _, ok := metaConnectedTargetScript("chenpaiteng", "17841416733781085", "AlphaIntelligence", ""); ok {
		t.Fatal("missing connected Page id must not build a selection script")
	}
	script, ok := metaConnectedTargetScript(
		"chenpaiteng",
		"17841416733781085",
		"AlphaIntelligence",
		"1241410172381837",
	)
	if !ok {
		t.Fatal("expected connected Meta target script")
	}
	for _, required := range []string{
		`"chenpaiteng"`,
		`"17841416733781085"`,
		`"AlphaIntelligence"`,
		`"1241410172381837"`,
		`searchParams.get('asset_id')`,
		`instagram.com`,
		`前往 instagram`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("script missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "document.body.innerText") {
		t.Fatalf("connected selection script is too broad: %s", script)
	}
	if strings.Contains(script, "if (!pageConfirmed)") {
		t.Fatalf("connected target must not depend on a UI-truncated Page name: %s", script)
	}
}

func TestMetaSessionContinuationUsesExactNativeControl(t *testing.T) {
	script := metaLoginContinueScript()
	for _, required := range []string{"用 facebook 继续", "continue with facebook", "data-alpha-nexus-meta-login"} {
		if !strings.Contains(script, required) {
			t.Fatalf("Meta login continuation missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "document.body.innerText") {
		t.Fatalf("Meta login continuation is too broad: %s", script)
	}
}

func TestMetaTargetConfirmationPollWaitsForSPAIdentity(t *testing.T) {
	checks := 0
	waits := 0
	confirmed := pollMetaTargetConfirmation(
		func() bool {
			checks++
			return checks == 3
		},
		func() { waits++ },
	)
	if !confirmed || checks != 3 || waits != 2 {
		t.Fatalf("confirmed=%v checks=%d waits=%d", confirmed, checks, waits)
	}
}

func TestMetaComposerConfirmationPollWaitsForVisibleEditor(t *testing.T) {
	checks := 0
	waits := 0
	confirmed := pollMetaComposerConfirmation(
		func() bool {
			checks++
			return checks == 3
		},
		func() { waits++ },
	)
	if !confirmed || checks != 3 || waits != 2 {
		t.Fatalf("confirmed=%v checks=%d waits=%d", confirmed, checks, waits)
	}
}

func TestMetaComposerScriptsRequireVisibleComposerAndExactCreateControl(t *testing.T) {
	ready := metaComposerReadyScript()
	for _, required := range []string{`[role="combobox"]`, `[contenteditable="true"]`, `resolveComposerScope`} {
		if !strings.Contains(ready, required) {
			t.Fatalf("composer ready script missing %q: %s", required, ready)
		}
	}
	click := metaCreateControlScript("instagram", "graph_text")
	if !strings.Contains(click, "创建帖子") || !strings.Contains(click, "create post") {
		t.Fatalf("graph create script=%s", click)
	}
	reel := metaCreateControlScript("instagram", "video")
	if !strings.Contains(reel, "创建 reels") || !strings.Contains(reel, "create reels") {
		t.Fatalf("reel create script=%s", reel)
	}
	facebookReel := metaCreateControlScript("facebook", "video")
	if !strings.Contains(facebookReel, "创建 reels") || !strings.Contains(facebookReel, "create reels") {
		t.Fatalf("facebook reel create script=%s", facebookReel)
	}
	if urls := ComposerEntryURLs("facebook", "video"); len(urls) == 0 || !strings.Contains(urls[0], "reels_composer") {
		t.Fatalf("facebook video composer urls=%v", urls)
	}
	for _, script := range []string{ready, click, reel, facebookReel} {
		if strings.Contains(script, "document.body.innerText") {
			t.Fatalf("composer script is too broad: %s", script)
		}
	}
}

func TestMetaMediaInputConfirmationReadsAssignedFiles(t *testing.T) {
	script := metaMediaInputConfirmedScript(`[role="dialog"] input[type="file"]`, 2)
	for _, required := range []string{`[role=\"dialog\"] input[type=\"file\"]`, `input.files.length >= expected`} {
		if !strings.Contains(script, required) {
			t.Fatalf("media confirmation script missing %q: %s", required, script)
		}
	}
}

func TestMetaMediaControlScriptsUseExactUploadButtonAndMarkedInput(t *testing.T) {
	click := metaMediaControlScript()
	mark := metaMarkMediaInputScript()
	for _, required := range []string{"添加照片/视频", "add photo/video", "data-alpha-nexus-upload-target"} {
		if !strings.Contains(click+mark, required) {
			t.Fatalf("media control scripts missing %q", required)
		}
	}
	if strings.Contains(click+mark, "document.body.innerText") {
		t.Fatalf("media control scripts are too broad")
	}
}

func TestWaitForMetaFileChooserRequiresBackendInputNode(t *testing.T) {
	events := make(chan *page.EventFileChooserOpened, 1)
	events <- &page.EventFileChooserOpened{BackendNodeID: cdp.BackendNodeID(42)}
	event, ok := waitForMetaFileChooser(events, time.Millisecond)
	if !ok || event.BackendNodeID != cdp.BackendNodeID(42) {
		t.Fatalf("event=%+v ok=%v", event, ok)
	}

	events <- &page.EventFileChooserOpened{}
	if event, ok := waitForMetaFileChooser(events, time.Millisecond); ok || event != nil {
		t.Fatalf("chooser without an input node must fail closed: event=%+v ok=%v", event, ok)
	}
}

func TestMetaMediaAttachmentUsesVisibleNativeRemovalEvidence(t *testing.T) {
	script := metaMediaAttachedScript([]string{"/tmp/video.mp4"})
	for _, required := range []string{"移除照片", "移除视频", "删除", "video.mp4", "100%", `\u200B`} {
		if !strings.Contains(script, required) {
			t.Fatalf("media attachment evidence missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "document.body.innerText") {
		t.Fatalf("media attachment evidence is too broad: %s", script)
	}
}

func TestMetaMediaAttachmentRequiresComposerPreviewAndCompletion(t *testing.T) {
	script := metaMediaAttachedScript([]string{"/tmp/image.png"})
	if !runMetaMediaAttachmentFixture(t, script, "ready") {
		t.Fatal("rootless scoped completed preview must confirm")
	}
	for _, mode := range []string{
		"input-only",
		"outside-remove",
		"processing",
		"duplicate-target",
		"duplicate-editor",
		"body-only",
	} {
		if runMetaMediaAttachmentFixture(t, script, mode) {
			t.Fatalf("mode=%s must not confirm upload completion", mode)
		}
	}
}

func TestMetaMediaAttachmentTimeoutAllowsNativeVideoProcessing(t *testing.T) {
	if got := metaMediaAttachmentTimeout([]string{"/tmp/video.mp4"}); got != 90*time.Second {
		t.Fatalf("video timeout=%s want 90s", got)
	}
	if got := metaMediaAttachmentTimeout([]string{"/tmp/image.png"}); got != 20*time.Second {
		t.Fatalf("image timeout=%s want 20s", got)
	}
}

func TestMetaCaptionAndPublishReadyScriptsAreNativeAndFailClosed(t *testing.T) {
	editor := metaCaptionEditorScript()
	matches := metaCaptionMatchesScript("正文 #标签")
	ready := metaPublishReadyScript("graph_text") + metaPublishReadyScript("video")
	for _, required := range []string{
		`contenteditable="true"`,
		"data-alpha-nexus-caption-editor",
		"在对话框中输入内容",
		"正文 #标签",
		"composer",
		"aria-disabled",
		`replace(/\s+/g, ' ')`,
		"分享",
		`closest('[role="list"]')`,
	} {
		if !strings.Contains(editor+matches+ready, required) {
			t.Fatalf("native composer scripts missing %q", required)
		}
	}
	if strings.Contains(editor+matches+ready, "document.body.innerText") ||
		strings.Contains(ready, ".click()") {
		t.Fatalf("native composer scripts are too broad or can submit")
	}
}

func TestMetaPublishReadyUsesUniqueRootlessScopeAndExactContentControl(t *testing.T) {
	graph := metaPublishReadyScript("graph_text")
	if !runMetaPublishReadyFixture(t, graph, "graph-ready") {
		t.Fatal("rootless graph composer with one enabled 发布 control must be ready")
	}
	video := metaPublishReadyScript("video")
	if !runMetaPublishReadyFixture(t, video, "video-ready") {
		t.Fatal("rootless video composer with one enabled 分享 control must be ready")
	}
	for _, test := range []struct {
		script string
		mode   string
	}{
		{graph, "wrong-control"},
		{graph, "double-final"},
		{graph, "disabled-final"},
		{graph, "outside-final"},
		{graph, "duplicate-target"},
		{graph, "duplicate-editor"},
		{graph, "body-only"},
		{video, "graph-ready"},
	} {
		if runMetaPublishReadyFixture(t, test.script, test.mode) {
			t.Fatalf("mode=%s must not report final control ready", test.mode)
		}
	}
}

func TestMetaReelCoverAndReviewControlsUseNativeEvidence(t *testing.T) {
	scripts := metaCoverTabScript() + metaCoverUploadControlScript() +
		metaCoverAttachedScript() + metaReelNextControlScript() +
		metaScrollComposerToEndScript() + metaDefaultReelCoverReadyScript()
	for _, required := range []string{
		"上传图片",
		"upload image",
		"更换图片",
		"replace image",
		"下一页",
		"next",
		"data-alpha-nexus-cover-upload",
		"data-alpha-nexus-reel-next",
		"scrollTop = node.scrollHeight",
		"缩略图",
	} {
		if !strings.Contains(scripts, required) {
			t.Fatalf("Meta Reels controls missing %q", required)
		}
	}
	if strings.Contains(scripts, "document.body.innerText") ||
		strings.Contains(metaReelNextControlScript(), "分享") {
		t.Fatalf("Meta Reels controls are too broad or can submit")
	}
}

func runMetaMediaAttachmentFixture(t *testing.T, script, mode string) bool {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required by the publish worker test contract: %v", err)
	}
	command := exec.Command(node, filepath.Join("testdata", "meta_media_attachment_fixture.cjs"), mode)
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Meta media attachment fixture: %v\n%s", err, output)
	}
	var result bool
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode Meta media attachment fixture: %v\n%s", err, output)
	}
	return result
}

func runMetaPublishReadyFixture(t *testing.T, script, mode string) bool {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required by the publish worker test contract: %v", err)
	}
	command := exec.Command(node, filepath.Join("testdata", "meta_publish_ready_fixture.cjs"), mode)
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Meta publish-ready fixture: %v\n%s", err, output)
	}
	var result bool
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode Meta publish-ready fixture: %v\n%s", err, output)
	}
	return result
}

func TestMetaPreparationEvidenceIncludesDefaultFrameFallback(t *testing.T) {
	evidence := metaPreparationEvidence([]string{"video"}, "platform_generated_frame")
	if evidence["coverAppliedAs"] != "platform_generated_frame" {
		t.Fatalf("evidence=%+v", evidence)
	}
	if evidence["tagsAppliedAs"] != "caption_hashtags" {
		t.Fatalf("tags missing from evidence=%+v", evidence)
	}
}

func TestAppendPrepNotesDoesNotRepeatComposerState(t *testing.T) {
	got := appendPrepNotes("Composer prepared", true, true, true, "/tmp/video.mp4")
	if strings.Count(strings.ToLower(got), "composer") != 1 {
		t.Fatalf("composer state repeated: %q", got)
	}
	if !strings.Contains(got, "caption fill attempted") || !strings.Contains(got, "media attach attempted") {
		t.Fatalf("useful prep notes missing: %q", got)
	}
}

func TestMetaTargetHomeURLUsesVerifiedPublicAsset(t *testing.T) {
	facebook := metaTargetHomeURL("facebook", platforms.PublishRequest{AssetExternalID: "1241410172381837"})
	if facebook != "https://business.facebook.com/latest/home?asset_id=1241410172381837" {
		t.Fatalf("facebook target URL=%q", facebook)
	}
	instagram := metaTargetHomeURL("instagram", platforms.PublishRequest{
		AssetExternalID:          "17841416733781085",
		ConnectedAssetExternalID: "1241410172381837",
	})
	if instagram != "https://business.facebook.com/latest/home?asset_id=1241410172381837" {
		t.Fatalf("instagram target URL=%q", instagram)
	}
	if got := metaTargetHomeURL("instagram", platforms.PublishRequest{AssetExternalID: "17841416733781085"}); got != "https://business.facebook.com/" {
		t.Fatalf("unlinked Instagram URL=%q", got)
	}
}

func TestDefaultFlowFromRequest(t *testing.T) {
	f := DefaultFlow(platforms.PublishRequest{MediaPath: "/tmp/a.jpg", AutoSubmit: true})
	if !f.HasMedia || !f.AutoSubmit {
		t.Fatalf("got %+v", f)
	}
	structured := DefaultFlow(platforms.PublishRequest{Media: []platforms.PublishMedia{
		{Path: "/tmp/a.mp4", Kind: "video", Role: "video"},
		{Path: "/tmp/cover.jpg", Kind: "image", Role: "cover"},
	}})
	if !structured.HasMedia {
		t.Fatalf("structured media was ignored: %+v", structured)
	}
	f2 := DefaultFlow(platforms.PublishRequest{})
	if f2.HasMedia {
		t.Fatal("empty media should not set HasMedia")
	}
}

func TestMetaMediaPlanSeparatesOrderedContentVideoAndCoverRoles(t *testing.T) {
	video, err := buildMetaMediaPlan(platforms.PublishRequest{Media: []platforms.PublishMedia{
		{Path: "/tmp/cover.jpg", Kind: "image", Role: "cover", Position: 1},
		{Path: "/tmp/video.mp4", Kind: "video", Role: "video", Position: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(video.UploadPaths, ",") != "/tmp/video.mp4" || video.CoverPath != "/tmp/cover.jpg" {
		t.Fatalf("video plan=%+v", video)
	}

	images, err := buildMetaMediaPlan(platforms.PublishRequest{Media: []platforms.PublishMedia{
		{Path: "/tmp/second.jpg", Kind: "image", Role: "content", Position: 1},
		{Path: "/tmp/first.jpg", Kind: "image", Role: "content", Position: 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(images.UploadPaths, ",") != "/tmp/first.jpg,/tmp/second.jpg" || images.CoverPath != "" {
		t.Fatalf("image plan=%+v", images)
	}
}

func TestPublishWithNativeScheduleFailsClosedBeforeCDP(t *testing.T) {
	p := &Publisher{platform: "facebook"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:       1,
		AssetID:         2,
		Caption:         "scheduled post",
		AssetType:       "facebook_page",
		AssetName:       "Alpha Page",
		AssetExternalID: "123456789012345",
		PublishAt:       "2030-01-02T03:04:05Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Status != "manual_takeover" || res.Step != StepSchedule || res.ErrorCode != "META_NATIVE_SCHEDULE_UNVERIFIED" {
		t.Fatalf("scheduled result=%+v", res)
	}
}

func TestPublishWithStructuredCoverReachesBrowserPreflight(t *testing.T) {
	p := &Publisher{platform: "instagram"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:                1,
		ContentType:              "video",
		AssetID:                  2,
		Caption:                  "video post",
		AssetType:                "instagram_account",
		AssetName:                "Alpha IG",
		AssetExternalID:          "987654321098765",
		ConnectedAssetExternalID: "123456789012345",
		Media: []platforms.PublishMedia{
			{Path: "/tmp/video.mp4", Kind: "video", Role: "video"},
			{Path: "/tmp/cover.jpg", Kind: "image", Role: "cover"},
		},
	})
	if err == nil || res == nil || res.Step != StepBrowserAttach || res.ErrorCode == "META_COVER_UPLOAD_UNVERIFIED" {
		t.Fatalf("cover result=%+v", res)
	}
}

func TestMediaUploadFailureResultPreventsSubmit(t *testing.T) {
	plan := metaMediaPlan{UploadPaths: []string{"/tmp/photo.jpg"}}
	res := mediaUploadFailureResult("instagram", "graph_text", plan, false, "")
	if res == nil || res.Status != "manual_takeover" || res.Step != StepUploadMedia || res.ErrorCode != "MEDIA_UPLOAD_UNCONFIRMED" {
		t.Fatalf("upload failure=%+v", res)
	}
	if got := mediaUploadFailureResult("instagram", "graph_text", plan, true, ""); got != nil {
		t.Fatalf("confirmed upload should continue: %+v", got)
	}
}

func TestValidateAndDryRunInstagramFlow(t *testing.T) {
	p := &Publisher{platform: "instagram"}
	req := platforms.PublishRequest{
		ContentID: 1,
		AssetID:   2,
		Caption:   "hello",
		AssetType: "instagram_account",
		AssetName: "ig",
		DryRun:    true,
	}
	if err := p.Validate(req); err != nil {
		t.Fatal(err)
	}
	res, err := p.Publish(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
	if res.Step != StepPreflight {
		t.Fatalf("step=%s", res.Step)
	}
	// Dry-run message embeds planned steps for observability.
	if !strings.Contains(res.Message, StepOpenSuite) || !strings.Contains(res.Message, StepResult) {
		t.Fatalf("expected pipeline steps in dry-run message: %s", res.Message)
	}
}

func TestDryRunWithMediaListsUploadStep(t *testing.T) {
	p := &Publisher{platform: "facebook"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:  1,
		AssetID:    2,
		Caption:    "cap",
		AssetType:  "facebook_page",
		AssetName:  "page",
		MediaPath:  "/tmp/photo.jpg",
		AutoSubmit: true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
	if !strings.Contains(res.Message, StepUploadMedia) {
		t.Fatalf("expected upload_media in steps: %s", res.Message)
	}
	if !strings.Contains(res.Message, "media=") || !strings.Contains(res.Message, "auto_submit=true") {
		t.Fatalf("expected media/auto_submit in message: %s", res.Message)
	}
}

func TestPublishIgnoresLegacySimulateEnvAndRequiresCDP(t *testing.T) {
	t.Setenv("AGENTPUSH_SIMULATE_PUBLISH", "1")

	p := &Publisher{platform: "facebook"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:       1,
		AssetID:         2,
		Caption:         "ci",
		AssetType:       "facebook_page",
		AssetName:       "Alpha Page",
		AssetExternalID: "123456789012345",
		// A legacy environment variable must never bypass live CDP evidence.
	})
	if err == nil {
		t.Fatal("expected error for empty CDP even when legacy simulate env is set")
	}
	if res == nil || res.Status != "retryable_failed" {
		t.Fatalf("expected retryable_failed, got %+v", res)
	}
	if res.ErrorCode != "CDP_CONNECT" {
		t.Fatalf("error_code=%q", res.ErrorCode)
	}
}

func TestPublishFlowRequiresCDPWhenNotDryRun(t *testing.T) {
	p := &Publisher{platform: "instagram"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:                1,
		AssetID:                  2,
		Caption:                  "hello",
		AssetType:                "instagram_account",
		AssetName:                "Alpha IG",
		AssetExternalID:          "987654321098765",
		ConnectedAssetExternalID: "123456789012345",
		DryRun:                   false,
		CDPURL:                   "",
	})
	if err == nil {
		t.Fatal("expected error for empty CDP")
	}
	if res == nil || res.Status != "retryable_failed" {
		t.Fatalf("expected retryable_failed, got %+v", res)
	}
	if res.Step != StepBrowserAttach {
		t.Fatalf("step=%s", res.Step)
	}
}
