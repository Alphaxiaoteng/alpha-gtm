# Design QA — Shop workspace data isolation

## Scope

- Shop 经营总览使用真实本地数据，不展示伪造 GMV、订单或趋势。
- Shop 内容库与知识库复用 Media 的交互模式，但使用独立 SQLite 表和 API。
- Media / Shop 侧栏移除运行监控；运行日志收拢到设置页。
- 浏览器身份仍按 Space 共享，账号继续按 `platform_code` 区分。

## Automated evidence

- Shop 内容创建、知识库创建、Markdown 导入与搜索的 HTTP 闭环通过。
- Media 与 Shop 表隔离测试通过，两个域可独立从 ID 1 开始。
- 前端契约测试 204/204 通过。
- Go 全量测试通过。
- Vite 生产构建通过。
- 本地服务监听 `127.0.0.1:17421`，健康检查与首页 HTTP 200 通过。

## Visual verification

Codex 内置浏览器拒绝对当前本地 URL 执行自动化操作，因此本轮未取得可信的新截图、元素检查或控制台证据。没有使用 EgoLite、CDP、Chrome 或额外浏览器 Space 绕过该限制。

视觉状态：`UNVERIFIED`。用户可在已经打开的 `http://127.0.0.1:17421/` 页面直接检查。

## Result

- 数据隔离、接口闭环、构建与服务状态：`VERIFIED`
- 实际页面视觉回归：`UNVERIFIED`
