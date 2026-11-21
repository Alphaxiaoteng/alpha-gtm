package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

type recordingContentAssistBackend struct {
	calls int
}

func (b *recordingContentAssistBackend) Manifest() knowledge.BackendManifest {
	return knowledge.BackendManifest{ID: "recording"}
}

func (b *recordingContentAssistBackend) Search(_ context.Context, _ knowledge.BackendSearchRequest) ([]db.KnowledgeSearchResult, error) {
	b.calls++
	return nil, nil
}

func TestContentAssistRejectsUnscopedKnowledgeBeforeSearchOrModel(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "omitted", body: `{"action":"rewrite","body":"正文","allow_external":true,"use_knowledge":true}`},
		{name: "empty", body: `{"action":"rewrite","body":"正文","allow_external":true,"use_knowledge":true,"knowledge_base_ids":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			backend := &recordingContentAssistBackend{}
			server.knowledgeService = knowledge.NewServiceWithBackend(server.db, backend)
			modelCalls := 0
			server.completeAI = func(context.Context, string, string) (string, error) {
				modelCalls++
				return "remote", nil
			}
			recorder := httptest.NewRecorder()
			server.handleAPIContentAssist(recorder, httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(test.body)))
			if recorder.Code != http.StatusBadRequest || backend.calls != 0 || modelCalls != 0 {
				t.Fatalf("status=%d backend=%d model=%d body=%s", recorder.Code, backend.calls, modelCalls, recorder.Body.String())
			}
		})
	}
}

func TestContentAssistStaysLocalWithoutExternalApproval(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	called := false
	server.completeAI = func(context.Context, string, string) (string, error) { called = true; return "remote", nil }
	req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(`{"action":"summary","body":"本地内容"}`))
	recorder := httptest.NewRecorder()
	server.handleAPIContentAssist(recorder, req)
	if recorder.Code != http.StatusOK || called {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["source"] != "local" {
		t.Fatalf("response=%v", response)
	}
}

func TestContentAssistLocalDraftUsesExplicitKnowledgeWithCitations(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	base, err := server.db.CreateKnowledgeBase("编导知识", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hook.md")
	if err := os.WriteFile(path, []byte("# 开场\n\n前三秒先说明观众能获得什么，再展示可核验依据。"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledge.NewImporter(server.db).Import(context.Background(), knowledge.ImportRequest{
		BaseID: base.ID, Files: []knowledge.InputFile{{Name: "hook.md", Path: path}},
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(
		`{"action":"script","title":"观众收益","body":"生成脚本","use_knowledge":true,"knowledge_base_ids":[1],"knowledge_query":"观众 获得"}`,
	))
	recorder := httptest.NewRecorder()
	server.handleAPIContentAssist(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "[KB:1]") ||
		!strings.Contains(recorder.Body.String(), `"chunk_id":1`) || !strings.Contains(recorder.Body.String(), `"external":false`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestContentAssistExternalRequiresExplicitFlag(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.completeAI = func(context.Context, string, string) (string, error) { return "模型结果", nil }
	req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(`{"action":"rewrite","body":"内容","allow_external":true}`))
	recorder := httptest.NewRecorder()
	server.handleAPIContentAssist(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "模型结果") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestContentAssistPlatformAdaptRequiresKnownPlatform(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing", body: `{"action":"platform_adapt","body":"内容"}`, want: "platform is required"},
		{name: "unknown", body: `{"action":"platform_adapt","body":"内容","platform":"not-real"}`, want: "unsupported platform"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(test.body))
			recorder := httptest.NewRecorder()
			server.handleAPIContentAssist(recorder, req)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), test.want) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestContentAssistPlatformAdaptUsesSelectedPlatform(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(`{"action":"platform_adapt","body":"内容","platform":"xhs"}`))
	recorder := httptest.NewRecorder()
	server.handleAPIContentAssist(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "（xhs）") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestContentAssistSupportsExplicitCreationWorkbenchStages(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for _, test := range []struct {
		action string
		want   string
	}{
		{action: "topic", want: "选题"},
		{action: "script", want: "编导"},
		{action: "storyboard", want: "分镜"},
	} {
		t.Run(test.action, func(t *testing.T) {
			var system string
			server.completeAI = func(_ context.Context, gotSystem, _ string) (string, error) {
				system = gotSystem
				return "阶段结果", nil
			}
			req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(
				`{"action":"`+test.action+`","title":"趋势主题","body":"来源内容","allow_external":true}`,
			))
			recorder := httptest.NewRecorder()
			server.handleAPIContentAssist(recorder, req)
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "阶段结果") || !strings.Contains(system, test.want) {
				t.Fatalf("status=%d system=%q body=%s", recorder.Code, system, recorder.Body.String())
			}
		})
	}
}

func TestContentReviewDecisionFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want string
	}{
		{name: "explicit pass", text: "审核结论：pass\n- 无阻塞问题", want: "pass"},
		{name: "bare pass line", text: "pass\n标题与正文一致", want: "pass"},
		{name: "explicit revise", text: "本地审核结论：revise\n- 数据缺少证据", want: "revise"},
		{name: "ambiguous decisions", text: "审核结论：pass\n另一个检查建议 revise", want: "unknown"},
		{name: "missing decision", text: "整体看起来可以，但仍需人工确认", want: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := contentReviewDecision(test.text); got != test.want {
				t.Fatalf("decision=%q want=%q text=%q", got, test.want, test.text)
			}
		})
	}
}

func TestContentAssistCheckReturnsStructuredReviewDecision(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for _, test := range []struct {
		name          string
		body          string
		modelResponse string
		want          string
	}{
		{name: "local revise", body: `{"action":"check","title":"草稿","body":"需要审核"}`, want: "revise"},
		{name: "configured pass", body: `{"action":"check","title":"草稿","body":"已经核验","allow_external":true}`, modelResponse: "审核结论：pass\n- 无阻塞问题", want: "pass"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server.completeAI = func(context.Context, string, string) (string, error) { return test.modelResponse, nil }
			recorder := httptest.NewRecorder()
			server.handleAPIContentAssist(recorder, httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(test.body)))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				ReviewDecision string `json:"review_decision"`
			}
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.ReviewDecision != test.want {
				t.Fatalf("decision=%q want=%q", response.ReviewDecision, test.want)
			}
		})
	}
}

func TestContentAssistMetricsReviewUsesOnlyExplicitConfiguredModelRequest(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var system, prompt string
	server.completeAI = func(_ context.Context, gotSystem, gotPrompt string) (string, error) {
		system, prompt = gotSystem, gotPrompt
		return "模型复盘结果", nil
	}
	req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(
		`{"action":"metrics_review","title":"浏览器1近7天复盘","body":"{\"snapshots\":[{\"platform_code\":\"douyin\",\"views\":1200}]}","allow_external":true}`,
	))
	recorder := httptest.NewRecorder()
	server.handleAPIContentAssist(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "模型复盘结果") ||
		!strings.Contains(system, "只分析提供的已验证指标") || !strings.Contains(prompt, `"views":1200`) {
		t.Fatalf("status=%d system=%q prompt=%q body=%s", recorder.Code, system, prompt, recorder.Body.String())
	}
}

func TestContentAssistUsesSelectedKnowledgeBaseLimits(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	base, err := server.db.CreateKnowledgeBase("创作知识", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.UpdateKnowledgeBase(base.ID, base.Name, base.Description, 1, 1000); err != nil {
		t.Fatal(err)
	}
	files := make([]knowledge.InputFile, 0, 2)
	for _, name := range []string{"甲.md", "乙.md"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte("# 编导原则\n\n前三秒建立冲突，并给出具体证据。来源："+name), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, knowledge.InputFile{Name: name, Path: path})
	}
	if _, err := knowledge.NewImporter(server.db).Import(context.Background(), knowledge.ImportRequest{BaseID: base.ID, Files: files}); err != nil {
		t.Fatal(err)
	}
	var modelPrompt string
	server.completeAI = func(_ context.Context, _, prompt string) (string, error) {
		modelPrompt = prompt
		return "结果", nil
	}
	req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(
		`{"action":"script","title":"主题","body":"正文","allow_external":true,"use_knowledge":true,"knowledge_base_ids":[1],"knowledge_query":"前三秒建立冲突"}`,
	))
	recorder := httptest.NewRecorder()
	server.handleAPIContentAssist(recorder, req)
	if recorder.Code != http.StatusOK || strings.Count(modelPrompt, "\n[KB:") != 1 || strings.Contains(recorder.Body.String(), `"chunk_id"`) {
		t.Fatalf("status=%d citations=%d prompt=%q", recorder.Code, strings.Count(modelPrompt, "\n[KB:"), modelPrompt)
	}
}

func TestContentAssistReturnsOnlyCitationsTheModelActuallyUses(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	base, err := server.db.CreateKnowledgeBase("引用库", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.md")
	if err := os.WriteFile(path, []byte("# 编导\n\n前三秒说明用户收益。"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledge.NewImporter(server.db).Import(context.Background(), knowledge.ImportRequest{
		BaseID: base.ID, Files: []knowledge.InputFile{{Name: "source.md", Path: path}},
	}); err != nil {
		t.Fatal(err)
	}
	server.completeAI = func(_ context.Context, _, _ string) (string, error) {
		return "脚本引用 [KB:1]", nil
	}
	req := httptest.NewRequest(http.MethodPost, "/api/content-assist", bytes.NewBufferString(
		`{"action":"script","title":"主题","body":"正文","allow_external":true,"use_knowledge":true,"knowledge_base_ids":[1],"knowledge_query":"用户收益"}`,
	))
	recorder := httptest.NewRecorder()
	server.handleAPIContentAssist(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"chunk_id":1`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
