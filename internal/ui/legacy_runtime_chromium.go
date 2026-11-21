//go:build legacy_chromium

package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"alpha-nexus-egolite/internal/sessionbackup"
)

type legacyUIRuntime struct {
	accountObserver *accountObserver
	sessionBackups  *sessionbackup.Store
}

const uiBackgroundProbeWait = accountQuickProbeTimeout + 250*time.Millisecond

func initializeLegacyUIRuntime(server *Server, dataDir string) error {
	backups, err := sessionbackup.New(filepath.Join(dataDir, "session-backups"))
	if err != nil {
		return fmt.Errorf("initialize session backups: %w", err)
	}
	server.sessionBackups = backups
	return nil
}
