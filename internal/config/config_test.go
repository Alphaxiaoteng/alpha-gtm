package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	c := config.DefaultConfig()
	if c.DataDir != config.DefaultDataDir {
		t.Errorf("DefaultDataDir = %q, want %q", c.DataDir, config.DefaultDataDir)
	}
	if c.UIPort != config.DefaultUIPort {
		t.Errorf("DefaultUIPort = %d, want %d", c.UIPort, config.DefaultUIPort)
	}
	if c.EgoBrowserCommand != "ego-browser" {
		t.Errorf("EgoBrowserCommand = %q, want ego-browser", c.EgoBrowserCommand)
	}
	if c.MaxConcurrentSpaces != 3 {
		t.Errorf("MaxConcurrentSpaces = %d, want safe default 3", c.MaxConcurrentSpaces)
	}
	if c.AIImageEnabled {
		t.Error("AIImageEnabled = true, want hidden by default")
	}
}

func TestResolvedDataDir(t *testing.T) {
	t.Run("default tilde expansion", func(t *testing.T) {
		c := config.DefaultConfig()
		dir, err := c.ResolvedDataDir()
		if err != nil {
			t.Fatal(err)
		}
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".alpha-nexus-egolite")
		if dir != want {
			t.Errorf("ResolvedDataDir() = %q, want %q", dir, want)
		}
	})

	t.Run("custom absolute path", func(t *testing.T) {
		c := config.Config{DataDir: "/tmp/test-unibuy"}
		dir, err := c.ResolvedDataDir()
		if err != nil {
			t.Fatal(err)
		}
		if dir != "/tmp/test-unibuy" {
			t.Errorf("ResolvedDataDir() = %q, want %q", dir, "/tmp/test-unibuy")
		}
	})
}

func TestDBPath(t *testing.T) {
	c := config.Config{DataDir: "/tmp/test-unibuy-db"}
	p, err := c.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/tmp/test-unibuy-db/app.db" {
		t.Errorf("DBPath() = %q, want %q", p, "/tmp/test-unibuy-db/app.db")
	}
}

func TestValidate(t *testing.T) {
	t.Run("missing ego browser command", func(t *testing.T) {
		c := config.Config{}
		err := c.Validate()
		if err == nil {
			t.Fatal("expected error for empty ego browser command")
		}
	})

	t.Run("command name is accepted without treating it as a file path", func(t *testing.T) {
		c := config.Config{EgoBrowserCommand: "ego-browser"}
		err := c.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
