package feishu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config is the local Feishu Base binding. It contains only the app token and
// table ids: app secrets and access tokens stay in lark-cli's own store.
type Config struct {
	AppToken string            `json:"app_token"`
	URL      string            `json:"url"`
	Tables   map[string]string `json:"tables"`
}

func LoadConfig(dataDir string) (*Config, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "feishu.json"))
	if err != nil {
		return nil, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse feishu config: %w", err)
	}
	if strings.TrimSpace(config.AppToken) == "" || len(config.Tables) == 0 {
		return nil, fmt.Errorf("feishu config is incomplete")
	}
	return &config, nil
}

type ModuleStatus struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	TableID string `json:"table_id"`
	Synced  int    `json:"synced"`
	// LocalTotal 是本地可同步的总行数，Synced 只是已经镜像过去的那部分。
	// 两者相差说明这一轮被 LIMIT 截断了——界面必须把差额说出来，否则用户
	// 会以为飞书里的就是本地全量。
	LocalTotal int `json:"local_total"`
	// CapsAt 是单轮同步的上限；0 表示这个模块不截断。
	CapsAt int `json:"caps_at,omitempty"`
}

type SyncStatus struct {
	Configured  bool           `json:"configured"`
	URL         string         `json:"url,omitempty"`
	AppToken    string         `json:"app_token,omitempty"`
	Modules     []ModuleStatus `json:"modules"`
	Syncing     bool           `json:"syncing"`
	LastResults []Result       `json:"last_results"`
	LastError   string         `json:"last_error,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type Result struct {
	Module  string `json:"module"`
	Created int    `json:"created"`
	Updated int    `json:"updated"`
	Skipped int    `json:"skipped"`
	Error   string `json:"error,omitempty"`
}

// pendingItem is one local row waiting to become a Feishu record. key is the
// local primary key the feishu_sync mapping remembers.
type pendingItem struct {
	key      string
	fields   map[string]any
	hash     string
	recordID string
}

type moduleDef struct {
	ID    string
	Label string
	Query string
	Build func(*sql.Rows) (string, map[string]any, error)
	// Collect reads rows that are not a single SQL shape, such as per-bot
	// memory Markdown files. When set it replaces Query/Build.
	Collect func(ctx context.Context, db *sql.DB, dataDir string) ([]pendingItem, error)
	// Total counts how many rows this module *could* sync. It must not reuse
	// Query, because that one carries the LIMIT that caps a single pass.
	//
	// 没有它，界面只知道「已同步 300 条」——用户会以为云端就是本地全量，
	// 而实际本地有两千多条知识文档永远进不去。空字符串表示无法计数。
	Total string
}

// offsetQuery 在模块 Query 的 LIMIT 之后追加 OFFSET。
// 没有 LIMIT 的模块无法安全偏移（会跨轮重复读同一批），原样返回。
func offsetQuery(module moduleDef, offset int) string {
	if offset <= 0 {
		return module.Query
	}
	upper := strings.ToUpper(module.Query)
	index := strings.LastIndex(upper, " LIMIT ")
	if index < 0 {
		return module.Query
	}
	return module.Query[:index] + fmt.Sprintf(" LIMIT %d OFFSET %d", module.capPerRun(), offset)
}

// capPerRun 从模块自带的 Query 里读出 LIMIT，用于告诉界面「单轮最多多少条」。
//
// 硬编码一份上限常量会和 Query 漂移，所以直接解析已有的 SQL——它是这个包
// 里唯一的事实来源。没有 LIMIT 的模块返回 0（不截断）。
func (m moduleDef) capPerRun() int {
	upper := strings.ToUpper(m.Query)
	index := strings.LastIndex(upper, " LIMIT ")
	if index < 0 {
		return 0
	}
	rest := strings.TrimSpace(m.Query[index+len(" LIMIT "):])
	digits := rest
	for i, r := range rest {
		if r < '0' || r > '9' {
			digits = rest[:i]
			break
		}
	}
	value, err := strconv.Atoi(digits)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

var modules = []moduleDef{
	{
		ID: "gtm", Label: "冠英 GTM 全链路（选型/线索/SEO）",
		Total: "SELECT (SELECT COUNT(1) FROM guanying_leads) + (SELECT COUNT(1) FROM guanying_seo_pages)",
		Collect: func(ctx context.Context, db *sql.DB, _ string) ([]pendingItem, error) {
			items := make([]pendingItem, 0)
			lRows, err := db.QueryContext(ctx, `SELECT id, company, country, channel, buyer_type, confidence, matched_code, status, created_at FROM guanying_leads ORDER BY created_at DESC LIMIT 100`)
			if err == nil {
				for lRows.Next() {
					var id, company, country, channel, buyerType, matchedCode, status, at string
					var conf int
					if scanErr := lRows.Scan(&id, &company, &country, &channel, &buyerType, &conf, &matchedCode, &status, &at); scanErr == nil {
						stage := "信号"
						feishuStatus := "进行中"
						if status == "已出报价单" {
							stage = "渠道"
							feishuStatus = "已完成"
						} else if status == "已过滤噪音" {
							feishuStatus = "阻塞"
						}
						items = append(items, pendingItem{
							key: "lead_" + id,
							fields: map[string]any{
								"GTM 任务": "[线索] " + company + " (" + buyerType + ")",
								"阶段":     stage,
								"市场":     country,
								"负责人":    "跨时差询盘捕手 / 终端猎手",
								"状态":     feishuStatus,
								"渠道":     channel,
								"关键指标":   "终端置信度(%) / 匹配:" + matchedCode,
								"目标值":    85,
								"当前值":    conf,
								"下一步":    "核对尺寸/换桥风险并发送双语报价单",
							},
						})
					}
				}
				_ = lRows.Close()
			}
			sRows, err := db.QueryContext(ctx, `SELECT id, title, target_market, lang, inquiry_count FROM guanying_seo_pages ORDER BY created_at DESC LIMIT 50`)
			if err == nil {
				for sRows.Next() {
					var id, title, market, lang string
					var cnt int
					if scanErr := sRows.Scan(&id, &title, &market, &lang, &cnt); scanErr == nil {
						items = append(items, pendingItem{
							key: "seo_" + id,
							fields: map[string]any{
								"GTM 任务": "[SEO建站] " + title,
								"阶段":     "内容",
								"市场":     market,
								"负责人":    "外贸独立站建站师",
								"状态":     "已完成",
								"渠道":     "Kooying 独立站 (" + lang + ")",
								"关键指标":   "着陆页直接询盘数",
								"目标值":    10,
								"当前值":    cnt,
								"下一步":    "持续扩展长尾 WVA 与车桥着陆页",
							},
						})
					}
				}
				_ = sRows.Close()
			}
			return items, nil
		},
	},
	{
		ID: "accounts", Label: "账号",
		Query: `SELECT id, platform_code, name, account_identifier, login_state, COALESCE(last_check_at,'') FROM accounts ORDER BY updated_at DESC LIMIT 300`,
		Total: "SELECT COUNT(1) FROM accounts",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id int64
			var platform, name, identifier, state, updated string
			if err := rows.Scan(&id, &platform, &name, &identifier, &state, &updated); err != nil {
				return "", nil, err
			}
			label := name
			if label == "" {
				label = identifier
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"名称": label, "平台": platform, "状态": state, "账号 ID": identifier, "更新时间": updated,
			}, nil
		},
	},
	{
		ID: "contents", Label: "内容",
		Query: `SELECT id, title, source, status, COALESCE(updated_at,'') FROM contents ORDER BY updated_at DESC LIMIT 300`,
		Total: "SELECT COUNT(1) FROM contents",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id int64
			var title, source, status, updated string
			if err := rows.Scan(&id, &title, &source, &status, &updated); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"标题": title, "平台": source, "状态": status, "账号": "", "更新时间": updated,
			}, nil
		},
	},
	{
		ID: "knowledge", Label: "知识库",
		Query: `SELECT id, source_name, COALESCE(original_path,''), COALESCE(updated_at,'') FROM knowledge_documents ORDER BY updated_at DESC LIMIT 300`,
		Total: "SELECT COUNT(1) FROM knowledge_documents",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id int64
			var title, source, updated string
			if err := rows.Scan(&id, &title, &source, &updated); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"标题": title, "来源": source, "分块数": 0, "更新时间": updated,
			}, nil
		},
	},
	{
		ID: "radar", Label: "趋势雷达",
		Query: `SELECT id, title, platform, author, url, COALESCE(published_at,''), COALESCE(last_seen_at,'') FROM radar_works ORDER BY last_seen_at DESC LIMIT 300`,
		Total: "SELECT COUNT(1) FROM radar_works",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id int64
			var title, platform, author, url, published, seen string
			if err := rows.Scan(&id, &title, &platform, &author, &url, &published, &seen); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"标题": title, "平台": platform, "作者": author, "链接": url, "发布时间": published, "抓取时间": seen,
			}, nil
		},
	},
	{
		ID: "memory", Label: "认知记忆",
		// 内存模块读的是每个 Bot 的 Markdown 记忆文件，行数等于 Bot 数。
		Total: "SELECT COUNT(1) FROM agent_bots",
		Collect: func(ctx context.Context, db *sql.DB, dataDir string) ([]pendingItem, error) {
			rows, err := db.QueryContext(ctx, `SELECT id, name FROM agent_bots ORDER BY pinned DESC, name`)
			if err != nil {
				return nil, err
			}
			type bot struct{ id, name string }
			var bots []bot
			for rows.Next() {
				var item bot
				if err := rows.Scan(&item.id, &item.name); err != nil {
					_ = rows.Close()
					return nil, err
				}
				bots = append(bots, item)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return nil, err
			}
			_ = rows.Close()

			items := make([]pendingItem, 0, len(bots))
			for _, item := range bots {
				if !strings.HasPrefix(item.id, "bot_") || strings.ContainsAny(item.id, `/\`) {
					continue
				}
				path := filepath.Join(dataDir, "bot-memory", item.id+".md")
				raw, readErr := os.ReadFile(path)
				if readErr != nil {
					continue
				}
				text := strings.TrimSpace(string(raw))
				if text == "" {
					continue
				}
				if runes := []rune(text); len(runes) > 4000 {
					text = string(runes[:4000])
				}
				info, _ := os.Stat(path)
				updated := ""
				if info != nil {
					updated = info.ModTime().UTC().Format(time.RFC3339)
				}
				items = append(items, pendingItem{key: item.id, fields: map[string]any{
					"Bot": item.name, "内容": text, "更新时间": updated,
				}})
			}
			return items, nil
		},
	},
	{
		ID: "calendar", Label: "内容日历",
		Query: `SELECT id, title, COALESCE(planned_at,''), COALESCE(source,''), status FROM contents WHERE COALESCE(planned_at,'') <> '' ORDER BY planned_at DESC LIMIT 300`,
		Total: "SELECT COUNT(1) FROM contents WHERE COALESCE(planned_at,'') <> ''",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id int64
			var title, planned, platform, status string
			if err := rows.Scan(&id, &title, &planned, &platform, &status); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"标题": title, "计划时间": planned, "平台": platform, "状态": status,
			}, nil
		},
	},
	{
		ID: "metrics", Label: "账号数据",
		Query: `SELECT id, account_id, platform_code, snapshot_date, window_days, followers, views, likes, comments, shares, COALESCE(collected_at,'') FROM creator_metric_snapshots ORDER BY snapshot_date DESC, id DESC LIMIT 300`,
		Total: "SELECT COUNT(1) FROM creator_metric_snapshots",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id, accountID int64
			var platform, date string
			var windowDays, followers, views, likes, comments, shares int
			var collected string
			if err := rows.Scan(&id, &accountID, &platform, &date, &windowDays, &followers, &views, &likes, &comments, &shares, &collected); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"账号": fmt.Sprintf("#%d", accountID), "平台": platform, "日期": date,
				"统计天数": windowDays, "粉丝": followers, "播放": views, "点赞": likes,
				"评论": comments, "转发": shares, "采集时间": collected,
			}, nil
		},
	},
	{
		ID: "captures", Label: "抓取采集",
		Query: `SELECT id, COALESCE(pool_name,''), platform, state, COALESCE(started_at,''), COALESCE(finished_at,''), total_calls, estimated_cost_usd FROM radar_capture_runs ORDER BY id DESC LIMIT 200`,
		Total: "SELECT COUNT(1) FROM radar_capture_runs",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id string
			var calls int64
			var pool, platform, state, started, finished string
			var cost float64
			if err := rows.Scan(&id, &pool, &platform, &state, &started, &finished, &calls, &cost); err != nil {
				return "", nil, err
			}
			return id, map[string]any{
				"任务": pool, "平台": platform, "状态": state, "开始": started,
				"结束": finished, "调用次数": calls, "预估费用": cost,
			}, nil
		},
	},
	{
		ID: "bots", Label: "Bot 团队",
		Query: `SELECT id, name, COALESCE(role,''), COALESCE(model,''), COALESCE(group_name,'') FROM agent_bots ORDER BY is_primary DESC, pinned DESC, name`,
		Total: "SELECT COUNT(1) FROM agent_bots",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id, name, role, model, group string
			if err := rows.Scan(&id, &name, &role, &model, &group); err != nil {
				return "", nil, err
			}
			return id, map[string]any{"名字": name, "角色": role, "模型": model, "分组": group}, nil
		},
	},
	{
		ID: "publish", Label: "发布记录",
		Query: `SELECT id, COALESCE(content_id,0), platform_code, status, COALESCE(post_url,''), COALESCE(published_at, created_at, '') FROM publish_records ORDER BY id DESC LIMIT 300`,
		Total: "SELECT COUNT(1) FROM publish_records",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id, contentID int64
			var platform, status, url, at string
			if err := rows.Scan(&id, &contentID, &platform, &status, &url, &at); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"内容": fmt.Sprintf("#%d", contentID), "平台": platform, "状态": status, "时间": at, "证据": url,
			}, nil
		},
	},
	{
		ID: "logs", Label: "运行日志",
		Query: `SELECT id, component, status, COALESCE(error_code,''), created_at FROM operation_events ORDER BY id DESC LIMIT 200`,
		Total: "SELECT COUNT(1) FROM operation_events",
		Build: func(rows *sql.Rows) (string, map[string]any, error) {
			var id int64
			var component, status, code, at string
			if err := rows.Scan(&id, &component, &status, &code, &at); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%d", id), map[string]any{
				"时间": at, "组件": component, "状态": status, "错误码": code,
			}, nil
		},
	},
}

// fieldsHash fingerprints the record payload so an unchanged row is not
// rewritten while an edited one is pushed. encoding/json sorts map keys, so the
// digest is stable across runs.
func fieldsHash(fields map[string]any) string {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:8])
}

// batchUpdate overwrites existing records in place. The Base API rejects a
// record that does not exist, so the caller treats any error as "retry later".
func batchUpdate(ctx context.Context, binary, appToken, tableID string, records []map[string]any) error {
	body, err := json.Marshal(map[string]any{"records": records})
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/open-apis/bitable/v1/apps/%s/tables/%s/records/batch_update", appToken, tableID)
	callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, binary, "api", "POST", path, "--data", "-")
	cmd.Env = directFeishuEnv()
	cmd.Stdin = bytes.NewReader(body)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lark-cli: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var envelope cliEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return fmt.Errorf("parse lark-cli output: %w", err)
	}
	if !envelope.OK {
		return fmt.Errorf("feishu rejected batch_update: %s", strings.TrimSpace(string(envelope.Error)))
	}
	return nil
}

func resolveLarkCLI() string {
	if value := strings.TrimSpace(os.Getenv("LARK_CLI")); value != "" {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".local", "bin", "lark-cli")
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return path
		}
	}
	if path, err := exec.LookPath("lark-cli"); err == nil {
		return path
	}
	return ""
}

type cliEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error json.RawMessage `json:"error"`
}

func batchCreate(ctx context.Context, binary, appToken, tableID string, records []map[string]any) ([]string, error) {
	wrapped := make([]map[string]any, 0, len(records))
	for _, fields := range records {
		wrapped = append(wrapped, map[string]any{"fields": fields})
	}
	body, err := json.Marshal(map[string]any{"records": wrapped})
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/open-apis/bitable/v1/apps/%s/tables/%s/records/batch_create", appToken, tableID)
	callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, binary, "api", "POST", path, "--data", "-")
	cmd.Env = directFeishuEnv()
	cmd.Stdin = bytes.NewReader(body)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("lark-cli: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var envelope cliEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("parse lark-cli output: %w", err)
	}
	if !envelope.OK {
		return nil, fmt.Errorf("feishu rejected batch_create: %s", strings.TrimSpace(string(envelope.Error)))
	}
	var data struct {
		Records []struct {
			RecordID string `json:"record_id"`
		} `json:"records"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(data.Records))
	for _, record := range data.Records {
		ids = append(ids, record.RecordID)
	}
	return ids, nil
}

// truncateErrorMessage keeps a CLI dump from being echoed whole into the UI.
func truncateErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	const limit = 500
	if len(message) <= limit {
		return message
	}
	cut := limit
	for cut > 0 && message[cut]&0xC0 == 0x80 {
		cut--
	}
	return message[:cut] + "…"
}

var syncState struct {
	sync.Mutex
	running bool
	results []Result
	err     string
}

// StartSync runs one sync in the background so the HTTP request never blocks
// on Feishu latency. Only one sync may run at a time.
func StartSync(db *sql.DB, dataDir, only string) bool {
	syncState.Lock()
	if syncState.running {
		syncState.Unlock()
		return false
	}
	syncState.running = true
	syncState.err = ""
	syncState.Unlock()
	go func() {
		var results []Result
		var err error
		defer func() {
			// A panic must not leave the UI stuck on "syncing" forever.
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("feishu sync panicked: %v", recovered)
			}
			syncState.Lock()
			syncState.running = false
			syncState.results = results
			syncState.err = truncateErrorMessage(err)
			syncState.Unlock()
		}()
		results, err = Sync(context.Background(), db, dataDir, only)
	}()
	return true
}

// Sync pushes local rows into their Feishu tables. It is one-way (local is the
// source of truth) and idempotent through feishu_sync. A module that is not
// configured in feishu.json is skipped, never guessed.
func Sync(ctx context.Context, db *sql.DB, dataDir string, only string) ([]Result, error) {
	config, err := LoadConfig(dataDir)
	if err != nil {
		return nil, err
	}
	binary := resolveLarkCLI()
	if binary == "" {
		return nil, fmt.Errorf("lark-cli not found")
	}
	results := make([]Result, 0, len(modules))
	for _, module := range modules {
		if only != "" && only != "all" && only != module.ID {
			continue
		}
		tableID := strings.TrimSpace(config.Tables[module.ID])
		if tableID == "" {
			results = append(results, Result{Module: module.ID, Error: "table_not_configured"})
			continue
		}
		result, syncErr := syncModule(ctx, db, dataDir, binary, config.AppToken, module, tableID)
		if syncErr != nil {
			result.Error = syncErr.Error()
		}
		results = append(results, result)
	}
	return results, nil
}

// collectModule 读取本轮要镜像的行。
//
// query 允许调用方传入一个带了 OFFSET 的变体：模块的 Query 自带 LIMIT 300，
// 而这 300 条永远是最新的那批，已经同步过的会被逐条跳过，于是更早的行
// **永远**轮不到——本地 2338 条知识文档里有 2000 多条进不了飞书，且没有
// 任何提示。syncModule 因此会在检测到「这一轮被截断」时，用 offset 继续
// 向后取，直到取完全部或本轮额度用尽。
func collectModule(ctx context.Context, db *sql.DB, dataDir string, module moduleDef, query string) ([]pendingItem, error) {
	if module.Collect != nil {
		return module.Collect(ctx, db, dataDir)
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var all []pendingItem
	for rows.Next() {
		key, fields, buildErr := module.Build(rows)
		if buildErr != nil {
			_ = rows.Close()
			return nil, buildErr
		}
		all = append(all, pendingItem{key: key, fields: fields})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// Close before any further query: the pool is capped at one connection, so
	// an open Rows would block every later statement (including the UI status).
	_ = rows.Close()
	return all, nil
}

func syncModule(ctx context.Context, db *sql.DB, dataDir, binary, appToken string, module moduleDef, tableID string) (Result, error) {
	result := Result{Module: module.ID}

	// 已知映射：local_key → 飞书 record_id 与上次写入的字段哈希。
	type mapping struct {
		recordID string
		hash     string
	}
	existing := map[string]struct{ recordID, hash string }{}
	rows, err := db.QueryContext(ctx, `SELECT local_key, record_id, COALESCE(content_hash,'') FROM feishu_sync WHERE module=?`, module.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var key string
		var item mapping
		if err := rows.Scan(&key, &item.recordID, &item.hash); err != nil {
			_ = rows.Close()
			return result, err
		}
		existing[key] = struct{ recordID, hash string }{recordID: item.recordID, hash: item.hash}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, err
	}
	_ = rows.Close()

	// 向后补齐：模块 Query 自带 LIMIT，最新那批永远占住名额。若本轮这一页
	// 的每一行都已经镜像过，就带 OFFSET 继续取下一页，直到取不到新行或
	// 达到 maxPages 上限（防止一次同步把几万行全推出去）。
	const maxPages = 20
	offset := 0
	for page := 0; page < maxPages; page++ {
		batch, collectErr := collectModule(ctx, db, dataDir, module, offsetQuery(module, offset))
		if collectErr != nil {
			return result, collectErr
		}
		if len(batch) == 0 {
			break
		}
		fresh, writeErr := writeModuleBatch(ctx, db, binary, appToken, module, tableID, batch, existing, &result)
		if writeErr != nil {
			return result, writeErr
		}
		// 这一页全部已同步（fresh==0）说明最新区已经被镜像完，继续向后翻；
		// 有新鲜行说明还没消化完，下一轮同步自然会继续。
		if fresh > 0 {
			break
		}
		cap := module.capPerRun()
		if cap == 0 || len(batch) < cap {
			// 没有 LIMIT，或者已经读到表的末尾。
			break
		}
		offset += cap
	}
	return result, nil
}

// writeModuleBatch 把一页数据写进飞书，返回这一页里「真正需要写」的行数。
//
// 返回 0 表示这一页全是已知且未变动的行——调用方据此决定是否继续向后翻。
func writeModuleBatch(
	ctx context.Context, db *sql.DB, binary, appToken string,
	module moduleDef, tableID string, all []pendingItem,
	existing map[string]struct{ recordID, hash string },
	result *Result,
) (int, error) {
	var creates, updates []pendingItem
	for _, item := range all {
		item.hash = fieldsHash(item.fields)
		known, ok := existing[item.key]
		if !ok {
			creates = append(creates, item)
			continue
		}
		// Same fields means the mirror is current; changed fields must be
		// pushed, otherwise an edited local row keeps its old Feishu values.
		if known.recordID == "" || known.hash == item.hash {
			result.Skipped++
			continue
		}
		item.recordID = known.recordID
		updates = append(updates, item)
	}

	for start := 0; start < len(creates); start += 20 {
		end := start + 20
		if end > len(creates) {
			end = len(creates)
		}
		chunk := creates[start:end]
		records := make([]map[string]any, 0, len(chunk))
		for _, item := range chunk {
			records = append(records, item.fields)
		}
		ids, err := batchCreate(ctx, binary, appToken, tableID, records)
		if err != nil {
			return len(creates) + len(updates), err
		}
		if len(ids) != len(chunk) {
			// A short response means Feishu accepted fewer rows than we sent.
			// Writing the mapping anyway would hide the missing rows forever.
			return len(creates) + len(updates), fmt.Errorf(
				"feishu accepted %d of %d records for %s; nothing recorded, retry later",
				len(ids), len(chunk), module.ID)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for index, item := range chunk {
			if _, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO feishu_sync(module,local_key,table_id,record_id,content_hash,synced_at) VALUES(?,?,?,?,?,?)`,
				module.ID, item.key, tableID, ids[index], item.hash, now); err != nil {
				return len(creates) + len(updates), err
			}
			existing[item.key] = struct{ recordID, hash string }{recordID: ids[index], hash: item.hash}
		}
		result.Created += len(chunk)
	}

	for start := 0; start < len(updates); start += 20 {
		end := start + 20
		if end > len(updates) {
			end = len(updates)
		}
		chunk := updates[start:end]
		records := make([]map[string]any, 0, len(chunk))
		for _, item := range chunk {
			records = append(records, map[string]any{"record_id": item.recordID, "fields": item.fields})
		}
		if err := batchUpdate(ctx, binary, appToken, tableID, records); err != nil {
			return len(creates) + len(updates), err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for _, item := range chunk {
			if _, err := db.ExecContext(ctx, `UPDATE feishu_sync SET content_hash=?, synced_at=? WHERE module=? AND local_key=?`,
				item.hash, now, module.ID, item.key); err != nil {
				return len(creates) + len(updates), err
			}
			existing[item.key] = struct{ recordID, hash string }{recordID: item.recordID, hash: item.hash}
		}
		result.Updated += len(chunk)
	}
	return len(creates) + len(updates), nil
}

func SyncStatusFor(ctx context.Context, db *sql.DB, dataDir string) SyncStatus {
	config, err := LoadConfig(dataDir)
	if err != nil {
		reason := "feishu.json 未配置"
		if !os.IsNotExist(err) {
			reason = "feishu.json 读取失败：" + truncateErrorMessage(err)
		}
		return SyncStatus{Configured: false, Error: reason, Modules: []ModuleStatus{}}
	}
	syncState.Lock()
	status := SyncStatus{
		Configured: true, URL: config.URL, AppToken: config.AppToken,
		Syncing: syncState.running, LastResults: append([]Result(nil), syncState.results...), LastError: syncState.err,
	}
	syncState.Unlock()
	for _, module := range modules {
		tableID := strings.TrimSpace(config.Tables[module.ID])
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM feishu_sync WHERE module=?`, module.ID).Scan(&count); err != nil {
			count = 0
		}
		localTotal := 0
		if module.Total != "" {
			if err := db.QueryRowContext(ctx, module.Total).Scan(&localTotal); err != nil {
				localTotal = count
			}
		}
		status.Modules = append(status.Modules, ModuleStatus{
			ID: module.ID, Label: module.Label, TableID: tableID,
			Synced: count, LocalTotal: localTotal, CapsAt: module.capPerRun(),
		})
	}
	return status
}

func directFeishuEnv() []string {
	raw := os.Environ()
	filtered := make([]string, 0, len(raw))
	for _, kv := range raw {
		key, _, _ := strings.Cut(kv, "=")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "http_proxy", "https_proxy", "all_proxy":
			continue
		default:
			filtered = append(filtered, kv)
		}
	}
	return filtered
}
