// Package tools is the application capability surface for Tool Agent / workflows.
//
// Product model (not CLI-first):
//
//	Workflow / Tool-Agent  →  tools.Registry.Call(name, args)
//	CLI / Local UI         →  thin adapters over the same registry
//
// Domain packages (browser, db, publish, tikhub, …) own real work.
// This package only catalogs and dispatches — no parallel business logic.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/fleet"
	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/skilllib"
	"alpha-nexus-egolite/internal/trace"
)

// Param describes one JSON argument for a tool.
type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // string|int|bool|object|array
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// AgentRisk is the approval level enforced by the private Agent broker.
type AgentRisk string

const (
	AgentRiskL0 AgentRisk = "L0"
	AgentRiskL1 AgentRisk = "L1"
	AgentRiskL2 AgentRisk = "L2"
	AgentRiskL3 AgentRisk = "L3"
)

// AgentRedactor removes fields that must not cross the private MCP boundary.
type AgentRedactor func(*Result) *Result

// Def is a callable tool definition.
type Def struct {
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Params      []Param `json:"params,omitempty"`
	// SideEffect: read | write | browser | network
	SideEffect string `json:"side_effect"`
	// FreeCreditHint: optional note for paid third-party APIs (e.g. TikHub).
	FreeCreditHint string `json:"free_credit_hint,omitempty"`
	// AgentVisible is an explicit allowlist. Unclassified tools are never
	// exported by the private Agent broker.
	AgentVisible     bool          `json:"agent_visible"`
	AgentWorkspace   string        `json:"agent_workspace,omitempty"`
	AgentRisk        AgentRisk     `json:"agent_risk,omitempty"`
	AgentImpact      string        `json:"agent_impact,omitempty"`
	AgentIdempotency string        `json:"agent_idempotency,omitempty"`
	AgentRedactor    AgentRedactor `json:"-"`
	Handler          Handler       `json:"-"`
}

// Handler executes a tool. Args are JSON-decoded map keys.
type Handler func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error)

// Result is a stable JSON envelope for agents.
type Result struct {
	OK       bool                   `json:"ok"`
	Tool     string                 `json:"tool"`
	Message  string                 `json:"message,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Duration string                 `json:"duration,omitempty"`
}

// Runtime is shared context for tool handlers (data dir, CDP port, etc.).
type Runtime struct {
	DataDir string
	// Database is the startup-checked connection owned by a long-lived local
	// UI. One-shot CLI runtimes leave it nil and open their DataDir on demand.
	Database            *db.DB
	EgoBrowserCommand   string
	MaxConcurrentSpaces int
	EgoRunner           egolite.ProgramRunner
	CDPPort             int
	Browser             string // fingerprint-chromium path
	BrowserOS           string // BrowserOS binary path
	Engine              string // default engine hint
	// Fleet is provided by the long-lived UI server. CLI may leave it nil and
	// create a one-shot local service through the same registry handler.
	Fleet  *fleet.Service
	Skills *skilllib.Library
	Events observability.Emitter
	// TrustedLocalUI is set only on a short-lived runtime created by an
	// explicit localhost UI handler. Shared Agent and MCP runtimes leave it
	// false, so they cannot trigger paid media generation.
	TrustedLocalUI bool
	GenerateMedia  func(context.Context, map[string]interface{}) (*Result, error)
	// CheckEgoSessions lets the long-lived UI reuse its serialized session
	// monitor. One-shot CLI runtimes leave it nil and use the local Registry
	// fallback, while in-app Agent calls cannot create a competing monitor.
	CheckEgoSessions func(context.Context) (*Result, error)
	// AuthorizeAgentTool is owned by the trusted local UI. It creates a
	// domain-scoped, single-use approval for the already frozen Agent args and
	// returns the one-shot Registry args. The returned credentials must never
	// be written to the Agent call or event stream.
	AuthorizeAgentTool func(context.Context, string, map[string]interface{}) (map[string]interface{}, error)
	// Long-lived local UIs reuse their startup-checked SQLite connection.
	// One-shot CLI runtimes leave these nil and keep the open/migrate/close path.
	FreezePublishPlan  func(context.Context, publishplan.FreezeInput) (*Result, error)
	GetPublishPlan     func(context.Context, int64) (*Result, error)
	PreparePublishPlan func(context.Context, int64, string, string, string) (*Result, error)
	PrepareShopListing func(context.Context, int64, string, string, string) (*Result, error)
	SubmitPublishPlan  func(context.Context, int64, string, string, string) (*Result, error)
	// InspectPublishPlan reads the currently open creator page through the
	// reviewed worker. It never navigates, fills, uploads, or submits.
	InspectPublishPlan func(context.Context, int64) (*Result, error)
	// InspectPublishPlanComposer opens one reviewed direct composer URL in the
	// account's current tab, then returns sanitized readiness only. It never
	// fills, uploads, or submits.
	InspectPublishPlanComposer func(context.Context, int64) (*Result, error)
	// InspectPublishPlanManager opens a reviewed platform content manager and
	// returns only sanitized verification evidence.
	InspectPublishPlanManager func(context.Context, int64) (*Result, error)
	DeletePublishContent      func(context.Context, int64) (*Result, error)
	// RevealPublishPlanSettings opens only a reviewed settings disclosure, then
	// returns a sanitized inspection. It never fills, uploads, or submits.
	RevealPublishPlanSettings func(context.Context, int64) (*Result, error)
	// ResumePublishPlanTopics repairs only native topics in an existing editor.
	// It never navigates, uploads, or submits a post.
	ResumePublishPlanTopics func(context.Context, int64) (*Result, error)
	// OnEventError receives an emitter failure without changing tool semantics.
	// Callers must not print the underlying error because it can contain paths.
	OnEventError func(error)
	eventMu      sync.Mutex
	eventCloser  io.Closer
}

// WithTrustedLocalUI returns a per-call Runtime view without copying the
// mutexes that protect the long-lived server Runtime.
func (rt *Runtime) WithTrustedLocalUI(trusted bool) *Runtime {
	if rt == nil {
		return nil
	}
	return &Runtime{
		DataDir:                    rt.DataDir,
		Database:                   rt.Database,
		EgoBrowserCommand:          rt.EgoBrowserCommand,
		MaxConcurrentSpaces:        rt.MaxConcurrentSpaces,
		EgoRunner:                  rt.EgoRunner,
		CDPPort:                    rt.CDPPort,
		Browser:                    rt.Browser,
		BrowserOS:                  rt.BrowserOS,
		Engine:                     rt.Engine,
		Fleet:                      rt.Fleet,
		Skills:                     rt.Skills,
		Events:                     rt.Events,
		TrustedLocalUI:             trusted,
		GenerateMedia:              rt.GenerateMedia,
		CheckEgoSessions:           rt.CheckEgoSessions,
		AuthorizeAgentTool:         rt.AuthorizeAgentTool,
		FreezePublishPlan:          rt.FreezePublishPlan,
		GetPublishPlan:             rt.GetPublishPlan,
		PreparePublishPlan:         rt.PreparePublishPlan,
		PrepareShopListing:         rt.PrepareShopListing,
		SubmitPublishPlan:          rt.SubmitPublishPlan,
		InspectPublishPlan:         rt.InspectPublishPlan,
		InspectPublishPlanComposer: rt.InspectPublishPlanComposer,
		InspectPublishPlanManager:  rt.InspectPublishPlanManager,
		DeletePublishContent:       rt.DeletePublishContent,
		RevealPublishPlanSettings:  rt.RevealPublishPlanSettings,
		ResumePublishPlanTopics:    rt.ResumePublishPlanTopics,
		OnEventError:               rt.OnEventError,
	}
}

type sharedRuntimeEmitter struct {
	emitter *observability.FileEmitter
	refs    int
}

var (
	runtimeEmitterMu sync.Mutex
	runtimeEmitters  = map[string]*sharedRuntimeEmitter{}
)

type runtimeEmitterLease struct {
	path string
	once sync.Once
	err  error
}

func (l *runtimeEmitterLease) Close() error {
	l.once.Do(func() {
		runtimeEmitterMu.Lock()
		defer runtimeEmitterMu.Unlock()
		shared := runtimeEmitters[l.path]
		if shared == nil {
			return
		}
		shared.refs--
		if shared.refs <= 0 {
			l.err = shared.emitter.Close()
			delete(runtimeEmitters, l.path)
		}
	})
	return l.err
}

// EnableFileEvents installs one bounded JSONL emitter for this Runtime.
func (rt *Runtime) EnableFileEvents(service string) error {
	if rt == nil {
		return fmt.Errorf("tool runtime is required")
	}
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	if rt.Events != nil {
		return nil
	}
	if rt.DataDir == "" {
		return fmt.Errorf("tool runtime data directory is required")
	}
	path := filepath.Join(rt.DataDir, "logs", fmt.Sprintf("events-%d.jsonl", os.Getpid()))
	runtimeEmitterMu.Lock()
	defer runtimeEmitterMu.Unlock()
	if shared := runtimeEmitters[path]; shared != nil {
		shared.refs++
		rt.Events = shared.emitter
		rt.eventCloser = &runtimeEmitterLease{path: path}
		return nil
	}
	emitter, err := observability.NewFileEmitter(
		path,
		observability.FileOptions{Options: observability.Options{Service: service}},
	)
	if err != nil {
		return err
	}
	runtimeEmitters[path] = &sharedRuntimeEmitter{emitter: emitter, refs: 1}
	rt.Events = emitter
	rt.eventCloser = &runtimeEmitterLease{path: path}
	return nil
}

// CloseEvents closes an emitter owned by EnableFileEvents.
func (rt *Runtime) CloseEvents() error {
	if rt == nil {
		return nil
	}
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	if rt.eventCloser == nil {
		return nil
	}
	err := rt.eventCloser.Close()
	rt.eventCloser = nil
	rt.Events = nil
	return err
}

func (rt *Runtime) reportEventError(err error) {
	if err == nil {
		return
	}
	if rt != nil && rt.OnEventError != nil {
		rt.OnEventError(err)
		return
	}
	// Do not attach err: file paths and OS details are outside the log allowlist.
	slog.Error("diagnostic event emit failed", "error_code", "OBSERVABILITY_EMIT_FAILED")
}

// Registry holds named tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Def
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Def)}
}

func (r *Registry) Register(d Def) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d.Name == "" {
		return
	}
	r.tools[d.Name] = d
}

// SetAgentMeta exposes one existing Registry tool to the private Agent broker.
// Keeping this separate from Register makes the allowlist easy to audit.
func (r *Registry) SetAgentMeta(name, workspace string, risk AgentRisk, impact, idempotency string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.tools[name]
	if !ok {
		return fmt.Errorf("agent tool %q is not registered", name)
	}
	d.AgentVisible = true
	d.AgentWorkspace = strings.TrimSpace(workspace)
	d.AgentRisk = risk
	d.AgentImpact = strings.TrimSpace(impact)
	d.AgentIdempotency = strings.TrimSpace(idempotency)
	r.tools[name] = d
	return validateAgentDef(d)
}

// AgentList returns only fully classified Agent tools. A visible but
// incomplete definition fails closed instead of leaking an unsafe capability.
func (r *Registry) AgentList() ([]Def, error) {
	all := r.List()
	out := make([]Def, 0, len(all))
	for _, d := range all {
		if !d.AgentVisible {
			continue
		}
		if err := validateAgentDef(d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func validateAgentDef(d Def) error {
	if !d.AgentVisible {
		return nil
	}
	if d.AgentWorkspace == "" || d.AgentImpact == "" || d.AgentIdempotency == "" {
		return fmt.Errorf("agent tool %q is missing required metadata", d.Name)
	}
	switch d.AgentRisk {
	case AgentRiskL0, AgentRiskL1, AgentRiskL2, AgentRiskL3:
		return nil
	default:
		return fmt.Errorf("agent tool %q has invalid risk %q", d.Name, d.AgentRisk)
	}
}

// AgentRiskFor returns the effective risk for one proposed call. Aggregated
// workflows inherit the highest risk of every nested step.
func (r *Registry) AgentRiskFor(name string, args map[string]interface{}) (AgentRisk, error) {
	d, ok := r.Get(name)
	if !ok || !d.AgentVisible {
		return "", fmt.Errorf("agent tool %q is not available", name)
	}
	if err := validateAgentDef(d); err != nil {
		return "", err
	}
	if name != "workflow.run" {
		if name == "settings.patch" && settingsPatchEnablesExternalChannel(args) {
			return AgentRiskL2, nil
		}
		return d.AgentRisk, nil
	}
	steps, ok := args["steps"].([]interface{})
	if !ok || len(steps) == 0 || len(steps) > 20 {
		return "", fmt.Errorf("workflow.run requires 1-20 steps")
	}
	highest := AgentRiskL0
	for _, raw := range steps {
		step, ok := raw.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("workflow step must be an object")
		}
		stepName, _ := step["tool"].(string)
		stepArgs, _ := step["args"].(map[string]interface{})
		risk, err := r.AgentRiskFor(stepName, stepArgs)
		if err != nil {
			return "", err
		}
		if agentRiskRank(risk) > agentRiskRank(highest) {
			highest = risk
		}
	}
	return highest, nil
}

func agentRiskRank(risk AgentRisk) int {
	switch risk {
	case AgentRiskL0:
		return 0
	case AgentRiskL1:
		return 1
	case AgentRiskL2:
		return 2
	case AgentRiskL3:
		return 3
	default:
		return 99
	}
}

func (r *Registry) Get(name string) (Def, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.tools[name]
	return d, ok
}

func (r *Registry) List() []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Def, 0, len(r.tools))
	for _, d := range r.tools {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category == out[j].Category {
			return out[i].Name < out[j].Name
		}
		return out[i].Category < out[j].Category
	})
	return out
}

func (r *Registry) ListByCategory(cat string) []Def {
	all := r.List()
	if cat == "" {
		return all
	}
	var out []Def
	for _, d := range all {
		if d.Category == cat {
			out = append(out, d)
		}
	}
	return out
}

// Call invokes a tool by name with JSON-object args.
func (r *Registry) Call(ctx context.Context, rt *Runtime, name string, args map[string]interface{}) (res *Result, err error) {
	ctx, _ = trace.Ensure(ctx)
	if rt != nil && rt.Events == nil && rt.DataDir != "" {
		if initErr := rt.EnableFileEvents("alpha-nexus"); initErr != nil {
			rt.reportEventError(initErr)
		}
	}
	started := time.Now()
	observedStep := ""
	defer func() {
		if rt == nil || rt.Events == nil {
			return
		}
		event := observability.Event{
			Level:      slog.LevelInfo,
			Component:  "tools.registry",
			Name:       "tool.call.completed",
			Step:       observedStep,
			Status:     "succeeded",
			DurationMS: time.Since(started).Milliseconds(),
		}
		if err != nil || res == nil || !res.OK {
			event.Level = slog.LevelError
			event.Status = "failed"
			if res != nil {
				event.ErrorCode = observability.StableErrorCode(res.Error)
			}
			if event.ErrorCode == "" {
				event.ErrorCode = "INTERNAL_ERROR"
			}
		}
		// Diagnostic logging is best-effort and must not change tool semantics.
		if emitErr := rt.Events.Emit(ctx, event); emitErr != nil {
			rt.reportEventError(emitErr)
		}
	}()

	if args == nil {
		args = map[string]interface{}{}
	}
	ctx = observability.WithCorrelation(ctx, observability.Correlation{AccountID: accountIDFromArgs(args), Platform: platformFromArgs(args)})
	d, ok := r.Get(name)
	if !ok {
		return &Result{OK: false, Tool: name, Error: "TOOL_NOT_FOUND"}, fmt.Errorf("tool not found: %s", name)
	}
	observedStep = d.Name
	if d.Handler == nil {
		return &Result{OK: false, Tool: name, Error: "TOOL_NOT_IMPLEMENTED"}, fmt.Errorf("tool not implemented: %s", name)
	}
	for _, p := range d.Params {
		if !p.Required {
			continue
		}
		if _, exists := args[p.Name]; !exists {
			return &Result{OK: false, Tool: name, Error: "MISSING_PARAM:" + p.Name},
				fmt.Errorf("missing required param %q for tool %s", p.Name, name)
		}
	}
	if rt != nil && rt.Events != nil {
		if emitErr := rt.Events.Emit(ctx, observability.Event{Level: slog.LevelInfo, Component: "tools.registry", Name: "tool.call.started", Step: d.Name, Status: "running"}); emitErr != nil {
			rt.reportEventError(emitErr)
		}
	}
	start := time.Now()
	res, err = d.Handler(ctx, rt, args)
	if res == nil {
		res = &Result{OK: err == nil, Tool: name}
	}
	res.Tool = name
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		res.OK = false
		if res.Error == "" {
			res.Error = err.Error()
		}
	}
	return res, err
}

func accountIDFromArgs(args map[string]interface{}) int64 {
	for _, key := range []string{"account_id", "accountId"} {
		switch value := args[key].(type) {
		case int64:
			if value > 0 {
				return value
			}
		case int:
			if value > 0 {
				return int64(value)
			}
		case float64:
			if value > 0 && value == float64(int64(value)) {
				return int64(value)
			}
		}
	}
	return 0
}

func platformFromArgs(args map[string]interface{}) string {
	for _, key := range []string{"platform_code", "platform"} {
		if value, ok := args[key].(string); ok {
			return value
		}
	}
	return ""
}

// SchemaJSON returns OpenAI/Claude-style tool schemas for agent binding.
func (r *Registry) SchemaJSON() ([]byte, error) {
	type prop struct {
		Type        string `json:"type"`
		Description string `json:"description,omitempty"`
	}
	type schema struct {
		Name             string    `json:"name"`
		Description      string    `json:"description"`
		Category         string    `json:"category"`
		SideEffect       string    `json:"side_effect"`
		AgentVisible     bool      `json:"agent_visible"`
		AgentWorkspace   string    `json:"agent_workspace,omitempty"`
		AgentRisk        AgentRisk `json:"agent_risk,omitempty"`
		AgentImpact      string    `json:"agent_impact,omitempty"`
		AgentIdempotency string    `json:"agent_idempotency,omitempty"`
		Parameters       struct {
			Type       string          `json:"type"`
			Properties map[string]prop `json:"properties"`
			Required   []string        `json:"required"`
		} `json:"parameters"`
	}
	var list []schema
	for _, d := range r.List() {
		var s schema
		s.Name = d.Name
		s.Description = d.Description
		s.Category = d.Category
		s.SideEffect = d.SideEffect
		s.AgentVisible = d.AgentVisible
		s.AgentWorkspace = d.AgentWorkspace
		s.AgentRisk = d.AgentRisk
		s.AgentImpact = d.AgentImpact
		s.AgentIdempotency = d.AgentIdempotency
		s.Parameters.Type = "object"
		s.Parameters.Properties = map[string]prop{}
		for _, p := range d.Params {
			s.Parameters.Properties[p.Name] = prop{Type: p.Type, Description: p.Description}
			if p.Required {
				s.Parameters.Required = append(s.Parameters.Required, p.Name)
			}
		}
		list = append(list, s)
	}
	return json.MarshalIndent(list, "", "  ")
}

// Default is the process-wide registry (populated by catalog.go init).
var Default = NewRegistry()

func argString(args map[string]interface{}, key, def string) string {
	if v, ok := args[key]; ok && v != nil {
		s := fmt.Sprint(v)
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return def
}

func argInt(args map[string]interface{}, key string, def int) int {
	if v, ok := args[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		default:
			var i int
			if _, err := fmt.Sscan(fmt.Sprint(v), &i); err == nil {
				return i
			}
		}
	}
	return def
}

func argBool(args map[string]interface{}, key string, def bool) bool {
	if v, ok := args[key]; ok && v != nil {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			return b == "true" || b == "1" || b == "yes"
		}
	}
	return def
}

// okResult helper.
func okResult(msg string, data map[string]interface{}) *Result {
	return &Result{OK: true, Message: msg, Data: data}
}
