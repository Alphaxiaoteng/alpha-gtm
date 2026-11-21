package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// 清理只处理普通文件。符号链接指向的可能是目录外的重要文件，
// 因此干脆不碰；也不按目标大小统计，避免夸大回收量。
func TestPruneDirectoryContentsSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "important.txt")
	if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	regular := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(regular, []byte("png"), 0o600); err != nil {
		t.Fatalf("write regular: %v", err)
	}

	removed, bytes := pruneDirectoryContents(dir)
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (only the regular file)", removed)
	}
	if bytes != 3 {
		t.Errorf("bytes = %d, want 3 (the link's target size must not be counted)", bytes)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("the target outside the directory must survive: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the link itself must survive: %v", err)
	}
	if _, err := os.Stat(regular); !os.IsNotExist(err) {
		t.Errorf("the regular file should be gone")
	}
}
