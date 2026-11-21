# EgoLite 浏览器运行时

Alpha-Nexus EgoLite 产品执行链只有一个浏览器运行时：EgoLite。

## 运行模型

- 应用不启动浏览器进程，不分配 CDP 端口，也不管理 profile 目录。
- `ego-browser nodejs` 连接用户已运行的 EgoLite。
- 一个“浏览器1/2/3”对应一个持久 EgoLite Space。
- Space 是同一 EgoLite 进程内隔离的 BrowserContext，独立保存 cookies/storage。
- 同一 Space 内的平台共享该浏览器身份；同平台第二个账号必须使用另一个 Space。
- EgoLite Default 不属于 Alpha-Nexus，应用不得打开、重命名、接管或修改它。

## 能力握手

启动前必须通过：

```bash
alpha-nexus-egolite ego doctor
```

运行时至少需要 `listTaskSpaces`、`useOrCreateTaskSpace`、`snapshotText`、
`uploadFile`、`handOffTaskSpace` 和 `completeTaskSpace`。缺失能力时 fail closed。

## 账号与控制权

1. Electron 创建逻辑浏览器并配置平台。
2. 应用创建或复用该浏览器唯一 Space。
3. 一批最多打开 3 个平台页面，然后归还控制权让用户扫码、输密码或授权。
4. 核验时只读取公开页面 DOM、平台名称、账号 ID 和头像；完成后再次归还控制权。
5. 登录核验会话最长 15 分钟，成功、显式取消或过期后停止。

用户接管期间不得运行发布或数据采集。验证码、2FA、checkpoint 和风控不得自动绕过。

## 并发与资源

- 应用启动时创建 0 个 Space。
- 同一 Space 同时只能有一个任务。
- 登录页一批最多 3 个标签。
- 不同 Space 的上限由本机校准决定；默认不超过 3。
- 后台掉线检查串行执行，空闲时不长期保留 Agent 控制。

## 兼容代码

仓内 `internal/browser`、`internal/fleet`、`internal/logincheck`、
`internal/sessionbackup` 和 chromedp 依赖属于未注册兼容代码。它们不得被新的 UI、CLI、
Registry 或发布入口重新接入。Playwright 只允许用于 Alpha-Nexus 自身 UI E2E。

## 禁止事项

- 不读取、导出或记录 Cookie、平台密码和 EgoLite 用户目录。
- 不直接修改 EgoLite Profile、书签数据库或 Default。
- 不用 Chrome、Chromium、BrowserOS、Playwright 替代产品运行时。
- 不把 `unknown` 当成掉线或成功。
- 不在缺少冻结批次和当次审批时点击最终发布。
