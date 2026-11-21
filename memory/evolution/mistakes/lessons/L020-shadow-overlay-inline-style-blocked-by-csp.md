# L020: Shadow DOM 内联样式被创作者后台 CSP 禁用

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | medium |
| Date | 2026-07-30 |
| Last verified | 2026-07-30 |

## Symptoms

浏览器 Agent Overlay 的节点已注入页面，但控制条退化为普通文档流元素，按钮无法接收点击。无 CSP 的本地页面测试正常，严格 `style-src 'none'` 页面失败。

## Root cause

Shadow DOM 隔离 CSS 作用域，但不会绕过宿主页面的 Content Security Policy。向 Shadow Root 写入普通 `<style>` 仍会被严格 `style-src` 拦截。

## Verified correction

- 保留 Shadow DOM 作为结构和事件隔离边界。
- 用 Chromium 原生 `CSSStyleSheet.replaceSync` 和 `shadowRoot.adoptedStyleSheets` 安装样式，不关闭或绕过页面 CSP。
- 增加 `style-src 'none'` Chromium 测试，验证控制条保持 `position: fixed`，接管按钮保持 `pointer-events: auto`。

## Prevention

- 注入真实创作者后台的视觉层必须用严格 CSP fixture 测试，不能只测 `page.setContent` 的默认策略。
- Shadow DOM 只解决样式作用域冲突；CSP、跨文档导航和生命周期清理必须分别验证。
- 不使用 `page.setBypassCSP(true)` 修复装饰性界面。

## Related

- Code: `internal/publishworker/agent_overlay.cjs`
- Evidence: `internal/publishworker/agent_overlay_test.cjs`
