# Alpha-Nexus DSH plugin

One DSH tool — `alpha_nexus` — that proxies the **whole** local Alpha-Nexus
business surface (accounts, content, knowledge, Studio, publishing, trend radar,
shop) without flooding the prompt.

## Why one tool

Alpha-Nexus exposes 97 tools (66 agent-visible). Registering each as a DSH tool
would cost ~20k prompt tokens per turn. This plugin registers **one** tool
(~300 tokens) with two actions:

| action | purpose |
|---|---|
| `list` | discover agent-visible tools (optional `category` filter) |
| `call` | run one tool by `tool` + `args` |

## Safety

`POST /api/v1/tools/call` has no approval gate on the Alpha-Nexus side, so this
plugin adds one: a tool whose `side_effect` is `write` runs only when the caller
passes `confirm: true`. Tools marked `agent_visible: false` are refused.

## Install

```bash
dsh plugin --profile <profile> add file:/absolute/path/to/plugins/alpha-nexus
```

`cordis.patch.yml` ships with:

```yaml
- insert:
    - id: alpha-nexus
      name: '@alpha-nexus/dsh-plugin'
      config:
        base_url: http://127.0.0.1:17421
        allow_remote: false
```

Point `base_url` at your Alpha-Nexus UI port (default 17421). Remote hosts are
refused unless `allow_remote: true`.

## Typical turn

```
alpha_nexus { action: "list", category: "account" }
→ 13 tools (read/write, with parameter lists)

alpha_nexus { action: "call", tool: "account.list", args: { limit: 20 } }
→ { ok: true, data: { accounts: [...] } }

alpha_nexus { action: "call", tool: "account.tag.create", args: { name: "vip" } }
→ { ok: false, data: { error: "ALPHA_NEXUS_CONFIRM_REQUIRED" } }

alpha_nexus { action: "call", tool: "account.tag.create", args: { name: "vip" }, confirm: true }
→ { ok: true, data: { ... } }
```

## Local checks

```bash
# pass-through stub
node --test test/local.test.mjs

# real dsh-tools schema compiler
DSH_TOOLS_PATH=<dsh>/node_modules/@deepseek-ai/dsh-tools/lib/index.js \
  node --test test/local.test.mjs
```

## Scope boundaries

- Read-only HTTP client: no `fs`, no `child_process`, no listening socket.
- No credentials in the plugin: `base_url` + optional bearer `auth_token`.
- Not loaded by `dsh` in this repo's tests; enrolment is a user action.
