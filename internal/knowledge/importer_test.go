package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func openKnowledgeTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestChunkMarkdownPreservesHeadingPathAndBoundsLongText(t *testing.T) {
	markdown := "# 产品\n\n介绍。\n\n## 视频脚本\n\n" + strings.Repeat("真实场景和解决步骤。", 220)
	chunks := ChunkMarkdown(markdown)
	if len(chunks) < 2 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	for index, chunk := range chunks {
		if chunk.Ordinal != index {
			t.Fatalf("ordinal=%d index=%d", chunk.Ordinal, index)
		}
		if index > 0 && chunk.HeadingPath != "产品 / 视频脚本" {
			t.Fatalf("heading=%q", chunk.HeadingPath)
		}
		if count := len([]rune(chunk.Content)); count == 0 || count > 1400 {
			t.Fatalf("chunk chars=%d", count)
		}
	}
}

func TestChunkMarkdownRemovesMarkdownEscapesFromCitationHeadings(t *testing.T) {
	chunks := ChunkMarkdown("# AI电商AI化场景探索\\_纯净\\*完整\n\n正文")
	if len(chunks) != 1 || chunks[0].HeadingPath != "AI电商AI化场景探索_纯净*完整" {
		t.Fatalf("chunks=%+v", chunks)
	}
}

func TestImporterContinuesAfterBadFileAndDeduplicates(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("创作资料", "")
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := t.TempDir()
	goodPath := filepath.Join(sourceDir, "openwiki.md")
	if err := os.WriteFile(goodPath, []byte("---\nokf_version: \"0.1\"\ntitle: 品牌手册\n---\n# 品牌语气\n\n文案自然可信，不夸张。"), 0600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(sourceDir, "broken.pdf")
	if err := os.WriteFile(badPath, []byte("not a pdf"), 0600); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	result, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID,
		Files: []InputFile{
			{Name: "openwiki.md", Path: goodPath},
			{Name: "broken.pdf", Path: badPath},
			{Name: "copy.md", Path: goodPath},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 || result.Skipped != 1 || result.Failed != 1 {
		t.Fatalf("result=%+v", result)
	}
	results, err := database.SearchKnowledgeChunks([]int64{base.ID}, "自然可信", 6)
	if err != nil || len(results) != 1 || results[0].SourceName != "openwiki.md" {
		t.Fatalf("search=%+v err=%v", results, err)
	}
}

func TestImporterSkipsUnchangedSourceWithoutReopeningContents(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("增量资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(path, []byte("# 编导\n\n先确认受众，再拆分镜。"), 0o600); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "编导/guide.md", Path: path}},
	})
	if err != nil || first.Imported != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "编导/guide.md", Path: path}},
	})
	if err != nil || second.Skipped != 1 || second.Failed != 0 {
		t.Fatalf("unchanged source should use metadata cache: second=%+v err=%v", second, err)
	}
}

func TestImporterKeepsOnlyLatestSuccessfulSourceVersionSearchable(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("版本化资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(path, []byte("# 运营策略\n\nlegacyonlytoken：先扩大范围。"), 0o600); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "策略/guide.md", Path: path}},
	})
	if err != nil || first.Imported != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}

	if err := os.WriteFile(path, []byte("# 运营策略\n\ncurrentonlytoken：先验证证据。"), 0o600); err != nil {
		t.Fatal(err)
	}
	newTime := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "策略/guide.md", Path: path}},
	})
	if err != nil || second.Imported != 1 {
		t.Fatalf("second=%+v err=%v", second, err)
	}

	oldResults, err := database.SearchKnowledgeChunks([]int64{base.ID}, "legacyonlytoken", 6)
	if err != nil || len(oldResults) != 0 {
		t.Fatalf("stale source version remained searchable: results=%+v err=%v", oldResults, err)
	}
	newResults, err := database.SearchKnowledgeChunks([]int64{base.ID}, "currentonlytoken", 6)
	if err != nil || len(newResults) != 1 || newResults[0].DocumentID != second.Items[0].DocumentID {
		t.Fatalf("latest source version missing: results=%+v err=%v", newResults, err)
	}
	var ready, superseded int
	if err := database.QueryRow(`SELECT
		SUM(CASE WHEN status='ready' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='superseded' THEN 1 ELSE 0 END)
		FROM knowledge_documents WHERE base_id=? AND source_name=?`, base.ID, "策略/guide.md").Scan(&ready, &superseded); err != nil {
		t.Fatal(err)
	}
	if ready != 1 || superseded != 1 {
		t.Fatalf("ready=%d superseded=%d", ready, superseded)
	}
}

func TestImporterKeepsLastReadySourceVersionWhenReplacementFails(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("失败回退资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(path, []byte("# 可用版本\n\n稳定版本唯一词。"), 0o600); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "guide.md", Path: path}},
	})
	if err != nil || first.Imported != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}

	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0xfd}, 0o600); err != nil {
		t.Fatal(err)
	}
	newTime := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "guide.md", Path: path}},
	})
	if err != nil || second.Failed != 1 || second.Items[0].ErrorCode != "parse_failed" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	results, err := database.SearchKnowledgeChunks([]int64{base.ID}, "稳定版本唯一词", 6)
	if err != nil || len(results) != 1 || results[0].DocumentID != first.Items[0].DocumentID {
		t.Fatalf("last ready version was lost: results=%+v err=%v", results, err)
	}
}

func TestImporterDoesNotRollBackCurrentSourceToOlderModifiedVersion(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("防回滚资料", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current.md")
	stalePath := filepath.Join(dir, "stale.md")
	if err := os.WriteFile(currentPath, []byte("# 当前版本\n\ncurrentonlytoken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("# 旧版本\n\nrollbackonlytoken"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(currentPath, now, now); err != nil {
		t.Fatal(err)
	}
	older := now.Add(-time.Hour)
	if err := os.Chtimes(stalePath, older, older); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "guide.md", Path: currentPath}},
	})
	if err != nil || first.Imported != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "guide.md", Path: stalePath}},
	})
	if err != nil || second.Imported != 0 || second.Skipped != 1 || second.Items[0].ErrorCode != "stale_source_version" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	currentResults, err := database.SearchKnowledgeChunks([]int64{base.ID}, "currentonlytoken", 6)
	if err != nil || len(currentResults) != 1 || currentResults[0].DocumentID != first.Items[0].DocumentID {
		t.Fatalf("current results=%+v err=%v", currentResults, err)
	}
	staleResults, err := database.SearchKnowledgeChunks([]int64{base.ID}, "rollbackonlytoken", 6)
	if err != nil || len(staleResults) != 0 {
		t.Fatalf("stale results=%+v err=%v", staleResults, err)
	}
	staleDocument, err := database.GetKnowledgeDocument(second.Items[0].DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	third, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "guide.md", Path: stalePath}},
	})
	if err != nil || third.Imported != 0 || third.Skipped != 1 || third.Items[0].ErrorCode != "stale_source_version" {
		t.Fatalf("third=%+v err=%v", third, err)
	}
	unchangedStaleDocument, err := database.GetKnowledgeDocument(second.Items[0].DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if unchangedStaleDocument.ImportRunID != staleDocument.ImportRunID {
		t.Fatalf("unchanged stale source was reprocessed: before run=%d after run=%d", staleDocument.ImportRunID, unchangedStaleDocument.ImportRunID)
	}
}

func TestImporterSkipsUnchangedNoTextSourceUntilFileChanges(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("空正文增量资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "empty.html")
	if err := os.WriteFile(path, []byte("<!doctype html><html><head><script>window.app=true</script></head><body></body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "dashboard/empty.html", Path: path}},
	})
	if err != nil || first.Failed != 1 || first.Items[0].ErrorCode != "no_extractable_text" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	document, err := database.GetKnowledgeDocument(first.Items[0].DocumentID)
	if err != nil || document.SourceSizeBytes <= 0 || document.SourceModifiedNS <= 0 {
		t.Fatalf("failed document did not retain source metadata: document=%+v err=%v", document, err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "dashboard/empty.html", Path: path}},
	})
	if err != nil || second.Skipped != 1 || second.Failed != 0 || second.Items[0].ErrorCode != "unchanged_no_extractable_text" {
		t.Fatalf("unchanged terminal parse result should be skipped: second=%+v err=%v", second, err)
	}
}

func TestImporterBootstrapsLegacyMetadataFromUnchangedStoredOriginal(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("旧增量资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "legacy.md")
	if err := os.WriteFile(path, []byte("# 旧编导库\n\n保留来源再做检索。"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "旧库/legacy.md", Path: path}},
	})
	if err != nil || first.Imported != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := database.Exec(`UPDATE knowledge_documents SET source_size_bytes=0,source_modified_ns=0 WHERE id=?`, first.Items[0].DocumentID); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "旧库/legacy.md", Path: path}},
	})
	if err != nil || second.Skipped != 1 || second.Failed != 0 {
		t.Fatalf("legacy unchanged source should bootstrap metadata: second=%+v err=%v", second, err)
	}
	document, err := database.GetKnowledgeDocument(first.Items[0].DocumentID)
	if err != nil || document.SourceSizeBytes <= 0 || document.SourceModifiedNS != oldTime.UnixNano() {
		t.Fatalf("document=%+v err=%v", document, err)
	}
}

func TestImporterDoesNotBootstrapLegacyMetadataWhenSourceIsNewer(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("更新资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "changed.md")
	original := []byte("# 原始资料\n\n这是一段旧内容。")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "changed.md", Path: path}},
	})
	if err != nil || first.Imported != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := database.Exec(`UPDATE knowledge_documents SET source_size_bytes=0,source_modified_ns=0 WHERE id=?`, first.Items[0].DocumentID); err != nil {
		t.Fatal(err)
	}
	changed := append([]byte(nil), original...)
	changed[len(changed)-1] = '!'
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	newTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "changed.md", Path: path}},
	})
	if err != nil || second.Failed != 1 || second.Skipped != 0 || second.Items[0].ErrorCode != "read_failed" {
		t.Fatalf("newer source must be hashed instead of bootstrapped: second=%+v err=%v", second, err)
	}
}

func TestImporterMarksCancelledRunInterrupted(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("可中断导入", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(path, []byte("# 内容\n\n不会在取消后处理。"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewImporter(database).Import(ctx, ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "guide.md", Path: path}},
	})
	if err != context.Canceled || result.Total != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	run, err := database.GetKnowledgeImportRun(result.RunID)
	if err != nil || run.Status != "interrupted" || run.CompletedAt == "" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}

func TestImporterRetriesPreviouslyFailedDocumentWithSameSHA(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("重试库", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "retry.bin")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0xfd}, 0600); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(database)
	first, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "retry.md", Path: path}},
	})
	if err != nil || first.Failed != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := importer.Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "retry.pdf", Path: path}},
	})
	if err != nil || second.Failed != 1 || second.Skipped != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestImporterReadsDOCXAndRejectsZipTraversalWithoutStopping(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("文档", "")
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := t.TempDir()
	docxPath := filepath.Join(sourceDir, "script.docx")
	writeDOCXFixture(t, docxPath, "短视频脚本开场要直接说明收益")
	zipPath := filepath.Join(sourceDir, "wiki.zip")
	writeZipFixture(t, zipPath, map[string]string{
		"guide.md":   "# 运营\n\n国内视频平台优先。",
		"../evil.md": "不得写出目标目录",
	})
	result, err := NewImporter(database).Import(context.Background(), ImportRequest{
		BaseID: base.ID,
		Files:  []InputFile{{Name: "script.docx", Path: docxPath}, {Name: "wiki.zip", Path: zipPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 2 || result.Failed != 1 {
		t.Fatalf("result=%+v", result)
	}
	search, err := database.SearchKnowledgeChunks([]int64{base.ID}, "直接说明收益", 6)
	if err != nil || len(search) != 1 {
		t.Fatalf("search=%+v err=%v", search, err)
	}
}

func TestImporterSkipsSensitiveAndUnsupportedSourcesBeforeReading(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("安全资料", "")
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "wiki.zip")
	writeZipFixture(t, zipPath, map[string]string{
		"safe/guide.md":           "# 安全内容\n\n只索引经过筛选的资料。",
		"credentials/token.md":    "不得索引 token",
		"notes/.env-backup.md":    "不得索引 env",
		"assets/illustration.png": "not a supported document",
	})
	result, err := NewImporter(database).Import(context.Background(), ImportRequest{
		BaseID: base.ID,
		Files: []InputFile{
			{Name: "wiki.zip", Path: zipPath},
			{Name: "credentials/missing.md", Path: filepath.Join(t.TempDir(), "does-not-exist")},
			{Name: "secrets/missing.zip", Path: filepath.Join(t.TempDir(), "does-not-exist")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 || result.Skipped != 5 || result.Failed != 0 {
		t.Fatalf("result=%+v", result)
	}
	search, err := database.SearchKnowledgeChunks([]int64{base.ID}, "不得索引", 6)
	if err != nil || len(search) != 0 {
		t.Fatalf("sensitive archive content became searchable: %+v err=%v", search, err)
	}
}

func TestImporterReadsTextPDF(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("PDF 资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "guide.pdf")
	writeTextPDFFixture(t, path, "PDF knowledge workflow")
	result, err := NewImporter(database).Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "guide.pdf", Path: path}},
	})
	if err != nil || result.Imported != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	search, err := database.SearchKnowledgeChunks([]int64{base.ID}, "knowledge workflow", 6)
	if err != nil || len(search) != 1 || search[0].SourceName != "guide.pdf" {
		t.Fatalf("search=%+v err=%v", search, err)
	}
}

func TestImporterReadsOpenWikiHTMLAndDropsExecutableMarkup(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("OpenWiki 资料", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "openwiki.html")
	body := `<!doctype html><html><head><title>编导手册</title><style>.hidden{display:none}</style></head><body><h1>前三秒钩子</h1><p>先说明用户收益，再建立冲突。</p><script>window.secret = "不得索引"</script></body></html>`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewImporter(database).Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "openwiki.html", Path: path}},
	})
	if err != nil || result.Imported != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	search, err := database.SearchKnowledgeChunks([]int64{base.ID}, "用户收益", 6)
	if err != nil || len(search) != 1 || search[0].SourceName != "openwiki.html" || search[0].HeadingPath != "前三秒钩子" {
		t.Fatalf("search=%+v err=%v", search, err)
	}
	unsafe, err := database.SearchKnowledgeChunks([]int64{base.ID}, "不得索引", 6)
	if err != nil || len(unsafe) != 0 {
		t.Fatalf("executable markup became searchable: %+v err=%v", unsafe, err)
	}
}

func TestImporterPersistsReasonForHTMLWithoutExtractableBody(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("空 HTML", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "index.html")
	body := `<!doctype html><html><head><title>应用壳</title></head><body><div id="root"></div><script type="module" src="/src/main.jsx"></script></body></html>`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewImporter(database).Import(context.Background(), ImportRequest{
		BaseID: base.ID, Files: []InputFile{{Name: "index.html", Path: path}},
	})
	if err != nil || result.Failed != 1 || len(result.Items) != 1 || result.Items[0].ErrorCode != "no_extractable_text" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	documents, err := database.ListKnowledgeDocuments(base.ID)
	if err != nil || len(documents) != 1 || documents[0].ErrorCode != "no_extractable_text" {
		t.Fatalf("documents=%+v err=%v", documents, err)
	}
}

func writeDOCXFixture(t *testing.T, path, text string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`,
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeZipFixture(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeTextPDFFixture(t *testing.T, path, text string) {
	t.Helper()
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	var body strings.Builder
	body.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = body.Len()
		fmt.Fprintf(&body, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := body.Len()
	fmt.Fprintf(&body, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&body, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&body, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}
