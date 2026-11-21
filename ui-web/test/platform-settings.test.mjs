import assert from 'node:assert/strict';
import { test } from 'node:test';
import * as settings from '../src/platformSettings.ts';

const xhsSchema = {
  platform_code: 'xhs',
  version: 2,
  fields: [
    { key: 'visibility', label: '可见范围', type: 'enum', required: false, default: 'public', options: [{ value: 'public', label: '公开' }, { value: 'private', label: '私密' }] },
    { key: 'comment_enabled', label: '允许评论', type: 'boolean', required: false, default: true },
    { key: 'declare_original', label: '声明原创', type: 'boolean', required: false, default: false },
    { key: 'location', label: '位置', type: 'string', required: false, default: '' },
  ],
};

const mixedSchema = {
  platform_code: 'douyin',
  version: 3,
  verification: 'console_verified',
  fields: [
    { key: 'location', label: '位置', type: 'string', required: true, remember: true, default: '' },
    { key: 'friend_mention', label: '@好友', type: 'manual', required: false, remember: false, default: null },
  ],
};

test('platform settings apply schema defaults and serialize in field order', () => {
  assert.deepEqual(settings.parsePlatformSettings(xhsSchema, '{}'), {
    visibility: 'public',
    comment_enabled: true,
    declare_original: false,
    location: '',
  });
  assert.equal(
    settings.serializePlatformSettings(xhsSchema, { declare_original: true, visibility: 'private', comment_enabled: false, location: '上海市徐汇区' }),
    '{"visibility":"private","comment_enabled":false,"declare_original":true,"location":"上海市徐汇区"}',
  );
});

test('platform settings reject unknown, mistyped, and invalid enum values', () => {
  for (const raw of ['[]', '{"collection":"Alpha"}', '{"location":true}', '{"comment_enabled":"true"}', '{"visibility":"friends"}']) {
    assert.throws(() => settings.parsePlatformSettings(xhsSchema, raw));
  }
});

test('platform settings produce a readable summary', () => {
  assert.equal(
    settings.summarizePlatformSettings(xhsSchema, '{"visibility":"private"}'),
    '可见范围：私密 · 允许评论：开启 · 声明原创：关闭 · 位置：未设置',
  );
});

test('required text rejects blank values and manual official fields never enter saved settings', () => {
  assert.throws(() => settings.parsePlatformSettings(mixedSchema, '{}'), /位置不能为空/);
  assert.deepEqual(settings.parsePlatformSettings(mixedSchema, '{"location":"杭州"}'), { location: '杭州' });
  assert.equal(settings.summarizePlatformSettings(mixedSchema, '{"location":"杭州"}'), '位置：杭州');
  assert.throws(() => settings.parsePlatformSettings(mixedSchema, '{"location":"杭州","friend_mention":"某人"}'), /未知设置/);
});

test('draft settings keep required enum visible until the user makes an explicit choice', () => {
  const youtubeSchema = {
    platform_code: 'youtube',
    version: 1,
    fields: [{
      key: 'audience',
      label: '受众',
      type: 'enum',
      required: true,
      default: '',
      options: [
        { value: '', label: '请选择' },
        { value: 'not_made_for_kids', label: '不是面向儿童的内容' },
      ],
    }],
  };

  assert.deepEqual(settings.parsePlatformSettingsDraft(youtubeSchema, '{}'), { audience: '' });
  assert.equal(settings.serializePlatformSettingsDraft(youtubeSchema, { audience: '' }), '{"audience":""}');
  assert.throws(() => settings.serializePlatformSettings(youtubeSchema, { audience: '' }), /受众不能为空/);
});

test('settings summary only includes fields for the selected content type', () => {
  const schema = {
    platform_code: 'douyin',
    version: 3,
    fields: [
      { key: 'visibility', label: '谁可以看', type: 'enum', default: 'public', options: [{ value: 'public', label: '公开' }] },
      { key: 'music', label: '音乐', type: 'string', default: '', content_types: ['graph_text'] },
    ],
  };
  assert.equal(settings.summarizePlatformSettings(schema, '{}', 'video'), '谁可以看：公开');
  assert.equal(settings.summarizePlatformSettings(schema, '{}', 'graph_text'), '谁可以看：公开 · 音乐：未设置');
});

test('serialized publish settings omit fields for another content type', () => {
  const schema = {
    platform_code: 'douyin',
    version: 3,
    fields: [
      { key: 'visibility', label: '谁可以看', type: 'enum', default: 'public', options: [{ value: 'public', label: '公开' }] },
      { key: 'music', label: '音乐', type: 'string', default: '', content_types: ['graph_text'] },
    ],
  };
  assert.equal(
    settings.serializePlatformSettings(schema, { visibility: 'public', music: '' }, 'video'),
    '{"visibility":"public"}',
  );
  assert.equal(
    settings.serializePlatformSettings(schema, { visibility: 'public', music: '' }, 'graph_text'),
    '{"visibility":"public","music":""}',
  );
});

test('remembered optional declarations migrate back to platform defaults', () => {
  assert.equal(
    settings.withoutRememberedOptionalDeclarations(JSON.stringify({
      visibility: 'public',
      self_declaration: 'ai',
      declaration: 'ai',
      content_source: 'ai',
      ai_generated: true,
      altered_content: true,
    })),
    '{"visibility":"public"}',
  );
  assert.equal(settings.withoutRememberedOptionalDeclarations('not-json'), '{}');
});

test('location fields offer one safe suggestion without silently selecting it', () => {
  assert.deepEqual(settings.platformSettingInputHint({ key: 'location' }), {
    placeholder: '建议：杭州滨江天街',
    help: '输入位置后会在平台内模糊匹配，并选择第一个结果；发布前仍可修改。',
  });
  assert.deepEqual(settings.platformSettingInputHint({ key: 'collection' }), {
    placeholder: '',
    help: '',
  });
});
