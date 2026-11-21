package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agentcfg"
	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/tools"
	"alpha-nexus-egolite/internal/trace"
)

const (
	errLLMGatewayUnavailable = "LLM_GATEWAY_UNAVAILABLE"
	errLLMModelNotVerified   = "LLM_MODEL_NOT_VERIFIED"
	errLLMToolPlanInvalid    = "LLM_TOOL_PLAN_INVALID"
	errExternalActionConfirm = "AUTOMATED_EXTERNAL_ACTION_REQUIRES_USER_CONFIRMATION"
	errMediaImportPath       = "MEDIA_IMPORT_PATH_NOT_ALLOWED"
)

type toolPlannerCompleter interface {
	Complete(context.Context, string) (string, error)
}

var newToolPlannerCompleter = func(model string) (toolPlannerCompleter, error) {
	return llm.NewArkResponsesClient(model, 60*time.Second)
}

// LLMToolsAgent: model plans tool calls from tools.Registry schema, ToolAgent executes.
// Lightweight — no extra framework; uses the verified Ark Responses model
// selected from the current data directory.
func LLMToolsAgent(ctx context.Context, req SchemeRunRequest, tid string) (*SchemeRunResult, error) {
	schemaBytes, err := tools.Default.SchemaJSON()
	if err != nil {
		return &SchemeRunResult{OK: false, Error: err.Error()}, err
	}
	// keep prompt small: only name+desc+params of first N tools by category priority
	compact := compactToolSchema(schemaBytes, 48)

	sys := `You are Alpha-Nexus Tool Planner. Reply with ONE JSON object only:
{"steps":[{"tool":"name","args":{...}}],"reason":"..."}
Rules:
- Only use tools from the catalog.
- Prefer read tools: account.list, ego.space.list, tikhub.config.status.
- For an explicit account login-check request, skill.run may use only social.account-check with explicit account_ids. For local material inventory, skill.run may use only social.materials-list.
- Never invent credentials. Do not call publish, queue, workflow, browser-action, or external-network tools: they require a separate user action in their own screen.
- For local Studio, you may import media, save scripts, and create a clip plan. Never call studio.clip.execute; a human must explicitly confirm that separate file-writing action.
- When the goal lists current-session attachments, media.import may use only those exact paths. Never read or import another local path.
- Max 5 steps. If unsure, only account.list + ego.space.list.`

	model, err := verifiedToolPlannerModel(req.DataDir, req.Model)
	if err != nil {
		return llmToolPlanFailure(errLLMModelNotVerified, "所选模型尚未验证；未执行任何工具。", "", "")
	}
	completer, err := newToolPlannerCompleter(model)
	if err != nil || completer == nil {
		return llmToolPlanFailure(errLLMGatewayUnavailable, "模型网关不可用；未执行任何工具。", "", model)
	}
	input := fmt.Sprintf(
		"[system]\n%s\n[/system]\n\n[goal]\n%s\n[/goal]\n\n[tools]\n%s\n[/tools]",
		sys,
		req.Goal,
		compact,
	)
	raw, err := completer.Complete(ctx, input)
	if err != nil {
		return llmToolPlanFailure(errLLMGatewayUnavailable, "模型网关不可用；未执行任何工具。", "", model)
	}
	steps, planNote := parseToolPlan(raw)
	if req.RestrictMediaImport && !mediaImportPathsAllowed(steps, req.AllowedMediaImportPaths) {
		return &SchemeRunResult{
			OK:      false,
			Message: "素材导入路径不在当前会话附件中；未执行任何工具。",
			Output:  strings.TrimSpace(raw),
			Error:   errMediaImportPath,
			Steps:   blockedMediaImportSteps(steps),
			Data: map[string]interface{}{
				"engine":   "llm.ArkResponsesClient + ToolAgent",
				"model":    model,
				"executed": false,
			},
		}, nil
	}
	steps, removedStudioExecution := removeAutomaticStudioExecution(steps)
	steps, removedExternalActions := removeAutomaticExternalActions(steps)
	if removedStudioExecution {
		planNote = strings.TrimSpace(planNote + "; studio.clip.execute removed: user confirmation is required")
	}
	if len(removedExternalActions) > 0 {
		planNote = strings.TrimSpace(planNote + "; external action removed: user confirmation is required")
	}
	blockedCode, blockedMessage := plannerBlockedResult(removedStudioExecution, removedExternalActions)
	blockedSteps := plannerBlockedSteps(removedStudioExecution, removedExternalActions)
	if len(steps) == 0 && blockedCode != "" {
		return &SchemeRunResult{
			OK:      false,
			Message: blockedMessage,
			Output:  strings.TrimSpace(raw),
			Error:   blockedCode,
			Steps:   blockedSteps,
			Data: map[string]interface{}{
				"engine":                   "llm.ArkResponsesClient + ToolAgent",
				"plan_note":                planNote,
				"model":                    model,
				"studio_execution_removed": removedStudioExecution,
				"external_actions_removed": len(removedExternalActions),
			},
		}, nil
	}
	if len(steps) == 0 {
		return llmToolPlanFailure(errLLMToolPlanInvalid, "模型未返回可执行工具计划；未执行任何工具。", raw, model)
	}

	if req.DryRun {
		// dry: return plan only, do not execute side-effect tools; still allow listing plan
		planSteps := make([]map[string]interface{}, 0, len(steps))
		for i, s := range steps {
			planSteps = append(planSteps, map[string]interface{}{
				"index": i, "tool": s.Tool, "args": s.Args, "executed": false, "dry_run": true,
			})
		}
		planSteps = append(planSteps, blockedSteps...)
		out := &SchemeRunResult{
			OK:      blockedCode == "",
			Message: "llm_tools dry_run: plan only, not executed",
			Output:  strings.TrimSpace(raw),
			Steps:   planSteps,
			Data: map[string]interface{}{
				"engine":                   "llm.ArkResponsesClient + tools.Registry",
				"plan_note":                planNote,
				"model":                    model,
				"executed":                 false,
				"studio_execution_removed": removedStudioExecution,
				"external_actions_removed": len(removedExternalActions),
			},
		}
		if blockedCode != "" {
			out.Message = blockedMessage
			out.Error = blockedCode
		}
		return out, nil
	}

	in := ToolAgentInput{
		Goal:        req.Goal,
		Steps:       steps,
		MaxRetries:  1,
		StopOnError: false,
		Runtime:     req.Runtime,
		// safety: don't auto-confirm paid network unless steps set confirm
		RequireConfirmNetwork: true,
	}
	ac := &Context{TaskID: "scheme-llm-tools", MaxSteps: 30, TraceID: trace.ID(tid)}
	rep, err := ToolAgent(ctx, ac, in)
	out := &SchemeRunResult{
		OK:      rep != nil && rep.OK,
		Message: "llm_tools loop finished",
		Output:  strings.TrimSpace(raw),
		Data: map[string]interface{}{
			"engine":                   "llm.ArkResponsesClient + ToolAgent",
			"plan_note":                planNote,
			"model":                    model,
			"studio_execution_removed": removedStudioExecution,
			"external_actions_removed": len(removedExternalActions),
		},
	}
	if rep != nil {
		out.Steps = rep.Steps
		out.Message = rep.Message
		out.OK = rep.OK
	}
	if blockedCode != "" {
		out.OK = false
		out.Message = blockedMessage
		out.Error = blockedCode
		out.Steps = append(out.Steps, blockedSteps...)
	}
	if err != nil {
		out.Error = err.Error()
		out.OK = false
	}
	return out, err
}

func verifiedToolPlannerModel(dataDir, requested string) (string, error) {
	cfg, err := agentcfg.Load(dataDir)
	if err != nil {
		return "", err
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = cfg.DefaultModel
	}
	candidate, ok := agentcfg.CandidateModel(requested)
	if !ok || !agentcfg.ModelEnabled(cfg, candidate.ID) {
		return "", errors.New(errLLMModelNotVerified)
	}
	return candidate.ID, nil
}

// mediaImportPathsAllowed makes the chat attachment boundary enforceable at
// execution time; prompt instructions alone cannot be a security boundary.
func mediaImportPathsAllowed(steps []ToolAgentStep, allowed []string) bool {
	paths := make(map[string]struct{}, len(allowed))
	for _, path := range allowed {
		if path = strings.TrimSpace(path); path != "" {
			paths[path] = struct{}{}
		}
	}
	for _, step := range steps {
		if step.Tool != "media.import" {
			continue
		}
		path, ok := step.Args["path"].(string)
		if !ok {
			return false
		}
		if _, ok := paths[strings.TrimSpace(path)]; !ok {
			return false
		}
	}
	return true
}

func blockedMediaImportSteps(steps []ToolAgentStep) []map[string]interface{} {
	blocked := make([]map[string]interface{}, 0)
	for i, step := range steps {
		if step.Tool == "media.import" {
			blocked = append(blocked, map[string]interface{}{
				"index": i, "tool": step.Tool, "executed": false, "error": errMediaImportPath,
			})
		}
	}
	return blocked
}

func llmToolPlanFailure(code, message, output, model string) (*SchemeRunResult, error) {
	return &SchemeRunResult{
		OK:      false,
		Message: message,
		Output:  strings.TrimSpace(output),
		Error:   code,
		Data: map[string]interface{}{
			"engine":   "llm.ArkResponsesClient planner",
			"model":    model,
			"executed": false,
		},
	}, errors.New(code)
}

func removeAutomaticStudioExecution(steps []ToolAgentStep) ([]ToolAgentStep, bool) {
	safe := make([]ToolAgentStep, 0, len(steps))
	removed := false
	for _, step := range steps {
		if step.Tool == "studio.clip.execute" {
			removed = true
			continue
		}
		safe = append(safe, step)
	}
	return safe, removed
}

func removeAutomaticExternalActions(steps []ToolAgentStep) ([]ToolAgentStep, []ToolAgentStep) {
	safe := make([]ToolAgentStep, 0, len(steps))
	removed := make([]ToolAgentStep, 0)
	for _, step := range steps {
		def, ok := tools.Default.Get(step.Tool)
		if !ok {
			// Unknown tools must not become implicitly safe when a runtime was removed.
			removed = append(removed, step)
			continue
		}
		if plannerStepNeedsSeparateConfirmation(step.Tool, def.SideEffect, step.Args) {
			removed = append(removed, step)
			continue
		}
		safe = append(safe, step)
	}
	return safe, removed
}

func plannerActionNeedsSeparateConfirmation(name, sideEffect string) bool {
	// skill.run is visible to the planner, but its runtime arguments are
	// checked by plannerStepNeedsSeparateConfirmation below.
	if name == "skill.run" {
		return false
	}
	if name == "studio.clip.execute" || name == "workflow.run" || name == "queue.enqueue" || name == "batch.publish" {
		return true
	}
	return sideEffect == "external_publish" || sideEffect == "network" || sideEffect == "browser"
}

func plannerStepNeedsSeparateConfirmation(name, sideEffect string, args map[string]interface{}) bool {
	if name == "skill.run" {
		// ponytail: only two fixed, non-navigating workflows may be started by
		// a model. Publish preparation keeps its one-time local UI approval.
		id, _ := args["skill_id"].(string)
		return id != "social.account-check" && id != "social.materials-list"
	}
	return plannerActionNeedsSeparateConfirmation(name, sideEffect)
}

func plannerBlockedResult(removedStudio bool, removedExternal []ToolAgentStep) (code, message string) {
	if len(removedExternal) > 0 {
		return errExternalActionConfirm, "发布、浏览器、任务入队或外部网络动作需要在对应界面中单独确认；未执行这些工具。"
	}
	if removedStudio {
		return "STUDIO_CLIP_EXECUTION_REQUIRES_USER_CONFIRMATION", "本地剪辑执行需要单独确认；未执行该工具。"
	}
	return "", ""
}

func plannerBlockedSteps(removedStudio bool, removedExternal []ToolAgentStep) []map[string]interface{} {
	steps := make([]map[string]interface{}, 0, len(removedExternal)+1)
	if removedStudio {
		steps = append(steps, map[string]interface{}{
			"tool": "studio.clip.execute", "executed": false, "reason": "requires a separate confirm=true call",
		})
	}
	for _, step := range removedExternal {
		steps = append(steps, map[string]interface{}{
			"tool": step.Tool, "args": step.Args, "executed": false, "reason": "requires a separate user confirmation path",
		})
	}
	return steps
}

func parseToolPlan(raw string) ([]ToolAgentStep, string) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var plan struct {
		Steps  []ToolAgentStep `json:"steps"`
		Reason string          `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, ""
	}
	// filter empty tool names
	var out []ToolAgentStep
	for _, s := range plan.Steps {
		if strings.TrimSpace(s.Tool) == "" {
			continue
		}
		if s.Args == nil {
			s.Args = map[string]interface{}{}
		}
		out = append(out, s)
		if len(out) >= 5 {
			break
		}
	}
	return out, plan.Reason
}

func compactToolSchema(schemaJSON []byte, max int) string {
	var list []map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &list); err != nil {
		return string(schemaJSON)
	}
	// prioritize useful categories
	prio := map[string]int{
		"account": 0, "browser": 1, "tikhub": 2, "ops": 3, "workflow": 4, "publish": 5, "agent": 6, "media": 7, "studio": 8, "meta": 9,
	}
	type row struct {
		name, cat, desc string
		score           int
	}
	var rows []row
	for _, t := range list {
		name, _ := t["name"].(string)
		cat, _ := t["category"].(string)
		desc, _ := t["description"].(string)
		sideEffect, _ := t["side_effect"].(string)
		if plannerActionNeedsSeparateConfirmation(name, sideEffect) {
			continue
		}
		sc, ok := prio[cat]
		if !ok {
			sc = 50
		}
		rows = append(rows, row{name, cat, desc, sc})
	}
	// simple insertion sort by score
	for i := 1; i < len(rows); i++ {
		j := i
		for j > 0 && rows[j].score < rows[j-1].score {
			rows[j], rows[j-1] = rows[j-1], rows[j]
			j--
		}
	}
	if max > 0 && len(rows) > max {
		rows = rows[:max]
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("- %s [%s]: %s\n", r.name, r.cat, r.desc))
	}
	return b.String()
}
