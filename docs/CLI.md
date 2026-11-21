# Alpha-Nexus EgoLite CLI

CLI、本地 HTTP 管理界面和 Tool Agent 共享同一套 Go 领域服务与 SQLite 真源。
产品浏览器操作只通过 EgoLite Space。

```bash
alpha-nexus-egolite --data-dir ~/.alpha-nexus-egolite <command>
```

默认数据目录是 `~/.alpha-nexus-egolite`，默认 UI 端口是 `17421`。
管理界面直接用浏览器打开该端口，不需要 sidecar descriptor。

## 初始化与诊断

```bash
alpha-nexus-egolite init --data-dir ~/.alpha-nexus-egolite
alpha-nexus-egolite version
alpha-nexus-egolite ego doctor
```

`ego doctor` 只做 EgoLite helper 能力握手；失败时浏览器操作 fail closed。

## 浏览器身份与 Space

```bash
alpha-nexus-egolite ego identities
alpha-nexus-egolite ego spaces
alpha-nexus-egolite ego open --identity-id 1
alpha-nexus-egolite ego calibrate
```

- `ego open` 每次最多打开该浏览器的 3 个已配置平台页面。
- `ego calibrate` 只用于测量本机 Space 并发能力；同一 Space 始终串行。
- CLI 不暴露内部 profile 选择，也不提供 Default 操作。

## 登录与资料核验

```bash
alpha-nexus-egolite ego login-check --account-id 9
alpha-nexus-egolite ego verify --identity-id 1 --platform wechat_mp
alpha-nexus-egolite ego verify --identity-id 1 --platform douyin,csdn
```

- `login-check` 打开或复用目标平台页，只报告二维码是否可用，不输出二维码字节。
- `verify` 每次最多核验 3 个平台，保存可靠的平台名称、账号 ID、头像和登录状态。
- 验证码、扫码、密码、2FA 和授权由用户在 EgoLite 完成。
- 命令不得打印 Cookie、密码、完整敏感 URL 或 EgoLite 本地路径。

日常登录建议使用管理界面：它会保存 15 分钟有界核验会话，并在每个平台成功后立即刷新。

## 本地知识库

```bash
alpha-nexus-egolite knowledge import-dir \
  --base "Alpha-Notes 编导库" \
  --path "/absolute/local/path"

alpha-nexus-egolite knowledge search \
  --base "Alpha-Notes 编导库" \
  --query "短视频开头钩子和分镜脚本" \
  --top-k 6
```

导入支持的文件类型由 `internal/knowledge` 真源决定。搜索结果包含可追溯 chunk 引用，
不会调用远端知识库。

## Tool Registry

```bash
alpha-nexus-egolite tools list
alpha-nexus-egolite tools list --category ego
alpha-nexus-egolite tools schema
alpha-nexus-egolite tools call ego.space.list
alpha-nexus-egolite tools call knowledge.search \
  --args '{"query":"编导 SOP","top_k":6}'
# Bot 花名册（只读；创建/修改仍是 UI 的受审核动作）
alpha-nexus-egolite tools call agent.bot.list --args '{"limit":20}'
```

Agent 与工作流必须通过 Registry 调用业务能力，不得使用任意 Shell 拼装浏览器动作。

`agent.bot.list` 返回 `{bots:[{id,name,role,model,avatar}],count}`。一次性 CLI 运行时没有长驻数据库连接，会按 `--data-dir` 即时打开。

## 冻结批次发布

```bash
alpha-nexus-egolite publish batch \
  --action prepare \
  --plan-id <id> \
  --snapshot-hash <hash> \
  --approval-id <single-use-id> \
  --nonce <single-use-nonce>

alpha-nexus-egolite publish batch \
  --action submit \
  --plan-id <id> \
  --snapshot-hash <hash> \
  --approval-id <submit-bound-id> \
  --nonce <submit-bound-nonce>
```

约束：

- `prepare` 只能导航、上传、填写和回读，禁止最终点击。
- `submit` 必须匹配冻结快照与单次、action-bound 审批。
- 每个目标最多一次最终点击。
- `result_unknown` 禁止重发，只能只读核验创作者后台。
- 没有 Published/Public 或可靠排期证据时不得写成功记录。

当前只有抖音与 CSDN 具备自动 prepare；其余平台保持人工接管。Manifest 中的
10 平台/16 形态不等于真实平台已验收。

## 全平台只读数据

内容平台创作者数据通过 Registry 暴露：

```bash
alpha-nexus-egolite tools call social.metrics.collect \
  --args '{"account_id":1}'

alpha-nexus-egolite tools call social.metrics.collect-batch \
  --args '{"account_ids":[1,2,3,4,5,6,15,9,10]}'

alpha-nexus-egolite tools call social.metrics.overview \
  --args '{}'
```

支持抖音、小红书、视频号、快手、TikTok、YouTube、Meta、微信公众号和 CSDN；只处理已登录且绑定 Space 的账号。只有
`range_verified=true` 才会把 7 天快照写入 SQLite。采集必须由用户显式触发。

## 管理界面

```bash
./alpha-nexus-egolite ui serve --port 17421
```

启动后在浏览器打开 http://127.0.0.1:17421。界面由 Go 服务直接提供静态资源和 API，
不含 Electron 桌面壳；停止即结束该进程。

## 测试

```bash
go test ./...
node --test ui-web/test/*.test.mjs
./scripts/e2e-run.sh
```

不要用旧 Chromium 发布截图、旧 `~/.alpha-nexus` 数据或 mock Runner 结果声明真实平台成功。
