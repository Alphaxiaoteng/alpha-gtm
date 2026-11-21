package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/tools"
)

type BrokerConfig struct {
	Endpoint string
	RunID    string
	Token    string
}

// ServeBrokerStdio exposes only the current run's private Agent broker. The
// external mcp-serve command remains a separate, permanently read-only server.
func ServeBrokerStdio(ctx context.Context, cfg BrokerConfig) error {
	client, endpoint, err := newBrokerClient(cfg)
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/agent/broker/tools", nil)
	setBrokerHeaders(request, cfg)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("AGENT_BROKER_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("AGENT_BROKER_AUTH_FAILED")
	}
	var body struct {
		Tools []tools.Def `json:"tools"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return fmt.Errorf("AGENT_BROKER_SCHEMA_INVALID")
	}
	server := NewServer("alpha-nexus-private", "1.0.0", "Private per-run Alpha-Nexus tools. Side effects return an approval request and execute only after the user approves in the Drawer.")
	for _, item := range body.Tools {
		def := item
		name := "alpha_" + strings.ReplaceAll(def.Name, ".", "_")
		properties := map[string]any{}
		required := make([]string, 0)
		for _, param := range def.Params {
			properties[param.Name] = brokerParamSchema(def.Name, param)
			if param.Required {
				required = append(required, param.Name)
			}
		}
		server.AddTool(ToolDef{
			Name:        name,
			Description: fmt.Sprintf("[%s %s] %s", def.AgentWorkspace, def.AgentRisk, def.Description),
			// An MCP call only records a broker proposal. It cannot submit a
			// publish action itself; the broker and the publish nonce enforce
			// the real side effect after local approval.
			SideEffect:  "read",
			InputSchema: schemaObject(properties, required...),
			Handler: func(callCtx context.Context, args map[string]any) (any, error) {
				payload, _ := json.Marshal(map[string]any{"name": def.Name, "args": args})
				callRequest, _ := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint+"/api/agent/broker/call", bytes.NewReader(payload))
				callRequest.Header.Set("Content-Type", "application/json")
				setBrokerHeaders(callRequest, cfg)
				callResponse, err := client.Do(callRequest)
				if err != nil {
					return nil, fmt.Errorf("AGENT_BROKER_UNAVAILABLE")
				}
				defer callResponse.Body.Close()
				var result map[string]any
				if json.NewDecoder(callResponse.Body).Decode(&result) != nil {
					return nil, fmt.Errorf("AGENT_BROKER_RESPONSE_INVALID")
				}
				if callResponse.StatusCode != http.StatusOK && callResponse.StatusCode != http.StatusAccepted {
					return nil, fmt.Errorf("AGENT_BROKER_CALL_REJECTED")
				}
				return result, nil
			},
		})
	}
	return server.ServeStdio(ctx)
}

func brokerParamSchema(toolName string, param tools.Param) map[string]any {
	if toolName == "publish.plan.freeze" {
		switch param.Name {
		case "media_ids":
			return map[string]any{"type": "array", "description": param.Description, "items": map[string]any{"type": "number"}}
		case "targets":
			return map[string]any{"type": "array", "description": param.Description, "items": map[string]any{
				"type": "object", "properties": map[string]any{
					"account_id":    propNumber("local account id"),
					"asset_id":      propNumber("local account asset id"),
					"variant_id":    propNumber("local content variant id"),
					"settings_json": propString("platform settings JSON string"),
				}, "required": []string{"account_id"},
			}}
		}
	}
	switch param.Type {
	case "bool", "boolean":
		return propBool(param.Description)
	case "int", "integer", "number":
		return propNumber(param.Description)
	case "array":
		return propArray(param.Description)
	case "object":
		return propObject(param.Description)
	default:
		return propString(param.Description)
	}
}

func newBrokerClient(cfg BrokerConfig) (*http.Client, string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, "", fmt.Errorf("invalid Agent broker endpoint")
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return nil, "", fmt.Errorf("Agent broker must be loopback")
	}
	if !strings.HasPrefix(cfg.RunID, "run_") || len(cfg.Token) != 64 {
		return nil, "", fmt.Errorf("invalid Agent broker credentials")
	}
	client := &http.Client{
		Timeout: 70 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("Agent broker redirects are forbidden")
		},
	}
	return client, endpoint, nil
}

func setBrokerHeaders(request *http.Request, cfg BrokerConfig) {
	request.Header.Set("Authorization", "Bearer "+cfg.Token)
	request.Header.Set("X-Alpha-Agent-Run", cfg.RunID)
}
