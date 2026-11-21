# ADR 0016: 本地 Skill 库与审核后执行桥

- Status: accepted
- Date: 2026-07-24
- Deciders: Alpha-Nexus maintainers

## Context

Alpha-Nexus 已同时存在项目级说明 Skill 与 `tools.Registry` 中经过审核的社媒工作流，但用户无法在应用内发现、启停、导入或通过 Agent 创建自己的 Skill。允许模型生成后直接执行任意 Playwright、CDP 或发布脚本会绕过 BrowserFacade、账号隔离、单次审批与成功证据契约。

## Decision

建立一个本地 Skill 库：

- SQLite `skills` 表是目录、来源、审核状态和启停状态的真源。
- 每个 Skill 的标准 `SKILL.md` 保存在私有数据目录 `skills/<id>/`，不上传云端。
- Agent 仅在用户显式输入 `/skill-id` 或 `@skill-id` 时注入已启用 Skill 的指令正文。
- 指令注入不授予工具、凭据、浏览器、账号或发布权限。
- 指令型本地 Skill 可由用户启用；声明浏览器、Playwright 或固定脚本的自动化 Skill 进入 `review_required`，审核前不能启用。
- 审核上线的自动化能力继续绑定到 `tools.Registry`、BrowserFacade 和既有审批/证据契约；不从本地 Skill 目录直接执行任意脚本。

## Alternatives considered

1. 继续使用静态内置目录：最简单，但不能满足用户本地创建、导入和启停。
2. 直接执行用户或模型生成的本地脚本：体验直接，但权限边界不可控，拒绝。
3. 建设云端 Skill 市场：不符合本地单用户产品边界，也增加账号、审核和分发基础设施，拒绝。

## Consequences

- 用户可在本机创建、导入、启停并通过 Agent 调用指令 Skill。
- Agent 对话可使用 Skill Creator 生成标准 `SKILL.md`，再显式添加到本地库。
- 自动化 Skill 上线需要开发审核和固定 Registry 实现，流程更慢但权限与证据边界可验证。
- 当前不提供云同步、市场分发、任意脚本执行或最终发布授权。

## Success criteria

- 本地 Skill 重启后仍可发现，文件权限为私有。
- 禁用 Skill 不会注入 Agent Prompt。
- `review_required` Skill 无法启用。
- Agent 保存的用户消息保持原文，Skill 指令只发送给当前 Provider Run。
