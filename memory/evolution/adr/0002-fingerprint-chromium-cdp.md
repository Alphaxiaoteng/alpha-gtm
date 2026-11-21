# 0002. 主线使用 fingerprint-chromium + 本机 CDP

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-05-01 |
| Area | browser |

## Context

多账号社媒自动化需要指纹隔离与可调试的浏览器控制。可选 Camoufox/Firefox、Playwright 托管浏览器、官方 Graph API 等。

## Decision

- 主浏览器：**fingerprint-chromium**  
- 控制面：本机 CDP `127.0.0.1:15101–15199` + **chromedp**  
- 不默认 headless；不暴露 CDP 到公网  
- 一账号一 `user-data-dir` + 固定 fingerprint seed  

## Consequences

### 正

- 与 chromedp 生态一致  
- 账号物理隔离清晰  

### 负 / 代价

- 非 Windows 优先（syscall flock 等）  
- 不能把 Camoufox 当 V1 主线  

## Alternatives

| 方案 | 为何未选 |
|------|----------|
| Chrome/Edge 官方 | 指纹弱、与项目约束冲突 |
| Camoufox 主线 | Firefox/Juggler，偏离 CDP 主线 |
| Graph API 发帖 | V1 明确不做 |

## Related

- ADR: 0003  
- Docs: `docs/MULTI_BROWSER_ACCOUNT_FRAMEWORK.md`
