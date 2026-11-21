'use strict';

const vm = require('node:vm');

let source = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', chunk => { source += chunk; });
process.stdin.on('end', () => {
  const mode = process.argv[2] || 'graph-ready';
  const scope = makeNode('section', '');
  const target = makeNode('combobox', 'chenpaiteng');
  const editor = makeNode('combobox', '', { contenteditable: 'true' });
  const label = mode === 'video-ready' ? '分享' : (mode === 'wrong-control' ? '分享' : '发布');
  const final = makeNode('button', label, {
    'aria-disabled': mode === 'disabled-final' ? 'true' : 'false',
  });
  final.disabled = mode === 'disabled-final';
  scope.append(target, editor);
  if (mode !== 'outside-final') scope.append(final);

  const body = makeNode('body', '');
  if (mode === 'body-only') {
    const targetSide = makeNode('div', '');
    const editorSide = makeNode('div', '');
    targetSide.append(target);
    editorSide.append(editor);
    body.append(targetSide, editorSide, final);
  } else {
    body.append(scope);
    if (mode === 'outside-final') body.append(final);
  }

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
  const finals = mode === 'double-final' ? [final, makeNode('button', label)] : [final];
  if (mode === 'double-final') scope.append(finals[1]);

  scope.querySelectorAll = selector => {
    if (selector === 'button, [role="button"]') return finals.filter(node => node.parentElement === scope);
    return [];
  };
  const document = {
    querySelectorAll(selector) {
      if (selector === '[role="combobox"]') return [...targets, ...editors];
      if (selector === '[contenteditable="true"]') return editors;
      return [];
    },
  };
  const location = { pathname: '/latest/composer/' };
  process.stdout.write(JSON.stringify(vm.runInNewContext(source, { document, location, Set })));
});

function makeNode(role, text, attrs = {}) {
  return {
    role,
    tagName: role.toUpperCase(),
    innerText: text,
    textContent: text,
    attrs: { role, ...attrs },
    visible: true,
    disabled: false,
    children: [],
    parentElement: null,
    getClientRects() { return this.visible ? [{}] : []; },
    getAttribute(name) { return this.attrs[name] ?? null; },
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
