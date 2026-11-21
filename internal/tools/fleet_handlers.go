package tools

import (
	"context"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/fleet"
)

func fleetAccountIDs(args map[string]interface{}) ([]int64, error) {
	raw, exists := args["account_ids"]
	if !exists || raw == nil {
		return nil, fmt.Errorf("account_ids required")
	}
	values, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("account_ids must be an array")
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		id := int64(argInt(map[string]interface{}{"value": value}, "value", 0))
		if id <= 0 {
			return nil, fmt.Errorf("account_ids must contain positive integers")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func fleetService(rt *Runtime) (*fleet.Service, error) {
	if rt != nil && rt.Fleet != nil {
		return rt.Fleet, nil
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	// CLI callers are short lived and mirror browser.start's detached process
	// model. The UI injects a long-lived service above, retaining its facade.
	return fleet.NewService(database, facadeFrom(rt, database)), nil
}

func fleetResult(runID int64, service *fleet.Service) (*Result, error) {
	detail, err := service.Detail(runID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(detail.Items))
	for _, item := range detail.Items {
		items = append(items, map[string]interface{}{
			"id": item.ID, "account_id": item.AccountID, "status": item.Status,
			"step": item.CurrentStep, "error_code": item.ErrorCode,
		})
	}
	run := detail.Run
	return okResult("fleet run "+run.Status, map[string]interface{}{
		"run_id": run.ID, "operation": strings.TrimPrefix(run.OperationType, "fleet_"), "status": run.Status,
		"concurrency": run.Concurrency, "total": run.TotalItems, "pending": run.PendingCount,
		"running": run.RunningCount, "success": run.SuccessCount, "failed": run.FailedCount,
		"items": items,
	}), nil
}

func handleFleetRun(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if !argBool(args, "confirm", false) {
		return &Result{OK: false, Error: "CONFIRM_REQUIRED"}, fmt.Errorf("fleet.run requires confirm=true")
	}
	ids, err := fleetAccountIDs(args)
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	service, err := fleetService(rt)
	if err != nil {
		return nil, err
	}
	req := fleet.RunRequest{
		Operation:   fleet.Operation(strings.TrimSpace(argString(args, "operation", ""))),
		AccountIDs:  ids,
		Concurrency: argInt(args, "concurrency", 1),
	}
	// The UI owns a long-lived Fleet service, so persist and return its run ID
	// before executing. This lets the observatory show items while they are
	// running. CLI-created services remain synchronous because their process
	// lifetime is not a safe background worker.
	if rt != nil && rt.Fleet != nil {
		run, createErr := service.Create(req)
		if createErr != nil {
			return &Result{OK: false, Error: "FLEET_RUN_FAILED"}, createErr
		}
		go func(runID int64) { _ = service.Execute(context.Background(), runID) }(run.ID)
		return fleetResult(run.ID, service)
	}
	run, err := service.Run(ctx, req)
	if err != nil {
		data := map[string]interface{}{}
		if run != nil {
			data["run_id"] = run.ID
		}
		return &Result{OK: false, Error: "FLEET_RUN_FAILED", Data: data}, err
	}
	return fleetResult(run.ID, service)
}

func handleFleetStatus(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	service, err := fleetService(rt)
	if err != nil {
		return nil, err
	}
	return fleetResult(int64(argInt(args, "run_id", 0)), service)
}
