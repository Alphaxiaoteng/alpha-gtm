# 0022. YouTube/TikTok 仅经冻结单目标计划受控浏览器提交

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-07-26 |
| Area | publish |

## Context

YouTube 与 TikTok 已有经 Go 控制面调用的创作者后台 Worker，可上传媒体、填写经过审核的字段，并在明确许可时构造一次最终点击。旧平台目录仍把两者与 X 一并归类为 API dry-run，占位 Adapter 的 live 请求固定返回 `API_CONNECTOR_UNAVAILABLE`。这使真实执行路径、readiness 和 CLI 文案互相矛盾。

最终点击是不可逆动作；上传完成、Toast、处理完成或按钮可用都不是 Published 证据。真实账号 DOM 与后台 Published 验收也尚未完成，因此不能把“有 Worker”写成“已真发成功”。

## Decision

1. YouTube/TikTok 的规范执行面使用 `ExecutionPublisher` 选择受控浏览器 Worker，浏览器生命周期仍只走 BrowserFacade。
2. 直接 CLI / Tool live 只允许 `prepare`，不得点击最终发布；直接 `--auto-submit` 在数据库写入或浏览器动作前返回 `PUBLISH_FROZEN_SNAPSHOT_REQUIRED`。
3. 最终点击只允许 `publish.plan.submit`：计划必须已冻结且只有一个 target，快照哈希必须匹配，并消费绑定账号、资产、媒体哈希和设置的 `submit-bound` 单次 approval/nonce。
4. Runner 再次校验 `plan_id`、`plan_target_id` 和 64 位 `snapshot_hash`，并在调用 Worker 前持久化单次 submit marker，禁止重复提交。
5. 点击后保持等待创作者后台核验；没有公开链接、外部内容 ID、后台截图及明确 `Published` / `Public` 状态，不写 `publish_records`。
6. YouTube/TikTok 的 legacy API Adapter 继续 live fail-closed；X 继续只有 API dry-run，不获得浏览器提交能力。

## Consequences

### 正

- readiness、CLI 和真实执行选择器描述同一条受控路径。
- 直接命令、队列和旧 API Adapter 不能绕过冻结快照与独立审批。
- 保留未来原生 API Connector 的接入点，不引入第二套 job 或浏览器生命周期真源。

### 负 / 代价

- 受控点击能力不代表真实账号验收；当前不能宣称 YouTube/TikTok live 已通过。
- 页面变化、登录检查或后台状态不明确时必须停止并人工核验，不能自动重试。
- 原生 API 上传仍未实现。

## Alternatives

| 方案 | 为何未选 |
|------|----------|
| 继续将 YouTube/TikTok 与 X 都标为 dry-run | 与已存在的 Browser Worker 执行路径不一致，隐藏真实安全边界 |
| 允许直接 CLI `--auto-submit` | 无冻结快照和 submit-bound 单次审批，存在错账号、错素材与重复点击风险 |
| 将 Worker 结果直接记为 published | 上传或点击结果不是创作者后台 Published 证据 |

## Related

- ADR 0003：BrowserFacade 为浏览器生命周期真源
- ADR 0007：Go 控制面 + 可选 Worker
- `docs/CLI.md`
- `docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md`
