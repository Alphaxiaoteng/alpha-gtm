# AgentPush 开发实施计划

> **状态：Superseded / 历史计划。** 32 周、Node/Python Squad 和原始平台路线不再是当前执行计划；架构以 accepted ADR 0007 为准，当前范围与验收以 Goal 文档为准。

> 本文保留原始 10 平台长期计划。当前代码事实以 GOAL.md / FULL_IMPLEMENTATION.md 为准：23 项目录、22 个发布代码、2 Meta best-effort、17 manual handoff、3 API dry-run-only、custom unavailable；无定时发布或周期采集。

**目标：** 在 32 周内交付 AgentPush 完整版 v1.0，不设 MVP 阶段。  
**交付范围：** 多平台管理、本地内容和素材系统、BrowserOS/CDP、Cloak Provider、内置 Agent、10 平台 Adapter、队列、分析、macOS/Windows 安装包。定时发布不在范围内。

## 1. 团队配置

| 角色 | 人数 | 职责 |
|---|---:|---|
| 产品负责人 | 1 | 范围、平台策略、验收、风险决策 |
| 产品设计师 | 1 | 信息架构、编辑器、审核、任务体验 |
| Electron/前端工程师 | 2 | Desktop、内容中心、账号、任务、数据 UI |
| Node 自动化工程师 | 2 | CDP、Playwright、Browser Provider、Adapter |
| Python/AI 工程师 | 1-2 | Agent、FFmpeg、分析、LLM 服务 |
| 基础设施工程师 | 1 | DB、队列、状态机、更新、日志 |
| QA/SDET | 1-2 | E2E、Fixture、回归、性能和故障注入 |
| DevOps/Release | 0.5-1 | CI/CD、签名、安装包、崩溃报告 |

## 2. 并发 Subagent/Squad 划分

| Squad | 负责人类型 | 并发边界 | 主要输出 |
|---|---|---|---|
| A：Desktop & UX | 前端/设计 | UI、IPC Contract | 桌面端、内容中心、账号与任务 UI |
| B：Browser Runtime | 自动化 | Provider、CDP、Profile | BrowserOS/Cloak、Playwright、人工接管 |
| C：Publish Platform | 自动化/后端 | Platform Adapter | 10 平台 API 与浏览器发布 |
| D：Agent & Media | AI/Python | Agent、FFmpeg、LLM | 文案、适配、转码、洞察 |
| E：Workflow & Data | 后端 | 状态机、队列、DB | 任务引擎、审计、指标 |
| F：Quality & Release | QA/DevOps | 测试、打包、发布 | E2E、回归、签名、Release |

### 并发协作约束

- `packages/core` 维护共享领域模型、事件与状态机，所有 Squad 不得私自定义重复类型。
- Adapter 只能依赖 `core`、`browser`、`db`、`shared`，不得直接依赖 Desktop UI。
- Agent 通过 Tool Contract 调用能力，不得直接操作数据库或 Playwright 实例。
- 每周冻结一次 IPC、领域模型和 Adapter Contract，避免并行开发破坏接口。
- 每个 Squad 必须提供单元测试、集成测试、变更日志和可复现 Fixture。

## 3. 32 周路线图

| 周期 | A Desktop & UX | B Browser Runtime | C Platform | D Agent & Media | E Workflow & Data | F Quality & Release |
|---|---|---|---|---|---|---|
| W1-W4 | 应用 Shell、导航、内容模型 UI | Provider Contract、CDP PoC | Adapter Contract | LLM/FFmpeg PoC | SQLite、事件、状态机 | CI、测试基座 |
| W5-W8 | 素材库、编辑器、账号 UI | BrowserOS Provider、锁、截图 | YouTube/TikTok API | Content/Adapt Agent | Queue、审批 | Browser E2E 框架 |
| W9-W12 | 发布向导、队列 UI | Cloak Provider、人工接管 | 抖音/小红书 Adapter | Publish/Monitor Agent | 重试、Checkpoint、审计 | 关键流程回归 |
| W13-W16 | 任务详情、失败中心 | 崩溃恢复、Profile 健康 | 视频号/公众号/IG/FB | 规则引擎、媒体变体 | 多账号并发、限速 | Chaos 测试 |
| W17-W20 | 数据中心、设置 | 性能优化、Trace | X/快手、指标采集 | Analytics Agent | 跨平台指标模型 | 真机平台认证 |
| W21-W24 | UX 打磨、批量发布 | 稳定性加固 | 全平台缺口修复 | Prompt/成本优化 | 数据导出、备份恢复 | 全量回归 |
| W25-W28 | 文案、空状态、帮助 | 浏览器兼容矩阵 | Adapter 兼容矩阵 | Agent 可靠性评测 | 性能、迁移、故障恢复 | Beta、漏洞修复 |
| W29-W32 | Release UI | Provider 发布配置 | 平台认证收尾 | 生产模型策略 | 升级/遥测开关 | 签名、安装包、GA |

## 4. Epic Backlog

### EPIC-100：Foundation

- AP-101 初始化 pnpm Monorepo、Turbo/Nx、TypeScript strict mode
- AP-102 Electron 主进程、Renderer、IPC 安全白名单
- AP-103 SQLite Schema、Migration、Repository 层
- AP-104 领域模型：Content、Asset、Account、Profile、Task、Post、Metric
- AP-105 Local Event Bus 与 traceId
- AP-106 macOS Keychain、Windows DPAPI 凭据服务
- AP-107 开发环境 Docker/Mock Service

### EPIC-200：内容与素材

- AP-201 素材根目录管理与 Chokidar 监听
- AP-202 SHA-256 去重与索引
- AP-203 ffprobe 元数据提取
- AP-204 FFmpeg 转码与封面抽帧
- AP-205 Markdown/富文本编辑器
- AP-206 内容版本、语言版本、差异比较
- AP-207 内容模板与平台规则库
- AP-208 发布前媒体预检

### EPIC-300：Browser Runtime

- AP-301 BrowserProfileProvider 接口
- AP-302 BrowserOS Provider：启动、发现 CDP、attach
- AP-303 Chrome Provider：连接用户授权 Profile
- AP-304 Cloak Provider：Profile 生命周期与 CDP endpoint
- AP-305 Playwright CDP Session 封装
- AP-306 Profile Lock、Lease、Heartbeat
- AP-307 Screenshot、DOM Snapshot、Trace、Network Error 捕获
- AP-308 浏览器崩溃恢复
- AP-309 人工接管与继续执行

### EPIC-400：Workflow Runtime

- AP-401 PublishTask 状态机
- AP-402 SQLite-backed Queue
- AP-404 指数退避和错误分类
- AP-405 Checkpoint 与断点恢复
- AP-406 审核策略与 Trusted Auto Submit
- AP-407 账号限速、每日上限、全局并发
- AP-408 审计日志和任务导出

### EPIC-500：AI Agent

- AP-501 LLM Gateway：OpenAI、Anthropic、Gemini、DeepSeek、Ollama
- AP-502 Prompt Versioning 与评测集
- AP-503 ContentAgent：标题、描述、标签、翻译、CTA
- AP-504 AdaptAgent：平台 Payload、内容压缩、媒体计划
- AP-505 PublishAgent：受限 Tool Runtime
- AP-506 MonitorAgent：会话与异常检查
- AP-507 AnalyticsAgent：趋势和优化建议
- AP-508 Agent Guardrail：工具白名单、步数、时间、审批

### EPIC-600：Platform Adapter

- AP-601 YouTube API：OAuth、上传、元数据、封面
- AP-602 TikTok API：授权、本地上传、状态查询
- AP-603 Instagram Graph API：容器、发布、Insights
- AP-604 Facebook Graph API：Page、媒体、Insights
- AP-605 X API：文本、媒体、线程
- AP-606 抖音 Browser/CDP：上传、标题、标签、发布
- AP-607 小红书 Browser/CDP：图文、视频、话题
- AP-608 视频号 Browser/CDP：上传、描述、状态
- AP-609 公众号 Browser/API：文章草稿、富文本、素材
- AP-610 快手 Browser/CDP：视频、图文
- AP-611 Adapter dryRun、Fixture、回归套件

### EPIC-700：运营与数据

- AP-701 发布队列与实时日志 UI
- AP-703 账号健康仪表盘
- AP-704 指标手动采集
- AP-705 内容/平台/账号/素材维度报表
- AP-706 跨平台内容比较
- AP-707 失败任务中心与一键重试
- AP-708 数据导出与本地备份

## 5. Definition of Done

每个任务满足以下条件才可关闭：

- 功能实现通过代码审查
- 单元测试覆盖核心逻辑
- 对外接口有类型定义与文档
- 失败路径有明确错误码和用户可读提示
- 涉及浏览器操作时保存 E2E Fixture 或 Mock
- 涉及写操作时有审计事件
- 涉及隐私/密钥时完成安全检查
- 更新 Changelog 与关联 PRD 条目

## 6. CI/CD

```text
Pull Request
  → lint + typecheck
  → unit tests
  → integration tests
  → adapter fixture tests
  → build desktop
  → Playwright smoke tests
  → security/license scan
  → preview artifact

main merge
  → nightly browser E2E
  → macOS/Windows package
  → code signing
  → internal beta channel
```

## 7. 测试门禁

| 门禁 | 条件 |
|---|---|
| Unit | Core/Adapter/Agent 新增逻辑必须通过 |
| Typecheck | TypeScript strict 无错误 |
| E2E Smoke | Browser Profile 启动、上传、任务状态迁移通过 |
| Adapter Regression | 每个平台关键流程 Fixture 不回退 |
| Security | 无明文 Token、无危险 IPC、无未审查依赖 |
| Release | macOS/Windows 打包、签名、升级流程通过 |

## 8. 风险与决策点

| 风险 | 决策/缓解 | Owner | 截止 |
|---|---|---|---|
| BrowserOS/Cloak 许可证 | 首周发起法务审查；保留 Provider 抽象与 Chrome 回退 | 产品/法务 | W2 |
| 平台 API 审核 | W1 同步提交开发者申请；Browser 流程作为兜底 | 平台 Squad | W1 |
| 页面频繁改版 | 集中选择器、视觉回归、AI fallback、人工接管 | 自动化 Squad | 持续 |
| 账号风险 | 限速、预检、审核、无验证码绕过 | 产品/自动化 | 持续 |
| 大视频上传失败 | 网络预检、断点/API 上传、重试、缓存 | Media Squad | W10 |
| Agent 误操作 | Tool 白名单、状态机、回读验证、审批 | AI Squad | W12 |

## 9. 发布验收清单

- [ ] macOS/Windows 安装、升级、卸载验证完成
- [ ] 10 平台 Adapter 在测试账号完成认证
- [ ] 本地素材导入、转码、发布和恢复全链路通过
- [ ] API-first 与 Browser fallback 策略通过
- [ ] 多账号隔离与同 Profile 互斥锁通过
- [ ] 审核、人工接管、重试、断点续跑通过
- [ ] URL、截图、日志、指标可追溯
- [ ] 许可证、隐私、密钥管理审查完成
- [ ] 崩溃报告、性能监控、备份恢复完成
