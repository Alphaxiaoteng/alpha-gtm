# L015: 全局悬浮控件遮住沉浸式工作区操作

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | ui |
| Severity | medium |
| Date | 2026-07-24 |
| Last verified | 2026-07-24 |

## Symptoms

AI 生图页面的“资产”和“保存”按钮存在于 DOM，也通过了静态测试，但真实界面中被右上角全局“日历 / Agent”悬浮按钮覆盖。点击“资产”实际打开了日历。

## Wrong path

只确认控件数量、可访问名称和事件代码，没有在完整 `Layout` 中检查沉浸式工作区的最终可点击区域。局部组件看起来布局正确，单独的 DOM 断言也无法发现跨层覆盖。

## Root cause

`Layout` 把全局操作固定在右上角高层级；沉浸式工作区的自定义头部仍占满整行，没有为这组固定控件预留水平空间。

## Verified correction

`AIImageWorkspaceView` 的头部右侧预留全局操作宽度。重新构建并在本地完整应用中截图、点击验证后，“资产 / 保存”和“日历 / Agent”均独立可见，“资产”按钮正确展开素材面板。

## Prevention

- 新增或调整沉浸式工作区头部时，必须在完整 `Layout` 中做一次真实截图和点击验收。
- 固定或悬浮的全局控件必须有明确的布局保留区，不能只依赖 `z-index`。
- DOM 存在、测试 ID 唯一不等于可点击；至少验证一次关键控件点击后的权威状态变化。

## Related

- Evidence: `ui-web/test/ai-image-workspace.test.mjs`
- Code: `ui-web/src/components/AIImageWorkspaceView.tsx`
- Code: `ui-web/src/components/Layout.tsx`
