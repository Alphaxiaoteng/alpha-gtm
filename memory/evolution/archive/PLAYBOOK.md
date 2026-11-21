# Evolution Playbook

每个 task / PR / Agent 会话按此闭环。目标：**可重复、可审查、少仪式**。

---

## 任务前（≤ 3 min）

```text
[ ] 按 AGENTS.md 完成入口读序
[ ] 进入 evolution 后按 area 读 CURRENT.md 与 mistakes/INDEX.md
[ ] 若有命中 → 打开对应 lesson / ADR，记下 Prevention
[ ] 确认 V1 范围（docs/PLAN.md 非目标）
```

### Area 快查

| 改动 | area 关键词 |
|------|-------------|
| 浏览器起停/端口/租约 | `browser` |
| React / API / SWR | `ui` |
| SQLite / migrate | `db` |
| Cobra 命令 | `cli` |
| Electron 壳 | `electron` |
| Meta 发帖 | `publish` / `meta` |
| 代理 IP | `proxy` |
| build.sh / embed | `build` |

---

## 任务中

```text
BDD/场景 → TDD 红灯 → 最小实现 → 绿灯 → Ponytail 检查
```

- 浏览器生命周期只走 **BrowserFacade**（见 ADR 0003、L001）
- 失败保留 error_code + 截图路径（不要贴 cookie）
- 发现「好像以前修过」→ **立刻**打开 `mistakes/INDEX.md`，勿凭印象硬刚

---

## 任务后（必做清单）

```text
[ ] go test ./...（或相关包测试）通过
[ ] 有新坑？
      → 复制 mistakes/templates/lesson.md
      → 编号 = mistakes/lessons 最大号 + 1
      → 更新 mistakes/INDEX.md 顶部插一行
[ ] 有架构选择（换库/换主路径/否决方案）？
      → 复制 templates/adr.md → adr/
      → 更新 adr/README.md
[ ] 可升格硬约束？
      → 仅当 ≥2 次或高成本；改 AGENTS 保持短
[ ] 可选：templates/session-note.md 写 3 条
[ ] ponytail: deferred <reason>（若砍了范围）
```

### 什么时候**不必**写 lesson

- 纯 typo / 格式
- 一次查文档就懂的 API 用法
- 已有 lesson 完全覆盖且无新信息

### 什么时候**必须**写

- 调试超过 ~30 分钟的根因
- 多模块交互（UI↔Supervisor↔SQLite）
- 假阳性登录、数据损坏、端口/锁死
- 你希望「下一个 Agent 别再犯」

---

## PR 自检（人类或 Agent）

```markdown
## Evolution
- [ ] No new lesson needed / added L0NN
- [ ] mistakes/INDEX updated if lesson added
- [ ] ADR added if architectural choice
- [ ] No secrets in docs
```

---

## 反模式

| 反模式 | 正确做法 |
|--------|----------|
| 把 20 条坑贴进 AGENTS | lesson + 索引；AGENTS 只留硬约束 |
| 只在对话里说「记住了」 | 已验证事实写 active memory；错误写 lesson/ADR |
| 删除旧 ADR | `superseded` + 链接新决策 |
| lesson 里贴 cookie/profile 绝对路径 | 只写机制与文件角色 |
| 为交差写空 lesson | 不写；或合并进已有条目 |

---

## 度量（轻量）

不必上仪表盘。季度可人工数：

- 新增 lesson 数 / 重复 area 占比
- 因 lesson 避免的重复 PR
- 升格进 AGENTS 的条数（宜少）
