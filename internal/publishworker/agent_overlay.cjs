'use strict';

const HOST_ID = 'alpha-nexus-agent-overlay';
const TAKEOVER_RESULT = Object.freeze({
  status: 'manual_takeover',
  step: 'user_takeover',
  errorCode: 'FORM_REVIEW_REQUIRED',
  evidence: {},
});
const STOP_RESULT = Object.freeze({
  status: 'permanent_failed',
  step: 'user_stop',
  errorCode: 'WORKER_CANCELLED',
  evidence: {},
});
let nextControllerID = 0;

function installOverlayDOM({ controllerID, hostID, taskLabel }) {
  const staleHost = document.getElementById(hostID);
  if (typeof staleHost?.__alphaNexusCleanup === 'function') staleHost.__alphaNexusCleanup();
  staleHost?.remove();

  const host = document.createElement('div');
  host.id = hostID;
  host.dataset.controllerId = controllerID;
  host.dataset.controlRequest = '';
  host.style.cssText = [
    'position:fixed',
    'inset:0',
    'z-index:2147483647',
    'pointer-events:none',
    'contain:layout style',
  ].join(';');

  const root = host.attachShadow({ mode: 'open' });
  root.innerHTML = `
    <style>
      :host {
        color-scheme: dark;
        font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        pointer-events: none;
      }
      * { box-sizing: border-box; }
      [data-agent-cursor] {
        position: fixed;
        left: 0;
        top: 0;
        width: 22px;
        height: 28px;
        opacity: 0;
        transform: translate3d(-40px, -40px, 0);
        transition: transform 180ms cubic-bezier(.2,.8,.2,1), opacity 120ms ease;
        filter: drop-shadow(0 0 1px rgba(255, 255, 255, 1)) drop-shadow(0 8px 12px rgba(0, 0, 0, .32));
        pointer-events: none;
      }
      [data-agent-cursor]::before {
        content: "";
        display: block;
        width: 18px;
        height: 24px;
        background: #181818;
        clip-path: polygon(0 0, 92% 63%, 58% 68%, 74% 100%, 58% 100%, 43% 72%, 17% 94%);
        -webkit-clip-path: polygon(0 0, 92% 63%, 58% 68%, 74% 100%, 58% 100%, 43% 72%, 17% 94%);
      }
      [data-agent-cursor][data-visible="true"] { opacity: 1; }
      [data-agent-action] {
        position: fixed;
        max-width: 320px;
        min-height: 44px;
        padding: 10px 16px;
        opacity: 0;
        overflow: hidden;
        border: 1px solid rgba(255, 255, 255, .08);
        border-radius: 999px;
        background: #515151;
        box-shadow: 0 18px 36px rgba(0, 0, 0, .28);
        color: #fff;
        font-size: 16px;
        font-weight: 760;
        line-height: 22px;
        text-overflow: ellipsis;
        white-space: nowrap;
        transition: left 180ms cubic-bezier(.2,.8,.2,1), top 180ms cubic-bezier(.2,.8,.2,1), opacity 120ms ease;
        pointer-events: none;
      }
      [data-agent-action][data-visible="true"] { opacity: 1; }
      [data-agent-ripple] {
        position: fixed;
        width: 18px;
        height: 18px;
        margin: -9px 0 0 -9px;
        border: 2px solid rgba(133, 177, 255, .95);
        border-radius: 50%;
        animation: alpha-nexus-agent-ripple 600ms ease-out forwards;
        pointer-events: none;
      }
      [data-agent-bar] {
        position: fixed;
        left: 50%;
        bottom: 20px;
        display: flex;
        align-items: center;
        gap: 14px;
        width: min(860px, calc(100vw - 32px));
        min-height: 86px;
        padding: 13px 15px;
        transform: translateX(-50%);
        border: 2px solid #242830;
        border-radius: 34px;
        background: rgba(5, 6, 8, .96);
        box-shadow: 0 20px 56px rgba(0, 0, 0, .5);
        backdrop-filter: blur(18px);
        -webkit-backdrop-filter: blur(18px);
        pointer-events: none;
      }
      [data-agent-pause] {
        position: relative;
        width: 18px;
        height: 30px;
        flex: 0 0 auto;
        opacity: .42;
      }
      [data-agent-pause]::before,
      [data-agent-pause]::after {
        content: "";
        position: absolute;
        top: 5px;
        width: 5px;
        height: 20px;
        border-radius: 3px;
        background: #fff;
      }
      [data-agent-pause]::before { left: 2px; }
      [data-agent-pause]::after { right: 2px; }
      [data-agent-spinner] {
        width: 34px;
        height: 34px;
        flex: 0 0 auto;
        border: 5px dotted #fff;
        border-radius: 50%;
        animation: alpha-nexus-agent-spin 1.1s linear infinite;
      }
      [data-agent-copy] { min-width: 0; flex: 1; }
      [data-agent-task] {
        overflow: hidden;
        color: #f7f9ff;
        font-size: 20px;
        font-weight: 780;
        line-height: 28px;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      [data-agent-status] {
        overflow: hidden;
        color: #dce7ff;
        font-size: 16px;
        font-weight: 680;
        line-height: 22px;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      [data-agent-takeover],
      [data-agent-stop] {
        min-width: 132px;
        min-height: 58px;
        padding: 0 20px;
        border: 0;
        border-radius: 20px;
        background: #272727;
        color: #f2f2f2;
        font: inherit;
        font-size: 17px;
        font-weight: 760;
        cursor: pointer;
        pointer-events: auto;
      }
      [data-agent-stop] {
        min-width: 126px;
        color: #ff6238;
      }
      [data-agent-takeover]:focus-visible,
      [data-agent-stop]:focus-visible {
        outline: 3px solid #fff;
        outline-offset: 3px;
      }
      @keyframes alpha-nexus-agent-ripple {
        to { opacity: 0; transform: scale(3.4); }
      }
      @keyframes alpha-nexus-agent-spin { to { transform: rotate(360deg); } }
      @media (prefers-reduced-motion: reduce) {
        [data-agent-cursor],
        [data-agent-action] { transition: none; }
        [data-agent-ripple],
        [data-agent-spinner] { animation: none; }
      }
    </style>
    <div data-agent-cursor data-visible="false"></div>
    <div data-agent-action data-visible="false"></div>
    <div data-agent-ripples></div>
    <div data-agent-bar>
      <span data-agent-pause aria-hidden="true"></span>
      <span data-agent-spinner aria-hidden="true"></span>
      <div data-agent-copy>
        <div data-agent-task></div>
        <div data-agent-status aria-live="polite">Agent 正在控制</div>
      </div>
      <button type="button" data-agent-takeover aria-label="接管 Agent 操作">接管</button>
      <button type="button" data-agent-stop aria-label="终止 Agent 任务">终止任务</button>
    </div>
  `;
  const inlineStyle = root.querySelector('style');
  if ('adoptedStyleSheets' in root && typeof CSSStyleSheet === 'function') {
    const sheet = new CSSStyleSheet();
    sheet.replaceSync(inlineStyle.textContent);
    root.adoptedStyleSheets = [sheet];
    inlineStyle.remove();
  }

  const cursor = root.querySelector('[data-agent-cursor]');
  const action = root.querySelector('[data-agent-action]');
  const ripples = root.querySelector('[data-agent-ripples]');
  const task = root.querySelector('[data-agent-task]');
  const status = root.querySelector('[data-agent-status]');
  const takeover = root.querySelector('[data-agent-takeover]');
  const stop = root.querySelector('[data-agent-stop]');
  task.textContent = String(taskLabel || '浏览器自动化任务').trim().slice(0, 60);

  const controlName = (target) => {
    const candidate = target?.getAttribute?.('aria-label')
      || target?.labels?.[0]?.textContent
      || target?.getAttribute?.('placeholder')
      || target?.getAttribute?.('title')
      || (['BUTTON', 'A'].includes(target?.tagName) ? target.textContent : '');
    return String(candidate || '').replace(/\s+/gu, ' ').trim().slice(0, 28);
  };
  const labelFor = (target) => {
    const tag = String(target?.tagName || '').toLowerCase();
    const role = String(target?.getAttribute?.('role') || '').toLowerCase();
    const name = controlName(target);
    if (tag === 'input' || tag === 'textarea' || target?.isContentEditable) return `填写${name || '内容'}`;
    if (tag === 'select' || role === 'combobox' || role === 'option') return `选择${name || '选项'}`;
    if (tag === 'button' || tag === 'a' || role === 'button' || role === 'link') return `点击${name || '按钮'}`;
    return '操作控件';
  };

  const showTarget = (eventTarget, x, y) => {
    if (!(eventTarget instanceof Element) || eventTarget === host || host.contains(eventTarget)) return;
    const controlTarget = eventTarget.closest(
      'button,a,input,textarea,select,[role="button"],[role="link"],[role="combobox"],[role="option"],[contenteditable="true"]',
    ) || eventTarget;
    const rect = controlTarget.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;
    const label = labelFor(controlTarget);
    const cursorX = Number.isFinite(x) ? x : rect.left + Math.min(rect.width / 2, 24);
    const cursorY = Number.isFinite(y) ? y : rect.top + Math.min(rect.height / 2, 24);
    cursor.style.transform = `translate3d(${Math.round(cursorX)}px, ${Math.round(cursorY)}px, 0)`;
    cursor.dataset.visible = 'true';
    action.style.left = `${Math.round(Math.min(window.innerWidth - 330, Math.max(8, cursorX + 20)))}px`;
    action.style.top = `${Math.round(Math.min(window.innerHeight - 58, Math.max(8, cursorY + 20)))}px`;
    action.textContent = label;
    action.dataset.visible = 'true';
    status.textContent = label;
  };

  const onPointerMove = (event) => showTarget(event.target, event.clientX, event.clientY);
  const onFocus = (event) => showTarget(event.target);
  const onClick = (event) => {
    if (event.target === host || host.contains(event.target)) return;
    showTarget(event.target, event.clientX, event.clientY);
    const ripple = document.createElement('span');
    ripple.dataset.agentRipple = '';
    ripple.style.left = `${Math.round(event.clientX)}px`;
    ripple.style.top = `${Math.round(event.clientY)}px`;
    ripples.replaceChildren(ripple);
    window.setTimeout(() => ripple.remove(), 650);
  };

  document.addEventListener('pointermove', onPointerMove, true);
  document.addEventListener('focusin', onFocus, true);
  document.addEventListener('click', onClick, true);
  takeover.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    host.dataset.controlRequest = 'takeover';
    status.textContent = '正在交还控制权';
  });
  stop.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    host.dataset.controlRequest = 'stop';
    status.textContent = '正在终止任务';
  });
  host.__alphaNexusCleanup = () => {
    document.removeEventListener('pointermove', onPointerMove, true);
    document.removeEventListener('focusin', onFocus, true);
    document.removeEventListener('click', onClick, true);
  };
  document.documentElement.append(host);
}

async function waitForControlRequest(page, isActive) {
  while (isActive()) {
    const request = await page.evaluate((hostID) =>
      document.getElementById(hostID)?.dataset.controlRequest || '', HOST_ID).catch(() => '');
    if (request === 'takeover') return { ...TAKEOVER_RESULT, evidence: {} };
    if (request === 'stop') return { ...STOP_RESULT, evidence: {} };
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  return new Promise(() => {});
}

async function createAgentControl(page, options = {}) {
  const controllerID = `controller-${++nextControllerID}`;
  const taskLabel = String(options.taskLabel || '浏览器自动化任务').trim().slice(0, 60);
  let active = true;
  const install = async () => {
    try {
      await page.evaluate(installOverlayDOM, { controllerID, hostID: HOST_ID, taskLabel });
      return true;
    } catch (_) {
      return false;
    }
  };
  const onNavigation = () => {
    if (active) void install();
  };
  page.on('domcontentloaded', onNavigation);
  const available = await install();
  if (!available) {
    active = false;
    page.off('domcontentloaded', onNavigation);
  }
  return {
    available,
    async race(operation) {
      if (!available) return operation();
      return Promise.race([
        Promise.resolve().then(operation),
        waitForControlRequest(page, () => active),
      ]);
    },
    async dispose() {
      if (!active) return;
      active = false;
      page.off('domcontentloaded', onNavigation);
      await page.evaluate(({ controllerID: expected, hostID }) => {
        const host = document.getElementById(hostID);
        if (!host || host.dataset.controllerId !== expected) return;
        if (typeof host.__alphaNexusCleanup === 'function') host.__alphaNexusCleanup();
        host.remove();
      }, { controllerID, hostID: HOST_ID }).catch(() => {});
    },
  };
}

module.exports = { createAgentControl };
