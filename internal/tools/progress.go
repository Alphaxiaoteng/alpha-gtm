package tools

import "context"

// ProgressFunc reports how far a long-running tool has come. total is 0 while the
// upstream has not reported a count; callers must not fake it.
type ProgressFunc func(stage string, done, total int)

type progressKey struct{}

// WithProgress installs a progress sink for tools that can report progress. A
// caller that has nowhere to show progress simply does not install one, so tools
// keep working unchanged; a nil sink is ignored rather than clearing an outer one.
func WithProgress(ctx context.Context, sink ProgressFunc) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, sink)
}

// ProgressFrom returns the installed sink, if the caller provided one.
func ProgressFrom(ctx context.Context) (ProgressFunc, bool) {
	if ctx == nil {
		return nil, false
	}
	sink, ok := ctx.Value(progressKey{}).(ProgressFunc)
	return sink, ok && sink != nil
}
