# Alpha-Nexus creation-workflow · DSH plugin (minimal skeleton)

一个**最小骨架** DSH 插件:只暴露 **1 个工具**,把 dsh / dshbot 接到本机 Alpha-Nexus 的创作工作流 HTTP API 上。

> ⚠️ **状态**:插件代码已用真实 `@deepseek-ai/cordis` 4.0.5 + `@deepseek-ai/dsh-tools` 0.2.1-rc.5 的 `defineTool` 编译器验证,但**尚未在真实 dsh 会话里装载跑通**(本机 dsh 0.2.1-rc.5 未执行 `dsh plugin add` + 重启 web)。

只做一件事,不扩张:先证明链路通,其它接口(advance / drafts / model-roles)以后逐个加。

---

## 1. 目录

```text
plugins/alpha-nexus-creation/
├── package.json          # 零运行时依赖;dsh.bundle.patch 指向 cordis.patch.yml
├── cordis.patch.yml      # DSH 插件清单(bundle 层 insert 形式)
├── index.js              # 入口:name / inject / Config / apply(ctx, config)
├── README.md
└── test/
    ├── local.test.mjs    # node --test 用例(13 个)
    ├── stub-hooks.mjs    # 仅测试用:把 @deepseek-ai/dsh-tools 映射到桩
    └── stub-dsh-tools.mjs
```

## 2. 提供的工具

`alpha_nexus.creation_workflow_run`

| 输入 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | string | ✅ | 创作会话标题 |
| `stage` | string | ✅ | `topic` / `copy` / `script` / `review`(另有 `outline` 别名,见 §6) |
| `message` | string | ✅ | 本轮对创作助手说的话(后端限 12000 字符) |
| `trend_context` | string | ❌ | 本轮附加的趋势参考 |

行为(两步,纯 HTTP):

1. `POST {base_url}/api/creation/workflows` body `{ title, stage }` → 取 `workflow.id`
2. `POST {base_url}/api/creation/workflows/{workflow_id}/runs` body `{ stage, message, trend_context }`

返回 `{ workflow_id, run }`。**不推进阶段、不发布、不落盘**。

## 3. 配置项

schema 的真源是 `index.js` 里的 `Config`(一个 `StandardSchemaV1` 校验器);`cordis.patch.yml` 只放**值**。cordis 会在 `apply()` 之前调用 `Config['~standard'].validate(config)`,默认值也在那里补齐。

| 键 | 默认 | 说明 |
| --- | --- | --- |
| `base_url` | `http://127.0.0.1:17421` | 只允许 `http` / `https`;不允许内嵌账号密码 |
| `allow_remote` | `false` | 仅当 `base_url` 主机**不是**回环时才需要显式置 `true` |
| `auth_token` | `''`(不发) | 非空时加 `Authorization: Bearer <token>` |

`base_url` 校验规则(违反即抛错,`validate()` 返回 `issues`):

- 必须是合法 URL,协议只能是 `http` / `https`
- 主机 ∈ `127.0.0.1` / `localhost` / `::1` → 放行
- 其它主机 → 必须 `allow_remote: true`,否则拒绝(防止把本地 API 打到别人机器)
- 尾部 `/` 自动去掉;允许保留路径前缀(反代场景)

## 4. 安装到 dsh

本机 dsh 0.2.1-rc.5 的 `dsh plugin` 只是把参数转给 pnpm(`dsh plugin --help` 原文: "forwarding the remaining arguments to pnpm")。

```bash
# 1) 装进 web profile(本地路径,file: 协议)
dsh plugin --profile web add file:/Users/albert/Documents/project/App/Alpha-Nexus-egolite/plugins/alpha-nexus-creation

# 2) 重启 web 服务
dsh web
```

`package.json` 已声明 `dsh.bundle.patch = ./cordis.patch.yml`,所以 `dsh plugin add` 会把它并入 `dsh.profile.bundles` 自动挂载。

**手动挂载(二选一,别同时做)**:把 `cordis.patch.yml` 里的 `- insert:` 行粘到 `~/.dsh/profiles/web/cordis.patch.yml`。

> ⚠️ 已知坑(社区 issue):同一条目**同时**经 `dsh plugin add` 和手写 `cordis.patch.yml` 挂载,会导致 dsh web 启动崩溃 `duplicate loader entry id`。只选一种。

## 5. 在 dsh 中验证(需要 dsh 环境,本仓未能执行)

```bash
# 配置是否被正确组装(能看到 alpha-nexus-creation 这一行)
dsh web --dump-config | grep -A3 alpha-nexus-creation

# 起服务后在会话里让模型调用,预期工具名出现在工具列表:
#   alpha_nexus.creation_workflow_run
```

**前置条件**:Alpha-Nexus 必须在跑且已选定可用的文本模型,否则后端会先返回
`422 {"error":"CREATION_MODEL_UNAVAILABLE"}`(本机实测现状就是这样)。

## 6. 与原始需求的两处偏差(有意为之,基于源码证据)

| 项 | 原需求 | 实际实现 | 依据 |
| --- | --- | --- | --- |
| 默认端口 | `17321` | **`17421`** | `internal/config/config.go:13` `DefaultUIPort = 17421`;本机 `lsof` 实测 `127.0.0.1:17421 (LISTEN)`。`17321` 只是 `--port` 覆盖示例 |
| 阶段枚举 | `topic\|outline\|script\|review` | **`topic\|copy\|script\|review`**,并接受 `outline` 作为 `copy` 的别名 | `internal/ui/creation_workflow_handlers.go` 的 `validCreationWorkflowStageUI()` 只认 `topic/copy/script/review`,后端没有 `outline`;直接发 `outline` 会被拒 |

别名实现:`resolveStage()` 把 `outline` 映射成 `copy` **后再发请求**,所以创建与首轮 run 用的是同一个真实阶段,不会触发后端的 "workflow stage changed"。

## 7. 本地验证(不需要 dsh)

```bash
node --check index.js
node --test test/local.test.mjs

# 用真实 defineTool 编译器跑同一套用例(需要本机装了 dsh)
DSH_TOOLS_PATH=/Users/albert/.local/opt/node-v24.15.0-darwin-arm64/lib/node_modules/@deepseek-ai/dsh/node_modules/@deepseek-ai/dsh-tools/lib/index.js \
  node --test test/local.test.mjs
```

测试覆盖:工具注册、两次请求的 URL / method / header / body、`outline→copy`、Bearer 只在配置时发送、workflow id URI 编码、后端错误码透出、base_url 回环/远程/协议/凭据校验、`Config` 同步校验契约。

## 8. API 形状:哪些查证过,哪些是推断

**已查证(读真实源码 / 真实模块 / 真实 HTTP)**

| 结论 | 证据 |
| --- | --- |
| 插件入口是 `(ctx, config)` 函数 + 静态 `name` / `inject` / `Config` | `@deepseek-ai/cordis@4.0.5` `lib/types/registry.d.ts` → `Plugin.Function` |
| `Config` 是 **StandardSchemaV1**,cordis 同步调用 `Config["~standard"].validate(config)` 并用返回的 `value` | `cordis/lib/index.js` `resolveConfig()` |
| 工具必须用 `ctx.tools.register(defineTool({...}))`,**不存在 `ctx.tool()`** | `dsh-tools` `index.d.ts`;`Plugin` 里没有 tool 相关 API |
| `defineTool` 要求 `output.schema`(对象必须显式 `additionalProperties`) | 真实编译器实测:不写会抛 `JsonSchemaError` |
| `ctx.effect(fn, label)` 立即执行并收集返回的 disposer | `cordis/lib/types/fiber.d.ts` |
| 工具名只保留 `run_code`,`alpha_nexus.creation_workflow_run` 合法 | `dsh-tools/lib/index.js` 名称保留检查 |
| `cordis.patch.yml` 顶层是**列表**;bundle 层用 `- insert:`,profile 用户层用裸 `- id:` | 本机 `~/.dsh/profiles/web/node_modules/ds-bot/cordis.patch.yml` 等 4 个真实文件 |
| `package.json` 的 `dsh.bundle.patch` 指向 patch 文件 | 真实已发布插件 `ds-bot` 的 `package.json` |
| 创建/运行接口路径、body 字段、201/200 语义 | `internal/ui/creation_workflow_handlers.go` + `server.go:984-989` |
| 后端鉴权是 **loopback-only**,不读 `Authorization` | `internal/ui/terminal_handlers.go:38` `requireLocalTerminal` → `isLoopbackRequest`(只看 RemoteAddr) |
| 端点真实存在 | 本机 `curl GET /api/creation/workflows` → `200`;`POST .../does-not-exist/runs` → `404 CREATION_NOT_FOUND` |

**未查证 / 推断**

- **插件被 dsh 真实装载**未验证:本机没执行 `dsh plugin add` + 重启,属 UNVERIFIED。
- 本机 Alpha-Nexus 当前返回 `422 CREATION_MODEL_UNAVAILABLE`(未选定可用文本模型),**端到端真实创作链路 UNVERIFIED**。
- `auth_token` 目前对 Alpha-Nexus 后端**无效**:后端不读该头。保留是为了①满足配置契约 ②将来后端加 token 时不用改插件。
- `insert` 与 `dsh plugin add` 的自动 reconcile 行为来自社区文档/issue,未在本机复现。

## 9. 后续扩展

加新接口的顺序:**先改 Alpha-Nexus 的 `internal/ui/creation_workflow_handlers.go`,再在本插件加对应分支**。保持"一个工具一件事",不要在插件里堆 REST 客户端。
