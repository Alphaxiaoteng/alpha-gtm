# 长任务：Bot 工作台 · 工具调用 / subagent 交接 / 长任务运行

目标：Bot 每做一步都看得懂、等得到、停得下。不新增能力面，只把已有真实数据展示完整。

约束：
- 只用真实后端数据；读不到就显示「读不到」，不编造耗时/结果。
- 不引入新依赖；沿用现有 Tailwind + SWR + SSE。
- 每个改动都要有测试：Go 侧 handler 测试，前端 node:test 源码断言 + 真机截图。

---

## A. 工具调用（每个工具都能展开）

现状：`AgentDrawer` 只有一行 `tool_name · impact_preview · 状态`，最多 12 条，看不到参数与结果。
`agent_tool_calls` 表已经有 `params_json / result_json / error_code / created_at / updated_at`。

要做：

- [x] 后端：`GET /api/agent/conversations/{id}/tool-calls?limit=50`
  - 返回按 `created_at` 倒序：`id, tool_name, workspace, risk_level, status, impact_preview, params, result, error_code, created_at, updated_at, duration_ms`
  - `params` / `result` 解析成 JSON 对象返回；解析失败给字符串；单字段截断 2000 字符
  - 脱敏：键名含 `token|secret|password|cookie|authorization` 的值替换为 `***`（大小写不敏感，递归一层即可）
  - 空会话返回 `{"tool_calls":[]}`，不 404
- [x] 前端：工具卡片
  - 头部：状态点（提议/等待批准/执行中/完成/失败）+ 工具名 + 一句话影响 + 耗时（`duration_ms`，缺失显示「—」）
  - 展开区三个 section：**参数**、**结果**、**错误**（有 error_code 时显示错误码 + 下一步建议）
  - 折叠状态默认：失败/执行中展开，完成折叠
  - SSE 事件到达时按 `tool_call_id` 合并，不重复追加
  - 列表为空时不渲染整块

## B. subagent 交接

现状：底部一条 strip 只显示 `@Bot 正在处理：task` + 计时 + 停止；结束后消失，看不到结果。

要做：

- [x] 运行中：显示目标 Bot、任务前 80 字、已运行时长、停止按钮（保留）
- [x] 结束态：同一条变成结果卡片
  - `completed`：显示结果前 200 字 + 「查看对话」
  - `failed`：显示 `error_code` + 原文原因 + 「重试」
  - `cancelled`：显示「已停止」+ 用时
- [x] 交接结束后自动刷新父会话（已有），并保留最近 3 条结束记录，不立刻消失

## C. 长任务运行

现状：有 `isCancellingRun` 与 run status，但界面上只有「正在思考」点和失败提示。

要做：

- [x] 运行条：`运行中 · 已用 mm:ss · 工具 N 次`，右侧「停止」
- [x] 停止后显示「已停止 · 用时 mm:ss」，不显示成功
- [x] 工具次数来自本会话 tool calls 计数（真实数据；没有就显示「—」）
- [x] 长任务（>60s）在运行条上显示「仍在运行，可继续等待或停止」

## 验收

- [x] `go test ./internal/agenthub/ ./internal/ui/` 通过
- [x] `npx tsc -b --force` 0；`node --test test/*.test.mjs` 全绿
- [x] `go build ./...` 0
- [x] 真机：打开 Bot，跑一轮带工具调用的对话，截图证明参数/结果/耗时可见
- [x] 真机：@ 另一个 Bot 派活，截图证明运行中与结束卡片
- [x] 文档：`docs/BOT_WORKSPACE.md` 记录接口与状态
