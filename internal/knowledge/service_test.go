package knowledge

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

type recordingSearchBackend struct {
	requests []BackendSearchRequest
	contexts []context.Context
	results  []db.KnowledgeSearchResult
}

func (b *recordingSearchBackend) Manifest() BackendManifest {
	return BackendManifest{ID: "recording"}
}

func (b *recordingSearchBackend) Search(ctx context.Context, request BackendSearchRequest) ([]db.KnowledgeSearchResult, error) {
	b.contexts = append(b.contexts, ctx)
	b.requests = append(b.requests, request)
	return b.results, nil
}

type recordingContextKey struct{}

func TestFTS5BackendManifestAndNilFallback(t *testing.T) {
	database := openKnowledgeTestDB(t)
	want := BackendManifest{ID: "fts5", Capabilities: []string{"scoped_search"}}
	for _, service := range []*Service{NewService(database), NewServiceWithBackend(database, nil)} {
		if got := service.backend.Manifest(); !reflect.DeepEqual(got, want) {
			t.Fatalf("manifest=%+v want=%+v", got, want)
		}
	}
}

func TestFTS5BackendReturnsCancelledContextBeforeDatabaseQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, err := newFTS5SearchBackend(nil).Search(ctx, BackendSearchRequest{Query: "query", CandidateLimit: 6})
	if err != context.Canceled || results != nil {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestServiceSearchPassesOriginalContextToBackend(t *testing.T) {
	backend := &recordingSearchBackend{}
	wantCtx := context.WithValue(context.Background(), recordingContextKey{}, "sentinel")
	if _, err := NewServiceWithBackend(openKnowledgeTestDB(t), backend).Search(wantCtx, SearchRequest{Query: "context"}); err != nil {
		t.Fatal(err)
	}
	if len(backend.contexts) != 1 || backend.contexts[0] != wantCtx {
		t.Fatalf("contexts=%+v want=%v", backend.contexts, wantCtx)
	}
}

func TestServiceSearchPassesNormalizedRequestToBackend(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("后端范围资料", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "品牌/语气.md", SourceType: "markdown",
		SHA256: strings.Repeat("a", 64), Status: "ready",
	}); err != nil {
		t.Fatal(err)
	}
	backend := &recordingSearchBackend{}
	service := NewServiceWithBackend(database, backend)
	if _, err := service.Search(context.Background(), SearchRequest{
		Scopes: []Scope{{BaseID: base.ID, SourcePrefix: "  品牌\\  "}, {BaseID: base.ID, SourcePrefix: "品牌"}},
		Query:  "  可靠引用  ", TopK: 2,
	}); err != nil {
		t.Fatal(err)
	}
	want := []BackendSearchRequest{{
		Scopes: []Scope{{BaseID: base.ID, SourcePrefix: "品牌"}}, Query: "可靠引用", CandidateLimit: 8,
	}}
	if !reflect.DeepEqual(backend.requests, want) {
		t.Fatalf("requests=%+v want=%+v", backend.requests, want)
	}
}

func TestServiceSearchBoundsCandidatesBeforeCallingBackend(t *testing.T) {
	database := openKnowledgeTestDB(t)
	results := make([]db.KnowledgeSearchResult, 12)
	for index := range results {
		results[index] = db.KnowledgeSearchResult{ChunkID: int64(index + 1), DocumentID: int64(index + 1)}
	}
	backend := &recordingSearchBackend{results: results}
	service := NewServiceWithBackend(database, backend)
	defaultResults, err := service.Search(context.Background(), SearchRequest{Query: "default"})
	if err != nil {
		t.Fatal(err)
	}
	clampedResults, err := service.Search(context.Background(), SearchRequest{Query: "clamped", TopK: 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultResults) != 6 || len(clampedResults) != 12 {
		t.Fatalf("default=%d clamped=%d", len(defaultResults), len(clampedResults))
	}
	if len(backend.requests) != 2 || backend.requests[0].CandidateLimit != 12 || backend.requests[1].CandidateLimit != 12 {
		t.Fatalf("requests=%+v", backend.requests)
	}
}

func TestServiceSearchSkipsBackendForBlankOrInvalidRequest(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("范围验证资料", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "有效/资料.md", SourceType: "markdown",
		SHA256: strings.Repeat("b", 64), Status: "ready",
	}); err != nil {
		t.Fatal(err)
	}
	backend := &recordingSearchBackend{}
	service := NewServiceWithBackend(database, backend)
	if _, err := service.Search(context.Background(), SearchRequest{Query: " \t "}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search(context.Background(), SearchRequest{
		Scopes: []Scope{{BaseID: 0}}, Query: "invalid",
	}); err == nil {
		t.Fatal("invalid scope was accepted")
	}
	if _, err := service.Search(context.Background(), SearchRequest{
		Scopes: []Scope{{BaseID: base.ID, SourcePrefix: "不存在"}}, Query: "invalid folder",
	}); err == nil {
		t.Fatal("invalid folder scope was accepted")
	}
	if len(backend.requests) != 0 {
		t.Fatalf("backend called for blank or invalid request: %+v", backend.requests)
	}
}

func TestBuildCitedContextRespectsBudgetAndKeepsSourceIDs(t *testing.T) {
	results := []db.KnowledgeSearchResult{
		{ChunkID: 11, BaseName: "品牌库", SourceName: "voice.md", HeadingPath: "语气", Content: "自然可信，不夸张。"},
		{ChunkID: 12, BaseName: "脚本库", SourceName: "video.md", HeadingPath: "开场", Content: strings.Repeat("收益", 100)},
	}
	contextText, citations := BuildCitedContext(results, 80)
	if len([]rune(contextText)) > 80 {
		t.Fatalf("context chars=%d", len([]rune(contextText)))
	}
	if len(citations) != 1 || citations[0].ChunkID != 11 || !strings.Contains(contextText, "[KB:11]") {
		t.Fatalf("context=%q citations=%+v", contextText, citations)
	}
}

func TestBuildCitedContextTruncatesFirstOversizedChunkInsteadOfDroppingRAG(t *testing.T) {
	results := []db.KnowledgeSearchResult{{
		ChunkID: 21, BaseName: "长文库", SourceName: "guide.md", HeadingPath: "章节",
		Content: strings.Repeat("知识", 1000),
	}}
	contextText, citations := BuildCitedContext(results, 1000)
	if len([]rune(contextText)) != 1000 || len(citations) != 1 || citations[0].ChunkID != 21 {
		t.Fatalf("context chars=%d citations=%+v", len([]rune(contextText)), citations)
	}
}

func TestCitationsUsedInOutputRequiresExplicitRetrievedMarker(t *testing.T) {
	retrieved := []Citation{
		{ChunkID: 11, BaseName: "品牌库", SourceName: "voice.md"},
		{ChunkID: 12, BaseName: "脚本库", SourceName: "video.md"},
		{ChunkID: 11, BaseName: "品牌库", SourceName: "voice.md"},
	}
	used := CitationsUsedInOutput("按来源改写 [KB:12]；未知标记 [KB:99] 不得采信。", retrieved)
	if len(used) != 1 || used[0].ChunkID != 12 {
		t.Fatalf("used=%+v", used)
	}
	if got := CitationsUsedInOutput("没有引用标记", retrieved); len(got) != 0 {
		t.Fatalf("unattributed output kept citations: %+v", got)
	}
}

func TestServiceSearchClampsTopKAndReturnsEmptyForBlankQuery(t *testing.T) {
	database := openKnowledgeTestDB(t)
	service := NewService(database)
	results, err := service.Search(context.Background(), SearchRequest{Query: "", TopK: 99})
	if err != nil || len(results) != 0 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	base, err := database.CreateKnowledgeBase("资料", "")
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "guide.md", SourceType: "markdown",
		SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(doc.ID, []db.KnowledgeChunkInput{{Ordinal: 0, HeadingPath: "标题", Content: "内容创作需要可靠引用。"}}); err != nil {
		t.Fatal(err)
	}
	results, err = service.Search(context.Background(), SearchRequest{BaseIDs: []int64{base.ID}, Query: "可靠引用", TopK: 99})
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestServiceSearchScopesToAnExplicitFolder(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("分范围资料", "")
	if err != nil {
		t.Fatal(err)
	}
	for index, source := range []string{"品牌/语气.md", "脚本/开场.md"} {
		document, _, createErr := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
			BaseID: base.ID, SourceName: source, SourceType: "markdown",
			SHA256: fmt.Sprintf("%064d", index+1), Status: "ready",
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if replaceErr := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "真实创作证据"}}); replaceErr != nil {
			t.Fatal(replaceErr)
		}
	}
	results, err := NewService(database).Search(context.Background(), SearchRequest{
		Scopes: []Scope{{BaseID: base.ID, SourcePrefix: "品牌"}}, Query: "真实创作证据", TopK: 6,
	})
	if err != nil || len(results) != 1 || results[0].SourceName != "品牌/语气.md" {
		t.Fatalf("scoped results=%+v err=%v", results, err)
	}
	if _, err := NewService(database).ValidateScopes(context.Background(), []Scope{{BaseID: base.ID, SourcePrefix: "不存在"}}); err == nil {
		t.Fatal("unknown folder scope was accepted")
	}
}

func TestServiceSearchScopesBeforeTheGlobalCandidateLimit(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("候选集范围资料", "")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 12; index++ {
		document, _, createErr := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
			BaseID: base.ID, SourceName: fmt.Sprintf("其他/候选-%02d.md", index), SourceType: "markdown",
			SHA256: fmt.Sprintf("%064d", index+1), Status: "ready",
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if replaceErr := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "作用域候选命中 洞见"}}); replaceErr != nil {
			t.Fatal(replaceErr)
		}
	}
	target, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "目标/唯一资料.md", SourceType: "markdown",
		SHA256: fmt.Sprintf("%064d", 99), Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(target.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "作用域候选命中 洞见"}}); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"作用域候选命中", "洞见"} {
		results, searchErr := NewService(database).Search(context.Background(), SearchRequest{
			Scopes: []Scope{{BaseID: base.ID, SourcePrefix: "目标"}}, Query: query, TopK: 6,
		})
		if searchErr != nil || len(results) != 1 || results[0].DocumentID != target.ID || results[0].SourceName != "目标/唯一资料.md" {
			t.Fatalf("query=%q scoped results=%+v err=%v", query, results, searchErr)
		}
	}
}

func TestServiceSearchCleansLegacyEscapesFromCitationHeading(t *testing.T) {
	database := openKnowledgeTestDB(t)
	base, err := database.CreateKnowledgeBase("旧索引", "")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "legacy.docx", SourceType: "docx",
		SHA256: strings.Repeat("d", 64), Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{{
		Ordinal: 0, HeadingPath: `AI电商\_纯净\*完整`, Content: "AI 电商知识库",
	}}); err != nil {
		t.Fatal(err)
	}
	results, err := NewService(database).Search(context.Background(), SearchRequest{
		BaseIDs: []int64{base.ID}, Query: "AI", TopK: 1,
	})
	if err != nil || len(results) != 1 || results[0].HeadingPath != "AI电商_纯净*完整" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestServiceSearchPrefersDistinctDocumentsBeforeRepeatedChunks(t *testing.T) {
	database := openKnowledgeTestDB(t)
	service := NewService(database)
	base, err := database.CreateKnowledgeBase("编导库", "")
	if err != nil {
		t.Fatal(err)
	}
	for index, chunks := range [][]db.KnowledgeChunkInput{
		{{Ordinal: 0, Content: "AI 开场钩子"}, {Ordinal: 1, Content: "AI 分镜推进"}},
		{{Ordinal: 0, Content: "AI 叙事结构"}},
		{{Ordinal: 0, Content: "AI 结尾行动"}},
	} {
		document, _, createErr := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
			BaseID: base.ID, SourceName: fmt.Sprintf("source-%d.md", index+1), SourceType: "markdown",
			SHA256: fmt.Sprintf("%064d", index+1), Status: "ready",
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if replaceErr := database.ReplaceKnowledgeChunks(document.ID, chunks); replaceErr != nil {
			t.Fatal(replaceErr)
		}
	}
	results, err := service.Search(context.Background(), SearchRequest{BaseIDs: []int64{base.ID}, Query: "AI", TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].DocumentID == results[1].DocumentID || results[1].DocumentID == results[2].DocumentID || results[0].DocumentID == results[2].DocumentID {
		t.Fatalf("results=%+v", results)
	}
}

func TestServiceSearchBackfillsTopKAfterDuplicateCandidates(t *testing.T) {
	database := openKnowledgeTestDB(t)
	service := NewService(database)
	base, err := database.CreateKnowledgeBase("去重资料", "")
	if err != nil {
		t.Fatal(err)
	}
	contents := []string{
		"AI 重复编导模板", "AI 重复编导模板", "AI 重复编导模板", "AI 重复编导模板",
		"AI 重复编导模板", "AI 重复编导模板", "AI 重复编导模板",
		"AI 先确认用户冲突再设计开场", "AI 根据数据复盘调整选题",
	}
	for index, content := range contents {
		document, _, createErr := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
			BaseID: base.ID, SourceName: fmt.Sprintf("candidate-%d.md", index+1), SourceType: "markdown",
			SHA256: fmt.Sprintf("%064x", index+1), Status: "ready",
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if replaceErr := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: content}}); replaceErr != nil {
			t.Fatal(replaceErr)
		}
	}
	results, err := service.Search(context.Background(), SearchRequest{BaseIDs: []int64{base.ID}, Query: "AI", TopK: 3})
	if err != nil || len(results) != 3 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if results[0].Content != "AI 重复编导模板" || results[1].Content == results[0].Content || results[2].Content == results[0].Content {
		t.Fatalf("search did not backfill distinct candidates: %+v", results)
	}
}

func TestDiversifyKnowledgeResultsDropsSameContentImportedIntoAnotherBase(t *testing.T) {
	results := []db.KnowledgeSearchResult{
		{ChunkID: 1, DocumentID: 11, DocumentSHA: strings.Repeat("a", 64), SourceName: "guide.md"},
		{ChunkID: 2, DocumentID: 22, DocumentSHA: strings.Repeat("a", 64), SourceName: "guide-copy.md"},
		{ChunkID: 3, DocumentID: 33, DocumentSHA: strings.Repeat("b", 64), SourceName: "story.md"},
	}
	got := diversifyKnowledgeResults(results, 3)
	if len(got) != 2 || got[0].ChunkID != 1 || got[1].ChunkID != 3 {
		t.Fatalf("cross-base duplicate content was not removed: %+v", got)
	}
}

func TestDiversifyKnowledgeResultsDropsIdenticalChunksFromDifferentFiles(t *testing.T) {
	results := []db.KnowledgeSearchResult{
		{ChunkID: 1, DocumentID: 11, DocumentSHA: strings.Repeat("a", 64), SourceName: "1681.md", Content: "找对标、编剧、分镜脚本、出图片、出视频、剪辑。"},
		{ChunkID: 2, DocumentID: 22, DocumentSHA: strings.Repeat("b", 64), SourceName: "1682.md", Content: "  找对标、编剧、分镜脚本、出图片、出视频、剪辑。  "},
		{ChunkID: 3, DocumentID: 33, DocumentSHA: strings.Repeat("c", 64), SourceName: "story.md", Content: "先确认场景，再按镜号拆分画面、旁白和素材。"},
	}
	got := diversifyKnowledgeResults(results, 3)
	if len(got) != 2 || got[0].ChunkID != 1 || got[1].ChunkID != 3 {
		t.Fatalf("identical retrieved chunks were not removed: %+v", got)
	}
}

func TestDiversifyKnowledgeResultsDropsContainedNearDuplicateChunks(t *testing.T) {
	shared := strings.Repeat("找对标编剧分镜脚本出图片出视频剪辑保持角色一致性", 8)
	results := []db.KnowledgeSearchResult{
		{ChunkID: 1, DocumentID: 11, DocumentSHA: strings.Repeat("a", 64), SourceName: "1681.md", Content: "流程分享：" + shared},
		{ChunkID: 2, DocumentID: 22, DocumentSHA: strings.Repeat("b", 64), SourceName: "1682.md", Content: "目录一找对标二编剧与分镜三图片处理四出视频五剪辑。详细制作流程：" + shared},
		{ChunkID: 3, DocumentID: 33, DocumentSHA: strings.Repeat("c", 64), SourceName: "story.md", Content: strings.Repeat("先确认用户冲突，再按镜号拆分画面旁白素材和时长。", 8)},
	}
	got := diversifyKnowledgeResults(results, 3)
	if len(got) != 2 || got[0].ChunkID != 1 || got[1].ChunkID != 3 {
		t.Fatalf("near-duplicate retrieved chunks were not removed: %+v", got)
	}
}
