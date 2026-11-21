# 深度补强 · Wave 3（后端审计驱动）

分支：`feat/polish-wave-1`（承接 Wave 2）
日期：2026-11-21

本轮由一次完整后端架构审计驱动。审计方式为只读，全部结论都在本机真实库
（`~/.alpha-nexus-egolite/app.db`，131MB）或临时副本上实测，未修改项目文件。

## 1. 已修（每项都有实测数字）

### 1.1 账号数据页与概览页口径不一致

`GetMetricSummary`（账号数据页）的「每账号最新一天」子查询缺 `window_days` 与
`range_verified` 约束，而 `CreatorMetricOverview`（概览页）两个都有。schema 的
`UNIQUE(account_id, snapshot_date, window_days)` 明确允许 1/7/30/90 天并存，
所以同一账号同一天可以有多行。

实测：往副本插一条 `range_verified=0, views=999999` 的 profile 行，summary 从
997 变成 1000996，概览页仍是 997。

修法：两个子查询都加 `WHERE window_days=7 AND range_verified=1`，`window_days`
引用包内已有的 `creatorMetricWindowDays` 常量而非硬编码字面量。

### 1.2 关注数快照会清零当天的真实浏览

`saveCreatorFollowerSnapshot` 以 `range_verified=false` 写 profile 快照，而
UPSERT 的 `DO UPDATE SET` 无条件覆盖 `views/likes/comments/shares/favorites/posts`。

实测（真实库副本）：账号 10 (csdn) 11-21 原有 `range_verified=1, views=994`，
写入 profile 快照后变成 `range_verified=0, views=0`——当天 994 次浏览被清零，
而 `creator_metric_collection_states` 仍显示 `observed / range_verified=1`，
状态与数据自相矛盾。

修法：指标列改成 `CASE WHEN excluded.range_verified=1 OR 库里这行本来就未验证
THEN excluded ELSE 保留原值 END`。关注数与 raw_json 照常更新（那是 profile
快照自己的产出）。这样：
- 已验证行不会被未验证行覆盖；
- 未验证行之间仍能互相覆盖（第二次 profile 更新不丢关注数）。

三条回归测试固定在 `internal/db/creator_metrics_test.go`。

### 1.3 Migrate 每次启动重放全部 DDL

`Migrate` 在真实库上冷启要 433~516ms，其中 226 条 DDL、78 次
`PRAGMA table_info`、19 条无条件 UPDATE 全在一个事务里。每条 CLI 命令、每次
Bot 工具调用都会重跑一遍。

修法：用 `PRAGMA user_version` 记录 DDL 版本，并做一次轻量结构哨兵检查
（三张哨兵表 + 一个索引 + 一处 CHECK 枚举）。都对得上才跳过 DDL。

刻意保留每次都跑的部分：数据回填与清洗语句全部带 `WHERE 条件不满足`，
是幂等收敛，删掉会让老库永远修不了历史脏数据。

实测（真实库副本，连跑三次）：

| | 改前 | 改后 |
| --- | --- | --- |
| 冷启 | 433~516ms | 38.6ms |
| 热启 | 47~53ms | 1.9ms |

顺带修了 `reconcileKnowledgeLatestSources`：原先无条件
`DELETE FROM knowledge_chunks_fts WHERE document_id IN (子查询)`，而 FTS 表的
`document_id` 无索引，实测冷启 331ms 全扫 19040 行、结果恒 0 行。改为先查出
stale id 列表，为空就直接返回。同时把它挪到建
`idx_knowledge_documents_ready_source` 之前——否则老库里并存的 ready 行会让
唯一索引建失败（这是原本就存在的顺序缺陷，只在有脏数据的库上暴露）。

### 1.4 账号列表每秒钟的 N+1

`GET /api/accounts` 被账号页以 1 秒间隔轮询。循环内每个账号调一次
`GetEgoSpaceByAccount`（内部 2 条 SQL：先查 account 只为拿 identity_id）和一次
`LatestAccountLoginEvent`。27 账号 = 54 次额外查询。

修法：新增 `EgoSpacesByIdentity()` 与 `LatestAccountLoginEvents()` 两个整表查询，
handler 里按 id 索引。查询数从 2N 降到 2。

### 1.5 部分索引完全没被用上

`idx_publish_jobs_idempotency` 是部分索引
`WHERE idempotency_key IS NOT NULL AND idempotency_key <> ''`，而三处 Go 查询
只写 `WHERE idempotency_key=?`。参数化后 SQLite 无法证明蕴含谓词。

实测（`EXPLAIN QUERY PLAN`）：加谓词前 `SCAN publish_jobs`，加后
`SEARCH publish_jobs USING COVERING INDEX idx_publish_jobs_idempotency (idempotency_key=?)`。

### 1.6 落库失败静默丢弃

`publish_plan_handlers.go` 里 5 处 `_, _ = db.Exec(UPDATE publish_plan_targets /
publish_plans)` 连返回值都不看。写库失败时内存里显示新状态，刷新就回退，
而调用方拿到的是「成功」。

改为收集 `persistErrs`，有错就返回 `EGO_PREPARE_PERSIST_FAILED` /
`EGO_SUBMIT_PERSIST_FAILED` 并把错误明细放进 `persist_errors`。发布结果落库失败
比任何单平台结果都严重，所以排在 `result_unknown` / `partial_failed` 之前判定。

同类修复：
- 通知投递失败（`notify/service.go`）现在写日志，此前 `_, _ =` 直接丢弃；
- `resumeAIMediaGenerationJobs` 标记失败失败会写日志，此前任务永远停在 running；
- `UpsertShopContentMedia` 回查 id 失败会返回错误，此前用 id=0 继续读详情；
- `CreateAccountForIdentity` 回滚删除失败会连同原错误一起报出，此前库里留孤儿行。

### 1.7 `health.Nonce` 的数据竞争

`server.go:417` 在后台 goroutine 里裸写 `s.health.Nonce`，而
`shop_listing_handlers.go` 与 `publish_plan_handlers.go` 裸读。加了
`healthSnapshot()` 方法，写端与两个读点都走 `serverMu`。
`go test -race` 通过。

### 1.8 多进程同时启动会因数据库锁直接退出

实测：4 个进程同时 `db.Open` + `Migrate` 同一个数据目录，**3 个以
`database is locked (5) (SQLITE_BUSY)` 退出**。桌面端与 CLI 同时操作就会撞上。

两处根因：

1. **deferred 事务的锁升级不走 `busy_timeout`**。默认 `BEGIN` 只拿读锁，等第一条
   写语句才升级；SQLite 在升级时发现别人持有写锁会**直接**返回 BUSY，不等待。
   迁移恰好是先读 `PRAGMA user_version` 再写 DDL。修法是在 DSN 加
   `_txlock=immediate`，让事务开头就取写锁，此时 `busy_timeout` 正常生效。
2. **已处于 WAL 的库仍在重设 `journal_mode`**。切换 WAL 需要独占锁，并发打开时
   后到的拿不到。改为先读 `PRAGMA journal_mode`，已是 WAL 就直接返回；确实要切
   时做 5 秒内的有限重试（`journal_mode` 切换不认 `busy_timeout`）。

修后实测：并发 4 进程失败数 3/4 → 0/4，连跑 5 次稳定。
回归测试：`TestMigrateIsSafeUnderConcurrentOpen`。

### 1.9 前端轮询两个永远 404 的接口

实测 `curl http://127.0.0.1:17421/api/login-sessions` → 404，
`/api/account-observer/status` → 404。

这两个都不是 bug，是有意设计：`internal/ui/browser_config_test.go` 里
`TestLegacyBrowserConfigurationRoutesAreUnavailable` 明确断言它们必须 404
（`legacy_chromium` 之外的构建不提供）。但前端没有同步下线：

- `BrowserWorkspaceView` 每 2 秒轮询 `/api/login-sessions`。它想恢复的「批量
  登录进行中」状态其实由 `/api/ego/identities/{id}/login-batches` 与本地
  `activeBatches` 维护，这条轮询纯属多余。已删除，连带删掉 `api.ts` 里零调用的
  `getLoginSessions` / `cancelLoginSession`。
- `RuntimeMonitorView`（`/monitor` 路由，真实可达）拉 `/api/account-observer/status`
  。同一页面已经在拉 `/api/ego/session-monitor`，两者语义重复。已改用后者。

对应的两条源码断言测试同步更新为「不再调用该端点」。

## 2. 审计发现但本轮不动（附理由）

### 2.1 `record_infos` / `post_metrics` / `creator_post_metric_snapshots` 恒为空

`CreateRecordInfo`（`repository.go:3839`）全仓库无生产调用者，所以 MCP 的
`get_record_info` 永远返回 `infos: []`。`post_metrics` 只由 CLI 手工命令写入，
而账号数据页读的是 `creator_metric_snapshots`，写了也看不见。

**不动理由**：这不是「查错表」，是这三张表对应的采集链路本身没接通
（`creator_post_metric_snapshots` 只认 `REVIEWED_CONTENT_*` 证据，实际采集全是
`REVIEWED_ANALYTICS_DOM`）。接通需要真实账号逐个验收，属长期项。

### 2.2 全仓 13 个前端仍在调用但后端 404 的端点

实测清单：`/api/browsers`、`/api/proxies`、`/api/fleet/*`、
`/api/account-observer/status`（已修）、`/api/config/update`、
`/api/config/browsers`、`/api/batch/publish`、`/api/browser/ai-chat`、
`/api/browser/tabs`、`/api/browser/layout`、`/api/accounts/verify`、
`/api/accounts/set-engine`、`/api/accounts/*/session-backups`、
`/api/accounts/1/assets/discover`、`/api/data/collect`。

**不动理由**：这些调用点全部落在零引用组件里（`AccountsView.tsx` 2558 行、
`ProxyPoolView.tsx`、`SettingsView.tsx` 里除 `LLMConfigPanel` 外的部分）。
用户碰不到，删除 `api.ts` 方法会让这些死组件编译失败，改造收益为零。
真要清理，应该整组件删除一次性完成，而不是删方法。

`AccountsView.tsx` 里还有三个 fingerprint 接口
（`/api/accounts/set-fingerprint`、`/set-fingerprint-config`、`/smart-fingerprint`）
同样未注册——同名 handler 在 `server.go` 里存在但从未注册路由。同样不可达。

### 2.3 巨型文件拆分

审计给出了逐行边界，收益最大的三处：

| 文件 | 行数 | 其中 raw string JS | 建议 |
| --- | --- | --- | --- |
| `internal/egolite/publish_runtime.go` | 3094 | 2846（92%） | JS 抽到 `go:embed`，Go 缩到 ~200 行 |
| `internal/db/repository.go` | 5169 | 0 | 按实体拆 19 个文件，纯机械操作 |
| `internal/egolite/service.go` | 5507 | 1878（34%） | 拆 7 个文件 |
| `internal/ui/server.go` | 2888 | 0 | 拆 routes/boot/accounts/browser_config/helpers |
| `internal/db/db.go` | 2807 | 0 | 拆 schema/migrate/open |

**不动理由**：纯机械移动，零功能收益，但会制造巨大 diff 并打断其他分支的合并。
本轮优先修行为缺陷。建议单独开一个纯重构分支做。

### 2.4 缺索引（本地数据量下无感）

| 表 | 缺失索引 | 本地现状 |
| --- | --- | --- |
| notifications | (read)、(related_type, related_id) | 17 行 |
| browser_events | (profile_id, pid, created_at) | 0 行 |
| content_versions | (content_id) | 无生产调用者 |
| credentials | (account_id, platform_code) | — |
| publish_content_records | (updated_at DESC, id DESC) | — |
| publish_records | (id DESC) | — |

**不动理由**：真实数据量下全部是毫秒级。加了只会增加写入成本。等数据长起来再按
`EXPLAIN QUERY PLAN` 的实测结果加，而不是凭表名猜。

### 2.5 `handleAPIEgoSpaceScreen` 的全局锁

`ego_handlers.go:747` 第二次 `Lock()` 之后 `defer Unlock()` 一直持有到函数结束，
中间是 `egoRuntime.Run(...)`（起 node 子进程 + 截图 + JPEG 压缩）。所以所有
Space 的截图请求被全局串行，注释说 per Space，实际是全局。

**不动理由**：设计意图是防并发 spawn，改成细粒度锁需要先证明同一 Space 的并发
截图是安全的。当前最大并发 Space 是 3，串行代价可接受。

## 3. 验收口径

- `go build ./...`、`go vet ./internal/...` 干净
- `go test -p 1 ./internal/...` 51 个包全绿
- `node --test ui-web/test/*.test.mjs` 377 项全通过
- `pnpm run build` 通过
- 每项修复附改动前后的真实数字
