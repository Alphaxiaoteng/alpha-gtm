# L004: SWR 同 key 不同 fetcher 导致崩溃

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | ui |
| Severity | high |
| Date | 2026-06-15 |

## Symptoms

`TypeError: instances.map is not a function` / `browserTabs.filter is not a function`，页面间歇崩溃。

## Root cause

`Layout` 与 `BrowserTabsView` 共用 SWR key `api/browser/tabs`，但 fetcher 一个返回 `{instances:[]}`、一个返回 `[]`，缓存互相覆盖。

## Fix

- 统一 fetcher 形状（约定返回数组）
- 消费侧防御：`Array.isArray(data) ? data : (data?.instances || [])`

## Prevention

- 同一 SWR key = 同一数据结构
- 新增 API hook 时先查 `api.ts` 是否已有同路径

## Related

- Code: `ui-web/src/api.ts`, `Layout.tsx`（历史 BrowserTabsView）
