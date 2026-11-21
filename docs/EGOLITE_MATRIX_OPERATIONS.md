# EgoLite 矩阵通用操作

Electron 是唯一管理界面。EgoLite 仅打开平台创作者页面；禁止在 EgoLite 中打开 Alpha-Nexus 前端，禁止修改用户的 Default、Cookie、密码或 Profile 文件。

## 启动与停止

```bash
./scripts/start-electron-dev.sh
./scripts/stop-electron-dev.sh
```

启动脚本会复用本项目已有 Electron 进程，以脱离调用终端的方式运行，并等待本地 sidecar 健康检查成功。停止脚本只终止 PID 与命令行都能确认属于当前目录的 Electron；不会终止旧 Alpha-Nexus、其他 Chromium 或 EgoLite Default。

Electron 启动不会创建 Space。运行日志与 PID 在 `.alpha-nexus-egolite-run/`，应用数据在 `~/.alpha-nexus-egolite/`。

## 账号接入

1. 在 Electron 的“浏览器管理”中创建浏览器。留空时依次生成“浏览器1、浏览器2……”，内部仍以逻辑身份和 Space 隔离。
2. 给浏览器选择平台。每个浏览器同一平台最多一个账号，平台可随时增删。
3. 点击平台“登录”或“登录未完成平台”。系统创建或复用该浏览器唯一 Space，并按国内视频优先顺序最多打开 3 个创作者后台。
4. 用户在 EgoLite 中自行扫码、输入验证码或完成 2FA。
5. 登录页打开后，Electron 在 15 分钟有界会话内逐平台快速核验；任一平台成功后立即更新登录状态、平台用户名、账号 ID 和头像，不等待整批完成。也可点击“本批登录完成”立即触发一次核验。
6. 关闭二维码弹窗只归还 EgoLite 控制权，不取消这段核验会话；二维码过期时会替换会话并重新同步，避免回退到 30/60 分钟后台检查。
7. 头像只接受去除 query/fragment 的公开 HTTPS 地址；本地地址、data URL、带凭证 URL 均拒绝保存。已有受管头像不会被远端回退地址覆盖。
8. 平台账号名称优先采用已登录创作者后台的账号昵称；例如 CSDN 的公开博客标题与创作中心昵称不一致时，不得用公开标题覆盖创作中心昵称。

固定平台顺序：

抖音、视频号、快手、小红书、TikTok、YouTube、Instagram、Facebook、微信公众号、CSDN。

## 收藏夹

“导入 EgoLite 收藏夹”生成标准 Netscape Bookmark HTML，并尝试通过受控 macOS UI 新增 `Alpha-Nexus 平台` 文件夹。操作幂等；缺少辅助功能权限或菜单无法可靠定位时返回人工导入文件，不直接修改 EgoLite Profile。

## 登录状态监控

- 默认开启，每 60 分钟检查，可在 Electron 中设置 1–1440 分钟或关闭。
- 每次只检查已有、由 Agent 持有的 Space；不会因为监控创建 Space，也不会抢占用户正在操作的 Space。
- 固定并发为 1，逐矩阵、逐平台检查，检查产生的临时标签会关闭。
- 只有明确登录页证据才记为 `logged_out`；`unknown` 保留原状态。
- 验证码、扫码、2FA 或风控记为 `manual_takeover`，通知一次并等待用户处理。
- 登录恢复时同步平台用户名、账号 ID 和安全头像引用。

## 结构化能力

| 操作 | Registry 工具 | HTTP |
| --- | --- | --- |
| 创建矩阵 | `ego.matrix.create` | `POST /api/ego/identities` |
| 配置平台 | `ego.matrix.configure-platforms` | `PUT /api/ego/identities/{id}/platforms` |
| 导入收藏夹 | `ego.bookmarks.import` | `POST /api/ego/bookmarks/import` |
| 打开登录批次 | `ego.login.batch` | `POST /api/ego/identities/{id}/login-batches` |
| 登录核验 | `ego.login.verify` | `POST /api/ego/identities/{id}/login-verify` |
| 状态检查 | `ego.session.check` | `POST /api/ego/session-monitor/run` |

Agent 只能调用这些结构化能力。禁止任意 Shell、Cookie 读取、浏览器 profile 读取和未经冻结批次确认的最终发布。

## 资源边界

- 启动活跃 Space：0。
- 活跃 Space 上限：3。
- 单次登录批次标签上限：3。
- 后台检查并发：1。
- 同一矩阵只有一个 Space；多平台共享该 Space。
- 不运行 `~/.alpha-nexus-egolite/profiles` 下的 Chromium，不分配 CDP 端口。
