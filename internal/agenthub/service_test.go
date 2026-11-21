package agenthub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

type scriptedRunner struct {
	events []ProviderEvent
	block  bool
}

func (r scriptedRunner) Run(ctx context.Context, _ RunRequest, emit func(ProviderEvent)) (string, error) {
	for _, event := range r.events {
		emit(event)
	}
	if r.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "thread_test", nil
}

type captureRunner struct{ request chan RunRequest }

func (r captureRunner) Run(_ context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	r.request <- request
	emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": "done"}})
	return "thread_skill", nil
}

type blockingTextTaskRunner struct {
	started chan RunRequest
	release <-chan struct{}
}

func (r blockingTextTaskRunner) Run(_ context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	r.started <- request
	<-r.release
	emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": "done"}})
	return "thread_temporary", nil
}

type testPromptExpander struct{}

const testVerifiedModel = "test/model"

func (testPromptExpander) ExpandPrompt(_ context.Context, message string) (string, []string, error) {
	return "skill instructions\n\n" + message, []string{"caption-check"}, nil
}

func setupService(t *testing.T, runner Runner) *Service {
	return setupServiceWithBrokerSupport(t, runner, false)
}

func setupServiceWithBrokerSupport(t *testing.T, runner Runner, supportsBroker bool) *Service {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderCodex, Label: "Codex"},
		SupportsBroker: supportsBroker,
		Probe:          func(context.Context) ProviderProbe { return ProviderProbe{Available: true, Ready: true} },
		Runner:         runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	seedVerifiedModel(t, service, ProviderCodex, testVerifiedModel)
	return service
}

func TestServiceDoesNotGrantBrokerToProviderWithoutBrokerSupport(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, captureRunner{request: requests})
	service.SetToolBroker(&ToolBroker{})
	service.SetBrokerEndpoint("http://127.0.0.1:17321", "/tmp/alpha-nexus")

	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "hello", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request.BrokerEndpoint != "" || request.BrokerToken != "" || request.Executable != "" {
		t.Fatalf("provider without broker capability received broker credentials: %+v", request)
	}
	waitForRun(t, service, run.ID, "completed")
}

func TestServiceGrantsBrokerOnlyToProviderWithBrokerSupport(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupServiceWithBrokerSupport(t, captureRunner{request: requests}, true)
	service.SetToolBroker(&ToolBroker{})
	service.SetBrokerEndpoint("http://127.0.0.1:17321", "/tmp/alpha-nexus")

	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "hello", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request.BrokerEndpoint != "http://127.0.0.1:17321" || request.Executable != "/tmp/alpha-nexus" || len(request.BrokerToken) != 64 {
		t.Fatalf("broker-capable provider did not receive run-scoped broker access: %+v", request)
	}
	waitForRun(t, service, run.ID, "completed")
}

func TestRealServiceAddsOnlyVerifiedProviderModel(t *testing.T) {
	if os.Getenv("ALPHA_NEXUS_REAL_SERVICE_ADD") != "1" {
		t.Skip("set ALPHA_NEXUS_REAL_SERVICE_ADD=1 for an explicit live add-provider smoke")
	}
	providerID := ProviderID(strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_PROVIDER")))
	modelID := strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_MODEL"))
	if providerID == "" || modelID == "" {
		t.Fatal("ALPHA_NEXUS_REAL_PROVIDER and ALPHA_NEXUS_REAL_MODEL are required")
	}
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := DefaultRegistry(database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })

	descriptor, err := service.TestAndAddProvider(context.Background(), ProviderConfig{
		ProviderID:     providerID,
		ExecutablePath: strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_EXECUTABLE")),
		LaunchMode:     ProviderLaunchMode(strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_LAUNCH_MODE"))),
	}, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != providerID {
		t.Fatalf("descriptor=%+v", descriptor)
	}
}

func TestCatalogProviderProbeAndConnectDoNotActivateProvider(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	original := optionalProviderBuilders[ProviderKimi]
	t.Cleanup(func() { optionalProviderBuilders[ProviderKimi] = original })
	connected := false
	optionalProviderBuilders[ProviderKimi] = func(ProviderConfig, providerResolverFactory) Provider {
		return Provider{
			Descriptor: ProviderDescriptor{ID: ProviderKimi, Label: "Kimi Code", Removable: true},
			Probe: func(context.Context) ProviderProbe {
				return ProviderProbe{
					ProviderID: ProviderKimi, Available: true, Ready: true,
					ModelCandidates: []string{"kimi-k2.5"},
				}
			},
			Runner: scriptedRunner{},
			Connect: func(context.Context) error {
				connected = true
				return nil
			},
		}
	}
	probe, err := service.ProbeCatalogProvider(context.Background(), ProviderConfig{ProviderID: ProviderKimi})
	if err != nil {
		t.Fatal(err)
	}
	if !probe.Ready || strings.Join(probe.ModelCandidates, ",") != "kimi-k2.5" {
		t.Fatalf("probe=%+v", probe)
	}
	if err := service.ConnectCatalogProvider(context.Background(), ProviderConfig{ProviderID: ProviderKimi}); err != nil {
		t.Fatal(err)
	}
	if !connected {
		t.Fatal("catalog provider connection surface was not started")
	}
	if _, active := service.registry.Get(ProviderKimi); active {
		t.Fatal("catalog probe activated provider before a real model validation")
	}
}

func TestServiceAddsAndRemovesOptionalProviderWithoutDeletingHistory(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := DefaultRegistry(database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	service, err := NewService(store, registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	provider, ok := providerFromCatalog(ProviderKimi)
	if !ok {
		t.Fatal("Kimi catalog adapter missing")
	}
	if err := store.AddConfiguredProvider(context.Background(), ProviderKimi); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(provider); err != nil {
		t.Fatal(err)
	}
	descriptor := provider.Descriptor
	if descriptor.ID != ProviderKimi || !descriptor.Removable {
		t.Fatalf("descriptor=%+v", descriptor)
	}
	configured, err := store.ListConfiguredProviders(context.Background())
	if err != nil || len(configured) != 1 || configured[0] != ProviderKimi {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
	if err := service.RemoveProvider(context.Background(), ProviderKimi); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get(ProviderKimi); ok {
		t.Fatal("removed provider is still active")
	}
	configured, err = store.ListConfiguredProviders(context.Background())
	if err != nil || len(configured) != 0 {
		t.Fatalf("configured after remove=%v err=%v", configured, err)
	}
}

func TestServiceDoesNotPersistProviderWhenExecutableTestFails(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := DefaultRegistry(database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	service, err := NewService(store, registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })

	_, err = service.TestAndAddProvider(context.Background(), ProviderConfig{
		ProviderID:     ProviderKimi,
		ExecutablePath: filepath.Join(t.TempDir(), "missing-kimi"),
	}, "kimi-code/kimi-for-coding")
	if !errors.Is(err, ErrProviderExecutable) {
		t.Fatalf("error=%v, want ErrProviderExecutable", err)
	}
	configs, listErr := store.ListProviderConfigs(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(configs) != 0 {
		t.Fatalf("failed provider was persisted: %+v", configs)
	}
	if _, ok := registry.Get(ProviderKimi); ok {
		t.Fatal("failed provider was activated")
	}
}

func TestStorePersistsProviderLaunchModeWithoutCredentials(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	config := ProviderConfig{
		ProviderID:     ProviderClaude,
		ExecutablePath: "/opt/local/bin/claude",
		LaunchMode:     ProviderLaunchModeCCSwitch,
	}
	if err := store.SaveProviderConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	configs, err := store.ListProviderConfigs(context.Background())
	if err != nil || len(configs) != 1 {
		t.Fatalf("configs=%+v err=%v", configs, err)
	}
	if configs[0] != config {
		t.Fatalf("config=%+v want=%+v", configs[0], config)
	}
}

func TestServicePersistsProviderOnlyAfterRealModelResponse(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := DefaultRegistry(database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	service, err := NewService(store, registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })

	binary := filepath.Join(t.TempDir(), "codebuddy")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'CodeBuddy 1.0.0\n'
  exit 0
fi
input=$(cat)
nonce=$(printf '%s' "$input" | sed -n 's/.*Reply with exactly \([^ ]*\) and nothing else\..*/\1/p')
printf '{"type":"assistant","session_id":"verified","message":{"content":[{"type":"text","text":"%s"}]}}\n' "$nonce"
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor, err := service.TestAndAddProvider(context.Background(), ProviderConfig{
		ProviderID:     ProviderCodeBuddy,
		ExecutablePath: binary,
	}, "glm-5")
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != ProviderCodeBuddy {
		t.Fatalf("descriptor=%+v", descriptor)
	}
	configs, err := store.ListProviderConfigs(context.Background())
	if err != nil || len(configs) != 1 || configs[0].ExecutablePath != binary {
		t.Fatalf("configs=%+v err=%v", configs, err)
	}
	_, probes := service.Providers(context.Background())
	var found bool
	for _, probe := range probes {
		if probe.ProviderID == ProviderCodeBuddy {
			found = true
			if len(probe.Models) != 1 || probe.Models[0] != "glm-5" {
				t.Fatalf("verified models=%v", probe.Models)
			}
		}
	}
	if !found {
		t.Fatal("verified provider was not activated")
	}
}

func TestLegacyAntigravityStateEnablesOptionalProvider(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	if _, err := store.PutProviderSelection(context.Background(), ProviderSelection{ProviderID: ProviderAntigravity}); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureLegacyOptionalProviders(context.Background()); err != nil {
		t.Fatal(err)
	}
	configured, err := store.ListConfiguredProviders(context.Background())
	if err != nil || len(configured) != 1 || configured[0] != ProviderAntigravity {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
}

func TestServicePersistsEventsAndNativeSession(t *testing.T) {
	service := setupService(t, scriptedRunner{events: []ProviderEvent{{Type: "status", Payload: map[string]any{"label": "working"}}, {Type: "message.delta", Payload: map[string]any{"text": "hello"}}}})
	seedVerifiedModel(t, service, ProviderCodex, "model-1")
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", "model-1", ContextSnapshot{Workspace: "browser", Page: "accounts", Selections: []ContextSelection{{ID: "account_1", Name: "Demo"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, "model-1", "summarize", conversation.Context)
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForRun(t, service, run.ID, "completed")
	if completed.ProviderSessionID != "thread_test" {
		t.Fatalf("session=%q", completed.ProviderSessionID)
	}
	if completed.ModelID != "model-1" {
		t.Fatalf("model=%q", completed.ModelID)
	}
	events, err := service.Events(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 || events[4].Type != "provider.session.saved" {
		t.Fatalf("events=%d, want provider session event: %#v", len(events), events)
	}
	for index, event := range events {
		if event.Seq != int64(index+1) {
			t.Fatalf("event %d sequence=%d", index, event.Seq)
		}
	}
	loaded, err := service.GetConversation(context.Background(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "hello" {
		t.Fatalf("messages=%#v", loaded.Messages)
	}
	if loaded.Messages[0].ProviderID != ProviderCodex || loaded.Messages[0].RunID != run.ID {
		t.Fatalf("message run linkage=%#v", loaded.Messages[0])
	}
	if loaded.Title != "accounts · summarize" || loaded.ModelID != "model-1" {
		t.Fatalf("conversation title=%q model=%q", loaded.Title, loaded.ModelID)
	}
}

func TestServiceDoesNotEmitCompletedBeforeRunIsDurable(t *testing.T) {
	started := make(chan RunRequest, 1)
	release := make(chan struct{})
	service := setupService(t, blockingTextTaskRunner{started: started, release: release})
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "hello", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := service.store.db.ExecContext(context.Background(), `
		CREATE TRIGGER reject_agent_run_completion
		BEFORE UPDATE OF status ON agent_runs
		WHEN NEW.status = 'completed'
		BEGIN
			SELECT RAISE(FAIL, 'forced completion failure');
		END`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = service.store.db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS reject_agent_run_completion`)
		_, _ = service.CancelRun(context.Background(), run.ID)
	}()
	close(release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, err := service.Events(context.Background(), run.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		providerSessionSaved := false
		for _, event := range events {
			if event.Type == "run.completed" {
				t.Fatal("run.completed was emitted before the durable run transition")
			}
			providerSessionSaved = providerSessionSaved || event.Type == "provider.session.saved"
		}
		if providerSessionSaved {
			time.Sleep(20 * time.Millisecond)
			events, err = service.Events(context.Background(), run.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Type == "run.completed" {
					t.Fatal("run.completed was emitted after the durable run transition failed")
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("provider session event was not persisted")
}

func TestServiceExpandsSkillPromptForProviderButPersistsUserMessage(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, captureRunner{request: requests})
	service.SetSkillPromptExpander(testPromptExpander{})
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	const message = "请用 /caption-check 检查"
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, message, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if !strings.Contains(request.Message, "skill instructions") || !strings.Contains(request.Message, message) {
		t.Fatalf("provider message=%q", request.Message)
	}
	waitForRun(t, service, run.ID, "completed")
	loaded, err := service.GetConversation(context.Background(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[0].Content != message {
		t.Fatalf("messages=%+v", loaded.Messages)
	}
}

func TestCompleteTextTaskUsesSelectedVerifiedAgentWithoutToolsOrSession(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, captureRunner{request: requests})
	service.probeMu.Lock()
	probe := service.probes[ProviderCodex]
	probe.State = ProviderStateReady
	probe.Available = true
	probe.Ready = true
	service.probes[ProviderCodex] = probe
	service.probeMu.Unlock()
	if _, err := service.SaveProviderSelection(context.Background(), ProviderSelection{
		ProviderID: ProviderCodex, ModelID: testVerifiedModel,
	}); err != nil {
		t.Fatal(err)
	}
	selection, err := service.TextTaskSelection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.CompleteTextTask(
		context.Background(),
		*selection,
		"视频拆解",
		`{"summary":"test"}`,
		ContextSnapshot{Workspace: "trend-radar", Page: "video-analysis"},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if !request.Ephemeral || request.SessionID != "" || request.BrokerEndpoint != "" || request.BrokerToken != "" || request.Executable != "" {
		t.Fatalf("text task leaked session or broker access: %+v", request)
	}
	if result.Answer != "done" || result.RunID == "" || result.ConversationID == "" ||
		result.ProviderID != ProviderCodex || result.ModelID != testVerifiedModel {
		t.Fatalf("result=%+v", result)
	}
}

func TestValidateTextTaskSelectionRequiresVerifiedSupportedModel(t *testing.T) {
	service := setupService(t, captureRunner{request: make(chan RunRequest, 1)})
	if err := service.ValidateTextTaskSelection(context.Background(), ProviderSelection{
		ProviderID: ProviderCodex, ModelID: testVerifiedModel,
	}); err != nil {
		t.Fatalf("verified text selection rejected: %v", err)
	}
	if err := service.ValidateTextTaskSelection(context.Background(), ProviderSelection{
		ProviderID: ProviderCodex, ModelID: "unverified/model",
	}); !errors.Is(err, ErrModelNotVerified) {
		t.Fatalf("unverified selection error=%v", err)
	}
}

func TestCompleteTextTaskHidesAndDeletesTemporaryConversation(t *testing.T) {
	started := make(chan RunRequest, 1)
	release := make(chan struct{})
	service := setupService(t, blockingTextTaskRunner{started: started, release: release})
	type outcome struct {
		result TextTaskResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := service.CompleteTextTask(
			context.Background(),
			ProviderSelection{ProviderID: ProviderCodex, ModelID: testVerifiedModel},
			"视频拆解",
			`{"summary":"test"}`,
			ContextSnapshot{Workspace: "trend-radar", Page: "video-analysis"},
		)
		completed <- outcome{result: result, err: err}
	}()

	<-started
	conversations, listErr := service.ListConversations(context.Background(), ProviderCodex)
	close(release)
	got := <-completed
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(conversations) != 0 {
		t.Fatalf("temporary text task leaked into Agent history: %+v", conversations)
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result.ConversationID == "" || got.result.RunID == "" {
		t.Fatalf("result=%+v", got.result)
	}
	if _, err := service.GetConversation(context.Background(), got.result.ConversationID); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("temporary conversation still exists: %v", err)
	}
	workspace := filepath.Join(service.dataDir, "agent-workspace", got.result.ConversationID)
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary workspace still exists: %v", err)
	}
}

func TestCompleteTextTaskRejectsProviderWithoutEphemeralSessionSupport(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderKimi, Label: "Kimi Code"},
		Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Available: true, Ready: true} },
		Runner:     captureRunner{request: make(chan RunRequest, 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	seedVerifiedModel(t, service, ProviderKimi, testVerifiedModel)

	_, err = service.CompleteTextTask(
		context.Background(),
		ProviderSelection{ProviderID: ProviderKimi, ModelID: testVerifiedModel},
		"视频拆解",
		`{"summary":"test"}`,
		ContextSnapshot{Workspace: "trend-radar", Page: "video-analysis"},
	)
	if !errors.Is(err, ErrProviderConnect) {
		t.Fatalf("error=%v, want fail-closed provider fallback", err)
	}
	conversations, listErr := service.store.ListConversations(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(conversations) != 0 {
		t.Fatalf("unsupported provider created temporary history: %+v", conversations)
	}
}

// The default runner is dsh; its hermetic run path must be usable for
// business text tasks (creation workflow, video analysis) or the default
// choice would fail closed on every non-chat surface.
func TestDshProviderSupportsEphemeralTextTasks(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	requests := make(chan RunRequest, 1)
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderDsh, Label: "DSH Agent"},
		Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Available: true, Ready: true} },
		Runner:     captureRunner{request: requests},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	seedVerifiedModel(t, service, ProviderDsh, testVerifiedModel)

	if err := service.ValidateTextTaskSelection(context.Background(), ProviderSelection{
		ProviderID: ProviderDsh, ModelID: testVerifiedModel,
	}); err != nil {
		t.Fatalf("dsh verified text selection rejected: %v", err)
	}
	if _, err := service.CompleteTextTask(
		context.Background(),
		ProviderSelection{ProviderID: ProviderDsh, ModelID: testVerifiedModel},
		"创作访谈",
		`{"summary":"test"}`,
		ContextSnapshot{Workspace: "creation-workbench", Page: "knowledge-assisted-copywriting"},
	); err != nil {
		t.Fatalf("dsh text task failed: %v", err)
	}
	request := <-requests
	if !request.Ephemeral {
		t.Fatalf("dsh text task did not use the hermetic path: %+v", request)
	}
}

func TestCompleteTextTaskRejectsNewWorkAfterShutdown(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, captureRunner{request: requests})
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := service.CompleteTextTask(
		context.Background(),
		ProviderSelection{ProviderID: ProviderCodex, ModelID: testVerifiedModel},
		"视频拆解",
		`{"summary":"test"}`,
		ContextSnapshot{Workspace: "trend-radar", Page: "video-analysis"},
	)
	if !errors.Is(err, ErrProviderConnect) {
		t.Fatalf("error=%v, want shutdown rejection", err)
	}
	select {
	case request := <-requests:
		t.Fatalf("provider ran after shutdown: %+v", request)
	default:
	}
}

func TestModelValidationRejectsNewWorkAfterShutdown(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	service.registry.mu.Lock()
	provider := service.registry.providers[ProviderCodex]
	provider.ValidateModel = func(context.Context, string, string, string) error { return nil }
	service.registry.providers[ProviderCodex] = provider
	service.registry.mu.Unlock()
	service.probeMu.Lock()
	probe := service.probes[ProviderCodex]
	probe.State = ProviderStateReady
	probe.Available = true
	probe.Ready = true
	service.probes[ProviderCodex] = probe
	service.probeMu.Unlock()
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.BeginModelValidation(ProviderCodex, testVerifiedModel); !errors.Is(err, ErrModelValidation) {
		t.Fatalf("error=%v, want shutdown rejection", err)
	}
}

func TestCreateConversationCannotMarkUserChatTransient(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	conversation, err := service.CreateConversation(
		context.Background(),
		ProviderCodex,
		"普通聊天",
		testVerifiedModel,
		ContextSnapshot{Workspace: "trend-radar", Page: "video-analysis", Transient: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Context.Transient {
		t.Fatalf("public conversation retained internal transient marker: %+v", conversation.Context)
	}
	transientIDs, err := service.store.ListTransientConversationIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(transientIDs) != 0 {
		t.Fatalf("ordinary user chat became restart-cleanup target: %v", transientIDs)
	}
	visible, err := service.ListConversations(context.Background(), ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].ID != conversation.ID {
		t.Fatalf("ordinary user chat was hidden as a legacy system task: %+v", visible)
	}
}

func TestNewServiceDeletesInterruptedTransientConversationsAndWorkspaces(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	conversation, err := store.CreateConversation(
		context.Background(),
		ProviderCodex,
		"视频拆解",
		testVerifiedModel,
		ContextSnapshot{Workspace: "trend-radar", Page: "video-analysis", Transient: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(database.DataDir, "agent-workspace", conversation.ID)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderCodex, Label: "Codex"},
		Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Available: true, Ready: true} },
		Runner:     scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(store, registry, database.DataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConversation(context.Background(), conversation.ID); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("transient conversation survived restart: %v", err)
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transient workspace survived restart: %v", err)
	}
}

func TestProviderErrorCodeUsesFirstStableCodeFromJoinedErrors(t *testing.T) {
	err := errors.Join(
		errors.New("AGENT_PROVIDER_OPENCODE_FAILED"),
		errors.New("AGENT_PROVIDER_OPENCODE_SESSION_CLEANUP_FAILED"),
	)
	if got := providerErrorCode(err); got != "AGENT_PROVIDER_OPENCODE_FAILED" {
		t.Fatalf("code=%q", got)
	}
}

func TestListConversationsHidesLegacyVideoAnalysisTasks(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	legacy, err := service.store.CreateConversation(
		context.Background(),
		ProviderCodex,
		"趋势雷达 · 视频拆解",
		testVerifiedModel,
		ContextSnapshot{Workspace: "trend-radar", Page: "video-analysis"},
	)
	if err != nil {
		t.Fatal(err)
	}
	conversations, err := service.ListConversations(context.Background(), ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 0 {
		t.Fatalf("legacy system task leaked into Agent history: %+v", conversations)
	}
	if _, err := service.GetConversation(context.Background(), legacy.ID); err != nil {
		t.Fatalf("legacy audit record should remain locally available: %v", err)
	}
}

func TestTextTaskSelectionFailsClosedWhenProviderIsNotReady(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	service.probeMu.Lock()
	probe := service.probes[ProviderCodex]
	probe.State = ProviderStateUnavailable
	probe.Available = false
	probe.Ready = false
	service.probes[ProviderCodex] = probe
	service.probeMu.Unlock()
	if _, err := service.SaveProviderSelection(context.Background(), ProviderSelection{
		ProviderID: ProviderCodex, ModelID: testVerifiedModel,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TextTaskSelection(context.Background()); !errors.Is(err, ErrProviderConnect) {
		t.Fatalf("error=%v want=%v", err, ErrProviderConnect)
	}
}

func TestTextTaskSelectionRefreshesStaleProviderBeforeCheckingVerifiedModel(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	if _, err := service.SaveProviderSelection(context.Background(), ProviderSelection{
		ProviderID: ProviderCodex, ModelID: testVerifiedModel,
	}); err != nil {
		t.Fatal(err)
	}
	service.probeMu.Lock()
	probe := service.probes[ProviderCodex]
	probe.State = ProviderStateUnknown
	probe.Available = true
	probe.Ready = false
	service.probes[ProviderCodex] = probe
	service.probeMu.Unlock()

	selection, err := service.TextTaskSelection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selection.ProviderID != ProviderCodex || selection.ModelID != testVerifiedModel {
		t.Fatalf("selection=%+v", selection)
	}
}

func TestConversationTitleFallsBackToPageForNonIntentMessage(t *testing.T) {
	if got := conversationTitle(ContextSnapshot{Page: "浏览器管理"}, "1"); got != "浏览器管理 · 助手任务" {
		t.Fatalf("title=%q", got)
	}
}

func TestStoreDefaultsConversationToDsh(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	conversation, err := NewStore(database).CreateConversation(context.Background(), "", "", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Provider != ProviderDsh {
		t.Fatalf("provider=%q, want %q", conversation.Provider, ProviderDsh)
	}
}

func TestServiceKeepsLegacyAlphaConversationsReadOnly(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	if _, err := service.CreateConversation(context.Background(), ProviderAlpha, "legacy", "", ContextSnapshot{}); !errors.Is(err, ErrLegacyProviderReadOnly) {
		t.Fatalf("create legacy error=%v", err)
	}
	legacy, err := service.store.CreateConversation(context.Background(), ProviderAlpha, "legacy", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(context.Background(), legacy.ID, ProviderAlpha, "", "hello", ContextSnapshot{}); !errors.Is(err, ErrLegacyProviderReadOnly) {
		t.Fatalf("run legacy error=%v", err)
	}
	loaded, err := service.GetConversation(context.Background(), legacy.ID)
	if err != nil || loaded.Provider != ProviderAlpha {
		t.Fatalf("legacy conversation lost: conversation=%+v err=%v", loaded, err)
	}
}

func TestCustomLLMProbeSynchronizesDirectlyVerifiedModels(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	const syncedModel = "alpha-nexus-llm/vendor/coder"
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderCustom, Label: "自定义 LLM"},
		Probe: func(context.Context) ProviderProbe {
			return ProviderProbe{
				ProviderID:      ProviderCustom,
				Available:       true,
				Ready:           true,
				Version:         "opencode-1.18.4",
				ModelCandidates: []string{syncedModel},
			}
		},
		ValidateModel: func(context.Context, string, string, string) error { return nil },
		Runner:        scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, registry, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	probe, err := service.ProbeModel(context.Background(), ProviderCustom)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Models) != 1 || probe.Models[0] != syncedModel {
		t.Fatalf("synced models=%q", probe.Models)
	}
}

func TestVerifiedModelRemainsAvailableForSameProviderVersion(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	verifiedAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return verifiedAt }
	if err := store.SaveVerifiedModel(context.Background(), ProviderCodex, "codex-cli 1.0", "gpt-5.6-terra"); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return verifiedAt.Add(30 * 24 * time.Hour) }
	models, err := store.LoadVerifiedModels(context.Background(), ProviderCodex, "codex-cli 1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "gpt-5.6-terra" {
		t.Fatalf("models=%q, want version-bound verified model", models)
	}
	models, err = store.LoadVerifiedModels(context.Background(), ProviderCodex, "codex-cli 2.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("models=%q, provider upgrade must require revalidation", models)
	}
}

func TestOrderVerifiedModelsDropsModelsMissingFromCurrentCatalog(t *testing.T) {
	got := orderVerifiedModels(
		[]string{"retired/model", "current/model"},
		[]string{"current/model", "other/model"},
	)
	if len(got) != 1 || got[0] != "current/model" {
		t.Fatalf("models=%q, want only current catalog intersection", got)
	}
}

func TestServiceAllowsOnlyOneActiveRunAndCancels(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "wait", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "second", ContextSnapshot{}); !errors.Is(err, ErrRunActive) {
		t.Fatalf("second start error=%v, want ErrRunActive", err)
	}
	if _, err := service.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, service, run.ID, "cancelled")
	events, err := service.Events(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var cancelled int
	for _, event := range events {
		if event.Type == "run.cancelled" {
			cancelled++
		}
	}
	if cancelled != 1 {
		t.Fatalf("cancelled events=%d", cancelled)
	}
}

func TestServiceFailsRunWhenProviderExceedsDeadline(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	service.runTimeout = 20 * time.Millisecond
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "wait", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForRun(t, service, run.ID, "failed")
	if failed.ErrorCode != "AGENT_PROVIDER_TIMEOUT" {
		t.Fatalf("error_code=%q", failed.ErrorCode)
	}
}

func TestServiceAllowsOneRunPerConversation(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	first, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), first.ID, ProviderCodex, testVerifiedModel, "wait", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	// Same conversation stays mutually exclusive.
	if _, err := service.StartRun(context.Background(), first.ID, ProviderCodex, testVerifiedModel, "again", ContextSnapshot{}); !errors.Is(err, ErrRunActive) {
		t.Fatalf("same conversation start error=%v, want ErrRunActive", err)
	}
	// A different conversation is allowed to run in parallel.
	if _, err := service.StartRun(context.Background(), second.ID, ProviderCodex, testVerifiedModel, "second", ContextSnapshot{}); err != nil {
		t.Fatalf("second conversation start error=%v, want parallel run allowed", err)
	}
	if _, err := service.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestServiceAllowsParallelRunsUpToGlobalCapacity(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	for index := 0; index < MaxConcurrentRuns; index++ {
		conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "wait", ContextSnapshot{}); err != nil {
			t.Fatalf("run %d start error=%v, want capacity to allow %d parallel runs", index, err, MaxConcurrentRuns)
		}
	}
	extra, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(context.Background(), extra.ID, ProviderCodex, testVerifiedModel, "over", ContextSnapshot{}); !errors.Is(err, ErrRunCapacity) {
		t.Fatalf("start above capacity error=%v, want ErrRunCapacity", err)
	}
}

func TestServiceShutdownCancelsAndMarksForegroundRunInterrupted(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "wait", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "interrupted" {
		t.Fatalf("status=%q, want interrupted", loaded.Status)
	}
	events, err := service.Events(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Type != "run.interrupted" {
		t.Fatalf("events=%+v, want durable run.interrupted terminal event", events)
	}
}

func TestContextSnapshotRejectsPathsAndCredentials(t *testing.T) {
	for _, value := range []string{"/Users/albert/private", "token=not-allowed", `C:\\private`} {
		if err := (ContextSnapshot{Page: value}).Validate(); !errors.Is(err, ErrUnsafeContext) {
			t.Fatalf("value=%q error=%v", value, err)
		}
	}
}

func TestModelIDValidationRejectsUnsafeCLIInput(t *testing.T) {
	for _, model := range []string{"", "model-1", "provider/model:preview"} {
		if err := ValidateModelID(model); err != nil {
			t.Fatalf("model=%q error=%v", model, err)
		}
	}
	for _, model := range []string{"model name", "model;command", "model\nnext"} {
		if err := ValidateModelID(model); !errors.Is(err, ErrInvalidModel) {
			t.Fatalf("model=%q error=%v", model, err)
		}
	}
}

func TestRegistryRunsModelProbeOnlyWhenRequested(t *testing.T) {
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderCodex, Label: "Codex"},
		Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Ready: true} },
		ModelProbe: func(context.Context) ProviderProbe {
			return ProviderProbe{Ready: true, ModelCandidates: []string{"gpt-5.6-sol"}}
		},
		Runner: scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.List(context.Background())[0].Models; len(got) != 0 {
		t.Fatalf("regular probe models=%q", got)
	}
	if got := registry.ProbeModels(context.Background())[0].ModelCandidates; len(got) != 1 || got[0] != "gpt-5.6-sol" {
		t.Fatalf("model probe candidates=%q", got)
	}
}

func TestServiceAddsOnlyModelsThatPassLiveValidation(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderOpenCode, Label: "OpenCode"},
		Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Available: true, Ready: true} },
		ModelProbe: func(context.Context) ProviderProbe {
			return ProviderProbe{Available: true, Ready: true, ModelCandidates: []string{"opencode/works", "opencode/broken"}}
		},
		ValidateModel: func(_ context.Context, modelID, _, _ string) error {
			if modelID == "opencode/works" {
				return nil
			}
			return errors.New("upstream rejected model")
		},
		Runner: scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProbeModel(context.Background(), ProviderOpenCode); err != nil {
		t.Fatal(err)
	}
	_, probes := service.Providers(context.Background())
	if len(probes) != 1 || len(probes[0].Models) != 0 || len(probes[0].ModelCandidates) != 2 {
		t.Fatalf("catalog was exposed as verified models: %+v", probes)
	}
	if _, err := service.CreateConversation(context.Background(), ProviderOpenCode, "", "", ContextSnapshot{}); !errors.Is(err, ErrModelNotVerified) {
		t.Fatalf("OpenCode default model error=%v, want ErrModelNotVerified", err)
	}

	if err := service.BeginModelValidation(ProviderOpenCode, "opencode/works"); err != nil {
		t.Fatal(err)
	}
	waitForModelValidation(t, service, ProviderOpenCode)
	_, probes = service.Providers(context.Background())
	if len(probes[0].Models) != 1 || probes[0].Models[0] != "opencode/works" || probes[0].ModelValidationError != "" {
		t.Fatalf("successful model was not persisted: %+v", probes[0])
	}
	if _, err := service.CreateConversation(context.Background(), ProviderOpenCode, "", "opencode/works", ContextSnapshot{}); err != nil {
		t.Fatalf("verified OpenCode model rejected: %v", err)
	}

	if err := service.BeginModelValidation(ProviderOpenCode, "opencode/broken"); err != nil {
		t.Fatal(err)
	}
	waitForModelValidation(t, service, ProviderOpenCode)
	_, probes = service.Providers(context.Background())
	if len(probes[0].Models) != 1 || probes[0].Models[0] != "opencode/works" {
		t.Fatalf("failed model entered the verified list: %+v", probes[0])
	}
	if probes[0].ModelValidationError != "AGENT_MODEL_UNREACHABLE" {
		t.Fatalf("validation error=%q", probes[0].ModelValidationError)
	}

	reloaded, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	_, probes = reloaded.Providers(context.Background())
	if len(probes[0].Models) != 1 || probes[0].Models[0] != "opencode/works" {
		t.Fatalf("verified model did not survive restart: %+v", probes[0])
	}
}

func TestServiceBatchModelValidationKeepsOnlyReachableModelsAndReportsProgress(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderOpenCode, Label: "OpenCode"},
		Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Available: true, Ready: true} },
		ModelProbe: func(context.Context) ProviderProbe {
			return ProviderProbe{
				Available:       true,
				Ready:           true,
				ModelCandidates: []string{"opencode/works", "opencode/broken", "opencode/also-works"},
			}
		},
		ValidateModel: func(_ context.Context, modelID, _, _ string) error {
			if modelID == "opencode/broken" {
				return errors.New("upstream rejected model")
			}
			return nil
		},
		Runner: scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProbeModel(context.Background(), ProviderOpenCode); err != nil {
		t.Fatal(err)
	}

	if err := service.BeginModelValidationBatch(ProviderOpenCode, []string{
		"opencode/works",
		"opencode/broken",
		"opencode/works",
		"opencode/also-works",
	}); err != nil {
		t.Fatal(err)
	}
	waitForModelValidation(t, service, ProviderOpenCode)

	_, probes := service.Providers(context.Background())
	if len(probes) != 1 {
		t.Fatalf("probes=%+v", probes)
	}
	probe := probes[0]
	if probe.ModelValidationTotal != 3 || probe.ModelValidationCompleted != 3 || probe.ModelValidationPassed != 2 {
		t.Fatalf("progress=%+v", probe)
	}
	if len(probe.ModelValidationFailures) != 1 ||
		probe.ModelValidationFailures[0].ModelID != "opencode/broken" ||
		probe.ModelValidationFailures[0].ErrorCode != "AGENT_MODEL_UNREACHABLE" {
		t.Fatalf("failures=%+v", probe.ModelValidationFailures)
	}
	if diff := strings.Join(probe.Models, ","); diff != "opencode/works,opencode/also-works" {
		t.Fatalf("verified models=%q", probe.Models)
	}
}

func TestServiceResumesInterruptedModelValidationBatchAfterRestart(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderAntigravity, Label: "Antigravity"},
		Probe: func(context.Context) ProviderProbe {
			return ProviderProbe{Available: true, Ready: true, Version: "1.1.7"}
		},
		ModelProbe: func(context.Context) ProviderProbe {
			return ProviderProbe{
				ProviderID:      ProviderAntigravity,
				Available:       true,
				Ready:           true,
				Version:         "1.1.7",
				ModelCandidates: []string{"gemini/first", "gemini/second"},
			}
		},
		ValidateModel: func(ctx context.Context, _, _, _ string) error {
			if calls.Add(1) == 1 {
				started <- struct{}{}
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		},
		Runner: scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProbeModel(context.Background(), ProviderAntigravity); err != nil {
		t.Fatal(err)
	}
	if err := service.BeginModelValidationBatch(ProviderAntigravity, []string{"gemini/first", "gemini/second"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model validation did not start")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := service.Shutdown(shutdownCtx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()

	reloaded, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reloaded.Shutdown(context.Background()) })
	reloaded.PrewarmProviders(1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		batch, loadErr := NewStore(database).LoadModelValidationBatch(context.Background(), ProviderAntigravity)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if batch == nil && calls.Load() == 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	_, probes := reloaded.Providers(context.Background())
	if len(probes) != 1 {
		t.Fatalf("probes=%+v", probes)
	}
	probe := probes[0]
	if probe.ModelValidationTotal != 2 || probe.ModelValidationCompleted != 2 || probe.ModelValidationPassed != 2 {
		t.Fatalf("resumed progress=%+v", probe)
	}
	if diff := strings.Join(probe.Models, ","); diff != "gemini/first,gemini/second" {
		t.Fatalf("resumed verified models=%q", probe.Models)
	}
	if batch, err := NewStore(database).LoadModelValidationBatch(context.Background(), ProviderAntigravity); err != nil || batch != nil {
		t.Fatalf("completed validation batch was not cleared: batch=%+v err=%v", batch, err)
	}
}

func TestServiceRejectsUnverifiedExplicitModelEverywhere(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	if _, err := service.CreateConversation(context.Background(), ProviderCodex, "", "", ContextSnapshot{}); !errors.Is(err, ErrModelNotVerified) {
		t.Fatalf("empty model create error=%v, want ErrModelNotVerified", err)
	}
	if _, err := service.CreateConversation(context.Background(), ProviderCodex, "", "unverified/model", ContextSnapshot{}); !errors.Is(err, ErrModelNotVerified) {
		t.Fatalf("create error=%v, want ErrModelNotVerified", err)
	}
	seedVerifiedModel(t, service, ProviderCodex, "verified/model")
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", "verified/model", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveProviderSelection(context.Background(), ProviderSelection{ProviderID: ProviderCodex, ConversationID: conversation.ID, ModelID: "unverified/model"}); !errors.Is(err, ErrModelNotVerified) {
		t.Fatalf("selection error=%v, want ErrModelNotVerified", err)
	}
	if _, err := service.UpdateConversation(context.Background(), conversation.ID, nil, stringPointer("unverified/model")); !errors.Is(err, ErrModelNotVerified) {
		t.Fatalf("update error=%v, want ErrModelNotVerified", err)
	}
	if _, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, "unverified/model", "hello", ContextSnapshot{}); !errors.Is(err, ErrModelNotVerified) {
		t.Fatalf("run error=%v, want ErrModelNotVerified", err)
	}
}

func TestRegistryRejectsLegacyDirectAPIProvider(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	_, err = NewRegistry(
		Provider{
			Descriptor: ProviderDescriptor{ID: ProviderID("api"), Label: "API 模型"},
			Probe: func(context.Context) ProviderProbe {
				t.Fatal("legacy API provider must not be probed")
				return ProviderProbe{}
			},
			Runner: scriptedRunner{},
		},
		Provider{
			Descriptor: ProviderDescriptor{ID: ProviderOpenCode, Label: "OpenCode"},
			Probe: func(context.Context) ProviderProbe {
				return ProviderProbe{Ready: true, Models: []string{"opencode/model"}}
			},
			Runner: scriptedRunner{},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "direct API") {
		t.Fatalf("legacy direct API provider was accepted: %v", err)
	}
}

func TestServiceProvidersKeepsCatalogSeparateFromVerifiedModels(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderCodex, Label: "Codex"},
		Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Ready: true} },
		ModelProbe: func(context.Context) ProviderProbe {
			return ProviderProbe{Ready: true, ModelCandidates: []string{"gpt-5.6-sol"}}
		},
		Runner: scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProbeModel(context.Background(), ProviderCodex); err != nil {
		t.Fatal(err)
	}
	_, probes := service.Providers(context.Background())
	if len(probes) != 1 || len(probes[0].Models) != 0 || len(probes[0].ModelCandidates) != 1 || probes[0].ModelCandidates[0] != "gpt-5.6-sol" {
		t.Fatalf("probes=%+v", probes)
	}
}

func TestServiceProvidersNeverWaitsForCLIProbe(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	probeStarted := make(chan struct{}, 1)
	releaseProbe := make(chan struct{})
	registry, err := NewRegistry(Provider{
		Descriptor: ProviderDescriptor{ID: ProviderCodex, Label: "Codex"},
		Probe: func(context.Context) ProviderProbe {
			probeStarted <- struct{}{}
			<-releaseProbe
			return ProviderProbe{Ready: true}
		},
		Runner: scriptedRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		service.Providers(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cached provider listing blocked on a CLI probe")
	}
	select {
	case <-probeStarted:
		t.Fatal("cached provider listing unexpectedly invoked the CLI")
	default:
	}
	close(releaseProbe)
}

func TestServiceRejectsProviderChangeForExistingConversation(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(
		Provider{Descriptor: ProviderDescriptor{ID: ProviderCodex, Label: "Codex"}, Probe: func(context.Context) ProviderProbe { return ProviderProbe{Ready: true} }, Runner: scriptedRunner{}},
		Provider{Descriptor: ProviderDescriptor{ID: ProviderOpenCode, Label: "OpenCode"}, Probe: func(context.Context) ProviderProbe { return ProviderProbe{Ready: true} }, Runner: scriptedRunner{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	seedVerifiedModel(t, service, ProviderCodex, testVerifiedModel)
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(context.Background(), conversation.ID, ProviderOpenCode, testVerifiedModel, "hello", ContextSnapshot{}); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("error=%v, want ErrProviderMismatch", err)
	}
	loaded, err := service.GetConversation(context.Background(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Provider != ProviderCodex {
		t.Fatalf("provider mutated to %q", loaded.Provider)
	}
}

func TestProviderSelectionsStayIndependentAndRejectForeignConversation(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(
		Provider{Descriptor: ProviderDescriptor{ID: ProviderCodex, Label: "Codex"}, Probe: func(context.Context) ProviderProbe { return ProviderProbe{} }, Runner: scriptedRunner{}},
		Provider{Descriptor: ProviderDescriptor{ID: ProviderOpenCode, Label: "OpenCode"}, Probe: func(context.Context) ProviderProbe { return ProviderProbe{} }, Runner: scriptedRunner{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(NewStore(database), registry, database.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	seedVerifiedModel(t, service, ProviderCodex, "gpt-5")
	seedVerifiedModel(t, service, ProviderOpenCode, "opencode/big-pickle")
	codexConversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", "gpt-5", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveProviderSelection(context.Background(), ProviderSelection{ProviderID: ProviderCodex, ConversationID: codexConversation.ID, ModelID: "gpt-5", Draft: "codex draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveProviderSelection(context.Background(), ProviderSelection{ProviderID: ProviderOpenCode, ConversationID: codexConversation.ID, ModelID: "opencode/big-pickle"}); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("foreign conversation error=%v", err)
	}
	opencode, err := service.ProviderSelection(context.Background(), ProviderOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if opencode.ConversationID != "" || opencode.ModelID != "" || opencode.Draft != "" {
		t.Fatalf("opencode selection leaked: %+v", opencode)
	}
	last, err := service.ProviderSelection(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if last.ProviderID != ProviderCodex || last.ConversationID != codexConversation.ID {
		t.Fatalf("last selection=%+v", last)
	}
}

func TestRegistryProbesOnlyRequestedProviderModel(t *testing.T) {
	registry, err := NewRegistry(
		Provider{
			Descriptor: ProviderDescriptor{ID: ProviderCodex, Label: "Codex"},
			Probe:      func(context.Context) ProviderProbe { return ProviderProbe{Ready: true} },
			ModelProbe: func(context.Context) ProviderProbe {
				return ProviderProbe{Ready: true, Models: []string{"gpt-5.6-sol"}}
			},
			Runner: scriptedRunner{},
		},
		Provider{
			Descriptor: ProviderDescriptor{ID: ProviderOpenCode, Label: "OpenCode"},
			Probe: func(context.Context) ProviderProbe {
				t.Fatal("unrequested provider probe should not run")
				return ProviderProbe{}
			},
			Runner: scriptedRunner{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	probe, err := registry.ProbeModel(context.Background(), ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	if probe.ProviderID != ProviderCodex || len(probe.Models) != 1 || probe.Models[0] != "gpt-5.6-sol" {
		t.Fatalf("probe=%+v", probe)
	}
}

func TestCodexJSONLFiltersNonAssistantItems(t *testing.T) {
	var parts []string
	sessionID, err := consumeCodexJSONL(strings.NewReader("{\"type\":\"thread.started\",\"thread_id\":\"thread_1\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"reasoning\",\"text\":\"hidden\"}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"visible\"}}\n"), func(event ProviderEvent) { parts = append(parts, event.Payload["text"].(string)) })
	if err != nil || sessionID != "thread_1" || len(parts) != 1 || parts[0] != "visible" {
		t.Fatalf("session=%q parts=%v err=%v", sessionID, parts, err)
	}
}

func TestWorkspaceIsPrivateToConversation(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	directory, err := service.workspaceForConversation("conv_abc")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("workspace mode=%v err=%v", info.Mode(), err)
	}
}

func waitForRun(t *testing.T, service *Service, id, want string) *Run {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, err := service.GetRun(context.Background(), id)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	run, err := service.GetRun(context.Background(), id)
	t.Fatalf("run=%+v err=%v, want status=%q", run, err, want)
	return nil
}

func waitForModelValidation(t *testing.T, service *Service, providerID ProviderID) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, probes := service.Providers(context.Background())
		if len(probes) == 1 && probes[0].ProviderID == providerID && probes[0].ValidatingModel == "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, probes := service.Providers(context.Background())
	t.Fatalf("model validation did not finish: %+v", probes)
}

func stringPointer(value string) *string {
	return &value
}

func seedVerifiedModel(t *testing.T, service *Service, providerID ProviderID, modelID string) {
	t.Helper()
	service.probeMu.Lock()
	probe := service.probes[providerID]
	probe.Models = append(probe.Models, modelID)
	service.probes[providerID] = probe
	service.probeMu.Unlock()
	if err := service.store.SaveVerifiedModel(context.Background(), providerID, probe.Version, modelID); err != nil {
		t.Fatal(err)
	}
}

func TestStartRunRejectsEmptyAndOversizedMessages(t *testing.T) {
	service := setupService(t, captureRunner{request: make(chan RunRequest, 1)})
	conversation, err := service.CreateConversation(context.Background(), ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, "   ", ContextSnapshot{}); !errors.Is(err, ErrMessageRequired) {
		t.Fatalf("empty message error=%v, want ErrMessageRequired", err)
	}
	oversized := strings.Repeat("x", maxAgentMessageBytes+1)
	if _, err := service.StartRun(context.Background(), conversation.ID, ProviderCodex, testVerifiedModel, oversized, ContextSnapshot{}); !errors.Is(err, ErrMessageTooLong) {
		t.Fatalf("oversized message error=%v, want ErrMessageTooLong", err)
	}
}
