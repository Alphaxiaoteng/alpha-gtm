# 0008. Turbo Push 能力采用混合绞杀集成

| 字段 | 值 |
|------|-----|
| Status | proposed |
| Date | 2026-07-16 |
| Area | architecture |

> 2026-07-16 范围修订：定时发布因过度工程退出 Alpha-Nexus 范围。本文其余混合绞杀、安全迁移和唯一真源决策继续有效。

## Context

最初目标包含 Turbo Push 的内容、账号登录、模板、自动发布、定时发布、记录、指标、代理、指纹和桌面能力；现已按上方范围修订排除知识库与定时发布。

约束：

- Alpha-Nexus 已确定 Go 控制面、SQLite 唯一真源和 BrowserFacade 唯一生命周期入口。
- 本机 `.turbopush` 是实时运行目录，包含主业务 DB、选择器目录、Sidecar、浏览器 Profile 和日志；`.tp` 只是外部加密导出包。
- Turbo Push 账号索引可见，但敏感数据解密失败。
- 实时 DB 有 2 个账号而浏览器目录有 3 个，无法安全推断账号/Profile 对应关系。
- Turbo Push 主服务、VIP、云登录和随机认证 Token 不应成为长期核心依赖。
- 当前 Alpha-Nexus 工作树有大量未提交修改。

## Decision

采用混合绞杀：

1. Alpha-Nexus 保持账号、内容、模板、任务、记录、指标和审计的唯一真源。
2. 所有 UI、CLI、MCP 统一走 tools.Registry。
3. 所有浏览器登录和原生发布统一走 BrowserFacade。
4. 新增可选 Turbo Connector，只作为未完成平台的临时 Publisher Provider。
5. Connector 不迁移 Cookie，不拥有账号真值，不直接写 Alpha 数据库。
6. 每个平台原生 Adapter 成熟后通过 Feature Flag 切换并移除 Connector 依赖。
7. 不集成知识库、Turbo 云会员、支付和邀请码。
8. Go Sidecar、动态端口认证和统一可观测性由 ADR 0009 单独约束。

## Consequences

### 正

- 可比完全原生更快获得平台覆盖。
- 不牺牲 Alpha-Nexus 的本地唯一真源。
- 迁移路径逐平台、可回滚。
- 避免复制不可解密的账号敏感数据。
- 最终可完全脱离 Turbo Push。

### 负 / 代价

- 过渡期需维护 Native 和 Connector 两类 Provider。
- Turbo Connector 可用性仍受 Turbo Push 版本、云登录和 VIP 影响。
- 需要统一两边事件和错误码。
- 真实 23 平台覆盖仍需持续 Adapter 开发和测试账号。

## Implementation status

已实现安全迁移边界：`.turbopush/data.db` 只读取允许的账号、平台设置元数据与内容字段；`.tp` 明确拒绝；Cookie、设备材料、浏览器 Profile、源设置 JSON 与原始发布快照不会跨入 Alpha。导入使用单一源/目标事务，重导默认 insert-only，因此不会覆盖 Alpha 已登录状态、本地设置、内容编辑或发布计数。

尚未引入 Turbo Connector，也未宣称任何 Turbo 平台的真实发布兼容。原生 Adapter 的逐平台验收、SelectorCatalog、测试账号三次连续成功和 Connector feature flag，仍是本 ADR 的后续交付条件。

## Alternatives

| 方案 | 为何未选 |
|------|----------|
| 全部原生一次完成 | 长期正确，但首个可用版本周期过长 |
| Alpha 直接依赖 Turbo REST/MCP | 双真源、闭源依赖、VIP 与账号解密问题不可接受 |
| 仅复制 Turbo UI | 无真实 Worker/Adapter，无法满足自动发布 |

## Success Metrics

- SQLite 是唯一业务真源。
- 0 明文凭证落库。
- 0 重复发布。
- 0 假 published。
- Connector 可按平台关闭。
- 每个平台完成原生验收后不再依赖 Connector。

## Review Date

首批 6 个浏览器平台原生 Adapter 完成后复审。

## Related

- docs/agentpush/TURBOPUSH_DEEP_DIVE.md
- ADR 0003、0005、0006、0007、0009
