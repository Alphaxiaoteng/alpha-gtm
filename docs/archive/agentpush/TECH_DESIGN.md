# AgentPush 技术设计文档

> **状态：Superseded。** 本文是原始 Node/Playwright 方案基线，已被 accepted ADR 0007 的 Go + chromedp 控制面取代。保留用于设计溯源，不得作为当前实现指令。

**版本：** v1.0  
**架构原则：** Local-first、API-first、Browser fallback、可审计自动化。

## 1. 总体架构

```text
AgentPush Desktop (Electron + React)
  ├─ 内容中心 / 账号中心 / 发布中心 / 数据中心
  ├─ Agent Orchestrator
  └─ Workflow Runtime
          │ IPC / localhost HTTPS
Automation Service (Node.js + TypeScript)
  ├─ CDP Gateway
  ├─ Playwright Workers
  ├─ Browser Profile Manager
  ├─ Task Queue
  └─ Platform Adapter Layer
          │ Chrome DevTools Protocol
BrowserOS / Cloak Browser / Chrome Provider
          │
Platform API 或平台 Web 后台
```

## 2. 技术栈

| 层 | 选型 |
|---|---|
| Desktop | Electron、React、TypeScript、Tailwind、Zustand |
| 自动化服务 | Node.js 22、TypeScript、Playwright |
| AI/媒体 Worker | Python 3.12、FastAPI、FFmpeg、ffprobe |
| 浏览器底座 | BrowserOS + CDP；Cloak/Chrome 为可插拔 Provider |
| 本地数据库 | SQLite + Drizzle ORM 或 Prisma |
| 密钥 | macOS Keychain、Windows Credential Manager/DPAPI |
| 任务执行 | SQLite-backed queue + 状态机 |
| LLM Gateway | LiteLLM-compatible provider adapter |
| 观测 | OpenTelemetry、Sentry、Local Event Log |

## 3. Monorepo

```text
agentpush/
├── apps/
│   ├── desktop/
│   ├── automation-service/
│   └── ai-worker/
├── packages/
│   ├── core/
│   ├── db/
│   ├── browser/
│   ├── adapters/
│   ├── agents/
│   ├── media/
│   ├── llm/
│   ├── ui-kit/
│   └── shared/
├── config/platform-rules/
├── tests/
└── docs/
```

## 4. Browser/CDP 设计

### 4.1 Provider 抽象

```typescript
interface BrowserProfileProvider {
  providerName: "browseros" | "cloak" | "chrome";
  createProfile(input: CreateProfileInput): Promise<BrowserProfile>;
  launch(profileId: string): Promise<{ cdpWsEndpoint: string; pid: number }>;
  stop(profileId: string): Promise<void>;
  getHealth(profileId: string): Promise<ProfileHealth>;
}
```

### 4.2 CDP 连接

```typescript
import { chromium } from "playwright";

const browser = await chromium.connectOverCDP(cdpWsEndpoint);
const context = browser.contexts()[0];
const page = context.pages()[0] ?? await context.newPage();
```

### 4.3 Profile 隔离

- 每个账号拥有独立 Profile 路径、Cookie、LocalStorage、IndexedDB、缓存与下载目录
- 同一 Profile 的写操作必须使用互斥锁串行执行
- 不同账号可在资源阈值内并行执行
- 浏览器崩溃后保存当前 checkpoint、截图、DOM Snapshot 和错误信息
- 自动化与用户手动操作不可同时占有同一 Profile

## 5. 发布状态机

```text
queued → preflight → browser_attached/api_authenticated
→ asset_processing → asset_uploaded → metadata_filled → awaiting_review
→ submitting → published

失败：retryable_failed → queued
人工：manual_takeover → resume 或 completed
终止：permanent_failed / cancelled
```

```typescript
type PublishTaskStatus =
  | "queued" | "preflight"
  | "browser_attached" | "api_authenticated"
  | "asset_processing" | "asset_uploaded"
  | "metadata_filled" | "awaiting_review"
  | "submitting" | "published"
  | "retryable_failed" | "permanent_failed"
  | "manual_takeover" | "cancelled";
```

## 6. Adapter 接口

```typescript
interface PlatformAdapter {
  platform: Platform;
  validate(payload: PlatformPayload, account: PlatformAccount): Promise<ValidationResult>;
  getPublishStrategy(account: PlatformAccount): Promise<"api" | "browser" | "hybrid">;
  publishByApi?(task: PublishTask, ctx: AdapterContext): Promise<PublishResult>;
  publishByBrowser(task: PublishTask, browser: BrowserContext, ctx: AdapterContext): Promise<PublishResult>;
  collectMetrics?(post: PublishedPost, ctx: AdapterContext): Promise<PostMetrics>;
  healthCheck(account: PlatformAccount, browser: BrowserContext): Promise<AccountHealth>;
}
```

Adapter 实现要求：

- 选择器、Role Locator 和页面断言集中存放
- 所有发布流程支持 `dryRun`，仅填表或存草稿而不提交
- 最终提交前必须回读标题、正文、素材状态和发布按钮状态
- 页面异常时保存 Screenshot、HTML/DOM Snapshot、网络错误摘要
- 语义/视觉 Agent 仅能作为选择器失败后的 fallback

## 7. Agent Tool Contract

```typescript
type AgentTool =
  | "llm.generate" | "files.read" | "files.write"
  | "media.transcode" | "browser.attach" | "browser.snapshot"
  | "browser.action" | "browser.upload" | "api.request"
  | "task.update" | "task.require_approval";

interface AgentExecutionContext {
  taskId: string;
  accountId?: string;
  platform?: Platform;
  approvalMode: "always_review" | "trusted_auto_submit";
  allowedTools: AgentTool[];
  maxSteps: number;
  timeoutMs: number;
}
```

## 8. 本地存储

```text
~/Library/Application Support/AgentPush/
├── agentpush.db
├── profiles/
├── assets/
├── content/
├── transcodes/
├── screenshots/
├── logs/
└── exports/
```

- SQLite 保存内容、任务、账号元数据、指标、日志和检查点
- Keychain/DPAPI 保存 API Key、OAuth Token、密钥引用
- 素材库只建立索引，不强制复制源文件
- 使用 SHA-256 进行文件去重
- 转码缓存具备容量阈值与 LRU 清理策略

## 9. 安全与合规

- API 优先，浏览器仅用于用户授权账号的功能补齐
- 不实现验证码破解、MFA 绕过或平台对抗能力
- 对敏感步骤、异常页面、最终提交支持强制人工确认
- 所有 Agent 工具调用和浏览器写操作写入审计日志
- BrowserOS/Cloak 通过 Provider 抽象接入，避免业务逻辑绑定单一浏览器
- 商业分发前需要对 BrowserOS 和 Cloak Browser 的许可证、分发方式与修改义务完成法务审查

## 10. 可观测性

每个任务必须关联 `traceId`，并记录：

- 状态迁移时间线
- API 请求摘要及错误类型
- 浏览器步骤、页面 URL、截图路径
- Profile 锁申请与释放
- 文件上传、转码和下载事件
- Agent 决策、工具调用、重试和人工接管

## 11. 测试

| 类型 | 目标 |
|---|---|
| Unit | 规则引擎、状态机、Payload 映射、重试 |
| Integration | SQLite、队列、FFmpeg、LLM Gateway |
| Adapter | 字段校验、选择器、响应解析 |
| Browser E2E | 上传、填表、草稿、发布、恢复 |
| Visual Regression | 平台关键页面变化检测 |
| Chaos | 断网、浏览器崩溃、磁盘不足、会话失效 |

## 12. 性能目标

| 指标 | 目标 |
|---|---:|
| Desktop 启动 | < 5 秒 |
| CDP 连接 | < 2 秒 |
| Profile 启动 | < 8 秒 |
| UI 任务状态更新 | < 500 ms |
| 默认全局浏览器并发 | 3 |
| 单账号写操作并发 | 1 |
| 成功任务日志完整率 | 100% |
