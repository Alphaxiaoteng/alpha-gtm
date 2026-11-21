# L021: 账号加载失败与旧配置覆盖造成“记录消失”

| 字段 | 值 |
|------|-----|
| Status | mitigated |
| Area | ui |
| Severity | high |
| Date | 2026-09-02 |
| Last verified | 2026-09-02 |

## Symptoms / Root cause

实际账号入口是 `BrowserWorkspaceView`，不是兼容页 `AccountsView`。请求未完成或失败时回退空数组会显示“还没有账号”；重复绑定被排除于登录计数，使已登录浏览器显示 0/1。配置轮询会重置未保存选择，而保存是全量替换，旧快照可能关闭新绑定或其他工作域的平台。

## Correction / Prevention

- 分别渲染未加载、失败、旧数据和真实空集合；不完整或失败时禁止修改配置。未知认证不隐藏已有公开资料，也不等同掉线。
- 重复绑定与认证状态分开显示，发布去重保护仍保留。
- 打开配置时冻结全部启用平台；保存保留非当前工作域及未展示的平台。事务内核对 `expected_platform_codes`，冲突返回 409，不自动重试覆盖；HTTP 缺失/null 配置拒绝，只有显式数组允许修改。
- 测试实际挂载入口，覆盖失败/旧数据渲染、共享浏览器跨域配置、陈旧快照及并发写入。此修复不保证第三方平台永不过期，不恢复真实登录态。

## Evidence

- `ui-web/test/browser-workspace-state.test.mjs`
- `internal/db/ego_platforms_conflict_test.go`
- `internal/ui/ego_platforms_conflict_test.go`
