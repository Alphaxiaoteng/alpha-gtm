# Alpha-Nexus Agent Guide

AI-native **本地**社媒工具。本地 Web 管理界面是唯一管理界面；自动化工作流、Tool Agent 与 CLI 复用同一能力面，不写独立发布逻辑。EgoLite 只承载平台页面。

冲突时以本文件为准。细节按任务按需读 `docs/*`，**禁止**进场就通读全部文档。

**项目地图（找代码）→ [`docs/MAP.md`](docs/MAP.md)**  
**EgoLite 矩阵操作 → [`docs/EGOLITE_MATRIX_OPERATIONS.md`](docs/EGOLITE_MATRIX_OPERATIONS.md)**：Go 控制面 + SQLite + EgoRuntime；Playwright 只允许用于 Alpha-Nexus UI E2E。

---

## Agent 入口与读序

`AGENTS.md` 是唯一标准入口。禁止进场通读文档或整库灌入 `memory/`。

1. 当前 Goal（默认 [`docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md`](docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md)）+ 本文件。
2. **仅当需要找包或改代码位置时**再读 [`docs/MAP.md`](docs/MAP.md)；问答/规划/纯文档任务不必开。
3. 仅当任务 area 相关时，才打开 `memory/evolution/CURRENT.md` 或 `mistakes/INDEX.md` 的命中行；再打开单个 lesson / ADR。默认不读 `archive/`。
4. 源码与本轮真实测试优先。有可复用坑写 lesson，有架构取舍写 ADR——不写协议文、不维护第二套文档站。

禁止运行时 Agent、后台任务或模型自动改写规则、源码、Prompt 或权限。

---

默认只开发本地 Web 管理界面（浏览器打开 http://127.0.0.1:17421）；除非用户明确要求，否则不打包桌面应用；打包完成后执行 `go clean -cache`。

---

## 硬约束（不可砍）

| 必须 | 禁止 |
| --- | --- |
| EgoLite + 一浏览器一个持久 Space | 启动 Chromium/Chrome、分配 CDP 端口、管理 profile |
| SQLite 为资产真源（`~/.alpha-nexus-egolite`） | 云后台、多用户、会员、公网暴露 UI |
| 任务经 **tools.Registry / CLI** 调度；长任务 NDJSON | UI/服务绕过 tools 自写发布逻辑 |
| Go 主线实现控制面 | 整仓迁 Node monorepo；绕 2FA/验证码；存平台密码 |
| 同一 Space 互斥；登录批次最多 3 标签 | 读取、导出或记录 Cookie、EgoLite Profile、密码 |
| 批量可 resume；活跃 Space 最多 3，监控并发 1 | 伪造 `published` 或把 `unknown` 误记掉线 |
| 只执行用户或 Agent 当次明确发布动作 | 定时发布、周期内容采集、任意 webhook 假成功 |

平台目录有 23 项；除 `custom` 外的 22 项都有显式 dry-run 契约。目录或演练可用不等于真实平台已验收。

### EgoLite 资源纪律

- 同一用户任务及其继续、重试和检查必须复用同一个 EgoLite Space，不得重复创建浏览器或 Space。
- 平台操作默认严格串行，运行并发固定为 1；同一网站优先复用现有标签页，每个 Space 最多保留 3 个非内部标签页。
- 临时搜索页、验证页用完立即关闭；只有等待用户操作时才保留页面。
- 不得通过杀死整个 EgoLite、关闭其他用户 Space 或强制限速来处理负载，只能停止当前任务自己创建的子进程或页面。

---

## 一句话架构

```text
Workflow / Tool-Agent
  → tools.Registry（全应用能力目录）
  → local HTTP / CLI 薄适配
  → SQLite + EgoRuntime / EgoSpaceService + domain 包
  → ego-browser nodejs · EgoLite Space · 人工接管 · 受控提交
```

入口：`cmd/alpha-nexus-egolite`  
核心包：`internal/{tools,cli,egolite,db,ui,batch,platforms,publish,media,queue,agents,llm,tikhub,trace,importers,imagegen,agenthub}`
浏览器运行时：只允许 `ego-browser nodejs` 调用已审核 helper；产品代码不启动浏览器进程。  
前端：`ui-web/`；管理界面：本地 HTTP 服务（无桌面壳）。

数据目录：`~/.alpha-nexus-egolite/{app.db,media,screenshots,logs,config.json,tikhub/}`。EgoLite Space 由应用数据库绑定，不使用旧 Chromium `runtime.json` 作为运行时真源。

**工作流与 Tool 契约 → [`docs/AGENT_WORKFLOW.md`](docs/AGENT_WORKFLOW.md)**

---

## 平台

| 能力档 | 平台 | 当前边界 |
| ------ | ---- | -------- |
| 浏览器自动化尝试 | Instagram / Facebook | dry-run + best-effort；真实账号验收未完成，无新成功证据就进入审核 |
| 安全人工接管 | 小红书等 17 个创作后台平台 | dry-run + 打开目标账号后台；不自动点击发布 |
| 受控浏览器提交 | YouTube / TikTok | direct live 仅准备；最终点击只允许冻结单目标计划 + submit-bound 单次审批；真实账号验收未完成 |
| API 占位 | X；YouTube/TikTok legacy API adapter | 仅 dry-run；live 固定 fail-closed |
| 不可发布 | custom | 无创作后台或可信 Connector |

Meta 模型：`accounts` = 登录身份；`account_assets` = Page / IG 资产。不要拆成两个无关登录账号。

当前执行章程 → [`docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md`](docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md)。历史资料 → [`docs/archive/`](docs/archive/)。

---

## Ponytail（执行默认 full）

标准库优先 · 少抽象 · 少依赖 · 能 CLI 就不加服务 · 能 SQLite 就不加重型队列中间件。

**不能砍**：EgoLite、SQLite、矩阵共享 Space、批量、账号迁移、TDD、结构化 Registry。  
**可砍**：多余 HTTP 服务、插件系统、未验收的抽象层。

循环：`场景 → 红灯测试 → 最小实现 → 绿灯 → 简化检查`。禁止先写完再补测。

任务结束：可复用坑写 lesson；架构取舍写 ADR。薄记忆即可，不写协议站。

---

## 按任务读文档（不要全读）

| 任务 | 只读 |
| --- | --- |
| **找文件 / 包职责 / 改哪** | **`docs/MAP.md`** |
| **踩坑 / 硬事实（按 area）** | `memory/evolution/CURRENT.md` 或 `mistakes/INDEX.md` → 命中条 |
| **当前 Goal / 验收** | **`docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md`** |
| Schema | `docs/SCHEMA.md` + `migrations/` + `internal/db/` |
| CLI 契约 | `docs/CLI.md` |
| Meta 发布 | `docs/META_AUTOMATION.md` |
| TikHub | `docs/TIKHUB.md` |
| 工作流 / Tool Agent | `docs/AGENT_WORKFLOW.md` |
| 测试 | `docs/TDD_STRATEGY.md`；E2E → `docs/E2E_PLAYWRIGHT.md` |
| 过时资料 | `docs/archive/`、`memory/evolution/archive/`（默认不读） |

---

## 常用命令

```bash
go test ./...
./alpha-nexus --help
./alpha-nexus-egolite init --data-dir ~/.alpha-nexus-egolite
./alpha-nexus-egolite ego doctor
./alpha-nexus-egolite ui serve --port 17421
./scripts/e2e-run.sh
./build.sh
./scripts/install-git-hooks.sh              # 启用 commit-msg / pre-push
./scripts/auto-commit.sh -m "feat: ..."     # 有信息的自动提交；可加 --push
```

浏览器运行时真源：`internal/egolite.EgoRuntime` 与 `EgoSpaceService`。禁止调用旧 `BrowserFacade`、`BrowserManager`、CDP 或直接 exec 起浏览器。

---

## 当前实现缺口（别假装已完成）

- 当前 Goal 见 [`GOAL_ALPHA_GAMER_LAUNCH.md`](docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md)。无创作者后台独立证据不得记为真实发布成功。
- 控制面已接线；平台真发按 Goal 范围逐形态验收。社媒自动发布仅支持平台原生定时填表；趋势雷达支持自动化周期抓取（经 LaunchAgent 由 radar worker 守护）。
- 旧浏览器代码可能仍为历史兼容而存在，但 EgoLite 产品运行时不得调用。
- 管理界面只走本地 HTTP；Electron 壳已移除，不再恢复。

---

## 开发原则（短）

1. Registry 先于 UI；状态必须落库可恢复。
2. 批量是核心；长任务 NDJSON；失败必截图+error_code。
3. 登录/2FA/checkpoint → 人工接管。
4. 大文件在磁盘，DB 只存路径与元数据。
5. 扩大范围对照当前 Goal 与 accepted ADR，不静默砍主线栈。
