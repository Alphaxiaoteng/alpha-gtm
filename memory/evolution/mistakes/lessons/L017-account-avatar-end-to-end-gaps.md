# L017: 账号头像只测 selector 漏掉抓取与认证显示

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | high |
| Date | 2026-07-25 |
| Last verified | 2026-07-25 |

## Symptoms

抖音和 Meta 均显示平台图标：抖音已登录但新页面的顶部头像偶发无法更新；Meta 检测为已登录却记录 `AVATAR_PERSIST_FAILED`。开启应用认证后，已经落库的头像还可能因图片请求 401 而无法显示。

## Wrong path

只验证 selector 能返回身份元素，或只检查账号 API 里存在头像 URL。这样会漏掉 CSS 背景头像截图阻塞、Meta 账号切换器选错图片、截图把平台角标叠进头像、单账号被双 Meta 卡忽略，以及原生 `<img>` 不携带 Bearer 等真实失败。

## Root cause

头像链路跨越 DOM 证据、CDP 抓取、PNG 校验、本地端点和 React 渲染。旧测试停在链路中间：抖音顶部头像是 CSS 背景资源，元素截图可能长期不返回；Meta Business Suite 同一区域同时包含 Facebook 与 Instagram 图片，宽泛选择器会抓到错误元素；浏览器原生图片请求不会复用 API fetch 的认证头。

## Verified correction

- `internal/logincheck/profiles.go` 为抖音顶部头像和 Meta 账号切换器登记窄选择器，并区分 Facebook / Instagram 徽标；Meta 头像从已审核 CDN 原图读取，避免元素截图把平台角标合成进去。
- `internal/logincheck/passive.go` 对审核过的抖音 CDN 域名使用无 Cookie 的受限 CDP 资源读取，再统一转为 PNG；普通头像仍走元素截图。
- `ui-web/src/components/AccountAvatar.tsx` 通过带认证头的 Blob 请求加载本地头像，失败回退平台图标。
- `ui-web/src/components/AccountsView.tsx` 保持 Meta / Facebook / Instagram 为一个合并登录卡片，历史拆分记录只通过显式关联组归并；头像选择圆点移到外沿，避免遮住头像左上区域。
- 真实重启抖音账号浏览器后，抖音与 Meta 的观察结果均为 `observed`、SHA-256 长度 64、无头像错误；实际卡片加载 Blob 图片成功。

## Prevention

- 头像测试必须覆盖 `选择元素 → 获取像素 → PNG 校验 → 本地头像端点 → 认证前端渲染`，不能只断言 selector。
- CSS 背景头像只允许从已审核 HTTPS CDN 后缀读取，不携带 Cookie，不持久化远程 URL。
- 同一 DOM 区域有多平台头像时，必须用平台徽标或等价强证据配对，禁止使用全页最近图片。
- 抖音头像改动完成前必须做一次真实浏览器重启回归。

## Related

- Evidence: `internal/logincheck/observation_test.go`
- Evidence: `ui-web/test/account-observer-ui.test.mjs`
- Code: `internal/logincheck/passive.go`
- Code: `ui-web/src/components/AccountAvatar.tsx`
