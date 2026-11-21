package platforms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/publishworker"
)

type PlaywrightWorker interface {
	Run(context.Context, publishworker.Input) (*publishworker.Output, error)
}

type playwrightPublisher struct {
	code   string
	worker PlaywrightWorker
}

// ExecutionPublisher returns the reviewed browser worker for creator platforms
// whose field-level flow is implemented. YouTube/TikTok submit is still gated
// upstream by a frozen single-target plan and submit-bound approval. Other
// platforms keep their registered native, API, or manual-handoff adapter.
func ExecutionPublisher(platformCode string) (Publisher, error) {
	code := normalizePlatformCode(platformCode)
	switch code {
	case "xhs", "douyin", "kuaishou", "wechat_video", "wechat_mp", "csdn", "youtube", "tiktok":
		return NewPlaywrightPublisher(code, publishworker.Runner{})
	default:
		return GetPublisher(code)
	}
}

func NewPlaywrightPublisher(platformCode string, worker PlaywrightWorker) (Publisher, error) {
	code := normalizePlatformCode(platformCode)
	supported := map[string]bool{
		"xhs": true, "douyin": true, "kuaishou": true, "wechat_video": true,
		"wechat_mp": true, "csdn": true, "youtube": true, "tiktok": true,
	}
	if !supported[code] || worker == nil {
		return nil, fmt.Errorf("PLAYWRIGHT_PUBLISHER_UNAVAILABLE")
	}
	return &playwrightPublisher{code: code, worker: worker}, nil
}

func (p *playwrightPublisher) Platform() string { return p.code }

func (p *playwrightPublisher) Strategy() PublishStrategy { return StrategyBrowser }

func playwrightSettings(platformCode, contentType, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &envelope) == nil {
		if _, snapshot := envelope["snapshot_version"]; snapshot {
			parsed, err := ParsePublishSettingsSnapshot(raw)
			if err != nil {
				return "", err
			}
			adapterVersion, _ := PublishSettingsAdapterVersion(platformCode)
			if parsed.PlatformCode != platformCode || parsed.AdapterVersion != adapterVersion {
				return "", fmt.Errorf("unsupported %s publish settings snapshot", platformCode)
			}
			return filterNormalizedSettingsForContentType(platformCode, contentType, string(parsed.Settings))
		}
	}
	return NormalizeSettingsForContentType(platformCode, contentType, raw)
}

func (p *playwrightPublisher) Validate(req PublishRequest) error {
	if strings.TrimSpace(req.CDPURL) == "" {
		return fmt.Errorf("CDP_CONNECT")
	}
	if strings.TrimSpace(req.Title) == "" && strings.TrimSpace(req.Caption) == "" && strings.TrimSpace(req.Body) == "" {
		return fmt.Errorf("content is required")
	}
	if _, ok := SettingsSchemaFor(p.code); ok {
		if _, err := playwrightSettings(p.code, req.ContentType, req.SettingsJSON); err != nil {
			return err
		}
	} else {
		var empty map[string]interface{}
		if strings.TrimSpace(req.SettingsJSON) != "" && (json.Unmarshal([]byte(req.SettingsJSON), &empty) != nil || len(empty) != 0) {
			return fmt.Errorf("platform has no reviewed settings fields")
		}
	}
	return nil
}

func (p *playwrightPublisher) Publish(ctx context.Context, req PublishRequest) (*PublishResult, error) {
	if req.DryRun {
		return &PublishResult{Status: "dry_run_ok", Step: "preflight", Message: "playwright worker dry-run validated"}, nil
	}
	if err := p.Validate(req); err != nil {
		return &PublishResult{Status: "permanent_failed", Step: "preflight", ErrorCode: "VALIDATION_FAILED", ErrorMessage: err.Error()}, err
	}
	settingsJSON, settingsErr := playwrightSettings(p.code, req.ContentType, req.SettingsJSON)
	if settingsErr != nil {
		return &PublishResult{Status: "permanent_failed", Step: "preflight", ErrorCode: "SETTINGS_INVALID"}, settingsErr
	}
	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return &PublishResult{Status: "permanent_failed", Step: "preflight", ErrorCode: "SETTINGS_INVALID"}, err
	}
	if schema, ok := SettingsSchemaFor(p.code); ok {
		for _, field := range schema.Fields {
			if len(field.ContentTypes) == 0 {
				continue
			}
			allowed := false
			for _, contentType := range field.ContentTypes {
				if contentType == req.ContentType {
					allowed = true
					break
				}
			}
			if !allowed {
				delete(settings, field.Key)
			}
		}
	}
	tags := make([]string, 0)
	if strings.TrimSpace(req.TagsJSON) != "" {
		if err := json.Unmarshal([]byte(req.TagsJSON), &tags); err != nil {
			return &PublishResult{Status: "permanent_failed", Step: "preflight", ErrorCode: "TAGS_INVALID"}, err
		}
	}
	media := make([]publishworker.Media, 0, len(req.Media)+1)
	for _, item := range req.Media {
		media = append(media, publishworker.Media{Path: item.Path, Kind: item.Kind, Role: item.Role, Position: item.Position, SHA256: strings.ToLower(item.SHA256)})
	}
	if len(media) == 0 && strings.TrimSpace(req.MediaPath) != "" {
		kind := "image"
		role := "content"
		if req.ContentType == "video" {
			kind, role = "video", "video"
		}
		media = append(media, publishworker.Media{Path: req.MediaPath, Kind: kind, Role: role, Position: 0})
	}
	action := "prepare"
	if req.AutoSubmit {
		action = "submit"
		if req.SubmitPreparedOnly {
			action = "submit_prepared"
		}
	}
	output, err := p.worker.Run(ctx, publishworker.Input{
		SchemaVersion: 1, Action: action, Platform: p.code, CDPURL: req.CDPURL,
		Content: publishworker.Content{Type: req.ContentType, Title: req.Title, Caption: req.Caption, Body: req.Body, Tags: tags},
		Media:   media, Settings: settings, PublishAt: req.PublishAt,
		AllowSubmit: req.AutoSubmit, TimeoutMS: 10 * 60 * 1000,
	})
	if output == nil {
		code := "PLAYWRIGHT_WORKER_FAILED"
		if stable := publishworker.ErrorCode(err); stable != "" {
			code = stable
		}
		return &PublishResult{Status: "retryable_failed", Step: "worker", ErrorCode: code}, err
	}
	evidence := make(map[string]interface{}, len(output.Evidence)+1)
	for key, value := range output.Evidence {
		evidence[key] = value
	}
	// The worker only returns origin and pathname here; retain it so a stopped
	// prepare run can be diagnosed without exposing query parameters or tokens.
	if output.Location != "" {
		evidence["workerLocation"] = output.Location
	}
	screenshot, _ := evidence["screenshotPath"].(string)
	result := &PublishResult{
		Status: output.Status, Step: output.Step, ErrorCode: output.ErrorCode,
		Screenshot: screenshot, Evidence: evidence,
	}
	if err != nil {
		result.ErrorMessage = result.ErrorCode
		return result, err
	}
	return result, nil
}
