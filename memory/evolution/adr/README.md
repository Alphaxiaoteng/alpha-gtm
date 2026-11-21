# Architecture Decision Records

单决策短文。编号：`NNNN-short-kebab-slug.md`。模板：[../templates/adr.md](../templates/adr.md)

## Catalog（现行）

| ID | Status | Title |
|----|--------|-------|
| [0001](./0001-cli-first-interface.md) | accepted | CLI 是唯一核心接口 |
| [0002](./0002-fingerprint-chromium-cdp.md) | accepted | fingerprint-chromium + 本机 CDP |
| [0003](./0003-browser-facade-not-manager.md) | accepted | BrowserFacade 为生命周期真源 |
| [0005](./0005-agentpush-via-alpha-nexus-stack.md) | accepted | AgentPush 愿景落在 Go 主线 |
| [0006](./0006-agentpush-full-scope.md) | accepted | AgentPush 全量能力面在 Go 主线落地 |
| [0007](./0007-stack-hybrid-go-control-plane.md) | accepted | 栈决策：Go 控制面，不整仓 Node 重写 |
| [0011](./0011-browser-fleet-orchestrator.md) | accepted | 本地 BrowserFacade Fleet 编排 |
| [0012](./0012-agent-operation-observability.md) | accepted | Agent 操作使用手动刷新的语义事件看板 |
| [0014](./0014-global-agent-hub-local-cli.md) | accepted | 全局 Agent Hub 使用受控本地 CLI Provider |
| [0015](./0015-encrypted-account-session-transfer.md) | accepted | 本地 Profile 真源与显式加密会话迁移 |
| [0016](./0016-local-skill-library.md) | accepted | 本地 Skill 库与审核后执行桥 |
| [0017](./0017-platform-native-publish-time-without-local-scheduler.md) | accepted | 显式发布准备填写平台原生时间，不建立本地调度器 |
| [0018](./0018-local-trend-radar-explicit-snapshots.md) | accepted | 趋势雷达采用显式采集与本地可追溯快照 |
| [0019](./0019-activated-fixed-data-api-catalog.md) | accepted | 数据 API 使用激活门与固定平台接口目录 |
| [0020](./0020-curated-tikhub-workbench-and-durable-calls.md) | accepted | 精选 TikHub 调用工作台与长期原始响应留档 |
| [0021](./0021-monitoring-first-trend-radar.md) | accepted | 趋势雷达以博主与赛道监控为主 |
| [0022](./0022-controlled-browser-submit-youtube-tiktok.md) | accepted | YouTube/TikTok 仅经冻结单目标计划受控浏览器提交 |
| [0023](./0023-alpha-agent-on-isolated-pi-runtime.md) | accepted | Alpha Agent 默认使用隔离的 Pi 运行时 |

## 归档

`proposed` / `superseded`：[`../archive/adr/`](../archive/adr/)（0004、0008、0009、0010、0013）。默认不读。

## Next ID

`0024`
