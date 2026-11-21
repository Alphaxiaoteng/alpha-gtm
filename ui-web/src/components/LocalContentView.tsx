import { useEffect, useMemo, useRef, useState } from 'react';
import useSWR from 'swr';
import ReactMarkdown from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import {
  CalendarClock,
  FileText,
  FilePlus2,
  FolderOpen,
  Images,
  Pencil,
  Save,
  Search,
  Send,
  Loader2,
  Trash2,
  UploadCloud,
  Video,
  X,
} from 'lucide-react';
import { api, apiErrorText, type ContentAssistAction, type ContentItem, type MediaLibraryItem, type StudioScheduleHandoff } from '../api';
import { extractContentTags } from '../contentTags';
import { BEFORE_WORKSPACE_CHANGE_EVENT, useUIStore } from '../store';
import { ContentCover } from './ContentCover';
import { LocalLoadingState } from './LocalLoadingState';
import { PlatformIcon } from './PlatformIcon';

export type LocalContentKind = 'video' | 'graph_text' | 'article';

// 编辑器里可用的 AI 动作。只放对写文案真正有用的三种，避免下拉里塞满
// 数据复盘、分镜这类别处才需要的能力。
const assistActions: Array<{ action: ContentAssistAction; label: string; hint: string }> = [
  { action: 'title', label: '生成标题', hint: '基于当前正文给几个可选标题' },
  { action: 'rewrite', label: '润色正文', hint: '保留原意，改得更像人写的' },
  { action: 'check', label: '检查问题', hint: '找出口吻、事实与合规上的风险' },
];

type Draft = {
  title: string;
  caption: string;
  body: string;
  tags: string;
  plannedAt: string;
};

const emptyDraft = (): Draft => ({
  title: '',
  caption: '',
  body: '',
  tags: '',
  plannedAt: '',
});

function contentMatchesKind(item: ContentItem, kind: LocalContentKind) {
  return item.type === kind;
}

function parseTags(value: string): string[] {
  try {
    const parsed = JSON.parse(value || '[]');
    return Array.isArray(parsed)
      ? parsed.filter((tag): tag is string => typeof tag === 'string' && Boolean(tag.trim()))
      : [];
  } catch {
    return [];
  }
}

function tagsFromInput(value: string): string[] {
  return Array.from(new Set(
    value
      .split(/[\s,，;；、]+/)
      .map((tag) => tag.replace(/^#+|#+$/gu, '').trim())
      .filter(Boolean),
  ));
}

function draftFromContent(item: ContentItem): Draft {
  return {
    title: item.title,
    caption: item.caption,
    body: item.body,
    tags: parseTags(item.tags_json).join(', '),
    plannedAt: toDateTimeLocal(item.planned_at),
  };
}

function toDateTimeLocal(value?: string): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function toPlannedAt(value: string): string | null {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

function quickPlannedAt(dayOffset: number) {
  const date = new Date();
  date.setDate(date.getDate() + dayOffset);
  date.setHours(20, 0, 0, 0);
  return toDateTimeLocal(date.toISOString());
}

function appendUniqueFiles(current: File[], incoming: File[]) {
  const keys = new Set(current.map((file) => `${file.name}:${file.size}:${file.lastModified}`));
  return [...current, ...incoming.filter((file) => {
    const key = `${file.name}:${file.size}:${file.lastModified}`;
    if (keys.has(key)) return false;
    keys.add(key);
    return true;
  })];
}

function mediaTypeLabel(type: ContentItem['type']) {
  return type === 'article' ? '文章' : type === 'graph_text' ? '图文' : '视频';
}

const contentTypeOptions = [
  {
    value: 'article',
    label: '文章',
    description: 'Markdown 长文创作',
    icon: FileText,
    iconClassName: 'bg-brand-accent/10 text-brand-accent',
  },
  {
    value: 'graph_text',
    label: '图文',
    description: '图片配文内容',
    icon: Images,
    iconClassName: 'bg-rose-50 text-rose-600',
  },
  {
    value: 'video',
    label: '视频',
    description: '上传视频内容',
    icon: Video,
    iconClassName: 'bg-brand-warning/10 text-brand-warning',
  },
] as const;

function contentStatusLabel(status: ContentItem['status']) {
  return status === 'published' ? '已发布' : status === 'archived' ? '已归档' : '草稿';
}

function contentWasPublished(item: ContentItem) {
  return item.publish_count > 0 || item.status === 'published';
}

function publicationLabel(item: ContentItem) {
  return contentWasPublished(item) ? '已发布' : '未发布';
}

function studioHandoffLabel(handoff: StudioScheduleHandoff) {
  return `${handoff.original_local_time.replace('T', ' ')} · ${handoff.timezone} · ${handoff.status}`;
}

function MarkdownBody({ body, emptyText }: { body: string; emptyText: string }) {
  const { toast } = useUIStore();
  const openLink = async (href?: string) => {
    if (!href || !/^https?:\/\//i.test(href)) {
      toast.show('只支持打开 HTTP 或 HTTPS 链接', 'error');
      return;
    }
    if (window.electronAPI?.openExternalURL) {
      const result = await window.electronAPI.openExternalURL(href);
      if (!result.ok) toast.show(result.error || '打开链接失败', 'error');
      return;
    }
    const opened = window.open(href, '_blank', 'noopener,noreferrer');
    if (!opened) toast.show('浏览器阻止了新窗口，请允许弹窗后重试', 'error');
  };
  if (!body) return <p className="text-gray-400">{emptyText}</p>;
  return <ReactMarkdown
    remarkPlugins={[remarkGfm, remarkBreaks]}
    components={{
      h1: ({ children }) => <h1 className="mb-4 border-b pb-2 text-2xl font-bold">{children}</h1>,
      h2: ({ children }) => <h2 className="mb-3 mt-5 text-xl font-bold">{children}</h2>,
      h3: ({ children }) => <h3 className="mb-2 mt-4 text-lg font-bold">{children}</h3>,
      p: ({ children }) => <p className="my-3">{children}</p>,
      a: ({ children, href }) => <a href={href} onClick={(event) => {
        event.preventDefault();
        void openLink(href);
      }} className="text-brand-accent underline">{children}</a>,
      blockquote: ({ children }) => <blockquote className="my-3 border-l-4 border-brand-accent bg-brand-bg px-4 py-2 text-gray-600">{children}</blockquote>,
      code: ({ children, className }) => <code className={className ? 'block overflow-x-auto rounded-lg bg-gray-950 p-3 text-gray-100' : 'rounded bg-gray-100 px-1 py-0.5 font-mono text-[0.9em]'}>{children}</code>,
      ul: ({ children }) => <ul className="my-3 list-disc pl-6">{children}</ul>,
      ol: ({ children }) => <ol className="my-3 list-decimal pl-6">{children}</ol>,
      table: ({ children }) => <div className="overflow-x-auto"><table className="my-3 w-full border-collapse border border-brand-border">{children}</table></div>,
      th: ({ children }) => <th className="border border-brand-border bg-brand-bg px-2 py-1 text-left">{children}</th>,
      td: ({ children }) => <td className="border border-brand-border px-2 py-1">{children}</td>,
    }}
  >{body}</ReactMarkdown>;
}

export type ContentPublishInfo = {
  platformCode: string;
  status: string; // published | pending | failed | ...
  accountId: number;
};

export function LocalContentView({
  kind,
  domain = 'media',
  publishByContent,
  accountFilter,
}: {
  kind: LocalContentKind;
  domain?: 'media' | 'shop';
  /** 内容 -> 各平台发布记录（用于社交页打通账号/发布，read-only 展示）。 */
  publishByContent?: Map<number, ContentPublishInfo[]>;
  /** 仅显示关联了指定账号的内容（社交页按账号筛选）。 */
  accountFilter?: number | null;
}) {
  const { toast, requestPublishHandoff, setActiveTab } = useUIStore();
  const contentAPI = domain === 'shop' ? {
    list: api.getShopContents,
    create: api.createShopContent,
    update: api.updateShopContent,
    remove: api.deleteShopContent,
    media: api.getShopContentMedia,
    attach: api.attachShopContentMediaFromLibrary,
  } : {
    list: api.getContents,
    create: api.createContent,
    update: api.updateContent,
    remove: api.deleteContent,
    media: api.getContentMedia,
    attach: api.attachContentMediaFromLibrary,
  };
  const { data: contents, error: contentsError, mutate: mutateContents } = useSWR(`api/${domain}/contents`, contentAPI.list);
  const { data: mediaLibrary } = useSWR('api/media/library', api.getMediaLibrary);
  const { data: studioHandoffs = [] } = useSWR(
    domain === 'media' ? 'api/studio/schedule-handoffs' : null,
    api.getStudioScheduleHandoffs,
  );
  const [detailID, setDetailID] = useState<number | null>(null);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [isEditorOpen, setIsEditorOpen] = useState(false);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  // AI 辅助：只在编辑器里回填，不自动保存——用户要先看到结果再决定。
  const [assistBusy, setAssistBusy] = useState('');
  const [assistResult, setAssistResult] = useState<{ action: ContentAssistAction; label: string; text: string } | null>(null);
  const [createType, setCreateType] = useState<ContentItem['type'] | null>(null);
  const [videoFile, setVideoFile] = useState<File | null>(null);
  const [imageFiles, setImageFiles] = useState<File[]>([]);
  const [libraryPickerID, setLibraryPickerID] = useState(0);
  const [pendingLibraryIDs, setPendingLibraryIDs] = useState<number[]>([]);
  const [selectedCoverLibraryID, setSelectedCoverLibraryID] = useState(0);
  const [saving, setSaving] = useState(false);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleanupIDs, setCleanupIDs] = useState<number[]>([]);
  const [cleanupBusy, setCleanupBusy] = useState(false);
  const [contentSearch, setContentSearch] = useState('');
  const videoInputRef = useRef<HTMLInputElement>(null);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const baselineDraftRef = useRef<Draft>(emptyDraft());

  const visibleContents = useMemo(() => {
    const query = contentSearch.trim().toLocaleLowerCase();
    return (contents ?? []).filter((item) => contentMatchesKind(item, kind)
      && (!query || [item.title, item.caption, item.body]
        .some((value) => value.toLocaleLowerCase().includes(query)))
      && (!accountFilter || (publishByContent?.get(item.id) ?? []).some((p) => p.accountId === accountFilter)));
  }, [contentSearch, contents, kind, accountFilter, publishByContent]);
  const studioHandoffByContent = useMemo(() => {
    const latest = new Map<number, StudioScheduleHandoff>();
    for (const handoff of studioHandoffs) {
      const current = latest.get(handoff.content_id);
      if (!current || handoff.updated_at > current.updated_at) latest.set(handoff.content_id, handoff);
    }
    return latest;
  }, [studioHandoffs]);
  const selected = useMemo(
    () => (contents ?? []).find((item) => item.id === selectedID) ?? null,
    [contents, selectedID],
  );
  const detail = useMemo(
    () => (contents ?? []).find((item) => item.id === detailID) ?? null,
    [contents, detailID],
  );

  // 已保存的知识库引用。只在详情打开时拉取，没选内容就不发请求。
  const detailCitations = useSWR(
    detail ? `api/contents/${detail.id}/knowledge-citations` : null,
    () => api.getContentKnowledgeCitations(detail!.id),
    { revalidateOnFocus: false },
  );
  const cleanupCandidates = useMemo(
    () => visibleContents.filter((item) => !contentWasPublished(item)),
    [visibleContents],
  );
  const { data: selectedMedia, mutate: mutateMedia } = useSWR(
    selectedID ? `api/${domain}/contents/${selectedID}/media` : null,
    () => contentAPI.media(selectedID!),
  );
  const hasExistingVideo = Boolean(
    selectedMedia?.some((media) => media.mime_type.toLowerCase().startsWith('video/')),
  );
  const hasExistingImages = Boolean(
    selectedMedia?.some((media) => media.mime_type.toLowerCase().startsWith('image/')),
  );
  const selectedType: ContentItem['type'] = selected?.type
    ?? createType
    ?? kind;

  async function runAssist(action: ContentAssistAction) {
    if (!selected || assistBusy) return;
    const item = assistActions.find((candidate) => candidate.action === action);
    setAssistBusy(action);
    setAssistResult(null);
    try {
      const response = await api.assistContent({
        action,
        title: draft.title,
        caption: draft.caption,
        body: draft.body,
        platform: '',
        allow_external: false,
        use_knowledge: false,
      });
      const text = (response.result || '').trim();
      if (!text) {
        toast.show('AI 没有返回内容', 'error');
        return;
      }
      setAssistResult({ action, label: item?.label ?? 'AI', text });
    } catch (reason) {
      toast.show(`AI 辅助失败：${apiErrorText(reason)}`, 'error');
    } finally {
      setAssistBusy('');
    }
  }

  function applyAssist() {
    if (!assistResult) return;
    // 标题类结果填标题，其余填正文，避免覆盖用户正在写的另一部分。
    setDraft((current) => (
      assistResult.action === 'title'
        ? { ...current, title: assistResult.text }
        : { ...current, body: assistResult.text }
    ));
    setAssistResult(null);
    toast.show('已填入编辑器，记得保存', 'success');
  }

  const recognizedTags = useMemo(
    () => Array.from(new Set([
      ...extractContentTags(selectedType === 'article' ? draft.body : draft.caption),
      ...tagsFromInput(draft.tags),
    ])),
    [draft.body, draft.caption, draft.tags, selectedType],
  );
  const attachedLibraryIDs = useMemo(
    () => new Set((selectedMedia ?? []).map((item) => item.library_id)),
    [selectedMedia],
  );
  const compatibleLibraryMedia = useMemo(
    () => (mediaLibrary ?? []).filter((item) => {
      if (attachedLibraryIDs.has(item.id)) return false;
      if (selectedType === 'video') {
        return (item.purpose === 'shared' || item.purpose === 'video')
          && item.mime.toLowerCase().startsWith('video/');
      }
      return (item.purpose === 'shared' || item.purpose === 'graph_text' || item.purpose === 'article')
        && item.mime.toLowerCase().startsWith('image/');
    }),
    [attachedLibraryIDs, mediaLibrary, selectedType],
  );
  const compatibleCoverMedia = useMemo(
    () => (mediaLibrary ?? []).filter((item) =>
      !attachedLibraryIDs.has(item.id)
      && (item.purpose === 'shared' || item.purpose === 'video')
      && item.mime.toLowerCase().startsWith('image/')),
    [attachedLibraryIDs, mediaLibrary],
  );
  const pendingLibraryItems = useMemo(
    () => pendingLibraryIDs
      .map((id) => mediaLibrary?.find((item) => item.id === id))
      .filter((item): item is MediaLibraryItem => Boolean(item)),
    [mediaLibrary, pendingLibraryIDs],
  );
  const pendingLibraryHasVideo = pendingLibraryItems.some((item) =>
    item.mime.toLowerCase().startsWith('video/'));
  const hasUnsavedEditorChanges = useMemo(() => {
    if (!isEditorOpen) return false;
    const draftChanged = JSON.stringify(draft) !== JSON.stringify(baselineDraftRef.current);
    return draftChanged
      || Boolean(videoFile)
      || imageFiles.length > 0
      || pendingLibraryIDs.length > 0
      || selectedCoverLibraryID > 0;
  }, [
    draft,
    imageFiles.length,
    isEditorOpen,
    pendingLibraryIDs.length,
    selectedCoverLibraryID,
    videoFile,
  ]);

  const pendingImageURLs = useMemo(
    () => imageFiles.map((file) => ({ file, url: URL.createObjectURL(file) })),
    [imageFiles],
  );
  const pendingVideoURL = useMemo(
    () => videoFile ? URL.createObjectURL(videoFile) : '',
    [videoFile],
  );
  useEffect(
    () => () => pendingImageURLs.forEach(({ url }) => URL.revokeObjectURL(url)),
    [pendingImageURLs],
  );
  useEffect(() => () => {
    if (pendingVideoURL) URL.revokeObjectURL(pendingVideoURL);
  }, [pendingVideoURL]);

  const clearFiles = () => {
    setVideoFile(null);
    setImageFiles([]);
    setLibraryPickerID(0);
    setPendingLibraryIDs([]);
    setSelectedCoverLibraryID(0);
    if (videoInputRef.current) videoInputRef.current.value = '';
    if (imageInputRef.current) imageInputRef.current.value = '';
  };

  useEffect(() => {
    setDetailID(null);
    setSelectedID(null);
    setIsEditorOpen(false);
    setDraft(emptyDraft());
    setCreateType(null);
    setCleanupOpen(false);
    setCleanupIDs([]);
    clearFiles();
  }, [kind]);

  useEffect(() => {
    if (selected) {
      setDraft(draftFromContent(selected));
      setCreateType(selected.type);
    }
    if (contents && !selected && selectedID !== null) setSelectedID(null);
  }, [contents, selected, selectedID]);

  const addUploadFiles = (files: File[]) => {
    if (selectedType === 'video') {
      const video = files.find((file) => file.type.startsWith('video/'));
      if (video) {
        setVideoFile(video);
        setPendingLibraryIDs([]);
        setLibraryPickerID(0);
      }
      else toast.show('请拖入视频文件', 'error');
      return;
    }
    const images = files.filter((file) => file.type.startsWith('image/'));
    if (!images.length) {
      toast.show('请拖入图片文件', 'error');
      return;
    }
    if (selectedType === 'article') {
      setImageFiles([images[0]]);
    } else {
      setImageFiles((current) => appendUniqueFiles(current, images));
    }
  };

  const addSelectedLibraryMedia = () => {
    if (libraryPickerID <= 0) return;
    if (selectedType === 'video') {
      setPendingLibraryIDs([libraryPickerID]);
      setVideoFile(null);
      if (videoInputRef.current) videoInputRef.current.value = '';
    } else if (selectedType === 'article') {
      setPendingLibraryIDs([libraryPickerID]);
    } else {
      setPendingLibraryIDs((current) => current.includes(libraryPickerID) ? current : [...current, libraryPickerID]);
    }
    setLibraryPickerID(0);
  };

  useEffect(() => {
    if (!isEditorOpen) return;
    const onPaste = (event: ClipboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.matches('input, textarea, [contenteditable="true"]')) return;
      const files = event.clipboardData ? Array.from(event.clipboardData.files) : [];
      if (!files.length) return;
      event.preventDefault();
      addUploadFiles(files);
    };
    document.addEventListener('paste', onPaste);
    return () => document.removeEventListener('paste', onPaste);
  });

  const openNew = () => {
    setSelectedID(null);
    const nextDraft = emptyDraft();
    baselineDraftRef.current = nextDraft;
    setDraft(nextDraft);
    setCreateType(kind);
    clearFiles();
    setIsEditorOpen(true);
  };

  const openEditor = (item: ContentItem) => {
    setSelectedID(item.id);
    const nextDraft = draftFromContent(item);
    baselineDraftRef.current = nextDraft;
    setDraft(nextDraft);
    setCreateType(item.type);
    clearFiles();
    setDetailID(null);
    setIsEditorOpen(true);
  };

  const closeEditor = () => {
    if (hasUnsavedEditorChanges && !window.confirm('当前内容尚未保存，确定放弃这些更改吗？')) return;
    setIsEditorOpen(false);
    setSelectedID(null);
    setDraft(emptyDraft());
    setCreateType(null);
    clearFiles();
  };

  useEffect(() => {
    if (!isEditorOpen || !hasUnsavedEditorChanges) return;
    const onWorkspaceChange = (event: Event) => {
      if (!window.confirm('当前内容尚未保存，确定离开吗？')) event.preventDefault();
    };
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener(BEFORE_WORKSPACE_CHANGE_EVENT, onWorkspaceChange);
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => {
      window.removeEventListener(BEFORE_WORKSPACE_CHANGE_EVENT, onWorkspaceChange);
      window.removeEventListener('beforeunload', onBeforeUnload);
    };
  }, [hasUnsavedEditorChanges, isEditorOpen]);

  const openCleanup = () => {
    setCleanupIDs([]);
    setCleanupOpen(true);
  };

  const cleanupSelected = async () => {
    if (!cleanupIDs.length || cleanupBusy) return;
    if (!window.confirm(`确定清理选中的 ${cleanupIDs.length} 条未发布内容及其关联媒体吗？此操作无法撤销。`)) return;
    setCleanupBusy(true);
    const failed: number[] = [];
    for (const id of cleanupIDs) {
      try {
        await contentAPI.remove(id);
      } catch {
        failed.push(id);
      }
    }
    await mutateContents();
    setCleanupBusy(false);
    setCleanupIDs(failed);
    if (failed.length) {
      toast.show(`${cleanupIDs.length - failed.length} 条已清理，${failed.length} 条因存在发布记录而保留`, 'error');
      return;
    }
    setCleanupOpen(false);
    setDetailID(null);
    setSelectedID(null);
    toast.show(`${cleanupIDs.length} 条内容已清理`, 'success');
  };

  const save = async () => {
    const title = draft.title.trim();
    const body = draft.body.trim();
    if (!title) {
      toast.show(selectedType === 'article' ? '请填写文章标题' : '请填写内容标题', 'error');
      return;
    }
    if (selectedType === 'article' && !body) {
      toast.show('请填写 Markdown 正文', 'error');
      return;
    }
    if (selectedType === 'video' && !videoFile && !hasExistingVideo && !pendingLibraryHasVideo) {
      toast.show('请选择一个视频文件，首帧将作为看板封面', 'error');
      return;
    }
    if (selectedType === 'graph_text' && imageFiles.length === 0 && !hasExistingImages && pendingLibraryIDs.length === 0) {
      toast.show('请选择至少一张图片', 'error');
      return;
    }
    const plannedAt = toPlannedAt(draft.plannedAt);
    if (plannedAt === null) {
      toast.show('排期时间无效', 'error');
      return;
    }

    setSaving(true);
    try {
      const contentBody = {
        type: selectedType,
        title,
        caption: draft.caption.trim(),
        body: selectedType === 'article' ? body : '',
        tags_json: JSON.stringify(recognizedTags),
        status: selected?.status ?? 'draft' as const,
        planned_at: plannedAt,
      };
      const saved = selectedID === null
        ? await contentAPI.create(contentBody)
        : await contentAPI.update(selectedID, contentBody);
      for (const [position, libraryID] of pendingLibraryIDs.entries()) {
        await contentAPI.attach(saved.id, libraryID, 'content', position);
      }
      if (selectedType === 'video' && videoFile) {
        const media = await api.uploadContentVideo(videoFile);
        await contentAPI.attach(saved.id, media.id, 'content', 0);
      }
      if (selectedType !== 'video' && imageFiles.length) {
        const uploaded = await Promise.all(imageFiles.map((image) => api.uploadContentImage(image, selectedType)));
        for (const [index, media] of uploaded.entries()) {
          await contentAPI.attach(saved.id, media.id, 'content', pendingLibraryIDs.length + index);
        }
      }
      if (selectedType === 'video' && selectedCoverLibraryID > 0) {
        await contentAPI.attach(saved.id, selectedCoverLibraryID, 'cover', 0);
      }
      await Promise.all([mutateContents(), mutateMedia()]);
      clearFiles();
      setSelectedID(null);
      setIsEditorOpen(false);
      setDetailID(saved.id);
      toast.show(`${mediaTypeLabel(selectedType)}已保存到内容库`, 'success');
    } catch (reason) {
      toast.show(
        reason instanceof Error ? `保存失败：${reason.message}` : '保存失败；已创建的草稿会保留',
        'error',
      );
    } finally {
      setSaving(false);
    }
  };

  const noun = selectedType === 'article' ? '文章' : selectedType === 'graph_text' ? '图文' : '视频';
  const detailTags = detail ? parseTags(detail.tags_json) : [];
  const editingMediaLoading = selectedID !== null && selectedMedia === undefined;

  return (
    <section className="space-y-5" data-testid={`local-content-${kind}`}>
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="flex items-center gap-2 text-lg font-bold text-gray-900">
            <FolderOpen className="h-5 w-5 text-brand-accent" />
            {mediaTypeLabel(kind)}看板
          </h3>
          <p className="mt-1 text-xs text-gray-500">
            点击封面查看详情。看板固定使用 1:1 封面，录入信息只在添加窗口中填写。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={openCleanup}
            disabled={!visibleContents.length}
            className="ui-btn text-gray-600 disabled:cursor-not-allowed disabled:opacity-40"
            data-testid="local-content-cleanup-open"
          >
            <Trash2 className="h-3.5 w-3.5" />
            清理
          </button>
          <button
            type="button"
            onClick={openNew}
            className="ui-btn ui-btn-primary"
            data-testid="local-content-add"
          >
            <FilePlus2 className="h-3.5 w-3.5" />
            添加内容
          </button>
        </div>
      </header>

      <div data-testid="content-library-board">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <strong className="text-sm text-gray-900">
            {contents === undefined ? '正在准备内容' : `${visibleContents.length} 个内容`}
          </strong>
          <label className="relative min-w-56 flex-1 sm:max-w-sm">
            <span className="sr-only">搜索当前内容库</span>
            <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-400" />
            <input
              type="search"
              value={contentSearch}
              onChange={(event) => setContentSearch(event.target.value)}
              data-testid="local-content-search"
              placeholder="搜索当前内容库…"
              className="min-h-9 w-full rounded-lg border border-brand-border bg-brand-bg py-2 pl-9 pr-3 text-xs text-gray-800 focus:border-brand-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/35"
            />
          </label>
        </div>
        {contents === undefined && !contentsError ? (
          <LocalLoadingState
            label="正在读取本地内容"
            detail="恢复内容、封面和发布状态"
            className="rounded-2xl border border-brand-border bg-brand-surface"
          />
        ) : null}
        {contentsError ? <p className="py-8 text-center text-xs text-red-600">内容加载失败</p> : null}
        {contents !== undefined && !contentsError && visibleContents.length === 0 && contentSearch ? (
          <div className="flex min-h-40 flex-col items-center justify-center rounded-2xl border border-dashed border-brand-border bg-brand-surface text-sm text-gray-500">
            <Search className="mb-2 h-6 w-6" />
            <p>没有匹配的内容</p>
            <button type="button" onClick={() => setContentSearch('')} className="mt-3 text-xs font-semibold text-brand-accent hover:underline">
              清除搜索
            </button>
          </div>
        ) : null}
        {contents !== undefined && !contentsError && visibleContents.length === 0 && !contentSearch ? (
          <button
            type="button"
            onClick={openNew}
            className="flex min-h-56 w-full flex-col items-center justify-center rounded-2xl border border-dashed border-brand-border bg-brand-surface text-sm text-gray-500 hover:border-brand-accent hover:text-brand-accent"
          >
            <FilePlus2 className="mb-2 h-7 w-7" />
            还没有内容，添加第一条
          </button>
        ) : null}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-5 2xl:grid-cols-6">
          {visibleContents.map((item) => (
            (() => {
              const handoff = domain === 'media' ? studioHandoffByContent.get(item.id) : undefined;
              return (
            <button
              key={item.id}
              type="button"
              onClick={() => setDetailID(item.id)}
              data-testid={`local-content-item-${item.id}`}
              aria-label={`查看${item.title || `未命名${mediaTypeLabel(item.type)}`}详情`}
              title={item.title || `未命名${mediaTypeLabel(item.type)}`}
              className="group relative overflow-hidden rounded-lg border border-brand-border bg-brand-surface text-left transition-colors hover:border-gray-400 focus:outline-none focus:ring-2 focus:ring-brand-accent/40"
            >
              <ContentCover content={item} domain={domain} className="aspect-square w-full" />
              <span className="block border-t border-brand-border px-3 py-2.5">
                <strong className="block truncate text-xs text-gray-900">
                  {item.title || `未命名${mediaTypeLabel(item.type)}`}
                </strong>
                <span className="mt-1 block text-[10px] text-gray-500">
                  {mediaTypeLabel(item.type)} · {publicationLabel(item)}
                </span>
                {handoff ? <span data-testid={`local-content-studio-handoff-${item.id}`} className="mt-1 block truncate text-[10px] font-semibold text-brand-accent">来自 Studio · {studioHandoffLabel(handoff)}</span> : null}
                {publishByContent ? (
                  <span className="mt-1.5 flex flex-wrap gap-1">
                    {(publishByContent.get(item.id) ?? []).slice(0, 6).map((p) => {
                      const done = p.status === 'published';
                      const pending = p.status === 'pending' || p.status === 'manual_takeover' || p.status === 'awaiting_review' || p.status === 'submitted';
                      return (
                        <span
                          key={`${p.accountId}-${p.platformCode}`}
                          title={`${p.platformCode} · ${done ? '已发布' : pending ? '进行中' : '未发布'}`}
                          className={`inline-flex items-center gap-0.5 rounded-full px-1.5 py-0.2 text-[9px] font-semibold ring-1 ring-inset ${
                            done
                              ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
                              : pending
                                ? 'bg-brand-accent/10 text-brand-accent ring-brand-accent/20'
                                : 'bg-brand-surface text-brand-muted ring-brand-border'
                          }`}
                        >
                          <PlatformIcon platform={p.platformCode} className="h-2.5 w-2.5" />
                          {done ? '已发' : pending ? '发中' : '未发'}
                        </span>
                      );
                    })}
                  </span>
                ) : null}
              </span>
              <span
                data-testid={`local-content-publication-${item.id}`}
                className={`absolute right-2 top-2 rounded-full px-2.5 py-1 text-[11px] font-bold shadow-sm ring-1 ring-inset ${
                  contentWasPublished(item)
                    ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
                    : 'bg-brand-surface text-brand-fg-subtle ring-brand-border'
                }`}
              >
                {publicationLabel(item)}
              </span>
            </button>
              );
            })()
          ))}
        </div>
      </div>

      {cleanupOpen ? (
        <div
          data-testid="local-content-cleanup-modal"
          className="fixed inset-0 z-[70] flex items-center justify-center bg-black/55 p-4 backdrop-blur-sm"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget && !cleanupBusy) setCleanupOpen(false);
          }}
        >
          <article role="dialog" aria-modal="true" aria-label="清理未发布内容" className="max-h-[82vh] w-full max-w-xl overflow-hidden rounded-2xl border border-brand-border bg-brand-surface shadow-2xl">
            <header className="flex items-center justify-between border-b border-brand-border px-5 py-4">
              <div>
                <p className="text-[11px] font-semibold text-brand-accent">内容库维护</p>
                <h4 className="mt-0.5 text-lg font-bold text-gray-900">清理未发布内容</h4>
              </div>
              <button
                type="button"
                onClick={() => setCleanupOpen(false)}
                disabled={cleanupBusy}
                aria-label="关闭清理窗口"
                className="rounded-full p-2 text-gray-500 hover:bg-black/5 hover:text-gray-900 disabled:opacity-40"
              >
                <X className="h-5 w-5" />
              </button>
            </header>

            <div className="space-y-4 overflow-y-auto p-5">
              <p className="rounded-xl border border-brand-warning/30 bg-brand-warning/10 px-3 py-2 text-xs leading-5 text-brand-warning">
                有发布记录的内容会保留。清理只删除你勾选的未发布内容及其关联媒体，删除后无法撤销。
              </p>
              <div className="flex items-center justify-between gap-3">
                <span className="text-xs text-gray-500">
                  可清理 {cleanupCandidates.length} 条 · 已保留 {visibleContents.length - cleanupCandidates.length} 条
                </span>
                <button
                  type="button"
                  onClick={() => setCleanupIDs(
                    cleanupIDs.length === cleanupCandidates.length
                      ? []
                      : cleanupCandidates.map((item) => item.id),
                  )}
                  disabled={!cleanupCandidates.length || cleanupBusy}
                  className="text-xs font-semibold text-brand-accent disabled:opacity-40"
                >
                  {cleanupIDs.length === cleanupCandidates.length && cleanupCandidates.length ? '取消全选' : '全选可清理内容'}
                </button>
              </div>

              <div className="space-y-2">
                {visibleContents.map((item) => {
                  const protectedItem = contentWasPublished(item);
                  const checked = cleanupIDs.includes(item.id);
                  return (
                    <label
                      key={item.id}
                      data-testid={`local-content-cleanup-item-${item.id}`}
                      className={`flex items-center gap-3 rounded-xl border p-2.5 ${
                        protectedItem ? 'cursor-not-allowed border-brand-border bg-brand-bg opacity-65' : 'cursor-pointer border-brand-border hover:border-brand-accent'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={protectedItem || cleanupBusy}
                        onChange={(event) => setCleanupIDs((current) => (
                          event.target.checked
                            ? [...current, item.id]
                            : current.filter((id) => id !== item.id)
                        ))}
                        className="h-4 w-4 accent-brand-accent"
                        aria-label={`选择清理 ${item.title || `未命名${mediaTypeLabel(item.type)}`}`}
                      />
                      <ContentCover content={item} domain={domain} className="h-12 w-12 shrink-0 rounded-lg" />
                      <span className="min-w-0 flex-1">
                        <strong className="block truncate text-sm text-gray-900">{item.title || `未命名${mediaTypeLabel(item.type)}`}</strong>
                        <span className="mt-0.5 block text-[11px] text-gray-500">{mediaTypeLabel(item.type)} · {publicationLabel(item)}</span>
                      </span>
                      <span className={`rounded-full px-2 py-1 text-[10px] font-semibold ${protectedItem ? 'bg-emerald-50 text-emerald-700' : 'bg-gray-100 text-gray-600'}`}>
                        {protectedItem ? '保留' : '可清理'}
                      </span>
                    </label>
                  );
                })}
                {!visibleContents.length ? <p className="py-8 text-center text-sm text-gray-400">没有可清理的内容</p> : null}
              </div>
            </div>

            <footer className="flex items-center justify-between border-t border-brand-border px-5 py-4">
              <span className="text-xs text-gray-500">已选择 {cleanupIDs.length} 条</span>
              <div className="flex items-center gap-2">
                <button type="button" onClick={() => setCleanupOpen(false)} disabled={cleanupBusy} className="ui-btn">取消</button>
                <button
                  type="button"
                  onClick={() => void cleanupSelected()}
                  disabled={!cleanupIDs.length || cleanupBusy}
                  className="ui-btn border-red-200 bg-red-50 text-red-700 hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  {cleanupBusy ? '清理中…' : `清理 ${cleanupIDs.length || ''}`}
                </button>
              </div>
            </footer>
          </article>
        </div>
      ) : null}

      {detail ? (
        <div
          data-testid="local-content-detail-modal"
          className="fixed inset-0 z-[70] flex items-center justify-center bg-black/55 p-4 backdrop-blur-sm"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setDetailID(null);
          }}
        >
          <article role="dialog" aria-modal="true" aria-label="内容详情" className="max-h-[88vh] w-full max-w-4xl overflow-y-auto rounded-2xl border border-brand-border bg-brand-surface shadow-2xl">
            <header className="sticky top-0 z-10 flex items-center justify-between border-b border-brand-border bg-brand-surface/95 px-5 py-4 backdrop-blur">
              <div>
                <p className="text-[11px] font-semibold text-brand-accent">{mediaTypeLabel(detail.type)}</p>
                <h4 className="mt-0.5 text-lg font-bold text-gray-900">内容详情</h4>
              </div>
              <button type="button" onClick={() => setDetailID(null)} aria-label="关闭内容详情" className="rounded-full p-2 text-gray-500 hover:bg-black/5 hover:text-gray-900">
                <X className="h-5 w-5" />
              </button>
            </header>

            <div className="grid items-start gap-6 p-5 md:grid-cols-[minmax(16rem,22rem)_minmax(0,1fr)]">
              <div data-testid="local-content-detail-cover" className="md:sticky md:top-24">
                <ContentCover content={detail} domain={domain} controls className="aspect-square w-full rounded-2xl border border-brand-border" />
              </div>
              <div data-testid="local-content-detail-fields" className="min-w-0 space-y-5">
                <dl>
                  <div data-testid="local-content-detail-title" className="rounded-xl border border-brand-border bg-brand-bg p-4">
                    <dt className="text-xs font-semibold text-gray-500">标题</dt>
                    <dd className="mt-2 break-words text-lg font-bold leading-7 text-gray-900">{detail.title || `未命名${mediaTypeLabel(detail.type)}`}</dd>
                  </div>
                </dl>

                <dl className="grid grid-cols-2 gap-3 text-xs">
                  <div className="rounded-xl bg-brand-bg p-3"><dt className="text-gray-500">类型</dt><dd className="mt-1 font-semibold text-gray-900">{mediaTypeLabel(detail.type)}</dd></div>
                  <div data-testid="local-content-detail-publication" className="rounded-xl bg-brand-bg p-3">
                    <dt className="text-gray-500">发布状态</dt>
                    <dd className={`mt-1 font-bold ${contentWasPublished(detail) ? 'text-emerald-700' : 'text-gray-700'}`}>{publicationLabel(detail)}</dd>
                  </div>
                  <div className="rounded-xl bg-brand-bg p-3"><dt className="text-gray-500">内容状态</dt><dd className="mt-1 font-semibold text-gray-900">{contentStatusLabel(detail.status)}</dd></div>
                  {(detailCitations.data ?? []).length > 0 ? (
                    <div data-testid="local-content-detail-citations" className="rounded-xl bg-brand-bg p-3">
                      <dt className="text-gray-500">知识库引用</dt>
                      <dd className="mt-1 space-y-0.5">
                        {(detailCitations.data ?? []).map((citation) => (
                          <span key={citation.chunk_id} className="block truncate text-[11px] text-gray-700" title={`${citation.base_name} / ${citation.source_name}`}>
                            {citation.base_name} · {citation.source_name}{citation.heading_path ? ` · ${citation.heading_path}` : ''}
                          </span>
                        ))}
                      </dd>
                    </div>
                  ) : null}
                  <div className="rounded-xl bg-brand-bg p-3"><dt className="text-gray-500">排期</dt><dd className="mt-1 font-semibold text-gray-900">{domain === 'media' && studioHandoffByContent.get(detail.id) ? <span data-testid="local-content-detail-studio-handoff">来自 Studio · {studioHandoffLabel(studioHandoffByContent.get(detail.id)!)}</span> : detail.planned_at ? new Date(detail.planned_at).toLocaleString() : '未排期'}</dd></div>
                  <div className="rounded-xl bg-brand-bg p-3"><dt className="text-gray-500">发布次数</dt><dd className="mt-1 font-semibold text-gray-900">{detail.publish_count}</dd></div>
                </dl>

                {publishByContent ? (
                  <div data-testid="local-content-detail-platforms" className="rounded-xl border border-brand-border bg-brand-bg p-4">
                    <p className="text-xs font-semibold text-gray-500">各平台发布状态</p>
                    {(publishByContent.get(detail.id) ?? []).length ? (
                      <ul className="mt-2.5 space-y-1.5">
                        {(publishByContent.get(detail.id) ?? []).map((p) => (
                          <li key={`${p.accountId}-${p.platformCode}`} className="flex items-center gap-2 text-xs">
                            <PlatformIcon platform={p.platformCode} className="h-3.5 w-3.5" />
                            <span className="min-w-0 flex-1 truncate text-gray-700">{p.platformCode}</span>
                            <span className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold ring-1 ring-inset ${
                              p.status === 'published'
                                ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
                                : p.status === 'pending'
                                  ? 'bg-brand-accent/10 text-brand-accent ring-brand-accent/20'
                                  : 'bg-brand-surface text-brand-muted ring-brand-border'
                            }`}>
                              {p.status === 'published' ? '已发布' : p.status === 'pending' ? '进行中' : '未发布'}
                            </span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="mt-2 text-xs text-gray-500">该内容尚未发布到任何平台账号。</p>
                    )}
                  </div>
                ) : null}

                <dl className="space-y-5">
                  <div>
                    <dt className="text-xs font-semibold text-gray-700">说明 / 摘要</dt>
                    <dd className="mt-2 whitespace-pre-wrap text-sm leading-6 text-gray-700">{detail.caption || '未填写'}</dd>
                  </div>

                  <div>
                    <dt className="text-xs font-semibold text-gray-700">标签</dt>
                    <dd className="mt-2 flex flex-wrap gap-2">
                      {detailTags.length
                        ? detailTags.map((tag) => <span key={tag} className="rounded-full bg-brand-accent/10 px-2.5 py-1 text-[11px] font-semibold text-brand-accent">{tag}</span>)
                        : <span className="text-sm text-gray-400">未填写</span>}
                    </dd>
                  </div>

                  {detail.type === 'article' ? (
                    <div>
                      <dt className="text-xs font-semibold text-gray-700">Markdown 正文</dt>
                      <dd className="mt-2 rounded-xl border border-brand-border bg-white px-5 py-4 text-sm leading-7 text-gray-800">
                        <MarkdownBody body={detail.body} emptyText="未填写正文" />
                      </dd>
                    </div>
                  ) : null}
                </dl>
              </div>
            </div>

            <footer className="flex items-center justify-between gap-2 border-t border-brand-border px-5 py-4">
              <div>
                {!contentWasPublished(detail) && (
                  <button
                    type="button"
                    onClick={async () => {
                      if (window.confirm(`确定要彻底删除该内容「${detail.title || '未命名'}」吗？`)) {
                        try {
                          await contentAPI.remove(detail.id);
                          setDetailID(null);
                          await mutateContents();
                          toast.show('内容已删除', 'success');
                        } catch (err) {
                          toast.show(err instanceof Error ? err.message : '删除失败', 'error');
                        }
                      }
                    }}
                    className="ui-btn text-rose-600 hover:bg-rose-50 hover:border-rose-300"
                    title="彻底删除此内容条目"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    ���除此内容
                  </button>
                )}
              </div>
              <div className="flex items-center gap-2">
                {publishByContent ? (
                  <button
                    type="button"
                    data-testid={`local-content-handoff-${detail.id}`}
                    title="把该内容交给发布页人工驱动的发布流程（不会自动提交）"
                    onClick={() => {
                      requestPublishHandoff({ contentID: detail.id, accountID: accountFilter ?? null });
                      setDetailID(null);
                      setActiveTab('publish');
                    }}
                    className="ui-btn"
                  >
                    <Send className="h-3.5 w-3.5" />
                    手动交接发布
                  </button>
                ) : null}
                <button type="button" onClick={() => setDetailID(null)} className="ui-btn">关闭</button>
                <button type="button" onClick={() => openEditor(detail)} className="ui-btn ui-btn-primary">
                  <Pencil className="h-3.5 w-3.5" />
                  编辑信息
                </button>
              </div>
            </footer>
          </article>
        </div>
      ) : null}

      {isEditorOpen ? (
        <div
          data-testid="local-content-add-modal"
          className="fixed inset-0 z-[70] flex items-center justify-center bg-black/55 p-4 backdrop-blur-sm"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget && !saving) closeEditor();
          }}
        >
          <article role="dialog" aria-modal="true" aria-label={selectedID === null ? '添加内容' : '编辑内容'} className="flex max-h-[90vh] w-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-brand-border bg-brand-surface shadow-2xl">
            <header className="flex items-center justify-between border-b border-brand-border px-5 py-4">
              <div>
                <p className="text-[11px] font-semibold text-brand-accent">{selectedID === null ? '添加到内容库' : '编辑内容'}</p>
                <h4 className="mt-0.5 text-lg font-bold text-gray-900">
                  {selectedID === null && createType === null ? '选择内容类型' : selectedID === null ? `新建${noun}` : `编辑${noun}`}
                </h4>
              </div>
              <button type="button" onClick={closeEditor} disabled={saving} aria-label="关闭内容表单" className="rounded-full p-2 text-gray-500 hover:bg-black/5 hover:text-gray-900 disabled:opacity-40">
                <X className="h-5 w-5" />
              </button>
            </header>

            {selectedID === null && createType === null ? (
              <div data-testid="local-content-type-picker" className="min-h-0 flex-1 overflow-y-auto p-6">
                <p className="text-sm text-gray-600">先选择要创建的内容，下一步只显示对应字段。</p>
                <div className="mt-5 grid gap-3 sm:grid-cols-3">
                  {contentTypeOptions.map(({ value, label, description, icon: Icon, iconClassName }) => (
                    <button
                      key={value}
                      type="button"
                      data-testid={`local-content-type-${value}`}
                      onClick={() => {
                        setCreateType(value);
                        setDraft(emptyDraft());
                        clearFiles();
                      }}
                  className="group rounded-lg border border-brand-border bg-brand-bg p-5 text-left transition-colors hover:border-gray-400 hover:bg-black/[0.02] focus:outline-none focus:ring-2 focus:ring-brand-accent/35"
                    >
                      <span className={`flex h-12 w-12 items-center justify-center rounded-2xl ${iconClassName}`}>
                        <Icon className="h-6 w-6" />
                      </span>
                      <strong className="mt-4 block text-base text-gray-900">{label}</strong>
                      <span className="mt-1 block text-xs leading-5 text-gray-500">{description}</span>
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              <>
                <div data-testid="local-content-editor-form" className="min-h-0 flex-1 overflow-y-auto p-5">
                  <div className="mb-5 flex items-center justify-between gap-3 rounded-xl border border-brand-border bg-brand-bg px-4 py-3">
                    <span className="text-sm font-semibold text-gray-900">正在创建：{noun}</span>
                    {selectedID === null ? (
                      <button
                        type="button"
                        onClick={() => {
                          setCreateType(null);
                          setDraft(emptyDraft());
                          clearFiles();
                        }}
                        className="text-xs font-semibold text-brand-accent hover:underline"
                      >
                        重新选择
                      </button>
                    ) : null}
                  </div>

                <label className="block text-xs font-semibold text-gray-700">
                  标题
                  <input
                    data-testid="local-content-title"
                    value={draft.title}
                    onChange={(event) => setDraft((current) => ({ ...current, title: event.target.value }))}
                    placeholder={`填写${noun}标题`}
                    className="mt-1.5 w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2.5 text-sm text-gray-900"
                  />
                </label>

              <label className="mt-4 block text-xs font-semibold text-gray-700">
                {selectedType === 'article' ? '文章摘要（可选）' : '描述'}
                <textarea
                  data-testid="local-content-caption"
                  value={draft.caption}
                  onChange={(event) => setDraft((current) => ({ ...current, caption: event.target.value }))}
                  placeholder={selectedType === 'article'
                    ? '填写文章摘要，详情页会完整展示'
                    : '先写完整描述，可在描述中加入 #标签# 或 #标签'}
                  rows={selectedType === 'article' ? 3 : 6}
                  className="mt-1.5 w-full resize-y rounded-lg border border-brand-border bg-brand-bg px-3 py-2.5 text-sm leading-6 text-gray-900"
                />
              </label>

              <div className="mt-4 rounded-xl border border-brand-border bg-brand-bg p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-xs font-semibold text-gray-700">从素材库选择{selectedType === 'video' ? '视频' : '图片'}</span>
                  <span className="text-[11px] text-gray-500">按添加顺序保存正文素材</span>
                </div>
                <div className="mt-2 flex gap-2">
                  <select
                    data-testid="local-content-library-media-select"
                    value={libraryPickerID}
                    onChange={(event) => setLibraryPickerID(Number(event.target.value))}
                    className="min-w-0 flex-1 rounded-lg border border-brand-border bg-white px-3 py-2 text-sm text-gray-900"
                  >
                    <option value={0}>选择已有素材</option>
                    {compatibleLibraryMedia
                      .filter((item) => !pendingLibraryIDs.includes(item.id))
                      .map((item) => (
                        <option key={item.id} value={item.id}>
                          {item.file_path.split(/[\\/]/).pop()} · {item.width && item.height ? `${item.width}×${item.height}` : item.mime}
                        </option>
                      ))}
                  </select>
                  <button
                    type="button"
                    data-testid="local-content-library-media-add"
                    disabled={!libraryPickerID}
                    onClick={addSelectedLibraryMedia}
                    className="ui-btn disabled:opacity-40"
                  >
                    添加
                  </button>
                </div>
                {pendingLibraryItems.length ? (
                  <ol className="mt-3 space-y-1 text-xs text-gray-600">
                    {pendingLibraryItems.map((item, index) => (
                      <li key={item.id} className="flex items-center justify-between gap-2 rounded-lg bg-white px-3 py-2">
                        <span className="min-w-0 truncate">{index + 1}. {item.file_path.split(/[\\/]/).pop()}</span>
                        <button
                          type="button"
                          onClick={() => setPendingLibraryIDs((current) => current.filter((id) => id !== item.id))}
                          className="shrink-0 font-semibold text-red-600"
                          aria-label={`移除素材 ${index + 1}`}
                        >
                          移除
                        </button>
                      </li>
                    ))}
                  </ol>
                ) : null}

                {selectedType === 'video' ? (
                  <label className="mt-3 block border-t border-brand-border pt-3 text-xs font-semibold text-gray-700">
                    单独选择视频封面（可选）
                    <select
                      data-testid="local-content-library-cover-select"
                      value={selectedCoverLibraryID}
                      onChange={(event) => setSelectedCoverLibraryID(Number(event.target.value))}
                      className="mt-1.5 w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-sm font-normal text-gray-900"
                    >
                      <option value={0}>不指定，发布前再确认</option>
                      {compatibleCoverMedia.map((item) => (
                        <option key={item.id} value={item.id}>
                          {item.file_path.split(/[\\/]/).pop()} · {item.width && item.height ? `${item.width}×${item.height}` : item.mime}
                        </option>
                      ))}
                    </select>
                  </label>
                ) : null}
              </div>

              <div
                data-testid="local-content-upload-dropzone"
                role="button"
                tabIndex={0}
                onDragOver={(event) => event.preventDefault()}
                onDrop={(event) => {
                  event.preventDefault();
                  addUploadFiles(Array.from(event.dataTransfer.files));
                }}
                onClick={() => selectedType === 'video'
                  ? videoInputRef.current?.click()
                  : imageInputRef.current?.click()}
                onKeyDown={(event) => {
                  if (event.key !== 'Enter' && event.key !== ' ') return;
                  event.preventDefault();
                  if (selectedType === 'video') videoInputRef.current?.click();
                  else imageInputRef.current?.click();
                }}
                className="mt-4 cursor-pointer rounded-xl border-2 border-dashed border-brand-border bg-brand-bg p-6 text-center transition hover:border-brand-accent hover:bg-brand-accent/5"
              >
                <UploadCloud className="mx-auto h-7 w-7 text-brand-accent" />
                <p className="mt-2 text-sm font-semibold text-gray-800">
                  {selectedType === 'video'
                    ? '上传视频，也可在上方单独指定封面'
                    : selectedType === 'article'
                      ? '上传一张文章封面'
                      : '上传图文图片，第一张作为封面'}
                </p>
                <p className="mt-1 text-xs text-gray-500">支持点击选择、拖拽上传和剪切板粘贴</p>
                <input
                  ref={videoInputRef}
                  data-testid="local-content-video-file"
                  type="file"
                  accept="video/mp4,video/quicktime,video/webm,video/x-matroska"
                  onChange={(event) => {
                    const file = event.target.files?.[0] ?? null;
                    setVideoFile(file);
                    if (file) {
                      setPendingLibraryIDs([]);
                      setLibraryPickerID(0);
                    }
                  }}
                  className="hidden"
                />
                <input ref={imageInputRef} data-testid="local-content-image-files" type="file" multiple={selectedType === 'graph_text'} accept="image/png,image/jpeg,image/gif,image/webp" onChange={(event) => addUploadFiles(Array.from(event.target.files ?? []))} className="hidden" />
              </div>

              {pendingVideoURL ? (
                <div className="mt-3 aspect-square max-w-xs overflow-hidden rounded-xl border border-brand-border bg-black">
                  <video src={pendingVideoURL} muted controls preload="metadata" className="h-full w-full object-cover" />
                </div>
              ) : null}
              {pendingImageURLs.length ? (
                <div className="mt-3 grid grid-cols-3 gap-2 sm:grid-cols-5">
                  {pendingImageURLs.map(({ file, url }) => (
                    <div key={url} className="relative aspect-square overflow-hidden rounded-lg border border-brand-border">
                      <img src={url} alt={file.name} className="h-full w-full object-cover" />
                      <button type="button" onClick={(event) => {
                        event.stopPropagation();
                        setImageFiles((current) => current.filter((candidate) => candidate !== file));
                      }} className="absolute right-1 top-1 rounded-full bg-black/65 px-1.5 py-0.5 text-xs text-white" aria-label={`移除 ${file.name}`}>×</button>
                    </div>
                  ))}
                </div>
              ) : null}
              {selectedType === 'video' && hasExistingVideo && !videoFile ? <p className="mt-2 text-xs text-gray-500">已有关联视频；不上传新文件会保留原素材。</p> : null}
              {selectedType !== 'video' && hasExistingImages && !imageFiles.length ? <p className="mt-2 text-xs text-gray-500">已有关联封面；不上传新图片会保留原素材。</p> : null}

              {selectedType === 'article' ? (
                <div className="mt-5 grid min-h-[28rem] gap-3 lg:grid-cols-2">
                  <label className="flex min-h-0 flex-col text-xs font-semibold text-gray-700">
                    Markdown 正文
                    <textarea
                      data-testid="local-content-body"
                      value={draft.body}
                      onChange={(event) => setDraft((current) => ({ ...current, body: event.target.value }))}
                      placeholder={'# 标题\n\n支持 Markdown、表格、任务列表和代码块…\n\n#标签1#  #标签2#'}
                      className="mt-1.5 min-h-[26rem] flex-1 resize-y rounded-lg border border-brand-border bg-brand-bg px-4 py-3 font-mono text-sm leading-6 text-gray-900"
                    />
                  </label>
                  <div className="flex min-h-0 flex-col">
                    <span className="text-xs font-semibold text-gray-700">实时预览</span>
                    <div data-testid="markdown-preview" className="mt-1.5 min-h-[26rem] flex-1 overflow-auto rounded-lg border border-brand-border bg-white px-5 py-4 text-sm leading-7 text-gray-800">
                      <MarkdownBody body={draft.body} emptyText="Markdown 预览会显示在这里。" />
                    </div>
                  </div>
                </div>
              ) : null}

              <div data-testid="local-content-recognized-tags" className="mt-5 rounded-xl border border-brand-border bg-brand-bg p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-xs font-semibold text-gray-700">
                    已识别 {recognizedTags.length} 个标签
                  </span>
                  <span className="text-[11px] text-gray-500">正文和描述会原样保留</span>
                </div>
                <div className="mt-3 flex min-h-7 flex-wrap gap-2">
                  {recognizedTags.length ? recognizedTags.map((tag) => (
                    <span key={tag} className="rounded-full bg-brand-accent/10 px-2.5 py-1 text-[11px] font-semibold text-brand-accent">
                      #{tag}
                    </span>
                  )) : <span className="text-xs text-gray-400">在{selectedType === 'article' ? '正文' : '描述'}中输入 #标签，识别结果会显示在这里。</span>}
                </div>
                <label className="mt-3 block border-t border-brand-border pt-3 text-[11px] font-semibold text-gray-600">
                  补充标签（可选）
                  <input
                    data-testid="local-content-tags"
                    value={draft.tags}
                    onChange={(event) => setDraft((current) => ({ ...current, tags: event.target.value }))}
                    placeholder="#标签1#  #标签2#，也支持空格、逗号或换行"
                    className="mt-1.5 w-full rounded-lg border border-brand-border bg-white px-3 py-2.5 text-sm font-normal text-gray-900"
                  />
                </label>
              </div>

              <div data-testid="local-content-assist" className="mt-5 rounded-xl border border-brand-border bg-brand-bg p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-xs font-semibold text-gray-700">AI 辅助</span>
                  <span className="text-[11px] text-gray-500">结果只回填到编辑器，不会自动保存</span>
                </div>
                <div className="mt-3 flex flex-wrap gap-2">
                  {assistActions.map((item) => (
                    <button
                      key={item.action}
                      type="button"
                      data-testid={`local-content-assist-${item.action}`}
                      onClick={() => void runAssist(item.action)}
                      disabled={!selected || assistBusy !== ''}
                      title={item.hint}
                      className="ui-btn text-xs disabled:opacity-40"
                    >
                      {assistBusy === item.action ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                      {item.label}
                    </button>
                  ))}
                </div>
                {assistResult ? (
                  <div className="mt-3 rounded-lg border border-brand-border bg-white p-3">
                    <p className="text-[11px] font-semibold text-gray-600">{assistResult.label}结果</p>
                    <pre className="mt-1.5 max-h-56 overflow-auto whitespace-pre-wrap break-words font-sans text-xs leading-relaxed text-gray-800">{assistResult.text}</pre>
                    <div className="mt-2 flex gap-2">
                      <button type="button" data-testid="local-content-assist-apply" onClick={applyAssist} className="ui-btn ui-btn-primary text-xs">填入编辑器</button>
                      <button type="button" onClick={() => setAssistResult(null)} className="ui-btn text-xs">丢弃</button>
                    </div>
                  </div>
                ) : null}
              </div>

              <div className="mt-5 rounded-xl border border-brand-border bg-brand-bg p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="flex items-center gap-1.5 text-xs font-semibold text-gray-700">
                    <CalendarClock className="h-4 w-4 text-brand-accent" />
                    内容排期
                    <span className="font-normal text-gray-500">点击发布时同步到支持平台的原生定时</span>
                  </span>
                  <div className="flex flex-wrap gap-1.5">
                    <button type="button" onClick={() => setDraft((current) => ({ ...current, plannedAt: '' }))} className="ui-btn text-xs">不排期</button>
                    <button type="button" onClick={() => setDraft((current) => ({ ...current, plannedAt: quickPlannedAt(0) }))} className="ui-btn text-xs">今天 20:00</button>
                    <button type="button" onClick={() => setDraft((current) => ({ ...current, plannedAt: quickPlannedAt(1) }))} className="ui-btn text-xs">明天 20:00</button>
                  </div>
                </div>
                <input data-testid="local-content-planned-at" aria-label="自定义排期时间" type="datetime-local" value={draft.plannedAt} onChange={(event) => setDraft((current) => ({ ...current, plannedAt: event.target.value }))} className="mt-2 w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-sm text-gray-900 sm:w-auto" />
              </div>
                </div>

                <footer className="flex justify-end gap-2 border-t border-brand-border px-5 py-4">
                  <button type="button" onClick={closeEditor} disabled={saving} className="ui-btn disabled:opacity-40">取消</button>
                  <button
                    type="button"
                    onClick={() => void save()}
                    disabled={saving || editingMediaLoading}
                    className="ui-btn ui-btn-primary disabled:opacity-50"
                    data-testid="local-content-save"
                  >
                    <Save className="h-3.5 w-3.5" />
                    {saving ? '保存中…' : editingMediaLoading ? '读取素材…' : `保存${noun}`}
                  </button>
                </footer>
              </>
            )}
          </article>
        </div>
      ) : null}
    </section>
  );
}
