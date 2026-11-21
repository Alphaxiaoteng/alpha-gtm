# Alpha-Nexus EgoLite

AI-native **本地**多平台账号、内容与发布控制台，面向内容商业与电商联动逐步扩展。

> **公共产品身份（v0.1）：PLEIAD** — AI 原生全渠道商业运营平台。规划域名：`pleiadops.com`（尚未注册）；内部仓库名和运行时路径暂不变更。品牌与域名边界见 [BRAND_GUIDELINES.md](BRAND_GUIDELINES.md) 和 [docs/PUBLIC_SITE.md](docs/PUBLIC_SITE.md)。

- **本地 Web 管理界面** 是唯一管理界面；CLI 与 Agent 复用同一 Tool Registry
- **EgoLite** 只承载平台页面；一个账号矩阵共享一个持久 Space
- 不启动 Chromium、Chrome，不管理 CDP 端口或浏览器 profile
- 状态：`SQLite`（`~/.alpha-nexus-egolite`）
- 主线栈：**Go**（不迁 Node monorepo）

> 不是云后台，不是验证码破解工具。不绕 2FA。

---

## AgentPush

产品愿景在本仓用 Go 主线落地。**当前 Goal：** [docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md](docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md)。

栈决策：[ADR 0005](memory/evolution/adr/0005-agentpush-via-alpha-nexus-egolite-stack.md) · [ADR 0007](memory/evolution/adr/0007-stack-hybrid-go-control-plane.md)。找代码：[docs/MAP.md](docs/MAP.md)。过时 PRD/计划：[docs/archive/](docs/archive/)。

核心包：`internal/publish` · `platforms` · `media` · `queue` · `agents` · `llm` · `trace`。

Alpha-Nexus EgoLite 只执行用户或 Agent 当次明确发起的发布任务；不提供本地定时发布或周期内容采集。登录状态监控只读检查既有 Space，可关闭，默认每 60 分钟串行执行。

---

## 平台范围

| 能力档 | 平台 | 当前边界 |
|--------|------|----------|
| 浏览器自动化尝试 | Instagram / Facebook | 支持 dry-run；只有用户明确请求时可尝试自动提交。真实账号验收未完成；无点击后新增成功证据就进入 `awaiting_review` |
| 安全人工接管 | 小红书、抖音、视频号、快手、微信公众号及其余 12 个内容平台 | 支持任务创建与 dry-run；只打开已运行账号的创作后台，不自动点击发布 |
| API 占位 | YouTube / TikTok / X | 仅 dry-run；live 明确返回 `API_CONNECTOR_UNAVAILABLE`，不会外发正文、媒体路径或凭证引用 |
| 不可发布 | custom | 无创作后台和已实现 Connector，不能创建发布任务 |

平台目录共 23 项；除 `custom` 外的 22 项均有显式 Publisher 契约和离线演练。人工接管后的成功记录只能由用户明确确认，来源标记为 `user_confirmed`。

---

## 架构一览

```text
Web 管理界面 / Tool Registry / CLI
  → SQLite + EgoRuntime / EgoSpaceService
  → publish / queue / agents / media
  → ego-browser nodejs → EgoLite Space
  → 平台准备 · 创作后台人工接管 · 受批次确认约束的提交
```

数据目录：

```text
~/.alpha-nexus-egolite/
  app.db
  config.json
  media/ screenshots/ logs/
  runtime.json
```

---

## 环境要求

- Go **1.22+**（仓库 `go.mod` 以实际为准）
- Node.js（构建 `ui-web`）
- [EgoLite](https://lite.ego.app/) 与随应用提供的 `ego-browser`

---

## 快速开始

```bash
# 1. 克隆
git clone <repository-url> Alpha-Nexus-egolite
cd Alpha-Nexus-egolite

# 2. 依赖
go mod download
cd ui-web && npm install && cd ..

# 3. 编译 CLI（可选：完整构建含前端 embed）
go build -o alpha-nexus-egolite ./cmd/alpha-nexus-egolite
# 或
./build.sh

# 4. 初始化数据目录
./alpha-nexus-egolite init --data-dir ~/.alpha-nexus-egolite

# 5. 看命令
./alpha-nexus-egolite --help
```

### 常用命令

```bash
# EgoLite 诊断与矩阵
./alpha-nexus-egolite ego doctor
./alpha-nexus-egolite ego identities
./alpha-nexus-egolite ego spaces

# 账号核验与发布均使用 EgoLite；最终提交所需单次授权在管理界面里确认
./alpha-nexus-egolite ego verify --identity-id 1 --platform douyin
./alpha-nexus-egolite publish batch --plan-id 1 --snapshot-hash <hash> --approval-id <id> --nonce <nonce> --action prepare

# 管理界面（本地 HTTP 服务，用浏览器打开 http://127.0.0.1:17421）
./alpha-nexus-egolite ui serve --port 17421
```

### AgentPush 扩展命令（目标面）

```bash
./alpha-nexus-egolite media import|index|preflight
./alpha-nexus-egolite content version|adapt
./alpha-nexus-egolite queue enqueue|list|run  # 仅兼容 dry-run；live 固定拒绝
./alpha-nexus-egolite agent content|adapt|publish|monitor|analytics
./alpha-nexus-egolite metrics list|collect
./alpha-nexus-egolite audit list
./alpha-nexus-egolite export data
```

live 发布只接受冻结计划；prepare 不点击最终按钮，submit 必须消费管理界面确认的 submit-bound 单次授权。

### Tools / 工作流（Tool Agent 能力面）

CLI 不是唯一形态：**Tool Agent** 通过同一 Registry 调用 Ego Space、账号、发布和本地能力；旧 `publish.run` 只保留 dry-run 兼容且不向 Agent 暴露。

```bash
./alpha-nexus-egolite tools list
./alpha-nexus-egolite tools schema
./alpha-nexus-egolite tools call ego.matrix.create --args '{"name":"矩阵二"}'
./alpha-nexus-egolite tools call ego.matrix.configure-platforms --args '{"identity_id":2,"platform_codes":["douyin","youtube"]}'
./alpha-nexus-egolite agent tools-run --json '{"goal":"health","steps":[{"tool":"account.list","args":{}}]}'
```

矩阵通用操作 → [docs/EGOLITE_MATRIX_OPERATIONS.md](docs/EGOLITE_MATRIX_OPERATIONS.md)  
工作流 → [docs/AGENT_WORKFLOW.md](docs/AGENT_WORKFLOW.md)。

### TikHub（榜单 / 用户监控 / 搜索 · 可被 tools 调用）

```bash
export TIKHUB_API_KEY=...
# 或: ./alpha-nexus-egolite tikhub config set-key --key ...

./alpha-nexus-egolite tikhub rank daily          # 每日 Top 榜单抓取（JSON+MD）
./alpha-nexus-egolite tikhub update              # 每日一键：榜单 + 用户
./alpha-nexus-egolite tools call tikhub.update --args '{"confirm":false}'
```

详见 [docs/TIKHUB.md](docs/TIKHUB.md)。找代码 → [docs/MAP.md](docs/MAP.md)。

### 管理界面

```bash
./alpha-nexus-egolite ui serve --port 17421
```

启动后用浏览器打开 http://127.0.0.1:17421。界面只提供本地 HTTP 服务，不含桌面壳。

---

## 项目结构

```text
cmd/alpha-nexus-egolite/     CLI 入口
internal/
  egolite/           EgoRuntime、Space、登录批次与串行状态监控
  cli/               Cobra 命令
  db/                SQLite
  ui/                本地 HTTP API + 嵌入前端
  publish/           发布状态机 + Runner
  platforms/         平台注册 + Publisher（meta/…）
  media/ queue/ agents/ llm/ trace/
  batch/ importers/ xhs/ tikhub/ ...
ui-web/              React 操作台
e2e/                 Playwright
docs/                设计与规范（含 agentpush/ · evolution/）
migrations/          SQL 参考
```

更细的「改哪看哪」→ **[docs/MAP.md](docs/MAP.md)**  
Agent / 开发规范 → **[AGENTS.md](AGENTS.md)**

---

## 文档索引

| 文档 | 内容 |
|------|------|
| [docs/MAP.md](docs/MAP.md) | 项目地图、包职责 |
| [docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md](docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md) | **当前 Goal** |
| [memory/](memory/README.md) | 薄记忆：CURRENT / mistakes / ADR |
| [docs/CLI.md](docs/CLI.md) | CLI 契约 |
| [docs/SCHEMA.md](docs/SCHEMA.md) | SQLite 表 |
| [docs/UI.md](docs/UI.md) | UI 约定 |
| [docs/META_AUTOMATION.md](docs/META_AUTOMATION.md) | Meta 发布流程 |
| [docs/PLATFORM_LITE.md](docs/PLATFORM_LITE.md) | Lite 平台边界 |
| [docs/E2E_PLAYWRIGHT.md](docs/E2E_PLAYWRIGHT.md) | E2E |
| [docs/EGOLITE_MATRIX_OPERATIONS.md](docs/EGOLITE_MATRIX_OPERATIONS.md) | 矩阵、平台、登录、监控与资源边界 |
| [docs/archive/](docs/archive/README.md) | 过时文档（默认不读） |

---

## 开发

```bash
go test ./...
./scripts/e2e-run.sh          # 需先起 UI / 按文档准备
./build.sh                    # ui-web build → embed → 多平台二进制到 dist/
```

本轮只验证浏览器版管理界面，不生成正式发布包。

### Git hooks 与自动提交

克隆后安装一次版本化 hooks（写入本仓 `core.hooksPath`，不污染全局）：

```bash
./scripts/install-git-hooks.sh
```

| 脚本 / Hook | 作用 |
|-------------|------|
| `scripts/auto-commit.sh` | 暂存变更并生成**有正文**的 Conventional Commit；`--dry-run` 只预览；`--push` 提交后推送；`-m "..."` 指定 subject |
| `scripts/git-hooks/commit-msg` | 拒绝空提交、过短 subject、`wip`/`tmp` 等占位信息 |
| `scripts/git-hooks/pre-push` | 拒绝 `main`/`master` 非快进推送；检查 tip 提交信息 |

```bash
./scripts/auto-commit.sh -m "feat(tools): wire meta discover"
./scripts/auto-commit.sh --push -m "chore: sync local work"
```

运行时目录 `.e2e-data/`、`.alpha-nexus-egolite-run/`、`~/.alpha-nexus-egolite` 不会进入提交。

原则摘要：

- 管理界面只调用本地 API / Registry，不写独立发布逻辑
- 长任务输出 **NDJSON**；失败保留截图与 error code
- 同一 Space 同时只跑一个任务；默认最多 3 个活跃 Space
- 登录批次最多 3 个标签；后台监控固定串行

---

## 安全注意

- 不读取或修改 EgoLite Profile、Default、Cookie 或密码存储
- 不保存平台密码；不自动绕过验证码 / 2FA / checkpoint
- Cookie、token 不要打进普通日志

---

## License

未单独声明时，以仓库所有者约定为准。欢迎提 Issue / PR。
