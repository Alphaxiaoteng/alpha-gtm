package tools

import (
	"context"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/contentstage"
)

// The stage pipeline is exposed read-mostly: these tools answer questions about
// a schedule, they never advance anything on their own. Advancing content stays
// an explicit operator action in the interface.
func registerContentStageTools(r *Registry) {
	r.Register(Def{
		Name: "content.stage.pipeline", Category: "content", SideEffect: "read",
		Description: "List the editorial stages in order, with which ones accept a planned date",
		Handler: func(_ context.Context, _ *Runtime, _ map[string]interface{}) (*Result, error) {
			type stageInfo struct {
				Stage       string `json:"stage"`
				Label       string `json:"label"`
				Rank        int    `json:"rank"`
				Schedulable bool   `json:"schedulable"`
			}
			stages := make([]stageInfo, 0, len(contentstage.Pipeline))
			for index, stage := range contentstage.Pipeline {
				stages = append(stages, stageInfo{
					Stage: string(stage), Label: contentstage.Labels[stage],
					Rank: index, Schedulable: contentstage.Schedulable(stage),
				})
			}
			return okResult("ok", map[string]interface{}{"stages": stages}), nil
		},
	})

	r.Register(Def{
		Name: "content.stage.check", Category: "content", SideEffect: "read",
		Description: "Report whether a planned schedule is internally consistent (no stage planned before an earlier one)",
		Params: []Param{
			{Name: "events", Type: "array", Required: true, Description: "Each item: {stage, planned_date, completed}"},
		},
		Handler: handleContentStageCheck,
	})
}

func handleContentStageCheck(_ context.Context, _ *Runtime, args map[string]interface{}) (*Result, error) {
	raw, ok := args["events"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("events must be an array")
	}
	events := make([]contentstage.Event, 0, len(raw))
	for index, item := range raw {
		entry, isMap := item.(map[string]interface{})
		if !isMap {
			return nil, fmt.Errorf("events[%d] must be an object", index)
		}
		stage := contentstage.Stage(argString(entry, "stage", ""))
		planned := argString(entry, "planned_date", "")
		completed := false
		if value, ok := entry["completed"].(bool); ok {
			completed = value
		}
		events = append(events, contentstage.Event{Stage: stage, PlannedDate: planned, Completed: completed})
	}

	violations, err := contentstage.CheckOrder(events)
	if err != nil {
		return nil, err
	}
	messages := make([]string, 0, len(violations))
	for _, violation := range violations {
		messages = append(messages, strings.TrimSpace(violation.Error()))
	}
	status := "ok"
	if len(violations) > 0 {
		status = "inconsistent"
	}
	return okResult(status, map[string]interface{}{
		"violations": messages, "count": len(violations),
	}), nil
}
