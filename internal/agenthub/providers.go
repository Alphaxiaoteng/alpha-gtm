package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/cliresolver"
)

var errProviderUnavailable = errors.New("AGENT_PROVIDER_UNAVAILABLE")

var codexDoctorModelPattern = regexp.MustCompile(`"model"\s*:\s*"([A-Za-z0-9._:/-]{1,120})"`)
var codeBuddySupportedModelsPattern = regexp.MustCompile(`(?s)Currently supported:\s*\(([^)]*)\)`)
var piModelColumnsPattern = regexp.MustCompile(`\s{2,}`)
var piSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Codex documents this as its supported per-run config override. OpenCode's
// --variant is model/provider-specific and Antigravity exposes no equivalent
// non-interactive setting, so neither receives a guessed universal value.
const codexDefaultReasoningEffort = "medium"

type providerResolverFactory func(...string) func() string
type providerBuilder func(ProviderConfig, providerResolverFactory) Provider

// optionalProviderBuilders is the executable half of the declarative catalog.
// Catalog metadata and builders share the same ProviderID; unknown products
// cannot fall through to a guessed shell command or arbitrary arguments.
var optionalProviderBuilders = map[ProviderID]providerBuilder{
	ProviderClaude: func(config ProviderConfig, resolve providerResolverFactory) Provider {
		claudeResolve := resolve("claude", "claude-code")
		if strings.TrimSpace(config.ExecutablePath) == "" {
			claudeResolve = resolveClaude
		}
		switch config.LaunchMode {
		case ProviderLaunchModeCCSwitch:
			return claudeCCSwitchProviderWithResolver(claudeResolve, resolveCCSwitchClaudeSettings)
		case ProviderLaunchModeRouter:
			routerResolve := resolve("claude-volcengine", "claude-router")
			if strings.TrimSpace(config.ExecutablePath) == "" {
				routerResolve = resolveClaudeRouter
			}
			return claudeRouterProviderWithResolver(routerResolve, resolveClaudeRouterConfig)
		default:
			return claudeProviderWithResolver(claudeResolve)
		}
	},
	ProviderKimi: func(config ProviderConfig, resolve providerResolverFactory) Provider {
		if strings.TrimSpace(config.ExecutablePath) == "" {
			return kimiProvider(resolveKimi)
		}
		return kimiProvider(resolve("kimi"))
	},
	ProviderCodeBuddy: func(config ProviderConfig, resolve providerResolverFactory) Provider {
		if strings.TrimSpace(config.ExecutablePath) == "" {
			return codeBuddyProvider(resolveCodeBuddy)
		}
		return codeBuddyProvider(resolve("codebuddy", "cbc"))
	},
	ProviderAntigravity: func(config ProviderConfig, resolve providerResolverFactory) Provider {
		if strings.TrimSpace(config.ExecutablePath) == "" {
			return antigravityProvider()
		}
		return antigravityProviderWithResolver(resolve("agy"))
	},
}

type unavailableRunner struct{ code string }

func (r unavailableRunner) Run(context.Context, RunRequest, func(ProviderEvent)) (string, error) {
	return "", errors.New(r.code)
}

type commandProbe struct {
	id      ProviderID
	binary  func() string
	ready   bool
	reason  string
	version func(context.Context, string) (string, error)
}

func (p commandProbe) Probe(ctx context.Context) ProviderProbe {
	binary := strings.TrimSpace(p.binary())
	if binary == "" {
		return ProviderProbe{ProviderID: p.id, Available: false, Ready: false, Reason: p.reason}
	}
	probe := ProviderProbe{ProviderID: p.id, Available: true, Ready: p.ready}
	if p.version != nil {
		version, err := p.version(ctx, binary)
		if err != nil {
			probe.Ready = false
			probe.Reason = "version probe failed"
		} else {
			probe.Version = version
		}
	}
	if !probe.Ready && probe.Reason == "" {
		probe.Reason = p.reason
	}
	return probe
}

func DefaultRegistry(_ string, optionalProviderIDs ...string) (*Registry, error) {
	configs := make([]ProviderConfig, 0, len(optionalProviderIDs))
	for _, id := range optionalProviderIDs {
		configs = append(configs, ProviderConfig{ProviderID: ProviderID(strings.TrimSpace(id))})
	}
	return DefaultRegistryWithConfigs(configs)
}

func DefaultRegistryWithConfigs(configs []ProviderConfig) (*Registry, error) {
	providers := []Provider{dshProvider(), alphaAgentProvider(), openCodeProvider(), codexProvider()}
	seen := map[ProviderID]bool{ProviderDsh: true, ProviderAlpha: true, ProviderOpenCode: true, ProviderCodex: true}
	for _, config := range configs {
		id := ProviderID(strings.TrimSpace(string(config.ProviderID)))
		if id == "" || seen[id] {
			continue
		}
		if isRetiredProvider(id) {
			continue
		}
		config.ProviderID = id
		provider, ok := providerFromConfig(config)
		if !ok {
			return nil, fmt.Errorf("unknown optional agent provider %q", id)
		}
		seen[id] = true
		providers = append(providers, provider)
	}
	return NewRegistry(providers...)
}

func alphaAgentProvider() Provider {
	return alphaAgentProviderWithResolver(resolvePi)
}

func alphaAgentProviderWithResolver(resolve func() string) Provider {
	return Provider{
		Descriptor: ProviderDescriptor{
			ID: ProviderAlpha, Label: "Alpha Agent", BuiltIn: true,
			Capabilities: []string{"Pi runtime", "chat / RAG only", "no broker", "streaming chat", "native resume", "cancel"},
		},
		SupportsBroker: false,
		Probe: func(ctx context.Context) ProviderProbe {
			return (commandProbe{id: ProviderAlpha, binary: resolve, ready: true, reason: "Alpha Agent runtime not found", version: commandVersion}).Probe(ctx)
		},
		ModelProbe: func(ctx context.Context) ProviderProbe {
			return alphaAgentModelProviderProbe(ctx, resolve)
		},
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, piRunner{binary: resolve}, ProviderAlpha, modelID, workDir, nonce)
			return err
		},
		Runner:  piRunner{binary: resolve},
		Connect: launchPiLoginWithResolver(resolve),
	}
}

func ProviderCatalog() []ProviderCatalogItem {
	return []ProviderCatalogItem{
		{
			ID: ProviderClaude, Label: "Claude Code",
			Description: "Anthropic 官方本地 CLI；使用临时私有 MCP，并禁用源码、Shell 与内建网络工具。",
			InstallURL:  "https://code.claude.com/docs/en/setup", Supported: true, ExecutableConfigurable: true,
			IntegrationKind: "right_agent", PrimaryCommand: "claude", CommandAliases: []string{"claude-code"},
			VersionArgs: []string{"--version"}, Transport: "claude_stream_json", AuthMode: "cli_login",
			ModelSource: "documented_alias_then_nonce", MCPInjection: "temporary_config", SessionResume: true,
		},
		{
			ID: ProviderKimi, Label: "Kimi Code",
			Description: "Moonshot AI 本地 CLI；通过官方 ACP 协商模型并注入临时私有 MCP，不修改 Kimi 全局配置。",
			InstallURL:  "https://www.kimi.com/code/docs/en/kimi-code-cli/guides/getting-started.html", Supported: true, ExecutableConfigurable: true,
			IntegrationKind: "right_agent", PrimaryCommand: "kimi",
			VersionArgs: []string{"--version"}, Transport: "acp_stdio", AuthMode: "device_code_terminal",
			ModelSource: "acp_session_config", MCPInjection: "acp_session_mcp", SessionResume: true,
		},
		{
			ID: ProviderCodeBuddy, Label: "CodeBuddy Code",
			Description: "腾讯 CodeBuddy Code CLI；使用官方 headless stream-json 与私有 MCP。",
			InstallURL:  "https://www.codebuddy.cn/docs/cli/headless", Supported: true, ExecutableConfigurable: true,
			IntegrationKind: "right_agent", PrimaryCommand: "codebuddy", CommandAliases: []string{"cbc"},
			VersionArgs: []string{"--version"}, Transport: "claude_stream_json", AuthMode: "cli_login",
			ModelSource: "cli_help_then_nonce", MCPInjection: "temporary_config", SessionResume: true,
		},
		{
			ID: ProviderAntigravity, Label: "Antigravity CLI",
			Description: "可选的实验性本地 CLI；不会修改用户全局 MCP 配置。",
			InstallURL:  "https://antigravity.google/docs/cli/install", Supported: true, ExecutableConfigurable: true,
			IntegrationKind: "right_agent", PrimaryCommand: "agy",
			VersionArgs: []string{"--version"}, Transport: "print_stream", AuthMode: "authenticated_cli",
			ModelSource: "cli_model_catalog", MCPInjection: "not_supported", SessionResume: true,
		},
	}
}

func providerFromCatalog(id ProviderID) (Provider, bool) {
	if !isCatalogProvider(id) {
		return Provider{}, false
	}
	return providerFromConfig(ProviderConfig{ProviderID: id})
}

func isCatalogProvider(id ProviderID) bool {
	for _, item := range ProviderCatalog() {
		if item.ID == id && item.Supported {
			return true
		}
	}
	return false
}

func isRetiredProvider(id ProviderID) bool {
	switch strings.ToLower(strings.TrimSpace(string(id))) {
	case "qwen", "cursor", "copilot", "github", "github-copilot", "zcode", "gemini":
		return true
	default:
		return false
	}
}

func providerFromConfig(config ProviderConfig) (Provider, bool) {
	if validateProviderLaunchMode(config) != nil {
		return Provider{}, false
	}
	executable := strings.TrimSpace(config.ExecutablePath)
	resolve := func(names ...string) func() string {
		return func() string {
			if executable != "" {
				return executable
			}
			for _, name := range names {
				if path := resolvePath(name); path != "" {
					return path
				}
			}
			return ""
		}
	}
	builder, ok := optionalProviderBuilders[config.ProviderID]
	if !ok {
		return Provider{}, false
	}
	return builder(config, resolve), true
}

func openCodeProvider() Provider {
	return Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderOpenCode, Label: "OpenCode", BuiltIn: true, Capabilities: []string{"streaming chat", "native resume", "cancel"}},
		SupportsBroker: true,
		Probe:          openCodeProviderProbe,
		ValidateModel:  openCodeModelValidate,
		Runner:         openCodeRunner{binary: resolveOpenCode},
		Connect:        launchOpenCodeApp,
	}
}

// NewCustomLLMProvider exposes the user's already-verified LLM configuration
// through a run-scoped OpenCode adapter without changing OpenCode's global
// config or credential store.
func NewCustomLLMProvider(resolve func() OpenCodeLLMConfig) Provider {
	return customLLMProviderWithResolver(resolve, resolveOpenCode)
}

func customLLMProviderWithResolver(resolve func() OpenCodeLLMConfig, binary func() string) Provider {
	runner := customLLMRunner{resolve: resolve, binary: binary}
	probe := func(ctx context.Context) ProviderProbe {
		base := (commandProbe{
			id: ProviderCustom, binary: binary, ready: true,
			reason: "OpenCode CLI not found", version: commandVersion,
		}).Probe(ctx)
		if !base.Available || !base.Ready {
			return base
		}
		config := resolve()
		models, err := validatedOpenCodeLLMModels(config)
		if err != nil {
			base.Ready = false
			base.Reason = "Configure and verify an LLM model first"
			return base
		}
		base.ModelCandidates = models
		return base
	}
	return Provider{
		Descriptor: ProviderDescriptor{
			ID: ProviderCustom, Label: "自定义 LLM", BuiltIn: true,
			Capabilities: []string{"streaming chat", "native resume", "cancel", "OpenCode runtime"},
		},
		SupportsBroker: true,
		Probe:          probe,
		ModelProbe:     probe,
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, runner, ProviderCustom, modelID, workDir, nonce)
			return err
		},
		Runner:  runner,
		Connect: launchOpenCodeApp,
	}
}

type customLLMRunner struct {
	resolve func() OpenCodeLLMConfig
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r customLLMRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	config := r.resolve()
	if _, ok := config.Models[request.ModelID]; !ok {
		return "", ErrModelNotVerified
	}
	request.OpenCodeLLM = &config
	return (openCodeRunner{binary: r.binary, command: r.command}).Run(ctx, request, emit)
}

func OpenCodeLLMModelID(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	return "alpha-nexus-llm/" + modelID
}

func validatedOpenCodeLLMModels(config OpenCodeLLMConfig) ([]string, error) {
	if strings.TrimSpace(config.ProviderID) != "alpha-nexus-llm" ||
		strings.TrimSpace(config.Name) == "" ||
		!validOpenCodeLLMBaseURL(config.BaseURL) ||
		strings.TrimSpace(config.APIKey) == "" ||
		(config.Package != "@ai-sdk/openai" && config.Package != "@ai-sdk/openai-compatible") {
		return nil, ErrProviderConfig
	}
	models := make([]string, 0, len(config.Models))
	for fullID, upstreamID := range config.Models {
		if !strings.HasPrefix(fullID, config.ProviderID+"/") ||
			strings.TrimSpace(upstreamID) == "" ||
			ValidateModelID(fullID) != nil ||
			ValidateModelID(upstreamID) != nil {
			return nil, ErrInvalidModel
		}
		models = append(models, fullID)
	}
	if len(models) == 0 {
		return nil, ErrModelNotVerified
	}
	sort.Strings(models)
	return models, nil
}

func validOpenCodeLLMBaseURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.User == nil && parsed.Host != "" &&
		(parsed.Scheme == "https" || parsed.Scheme == "http")
}

func codexProvider() Provider {
	return Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderCodex, Label: "Codex", BuiltIn: true, Capabilities: []string{"Registry Skills", "approval-gated Registry tools", "read-only sandbox", "run-scoped broker", "streaming chat", "native resume", "cancel"}},
		SupportsBroker: true,
		Probe:          codexProviderProbe,
		ModelProbe:     codexModelProviderProbe,
		ValidateModel:  codexModelValidate,
		Runner:         codexAppServerRunner{binary: resolveCodex},
		Connect:        launchCodexApp,
	}
}

func antigravityProvider() Provider {
	return antigravityProviderWithResolver(resolveAntigravity)
}

func antigravityProviderWithResolver(resolve func() string) Provider {
	return Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderAntigravity, Label: "Antigravity CLI", Experimental: true, Removable: true, Capabilities: []string{"text chat", "cancel", "authenticated CLI", "open external CLI"}},
		SupportsBroker: false,
		Probe: func(ctx context.Context) ProviderProbe {
			return applyProviderCompatibility(providerModelsProbe(ctx, ProviderAntigravity, resolve, []string{"models"}, "Open Antigravity and finish sign-in"))
		},
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, antigravityRunner{binary: resolve}, ProviderAntigravity, modelID, workDir, nonce)
			return err
		},
		Runner:  antigravityRunner{binary: resolve},
		Connect: launchAntigravityApp,
	}
}

func claudeProvider() Provider {
	return claudeProviderWithResolver(resolveClaude)
}

func claudeProviderWithResolver(resolve func() string) Provider {
	return claudeProviderWithRuntime(resolve, nil)
}

func claudeCCSwitchProviderWithResolver(resolve, settings func() string) Provider {
	return claudeProviderWithRuntime(resolve, settings)
}

func claudeProviderWithRuntime(resolve, settings func() string) Provider {
	return Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderClaude, Label: "Claude Code", Removable: true, Capabilities: []string{"streaming chat", "native resume", "cancel", "private MCP"}},
		SupportsBroker: true,
		Probe:          func(ctx context.Context) ProviderProbe { return claudeProviderProbeWithRuntime(ctx, resolve, settings) },
		ModelProbe: func(ctx context.Context) ProviderProbe {
			return claudeModelProviderProbeWithRuntime(ctx, resolve, settings)
		},
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, claudeRunner{binary: resolve, settings: settings}, ProviderClaude, modelID, workDir, nonce)
			return err
		},
		Runner:  claudeRunner{binary: resolve, settings: settings},
		Connect: connectClaudeWithRuntime(resolve, settings),
	}
}

func claudeRouterProviderWithResolver(resolve, revisionFile func() string) Provider {
	return Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderClaude, Label: "Claude Code", Removable: true, Capabilities: []string{"streaming chat", "native resume", "cancel", "private MCP", "local model router"}},
		SupportsBroker: true,
		Probe: func(ctx context.Context) ProviderProbe {
			return claudeRouterProviderProbeWithResolver(ctx, resolve, revisionFile)
		},
		ModelProbe: func(ctx context.Context) ProviderProbe {
			return claudeRouterProviderProbeWithResolver(ctx, resolve, revisionFile)
		},
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, claudeRunner{binary: resolve}, ProviderClaude, modelID, workDir, nonce)
			return err
		},
		Runner:  claudeRunner{binary: resolve},
		Connect: connectClaudeWithRuntime(resolve, revisionFile),
	}
}

func kimiProvider(resolve func() string) Provider {
	return kimiProviderWithLogin(resolve, launchKimiACPLogin)
}

func kimiProviderWithLogin(resolve func() string, login func(context.Context, string) error) Provider {
	return Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderKimi, Label: "Kimi Code", Removable: true, Capabilities: []string{"streaming chat", "cancel", "private MCP"}},
		SupportsBroker: true,
		Probe: func(ctx context.Context) ProviderProbe {
			return kimiACPModelProviderProbeWithResolver(ctx, resolve)
		},
		ModelProbe: func(ctx context.Context) ProviderProbe { return kimiACPModelProviderProbeWithResolver(ctx, resolve) },
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, kimiACPRunner{binary: resolve}, ProviderKimi, modelID, workDir, nonce)
			return err
		},
		Runner: kimiACPRunner{binary: resolve},
		Connect: func(ctx context.Context) error {
			if resolve == nil || login == nil {
				return ErrProviderConnect
			}
			binary := strings.TrimSpace(resolve())
			if binary == "" {
				return ErrProviderConnect
			}
			return login(ctx, binary)
		},
	}
}

func codeBuddyProvider(resolve func() string) Provider {
	return Provider{
		Descriptor:     ProviderDescriptor{ID: ProviderCodeBuddy, Label: "CodeBuddy Code", Removable: true, Capabilities: []string{"streaming chat", "native resume", "cancel", "private MCP"}},
		SupportsBroker: true,
		Probe: func(ctx context.Context) ProviderProbe {
			return (commandProbe{id: ProviderCodeBuddy, binary: resolve, ready: true, reason: "CodeBuddy CLI not found", version: commandVersion}).Probe(ctx)
		},
		ModelProbe: func(ctx context.Context) ProviderProbe {
			return codeBuddyModelProviderProbeWithResolver(ctx, resolve)
		},
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, codeBuddyRunner{binary: resolve}, ProviderCodeBuddy, modelID, workDir, nonce)
			return err
		},
		Runner:  codeBuddyRunner{binary: resolve},
		Connect: connectInstalledCLIWithResolver(resolve),
	}
}

func supportsEphemeralTextTask(id ProviderID) bool {
	switch id {
	// dsh already implements the hermetic one-process/one-turn path in
	// dshRunner.runEphemeral (covered by TestDshRunnerEphemeralRunIsHermetic);
	// the whitelist simply had not caught up with it.
	case ProviderDsh, ProviderAlpha, ProviderCodex, ProviderOpenCode, ProviderCustom, ProviderClaude, ProviderCodeBuddy:
		return true
	default:
		return false
	}
}

func resolvePi() string {
	if configured := strings.TrimSpace(os.Getenv("ALPHA_NEXUS_PI_PATH")); configured != "" {
		if info, err := os.Stat(configured); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return configured
		}
	}
	if path := resolvePath("pi"); path != "" {
		return path
	}
	for _, parts := range [][]string{{".local", "bin", "pi"}, {".pi", "bin", "pi"}} {
		if path := resolveUserBinary(parts...); path != "" {
			return path
		}
	}
	return ""
}

func resolveCodex() string {
	if path := resolvePath("codex"); path != "" {
		return path
	}
	// Packaged desktop shells start the sidecar with a fixed PATH that
	// excludes user-local install dirs, so look there explicitly.
	if path := resolveUserBinary(".local", "bin", "codex"); path != "" {
		return path
	}
	// Codex 的插件式安装不带 standalone 包，二进制在 .codex/plugins 下。
	if path := resolveUserBinary(".codex", "plugins", ".plugin-appserver", "codex-cli", "bin", "codex"); path != "" {
		return path
	}
	const bundled = "/Applications/ChatGPT.app/Contents/Resources/codex"
	if info, err := os.Stat(bundled); err == nil && !info.IsDir() {
		return bundled
	}
	return ""
}

func resolveOpenCode() string {
	if path := resolvePath("opencode"); path != "" {
		return path
	}
	return resolveUserBinary(".opencode", "bin", "opencode")
}

func resolveAntigravity() string {
	if path := resolvePath("agy"); path != "" {
		return path
	}
	return resolveUserBinary(".local", "bin", "agy")
}

func resolveClaude() string {
	path, ok := cliresolver.Claude()
	if !ok {
		return ""
	}
	return path
}

func resolveCCSwitchClaudeSettings() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".claude", "settings.json")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func resolveClaudeRouter() string {
	for _, name := range []string{"claude-volcengine", "claude-router"} {
		if path := resolvePath(name); path != "" {
			return path
		}
		if path := resolveUserBinary(".local", "bin", name); path != "" {
			return path
		}
	}
	return ""
}

func resolveClaudeRouterConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".claude-code-router", "config.json")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func resolveKimi() string {
	if path := resolvePath("kimi"); path != "" {
		return path
	}
	return resolveUserBinary(".local", "bin", "kimi")
}

func resolveCodeBuddy() string {
	for _, name := range []string{"codebuddy", "cbc"} {
		if path := resolvePath(name); path != "" {
			return path
		}
		if path := resolveUserBinary(".local", "bin", name); path != "" {
			return path
		}
	}
	return ""
}

func resolveUserBinary(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(append([]string{home}, parts...)...)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return path
}

func resolvePath(binary string) string {
	path, err := exec.LookPath(binary)
	if err != nil {
		return ""
	}
	return path
}

// commandVersionTimeout bounds a single version probe. Cold-starting signed
// CLIs can exceed three seconds while the desktop is also starting browsers and
// the Go test suite. The outer provider probe remains bounded, so allow the
// lightweight version command eight seconds.
const commandVersionTimeout = 8 * time.Second

func commandVersion(ctx context.Context, binary string) (string, error) {
	return commandVersionWithTimeout(ctx, binary, commandVersionTimeout)
}

// commandVersionWithTimeout is commandVersion with an injectable budget so
// tests are not subject to the production cold-start allowance. Tests that
// shell out run alongside every other package under `go test ./...`; with the
// default -p equal to the CPU count the machine can saturate and a trivial
// shell script can blow past eight seconds, which surfaces as "signal: killed".
func commandVersionWithTimeout(ctx context.Context, binary string, timeout time.Duration) (string, error) {
	versionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(versionCtx, binary, "--version")
	command.Env = providerEnvironment(binary, false)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func codexProviderProbe(ctx context.Context) ProviderProbe {
	probe := (commandProbe{
		id: ProviderCodex, binary: resolveCodex, ready: true,
		reason: "Codex CLI not found", version: commandVersion,
	}).Probe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	if err := codexLoginStatus(ctx, resolveCodex()); err != nil {
		probe.Ready = false
		probe.Reason = "Open Codex and sign in to connect"
	}
	return probe
}

// codexModelProviderProbe asks Codex's local app-server for its public model
// catalog. The selected model stays in the conversation record; no diagnostic
// report, account identity, or credential is persisted by Alpha-Nexus.
func codexModelProviderProbe(ctx context.Context) ProviderProbe {
	probe := codexProviderProbe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	if models, err := codexAvailableModels(ctx, resolveCodex()); err == nil && len(models) > 0 {
		probe.ModelCandidates = models
		return probe
	}
	// ponytail: older Codex CLIs may not expose app-server yet. Keep the
	// existing redacted doctor fallback until those versions are unsupported.
	model, err := codexConfiguredModel(ctx, resolveCodex())
	if err == nil && model != "" {
		probe.ModelCandidates = []string{model}
	}
	return probe
}

func codexAvailableModels(ctx context.Context, binary string) ([]string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return codexAppServerModels(probeCtx, binary)
}

func codexModelsFromAppServerResponse(raw []byte) ([]string, bool) {
	var response struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &response) != nil || response.ID != 2 {
		return nil, false
	}
	var result struct {
		Data []struct {
			Model  string `json:"model"`
			Hidden bool   `json:"hidden"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Result, &result) != nil {
		return nil, false
	}
	models := make([]string, 0, len(result.Data))
	seen := make(map[string]bool)
	for _, item := range result.Data {
		if item.Hidden || item.Model == "" || seen[item.Model] || ValidateModelID(item.Model) != nil {
			continue
		}
		seen[item.Model] = true
		models = append(models, item.Model)
	}
	return models, true
}

func codexConfiguredModel(ctx context.Context, binary string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, binary, "doctor", "--json")
	command.Stderr = io.Discard
	output, err := command.Output()
	if model := codexModelFromDoctorJSON(output); model != "" {
		return model, nil
	}
	if err != nil {
		return "", err
	}
	return "", nil
}

func codexModelFromDoctorJSON(raw []byte) string {
	var report any
	models := make(map[string]bool)
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if model, ok := typed["model"].(string); ok && ValidateModelID(model) == nil && model != "" {
				models[model] = true
			}
			for _, nested := range typed {
				visit(nested)
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		}
	}
	if json.Unmarshal(raw, &report) == nil {
		visit(report)
		if len(models) == 1 {
			for model := range models {
				return model
			}
		}
		if len(models) > 1 {
			return ""
		}
	}
	for _, match := range codexDoctorModelPattern.FindAllSubmatch(raw, -1) {
		if len(match) == 2 && ValidateModelID(string(match[1])) == nil {
			models[string(match[1])] = true
		}
	}
	if len(models) == 1 {
		for model := range models {
			return model
		}
	}
	return ""
}

func openCodeProviderProbe(ctx context.Context) ProviderProbe {
	return providerModelsProbe(ctx, ProviderOpenCode, resolveOpenCode, []string{"models"}, "Open OpenCode and finish sign-in")
}

func alphaAgentModelProviderProbe(ctx context.Context, resolve func() string) ProviderProbe {
	probe := (commandProbe{id: ProviderAlpha, binary: resolve, ready: true, reason: "Alpha Agent runtime not found", version: commandVersion}).Probe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	binary := resolve()
	command := exec.CommandContext(probeCtx, binary, "--offline", "--list-models")
	command.Env = append(providerEnvironment(binary, false), "PI_OFFLINE=1")
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		probe.Ready = false
		probe.Reason = "Open Alpha Agent and finish model sign-in"
		return probe
	}
	probe.ModelCandidates = piModelIDs(string(output))
	if len(probe.ModelCandidates) == 0 {
		probe.Ready = false
		probe.Reason = "Open Alpha Agent and finish model sign-in"
	}
	return probe
}

func piModelIDs(output string) []string {
	const maxModels = 512
	models := make([]string, 0)
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		columns := piModelColumnsPattern.Split(strings.TrimSpace(scanner.Text()), -1)
		if len(columns) < 2 || columns[0] == "provider" || columns[1] == "model" {
			continue
		}
		modelID := strings.TrimSpace(columns[0]) + "/" + strings.TrimSpace(columns[1])
		if seen[modelID] || ValidateModelID(modelID) != nil {
			continue
		}
		seen[modelID] = true
		models = append(models, modelID)
		if len(models) == maxModels {
			break
		}
	}
	return models
}

func antigravityProviderProbe(ctx context.Context) ProviderProbe {
	return applyProviderCompatibility(providerModelsProbe(ctx, ProviderAntigravity, resolveAntigravity, []string{"models"}, "Open Antigravity and finish sign-in"))
}

func claudeProviderProbe(ctx context.Context) ProviderProbe {
	return claudeProviderProbeWithResolver(ctx, resolveClaude)
}

func claudeProviderProbeWithResolver(ctx context.Context, resolve func() string) ProviderProbe {
	return claudeProviderProbeWithRuntime(ctx, resolve, nil)
}

func claudeProviderProbeWithRuntime(ctx context.Context, resolve, settings func() string) ProviderProbe {
	probe := (commandProbe{
		id: ProviderClaude, binary: resolve, ready: true,
		reason: "Claude CLI not found", version: commandVersion,
	}).Probe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	args := []string{"auth", "status", "--json"}
	if settings != nil {
		settingsPath := strings.TrimSpace(settings())
		if settingsPath == "" {
			probe.Ready = false
			probe.Reason = "Open CC Switch and select a Claude provider"
			return probe
		}
		probe.Version = claudeCCSwitchRouteVersion(probe.Version, settingsPath)
		args = append([]string{"--bare", "--settings", settingsPath}, args...)
	}
	authCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	binary := resolve()
	command := exec.CommandContext(authCtx, binary, args...)
	command.Env = providerEnvironment(binary, false)
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil || !claudeAuthStatusReady(output) {
		probe.Ready = false
		probe.Reason = "Open Claude and finish sign-in"
	}
	return probe
}

func claudeCCSwitchRouteVersion(version, settingsPath string) string {
	return claudeRouteVersion(version, settingsPath, "ccswitch")
}

func claudeRouteVersion(version, routePath, label string) string {
	info, err := os.Stat(routePath)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return ""
	}
	metadata := fmt.Sprintf("%s|%d|%d", version, info.Size(), info.ModTime().UTC().UnixNano())
	sum := sha256.Sum256([]byte(metadata))
	return fmt.Sprintf("%s+%s-%x", version, label, sum[:6])
}

func claudeAuthStatusReady(raw []byte) bool {
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	return json.Unmarshal(raw, &status) == nil && status.LoggedIn
}

func claudeModelProviderProbeWithResolver(ctx context.Context, resolve func() string) ProviderProbe {
	return claudeModelProviderProbeWithRuntime(ctx, resolve, nil)
}

func claudeModelProviderProbeWithRuntime(ctx context.Context, resolve, settings func() string) ProviderProbe {
	probe := claudeProviderProbeWithRuntime(ctx, resolve, settings)
	if probe.Available && probe.Ready {
		probe.ModelCandidates = claudeModelCandidates()
	}
	return probe
}

func claudeModelCandidates() []string {
	// These are the public model aliases documented by Claude Code. They remain
	// candidates until the existing exact-response validator proves account access.
	return []string{"best", "fable", "sonnet", "opus", "haiku"}
}

func claudeRouterProviderProbeWithResolver(ctx context.Context, resolve, revisionFile func() string) ProviderProbe {
	probe := (commandProbe{
		id: ProviderClaude, binary: resolve, ready: true,
		reason: "Claude-compatible router not found", version: commandVersion,
	}).Probe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	revisionPath := ""
	if revisionFile != nil {
		revisionPath = strings.TrimSpace(revisionFile())
	}
	if revisionPath == "" {
		probe.Ready = false
		probe.Reason = "Configure Claude Code Router first"
		return probe
	}
	probe.Version = claudeRouteVersion(probe.Version, revisionPath, "router")
	if probe.Version == "" {
		probe.Ready = false
		probe.Reason = "Claude Code Router configuration is unavailable"
	}
	return probe
}

func kimiACPModelProviderProbeWithResolver(ctx context.Context, resolve func() string) ProviderProbe {
	probe := (commandProbe{id: ProviderKimi, binary: resolve, ready: true, reason: "Kimi CLI not found", version: commandVersion}).Probe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	binary := resolve()
	workDir, err := os.MkdirTemp("", "alpha-nexus-kimi-acp-")
	if err != nil {
		probe.Ready = false
		probe.Reason = "Kimi ACP probe workspace is unavailable"
		return probe
	}
	defer os.RemoveAll(workDir)
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, binary, "acp")
	command.Env = append(providerEnvironment(binary, false), "KIMI_CODE_NO_AUTO_UPDATE=1")
	models, err := discoverACPModelsFromCommand(probeCtx, command, workDir)
	if err != nil || len(models) == 0 {
		probe.Ready = false
		probe.Reason = "Run Kimi device-code login to discover available models"
		return probe
	}
	probe.ModelCandidates = models
	return probe
}

func codeBuddyModelProviderProbeWithResolver(ctx context.Context, resolve func() string) ProviderProbe {
	probe := (commandProbe{id: ProviderCodeBuddy, binary: resolve, ready: true, reason: "CodeBuddy CLI not found", version: commandVersion}).Probe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	binary := resolve()
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, binary, "--help")
	command.Env = providerEnvironment(binary, false)
	command.Stderr = io.Discard
	output, err := command.Output()
	if err == nil {
		probe.ModelCandidates = codeBuddyModelsFromHelp(string(output))
	}
	return probe
}

func codeBuddyModelsFromHelp(output string) []string {
	match := codeBuddySupportedModelsPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return nil
	}
	models := make([]string, 0)
	for _, candidate := range strings.Split(match[1], ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "default-model" || ValidateModelID(candidate) != nil {
			continue
		}
		models = append(models, candidate)
	}
	return models
}

func applyProviderCompatibility(probe ProviderProbe) ProviderProbe {
	legacyIncompatibleCache := probe.State == ProviderStateIncompatible &&
		strings.Contains(probe.Reason, "不支持临时私有 MCP")
	if probe.ProviderID == ProviderAntigravity &&
		probe.Available &&
		(probe.Ready || legacyIncompatibleCache) {
		probe.Ready = true
		probe.State = ProviderStateLimited
		probe.Reason = "对话已连接，模型目录可读取；当前 CLI 未验证临时私有 MCP，仅限聊天，未修改全局配置"
	}
	return probe
}

// providerModelsProbe verifies that a CLI can reach its own configured model
// catalog without exposing account identities or credentials. Model IDs are
// public selection values, kept separate from the CLI's stderr diagnostics.
func providerModelsProbe(ctx context.Context, id ProviderID, binary func() string, args []string, unavailableReason string) ProviderProbe {
	probe := (commandProbe{id: id, binary: binary, ready: true, reason: unavailableReason, version: commandVersion}).Probe(ctx)
	if !probe.Available || !probe.Ready {
		return probe
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, binary(), args...)
	command.Env = providerEnvironment(binary(), id == ProviderOpenCode)
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		probe.Ready = false
		probe.Reason = unavailableReason
		return probe
	}
	probe.ModelCandidates = providerModelIDs(string(output))
	return probe
}

func codexModelValidate(ctx context.Context, modelID, workDir, nonce string) error {
	_, err := runModelValidation(ctx, codexAppServerRunner{binary: resolveCodex}, ProviderCodex, modelID, workDir, nonce)
	return err
}

func openCodeModelValidate(ctx context.Context, modelID, workDir, nonce string) error {
	_, err := runModelValidation(ctx, openCodeRunner{binary: resolveOpenCode}, ProviderOpenCode, modelID, workDir, nonce)
	return err
}

func antigravityModelValidate(ctx context.Context, modelID, workDir, nonce string) error {
	_, err := runModelValidation(ctx, antigravityRunner{binary: resolveAntigravity}, ProviderAntigravity, modelID, workDir, nonce)
	return err
}

func claudeModelValidate(ctx context.Context, modelID, workDir, nonce string) error {
	_, err := runModelValidation(ctx, claudeRunner{binary: resolveClaude}, ProviderClaude, modelID, workDir, nonce)
	return err
}

func runModelValidation(ctx context.Context, runner Runner, providerID ProviderID, modelID, workDir, nonce string) (string, error) {
	var response strings.Builder
	sessionID, err := runner.Run(ctx, RunRequest{
		ProviderID: providerID,
		ModelID:    modelID,
		Message:    "Reply with exactly " + nonce + " and nothing else.",
		WorkDir:    workDir,
		Ephemeral:  true,
	}, func(event ProviderEvent) {
		if event.Type == "message.delta" {
			if text, ok := event.Payload["text"].(string); ok {
				response.WriteString(text)
			}
		}
	})
	if err != nil {
		return sessionID, err
	}
	if strings.TrimSpace(response.String()) != nonce {
		return sessionID, errors.New("AGENT_MODEL_RESPONSE_MISMATCH")
	}
	return sessionID, nil
}

func providerModelIDs(output string) []string {
	const maxModels = 512
	scanner := bufio.NewScanner(strings.NewReader(output))
	models := make([]string, 0)
	seen := make(map[string]bool)
	for scanner.Scan() {
		model := strings.TrimSpace(scanner.Text())
		// Antigravity prints a tab-separated display label after the public
		// model id, while OpenCode prints one bare id per line. Accept that
		// reviewed shape without treating arbitrary space-delimited diagnostics
		// as model ids.
		if separator := strings.IndexByte(model, '\t'); separator >= 0 {
			model = strings.TrimSpace(model[:separator])
		}
		if model == "" || seen[model] || ValidateModelID(model) != nil {
			continue
		}
		seen[model] = true
		models = append(models, model)
		if len(models) == maxModels {
			break
		}
	}
	return models
}

// codexLoginStatus deliberately discards output because it can contain the
// signed-in account identity. Alpha-Nexus only needs the exit status.
var codexLoginStatusTimeout = 8 * time.Second

func codexLoginStatus(ctx context.Context, binary string) error {
	loginCtx, cancel := context.WithTimeout(ctx, codexLoginStatusTimeout)
	defer cancel()
	command := exec.CommandContext(loginCtx, binary, "login", "status")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

// launchCodexApp opens Codex's own connection surface. Authentication stays
// entirely in that app; Alpha-Nexus neither receives nor writes credentials.
func launchCodexApp(_ context.Context) error {
	binary := resolveCodex()
	if binary == "" {
		return errProviderUnavailable
	}
	command := exec.Command(binary, "app")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

// launchAntigravityApp is deliberately separate from the agy CLI capability
// probe: the user signs in in Antigravity's own app before the experimental
// CLI provider can ever be marked ready.
func launchAntigravityApp(_ context.Context) error {
	if err := launchDesktopApp("/Applications/Antigravity.app", "Antigravity"); err == nil {
		return nil
	}
	if resolveAntigravity() != "" {
		return nil
	}
	return ErrProviderConnect
}

func launchOpenCodeApp(_ context.Context) error {
	if err := launchDesktopApp("/Applications/OpenCode.app", "OpenCode"); err == nil {
		return nil
	}
	if resolveOpenCode() != "" {
		return nil
	}
	return ErrProviderConnect
}

// Claude has no separate desktop connection requirement. A successful
// resolver means the user's own CLI is present; auth readiness is decided only
// by the subsequent `claude auth status` probe.
func connectClaudeWithResolver(resolve func() string) func(context.Context) error {
	return connectClaudeWithRuntime(resolve, nil)
}

func connectClaudeWithRuntime(resolve, settings func() string) func(context.Context) error {
	return func(_ context.Context) error {
		if resolve == nil || strings.TrimSpace(resolve()) == "" {
			return ErrProviderConnect
		}
		if settings != nil && strings.TrimSpace(settings()) == "" {
			return ErrProviderConnect
		}
		return nil
	}
}

func connectInstalledCLIWithResolver(resolve func() string) func(context.Context) error {
	return func(_ context.Context) error {
		if resolve == nil || strings.TrimSpace(resolve()) == "" {
			return ErrProviderConnect
		}
		return nil
	}
}

func launchPiLoginWithResolver(resolve func() string) func(context.Context) error {
	return func(ctx context.Context) error {
		if resolve == nil {
			return ErrProviderConnect
		}
		binary := strings.TrimSpace(resolve())
		if binary == "" {
			return ErrProviderConnect
		}
		script := `on run argv
tell application "Terminal"
	do script (item 1 of argv)
end tell
end run`
		commandLine := shellSingleQuote(binary) + " --no-tools --no-extensions --no-skills --no-prompt-templates --no-themes --no-context-files --no-approve"
		command := exec.CommandContext(ctx, "osascript", "-e", script, commandLine)
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			return err
		}
		go func() { _ = command.Wait() }()
		return nil
	}
}

// launchKimiACPLogin is called only from an explicit user click. Terminal owns
// the device-code flow, so Alpha-Nexus never receives or stores login tokens.
// Terminal opens without an explicit activate command and follows macOS's
// current-screen placement (the Electron settings window is on the secondary
// display when available).
func launchKimiACPLogin(ctx context.Context, binary string) error {
	if strings.TrimSpace(binary) == "" {
		return ErrProviderConnect
	}
	script := `on run argv
tell application "Terminal"
	do script (item 1 of argv)
end tell
end run`
	commandLine := shellSingleQuote(binary) + " acp --login"
	command := exec.CommandContext(ctx, "osascript", "-e", script, commandLine)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func launchDesktopApp(appPath, appName string) error {
	info, err := os.Stat(appPath)
	if err != nil || !info.IsDir() {
		return ErrProviderConnect
	}
	command := exec.Command("open", "-a", appName)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

// codexRunner starts outside the checkout in a read-only sandbox. When the
// current run has a private broker token it receives only that temporary MCP.
type codexRunner struct {
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r codexRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	command := r.command
	if command == nil {
		command = exec.CommandContext
	}
	if request.SessionID != "" {
		return r.resume(ctx, command, binary, request, emit)
	}
	args := []string{
		"exec",
		"--ignore-user-config", "--ignore-rules",
		"--config", `model_reasoning_effort="` + codexDefaultReasoningEffort + `"`,
		"--config", `approval_policy="never"`,
		"--json", "--sandbox", "read-only", "--skip-git-repo-check", "--cd", request.WorkDir,
	}
	if request.Ephemeral {
		args = append(args, "--ephemeral")
	}
	args = append(args, codexBrokerConfigArgs(request)...)
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	args = append(args, "-")
	return runCodexCommand(ctx, command(ctx, binary, args...), request, emit)
}

func (r codexRunner) resume(ctx context.Context, command func(context.Context, string, ...string) *exec.Cmd, binary string, request RunRequest, emit func(ProviderEvent)) (string, error) {
	// `codex exec resume` has its own option parser and does not accept the
	// top-level --sandbox flag. Pin the same read-only policy through config.
	args := []string{
		"exec", "resume",
		"--ignore-user-config", "--ignore-rules",
		"--config", `model_reasoning_effort="` + codexDefaultReasoningEffort + `"`,
		"--config", `sandbox_mode="read-only"`,
		"--config", `approval_policy="never"`,
		"--json", "--skip-git-repo-check",
	}
	args = append(args, codexBrokerConfigArgs(request)...)
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	args = append(args, request.SessionID, "-")
	return runCodexCommand(ctx, command(ctx, binary, args...), request, emit)
}

func runCodexCommand(ctx context.Context, command *exec.Cmd, request RunRequest, emit func(ProviderEvent)) (string, error) {
	if command == nil {
		return "", errProviderUnavailable
	}
	configureProcess(command)
	command.Dir = request.WorkDir
	command.Env = providerEnvironmentForRequest(command.Path, false, request)
	command.Stdin = strings.NewReader(codexPrompt(request))
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", err
	}
	sessionID, scanErr := consumeCodexJSONL(stdout, emit)
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return sessionID, ctx.Err()
	}
	if scanErr != nil {
		return sessionID, scanErr
	}
	if waitErr != nil {
		return sessionID, fmt.Errorf("AGENT_PROVIDER_CODEX_FAILED")
	}
	return sessionID, nil
}

func codexPrompt(request RunRequest) string {
	contextJSON, _ := json.Marshal(request.Context)
	return "You are the Alpha-Nexus in-app business assistant. Reply only with the requested answer. " +
		"You are in an isolated runtime workspace, not the Alpha-Nexus source checkout. " +
		"Never read or modify source code and never use shell or arbitrary network access. " +
		brokerPrompt(request) + "\n\nVisible context: " + string(contextJSON) + "\n\nUser: " + strings.TrimSpace(request.Message)
}

func consumeCodexJSONL(reader io.Reader, emit func(ProviderEvent)) (string, error) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	var sessionID string
	for scanner.Scan() {
		var value map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			continue // CLI diagnostics are never surfaced as a model event.
		}
		if value["type"] == "thread.started" {
			if threadID, ok := value["thread_id"].(string); ok {
				sessionID = threadID
			}
			continue
		}
		if value["type"] != "item.completed" {
			continue
		}
		item, _ := value["item"].(map[string]any)
		if item["type"] != "agent_message" {
			continue
		}
		if text, ok := item["text"].(string); ok && text != "" {
			emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": text}})
		}
	}
	return sessionID, scanner.Err()
}

// piRunner is the reviewed Pi harness behind the Alpha Agent product surface.
// It runs only for one explicit request and disables every ambient tool,
// extension, Skill, prompt template and context file.
type piRunner struct {
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r piRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	// Alpha Agent is chat-only. Reject broker credentials even if a future
	// caller bypasses Service's provider capability gate.
	request.BrokerEndpoint = ""
	request.BrokerToken = ""
	request.Executable = ""
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	if request.Ephemeral && request.SessionID != "" {
		return "", errors.New("AGENT_PROVIDER_ALPHA_EPHEMERAL_RESUME_REJECTED")
	}
	if request.SessionID != "" && !piSessionIDPattern.MatchString(request.SessionID) {
		return "", errors.New("AGENT_PROVIDER_ALPHA_SESSION_INVALID")
	}
	commandFactory := r.command
	if commandFactory == nil {
		commandFactory = exec.CommandContext
	}
	sessionDir := filepath.Join(request.WorkDir, ".alpha-agent-sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return "", errors.New("AGENT_PROVIDER_ALPHA_SESSION_UNAVAILABLE")
	}
	args := []string{
		"--offline",
		"--mode", "json",
		"--no-tools",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
		"--no-context-files",
		"--no-approve",
		"--name", "Alpha Agent",
		"--session-dir", sessionDir,
		"--system-prompt", alphaAgentSystemPrompt,
	}
	if request.Ephemeral {
		args = append(args, "--no-session")
	} else if request.SessionID != "" {
		args = append(args, "--session", request.SessionID)
	}
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	args = append(args, alphaAgentPrompt(request))
	command := commandFactory(ctx, binary, args...)
	configureProcess(command)
	command.Dir = request.WorkDir
	command.Env = append(providerEnvironmentForRequest(binary, false, request), "PI_OFFLINE=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", err
	}
	sessionID, receivedText, scanErr := consumePiJSONL(stdout, emit)
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return sessionID, ctx.Err()
	}
	if scanErr != nil {
		return sessionID, scanErr
	}
	if waitErr != nil || !receivedText {
		return sessionID, errors.New("AGENT_PROVIDER_ALPHA_FAILED")
	}
	return sessionID, nil
}

const alphaAgentSystemPrompt = "You are Alpha Agent, the private in-app content strategy assistant for Alpha-Nexus EgoLite. Follow the visible user request, distinguish verified evidence from assumptions, and answer in concise Chinese unless asked otherwise. You have no filesystem, shell, browser, network, publishing, or credential tools in this runtime. Never claim an action was executed when it was not."

func alphaAgentPrompt(request RunRequest) string {
	contextJSON, _ := json.Marshal(request.Context)
	return "You are Alpha Agent. Reply only with the requested answer. " +
		"This process runs only for the current user request and exits after answering. " +
		"There are no business tools in this run.\n\nVisible context: " + string(contextJSON) +
		"\n\nUser: " + strings.TrimSpace(request.Message)
}

func consumePiJSONL(reader io.Reader, emit func(ProviderEvent)) (string, bool, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var sessionID string
	receivedText := false
	for scanner.Scan() {
		var value map[string]any
		if json.Unmarshal(scanner.Bytes(), &value) != nil {
			continue
		}
		eventType, _ := value["type"].(string)
		if eventType == "session" {
			if id, _ := value["id"].(string); piSessionIDPattern.MatchString(id) {
				sessionID = id
			}
			continue
		}
		if strings.HasPrefix(eventType, "tool_execution_") {
			return sessionID, receivedText, errors.New("AGENT_PROVIDER_ALPHA_UNSAFE_TOOL_EVENT")
		}
		if eventType != "message_update" {
			continue
		}
		assistantEvent, _ := value["assistantMessageEvent"].(map[string]any)
		if assistantEvent["type"] != "text_delta" {
			continue
		}
		text, _ := assistantEvent["delta"].(string)
		if text == "" {
			continue
		}
		receivedText = true
		emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": text}})
	}
	return sessionID, receivedText, scanner.Err()
}

// openCodeRunner uses OpenCode's documented `run --format json` protocol. Its
// inline config denies built-ins and allows only the temporary private MCP.
type openCodeRunner struct {
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r openCodeRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	command := r.command
	if command == nil {
		command = exec.CommandContext
	}
	if request.Ephemeral && request.SessionID != "" {
		return "", errors.New("AGENT_PROVIDER_OPENCODE_EPHEMERAL_RESUME_REJECTED")
	}
	args := []string{"run", "--format", "json", "--dir", request.WorkDir}
	if request.Ephemeral {
		args = append(args, "--pure", "--title", "Alpha-Nexus model check")
	}
	if request.SessionID != "" {
		args = append(args, "--session", request.SessionID)
	}
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	args = append(args, openCodePrompt(request))
	sessionID, runErr := runOpenCodeCommand(ctx, command(ctx, binary, args...), request, emit)
	if request.Ephemeral && sessionID == "" {
		return "", errors.Join(runErr, errors.New("AGENT_PROVIDER_OPENCODE_SESSION_CLEANUP_FAILED"))
	}
	if request.Ephemeral {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cleanupCommand := command(cleanupCtx, binary, "session", "delete", sessionID)
		configureProcess(cleanupCommand)
		cleanupCommand.Dir = request.WorkDir
		cleanupCommand.Env = providerEnvironmentForRequest(binary, true, request)
		cleanupCommand.Stdout = io.Discard
		cleanupCommand.Stderr = io.Discard
		cleanupErr := cleanupCommand.Run()
		cancel()
		if cleanupErr != nil {
			return sessionID, errors.Join(runErr, errors.New("AGENT_PROVIDER_OPENCODE_SESSION_CLEANUP_FAILED"))
		}
	}
	return sessionID, runErr
}

func runOpenCodeCommand(ctx context.Context, command *exec.Cmd, request RunRequest, emit func(ProviderEvent)) (string, error) {
	if command == nil {
		return "", errProviderUnavailable
	}
	configureProcess(command)
	command.Dir = request.WorkDir
	command.Env = providerEnvironmentForRequest(command.Path, true, request)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", err
	}
	sessionID, receivedText, scanErr := consumeOpenCodeJSONL(stdout, emit)
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return sessionID, ctx.Err()
	}
	if scanErr != nil {
		return sessionID, scanErr
	}
	if waitErr != nil || !receivedText {
		return sessionID, errors.New("AGENT_PROVIDER_OPENCODE_FAILED")
	}
	return sessionID, nil
}

func openCodePrompt(request RunRequest) string {
	contextJSON, _ := json.Marshal(request.Context)
	return "You are the Alpha-Nexus in-app business assistant. Reply only with the requested answer. " +
		"You are in an isolated runtime workspace, not the Alpha-Nexus source checkout. " +
		"All built-in filesystem, shell, and network tools are disabled. Never read or modify source code. " +
		brokerPrompt(request) + "\n\n" +
		"Visible context: " + string(contextJSON) + "\n\nUser: " + strings.TrimSpace(request.Message)
}

func consumeOpenCodeJSONL(reader io.Reader, emit func(ProviderEvent)) (string, bool, error) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	var sessionID string
	receivedText := false
	for scanner.Scan() {
		var value map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			continue
		}
		if id, ok := value["sessionID"].(string); ok && id != "" {
			sessionID = id
		}
		if value["type"] != "text" {
			continue
		}
		part, _ := value["part"].(map[string]any)
		if part["type"] != "text" {
			continue
		}
		text, _ := part["text"].(string)
		if text == "" {
			continue
		}
		receivedText = true
		emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": text}})
	}
	return sessionID, receivedText, scanner.Err()
}

// claudeRunner uses Claude Code's documented headless stream-json protocol.
// Every run ignores user/project settings, disables built-in tools, and loads
// only the temporary Alpha-Nexus MCP file when a Broker token is present.
type claudeRunner struct {
	binary   func() string
	settings func() string
	command  func(context.Context, string, ...string) *exec.Cmd
}

func (r claudeRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	command := r.command
	if command == nil {
		command = exec.CommandContext
	}
	mcpPath, cleanup, err := prepareClaudeMCP(request)
	if err != nil {
		return "", err
	}
	defer cleanup()
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "dontAsk",
		"--tools", "",
		"--disable-slash-commands",
		"--no-chrome",
		"--strict-mcp-config",
	}
	if r.settings == nil {
		args = append(args, "--safe-mode")
	} else {
		settingsPath := strings.TrimSpace(r.settings())
		if settingsPath == "" {
			return "", errProviderUnavailable
		}
		args = append(args, "--bare", "--settings", settingsPath)
	}
	if request.Ephemeral {
		args = append(args, "--no-session-persistence")
	}
	if mcpPath != "" {
		args = append(args, "--mcp-config", mcpPath, "--allowedTools", "mcp__alpha_nexus__*")
	}
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	if request.SessionID != "" && !request.Ephemeral {
		args = append(args, "--resume", request.SessionID)
	}
	return runClaudeCommand(ctx, command(ctx, binary, args...), request, emit)
}

func runClaudeCommand(ctx context.Context, command *exec.Cmd, request RunRequest, emit func(ProviderEvent)) (string, error) {
	return runClaudeCompatibleCommand(ctx, command, request, emit, "AGENT_PROVIDER_CLAUDE_FAILED")
}

func runClaudeCompatibleCommand(ctx context.Context, command *exec.Cmd, request RunRequest, emit func(ProviderEvent), errorCode string) (string, error) {
	if command == nil {
		return "", errProviderUnavailable
	}
	configureProcess(command)
	command.Dir = request.WorkDir
	command.Env = providerEnvironmentForRequest(command.Path, false, request)
	input, _ := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": claudePrompt(request)}},
		},
	})
	command.Stdin = bytes.NewReader(append(input, '\n'))
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", err
	}
	sessionID, receivedText, resultError, scanErr := consumeClaudeJSONL(stdout, emit)
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return sessionID, ctx.Err()
	}
	if scanErr != nil {
		return sessionID, scanErr
	}
	if waitErr != nil || resultError || !receivedText {
		return sessionID, errors.New(errorCode)
	}
	return sessionID, nil
}

func consumeClaudeJSONL(reader io.Reader, emit func(ProviderEvent)) (string, bool, bool, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var sessionID string
	receivedText := false
	resultError := false
	for scanner.Scan() {
		var value map[string]any
		if json.Unmarshal(scanner.Bytes(), &value) != nil {
			continue
		}
		if id, ok := value["session_id"].(string); ok && id != "" {
			sessionID = id
		}
		switch value["type"] {
		case "assistant":
			message, _ := value["message"].(map[string]any)
			content, _ := message["content"].([]any)
			for _, rawBlock := range content {
				block, _ := rawBlock.(map[string]any)
				text, _ := block["text"].(string)
				if block["type"] == "text" && text != "" {
					receivedText = true
					emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": text}})
				}
			}
		case "result":
			if isError, _ := value["is_error"].(bool); isError {
				resultError = true
			}
			if !receivedText {
				if text, _ := value["result"].(string); text != "" {
					receivedText = true
					emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": text}})
				}
			}
		}
	}
	return sessionID, receivedText, resultError, scanner.Err()
}

func claudePrompt(request RunRequest) string {
	contextJSON, _ := json.Marshal(request.Context)
	return "You are the Alpha-Nexus in-app business assistant. Reply only with the requested answer. " +
		"You are in an isolated runtime workspace, not the Alpha-Nexus source checkout. " +
		"All built-in filesystem, shell, and network tools are disabled. Never read or modify source code. " +
		brokerPrompt(request) + "\n\nVisible context: " + string(contextJSON) + "\n\nUser: " + strings.TrimSpace(request.Message)
}

func prepareClaudeMCP(request RunRequest) (string, func(), error) {
	command := brokerCommand(request)
	if len(command) == 0 {
		return "", func() {}, nil
	}
	configPath := filepath.Join(request.WorkDir, ".alpha-nexus-claude-mcp.json")
	encoded, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"alpha_nexus": map[string]any{
				"type":    "stdio",
				"command": command[0],
				"args":    command[1:],
				"env": map[string]string{
					"ALPHA_NEXUS_AGENT_RUN_TOKEN": request.BrokerToken,
				},
			},
		},
	})
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return "", nil, fmt.Errorf("AGENT_MCP_CONFIG_UNAVAILABLE")
	}
	return configPath, func() { _ = os.Remove(configPath) }, nil
}

// codeBuddyRunner uses CodeBuddy's documented Claude-compatible headless
// protocol. It does not use -y/dangerously-skip-permissions and its explicit
// allowlist leaves only the temporary Alpha-Nexus MCP tools visible.
type codeBuddyRunner struct {
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r codeBuddyRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	command := r.command
	if command == nil {
		command = exec.CommandContext
	}
	mcpPath, cleanup, err := prepareClaudeMCP(request)
	if err != nil {
		return "", err
	}
	defer cleanup()
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "dontAsk",
		"--tools", "",
		"--allowedTools", "mcp__alpha_nexus__*",
		"--setting-sources", "",
		"--strict-mcp-config",
	}
	if request.Ephemeral {
		args = append(args, "--no-session-persistence")
	}
	if mcpPath != "" {
		args = append(args, "--mcp-config", mcpPath)
	}
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	if request.SessionID != "" && !request.Ephemeral {
		args = append(args, "--resume", request.SessionID)
	}
	return runClaudeCompatibleCommand(ctx, command(ctx, binary, args...), request, emit, "AGENT_PROVIDER_CODEBUDDY_FAILED")
}

// kimiRunner uses Kimi Code's print stream and an explicit one-run Agent
// profile. The Agent profile is the highest-precedence Kimi prompt/tool scope:
// it exposes only Alpha-Nexus MCP tools and disables subagents. It intentionally
// keeps the user's Kimi data root so Kimi itself can use its existing login,
// while never reading or copying those credentials.
type kimiRunner struct {
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r kimiRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	if err := prepareKimiRuntime(request); err != nil {
		return "", err
	}
	commandFn := r.command
	if commandFn == nil {
		commandFn = exec.CommandContext
	}
	agentPath := filepath.Join(request.WorkDir, ".alpha-nexus-kimi-agent.md")
	skillsPath := filepath.Join(request.WorkDir, ".alpha-nexus-kimi-skills")
	args := []string{
		"--prompt", claudePrompt(request),
		"--output-format", "stream-json",
		"--skills-dir", skillsPath,
		"--agent-file", agentPath,
	}
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	if request.SessionID != "" && !request.Ephemeral {
		args = append(args, "--session", request.SessionID)
	}
	command := commandFn(ctx, binary, args...)
	configureProcess(command)
	command.Dir = request.WorkDir
	command.Env = append(providerEnvironmentForRequest(binary, false, request),
		"KIMI_CODE_EXPERIMENTAL_FLAG=1",
		"KIMI_DISABLE_TELEMETRY=1",
		"KIMI_CODE_NO_AUTO_UPDATE=1",
		"KIMI_CODE_BACKGROUND_KEEP_ALIVE_ON_EXIT=0",
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", err
	}
	receivedText, sessionID, scanErr := consumeKimiJSONL(stdout, emit)
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return request.SessionID, ctx.Err()
	}
	if scanErr != nil {
		return request.SessionID, scanErr
	}
	if waitErr != nil || !receivedText {
		return request.SessionID, errors.New("AGENT_PROVIDER_KIMI_FAILED")
	}
	if sessionID == "" {
		sessionID = request.SessionID
	}
	return sessionID, nil
}

func prepareKimiRuntime(request RunRequest) error {
	skillsPath := filepath.Join(request.WorkDir, ".alpha-nexus-kimi-skills")
	mcpDir := filepath.Join(request.WorkDir, ".kimi-code")
	if err := os.MkdirAll(skillsPath, 0o700); err != nil {
		return fmt.Errorf("AGENT_KIMI_RUNTIME_UNAVAILABLE")
	}
	if err := os.MkdirAll(mcpDir, 0o700); err != nil {
		return fmt.Errorf("AGENT_KIMI_RUNTIME_UNAVAILABLE")
	}
	agent := "---\nname: alpha-nexus\ndescription: Alpha-Nexus business assistant\ntools:\n  - mcp__alpha_nexus__*\nsubagents: []\n---\n" +
		"You are the Alpha-Nexus in-app business assistant. Use only the explicitly allowed Alpha-Nexus MCP tools. " +
		"Never read or modify source code, run shell commands, or access arbitrary local files."
	if err := os.WriteFile(filepath.Join(request.WorkDir, ".alpha-nexus-kimi-agent.md"), []byte(agent), 0o600); err != nil {
		return fmt.Errorf("AGENT_KIMI_RUNTIME_UNAVAILABLE")
	}
	servers := map[string]any{}
	if command := brokerCommand(request); len(command) > 0 {
		servers["alpha_nexus"] = map[string]any{
			"command": command[0],
			"args":    command[1:],
			"env": map[string]string{
				"ALPHA_NEXUS_AGENT_RUN_TOKEN": request.BrokerToken,
			},
		}
	}
	encoded, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return fmt.Errorf("AGENT_KIMI_RUNTIME_UNAVAILABLE")
	}
	if err := os.WriteFile(filepath.Join(mcpDir, "mcp.json"), encoded, 0o600); err != nil {
		return fmt.Errorf("AGENT_KIMI_RUNTIME_UNAVAILABLE")
	}
	return nil
}

func consumeKimiJSONL(reader io.Reader, emit func(ProviderEvent)) (bool, string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	receivedText := false
	sessionID := ""
	for scanner.Scan() {
		var value map[string]any
		if json.Unmarshal(scanner.Bytes(), &value) != nil {
			continue
		}
		if value["role"] == "meta" && value["type"] == "session.resume_hint" {
			if candidate, _ := value["session_id"].(string); len(candidate) <= 512 {
				sessionID = strings.TrimSpace(candidate)
			}
			continue
		}
		if value["role"] != "assistant" {
			continue
		}
		switch content := value["content"].(type) {
		case string:
			if content != "" {
				receivedText = true
				emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": content}})
			}
		case []any:
			for _, raw := range content {
				block, _ := raw.(map[string]any)
				text, _ := block["text"].(string)
				if block["type"] == "text" && text != "" {
					receivedText = true
					emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": text}})
				}
			}
		}
	}
	return receivedText, sessionID, scanner.Err()
}

// antigravityRunner uses agy's documented non-interactive print mode.
// Each conversation owns a private workspace, so --continue resumes only that
// workspace's latest Agent conversation.
type antigravityRunner struct {
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func (r antigravityRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	// ponytail: agy can run --print reliably but does not yet load the
	// run-scoped private MCP. Keep chat usable without advertising fake tools
	// or mutating the user's global Antigravity configuration.
	request.BrokerEndpoint = ""
	request.BrokerToken = ""
	request.Executable = ""
	command := r.command
	if command == nil {
		command = exec.CommandContext
	}
	args := []string{"--sandbox", "--mode", "plan", "--print-timeout", "2m"}
	if request.Ephemeral {
		args = append(args, "--new-project")
	}
	if request.SessionID != "" {
		args = append(args, "--continue")
	}
	if request.ModelID != "" {
		args = append(args, "--model", request.ModelID)
	}
	args = append(args, "--print", antigravityPrompt(request))
	return runAntigravityCommand(ctx, command(ctx, binary, args...), request, emit)
}

func runAntigravityCommand(ctx context.Context, command *exec.Cmd, request RunRequest, emit func(ProviderEvent)) (string, error) {
	if command == nil {
		return "", errProviderUnavailable
	}
	configureProcess(command)
	command.Dir = request.WorkDir
	command.Env = providerEnvironmentForRequest(command.Path, false, request)
	cleanup, err := prepareAntigravityMCP(request)
	if err != nil {
		return "", err
	}
	defer cleanup()
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, 4*1024*1024))
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if readErr != nil {
		return "", readErr
	}
	text := strings.TrimSpace(string(output))
	if waitErr != nil || text == "" {
		return "", errors.New("AGENT_PROVIDER_ANTIGRAVITY_FAILED")
	}
	emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": text}})
	return "workspace", nil
}

func antigravityPrompt(request RunRequest) string {
	contextJSON, _ := json.Marshal(request.Context)
	return "You are the Alpha-Nexus in-app business assistant. Reply only with the requested answer. " +
		"You are in an isolated runtime workspace, not the Alpha-Nexus source checkout. " +
		"You are running in a restricted plan sandbox. Never read or modify source code and never use shell or arbitrary network access. " +
		brokerPrompt(request) + "\n\nVisible context: " + string(contextJSON) + "\n\nUser: " + strings.TrimSpace(request.Message)
}

func brokerPrompt(request RunRequest) string {
	if request.BrokerToken == "" {
		return "There are no business tools in this run."
	}
	return "Use only the alpha_* MCP tools for Alpha-Nexus business actions. L0 reads execute immediately; other actions return a pending approval and execute only after the user confirms them in the Drawer. For media.import, use only an exact local path explicitly written in the current user message. Never invent approval fields."
}

func brokerCommand(request RunRequest) []string {
	if request.BrokerToken == "" || request.BrokerEndpoint == "" || request.RunID == "" || request.Executable == "" {
		return nil
	}
	return []string{request.Executable, "agent", "mcp-broker", "--endpoint", request.BrokerEndpoint, "--run-id", request.RunID}
}

func codexBrokerConfigArgs(request RunRequest) []string {
	command := brokerCommand(request)
	if len(command) == 0 {
		return nil
	}
	commandJSON, _ := json.Marshal(command[0])
	argsJSON, _ := json.Marshal(command[1:])
	return []string{
		"--config", "mcp_servers.alpha_nexus.command=" + string(commandJSON),
		"--config", "mcp_servers.alpha_nexus.args=" + string(argsJSON),
		"--config", `mcp_servers.alpha_nexus.env_vars=["ALPHA_NEXUS_AGENT_RUN_TOKEN"]`,
		"--config", `mcp_servers.alpha_nexus.required=true`,
		// Broker tools only propose actions. The broker itself retains the
		// risk classification and one-time local approval, so Codex may use
		// its normal automatic mode for this private transport.
		"--config", `mcp_servers.alpha_nexus.default_tools_approval_mode="auto"`,
	}
}

func prepareAntigravityMCP(request RunRequest) (func(), error) {
	command := brokerCommand(request)
	if len(command) == 0 {
		return func() {}, nil
	}
	agentsDir := filepath.Join(request.WorkDir, ".agents")
	pluginsDir := filepath.Join(agentsDir, "plugins")
	configDir := filepath.Join(pluginsDir, "alpha_nexus")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("AGENT_MCP_CONFIG_UNAVAILABLE")
	}
	if err := os.Chmod(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("AGENT_MCP_CONFIG_UNAVAILABLE")
	}
	manifestPath := filepath.Join(configDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"alpha-nexus-private-broker"}`), 0o600); err != nil {
		return nil, fmt.Errorf("AGENT_MCP_CONFIG_UNAVAILABLE")
	}
	configPath := filepath.Join(configDir, "mcp_config.json")
	encoded, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"alpha_nexus": map[string]any{
				"command": command[0],
				"args":    command[1:],
				"env": map[string]string{
					"ALPHA_NEXUS_AGENT_RUN_TOKEN": request.BrokerToken,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return nil, fmt.Errorf("AGENT_MCP_CONFIG_UNAVAILABLE")
	}
	return func() {
		_ = os.Remove(configPath)
		_ = os.Remove(manifestPath)
		_ = os.Remove(configDir)
		_ = os.Remove(pluginsDir)
		_ = os.Remove(agentsDir)
	}, nil
}

func providerEnvironment(binary string, openCode bool) []string {
	return providerEnvironmentForRequest(binary, openCode, RunRequest{})
}

func providerEnvironmentForRequest(binary string, openCode bool, request RunRequest) []string {
	home, _ := os.UserHomeDir()
	pathDirs := []string{filepath.Dir(binary), "/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	seen := make(map[string]bool, len(pathDirs))
	cleanPath := make([]string, 0, len(pathDirs))
	for _, dir := range pathDirs {
		dir = filepath.Clean(dir)
		if filepath.IsAbs(dir) && !seen[dir] {
			seen[dir] = true
			cleanPath = append(cleanPath, dir)
		}
	}
	locale := strings.TrimSpace(os.Getenv("LC_ALL"))
	if locale == "" {
		locale = strings.TrimSpace(os.Getenv("LANG"))
	}
	if locale == "" {
		locale = "C.UTF-8"
	}
	env := []string{
		"PATH=" + strings.Join(cleanPath, string(os.PathListSeparator)),
		"HOME=" + home,
		"TMPDIR=" + os.TempDir(),
		"LANG=" + locale,
		"LC_ALL=" + locale,
		"AGY_CLI_DISABLE_AUTO_UPDATE=true",
	}
	for _, key := range []string{
		"ANTIGRAVITY_LS_ADDRESS",
		"ANTIGRAVITY_CSRF_TOKEN",
		"ANTIGRAVITY_PROJECT_ID",
		"ANTIGRAVITY_CONVERSATION_ID",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
	} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			env = append(env, key+"="+val)
		}
	}
	if openCode {
		config := map[string]any{"permission": "deny"}
		if command := brokerCommand(request); len(command) > 0 {
			config = map[string]any{
				"permission": map[string]any{"*": "deny", "alpha_*": "allow"},
				"mcp": map[string]any{
					"alpha_nexus": map[string]any{"type": "local", "command": command, "enabled": true},
				},
			}
		}
		if request.OpenCodeLLM != nil {
			if providerConfig, ok := openCodeLLMProviderConfig(*request.OpenCodeLLM); ok {
				config["provider"] = map[string]any{
					request.OpenCodeLLM.ProviderID: providerConfig,
				}
				env = append(env, "ALPHA_NEXUS_LLM_API_KEY="+request.OpenCodeLLM.APIKey)
			}
		}
		configJSON, _ := json.Marshal(config)
		env = append(env,
			"OPENCODE_CONFIG_CONTENT="+string(configJSON),
			"OPENCODE_DISABLE_AUTOUPDATE=1",
			"OPENCODE_DISABLE_DEFAULT_PLUGINS=1",
			"OPENCODE_DISABLE_CLAUDE_CODE=1",
			"OPENCODE_DISABLE_LSP_DOWNLOAD=1",
		)
	}
	if request.BrokerToken != "" {
		env = append(env, "ALPHA_NEXUS_AGENT_RUN_TOKEN="+request.BrokerToken)
	}
	return env
}

func openCodeLLMProviderConfig(config OpenCodeLLMConfig) (map[string]any, bool) {
	modelIDs, err := validatedOpenCodeLLMModels(config)
	if err != nil {
		return nil, false
	}
	models := make(map[string]any, len(modelIDs))
	for _, fullID := range modelIDs {
		upstreamID := config.Models[fullID]
		localID := strings.TrimPrefix(fullID, config.ProviderID+"/")
		models[localID] = map[string]any{"name": upstreamID}
	}
	return map[string]any{
		"npm":  config.Package,
		"name": config.Name,
		"options": map[string]any{
			"baseURL": strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
			"apiKey":  "{env:ALPHA_NEXUS_LLM_API_KEY}",
		},
		"models": models,
	}, true
}

// workspacePath is retained as a small testable guard against accidental use
// of the Alpha-Nexus checkout as a CLI cwd.
func workspacePath(dataDir, conversationID string) string {
	return filepath.Join(dataDir, "agent-workspace", conversationID)
}
