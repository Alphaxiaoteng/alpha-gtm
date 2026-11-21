import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import useSWR from 'swr';
import {
  ClipboardPaste,
  FolderOpen,
  History,
  Image as ImageIcon,
  ImagePlus,
  LoaderCircle,
  Maximize2,
  Plus,
  Save,
  Settings2,
  Sparkles,
  Trash2,
  Upload,
  Video,
  ZoomIn,
  ZoomOut,
} from 'lucide-react';
import {
  api,
  type AIImageAsset,
  type AIImageCanvasNode,
  type AIImageGeneration,
  type AIImageBatchGenerationRequest,
	type AIImageGenerationRequest,
  type AIImageGenerationMode,
  type AIImageReferenceRole,
  type AIImageWorkspace,
} from '../api';
import { BEFORE_WORKSPACE_CHANGE_EVENT, useUIStore } from '../store';

const blankWorkspace: AIImageWorkspace = {
  prompts: [],
  nodes: [],
  viewport: { x: 0, y: 0, zoom: 1 },
};

type AssetTab = 'images' | 'videos' | 'prompts';

const referenceRoleLabel: Record<Exclude<AIImageReferenceRole, 'first_frame'>, string> = {
  product: '主体 / 设计',
  model: '人物 / 部位 / 姿势',
  style: '风格 / 材质',
};

const pendingGenerationRequestKey = 'alpha_nexus_ai_generation_pending_v1';
const activeProjectStorageKey = 'alpha_nexus_ai_image_active_project_v1';

type PendingGenerationRequest = { request_id: string; intent_hash: string };

async function generationIntentHash(intent: Omit<AIImageGenerationRequest, 'request_id'>, variant = '') {
	const canonical = JSON.stringify({
		kind: intent.kind,
		mode: intent.mode,
		model: intent.model,
		output_size: intent.output_size,
		duration_seconds: intent.duration_seconds ?? 0,
		prompt: intent.prompt,
		references: intent.references.map((reference) => ({ asset_name: reference.asset_name, role: reference.role })),
    variant,
	});
	const digest = await window.crypto.subtle.digest('SHA-256', new TextEncoder().encode(canonical));
	return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

function readPendingGenerationRequest(): PendingGenerationRequest | null {
	try {
		const parsed = JSON.parse(localStorage.getItem(pendingGenerationRequestKey) || 'null') as Partial<PendingGenerationRequest> | null;
		if (!parsed || typeof parsed.request_id !== 'string' || typeof parsed.intent_hash !== 'string') return null;
		if (!/^[A-Za-z0-9._-]{16,128}$/.test(parsed.request_id) || !/^[a-f0-9]{64}$/.test(parsed.intent_hash)) return null;
		return { request_id: parsed.request_id, intent_hash: parsed.intent_hash };
	} catch {
		return null;
	}
}

async function generationRequestForIntent(intent: Omit<AIImageGenerationRequest, 'request_id'>, variant = ''): Promise<AIImageGenerationRequest> {
	const intentHash = await generationIntentHash(intent, variant);
	const pending = readPendingGenerationRequest();
	if (pending?.intent_hash === intentHash) return { ...intent, request_id: pending.request_id };
	const requestID = `gen-${window.crypto.randomUUID()}`;
	try {
		// Only the nonce and one-way intent hash survive a reload; never persist the prompt.
		localStorage.setItem(pendingGenerationRequestKey, JSON.stringify({ request_id: requestID, intent_hash: intentHash }));
	} catch {
		// The server-side unique request ID still protects a single in-memory attempt.
	}
	return { ...intent, request_id: requestID };
}

function clearPendingGenerationRequest(requestID: string) {
	try {
		const pending = readPendingGenerationRequest();
		if (pending?.request_id === requestID) localStorage.removeItem(pendingGenerationRequestKey);
	} catch {
		// Storage cleanup is best-effort and contains no prompt or provider data.
	}
}

function nextID(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function getAssetKind(asset: AIImageAsset): 'image' | 'video' {
  if (asset.kind === 'video') return 'video';
  return (asset.mime_type || asset.mime || '').startsWith('video/') ? 'video' : 'image';
}

function nextReferenceLabel(kind: 'image' | 'video', used: Set<string>) {
  const prefix = kind === 'video' ? '视频' : '图';
  let index = 1;
  while (used.has(`${prefix}${String(index).padStart(2, '0')}`)) index += 1;
  const label = `${prefix}${String(index).padStart(2, '0')}`;
  used.add(label);
  return label;
}

function ensureReferenceLabels(workspace: AIImageWorkspace): { workspace: AIImageWorkspace; changed: boolean } {
  const used = new Set(workspace.nodes.map((node) => node.ref_label).filter((label): label is string => Boolean(label)));
  let changed = false;
  const nodes = workspace.nodes.map((node) => {
    if (node.kind === 'prompt' || node.ref_label) return node;
    changed = true;
    return { ...node, ref_label: nextReferenceLabel(node.kind, used) };
  });
  return { workspace: changed ? { ...workspace, nodes } : workspace, changed };
}

function mentionedImageLabels(text: string) {
  return [...new Set(Array.from(text.matchAll(/@(图\d{2,3})/g), (match) => match[1]))];
}

function AssetPreview({
  asset,
  className = '',
  controls = false,
}: {
  asset: AIImageAsset;
  className?: string;
  controls?: boolean;
}) {
  const { data } = useSWR(['ai-image-asset-blob', asset.name], () => api.getAIImageAssetBlob(asset.name));
  const [objectURL, setObjectURL] = useState('');

  useEffect(() => {
    if (!data) return;
    const url = URL.createObjectURL(data);
    setObjectURL(url);
    return () => URL.revokeObjectURL(url);
  }, [data]);

  return objectURL && getAssetKind(asset) === 'video' ? (
    <video
      src={objectURL}
      aria-label="本地视频资产"
      controls={controls}
      muted={!controls}
      playsInline
      preload="metadata"
      className={className || 'object-cover'}
      onPointerDown={controls ? (event) => event.stopPropagation() : undefined}
    />
  ) : objectURL ? (
    <img src={objectURL} alt="本地图片资产" draggable={false} className={className || 'object-cover'} />
  ) : (
    <div className={`flex items-center justify-center bg-black/5 text-gray-400 ${className}`}>
      {getAssetKind(asset) === 'video' ? <Video className="h-5 w-5" /> : <ImageIcon className="h-5 w-5" />}
    </div>
  );
}

function AssetCard({
  asset,
  selected,
  onSelect,
  onCanvas,
}: {
  asset: AIImageAsset;
  selected: boolean;
  onSelect: () => void;
  onCanvas: () => void;
}) {
  return (
    <article
      className={`overflow-hidden rounded-xl border bg-brand-bg ${
        selected ? 'border-brand-accent ring-1 ring-brand-accent/40' : 'border-brand-border'
      }`}
    >
      <button type="button" onClick={onSelect} className="block w-full text-left">
        <AssetPreview asset={asset} className="aspect-square w-full object-cover" />
        <span className="block truncate px-2 pt-2 text-[10px] font-semibold text-gray-700">
          {asset.width}×{asset.height} · {Math.max(1, Math.round(asset.size_bytes / 1024))} KB
          {getAssetKind(asset) === 'video' && asset.duration_ms > 0 ? ` · ${(asset.duration_ms / 1000).toFixed(1)}s` : ''}
        </span>
        <span className="block truncate px-2 pb-2 pt-0.5 font-mono text-[9px] text-gray-400">
          {asset.sha256.slice(0, 12)}
        </span>
      </button>
      <button
        type="button"
        onClick={onCanvas}
        className="w-full border-t border-brand-border px-2 py-1.5 text-[10px] font-bold text-brand-accent hover:bg-brand-accent/10"
      >
        放到画布
      </button>
    </article>
  );
}

export function AIImageWorkspaceView() {
  const { toast, setActiveTab } = useUIStore();
  const { data: config, error: configError } = useSWR('api/ai-image/config', api.getAIImageConfig);
  const { data: assets = [], mutate: mutateAssets } = useSWR('api/ai-image/assets', api.getAIImageAssets);
  const { data: generationHistory = [], mutate: mutateGenerationHistory } = useSWR(
    'api/ai-image/generations',
    api.getAIImageGenerations,
  );
  const { data: projects = [], mutate: mutateProjects } = useSWR(
    'api/ai-image/projects',
    api.getAIImageProjects,
  );
  const [activeProjectID, setActiveProjectID] = useState(() => {
    const stored = Number(window.localStorage.getItem(activeProjectStorageKey));
    return Number.isSafeInteger(stored) && stored > 0 ? stored : 0;
  });
  const { data: savedProject, mutate: mutateSavedProject } = useSWR(
    activeProjectID > 0 ? `api/ai-image/projects/${activeProjectID}` : null,
    () => api.getAIImageProject(activeProjectID),
  );
  const [workspace, setWorkspace] = useState<AIImageWorkspace>(blankWorkspace);
  const [workspaceReady, setWorkspaceReady] = useState(false);
  const [loadedProjectID, setLoadedProjectID] = useState(0);
  const [projectName, setProjectName] = useState('');
  const [dirty, setDirty] = useState(false);
  const [prompt, setPrompt] = useState('');
  const [generationModel, setGenerationModel] = useState('agnes-image-2.1-flash');
  const [outputSize, setOutputSize] = useState('1:1');
  const [videoDurationSeconds, setVideoDurationSeconds] = useState<5 | 10>(5);
  const [generation, setGeneration] = useState<AIImageGeneration | null>(null);
  const [batchGenerations, setBatchGenerations] = useState<AIImageGeneration[]>([]);
  const [batchCount, setBatchCount] = useState(1);
  const [generating, setGenerating] = useState(false);
  const [selectedAssetName, setSelectedAssetName] = useState('');
  const [selectedNodeIDs, setSelectedNodeIDs] = useState<string[]>([]);
  const [referenceRoles, setReferenceRoles] = useState<Record<string, Exclude<AIImageReferenceRole, 'first_frame'>>>({});
  const [assetsOpen, setAssetsOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [confirmingGeneration, setConfirmingGeneration] = useState(false);
  const [assetTab, setAssetTab] = useState<AssetTab>('images');
  const [importing, setImporting] = useState(false);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const videoInputRef = useRef<HTMLInputElement>(null);
  const generationPollRef = useRef(0);
  const canvasRef = useRef<HTMLDivElement>(null);
  const workbenchRef = useRef<HTMLDivElement>(null);
  const panRef = useRef<{ x: number; y: number; originX: number; originY: number } | null>(null);
  const dragRef = useRef<{ id: string; x: number; y: number; originX: number; originY: number } | null>(null);

  const saveWorkspace = useCallback(async (next = workspace, notify = true): Promise<boolean> => {
    if (activeProjectID <= 0 || loadedProjectID !== activeProjectID) return false;
    try {
      const saved = await api.updateAIImageProject(activeProjectID, {
        name: projectName.trim() || '未命名项目',
        workspace: next,
      });
      setWorkspace(saved.workspace);
      setProjectName(saved.name);
      setWorkspaceReady(true);
      setDirty(false);
      await mutateSavedProject(saved, { revalidate: false });
      void mutateProjects();
      if (notify) toast.show('项目画布已保存到本地', 'success');
      return true;
    } catch (error: any) {
      toast.show(`保存失败：${error.message || '本地项目不可写'}`, 'error');
      return false;
    }
  }, [activeProjectID, loadedProjectID, mutateProjects, mutateSavedProject, projectName, toast, workspace]);

  useEffect(() => {
    if (!projects.length) return;
    const nextID = projects.some((project) => project.id === activeProjectID)
      ? activeProjectID
      : projects[0].id;
    if (nextID === activeProjectID) return;
    setActiveProjectID(nextID);
    window.localStorage.setItem(activeProjectStorageKey, String(nextID));
  }, [activeProjectID, projects]);

  useEffect(() => {
    if (savedProject && savedProject.id === activeProjectID && loadedProjectID !== savedProject.id) {
      const normalized = ensureReferenceLabels(savedProject.workspace);
      setWorkspace(normalized.workspace);
      setProjectName(savedProject.name);
      setDirty(normalized.changed);
      setWorkspaceReady(true);
      setLoadedProjectID(savedProject.id);
      setSelectedNodeIDs([]);
      setReferenceRoles({});
    }
  }, [activeProjectID, loadedProjectID, savedProject]);

  useEffect(() => () => {
    generationPollRef.current += 1;
  }, []);

  useEffect(() => {
    if (!dirty) return;
    const saveBeforeLeaving = () => {
      void saveWorkspace(workspace, false);
    };
    window.addEventListener(BEFORE_WORKSPACE_CHANGE_EVENT, saveBeforeLeaving);
    window.addEventListener('pagehide', saveBeforeLeaving);
    return () => {
      window.removeEventListener(BEFORE_WORKSPACE_CHANGE_EVENT, saveBeforeLeaving);
      window.removeEventListener('pagehide', saveBeforeLeaving);
    };
  }, [dirty, saveWorkspace, workspace]);

  useEffect(() => {
    if (!dirty || !workspaceReady || activeProjectID <= 0 || loadedProjectID !== activeProjectID) return;
    const timer = window.setTimeout(() => {
      void saveWorkspace(workspace, false);
    }, 500);
    return () => window.clearTimeout(timer);
  }, [activeProjectID, dirty, loadedProjectID, projectName, saveWorkspace, workspace, workspaceReady]);

  const assetByName = useMemo(() => new Map(assets.map((asset) => [asset.name, asset])), [assets]);
  const selectedImageNodes = useMemo(
    () => selectedNodeIDs
      .map((id) => workspace.nodes.find((node) => node.id === id))
      .filter((node): node is AIImageCanvasNode => Boolean(node && node.kind === 'image' && node.asset_name)),
    [selectedNodeIDs, workspace.nodes],
  );
  const imageReferenceNodes = useMemo(
    () => workspace.nodes.filter((node) => node.kind === 'image' && node.asset_name && node.ref_label),
    [workspace.nodes],
  );
  const modelOptions = [...new Set([
    config?.image_model || config?.model || 'agnes-image-2.1-flash',
    'agnes-image-2.1-flash',
    'agnes-image-2.0-flash',
    config?.video_model || 'agnes-video-v2.0',
  ])];
  const selectedModel = modelOptions.includes(generationModel) ? generationModel : modelOptions[0];
  const generationKind = selectedModel.startsWith('agnes-video-') ? 'video' : 'image';
  const generationMode: AIImageGenerationMode = generationKind === 'video'
    ? selectedImageNodes.length ? 'image_to_video' : 'text_to_video'
    : selectedImageNodes.length ? 'image_to_image' : 'text_to_image';
  const visibleAssets = useMemo(
    () => assets.filter((asset) => getAssetKind(asset) === (assetTab === 'videos' ? 'video' : 'image')),
    [assetTab, assets],
  );
  const promptBytes = useMemo(() => new TextEncoder().encode(prompt.trim()).length, [prompt]);
  const promptError = !prompt.trim()
    ? '请描述要生成的画面'
    : promptBytes > 8000
      ? '提示词超过 8000 字节'
      : '';
  const referenceError = generationKind === 'image' && selectedImageNodes.length > 8
    ? '图片模型最多使用 8 张参考图'
    : generationKind === 'video' && selectedImageNodes.length > 1
      ? '视频模型最多使用 1 张参考图'
      : '';

  const updateWorkspace = (updater: (current: AIImageWorkspace) => AIImageWorkspace) => {
    setWorkspace((current) => updater(current));
    setDirty(true);
  };

  const visibleCanvasPoint = (width: number, height: number) => {
    const rect = canvasRef.current?.getBoundingClientRect();
    const zoom = workspace.viewport.zoom || 1;
    const stagger = (workspace.nodes.length % 8) * 22;
    return {
      x: ((rect?.width ?? 900) / 2 - workspace.viewport.x) / zoom - width / 2 + stagger,
      y: ((rect?.height ?? 620) / 2 - workspace.viewport.y) / zoom - height / 2 + stagger,
    };
  };

  const validatedPrompt = () => {
    const text = prompt.trim();
    if (!text) {
      toast.show('先填写提示词，再点击产出', 'error');
      return null;
    }
    if (promptBytes > 8000) {
      toast.show('提示词超过 8000 字节，请精简后再产出', 'error');
      return null;
    }
    return text;
  };

  const producePromptCard = (validatedText?: string) => {
    const text = validatedText || validatedPrompt();
    if (!text) return;
    if (workspace.nodes.length >= 300) {
      toast.show('画布已达到 300 个节点上限', 'error');
      return;
    }
    const position = visibleCanvasPoint(320, 180);
    updateWorkspace((current) => ({
      ...current,
      nodes: [
        ...current.nodes,
        {
          id: nextID('prompt'),
          kind: 'prompt',
          text,
          x: position.x,
          y: position.y,
          width: 320,
          height: 180,
        },
      ],
    }));
    toast.show('已产出待执行提示词卡；未调用模型', 'success');
  };

  const addMediaNodes = (mediaAssets: AIImageAsset[], notify = true, select = true) => {
    const room = Math.max(0, 300 - workspace.nodes.length);
    const usedLabels = new Set(workspace.nodes.map((node) => node.ref_label).filter((label): label is string => Boolean(label)));
    const accepted = mediaAssets.slice(0, room).map((asset) => {
      const kind = getAssetKind(asset);
      return {
        asset,
        id: nextID(kind),
        kind,
        refLabel: nextReferenceLabel(kind, usedLabels),
        dimensions: kind === 'video' ? { width: 320, height: 220 } : { width: 260, height: 260 },
      } as const;
    });
    if (!accepted.length) {
      toast.show('画布已达到 300 个节点上限', 'error');
      return;
    }

    updateWorkspace((current) => {
      const rect = canvasRef.current?.getBoundingClientRect();
      const zoom = current.viewport.zoom || 1;
      const nodes = accepted.map(({ asset, id, kind, refLabel, dimensions }, index) => {
        const stagger = ((current.nodes.length + index) % 8) * 22;
        return {
          id,
          kind,
          asset_name: asset.name,
          ref_label: refLabel,
          x: ((rect?.width ?? 900) / 2 - current.viewport.x) / zoom - dimensions.width / 2 + stagger,
          y: ((rect?.height ?? 620) / 2 - current.viewport.y) / zoom - dimensions.height / 2 + stagger,
          ...dimensions,
        } satisfies AIImageCanvasNode;
      });
      return { ...current, nodes: [...current.nodes, ...nodes] };
    });

    if (select) {
      const ids = accepted.map((item) => item.id);
      setSelectedNodeIDs((current) => [...new Set([...current, ...ids])]);
      setReferenceRoles((current) => {
        const next = { ...current };
        let imageIndex = 0;
        accepted.forEach((item) => {
          if (item.kind === 'image') {
            next[item.id] = selectedImageNodes.length === 0 && imageIndex === 0 ? 'product' : 'style';
            imageIndex += 1;
          }
        });
        return next;
      });
    }
    if (notify) toast.show(`${accepted.length} 个本地资产已放到画布`, 'success');
  };

  const uploadAssets = async (files: File[]) => {
    if (!files.length) return;
    if (files.length > 20) {
      toast.show('单次最多添加 20 个媒体文件', 'error');
      return;
    }
    setImporting(true);
    const imported: AIImageAsset[] = [];
    let failed = 0;
    try {
      const settled = await Promise.allSettled(files.map((file) => api.uploadAIImageAsset(file)));
      settled.forEach((result) => {
        if (result.status === 'fulfilled') imported.push(result.value);
        else failed += 1;
      });
      if (!imported.length) throw new Error('媒体文件不可用');
      await mutateAssets();
      addMediaNodes(imported, false, true);
      const last = imported[imported.length - 1];
      setSelectedAssetName(last.name);
      setAssetTab(getAssetKind(last) === 'video' ? 'videos' : 'images');
      toast.show(
        `${imported.length} 个媒体已保存并放到画布${failed ? `，${failed} 个失败` : ''}`,
        failed ? 'info' : 'success',
      );
    } catch (error: any) {
      toast.show(`导入失败：${error.message || '媒体文件不可用'}`, 'error');
    } finally {
      setImporting(false);
    }
  };

  useEffect(() => {
    const onPaste = (event: ClipboardEvent) => {
      if (!event.clipboardData) return;
      const target = event.target as Node | null;
      const active = document.activeElement;
      const inCanvas = Boolean((target && canvasRef.current?.contains(target)) || (active && canvasRef.current?.contains(active)));
      const inWorkbench = Boolean((target && workbenchRef.current?.contains(target)) || (active && workbenchRef.current?.contains(active)));
      if (!inCanvas && !inWorkbench) return;
      const files = Array.from(event.clipboardData.files).filter(
        (file) => file.type.startsWith('image/') || file.type.startsWith('video/'),
      );
      if (!files.length) return;
      event.preventDefault();
      void uploadAssets(files);
    };
    document.addEventListener('paste', onPaste);
    return () => document.removeEventListener('paste', onPaste);
  });

  const addMediaNode = (asset: AIImageAsset, notify = true) => {
    addMediaNodes([asset], notify, false);
  };

  const produce = async () => {
    const text = validatedPrompt();
    if (!text || generating) return;
    const mentionedLabels = mentionedImageLabels(text);
    const mentionedNodes = mentionedLabels.map((label) => imageReferenceNodes.find((node) => node.ref_label === label));
    const unresolvedLabels = mentionedLabels.filter((_, index) => !mentionedNodes[index]);
    if (unresolvedLabels.length) {
      toast.show(`找不到 ${unresolvedLabels.map((label) => `@${label}`).join('、')}，请重新选择画布图片`, 'error');
      return;
    }
    const effectiveImageNodes = (mentionedLabels.length
      ? mentionedNodes.filter((node): node is AIImageCanvasNode => Boolean(node))
      : selectedImageNodes
    );
    const references = effectiveImageNodes.map((node, index) => ({
      asset_name: node.asset_name!,
      role: generationKind === 'video'
        ? 'first_frame' as const
        : referenceRoles[node.id] || (index === 0 ? 'product' as const : 'style' as const),
    }));
    if (generationKind === 'image' && references.length > 8) {
      toast.show('图片模型最多使用 8 张参考图片', 'error');
      return;
    }
    if (generationKind === 'video' && references.length > 1) {
      toast.show('视频模型最多使用 1 张参考图片', 'error');
      return;
    }
    if (!config?.credential_configured) {
      producePromptCard(text);
      return;
    }
    if (workspace.nodes.length + batchCount > 300) {
      toast.show('画布已达到 300 个节点上限', 'error');
      return;
    }

    const pollToken = generationPollRef.current + 1;
    generationPollRef.current = pollToken;
    setGenerating(true);
	let activeRequestID = '';
    try {
      const effectiveMode: AIImageGenerationMode = generationKind === 'video'
        ? references.length ? 'image_to_video' : 'text_to_video'
        : references.length ? 'image_to_image' : 'text_to_image';
      const intent = {
        kind: generationKind,
        mode: effectiveMode,
        model: selectedModel,
        output_size: outputSize,
        duration_seconds: generationKind === 'video' ? videoDurationSeconds : undefined,
        prompt: text,
        references,
      } satisfies Omit<AIImageGenerationRequest, 'request_id'>;
		const request = await generationRequestForIntent(intent, batchCount > 1 ? `batch:${batchCount}` : '');
		activeRequestID = request.request_id;
      let currentItems: AIImageGeneration[];
      if (batchCount > 1) {
        const batchRequest: AIImageBatchGenerationRequest = { ...request, count: batchCount };
        const batch = await api.createAIImageGenerationBatch(batchRequest);
        currentItems = batch.items;
      } else {
        currentItems = [await api.createAIImageGeneration(request)];
      }
      setBatchGenerations(currentItems);
      setGeneration(currentItems[0] || null);

      for (
        let attempt = 0;
        attempt < 900 && currentItems.some((item) => item.status !== 'succeeded' && item.status !== 'failed');
        attempt += 1
      ) {
        await new Promise((resolve) => window.setTimeout(resolve, 1200));
        if (generationPollRef.current !== pollToken) return;
        currentItems = await Promise.all(currentItems.map((item) =>
          item.status === 'succeeded' || item.status === 'failed' ? item : api.getAIImageGeneration(item.id),
        ));
        setBatchGenerations(currentItems);
        setGeneration(currentItems.find((item) => item.status !== 'succeeded') || currentItems[0] || null);
      }

      const unfinished = currentItems.filter((item) => item.status !== 'succeeded' && item.status !== 'failed');
      if (unfinished.length) {
        toast.show(`${unfinished.length} 个任务仍在后台进行，可稍后回到画布查看资产`, 'info');
        return;
      }
      const unknown = currentItems.find((item) => item.error_code === 'GENERATION_PROVIDER_STATE_UNKNOWN');
      if (!unknown) clearPendingGenerationRequest(activeRequestID);
      await mutateGenerationHistory();

      const succeeded = currentItems.filter((item) => item.status === 'succeeded' && item.output_asset);
      const failed = currentItems.filter((item) => item.status === 'failed');
      if (!succeeded.length) {
        if (unknown) {
          toast.show('服务商提交状态不确定，不能安全自动重试；请先到服务商侧核对', 'error');
        } else {
          toast.show(`生成失败：${failed[0]?.error_code || 'GENERATION_FAILED'}`, 'error');
        }
        return;
      }

      await mutateAssets();
      const outputs = succeeded.map((item) => item.output_asset!);
      addMediaNodes(outputs, false, false);
      const lastOutput = outputs[outputs.length - 1];
      setSelectedAssetName(lastOutput.name);
      setAssetTab(getAssetKind(lastOutput) === 'video' ? 'videos' : 'images');
      if (unknown) {
        toast.show(`${outputs.length} 个产物已保存；另有任务状态不确定，请先到服务商侧核对`, 'info');
      } else {
        toast.show(`${outputs.length} 个${generationKind === 'video' ? '视频' : '图片'}已生成并保存到本地资产${failed.length ? `，${failed.length} 个失败` : ''}`, failed.length ? 'info' : 'success');
      }
    } catch (error: any) {
		if (String(error?.message || '').startsWith('GENERATION_REQUEST_CONFLICT')) {
			clearPendingGenerationRequest(activeRequestID);
		}
      toast.show(`生成请求失败：${error.message || 'GENERATION_REQUEST_FAILED'}`, 'error');
    } finally {
      if (generationPollRef.current === pollToken) setGenerating(false);
    }
  };

  const requestProduce = () => {
    if (promptError || referenceError || generating) {
      if (promptError) toast.show(promptError, 'error');
      else if (referenceError) toast.show(referenceError, 'error');
      return;
    }
    if (config?.credential_configured) {
      setConfirmingGeneration(true);
      return;
    }
    void produce();
  };

  const removeNode = (id: string) => {
    updateWorkspace((current) => ({ ...current, nodes: current.nodes.filter((node) => node.id !== id) }));
    setSelectedNodeIDs((current) => current.filter((candidate) => candidate !== id));
    setReferenceRoles((current) => {
      const next = { ...current };
      delete next[id];
      return next;
    });
  };

  const toggleNodeSelection = (node: AIImageCanvasNode) => {
    if (node.kind === 'prompt') return;
    setSelectedNodeIDs((current) => {
      if (current.includes(node.id)) return current.filter((id) => id !== node.id);
      if (generationKind === 'video' && node.kind === 'image') {
        const imageIDs = new Set(workspace.nodes.filter((item) => item.kind === 'image').map((item) => item.id));
        return [...current.filter((id) => !imageIDs.has(id)), node.id];
      }
      return [...current, node.id];
    });
    if (node.kind === 'image') {
      setReferenceRoles((current) => current[node.id] ? current : { ...current, [node.id]: selectedImageNodes.length ? 'style' : 'product' });
    }
  };

  const changeGenerationModel = (next: string) => {
    setGenerationModel(next);
    setConfirmingGeneration(false);
    if (!next.startsWith('agnes-video-')) return;
    if (outputSize === '21:9') setOutputSize('16:9');
    if (selectedImageNodes.length <= 1) return;
    const keepImageID = selectedImageNodes[0].id;
    const imageIDs = new Set(selectedImageNodes.map((node) => node.id));
    setSelectedNodeIDs((current) => current.filter((id) => !imageIDs.has(id) || id === keepImageID));
  };

  const selectProject = async (nextID: number) => {
    if (nextID <= 0 || nextID === activeProjectID) return;
    if (dirty && !(await saveWorkspace())) return;
    setWorkspaceReady(false);
    setLoadedProjectID(0);
    setActiveProjectID(nextID);
    window.localStorage.setItem(activeProjectStorageKey, String(nextID));
    setPrompt('');
    setSelectedNodeIDs([]);
    setReferenceRoles({});
    setConfirmingGeneration(false);
  };

  const createProject = async () => {
    if (dirty && !(await saveWorkspace())) return;
    try {
      const created = await api.createAIImageProject(`未命名项目 ${projects.length + 1}`);
      await mutateProjects();
      setActiveProjectID(created.id);
      window.localStorage.setItem(activeProjectStorageKey, String(created.id));
      setLoadedProjectID(created.id);
      setProjectName(created.name);
      setWorkspace(created.workspace);
      setWorkspaceReady(true);
      setDirty(false);
      setPrompt('');
      setSelectedNodeIDs([]);
      setReferenceRoles({});
      toast.show('新项目已创建', 'success');
    } catch (error: any) {
      toast.show(`新建失败：${error.message || '本地项目不可写'}`, 'error');
    }
  };

  const moveReference = (nodeID: string, direction: -1 | 1) => {
    setSelectedNodeIDs((current) => {
      const index = current.indexOf(nodeID);
      const nextIndex = index + direction;
      if (index < 0 || nextIndex < 0 || nextIndex >= current.length) return current;
      const next = [...current];
      [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
      return next;
    });
  };

  const savePromptAsset = async () => {
    const text = prompt.trim();
    if (!text) {
      toast.show('先填写要保存的提示词', 'error');
      return;
    }
    if (promptBytes > 4000) {
      toast.show('常用提示词超过 4000 字节，请精简后再保存', 'error');
      return;
    }
    if (workspace.prompts.length >= 100) {
      toast.show('提示词资产已达到 100 条上限', 'error');
      return;
    }
    const title = text.length > 20 ? `${text.slice(0, 20)}…` : text;
    const next: AIImageWorkspace = {
      ...workspace,
      prompts: [...workspace.prompts, { id: nextID('preset'), title, text }],
    };
    await saveWorkspace(next);
  };

  const setViewport = (next: AIImageWorkspace['viewport']) => {
    updateWorkspace((current) => ({ ...current, viewport: next }));
  };

  const focusCanvasNode = (node: AIImageCanvasNode) => {
    const rect = canvasRef.current?.getBoundingClientRect();
    const zoom = workspace.viewport.zoom || 1;
    setViewport({
      zoom,
      x: (rect?.width || 900) / 2 - (node.x + node.width / 2) * zoom,
      y: (rect?.height || 620) / 2 - (node.y + node.height / 2) * zoom,
    });
  };

  const zoomBy = (delta: number) => {
    const zoom = Math.min(4, Math.max(0.1, Number((workspace.viewport.zoom + delta).toFixed(2))));
    setViewport({ ...workspace.viewport, zoom });
  };

  const onCanvasPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if ((event.target as HTMLElement).closest('[data-canvas-node], [data-canvas-control]')) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    panRef.current = {
      x: event.clientX,
      y: event.clientY,
      originX: workspace.viewport.x,
      originY: workspace.viewport.y,
    };
  };

  const onCanvasPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!panRef.current) return;
    const next = {
      ...workspace.viewport,
      x: panRef.current.originX + event.clientX - panRef.current.x,
      y: panRef.current.originY + event.clientY - panRef.current.y,
    };
    setWorkspace((current) => ({ ...current, viewport: next }));
    setDirty(true);
  };

  const onNodePointerDown = (event: React.PointerEvent<HTMLElement>, node: AIImageCanvasNode) => {
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = {
      id: node.id,
      x: event.clientX,
      y: event.clientY,
      originX: node.x,
      originY: node.y,
    };
  };

  const onNodePointerMove = (event: React.PointerEvent<HTMLElement>) => {
    const drag = dragRef.current;
    if (!drag) return;
    const zoom = workspace.viewport.zoom || 1;
    setWorkspace((current) => ({
      ...current,
      nodes: current.nodes.map((node) =>
        node.id === drag.id
          ? {
              ...node,
              x: drag.originX + (event.clientX - drag.x) / zoom,
              y: drag.originY + (event.clientY - drag.y) / zoom,
            }
          : node,
      ),
    }));
    setDirty(true);
  };

  const openSettings = () => {
    setActiveTab('settings');
    window.setTimeout(
      () => document.querySelector('[data-testid="settings-ai-image"]')?.scrollIntoView({ behavior: 'smooth' }),
      0,
    );
  };

  return (
    <section className="h-full min-h-0 w-full" data-testid="ai-image-workspace">
      <div className="flex h-full min-h-0 w-full flex-col overflow-hidden">
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border py-3 pl-4 pr-52">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <Sparkles className="h-4 w-4 shrink-0 text-brand-accent" />
              <h3 className="text-sm font-bold text-gray-900">AI 生图 · 无限画布</h3>
              <span className="rounded-full border border-brand-warning/30/30 bg-brand-warning/20/10 px-2 py-0.5 text-[9px] font-semibold text-brand-warning">
                本地创作
              </span>
            </div>
            <div className="mt-1 flex min-w-0 items-center gap-1.5">
              <select
                value={activeProjectID || ''}
                onChange={(event) => void selectProject(Number(event.target.value))}
                data-testid="ai-image-project-select"
                aria-label="画布项目"
                className="max-w-40 rounded-md border border-brand-border bg-brand-surface px-2 py-1 text-[10px] font-semibold text-gray-700 outline-none"
              >
                {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
              </select>
              <input
                value={projectName}
                onChange={(event) => {
                  setProjectName(event.target.value.slice(0, 100));
                  setDirty(true);
                }}
                data-testid="ai-image-project-name"
                aria-label="项目名称"
                className="min-w-24 max-w-44 rounded-md border border-brand-border bg-transparent px-2 py-1 text-[10px] text-gray-600 outline-none focus:border-brand-accent"
              />
              <button type="button" onClick={() => void createProject()} data-testid="ai-image-new-project" className="ui-btn h-7 shrink-0 px-2 text-[10px]">
                <Plus className="h-3 w-3" />新项目
              </button>
            </div>
            <p className="mt-0.5 truncate text-[10px] text-gray-500">
              {generating ? '生成任务进行中，离开页面不会伪造完成状态。' : dirty ? '正在保存项目画布…' : '项目画布已同步到本地。'}
            </p>
          </div>

          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => {
                setHistoryOpen((current) => !current);
                setAssetsOpen(false);
              }}
              className={`ui-btn ${historyOpen ? 'border-brand-accent text-brand-accent' : ''}`}
              data-testid="ai-image-history-toggle"
              aria-expanded={historyOpen}
            >
              <History className="h-3.5 w-3.5" />历史 {generationHistory.length}
            </button>
            <button
              type="button"
              onClick={() => {
                setAssetsOpen((current) => !current);
                setHistoryOpen(false);
              }}
              className={`ui-btn ${assetsOpen ? 'border-brand-accent text-brand-accent' : ''}`}
              data-testid="ai-image-assets-toggle"
              aria-expanded={assetsOpen}
            >
              <ImageIcon className="h-3.5 w-3.5" />资产 {assets.length + workspace.prompts.length}
            </button>
          </div>
        </header>

        {configError ? (
          <div role="status" className="border-b border-red-500/20 bg-red-500/5 px-4 py-2 text-[10px] text-red-500">
            本地 AI 生图配置暂时无法读取，画布仍可离线编辑。
          </div>
        ) : null}

        <div
          ref={canvasRef}
          data-testid="ai-image-canvas"
          tabIndex={0}
          className="relative min-h-0 flex-1 cursor-grab touch-none overflow-hidden bg-brand-bg active:cursor-grabbing"
          style={{
            backgroundImage: 'radial-gradient(circle, color-mix(in srgb, var(--brand-fg) 14%, transparent) 1px, transparent 1px)',
            backgroundPosition: `${workspace.viewport.x}px ${workspace.viewport.y}px`,
            backgroundSize: `${24 * workspace.viewport.zoom}px ${24 * workspace.viewport.zoom}px`,
          }}
          onPointerDown={(event) => {
            if (!(event.target as HTMLElement).closest('[data-canvas-control]')) event.currentTarget.focus({ preventScroll: true });
            onCanvasPointerDown(event);
          }}
          onPointerMove={onCanvasPointerMove}
          onPointerUp={() => { panRef.current = null; }}
          onPointerCancel={() => { panRef.current = null; }}
          onWheel={(event) => {
            event.preventDefault();
            zoomBy(event.deltaY > 0 ? -0.08 : 0.08);
          }}
          onDragOver={(event) => {
            if (event.dataTransfer.types.includes('Files')) event.preventDefault();
          }}
          onDrop={(event) => {
            event.preventDefault();
            const files = Array.from(event.dataTransfer.files).filter(
              (file) => file.type.startsWith('image/') || file.type.startsWith('video/'),
            );
            void uploadAssets(files);
          }}
        >
          <div
            className="absolute left-0 top-0 h-0 w-0 origin-top-left"
            style={{
              transform: `translate(${workspace.viewport.x}px, ${workspace.viewport.y}px) scale(${workspace.viewport.zoom})`,
            }}
          >
            {workspace.nodes.map((node) => {
              const asset = node.asset_name ? assetByName.get(node.asset_name) : undefined;
              const selected = selectedNodeIDs.includes(node.id);
              return (
                <article
                  key={node.id}
                  data-canvas-node
                  data-canvas-node-kind={node.kind}
                  data-selected={selected ? 'true' : 'false'}
                  className={`group absolute cursor-move overflow-hidden rounded-xl border bg-brand-surface shadow-lg ${
                    selected ? 'border-brand-accent ring-2 ring-brand-accent/40' : 'border-brand-border'
                  }`}
                  style={{ left: node.x, top: node.y, width: node.width, height: node.height }}
                  onPointerDown={(event) => onNodePointerDown(event, node)}
                  onPointerMove={onNodePointerMove}
                  onPointerUp={() => { dragRef.current = null; }}
                  onPointerCancel={() => { dragRef.current = null; }}
                >
                  <div className="flex items-center justify-between gap-2 border-b border-brand-border px-3 py-2 text-[10px] font-bold text-gray-600">
                    <span className="truncate">
                      {node.ref_label ? `@${node.ref_label} · ` : ''}
                      {node.kind === 'video' ? '视频资产' : node.kind === 'image' ? '图片资产' : '待执行提示词'}
                    </span>
                    <div className="flex shrink-0 items-center gap-1">
                      {node.kind !== 'prompt' ? (
                        <button
                          type="button"
                          onPointerDown={(event) => event.stopPropagation()}
                          onClick={() => toggleNodeSelection(node)}
                          aria-pressed={selected}
                          aria-label={selected ? '取消选择媒体' : node.kind === 'image' ? '选择为参考图' : '选择媒体'}
                          className={`rounded px-1.5 py-1 text-[9px] ${selected ? 'bg-brand-accent/15 text-brand-accent' : 'text-gray-400 hover:bg-brand-bg'}`}
                        >
                          {selected ? '已选择' : '选择'}
                        </button>
                      ) : null}
                      <button
                        type="button"
                        onPointerDown={(event) => event.stopPropagation()}
                        onClick={() => removeNode(node.id)}
                        className="rounded p-1 text-gray-400 hover:bg-red-500/10 hover:text-red-500"
                        aria-label="移除画布节点"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>
                  {node.kind === 'image' || node.kind === 'video' ? asset ? (
                    <AssetPreview
                      asset={asset}
                      controls={node.kind === 'video'}
                      className="h-[calc(100%-33px)] w-full bg-black object-contain"
                    />
                  ) : (
                    <div className="flex h-full items-center justify-center px-4 text-center text-[10px] text-gray-400">
                      媒体资产不可用，请重新导入
                    </div>
                  ) : (
                    <p className="max-h-[140px] select-text overflow-auto p-3 text-xs leading-5 text-gray-700">
                      {node.text || '空提示词'}
                    </p>
                  )}
                </article>
              );
            })}
          </div>

          <div
            data-canvas-control
            className="absolute left-4 top-4 z-20 flex items-center gap-1 rounded-xl border border-brand-border bg-brand-surface/95 p-1 shadow-sm backdrop-blur"
          >
            <input
              ref={imageInputRef}
              type="file"
              multiple
              className="sr-only"
              data-testid="ai-media-file-input"
              accept="image/png,image/jpeg,image/gif"
              onChange={(event) => {
                const files = Array.from(event.currentTarget.files || []);
                event.currentTarget.value = '';
                void uploadAssets(files);
              }}
            />
            <input
              ref={videoInputRef}
              type="file"
              multiple
              className="sr-only"
              data-testid="ai-video-file-input"
              accept="video/mp4,video/quicktime,video/webm,video/x-matroska"
              onChange={(event) => {
                const files = Array.from(event.currentTarget.files || []);
                event.currentTarget.value = '';
                void uploadAssets(files);
              }}
            />
            <button
              type="button"
              onClick={() => imageInputRef.current?.click()}
              disabled={importing}
              className="ui-btn border-0 px-2 disabled:opacity-40"
              data-testid="ai-image-add-images"
            >
              <ImagePlus className="h-3.5 w-3.5" />添加图片
            </button>
            <button
              type="button"
              onClick={() => videoInputRef.current?.click()}
              disabled={importing}
              className="ui-btn border-0 px-2 disabled:opacity-40"
              data-testid="ai-image-add-videos"
            >
              <Video className="h-3.5 w-3.5" />添加视频
            </button>
            <span className="flex items-center gap-1 border-l border-brand-border px-2 text-[9px] text-gray-400">
              <ClipboardPaste className="h-3 w-3" />可拖拽或直接粘贴
            </span>
            {importing ? <LoaderCircle className="mx-1 h-3.5 w-3.5 animate-spin text-brand-accent" aria-label="媒体导入中" /> : null}
          </div>

          {!workspace.nodes.length ? (
            <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center pb-20 text-center">
              <ImageIcon className="h-9 w-9 text-gray-300" />
              <p className="mt-3 text-xs font-semibold text-gray-500">输入提示词后点击“产出”</p>
              <p className="mt-1 text-[10px] text-gray-400">拖入或粘贴图片/视频 · 拖拽平移 · 滚轮缩放 · 所有内容保存在本地</p>
            </div>
          ) : null}

          <div data-canvas-control className="absolute bottom-4 left-4 z-20 flex items-center gap-1 rounded-xl border border-brand-border bg-brand-surface/95 p-1 shadow-sm backdrop-blur">
            <button type="button" onClick={() => zoomBy(-0.1)} className="ui-btn border-0 px-2" aria-label="缩小">
              <ZoomOut className="h-3.5 w-3.5" />
            </button>
            <span className="w-12 text-center font-mono text-[10px] text-gray-500">
              {Math.round(workspace.viewport.zoom * 100)}%
            </span>
            <button type="button" onClick={() => zoomBy(0.1)} className="ui-btn border-0 px-2" aria-label="放大">
              <ZoomIn className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={() => setViewport({ x: 0, y: 0, zoom: 1 })}
              className="ui-btn border-0 px-2"
              aria-label="复位画布"
            >
              <Maximize2 className="h-3.5 w-3.5" />
            </button>
          </div>

          {assetsOpen ? (
            <aside
              data-canvas-control
              data-testid="ai-image-assets-panel"
              aria-label="本地资产库"
              className="absolute bottom-4 right-4 top-4 z-30 flex w-[min(360px,calc(100%-32px))] flex-col overflow-hidden rounded-2xl border border-brand-border bg-brand-surface/95 shadow-2xl backdrop-blur"
              onPointerDown={(event) => event.stopPropagation()}
            >
              <div className="flex items-start justify-between gap-2 border-b border-brand-border px-4 py-3">
                <div>
                  <h4 className="text-sm font-bold text-gray-900">资产库</h4>
                  <p className="mt-0.5 text-[10px] text-gray-500">图片、视频和常用提示词都保存在本机</p>
                </div>
                <div className="flex items-center gap-1">
                  {config?.asset_dir && window.electronAPI?.openPathInFinder ? (
                    <button
                      type="button"
                      onClick={() => void window.electronAPI?.openPathInFinder(config.asset_dir)}
                      className="ui-btn px-2"
                      aria-label="打开资产目录"
                    >
                      <FolderOpen className="h-3.5 w-3.5" />
                    </button>
                  ) : null}
                  <button type="button" onClick={() => setAssetsOpen(false)} className="ui-btn px-2 text-[10px]">
                    收起
                  </button>
                </div>
              </div>

              <div className="grid grid-cols-3 border-b border-brand-border p-1.5">
                <button
                  type="button"
                  onClick={() => setAssetTab('images')}
                  className={`rounded-lg px-3 py-2 text-[11px] font-bold ${assetTab === 'images' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-500'}`}
                >
                  图片资产
                </button>
                <button
                  type="button"
                  onClick={() => setAssetTab('videos')}
                  className={`rounded-lg px-3 py-2 text-[11px] font-bold ${assetTab === 'videos' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-500'}`}
                >
                  视频资产
                </button>
                <button
                  type="button"
                  onClick={() => setAssetTab('prompts')}
                  className={`rounded-lg px-3 py-2 text-[11px] font-bold ${assetTab === 'prompts' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-500'}`}
                >
                  提示词资产
                </button>
              </div>

              {assetTab !== 'prompts' ? (
                <div className="min-h-0 flex-1 overflow-y-auto p-3">
                  <button
                    type="button"
                    onClick={() => (assetTab === 'videos' ? videoInputRef : imageInputRef).current?.click()}
                    disabled={importing}
                    className="ui-btn ui-btn-primary w-full justify-center text-[10px] disabled:opacity-40"
                    data-testid="ai-image-import"
                  >
                    {importing ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                    {importing ? '导入中' : `上传${assetTab === 'videos' ? '视频' : '图片'}`}
                  </button>
                  <div className="mt-3 grid grid-cols-2 gap-2">
                    {visibleAssets.map((asset) => (
                      <AssetCard
                        key={asset.name}
                        asset={asset}
                        selected={asset.name === selectedAssetName}
                        onSelect={() => setSelectedAssetName(asset.name)}
                        onCanvas={() => addMediaNode(asset)}
                      />
                    ))}
                  </div>
                  {!visibleAssets.length ? (
                    <div className="mt-3 rounded-xl border border-dashed border-brand-border px-3 py-10 text-center text-[10px] leading-4 text-gray-400">
                      {assetTab === 'videos'
                        ? '暂无视频资产。可上传 MP4、MOV、WebM 或 MKV，原文件不会被移动。'
                        : '暂无图片资产。可上传 PNG、JPEG 或 GIF，原图不会被移动。'}
                    </div>
                  ) : null}
                </div>
              ) : (
                <div className="min-h-0 flex-1 overflow-y-auto p-3">
                  <button
                    type="button"
                    onClick={() => void savePromptAsset()}
                    disabled={!workspaceReady}
                    className="ui-btn ui-btn-primary w-full justify-center text-[10px] disabled:opacity-40"
                  >
                    <Save className="h-3.5 w-3.5" />保存当前提示词
                  </button>
                  <div className="mt-3 space-y-2">
                    {workspace.prompts.map((preset) => (
                      <button
                        key={preset.id}
                        type="button"
                        onClick={() => setPrompt(preset.text)}
                        className="block w-full rounded-xl border border-brand-border bg-brand-bg p-3 text-left hover:border-brand-accent"
                      >
                        <strong className="block truncate text-[11px] text-gray-700">{preset.title}</strong>
                        <span className="mt-1 block line-clamp-3 text-[10px] leading-4 text-gray-500">{preset.text}</span>
                      </button>
                    ))}
                  </div>
                  {!workspace.prompts.length ? (
                    <div className="mt-3 rounded-xl border border-dashed border-brand-border px-3 py-10 text-center text-[10px] leading-4 text-gray-400">
                      暂无提示词资产。先在画布下方输入，再点击上方按钮保存。
                    </div>
                  ) : null}
                </div>
              )}
            </aside>
          ) : null}

          {historyOpen ? (
            <aside
              data-canvas-control
              data-testid="ai-image-history-panel"
              aria-label="生成历史"
              className="absolute bottom-4 right-4 top-4 z-30 flex w-[min(380px,calc(100%-32px))] flex-col overflow-hidden rounded-2xl border border-brand-border bg-brand-surface/95 shadow-2xl backdrop-blur"
              onPointerDown={(event) => event.stopPropagation()}
            >
              <div className="flex items-start justify-between gap-2 border-b border-brand-border px-4 py-3">
                <div>
                  <h4 className="text-sm font-bold text-gray-900">生成历史</h4>
                  <p className="mt-0.5 text-[10px] text-gray-500">保留真实任务状态、失败代码与本地产物</p>
                </div>
                <button type="button" onClick={() => setHistoryOpen(false)} className="ui-btn px-2 text-[10px]">收起</button>
              </div>
              <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
                {generationHistory.map((item) => (
                  <article key={item.id} className="rounded-xl border border-brand-border bg-brand-bg p-3">
                    <div className="flex items-center justify-between gap-2">
                      <strong className="text-[11px] text-gray-800">任务 #{item.id}</strong>
                      <span className={`rounded-full px-2 py-0.5 text-[9px] font-semibold ${
                        item.status === 'succeeded' ? 'bg-emerald-500/10 text-emerald-700'
                          : item.status === 'failed' ? 'bg-red-500/10 text-red-600'
                            : 'bg-brand-warning/20/10 text-brand-warning'
                      }`}>{item.status}</span>
                    </div>
                    <p className="mt-1 text-[10px] text-gray-500">
                      {item.mode} · {item.model} · {item.output_size} · {Math.round(Math.max(0, Math.min(100, item.progress || 0)))}%
                    </p>
                    {item.error_code ? <code className="mt-2 block text-[10px] text-red-600">{item.error_code}</code> : null}
                    {item.output_asset ? (
                      <button type="button" onClick={() => addMediaNode(item.output_asset!)} className="mt-2 text-[10px] font-semibold text-brand-accent hover:underline">
                        将产物放到画布
                      </button>
                    ) : null}
                  </article>
                ))}
                {!generationHistory.length ? <p className="py-10 text-center text-[10px] text-gray-400">暂无生成任务</p> : null}
              </div>
            </aside>
          ) : null}
        </div>

        <div
          ref={workbenchRef}
          data-testid="ai-image-generation-workbench"
          className="shrink-0 border-t border-brand-border bg-brand-surface px-4 py-3 shadow-[0_-10px_24px_rgba(0,0,0,0.05)]"
        >
          <div className="mx-auto max-w-6xl">
            <div className="mb-2 flex min-h-12 items-center gap-2 overflow-x-auto">
              <strong className="shrink-0 text-[10px] text-gray-600">本次参考</strong>
              {selectedImageNodes.length ? selectedImageNodes.map((node, index) => {
                const asset = node.asset_name ? assetByName.get(node.asset_name) : undefined;
                return (
                  <article key={node.id} data-testid="ai-image-reference-chip" className="flex h-11 shrink-0 items-center gap-1.5 rounded-lg border border-brand-border bg-brand-bg pr-1.5">
                    {asset ? <AssetPreview asset={asset} className="h-11 w-11 rounded-l-lg object-cover" /> : <ImageIcon className="mx-2 h-4 w-4" />}
                    <button type="button" onClick={() => focusCanvasNode(node)} className="max-w-20 truncate text-[9px] font-semibold text-brand-accent" title="定位原图">
                      @{node.ref_label || `图${String(index + 1).padStart(2, '0')}`}
                    </button>
                    {generationKind === 'video' ? (
                      <span className="text-[9px] font-semibold text-brand-accent">首帧</span>
                    ) : (
                      <select
                        value={referenceRoles[node.id] || (index === 0 ? 'product' : 'style')}
                        onChange={(event) => setReferenceRoles((current) => ({ ...current, [node.id]: event.target.value as Exclude<AIImageReferenceRole, 'first_frame'> }))}
                        className="max-w-28 bg-transparent text-[9px] font-semibold text-gray-600 outline-none"
                        data-testid={`ai-reference-role-${node.id}`}
                        aria-label={`参考图 ${index + 1} 角色`}
                      >
                        {Object.entries(referenceRoleLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                      </select>
                    )}
                    <button type="button" onClick={() => moveReference(node.id, -1)} disabled={index === 0} className="px-1 text-[10px] text-gray-400 disabled:opacity-20" aria-label="参考图前移">←</button>
                    <button type="button" onClick={() => moveReference(node.id, 1)} disabled={index === selectedImageNodes.length - 1} className="px-1 text-[10px] text-gray-400 disabled:opacity-20" aria-label="参考图后移">→</button>
                    <button type="button" onClick={() => toggleNodeSelection(node)} className="p-1 text-gray-400 hover:text-red-500" aria-label="移除本次参考">
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </article>
                );
              }) : (
                <span className="text-[10px] text-gray-400">没有参考图时直接生成；上传图片后会自动作为本次参考</span>
              )}
              <button type="button" onClick={() => imageInputRef.current?.click()} disabled={importing} className="ui-btn h-9 shrink-0 px-2 text-[10px] disabled:opacity-40">
                <ImagePlus className="h-3.5 w-3.5" />上传参考
              </button>
            </div>

            <div className="grid items-end gap-2 lg:grid-cols-[minmax(280px,1fr)_auto_auto_auto_auto]">
              <label className="min-w-0">
                <span className="sr-only">生成提示词</span>
                <textarea
                  value={prompt}
                  onChange={(event) => setPrompt(event.target.value)}
                  rows={2}
                  maxLength={4000}
                  placeholder={generationKind === 'video'
                    ? selectedImageNodes.length ? '描述首帧之后的动作、镜头和节奏…' : '描述要生成的视频…'
                    : selectedImageNodes.length ? '描述如何调整参考图的主体、构图或风格…' : '描述你想生成的画面…'}
                  className={`max-h-28 min-h-[48px] w-full resize-none rounded-lg border bg-brand-bg px-3 py-2 text-xs leading-5 outline-none ${promptError ? 'border-red-400' : 'border-brand-border focus:border-brand-accent'}`}
                  data-testid="ai-image-prompt"
                />
              </label>
              <label className="ui-btn h-10">
                模型
                <select
                  id="ai-image-generation-model"
                  data-testid="ai-image-generation-model"
                  value={selectedModel}
                  onChange={(event) => changeGenerationModel(event.target.value)}
                  className="max-w-40 bg-transparent text-[11px] font-semibold outline-none"
                >
                  {modelOptions.map((model) => <option key={model} value={model}>{model}</option>)}
                </select>
              </label>
              <label className="ui-btn h-10">
                比例
                <select value={outputSize} onChange={(event) => setOutputSize(event.target.value)} data-testid="ai-image-output-size" className="bg-transparent text-[11px] font-semibold outline-none" aria-label="生成比例">
                  {['1:1', '3:4', '4:3', '16:9', '9:16', '2:3', '3:2'].map((ratio) => <option key={ratio} value={ratio}>{ratio}</option>)}
                  {generationKind === 'image' ? <option value="21:9">21:9</option> : null}
                </select>
              </label>
              {generationKind === 'video' ? (
                <label className="ui-btn h-10">
                  时长
                  <select
                    value={videoDurationSeconds}
                    onChange={(event) => setVideoDurationSeconds(Number(event.target.value) as 5 | 10)}
                    data-testid="ai-video-duration-seconds"
                    className="bg-transparent text-[11px] font-semibold outline-none"
                    aria-label="视频生成时长"
                  >
                    <option value={5}>5 秒</option>
                    <option value={10}>10 秒</option>
                  </select>
                </label>
              ) : null}
              <label className="ui-btn h-10">
                数量
                <input type="number" min={1} max={8} value={batchCount} onChange={(event) => setBatchCount(Math.min(8, Math.max(1, Number(event.target.value) || 1)))} data-testid="ai-image-batch-count" className="w-8 bg-transparent text-center font-mono text-xs outline-none" aria-label="批量生成数量，最多 8 个" />
              </label>
              <button type="button" onClick={requestProduce} disabled={!workspaceReady || generating || Boolean(promptError || referenceError)} className="ui-btn ui-btn-primary h-10 px-4 disabled:opacity-40" data-testid="ai-image-produce">
                {generating ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
                {generating ? '生成中' : batchCount > 1 ? `生成 ×${batchCount}` : '生成'}
              </button>
            </div>

            <div className="mt-1 flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-[9px]">
              <span
                className={promptError || referenceError || generation?.error_code ? 'text-red-600' : 'text-gray-400'}
                data-testid={generation?.error_code === 'GENERATION_PROVIDER_STATE_UNKNOWN' ? 'ai-generation-provider-state-unknown' : undefined}
              >
                {generation?.error_code === 'GENERATION_PROVIDER_STATE_UNKNOWN'
                  ? '服务商提交状态不确定，不能安全自动重试；请先核对后再主动发起新任务'
                  : promptError || referenceError || (generating && batchGenerations.length > 1
                  ? `批量任务 ${batchGenerations.filter((item) => item.status === 'succeeded' || item.status === 'failed').length}/${batchGenerations.length} 已完成`
                  : generating && generation
                    ? `任务 #${generation.id} · ${generation.status} · ${Math.round(Math.max(0, Math.min(100, generation.progress || 0)))}%`
                    : config?.credential_configured
                      ? '生成会调用付费服务；每次提交前都需要再次确认'
                      : '未配置密钥时只创建待执行卡，不会伪造生成内容')}
              </span>
              <div className="flex items-center gap-2">
                <span className="text-gray-400">{promptBytes}/8000 B</span>
                <button type="button" onClick={() => setAdvancedOpen((current) => !current)} className="font-semibold text-gray-500 hover:text-brand-accent">
                  高级设置 {advancedOpen ? '收起' : '展开'}
                </button>
              </div>
            </div>

            {advancedOpen ? (
              <div className="mt-2 flex flex-wrap items-center gap-3 rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-[9px] text-gray-500">
                <button type="button" onClick={openSettings} className="inline-flex items-center gap-1 font-semibold text-brand-accent hover:underline" data-testid="ai-image-open-settings">
                  <Settings2 className="h-3 w-3" />Provider 与凭证设置
                </button>
                <span>@图01 仍可作为快捷引用；常规使用直接在上方参考托盘管理即可。</span>
                {generationMode === 'image_to_image' ? <span>参考角色是本地标记，服务商按当前排序读取图片。</span> : null}
                {generationMode === 'image_to_video' ? <span className="text-brand-warning">图生视频会先生成托管派生首帧，再调用视频模型，可能产生两次费用。</span> : null}
              </div>
            ) : null}

            {confirmingGeneration ? (
              <div role="alertdialog" aria-label="确认付费生成" className="mt-2 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-brand-warning/30/40 bg-brand-warning/20/10 px-3 py-2 text-[10px] text-brand-warning">
                <span>将提交 {batchCount} 个{generationKind === 'video' ? '视频' : '图片'}生成任务，可能产生费用。失败不会自动重试。</span>
                <div className="flex gap-2">
                  <button type="button" onClick={() => setConfirmingGeneration(false)} className="ui-btn h-8">取消</button>
                  <button type="button" onClick={() => { setConfirmingGeneration(false); void produce(); }} className="ui-btn ui-btn-primary h-8">确认并生成</button>
                </div>
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </section>
  );
}
