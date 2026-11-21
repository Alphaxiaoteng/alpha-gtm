# SQLite Schema

## 结论

SQLite 是核心资产库，不是缓存。账号、资产、任务、批量执行、发布记录都必须可恢复。

## 平台枚举

`platforms.code` 必须内置：

```text
instagram, facebook, xhs, douyin, wechat_video, kuaishou, wechat_mp,
youtube, tiktok, x, toutiao, omtencent, weishi, bilibili, acfun,
baijiahao, sina, jianshu, zhihu, juejin, csdn, pdd, custom
```

共 23 个目录项；`PublishCodes()` 包含除 `custom` 外的 22 项。SQL migrations 和运行时 `SeedPlatforms()` 必须保持相同代码集合，回归测试见 `internal/db/migration_parity_test.go`。

`account_assets.asset_type` 不是固定平台枚举。账号创建默认写入：

```text
browser_account
```

平台发现可写入更具体的类型，例如：

```text
instagram_account
facebook_page
xhs_account
```

Facebook / Instagram 的浏览器自动发布不会使用默认 `browser_account` 作为目标：桌面端只允许在对应账号上显式新增 `facebook_page` 或 `instagram_account`。新增目标必须有名称和公开外部 ID，写入 `audit_events`，且不保存密码、Cookie 或令牌。发现流程可以先写入名称；用户以相同名称提交公开外部 ID 时会原地确认该资产。缺少外部 ID 的资产不能启用自动提交。

## 核心表

必须实现：

- `platforms`
- `accounts`
- `browser_profiles`
- `account_assets`
- `platform_settings`
- `contents`
- `media_assets`
- `batch_runs`
- `batch_items`
- `publish_jobs`
- `job_events`
- `publish_records`
- `record_infos`
- `notifications`
- `proxy_endpoints`
- `login_sessions`
- `audit_events`
- `operation_events`（脱敏操作时间线）
- `tikhub_api_calls`（TikHub 调用索引与原始响应路径）
- `post_metrics`
- `content_versions`
- `credentials`
- `media_library`
- `generation_jobs`
- `ai_image_projects`
- `studio_scripts`
- `studio_schedule_handoffs`
- `clip_jobs`
- `app_users`
- `browser_runtime_instances`（多浏览器运行时）
- `profile_leases`（profile 互斥租约）
- `browser_events`（浏览器生命周期事件）
- `agent_conversations`、`agent_messages`、`agent_runs`、`agent_events`
- `agent_provider_sessions`、`agent_approvals`（全局 Agent Hub）
- `agent_bots`（Agent Hub 多 Bot 人格名册）
- `skills`（本地 Skill 目录、审核状态和启停真源）
- `feedback_receipts`（本机匿名反馈查询凭证与非权威同步缓存）
- `data_link_sources`、`data_link_snapshots`（本地 SQL 数据源与一次性采集快照元数据）

`operation_events` 只接收可枚举的组件、事件、步骤、稳定错误码和关联 ID，不保存消息正文、URL、请求体或任意 JSON。设置页生成的诊断快照会执行 SQLite `quick_check`，汇总运行状态，并对 `job_events`、`browser_events`、`account_login_events`、`audit_events` 和 `agent_runs` 做脱敏投影；它不会复制这些表的消息、内容、截图路径、profile 路径或 Agent Prompt。完整安全边界和排障流程见 [`DIAGNOSTICS.md`](DIAGNOSTICS.md)。

## SQL 迁移与运行时一致性

新库依次应用 `migrations/*.sql` 后，表、索引和列必须覆盖 `internal/db.DB.Migrate()` 的运行时 Schema。`internal/db/migration_parity_test.go` 同时检查 SQLite 对象和列，防止两条建库路径漂移。

- `migrations/001_init.sql` 已包含 `accounts.account_group`
- `migrations/018_account_focus.sql` 增加 `accounts.last_focused_at`，只记录最近一次成功置前时间，不代表平台登录仍有效
- `migrations/001_init.sql` 已包含 `browser_profiles.fingerprint_config`、`last_pid`、`proxy_server`、`is_proxy_custom`
- `migrations/003_runtime_parity.sql` 以增量方式补充 `proxy_endpoints`、`login_sessions`、`audit_events`、`post_metrics`、`content_versions`、`credentials`、`media_library`、`app_users`
- `migrations/003_runtime_parity.sql` 同时补充 browser profile 代理、启用代理、trace 审计和账号登录会话索引
- `migrations/004_local_studio.sql` 补充本地脚本与剪辑方案；它不创建远程存储、队列或定时器
- `migrations/008_ai_media_generation.sql` 补充显式触发的图片/视频生成任务状态；不保存凭据或远程下载 URL
- `migrations/013_content_media_library_links.sql` 为 `media_assets.library_id` 增加素材库外键与索引；旧行允许保持 `NULL`，新关联必须指向现存素材库项目
- `migrations/016_agent_hub.sql` 增加全局 Agent Hub 的会话、Run、SSE 重放事件、provider 原生会话和审批记录；它不存 API key、Cookie、DOM、完整 CLI 转录或应用源码路径
- `migrations/017_agent_hub_models.sql` 为会话和 Run 增加显式 `model_id`；只保存用户选择的模型标识，不保存 Provider 凭证
- `migrations/019_skill_library.sql` 增加本地 Skill 目录；Skill 正文仍保存在私有数据目录，不把脚本或凭据写入 SQLite
- `migrations/024_tikhub_api_calls.sql` 增加 TikHub 调用生命周期、参数、结果规模、耗时与原始 JSON 路径；完整供应商响应仍保存在权限 `0600` 的本地文件
- `migrations/028_feedback_receipts.sql` 增加本机匿名反馈回执；飞书 Base 仍是处理状态和开发者回复的唯一业务真源
- `migrations/031_publish_evidence_safety.sql` 将发布任务绑定到冻结计划目标，并记录单次提交尝试及其有界结果；数据库触发器禁止修改冻结关联或在缺少创作者后台已发布证据时写入发布记录
- `migrations/035_ai_image_projects.sql` 增加 AI 生图 Project 真源；一个 Project 保存一份独立画布 JSON，旧 `workspace.json` 首次访问时迁入默认项目
- `migrations/054_knowledge_latest_sources.sql` 先建立来源版本索引，再收敛同一知识库、同一来源路径的历史版本：只允许最新成功版本保持 `ready` 并进入 FTS，旧版本保留为 `superseded`；新版本解析、索引失败或来源 mtime 更旧时不替换上一版可用数据
- `migrations/055_data_link_sqlite.sql` 增加本地 SQLite 数据源和一次性快照元数据；源文件、NDJSON 快照与复盘 Markdown 保存在权限受控的数据目录，数据库只保存相对路径、schema 指纹、行数、校验和、质量结果及确认后的知识库引用
- `migrations/057_studio_schedule_handoffs.sql` 增加 Studio 到 Media 的一次性排期交接；它只冻结已审计的内容/素材版本与时区明确的计划时间，不创建本地定时器、cron 或到点发布任务

## SQL Link 数据接入

`data_link_sources` 只登记用户显式选择并复制到 `data-link/sources/` 的 SQLite 文件。对外结构隐藏文件路径；运行时通过 `source_id` 访问，并以只读连接打开。

`data_link_snapshots` 保存一次手动采集的状态与证据，包括数据源、表/视图、查询 SHA-256、schema 指纹、字段映射、行数、字节数、快照校验和、质量问题以及知识库文档引用。原始查询正文和外部绝对路径不写入表；大数据保存在 `data-link/snapshots/*.ndjson`。只有用户明确确认后，复盘 Markdown 才通过现有知识库导入流程进入知识主干。

## Agent Hub 表

`agent_conversations` 保存用户可见的会话标题、当前 provider、模型选择与可见页面上下文；首条用户消息会在本地生成标题。`agent_messages` 只保存用户和最终助手文本。`agent_runs` 记录实际使用的模型，并通过部分唯一索引按会话只允许一个 `running` Run（不同会话可并行，全库并发上限 4）；应用启动时遗留 Run 会标为 `interrupted`。

`agent_events` 是 SSE 的可重放信封，带单调 `seq` 与数据库 event ID；只记录状态、最终回答和工具/审批事件，不记录隐藏思维链。`agent_provider_sessions` 将同一会话分别映射到各 provider 的原生 session。`agent_approvals` 为后续受管工具预留不可变参数哈希与单次决议，当前 chat-only provider 不会创建或执行业务工具审批。

### Bot 人格表

`agent_bots` 保存 Agent Hub 的可复用 Bot 人格：`id`、`name`（必填、去空白）、`role`、`instructions`、`model`（`provider/model` 形式，按 `ValidateModelID` 校验）、`avatar`、`color`、`effort`、`kickoff`、`capabilities_json` 与时间戳。`capabilities` 默认为空数组，为下一轮「只问答不触发工具」预留；本轮不授予任何工具。

`agent_conversations.bot_id` 是可空的 Bot 引用，老行与老 JSON 读回为空字符串并照常工作。Run 启动时服务端按 `bot_id` 取 `instructions`，拼进该次 Run 的 provider 消息（先人格、后证据），同时把同一文本放进 `RunRequest.Instructions`。删除 Bot 只删除名册行，并在同一事务内把引用会话的 `bot_id` 清空；会话、消息与 Run 历史全部保留。`instructions` 是提示词文本，因此不套用凭据/路径过滤，只做长度上限；`name`/`role`/`avatar`/`color`/`effort` 这类短展示字段仍拒绝绝对路径与 token/cookie/password 之类字面量。

## Skill 库表

`skills` 保存 Skill ID、显示信息、来源、审核状态、执行类型、启停状态和本地目录名。标准 `SKILL.md` 正文位于数据目录的 `skills/<id>/`，文件和目录使用私有权限。`review_required` 自动化 Skill 不能启用；审核后的浏览器与发布能力仍只绑定 `tools.Registry`，不从本地目录直接执行任意脚本。

## 匿名反馈回执

`feedback_receipts` 只保存当前设备提交记录的反馈编号、随机查询凭证、用户反馈
正文和最近一次飞书同步结果。查询凭证只用于网关校验单条记录归属，不是飞书
密钥或用户身份；API JSON 明确隐藏它。卸载应用或更换设备后不恢复历史。
飞书字段和本机 Keychain 安全边界见 [`FEEDBACK.md`](FEEDBACK.md)。

## 内容媒体关联

`media_assets.library_id` 记录内容素材对应的 `media_library` 项目。升级前的旧记录可继续读取；迁移后的新记录由数据库触发器强制要求有效 `library_id`。解除内容关联只删除 `media_assets` 行，不删除素材库索引或用户原文件；仍被内容引用的素材库项目受 `RESTRICT` 与显式删除检查保护。

`contents.planned_at` 是可选的本地编辑排期（UTC RFC3339），供内容库时间线和日历预览排序。用户显式准备一键发布时，它会冻结到每个目标的 `publish_at` 并尝试填写平台原生时间控件；它自身不创建后台任务、不启动浏览器，也不在到点时发送任何内容。

## AI 媒体生成任务

### ai_image_projects

保存本地 AI 生图 Project 的名称、独立画布 JSON 与更新时间。画布包含节点、常用提示词和视口；
前端按 Project 自动保存并恢复。升级前的单画布 `ai-image/workspace.json` 会原样迁入“默认项目”，
不会把 Project 数据、素材文件或生成任务写入前端内存作为唯一真源。

### generation_jobs

保存用户明确点击生成后创建的本地、可恢复任务。`kind` 仅允许 `image|video`，状态仅允许
`queued|running|succeeded|failed`；`output_media_id` 只关联已验证并写入 `media_library` 的真实本地产物。
提示词保存在 SQLite 以支持崩溃恢复，但 Go 的 JSON 结构明确隐藏正文，只对外提供 SHA-256；表中不含
API key、credential、签名下载 URL 或远程响应正文。

## 本地 Studio 表

### studio_scripts

用户创作的标题与正文。脚本独立于内容发布和知识库，只用于本地创作与剪辑方案生成。

### studio_schedule_handoffs

保存 Studio 交给 Media 的一次性排期意图：工作流、可选脚本、内容修订、规范化素材快照哈希、UTC 时间、IANA 时区与原始本地墙上时间。状态只能从 `ready` 进入 `frozen`，或因内容/素材变动、取消或过期进入终态；同一工作流最多保留一条 `ready` 交接。冻结发布计划时会在同一事务中重新校验快照并绑定唯一的 `publish_plans.studio_handoff_id`。表不保存审批、nonce、平台凭据或后台定时任务；Media 仍须选择账号/平台并由已验证的平台原生排期能力完成后续交付。

### clip_jobs

保存一次可审阅的本地粗剪方案及其明确执行后的产物。`source_media_id` 指向已有的 `media_library` 索引；`script_id` 受 `RESTRICT` 保护，避免删除仍被方案引用的脚本。被 `clip_jobs.source_media_id` 引用的素材索引也不能删除；未引用索引可删除，但只移除 Alpha 记录，绝不删除用户原文件。任务不会自行入队、重试或定时执行。

剪辑输出只会写入 Alpha 数据目录的 `edits/clip-{id}.mp4`，不会覆盖原始素材。

## 多浏览器运行时表

与 `internal/db` bootstrap（`schemaBrowserRuntimeInstances` / `schemaProfileLeases` / `schemaBrowserEvents`）及 `migrations/002_browser_runtime.sql` 对齐。空库走 `db.Migrate()` 或 `scripts/init-dev-db.sh` 即可建表。

### browser_runtime_instances

进程级运行中实例真源。Supervisor 启动时清空；启动写行、停止删行；`Reconcile` 清理死 PID 与僵尸端口。

```sql
CREATE TABLE browser_runtime_instances (
  profile_id    TEXT PRIMARY KEY,
  account_id    INTEGER,
  pid           INTEGER NOT NULL,
  cdp_port      INTEGER NOT NULL,
  cdp_url       TEXT NOT NULL,
  state         TEXT NOT NULL DEFAULT 'starting',  -- starting|ready|busy|degraded|crashed|stopped
  active_job_id INTEGER,
  lease_owner   TEXT,
  started_at    TEXT NOT NULL,
  last_seen_at  TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
```

### profile_leases

跨进程 profile 互斥（CLI / UI / job / batch）。TTL 默认约 30s，过期可被 `CleanExpired` 清掉后抢占。

```sql
CREATE TABLE profile_leases (
  profile_id    TEXT PRIMARY KEY,
  holder_type   TEXT NOT NULL,   -- ui|cli|job|batch
  holder_id     TEXT NOT NULL,
  acquired_at   TEXT NOT NULL,
  expires_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
```

### browser_events

可选审计：start / stop / crash / heartbeat_failed / focus_operation_failed / focus_succeeded 等（非 cookie 内容）。

```sql
CREATE TABLE browser_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  profile_id    TEXT NOT NULL,
  event_type    TEXT NOT NULL,
  message       TEXT,
  pid           INTEGER,
  cdp_port      INTEGER,
  job_id        INTEGER,
  created_at    TEXT NOT NULL
);
```

## 批量表

### batch_runs

保存一次批量操作，例如批量账号检查、批量导入、批量发布。

```sql
CREATE TABLE batch_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  operation_type TEXT NOT NULL,
  platform_code TEXT,
  status TEXT NOT NULL,
  total_items INTEGER NOT NULL DEFAULT 0,
  pending_count INTEGER NOT NULL DEFAULT 0,
  running_count INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  skipped_count INTEGER NOT NULL DEFAULT 0,
  concurrency INTEGER NOT NULL DEFAULT 1,
  settings_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

### batch_items

保存批量操作里的每一条 item。失败、重试、断点续跑都以 item 为单位。

```sql
CREATE TABLE batch_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  item_type TEXT NOT NULL,
  item_id INTEGER,
  account_id INTEGER,
  asset_id INTEGER,
  content_id INTEGER,
  job_id INTEGER,
  status TEXT NOT NULL,
  current_step TEXT,
  error_code TEXT,
  error_message TEXT,
  screenshot_path TEXT,
  dom_snapshot_path TEXT,
  raw_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

## publish_jobs

`publish_jobs` 必须有 `batch_id`，单条发布时为空，批量发布时关联 `batch_runs.id`。

```sql
CREATE TABLE publish_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER,
  plan_id INTEGER,
  plan_target_id INTEGER,
  content_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER,
  platform_code TEXT NOT NULL,
  content_type TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'browser',
  status TEXT NOT NULL,
  current_step TEXT,
  settings_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  retry_count INTEGER NOT NULL DEFAULT 0,
  max_retries INTEGER NOT NULL DEFAULT 2,
  error_code TEXT,
  error_message TEXT,
  trace_id TEXT,
  idempotency_key TEXT,
  cancel_requested_at TEXT,
  snapshot_hash TEXT NOT NULL DEFAULT '',
  submit_attempted_at TEXT NOT NULL DEFAULT '',
  submit_outcome TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (batch_id) REFERENCES batch_runs(id),
  FOREIGN KEY (plan_id) REFERENCES publish_plans(id),
  FOREIGN KEY (plan_target_id) REFERENCES publish_plan_targets(id),
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);
```

真实提交任务的 `plan_id`、`plan_target_id` 与 `snapshot_hash` 必须同时存在且不可变。最终点击前原子写入 `submit_attempted_at`；该字段一旦存在，取消、重试和恢复都会关闭，直到创作者后台验证明确结果。只有后台状态为 `Published`、`Public`、`已发布` 或 `已发表`，并同时保存公开链接、外部内容 ID 与私有截图后，任务才可转为 `creator_backend_verified` 并写入唯一的 `published` 记录。上传完成、处理完成、Toast、草稿和用户口头确认都不构成发布成功。

2026-07-16 起，新库不再创建 `schedule_entries` 或 `publish_jobs.scheduled_at`。已有数据库中的旧表/列不做破坏性删除，但运行时不读取、不写入、不执行。

## TurboPush 映射

来源：

```text
/Users/albert/.TurboPush/data.db
```

当前安全映射：

```text
TurboPush platforms       -> 只映射到 Alpha 已内置的等价 platform code
TurboPush accounts        -> accounts + 全新 Alpha browser_profiles
TurboPush plat_sets       -> platform_settings 安全元数据；settings_json 固定为空对象
TurboPush articles        -> contents
TurboPush publish_records -> 只检查并报告跳过，不伪造账号/平台关系
```

`record_infos`、源媒体、源代理、Cookie、Profile、salt、nonce 与设备身份当前均不导入。

必须保留来源字段：

```text
source = turbopush
source_platform_id
source_account_id
source_setting_id
source_content_id
```

TurboPush `accounts.cookie`、Profile、salt、nonce 和设备身份字段禁止导入 Alpha-Nexus。只迁移账号显示名、平台、分组、备注等非敏感元数据；账号必须在 Alpha-Nexus 重新登录或走后续官方 OAuth。
