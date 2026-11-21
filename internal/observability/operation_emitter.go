package observability

import (
	"context"
	"log/slog"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/trace"
)

// OperationEmitter persists only allowlisted diagnostic fields for the local
// activity timeline. It never receives tool args, URLs, browser payloads, or
// credentials.
type OperationEmitter struct{ database *db.DB }

func NewOperationEmitter(database *db.DB) *OperationEmitter {
	return &OperationEmitter{database: database}
}

func (e *OperationEmitter) Emit(ctx context.Context, event Event) error {
	if e == nil || e.database == nil {
		return nil
	}
	corr := CorrelationFromContext(ctx)
	traceID := firstToken(event.TraceID, string(trace.FromContext(ctx)))
	if traceID == "" {
		return ErrInvalidEvent
	}
	level := "info"
	switch {
	case event.Level <= slog.LevelDebug:
		level = "debug"
	case event.Level == slog.LevelWarn:
		level = "warn"
	case event.Level >= slog.LevelError:
		level = "error"
	}
	_, err := e.database.CreateOperationEvent(db.OperationEventInput{
		TraceID: traceID, RequestID: firstToken(event.RequestID, corr.RequestID), TaskID: firstToken(event.TaskID, corr.TaskID),
		AccountID: firstPositive(event.AccountID, corr.AccountID), PlatformCode: firstToken(event.Platform, corr.Platform),
		Component: safeToken(event.Component), EventType: safeToken(event.Name), Step: safeToken(event.Step), Status: safeToken(event.Status),
		Level: level, ErrorCode: StableErrorCode(event.ErrorCode), DurationMS: event.DurationMS,
	})
	return err
}

// FanoutEmitter keeps file diagnostics and durable operation visibility in
// lockstep. A failed observer must never alter the underlying tool result.
type FanoutEmitter struct{ emitters []Emitter }

func NewFanoutEmitter(emitters ...Emitter) *FanoutEmitter { return &FanoutEmitter{emitters: emitters} }

func (e *FanoutEmitter) Emit(ctx context.Context, event Event) error {
	var firstErr error
	for _, emitter := range e.emitters {
		if emitter == nil {
			continue
		}
		if err := emitter.Emit(ctx, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
