package tools

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCreatorProgressWriterDedupesPhaseAndDone(t *testing.T) {
	buf := new(bytes.Buffer)
	writer := NewCreatorProgressWriter(buf)
	writer.OnProgress("posts", 1, 0)
	writer.OnProgress("posts", 1, 0) // duplicate progress after the same page
	writer.OnProgress("posts", 2, 0)
	writer.OnProgress("media", 1, 5)
	writer.OnProgress("media", 1, 5) // duplicate worker report
	writer.OnProgress("retry", 1, 0)
	writer.OnProgress("retry", 1, 0)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("progress lines = %d (%q), want 4", len(lines), buf.String())
	}
	var got []CreatorProgressLine
	for _, line := range lines {
		var record CreatorProgressLine
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		got = append(got, record)
	}
	want := []CreatorProgressLine{
		{Type: "progress", Phase: "posts", Done: 1, Total: 0},
		{Type: "progress", Phase: "posts", Done: 2, Total: 0},
		{Type: "progress", Phase: "media", Done: 1, Total: 5},
		{Type: "progress", Phase: "retry", Done: 1, Total: 0},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCreatorProgressWriterIgnoresNil(t *testing.T) {
	var writer *CreatorProgressWriter
	writer.OnProgress("posts", 1, 0) // must not panic
}
