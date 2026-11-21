import type { KnowledgeCitation, KnowledgeDocument, KnowledgeImportItem, KnowledgeImportResult, KnowledgeImportRun } from './api';
import type { AgentDraft } from './store';

export const KNOWLEDGE_FILE_ACCEPT = '.md,.markdown,.txt,.html,.htm,.pdf,.docx,.zip';
export const RECOMMENDED_KNOWLEDGE_TOP_K = 6;
export const RECOMMENDED_KNOWLEDGE_CONTEXT_CHARS = 12000;

export function knowledgeAgentDraft(baseID: number, baseName: string): AgentDraft {
  return {
    message: `请基于“${baseName.trim()}”中的资料回答：`,
    mode: 'chat',
    knowledgeScopes: [{ base_id: baseID }],
  };
}

export function summarizeKnowledgeImport(result: KnowledgeImportResult): string {
  return `新增 ${result.imported} · 已存在 ${result.skipped} · 失败 ${result.failed}`;
}

export function summarizeKnowledgeImportRun(run: KnowledgeImportRun): string {
  if (run.status === 'running') {
    return `正在同步 · 已扫描 ${run.total_files} · 新增 ${run.imported_files} · 已存在 ${run.skipped_files} · 失败 ${run.failed_files}`;
  }
  if (run.status === 'interrupted') {
    return `同步已中断 · 已扫描 ${run.total_files} · 新增 ${run.imported_files} · 已存在 ${run.skipped_files} · 失败 ${run.failed_files}`;
  }
  return `索引已同步 · 共 ${run.total_files} · 新增 ${run.imported_files} · 已存在 ${run.skipped_files} · 失败 ${run.failed_files}`;
}

export function knowledgeImportItemLabel(item: KnowledgeImportItem): string {
  if (item.status === 'imported') return '已索引';
  if (item.status === 'failed') return `失败（${item.error_code || 'parse_failed'}）`;
  if (item.error_code === 'unchanged_no_extractable_text') return '未变化，无可提取正文';
  if (item.error_code === 'stale_source_version') return '旧版本已跳过，当前索引不变';
  return '已存在，无需重复索引';
}

export function knowledgeDocumentStatusLabel(document: Pick<KnowledgeDocument, 'status' | 'error_code'>): string {
  if (document.status === 'ready') return '已索引';
  if (document.status === 'failed') return `失败（${document.error_code || 'parse_failed'}）`;
  return '待处理';
}

export function citationLabel(citation: KnowledgeCitation): string {
  return [citation.base_name, citation.source_name, citation.heading_path].filter(Boolean).join(' / ');
}

export function knowledgeSearchQuery(explicitQuery: string, draftContext: string): string {
  return explicitQuery.trim() || draftContext.trim();
}
