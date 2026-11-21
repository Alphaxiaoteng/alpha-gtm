package trace

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// ID is a correlation id for a job/agent run (AgentPush observability).
type ID string

var fallbackCounter uint64

func New() ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(b[8:], atomic.AddUint64(&fallbackCounter, 1))
	}
	// time prefix for sortability
	ts := time.Now().UTC().Format("20060102T150405")
	return ID(ts + "-" + hex.EncodeToString(b[:]))
}

type ctxKey struct{}

// WithID attaches a correlation ID to ctx. Empty IDs are ignored.
func WithID(ctx context.Context, id ID) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the correlation ID attached to ctx, if any.
func FromContext(ctx context.Context) ID {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(ctxKey{}).(ID)
	return id
}

// Ensure returns a context with one correlation ID, preserving an existing ID.
func Ensure(ctx context.Context) (context.Context, ID) {
	if ctx == nil {
		ctx = context.Background()
	}
	if id := FromContext(ctx); id != "" {
		return ctx, id
	}
	id := New()
	return WithID(ctx, id), id
}

// Span is a lightweight in-memory event (also written to audit by callers).
type Span struct {
	TraceID   ID
	Name      string
	StartedAt time.Time
	Attrs     map[string]string
}

var (
	mu    sync.Mutex
	spans []Span
)

func Record(id ID, name string, attrs map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	spans = append(spans, Span{TraceID: id, Name: name, StartedAt: time.Now().UTC(), Attrs: cloneAttrs(attrs)})
	if len(spans) > 5000 {
		spans = spans[len(spans)-2500:]
	}
}

func Recent(limit int) []Span {
	mu.Lock()
	defer mu.Unlock()
	if limit <= 0 || limit > len(spans) {
		limit = len(spans)
	}
	out := make([]Span, limit)
	copy(out, spans[len(spans)-limit:])
	for i := range out {
		out[i].Attrs = cloneAttrs(out[i].Attrs)
	}
	return out
}

func cloneAttrs(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for key, value := range attrs {
		out[key] = value
	}
	return out
}
