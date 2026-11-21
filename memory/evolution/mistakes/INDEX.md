# Mistakes Index

犯错 / 踩坑**索引**。最新在上。详情点进 `lessons/L0xx`。

新增条目时：1) 按 [lesson 模板](./templates/lesson.md) 写文件 2) 只更新本表；不维护第二张 Catalog。

---

## Index

| ID | Date | Area | Severity | Status | Title |
|----|------|------|----------|--------|-------|
| [L021](./lessons/L021-account-list-snapshot-overwrite.md) | 2026-09-02 | ui | high | mitigated | 账号假空状态、重复绑定误报与旧配置覆盖 |
| [L020](./lessons/L020-shadow-overlay-inline-style-blocked-by-csp.md) | 2026-07-30 | browser | medium | resolved | Shadow DOM 内联样式被创作者后台 CSP 禁用 |
| [L019](./lessons/L019-fast-login-probe-demotes-verified-session.md) | 2026-07-26 | browser | high | resolved | 快速登录探针未经复核降级已验证会话 |
| [L018](./lessons/L018-meta-alias-missing-login-contract.md) | 2026-07-26 | browser | high | resolved | 统一 Meta 账号缺少登录契约导致状态反复降级 |
| [L017](./lessons/L017-account-avatar-end-to-end-gaps.md) | 2026-07-25 | browser | high | resolved | 账号头像只测 selector 漏掉抓取与认证显示 |
| [L016](./lessons/L016-unpacked-electron-bundle-misdetected-as-development.md) | 2026-07-24 | electron | high | resolved | 未压缩 Electron 包被误判为开发壳 |
| [L015](./lessons/L015-fixed-global-controls-cover-immersive-workspace.md) | 2026-07-24 | ui | medium | resolved | 全局悬浮控件遮住沉浸式工作区操作 |
| [L014](./lessons/L014-stale-worktree-shuts-down-shared-browser-runtime.md) | 2026-07-22 | browser | high | resolved | 旧工作副本关闭共享浏览器运行态 |
| [L013](./lessons/L013-temporary-data-root-loses-account-continuity.md) | 2026-07-21 | db | high | resolved | 临时数据根目录导致账号与登录会话看似丢失 |
| [L012](./lessons/L012-editor-host-login-false-negative.md) | 2026-07-20 | browser | high | resolved | 创作编辑器域名导致登录假阴性 |
| [L011](./lessons/L011-browseros-cdp-port-mismatch.md) | 2026-07-15 | browser | high | mitigated | BrowserOS 侧边栏端口不匹配导致崩溃 |
| [L010](./lessons/L010-proxy-api-stale-cache.md) | 2026-06-25 | proxy | medium | resolved | 动态代理 API 被旧 IP 缓存挡住 |
| [L009](./lessons/L009-cli-exit-kills-supervisor-heartbeat.md) | 2026-06-01 | browser | medium | mitigated | CLI 退出后 Supervisor 心跳停止 |
| [L008](./lessons/L008-sqlite-busy-under-concurrency.md) | 2026-06-22 | db | medium | resolved | 并发写 SQLite database is locked |
| [L007](./lessons/L007-passive-login-false-positive.md) | 2026-06-20 | browser | high | resolved | 游客 Cookie 登录假阳性 / 二维码头像 |
| [L006](./lessons/L006-login-poll-navigate-hijack.md) | 2026-06-20 | browser | high | resolved | 登录轮询 Navigate 劫持页面 |
| [L005](./lessons/L005-electron-state-enum-mismatch.md) | 2026-06-15 | electron | high | resolved | UI 状态枚举与 Supervisor 不一致 |
| [L004](./lessons/L004-swr-cache-key-collision.md) | 2026-06-15 | ui | high | resolved | SWR 同 key 不同 fetcher 崩溃 |
| [L003](./lessons/L003-go-embed-no-parent-path.md) | 2026-06-01 | build | medium | resolved | go:embed 禁止父路径 |
| [L002](./lessons/L002-cdp-port-time-wait.md) | 2026-06-01 | browser | medium | resolved | CDP 端口 TIME_WAIT 假空闲 |
| [L001](./lessons/L001-browser-manager-vs-facade.md) | 2026-06-01 | browser | high | mitigated | Manager 与 Facade 双路径 |

---

## By area（速查）

| Area | IDs |
|------|-----|
| browser | L001 L002 L006 L007 L009 L011 L012 L014 L018 L019 L020 |
| ui | L004 L015 L021 |
| electron | L005 L016 |
| db | L008 L013 |
| build | L003 |
| proxy | L010 |

---

## Historical material

旧 `project_agent.md` 已原样归档到 [`../archive/`](../archive/README.md)。归档仅作考古；只有根因与修复经过验证、并对未来有可执行 Prevention 时，才升格为 lesson 并登记到本表。
