package tools

import (
	"context"

	"alpha-nexus-egolite/internal/openbot"
)

// registerOpenBotTools surfaces local OpenBot installs. Discovery only reads
// ports and well-known paths, so the Bot page can offer a one-click entry
// without asking anyone to configure an endpoint first.
func registerOpenBotTools(r *Registry) {
	r.Register(Def{
		Name: "agent.openbot.discover", Category: "agent", SideEffect: "read",
		Description: "Detect local OpenBot installs (CopilotKit workspace or local agent server) with no configuration",
		Handler:     runOpenBotDiscover,
	})
}

func runOpenBotDiscover(ctx context.Context, _ *Runtime, _ map[string]interface{}) (*Result, error) {
	instances := openbot.Discover(ctx)
	return okResult("local OpenBot discovery", map[string]interface{}{
		"instances": instances,
		"count":     len(instances),
	}), nil
}
