package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashAndPreflight(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello agentpush"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, size, err := HashFile(p)
	if err != nil || size == 0 || sum == "" {
		t.Fatalf("hash: %v size=%d sum=%s", err, size, sum)
	}
	r, err := Preflight(p, "instagram", "graph_text")
	if err != nil || !r.OK {
		t.Fatalf("preflight: %+v %v", r, err)
	}
}

func TestValidateLocalPathAllowsMediaAndRejectsProtectedLocations(t *testing.T) {
	dir := t.TempDir()
	allowed := filepath.Join(dir, "image.png")
	if err := os.WriteFile(allowed, []byte("png-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := ValidateLocalPath(allowed)
	wantResolved, resolveErr := filepath.EvalSymlinks(allowed)
	if err != nil || resolveErr != nil || resolved != wantResolved {
		t.Fatalf("allowed path: resolved=%q err=%v", resolved, err)
	}
	for _, path := range []string{
		filepath.Join(dir, ".turbopush", "session.jpg"),
		filepath.Join(dir, "1key", "secret.png"),
		filepath.Join(dir, ".env", "secret.jpg"),
		filepath.Join(dir, ".env.production", "secret.jpg"),
	} {
		if _, err := ValidateLocalPath(path); err == nil {
			t.Fatalf("protected path accepted: %s", path)
		}
	}
}

func TestProbeDecodableVideoAcceptsRealFixture(t *testing.T) {
	if localFFprobePath() == "" {
		t.Skip("ffprobe is not available")
	}
	fixture := filepath.Join("..", "..", "e2e", "fixtures", "local-video.mp4")
	meta, err := ProbeDecodableVideo(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Width <= 0 || meta.Height <= 0 || meta.DurationMS <= 0 {
		t.Fatalf("invalid probed video metadata: %+v", meta)
	}
	if !strings.HasPrefix(meta.MimeGuess, "video/") || meta.SHA256 == "" || meta.SizeBytes <= 0 || meta.ProbeJSON == "" {
		t.Fatalf("incomplete probed video metadata: %+v", meta)
	}
}

func TestExtractFirstFrameCreatesPrivateImageCover(t *testing.T) {
	ffmpegPath := localFFmpegPath()
	if ffmpegPath == "" {
		t.Skip("ffmpeg is not available")
	}
	dir := t.TempDir()
	rawFrame := filepath.Join(dir, "frame.rgb")
	if err := os.WriteFile(rawFrame, make([]byte, 32*32*3), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.mp4")
	commandOutput, err := exec.Command(
		ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "rawvideo", "-pixel_format", "rgb24", "-video_size", "32x32",
		"-framerate", "1", "-i", rawFrame, "-frames:v", "1",
		"-pix_fmt", "yuv420p", "-y", source,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("create video fixture: %v: %s", err, strings.TrimSpace(string(commandOutput)))
	}
	output := filepath.Join(dir, "cover.jpg")
	meta, err := ExtractFirstFrame(source, output)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || meta.Path != output || meta.SizeBytes <= 0 || meta.SHA256 == "" || meta.MimeGuess != "image/jpeg" {
		t.Fatalf("meta=%+v mode=%o", meta, info.Mode().Perm())
	}
}

func TestProbeDecodableVideoRejectsTextDisguisedAsMP4WithoutLeakingPath(t *testing.T) {
	if localFFprobePath() == "" {
		t.Skip("ffprobe is not available")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "private-video-marker.mp4")
	if err := os.WriteFile(fake, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeDecodableVideo(fake); err == nil {
		t.Fatal("text file with .mp4 extension was accepted")
	} else if strings.Contains(err.Error(), "private-video-marker") || strings.Contains(err.Error(), dir) {
		t.Fatalf("public video probe error leaked a local path: %v", err)
	}
}

func TestProbeDecodableVideoRequiresFFprobe(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(fake, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ALPHA_NEXUS_DISABLE_APP_FFPROBE_FALLBACK", "1")
	if _, err := ProbeDecodableVideo(fake); err == nil || err.Error() != "video probe unavailable" {
		t.Fatalf("error=%v, want video probe unavailable", err)
	}
}
