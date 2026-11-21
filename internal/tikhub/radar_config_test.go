package tikhub

import (
	"reflect"
	"testing"
)

func TestDefaultTrendRadarConfigStartsEmpty(t *testing.T) {
	config := DefaultTrendRadarConfig()
	if len(config.Niches) != 0 {
		t.Fatalf("niches=%d want 0", len(config.Niches))
	}
}

func TestTrendRadarConfigAllowsNoNiches(t *testing.T) {
	dataDir := t.TempDir()
	saved, err := SaveRadarConfig(dataDir, RadarConfig{Niches: []RadarNiche{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Niches) != 0 {
		t.Fatalf("niches=%d want 0", len(saved.Niches))
	}
	loaded, err := LoadRadarConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Niches) != 0 {
		t.Fatalf("loaded niches=%d want 0", len(loaded.Niches))
	}
}

func TestTrendRadarConfigRoundTripNormalizesAndPersists(t *testing.T) {
	dataDir := t.TempDir()
	input := RadarConfig{
		Niches: []RadarNiche{
			{
				ID:          "ai",
				Name:        " AI赛道 ",
				Enabled:     true,
				SearchTerms: []string{" AI工具 ", "AI工具", "", "AI智能体"},
				Platforms:   []string{"xhs", "douyin", "xhs"},
			},
			{
				ID:          "emotion",
				Name:        "情感赛道",
				Enabled:     true,
				SearchTerms: []string{"亲密关系", "情绪价值"},
				Platforms:   []string{"xhs"},
			},
		},
	}
	saved, err := SaveRadarConfig(dataDir, input)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := saved.Niches[0].SearchTerms, []string{"AI工具", "AI智能体"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("terms=%#v want %#v", got, want)
	}
	if got, want := saved.Niches[0].Platforms, []string{"xhs", "douyin"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("platforms=%#v want %#v", got, want)
	}
	loaded, err := LoadRadarConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Niches, saved.Niches) {
		t.Fatalf("round trip mismatch\nloaded=%#v\nsaved=%#v", loaded.Niches, saved.Niches)
	}
}

func TestSaveTrendRadarConfigRejectsEmptySearchTerms(t *testing.T) {
	_, err := SaveRadarConfig(t.TempDir(), RadarConfig{
		Niches: []RadarNiche{{ID: "ai", Name: "AI赛道", Enabled: true, Platforms: []string{"xhs"}}},
	})
	if err == nil {
		t.Fatal("empty search terms accepted")
	}
}

func TestTrendRadarConfigAcceptsYouTubeAndX(t *testing.T) {
	saved, err := SaveRadarConfig(t.TempDir(), RadarConfig{
		Niches: []RadarNiche{{
			ID: "ai", Name: "AI赛道", Enabled: true,
			SearchTerms: []string{"AI工具"},
			Platforms:   []string{"douyin", "youtube", "x"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := saved.Niches[0].Platforms, []string{"douyin", "youtube", "x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("platforms=%#v want %#v", got, want)
	}
}

func TestRadarNichePlatformLimitsDefaultAndClamp(t *testing.T) {
	config, err := normalizeRadarConfig(RadarConfig{Niches: []RadarNiche{{
		ID: "niche-limits", Name: "上限池", Enabled: true,
		SearchTerms: []string{"a"}, Platforms: []string{"xhs", "douyin", "kuaishou"},
		PlatformLimits: map[string]int{"kuaishou": 500, "youtube": 5},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	niche := config.Niches[0]
	if got := niche.ItemLimit("xhs"); got != RadarXHSItemCap {
		t.Fatalf("xhs default cap = %d, want %d", got, RadarXHSItemCap)
	}
	if got := niche.ItemLimit("douyin"); got != RadarDefaultItemCap {
		t.Fatalf("douyin default cap = %d, want %d", got, RadarDefaultItemCap)
	}
	if got := niche.ItemLimit("kuaishou"); got != RadarMaxItemCap {
		t.Fatalf("kuaishou cap must clamp to %d, got %d", RadarMaxItemCap, got)
	}
	if _, ok := niche.PlatformLimits["youtube"]; ok {
		t.Fatal("limits for platforms outside the niche must be dropped")
	}
	if niche.PlatformLimits["xhs"] != RadarXHSItemCap || niche.PlatformLimits["douyin"] != RadarDefaultItemCap {
		t.Fatalf("normalized limits not persisted: %+v", niche.PlatformLimits)
	}
}

func TestEstimateCallCostUSDNeverGuesses(t *testing.T) {
	douyin, ok := FindEndpoint("douyin.video_search")
	if !ok {
		t.Fatal("douyin endpoint missing")
	}
	if cost, priced := EstimateCallCostUSD(douyin); !priced || cost <= 0 {
		t.Fatalf("documented price must parse, got %v priced=%v", cost, priced)
	}
	xhs, ok := FindEndpoint("xhs.note_search")
	if !ok {
		t.Fatal("xhs endpoint missing")
	}
	if cost, priced := EstimateCallCostUSD(xhs); priced || cost != 0 {
		t.Fatalf("unpriced endpoint must report not priced, got %v priced=%v", cost, priced)
	}
}
