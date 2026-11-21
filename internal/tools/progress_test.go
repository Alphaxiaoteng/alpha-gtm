package tools

import (
	"context"
	"testing"
)

// TestProgressSinkRoundTrips proves a caller can install a sink and the tool can
// find it again, which is what makes `tools call` observable for long archives.
func TestProgressSinkRoundTrips(t *testing.T) {
	var got []string
	sink := func(stage string, done, total int) {
		got = append(got, stage)
	}

	ctx := WithProgress(context.Background(), sink)
	found, ok := ProgressFrom(ctx)
	if !ok {
		t.Fatal("ProgressFrom() did not find the installed sink")
	}
	found("media", 3, 10)

	if len(got) != 1 || got[0] != "media" {
		t.Fatalf("sink calls = %#v, want one media report", got)
	}
}

// TestProgressFromWithoutSink keeps the old behaviour intact: no sink means tools
// fall back to a single result at the end, exactly as before.
func TestProgressFromWithoutSink(t *testing.T) {
	if _, ok := ProgressFrom(context.Background()); ok {
		t.Fatal("ProgressFrom() reported a sink that was never installed")
	}
	if _, ok := ProgressFrom(nil); ok {
		t.Fatal("ProgressFrom(nil) reported a sink")
	}
}

// TestWithProgressIgnoresNilSink makes sure a nil sink cannot silently disable an
// outer one, which would make a nested call lose its progress.
func TestWithProgressIgnoresNilSink(t *testing.T) {
	outer := func(string, int, int) {}
	ctx := WithProgress(context.Background(), outer)
	ctx = WithProgress(ctx, nil)

	found, ok := ProgressFrom(ctx)
	if !ok {
		t.Fatal("a nil sink cleared the outer sink")
	}
	found("posts", 1, 0)
}
