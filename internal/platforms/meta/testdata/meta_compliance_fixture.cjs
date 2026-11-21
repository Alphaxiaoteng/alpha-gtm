'use strict';

const vm = require('node:vm');

let source = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', chunk => { source += chunk; });
process.stdin.on('end', () => {
  const mode = process.argv[2] || 'facebook_public';
  const controls = mode === 'instagram_empty' || mode === 'composer_missing' ||
    mode === 'duplicate_target' || mode === 'duplicate_editor' || mode === 'body_only'
    ? []
    : [
      makeNode('switch', '创建为广告帖', {
        'aria-checked': mode === 'facebook_ad_true' ? 'true' : 'false',
      }),
      makeNode('radio', '公开 · 任何人都能看到', {
        'aria-checked': mode === 'facebook_private' ? 'false' : 'true',
      }),
      makeNode('radio', '特定分享对象', {
        'aria-checked': mode === 'facebook_private' ? 'true' : 'false',
      }),
    ];
  if (mode === 'instagram_unknown_control') {
    controls.splice(0, controls.length, makeNode('switch', '商业合作伙伴', { 'aria-checked': 'true' }));
  }

  const scope = makeNode('section', '');
  const target = makeNode('combobox', 'chenpaiteng');
  const editor = makeNode('combobox', '', { contenteditable: 'true' });
  scope.append(target, editor, ...controls);
  scope.querySelectorAll = selector => {
    if (selector === '[role="switch"], [role="radio"], [role="checkbox"], input[type="radio"]') return controls;
    return [];
  };

  const body = makeNode('body', '');
  if (mode === 'body_only') {
    const targetSide = makeNode('div', '');
    const editorSide = makeNode('div', '');
    targetSide.append(target);
    editorSide.append(editor);
    body.append(targetSide, editorSide);
  } else {
    body.append(scope);
  }

  const targets = mode === 'composer_missing' ? [] : [target];
  const editors = mode === 'composer_missing' ? [] : [editor];
  if (mode === 'duplicate_target') {
    const duplicate = makeNode('combobox', 'another target');
    scope.append(duplicate);
    targets.push(duplicate);
  }
  if (mode === 'duplicate_editor') {
    const duplicate = makeNode('combobox', '', { contenteditable: 'true' });
    scope.append(duplicate);
    editors.push(duplicate);
  }

  const document = {
    querySelectorAll(selector) {
      if (selector === '[role="combobox"]') return [...targets, ...editors];
      if (selector === '[contenteditable="true"]') return editors;
      return [];
    },
  };
  const location = {
    hostname: 'business.facebook.com',
    pathname: '/latest/composer/',
  };
  process.stdout.write(JSON.stringify(vm.runInNewContext(source, { document, location, Set })));
});

function makeNode(role, text, attrs = {}) {
  return {
    role,
    innerText: text,
    textContent: text,
    tagName: role.toUpperCase(),
    attrs: { role, ...attrs },
    checked: attrs['aria-checked'] === 'true',
    visible: true,
    parentElement: null,
    children: [],
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
