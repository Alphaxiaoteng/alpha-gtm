package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
)

func registerFriendlyTools(s *Server, h *handlers) {
	s.AddTool(ToolDef{
		Name:        "list_platforms",
		Description: "获取平台能力与发布边界：browser_automation / api_credentials / manual_handoff / unavailable",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			plats, err := h.db.ListPlatforms()
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0, len(plats))
			for _, p := range plats {
				readiness := platforms.Readiness(p.Code)
				canCreate := p.Enabled && readiness.CanCreateJob
				out = append(out, map[string]any{
					"id": p.ID, "code": p.Code, "name": p.Name, "enabled": p.Enabled,
					"supports_article": p.SupportsArticle, "supports_graph_text": p.SupportsGraphText,
					"supports_video": p.SupportsVideo, "v1_publish": canCreate, "login_url": p.LoginURL,
					"publish_mode": readiness.Mode, "can_create_job": canCreate,
					"can_attempt_auto_submit": canCreate && readiness.CanAttemptAutoSubmit,
					"publish_summary":         readiness.Summary,
				})
			}
			return map[string]any{"count": len(out), "platforms": out}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "list_accounts",
		Description: "获取所有平台账号列表（ID、名称、平台、登录态、分组）",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{"platform": propString("可选平台过滤")}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			var accounts []db.Account
			var err error
			if p := argString(args, "platform", ""); p != "" {
				accounts, err = h.db.ListAccountsByPlatform(p)
			} else {
				accounts, err = h.db.ListAccounts()
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{"count": len(accounts), "accounts": accountRows(accounts)}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "list_logged_accounts",
		Description: "仅返回已登录（login_state=logged_in）的账号",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{"platform": propString("可选平台过滤")}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			var accounts []db.Account
			var err error
			if p := argString(args, "platform", ""); p != "" {
				accounts, err = h.db.ListAccountsByPlatform(p)
			} else {
				accounts, err = h.db.ListAccounts()
			}
			if err != nil {
				return nil, err
			}
			var logged []db.Account
			for _, a := range accounts {
				if a.LoginState == "logged_in" {
					logged = append(logged, a)
				}
			}
			return map[string]any{"count": len(logged), "accounts": accountRows(logged)}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "list_articles",
		Description: "获取内容列表（兼容 TurboPush 命名；含 article/graph_text/video/reel）",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{
			"type":  propString("可选类型过滤"),
			"limit": propNumber("最多返回条数，默认 50"),
		}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			items, err := h.db.ListContents()
			if err != nil {
				return nil, err
			}
			typ := argString(args, "type", "")
			limit := 50
			if n, ok := argInt64(args, "limit"); ok && n > 0 {
				limit = int(n)
			}
			rows := make([]map[string]any, 0)
			for _, c := range items {
				if typ != "" && c.Type != typ {
					continue
				}
				rows = append(rows, contentRow(c))
				if len(rows) >= limit {
					break
				}
			}
			return map[string]any{"count": len(rows), "articles": rows}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "get_article",
		Description: "获取内容详情（含关联 media_assets）",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{"article_id": propNumber("内容 ID")}, "article_id"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			id, ok := argInt64(args, "article_id")
			if !ok || id <= 0 {
				return nil, fmt.Errorf("article_id required")
			}
			c, err := h.db.GetContent(id)
			if err != nil {
				return nil, err
			}
			media, _ := h.db.ListMediaByContent(id)
			mrows := make([]map[string]any, 0, len(media))
			for _, m := range media {
				mrows = append(mrows, map[string]any{
					"id": m.ID, "kind": m.Kind, "path": m.FilePath, "mime": m.MimeType,
				})
			}
			row := contentRow(*c)
			row["media"] = mrows
			return row, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "create_article",
		Description: "创建文章内容（Markdown → body；写入 SQLite contents）",
		SideEffect:  "write",
		InputSchema: schemaObject(map[string]any{
			"title":    propString("标题"),
			"markdown": propString("Markdown 正文"),
			"desc":     propString("摘要 → caption"),
		}, "title", "markdown"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			title := argString(args, "title", "")
			md := argString(args, "markdown", "")
			if title == "" || md == "" {
				return nil, fmt.Errorf("title and markdown required")
			}
			c, err := h.db.CreateContent("article", title, argString(args, "desc", ""), md, "[]", "draft")
			if err != nil {
				return nil, err
			}
			return map[string]any{"ok": true, "article_id": c.ID, "type": c.Type, "title": c.Title}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "create_graph_text",
		Description: "创建图文内容；files 为本地图片路径数组",
		SideEffect:  "write",
		InputSchema: schemaObject(map[string]any{
			"title": propString("标题"),
			"desc":  propString("描述"),
			"files": propArray("图片绝对路径"),
		}, "title"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return h.createMediaContent(args, "graph_text", false)
		},
	})

	s.AddTool(ToolDef{
		Name:        "create_video",
		Description: "创建视频/Reel 内容；files 为本地视频路径数组",
		SideEffect:  "write",
		InputSchema: schemaObject(map[string]any{
			"title": propString("标题"),
			"desc":  propString("描述"),
			"files": propArray("视频绝对路径"),
		}, "title", "files"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return h.createMediaContent(args, "video", true)
		},
	})

	s.AddTool(ToolDef{
		Name:        "update_article",
		Description: "更新已有内容",
		SideEffect:  "write",
		InputSchema: schemaObject(map[string]any{
			"article_id": propNumber("内容 ID"),
			"title":      propString("标题"),
			"markdown":   propString("正文"),
			"desc":       propString("caption"),
			"status":     propString("draft|ready"),
		}, "article_id"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			id, ok := argInt64(args, "article_id")
			if !ok || id <= 0 {
				return nil, fmt.Errorf("article_id required")
			}
			err := h.db.UpdateContent(id,
				argString(args, "title", ""),
				argString(args, "desc", ""),
				argString(args, "markdown", ""),
				"",
				argString(args, "status", "draft"),
			)
			if err != nil {
				return nil, err
			}
			return map[string]any{"ok": true, "article_id": id}, nil
		},
	})

	for _, name := range []struct {
		tool, ctype, desc string
	}{
		{"publish_article", "article", "创建文章发布任务。默认演练；具体自动化、API 或人工接管边界见 list_platforms"},
		{"publish_graph_text", "graph_text", "创建图文发布任务。默认演练；具体自动化、API 或人工接管边界见 list_platforms"},
		{"publish_video", "video", "创建视频/Reel 发布任务。默认演练；具体自动化、API 或人工接管边界见 list_platforms"},
	} {
		ctype := name.ctype
		s.AddTool(ToolDef{
			Name:        name.tool,
			Description: name.desc,
			SideEffect:  "write",
			InputSchema: schemaObject(map[string]any{
				"article_id":      propNumber("内容 ID"),
				"account_id":      propNumber("账号 ID"),
				"target_id":       propNumber("account_asset ID；账号只有一个匹配资产时可省略"),
				"platform":        propString("list_platforms 返回的平台 code"),
				"template_id":     propNumber("同平台且已启用的发布模板 ID"),
				"dry_run":         propBool("默认 true"),
				"auto_submit":     propBool("仅 readiness 允许的平台可尝试自动提交"),
				"idempotency_key": propString("dry_run=false 时必填；同一逻辑请求重试必须复用"),
				"confirm":         propBool("创建持久化任务必须 true"),
			}, "article_id", "account_id"),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return h.publishContent(ctx, args, ctype)
			},
		})
	}

	s.AddTool(ToolDef{
		Name:        "list_records",
		Description: "获取发布记录列表",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{"limit": propNumber("默认 30")}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			limit := 30
			if n, ok := argInt64(args, "limit"); ok && n > 0 {
				limit = int(n)
			}
			recs, err := h.db.ListPublishRecords(limit)
			if err != nil {
				return nil, err
			}
			rows := make([]map[string]any, 0, len(recs))
			for _, r := range recs {
				rows = append(rows, map[string]any{
					"id": r.ID, "job_id": r.JobID, "content_id": r.ContentID,
					"account_id": r.AccountID, "platform": r.PlatformCode,
					"type": r.ContentType, "status": r.Status, "post_url": r.PostURL,
				})
			}
			return map[string]any{"count": len(rows), "records": rows}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "get_record_info",
		Description: "获取发布记录详情 + record_infos",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{"record_id": propNumber("publish_records.id")}, "record_id"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			id, ok := argInt64(args, "record_id")
			if !ok || id <= 0 {
				return nil, fmt.Errorf("record_id required")
			}
			recs, err := h.db.ListPublishRecords(200)
			if err != nil {
				return nil, err
			}
			var found *db.PublishRecord
			for i := range recs {
				if recs[i].ID == id {
					found = &recs[i]
					break
				}
			}
			if found == nil {
				return nil, fmt.Errorf("record %d not found", id)
			}
			infos, _ := h.db.ListRecordInfosByRecord(id)
			infoRows := make([]map[string]any, 0, len(infos))
			for _, ri := range infos {
				infoRows = append(infoRows, map[string]any{
					"id": ri.ID, "account_id": ri.AccountID, "platform": ri.PlatformCode,
					"link": ri.Link, "success": ri.Success, "reason": ri.Reason,
				})
			}
			return map[string]any{
				"record": map[string]any{
					"id": found.ID, "content_id": found.ContentID, "account_id": found.AccountID,
					"platform": found.PlatformCode, "status": found.Status, "post_url": found.PostURL,
				},
				"infos": infoRows,
			}, nil
		},
	})

	s.AddTool(ToolDef{
		Name:        "ego_space_list",
		Description: "列出账号绑定的 EgoLite Space 与接管状态",
		SideEffect:  "read",
		InputSchema: schemaObject(map[string]any{}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return h.callTool(ctx, "ego.space.list", args)
		},
	})
}

func (h *handlers) createMediaContent(args map[string]any, contentType string, requireFiles bool) (any, error) {
	title := argString(args, "title", "")
	if title == "" {
		return nil, fmt.Errorf("title required")
	}
	desc := argString(args, "desc", "")
	files := asStringSlice(args["files"])
	if requireFiles && len(files) == 0 {
		return nil, fmt.Errorf("files required")
	}
	ctype := contentType
	if contentType == "video" {
		ctype = "reel"
	}
	c, err := h.db.CreateContent(ctype, title, desc, desc, "[]", "draft")
	if err != nil {
		return nil, err
	}
	mediaIDs := []int64{}
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(h.dataDir, "media", f)
		}
		kind := "image"
		low := strings.ToLower(f)
		if contentType == "video" || strings.HasSuffix(low, ".mp4") || strings.HasSuffix(low, ".mov") {
			kind = "video"
		}
		m, err := h.db.CreateMediaAsset(c.ID, kind, f, f, "", 0, "", 0, 0, 0)
		if err != nil {
			return nil, err
		}
		mediaIDs = append(mediaIDs, m.ID)
	}
	return map[string]any{
		"ok": true, "article_id": c.ID, "type": c.Type, "title": c.Title, "media_ids": mediaIDs,
	}, nil
}

func (h *handlers) publishContent(ctx context.Context, args map[string]any, contentType string) (any, error) {
	articleID, ok := argInt64(args, "article_id")
	if !ok || articleID <= 0 {
		return nil, fmt.Errorf("article_id required")
	}
	accountID, ok := argInt64(args, "account_id")
	if !ok || accountID <= 0 {
		return nil, fmt.Errorf("account_id required")
	}
	content, err := h.db.GetContent(articleID)
	if err != nil {
		return nil, err
	}
	if !friendlyContentTypeMatches(content.Type, contentType) {
		return nil, fmt.Errorf("CONTENT_TYPE_MISMATCH: tool expects %s, content %d is %s", contentType, articleID, content.Type)
	}
	account, err := h.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	platform := normalizeFriendlyPlatform(argString(args, "platform", ""))
	accountPlatform := normalizeFriendlyPlatform(account.PlatformCode)
	if platform == "" && accountPlatform != "meta" {
		platform = accountPlatform
	}
	if platform != "" && accountPlatform != "meta" && accountPlatform != platform {
		return nil, fmt.Errorf("ACCOUNT_PLATFORM_MISMATCH: account %d is %s, requested %s", accountID, accountPlatform, platform)
	}

	dry := argBool(args, "dry_run", true)
	confirm := argBool(args, "confirm", false)
	if !dry && !confirm {
		return nil, fmt.Errorf("live publish requires confirm=true (and dry_run=false)")
	}
	idempotencyKey := strings.TrimSpace(argString(args, "idempotency_key", ""))
	if !dry && idempotencyKey == "" {
		return nil, fmt.Errorf("IDEMPOTENCY_KEY_REQUIRED: live publish requires a stable idempotency_key")
	}
	targetID, _ := argInt64(args, "target_id")
	var target *db.AccountAsset
	if targetID > 0 {
		target, err = h.db.GetAccountAsset(targetID)
		if err != nil {
			return nil, err
		}
		if target.AccountID != accountID {
			return nil, fmt.Errorf("TARGET_ACCOUNT_MISMATCH: asset %d belongs to account %d, not %d", targetID, target.AccountID, accountID)
		}
		if !target.Enabled {
			return nil, fmt.Errorf("TARGET_ASSET_DISABLED: asset %d is disabled", targetID)
		}
		targetPlatform := normalizeFriendlyPlatform(target.PlatformCode)
		if platform == "" {
			platform = targetPlatform
		}
		if targetPlatform != platform {
			return nil, fmt.Errorf("TARGET_PLATFORM_MISMATCH: asset %d is %s, requested %s", targetID, targetPlatform, platform)
		}
	} else {
		assets, err := h.db.ListAccountAssetsByAccount(accountID)
		if err != nil {
			return nil, err
		}
		candidates := make([]db.AccountAsset, 0, len(assets))
		for _, asset := range assets {
			if !asset.Enabled {
				continue
			}
			if platform == "" || normalizeFriendlyPlatform(asset.PlatformCode) == platform {
				candidates = append(candidates, asset)
			}
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("TARGET_ASSET_REQUIRED: account %d has no enabled asset for platform %s", accountID, platform)
		}
		if len(candidates) > 1 {
			return nil, fmt.Errorf("TARGET_ASSET_REQUIRED: account %d has %d matching assets; choose target_id explicitly", accountID, len(candidates))
		}
		target = &candidates[0]
		targetID = target.ID
		if platform == "" {
			platform = normalizeFriendlyPlatform(target.PlatformCode)
		}
	}
	readiness := platforms.Readiness(platform)
	if !readiness.CanCreateJob {
		return nil, fmt.Errorf("PLATFORM_PUBLISH_UNAVAILABLE: %s", platform)
	}
	callArgs := map[string]any{
		"platform": platform, "content_id": articleID, "target_id": targetID, "dry_run": dry,
		"auto_submit": argBool(args, "auto_submit", false), "confirm": confirm,
	}
	if idempotencyKey != "" {
		callArgs["idempotency_key"] = idempotencyKey
	}
	if templateID, ok := argInt64(args, "template_id"); ok && templateID > 0 {
		callArgs["template_id"] = templateID
	}
	res, err := h.callTool(ctx, "publish.run", callArgs)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "content_type": contentType, "tool_result": res, "dry_run": dry}, nil
}

func normalizeFriendlyPlatform(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "xiaohongshu" {
		return "xhs"
	}
	return code
}

func friendlyContentTypeMatches(actual, expected string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	switch expected {
	case "article":
		return actual == "article"
	case "graph_text":
		return actual == "graph_text" || actual == "graph" || actual == "image"
	case "video":
		return actual == "video" || actual == "reel"
	default:
		return false
	}
}

func accountRows(accounts []db.Account) []map[string]any {
	rows := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		rows = append(rows, map[string]any{
			"id": a.ID, "platform": a.PlatformCode, "name": a.Name,
			"login_state": a.LoginState, "group": a.AccountGroup, "remark": a.Remark,
		})
	}
	return rows
}

func contentRow(c db.Content) map[string]any {
	return map[string]any{
		"id": c.ID, "article_id": c.ID, "type": c.Type, "title": c.Title,
		"caption": c.Caption, "body": c.Body, "status": c.Status,
		"publish_count": c.PublishCount, "publish_type": publishTypeOf(c.Type),
	}
}

func publishTypeOf(t string) int {
	switch t {
	case "article":
		return 1
	case "graph_text", "graph", "image":
		return 2
	case "video", "reel":
		return 3
	default:
		return 0
	}
}

func asStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			out = append(out, fmt.Sprint(x))
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	default:
		return nil
	}
}
