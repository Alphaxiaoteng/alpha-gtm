package tools

import (
	"context"
	"errors"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/transcript"
)

// Transcript tools are read-only and take a public URL. They are registered
// here rather than behind a UI-only handler so the CLI and Tool Agent reach the
// same capability the interface uses.
func registerTranscriptTools(r *Registry) {
	r.Register(Def{
		Name: "media.transcript", Category: "media", SideEffect: "read",
		Description: "Extract timed captions from a public video URL; reports unavailable when a source needs a login",
		Params: []Param{
			{Name: "url", Type: "string", Required: true, Description: "Public http(s) media URL"},
			{Name: "languages", Type: "array", Description: "Preferred language tags, e.g. zh-Hans, en"},
			{Name: "timeout_seconds", Type: "int", Description: "Defaults to 90"},
		},
		Handler: handleMediaTranscript,
	})
	r.Register(Def{
		Name: "media.transcript.status", Category: "media", SideEffect: "read",
		Description: "Report whether the optional transcript extractor is installed",
		Handler: func(_ context.Context, _ *Runtime, _ map[string]interface{}) (*Result, error) {
			available := transcript.New().Available()
			return okResult("ok", map[string]interface{}{"available": available}), nil
		},
	})
}

func handleMediaTranscript(ctx context.Context, _ *Runtime, args map[string]interface{}) (*Result, error) {
	url := strings.TrimSpace(argString(args, "url", ""))
	if url == "" {
		return nil, errors.New("url is required")
	}
	var languages []string
	if raw, ok := args["languages"]; ok {
		switch value := raw.(type) {
		case []interface{}:
			for _, item := range value {
				if text, isText := item.(string); isText && strings.TrimSpace(text) != "" {
					languages = append(languages, strings.TrimSpace(text))
				}
			}
		case string:
			for _, part := range strings.Split(value, ",") {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					languages = append(languages, trimmed)
				}
			}
		}
	}
	timeout := time.Duration(argInt(args, "timeout_seconds", 90)) * time.Second

	result, err := transcript.New().Extract(ctx, transcript.Request{
		URL: url, Languages: languages, Timeout: timeout,
	})
	if err != nil {
		// A missing tool or a login wall is an expected outcome of an optional
		// capability, so it reports as data the caller can branch on rather
		// than as an opaque tool failure.
		switch {
		case errors.Is(err, transcript.ErrToolMissing):
			return okResult("unavailable", map[string]interface{}{
				"available": false, "reason": "extractor_not_installed",
			}), nil
		case errors.Is(err, transcript.ErrLoginRequired):
			return okResult("login_required", map[string]interface{}{
				"available": true, "reason": "source_requires_login",
				"hint": "该项目需要登录态；应用不会读取或保存 Cookie，请人工处理。",
			}), nil
		}
		return nil, err
	}
	return okResult(string(result.Source), map[string]interface{}{
		"source": result.Source, "title": result.Title, "duration": result.Duration,
		"language": result.Language, "segments": result.Segments,
		"plain_text": result.PlainText, "elapsed_ms": result.ElapsedMS,
	}), nil
}
