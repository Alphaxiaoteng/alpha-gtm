// Package agenthub owns the durable, provider-neutral contract behind the
// in-app Agent drawer. It intentionally contains no business tools: those
// stay behind tools.Registry and will be wired only after a reviewed approval
// bridge is available.
package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type ProviderID string

const (
	ProviderDsh         ProviderID = "dsh"
	ProviderAlpha       ProviderID = "alpha"
	ProviderCodex       ProviderID = "codex"
	ProviderOpenCode    ProviderID = "opencode"
	ProviderAntigravity ProviderID = "antigravity"
	ProviderClaude      ProviderID = "claude"
	ProviderKimi        ProviderID = "kimi"
	ProviderCodeBuddy   ProviderID = "codebuddy"
	ProviderCustom      ProviderID = "custom"
	DefaultProvider     ProviderID = ProviderDsh
)

type ProviderLaunchMode string

const (
	ProviderLaunchModeCCSwitch ProviderLaunchMode = "cc_switch"
	ProviderLaunchModeRouter   ProviderLaunchMode = "claude_router"
)

// WorkspaceMode is the product/workspace boundary for Agent state. Agent
// conversations, drafts and activity views are partitioned by this value so
// Media and Shop work cannot accidentally reuse one another's state.
type WorkspaceMode string

const (
	WorkspaceModeMedia   WorkspaceMode = "media"
	WorkspaceModeShop    WorkspaceMode = "shop"
	DefaultWorkspaceMode               = WorkspaceModeMedia
)

// ProductMode is kept as an alias for callers that use the UI's product-mode
// terminology. The persisted contract remains workspace_mode.
type ProductMode = WorkspaceMode

const (
	ProductModeMedia = WorkspaceModeMedia
	ProductModeShop  = WorkspaceModeShop
)

const maxAgentMessageBytes = 16000

// MaxConcurrentRuns caps how many runs may be running at the same time across
// all conversations. Runs stay mutually exclusive per conversation; this global
// cap only guards shared provider resources.
const MaxConcurrentRuns = 4

var (
	ErrConversationNotFound      = errors.New("agent conversation not found")
	ErrRunNotFound               = errors.New("agent run not found")
	ErrActivityNotFound          = errors.New("agent activity not found")
	ErrRunActive                 = errors.New("agent conversation already has an active run")
	ErrRunCapacity               = errors.New("agent run capacity reached")
	ErrMessageRequired           = errors.New("agent message is required")
	ErrMessageTooLong            = errors.New("agent message is too long")
	ErrRunNotActive              = errors.New("agent run is not active")
	ErrUnsafeContext             = errors.New("agent context contains an unsupported value")
	ErrProviderNotFound          = errors.New("agent provider not found")
	ErrProviderConnect           = errors.New("agent provider connection is unavailable")
	ErrProviderConfig            = errors.New("agent provider configuration is invalid")
	ErrProviderExecutable        = errors.New("agent provider executable is invalid")
	ErrProviderMismatch          = errors.New("agent conversation provider cannot be changed")
	ErrInvalidModel              = errors.New("agent model id is invalid")
	ErrModelNotVerified          = errors.New("agent model is not verified")
	ErrModelValidation           = errors.New("agent model validation is unavailable")
	ErrModelValidationActive     = errors.New("agent model validation is already running")
	ErrInvalidWorkspaceMode      = errors.New("invalid agent workspace mode")
	ErrWorkspaceModeMismatch     = errors.New("agent workspace mode does not match conversation")
	ErrWorkspaceConversationMode = errors.New("agent workspace state conversation belongs to another mode")
	ErrLegacyProviderReadOnly    = errors.New("legacy agent provider is read only")
)

type ProviderState string

const (
	ProviderStateUnknown      ProviderState = "unknown"
	ProviderStateProbing      ProviderState = "probing"
	ProviderStateReady        ProviderState = "ready"
	ProviderStateLimited      ProviderState = "limited"
	ProviderStateUnavailable  ProviderState = "unavailable"
	ProviderStateIncompatible ProviderState = "incompatible"
)

type ProviderDescriptor struct {
	ID           ProviderID `json:"id"`
	Label        string     `json:"label"`
	Experimental bool       `json:"experimental"`
	BuiltIn      bool       `json:"built_in"`
	Removable    bool       `json:"removable"`
	Capabilities []string   `json:"capabilities"`
}

// ProviderCatalogItem describes an adapter Alpha-Nexus can safely add at
// runtime. Catalog presence does not mean the local CLI or any model works:
// those remain unavailable until the real probe and model validation succeed.
type ProviderCatalogItem struct {
	ID                     ProviderID `json:"id"`
	Label                  string     `json:"label"`
	Description            string     `json:"description"`
	InstallURL             string     `json:"install_url,omitempty"`
	Supported              bool       `json:"supported"`
	ExecutableConfigurable bool       `json:"executable_configurable"`
	UnavailableReason      string     `json:"unavailable_reason,omitempty"`
	IntegrationKind        string     `json:"integration_kind"`
	PrimaryCommand         string     `json:"primary_command"`
	CommandAliases         []string   `json:"command_aliases,omitempty"`
	VersionArgs            []string   `json:"version_args"`
	Transport              string     `json:"transport"`
	AuthMode               string     `json:"auth_mode"`
	ModelSource            string     `json:"model_source"`
	MCPInjection           string     `json:"mcp_injection"`
	SessionResume          bool       `json:"session_resume"`
}

// ProviderConfig contains only an adapter id, an optional executable path and
// a reviewed launch mode. Raw arguments, endpoints and credentials are never
// accepted through the Agent surface.
type ProviderConfig struct {
	ProviderID     ProviderID         `json:"provider_id"`
	ExecutablePath string             `json:"executable_path,omitempty"`
	LaunchMode     ProviderLaunchMode `json:"launch_mode,omitempty"`
}

type ProviderProbe struct {
	ProviderID               ProviderID               `json:"provider_id"`
	State                    ProviderState            `json:"state"`
	Available                bool                     `json:"available"`
	Ready                    bool                     `json:"ready"`
	Version                  string                   `json:"version,omitempty"`
	Models                   []string                 `json:"models,omitempty"`
	ModelCandidates          []string                 `json:"model_candidates,omitempty"`
	ValidatingModel          string                   `json:"validating_model,omitempty"`
	ModelValidationError     string                   `json:"model_validation_error,omitempty"`
	ModelValidationTotal     int                      `json:"model_validation_total,omitempty"`
	ModelValidationCompleted int                      `json:"model_validation_completed,omitempty"`
	ModelValidationPassed    int                      `json:"model_validation_passed,omitempty"`
	ModelValidationFailures  []ModelValidationFailure `json:"model_validation_failures,omitempty"`
	Reason                   string                   `json:"reason,omitempty"`
	Cached                   bool                     `json:"cached"`
	CheckedAt                string                   `json:"checked_at,omitempty"`
}

type ModelValidationFailure struct {
	ModelID   string `json:"model_id"`
	ErrorCode string `json:"error_code"`
}

type ContextSelection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ContextSnapshot is deliberately a small, visible contract. It accepts
// opaque IDs and display names, never DOM, cookies, credentials or paths.
type ContextSnapshot struct {
	Workspace     string             `json:"workspace,omitempty"`
	Page          string             `json:"page,omitempty"`
	Filters       map[string]string  `json:"filters,omitempty"`
	Selections    []ContextSelection `json:"selections,omitempty"`
	Attachments   []string           `json:"attachments,omitempty"`
	Transient     bool               `json:"transient,omitempty"`
	ProductMode   ProductMode        `json:"product_mode,omitempty"`
	WorkspaceMode WorkspaceMode      `json:"workspace_mode,omitempty"`
}

func (c *ContextSnapshot) UnmarshalJSON(data []byte) error {
	type snapshotAlias ContextSnapshot
	var decoded snapshotAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	normalized, err := ContextSnapshot(decoded).Normalize()
	if err != nil {
		return err
	}
	*c = normalized
	return nil
}

func (c ContextSnapshot) Validate() error {
	if _, err := c.resolveWorkspaceMode(); err != nil {
		return err
	}
	if unsafeContextValue(c.Workspace) || unsafeContextValue(c.Page) {
		return ErrUnsafeContext
	}
	for key, value := range c.Filters {
		if unsafeContextValue(key) || unsafeContextValue(value) {
			return ErrUnsafeContext
		}
	}
	for _, selection := range c.Selections {
		if unsafeContextValue(selection.ID) || unsafeContextValue(selection.Name) {
			return ErrUnsafeContext
		}
	}
	for _, attachment := range c.Attachments {
		if unsafeContextValue(attachment) {
			return ErrUnsafeContext
		}
	}
	return nil
}

// Normalize returns a snapshot with the compatibility default applied. Empty
// mode is intentionally accepted for old callers and old JSON rows.
func (c ContextSnapshot) Normalize() (ContextSnapshot, error) {
	if err := c.Validate(); err != nil {
		return ContextSnapshot{}, err
	}
	mode, err := c.resolveWorkspaceMode()
	if err != nil {
		return ContextSnapshot{}, err
	}
	c.WorkspaceMode = mode
	c.ProductMode = ProductMode(mode)
	return c, nil
}

func (c ContextSnapshot) resolveWorkspaceMode() (WorkspaceMode, error) {
	workspaceMode := strings.TrimSpace(string(c.WorkspaceMode))
	productMode := strings.TrimSpace(string(c.ProductMode))
	if workspaceMode != "" && productMode != "" {
		workspace, err := normalizeWorkspaceMode(WorkspaceMode(workspaceMode))
		if err != nil {
			return "", err
		}
		product, err := normalizeWorkspaceMode(WorkspaceMode(productMode))
		if err != nil {
			return "", err
		}
		if workspace != product {
			return "", ErrWorkspaceModeMismatch
		}
		return workspace, nil
	}
	if workspaceMode != "" {
		return normalizeWorkspaceMode(WorkspaceMode(workspaceMode))
	}
	return normalizeWorkspaceMode(WorkspaceMode(productMode))
}

func normalizeWorkspaceMode(mode WorkspaceMode) (WorkspaceMode, error) {
	mode = WorkspaceMode(strings.TrimSpace(string(mode)))
	if mode == "" {
		return DefaultWorkspaceMode, nil
	}
	switch mode {
	case WorkspaceModeMedia, WorkspaceModeShop:
		return mode, nil
	default:
		return "", ErrInvalidWorkspaceMode
	}
}

func (mode WorkspaceMode) Validate() error {
	_, err := normalizeWorkspaceMode(mode)
	return err
}

func unsafeContextValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	if filepath.IsAbs(value) || strings.Contains(value, "\\") || strings.Contains(lower, "/users/") || strings.Contains(lower, "cookie") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
		return true
	}
	return len(value) > 512
}

type Conversation struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Provider ProviderID `json:"provider_id"`
	ModelID  string     `json:"model_id,omitempty"`
	// BotID is an optional reference to one stored agent bot persona. It is
	// empty for every conversation created before bots existed, so old rows and
	// old JSON keep round-tripping unchanged.
	BotID         string          `json:"bot_id,omitempty"`
	WorkspaceMode WorkspaceMode   `json:"workspace_mode"`
	Context       ContextSnapshot `json:"context"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
	Messages      []Message       `json:"messages,omitempty"`
}

func (c *Conversation) UnmarshalJSON(data []byte) error {
	type conversationAlias Conversation
	var decoded conversationAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	mode := decoded.WorkspaceMode
	var err error
	if strings.TrimSpace(string(mode)) == "" {
		mode, err = decoded.Context.resolveWorkspaceMode()
	} else {
		mode, err = normalizeWorkspaceMode(mode)
	}
	if err != nil {
		return err
	}
	decoded.WorkspaceMode = mode
	decoded.Context.WorkspaceMode = mode
	decoded.Context.ProductMode = ProductMode(mode)
	*c = Conversation(decoded)
	return nil
}

// WorkspaceState is the durable shell state for one product mode. It is
// deliberately limited to resumable UI state and never stores tool payloads,
// source paths or provider credentials.
type WorkspaceState struct {
	WorkspaceMode         WorkspaceMode `json:"workspace_mode"`
	CurrentConversationID string        `json:"current_conversation_id,omitempty"`
	Draft                 string        `json:"draft,omitempty"`
	LastModule            string        `json:"last_module,omitempty"`
	UpdatedAt             string        `json:"updated_at,omitempty"`

	// Compatibility aliases for callers that use the shorter names. They are
	// normalized into the canonical fields and are never serialized.
	Mode           WorkspaceMode `json:"-"`
	ConversationID string        `json:"-"`
}

func (state WorkspaceState) Normalize() (WorkspaceState, error) {
	if state.WorkspaceMode == "" {
		state.WorkspaceMode = state.Mode
	}
	mode, err := normalizeWorkspaceMode(state.WorkspaceMode)
	if err != nil {
		return WorkspaceState{}, err
	}
	state.WorkspaceMode = mode
	if state.CurrentConversationID == "" {
		state.CurrentConversationID = strings.TrimSpace(state.ConversationID)
	}
	state.Mode = ""
	state.ConversationID = ""
	return state, nil
}

// AgentActivity is a safe, user-visible projection of an Agent event. It is
// intentionally not an event envelope: raw provider payloads, arguments,
// paths, tokens and credentials cannot cross this boundary.
type AgentActivity struct {
	ID         int64                   `json:"id"`
	RunID      string                  `json:"runId"`
	Kind       string                  `json:"kind"`
	Title      string                  `json:"title"`
	Summary    string                  `json:"summary"`
	Status     string                  `json:"status"`
	DetailRef  *AgentActivityDetailRef `json:"detailRef,omitempty"`
	OccurredAt string                  `json:"occurredAt"`
}

// AgentActivityDetailRef is an opaque, server-resolved reference. It never
// carries raw provider arguments, DOM, paths, credentials, or event payloads.
type AgentActivityDetailRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type ProviderSelection struct {
	ProviderID     ProviderID `json:"provider_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	ModelID        string     `json:"model_id,omitempty"`
	Draft          string     `json:"draft,omitempty"`
	UpdatedAt      string     `json:"updated_at,omitempty"`
}

type Message struct {
	ID         string          `json:"id"`
	ProviderID ProviderID      `json:"provider_id,omitempty"`
	RunID      string          `json:"run_id,omitempty"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	Context    ContextSnapshot `json:"context,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

type Run struct {
	ID                string     `json:"id"`
	ConversationID    string     `json:"conversation_id"`
	ProviderID        ProviderID `json:"provider_id"`
	ModelID           string     `json:"model_id,omitempty"`
	ProviderSessionID string     `json:"provider_session_id,omitempty"`
	Status            string     `json:"status"`
	ErrorCode         string     `json:"error_code,omitempty"`
	CreatedAt         string     `json:"created_at"`
	UpdatedAt         string     `json:"updated_at"`
}

// TextTaskResult is the durable result of a tool-free, ephemeral Agent task.
// The provider answer remains plain text so callers can enforce their own
// schema instead of trusting a CLI-specific response shape.
type TextTaskResult struct {
	ProviderID     ProviderID `json:"provider_id"`
	ModelID        string     `json:"model_id"`
	ConversationID string     `json:"conversation_id"`
	RunID          string     `json:"run_id"`
	Answer         string     `json:"answer"`
	DurationMS     int64      `json:"duration_ms"`
}

type Event struct {
	ID             int64          `json:"id"`
	RunID          string         `json:"runId"`
	ConversationID string         `json:"conversationId"`
	ProviderID     ProviderID     `json:"providerId"`
	Seq            int64          `json:"seq"`
	Timestamp      string         `json:"timestamp"`
	Type           string         `json:"type"`
	Payload        map[string]any `json:"payload"`
}

type Approval struct {
	ID            string `json:"id"`
	RunID         string `json:"run_id"`
	ToolName      string `json:"tool_name"`
	ParamsHash    string `json:"params_hash"`
	ImpactPreview string `json:"impact_preview"`
	Status        string `json:"status"`
	ExpiresAt     string `json:"expires_at"`
	CreatedAt     string `json:"created_at"`
	ResolvedAt    string `json:"resolved_at,omitempty"`
}

type ToolCall struct {
	ID             string         `json:"id"`
	RunID          string         `json:"run_id"`
	ConversationID string         `json:"conversation_id"`
	ProviderID     ProviderID     `json:"provider_id"`
	ToolName       string         `json:"tool_name"`
	Workspace      string         `json:"workspace"`
	RiskLevel      string         `json:"risk_level"`
	ParamsHash     string         `json:"params_hash"`
	ImpactPreview  string         `json:"impact_preview"`
	Status         string         `json:"status"`
	Result         map[string]any `json:"result,omitempty"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ApprovalID     string         `json:"approval_id,omitempty"`
	ExpiresAt      string         `json:"expires_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type RunRequest struct {
	ConversationID string
	ProviderID     ProviderID
	ModelID        string
	Message        string
	// Instructions is the resolved bot persona text for this run. Service also
	// appends it to Message so every existing runner receives it; the field is
	// kept so adapters can place it explicitly later.
	Instructions   string
	Context        ContextSnapshot
	WorkDir        string
	SessionID      string
	RunID          string
	BrokerEndpoint string
	BrokerToken    string
	Executable     string
	SkillRoot      string
	Skills         []SkillInput
	Ephemeral      bool
	OpenCodeLLM    *OpenCodeLLMConfig
}

// SkillInput is a server-resolved reference into the reviewed local Skill
// library. Provider adapters must not accept raw client paths for this field.
type SkillInput struct {
	Name string
	Path string
}

// OpenCodeLLMConfig is a per-run, in-memory OpenCode provider overlay.
// APIKey is passed only through the child process environment; it is never
// serialized into OPENCODE_CONFIG_CONTENT or persisted by Agent Hub.
type OpenCodeLLMConfig struct {
	ProviderID string
	Name       string
	BaseURL    string
	Package    string
	APIKey     string
	// Models maps the full OpenCode model id to the upstream model id.
	Models map[string]string
}

// ValidateModelID permits provider model identifiers only. The value is later
// passed as one CLI argument, so whitespace and shell-like characters are
// intentionally rejected instead of trying to quote arbitrary input.
func ValidateModelID(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	if len(model) > 120 {
		return ErrInvalidModel
	}
	for _, runeValue := range model {
		if (runeValue >= 'a' && runeValue <= 'z') || (runeValue >= 'A' && runeValue <= 'Z') || (runeValue >= '0' && runeValue <= '9') {
			continue
		}
		switch runeValue {
		case '.', '_', '-', '/', ':':
			continue
		default:
			return ErrInvalidModel
		}
	}
	return nil
}

type ProviderEvent struct {
	Type    string
	Payload map[string]any
}

type Runner interface {
	Run(context.Context, RunRequest, func(ProviderEvent)) (providerSessionID string, err error)
}

type ProbeFunc func(context.Context) ProviderProbe
type ModelValidateFunc func(context.Context, string, string, string) error

type Provider struct {
	Descriptor     ProviderDescriptor
	SupportsBroker bool
	Probe          ProbeFunc
	ModelProbe     ProbeFunc
	ValidateModel  ModelValidateFunc
	Runner         Runner
	Connect        func(context.Context) error
}

func (p Provider) check() error {
	if p.Descriptor.ID == "" || p.Probe == nil || p.Runner == nil {
		return fmt.Errorf("invalid agent provider %q", p.Descriptor.ID)
	}
	if p.Descriptor.ID == ProviderID("api") {
		return fmt.Errorf("direct API cannot be registered as an agent provider")
	}
	return nil
}

func nowText(now time.Time) string { return now.UTC().Format(time.RFC3339Nano) }
