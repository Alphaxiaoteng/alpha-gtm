package tools

import (
	"context"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/datalink"
)

func registerDataLinkTools(registry *Registry) {
	for _, definition := range []Def{
		{
			Name: "data.sql.list", Category: "data", SideEffect: "read",
			Description: "List registered local SQL data sources and recent snapshots without exposing file paths",
			Handler:     handleDataLinkList,
		},
		{
			Name: "data.sql.inspect", Category: "data", SideEffect: "read",
			Description: "List SQLite tables/views, inspect fields, and preview bounded read-only data",
			Params:      []Param{{Name: "source_id", Type: "int", Required: true}, {Name: "object_name", Type: "string"}, {Name: "query", Type: "string"}, {Name: "limit", Type: "int"}},
			Handler:     handleDataLinkInspect,
		},
		{
			Name: "data.sql.preflight", Category: "data", SideEffect: "read",
			Description: "Validate a one-time read-only collection and return its schema fingerprint and limits",
			Params:      []Param{{Name: "source_id", Type: "int", Required: true}, {Name: "object_name", Type: "string"}, {Name: "query", Type: "string"}, {Name: "mapping", Type: "object"}, {Name: "max_rows", Type: "int"}},
			Handler:     handleDataLinkPreflight,
		},
		{
			Name: "data.sql.collect", Category: "data", SideEffect: "write",
			Description: "Run one bounded read-only collection and persist a traceable local snapshot",
			Params:      []Param{{Name: "source_id", Type: "int", Required: true}, {Name: "object_name", Type: "string"}, {Name: "query", Type: "string"}, {Name: "mapping", Type: "object"}, {Name: "max_rows", Type: "int"}, {Name: "max_bytes", Type: "int"}, {Name: "timeout_seconds", Type: "int"}},
			Handler:     handleDataLinkCollect,
		},
		{
			Name: "data.sql.review.confirm", Category: "data", SideEffect: "write",
			Description: "After explicit confirmation, import one completed SQL snapshot review into a selected local knowledge base",
			Params:      []Param{{Name: "snapshot_id", Type: "int", Required: true}, {Name: "base_id", Type: "int", Required: true}, {Name: "confirm", Type: "bool", Required: true}},
			Handler:     handleDataLinkConfirm,
		},
	} {
		registry.Register(definition)
	}
}

func withDataLink(runtime *Runtime, call func(*datalink.Service) (*Result, error)) (*Result, error) {
	database, err := openDB(runtime)
	if err != nil {
		return failResult("DATA_LINK_DATABASE_FAILED", "local data link database is unavailable"), nil
	}
	defer database.Close()
	return call(datalink.NewService(database))
}

func handleDataLinkList(ctx context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	return withDataLink(runtime, func(service *datalink.Service) (*Result, error) {
		sources, snapshots, err := service.List(ctx)
		if err != nil {
			return dataLinkFailure(err), nil
		}
		return okResult("ok", map[string]interface{}{"sources": sources, "snapshots": snapshots}), nil
	})
}

func handleDataLinkInspect(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	return withDataLink(runtime, func(service *datalink.Service) (*Result, error) {
		inspection, err := service.Inspect(ctx, int64(argInt(args, "source_id", 0)), datalink.InspectRequest{
			ObjectName: argString(args, "object_name", ""), Query: argString(args, "query", ""), Limit: argInt(args, "limit", 20),
		})
		if err != nil {
			return dataLinkFailure(err), nil
		}
		return okResult("source inspected", map[string]interface{}{"inspection": inspection}), nil
	})
}

func handleDataLinkPreflight(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	return withDataLink(runtime, func(service *datalink.Service) (*Result, error) {
		preflight, err := service.Preflight(ctx, int64(argInt(args, "source_id", 0)), dataLinkCollectRequest(args))
		if err != nil {
			return dataLinkFailure(err), nil
		}
		return okResult("preflight completed", map[string]interface{}{"preflight": preflight}), nil
	})
}

func handleDataLinkCollect(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	return withDataLink(runtime, func(service *datalink.Service) (*Result, error) {
		snapshot, err := service.Collect(ctx, int64(argInt(args, "source_id", 0)), dataLinkCollectRequest(args))
		if err != nil {
			return dataLinkFailure(err), nil
		}
		return okResult("snapshot collected", map[string]interface{}{"snapshot": snapshot}), nil
	})
}

func handleDataLinkConfirm(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	return withDataLink(runtime, func(service *datalink.Service) (*Result, error) {
		snapshot, err := service.ConfirmReview(ctx, int64(argInt(args, "snapshot_id", 0)), int64(argInt(args, "base_id", 0)), argBool(args, "confirm", false))
		if err != nil {
			return dataLinkFailure(err), nil
		}
		return okResult("review imported into knowledge base", map[string]interface{}{"snapshot": snapshot}), nil
	})
}

func dataLinkCollectRequest(args map[string]interface{}) datalink.CollectRequest {
	return datalink.CollectRequest{
		ObjectName: argString(args, "object_name", ""), Query: argString(args, "query", ""), Mapping: dataLinkMapping(args["mapping"]),
		MaxRows: argInt(args, "max_rows", 10_000), MaxBytes: int64(argInt(args, "max_bytes", 32<<20)), TimeoutSecond: argInt(args, "timeout_seconds", 8),
	}
}

func dataLinkMapping(value interface{}) map[string]string {
	result := map[string]string{}
	if mapping, ok := value.(map[string]interface{}); ok {
		for key, raw := range mapping {
			if text, ok := raw.(string); ok {
				result[key] = strings.TrimSpace(text)
			}
		}
	}
	if mapping, ok := value.(map[string]string); ok {
		for key, text := range mapping {
			result[key] = strings.TrimSpace(text)
		}
	}
	return result
}

func dataLinkFailure(err error) *Result {
	code := "DATA_LINK_FAILED"
	if err != nil {
		candidate := strings.TrimSpace(err.Error())
		if strings.HasPrefix(candidate, "DATA_") || candidate == "CONFIRMATION_REQUIRED" {
			code = candidate
		}
	}
	return failResult(code, fmt.Sprintf("data link operation failed: %s", code))
}
