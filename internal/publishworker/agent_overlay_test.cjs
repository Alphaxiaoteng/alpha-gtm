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
    const control = await createAgentControl(page);
    assert.equal(control.available, true);
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

    await page.goto('data:text/html,<button id="next">Next</button>');
    assert.equal(await page.locator('#alpha-nexus-agent-overlay').count(), 1);

    await control.dispose();
    assert.equal(await page.locator('#alpha-nexus-agent-overlay').count(), 0);
    await page.goto('data:text/html,<button id="done">Done</button>');
    assert.equal(await page.locator('#alpha-nexus-agent-overlay').count(), 0);
  } finally {
    await browser.close();
  }
});

test('duplicate installation replaces the stale host instead of stacking overlays', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<main></main>');
    const first = await createAgentControl(page);
    const second = await createAgentControl(page);
    assert.equal(first.available, true);
    assert.equal(second.available, true);
    assert.equal(await page.locator('#alpha-nexus-agent-overlay').count(), 1);
    await first.dispose();
    await second.dispose();
  } finally {
    await browser.close();
  }
});

test('shows EgoLite-style cursor feedback with the concrete control name', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<textarea aria-label="视频描述"></textarea><button aria-label="发布视频">Publish</button>');
    const control = await createAgentControl(page, { taskLabel: 'TikTok 一键发布' });
    await page.getByLabel('视频描述').click();
    assert.equal(
      await page.locator('#alpha-nexus-agent-overlay').evaluate((host) =>
        host.shadowRoot.querySelector('[data-agent-action]').textContent),
      '填写视频描述',
    );
    await page.getByLabel('发布视频').click();
    const state = await page.locator('#alpha-nexus-agent-overlay').evaluate((host) => ({
      cursorVisible: host.shadowRoot.querySelector('[data-agent-cursor]').dataset.visible,
      rippleCount: host.shadowRoot.querySelectorAll('[data-agent-ripple]').length,
      action: host.shadowRoot.querySelector('[data-agent-action]').textContent,
      task: host.shadowRoot.querySelector('[data-agent-task]').textContent,
      cursorFill: getComputedStyle(host.shadowRoot.querySelector('[data-agent-cursor]'), '::before').backgroundColor,
      actionBackground: getComputedStyle(host.shadowRoot.querySelector('[data-agent-action]')).backgroundColor,
    }));
    assert.equal(state.cursorVisible, 'true');
    assert.equal(state.rippleCount, 1);
    assert.equal(state.action, '点击发布视频');
    assert.equal(state.task, 'TikTok 一键发布');
    assert.equal(state.cursorFill, 'rgb(24, 24, 24)');
    assert.equal(state.actionBackground, 'rgb(81, 81, 81)');
    await control.dispose();
  } finally {
    await browser.close();
  }
});

test('uses the accessible name of a nested interactive control', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<button aria-label="上传"><span id="upload-icon">↑</span></button>');
    const control = await createAgentControl(page, { taskLabel: 'TikTok 内容发布准备' });
    const box = await page.locator('#upload-icon').boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    assert.equal(
      await page.locator('#alpha-nexus-agent-overlay').evaluate((host) =>
        host.shadowRoot.querySelector('[data-agent-action]').textContent),
      '点击上传',
    );
    await control.dispose();
  } finally {
    await browser.close();
  }
});

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

test('strict page CSP cannot disable the isolated overlay styles', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent(`
      <meta http-equiv="Content-Security-Policy" content="style-src 'none'">
      <button>Publish</button>
    `);
    const control = await createAgentControl(page);
    const styles = await page.locator('#alpha-nexus-agent-overlay').evaluate((host) => {
      const bar = host.shadowRoot.querySelector('[data-agent-bar]');
      const button = host.shadowRoot.querySelector('[data-agent-takeover]');
      return {
        barPosition: getComputedStyle(bar).position,
        buttonPointerEvents: getComputedStyle(button).pointerEvents,
      };
    });
    assert.deepEqual(styles, {
      barPosition: 'fixed',
      buttonPointerEvents: 'auto',
    });
    await control.dispose();
  } finally {
    await browser.close();
  }
});

test('takeover wins the race without waiting for the pending operation', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<main></main>');
    const control = await createAgentControl(page);
    const resultPromise = control.race(() =>
      new Promise((resolve) => setTimeout(() => resolve({ status: 'finished' }), 1000)));
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

test('stop task wins the race with the reviewed worker cancellation code', async () => {
  const { browser, page } = await launchPage();
  try {
    await page.setContent('<main></main>');
    const control = await createAgentControl(page, { taskLabel: 'TikTok 一键发布' });
    const resultPromise = control.race(() =>
      new Promise((resolve) => setTimeout(() => resolve({ status: 'finished' }), 1000)));
    await page.locator('#alpha-nexus-agent-overlay').evaluate((host) =>
      host.shadowRoot.querySelector('[data-agent-stop]').click());
    assert.deepEqual(await resultPromise, {
      status: 'permanent_failed',
      step: 'user_stop',
      errorCode: 'WORKER_CANCELLED',
      evidence: {},
    });
    await control.dispose();
  } finally {
    await browser.close();
  }
});

test('visual installation failure preserves the original operation result', async () => {
  const control = await createAgentControl({
    evaluate: async () => {
      throw new Error('page closed');
    },
    on() {},
    off() {},
  });
  assert.equal(control.available, false);
  assert.deepEqual(
    await control.race(async () => ({ status: 'prepared' })),
    { status: 'prepared' },
  );
  await control.dispose();
});
