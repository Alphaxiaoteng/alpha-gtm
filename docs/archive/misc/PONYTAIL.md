# Ponytail Rules

> **状态：扩展参考，待去重。** 根 `AGENTS.md` 拥有项目硬约束；本文暂时只承载尚未迁移的触发词、执行模式、输出契约和 `ponytail: deferred` 协议，不得覆盖根规则。

## 结论

`ponytail` 是项目级 AI 执行规则，用来保持最小实现、少依赖、少抽象、少 token。它不是产品功能，不进入运行时代码。

默认强度：

```text
full
```

## 触发词

出现以下词时，后续回答和实现必须启用 Ponytail：

```text
ponytail
be lazy
lazy mode
simplest solution
minimal solution
yagni
YAGNI
do less
shortest path
over-engineering
bloat
boilerplate
unnecessary dependencies
```

## 模式

### ponytail

默认最小可用方案。先质疑功能是否需要存在，再用最少代码完成。

### ponytail-audit

全项目过度工程审计。只报告，不修改。输出按优先级列出：删什么、简化什么、用标准库替代什么。

### ponytail-debt

收集代码和文档里的 `ponytail:` 注释，形成债务清单。只报告，不修改。

### ponytail-gain

展示 Ponytail 的节省效果说明。只做简短 scoreboard，不做项目级精确统计。

### ponytail-help

输出 Ponytail 命令速查卡。

### ponytail-review

代码评审模式，只找过度工程：多余依赖、过早抽象、死灵活性、重复造轮子。

## 优先级

按顺序选择方案：

1. 不做这个功能。
2. 用现有代码。
3. 用 Go 标准库。
4. 写一个本地小函数。
5. 使用已有项目依赖。
6. 新增依赖。

新增依赖必须说明为什么标准库不够。

## 项目硬边界

Ponytail 不能砍掉：

- fingerprint-chromium。
- CDP `127.0.0.1:15101`。
- SQLite。
- 批量化。
- 账号迁移。
- TDD/BDD。
- Instagram / Facebook 自动发布主线。
- Lite 平台只做账号管理的边界。

Ponytail 可以砍掉：

- 不必要的 HTTP service。
- 过早的插件系统。
- 多余 interface。
- 不必要依赖。
- 未来平台的发布抽象。
- 没有 V1 验收场景支撑的功能。
- 大型测试框架。

## 输出格式

Ponytail 输出必须短：

```text
结论：删/简化/保留什么。
理由：一句话。
动作：最多 3 条。
```

`ponytail-audit` 和 `ponytail-review` 输出一行一个 finding：

```text
[P1] 删除/简化目标 - 理由 - 文件或模块
```

## 债务标记

刻意推迟的复杂度必须写成：

```text
ponytail: deferred <reason>
```

不要用含糊的 `TODO later`。
