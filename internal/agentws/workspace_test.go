package agentws

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureAndSandbox(t *testing.T) {
	dir := t.TempDir()
	root, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if root != Path(dir) {
		t.Fatalf("root %s != Path %s", root, Path(dir))
	}
	if _, err := os.Stat(AutomationsPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Fatal(err)
	}

	// inside ok
	wd, err := ResolveWorkDir(dir, AutomationsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if wd != AutomationsPath(dir) {
		t.Fatalf("want automations, got %s", wd)
	}

	// empty → root
	wd, err = ResolveWorkDir(dir, "")
	if err != nil || wd != root {
		t.Fatalf("empty workdir: %v %s", err, wd)
	}

	// outside rejected
	if _, err := ResolveWorkDir(dir, filepath.Join(dir, "..", "evil")); err == nil {
		t.Fatal("expected outside path rejected")
	}
	if IsInside(dir, filepath.Join(os.TempDir(), "nope")) {
		t.Fatal("temp should not be inside")
	}
	if !IsInside(dir, root) {
		t.Fatal("root should be inside")
	}
}
