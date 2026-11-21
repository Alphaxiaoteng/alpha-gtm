package egolite

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishInteractionPacingIsDeterministicBoundedAndCallerCannotOverrideIt(t *testing.T) {
	tests := []struct {
		platformCode string
		contentType  string
	}{
		{"douyin", "graph_text"},
		{"kuaishou", "video"},
		{"tiktok", "video"},
		{"youtube", "video"},
		{"facebook", "graph_text"},
		{"instagram", "video"},
		{"wechat_mp", "article"},
		{"wechat_video", "video"},
		{"csdn", "article"},
	}
	for _, test := range tests {
		t.Run(test.platformCode+"_"+test.contentType, func(t *testing.T) {
			prepare := publishInteractionPauseMillis(test.platformCode, test.contentType, publishInteractionPrepare)
			if prepare < 450 || prepare > 800 {
				t.Fatalf("prepare pause = %d, want 450..800ms", prepare)
			}
			if repeat := publishInteractionPauseMillis(test.platformCode, test.contentType, publishInteractionPrepare); repeat != prepare {
				t.Fatalf("prepare pause changed between calls: %d then %d", prepare, repeat)
			}
			final := publishInteractionPauseMillis(test.platformCode, test.contentType, publishInteractionFinalSubmit)
			if final < 900 || final > 1500 {
				t.Fatalf("final-submit pause = %d, want 900..1500ms", final)
			}
		})
	}

	targets := publishRuntimeTargetsForExecution([]publishRuntimeTarget{{
		PlatformCode: "csdn", ContentType: "article", PreparePauseMS: 1, FinalPauseMS: 60_000,
	}})
	if len(targets) != 1 || targets[0].PreparePauseMS < 450 || targets[0].PreparePauseMS > 800 || targets[0].FinalPauseMS < 900 || targets[0].FinalPauseMS > 1500 {
		t.Fatalf("runtime did not replace caller pacing with bounded values: %+v", targets)
	}
}

func TestGeneratedPublishProgramsPaceOperationsBeforeNavigationAndFinalClick(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "csdn", ContentType: "article", ComposerURL: "https://editor.csdn.net/md/",
		Title: "标题", Body: "正文",
	}
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	loopAt := strings.Index(prepare, "for (const target of publishTargets)")
	pauseAt := strings.Index(prepare, "await alphaInteractionPause(target.preparePauseMs, 450, 800)")
	navigationAt := strings.Index(prepare, "const tabsBeforeComposer")
	if loopAt < 0 || pauseAt <= loopAt || navigationAt <= pauseAt || strings.Count(prepare, "target.preparePauseMs") != 1 {
		t.Fatalf("prepare pacing must occur once inside the serial target loop before composer navigation")
	}

	submit := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	verifiedAt := strings.Index(submit, `if (!verified || !selector) throw new Error("frozen_form_mismatch")`)
	finalPauseAt := strings.Index(submit, "await alphaInteractionPause(target.finalPauseMs, 900, 1500)")
	clickAt := strings.Index(submit, "await click(selector, { label: 'submit approved publish' })")
	if verifiedAt < 0 || finalPauseAt <= verifiedAt || clickAt <= finalPauseAt || strings.Count(submit, "target.finalPauseMs") != 1 {
		t.Fatalf("submit pacing must occur once after frozen readback and immediately before the approved final click")
	}
}

func TestGeneratedPublishProgramsAreValidJavaScriptModules(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	targets := []publishRuntimeTarget{{
		PlatformCode: "csdn", ContentType: "article", ComposerURL: "https://editor.csdn.net/md/",
		Title: "标题含 ` 与 ${value}", Body: "正文\n第二行", Tags: []string{"AlphaNexus"},
	}}
	programs := map[string]string{
		"prepare":    buildEgoPublishPrepareProgram(14, targets),
		"submit":     buildEgoPublishSubmitProgram(14, targets),
		"completion": buildEgoPublishCompletionProgram(14),
	}
	for name, program := range programs {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".mjs")
			if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("generated JavaScript is invalid: %v\n%s\n%s", err, output, program)
			}
		})
	}
	if strings.Contains(programs["prepare"], "submit approved publish") || strings.Count(programs["submit"], "await click(selector") != 1 {
		t.Fatal("prepare/submit click boundary drifted")
	}
	if strings.Contains(programs["prepare"], "completeTaskSpace(") || strings.Contains(programs["submit"], "completeTaskSpace(") {
		t.Fatal("prepare and submit must report their result before a dedicated Space completion run")
	}
	if !strings.Contains(programs["prepare"], `if (existingSpace.ownership === "user")`) ||
		!strings.Contains(programs["prepare"], "await takeOverTaskSpace(14)") {
		t.Fatal("prepare must fail closed for a user-controlled Space and reclaim only a delegated Space")
	}
	for _, required := range []string{"tabsBeforeComposer", "reusableComposerTab", "publish_composer_tab_limit", "await switchTab(reusableComposerTab"} {
		if !strings.Contains(programs["prepare"], required) {
			t.Fatalf("prepare must reuse its current composer tab or fail at the tab cap: missing %q", required)
		}
	}
	if strings.Contains(programs["prepare"], "handOffTaskSpace(") || strings.Contains(programs["submit"], "handOffTaskSpace(") {
		t.Fatal("prepare and submit must not transfer publish work to a user")
	}
	if !strings.Contains(programs["submit"], "await useOrCreateTaskSpace(14)") ||
		strings.Contains(programs["submit"], "takeOverTaskSpace(") || strings.Contains(programs["submit"], "claimTaskSpace(") {
		t.Fatal("submit must reuse the retained agent Space without taking control from a user")
	}
	if strings.Count(programs["completion"], "await completeTaskSpace(14, { keep: true })") != 1 ||
		!strings.Contains(programs["completion"], "const spaces = await listTaskSpaces()") ||
		!strings.Contains(programs["completion"], `existingSpace.ownership === "user"`) ||
		!strings.Contains(programs["completion"], "await claimTaskSpace(14)") ||
		!strings.Contains(programs["completion"], "cliLog(\"ALPHA_EGO_RESULT:") {
		t.Fatal("dedicated completion must close only the exact retained agent Space and report the result")
	}
	if claimAt := strings.Index(programs["completion"], "await claimTaskSpace(14)"); claimAt < 0 ||
		strings.Index(programs["completion"], "await completeTaskSpace(14, { keep: true })") <= claimAt {
		t.Fatal("dedicated completion must resume the inactive exact Space before completing it")
	}
}

func TestBilibiliVideoPrepareUsesScopedReadbackAndNeverClicksFinalPost(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "bilibili", ContentType: "video", ComposerURL: "https://member.bilibili.com/platform/upload/video/frame",
		Title: "冻结 Bilibili 标题", Body: "冻结 Bilibili 简介",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-bilibili.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		".bcc-upload-wrapper",
		"bilibili_foreign_draft",
		"bilibili_upload_timeout",
		"bilibili_content_readback_mismatch",
		"立即投稿",
		"status = \"awaiting_review\"",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Bilibili prepare missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"await click(bilibiliFinal",
		"await click(finalSelector",
		"submit approved publish",
		"%%",
	} {
		if strings.Contains(program, forbidden) {
			t.Fatalf("Bilibili prepare crossed the final-submit or formatting boundary %q", forbidden)
		}
	}
}

func TestScheduledYouTubePrepareDoesNotClick(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "youtube", ContentType: "video", ComposerURL: "https://studio.youtube.com/channel/upload",
		PublishAt: "2030-01-02T05:00:00Z", Title: "冻结 YouTube 标题", Body: "冻结 YouTube 描述",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-youtube.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		"Asia/Shanghai",
		"uniqueYouTubeScheduleOption",
		"youtubeScheduleReadbackStable",
		"youtube_schedule_final_button_missing_or_ambiguous",
		"await fillInput(timeSelector",
		"await pressKey(\"TAB\")",
		"hour % 12",
		"Schedule",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("scheduled YouTube prepare missing %q", required)
		}
	}
	if strings.Contains(program, "hour %% 12") || strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(finalSelector") {
		t.Fatal("scheduled YouTube prepare crossed the final-click boundary or left fmt percent escaping")
	}
}

func TestScheduledYouTubeReadbackMismatchFailsClosed(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "youtube", ContentType: "video", ComposerURL: "https://studio.youtube.com/channel/upload",
		PublishAt: "2030-01-02T05:00:00Z", Title: "标题", Body: "描述",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-youtube.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		"youtube_schedule_mode_readback_mismatch",
		"youtube_schedule_date_control_missing_or_ambiguous",
		"youtube_schedule_time_control_missing_or_ambiguous",
		"youtube_schedule_time_readback_mismatch",
		"youtube_schedule_final_button_missing_or_ambiguous",
		"if (!await youtubeScheduleSelected())",
		"if (!timeReadback) throw new Error(\"youtube_schedule_time_readback_mismatch\")",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("scheduled YouTube prepare missing fail-closed guard %q", required)
		}
	}
}

func TestYouTubeScheduledSubmitUsesUniqueScheduleAndOneFinalClick(t *testing.T) {
	program := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{{
		PlatformCode: "youtube", ContentType: "video", ComposerURL: "https://studio.youtube.com/channel/upload",
		PublishAt: "2030-01-02T05:00:00Z", Title: "冻结 YouTube 标题", Body: "冻结 YouTube 描述",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-youtube.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		"target.platformCode === \"youtube\"",
		"youtube_submit_title_readback_mismatch",
		"youtube_submit_description_readback_mismatch",
		"youtubeScheduleReadbackStable",
		"uniqueYouTubeScheduleFinalButton",
		"youtube_submit_schedule_mode_readback_mismatch",
		"submitted_pending_verification",
		"EGO_BACKEND_VERIFICATION_REQUIRED",
		"EGO_YOUTUBE_SUBMIT_FINAL_CONTROL_MISSING",
		"EGO_SPACE_USER_CONTROLLED",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("YouTube submit missing %q", required)
		}
	}
	if got := strings.Count(program, "await click(selector, { label: 'submit approved publish' })"); got != 1 {
		t.Fatalf("YouTube submit must have exactly one final click, got %d", got)
	}
	if strings.Contains(program, "await click(scheduleSelector") {
		t.Fatal("YouTube submit must not reselect Schedule during the final submit")
	}
}

func TestYouTubeImmediateSubmitDoesNotUseSchedule(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "youtube", ContentType: "video", ComposerURL: "https://studio.youtube.com/channel/upload",
		Visibility: "public", Title: "即时标题", Body: "即时描述",
		Media: []PublishMedia{{FilePath: "/tmp/immediate-youtube.mp4", Kind: "video", Role: "video"}},
	}
	targetJSON, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(targetJSON), `"publishAt"`) {
		t.Fatal("immediate YouTube fixture unexpectedly has publishAt")
	}
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	submit := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	if !strings.Contains(prepare, "const visibilityLabels = target.visibility === \"private\"") {
		t.Fatal("immediate YouTube prepare no longer uses the existing visibility path")
	}
	if !strings.Contains(submit, "youtube_immediate_submit_unsupported") {
		t.Fatal("immediate YouTube submit must fail closed instead of being treated as scheduled")
	}
}

func TestScheduledTikTokPrepareDoesNotClick(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "tiktok", ContentType: "video", ComposerURL: "https://www.tiktok.com/tiktokstudio/upload",
		PublishAt: "2030-01-02T05:03:00Z", Title: "冻结 TikTok 标题", Caption: "冻结 TikTok caption", Body: "冻结 TikTok caption",
		Visibility: "public", Media: []PublishMedia{{FilePath: "/tmp/frozen-tiktok.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		"Asia/Shanghai",
		"Math.ceil((hour * 60 + minute) / 5) * 5",
		"uniqueTikTokScheduleControl",
		"tiktokScheduleInputSelectors",
		"tiktok_schedule_mode_readback_mismatch",
		"tiktok_schedule_time_readback_mismatch",
		"tiktok_schedule_hour_missing_or_ambiguous",
		"tiktok_schedule_minute_missing_or_ambiguous",
		"tiktok_schedule_final_button_missing_or_disabled",
		"attempt < 2",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("TikTok scheduled prepare missing %q", required)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector, { label: 'submit approved publish' })") {
		t.Fatal("TikTok scheduled prepare crossed the final-click boundary")
	}
}

func TestScheduledTikTokProgramsAreValidJavaScriptModules(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	target := publishRuntimeTarget{
		PlatformCode: "tiktok", ContentType: "video", ComposerURL: "https://www.tiktok.com/tiktokstudio/upload",
		PublishAt: "2030-01-02T05:03:00Z", Title: "标题", Caption: "描述", Body: "描述", Visibility: "public",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-tiktok.mp4", Kind: "video", Role: "video"}},
	}
	for name, program := range map[string]string{
		"prepare": buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target}),
		"submit":  buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target}),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".mjs")
			if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("generated scheduled TikTok JavaScript is invalid: %v\n%s", err, output)
			}
		})
	}
}

func TestTikTokScheduledSubmitUsesCaptionReadbackAndOneFinalClick(t *testing.T) {
	program := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{{
		PlatformCode: "tiktok", ContentType: "video", ComposerURL: "https://www.tiktok.com/tiktokstudio/upload",
		PublishAt: "2030-01-02T05:03:00Z", Title: "冻结标题", Caption: "冻结 caption", Body: "冻结 caption", Visibility: "public",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-tiktok.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		"tiktok_immediate_submit_unsupported",
		"tiktok_submit_caption_readback_mismatch",
		"tiktok_submit_schedule_mode_readback_mismatch",
		"tiktok_submit_schedule_readback_mismatch",
		"uniqueTikTokSubmitFinalButton",
		"submitted_pending_verification",
		"EGO_BACKEND_VERIFICATION_REQUIRED",
		"EGO_TIKTOK_SUBMIT_FINAL_CONTROL_MISSING",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("TikTok scheduled submit missing %q", required)
		}
	}
	if got := strings.Count(program, "await click(selector, { label: 'submit approved publish' })"); got != 1 {
		t.Fatalf("TikTok submit must have exactly one final click, got %d", got)
	}
	if strings.Contains(program, "await click(scheduleSelector") {
		t.Fatal("TikTok submit must not select or mutate Schedule during final submit")
	}
}

func TestTikTokImmediateSubmitFailsClosed(t *testing.T) {
	program := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{{
		PlatformCode: "tiktok", ContentType: "video", ComposerURL: "https://www.tiktok.com/tiktokstudio/upload",
		Title: "即时标题", Caption: "即时 caption", Body: "即时 caption", Visibility: "public",
		Media: []PublishMedia{{FilePath: "/tmp/immediate-tiktok.mp4", Kind: "video", Role: "video"}},
	}})
	if !strings.Contains(program, "tiktok_immediate_submit_unsupported") {
		t.Fatal("immediate TikTok submit must fail closed")
	}
	if strings.Contains(program, "target.platformCode === \"tiktok\" && !target.publishAt") {
		t.Fatal("TikTok immediate guard must be inside the TikTok submit branch")
	}
}

func TestScheduledTikTokSupportMatrix(t *testing.T) {
	if !scheduledPublishTargetSupported("tiktok", "video") {
		t.Fatal("TikTok video must support scheduled publishing")
	}
	if scheduledPublishTargetSupported("tiktok", "article") {
		t.Fatal("TikTok article must remain unsupported")
	}
}

func TestScheduledKuaishouPrepareDoesNotClick(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "kuaishou", ContentType: "video", ComposerURL: "https://cp.kuaishou.com/article/publish/video",
		PublishAt: "2030-01-02T05:03:00Z", Title: "冻结快手标题", Body: "冻结快手正文",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-kuaishou.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		"Asia/Shanghai",
		"chooseKuaishouDateTimePicker",
		"ant-picker-dropdown",
		"ant-picker-cell",
		"kuaishouScheduleReadbackStable",
		"kuaishou_schedule_mode_readback_mismatch",
		"kuaishou_schedule_time_readback_mismatch",
		"kuaishou_schedule_final_button_missing_or_disabled",
		"await fillInput",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Kuaishou scheduled prepare missing %q", required)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector, { label: 'submit approved publish' })") {
		t.Fatal("Kuaishou scheduled prepare crossed the final-click boundary")
	}
}

func TestScheduledKuaishouProgramsAreValidJavaScriptModules(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	target := publishRuntimeTarget{
		PlatformCode: "kuaishou", ContentType: "video", ComposerURL: "https://cp.kuaishou.com/article/publish/video",
		PublishAt: "2030-01-02T05:03:00Z", Title: "标题", Body: "正文",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-kuaishou.mp4", Kind: "video", Role: "video"}},
	}
	for name, program := range map[string]string{
		"prepare": buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target}),
		"submit":  buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target}),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".mjs")
			if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("generated scheduled Kuaishou JavaScript is invalid: %v\n%s", err, output)
			}
		})
	}
}

func TestKuaishouScheduledSubmitUsesStableReadbackAndOneFinalClick(t *testing.T) {
	program := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{{
		PlatformCode: "kuaishou", ContentType: "video", ComposerURL: "https://cp.kuaishou.com/article/publish/video",
		PublishAt: "2030-01-02T05:03:00Z", Title: "冻结快手标题", Body: "冻结快手正文",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-kuaishou.mp4", Kind: "video", Role: "video"}},
	}})
	for _, required := range []string{
		"kuaishou_immediate_submit_unsupported",
		"kuaishou_submit_title_readback_mismatch",
		"kuaishou_submit_body_readback_mismatch",
		"kuaishou_submit_schedule_readback_mismatch",
		"kuaishouScheduleReadbackStable",
		"kuaishou_submit_final_button_ambiguous",
		"submitted_pending_verification",
		"result_unknown",
		"EGO_BACKEND_VERIFICATION_REQUIRED",
		"EGO_KUAISHOU_SUBMIT_FINAL_CONTROL_MISSING",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Kuaishou scheduled submit missing %q", required)
		}
	}
	if got := strings.Count(program, "await click(selector, { label: 'submit approved publish' })"); got != 1 {
		t.Fatalf("Kuaishou submit must have exactly one final click, got %d", got)
	}
	if strings.Contains(program, "await click(scheduleSelector") {
		t.Fatal("Kuaishou submit must not reselect or mutate schedule during final submit")
	}
	if strings.Contains(program, `status = "published"`) {
		t.Fatal("Kuaishou submit must never fabricate published status")
	}
}

func TestKuaishouImmediateSubmitFailsClosed(t *testing.T) {
	program := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{{
		PlatformCode: "kuaishou", ContentType: "video", ComposerURL: "https://cp.kuaishou.com/article/publish/video",
		Title: "即时标题", Body: "即时正文",
		Media: []PublishMedia{{FilePath: "/tmp/immediate-kuaishou.mp4", Kind: "video", Role: "video"}},
	}})
	if !strings.Contains(program, "kuaishou_immediate_submit_unsupported") {
		t.Fatal("immediate Kuaishou submit must fail closed")
	}
}

func TestWechatVideoScheduledPrepareAndSubmitUseIframeReadbackAndOneFinalClick(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "wechat_video", ContentType: "video", ComposerURL: "https://channels.weixin.qq.com/platform/post/create",
		PublishAt: "2030-01-02T05:03:00Z", Title: "冻结视频号标题", Body: "冻结视频号正文", Tags: []string{"AlphaNexus"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-wechat-video.mp4", Kind: "video", Role: "video"}},
	}
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	submit := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	for _, required := range []string{
		"https://channels.weixin.qq.com/micro/content/post/create", "Asia/Shanghai",
		"wechatVideoExpectedSchedule", "uniqueWechatVideoScheduleControl", "setWechatVideoScheduleInput",
		"wechatVideoScheduleReadbackStable", "wechat_video_schedule_time_readback_mismatch",
	} {
		if !strings.Contains(prepare, required) {
			t.Fatalf("WeChat Video scheduled prepare missing %q", required)
		}
	}
	if strings.Contains(prepare, `clickWechatVideoFrameControl("submit-final")`) || strings.Contains(prepare, "submit approved publish") {
		t.Fatal("WeChat Video prepare crossed the final-click boundary")
	}
	for _, required := range []string{
		"wechat_video_immediate_submit_unsupported", "wechat_video_submit_title_readback_mismatch",
		"wechat_video_submit_body_readback_mismatch", "wechat_video_submit_schedule_readback_mismatch",
		"uniqueWechatVideoSubmitFinalButton", `clickWechatVideoFrameControl("submit-final")`,
		"EGO_WECHAT_VIDEO_SUBMIT_FINAL_CONTROL_MISSING", "EGO_BACKEND_VERIFICATION_REQUIRED", "result_unknown",
	} {
		if !strings.Contains(submit, required) {
			t.Fatalf("WeChat Video scheduled submit missing %q", required)
		}
	}
	if got := strings.Count(submit, `clickWechatVideoFrameControl("submit-final")`); got != 1 {
		t.Fatalf("WeChat Video submit must have exactly one iframe final click, got %d", got)
	}
	if strings.Contains(submit, `clickWechatVideoFrameControl("submit-schedule-mode")`) || strings.Contains(submit, "setWechatVideoScheduleInput(scheduleInputSelector") {
		t.Fatal("WeChat Video submit must only read back the frozen schedule")
	}
	if strings.Contains(submit, `status = "published"`) {
		t.Fatal("WeChat Video submit must never fabricate published status")
	}
}

func TestWechatMPArticleSubmitOnlyClicksOneFrozenEditorFinalControl(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "wechat_mp", ContentType: "article", ComposerURL: "https://mp.weixin.qq.com/",
		Title: "冻结公众号标题", Body: "冻结公众号正文", Author: "Alpha Nexus",
		CommentEnabled: false, DeclareOriginal: false, AllowReprint: false,
		PublishType: "publish", ContentSource: "none",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-wechat-article.png", Kind: "image", Role: "content"}},
	}
	program := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	for _, required := range []string{
		`target.platformCode === "wechat_mp"`, "wechat_mp_submit_editor_missing_or_ambiguous",
		"wechat_mp_submit_title_readback_mismatch", "wechat_mp_submit_settings_readback_mismatch", "wechat_mp_submit_final_button_ambiguous",
		"EGO_WECHAT_MP_SUBMIT_COMPOSER_MISSING", "EGO_WECHAT_MP_SUBMIT_FINAL_CONTROL_MISSING",
		"submitted_pending_verification", "result_unknown", "LOGIN_REQUIRED",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("WeChat Official Account submit missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"wechatMPSetField(", "uploadFile(", "await click(advanceSelector", "从正文选择",
	} {
		if strings.Contains(program, forbidden) {
			t.Fatalf("WeChat Official Account submit mutated frozen prepared form with %q", forbidden)
		}
	}
	if got := strings.Count(program, "await click(selector, { label: 'submit approved publish' })"); got != 1 {
		t.Fatalf("WeChat Official Account submit must have exactly one final click, got %d", got)
	}
	if strings.Contains(program, `status = "published"`) {
		t.Fatal("WeChat Official Account submit must never fabricate published status")
	}
	if node, err := exec.LookPath("node"); err == nil {
		path := filepath.Join(t.TempDir(), "wechat-mp-submit.mjs")
		if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
			t.Fatalf("generated WeChat Official Account submit JavaScript is invalid: %v\n%s", err, output)
		}
	}
}

func TestScheduledWechatVideoProgramsAreValidJavaScriptModules(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	target := publishRuntimeTarget{
		PlatformCode: "wechat_video", ContentType: "video", ComposerURL: "https://channels.weixin.qq.com/platform/post/create",
		PublishAt: "2030-01-02T05:03:00Z", Title: "标题", Body: "正文",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-wechat-video.mp4", Kind: "video", Role: "video"}},
	}
	for name, program := range map[string]string{
		"prepare": buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target}),
		"submit":  buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target}),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".mjs")
			if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("generated scheduled WeChat Video JavaScript is invalid: %v\n%s", err, output)
			}
		})
	}
}

func TestScheduledYouTubeProgramsAreValidJavaScriptModules(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	target := publishRuntimeTarget{
		PlatformCode: "youtube", ContentType: "video", ComposerURL: "https://studio.youtube.com/channel/upload",
		PublishAt: "2030-01-02T05:00:00Z", Title: "标题", Body: "描述",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-youtube.mp4", Kind: "video", Role: "video"}},
	}
	for name, program := range map[string]string{
		"prepare": buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target}),
		"submit":  buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target}),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".mjs")
			if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("generated scheduled YouTube JavaScript is invalid: %v\n%s", err, output)
			}
		})
	}
}

func TestScheduledPublishTargetSupportMatrix(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		contentType string
		want        bool
	}{
		{name: "douyin graph text", platform: "douyin", contentType: "graph_text", want: true},
		{name: "douyin video", platform: "douyin", contentType: "video", want: true},
		{name: "douyin article", platform: "douyin", contentType: "article", want: false},
		{name: "youtube video", platform: "youtube", contentType: "video", want: true},
		{name: "tiktok video", platform: "tiktok", contentType: "video", want: true},
		{name: "kuaishou video", platform: "kuaishou", contentType: "video", want: true},
		{name: "kuaishou graph text", platform: "kuaishou", contentType: "graph_text", want: true},
		{name: "kuaishou article", platform: "kuaishou", contentType: "article", want: false},
		{name: "facebook page graph text", platform: "facebook", contentType: "graph_text", want: true},
		{name: "facebook page video", platform: "facebook", contentType: "video", want: true},
		{name: "instagram graph text", platform: "instagram", contentType: "graph_text", want: false},
		{name: "wechat article", platform: "wechat_mp", contentType: "article", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scheduledPublishTargetSupported(test.platform, test.contentType); got != test.want {
				t.Fatalf("scheduledPublishTargetSupported(%q, %q)=%v, want %v", test.platform, test.contentType, got, test.want)
			}
		})
	}
}

func TestFacebookScheduledProgramsUseNativeMetaScheduleWithoutPrepareFinalClick(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "facebook", ContentType: "graph_text",
		ComposerURL: "https://business.facebook.com/latest/composer/?asset_id=1241410172381837",
		MetaPageID:  "1241410172381837", MetaPageName: "Alpha Page",
		PublishAt: "2030-01-02T05:00:00Z", Caption: "冻结 Facebook 文案", Body: "冻结 Facebook 文案",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-facebook.png", Kind: "image", Role: "content"}},
	}
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	submit := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	for _, required := range []string{
		"Scheduling options", "Set date and time", "Asia/Shanghai", "meta_schedule",
		"meta_schedule_readback_mismatch", "metaPageID", "Alpha Page", "Schedule", "定时发布",
	} {
		if !strings.Contains(prepare, required) {
			t.Fatalf("Facebook scheduled prepare missing %q: %s", required, prepare)
		}
	}
	for _, required := range []string{
		"business.facebook.com", "searchParams.get(\"asset_id\")", "metaPageID", "Alpha Page",
		"meta_submit_caption_readback_mismatch", "meta_submit_media_readback_mismatch",
		"meta_submit_schedule_readback_mismatch", "Schedule", "定时发布", "result_unknown",
	} {
		if !strings.Contains(submit, required) {
			t.Fatalf("Facebook scheduled submit missing %q: %s", required, submit)
		}
	}
	for _, forbidden := range []string{"submit approved publish", "click(metaFinalSelector", "click(metaSubmitFinalSelector"} {
		if strings.Contains(prepare, forbidden) {
			t.Fatalf("Facebook prepare crossed final publish boundary %q: %s", forbidden, prepare)
		}
	}
	if strings.Contains(submit, "click(metaScheduleSelector") || strings.Contains(submit, "click(metaSetDateTimeSelector") {
		t.Fatalf("Facebook submit must only click the final Schedule control: %s", submit)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	for name, program := range map[string]string{"prepare": prepare, "submit": submit} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".mjs")
			if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("generated Facebook JavaScript is invalid: %v\n%s", err, output)
			}
		})
	}
}

func TestGeneratedDouyinScheduleProgramsRequireExactReadbackBeforeSubmit(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "douyin", ContentType: "video", ComposerURL: "https://creator.douyin.com/creator-micro/content/post/video",
		PublishAt: "2030-01-02T13:00:00+08:00", Title: "冻结抖音标题", Body: "冻结抖音正文",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.mp4", Kind: "video", Role: "video"}},
	}
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	submit := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	for _, required := range []string{
		"douyin_schedule_mode_selector",
		"douyin_schedule_time_selector",
		"日期和时间",
		"douyin_schedule_time_readback_mismatch",
		"douyin_final_button_missing_or_ambiguous",
		"target.publishAt",
		"EGO_DOUYIN_UPLOAD_FAILED",
		"EGO_DOUYIN_SCHEDULE_TIME_CONTROL_MISSING",
		"reviewedPrepareErrorCode",
		"data-alpha-douyin-title",
		"EGO_DOUYIN_TITLE_READBACK_MISMATCH",
	} {
		if !strings.Contains(prepare, required) && !strings.Contains(submit, required) {
			t.Fatalf("prepare/submit programs missing schedule contract %q", required)
		}
	}
	for _, required := range []string{
		"douyin_submit_title_readback_mismatch",
		"douyin_submit_body_readback_mismatch",
		"douyin_submit_schedule_readback_mismatch",
		"douyin_submit_final_button_ambiguous",
		"submit approved publish",
		"EGO_DOUYIN_SUBMIT_CONTENT_READBACK_MISMATCH",
		"EGO_DOUYIN_SUBMIT_TITLE_READBACK_MISMATCH",
		"EGO_DOUYIN_SUBMIT_BODY_READBACK_MISMATCH",
		"normalizeDouyinTitle",
		"normalizeDouyinBody",
		"titleCandidates",
		"bodyCandidates",
	} {
		if !strings.Contains(submit, required) {
			t.Fatalf("submit program missing final-boundary contract %q", required)
		}
	}
	if strings.Contains(prepare, "submit approved publish") {
		t.Fatal("prepare program crossed final submit boundary")
	}
}

func TestDouyinExpectedBodyStripsOnlyCompleteUniqueFrozenTrailingTags(t *testing.T) {
	tests := []struct {
		name        string
		description string
		tags        []string
		want        string
	}{
		{
			name:        "complete unique trailing set is stripped",
			description: "正文\n\n#αgamer #阿尔法玩家 #游戏内容",
			tags:        []string{"αgamer", "阿尔法玩家", "游戏内容"},
			want:        "正文",
		},
		{
			name:        "incomplete trailing set is preserved",
			description: "正文\n\n#αgamer #阿尔法玩家",
			tags:        []string{"αgamer", "阿尔法玩家", "游戏内容"},
			want:        "正文\n\n#αgamer #阿尔法玩家",
		},
		{
			name:        "extra trailing tag is preserved",
			description: "正文\n\n#αgamer #阿尔法玩家 #游戏内容 #额外",
			tags:        []string{"αgamer", "阿尔法玩家", "游戏内容"},
			want:        "正文\n\n#αgamer #阿尔法玩家 #游戏内容 #额外",
		},
		{
			name:        "duplicate trailing tag is preserved",
			description: "正文\n\n#αgamer #阿尔法玩家 #游戏内容 #游戏内容",
			tags:        []string{"αgamer", "阿尔法玩家", "游戏内容"},
			want:        "正文\n\n#αgamer #阿尔法玩家 #游戏内容 #游戏内容",
		},
		{
			name:        "matching set outside trailing line is preserved",
			description: "#αgamer #阿尔法玩家 #游戏内容\n正文",
			tags:        []string{"αgamer", "阿尔法玩家", "游戏内容"},
			want:        "#αgamer #阿尔法玩家 #游戏内容\n正文",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := douyinExpectedBody(test.description, test.tags); got != test.want {
				t.Fatalf("douyinExpectedBody()=%q, want %q", got, test.want)
			}
		})
	}
}

func TestDouyinPrepareAndSubmitShareExpectedBodyAndNormalizationContract(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Caption:     "正文\n\n#αgamer #阿尔法玩家 #游戏内容", Tags: []string{"αgamer", "阿尔法玩家", "游戏内容"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.png", Kind: "image", Role: "image"}},
	}
	expectedJSON, err := json.Marshal(douyinExpectedBody(target.Caption, target.Tags))
	if err != nil {
		t.Fatal(err)
	}
	needle := `"expectedBody":` + string(expectedJSON)
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	submit := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	for name, program := range map[string]string{"prepare": prepare, "submit": submit} {
		for _, required := range []string{
			needle,
			"normalizeDouyinTitle",
			"normalizeDouyinBody",
			"douyinCandidateMatches",
			"titleCandidates",
			"bodyCandidates",
		} {
			if !strings.Contains(program, required) {
				t.Fatalf("%s program missing shared Douyin body contract %q", name, required)
			}
		}
	}
}

func TestGeneratedDouyinMultiImagePrepareUsesNativeArrayUploadAndFailsClosed(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文",
		Media: []PublishMedia{
			{FilePath: "/tmp/frozen-first.png", Kind: "image", Role: "image"},
			{FilePath: "/tmp/frozen-second.png", Kind: "image", Role: "image"},
		},
	}
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	required := []string{
		"target.media.length > 1",
		"node.multiple === true",
		"await uploadFile(imageInputSelector, target.media.map((item) => item.filePath))",
		"douyin_image_file_input_multiple_unsupported",
		"douyin_image_upload_file_readback_mismatch",
		"EGO_DOUYIN_IMAGE_INPUT_MULTIPLE_UNSUPPORTED",
		"EGO_DOUYIN_IMAGE_UPLOAD_READBACK_MISMATCH",
		"/creator-micro/content/post/image",
		"douyin_upload_still_in_progress",
	}
	for _, contract := range required {
		if !strings.Contains(program, contract) {
			t.Fatalf("multi-image prepare program missing %q", contract)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector") {
		t.Fatalf("multi-image prepare crossed the final-submit boundary: %s", program)
	}
}

func TestDouyinGeneratedRichEditorWriteIsSinglePassWithEndCaret(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.png", Kind: "image", Role: "image"}},
	}})
	start := strings.Index(program, "const fillRichEditor")
	end := strings.Index(program, "const uniqueDouyinScheduleControl")
	if start < 0 || end <= start {
		t.Fatal("fillRichEditor helper boundary missing")
	}
	helper := program[start:end]
	if strings.Contains(helper, "apply(probe)") || strings.Contains(helper, "probe") || strings.Contains(helper, "apply(") {
		t.Fatal("fillRichEditor must not use probe or a second apply pass")
	}
	if strings.Count(helper, "dispatchEvent(new InputEvent('input'") != 1 {
		t.Fatal("fillRichEditor must dispatch exactly one input event")
	}
	for _, required := range []string{
		"node.textContent=value",
		"const selection=window.getSelection();if(!selection)return false",
		"range.selectNodeContents(node)",
		"range.collapse(false)",
		"selection.removeAllRanges()",
		"selection.addRange(range)",
		"selection.rangeCount!==1||!selection.isCollapsed",
		"node.contains(selection.anchorNode)",
		"node.contains(selection.focusNode)",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("fillRichEditor missing end-caret fail-closed contract %q", required)
		}
	}
	if !strings.Contains(program, "douyin_topic_preflight_readback_runtime_failed") {
		t.Fatal("Douyin prepare must classify preflight readback runtime failures")
	}
}

func TestDouyinGeneratedRichEditorUsesNativeTypingAndStableReadback(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.png", Kind: "image", Role: "image"}},
	}})
	start := strings.Index(program, "const fillDouyinRichEditor")
	end := strings.Index(program, "const uniqueDouyinScheduleControl")
	if start < 0 || end <= start {
		t.Fatal("Douyin native rich editor helper boundary missing")
	}
	helper := program[start:end]
	for _, required := range []string{
		"getAttribute('contenteditable')!=='true'",
		"node.focus()",
		"range.selectNodeContents(node)",
		"selection.removeAllRanges()",
		"selection.addRange(range)",
		"selection.rangeCount===1&&inside(selection.anchorNode)&&inside(selection.focusNode)",
		"await typeText(value)",
		"await douyinBodyAndTopicReadback(selector)",
		"douyinCandidateMatches(readback.bodyCandidates, value, normalizeDouyinBody)",
		"for (let attempt = 0; attempt < 2; attempt += 1)",
		"setTimeout(resolve, 350)",
		"throw new Error(\"douyin_body_readback_unstable\")",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("Douyin native rich editor helper missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"textContent=value",
		"new InputEvent",
		"dispatchEvent(",
	} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("Douyin native rich editor helper must not use synthetic DOM input %q", forbidden)
		}
	}
	if strings.Count(helper, "await typeText(value)") != 1 {
		t.Fatalf("Douyin native rich editor must type exactly once, got %d", strings.Count(helper, "await typeText(value)"))
	}

	bodyBranch := strings.Index(program, "if (fields.bodyTag === 'textarea')")
	if bodyBranch < 0 {
		t.Fatal("Douyin body input branch missing")
	}
	bodyBranchEnd := strings.Index(program[bodyBranch:], "let expectedTopics = []")
	if bodyBranchEnd <= 0 {
		t.Fatal("Douyin body input branch boundary missing")
	}
	bodyBranchText := program[bodyBranch : bodyBranch+bodyBranchEnd]
	if !strings.Contains(bodyBranchText, "if (fields.bodyTag === 'textarea')") || !strings.Contains(bodyBranchText, "await fillInput(fields.body, expectedBody)") {
		t.Fatal("Douyin textarea body must continue using fillInput")
	}
	if !strings.Contains(bodyBranchText, "target.platformCode === \"douyin\"") || !strings.Contains(bodyBranchText, "fillDouyinRichEditor(fields.body, expectedBody)") {
		t.Fatal("Douyin contenteditable body must use the native typing helper")
	}
	douyinBranch := strings.Index(bodyBranchText, "else if (target.platformCode === \"douyin\")")
	genericBranch := strings.Index(bodyBranchText, "else if (!await fillRichEditor(fields.body, expectedBody))")
	if douyinBranch < 0 || genericBranch <= douyinBranch {
		t.Fatalf("Douyin contenteditable body must use its native helper before the generic branch: douyin=%d generic=%d", douyinBranch, genericBranch)
	}

	mappingStart := strings.Index(program, "const reviewedPrepareErrorCode")
	mappingEnd := strings.Index(program[mappingStart:], "for (const target of publishTargets)")
	if mappingStart < 0 || mappingEnd <= 0 {
		t.Fatal("Douyin prepare error mapping boundary missing")
	}
	mapping := program[mappingStart : mappingStart+mappingEnd]
	for _, required := range []string{
		`if (/douyin_body_selection_failed/i.test(message)) return "EGO_DOUYIN_BODY_SELECTION_FAILED"`,
		`if (/douyin_body_input_failed/i.test(message)) return "EGO_DOUYIN_BODY_INPUT_FAILED"`,
		`if (/douyin_body_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_RUNTIME_FAILED"`,
		`if (/douyin_body_readback_invalid/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_INVALID"`,
		`if (/douyin_body_readback_unstable/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_UNSTABLE"`,
	} {
		if !strings.Contains(mapping, required) {
			t.Fatalf("Douyin native rich editor error mapping missing %q", required)
		}
	}
}

func TestDouyinTopicPreflightClassifiesSafeFailuresAndWrapsFinalReadback(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
	}})
	preflightStart := strings.Index(program, "let beforeTopics")
	if preflightStart < 0 {
		t.Fatal("Douyin topic preflight boundary missing")
	}
	preflightEnd := strings.Index(program[preflightStart:], "expectedTopics = await commitDouyinNativeTopics")
	if preflightEnd <= 0 {
		t.Fatal("Douyin topic preflight boundary missing")
	}
	preflight := program[preflightStart : preflightStart+preflightEnd]
	branches := []string{
		`beforeTopics = await douyinBodyAndTopicReadback(fields.body)`,
		`throw new Error("douyin_topic_preflight_readback_runtime_failed")`,
		`throw new Error("douyin_topic_preflight_readback_invalid")`,
		`throw new Error("douyin_topic_preflight_topics_present")`,
		`throw new Error("douyin_topic_preflight_body_mismatch")`,
	}
	previous := -1
	for _, branch := range branches {
		position := strings.Index(preflight, branch)
		if position < 0 {
			t.Fatalf("Douyin topic preflight missing branch %q", branch)
		}
		if position <= previous {
			t.Fatalf("Douyin topic preflight branches are out of order: %q at %d after %d", branch, position, previous)
		}
		previous = position
	}
	if strings.Contains(preflight, "douyin_body_unstable_before_topics") {
		t.Fatal("Douyin topic preflight must not collapse failures into the old generic error")
	}

	finalStart := strings.Index(program, "let bodyAndTopics")
	if finalStart < 0 {
		t.Fatal("final Douyin body readback boundary missing")
	}
	finalEnd := strings.Index(program[finalStart:], "if (target.publishAt)")
	if finalEnd <= 0 {
		t.Fatal("final Douyin body readback boundary missing")
	}
	finalReadback := program[finalStart : finalStart+finalEnd]
	runtimeError := strings.Index(finalReadback, `throw new Error("douyin_body_readback_runtime_failed")`)
	bodyCheck := strings.Index(finalReadback, `if (!douyinCandidateMatches(bodyAndTopics?.bodyCandidates`)
	if runtimeError < 0 || bodyCheck < 0 || runtimeError >= bodyCheck {
		t.Fatalf("final body readback runtime error must precede body judgment: runtime=%d body=%d", runtimeError, bodyCheck)
	}
}

func TestGeneratedDouyinGraphTextUsesExactNativeTopicContract(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "冻结标题", Caption: "正文\n\n#αgamer #阿尔法玩家 #游戏内容",
		Tags:  []string{"αgamer", "阿尔法玩家", "游戏内容"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.png", Kind: "image", Role: "image"}},
	}
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	submit := buildEgoPublishSubmitProgram(14, []publishRuntimeTarget{target})
	for _, required := range []string{
		"commitDouyinNativeTopics",
		"const stableDouyinBodyReadback = async (selector, expectedValue)",
		"for (let attempt = 0; attempt < 3; attempt += 1)",
		"douyinBodyAndTopicReadback(selector)",
		"readback.invalid",
		"douyin_topic_body_readback_mismatch",
		"douyin_topic_body_readback_drift",
		"douyin_topic_body_readback_unstable",
		"setTimeout(resolve, 100)",
		"await stableDouyinBodyReadback(bodySelector, expectedBody)",
		"['#添加话题','添加话题']",
		"const exact=(node)=>names.has",
		"!body?.contains(node)&&exact(node)",
		"const reviewed=node.closest('button,a,[role=button],[role=link]')",
		"reviewed&&visible(reviewed)&&exact(reviewed)?reviewed:node",
		`[data-mention='#']`,
		`[data-topic-selected='true']`,
		"expected.length > 3",
		"#creator-editor-topic-container .item",
		"item.querySelector('.name')",
		"for (let suggestionAttempt = 0; suggestionAttempt < 6; suggestionAttempt += 1)",
		"if (suggestionSelector === \"__ambiguous__\") throw new Error(\"douyin_topic_suggestion_ambiguous\")",
		"if (suggestionSelector) break",
		"candidates.length!==1",
		`if (!suggestionSelector)`,
		"for (let attempt = 0; attempt < 6; attempt += 1)",
		"setTimeout(resolve, 500)",
		"let nativeCommitted = false",
		"if (!nativeCommitted)",
		"douyinExactTopicSetMatches",
		"douyinBodyAndTopicReadback",
		"copy.querySelectorAll(topicSelector).forEach((node)=>node.remove())",
		"EGO_DOUYIN_TOPIC_TRIGGER_FAILED",
		"EGO_DOUYIN_TOPIC_SUGGESTION_FAILED",
		"EGO_DOUYIN_TOPIC_COMMIT_PRECONDITION_FAILED",
		"EGO_DOUYIN_TOPIC_NATIVE_COMMIT_FAILED",
		"EGO_DOUYIN_TOPIC_READBACK_MISMATCH",
	} {
		if !strings.Contains(prepare, required) {
			t.Fatalf("prepare program missing exact native topic contract %q", required)
		}
	}
	for _, required := range []string{
		"douyinExpectedTopics(target.tags)",
		"douyinExactTopicSetMatches",
		"douyinBodyAndTopicReadback",
		"EGO_DOUYIN_SUBMIT_TOPIC_READBACK_MISMATCH",
	} {
		if !strings.Contains(submit, required) {
			t.Fatalf("submit program missing exact native topic revalidation %q", required)
		}
	}
	if strings.Contains(submit, "commitDouyinNativeTopics") {
		t.Fatal("submit must only revalidate native topics, never re-enter them")
	}
	commitStart := strings.Index(prepare, "const commitDouyinNativeTopics")
	commitEnd := strings.Index(prepare, "const reviewedPrepareErrorCode")
	if commitStart < 0 || commitEnd <= commitStart {
		t.Fatal("prepare native topic helper boundary missing")
	}
	commitProgram := prepare[commitStart:commitEnd]
	stableStart := strings.Index(commitProgram, "const stableDouyinBodyReadback")
	stableCall := strings.Index(commitProgram, "await stableDouyinBodyReadback(bodySelector, expectedBody)")
	triggerStart := strings.Index(commitProgram, "let triggerSelector")
	if stableStart < 0 || stableCall < stableStart || triggerStart <= stableCall {
		t.Fatal("Douyin stable body readback must run before the topic trigger")
	}
	if strings.Contains(commitProgram[stableStart:triggerStart], "douyin_topic_body_readback_mismatch") == false {
		t.Fatal("Douyin stable body readback must fail closed on body drift")
	}
	if strings.Contains(commitProgram, "Enter") {
		t.Fatal("Douyin native topic commit must never blindly accept a suggestion with Enter")
	}
	if strings.Contains(commitProgram, `await pressKey("SPACE")`) {
		t.Fatal("Douyin native topic commit must not accept plain hashtag text with Space")
	}
	if strings.Contains(commitProgram, ".includes(String(node.innerText") {
		t.Fatal("Douyin topic suggestions must not use broad text containment matching")
	}
	if strings.Contains(commitProgram, "document.body?.innerText") || strings.Contains(commitProgram, "document.body.innerText") {
		t.Fatal("Douyin topic trigger must not inspect broad body text")
	}
	if strings.Count(commitProgram, "await click(suggestionSelector") != 1 {
		t.Fatal("Douyin native topic polling must not re-click the exact suggestion")
	}
	pollStart := strings.Index(commitProgram, "for (let suggestionAttempt = 0; suggestionAttempt < 6; suggestionAttempt += 1)")
	ambiguousStop := strings.Index(commitProgram, `if (suggestionSelector === "__ambiguous__") throw new Error("douyin_topic_suggestion_ambiguous")`)
	fallbackStart := strings.Index(commitProgram, "if (!suggestionSelector)")
	if pollStart < 0 || ambiguousStop <= pollStart || fallbackStart <= ambiguousStop {
		t.Fatal("Douyin exact suggestion polling and ambiguity stop must complete before the safe missing-suggestion stop")
	}
	if strings.Count(commitProgram[pollStart:fallbackStart], "setTimeout(resolve, 500)") != 1 {
		t.Fatal("Douyin exact suggestion polling must use one bounded 500ms wait per iteration")
	}
	if strings.Contains(commitProgram[pollStart:fallbackStart], "startsWith(") || strings.Contains(commitProgram[pollStart:fallbackStart], ".includes(String(node.innerText") {
		t.Fatal("Douyin exact suggestion polling must not introduce fuzzy text matching")
	}
}

func TestGeneratedDouyinBodyAndTopicReadbackFoldsOnlyNestedMarkedNodes(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏"},
	}})
	start := strings.Index(program, "const douyinBodyAndTopicReadback")
	end := strings.Index(program, "const wechatVideoFramePrefix")
	if end < 0 {
		end = strings.Index(program, "const preparationResults")
	}
	if start < 0 || end <= start {
		t.Fatal("Douyin body/topic readback helper boundary missing")
	}
	helper := program[start:end]
	for _, required := range []string{
		"const rawNodes=[...root.querySelectorAll(topicSelector)]",
		"const nodes=rawNodes.filter((node)=>!rawNodes.some((other)=>other!==node&&other.contains(node)))",
		"for(const node of nodes)",
		"const candidate=nativeTopicName || text",
		"topics.push(candidate.slice(1).trim())",
		"copy.querySelectorAll(topicSelector).forEach((node)=>node.remove())",
		"const liveBodyCandidates=[root.innerText,root.textContent]",
		"if(nodes.length===0)return {bodyCandidates:liveBodyCandidates,topics,invalid}",
		"const container=document.createElement('div')",
		"container.setAttribute('aria-hidden','true')",
		"container.setAttribute('inert','')",
		"container.style.pointerEvents='none'",
		"container.style.opacity='0'",
		"container.appendChild(copy)",
		"document.body.appendChild(container)",
		"renderedText=copy.innerText",
		"cloneTextContent=copy.textContent",
		"finally{container.remove()}",
		"bodyCandidates:[renderedText,cloneTextContent]",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("Douyin body/topic readback missing containment contract %q", required)
		}
	}
	attachIndex := strings.Index(helper, "document.body.appendChild(container)")
	readIndex := strings.Index(helper, "renderedText=copy.innerText")
	if attachIndex < 0 || readIndex <= attachIndex {
		t.Fatal("Douyin topic clone innerText must be read only after temporary attachment")
	}
	if strings.Contains(helper, "return {bodyCandidates:[copy.innerText,copy.textContent]") {
		t.Fatal("detached Douyin clone innerText must not be the rendered body candidate")
	}
	if strings.Contains(helper, "new Set") || strings.Contains(helper, "Set(") {
		t.Fatal("Douyin body/topic readback must preserve duplicate sibling topics and never Set-deduplicate them")
	}
}

func TestDouyinTopicReadbackUsesNativeTopicMetadata(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏"},
	}})
	start := strings.Index(program, "const douyinBodyAndTopicReadback")
	end := strings.Index(program, "const preparationResults")
	if start < 0 || end <= start {
		t.Fatal("Douyin body/topic readback helper boundary missing")
	}
	helper := program[start:end]
	for _, required := range []string{
		"a.tiptap-topic[data-topic]",
		"let nativeTopicName = ''",
		"JSON.parse(node.getAttribute('data-topic') || '{}').name",
		"nativeTopicName || text",
		"copy.querySelectorAll(topicSelector).forEach((node)=>node.remove())",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("Douyin native topic readback missing metadata contract %q", required)
		}
	}
	if strings.Contains(helper, "topics.push(text.slice(1).trim())") {
		t.Fatal("Douyin native topic readback must prefer the platform metadata over hidden display text")
	}
}

func TestGeneratedDouyinNativeTopicCommitRequiresTwoStableBodyAndTopicReadbacks(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.png", Kind: "image", Role: "image"}},
	}})
	commitStart := strings.Index(program, "const commitDouyinNativeTopics")
	commitEnd := strings.Index(program, "const reviewedPrepareErrorCode")
	if commitStart < 0 || commitEnd <= commitStart {
		t.Fatal("prepare native topic helper boundary missing")
	}
	commitProgram := program[commitStart:commitEnd]
	nativeStart := strings.Index(commitProgram, "let nativeCommitted = false")
	nativeEnd := strings.Index(commitProgram[nativeStart:], "if (!nativeCommitted)")
	if nativeStart < 0 || nativeEnd <= 0 {
		t.Fatalf("native topic commit polling boundary missing: start=%d end=%d", nativeStart, nativeEnd)
	}
	nativePoll := commitProgram[nativeStart : nativeStart+nativeEnd]
	for _, required := range []string{
		"let consecutiveNativeReadbacks = 0",
		"for (let attempt = 0; attempt < 6; attempt += 1)",
		"setTimeout(resolve, 500)",
		"const readbackMatches = Boolean(committed &&",
		"!committed.invalid",
		"douyinExactTopicSetMatches(committed?.topics, expected.slice(0, index + 1))",
		"douyinCandidateMatches(committed?.bodyCandidates, expectedBody, normalizeDouyinBody)",
		"if (readbackMatches)",
		"consecutiveNativeReadbacks += 1",
		"if (consecutiveNativeReadbacks >= 2)",
		"nativeCommitted = true",
		"consecutiveNativeReadbacks = 0",
	} {
		if !strings.Contains(nativePoll, required) {
			t.Fatalf("native topic commit polling missing stability contract %q", required)
		}
	}
	readbackStart := strings.Index(nativePoll, "const readbackMatches = Boolean(committed &&")
	readbackEnd := strings.Index(nativePoll[readbackStart:], "if (readbackMatches)")
	if readbackStart < 0 || readbackEnd <= 0 {
		t.Fatalf("native topic readback condition boundary missing: start=%d end=%d", readbackStart, readbackEnd)
	}
	readbackCondition := nativePoll[readbackStart : readbackStart+readbackEnd]
	if strings.Index(readbackCondition, "douyinExactTopicSetMatches") < 0 || strings.Index(readbackCondition, "douyinCandidateMatches") < 0 {
		t.Fatalf("native topic readback must validate exact topics and body in one condition: %s", readbackCondition)
	}
	threshold := strings.Index(nativePoll, "if (consecutiveNativeReadbacks >= 2)")
	committed := strings.Index(nativePoll, "nativeCommitted = true")
	if threshold < 0 || committed <= threshold {
		t.Fatalf("nativeCommitted must only become true after two consecutive matches: threshold=%d committed=%d", threshold, committed)
	}
	resetBranch := strings.Index(nativePoll, "} else {")
	reset := strings.LastIndex(nativePoll, "consecutiveNativeReadbacks = 0")
	if resetBranch < 0 || reset <= resetBranch {
		t.Fatalf("native topic readback mismatch must reset consecutive count in the else branch: branch=%d reset=%d", resetBranch, reset)
	}
}

func TestGeneratedDouyinStableBodyReadbackUsesMatchingCandidatesOnly(t *testing.T) {
	prepare := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "冻结标题", Body: "正文", Tags: []string{"游戏内容"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.png", Kind: "image", Role: "image"}},
	}})
	commitStart := strings.Index(prepare, "const commitDouyinNativeTopics")
	commitEnd := strings.Index(prepare, "const reviewedPrepareErrorCode")
	if commitStart < 0 || commitEnd <= commitStart {
		t.Fatal("prepare native topic helper boundary missing")
	}
	commitProgram := prepare[commitStart:commitEnd]
	stableStart := strings.Index(commitProgram, "const stableDouyinBodyReadback")
	triggerStart := strings.Index(commitProgram, "let triggerSelector")
	if stableStart < 0 || triggerStart <= stableStart {
		t.Fatal("Douyin stable body readback helper boundary missing")
	}
	stableProgram := commitProgram[stableStart:triggerStart]
	for _, required := range []string{
		"const matchingValues = [...new Set(values.filter((value) => value === expectedNormalized))]",
		"if (!matchingValues.length) throw new Error(\"douyin_topic_body_readback_mismatch\")",
		"const signature = JSON.stringify(matchingValues)",
		"if (previousSignature && signature !== previousSignature)",
		"if (previousSignature) return true",
		"for (let attempt = 0; attempt < 3; attempt += 1)",
		"await new Promise((resolve) => setTimeout(resolve, 100))",
		"throw new Error(\"douyin_topic_body_readback_unstable\")",
	} {
		if !strings.Contains(stableProgram, required) {
			t.Fatalf("Douyin stable body readback missing matching-candidate stability contract %q", required)
		}
	}
	if strings.Contains(stableProgram, "values.some((value) => value !== expectedNormalized)") {
		t.Fatal("Douyin stable body readback must not require every candidate to match")
	}
	if strings.Contains(stableProgram, "JSON.stringify(values)") {
		t.Fatal("Douyin stable body readback signature must only include matching candidates")
	}
}

func TestGeneratedDouyinTopicReadbackDiagnosticsSetContextAndMapBodyError(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏"},
	}})
	commitStart := strings.Index(program, "const commitDouyinNativeTopics")
	commitEnd := strings.Index(program, "const reviewedPrepareErrorCode")
	if commitStart < 0 || commitEnd <= commitStart {
		t.Fatal("prepare native topic helper boundary missing")
	}
	commitProgram := program[commitStart:commitEnd]
	stableStart := strings.Index(commitProgram, "const stableDouyinBodyReadback")
	stableCall := strings.Index(commitProgram, "await stableDouyinBodyReadback(bodySelector, expectedBody)")
	branchSet, stageSet := -1, -1
	if stableCall >= 0 {
		branchSet = strings.LastIndex(commitProgram[:stableCall], `douyinTopicContext.branch = "trigger"`)
		stageSet = strings.LastIndex(commitProgram[:stableCall], `douyinTopicContext.stage = "precondition"`)
	}
	if stableStart < 0 || stableCall <= stableStart || branchSet <= stableStart || stageSet <= branchSet || stableCall <= stageSet {
		t.Fatalf("Douyin diagnostic context must be set before stable body readback: stable=%d branch=%d stage=%d call=%d", stableStart, branchSet, stageSet, stableCall)
	}
	finalReadback := strings.LastIndex(program, "let bodyAndTopics")
	if finalReadback < 0 {
		t.Fatal("final Douyin body and topic readback missing")
	}
	finalReadbackProgram := program[finalReadback:]
	topicsSave := strings.Index(finalReadbackProgram, "douyinTopicContext.topics = bodyAndTopics.topics.slice(0, 4)")
	bodyJudgment := strings.Index(finalReadbackProgram, "if (!douyinCandidateMatches(bodyAndTopics?.bodyCandidates")
	if topicsSave < 0 || bodyJudgment < 0 || topicsSave >= bodyJudgment {
		t.Fatalf("actual topics must be saved before final readback checks: save=%d judgment=%d", topicsSave, bodyJudgment)
	}
	readbackStage := strings.LastIndex(program[:finalReadback], `douyinTopicContext.stage = "readback"`)
	if readbackStage < 0 || readbackStage >= finalReadback {
		t.Fatalf("Douyin readback stage must be set before final body readback: stage=%d readback=%d", readbackStage, finalReadback)
	}

	reviewedStart := strings.Index(program, "const reviewedPrepareErrorCode")
	reviewedEnd := -1
	if reviewedStart >= 0 {
		reviewedEnd = strings.Index(program[reviewedStart:], "for (const target of publishTargets)")
	}
	if reviewedStart < 0 || reviewedEnd <= 0 {
		t.Fatal("reviewed prepare error-code helper boundary missing")
	}
	reviewedProgram := program[reviewedStart : reviewedStart+reviewedEnd]
	specific := strings.Index(reviewedProgram, `if (/douyin_body_readback_mismatch/i.test(message))`)
	generic := strings.Index(reviewedProgram, `if (/douyin_topic_/i.test(message))`)
	if specific < 0 || generic < 0 || specific >= generic {
		t.Fatalf("body readback mapping must precede generic topic mapping: specific=%d generic=%d", specific, generic)
	}
	bodyMapping := reviewedProgram[specific:generic]
	if !strings.Contains(bodyMapping, `return "EGO_DOUYIN_BODY_READBACK_MISMATCH"`) {
		t.Fatalf("body readback mapping missing safe error code: %s", bodyMapping)
	}
	for _, forbidden := range []string{"return message", "return error", "return String(error", "+ message"} {
		if strings.Contains(bodyMapping, forbidden) {
			t.Fatalf("body readback mapping leaks raw error data through %q: %s", forbidden, bodyMapping)
		}
	}
}

func TestDouyinFinalTopicReadbackClassifiesEachSafeFailureBranch(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
	}})
	start := strings.LastIndex(program, "let bodyAndTopics")
	if start < 0 {
		t.Fatal("final Douyin body and topic readback missing")
	}
	relativeEnd := strings.Index(program[start:], "if (target.publishAt)")
	if relativeEnd <= 0 {
		t.Fatal("final Douyin topic readback boundary missing")
	}
	readback := program[start : start+relativeEnd]
	branches := []string{
		`if (bodyAndTopics?.invalid || !Array.isArray(bodyAndTopics?.topics)) throw new Error("douyin_topic_readback_invalid")`,
		`if (expectedTopics.length > 0 && bodyAndTopics.topics.length === 0) throw new Error("douyin_topic_readback_empty")`,
		`if (bodyAndTopics.topics.length !== expectedTopics.length) throw new Error("douyin_topic_readback_count_mismatch")`,
		`if (!douyinExactTopicSetMatches(bodyAndTopics.topics, expectedTopics)) throw new Error("douyin_topic_readback_value_mismatch")`,
	}
	previous := -1
	for _, branch := range branches {
		position := strings.Index(readback, branch)
		if position < 0 {
			t.Fatalf("final Douyin topic readback missing branch %q", branch)
		}
		if position <= previous {
			t.Fatalf("final Douyin topic readback branches are out of order: %q at %d after %d", branch, position, previous)
		}
		previous = position
	}
	if strings.Contains(readback, `throw new Error("douyin_topic_readback_mismatch")`) {
		t.Fatal("final Douyin topic readback must not use the old single mismatch error")
	}
}

func TestDouyinPlan44AllowsAnExactEmptyTopicSet(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "无话题正文", Tags: []string{},
	}})
	start := strings.LastIndex(program, "let bodyAndTopics")
	if start < 0 {
		t.Fatal("final Douyin body and topic readback missing")
	}
	relativeEnd := strings.Index(program[start:], "if (target.publishAt)")
	if relativeEnd <= 0 {
		t.Fatal("final Douyin topic readback boundary missing")
	}
	readback := program[start : start+relativeEnd]
	required := []string{
		`if (expectedTopics.length > 0 && bodyAndTopics.topics.length === 0) throw new Error("douyin_topic_readback_empty")`,
		`if (bodyAndTopics.topics.length !== expectedTopics.length) throw new Error("douyin_topic_readback_count_mismatch")`,
		`if (!douyinExactTopicSetMatches(bodyAndTopics.topics, expectedTopics)) throw new Error("douyin_topic_readback_value_mismatch")`,
	}
	previous := -1
	for _, contract := range required {
		position := strings.Index(readback, contract)
		if position < 0 {
			t.Fatalf("plan 44 final topic validation missing %q", contract)
		}
		if position <= previous {
			t.Fatalf("plan 44 final topic validations out of order: %q at %d after %d", contract, position, previous)
		}
		previous = position
	}
	if strings.Contains(readback, `if (bodyAndTopics.topics.length === 0) throw new Error("douyin_topic_readback_empty")`) {
		t.Fatal("an empty expected topic set must not fail the final non-empty guard")
	}
}

func TestDouyinTopicReadbackErrorMappingsPrecedeGenericFallback(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
	}})
	start := strings.Index(program, "const reviewedPrepareErrorCode")
	if start < 0 {
		t.Fatal("reviewed prepare error-code helper missing")
	}
	relativeEnd := strings.Index(program[start:], "for (const target of publishTargets)")
	if relativeEnd <= 0 {
		t.Fatal("reviewed prepare error-code helper boundary missing")
	}
	mapping := program[start : start+relativeEnd]
	expected := []string{
		`if (/douyin_topic_preflight_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_READBACK_RUNTIME_FAILED"`,
		`if (/douyin_topic_preflight_readback_invalid/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_READBACK_INVALID"`,
		`if (/douyin_topic_preflight_topics_present/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_TOPICS_PRESENT"`,
		`if (/douyin_topic_preflight_body_mismatch/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_BODY_MISMATCH"`,
		`if (/douyin_body_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_RUNTIME_FAILED"`,
		`if (/douyin_topic_readback_invalid/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_INVALID"`,
		`if (/douyin_topic_readback_empty/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_EMPTY"`,
		`if (/douyin_topic_readback_count_mismatch/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_COUNT_MISMATCH"`,
		`if (/douyin_topic_readback_value_mismatch/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_VALUE_MISMATCH"`,
	}
	previous := -1
	for _, branch := range expected {
		position := strings.Index(mapping, branch)
		if position < 0 {
			t.Fatalf("reviewed prepare mapping missing %q", branch)
		}
		if position <= previous {
			t.Fatalf("specific topic readback mappings are out of order: %q at %d after %d", branch, position, previous)
		}
		previous = position
	}
	generic := `if (/douyin_topic_/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_MISMATCH"`
	genericPosition := strings.Index(mapping, generic)
	if genericPosition < 0 {
		t.Fatal("generic topic fallback was removed")
	}
	if genericPosition <= previous {
		t.Fatalf("specific topic readback mappings must precede generic fallback: specific=%d generic=%d", previous, genericPosition)
	}
	if !strings.Contains(mapping, `return "EGO_PUBLISH_PREPARE_REVIEW_REQUIRED"`) {
		t.Fatal("generic prepare review fallback was removed")
	}
}

func TestGeneratedDouyinNativeTopicCommitWrapsExternalOperations(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
	}})
	start := strings.Index(program, "const commitDouyinNativeTopics")
	end := strings.Index(program, "const reviewedPrepareErrorCode")
	if start < 0 || end <= start {
		t.Fatal("Douyin native topic commit helper boundary missing")
	}
	helper := program[start:end]
	for _, required := range []string{
		`readback = await douyinBodyAndTopicReadback(selector)`,
		`await typeText(character)`,
		`committed = await douyinBodyAndTopicReadback(bodySelector)`,
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("Douyin native topic external operation is not wrapped: %q", required)
		}
	}
	for _, required := range []string{
		`douyin_topic_readback_runtime_failed`,
		`douyin_topic_input_failed`,
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("Douyin native topic helper missing fixed runtime error %q", required)
		}
	}
	if strings.Count(helper, "await douyinBodyAndTopicReadback(") != 2 {
		t.Fatalf("unexpected number of wrapped native topic readbacks: %d", strings.Count(helper, "await douyinBodyAndTopicReadback("))
	}
	if strings.Count(helper, "await typeText(character)") != 1 || strings.Contains(helper, `await pressKey("SPACE")`) {
		t.Fatal("native topic input must remain character-wise and must not use Space fallback")
	}
	for _, forbidden := range []string{
		"const triggerSelector = await js(",
		"const nativeFocus = await js(",
		"const suggestionSelector = await js(",
		"const commitPrecondition = await js(",
	} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("Douyin native topic helper still contains an unwrapped direct call %q", forbidden)
		}
	}
	for _, operation := range []struct {
		assignment string
		runtimeErr string
	}{
		{assignment: "triggerSelector = await js(", runtimeErr: "douyin_topic_trigger_discovery_runtime_failed"},
		{assignment: "nativeFocus = await js(", runtimeErr: "douyin_topic_focus_readback_runtime_failed"},
	} {
		if strings.Count(helper, operation.assignment) != 1 {
			t.Fatalf("Douyin native topic operation %q must occur exactly once", operation.assignment)
		}
		position := strings.Index(helper, operation.assignment)
		if position < 0 {
			t.Fatalf("Douyin native topic helper missing operation %q", operation.assignment)
		}
		tryPosition := strings.LastIndex(helper[:position], "try {")
		catchPosition := strings.Index(helper[position:], "} catch {")
		errorPosition := strings.Index(helper[position:], operation.runtimeErr)
		if catchPosition >= 0 {
			catchPosition += position
		}
		if errorPosition >= 0 {
			errorPosition += position
		}
		if tryPosition < 0 || tryPosition >= position || catchPosition < position || errorPosition <= catchPosition {
			t.Fatalf("Douyin native topic operation %q is not enclosed by a fixed runtime-error guard", operation.assignment)
		}
	}
}

func TestDouyinNativeTopicOperationMappingsPrecedeGenericFallback(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
	}})
	start := strings.Index(program, "const reviewedPrepareErrorCode")
	end := strings.Index(program[start:], "for (const target of publishTargets)")
	if start < 0 || end <= 0 {
		t.Fatal("reviewed prepare error-code helper boundary missing")
	}
	mapping := program[start : start+end]
	expected := []string{
		`if (/douyin_topic_trigger_discovery_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_TRIGGER_DISCOVERY_RUNTIME_FAILED"`,
		`if (/douyin_topic_focus_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_FOCUS_READBACK_RUNTIME_FAILED"`,
		`if (/douyin_topic_suggestion_discovery_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_SUGGESTION_DISCOVERY_RUNTIME_FAILED"`,
		`if (/douyin_topic_precondition_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_PRECONDITION_RUNTIME_FAILED"`,
		`if (/douyin_topic_input_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_INPUT_FAILED"`,
		`if (/douyin_topic_space_commit_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_SPACE_COMMIT_FAILED"`,
		`if (/douyin_topic_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_RUNTIME_FAILED"`,
	}
	previous := -1
	for _, branch := range expected {
		position := strings.Index(mapping, branch)
		if position < 0 {
			t.Fatalf("reviewed prepare mapping missing %q", branch)
		}
		if position <= previous {
			t.Fatalf("native topic operation mappings are out of order: %q at %d after %d", branch, position, previous)
		}
		previous = position
	}
	generic := strings.Index(mapping, `if (/douyin_topic_/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_MISMATCH"`)
	if generic < 0 || generic <= previous {
		t.Fatalf("native topic operation mappings must precede generic fallback: specific=%d generic=%d", previous, generic)
	}
}

func TestGeneratedDouyinNativeTopicPickerUsesReviewedIncrementalSuggestions(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏日常分享"},
	}})
	start := strings.Index(program, "const commitDouyinNativeTopics")
	end := strings.Index(program, "const reviewedPrepareErrorCode")
	if start < 0 || end <= start {
		t.Fatal("Douyin native topic helper boundary missing")
	}
	helper := program[start:end]
	for _, required := range []string{
		"const typeDouyinInlinePickerTopic = async (tag) =>",
		"for (const character of [...tag])",
		"await typeText(character)",
		"setTimeout(resolve, 80)",
		"const douyinExactTopicSuggestionSelector = async (expectedValue) =>",
		"item.querySelector('.name')",
		"candidate===label||candidate.startsWith(label+' ')",
		"await typeDouyinInlinePickerTopic(tag)",
		"suggestionSelector = await douyinExactTopicSuggestionSelector(tag)",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("Douyin native topic picker missing reviewed incremental contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"await typeText(tag)",
		"douyinOfficialTopicSuggestionReady(partial)",
		"douyin_topic_suggestion_not_ready",
		`await pressKey("SPACE")`,
	} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("Douyin native topic picker must not contain %q", forbidden)
		}
	}
}

func TestDouyinPlan41WaitsForExactSuggestionOnlyAfterFullTag(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏日常分享"},
	}})
	start := strings.Index(program, "const commitDouyinNativeTopics")
	end := strings.Index(program, "const reviewedPrepareErrorCode")
	if start < 0 || end <= start {
		t.Fatal("Douyin native topic helper boundary missing")
	}
	helper := program[start:end]
	typeStart := strings.Index(helper, "const typeDouyinInlinePickerTopic")
	typeEnd := strings.Index(helper, "const douyinExactTopicSuggestionSelector")
	if typeStart < 0 || typeEnd <= typeStart {
		t.Fatal("Douyin incremental typing helper boundary missing")
	}
	typingHelper := helper[typeStart:typeEnd]
	for _, required := range []string{
		"for (const character of [...tag])",
		"await typeText(character)",
		"setTimeout(resolve, 80)",
		"await typeDouyinInlinePickerTopic(tag)",
		"for (let suggestionAttempt = 0; suggestionAttempt < 6; suggestionAttempt += 1)",
		"suggestionSelector = await douyinExactTopicSuggestionSelector(tag)",
		"if (!suggestionSelector) throw new Error(\"douyin_topic_suggestion_missing\")",
		"await click(suggestionSelector",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("plan 41 topic flow missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"douyinOfficialTopicSuggestionReady(partial)",
		"douyin_topic_suggestion_not_ready",
		"await pressKey(\"SPACE\")",
		"Enter",
	} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("plan 41 topic flow must not contain %q", forbidden)
		}
	}
	if strings.Contains(typingHelper, "suggestion") || strings.Contains(typingHelper, "partial") {
		t.Fatal("partial text must only settle; exact suggestion checks begin after the complete tag")
	}
	typeCall := strings.Index(helper, "await typeDouyinInlinePickerTopic(tag)")
	pollStart := strings.Index(helper, "for (let suggestionAttempt = 0; suggestionAttempt < 6; suggestionAttempt += 1)")
	if typeCall < 0 || pollStart <= typeCall {
		t.Fatalf("full tag typing must precede final exact polling: type=%d poll=%d", typeCall, pollStart)
	}
}

func TestDouyinPlan42FallsBackToOneExactSemanticSuggestion(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏日常分享"},
	}})
	start := strings.Index(program, "const douyinExactTopicSuggestionSelector")
	end := strings.Index(program[start:], "const stableDouyinBodyReadback")
	if start < 0 || end <= 0 {
		t.Fatal("Douyin exact suggestion helper boundary missing")
	}
	helper := program[start : start+end]
	for _, required := range []string{
		"const body=document.querySelector(",
		"const official=[...document.querySelectorAll('#creator-editor-topic-container .item')]",
		"if(official.length===1)",
		"if(official.length>1)return '__ambiguous__'",
		"const semanticSelectors=['[role=option]','[role=listbox] li','li','button','a']",
		"const semanticCandidates=[...new Set(semanticSelectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))]",
		"!body?.contains(node)",
		"matches(node.innerText||node.textContent||'')",
		"if(semanticCandidates.length!==1)return semanticCandidates.length>1?'__ambiguous__':''",
		"semanticCandidates[0].setAttribute('data-alpha-douyin-topic-suggestion','true')",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("plan 42 semantic suggestion fallback missing %q", required)
		}
	}
	official := strings.Index(helper, "const official=")
	semantic := strings.Index(helper, "const semanticSelectors=")
	if official < 0 || semantic <= official {
		t.Fatalf("official suggestion lookup must precede semantic fallback: official=%d semantic=%d", official, semantic)
	}
	for _, forbidden := range []string{
		"[class*=",
		"document.querySelectorAll('body *')",
		`"div"`,
		`await pressKey("SPACE")`,
		"Enter",
	} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("plan 42 suggestion fallback must not contain %q", forbidden)
		}
	}
}

func TestGeneratedDouyinGenericTopicSuggestionRequiresExactWholeCandidateText(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"游戏"},
	}})
	start := strings.Index(program, "const commitDouyinNativeTopics")
	end := strings.Index(program, "const reviewedPrepareErrorCode")
	if start < 0 || end <= start {
		t.Fatal("Douyin native topic helper boundary missing")
	}
	helper := program[start:end]
	suggestionStart := strings.Index(helper, "const douyinExactTopicSuggestionSelector")
	if suggestionStart < 0 {
		t.Fatal("Douyin official topic suggestion helper boundary missing")
	}
	suggestionEnd := strings.Index(helper[suggestionStart:], "const stableDouyinBodyReadback")
	if suggestionEnd <= 0 {
		t.Fatal("Douyin official topic suggestion helper end boundary missing")
	}
	suggestionHelper := helper[suggestionStart : suggestionStart+suggestionEnd]
	for _, required := range []string{
		"#creator-editor-topic-container .item",
		"item.querySelector('.name')",
		"const douyinExactTopicSuggestionSelector = async (expectedValue) =>",
		"const official=[...document.querySelectorAll('#creator-editor-topic-container .item')]",
		"candidate===label||candidate.startsWith(label+' ')",
		"official[0].setAttribute('data-alpha-douyin-topic-suggestion','true')",
	} {
		if !strings.Contains(suggestionHelper, required) {
			t.Fatalf("Douyin official topic suggestion missing boundary guard %q", required)
		}
	}
	if strings.Contains(suggestionHelper, "document.querySelectorAll('body *')") {
		t.Fatal("Douyin topic suggestion must not search arbitrary body descendants")
	}
}

func TestGeneratedDouyinTopicDiagnosticIsBoundedAndSanitized(t *testing.T) {
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
		Title:       "标题", Body: "正文", Tags: []string{"αgamer"},
	}})
	start := strings.Index(program, "const boundedDouyinTopicDiagnostic")
	end := strings.Index(program, "const commitDouyinNativeTopics")
	if start < 0 || end <= start {
		t.Fatal("bounded Douyin diagnostic helper missing")
	}
	helper := program[start:end]
	for _, required := range []string{
		`[data-mention],[data-topic-selected],[data-topic],[contenteditable=false],a`,
		"slice(0,12)", "slice(0,80)", "slice(-80)",
		"['data-mention','data-topic-selected','data-topic','contenteditable']",
		"new TextEncoder().encode(JSON.stringify(bounded)).length <= 4096",
		"branch: douyinTopicContext.branch", "stage: douyinTopicContext.stage",
		"topics: douyinTopicContext.topics.slice(0, 4)",
		"if (!bodySelector)", "if (!diagnostic)",
		"bodyTail: diagnostic.bodyTail", "nodes: diagnostic.nodes",
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("diagnostic helper missing %q", required)
		}
	}
	for _, forbidden := range []string{"outerHTML", "location.href", "document.body", "cookie", "className", "getAttribute('id')"} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("diagnostic helper exposes forbidden field %q", forbidden)
		}
	}
}

func TestDouyinTopicDiagnosticDOMNullFallbackKeepsContextTopics(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image",
	}})
	start := strings.Index(program, "const douyinTopicContext")
	end := strings.Index(program, "const commitDouyinNativeTopics")
	if start < 0 || end <= start {
		t.Fatal("bounded Douyin diagnostic helper boundary missing")
	}
	script := "const js = async () => null\n" + program[start:end] + `
douyinTopicContext.branch = "verified_space"
douyinTopicContext.stage = "readback"
douyinTopicContext.topics = ["one", "two", "three", "four", "five"]
const diagnostic = await boundedDouyinTopicDiagnostic("[data-missing]")
process.stdout.write(JSON.stringify(diagnostic))
`
	path := filepath.Join(t.TempDir(), "douyin-topic-diagnostic.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(node, path).CombinedOutput()
	if err != nil {
		t.Fatalf("DOM-null diagnostic fallback failed: %v\n%s", err, output)
	}
	var diagnostic PublishPrepareDiagnostic
	if err := json.Unmarshal(output, &diagnostic); err != nil {
		t.Fatalf("decode DOM-null diagnostic: %v\n%s", err, output)
	}
	if diagnostic.Branch != "verified_space" || diagnostic.Stage != "readback" ||
		strings.Join(diagnostic.Topics, ",") != "one,two,three,four" || diagnostic.BodyTail != "" || len(diagnostic.Nodes) != 0 {
		t.Fatalf("DOM-null diagnostic=%+v", diagnostic)
	}
}

func TestGeneratedDouyinGraphTextPrepareUsesFreshEntryFlow(t *testing.T) {
	target := publishRuntimeTarget{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/post/image?stale=1",
		Title:       "冻结图文标题", Body: "冻结图文正文",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-douyin.png", Kind: "image", Role: "image"}},
	}
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{target})
	if node, err := exec.LookPath("node"); err == nil {
		path := filepath.Join(t.TempDir(), "douyin-graph-text-prepare.mjs")
		if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
			t.Fatalf("generated Douyin graph-text JavaScript is invalid: %v\n%s", err, output)
		}
	}
	for _, required := range []string{
		"douyinGraphText",
		"https://creator.douyin.com/creator-micro/home",
		"发布图文",
		"const leaves=",
		"node.closest('button,a,",
		"let entryClickSucceeded = false",
		"if (entrySelector)",
		"https://creator.douyin.com/creator-micro/content/upload?default-tab=3",
		"/creator-micro/content/upload",
		"/creator-micro/content/post/image",
		"data-alpha-douyin-image-input",
		"input[type='file']",
		"douyin_required_image_route_missing",
		"EGO_DOUYIN_HOME_ROUTE_UNVERIFIED",
		"EGO_DOUYIN_ENTRY_OR_FALLBACK_FAILED",
		"EGO_DOUYIN_IMAGE_INPUT_MISSING",
		"EGO_DOUYIN_REQUIRED_IMAGE_ROUTE_MISSING",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Douyin graph-text prepare missing fresh-entry contract %q", required)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector") {
		t.Fatal("Douyin graph-text prepare crossed final submit boundary")
	}
	if strings.Contains(program, `if (!entrySelector) throw`) {
		t.Fatal("missing Douyin home entry must fall back to the reviewed official upload URL")
	}
}

func TestPublishPrepareProgramHasBoundedTimeoutAndIncludesXHSPrepareWithoutFinalClick(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "video", ComposerURL: "https://creator.douyin.com/creator-micro/content/publish",
		Title: "冻结标题", Body: "冻结正文",
	}, {
		PlatformCode: "xhs", ContentType: "graph_text", ComposerURL: "https://creator.xiaohongshu.com/publish/publish?from=menu&target=image",
		Title: "冻结小红书标题", Body: "冻结小红书正文",
		Media: []PublishMedia{{FilePath: "/tmp/frozen-xhs.png", Kind: "image", Role: "image"}},
	}}
	program := buildEgoPublishPrepareProgram(14, targets)
	for _, required := range []string{
		"const publishDeadlineAt = Date.now() + 25000",
		"const remainingPublishBudget = () => publishDeadlineAt - Date.now()",
		"EGO_PUBLISH_PREPARE_TIMEOUT",
		"timeout: 8",
		"attempt < 24 && remainingPublishBudget() > 1200",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("prepare program missing bounded-runtime guard %q", required)
		}
	}
	for _, required := range []string{"\"platformCode\":\"xhs\"", `target.platformCode === "xhs"`, "xhs-publish-btn", "xhs_final_button_missing_or_ambiguous"} {
		if !strings.Contains(program, required) {
			t.Fatalf("XHS prepare contract missing %q", required)
		}
	}
	if strings.Contains(program, "click(xhsFinalSelector") || strings.Contains(program, "click(finalSelector") {
		t.Fatal("XHS prepare must not click the final publish control")
	}
}

func TestPublishPrepareProgramFailsClosedForUserAndReclaimsDelegatedSpace(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text", ComposerURL: "https://creator.douyin.com/creator-micro/content/publish",
		Title: "冻结标题", Body: "冻结正文",
	}}
	program := buildEgoPublishPrepareProgram(14, targets)
	userGuard := `if (existingSpace.ownership === "user") {
  throw new Error("EGO_SPACE_USER_CONTROLLED")
}`
	if !strings.Contains(program, userGuard) {
		t.Fatalf("prepare program has no structured user-control stop: %s", program)
	}
	delegatedGuard := `else if (existingSpace.ownership === "agentDelegatedToUser") {
  await takeOverTaskSpace(14)
}`
	if !strings.Contains(program, delegatedGuard) {
		t.Fatalf("prepare program does not reclaim the explicitly delegated Space: %s", program)
	}
	if strings.Contains(program, "completeTaskSpace(") {
		t.Fatalf("publish prepare must leave completion to the dedicated final runtime call: %s", program)
	}
}

func TestXHSPrepareProgramHasFailClosedChallengeAndFinalButtonContract(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "xhs", ContentType: "graph_text", ComposerURL: "https://creator.xiaohongshu.com/publish/publish?from=menu&target=image",
		Title: "冻结标题", Body: "冻结正文", Tags: []string{"AI"},
		Media: []PublishMedia{{FilePath: "/tmp/frozen-xhs.png", Kind: "image", Role: "image"}},
	}}
	program := buildEgoPublishPrepareProgram(14, targets)
	for _, required := range []string{
		"PLATFORM_VERIFICATION_REQUIRED", "xhs-publish-btn", "xhs_final_button_missing_or_ambiguous",
		"xhs_topic_readback_mismatch", "awaiting_review",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("XHS prepare program missing fail-closed contract %q", required)
		}
	}
	if strings.Contains(program, "handOffTaskSpace") {
		t.Fatalf("XHS prepare must not hand off the Space: %s", program)
	}
	if strings.Contains(program, "completeTaskSpace(") {
		t.Fatalf("XHS prepare must report before the dedicated Space completion run: %s", program)
	}
	if strings.Contains(program, "click(xhsFinalSelector") || strings.Contains(program, "click(finalSelector") {
		t.Fatalf("XHS prepare must not click the final publish control: %s", program)
	}
}

func TestMetaPrepareProgramUsesPageAssetAndPairReadbackWithoutFinalClick(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "instagram", ContentType: "graph_text",
		ComposerURL: "https://business.facebook.com/latest/composer/?asset_id=1241410172381837",
		MetaPageID:  "1241410172381837", MetaPageName: "Alpha Page",
		MetaInstagramID: "17841416733781085", MetaInstagramName: "Alpha IG",
		Title: "Meta 标题", Body: "Meta 正文",
		Media: []PublishMedia{{FilePath: "/tmp/meta-image.png", Kind: "image", Role: "content"}},
	}}
	program := buildEgoPublishPrepareProgram(14, targets)
	for _, required := range []string{
		"business.facebook.com",
		"searchParams.get(\"asset_id\")",
		"metaPageID",
		"metaInstagramID",
		"Alpha Page",
		"Alpha IG",
		"meta_target_pair",
		"uploadFile(metaMediaInputSelector",
		"meta_caption_readback",
		"meta_final_button_missing_or_disabled",
		"awaiting_review",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Meta prepare program missing %q: %s", required, program)
		}
	}
	for _, forbidden := range []string{
		"submit approved publish",
		"click(metaFinalSelector",
		"click(metaFinalButtonSelector",
	} {
		if strings.Contains(program, forbidden) {
			t.Fatalf("Meta prepare crossed final publish boundary %q: %s", forbidden, program)
		}
	}
}

func TestMetaPrepareProgramIsValidJavaScriptModule(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	program := buildEgoPublishPrepareProgram(14, []publishRuntimeTarget{{
		PlatformCode: "instagram", ContentType: "graph_text",
		ComposerURL: "https://business.facebook.com/latest/composer/?asset_id=1241410172381837",
		MetaPageID:  "1241410172381837", MetaPageName: "Alpha Page",
		MetaInstagramID: "17841416733781085", MetaInstagramName: "Alpha IG",
		Body:  "Meta 正文",
		Media: []PublishMedia{{FilePath: "/tmp/meta-image.png", Kind: "image", Role: "content"}},
	}})
	path := filepath.Join(t.TempDir(), "meta-prepare.mjs")
	if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("generated Meta JavaScript is invalid: %v\n%s", err, output)
	}
}
