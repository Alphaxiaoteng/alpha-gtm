# Turbo Push 全功能解剖与 Alpha-Nexus 集成方案

| 项目 | 结论 |
|------|------|
| 调研日期 | 2026-07-16；账号切换复核 2026-07-24 |
| Turbo Push | 2026-07-24 桌面 UI v2.3.2；主服务 v2.3.6 |
| 集成状态 | Phase 0–2 控制面已落地并经本地测试；内容/平台原生自动化仍按后续阶段推进 |
| 排除范围 | 知识库（含 KB 数据库/日志/Sidecar）、定时发布、Turbo Push 云会员、支付、邀请码 |
| 关键修正 | `.turbopush` 是实时运行目录；`.tp` 只是外部加密导出包 |
| 推荐方案 | Electron 监管单一 Alpha Go Sidecar；Alpha-Nexus 为唯一真源；Turbo Connector 仅作可关闭的过渡桥 |

## 1. 证据等级与安全边界

本文分三类证据：

- 实测：通过 Turbo Push 桌面 UI 逐页操作所得。
- 本机取证：对 `.turbopush` 的目录元数据、SQLite Schema/计数、非敏感配置键、进程、端口、二进制版本和日志结构做只读检查。
- 官方：Turbo Push 官网、官方 MCP 仓库和 API 文档。
- 代码审计：Alpha-Nexus 当前工作树的实际代码。

调研期间没有：

- 点击“开始发布”；
- 修改、删除或导出账号；
- 读取 Cookie、Token、API Key、密码或敏感字段值；数据库只读取 Schema、计数、JSON 键和完整性结果；
- 调用 AI、图片生成或其它付费 API；
- 绕过本地加密。

官方资料：

- https://www.turbopush.top/
- https://github.com/xueyc1f/turbopush-mcp
- https://github.com/xueyc1f/turbopush-mcp/blob/main/docs/api.md

## 2. 结论

Turbo Push 不是单一“批量发布页面”，也不是一个账号文件，而是以 `.turbopush` 为状态根的六层本地系统：

1. Tauri 桌面壳与 React 类 Web UI。
2. 桌面壳监管、可独立热更新的 Go REST 主服务；知识库另有 Go Sidecar，但本方案排除。
3. `data.db`、`config.yaml`、浏览器 Profile、运行发现文件和加密导入导出组成的数据层。
4. 每账号独立浏览器会话、代理与指纹环境。
5. 内容、平台模板、队列、SSE 发布事件和数据同步。
6. MCP stdio 代理，把 18 个工具转发给本地 REST 服务。

Alpha-Nexus 已具备 SQLite、BrowserFacade、Lease、Runner、Adapter、队列、React/Electron 和 MCP 服务。`tools.Registry`、`job_events`、`audit_events`、`browser_events` 和 `trace.ID` 可复用，但还不是生产级统一控制面。缺口集中在：

- 真实平台自动化；
- 内容与发布 UI；
- 统一 tools HTTP 桥；
- 生产级队列 Worker；
- 平台参数 Schema；
- 安全、幂等的数据迁移；
- 登录状态、事件流和指标闭环。
- 动态 Sidecar 端口、进程级认证、优雅关闭和受限自动重启；
- 结构化日志、上下文关联、脱敏、轮转和任务生命周期审计。

不建议复制 Turbo Push 的云会员层，也不建议把其闭源 Sidecar 变成 Alpha-Nexus 的永久依赖。

### 2.1 已落地基础（2026-07-16）

本报告的第 6 节记录的是实施前审计基线；下列能力已经实现，不再是待办：

| 能力 | Alpha-Nexus 实现与边界 |
|---|---|
| Turbo 本地迁移 | 识别 `.turbopush/data.db` 与加密 `.tp`；只读白名单字段、事务回滚、路径命名空间、默认 insert-only 重导；绝不导入 Cookie、Profile、密钥或原始设置 JSON。 |
| 任务可靠性 | `publish_jobs` 原子 Claim、CLI/tools/队列请求级 Idempotency-Key、重复 `publish_records` 插入触发器护栏、旧 SQLite 关系触发器、typed `job_events` 校验、`raw_json` API 隔离与 `Last-Event-ID` SSE durable replay。`task_queue` 使用 owner-bound lease、心跳续租、崩溃回收和取消/单次任务收口。 |
| 统一控制面 | `GET /api/v1/tools/schema`、`POST /api/v1/tools/call` 与 `GET /api/v1/events` 已成为 HTTP adapter；它们复用同一 Registry/SQLite，而不复制业务实现。 |
| 发布审计 | Runner 从 context 继承/生成 trace，发布器原始报错被稳定错误码与固定文案替代；成功但持久化失败会落 `job.published_with_warning`，不会被自动重试为二次发布。 |
| Go Sidecar | `ui serve --port 0 --sidecar-descriptor` 写 0700/0600 descriptor，loopback + header transport token，PID/nonce health 校验、优雅关闭与有限重启。 |
| Electron | 打包态监管 Sidecar；开发态直接复用现有 `run_electron.sh` 的 Go/Vite/Electron 流程。已实际打开项目窗口并验证 `/healthz` 为 ready。 |
| 诊断日志与通知 | `internal/observability` 为 Registry 生产运行时创建按 PID 共享的 0600、有界 JSONL；字段白名单、轮转、trace/correlation 合并，不把业务真值放进日志。发布终态写入 SQLite 通知箱，提供 API 与 React 通知中心；通知失败不影响发布真值。 |

明确未交付：全平台 Native Adapter 与真实 fixture、真实平台登录/自动发布验收、带凭证代理/Keychain 引用和经授权的代理健康检查。这些仍需平台测试账号与单独发布授权。当前目录共 23 项：22 个发布代码都可 dry-run；Instagram/Facebook 为 best-effort，YouTube/TikTok 只有冻结单目标计划 + `submit-bound` 审批的受控浏览器提交契约，X live fail-closed，`custom` unavailable。任何最终点击都必须等待创作者后台的明确 Published 证据，不能由人工口头确认直接写发布记录。

范围修订：Alpha-Nexus 不实现本地后台定时器、到点唤醒或周期采集。内容库排期只在用户显式准备发布时填写平台原生时间控件；旧库调度数据仍只保留且不可执行。下文出现的无人值守定时能力仅描述 Turbo Push 竞品事实或实施前基线。

### 2.2 账号切换与诊断复核（2026-07-24）

本轮只读复核 Turbo Push 的账号管理页，未打开数据库敏感字段、未复制 Profile/Cookie，也未执行发布：

- 账号卡片会同时展示平台、登录状态、最近登录时间、添加时间、分类、备注和排序；
- 对 UI 标记“已登录”的账号点击“打开管理器”，实际返回 `解密敏感数据失败`；
- 这证明“数据库登录标签”“敏感会话可解密”“浏览器可启动”“平台实时认证有效”是四个不同状态，不能用一个 `logged_in` 布尔值代替；
- Turbo Push 没有向用户提供足够的结构化关联信息，无法从该错误直接追到账号、浏览器实例、数据库状态和恢复动作。

Alpha-Nexus 针对这个缺陷采用以下真源，不复制 Turbo 的加密实现：

| 真源 | 当前行为 |
|---|---|
| SQLite 健康 | 每次打开 `app.db` 先执行 `PRAGMA quick_check(1)`；异常时停止启动，不继续迁移或浏览器操作。 |
| 登录状态 | `accounts.login_state` 只保存已验证平台证据；浏览器运行状态、租约和登录证据分别记录。 |
| 账号切换 | 成功置前后原子写入 `accounts.last_focused_at` 与 `browser_events.focus_succeeded`；失败写 `focus_operation_failed`，无页面目标时记录失败后仅为显式打开动作创建平台初始页。 |
| Bug/崩溃 | HTTP panic 被恢复为带 request ID 的安全 `INTERNAL_ERROR`，同时写 0600 轮转 JSONL 和 SQLite allowlist 事件；不记录 panic 原文、URL、Cookie、Profile 路径或请求参数。 |

知识库的数据库、日志、Sidecar、搜索和同步能力全部保持排除，不新增兼容层或迁移器。

## 3. `.turbopush` 实时运行目录深度解剖

本机的 `/Users/albert/.turbopush` 与进程路径中显示的 `/Users/albert/.TurboPush` inode 相同；这是 macOS 大小写不敏感文件系统上的同一目录，不是两套数据。目录约 121 MB，根权限 `0700`。

### 3.1 目录职责

```text
~/.turbopush/
├── data.db                         主业务 SQLite
├── config.yaml                     浏览器 DOM 选择器/回退目录
├── mcp.json                        主 Go 服务的端口与认证发现文件（0600）
├── kb.json                         KB 服务发现文件（排除）
├── tp-browser/<profile>/           完整 Chromium User Data roots
├── bin/
│   ├── turbo_push_server-*         实际运行的 Go 主服务
│   ├── turbo_push_kb-*             KB Sidecar（排除）
│   └── versions.json               Sidecar 版本登记
├── logs/kb.log                     KB 文本日志（排除业务，但用于日志对照）
├── panic.log                       Go 主服务崩溃记录
└── kb.db*                          独立知识库数据库（不集成）
```

`mcp.json`、`kb.json` 只检查了权限、根键名和字段类型；没有读取或输出认证值。

### 3.2 `data.db`：实时业务真值

已验证：

- SQLite 5,144,576 bytes，`DELETE` journal，`quick_check=ok`；
- 13 张表、38 个显式索引、4 个声明 FK，无 view/trigger；
- `user_version=0`，不能把它当正式迁移版本；
- 1256 pages 中 1048 pages 在 freelist，占 83.4%；`secure_delete=FAST`，旧数据残片可能继续存在于空闲页；
- 文件模式 `0644`，当前依靠父目录 `0700`；复制到普通目录会扩大暴露面。

安全计数：

| 表 | 行数 | 说明 |
|----|------|------|
| accounts | 2 | 敏感字段只统计存在性，不读取值 |
| platforms | 37 | 大于浏览器配置覆盖的平台数 |
| categories / account_categories | 6 / 15 | 15 条关联全部指向不存在的账号；因无 FK 未被检查发现 |
| articles / media | 1 / 0 | 一条草稿，无独立媒体行 |
| plat_sets | 3 | 平台发布设置 |
| publish_records / record_infos | 0 / 0 | 尚无发布历史 |
| users / editor_style_configs | 1 / 1 | 含设备身份/加密相关字段，禁止迁移 |
| proxies / webhook_configs | 0 / 1 | 当前无代理记录 |

账号两行都存在 Cookie 密文及 salt/nonce，但算法和设备密钥未验证。Alpha 不得把源密文当作可用 Cookie，也不得导入 `users.prv/pub/sign/set` 一类设备身份材料。

`articles.publish_data` 已验证的业务键包括 `title`、`content`、`desc`、`files`、`thumb`、`headless`、`syncDraft`、`closeBrowser`、`autoThumb`、`markdown` 和 `tempID`。这证明内容、媒体与发布选项被聚合为一份 JSON 快照，但 Alpha 应拆回版本化领域模型。

### 3.3 `config.yaml`：自动化选择器目录

`config.yaml` 为 62,124 bytes、2756 行，覆盖 18 个浏览器平台：

```text
acfun, baijiahao, bilibili, csdn, douyin, jianshu, juejin,
kuaishou, omtencent, pdd, sina, tiktok, toutiao, wechat,
wechat_video, weishi, xiaohongshu, zhihu
```

每个平台包含 `Login`、`SetArticle`、`SetGraphText`、`SetVideo`、`Setting.*` 和 `TimerPublish.*` 等操作。规则字段只有 `query`、`attr`、`numVisible`、`notContain`；数组顺序表现为选择器 fallback 顺序。

关键结论：Turbo 的平台自动化核心不是 18 套散落在代码里的 CSS Selector，而是“版本化操作目录 + 有序回退规则”。Alpha 应复用这个设计思想，建立自己的 `SelectorCatalog`、fixture 和验收测试；在许可证与来源未确认前，不直接复制 Turbo 的 2756 行配置。

### 3.4 `tp-browser`：登录会话边界

存在 3 个完整 Chromium User Data root，分别约 14 MB、14 MB、30 MB，均包含 Cookies、Login Data、Local State、History、Local Storage、IndexedDB、Service Worker 和 Session Storage。

数据库只有 2 个账号，Profile 却有 3 个，且 `accounts` 没有 profile path 字段。数字目录名可能是调试端口、会话 ID 或遗留实例，不能推断成账号 ID。Alpha 必须显式持久化：

```text
account_id -> profile_id -> profile_path -> lease/runtime instance
```

现有 Profile 不直接复制。账号登录采用 Alpha 新建 Profile、用户完成二维码/短信/2FA/OAuth、平台被动校验的流程。

### 3.5 Go Server 与进程拓扑

本机实际拓扑：

```text
Tauri/Rust 桌面壳 v2.3.1
├── Go 主服务 v2.3.6 -> loopback 动态端口
│   ├── data.db
│   ├── config.yaml
│   └── panic.log
└── Go KB 服务 v2.3.0 -> 独立 loopback 动态端口（排除）

独立 MCP stdio 进程
└── 读取本地发现文件 -> 转发主服务 REST
```

桌面壳和 KB 自前一日持续运行，主 Go 服务在 2026-07-16 独立热更新并重启。安装包内主服务为 Go 1.26.4；运行目录内主服务为 Go 1.26.5/v2.3.6，说明 Sidecar 可独立于桌面壳升级。

已验证主服务依赖 Gin/GORM/SQLite/chromedp/OAuth1/2/YouTube/X/SOCKS5 等能力。官方路由共 69 条，覆盖系统、用户、账号、平台、模板、内容、SSE、记录、仪表盘、代理、图片、Pandoc、导入导出、OAuth 和支付。Alpha 排除支付，并把其余能力重组为版本化异步 API，不逐字复刻非版本化路由。

认证行为：主服务 `/ping` 无认证可达，`/check` 和未知路径先经过认证；官方兼容 Header 和 query auth。Alpha 只允许 Header/IPC，不支持 query token。

更新链已验证有 `.staging`、SHA-256、minisign 签名、临时版本文件、进度事件、TERM 后限时 KILL。信任根固定、原子 rename、回滚保护未验证，Alpha 实现更新前必须补齐。

### 3.6 Turbo 日志与可观测性现状

Turbo 有四个互不连通的观测面：

| 观测面 | 已验证事实 | 缺口 |
|--------|------------|------|
| `logs/kb.log` | 93 行，时间戳 + 文本，`0600` | 无级别、关联 ID、轮转；启动与优雅关闭数量不匹配 |
| `panic.log` | 24 行，单次 nil pointer panic，`0644` | 无结构、无轮转、权限过宽 |
| `notifications.json` | 54 条，含 title/body/type/level/read/time | 可能含 PII，无上限/轮转 |
| macOS Unified Log | 7 天 33,395 条，绝大部分来自桌面壳 | Go 业务路径几乎不可观测，无稳定 job/account/content 关联 |

浏览器目录中的 LevelDB `*.log` 是数据文件，不是诊断日志，禁止当文本采集。Turbo 的日志设计不应照搬。

`logs/kb.log` 只作为反例证据保留在本报告中；Alpha-Nexus 不接入、不解析也不复刻 KB 日志。

### 3.7 `.tp`：外部导出，不是实时账号目录

另发现一个权限 `0600`、大小 5,177,420 bytes 的 `.tp` 加密导出包。它是二进制加密封装，头部存在 nonce；未解析密文。

从 Turbo Push 点击 UI 显示“在线”的账号管理器时，应用实际返回“解密敏感数据失败”。因此 UI 登录标记不是可发布会话的充分证据。安全迁移只能是“读取实时 `data.db` 中允许的非敏感元数据 + Alpha 重新登录/OAuth”，不能破解 `.tp`、复制密文、Cookie、设备私钥或直接搬运 Profile。

## 4. Turbo Push 全功能清单

### 4.1 全局能力

- 全局内容搜索。
- 快速创作、快速发布。
- 任务进度浮层。
- 通知中心。
- 登录失效、同步完成、任务状态提醒。
- 明暗/跟随系统主题。
- 桌面托盘、开机自启、版本更新。

### 4.2 仪表盘

- 内容总数、本周新增。
- 发布次数、成功发布、成功率。
- 已登录/总账号、连接平台、未登录账号。
- 最近发布。
- 近 7 日阅读、评论、点赞趋势。
- 最近动态。
- 内容排行与作品数据同步入口。

### 4.3 内容库

内容类型：

- 文章；
- 图文；
- 视频。

管理能力：

- 全部/草稿/已发布筛选；
- 搜索；
- 列表/网格；
- 编辑、复制、删除、发布；
- 内容发布次数和更新时间；
- 媒体缩略图。

### 4.4 Markdown 文章编辑器

- 双栏编辑/预览与可调分隔线。
- 目录。
- 粗体、斜体、删除线、链接、行内代码、格式化。
- 主题、字体、字号、主色、代码块主题。
- 字数、阅读时间。
- 明暗/系统主题。
- Markdown 语法帮助。
- 表格、链接、图片插入。
- 自定义 CSS，300ms 防抖实时预览。
- Markdown、LaTeX、Mermaid 渲染及平台兼容处理。
- 微信外链兼容提示。
- 导入 Markdown。
- 导出 Markdown、HTML。
- 文档导入/导出：docx、html、wiki、rst 等，依赖 pandoc。
- 格式化 Markdown/CSS。
- 复制纯文本。

### 4.5 图文与视频

图文：

- 标题；
- 描述；
- 话题与 @用户；
- 多图片。

视频：

- 标题；
- MP4、MOV、AVI、MKV、FLV；
- 描述、话题、@用户；
- 单/多封面；
- 未选择封面时提取视频首帧；
- 首帧提取依赖 ffmpeg；
- 平台只允许一张封面时取第一张。

### 4.6 非知识库 AI

纳入 Alpha-Nexus：

- 标题生成；
- 摘要生成；
- 内容检查；
- 改写；
- 平台文案适配；
- 文章编辑器内的内容补全与配图入口。

排除：

- 我的知识库；
- 检索 Top K；
- 基于知识库找选题、整理资料、搭大纲和仿写。

Turbo Push 支持通义千问、硅基流动、DeepSeek、OpenAI 和 OpenAI-compatible 自定义服务。Alpha-Nexus 只复用已有 LLM Gateway，并保留付费调用确认。

### 4.7 账号中心

- 平台概览。
- 账号搜索。
- 在线/离线、内容类型、分类筛选。
- 添加账号、重新登录。
- 打开管理后台。
- 批量打开后台。
- 删除。
- 分类、备注、排序优先级。
- 头像、平台、主机、最近登录时间。
- 绑定代理。
- 绑定指纹环境。
- 浏览器内完成二维码、短信、OAuth 或人工 2FA。

平台登录流程：

    选择平台
      → 可选代理
      → 打开独立浏览器 Profile
      → 用户完成平台登录/2FA
      → 被动校验登录状态
      → 保存非敏感元数据与 Profile 引用

### 4.8 平台发布模板

- 按平台创建多套模板。
- 名称、描述、启用、默认。
- 复制、编辑。
- 发布任务可选择默认模板或临时覆盖。
- 自定义平台：API 端点、API Key、超时时间。

小红书实测参数：

- 位置；
- 合集；
- 关联群聊；
- 创作来源；
- 关联媒体；
- 可见范围；
- 声明原创。

其它平台的完整参数由官方 API 文档定义，Alpha-Nexus 应转为版本化 Go Schema，不把 UI 字段硬编码进页面。

### 4.9 发布流程

五步向导：

1. 选择账号。
2. 平台设置。
3. 发布选项。
4. 发布中。
5. 发布结果。

选项：

- 多账号、搜索、标签筛选。
- 每账号独立设置。
- 模板复用与任务级覆盖。
- 直接发布或保存草稿。
- 立即发布或定时发布。
- 浏览器可视或后台静默。
- 完成后关闭浏览器。

官方 SSE 契约：

- 请求：headless、syncDraft、postAccounts。
- 事件：info、success、error、finish、wait、vip。
- 全局事件：heartbeat、exit、logout、sync。

Alpha-Nexus 不复制 VIP 限制；本地策略决定并发和自动发布权限。
上面的无人值守“定时发布”仍不进入 Alpha-Nexus 产品范围；Alpha-Nexus 只在用户当次显式准备中复用平台原生时间字段。

### 4.10 发布记录与数据

- 状态：发布中、失败、部分成功、成功。
- 类型：文章、图文、视频。
- 搜索。
- 单任务账号级结果。
- 发布 URL、外部 ID、错误。
- 阅读/播放、点赞、评论、收藏、分享。
- 周期同步。

### 4.11 代理与指纹

代理：

- 添加、编辑、删除；
- 导入、导出；
- 单个/批量健康检查；
- 搜索、健康筛选；
- 账号粘性绑定。

指纹：

- 模板名称、种子、随机种子；
- Windows/macOS 等操作系统；
- Chrome 等浏览器品牌；
- WebRTC 非代理 UDP 禁用；
- 语言、Accept-Language、时区；
- 系统/浏览器版本；
- CPU 核数；
- GPU Vendor/Renderer；
- 自定义启动参数。

### 4.12 系统与集成

- Chrome、Edge、指纹浏览器选择。
- 数据同步间隔 10–1440 分钟。
- 钉钉、飞书、Telegram Webhook。
- 可选签名密钥。
- 开机自启、托盘。
- 数据导入/导出 .tp。
- 打开数据目录。
- 前后端组件更新。
- pandoc 安装。
- MCP 18 工具：平台、账号、模板、内容 CRUD、发布、记录。

## 5. 平台范围

### 5.1 目标平台

Turbo Push 的 20 个正式平台，加自定义平台，再保留 Alpha-Nexus 的 Facebook、Instagram，共 23 类目标：

| 平台 | 内容类型 | Alpha 目标策略 |
|------|----------|----------------|
| 微信公众号 | 文章、图文 | Browser/Hybrid |
| 微信视频号 | 图文、视频 | Browser |
| 抖音 | 图文、视频 | Browser |
| 今日头条 | 文章、图文、视频 | Browser |
| 快手 | 图文、视频 | Browser |
| 小红书 | 图文、视频 | Browser |
| 腾讯内容开放平台 | 文章、视频 | Browser |
| 微视 | 视频 | Browser |
| 哔哩哔哩 | 文章、视频 | Browser/API 可演进 |
| AcFun | 文章、视频 | Browser |
| 百家号 | 文章、图文、视频 | Browser |
| 新浪微博 | 文章、图文、视频 | Browser/API 可演进 |
| 简书号 | 文章 | Browser |
| 知乎 | 文章、图文、视频 | Browser |
| 掘金 | 文章、图文 | Browser |
| CSDN | 文章、视频 | Browser |
| TikTok | 视频 | OAuth/API 优先 |
| YouTube | 视频 | OAuth/API |
| X | 图文、视频 | OAuth/API |
| 拼多多 | 视频 | Browser |
| 自定义平台 | 动态 | unavailable；不接受任意 webhook 成功语义 |
| Facebook | 文章、图文、视频 | Browser/API |
| Instagram | 图文、视频 | Browser/API |

### 5.2 仅保留为 planned 的 16 个平台

Turbo Push UI 标记为开发中的平台只进入能力目录，不宣称可发布：

- 大鱼号、一点号、搜狐号、网易号、快传号、大风号；
- 爱奇艺号、腾讯视频、优酷、搜狐视频、美拍；
- 百花号、皮皮虾、最右、雪球号、凯迪号。

## 6. Alpha-Nexus 现状与缺口

| 能力 | 当前状态 | 主要问题 |
|------|----------|----------|
| SQLite 业务模型 | 已有 | 运行时 Schema 与 migrations 不完全一致；缺外键与关键索引 |
| BrowserFacade/Lease | 已有 | 可复用；必须保持唯一生命周期入口 |
| 登录 Profile | 已有 | 需要平台化被动校验、OAuth 回调和人工 2FA 状态 |
| 平台目录 | 10 个 | 需扩为 23 active + 16 planned |
| 平台 Adapter | 骨架 | 国内平台仍需逐平台 live 验收；YT/TikTok 已有受控浏览器契约但未通过真实账号验收，X 仍为 API stub |
| Publish Runner | 已有状态机 | 缺可靠重试、取消/恢复、审批策略 |
| Queue | 脚手架 | 没有生产 daemon；Claim 不是完整事务队列 |
| Go Sidecar | `ui serve` 已有 | Electron 固定 17321；无进程级 Token、启动握手、优雅关闭、受限重启 |
| 内容库 | DB/CLI 部分已有 | React 无内容中心、编辑器、图文/视频表单 |
| 平台模板 | 表已存在 | 无版本化 Schema、完整 CRUD/UI |
| 发布向导 | 缺失 | UI 仅有批次读取页 |
| 历史/指标 | 表和部分接口已有 | 缺发布记录页、账号级结果、周期同步 |
| 通知 | 表已有 | 缺事件总线、Webhook、桌面闭环 |
| 日志/Trace | `trace.ID`、audit/job/browser events 已有 | 分散、无 context 传播、无 `slog`、无脱敏/轮转/保留策略 |
| 代理/指纹 | 部分已有 | 缺代理池 CRUD/检查、模板复用 |
| 选择器目录 | 无统一实现 | 平台 DOM 规则散落风险；缺版本、fixture、回退遥测和热修复边界 |
| tools.Registry | 已有 | content/queue 多数仍是 planHandler |
| HTTP UI | 部分已有 | 没有统一 tools/schema 和 tools/call |
| Turbo Push 导入 | 有 importer | 只接受 data.db；不能读取 .tp；存在安全与映射错误 |

### 6.1 实施前导入器缺口（已修复）

`internal/importers/turbopush/importer.go` 当前：

- 只把 source 当作 SQLite data.db 打开；
- toutiaohao 错映射为 douyin；
- weishi 错映射为 wechat_video；
- tiktok 错映射为 xhs；
- 跳过 Bilibili、AcFun、YouTube、X、微博等；
- 把源 cookie 原文写入名为 cookie_encrypted 的字段；
- 多处忽略 Exec 返回错误；
- 发布记录写入 0 外键；
- 缺事务、幂等和回滚；
- 无测试。

这条路径可把实时 `data.db` 当 SQLite 打开，但不可用于 `.tp`，也不能安全迁移源 Cookie/Profile。生产版必须区分 `live-db`、`encrypted-export` 与 `metadata-manifest` 三种来源，默认只导入白名单元数据。

### 6.2 实施前可观测性缺口（基础已补齐）

- `internal/trace/trace.go` 只有进程内 ring buffer，`ctxKey` 未使用；
- `publish_jobs` 无 `trace_id`，`job_events` 缺 request/task/duration/error/retry 字段；
- Repository 忽略现有 `raw_json`，Runner 只写 preflight 与 terminal，且吞事件写入错误；
- 运行时 Schema 与 SQL migration 在 FK/索引上分叉，存在 `job_id=0` 写入路径；
- 批处理 NDJSON 接收任意 map，无 Schema、时间、级别、脱敏和写错误处理；
- Electron 打包态丢弃 Go stdout/stderr，同时部分路径输出代理规则、指纹和本地路径；
- Browser Supervisor 事件为 fire-and-forget，未写 `job_id`，可能重复 stopped，并每 5 秒重复 heartbeat_failed；
- `notifications` 只有表，没有 CRUD/API/UI；设置页日志级别与保留期只写 localStorage；
- HTTP 无 request ID、access log、panic recovery，登录成功/失败无审计。

2026-07-24 复核后，Alpha 已补齐 SQLite 启动 quick-check、账号切换成功/失败事件和 HTTP panic recovery/request ID。通用 HTTP access log 仍未增加：业务/工具/浏览器已有语义事件，避免把每次轮询和静态资源请求重复写入 SQLite；如未来确需 access log，应只进有界文件，不污染业务事件真源。

## 7. 三种集成方案

### 方案 A：完全原生复刻

Alpha-Nexus 独立实现全部功能和平台 Adapter。

优点：

- 无 Turbo Push 运行依赖和 VIP 限制；
- 数据、安全、发布真值完全受控；
- 可测试、可维护。

缺点：

- 23 类平台自动化工作量最大；
- DOM 变化需要持续维护；
- 完整交付周期最长。

适用：长期最终形态。

### 方案 B：直接桥接 Turbo Push

Alpha-Nexus UI/Agent 通过 Turbo Push MCP 或本地 REST 完成发布。

优点：

- 最快获得 20 平台覆盖；
- 可复用现有发布参数和 SSE。

缺点：

- 依赖 Turbo Push 桌面端、云登录、随机 Token、VIP 和闭源主服务；
- 当前账号敏感数据已经解密失败；
- 双数据库、双任务状态、双浏览器生命周期；
- 难以满足 Alpha-Nexus 的本地唯一真源规则。

适用：短期兼容验证，不适合作为长期核心。

### 方案 C：混合绞杀，推荐

Alpha-Nexus 负责：

- 唯一数据库；
- 唯一账号/Profile；
- 唯一任务、队列和审计；
- 全部 UI、Agent、MCP；
- 已成熟平台的原生 Adapter。

可选 Turbo Connector 负责：

- 暂未原生实现的平台发布；
- 只接收规范化内容、账号引用和任务设置；
- 只回传事件、外部 ID、URL 和错误；
- 不复制 Cookie，不成为账号真源；
- 可按平台 Feature Flag 关闭。

平台 Adapter 成熟后逐个从 Connector 切换到 Native，不迁移上层 UI 和任务数据。

## 8. 推荐架构

~~~mermaid
flowchart TB
    EM["Electron Main"] --> SUP["Sidecar Supervisor"]
    SUP --> GO["单一 Alpha Go Sidecar"]
    EM --> IPC["受限 IPC / Backend API"]
    UI["React Renderer"] --> IPC
    IPC --> AUTH["Transport Token + User Auth"]
    WEB["Standalone Web"] --> AUTH
    MCP["Alpha MCP / CLI / Agent"] --> REG["tools.Registry"]
    AUTH --> V1["/api/v1"]
    V1 --> REG
    REG --> DOMAIN["Account · Content · Template · Publish"]
    DOMAIN --> JOB["Persistent Jobs · Queue · Scheduler"]
    JOB --> BF["BrowserFacade · Lease · Supervisor"]
    BF --> SC["Versioned SelectorCatalog"]
    SC --> NATIVE["Native Browser Adapters"]
    JOB --> API["OAuth / API Adapters"]
    JOB --> TC["Optional Turbo Connector"]
    NATIVE --> EVT["Typed Event Emitter"]
    API --> EVT
    TC --> EVT
    EVT --> DB["SQLite Job/Audit Truth"]
    EVT --> LOG["0600 Rotating JSONL"]
    EVT --> SSE["SSE with Replay Cursor"]
    EVT --> NOTIFY["Desktop / Webhook Notifications"]
    SSE --> UI
~~~

核心规则：

- UI、CLI、MCP 都只调用 tools.Registry。
- BrowserFacade 是唯一浏览器生命周期入口。
- SQLite 是唯一业务真源。
- Electron 只监管进程、保管 Sidecar 传输 Token、提供受限 IPC 和桌面能力；不承载业务状态。
- Alpha 只运行一个业务 Go Sidecar；知识库服务不建，MCP 复用同一 Registry。
- Connector 只是一种 Publisher Provider。
- 不因 DOM 不确定而伪造 published。
- dry-run 不写正式发布记录。
- 任务必须有 idempotency key，防重复发布。
- 事件先持久化再广播；SSE 断线后可用事件 ID 续传。
- 日志只用于诊断，`job_events`/`audit_events` 才是可恢复的审计真值。

## 9. 数据与接口设计

### 9.1 数据真值与 Schema

优先复用现有表，减少新抽象：

- platforms：扩充 active/planned 状态和内容能力。
- accounts：账号展示元数据与登录状态。
- browser_profiles：Profile、指纹、粘性代理；不存明文 Cookie。
- credentials：只存 Keychain/系统凭证引用。
- platform_settings：settings_json 加 schema_version。
- contents/media_assets：文章、图文、视频和媒体。
- publish_jobs：增加 `trace_id`、`idempotency_key`、`cancel_requested_at`；headless、sync_draft、auto_submit、close_browser 存版本化 settings_json。
- job_events：增加 `trace_id`、`request_id`、`task_id`、`duration_ms`、`error_code`、`error_class`、`retryable`、`schema_version`；保留完整步骤、截图和 DOM 证据引用。
- publish_records/record_infos/post_metrics：结果与周期数据。
- task_queue：增加 `worker_id`、`lease_until`、`heartbeat_at`、`next_attempt_at`，支持 crash reclaim。
- notifications/audit_events：通知与审计。

运行时 Go Schema 与 `migrations/` 必须合并为同一迁移真源；启动时启用 FK，所有 FK 列建索引。Queue Claim 在短事务内完成，不在事务中启动浏览器或请求网络。

### 9.2 Go Sidecar 生命周期与认证

Alpha 采用 Turbo 已验证有效的 Sidecar 模式，但修正其安全和可运维缺口：

1. Electron 启动 Go 时传 `127.0.0.1:0`，由 OS 分配端口；禁止固定 17321 作为生产发现机制。
2. Go 生成 256-bit 随机传输 Token，原子写入 `run/backend.json`；目录 `0700`、文件 `0600`，字段只含 schema_version、pid、port、started_at、app_version 和 auth。
3. Token 不放 argv、URL query、renderer localStorage 或日志。Electron Main 读取后保存在内存，通过受限 IPC 代理请求。
4. `GET /healthz` 仅返回进程/版本/ready 状态，不暴露业务数据；全部 `/api/v1/*` 强制传输 Token。
5. 用户 JWT/RBAC 与 Sidecar 传输 Token 分层。即使数据库没有 app_user，打包态也不能关闭传输认证。
6. Go 使用 `http.Server`：收到退出信号后停止接单、释放 lease、flush 事件、`Shutdown`；Electron 等待 5 秒后才 KILL。
7. 意外退出采用有上限指数退避；连续崩溃进入 degraded 并提示用户，禁止无限重启风暴。
8. 正常退出删除发现文件；启动时校验旧 pid，不信任陈旧 descriptor。

开发态可以继续固定端口，但必须显式标记为 dev-only，并使用独立测试 Token。

### 9.3 版本化 API

```text
GET  /healthz
GET  /api/v1/tools/schema
POST /api/v1/tools/call

POST /api/v1/login-sessions
GET  /api/v1/login-sessions/{id}
POST /api/v1/login-sessions/{id}/cancel

POST /api/v1/publish-jobs          Idempotency-Key 必填，返回 202 + job_id
GET  /api/v1/jobs/{id}
POST /api/v1/jobs/{id}/cancel
GET  /api/v1/jobs/{id}/events      SSE，支持 Last-Event-ID

/api/v1/accounts
/api/v1/platforms
/api/v1/platform-settings
/api/v1/contents
/api/v1/proxies
/api/v1/fingerprint-templates
/api/v1/records
/api/v1/notifications
/api/v1/metrics
/api/v1/import-export
```

REST 只创建/查询资源，不把发布生命周期绑在一个长 POST 上。错误响应固定为 `error_code`、安全 message、retryable、trace_id；第三方原始错误先归类和脱敏。

### 9.4 登录与自动发布状态机

登录：

```text
requested -> profile_allocated -> browser_ready
          -> human_action_required -> verifying
          -> verified | expired | cancelled | failed
```

发布：

```text
requested -> queued -> leased -> started -> preflight
          -> browser_attached/api_authenticated
          -> asset_processed -> metadata_filled -> submitting
          -> submit_confirmed
          -> succeeded | retry_wait | manual_takeover | cancelled | failed
```

只有 `submit_confirmed` 获取到平台 URL、外部 ID、明确成功 Toast 或可复核页面证据后，才写正式 publish_record。Worker 每个状态转换必须同事务写 job 状态与事件；事件写失败不得被吞掉。

### 9.5 版本化 `SelectorCatalog`

新增 Go 能力目录：

```text
internal/platforms/schema/
internal/platforms/selectors/
testdata/platforms/<platform>/<version>/
```

每个平台定义：

- 内容类型；
- 登录方式；
- 发布策略；
- 参数 JSON Schema；
- 校验器；
- 成功证据；
- 可重试错误；
- 版本号。

选择器条目包含 platform、operation、catalog_version、ordered candidates、visibility/text constraints、success evidence、fallback telemetry。UI 由参数 Schema 动态渲染平台字段，避免 23 套硬编码表单。

发布时把 catalog_version 写入 job snapshot，确保任务可复盘。热更新先下载到 staging，验签/校验 fixture 后原子切换；失败回滚上一个版本。Turbo 配置只能用来理解模型，Alpha 选择器必须通过自有测试账号和 fixture 重新建立。

### 9.6 日志、审计与通知设计

统一新包：

```text
internal/observability
├── Event / Emitter
├── context correlation
├── slog JSON handler
├── allowlist sanitizer
├── rotating file sink
├── job/audit sink
└── notification/SSE sink
```

四类数据分责：

| 层 | 真值用途 | 保留 |
|----|----------|------|
| JSONL 诊断日志 | 开发/排障，不用于恢复业务 | 10 MB × 5，14 天，`0600` |
| `job_events` | 发布/登录/队列生命周期，可重放 | 随业务保留策略 |
| `audit_events` | 用户/工具/配置安全审计 | 默认 90 天，可配置 |
| notifications | 用户需要处理的状态 | 已读归档/定期清理 |
| crash reports | panic、stack、版本，不含业务内容 | 30 天，`0600` |

日志字段白名单：

```text
ts level service component event schema_version
trace_id request_id task_id job_id publish_record_id batch_id
platform account_ref attempt step status
error_code error_class retryable duration_ms http_status
app_version sidecar_version pid
```

`account_ref` 使用安装级密钥生成不可逆 HMAC，不记录账号名。禁止记录：Cookie、Authorization、密码、API Key、代理凭证、URL query/fragment、文章标题/正文、通知 body、DOM 原文、完整指纹、绝对媒体路径。截图与 DOM 只记录受控 artifact ID；第三方错误先映射稳定错误码，再保留经过 sanitizer 的短摘要。

统一 Emitter 接入五个首要入口：`tools.Registry.Call`、HTTP middleware、Publish Runner、Queue Worker、Browser Supervisor。旧 NDJSON 变为同一 typed Event 的 stdout sink，不再接受任意 map。Electron 不丢弃 Go stderr/stdout，也不直接打印代理/指纹配置。

核心生命周期事件：

```text
sidecar.starting -> ready -> stopping -> stopped | crashed
login.opened -> human_action_required -> verified | expired
publish.requested -> queued -> leased -> started -> submit_confirmed -> succeeded|failed|cancelled
queue.claimed -> heartbeat -> retry_scheduled -> released
selector.candidate_failed -> fallback_selected -> evidence_confirmed
```

先写测试再接线：sanitizer、0600 权限、轮转、trace propagation、状态顺序、FK/索引、SSE 断线续传、崩溃恢复和敏感字段负面测试。

### 9.7 Sidecar 与选择器更新

借鉴 Turbo 的 staging + hash + signature + 独立重启，但 Alpha 明确要求：

- 固定签名公钥与 HTTPS 下载域；
- manifest 绑定 target、版本、最小桌面版本、SHA-256、大小和签名；
- 同文件系统 staging，fsync 后原子 rename；
- 保留上一版本并做启动健康检查，失败自动回滚；
- 防降级版本检查；
- 下载/安装/回滚全程发结构化事件；
- 更新只替换已签名二进制/目录，不覆盖 `app.db`、Profile 或用户媒体。

## 10. 账号迁移方案

### 10.1 不做

- 不破解 .tp；
- 不复制密文或 Cookie；
- 不复制 `users.prv/pub/sign/set`、发现文件中的认证值或 Chromium Profile；
- 不按 `tp-browser` 数字目录猜测账号映射；
- 不把 Turbo Push 的“已登录”直接映射为 Alpha logged_in；
- 不读取或输出平台凭证。

### 10.2 可迁移

- 平台代码；
- 账号显示名、头像、备注、分类；
- 内容与媒体；
- 平台模板；
- 发布记录和指标；
- 代理元数据，但密码必须重新录入 Keychain。

来源优先级：实时 `data.db` 的白名单字段优先；`.tp` 只识别类型并提示用户走 Turbo 官方导入后再迁移元数据。导入前做 Schema 指纹和完整性报告；`account_categories` 等孤儿关系只报告，不自动猜测修复。

### 10.3 登录恢复

1. 从 Turbo Push `data.db` 或 UI/API 获取非敏感账号目录。
2. Alpha-Nexus 创建账号和独立 Browser Profile。
3. 对浏览器平台打开登录页。
4. 用户完成二维码、短信、验证码、2FA。
5. 被动校验创作者后台。
6. 标记 logged_in，记录校验时间和证据类型。
7. YouTube、X 等走 OAuth，Token 只写系统凭证存储。

每个登录会话都有 `login_session_id`、trace_id、超时、取消与事件流。验证码、二维码、短信、2FA 只由用户在浏览器完成，不进入 Alpha 日志或数据库。

## 11. 分阶段实施

### Phase 0：数据真实性与 Schema 护栏，2–3 天（已完成基础）

- 为 .tp 返回明确的 encrypted-export 错误，不再当 SQLite 打开。
- 新增 `.turbopush/data.db` 只读探测器，只输出 Schema 指纹、计数和白名单预览。
- 重写 Turbo importer 映射。
- 禁止明文 Cookie 写入。
- 事务、幂等、错误返回、回滚。
- 修复运行时 Schema/migrations、外键和索引。
- 定义 Event、错误码、trace/job/login 状态机契约。
- importer 单测与脱敏 fixture。
- dry-run 不污染发布历史。

完成标准：

- 对现有 .tp 安全识别，不输出密文；
- 对实时 data.db 只导入允许字段，不碰 Cookie/私钥/Profile；
- 重复导入不产生重复数据；
- 任一步失败全量回滚；
- 无 0 外键发布记录。

### Phase 1：Go Sidecar 与统一可观测性，3–5 天（已完成基础）

- Electron 动态端口、运行发现文件、传输 Token、启动握手。
- `http.Server` 优雅关闭、TERM→限时 KILL、受限自动重启。
- `/healthz`、request ID、panic recovery；业务 access 由 Registry/浏览器语义事件覆盖，不记录静态资源噪声。
- `internal/observability`：typed Event、slog JSONL、sanitizer、rotation。
- 对齐 publish_jobs/job_events/audit/browser/queue Schema。
- 清除 Electron 代理、指纹、路径明文输出，接管 Go stderr/stdout。

完成标准：

- 固定生产端口不可见；陈旧发现文件不能劫持新进程；
- `/api/v1/*` 无 Token 全部拒绝，query token 无效；
- 任务可从 DB 事件还原，日志删除不影响业务恢复；
- 敏感字段负面测试、0600、轮转与 trace propagation 测试通过。

### Phase 2：统一控制面与可靠任务引擎，5–8 天（控制面已完成）

- `GET /api/v1/tools/schema`、`POST /api/v1/tools/call`。
- tools HTTP adapter、CLI、MCP 复用同一 Registry；其中既有 plan-only 工具仍会明确返回其 CLI 路径，不伪造实现。
- `queue worker` 已提供原子 Claim、lease heartbeat、crash reclaim、取消与单次任务收口；指数退避策略仍待按平台错误码定义。
- Idempotency-Key、人工接管与 durable `Last-Event-ID` SSE 已实现。
- Notifications CRUD/API 与 React 通知中心已实现；系统桌面/Webhook 投递仍待加入可配置 Provider。
- Turbo Connector 仍未实现，保持默认不依赖 Turbo。

完成标准：

- React、CLI、MCP 对同一工具得到同一结果；
- 应用重启后任务恢复率 100%；
- 同一 idempotency key 不重复执行；
- Sidecar 或 SSE 重启不丢失任务真值。

### Phase 3：内容、登录与发布 UI，5–8 天

- 文章、图文、视频 CRUD（已实现内容中心、创建、编辑、复制、删除和发布历史保护）。
- 发布向导（已实现内容/账号资产选择、请求级幂等键、默认 dry-run；真实自动提交仍由用户明确切换并受 Adapter 能力限制）。
- Markdown 编辑/预览、主题、CSS、导入导出。
- 图片和视频媒体库。
- ffmpeg/pandoc 能力探测；缺失时明确降级。
- 非知识库 AI：标题、摘要、改写、检查。
- Login Session UI：二维码/短信/2FA/OAuth、取消、超时、被动校验。
- 发布向导、平台 Schema 动态表单、任务时间线、记录与通知中心。
- 新导航：总览、内容库、创作、发布、账号库、配置库、设置；保留 Agent、Terminal、Coding。

完成标准：

- 三种内容可创建、编辑、复制和导出；
- 媒体 SHA-256 去重；
- 不配置 AI 仍可完整使用内容中心；
- 登录材料不进入 renderer storage、日志或业务表；
- 每账号结果独立；
- 成功、失败、部分成功准确。

### Phase 4：真实平台 Adapter，持续交付

第一批：

- 小红书；
- 抖音；
- 快手；
- 微信视频号；
- 微信公众号；
- Facebook、Instagram。

第二批：

- YouTube、X、TikTok；
- 今日头条、Bilibili、知乎、微博。

第三批：

- 腾讯内容开放平台、微视、AcFun、百家号、简书、掘金、CSDN、拼多多、自定义平台。

每个平台完成标准：

- 登录/重登；
- 自有 `SelectorCatalog` 版本、fixture 与 fallback 遥测；
- 内容类型校验；
- 草稿与直发的适用能力；
- 明确成功证据；
- 截图和稳定错误码；
- 测试账号连续 3 次成功；
- DOM 变化时 manual_takeover，不伪造成功。

### Phase 5：数据、代理、指纹和桌面能力，5–8 天

- 指标同步与趋势。
- 代理池 CRUD/健康检查/粘性绑定。
- 指纹模板。
- 数据备份恢复。
- 托盘、自启、更新。
- Sidecar/Selector 签名更新、原子安装、健康检查和自动回滚。
- 全局搜索。

## 12. 风险与控制

| 风险 | 控制 |
|------|------|
| 平台 DOM 高频变化 | Adapter 版本化、选择器 fixture、manual_takeover |
| 复制闭源选择器的版权/维护风险 | 只借鉴目录模型；自建 SelectorCatalog 与测试证据，来源/许可未确认不复制 |
| 账号风控 | 独立 Profile、粘性代理、低并发、人工 2FA |
| 重复发布 | idempotency key、原子 Claim、提交前后二次校验 |
| 假成功 | 只接受平台 Toast、URL、外部 ID 等明确证据 |
| 会话失效 | 被动健康检查、通知、人工重新登录 |
| Turbo 锁定 | Connector Feature Flag、逐平台替换 |
| 凭证泄露 | Keychain 引用、日志脱敏、禁止 Cookie 导入 |
| 本地端口劫持 | 动态 loopback 端口、进程 Token、pid/descriptor 校验、query token 禁用 |
| Sidecar 崩溃循环 | 有上限退避、degraded 状态、优雅关闭、崩溃事件与上次版本回滚 |
| 日志泄露/洪泛 | 字段白名单、sanitizer、0600、轮转、去重/限流、DB 审计与诊断日志分离 |
| 源数据库孤儿/无版本 | Schema 指纹、白名单导入、孤儿只报告、事务与幂等，不依赖 user_version |
| 工作树冲突 | 先保护当前未提交改动，再按 Phase 切小提交 |

## 13. 成功指标

- 0 次明文 Cookie/API Key 落库或日志输出。
- 0 次重复发布。
- 0 次不确定结果标记为 published。
- Worker 重启恢复率 100%。
- Sidecar 连续异常退出不超过限定重启预算，descriptor 不残留。
- 登录失效检测到通知不超过一个同步周期。
- 队列 Claim p95 小于 2 秒。
- 发布任务事件具有统一 trace_id。
- 100% HTTP/tool/job/browser 核心事件具有同一 trace 上下文；0 个 query token。
- 日志敏感字段负面测试覆盖 Cookie、Authorization、代理凭证、内容、路径和指纹。
- SSE 断线重连后事件无缺口、无重复状态转换。
- 每个正式平台具有契约测试和至少一个真实测试账号验收记录。

## 14. 下一步

当前可由独立守护进程执行，且不会在桌面端未获用户操作时擅自触发已有队列任务：

```bash
alpha-nexus queue worker
```

下一批聚焦内容/发布 UI 与平台适配：

1. 落地 Alpha 自有 `SelectorCatalog`、fixture 与版本策略，再逐个平台做登录/发布测试。
2. 完成内容中心、发布向导、动态平台设置表单与前端任务时间线。（内容 CRUD、媒体关联、发布向导与安全发布模板 CRUD 已完成；模板仅作为任务设置快照，待各 Adapter 逐项消费并验收。）
3. 为守护进程补平台错误码的退避/重试策略，以及可配置桌面/Webhook 通知 Provider。
4. 获得单独授权后，才用测试账号/素材验证真实登录和发布；不使用生产会话。

真实平台发布需要测试账号和发布素材；执行前单独确认测试范围，不使用生产账号做破坏性试验。任何付费 AI/API、批量下载或生产发布仍需单独明确授权。

## 附录 A：Alpha-Nexus 实现落点

| 路径 | 复用/修改方向 |
|------|---------------|
| `internal/ui/server.go` | 注入 Registry、Emitter、Worker；版本化 API、health/recovery/request ID、SSE、`http.Server.Shutdown` |
| `internal/ui/auth_handlers.go` | 分离 transport auth 与 user JWT；取消“零用户即关闭 Sidecar 认证”和 query token |
| `electron/main.js` | 动态端口/descriptor 握手、Token 内存保管、后端输出接管、退出预算与受限重启 |
| `electron/backend-api.js` | 动态 Base URL、Main-process IPC、统一超时与稳定错误码 |
| `internal/tools/tools.go` | 保持唯一能力注册表；在 `Registry.Call` 注入 trace、duration 和 typed event |
| `internal/mcp/server.go` | 继续直接调用 Registry，不经 Turbo REST；Connector 独立且可关闭 |
| `internal/publish/runner.go` | 从同步请求生命周期升级为持久状态机；事件写失败显式返回；成功证据门 |
| `internal/queue/queue.go` | lease/heartbeat/crash reclaim、取消、重试排期；Poll 错误进入 Emitter |
| `internal/db/db.go`、`repository.go`、`migrations/` | 合并迁移真源，扩展 correlation/error 字段，启用 FK/索引与启动 quick-check，记录最近成功置前账号，禁止 `job_id=0` |
| `internal/browser/facade.go` | 保持唯一浏览器入口，不旁路 |
| `internal/browser/supervisor.go` | typed browser event、账号切换成功/失败轨迹、job_id、start_failed、去重 stopped、heartbeat 限流与错误处理 |
| `internal/trace/trace.go` | 只保留 ID 兼容层，context/Emitter 迁入 `internal/observability` |
| `internal/batch/batch.go` | 任意 map NDJSON 改 typed Event sink，返回 marshal/write 错误 |
| `ui-web/src/components/SettingsView.tsx` | 日志级别、保留期、清理从 localStorage 改为后端真实配置 |

实施顺序必须是：测试契约 → Schema migration → observability → Sidecar supervision → Worker/API → UI → 平台 Adapter。这样可以在不触碰现有账号会话和未提交业务改动的前提下逐步交付。
