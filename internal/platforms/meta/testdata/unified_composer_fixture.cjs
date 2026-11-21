'use strict';

const vm = require('node:vm');

let source = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', chunk => { source += chunk; });
process.stdin.on('end', () => {
  const mode = process.argv[2] || 'default';
  const options = [
    makeNode('option', 'AlphaIntelligence', { 'aria-selected': 'true', 'data-asset-id': '1241410172381837' }),
    makeNode('option', 'chenpaiteng', { 'aria-selected': 'true', 'data-asset-id': '17841416733781085' }),
    makeNode('option', '保存偏好设置', { 'aria-selected': 'true' }),
  ];
  if (mode === 'live-rootless-no-ids') {
    options.splice(2, 1);
    delete options[0].attrs['data-asset-id'];
    delete options[1].attrs['data-asset-id'];
    options[0].images = [makeImage(''), makeImage('Facebook')];
    options[1].images = [makeImage(''), makeImage('Instagram')];
  }
  if (mode === 'unknown-selected') {
    options.splice(2, 0, makeNode('option', 'Unexpected Page', { 'aria-selected': 'true' }));
  }
  if (mode === 'unknown-unselected') {
    options.splice(2, 0, makeNode('option', 'Unexpected Page', { 'aria-selected': 'false' }));
  }
  if (mode === 'wrong-target-id') options[1].attrs['data-asset-id'] = '17841400000000000';
  if (mode === 'wrong-connected-id') options[0].attrs['data-asset-id'] = '1241410000000000';
  if (mode === 'missing-target-id') delete options[1].attrs['data-asset-id'];
  if (mode === 'missing-connected-id') delete options[0].attrs['data-asset-id'];
  const combobox = makeNode('combobox', mode === 'live-rootless-no-ids' ? 'AlphaIntelligence和chenpaiteng' : 'AlphaIntelligence chenpaiteng');
  const editorCombobox = makeNode('combobox', '', { contenteditable: 'true', 'aria-label': '在对话框中输入内容，即可为帖子添加文字。' });
  const listbox = makeNode('listbox', '');
  const root = makeNode('dialog', '', { 'data-testid': 'composer-root' });
  listbox.visible = mode !== 'delayed-listbox';

  combobox.click = () => {
    combobox.clicks += 1;
    if (mode !== 'delayed-listbox') listbox.visible = true;
  };
  for (const option of options) {
    option.click = () => {
      option.clicks += 1;
      option.attrs['aria-selected'] = option.attrs['aria-selected'] === 'true' ? 'false' : 'true';
      combobox.innerText = options
        .filter(candidate => candidate.innerText !== '保存偏好设置')
        .filter(candidate => candidate.attrs['aria-selected'] === 'true')
        .map(candidate => candidate.innerText)
        .join(' ');
    };
  }
  for (const option of options) {
    option.querySelectorAll = selector => selector === 'img[alt]' ? option.images : [];
  }
  listbox.querySelectorAll = selector => selector === '[role="option"]' ? options : [];
  root.querySelectorAll = selector => selector === '[role="combobox"]' ? [combobox] : [];

  const document = {
    querySelectorAll(selector) {
      switch (selector) {
      case '[role="combobox"]':
        return mode === 'live-rootless-no-ids' ? [combobox, editorCombobox] : [combobox];
      case '[role="listbox"]':
        return listbox.visible ? [listbox] : [];
      case '[role="dialog"], [data-testid*="composer"], [aria-label*="Create post"], [aria-label*="创建帖子"]':
        return mode === 'live-rootless-no-ids' ? [] : [root];
      default:
        return [];
      }
    },
  };
  const location = {
    href: 'https://business.facebook.com/latest/composer/?asset_id=1241410172381837',
    hostname: 'business.facebook.com',
    pathname: '/latest/composer/',
  };
  const sandbox = { document, location, URL };
  const steps = [];
  for (let attempt = 0; attempt < 6; attempt += 1) {
    steps.push(vm.runInNewContext(source, sandbox));
    if (steps[steps.length - 1].state === 'confirmed') break;
    if (mode === 'delayed-listbox' && attempt === 1) listbox.visible = true;
  }
  process.stdout.write(JSON.stringify({
    steps,
    comboboxText: combobox.innerText,
    comboboxClicks: combobox.clicks,
    options: options.map(option => ({
      name: option.innerText,
      selected: option.attrs['aria-selected'],
      clicks: option.clicks,
    })),
  }));
});

function makeNode(role, text, attrs = {}) {
  return {
    role,
    tagName: 'DIV',
    innerText: text,
    textContent: text,
    attrs: { role, ...attrs },
    visible: true,
    disabled: false,
    clicks: 0,
    getClientRects() { return this.visible ? [{}] : []; },
    getAttribute(name) { return this.attrs[name] ?? null; },
    getAttributeNames() { return Object.keys(this.attrs); },
    setAttribute(name, value) { this.attrs[name] = String(value); },
    click() {},
    querySelectorAll() { return []; },
    images: [],
  };
}

function makeImage(alt) {
  return {
    alt,
    tagName: 'IMG',
    attrs: { alt },
    visible: true,
    getClientRects() { return this.visible ? [{}] : []; },
    getAttribute(name) { return this.attrs[name] ?? null; },
    getAttributeNames() { return Object.keys(this.attrs); },
    querySelectorAll() { return []; },
  };
}
