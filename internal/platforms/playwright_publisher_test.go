package platforms

import (
	"context"
	"fmt"
	"testing"

	"alpha-nexus-egolite/internal/publishworker"
)

type recordingPlaywrightWorker struct {
	input publishworker.Input
	calls int
}

type failingPlaywrightWorker struct{ err error }

type evidencePlaywrightWorker struct{}

func (w failingPlaywrightWorker) Run(_ context.Context, _ publishworker.Input) (*publishworker.Output, error) {
	return nil, w.err
}

func (evidencePlaywrightWorker) Run(_ context.Context, _ publishworker.Input) (*publishworker.Output, error) {
	return &publishworker.Output{
		SchemaVersion: 1, OK: true, Status: "manual_takeover", Step: "required_control_missing",
		Location: "https://creator.example.com/publish",
		Evidence: map[string]interface{}{"screenshotPath": "/tmp/alpha-nexus-publish-evidence/xhs.png"},
	}, nil
}

func (w *recordingPlaywrightWorker) Run(_ context.Context, input publishworker.Input) (*publishworker.Output, error) {
	w.calls++
	w.input = input
	return &publishworker.Output{SchemaVersion: 1, OK: true, Status: "awaiting_review", Step: "ready_to_submit"}, nil
}

func TestPlaywrightPublisherPreparesWithoutSubmitAndPreservesMediaOrder(t *testing.T) {
	worker := &recordingPlaywrightWorker{}
	publisher, err := NewPlaywrightPublisher("xhs", worker)
	if err != nil {
		t.Fatal(err)
	}
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "xhs", ContentType: "graph_text", Title: "title", Body: "body", TagsJSON: `["tag"]`,
		CDPURL: "http://127.0.0.1:15101/", SettingsJSON: `{"visibility":"public","comment_enabled":true,"declare_original":false}`,
		Media: []PublishMedia{
			{Path: "/tmp/second.png", Kind: "image", Role: "content", Position: 1, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Path: "/tmp/first.png", Kind: "image", Role: "content", Position: 0, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	})
	if err != nil || result.Status != "awaiting_review" || worker.input.Action != "prepare" || worker.input.AllowSubmit {
		t.Fatalf("result=%+v input=%+v err=%v", result, worker.input, err)
	}
	if len(worker.input.Media) != 2 || worker.input.Media[0].Position != 1 || worker.input.Content.Tags[0] != "tag" {
		t.Fatalf("worker input=%+v", worker.input)
	}
}

func TestPlaywrightPublisherUnwrapsPersistedSettingsSnapshotForWorker(t *testing.T) {
	snapshot, err := BuildPublishSettingsSnapshot(
		"xhs", 0, `{"visibility":"private","comment_enabled":false,"declare_original":true}`,
		2, XHSBrowserAdapterVersion,
	)
	if err != nil {
		t.Fatal(err)
	}
	worker := &recordingPlaywrightWorker{}
	publisher, err := NewPlaywrightPublisher("xhs", worker)
	if err != nil {
		t.Fatal(err)
	}
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "xhs", ContentType: "graph_text", Title: "title", Body: "body",
		CDPURL: "http://127.0.0.1:15101/", SettingsJSON: snapshot,
	})
	if err != nil || result.Status != "awaiting_review" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if worker.input.Settings["visibility"] != "private" || worker.input.Settings["comment_enabled"] != false || worker.input.Settings["declare_original"] != true {
		t.Fatalf("worker settings=%+v", worker.input.Settings)
	}
	if _, leakedEnvelope := worker.input.Settings["snapshot_version"]; leakedEnvelope {
		t.Fatalf("worker received settings envelope: %+v", worker.input.Settings)
	}
}

func TestPlaywrightPublisherDoesNotReintroduceAnotherContentTypesDefaults(t *testing.T) {
	worker := &recordingPlaywrightWorker{}
	publisher, err := NewPlaywrightPublisher("douyin", worker)
	if err != nil {
		t.Fatal(err)
	}
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "douyin", ContentType: "video", Title: "title", Body: "body",
		CDPURL:       "http://127.0.0.1:15101/",
		SettingsJSON: `{"visibility":"public","comment_enabled":true,"self_declaration":"none"}`,
	})
	if err != nil || result.Status != "awaiting_review" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, exists := worker.input.Settings["music"]; exists {
		t.Fatalf("video worker received graph-text-only music: %+v", worker.input.Settings)
	}
}

func TestPlaywrightPublisherReturnsWorkerScreenshotEvidence(t *testing.T) {
	publisher, err := NewPlaywrightPublisher("xhs", evidencePlaywrightWorker{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "xhs", ContentType: "graph_text", Title: "title", Body: "body",
		CDPURL: "http://127.0.0.1:15101/", SettingsJSON: `{}`,
	})
	if err != nil || result.Screenshot != "/tmp/alpha-nexus-publish-evidence/xhs.png" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Evidence["workerLocation"] != "https://creator.example.com/publish" {
		t.Fatalf("worker location was not retained: %+v", result.Evidence)
	}
}

func TestExecutionPublisherUsesPlaywrightForReviewedCreatorPlatforms(t *testing.T) {
	for _, code := range []string{"xhs", "douyin", "kuaishou", "wechat_video", "wechat_mp", "csdn", "youtube", "tiktok"} {
		t.Run(code, func(t *testing.T) {
			publisher, err := ExecutionPublisher(code)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := publisher.(*playwrightPublisher); !ok {
				t.Fatalf("publisher %T is not the reviewed playwright publisher", publisher)
			}
		})
	}
}

func TestExecutionPublisherPreservesRegisteredAdaptersForOtherPlatforms(t *testing.T) {
	for _, code := range []string{"pdd", "x"} {
		t.Run(code, func(t *testing.T) {
			registered, err := GetPublisher(code)
			if err != nil {
				t.Fatal(err)
			}
			publisher, err := ExecutionPublisher(code)
			if err != nil {
				t.Fatal(err)
			}
			if publisher != registered {
				t.Fatalf("execution publisher %T does not preserve registered adapter %T", publisher, registered)
			}
		})
	}
}

func TestControlledSubmitPlatformsReachWorkerOnce(t *testing.T) {
	tests := map[string]string{
		"xhs":          `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"ai","related_media":"","allow_duet":false,"allow_copy":false}`,
		"douyin":       `{}`,
		"kuaishou":     `{}`,
		"wechat_video": `{}`,
		"youtube":      `{"audience":"not_made_for_kids","visibility":"public","playlist":"","allow_comments":true,"altered_content":false}`,
		"tiktok":       `{"visibility":"public","allow_comment":true,"allow_duet":false,"allow_stitch":false,"ai_generated":true}`,
	}
	for code, settings := range tests {
		t.Run(code, func(t *testing.T) {
			worker := &recordingPlaywrightWorker{}
			publisher, err := NewPlaywrightPublisher(code, worker)
			if err != nil {
				t.Fatal(err)
			}
			result, err := publisher.Publish(context.Background(), PublishRequest{
				PlatformCode: code, ContentType: "video", Title: "title", Caption: "caption",
				CDPURL: "http://127.0.0.1:15108/", SettingsJSON: settings, AutoSubmit: true,
			})
			if err != nil || result.Status != "awaiting_review" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if worker.calls != 1 || worker.input.Action != "submit" || !worker.input.AllowSubmit {
				t.Fatalf("worker calls=%d input=%+v", worker.calls, worker.input)
			}
		})
	}
}

func TestPlaywrightPublisherArticlePrepareNeverConstructsSubmit(t *testing.T) {
	for _, code := range []string{"csdn", "wechat_mp"} {
		t.Run(code, func(t *testing.T) {
			worker := &recordingPlaywrightWorker{}
			publisher, err := NewPlaywrightPublisher(code, worker)
			if err != nil {
				t.Fatal(err)
			}
			result, err := publisher.Publish(context.Background(), PublishRequest{
				PlatformCode: code, ContentType: "article", Title: "title", Body: "body",
				CDPURL: "http://127.0.0.1:15106/", SettingsJSON: `{}`,
			})
			if err != nil || result.Status != "awaiting_review" || worker.calls != 1 || worker.input.Action != "prepare" || worker.input.AllowSubmit {
				t.Fatalf("result=%+v calls=%d input=%+v err=%v", result, worker.calls, worker.input, err)
			}
		})
	}
}

func TestPlaywrightPublisherWeChatMPArticleAutoSubmitUsesReviewedWorker(t *testing.T) {
	worker := &recordingPlaywrightWorker{}
	publisher, err := NewPlaywrightPublisher("wechat_mp", worker)
	if err != nil {
		t.Fatal(err)
	}
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "wechat_mp", ContentType: "article", Title: "title", Body: "body",
		CDPURL: "http://127.0.0.1:15106/", SettingsJSON: `{}`, AutoSubmit: true,
	})
	if err != nil || result == nil || result.Status != "awaiting_review" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if worker.calls != 1 || worker.input.Action != "submit" || !worker.input.AllowSubmit {
		t.Fatalf("WeChat MP submit did not reach reviewed worker: calls=%d input=%+v", worker.calls, worker.input)
	}
}

func TestPlaywrightPublisherCSDNArticleAutoSubmitUsesReviewedWorker(t *testing.T) {
	worker := &recordingPlaywrightWorker{}
	publisher, err := NewPlaywrightPublisher("csdn", worker)
	if err != nil {
		t.Fatal(err)
	}
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "csdn", ContentType: "article", Title: "title", Body: "body",
		CDPURL: "http://127.0.0.1:15106/", SettingsJSON: `{}`, AutoSubmit: true,
	})
	if err != nil || result == nil || result.Status != "awaiting_review" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if worker.calls != 1 || worker.input.Action != "submit" || !worker.input.AllowSubmit {
		t.Fatalf("CSDN submit did not reach reviewed worker: calls=%d input=%+v", worker.calls, worker.input)
	}
}

func TestPlaywrightPublisherCSDNPreparedSubmitDoesNotReprepare(t *testing.T) {
	worker := &recordingPlaywrightWorker{}
	publisher, err := NewPlaywrightPublisher("csdn", worker)
	if err != nil {
		t.Fatal(err)
	}
	_, err = publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "csdn", ContentType: "article", Title: "title", Body: "body",
		CDPURL: "http://127.0.0.1:15106/", SettingsJSON: `{}`,
		AutoSubmit: true, SubmitPreparedOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.input.Action != "submit_prepared" || !worker.input.AllowSubmit {
		t.Fatalf("worker input=%+v", worker.input)
	}
}

func TestPlaywrightPublisherNormalizesMissingTagsToEmptyArray(t *testing.T) {
	worker := &recordingPlaywrightWorker{}
	publisher, _ := NewPlaywrightPublisher("csdn", worker)
	_, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "csdn", ContentType: "article", Title: "title", Body: "body",
		CDPURL: "http://127.0.0.1:15106/", SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.input.Content.Tags == nil || len(worker.input.Content.Tags) != 0 {
		t.Fatalf("missing tags must serialize as an empty JSON array: %+v", worker.input.Content.Tags)
	}
}

func TestPlaywrightPublisherPreservesReviewedWorkerErrorCode(t *testing.T) {
	publisher, _ := NewPlaywrightPublisher("csdn", failingPlaywrightWorker{err: fmt.Errorf("WORKER_CANCELLED")})
	result, err := publisher.Publish(context.Background(), PublishRequest{
		PlatformCode: "csdn", ContentType: "article", Title: "title", Body: "body",
		CDPURL: "http://127.0.0.1:15106/", SettingsJSON: `{}`,
	})
	if err == nil || result == nil || result.ErrorCode != "WORKER_CANCELLED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
