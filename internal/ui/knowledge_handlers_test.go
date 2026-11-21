package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestKnowledgeHTTPMixedImportSearchAndCitedAssist(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	create := httptest.NewRecorder()
	server.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/knowledge/bases",
		bytes.NewBufferString(`{"name":"品牌知识","description":"创作规范"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var base struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(create.Body).Decode(&base); err != nil || base.ID <= 0 {
		t.Fatalf("base=%+v err=%v", base, err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("base_id", "1"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("source_name", "编导/voice.md"); err != nil {
		t.Fatal(err)
	}
	good, err := writer.CreateFormFile("files", "voice.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := good.Write([]byte("# 品牌语气\n\n内容自然可信，不夸张承诺。")); err != nil {
		t.Fatal(err)
	}
	bad, err := writer.CreateFormFile("files", "broken.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Write([]byte("broken")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	importRequest := httptest.NewRequest(http.MethodPost, "/api/knowledge/imports", &body)
	importRequest.Header.Set("Content-Type", writer.FormDataContentType())
	importResponse := httptest.NewRecorder()
	server.ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusOK ||
		!strings.Contains(importResponse.Body.String(), `"imported":1`) ||
		!strings.Contains(importResponse.Body.String(), `"failed":1`) ||
		!strings.Contains(importResponse.Body.String(), `"run_id":`) {
		t.Fatalf("import status=%d body=%s", importResponse.Code, importResponse.Body.String())
	}
	basesResponse := httptest.NewRecorder()
	server.ServeHTTP(basesResponse, httptest.NewRequest(http.MethodGet, "/api/knowledge/bases", nil))
	if basesResponse.Code != http.StatusOK ||
		!strings.Contains(basesResponse.Body.String(), `"document_count":1`) ||
		!strings.Contains(basesResponse.Body.String(), `"chunk_count":1`) ||
		!strings.Contains(basesResponse.Body.String(), `"latest_import":{"id":1`) {
		t.Fatalf("base summary status=%d body=%s", basesResponse.Code, basesResponse.Body.String())
	}
	runResponse := httptest.NewRecorder()
	server.ServeHTTP(runResponse, httptest.NewRequest(http.MethodGet, "/api/knowledge/imports/1", nil))
	if runResponse.Code != http.StatusOK ||
		!strings.Contains(runResponse.Body.String(), `"status":"completed_with_errors"`) ||
		!strings.Contains(runResponse.Body.String(), `"failed_files":1`) {
		t.Fatalf("run status=%d body=%s", runResponse.Code, runResponse.Body.String())
	}
	documentsResponse := httptest.NewRecorder()
	server.ServeHTTP(documentsResponse, httptest.NewRequest(http.MethodGet, "/api/knowledge/documents?base_id=1", nil))
	if documentsResponse.Code != http.StatusOK ||
		!strings.Contains(documentsResponse.Body.String(), "编导/voice.md") ||
		!strings.Contains(documentsResponse.Body.String(), "broken.pdf") {
		t.Fatalf("documents status=%d body=%s", documentsResponse.Code, documentsResponse.Body.String())
	}

	searchResponse := httptest.NewRecorder()
	server.ServeHTTP(searchResponse, httptest.NewRequest(http.MethodPost, "/api/knowledge/search",
		bytes.NewBufferString(`{"base_ids":[1],"query":"自然可信","top_k":6}`)))
	if searchResponse.Code != http.StatusOK || !strings.Contains(searchResponse.Body.String(), "voice.md") {
		t.Fatalf("search status=%d body=%s", searchResponse.Code, searchResponse.Body.String())
	}

	var modelPrompt string
	server.completeAI = func(_ context.Context, _, prompt string) (string, error) {
		modelPrompt = prompt
		return "引用知识后的草稿 [KB:1]", nil
	}
	assistResponse := httptest.NewRecorder()
	server.ServeHTTP(assistResponse, httptest.NewRequest(http.MethodPost, "/api/content-assist",
		bytes.NewBufferString(`{"action":"rewrite","body":"请改写","allow_external":true,"use_knowledge":true,"knowledge_base_ids":[1],"knowledge_query":"自然可信"}`)))
	if assistResponse.Code != http.StatusOK ||
		!strings.Contains(assistResponse.Body.String(), `"chunk_id"`) ||
		!strings.Contains(modelPrompt, "[KB:") ||
		!strings.Contains(modelPrompt, "自然可信") {
		t.Fatalf("assist status=%d body=%s prompt=%q", assistResponse.Code, assistResponse.Body.String(), modelPrompt)
	}
}

func TestAlphaNotesPresetImportsOnlyReviewedLocalKnowledgeTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, alphaNotesKnowledgeRelativePath)
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("编导知识库/前三秒.md", "# 前三秒\n\n用冲突建立观看理由。")
	write("node_modules/依赖.md", "不得导入")
	write("账号与Key/平台token.md", "不得导入")

	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	defaultBase, err := server.db.CreateKnowledgeBase("普通资料", "手动文件和文件夹导入")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/knowledge/presets/alpha-notes/import", bytes.NewBufferString(`{"base_id":0}`)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"imported":1`) ||
		!strings.Contains(response.Body.String(), `"name":"Alpha-Notes 编导与短视频"`) ||
		strings.Contains(response.Body.String(), "平台token") || strings.Contains(response.Body.String(), "依赖.md") {
		t.Fatalf("preset import status=%d body=%s", response.Code, response.Body.String())
	}
	items, err := server.db.ListKnowledgeBases()
	if err != nil || len(items) != 2 {
		t.Fatalf("knowledge bases=%+v err=%v", items, err)
	}
	for _, item := range items {
		if item.ID == defaultBase.ID && item.DocumentCount != 0 {
			t.Fatalf("Alpha-Notes leaked into selected/default base: %+v", item)
		}
	}
	search := httptest.NewRecorder()
	server.ServeHTTP(search, httptest.NewRequest(http.MethodPost, "/api/knowledge/search", bytes.NewBufferString(`{"query":"观看理由","top_k":6}`)))
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), "前三秒.md") {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}
}

func TestKnowledgeBaseUpdateAndDeleteKeepAPIConsistent(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	base, err := server.db.CreateKnowledgeBase("旧名称", "")
	if err != nil {
		t.Fatal(err)
	}
	update := httptest.NewRecorder()
	server.ServeHTTP(update, httptest.NewRequest(http.MethodPatch, "/api/knowledge/bases/1",
		bytes.NewBufferString(`{"name":"新名称","description":"更新说明","top_k":8,"max_context_chars":9000}`)))
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"name":"新名称"`) {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
	remove := httptest.NewRecorder()
	server.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/api/knowledge/bases/1", nil))
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}
	if _, err := server.db.GetKnowledgeBase(base.ID); err == nil {
		t.Fatal("knowledge base remained after delete")
	}
}

func TestKnowledgeImportRejectsOversizedRequest(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/knowledge/imports", strings.NewReader(strings.Repeat("x", (64<<20)+1)))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge && response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
