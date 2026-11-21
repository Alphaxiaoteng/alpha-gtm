package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Engine identifies which Chromium binary + flag profile to launch.
// Default is chromium; ego-lite, browseros, and fingerprint-chromium are supported backends.
const (
	EngineFingerprintChromium = "fingerprint-chromium"
	EngineEgoLite             = "ego-lite"  // https://github.com/citrolabs/ego-lite
	EngineBrowserOS           = "browseros" // https://github.com/browseros-ai/BrowserOS
	EngineChromium            = "chromium"  // generic Chromium-compatible
)

// NormalizeEngine maps aliases → canonical engine id.
func NormalizeEngine(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "default", "chromium", "chrome", "generic":
		return EngineChromium
	case "ego-lite", "ego", "egolite", "ego-browser", "agentic":
		return EngineEgoLite
	case "browseros", "browser-os", "cloak", "cloak-browser":
		return EngineBrowserOS
	case "fp", "fingerprint", EngineFingerprintChromium:
		return EngineFingerprintChromium
	default:
		return s
	}
}

// EngineSupportsFingerprintFlags is true only for ungoogled fingerprint-chromium forks.
func EngineSupportsFingerprintFlags(engine string) bool {
	return NormalizeEngine(engine) == EngineFingerprintChromium
}

// ResolveBinary picks executable for engine using LaunchConfig / Facade paths.
func ResolveBinary(engine, binPath, egoLitePath, browserOSPath, fingerprintPath string) (string, error) {
	engine = NormalizeEngine(engine)
	candidates := []string{}
	switch engine {
	case EngineEgoLite:
		if egoLitePath != "" {
			candidates = append(candidates, egoLitePath)
		}
		if binPath != "" && (strings.Contains(strings.ToLower(binPath), "ego") || strings.HasSuffix(binPath, "ego lite")) {
			candidates = append(candidates, binPath)
		}
		candidates = append(candidates, defaultEgoLitePaths()...)
	case EngineBrowserOS:
		if browserOSPath != "" {
			candidates = append(candidates, browserOSPath)
		}
		if binPath != "" && strings.Contains(strings.ToLower(binPath), "browseros") {
			candidates = append(candidates, binPath)
		}
		candidates = append(candidates, defaultBrowserOSPaths()...)
	case EngineChromium:
		if binPath != "" {
			candidates = append(candidates, binPath)
		}
		candidates = append(candidates, defaultChromiumPaths()...)
	default: // fingerprint-chromium
		if fingerprintPath != "" {
			candidates = append(candidates, fingerprintPath)
		}
		if binPath != "" {
			candidates = append(candidates, binPath)
		}
		candidates = append(candidates, defaultFingerprintPaths()...)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
		// macOS .app bundle → Contents/MacOS/*
		if st, err := os.Stat(c); err == nil && st.IsDir() && strings.HasSuffix(c, ".app") {
			inner := filepath.Join(c, "Contents", "MacOS")
			entries, _ := os.ReadDir(inner)
			for _, e := range entries {
				if !e.IsDir() {
					return filepath.Join(inner, e.Name()), nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s binary not found for engine %q (set browser_path / ego_lite_path / browseros_path)", engine, engine)
}

func defaultFingerprintPaths() []string {
	// Do NOT fall back to stock Chrome/Chromium — that breaks isolation tests
	// and weakens account fingerprint guarantees. Explicit --browser only.
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "Applications", "fingerprint-chromium"),
		filepath.Join(home, "bin", "fingerprint-chromium"),
		"/usr/local/bin/fingerprint-chromium",
		"/opt/fingerprint-chromium/fingerprint-chromium",
		"fingerprint-chromium",
	}
}

func defaultEgoLitePaths() []string {
	home, _ := os.UserHomeDir()
	list := []string{
		"/Applications/ego lite.app/Contents/MacOS/ego lite",
		"/Applications/ego lite.app",
		"/Applications/ego-lite.app/Contents/MacOS/ego-lite",
		"/Applications/ego-lite.app",
		filepath.Join(home, "Applications", "ego lite.app", "Contents", "MacOS", "ego lite"),
		filepath.Join(home, "Applications", "ego lite.app"),
		filepath.Join(home, "Applications", "ego-lite.app", "Contents", "MacOS", "ego-lite"),
		filepath.Join(home, "Applications", "ego-lite.app"),
		"/usr/local/bin/ego-lite",
		"ego-lite",
	}
	if runtime.GOOS == "linux" {
		list = append(list,
			"/opt/ego-lite/ego-lite",
			"/usr/bin/ego-lite",
		)
	}
	return list
}

func defaultBrowserOSPaths() []string {
	home, _ := os.UserHomeDir()
	list := []string{
		"/Applications/BrowserOS.app/Contents/MacOS/BrowserOS",
		"/Applications/BrowserOS.app",
		filepath.Join(home, "Applications", "BrowserOS.app", "Contents", "MacOS", "BrowserOS"),
		filepath.Join(home, "Applications", "BrowserOS.app"),
		"/usr/local/bin/browseros",
		"/opt/BrowserOS/browseros",
		"/opt/browseros/BrowserOS",
		"browseros",
	}
	if runtime.GOOS == "linux" {
		list = append(list,
			"/opt/browseros/browseros",
			"/usr/bin/browseros",
		)
	}
	return list
}

// DetectEgoLite returns the first existing ego-lite binary, or "".
func DetectEgoLite() string {
	p, err := ResolveBinary(EngineEgoLite, "", "", "", "")
	if err != nil {
		return ""
	}
	return p
}

// DetectBrowserOS returns the first existing BrowserOS binary, or "".
func DetectBrowserOS() string {
	p, err := ResolveBinary(EngineBrowserOS, "", "", "", "")
	if err != nil {
		return ""
	}
	return p
}

// BrowserCandidate is a locally detected browser binary for UI quick-switch.
type BrowserCandidate struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Engine      string `json:"engine"`
	Path        string `json:"path"`
	Found       bool   `json:"found"`
	Primary     bool   `json:"primary,omitempty"` // recommended default
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"` // logical icon key for UI SVG
}

// DetectLocalBrowsers scans the supported user-facing products (no user pick needed).
// Product list: Chromium (默认) · BrowserOS · Chrome. ego-lite is intentionally excluded.
func DetectLocalBrowsers() []BrowserCandidate {
	home, _ := os.UserHomeDir()
	type spec struct {
		id, label, engine, desc, icon string
		paths                         []string
		primary                       bool
	}
	specs := []spec{
		{
			id: "chromium", label: "Chromium", engine: EngineChromium, primary: true,
			icon: "chromium",
			desc: "开源 Chromium 内核，轻量稳定，独立指纹环境默认首选",
			paths: []string{
				"/Applications/Chromium.app/Contents/MacOS/Chromium",
				filepath.Join(home, "Applications", "Chromium.app", "Contents", "MacOS", "Chromium"),
				"/usr/bin/chromium",
				"/usr/bin/chromium-browser",
			},
		},
		{
			id: "browseros", label: "BrowserOS", engine: EngineBrowserOS,
			icon: "browseros",
			desc: "开源 agentic 浏览器，CDP 本机控制，适合自动化与账号隔离启动",
			paths: []string{
				"/Applications/BrowserOS.app/Contents/MacOS/BrowserOS",
				filepath.Join(home, "Applications", "BrowserOS.app", "Contents", "MacOS", "BrowserOS"),
			},
		},
		{
			id: "chrome", label: "Google Chrome", engine: EngineChromium,
			icon: "chrome",
			desc: "官方 Chrome，兼容性好；自动化特征更明显，仅作备选",
			paths: []string{
				"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
				filepath.Join(home, "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"),
			},
		},
	}
	out := make([]BrowserCandidate, 0, len(specs))
	for _, sp := range specs {
		c := BrowserCandidate{
			ID: sp.id, Label: sp.label, Engine: sp.engine, Primary: sp.primary,
			Description: sp.desc, Icon: sp.icon,
		}
		for _, p := range sp.paths {
			if p == "" {
				continue
			}
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				c.Path = p
				c.Found = true
				break
			}
			// .app bundle path given
			if st, err := os.Stat(p); err == nil && st.IsDir() && strings.HasSuffix(p, ".app") {
				inner := filepath.Join(p, "Contents", "MacOS")
				entries, _ := os.ReadDir(inner)
				for _, e := range entries {
					if !e.IsDir() {
						c.Path = filepath.Join(inner, e.Name())
						c.Found = true
						break
					}
				}
				if c.Found {
					break
				}
			}
		}
		// also try ResolveBinary for browseros/fingerprint
		if !c.Found {
			if resolved, err := ResolveBinary(sp.engine, "", "", "", ""); err == nil && resolved != "" {
				c.Path = resolved
				c.Found = true
			}
		}
		out = append(out, c)
	}
	return out
}

// PreferredLocalBrowser returns the installed product recommendation (Chromium first).
func PreferredLocalBrowser() (path string, engine string) {
	cands := DetectLocalBrowsers()
	// prefer primary found
	for _, c := range cands {
		if c.Found && c.Primary {
			return c.Path, c.Engine
		}
	}
	for _, c := range cands {
		if c.Found {
			return c.Path, c.Engine
		}
	}
	return "", ""
}

func defaultChromiumPaths() []string {
	return []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"chromium",
	}
}

// SecurityBaseArgs are always applied for CDP isolation + anti-leak.
// CDP must stay on 127.0.0.1 only (never 0.0.0.0).
func SecurityBaseArgs(cdpPort int, userDataDir string) []string {
	return []string{
		fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
		"--remote-debugging-address=127.0.0.1",
		"--remote-allow-origins=*", // CDP remains loopback-only; permits the local Worker WebSocket origin.
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-sync",
		"--disable-default-apps",
		"--no-ping",
		"--password-store=basic",
		"--disable-dev-shm-usage",
		"--disable-breakpad", // crash reporter off
		"--disable-crash-reporter",
		"--metrics-recording-only",
		"--disable-component-update",
		"--disable-domain-reliability",
		"--disable-features=Translate,MediaRouter,OptimizationHints",
		"--disable-client-side-phishing-detection",
		"--disable-non-proxied-udp",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--disable-blink-features=AutomationControlled",
	}
}
