package db_test

import (
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func setupContentKnowledge(t *testing.T) (*db.DB, int64, []int64, func()) {
	t.Helper()
	database, cleanup := setupDB(t)
	if err := database.Migrate(); err != nil {
		cleanup()
		t.Fatal(err)
	}
	base, err := database.CreateKnowledgeBase("引用资料", "")
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	document, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "品牌/voice.md", SourceType: "markdown",
		SHA256: strings.Repeat("a", 64), Status: "ready",
	})
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{
		{Ordinal: 0, HeadingPath: "语气", Content: "自然可信"},
		{Ordinal: 1, HeadingPath: "边界", Content: "不夸张承诺"},
	}); err != nil {
		cleanup()
		t.Fatal(err)
	}
	rows, err := database.Query(`SELECT id FROM knowledge_chunks WHERE document_id=? ORDER BY ordinal`, document.ID)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			cleanup()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		cleanup()
		t.Fatal(err)
	}
	return database, base.ID, ids, cleanup
}

func TestContentKnowledgeCitationsPreserveCanonicalOrderAndSourceSnapshot(t *testing.T) {
	database, baseID, chunks, cleanup := setupContentKnowledge(t)
	defer cleanup()
	content, err := database.CreateContentWithPlannedAtAndKnowledgeCitations(
		"article", "标题", "", "正文", "[]", "draft", "", []int64{chunks[1], chunks[0], chunks[1]},
	)
	if err != nil {
		t.Fatal(err)
	}
	citations, err := database.ListContentKnowledgeCitations(content.ID)
	if err != nil || len(citations) != 2 || citations[0].ChunkID != chunks[1] || citations[0].Ordinal != 0 || citations[1].ChunkID != chunks[0] || citations[1].Ordinal != 1 {
		t.Fatalf("citations=%+v err=%v", citations, err)
	}
	if citations[0].BaseID != baseID || citations[0].DocumentSHA256 != strings.Repeat("a", 64) || citations[0].SourceName != "品牌/voice.md" || citations[0].HeadingPath != "边界" {
		t.Fatalf("canonical snapshot=%+v", citations[0])
	}
	if _, err := database.Exec(`DELETE FROM knowledge_bases WHERE id=?`, baseID); err != nil {
		t.Fatal(err)
	}
	afterSourceDelete, err := database.ListContentKnowledgeCitations(content.ID)
	if err != nil || len(afterSourceDelete) != 2 || afterSourceDelete[0].DocumentSHA256 != citations[0].DocumentSHA256 {
		t.Fatalf("after source delete=%+v err=%v", afterSourceDelete, err)
	}
	if err := database.DeleteContent(content.ID); err != nil {
		t.Fatal(err)
	}
	afterContentDelete, err := database.ListContentKnowledgeCitations(content.ID)
	if err != nil || len(afterContentDelete) != 0 {
		t.Fatalf("after content delete=%+v err=%v", afterContentDelete, err)
	}
}

func TestContentKnowledgeCitationWritesAreAtomicAndBounded(t *testing.T) {
	database, _, chunks, cleanup := setupContentKnowledge(t)
	defer cleanup()
	base, err := database.CreateKnowledgeBase("未就绪资料", "")
	if err != nil {
		t.Fatal(err)
	}
	notReady, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "pending.md", SourceType: "markdown",
		SHA256: strings.Repeat("e", 64), Status: "pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(notReady.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "尚未就绪"}}); err != nil {
		t.Fatal(err)
	}
	var notReadyChunkID int64
	if err := database.QueryRow(`SELECT id FROM knowledge_chunks WHERE document_id=?`, notReady.ID).Scan(&notReadyChunkID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateContentWithPlannedAtAndKnowledgeCitations(
		"article", "未就绪不得留下", "", "正文", "[]", "draft", "", []int64{notReadyChunkID},
	); err == nil {
		t.Fatal("non-ready citation created content")
	}
	var notReadyLeaked int
	if err := database.QueryRow(`SELECT COUNT(*) FROM contents WHERE title='未就绪不得留下'`).Scan(&notReadyLeaked); err != nil || notReadyLeaked != 0 {
		t.Fatalf("leaked=%d err=%v", notReadyLeaked, err)
	}
	if _, err := database.CreateContentWithPlannedAtAndKnowledgeCitations(
		"article", "不得留下", "", "正文", "[]", "draft", "", []int64{999999},
	); err == nil {
		t.Fatal("invalid citation created content")
	}
	var leaked int
	if err := database.QueryRow(`SELECT COUNT(*) FROM contents WHERE title='不得留下'`).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("leaked=%d err=%v", leaked, err)
	}
	content, err := database.CreateContentWithPlannedAtAndKnowledgeCitations(
		"article", "原题", "", "正文", "[]", "draft", "", []int64{chunks[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceContentWithPlannedAtAndKnowledgeCitations(
		content.ID, "article", "不应提交", "", "正文", "[]", "draft", "", []int64{999999},
	); err == nil {
		t.Fatal("invalid citation updated content")
	}
	stored, err := database.GetContent(content.ID)
	if err != nil || stored.Title != "原题" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	citations, err := database.ListContentKnowledgeCitations(content.ID)
	if err != nil || len(citations) != 1 || citations[0].ChunkID != chunks[0] {
		t.Fatalf("citations=%+v err=%v", citations, err)
	}
	if err := database.ReplaceContentWithPlannedAtAndKnowledgeCitations(
		content.ID, "article", "清空", "", "正文", "[]", "draft", "", []int64{},
	); err != nil {
		t.Fatal(err)
	}
	citations, err = database.ListContentKnowledgeCitations(content.ID)
	if err != nil || len(citations) != 0 {
		t.Fatalf("citations=%+v err=%v", citations, err)
	}
	tooMany := make([]int64, 13)
	for index := range tooMany {
		tooMany[index] = chunks[0]
	}
	if err := database.ReplaceContentWithPlannedAtAndKnowledgeCitations(
		content.ID, "article", "过多", "", "正文", "[]", "draft", "", tooMany,
	); err == nil {
		t.Fatal("more than 12 raw citations accepted")
	}
	if err := database.ReplaceContentWithPlannedAtAndKnowledgeCitations(
		content.ID, "article", "无效", "", "正文", "[]", "draft", "", []int64{0},
	); err == nil {
		t.Fatal("non-positive citation accepted")
	}
}
