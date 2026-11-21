package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"alpha-nexus-egolite/internal/mcp"
	"alpha-nexus-egolite/internal/tools"
)

// ServeMCPStdio starts the TurboPush-style MCP server.
// Agent scheme tools registered via ExtraRegister (no import cycle).
func ServeMCPStdio(ctx context.Context, rt *tools.Runtime) error {
	dataDir := ""
	if rt != nil {
		dataDir = rt.DataDir
	}
	return mcp.ServeStdio(ctx, mcp.Config{
		DataDir: dataDir,
		Runtime: rt,
		ExtraRegister: func(s *mcp.Server) {
			registerSchemeMCPTools(s, rt, dataDir)
		},
	})
}

func registerSchemeMCPTools(s *mcp.Server, rt *tools.Runtime, dataDir string) {
	s.AddTool(mcp.ToolDef{
		Name:        "agent_schemes",
		Description: "列出 multi-scheme agent 接入方案（native/llm_tools/opencode/mcp_export）",
		SideEffect:  "read",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return ListSchemes(), nil
		},
	})

	s.AddTool(mcp.ToolDef{
		Name:        "agent_scheme_run",
		Description: "运行一个 agent scheme。默认 dry_run=true。",
		SideEffect:  "browser",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scheme":    map[string]any{"type": "string", "description": "native|llm_tools|opencode|mcp_export"},
				"goal":      map[string]any{"type": "string"},
				"dry_run":   map[string]any{"type": "boolean"},
				"max_steps": map[string]any{"type": "number"},
			},
			"required": []string{"scheme"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			scheme, _ := args["scheme"].(string)
			if scheme == "" {
				return nil, fmt.Errorf("scheme required")
			}
			dry := true
			if v, ok := args["dry_run"].(bool); ok {
				dry = v
			}
			goal, _ := args["goal"].(string)
			maxSteps := 6
			if n, ok := args["max_steps"].(float64); ok && n > 0 {
				maxSteps = int(n)
			}
			res, err := RunScheme(ctx, SchemeRunRequest{
				Scheme:   SchemeID(scheme),
				Goal:     goal,
				DryRun:   dry,
				MaxSteps: maxSteps,
				DataDir:  dataDir,
				Runtime:  rt,
			})
			if res != nil {
				// return as object (stdio layer marshals)
				_ = json.Marshal
				return res, nil
			}
			return nil, err
		},
	})
}
