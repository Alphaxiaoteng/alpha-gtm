package tikhub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	radarConfigVersion = 1
	maxRadarNiches     = 8
	maxRadarTerms      = 20
	// RadarPlatformItemCap bounds how many items one platform may contribute to
	// the pool per paid call. It is a product guard, not a provider parameter:
	// TikHub bills per request, so the cap protects downstream volume while the
	// per-run call budget protects credit.
	RadarDefaultItemCap = 30
	RadarXHSItemCap     = 10
	RadarMaxItemCap     = 100
)

type RadarNiche struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	SearchTerms []string `json:"search_terms"`
	Platforms   []string `json:"platforms"`
	// PlatformLimits caps how many items each platform may add per run.
	// Missing entries fall back to DefaultRadarItemCap (xhs: 10).
	PlatformLimits map[string]int `json:"platform_limits,omitempty"`
}

type RadarConfig struct {
	Version   int          `json:"version"`
	Niches    []RadarNiche `json:"niches"`
	UpdatedAt string       `json:"updated_at,omitempty"`
}

// DefaultRadarItemCapForPlatform returns the product default item cap for a platform.
func DefaultRadarItemCapForPlatform(platform string) int {
	if platform == "xhs" {
		return RadarXHSItemCap
	}
	return RadarDefaultItemCap
}

// ItemLimit returns the effective per-run item cap for one platform.
func (n RadarNiche) ItemLimit(platform string) int {
	if limit, ok := n.PlatformLimits[platform]; ok && limit > 0 {
		if limit > RadarMaxItemCap {
			return RadarMaxItemCap
		}
		return limit
	}
	return DefaultRadarItemCapForPlatform(platform)
}

func normalizePlatformLimits(raw map[string]int, platforms []string) map[string]int {
	limits := make(map[string]int, len(platforms))
	for _, platform := range platforms {
		limit := DefaultRadarItemCapForPlatform(platform)
		if value, ok := raw[platform]; ok && value > 0 {
			limit = value
		}
		if limit > RadarMaxItemCap {
			limit = RadarMaxItemCap
		}
		if limit < 1 {
			limit = 1
		}
		limits[platform] = limit
	}
	return limits
}

var radarConfigMu sync.Mutex

func DefaultTrendRadarConfig() RadarConfig {
	return RadarConfig{
		Version: radarConfigVersion,
		Niches:  []RadarNiche{},
	}
}

// BuiltinCuratedNiches returns the high-converting default niche catalog covering
// male emotional mindset, trending cultural memes, and AI creator monetization.
func BuiltinCuratedNiches() []RadarNiche {
	return []RadarNiche{
		{
			ID:          "niche-male-emotion",
			Name:        "男性情感与逆袭",
			Description: "男性思维、两性脱单、聊天实战���片、展示面与自我提升",
			Enabled:     true,
			SearchTerms: []string{
				"男性情感",
				"男生聊天技巧",
				"恋爱实战",
				"高情商回复",
				"女生潜台词",
				"断联挽回",
				"男性展示面",
				"男性认知提升",
			},
			Platforms: []string{"douyin", "xhs", "kuaishou", "wechat_video"},
		},
		{
			ID:          "niche-trending-memes",
			Name:        "热点爆梗与大众情绪",
			Description: "全网热搜爆梗、高赞神回复反转、大众日常情绪共鸣话题",
			Enabled:     true,
			SearchTerms: []string{
				"今日热梗",
				"全网都在搜",
				"神回复",
				"神反转",
				"年轻人现状",
				"发疯文学",
				"搞钱思维",
			},
			Platforms: []string{"douyin", "xhs", "kuaishou"},
		},
		{
			ID:          "niche-ai-monetization",
			Name:        "AI赛道与变现博主",
			Description: "AI工具推荐、数字人、自媒体搞钱、人像展示面与视频生产",
			Enabled:     true,
			SearchTerms: []string{
				"AI变现",
				"AI工具推荐",
				"AI展示面",
				"AI颜值变现",
				"AI自媒体搞钱",
				"Sora���频",
				"AI智能体",
				"ChatGPT指令",
			},
			Platforms: []string{"douyin", "xhs", "youtube", "wechat_video"},
		},
	}
}

func radarConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "tikhub", "trend_radar.json")
}

func LoadRadarConfig(dataDir string) (RadarConfig, error) {
	radarConfigMu.Lock()
	defer radarConfigMu.Unlock()
	raw, err := os.ReadFile(radarConfigPath(dataDir))
	if os.IsNotExist(err) {
		return DefaultTrendRadarConfig(), nil
	}
	if err != nil {
		return RadarConfig{}, err
	}
	var config RadarConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return RadarConfig{}, fmt.Errorf("decode trend radar config: %w", err)
	}
	return normalizeRadarConfig(config)
}

func SaveRadarConfig(dataDir string, input RadarConfig) (RadarConfig, error) {
	config, err := normalizeRadarConfig(input)
	if err != nil {
		return RadarConfig{}, err
	}
	config.Version = radarConfigVersion
	config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return RadarConfig{}, err
	}

	radarConfigMu.Lock()
	defer radarConfigMu.Unlock()
	dir := filepath.Join(dataDir, "tikhub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return RadarConfig{}, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return RadarConfig{}, err
	}
	temp, err := os.CreateTemp(dir, ".trend-radar-*.tmp")
	if err != nil {
		return RadarConfig{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return RadarConfig{}, err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return RadarConfig{}, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return RadarConfig{}, err
	}
	if err := temp.Close(); err != nil {
		return RadarConfig{}, err
	}
	if err := os.Rename(tempPath, radarConfigPath(dataDir)); err != nil {
		return RadarConfig{}, err
	}
	return config, nil
}

func normalizeRadarConfig(input RadarConfig) (RadarConfig, error) {
	if len(input.Niches) > maxRadarNiches {
		return RadarConfig{}, fmt.Errorf("赛道数量不能超过 %d 个", maxRadarNiches)
	}
	allowedPlatforms := map[string]bool{
		"douyin": true, "xhs": true, "kuaishou": true, "wechat_video": true, "wechat_mp": true,
		"youtube": true, "x": true,
	}
	seenIDs := map[string]bool{}
	output := RadarConfig{Version: radarConfigVersion, UpdatedAt: input.UpdatedAt, Niches: make([]RadarNiche, 0, len(input.Niches))}
	for _, rawNiche := range input.Niches {
		niche := RadarNiche{
			ID:             strings.ToLower(strings.TrimSpace(rawNiche.ID)),
			Name:           strings.TrimSpace(rawNiche.Name),
			Description:    strings.TrimSpace(rawNiche.Description),
			Enabled:        rawNiche.Enabled,
			SearchTerms:    uniqueTrimmed(rawNiche.SearchTerms),
			Platforms:      uniqueTrimmed(rawNiche.Platforms),
			PlatformLimits: normalizePlatformLimits(rawNiche.PlatformLimits, uniqueTrimmed(rawNiche.Platforms)),
		}
		if !validRadarID(niche.ID) || seenIDs[niche.ID] {
			return RadarConfig{}, fmt.Errorf("赛道 ID 无效或重复")
		}
		seenIDs[niche.ID] = true
		if niche.Name == "" || utf8.RuneCountInString(niche.Name) > 32 {
			return RadarConfig{}, fmt.Errorf("赛道名称必须为 1-32 个字符")
		}
		if len(niche.SearchTerms) == 0 || len(niche.SearchTerms) > maxRadarTerms {
			return RadarConfig{}, fmt.Errorf("%s 的搜索词必须为 1-%d 个", niche.Name, maxRadarTerms)
		}
		for _, term := range niche.SearchTerms {
			if utf8.RuneCountInString(term) > 64 {
				return RadarConfig{}, fmt.Errorf("%s 的搜索词过长", niche.Name)
			}
		}
		if len(niche.Platforms) == 0 {
			return RadarConfig{}, fmt.Errorf("%s 至少选择一个平台", niche.Name)
		}
		for _, platform := range niche.Platforms {
			if !allowedPlatforms[platform] {
				return RadarConfig{}, fmt.Errorf("不支持的监测平台 %q", platform)
			}
		}
		output.Niches = append(output.Niches, niche)
	}
	return output, nil
}

func uniqueTrimmed(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func validRadarID(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
