package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/sidecar"
	"github.com/spf13/cobra"
)

func TestValidateUIServeDataDirRejectsDevelopmentBuildCache(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "prepare-dev.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.DataDir = dataDir
	if err := validateUIServeDataDir(cfg); err == nil || !strings.Contains(err.Error(), "UI_DATA_DIR_IS_BUILD_CACHE") {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateUIServeDataDirAllowsOrdinaryExplicitDirectory(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	if err := validateUIServeDataDir(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestApplyUIServePortHonorsExplicitZero(t *testing.T) {
	cmd := &cobra.Command{Use: "serve"}
	cmd.Flags().Int("port", config.DefaultUIPort, "UI port")
	if err := cmd.Flags().Set("port", "0"); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	applyUIServePort(cmd, &cfg)
	if cfg.UIPort != 0 {
		t.Fatalf("UI port = %d, want 0", cfg.UIPort)
	}
}

func TestUIServeExposesSidecarDescriptorFlag(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"ui", "serve"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Flags().Lookup("sidecar-descriptor") == nil {
		t.Fatal("ui serve missing --sidecar-descriptor")
	}
}

func TestJoinSidecarDescriptorCleanupPreservesBothErrors(t *testing.T) {
	runErr := errors.New("serve failed")
	got := joinSidecarDescriptorCleanup(runErr, sidecar.ErrDescriptorOwnership)
	if !errors.Is(got, runErr) {
		t.Fatalf("joined error lost serve failure: %v", got)
	}
	if !errors.Is(got, errSidecarDescriptorCleanup) {
		t.Fatalf("joined error lacks cleanup classification: %v", got)
	}
	if !errors.Is(got, sidecar.ErrDescriptorOwnership) {
		t.Fatalf("joined error lost cleanup cause: %v", got)
	}
	if strings.Contains(got.Error(), sidecar.ErrDescriptorOwnership.Error()) {
		t.Fatalf("cleanup detail leaked through public error: %v", got)
	}
}
