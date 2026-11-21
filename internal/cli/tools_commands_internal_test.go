package cli

import (
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestToolsRuntimeUsesOnlyEgoLiteBrowserConfiguration(t *testing.T) {
	previous := activeCfg
	t.Cleanup(func() { activeCfg = previous })
	activeCfg = config.Config{
		DataDir:             t.TempDir(),
		EgoBrowserCommand:   "/reviewed/ego-browser",
		MaxConcurrentSpaces: 3,
		CDPPort:             15555,
		BrowserPath:         "/legacy/chromium",
		EgoLitePath:         "/legacy/browseros",
		BrowserEngine:       "chromium",
	}

	runtime := toolsRuntime()
	if runtime.EgoBrowserCommand != "/reviewed/ego-browser" || runtime.MaxConcurrentSpaces != 3 {
		t.Fatalf("EgoLite runtime config not propagated: %+v", runtime)
	}
	if runtime.CDPPort != 0 || runtime.Browser != "" || runtime.BrowserOS != "" || runtime.Engine != "" {
		t.Fatalf("legacy browser config leaked into EgoLite runtime: %+v", runtime)
	}
}
