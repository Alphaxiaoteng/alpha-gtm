package agenthub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"alpha-nexus-egolite/internal/tools"
)

// ToolBroker is the only bridge from local CLI providers to application
// capabilities. It accepts Registry tools, never arbitrary HTTP or shell.
type ToolBroker struct {
	store    *Store
	registry *tools.Registry
	runtime  *tools.Runtime
	now      func() time.Time

	mu       sync.Mutex
	l1Memory map[string]time.Time
}

func NewToolBroker(store *Store, registry *tools.Registry, runtime *tools.Runtime) (*ToolBroker, error) {
	if store == nil || registry == nil || runtime == nil {
		return nil, fmt.Errorf("agent tool broker requires store, registry and runtime")
	}
	if _, err := registry.AgentList(); err != nil {
		return nil, err
	}
	return &ToolBroker{store: store, registry: registry, runtime: runtime, now: time.Now, l1Memory: map[string]time.Time{}}, nil
}

func (b *ToolBroker) Tools() ([]tools.Def, error) {
	return b.registry.AgentList()
}

func (b *ToolBroker) Propose(ctx context.Context, runID, name string, input map[string]interface{}) (*ToolCall, error) {
	run, err := b.store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != "running" {
		return nil, ErrRunNotActive
	}
	def, ok := b.registry.Get(name)
	if !ok || !def.AgentVisible {
		return nil, fmt.Errorf("AGENT_TOOL_NOT_ALLOWED")
	}
	args, err := freezeAgentArgs(name, input)
	if err != nil {
		return nil, err
	}
	if name == "media.import" {
		if err := b.authorizeMediaImportPath(ctx, run.ID, args); err != nil {
			return nil, err
		}
	}
	risk, err := b.registry.AgentRiskFor(name, args)
	if err != nil {
		return nil, err
	}
	paramsJSON, err := canonicalAgentArgs(args)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(name + "\n" + paramsJSON))
	hash := hex.EncodeToString(sum[:])
	now := b.now().UTC()
	scope := agentObjectScope(args)
	call := &ToolCall{
		ID: newID("tool"), RunID: run.ID, ConversationID: run.ConversationID, ProviderID: run.ProviderID,
		ToolName: name, Workspace: def.AgentWorkspace, RiskLevel: string(risk), ParamsHash: hash,
		ImpactPreview: def.AgentImpact + scope, Status: "pending",
		ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339Nano),
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	remembered := risk == tools.AgentRiskL1 && b.isL1Remembered(run, name, scope, now)
	if risk == tools.AgentRiskL0 || remembered {
		call.Status = "proposed"
		call.ExpiresAt = ""
	}
	if err := b.store.CreateToolCall(ctx, run, call, paramsJSON, call.Status == "pending"); err != nil {
		return nil, err
	}
	_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "tool.proposed", Payload: toolEventPayload(call)})
	if call.Status == "pending" {
		payload := toolEventPayload(call)
		payload["approval_id"] = call.ApprovalID
		_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "approval.required", Payload: payload})
		return call, nil
	}
	return b.execute(ctx, run, call, paramsJSON, "proposed")
}

func (b *ToolBroker) Resolve(ctx context.Context, approvalID, status string, remember bool) (*Approval, *ToolCall, error) {
	if status != "approved" && status != "rejected" {
		return nil, nil, fmt.Errorf("invalid approval resolution")
	}
	call, paramsJSON, err := b.store.GetToolCallByApproval(ctx, approvalID)
	if err != nil {
		return nil, nil, err
	}
	run, err := b.store.GetRun(ctx, call.RunID)
	if err != nil {
		return nil, nil, err
	}
	if status == "approved" && isDomainApprovalTool(call.ToolName) &&
		(call.Status != "pending" || call.RiskLevel != string(tools.AgentRiskL3)) {
		return nil, nil, fmt.Errorf("AGENT_DOMAIN_APPROVAL_NOT_ALLOWED")
	}
	if status == "rejected" {
		approval, err := b.store.ResolveApproval(ctx, approvalID, status)
		if err != nil {
			return nil, nil, err
		}
		if err := b.store.MarkToolCall(ctx, call.ID, "pending", "rejected", "", "AGENT_TOOL_REJECTED"); err != nil {
			return nil, nil, err
		}
		call.Status = "rejected"
		_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "approval.resolved", Payload: map[string]any{"approval_id": approvalID, "tool_call_id": call.ID, "status": status}})
		return approval, call, nil
	}
	var frozen map[string]interface{}
	if err := json.Unmarshal([]byte(paramsJSON), &frozen); err != nil {
		return nil, nil, fmt.Errorf("decode frozen tool args: %w", err)
	}
	canonical, err := canonicalAgentArgs(frozen)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256([]byte(call.ToolName + "\n" + canonical))
	if hex.EncodeToString(sum[:]) != call.ParamsHash {
		return nil, nil, fmt.Errorf("AGENT_TOOL_PARAMS_TAMPERED")
	}
	approval, err := b.store.ResolveApproval(ctx, approvalID, status)
	if err != nil {
		return nil, nil, err
	}
	_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "approval.resolved", Payload: map[string]any{"approval_id": approvalID, "tool_call_id": call.ID, "status": status}})
	executed, err := b.execute(ctx, run, call, paramsJSON, "pending")
	if err == nil && remember && call.RiskLevel == string(tools.AgentRiskL1) {
		b.rememberL1(run, call.ToolName, agentObjectScope(frozen), b.now().UTC())
	}
	return approval, executed, err
}

func (b *ToolBroker) ClearApprovalMemory() {
	b.mu.Lock()
	b.l1Memory = map[string]time.Time{}
	b.mu.Unlock()
}

func (b *ToolBroker) execute(ctx context.Context, run *Run, call *ToolCall, paramsJSON, fromStatus string) (*ToolCall, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(paramsJSON), &args); err != nil {
		return b.failBeforeExecution(ctx, run, call, fromStatus, "AGENT_TOOL_ARGUMENTS_INVALID")
	}
	if isDomainApprovalTool(call.ToolName) {
		if fromStatus != "pending" || call.Status != "pending" || call.RiskLevel != string(tools.AgentRiskL3) {
			return b.failBeforeExecution(ctx, run, call, fromStatus, "AGENT_DOMAIN_APPROVAL_NOT_ALLOWED")
		}
		if b.runtime == nil || b.runtime.AuthorizeAgentTool == nil {
			return b.failBeforeExecution(ctx, run, call, fromStatus, "AGENT_DOMAIN_APPROVAL_UNAVAILABLE")
		}
		authorizedArgs, err := b.runtime.AuthorizeAgentTool(ctx, call.ToolName, cloneAgentArgs(args))
		if err != nil {
			return b.failBeforeExecution(ctx, run, call, fromStatus, "AGENT_DOMAIN_APPROVAL_FAILED")
		}
		if err := validateDomainApprovalArgs(call.ToolName, authorizedArgs); err != nil {
			return b.failBeforeExecution(ctx, run, call, fromStatus, "AGENT_DOMAIN_APPROVAL_INVALID")
		}
		// These credentials exist only in this local call frame. They are not
		// written back to params_json, ToolCall, or any Agent event.
		args = authorizedArgs
	}
	// Confirmation fields only originate here after an approved frozen call.
	// Publish and shop actions have their own one-time domain approval and
	// strict schemas; neither receives a generic top-level confirm.
	if call.RiskLevel != string(tools.AgentRiskL0) && !isPublishPlanTool(call.ToolName) && !isDomainApprovalTool(call.ToolName) {
		args["confirm"] = true
	}
	if err := b.store.MarkToolCall(ctx, call.ID, fromStatus, "running", "", ""); err != nil {
		return nil, err
	}
	call.Status = "running"
	_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "tool.started", Payload: toolEventPayload(call)})
	localRuntime := b.runtime.WithTrustedLocalUI(call.RiskLevel != string(tools.AgentRiskL0))
	result, callErr := b.registry.Call(ctx, localRuntime, call.ToolName, args)
	redacted := redactAgentResult(result)
	resultJSON := ""
	if redacted != nil {
		encoded, _ := json.Marshal(redacted)
		resultJSON = string(encoded)
		call.Result = map[string]any{}
		_ = json.Unmarshal(encoded, &call.Result)
	}
	if callErr != nil || result == nil || !result.OK {
		code := "AGENT_TOOL_FAILED"
		if result != nil && result.Error != "" {
			code = result.Error
		}
		_ = b.store.MarkToolCall(ctx, call.ID, "running", "failed", resultJSON, code)
		call.Status = "failed"
		call.ErrorCode = code
		payload := toolEventPayload(call)
		payload["error_code"] = code
		_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "tool.failed", Payload: payload})
		if callErr == nil {
			callErr = fmt.Errorf("%s", code)
		}
		return call, callErr
	}
	if err := b.store.MarkToolCall(ctx, call.ID, "running", "completed", resultJSON, ""); err != nil {
		return nil, err
	}
	call.Status = "completed"
	_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "tool.completed", Payload: toolEventPayload(call)})
	return call, nil
}

func (b *ToolBroker) failBeforeExecution(ctx context.Context, run *Run, call *ToolCall, fromStatus, code string) (*ToolCall, error) {
	if err := b.store.MarkToolCall(ctx, call.ID, fromStatus, "failed", "", code); err != nil {
		return nil, err
	}
	call.Status = "failed"
	call.ErrorCode = code
	payload := toolEventPayload(call)
	payload["error_code"] = code
	_, _ = b.store.AppendEvent(ctx, run, ProviderEvent{Type: "tool.failed", Payload: payload})
	return call, fmt.Errorf("%s", code)
}

func isPublishPlanTool(name string) bool {
	switch name {
	case "publish.plan.freeze", "publish.plan.prepare", "publish.plan.submit":
		return true
	default:
		return false
	}
}

func isDomainApprovalTool(name string) bool {
	switch name {
	case "publish.plan.prepare", "publish.plan.submit", "shop.listing.prepare":
		return true
	default:
		return false
	}
}

func validateDomainApprovalArgs(toolName string, args map[string]interface{}) error {
	switch toolName {
	case "publish.plan.prepare", "publish.plan.submit":
		if !hasExactAgentKeys(args, "plan_id", "snapshot_hash", "approval_id", "nonce") {
			return fmt.Errorf("publish domain approval args are invalid")
		}
		if !validAgentPositiveInteger(args["plan_id"]) ||
			!validAgentString(args["snapshot_hash"]) ||
			!validAgentString(args["approval_id"]) ||
			!validAgentString(args["nonce"]) {
			return fmt.Errorf("publish domain approval args are invalid")
		}
		return nil
	case "shop.listing.prepare":
		input, ok := args["input"].(map[string]interface{})
		if !ok || !hasExactAgentKeys(args, "input") ||
			!hasExactAgentKeys(input, "plan_id", "snapshot_hash", "approval_id", "nonce") {
			return fmt.Errorf("shop domain approval args are invalid")
		}
		if !validAgentPositiveInteger(input["plan_id"]) ||
			!validAgentString(input["snapshot_hash"]) ||
			!validAgentString(input["approval_id"]) ||
			!validAgentString(input["nonce"]) {
			return fmt.Errorf("shop domain approval args are invalid")
		}
		return nil
	default:
		return fmt.Errorf("domain approval is not supported for %s", toolName)
	}
}

func hasExactAgentKeys(args map[string]interface{}, keys ...string) bool {
	if args == nil || len(args) != len(keys) {
		return false
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	for key := range args {
		if _, ok := wanted[key]; !ok {
			return false
		}
	}
	return true
}

func validAgentPositiveInteger(value interface{}) bool {
	switch number := value.(type) {
	case int:
		return number > 0
	case int8:
		return number > 0
	case int16:
		return number > 0
	case int32:
		return number > 0
	case int64:
		return number > 0
	case uint:
		return number > 0
	case uint8:
		return number > 0
	case uint16:
		return number > 0
	case uint32:
		return number > 0
	case uint64:
		return number > 0 && number <= math.MaxInt64
	case float64:
		return number > 0 && number < float64(math.MaxInt64) && number == math.Trunc(number)
	case json.Number:
		integer, err := strconv.ParseInt(string(number), 10, 64)
		return err == nil && integer > 0
	default:
		return false
	}
}

func validAgentString(value interface{}) bool {
	text, ok := value.(string)
	return ok && text != "" && text == strings.TrimSpace(text) && len(text) <= 4096 &&
		!strings.ContainsAny(text, "\x00\r\n\t")
}

func cloneAgentArgs(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return nil
	}
	cloned := make(map[string]interface{}, len(args))
	for key, value := range args {
		cloned[key] = cloneAgentValue(value)
	}
	return cloned
}

func cloneAgentValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneAgentArgs(typed)
	case []interface{}:
		cloned := make([]interface{}, len(typed))
		for index, item := range typed {
			cloned[index] = cloneAgentValue(item)
		}
		return cloned
	default:
		return value
	}
}

func freezeAgentArgs(toolName string, input map[string]interface{}) (map[string]interface{}, error) {
	if input == nil {
		return map[string]interface{}{}, nil
	}
	safeInput := input
	localPath := ""
	if toolName == "media.import" {
		rawPath, ok := input["path"].(string)
		if !ok {
			return nil, fmt.Errorf("AGENT_MEDIA_PATH_NOT_AUTHORIZED")
		}
		localPath = strings.TrimSpace(rawPath)
		if len(localPath) > 4096 || strings.ContainsAny(localPath, "\x00\r\n") || !filepath.IsAbs(localPath) {
			return nil, fmt.Errorf("AGENT_MEDIA_PATH_NOT_AUTHORIZED")
		}
		safeInput = make(map[string]interface{}, len(input)-1)
		for key, item := range input {
			if key != "path" {
				safeInput[key] = item
			}
		}
	}
	value, err := freezeAgentValue("", safeInput)
	if err != nil {
		return nil, err
	}
	result, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("agent tool arguments must be an object")
	}
	if localPath != "" {
		result["path"] = localPath
	}
	return result, nil
}

func (b *ToolBroker) authorizeMediaImportPath(ctx context.Context, runID string, args map[string]interface{}) error {
	path, _ := args["path"].(string)
	clean := filepath.Clean(path)
	home, _ := os.UserHomeDir()
	if filepath.Dir(clean) == clean || (home != "" && clean == filepath.Clean(home)) || agentMediaPathProtected(clean) {
		return fmt.Errorf("AGENT_MEDIA_PATH_NOT_AUTHORIZED")
	}
	message, err := b.store.UserMessageForRun(ctx, runID)
	if err != nil {
		return err
	}
	if !messageContainsExactAgentPath(message, path) {
		return fmt.Errorf("AGENT_MEDIA_PATH_NOT_AUTHORIZED")
	}
	return nil
}

func messageContainsExactAgentPath(message, path string) bool {
	offset := 0
	for offset <= len(message) {
		index := strings.Index(message[offset:], path)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(path)
		beforeOK := start == 0 || !agentPathContinuationRune(lastRune(message[:start]))
		afterOK := end == len(message) || !agentPathContinuationRune(firstRune(message[end:]))
		if beforeOK && afterOK {
			return true
		}
		offset = end
	}
	return false
}

func firstRune(value string) rune {
	for _, current := range value {
		return current
	}
	return 0
}

func lastRune(value string) rune {
	var last rune
	for _, current := range value {
		last = current
	}
	return last
}

func agentPathContinuationRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || strings.ContainsRune("/\\._-~", value)
}

func agentMediaPathProtected(path string) bool {
	for _, segment := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		switch strings.ToLower(segment) {
		case ".env", ".git", ".ssh", ".gnupg", ".aws", ".azure", "1key", "keychains", "profiles", "session-backups":
			return true
		}
	}
	return false
}

func freezeAgentValue(key string, value interface{}) (interface{}, error) {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	switch lowerKey {
	case "confirm", "approval_id", "nonce":
		return nil, nil
	case "path", "file", "directory", "data_dir", "base_url", "api_key", "token", "cookie", "credential", "password", "secret", "cdp_port", "transport_token", "source_code", "shell", "command":
		return nil, fmt.Errorf("AGENT_TOOL_ARGUMENT_FORBIDDEN")
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for childKey, child := range typed {
			frozen, err := freezeAgentValue(childKey, child)
			if err != nil {
				return nil, err
			}
			childLower := strings.ToLower(strings.TrimSpace(childKey))
			if childLower == "confirm" || childLower == "approval_id" || childLower == "nonce" {
				continue
			}
			out[childKey] = frozen
		}
		return out, nil
	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			frozen, err := freezeAgentValue(key, item)
			if err != nil {
				return nil, err
			}
			out = append(out, frozen)
		}
		return out, nil
	case string:
		if filepath.IsAbs(typed) || strings.Contains(strings.ToLower(typed), "file://") {
			return nil, fmt.Errorf("AGENT_TOOL_ARGUMENT_FORBIDDEN")
		}
		return typed, nil
	default:
		return value, nil
	}
}

func canonicalAgentArgs(args map[string]interface{}) (string, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("encode agent tool args: %w", err)
	}
	return string(encoded), nil
}

func agentObjectScope(args map[string]interface{}) string {
	keys := make([]string, 0)
	for key, value := range args {
		lower := strings.ToLower(key)
		if lower == "id" || strings.HasSuffix(lower, "_id") || strings.HasSuffix(lower, "_ids") {
			encoded, _ := json.Marshal(value)
			keys = append(keys, key+"="+string(encoded))
		}
	}
	if path, ok := args["path"].(string); ok && strings.TrimSpace(path) != "" {
		keys = append(keys, "路径="+strconv.Quote(filepath.Base(filepath.Clean(path))))
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return "；对象 " + strings.Join(keys, ", ")
}

func toolEventPayload(call *ToolCall) map[string]any {
	return map[string]any{
		"tool_call_id": call.ID, "tool_name": call.ToolName, "workspace": call.Workspace,
		"risk_level": call.RiskLevel, "impact_preview": call.ImpactPreview, "status": call.Status,
	}
}

func redactAgentResult(result *tools.Result) *tools.Result {
	if result == nil {
		return nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return &tools.Result{OK: false, Error: "AGENT_RESULT_REDACTION_FAILED"}
	}
	var value map[string]interface{}
	if json.Unmarshal(encoded, &value) != nil {
		return &tools.Result{OK: false, Error: "AGENT_RESULT_REDACTION_FAILED"}
	}
	redactAgentMap(value)
	reencoded, _ := json.Marshal(value)
	var redacted tools.Result
	_ = json.Unmarshal(reencoded, &redacted)
	return &redacted
}

func redactAgentMap(value map[string]interface{}) {
	for key, item := range value {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "path") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "cookie") || strings.Contains(lower, "credential") ||
			lower == "data_dir" || lower == "base_url" || lower == "api_key" || lower == "cdp_port" ||
			lower == "publish_data" || lower == "raw_json" || lower == "approval_id" || lower == "nonce" || lower == "confirm" {
			delete(value, key)
			continue
		}
		switch child := item.(type) {
		case map[string]interface{}:
			redactAgentMap(child)
		case []interface{}:
			for _, nested := range child {
				if nestedMap, ok := nested.(map[string]interface{}); ok {
					redactAgentMap(nestedMap)
				}
			}
		}
	}
}

func (b *ToolBroker) memoryKey(run *Run, toolName, scope string) string {
	return string(run.ProviderID) + "\x00" + run.ConversationID + "\x00" + toolName + "\x00" + scope
}

func (b *ToolBroker) isL1Remembered(run *Run, toolName, scope string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := b.memoryKey(run, toolName, scope)
	expires := b.l1Memory[key]
	if expires.IsZero() || !now.Before(expires) {
		delete(b.l1Memory, key)
		return false
	}
	return true
}

func (b *ToolBroker) rememberL1(run *Run, toolName, scope string, now time.Time) {
	b.mu.Lock()
	b.l1Memory[b.memoryKey(run, toolName, scope)] = now.Add(30 * time.Minute)
	b.mu.Unlock()
}
