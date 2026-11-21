# Agent provider icon sources

These icons identify third-party local CLI platforms in Alpha-Nexus. They do
not imply endorsement.

- `kimi-code.png`: MoonshotAI's official `kimi-cli` repository,
  `web/public/logo.png`.
- `codebuddy.svg`: official CodeBuddy documentation favicon,
  `https://www.codebuddy.cn/docs/cli/quickstart`.
- `claude-code.ico`: official Claude favicon,
  `https://claude.ai/favicon.ico`, used for the Claude Code CLI row.
The catalog coverage and status presentation were checked against:

- `nexu-io/open-design`, `apps/daemon/src/runtimes/registry.ts`
- `stablyai/orca`, `src/renderer/src/lib/agent-catalog.tsx`

The Settings catalog only shows providers with an Alpha-Nexus runtime adapter.
Catalog presence still does not prove that the local CLI or model works: the
UI adds it only after the requested model completes a real validation call.
