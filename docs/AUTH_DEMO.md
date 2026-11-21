# 本机登录 Demo（方案 A）

## 行为

- **无用户**：API 开放（方便开发 / Playwright）
- **有用户**：`/api/*` 需 `Authorization: Bearer <token>`
- 公开：`/api/auth/status`、`/api/auth/login`
- 密码哈希本地 SQLite `app_users`；JWT 密钥 `data-dir/auth_secret`

## 跑通

```bash
# 一键 API 演示
./scripts/auth-demo.sh

# 手工 UI
DATA=~/.alpha-nexus
./dist/alpha-nexus --data-dir "$DATA" init
./dist/alpha-nexus --data-dir "$DATA" auth bootstrap --username demo --password demo123
./dist/alpha-nexus --data-dir "$DATA" ui serve
# 浏览器或 Electron 打开 UI → 登录 demo / demo123
```

## CLI

```bash
alpha-nexus auth status
alpha-nexus auth user-list
alpha-nexus auth user-add --username bob --password secret1
```
