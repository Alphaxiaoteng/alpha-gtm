# Playwright E2E

历史规划与通过记录已移除；当前结果必须以本轮真实执行为准。

## 跑测

```bash
# 一键（自动起 Go ui serve + Vite，reuse 已有进程）
./scripts/e2e-run.sh

# 测完并打开 Electron
./scripts/e2e-open-electron.sh
```

或：

```bash
cd e2e && npm install && npx playwright install chromium && npm test
```

## 范围

| 套件 | 内容 |
|------|------|
| `api.health` | overview / browsers / accounts 契约 |
| `shell` | 壳、后端状态、四 Tab |
| `accounts` | 列表/空态、搜索 |
| `settings` | 浏览器路径、并发、保存按钮 |

不测：平台真实登录、自动发布、真实 fingerprint 多开（用 Go unit + 手工）。

## testid 约定

`nav-*` / `app-shell` / `accounts-*` / `settings-*` / `account-card-{id}`

## Electron

Dev 下 Electron 启动或复用受监督的本地 Go sidecar，并加载 sidecar 返回的本地后端 URL；
端口由运行时描述符和健康握手确定，不能假定为 `5173`。独立 Playwright Web E2E 使用其
自己的隔离 `webServer` 配置，不作为 Electron 当前监听地址的证据。打包态另议。

## 失败产物

- 截图 / video / trace: `e2e/test-results/`
- HTML 报告: `e2e/playwright-report/`（`cd e2e && npm run report`）

## 当前证据

当前结果必须以本轮真实执行为准，不继承任何历史通过状态。
