package trace

import (
	"context"
	"testing"
)

func TestEnsureCreatesIDOnce(t *testing.T) {
	ctx, id := Ensure(context.Background())
	if id == "" || FromContext(ctx) != id {
		t.Fatalf("id=%q context=%q", id, FromContext(ctx))
	}
	ctx2, id2 := Ensure(ctx)
	if id2 != id || FromContext(ctx2) != id {
		t.Fatalf("trace replaced: %q -> %q", id, id2)
	}
}

func TestWithIDPreservesExplicitID(t *testing.T) {
	ctx := WithID(context.Background(), ID("trace-explicit"))
	if got := FromContext(ctx); got != "trace-explicit" {
		t.Fatalf("got %q", got)
	}
}

func TestRecentReturnsIndependentAttributes(t *testing.T) {
	attrs := map[string]string{"status": "original"}
	Record("trace-copy", "copy.test", attrs)
	attrs["status"] = "caller-mutated"
	first := Recent(1)
	if first[0].Attrs["status"] != "original" {
		t.Fatalf("record retained caller map: %+v", first[0].Attrs)
	}
	first[0].Attrs["status"] = "reader-mutated"
	second := Recent(1)
	if second[0].Attrs["status"] != "original" {
		t.Fatalf("recent returned shared map: %+v", second[0].Attrs)
	}
}
