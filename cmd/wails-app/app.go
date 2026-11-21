package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/ui"
)

// App struct
type App struct {
	ctx    context.Context
	server *ui.Server
}

// NewApp creates a new App struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Load configuration
	cfg := config.DefaultConfig()

	// Resolve data directory
	dataDir, err := cfg.ResolvedDataDir()
	if err == nil {
		configPath := filepath.Join(dataDir, "config.json")
		if _, err := os.Stat(configPath); err == nil {
			if fileData, err := os.ReadFile(configPath); err == nil {
				var configData struct {
					BrowserPath string `json:"browser_path"`
				}
				if err := json.Unmarshal(fileData, &configData); err == nil {
					if configData.BrowserPath != "" {
						cfg.BrowserPath = configData.BrowserPath
					}
				}
			}
		}
	}

	// Initialize our standard HTTP UI Server
	srv, err := ui.NewServer(cfg)
	if err != nil {
		fmt.Printf("Failed to initialize server: %v\n", err)
		return
	}
	a.server = srv
}

// shutdown is called when the app closes.
func (a *App) shutdown(ctx context.Context) {
	if a.server != nil {
		a.server.Close()
	}
}

// ServeHTTP implements http.Handler to dynamic route Wails requests.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.server != nil {
		a.server.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Service Unavailble (initializing)", 503)
}
