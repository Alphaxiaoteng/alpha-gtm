package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultDataDir  = "~/.alpha-nexus-egolite"
	DefaultCDPPort  = 15101
	DefaultUIPort   = 17421
	DBFilename      = "app.db"
	ProfilesDir     = "profiles"
	MediaDir        = "media"
	ScreenshotsDir  = "screenshots"
	LogsDir         = "logs"
	RuntimeFilename = "runtime.json"
)

type Config struct {
	DataDir               string `json:"data_dir"`
	EgoBrowserCommand     string `json:"ego_browser_command"`
	MaxConcurrentSpaces   int    `json:"max_concurrent_spaces"`
	MeasuredSpaceCapacity int    `json:"measured_space_capacity"`
	AIImageEnabled        bool   `json:"ai_image_enabled"`
	BrowserPath           string `json:"browser_path"`   // Chromium default binary
	EgoLitePath           string `json:"ego_lite_path"`  // ego-lite agentic Chromium
	BrowserOSPath         string `json:"browseros_path"` // BrowserOS agentic Chromium
	BrowserEngine         string `json:"browser_engine"` // default engine for new profiles
	AIImageProvider       string `json:"ai_image_provider"`
	AIImageModel          string `json:"ai_image_model"`
	AIVideoModel          string `json:"ai_video_model"`
	AIImageAssetDir       string `json:"ai_image_asset_dir"`
	ContentLibraryDir     string `json:"content_library_dir"`
	CDPPort               int    `json:"cdp_port"`
	UIPort                int    `json:"ui_port"`
	MaxConcurrentBrowsers int    `json:"max_concurrent_browsers"` // 0 = Facade default (port pool size)

	// Agent & LLM API Configuration
	AgentAPIProvider string `json:"agent_api_provider"` // openai | deepseek | anthropic | gemini | ollama | qwen | siliconflow | custom
	AgentAPIBaseURL  string `json:"agent_api_base_url"`
	AgentAPIKey      string `json:"agent_api_key"`
	AgentModel       string `json:"agent_model"`
	AgentAPIVerified bool   `json:"agent_api_verified"`
	ThinkingMode     string `json:"thinking_mode"` // auto | none | deep
	CodingModel      string `json:"coding_model"`
	PlanModel        string `json:"plan_model"`

	// Image Generation API Configuration
	ImageAPIProvider string `json:"image_api_provider"`
	ImageAPIBaseURL  string `json:"image_api_base_url"`
	ImageAPIKey      string `json:"image_api_key"`

	// Video Generation API Configuration
	VideoAPIProvider string `json:"video_api_provider"`
	VideoAPIBaseURL  string `json:"video_api_base_url"`
	VideoAPIKey      string `json:"video_api_key"`

	// Media legacy fallback
	MediaAPIProvider string `json:"media_api_provider"`
	MediaAPIBaseURL  string `json:"media_api_base_url"`
	MediaAPIKey      string `json:"media_api_key"`
}

// ResolvedAIImageAssetDir returns the user-selected managed asset folder. The
// caller is responsible for creating it only after an explicit UI action.
func (c Config) ResolvedAIImageAssetDir() (string, error) {
	if c.AIImageAssetDir != "" {
		return filepath.Abs(c.AIImageAssetDir)
	}
	dir, err := c.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ai-image", "assets"), nil
}

// ResolvedContentLibraryDir returns the user-selected local content folder.
// Content records remain in SQLite; only user-selected video files live here.
func (c Config) ResolvedContentLibraryDir() (string, error) {
	if dir := strings.TrimSpace(c.ContentLibraryDir); dir != "" {
		return filepath.Abs(dir)
	}
	dataDir, err := c.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "content"), nil
}

func DefaultConfig() Config {
	return Config{
		DataDir:               DefaultDataDir,
		EgoBrowserCommand:     "ego-browser",
		MaxConcurrentSpaces:   3,
		BrowserEngine:         "ego-lite",
		CDPPort:               DefaultCDPPort,
		UIPort:                DefaultUIPort,
		MaxConcurrentBrowsers: 0,
	}
}

func (c Config) ResolvedDataDir() (string, error) {
	dir := c.DataDir
	if dir == "" {
		dir = DefaultDataDir
	}
	if len(dir) > 0 && dir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand home dir: %w", err)
		}
		dir = filepath.Join(home, dir[1:])
	}
	return filepath.Abs(dir)
}

func (c Config) DBPath() (string, error) {
	dir, err := c.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DBFilename), nil
}

func (c Config) ProfilesDir() (string, error) {
	dir, err := c.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ProfilesDir), nil
}

func (c Config) MediaDir() (string, error) {
	dir, err := c.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, MediaDir), nil
}

func (c Config) ScreenshotsDir() (string, error) {
	dir, err := c.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ScreenshotsDir), nil
}

func (c Config) LogsDir() (string, error) {
	dir, err := c.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LogsDir), nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.EgoBrowserCommand) == "" {
		return fmt.Errorf("ego browser command is required")
	}
	return nil
}
