package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type agentSettingSpec struct {
	Type    string
	Default interface{}
	Enum    []string
	Min     int
	Max     int
}

var agentSettingSpecs = map[string]agentSettingSpec{
	"ui.theme":                               {Type: "string", Default: "system", Enum: []string{"system", "light", "dark"}},
	"agent.drawer_width":                     {Type: "string", Default: "standard", Enum: []string{"narrow", "standard", "wide"}},
	"ego.max_concurrency":                    {Type: "int", Default: 1, Min: 1, Max: 12},
	"notifications.frequency":                {Type: "string", Default: "realtime", Enum: []string{"realtime", "daily", "off"}},
	"notifications.wechat_enabled":           {Type: "bool", Default: false},
	"notifications.feishu_enabled":           {Type: "bool", Default: false},
	"notifications.dingtalk_enabled":         {Type: "bool", Default: false},
	"provider.codex.model":                   {Type: "model", Default: ""},
	"provider.opencode.model":                {Type: "model", Default: ""},
	"provider.antigravity.model":             {Type: "model", Default: ""},
	"provider.codex.last_conversation":       {Type: "id", Default: ""},
	"provider.opencode.last_conversation":    {Type: "id", Default: ""},
	"provider.antigravity.last_conversation": {Type: "id", Default: ""},
}

func registerAgentSurface(r *Registry) {
	r.Register(Def{
		Name: "content.list", Category: "content", SideEffect: "read",
		Description: "List local content drafts and publishing state",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-200; default 100"}},
		Handler:     handleAgentContentList,
	})
	r.Register(Def{
		Name: "media.generation.list", Category: "media", SideEffect: "read",
		Description: "List recent AI generation jobs without prompts or local paths",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-100; default 50"}},
		Handler:     handleAgentGenerationList,
	})
	r.Register(Def{
		Name: "dashboard.summary", Category: "dashboard", SideEffect: "read",
		Description: "Read the local dashboard summary without collecting remote data",
		Handler:     handleAgentDashboardSummary,
	})
	r.Register(Def{
		Name: "dashboard.metrics", Category: "dashboard", SideEffect: "read",
		Description: "Read locally stored aggregate post metrics",
		Handler:     handleAgentDashboardMetrics,
	})
	r.Register(Def{
		Name: "dashboard.publish_records", Category: "dashboard", SideEffect: "read",
		Description: "List redacted local publish records",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-100; default 50"}},
		Handler:     handleAgentDashboardPublishRecords,
	})
	r.Register(Def{
		Name: "dashboard.notifications.read_all", Category: "dashboard", SideEffect: "write",
		Description: "Mark local dashboard notifications as read",
		Handler:     handleAgentDashboardNotificationsRead,
	})
	r.Register(Def{
		Name: "settings.get", Category: "settings", SideEffect: "read",
		Description: "Read only allowlisted non-sensitive Agent settings",
		Params:      []Param{{Name: "keys", Type: "array", Description: "Optional allowlisted keys"}},
		Handler:     handleAgentSettingsGet,
	})
	r.Register(Def{
		Name: "settings.patch", Category: "settings", SideEffect: "write",
		Description: "Versioned update of allowlisted non-sensitive Agent settings",
		Params: []Param{
			{Name: "changes", Type: "object", Required: true, Description: "key to value map"},
			{Name: "versions", Type: "object", Required: true, Description: "key to expected version map"},
		},
		Handler: handleAgentSettingsPatch,
	})

	type exposure struct {
		name, workspace string
		risk            AgentRisk
		impact, idem    string
	}
	exposures := []exposure{
		{"account.list", "browser", AgentRiskL0, "读取已入库账号的非敏感摘要", "read"},
		{"account.tag.list", "browser", AgentRiskL0, "读取账号标签、顺序和账号数量", "read"},
		{"account.tag.resolve", "browser", AgentRiskL0, "按标签读取匹配账号", "read"},
		{"account.tag.create", "browser", AgentRiskL1, "创建或修改一个本地账号标签", "upsert"},
		{"account.tag.update", "browser", AgentRiskL1, "修改明确选择账号的标签", "object-scoped"},
		{"account.tag.reorder", "browser", AgentRiskL1, "调整账号标签的展示优先级", "replace-order"},
		{"ego.doctor", "browser", AgentRiskL0, "验证 ego-browser 能力契约", "read"},
		{"ego.identity.list", "browser", AgentRiskL0, "读取逻辑运营身份", "read"},
		{"ego.matrix.create", "browser", AgentRiskL1, "创建一个本地浏览器配置，不打开浏览器", "create-once"},
		{"ego.matrix.configure-platforms", "browser", AgentRiskL1, "替换一个浏览器配置的启用平台账号", "replace-set"},
		{"ego.space.list", "browser", AgentRiskL0, "读取 Space 状态", "read"},
		{"ego.space.ensure", "browser", AgentRiskL1, "为指定账号所属浏览器创建或复用隔离 Space", "object-scoped"},
		{"ego.space.open", "browser", AgentRiskL2, "在指定矩阵绑定的 EgoLite Space 中打开最多三个已配置平台后台", "object-scoped"},
		{"ego.space.login", "browser", AgentRiskL2, "打开登录页并交接给用户", "object-scoped"},
		{"ego.space.resume", "browser", AgentRiskL2, "经用户确认后重新接管 Space", "object-scoped"},
		{"ego.bookmarks.import", "browser", AgentRiskL2, "通过受控界面导入 Alpha-Nexus 平台收藏夹", "versioned"},
		{"ego.login.batch", "browser", AgentRiskL2, "最多打开三个创作者后台并交接用户登录", "object-scoped"},
		{"ego.login.verify", "browser", AgentRiskL2, "用户确认后只读核验本批账号资料", "object-scoped"},
		{"ego.session.check", "browser", AgentRiskL1, "串行检查现有浏览器的平台登录状态", "transition-deduplicated"},

		{"content.list", "content", AgentRiskL0, "读取本地内容草稿", "read"},
		{"studio.media.list", "content", AgentRiskL0, "读取已入库素材索引", "read"},
		{"studio.script.list", "content", AgentRiskL0, "读取本地脚本", "read"},
		{"media.import", "content", AgentRiskL2, "读取并导入你在本轮消息中明确给出的本地文件或文件夹", "content-hash"},
		{"content.create", "content", AgentRiskL1, "创建可恢复的本地内容草稿", "create-once"},
		{"content.media.attach", "content", AgentRiskL1, "把已入库素材按角色和顺序关联到本地内容", "object-scoped"},
		{"content.variant.upsert", "content", AgentRiskL1, "更新本地平台内容变体", "upsert"},
		{"knowledge.search", "knowledge", AgentRiskL0, "在本地知识库检索品牌资料与历史内容", "read"},
		{"media.generate", "content", AgentRiskL2, "生成一张图片或一段视频素材（需 confirm=true）", "create-once"},
		{"studio.script.create", "content", AgentRiskL1, "保存本地脚本", "create-once"},
		{"studio.clip.plan", "content", AgentRiskL2, "联网生成可审核剪辑计划", "create-once"},
		{"studio.clip.execute", "content", AgentRiskL2, "生成本地视频文件", "object-scoped"},
		{"studio.schedule-handoff.create", "content", AgentRiskL2, "保存已审核内容和 IANA 墙钟时间的本地交接，不启动定时器", "intent-hash"},
		{"studio.schedule-handoff.get", "content", AgentRiskL0, "读取本地排期交接状态", "read"},
		{"studio.schedule-handoff.list", "content", AgentRiskL0, "读取本地排期交接摘要", "read"},
		{"studio.schedule-handoff.cancel", "content", AgentRiskL2, "取消一个尚未冻结的本地排期交接", "object-scoped"},

		{"skill.list", "skills", AgentRiskL0, "读取已审核 Skill 清单", "read"},
		{"skill.get", "skills", AgentRiskL0, "读取一个已审核 Skill", "read"},
		{"dashboard.summary", "dashboard", AgentRiskL0, "读取本地汇总", "read"},
		{"dashboard.metrics", "dashboard", AgentRiskL0, "读取本地指标", "read"},
		{"dashboard.publish_records", "dashboard", AgentRiskL0, "读取脱敏发布记录", "read"},
		{"dashboard.notifications.read_all", "dashboard", AgentRiskL1, "将本地通知标记为已读", "idempotent"},
		{"social.metrics.overview", "dashboard", AgentRiskL0, "读取创作者近 7 天指标总览", "read"},

		{"publish.plan.get", "publish", AgentRiskL0, "读取一个冻结发布计划", "read"},
		{"publish.plan.list", "publish", AgentRiskL0, "读取发布计划摘要与目标状态", "read"},
		{"publish.plan.freeze", "publish", AgentRiskL3, "冻结不可变发布计划", "snapshot-hash"},
		{"publish.plan.freeze-handoff", "publish", AgentRiskL3, "从 Studio 已审核交接冻结不可变发布计划", "handoff-and-snapshot-hash"},
		{"publish.plan.prepare", "publish", AgentRiskL3, "预填发布后台但不点击最终发布", "single-use-domain-approval"},
		{"publish.plan.submit", "publish", AgentRiskL3, "使用管理界面单次授权提交冻结发布计划", "single-use-domain-approval"},
		{"settings.get", "settings", AgentRiskL0, "读取固定白名单设置", "read"},
		{"settings.patch", "settings", AgentRiskL1, "修改可恢复的非敏感设置", "versioned"},
		{"workflow.run", "workflow", AgentRiskL0, "执行已分类步骤，采用内部最高风险", "step-dependent"},
	}
	for _, item := range exposures {
		if err := r.SetAgentMeta(item.name, item.workspace, item.risk, item.impact, item.idem); err != nil {
			panic(err)
		}
	}
}

func handleAgentContentList(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	items, err := database.ListContents()
	if err != nil {
		return nil, err
	}
	limit := boundedLimit(args, 100, 200)
	if len(items) > limit {
		items = items[:limit]
	}
	return okResult("local contents", map[string]interface{}{"count": len(items), "contents": items}), nil
}

func handleAgentGenerationList(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	items, err := database.ListRecentGenerationJobs(boundedLimit(args, 50, 100))
	if err != nil {
		return nil, err
	}
	return okResult("generation history", map[string]interface{}{"count": len(items), "jobs": items}), nil
}

func handleAgentDashboardSummary(_ context.Context, rt *Runtime, _ map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	summary, err := database.GetLocalDashboardSummary()
	if err != nil {
		return nil, err
	}
	return okResult("dashboard summary", map[string]interface{}{"summary": summary}), nil
}

func handleAgentDashboardMetrics(_ context.Context, rt *Runtime, _ map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	summary, err := database.GetMetricSummary()
	if err != nil {
		return nil, err
	}
	return okResult("dashboard metrics", map[string]interface{}{"metrics": summary}), nil
}

func handleAgentDashboardPublishRecords(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	records, err := database.ListPublishRecords(boundedLimit(args, 50, 100))
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]interface{}{
			"id": record.ID, "content_id": record.ContentID, "account_id": record.AccountID,
			"platform": record.PlatformCode, "content_type": record.ContentType, "status": record.Status,
			"source": record.Source,
		})
	}
	return okResult("publish records", map[string]interface{}{"count": len(items), "records": items}), nil
}

func handleAgentDashboardNotificationsRead(_ context.Context, rt *Runtime, _ map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	changed, err := database.MarkAllNotificationsRead()
	if err != nil {
		return nil, err
	}
	return okResult("notifications marked read", map[string]interface{}{"changed": changed}), nil
}

func handleAgentSettingsGet(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	keys, err := requestedSettingKeys(args["keys"])
	if err != nil {
		return &Result{OK: false, Error: "SETTINGS_KEY_FORBIDDEN"}, err
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	settings := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		spec := agentSettingSpecs[key]
		var raw string
		var version int
		err := database.QueryRow(`SELECT value_json,version FROM agent_settings WHERE key=?`, key).Scan(&raw, &version)
		value := spec.Default
		if err == nil {
			if decodeErr := json.Unmarshal([]byte(raw), &value); decodeErr != nil {
				return nil, decodeErr
			}
		} else if err != sql.ErrNoRows {
			return nil, err
		}
		settings[key] = map[string]interface{}{"value": value, "version": version}
	}
	return okResult("allowlisted settings", map[string]interface{}{"settings": settings}), nil
}

func handleAgentSettingsPatch(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	changes, ok := args["changes"].(map[string]interface{})
	if !ok || len(changes) == 0 || len(changes) > 20 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("changes must contain 1-20 settings")
	}
	versions, ok := args["versions"].(map[string]interface{})
	if !ok {
		return &Result{OK: false, Error: "SETTINGS_VERSION_REQUIRED"}, fmt.Errorf("versions are required")
	}
	normalized := make(map[string]interface{}, len(changes))
	expected := make(map[string]int, len(changes))
	for key, value := range changes {
		spec, exists := agentSettingSpecs[key]
		if !exists {
			return &Result{OK: false, Error: "SETTINGS_KEY_FORBIDDEN"}, fmt.Errorf("setting %q is not allowlisted", key)
		}
		validated, err := validateAgentSetting(spec, value)
		if err != nil {
			return &Result{OK: false, Error: "SETTINGS_VALUE_INVALID"}, fmt.Errorf("%s: %w", key, err)
		}
		version, ok := interfaceInt(versions[key])
		if !ok || version < 0 {
			return &Result{OK: false, Error: "SETTINGS_VERSION_REQUIRED"}, fmt.Errorf("expected version is required for %s", key)
		}
		normalized[key] = validated
		expected[key] = version
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	tx, err := database.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated := make(map[string]interface{}, len(normalized))
	for key, value := range normalized {
		var current int
		err := tx.QueryRow(`SELECT version FROM agent_settings WHERE key=?`, key).Scan(&current)
		if err == sql.ErrNoRows {
			current = 0
		} else if err != nil {
			return nil, err
		}
		if current != expected[key] {
			return &Result{OK: false, Error: "SETTINGS_VERSION_CONFLICT"}, fmt.Errorf("setting %s changed", key)
		}
		encoded, _ := json.Marshal(value)
		next := current + 1
		if _, err := tx.Exec(`INSERT INTO agent_settings(key,value_json,version,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,version=excluded.version,updated_at=excluded.updated_at`,
			key, string(encoded), next, now); err != nil {
			return nil, err
		}
		updated[key] = map[string]interface{}{"value": value, "version": next}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return okResult("settings updated", map[string]interface{}{"settings": updated}), nil
}

func settingsPatchEnablesExternalChannel(args map[string]interface{}) bool {
	changes, _ := args["changes"].(map[string]interface{})
	for key, value := range changes {
		if strings.HasPrefix(key, "notifications.") && strings.HasSuffix(key, "_enabled") {
			enabled, _ := value.(bool)
			if enabled {
				return true
			}
		}
	}
	return false
}

func requestedSettingKeys(raw interface{}) ([]string, error) {
	if raw == nil {
		keys := make([]string, 0, len(agentSettingSpecs))
		for key := range agentSettingSpecs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys, nil
	}
	values, ok := raw.([]interface{})
	if !ok || len(values) > len(agentSettingSpecs) {
		return nil, fmt.Errorf("settings keys must be an array")
	}
	keys := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		key, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("settings key must be a string")
		}
		if _, exists := agentSettingSpecs[key]; !exists {
			return nil, fmt.Errorf("setting %q is not allowlisted", key)
		}
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func validateAgentSetting(spec agentSettingSpec, value interface{}) (interface{}, error) {
	switch spec.Type {
	case "bool":
		result, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("must be boolean")
		}
		return result, nil
	case "int":
		result, ok := interfaceInt(value)
		if !ok || result < spec.Min || result > spec.Max {
			return nil, fmt.Errorf("must be between %d and %d", spec.Min, spec.Max)
		}
		return result, nil
	case "string":
		result, ok := value.(string)
		if !ok || !containsString(spec.Enum, result) {
			return nil, fmt.Errorf("unsupported value")
		}
		return result, nil
	case "model":
		result, ok := value.(string)
		if !ok || len(result) > 120 || unsafeIdentifier(result) {
			return nil, fmt.Errorf("invalid model identifier")
		}
		return strings.TrimSpace(result), nil
	case "id":
		result, ok := value.(string)
		if !ok || len(result) > 160 || (result != "" && !strings.HasPrefix(result, "conv_")) || unsafeIdentifier(result) {
			return nil, fmt.Errorf("invalid conversation identifier")
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported setting type")
	}
}

func unsafeIdentifier(value string) bool {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\ \t\r\n") {
		return true
	}
	lower := strings.ToLower(value)
	return strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "://")
}

func interfaceInt(value interface{}) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case int64:
		return int(number), true
	case float64:
		if number == float64(int(number)) {
			return int(number), true
		}
	}
	return 0, false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func boundedLimit(args map[string]interface{}, fallback, maximum int) int {
	limit := argInt(args, "limit", fallback)
	if limit < 1 {
		return fallback
	}
	if limit > maximum {
		return maximum
	}
	return limit
}
