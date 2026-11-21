# Evolution Protocol

本协议是 Alpha-Nexus 的通用、可审计自进化框架。它把“经验”限制为可验证的项目资料：**捕获 → 验证 → 晋升 → 过期/归档**。它不是运行时学习、自动 Prompt 改写、后台循环或自修改代码系统。

## 加载与边界

每次任务只按以下顺序渐进读取：

```text
用户 Goal + AGENTS.md
→ PROJECT_AGENT.md
→ CURRENT.md（匹配 area）
→ mistakes/INDEX.md（匹配 area）
→ 单个 lesson 或 accepted ADR
```

`archive/` 默认不读。源码、用户当前指令和本轮真实测试高于任何记忆。所有记录必须脱敏：不得写密钥、Cookie、账号数据、完整请求/响应、运行时绝对路径或原始输出。

## 文件职责

| 内容 | 唯一位置 | 进入条件 |
| --- | --- | --- |
| 当前且跨任务复用的事实 | `evolution/CURRENT.md` | 有源码路径、测试或 accepted ADR 证据 |
| 已修复的非显然错误 | `evolution/mistakes/lessons/L0NN-*.md` | 根因与 Prevention 已验证 |
| 错误检索入口 | `evolution/mistakes/INDEX.md` | 每个 lesson 一行，最新在上 |
| 架构取舍 | `evolution/adr/NNNN-*.md` | 需要选择或明确否决方案 |
| 已退出默认上下文的资料 | `evolution/archive/` | 只读，不恢复为现行事实 |

不另建错误日志、学习清单、知识库、数据库、队列或自动同步层；同一事实只能有一个当前来源。

## 捕获与验证门禁

| 触发 | 先做什么 | 验证后写入 |
| --- | --- | --- |
| 用户纠正、任务失败、外部接口异常 | 保留最短脱敏事实，先定位根因 | lesson；必要时更新索引 |
| 发现可复用实现事实 | 以源码或真实测试确认 | `CURRENT.md` 原条目 |
| 选择/否决技术路径 | 记录备选、后果和证据 | ADR |
| 只是一时网络波动、typo、猜测 | 修复/重试即可 | 不记录 |

每条记录必须包含：发生条件、根因或事实、最小 Prevention、证据、验证日期。没有证据就标为“未验证”并留在任务对话中，不能写入记忆。

## 晋升、收敛与过期

1. 新 lesson 先进入 `mistakes/lessons/`，索引只新增一行；不要复制到多份清单。
2. 同类错误至少两次，或一次就有高数据/账号风险，才可压缩为 `AGENTS.md` 的一条硬约束；必须反链原 lesson。
3. 当前事实变更时改原行并更新 `Last verified`；失效项标 `superseded` 或移入 archive，不追加近义行。
4. `CURRENT.md` 目标不超过 50 条或 200 行；超限先合并、删除失效项，再新增。
5. 单个 lesson 目标不超过 80 行，ADR 目标不超过 60 行；细节放源码、测试或按需文档。

## 明确不做

- 不安装自进化 CLI、npm 包、hooks、守护进程或 `--loop`。
- 不扫描/上传对话、凭证、运行日志或用户数据。
- 不让 Agent 静默执行、静默发布、静默登录或替代用户确认。
- 不把“记忆”当成代码正确性、平台验收或真实发布成功的证据。

任务后：可复用事实写 `CURRENT.md`；非显然已修错误写 lesson；架构取舍写 ADR。旧 PLAYBOOK 见 [`evolution/archive/PLAYBOOK.md`](./evolution/archive/PLAYBOOK.md)。错误入口见 [`evolution/mistakes/INDEX.md`](./evolution/mistakes/INDEX.md)。
