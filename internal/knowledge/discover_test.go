package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverDirectoryKeepsSupportedFilesAndSkipsGeneratedTrees(t *testing.T) {
	root := t.TempDir()
	write := func(name string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# 创作资料\n\n真实内容。"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("编导/脚本.md")
	write("资料.txt")
	write("导出/OpenWiki.html")
	write("忽略.js")
	write("node_modules/依赖.md")
	write("opc-dashboard/dist/构建.md")
	write(".git/内部.md")
	write("1key/账号.md")
	write("credentials/token.md")
	write("账号与Key/平台.md")
	write("密钥/服务.md")
	write("编导/API_Key备份.md")
	write("编导/平台token.txt")

	files, err := DiscoverDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files=%+v", files)
	}
	if files[0].Name != "导出/OpenWiki.html" || files[1].Name != "编导/脚本.md" || files[2].Name != "资料.txt" {
		t.Fatalf("unexpected stable relative names: %+v", files)
	}
}

func TestDiscoverDirectoryRejectsAFileRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one.md")
	if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverDirectory(path); err == nil {
		t.Fatal("expected a directory validation error")
	}
}
