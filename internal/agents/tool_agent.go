package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/tools"
	"alpha-nexus-egolite/internal/trace"
)

// ToolAgent is the generic executor that can call the full tools.Registry.
// Role agents (Content/Publish/…) plan; ToolAgent executes with whitelist + retries.
type ToolAgentInput struct {
	Goal                  string          `json:"goal"`
	Steps                 []ToolAgentStep `json:"steps"` // explicit plan; if empty, goal is only logged
	MaxRetries            int             `json:"max_retries"`
	StopOnError           bool            `json:"stop_on_error"`
	RequireConfirmNetwork bool            `json:"require_confirm_network"`
	Allowed               []string        `json:"allowed_tools"` // empty = all
	Runtime               *tools.Runtime  `json:"-"`
}

type ToolAgentStep struct {
	Tool string                 `json:"tool"`
	Args map[string]interface{} `json:"args"`
}

type ToolAgentReport struct {
	OK      bool                     `json:"ok"`
	Goal    string                   `json:"goal"`
	Steps   []map[string]interface{} `json:"steps"`
	TraceID string                   `json:"trace_id"`
	Message string                   `json:"message"`
}

// ToolAgent runs ordered tool steps with retry. No LLM required.
func ToolAgent(ctx context.Context, ac *Context, in ToolAgentInput) (*ToolAgentReport, error) {
	if ac == nil {
		ac = &Context{TaskID: "tool-agent", MaxSteps: 50, TraceID: trace.New()}
	}
	ctx, traceID := trace.Ensure(trace.WithID(ctx, ac.TraceID))
	ac.TraceID = traceID
	ctx = observability.WithCorrelation(ctx, observability.Correlation{TaskID: ac.TaskID})
	if in.MaxRetries < 0 {
		in.MaxRetries = 0
	}
	if in.MaxRetries == 0 {
		in.MaxRetries = 1
	}
	allow := map[string]bool{}
	for _, a := range in.Allowed {
		allow[a] = true
	}
	rt := in.Runtime
	if rt == nil {
		rt = &tools.Runtime{}
	}
	rep := &ToolAgentReport{Goal: in.Goal, TraceID: string(ac.TraceID), Steps: []map[string]interface{}{}}
	if len(in.Steps) == 0 {
		rep.OK = true
		rep.Message = "no steps; use tools.schema to plan"
		return rep, nil
	}
	allOK := true
	// TODO: Space-aware execution (use SpaceControl for handoff/resume in AgentPush / publish steps)
	// Integration point: check if step.Tool involves browser or publish, then wrap with SpaceControl
	for i, step := range in.Steps {
		if err := ac.Step("tool." + step.Tool); err != nil {
			return rep, err
		}
		if len(allow) > 0 && !allow[step.Tool] {
			allOK = false
			rep.Steps = append(rep.Steps, map[string]interface{}{
				"index": i, "tool": step.Tool, "ok": false, "error": "TOOL_NOT_ALLOWED",
			})
			if in.StopOnError {
				break
			}
			continue
		}
		args := step.Args
		if args == nil {
			args = map[string]interface{}{}
		}
		// Safety: network tools need confirm unless already set
		if in.RequireConfirmNetwork && isNetworkTool(step.Tool) {
			if v, ok := args["confirm"]; !ok || v == false || v == "false" {
				args["confirm"] = false // force dry plan unless caller set true
			}
		}
		var last *tools.Result
		var lastErr error
		attempts := toolAttempts(step.Tool, in.MaxRetries)
		for attempt := 1; attempt <= attempts; attempt++ {
			last, lastErr = tools.Default.Call(ctx, rt, step.Tool, args)
			if lastErr == nil && last != nil && last.OK {
				break
			}
			if attempt < attempts {
				select {
				case <-ctx.Done():
					return rep, ctx.Err()
				case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
				}
			}
		}
		entry := map[string]interface{}{"index": i, "tool": step.Tool}
		if last != nil {
			entry["ok"] = last.OK
			entry["message"] = last.Message
			entry["error"] = last.Error
			entry["data"] = last.Data
			entry["duration"] = last.Duration
			if !last.OK {
				allOK = false
			}
		}
		if lastErr != nil {
			allOK = false
			entry["error"] = lastErr.Error()
		}
		rep.Steps = append(rep.Steps, entry)
		if !allOK && in.StopOnError {
			break
		}
	}
	rep.OK = allOK
	if allOK {
		rep.Message = "all steps ok"
	} else {
		rep.Message = "finished with errors"
	}
	return rep, nil
}

// toolAttempts 只允许重试无副作用工具，避免 Agent 因超时重复创建脚本、方案或文件。
func toolAttempts(name string, requested int) int {
	if requested < 1 {
		requested = 1
	}
	if def, ok := tools.Default.Get(name); ok && def.SideEffect != "read" {
		return 1
	}
	return requested
}

func isNetworkTool(name string) bool {
	return strings.HasPrefix(name, "tikhub.") && name != "tikhub.config.status" &&
		name != "tikhub.user.list" && name != "tikhub.user.add"
}

// ParseToolAgentJSON builds input from agent CLI / HTTP body.
func ParseToolAgentJSON(raw []byte) (ToolAgentInput, error) {
	var in ToolAgentInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, fmt.Errorf("invalid tool agent json: %w", err)
	}
	return in, nil
}
