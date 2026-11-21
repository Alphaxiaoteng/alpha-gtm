package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

type agentHubTestRunner struct{}

func TestAgentKnowledgePromptForEmptyEvidenceForbidsFakeCitation(t *testing.T) {
	prompt := agentKnowledgePromptContext("")
	for _, required := range []string{"未检索到可引用证据", "创作建议", "不得伪造引用"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("empty evidence prompt %q missing %q", prompt, required)
		}
	}
}

func (agentHubTestRunner) Run(_ context.Context, _ agenthub.RunRequest, emit func(agenthub.ProviderEvent)) (string, error) {
	emit(agenthub.ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": "offline reply"}})
	return "test-thread", nil
}

type agentHubCitedTestRunner struct{ reply string }

func (runner *agentHubCitedTestRunner) Run(_ context.Context, _ agenthub.RunRequest, emit func(agenthub.ProviderEvent)) (string, error) {
	emit(agenthub.ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": runner.reply}})
	return "test-thread-cited", nil
}

func TestAgentHubRoutesPersistConversationAndReplayEvents(t *testing.T) {
	server := testAgentHubServer(t)
	providers := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/providers", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(providers, request)
	if providers.Code != http.StatusOK || !strings.Contains(providers.Body.String(), "codex") {
		t.Fatalf("providers=%d body=%s", providers.Code, providers.Body.String())
	}

	body := []byte(`{"provider_id":"codex","model_id":"gpt-5.6-sol","context":{"workspace":"浏览器管理","page":"accounts","selections":[{"id":"account_1","name":"Demo"}]}}`)
	created := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agent/conversations", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var conversation agenthub.Conversation
	if err := json.Unmarshal(created.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}

	runRecorder := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agent/conversations/"+conversation.ID+"/runs", strings.NewReader(`{"model_id":"gpt-5.6-sol","message":"hello","context":{"workspace":"浏览器管理","page":"accounts"}}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(runRecorder, request)
	if runRecorder.Code != http.StatusAccepted {
		t.Fatalf("run=%d body=%s", runRecorder.Code, runRecorder.Body.String())
	}
	var run agenthub.Run
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitForAgentHubRun(t, server, run.ID)

	events := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/runs/"+run.ID+"/events", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(events, request)
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), "message.completed") || !strings.Contains(events.Body.String(), "run.completed") {
		t.Fatalf("events=%d body=%s", events.Code, events.Body.String())
	}
	activities := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/runs/"+run.ID+"/activities", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(activities, request)
	var activityBody struct {
		Activities []agenthub.AgentActivity `json:"activities"`
	}
	if activities.Code != http.StatusOK || json.Unmarshal(activities.Body.Bytes(), &activityBody) != nil || len(activityBody.Activities) == 0 {
		t.Fatalf("activities=%d body=%s", activities.Code, activities.Body.String())
	}
	activity := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/agent/activities/%d", activityBody.Activities[0].ID), nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(activity, request)
	if activity.Code != http.StatusOK || strings.Contains(activity.Body.String(), "payload") || !strings.Contains(activity.Body.String(), `"runId":"`+run.ID+`"`) {
		t.Fatalf("activity=%d body=%s", activity.Code, activity.Body.String())
	}
}

func TestAgentHubKnowledgeScopesPersistCitationsWithTheSharedRun(t *testing.T) {
	server := testAgentHubServer(t)
	base, err := server.db.CreateKnowledgeBase("创作范围测试", "")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := server.db.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "品牌/语气.md", SourceType: "markdown",
		SHA256: strings.Repeat("a", 64), Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.db.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "真实创作证据"}}); err != nil {
		t.Fatal(err)
	}

	created := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agent/conversations", strings.NewReader(`{"provider_id":"codex","model_id":"gpt-5.6-sol","title":"创作工作台","context":{"page":"创作工作台"}}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var conversation agenthub.Conversation
	if err := json.Unmarshal(created.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"model_id":"gpt-5.6-sol","message":"真实创作证据","context":{"page":"创作工作台"},"knowledge_scopes":[{"base_id":%d,"source_prefix":"品牌"}]}`, base.ID)
	runRecorder := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agent/conversations/"+conversation.ID+"/runs", strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(runRecorder, request)
	if runRecorder.Code != http.StatusAccepted {
		t.Fatalf("run=%d body=%s", runRecorder.Code, runRecorder.Body.String())
	}
	var run agenthub.Run
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitForAgentHubRun(t, server, run.ID)

	audit := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/conversations/"+conversation.ID+"/knowledge-turns", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(audit, request)
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), `"source_name":"品牌/语气.md"`) || !strings.Contains(audit.Body.String(), `"output":"offline reply"`) || !strings.Contains(audit.Body.String(), `"used_citations":[]`) {
		t.Fatalf("audit=%d body=%s", audit.Code, audit.Body.String())
	}
}

func TestAgentHubKnowledgeTurnsMarkOnlyCitationsUsedInActualOutput(t *testing.T) {
	runner := &agentHubCitedTestRunner{}
	server := testAgentHubServerWithRunner(t, runner)
	base, err := server.db.CreateKnowledgeBase("引用回读测试", "")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := server.db.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "事实/品牌.md", SourceType: "markdown",
		SHA256: strings.Repeat("b", 64), Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.db.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "必须保留的品牌事实"}}); err != nil {
		t.Fatal(err)
	}
	results, err := server.db.SearchKnowledgeChunks([]int64{base.ID}, "品牌事实", 1)
	if err != nil || len(results) != 1 || results[0].ChunkID <= 0 {
		t.Fatalf("knowledge results=%+v err=%v", results, err)
	}

	// The runner has no access to the retrieval layer. It emits one exact citation
	// marker, which proves the persisted Agent turn derives used_citations from
	// the finalized assistant output rather than from the retrieval list alone.
	runner.reply = fmt.Sprintf("根据资料整理草稿 [KB:%d]；其余内容是创作建议。", results[0].ChunkID)
	created := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agent/conversations", strings.NewReader(`{"provider_id":"codex","model_id":"gpt-5.6-sol","title":"引用草稿","context":{"page":"创作工作台"}}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var conversation agenthub.Conversation
	if err := json.Unmarshal(created.Body.Bytes(), &conversation); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"model_id":"gpt-5.6-sol","message":"品牌事实","context":{"page":"创作工作台"},"knowledge_scopes":[{"base_id":%d,"source_prefix":"事实"}]}`, base.ID)
	runRecorder := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agent/conversations/"+conversation.ID+"/runs", strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(runRecorder, request)
	if runRecorder.Code != http.StatusAccepted {
		t.Fatalf("run=%d body=%s", runRecorder.Code, runRecorder.Body.String())
	}
	var run agenthub.Run
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	waitForAgentHubRun(t, server, run.ID)

	audit := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/conversations/"+conversation.ID+"/knowledge-turns", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(audit, request)
	if audit.Code != http.StatusOK ||
		!strings.Contains(audit.Body.String(), fmt.Sprintf(`"chunk_id":%d`, results[0].ChunkID)) ||
		!strings.Contains(audit.Body.String(), `"used_citations":[{`) ||
		!strings.Contains(audit.Body.String(), `"output":"根据资料整理草稿 [KB:`) {
		t.Fatalf("audit=%d body=%s", audit.Code, audit.Body.String())
	}
}

func TestAgentHubRejectsUnsafeContextBeforeProviderRun(t *testing.T) {
	server := testAgentHubServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agent/conversations", strings.NewReader(`{"context":{"page":"/Users/albert/private"}}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "AGENT_CONTEXT_REJECTED") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAgentHubProviderConnectStartsOnlyTheProviderSurface(t *testing.T) {
	server, connected := testAgentHubServerWithConnect(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agent/providers/codex/connect", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || !*connected {
		t.Fatalf("connect=%d connected=%v body=%s", recorder.Code, *connected, recorder.Body.String())
	}
}

func TestAgentHubOptionalProviderCatalogRequiresRealModelCheckBeforeAdd(t *testing.T) {
	server := testAgentHubServer(t)
	catalog := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/provider-catalog", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(catalog, request)
	body := catalog.Body.String()
	if catalog.Code != http.StatusOK ||
		!strings.Contains(body, `"id":"claude"`) ||
		!strings.Contains(body, `"id":"kimi"`) ||
		!strings.Contains(body, `"id":"codebuddy"`) ||
		!strings.Contains(body, `"id":"antigravity"`) ||
		strings.Contains(body, `"id":"qwen"`) ||
		strings.Contains(body, `"id":"cursor"`) ||
		strings.Contains(body, `"id":"copilot"`) ||
		strings.Contains(body, `"id":"zcode"`) ||
		strings.Contains(body, `"id":"gemini"`) ||
		strings.Contains(body, `"supported":false`) {
		t.Fatalf("catalog=%d body=%s", catalog.Code, body)
	}

	added := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agent/providers", strings.NewReader(`{"provider_id":"kimi"}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(added, request)
	if added.Code != http.StatusBadRequest || !strings.Contains(added.Body.String(), "AGENT_MODEL_INVALID") {
		t.Fatalf("add=%d body=%s", added.Code, added.Body.String())
	}

	providers := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/providers", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(providers, request)
	if providers.Code != http.StatusOK || strings.Contains(providers.Body.String(), `"id":"kimi"`) {
		t.Fatalf("failed provider was activated: status=%d body=%s", providers.Code, providers.Body.String())
	}
}

func TestAgentHubDoesNotExposeSeparateExternalClients(t *testing.T) {
	server := testAgentHubServer(t)
	listed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/external-clients", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(listed, request)
	if listed.Code != http.StatusNotFound {
		t.Fatalf("external clients endpoint remains exposed: status=%d body=%s", listed.Code, listed.Body.String())
	}

	providers := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/providers", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(providers, request)
	if strings.Contains(providers.Body.String(), `"id":"workbuddy"`) ||
		strings.Contains(providers.Body.String(), `"id":"zcode"`) {
		t.Fatalf("external client leaked into right Agent providers: %s", providers.Body.String())
	}
}

func TestAgentHubProviderProbeCanTargetOneProvider(t *testing.T) {
	server := testAgentHubServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agent/providers/codex/probe", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), "probing") {
		t.Fatalf("probe=%d body=%s", recorder.Code, recorder.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for {
		cached := httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/api/agent/providers", nil)
		request.RemoteAddr = "127.0.0.1:17321"
		server.ServeHTTP(cached, request)
		if strings.Contains(cached.Body.String(), "gpt-5.6-sol") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cached probe did not complete: %s", cached.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAgentHubProviderModelValidationUpdatesVerifiedList(t *testing.T) {
	server := testAgentHubServer(t)
	probeRecorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agent/providers/codex/probe", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(probeRecorder, request)
	if probeRecorder.Code != http.StatusAccepted {
		t.Fatalf("probe=%d body=%s", probeRecorder.Code, probeRecorder.Body.String())
	}
	waitForAgentProviderProbe(t, server, agenthub.ProviderCodex)

	recorder := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agent/providers/codex/models/validate", strings.NewReader(`{"model_id":"gpt-5.6-sol"}`))
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), `"validating"`) {
		t.Fatalf("validate=%d body=%s", recorder.Code, recorder.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, probes := server.agentHub.Providers(context.Background())
		if len(probes) == 1 && probes[0].ValidatingModel == "" && len(probes[0].Models) == 1 {
			if probes[0].Models[0] != "gpt-5.6-sol" {
				t.Fatalf("verified models=%q", probes[0].Models)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, probes := server.agentHub.Providers(context.Background())
	t.Fatalf("verified model did not appear: %+v", probes)
}

func TestAgentHubProviderBatchModelValidationReturnsAcceptedAndProgress(t *testing.T) {
	server := testAgentHubServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/agent/providers/codex/models/validate-batch",
		strings.NewReader(`{"model_ids":["gpt-5.6-sol","gpt-5.6-sol"]}`),
	)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted ||
		!strings.Contains(recorder.Body.String(), `"total":1`) ||
		!strings.Contains(recorder.Body.String(), `"validating"`) {
		t.Fatalf("validate batch=%d body=%s", recorder.Code, recorder.Body.String())
	}
	waitForAgentProviderModelValidation(t, server, agenthub.ProviderCodex)
	_, probes := server.agentHub.Providers(context.Background())
	if len(probes) != 1 || probes[0].ModelValidationTotal != 1 ||
		probes[0].ModelValidationCompleted != 1 || probes[0].ModelValidationPassed != 1 {
		t.Fatalf("batch progress=%+v", probes)
	}
}

func TestAgentHubBootstrapAndProviderFilteredConversations(t *testing.T) {
	server := testAgentHubServer(t)
	for _, provider := range []string{"codex", "codex"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/agent/conversations", strings.NewReader(`{"provider_id":"`+provider+`","model_id":"gpt-5.6-sol","context":{}}`))
		request.RemoteAddr = "127.0.0.1:17321"
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/bootstrap?provider_id=codex", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.Count(recorder.Body.String(), `"provider_id":"codex"`) < 3 {
		t.Fatalf("bootstrap=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAgentHubWorkspaceModeFiltersAndRestoresIndependentSelection(t *testing.T) {
	server := testAgentHubServer(t)
	create := func(mode, title string) agenthub.Conversation {
		t.Helper()
		recorder := httptest.NewRecorder()
		body := fmt.Sprintf(`{"provider_id":"codex","model_id":"gpt-5.6-sol","title":%q,"context":{"product_mode":%q,"page":"contents"}}`, title, mode)
		request := httptest.NewRequest(http.MethodPost, "/api/agent/conversations", strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:17321"
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create %s=%d body=%s", mode, recorder.Code, recorder.Body.String())
		}
		var conversation agenthub.Conversation
		if err := json.Unmarshal(recorder.Body.Bytes(), &conversation); err != nil {
			t.Fatal(err)
		}
		return conversation
	}
	media := create("media", "Media only")
	shop := create("shop", "Shop only")

	selection := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/agent/providers/codex/selection", strings.NewReader(fmt.Sprintf(`{"conversation_id":%q,"model_id":"gpt-5.6-sol","draft":"shop draft","workspace_mode":"shop","last_module":"knowledge"}`, shop.ID)))
	request.SetPathValue("id", "codex")
	request.RemoteAddr = "127.0.0.1:17321"
	server.handleAPIAgentProviderSelection(selection, request)
	if selection.Code != http.StatusOK {
		t.Fatalf("selection=%d body=%s", selection.Code, selection.Body.String())
	}

	for _, test := range []struct {
		mode, want, absent string
	}{{"media", media.ID, shop.ID}, {"shop", shop.ID, media.ID}} {
		recorder := httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/api/agent/conversations?provider_id=codex&workspace_mode="+test.mode, nil)
		request.RemoteAddr = "127.0.0.1:17321"
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), test.want) || strings.Contains(recorder.Body.String(), test.absent) {
			t.Fatalf("mode %s=%d body=%s", test.mode, recorder.Code, recorder.Body.String())
		}
	}

	bootstrap := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/bootstrap?provider_id=codex&workspace_mode=shop", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(bootstrap, request)
	if bootstrap.Code != http.StatusOK || !strings.Contains(bootstrap.Body.String(), `"conversation_id":"`+shop.ID+`"`) || !strings.Contains(bootstrap.Body.String(), `"draft":"shop draft"`) || !strings.Contains(bootstrap.Body.String(), `"last_module":"knowledge"`) {
		t.Fatalf("bootstrap=%d body=%s", bootstrap.Code, bootstrap.Body.String())
	}

	invalid := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/agent/conversations?workspace_mode=other", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "AGENT_WORKSPACE_MODE_INVALID") {
		t.Fatalf("invalid=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestAgentHubRunCapacityErrorMapping(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeAgentHubError(recorder, agenthub.ErrRunCapacity)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("capacity status=%d, want 409", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "AGENT_RUN_CAPACITY") || strings.Contains(body, "AGENT_RUN_ACTIVE") {
		t.Fatalf("capacity body=%s", body)
	}
}

func TestAgentHubRunActiveErrorMappingStillConflict(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeAgentHubError(recorder, agenthub.ErrRunActive)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("active status=%d, want 409", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "AGENT_RUN_ACTIVE") {
		t.Fatalf("active body=%s", body)
	}
}

func testAgentHubServer(t *testing.T) *Server {
	return testAgentHubServerWithRunner(t, agentHubTestRunner{})
}

func testAgentHubServerWithRunner(t *testing.T, runner agenthub.Runner) *Server {
	t.Helper()
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	registry, err := agenthub.NewRegistry(agenthub.Provider{
		Descriptor: agenthub.ProviderDescriptor{ID: agenthub.ProviderCodex, Label: "Codex"},
		Probe: func(context.Context) agenthub.ProviderProbe {
			return agenthub.ProviderProbe{Available: true, Ready: true}
		},
		ModelProbe: func(context.Context) agenthub.ProviderProbe {
			return agenthub.ProviderProbe{Available: true, Ready: true, ModelCandidates: []string{"gpt-5.6-sol"}}
		},
		ValidateModel: func(context.Context, string, string, string) error { return nil },
		Runner:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	hub, err := agenthub.NewService(agenthub.NewStore(server.db), registry, server.db.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.ProbeModel(context.Background(), agenthub.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	if err := hub.BeginModelValidation(agenthub.ProviderCodex, "gpt-5.6-sol"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, probes := hub.Providers(context.Background())
		if len(probes) == 1 && probes[0].ValidatingModel == "" && len(probes[0].Models) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("test model validation did not finish: %+v", probes)
		}
		time.Sleep(5 * time.Millisecond)
	}
	server.agentHub = hub
	return server
}

func testAgentHubServerWithConnect(t *testing.T) (*Server, *bool) {
	t.Helper()
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	connected := false
	registry, err := agenthub.NewRegistry(agenthub.Provider{
		Descriptor: agenthub.ProviderDescriptor{ID: agenthub.ProviderCodex, Label: "Codex"},
		Probe: func(context.Context) agenthub.ProviderProbe {
			return agenthub.ProviderProbe{Available: true, Ready: false}
		},
		Runner: agentHubTestRunner{},
		Connect: func(context.Context) error {
			connected = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hub, err := agenthub.NewService(agenthub.NewStore(server.db), registry, server.db.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	server.agentHub = hub
	return server, &connected
}

func waitForAgentHubRun(t *testing.T, server *Server, runID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, err := server.agentHub.GetRun(context.Background(), runID)
		if err == nil && run.Status == "completed" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("agent run %s did not complete", runID)
}

func waitForAgentProviderProbe(t *testing.T, server *Server, providerID agenthub.ProviderID) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, probes := server.agentHub.Providers(context.Background())
		if len(probes) == 1 && probes[0].ProviderID == providerID && probes[0].State != agenthub.ProviderStateProbing {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("provider %s probe did not finish", providerID)
}

func waitForAgentProviderModelValidation(t *testing.T, server *Server, providerID agenthub.ProviderID) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, probes := server.agentHub.Providers(context.Background())
		if len(probes) == 1 && probes[0].ProviderID == providerID && probes[0].ValidatingModel == "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("provider %s model validation did not finish", providerID)
}

func TestAgentConversationDeleteRemovesMessages(t *testing.T) {
	server := testAgentHubServer(t)
	ctx := context.Background()

	conversation, err := server.agentHub.CreateConversation(ctx, agenthub.ProviderCodex, "删除测试", "gpt-5.6-sol", agenthub.ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.agentHub.StartRun(ctx, conversation.ID, agenthub.ProviderCodex, "gpt-5.6-sol", "hi", agenthub.ContextSnapshot{}); err != nil {
		t.Fatal(err)
	}
	// Wait for the run to settle so the delete is not blocked by an active turn.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var running int
		if scanErr := server.db.QueryRow(`SELECT COUNT(*) FROM agent_runs WHERE conversation_id=? AND status='running'`, conversation.ID).Scan(&running); scanErr != nil {
			t.Fatal(scanErr)
		}
		if running == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run did not settle")
		}
		time.Sleep(5 * time.Millisecond)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/agent/conversations/"+conversation.ID, nil)
	request.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// Conversation and its messages are both gone (FK cascade).
	if _, err := server.agentHub.GetConversation(ctx, conversation.ID); err == nil {
		t.Fatal("conversation still readable after delete")
	}
	var messages int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM agent_messages WHERE conversation_id=?`, conversation.ID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 0 {
		t.Fatalf("orphan messages = %d", messages)
	}

	// Second delete is a clean 404.
	again := httptest.NewRecorder()
	request2 := httptest.NewRequest(http.MethodDelete, "/api/agent/conversations/"+conversation.ID, nil)
	request2.RemoteAddr = "127.0.0.1:17321"
	server.ServeHTTP(again, request2)
	if again.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d body=%s", again.Code, again.Body.String())
	}
}
