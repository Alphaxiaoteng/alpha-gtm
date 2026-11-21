package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/fleet"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/tools"
)

func registerRegistryTools(s *Server, h *handlers) {
	s.AddTool(ToolDef{
		Name:        "tools_list",
		Description: "列出 tools.Registry 全部工具（点分名：account.list / browser.status / …）",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{"category": propString("可选分类")}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			cat := argString(args, "category", "")
			defs := tools.Default.ListByCategory(cat)
			rows := make([]map[string]any, 0, len(defs))
			for _, d := range defs {
				if !isMCPRegistryReadTool(d) {
					continue
				}
				rows = append(rows, map[string]any{
					"name": d.Name, "category": d.Category, "description": d.Description,
					"side_effect": "read",
				})
			}
			return map[string]any{"count": len(rows), "tools": rows}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "tools_call",
		Description: "调用 tools.Registry 的只读工具。write/browser/network 工具固定拒绝。",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{
			"name": propString("只读工具名，如 account.list"),
			"args": propObject("参数 JSON 对象"),
		}, "name"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			name := argString(args, "name", "")
			if name == "" {
				return nil, fmt.Errorf("name required")
			}
			callArgs := map[string]any{}
			if raw, ok := args["args"].(map[string]any); ok {
				callArgs = raw
			}
			return h.callTool(ctx, name, callArgs)
		},
	})

	// full registry projection: registry_account_list etc.
	for _, d := range tools.Default.List() {
		if !isMCPRegistryReadTool(d) {
			continue
		}
		def := d
		mcpName := "registry_" + strings.ReplaceAll(def.Name, ".", "_")
		props := map[string]any{}
		var required []string
		for _, p := range def.Params {
			switch p.Type {
			case "bool", "boolean":
				props[p.Name] = propBool(p.Description)
			case "int", "integer", "number":
				props[p.Name] = propNumber(p.Description)
			case "array":
				props[p.Name] = propArray(p.Description)
			case "object":
				props[p.Name] = propObject(p.Description)
			default:
				props[p.Name] = propString(p.Description)
			}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		toolName := def.Name
		side := def.SideEffect
		s.AddTool(ToolDef{
			Name:        mcpName,
			Description: fmt.Sprintf("[registry] %s — %s (side_effect=%s)", def.Name, def.Description, def.SideEffect),
			SideEffect:  "read",
			InputSchema: schemaObject(props, required...),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				if err := guardMCPSideEffect(toolName, side, args); err != nil {
					return nil, err
				}
				return h.callTool(ctx, toolName, args)
			},
		})
	}
}

func (h *handlers) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	args = withoutMCPConfirmation(args)
	if d, ok := tools.Default.Get(name); ok {
		if err := guardMCPSideEffect(name, d.SideEffect, args); err != nil {
			return nil, err
		}
	}
	if name == "tools.list" {
		return readOnlyRegistryList(args), nil
	}
	if name == "account.list" {
		return h.readOnlyAccountList(args)
	}
	if name == "ego.space.list" {
		return h.readOnlyEgoSpaceList()
	}
	if name == "fleet.status" {
		return h.readOnlyFleetStatus(args)
	}
	if name == "studio.media.list" {
		return h.readOnlyStudioMediaList(args)
	}
	if name == "studio.script.list" {
		return h.readOnlyStudioScriptList(args)
	}
	if name == "tikhub.config.status" {
		return h.readOnlyTikHubConfigStatus()
	}
	if name == "tikhub.user.list" {
		return h.readOnlyTikHubUserList()
	}
	return nil, errors.New("MCP_READ_TOOL_UNAVAILABLE")
}

func (h *handlers) readOnlyEgoSpaceList() (any, error) {
	spaces, err := h.db.ListEgoSpaces()
	if err != nil {
		return nil, err
	}
	return &tools.Result{
		OK: true, Tool: "ego.space.list", Message: "EgoLite Spaces listed",
		Data: map[string]any{"count": len(spaces), "spaces": spaces},
	}, nil
}

func (h *handlers) readOnlyFleetStatus(args map[string]any) (any, error) {
	runID, ok := argInt64(args, "run_id")
	if !ok || runID <= 0 {
		return nil, fmt.Errorf("invalid fleet run id")
	}
	detail, err := fleet.NewService(h.db, nil).Detail(runID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(detail.Items))
	for _, item := range detail.Items {
		items = append(items, map[string]any{
			"id": item.ID, "account_id": item.AccountID, "status": item.Status,
			"step": item.CurrentStep, "error_code": item.ErrorCode,
		})
	}
	run := detail.Run
	return &tools.Result{
		OK: true, Tool: "fleet.status", Message: "fleet run " + run.Status,
		Data: map[string]any{
			"run_id": run.ID, "operation": strings.TrimPrefix(run.OperationType, "fleet_"), "status": run.Status,
			"concurrency": run.Concurrency, "total": run.TotalItems, "pending": run.PendingCount,
			"running": run.RunningCount, "success": run.SuccessCount, "failed": run.FailedCount,
			"items": items,
		},
	}, nil
}

func (h *handlers) readOnlyStudioMediaList(args map[string]any) (any, error) {
	limit := int64(200)
	if value, ok := argInt64(args, "limit"); ok {
		limit = value
	}
	items, err := h.db.ListMediaLibrary(int(limit))
	if err != nil {
		return nil, err
	}
	return &tools.Result{
		OK: true, Tool: "studio.media.list", Message: "local media indexes listed",
		Data: map[string]any{"count": len(items), "items": items},
	}, nil
}

func (h *handlers) readOnlyStudioScriptList(args map[string]any) (any, error) {
	limit := int64(200)
	if value, ok := argInt64(args, "limit"); ok {
		limit = value
	}
	scripts, err := h.db.ListStudioScripts(int(limit))
	if err != nil {
		return nil, err
	}
	return &tools.Result{
		OK: true, Tool: "studio.script.list", Message: "local scripts listed",
		Data: map[string]any{"count": len(scripts), "scripts": scripts},
	}, nil
}

func (h *handlers) readOnlyTikHubConfigStatus() (any, error) {
	info, err := os.Lstat(filepath.Join(h.dataDir, "tikhub_api_key"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	configured := err == nil && info.Mode().IsRegular() && info.Size() > 0
	return &tools.Result{
		OK: true, Tool: "tikhub.config.status", Message: "TikHub local key-file status",
		Data: map[string]any{"configured": configured},
	}, nil
}

func (h *handlers) readOnlyTikHubUserList() (any, error) {
	users, err := tikhub.LoadWatchlist(h.dataDir)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(users))
	for _, user := range users {
		rows = append(rows, map[string]any{
			"platform": user.Platform, "user_id": user.UserID, "name": user.Name,
			"last_check": user.LastCheck, "last_count": user.LastCount, "note": user.Note,
		})
	}
	return &tools.Result{
		OK: true, Tool: "tikhub.user.list", Message: fmt.Sprintf("%d watched", len(rows)),
		Data: map[string]any{"users": rows, "count": len(rows)},
	}, nil
}

func (h *handlers) readOnlyAccountList(args map[string]any) (any, error) {
	var (
		accounts []db.Account
		err      error
	)
	switch {
	case argString(args, "platform", "") != "":
		accounts, err = h.db.ListAccountsByPlatform(argString(args, "platform", ""))
	case argString(args, "group", "") != "":
		accounts, err = h.db.ListAccountsByGroup(argString(args, "group", ""))
	default:
		accounts, err = h.db.ListAccounts()
	}
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, map[string]any{
			"id": account.ID, "platform": account.PlatformCode, "name": account.Name,
			"login_state": account.LoginState, "group": account.AccountGroup, "remark": account.Remark,
		})
	}
	return &tools.Result{
		OK: true, Tool: "account.list", Message: fmt.Sprintf("%d accounts", len(rows)),
		Data: map[string]any{"count": len(rows), "accounts": rows},
	}, nil
}

func readOnlyRegistryList(args map[string]any) *tools.Result {
	category := argString(args, "category", "")
	defs := tools.Default.ListByCategory(category)
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		if isMCPRegistryReadTool(def) {
			names = append(names, def.Name)
		}
	}
	return &tools.Result{
		OK: true, Tool: "tools.list", Message: "read-only Registry tools",
		Data: map[string]any{"count": len(names), "tools": names, "category": category},
	}
}

func withoutMCPConfirmation(args map[string]any) map[string]any {
	clean := make(map[string]any, len(args))
	for key, value := range args {
		if strings.EqualFold(strings.TrimSpace(key), "confirm") {
			continue
		}
		clean[key] = value
	}
	return clean
}

// readOnlyBrowserStatus reads the durable SQLite snapshot directly. It must
// not construct BrowserFacade/Supervisor: Supervisor startup reconciles stale
// rows and writes runtime.json, which would violate MCP's read-only contract.
func (h *handlers) readOnlyBrowserStatus(ctx context.Context, args map[string]any) (any, error) {
	if accountID, ok := argInt64(args, "account_id"); ok && accountID > 0 {
		account, err := h.db.GetAccount(accountID)
		if err != nil {
			return nil, err
		}
		state := "stopped"
		err = h.db.QueryRowContext(ctx,
			`SELECT state FROM browser_runtime_instances WHERE account_id=? ORDER BY updated_at DESC LIMIT 1`,
			accountID,
		).Scan(&state)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		state, running := mcpRuntimeState(state, err == nil)
		return &tools.Result{
			OK: true, Tool: "browser.status", Message: "read-only browser status snapshot",
			Data: map[string]any{
				"account_id": account.ID, "platform": account.PlatformCode, "name": account.Name,
				"login_state": account.LoginState, "running": running, "state": state,
			},
		}, nil
	}

	runtimeStates := make(map[int64]string)
	rows, err := h.db.QueryContext(ctx, `SELECT account_id, state FROM browser_runtime_instances WHERE account_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var accountID sql.NullInt64
		var state string
		if err := rows.Scan(&accountID, &state); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if accountID.Valid && accountID.Int64 > 0 {
			runtimeStates[accountID.Int64] = state
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	accounts, err := h.db.ListAccounts()
	if err != nil {
		return nil, err
	}
	statuses := make([]map[string]any, 0, len(accounts))
	runningAccounts := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		rawState, exists := runtimeStates[account.ID]
		state, running := mcpRuntimeState(rawState, exists)
		row := map[string]any{
			"account_id": account.ID, "platform": account.PlatformCode, "name": account.Name,
			"login_state": account.LoginState, "running": running, "state": state,
		}
		statuses = append(statuses, row)
		if running {
			runningAccounts = append(runningAccounts, row)
		}
	}
	return &tools.Result{
		OK: true, Tool: "browser.status", Message: fmt.Sprintf("%d recorded running accounts", len(runningAccounts)),
		Data: map[string]any{"running": runningAccounts, "accounts": statuses},
	}, nil
}

func mcpRuntimeState(raw string, exists bool) (string, bool) {
	if !exists {
		return "stopped", false
	}
	state := strings.ToLower(strings.TrimSpace(raw))
	switch state {
	case "starting", "ready", "busy", "degraded", "stopping":
		return state, true
	case "stopped", "crashed":
		return state, false
	default:
		return "unknown", false
	}
}

const mcpSideEffectDenied = "MCP_SIDE_EFFECT_DENIED"

func isMCPReadSideEffect(sideEffect string) bool {
	return strings.EqualFold(strings.TrimSpace(sideEffect), "read")
}

func isMCPRegistryReadTool(def tools.Def) bool {
	if !isMCPReadSideEffect(def.SideEffect) {
		return false
	}
	switch def.Name {
	case "tools.list", "ego.space.list", "fleet.status", "account.list",
		"studio.media.list", "studio.script.list", "tikhub.config.status", "tikhub.user.list":
		return true
	default:
		return false
	}
}

// guardMCPSideEffect is intentionally independent of model arguments and
// process environment. Approval belongs to the trusted local UI, not MCP.
func guardMCPSideEffect(name, sideEffect string, args map[string]any) error {
	if isMCPReadSideEffect(sideEffect) {
		return nil
	}
	return errors.New(mcpSideEffectDenied)
}
