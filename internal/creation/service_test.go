package creation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestRunUsesOnlyAttachedKnowledgeBases(t *testing.T) {
	store := &fakeStore{
		session: db.CreationSession{ID: "session_1", Title: "露营避坑", TopicBrief: "新手常见错误", Style: "真实具体", KnowledgeBaseIDs: []int64{8}},
		roles: map[string]db.CreationModelRole{
			"copywriter": {Role: "copywriter", ProviderID: "custom", ModelID: "kimi-k2"},
			"reviewer":   {Role: "reviewer", ProviderID: "custom", ModelID: "gemini-3.6-flash"},
		},
	}
	retriever := &fakeRetriever{results: []db.KnowledgeSearchResult{{ChunkID: 12, BaseID: 8, BaseName: "露营资料", SourceName: "avoid.md", Content: "帐篷要先检查防水。"}}}
	runner := &fakeRunner{responses: []string{"文案 [KB:12]", "结论：pass\n依据充分"}}
	service := NewService(store, retriever, runner)

	result, err := service.Run(context.Background(), RunRequest{SessionID: "session_1", Action: "draft", AllowExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := retriever.baseIDs, []int64{8}; !sameIDs(got, want) {
		t.Fatalf("retrieval base IDs=%v want=%v", got, want)
	}
	if result.Quality.Decision != "pass" || len(result.Citations) != 1 || result.Citations[0].ChunkID != 12 {
		t.Fatalf("result=%+v", result)
	}
	if len(store.runs) != 1 || store.runs[0].ProviderID != "custom" || store.runs[0].ModelID != "kimi-k2" {
		t.Fatalf("persisted runs=%+v", store.runs)
	}
}

func TestRunWithoutExternalPermissionReturnsLocalDraft(t *testing.T) {
	store := &fakeStore{session: db.CreationSession{ID: "session_1", Title: "露营避坑", KnowledgeBaseIDs: []int64{8}}, roles: map[string]db.CreationModelRole{}}
	runner := &fakeRunner{}
	service := NewService(store, &fakeRetriever{}, runner)

	result, err := service.Run(context.Background(), RunRequest{SessionID: "session_1", Action: "draft", AllowExternal: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "local" || result.Result == "" || runner.calls != 0 {
		t.Fatalf("result=%+v runner calls=%d", result, runner.calls)
	}
	if result.Quality.Decision != "revise" {
		t.Fatalf("local quality=%+v", result.Quality)
	}
}

func TestLocalTopicKeepsAttachedKnowledgeCitation(t *testing.T) {
	store := &fakeStore{session: db.CreationSession{ID: "session_1", Title: "露营避坑", KnowledgeBaseIDs: []int64{8}}, roles: map[string]db.CreationModelRole{}}
	retriever := &fakeRetriever{results: []db.KnowledgeSearchResult{{ChunkID: 12, BaseID: 8, BaseName: "露营资料", SourceName: "avoid.md", Content: "帐篷要先检查防水。"}}}
	service := NewService(store, retriever, &fakeRunner{})

	result, err := service.Run(context.Background(), RunRequest{SessionID: "session_1", Action: "topic", AllowExternal: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Result, "[KB:12]") || len(result.Citations) != 1 || result.Citations[0].ChunkID != 12 {
		t.Fatalf("local topic result=%+v", result)
	}
}

func TestReviewerFallsBackToCopywriterAndMarksFallback(t *testing.T) {
	store := &fakeStore{
		session: db.CreationSession{ID: "session_1", Title: "露营避坑", KnowledgeBaseIDs: []int64{8}},
		roles: map[string]db.CreationModelRole{
			"copywriter": {Role: "copywriter", ProviderID: "custom", ModelID: "kimi-k2"},
		},
	}
	runner := &fakeRunner{responses: []string{"结论：revise\n缺少来源"}}
	service := NewService(store, &fakeRetriever{}, runner)

	result, err := service.Run(context.Background(), RunRequest{SessionID: "session_1", Action: "review", AllowExternal: true, Input: "帐篷一定不会漏水"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsedFallback || result.ProviderID != "custom" || result.ModelID != "kimi-k2" || result.Quality.Decision != "revise" {
		t.Fatalf("result=%+v", result)
	}
}

func TestExternalPromptFitsIsolatedTextTaskByteLimitWithChineseKnowledge(t *testing.T) {
	prompt := externalPrompt(&db.CreationSession{Title: "露营避坑", Style: "真实具体"}, RunRequest{Action: "draft", Input: "写一篇完整文案"}, strings.Repeat("高密度中文资料。", 3_000))
	if got := len([]byte(prompt)); got > 15_000 {
		t.Fatalf("prompt bytes=%d exceeds isolated text task limit=15000", got)
	}
}

type fakeStore struct {
	session db.CreationSession
	roles   map[string]db.CreationModelRole
	runs    []db.CreationRunInput
}

func (store *fakeStore) GetCreationSession(id string) (*db.CreationSession, error) {
	if id != store.session.ID {
		return nil, errors.New("not found")
	}
	copy := store.session
	return &copy, nil
}

func (store *fakeStore) GetCreationModelRole(role string) (*db.CreationModelRole, error) {
	item, ok := store.roles[role]
	if !ok {
		return nil, errors.New("not found")
	}
	return &item, nil
}

func (store *fakeStore) AppendCreationRun(input db.CreationRunInput) (*db.CreationRun, error) {
	store.runs = append(store.runs, input)
	return &db.CreationRun{ID: "run_" + input.Action, SessionID: input.SessionID, Action: input.Action}, nil
}

type fakeRetriever struct {
	baseIDs []int64
	results []db.KnowledgeSearchResult
}

func (retriever *fakeRetriever) Search(_ context.Context, baseIDs []int64, _ string) ([]db.KnowledgeSearchResult, error) {
	retriever.baseIDs = append([]int64(nil), baseIDs...)
	return retriever.results, nil
}

type fakeRunner struct {
	calls     int
	responses []string
}

func (runner *fakeRunner) Verify(_ context.Context, _ ModelSelection) error { return nil }

func (runner *fakeRunner) Complete(_ context.Context, _ ModelSelection, _, _ string) (string, error) {
	runner.calls++
	if len(runner.responses) == 0 {
		return "", errors.New("missing fake response")
	}
	response := runner.responses[0]
	runner.responses = runner.responses[1:]
	return response, nil
}

func sameIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
