package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/publishplan"
)

func registerStudioScheduleHandoffTools(registry *Registry) {
	registry.Register(Def{
		Name: "studio.schedule-handoff.create", Category: "studio", SideEffect: "write",
		Description: "Persist one reviewed Studio content handoff with an IANA wall-clock time; never starts a local scheduler",
		Params:      []Param{{Name: "workflow_id", Type: "string", Required: true}, {Name: "content_id", Type: "int", Required: true}, {Name: "script_id", Type: "int"}, {Name: "original_local_time", Type: "string", Required: true, Description: "YYYY-MM-DDTHH:MM"}, {Name: "timezone", Type: "string", Required: true, Description: "IANA timezone"}},
		Handler:     handleStudioScheduleHandoffCreate,
	})
	registry.Register(Def{
		Name: "studio.schedule-handoff.get", Category: "studio", SideEffect: "read",
		Description: "Read one Studio schedule handoff with its effective expired or superseded status; never writes",
		Params:      []Param{{Name: "handoff_id", Type: "int", Required: true}}, Handler: handleStudioScheduleHandoffGet,
	})
	registry.Register(Def{
		Name: "studio.schedule-handoff.list", Category: "studio", SideEffect: "read",
		Description: "List local Studio schedule handoffs without executing publication",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-200; default 50"}}, Handler: handleStudioScheduleHandoffList,
	})
	registry.Register(Def{
		Name: "studio.schedule-handoff.cancel", Category: "studio", SideEffect: "write",
		Description: "Cancel one ready Studio schedule handoff; frozen snapshots stay immutable",
		Params:      []Param{{Name: "handoff_id", Type: "int", Required: true}}, Handler: handleStudioScheduleHandoffCancel,
	})
	registry.Register(Def{
		Name: "publish.plan.freeze-handoff", Category: "publish", SideEffect: "write",
		Description: "Freeze one strict-serial native-platform publish plan from a Studio handoff; caller supplies targets only",
		Params:      []Param{{Name: "handoff_id", Type: "int", Required: true}, {Name: "targets", Type: "array", Required: true}, {Name: "compliance_policy", Type: "object"}, {Name: "concurrency", Type: "int"}},
		Handler:     handlePublishPlanFreezeHandoff,
	})
}

func decodeStudioScheduleHandoffInput(args map[string]interface{}) (db.StudioScheduleHandoffInput, error) {
	return decodeStudioScheduleJSON[db.StudioScheduleHandoffInput](args)
}

type studioScheduleHandoffIDInput struct {
	HandoffID int64 `json:"handoff_id"`
}

type studioScheduleHandoffListInput struct {
	Limit int `json:"limit"`
}

func decodeStudioScheduleJSON[T any](args map[string]interface{}) (T, error) {
	var input T
	encoded, err := json.Marshal(args)
	if err != nil {
		return input, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	return input, nil
}

func openStudioScheduleToolDB(rt *Runtime) (*db.DB, *Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, &Result{OK: false, Error: "STUDIO_SCHEDULE_DATABASE_UNAVAILABLE"}, err
	}
	return database, nil, nil
}

func studioScheduleToolError(err error) string {
	if err == nil {
		return ""
	}
	for _, code := range []string{
		egopublish.ScheduleTimezoneInvalid, egopublish.ScheduleDSTAmbiguous, egopublish.ScheduleTimeInvalid, egopublish.ScheduleWorkflowNotReview, egopublish.ScheduleHandoffSuperseded,
		egopublish.ScheduleAccountSessionUnverified, egopublish.ScheduleReadbackMismatch, egopublish.ScheduleResultUnknown, "EGO_PUBLISH_SCHEDULE_UNVERIFIED",
	} {
		if strings.Contains(err.Error(), code) {
			return code
		}
	}
	return "VALIDATION_FAILED"
}

func handleStudioScheduleHandoffCreate(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeStudioScheduleHandoffInput(args)
	if err != nil {
		return failResult("VALIDATION_FAILED", "Studio 排期交接输入无效"), err
	}
	database, failure, err := openStudioScheduleToolDB(rt)
	if failure != nil {
		return failure, err
	}
	defer database.Close()
	handoff, created, err := database.CreateStudioScheduleHandoff(ctx, input)
	if err != nil {
		return failResult(studioScheduleToolError(err), "Studio 排期交接未保存"), err
	}
	return okResult("Studio 排期交接已保存", map[string]interface{}{"handoff": handoff, "created": created, "replayed": !created}), nil
}

func handleStudioScheduleHandoffGet(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeStudioScheduleJSON[studioScheduleHandoffIDInput](args)
	if err != nil || input.HandoffID <= 0 {
		return failResult("VALIDATION_FAILED", "handoff_id 必须为正数"), fmt.Errorf("handoff_id required")
	}
	database, failure, err := openStudioScheduleToolDB(rt)
	if failure != nil {
		return failure, err
	}
	defer database.Close()
	handoff, err := database.GetStudioScheduleHandoff(ctx, input.HandoffID)
	if err != nil {
		return failResult(studioScheduleToolError(err), "Studio 排期交接不可用"), err
	}
	return okResult("Studio 排期交接", map[string]interface{}{"handoff": handoff}), nil
}

func handleStudioScheduleHandoffList(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeStudioScheduleJSON[studioScheduleHandoffListInput](args)
	if err != nil || input.Limit < 0 || input.Limit > 200 {
		return failResult("VALIDATION_FAILED", "limit 必须为 1-200"), fmt.Errorf("invalid handoff list input")
	}
	database, failure, err := openStudioScheduleToolDB(rt)
	if failure != nil {
		return failure, err
	}
	defer database.Close()
	handoffs, err := database.ListStudioScheduleHandoffs(ctx, input.Limit)
	if err != nil {
		return failResult("VALIDATION_FAILED", "Studio 排期交接列表不可用"), err
	}
	return okResult("Studio 排期交接列表", map[string]interface{}{"count": len(handoffs), "handoffs": handoffs}), nil
}

func handleStudioScheduleHandoffCancel(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeStudioScheduleJSON[studioScheduleHandoffIDInput](args)
	if err != nil || input.HandoffID <= 0 {
		return failResult("VALIDATION_FAILED", "handoff_id 必须为正数"), fmt.Errorf("handoff_id required")
	}
	database, failure, err := openStudioScheduleToolDB(rt)
	if failure != nil {
		return failure, err
	}
	defer database.Close()
	handoff, err := database.CancelStudioScheduleHandoff(ctx, input.HandoffID)
	if err != nil {
		return failResult(studioScheduleToolError(err), "Studio 排期交接未取消"), err
	}
	return okResult("Studio 排期交接已取消", map[string]interface{}{"handoff": handoff}), nil
}

func handlePublishPlanFreezeHandoff(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodeStudioScheduleJSON[publishplan.FreezeHandoffInput](args)
	if err != nil || input.HandoffID <= 0 || len(input.Targets) == 0 {
		return failResult("VALIDATION_FAILED", "handoff_id 和 targets 必须提供，内容与时间只能来自 handoff"), fmt.Errorf("freeze handoff input invalid")
	}
	database, failure, err := openStudioScheduleToolDB(rt)
	if failure != nil {
		return failure, err
	}
	defer database.Close()
	plan, err := (&publishplan.Service{DB: database}).FreezeHandoff(input)
	if err != nil {
		return failResult(studioScheduleToolError(err), "Studio 排期交接未冻结"), err
	}
	return okResult("Studio 排期交接已冻结", map[string]interface{}{"plan": plan}), nil
}
