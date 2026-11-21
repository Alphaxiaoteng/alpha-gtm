package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/agentcfg"
	"alpha-nexus-egolite/internal/llm"
)

type fakeArkResponsesClient struct {
	reply string
	err   error
	input string
}

func (f *fakeArkResponsesClient) Complete(_ context.Context, input string) (string, error) {
	f.input = input
	return f.reply, f.err
}

func saveVerifiedModels(t *testing.T, defaultModel string, models ...string) string {
	t.Helper()
	dataDir := t.TempDir()
	entries := make([]agentcfg.ModelEntry, 0, len(models))
	validatedAt := time.Now().UTC().Format(time.RFC3339)
	for _, model := range models {
		entries = append(entries, agentcfg.ModelEntry{
			ID:          model,
			Enabled:     true,
			ValidatedAt: validatedAt,
		})
	}
	if err := agentcfg.Save(dataDir, &agentcfg.Config{
		DefaultModel: defaultModel,
		Models:       entries,
	}); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

func replaceArkResponsesFactory(t *testing.T, replacement arkResponsesFactory) {
	t.Helper()
	previous := newArkResponsesClient
	newArkResponsesClient = replacement
	t.Cleanup(func() { newArkResponsesClient = previous })
}

func TestGatewayFailurePresentationUsesDoubaoMessage(t *testing.T) {
	message, code, ok := presentGatewayFailure(`{"error":{"message":"<urlopen error [Errno 61] Connection refused>"}}`)
	if !ok || code != "DOUBAO_API_UNREACHABLE" {
		t.Fatalf("message=%q code=%q ok=%v", message, code, ok)
	}
	if !strings.Contains(message, "豆包 API 暂时不可达") || strings.Contains(message, "本地模型网关") {
		t.Fatalf("message=%q", message)
	}
}

func TestGatewayFailurePresentationLeavesNormalReplyUntouched(t *testing.T) {
	if _, _, ok := presentGatewayFailure("账号状态正常，可以继续发布前检查。"); ok {
		t.Fatal("normal assistant reply must not be treated as a gateway failure")
	}
}

func TestChatWithHistoryRejectsUnverifiedModelBeforeCreatingClient(t *testing.T) {
	dataDir := saveVerifiedModels(
		t,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed21TurboModelID,
	)
	called := false
	replaceArkResponsesFactory(t, func(string, time.Duration) (arkResponsesCompleter, error) {
		called = true
		return &fakeArkResponsesClient{reply: "must not run"}, nil
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message: "hello",
		Model:   agentcfg.DoubaoSeed21ProModelID,
		DataDir: dataDir,
	}, nil)
	if err == nil || result == nil || result.Error != "AGENT_MODEL_NOT_VERIFIED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if called {
		t.Fatal("unverified model must fail before an Ark client is created")
	}
	if !strings.Contains(result.Reply, "测试成功") {
		t.Fatalf("reply=%q", result.Reply)
	}
}

func TestChatWithHistoryRequiresVerifiedDefaultModel(t *testing.T) {
	called := false
	replaceArkResponsesFactory(t, func(string, time.Duration) (arkResponsesCompleter, error) {
		called = true
		return &fakeArkResponsesClient{reply: "must not run"}, nil
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message: "hello",
		DataDir: t.TempDir(),
	}, nil)
	if err == nil || result == nil || result.Error != "AGENT_MODEL_NOT_VERIFIED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if called {
		t.Fatal("missing verified default must fail before an Ark client is created")
	}
}

func TestChatWithHistoryUsesVerifiedDefaultAndBuildsOneResponsesInput(t *testing.T) {
	dataDir := saveVerifiedModels(
		t,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed21TurboModelID,
	)
	client := &fakeArkResponsesClient{reply: "mock Ark reply"}
	var gotModel string
	var gotTimeout time.Duration
	replaceArkResponsesFactory(t, func(model string, timeout time.Duration) (arkResponsesCompleter, error) {
		gotModel = model
		gotTimeout = timeout
		return client, nil
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message:      "current question",
		DataDir:      dataDir,
		SystemAppend: "system policy",
		Timeout:      3 * time.Second,
		Files:        []string{"/tmp/reference.png"},
	}, []ChatTurn{
		{Role: "user", Content: "earlier question"},
		{Role: "assistant", Content: "earlier answer"},
		{Role: "invalid", Content: "must be omitted"},
	})
	if err != nil || result == nil || !result.OK || result.Reply != "mock Ark reply" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Engine != "doubao_api" || result.Model != agentcfg.DoubaoSeed21TurboModelID {
		t.Fatalf("result=%+v", result)
	}
	if gotModel != agentcfg.DoubaoSeed21TurboModelID || gotTimeout != 3*time.Second {
		t.Fatalf("model=%q timeout=%s", gotModel, gotTimeout)
	}
	for _, want := range []string{
		"[System]\nsystem policy",
		"[Conversation]",
		"User:\nearlier question",
		"Assistant:\nearlier answer",
		"[Current user]\ncurrent question",
		"[Attached files]\n- reference.png",
	} {
		if !strings.Contains(client.input, want) {
			t.Fatalf("input missing %q:\n%s", want, client.input)
		}
	}
	if strings.Contains(client.input, "must be omitted") {
		t.Fatalf("invalid history role survived:\n%s", client.input)
	}
	if strings.Contains(client.input, "/tmp/") {
		t.Fatalf("local path leaked to model input:\n%s", client.input)
	}
}

func TestChatWithHistoryAllowsRequestedVerifiedModel(t *testing.T) {
	dataDir := saveVerifiedModels(
		t,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed20LiteModelID,
	)
	var gotModel string
	replaceArkResponsesFactory(t, func(model string, _ time.Duration) (arkResponsesCompleter, error) {
		gotModel = model
		return &fakeArkResponsesClient{reply: "lite reply"}, nil
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message: "hello",
		Model:   agentcfg.DoubaoSeed20LiteModelID,
		DataDir: dataDir,
	}, nil)
	if err != nil || result == nil || result.Model != agentcfg.DoubaoSeed20LiteModelID ||
		gotModel != agentcfg.DoubaoSeed20LiteModelID {
		t.Fatalf("result=%+v model=%q err=%v", result, gotModel, err)
	}
}

func TestChatWithHistoryRequiresConfiguredArkCredential(t *testing.T) {
	dataDir := saveVerifiedModels(
		t,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed21TurboModelID,
	)
	replaceArkResponsesFactory(t, func(string, time.Duration) (arkResponsesCompleter, error) {
		return nil, &llm.ArkResponsesError{Code: llm.ArkResponsesNotConfigured}
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message: "hello",
		DataDir: dataDir,
	}, nil)
	if err == nil || result == nil || result.Error != "AGENT_API_NOT_CONFIGURED" ||
		!strings.Contains(result.Reply, "Agent API 尚未配置") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestChatWithHistoryDoesNotExposeArkHTTPPayload(t *testing.T) {
	dataDir := saveVerifiedModels(
		t,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed21TurboModelID,
	)
	secretBody := "sensitive upstream detail"
	replaceArkResponsesFactory(t, func(string, time.Duration) (arkResponsesCompleter, error) {
		return &fakeArkResponsesClient{
			err: &llm.ArkResponsesError{
				Code:       llm.ArkResponsesHTTPFailed,
				StatusCode: 401,
			},
		}, nil
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message: secretBody,
		DataDir: dataDir,
	}, nil)
	if err == nil || result == nil || result.Error != "DOUBAO_API_AUTH_FAILED" ||
		strings.Contains(result.Reply, secretBody) || strings.Contains(err.Error(), secretBody) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestChatWithHistoryMapsArkTransportFailureToSafeExistingCode(t *testing.T) {
	dataDir := saveVerifiedModels(
		t,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed21TurboModelID,
	)
	replaceArkResponsesFactory(t, func(string, time.Duration) (arkResponsesCompleter, error) {
		return &fakeArkResponsesClient{
			err: &llm.ArkResponsesError{Code: llm.ArkResponsesTransportFailed},
		}, nil
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message: "hello",
		DataDir: dataDir,
	}, nil)
	if err == nil || result == nil || result.Error != "DOUBAO_API_UNREACHABLE" ||
		!strings.Contains(result.Reply, "暂时不可达") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestChatWithHistoryDoesNotTreatNormalArkReplyAsError(t *testing.T) {
	dataDir := saveVerifiedModels(
		t,
		agentcfg.DoubaoSeed21TurboModelID,
		agentcfg.DoubaoSeed21TurboModelID,
	)
	replaceArkResponsesFactory(t, func(string, time.Duration) (arkResponsesCompleter, error) {
		return &fakeArkResponsesClient{reply: "账号状态正常，可以继续发布前检查。"}, nil
	})

	result, err := ChatWithHistory(context.Background(), ChatRequest{
		Message: "hello",
		DataDir: dataDir,
	}, nil)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestArkResponsesFailureMappingKeepsExistingSafeCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "auth", err: &llm.ArkResponsesError{Code: llm.ArkResponsesHTTPFailed, StatusCode: 403}, code: "DOUBAO_API_AUTH_FAILED"},
		{name: "rate limit", err: &llm.ArkResponsesError{Code: llm.ArkResponsesHTTPFailed, StatusCode: 429}, code: "DOUBAO_API_RATE_LIMITED"},
		{name: "unavailable", err: &llm.ArkResponsesError{Code: llm.ArkResponsesHTTPFailed, StatusCode: 503}, code: "DOUBAO_API_UNAVAILABLE"},
		{name: "invalid output", err: &llm.ArkResponsesError{Code: llm.ArkResponsesInvalidResponse}, code: "DOUBAO_API_ERROR"},
		{name: "unknown", err: errors.New("opaque failure"), code: "DOUBAO_API_ERROR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, code := arkResponsesFailure(test.err)
			if code != test.code {
				t.Fatalf("code=%q want=%q", code, test.code)
			}
		})
	}
}
