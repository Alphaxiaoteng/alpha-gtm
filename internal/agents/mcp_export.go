package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"alpha-nexus-egolite/internal/tools"
)

// MCPExport is tools.Registry projected for MCP / OpenCode / Claude Desktop.
type MCPExport struct {
	ToolCount        int                    `json:"tool_count"`
	Categories       map[string]int         `json:"categories"`
	SchemaPath       string                 `json:"schema_path,omitempty"`
	SchemaPreview    string                 `json:"schema_preview"`
	MCPConfigSnippet map[string]interface{} `json:"mcp_config_snippet"`
	OpenCodeHint     string                 `json:"opencode_hint"`
	Tools            []MCPTool              `json:"tools"`
}

// MCPTool mirrors MCP tools/list entry shape (subset).
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Category    string                 `json:"_meta_category,omitempty"`
	SideEffect  string                 `json:"_meta_side_effect,omitempty"`
}

// BuildMCPExport converts the in-process registry to MCP-shaped tool defs.
func BuildMCPExport(dataDir string) (*MCPExport, error) {
	defs := tools.Default.List()
	cats := map[string]int{}
	out := make([]MCPTool, 0, len(defs))
	for _, d := range defs {
		cats[d.Category]++
		props := map[string]interface{}{}
		required := []string{}
		for _, p := range d.Params {
			props[p.Name] = map[string]interface{}{
				"type":        mapJSONSchemaType(p.Type),
				"description": p.Description,
			}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		schema := map[string]interface{}{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		out = append(out, MCPTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: schema,
			Category:    d.Category,
			SideEffect:  d.SideEffect,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	schemaPath := ""
	if dataDir != "" {
		dir := filepath.Join(dataDir, "agent")
		_ = os.MkdirAll(dir, 0o755)
		schemaPath = filepath.Join(dir, "mcp-tools.json")
		payload := map[string]interface{}{
			"name":    "alpha-nexus",
			"version": "1",
			"tools":   out,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		_ = os.WriteFile(schemaPath, b, 0o644)
	}

	// OpenCode / Claude Desktop — same shape as turbopush-mcp install docs.
	// Server: mark3labs/mcp-go stdio (internal/mcp), not a bare JSON export.
	mcpSnippet := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"alpha-nexus": map[string]interface{}{
				"command": "alpha-nexus",
				"args":    []string{"agent", "mcp-serve"},
				"env": map[string]string{
					"ALPHA_NEXUS_DATA_DIR": dataDirOrDefault(dataDir),
				},
			},
			// optional second entry if user also runs TurboPush MCP side-by-side
			"turbo-push": map[string]interface{}{
				"command": "turbo-push-mcp",
				"note":    "https://github.com/xueyc1f/turbopush-mcp — HTTP proxy to TurboPush app",
			},
		},
	}

	previewBytes, _ := json.MarshalIndent(out[:min(5, len(out))], "", "  ")
	preview := string(previewBytes)
	if len(out) > 5 {
		preview += fmt.Sprintf("\n… +%d more tools (see schema_path)", len(out)-5)
	}

	return &MCPExport{
		ToolCount:        len(out),
		Categories:       cats,
		SchemaPath:       schemaPath,
		SchemaPreview:    preview,
		MCPConfigSnippet: mcpSnippet,
		OpenCodeHint:     "opencode mcp add alpha-nexus -- alpha-nexus-egolite agent mcp-serve",
		Tools:            out,
	}, nil
}

func mapJSONSchemaType(t string) string {
	switch strings.ToLower(t) {
	case "int", "integer", "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string"
	}
}

func dataDirOrDefault(d string) string {
	if d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".alpha-nexus")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// marshalIndent shared helper (avoid import cycle / dup).
func marshalIndent(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
