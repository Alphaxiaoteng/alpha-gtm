# AgentPush 全量落地说明（Alpha-Nexus 主线）

三份 MD（PRD / TECH / IMPLEMENTATION）的能力在本仓用 **Go 实现**。  
Node monorepo **不迁移**；Electron / 本地 UI 继续作操作台。
为避免过度工程，Alpha-Nexus 不实现本地后台定时器、到点唤醒或周期采集；业务动作只由用户或 Agent 当次明确发起。内容库排期只在该次显式准备中尝试填写平台原生时间控件。

- Goal 冲刺：[`GOAL.md`](./GOAL.md)（G1 ✅ · G2 in_progress · G-ALL）  
- 决策：[`memory/evolution/adr/0006-agentpush-full-scope.md`](../../memory/evolution/adr/0006-agentpush-full-scope.md)
- 接轨原则：[`BRIDGE.md`](./BRIDGE.md)

---

## 包地图（代码真源）

| 包 | 职责 |
|----|------|
| `internal/publish` | 发布状态机 + Runner（dry-run / 适配器调度 / NDJSON） |
| `internal/platforms` | 23 平台目录、22 Publisher 契约、readiness 与设置 schema |
| `internal/platforms/meta` | IG + Facebook Page 浏览器发布适配器 |
| `internal/media` | 本地素材索引、SHA256、mime、ffprobe 预检 |
| `internal/queue` | 队列 Item / Claim / Worker（concurrency 默认 1） |
| `internal/agents` | Content / Adapt / Publish / Monitor / Analytics Agent 骨架 |
| `internal/llm` | Ollama / OpenAI-compatible HTTP 网关 |
| `internal/trace` | trace id + 步骤记录 |
| `internal/browser` | **Facade** = Browser Provider 真源（CDP lease/ports） |
| `internal/db` | SQLite 真源与 migrate |
| `internal/cli` | 全部子命令入口 |
| `internal/batch` | 批量 + NDJSON |
| `internal/errors` | 错误码（含历史 Lite 拒绝码） |

新增适配器：在 `internal/platforms/<name>/` 实现 `Publisher` 并 `RegisterPublisher`；**不要**在 UI 里写发布流水线。

---

## Epic 对照（AP-xxx）

| Epic | 包/落点 | 状态说明 |
|------|---------|----------|
| EPIC-100 Foundation | `db` migrate、领域表、`trace`、CLI | 基线可用 |
| EPIC-200 Content/Media | `internal/media`；content/media CLI | 库函数可用；命令面按 GOAL 扩展 |
| EPIC-300 Browser | Facade = Provider；screenshot 辅助 | ✅ 主线 |
| EPIC-400 Workflow | `publish` 状态机；`queue`；retry/audit | G1 状态机 ✅；队列脚手架 |
| EPIC-500 Agents | `agents` + `llm` 五 Agent | 骨架；无密钥模板回落 |
| EPIC-600 Adapters | 22 个发布代码 | Meta 2 best-effort；YT/TikTok 受控浏览器路径；其余按 manual handoff / legacy API 边界；custom unavailable |
| EPIC-700 Ops/Data | metrics/export/audit/queue CLI | 规划 + 部分表/脚手架 |

「脚手架 / 接口已有」≠「生产级真发完成」。真提交以 `GOAL.md` G2 及后续 Goal 验收为准。

---

## 诚实边界

- **真点发布按钮**：Meta 走多步 CDP（打开创作器/尝试填表）；DOM 变更时回落 `manual_takeover`，不伪造 published。  
- **受控浏览器提交**：YouTube/TikTok direct live 只准备后台；只有冻结单目标计划经 `submit-bound` 单次审批才可点击一次。代码契约已接线，但真实账号 Published 验收尚未完成。
- **OAuth API 上传**：YouTube/TikTok/X 原生 Connector 尚未实现；legacy API adapter live 固定 `API_CONNECTOR_UNAVAILABLE`。X 没有受控浏览器提交路径。
- **LLM**：默认 Ollama/OpenAI-compatible HTTP；无密钥时 Agent 返回本地模板改写。  
- **媒体索引**：用户明确执行 CLI `media index` 或桌面端导入时进行一次本地扫描；不提供目录监听或后台轮询。
- **Lite 四平台**：dry-run 已接线；live 仅安全人工接管。XHS 会校验设置快照，其余打开创作后台；都不自动点击发布。
- **未接线工具**：显式返回 `TOOL_NOT_IMPLEMENTED`，不得用 planned success 冒充完成。

---

## 命令面（目标摘要）

```bash
alpha-nexus platform list
alpha-nexus media import|index|preflight
alpha-nexus content version|adapt
alpha-nexus queue enqueue|list|run
alpha-nexus agent content|adapt|publish|monitor|analytics
alpha-nexus metrics list|collect
alpha-nexus audit list
alpha-nexus export data
alpha-nexus publish -P <platform> -c N -t N [--dry-run] [--auto-submit]
```

已稳定的基础命令仍见根 `README.md` / `docs/CLI.md`（account / browser / meta / batch / ui 等）。

---

## 与 BRIDGE 的关系

| 文档 | 角色 |
|------|------|
| PRD / TECH / IMPLEMENTATION | 产品与原始计划（只增不改业务事实） |
| BRIDGE | 栈对齐与分期原则 |
| GOAL | 当前冲刺 AC |
| **本文** | 包级落地真源与 Epic 诚实状态 |
| ADR 0005 / 0006 | 落在 Go 主线；全量能力面 in scope |
