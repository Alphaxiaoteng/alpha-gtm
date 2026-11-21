'use strict';

const vm = require('node:vm');

let source = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', chunk => { source += chunk; });
process.stdin.on('end', () => {
  const mode = process.argv[2] || 'ready';
  const remove = makeNode('button', '移除照片');
  const preview = makeNode('img', '');
  const progress = makeNode('progressbar', '上传中');
  const filename = makeNode('span', 'image.png');
  const input = makeNode('input', '');
  input.files = [{ name: 'image.png' }];

  const scope = makeNode('section', '');
  const target = makeNode('combobox', 'chenpaiteng');
  const editor = makeNode('combobox', '', { contenteditable: 'true' });
  scope.append(target, editor);
  if (['ready', 'processing'].includes(mode)) {
    scope.append(remove, preview, filename);
  }
  if (mode === 'processing') scope.append(progress);
  if (mode === 'input-only') scope.append(input);

  scope.querySelectorAll = selector => {
    if (selector === 'button, [role="button"]') {
      return ['ready', 'processing'].includes(mode) ? [remove] : [];
    }
    if (selector === 'img, video, [data-testid*="media_preview"], [data-testid*="preview"]') {
      return ['ready', 'processing'].includes(mode) ? [preview] : [];
    }
    if (selector === '[role="progressbar"], [aria-busy="true"], [data-testid*="progress"], [data-testid*="processing"]') {
      return mode === 'processing' ? [progress] : [];
    }
    if (selector === 'div, span') {
      return ['ready', 'processing'].includes(mode) ? [filename] : [];
    }
    if (selector === 'input[type="file"]') return mode === 'input-only' ? [input] : [];
    return [];
  };

  const body = makeNode('body', '');
  if (mode === 'body-only') {
    const targetSide = makeNode('div', '');
    const editorSide = makeNode('div', '');
    targetSide.append(target);
    editorSide.append(editor);
    body.append(targetSide, editorSide);
  } else {
    body.append(scope);
  }
  if (mode === 'outside-remove') body.append(remove, preview);

  const targets = [target];
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
      if (selector === 'button, [role="button"]' && mode === 'outside-remove') return [remove];
      if (selector === 'input[type="file"]' && mode === 'input-only') return [input];
      return [];
    },
  };
  process.stdout.write(JSON.stringify(vm.runInNewContext(source, { document, Set })));
});

function makeNode(role, text, attrs = {}) {
  return {
    role,
    innerText: text,
    textContent: text,
    tagName: role.toUpperCase(),
    children: [],
    attrs: { role, ...attrs },
    parentElement: null,
    visible: true,
    getClientRects() { return this.visible ? [{}] : []; },
    getAttribute(name) { return this.attrs[name] ?? null; },
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
