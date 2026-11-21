package transcript

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJSON3(t *testing.T) {
	raw := []byte(`{"events":[
		{"tStartMs":0,"dDurationMs":1500,"segs":[{"utf8":"你好，"},{"utf8":"世界"}]},
		{"tStartMs":1500,"dDurationMs":2000,"segs":[{"utf8":"第二行\n换行"}]},
		{"tStartMs":4000,"dDurationMs":100,"segs":[{"utf8":"   "}]},
		{"tStartMs":5000,"dDurationMs":500,"segs":[]}
	]}`)

	segments, err := parseJSON3(raw)
	if err != nil {
		t.Fatalf("parseJSON3: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2 (blank and empty events dropped): %+v", len(segments), segments)
	}
	if segments[0].Text != "你好，世界" {
		t.Errorf("first text = %q", segments[0].Text)
	}
	if segments[0].Start != 0 || segments[0].End != 1.5 {
		t.Errorf("first timing = %v-%v, want 0-1.5", segments[0].Start, segments[0].End)
	}
	if strings.Contains(segments[1].Text, "\n") {
		t.Errorf("newlines should be folded, got %q", segments[1].Text)
	}
}

func TestLanguageMatches(t *testing.T) {
	cases := []struct {
		have, want string
		ok         bool
	}{
		{"zh-Hans", "zh", true},
		{"zh", "zh", true},
		{"zh-Hant", "zh-Hans", false},
		{"en-US", "en", true},
		{"en", "zh", false},
	}
	for _, tc := range cases {
		if got := languageMatches(tc.have, tc.want); got != tc.ok {
			t.Errorf("languageMatches(%q,%q) = %v, want %v", tc.have, tc.want, got, tc.ok)
		}
	}
}

func TestPickSubtitlePrefersManualOverAutomatic(t *testing.T) {
	meta := metadata{
		Subtitles:         map[string][]subtitleVariant{"en": {{Ext: "json3", URL: "manual-en"}}},
		AutomaticCaptions: map[string][]subtitleVariant{"zh-Hans": {{Ext: "json3", URL: "auto-zh"}}},
	}

	language, variant, ok := pickSubtitle(meta, []string{"zh-Hans", "en"})
	if !ok {
		t.Fatal("expected a subtitle")
	}
	// Manual English beats automatic Chinese: hand-written tracks are accurate.
	if language != "en" || variant.URL != "manual-en" {
		t.Errorf("picked %q/%q, want en/manual-en", language, variant.URL)
	}
}

func TestPickSubtitleFallsBackToAutomatic(t *testing.T) {
	meta := metadata{AutomaticCaptions: map[string][]subtitleVariant{"zh-Hans": {{Ext: "json3", URL: "auto"}}}}
	language, _, ok := pickSubtitle(meta, nil)
	if !ok || language != "zh-Hans" {
		t.Errorf("got %q ok=%v, want zh-Hans", language, ok)
	}
}

func TestPickSubtitleRejectsUnusableFormat(t *testing.T) {
	meta := metadata{Subtitles: map[string][]subtitleVariant{"en": {{Ext: "ttml", URL: "x"}}}}
	if _, _, ok := pickSubtitle(meta, nil); ok {
		t.Error("ttml must not be picked: timing would be wrong")
	}
}

func TestExtractRejectsNonPublicURL(t *testing.T) {
	extractor := &Extractor{Binary: "yt-dlp"}
	for _, url := range []string{"", "file:///etc/passwd", "ftp://example.com/x", "javascript:alert(1)"} {
		if _, err := extractor.Extract(context.Background(), Request{URL: url}); err == nil {
			t.Errorf("url %q should be rejected", url)
		}
	}
}

func TestExtractReportsMissingTool(t *testing.T) {
	extractor := &Extractor{Binary: "definitely-not-installed-xyz"}
	if extractor.Available() {
		t.Fatal("should report unavailable")
	}
	_, err := extractor.Extract(context.Background(), Request{URL: "https://example.com/v"})
	if !errors.Is(err, ErrToolMissing) {
		t.Errorf("err = %v, want ErrToolMissing", err)
	}
}

func TestNeedsLogin(t *testing.T) {
	cases := []struct {
		output string
		want   bool
	}{
		{"ERROR: Sign in to confirm you're not a bot", true},
		{"ERROR: This video is private", true},
		{"ERROR: Video unavailable", false},
		{"ERROR: Login required", true},
	}
	for _, tc := range cases {
		if got := needsLogin([]byte(tc.output)); got != tc.want {
			t.Errorf("needsLogin(%q) = %v, want %v", tc.output, got, tc.want)
		}
	}
}

// TestExtractWithStubBinary exercises the full subprocess path using a shell
// stub, so the argument wiring and JSON decoding are both covered without
// touching the network.
func TestExtractWithStubBinary(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "fake-extractor")
	script := `#!/bin/sh
cat <<'JSON'
{"title":"示例","duration":42.5,"subtitles":{"zh-Hans":[{"ext":"json3","data":"{\"events\":[{\"tStartMs\":0,\"dDurationMs\":1000,\"segs\":[{\"utf8\":\"第一句\"}]}]}"}]}}
JSON
`
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	extractor := &Extractor{Binary: stub}
	result, err := extractor.Extract(context.Background(), Request{URL: "https://example.com/v"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Source != SourcePlatformSubtitle {
		t.Errorf("Source = %q, want platform_subtitle", result.Source)
	}
	if result.Title != "示例" || result.Duration != 42.5 {
		t.Errorf("metadata = %q/%v", result.Title, result.Duration)
	}
	if len(result.Segments) != 1 || result.Segments[0].Text != "第一句" {
		t.Errorf("segments = %+v", result.Segments)
	}
	if result.PlainText != "第一句" {
		t.Errorf("PlainText = %q", result.PlainText)
	}
}

func TestExtractWithoutCaptionsReportsUnavailable(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "fake-extractor")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho '{\"title\":\"无字幕\",\"duration\":10}'\n"), 0o700); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	result, err := (&Extractor{Binary: stub}).Extract(context.Background(), Request{URL: "https://example.com/v"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Source != SourceUnavailable {
		t.Errorf("Source = %q, want unavailable (no captions is a normal answer)", result.Source)
	}
}

func TestMetadataArgsNeverCarryCookies(t *testing.T) {
	for _, arg := range metadataArgs("https://example.com/v") {
		if strings.Contains(arg, "cookie") || strings.Contains(arg, "--profile") {
			t.Errorf("metadata args must never reference browser sessions, got %q", arg)
		}
	}
}

func TestIsPrivateHost(t *testing.T) {
	private := []string{
		"127.0.0.1", "localhost", "api.localhost", "10.0.0.5", "192.168.1.1",
		"172.16.0.1", "169.254.169.254", "0.0.0.0", "::1", "",
	}
	for _, host := range private {
		if !isPrivateHost(host) {
			t.Errorf("%q should be treated as private", host)
		}
	}
	public := []string{"example.com", "8.8.8.8", "cdn.example.net", "1.1.1.1"}
	for _, host := range public {
		if isPrivateHost(host) {
			t.Errorf("%q should be treated as public", host)
		}
	}
}

func TestFetchCaptionRejectsPrivateHost(t *testing.T) {
	// 元数据里如果出现指向本机的字幕地址，不能去请求它。
	if _, err := fetchCaption(context.Background(), "http://127.0.0.1:17421/api/accounts"); err == nil {
		t.Fatal("loopback caption host must be refused")
	}
	if _, err := fetchCaption(context.Background(), "http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("link-local caption host must be refused")
	}
}

func TestIsPublicURLTrimsWhitespace(t *testing.T) {
	if !isPublicURL("  https://example.com/v  ") {
		t.Error("leading/trailing space should not change the verdict")
	}
	if isPublicURL("javascript:alert(1)") {
		t.Error("non-http scheme must be refused")
	}
}

// 提取器本身会去请求这个地址，所以本机与内网地址必须在调用前就拒绝。
func TestExtractRejectsPrivateTargets(t *testing.T) {
	extractor := &Extractor{Binary: "yt-dlp"}
	for _, url := range []string{
		"http://127.0.0.1:17421/api/storage/usage",
		"http://localhost:17421/",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.5/video",
	} {
		if _, err := extractor.Extract(context.Background(), Request{URL: url}); err == nil {
			t.Errorf("%s should be refused before the extractor runs", url)
		}
	}
}

// 提取器的 stderr 常带完整源地址（含签名参数），不能原样进日志。
func TestRunCaptureErrorRedactsURLs(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "failing-extractor")
	script := "#!/bin/sh\necho 'ERROR: unable to download https://cdn.example.com/v.mp4?sig=SECRET123&exp=99' >&2\nexit 1\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	_, err := (&Extractor{Binary: stub}).Extract(context.Background(), Request{URL: "https://example.com/v"})
	if err == nil {
		t.Fatal("expected the stub to fail")
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Errorf("signed parameters must not reach the error: %v", err)
	}
	if !strings.Contains(err.Error(), "[url]") {
		t.Errorf("the URL should be replaced by a placeholder: %v", err)
	}
}
