'use strict';

const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { test } = require('node:test');
const { chromium } = require('../../e2e/node_modules/playwright');
const {
  validateInput,
  PLATFORM_RULES,
  submitSelectorCandidates,
  contentEditableValueMatches,
  hasTrailingTopicMarker,
  applyPublishTime,
  applySettings,
  applyNativeVideoCover,
  nativeVideoCoverPreviewCommitted,
  settleDouyinAIConfirmation,
  clickReviewedSubmitOnce,
  exactComposerSubmit,
  xhsComposerModeMatches,
  prepareAndMaybeSubmit,
  submitPreparedPage,
  prepareForm,
  preparePage,
  prepareTargetPage,
  preparedSubmitTargetPage,
  recoverPreUploadTransientError,
  newestMatchingPage,
  reviewedEntryReachPrefixes,
  reviewedEditorWaitMs,
  immutableFilePayloadBundle,
  reviewedUploadSnapshot,
  inspectPage,
  revealReviewSettings,
  resumeTopics,
  inspectKuaishouManager,
  managerInspectionPage,
  deleteYouTubeManagerContent,
  classifyWorkerFailure,
  resolveLoopbackCDPWebSocket,
  localCDPConnectionOptions,
  discardReviewedStaleDraft,
  withAgentControl,
  workerAgentTaskLabel,
} = require('./worker.cjs');

test('withAgentControl maps takeover to the existing manual takeover contract', async () => {
  let receivedOptions;
  const fakeControl = {
    async race() {
      return {
        status: 'manual_takeover',
        step: 'user_takeover',
        errorCode: 'FORM_REVIEW_REQUIRED',
        evidence: {},
      };
    },
    async dispose() {},
  };
  const result = await withAgentControl(
    {},
    'TikTok 一键发布',
    async () => ({ status: 'prepared' }),
    async (_page, options) => {
      receivedOptions = options;
      return fakeControl;
    },
  );
  assert.deepEqual(receivedOptions, { taskLabel: 'TikTok 一键发布' });
  assert.equal(result.status, 'manual_takeover');
  assert.equal(result.step, 'user_takeover');
  assert.equal(result.errorCode, 'FORM_REVIEW_REQUIRED');
});

test('workerAgentTaskLabel describes the real platform action without content text', () => {
  assert.equal(
    workerAgentTaskLabel({ platform: 'tiktok', action: 'prepare' }),
    'TikTok 内容发布准备',
  );
  assert.equal(
    workerAgentTaskLabel({ platform: 'tiktok', action: 'submit_prepared' }),
    'TikTok 一键发布',
  );
  assert.equal(
    workerAgentTaskLabel({ platform: 'douyin', action: 'resume_topics' }),
    '抖音 话题补全',
  );
});

test('YouTube deletion targets the matching scheduled row and confirms once', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <ytcp-video-row id="target">
        <a id="video-title">αgamer | 阿尔法玩家，正式入驻</a>
        <span>Scheduled</span>
        <button aria-label="Options" id="options">Options</button>
      </ytcp-video-row>
      <ytcp-video-row>
        <a id="video-title">αgamer | 阿尔法玩家，正式入驻</a>
        <span>Draft</span>
      </ytcp-video-row>
      <button id="delete-menu" hidden>Delete forever</button>
      <section role="dialog" id="dialog" hidden>
        <label><input type="checkbox" />I understand that deleting is permanent</label>
        <button id="confirm-delete">Delete forever</button>
      </section>
      <p id="deleted" hidden>Content deleted</p>
      <script>
        document.querySelector('#options').onclick = () => document.querySelector('#delete-menu').hidden = false;
        document.querySelector('#delete-menu').onclick = () => {
          document.querySelector('#delete-menu').hidden = true;
          document.querySelector('#dialog').hidden = false;
        };
        document.querySelector('#confirm-delete').onclick = () => {
          document.querySelector('#target').remove();
          document.querySelector('#dialog').hidden = true;
          document.querySelector('#deleted').hidden = false;
        };
      </script>
    `);
    const result = await deleteYouTubeManagerContent(page, {
      content: { title: 'αgamer｜阿尔法玩家，正式入驻' },
      timeoutMs: 5000,
    });
    assert.equal(result.status, 'deleted', JSON.stringify(result));
    assert.equal(await page.locator('#target').count(), 0);
    assert.equal(await page.locator('ytcp-video-row').count(), 1);
  } finally {
    await browser.close();
  }
});

test('classifyWorkerFailure exposes only reviewed categories', () => {
  assert.equal(
    classifyWorkerFailure(new Error('Target page, context or browser has been closed')),
    'CDP_TARGET_CLOSED',
  );
  assert.equal(classifyWorkerFailure(new Error("Cannot find module 'playwright'")), 'PLAYWRIGHT_RUNTIME_UNAVAILABLE');
  assert.equal(classifyWorkerFailure(new Error('SETTINGS_UNSUPPORTED')), 'SETTINGS_UNSUPPORTED');
  assert.equal(classifyWorkerFailure(new Error('cover write timeout')), 'WORKER_ACTION_TIMEOUT');
  assert.equal(classifyWorkerFailure(new Error('unexpected private URL https://example.test/?token=x')), 'WORKER_EXECUTION_FAILED');
});

test('Douyin confirms the observed no-declaration dialog choice before continuing', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <section role="dialog"><h2>对作品内容添加声明</h2>
        <div id="no-declaration">无需添加自主声明</div><button id="confirm">确定</button>
      </section>
      <script>document.querySelector('#confirm').onclick = () => document.querySelector('[role=dialog]').hidden = true;</script>
    `);
    assert.equal(await settleDouyinAIConfirmation(page, { platform: 'douyin', settings: { self_declaration: 'none' } }), true);
    assert.equal(await page.locator('[role=dialog]').isVisible(), false);
  } finally {
    await browser.close();
  }
});

test('Douyin does not toggle an already-selected no-declaration radio before confirming', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <section role="dialog"><h2>对作品内容添加声明</h2>
        <label id="no-declaration"><input type="radio" name="declaration" aria-label="无需添加自主声明" checked><span>无需添加自主声明</span></label>
        <button id="confirm"><span>确定</span></button>
      </section>
      <script>
        document.querySelector('#no-declaration').onclick = () => {
          document.querySelector('input').checked = !document.querySelector('input').checked;
        };
        document.querySelector('#confirm').onclick = () => {
          if (document.querySelector('input').checked) document.querySelector('[role=dialog]').hidden = true;
        };
      </script>
    `);
    assert.equal(await settleDouyinAIConfirmation(page, { platform: 'douyin', settings: { self_declaration: 'none' } }), true);
    assert.equal(await page.locator('[role=dialog]').isVisible(), false);
  } finally {
    await browser.close();
  }
});

test('resolveLoopbackCDPWebSocket uses the canonical version path and rejects remote sockets', async () => {
  let requested = '';
  const endpoint = await resolveLoopbackCDPWebSocket('http://127.0.0.1:15102/', async (url) => {
    requested = url;
    return { ok: true, json: async () => ({ webSocketDebuggerUrl: 'ws://127.0.0.1:15102/devtools/browser/test' }) };
  });
  assert.equal(requested, 'http://127.0.0.1:15102/json/version');
  assert.equal(endpoint, 'ws://127.0.0.1:15102/devtools/browser/test');
  await assert.rejects(
    () => resolveLoopbackCDPWebSocket('http://127.0.0.1:15102/', async () => ({ ok: true, json: async () => ({ webSocketDebuggerUrl: 'ws://example.test:15102/devtools/browser/test' }) })),
    /CDP_CONNECT_FAILED/,
  );
});

test('localCDPConnectionOptions preserves the user browser context', () => {
  assert.deepEqual(localCDPConnectionOptions(600000), {
    timeout: 15000,
    isLocal: true,
    noDefaults: true,
  });
});

test('manager inspection fails closed when clicking the sidebar does not produce manager evidence', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button onclick="history.pushState({}, '', '/profile')">内容管理</button>
      <button>定时发布</button>
    `);
    const result = await inspectKuaishouManager(page, {
      platform: 'kuaishou', content: { title: 'αgamer｜阿尔法玩家，正式入驻' }, timeoutMs: 3000,
    });
    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'manager_navigation_unverified');
    assert.equal(result.evidence.managerNavigation, true);
    assert.equal(result.evidence.managerLocationChanged, false);
    assert.match(result.evidence.screenshotPath, /kuaishou-\d+-\d+\.png$/);
    assert.equal(fs.existsSync(result.evidence.screenshotPath), true);
  } finally {
    await browser.close();
  }
});

test('WeChat Video manager inspection ignores a newer login tab when a platform tab exists', async () => {
  const browser = await launchChromium();
  try {
    const platform = await browser.newPage();
    await platform.route('https://channels.weixin.qq.com/**', (route) => route.fulfill({
      status: 200, contentType: 'text/html', body: '<main>fixture</main>',
    }));
    await platform.goto('https://channels.weixin.qq.com/platform');
    const login = await browser.newPage();
    await login.route('https://channels.weixin.qq.com/**', (route) => route.fulfill({
      status: 200, contentType: 'text/html', body: '<main>fixture</main>',
    }));
    await login.goto('https://channels.weixin.qq.com/login.html');
    assert.equal(
      managerInspectionPage([platform, login], login, 'wechat_video'),
      platform,
    );
  } finally {
    await browser.close();
  }
});

test('WeChat Video manager inspection requires title, scheduled state, and exact time', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="manager">内容管理</button>
      <button id="video" hidden>视频</button>
      <section id="items" hidden>
        <h2>αgamer｜阿尔法玩家，正式入驻</h2>
        <span>定时发布中</span>
        <time>2026-07-30 21:55</time>
      </section>
      <script>
        document.querySelector('#manager').onclick = () => {
          document.querySelector('#video').hidden = false;
        };
        document.querySelector('#video').onclick = () => {
          document.querySelector('#items').hidden = false;
        };
      </script>
    `);
    const result = await inspectKuaishouManager(page, {
      platform: 'wechat_video',
      content: { title: 'αgamer｜阿尔法玩家，正式入驻' },
      publishAt: '2026-07-30T13:55:00Z',
      timeoutMs: 5000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.scheduledState, true);
    assert.equal(result.evidence.scheduledTime, true);
    assert.equal(result.evidence.effectiveScheduledAt, '2026-07-30T13:55:00.000Z');
  } finally {
    await browser.close();
  }
});

test('TikTok manager inspection verifies the scheduled card without returning to upload', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://www.tiktok.com/tiktokstudio/content', (route) => route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: `
        <article>
          <h2>你好，这里是 αgamer | 阿尔法玩家。从这条内容开始…</h2>
          <time>Jul 31, 2026</time>
          <time>1:45 PM</time>
        </article>
      `,
    }));
    await page.goto('https://www.tiktok.com/tiktokstudio/content');
    const result = await inspectKuaishouManager(page, {
      platform: 'tiktok',
      content: {
        title: 'αgamer｜阿尔法玩家，正式入驻',
        caption: '你好，这里是 αgamer｜阿尔法玩家。\n后续内容',
      },
      publishAt: '2026-07-31T13:44:00+08:00',
      timeoutMs: 5000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.location, 'https://www.tiktok.com/tiktokstudio/content');
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.scheduledState, true);
    assert.equal(result.evidence.effectiveScheduledAt, '2026-07-31T05:45:00.000Z');
    assert.equal(result.evidence.scheduledTime, true);
  } finally {
    await browser.close();
  }
});

test('TikTok manager inspection opens the localized 作品 entry', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://www.tiktok.com/tiktokstudio', (route) => route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: `
        <button id="content">作品</button>
        <article id="scheduled" hidden>
          <h2>你好，这里是 αgamer | 阿尔法玩家。从这条内容开始…</h2>
          <time>⏰ 7月31日 13:45</time>
        </article>
        <script>
          document.querySelector('#content').onclick = () => {
            history.pushState({}, '', '/tiktokstudio/content');
            document.querySelector('#scheduled').hidden = false;
          };
        </script>
      `,
    }));
    await page.goto('https://www.tiktok.com/tiktokstudio');
    const result = await inspectKuaishouManager(page, {
      platform: 'tiktok',
      content: {
        title: 'αgamer｜阿尔法玩家，正式入驻',
        caption: '你好，这里是 αgamer｜阿尔法玩家。\n后续内容',
      },
      publishAt: '2026-07-31T13:44:00+08:00',
      timeoutMs: 5000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.location, 'https://www.tiktok.com/tiktokstudio/content');
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.scheduledState, true);
    assert.equal(result.evidence.effectiveScheduledAt, '2026-07-31T05:45:00.000Z');
  } finally {
    await browser.close();
  }
});

test('YouTube manager inspection verifies the scheduled row through Studio Content', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://studio.youtube.com/**', (route) => route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: `
        <button id="content">Content</button>
        <article id="scheduled" hidden>
          <h2>αgamer｜阿尔法玩家，正式入驻</h2>
          <span>Scheduled</span>
        </article>
        <script>
          document.querySelector('#content').onclick = () => {
            history.pushState({}, '', '/channel/test/videos/upload');
            document.querySelector('#scheduled').hidden = false;
          };
        </script>
      `,
    }));
    await page.goto('https://studio.youtube.com/channel/test');
    const result = await inspectKuaishouManager(page, {
      platform: 'youtube',
      content: { title: 'αgamer｜阿尔法玩家，正式入驻' },
      publishAt: '2026-07-31T19:26:00+08:00',
      timeoutMs: 5000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.scheduledState, true);
  } finally {
    await browser.close();
  }
});

test('YouTube manager inspection reuses an already-open Studio content list', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://studio.youtube.com/**', (route) => route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: `
        <article>
          <h2>αgamer｜阿尔法玩家，正式入驻</h2>
          <span>Scheduled</span>
        </article>
      `,
    }));
    await page.goto('https://studio.youtube.com/channel/test/videos/upload');
    const result = await inspectKuaishouManager(page, {
      platform: 'youtube',
      content: { title: 'αgamer｜阿尔法玩家，正式入驻' },
      publishAt: '2026-07-31T19:26:00+08:00',
      timeoutMs: 1000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.evidence.managerLocationChanged, false);
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.scheduledState, true);
  } finally {
    await browser.close();
  }
});

test('YouTube manager inspection returns public identity only from the exact matching row', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://studio.youtube.com/**', (route) => route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: `
        <ytcp-video-row>
          <a id="video-title" href="/video/AbCdEf12345/edit">αgamer | 阿尔法玩家，正式入驻</a>
          <span>Public</span>
        </ytcp-video-row>
        <ytcp-video-row>
          <a id="video-title" href="/video/OtherVideo99/edit">别的作品</a>
          <span>Draft</span>
        </ytcp-video-row>
      `,
    }));
    await page.goto('https://studio.youtube.com/channel/test/videos/upload');
    const result = await inspectKuaishouManager(page, {
      platform: 'youtube',
      content: { title: 'αgamer｜阿尔法玩家，正式入驻' },
      publishAt: '2026-07-31T19:26:00+08:00',
      timeoutMs: 1000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.scheduledState, false);
    assert.equal(result.evidence.publishedState, true);
    assert.equal(result.evidence.externalPostID, 'AbCdEf12345');
    assert.equal(result.evidence.postURL, 'https://www.youtube.com/shorts/AbCdEf12345');
  } finally {
    await browser.close();
  }
});

test('YouTube manager inspection ignores page-wide Public outside the matching row', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://studio.youtube.com/**', (route) => route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: `
        <span>Public</span>
        <ytcp-video-row>
          <a id="video-title" href="/video/AbCdEf12345/edit">αgamer | 阿尔法玩家，正式入驻</a>
          <span>Draft</span>
        </ytcp-video-row>
      `,
    }));
    await page.goto('https://studio.youtube.com/channel/test/videos/upload');
    const result = await inspectKuaishouManager(page, {
      platform: 'youtube',
      content: { title: 'αgamer｜阿尔法玩家，正式入驻' },
      publishAt: '2026-07-31T19:26:00+08:00',
      timeoutMs: 1000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.publishedState, false);
    assert.equal(result.evidence.externalPostID, '');
    assert.equal(result.evidence.postURL, '');
  } finally {
    await browser.close();
  }
});

test('YouTube manager inspection checks the Shorts tab when Videos has no matching row', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://studio.youtube.com/**', (route) => route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: `
        <button id="shorts">Shorts</button>
        <article>
          <a id="video-title">αgamer | 阿尔法玩家，正式入驻</a>
          <span>Draft</span>
        </article>
        <article id="scheduled" hidden>
          <a id="video-title">αgamer | 阿尔法玩家，正式入驻</a>
          <span>Scheduled</span>
        </article>
        <script>
          document.querySelector('#shorts').onclick = () => {
            history.pushState({}, '', '/channel/test/videos/short');
            document.querySelector('#scheduled').hidden = false;
          };
        </script>
      `,
    }));
    await page.goto('https://studio.youtube.com/channel/test/videos/upload');
    const result = await inspectKuaishouManager(page, {
      platform: 'youtube',
      content: { title: 'αgamer｜阿尔法玩家，正式入驻' },
      publishAt: '2026-07-31T19:26:00+08:00',
      timeoutMs: 5000,
    });
    assert.equal(result.status, 'inspected', JSON.stringify(result));
    assert.equal(result.location, 'https://studio.youtube.com/channel/test/videos/short');
    assert.equal(result.evidence.matchingTitle, true);
    assert.equal(result.evidence.scheduledState, true);
  } finally {
    await browser.close();
  }
});

test('YouTube acknowledges its post-submit checks reminder and records the scheduled signal', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="done-button">Schedule</button>
      <p>Checks complete. No issues found.</p>
      <section role="dialog" id="checking" hidden>
        <h2>We’re still checking your content</h2>
        <button id="got-it">Got it</button>
      </section>
      <section id="scheduled" hidden>Video scheduled</section>
      <script>
        window.finalClicks = 0;
        window.acknowledgementClicks = 0;
        document.querySelector('#done-button').onclick = () => {
          window.finalClicks += 1;
          document.querySelector('#checking').hidden = false;
        };
        document.querySelector('#got-it').onclick = () => {
          window.acknowledgementClicks += 1;
          document.querySelector('#checking').hidden = true;
          document.querySelector('#scheduled').hidden = false;
        };
      </script>
    `);
    const result = await submitPreparedPage(page, {
      ...validInput(),
      action: 'submit',
      allowSubmit: true,
      platform: 'youtube',
      content: { type: 'video', title: 'YouTube title', caption: '', body: '', tags: [] },
      media: [],
    }, {}, {
      ...PLATFORM_RULES.youtube,
      entryFlows: {},
      composerUrl: '',
      requiredURLPrefixes: [],
    });
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(result.errorCode, '');
    assert.equal(result.evidence.postSubmitReminderAcknowledged, true);
    assert.equal(result.evidence.platformSubmissionSignal, 'Video scheduled');
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
    assert.equal(await page.evaluate(() => window.acknowledgementClicks), 1);
  } finally {
    await browser.close();
  }
});

function launchChromium() {
  const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
  return chromium.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
}

function fileSHA256(filePath) {
  return crypto.createHash('sha256').update(fs.readFileSync(filePath)).digest('hex');
}

function topicPickerFixture(triggerLabel, highlighted = true) {
  return `
    <button id="topic-trigger">${triggerLabel}</button>
    <div id="topic-options" class="topic-suggest" role="listbox"></div>
    <script>
      const topicBody = document.querySelector('#body');
      const topicOptions = document.querySelector('#topic-options');
      document.querySelector('#topic-trigger').onclick = () => {
        topicBody.focus();
        topicBody.append(document.createTextNode('#'));
        const selection = window.getSelection();
        const range = document.createRange();
        range.selectNodeContents(topicBody);
        range.collapse(false);
        selection.removeAllRanges();
        selection.addRange(range);
      };
      topicBody.addEventListener('input', () => {
        const topicTextNode = [...topicBody.childNodes].reverse()
          .find((node) => node.nodeType === Node.TEXT_NODE);
        const raw = topicTextNode?.textContent || '';
        const match = raw.match(/#([^#\\s]+)$/);
        topicOptions.innerHTML = match
          ? '<div role="option">#' + match[1] + ' 1万浏览</div>'
          : '';
        const option = topicOptions.querySelector('[role="option"]');
        if (!option) return;
        option.onclick = () => {
          const tag = match[1];
          topicTextNode.textContent = raw.slice(0, -match[0].length);
          const selected = document.createElement('span');
          selected.dataset.topicSelected = 'true';
          selected.contentEditable = 'false';
          ${highlighted ? "selected.style.color = 'rgb(22, 119, 255)';" : ''}
          selected.textContent = '#' + tag;
          topicBody.append(selected, document.createTextNode(' '));
          topicOptions.replaceChildren();
        };
      });
    </script>
  `;
}

function xhsNativeTopicPickerFixture(existingTopics = []) {
  const existing = existingTopics.map((tag) => `
    <a class="tiptap-topic" data-topic='${JSON.stringify({ id: `existing-${tag}`, name: tag })}'
      contenteditable="false" style="color: rgb(16, 77, 161)">#${tag}</a>
  `).join('');
  return `
    <button id="topicBtn">话题</button>
    <script>
      const xhsTopicBody = document.querySelector('#body');
      let xhsExistingTopicsInserted = false;
      document.querySelector('#topicBtn').addEventListener('click', () => {
        if (!xhsExistingTopicsInserted) {
          xhsTopicBody.insertAdjacentHTML('beforeend', ${JSON.stringify(existing)});
          xhsExistingTopicsInserted = true;
        }
        xhsTopicBody.focus();
        xhsTopicBody.append(document.createTextNode('#'));
        const selection = window.getSelection();
        const range = document.createRange();
        range.selectNodeContents(xhsTopicBody);
        range.collapse(false);
        selection.removeAllRanges();
        selection.addRange(range);
      });
      xhsTopicBody.addEventListener('input', () => {
        document.querySelector('#creator-editor-topic-container')?.remove();
        const topicTextNode = [...xhsTopicBody.childNodes].reverse()
          .find((node) => node.nodeType === Node.TEXT_NODE);
        const raw = topicTextNode?.textContent || '';
        const match = raw.match(/#([^#\\s]+)$/);
        if (!match) return;
        const container = document.createElement('div');
        container.id = 'creator-editor-topic-container';
        const option = document.createElement('div');
        option.className = 'item';
        option.innerHTML = '<span class="name">#' + match[1] + '</span>';
        option.addEventListener('click', () => {
          topicTextNode.textContent = raw.slice(0, -match[0].length);
          const selected = document.createElement('a');
          selected.className = 'tiptap-topic';
          selected.dataset.topic = JSON.stringify({ id: 'selected-' + match[1], name: match[1] });
          selected.contentEditable = 'false';
          selected.style.color = 'rgb(16, 77, 161)';
          selected.textContent = '#' + match[1];
          topicTextNode.after(selected, document.createTextNode(' '));
          container.remove();
        });
        container.append(option);
        document.body.append(container);
      });
    </script>
  `;
}

function xhsLivePolicyLandmarks({
  duplicateDuet = false,
  duplicateSchedule = false,
  aiText = '添加内容类型声明',
  scheduleChecked = false,
} = {}) {
  const switchCard = (label, id, checked = false) => `
    <div class="custom-switch-wrapper">
      <div class="custom-switch-text-content"><span>${label}</span></div>
      <div class="custom-switch-switch">
        <button id="${id}-switch" class="d-switch-simulator ${checked ? 'checked' : 'unchecked'}"></button>
        <input id="${id}-state" type="checkbox" ${checked ? 'checked' : ''} hidden>
      </div>
    </div>
  `;
  return `
    <div class="permission-card-select"><div class="d-select-content">公开可见</div></div>
    ${switchCard('允许合拍', 'duet')}
    ${duplicateDuet ? switchCard('允许合拍', 'duet-duplicate') : ''}
    ${switchCard('允许正文复制', 'copy')}
    ${switchCard('定时发布', 'schedule', scheduleChecked)}
    ${duplicateSchedule ? switchCard('定时发布', 'schedule-duplicate', scheduleChecked) : ''}
    <div class="address-card-select"><div id="xhs-location-empty" class="d-select-placeholder">添加地点</div></div>
    <div class="group-card-select"><div id="xhs-group-empty" class="d-select-placeholder">选择群聊</div></div>
    <div class="collection-plugin-wrapper"><div id="xhs-collection-empty" class="collection-plugin-content-title">加入合集</div></div>
    <div class="setting-card-wrapper"><div id="xhs-related-media-empty" class="setting-card-text">引用笔记</div></div>
    <div class="publish-page-content-setting-content">
      <div class="custom-select-44"><div class="d-select-content">${aiText}</div></div>
    </div>
  `;
}

function xhsScopedImageComposerFixture(markup) {
  return xhsClosedShadowSubmitFixture(`
    <main class="publish-page">
      ${markup}
      <div id="xhs-upload-preview"></div>
    </main>
    <script>
      document.querySelector(".publish-page input[type='file'][accept*='image' i]")
        ?.addEventListener('change', (event) => {
          const preview = document.querySelector('#xhs-upload-preview');
          preview.replaceChildren();
          for (const file of event.currentTarget.files) {
            const image = document.createElement('img');
            image.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
            image.width = 320;
            image.height = 420;
            image.dataset.fileName = file.name;
            preview.append(image);
          }
        });
    </script>
  `);
}

function xhsClosedShadowSubmitFixture(markup) {
  return `
    ${markup}
    <script>
      (() => {
        const controls = new WeakMap();
        const sync = (host, submits) => {
          const disabled = host.getAttribute('aria-disabled')
            ?? host.getAttribute('submit-disabled')
            ?? 'false';
          const busy = host.getAttribute('submit-loading')
            ?? host.getAttribute('aria-busy')
            ?? 'false';
          for (const submit of submits) {
            submit.setAttribute('aria-disabled', disabled);
            submit.setAttribute('aria-busy', busy);
          }
        };
        const upgrade = (host) => {
          if (controls.has(host)) return;
          host.setAttribute('is-publish', host.getAttribute('is-publish') ?? 'true');
          host.setAttribute('submit-text', host.getAttribute('submit-text') ?? '发布');
          host.setAttribute('submit-disabled', host.getAttribute('submit-disabled')
            ?? host.getAttribute('aria-disabled')
            ?? 'false');
          host.setAttribute('submit-loading', host.getAttribute('submit-loading')
            ?? host.getAttribute('aria-busy')
            ?? 'false');
          const shadow = host.attachShadow({ mode: 'closed' });
          const wrapper = document.createElement('div');
          wrapper.className = 'publish-page-publish-btn';
          const leave = document.createElement('button');
          leave.textContent = '暂存离开';
          wrapper.append(leave);
          const publishCount = Number(host.getAttribute('data-publish-count') ?? '1');
          const submits = [];
          for (let index = 0; index < publishCount; index += 1) {
            const submit = document.createElement('button');
            submit.className = 'ce-btn bg-red';
            submit.textContent = host.getAttribute('submit-text');
            submit.addEventListener('click', (event) => {
              window.xhsInnerSubmitClicks = (window.xhsInnerSubmitClicks || 0) + 1;
              if (event.isTrusted) {
                window.xhsInnerSubmitTrustedClicks = (window.xhsInnerSubmitTrustedClicks || 0) + 1;
              }
            });
            wrapper.append(submit);
            submits.push(submit);
          }
          sync(host, submits);
          shadow.append(wrapper);
          controls.set(host, submits);
        };
        const scan = (root) => {
          if (root.matches?.('xhs-publish-btn')) upgrade(root);
          root.querySelectorAll?.('xhs-publish-btn').forEach(upgrade);
        };
        const observer = new MutationObserver((records) => {
          for (const record of records) {
            if (record.type === 'attributes') {
              const submits = controls.get(record.target);
              if (submits) sync(record.target, submits);
              continue;
            }
            record.addedNodes.forEach((node) => {
              if (node.nodeType === Node.ELEMENT_NODE) scan(node);
            });
          }
        });
        observer.observe(document.documentElement, {
          attributes: true,
          attributeFilter: ['aria-busy', 'aria-disabled', 'submit-disabled', 'submit-loading'],
          childList: true,
          subtree: true,
        });
        scan(document);
      })();
    </script>
  `;
}

function xhsFormalSettings() {
  return {
    location: '',
    collection: '',
    group: '',
    declare_original: false,
    content_source: 'ai',
    related_media: '',
    visibility: 'public',
    comment_enabled: true,
    allow_duet: false,
    allow_copy: false,
  };
}

function locationPickerFixture(commitSelection = true) {
  return `
    <div class="post-position-wrap">
      <div class="position-display-wrap"><span class="location-name"></span></div>
      <input id="location" role="combobox" aria-expanded="false" placeholder="请输入位置">
      <div id="location-options" role="listbox" hidden>
        <div id="location-first" role="option" aria-selected="false">龙湖滨江天街（江汉路店） 杭州市滨江区</div>
        <div id="location-second" role="option" aria-selected="false">杭州滨江天街北门 杭州市滨江区</div>
      </div>
    </div>
    <script>
      window.locationOptionClicks = [];
      const locationInput = document.querySelector('#location');
      const locationOptions = document.querySelector('#location-options');
      locationInput.addEventListener('input', () => {
        locationInput.setAttribute('aria-expanded', 'true');
        locationOptions.hidden = false;
      });
      for (const option of locationOptions.querySelectorAll('[role="option"]')) {
        option.addEventListener('click', () => {
          window.locationOptionClicks.push(option.id);
          ${commitSelection ? `
            option.setAttribute('aria-selected', 'true');
            locationInput.value = option.textContent.trim();
            document.querySelector('.location-name').textContent = option.textContent.trim();
            locationInput.setAttribute('aria-expanded', 'false');
            locationOptions.hidden = true;
          ` : ''}
        });
      }
    </script>
  `;
}

function douyinLiveGraphTextComposerFixture() {
  return `
    <input id="title" placeholder="添加作品标题">
    <div id="body" contenteditable="true"></div>
    <input id="images" type="file" accept="image/png" multiple>
    <div id="upload-preview"></div>
    ${topicPickerFixture('#添加话题')}

    <fieldset id="visibility">
      <legend>谁可以看</legend>
      <label><input type="checkbox" name="visibility" checked>公开</label>
      <label><input type="checkbox" name="visibility">好友可见</label>
      <label><input type="checkbox" name="visibility">仅自己可见</label>
    </fieldset>
    <fieldset id="download">
      <legend>保存权限</legend>
      <label><input type="checkbox" name="download" checked>允许</label>
      <label><input type="checkbox" name="download">不允许</label>
    </fieldset>
    <fieldset id="publish-time">
      <legend>发布时间</legend>
      <label><input type="checkbox" name="publish-time" checked>立即发布</label>
      <label><input type="checkbox" name="publish-time">定时发布</label>
      <input id="scheduled-at" type="text" placeholder="日期和时间"
        format="yyyy-MM-dd HH:mm" hidden>
    </fieldset>

    <div class="collection-setting">
      <span class="semi-select-selection-text">不选择合集</span>
    </div>
    <div class="hotspot-setting">
      <span class="semi-select-selection-text">点击输入热点词</span>
    </div>
    <div class="music-setting">
      <span class="desc-live">点击添加合适作品风格音乐</span>
    </div>

    <button id="declaration-trigger" class="selectText-XSrMFZ">请选择自主声明</button>
    <section id="declaration-dialog" role="dialog" hidden>
      <label><input type="radio" name="declaration">内容由AI生成</label>
      <button id="declaration-confirm">确定</button>
    </section>

    <button id="publish-decoy" disabled>发布设置</button>
    <button id="publish">发布</button>
    <script>
      window.submitClicks = 0;
      window.declarationConfirmClicks = 0;
      const makeExclusive = (selector) => {
        const controls = [...document.querySelectorAll(selector)];
        for (const control of controls) {
          control.addEventListener('change', () => {
            if (!control.checked) return;
            for (const other of controls) other.checked = other === control;
          });
        }
      };
      makeExclusive('input[name="visibility"]');
      makeExclusive('input[name="download"]');
      makeExclusive('input[name="publish-time"]');
      document.querySelector('#images').addEventListener('change', (event) => {
        const preview = document.querySelector('#upload-preview');
        preview.replaceChildren();
        for (const file of event.currentTarget.files) {
          const image = document.createElement('img');
          image.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
          image.width = 320;
          image.height = 420;
          image.dataset.fileName = file.name;
          preview.append(image);
        }
      });
      const schedule = [...document.querySelectorAll('input[name="publish-time"]')]
        .find((control) => control.labels[0].innerText.trim() === '定时发布');
      schedule.addEventListener('change', () => {
        document.querySelector('#scheduled-at').hidden = !schedule.checked;
      });
      document.querySelector('#declaration-trigger').onclick = () => {
        document.querySelector('#declaration-dialog').hidden = false;
      };
      document.querySelector('#declaration-confirm').onclick = () => {
        window.declarationConfirmClicks += 1;
        const selected = document.querySelector('input[name="declaration"]:checked');
        if (!selected) return;
        document.querySelector('#declaration-trigger').textContent = selected.labels[0].innerText.trim();
        document.querySelector('#declaration-dialog').hidden = true;
      };
      document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
    </script>
  `;
}

function validInput() {
  return {
    schemaVersion: 1,
    action: 'prepare',
    platform: 'csdn',
    cdpUrl: 'http://127.0.0.1:15106/',
    content: { type: 'article', title: 'title', caption: '', body: 'body', tags: [] },
    media: [],
    settings: {},
    allowSubmit: false,
    timeoutMs: 30000,
  };
}

function douyinLiveSubmitInput(imagePath) {
  return {
    ...validInput(),
    action: 'submit',
    allowSubmit: true,
    platform: 'douyin',
    content: {
      type: 'graph_text',
      title: '图文标题',
      caption: '正文保留 #正文示例\n#αgamer #阿尔法玩家 #游戏内容',
      body: '',
      tags: ['αgamer', '阿尔法玩家', '游戏内容'],
    },
    media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: fileSHA256(imagePath) }],
    settings: {
      collection: '',
      extension_type: 'none',
      location: '',
      hotspot: '',
      music: '',
      allow_download: false,
      visibility: 'public',
      comment_enabled: true,
      self_declaration: 'ai',
    },
    publishAt: '2030-07-24T18:30:00+08:00',
  };
}

test('strict input accepts the reviewed controlled-platform contract', () => {
  assert.equal(Object.keys(PLATFORM_RULES).length, 8);
  assert.equal(validateInput(validInput()).platform, 'csdn');
});

test('inspect accepts an empty media and settings payload without publish validation', () => {
  const input = {
    schemaVersion: 1,
    action: 'inspect',
    platform: 'kuaishou',
    cdpUrl: 'http://127.0.0.1:15106/',
    content: {},
    media: [],
    settings: {},
    allowSubmit: false,
    timeoutMs: 30000,
  };
  assert.equal(validateInput(input).action, 'inspect');
});

test('passive inspection returns only concise visible setting labels, never editor text', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div contenteditable="true">这是编辑器正文，不能进入设置诊断。</div>
      <button>作品声明</button>
      <label><input type="checkbox">允许下载此作品</label>
      <label><input type="radio" name="scope" checked>所有人可见</label>
      <label><input type="radio" name="scope">仅自己可见</label>
      <button>定时发布</button>
    `);
    const inspection = await inspectPage(page, PLATFORM_RULES.kuaishou);
    assert.deepEqual(inspection.settingLabels, ['作品声明', '允许下载此作品', '所有人可见', '仅自己可见', '定时发布']);
    assert.ok(!JSON.stringify(inspection.settingLabels).includes('编辑器正文'));
    assert.equal(Object.hasOwn(inspection, 'controls'), false);

    await page.setContent('<button class="article-bar__title-display">无标题</button><input id="title-control" class="editor-title" placeholder="填写标题" value="不应泄露">');
    const titleInspection = await inspectPage(page, {
      ...PLATFORM_RULES.kuaishou,
      titleActivationSelectors: ['.article-bar__title-display'],
      titleActivationNames: ['无标题'],
    });
    assert.deepEqual(titleInspection.titleCandidates, [{
      tag: 'input', id: 'title-control', class: 'editor-title', placeholder: '填写标题', ariaLabel: '', readOnly: false, disabled: false, contentEditable: false,
    }]);
    assert.equal(titleInspection.formFieldCandidates.length, 1);
    assert.deepEqual(
      (({ tag, id, class: className, placeholder, ariaLabel, readOnly, disabled, contentEditable }) => ({
        tag, id, class: className, placeholder, ariaLabel, readOnly, disabled, contentEditable,
      }))(titleInspection.formFieldCandidates[0]),
      {
        tag: 'input', id: 'title-control', class: 'editor-title',
        placeholder: '填写标题', ariaLabel: '', readOnly: false, disabled: false, contentEditable: false,
      },
    );
    assert.equal(titleInspection.composerReadiness.titleActivation, true);
    assert.ok(!JSON.stringify(titleInspection.titleCandidates).includes('不应泄露'));
    assert.ok(!JSON.stringify(titleInspection.formFieldCandidates).includes('不应泄露'));
    assert.equal(inspection.composerReadiness.schedule, true);
  } finally {
    await browser.close();
  }
});

test('Kuaishou settings reveal clicks only the exact publish-settings section', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="settings">发布设置</button>
      <div id="settings-panel" hidden><label><input type="checkbox">允许下载此作品</label></div>
      <script>document.querySelector('#settings').onclick = () => { document.querySelector('#settings-panel').hidden = false; };</script>
    `);
    assert.equal(await revealReviewSettings(page, 'kuaishou'), '发布设置');
    assert.deepEqual((await inspectPage(page)).settingLabels, ['发布设置', '允许下载此作品']);
    assert.equal(await revealReviewSettings(page, 'douyin'), '');
  } finally {
    await browser.close();
  }
});

test('TikTok settings reveal expands only the exact show-more section', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="more">显示更多</button>
      <button id="publish">发布</button>
      <div id="settings" hidden>允许用户：评论、合拍、拼接、下载</div>
      <script>
        document.querySelector('#more').onclick = () => {
          document.querySelector('#more').hidden = true;
          document.querySelector('#settings').hidden = false;
        };
      </script>
    `);
    assert.equal(await revealReviewSettings(page, 'tiktok'), '显示更多');
    assert.equal(await page.locator('#settings').isVisible(), true);
  } finally {
    await browser.close();
  }
});

test('CSDN settings reveal opens only the first-stage options dialog', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button class="btn btn-publish">发布文章</button>
      <section class="publish-article-modal__body" hidden><label><input type="radio">原创</label><label><input type="checkbox">同时备份到 GitCode</label><button class="btn-b-red">发布文章</button></section>
      <script>
        window.finalClicks = 0;
        window.advanceClicks = 0;
        document.querySelector('.btn.btn-publish').onclick = () => { window.advanceClicks += 1; document.querySelector('section').hidden = false; };
        document.querySelector('.btn-b-red').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    assert.equal(await revealReviewSettings(page, 'csdn'), '发布选项');
    assert.deepEqual((await inspectPage(page)).settingLabels, ['发布文章', '原创', '同时备份到 GitCode']);
    assert.equal(await page.evaluate(() => window.finalClicks), 0);
  } finally {
    await browser.close();
  }
});

test('resume topics never reaches media inputs or the final publish control', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: `
        <input id="title" placeholder="标题">
        <div id="body" contenteditable="true"></div>
        ${topicPickerFixture('#添加话题')}
        <input id="media" type="file" accept="video/mp4">
        <button id="publish">发布</button>
        <script>
          window.mediaClicks = 0;
          window.publishClicks = 0;
          document.querySelector('#media').addEventListener('click', () => { window.mediaClicks += 1; });
          document.querySelector('#publish').addEventListener('click', () => { window.publishClicks += 1; });
        </script>
      `,
    }));
    await page.goto('https://creator.douyin.com/creator-micro/content/post/video');

    const result = await resumeTopics(page, {
      platform: 'douyin',
      timeoutMs: 1500,
      content: { type: 'video', title: '阿尔法玩家正式入驻', caption: '你好，这里是 αgamer。', body: '', tags: ['αgamer'] },
    });

    assert.equal(result.status, 'resumed', JSON.stringify(result));
    assert.deepEqual(result.evidence.selectedTopics, ['αgamer']);
    assert.equal(await page.locator('#title').inputValue(), '阿尔法玩家正式入驻');
    assert.equal(await page.evaluate(() => window.mediaClicks), 0);
    assert.equal(await page.evaluate(() => window.publishClicks), 0);
  } finally {
    await browser.close();
  }
});

test('immutable media payload keeps the SHA-verified bytes after the source path changes', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-immutable-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    const frozenBytes = Buffer.from('frozen-image-bytes');
    fs.writeFileSync(imagePath, frozenBytes);
    const bundle = immutableFilePayloadBundle([{
      path: imagePath,
      kind: 'image',
      role: 'content',
      position: 0,
      sha256: fileSHA256(imagePath),
    }]);
    fs.writeFileSync(imagePath, Buffer.from('changed-image-data'));

    assert.ok(bundle);
    assert.deepEqual(bundle.payloads[0].buffer, frozenBytes);
    assert.equal(bundle.descriptors[0].size, frozenBytes.length);
    assert.equal(
      bundle.sha256s[0],
      crypto.createHash('sha256').update(frozenBytes).digest('hex'),
    );
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('immutable media payload rejects bytes that do not match the frozen SHA', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-sha-mismatch-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    assert.equal(immutableFilePayloadBundle([{
      path: imagePath,
      kind: 'image',
      role: 'content',
      position: 0,
      sha256: '0'.repeat(64),
    }]), null);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('article input accepts one explicit image cover and rejects ambiguous cover media', () => {
  const article = validInput();
  article.media = [{
    path: '/tmp/article-cover.png',
    kind: 'image',
    role: 'cover',
    position: 0,
    sha256: 'a'.repeat(64),
  }];
  assert.equal(validateInput(article).media[0].role, 'cover');

  const duplicateCover = structuredClone(article);
  duplicateCover.media.push({
    path: '/tmp/article-cover-2.png',
    kind: 'image',
    role: 'cover',
    position: 1,
    sha256: 'b'.repeat(64),
  });
  assert.throws(() => validateInput(duplicateCover), /MEDIA_INVALID/);

  const videoCover = structuredClone(article);
  videoCover.media[0].kind = 'video';
  assert.throws(() => validateInput(videoCover), /MEDIA_INVALID/);
});

test('TikTok and YouTube accept only their reviewed content families', () => {
  const youtube = validInput();
  youtube.platform = 'youtube';
  youtube.content = { type: 'video', title: 'YouTube title', caption: '', body: '', tags: ['AlphaNexus'] };
  youtube.media = [{ path: '/tmp/video.mp4', kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64) }];
  assert.equal(validateInput(youtube).content.type, 'video');

  const tiktok = structuredClone(youtube);
  tiktok.platform = 'tiktok';
  tiktok.content = { type: 'graph_text', title: 'TikTok photo', caption: 'Photo description', body: '', tags: ['AlphaNexus'] };
  tiktok.media = [{ path: '/tmp/image.png', kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }];
  assert.throws(() => validateInput(tiktok), /CONTENT_TYPE_UNSUPPORTED/);
  tiktok.content = { type: 'video', title: 'TikTok video', caption: 'Video description', body: '', tags: ['AlphaNexus'] };
  tiktok.media = [{ path: '/tmp/video.mp4', kind: 'video', role: 'video', position: 0, sha256: 'b'.repeat(64) }];
  tiktok.settings = { visibility: 'private', allow_comment: true, ai_generated: false, allow_download: false };
  assert.equal(validateInput(tiktok).settings.visibility, 'private');

  youtube.settings = {
    audience: 'not_made_for_kids',
    visibility: 'unlisted',
    playlist: '',
    allow_comments: true,
    altered_content: false,
  };
  assert.equal(validateInput(youtube).settings.audience, 'not_made_for_kids');

  const youtubeArticle = structuredClone(youtube);
  youtubeArticle.content = { type: 'article', title: 'not supported', caption: '', body: 'body', tags: [] };
  youtubeArticle.media = [];
  assert.throws(() => validateInput(youtubeArticle), /CONTENT_TYPE_UNSUPPORTED/);
});

test('YouTube fixture fills media metadata tags audience cover and reaches the final review control', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-youtube-'));
  try {
    const videoPath = path.join(directory, 'video.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <div class="dialog-content style-scope ytcp-uploads-dialog">
      <input id="video-file" type="file" accept="video/*">
      <span id="upload-status">Uploading 73% ...</span>
      <img id="video-preview" width="120" height="100" hidden>
      <div id="title-textarea"><div id="textbox" contenteditable="true"></div></div>
      <div id="description-textarea"><div id="textbox" contenteditable="true"></div></div>
      <button id="upload-thumbnail" hidden>Upload thumbnail</button>
      <ytcp-video-metadata-editor-thumb><div id="thumbnail-container" hidden></div></ytcp-video-metadata-editor-thumb>
      <button id="more">Show more</button>
      <div id="tags" hidden><input aria-label="Tags"></div>
      <button id="not-kids" role="radio" aria-checked="false">No, it's not made for kids</button>
      <div id="enablement-state-select"><ytcp-dropdown-trigger>Comments On</ytcp-dropdown-trigger></div>
      <tp-yt-paper-radio-button id="ai-yes" role="radio" aria-label="Yes, AI was used" aria-checked="false">Yes, AI was used</tp-yt-paper-radio-button>
      <tp-yt-paper-radio-button id="ai-no" role="radio" aria-label="No, AI wasn’t used" aria-checked="false">No, AI wasn’t used</tp-yt-paper-radio-button>
      <button id="next">Next</button>
      <button id="private" role="radio" aria-checked="false" hidden>Private</button>
      <button id="unlisted" role="radio" aria-checked="false" hidden>Unlisted</button>
      <button id="public" role="radio" aria-checked="false" hidden>Public</button>
      <button id="save" hidden disabled>Save</button>
      <div id="tutorial" role="dialog" hidden><button id="tutorial-next">Next</button></div>
      </div>
      <script>
        let step = 0;
        document.querySelector('#video-file').onchange = () => {
          setTimeout(() => {
            document.querySelector('#upload-status').hidden = true;
            document.querySelector('#upload-thumbnail').hidden = false;
            const preview = document.querySelector('#video-preview');
            preview.src = 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" width="120" height="100"></svg>';
            preview.hidden = false;
          }, 300);
        };
        document.querySelector('#upload-thumbnail').onclick = () => {
          const input = document.createElement('input');
          input.type = 'file';
          input.accept = 'image/*';
          input.onchange = () => {
            const preview = document.querySelector('#thumbnail-container');
            preview.style.backgroundImage = 'url("blob:accepted-thumbnail")';
            preview.style.width = '120px';
            preview.style.height = '100px';
            preview.hidden = false;
          };
          document.body.appendChild(input);
          input.click();
          setTimeout(() => input.remove(), 0);
        };
        document.querySelector('#not-kids').onclick = (event) => { event.currentTarget.setAttribute('aria-checked', 'true'); };
        document.querySelector('#ai-yes').onclick = () => {
          document.querySelector('#ai-yes').setAttribute('aria-checked', 'true');
          document.querySelector('#ai-no').setAttribute('aria-checked', 'false');
        };
        document.querySelector('#ai-no').onclick = () => {
          document.querySelector('#ai-yes').setAttribute('aria-checked', 'false');
          document.querySelector('#ai-no').setAttribute('aria-checked', 'true');
        };
        for (const id of ['private', 'unlisted', 'public']) {
          document.querySelector('#' + id).onclick = (event) => {
            for (const sibling of ['private', 'unlisted', 'public']) document.querySelector('#' + sibling).setAttribute('aria-checked', 'false');
            event.currentTarget.setAttribute('aria-checked', 'true');
          };
        }
        document.querySelector('#more').onclick = () => { document.querySelector('#tags').hidden = false; };
        document.querySelector('[aria-label="Tags"]').onkeydown = (event) => {
          if (event.key !== 'Enter') return;
          const chip = document.createElement('ytcp-chip');
          chip.className = 'chip style-scope ytcp-chip-bar';
          chip.setAttribute('role', 'button');
          chip.setAttribute('aria-label', event.currentTarget.value + '. Press enter to edit');
          const text = document.createElement('div');
          text.id = 'chip-text';
          text.className = 'text style-scope ytcp-chip';
          text.textContent = event.currentTarget.value;
          chip.appendChild(text);
          document.body.appendChild(chip);
          event.currentTarget.value = '';
        };
        document.querySelector('#next').onclick = () => {
          step += 1;
          document.querySelector('ytcp-video-metadata-editor-thumb')?.remove();
          if (step === 3) {
            document.querySelector('#private').hidden = false;
            document.querySelector('#unlisted').hidden = false;
            document.querySelector('#public').hidden = false;
            document.querySelector('#save').hidden = false;
            document.querySelector('#tutorial').hidden = false;
            setTimeout(() => { document.querySelector('#save').disabled = false; }, 8000);
          }
        };
        document.querySelector('#tutorial-next').onclick = () => { document.querySelector('#tutorial').hidden = true; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'youtube',
      content: { type: 'video', title: 'YouTube title', caption: '', body: 'YouTube description', tags: ['AlphaNexus'] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'b'.repeat(64) },
      ],
      settings: {
        audience: 'not_made_for_kids', visibility: 'unlisted', playlist: '',
        allow_comments: true, altered_content: false,
      },
    };
    const result = await prepareForm(page, input, PLATFORM_RULES.youtube);
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.titleFilled, true);
    assert.equal(result.evidence.bodyFilled, true);
    assert.equal(result.evidence.videoUploaded, true);
    assert.equal(result.evidence.coverUploaded, true);
    assert.equal(await page.locator('#ai-no').getAttribute('aria-checked'), 'true');
    assert.deepEqual(result.evidence.missingRequiredControls, []);
    assert.equal(await page.locator('#tutorial').isVisible(), false);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('YouTube reviewed upload entry treats the trusted Studio page as its in-place upload surface', () => {
  assert.deepEqual(
    reviewedEntryReachPrefixes(PLATFORM_RULES.youtube.entryFlows.video),
    ['https://studio.youtube.com/'],
  );
  assert.deepEqual(
    reviewedEntryReachPrefixes(PLATFORM_RULES.douyin.entryFlows.graph_text),
    ['https://creator.douyin.com/creator-micro/content/upload'],
  );
});

test('YouTube verifies the empty playlist default without opening its dropdown', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button>Select</button>
      <ytcp-video-metadata-playlists>
        <ytcp-dropdown-trigger id="playlists" role="button">Select</ytcp-dropdown-trigger>
        <div id="playlist-menu" hidden>No playlists available</div>
      </ytcp-video-metadata-playlists>
      <script>
        window.playlistClicks = 0;
        document.querySelector('ytcp-video-metadata-playlists ytcp-dropdown-trigger').onclick = () => {
          window.playlistClicks += 1;
          document.querySelector('#playlist-menu').hidden = false;
        };
      </script>
    `);

    const result = await applySettings(page, {
      ...validInput(),
      platform: 'youtube',
      content: { type: 'video', title: 'YouTube title', caption: '', body: '', tags: [] },
      settings: { playlist: '' },
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.verified, ['playlist']);
    assert.deepEqual(result.unverified, []);
    assert.equal(await page.evaluate(() => window.playlistClicks), 0);
    assert.equal(await page.locator('#playlist-menu').isVisible(), false);
  } finally {
    await browser.close();
  }
});

test('YouTube Shorts uses the reviewed native first-frame fallback when Studio forbids desktop thumbnails', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <p>You can change the thumbnail in the YouTube mobile app</p>
    `);
    const result = await applyNativeVideoCover(
      page,
      [{ path: '/tmp/cover.png', kind: 'image', role: 'cover', position: 0, sha256: 'a'.repeat(64) }],
      PLATFORM_RULES.youtube.coverControl,
    );
    assert.deepEqual(result, { applied: true, method: 'native_first_frame' });
  } finally {
    await browser.close();
  }
});

test('YouTube commits its reviewed Visibility date and 12-hour time before Schedule', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="schedule-choice" role="radio" aria-checked="false">Schedule</button>
      <ytcp-datetime-picker>
        <ytcp-text-dropdown-trigger><ytcp-dropdown-trigger id="datepicker-trigger">Jul 31, 2026</ytcp-dropdown-trigger></ytcp-text-dropdown-trigger>
        <form onsubmit="return false"><tp-yt-paper-input><input value="12:00 AM"></tp-yt-paper-input></form>
      </ytcp-datetime-picker>
      <button id="done-button">Schedule</button>
      <script>
        const timeInput = document.querySelector('ytcp-datetime-picker input');
        let committedTime = timeInput.value;
        window.timeKeypresses = 0;
        window.timeInputEvents = 0;
        timeInput.addEventListener('input', (event) => {
          if (!event.isTrusted) {
            event.currentTarget.value = committedTime;
            return;
          }
          window.timeInputEvents += 1;
        });
        timeInput.addEventListener('keydown', (event) => {
          if (event.key.length === 1) window.timeKeypresses += 1;
          if (event.key === 'Enter') committedTime = event.currentTarget.value;
        });
        timeInput.addEventListener('blur', (event) => { event.currentTarget.value = committedTime; });
        document.querySelector('#schedule-choice').onclick = (event) => {
          event.currentTarget.setAttribute('aria-checked', 'true');
          setTimeout(() => {
            timeInput.value = '12:00 AM';
            committedTime = timeInput.value;
          }, 300);
        };
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'youtube',
      publishAt: '2026-07-31T12:07:00+08:00',
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.unverified, []);
    assert.deepEqual(result.applied, ['publish.time']);
    assert.equal(result.value, '2026-07-31 12:07:00');
    assert.equal(result.requestedValue, '2026-07-31 12:07:00');
    assert.equal(result.fallbackUsed, false);
    assert.equal(await page.locator('ytcp-datetime-picker form input').inputValue(), '12:07 PM');
    assert.equal(await page.evaluate(() => window.timeInputEvents > 0), true);
    assert.equal(await page.locator('#schedule-choice').getAttribute('aria-checked'), 'true');
  } finally {
    await browser.close();
  }
});

test('YouTube accepts its unique visible scheduler when Studio exposes no radio state', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="schedule-choice">Schedule</button>
      <ytcp-visibility-scheduler id="scheduler"></ytcp-visibility-scheduler>
      <script>
        document.querySelector('#schedule-choice').onclick = () => {
          document.querySelector('#scheduler').innerHTML = \`
            <ytcp-datetime-picker>
              <ytcp-text-dropdown-trigger><ytcp-dropdown-trigger>Jul 31, 2026</ytcp-dropdown-trigger></ytcp-text-dropdown-trigger>
              <form onsubmit="return false"><tp-yt-paper-input><input value="12:00 AM"></tp-yt-paper-input></form>
            </ytcp-datetime-picker>
          \`;
          const timeInput = document.querySelector('ytcp-datetime-picker input');
          let committedTime = timeInput.value;
          timeInput.addEventListener('input', (event) => { committedTime = event.currentTarget.value; });
          timeInput.addEventListener('blur', (event) => { event.currentTarget.value = committedTime; });
        };
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'youtube',
      publishAt: '2026-07-31T13:45:00+08:00',
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.unverified, []);
    assert.equal(result.value, '2026-07-31 13:45:00');
    assert.equal(await page.locator('ytcp-datetime-picker form input').inputValue(), '1:45 PM');
  } finally {
    await browser.close();
  }
});

test('YouTube waits through a long metadata autosave before committing its scheduled time', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="schedule-choice" role="radio" aria-checked="true">Schedule</button>
      <span id="saving">Saving changes...</span>
      <ytcp-datetime-picker>
        <ytcp-text-dropdown-trigger><ytcp-dropdown-trigger>Jul 31, 2026</ytcp-dropdown-trigger></ytcp-text-dropdown-trigger>
        <form onsubmit="return false"><tp-yt-paper-input><input value="12:00 AM"></tp-yt-paper-input></form>
      </ytcp-datetime-picker>
      <script>
        const saving = document.querySelector('#saving');
        const timeInput = document.querySelector('ytcp-datetime-picker input');
        let committedTime = timeInput.value;
        let autosaving = true;
        timeInput.addEventListener('input', (event) => {
          if (autosaving) {
            event.currentTarget.value = committedTime;
            return;
          }
          committedTime = event.currentTarget.value;
        });
        timeInput.addEventListener('blur', (event) => { event.currentTarget.value = committedTime; });
        setTimeout(() => {
          autosaving = false;
          saving.hidden = true;
        }, 16000);
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'youtube',
      publishAt: '2026-07-31T13:45:00+08:00',
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.unverified, []);
    assert.equal(result.value, '2026-07-31 13:45:00');
    assert.equal(await page.locator('ytcp-datetime-picker form input').inputValue(), '1:45 PM');
  } finally {
    await browser.close();
  }
});

test('YouTube requires the scheduled time to remain stable when the saving badge is inaccessible', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="schedule-choice" role="radio" aria-checked="true">Schedule</button>
      <ytcp-datetime-picker>
        <ytcp-text-dropdown-trigger><ytcp-dropdown-trigger>Jul 31, 2026</ytcp-dropdown-trigger></ytcp-text-dropdown-trigger>
        <form onsubmit="return false"><tp-yt-paper-input><input value="12:00 AM"></tp-yt-paper-input></form>
      </ytcp-datetime-picker>
      <script>
        const timeInput = document.querySelector('ytcp-datetime-picker input');
        let committedTime = timeInput.value;
        let autosaving = true;
        timeInput.addEventListener('input', (event) => {
          if (autosaving) {
            event.currentTarget.value = committedTime;
            return;
          }
          committedTime = event.currentTarget.value;
        });
        timeInput.addEventListener('blur', (event) => { event.currentTarget.value = committedTime; });
        setTimeout(() => { autosaving = false; }, 4000);
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'youtube',
      publishAt: '2026-07-31T13:45:00+08:00',
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.unverified, []);
    assert.equal(result.value, '2026-07-31 13:45:00');
    assert.equal(await page.locator('ytcp-datetime-picker form input').inputValue(), '1:45 PM');
  } finally {
    await browser.close();
  }
});

test('YouTube waits for the scheduled time input to become enabled', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="schedule-choice" role="radio" aria-checked="true">Schedule</button>
      <ytcp-datetime-picker>
        <ytcp-text-dropdown-trigger><ytcp-dropdown-trigger>Jul 31, 2026</ytcp-dropdown-trigger></ytcp-text-dropdown-trigger>
        <form onsubmit="return false"><tp-yt-paper-input><input disabled value="12:00 AM"></tp-yt-paper-input></form>
      </ytcp-datetime-picker>
      <script>
        const timeInput = document.querySelector('ytcp-datetime-picker input');
        let committedTime = timeInput.value;
        timeInput.addEventListener('input', (event) => { committedTime = event.currentTarget.value; });
        timeInput.addEventListener('blur', (event) => { event.currentTarget.value = committedTime; });
        setTimeout(() => { timeInput.disabled = false; }, 4000);
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'youtube',
      publishAt: '2026-07-31T13:45:00+08:00',
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.unverified, []);
    assert.equal(await page.locator('ytcp-datetime-picker form input').inputValue(), '1:45 PM');
  } finally {
    await browser.close();
  }
});

test('YouTube waits for its scheduled date and time controls to mount', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="schedule-choice" role="radio" aria-checked="true">Schedule</button>
      <div id="schedule-fields"></div>
      <script>
        setTimeout(() => {
          document.querySelector('#schedule-fields').innerHTML = \`
            <ytcp-datetime-picker>
              <ytcp-text-dropdown-trigger><ytcp-dropdown-trigger>Jul 31, 2026</ytcp-dropdown-trigger></ytcp-text-dropdown-trigger>
              <form onsubmit="return false"><tp-yt-paper-input><input value="12:00 AM"></tp-yt-paper-input></form>
            </ytcp-datetime-picker>
          \`;
          const timeInput = document.querySelector('ytcp-datetime-picker input');
          let committedTime = timeInput.value;
          timeInput.addEventListener('input', (event) => { committedTime = event.currentTarget.value; });
          timeInput.addEventListener('blur', (event) => { event.currentTarget.value = committedTime; });
        }, 4000);
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'youtube',
      publishAt: '2026-07-31T13:45:00+08:00',
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.unverified, []);
    assert.equal(await page.locator('ytcp-datetime-picker form input').inputValue(), '1:45 PM');
  } finally {
    await browser.close();
  }
});

test('YouTube closes its native time picker after the scheduled time is stable', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button role="radio" aria-checked="true">Schedule</button>
      <ytcp-visibility-scheduler>
        <ytcp-datetime-picker>
          <ytcp-text-dropdown-trigger><ytcp-dropdown-trigger>Jul 31, 2026</ytcp-dropdown-trigger></ytcp-text-dropdown-trigger>
          <form onsubmit="return false"><tp-yt-paper-input><input value="12:00 AM"></tp-yt-paper-input></form>
        </ytcp-datetime-picker>
      </ytcp-visibility-scheduler>
      <tp-yt-paper-dialog class="ytcp-time-of-day-picker" hidden>
        <div style="width:200px;height:120px">Choose time</div>
      </tp-yt-paper-dialog>
      <script>
        const timeInput = document.querySelector('ytcp-datetime-picker input');
        const picker = document.querySelector('.ytcp-time-of-day-picker');
        let committedTime = timeInput.value;
        timeInput.addEventListener('click', () => { picker.hidden = false; });
        timeInput.addEventListener('input', (event) => { committedTime = event.currentTarget.value; });
        timeInput.addEventListener('blur', (event) => { event.currentTarget.value = committedTime; });
        document.addEventListener('keydown', (event) => {
          if (event.key === 'Escape') picker.hidden = true;
        });
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'youtube',
      publishAt: '2026-07-31T19:15:00+08:00',
    }, PLATFORM_RULES.youtube);

    assert.deepEqual(result.unverified, []);
    assert.equal(await page.locator('ytcp-datetime-picker input').inputValue(), '7:15 PM');
    assert.equal(await page.locator('.ytcp-time-of-day-picker').isVisible(), false);
  } finally {
    await browser.close();
  }
});

test('YouTube submit-prepared ignores policy-check progress after the upload preview is stable', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-youtube-checks-progress-'));
  try {
    const videoPath = path.join(directory, 'launch-video.mp4');
    const coverPath = path.join(directory, 'launch-cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <section id="dialog">
        <img
          style="width:320px;height:180px"
          src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII="
        >
        <div role="progressbar"></div>
        <span>Checking 2% ... 10 minutes left</span>
      </section>
      <button id="done-button">Schedule</button>
      <script>
        window.finalClicks = 0;
        document.querySelector('#done-button').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      action: 'submit_prepared',
      allowSubmit: true,
      platform: 'youtube',
      content: { type: 'video', title: 'YouTube title', caption: '', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'b'.repeat(64) },
      ],
    };

    const result = await submitPreparedPage(page, input, {}, {
      ...PLATFORM_RULES.youtube,
      entryFlows: {},
      composerUrl: '',
      requiredURLPrefixes: [],
      uploadRootSelector: '#dialog',
      uploadCompletionStableMs: 0,
    });

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(result.evidence.submitClicked, true);
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('TikTok discards only the reviewed unsaved-test-draft prompt before a new upload', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <section id="stale">
        <p>过往编辑的视频未保存。继续编辑?</p>
        <button id="discard">放弃</button>
        <button>继续</button>
      </section>
      <button id="unrelated">放弃</button>
      <script>
        window.discardClicks = 0;
        document.querySelector('#discard').onclick = () => {
          window.discardClicks += 1;
          document.querySelector('#stale').hidden = true;
        };
      </script>
    `);

    assert.equal(await discardReviewedStaleDraft(page, PLATFORM_RULES.tiktok), true);
    assert.equal(await page.evaluate(() => window.discardClicks), 1);
    assert.equal(await page.locator('#stale').isVisible(), false);
  } finally {
    await browser.close();
  }
});

test('TikTok photo fixture uploads images and commits a native highlighted topic before review', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-tiktok-'));
  try {
    const imagePath = path.join(directory, 'photo.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input type="file" accept="image/*">
      <input placeholder="Add a title">
      <div id="caption" class="notranslate public-DraftEditor-content" contenteditable="true"></div>
      <button id="hashtag-trigger">Hashtags</button>
      <div id="topic-suggestions"></div>
      <button id="only-you" role="radio" aria-checked="false">Only you</button>
      <button role="switch" aria-label="Allow comments" aria-checked="true"></button>
      <button role="switch" aria-label="Allow Duet" aria-checked="true"></button>
      <button role="switch" aria-label="Allow Stitch" aria-checked="true"></button>
      <button role="switch" aria-label="AI-generated content" aria-checked="false"></button>
      <button id="post">Post</button>
      <div id="auto-check" role="dialog" hidden>
        <button id="cancel-auto-check">Cancel</button>
        <button>Enable</button>
      </div>
      <aside id="tutorial" hidden><button id="dismiss-tutorial">Got it</button></aside>
      <script>
        window.cancelAutoCheckClicks = 0;
        window.dismissTutorialClicks = 0;
        window.submitClicks = 0;
        document.querySelector('#post').onclick = () => { window.submitClicks += 1; };
        document.querySelector('#only-you').onclick = (event) => { event.currentTarget.setAttribute('aria-checked', 'true'); };
        document.querySelector("input[type='file']").addEventListener('change', () => {
          const preview = document.createElement('canvas');
          preview.width = 120;
          preview.height = 100;
          document.body.append(preview);
          document.querySelector('#auto-check').hidden = false;
          document.querySelector('#tutorial').hidden = false;
        });
        document.querySelector('#cancel-auto-check').onclick = () => {
          window.cancelAutoCheckClicks += 1;
          document.querySelector('#auto-check').hidden = true;
        };
        document.querySelector('#dismiss-tutorial').onclick = () => {
          window.dismissTutorialClicks += 1;
          document.querySelector('#tutorial').hidden = true;
        };
        const caption = document.querySelector('#caption');
        document.querySelector('#hashtag-trigger').onclick = () => {
          caption.append('#');
          caption.focus();
          const range = document.createRange();
          range.selectNodeContents(caption);
          range.collapse(false);
          const selection = window.getSelection();
          selection.removeAllRanges();
          selection.addRange(range);
        };
        caption.addEventListener('input', (event) => {
          const root = event.currentTarget;
          const text = root.innerText;
          const match = text.match(/#([^#\\s]+)$/u);
          if (!match) return;
          setTimeout(() => {
            const option = document.createElement('button');
            option.setAttribute('role', 'option');
            option.textContent = match[0];
            option.onclick = () => {
              root.textContent = text.slice(0, -match[0].length);
              const topic = document.createElement('a');
              topic.dataset.topic = JSON.stringify({ name: match[1] });
              topic.contentEditable = 'false';
              topic.style.color = 'rgb(0, 90, 220)';
              topic.textContent = match[0];
              root.appendChild(topic);
              root.append(' ');
              document.querySelector('#topic-suggestions').replaceChildren();
            };
            document.querySelector('#topic-suggestions').replaceChildren(option);
          }, 900);
        });
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'tiktok',
      content: { type: 'graph_text', title: 'TikTok photo', caption: 'Photo description', body: '', tags: ['AlphaNexus'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'c'.repeat(64) }],
      settings: { visibility: 'private', allow_comment: true, allow_duet: true, allow_stitch: true, ai_generated: false },
    };
    const result = await prepareForm(page, input, PLATFORM_RULES.tiktok);
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.selectedTopics, ['AlphaNexus']);
    assert.equal(result.evidence.contentImagesUploaded, true);
    assert.deepEqual(result.evidence.missingRequiredControls, []);
    assert.equal(await page.evaluate(() => window.cancelAutoCheckClicks), 1);
    assert.equal(await page.evaluate(() => window.dismissTutorialClicks), 1);
    const submitted = await submitPreparedPage(page, {
      ...input,
      action: 'submit',
      allowSubmit: true,
    }, result.evidence, { ...PLATFORM_RULES.tiktok, composerUrl: '' });
    assert.equal(submitted.status, 'awaiting_review', JSON.stringify(submitted));
    assert.equal(submitted.step, 'creator_backend_verification');
    assert.equal(submitted.evidence.submitOutcome, 'dispatched');
    assert.equal(await page.evaluate(() => window.submitClicks), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('TikTok commits a typed hashtag with Space when the live picker has no clickable exact option', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div id="caption" class="notranslate public-DraftEditor-content" contenteditable="true"></div>
      <button id="hashtag-trigger">Hashtags</button>
      <button>Post</button>
      <script>
        const caption = document.querySelector('#caption');
        document.querySelector('#hashtag-trigger').onclick = () => {
          caption.append('#');
          caption.focus();
          const range = document.createRange();
          range.selectNodeContents(caption);
          range.collapse(false);
          const selection = window.getSelection();
          selection.removeAllRanges();
          selection.addRange(range);
        };
        caption.addEventListener('keydown', (event) => {
          if (event.key !== ' ') return;
          const match = caption.innerText.match(/#([^#\\s]+)$/u);
          if (!match) return;
          event.preventDefault();
          caption.textContent = caption.innerText.slice(0, -match[0].length);
          const topic = document.createElement('span');
          topic.className = 'mention';
          topic.dataset.testid = 'mentionText';
          topic.spellcheck = false;
          topic.textContent = match[0];
          caption.append(topic, ' ');
        });
      </script>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'tiktok',
      content: { type: 'video', title: 'TikTok video', caption: 'Video caption', body: '', tags: ['AlphaNexus'] },
      settings: {},
    }, PLATFORM_RULES.tiktok);
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.selectedTopics, ['AlphaNexus']);
    assert.deepEqual(result.evidence.missingRequiredControls, []);
    assert.equal(await page.locator('#caption span.mention[data-testid="mentionText"]').count(), 1);
  } finally {
    await browser.close();
  }
});

test('TikTok video dismisses optional checks before selecting the native first-frame cover', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-tiktok-video-'));
  try {
    const videoPath = path.join(directory, 'video.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <input type="file" accept="video/*">
      <div class="notranslate public-DraftEditor-content" contenteditable="true"></div>
      <button id="edit-cover">Edit cover</button>
      <div id="cover-dialog" role="dialog" hidden><button id="save-cover">Save</button></div>
      <div id="auto-check" role="dialog" hidden><button id="cancel-check">Cancel</button><button>Enable</button></div>
      <button>Post</button>
      <script>
        document.querySelector("input[type='file']").onchange = () => { document.querySelector('#auto-check').hidden = false; };
        document.querySelector('#cancel-check').onclick = () => { document.querySelector('#auto-check').hidden = true; };
        document.querySelector('#edit-cover').onclick = () => { document.querySelector('#cover-dialog').hidden = false; };
        document.querySelector('#save-cover').onclick = () => {
          setTimeout(() => { document.querySelector('#cover-dialog').hidden = true; }, 800);
        };
      </script>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'tiktok',
      content: { type: 'video', title: 'TikTok video', caption: 'Video caption', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'b'.repeat(64) },
      ],
      settings: {},
    }, PLATFORM_RULES.tiktok);
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.coverUploaded, true);
    assert.equal(result.evidence.coverAppliedAs, 'native_first_frame');
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('TikTok reloads one explicit pre-upload error page and continues only after it clears', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    let loads = 0;
    await page.route('https://www.tiktok.com/tiktokstudio/upload', (route) => {
      loads += 1;
      return route.fulfill({
        headers: { 'content-type': 'text/html; charset=utf-8' },
        body: loads === 1 ? '<main><h1>出错了</h1><button>重试</button></main>' : '<main><h1>上传</h1></main>',
      });
    });
    await page.goto('https://www.tiktok.com/tiktokstudio/upload');

    const result = await recoverPreUploadTransientError(page, PLATFORM_RULES.tiktok, 3000);

    assert.deepEqual(result, { detected: true, reloadAttempted: true, recovered: true });
    assert.equal(loads, 2);
    assert.equal(await page.getByText('上传', { exact: true }).isVisible(), true);
  } finally {
    await browser.close();
  }
});

test('TikTok uses the exact native retry button before reloading the error page', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <main><h1 id="error">出错了</h1><button id="retry">重试</button><h1 id="upload" hidden>上传</h1></main>
      <script>
        document.querySelector('#retry').onclick = () => {
          document.querySelector('#error').hidden = true;
          document.querySelector('#retry').hidden = true;
          document.querySelector('#upload').hidden = false;
        };
      </script>
    `);

    const result = await recoverPreUploadTransientError(page, PLATFORM_RULES.tiktok, 3000);

    assert.deepEqual(result, { detected: true, reloadAttempted: false, recovered: true });
    assert.equal(await page.getByText('上传', { exact: true }).isVisible(), true);
  } finally {
    await browser.close();
  }
});

test('TikTok detects a reviewed pre-upload error that renders after navigation settles', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <main><h1 id="error" hidden>出错了</h1><button id="retry" hidden>重试</button><h1 id="upload" hidden>上传</h1></main>
      <script>
        setTimeout(() => {
          document.querySelector('#error').hidden = false;
          document.querySelector('#retry').hidden = false;
        }, 300);
        document.querySelector('#retry').onclick = () => {
          document.querySelector('#error').hidden = true;
          document.querySelector('#retry').hidden = true;
          document.querySelector('#upload').hidden = false;
        };
      </script>
    `);

    const result = await recoverPreUploadTransientError(page, PLATFORM_RULES.tiktok, 3000);

    assert.deepEqual(result, { detected: true, reloadAttempted: false, recovered: true });
    assert.equal(await page.getByText('上传', { exact: true }).isVisible(), true);
  } finally {
    await browser.close();
  }
});

test('TikTok uses the reviewed web upload fallback after the Studio error persists', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    let loads = 0;
    await page.route('https://www.tiktok.com/tiktokstudio/upload', (route) => {
      loads += 1;
      return route.fulfill({
        headers: { 'content-type': 'text/html; charset=utf-8' },
        body: '<main><h1>出错了</h1><button>重试</button></main>',
      });
    });
    await page.route('https://www.tiktok.com/upload?lang=en', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: '<main><h1>Upload</h1></main>',
    }));
    await page.goto('https://www.tiktok.com/tiktokstudio/upload');

    const result = await recoverPreUploadTransientError(page, PLATFORM_RULES.tiktok, 3000);

    assert.deepEqual(result, {
      detected: true,
      reloadAttempted: true,
      fallbackAttempted: true,
      recovered: true,
    });
    assert.equal(loads, 2);
    assert.equal(page.url(), 'https://www.tiktok.com/upload?lang=en');
  } finally {
    await browser.close();
  }
});

test('TikTok submit-prepared rechecks the live uploaded video and native cover before one final click', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-tiktok-submit-prepared-'));
  try {
    const videoPath = path.join(directory, 'launch-video.mp4');
    const coverPath = path.join(directory, 'launch-cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <img
        id="uploaded-preview"
        width="320"
        height="180"
        src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII="
      >
      <span>launch-video.mp4</span>
      <span>已上传</span>
      <button>Edit cover</button>
      <button id="schedule">Schedule</button>
      <script>
        window.finalClicks = 0;
        document.querySelector('#schedule').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      action: 'submit_prepared',
      allowSubmit: true,
      timeoutMs: 250,
      platform: 'tiktok',
      content: { type: 'video', title: 'TikTok video', caption: 'Video caption', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'b'.repeat(64) },
      ],
      settings: {},
    };

    const result = await submitPreparedPage(
      page,
      input,
      {},
      {
        ...PLATFORM_RULES.tiktok,
        composerUrl: '',
        composerURLPrefixes: {},
        requiredURLPrefixes: [],
        uploadCompletionStableMs: 0,
      },
    );

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(result.evidence.submitClicked, true);
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
    assert.equal(result.evidence.uploadCompletion.find((check) => check.key === 'videoUploaded').current, true);
    assert.equal(result.evidence.uploadCompletion.find((check) => check.key === 'coverUploaded').nativeFirstFrame, true);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('TikTok submit-prepared never clicks when the live upload preview disappeared', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-tiktok-submit-prepared-missing-'));
  try {
    const videoPath = path.join(directory, 'launch-video.mp4');
    const coverPath = path.join(directory, 'launch-cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <span>launch-video.mp4</span>
      <span>已上传</span>
      <button>Edit cover</button>
      <button id="schedule">Schedule</button>
      <script>
        window.finalClicks = 0;
        document.querySelector('#schedule').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      action: 'submit_prepared',
      allowSubmit: true,
      timeoutMs: 250,
      platform: 'tiktok',
      content: { type: 'video', title: 'TikTok video', caption: 'Video caption', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'b'.repeat(64) },
      ],
      settings: {},
    };

    const result = await submitPreparedPage(
      page,
      input,
      {},
      {
        ...PLATFORM_RULES.tiktok,
        composerUrl: '',
        composerURLPrefixes: {},
        requiredURLPrefixes: [],
        uploadCompletionStableMs: 0,
      },
    );

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.step, 'upload_completion_unverified');
    assert.equal(result.errorCode, 'MEDIA_UPLOAD_UNVERIFIED');
    assert.equal(result.evidence.submitAttempted, false);
    assert.equal(await page.evaluate(() => window.finalClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('publish time is optional but must be a timezone-aware RFC3339 value', () => {
  assert.equal(validateInput({ ...validInput(), publishAt: '' }).publishAt, '');
  assert.equal(
    validateInput({ ...validInput(), publishAt: '2026-07-24T18:30:00+08:00' }).publishAt,
    '2026-07-24T18:30:00+08:00',
  );
  assert.throws(() => validateInput({ ...validInput(), publishAt: '2026-07-24 18:30' }), /PUBLISH_TIME_INVALID/);
});

test('scheduled publish is limited to the reviewed product platforms', () => {
  for (const platform of ['xhs', 'douyin', 'kuaishou', 'wechat_video', 'csdn', 'youtube', 'tiktok']) {
    assert.ok(PLATFORM_RULES[platform].scheduleControl?.toggleNames.includes('定时发布'));
  }
  assert.deepEqual(PLATFORM_RULES.wechat_mp.scheduleControl.nearbyLabelNames, ['定时发表']);
});

test('scheduled publish fails closed when the platform control cannot be verified', async () => {
  const empty = {
    count: async () => 0,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => false,
  };
  const page = {
    getByRole: () => empty,
    getByText: () => empty,
    locator: () => empty,
  };
  const result = await applyPublishTime(page, {
    ...validInput(),
    platform: 'xhs',
    publishAt: '2026-07-24T18:30:00+08:00',
  }, PLATFORM_RULES.xhs);

  assert.deepEqual(result, {
    applied: [],
    unverified: ['publish.time'],
    value: '',
    requestedValue: '',
    fallbackUsed: false,
    maximumValue: '',
  });
});

test('XHS scheduled publish clicks and verifies its scoped native switch', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      ${xhsLivePolicyLandmarks()}
      <div id="schedule-fields"></div>
      <script>
        document.querySelector('#schedule-switch').addEventListener('click', () => {
          const state = document.querySelector('#schedule-state');
          const button = document.querySelector('#schedule-switch');
          state.checked = true;
          button.classList.remove('unchecked');
          button.classList.add('checked');
          document.querySelector('#schedule-fields').innerHTML =
            '<input id="scheduled-at" type="datetime-local">';
        });
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'xhs',
      publishAt: '2030-07-30T19:43:00+08:00',
    }, PLATFORM_RULES.xhs);

    assert.deepEqual(result.unverified, []);
    assert.deepEqual(result.applied, ['publish.time']);
    assert.equal(await page.locator('#schedule-state').isChecked(), true);
    assert.equal(await page.locator('#scheduled-at').inputValue(), '2030-07-30T19:43');
  } finally {
    await browser.close();
  }
});

test('XHS scheduled publish commits the reviewed minute-only text value', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      ${xhsLivePolicyLandmarks()}
      <input id="scheduled-at" aria-label="发布时间" value="2026-07-30 12:35">
      <script>
        document.querySelector('#schedule-switch').addEventListener('click', () => {
          document.querySelector('#schedule-state').checked = true;
        });
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'xhs',
      publishAt: '2030-07-30T19:43:00+08:00',
    }, PLATFORM_RULES.xhs);

    assert.deepEqual(result.unverified, []);
    assert.deepEqual(result.applied, ['publish.time']);
    assert.equal(await page.locator('#scheduled-at').inputValue(), '2030-07-30 19:43');
    assert.equal(result.value, '2030-07-30 19:43');
  } finally {
    await browser.close();
  }
});

test('scheduled publish clicks the exact choice and verifies the native datetime value', async () => {
  let clicked = false;
  let checked = false;
  let currentValue = '';
  const empty = {
    count: async () => 0,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => false,
  };
  const toggle = {
    count: async () => 1,
    first() { return this; },
    isVisible: async () => true,
    isChecked: async () => checked,
    getAttribute: async () => null,
    click: async () => {
      clicked = true;
      checked = true;
    },
  };
  const dateTimeInput = {
    count: async () => 1,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => clicked,
    getAttribute: async (name) => name === 'type' ? 'datetime-local' : null,
    evaluate: async () => 'input',
    fill: async (value) => { currentValue = value; },
    inputValue: async () => currentValue,
  };
  const page = {
    getByRole: (role, options) => role === 'radio' && options.name === '定时发布' ? toggle : empty,
    getByText: () => empty,
    locator: (selector) => selector === "input[type='datetime-local']" ? dateTimeInput : empty,
  };
  const requested = new Date(Date.now() + 48 * 60 * 60 * 1000);
  requested.setSeconds(0, 0);
  const pad = (value) => String(value).padStart(2, '0');
  const expected = `${requested.getFullYear()}-${pad(requested.getMonth() + 1)}-${pad(requested.getDate())}T${pad(requested.getHours())}:${pad(requested.getMinutes())}`;
  const rule = {
    scheduleControl: {
      toggleNames: ['定时发布'],
      dateTimeInputs: ["input[type='datetime-local']"],
    },
  };
  const result = await applyPublishTime(page, {
    ...validInput(),
    platform: 'xhs',
    publishAt: requested.toISOString(),
  }, rule);

  assert.equal(clicked, true);
  assert.equal(currentValue, expected);
  assert.deepEqual(result.applied, ['publish.time']);
  assert.deepEqual(result.unverified, []);
  assert.equal(result.value, `${expected.replace('T', ' ')}:00`);
  assert.equal(result.fallbackUsed, false);
  assert.equal(result.maximumValue, '');
});

test('scheduled publish uses the native maximum when three days exceeds the platform limit', async () => {
  let clicked = false;
  let checked = false;
  let currentValue = '';
  const maximumDate = new Date(Date.now() + 24 * 60 * 60 * 1000);
  maximumDate.setSeconds(0, 0);
  const requestedDate = new Date(Date.now() + 72 * 60 * 60 * 1000);
  requestedDate.setSeconds(0, 0);
  const pad = (value) => String(value).padStart(2, '0');
  const localMinute = (value) => `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())}T${pad(value.getHours())}:${pad(value.getMinutes())}`;
  const maximum = localMinute(maximumDate);
  const requested = localMinute(requestedDate);
  const empty = {
    count: async () => 0,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => false,
  };
  const toggle = {
    count: async () => 1,
    first() { return this; },
    isVisible: async () => true,
    isChecked: async () => checked,
    getAttribute: async () => null,
    click: async () => {
      clicked = true;
      checked = true;
    },
  };
  const dateTimeInput = {
    count: async () => 1,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => clicked,
    getAttribute: async (name) => name === 'type' ? 'datetime-local' : (name === 'max' ? maximum : null),
    evaluate: async () => 'input',
    fill: async (value) => { currentValue = value; },
    inputValue: async () => currentValue,
  };
  const page = {
    getByRole: (role, options) => role === 'radio' && options.name === '定时发布' ? toggle : empty,
    getByText: () => empty,
    locator: (selector) => selector === "input[type='datetime-local']" ? dateTimeInput : empty,
  };
  const rule = {
    scheduleControl: {
      toggleNames: ['定时发布'],
      dateTimeInputs: ["input[type='datetime-local']"],
    },
  };
  const result = await applyPublishTime(page, {
    ...validInput(),
    platform: 'xhs',
    publishAt: requestedDate.toISOString(),
  }, rule);

  assert.equal(currentValue, maximum);
  assert.equal(result.value, `${maximum.replace('T', ' ')}:00`);
  assert.equal(result.requestedValue, requested);
  assert.equal(result.maximumValue, maximum);
  assert.equal(result.fallbackUsed, true);
});

test('scheduled publish uses the latest reviewed split date and time controls when three days exceeds the platform limit', async () => {
  let clicked = false;
  let dateValue = '';
  let timeValue = '';
  const empty = {
    count: async () => 0,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => false,
  };
  const toggle = {
    count: async () => 1,
    first() { return this; },
    isVisible: async () => true,
    isChecked: async () => false,
    getAttribute: async () => null,
    click: async () => { clicked = true; },
  };
  const dateInput = {
    count: async () => 1,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => clicked,
    getAttribute: async (name) => name === 'max' ? '2026-07-23' : (name === 'type' ? 'date' : null),
    evaluate: async () => 'input',
    fill: async (value) => { dateValue = value; },
    inputValue: async () => dateValue,
  };
  const timeInput = {
    count: async () => 1,
    first() { return this; },
    nth() { return this; },
    isVisible: async () => clicked,
    getAttribute: async (name) => name === 'max' ? '21:45' : (name === 'type' ? 'time' : null),
    evaluate: async () => 'input',
    fill: async (value) => { timeValue = value; },
    inputValue: async () => timeValue,
  };
  const page = {
    getByRole: (role, options) => role === 'radio' && options.name === '定时发布' ? toggle : empty,
    getByText: () => empty,
    waitForTimeout: async () => {},
    locator: (selector) => {
      if (selector === "input[type='date']") return dateInput;
      if (selector === "input[type='time']") return timeInput;
      return empty;
    },
  };
  const result = await applyPublishTime(page, {
    ...validInput(),
    platform: 'kuaishou',
    publishAt: '2026-07-24T10:30:00Z',
  }, PLATFORM_RULES.kuaishou);

  assert.equal(dateValue, '2026-07-23');
  assert.equal(timeValue, '21:45');
  assert.equal(result.value, '2026-07-23 21:45:00');
  assert.equal(result.requestedValue, '2026-07-24 18:30:00');
  assert.equal(result.maximumValue, '2026-07-23 21:45:00');
  assert.equal(result.fallbackUsed, true);
});

test('CSDN selects its native Element date and 15-minute time controls without clicking final publish', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="open-schedule">定时发布</button>
      <div class="publish-timer-dialog" hidden>
        <div class="txt-date"><input id="date" class="el-input__inner" placeholder="选择日期"></div>
        <div class="txt-date ml8"><input id="time" class="el-input__inner" placeholder="选择时间" disabled></div>
      </div>
      <div id="calendar" class="el-picker-panel" hidden><table><tr><td class="available">30</td></tr></table></div>
      <div id="times" class="el-picker-panel time-select" hidden><div class="time-select-item">11:00</div></div>
      <script>
        window.finalScheduleClicks = 0;
        document.querySelector('#open-schedule').onclick = () => { document.querySelector('.publish-timer-dialog').hidden = false; };
        document.querySelector('#date').onclick = () => { document.querySelector('#calendar').hidden = false; };
        document.querySelector('#calendar td').onclick = () => { document.querySelector('#date').value = '2026-07-30'; document.querySelector('#calendar').hidden = true; };
        document.querySelector('#date').onkeydown = (event) => { if (event.key === 'Enter') document.querySelector('#time').disabled = false; };
        document.querySelector('#time').onclick = () => { document.querySelector('#times').hidden = false; };
        document.querySelector('.time-select-item').onclick = () => { document.querySelector('#time').value = '11:00'; document.querySelector('#times').hidden = true; };
      </script>
    `);
    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'csdn',
      publishAt: '2026-07-30T10:46:00+08:00',
    }, PLATFORM_RULES.csdn);
    assert.deepEqual(result.applied, ['publish.time']);
    assert.deepEqual(result.unverified, []);
    assert.equal(result.value, '2026-07-30 11:00:00');
    assert.equal(result.requestedValue, '2026-07-30 10:46:00');
    assert.equal(result.fallbackUsed, true);
    assert.equal(await page.locator('#date').inputValue(), '2026-07-30');
    assert.equal(await page.locator('#time').inputValue(), '11:00');
    assert.equal(await page.evaluate(() => window.finalScheduleClicks), 0);
  } finally {
    await browser.close();
  }
});

test('CSDN reads hidden native settings through their reviewed visible labels', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <section class="publish-article-modal__body">
        <input id="original" type="radio" name="article" checked style="display:none"><label for="original">原创</label>
        <input id="repost" type="radio" name="article" style="display:none"><label for="repost">转载</label>
        <div class="gitcode-box"><input id="backup" type="checkbox" style="display:none"><label class="el-checkbox" for="backup">同时备份到 GitCode</label></div>
        <input id="public" type="radio" name="visibility" checked style="display:none"><label for="public">全部可见</label>
        <input id="private" type="radio" name="visibility" style="display:none"><label for="private">仅自己可见</label>
      </section>
    `);
    const result = await applySettings(page, {
      ...validInput(),
      settings: { article_type: 'original', backup_gitcode: false, collection: '', visibility: 'public' },
    }, PLATFORM_RULES.csdn);
    assert.deepEqual(result.unverified, []);
    assert.deepEqual(result.verified.sort(), ['article_type', 'backup_gitcode', 'visibility']);
    assert.deepEqual(result.skipped, ['collection']);
  } finally {
    await browser.close();
  }
});

test('TikTok Studio applies the current Chinese settings surface and native scheduled time', async () => {
  const browser = await launchChromium();
  try {
    const context = await browser.newContext();
    await context.route('https://www.tiktok.com/tiktokstudio/upload', async (route) => {
      await route.fulfill({
        contentType: 'text/html; charset=utf-8',
        body: `
          <main>
            <div class="caption public-DraftEditor-content" contenteditable="true"></div>
            <div class="view-auth-container">
              <button id="visibility" class="Select__trigger" role="combobox" aria-label="谁可以看" aria-expanded="false">所有人</button>
            </div>
            <div id="visibility-options" role="listbox" hidden>
              <button role="option" aria-selected="true">所有人</button>
              <button role="option" aria-selected="false">好友</button>
              <button role="option" aria-selected="false">仅自己</button>
            </div>
            <label><input id="comments" class="Checkbox__input" type="checkbox" aria-label="评论" checked>评论</label>
            <label><input id="reuse" class="Checkbox__input" type="checkbox" aria-label="复用内容" checked>复用内容</label>
            <div class="setting-row">
              <span>AI 生成的内容</span>
              <label><input id="ai" class="Switch__input" type="checkbox"></label>
            </div>
            <label><input id="now" class="Radio__input" type="radio" name="publish-time" aria-label="现在" checked>现在</label>
            <label><input id="scheduled" class="Radio__input" type="radio" name="publish-time" aria-label="预约发布">预约发布</label>
            <section id="scheduled-fields" hidden>
              <input id="date" type="date">
              <input id="time" type="time">
            </section>
            <button id="submit">发布</button>
          </main>
          <script>
            const visibility = document.querySelector('#visibility');
            visibility.onclick = () => {
              const options = document.querySelector('#visibility-options');
              options.hidden = false;
              visibility.setAttribute('aria-expanded', 'true');
            };
            document.addEventListener('keydown', (event) => {
              if (event.key !== 'Escape') return;
              document.querySelector('#visibility-options').hidden = true;
              visibility.setAttribute('aria-expanded', 'false');
            });
            document.querySelector('#scheduled').onchange = (event) => {
              document.querySelector('#scheduled-fields').hidden = !event.currentTarget.checked;
              document.querySelector('#submit').textContent = event.currentTarget.checked ? '预约发布' : '发布';
            };
          </script>
        `,
      });
    });
    const page = await context.newPage();
    await page.goto('https://www.tiktok.com/tiktokstudio/upload');
    assert.equal(await page.locator('main').count(), 1);
    assert.equal(await page.getByRole('checkbox', { name: '评论', exact: true }).count(), 1);

    const settings = await applySettings(page, {
      ...validInput(),
      platform: 'tiktok',
      settings: {
        visibility: 'public',
        allow_comment: true,
        allow_duet: false,
        allow_stitch: false,
        ai_generated: false,
        allow_download: false,
      },
    }, PLATFORM_RULES.tiktok);
    const publishTime = await applyPublishTime(page, {
      ...validInput(),
      platform: 'tiktok',
      publishAt: '2026-07-29T23:15:00+08:00',
    }, PLATFORM_RULES.tiktok);
    const scheduledSettings = await applySettings(page, {
      ...validInput(),
      platform: 'tiktok',
      settings: {
        visibility: 'public',
        allow_comment: true,
        allow_duet: false,
        allow_stitch: false,
        ai_generated: false,
        allow_download: false,
      },
    }, PLATFORM_RULES.tiktok);

    assert.deepEqual(settings.unverified, [], JSON.stringify(settings));
    assert.deepEqual(settings.unsupported, []);
    assert.deepEqual(settings.notPresent, ['allow_download']);
    assert.equal(await page.locator('#comments').isChecked(), true);
    assert.equal(await page.locator('#reuse').isChecked(), false);
    assert.equal(await page.locator('#ai').isChecked(), false);
    assert.equal(await page.locator('#visibility-options').isVisible(), false);
    assert.deepEqual(publishTime.unverified, []);
    assert.deepEqual(publishTime.applied, ['publish.time']);
    assert.deepEqual(scheduledSettings.unverified, [], JSON.stringify(scheduledSettings));
    assert.deepEqual(scheduledSettings.notPresent, ['allow_download']);
    assert.equal(await page.locator('#scheduled').isChecked(), true);
    assert.equal(await page.locator('#submit').innerText(), '预约发布');
    assert.equal(await page.locator('#date').inputValue(), '2026-07-29');
    assert.equal(await page.locator('#time').inputValue(), '23:15');
  } finally {
    await browser.close();
  }
});

test('TikTok Studio confirms schedule storage and selects its reviewed custom date and time pickers', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div class="scheduled-container new-form">
        <label><input id="now" class="Radio__input" type="radio" name="publish-time" checked>现在</label>
        <label><input id="scheduled" class="Radio__input" type="radio" name="publish-time">预约发布</label>
        <div id="permission" hidden>
          <button id="cancel">取消</button><button id="allow">允许</button>
        </div>
        <div class="scheduled-picker" hidden>
          <input id="time" class="TUXTextInputCore-input" value="23:55" readonly>
          <input id="date" class="TUXTextInputCore-input" value="2026-07-28" readonly>
        </div>
        <button id="submit" hidden>预约发布</button>
      </div>
      <div class="calendar-wrapper" hidden>
        <div class="days-wrapper"></div>
      </div>
      <style>
        .tiktok-timepicker-column { position: relative; display: inline-block; height: 60px; width: 80px; overflow-y: auto; }
        .tiktok-timepicker-spacer { height: 480px; }
        .tiktok-timepicker-options { position: absolute; inset: 0; }
        .tiktok-timepicker-option-item { position: absolute; width: 100%; height: 20px; }
      </style>
      <div id="time-picker" hidden>
        <div id="hours" class="tiktok-timepicker-column"><div class="tiktok-timepicker-spacer"></div><div class="tiktok-timepicker-options"></div></div>
        <div id="minutes" class="tiktok-timepicker-column"><div class="tiktok-timepicker-spacer"></div><div class="tiktok-timepicker-options"></div></div>
      </div>
      <script>
        let permissionGranted = false;
        const scheduled = document.querySelector('#scheduled');
        const picker = document.querySelector('.scheduled-picker');
        const selectSchedule = () => {
          if (!permissionGranted) {
            document.querySelector('#permission').hidden = false;
            scheduled.checked = false;
            document.querySelector('#now').checked = true;
            return;
          }
          scheduled.checked = true;
          document.querySelector('#now').checked = false;
          picker.hidden = false;
          document.querySelector('#submit').hidden = false;
        };
        scheduled.onclick = selectSchedule;
        document.querySelector('#allow').onclick = () => {
          permissionGranted = true;
          document.querySelector('#permission').hidden = true;
        };
        const days = document.querySelector('.days-wrapper');
        for (let index = 0; index < 42; index += 1) {
          const day = document.createElement('span');
          day.className = 'day';
          day.textContent = String(index < 3 ? 28 + index : index - 2);
          day.onclick = () => {
            if (index === 31) document.querySelector('#date').value = '2026-07-29';
            document.querySelector('.calendar-wrapper').hidden = true;
          };
          days.append(day);
        }
        document.querySelector('#date').onclick = () => {
          document.querySelector('.calendar-wrapper').hidden = false;
        };
        document.querySelector('#time').onclick = () => {
          document.querySelector('#time-picker').hidden = false;
        };
        const mountClockColumn = (id, values, className, initialIndex, apply) => {
          const column = document.querySelector('#' + id);
          const options = column.querySelector('.tiktok-timepicker-options');
          const render = () => {
            const start = Math.max(0, Math.min(values.length - 3, Math.floor(column.scrollTop / 20)));
            options.innerHTML = values.slice(start, start + 4).map((value, offset) => '<div class="tiktok-timepicker-option-item" style="top:' + ((start + offset) * 20) + 'px"><span class="tiktok-timepicker-option-text ' + className + '">' + value + '</span></div>').join('');
          };
          column.addEventListener('scroll', render);
          column.addEventListener('click', (event) => {
            const option = event.target.closest('.tiktok-timepicker-option-text');
            if (option) apply(option.textContent);
          });
          column.scrollTop = initialIndex * 20;
          render();
        };
        mountClockColumn('hours', Array.from({ length: 24 }, (_, index) => String(index).padStart(2, '0')), 'tiktok-timepicker-left', 14, (hour) => {
          document.querySelector('#time').value = hour + ':' + document.querySelector('#time').value.slice(3);
        });
        mountClockColumn('minutes', Array.from({ length: 12 }, (_, index) => String(index * 5).padStart(2, '0')), 'tiktok-timepicker-right', 2, (minute) => {
          document.querySelector('#time').value = document.querySelector('#time').value.slice(0, 3) + minute;
          document.querySelector('#time-picker').hidden = true;
        });
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'tiktok',
      publishAt: '2026-07-29T13:13:00+08:00',
    }, PLATFORM_RULES.tiktok);

    assert.deepEqual(result.unverified, []);
    assert.deepEqual(result.applied, ['publish.time']);
    assert.equal(result.value, '2026-07-29 13:15:00');
    assert.equal(result.fallbackUsed, true);
    assert.equal(await page.locator('#scheduled').isChecked(), true);
    assert.equal(await page.locator('#date').inputValue(), '2026-07-29');
    assert.equal(await page.locator('#time').inputValue(), '13:15');
  } finally {
    await browser.close();
  }
});

test('TikTok Studio falls back to the focused clock trigger when a virtual minute is not mounted', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <label><input id="now" class="Radio__input" type="radio" name="publish-time" checked>现在</label>
      <label><input id="scheduled" class="Radio__input" type="radio" name="publish-time">预约发布</label>
      <div class="scheduled-picker" hidden>
        <input id="time" class="TUXTextInputCore-input" value="14:30" readonly>
        <input id="date" class="TUXTextInputCore-input" value="2026-07-29" readonly>
      </div>
      <script>
        const scheduled = document.querySelector('#scheduled');
        scheduled.onclick = () => {
          scheduled.checked = true;
          document.querySelector('#now').checked = false;
          document.querySelector('.scheduled-picker').hidden = false;
        };
        document.querySelector('#time').onkeydown = (event) => {
          if (event.key !== 'ArrowUp') return;
          event.preventDefault();
          const input = event.currentTarget;
          const [hour, minute] = input.value.split(':').map(Number);
          const total = (hour * 60 + minute + 24 * 60 - 5) % (24 * 60);
          input.value = String(Math.floor(total / 60)).padStart(2, '0') + ':' + String(total % 60).padStart(2, '0');
        };
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'tiktok',
      publishAt: '2026-07-29T14:11:00+08:00',
    }, PLATFORM_RULES.tiktok);

    assert.deepEqual(result.unverified, [], JSON.stringify(result));
    assert.deepEqual(result.applied, ['publish.time']);
    assert.equal(result.value, '2026-07-29 14:15:00');
    assert.equal(await page.locator('#time').inputValue(), '14:15');
  } finally {
    await browser.close();
  }
});

test('each scheduled platform uses its reviewed combined native maximum before final review', async () => {
  // TikTok, WeChat Video, and WeChat MP use dedicated reviewed native pickers
  // with separate tests; they do not expose this combined native max control.
  const platforms = ['xhs', 'douyin', 'kuaishou', 'csdn', 'youtube'];
  const pad = (value) => String(value).padStart(2, '0');
  const maximumDate = new Date(Date.now() + 24 * 60 * 60 * 1000);
  const maximum = `${maximumDate.getFullYear()}-${pad(maximumDate.getMonth() + 1)}-${pad(maximumDate.getDate())}T${pad(maximumDate.getHours())}:${pad(maximumDate.getMinutes())}`;
  const requested = new Date(Date.now() + 48 * 60 * 60 * 1000).toISOString();
  for (const platform of platforms) {
    const rule = PLATFORM_RULES[platform];
    const reviewedRule = {
      ...rule,
      scheduleControl: {
        ...rule.scheduleControl,
        // This table test covers the shared max/readback contract. Dedicated
        // picker fixtures cover their own native navigation separately.
        elementDateTimePicker: null,
        antDateTimePicker: null,
        youtubeDateTimePicker: null,
        ...(platform === 'xhs'
          ? {
            scopedLabel: '',
            wrapperSelector: '',
            labelSelector: '',
            stateSelector: '',
            clickSelector: '',
          }
          : {}),
      },
    };
    let checked = false;
    let currentValue = '';
    const empty = {
      count: async () => 0,
      first() { return this; },
      nth() { return this; },
      isVisible: async () => false,
    };
    const toggle = {
      count: async () => 1,
      first() { return this; },
      isVisible: async () => true,
      isChecked: async () => checked,
      getAttribute: async () => null,
      click: async () => { checked = true; },
    };
    const dateTimeInput = {
      count: async () => 1,
      first() { return this; },
      nth() { return this; },
      isVisible: async () => true,
      getAttribute: async (name) => {
        if (name === 'max') return rule.dateTimeFormat === 'yyyy-MM-dd HH:mm' ? maximum.replace('T', ' ') : maximum;
        return rule.dateTimeFormat === 'yyyy-MM-dd HH:mm' ? 'text' : 'datetime-local';
      },
      evaluate: async () => 'input',
      fill: async (value) => { currentValue = value; },
      inputValue: async () => currentValue,
    };
    const page = {
      getByRole: (role, options) => role === 'radio' && options.name === reviewedRule.scheduleControl.toggleNames[0] ? toggle : empty,
      getByText: () => empty,
      waitForTimeout: async () => {},
      locator: (selector) => reviewedRule.scheduleControl.dateTimeInputs.includes(selector) ? dateTimeInput : empty,
    };
    const result = await applyPublishTime(page, {
      ...validInput(),
      platform,
      publishAt: requested,
    }, reviewedRule);
    assert.equal(result.fallbackUsed, true, platform);
    assert.equal(result.unverified.length, 0, platform);
    assert.equal(currentValue, rule.dateTimeFormat === 'yyyy-MM-dd HH:mm' ? maximum.replace('T', ' ') : maximum, platform);
  }
});

test('Douyin schedule fails closed when the exact checkbox does not remain checked', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <label><input id="schedule" type="checkbox">定时发布</label>
      <input id="scheduled-at" type="text" aria-label="发布时间"
        placeholder="日期和时间" format="yyyy-MM-dd HH:mm">
      <script>
        document.querySelector('#schedule').addEventListener('change', (event) => {
          event.currentTarget.checked = false;
        });
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'douyin',
      publishAt: '2030-07-24T18:30:00+08:00',
    }, PLATFORM_RULES.douyin);

    assert.deepEqual(result, {
      applied: [],
      unverified: ['publish.time'],
      value: '',
      requestedValue: '',
      fallbackUsed: false,
      maximumValue: '',
    });
    assert.equal(await page.locator('#schedule').isChecked(), false);
  } finally {
    await browser.close();
  }
});

test('CSDN contenteditable accepts only its terminal editor newline', () => {
  assert.equal(contentEditableValueMatches('正文\n', '正文'), true);
  assert.equal(contentEditableValueMatches('正文', '正文'), true);
  assert.equal(contentEditableValueMatches('正文\n\n', '正文'), false);
  assert.equal(contentEditableValueMatches('不同正文\n', '正文'), false);
});

test('Douyin contenteditable accepts only its reviewed zero-width hashtag separators', () => {
  assert.equal(contentEditableValueMatches('正文\u200b#新品\u200b', '正文\n\n#新品'), true);
  assert.equal(contentEditableValueMatches('正文被修改\u200b#新品\u200b', '正文\n\n#新品'), false);
});

test('Douyin topic trigger recognizes a trailing marker with zero-width separators', () => {
  assert.equal(hasTrailingTopicMarker('正文', '正文#'), true);
  assert.equal(hasTrailingTopicMarker('正文', '正文#\u200b'), true);
  assert.equal(hasTrailingTopicMarker('正文', '正文#\u200b '), true);
  assert.equal(hasTrailingTopicMarker('正文#', '正文#'), false);
});

test('submit requires a separate positive flag on a submit-capable platform', () => {
  const input = {
    ...validInput(),
    platform: 'xhs',
    content: { type: 'graph_text', title: 'title', caption: '', body: 'body', tags: [] },
    media: [{ path: '/tmp/image.png', kind: 'image', role: 'content', position: 0, sha256: 'a'.repeat(64) }],
  };
  input.action = 'submit';
  assert.throws(() => validateInput(input), /SUBMIT_APPROVAL_REQUIRED/);
  input.allowSubmit = true;
  assert.equal(validateInput(input).action, 'submit');
});

test('XHS resolves and dispatches the unique trusted inner button in a closed shadow host', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <main class="publish-page">
        <xhs-publish-btn
          is-publish="true"
          submit-text="发布"
          submit-disabled="false"
          submit-loading="false"
        ></xhs-publish-btn>
      </main>
    `));
    await page.locator('xhs-publish-btn').evaluate((host) => host.click());
    assert.equal(await page.evaluate(() => window.xhsInnerSubmitClicks || 0), 0);

    const exact = await exactComposerSubmit(page, PLATFORM_RULES.xhs);
    assert.equal(exact.ambiguous, false);
    assert.ok(exact.control);
    assert.equal(await exact.control.evaluate((node) => node.localName), 'button');
    assert.equal(await exact.control.isEnabled(), true);

    const evidence = { submitAttempted: false, submitClicked: false };
    assert.equal(await clickReviewedSubmitOnce(page, exact.control, evidence), true);
    assert.equal(await page.evaluate(() => window.xhsInnerSubmitClicks), 1);
    assert.equal(await page.evaluate(() => window.xhsInnerSubmitTrustedClicks), 1);
    assert.equal(evidence.submitAttempted, true);
    assert.equal(evidence.submitClicked, true);
    assert.equal(evidence.submitOutcome, 'dispatched');
  } finally {
    await browser.close();
  }
});

test('XHS recognizes the reviewed scheduled final label in its closed shadow host', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <main class="publish-page">
        <xhs-publish-btn
          is-publish="true"
          submit-text="定时发布"
          submit-disabled="false"
          submit-loading="false"
        ></xhs-publish-btn>
      </main>
    `));

    const exact = await exactComposerSubmit(page, PLATFORM_RULES.xhs);

    assert.equal(exact.ambiguous, false);
    assert.ok(exact.control);
    assert.equal(await exact.control.evaluate((node) => node.textContent), '定时发布');
    assert.equal(await page.evaluate(() => window.xhsInnerSubmitClicks || 0), 0);
  } finally {
    await browser.close();
  }
});

test('XHS rejects missing, ambiguous, disabled, and loading closed-shadow publish controls', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    const scenarios = [
      { label: 'missing', attributes: 'data-publish-count="0" submit-disabled="false" submit-loading="false"', found: false, ambiguous: false },
      { label: 'ambiguous', attributes: 'data-publish-count="2" submit-disabled="false" submit-loading="false"', found: false, ambiguous: true },
      { label: 'disabled', attributes: 'submit-disabled="true" submit-loading="false"', found: true, ambiguous: false },
      { label: 'loading', attributes: 'submit-disabled="false" submit-loading="true"', found: true, ambiguous: false },
    ];
    for (const scenario of scenarios) {
      await page.setContent(xhsClosedShadowSubmitFixture(`
        <main class="publish-page">
          <xhs-publish-btn
            is-publish="true"
            submit-text="立即发布"
            ${scenario.attributes}
          ></xhs-publish-btn>
        </main>
      `));
      const exact = await exactComposerSubmit(page, PLATFORM_RULES.xhs);
      assert.equal(Boolean(exact.control), scenario.found, scenario.label);
      assert.equal(exact.ambiguous, scenario.ambiguous, scenario.label);
      if (exact.control) {
        assert.equal(await exact.control.isEnabled(), false, scenario.label);
      }
      assert.equal(await page.evaluate(() => window.xhsInnerSubmitClicks || 0), 0, scenario.label);
    }
  } finally {
    await browser.close();
  }
});

test('one-click submit records the real target and click instant then awaits creator-backend verification', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-submit-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.xiaohongshu.com/**', async (route) => {
      await route.fulfill({
        headers: { 'content-type': 'text/html; charset=utf-8' },
        body: xhsClosedShadowSubmitFixture(`
          <main class="publish-page">
            <input placeholder="标题">
            <div id="body" contenteditable="true"></div>
            <input id="images" type="file" accept="image/png" multiple>
            <div id="upload-state"></div>
            ${xhsLivePolicyLandmarks({ aiText: '笔记含AI合成内容' })}
            <xhs-publish-btn role="button">发布</xhs-publish-btn>
          </main>
          <script>
            window.submitClicks = 0;
            document.querySelector('#images').addEventListener('change', () => {
              const progress = document.createElement('div');
              progress.setAttribute('role', 'progressbar');
              document.querySelector('#upload-state').append(progress);
              const preview = document.createElement('img');
              preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
              preview.width = 320;
              preview.height = 420;
              document.querySelector('#upload-state').append(preview);
              setTimeout(() => progress.remove(), 150);
            });
            document.querySelector('xhs-publish-btn').addEventListener('click', () => {
              window.submitClicks += 1;
              const success = document.createElement('div');
              success.textContent = '发布成功';
              document.body.append(success);
            });
          </script>
        `),
      });
    });
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=image');
    const input = {
      ...validInput(),
      timeoutMs: 3000,
      action: 'submit',
      allowSubmit: true,
      platform: 'xhs',
      content: { type: 'graph_text', title: '一键发布标题', caption: '', body: '一键发布正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        visibility: 'public', content_source: 'ai', allow_duet: false, allow_copy: false,
      },
    };

    const beforeSubmit = Date.now();
    const result = await prepareAndMaybeSubmit(page, input, PLATFORM_RULES.xhs);
    const afterSubmit = Date.now();

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(await page.locator("input[placeholder*='标题']").inputValue(), '一键发布标题');
    assert.equal(await page.locator('#body').innerText(), '一键发布正文');
    assert.deepEqual(await page.locator('#images').evaluate((node) => [...node.files].map((file) => file.name)), ['content.png']);
    assert.equal(await page.evaluate(() => window.submitClicks), 1);
    assert.equal(result.evidence.titleFilled, true);
    assert.equal(result.evidence.bodyFilled, true);
    assert.equal(result.evidence.submitAttempted, true);
    assert.equal(result.evidence.submitClicked, true);
    assert.match(result.evidence.submitTargetId, /^[a-f0-9]{32}$/i);
    assert.ok(Date.parse(result.evidence.submitClickedAt) >= beforeSubmit);
    assert.ok(Date.parse(result.evidence.submitClickedAt) <= afterSubmit);
    assert.equal(result.evidence.platformSubmissionSignal, '发布成功');
    assert.equal(result.evidence.contentImagesUploadedSelectionVerified, true);
    assert.deepEqual(result.evidence.contentImagesUploadedSelectedFiles, [{
      name: 'content.png',
      size: fs.statSync(imagePath).size,
    }]);
    assert.equal(result.evidence.contentImagesUploadedExpectedItemCount, 1);
    assert.equal(result.evidence.contentImagesUploadedPreviewIncrement, 1);
    assert.equal(result.evidence.contentImagesUploadedPreviewObserved, true);
    assert.equal(result.evidence.contentImagesUploadedProgressCleared, true);
    assert.equal(result.evidence.contentImagesUploadedErrorFree, true);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('submit refuses the final click when the uploaded media preview is not independently observed', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-upload-proof-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.xiaohongshu.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: xhsClosedShadowSubmitFixture(`
        <img id="global-avatar" width="320" height="320" hidden>
        <main class="publish-page">
          <input placeholder="标题">
          <div id="body" contenteditable="true"></div>
          <input id="images" type="file" accept="image/png">
          ${xhsLivePolicyLandmarks({ aiText: '笔记含AI合成内容' })}
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        </main>
        <script>
          window.submitClicks = 0;
          document.querySelector('#images').onchange = () => {
            const avatar = document.querySelector('#global-avatar');
            avatar.hidden = false;
            avatar.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
          };
          document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
        </script>
      `),
    }));
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=image');
    const input = {
      ...validInput(),
      timeoutMs: 3000,
      action: 'submit',
      allowSubmit: true,
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        visibility: 'public', content_source: 'ai', allow_duet: false, allow_copy: false,
      },
    };

    const result = await prepareAndMaybeSubmit(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.step, 'upload_completion_unverified');
    assert.equal(result.errorCode, 'MEDIA_UPLOAD_UNVERIFIED');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.submitAttempted, false);
    assert.equal(result.evidence.submitClicked, false);
    assert.equal(result.evidence.submitClickedAt, undefined);
    assert.equal(result.evidence.submitTargetId, undefined);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS verifies the reviewed upload root when it mounts only after file selection', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-delayed-upload-root-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <input id="images" type="file" accept="image/png" multiple>
      <script>
        const images = document.querySelector('#images');
        images.addEventListener('change', () => {
          const root = document.createElement('main');
          root.className = 'publish-page';
          root.innerHTML = [
            '<input placeholder="标题">',
            '<textarea placeholder="正文"></textarea>',
            '<div id="upload-state"></div>',
            '<xhs-publish-btn role="button">发布</xhs-publish-btn>',
          ].join('');
          root.prepend(images);
          document.body.append(root);
          const preview = document.createElement('img');
          preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
          preview.width = 320;
          preview.height = 420;
          document.querySelector('#upload-state').append(preview);
        });
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      timeoutMs: 3000,
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'e'.repeat(64) }],
      settings: {},
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.contentImagesUploadedScopeVerified, true);
    assert.equal(result.evidence.contentImagesUploadedPreviewObserved, true);
    assert.equal(result.evidence.contentImagesUploadedPreviewIncrement, 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS rejects a late upload root when the only large preview is outside that scope', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-unscoped-late-preview-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <input id="images" type="file" accept="image/png" multiple>
      <div id="outside-preview"></div>
      <script>
        window.submitClicks = 0;
        const images = document.querySelector('#images');
        images.addEventListener('change', () => {
          const root = document.createElement('main');
          root.className = 'publish-page';
          root.innerHTML = [
            '<input placeholder="标题">',
            '<textarea placeholder="正文"></textarea>',
            '<xhs-publish-btn role="button">发布</xhs-publish-btn>',
          ].join('');
          root.prepend(images);
          document.body.append(root);
          const outside = document.createElement('img');
          outside.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
          outside.width = 320;
          outside.height = 420;
          document.querySelector('#outside-preview').append(outside);
          root.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
        });
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      timeoutMs: 1500,
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {},
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.evidence.contentImagesUploadedScopeVerified, true);
    assert.equal(result.evidence.contentImagesUploadedPreviewObserved, false);
    assert.equal(result.evidence.contentImagesUploadedPreviewMediaCount, 0);
    assert.ok(result.evidence.missingRequiredControls.includes('media.upload_completion'));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS submit requires one scoped preview increment for every frozen image', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-upload-count-'));
  try {
    const firstPath = path.join(directory, 'first.png');
    const secondPath = path.join(directory, 'second.png');
    fs.writeFileSync(firstPath, 'fixture-image-one');
    fs.writeFileSync(secondPath, 'fixture-image-two');
    const page = await browser.newPage();
    await page.route('https://creator.xiaohongshu.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: xhsClosedShadowSubmitFixture(`
        <main class="publish-page">
          <input placeholder="标题">
          <div id="body" contenteditable="true"></div>
          <input id="images" type="file" accept="image/png" multiple>
          <div id="upload-state"></div>
          ${xhsLivePolicyLandmarks({ aiText: '笔记含AI合成内容' })}
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        </main>
        <script>
          window.submitClicks = 0;
          document.querySelector('#images').onchange = () => {
            const preview = document.createElement('img');
            preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
            preview.width = 320;
            preview.height = 420;
            document.querySelector('#upload-state').append(preview);
          };
          document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
        </script>
      `),
    }));
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=image');
    const input = {
      ...validInput(),
      timeoutMs: 3000,
      action: 'submit',
      allowSubmit: true,
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [
        { path: firstPath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) },
        { path: secondPath, kind: 'image', role: 'content', position: 1, sha256: 'c'.repeat(64) },
      ],
      settings: {
        visibility: 'public', content_source: 'ai', allow_duet: false, allow_copy: false,
      },
    };

    const result = await prepareAndMaybeSubmit(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.step, 'upload_completion_unverified');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.contentImagesUploadedExpectedItemCount, 2);
    assert.equal(result.evidence.contentImagesUploadedPreviewIncrement, 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS prepare waits beyond five seconds for upload completion and the unique final button', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-upload-wait-'));
  try {
    const imagePath = path.join(directory, 'slow.png');
    fs.writeFileSync(imagePath, 'slow-fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <main class="publish-page">
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <input id="images" type="file" accept="image/png">
        <div id="upload-state"></div>
        <xhs-publish-btn role="button" aria-disabled="true">发布</xhs-publish-btn>
      </main>
      <script>
        document.querySelector('#images').onchange = () => {
          setTimeout(() => {
            const preview = document.createElement('img');
            preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
            preview.width = 320;
            preview.height = 420;
            document.querySelector('#upload-state').append(preview);
          }, 5500);
        };
        setTimeout(() => {
          const host = document.querySelector('xhs-publish-btn');
          host.setAttribute('aria-disabled', 'false');
          host.setAttribute('submit-disabled', 'false');
        }, 11500);
      </script>
    `));
    const input = {
      ...validInput(),
      timeoutMs: 20000,
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.contentImagesUploadedPreviewObserved, true);
    assert.equal(result.evidence.submitEnabled, true);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS prepare rejects ambiguous final publish buttons', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-submit-unique-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <main class="publish-page">
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <input id="images" type="file" accept="image/png">
        <div id="upload-state"></div>
        <xhs-publish-btn role="button">发布</xhs-publish-btn>
        <xhs-publish-btn role="button">发布</xhs-publish-btn>
      </main>
      <script>
        document.querySelector('#images').onchange = () => {
          const preview = document.createElement('img');
          preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
          preview.width = 320;
          preview.height = 420;
          document.querySelector('#upload-state').append(preview);
        };
      </script>
    `));
    const input = {
      ...validInput(),
      timeoutMs: 3000,
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.ok(result.evidence.missingRequiredControls.includes('submit.review_button'));
    assert.equal(result.evidence.submitEnabled, false);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS submit binds upload evidence to frozen file basename and size', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-upload-selection-'));
  try {
    const imagePath = path.join(directory, 'frozen.png');
    fs.writeFileSync(imagePath, 'frozen-fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.xiaohongshu.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: xhsClosedShadowSubmitFixture(`
        <main class="publish-page">
          <input placeholder="标题">
          <div id="body" contenteditable="true"></div>
          <input id="images" type="file" accept="image/png">
          <div id="upload-state"></div>
          ${xhsLivePolicyLandmarks({ aiText: '笔记含AI合成内容' })}
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        </main>
        <script>
          window.submitClicks = 0;
          document.querySelector('#images').onchange = (event) => {
            const replacement = new DataTransfer();
            replacement.items.add(new File(['not-the-frozen-file'], 'other.png', { type: 'image/png' }));
            event.currentTarget.files = replacement.files;
            const preview = document.createElement('img');
            preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
            preview.width = 320;
            preview.height = 420;
            document.querySelector('#upload-state').append(preview);
          };
          document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
        </script>
      `),
    }));
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=image');
    const input = {
      ...validInput(),
      action: 'submit',
      allowSubmit: true,
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        visibility: 'public', content_source: 'ai', allow_duet: false, allow_copy: false,
      },
    };

    const result = await prepareAndMaybeSubmit(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.step, 'required_control_missing');
    assert.ok(result.evidence.missingRequiredControls.includes('media.content_images'));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.contentImagesUploadedSelectionVerified, false);
    assert.deepEqual(result.evidence.contentImagesUploadedSelectedFiles, [{
      name: 'other.png',
      size: Buffer.byteLength('not-the-frozen-file'),
    }]);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS submit critical section refuses changed content and ambiguous composer buttons', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-critical-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.xiaohongshu.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: xhsClosedShadowSubmitFixture(`
        <main class="publish-page">
          <input id="title" placeholder="标题">
          <div id="body" contenteditable="true"></div>
          <input id="images" type="file" accept="image/png">
          <div id="upload-state"></div>
          ${xhsLivePolicyLandmarks({ aiText: '笔记含AI合成内容' })}
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        </main>
        <script>
          window.submitClicks = 0;
          document.querySelector('#images').onchange = () => {
            const preview = document.createElement('img');
            preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
            preview.width = 320;
            preview.height = 420;
            document.querySelector('#upload-state').append(preview);
          };
          document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
        </script>
      `),
    }));
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=image');
    const input = {
      ...validInput(),
      action: 'submit',
      allowSubmit: true,
      platform: 'xhs',
      content: { type: 'graph_text', title: '冻结标题', caption: '', body: '冻结正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        visibility: 'public', content_source: 'ai', allow_duet: false, allow_copy: false,
      },
    };
    const prepared = await prepareForm(page, input, PLATFORM_RULES.xhs);
    assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));

    await page.locator('#title').fill('被篡改标题');
    const changed = await submitPreparedPage(page, input, prepared.evidence);
    assert.equal(changed.status, 'manual_takeover', JSON.stringify(changed));
    assert.equal(changed.step, 'critical_revalidation_failed');
    assert.equal(changed.evidence.criticalRevalidation.title, false);
    assert.equal(changed.evidence.submitAttempted, false);
    assert.equal(changed.evidence.submitClicked, false);
    assert.equal(changed.evidence.submitTargetId, undefined);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);

    await page.locator('#title').fill('冻结标题');
    await page.locator('.publish-page').evaluate((root) => {
      const duplicate = document.createElement('xhs-publish-btn');
      duplicate.setAttribute('role', 'button');
      duplicate.textContent = '发布';
      root.append(duplicate);
    });
    const ambiguous = await submitPreparedPage(page, input, prepared.evidence);
    assert.equal(ambiguous.status, 'manual_takeover', JSON.stringify(ambiguous));
    assert.equal(ambiguous.step, 'submit_button_ambiguous');
    assert.equal(ambiguous.evidence.submitAttempted, false);
    assert.equal(ambiguous.evidence.submitClicked, false);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS final critical section revalidates every frozen policy field, immediate mode, and composer mode', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-policy-critical-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.xiaohongshu.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: xhsClosedShadowSubmitFixture(`
        <main class="publish-page">
          <input id="title" placeholder="标题">
          <div id="body" contenteditable="true"></div>
          <input id="images" type="file" accept="image/png">
          <div id="upload-state"></div>
          ${xhsLivePolicyLandmarks({ aiText: '笔记含AI合成内容' })}
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        </main>
        <script>
          window.submitClicks = 0;
          document.querySelector('#images').onchange = () => {
            const preview = document.createElement('img');
            preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
            preview.width = 320;
            preview.height = 420;
            document.querySelector('#upload-state').append(preview);
          };
          document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
        </script>
      `),
    }));
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=image');
    const preparedInput = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '冻结标题', caption: '', body: '冻结正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        visibility: 'public', content_source: 'ai', allow_duet: false, allow_copy: false,
      },
    };
    const prepared = await prepareForm(page, preparedInput, PLATFORM_RULES.xhs);
    assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));
    const input = {
      ...preparedInput,
      action: 'submit',
      allowSubmit: true,
      settings: xhsFormalSettings(),
    };
    const restoreURL = PLATFORM_RULES.xhs.composerURLs.graph_text;
    const scenarios = [
      {
        field: 'ai',
        mutate: () => page.locator('.publish-page-content-setting-content .d-select-content').evaluate((node) => { node.textContent = '添加内容类型声明'; }),
        restore: () => page.locator('.publish-page-content-setting-content .d-select-content').evaluate((node) => { node.textContent = '笔记含AI合成内容'; }),
      },
      {
        field: 'visibility',
        mutate: () => page.locator('.permission-card-select .d-select-content').evaluate((node) => { node.textContent = '仅自己可见'; }),
        restore: () => page.locator('.permission-card-select .d-select-content').evaluate((node) => { node.textContent = '公开可见'; }),
      },
      {
        field: 'commentEnabled',
        mutate: () => page.locator('.publish-page').evaluate((root) => {
          root.insertAdjacentHTML('beforeend', '<input id="comment-state" data-testid="comment-enabled" type="checkbox">');
        }),
        restore: () => page.locator('#comment-state').evaluate((node) => node.remove()),
      },
      {
        field: 'declareOriginal',
        mutate: () => page.locator('.publish-page').evaluate((root) => {
          root.insertAdjacentHTML('beforeend', '<input id="original-state" data-testid="declare-original" type="checkbox" checked>');
        }),
        restore: () => page.locator('#original-state').evaluate((node) => node.remove()),
      },
      {
        field: 'allowDuet',
        mutate: () => page.locator('#duet-state').evaluate((node) => { node.checked = true; }),
        restore: () => page.locator('#duet-state').evaluate((node) => { node.checked = false; }),
      },
      {
        field: 'allowCopy',
        mutate: () => page.locator('#copy-state').evaluate((node) => { node.checked = true; }),
        restore: () => page.locator('#copy-state').evaluate((node) => { node.checked = false; }),
      },
      {
        field: 'locationEmpty',
        mutate: () => page.locator('#xhs-location-empty').evaluate((node) => { node.textContent = '上海'; }),
        restore: () => page.locator('#xhs-location-empty').evaluate((node) => { node.textContent = '添加地点'; }),
      },
      {
        field: 'collectionEmpty',
        mutate: () => page.locator('#xhs-collection-empty').evaluate((node) => { node.textContent = '测试合集'; }),
        restore: () => page.locator('#xhs-collection-empty').evaluate((node) => { node.textContent = '加入合集'; }),
      },
      {
        field: 'groupEmpty',
        mutate: () => page.locator('#xhs-group-empty').evaluate((node) => { node.textContent = '测试群聊'; }),
        restore: () => page.locator('#xhs-group-empty').evaluate((node) => { node.textContent = '选择群聊'; }),
      },
      {
        field: 'relatedMediaEmpty',
        mutate: () => page.locator('#xhs-related-media-empty').evaluate((node) => { node.textContent = '已引用笔记'; }),
        restore: () => page.locator('#xhs-related-media-empty').evaluate((node) => { node.textContent = '引用笔记'; }),
      },
      {
        field: 'immediatePublish',
        mutate: () => page.locator('#schedule-state').evaluate((node) => { node.checked = true; }),
        restore: () => page.locator('#schedule-state').evaluate((node) => { node.checked = false; }),
      },
      {
        field: 'immediatePublish',
        mutate: () => page.locator('#schedule-switch').evaluate((node) => {
          node.closest('.custom-switch-wrapper').insertAdjacentHTML(
            'afterend',
            '<div id="duplicate-schedule" class="custom-switch-wrapper"><div class="custom-switch-text-content"><span>定时发布</span></div><div class="custom-switch-switch"><button class="d-switch-simulator unchecked"></button><input type="checkbox" hidden></div></div>',
          );
        }),
        restore: () => page.locator('#duplicate-schedule').evaluate((node) => node.remove()),
      },
      {
        field: 'composerMode',
        mutate: () => page.evaluate(() => history.replaceState(null, '', '/publish/publish?from=menu&target=video')),
        restore: () => page.evaluate((url) => history.replaceState(null, '', url), restoreURL),
      },
    ];

    for (const scenario of scenarios) {
      await scenario.mutate();
      const result = await submitPreparedPage(page, input, prepared.evidence);
      assert.equal(result.status, 'manual_takeover', `${scenario.field}: ${JSON.stringify(result)}`);
      assert.equal(result.step, 'critical_revalidation_failed', scenario.field);
      assert.equal(result.evidence.criticalRevalidation[scenario.field], false, scenario.field);
      assert.equal(result.evidence.submitAttempted, false, scenario.field);
      assert.equal(result.evidence.submitClicked, false, scenario.field);
      assert.equal(await page.evaluate(() => window.submitClicks), 0, scenario.field);
      await scenario.restore();
    }
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('final click failure preserves one-attempt unknown evidence and its click instant', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="dispatched">发布</button>
      <button id="not-dispatched">发布</button>
      <script>
        window.dispatchedClicks = 0;
        window.notDispatchedClicks = 0;
        document.querySelector('#dispatched').onclick = () => { window.dispatchedClicks += 1; };
        document.querySelector('#not-dispatched').onclick = () => { window.notDispatchedClicks += 1; };
      </script>
    `);
    const dispatched = page.locator('#dispatched');
    let dispatchedCalls = 0;
    const dispatchedEvidence = { submitAttempted: false, submitClicked: false };
    const before = Date.now();
    const completed = await clickReviewedSubmitOnce(page, {
      evaluate: (...args) => dispatched.evaluate(...args),
      click: async (options) => {
        dispatchedCalls += 1;
        assert.equal(options.noWaitAfter, true);
        await dispatched.click(options);
        throw new Error('page closed after click dispatch');
      },
    }, dispatchedEvidence);
    const after = Date.now();

    assert.equal(completed, false);
    assert.equal(dispatchedCalls, 1);
    assert.equal(await page.evaluate(() => window.dispatchedClicks), 1);
    assert.equal(dispatchedEvidence.submitAttempted, true);
    assert.equal(dispatchedEvidence.submitClicked, true);
    assert.equal(dispatchedEvidence.submitOutcome, 'unknown');
    assert.ok(Date.parse(dispatchedEvidence.submitClickedAt) >= before);
    assert.ok(Date.parse(dispatchedEvidence.submitClickedAt) <= after);

    const notDispatched = page.locator('#not-dispatched');
    let notDispatchedCalls = 0;
    const notDispatchedEvidence = { submitAttempted: false, submitClicked: false };
    const failedBeforeDispatch = await clickReviewedSubmitOnce(page, {
      evaluate: (...args) => notDispatched.evaluate(...args),
      click: async (options) => {
        notDispatchedCalls += 1;
        assert.equal(options.noWaitAfter, true);
        throw new Error('control detached before dispatch');
      },
    }, notDispatchedEvidence);

    assert.equal(failedBeforeDispatch, false);
    assert.equal(notDispatchedCalls, 1);
    assert.equal(await page.evaluate(() => window.notDispatchedClicks), 0);
    assert.equal(notDispatchedEvidence.submitAttempted, true);
    assert.equal(notDispatchedEvidence.submitClicked, false);
    assert.equal(notDispatchedEvidence.submitClickedAt, undefined);
    assert.equal(notDispatchedEvidence.submitOutcome, 'unknown');

    let unverifiedCalls = 0;
    const unverifiedEvidence = { submitAttempted: false, submitClicked: false };
    const unverified = await clickReviewedSubmitOnce(page, {
      evaluate: async () => { throw new Error('listener installation failed'); },
      click: async () => { unverifiedCalls += 1; },
    }, unverifiedEvidence);

    assert.equal(unverified, false);
    assert.equal(unverifiedCalls, 0);
    assert.equal(unverifiedEvidence.submitAttempted, false);
    assert.equal(unverifiedEvidence.submitClicked, false);
    assert.equal(unverifiedEvidence.submitClickedAt, undefined);
    assert.equal(unverifiedEvidence.submitOutcome, 'unverified');
  } finally {
    await browser.close();
  }
});

test('XHS final mode readback distinguishes the frozen graph and video composers', () => {
  const graph = { ...validInput(), content: { ...validInput().content, type: 'graph_text' } };
  const video = { ...validInput(), content: { ...validInput().content, type: 'video' } };
  const graphPage = { url: () => `${PLATFORM_RULES.xhs.composerURLs.graph_text}&draft=1` };
  const videoPage = { url: () => `${PLATFORM_RULES.xhs.composerURLs.video}&draft=1` };

  assert.equal(xhsComposerModeMatches(graphPage, graph, PLATFORM_RULES.xhs), true);
  assert.equal(xhsComposerModeMatches(videoPage, video, PLATFORM_RULES.xhs), true);
  assert.equal(xhsComposerModeMatches(videoPage, graph, PLATFORM_RULES.xhs), false);
  assert.equal(xhsComposerModeMatches(graphPage, video, PLATFORM_RULES.xhs), false);
  assert.equal(xhsComposerModeMatches({
    url: () => `${PLATFORM_RULES.xhs.composerURLs.graph_text}&target=video`,
  }, graph, PLATFORM_RULES.xhs), false);
});

test('XHS page closure after a resolved click keeps evidence and enters backend verification', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-close-after-click-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    let closeCalls = 0;
    await page.exposeFunction('closeAfterSubmit', () => {
      closeCalls += 1;
      setImmediate(() => page.close().catch(() => {}));
    });
    await page.route('https://creator.xiaohongshu.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: xhsClosedShadowSubmitFixture(`
        <main class="publish-page">
          <input placeholder="标题">
          <div id="body" contenteditable="true"></div>
          <input id="images" type="file" accept="image/png">
          <div id="upload-state"></div>
          ${xhsLivePolicyLandmarks({ aiText: '笔记含AI合成内容' })}
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        </main>
        <script>
          document.querySelector('#images').onchange = () => {
            const preview = document.createElement('img');
            preview.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=';
            preview.width = 320;
            preview.height = 420;
            document.querySelector('#upload-state').append(preview);
          };
          document.querySelector('xhs-publish-btn').onclick = () => window.closeAfterSubmit();
        </script>
      `),
    }));
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=image');
    const preparedInput = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '冻结标题', caption: '', body: '冻结正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        visibility: 'public', content_source: 'ai', allow_duet: false, allow_copy: false,
      },
    };
    const prepared = await prepareForm(page, preparedInput, PLATFORM_RULES.xhs);
    assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));

    const result = await submitPreparedPage(page, {
      ...preparedInput,
      action: 'submit',
      allowSubmit: true,
      settings: xhsFormalSettings(),
    }, prepared.evidence);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(closeCalls, 1);
    assert.equal(page.isClosed(), true);
    assert.equal(result.evidence.submitAttempted, true);
    assert.equal(result.evidence.submitClicked, true);
    assert.equal(result.evidence.submitOutcome, 'dispatched');
    assert.match(result.evidence.submitTargetId, /^[a-f0-9]{32}$/i);
    assert.ok(Number.isFinite(Date.parse(result.evidence.submitClickedAt)));
    assert.equal(result.evidence.postClickObservation, 'unavailable');
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('default-valued boolean and enum settings are read back instead of being skipped', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-defaults-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <input data-testid="visibility-public" type="radio" name="visibility" checked>
      <input data-testid="visibility-private" type="radio" name="visibility">
      <input data-testid="comment-enabled" type="checkbox" checked>
      <input data-testid="declare-original" type="checkbox">
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
    `));
    const input = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { visibility: 'public', comment_enabled: true, declare_original: false },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.settingsVerified.sort(), ['comment_enabled', 'declare_original', 'visibility']);
    assert.deepEqual(result.evidence.settingsSkipped, []);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS applies and reads back the AI content-source declaration from the live label', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-source-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <div class="custom-select-44"><div class="d-select-content">添加地点</div></div>
      <div class="custom-select-44"><div class="d-select-content">选择群聊</div></div>
      <div class="custom-select-44"><div class="d-select-content">公开可见</div></div>
      <div class="publish-page-content-setting-content">
        <div class="custom-select-44"><div id="source-trigger" class="d-select-content">添加内容类型声明</div></div>
      </div>
      <section id="source-options" class="declaration-drop-down" hidden>
        <div id="source-ai" class="d-grid-item">笔记含AI合成内容</div>
      </section>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        document.querySelector('#source-trigger').onclick = () => {
          document.querySelector('#source-options').hidden = false;
        };
        document.querySelector('#source-ai').onclick = () => {
          document.querySelector('#source-trigger').textContent = '笔记含AI合成内容';
          document.querySelector('#source-options').hidden = true;
        };
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const input = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { content_source: 'ai' },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.locator('#source-trigger').innerText(), '笔记含AI合成内容');
    assert.deepEqual(result.evidence.settingsApplied, ['content_source']);
    assert.deepEqual(result.evidence.settingsSkipped, []);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS does not accept a custom AI declaration option click without selected-text readback', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-source-readback-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <div class="publish-page-content-setting-content">
        <div class="custom-select-44"><div id="source-trigger" class="d-select-content">添加内容类型声明</div></div>
      </div>
      <section id="source-options" class="declaration-drop-down" hidden>
        <div id="source-ai" class="d-grid-item">笔记含AI合成内容</div>
      </section>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.optionClicks = 0;
        window.submitClicks = 0;
        document.querySelector('#source-trigger').onclick = () => {
          document.querySelector('#source-options').hidden = false;
        };
        document.querySelector('#source-ai').onclick = () => {
          window.optionClicks += 1;
          document.querySelector('#source-options').hidden = true;
        };
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { content_source: 'ai' },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.ok(await page.evaluate(() => window.optionClicks) >= 1);
    assert.deepEqual(result.evidence.unverifiedFields, ['settings.content_source']);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS reads back the live 公开可见 visibility label', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-visibility-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <div class="permission-card-select"><div class="d-select-content">公开可见</div></div>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { visibility: 'public' },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.settingsVerified, ['visibility']);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS disables and reads back the live duet and body-copy switches', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-switches-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <div class="custom-switch-wrapper">
        <div class="custom-switch-text-content"><span>允许合拍</span></div>
        <div class="custom-switch-switch">
          <button id="duet-switch" class="d-switch-simulator checked"></button>
          <input id="duet-state" type="checkbox" checked hidden>
        </div>
      </div>
      <div class="custom-switch-wrapper">
        <div class="custom-switch-text-content"><span>允许正文复制</span></div>
        <div class="custom-switch-switch">
          <button id="copy-switch" class="d-switch-simulator checked"></button>
          <input id="copy-state" type="checkbox" checked hidden>
        </div>
      </div>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        for (const control of document.querySelectorAll('.d-switch-simulator')) {
          control.addEventListener('click', () => {
            const input = control.parentElement.querySelector('input[type=checkbox]');
            input.checked = !input.checked;
            control.classList.toggle('checked', input.checked);
            control.classList.toggle('unchecked', !input.checked);
          });
        }
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { allow_duet: false, allow_copy: false },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.locator('#duet-state').isChecked(), false);
    assert.equal(await page.locator('#copy-state').isChecked(), false);
    assert.equal(await page.locator('#duet-switch').evaluate((node) => node.classList.contains('unchecked')), true);
    assert.equal(await page.locator('#copy-switch').evaluate((node) => node.classList.contains('unchecked')), true);
    assert.deepEqual(result.evidence.settingsApplied.sort(), ['allow_copy', 'allow_duet']);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS switch controls fail closed when the live label is ambiguous or state is unknown', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-switch-fail-closed-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    for (const fixture of [
      {
        name: 'ambiguous',
        controls: `
          <div class="custom-switch-wrapper"><div class="custom-switch-text-content"><span>允许合拍</span></div><div class="custom-switch-switch"><button class="d-switch-simulator checked"></button><input type="checkbox" checked hidden></div></div>
          <div class="custom-switch-wrapper"><div class="custom-switch-text-content"><span>允许合拍</span></div><div class="custom-switch-switch"><button class="d-switch-simulator checked"></button><input type="checkbox" checked hidden></div></div>
        `,
      },
      {
        name: 'unknown',
        controls: '<div class="custom-switch-wrapper"><div class="custom-switch-text-content"><span>允许合拍</span></div><div class="custom-switch-switch"><button class="d-switch-simulator checked"></button></div></div>',
      },
    ]) {
      const page = await browser.newPage();
      await page.setContent(xhsScopedImageComposerFixture(`
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <input id="images" type="file" accept="image/png" multiple>
        ${fixture.controls}
        <xhs-publish-btn role="button">发布</xhs-publish-btn>
        <script>
          window.submitClicks = 0;
          document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
        </script>
      `));
      const result = await prepareForm(page, {
        ...validInput(),
        platform: 'xhs',
        content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
        media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
        settings: { allow_duet: false },
      }, PLATFORM_RULES.xhs);

      assert.equal(result.status, 'manual_takeover', `${fixture.name}: ${JSON.stringify(result)}`);
      assert.deepEqual(result.evidence.unverifiedFields, ['settings.allow_duet'], fixture.name);
      assert.equal(await page.evaluate(() => window.submitClicks), 0, fixture.name);
      await page.close();
    }
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS fails closed when the requested AI content-source control is ambiguous', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-source-ambiguous-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <button id="source-trigger">内容来源声明</button>
      <section id="source-options" hidden>
        <input id="source-ai-a" type="radio" name="content-source-a" aria-label="笔记含AI合成内容">
        <input id="source-ai-b" type="radio" name="content-source-b" aria-label="笔记含AI合成内容">
      </section>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        document.querySelector('#source-trigger').onclick = () => {
          document.querySelector('#source-options').hidden = false;
        };
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const input = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { content_source: 'ai' },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.deepEqual(result.evidence.unverifiedFields, ['settings.content_source']);
    assert.equal(await page.locator('#source-ai-a').isChecked(), false);
    assert.equal(await page.locator('#source-ai-b').isChecked(), false);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS fails closed when a default-valued boolean readback is ambiguous', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-default-ambiguous-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <input type="checkbox" role="switch" aria-label="允许评论" checked>
      <input type="checkbox" role="switch" aria-label="允许评论" checked>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
    `));
    const input = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { comment_enabled: true },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.deepEqual(result.evidence.unverifiedFields, ['settings.comment_enabled']);
    assert.deepEqual(result.evidence.settingsSkipped, []);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS records absent default comment and original controls only on the verified live composer', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-absent-defaults-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const context = await browser.newContext();
    const composerURL = 'https://creator.xiaohongshu.com/publish/publish?fixture=absence';
    await context.route(composerURL, (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: xhsScopedImageComposerFixture(`
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <input id="images" type="file" accept="image/png" multiple>
        ${xhsLivePolicyLandmarks()}
        <xhs-publish-btn role="button">发布</xhs-publish-btn>
      `),
    }));
    const page = await context.newPage();
    await page.goto(composerURL);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { comment_enabled: true, declare_original: false },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.settingsNotPresent.sort(), ['comment_enabled', 'declare_original']);
    assert.deepEqual(result.evidence.settingsApplied, []);
    assert.deepEqual(result.evidence.settingsSkipped, []);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS video verifies unavailable interaction controls and the no-declaration default on its complete composer', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-video-defaults-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const context = await browser.newContext();
    const composerURL = 'https://creator.xiaohongshu.com/publish/publish?from=menu&target=video';
    await context.route(composerURL, (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: xhsClosedShadowSubmitFixture(`
        <main class="publish-page">
          <input placeholder="标题">
          <textarea placeholder="正文"></textarea>
          <input id="video" type="file" accept="video/mp4">
          <div id="upload-state"></div>
          <div class="permission-card-select"><div class="d-select-content">公开可见</div></div>
          <div class="publish-page-content-setting-content">
            <div class="custom-select-44"><div class="d-select-content">添加内容类型声明</div></div>
          </div>
          <span>定时发布</span>
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        </main>
        <script>
          document.querySelector('#video').onchange = () => {
            document.querySelector('#upload-state').textContent =
              '默认截取第一帧作为封面，优质的封面会吸引更多人浏览笔记';
          };
        </script>
      `),
    }));
    const page = await context.newPage();
    await page.goto(composerURL);
    const result = await prepareForm(page, {
      ...validInput(),
      timeoutMs: 2000,
      platform: 'xhs',
      content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
      media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        visibility: 'public',
        comment_enabled: true,
        declare_original: false,
        allow_duet: false,
        allow_copy: false,
        content_source: 'none',
      },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(
      result.evidence.settingsNotPresent.sort(),
      ['allow_copy', 'allow_duet', 'comment_enabled', 'declare_original'],
    );
    assert.deepEqual(result.evidence.settingsVerified.sort(), ['content_source', 'visibility']);
    assert.deepEqual(result.evidence.unverifiedFields, []);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS absent defaults remain unverified off-composer or with ambiguous landmarks', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-absence-guard-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    for (const fixture of [
      { name: 'off-composer', landmarks: xhsLivePolicyLandmarks(), composer: false },
      { name: 'ambiguous-landmark', landmarks: xhsLivePolicyLandmarks({ duplicateDuet: true }), composer: true },
    ]) {
      const context = await browser.newContext();
      const page = await context.newPage();
      const html = xhsScopedImageComposerFixture(`
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <input id="images" type="file" accept="image/png" multiple>
        ${fixture.landmarks}
        <xhs-publish-btn role="button">发布</xhs-publish-btn>
      `);
      if (fixture.composer) {
        const url = `https://creator.xiaohongshu.com/publish/publish?fixture=${fixture.name}`;
        await context.route(url, (route) => route.fulfill({ contentType: 'text/html; charset=utf-8', body: html }));
        await page.goto(url);
      } else {
        await page.setContent(html);
      }
      const result = await prepareForm(page, {
        ...validInput(),
        platform: 'xhs',
        content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
        media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
        settings: { comment_enabled: true, declare_original: false },
      }, PLATFORM_RULES.xhs);

      assert.equal(result.status, 'manual_takeover', `${fixture.name}: ${JSON.stringify(result)}`);
      assert.deepEqual(
        result.evidence.unverifiedFields.sort(),
        ['settings.comment_enabled', 'settings.declare_original'],
        fixture.name,
      );
      assert.deepEqual(result.evidence.settingsNotPresent, [], fixture.name);
      await context.close();
    }
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS absent controls do not satisfy opposite comment or original intentions', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-absence-opposite-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const context = await browser.newContext();
    const composerURL = 'https://creator.xiaohongshu.com/publish/publish?fixture=opposite';
    await context.route(composerURL, (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: xhsScopedImageComposerFixture(`
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <input id="images" type="file" accept="image/png" multiple>
        ${xhsLivePolicyLandmarks()}
        <xhs-publish-btn role="button">发布</xhs-publish-btn>
      `),
    }));
    const page = await context.newPage();
    await page.goto(composerURL);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { comment_enabled: false, declare_original: true },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.deepEqual(result.evidence.unverifiedFields.sort(), ['settings.comment_enabled', 'settings.declare_original']);
    assert.deepEqual(result.evidence.settingsNotPresent, []);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('explicit empty settings clear reviewed controls or report the control as not present', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-empty-settings-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const input = {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '标题', caption: '正文', body: '', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { activity: '', collection: '', hotspot: '', music: '' },
    };
    const pageWithOldValues = await browser.newPage();
    await pageWithOldValues.setContent(`
      <input placeholder="标题">
      <div contenteditable="true"></div>
      <input type="file" accept="image/png">
      <input placeholder="参与活动" value="旧活动">
      <input placeholder="合集" value="旧合集">
      <input placeholder="热点" value="旧热点">
      <input placeholder="音乐" value="旧音乐">
      <button>发布</button>
    `);

    const cleared = await prepareForm(pageWithOldValues, input, PLATFORM_RULES.douyin);

    assert.equal(cleared.status, 'awaiting_review', JSON.stringify(cleared));
    assert.deepEqual(cleared.evidence.settingsApplied.sort(), ['activity', 'collection', 'hotspot', 'music']);
    assert.deepEqual(cleared.evidence.settingsSkipped, []);
    assert.deepEqual(cleared.evidence.settingsNotPresent, []);
    assert.deepEqual(
      await pageWithOldValues.locator("input[placeholder='参与活动'],input[placeholder='合集'],input[placeholder='热点'],input[placeholder='音乐']").evaluateAll(
        (nodes) => nodes.map((node) => node.value),
      ),
      ['', '', '', ''],
    );

    const pageWithoutControls = await browser.newPage();
    await pageWithoutControls.setContent(`
      <input placeholder="标题">
      <div contenteditable="true"></div>
      <input type="file" accept="image/png">
      <button>发布</button>
    `);
    const absent = await prepareForm(pageWithoutControls, input, PLATFORM_RULES.douyin);

    assert.equal(absent.status, 'awaiting_review', JSON.stringify(absent));
    assert.deepEqual(absent.evidence.settingsApplied, []);
    assert.deepEqual(absent.evidence.settingsSkipped, []);
    assert.deepEqual(absent.evidence.settingsNotPresent.sort(), ['activity', 'collection', 'hotspot', 'music']);
    assert.deepEqual(absent.evidence.unverifiedFields, []);

    const pageWithUnresolvedTrigger = await browser.newPage();
    await pageWithUnresolvedTrigger.setContent(`
      <input placeholder="标题">
      <div contenteditable="true"></div>
      <input type="file" accept="image/png">
      <button>添加合集</button>
      <button>发布</button>
    `);
    const unresolved = await prepareForm(pageWithUnresolvedTrigger, {
      ...input,
      settings: { collection: '' },
    }, PLATFORM_RULES.douyin);

    assert.equal(unresolved.status, 'manual_takeover', JSON.stringify(unresolved));
    assert.equal(unresolved.step, 'form_unverified');
    assert.deepEqual(unresolved.evidence.settingsNotPresent, []);
    assert.deepEqual(unresolved.evidence.unverifiedFields, ['settings.collection']);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin video verifies an absent default comment control only against complete video landmarks', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: `
        <input placeholder="添加作品标题">
        <div contenteditable="true"></div>
        <label><input type="radio" name="visibility" checked>公开</label>
        <label><input type="radio" name="visibility">好友可见</label>
        <label><input type="radio" name="visibility">仅自己可见</label>
        <label><input type="radio" name="download">允许</label>
        <label><input type="radio" name="download" checked>不允许</label>
        <label><input type="radio" name="schedule">立即发布</label>
        <label><input type="radio" name="schedule" checked>定时发布</label>
        <div class="semi-select-selection-text">请选择合集</div>
        <div class="selectText-XSrMFZ">无需添加自主声明</div>
        <div style="display:none">允许评论</div>
        <div style="display:none"><button role="switch" aria-label="允许评论"></button></div>
        <button>发布</button>
      `,
    }));
    await page.goto('https://creator.douyin.com/creator-micro/content/post/video?enter_from=publish_page');
    const verified = await applySettings(page, {
      content: { type: 'video' },
      settings: { collection: '', comment_enabled: true },
    }, PLATFORM_RULES.douyin);
    assert.deepEqual(verified.notPresent, ['comment_enabled']);
    assert.deepEqual(verified.verified, ['collection']);
    assert.deepEqual(verified.unverified, []);

    await page.getByRole('button', { name: '发布', exact: true }).evaluate((node) => node.remove());
    const incomplete = await applySettings(page, {
      content: { type: 'video' },
      settings: { comment_enabled: true },
    }, PLATFORM_RULES.douyin);
    assert.deepEqual(incomplete.notPresent, []);
    assert.deepEqual(incomplete.unverified, ['settings.comment_enabled']);
  } finally {
    await browser.close();
  }
});

test('Douyin video rechecks an absent comment control after native scheduling settles', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-douyin-schedule-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const page = await browser.newPage();
    await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: `
        <input placeholder="添加作品标题">
        <textarea placeholder="描述"></textarea>
        <input id="video" type="file" accept="video/mp4">
        <label><input type="radio" name="visibility" checked>公开</label>
        <label><input type="radio" name="visibility">好友可见</label>
        <label><input type="radio" name="visibility">仅自己可见</label>
        <label><input type="radio" name="download" checked>允许</label>
        <label><input type="radio" name="download">不允许</label>
        <label><input type="radio" name="schedule">立即发布</label>
        <label><input type="radio" name="schedule">定时发布</label>
        <input id="scheduled-at" type="text" placeholder="日期和时间"
          format="yyyy-MM-dd HH:mm" hidden>
        <div class="semi-select-selection-text">不选择合集</div>
        <div class="selectText-XSrMFZ">无需添加自主声明</div>
        <button id="publish">发布</button>
        <script>
          window.submitClicks = 0;
          const makeExclusive = (selector) => {
            const controls = [...document.querySelectorAll(selector)];
            for (const control of controls) {
              control.addEventListener('change', () => {
                if (!control.checked) return;
                for (const other of controls) other.checked = other === control;
              });
            }
          };
          makeExclusive('input[name="visibility"]');
          makeExclusive('input[name="download"]');
          makeExclusive('input[name="schedule"]');
          document.querySelector('#video').onchange = () => {
            const preview = document.createElement('canvas');
            preview.width = 120;
            preview.height = 100;
            document.body.append(preview);
          };
          const scheduled = [...document.querySelectorAll('input[name="schedule"]')]
            .find((control) => control.labels[0].innerText.trim() === '定时发布');
          scheduled.addEventListener('change', () => {
            document.querySelector('#scheduled-at').hidden = !scheduled.checked;
          });
          document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
        </script>
      `,
    }));
    await page.goto('https://creator.douyin.com/creator-micro/content/upload');
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [{
        path: videoPath,
        kind: 'video',
        role: 'video',
        position: 0,
        sha256: fileSHA256(videoPath),
      }],
      settings: {
        allow_download: false,
        visibility: 'public',
        comment_enabled: true,
        self_declaration: 'none',
      },
      publishAt: '2030-07-30T19:43:00+08:00',
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.ok(result.evidence.settingsNotPresent.includes('comment_enabled'));
    assert.ok(!result.evidence.unverifiedFields.includes('settings.comment_enabled'));
    assert.equal(result.evidence.publishTimeApplied, true);
    assert.equal(await page.getByRole('radio', { name: '定时发布', exact: true }).isChecked(), true);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin uploads the frozen cover through the reviewed upload-cover button', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-douyin-cover-'));
  try {
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(coverPath, 'cover-fixture');
    const page = await browser.newPage();
    await page.setContent(`
      <button id="open">选择封面</button>
      <div id="cover-dialog" role="dialog" hidden>
        <span>AI生成封面</span>
        <input id="unrelated" type="file" accept="image/png">
        <canvas id="vertical_coverCanvas" width="4" height="4" style="width:160px;height:160px"></canvas>
        <div id="upload-area">
          <div id="upload-copy"><label id="upload" for="cover">上传封面</label></div>
          <input id="cover" class="semi-upload-hidden-input" type="file" accept="image/png">
          <input class="semi-upload-hidden-input-replace" type="file" accept="image/png">
        </div>
        <button id="done" type="button" disabled>完成</button>
      </div>
      <script>
        const preview = document.querySelector('#vertical_coverCanvas');
        const previewContext = preview.getContext('2d');
        previewContext.fillStyle = '#000';
        previewContext.fillRect(0, 0, preview.width, preview.height);
        document.querySelector('#open').onclick = () => {
          document.querySelector('#cover-dialog').hidden = false;
        };
        document.querySelector('#cover').onchange = () => {
          setTimeout(() => {
            document.querySelector('#done').disabled = false;
          }, 250);
          document.querySelector('#cover').value = '';
        };
        document.querySelector('#done').onclick = () => { document.querySelector('#cover-dialog').hidden = true; };
      </script>
    `);
    const result = await applyNativeVideoCover(page, [{
      path: coverPath,
      kind: 'image',
      role: 'cover',
      position: 1,
      sha256: crypto.createHash('sha256').update(fs.readFileSync(coverPath)).digest('hex'),
    }], {
      triggerNames: ['选择封面'],
      panelMarkerNames: ['AI生成封面'],
      uploadTriggerNames: ['上传封面'],
      uploadInputAncestorLevels: 2,
      uploadInputSelectors: ['input.semi-upload-hidden-input:not(.semi-upload-hidden-input-replace)'],
      useVerifiedFilePaths: true,
      previewCanvasSelectors: ['#vertical_coverCanvas'],
      requirePreviewCanvasChange: false,
      uploadProcessingTimeoutMs: 2000,
      confirmNames: ['完成'],
    });

    assert.equal(result.applied, true, JSON.stringify(result));
    assert.equal(result.method, 'cover_upload_dialog');
    assert.equal(result.selectionVerified, true);
    assert.equal(result.previewChanged, false);
    assert.equal(await page.locator('#cover-dialog').isVisible(), false);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin revalidates the committed vertical cover on the prepared page', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <section>
        <span>设置封面</span>
        <div><img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==" alt="" style="width:120px;height:160px"></div>
        <span>竖封面3:4</span>
      </section>
    `);
    assert.equal(await nativeVideoCoverPreviewCommitted(page, {
      committedPreviewNames: ['竖封面3:4'],
      committedPreviewAncestorLevels: 3,
    }), true);
    await page.locator('img').evaluate((node) => node.removeAttribute('src'));
    assert.equal(await nativeVideoCoverPreviewCommitted(page, {
      committedPreviewNames: ['竖封面3:4'],
      committedPreviewAncestorLevels: 3,
    }), false);
  } finally {
    await browser.close();
  }
});

test('formal settings without reviewed selectors remain explicit and fail closed', async () => {
  const browser = await launchChromium();
  try {
    const tests = [
      {
        platform: 'douyin',
        html: '<input placeholder="标题"><div contenteditable="true"></div><button>发布</button>',
        settings: { comment_enabled: true, self_declaration: 'ai' },
        unsupported: [],
        unverified: ['comment_enabled', 'self_declaration'],
      },
      {
        platform: 'kuaishou',
        html: '<div contenteditable="true"></div><div>发布</div>',
        settings: { comment_enabled: true },
        unsupported: [],
        unverified: ['comment_enabled'],
      },
      {
        platform: 'tiktok',
        html: '<input placeholder="title"><div class="public-DraftEditor-content" contenteditable="true"></div><button>Post</button>',
        settings: { allow_download: false },
        unsupported: [],
        unverified: ['allow_download'],
      },
    ];
    for (const fixture of tests) {
      const page = await browser.newPage();
      await page.setContent(fixture.html);
      const result = await prepareForm(page, {
        ...validInput(),
        platform: fixture.platform,
        content: { type: fixture.platform === 'tiktok' ? 'video' : 'video', title: '标题', caption: '正文', body: '', tags: [] },
        settings: fixture.settings,
      }, PLATFORM_RULES[fixture.platform]);

      assert.equal(result.status, 'manual_takeover', `${fixture.platform}: ${JSON.stringify(result)}`);
      assert.equal(result.step, 'form_unverified', fixture.platform);
      assert.deepEqual(result.evidence.settingsUnsupported.sort(), fixture.unsupported, fixture.platform);
      assert.deepEqual(
        result.evidence.unverifiedFields.sort(),
        (fixture.unverified || fixture.unsupported).map((key) => `settings.${key}`).sort(),
        `${fixture.platform}: ${JSON.stringify(result)}`,
      );
      await page.close();
    }
  } finally {
    await browser.close();
  }
});

test('reviewed article platforms accept frozen submit actions', () => {
  assert.doesNotThrow(() => validateInput({ ...validInput(), platform: 'csdn', action: 'submit', allowSubmit: true }));
  assert.doesNotThrow(() => validateInput({ ...validInput(), platform: 'wechat_mp', action: 'submit', allowSubmit: true }));
  assert.doesNotThrow(() => validateInput({ ...validInput(), platform: 'wechat_mp', action: 'submit_prepared', allowSubmit: true }));
});

test('unknown fields and non-loopback CDP fail closed', () => {
  assert.throws(() => validateInput({ ...validInput(), unknown: true }), /INPUT_UNKNOWN_FIELD/);
  assert.throws(() => validateInput({ ...validInput(), cdpUrl: 'https://example.com:15106/' }), /CDP_URL_INVALID/);
  assert.throws(() => validateInput({ ...validInput(), settings: { arbitrary: true } }), /SETTINGS_UNKNOWN_FIELD/);
});

test('current four-platform setting fields pass strict validation', () => {
  const input = {
    ...validInput(),
    platform: 'xhs',
    content: { type: 'graph_text', title: 'title', caption: '', body: 'body', tags: [] },
    media: [{ path: '/tmp/image.png', kind: 'image', role: 'content', position: 0, sha256: 'a'.repeat(64) }],
    settings: {
      location: '上海市徐汇区', collection: '', group: '', declare_original: false,
      content_source: 'ai', related_media: '', visibility: 'friends',
      comment_enabled: true, allow_duet: false, allow_copy: false,
    },
  };
  assert.equal(validateInput(input).settings.location, '上海市徐汇区');
  assert.equal(validateInput(input).settings.allow_duet, false);
  assert.equal(validateInput(input).settings.allow_copy, false);

  const douyin = {
    ...validInput(),
    platform: 'douyin',
    content: { type: 'video', title: 'title', caption: 'body', body: '', tags: [] },
    media: [{ path: '/tmp/video.mp4', kind: 'video', role: 'video', position: 0, sha256: 'b'.repeat(64) }],
    settings: {
      activity: '', collection: '', extension_type: 'location', location: '上海',
      hotspot: '', allow_download: true, visibility: 'public', music: '',
      comment_enabled: true, self_declaration: 'ai',
    },
  };
  assert.equal(validateInput(douyin).settings.extension_type, 'location');

  const kuaishou = {
    ...douyin,
    platform: 'kuaishou',
    content: { type: 'video', title: '', caption: 'body', body: '', tags: [] },
    settings: {
      link_applet: '', declaration: 'ai', collection: '', location: '杭州',
      allow_duet: true, allow_download: true, same_city: true, visibility: 'friends', music: '',
      comment_enabled: true,
    },
  };
  assert.equal(validateInput(kuaishou).settings.declaration, 'ai');

  const wechatVideo = {
    ...douyin,
    platform: 'wechat_video',
    settings: {
      location: 'auto', collection: '', link_type: 'article', link_address: '公众号文章',
      activity: '', declare_original: false, music: '',
    },
  };
  assert.equal(validateInput(wechatVideo).settings.link_type, 'article');

  const wechat = {
    ...validInput(),
    platform: 'wechat_mp',
    content: { type: 'article', title: 'title', caption: '', body: 'body', tags: [] },
    media: [],
    settings: {
      author: 'Alpha Nexus', original_link: '', comment_enabled: true, declare_original: true,
      allow_reprint: true, publish_type: 'mass', collection: '', content_source: 'none',
    },
  };
  assert.equal(validateInput(wechat).settings.author, 'Alpha Nexus');

  const csdn = {
    ...wechat,
    platform: 'csdn',
    settings: {
      collection: '', article_type: 'original', original_link: '', backup_gitcode: false,
      visibility: 'public', activity: '', topic: '',
    },
  };
  assert.equal(validateInput(csdn).settings.article_type, 'original');
});

test('CDP input accepts the 500th local port and rejects the next port', () => {
  assert.equal(validateInput({ ...validInput(), cdpUrl: 'http://127.0.0.1:15600/' }).cdpUrl, 'http://127.0.0.1:15600/');
  assert.throws(() => validateInput({ ...validInput(), cdpUrl: 'http://127.0.0.1:15601/' }), /CDP_URL_INVALID/);
});

test('reviewed live controls cover custom XHS submit and Douyin onboarding', () => {
  assert.deepEqual(PLATFORM_RULES.xhs.submitSelectors, ['xhs-publish-btn']);
  assert.deepEqual(PLATFORM_RULES.douyin.dismissNames, ['我知道了']);
  assert.deepEqual(PLATFORM_RULES.csdn.body, ["pre.editor__inner[contenteditable='true']", '.CodeMirror textarea']);
  assert.deepEqual(PLATFORM_RULES.wechat_mp.body, [".mock-iframe-body .ProseMirror[contenteditable='true']"]);
  assert.equal(submitSelectorCandidates(PLATFORM_RULES.xhs)[0], 'xhs-publish-btn');
});

test('direct-composer preparation reuses the current account tab', async () => {
  const browser = await launchChromium();
  try {
    const context = await browser.newContext();
    const original = await context.newPage();
    await original.goto('data:text/html,existing-user-tab');
    const prepared = await prepareTargetPage(original.context(), PLATFORM_RULES.csdn);
    assert.equal(prepared, original);
    assert.equal(original.url(), 'data:text/html,existing-user-tab');
    assert.equal(original.context().pages().length, 1);
  } finally {
    await browser.close();
  }
});

test('direct-composer preparation reuses an existing reviewed composer tab', async () => {
  const browser = await launchChromium();
  try {
    const context = await browser.newContext();
    await context.route(PLATFORM_RULES.wechat_video.composerUrl, (route) => route.fulfill({
      contentType: 'text/html', body: '<title>reviewed composer</title>',
    }));
    const existing = await context.newPage();
    await existing.goto(PLATFORM_RULES.wechat_video.composerUrl);

    const prepared = await prepareTargetPage(context, PLATFORM_RULES.wechat_video);

    assert.equal(prepared, existing);
    assert.equal(context.pages().length, 1);
  } finally {
    await browser.close();
  }
});

test('preparation prefers the newest reviewed composer when stale drafts remain open', async () => {
  const stale = { url: () => `${PLATFORM_RULES.xhs.composerUrl}?draft=stale` };
  const current = { url: () => `${PLATFORM_RULES.xhs.composerUrl}?draft=current` };
  assert.equal(newestMatchingPage([stale, current], [PLATFORM_RULES.xhs.composerUrl]), current);
});

test('XHS graph-text preparation reuses the active account tab before navigation', async () => {
  const graphComposer = {
    isClosed: () => false,
    url: () => 'https://creator.xiaohongshu.com/publish/publish?from=menu&target=image',
  };
  const videoComposer = {
    isClosed: () => false,
    url: () => 'https://creator.xiaohongshu.com/publish/publish?from=menu&target=video',
  };
  const selected = await prepareTargetPage(
    { pages: () => [graphComposer, videoComposer] },
    PLATFORM_RULES.xhs,
    'graph_text',
  );
  assert.equal(selected, videoComposer);
});

test('Douyin graph-text preparation rejects a direct jump that bypasses the reviewed upload page', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const context = await browser.newContext();
    await context.route('https://creator.douyin.com/creator-micro/home', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <script>
          setTimeout(() => {
            const card = document.createElement('div');
            card.textContent = '发布图文支持常用图片格式，如png/jpg等';
            card.onclick = () => { location.href = '/creator-micro/content/post/image'; };
            document.body.appendChild(card);
          }, 500);
        </script>
      `,
    }));
    await context.route('https://creator.douyin.com/creator-micro/content/post/image', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <input id="images" type="file" accept="image/png" multiple>
        ${topicPickerFixture('#添加话题')}
        <button id="publish">发布</button>
        <script>
          window.submitClicks = 0;
          document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
        </script>
      `,
    }));
    const selected = await prepareTargetPage(context, PLATFORM_RULES.douyin, 'graph_text');
    const result = await preparePage(selected, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '图文标题', caption: '图文描述', body: '', tags: ['联调'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    });

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.step, 'prepared_page_missing', JSON.stringify(result));
    assert.equal(result.evidence.entryLocation, 'https://creator.douyin.com/creator-micro/content/upload');
    assert.equal(fs.existsSync(result.evidence.screenshotPath), true, JSON.stringify(result));
    assert.equal(await selected.evaluate(() => window.submitClicks ?? 0), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin graph-text preparation follows the reviewed intermediate upload page', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-intermediate-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const context = await browser.newContext();
    await context.route('https://creator.douyin.com/creator-micro/home', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: '<p>创作者首页加载完成</p>',
    }));
    await context.route('https://creator.douyin.com/creator-micro/content/upload?default-tab=3', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <button>上传图文</button>
        <input id="intermediate-images" type="file" accept="image/png" multiple>
        <script>
          document.querySelector('#intermediate-images').onchange = (event) => {
            const files = [...event.currentTarget.files]
              .map((file) => ({ name: file.name, size: file.size }));
            sessionStorage.setItem('entryFiles', JSON.stringify(files));
            sessionStorage.setItem(
              'entryUploadChangeCount',
              String(Number(sessionStorage.getItem('entryUploadChangeCount') || '0') + 1),
            );
            setTimeout(() => { location.href = '/creator-micro/content/post/image'; }, 50);
          };
        </script>
      `,
    }));
    await context.route('https://p0-creator-media-private.douyin.com/**', (route) => route.fulfill({
      contentType: 'image/png',
      body: Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=',
        'base64',
      ),
    }));
    await context.route('https://creator.douyin.com/creator-micro/content/post/image', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <input placeholder="标题">
        <div id="body" contenteditable="true"></div>
        <div class="ant-carousel"><img id="uploaded-preview" width="243" height="324" src="https://p0-creator-media-private.douyin.com/fixture/content.png"></div>
        ${topicPickerFixture('#添加话题')}
        <button id="publish">发布</button>
        <script>
          window.submitClicks = 0;
          document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
        </script>
      `,
    }));
    const selected = await prepareTargetPage(context, PLATFORM_RULES.douyin, 'graph_text');
    const result = await preparePage(selected, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '图文标题', caption: '图文描述', body: '', tags: ['联调'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: fileSHA256(imagePath) }],
    });
    const composer = context.pages().find((candidate) => (
      candidate.url().startsWith('https://creator.douyin.com/creator-micro/content/post/image')
    ));

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.location, 'https://creator.douyin.com/creator-micro/content/post/image');
    assert.ok(composer);
    assert.equal(selected, composer);
    assert.equal(await composer.locator("input[placeholder='标题']").inputValue(), '图文标题');
    assert.equal(await composer.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.contentImagesUploadedSelectionVerified, true);
    assert.equal(result.evidence.contentImagesUploadedPreviewObserved, true);
    assert.equal(result.evidence.contentImagesUploadedSHA256Verified, true);
    assert.deepEqual(
      result.evidence.contentImagesUploadedSHA256s,
      [fileSHA256(imagePath)],
    );
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin upload evidence rejects a large off-domain placeholder', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div class="ant-carousel">
        <img width="320" height="420"
          src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=">
      </div>
    `);
    const rule = {
      ...PLATFORM_RULES.douyin,
      ...PLATFORM_RULES.douyin.entryFlows.graph_text,
    };
    const snapshot = await reviewedUploadSnapshot(page, rule);

    assert.equal(snapshot.scopeVerified, true);
    assert.deepEqual(snapshot.fingerprints, []);
  } finally {
    await browser.close();
  }
});

test('inspection reports only upload preview host and counts', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div class="ant-carousel"><img width="320" height="420" src="https://p0-creator-media-private.douyin.com/fixture/content.png?private=value"></div>
    `);
    const inspection = await inspectPage(page, {
      ...PLATFORM_RULES.douyin,
      ...PLATFORM_RULES.douyin.entryFlows.graph_text,
    });
    assert.deepEqual(inspection.uploadPreview, {
      scopeVisible: true,
      candidateCount: 1,
      matchingSourceCount: 1,
      hosts: ['p0-creator-media-private.douyin.com'],
    });
    assert.ok(!JSON.stringify(inspection.uploadPreview).includes('private=value'));
  } finally {
    await browser.close();
  }
});

test('Douyin graph-text full entry submit uploads once and clicks the exact final control once', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-entry-submit-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const context = await browser.newContext();
    await context.route('https://creator.douyin.com/creator-micro/home', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: '<a href="/creator-micro/content/upload?default-tab=3">发布图文</a>',
    }));
    await context.route('https://creator.douyin.com/creator-micro/content/upload?default-tab=3', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <button>上传图文</button>
        <input id="intermediate-images" type="file" accept="image/png" multiple>
        <script>
          document.querySelector('#intermediate-images').onchange = (event) => {
            const files = [...event.currentTarget.files]
              .map((file) => ({ name: file.name, size: file.size }));
            sessionStorage.setItem('entryFiles', JSON.stringify(files));
            sessionStorage.setItem(
              'entryUploadChangeCount',
              String(Number(sessionStorage.getItem('entryUploadChangeCount') || '0') + 1),
            );
            setTimeout(() => { location.href = '/creator-micro/content/post/image'; }, 50);
          };
        </script>
      `,
    }));
    await context.route('https://p0-creator-media-private.douyin.com/**', (route) => route.fulfill({
      contentType: 'image/png',
      body: Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII=',
        'base64',
      ),
    }));
    await context.route('https://creator.douyin.com/creator-micro/content/post/image', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <div class="ant-carousel"></div>
        ${douyinLiveGraphTextComposerFixture()}
        <script>
          const entryFiles = JSON.parse(sessionStorage.getItem('entryFiles') || '[]');
          for (const [index, file] of entryFiles.entries()) {
            const image = document.createElement('img');
            image.width = 243;
            image.height = 324;
            image.dataset.fileName = file.name;
            image.dataset.fileSize = String(file.size);
            image.src = 'https://p0-creator-media-private.douyin.com/fixture/' + index + '.png';
            document.querySelector('.ant-carousel').append(image);
          }
        </script>
      `,
    }));
    const selected = await prepareTargetPage(context, PLATFORM_RULES.douyin, 'graph_text');
    const result = await preparePage(selected, douyinLiveSubmitInput(imagePath));
    const composer = context.pages().find((candidate) => (
      candidate.url().startsWith('https://creator.douyin.com/creator-micro/content/post/image')
    ));

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'creator_backend_verification');
    assert.ok(composer);
    assert.equal(await composer.evaluate(() => window.submitClicks), 1);
    assert.equal(await composer.evaluate(() => sessionStorage.getItem('entryUploadChangeCount')), '1');
    assert.deepEqual(
      await composer.evaluate(() => JSON.parse(sessionStorage.getItem('entryFiles') || '[]')),
      [{ name: 'content.png', size: fs.statSync(imagePath).size }],
    );
    assert.equal(result.evidence.contentImagesUploadedSHA256Verified, true);
    assert.equal(result.evidence.criticalRevalidation.upload, true);
    assert.equal(result.evidence.submitAttempted, true);
    assert.equal(result.evidence.submitClicked, true);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Kuaishou graph-text one-click flow selects the native image tab and submits once', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const context = await browser.newContext();
    const current = await context.newPage();
    await current.goto('data:text/html,current-account-tab');
    await context.route('https://cp.kuaishou.com/article/publish/video', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <button>上传视频</button><button id="image-tab">上传图文</button><button>上传全景视频</button>
        <p>默认视频上传页</p>
        <script>
          window.submitClicks = 0;
          window.imageTabClicks = 0;
          document.querySelector('#image-tab').onclick = () => {
            window.imageTabClicks += 1;
            sessionStorage.setItem('imageTabClicks', String(window.imageTabClicks));
            location.href = '/article/publish/video?tabType=2';
          };
        </script>
      `,
    }));
    await context.route('https://cp.kuaishou.com/article/publish/video?tabType=2', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <p>还有上次未发布的图集，是否继续编辑？</p>
        <button id="resume">继续编辑</button><button>放弃</button>
        <script>
          document.querySelector('#resume').onclick = () => {
            sessionStorage.setItem('resumeDraftClicks', '1');
            location.href = '/article/publish/video?tabType=2&resume=1';
          };
        </script>
      `,
    }));
    await context.route('https://cp.kuaishou.com/article/publish/video?tabType=2&resume=1', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <button id="clear" onclick="window.clearClicks = (window.clearClicks || 0) + 1">清空并重新上传</button>
        <div id="body" contenteditable="true"></div>
        <input id="cover" type="file" accept="image/png">
        <input id="images" type="file" accept="image/png" multiple>
        <button id="upload" onclick="document.querySelector('#images').click()">上传图片</button>
        ${topicPickerFixture('#话题')}
        <button id="publish">发布</button>
        <div id="success" hidden>发布成功</div>
        <script>
          window.submitClicks = 0;
          document.querySelector('#images').onchange = () => {
            const preview = document.createElement('canvas');
            preview.width = 120;
            preview.height = 100;
            document.body.append(preview);
          };
          document.querySelector('#publish').onclick = () => {
            window.submitClicks += 1;
            document.querySelector('#success').hidden = false;
          };
        </script>
      `,
    }));
    const selected = await prepareTargetPage(context, PLATFORM_RULES.kuaishou, 'graph_text');
    assert.equal(selected, current);
    const result = await preparePage(selected, {
      ...validInput(),
      action: 'submit',
      allowSubmit: true,
      platform: 'kuaishou',
      content: { type: 'graph_text', title: '图文标题', caption: '图文描述', body: '', tags: ['联调'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    });

    assert.equal(
      result.status,
      'awaiting_review',
      JSON.stringify({ result, url: selected.url(), body: await selected.locator('body').innerText() }),
    );
    assert.equal(result.location, 'https://cp.kuaishou.com/article/publish/video');
    assert.equal(context.pages().length, 1);
    assert.equal(await selected.evaluate(() => sessionStorage.getItem('imageTabClicks')), '1');
    assert.equal(await selected.evaluate(() => sessionStorage.getItem('resumeDraftClicks')), '1');
    assert.equal(await selected.evaluate(() => window.clearClicks), 1);
    assert.deepEqual(await selected.locator('#images').evaluate((node) => [...node.files].map((file) => file.name)), ['content.png']);
    assert.deepEqual(result.evidence.selectedTopics, ['联调']);
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(result.evidence.submitOutcome, 'dispatched');
    assert.equal(await selected.evaluate(() => window.submitClicks), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('WeChat MP preparation prefers creator home over existing drafts', async () => {
  const staleDraft = {
    isClosed: () => false,
    url: () => 'https://mp.weixin.qq.com/cgi-bin/appmsg?t=media/appmsg_edit_v2',
  };
  const creatorHome = {
    isClosed: () => false,
    url: () => 'https://mp.weixin.qq.com/cgi-bin/home?t=home/index',
  };
  const selected = await prepareTargetPage(
    { pages: () => [staleDraft, creatorHome] },
    PLATFORM_RULES.wechat_mp,
  );
  assert.equal(selected, creatorHome);
});

test('WeChat MP waits for the creator home tab after a cold browser attach', async () => {
  let ready = false;
  const creatorHome = {
    isClosed: () => false,
    url: () => ready
      ? 'https://mp.weixin.qq.com/cgi-bin/home?t=home/index'
      : 'about:blank',
  };
  setTimeout(() => { ready = true; }, 300);
  const selected = await prepareTargetPage(
    { pages: () => [creatorHome] },
    PLATFORM_RULES.wechat_mp,
  );
  assert.equal(selected, creatorHome);
});

test('direct-composer fill stays on the selected current tab when another user tab opens', async () => {
  const browser = await launchChromium();
  const form = `
    <div class="article-bar__title-display">已有标题</div>
    <input id="txtTitle" placeholder="标题" hidden>
    <pre class="editor__inner" contenteditable="true"></pre>
    <button class="btn btn-publish">发布文章</button>
    <section class="csdn-publish-dialog" hidden>
      <button id="add-tag">添加文章标签</button>
      <input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签" hidden>
      <label>文章摘要</label>
      <div id="selected-tags"></div>
      <button class="button btn-b-red ml16">发布文章</button>
    </section>
    <script>
      window.finalClicks = 0;
      document.querySelector('.article-bar__title-display').onclick = () => { document.querySelector('#txtTitle').hidden = false; };
      document.querySelector('.btn-publish').onclick = () => { document.querySelector('section').hidden = false; };
      document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-input').hidden = false; };
      document.querySelector('#tag-input').onkeydown = (event) => {
        if (event.key !== 'Enter') return;
        const chip = document.createElement('span'); chip.textContent = event.currentTarget.value;
        document.querySelector('#selected-tags').appendChild(chip); event.currentTarget.value = '';
      };
      document.querySelector('.btn-b-red').onclick = () => { window.finalClicks += 1; };
    </script>`;
  try {
    const context = await browser.newContext();
    await context.route('https://editor.csdn.net/md/', (route) => route.fulfill({ contentType: 'text/html', body: form }));
    const original = await context.newPage();
    await original.goto('data:text/html,existing-user-tab');
    const composer = await prepareTargetPage(context, PLATFORM_RULES.csdn);
    const unrelated = await context.newPage();
    await unrelated.setContent(form);

    const input = validInput();
    input.content.tags = ['AlphaNexus'];
    const result = await preparePage(composer, input);

    assert.notEqual(result.step, 'prepared_page_missing');
    assert.equal(await composer.locator('#txtTitle').inputValue(), 'title');
    assert.equal(await unrelated.locator('#txtTitle').inputValue(), '');
    assert.equal(await composer.evaluate(() => window.finalClicks), 0);
    assert.equal(await unrelated.evaluate(() => window.finalClicks), 0);
    assert.equal(original.url(), 'https://editor.csdn.net/md/');
  } finally {
    await browser.close();
  }
});

test('CSDN does not accept cleared input plus ordinary matching text as a selected tag chip', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <button class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article publish-article-modal__body" hidden>
        <button id="add-tag">添加文章标签</button>
        <input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签" hidden>
        <div id="selected-tags"></div>
        <label>文章摘要</label>
        <button class="btn-b-red">发布文章</button>
      </section>
      <script>
        window.submitClicks = 0;
        document.querySelector('.btn-publish').onclick = () => { document.querySelector('section').hidden = false; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-input').hidden = false; };
        document.querySelector('#tag-input').onkeydown = (event) => {
          if (event.key !== 'Enter') return;
          const plainText = document.createElement('span');
          plainText.textContent = event.currentTarget.value;
          document.querySelector('#selected-tags').appendChild(plainText);
          event.currentTarget.value = '';
        };
        document.querySelector('.btn-b-red').onclick = () => { window.submitClicks += 1; };
      </script>
    `);

    const input = validInput();
    input.content.tags = ['AlphaNexus'];
    const result = await prepareForm(page, input, PLATFORM_RULES.csdn);

    assert.equal(result.status, 'manual_takeover');
    assert.ok(result.evidence.missingRequiredControls.includes('content.tags'));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
  }
});

test('strict input rejects invalid tag, media, and platform-setting combinations', () => {
  const missingBody = validInput();
  missingBody.content.body = '   ';
  assert.throws(() => validateInput(missingBody), /CONTENT_INVALID/);

  const newlineTag = validInput();
  newlineTag.content.tags = ['unsafe\nlabel'];
  assert.throws(() => validateInput(newlineTag), /TAGS_INVALID/);
  const emptyHashtag = validInput();
  emptyHashtag.content.tags = ['###'];
  assert.throws(() => validateInput(emptyHashtag), /TAGS_INVALID/);

  const wrongVideoMedia = validInput();
  wrongVideoMedia.platform = 'douyin';
  wrongVideoMedia.content.type = 'video';
  wrongVideoMedia.media = [{
    path: '/tmp/not-a-video.png', kind: 'image', role: 'content', position: 0,
    sha256: 'a'.repeat(64),
  }];
  assert.throws(() => validateInput(wrongVideoMedia), /MEDIA_INVALID/);

  const articleSettings = validInput();
  articleSettings.settings = { allow_download: true };
  assert.throws(() => validateInput(articleSettings), /SETTINGS_UNSUPPORTED/);

  const wrongPlatformMode = validInput();
  wrongPlatformMode.platform = 'wechat_video';
  assert.throws(() => validateInput(wrongPlatformMode), /CONTENT_TYPE_UNSUPPORTED/);

  const douyinGraph = validInput();
  douyinGraph.platform = 'douyin';
  douyinGraph.content = { type: 'graph_text', title: '图文', caption: '正文', body: '', tags: [] };
  douyinGraph.media = [{
    path: '/tmp/image.png', kind: 'image', role: 'content', position: 0,
    sha256: 'b'.repeat(64),
  }];
  assert.equal(validateInput(douyinGraph).content.type, 'graph_text');

  const kuaishouGraph = structuredClone(douyinGraph);
  kuaishouGraph.platform = 'kuaishou';
  assert.equal(validateInput(kuaishouGraph).content.type, 'graph_text');

  const xhsSettings = validInput();
  xhsSettings.platform = 'xhs';
  xhsSettings.content.type = 'graph_text';
  xhsSettings.media = [{
    path: '/tmp/image.png', kind: 'image', role: 'content', position: 0,
    sha256: 'b'.repeat(64),
  }];
  xhsSettings.settings = { visibility: 'private', comment_enabled: false, declare_original: true };
  assert.equal(validateInput(xhsSettings).settings.visibility, 'private');
});

test('core platforms fuzzy-match the first official location result and verify selection', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-location-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(imagePath, 'fixture-image');
    fs.writeFileSync(videoPath, 'fixture-video');
    const fixtures = {
      xhs: {
        html: xhsScopedImageComposerFixture(`
          <input placeholder="标题"><textarea placeholder="正文"></textarea>
          <input type="file" accept="image/png" multiple>
          ${locationPickerFixture()}
          <xhs-publish-btn role="button">发布</xhs-publish-btn>
        `),
        content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
        media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'a'.repeat(64) }],
      },
      douyin: {
        html: `
          <input placeholder="标题"><textarea placeholder="描述"></textarea>
          <input type="file" accept="image/png" multiple>
          ${locationPickerFixture()}
          <button id="publish">发布</button>
        `,
        content: { type: 'graph_text', title: '标题', caption: '正文', body: '', tags: [] },
        media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      },
      kuaishou: {
        html: `
          <textarea placeholder="描述"></textarea>
          <input type="file" accept="video/mp4">
          ${locationPickerFixture()}
          <div id="publish">发布</div>
        `,
        content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
        media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) }],
      },
      wechat_video: {
        html: `
          <input placeholder="标题"><textarea placeholder="描述"></textarea>
          <input type="file" accept="video/mp4">
          ${locationPickerFixture()}
          <button id="publish" class="weui-desktop-btn_primary">发表</button>
        `,
        content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
        media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'd'.repeat(64) }],
      },
    };

    for (const [platform, fixture] of Object.entries(fixtures)) {
      const page = await browser.newPage();
      await page.setContent(`${fixture.html}<script>window.submitClicks = 0; document.querySelector('#publish, xhs-publish-btn').onclick = () => { window.submitClicks += 1; };</script>`);
      const result = await prepareForm(page, {
        ...validInput(),
        platform,
        content: fixture.content,
        media: fixture.media,
        settings: { location: '杭州滨江天街' },
      }, PLATFORM_RULES[platform]);

      assert.ok(result.evidence.settingsApplied.includes('location'), `${platform}: ${JSON.stringify(result)}`);
      assert.ok(!result.evidence.unverifiedFields.includes('settings.location'), platform);
      assert.ok((await page.evaluate(() => window.locationOptionClicks)).every((id) => id === 'location-first'), platform);
      assert.ok((await page.evaluate(() => window.locationOptionClicks)).length >= 1, platform);
      assert.equal(await page.locator('#location-first').getAttribute('aria-selected'), 'true', platform);
      assert.equal(await page.locator('#location').getAttribute('aria-expanded'), 'false', platform);
      assert.match(await page.locator('#location').inputValue(), /滨江天街.*杭州/, platform);
      assert.equal(await page.evaluate(() => window.submitClicks), 0, platform);
      await page.close();
    }
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('location text and an uncommitted dropdown click do not count as selected', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-location-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题"><textarea placeholder="正文"></textarea>
      <input type="file" accept="image/png" multiple>
      ${locationPickerFixture(false)}
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>window.submitClicks = 0; document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };</script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'e'.repeat(64) }],
      settings: { location: '杭州滨江天街' },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover');
    assert.ok(result.evidence.unverifiedFields.includes('settings.location'));
    assert.equal(await page.locator('#location-first').getAttribute('aria-selected'), 'false');
    assert.equal(await page.locator('#location').getAttribute('aria-expanded'), 'true');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS verifies an empty live address card without opening its picker', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-empty-location-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题"><textarea placeholder="正文"></textarea>
      <input type="file" accept="image/png" multiple>
      <style>.d-select-input-filter.hide { display: none; }</style>
      <div class="address-card-select custom-select-44">
        <div class="d-select-main">
          <div class="d-select-content">
            <div class="d-select-input-filter hide"><input class="d-text" type="text"></div>
            <div class="d-select-placeholder">添加地点</div>
          </div>
          <div class="d-select-suffix">⌄</div>
        </div>
      </div>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.locationTriggerClicks = 0;
        document.querySelector('.address-card-select').addEventListener('click', () => {
          window.locationTriggerClicks += 1;
          document.querySelector('.d-select-input-filter').classList.remove('hide');
        });
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'f'.repeat(64) }],
      settings: { location: '' },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.ok(result.evidence.settingsVerified.includes('location'));
    assert.ok(!result.evidence.unverifiedFields.includes('settings.location'));
    assert.equal(await page.evaluate(() => window.locationTriggerClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS does not open the location picker when empty readback is selected or ambiguous', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-unsafe-empty-location-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    for (const placeholders of [['上海'], ['添加地点', '添加地点']]) {
      const page = await browser.newPage();
      await page.setContent(xhsScopedImageComposerFixture(`
        <input placeholder="标题"><textarea placeholder="正文"></textarea>
        <input type="file" accept="image/png" multiple>
        <style>.d-select-input-filter.hide { display: none; }</style>
        <div class="address-card-select custom-select-44">
          <div class="d-select-main">
            <div class="d-select-content">
              <div class="d-select-input-filter hide"><input class="d-text" type="text"></div>
              ${placeholders.map((value) => `<div class="d-select-placeholder">${value}</div>`).join('')}
            </div>
            <div class="d-select-suffix">⌄</div>
          </div>
        </div>
        <xhs-publish-btn role="button">发布</xhs-publish-btn>
        <script>
          window.locationTriggerClicks = 0;
          document.querySelector('.address-card-select').addEventListener('click', () => {
            window.locationTriggerClicks += 1;
            document.querySelector('.d-select-input-filter').classList.remove('hide');
          });
        </script>
      `));
      const result = await prepareForm(page, {
        ...validInput(),
        platform: 'xhs',
        content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
        media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'a'.repeat(64) }],
        settings: { location: '' },
      }, PLATFORM_RULES.xhs);

      assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
      assert.ok(result.evidence.unverifiedFields.includes('settings.location'));
      assert.equal(await page.evaluate(() => window.locationTriggerClicks), 0);
      await page.close();
    }
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('location picker waits for query-matching official results before choosing the first POI', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <textarea placeholder="正文"></textarea>
      <input id="location" role="combobox" aria-expanded="true" placeholder="输入位置">
      <div id="stale" role="option" aria-selected="false">杭州滨兴房地产开发有限公司</div>
      <div id="fresh" role="option" aria-selected="false" hidden>
        龙湖天街购物中心 浙江省杭州市滨江区江汉路1515号
      </div>
      <xhs-publish-btn id="publish" role="button">发布</xhs-publish-btn>
      <script>
        window.staleClicks = 0;
        window.freshClicks = 0;
        const locationInput = document.querySelector('#location');
        locationInput.addEventListener('input', () => {
          setTimeout(() => {
            document.querySelector('#stale').hidden = true;
            document.querySelector('#fresh').hidden = false;
          }, 350);
        });
        document.querySelector('#stale').onclick = (event) => {
          window.staleClicks += 1;
          event.currentTarget.setAttribute('aria-selected', 'true');
        };
        document.querySelector('#fresh').onclick = (event) => {
          window.freshClicks += 1;
          event.currentTarget.setAttribute('aria-selected', 'true');
          locationInput.value = event.currentTarget.textContent.trim();
        };
      </script>
    `));

    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      settings: { location: '杭州滨江天街' },
    }, PLATFORM_RULES.xhs);

    assert.equal(await page.evaluate(() => window.staleClicks), 0);
    assert.equal(await page.evaluate(() => window.freshClicks), 1);
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.ok(result.evidence.settingsApplied.includes('location'));
  } finally {
    await browser.close();
  }
});

test('WeChat Video rejects a stale prior location when the official result was not committed', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <textarea placeholder="添加描述"></textarea>
      <div class="post-position-wrap">
        <div class="position-display-wrap">
          <span class="location-name">瑞幸咖啡(杭州萧山万象汇)</span>
        </div>
        <input id="location" placeholder="搜索附近位置">
        <div id="fresh" class="option-item">
          <div class="location-item">
            <div class="name">杭州滨江江南之星</div>
            <div class="desc">浙江省杭州市萧山区博奥路1111号</div>
          </div>
        </div>
      </div>
      <button class="weui-desktop-btn_primary">发表</button>
      <script>
        document.querySelector('#fresh').onclick = () => {
          // Reproduce the live failure: the menu closes, while the selected
          // location remains the previous one.
          document.querySelector('#fresh').hidden = true;
        };
      </script>
    `);

    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'wechat_video',
      content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
      settings: { location: '杭州滨江天街' },
    }, PLATFORM_RULES.wechat_video);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.ok(result.evidence.unverifiedFields.includes('settings.location'), JSON.stringify(result));
  } finally {
    await browser.close();
  }
});

test('XHS fills the official address card and commits the first matching POI', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <textarea placeholder="正文"></textarea>
      <style>.d-select-input-filter.hide { display: none; }</style>
      <div class="wrapper">
        <div class="address-card-select custom-select-44">
          <div class="d-select-main">
            <div class="d-select-content">
              <div class="d-select-input-filter hide">
                <input class="d-text --color-text-title" type="text">
              </div>
              <div class="d-select-placeholder">添加地点</div>
            </div>
            <div class="d-select-suffix">⌄</div>
          </div>
        </div>
      </div>
      <div class="d-popover custom-dropdown-44" hidden>
        <div class="d-grid-item">
          <div class="option-item">
            <div class="option-name">龙湖杭州滨江天街</div>
            <div class="option-subname">杭州市滨江区江汉路1515号</div>
          </div>
        </div>
      </div>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.locationQuery = '';
        window.locationClicks = 0;
        window.locationTriggerClicks = 0;
        const card = document.querySelector('.address-card-select');
        const placeholder = card.querySelector('.d-select-placeholder');
        const filter = card.querySelector('.d-select-input-filter');
        const input = card.querySelector('input');
        const dropdown = document.querySelector('.custom-dropdown-44');
        placeholder.addEventListener('click', (event) => event.stopPropagation());
        card.addEventListener('click', () => {
          window.locationTriggerClicks += 1;
          filter.classList.remove('hide');
          input.style.width = '10px';
          input.focus();
        });
        // The live Semi Select editor ignores synthesized key events for CJK
        // text but accepts CDP text insertion once its native input is focused.
        input.addEventListener('keydown', (event) => event.preventDefault());
        input.addEventListener('input', () => {
          window.locationQuery = input.value;
          dropdown.hidden = !input.value.includes('杭州滨江天街');
        });
        dropdown.querySelector('.d-grid-item').addEventListener('click', () => {
          window.locationClicks += 1;
          card.querySelector('.d-select-content').textContent = '龙湖杭州滨江天街';
          filter.classList.add('hide');
          dropdown.hidden = true;
        });
      </script>
    `));

    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: [] },
      settings: { location: '杭州滨江天街' },
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.ok(await page.evaluate(() => window.locationTriggerClicks >= 1));
    assert.equal(await page.evaluate(() => window.locationQuery), '杭州滨江天街');
    assert.equal(await page.evaluate(() => window.locationClicks), 1);
    assert.equal(await page.locator('.address-card-select .d-select-content').innerText(), '龙湖杭州滨江天街');
    assert.ok(result.evidence.settingsApplied.includes('location'));
  } finally {
    await browser.close();
  }
});

test('Douyin waits for the official geographic picker and accepts its data-placeholder editor', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <textarea placeholder="描述"></textarea>
      <button id="location-trigger">位置</button>
      <div id="location-slot"></div>
      <button id="publish">发布</button>
      <script>
        window.submitClicks = 0;
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
        document.querySelector('#location-trigger').onclick = () => {
          setTimeout(() => {
            document.querySelector('#location-slot').innerHTML = \`
              <div id="location" contenteditable="true" data-placeholder="输入地理位置"></div>
              <div id="location-first" role="option" aria-selected="false">杭州滨江天街 杭州市滨江区</div>
            \`;
            const location = document.querySelector('#location');
            document.querySelector('#location-first').onclick = (event) => {
              event.currentTarget.setAttribute('aria-selected', 'true');
              location.textContent = event.currentTarget.textContent;
            };
          }, 2200);
        };
      </script>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
      settings: { extension_type: 'location', location: '杭州滨江天街' },
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.ok(result.evidence.settingsApplied.includes('location'));
    assert.equal(await page.locator('#location-first').getAttribute('aria-selected'), 'true');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
  }
});

test('Douyin types into the official filterable location select and commits the first POI', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <textarea placeholder="描述"></textarea>
      <div id="douyin_creator_pc_anchor_jump">
        <div
          id="location"
          class="semi-select semi-select-single semi-select-filterable semi-select-no-arrow"
          tabindex="0"
          aria-expanded="false"
        >
          <span class="semi-select-selection-text semi-select-selection-placeholder">输入地理位置</span>
          <input id="location-native-editor" style="position:absolute;opacity:0" aria-hidden="true">
        </div>
      </div>
      <div id="location-options" role="listbox" hidden>
        <div id="location-first" class="semi-select-option option-v2-_HHRPT" role="option" aria-selected="false">
          杭州滨江天街 杭州市滨江区
        </div>
      </div>
      <button id="publish">发布</button>
      <script>
        window.locationQuery = '';
        window.submitClicks = 0;
        const control = document.querySelector('#location');
        const nativeEditor = document.querySelector('#location-native-editor');
        const options = document.querySelector('#location-options');
        control.addEventListener('click', () => nativeEditor.focus());
        nativeEditor.addEventListener('input', () => {
          window.locationQuery = nativeEditor.value;
          if (window.locationQuery.includes('杭州滨江天街')) {
            control.setAttribute('aria-expanded', 'true');
            options.hidden = false;
          }
        });
        document.querySelector('#location-first').onclick = (event) => {
          event.currentTarget.setAttribute('aria-selected', 'true');
          control.setAttribute('aria-expanded', 'false');
          control.querySelector('span').textContent = event.currentTarget.textContent.trim();
          options.hidden = true;
        };
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    assert.equal(await page.locator('#douyin_creator_pc_anchor_jump .semi-select-filterable').count(), 1);
    assert.equal(await page.locator('#douyin_creator_pc_anchor_jump .semi-select-filterable').isVisible(), true);

    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
      settings: { extension_type: 'location', location: '杭州滨江天街' },
    }, PLATFORM_RULES.douyin);

    assert.equal(await page.evaluate(() => window.locationQuery), '杭州滨江天街');
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.locator('#location-first').getAttribute('aria-selected'), 'true');
    assert.ok(result.evidence.settingsApplied.includes('location'));
    assert.ok(result.evidence.settingsApplied.includes('extension_type'), JSON.stringify(result));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
  }
});

test('Kuaishou selects the required city before entering its detailed address', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <textarea placeholder="描述"></textarea>
      <button id="region-trigger">请选择所在地区</button>
      <div id="region-option" hidden>杭州市</div>
      <input id="location" placeholder="请输入视频详细地址，让同城老铁看见你">
      <div id="location-first" role="option" aria-selected="false">杭州滨江天街 杭州市滨江区</div>
      <div id="publish">发布</div>
      <script>
        window.regionClicks = 0;
        document.querySelector('#region-trigger').onclick = () => {
          document.querySelector('#region-option').hidden = false;
        };
        document.querySelector('#region-option').onclick = (event) => {
          window.regionClicks += 1;
          event.currentTarget.hidden = true;
          document.querySelector('#region-trigger').textContent = event.currentTarget.textContent;
        };
        document.querySelector('#location-first').onclick = (event) => {
          event.currentTarget.setAttribute('aria-selected', 'true');
          document.querySelector('#location').value = event.currentTarget.textContent;
        };
      </script>
    `);

    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'kuaishou',
      content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
      settings: { location: '杭州滨江天街' },
    }, PLATFORM_RULES.kuaishou);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.evaluate(() => window.regionClicks), 1);
    assert.ok(result.evidence.settingsApplied.includes('location'));
  } finally {
    await browser.close();
  }
});

test('Kuaishou fills the official Ant location controls and commits the first POI', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <textarea placeholder="描述"></textarea>
      <div class="ant-select ant-cascader">
        <span class="ant-select-selection-placeholder">请选择所在地区</span>
        <input class="ant-select-selection-search-input" type="search" readonly>
      </div>
      <div id="wrong-city">杭州市</div>
      <ul id="city-menu" class="ant-cascader-menu" hidden>
        <li id="city" class="ant-cascader-menu-item" role="menuitemcheckbox" title="杭州市">
          <div class="ant-cascader-menu-item-content">杭州市</div>
        </li>
      </ul>
      <div id="detail-select" class="ant-select ant-select-disabled">
        <span class="ant-select-selection-placeholder">请输入视频详细地址，让同城老铁看见你</span>
        <input id="detail" class="ant-select-selection-search-input" type="search" disabled>
      </div>
      <div id="poi" class="ant-select-item ant-select-item-option" hidden>
        龙湖天街购物中心 浙江省杭州市滨江区江汉路1515号
      </div>
      <div id="publish">发布</div>
      <script>
        window.cityClicks = 0;
        window.wrongCityClicks = 0;
        window.locationQuery = '';
        window.poiClicks = 0;
        const region = document.querySelector('.ant-cascader');
        const cityMenu = document.querySelector('#city-menu');
        const detailSelect = document.querySelector('#detail-select');
        const detail = document.querySelector('#detail');
        const poi = document.querySelector('#poi');
        region.onclick = () => { cityMenu.hidden = false; };
        document.querySelector('#wrong-city').onclick = () => { window.wrongCityClicks += 1; };
        document.querySelector('#city').onclick = () => {
          window.cityClicks += 1;
          cityMenu.hidden = true;
          region.querySelector('.ant-select-selection-placeholder').textContent = '杭州市';
          detailSelect.classList.remove('ant-select-disabled');
          detail.disabled = false;
        };
        detail.addEventListener('input', () => {
          window.locationQuery = detail.value;
          poi.hidden = !detail.value.includes('杭州滨江天街');
        });
        poi.onclick = () => {
          window.poiClicks += 1;
          poi.hidden = true;
          detail.value = '';
          detailSelect.querySelector('.ant-select-selection-placeholder').remove();
          const selected = document.createElement('span');
          selected.className = 'ant-select-selection-item';
          selected.textContent = '龙湖天街购物中心';
          detailSelect.prepend(selected);
        };
      </script>
    `);

    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'kuaishou',
      content: { type: 'video', title: '标题', caption: '正文', body: '', tags: [] },
      settings: { location: '杭州滨江天街' },
    }, PLATFORM_RULES.kuaishou);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.evaluate(() => window.wrongCityClicks), 0);
    assert.equal(await page.evaluate(() => window.cityClicks), 1);
    assert.equal(await page.evaluate(() => window.locationQuery), '杭州滨江天街');
    assert.equal(await page.evaluate(() => window.poiClicks), 1);
    assert.equal(await page.locator('#detail-select .ant-select-selection-item').innerText(), '龙湖天街购物中心');
    assert.ok(result.evidence.settingsApplied.includes('location'));
  } finally {
    await browser.close();
  }
});

test('reviewed editor wait uses the platform post-upload readiness budget', () => {
  assert.equal(reviewedEditorWaitMs({ timeoutMs: 90000 }, { editorReadyTimeoutMs: 60000 }), 60000);
  assert.equal(reviewedEditorWaitMs({ timeoutMs: 45000 }, { editorReadyTimeoutMs: 60000 }), 45000);
});

test('XHS keeps body separate and selects the requested topics as blue native nodes', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <input data-testid="visibility-public" type="radio" name="visibility" checked>
      <input data-testid="visibility-private" type="radio" name="visibility">
      <input data-testid="comment-enabled" type="checkbox" checked>
      <input data-testid="declare-original" type="checkbox">
      <button id="topicBtn">话题</button>
      <style>#creator-editor-topic-container .name { pointer-events: none; }</style>
      <div class="recommend-topic-wrapper">
        <span class="tag">#平台推荐一</span>
        <span class="tag">#平台推荐二</span>
        <span class="tag">#平台推荐三</span>
      </div>
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        window.topicProviderActive = false;
        window.topicRequestPending = false;
        window.lastTopicSelectedAt = 0;
        document.querySelector('xhs-publish-btn').addEventListener('click', () => { window.submitClicks += 1; });
        document.querySelector('#topicBtn').addEventListener('click', () => {
          if (Date.now() - window.lastTopicSelectedAt < 650) return;
          const body = document.querySelector('#body');
          window.topicProviderActive = true;
          body.focus();
          // The live XHS editor replaces its trailing empty paragraph with
          // the topic marker, so innerText can change without getting longer.
          // It may also append a non-breaking-space placeholder.
          const marker = document.createTextNode('#\u00a0');
          const trailing = body.lastElementChild;
          if (trailing && !trailing.textContent.trim()) trailing.replaceWith(marker);
          else body.append(marker);
          const selection = window.getSelection();
          const range = document.createRange();
          range.selectNodeContents(body);
          range.collapse(false);
          selection.removeAllRanges();
          selection.addRange(range);
        });
        document.querySelector('#body').addEventListener('input', () => {
          document.querySelector('#creator-editor-topic-container')?.remove();
          if (!window.topicProviderActive || window.topicRequestPending) return;
          const body = document.querySelector('#body');
          const selection = window.getSelection();
          const topicTextNode = selection.anchorNode;
          const raw = topicTextNode?.textContent || '';
          const match = raw.replace(/\\u200b/g, '').match(/#([^#\\s]+)$/);
          if (!match) return;
          window.topicRequestPending = true;
          setTimeout(() => {
            const options = document.createElement('div');
            options.id = 'creator-editor-topic-container';
            options.className = 'items';
            const option = document.createElement('div');
            option.className = 'item is-selected';
            option.innerHTML = '<span class="name">#' + match[1] + '</span><span class="num">1万人浏览</span>';
            option.querySelector('.name').onclick = (event) => event.stopPropagation();
            option.onclick = () => {
              topicTextNode.textContent = raw.slice(0, raw.lastIndexOf('#'));
              const selected = document.createElement('a');
              selected.className = 'tiptap-topic';
              selected.dataset.topic = JSON.stringify({ id: 'fixture', name: match[1] });
              selected.contentEditable = 'false';
              selected.style.color = 'rgb(16, 77, 161)';
              selected.innerHTML = '#' + match[1] + '<span class="content-hide">[话题]#</span>';
              topicTextNode.parentNode.insertBefore(selected, topicTextNode.nextSibling);
              selected.after(document.createTextNode(' '));
              window.topicProviderActive = false;
              window.lastTopicSelectedAt = Date.now();
              options.remove();
              const range = document.createRange();
              range.selectNodeContents(body);
              range.collapse(false);
              selection.removeAllRanges();
              selection.addRange(range);
            };
            options.append(option);
            document.body.append(options);
            window.topicRequestPending = false;
          }, 250);
        });
        document.querySelector('#body').addEventListener('keydown', (event) => {
          // Live XHS did not reliably commit the highlighted option with
          // Space; the automation must click the official parent item.
          if (event.key === ' ') event.preventDefault();
        });
        document.querySelectorAll('.recommend-topic-wrapper .tag').forEach((recommendation) => {
          recommendation.addEventListener('click', () => {
            const name = recommendation.textContent.replace(/^#/, '');
            const selected = document.createElement('a');
            selected.className = 'tiptap-topic';
            selected.dataset.topic = JSON.stringify({ id: name, name });
            selected.contentEditable = 'false';
            selected.style.color = 'rgb(16, 77, 161)';
            selected.innerHTML = '#' + name + '<span class="content-hide">[话题]#</span>';
            document.querySelector('#body').append(selected, document.createTextNode(' '));
          });
        });
      </script>
    `));
    const input = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: ['珠宝', '#新品'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { visibility: 'private', comment_enabled: false, declare_original: true },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'ready_to_submit');
    assert.equal(await page.locator("input[placeholder*='标题']").inputValue(), '标题');
    assert.equal(
      await page.locator('#body').evaluate((node) => {
        const copy = node.cloneNode(true);
        copy.querySelectorAll('a.tiptap-topic[data-topic]').forEach((topic) => topic.remove());
        return copy.innerText.trim();
      }),
      '正文',
    );
    assert.equal(await page.locator("a.tiptap-topic[data-topic]").count(), 2);
    assert.deepEqual(
      await page.locator("a.tiptap-topic[data-topic]").evaluateAll((nodes) => nodes.map((node) => JSON.parse(node.dataset.topic).name)),
      ['珠宝', '新品'],
    );
    assert.deepEqual(
      await page.locator("a.tiptap-topic[data-topic]").evaluateAll((nodes) => nodes.map((node) => getComputedStyle(node).color)),
      ['rgb(16, 77, 161)', 'rgb(16, 77, 161)'],
    );
    assert.deepEqual(await page.locator('#images').evaluate((node) => [...node.files].map((file) => file.name)), ['content.png']);
    assert.equal(await page.locator("[data-testid='visibility-private']").isChecked(), true);
    assert.equal(await page.locator("[data-testid='comment-enabled']").isChecked(), false);
    assert.equal(await page.locator("[data-testid='declare-original']").isChecked(), true);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.tagsAppliedAs, 'platform_topics');
    assert.deepEqual(result.evidence.selectedTopics, ['珠宝', '新品']);
    assert.deepEqual(result.evidence.settingsApplied.sort(), ['comment_enabled', 'declare_original', 'visibility']);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS strips only the exact frozen trailing hashtag line before inserting native topics', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-trailing-tags-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      ${xhsNativeTopicPickerFixture()}
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const subject = '正文中保留 #版本说明，不得删除。\n第二行也保留 #αgamer 示例。';
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: {
        type: 'graph_text',
        title: '标题',
        caption: '',
        body: `${subject}\n#αgamer #阿尔法玩家 #游戏内容`,
        tags: ['αgamer', '阿尔法玩家', '游戏内容'],
      },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    const bodyWithoutTopics = await page.locator('#body').evaluate((node) => {
        const copy = node.cloneNode(true);
        copy.querySelectorAll('a.tiptap-topic[data-topic]').forEach((topic) => topic.remove());
        return copy.innerText.trim();
      });
    assert.equal(bodyWithoutTopics.replace(/\s+/gu, ''), subject.replace(/\s+/gu, ''));
    assert.ok(bodyWithoutTopics.includes('#版本说明'));
    assert.ok(bodyWithoutTopics.includes('#αgamer 示例'));
    assert.deepEqual(
      await page.locator('a.tiptap-topic[data-topic]').evaluateAll(
        (nodes) => nodes.map((node) => JSON.parse(node.dataset.topic).name).sort(),
      ),
      ['αgamer', '游戏内容', '阿尔法玩家'].sort(),
    );
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS removes an unexpected native topic and then requires the exact final set', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-extra-topic-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true">正文</div>
      <input id="images" type="file" accept="image/png" multiple>
      ${xhsNativeTopicPickerFixture(['小红书科技AMA'])}
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: ['αgamer'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(
      await page.locator('a.tiptap-topic[data-topic]').evaluateAll(
        (nodes) => nodes.map((node) => JSON.parse(node.dataset.topic).name),
      ),
      ['αgamer'],
    );
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.ok(result.evidence.topicDiagnostics.some((entry) => entry.topicSet === 'normalized'));
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS removes a duplicate native topic and then requires the exact final set', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-duplicate-topic-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true">正文</div>
      <input id="images" type="file" accept="image/png" multiple>
      ${xhsNativeTopicPickerFixture(['αgamer'])}
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: ['αgamer'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.locator('a.tiptap-topic[data-topic]').count(), 1);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.ok(result.evidence.topicDiagnostics.some((entry) => entry.topicSet === 'normalized'));
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS fails closed when an unexpected native topic cannot be removed', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-unremovable-topic-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(xhsScopedImageComposerFixture(`
      <input placeholder="标题">
      <div id="body" contenteditable="true">正文</div>
      <input id="images" type="file" accept="image/png" multiple>
      ${xhsNativeTopicPickerFixture(['小红书科技AMA'])}
      <xhs-publish-btn role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        document.querySelector('#body').addEventListener('keydown', (event) => {
          if (event.key === 'Backspace') event.preventDefault();
        });
        document.querySelector('xhs-publish-btn').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'graph_text', title: '标题', caption: '', body: '正文', tags: ['αgamer'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.ok(result.evidence.missingRequiredControls.includes('content.tags'));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.ok(result.evidence.topicDiagnostics.some((entry) => entry.topicSet === 'mismatch'));
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin does not treat pasted body hashtags as selected platform topics', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <button id="publish">发布</button>
      <script>
        window.submitClicks = 0;
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '图文标题', caption: '正文', body: '', tags: ['联调'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'manual_takeover');
    assert.ok(result.evidence.missingRequiredControls.includes('content.tags'));
    assert.deepEqual(result.evidence.selectedTopics, []);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin does not accept an unhighlighted topic token as selected', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      ${topicPickerFixture('#添加话题', false)}
      <button id="publish">发布</button>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '图文标题', caption: '正文', body: '', tags: ['联调'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'manual_takeover');
    assert.ok(result.evidence.missingRequiredControls.includes('content.tags'));
    assert.deepEqual(result.evidence.selectedTopics, []);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin commits a highlighted topic with Space after a zero-width trigger marker', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      <button id="topic-trigger">#添加话题</button>
      <button id="publish">发布</button>
      <script>
        const body = document.querySelector('#body');
        document.querySelector('#topic-trigger').onclick = () => {
          body.focus();
          body.append(document.createTextNode('#\\u200b'));
          const selection = window.getSelection();
          const range = document.createRange();
          range.selectNodeContents(body);
          range.collapse(false);
          selection.removeAllRanges();
          selection.addRange(range);
        };
        body.addEventListener('keydown', (event) => {
          if (event.key !== ' ') return;
          const raw = body.innerText.replace(/\\u200b/g, '');
          const match = raw.match(/#([^#\\s]+)$/);
          if (!match) return;
          event.preventDefault();
          const selected = document.createElement('span');
          selected.dataset.mention = '#';
          selected.contentEditable = 'false';
          const highlight = document.createElement('span');
          highlight.style.background = 'rgba(1, 118, 247, 0.12)';
          highlight.textContent = '#' + match[1];
          selected.append(highlight);
          body.replaceChildren(document.createTextNode(raw.slice(0, -match[0].length)), selected, document.createTextNode(' '));
        });
      </script>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '图文标题', caption: '正文', body: '', tags: ['联调'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.selectedTopics, ['联调']);
    assert.equal(await page.locator("[data-mention='#']").count(), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin keeps body separate and commits requested topics one at a time', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="images" type="file" accept="image/png" multiple>
      ${topicPickerFixture('#添加话题')}
      <button id="publish">发布</button>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '图文标题', caption: '正文', body: '', tags: ['联调', '#新品'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.selectedTopics, ['联调', '新品']);
    assert.deepEqual(
      await page.locator("[data-topic-selected='true']").evaluateAll((nodes) => nodes.map((node) => node.textContent)),
      ['#联调', '#新品'],
    );
    assert.deepEqual(
      await page.locator("[data-topic-selected='true']").evaluateAll((nodes) => nodes.map((node) => getComputedStyle(node).color)),
      ['rgb(22, 119, 255)', 'rgb(22, 119, 255)'],
    );
    assert.equal(
      await page.locator('#body').evaluate((node) => {
        const copy = node.cloneNode(true);
        copy.querySelectorAll("[data-topic-selected='true']").forEach((topic) => topic.remove());
        return copy.innerText.trim();
      }),
      '正文',
    );
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin live graph-text form applies reviewed settings and strips the frozen trailing tag line', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-live-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: douyinLiveGraphTextComposerFixture(),
    }));
    await page.goto('https://creator.douyin.com/creator-micro/content/post/image?enter_from=publish');
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: {
        type: 'graph_text',
        title: '图文标题',
        caption: '正文保留 #正文示例\n#αgamer #阿尔法玩家 #游戏内容',
        body: '',
        tags: ['αgamer', '阿尔法玩家', '游戏内容'],
      },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: {
        collection: '',
        extension_type: 'none',
        location: '',
        hotspot: '',
        music: '',
        allow_download: false,
        visibility: 'private',
        comment_enabled: true,
        self_declaration: 'ai',
      },
      publishAt: '2030-07-24T18:30:00+08:00',
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'ready_to_submit');
    assert.equal(await page.locator('#title').inputValue(), '图文标题');
    assert.equal(
      await page.locator('#body').evaluate((node) => {
        const copy = node.cloneNode(true);
        copy.querySelectorAll("[data-topic-selected='true']").forEach((topic) => topic.remove());
        return copy.innerText.trim();
      }),
      '正文保留 #正文示例',
    );
    assert.deepEqual(result.evidence.selectedTopics, ['αgamer', '阿尔法玩家', '游戏内容']);
    assert.equal(await page.getByRole('checkbox', { name: '仅自己可见', exact: true }).isChecked(), true);
    assert.equal(await page.getByRole('checkbox', { name: '不允许', exact: true }).isChecked(), true);
    assert.equal(await page.getByRole('checkbox', { name: '定时发布', exact: true }).isChecked(), true);
    assert.equal(await page.locator('#scheduled-at').inputValue(), '2030-07-24 18:30');
    assert.equal(await page.locator('.selectText-XSrMFZ').innerText(), '内容由AI生成');
    assert.equal(await page.evaluate(() => window.declarationConfirmClicks), 1);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.deepEqual(
      result.evidence.settingsApplied.slice().sort(),
      ['allow_download', 'self_declaration', 'visibility'],
    );
    assert.deepEqual(
      result.evidence.settingsVerified.slice().sort(),
      ['collection', 'hotspot', 'music'],
    );
    assert.deepEqual(
      result.evidence.settingsNotPresent.slice().sort(),
      ['comment_enabled', 'extension_type', 'location'],
    );
    assert.deepEqual(result.evidence.unverifiedFields, []);
    assert.equal(result.evidence.publishTimeApplied, true);
    assert.equal(result.evidence.publishTimeValue, '2030-07-24 18:30');
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin graph-text submit accepts the reviewed live URL and revalidates every critical field', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-submit-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: douyinLiveGraphTextComposerFixture(),
    }));
    await page.goto('https://creator.douyin.com/creator-micro/content/post/image?enter_from=publish');
    const input = douyinLiveSubmitInput(imagePath);
    const prepared = await prepareForm(page, input, PLATFORM_RULES.douyin);
    assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));

    const result = await submitPreparedPage(page, input, prepared.evidence);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(await page.evaluate(() => window.submitClicks), 1);
    assert.deepEqual(result.evidence.criticalRevalidation, {
      upload: true,
      composerMode: true,
      title: true,
      body: true,
      topics: true,
      visibilityPublic: true,
      saveDisabled: true,
      aiDeclaration: true,
      schedule: true,
      collectionEmpty: true,
      hotspotEmpty: true,
      musicEmpty: true,
      commentNotPresent: true,
    });
    assert.equal(result.evidence.submitAttempted, true);
    assert.equal(result.evidence.submitClicked, true);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

for (const scenario of [
  {
    name: 'a delayed page-wide upload error',
    mutate: `setTimeout(() => {
      const hiddenTemplate = document.createElement('div');
      hiddenTemplate.hidden = true;
      hiddenTemplate.textContent = '上传失败';
      document.body.append(hiddenTemplate);
      const error = document.createElement('div');
      error.textContent = '上传失败';
      document.body.append(error);
    }, 100);`,
  },
  {
    name: 'an upload error arriving after the first stable window',
    mutate: `setTimeout(() => {
      const error = document.createElement('div');
      error.textContent = '上传失败';
      document.body.append(error);
    }, 650);`,
  },
  {
    name: 'the uploaded preview disappearing',
    mutate: `setTimeout(() => {
      document.querySelector('#upload-preview img')?.remove();
    }, 100);`,
  },
]) {
  test(`Douyin graph-text submit blocks before the final click when ${scenario.name}`, async () => {
    const browser = await launchChromium();
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-live-upload-'));
    try {
      const imagePath = path.join(directory, 'content.png');
      fs.writeFileSync(imagePath, 'fixture-image');
      const page = await browser.newPage();
      await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
        headers: { 'content-type': 'text/html; charset=utf-8' },
        body: douyinLiveGraphTextComposerFixture(),
      }));
      await page.goto('https://creator.douyin.com/creator-micro/content/post/image?enter_from=publish');
      const input = douyinLiveSubmitInput(imagePath);
      const guardedRule = {
        ...PLATFORM_RULES.douyin,
        uploadRootSelector: '#upload-preview',
        uploadRootMayMountAfterSelection: true,
        requireFileSelectionReadback: true,
        requirePreviewIncrementPerItem: true,
        requireUploadCompletionAtPrepare: true,
        uploadCompletionTimeoutMs: 5000,
        uploadStatusPageWide: true,
        uploadCompletionStableMs: 500,
      };
      const prepared = await prepareForm(page, input, guardedRule);
      assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));
      await page.evaluate((source) => Function(source)(), scenario.mutate);

      const result = await submitPreparedPage(page, input, prepared.evidence, guardedRule);

      assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
      assert.equal(result.step, 'critical_revalidation_failed');
      assert.equal(result.evidence.criticalRevalidation.upload, false);
      assert.equal(result.evidence.submitAttempted, false);
      assert.equal(result.evidence.submitClicked, false);
      assert.equal(await page.evaluate(() => window.submitClicks), 0);
    } finally {
      await browser.close();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
}

test('Douyin graph-text submit blocks when immediate and scheduled publishing are both checked', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-schedule-conflict-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: douyinLiveGraphTextComposerFixture(),
    }));
    await page.goto('https://creator.douyin.com/creator-micro/content/post/image?enter_from=publish');
    const input = douyinLiveSubmitInput(imagePath);
    const prepared = await prepareForm(page, input, PLATFORM_RULES.douyin);
    assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));

    await page.evaluate(() => {
      const immediate = [...document.querySelectorAll('input[name="publish-time"]')]
        .find((control) => control.labels[0].innerText.trim() === '立即发布');
      immediate.checked = true;
    });
    const result = await submitPreparedPage(page, input, prepared.evidence);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.step, 'critical_revalidation_failed');
    assert.equal(result.evidence.criticalRevalidation.schedule, false);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.submitAttempted, false);
    assert.equal(result.evidence.submitClicked, false);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin graph-text submit blocks when any critical live readback changes after prepare', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-critical-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.route('https://creator.douyin.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: douyinLiveGraphTextComposerFixture(),
    }));
    await page.goto('https://creator.douyin.com/creator-micro/content/post/image?enter_from=publish');
    const input = douyinLiveSubmitInput(imagePath);
    const prepared = await prepareForm(page, input, PLATFORM_RULES.douyin);
    assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));

    await page.evaluate(() => {
      document.querySelector('#title').value = '篡改标题';
      document.querySelector('#body').replaceChildren(document.createTextNode('篡改正文'));
      const checkedByLabel = (name, checked) => {
        const control = [...document.querySelectorAll("input[type='checkbox']")]
          .find((input) => input.labels[0]?.innerText.trim() === name);
        control.checked = checked;
      };
      checkedByLabel('公开', false);
      checkedByLabel('允许', true);
      checkedByLabel('不允许', false);
      checkedByLabel('立即发布', true);
      checkedByLabel('定时发布', false);
      document.querySelector('#scheduled-at').value = '2030-07-24 18:31';
      document.querySelector('.selectText-XSrMFZ').textContent = '请选择自主声明';
      const emptyStates = document.querySelectorAll('.semi-select-selection-text');
      emptyStates[0].textContent = '已选择合集';
      emptyStates[1].textContent = '已选择热点';
      document.querySelector("[class^='desc-']").textContent = '已选择音乐';
      const comment = document.createElement('label');
      comment.innerHTML = '<input type="checkbox" checked>允许评论';
      document.body.append(comment);
    });
    const result = await submitPreparedPage(page, input, prepared.evidence);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.step, 'critical_revalidation_failed');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    for (const key of [
      'title', 'body', 'topics', 'visibilityPublic', 'saveDisabled', 'aiDeclaration',
      'schedule', 'collectionEmpty', 'hotspotEmpty', 'musicEmpty', 'commentNotPresent',
    ]) {
      assert.equal(result.evidence.criticalRevalidation[key], false, key);
    }
    assert.equal(result.evidence.criticalRevalidation.composerMode, true);
    assert.equal(result.evidence.submitAttempted, false);
    assert.equal(result.evidence.submitClicked, false);
    assert.equal(result.evidence.submitTargetId, undefined);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin blocks final review when save or AI declaration readback does not commit', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-readback-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(douyinLiveGraphTextComposerFixture());
    await page.evaluate(() => {
      const deny = [...document.querySelectorAll('input[name="download"]')]
        .find((control) => control.labels[0].innerText.trim() === '不允许');
      deny.addEventListener('change', (event) => {
        event.currentTarget.checked = false;
      }, { capture: true });
      document.querySelector('#declaration-confirm').onclick = () => {
        window.declarationConfirmClicks += 1;
        document.querySelector('#declaration-dialog').hidden = true;
      };
    });

    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '图文标题', caption: '正文', body: '', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { allow_download: false, self_declaration: 'ai' },
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'form_unverified');
    assert.ok(!result.evidence.settingsApplied.includes('allow_download'));
    assert.ok(!result.evidence.settingsApplied.includes('self_declaration'));
    assert.deepEqual(
      result.evidence.unverifiedFields.slice().sort(),
      ['settings.allow_download', 'settings.self_declaration'],
    );
    assert.equal(await page.getByRole('checkbox', { name: '不允许', exact: true }).isChecked(), false);
    assert.equal(await page.locator('.selectText-XSrMFZ').innerText(), '请选择自主声明');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin does not call comment absence verified outside the reviewed composer landmarks', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-comment-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div contenteditable="true"></div>
      <input type="file" accept="image/png">
      <button>发布</button>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '标题', caption: '正文', body: '', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { comment_enabled: true },
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'manual_takeover');
    assert.ok(!result.evidence.settingsNotPresent.includes('comment_enabled'));
    assert.ok(result.evidence.unverifiedFields.includes('settings.comment_enabled'));
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin does not call comment absence verified on complete landmarks outside the reviewed live URL', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-douyin-comment-url-'));
  try {
    const imagePath = path.join(directory, 'content.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(douyinLiveGraphTextComposerFixture());
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'graph_text', title: '标题', caption: '正文', body: '', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'b'.repeat(64) }],
      settings: { comment_enabled: true },
    }, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'form_unverified');
    assert.ok(!result.evidence.settingsNotPresent.includes('comment_enabled'));
    assert.ok(result.evidence.unverifiedFields.includes('settings.comment_enabled'));
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('prepare maps current Douyin options and media without clicking submit', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div id="body" contenteditable="true"></div>
      <input id="video" type="file" accept="video/mp4">
      <input id="cover" type="file" accept="image/png">
      ${topicPickerFixture('#添加话题')}
      <button>添加位置</button>
      <input placeholder="请输入位置">
      <div id="douyin-location-option" role="option" aria-selected="false">上海</div>
      <div><span class="semi-select-selection-text">不选择合集</span></div>
      <div><span class="semi-select-selection-text">点击输入热点词</span></div>
      <div><span class="desc-live">点击添加合适作品风格音乐</span></div>
      <label><input type="checkbox" name="download" checked>允许</label>
      <label><input type="checkbox" name="download">不允许</label>
      <input type="radio" name="scope" aria-label="所有人可见" checked>
      <input type="radio" name="scope" aria-label="仅自己">
      <button id="publish">发布</button>
      <script>
        window.submitClicks = 0;
        document.querySelector('#douyin-location-option').onclick = (event) => { event.currentTarget.setAttribute('aria-selected', 'true'); };
        for (const control of document.querySelectorAll('input[name="download"]')) {
          control.onchange = () => {
            if (!control.checked) return;
            for (const other of document.querySelectorAll('input[name="download"]')) {
              other.checked = other === control;
            }
          };
        }
        document.querySelector('#publish').addEventListener('click', () => { window.submitClicks += 1; });
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: ['新品'] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'd'.repeat(64) },
      ],
      settings: {
        activity: '', collection: '', extension_type: 'location', location: '上海',
        hotspot: '', allow_download: false, visibility: 'private', music: '',
      },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'awaiting_review');
    assert.equal(result.step, 'ready_to_submit');
    assert.deepEqual(await page.locator('#video').evaluate((node) => [...node.files].map((file) => file.name)), ['content.mp4']);
    assert.deepEqual(await page.locator('#cover').evaluate((node) => [...node.files].map((file) => file.name)), ['cover.png']);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.deepEqual(result.evidence.settingsApplied.sort(), ['allow_download', 'extension_type', 'location', 'visibility']);
    assert.deepEqual(result.evidence.settingsVerified.sort(), ['collection', 'hotspot', 'music']);
    assert.deepEqual(result.evidence.settingsSkipped, []);
    assert.deepEqual(result.evidence.settingsNotPresent, ['activity']);
    assert.deepEqual(result.evidence.unverifiedFields, []);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Douyin explicit cover fails closed when the reviewed upload control is missing', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <textarea placeholder="描述"></textarea>
      <input id="video" type="file" accept="video/mp4">
      <button id="cover-trigger">选择封面</button>
      <div id="cover-dialog" hidden>
        <button>AI生成封面</button>
        <button>设置竖封面</button>
        <div id="cover-done" tabindex="0">完成</div>
      </div>
      <button id="publish">发布</button>
      <script>
        window.coverSelected = false;
        window.submitClicks = 0;
        document.querySelector('#cover-trigger').onclick = () => { document.querySelector('#cover-dialog').hidden = false; };
        document.querySelector('#cover-done').onclick = () => {
          window.coverSelected = true;
          document.querySelector('#cover-dialog').hidden = true;
        };
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'd'.repeat(64) },
      ],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.errorCode, 'FORM_REVIEW_REQUIRED');
    assert.equal(await page.evaluate(() => window.coverSelected), false);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.coverUploaded, false);
    assert.ok(result.evidence.missingRequiredControls.includes('media.cover'));
    assert.notEqual(result.evidence.coverAppliedAs, 'native_first_frame');
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS fails closed when an explicit frozen cover has no reviewed upload control', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <textarea placeholder="正文"></textarea>
      <div id="native-cover-state"></div>
      <xhs-publish-btn id="publish" role="button">发布</xhs-publish-btn>
      <script>
        window.submitClicks = 0;
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
        setTimeout(() => {
          const input = document.createElement('input');
          input.id = 'video';
          input.type = 'file';
          input.accept = 'video/mp4';
          document.body.prepend(input);
        }, 250);
        setTimeout(() => {
          document.querySelector('#native-cover-state').textContent =
            '默认截取第一帧作为封面，优质的封面会吸引更多人浏览笔记';
        }, 1300);
      </script>
    `);
    const input = {
      ...validInput(),
      timeoutMs: 3000,
      platform: 'xhs',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'd'.repeat(64) },
      ],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.equal(result.errorCode, 'FORM_REVIEW_REQUIRED');
    assert.ok(result.evidence.missingRequiredControls.includes('media.cover'));
    assert.equal(result.evidence.coverUploaded, false);
    assert.notEqual(result.evidence.coverAppliedAs, 'native_first_frame');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS video accepts the reviewed native first-frame marker when no custom cover was frozen', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-first-frame-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <main class="publish-page">
        <input placeholder="标题">
        <textarea placeholder="正文"></textarea>
        <input id="video" type="file" accept="video/mp4">
        <img
          width="320"
          height="420"
          alt="unrelated composer artwork"
          src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2tWQAAAAASUVORK5CYII="
        >
        <div id="upload-state"></div>
        <xhs-publish-btn id="publish" role="button">发布</xhs-publish-btn>
      </main>
      <script>
        window.submitClicks = 0;
        document.querySelector('#video').onchange = () => {
          document.querySelector('#upload-state').textContent =
            '默认截取第一帧作为封面，优质的封面会吸引更多人浏览笔记';
        };
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const result = await prepareForm(page, {
      ...validInput(),
      timeoutMs: 1500,
      platform: 'xhs',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
      ],
      settings: {},
    }, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.videoUploaded, true);
    assert.equal(result.evidence.videoUploadedPreviewObserved, true);
    assert.equal(result.evidence.videoUploadedCompletionStable, true);
    assert.deepEqual(result.evidence.missingRequiredControls, []);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS always navigates the current tab to a fresh composer before selecting frozen media', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-fresh-composer-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const page = await browser.newPage();
    let loads = 0;
    await page.route('https://creator.xiaohongshu.com/**', (route) => {
      loads += 1;
      const body = loads === 1
        ? xhsClosedShadowSubmitFixture(`
            <main class="publish-page">
              <p>默认截取第一帧作为封面，优质的封面会吸引更多人浏览笔记</p>
              <xhs-publish-btn role="button">发布</xhs-publish-btn>
            </main>
          `)
        : xhsClosedShadowSubmitFixture(`
            <main class="publish-page">
              <input placeholder="标题">
              <textarea placeholder="正文"></textarea>
              <input id="video" type="file" accept="video/mp4">
              <div id="upload-state"></div>
              <xhs-publish-btn role="button">发布</xhs-publish-btn>
            </main>
            <script>
              document.querySelector('#video').onchange = () => {
                document.querySelector('#upload-state').textContent =
                  '默认截取第一帧作为封面，优质的封面会吸引更多人浏览笔记';
              };
            </script>
          `);
      return route.fulfill({ contentType: 'text/html; charset=utf-8', body });
    });
    await page.goto('https://creator.xiaohongshu.com/publish/publish?from=menu&target=video');

    const result = await preparePage(page, {
      ...validInput(),
      timeoutMs: 1500,
      platform: 'xhs',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
      ],
      settings: {},
    });

    assert.equal(loads, 2);
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('XHS uploads the frozen custom cover through the reviewed modal and verifies its preview', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-xhs-cover-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(xhsClosedShadowSubmitFixture(`
      <main class="publish-page">
        <input placeholder="标题">
        <textarea placeholder="正文"></textarea>
        <input id="video" type="file" accept="video/mp4">
        <div id="upload-state"></div>
        <div class="cover-plugin-preview">
          <div class="cover">
            <div class="default" style="width:320px;height:420px;background-image:url('default-frame')"></div>
          </div>
          <button class="operator default">修改封面</button>
        </div>
        <xhs-publish-btn id="publish" role="button">发布</xhs-publish-btn>
      </main>
      <div class="cover-modal" hidden>
        <input id="cover" type="file" accept="image/png" hidden>
        <button>确定</button>
      </div>
      <script>
        window.submitClicks = 0;
        document.querySelector('#video').onchange = () => {
          const preview = document.createElement('video');
          preview.style.width = '320px';
          preview.style.height = '420px';
          preview.src = 'data:video/mp4;base64,AAAA';
          Object.defineProperty(preview, 'readyState', { value: 4 });
          document.querySelector('#upload-state').append(preview);
        };
        document.querySelector('.operator.default').onclick = () => {
          document.querySelector('.cover-modal').hidden = false;
        };
        document.querySelector('.cover-modal button').onclick = () => {
          if (document.querySelector('#cover').files.length === 1) {
            document.querySelector('.cover-plugin-preview .cover .default').style.backgroundImage =
              'url("custom-cover")';
          }
          document.querySelector('.cover-modal').hidden = true;
        };
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `));
    const input = {
      ...validInput(),
      platform: 'xhs',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'd'.repeat(64) },
      ],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.xhs);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.videoUploadedSelectionVerified, true);
    assert.equal(result.evidence.videoUploadedPreviewIncrement, 1);
    assert.equal(result.evidence.coverUploaded, true);
    assert.equal(result.evidence.coverUploadedSelectionVerified, true);
    assert.equal(result.evidence.coverAppliedAs, 'cover_upload_dialog');
    assert.equal(result.evidence.coverUploadedPreviewObserved, true);
    assert.notEqual(
      result.evidence.coverUploadedPreviewBackgroundAfter,
      result.evidence.coverUploadedPreviewBackgroundBefore,
    );
    assert.deepEqual(await page.locator('#cover').evaluate((node) => (
      [...node.files].map((file) => ({ name: file.name, size: file.size }))
    )), [{ name: 'cover.png', size: fs.statSync(coverPath).size }]);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Kuaishou accepts absent comment and declaration controls only on the complete reviewed settings surface', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div contenteditable="true"></div><div>发布</div>
      <label>允许别人跟我拍同框<input type="checkbox"></label>
      <label>允许下载此作品<input type="checkbox"></label>
      <label>作品展示在同城页<input type="checkbox"></label>
      <label>所有人可见<input type="radio"></label><label>好友可见<input type="radio"></label><label>仅自己可见<input type="radio"></label>
      <div>立即发布</div><div>定时发布</div>
    `);
    const result = await applySettings(page, {
      settings: { declaration: 'ai', comment_enabled: true },
    }, PLATFORM_RULES.kuaishou);
    assert.deepEqual(result.notPresent.sort(), ['comment_enabled', 'declaration']);
    assert.deepEqual(result.unverified, []);
  } finally {
    await browser.close();
  }
});

test('Kuaishou replaces a retained draft through its native re-upload trigger', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-kuaishou-reupload-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const page = await browser.newPage();
    await page.setContent(`
      <div id="body" contenteditable="true"></div>
      <button id="reupload">重新上传</button>
      <input id="video" type="file" accept="video/mp4" hidden>
      <div id="preview"></div>
      <div id="publish">发布</div>
      <script>
        window.reuploadClicks = 0;
        const oldVideo = document.createElement('video');
        oldVideo.src = 'data:video/mp4;base64,T0xE';
        Object.defineProperty(oldVideo, 'readyState', { value: 4 });
        document.querySelector('#preview').append(oldVideo);
        document.querySelector('#reupload').onclick = () => {
          window.reuploadClicks += 1;
          document.querySelector('#video').click();
        };
        document.querySelector('#video').onchange = () => {
          const video = document.createElement('video');
          video.src = 'data:video/mp4;base64,TkVX';
          Object.defineProperty(video, 'readyState', { value: 4 });
          document.querySelector('#preview').replaceChildren(video);
        };
      </script>
    `);
    const result = await prepareForm(page, {
      ...validInput(),
      platform: 'kuaishou',
      content: { type: 'video', title: '', caption: '快手描述', body: '', tags: [] },
      media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) }],
      settings: {},
    }, PLATFORM_RULES.kuaishou);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.evaluate(() => window.reuploadClicks), 1);
    assert.equal(result.evidence.videoUploaded, true);
    assert.equal(result.evidence.videoUploadedPreviewObserved, true);
    assert.equal(result.evidence.videoUploadedPreviewReplacementCount, 1);
    assert.equal(result.evidence.videoUploadedCompletionStable, true);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Kuaishou prepares reviewed fields, then clicks the official publish control once', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.route('https://cp.kuaishou.com/**', (route) => route.fulfill({
      headers: { 'content-type': 'text/html; charset=utf-8' },
      body: `
      <button id="upload-trigger">上传视频</button>
      <div id="upload-slot"></div>
      <div id="body" contenteditable="true"></div>
      <button id="smart-topic">智能话题</button>
      <div id="smart-topic-list"></div>
      <button>作品声明</button>
      <button id="ai-declaration" aria-pressed="false">内容为 AI 生成</button>
      <button>位置</button>
      <input placeholder="输入位置">
      <div id="kuaishou-location-option" role="option" aria-selected="false">杭州</div>
      <input type="checkbox" aria-label="允许别人跟我拍同框" checked>
      <input type="checkbox" aria-label="允许下载此作品" checked>
      <input type="checkbox" aria-label="作品展示在同城页" checked>
      <input type="radio" name="scope" aria-label="所有人可见" checked>
      <input type="radio" name="scope" aria-label="好友">
      <button id="cover-trigger" hidden>设置封面</button>
      <button id="retry-upload" hidden>重新上传</button>
      <section id="cover-panel" hidden>
        <button>封面设置</button>
        <button id="cover-done">完成</button>
      </section>
      <aside>作品管理</aside>
      <div id="publish">发布</div>
      <script>
        window.submitClicks = 0;
        window.wrongTopicClicks = 0;
        window.coverSelected = false;
        document.querySelector('#ai-declaration').onclick = (event) => { event.currentTarget.setAttribute('aria-pressed', 'true'); };
        document.querySelector('#kuaishou-location-option').onclick = (event) => { event.currentTarget.setAttribute('aria-selected', 'true'); };
        document.querySelector('#smart-topic').onclick = () => {
          const wrongOption = document.createElement('div');
          wrongOption.className = '_ai-topics-item_fixture';
          wrongOption.textContent = '#快手智能测试';
          wrongOption.onclick = () => { window.wrongTopicClicks += 1; };
          const exactOption = document.createElement('div');
          exactOption.className = '_ai-topics-item_fixture';
          exactOption.textContent = '#测试';
          exactOption.onclick = () => {
            const selected = document.createElement('span');
            selected.setAttribute('is-tag', 'true');
            selected.innerHTML = '<span class="at-tag-item" data-tag-name="测试" style="color: rgb(56, 80, 128)">#测试</span>';
            document.querySelector('#body').append(selected, document.createTextNode(' '));
          };
          document.querySelector('#smart-topic-list').replaceChildren(wrongOption, exactOption);
        };
        document.querySelector('#upload-trigger').onclick = () => {
          document.querySelector('#upload-slot').innerHTML = '<input id="video" type="file" accept="video/mp4">';
          const preview = document.createElement('video');
          preview.src = 'data:video/mp4;base64,AAAA';
          document.querySelector('#upload-slot').append(preview);
          document.querySelector('#cover-trigger').hidden = false;
          document.querySelector('#retry-upload').hidden = false;
        };
        document.querySelector('#cover-trigger').onclick = () => { document.querySelector('#cover-panel').hidden = false; };
        document.querySelector('#cover-done').onclick = () => {
          window.coverSelected = true;
          document.querySelector('#cover-panel').hidden = true;
        };
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `,
    }));
    await page.goto('https://cp.kuaishou.com/article/publish/video');
    const input = {
      ...validInput(),
      platform: 'kuaishou',
      content: { type: 'video', title: '快手标题', caption: '快手描述', body: '', tags: ['测试'] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'd'.repeat(64) },
      ],
      settings: {
        link_applet: '', declaration: 'ai', collection: '', location: '杭州',
        allow_duet: false, allow_download: false, same_city: false, visibility: 'public', music: '',
      },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.kuaishou);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.titleFound, false);
    assert.equal(result.evidence.titleFilled, true);
    assert.equal(await page.evaluate(() => window.coverSelected), true);
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    assert.equal(result.evidence.videoUploaded, true);
    assert.equal(result.evidence.coverAppliedAs, 'native_first_frame');
    assert.deepEqual(result.evidence.selectedTopics, ['测试']);
    assert.equal(await page.evaluate(() => window.wrongTopicClicks), 0);
    // A concrete setting already matching the frozen value is independently
    // read back as verified rather than clicked again.
    assert.deepEqual(
      [...result.evidence.settingsApplied, ...result.evidence.settingsVerified].sort(),
      ['allow_download', 'allow_duet', 'declaration', 'location', 'same_city', 'visibility'],
    );
    assert.deepEqual(result.evidence.settingsSkipped, []);
    assert.deepEqual(result.evidence.settingsNotPresent.sort(), ['collection', 'link_applet', 'music']);

    const submitted = await submitPreparedPage(page, input, result.evidence);
    assert.equal(submitted.status, 'awaiting_review', JSON.stringify(submitted));
    assert.equal(submitted.step, 'submission_unknown');
    assert.equal(submitted.errorCode, 'PUBLISH_CLICK_OUTCOME_UNKNOWN');
    assert.equal(submitted.evidence.submitAttempted, true);
    assert.equal(submitted.evidence.submitClicked, true);
    assert.equal(await page.evaluate(() => window.submitClicks), 1);
    assert.equal(submitted.evidence.platformSubmissionSignal, undefined);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('Kuaishou uses an official native smart-topic fallback when the exact tag is unavailable', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div id="body" contenteditable="true"></div>
      <button id="smart-topic">智能话题</button>
      <div id="smart-topic-list"></div>
      <div id="publish">发布</div>
      <script>
        window.recommendedTopicClicks = 0;
        document.querySelector('#smart-topic').onclick = () => {
          const recommendation = document.createElement('div');
          recommendation.className = '_ai-topics-item_fixture';
          recommendation.textContent = '#官方推荐';
          recommendation.onclick = () => {
            window.recommendedTopicClicks += 1;
            const selected = document.createElement('span');
            selected.dataset.topicSelected = 'true';
            selected.contentEditable = 'false';
            selected.style.color = 'rgb(22, 119, 255)';
            selected.textContent = '#官方推荐';
            document.querySelector('#body').append(selected, document.createTextNode(' '));
          };
          document.querySelector('#smart-topic-list').replaceChildren(recommendation);
        };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'kuaishou',
      content: { type: 'video', title: '快手标题', caption: '快手描述', body: '', tags: ['测试'] },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.kuaishou);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.deepEqual(result.evidence.selectedTopics, ['官方推荐']);
    assert.ok(!result.evidence.missingRequiredControls.includes('content.tags'));
    assert.equal(await page.evaluate(() => window.recommendedTopicClicks), 1);
  } finally {
    await browser.close();
  }
});

test('Kuaishou confirms an uploaded cover before reporting the form ready', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    const coverPath = path.join(directory, 'cover.png');
    fs.writeFileSync(videoPath, 'fixture-video');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <input id="video" type="file" accept="video/mp4">
      <canvas id="video-preview" width="120" height="100" hidden></canvas>
      <button id="retry-upload" hidden>重新上传</button>
      <textarea placeholder="描述"></textarea>
      <button id="cover-trigger">设置封面</button>
      <canvas id="final-cover-preview" width="120" height="100" hidden></canvas>
      <section id="cover-panel" hidden>
        <button>封面设置</button>
        <input id="cover" type="file" accept="image/png">
        <img id="cover-preview" hidden alt="封面预览">
        <button id="cover-done">完成</button>
      </section>
      <button id="publish">发布</button>
      <script>
        window.coverConfirmed = false;
        window.submitClicks = 0;
        document.querySelector('#video').onchange = () => {
          document.querySelector('#video-preview').hidden = false;
          document.querySelector('#retry-upload').hidden = false;
        };
        document.querySelector('#cover-trigger').onclick = () => { document.querySelector('#cover-panel').hidden = false; };
        document.querySelector('#cover').onchange = () => {
          document.querySelector('#cover-preview').src = 'data:image/png;base64,AAAA';
          document.querySelector('#cover-preview').hidden = false;
        };
        document.querySelector('#cover-done').onclick = () => {
          window.coverConfirmed = document.querySelector('#cover').files.length === 1;
          document.querySelector('#final-cover-preview').hidden = false;
          document.querySelector('#cover-panel').hidden = true;
        };
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'kuaishou',
      content: { type: 'video', title: '快手标题', caption: '快手描述', body: '', tags: [] },
      media: [
        { path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) },
        { path: coverPath, kind: 'image', role: 'cover', position: 1, sha256: 'd'.repeat(64) },
      ],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.kuaishou);

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await page.evaluate(() => window.coverConfirmed), true);
    assert.equal(result.evidence.coverUploaded, true);
    assert.equal(result.evidence.coverAppliedAs, 'cover_upload_dialog');
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('CSDN fails preparation when the selected-tag picker overlay remains open', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <button class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article publish-article-modal__body" hidden>
        <button id="add-tag">添加文章标签</button>
        <div class="mark_selection_box" hidden style="position:fixed;inset:0"><button aria-label="关闭">×</button><input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签"></div>
        <div id="selected-tags"></div>
        <label>文章摘要</label><button class="btn-b-red">发布文章</button>
      </section>
      <script>
        window.submitClicks = 0;
        document.querySelector('.btn-publish').onclick = () => { document.querySelector('section').hidden = false; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('.mark_selection_box').hidden = false; };
        document.querySelector('#tag-input').onkeydown = (event) => {
          if (event.key !== 'Enter') return;
          const chip = document.createElement('span'); chip.className = 'tag-chip selected'; chip.style.color = 'rgb(22, 119, 255)'; chip.textContent = event.currentTarget.value;
          document.querySelector('#selected-tags').appendChild(chip); event.currentTarget.value = '';
        };
        document.querySelector('.btn-b-red').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    const input = validInput();
    input.content.tags = ['AlphaNexus'];
    const result = await prepareForm(page, input, PLATFORM_RULES.csdn);
    assert.equal(result.status, 'manual_takeover', JSON.stringify(result));
    assert.ok(result.evidence.missingRequiredControls.includes('content.tags'));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
  }
});

test('Douyin video prepare and reviewed submit dispatch exactly once', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <textarea placeholder="描述"></textarea>
      <input id="video" type="file" accept=".mp4,.mov,.mkv">
      <button id="publish">发布</button>
      <script>
        window.submitClicks = 0;
        document.querySelector('#video').onchange = () => {
          const preview = document.createElement('canvas');
          preview.width = 120;
          preview.height = 100;
          document.body.append(preview);
        };
        document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'douyin',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'c'.repeat(64) }],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.douyin);

    assert.equal(result.status, 'awaiting_review');
    assert.deepEqual(await page.locator('#video').evaluate((node) => [...node.files].map((file) => file.name)), ['content.mp4']);
    const submitted = await submitPreparedPage(
      page,
      { ...input, action: 'submit', allowSubmit: true },
      result.evidence,
      { ...PLATFORM_RULES.douyin, composerUrl: '', composerURLPrefixes: {} },
    );
    assert.equal(submitted.status, 'awaiting_review', JSON.stringify(submitted));
    assert.equal(submitted.step, 'creator_backend_verification');
    assert.equal(submitted.evidence.submitOutcome, 'dispatched');
    assert.equal(await page.evaluate(() => window.submitClicks), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('prepare uploads article images and fails closed when a required media control is missing', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'article.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const input = {
      ...validInput(),
      content: { type: 'article', title: '文章标题', caption: '', body: '文章正文', tags: ['AlphaNexus'] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'e'.repeat(64) }],
      settings: {
        collection: '', article_type: 'repost', original_link: '', backup_gitcode: true,
        visibility: 'private', activity: '', topic: '',
      },
    };

    const completePage = await browser.newPage();
    await completePage.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <input id="article-images" type="file" accept="image/png" multiple>
      <button class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article publish-article-modal__body" hidden>
        <label>文章标签</label><button id="add-tag">添加文章标签</button>
        <div id="tag-picker" class="mark_selection_box" hidden style="position:fixed;inset:0;z-index:10;background:rgba(0,0,0,.2)"><button class="tag-picker-close" aria-label="关闭">×</button><input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签"></div>
        <label>文章摘要</label>
        <input type="radio" name="article-type" aria-label="原创" checked>
        <input type="radio" name="article-type" aria-label="转载">
        <input type="checkbox" aria-label="同时备份到 GitCode">
        <input type="radio" name="visibility" aria-label="全部可见" checked>
        <input type="radio" name="visibility" aria-label="仅自己可见">
        <div id="selected-tags"></div>
        <button class="btn-b-red">发布文章</button>
      </section>
      <script>
        window.submitClicks = 0;
        document.querySelector('.btn-publish').onclick = () => { document.querySelector('section').hidden = false; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-picker').hidden = false; };
        document.querySelector('.tag-picker-close').onclick = () => { document.querySelector('#tag-picker').hidden = true; };
        document.querySelector('#tag-input').onkeydown = (event) => {
          if (event.key === 'Escape') return;
          if (event.key !== 'Enter') return;
          const chip = document.createElement('span'); chip.className = 'tag-chip selected'; chip.style.color = 'rgb(22, 119, 255)'; chip.textContent = event.currentTarget.value;
          document.querySelector('#selected-tags').appendChild(chip); event.currentTarget.value = '';
        };
        document.querySelector('.btn-b-red').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    const complete = await prepareForm(completePage, input, PLATFORM_RULES.csdn);
    assert.equal(complete.status, 'awaiting_review');
    assert.deepEqual(await completePage.locator('#article-images').evaluate((node) => [...node.files].map((file) => file.name)), ['article.png']);
    assert.equal(await completePage.getByText('AlphaNexus', { exact: true }).count(), 1);
    assert.equal(await completePage.evaluate(() => window.submitClicks), 0);
    assert.deepEqual(complete.evidence.requiredOptionsApplied, ['publish.final_review_open', 'content.tags']);
    assert.deepEqual(complete.evidence.settingsApplied.sort(), ['article_type', 'backup_gitcode', 'visibility']);
    assert.deepEqual(complete.evidence.settingsSkipped, ['collection']);
    assert.deepEqual(complete.evidence.settingsNotPresent.sort(), ['activity', 'original_link', 'topic']);

    const missingPage = await browser.newPage();
    await missingPage.setContent(`
      <input id="txtTitle" placeholder="标题" readonly>
      <pre class="editor__inner" contenteditable="true" oninput="this.textContent = ''"></pre>
      <button class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article publish-article-modal__body" hidden>
        <label>文章标签</label><button id="add-tag">添加文章标签</button>
        <input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签" hidden>
        <label>文章摘要</label><button class="btn-b-red">发布文章</button>
      </section>
      <script>
        document.querySelector('.btn-publish').onclick = () => { document.querySelector('section').hidden = false; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-input').hidden = false; };
      </script>
    `);
    const missing = await prepareForm(missingPage, input, PLATFORM_RULES.csdn);
    assert.equal(missing.status, 'manual_takeover');
    assert.equal(missing.step, 'required_control_missing');
    assert.deepEqual(missing.evidence.missingRequiredControls, ['media.article_images', 'content.title', 'content.body', 'content.tags']);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('CSDN fails closed for an explicit article cover when no reviewed native cover control exists', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const coverPath = path.join(directory, 'article-cover.png');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <input id="unreviewed-image-input" type="file" accept="image/png">
      <button class="btn btn-publish">发布文章</button>
        <section class="modal__publish-article publish-article-modal__body" hidden>
        <label>文章标签</label><button id="add-tag">添加文章标签</button>
        <input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签" hidden>
        <label>文章摘要</label>
        <div id="selected-tags"></div>
        <button class="btn-b-red">发布文章</button>
      </section>
      <script>
        document.querySelector('.btn-publish').onclick = () => {
          document.querySelector('section').hidden = false;
        };
        document.querySelector('#add-tag').onclick = () => {
          document.querySelector('#tag-input').hidden = false;
        };
        document.querySelector('#tag-input').onkeydown = (event) => {
          if (event.key !== 'Enter') return;
          const chip = document.createElement('span');
          chip.className = 'tag-chip selected';
          chip.style.color = 'rgb(22, 119, 255)';
          chip.textContent = event.currentTarget.value;
          document.querySelector('#selected-tags').appendChild(chip);
          event.currentTarget.value = '';
        };
      </script>
    `);
    const input = validInput();
    input.content.tags = ['AlphaNexus'];
    input.media = [{
      path: coverPath,
      kind: 'image',
      role: 'cover',
      position: 0,
      sha256: 'c'.repeat(64),
    }];

    const result = await prepareForm(page, input, PLATFORM_RULES.csdn);

    assert.equal(result.status, 'manual_takeover');
    assert.ok(result.evidence.missingRequiredControls.includes('media.cover'));
    assert.equal(result.evidence.articleCoverSourceUploaded, false);
    assert.deepEqual(
      await page.locator('#unreviewed-image-input').evaluate((node) => [...node.files].map((file) => file.name)),
      [],
    );
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('CSDN uploads the frozen article cover only after opening publish options', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const coverPath = path.join(directory, 'article-cover.png');
    fs.writeFileSync(coverPath, 'fixture-cover');
    const page = await browser.newPage();
    await page.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <button class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article" hidden>
        <button id="outer-close" class="modal__close-button" aria-label="关闭">×</button>
        <label>文章标签</label><button id="add-tag">添加文章标签</button>
        <div id="tag-picker" class="mark_selection_box" hidden style="position:fixed;inset:0;z-index:10;background:rgba(0,0,0,.2)"><button class="tag-picker-close" aria-label="关闭">×</button><input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签"></div>
        <div id="selected-tags"></div><button id="article-summary">文章摘要</button>
        <div class="publish-article-modal__body"><button class="upload-img-box">从本地上传</button><div class="preview-box"><img class="preview" src=""></div><div class="img-selection-box" style="height:90px;width:160px"><div class="no-data">暂无内容图片</div></div><input class="cfw-file-input" type="file" accept="image/*"><input class="el-upload__input" type="file" accept="image/*"></div>
        <div id="image-editor" class="vue-image-crop-upload" hidden><div class="vicp-operate"><div>取消</div><div id="confirm-upload" class="vicp-operate-btn">确认上传</div></div></div>
        <button class="btn-b-red">发布文章</button>
      </section>
      <script>
        window.finalClicks = 0; window.advanceClicks = 0; window.outerCloseClicks = 0; window.summaryClicks = 0; window.escapeDismisses = 0;
        document.querySelector('.btn.btn-publish').onclick = () => { window.advanceClicks += 1; document.querySelector('section').hidden = false; };
        document.querySelector('#outer-close').onclick = () => { window.outerCloseClicks += 1; document.querySelector('section').hidden = true; };
        document.querySelector('#article-summary').onclick = () => { window.summaryClicks += 1; document.querySelector('section').hidden = true; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-picker').hidden = false; };
        document.querySelector('.tag-picker-close').onclick = () => { document.querySelector('#tag-picker').hidden = true; };
        document.querySelector('#tag-input').onkeydown = (event) => {
          if (event.key === 'Escape') { window.escapeDismisses += 1; document.querySelector('section').hidden = true; return; }
          if (event.key !== 'Enter') return;
          const chip = document.createElement('span'); chip.className = 'tag-chip selected'; chip.style.color = 'rgb(22, 119, 255)'; chip.textContent = event.currentTarget.value;
          document.querySelector('#selected-tags').appendChild(chip); event.currentTarget.value = '';
        };
        // The stale CFW control drops its selection. CSDN's active Element
        // upload control must be retried and prove the preview instead.
        window.coverConfirmClicks = 0;
        document.querySelector('.upload-img-box').onclick = () => { document.querySelector('.el-upload__input').click(); };
        document.querySelector('.cfw-file-input').onchange = (event) => { event.currentTarget.value = ''; };
        document.querySelector('.el-upload__input').onchange = () => { document.querySelector('#image-editor').hidden = false; };
        document.querySelector('#confirm-upload').onclick = () => { window.coverConfirmClicks += 1; document.querySelector('#image-editor').hidden = true; document.querySelector('section').hidden = true; setTimeout(() => { document.querySelector('.preview-box').classList.add('hasImg'); document.querySelector('.preview').src = 'https://img.csdnimg.cn/cover.png'; }, 300); };
        document.querySelector('.btn-b-red').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      content: { type: 'article', title: '文章标题', caption: '', body: '文章正文', tags: ['AlphaNexus'] },
      media: [{ path: coverPath, kind: 'image', role: 'cover', position: 0, sha256: 'c'.repeat(64) }],
      settings: {},
    };
    const result = await prepareForm(page, input, PLATFORM_RULES.csdn);
    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(result.evidence.articleCoverApplied, true);
    assert.equal(result.evidence.articleCoverSelectionVerified, true);
    assert.equal(result.evidence.articleCoverPreviewCommitted, true);
    assert.equal(result.evidence.articleCoverFinalPreviewCommitted, true);
    assert.equal(result.evidence.articleCoverNativeUploadObserved, false);
    assert.deepEqual(result.evidence.articleCoverNativeUploadStatuses, []);
    assert.deepEqual(await page.locator('.cfw-file-input').evaluate((node) => [...node.files].map((file) => file.name)), []);
    assert.deepEqual(await page.locator('.el-upload__input').evaluate((node) => [...node.files].map((file) => file.name)), ['article-cover.png']);
    assert.equal(await page.evaluate(() => window.coverConfirmClicks), 1);
    assert.equal(await page.locator('.preview-box').evaluate((node) => node.classList.contains('hasImg')), true);
    assert.equal(await page.evaluate(() => window.advanceClicks), 2);
    assert.equal(await page.evaluate(() => window.outerCloseClicks), 0);
    assert.equal(await page.evaluate(() => window.summaryClicks), 0);
    assert.equal(await page.evaluate(() => window.escapeDismisses), 0);
    assert.equal(await page.locator('section').isVisible(), true);
    assert.equal(await page.evaluate(() => window.finalClicks), 0);

    const submitted = await submitPreparedPage(page, {
      ...input,
      action: 'submit_prepared',
      allowSubmit: true,
    }, {}, { ...PLATFORM_RULES.csdn, composerUrl: '', requiredURLPrefixes: [] });
    assert.equal(submitted.status, 'awaiting_review', JSON.stringify(submitted));
    assert.equal(submitted.step, 'submission_unknown');
    assert.equal(submitted.errorCode, 'PUBLISH_CLICK_OUTCOME_UNKNOWN');
    assert.equal(submitted.evidence.articleCoverFinalPreviewCommitted, true);
    assert.equal(submitted.evidence.submitClicked, true);
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('prepare does not report ready while the final review button is disabled', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <button class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article publish-article-modal__body" hidden>
        <label>文章标签</label><button id="add-tag">添加文章标签</button>
        <input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签" hidden>
        <label>文章摘要</label><button class="btn-b-red" disabled>发布文章</button>
      </section>
      <script>
        document.querySelector('.btn-publish').onclick = () => { document.querySelector('section').hidden = false; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-input').hidden = false; };
        document.querySelector('#tag-input').onkeydown = (event) => {
          if (event.key !== 'Enter') return;
          const chip = document.createElement('span'); chip.className = 'tag-chip selected'; chip.style.color = 'rgb(22, 119, 255)'; chip.textContent = event.currentTarget.value;
          document.querySelector('section').appendChild(chip); event.currentTarget.value = '';
        };
      </script>
    `);

    const input = validInput();
    input.content.tags = ['AlphaNexus'];
    const result = await prepareForm(page, input, PLATFORM_RULES.csdn);

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'required_control_missing');
    assert.equal(result.evidence.submitFound, true);
    assert.equal(result.evidence.submitEnabled, false);
    assert.deepEqual(result.evidence.missingRequiredControls, ['submit.review_button']);
  } finally {
    await browser.close();
  }
});

test('CSDN never clicks a red terminal-like button that shares the initial publish classes', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <button id="terminal-like" class="btn btn-publish btn-b-red">发布草稿</button>
      <button id="advance" class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article publish-article-modal__body" hidden>
        <label>文章标签</label><button id="add-tag">添加文章标签</button>
        <input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签" hidden>
        <div id="selected-tags"></div>
        <label>文章摘要</label><button class="btn-b-red" disabled>发布文章</button>
      </section>
      <script>
        window.terminalClicks = 0; window.advanceClicks = 0;
        document.querySelector('#terminal-like').onclick = () => { window.terminalClicks += 1; };
        document.querySelector('#advance').onclick = () => { window.advanceClicks += 1; document.querySelector('section').hidden = false; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-input').hidden = false; };
        document.querySelector('#tag-input').onkeydown = (event) => {
          if (event.key !== 'Enter') return;
          const chip = document.createElement('span'); chip.className = 'tag-chip selected'; chip.style.color = 'rgb(22, 119, 255)'; chip.textContent = event.currentTarget.value;
          document.querySelector('#selected-tags').appendChild(chip); event.currentTarget.value = '';
        };
      </script>
    `);

    const input = validInput();
    input.content.tags = ['AlphaNexus'];
    const result = await prepareForm(page, input, PLATFORM_RULES.csdn);

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'required_control_missing');
    assert.equal(await page.evaluate(() => window.terminalClicks), 0);
    assert.equal(await page.evaluate(() => window.advanceClicks), 1);
  } finally {
    await browser.close();
  }
});

test('CSDN does not mistake a visible tag suggestion for a selected tag', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input id="txtTitle" placeholder="标题">
      <pre class="editor__inner" contenteditable="true"></pre>
      <button class="btn btn-publish">发布文章</button>
      <section class="modal__publish-article" hidden>
        <label>文章标签</label><button id="add-tag">添加文章标签</button>
        <input id="tag-input" placeholder="请输入文字搜索，Enter键入可添加自定义标签" hidden>
        <span id="candidate">AlphaNexus</span>
        <label>文章摘要</label><button class="btn-b-red">发布文章</button>
      </section>
      <script>
        window.submitClicks = 0;
        document.querySelector('.btn-publish').onclick = () => { document.querySelector('section').hidden = false; };
        document.querySelector('#add-tag').onclick = () => { document.querySelector('#tag-input').hidden = false; };
        document.querySelector('.btn-b-red').onclick = () => { window.submitClicks += 1; };
      </script>
    `);
    const input = validInput();
    input.content.tags = ['AlphaNexus'];
    const result = await prepareForm(page, input, PLATFORM_RULES.csdn);

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'required_control_missing');
    assert.ok(result.evidence.missingRequiredControls.includes('content.tags'));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
  } finally {
    await browser.close();
  }
});

test('WeChat MP advances to the reviewed final dialog but never clicks its final publish control', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div class="mock-iframe-body"><div class="ProseMirror" contenteditable="true"></div></div>
      <input placeholder="请输入作者">
      <input class="js_interaction_setting" type="checkbox" checked>
      <div class="setting-group__switch js_original_apply js_edit_ori"><div class="js_unset_original_title">未声明</div></div>
      <span class="js_article_tags_content">未添加</span>
      <span class="js_claim_source_selected">无需声明</span>
      <button class="editor-submit">发表</button>
      <section class="new_mass_send_dialog" hidden>
        <div class="weui-desktop-dialog">
          <div class="mass-send__td"><label>群发通知</label><label><input id="notify" type="checkbox" checked style="pointer-events:none"><span></span></label></div>
          <div class="mass-send__td"><label>定时发表</label><input id="schedule" type="checkbox"></div>
          <button id="options-submit" class="weui-desktop-btn weui-desktop-btn_primary">发表</button>
        </div>
      </section>
      <section id="publish-dialog" role="dialog" hidden>
        <button class="weui-desktop-btn weui-desktop-btn_primary">无需声明并发表</button>
      </section>
      <script>
        window.advanceClicks = 0; window.intermediateClicks = 0; window.finalClicks = 0;
        document.querySelector('.editor-submit').onclick = () => {
          window.advanceClicks += 1;
          setTimeout(() => { document.querySelector('.new_mass_send_dialog').hidden = false; }, 1000);
        };
        document.querySelector('#options-submit').onclick = () => {
          if (document.querySelector('#notify').checked
            || document.querySelector('#schedule').checked) return;
          window.intermediateClicks += 1;
          document.querySelector('.new_mass_send_dialog').hidden = true;
          document.querySelector('#publish-dialog').hidden = false;
        };
        document.querySelector('#publish-dialog .weui-desktop-btn_primary').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'wechat_mp',
      content: { type: 'article', title: '公众号标题', caption: '', body: '公众号正文', tags: [] },
      settings: {
        author: 'Alpha Nexus', original_link: '', comment_enabled: false, declare_original: false,
        allow_reprint: false, publish_type: 'publish', collection: '', content_source: 'none',
      },
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.wechat_mp);

    assert.equal(result.status, 'awaiting_review');
    assert.equal(result.step, 'ready_to_submit');
    assert.equal(await page.evaluate(() => window.advanceClicks), 1);
    assert.equal(await page.evaluate(() => window.intermediateClicks), 0);
    assert.equal(await page.evaluate(() => window.finalClicks), 0);
    assert.deepEqual(result.evidence.requiredOptionsApplied, ['publish.options_dialog_open']);
    assert.equal(result.evidence.submitFound, true);
    assert.equal(result.evidence.submitEnabled, true);
    assert.deepEqual(result.evidence.settingsApplied.sort(), ['author', 'comment_enabled', 'publish_type']);
    assert.deepEqual(result.evidence.settingsVerified.sort(), ['collection', 'content_source', 'declare_original']);
    assert.deepEqual(result.evidence.settingsSkipped, []);
    assert.deepEqual(result.evidence.settingsNotPresent.sort(), ['allow_reprint', 'original_link']);

    // The official options modal can disappear when the prepare CDP client
    // disconnects. Submit must reopen and revalidate it before either click.
    await page.locator('.new_mass_send_dialog').evaluate((node) => { node.hidden = true; });
    const submitted = await submitPreparedPage(page, {
      ...input,
      action: 'submit',
      allowSubmit: true,
    }, result.evidence, { ...PLATFORM_RULES.wechat_mp, requiredURLPrefixes: [] });
    assert.equal(submitted.status, 'awaiting_review');
    assert.equal(submitted.step, 'creator_backend_verification');
    assert.equal(submitted.evidence.submitAttempted, true);
    assert.equal(submitted.evidence.submitOutcome, 'dispatched');
    assert.equal(await page.evaluate(() => window.advanceClicks), 2);
    assert.equal(await page.evaluate(() => window.intermediateClicks), 1);
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
  } finally {
    await browser.close();
  }
});

test('WeChat MP sets and verifies the native +24 hour article schedule', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    const target = new Date(Date.now() + 24 * 60 * 60 * 1000);
    target.setSeconds(0, 0);
    const publishAt = target.toISOString();
    const hour = String(target.getHours()).padStart(2, '0');
    const minute = String(target.getMinutes()).padStart(2, '0');
    const options = (count) => Array.from({ length: count }, (_, index) => {
      const value = String(index).padStart(2, '0');
      return `<li data-value="${value}">${value}</li>`;
    }).join('');
    await page.setContent(`
      <section class="new_mass_send_dialog">
        <div class="weui-desktop-dialog">
          <div class="mass-send__td"><label>群发通知</label><label><input id="notify" type="checkbox"><span></span></label></div>
          <div class="mass-send__td-setting">
            <div class="publish_container weui-desktop-form__control-group">
              <label class="weui-desktop-form__label">定时发表</label>
              <div class="mass-send__timer-wrp"><label><input id="schedule" type="checkbox"><span></span></label></div>
            </div>
            <div class="mass-send__timer-container" hidden>
              <div class="mass-send__timer">
                <dl class="weui-desktop-form__dropdown-label">
                  <dt class="weui-desktop-form__dropdown__dt"><span class="weui-desktop-form__dropdown__value">今天</span></dt>
                  <div class="weui-desktop-dropdown-menu" hidden>
                    <ul class="weui-desktop-dropdown__list">
                      <li class="weui-desktop-dropdown__list-ele"><span class="weui-desktop-dropdown__list-ele__text">今天</span></li>
                      <li class="weui-desktop-dropdown__list-ele"><span class="weui-desktop-dropdown__list-ele__text">明天</span></li>
                    </ul>
                  </div>
                </dl>
                <dl class="weui-desktop-picker__time">
                  <dt><input id="schedule-time" type="text" value="00:00" placeholder="请选择时间"></dt>
                  <dd class="weui-desktop-picker__dd__time" hidden>
                    <ol class="weui-desktop-picker__time__hour">${options(24)}</ol>
                    <ol class="weui-desktop-picker__time__minute">${options(60)}</ol>
                  </dd>
                </dl>
              </div>
            </div>
          </div>
          <button class="weui-desktop-btn weui-desktop-btn_primary">发表</button>
        </div>
      </section>
      <script>
        const schedule = document.querySelector('#schedule');
        const container = document.querySelector('.mass-send__timer-container');
        const dropdown = document.querySelector('.weui-desktop-dropdown-menu');
        const timePanel = document.querySelector('.weui-desktop-picker__dd__time');
        const timeInput = document.querySelector('#schedule-time');
        schedule.onchange = () => { container.hidden = !schedule.checked; };
        document.querySelector('.weui-desktop-form__dropdown__dt').onclick = () => { dropdown.hidden = false; };
        for (const item of document.querySelectorAll('.weui-desktop-dropdown__list-ele')) {
          item.onclick = () => {
            document.querySelector('.weui-desktop-form__dropdown__value').textContent = item.innerText.trim();
            dropdown.hidden = true;
          };
        }
        timeInput.onclick = () => { timePanel.hidden = false; };
        let selectedHour = '00';
        let selectedMinute = '00';
        for (const item of document.querySelectorAll('.weui-desktop-picker__time__hour li')) {
          item.onclick = () => { selectedHour = item.dataset.value; timeInput.value = selectedHour + ':' + selectedMinute; };
        }
        for (const item of document.querySelectorAll('.weui-desktop-picker__time__minute li')) {
          item.onclick = () => { selectedMinute = item.dataset.value; timeInput.value = selectedHour + ':' + selectedMinute; };
        }
      </script>
    `);

    const result = await applyPublishTime(page, {
      ...validInput(),
      platform: 'wechat_mp',
      publishAt,
    }, PLATFORM_RULES.wechat_mp);

    assert.deepEqual(result.unverified, [], JSON.stringify({
      schedule: await page.locator('#schedule').isChecked(),
      date: await page.locator('.weui-desktop-form__dropdown__value').innerText(),
      time: await page.locator('#schedule-time').inputValue(),
      panelHidden: await page.locator('.weui-desktop-picker__dd__time').getAttribute('hidden'),
    }));
    assert.equal(result.value.endsWith(`${hour}:${minute}:00`), true);
    assert.equal(await page.locator('#schedule').isChecked(), true);
    assert.equal(await page.locator('.weui-desktop-form__dropdown__value').innerText(), '明天');
    assert.equal(await page.locator('#schedule-time').inputValue(), `${hour}:${minute}`);
  } finally {
    await browser.close();
  }
});

test('WeChat MP stops at platform verification and reports the publish failure reason', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <section class="new_mass_send_dialog">
        <div class="weui-desktop-dialog">
          <button id="options-submit" class="weui-desktop-btn weui-desktop-btn_primary">发表</button>
        </div>
      </section>
      <section id="publish-dialog" role="dialog" hidden>
        <button class="weui-desktop-btn weui-desktop-btn_primary">无需声明并发表</button>
      </section>
      <section id="verification" hidden>
        <h2>微信验证</h2>
        <p>扫码后，请联系管理员进行验证</p>
      </section>
      <script>
        window.finalClicks = 0;
        document.querySelector('#options-submit').onclick = () => {
          document.querySelector('.new_mass_send_dialog').hidden = true;
          document.querySelector('#publish-dialog').hidden = false;
        };
        document.querySelector('#publish-dialog button').onclick = () => {
          window.finalClicks += 1;
          document.querySelector('#publish-dialog').hidden = true;
          document.querySelector('#verification').hidden = false;
        };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'wechat_mp',
      action: 'submit',
      allowSubmit: true,
      content: { type: 'article', title: '公众号标题', caption: '', body: '公众号正文', tags: [] },
    };
    const result = await submitPreparedPage(
      page,
      input,
      {},
      { ...PLATFORM_RULES.wechat_mp, requiredURLPrefixes: [] },
    );

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'platform_verification_required');
    assert.equal(result.errorCode, 'PLATFORM_VERIFICATION_REQUIRED');
    assert.equal(result.evidence.platformVerificationSignal, '扫码后，请联系管理员进行验证');
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
  } finally {
    await browser.close();
  }
});

test('every platform stops on a visible security verification instead of continuing', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <button id="submit">发布</button>
      <section id="verification" role="dialog" hidden>
        <h2>安全验证</h2>
        <p>请完成验证后继续</p>
      </section>
      <script>
        window.finalClicks = 0;
        document.querySelector('#submit').onclick = () => {
          window.finalClicks += 1;
          document.querySelector('#verification').hidden = false;
        };
      </script>
    `);
    const input = {
      ...validInput(),
      action: 'submit',
      allowSubmit: true,
    };
    const result = await submitPreparedPage(page, input, {}, {
      ...PLATFORM_RULES.csdn,
      composerUrl: '',
      composerURLPrefixes: {},
      entryFlows: {},
      requiredURLPrefixes: [],
      submitSelectors: ['#submit'],
      submitNames: ['发布'],
      strictSubmitSelectors: true,
      successText: [],
      requirePostSubmitSignal: false,
    });

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'platform_verification_required');
    assert.equal(result.errorCode, 'PLATFORM_VERIFICATION_REQUIRED');
    assert.equal(result.evidence.platformVerificationSignal, '请完成验证后继续');
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
  } finally {
    await browser.close();
  }
});

test('ordinary security-help copy outside a dialog does not block publishing', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <p>安全验证帮助</p>
      <button id="submit">发布</button>
      <script>
        window.finalClicks = 0;
        document.querySelector('#submit').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = { ...validInput(), action: 'submit', allowSubmit: true };
    const result = await submitPreparedPage(page, input, {}, {
      ...PLATFORM_RULES.csdn,
      composerUrl: '',
      composerURLPrefixes: {},
      entryFlows: {},
      requiredURLPrefixes: [],
      submitSelectors: ['#submit'],
      submitNames: ['发布'],
      strictSubmitSelectors: true,
      successText: [],
      requirePostSubmitSignal: false,
    });

    assert.equal(result.status, 'awaiting_review');
    assert.equal(result.step, 'creator_backend_verification');
    assert.equal(await page.evaluate(() => window.finalClicks), 1);
  } finally {
    await browser.close();
  }
});

test('WeChat MP uses an explicit cover as its native article cover through its reviewed body-image-only flow', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'article.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div class="mock-iframe-body"><div class="ProseMirror" contenteditable="true"></div></div>
      <li class="tpl_item jsInsertIcon img"><span>图片</span></li>
      <input id="article-image" type="file" accept="image/png" hidden>
      <div class="js_cover_btn_area">拖拽或选择封面</div>
      <div class="js_cover_opr" hidden>
        <button hidden>从正文选择</button>
        <a id="from-content" class="js_selectCoverFromContent" style="display:contents;pointer-events:none">从正文选择<span>可选视频封面</span></a>
      </div>
      <section id="cover-choose" role="dialog" hidden>
        <div class="appmsg_content_img_item" hidden><img id="cover-option" alt="正文图片"></div>
        <button id="cover-next">下一步</button>
        <button id="cover-cancel">取消</button>
      </section>
      <section id="cover-crop" role="dialog" hidden>
        <a id="cover-confirm" role="link">完成</a>
      </section>
      <button class="mass_send">发表</button>
      <section id="publish-dialog" role="dialog" hidden>
        <button class="weui-desktop-btn weui-desktop-btn_primary">无需声明并发表</button>
      </section>
      <script>
        window.finalClicks = 0; window.coverConfirmClicks = 0;
        window.choiceClickAttempts = 0; window.pickerOpenClicks = 0;
        document.querySelector('.jsInsertIcon.img').onclick = () => {
          document.querySelector('#article-image').hidden = false;
        };
        document.querySelector('#article-image').onchange = () => {
          const image = document.createElement('img');
          image.alt = 'article cover';
          document.querySelector('.ProseMirror').appendChild(image);
        };
        document.querySelector('.js_cover_btn_area').onclick = () => {
          document.querySelector('.js_cover_opr').hidden = !document.querySelector('.js_cover_opr').hidden;
        };
        document.querySelector('#from-content').onclick = () => {
          window.choiceClickAttempts += 1;
          if (window.choiceClickAttempts <= 2) return;
          window.pickerOpenClicks += 1;
          document.querySelector('.js_cover_opr').hidden = true;
          document.querySelector('#cover-choose').hidden = false;
          if (window.pickerOpenClicks >= 2) {
            document.querySelector('.appmsg_content_img_item').hidden = false;
          }
        };
        document.querySelector('.appmsg_content_img_item').onclick = () => {
          document.querySelector('#cover-option').dataset.selected = 'true';
        };
        document.querySelector('#cover-next').onclick = () => {
          if (document.querySelector('#cover-option').dataset.selected === 'true') {
            document.querySelector('#cover-choose').hidden = true;
            setTimeout(() => {
              document.querySelector('#cover-crop').hidden = false;
            }, 1000);
          }
        };
        document.querySelector('#cover-cancel').onclick = () => {
          document.querySelector('#cover-choose').hidden = true;
        };
        document.querySelector('#cover-confirm').onclick = () => {
          if (document.querySelector('#cover-option').dataset.selected === 'true') {
            window.coverConfirmClicks += 1;
            document.querySelector('#cover-crop').hidden = true;
            if (window.coverConfirmClicks === 1) {
              document.querySelector('.js_cover_btn_area').style.pointerEvents = 'none';
              return;
            }
            setTimeout(() => {
              document.querySelector('.js_cover_btn_area').dataset.cover = 'selected';
              const cover = document.createElement('img');
              cover.className = 'js_cover_preview_new';
              cover.style.cssText = 'display:block;width:10px;height:10px;background-image:url("cover-preview")';
              document.querySelector('.js_cover_btn_area').after(cover);
            }, 300);
          }
        };
        document.querySelector('.mass_send').onclick = () => {
          document.querySelector('#publish-dialog').hidden = false;
        };
        document.querySelector('.weui-desktop-btn_primary').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'wechat_mp',
      content: { type: 'article', title: '公众号标题', caption: '', body: '公众号正文', tags: ['αgamer'] },
      media: [{ path: imagePath, kind: 'image', role: 'cover', position: 0, sha256: 'd'.repeat(64) }],
    };

    const result = await prepareForm(page, input, {
      ...PLATFORM_RULES.wechat_mp,
      intermediateSubmitControl: null,
      articleCoverControl: {
        ...PLATFORM_RULES.wechat_mp.articleCoverControl,
        commitTimeoutMs: 1500,
      },
    });

    assert.equal(
      result.status,
      'awaiting_review',
      JSON.stringify({
        result,
        choiceClickAttempts: await page.evaluate(() => window.choiceClickAttempts),
        pickerOpenClicks: await page.evaluate(() => window.pickerOpenClicks),
        coverConfirmClicks: await page.evaluate(() => window.coverConfirmClicks),
      }),
    );
    assert.equal(result.evidence.articleCoverSourceUploaded, true);
    assert.equal(result.evidence.articleCoverSource, 'body_image');
    assert.equal(result.evidence.articleCoverApplied, true);
    assert.equal(await page.evaluate(() => window.choiceClickAttempts), 5);
    assert.equal(await page.evaluate(() => window.pickerOpenClicks), 3);
    assert.equal(await page.evaluate(() => window.coverConfirmClicks), 2);
    assert.equal(await page.locator('.ProseMirror img').count(), 1);
    assert.equal(await page.locator('.js_cover_preview_new').count(), 1);
    assert.equal(await page.evaluate(() => window.finalClicks), 0);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('WeChat MP does not report a cover when the picker closes without committing it', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const imagePath = path.join(directory, 'article.png');
    fs.writeFileSync(imagePath, 'fixture-image');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <div class="mock-iframe-body"><div class="ProseMirror" contenteditable="true"></div></div>
      <li class="tpl_item jsInsertIcon img"><span>图片</span></li>
      <input id="article-image" type="file" accept="image/png" hidden>
      <div class="js_cover_btn_area">拖拽或选择封面</div>
      <div class="js_cover_opr" hidden><a id="from-content">从正文选择</a></div>
      <section id="cover-dialog" role="dialog" hidden>
        <div class="appmsg_content_img_item">正文图片</div>
        <button id="cover-next">下一步</button>
        <button id="cover-confirm">完成</button>
      </section>
      <button class="mass_send">发表</button>
      <script>
        document.querySelector('.jsInsertIcon.img').onclick = () => { document.querySelector('#article-image').hidden = false; };
        document.querySelector('#article-image').onchange = () => {
          document.querySelector('.ProseMirror').appendChild(document.createElement('img'));
        };
        document.querySelector('.js_cover_btn_area').onclick = () => { document.querySelector('.js_cover_opr').hidden = false; };
        document.querySelector('#from-content').onclick = () => { document.querySelector('#cover-dialog').hidden = false; };
        document.querySelector('#cover-next').onclick = () => { document.querySelector('#cover-next').hidden = true; };
        document.querySelector('#cover-confirm').onclick = () => { document.querySelector('#cover-dialog').hidden = true; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'wechat_mp',
      content: { type: 'article', title: '公众号标题', caption: '', body: '公众号正文', tags: [] },
      media: [{ path: imagePath, kind: 'image', role: 'content', position: 0, sha256: 'e'.repeat(64) }],
    };

    const result = await prepareForm(page, input, {
      ...PLATFORM_RULES.wechat_mp,
      articleCoverControl: {
        ...PLATFORM_RULES.wechat_mp.articleCoverControl,
        retryUncommittedOnce: false,
        commitTimeoutMs: 500,
      },
    });

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.evidence.articleCoverApplied, false);
    assert.ok(result.evidence.missingRequiredControls.includes('media.cover'));
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('WeChat MP fails fast when its editor body is missing and does not open review', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <button class="mass_send">发表</button>
      <section role="dialog" hidden>
        <button class="weui-desktop-btn weui-desktop-btn_primary">无需声明并发表</button>
      </section>
      <script>
        window.advanceClicks = 0; window.finalClicks = 0;
        document.querySelector('.mass_send').onclick = () => {
          window.advanceClicks += 1; document.querySelector('[role=dialog]').hidden = false;
        };
        document.querySelector('.weui-desktop-btn_primary').onclick = () => { window.finalClicks += 1; };
      </script>
    `);
    const input = {
      ...validInput(),
      platform: 'wechat_mp',
      timeoutMs: 120000,
      content: { type: 'article', title: '公众号标题', caption: '', body: '公众号正文', tags: [] },
    };

    const started = Date.now();
    const result = await prepareForm(page, input, PLATFORM_RULES.wechat_mp);
    const elapsedMs = Date.now() - started;

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'required_control_missing');
    assert.ok(result.evidence.missingRequiredControls.includes('content.body'));
    assert.deepEqual(result.evidence.requiredOptionsApplied, []);
    assert.equal(result.evidence.submitFound, false);
    assert.equal(await page.evaluate(() => window.advanceClicks), 0);
    assert.equal(await page.evaluate(() => window.finalClicks), 0);
    assert.ok(elapsedMs < 8000, `missing editor took ${elapsedMs}ms`);
  } finally {
    await browser.close();
  }
});

test('WeChat MP selects only a reviewed creator tab and adopts only its editor popup', async () => {
  const browser = await launchChromium();
  try {
    const context = await browser.newContext();
    const homeURL = 'https://mp.weixin.qq.com/cgi-bin/home';
    const editorURL = 'https://mp.weixin.qq.com/cgi-bin/appmsg?action=edit';
    const editorHTML = `
      <div class="title-editor__input"><div class="ProseMirror" contenteditable="true"></div></div>
      <div class="mock-iframe-body"><div class="ProseMirror" contenteditable="true"></div></div>
      <input placeholder="请输入作者">
      <input class="js_interaction_setting" type="checkbox" checked>
      <div class="setting-group__switch js_edit_ori"><input class="js_ori_setting_checkbox" type="checkbox"></div>
      <span class="js_article_tags_content">未添加</span>
      <span class="js_claim_source_selected">无需声明</span>
      <button class="editor-submit">发表</button>
      <section class="new_mass_send_dialog" hidden>
        <div class="weui-desktop-dialog">
          <div class="mass-send__td"><label>群发通知</label><input id="notify" type="checkbox" checked></div>
          <div class="mass-send__td"><label>分组通知</label><input id="group-notify" type="checkbox"></div>
          <div class="mass-send__td"><label>定时发表</label><input id="schedule" type="checkbox"></div>
          <button id="options-submit" class="weui-desktop-btn weui-desktop-btn_primary">发表</button>
        </div>
      </section>
      <script>
        window.finalClicks = 0;
        document.querySelector('.editor-submit').onclick = () => {
          document.querySelector('.new_mass_send_dialog').hidden = false;
        };
        document.querySelector('#options-submit').onclick = () => { window.finalClicks += 1; };
      </script>`;
    await context.route('https://mp.weixin.qq.com/**', async (route) => {
      const isEditor = route.request().url().startsWith('https://mp.weixin.qq.com/cgi-bin/appmsg');
      await route.fulfill({ contentType: 'text/html; charset=utf-8', body: isEditor ? editorHTML : `
        <a class="new-creation__menu-item" target="_blank" href="${editorURL}">文章</a>` });
    });
    const creator = await context.newPage();
    await creator.goto(homeURL);
    const unrelated = await context.newPage();
    await unrelated.setContent('<button>文章</button><script>window.entryClicks = 0; document.querySelector(\'button\').onclick = () => { window.entryClicks += 1; };</script>');

    const selected = await prepareTargetPage(context, PLATFORM_RULES.wechat_mp);
    assert.equal(selected, creator);
    const input = {
      ...validInput(),
      platform: 'wechat_mp',
      content: { type: 'article', title: '公众号标题', caption: '', body: '公众号正文', tags: [] },
    };
    const result = await preparePage(selected, input);
    const editor = context.pages().find((candidate) => candidate.url().startsWith('https://mp.weixin.qq.com/cgi-bin/appmsg'));

    assert.equal(result.status, 'awaiting_review');
    assert.equal(result.step, 'ready_to_submit');
    assert.ok(editor);
    assert.equal(await editor.locator('.title-editor__input .ProseMirror').textContent(), '公众号标题');
    assert.equal(await editor.locator('.mock-iframe-body .ProseMirror').textContent(), '公众号正文');
    assert.equal(await editor.evaluate(() => window.finalClicks), 0);
    assert.equal(await unrelated.evaluate(() => window.entryClicks), 0);
  } finally {
    await browser.close();
  }
});

test('WeChat MP submit-prepared selects the existing editor instead of creator home', async () => {
  const browser = await launchChromium();
  try {
    const context = await browser.newContext();
    await context.route('https://mp.weixin.qq.com/**', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: '<main>fixture</main>',
    }));
    const home = await context.newPage();
    await home.goto('https://mp.weixin.qq.com/cgi-bin/home');
    const editor = await context.newPage();
    await editor.goto('https://mp.weixin.qq.com/cgi-bin/appmsg?action=edit');
    const unrelated = await context.newPage();
    await unrelated.setContent('<main>unrelated</main>');

    assert.equal(await preparedSubmitTargetPage(context, PLATFORM_RULES.wechat_mp), editor);
  } finally {
    await browser.close();
  }
});

test('WeChat MP waits for a newly opened editor to finish navigating', async () => {
  const browser = await launchChromium();
  try {
    const context = await browser.newContext();
    const homeURL = 'https://mp.weixin.qq.com/cgi-bin/home';
    const editorURL = 'https://mp.weixin.qq.com/cgi-bin/appmsg?action=edit';
    await context.route(homeURL, (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `<a class="new-creation__menu-item" target="_blank" href="${editorURL}">文章</a>`,
    }));
    await context.route(editorURL, async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 3200));
      await route.fulfill({
        contentType: 'text/html; charset=utf-8',
        body: `
          <div class="title-editor__input"><div class="ProseMirror" contenteditable="true"></div></div>
          <div class="mock-iframe-body"><div class="ProseMirror" contenteditable="true"></div></div>
          <input placeholder="请输入作者">
          <input class="js_interaction_setting" type="checkbox" checked>
          <div class="setting-group__switch js_edit_ori"><input class="js_ori_setting_checkbox" type="checkbox"></div>
          <span class="js_article_tags_content">未添加</span>
          <span class="js_claim_source_selected">无需声明</span>
          <button class="editor-submit">发表</button>
          <section class="new_mass_send_dialog" hidden>
            <div class="weui-desktop-dialog">
              <div class="mass-send__td"><label>群发通知</label><input id="notify" type="checkbox" checked></div>
              <div class="mass-send__td"><label>分组通知</label><input id="group-notify" type="checkbox"></div>
              <div class="mass-send__td"><label>定时发表</label><input id="schedule" type="checkbox"></div>
              <button id="options-submit" class="weui-desktop-btn weui-desktop-btn_primary">发表</button>
            </div>
          </section>
          <script>
            document.querySelector('.editor-submit').onclick = () => {
              document.querySelector('.new_mass_send_dialog').hidden = false;
            };
          </script>`,
      });
    });
    const creator = await context.newPage();
    await creator.goto(homeURL);
    const selected = await prepareTargetPage(context, PLATFORM_RULES.wechat_mp);
    const result = await preparePage(selected, {
      ...validInput(),
      platform: 'wechat_mp',
      content: { type: 'article', title: '公众号标题', caption: '', body: '公众号正文', tags: [] },
    });
    assert.equal(result.status, 'awaiting_review');
    assert.equal(result.step, 'ready_to_submit');
  } finally {
    await browser.close();
  }
});

test('WeChat MP refuses to use an unrelated tab as a creator entry page', async () => {
  const browser = await launchChromium();
  try {
    const context = await browser.newContext();
    const unrelated = await context.newPage();
    await unrelated.setContent('<button>文章</button><script>window.entryClicks = 0; document.querySelector(\'button\').onclick = () => { window.entryClicks += 1; };</script>');

    assert.equal(await prepareTargetPage(context, PLATFORM_RULES.wechat_mp), null);
    assert.equal(await unrelated.evaluate(() => window.entryClicks), 0);
  } finally {
    await browser.close();
  }
});

test('WeChat Video fails closed when only a partial publish-label control exists', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const page = await browser.newPage();
    await page.setContent(`
      <input placeholder="标题">
      <textarea placeholder="描述"></textarea>
      <input type="file" accept="video/mp4">
      <button>查看发表规范</button>
      <script>window.partialClicks = 0; document.querySelector('button').onclick = () => { window.partialClicks += 1; };</script>
    `);
    const input = {
      ...validInput(),
      platform: 'wechat_video',
      content: { type: 'video', title: '视频标题', caption: '', body: '视频描述', tags: [] },
      media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'f'.repeat(64) }],
    };

    const result = await prepareForm(page, input, PLATFORM_RULES.wechat_video);

    assert.equal(result.status, 'manual_takeover');
    assert.equal(result.step, 'required_control_missing');
    assert.equal(result.evidence.videoUploaded, true);
    assert.equal(result.evidence.titleFilled, true);
    assert.equal(result.evidence.bodyFilled, true);
    assert.equal(result.evidence.submitFound, false);
    assert.deepEqual(result.evidence.missingRequiredControls, ['submit.review_button']);
    assert.equal(await page.evaluate(() => window.partialClicks), 0);
    assert.deepEqual(
      submitSelectorCandidates(PLATFORM_RULES.wechat_video),
      ['button.weui-desktop-btn_primary:has-text("发表")'],
    );
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('WeChat Video fills the reviewed post-upload iframe and stops before 发表', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-wechat-video-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const context = await browser.newContext();
    await context.route('https://channels.weixin.qq.com/platform/post/create', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <input id="video" type="file" accept="video/mp4">
        <canvas id="preview" width="120" height="100" hidden></canvas>
        <iframe src="https://channels.weixin.qq.com/micro/content/post/create"></iframe>
        <button id="publish" class="weui-desktop-btn weui-desktop-btn_primary">发表</button>
        <script>
          window.submitClicks = 0;
          document.querySelector('#video').onchange = () => { document.querySelector('#preview').hidden = false; };
          document.querySelector('#publish').onclick = () => { window.submitClicks += 1; };
        </script>`,
    }));
    await context.route('https://channels.weixin.qq.com/micro/content/post/create', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <input id="title" placeholder="填写短标题有机会获得更多流量">
        <div id="body" class="input-editor" data-placeholder="添加描述" contenteditable="true"></div>
        <div id="topic-trigger" class="finder-tag-wrap btn">#话题</div>
        <div id="topic-options" class="topic-suggest" role="listbox"></div>
        ${locationPickerFixture()}
        <script>
          const topicBody = document.querySelector('#body');
          topicBody.addEventListener('input', () => {
            const raw = topicBody.innerText;
            const match = raw.match(/#([^#\\s]+)\\s$/);
            if (!match) return;
            const selected = document.createElement('span');
            selected.className = 'hl topic';
            selected.dataset.type = 'topic';
            selected.style.color = 'rgb(87, 107, 149)';
            selected.textContent = '#' + match[1] + ' ';
            topicBody.replaceChildren(document.createTextNode('视频描述 '), selected);
          });
        </script>
      `,
    }));
    const page = await context.newPage();
    await page.goto('https://channels.weixin.qq.com/platform/post/create');
    const input = {
      ...validInput(),
      platform: 'wechat_video',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: ['测试'] },
      media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'f'.repeat(64) }],
      settings: { location: '杭州滨江天街' },
    };
    const result = await prepareForm(page, input, PLATFORM_RULES.wechat_video);
    const editor = page.frames().find((frame) => frame.url().includes('/micro/content/post/create'));

    assert.equal(result.status, 'awaiting_review', JSON.stringify(result));
    assert.equal(await editor.locator('#title').inputValue(), '视频标题');
    assert.deepEqual(result.evidence.selectedTopics, ['测试']);
    assert.ok(result.evidence.settingsApplied.includes('location'));
    assert.equal(await page.evaluate(() => window.submitClicks), 0);
    const submitted = await submitPreparedPage(page, {
      ...input,
      action: 'submit',
      allowSubmit: true,
    }, result.evidence, PLATFORM_RULES.wechat_video);
    assert.equal(submitted.status, 'awaiting_review', JSON.stringify(submitted));
    assert.equal(submitted.step, 'creator_backend_verification');
    assert.equal(submitted.evidence.submitOutcome, 'dispatched');
    assert.equal(await page.evaluate(() => window.submitClicks), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('WeChat Video completes one reviewed post-click confirmation before backend verification', async () => {
  const browser = await launchChromium();
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'alpha-nexus-worker-wechat-video-confirm-'));
  try {
    const videoPath = path.join(directory, 'content.mp4');
    fs.writeFileSync(videoPath, 'fixture-video');
    const context = await browser.newContext();
    await context.route('https://channels.weixin.qq.com/platform/post/create', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <input id="video" type="file" accept="video/mp4">
        <canvas id="preview" width="120" height="100" hidden></canvas>
        <iframe src="https://channels.weixin.qq.com/micro/content/post/create"></iframe>
        <button id="publish" class="weui-desktop-btn weui-desktop-btn_primary">发表</button>
        <section id="confirm-dialog" role="dialog" hidden>
          <div class="weui-desktop-dialog">
            <h2>确认发表</h2>
            <label><input id="no-declaration" type="radio" name="declaration">无需声明</label>
            <button id="confirm-publish" class="weui-desktop-btn weui-desktop-btn_primary">确认发表</button>
          </div>
        </section>
        <script>
          window.initialClicks = 0;
          window.confirmClicks = 0;
          document.querySelector('#video').onchange = () => { document.querySelector('#preview').hidden = false; };
          document.querySelector('#publish').onclick = () => {
            window.initialClicks += 1;
            document.querySelector('#confirm-dialog').hidden = false;
          };
          document.querySelector('#confirm-publish').onclick = () => {
            window.confirmClicks += 1;
            document.querySelector('#confirm-dialog').hidden = true;
          };
        </script>`,
    }));
    await context.route('https://channels.weixin.qq.com/micro/content/post/create', (route) => route.fulfill({
      contentType: 'text/html; charset=utf-8',
      body: `
        <input id="title" placeholder="填写短标题有机会获得更多流量">
        <div id="body" class="input-editor" data-placeholder="添加描述" contenteditable="true"></div>
        <div>不展示位置</div><div>选择合集</div><div>选择链接</div><div>不参与活动</div>
        <div class="original-setting"><span>声明原创</span><input type="checkbox"></div>
      `,
    }));
    const page = await context.newPage();
    await page.goto('https://channels.weixin.qq.com/platform/post/create');
    const input = {
      ...validInput(),
      platform: 'wechat_video',
      content: { type: 'video', title: '视频标题', caption: '视频描述', body: '', tags: [] },
      media: [{ path: videoPath, kind: 'video', role: 'video', position: 0, sha256: 'f'.repeat(64) }],
      settings: {
        location: '', collection: '', link_type: 'none', link_address: '',
        activity: '', declare_original: false,
      },
    };
    const prepared = await prepareForm(page, input, PLATFORM_RULES.wechat_video);
    assert.equal(prepared.status, 'awaiting_review', JSON.stringify(prepared));

    const submitted = await submitPreparedPage(page, {
      ...input,
      action: 'submit',
      allowSubmit: true,
    }, prepared.evidence, PLATFORM_RULES.wechat_video);

    assert.equal(submitted.status, 'awaiting_review', JSON.stringify(submitted));
    assert.equal(submitted.step, 'creator_backend_verification');
    assert.equal(submitted.evidence.postSubmitConfirmation, true);
    assert.equal(submitted.evidence.noDeclarationSelected, true);
    assert.equal(submitted.evidence.confirmationSubmitClicked, true);
    assert.equal(await page.evaluate(() => window.initialClicks), 1);
    assert.equal(await page.evaluate(() => window.confirmClicks), 1);
  } finally {
    await browser.close();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test('WeChat Video clears the native location default and verifies current schedule controls', async () => {
  const browser = await launchChromium();
  try {
    const page = await browser.newPage();
    await page.setContent(`
      <div class="position-display-wrap" role="button">
        <span class="location-name">杭州市</span>
      </div>
      <div id="location-menu" hidden>
        <button id="no-location">不展示位置</button>
      </div>
      <div>选择合集</div>
      <div>选择链接</div>
      <div>不参与活动</div>
      <div class="original-setting">
        <span>声明原创</span>
        <input type="checkbox">
      </div>
      <div class="schedule-setting">
        <label><input type="radio" name="schedule" checked><span>不定时</span></label>
        <label><input id="scheduled" type="radio" name="schedule"><span>定时</span></label>
        <div class="publish-time-row">
          <span>发表时间</span>
          <input id="scheduled-at" value="2026-07-30 00:00" hidden>
        </div>
      </div>
      <script>
        const locationTrigger = document.querySelector('.position-display-wrap');
        const locationMenu = document.querySelector('#location-menu');
        locationTrigger.onclick = () => { locationMenu.hidden = false; };
        document.querySelector('#no-location').onclick = () => {
          locationTrigger.querySelector('.location-name').textContent = '不展示位置';
          locationMenu.hidden = true;
        };
        document.querySelectorAll('input[name=schedule]').forEach((control) => {
          control.onchange = () => {
            if (!control.checked) return;
            document.querySelectorAll('input[name=schedule]').forEach((other) => {
              other.checked = other === control;
            });
            document.querySelector('#scheduled-at').hidden = control.id !== 'scheduled';
          };
        });
      </script>
    `);

    const input = {
      ...validInput(),
      platform: 'wechat_video',
      content: { type: 'video', title: '视频标题', caption: '', body: '视频描述', tags: [] },
      settings: {
        location: '', collection: '', link_type: 'none', link_address: '',
        activity: '', declare_original: false,
      },
      publishAt: '2026-07-30T20:55:00+08:00',
    };
    const settings = await applySettings(page, input, PLATFORM_RULES.wechat_video);
    const schedule = await applyPublishTime(page, input, PLATFORM_RULES.wechat_video);

    assert.deepEqual(settings.unverified, []);
    assert.deepEqual(settings.applied, ['location']);
    assert.deepEqual(settings.verified.sort(), ['activity', 'collection', 'declare_original', 'link_type']);
    assert.deepEqual(settings.notPresent, ['link_address']);
    assert.deepEqual(schedule.unverified, []);
    assert.deepEqual(schedule.applied, ['publish.time']);
    assert.equal(await page.locator('.location-name').innerText(), '不展示位置');
    assert.equal(await page.locator('#scheduled').isChecked(), true);
    assert.equal(await page.locator('#scheduled-at').inputValue(), '2026-07-30 20:55');
  } finally {
    await browser.close();
  }
});

test('WeChat Video commits time through the native calendar popover', async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  try {
    await page.setContent(`
      <div class="original-setting">
        <span>声明原创</span>
        <input id="original" type="checkbox">
        <input id="unrelated" type="checkbox" style="position:absolute;top:160px">
        <div role="radio" aria-checked="false"></div>
        <div role="radio" aria-checked="true"></div>
      </div>
      <div class="schedule-setting">
        <label><input type="radio" name="schedule"><span>不定时</span></label>
        <label><input id="scheduled" type="radio" name="schedule" checked><span>定时</span></label>
        <div class="publish-time-row">
          <span>发表时间</span>
          <input id="scheduled-at" value="2026-07-30 00:00" readonly>
        </div>
      </div>
      <dl class="weui-desktop-picker__date-time" hidden>
        <div class="weui-desktop-picker__panel__hd">
          <span class="weui-desktop-picker__panel__label">2026年</span>
          <span class="weui-desktop-picker__panel__label">07月</span>
          <button type="button" class="weui-desktop-btn__icon__right"></button>
        </div>
        <table class="weui-desktop-picker__table"><tbody><tr>
          <td><a id="day-30" class="weui-desktop-picker__selected">30</a></td>
          <td><a id="day-31">31</a></td>
        </tr></tbody></table>
        <input id="calendar-time" placeholder="请选择时间" value="00:00" readonly>
        <dl class="weui-desktop-picker__time" hidden>
          <ol class="weui-desktop-picker__time__panel weui-desktop-picker__time__hour">
            <li data-hour="00">00</li>
          </ol>
          <ol class="weui-desktop-picker__time__panel weui-desktop-picker__time__minute">
            <li data-minute="00">00</li>
            <li data-minute="08">08</li>
          </ol>
        </dl>
      </dl>
      <script>
        const combined = document.querySelector('#scheduled-at');
        const picker = document.querySelector('.weui-desktop-picker__date-time');
        const timePicker = document.querySelector('.weui-desktop-picker__time');
        const time = document.querySelector('#calendar-time');
        let selectedDate = '2026-07-30';
        let hour = '00';
        let minute = '00';
        let platformCommit = false;
        window.dayClicks = 0;
        combined.onclick = () => { picker.hidden = false; };
        time.onclick = () => { timePicker.hidden = false; };
        combined.addEventListener('input', () => {
          if (platformCommit) return;
          setTimeout(() => { combined.value = selectedDate + ' ' + time.value; }, 150);
        });
        const commit = () => {
          time.value = hour + ':' + minute;
          combined.value = selectedDate + ' ' + time.value;
          platformCommit = true;
          combined.dispatchEvent(new Event('input', { bubbles: true }));
          combined.dispatchEvent(new Event('change', { bubbles: true }));
          platformCommit = false;
        };
        document.querySelector('#day-31').onclick = () => {
          window.dayClicks += 1;
          selectedDate = '2026-07-31';
          commit();
        };
        document.querySelectorAll('[data-hour]').forEach((item) => {
          item.onclick = () => { hour = item.dataset.hour; commit(); };
        });
        document.querySelectorAll('[data-minute]').forEach((item) => {
          item.onclick = () => { minute = item.dataset.minute; commit(); };
        });
      </script>
    `);

    const input = {
      ...validInput(),
      platform: 'wechat_video',
      content: { type: 'video', title: '视频标题', caption: '', body: '视频描述', tags: [] },
      settings: { declare_original: false },
      publishAt: '2026-07-31T00:08:00+08:00',
    };
    const settings = await applySettings(page, input, PLATFORM_RULES.wechat_video);
    const schedule = await applyPublishTime(page, input, PLATFORM_RULES.wechat_video);

    assert.deepEqual(settings.unverified, []);
    assert.deepEqual(settings.verified, ['declare_original']);
    assert.deepEqual(schedule.unverified, []);
    assert.deepEqual(schedule.applied, ['publish.time']);
    assert.equal(await page.locator('#scheduled-at').inputValue(), '2026-07-31 00:08');
    assert.equal(await page.evaluate(() => window.dayClicks), 1);
  } finally {
    await browser.close();
  }
});
