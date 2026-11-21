import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import {
  KNOWLEDGE_FILE_ACCEPT,
  RECOMMENDED_KNOWLEDGE_CONTEXT_CHARS,
  RECOMMENDED_KNOWLEDGE_TOP_K,
  citationLabel,
  knowledgeAgentDraft,
  knowledgeDocumentStatusLabel,
  knowledgeImportItemLabel,
  knowledgeSearchQuery,
  summarizeKnowledgeImport,
  summarizeKnowledgeImportRun,
} from '../src/knowledge.ts';

const knowledgeView = await readFile(new URL('../src/components/KnowledgeView.tsx', import.meta.url), 'utf8');

test('knowledge base handoff creates an editable Agent draft with an explicit scope', () => {
  assert.deepEqual(knowledgeAgentDraft(12, '品牌资料'), {
    message: '请基于“品牌资料”中的资料回答：',
    mode: 'chat',
    knowledgeScopes: [{ base_id: 12 }],
  });
  assert.match(knowledgeView, /data-testid="knowledge-handoff-agent"/);
  assert.match(knowledgeView, /setAgentDraft\(knowledgeAgentDraft\(activeBaseID, activeBase\.name\)\)/);
  assert.match(knowledgeView, /openAgentDrawer\(\)/);
});

test('knowledge import item labels unchanged no-text sources without reporting a new failure', () => {
  assert.equal(knowledgeImportItemLabel({ name: 'empty.html', status: 'skipped', error_code: 'unchanged_no_extractable_text' }), '未变化，无可提取正文');
  assert.equal(knowledgeImportItemLabel({ name: 'guide.md', status: 'skipped', error_code: 'stale_source_version' }), '旧版本已跳过，当前索引不变');
  assert.equal(knowledgeImportItemLabel({ name: 'guide.md', status: 'skipped' }), '已存在，无需重复索引');
  assert.equal(knowledgeImportItemLabel({ name: 'broken.pdf', status: 'failed', error_code: 'parse_failed' }), '失败（parse_failed）');
});

test('knowledge document status distinguishes indexed, failed, and pending sources', () => {
  assert.equal(knowledgeDocumentStatusLabel({ status: 'ready', error_code: '' }), '已索引');
  assert.equal(knowledgeDocumentStatusLabel({ status: 'failed', error_code: 'parse_failed' }), '失败（parse_failed）');
  assert.equal(knowledgeDocumentStatusLabel({ status: 'pending', error_code: '' }), '待处理');
});

test('knowledge import summary exposes mixed results without hiding failures', () => {
  assert.equal(summarizeKnowledgeImport({ total: 3, imported: 1, skipped: 1, failed: 1, items: [] }), '新增 1 · 已存在 1 · 失败 1');
});

test('knowledge file picker includes OpenWiki and supported local document formats', () => {
  assert.equal(KNOWLEDGE_FILE_ACCEPT, '.md,.markdown,.txt,.html,.htm,.pdf,.docx,.zip');
  assert.equal(RECOMMENDED_KNOWLEDGE_TOP_K, 6);
  assert.equal(RECOMMENDED_KNOWLEDGE_CONTEXT_CHARS, 12000);
});

test('knowledge citations remain attributable to base, source and heading', () => {
  assert.equal(citationLabel({ chunk_id: 7, base_name: '品牌库', source_name: 'voice.md', heading_path: '语气' }), '品牌库 / voice.md / 语气');
});

test('knowledge search uses explicit query before draft context and trims both', () => {
  assert.equal(knowledgeSearchQuery(' 用户收益 ', ' 草稿标题 '), '用户收益');
  assert.equal(knowledgeSearchQuery('', ' 草稿标题 '), '草稿标题');
});

test('knowledge import run summary exposes live progress and persisted completion', () => {
  assert.equal(summarizeKnowledgeImportRun({
    id: 3, base_id: 1, status: 'running', total_files: 27, imported_files: 25,
    skipped_files: 1, failed_files: 1, created_at: '2026-08-01T00:00:00Z', completed_at: '',
  }), '正在同步 · 已扫描 27 · 新增 25 · 已存在 1 · 失败 1');
  assert.equal(summarizeKnowledgeImportRun({
    id: 3, base_id: 1, status: 'completed', total_files: 27, imported_files: 25,
    skipped_files: 2, failed_files: 0, created_at: '2026-08-01T00:00:00Z', completed_at: '2026-08-01T00:01:00Z',
  }), '索引已同步 · 共 27 · 新增 25 · 已存在 2 · 失败 0');
  assert.equal(summarizeKnowledgeImportRun({
    id: 4, base_id: 1, status: 'interrupted', total_files: 4, imported_files: 1,
    skipped_files: 3, failed_files: 0, created_at: '2026-08-01T00:00:00Z', completed_at: '2026-08-01T00:00:10Z',
  }), '同步已中断 · 已扫描 4 · 新增 1 · 已存在 3 · 失败 0');
});
