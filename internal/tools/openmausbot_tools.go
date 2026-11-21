package tools

import (
	"context"
	"os"
	"strings"

	"alpha-nexus-egolite/internal/openmausbot"
)

// registerOpenMausBotTools exposes a read-only view of a locally running
// OpenMausBot harness. Every write on the harness needs a pairing code, so this
// tool deliberately lists only: a caller reads the roster, never changes it.
func registerOpenMausBotTools(r *Registry) {
	r.Register(Def{
		Name: "openmausbot.bot.list", Category: "agent", SideEffect: "read",
		Description: "List bots of a local OpenMausBot harness (read-only; no pairing, no writes)",
		Params: []Param{
			{Name: "base_url", Type: "string", Description: "Harness origin; default http://127.0.0.1:8799"},
			{Name: "limit", Type: "int", Description: "1-200; default 200"},
		},
		// AgentVisible stays false: the roster carries other people's bot names,
		// working folders and persona hashes, and nothing has reviewed that for
		// the private Agent broker yet. An explicit SetAgentMeta is the place to
		// widen it — not this registration.
		Handler: runOpenMausBotBotList,
	})
}

func runOpenMausBotBotList(ctx context.Context, _ *Runtime, args map[string]interface{}) (*Result, error) {
	const tool = "openmausbot.bot.list"
	// The bearer token stays in the environment, never in tool args: args are
	// recorded in traces and event streams.
	client := openmausbot.Client{
		BaseURL:      argString(args, "base_url", ""),
		SessionToken: strings.TrimSpace(os.Getenv("OPENMAUSBOT_TOKEN")),
	}
	bots, err := client.ListBots(ctx)
	if err != nil {
		return &Result{
			OK:      false,
			Tool:    tool,
			Error:   openmausbot.CodeOf(err, openmausbot.CodeUnreachable),
			Message: err.Error(),
		}, err
	}
	limit := argInt(args, "limit", 200)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	total := len(bots)
	if len(bots) > limit {
		bots = bots[:limit]
	}
	return okResult("openmausbot roster", map[string]interface{}{
		"bots":     bots,
		"count":    len(bots),
		"total":    total,
		"base_url": client.Endpoint(),
	}), nil
}
