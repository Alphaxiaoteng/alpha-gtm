# Bot 工作台（工具调用 / 交接 / 长任务）

Bot 的每一步都要看得懂、等得到、停得下。本文件记录三块的真实数据来源与接口。

## 1. 工具调用

**数据源**：`agent_tool_calls`（`params_json / result_json / error_code / status / created_at / updated_at`）。

- `GET /api/agent/conversations/{id}/tool-calls?limit=50`
  - 返回 `tool_calls[]`，按时间倒序：`id, tool_name, workspace, risk_level, status, impact_preview, params, result, error_code, created_at, updated_at, duration_ms`
  - `params` / `result`：解析成 JSON；超过 2000 字符退化为截断字符串（结尾 `…`）
  - 脱敏：键名含 `token / secret / password / cookie / authorization / api_key` 的值替换为 `***`（递归）
  - 空会话与未知会话返回 `{"tool_calls":[]}`（不是 404）
- 前端卡片：
  - 头部：状态点 + 工具名 + 影响摘要 + 耗时（`2.1s` / `850ms`）+ 状态
  - 展开：**参数**、**结果**、**错误码 + 下一步建议**
  - 失败 / 执行中 / 等待批准默认展开；已完成默认折叠
  - SSE 只覆盖状态，数据库时间轴保证刷新后历史不丢
  - 「在详情面板打开」保留原来的上下文抽屉

## 2. subagent 交接

**数据源**：`agent_delegations`（`status / task / result / error_code / created_at / updated_at`）。

- 运行中：`@Bot` + 任务 + 已运行时长 + 停止
- 结束（保留最近 3 条）：
  - `completed`：结果前 120 字
  - `failed`：`error_code` 或「没有返回原因」
  - `cancelled`：已停止 + 用时
- 有交接在跑时 2 秒轮询，空闲 15 秒

## 2.5 会话历史

Bot 默认只打开该 Bot 的最新会话，历史对话原本没有入口。

- 顶部「最近会话」按钮（`data-testid="agent-history"`）
- 列出当前 Bot 最近 8 个会话：标题 + 更新时间
- 点击即切回该会话（`requestAgentConversation`），运行中不可切换
- 20 秒刷新一次，不轮询占用

## 3. 长任务运行

- 运行条：`运行中 · 已用 mm:ss · 工具 N 次` + 停止
- 超过 1 分钟追加「仍在运行，可继续等待或停止」
- 停止后由已有 run 摘要显示「已停止」，不显示成功

## 验收（2026-11-20）

- `go test ./internal/agenthub/`、`go test ./internal/ui/ -run TestAgentConversationToolCallsRoute` 通过
- 前端 `tsc 0`，`node --test` 358 通过
- 真机：打开「调研 reacher」→ 工具调用卡片显示 `account.list · 读取已入库账号的非敏感摘要 · 2.1s · 已完成`，展开可见参数与结果
- 真机：顶部「最近会话」切回 Alpha Agent → 3 条交接结束卡片（`完成 · 用时 18.2s` + 结果预览）
- 截图：`shots/bot-tool-card-final-1440.png`、`shots/bot-delegation-done-final2-1440.png`

## 已知缺口

- 运行条本身的「进行中」状态没有真机截图：本轮模型调用失败（provider 额度），没有新增长任务可跑；代码路径与静态断言已覆盖。
- 交接结束卡片的用时来自 `created_at → updated_at`，历史数据没有单独记录耗时，显示的是墙钟差值。
