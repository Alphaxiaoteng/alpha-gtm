//go:build legacy_chromium

package browser

import "testing"

func TestNormalizeEngine(t *testing.T) {
	cases := map[string]string{
		"":                     EngineChromium,
		"fingerprint-chromium": EngineFingerprintChromium,
		"ego-lite":             EngineEgoLite,
		"ego":                  EngineEgoLite,
		"egolite":              EngineEgoLite,
		"ego-browser":          EngineEgoLite,
		"browseros":            EngineBrowserOS,
		"chromium":             EngineChromium,
	}
	for in, want := range cases {
		if got := NormalizeEngine(in); got != want {
			t.Fatalf("%q → %q want %q", in, got, want)
		}
	}
}

func TestChromiumIsRecommendedLocalBrowser(t *testing.T) {
	candidates := DetectLocalBrowsers()
	primary := ""
	for _, candidate := range candidates {
		if candidate.ID == EngineEgoLite {
			t.Fatal("ego-lite must not be a user-facing local browser candidate")
		}
		if candidate.Primary {
			if primary != "" {
				t.Fatalf("multiple recommended browsers: %q and %q", primary, candidate.ID)
			}
			primary = candidate.ID
		}
	}
	if primary != "chromium" {
		t.Fatalf("recommended browser = %q, want chromium", primary)
	}
}

func TestLaunchArgsFingerprintOnly(t *testing.T) {
	cfg := LaunchConfig{
		Engine:          EngineFingerprintChromium,
		UserDataDir:     "/tmp/p",
		FingerprintSeed: 42,
		Platform:        "macos",
	}
	args := cfg.args(15101)
	joined := stringsJoin(args)
	if !contains(joined, "--fingerprint=42") {
		t.Fatalf("missing fingerprint flag: %v", args)
	}
	if !contains(joined, "127.0.0.1") {
		t.Fatal("CDP must bind 127.0.0.1")
	}
	if !contains(joined, "--start-maximized") {
		t.Fatalf("managed browser must start maximized: %v", args)
	}
}

func TestLaunchArgsPreferSecondaryDisplayVisibleBounds(t *testing.T) {
	bounds := DisplayBounds{X: 2560, Y: 24, Width: 1920, Height: 1056}
	cfg := LaunchConfig{
		Engine:       EngineFingerprintChromium,
		UserDataDir:  "/tmp/p",
		WindowBounds: &bounds,
	}
	joined := stringsJoin(cfg.args(15101))
	if !contains(joined, "--window-position=2560,24") || !contains(joined, "--window-size=1920,1056") {
		t.Fatalf("secondary display bounds missing: %s", joined)
	}
}

func TestLaunchArgsBackgroundAvoidsThrottleWithoutChangingDefault(t *testing.T) {
	background := LaunchConfig{Engine: EngineChromium, UserDataDir: "/tmp/p", Background: true}
	joined := stringsJoin(background.args(15101))
	for _, flag := range []string{
		"--disable-background-timer-throttling",
		"--disable-renderer-backgrounding",
		"--disable-backgrounding-occluded-windows",
	} {
		if !contains(joined, flag) {
			t.Fatalf("background launch missing %s: %s", flag, joined)
		}
	}
	defaultArgs := stringsJoin((LaunchConfig{Engine: EngineChromium, UserDataDir: "/tmp/p"}).args(15101))
	if contains(defaultArgs, "--disable-background-timer-throttling") {
		t.Fatalf("interactive launch unexpectedly disables throttle: %s", defaultArgs)
	}
}

func TestPreferredSecondaryDisplayFallsBackWithoutUsableExtendedDisplay(t *testing.T) {
	if _, ok := preferredSecondaryDisplay([]DisplayBounds{{X: 0, Y: 0, Width: 2560, Height: 1440}}); ok {
		t.Fatal("single display must use Chromium's safe default placement")
	}
	if _, ok := preferredSecondaryDisplay([]DisplayBounds{
		{X: 0, Y: 0, Width: 2560, Height: 1440},
		{X: 0, Y: 0, Width: 1920, Height: 1080},
	}); ok {
		t.Fatal("mirrored display must not be treated as an extended secondary")
	}
	want := DisplayBounds{X: -1920, Y: 120, Width: 1920, Height: 1080}
	got, ok := preferredSecondaryDisplay([]DisplayBounds{
		{X: 0, Y: 0, Width: 2560, Height: 1440},
		want,
	})
	if !ok || got != want {
		t.Fatalf("secondary=%+v ok=%v", got, ok)
	}
}

func TestLaunchArgsEgoLiteNoFingerprint(t *testing.T) {
	cfg := LaunchConfig{
		Engine:          EngineEgoLite,
		UserDataDir:     "/tmp/p",
		FingerprintSeed: 99,
	}
	args := cfg.args(15102)
	joined := stringsJoin(args)
	if contains(joined, "--fingerprint=") {
		t.Fatalf("ego-lite must not pass fingerprint flags: %v", args)
	}
	if !contains(joined, "--remote-debugging-address=127.0.0.1") {
		t.Fatal("need localhost CDP")
	}
	if !contains(joined, "--remote-allow-origins=*") {
		t.Fatal("need local CDP WebSocket origin allowance")
	}
}

func stringsJoin(a []string) string {
	s := ""
	for _, x := range a {
		s += x + "\n"
	}
	return s
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
