# 平台数据与发布工作流

本机 SQLite 是数据与状态真源；EgoLite Space 只在用户当次明确操作时使用。没有本地定时器、周期采集或自动发布。

## 数据工作流

```text
选择同一浏览器下的账号
  → 登录 / Space 绑定检查
  → 已验证解析器：读取近 7 天后台 → 校验时间范围与证据 → 写本地快照
  → 未验证解析器：写入 manual_takeover → 用户在创作者后台人工核验
  → 本地看板 / 可选模型复盘（只发送已验证快照）
```

- 已验证自动快照：抖音、小红书、视频号、快手、TikTok、YouTube、Meta、微信公众号、CSDN。
- 其他已配置平台不会被编造为“采集成功”；它们会留下 `manual_takeover` 和 `EGO_METRICS_MANUAL_HANDOFF`，待有真实后台样本和回归夹具后再接入解析器。
- 同一 Space 始终串行；最多允许一次选择 100 个账号。批次可混合自动快照与人工接管结果。

## 发布工作流

```text
本地草稿 / 素材预检
  → 内容与目标校验
  → 冻结 publish plan（内容、账号、Space、素材快照）
  → 单次 prepare approval：预填后台，停在最终按钮前
  → 人工检查冻结目标与后台页面
  → 单次 submit-bound approval：每个目标最多点一次
  → 创作者后台独立核验：published / result_unknown / manual_takeover
```

- 所有发布入口均复用 `publish.plan.freeze`、`social.publish-prepare`、`social.publish-submit-approved`；UI 不复制发布逻辑。
- 只有经过审查的受控提交路径可进入最终单次授权；其他平台保持 `manual_handoff`。
- 最终点击不等于成功。缺少独立后台证据时必须记为 `result_unknown`，不得自动重试或显示为已发布。

通过 `social.workflow.catalog` 可读取每个平台的数据与发布边界；该工具只读，不会打开浏览器或触发任务。
