# 0011. 本地 BrowserFacade Fleet 编排

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-07-17 |
| Area | browser, fleet |

## 决定

- Fleet 只编排本地 `BrowserFacade` 的 `start`、`stop`、`observe`；复用 `batch_runs` 与 `batch_items` 持久化目标和结果。
- `observe` 不导航、不登录、不填表、不发布；未运行或不确定时固定转 `waiting_human / manual_takeover`。
- 不同账号最多并发 5；同一 profile 通过 `browser_operation_leases` 串行化，禁止并发 Agent/CDP 动作。
- CDP 端口仍限 `15101–15199`。启动前以 `cdp_port_leases` 作短 TTL SQLite 原子预约，runtime 行持久化后才允许释放预约。
- 看板只读 Fleet run/item 状态，绝不返回 CDP URL、profile 路径、Cookie 或凭证。写操作走 Registry `fleet.run`，并要求 `confirm=true`。

## 后果

可批量启动、停止、人工检查多账号浏览器，并保留可审计状态；不承诺平台免封，也不提供 CAPTCHA/2FA 绕过或自动发布。
