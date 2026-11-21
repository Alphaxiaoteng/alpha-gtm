# 0015. 账号状态采用本地 Profile 真源与显式加密会话迁移

| 字段 | 值 |
|---|---|
| Status | accepted |
| Date | 2026-07-23 |
| Area | browser / account-data |

## Context

账号登录状态需要长期保留并能由用户迁移到另一台设备。Turbo Push 的可见产品流程将账号、文章、代理等本地配置导出为 `.tp`，并把完整数据目录拷贝作为人工迁移路径。

Alpha-Nexus 的账号身份由 SQLite 账号记录、独立 Chromium Profile 和 BrowserFacade 生命周期共同组成。直接复制整个 Chromium Profile 并不可靠：macOS Cookie 通常受系统 Keychain 与设备环境保护；同时它会带上无关页面数据、Profile 路径和更多本机材料。

此前的安全边界禁止从 Turbo Push 导入 Cookie 或 Browser Profile，仍然有效；它不应阻止用户明确授权的 Alpha-Nexus 自有会话迁移。

## Decision

1. 每账号现有独立 Chromium Profile 继续是本机登录态的唯一运行时真源，应用重启不删除它。
2. SQLite 继续只保存账号身份、登录状态、最后检测时间与状态证据；不保存 Cookie 或平台密码。
3. 在人工完整登录检测和用户显式关闭浏览器时，应用按账号写入本机加密会话快照；每日覆盖，至少保留 35 天。
4. 用户可显式导出单账号 `.anxsession` 迁移包。它使用用户输入的一次性口令经 Argon2id 派生密钥、AES-GCM 加密；口令、Cookie、Profile 路径和代理不进入日志、SQLite 或普通安全快照。
5. 导入在新设备创建新的隔离 Profile 后仅写入加密包中的平台会话 Cookie。绝不把导入结果标记为 `logged_in`；浏览器保持打开，由平台实际页面确认，平台要求再次验证时必须人工完成。
6. 账号迁移不是 Turbo Push `.tp` 导入扩权，也不读取、解密或复制 Turbo Push 的 Cookie/Profile。

## Consequences

### Positive

- 本机账号状态在浏览器与控制面重启后持续存在，并有不少于 30 天的独立加密恢复点。
- 用户可迁移自己明确选择的账号，而不是复制整套本机数据目录。
- API/UI 只返回备份时间、到期时间和结果码，不暴露会话内容。

### Trade-offs

- 平台可将会话与设备、IP、风控状态绑定；导入不能承诺免登录。
- 导出需要运行中的账号浏览器，且迁移口令丢失后无法恢复该包。
- 账号 Profile、Cookie 和本机加密密钥仍属于本地敏感材料，不能被上传、打印或纳入普通导出。

## Related

- ADR 0002、0003、0008
- `internal/sessionbackup/`
- `internal/ui/account_session_handlers.go`
- L013（本决策仅增加用户明确发起的加密例外，不允许裸拷贝 Profile）
