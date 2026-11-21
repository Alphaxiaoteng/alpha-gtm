# 0020. 精选 TikHub 调用工作台与长期原始响应留档

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-07-25 |
| Area | tikhub, ui, sqlite, trend-radar |

## Context

固定接口目录只能说明供应商存在什么接口，不能完成真实调用、保存和复盘。趋势雷达需要长期积累搜索、内容与博主数据，同时 TikHub 接口会计费、结构会变化，微信响应还包含超过 JavaScript 安全整数范围的 ID。

用户要求基于官方文档，只配置最常用、最好用的接口，并提供可调用位置；每次抓取要有详细记录，主要数据长期保存在本机。

## Decision

- 不复制 TikHub 完整 OpenAPI，只维护抖音、小红书、视频号、快手和公众号的趋势雷达主链。
- 调用只接受 `endpoint_id` 和固定参数 Schema；URL、HTTP 方法、固定垂类参数均由应用维护。
- 真实请求必须满足 Provider 已激活和本次 `confirm=true`，不增加后台轮询或静默采集。
- 调用开始先写 `tikhub_api_calls.running`；成功、供应商失败和本地保存失败都写稳定终态。
- 成功响应完整写入 `tikhub/calls/YYYY-MM-DD/` 的权限 `0600` JSON；SQLite 只保存检索索引、参数、状态、规模、耗时和路径。
- 保存原始响应前使用 `json.Decoder.UseNumber()`，保持 64 位数字的文本精度。
- 只有原始 JSON 原子落盘成功后，调用才标记为 `succeeded`。
- 工作台提供调用历史和原始记录回读；供应商错误正文不写入日志或 UI，避免潜在令牌回显。

## Consequences

### 正

- 每次外部调用都可见、可确认、可检索、可回读。
- 供应商字段变化不会导致主要数据丢失，后续分析可以从完整原始响应重建。
- 精选接口数量保持克制，产品界面围绕趋势和博主分析，不变成 Swagger 镜像。

### 负 / 代价

- 完整原始响应会持续占用本地磁盘；当前不自动删除，由用户掌控保留周期。
- 新增接口必须同时补固定定义、参数验证、官方文档核对和测试。
- SQLite 索引不承担跨平台统一字段抽取；标准化趋势实体属于后续分析层。

## Alternatives

| 方案 | 为何未选 |
|------|----------|
| 允许填写任意 URL 和 JSON | 扩大 SSRF、误调用和凭证外发风险，无法保证接口已核对 |
| 把完整响应直接塞进 SQLite | 放大数据库、降低可恢复性，且不利于按日归档 |
| 只保存前端摘要 | 供应商结构变化后无法重新分析，违反“主要数据都要保存” |
| 自动定时抓取 | 会产生未确认的网络与计费副作用 |

## Related

- ADR 0018：本地趋势雷达与显式快照
- ADR 0019：TikHub 激活门与固定平台目录
- ADR 0021：监控优先的趋势雷达产品结构
- Docs: `docs/TIKHUB.md`
- Code: `internal/tikhub/endpoints.go`
- Code: `internal/ui/data_api_calls_handlers.go`
- Schema: `migrations/024_tikhub_api_calls.sql`
