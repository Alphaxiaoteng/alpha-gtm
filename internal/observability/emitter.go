package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	"alpha-nexus-egolite/internal/trace"
)

var ErrInvalidEvent = errors.New("observability event requires safe component and name")

// Emitter is the small injection boundary used by tools and domain runners.
type Emitter interface {
	Emit(context.Context, Event) error
}

type Options struct {
	Service        string
	Level          slog.Leveler
	Clock          func() time.Time
	PID            int
	AppVersion     string
	SidecarVersion string
}

type JSONEmitter struct {
	handler        slog.Handler
	service        string
	clock          func() time.Time
	pid            int
	appVersion     string
	sidecarVersion string
}

func NewJSONEmitter(w io.Writer, opts Options) *JSONEmitter {
	if w == nil {
		w = io.Discard
	}
	service := safeToken(opts.Service)
	if service == "" {
		service = "alpha-nexus"
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	pid := opts.PID
	if pid <= 0 {
		pid = os.Getpid()
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: opts.Level,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey:
				attr.Key = "ts"
			case slog.MessageKey:
				return slog.Attr{}
			}
			return attr
		},
	})
	return &JSONEmitter{
		handler:        handler,
		service:        service,
		clock:          clock,
		pid:            pid,
		appVersion:     safeToken(opts.AppVersion),
		sidecarVersion: safeToken(opts.SidecarVersion),
	}
}

func (e *JSONEmitter) Emit(ctx context.Context, event Event) error {
	if ctx == nil {
		ctx = context.Background()
	}
	component := safeToken(event.Component)
	name := safeToken(event.Name)
	if component == "" || name == "" {
		return ErrInvalidEvent
	}
	if !e.handler.Enabled(ctx, event.Level) {
		return nil
	}

	corr := CorrelationFromContext(ctx)
	traceID := safeToken(event.TraceID)
	if traceID == "" {
		traceID = safeToken(string(trace.FromContext(ctx)))
	}
	requestID := firstToken(event.RequestID, corr.RequestID)
	taskID := firstToken(event.TaskID, corr.TaskID)
	accountID := firstPositive(event.AccountID, corr.AccountID)
	jobID := firstPositive(event.JobID, corr.JobID)
	recordID := firstPositive(event.PublishRecordID, corr.PublishRecordID)
	batchID := firstPositive(event.BatchID, corr.BatchID)

	attrs := []slog.Attr{
		slog.Int("schema_version", SchemaVersion),
		slog.String("service", e.service),
		slog.String("component", component),
		slog.String("event", name),
	}
	attrs = appendString(attrs, "trace_id", traceID)
	attrs = appendString(attrs, "request_id", requestID)
	attrs = appendString(attrs, "task_id", taskID)
	attrs = appendInt64(attrs, "account_id", accountID)
	attrs = appendInt64(attrs, "job_id", jobID)
	attrs = appendInt64(attrs, "publish_record_id", recordID)
	attrs = appendInt64(attrs, "batch_id", batchID)
	attrs = appendString(attrs, "platform", firstToken(event.Platform, corr.Platform))
	if event.Attempt > 0 {
		attrs = append(attrs, slog.Int("attempt", event.Attempt))
	}
	attrs = appendString(attrs, "step", safeToken(event.Step))
	attrs = appendString(attrs, "status", safeToken(event.Status))
	errorCode := StableErrorCode(event.ErrorCode)
	attrs = appendString(attrs, "error_code", errorCode)
	errorClass := safeToken(event.ErrorClass)
	if errorClass == "" {
		errorClass = ErrorClass(errorCode)
	}
	attrs = appendString(attrs, "error_class", errorClass)
	if errorCode != "" {
		attrs = append(attrs, slog.Bool("retryable", event.Retryable))
	}
	if event.DurationMS >= 0 {
		attrs = append(attrs, slog.Int64("duration_ms", event.DurationMS))
	}
	if event.HTTPStatus > 0 {
		attrs = append(attrs, slog.Int("http_status", event.HTTPStatus))
	}
	attrs = appendString(attrs, "app_version", firstToken(event.AppVersion, e.appVersion))
	attrs = appendString(attrs, "sidecar_version", firstToken(event.SidecarVersion, e.sidecarVersion))
	pid := event.PID
	if pid <= 0 {
		pid = e.pid
	}
	attrs = append(attrs, slog.Int("pid", pid))

	record := slog.NewRecord(e.clock().UTC(), event.Level, "", 0)
	record.AddAttrs(attrs...)
	return e.handler.Handle(ctx, record)
}

func firstToken(primary, fallback string) string {
	if token := safeToken(primary); token != "" {
		return token
	}
	return safeToken(fallback)
}

func firstPositive(primary, fallback int64) int64 {
	if primary > 0 {
		return primary
	}
	if fallback > 0 {
		return fallback
	}
	return 0
}

func appendString(attrs []slog.Attr, key, value string) []slog.Attr {
	if value != "" {
		return append(attrs, slog.String(key, value))
	}
	return attrs
}

func appendInt64(attrs []slog.Attr, key string, value int64) []slog.Attr {
	if value > 0 {
		return append(attrs, slog.Int64(key, value))
	}
	return attrs
}
