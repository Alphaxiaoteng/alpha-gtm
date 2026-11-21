# 在哪里修改 Alpha-Nexus

这是一份代码导航参考。先用“常见改动入口”定位文件，再按对应模块追踪调用链。架构约束以 [`AGENTS.md`](../AGENTS.md) 为准，源码和本轮测试结果优先于本文。

## 开始前先确认真源

Alpha-Nexus 使用一套能力面服务命令行界面（CLI）、本地界面和 Tool Agent。不要在界面或桌面壳中复制业务逻辑。

| 能力 | 真源 | 不要旁路到 |
|---|---|---|
| 浏览器生命周期 | `internal/egolite.EgoRuntime`、`EgoSpaceService` | Chromium/CDP、旧 `BrowserFacade` |
| 账号与任务数据 | SQLite `~/.alpha-nexus-egolite/app.db` | 前端状态、JSON 缓存 |
| 应用能力目录 | `internal/tools.Registry` | 界面私有实现 |
| 发布状态 | `internal/publish`、`publishplan`、`publishworker` | React 或 Electron 点击流 |
| 平台边界 | `internal/platforms` | 前端写死平台能力 |
| 诊断事件 | `internal/observability` 与 SQLite 事件表 | `console.log` 原始数据 |

## 运行时调用链

所有入口最终调用 Go 控制面。产品运行时只通过 `ego-browser nodejs` 操作账号绑定的 EgoLite Space；不启动 Chromium、不分配 CDP 端口，也不管理 profile 目录。

```text
CLI / React UI / Electron / Tool Agent
                  |
                  v
      tools.Registry + local HTTP API
                  |
                  v
 SQLite + domain services + EgoRuntime / EgoSpaceService
                  |
                  v
 ego-browser nodejs + EgoLite Space
```

发布调用链如下：

```text
PublishView / CLI / Tool
        |
        v
publishplan: freeze, validate, approve
        |
        v
EgoLite publish runtime -> reviewed platform adapter
        |
        v
prepare（不点最终按钮）/ submit-bound 单次授权 / 结果核验
```

## 仓库入口

这些目录定义应用入口、前端、桌面壳和测试。

| 路径 | 职责 | 入口 |
|---|---|---|
| `cmd/alpha-nexus-egolite` | CLI 与本地 HTTP 服务进程 | `main.go` |
| `cmd/wails-app` | Wails 兼容壳 | `main.go` |
| `internal` | Go 控制面和全部业务包 | 按下表定位 |
| `ui-web` | 当前 React 操作台 | `src/main.tsx`、`src/App.tsx` |
| `internal/ui/dist` | React 构建产物，由 Go 嵌入 | 不直接编辑 |
| `electron` | 当前桌面壳、Sidecar 管理和 IPC | `main.js` |
| `migrations` | SQL 迁移参考 | `*.sql` |
| `e2e` | Playwright 端到端测试 | `tests/*.spec.ts` |
| `scripts` | 构建、冒烟和测试脚本 | 按脚本名定位 |
| `memory` | 当前事实、架构决策记录和踩坑索引 | `evolution/CURRENT.md` |
| `docs` | 产品、架构和操作契约 | 本文末尾的文档表 |

## Go 包地图

Go 包按职责分组。新代码先复用现有包，不新建平行服务。

### 入口与能力适配

| 包 | 职责 | 关键文件 |
|---|---|---|
| `internal/cli` | CLI 命令注册与参数适配 | `cli.go`、`ego_commands.go`、`publish_ego_command.go` |
| `internal/ui` | 本地 HTTP API、前端嵌入、界面适配 | `server.go`、`*_handlers.go`、`assets.go` |
| `internal/tools` | 全应用 Tool Registry 和运行时 | `tools.go`、`catalog.go`、`handlers.go` |
| `internal/mcp` | Model Context Protocol（MCP）适配 | `server.go`、`tools_registry.go` |
| `internal/sidecar` | Go Sidecar 运行描述文件 | `runtime.go` |

### 账号与浏览器

| 包 | 职责 | 关键文件 |
|---|---|---|
| `internal/egolite` | EgoRuntime、矩阵 Space、并发调度与登录监控 | `runtime.go`、`service.go`、`scheduler.go`、`monitor.go` |
| `internal/selectors` | 登录观察与平台选择器目录 | `catalog.go`、`catalog.v1.json` |

`internal/browser`、`internal/fleet`、`internal/logincheck` 和 `internal/sessionbackup` 是未注册的兼容代码，不属于 EgoLite 产品执行链；新入口不得调用。

### 发布与平台

| 包 | 职责 | 关键文件 |
|---|---|---|
| `internal/platforms` | 23 个平台目录、能力边界、Publisher 契约 | `platforms.go`、`adapter.go`、`all_adapters.go` |
| `internal/platforms/meta` | 旧 Meta 资产兼容代码；产品入口使用单一 `meta` 账号和 EgoLite Business Manager 路由 | `discover.go`、`publisher.go` |
| `internal/publish` | 旧 dry-run 契约与兼容状态机；live 入口固定拒绝 | `status.go`、`runner.go` |
| `internal/publishguard` | 账号、资产和发布目标归属校验 | `guard.go` |
| `internal/publishplan` | 冻结、单次审批、幂等与证据计划 | `service.go` |
| `internal/egolite` | EgoLite 发布准备、单次提交和平台核验 | `publish_runtime.go`、`creator_metrics.go` |
| `internal/batch` | 批量运行和 NDJSON 辅助 | 包内 `*.go` |
| `internal/queue` | 本地任务队列、claim 和 resume | `queue.go` |

### 内容、素材与外部能力

| 包 | 职责 | 关键文件 |
|---|---|---|
| `internal/media` | 素材索引、SHA-256、预检 | `media.go` |
| `internal/studio` | 本地脚本与剪辑方案 | `agent.go`、`clip.go` |
| `internal/agnes` | 显式触发的图片和视频生成客户端 | `client.go`、`image.go`、`video.go` |
| `internal/imagegen` | zexapi 极简生图客户端（prompt + 可选参考图，参数后端固定） | `client.go` |
| `internal/xhs` | 小红书后台数据采集 | `scraper.go` |
| `internal/tikhub` | TikHub 激活、精选接口、调用与本地原始快照 | `provider.go`、`endpoints.go`、`call_storage.go` |
| `internal/importers/turbopush` | TurboPush 本地数据迁移 | `importer.go` |

### 插件（非 Go）

| 路径 | 职责 | 关键文件 |
|---|---|---|
| `plugins/alpha-nexus-creation` | DSH 插件骨架：一个 tool 调本地 `/api/creation/workflows`（只读链路，未在真实 dsh 装载） | `index.js`、`cordis.patch.yml`、`README.md` |

### Agent 与基础设施

| 包 | 职责 | 关键文件 |
|---|---|---|
| `internal/agentcfg` | Agent 本地配置 | `config.go` |
| `internal/agenthub` | 全局 Agent 会话、Provider 和 Run；`bots.go` 管理多 Bot 人格与头像；`dsh_sync.go` 1:1 镜像 `~/.dsh` 的 provider/model 目录 | `service.go`、`providers.go`、`store.go`、`bots.go`、`dsh_sync.go` |
| `internal/skilllib` | 本地 Skill 目录、标准包、启停与 Agent 指令解析 | `library.go` |
| `internal/agentws` | Agent 工作区上下文 | `workspace.go` |
| `internal/agents` | Content、Adapt、Publish、Monitor、Analytics Agent | `agents.go` |
| `internal/llm` | 固定豆包网关 | `doubao.go` |
| `internal/terminal` | 受控本地终端和聊天进程 | `session.go`、`spawn.go`、`chat.go` |
| `internal/db` | SQLite 打开、迁移、查询和诊断快照 | `db.go`、`repository.go`、`diagnostics.go` |
| `internal/config` | 数据目录、端口和浏览器配置 | `config.go` |
| `internal/auth` | 本地界面认证 | `auth.go` |
| `internal/notify` | 本地通知设置与发送 | `service.go`、`sender.go` |
| `internal/observability` | 脱敏结构化事件与轮转 JSONL | `event.go`、`emitter.go`、`file.go` |
| `internal/trace` | Trace ID 和步骤关联 | `trace.go` |
| `internal/errors` | 跨 CLI、API 和任务的稳定错误码 | `errors.go` |

## 常见改动入口

按用户动作定位代码，比按技术层逐目录查找更快。

| 目标 | 先看 | 继续追踪 |
|---|---|---|
| 新增浏览器与平台账号 | `internal/ui/ego_handlers.go`、`internal/db/repository.go` | `BrowserWorkspaceView.tsx`、Ego 身份/Space 测试 |
| 登录检测或头像同步 | `internal/egolite/service.go`、`internal/ui/ego_handlers.go` | `BrowserWorkspaceView.tsx`、`account_login_events` |
| CSDN 等平台检测 | `internal/egolite/service.go` 的 reviewed login rules | 平台目录、EgoLite 登录核验测试 |
| 浏览器打开与 Space 接管 | `internal/egolite/service.go`、`runtime.go` | `ego_handlers.go`、`BrowserWorkspaceView.tsx` |
| 掉线监控与恢复 | `internal/egolite/monitor.go` | `ego_handlers.go`、通知与监控测试 |
| 平台列表和能力档位 | `internal/platforms/platforms.go` | `all_adapters.go`、`settings_schema.go` |
| 一键发布和 dry-run | `internal/publishplan`、`internal/egolite/publish_runtime.go` | `PublishView.tsx`、`publish_plan_handlers.go` |
| 发布结果和失败证据 | `internal/egolite/service.go`、`internal/publishverify` | `publish_plan_handlers.go`、`PublishRecordsView.tsx` |
| 本地知识库与 RAG | `internal/knowledge`、`knowledge_handlers.go` | `KnowledgeView.tsx`、`CreationWorkbenchView.tsx` |
| 全平台 7 日数据 | `internal/egolite/creator_metrics.go`、`internal/tools/social_metrics.go` | `XHSDataDashboardView.tsx`、metrics 测试 |
| 内容库、素材和排期 | `content_handlers.go`、`content_library_handlers.go` | `LocalContentView.tsx`、`ContentScheduleCalendar.tsx` |
| AI 生图和视频生成 | `ai_image_handlers.go`、`ai_media_generation_handlers.go` | `AIImageWorkspaceView.tsx`、`internal/agnes` |
| Agent 会话和 Provider | `internal/agenthub` | `agent_hub_handlers.go`、`AgentDrawer.tsx` |
| Skill 库、导入和启停 | `internal/skilllib` | `skill_library_handlers.go`、`SkillLibraryView.tsx` |
| 本地日志和诊断 | `internal/observability`、`internal/db/diagnostics.go` | `diagnostic_handlers.go`、`electron/diagnostics.js` |
| 新表或字段 | `internal/db/db.go` | `migrations/*.sql`、`docs/SCHEMA.md` |
| 新本地 API | `internal/ui/server.go` 的 `routes()` | 对应 `*_handlers.go` 与测试 |
| TikHub 接口与调用记录 | `internal/tikhub/endpoints.go`、`data_api_calls_handlers.go` | `DataAPISettingsSection.tsx`、`tikhub_api_calls` |
| 新 CLI 命令 | `internal/cli/cli.go` | `commands.go` 或领域包 |
| 新平台 Publisher | `internal/platforms/<platform>` | 注册到平台目录并补契约测试 |

## React 操作台地图

`ui-web` 是当前界面真源。`internal/ui/dist` 只保存构建结果。

| 文件 | 职责 |
|---|---|
| `src/main.tsx` | React 入口、主题初始化、前端诊断边界 |
| `src/App.tsx` | 认证门禁与工作区分发 |
| `src/api.ts` | `/api/*` 类型和请求适配 |
| `src/store.ts` | Zustand 界面状态、账号选择和 Agent Drawer |
| `src/components/Layout.tsx` | 一级导航、日历入口、Agent Drawer 入口 |
| `BrowserWorkspaceView.tsx` | 浏览器身份、平台账号、Space、登录与监控 |
| `AccountsView.tsx`、`FleetView.tsx` | 旧界面兼容组件；当前工作区不路由到旧 CDP 操作 |
| `SocialMediaWorkspaceView.tsx` | 视频与文章内容入口 |
| `LocalContentView.tsx` | 内容库、素材、排期和草稿 |
| `ContentScheduleCalendar.tsx` | 时间线与日历预览，不触发定时发布 |
| `PublishWorkspaceView.tsx` | 发布、记录、指标和通知入口 |
| `PublishView.tsx` | 冻结、审批和准备发布 |
| `PublishRecordsView.tsx` | 任务状态、证据和人工确认 |
| `AIImageWorkspaceView.tsx` | AI 图片工作区 |
| `AgentDrawer.tsx`、`AgentView.tsx` | 全局 Agent 面板 |
| `SkillLibraryView.tsx` | 本地 Skill 目录、创建、导入、审核状态和启停 |
| `SettingsView.tsx` | 浏览器、内容目录、通知、代理和诊断设置 |
| `ClientErrorBoundary.tsx` | React 崩溃回退与脱敏指纹 |

旧 `ui/` 是遗留静态界面。新界面功能写入 `ui-web`。

## 本地 API 分组

路由注册表位于 `internal/ui/server.go` 的 `routes()`。处理器按领域拆入 `internal/ui/*_handlers.go`。

| 路由前缀 | 领域 |
|---|---|
| `/api/auth/*` | 本地认证 |
| `/api/ego/*`、`/api/accounts/{id}/space/*` | 浏览器身份、Space、登录批次、核验与串行监控 |
| `/api/accounts*`、`/api/account-tags*` | 平台账号、头像与本地标签 |
| `/api/contents*`、`/api/content-library/*`、`/api/media/*` | 内容与素材 |
| `/api/publish-plans*`、`/api/jobs/*`、`/api/publish-records` | 发布计划、任务和记录 |
| `/api/batch/*` | 批量发布 |
| `/api/agent/*`、`/api/v1/tools/*` | Agent Hub 与 Tool Registry |
| `/api/skills*` | 本地 Skill 目录、标准 Markdown 导入和启停 |
| `/api/ai-image/*`、`/api/scripts*`、`/api/clip-jobs*` | AI 媒体与本地 Studio |
| `/api/operations/events`、`/api/diagnostics/*` | 操作时间线与脱敏诊断 |
| `/api/config/*`、`/api/proxies*`、`/api/notification-settings*` | 本地设置 |
| `/api/data/*`、`/api/metrics*` | 数据采集与指标 |
| `/api/data-api/*` | TikHub 配置、精选接口调用、长期记录与原始响应回读 |

## 本地数据目录

运行时数据不进入仓库。不要读取、打印或提交敏感目录。

```text
~/.alpha-nexus-egolite/
  app.db             SQLite 资产真源
  config.json        本地配置
  account-avatars/   本地规范化平台头像
  account-asset-avatars/  Meta 资产头像
  media/ edits/      本地素材索引与生成产物
  screenshots/       失败证据，可能敏感
  logs/              安全事件与脱敏诊断快照
  knowledge/         本地知识库解析产物（如有磁盘附件）
  tikhub/            可选采集结果
```

日志结构、保留和排障步骤见 [`docs/DIAGNOSTICS.md`](DIAGNOSTICS.md)。数据库表和迁移规则见 [`docs/SCHEMA.md`](SCHEMA.md)。

## 文档入口

每次只读当前任务需要的文档。

| 任务 | 文档 |
|---|---|
| 项目规则 | [`AGENTS.md`](../AGENTS.md) |
| 当前可靠事实 | [`memory/evolution/CURRENT.md`](../memory/evolution/CURRENT.md)（按 area） |
| 历史踩坑 | [`memory/evolution/mistakes/INDEX.md`](../memory/evolution/mistakes/INDEX.md)（命中再开） |
| 架构决策 | `memory/evolution/adr/`（单个 ADR） |
| 当前 Goal | [`agentpush/GOAL_ALPHA_GAMER_LAUNCH.md`](agentpush/GOAL_ALPHA_GAMER_LAUNCH.md) |
| 过时资料 | [`archive/`](archive/)（默认不读） |
| 数据库 | [`docs/SCHEMA.md`](SCHEMA.md) |
| 浏览器引擎 | [`docs/BROWSER_ENGINES.md`](BROWSER_ENGINES.md) |
| CLI | [`docs/CLI.md`](CLI.md) |
| Tool Agent 工作流 | [`docs/AGENT_WORKFLOW.md`](AGENT_WORKFLOW.md) |
| Meta 发布 | [`docs/META_AUTOMATION.md`](META_AUTOMATION.md) |
| 小红书与 TikHub | [`docs/TIKHUB.md`](TIKHUB.md) |
| 日志与排障 | [`docs/DIAGNOSTICS.md`](DIAGNOSTICS.md) |
| 测试 | [`docs/TDD_STRATEGY.md`](TDD_STRATEGY.md)、[`docs/E2E_PLAYWRIGHT.md`](E2E_PLAYWRIGHT.md) |

## 构建与验证

按改动范围运行最小测试，再执行全仓串行回归。

```bash
go test ./internal/affected-package -count=1
go test -p 1 ./... -count=1
pnpm --dir ui-web run build
pnpm --dir ui-web run lint
```

前端构建后同步嵌入资源：

```bash
rsync -a --delete ui-web/dist/ internal/ui/dist/
```

端到端测试使用 `./scripts/e2e-run.sh`。桌面壳测试在 `electron` 目录运行 `node --test`。

## 不能跨越的边界

这些边界决定改动应该落在哪一层：

1. 只通过 `EgoRuntime` / `EgoSpaceService` 管理矩阵 Space
2. 每个矩阵一个持久 Space，同矩阵平台共享该 Space
3. UI 与 Electron 只做适配，不实现发布逻辑
4. 真实发布必须获得当次明确授权
5. DOM 不稳定时返回 `manual_takeover`，不能伪造 `published`
6. 验证码、双重验证和 checkpoint 交给人工处理
7. 不保存平台密码，不记录 Cookie、正文、完整 URL 或 profile 路径
8. 不新增自动定时发布、周期采集、云后台或公网 CDP

## 当前实现缺口

这些目录存在不代表平台已经通过真实账号验收：

- **Meta**: 单一登录身份已支持打开账号绑定的 Business Manager 页面；真实发布证据仍未验收
- **小红书**: 仅登录核验与只读数据回收，不自动发布
- **CSDN 与抖音**: 已有 EgoLite prepare 契约；最终提交仍要求冻结计划、Electron 单次授权和真实后台证据
- **其他平台**: 未取得真实创作者后台证据时只允许 prepare、人工接管或标记未验证
- **X**: 只支持 legacy API dry-run，live 请求以 `API_CONNECTOR_UNAVAILABLE` fail closed
- **Electron 分发**: 本地可构建 unsigned macOS 包，外部分发仍需签名和 Apple 公证
- **平台 Native 化**: 新增选择器前必须取得真实 fixture，并补失败证据测试
