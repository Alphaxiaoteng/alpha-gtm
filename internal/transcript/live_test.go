package transcript

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveExtract hits the network, so it only runs when explicitly asked for:
//
//	ALPHA_LIVE_TRANSCRIPT=1 go test ./internal/transcript -run TestLiveExtract -v
func TestLiveExtract(t *testing.T) {
	if os.Getenv("ALPHA_LIVE_TRANSCRIPT") != "1" {
		t.Skip("set ALPHA_LIVE_TRANSCRIPT=1 to run the live extraction check")
	}
	e := New()
	if !e.Available() {
		t.Skip("yt-dlp not installed")
	}
	res, err := e.Extract(context.Background(), Request{
		URL:     "https://www.youtube.com/watch?v=jNQXAC9IVRw",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	t.Logf("source=%s lang=%s title=%q duration=%.0f segments=%d elapsed=%dms",
		res.Source, res.Language, res.Title, res.Duration, len(res.Segments), res.ElapsedMS)
	if len(res.Segments) > 0 {
		t.Logf("first: %.1f-%.1fs %q", res.Segments[0].Start, res.Segments[0].End, res.Segments[0].Text)
	}
}
