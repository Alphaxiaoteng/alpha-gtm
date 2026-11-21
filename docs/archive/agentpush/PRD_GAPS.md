# PRD 缺口 vs 现状（持续更新）

对照 `PRD.md`。栈：Go 控制面 + fingerprint-chromium + CDP（ADR 0007）。

## 能 / 不能叫「AI 浏览器」

| 含义 | 现状 |
|------|------|
| 用 **AI 操控已启动的指纹浏览器**（snapshot → LLM → click/type/navigate） | ✅ **已接入 demo**：`agent browser` |
| 换成 BrowserOS / 第三方「AI Browser」产品当主浏览器 | ❌ 不做主线；Provider 接口可扩展 |
| AI 自动过验证码 / 2FA | ❌ 禁止；必须 `manual_takeover` |

```bash
# 1) 先起指纹浏览器
alpha-nexus browser start --account-id 1

# 2) AI 浏览器 agent（无 LLM 时：URL goal 会 navigate，否则 takeover）
alpha-nexus agent browser --account-id 1 --goal "https://example.com"

# 接 Ollama / OpenAI-compatible：
export AGENTPUSH_LLM_BASE=http://127.0.0.1:11434/v1
export AGENTPUSH_LLM_MODEL=llama3.2
```

## 功能矩阵

| PRD 块 | 状态 | 说明 |
|--------|------|------|
| 内容导入 / SHA256 / 预检 | ✅ 部分 | `media import/index/preflight`；无完整富文本编辑器 |
| 多语言 content version | ✅ 脚手架 | `content version` |
| 账号 + 独立 profile | ✅ | account / browser Facade |
| 账号分组 brand/region | ✅ | `account_group`；`account add --group` / `set-group` / list filter / API `?group=` |
| 登录态健康 | ✅ 保守实现 | 成功只认审核过的精确 Cookie 名；已识别登录页才写 `logged_out`，其余返回 `manual_verify` |
| 发布 dry-run / 状态机 / batch | ✅ | publish / batch publish |
| 发布 mode/approval | 🟡 | 参数与持久化意图已接线；`api_first` 没有可用原生 Connector，不能视为真实发布能力 |
| Meta 真提交 | 🟡 | best-effort；只有点击后新增的明确成功区域可判成功，真实账号验收未完成 |
| 22 个发布代码 | ✅ 契约 / 🟡 真发 | 2 个 Meta best-effort；YouTube/TikTok 受控浏览器提交契约；其余按人工接管 / legacy API 边界；custom unavailable |
| Content/Adapt/Publish/Monitor/Analytics Agent | ✅ | 无密钥模板回落 |
| **BrowserAgent（AI 控浏览器）** | ✅ demo | 本文件上线 |
| 指标采集 / 跨平台报表 | 🟡 | metrics collect/list；报表弱 |
| 工具用户登录 JWT | ✅ | `auth bootstrap` + 登录页 |
| 云端多租户 | ❌ | 非目标 |

## 建议下一刀

1. 使用专用测试账号分阶段验收 Meta：上传 → 填写 → 提交 → 新成功证据；每阶段单独授权。
2. 对 XHS Creator 获取真实 DOM fixture 后再实现 Native 上传/填写；证据不足继续人工接管。
3. 使用测试账号完成 YouTube/TikTok 受控浏览器路径的逐字段和 Published 验收；原生 API Connector 仍需官方应用与调用授权。X 继续 fail-closed。

不新增调度器、定时发布或周期采集。
