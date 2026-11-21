package tools

import (
	"context"
	"fmt"

	"alpha-nexus-egolite/internal/db"
)

// registerBotTools exposes the persona roster to Agent and CLI runtimes. It is
// read-only on purpose: mutating a Bot stays a reviewed UI action.
func registerBotTools(r *Registry) {
	r.Register(Def{
		Name: "agent.bot.list", Category: "agent", SideEffect: "read",
		Description: "List Agent Hub personas (id, name, role, model, avatar)",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-50; default 50"}},
		Handler:     runAgentBotList,
	})
}

func runAgentBotList(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	const tool = "agent.bot.list"
	// One-shot CLI runtimes leave Database nil; open the data dir on demand the
	// same way other registry tools do.
	database := rt.Database
	if database == nil {
		opened, err := db.Open(dataDir(rt))
		if err != nil {
			return &Result{OK: false, Tool: tool, Error: "NO_DATABASE", Message: "runtime database unavailable"}, fmt.Errorf("NO_DATABASE")
		}
		defer func() { _ = opened.Close() }()
		database = opened
	}
	limit := argInt(args, "limit", 50)
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := database.QueryContext(ctx,
		`SELECT id, name, role, model, avatar FROM agent_bots ORDER BY created_at DESC, id LIMIT ?`, limit)
	if err != nil {
		return &Result{OK: false, Tool: tool, Error: "BOT_LIST_FAILED", Message: "list agent bots failed"}, err
	}
	defer rows.Close()

	bots := make([]map[string]interface{}, 0, limit)
	for rows.Next() {
		var id, name, role, model, avatar string
		if err := rows.Scan(&id, &name, &role, &model, &avatar); err != nil {
			return &Result{OK: false, Tool: tool, Error: "BOT_LIST_FAILED", Message: "scan agent bot failed"}, err
		}
		bots = append(bots, map[string]interface{}{
			"id": id, "name": name, "role": role, "model": model, "avatar": avatar,
		})
	}
	if err := rows.Err(); err != nil {
		return &Result{OK: false, Tool: tool, Error: "BOT_LIST_FAILED", Message: "read agent bots failed"}, err
	}
	return okResult(fmt.Sprintf("%d persona(s)", len(bots)), map[string]interface{}{
		"bots":  bots,
		"count": len(bots),
	}), nil
}
