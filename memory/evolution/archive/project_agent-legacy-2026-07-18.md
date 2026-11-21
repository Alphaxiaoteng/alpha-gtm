# Alpha-Nexus — 运行时快照

> 最后更新：2026-07-17
> **不是入场必读。** 规范 → `AGENTS.md` · 地图 → `docs/MAP.md`  
> **踩坑 / 自进化 / 记忆** → [`memory/`](../../README.md)

---

## 依赖链（速记）

```text
PortAllocator + LeaseManager
    → Supervisor / BrowserFacade
    → CLI browser_*  &  UI server.go
```

```text
fingerprint-chromium → CDP 15101–15199 → chromedp
runtime.json ← Supervisor heartbeat
```

---

## 浏览器专属规则

- 生命周期真源：**BrowserFacade**（勿旁路 `BrowserManager` / 裸 `exec`）→ [L001](../mistakes/lessons/L001-browser-manager-vs-facade.md) · [ADR 0003](../adr/0003-browser-facade-not-manager.md)
- 端口池 15101–15199；Listen 测空闲 → [L002](../mistakes/lessons/L002-cdp-port-time-wait.md)
- 同 profile 互斥：LeaseManager
- CLI 短进程无持久 heartbeat → 长驻用 `ui serve` / Electron → [L009](../mistakes/lessons/L009-cli-exit-kills-supervisor-heartbeat.md)
- 登录检测默认被动（不 Navigate）→ [L006](../mistakes/lessons/L006-login-poll-navigate-hijack.md) · [L007](../mistakes/lessons/L007-passive-login-false-positive.md)
- SQLite：`busy_timeout` + WAL → [L008](../mistakes/lessons/L008-sqlite-busy-under-concurrency.md)
- profile / cookie 敏感，不进日志与仓库

---

## 表（运行时扩展）

业务表见 `docs/SCHEMA.md`。浏览器运行时额外：

| 表 | 用途 |
|----|------|
| browser_runtime_instances | 运行中实例；`active_job_id` 串行化同账号发布 |
| profile_leases | profile 互斥租约 |
| browser_events | 浏览器事件日志 |

---

## Roadmap 摘录

- [x] Turbo Push Phase 0 基础：区分实时 data.db/.tp、安全 insert-only importer、Schema/migrations 关系护栏
- [x] Turbo Push Phase 1 基础：动态 Go Sidecar、进程认证、受限重启、Registry JSONL/typed job events
- [x] Turbo Push Phase 2 控制面：HTTP tools/SSE、请求幂等、队列 lease reclaim、通知中心
- [x] Turbo Push Phase 3：内容中心 CRUD、媒体关联、登录/发布向导、安全模板和人工登录会话
- [ ] Turbo Push Phase 4：22 个发布路径逐批 Native 化；`custom` unavailable，真实平台验收仍进行中
- [ ] Supervisor 长驻 daemon（彻底解决 L009）
- [ ] Meta 真实自动发布（Phase 4）
- [ ] Electron 独立发行版打包
- [ ] TUI（Bubble Tea，可选）

已完成项与历史长文见 git history；**新踩坑只写 `memory/evolution/mistakes/lessons/`**。

---

## 2026-07-16 Turbo Push 调研

- 完整报告：docs/agentpush/TURBOPUSH_DEEP_DIVE.md
- ADR：`memory/evolution/adr/0008-turbopush-capability-integration.md`、`0009-supervised-go-sidecar-observability.md`（控制面和内容中心已实现；平台 Native 与真实账号验收未完成）
- `/Users/albert/.turbopush` 是实时运行根；`.TurboPush` 为同 inode。`.tp` 只是外部加密导出包。
- 运行根含 data.db、2756 行/18 平台 Selector Catalog、3 个 Chromium Profile、主/KB Go Sidecar、发现文件和四层分散日志。
- data.db 有 2 个账号而 Profile 有 3 个；不得按数字目录猜映射。源 Cookie/salt/nonce 与设备身份字段不得迁移。
- Turbo 拓扑：Tauri 监管主 Go v2.3.6 + KB Go v2.3.0；Alpha 排除 KB，保留单 Go Sidecar。
- Alpha 已实现 trace/job/audit/browser events 的 context、typed event、脱敏与轮转；内容/平台生命周期仍需逐平台验收。
- 禁止复制 Cookie；迁移非敏感元数据后在 Alpha-Nexus 重新登录/OAuth。
- 推荐动态 loopback 端口 + transport Token + 用户 JWT 分层；禁用 query token；SQLite 事件先持久化再 SSE。
- 推荐 Alpha 唯一真源 + 可关闭 Turbo Connector + 自有版本化 SelectorCatalog + 逐平台 Native 替换。

## 2026-07-16 集成基础实现

- `internal/importers/turbopush`：只读白名单迁移，`.tp` fail-closed，重导不覆盖 Alpha 本地登录/设置/内容状态；绝不读取或复制源凭证/Profile。
- `internal/db` / `internal/publish`：旧库关系触发器、发布记录去重触发器、原子 job Claim、CLI/tools/队列 Idempotency-Key、typed `job_events` 与 trace；真实发布成功但持久化异常会保存 `job.published_with_warning`，避免重试造成二次发布。
- `internal/sidecar` + `electron/sidecar.js`：0700/0600 descriptor、动态 loopback、PID/nonce/health、header transport auth、有限重启、TERM→KILL。Electron 开发态使用 `run_electron.sh`；2026-07-16 已实际打开项目 Electron 窗口并验证后端 ready。
- `internal/observability`：Registry 运行时使用按 PID 共享的有界 0600 JSONL；只允许关联 ID、稳定错误码与状态字段。业务恢复以 SQLite 为准，日志不是业务真值。
- `internal/ui`：`/api/v1/tools/schema`、`/api/v1/tools/call` 和 `/api/v1/events` 直接复用 Registry/SQLite；SSE 使用 `Last-Event-ID` 重放 durable job events，`RawJSON` 不会进入 HTTP。
- `internal/tools`：`content.create` 已写入本地内容，`media.import` 已探测并索引本地媒体，`queue.enqueue` 已写入可执行的 `publish` 任务；这三个入口不再只返回 `planned`。实时发布入队仍要求幂等键；其余未接线工具显式返回 `TOOL_NOT_IMPLEMENTED`，禁止伪造 planned success。
- `internal/agents`：`PublishAgent` 没有内容/账号/素材/幂等键输入，不能创建任务；它固定返回 `not_executed/PUBLISH_AGENT_REQUIRES_EXECUTABLE_JOB`，实际发布只允许经 ToolAgent 的 `publish.run` 或顶层 `publish` CLI。不得把摘要规划伪装为 `queued` 或 `awaiting_review`。
- `internal/queue`：`queue worker` 使用短事务 lease、心跳续租与崩溃回收；Registry 与 CLI 只接受当前可执行的 `publish` kind，worker 遇到遗留未知 kind 会显式失败，避免空任务被标记成功。`notifications` 有 API 与 React 通知中心，持久化错误只写稳定码。
- 发布任务控制：`GET /api/jobs/{id}` 返回脱敏任务和 durable events；`POST /api/jobs/{id}/cancel|retry` 分别执行真实取消和真实入队重试。运行中取消只在该次 Runner 生命周期内探测持久化 marker 并取消 Adapter context；终态写入与取消以单条条件 SQL 线性化，取消获胜时禁止写 `publish_record`。UI 复用“任务与记录”页，只有手动刷新和动作后刷新，不新增轮询、resume 或调度器。
- SQL 迁移一致性：`migrations/001_init.sql` 已补齐账号分组及浏览器 fingerprint、PID、代理字段；`migrations/003_runtime_parity.sql` 已补齐 8 张运行时表和 4 个索引。`internal/db/migration_parity_test.go` 检查 SQL 迁移与 `DB.Migrate()` 的对象和列覆盖。
- `internal/selectors`：自有版本化 SelectorCatalog；登录成功只匹配审核过的精确 Cookie 名，DOM 只用于识别明确登录页。无可靠成功证据返回 `manual_verify`，不会按 URL、Cookie 数量或通用 session/token 名猜测已登录。
- CLI `account check` 与 `batch account-check` 通过 `internal/logincheck` 对目标账号运行中的 CDP 做只读 Cookie 名校验；不导航、不读取 Cookie 值、未验证不会写 batch success，更不会回显数据库旧登录状态冒充检查结果。
- 账号启停状态已落到 `accounts.enabled`：`account disable|enable --id` 同步切换已有发布目标，禁用账号拒绝受管浏览器启动和发布目标复用；不删除 profile、素材或历史记录。
- `internal/ui/content_handlers.go` + React 内容中心：文章/图文/视频 CRUD、复制、删除保护。发布向导已可选择本地内容/账号资产并默认 dry-run；账号资产发现和真实平台验收仍未完成。未经单独授权不得触发真实发布。
- `platform_settings` + `/api/templates` + React 发布模板：平台绑定、启停、每平台默认模板与 JSON 校验；服务端拒绝 cookie、token、密码、会话、credential 等凭证形态字段。发布向导只可选择同平台且启用的模板，并把安全快照写入 `publish_jobs.settings_json`，不会声称未验证 Adapter 已应用该设置。
- `internal/media.ValidateLocalPath` 由 UI 媒体入口和 Tool Registry `media.import` 共用：用户明确选择或填写的本地媒体路径才会被探测与关联；支持常见图片、视频、音频，4 GiB 上限、SHA-256 内容去重、`media_library` 索引。校验真实路径并拒绝 `.turbopush`、`1key`、`.env` 目录，防止符号链接绕过；解除关联绝不删除原始文件。
- 本地 Studio：`studio_scripts` 存独立脚本，`clip_jobs` 存可审阅方案与产物状态；素材只索引不复制。素材库可删除未引用索引，永不删除原文件；被剪辑方案引用的源素材会被拒绝删除。粗剪 Agent 仅直连 `127.0.0.1:11434` 的本机 Ollama，禁用代理且不读取远程模型配置；先生成 JSON 方案，用户再次点击才用本机 ffmpeg 输出 `edits/clip-{id}.mp4`。无远程回退、无队列、无定时器、无页面轮询。执行时依次探测 `ffmpeg`、`ffmpeg-full`，并要求 MP4 muxer；开发机的 Playwright 精简 `ffmpeg` 不可用时会安全回退到 `ffmpeg-full`，仍不可用则返回稳定错误而不伪造产物。`ALPHA_NEXUS_REAL_LOCAL_SMOKE=1 go test -run TestStudioRealLocalSmoke ./internal/ui` 是显式本机验收：已在 2026-07-17 用本机 Ollama `llama3.2`、完整 ffmpeg 与临时 MP4 跑通计划及输出，默认 CI 跳过，不产生远程模型调用。
- Studio Agent 协同：已有 `media.import` 加上 `studio.media.list`、`studio.script.create|list`、`studio.clip.plan|execute` 已进入 Tool Registry，MCP 自动投影为 `registry_studio_*`；计划只访问本机 Ollama，`studio.clip.execute` 同时受 MCP write guard 与工具 `confirm=true` 约束，不能由计划阶段自动写视频。
- Studio Agent 执行边界：通用 LLM Tool Agent 的紧凑 schema 已包含 Studio 工具，但系统提示和代码都会移除 `studio.clip.execute`；若模型只请求执行则返回 `STUDIO_CLIP_EXECUTION_REQUIRES_USER_CONFIRMATION`，不回退到无关操作。Tool Agent 的 `MaxRetries` 仅对 read 工具生效，任何 write/browser/network 工具固定单次，避免重复写脚本、方案、输出文件或外部动作。
- 2026-07-17 为解决过度工程，移除定时发布和自动周期采集的 UI、API、Worker、CLI、Tool 与配置入口。旧库的 `schedule_entries` / `scheduled_at` 只作 Schema 兼容，运行时不读取、不执行、不对用户暴露；内部 heartbeat/取消探测仅维持当前任务正确性，不创建业务调度。
- 2026-07-17 进一步移除页面后台轮询、批次 SSE 自动刷新、登录被动轮询和空白标签 watchdog；账号、批次、通知、指标与仪表盘只在页面加载、用户手动刷新或用户刚执行的动作后读取本地状态。登录验证必须由用户点击触发。
- `media watch` 也是无必要的目录轮询，已移除；媒体只允许用户明确执行一次 `media index` 或导入动作，禁止把本地扫描变成常驻同步。
- `login_sessions` + 账号管理：启动人工浏览器登录时创建 15 分钟、单账号唯一的会话状态记录；被动或显式验证成功才标记 `verified`。只保存状态、时间和稳定错误码，绝不保存 Cookie、密码、OTP 或二维码内容；关闭浏览器会取消等待中的会话。
- 内容中心现有本地 Markdown 导入、预览与 MD/HTML 导出；用户在桌面端主动选择/导出文件时才发生本机文件读写，不经 Sidecar 网络传输。发布记录页只读取 `publish_records` 中的已确认成功记录；指标报告只聚合本地 `post_metrics`，不触发平台采集。
- `proxy_endpoints` + `/api/proxies` + React 代理池：端点仅允许 `http(s)`/`socks5` 的无凭证 `host:port`，唯一保存、启停、编辑、删除与账号粘性绑定均在本地 SQLite 完成。编辑会在短事务内同步已绑定画像；已绑定端点拒绝删除，单账号手工代理会自动脱离代理池。健康检查、轮换 API、代理凭证/Keychain 引用均未实现，避免未授权网络访问或明文凭证落库。2026-07-16 已通过 Electron 页面验收。
- 平台覆盖：目录 23 项、发布代码 22 项。Instagram/Facebook 可尝试自动提交但未完成真实账号验收；17 个平台只做安全人工接管；YouTube/TikTok/X 仅 dry-run，live 固定 `API_CONNECTOR_UNAVAILABLE`；`custom` unavailable。通用路径只打开已运行浏览器的创作后台，绝不点击发布。
- 人工发布确认：`/api/publish-jobs/{id}/confirm` 接受 `manual_takeover` 或 `awaiting_review`，短事务内将任务转为 `published/confirmed_by_user` 并写入 `status=user_confirmed`、`source=user_confirmed`。它是用户明示确认，不是平台回执；重复确认已发布任务返回 409。
- 发布向导结果必须按 `data.status` 解释：`manual_takeover` / `awaiting_review` 是“需人工处理”，不能因工具调用本身成功而显示“任务已完成”。`ui-web/src/publishOutcome.ts` 统一分类，并提供“打开任务与记录”入口；仅 `published`、`dry_run_ok` 等真正完成态使用成功提示。
- 安全快照导出：桌面设置页通过 `/api/export/snapshot` 下载可审阅业务元数据（账号显示名/分组、内容、模板、发布记录、无凭证代理端点与指标摘要）。明确不含登录会话、浏览器画像、Cookie、凭证引用、密钥、原始数据库或运行路径；有回归测试防止该类字段泄露。
- 代理安全收口：Proxy API URL 和返回 endpoint 都只接受无凭证、无 query/fragment 的 `http(s)`/`socks5` host:port；请求错误不会反射 URL 或响应内容。设置和账号页已移除 `user:pass@` 引导，凭证代理与外部代理健康检查仍未实现。
- XHS 数据采集只保留用户明确触发的一次性本地动作；无周期器。Webhook 未接入，不保存 URL/密钥或发送外部数据。
- 设置页不再保留未接线的开机启动、系统托盘、空标签自动关闭、按天删日志等伪开关；只展示已实现的固定数量/大小轮转审计日志契约，避免把纯前端状态误导为运行时能力。
- Meta 自动提交收口：只允许点击后出现的 `status` / `alert` / toast / live-region 中的明确成功语句作为成功回执；页面正文或历史内容中的“已发布”不再可使任务转 `published`。失败诊断截图权限为 0600。
- 批量演练：桌面端可按同一平台的启用账号资产创建持久化 `publish` 批次；创建 batch run 与所有 batch items 使用单个 SQLite 事务，避免半成品记录。服务端强制 `dry_run:true`、限制 1–100 项与 1–10 并发，并为每项写幂等 publish job；不启动浏览器、不提交平台。Electron 已实测入口、表单与后端 ready。
- 全局内容检索：侧栏搜索框支持 `⌘/Ctrl+K` 聚焦，Enter 跳转内容中心；按标题、摘要、正文、标签和内容状态实时筛选。本地内存筛选，不发起平台或第三方网络请求；Electron 已实测导航与匹配计数。
- 非知识库 AI 创作：内容中心提供标题、摘要、改写、检查、平台文案适配。默认 deterministic 本地建议，绝不读取模型配置或联网；只有用户在当前操作勾选“允许发送草稿到已配置模型”并点击后，才调用现有 Gateway（可能产生费用）。后端有不外发与显式外发的隔离测试；Electron 已核验默认未勾选界面。
- 全局仪表盘：`/api/dashboard/summary` 只读聚合 Alpha 自身的内容、账号、发布记录和任务状态；首页新增内容草稿、已登录账号、成功发布、待处理任务，不触发采集、登录或外网访问。Electron 已显示实际本地计数。
- 批量发布目标选择：批量演练表单现在列出所选平台的全部启用账号资产，默认全选但可逐项多选、全选/全不选；服务端仍只接受同平台、唯一 ID，且强制 dry-run。
- Electron 托盘：主进程创建原生 tray，关闭窗口默认隐藏而不停止本地 Sidecar；托盘菜单可显示、隐藏、明确退出，明确退出仍复用 `before-quit` 的 Go 清理路径。已通过 `node --check` 和 14 项 Electron 进程测试。
- Electron 开机启动：系统菜单新增显式 checkbox，初始不调用系统 API；仅用户点击后执行 `app.setLoginItemSettings`，勾选状态从 `getLoginItemSettings` 读取并重建菜单。不得以测试或启动过程自动修改该系统设置。
- Markdown 创作补充：内容编辑器显示正文词数与约定 400 词/分钟的阅读时长；提供仅复制标题、摘要、正文的纯文本按钮，未保存前也可用，不修改内容或发送网络请求。
- 主题系统收口：过亮的“马里奥像素”已改为默认“暗黑 Game”HUD（黑灰表面、硬边、仅少量彩色像素）；持久化 ID `mario` 只为兼容旧偏好，v3 会把旧默认 Hermes 一次迁移到 Game，之后尊重用户手动选择。保留主题 ARIA 状态、高对比焦点环和 reduced-motion；不增加角色或版权素材。
- 构建产物不再增量堆叠：`internal/ui/dist` 严格镜像当前 Vite 输出，失效哈希包已清理，内嵌资产从约 12 MiB 降为 852 KiB。
- 批量演练契约：POST create / GET runs / GET items 共用显式 camelCase DTO，`dryRun` 语义不再冒充真实发布；批次项从 SQLite 返回真实 `startedAt` / `finishedAt`，pending/running/success 计数按 worker 生命周期转移。精确 key-set 合约测试防止 PascalCase 回归。
- 内容 AI 辅助结果绑定内容 ID、action、平台和完整草稿版本；切换/编辑后旧结果不可应用，`check` 永不覆盖正文。`platform_adapt` 前后端都要求平台目录中的真实 code；默认本地路径不联网，本轮未调用付费模型。
- Electron 链路收口：外链保持窗口导航全拒绝，只经校验 sender 的 IPC 打开无凭据 HTTP(S)；看板/发布记录失败显式 Toast。第二实例统一走 `showMainWindow()`，macOS 额外激活 app；已实机验证托盘隐藏后二次启动能恢复同一窗口。
- 2026-07-17 桌面真实烟测：用临时数据目录启动 Go `ui serve`、Vite 与本仓 Electron，窗口实际加载 `127.0.0.1:17321`；暗黑 Game HUD、素材与剪辑、发布向导可访问，空数据时发布按钮保持禁用。验收后只停止本次三项临时进程，17321/5173 均已释放。
- Electron macOS 本地打包：`electron/package-macos.mjs` 复用已安装 Electron runtime，复制时必须保留 Framework 的相对软链接，避免 bundle 指回 `node_modules`；随后显式复制最小主进程文件与 assets，并把本次构建的 Go Sidecar 放到 `Contents/Resources/alpha-nexus`（与 `process.resourcesPath` 契约一致）。复制和改写 bundle 会使上游签名失效，因此本地包完成后必须做 ad-hoc 重签名；输出 `release/Alpha-Nexus.app` 已被 gitignore。正式分发仍需要 Developer ID 签名和 Apple 公证。
- XHS 发布安全底座：所有入口只接受目标账号 Facade 的运行态 CDP，不再回退默认 15101 或接受通用端口覆盖；账号资产与平台必须一致，多个媒体显式返回 `PUBLISH_MULTIPLE_MEDIA_UNSUPPORTED`。`browser_runtime_instances.active_job_id` 原子串行化同账号浏览器任务，终态残留可安全回收；前置失败进入失败中心并区分可重试/永久失败。
- 发布意图与模板：每次 Runner 持久化 `dry_run` / `prepare` / `submit`，重试只能复用原意图，不能从演练/审核升级为自动提交。XHS 模板按 settings schema、Selector Catalog、Adapter 三个版本生成不可变 `publish_jobs.settings_json` 快照；无模板任务也固化安全默认值。专用 XHS Adapter 仅离线校验快照并打开 Creator，仍返回人工接管。
- 发布并发边界：任务创建用单条 INSERT 原子绑定 idempotency key、mode 与 settings snapshot；Runner 只核验持久化意图，不再“先创建、后补权限”。CLI/ops 真实模式强制稳定幂等键，单任务与批次 resume 都拒绝改变原始 `dry_run` / `prepare` / `submit`；拒绝发生在消费 retry budget 或创建新 job 之前。
- 浏览器发布租约：显式 Stop 与发布 Claim 以同一 runtime 行线性化；运行中 `active_job_id` 返回 `BUSY_BY_JOB`，进程退出/Reconcile 也不得抹除。清理按 `profile_id + pid + started_at` 校验实例身份，DB 错误 fail-closed；Runner 同时 heartbeat job 并验证 runtime ownership。lease 统一用 RFC3339 UTC 比较、有限重试、Renew 检查 owner，账号删除在短事务内清理 setup 子表并保留发布历史 RESTRICT。
- 浏览器 launch 代次：调用方 holder 只是逻辑标签，每次 Open 都派生标准库随机 generation token；runtime reserve/delete/heartbeat 同时校验 `profile_id + pid + RFC3339Nano started_at + lease_owner`。启动前拒绝 DB 中的活 PID/运行中 job，starting/ready runtime 持久化失败会终止该代浏览器，防止同一 profile 双开、PID ABA 和游离进程。
- 批次恢复并发：恢复平台、`dry_run`、job BatchID/目标/内容类型/mode 都是不可变意图；失败 job 仅在校验和媒体/浏览器前置成功后消费 retry。batch item 用 pending→running CAS，失败态回 pending 也校验原状态；`running` 不自动回拨，避免两个 resume worker 并行。人工确认后的 `published` 按已完成对账，关键 item/link/counter 写失败直接返回错误且不发 success。
- XHS Native 自动发布仍未完成：真实 URL、上传控件、设置控件、草稿/提交和成功 toast 必须使用用户测试账号分阶段取证与验收。未获得明确页面证据前，不得把 XHS 标记为 `browser_automation`，不得点击提交或声称发布成功。
- 发布真实性收口：生产代码已删除 `AGENTPUSH_SIMULATE_PUBLISH`、合成 URL/ExternalID 和通用 webhook Publisher。旧环境变量不能恢复假成功；YouTube/TikTok/X live fail-closed，dry-run 仍可离线验证。
- MCP 友好工具与核心 Registry 共用 readiness、内容类型、账号/资产/平台绑定、幂等键和 live confirm；缺目标或多候选必须显式失败，不能返回虚假 planned success。
- Meta discover 只接受无凭证的 loopback CDP endpoint，并连接目标账号当前 BrowserFacade 代次；不再忽略 CDP URL 或回退首个账号。发布成功只认点击后新增的明确 success region。
- SQL migrations 与运行时平台 seed 共用 23 代码契约，并有迁移回归测试；`account_assets` 默认类型为 `browser_account`，不是六平台固定枚举。
- Meta 发布目标补齐：当前目录使用独立 `facebook` / `instagram` 账号，不存在历史文档中的 `meta` 账号 code。桌面账号卡只为这两类账号提供显式“发布目标”表单；`POST /api/accounts/{id}/assets` 由服务端固定映射为 `facebook_page` / `instagram_account`，要求名称与公开外部 ID、拒绝重复并写审计事件。默认 `browser_account` 不能满足 Meta Adapter 校验；目标表单不接收或保存密码、Cookie、令牌，发布仍由用户在向导中明确触发。
- Meta 目标一致性：`publishguard` 统一限制 Facebook 只能使用 `facebook_page`、Instagram 只能使用 `instagram_account`；发布向导和批量发布会隐藏默认浏览器容器，工具、HTTP 与 CLI 写入路径会在创建任务前拒绝错误资产类型。
- Meta 目标发现桌面闭环：账号卡新增“发现目标”，用户二次确认后才会使用该账号当前运行的 loopback BrowserFacade CDP 打开 Meta 设置并读取已连接的 Page / Instagram 名称；服务端不启动浏览器、不接受外部 CDP、不读取凭证，未运行浏览器统一返回 `PUBLISH_BROWSER_NOT_RUNNING`。
- Meta 自动提交目标门：发现的同名资产可由用户填写公开 ID 原地确认，避免重复目标；只有目标同时具备名称和公开 ID、Business Suite 页面精确匹配并暴露为选中态时，Adapter 才会继续进入创建器和提交。任何缺失都固定转 `awaiting_review/TARGET_SELECTION_UNCONFIRMED`，绝不在未知目标上下文点击发布。
- Meta 目标所有权收口：目标必须同时匹配请求平台、资产平台与所属 `accounts.platform_code`；Facebook/Instagram 的跨账号平台资产会在 UI、HTTP、Tool、CLI、Batch 和 Runner 消费旧任务时统一拒绝。唯一兼容例外是历史 `meta` 容器；新发现结果只写回当前 Facebook 或 Instagram 账号对应类型。
- Agent 离线反馈：OpenCode 本地代理若返回 transport JSON（如 `Connection refused`），`terminal.ChatWithHistory` 转为 `model_gateway_unreachable` 和可恢复中文提示；前端同时规范化旧会话，禁止把网关 JSON 伪装成助手答案。2026-07-17 已通过 Electron 临时数据目录实机复核；本地全包/前端/Electron 回归不等于真实第三方登录、模型连通或发布验收。
- Agent 本机模型：模型目录固定提供可选 `local/llama3.2`（不改变既有默认模型）；用户在 Agent 菜单显式选择时，聊天只请求 `127.0.0.1:11434/v1`，不读取代理/密钥/远端 endpoint。不可用时返回现有可恢复提示。`ALPHA_NEXUS_REAL_LOCAL_SMOKE=1 go test ./internal/terminal -run TestChatWithHistoryLocalOllamaSmoke -v` 仅以固定文本调用本机 Ollama；2026-07-17 已真实通过。Electron 临时目录亦已验证模型选择、会话持久化和 1.45 秒回复。
- E2E 基线已对齐 Alpha-Nexus：旧 `Unibuy Studio` 断言及隐藏 browser-path input 断言已移除；Playwright 覆盖 shell、账号、设置、API、Studio 三栏的无副作用入口和 Agent 本机模型菜单。`cd e2e && npm test -- --reporter=list` 在临时本地服务 10/10 通过；测试不导入文件、不写脚本、不调用模型或发布。
- E2E 隔离：Playwright 只启动专用 `127.0.0.1:17325` Go 服务和 `5174` Vite，启动前重置仓库受忽略的 `.e2e-data`；Vite 由 `VITE_API_PROXY_TARGET` 定向测试 API。它不复用桌面 `17321` 或默认 `~/.alpha-nexus`，因此不会读取真实账号、浏览器画像或凭证。标准入口是 `./scripts/e2e-run.sh`，2026-07-17 10/10 通过。
- LLM Tool Agent 真实性：模型网关错误或非 JSON / 空工具计划固定返回 `LLM_GATEWAY_UNAVAILABLE` 或 `LLM_TOOL_PLAN_INVALID`，且不调用 Registry；禁止以 `account.list + browser.status` 兜底后报告成功。回归测试用 mock 网关断言零工具事件；2026-07-17 全量 Go/vet、前端 15、Electron 20、隔离 E2E 10 均通过。真实第三方登录、发布和付费模型仍需单独授权与账号验收。
- LLM Tool Agent 本机模型：当请求模型为 `local/<name>` 时，Planner 与桌面聊天相同，剥离 `local/` 前缀后固定直连 `127.0.0.1:11434/v1`，清空远端 fallback、Key 和 proxy，并使用无代理 Transport；空名称返回 `LOCAL_OLLAMA_MODEL_REQUIRED`。`ALPHA_NEXUS_REAL_LOCAL_SMOKE=1 go test ./internal/agents -run TestLLMToolsAgentLocalOllamaSmoke -v` 只生成 dry-run 计划，2026-07-17 使用 `llama3.2` 5.61 秒通过，无工具执行。
- LLM Tool Agent 外部动作门：Planner catalog 不再暴露 `publish.run`、`queue.enqueue`、`workflow.run`、`batch.publish`、网络/付费工具与 `studio.clip.execute`；即使模型伪造 `confirm:true`，运行时仍会在 Registry 前移除并返回 `AUTOMATED_EXTERNAL_ACTION_REQUIRES_USER_CONFIRMATION`。安全本地步骤可继续执行，发布向导的显式自动提交不受影响。2026-07-17 本机 Ollama Planner 烟测 2.15 秒通过，全量 Go/vet 通过。
- 桌面 Agent 工具协同：`/api/coding/chat` 新增显式 `mode:"tools"`，只有用户点击 Agent 输入框的“工具协同”才运行 `llm_tools`；普通聊天不变。工具结果按会话持久化为 `llm_tools` 消息，界面只显示安全摘要和步骤，不展示模型原始 JSON。附件不进入该模式，需先从 Studio 素材库导入。临时空数据目录已真实用本机 Ollama 执行“检查账号状态”，`account.list` 与 `browser.status` 均成功并写入会话；未访问真实账号或外网。
- 工具协同模型默认：进入工具模式（包括“检查账号状态”“整理作品素材”快捷语）会优先选择已注册的 `local/llama3.2`；用户仍可从模型菜单改选其他模型。API 调用未给模型时，`mode:"tools"` 也固定默认本机 Ollama，普通聊天仍使用用户默认模型。2026-07-17 隔离 E2E 11/11 验证模式切换及模型标签。
- E2E 构建隔离：正式 `build.sh` 会生成 universal `dist/alpha-nexus`，Playwright 改为构建/启动 `dist/alpha-nexus-e2e`，避免 Go 覆盖 fat Mach-O 失败。2026-07-17 11/11 通过。
- 工具协同附件边界：`mode:"tools"` 现在可使用当前会话 `attaches/` 中用户显式上传的常规文件作为 `media.import` 候选；服务端解析真实路径、拒绝目录/会话外路径/逃逸符号链接并去重。Planner 只获知此白名单，`LLMToolsAgent` 还会在 Registry 前核对每个 `media.import.path`，不匹配返回 `MEDIA_IMPORT_PATH_NOT_ALLOWED` 且零工具调用。普通聊天不受影响，发布和 `studio.clip.execute` 仍需各自明确确认。2026-07-17 定向 Go 安全回归、前端 lint/build、隔离 E2E 11/11、`go test -short ./...`、`go vet ./...` 与 `build.sh` 均通过；未触发真实登录、发布或付费模型。
- 工具协同素材 UX：工具模式的附件选择器只允许图片、视频与音频扩展；普通聊天仍可选文本附件。若用户先在聊天模式选了文本附件再切换，UI 保持聊天模式并提示移除文本，避免把无法 `media.import` 的文件交给 Planner。隔离 Playwright 覆盖媒体附件芯片、文本切换拦截，2026-07-17 共 13/13 通过；最新 `dist/alpha-nexus` 的隔离浏览器也实测了这两条路径，控制台无 warn/error，未发送模型请求。
- Agent 作品区与 Studio 联通：`POST /api/coding/works` 在用户主动上传后仍写入本地 `works/`（供发布作品结构使用），同时对可识别图片/视频/音频建立 `media_library` 索引；caption/text 等非媒体仍保存为作品文件但不进入 Studio。API 返回 `indexed_media`，Agent 显示索引数量并提供“打开 Studio 素材库”跳转。HTTP 回归断言一视频一文案仅索引视频；2026-07-17 前端 lint/build、隔离 E2E 14/14 和最新桌面包的浏览器跳转实测通过，未调用模型、平台或外网。
- Studio→Agent 剪辑协同：用户可把已选本地视频与已保存脚本显式带入 Agent 的一次性 `agentDraft`，自动进入本机 Ollama 工具模式并预填可审阅的剪辑方案请求；只聚焦输入框，必须再次点击发送才会调用模型。导入后先刷新素材列表再选中，修复了旧列表 effect 偶发清空新视频选择的竞态。2026-07-17 前端离线测试、lint、build、Go 桌面包与隔离 Playwright 15/15 均通过；最新桌面包浏览器实测交接、预填、工具模式和零控制台错误，未发送模型请求。
- Agent 本机模型诊断：工具模式新增用户显式触发的 `POST /api/coding/local-model-status`，只读取固定 `127.0.0.1:11434/api/tags`，不读取代理/密钥/远端配置、不发送提示词、不自动探测；稳定区分未运行、模型缺失和响应异常。普通聊天的 `local/<model>` 也改用显式 `Proxy:nil` transport，兑现本机模型绝不经过环境代理的契约。2026-07-17 Go 短测/vet、前端 lint/build、隔离 Playwright 16/16 与桌面包浏览器渲染均通过；E2E 拦截检测响应，不读取开发机实际 Ollama 状态。
- Studio 脚本防丢失：切换脚本库下拉、粗剪脚本下拉或点击“新建”前，若标题/正文与已持久化版本不同，都会要求确认放弃；拒绝后完全保留草稿，头部显示“未保存”。删除已由独立确认覆盖，成功后不再二次询问。2026-07-17 前端静态测试、隔离 Playwright（拒绝确认路径）及全量短测/vet/build 均通过；无模型、ffmpeg、登录或发布调用。
- Studio 真实闭环与产物定位：`ALPHA_NEXUS_REAL_LOCAL_SMOKE=1 go test ./internal/ui -run TestStudioRealLocalSmoke -v` 已在开发机使用本机 Ollama `llama3.2`、临时 MP4 与 `ffmpeg-full` 通过导入→方案→执行→探测输出（4.60 秒），无远程访问。完成的 `clip_jobs` 在 Electron 中显示“在 Finder 中查看”，只在用户点击时复用既有受限 `openPathInFinder` 桥接定位 `output_path`；网页端不显示。隔离 Playwright mock 桌面桥接验证路径透传，2026-07-17 全量前端 15、Go short/vet、隔离 E2E 18、桌面包均通过。
- 发布任务意图可见：`publish_jobs.mode` 本已在 API 持久化返回，但任务中心原先只显示 `success`，会让 `dry_run` 被误读为已发布。现在任务行和详情都明确显示“演练 · 未发布 / 仅准备 · 不自动提交 / 尝试自动提交”；未知历史意图要求审查。纯函数、前端静态与隔离 Playwright 覆盖 `success + dry_run`，不触发平台、登录或模型。
- Registry 账号被动校验：`account.check` 不再是 `TOOL_NOT_IMPLEMENTED`，现在复用 CLI 的 `logincheck.VerifyPassive`。它只附着当前账号已运行的 loopback CDP，仅读取目录审核过的 Cookie 名，不导航、不读 Cookie 值；未运行浏览器返回 `ACCOUNT_BROWSER_NOT_RUNNING`，也绝不以 SQLite 旧 `logged_in` 伪造新验证。稳定错误码加入 observability allowlist；临时数据目录回归覆盖该 fail-closed 路径。
- Fleet 批次补全：`internal/fleet.Service` 支持用户显式 `start` / `stop` / `observe`，复用既有 `batch_runs` / `batch_items` 与 BrowserFacade 接口。`observe` 不启动、不导航，发现未运行时保留 `waiting_human / manual_takeover`；无调度器、无自动发布。并发上限 5；每 profile 另有 owner-bound `browser_operation_leases`，避免并发 Agent/CDP 动作。
- 端口竞争收口：`cdp_port_leases` 在 `15101–15199` 内为启动过程创建短 TTL SQLite 原子预约；starting runtime 持久化后才释放，修复多进程“扫描空闲端口后再启动”的窗口。迁移 `005/006` 与运行时 schema 保持 parity。
- Fleet Registry/看板：`fleet.run`（显式 `confirm=true`）和 `fleet.status` 进入统一 Tool Registry；`/api/fleet/runs` 仅返回脱敏批次/条目状态；React 新增“浏览器舰队”页。看板不暴露 CDP URL、profile 路径或任何凭证。ADR：`memory/evolution/adr/0011-browser-fleet-orchestrator.md`。
- 2026-07-17 Agent 模型最终收口（取代上方所有旧 Agent 本机模型、OpenCode 与 LLM 配置记录）：聊天、工具协同、内容外发辅助、浏览器规划、BrowserOS 注入与 `llm` CLI 统一固定直连方舟 `https://ark.cn-beijing.volces.com/api/v3/chat/completions`，模型固定 `doubao-seed-2-0-pro-260215`。这些路径不读取 `llm.json`、环境变量、fallback、路由或代理；密钥只读 macOS Keychain 项 `alpha-nexus.doubao.api_key`（账户 `alpha-nexus`）。UI 和状态接口仅给出是否已配置，不显示密钥、地址或钥匙串来源；账号发布代理池不受影响，Studio 粗剪仍为独立本机 Ollama 合约。Go short/vet、前端 lint/build、隔离 Playwright 19 项及 `build.sh` 已通过；已按授权进行一次真实豆包短请求，但服务端返回 401（密钥不存在），未自动重试，需替换有效密钥后再单次验收。

- 2026-07-17 Tool Registry：`meta.login` / `meta.discover` 从 plan 桩改为真实实现，统一要求 `account_id` + 目标账号已运行的 loopback Facade CDP；拒绝非 Meta 平台、禁用账号与调用方自带 CDP。登录只打开 Business Suite 并标记 `manual_takeover`，不写登录态、不存凭证；发现复用 `meta.Discover`，仅落本地资产名称，不宣称可自动提交。离线回归覆盖非 Meta 拒绝、浏览器未运行与旧 `profile` 参数拒绝。

- 2026-07-17 代理控制面精简：唯一新建/修改入口是 SQLite `proxy_endpoints` 与账号 `BindProxyEndpoint`；移除了全局代理模式、IP 池 API 拉取、Clash 多端口映射、连通性测试、Electron webview partition 代理、账号创建/指纹表单的手填代理及对应 API。BrowserFacade 只消费账号 profile 的已保存端点快照；历史 profile 的快照继续可启动，避免数据丢失。旧 `config.json` 代理字段保持原样但不执行、不展示，设置保存不得顺带删除用户旧数据；不实现质量评分、健康检查、自动轮换或第二个“优质代理池”。
- 2026-07-17 CDP Fleet Observatory：`GET /api/fleet/instances` 只返回账号名、平台、状态、CDP 端口与时间；明确不返回 CDP URL、profile、PID、lease owner 或凭证。React“浏览器舰队”页新增实例卡片和显式置前，单次目标最多 10、动作并发最多 5。长驻 UI 的 `fleet.run` 先持久化并返回 run ID，再后台执行；CLI 仍同步，避免短进程留下无主 worker。重复 Execute 先 CAS claim item，禁止覆盖另一执行者的 running 状态。`go test -race ./internal/fleet ./internal/tools ./internal/ui`、5 个 loopback fake-CDP 启动→端口探测→停止清理、前端 17、Electron 20、隔离 Playwright 20 与 `build.sh` 均通过；未启动真实账号浏览器、未调用模型或外网。后续若 Agent 的导航/DOM 操作要与 Fleet 互斥，必须把 operation lease 绑定到这些调用路径，不能仅依赖 Fleet 生命周期租约。
- 2026-07-17 macOS Fleet 窗口排列：真实 10 个临时 BrowserOS 窗口验收发现辅助功能未授权时 `osascript` 会无期限阻塞 `/api/browser/layout`。窗口控制统一改为有界命令（750ms），排列遇首个失败立即返回而不伪造成功；`TestRunCommandWithTimeoutStopsBlockedWindowControl` 覆盖阻塞子进程。实际临时实例在 5 秒内返回可见 500，Fleet 实例状态不受影响。用户需在 macOS「辅助功能」授权后才可成功平铺/置前窗口。
- 2026-07-17 极简工作区 IA：侧栏只保留“浏览器管理、一键发布、Agent、设置”四个一级入口，设置固定底部；旧全局二级导航和公开主题切换已移除，固定使用克制暗黑 Game 皮肤。账号/Fleet/只读运行概览在 `BrowserWorkspaceView` 内切换，默认进入账号；内容/素材剪辑/模板/批量/记录/报告/通知在 `PublishWorkspaceView` 的准备→发布→结果与高级区内复用。准备步骤使用现有内容、脚本、媒体与剪辑 API，把选中结果直接带入发布；发布可选择多个已有账号资产，每个账号独立调用现有 `publish.run`、生成独立幂等键并展示独立结果。旧 `TabType`、组件、内部跳转和数据保持兼容。代理池原组件仅以 `embedded` 模式折叠进“设置 > 代理与网络”。Fleet 与 Agent 操作现场仅页面加载、手动刷新或动作后刷新，不使用定时轮询；浏览器子进程不接收任何 LLM API 密钥。默认 dry run，自动提交门、账号归属校验、显式触发、人工接管与平台证据边界保留；人工确认至少需要公开 URL 或外部帖子 ID。未调用模型、真实发布、真实账号浏览器或外部代理；最终测试结果以当次验收输出为准。
- 2026-07-17 极简工作区终审收口：直接输入文案会同时写入 `caption/body`，可通过 XHS dry-run 预检；新建草稿在媒体关联失败后保留并复用，已有内容已关联其他媒体时拒绝追加不同文件。多账号发布按内容、账号、模板和模式保存稳定幂等键，响应丢失后的重复点击复用原键；跨平台模板和自动提交使用派生安全值。关闭 dry-run 后必须单独勾选本次真实任务确认，执行后自动清除。浏览器进程环境会过滤名称含 key/token/secret/password/credential 的父进程凭证变量；Agent 模型计划中的所有 `side_effect=browser` 工具均在 Registry 前移除，只允许用户在对应界面单独触发。Electron 已彻底删除 OpenCode/Claude 菜单、preload 与 IPC；首帧直接应用暗黑 Game，避免历史浅色闪屏。
- 2026-07-18 最终本地验收：`go test -short ./...`、`go vet ./...`、前端 22/22 + lint/build、Electron 21/21、隔离 Playwright 26/26、`build.sh` 和 `git diff --check` 全部通过。稳定性复测为前端 22 项连续 3 轮（66/66）、Electron 21 项连续 3 轮（63/63）、发布/浏览器相关 Go 包使用 `-p 1 -count=3` 全通过、关键四入口与逐账号 dry-run E2E 连续 3 轮（6/6）。并行运行 browser/fleet 测试包会争用 15101–15199 固定测试端口，稳定性复测必须串行包执行，不能误判为产品抖动。全程未调用豆包、真实发布、真实账号浏览器或外部代理。
- 2026-07-18 发布重放收口：一键发布以内容、账号、平台、模板和模式组成稳定意图；键保存在本地 UI 存储并最多保留 100 条，因此页面卸载或应用重启后的同一请求仍会复用键。`publish.run` 先验证不可变请求快照，再直接返回已有任务状态，绝不再次进入 Runner；首次真实发布仍在创建任务前检查运行中的目标浏览器。用户必须点击“开始新的发布请求”才会删除当前意图键并生成新任务。数据库、工具和隔离 E2E 都覆盖此路径。
- 2026-07-18 Ollama 移除：上方关于 Studio 本机 Ollama、`llama3.2`、11434 与真实本机 smoke 的记录均为历史，不再是运行时契约。Studio 的方案生成和 Agent 的 Studio 协同统一复用固定豆包网关；直连生成前 UI 会说明并要求用户确认，生成方案本身不会执行 ffmpeg。豆包网关强制直连且 fail-closed，未配置或请求失败时返回明确错误，不能退回本机模型或合成结果。Agent 与设置页不再列出或选择模型；旧后端模型配置 API 仅为兼容旧客户端保留且不由 UI 调用。删除本机真实 smoke，测试只使用注入 completion，不调用模型、ffmpeg、发布或账号浏览器。

- 2026-07-18 核心平台发布参数：小红书、抖音、视频号、快手复用既有 SettingsSchema → 模板 → `publish_jobs.settings_json` 不可变快照路径；一键发布可仅在单一平台账号选择时为当前请求覆盖安全参数，并将归一化 JSON 纳入幂等意图。抖音、视频号、快手的 Browser Adapter 只离线校验快照版本与平台归属；当前仍为 dry-run 或打开创作后台后的 `manual_takeover`，绝不将这些参数写入第三方页面。任何自动应用必须在该平台取得独立浏览器证据后另行实现与验收。隔离 E2E 仅 mock 本地 Tool Registry，不会访问账号、模型或平台。

- 2026-07-18 BrowserOS 指纹运行时：`NewServer` 必须把持久化的 `browseros_path` / `browser_engine` 传给既有 `BrowserFacade`，新建账号使用当前引擎，旧 profile 保留自身 `browser_type`；设置即时更新也同步 Facade。临时 profile 实测本机两份 BrowserOS.app 都在 CDP 就绪前被终止（exit 137），且 `codesign --verify` / `spctl --assess` 均报告 bundle 附带无效 Finder 元数据。不得自动去除 xattr、重签或替换用户浏览器；在用户从官方来源修复 BrowserOS.app 后，再用单账号、只读 CDP 烟测验收。
- 2026-07-18 豆包 Agent 连通性：经用户单次授权执行 `ALPHA_NEXUS_REAL_DOUBAO_SMOKE=1 go test ./internal/llm -run '^TestLiveDoubaoComplete$'`，固定直连方舟与固定模型实际返回 `401 AuthenticationError: The API key doesn't exist`。因此故障在当前钥匙串密钥的方舟侧有效性，不是 Agent 路由、Ollama 或本地回退；不得重试、伪造成功或把密钥写入代码/日志。用户更新有效密钥后，才可在再次单次授权下复测。
- 2026-07-18 豆包密钥恢复入口：旧实现只读 macOS Keychain，无法替换失效 Key。现复用 `PUT /api/agent/models` 接收可选 `api_key`，只写 `alpha-nexus.doubao.api_key` Keychain 项、响应不回显且绝不写 `agent.json`；设置页密码输入保存后立刻清空且不会自动模型请求。`security add-generic-password -w` 会要求同一值两次，必须经 stdin 双次输入，避免出现在命令参数。真实写 Key 可能触发 macOS 授权弹窗；无交互环境返回授权取消应显式报错，不能伪造已保存或已连通。
