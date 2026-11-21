# 本地诊断与日志

Alpha-Nexus 的日志目标是：先用稳定编号定位一次操作，再沿账号、任务、批次和浏览器事件还原故障，同时不把 Cookie、正文、URL、账号名称或本机路径写进可分享日志。

## 日志分层

| 层 | 位置 | 记录内容 | 保留 |
|---|---|---|---|
| Go 安全事件流 | `~/.alpha-nexus/logs/events-<pid>.jsonl` | 应用生命周期、HTTP 变更/失败/慢请求、Tool、浏览器和任务的白名单字段 | 单文件 10 MiB，含当前文件最多 5 份，权限 `0600` |
| Electron 壳事件流 | `~/.alpha-nexus/logs/electron-events.jsonl` | 壳启动、Sidecar 健康、Renderer/子进程退出和无响应 | 单文件 2 MiB，含当前文件最多 3 份，权限 `0600` |
| SQLite 事件真源 | `~/.alpha-nexus/app.db` | `operation_events`、`job_events`、`browser_events`、`account_login_events`、`audit_events`、`agent_runs` | 本地持久化；由业务数据策略管理 |
| 脱敏诊断快照 | 设置 → 日志与诊断 → 生成 | 数据库健康、状态计数、最近 100 条安全事件投影 | `diagnostic-*.json` 最近 10 份，权限 `0600` |

SQLite 事件表是本地业务证据，其中部分历史表允许消息或截图路径，不能直接作为可分享日志。需要提交问题时应优先生成脱敏诊断快照，并在分享前再次人工检查。

## 覆盖范围

- 桌面壳：进程启动、Sidecar 启停/健康、Renderer 崩溃、无响应、Electron 子进程退出。
- 应用：启动、数据库 `quick_check`、正常关闭和关闭失败。
- 本地 HTTP：所有写操作、所有 `4xx/5xx`、超过 2 秒的 API 请求；成功的轮询 GET 和静态资源不记，避免噪声。
- 前端：`window.error`、未处理 Promise、React 渲染边界；浏览器端先做 SHA-256，只上传错误指纹和固定类别。
- 账号与浏览器：账号 ID、平台、登录观察结果、头像来源/错误码、启动/停止/崩溃/置前结果、PID/CDP 端口。
- 发布与队列：job、batch、attempt、step、状态、稳定错误码、可重试性和耗时。
- Agent/Tool：trace、request、task、受控工具名、状态与错误码；不记录提示正文或隐藏推理。

## 关联字段

优先按以下顺序关联：

1. UI 报错响应中的 `requestId` → `request_id`
2. 一次工作流的 `trace_id`
3. `account_id`、`job_id`、`batch_id`、`task_id`
4. `component + event + step + error_code`

HTTP 日志只保存路由模板，例如 `POST.api.accounts.id.browser.start`；不保存实际 URL、查询参数或请求体。Go panic 和前端异常只保存 12 位哈希指纹，用于判断“是否同一个故障”，不保存堆栈正文。

## 快速排障

1. 在设置页点击“生成脱敏诊断快照”。
2. 先看 `database.quick_check` 和状态计数，确认是数据库、浏览器、发布、队列还是 Agent 层。
3. 用界面错误里的 `requestId` 搜索 `operation_events` 或 `events-<pid>.jsonl`。
4. 再按 `account_id` / `job_id` / `batch_id` 关联账号登录、浏览器和任务事件。
5. 若应用窗口本身打不开，先看 `electron-events.jsonl`，再看同时间段的 Go 事件文件。

## 明确禁止记录

- Cookie、Local Storage、平台密码、验证码、令牌和请求头。
- 内容标题/正文、Prompt、评论正文、DOM/截图内容。
- 完整 URL、查询参数、HTTP 请求体和第三方原始响应。
- profile 实际路径、素材路径、账号名称和公开账号标识。
- Go/JavaScript 原始堆栈；只保留不可逆短指纹。

新增日志字段必须先进入白名单；不能把任意 `message`、`args` 或属性 Map 直接传入安全事件流。
