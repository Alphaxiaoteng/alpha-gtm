package agenthub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCodexRunnerPassesSelectedModelAsOneArgument(t *testing.T) {
	var got []string
	workDir := t.TempDir()
	runner := codexRunner{
		binary: func() string { return "codex" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			got = append([]string(nil), args...)
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"thread.started","thread_id":"thread_1"}'`)
		},
	}
	_, err := runner.Run(context.Background(), RunRequest{WorkDir: workDir, ModelID: "provider/model:preview", Message: "hello"}, func(ProviderEvent) {})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--ignore-user-config", "--ignore-rules", "--config", `model_reasoning_effort="medium"`, "--config", `approval_policy="never"`, "--json", "--sandbox", "read-only", "--skip-git-repo-check", "--cd", workDir, "--model", "provider/model:preview", "-"}
	if len(got) != len(want) {
		t.Fatalf("args=%q", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("args=%q want=%q", got, want)
		}
	}
}

func TestCodexRunnerUsesMediumEffortWhenResuming(t *testing.T) {
	var got []string
	runner := codexRunner{
		binary: func() string { return "codex" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			got = append([]string(nil), args...)
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"thread.started","thread_id":"thread_1"}'`)
		},
	}
	_, err := runner.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), SessionID: "thread_previous", Message: "hello"}, func(ProviderEvent) {})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exec", "resume",
		"--ignore-user-config", "--ignore-rules",
		"--config", `model_reasoning_effort="medium"`,
		"--config", `sandbox_mode="read-only"`,
		"--config", `approval_policy="never"`,
		"--json", "--skip-git-repo-check", "thread_previous", "-",
	}
	if len(got) != len(want) {
		t.Fatalf("args=%q", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("args=%q want=%q", got, want)
		}
	}
}

func TestOpenCodeRunnerCapturesStructuredSessionAndText(t *testing.T) {
	var got []string
	var text strings.Builder
	workDir := t.TempDir()
	runner := openCodeRunner{
		binary: func() string { return "opencode" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			got = append([]string(nil), args...)
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"step_start","sessionID":"ses_1"}' '{"type":"text","sessionID":"ses_1","part":{"type":"text","text":"READY"}}'`)
		},
	}
	sessionID, err := runner.Run(context.Background(), RunRequest{WorkDir: workDir, SessionID: "ses_previous", ModelID: "opencode/big-pickle", Message: "hello"}, func(event ProviderEvent) {
		text.WriteString(event.Payload["text"].(string))
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "ses_1" || text.String() != "READY" {
		t.Fatalf("session=%q text=%q", sessionID, text.String())
	}
	want := []string{"run", "--format", "json", "--dir", workDir, "--session", "ses_previous", "--model", "opencode/big-pickle"}
	for index, value := range want {
		if got[index] != value {
			t.Fatalf("args=%q want-prefix=%q", got, want)
		}
	}
	if got[len(got)-1] == "hello" || !strings.Contains(got[len(got)-1], "User: hello") {
		t.Fatalf("prompt=%q", got[len(got)-1])
	}
}

func TestAlphaAgentRunnerUsesPiJSONModeWithoutAmbientTools(t *testing.T) {
	var got []string
	var text strings.Builder
	var command *exec.Cmd
	workDir := t.TempDir()
	runner := piRunner{
		binary: func() string { return "pi" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			got = append([]string(nil), args...)
			command = exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"session","id":"pi_session_1"}' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"READY"}}'`)
			return command
		},
	}
	sessionID, err := runner.Run(context.Background(), RunRequest{
		WorkDir:        workDir,
		SessionID:      "pi_previous",
		ModelID:        "volcengine-coding/glm-5.2",
		Message:        "hello",
		BrokerEndpoint: "http://127.0.0.1:17321",
		BrokerToken:    strings.Repeat("a", 64),
		Executable:     "/tmp/alpha-nexus",
	}, func(event ProviderEvent) {
		if value, _ := event.Payload["text"].(string); value != "" {
			text.WriteString(value)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "pi_session_1" || text.String() != "READY" {
		t.Fatalf("session=%q text=%q", sessionID, text.String())
	}
	for _, argument := range []string{"--mode", "json", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve"} {
		if !containsArgument(got, argument) {
			t.Fatalf("Alpha Agent did not isolate Pi argument %q: %q", argument, got)
		}
	}
	if !containsArgumentPair(got, "--session", "pi_previous") ||
		!containsArgumentPair(got, "--model", "volcengine-coding/glm-5.2") ||
		!containsArgumentPair(got, "--name", "Alpha Agent") {
		t.Fatalf("Alpha Agent Pi session arguments=%q", got)
	}
	if !strings.Contains(got[len(got)-1], "You are Alpha Agent") || !strings.Contains(got[len(got)-1], "User: hello") {
		t.Fatalf("Alpha Agent prompt=%q", got[len(got)-1])
	}
	for _, item := range command.Env {
		if strings.HasPrefix(item, "ALPHA_NEXUS_AGENT_RUN_TOKEN=") {
			t.Fatalf("Alpha Agent Pi environment received a broker token: %q", item)
		}
	}
}

func TestAlphaAgentRunnerFailsClosedOnPiToolEvent(t *testing.T) {
	runner := piRunner{
		binary: func() string { return "pi" },
		command: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"session","id":"pi_session_1"}' '{"type":"tool_execution_start","toolName":"shell"}' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"must-not-reach"}}'`)
		},
	}
	var text strings.Builder
	_, err := runner.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), Message: "hello"}, func(event ProviderEvent) {
		text.WriteString(event.Payload["text"].(string))
	})
	if err == nil || !strings.Contains(err.Error(), "AGENT_PROVIDER_ALPHA_UNSAFE_TOOL_EVENT") {
		t.Fatalf("error=%v, want Pi tool event rejection", err)
	}
	if text.Len() != 0 {
		t.Fatalf("unsafe Pi event allowed a later message: %q", text.String())
	}
}

func TestAlphaAgentRunnerHonorsPiDeadline(t *testing.T) {
	runner := piRunner{
		binary: func() string { return "pi" },
		command: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", "sleep 10")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := runner.Run(ctx, RunRequest{WorkDir: t.TempDir(), Message: "hello"}, func(ProviderEvent) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want Pi deadline", err)
	}
}

func TestConsumePiJSONLAcceptsCurrentTextDeltaEvents(t *testing.T) {
	var text strings.Builder
	sessionID, receivedText, err := consumePiJSONL(strings.NewReader(strings.Join([]string{
		`{"type":"session","id":"pi_session_1"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"你"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"好"}}`,
		`{"type":"message_end"}`,
	}, "\n")), func(event ProviderEvent) {
		text.WriteString(event.Payload["text"].(string))
	})
	if err != nil || !receivedText || sessionID != "pi_session_1" || text.String() != "你好" {
		t.Fatalf("session=%q received=%v text=%q error=%v", sessionID, receivedText, text.String(), err)
	}
}

func TestProviderBrokerCapabilitiesMatchPrivateMCPRunners(t *testing.T) {
	config := OpenCodeLLMConfig{
		ProviderID: "alpha-nexus-llm",
		Name:       "test",
		BaseURL:    "https://example.test/v1",
		APIKey:     "not-used",
		Package:    "@ai-sdk/openai-compatible",
		Models:     map[string]string{"alpha-nexus-llm/test": "test"},
	}
	for name, provider := range map[string]Provider{
		"alpha":       alphaAgentProviderWithResolver(func() string { return "pi" }),
		"antigravity": antigravityProviderWithResolver(func() string { return "agy" }),
		"opencode":    openCodeProvider(),
		"custom":      customLLMProviderWithResolver(func() OpenCodeLLMConfig { return config }, func() string { return "opencode" }),
		"codex":       codexProvider(),
		"claude":      claudeProviderWithResolver(func() string { return "claude" }),
		"router":      claudeRouterProviderWithResolver(func() string { return "router" }, func() string { return "revision" }),
		"kimi":        kimiProviderWithLogin(func() string { return "kimi" }, func(context.Context, string) error { return nil }),
		"codebuddy":   codeBuddyProvider(func() string { return "codebuddy" }),
	} {
		want := name != "alpha" && name != "antigravity"
		if provider.SupportsBroker != want {
			t.Fatalf("provider %s supportsBroker=%v want=%v", name, provider.SupportsBroker, want)
		}
	}
}

func TestCodexDescriptorAdvertisesOnlyControlledRegistrySkills(t *testing.T) {
	codex := codexProvider()
	if !codex.SupportsBroker {
		t.Fatal("Codex must receive the run-scoped broker required for controlled Registry Skills")
	}
	for _, capability := range []string{"Registry Skills", "approval-gated Registry tools", "read-only sandbox", "run-scoped broker"} {
		if !containsString(codex.Descriptor.Capabilities, capability) {
			t.Fatalf("Codex capabilities=%q, missing %q", codex.Descriptor.Capabilities, capability)
		}
	}
	pi := alphaAgentProviderWithResolver(func() string { return "pi" })
	if pi.SupportsBroker {
		t.Fatal("Pi must remain chat/RAG-only without a broker")
	}
	for _, capability := range []string{"chat / RAG only", "no broker"} {
		if !containsString(pi.Descriptor.Capabilities, capability) {
			t.Fatalf("Pi capabilities=%q, missing %q", pi.Descriptor.Capabilities, capability)
		}
	}
}

func TestPiModelIDsParsesPublicModelTable(t *testing.T) {
	raw := `provider                  model                          context  max-out  thinking  images
modelscope                deepseek-ai/DeepSeek-V4-Flash  128K     8.2K     no        no
volcengine-coding         glm-5.2                        1M       16.4K    yes       no
bad provider              invalid model                  1M       1K       no        no`
	got := piModelIDs(raw)
	want := []string{"modelscope/deepseek-ai/DeepSeek-V4-Flash", "volcengine-coding/glm-5.2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Pi models=%q want=%q", got, want)
	}
}

func TestOpenCodeEphemeralRunDeletesProviderSession(t *testing.T) {
	var calls [][]string
	runner := openCodeRunner{
		binary: func() string { return "opencode" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			calls = append(calls, append([]string(nil), args...))
			if len(args) > 0 && args[0] == "run" {
				return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"text","sessionID":"ses_ephemeral","part":{"type":"text","text":"READY"}}'`)
			}
			return exec.CommandContext(ctx, "sh", "-c", `exit 0`)
		},
	}
	sessionID, err := runner.Run(
		context.Background(),
		RunRequest{WorkDir: t.TempDir(), ModelID: "opencode/test", Message: "check", Ephemeral: true},
		func(ProviderEvent) {},
	)
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "ses_ephemeral" {
		t.Fatalf("session=%q", sessionID)
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%q, want run followed by session cleanup", calls)
	}
	want := []string{"session", "delete", "ses_ephemeral"}
	if len(calls[1]) != len(want) {
		t.Fatalf("cleanup args=%q want=%q", calls[1], want)
	}
	for index := range want {
		if calls[1][index] != want[index] {
			t.Fatalf("cleanup args=%q want=%q", calls[1], want)
		}
	}
}

func TestOpenCodeEphemeralRunRequiresSessionIDForCleanup(t *testing.T) {
	runner := openCodeRunner{
		binary: func() string { return "opencode" },
		command: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"text","part":{"type":"text","text":"READY"}}'`)
		},
	}
	_, err := runner.Run(
		context.Background(),
		RunRequest{WorkDir: t.TempDir(), ModelID: "opencode/test", Message: "check", Ephemeral: true},
		func(ProviderEvent) {},
	)
	if err == nil || !strings.Contains(err.Error(), "AGENT_PROVIDER_OPENCODE_SESSION_CLEANUP_FAILED") {
		t.Fatalf("error=%v, want cleanup failure without a provider session id", err)
	}
}

func TestOpenCodeEphemeralRunRejectsExistingSession(t *testing.T) {
	called := false
	runner := openCodeRunner{
		binary: func() string { return "opencode" },
		command: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			called = true
			return exec.CommandContext(ctx, "sh", "-c", "exit 0")
		},
	}
	_, err := runner.Run(
		context.Background(),
		RunRequest{WorkDir: t.TempDir(), SessionID: "existing-session", Message: "check", Ephemeral: true},
		func(ProviderEvent) {},
	)
	if err == nil || !strings.Contains(err.Error(), "AGENT_PROVIDER_OPENCODE_EPHEMERAL_RESUME_REJECTED") {
		t.Fatalf("error=%v, want fail-closed ephemeral resume rejection", err)
	}
	if called {
		t.Fatal("OpenCode command ran before rejecting destructive ephemeral resume")
	}
}

func TestAntigravityRunnerUsesPrintAndWorkspaceContinue(t *testing.T) {
	var got []string
	var text strings.Builder
	workDir := t.TempDir()
	runner := antigravityRunner{
		binary: func() string { return "agy" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			got = append([]string(nil), args...)
			return exec.CommandContext(ctx, "sh", "-c", "printf READY")
		},
	}
	sessionID, err := runner.Run(context.Background(), RunRequest{WorkDir: workDir, SessionID: "workspace", ModelID: "gemini-3.6-flash-medium", Message: "private user message"}, func(event ProviderEvent) {
		text.WriteString(event.Payload["text"].(string))
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"--sandbox", "--mode", "plan", "--print-timeout", "2m", "--continue", "--model", "gemini-3.6-flash-medium", "--print"}
	if len(got) != len(wantPrefix)+1 {
		t.Fatalf("args=%q want-prefix=%q", got, wantPrefix)
	}
	for index := range wantPrefix {
		if got[index] != wantPrefix[index] {
			t.Fatalf("args=%q want-prefix=%q", got, wantPrefix)
		}
	}
	if !strings.Contains(got[len(got)-1], "User: private user message") {
		t.Fatalf("print prompt=%q", got[len(got)-1])
	}
	if text.String() != "READY" || sessionID != "workspace" {
		t.Fatalf("text=%q session=%q", text.String(), sessionID)
	}
}

func TestProviderEnvironmentDoesNotInheritParentSecrets(t *testing.T) {
	t.Setenv("AGENT_HUB_TEST_SECRET", "must-not-reach-provider")
	for _, pair := range providerEnvironment("/usr/local/bin/opencode", true) {
		if strings.HasPrefix(pair, "AGENT_HUB_TEST_SECRET=") {
			t.Fatalf("provider environment leaked %q", pair)
		}
	}
}

func TestCommandVersionCanUseSiblingLauncherDependency(t *testing.T) {
	binDir := t.TempDir()
	dependency := filepath.Join(binDir, "ccr")
	wrapper := filepath.Join(binDir, "claude-volcengine")
	if err := os.WriteFile(dependency, []byte("#!/bin/sh\nprintf '2.0.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec ccr \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")

	// This test covers launcher resolution, not the production cold-start
	// budget, so it uses a wider injectable timeout.
	version, err := commandVersionWithTimeout(context.Background(), wrapper, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if version != "2.0.0" {
		t.Fatalf("version=%q", version)
	}
}

func TestProviderPrivateMCPConfigurationIsRunScoped(t *testing.T) {
	request := RunRequest{
		WorkDir: t.TempDir(), RunID: "run_123", BrokerEndpoint: "http://127.0.0.1:17321",
		BrokerToken: strings.Repeat("a", 64), Executable: "/tmp/alpha-nexus",
	}
	configArgs := codexBrokerConfigArgs(request)
	joined := strings.Join(configArgs, " ")
	if !strings.Contains(joined, "mcp_servers.alpha_nexus.command") ||
		!strings.Contains(joined, `mcp_servers.alpha_nexus.env_vars=["ALPHA_NEXUS_AGENT_RUN_TOKEN"]`) ||
		!strings.Contains(joined, "mcp_servers.alpha_nexus.required=true") ||
		!strings.Contains(joined, `mcp_servers.alpha_nexus.default_tools_approval_mode="auto"`) ||
		strings.Contains(joined, request.BrokerToken) {
		t.Fatalf("unsafe Codex MCP config: %q", configArgs)
	}
	env := providerEnvironmentForRequest("/usr/local/bin/opencode", true, request)
	var config, token string
	for _, pair := range env {
		if strings.HasPrefix(pair, "OPENCODE_CONFIG_CONTENT=") {
			config = pair
		}
		if strings.HasPrefix(pair, "ALPHA_NEXUS_AGENT_RUN_TOKEN=") {
			token = pair
		}
	}
	if !strings.Contains(config, `"alpha_nexus"`) || !strings.Contains(config, `"permission"`) || token == "" {
		t.Fatalf("OpenCode private MCP not configured: config=%q token=%v", config, token != "")
	}
	cleanup, err := prepareAntigravityMCP(request)
	if err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(request.WorkDir, ".agents", "plugins", "alpha_nexus")
	configPath := filepath.Join(pluginDir, "mcp_config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), request.BrokerToken) || !strings.Contains(string(raw), `"alpha_nexus"`) {
		t.Fatalf("unsafe Antigravity MCP config: %s", raw)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Antigravity MCP config mode=%v", info.Mode())
	}
	manifestInfo, err := os.Stat(filepath.Join(pluginDir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifestInfo.Mode().Perm() != 0o600 {
		t.Fatalf("Antigravity plugin manifest mode=%v", manifestInfo.Mode())
	}
	cleanup()
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("temporary Antigravity MCP config was not removed: %v", err)
	}
}

func TestOpenCodeLLMOverlayUsesInlineConfigWithoutEmbeddingCredential(t *testing.T) {
	runtime := OpenCodeLLMConfig{
		ProviderID: "alpha-nexus-llm",
		Name:       "Alpha-Nexus LLM",
		BaseURL:    "https://gateway.example.test/v1",
		Package:    "@ai-sdk/openai-compatible",
		APIKey:     "synthetic-secret-must-not-enter-json",
		Models: map[string]string{
			"alpha-nexus-llm/vendor/coder": "vendor/coder",
		},
	}
	env := providerEnvironmentForRequest("/usr/local/bin/opencode", true, RunRequest{
		ModelID:     "alpha-nexus-llm/vendor/coder",
		OpenCodeLLM: &runtime,
	})
	var inlineConfig, credential string
	for _, pair := range env {
		switch {
		case strings.HasPrefix(pair, "OPENCODE_CONFIG_CONTENT="):
			inlineConfig = pair
		case strings.HasPrefix(pair, "ALPHA_NEXUS_LLM_API_KEY="):
			credential = pair
		}
	}
	if !strings.Contains(inlineConfig, `"alpha-nexus-llm"`) ||
		!strings.Contains(inlineConfig, `"vendor/coder"`) ||
		!strings.Contains(inlineConfig, `"{env:ALPHA_NEXUS_LLM_API_KEY}"`) {
		t.Fatalf("custom LLM missing from inline config: %q", inlineConfig)
	}
	if strings.Contains(inlineConfig, runtime.APIKey) {
		t.Fatal("credential leaked into OPENCODE_CONFIG_CONTENT")
	}
	if credential != "ALPHA_NEXUS_LLM_API_KEY="+runtime.APIKey {
		t.Fatal("credential was not passed through the isolated child environment")
	}
}

func TestCustomLLMProviderPublishesOnlyConfiguredModels(t *testing.T) {
	binDir := t.TempDir()
	binary := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '1.18.4\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtime := func() OpenCodeLLMConfig {
		return OpenCodeLLMConfig{
			ProviderID: "alpha-nexus-llm",
			Name:       "Alpha-Nexus LLM",
			BaseURL:    "https://gateway.example.test/v1",
			Package:    "@ai-sdk/openai-compatible",
			APIKey:     "synthetic-key",
			Models: map[string]string{
				"alpha-nexus-llm/vendor/coder": "vendor/coder",
			},
		}
	}
	provider := customLLMProviderWithResolver(runtime, func() string { return binary })
	probe := provider.Probe(context.Background())
	if !probe.Ready || probe.Version != "1.18.4" {
		t.Fatalf("probe=%+v", probe)
	}
	if diff := strings.Join(probe.ModelCandidates, ","); diff != "alpha-nexus-llm/vendor/coder" {
		t.Fatalf("candidates=%q", diff)
	}
}

func TestDefaultRegistryStartsWithAlphaAgentThenLegacyProviders(t *testing.T) {
	registry, err := DefaultRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get(ProviderID("api")); ok {
		t.Fatal("direct API must not be registered as an Agent provider")
	}
	descriptors := registry.Descriptors(context.Background())
	if len(descriptors) != 4 || descriptors[0].ID != ProviderDsh || descriptors[0].Label != "DSH Agent" || descriptors[1].ID != ProviderAlpha || descriptors[2].ID != ProviderOpenCode || descriptors[3].ID != ProviderCodex {
		t.Fatalf("default providers=%v, want DSH Agent then Alpha Agent, OpenCode and Codex", descriptors)
	}
	for _, id := range []ProviderID{ProviderDsh, ProviderAlpha, ProviderOpenCode, ProviderCodex} {
		provider, ok := registry.Get(id)
		if !ok {
			t.Fatalf("missing CLI provider %q", id)
		}
		if provider.ValidateModel == nil {
			t.Fatalf("provider %q cannot validate real models", id)
		}
	}
	if _, ok := registry.Get(ProviderAntigravity); ok {
		t.Fatal("Antigravity must be user-added instead of hardcoded")
	}
}

func TestDefaultRegistryAddsCataloguedProvidersAndRestoresClaudeCode(t *testing.T) {
	registry, err := DefaultRegistry(t.TempDir(), string(ProviderAntigravity), string(ProviderClaude))
	if err != nil {
		t.Fatal(err)
	}
	descriptors := registry.Descriptors(context.Background())
	want := []ProviderID{ProviderDsh, ProviderAlpha, ProviderOpenCode, ProviderCodex, ProviderAntigravity, ProviderClaude}
	if len(descriptors) != len(want) {
		t.Fatalf("providers=%v want=%v", descriptors, want)
	}
	for index, id := range want {
		if descriptors[index].ID != id {
			t.Fatalf("providers[%d]=%q want=%q", index, descriptors[index].ID, id)
		}
	}
	for index := 0; index < 4; index++ {
		if descriptors[index].Removable {
			t.Fatalf("built-in provider %q must not be removable", descriptors[index].ID)
		}
	}
	if !descriptors[4].Removable || !descriptors[5].Removable {
		t.Fatal("optional providers must be removable")
	}
	if _, ok := registry.Get(ProviderClaude); !ok {
		t.Fatal("saved Claude Code provider must be restored")
	}
}

func TestProviderCatalogOffersVerifiedCLIAdapters(t *testing.T) {
	items := ProviderCatalog()
	if len(items) != 4 {
		t.Fatalf("catalog=%v", items)
	}
	want := []ProviderID{ProviderClaude, ProviderKimi, ProviderCodeBuddy, ProviderAntigravity}
	for index, id := range want {
		if items[index].ID != id {
			t.Fatalf("catalog[%d]=%q, want %q", index, items[index].ID, id)
		}
	}
	for index := range items {
		if !items[index].Supported || !items[index].ExecutableConfigurable {
			t.Fatalf("catalog item is not configurable: %+v", items[index])
		}
	}
	if items[2].Label != "CodeBuddy Code" || strings.Contains(items[2].Label, "WorkBuddy") {
		t.Fatalf("CodeBuddy CLI was mislabeled as another product: %+v", items[2])
	}
}

func TestClaudeProviderConnectUsesConfiguredExecutable(t *testing.T) {
	provider := claudeProviderWithResolver(func() string { return "/opt/local/bin/claude" })
	if err := provider.Connect(context.Background()); err != nil {
		t.Fatalf("connect configured Claude Code executable: %v", err)
	}
}

func TestClaudeAuthStatusRequiresLoggedInJSON(t *testing.T) {
	if !claudeAuthStatusReady([]byte(`{"loggedIn":true,"authMethod":"oauth_token"}`)) {
		t.Fatal("valid Claude login was rejected")
	}
	for _, raw := range [][]byte{
		[]byte(`{"loggedIn":false}`),
		[]byte(`{"authMethod":"oauth_token"}`),
		[]byte(`not json`),
	} {
		if claudeAuthStatusReady(raw) {
			t.Fatalf("invalid Claude auth status accepted: %s", raw)
		}
	}
}

func TestClaudeModelCandidatesExcludeDefaultAndSpecialModes(t *testing.T) {
	got := claudeModelCandidates()
	want := []string{"best", "fable", "sonnet", "opus", "haiku"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Claude candidates=%q want=%q", got, want)
	}
}

func TestClaudeCCSwitchRunnerLoadsOnlyExplicitSettings(t *testing.T) {
	var args []string
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	runner := claudeRunner{
		binary:   func() string { return "claude" },
		settings: func() string { return settingsPath },
		command: func(ctx context.Context, _ string, commandArgs ...string) *exec.Cmd {
			args = append([]string(nil), commandArgs...)
			return exec.CommandContext(ctx, "sh", "-c", `cat >/dev/null; printf '%s\n' '{"type":"assistant","session_id":"session_1","message":{"content":[{"type":"text","text":"READY"}]}}'`)
		},
	}
	if _, err := runner.Run(
		context.Background(),
		RunRequest{WorkDir: t.TempDir(), ModelID: "grok/grok-build", Message: "hello", Ephemeral: true},
		func(ProviderEvent) {},
	); err != nil {
		t.Fatal(err)
	}
	if !containsArgument(args, "--bare") || !containsArgumentPair(args, "--settings", settingsPath) {
		t.Fatalf("Claude did not use the explicit CC Switch settings: %q", args)
	}
	if containsArgument(args, "--safe-mode") || containsArgument(args, "--setting-sources") {
		t.Fatalf("Claude mixed CC Switch settings with another settings mode: %q", args)
	}
	if !containsArgumentPair(args, "--tools", "") || !containsArgument(args, "--strict-mcp-config") {
		t.Fatalf("Claude CC Switch run weakened tool isolation: %q", args)
	}
}

func TestCCSwitchRouteVersionChangesWithSettingsMetadata(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := claudeCCSwitchRouteVersion("2.1.220", settingsPath)
	if first == "" || first == "2.1.220" || strings.Contains(first, settingsPath) {
		t.Fatalf("route version=%q", first)
	}
	if err := os.WriteFile(settingsPath, []byte("{\"env\":{}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := claudeCCSwitchRouteVersion("2.1.220", settingsPath)
	if second == first {
		t.Fatalf("settings change did not invalidate model cache: %q", second)
	}
}

func TestCodeBuddyModelsFromHelpKeepsActualModelsOnly(t *testing.T) {
	raw := `--model <model>  Currently supported: (default-model, gemini-3.1-pro,
  gemini-3.5-flash, gpt-5.4, glm-5.0, kimi-k2.5)`
	got := codeBuddyModelsFromHelp(raw)
	want := []string{"gemini-3.1-pro", "gemini-3.5-flash", "gpt-5.4", "glm-5.0", "kimi-k2.5"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("CodeBuddy models=%q want=%q", got, want)
	}
}

func TestACPModelsFromSessionResultUsesOnlyModelConfigOptions(t *testing.T) {
	raw := []byte(`{
		"sessionId":"session_1",
		"configOptions":[
			{"type":"select","id":"model","category":"model","currentValue":"kimi-k2.5","options":[
				{"value":"kimi-k2.5","name":"Kimi K2.5"},
				{"value":"default-model","name":"Default"},
				{"value":"bad model id","name":"Bad"}
			]},
			{"type":"select","id":"mode","category":"mode","currentValue":"default","options":[
				{"value":"default","name":"Default"}
			]}
		]
	}`)
	got := acpModelsFromSessionResult(raw)
	want := []string{"kimi-k2.5"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ACP models=%q want=%q", got, want)
	}
}

func TestKimiACPModelProbeDiscoversNegotiatedModels(t *testing.T) {
	command := exec.CommandContext(context.Background(), "sh", "-c", `
read initialize
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true},"authMethods":[]}}'
read session
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"session_1","configOptions":[{"type":"select","id":"model","category":"model","currentValue":"kimi-k2.5","options":[{"value":"kimi-k2.5","name":"Kimi K2.5"},{"value":"moonshot-v1","name":"Moonshot"}]}]}}'
`)
	models, err := discoverACPModelsFromCommand(context.Background(), command, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kimi-k2.5", "moonshot-v1"}
	if strings.Join(models, ",") != strings.Join(want, ",") {
		t.Fatalf("models=%q want=%q", models, want)
	}
}

func TestKimiACPRunnerUsesNegotiatedModelAndPrivateMCP(t *testing.T) {
	requestsPath := filepath.Join(t.TempDir(), "requests.jsonl")
	var text strings.Builder
	runner := kimiACPRunner{
		binary: func() string { return "kimi" },
		command: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			script := `
read initialize
printf '%s\n' "$initialize" >> "$REQUESTS_PATH"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true},"authMethods":[]}}'
read session
printf '%s\n' "$session" >> "$REQUESTS_PATH"
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"session_2","configOptions":[{"type":"select","id":"model","category":"model","currentValue":"kimi-k2.5","options":[{"value":"kimi-k2.5","name":"Kimi K2.5"}]}]}}'
read model
printf '%s\n' "$model" >> "$REQUESTS_PATH"
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"configOptions":[{"type":"select","id":"model","category":"model","currentValue":"kimi-k2.5","options":[{"value":"kimi-k2.5","name":"Kimi K2.5"}]}]}}'
read prompt
printf '%s\n' "$prompt" >> "$REQUESTS_PATH"
printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session_2","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"READY"}}}}'
printf '%s\n' '{"jsonrpc":"2.0","id":4,"result":{"stopReason":"end_turn"}}'
`
			command := exec.CommandContext(ctx, "sh", "-c", script)
			command.Env = append(os.Environ(), "REQUESTS_PATH="+requestsPath)
			return command
		},
	}
	sessionID, err := runner.Run(context.Background(), RunRequest{
		WorkDir:        t.TempDir(),
		ModelID:        "kimi-k2.5",
		Message:        "reply with READY",
		BrokerToken:    "one-time-token",
		BrokerEndpoint: "http://127.0.0.1:17321",
		RunID:          "run_1",
		Executable:     "/Applications/Alpha-Nexus",
	}, func(event ProviderEvent) {
		if value, _ := event.Payload["text"].(string); value != "" {
			text.WriteString(value)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "session_2" || text.String() != "READY" {
		t.Fatalf("session=%q text=%q", sessionID, text.String())
	}
	raw, err := os.ReadFile(requestsPath)
	if err != nil {
		t.Fatal(err)
	}
	requests := string(raw)
	for _, fragment := range []string{
		`"method":"session/new"`,
		`"name":"alpha_nexus"`,
		`"ALPHA_NEXUS_AGENT_RUN_TOKEN"`,
		`"method":"session/set_config_option"`,
		`"value":"kimi-k2.5"`,
		`"method":"session/prompt"`,
	} {
		if !strings.Contains(requests, fragment) {
			t.Fatalf("ACP request missing %q: %s", fragment, requests)
		}
	}
}

func TestProviderCatalogDefinitionsAreUniqueAndActionable(t *testing.T) {
	seen := map[ProviderID]bool{}
	for _, item := range ProviderCatalog() {
		if seen[item.ID] {
			t.Fatalf("duplicate provider catalog id %q", item.ID)
		}
		seen[item.ID] = true
		if item.PrimaryCommand == "" || len(item.VersionArgs) == 0 {
			t.Fatalf("provider %q has no CLI detection contract: %+v", item.ID, item)
		}
		if item.Transport == "" || item.AuthMode == "" || item.ModelSource == "" || item.MCPInjection == "" {
			t.Fatalf("provider %q has incomplete adapter metadata: %+v", item.ID, item)
		}
		if item.IntegrationKind != "right_agent" {
			t.Fatalf("provider %q integration_kind=%q", item.ID, item.IntegrationKind)
		}
		if _, ok := optionalProviderBuilders[item.ID]; !ok {
			t.Fatalf("provider %q has metadata but no runtime builder", item.ID)
		}
	}
	for _, id := range []ProviderID{ProviderClaude, ProviderKimi, ProviderCodeBuddy, ProviderAntigravity} {
		if !seen[id] {
			t.Fatalf("provider %q missing from catalog", id)
		}
	}
}

func TestKimiAndCodeBuddyConnectWhenConfiguredCLIExists(t *testing.T) {
	kimiLoginCalled := false
	for name, provider := range map[string]Provider{
		"kimi": kimiProviderWithLogin(func() string { return "/opt/local/bin/kimi" }, func(_ context.Context, binary string) error {
			kimiLoginCalled = binary == "/opt/local/bin/kimi"
			return nil
		}),
		"codebuddy": codeBuddyProvider(func() string { return "/opt/local/bin/codebuddy" }),
	} {
		if err := provider.Connect(context.Background()); err != nil {
			t.Fatalf("connect configured %s executable: %v", name, err)
		}
	}
	if !kimiLoginCalled {
		t.Fatal("Kimi connection did not start the official ACP device-code login")
	}
}

func TestClaudeRunnerUsesPrivateMCPAndNoBuiltInTools(t *testing.T) {
	var args []string
	var stdin string
	runner := claudeRunner{
		binary: func() string { return "claude" },
		command: func(ctx context.Context, _ string, commandArgs ...string) *exec.Cmd {
			args = append([]string(nil), commandArgs...)
			script := `input=$(cat); printf '%s\n' '{"type":"assistant","session_id":"session_1","message":{"content":[{"type":"text","text":"READY"}]}}' '{"type":"result","session_id":"session_1","is_error":false,"result":"READY"}'; printf '%s' "$input" >&2`
			command := exec.CommandContext(ctx, "sh", "-c", script)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			return command
		},
	}
	request := RunRequest{
		WorkDir:        t.TempDir(),
		RunID:          "run_123",
		BrokerEndpoint: "http://127.0.0.1:17321",
		BrokerToken:    strings.Repeat("a", 64),
		Executable:     "/tmp/alpha-nexus",
		ModelID:        "claude-sonnet-4",
		Message:        "hello",
	}
	sessionID, err := runner.Run(context.Background(), request, func(event ProviderEvent) {
		if text, _ := event.Payload["text"].(string); text != "" {
			stdin += text
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "session_1" || stdin != "READY" {
		t.Fatalf("session=%q text=%q", sessionID, stdin)
	}
	if !containsArgument(args, "--strict-mcp-config") || !containsArgument(args, "--mcp-config") {
		t.Fatalf("Claude args do not pin private MCP: %q", args)
	}
	if !containsArgumentPair(args, "--tools", "") {
		t.Fatalf("Claude built-in tools were not disabled: %q", args)
	}
	if !containsArgument(args, "--safe-mode") {
		t.Fatalf("Claude customizations were not disabled: %q", args)
	}
	if containsArgumentPair(args, "--setting-sources", "") {
		t.Fatalf("Claude authentication was disabled with all setting sources: %q", args)
	}
}

func TestClaudeEphemeralRunDisablesSessionPersistence(t *testing.T) {
	var args []string
	runner := claudeRunner{
		binary: func() string { return "claude" },
		command: func(ctx context.Context, _ string, commandArgs ...string) *exec.Cmd {
			args = append([]string(nil), commandArgs...)
			return exec.CommandContext(ctx, "sh", "-c", `cat >/dev/null; printf '%s\n' '{"type":"assistant","session_id":"session_1","message":{"content":[{"type":"text","text":"READY"}]}}'`)
		},
	}
	if _, err := runner.Run(
		context.Background(),
		RunRequest{WorkDir: t.TempDir(), ModelID: "claude-sonnet-4", Message: "hello", Ephemeral: true},
		func(ProviderEvent) {},
	); err != nil {
		t.Fatal(err)
	}
	if !containsArgument(args, "--no-session-persistence") {
		t.Fatalf("Claude ephemeral session was persisted: %q", args)
	}
}

func TestCodeBuddyRunnerAllowsOnlyPrivateMCPTools(t *testing.T) {
	var args []string
	runner := codeBuddyRunner{
		binary: func() string { return "codebuddy" },
		command: func(ctx context.Context, _ string, commandArgs ...string) *exec.Cmd {
			args = append([]string(nil), commandArgs...)
			return exec.CommandContext(ctx, "sh", "-c", `cat >/dev/null; printf '%s\n' '{"type":"assistant","session_id":"session_1","message":{"content":[{"type":"text","text":"READY"}]}}'`)
		},
	}
	request := RunRequest{
		WorkDir:        t.TempDir(),
		RunID:          "run_123",
		BrokerEndpoint: "http://127.0.0.1:17321",
		BrokerToken:    strings.Repeat("a", 64),
		Executable:     "/tmp/alpha-nexus",
		ModelID:        "glm-5",
		Message:        "hello",
	}
	if _, err := runner.Run(context.Background(), request, func(ProviderEvent) {}); err != nil {
		t.Fatal(err)
	}
	if !containsArgumentPair(args, "--allowedTools", "mcp__alpha_nexus__*") {
		t.Fatalf("CodeBuddy private tool allowlist missing: %q", args)
	}
	if !containsArgumentPair(args, "--setting-sources", "") {
		t.Fatalf("CodeBuddy loaded user/project settings: %q", args)
	}
	if !containsArgumentPair(args, "--tools", "") {
		t.Fatalf("CodeBuddy built-in tools were not disabled: %q", args)
	}
	if !containsArgument(args, "--strict-mcp-config") || !containsArgument(args, "--mcp-config") {
		t.Fatalf("CodeBuddy did not pin the private MCP config: %q", args)
	}
	if containsArgument(args, "-y") || containsArgument(args, "--dangerously-skip-permissions") {
		t.Fatalf("CodeBuddy bypassed permissions: %q", args)
	}
}

func TestCodeBuddyEphemeralRunDisablesSessionPersistence(t *testing.T) {
	var args []string
	runner := codeBuddyRunner{
		binary: func() string { return "codebuddy" },
		command: func(ctx context.Context, _ string, commandArgs ...string) *exec.Cmd {
			args = append([]string(nil), commandArgs...)
			return exec.CommandContext(ctx, "sh", "-c", `cat >/dev/null; printf '%s\n' '{"type":"assistant","session_id":"session_1","message":{"content":[{"type":"text","text":"READY"}]}}'`)
		},
	}
	if _, err := runner.Run(
		context.Background(),
		RunRequest{WorkDir: t.TempDir(), ModelID: "glm-5", Message: "hello", Ephemeral: true},
		func(ProviderEvent) {},
	); err != nil {
		t.Fatal(err)
	}
	if !containsArgument(args, "--no-session-persistence") {
		t.Fatalf("CodeBuddy ephemeral session was persisted: %q", args)
	}
}

func TestKimiRunnerUsesExplicitToolIsolatedAgent(t *testing.T) {
	var args []string
	var agentFile string
	var mcpFile string
	runner := kimiRunner{
		binary: func() string { return "kimi" },
		command: func(ctx context.Context, _ string, commandArgs ...string) *exec.Cmd {
			args = append([]string(nil), commandArgs...)
			for index := 0; index+1 < len(commandArgs); index++ {
				switch commandArgs[index] {
				case "--agent-file":
					agentFile = commandArgs[index+1]
				}
			}
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"role":"assistant","content":"READY"}' '{"role":"meta","type":"session.resume_hint","session_id":"kimi_session_1"}'`)
		},
	}
	request := RunRequest{
		WorkDir:        t.TempDir(),
		RunID:          "run_123",
		BrokerEndpoint: "http://127.0.0.1:17321",
		BrokerToken:    strings.Repeat("a", 64),
		Executable:     "/tmp/alpha-nexus",
		ModelID:        "kimi-code/kimi-for-coding",
		Message:        "hello",
	}
	sessionID, err := runner.Run(context.Background(), request, func(ProviderEvent) {})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "kimi_session_1" {
		t.Fatalf("Kimi session was not captured: %q", sessionID)
	}
	rawAgent, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rawAgent, []byte("mcp__alpha_nexus__*")) || bytes.Contains(rawAgent, []byte("${base_prompt}")) {
		t.Fatalf("unsafe Kimi agent file: %s", rawAgent)
	}
	mcpFile = filepath.Join(request.WorkDir, ".kimi-code", "mcp.json")
	rawMCP, err := os.ReadFile(mcpFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rawMCP, []byte(`"alpha_nexus"`)) {
		t.Fatalf("Kimi private MCP config missing: %s", rawMCP)
	}
	if containsArgument(args, "--yolo") || containsArgument(args, "--auto") {
		t.Fatalf("Kimi unsafe approval mode: %q", args)
	}
	if containsArgument(args, "--final-message-only") {
		t.Fatalf("Kimi runner used removed CLI flag: %q", args)
	}
}

func TestAntigravity116KeepsChatReadyWithoutPrivatePerRunMCP(t *testing.T) {
	probe := applyProviderCompatibility(ProviderProbe{
		ProviderID: ProviderAntigravity,
		Available:  true,
		Ready:      true,
		Version:    "agy 1.1.6",
	})
	if probe.State != ProviderStateLimited || !probe.Ready || !strings.Contains(probe.Reason, "对话已连接") {
		t.Fatalf("probe=%+v", probe)
	}
}

func TestAntigravity117StaysLimitedUntilPrivatePerRunMCPIsVerified(t *testing.T) {
	probe := applyProviderCompatibility(ProviderProbe{
		ProviderID: ProviderAntigravity,
		Available:  true,
		Ready:      true,
		Version:    "agy 1.1.7",
	})
	if probe.State != ProviderStateLimited || !probe.Ready || !strings.Contains(probe.Reason, "仅限聊天") {
		t.Fatalf("probe=%+v", probe)
	}
}

func TestAntigravityRunnerDoesNotAdvertiseUnsupportedPrivateBroker(t *testing.T) {
	var prompt string
	var commandArgs []string
	runner := antigravityRunner{
		binary: func() string { return "agy" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			commandArgs = append([]string(nil), args...)
			prompt = args[len(args)-1]
			return exec.CommandContext(ctx, "sh", "-c", "printf READY")
		},
	}
	request := RunRequest{
		WorkDir:        t.TempDir(),
		RunID:          "run_123",
		BrokerEndpoint: "http://127.0.0.1:17321",
		BrokerToken:    strings.Repeat("a", 64),
		Executable:     "/tmp/alpha-nexus",
		Message:        "hello",
	}
	if _, err := runner.Run(context.Background(), request, func(ProviderEvent) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "There are no business tools in this run.") || strings.Contains(prompt, "Use only the alpha_") {
		t.Fatalf("Antigravity prompt advertised an unavailable private broker: %q", prompt)
	}
	if containsArgument(commandArgs, "--dangerously-skip-permissions") {
		t.Fatalf("Antigravity runner bypassed CLI permission checks: %q", commandArgs)
	}
}

func TestProviderModelIDsKeepsOnlySafeUniqueValues(t *testing.T) {
	got := providerModelIDs("gemini-3.6-flash-medium\ninvalid model\ngemini-3.6-flash-medium\nopencode/big-pickle\n")
	want := []string{"gemini-3.6-flash-medium", "opencode/big-pickle"}
	if len(got) != len(want) {
		t.Fatalf("models=%q want=%q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("models=%q want=%q", got, want)
		}
	}
}

func TestProviderModelIDsAcceptsAntigravityLabeledCatalog(t *testing.T) {
	got := providerModelIDs(strings.Join([]string{
		"gemini-3.7-flash-low\tGemini 3.7 Flash (Low)",
		"claude-sonnet-4-6\tClaude Sonnet 4.6 (Thinking)",
	}, "\n"))
	want := []string{"gemini-3.7-flash-low", "claude-sonnet-4-6"}
	if len(got) != len(want) {
		t.Fatalf("models=%q want=%q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("models=%q want=%q", got, want)
		}
	}
}

func TestProviderModelIDsKeepsProxyModelsBeyondLegacyCatalogLimit(t *testing.T) {
	var catalog strings.Builder
	for index := 0; index < 160; index++ {
		fmt.Fprintf(&catalog, "alibaba/model-%03d\n", index)
	}
	catalog.WriteString("proxy/deepseek-v4-flash-free\n")
	got := providerModelIDs(catalog.String())
	if len(got) != 161 || got[len(got)-1] != "proxy/deepseek-v4-flash-free" {
		t.Fatalf("proxy model was truncated from catalog: count=%d last=%q", len(got), got[len(got)-1])
	}
}

func TestModelValidationRequiresExactNonceResponse(t *testing.T) {
	const nonce = "check_123"
	runner := scriptedRunner{events: []ProviderEvent{{Type: "message.delta", Payload: map[string]any{"text": nonce}}}}
	if _, err := runModelValidation(context.Background(), runner, ProviderCodex, "gpt-5", t.TempDir(), nonce); err != nil {
		t.Fatalf("exact nonce rejected: %v", err)
	}
	runner.events[0].Payload["text"] = "connected"
	if _, err := runModelValidation(context.Background(), runner, ProviderCodex, "gpt-5", t.TempDir(), nonce); err == nil {
		t.Fatal("non-nonce response must not verify a model")
	}
}

func TestRealInstalledProviderModel(t *testing.T) {
	id := ProviderID(strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_PROVIDER")))
	if id == "" {
		t.Skip("set ALPHA_NEXUS_REAL_PROVIDER and ALPHA_NEXUS_REAL_MODEL for an explicit live smoke")
	}
	modelID := strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_MODEL"))
	if modelID == "" {
		t.Fatal("ALPHA_NEXUS_REAL_MODEL is required for a live smoke")
	}
	var provider Provider
	var ok bool
	switch id {
	case ProviderOpenCode:
		provider, ok = openCodeProvider(), true
	case ProviderCodex:
		provider, ok = codexProvider(), true
	default:
		provider, ok = providerFromConfig(ProviderConfig{
			ProviderID:     id,
			ExecutablePath: strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_EXECUTABLE")),
			LaunchMode:     ProviderLaunchMode(strings.TrimSpace(os.Getenv("ALPHA_NEXUS_REAL_LAUNCH_MODE"))),
		})
	}
	if !ok || provider.ValidateModel == nil {
		t.Fatalf("provider %q has no real model validator", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
	defer cancel()
	probe := provider.Probe(ctx)
	if !probe.Available || !probe.Ready {
		t.Fatalf("provider %q is not connected: %s", id, probe.Reason)
	}
	nonce := fmt.Sprintf("alpha_nexus_real_%s_%d", id, time.Now().UnixNano())
	if err := provider.ValidateModel(ctx, modelID, t.TempDir(), nonce); err != nil {
		t.Fatalf("provider %q model %q failed exact-response smoke: %v", id, modelID, err)
	}
}

func TestEphemeralModelValidationUsesNonPersistentCLIFlags(t *testing.T) {
	var codexArgs []string
	codex := codexRunner{
		binary: func() string { return "codex" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			codexArgs = append([]string(nil), args...)
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"thread.started","thread_id":"thread_1"}'`)
		},
	}
	if _, err := codex.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), ModelID: "gpt-5", Message: "check", Ephemeral: true}, func(ProviderEvent) {}); err != nil {
		t.Fatal(err)
	}
	if !containsArgument(codexArgs, "--ephemeral") {
		t.Fatalf("Codex validation args=%q", codexArgs)
	}

	var openCodeArgs []string
	openCode := openCodeRunner{
		binary: func() string { return "opencode" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			if len(args) > 0 && args[0] == "run" {
				openCodeArgs = append([]string(nil), args...)
			}
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' '{"type":"text","sessionID":"ses_check","part":{"type":"text","text":"check"}}'`)
		},
	}
	if _, err := openCode.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), ModelID: "opencode/test", Message: "check", Ephemeral: true}, func(ProviderEvent) {}); err != nil {
		t.Fatal(err)
	}
	if !containsArgument(openCodeArgs, "--pure") || !containsArgument(openCodeArgs, "--title") {
		t.Fatalf("OpenCode validation args=%q", openCodeArgs)
	}

	var antigravityArgs []string
	antigravity := antigravityRunner{
		binary: func() string { return "agy" },
		command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			antigravityArgs = append([]string(nil), args...)
			return exec.CommandContext(ctx, "sh", "-c", "printf check")
		},
	}
	if _, err := antigravity.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), ModelID: "gemini-3.6-flash-medium", Message: "check", Ephemeral: true}, func(ProviderEvent) {}); err != nil {
		t.Fatal(err)
	}
	if !containsArgument(antigravityArgs, "--new-project") {
		t.Fatalf("Antigravity validation args=%q", antigravityArgs)
	}
}

func containsArgument(args []string, want string) bool {
	for _, argument := range args {
		if argument == want {
			return true
		}
	}
	return false
}

func containsArgumentPair(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

func TestCodexModelFromDoctorJSONReturnsOnlyUnambiguousModel(t *testing.T) {
	if got := codexModelFromDoctorJSON([]byte(`{"checks":[{"details":{"model":"gpt-5.6-sol","model provider":"openai"}}]}`)); got != "gpt-5.6-sol" {
		t.Fatalf("model=%q", got)
	}
	if got := codexModelFromDoctorJSON([]byte(`{"first":{"model":"model-a"},"second":{"model":"model-b"}}`)); got != "" {
		t.Fatalf("ambiguous model=%q", got)
	}
	if got := codexModelFromDoctorJSON([]byte("diagnostic\n{\"model\": \"gpt-5.6-sol\"}")); got != "gpt-5.6-sol" {
		t.Fatalf("fallback model=%q", got)
	}
}

func TestCodexModelsFromAppServerResponseKeepsPublicVisibleModels(t *testing.T) {
	raw := []byte(`{"id":2,"result":{"data":[{"model":"gpt-5.6-sol","hidden":false},{"model":"gpt-5.6-sol","hidden":false},{"model":"hidden-model","hidden":true},{"model":"invalid model","hidden":false}]}}`)
	got, ok := codexModelsFromAppServerResponse(raw)
	if !ok {
		t.Fatal("model/list response was not recognized")
	}
	if len(got) != 1 || got[0] != "gpt-5.6-sol" {
		t.Fatalf("models=%q", got)
	}
	if _, ok := codexModelsFromAppServerResponse([]byte(`{"id":1,"result":{}}`)); ok {
		t.Fatal("initialize response must not be treated as a model list")
	}
}

func TestCodexLoginStatusBudgetMatchesColdSignedCLIStarts(t *testing.T) {
	if codexLoginStatusTimeout != 8*time.Second {
		t.Fatalf("codexLoginStatusTimeout=%v, want 8s to cover signed CLI cold starts", codexLoginStatusTimeout)
	}
}

func TestCodexLoginStatusAbortsSlowBinaries(t *testing.T) {
	binDir := t.TempDir()
	slow := filepath.Join(binDir, "codex-slow-login")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := codexLoginStatusTimeout
	codexLoginStatusTimeout = 50 * time.Millisecond
	defer func() { codexLoginStatusTimeout = previous }()

	started := time.Now()
	if err := codexLoginStatus(context.Background(), slow); err == nil {
		t.Fatal("slow login status must fail the probe budget")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("login status returned after %v, timeout did not abort the command", elapsed)
	}
}

func TestResolveCodexFallsBackToLocalBin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(localBin, "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'codex-cli 0.0.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveCodex(); got != fake {
		t.Fatalf("resolveCodex()=%q, want the user-local install %q over PATH misses and the bundled CLI", got, fake)
	}
}
