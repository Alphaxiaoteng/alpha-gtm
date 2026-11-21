# TDD / BDD Strategy

## 结论

本项目用 BDD 锁业务行为，用 TDD 驱动实现。Ponytail 同时生效：测试也必须最小、直接、少依赖。

## 开发循环

每个 task 必须按这个顺序：

```text
BDD 场景
-> TDD 红灯测试
-> 最小实现
-> go test ./...
-> Ponytail 简化检查
-> 重构
-> 验收命令
```

禁止先实现完再补测试。

## BDD 规则

BDD 场景用 Markdown 写，不默认引入大型 BDD 框架。

格式：

```text
Given <前置状态>
When <用户执行命令>
Then <可观察结果>
And <数据库或文件状态>
```

V1 必须覆盖：

- 初始化本地数据目录。
- 六平台账号新增和检查。
- Lite 平台发布被拒绝。
- 批量账号检查。
- 批量内容导入。
- Meta 发现 Facebook Page / Instagram account。
- Instagram / Facebook 单条发布。
- 批量发布失败后 resume。
- TurboPush 导入。

## TDD 分层

优先使用 Go 标准 `testing`。

测试分层：

- Unit tests：配置、平台枚举、错误码、CSV 解析、批量状态机。
- Integration tests：SQLite migration、repository、Batch Runner、TurboPush importer。
- Contract tests：CLI 参数、NDJSON 输出、Lite 平台拒绝发布。
- Browser smoke tests：CDP 连接、profile lock、登录态检查骨架。

不默认引入第三方断言库。只有测试收益明确时才加依赖。

## 浏览器测试

真实浏览器和真实发布默认跳过：

```text
UNIBUY_E2E_BROWSER=1   允许跑浏览器 smoke
UNIBUY_E2E_PUBLISH=1   允许跑真实发布
```

无环境变量时，只跑纯单测、SQLite 集成测试、CLI contract test。

## 必测错误码

每个错误码必须有测试：

```text
API_CONNECTOR_UNAVAILABLE
AUTO_SUBMIT_NOT_SUPPORTED
IDEMPOTENCY_KEY_REQUIRED
LOGIN_REQUIRED
PROFILE_LOCKED
CDP_CONNECT_FAILED
UPLOAD_TIMEOUT
```

## Ponytail 测试约束

- BDD 场景写 Markdown，不加框架。
- TDD 优先 `testing`，不加大型测试依赖。
- SQLite 测试用临时目录和临时库。
- 批量测试用小样本，覆盖状态机即可。
- 浏览器测试只做 smoke，不把真实发布放进默认 CI。
- 如果测试需要复杂 mock，先检查能不能把逻辑拆成纯函数。

## 验收命令

默认：

```bash
go test ./...
```

专项：

```bash
go test ./internal/batch -run Test
go test ./internal/importers/turbopush -run Test
go test ./internal/cli -run Test
```
