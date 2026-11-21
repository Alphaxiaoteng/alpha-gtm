package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestKnowledgeImportDirCreatesBaseAndIndexesOnlySourceDocuments(t *testing.T) {
	dataDir := initTestDir(t)
	sourceDir := t.TempDir()
	for name, body := range map[string]string{
		"编导/短视频脚本.md":          "# 短视频脚本\n\n前三秒建立冲突，再给出解决方案。",
		"node_modules/依赖说明.md": "# 不应导入",
	} {
		path := filepath.Join(sourceDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	output, err := executeCommand(t,
		"knowledge", "import-dir",
		"--base", "AI 内容与短视频",
		"--path", sourceDir,
		"--data-dir", dataDir,
	)
	if err != nil {
		t.Fatalf("output=%s err=%v", output, err)
	}
	for _, expected := range []string{`"type":"knowledge_import_item"`, `"type":"knowledge_import_complete"`, `"imported":1`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in %s", expected, output)
		}
	}

	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	bases, err := database.ListKnowledgeBases()
	if err != nil || len(bases) != 1 || bases[0].Name != "AI 内容与短视频" {
		t.Fatalf("bases=%+v err=%v", bases, err)
	}
	documents, err := database.ListKnowledgeDocuments(bases[0].ID)
	if err != nil || len(documents) != 1 || documents[0].SourceName != "编导/短视频脚本.md" {
		t.Fatalf("documents=%+v err=%v", documents, err)
	}
}

func TestKnowledgeSearchReturnsCitedSourceFromNamedBase(t *testing.T) {
	dataDir := initTestDir(t)
	sourceDir := t.TempDir()
	path := filepath.Join(sourceDir, "编导", "短视频脚本.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# 短视频脚本\n\n前三秒建立冲突，再给出解决方案。"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := executeCommand(t,
		"knowledge", "import-dir",
		"--base", "编导库",
		"--path", sourceDir,
		"--data-dir", dataDir,
	); err != nil {
		t.Fatalf("import output=%s err=%v", output, err)
	}

	output, err := executeCommand(t,
		"knowledge", "search",
		"--base", "编导库",
		"--query", "前三秒建立冲突",
		"--top-k", "3",
		"--data-dir", dataDir,
	)
	if err != nil {
		t.Fatalf("search output=%s err=%v", output, err)
	}
	for _, expected := range []string{`"type":"knowledge_search_results"`, `"source_name":"编导/短视频脚本.md"`, `"heading_path":"短视频脚本"`, `"content":"前三秒建立冲突`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in %s", expected, output)
		}
	}
}
