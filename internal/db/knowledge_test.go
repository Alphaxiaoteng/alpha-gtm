package db_test

import (
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestKnowledgeChunksSearchWithinSelectedBaseAndReplaceAtomically(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	baseA, err := database.CreateKnowledgeBase("品牌资料", "本地内容规范")
	if err != nil {
		t.Fatal(err)
	}
	baseB, err := database.CreateKnowledgeBase("产品资料", "")
	if err != nil {
		t.Fatal(err)
	}
	docA, created, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: baseA.ID, SourceName: "brand.md", SourceType: "markdown",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status: "ready", OriginalPath: "knowledge/originals/aa", NormalizedPath: "knowledge/normalized/aa.md",
	})
	if err != nil || !created {
		t.Fatalf("doc=%+v created=%v err=%v", docA, created, err)
	}
	duplicate, created, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: baseA.ID, SourceName: "brand-copy.md", SourceType: "markdown",
		SHA256: docA.SHA256, Status: "ready",
	})
	if err != nil || created || duplicate.ID != docA.ID {
		t.Fatalf("duplicate=%+v created=%v err=%v", duplicate, created, err)
	}
	docB, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: baseB.ID, SourceName: "product.md", SourceType: "markdown",
		SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := database.ReplaceKnowledgeChunks(docA.ID, []db.KnowledgeChunkInput{
		{Ordinal: 0, HeadingPath: "语气", Content: "品牌文案要自然、可信，避免夸张承诺。"},
		{Ordinal: 1, HeadingPath: "视频脚本", Content: "前三秒提出真实问题，再给出具体解决步骤。"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(docB.ID, []db.KnowledgeChunkInput{
		{Ordinal: 0, HeadingPath: "产品", Content: "产品支持矩阵账号并行发布。"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := database.SearchKnowledgeChunks([]int64{baseA.ID}, "视频 脚本", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].BaseID != baseA.ID || results[0].HeadingPath != "视频脚本" {
		t.Fatalf("results=%+v", results)
	}
	shortResults, err := database.SearchKnowledgeChunks([]int64{baseA.ID}, "语气", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(shortResults) != 1 || shortResults[0].DocumentID != docA.ID {
		t.Fatalf("two-rune Chinese search should use the bounded fallback: %+v", shortResults)
	}
	separatedResults, err := database.SearchKnowledgeChunks([]int64{baseA.ID}, "视频 前三秒", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(separatedResults) != 1 || separatedResults[0].HeadingPath != "视频脚本" {
		t.Fatalf("separated terms should match across heading and content: %+v", separatedResults)
	}
	broadenedResults, err := database.SearchKnowledgeChunks([]int64{baseA.ID}, "视频 前三秒 不存在的词", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(broadenedResults) != 1 || broadenedResults[0].HeadingPath != "视频脚本" {
		t.Fatalf("zero-result multi-term search should broaden without leaving the selected base: %+v", broadenedResults)
	}
	naturalQueryResults, err := database.SearchKnowledgeChunks([]int64{baseA.ID}, "请帮我做一个前三秒抓住注意力的短视频 脚本应该提供真实解决步骤", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(naturalQueryResults) != 1 || naturalQueryResults[0].HeadingPath != "视频脚本" {
		t.Fatalf("multi-field natural Chinese query should use the bounded bigram fallback: %+v", naturalQueryResults)
	}
	isolatedResults, err := database.SearchKnowledgeChunks([]int64{baseA.ID}, "视频 发布 不存在的词", 6)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range isolatedResults {
		if result.BaseID != baseA.ID {
			t.Fatalf("broadened search leaked another knowledge base: %+v", isolatedResults)
		}
	}

	if err := database.ReplaceKnowledgeChunks(docA.ID, []db.KnowledgeChunkInput{
		{Ordinal: 0, HeadingPath: "新规范", Content: "开场必须直接说明用户收益。"},
	}); err != nil {
		t.Fatal(err)
	}
	oldResults, err := database.SearchKnowledgeChunks([]int64{baseA.ID}, "前三秒", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldResults) != 0 {
		t.Fatalf("stale chunks remained searchable: %+v", oldResults)
	}
}

func TestKnowledgeRejectsInvalidNamesHashesAndChunks(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateKnowledgeBase("", ""); err == nil {
		t.Fatal("blank knowledge base name was accepted")
	}
	base, err := database.CreateKnowledgeBase("资料", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "bad.md", SourceType: "markdown", SHA256: "not-a-sha", Status: "ready",
	}); err == nil {
		t.Fatal("invalid sha was accepted")
	}
	doc, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "good.md", SourceType: "markdown",
		SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(doc.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: ""}}); err == nil {
		t.Fatal("empty chunk was accepted")
	}
}

func TestKnowledgeBaseListIncludesIndexedCountsAndLatestImport(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	base, err := database.CreateKnowledgeBase("编导资料", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := database.CreateKnowledgeImportRun(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, ImportRunID: run.ID, SourceName: "编导/开场.md", SourceType: "markdown",
		SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, ImportRunID: run.ID, SourceName: "空页面.html", SourceType: "html",
		SHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Status: "failed",
		ErrorCode: "no_extractable_text",
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{
		{Ordinal: 0, HeadingPath: "钩子", Content: "前三秒直接说明观众收益。"},
		{Ordinal: 1, HeadingPath: "正文", Content: "正文用案例证明结论。"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.CompleteKnowledgeImportRun(run.ID, 1, 1, 0, 0); err != nil {
		t.Fatal(err)
	}

	items, err := database.ListKnowledgeBases()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].DocumentCount != 1 || items[0].ChunkCount != 2 ||
		items[0].LatestImport == nil || items[0].LatestImport.ID != run.ID ||
		items[0].LatestImport.ImportedFiles != 1 {
		t.Fatalf("knowledge base summary=%+v", items)
	}
}

func TestKnowledgeSearchAndSummaryExcludeNonReadyDocumentChunks(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	base, err := database.CreateKnowledgeBase("状态隔离资料", "")
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "ready.md", SourceType: "markdown",
		SHA256: "1111111111111111111111111111111111111111111111111111111111111111", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "failed.md", SourceType: "markdown",
		SHA256: "2222222222222222222222222222222222222222222222222222222222222222", Status: "failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "pending.md", SourceType: "markdown",
		SHA256: "3333333333333333333333333333333333333333333333333333333333333333", Status: "pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		documentID int64
		content    string
	}{
		{ready.ID, "当前可引用的资料"},
		{failed.ID, "失败文档遗留片段"},
		{pending.ID, "处理中文档遗留片段"},
	} {
		if err := database.ReplaceKnowledgeChunks(item.documentID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: item.content}}); err != nil {
			t.Fatal(err)
		}
	}

	for _, query := range []string{"失败文档遗留片段", "处理中文档遗留片段"} {
		results, searchErr := database.SearchKnowledgeChunks([]int64{base.ID}, query, 6)
		if searchErr != nil || len(results) != 0 {
			t.Fatalf("query=%q results=%+v err=%v", query, results, searchErr)
		}
	}
	results, err := database.SearchKnowledgeChunks([]int64{base.ID}, "当前可引用的资料", 6)
	if err != nil || len(results) != 1 || results[0].DocumentID != ready.ID {
		t.Fatalf("ready results=%+v err=%v", results, err)
	}
	bases, err := database.ListKnowledgeBases()
	if err != nil || len(bases) != 1 || bases[0].DocumentCount != 1 || bases[0].ChunkCount != 1 {
		t.Fatalf("bases=%+v err=%v", bases, err)
	}
}

func TestKnowledgeMigrationKeepsNewestReadySourceAndPreventsDuplicateCurrentVersions(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	base, err := database.CreateKnowledgeBase("历史版本资料", "")
	if err != nil {
		t.Fatal(err)
	}
	older, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "guide.md", SourceType: "markdown",
		SHA256: "4444444444444444444444444444444444444444444444444444444444444444", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(older.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "legacyonlytoken"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DROP INDEX idx_knowledge_documents_ready_source`); err != nil {
		t.Fatal(err)
	}
	newer, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "guide.md", SourceType: "markdown",
		SHA256: "5555555555555555555555555555555555555555555555555555555555555555", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(newer.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "currentonlytoken"}}); err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var olderStatus, newerStatus string
	if err := database.QueryRow(`SELECT status FROM knowledge_documents WHERE id=?`, older.ID).Scan(&olderStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status FROM knowledge_documents WHERE id=?`, newer.ID).Scan(&newerStatus); err != nil {
		t.Fatal(err)
	}
	if olderStatus != "superseded" || newerStatus != "ready" {
		t.Fatalf("older=%q newer=%q", olderStatus, newerStatus)
	}
	oldResults, err := database.SearchKnowledgeChunks([]int64{base.ID}, "legacyonlytoken", 6)
	if err != nil || len(oldResults) != 0 {
		t.Fatalf("old results=%+v err=%v", oldResults, err)
	}
	newResults, err := database.SearchKnowledgeChunks([]int64{base.ID}, "currentonlytoken", 6)
	if err != nil || len(newResults) != 1 || newResults[0].DocumentID != newer.ID {
		t.Fatalf("new results=%+v err=%v", newResults, err)
	}
	if _, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "guide.md", SourceType: "markdown",
		SHA256: "6666666666666666666666666666666666666666666666666666666666666666", Status: "ready",
	}); err == nil {
		t.Fatal("duplicate current source version was accepted")
	}
}

func TestKnowledgeImportRunProgressIsVisibleBeforeCompletion(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	base, err := database.CreateKnowledgeBase("批量资料", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := database.CreateKnowledgeImportRun(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateKnowledgeImportRunProgress(run.ID, 2, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListKnowledgeBases()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].LatestImport == nil || items[0].LatestImport.Status != "running" ||
		items[0].LatestImport.TotalFiles != 2 || items[0].LatestImport.ImportedFiles != 1 ||
		items[0].LatestImport.SkippedFiles != 1 {
		t.Fatalf("running import summary=%+v", items)
	}
}
