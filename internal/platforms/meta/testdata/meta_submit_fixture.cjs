'use strict';

const vm = require('node:vm');

let source = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', chunk => { source += chunk; });
process.stdin.on('end', () => {
  const mode = process.argv[2] || 'target-only';
  const facebook = mode.startsWith('facebook-');
  const video = mode.startsWith('video-');
  const options = [
    makeNode('option', 'AlphaIntelligence', { 'aria-selected': facebook ? 'true' : 'false' }),
    makeNode('option', 'chenpaiteng', { 'aria-selected': facebook ? 'false' : 'true' }),
  ];
  options[0].images = [makeImage('Facebook')];
  options[1].images = [makeImage('Instagram')];

  const combobox = makeNode('combobox', facebook ? 'AlphaIntelligence' : 'chenpaiteng');
  const editor = makeNode('combobox', mode === 'caption-changed' ? '其他正文' : '正文', {
    contenteditable: 'true',
    'data-alpha-nexus-caption-editor': 'true',
    'aria-label': '在对话框中输入内容，即可为帖子添加文字。',
  });

  if (mode === 'target-changed') {
    options[0].attrs['aria-selected'] = 'true';
    options[1].attrs['aria-selected'] = 'false';
    combobox.innerText = 'AlphaIntelligence';
  }
  if (mode === 'unknown-selected') {
    const unknown = makeNode('option', 'Unexpected Page', { 'aria-selected': 'true' });
    unknown.images = [makeImage('Facebook')];
    options.push(unknown);
    combobox.innerText = 'chenpaiteng Unexpected Page';
  }
  if (mode === 'wrong-target-icon') options[1].images = [makeImage('Facebook')];
  if (mode === 'wrong-connected-icon') options[0].images = [makeImage('Instagram')];
  if (mode === 'missing-target-icon') options[1].images = [];
  if (mode === 'missing-connected-icon') options[0].images = [];

  const listbox = makeNode('listbox', '');
  listbox.visible = mode !== 'delayed-listbox';
  combobox.click = () => {
    combobox.clicks += 1;
    if (mode !== 'delayed-listbox') listbox.visible = true;
  };
  for (const option of options) {
    option.querySelectorAll = selector => selector === 'img[alt]' ? option.images : [];
  }
  listbox.querySelectorAll = selector => selector === '[role="option"]' ? options : [];

  let finalLabel = video ? '分享' : '发布';
  if (mode === 'wrong-content-control') finalLabel = '分享';
  if (mode === 'video-wrong-content-control') finalLabel = '发布';
  const publish = makeNode('button', finalLabel);
  const publishControls = mode === 'double-final'
    ? [publish, makeNode('button', finalLabel)]
    : [publish];
  const remove = makeNode('button', video ? '移除视频' : '移除照片');
  const preview = makeNode(video ? 'video' : 'img', '');
  const filename = makeNode('span', video ? 'video.mp4' : 'image.png');
  const percent = makeNode('span', '100%');
  const progress = makeNode('progressbar', '上传中');
  const input = makeNode('input', '');
  input.files = [{ name: video ? 'video.mp4' : 'image.png' }];

  let complianceControls = mode === 'compliance-exposed'
    ? [makeNode('switch', '商业内容', { 'aria-checked': 'true' })]
    : [];
  if (facebook) {
    complianceControls = [
      makeNode('switch', '创建为广告帖', { 'aria-checked': 'false' }),
      makeNode('radio', '公开 · 任何人都能看到', {
        'aria-checked': mode === 'facebook-private' ? 'false' : 'true',
      }),
      makeNode('radio', '特定分享对象', {
        'aria-checked': mode === 'facebook-private' ? 'true' : 'false',
      }),
    ];
  }

  const scope = makeNode('section', '');
  scope.append(combobox, editor);
  if (mode !== 'outside-final') scope.append(...publishControls);
  if (mode !== 'media-preview-missing' && mode !== 'input-only') scope.append(remove, preview);
  if (mode === 'media-processing') scope.append(progress);
  if (mode === 'input-only') scope.append(input);
  scope.append(filename);
  if (video) scope.append(percent);
  scope.append(...complianceControls);
  scope.querySelectorAll = selector => {
    if (selector === '[role="combobox"]') return [combobox, editor];
    if (selector === '[data-alpha-nexus-caption-editor="true"]') return [editor];
    if (selector === 'button, [role="button"]') {
      return [
        ...(mode === 'outside-final' ? [] : publishControls),
        ...(mode === 'media-preview-missing' || mode === 'input-only' ? [] : [remove]),
      ];
    }
    if (selector === 'img, video, [data-testid*="media_preview"], [data-testid*="preview"]') {
      return mode === 'media-preview-missing' || mode === 'input-only' ? [] : [preview];
    }
    if (selector === '[role="progressbar"], [aria-busy="true"], [data-testid*="progress"], [data-testid*="processing"]') {
      return mode === 'media-processing' ? [progress] : [];
    }
    if (selector === 'div, span') return video ? [filename, percent] : [filename];
    if (selector === '[role="switch"], [role="radio"], [role="checkbox"], input[type="radio"]') return complianceControls;
    if (selector === 'input[type="file"]') return mode === 'input-only' ? [input] : [];
    return [];
  };

  const body = makeNode('body', '');
  if (mode === 'body-only') {
    const targetSide = makeNode('div', '');
    const editorSide = makeNode('div', '');
    targetSide.append(combobox);
    editorSide.append(editor);
    body.append(targetSide, editorSide, scope);
  } else {
    body.append(scope);
  }
  if (mode === 'outside-final') body.append(...publishControls);

  const targets = [combobox];
  const editors = [editor];
  if (mode === 'duplicate-target') {
    const duplicate = makeNode('combobox', 'another target');
    scope.append(duplicate);
    targets.push(duplicate);
  }
  if (mode === 'duplicate-editor') {
    const duplicate = makeNode('combobox', '', { contenteditable: 'true' });
    scope.append(duplicate);
    editors.push(duplicate);
  }

  const document = {
    querySelectorAll(selector) {
      if (selector === '[role="combobox"]') return [...targets, ...editors];
      if (selector === '[contenteditable="true"]') return editors;
      if (selector === '[role="listbox"]') return listbox.visible ? [listbox] : [];
      return [];
    },
  };
  const location = {
    href: 'https://business.facebook.com/latest/composer/?asset_id=1241410172381837',
    hostname: 'business.facebook.com',
    pathname: '/latest/composer/',
  };
  let result;
  const sandbox = { document, location, URL, Set };
  const attempts = mode === 'delayed-listbox' ? 5 : 1;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    result = vm.runInNewContext(source, sandbox);
    if (result.state === 'clicked') break;
    if (mode === 'delayed-listbox' && attempt === 1) listbox.visible = true;
  }
  process.stdout.write(JSON.stringify({
    result,
    publishClicks: publishControls.reduce((sum, control) => sum + control.clicks, 0),
    comboboxClicks: combobox.clicks,
  }));
});

function makeNode(role, text, attrs = {}) {
  return {
    role,
    innerText: text,
    textContent: text,
    tagName: role.toUpperCase(),
    attrs: { role, ...attrs },
    visible: true,
    disabled: false,
    clicks: 0,
    children: [],
    parentElement: null,
    images: [],
    getClientRects() { return this.visible ? [{}] : []; },
    getAttribute(name) { return this.attrs[name] ?? null; },
    getAttributeNames() { return Object.keys(this.attrs); },
    setAttribute(name, value) { this.attrs[name] = String(value); },
    click() { this.clicks += 1; },
    closest() { return null; },
    querySelectorAll() { return []; },
    contains(node) {
      for (let current = node; current; current = current.parentElement) {
        if (current === this) return true;
      }
      return false;
    },
    append(...nodes) {
      for (const node of nodes) {
        node.parentElement = this;
        this.children.push(node);
      }
    },
  };
}

function makeImage(alt) {
  return makeNode('img', '', { alt });
}
