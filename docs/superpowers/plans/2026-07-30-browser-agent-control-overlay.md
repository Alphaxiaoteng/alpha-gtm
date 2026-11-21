# Browser Agent Control Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a safe Agent cursor, target highlight, action label, click ripple, and takeover control inside the fingerprint-chromium page while the existing publish Worker performs mutable browser actions.

**Architecture:** Add one dependency-free CommonJS visual controller beside the existing Worker. The Go runner embeds both scripts into its private temporary session. The Worker activates the controller only on the page selected for a mutable action and races the existing operation against a page-local takeover request; read-only inspection and all existing publish approval/evidence logic stay unchanged.

**Tech Stack:** Go `embed` and `os.WriteFile`; Node CommonJS; Playwright over the existing local CDP connection; Shadow DOM; Node test runner.

## Global Constraints

- Keep fingerprint-chromium and local CDP ports `15101–15600`; do not add a browser extension or change the browser engine.
- Do not add dependencies, database tables, services, schedulers, or public endpoints.
- The overlay may render only inside webpage content, never Chromium tabs, address bar, or system chrome.
- Do not change final-submit approval, frozen-plan, evidence, or `manual_takeover` safety contracts.
- Overlay failures must fail open visually while preserving the original Worker result.
- A takeover request must return `status: "manual_takeover"` and must never become publish-success evidence.
- Do not expose Cookie, content body, complete URL, profile path, selector text, or account identity in the overlay or logs.
- Respect `prefers-reduced-motion`; the takeover button must be keyboard accessible.
- Follow TDD: observe each focused test fail before adding its production implementation.

## Approved Visual Revision

The 2026-07-30 user screenshots supersede the earlier visual details in Tasks 2–3:

- Render an EgoLite-style black cursor with a white edge and a dark-gray action pill.
- Derive concrete on-page action text such as `填写视频描述` or `点击上传` from accessible control names; never read input values.
- Render a bottom black monitoring capsule up to 860px wide with the platform task name, control status, `接管`, and orange-red `终止任务`.
- Pass `taskLabel` through `createAgentControl(page, { taskLabel })`; TikTok prepare and submit labels are `TikTok 内容发布准备` and `TikTok 一键发布`.
- `接管` returns the existing `manual_takeover` contract; `终止任务` returns `WORKER_CANCELLED`.
- Do not render a fake `交还给 Agent`: same-task resume requires a future resumable-control protocol.
- Use Constructable Stylesheets so strict creator-site CSP cannot disable the Shadow DOM layout.
- Final visual acceptance attaches over CDP to the existing BrowserFacade-managed TikTok account browser; it must not call `chromium.launch`.

## File Structure

- Create `internal/publishworker/agent_overlay.cjs`: isolated Shadow DOM renderer, event-driven Agent pointer feedback, takeover polling, cleanup, and no-op fallback.
- Create `internal/publishworker/agent_overlay_test.cjs`: focused Playwright tests for rendering, navigation recovery, motion accessibility, takeover, cleanup, and visual failure fallback.
- Modify `internal/publishworker/runner.go`: embed and write the overlay module into the same private Worker session as `worker.cjs`.
- Modify `internal/publishworker/runner_test.go`: prove the overlay asset exists in the sanitized temporary Worker session.
- Modify `internal/publishworker/worker.cjs`: activate the controller around mutable operations only and preserve all existing output contracts.
- Modify `internal/publishworker/worker_test.cjs`: prove takeover mapping and read-only exclusion without changing existing platform fixtures.

---

### Task 1: Bundle the overlay module in the Worker session

**Files:**
- Create: `internal/publishworker/agent_overlay.cjs`
- Modify: `internal/publishworker/runner.go`
- Test: `internal/publishworker/runner_test.go`

**Interfaces:**
- Consumes: the existing `Runner.Run(ctx, input)` private temporary directory.
- Produces: `agent_overlay.cjs` beside `publisher-worker.cjs`, readable only by the current user.

- [ ] **Step 1: Write the failing Go test**

Add a Unix-only fake Node fixture that verifies the second embedded asset exists:

```go
func TestRunnerBundlesAgentOverlayBesideWorker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	node := filepath.Join(dir, "fake-node")
	script := `#!/bin/sh
overlay="$(dirname "$1")/agent_overlay.cjs"
if [ ! -f "$overlay" ]; then
  printf '%s\n' '{"schemaVersion":1,"ok":false,"status":"permanent_failed","step":"asset","errorCode":"OVERLAY_ASSET_MISSING"}'
  exit 1
fi
printf '%s\n' '{"schemaVersion":1,"ok":true,"status":"inspected","step":"inspect","errorCode":""}'
`
	if err := os.WriteFile(node, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(filepath.Join(moduleDir, "playwright"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := (Runner{NodePath: node, ModuleDir: moduleDir}).Run(context.Background(), baseInput())
	if err != nil || output == nil || output.Status != "inspected" {
		t.Fatalf("output=%+v err=%v", output, err)
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./internal/publishworker -run TestRunnerBundlesAgentOverlayBesideWorker -count=1
```

Expected: FAIL because `agent_overlay.cjs` is absent from the temporary session.

- [ ] **Step 3: Add the minimal embedded asset**

Create `agent_overlay.cjs` with an initially exported no-op controller:

```js
'use strict';

async function createAgentControl() {
  return {
    available: false,
    async race(operation) { return operation(); },
    async dispose() {},
  };
}

module.exports = { createAgentControl };
```

Add the second embed and session write:

```go
const agentOverlayScriptName = "agent_overlay.cjs"

//go:embed agent_overlay.cjs
var agentOverlayScript []byte
```

Immediately after writing `publisher-worker.cjs`:

```go
overlayPath := filepath.Join(sessionRoot, agentOverlayScriptName)
if err := os.WriteFile(overlayPath, agentOverlayScript, 0o600); err != nil {
	return nil, fmt.Errorf("WORKER_SESSION_FAILED")
}
```

- [ ] **Step 4: Run focused and package tests**

Run:

```bash
go test ./internal/publishworker -run 'TestRunnerBundlesAgentOverlayBesideWorker|TestRunnerUsesSanitizedSessionEnvironment' -count=1
go test ./internal/publishworker -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the isolated asset plumbing**

```bash
git add internal/publishworker/agent_overlay.cjs internal/publishworker/runner.go internal/publishworker/runner_test.go
git commit -m "feat: bundle browser agent overlay"
```

---

### Task 2: Render and restore the accessible page overlay

**Files:**
- Modify: `internal/publishworker/agent_overlay.cjs`
- Create: `internal/publishworker/agent_overlay_test.cjs`

**Interfaces:**
- Consumes: a Playwright `Page`.
- Produces: `createAgentControl(page) -> Promise<AgentControl>`.
- `AgentControl.available` is a boolean.
- `AgentControl.race(operation: () => Promise<object>) -> Promise<object>`.
- `AgentControl.dispose() -> Promise<void>`.

- [ ] **Step 1: Write failing rendering and navigation tests**

Use the existing bundled Playwright runtime:

```js
'use strict';

const assert = require('node:assert/strict');
const { test } = require('node:test');
const { chromium } = require('../../e2e/node_modules/playwright');
const { createAgentControl } = require('./agent_overlay.cjs');

async function launchPage() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  return { browser, page };
}

test('installs one isolated accessible overlay and restores it after navigation', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<button id="publish">Publish</button>');
    const first = await createAgentControl(page);
    const second = await createAgentControl(page);
    assert.equal(first.available, true);
    assert.equal(second.available, true);
    assert.equal(await page.locator('#alpha-nexus-agent-overlay').count(), 1);
    assert.deepEqual(await page.locator('#alpha-nexus-agent-overlay').evaluate((host) => {
      const button = host.shadowRoot.querySelector('[data-agent-takeover]');
      return {
        hostPointerEvents: getComputedStyle(host).pointerEvents,
        buttonPointerEvents: getComputedStyle(button).pointerEvents,
        label: button.getAttribute('aria-label'),
      };
    }), {
      hostPointerEvents: 'none',
      buttonPointerEvents: 'auto',
      label: '接管 Agent 操作',
    });
    await first.dispose();
    await second.dispose();
    await page.goto('data:text/html,<button>Next</button>');
    assert.equal(await page.locator('#alpha-nexus-agent-overlay').count(), 0);
  } finally {
    await browser.close();
  }
});

test('shows target feedback for Playwright pointer and focus events', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<input aria-label="title"><button>Publish</button>');
    const control = await createAgentControl(page);
    await page.getByLabel('title').click();
    await page.getByRole('button', { name: 'Publish' }).click();
    const state = await page.locator('#alpha-nexus-agent-overlay').evaluate((host) => ({
      cursorVisible: host.shadowRoot.querySelector('[data-agent-cursor]').dataset.visible,
      targetVisible: host.shadowRoot.querySelector('[data-agent-target]').dataset.visible,
      rippleCount: host.shadowRoot.querySelectorAll('[data-agent-ripple]').length,
      action: host.shadowRoot.querySelector('[data-agent-action]').textContent,
    }));
    assert.equal(state.cursorVisible, 'true');
    assert.equal(state.targetVisible, 'true');
    assert.equal(state.rippleCount, 1);
    assert.equal(state.action, '点击按钮');
    await control.dispose();
  } finally {
    await browser.close();
  }
});
```

- [ ] **Step 2: Run the rendering tests and verify RED**

Run:

```bash
node --test internal/publishworker/agent_overlay_test.cjs
```

Expected: FAIL because the no-op controller does not render a host or track events.

- [ ] **Step 3: Implement the Shadow DOM renderer**

Implement these private constants and functions:

```js
const HOST_ID = 'alpha-nexus-agent-overlay';
const NAVIGATION_EVENT = 'domcontentloaded';

function genericActionLabel(target) {
  const tag = String(target?.tagName || '').toLowerCase();
  const role = String(target?.getAttribute?.('role') || '').toLowerCase();
  if (tag === 'input' || tag === 'textarea' || target?.isContentEditable) return '填写内容';
  if (tag === 'select' || role === 'combobox' || role === 'option') return '选择选项';
  if (tag === 'button' || tag === 'a' || role === 'button' || role === 'link') return '点击按钮';
  return '操作控件';
}
```

`installOverlayDOM()` must:

- remove a stale host with `HOST_ID`;
- create a fixed host with `pointer-events: none`;
- attach an open Shadow DOM;
- render namespaced cursor, target, action, ripple container, status text, and takeover button;
- set `aria-live="polite"` on the status text;
- set `aria-label="接管 Agent 操作"` and `pointer-events: auto` on the button;
- listen in capture phase to `pointermove`, `focusin`, and `click`;
- derive only generic action labels, never page text;
- update target geometry with `getBoundingClientRect()`;
- create exactly one short-lived ripple per click;
- include `@media (prefers-reduced-motion: reduce)` rules that remove transitions, transforms, and keyframe animation.

`createAgentControl(page)` must install immediately, register a `domcontentloaded` listener that reinstalls after navigation, and make duplicate calls replace the stale host instead of stacking overlays.

- [ ] **Step 4: Add and pass reduced-motion and cleanup tests**

Add:

```js
test('reduced motion removes movement and dispose removes the overlay', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.setContent('<button>Publish</button>');
    const control = await createAgentControl(page);
    const transition = await page.locator('#alpha-nexus-agent-overlay').evaluate((host) =>
      getComputedStyle(host.shadowRoot.querySelector('[data-agent-cursor]')).transitionDuration);
    assert.equal(transition, '0s');
    await control.dispose();
    assert.equal(await page.locator('#alpha-nexus-agent-overlay').count(), 0);
  } finally {
    await browser.close();
  }
});
```

Run:

```bash
node --test internal/publishworker/agent_overlay_test.cjs
```

Expected: PASS.

- [ ] **Step 5: Commit the visual layer**

```bash
git add internal/publishworker/agent_overlay.cjs internal/publishworker/agent_overlay_test.cjs
git commit -m "feat: render browser agent controls"
```

---

### Task 3: Race mutable Worker actions against takeover

**Files:**
- Modify: `internal/publishworker/agent_overlay.cjs`
- Modify: `internal/publishworker/agent_overlay_test.cjs`
- Modify: `internal/publishworker/worker.cjs`
- Modify: `internal/publishworker/worker_test.cjs`

**Interfaces:**
- Consumes: `createAgentControl(page)`.
- Produces: takeover result `{ status: "manual_takeover", step: "user_takeover", errorCode: "FORM_REVIEW_REQUIRED", evidence: {} }`.
- Produces: `withAgentControl(page, operation) -> Promise<WorkerResult>` in `worker.cjs`.

- [ ] **Step 1: Write failing takeover and failure-fallback tests**

Add to `agent_overlay_test.cjs`:

```js
test('takeover wins the race without waiting for the pending operation', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<main></main>');
    const control = await createAgentControl(page);
    const resultPromise = control.race(() => new Promise((resolve) => setTimeout(() => resolve({ status: 'finished' }), 1000)));
    await page.locator('#alpha-nexus-agent-overlay').evaluate((host) =>
      host.shadowRoot.querySelector('[data-agent-takeover]').click());
    assert.deepEqual(await resultPromise, {
      status: 'manual_takeover',
      step: 'user_takeover',
      errorCode: 'FORM_REVIEW_REQUIRED',
      evidence: {},
    });
    await control.dispose();
  } finally {
    await browser.close();
  }
});

test('visual installation failure preserves the original operation result', async () => {
  const control = await createAgentControl({
    evaluate: async () => { throw new Error('page closed'); },
    on() {},
    off() {},
  });
  assert.equal(control.available, false);
  assert.deepEqual(await control.race(async () => ({ status: 'prepared' })), { status: 'prepared' });
  await control.dispose();
});
```

Add a `worker_test.cjs` test for the integration helper:

```js
test('withAgentControl maps takeover to the existing manual takeover contract', async () => {
  const fakeControl = {
    async race() {
      return { status: 'manual_takeover', step: 'user_takeover', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
    },
    async dispose() {},
  };
  const result = await withAgentControl({}, async () => ({ status: 'prepared' }), async () => fakeControl);
  assert.equal(result.status, 'manual_takeover');
  assert.equal(result.step, 'user_takeover');
  assert.equal(result.errorCode, 'FORM_REVIEW_REQUIRED');
});
```

- [ ] **Step 2: Run both tests and verify RED**

Run:

```bash
node --test internal/publishworker/agent_overlay_test.cjs
node --test --test-name-pattern='withAgentControl maps takeover' internal/publishworker/worker_test.cjs
```

Expected: FAIL because takeover polling and `withAgentControl` do not exist.

- [ ] **Step 3: Implement takeover polling and no-op fallback**

The real controller must use a page-local flag set by the takeover button. It must not install an exposed global binding or persistent browser extension.

Use a 50ms bounded poll while the operation is active:

```js
const TAKEOVER_RESULT = Object.freeze({
  status: 'manual_takeover',
  step: 'user_takeover',
  errorCode: 'FORM_REVIEW_REQUIRED',
  evidence: {},
});

async function waitForTakeover(page, isActive) {
  while (isActive()) {
    const requested = await page.evaluate((hostID) =>
      document.getElementById(hostID)?.dataset.takeover === 'true', HOST_ID).catch(() => false);
    if (requested) return { ...TAKEOVER_RESULT, evidence: {} };
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  return new Promise(() => {});
}
```

`race(operation)` starts the original operation lazily and returns `Promise.race([operation(), waitForTakeover(...)])`. `dispose()` sets the active flag to false, unregisters `domcontentloaded`, and removes the host. If initial installation fails, return a no-op controller whose `race` returns the original operation unchanged.

- [ ] **Step 4: Integrate only mutable Worker paths**

At the top of `worker.cjs`:

```js
const { createAgentControl } = require('./agent_overlay.cjs');
```

Add:

```js
async function withAgentControl(page, operation, factory = createAgentControl) {
  const control = await factory(page);
  try {
    return await control.race(operation);
  } finally {
    await control.dispose();
  }
}
```

Wrap these existing mutable paths:

- `delete_content`: `withAgentControl(activePage, () => deleteYouTubeManagerContent(activePage, input))`;
- `resume_topics`: `withAgentControl(activePage, () => resumeTopics(activePage, input))`;
- `submit_prepared`: `withAgentControl(page, () => submitPreparedPage(page, input, {}, PLATFORM_RULES[input.platform]))`;
- `prepare` and `submit`: `withAgentControl(page, () => preparePage(page, input))`.

Do not wrap `inspect`, `inspect_settings`, `inspect_composer`, or `inspect_manager`.

Export `withAgentControl` for the focused test. Preserve existing screenshot capture for non-takeover `manual_takeover`; do not capture a new screenshot solely because the user requested control.

- [ ] **Step 5: Run focused and full Worker tests**

Run:

```bash
node --test internal/publishworker/agent_overlay_test.cjs
node --test --test-name-pattern='withAgentControl maps takeover' internal/publishworker/worker_test.cjs
node --test internal/publishworker/worker_test.cjs
go test ./internal/publishworker ./internal/platforms -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit takeover integration**

```bash
git add internal/publishworker/agent_overlay.cjs internal/publishworker/agent_overlay_test.cjs internal/publishworker/worker.cjs internal/publishworker/worker_test.cjs
git commit -m "feat: allow browser agent takeover"
```

---

### Task 4: Verify regression and visual behavior

**Files:**
- Modify only if a test exposes an in-scope defect in the files listed above.

**Interfaces:**
- Consumes: the completed overlay and Worker integration.
- Produces: verified test evidence; no new runtime interface.

- [ ] **Step 1: Run formatting and static checks**

Run:

```bash
gofmt -w internal/publishworker/runner.go internal/publishworker/runner_test.go
git diff --check
node --check internal/publishworker/agent_overlay.cjs
node --check internal/publishworker/worker.cjs
```

Expected: no output from `git diff --check` or either `node --check`.

- [ ] **Step 2: Run affected test suites**

Run:

```bash
node --test internal/publishworker/agent_overlay_test.cjs
node --test internal/publishworker/worker_test.cjs
go test ./internal/publishworker ./internal/platforms ./internal/publishplan ./internal/publish -count=1
```

Expected: PASS.

- [ ] **Step 3: Run the repository Go regression**

Run:

```bash
go test -p 1 ./... -count=1
```

Expected: PASS. If an unrelated pre-existing failure occurs, record its exact package and error as unverified rather than changing unrelated code.

- [ ] **Step 4: Perform a local headed visual smoke test without a real publish**

Use a temporary Playwright page, not a creator account:

```bash
node --test --test-name-pattern='shows target feedback|takeover wins' internal/publishworker/agent_overlay_test.cjs
```

Expected: PASS; the test exercises real Chromium DOM, pointer, focus, click, Shadow DOM, and takeover behavior without clicking a platform publish button.

- [ ] **Step 5: Commit only if verification required an in-scope correction**

```bash
git add internal/publishworker/agent_overlay.cjs internal/publishworker/agent_overlay_test.cjs internal/publishworker/runner.go internal/publishworker/runner_test.go internal/publishworker/worker.cjs internal/publishworker/worker_test.cjs
git commit -m "fix: harden browser agent overlay"
```
