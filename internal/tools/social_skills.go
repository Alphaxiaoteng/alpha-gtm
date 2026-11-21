package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/platforms"
)

// SocialSkill is a small, declared workflow surface for local Agents. The
// Registry remains the executor; this catalog deliberately has no scripts or
// direct CDP access.
type SocialSkill struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	RuntimeRef         string   `json:"runtime_ref,omitempty"`
	AllowedTools       []string `json:"allowed_tools"`
	RequiresLocalUI    bool     `json:"requires_local_ui"`
	FinalPublishAction bool     `json:"final_publish_action"`
}

var socialSkills = []SocialSkill{
	{
		ID: "social.account-check", Name: "账号登录检查",
		Description:  "对明确选择的、已运行账号做被动登录检查；不导航、不读取 Cookie。",
		AllowedTools: []string{"social.account-check"},
	},
	{
		ID: "social.materials-list", Name: "本地素材清单",
		Description:  "读取本地视频、图片和文章脚本索引；不读取未入库路径。",
		AllowedTools: []string{"studio.media.list", "studio.script.list"},
	},
	{
		ID: "social.metrics-collect", Name: "创作者后台数据采集",
		Description:  "串行执行明确选择账号的数据工作流：已适配平台写入近 7 天快照，其余进入人工数据接管。",
		AllowedTools: []string{"social.metrics.collect-batch", "social.metrics.overview"},
	},
	{
		ID: "one-click-publish", Name: "一键发布（计划与预填）",
		Description:        "统一预检能力、冻结发布计划、读取状态或经独立授权预填；不包含最终发布。",
		AllowedTools:       []string{"social.publish-capabilities", "publish.plan.freeze", "publish.plan.get", "publish.plan.list", "publish.plan.prepare"},
		RequiresLocalUI:    true,
		FinalPublishAction: false,
	},
	{
		ID: "social.publish-prepare", Name: "多平台发布预填",
		Description:        "按已冻结方案填充账号后台，停在最终发布按钮之前。",
		RuntimeRef:         "social.publish-prepare",
		AllowedTools:       []string{"social.publish-prepare"},
		RequiresLocalUI:    true,
		FinalPublishAction: false,
	},
	{
		ID: "video-publisher", Name: "Video Publisher",
		Description:        "参考四平台草稿与 READY 顺序准备视频发布，READY 只表示预填完成。",
		RuntimeRef:         "social.publish-prepare",
		AllowedTools:       []string{"social.publish-prepare"},
		RequiresLocalUI:    true,
		FinalPublishAction: false,
	},
	{
		ID: "multipost", Name: "MultiPost",
		Description:        "参考平台映射与内容字段适配准备发布，不接入浏览器扩展运行时。",
		RuntimeRef:         "social.publish-prepare",
		AllowedTools:       []string{"social.publish-prepare"},
		RequiresLocalUI:    true,
		FinalPublishAction: false,
	},
	{
		ID: "social.publish-submit-approved", Name: "已批准的最终发布",
		Description:        "仅使用本机 UI 生成的独立单次授权，对已冻结且已准备的目标各点击一次最终发布。",
		AllowedTools:       []string{"social.publish-submit-approved"},
		RequiresLocalUI:    true,
		FinalPublishAction: true,
	},
}

func registerSocialSkills(r *Registry) {
	r.Register(Def{
		Name: "skill.list", Category: "skill", SideEffect: "read",
		Description: "List reviewed Alpha-Nexus social automation skills",
		Handler: func(ctx context.Context, rt *Runtime, _ map[string]interface{}) (*Result, error) {
			if rt != nil && rt.Skills != nil {
				items, err := rt.Skills.List(ctx)
				if err != nil {
					return &Result{OK: false, Error: "SKILL_LIST_FAILED"}, err
				}
				return okResult("local skill library", map[string]interface{}{"count": len(items), "skills": items}), nil
			}
			items := append([]SocialSkill(nil), socialSkills...)
			sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
			return okResult("reviewed social skills", map[string]interface{}{"count": len(items), "skills": items}), nil
		},
	})
	r.Register(Def{
		Name: "skill.get", Category: "skill", SideEffect: "read",
		Description: "Read one reviewed Alpha-Nexus social automation skill",
		Params:      []Param{{Name: "skill_id", Type: "string", Required: true}},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			if rt != nil && rt.Skills != nil {
				skill, err := rt.Skills.Get(ctx, argString(args, "skill_id", ""))
				if err != nil {
					return &Result{OK: false, Error: "SKILL_NOT_FOUND"}, fmt.Errorf("SKILL_NOT_FOUND")
				}
				return okResult("local skill", map[string]interface{}{"skill_id": skill.ID, "skill": skill}), nil
			}
			skill, ok := socialSkillByID(argString(args, "skill_id", ""))
			if !ok {
				return &Result{OK: false, Error: "SKILL_NOT_FOUND"}, fmt.Errorf("SKILL_NOT_FOUND")
			}
			return okResult("reviewed social skill", map[string]interface{}{"skill_id": skill.ID, "skill": skill}), nil
		},
	})
	r.Register(Def{
		Name: "skill.run", Category: "skill", SideEffect: "external_publish",
		Description: "Run one reviewed social skill; browser and publish steps stay account-scoped and approval-gated",
		Params: []Param{
			{Name: "skill_id", Type: "string", Required: true},
			{Name: "account_ids", Type: "array", Description: "Required by social.account-check"},
		},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			return runSocialSkill(ctx, r, rt, args)
		},
	})
	r.Register(Def{
		Name: "social.workflow.catalog", Category: "social", SideEffect: "read",
		Description:  "List the stable per-platform data and publish workflows without opening a browser",
		AgentVisible: true, AgentRisk: AgentRiskL0, AgentWorkspace: "dashboard",
		AgentImpact:      "Reads reviewed workflow boundaries; never collects data, prepares, submits, or claims publication",
		AgentIdempotency: "read",
		Handler: func(_ context.Context, _ *Runtime, _ map[string]interface{}) (*Result, error) {
			items := make([]map[string]interface{}, 0, len(platforms.All()))
			for _, platform := range platforms.All() {
				readiness := platforms.Readiness(platform.Code)
				dataWorkflow := "manual_handoff"
				if egolite.SupportsCreatorMetrics(platform.Code) {
					dataWorkflow = "automated_snapshot"
				}
				publishWorkflow := "manual_handoff"
				if readiness.CanAttemptAutoSubmit {
					publishWorkflow = "prepare_then_submit_approval"
				} else if readiness.CanCreateJob {
					publishWorkflow = "freeze_then_manual_handoff"
				}
				items = append(items, map[string]interface{}{
					"platform_code": platform.Code, "platform_name": platform.Name,
					"data_workflow":    dataWorkflow,
					"data_steps":       []string{"select_accounts", "login_and_space_check", "collect_or_handoff", "persist_local_state", "review_evidence"},
					"publish_workflow": publishWorkflow,
					"publish_steps":    []string{"content_preflight", "freeze_plan", "prepare_approval", "prepare_or_handoff", "submit_bound_approval", "creator_backend_verify"},
					"submit_verified":  false,
				})
			}
			return okResult("reviewed platform workflows", map[string]interface{}{"platforms": items, "count": len(items)}), nil
		},
	})
	r.Register(Def{
		Name: "social.account-check", Category: "social", SideEffect: "browser",
		Description:  "Read a sanitized snapshot from one existing ego Space without navigation",
		Params:       []Param{{Name: "account_id", Type: "int", Required: true}},
		AgentVisible: true, AgentRisk: AgentRiskL1, AgentWorkspace: "browser",
		AgentImpact:      "Reads one sanitized Space snapshot; never navigates or claims login success",
		AgentIdempotency: "read",
		Handler:          handleSocialAccountCheck,
	})
	r.Register(Def{
		Name: "social.materials-list", Category: "social", SideEffect: "read",
		Description:  "Read the indexed local media and script catalogs without scanning arbitrary paths",
		AgentVisible: true, AgentRisk: AgentRiskL0, AgentWorkspace: "creation",
		AgentImpact:      "Reads only existing local Studio index records",
		AgentIdempotency: "read",
		Handler: func(ctx context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
			return runSocialMaterialsList(ctx, r, runtime)
		},
	})
	r.Register(Def{
		Name: "social.metrics.collect", Category: "social", SideEffect: "browser",
		Description:  "Read one reviewed creator analytics page through the account-bound Ego Space and persist a local daily snapshot",
		Params:       []Param{{Name: "account_id", Type: "int", Required: true}},
		AgentVisible: true, AgentRisk: AgentRiskL2, AgentWorkspace: "dashboard",
		AgentImpact:      "Navigates one existing account Space to a reviewed analytics page, restores the prior page, and hands control back",
		AgentIdempotency: "daily-upsert",
		Handler:          handleSocialMetricsCollect,
	})
	r.Register(Def{
		Name: "social.metrics.collect-batch", Category: "social", SideEffect: "browser",
		Description:  "Collect up to 100 explicitly selected account data workflows; the same Ego Space is always serialized and unadapted platforms persist manual handoff",
		Params:       []Param{{Name: "account_ids", Type: "array", Required: true}},
		AgentVisible: true, AgentRisk: AgentRiskL2, AgentWorkspace: "dashboard",
		AgentImpact:      "Runs reviewed read-only analytics collection for all explicitly selected account-bound Spaces, recording manual handoff where no extractor is reviewed",
		AgentIdempotency: "daily-upsert-per-account",
		Handler:          handleSocialMetricsCollectBatch,
	})
	r.Register(Def{
		Name: "social.metrics.overview", Category: "social", SideEffect: "read",
		Description:  "Read local seven-day creator snapshots across a selected history range without opening a browser",
		Params:       []Param{{Name: "history_days", Type: "int", Description: "History range: 7, 30, or 90 days"}},
		AgentVisible: true, AgentRisk: AgentRiskL0, AgentWorkspace: "dashboard",
		AgentImpact:      "Reads only local sanitized metric snapshots",
		AgentIdempotency: "read",
		Handler:          handleSocialMetricsOverview,
	})
	r.Register(Def{
		Name: "social.metrics.preferences", Category: "social", SideEffect: "read",
		Description: "Read local creator metric history and retention preferences",
		Handler:     handleSocialMetricsPreferences,
	})
	r.Register(Def{
		Name: "social.metrics.preferences.update", Category: "social", SideEffect: "write",
		Description: "Update local creator metric history and retention preferences",
		Params: []Param{
			{Name: "history_days", Type: "int", Required: true},
			{Name: "retention_days", Type: "int", Required: true},
		},
		Handler: handleSocialMetricsPreferencesUpdate,
	})
	r.Register(Def{
		Name: "social.publish-capabilities", Category: "social", SideEffect: "read",
		Description:  "List reviewed EgoLite platform and content-type preparation boundaries",
		AgentVisible: true, AgentRisk: AgentRiskL0, AgentWorkspace: "publish",
		AgentImpact:      "Reads built-in capability metadata; never opens a Space or authorizes submission",
		AgentIdempotency: "read",
		Handler: func(_ context.Context, _ *Runtime, _ map[string]interface{}) (*Result, error) {
			contracts := make([]map[string]interface{}, 0, 16)
			for _, manifest := range egopublish.EgoLiteManifests() {
				for _, capability := range manifest.Capabilities {
					contracts = append(contracts, map[string]interface{}{
						"platform_code":             manifest.PlatformCode,
						"platform_name":             manifest.Name,
						"first_release":             manifest.FirstRelease,
						"content_type":              capability.ContentType,
						"prepare_mode":              capability.PrepareMode,
						"prepare_adapter_supported": capability.PrepareAdapterSupported,
						"submit_adapter_supported":  capability.SubmitAdapterSupported,
						"verify_adapter_supported":  capability.VerifyAdapterSupported,
						"submit_verified":           capability.SubmitVerified,
						"verify_verified":           capability.VerifyVerified,
						"status":                    manifest.Status,
						"skip_when_logged_out":      true,
					})
				}
			}
			return okResult("reviewed EgoLite publish capability boundaries", map[string]interface{}{
				"contracts": contracts, "count": len(contracts),
			}), nil
		},
	})
	r.Register(Def{
		Name: "social.publish-prepare", Category: "social", SideEffect: "external_publish",
		Description: "Prepare a frozen batch in account Spaces and stop before final submission",
		Params: []Param{
			{Name: "plan_id", Type: "int", Required: true},
			{Name: "snapshot_hash", Type: "string", Required: true},
			{Name: "approval_id", Type: "string", Required: true},
			{Name: "nonce", Type: "string", Required: true},
		},
		Handler: func(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
			if runtime == nil || !runtime.TrustedLocalUI {
				return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
			}
			return r.Call(ctx, runtime, "publish.plan.prepare", args)
		},
	})
	r.Register(Def{
		Name: "social.publish-submit-approved", Category: "social", SideEffect: "external_publish",
		Description: "Submit each frozen target at most once using a submit-bound approval",
		Params: []Param{
			{Name: "plan_id", Type: "int", Required: true},
			{Name: "snapshot_hash", Type: "string", Required: true},
			{Name: "approval_id", Type: "string", Required: true},
			{Name: "nonce", Type: "string", Required: true},
		},
		Handler: func(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
			if runtime == nil || !runtime.TrustedLocalUI {
				return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
			}
			return r.Call(ctx, runtime, "publish.plan.submit", args)
		},
	})
}

func handleSocialAccountCheck(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	accountID := int64(argInt(args, "account_id", 0))
	if accountID <= 0 {
		return &Result{OK: false, Error: "INVALID_ACCOUNT_ID"}, fmt.Errorf("INVALID_ACCOUNT_ID")
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	var runner egolite.ProgramRunner
	if runtime != nil {
		runner = runtime.EgoRunner
		if runner == nil {
			runner = egolite.NewRuntime(runtime.EgoBrowserCommand, 0)
		}
	}
	if runner == nil {
		return &Result{OK: false, Error: "EGO_RUNTIME_UNAVAILABLE"}, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	observation, err := egolite.NewSpaceService(database, runner).ObserveAccount(ctx, accountID)
	if err != nil {
		return &Result{OK: false, Error: safeSocialAccountCheckErrorCode(err)}, nil
	}
	return okResult("sanitized Space snapshot observed; account identity remains unverified", map[string]interface{}{
		"observation": observation,
	}), nil
}

func safeSocialAccountCheckErrorCode(err error) string {
	if err != nil && strings.TrimSpace(err.Error()) == "EGO_SPACE_USER_CONTROLLED" {
		return "EGO_SPACE_USER_CONTROLLED"
	}
	return "EGO_ACCOUNT_CHECK_FAILED"
}

func handleSocialMetricsCollect(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	accountID := int64(argInt(args, "account_id", 0))
	if accountID <= 0 {
		return &Result{OK: false, Error: "INVALID_ACCOUNT_ID"}, fmt.Errorf("INVALID_ACCOUNT_ID")
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	runner := egoMetricsRunner(runtime)
	if runner == nil {
		return &Result{OK: false, Error: "EGO_RUNTIME_UNAVAILABLE"}, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	result, err := egolite.NewCreatorMetricsService(database, runner).Collect(ctx, accountID)
	if err != nil {
		code := safeSocialMetricsErrorCode(err)
		data := creatorMetricCollectionFields(result)
		if data == nil {
			data = map[string]interface{}{"account_id": accountID}
		}
		data["error_code"] = code
		if _, ok := data["stage"]; !ok {
			data["stage"] = creatorMetricFailureStage(err)
		}
		if detail := egolite.SafeCreatorMetricErrorDetail(err); detail != "" {
			data["run_error"] = detail
		}
		return &Result{OK: false, Error: code, Data: data}, nil
	}
	return okResult("creator metrics collection finished", map[string]interface{}{"collection": result}), nil
}

func handleSocialMetricsCollectBatch(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	accountIDs, err := argInt64List(args["account_ids"])
	if err != nil || len(accountIDs) == 0 || len(accountIDs) > 100 {
		return &Result{OK: false, Error: "SKILL_INPUT_INVALID"}, fmt.Errorf("SKILL_INPUT_INVALID")
	}
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			return &Result{OK: false, Error: "SKILL_INPUT_INVALID"}, fmt.Errorf("SKILL_INPUT_INVALID")
		}
		if _, duplicate := seen[accountID]; duplicate {
			return &Result{OK: false, Error: "SKILL_INPUT_INVALID"}, fmt.Errorf("SKILL_INPUT_INVALID")
		}
		seen[accountID] = struct{}{}
	}
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	runner := egoMetricsRunner(runtime)
	if runner == nil {
		return &Result{OK: false, Error: "EGO_RUNTIME_UNAVAILABLE"}, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	service := egolite.NewCreatorMetricsService(database, runner)
	rows := make([]map[string]interface{}, len(accountIDs))
	jobs := make([]egolite.SpaceJob, 0, len(accountIDs))
	for index, accountID := range accountIDs {
		account, accountErr := database.GetAccount(accountID)
		if accountErr != nil {
			rows[index] = map[string]interface{}{"account_id": accountID, "ok": false, "error_code": "ACCOUNT_NOT_FOUND"}
			continue
		}
		if !egolite.SupportsCreatorMetrics(account.PlatformCode) {
			collection, collectErr := service.Collect(ctx, accountID)
			if collectErr != nil {
				rows[index] = creatorMetricFailureRow(collection, accountID, account.PlatformCode, collectErr)
				continue
			}
			row := creatorMetricCollectionFields(collection)
			row["ok"] = collection.State == "manual_takeover" || collection.State == "skipped"
			row["range_verified"] = false
			row["post_count"] = 0
			rows[index] = row
			continue
		}
		// Grouping only needs the persisted binding: Collect owns the runtime
		// membership check and re-binds a vanished Space itself, so probing here
		// would repeat that read once per account and could disagree with it.
		space, spaceErr := database.GetEgoSpaceByIdentity(account.IdentityID)
		if spaceErr != nil || space == nil {
			if account.LoginState != "logged_in" {
				rows[index] = map[string]interface{}{"account_id": accountID, "platform_code": account.PlatformCode, "ok": true, "state": "skipped", "error_code": "ACCOUNT_NOT_LOGGED_IN"}
			} else {
				rows[index] = map[string]interface{}{"account_id": accountID, "platform_code": account.PlatformCode, "ok": false, "error_code": "EGO_SPACE_BINDING_MISSING"}
			}
			continue
		}
		index, accountID := index, accountID
		spaceID := space.SpaceID
		jobs = append(jobs, egolite.SpaceJob{ID: accountID, SpaceID: spaceID, Run: func(jobCtx context.Context) error {
			collection, collectErr := service.Collect(jobCtx, accountID)
			if collectErr != nil {
				rows[index] = creatorMetricFailureRow(collection, accountID, account.PlatformCode, collectErr)
				return collectErr
			}
			row := creatorMetricCollectionFields(collection)
			row["ok"] = collection.State == "observed" || collection.State == "skipped" || collection.State == "manual_takeover"
			rows[index] = row
			return nil
		}})
	}
	concurrency := 1
	if runtime != nil && runtime.MaxConcurrentSpaces > 0 {
		concurrency = runtime.MaxConcurrentSpaces
	}
	runErr := egolite.NewSpaceScheduler(concurrency).Run(ctx, jobs)
	allObserved := true
	hasFailure := false
	for index := range rows {
		if rows[index] == nil {
			rows[index] = map[string]interface{}{"account_id": accountIDs[index], "ok": false, "error_code": "EGO_METRICS_COLLECTION_FAILED"}
		}
		if rows[index]["state"] != "observed" {
			allObserved = false
		}
		if rows[index]["ok"] != true {
			hasFailure = true
		}
	}
	if runErr != nil && ctx.Err() != nil {
		return &Result{OK: false, Error: "EGO_METRICS_BATCH_CANCELLED"}, ctx.Err()
	}
	data := map[string]interface{}{
		"accounts": rows, "all_observed": allObserved,
	}
	if hasFailure {
		return &Result{OK: false, Error: "EGO_METRICS_BATCH_FAILED", Message: "creator metrics batch finished with failures", Data: data}, nil
	}
	return okResult("creator metrics batch finished", data), nil
}

func safeSocialMetricsErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	switch code {
	case "EGO_HANDOFF_FAILED",
		"EGO_LOGIN_QR_PLATFORM_MISMATCH",
		"EGO_METRICS_ACCOUNT_UNAVAILABLE",
		"EGO_METRICS_INPUT_INVALID",
		"EGO_METRICS_RESULT_INVALID",
		"EGO_METRICS_TIMEOUT",
		"EGO_OUTPUT_INVALID",
		"EGO_OUTPUT_LIMIT",
		"EGO_PROFILE_BINDING_UNAVAILABLE",
		"EGO_PROFILE_NOT_FOUND",
		"EGO_PROGRAM_INVALID",
		"EGO_RUNTIME_CANCELLED",
		"EGO_RUNTIME_EXECUTION_FAILED",
		"EGO_RUNTIME_TIMEOUT",
		"EGO_RUNTIME_UNAVAILABLE",
		"EGO_SPACE_BINDING_MISSING",
		"EGO_SPACE_NOT_FOUND",
		"EGO_SPACE_REBIND_FAILED",
		"EGO_SPACE_INACTIVE",
		"EGO_SPACE_USER_CONTROLLED":
		return code
	default:
		return "EGO_METRICS_COLLECTION_FAILED"
	}
}

// creatorMetricCollectionFields is the one row shape shared by the
// single-account and batch paths. Optional fields are only present when they
// exist, so a missing scoped engagement is never reported as an absent key by
// one path and an explicit null by the other.
func creatorMetricCollectionFields(collection *egolite.CreatorMetricCollectResult) map[string]interface{} {
	if collection == nil {
		return nil
	}
	row := map[string]interface{}{
		"account_id": collection.AccountID, "platform_code": collection.PlatformCode,
		"state": collection.State, "error_code": collection.ErrorCode,
		"stage": collection.Stage, "range_verified": collection.RangeVerified,
		"metric_scope": collection.MetricScope, "scope_source": collection.ScopeSource,
		"metric_evidence": collection.MetricEvidence, "scoped_evidence": collection.ScopedEvidence,
		"snapshot": collection.Snapshot, "post_count": collection.PostCount,
		"page_title": collection.PageTitle, "matched_labels": collection.MatchedLabels,
		"matched_markers": collection.MatchedMarkers, "text_length": collection.TextLength,
		"label_hints": collection.LabelHints, "route_hints": collection.RouteHints,
		"resource_hints": collection.ResourceHints, "resource_diagnostics": collection.ResourceDiagnostics,
		"metric_candidates": collection.MetricCandidates, "iframe_count": collection.IframeCount,
		"attempts": collection.Attempts,
	}
	if collection.ScopedEngagement != nil {
		row["scoped_engagement"] = *collection.ScopedEngagement
	}
	if collection.RunError != "" {
		row["run_error"] = collection.RunError
	}
	return row
}

// creatorMetricFailureRow keeps whatever the service learned before it failed:
// account, stage and the runtime's own message, not only a generic code.
func creatorMetricFailureRow(collection *egolite.CreatorMetricCollectResult, accountID int64, platformCode string, err error) map[string]interface{} {
	row := creatorMetricCollectionFields(collection)
	if row == nil {
		// Even an early failure keeps the shape every other row has, so a
		// caller reading state/stage never has to special-case a missing row.
		row = map[string]interface{}{
			"account_id": accountID, "platform_code": platformCode,
			"state": "unknown", "stage": creatorMetricFailureStage(err),
		}
	}
	row["ok"] = false
	row["error_code"] = safeSocialMetricsErrorCode(err)
	if detail := egolite.SafeCreatorMetricErrorDetail(err); detail != "" {
		row["run_error"] = detail
	}
	return row
}

// creatorMetricFailureStage says where a collection stopped when the service
// could not return a staged result, such as a Space binding that no longer
// exists.
func creatorMetricFailureStage(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if strings.HasPrefix(text, "EGO_SPACE_") || strings.HasPrefix(text, "EGO_PROFILE_") {
		return "SPACE_BINDING"
	}
	return "UNKNOWN"
}

func handleSocialMetricsOverview(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	historyDays := argInt(args, "history_days", 7)
	overview, err := database.CreatorMetricOverview(historyDays, time.Now())
	if err != nil {
		return &Result{OK: false, Error: "CREATOR_METRICS_OVERVIEW_FAILED"}, err
	}
	return okResult("local creator metric history", map[string]interface{}{"overview": overview}), nil
}

func handleSocialMetricsPreferences(_ context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	preferences, err := database.CreatorMetricPreferences()
	if err != nil {
		return &Result{OK: false, Error: "CREATOR_METRIC_PREFERENCES_FAILED"}, err
	}
	return okResult("local creator metric preferences", map[string]interface{}{"preferences": preferences}), nil
}

func handleSocialMetricsPreferencesUpdate(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	preferences, err := database.UpdateCreatorMetricPreferences(
		argInt(args, "history_days", 0),
		argInt(args, "retention_days", 0),
	)
	if err != nil {
		return &Result{OK: false, Error: "CREATOR_METRIC_PREFERENCES_INVALID"}, err
	}
	return okResult("creator metric preferences updated", map[string]interface{}{"preferences": preferences}), nil
}

func egoMetricsRunner(runtime *Runtime) egolite.ProgramRunner {
	if runtime == nil {
		return nil
	}
	if runtime.EgoRunner != nil {
		return runtime.EgoRunner
	}
	return egolite.NewRuntime(runtime.EgoBrowserCommand, 90*time.Second)
}

func socialSkillByID(id string) (SocialSkill, bool) {
	for _, skill := range socialSkills {
		if skill.ID == id {
			return skill, true
		}
	}
	return SocialSkill{}, false
}

func runSocialSkill(ctx context.Context, registry *Registry, rt *Runtime, args map[string]interface{}) (*Result, error) {
	skill, ok := socialSkillByID(argString(args, "skill_id", ""))
	if !ok {
		return &Result{OK: false, Error: "SKILL_NOT_FOUND"}, fmt.Errorf("SKILL_NOT_FOUND")
	}
	switch skill.ID {
	case "social.account-check":
		return runSocialAccountChecks(ctx, registry, rt, args)
	case "social.materials-list":
		return registry.Call(ctx, rt, "social.materials-list", nil)
	case "social.metrics-collect":
		return registry.Call(ctx, rt, "social.metrics.collect-batch", map[string]interface{}{"account_ids": args["account_ids"]})
	case "one-click-publish":
		return &Result{OK: false, Error: "SKILL_ORCHESTRATION_REQUIRED"}, fmt.Errorf("SKILL_ORCHESTRATION_REQUIRED")
	case "social.publish-prepare", "video-publisher", "multipost", "social.publish-submit-approved":
		// Publishing is deliberately not executable through a generic Skill
		// wrapper. Agent-facing callers must use the exact top-level
		// publish.plan.freeze/prepare/submit tools, where the Broker can bind a
		// one-time domain approval to the frozen plan without exposing it to the
		// model. The local UI aliases remain available as non-Agent tools.
		return &Result{OK: false, Error: "SKILL_ORCHESTRATION_REQUIRED"}, fmt.Errorf("SKILL_ORCHESTRATION_REQUIRED")
	default:
		return &Result{OK: false, Error: "SKILL_NOT_IMPLEMENTED"}, fmt.Errorf("SKILL_NOT_IMPLEMENTED")
	}
}

func runSocialAccountChecks(ctx context.Context, registry *Registry, rt *Runtime, args map[string]interface{}) (*Result, error) {
	ids, err := argInt64List(args["account_ids"])
	if err != nil || len(ids) == 0 || len(ids) > 100 {
		return &Result{OK: false, Error: "SKILL_INPUT_INVALID"}, fmt.Errorf("SKILL_INPUT_INVALID")
	}
	seen := make(map[int64]struct{}, len(ids))
	rows := make([]map[string]interface{}, 0, len(ids))
	allOK := true
	for _, accountID := range ids {
		if accountID <= 0 {
			return &Result{OK: false, Error: "SKILL_INPUT_INVALID"}, fmt.Errorf("SKILL_INPUT_INVALID")
		}
		if _, duplicate := seen[accountID]; duplicate {
			return &Result{OK: false, Error: "SKILL_INPUT_INVALID"}, fmt.Errorf("SKILL_INPUT_INVALID")
		}
		seen[accountID] = struct{}{}
		result, callErr := registry.Call(ctx, rt, "social.account-check", map[string]interface{}{"account_id": accountID})
		row := map[string]interface{}{"account_id": accountID, "ok": callErr == nil && result != nil && result.OK}
		if result != nil {
			row["error"] = result.Error
			row["data"] = result.Data
		}
		rows = append(rows, row)
		if callErr != nil || result == nil || !result.OK {
			allOK = false
		}
	}
	if !allOK {
		return &Result{OK: false, Error: "SKILL_ACCOUNT_CHECK_FAILED", Data: map[string]interface{}{"accounts": rows}}, fmt.Errorf("SKILL_ACCOUNT_CHECK_FAILED")
	}
	return okResult("account checks complete", map[string]interface{}{"accounts": rows}), nil
}

func runSocialMaterialsList(ctx context.Context, registry *Registry, rt *Runtime) (*Result, error) {
	media, mediaErr := registry.Call(ctx, rt, "studio.media.list", nil)
	scripts, scriptsErr := registry.Call(ctx, rt, "studio.script.list", nil)
	if mediaErr != nil || scriptsErr != nil || media == nil || scripts == nil || !media.OK || !scripts.OK {
		return &Result{OK: false, Error: "SKILL_MATERIALS_LIST_FAILED"}, fmt.Errorf("SKILL_MATERIALS_LIST_FAILED")
	}
	return okResult("local material indexes", map[string]interface{}{"media": media.Data, "scripts": scripts.Data}), nil
}
