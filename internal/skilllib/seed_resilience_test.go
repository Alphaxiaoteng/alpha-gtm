package skilllib

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

// The desktop shell swaps managed skill directories for version symlinks; a
// damaged package directory must never keep the whole server from booting.
func TestLibrarySeedSkipsDamagedBuiltinPackage(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	// Seed once so the builtin rows exist, then damage one package directory
	// by replacing it with a symlink (what the desktop version manager does).
	if _, err := New(database, dataDir); err != nil {
		t.Fatal(err)
	}
	if len(builtins) == 0 {
		t.Fatal("expected built-in seeds")
	}
	damaged := builtins[0]
	if err := os.RemoveAll(filepath.Join(dataDir, "skills", damaged.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dataDir, "elsewhere"), filepath.Join(dataDir, "skills", damaged.ID)); err != nil {
		t.Fatal(err)
	}
	library, err := New(database, dataDir)
	if err != nil {
		t.Fatalf("a damaged builtin package must not block startup: %v", err)
	}
	if _, err := library.Get(context.Background(), damaged.ID); err == nil {
		t.Fatalf("damaged skill Get should fail, got err=%v", err)
	}
	// The remaining builtins must still resolve.
	if len(builtins) > 1 {
		next := builtins[1]
		if _, err := library.Get(context.Background(), next.ID); err != nil {
			t.Fatalf("undamaged builtin %s lost: %v", next.ID, err)
		}
	}
}
