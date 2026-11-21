package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

func registerEgoTools(registry *Registry) {
	definitions := []Def{
		{
			Name: "ego.doctor", Category: "ego", SideEffect: "read",
			Description: "Verify the reviewed ego-browser helper contract",
			Handler:     handleEgoDoctor,
		},
		{
			Name: "ego.identity.list", Category: "ego", SideEffect: "read",
			Description: "List logical operating identities without browser secrets",
			Handler:     handleEgoIdentityList,
		},
		{
			Name: "ego.matrix.create", Category: "ego", SideEffect: "write",
			Description: "Create a user-visible matrix with an automatic default name",
			Params:      []Param{{Name: "name", Type: "string"}},
			Handler:     handleEgoMatrixCreate,
		},
		{
			Name: "ego.matrix.configure-platforms", Category: "ego", SideEffect: "write",
			Description: "Replace the enabled platform accounts inside one matrix",
			Params: []Param{
				{Name: "identity_id", Type: "int", Required: true},
				{Name: "platform_codes", Type: "array", Required: true},
			},
			Handler: handleEgoMatrixConfigurePlatforms,
		},
		{
			Name: "ego.space.list", Category: "ego", SideEffect: "read",
			Description: "List matrix-owned Ego Spaces and ownership state",
			Handler:     handleEgoSpaceList,
		},
		{
			Name: "ego.space.ensure", Category: "ego", SideEffect: "browser",
			Description: "Create or reuse the stable isolated Space for an account's matrix",
			Params:      []Param{{Name: "account_id", Type: "int", Required: true}},
			Handler:     handleEgoSpaceEnsure,
		},
		{
			Name: "ego.space.open", Category: "ego", SideEffect: "browser",
			Description: "Open selected platform managers in a matrix-bound EgoLite Space without Electron",
			Params: []Param{
				{Name: "identity_id", Type: "int", Required: true},
				{Name: "platform_codes", Type: "array", Description: "Optional configured platform codes; at most three are opened"},
				{Name: "offset", Type: "int", Description: "Optional platform-page offset for the next group"},
			},
			Handler: handleEgoSpaceOpen,
		},
		{
			Name: "ego.space.login", Category: "ego", SideEffect: "browser",
			Description: "Compatibility login handoff for one configured platform account",
			Params:      []Param{{Name: "account_id", Type: "int", Required: true}},
			Handler:     handleEgoSpaceLogin,
		},
		{
			Name: "ego.space.resume", Category: "ego", SideEffect: "browser",
			Description: "Resume a handed-off Space only after explicit user confirmation",
			Params: []Param{
				{Name: "account_id", Type: "int", Required: true},
				{Name: "confirm", Type: "bool", Required: true},
			},
			Handler: handleEgoSpaceResume,
		},
		{
			Name: "ego.bookmarks.import", Category: "ego", SideEffect: "browser",
			Description: "Synchronize Browser 1 logged-in platform bookmarks through its bound EgoLite Space",
			Handler:     handleEgoBookmarksImport,
		},
		{
			Name: "ego.login.batch", Category: "ego", SideEffect: "browser",
			Description: "Open at most three configured creator backends and hand the matrix to the user",
			Params: []Param{
				{Name: "identity_id", Type: "int", Required: true},
				{Name: "platform_codes", Type: "array", Required: true},
			},
			Handler: handleEgoLoginBatch,
		},
		{
			Name: "ego.login.verify", Category: "ego", SideEffect: "browser",
			Description: "After explicit confirmation, read verified profile fields for one login batch",
			Params: []Param{
				{Name: "identity_id", Type: "int", Required: true},
				{Name: "platform_codes", Type: "array", Required: true},
				{Name: "confirm", Type: "bool", Required: true},
			},
			Handler: handleEgoLoginVerify,
		},
		{
			Name: "ego.session.check", Category: "ego", SideEffect: "browser",
			Description: "Run one serial conservative login-state check across existing matrix Spaces",
			Handler:     handleEgoSessionCheck,
		},
	}
	for _, definition := range definitions {
		registry.Register(definition)
	}
}

func handleEgoDoctor(ctx context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	command := "ego-browser"
	if runtime != nil && strings.TrimSpace(runtime.EgoBrowserCommand) != "" {
		command = runtime.EgoBrowserCommand
	}
	doctor, err := egolite.NewRuntime(command, 0).Doctor(ctx)
	if err != nil {
		return failResult("EGO_DOCTOR_FAILED", err.Error()), nil
	}
	return okResult("ego-browser helper contract available", structMap(doctor)), nil
}

func handleEgoIdentityList(_ context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	identities, err := database.ListEgoIdentities()
	if err != nil {
		return failResult("EGO_IDENTITIES_LIST_FAILED", "could not list identities"), nil
	}
	return okResult("ok", map[string]interface{}{"identities": identities, "count": len(identities)}), nil
}

func handleEgoMatrixCreate(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	identity, err := database.CreateEgoIdentity(argString(args, "name", ""))
	if err != nil {
		return failResult("EGO_MATRIX_CREATE_FAILED", "could not create matrix"), nil
	}
	return okResult("matrix created", map[string]interface{}{"identity": structMap(identity)}), nil
}

func handleEgoMatrixConfigurePlatforms(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	identityID := int64(argInt(args, "identity_id", 0))
	codes, err := egoPlatformCodes(args["platform_codes"])
	if identityID <= 0 || err != nil || len(codes) == 0 {
		return failResult("EGO_PLATFORM_CONFIG_INVALID", "identity_id and platform_codes are required"), nil
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	accounts, err := database.ConfigureIdentityPlatforms(identityID, codes)
	if err != nil {
		return failResult("EGO_PLATFORM_CONFIG_FAILED", "could not configure matrix platforms"), nil
	}
	return okResult("matrix platforms configured", map[string]interface{}{"accounts": accounts, "count": len(accounts)}), nil
}

func handleEgoSpaceList(_ context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	spaces, err := database.ListEgoSpaces()
	if err != nil {
		return failResult("EGO_SPACES_LIST_FAILED", "could not list Spaces"), nil
	}
	return okResult("ok", map[string]interface{}{"spaces": spaces, "count": len(spaces)}), nil
}

func handleEgoSpaceEnsure(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	return runEgoSpaceTool(ctx, runtime, args, "ensure")
}

func handleEgoSpaceOpen(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	identityID := int64(argInt(args, "identity_id", 0))
	offset := argInt(args, "offset", 0)
	if identityID <= 0 || offset < 0 {
		return failResult("EGO_BROWSER_OPEN_INPUT_INVALID", "identity_id is required and offset must not be negative"), nil
	}
	var codes []string
	if rawCodes, exists := args["platform_codes"]; exists {
		var err error
		codes, err = egoPlatformCodes(rawCodes)
		if err != nil {
			return failResult("EGO_BROWSER_OPEN_INPUT_INVALID", err.Error()), nil
		}
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	service, failure := egoToolSpaceService(database, runtime)
	if failure != nil {
		return failure, nil
	}
	var (
		result *egolite.BrowserOpenResult
		err    error
	)
	if len(codes) > 0 {
		result, err = service.OpenLoggedInPlatformsForCodes(ctx, identityID, offset, codes)
	} else {
		result, err = service.OpenLoggedInPlatforms(ctx, identityID, offset)
	}
	if err != nil {
		return failResult("EGO_SPACE_OPEN_FAILED", err.Error()), nil
	}
	return okResult("platform managers opened in the bound EgoLite Space", structMap(result)), nil
}

func handleEgoSpaceLogin(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	return runEgoSpaceTool(ctx, runtime, args, "login")
}

func handleEgoSpaceResume(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	if !argBool(args, "confirm", false) {
		return failResult("CONFIRMATION_REQUIRED", "explicit confirm=true is required after user handoff"), nil
	}
	return runEgoSpaceTool(ctx, runtime, args, "resume")
}

func handleEgoBookmarksImport(ctx context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	egoRuntime := egolite.NewRuntime(runtime.EgoBrowserCommand, 60*time.Second)
	egoSpaces := egolite.NewSpaceService(database, egoRuntime)
	result, err := egolite.NewBookmarkService(database, runtime.DataDir, egolite.NewSpaceBookmarkImporter(egoSpaces)).Import(ctx)
	if err != nil {
		return failResult(err.Error(), "Browser 1 bookmark sync failed in its bound EgoLite Space"), nil
	}
	return okResult("bookmark import prepared", map[string]interface{}{
		"status": result.Status, "mode": result.Mode, "version": result.Version,
	}), nil
}

func handleEgoLoginBatch(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	return runEgoMatrixBrowserTool(ctx, runtime, args, "login")
}

func handleEgoLoginVerify(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	if !argBool(args, "confirm", false) {
		return failResult("CONFIRMATION_REQUIRED", "confirm=true is required after the user completes this batch"), nil
	}
	return runEgoMatrixBrowserTool(ctx, runtime, args, "verify")
}

func handleEgoSessionCheck(ctx context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	if runtime != nil && runtime.CheckEgoSessions != nil {
		return runtime.CheckEgoSessions(ctx)
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	service, failure := egoToolSpaceService(database, runtime)
	if failure != nil {
		return failure, nil
	}
	result, err := egolite.NewSessionMonitor(database, service, nil).Run(ctx)
	if err != nil {
		return failResult("SESSION_MONITOR_RUN_FAILED", "could not complete session check"), nil
	}
	return okResult("session check completed", structMap(result)), nil
}

func runEgoMatrixBrowserTool(ctx context.Context, runtime *Runtime, args map[string]interface{}, action string) (*Result, error) {
	identityID := int64(argInt(args, "identity_id", 0))
	codes, err := egoPlatformCodes(args["platform_codes"])
	if identityID <= 0 || err != nil || len(codes) == 0 {
		return failResult("EGO_LOGIN_INPUT_INVALID", "identity_id and platform_codes are required"), nil
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	service, failure := egoToolSpaceService(database, runtime)
	if failure != nil {
		return failure, nil
	}
	var value any
	switch action {
	case "login":
		value, err = service.OpenLoginBatch(ctx, identityID, codes)
	case "verify":
		value, err = service.VerifyLoginBatch(ctx, identityID, codes)
	default:
		err = fmt.Errorf("EGO_ACTION_INVALID")
	}
	if err != nil {
		return failResult("EGO_LOGIN_ACTION_FAILED", err.Error()), nil
	}
	return okResult("ok", map[string]interface{}{"result": value}), nil
}

func runEgoSpaceTool(ctx context.Context, runtime *Runtime, args map[string]interface{}, action string) (*Result, error) {
	accountID := int64(argInt(args, "account_id", 0))
	if accountID <= 0 {
		return failResult("INVALID_ACCOUNT_ID", "account_id is required"), nil
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	service, failure := egoToolSpaceService(database, runtime)
	if failure != nil {
		return failure, nil
	}
	var value any
	var err error
	switch action {
	case "ensure":
		value, err = service.EnsureAccountSpace(ctx, accountID)
	case "login":
		value, err = service.OpenLoginAndHandoff(ctx, accountID)
	case "resume":
		value, err = service.ResumeAfterConfirmation(ctx, accountID)
	default:
		err = fmt.Errorf("EGO_ACTION_INVALID")
	}
	if err != nil {
		return failResult("EGO_SPACE_ACTION_FAILED", err.Error()), nil
	}
	return okResult("ok", map[string]interface{}{"space": value}), nil
}

func egoToolSpaceService(database *db.DB, runtime *Runtime) (*egolite.SpaceService, *Result) {
	runner := egolite.ProgramRunner(nil)
	if runtime != nil {
		runner = runtime.EgoRunner
		if runner == nil {
			runner = egolite.NewRuntime(runtime.EgoBrowserCommand, 0)
		}
	}
	if runner == nil {
		return nil, failResult("EGO_RUNTIME_UNAVAILABLE", "ego runtime unavailable")
	}
	return egolite.NewSpaceService(database, runner), nil
}

func egoPlatformCodes(value interface{}) ([]string, error) {
	items, ok := value.([]interface{})
	if !ok {
		if codes, ok := value.([]string); ok {
			return codes, nil
		}
		return nil, fmt.Errorf("platform_codes must be an array")
	}
	codes := make([]string, 0, len(items))
	for _, item := range items {
		code, ok := item.(string)
		if !ok || strings.TrimSpace(code) == "" {
			return nil, fmt.Errorf("platform_codes must contain strings")
		}
		codes = append(codes, strings.TrimSpace(code))
	}
	return codes, nil
}

func openEgoToolDB(runtime *Runtime) (*db.DB, func(), *Result) {
	if runtime == nil {
		return nil, func() {}, failResult("DATA_DIR_REQUIRED", "data directory is required")
	}
	if runtime.Database != nil {
		return runtime.Database, func() {}, nil
	}
	if strings.TrimSpace(runtime.DataDir) == "" {
		return nil, func() {}, failResult("DATA_DIR_REQUIRED", "data directory is required")
	}
	database, err := db.Open(runtime.DataDir)
	if err != nil {
		return nil, func() {}, failResult("DATABASE_OPEN_FAILED", "could not open database")
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		return nil, func() {}, failResult("DATABASE_MIGRATION_FAILED", "could not migrate database")
	}
	return database, func() { _ = database.Close() }, nil
}

func structMap(value any) map[string]interface{} {
	data, _ := json.Marshal(value)
	result := map[string]interface{}{}
	_ = json.Unmarshal(data, &result)
	return result
}

func failResult(code, message string) *Result {
	return &Result{OK: false, Error: code, Message: message}
}
