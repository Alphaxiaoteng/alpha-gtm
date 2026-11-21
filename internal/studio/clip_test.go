package studio

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestClipArgsAreBoundedAndNonShell(t *testing.T) {
	got := clipArgs("/tmp/source video.mp4", "/tmp/output.mp4", 1250, 61250)
	want := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-ss", "1.250", "-i", "/tmp/source video.mp4", "-t", "60.000", "-c", "copy", "-movflags", "+faststart", "/tmp/output.mp4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%q want=%q", got, want)
	}
}

func TestOutputPathStaysUnderDataDirectory(t *testing.T) {
	dir := t.TempDir()
	path, err := OutputPath(dir, 8)
	if err != nil || path != filepath.Join(dir, "edits", "clip-8.mp4") {
		t.Fatalf("path=%q err=%v", path, err)
	}
}

func TestHasMP4Muxer(t *testing.T) {
	if !hasMP4Muxer("Formats:\n DE mov,mp4,m4a,3gp,3g2,mj2 QuickTime / MOV") {
		t.Fatal("expected MP4 muxer")
	}
	if hasMP4Muxer("Formats:\n E image2 image2 sequence\n D matroska,webm Matroska / WebM") {
		t.Fatal("unexpected MP4 muxer")
	}
}
