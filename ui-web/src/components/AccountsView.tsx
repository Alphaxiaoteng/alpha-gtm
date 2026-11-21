import React, { useState, useEffect } from 'react';
import useSWR from 'swr';
import {
  api,
  type Account,
  type AccountLoginEvent,
  type BrowserLifecycleEvent,
  type BrowserInstance,
	 type AccountAsset,
	 type AccountTag,
	 type AccountSessionBackupList,
} from '../api';
import { useUIStore } from '../store';
import { AccountAvatar } from './AccountAvatar';
import { PlatformIcon } from './PlatformIcon';
import {
  Search,
  Plus,
  Play,
  Square,
  RefreshCw,
  Trash2,
  Fingerprint,
  Download,
  X,
  Sparkles,
  Info,
  Globe,
  ExternalLink,
  Check,
  LayoutGrid,
  MonitorDot,
  MoreHorizontal,
  History,
  Tags,
  ChevronUp,
  ChevronDown,
} from 'lucide-react';

const loginReasonLabels: Record<string, string> = {
  VERIFIED_COOKIE: '会话凭证有效',
  VERIFIED_PROFILE_DOM: '账号主页身份已确认',
  VERIFIED_SESSION_DOM: '创作后台会话已确认',
  REVIEWED_SESSION_DOM: '可信创作后台登录证据',
  LOGIN_REQUIRED_PAGE: '平台要求重新登录',
  SESSION_EXPIRED_PAGE: '平台会话已过期',
  CHECKPOINT_REQUIRED: '平台要求安全验证',
  PROFILE_SELECTOR_MISSING: '当前页面未找到账号身份',
  PLATFORM_TAB_MISSING: '未找到该平台页面',
  PLATFORM_UNREACHABLE: '平台当前不可访问，保留上次登录状态',
  CDP_UNREACHABLE: '浏览器调试连接不可用',
  BROWSER_PROCESS_EXITED: '账号浏览器已退出（平台登录态保留）',
  BROWSER_STOPPED_BY_USER: '用户从 Alpha-Nexus 关闭了浏览器',
  BROWSER_CRASHED: '浏览器进程异常退出',
  BROWSER_HEARTBEAT_FAILED: '浏览器 CDP 心跳失联',
  BROWSER_LEASE_LOST: '浏览器运行租约丢失',
  BROWSER_OPERATION_BUSY: '账号浏览器正在执行其他操作',
  AVATAR_CAPTURE_FAILED: '登录已确认，但账号头像暂未更新',
	AVATAR_PERSIST_FAILED: '登录已确认，但账号头像保存失败',
  MANUAL_VERIFY_REQUIRED: '需要在浏览器中人工确认',
};

const observerErrorLabels: Record<string, string> = {
  DATABASE_READ_FAILED: '账号数据读取失败',
  OBSERVATION_PERSIST_FAILED: '监测结果保存失败',
  CONTEXT_CANCELLED: '本轮监测已中止',
};

const loginModeLabels: Record<string, string> = {
  browser_profile_session: '独立浏览器会话',
  manual_browser: '人工浏览器登录',
  unknown: '历史方式未确认',
};

const browserEventLabels: Record<string, string> = {
  started: '浏览器开始启动',
  ready: '浏览器已就绪',
  stopped: '浏览器已退出（旧记录）',
  stopped_by_user: '用户主动关闭浏览器',
  process_exited: '浏览器进程已退出',
  process_missing_after_restart: '重启后发现浏览器已退出',
  crashed: '浏览器进程异常退出',
  supervisor_detached: '控制面已退出，浏览器继续运行',
  supervisor_shutdown: '控制面请求关闭浏览器',
  focus_failed: '窗口聚焦失败',
  focus_recovered: '窗口聚焦恢复',
  focus_operation_failed: '本次窗口置前失败',
  focus_succeeded: '账号窗口已切换置前',
  heartbeat_failed: '浏览器连接心跳中断',
  heartbeat_recovered: '浏览器连接心跳恢复',
  lease_lost: '浏览器运行租约丢失',
  lease_recovered: '浏览器运行租约恢复',
  degraded_unknown: '浏览器进入异常状态',
};

const browserReasonLabels: Record<string, string> = {
  USER_STOP: '用户主动关闭',
  PROCESS_EXIT_CLEAN: '浏览器正常退出',
  PROCESS_EXIT_NONZERO: '浏览器返回非零退出码',
  PROCESS_SIGNALED: '浏览器收到系统终止信号',
  PROCESS_MISSING_AFTER_RESTART: '控制面重启后发现原进程已不存在',
  SUPERVISOR_DETACHED: '控制面已安全分离，浏览器未关闭',
  SUPERVISOR_SHUTDOWN: '控制面明确请求关闭浏览器',
  LEASE_LOST: '运行租约已被其他会话接管',
  WAIT_RESULT_UNKNOWN: '浏览器退出结果无法分类',
};

function browserEventTone(eventType: string): string {
  if (eventType === 'crashed' || eventType === 'heartbeat_failed' || eventType === 'lease_lost') return 'bg-red-500';
  if (eventType === 'focus_failed' || eventType === 'focus_operation_failed' || eventType === 'degraded_unknown') return 'bg-brand-warning/20';
  if (eventType === 'ready' || eventType === 'focus_succeeded' || eventType.endsWith('_recovered')) return 'bg-emerald-500';
  return 'bg-gray-400';
}

const accountStateRefreshInterval = 1_000;

function formatObservedAt(value?: string): string {
  if (!value) return '尚未监测';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '时间未验证' : date.toLocaleString('zh-CN', { hour12: false });
}

function loginStateLabel(state: string): string {
  if (state === 'logged_in') return '已登录';
  if (state === 'logged_out') return '已掉线';
  return '待验证';
}

function displayAccountIdentifier(account: Account): string {
  const value = (account.accountIdentifier || '').trim();
  if (!value || account.platformCode !== 'xhs') return value;
  return value.replace(/^小红书账号\s*[:：]\s*/, '').trim();
}

function accountActionHint(account: Account): string {
  if (account.loginReasonCode === 'PLATFORM_UNREACHABLE') {
    return loginReasonLabels.PLATFORM_UNREACHABLE;
  }
  if (account.loginState === 'logged_out') {
    if (account.loginReasonCode === 'CHECKPOINT_REQUIRED') return '需要完成平台安全验证';
    return '需要重新登录';
  }
  if (account.browserState === 'degraded' || account.browserState === 'crashed') {
    return account.loginState === 'logged_in'
      ? '浏览器连接异常，但账号仍已登录'
      : '浏览器连接异常，请重新打开后检测';
  }
  if (account.loginState === 'unknown') {
    if (!account.browserRunning) return '打开浏览器后即可确认登录状态';
    if (account.loginReasonCode === 'CHECKPOINT_REQUIRED') return '需要完成平台安全验证';
    return '暂时无法确认登录状态，请点击检测';
  }
  if (account.avatarErrorCode) return '头像待更新，不影响账号登录';
  return '';
}

/** Merge /api/browsers runtime into account row fields for display. */
function mergeRuntime(acct: Account, byId: Map<number, BrowserInstance>): Account {
  const rt = byId.get(acct.id);
  if (!rt) {
    return {
      ...acct,
      browserRunning: acct.browserRunning ?? false,
      browserState: acct.browserState || 'stopped',
    };
  }
  return {
    ...acct,
    browserRunning: true,
    browserState: rt.state || 'ready',
    cdpPort: rt.cdpPort || acct.cdpPort,
    leaseOwner: rt.leaseOwner || acct.leaseOwner,
    profileId: rt.profileId || acct.profileId,
  };
}

type AccountCardGroup = {
  primary: Account;
  members: Account[];
  isMeta: boolean;
};

type AccountTagFilter = 'all' | 'unclassified' | number;
type AccountTagEditorMode = 'manage' | 'single' | 'bulk';
type AccountCategoryFilter = 'all' | 'multimedia' | 'article' | 'ecommerce' | 'other';
type AccountPlatformCategory = 'multimedia' | 'article' | 'ecommerce' | 'other';
const ACCOUNT_CATEGORY_OPTIONS: { key: AccountCategoryFilter; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'multimedia', label: '多媒体' },
  { key: 'article', label: '文章' },
  { key: 'ecommerce', label: '电商' },
];
function normalizeAccountCategory(category?: string): AccountPlatformCategory {
  if (category === 'multimedia' || category === 'article' || category === 'ecommerce') return category;
  return 'other';
}

function isMetaPlatform(platformCode: string) {
  return platformCode === 'meta' || platformCode === 'facebook' || platformCode === 'instagram';
}

function hasVerifiedDisplayName(account: Account) {
  const value = (account.name || '').trim();
  return Boolean(value) && !/^(facebook|instagram)-\d+$/i.test(value);
}

function buildAccountCards(accounts: Account[]): AccountCardGroup[] {
  const result: AccountCardGroup[] = [];
  const groupedMeta = new Map<string, Account[]>();
  for (const account of accounts) {
    const group = (account.accountGroup || '').trim();
    if (!isMetaPlatform(account.platformCode) || !group) {
      result.push({ primary: account, members: [account], isMeta: isMetaPlatform(account.platformCode) });
      continue;
    }
    const members = groupedMeta.get(group) ?? [];
    members.push(account);
    groupedMeta.set(group, members);
  }
  for (const members of groupedMeta.values()) {
    const facebook = members.find((account) => account.platformCode === 'facebook');
    const instagram = members.find((account) => account.platformCode === 'instagram');
    const primary = (facebook && hasVerifiedDisplayName(facebook) ? facebook : undefined)
      ?? (instagram && hasVerifiedDisplayName(instagram) ? instagram : undefined)
      ?? facebook
      ?? instagram
      ?? members[0];
    result.push({ primary, members, isMeta: true });
  }
  return result.sort((left, right) => {
    const leftRunning = left.members.some((account) => account.browserRunning);
    const rightRunning = right.members.some((account) => account.browserRunning);
    return Number(rightRunning) - Number(leftRunning);
  });
}

export const AccountsView: React.FC = () => {
  const { 
    searchQuery, 
    setSearchQuery, 
    toast,
    selectedAccountIds,
    setSelectedAccountIds,
    clearSelection,
    setActiveTab,
  } = useUIStore();
  // Drag selection state
  const [dragStart, setDragStart] = useState<{ x: number; y: number } | null>(null);
  const [dragEnd, setDragEnd] = useState<{ x: number; y: number } | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [isBatchSelecting, setIsBatchSelecting] = useState(false);
  const [isBatchClosing, setIsBatchClosing] = useState(false);
  
  // Modals state
  const [isAddOpen, setIsAddOpen] = useState(false);
  const [isFpOpen, setIsFpOpen] = useState(false);
  const [selectedAccountForFp, setSelectedAccountForFp] = useState<Account | null>(null);
  const [observingAccountID, setObservingAccountID] = useState<number | null>(null);
  const [isObservingAll, setIsObservingAll] = useState(false);
  const [editingRemarkAccountID, setEditingRemarkAccountID] = useState<number | null>(null);
  const [remarkDraft, setRemarkDraft] = useState('');
  const [isRemarkSaving, setIsRemarkSaving] = useState(false);
	const [isTagsOpen, setIsTagsOpen] = useState(false);
	const [selectedTagIds, setSelectedTagIds] = useState<number[]>([]);
	const [tagMode, setTagMode] = useState<'append' | 'remove' | 'replace'>('append');
	const [newTagName, setNewTagName] = useState('');
	const [newTagColor, setNewTagColor] = useState('#111111');
	const [isTagSaving, setIsTagSaving] = useState(false);
  const [activeTagFilter, setActiveTagFilter] = useState<AccountTagFilter>('all');
  const [activeCategory, setActiveCategory] = useState<AccountCategoryFilter>('all');
  const [tagEditorMode, setTagEditorMode] = useState<AccountTagEditorMode>('manage');
  const [tagEditorAccountID, setTagEditorAccountID] = useState<number | null>(null);
  const [historyAccount, setHistoryAccount] = useState<Account | null>(null);
  const [historyEvents, setHistoryEvents] = useState<AccountLoginEvent[]>([]);
  const [browserHistoryEvents, setBrowserHistoryEvents] = useState<BrowserLifecycleEvent[]>([]);
  const [isHistoryLoading, setIsHistoryLoading] = useState(false);
  const [isSessionTransferOpen, setIsSessionTransferOpen] = useState(false);
  const [sessionAccountID, setSessionAccountID] = useState<number | null>(null);
  const [sessionBackups, setSessionBackups] = useState<AccountSessionBackupList | null>(null);
  const [sessionExportPassphrase, setSessionExportPassphrase] = useState('');
  const [sessionExportConfirm, setSessionExportConfirm] = useState('');
  const [sessionImportPassphrase, setSessionImportPassphrase] = useState('');
  const [sessionImportFile, setSessionImportFile] = useState<File | null>(null);
  const [isSessionWorking, setIsSessionWorking] = useState(false);

  const exitBatchSelection = React.useCallback(() => {
    clearSelection();
    setIsBatchSelecting(false);
  }, [clearSelection]);

  // Workspace shortcuts are ignored while typing. Selection is derived from
  // rendered cards so Cmd/Ctrl+A respects the current search.
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const isTyping = Boolean(target?.closest('input, textarea, select, [contenteditable="true"]'));
      if (e.key === 'Escape') {
        e.preventDefault();
        if (historyAccount) {
          setHistoryAccount(null);
        } else if (isTagsOpen) {
          setIsTagsOpen(false);
		} else if (isSessionTransferOpen) {
			setIsSessionTransferOpen(false);
        } else if (isFpOpen) {
          setIsFpOpen(false);
        } else if (isAddOpen) {
          setIsAddOpen(false);
        } else {
          exitBatchSelection();
        }
		return;
      }
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			(document.querySelector('[data-testid="accounts-search"]') as HTMLInputElement | null)?.focus();
			return;
		}
		if (isTyping || document.querySelector('.fixed.inset-0')) return;
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
			e.preventDefault();
			const ids = Array.from(document.querySelectorAll('[data-account-id]'))
				.flatMap((node) => (node.getAttribute('data-account-ids') || node.getAttribute('data-account-id') || '')
					.split(',').map((value) => Number(value)))
				.filter((id) => Number.isInteger(id) && id > 0);
			setSelectedAccountIds(ids);
			setIsBatchSelecting(true);
			return;
		}
		if (!e.metaKey && !e.ctrlKey && !e.altKey && e.key.toLowerCase() === 'r') {
			e.preventDefault();
			(document.querySelector('[data-testid="observe-all-accounts"]') as HTMLButtonElement | null)?.click();
		}
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [exitBatchSelection, historyAccount, isAddOpen, isFpOpen, isTagsOpen, isSessionTransferOpen, setSelectedAccountIds]);

  // Add Account form fields (T-C03)
  const [addPlatform, setAddPlatform] = useState('xhs');
  const [addName, setAddName] = useState('');
  const [addRemark, setAddRemark] = useState('');
  const [addStartNow, setAddStartNow] = useState(true);

  // Fingerprint form fields
  const [fpSeed, setFpSeed] = useState(0);
  const [fpRegion, setFpRegion] = useState('beijing');
  const [fpPlatform, setFpPlatform] = useState('macos');
  const [fpBrand, setFpBrand] = useState('Chrome');
  const [fpBrandVer, setFpBrandVer] = useState('139');
  const [fpCpu, setFpCpu] = useState(8);
  const [fpLang, setFpLang] = useState('zh-CN');
  const [fpAccept, setFpAccept] = useState('zh-CN,zh,en');
  const [fpTz, setFpTz] = useState('Asia/Shanghai');
  const [fpGpuVendor, setFpGpuVendor] = useState('');
  const [fpGpuRenderer, setFpGpuRenderer] = useState('');
  const [fpEngine, setFpEngine] = useState('chromium');

  // The server owns passive monitoring; the UI only reads persisted local state.
  const { data: accountsRaw, error: accountsError, mutate: mutateAccounts } = useSWR('api/accounts', api.getAccounts, {
    refreshInterval: accountStateRefreshInterval,
  });
  const { data: browserInstances, mutate: mutateBrowsers } = useSWR(
    'api/browsers',
    api.getBrowsers,
    { refreshInterval: accountStateRefreshInterval },
  );
	const { data: accountTags, mutate: mutateAccountTags } = useSWR('api/account-tags', api.getAccountTags);
	const { data: accountAssets } = useSWR<AccountAsset[]>('api/account-assets', api.getAccountAssets, {
	  refreshInterval: 3_000,
	});
  const { data: platforms } = useSWR('api/platforms', api.getPlatforms);
  const addPlatformOptions = React.useMemo(() => {
    const listed = platforms ?? [];
    const metaSource = listed.find((platform) => platform.code === 'instagram')
      ?? listed.find((platform) => platform.code === 'facebook');
    const withoutLegacyMetaSplit = listed.filter(
      (platform) => platform.code !== 'facebook' && platform.code !== 'instagram',
    );
    return metaSource
      ? [{ ...metaSource, code: 'meta', name: 'Meta' }, ...withoutLegacyMetaSplit]
      : withoutLegacyMetaSplit;
  }, [platforms]);
  const { data: observerStatus, mutate: mutateObserverStatus } = useSWR(
    'api/account-observer/status',
    api.getAccountObserverStatus,
    { refreshInterval: accountStateRefreshInterval },
  );

  const runtimeByAccount = React.useMemo(() => {
    const m = new Map<number, BrowserInstance>();
    (browserInstances ?? []).forEach((b) => {
      if (b.accountId) m.set(b.accountId, b);
    });
    return m;
  }, [browserInstances]);

  const accounts = React.useMemo(
    () => (accountsRaw ?? []).map((a) => mergeRuntime(a, runtimeByAccount)),
    [accountsRaw, runtimeByAccount],
  );
  const orderedAccountTags = React.useMemo(
    () => [...(accountTags ?? [])].sort((left, right) => {
      const positionDiff = (left.position ?? 0) - (right.position ?? 0);
      return positionDiff || left.id - right.id;
    }),
    [accountTags],
  );
  const tagEditorAccount = React.useMemo(
    () => accounts.find((account) => account.id === tagEditorAccountID) ?? null,
    [accounts, tagEditorAccountID],
  );

  const sessionAccount = React.useMemo(
    () => accounts.find((account) => account.id === sessionAccountID) ?? null,
    [accounts, sessionAccountID],
  );

  const loadSessionBackups = React.useCallback(async (accountID: number) => {
    try {
      setSessionBackups(await api.getAccountSessionBackups(accountID));
    } catch (error: any) {
      setSessionBackups(null);
      toast.show(`读取会话备份失败: ${error.message}`, 'error');
    }
  }, [toast]);

  useEffect(() => {
    if (!isSessionTransferOpen || !sessionAccountID) {
      setSessionBackups(null);
      return;
    }
    void loadSessionBackups(sessionAccountID);
  }, [isSessionTransferOpen, loadSessionBackups, sessionAccountID]);

  const refreshAll = () => {
    mutateAccounts();
    mutateBrowsers();
		mutateAccountTags();
    mutateObserverStatus();
  };

  const openSessionTransfer = (account?: Account) => {
    setSessionAccountID(account?.id ?? selectedAccountIds[0] ?? null);
    setSessionExportPassphrase('');
    setSessionExportConfirm('');
    setSessionImportPassphrase('');
    setSessionImportFile(null);
    setIsSessionTransferOpen(true);
  };

  const handleCreateSessionBackup = async () => {
    if (!sessionAccount) return;
    setIsSessionWorking(true);
    try {
      await api.createAccountSessionBackup(sessionAccount.id);
      await loadSessionBackups(sessionAccount.id);
      toast.show('已创建本机加密会话备份，保留 35 天', 'success');
    } catch (error: any) {
      toast.show(`会话备份失败: ${error.message}`, 'error');
    } finally {
      setIsSessionWorking(false);
    }
  };

  const handleCreateAllSessionBackups = async () => {
    setIsSessionWorking(true);
    try {
      const result = await api.createAllAccountSessionBackups();
      const saved = result.results.filter((item) => item.status === 'backed_up').length;
      const unavailable = result.results.length - saved;
      if (sessionAccountID) await loadSessionBackups(sessionAccountID);
      toast.show(`已备份 ${saved} 个已登录账号${unavailable ? `，${unavailable} 个暂不可用` : ''}`, unavailable ? 'info' : 'success');
    } catch (error: any) {
      toast.show(`批量会话备份失败: ${error.message}`, 'error');
    } finally {
      setIsSessionWorking(false);
    }
  };

  const handleRestoreLatestSessionBackup = async () => {
    const latest = sessionBackups?.backups[0];
    if (!sessionAccount || !latest) return;
    setIsSessionWorking(true);
    try {
      const result = await api.restoreAccountSessionBackup(sessionAccount.id, latest.created_at.slice(0, 10));
      refreshAll();
      toast.show(result.message, 'info');
    } catch (error: any) {
      toast.show(`恢复会话失败: ${error.message}`, 'error');
    } finally {
      setIsSessionWorking(false);
    }
  };

  const handleExportSession = async () => {
    if (!sessionAccount) return;
    if (sessionExportPassphrase.length < 12) {
      toast.show('迁移口令至少需要 12 个字符', 'error');
      return;
    }
    if (sessionExportPassphrase !== sessionExportConfirm) {
      toast.show('两次迁移口令不一致', 'error');
      return;
    }
    setIsSessionWorking(true);
    try {
      const blob = await api.exportAccountSession(sessionAccount.id, sessionExportPassphrase);
      const link = document.createElement('a');
      const objectURL = URL.createObjectURL(blob);
      link.href = objectURL;
      link.download = 'alpha-nexus-account.anxsession';
      link.click();
      URL.revokeObjectURL(objectURL);
      setSessionExportPassphrase('');
      setSessionExportConfirm('');
      toast.show('加密迁移包已导出，请单独安全保存迁移口令', 'success');
    } catch (error: any) {
      toast.show(`导出会话失败: ${error.message}`, 'error');
    } finally {
      setIsSessionWorking(false);
    }
  };

  const handleImportSession = async () => {
    if (!sessionImportFile) {
      toast.show('请选择 .anxsession 迁移包', 'error');
      return;
    }
    if (sessionImportPassphrase.length < 12) {
      toast.show('请输入至少 12 个字符的迁移口令', 'error');
      return;
    }
    setIsSessionWorking(true);
    try {
      const result = await api.importAccountSession(sessionImportFile, sessionImportPassphrase);
      refreshAll();
      setIsSessionTransferOpen(false);
      toast.show(result.message || '迁移完成，请在新浏览器中确认平台登录状态', 'info');
    } catch (error: any) {
      toast.show(`导入会话失败: ${error.message}`, 'error');
    } finally {
      setIsSessionWorking(false);
    }
  };

	const toggleTag = (tagId: number) => {
		setSelectedTagIds((current) => current.includes(tagId) ? current.filter((id) => id !== tagId) : [...current, tagId]);
	};

  const openTagManager = () => {
    setTagEditorMode('manage');
    setTagEditorAccountID(null);
    setSelectedTagIds([]);
    setIsTagsOpen(true);
  };

  const openSingleTagEditor = (account: Account) => {
    setTagEditorMode('single');
    setTagEditorAccountID(account.id);
    setSelectedTagIds((account.tags ?? []).map((tag) => tag.id));
    setTagMode('replace');
    setIsTagsOpen(true);
  };

  const openBulkTagEditor = () => {
    if (selectedAccountIds.length === 0) {
      toast.show('请先选择当前筛选中的账号', 'error');
      return;
    }
    setTagEditorMode('bulk');
    setTagEditorAccountID(null);
    setSelectedTagIds([]);
    setTagMode('append');
    setIsTagsOpen(true);
  };

	const handleCreateTag = async () => {
		if (!newTagName.trim()) {
			toast.show('请输入标签名称', 'error');
			return;
		}
		setIsTagSaving(true);
		try {
			const tag = await api.createAccountTag(newTagName.trim(), newTagColor);
			await mutateAccountTags();
			setNewTagName('');
      if (tagEditorMode !== 'manage') {
        setSelectedTagIds((current) => current.includes(tag.id) ? current : [...current, tag.id]);
      }
			toast.show('标签已保存', 'success');
		} catch (error: any) {
			toast.show(`标签保存失败: ${error.message}`, 'error');
		} finally {
			setIsTagSaving(false);
		}
	};

  const moveAccountTag = async (tagId: number, direction: -1 | 1) => {
    const currentIndex = orderedAccountTags.findIndex((tag) => tag.id === tagId);
    const nextIndex = currentIndex + direction;
    if (currentIndex < 0 || nextIndex < 0 || nextIndex >= orderedAccountTags.length || isTagSaving) return;
    const previous = orderedAccountTags;
    const next = orderedAccountTags.map((tag) => ({ ...tag }));
    [next[currentIndex], next[nextIndex]] = [next[nextIndex], next[currentIndex]];
    const positioned = next.map((tag, index) => ({ ...tag, position: index }));
    setIsTagSaving(true);
    await mutateAccountTags(positioned, false);
    try {
      await api.reorderAccountTags(positioned.map((tag) => tag.id));
      await Promise.all([mutateAccountTags(), mutateAccounts()]);
      toast.show('标签顺序已保存', 'success');
    } catch (error: any) {
      await mutateAccountTags(previous, false);
      toast.show(`标签排序失败: ${error.message}`, 'error');
    } finally {
      setIsTagSaving(false);
    }
  };

	const handleBulkTags = async () => {
    const targetAccountIDs = tagEditorMode === 'single' && tagEditorAccountID
      ? [tagEditorAccountID]
      : selectedAccountIds;
    const updateMode = tagEditorMode === 'single' ? 'replace' : tagMode;
		if (targetAccountIDs.length === 0) {
			toast.show('请先选择账号', 'error');
			return;
		}
		if (selectedTagIds.length === 0 && updateMode !== 'replace') {
			toast.show('请选择至少一个标签', 'error');
			return;
		}
		setIsTagSaving(true);
		try {
			await api.bulkUpdateAccountTags(targetAccountIDs, selectedTagIds, updateMode);
			await Promise.all([mutateAccounts(), mutateAccountTags()]);
			setIsTagsOpen(false);
      if (tagEditorMode === 'bulk') exitBatchSelection();
      setTagEditorAccountID(null);
			toast.show(
        tagEditorMode === 'single'
          ? `${tagEditorAccount?.name ?? '账号'}的标签已更新`
          : `已为 ${targetAccountIDs.length} 个账号更新标签`,
        'success',
      );
		} catch (error: any) {
			toast.show(`标签更新失败: ${error.message}`, 'error');
		} finally {
			setIsTagSaving(false);
		}
	};

  const handleStartBrowser = async (id: number) => {
    toast.show('正在启动账号浏览器...', 'info');
    try {
      const res = await api.startLoginSession(id);
      refreshAll();
      if (res?.browser?.alreadyRunning) {
        toast.show('账号浏览器已在运行', 'success');
      } else {
        toast.show('账号浏览器已启动', 'success');
      }
    } catch (e: any) {
      toast.show(`启动失败: ${e.message}`, 'error');
    }
  };

  const handleStopBrowser = async (id: number) => {
    toast.show('正在关闭浏览器...', 'info');
    try {
      await api.stopBrowser(id);
      refreshAll();
      toast.show('浏览器已关闭', 'success');
    } catch (e: any) {
      toast.show(`关闭失败: ${e.message}`, 'error');
    }
  };

  const handleBatchStopBrowsers = async (accountIDs: number[], clearAfterSuccess: boolean) => {
    const seenProfiles = new Set<string>();
    const targets = accountIDs.filter((id) => {
      const account = accounts.find((item) => item.id === id);
      if (!account?.browserRunning) return false;
      const profileKey = account.profileId?.trim() || `account:${id}`;
      if (seenProfiles.has(profileKey)) return false;
      seenProfiles.add(profileKey);
      return true;
    });
    if (targets.length === 0) {
      toast.show('没有可关闭的运行中浏览器', 'info');
      return;
    }
    if (!window.confirm(`确认关闭 ${targets.length} 个正在运行的账号浏览器？账号 Profile 和登录数据会保留。`)) {
      return;
    }
    setIsBatchClosing(true);
    toast.show(`正在关闭 ${targets.length} 个浏览器…`, 'info');
    try {
      const result = await api.stopBrowsers(targets);
      const failedIDs = result.results.filter((item) => !item.stopped).map((item) => item.account_id);
      const succeeded = result.stopped;
      refreshAll();
      if (clearAfterSuccess) {
        if (failedIDs.length === 0) exitBatchSelection();
        else setSelectedAccountIds(failedIDs);
      }
      toast.show(
        failedIDs.length
          ? `已关闭 ${succeeded} / ${result.requested} 个；${failedIDs.length} 个关闭失败，已保留选择便于重试`
          : `已关闭 ${succeeded} 个浏览器`,
        failedIDs.length ? (succeeded > 0 ? 'info' : 'error') : 'success',
      );
    } catch (cause) {
      toast.show(`批量关闭失败：${cause instanceof Error ? cause.message : '未知错误'}`, 'error');
    } finally {
      setIsBatchClosing(false);
    }
  };

  const handleFocusBrowser = async (accountId: number) => {
    toast.show('正在将浏览器窗口置前...', 'info');
    try {
      await api.focusBrowser(accountId);
      refreshAll();
      toast.show('窗口已置前', 'success');
    } catch (e: any) {
      toast.show(`操作失败: ${e.message}`, 'error');
    }
  };

  const handleLayoutBrowsers = async () => {
    toast.show('正在排列浏览器窗口...', 'info');
    try {
      await api.layoutBrowsers();
      toast.show('窗口排列完成', 'success');
    } catch (e: any) {
      toast.show(`排列失败: ${e.message}`, 'error');
    }
  };

  const handleVerifyAccount = async (id: number) => {
    toast.show('正在检测登录态...', 'info');
    setObservingAccountID(id);
    try {
      const res = await api.observeAccount(id);
      refreshAll();
      if (res.authState === 'logged_in') {
		const avatarSaved = res.avatarSource === 'observed' && Boolean(res.avatarSha256);
		toast.show(
		  res.avatarErrorCode
			? `检测成功：${res.name || '账号'} 已登录，昵称已保存；头像本轮更新失败`
			: avatarSaved
			  ? `检测成功：${res.name || '账号'} 已登录，昵称和头像已保存`
			  : `检测成功：${res.name || '账号'} 已登录；头像待同步`,
		  res.avatarErrorCode || !avatarSaved ? 'info' : 'success',
		);
	  } else if (res.authState === 'unknown') {
		toast.show(`本次无法确认登录态：${loginReasonLabels[res.reasonCode] || res.reasonCode}`, 'info');
      } else {
        toast.show(`已检测为掉线：${loginReasonLabels[res.reasonCode] || res.reasonCode}`, 'error');
      }
    } catch (e: any) {
      toast.show(`检测失败: ${e.message}`, 'error');
    } finally {
      setObservingAccountID(null);
    }
  };

  const handleObserveAll = async () => {
    if (isObservingAll) return;
    setIsObservingAll(true);
    toast.show('正在串行监测账号身份、头像和登录态...', 'info');
    try {
      const result = await api.observeAccounts();
      refreshAll();
      const loggedIn = result.observations.filter((item) => item.authState === 'logged_in').length;
      const loggedOut = result.observations.filter((item) => item.authState === 'logged_out').length;
      const unknown = result.observations.length - loggedIn - loggedOut;
      toast.show(`监测完成：${loggedIn} 个已登录，${loggedOut} 个掉线，${unknown} 个待确认`, loggedOut > 0 ? 'error' : 'success');
    } catch (e: any) {
      toast.show(`监测失败: ${e.message}`, 'error');
    } finally {
      setIsObservingAll(false);
    }
  };

  const openLoginHistory = async (account: Account) => {
    setHistoryAccount(account);
    setHistoryEvents([]);
    setBrowserHistoryEvents([]);
    setIsHistoryLoading(true);
    try {
      const [loginResult, browserResult] = await Promise.allSettled([
        api.getAccountLoginEvents(account.id),
        api.getAccountBrowserEvents(account.id),
      ]);
      if (loginResult.status === 'rejected') throw loginResult.reason;
      setHistoryEvents(loginResult.value);
      if (browserResult.status === 'fulfilled') setBrowserHistoryEvents(browserResult.value);
    } catch (e: any) {
      toast.show(`读取状态记录失败: ${e.message}`, 'error');
    } finally {
      setIsHistoryLoading(false);
    }
  };

  const handleDeleteAccount = async (id: number) => {
    if (!window.confirm('确认删除该账号及对应的浏览器画像缓存吗？数据不可恢复！')) return;
    try {
      await api.deleteAccount(id);
      mutateAccounts();
      toast.show('账号删除成功', 'success');
    } catch (e: any) {
      toast.show(`删除失败: ${e.message}`, 'error');
    }
  };

  // Add account submit — creates account + independent browser_profile (T-C03)
  const handleAddAccountSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const newAccount = await api.addAccount(addPlatform, addName, addRemark, {
        startNow: addStartNow,
      });
      refreshAll();
      setIsAddOpen(false);
      setAddName('');
      setAddRemark('');
      if (newAccount?.started) {
        toast.show('账号已创建并启动浏览器，请人工登录接管', 'success');
      } else if (addStartNow && newAccount?.startError) {
        toast.show(`账号已创建，启动失败: ${newAccount.startError}`, 'error');
      } else if (addStartNow && newAccount?.id) {
        toast.show('账号添加成功，正在启动指纹浏览器...', 'success');
        handleStartBrowser(newAccount.id);
      } else {
        toast.show('账号与独立 profile 已创建', 'success');
      }
    } catch (e: any) {
      toast.show(`添加失败: ${e.message}`, 'error');
    }
  };

  const startRemarkEdit = (acct: Account) => {
    setEditingRemarkAccountID(acct.id);
    setRemarkDraft(acct.remark || '');
  };

  const saveRemark = async (accountID: number) => {
    if (Array.from(remarkDraft.trim()).length > 80 || isRemarkSaving) return;
    setIsRemarkSaving(true);
    try {
      await api.updateAccountRemark(accountID, remarkDraft);
      await mutateAccounts();
      setEditingRemarkAccountID(null);
      toast.show(remarkDraft.trim() ? '账号备注已更新' : '账号备注已清除', 'success');
    } catch (error: any) {
      toast.show(`更新备注失败: ${error.message}`, 'error');
    } finally {
      setIsRemarkSaving(false);
    }
  };

  // Open fingerprint modal
  const openFpModal = (acct: Account) => {
    setSelectedAccountForFp(acct);
    setFpSeed(acct.fingerprintSeed);
    
    // Parse current JSON config if it exists
    let cfgObj = {
      platform: 'macos',
      brand: 'Chrome',
      brandVersion: '139',
      hardwareConcurrency: 8,
      lang: 'zh-CN',
      acceptLang: 'zh-CN,zh,en',
      timezone: 'Asia/Shanghai',
      gpuVendor: '',
      gpuRenderer: '',
    };
    
    if (acct.fingerprintConfig) {
      try {
        cfgObj = { ...cfgObj, ...JSON.parse(acct.fingerprintConfig) };
      } catch {
        console.warn('[accounts] ignored invalid fingerprint metadata');
      }
    }
    
    setFpPlatform(cfgObj.platform);
    setFpBrand(cfgObj.brand);
    setFpBrandVer(cfgObj.brandVersion);
    setFpCpu(cfgObj.hardwareConcurrency);
    setFpLang(cfgObj.lang);
    setFpAccept(cfgObj.acceptLang);
    setFpTz(cfgObj.timezone);
    setFpGpuVendor(cfgObj.gpuVendor);
    setFpGpuRenderer(cfgObj.gpuRenderer);
    setFpEngine(acct.browserType === 'ego-lite' ? 'chromium' : (acct.browserType || 'chromium'));
    
    setIsFpOpen(true);
  };

  // Save manual fingerprint settings
  const handleSaveFpSubmit = async () => {
    if (!selectedAccountForFp) return;
    const cfgObj = {
      platform: fpPlatform,
      platformVersion: fpPlatform === 'macos' ? '14' : '10',
      brand: fpBrand,
      brandVersion: fpBrandVer,
      hardwareConcurrency: Number(fpCpu),
      gpuVendor: fpGpuVendor,
      gpuRenderer: fpGpuRenderer,
      lang: fpLang,
      acceptLang: fpAccept,
      timezone: fpTz,
    };

    try {
      await api.setFingerprint(selectedAccountForFp.id, fpSeed);
      await api.setFingerprintConfig(selectedAccountForFp.id, JSON.stringify(cfgObj));
      await api.setAccountEngine(selectedAccountForFp.id, fpEngine);
      mutateAccounts();
      setIsFpOpen(false);
      toast.show('指纹环境与绑定的浏览器引擎已更新', 'success');
    } catch (e: any) {
      toast.show(`保存配置失败: ${e.message}`, 'error');
    }
  };

  // Smart fingerprint allocation — 生成后自动刷新表单字段
  const handleSmartFpSubmit = async () => {
    if (!selectedAccountForFp) return;
    toast.show('正在重新生成智能指纹...', 'info');
    try {
      const res = await api.smartFingerprint(selectedAccountForFp.id, fpRegion, fpPlatform);
      // 刷新表单显示最新值
      if (res) {
        if (res.seed) setFpSeed(res.seed);
        if (res.platform) setFpPlatform(res.platform);
        if (res.brand) setFpBrand(res.brand);
        if (res.brandVersion) setFpBrandVer(res.brandVersion);
        if (res.hardwareConcurrency) setFpCpu(res.hardwareConcurrency);
        if (res.lang) setFpLang(res.lang);
        if (res.acceptLang) setFpAccept(res.acceptLang);
        if (res.timezone) setFpTz(res.timezone);
      }
      mutateAccounts();
      toast.show(`智能指纹已生成: ${res.platform || ''} · ${res.timezone || ''}`, 'success');
    } catch (e: any) {
      toast.show(`重新生成失败: ${e.message}`, 'error');
    }
  };

  // Mouse drag select handlers
  const handleMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    const target = e.target as HTMLElement;
    if (target.closest('button') || target.closest('input') || target.closest('select') || target.closest('a')) {
      return;
    }
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    setDragStart({ x, y });
    setDragEnd({ x, y });
    setIsDragging(true);
  };

  const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!isDragging || !dragStart) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    setDragEnd({ x, y });

    const x1 = Math.min(dragStart.x, x);
    const x2 = Math.max(dragStart.x, x);
    const y1 = Math.min(dragStart.y, y);
    const y2 = Math.max(dragStart.y, y);

    const container = e.currentTarget;
    const cards = container.querySelectorAll('[data-account-id]');
    const newSelectedIds: number[] = [];

    cards.forEach((card) => {
      const cardIDs = (card.getAttribute('data-account-ids') || card.getAttribute('data-account-id') || '')
        .split(',')
        .map((value) => Number(value))
        .filter((id) => Number.isInteger(id) && id > 0);
      const cardHtml = card as HTMLElement;
      const cardRect = {
        left: cardHtml.offsetLeft,
        top: cardHtml.offsetTop,
        right: cardHtml.offsetLeft + cardHtml.offsetWidth,
        bottom: cardHtml.offsetTop + cardHtml.offsetHeight,
      };

      const overlap = !(x1 > cardRect.right || x2 < cardRect.left || y1 > cardRect.bottom || y2 < cardRect.top);
      if (overlap) {
        newSelectedIds.push(...cardIDs);
      }
    });

    if (e.shiftKey) {
      const merged = Array.from(new Set([...selectedAccountIds, ...newSelectedIds]));
      setSelectedAccountIds(merged);
    } else {
      setSelectedAccountIds(newSelectedIds);
    }
  };

  const handleMouseUp = () => {
    setIsDragging(false);
    setDragStart(null);
    setDragEnd(null);
  };

  // Meta 等账号可能共享同一 Profile；顶部数量必须与实际关闭的浏览器数一致。
  const runningCount = new Set(
    accounts
      .filter((account) => account.browserRunning)
      .map((account) => account.profileId?.trim() || `account:${account.id}`),
  ).size;

  const unclassifiedCount = accounts.filter((account) => (account.tags ?? []).length === 0).length;

  // 平台分类：code -> category，账号查不到或分类为空时归 other。
  const platformCategoryByCode = React.useMemo(() => {
    const map = new Map<string, AccountPlatformCategory>();
    for (const platform of platforms ?? []) {
      map.set(platform.code, normalizeAccountCategory(platform.category));
    }
    return map;
  }, [platforms]);
  const accountCategoryOf = React.useCallback(
    (account: Account) => platformCategoryByCode.get(account.platformCode) ?? 'other',
    [platformCategoryByCode],
  );
  const categoryCounts = React.useMemo(() => {
    const counts: Record<AccountCategoryFilter, number> = { all: accounts.length, multimedia: 0, article: 0, ecommerce: 0, other: 0 };
    for (const account of accounts) counts[accountCategoryOf(account)] += 1;
    return counts;
  }, [accounts, accountCategoryOf]);

  // Filter + Sort：标签筛选与搜索组合，运行中的排最前。
  const filteredAccounts = (accounts.filter((a) => {
    const q = searchQuery.toLowerCase();
    const matchesSearch =
      a.name.toLowerCase().includes(q) ||
      a.platformCode.toLowerCase().includes(q) ||
      (a.accountIdentifier || '').toLowerCase().includes(q) ||
	  (a.remark || '').toLowerCase().includes(q) ||
	  (a.tags || []).some((tag: AccountTag) => tag.name.toLowerCase().includes(q));
    const matchesTag = activeTagFilter === 'all'
      || (activeTagFilter === 'unclassified'
        ? (a.tags ?? []).length === 0
        : (a.tags ?? []).some((tag) => tag.id === activeTagFilter));
    const matchesCategory = activeCategory === 'all' || accountCategoryOf(a) === activeCategory;
    return matchesSearch && matchesTag && matchesCategory;
  }) ?? []).sort((a, b) => {
    if (a.browserRunning === b.browserRunning) return 0;
    return a.browserRunning ? -1 : 1;
  });
  const accountCards = buildAccountCards(filteredAccounts);

  return (
    <div data-testid="accounts-view" className="space-y-5">
      {/* Search & Toolbars */}
      <div data-testid="accounts-toolbar" className="ui-section flex flex-col gap-3 p-3 lg:flex-row lg:items-center">
        <div className="relative min-w-[220px] flex-1 lg:max-w-md">
          <input
            data-testid="accounts-search"
            type="text"
            aria-label="搜索账号、平台或标签"
            placeholder="搜索账号、平台、标签…"
            value={searchQuery}
            onChange={(event) => setSearchQuery(event.target.value)}
            className="min-h-10 w-full rounded-lg border border-brand-border bg-brand-bg py-2 pl-9 pr-3 text-xs text-gray-700 transition focus:border-brand-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40"
          />
          <Search className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-550" />
        </div>

        <div className="flex flex-wrap items-center gap-2 lg:ml-auto lg:justify-end">
          <span
            data-testid="account-observer-status"
            role="status"
            aria-live="polite"
            className={`flex min-h-10 items-center gap-1.5 rounded-lg border bg-brand-bg px-3 text-[10.5px] font-semibold ${observerStatus?.lastErrorCode ? 'border-brand-warning/30/35 text-brand-warning' : 'border-brand-border text-gray-600'}`}
            title={`上次完成：${formatObservedAt(observerStatus?.lastFinishedAt)}${observerStatus?.lastErrorCode ? `；${observerErrorLabels[observerStatus.lastErrorCode] || observerStatus.lastErrorCode}` : ''}`}
          >
            <span
              data-testid="account-observer-indicator"
              aria-hidden="true"
              className={`h-1.5 w-1.5 rounded-full ${observerStatus?.observing ? 'bg-brand-warning/20' : observerStatus?.lastErrorCode ? 'bg-brand-warning/20' : observerStatus?.running ? 'bg-emerald-500' : 'bg-gray-500'}`}
            />
            {observerStatus?.observing ? '监测中' : observerStatus?.lastErrorCode ? '监测异常' : '监测正常'}
            {runningCount > 0 ? <span className="text-gray-400">·</span> : null}
            {runningCount > 0 ? <span data-testid="running-browser-indicator">{runningCount} 个运行中</span> : null}
            <span className="sr-only">上次完成：{formatObservedAt(observerStatus?.lastFinishedAt)}</span>
          </span>

          <button
            type="button"
            data-testid="observe-all-accounts"
            onClick={handleObserveAll}
            disabled={isObservingAll || observerStatus?.observing}
            className="flex min-h-10 items-center justify-center gap-1.5 rounded-lg border border-emerald-500/25 px-3.5 text-xs font-bold text-emerald-600 transition hover:bg-emerald-500/10 disabled:cursor-wait disabled:opacity-60"
            title="立即串行读取已打开平台页面的昵称、账号头像与登录证据；不会发布内容"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${isObservingAll ? 'animate-spin' : ''}`} />
            {isObservingAll || observerStatus?.observing ? '监测中' : '检测'}
          </button>

          {runningCount > 0 ? (
            <button
              type="button"
              data-testid="account-batch-close-running"
              onClick={() => void handleBatchStopBrowsers(accounts.map((account) => account.id), false)}
              disabled={isBatchClosing}
              className="flex min-h-10 items-center justify-center gap-1.5 rounded-lg border border-red-500/25 px-3.5 text-xs font-bold text-red-600 transition hover:bg-red-500/10 disabled:cursor-wait disabled:opacity-50"
              title="关闭全部正在运行的账号浏览器；关闭前会再次确认，Profile 和登录数据会保留"
            >
              <Square className="h-3.5 w-3.5 fill-current" />
              {isBatchClosing ? '关闭中…' : `关闭运行中（${runningCount}）`}
            </button>
          ) : null}

          <details
            data-testid="accounts-more-menu"
            className="group relative"
            onBlur={(event) => {
              if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
                event.currentTarget.removeAttribute('open');
              }
            }}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                event.currentTarget.removeAttribute('open');
                event.currentTarget.querySelector('summary')?.focus();
              }
            }}
          >
            <summary
              data-testid="accounts-more-menu-trigger"
              className="flex min-h-10 cursor-pointer list-none items-center justify-center gap-1.5 rounded-lg border border-brand-border px-3.5 text-xs font-bold text-gray-700 transition hover:bg-black/5 focus-visible:ring-2 focus-visible:ring-brand-accent/40 [&::-webkit-details-marker]:hidden"
            >
              <MoreHorizontal className="h-4 w-4 text-brand-accent" />
              更多
            </summary>
            <div className="absolute right-0 z-30 mt-2 w-52 overflow-hidden rounded-xl border border-brand-border bg-brand-surface p-1.5 shadow-xl">
              <div data-testid="account-batch-select-entry" title="批量管理账号">
                <button
                  type="button"
                  data-testid="account-batch-tags-entry"
                  aria-label="批量标签"
                  aria-pressed={isBatchSelecting}
                  onClick={(event) => {
                    event.currentTarget.closest('details')?.removeAttribute('open');
                    setIsBatchSelecting(true);
                  }}
                  className="flex min-h-10 w-full items-center gap-2 rounded-md px-3 text-left text-xs font-semibold text-gray-700 hover:bg-black/5"
                  title="先搜索或筛选账号，再一键选择当前结果"
                >
                  <Tags className="h-4 w-4 text-brand-accent" />
                  批量标签
                </button>
              </div>
              <button
                type="button"
                data-testid="cdp-monitor-entry"
                onClick={(event) => {
                  event.currentTarget.closest('details')?.removeAttribute('open');
                  setActiveTab('fleet');
                }}
                className="flex min-h-10 w-full items-center gap-2 rounded-lg px-3 text-left text-xs font-semibold text-gray-700 hover:bg-black/5"
                title="打开本机 CDP 浏览器状态看板；只读监控，不会操作浏览器"
              >
                <MonitorDot className="h-4 w-4 text-brand-accent" />
                浏览器状态
              </button>
              {runningCount > 0 ? (
                <button
                  type="button"
                  onClick={(event) => {
                    event.currentTarget.closest('details')?.removeAttribute('open');
                    handleLayoutBrowsers();
                  }}
                  className="flex min-h-10 w-full items-center gap-2 rounded-lg px-3 text-left text-xs font-semibold text-gray-700 hover:bg-black/5"
                  title="将所有打开的指纹浏览器窗口在桌面网格平铺"
                >
                  <LayoutGrid className="h-4 w-4 text-brand-accent" />
                  分屏平铺
                </button>
              ) : null}
              <button
                type="button"
                data-testid="account-tags-entry"
                onClick={(event) => {
                  event.currentTarget.closest('details')?.removeAttribute('open');
                  openTagManager();
                }}
                className="flex min-h-10 w-full items-center gap-2 rounded-lg px-3 text-left text-xs font-semibold text-gray-700 hover:bg-black/5"
                title="创建标签、批量追加/移除/替换，并按标签选择账号"
              >
                <Tags className="h-4 w-4 text-brand-accent" />
                标签管理
              </button>
              <button
                type="button"
                data-testid="account-session-transfer-entry"
                onClick={(event) => {
                  event.currentTarget.closest('details')?.removeAttribute('open');
                  openSessionTransfer();
                }}
                className="flex min-h-10 w-full items-center gap-2 rounded-lg px-3 text-left text-xs font-semibold text-gray-700 hover:bg-black/5"
                title="创建本机加密会话备份，或导出到另一台设备；不导出密码"
              >
                <Download className="h-4 w-4 text-brand-accent" />
                账号迁移
              </button>
            </div>
          </details>

          <button
            data-testid="accounts-add-btn"
            onClick={() => setIsAddOpen(true)}
            className="ui-btn ui-btn-primary min-h-10 px-4 text-xs"
          >
            <Plus className="h-4 w-4" />
            添加账号
          </button>
        </div>
      </div>

      <div
        data-testid="account-tag-filters"
        className="-mt-2 flex min-h-11 items-center gap-2 overflow-x-auto border-y border-brand-border px-1 py-2"
        aria-label="按账号标签筛选"
      >
        <button
          type="button"
          data-testid="account-tag-filter-all"
          aria-pressed={activeTagFilter === 'all'}
          onClick={() => setActiveTagFilter('all')}
          className={`shrink-0 rounded-full border px-3 py-1.5 text-[11px] font-semibold transition ${
            activeTagFilter === 'all'
              ? 'border-brand-accent bg-brand-accent text-brand-on-accent'
              : 'border-brand-border text-gray-650 hover:border-brand-accent/50'
          }`}
        >
          全部 <span className="opacity-70">{accounts.length}</span>
        </button>
        {orderedAccountTags.map((tag) => (
          <button
            key={tag.id}
            type="button"
            data-testid={`account-tag-filter-${tag.id}`}
            aria-pressed={activeTagFilter === tag.id}
            onClick={() => setActiveTagFilter(tag.id)}
            className={`shrink-0 rounded-full border px-3 py-1.5 text-[11px] font-semibold transition ${
              activeTagFilter === tag.id ? 'bg-black/5 ring-1 ring-current' : 'opacity-80 hover:opacity-100'
            }`}
            style={{ borderColor: tag.color, color: tag.color }}
          >
            {tag.name} <span className="opacity-65">{tag.accountCount}</span>
          </button>
        ))}
        <button
          type="button"
          data-testid="account-tag-filter-unclassified"
          aria-pressed={activeTagFilter === 'unclassified'}
          onClick={() => setActiveTagFilter('unclassified')}
          className={`shrink-0 rounded-full border px-3 py-1.5 text-[11px] font-semibold transition ${
            activeTagFilter === 'unclassified'
              ? 'border-gray-700 bg-gray-800 text-white'
              : 'border-brand-border text-gray-600 hover:border-gray-500'
          }`}
        >
          未分类 <span className="opacity-65">{unclassifiedCount}</span>
        </button>
        <span className="min-w-2 flex-1" aria-hidden="true" />
        <button
          type="button"
          data-testid="account-tag-manage-inline"
          onClick={openTagManager}
          className="ui-btn min-h-8 shrink-0 px-3 text-[11px]"
        >
          <Tags className="h-3.5 w-3.5" />
          管理标签
        </button>
      </div>

      <div
        data-testid="account-category-filters"
        className="-mt-2 flex min-h-10 items-center gap-2 overflow-x-auto border-b border-brand-border px-1 py-2"
        aria-label="按平台分类筛选"
      >
        {ACCOUNT_CATEGORY_OPTIONS.map((option) => (
          <button
            key={option.key}
            type="button"
            data-testid={`account-category-filter-${option.key}`}
            aria-pressed={activeCategory === option.key}
            onClick={() => setActiveCategory(option.key)}
            className={`shrink-0 rounded-full border px-3 py-1.5 text-[11px] font-semibold transition ${
              activeCategory === option.key
                ? 'border-brand-accent bg-brand-accent text-brand-on-accent'
                : 'border-brand-border text-gray-650 hover:border-brand-accent/50'
            }`}
          >
            {option.label} <span className="opacity-70">{categoryCounts[option.key]}</span>
          </button>
        ))}
        {categoryCounts.other > 0 && (
          <button
            type="button"
            data-testid="account-category-filter-other"
            aria-pressed={activeCategory === 'other'}
            onClick={() => setActiveCategory('other')}
            className={`shrink-0 rounded-full border px-3 py-1.5 text-[11px] font-semibold transition ${
              activeCategory === 'other'
                ? 'border-gray-700 bg-gray-800 text-white'
                : 'border-brand-border text-gray-600 hover:border-gray-500'
            }`}
          >
            其他 <span className="opacity-65">{categoryCounts.other}</span>
          </button>
        )}
        <span className="min-w-2 flex-1" aria-hidden="true" />
        <span className="shrink-0 text-[11px] text-gray-500">
          {/* 分类计数固定为全量账号，不受搜索影响 */}
          {activeCategory === 'all' ? `共 ${accounts.length} 个账号` : `${categoryCounts[activeCategory]} 个账号`}
        </span>
      </div>

      {isBatchSelecting ? (
        <div
          data-testid="account-batch-tag-selection"
          className="-mt-3 flex flex-col gap-3 rounded-xl border border-brand-accent/25 bg-brand-accent/5 px-3 py-3 sm:flex-row sm:items-center"
        >
          <div className="min-w-0 flex-1">
            <p className="text-xs font-bold text-gray-800">批量标签</p>
            <p className="mt-0.5 text-[10px] text-gray-500" role="status" aria-live="polite">
              当前筛选 {filteredAccounts.length} 个 · 已选择 {selectedAccountIds.length} 个
              {selectedAccountIds.length > filteredAccounts.filter((account) => selectedAccountIds.includes(account.id)).length
                ? `（当前可见 ${filteredAccounts.filter((account) => selectedAccountIds.includes(account.id)).length} 个）`
                : ''}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              data-testid="select-filtered-accounts-for-tags"
              onClick={() => setSelectedAccountIds(filteredAccounts.map((account) => account.id))}
              disabled={filteredAccounts.length === 0}
              aria-controls="accounts-list"
              className="ui-btn min-h-9 px-3 text-[11px] disabled:opacity-40"
            >
              选择当前筛选（{filteredAccounts.length}）
            </button>
            <button
              type="button"
              onClick={clearSelection}
              disabled={selectedAccountIds.length === 0}
              className="ui-btn min-h-9 px-3 text-[11px] disabled:opacity-40"
            >
              清空
            </button>
            <button
              type="button"
              onClick={openBulkTagEditor}
              disabled={selectedAccountIds.length === 0}
              className="ui-btn ui-btn-primary min-h-9 px-3 text-[11px] disabled:opacity-40"
            >
              设置标签（{selectedAccountIds.length}）
            </button>
            <button
              type="button"
              onClick={exitBatchSelection}
              className="min-h-9 px-2 text-[11px] font-semibold text-gray-500 hover:text-gray-800"
            >
              退出
            </button>
          </div>
        </div>
      ) : null}

      {/* Grid Accounts */}
      {accountsError && !accountsRaw ? (
        <div data-testid="accounts-error" className="flex flex-col items-center justify-center rounded-xl border border-red-200 bg-red-50/70 py-16 text-center text-red-700">
          <Info className="mb-3 h-8 w-8 opacity-70" />
          <p className="text-sm font-semibold">账号列表加载失败</p>
          <p className="mt-1 text-xs text-red-600">本地服务暂时不可用，已有账号数据不会丢失。</p>
          <button
            type="button"
            onClick={() => void mutateAccounts()}
            className="ui-btn mt-4 border-red-200 bg-white text-red-700"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            重新加载
          </button>
        </div>
      ) : !accountsRaw ? (
        <div data-testid="accounts-loading" className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 animate-pulse">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="h-44 rounded-xl border border-brand-border bg-brand-surface" />
          ))}
        </div>
      ) : accountCards.length === 0 ? (
        <div data-testid="accounts-empty" className="py-20 flex flex-col items-center justify-center border border-dashed border-brand-border rounded-xl bg-brand-surface/40 text-gray-500">
          <Globe className="w-12 h-12 opacity-10 mb-4" />
          <p className="text-xs font-medium">
            {activeTagFilter === 'unclassified'
              ? '未分类中暂无账号'
              : typeof activeTagFilter === 'number'
                ? `标签「${orderedAccountTags.find((tag) => tag.id === activeTagFilter)?.name ?? ''}」中暂无账号`
                : '没有找到符合条件的社交账号'}
          </p>
          <button
            onClick={() => setIsAddOpen(true)}
            className="mt-3 text-[11px] font-semibold text-brand-accent hover:underline"
          >
            立即添加一个
          </button>
        </div>
      ) : (
        <div 
          className="relative select-none pb-24"
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onMouseLeave={handleMouseUp}
        >
          {isDragging && dragStart && dragEnd && (
            <div
              style={{
                position: 'absolute',
                left: Math.min(dragStart.x, dragEnd.x),
                top: Math.min(dragStart.y, dragEnd.y),
                width: Math.abs(dragStart.x - dragEnd.x),
                height: Math.abs(dragStart.y - dragEnd.y),
                backgroundColor: 'rgba(0, 0, 0, 0.08)',
                border: '1.5px solid rgba(0, 0, 0, 0.22)',
                borderRadius: '6px',
                pointerEvents: 'none',
                zIndex: 10,
              }}
            />
          )}

          <div id="accounts-list" data-testid="accounts-list" className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {accountCards.map(({ primary: acct, members: cardMembers, isMeta: isMetaCard }) => {
            const platformName = isMetaCard ? 'Meta' : (platforms?.find((p) => p.code === acct.platformCode)?.name || acct.platformCode);
            const accountIdentifier = displayAccountIdentifier(acct);
            const cardAccountIDs = cardMembers.map((account) => account.id);
            const accountSelected = cardAccountIDs.every((id) => selectedAccountIds.includes(id));
			const actionHint = accountActionHint(acct);
			const metaAssets = isMetaCard
			  ? (accountAssets ?? []).filter((asset) =>
			      cardAccountIDs.includes(asset.account_id) &&
			      (asset.platform_code === 'facebook' || asset.platform_code === 'instagram') &&
			      asset.asset_type !== 'browser_account')
			  : [];
            const toggleCardSelection = () => {
              if (accountSelected) {
                setSelectedAccountIds(selectedAccountIds.filter((id) => !cardAccountIDs.includes(id)));
              } else {
                setSelectedAccountIds([...new Set([...selectedAccountIds, ...cardAccountIDs])]);
              }
            };
            return (
              <div
                key={acct.id}
                data-account-id={acct.id}
                data-account-ids={cardAccountIDs.join(',')}
                data-testid={`account-card-${acct.id}`}
                onClick={(e) => {
                  const target = e.target as HTMLElement;
                  if (target.closest('button') || target.closest('a') || target.closest('input')) return;
                  toggleCardSelection();
                }}
                className={`ui-card-flat p-4 flex flex-col justify-between transition-colors duration-150 relative group cursor-pointer ${
                  accountSelected
                    ? 'ring-2 ring-[color-mix(in_srgb,var(--brand-accent)_45%,transparent)] border-[var(--brand-accent)]'
                    : acct.browserRunning
                    ? 'border-[color-mix(in_srgb,var(--brand-accent)_40%,var(--brand-border))]'
                    : 'hover:border-[color-mix(in_srgb,var(--brand-fg)_20%,var(--brand-border))]'
                }`}
              >
                {/* Card Header */}
                <div className="flex items-start justify-between gap-4">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="relative flex-shrink-0">
                      <label
                        data-testid={`account-selection-${acct.id}`}
                        title={accountSelected ? '取消选择账号' : '选择账号'}
                        onClick={(event) => event.stopPropagation()}
                        className={`absolute left-0 top-0 -translate-x-1/2 -translate-y-1/2 z-10 w-4 h-4 rounded-full border flex items-center justify-center cursor-pointer transition ${
                          isBatchSelecting || accountSelected
                            ? 'opacity-100'
                            : 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100'
                        } ${
                          accountSelected
                            ? 'border-brand-accent bg-brand-accent text-brand-on-accent'
                            : 'border-brand-border bg-brand-surface text-transparent hover:border-brand-accent'
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={accountSelected}
                          onChange={toggleCardSelection}
                          aria-label={`选择账号 ${acct.name}`}
                          className="absolute inset-0 z-10 h-full w-full cursor-pointer opacity-0"
                        />
                        <Check className={`pointer-events-none w-2.5 h-2.5 ${accountSelected ? 'opacity-100' : 'opacity-0'}`} aria-hidden="true" />
                      </label>
                      {isMetaCard ? (
						<div data-testid={`meta-identities-${acct.id}`} className="min-w-[9.5rem]">
						  <div data-testid={`meta-asset-identities-${acct.id}`} className="grid gap-1.5">
							{metaAssets.length ? metaAssets.map((asset) => (
							  <div key={asset.id} className="flex min-w-0 items-center gap-2">
								<AccountAvatar src={asset.avatar} platform={asset.platform_code} size="sm" alt={`${asset.name} 的账号头像`} />
								<span className="min-w-0">
								  <span className="block text-[9px] font-semibold text-gray-700">{asset.platform_code === 'facebook' ? 'Facebook' : 'Instagram'}</span>
								  <span className="block max-w-24 truncate text-[9px] text-gray-500" title={asset.name}>{asset.name}</span>
								</span>
							  </div>
							)) : (
							  <AccountAvatar src={acct.avatar} platform="meta" size="lg" alt={`${acct.name} 的账号头像`} />
							)}
						  </div>
						</div>
                      ) : (
                        <AccountAvatar
                          src={acct.avatar}
                          platform={acct.platformCode}
                          size="lg"
                          alt={`${acct.name} 的账号头像`}
                        />
                      )}
                    </div>
                    <div className="min-w-0">
                      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                        <h4 className="min-w-0 truncate text-xs font-bold text-gray-900" title={acct.name}>
                          {acct.name}
                        </h4>
                        {editingRemarkAccountID === acct.id ? (
                          <form
                            className="flex min-w-0 items-center gap-1"
                            onClick={(event) => event.stopPropagation()}
                            onSubmit={(event) => {
                              event.preventDefault();
                              void saveRemark(acct.id);
                            }}
                          >
                            <input
                              autoFocus
                              value={remarkDraft}
                              maxLength={80}
                              aria-label={`${acct.name} 的账号备注`}
                              onChange={(event) => setRemarkDraft(event.target.value)}
                              onKeyDown={(event) => {
                                if (event.key === 'Escape') setEditingRemarkAccountID(null);
                              }}
                              className="w-28 rounded-full border border-brand-accent/50 bg-brand-bg px-2 py-0.5 text-[9px] text-gray-700 outline-none"
                            />
                            <button
                              type="submit"
                              disabled={isRemarkSaving}
                              className="text-[9px] font-semibold text-brand-accent disabled:opacity-50"
                            >
                              保存
                            </button>
                          </form>
                        ) : (
                          <button
                            type="button"
                            data-testid={`account-remark-${acct.id}`}
                            onClick={(event) => {
                              event.stopPropagation();
                              startRemarkEdit(acct);
                            }}
                            className="max-w-32 truncate rounded-full border border-brand-border bg-brand-bg px-2 py-0.5 text-[9px] font-medium text-gray-500 hover:border-brand-accent hover:text-brand-accent"
                            title={acct.remark || '添加账号备注'}
                          >
                            {acct.remark || '添加备注'}
                          </button>
                        )}
                      </div>
                      {accountIdentifier && (
                        <p className="text-[9.5px] text-gray-500 font-mono mt-0.5 truncate" title={`平台用户ID: ${accountIdentifier}`}>
                          ID: {accountIdentifier}
                        </p>
                      )}
                      <p className="text-[10px] text-gray-400 mt-0.5 flex items-center gap-1.5">
						<span className="font-semibold text-gray-600">{platformName}</span>
                        <span className="w-1 h-1 rounded-full bg-gray-600" />
                        <span
                          data-testid={`account-login-indicator-${acct.id}`}
                          aria-hidden="true"
                          className={`h-2 w-2 shrink-0 rounded-full ${
                            acct.loginState === 'logged_in'
                              ? 'bg-emerald-500'
                              : acct.loginState === 'logged_out'
                              ? 'bg-red-500'
                              : 'bg-brand-warning/20'
                          }`}
                        />
                        <span className={`font-semibold ${
                          acct.loginState === 'logged_in'
                            ? 'text-emerald-400'
                            : acct.loginState === 'logged_out'
                            ? 'text-red-400'
                            : 'text-brand-warning'
                        }`}>
                          {loginStateLabel(acct.loginState)}
                        </span>
                      </p>
                      {isMetaCard && cardMembers.length > 1 ? (
                        <p className="mt-1 flex flex-wrap gap-1 text-[9px] text-gray-500">
                          {cardMembers.map((member) => (
                            <button
                              key={member.id}
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation();
                                void handleFocusBrowser(member.id);
                              }}
                              className="rounded-full border border-brand-border px-1.5 py-0.5 hover:border-brand-accent"
                              title={`打开 ${member.platformCode} 会话`}
                            >
                              {member.platformCode === 'facebook' ? 'Facebook' : 'Instagram'} · {loginStateLabel(member.loginState)}
                            </button>
                          ))}
                        </p>
                      ) : null}
                    </div>
                  </div>

                  {/* Actions Header */}
                  <div className="flex items-center gap-1.5">
                    {acct.browserRunning ? (
                      <>
                        <button
                          onClick={() => handleStopBrowser(acct.id)}
                          title="关闭浏览器"
                          aria-label={`关闭 ${acct.name} 的浏览器`}
                          className="w-7 h-7 rounded-lg border border-brand-border hover:border-red-500 hover:bg-red-500/10 text-red-400 flex items-center justify-center transition"
                        >
                          <Square className="w-3.5 h-3.5 fill-red-400/20" />
                        </button>
                        <button
                          onClick={() => handleFocusBrowser(acct.id)}
                          title="聚焦并置顶浏览器窗口"
                          aria-label={`打开 ${acct.name} 的浏览器窗口`}
                          className="w-7 h-7 rounded-lg border border-brand-border hover:border-brand-accent hover:bg-brand-accent/15 text-brand-accent flex items-center justify-center transition"
                        >
                          <ExternalLink className="w-3.5 h-3.5" />
                        </button>
                      </>
                    ) : (
                      <button
                        onClick={() => handleStartBrowser(acct.id)}
                        title="启动账号浏览器"
                        aria-label={`启动 ${acct.name} 的账号浏览器`}
                        className="w-7 h-7 rounded-lg border border-brand-border hover:border-brand-accent hover:bg-brand-accent/15 text-brand-muted flex items-center justify-center transition"
                      >
                        <Play className="w-3.5 h-3.5 fill-current" />
                      </button>
                    )}

                    <button
                      onClick={() => handleDeleteAccount(acct.id)}
                      title="删除该账号"
                      aria-label={`删除账号 ${acct.name}`}
                      className="w-7 h-7 rounded-lg border border-brand-border hover:border-red-500 hover:bg-red-500/10 text-gray-400 hover:text-red-400 flex items-center justify-center transition"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>

                {actionHint ? (
                  <p
                    data-testid={`account-action-hint-${acct.id}`}
                    className={`mt-3 rounded-lg border px-3 py-2 text-xs leading-5 ${
                      acct.loginState === 'logged_out'
                        ? 'border-red-500/20 bg-red-500/5 text-red-600'
                        : 'border-brand-warning/30/20 bg-brand-warning/20/5 text-brand-warning'
                    }`}
                  >
                    {actionHint}
                  </p>
                ) : null}

                {/* The whole tag row is the edit affordance, so a card never needs a hidden menu. */}
                <button
                  type="button"
                  data-testid={`account-tags-edit-${acct.id}`}
                  onClick={() => {
                    if (isBatchSelecting) {
                      toggleCardSelection();
                      return;
                    }
                    openSingleTagEditor(acct);
                  }}
                  aria-label={isBatchSelecting ? `选择账号 ${acct.name}` : `修改 ${acct.name} 的账号标签`}
                  className="mt-3 w-full rounded-lg border border-brand-border/40 bg-brand-bg/50 px-3 py-2 text-left text-xs text-gray-550 transition hover:border-brand-accent/40 hover:bg-brand-accent/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40"
                >
                  <span className="mb-1 flex items-center justify-between gap-3">
                    <span className="text-[9.5px] font-semibold uppercase tracking-wider text-gray-500">账号标签</span>
                    <span className="flex items-center gap-1 text-[9px] font-semibold text-brand-accent opacity-75">
                      <Tags className="h-3 w-3" aria-hidden="true" />
                      {isBatchSelecting ? '选择账号' : '点击修改'}
                    </span>
                  </span>
                  {(acct.tags || []).length > 0 ? (
                    <span data-testid={`account-tags-${acct.id}`} className="flex flex-wrap gap-1">
                      {acct.tags.map((tag) => (
                        <span key={tag.id} className="rounded-full border px-1.5 py-0.5 text-[9px] font-semibold" style={{ borderColor: tag.color, color: tag.color }}>
                          {tag.name}
                        </span>
                      ))}
                    </span>
                  ) : (
                    <span data-testid={`account-tags-${acct.id}`} className="flex flex-wrap gap-1">
                      <span className="rounded-full border border-dashed border-gray-400/60 px-1.5 py-0.5 text-[9px] font-semibold text-gray-500">
                        未分类
                      </span>
                    </span>
                  )}
                </button>

                {/* Card Footer Actions */}
                <div className="mt-4 pt-3 border-t border-brand-border flex flex-wrap items-center justify-between text-[11px] gap-2">
                  <button
                    onClick={() => openFpModal(acct)}
                    className="flex items-center gap-1 text-[10px] text-brand-muted hover:underline font-semibold"
                  >
                    <Fingerprint className="w-3.5 h-3.5" />
                    指纹配置
                  </button>

                  <button
                    type="button"
                    onClick={() => openLoginHistory(acct)}
                    className="flex items-center gap-1 text-[10px] text-gray-500 hover:text-gray-800 font-semibold"
                  >
                    <History className="w-3.5 h-3.5" />
                    状态记录
                  </button>

                  <button
                    onClick={() => handleVerifyAccount(acct.id)}
                    disabled={!acct.browserRunning || observingAccountID !== null}
                    className={`flex items-center gap-1 text-[10px] font-semibold ${
                      acct.browserRunning && observingAccountID === null
                        ? 'text-emerald-400 hover:underline' 
                        : 'text-gray-500 cursor-not-allowed opacity-60'
                    }`}
                  >
                    <RefreshCw className={`w-3 h-3 ${observingAccountID === acct.id ? 'animate-spin' : ''}`} />
                    {observingAccountID === acct.id ? '检测中...' : '检测登录态'}
                  </button>
                </div>
              </div>
            );
          })}
        </div>

        {/* Batch Action Bar */}
        {selectedAccountIds.length > 0 && !isBatchSelecting && (
          <div data-testid="account-batch-actions" className="fixed bottom-3 left-3 right-3 z-40 flex flex-wrap items-center gap-3 rounded-2xl border border-brand-border bg-brand-surface/95 px-4 py-3 shadow-2xl backdrop-blur-md animate-slide-up sm:bottom-6 sm:left-1/2 sm:right-auto sm:-translate-x-1/2 sm:flex-nowrap sm:rounded-full sm:px-5">
            <span className="text-xs font-semibold text-gray-700" role="status" aria-live="polite">
              已选择 <strong className="text-brand-accent">{selectedAccountIds.length}</strong> 个账号
            </span>
            <span className="w-px h-4 bg-brand-border" />
            <div className="flex flex-1 flex-wrap items-center gap-2 sm:flex-none sm:flex-nowrap">
              <button
                onClick={async () => {
                  toast.show(`正在并发启动 ${selectedAccountIds.length} 个指纹浏览器...`, 'info');
                  const results = await Promise.allSettled(
                    selectedAccountIds.map((id) => api.startBrowser(id))
                  );
                  const count = results.filter((r) => r.status === 'fulfilled').length;
                  refreshAll();
                  exitBatchSelection();
                  toast.show(`已成功启动 ${count} / ${selectedAccountIds.length} 个浏览器`, count > 0 ? 'success' : 'error');
                }}
                className="flex items-center gap-1 px-3 py-1.5 text-[11px] font-bold text-brand-on-accent bg-brand-accent hover:bg-brand-accent-hover rounded-full transition shadow-md shadow-brand-accent/10 cursor-pointer"
              >
                <Play className="w-3 h-3 fill-current" />
                批量打开
              </button>
              <button
                onClick={() => void handleBatchStopBrowsers(selectedAccountIds, true)}
                disabled={isBatchClosing}
                className="flex items-center gap-1 px-3 py-1.5 text-[11px] font-bold text-red-500 hover:bg-red-500/10 border border-red-500/20 rounded-full transition cursor-pointer disabled:cursor-wait disabled:opacity-50"
              >
                <Square className="w-3 h-3 fill-current" />
                {isBatchClosing ? '关闭中…' : '批量关闭'}
              </button>
              <button
                onClick={async () => {
                  const running = selectedAccountIds.filter(
                    (id) => accounts.find((a) => a.id === id)?.browserRunning
                  );
                  if (running.length === 0) {
                    toast.show('所选账号中没有正在运行的浏览器', 'error');
                    return;
                  }
                  toast.show(`正在串行检测 ${running.length} 个账号的登录态...`, 'info');
                  const observations = [];
                  for (const id of running) {
                    try {
                      observations.push(await api.observeAccount(id));
                    } catch {
                      // Continue serially so one unavailable browser cannot hide the other account results.
                    }
                  }
                  const loggedIn = observations.filter((item) => item.authState === 'logged_in').length;
				  const manual = observations.filter((item) => item.authState === 'unknown').length;
                  const loggedOut = observations.filter((item) => item.authState === 'logged_out').length;
                  refreshAll();
                  exitBatchSelection();
				  toast.show(`检测完成：${loggedIn} 个已登录，${loggedOut} 个掉线，${manual} 个待确认`, loggedOut > 0 ? 'error' : 'success');
                }}
                className="flex items-center gap-1 px-3 py-1.5 text-[11px] font-bold text-emerald-500 hover:bg-emerald-500/10 border border-emerald-500/20 rounded-full transition cursor-pointer"
              >
                <RefreshCw className="w-3 h-3" />
                批量检测
              </button>
              <button
                type="button"
                onClick={openBulkTagEditor}
                className="flex items-center gap-1 rounded-full border border-brand-accent/20 px-3 py-1.5 text-[11px] font-bold text-brand-accent transition hover:bg-brand-accent/10"
              >
                <Tags className="h-3 w-3" />
                设置标签
              </button>
			  <button
                onClick={exitBatchSelection}
				className="px-3 py-1.5 text-[11px] font-bold text-gray-600 hover:bg-black/5 rounded-full transition cursor-pointer"
			  >
				取消（Esc）
			  </button>
            </div>
          </div>
        )}
      </div>
      )}

      {isTagsOpen && (
        <div
          data-testid="account-tags-modal"
          role="dialog"
          aria-modal="true"
          aria-labelledby="account-tags-modal-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-xs animate-fade-in"
        >
          <div className="flex max-h-[90vh] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-brand-border bg-brand-surface shadow-2xl">
            <div className="flex items-center justify-between gap-4 border-b border-brand-border p-4">
              <div className="min-w-0">
                <h4 id="account-tags-modal-title" className="truncate text-sm font-bold text-gray-900">
                  {tagEditorMode === 'single'
                    ? '修改账号标签'
                    : tagEditorMode === 'bulk'
                      ? '批量设置标签'
                      : '账号标签管理'}
                </h4>
                <p className="mt-0.5 truncate text-[10px] text-gray-500">
                  {tagEditorMode === 'single'
                    ? `${tagEditorAccount?.name ?? '当前账号'} · 点击标签即可切换`
                    : tagEditorMode === 'bulk'
                      ? `将修改 ${selectedAccountIds.length} 个账号`
                      : '拖动优先级用上下按钮调整 · 新账号默认未分类'}
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  setIsTagsOpen(false);
                  setTagEditorAccountID(null);
                }}
                aria-label="关闭标签窗口"
                className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-gray-455 transition hover:bg-black/5 hover:text-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="space-y-4 overflow-y-auto p-5">
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_auto]">
                <label className="sr-only" htmlFor="new-account-tag-name">新标签名称</label>
                <input
                  id="new-account-tag-name"
                  data-testid="account-tag-create-input"
                  value={newTagName}
                  onChange={(event) => setNewTagName(event.target.value)}
                  maxLength={40}
                  placeholder="新标签名称"
                  className="rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs text-gray-700"
                />
                <input
                  type="color"
                  value={newTagColor}
                  onChange={(event) => setNewTagColor(event.target.value.toUpperCase())}
                  aria-label="标签颜色"
                  className="h-9 w-11 rounded-lg border border-brand-border bg-brand-bg p-1"
                />
                <button
                  type="button"
                  data-testid="account-tag-create-submit"
                  disabled={isTagSaving || !newTagName.trim()}
                  onClick={handleCreateTag}
                  className="ui-btn ui-btn-primary px-3 text-xs disabled:opacity-50"
                >
                  {tagEditorMode === 'manage' ? '新建' : '创建并选中'}
                </button>
              </div>

              <div>
                <div className="mb-2">
                  <p className="text-[10px] font-bold uppercase tracking-wider text-gray-500">
                    {tagEditorMode === 'manage' ? '标签与显示顺序' : '选择标签'}
                  </p>
                  <p className="mt-0.5 text-[10px] text-gray-400">
                    {tagEditorMode === 'manage'
                      ? '排在前面的标签会优先出现在筛选栏和账号卡片。'
                      : '不选任何标签并保存，可将账号设为未分类。'}
                  </p>
                </div>
                <div className="min-h-12 space-y-2 rounded-lg border border-brand-border p-2">
                  {orderedAccountTags.map((tag, index) => {
                    const selected = selectedTagIds.includes(tag.id);
                    return (
                      <div key={tag.id} data-testid={`account-tag-order-${tag.id}`} className="flex min-h-10 items-center gap-2 rounded-lg border border-brand-border bg-brand-bg/45 p-1.5">
                        {tagEditorMode === 'manage' ? (
                          <div className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-[11px] font-semibold" style={{ color: tag.color }}>
                            <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: tag.color }} aria-hidden="true" />
                            <span className="truncate">{tag.name}</span>
                            <span className="ml-auto shrink-0 text-[10px] text-gray-400">{tag.accountCount} 个账号</span>
                          </div>
                        ) : (
                          <button
                            type="button"
                            aria-pressed={selected}
                            onClick={() => toggleTag(tag.id)}
                            className={`flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left text-[11px] font-semibold transition ${
                              selected ? 'bg-black/5 ring-1 ring-current' : 'hover:bg-black/5'
                            }`}
                            style={{ color: tag.color }}
                          >
                            <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: tag.color }} aria-hidden="true" />
                            <span className="truncate">{tag.name}</span>
                            <span className="ml-auto shrink-0 text-[10px] text-gray-400">{selected ? '已选择' : `${tag.accountCount} 个账号`}</span>
                          </button>
                        )}
                        {tagEditorMode === 'manage' ? (
                          <div className="flex shrink-0 items-center gap-1">
                            <button type="button" aria-label={`上移标签 ${tag.name}`} disabled={isTagSaving || index === 0} onClick={() => void moveAccountTag(tag.id, -1)} className="flex h-8 w-8 items-center justify-center rounded-md border border-brand-border text-gray-500 transition hover:text-brand-accent disabled:opacity-25">
                              <ChevronUp className="h-3.5 w-3.5" />
                            </button>
                            <button type="button" aria-label={`下移标签 ${tag.name}`} disabled={isTagSaving || index === orderedAccountTags.length - 1} onClick={() => void moveAccountTag(tag.id, 1)} className="flex h-8 w-8 items-center justify-center rounded-md border border-brand-border text-gray-500 transition hover:text-brand-accent disabled:opacity-25">
                              <ChevronDown className="h-3.5 w-3.5" />
                            </button>
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                  {orderedAccountTags.length === 0 ? (
                    <span className="block p-2 text-xs text-gray-400">
                      {tagEditorMode === 'manage' ? '还没有标签，请先新建。' : '还没有可用标签，请先到“管理标签”中创建。'}
                    </span>
                  ) : null}
                </div>
              </div>

              {tagEditorMode === 'bulk' ? (
                <label className="block text-[10px] font-bold uppercase text-gray-500">
                  设置方式
                  <select
                    value={tagMode}
                    onChange={(event) => setTagMode(event.target.value as 'append' | 'remove' | 'replace')}
                    className="mt-1 block w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal text-gray-700"
                  >
                    <option value="append">追加（保留原标签）</option>
                    <option value="remove">移除所选标签</option>
                    <option value="replace">替换（空选时清空）</option>
                  </select>
                </label>
              ) : null}
            </div>

            <div className="flex items-center justify-end gap-2 border-t border-brand-border p-4">
              {tagEditorMode === 'manage' ? (
                <button type="button" onClick={() => setIsTagsOpen(false)} className="ui-btn ui-btn-primary text-xs">
                  完成
                </button>
              ) : (
                <>
                  <button
                    type="button"
                    onClick={() => {
                      setIsTagsOpen(false);
                      setTagEditorAccountID(null);
                    }}
                    className="ui-btn text-xs"
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    disabled={
                      isTagSaving
                      || (tagEditorMode === 'bulk' && (
                        selectedAccountIds.length === 0
                        || (selectedTagIds.length === 0 && tagMode !== 'replace')
                      ))
                    }
                    onClick={handleBulkTags}
                    className="ui-btn ui-btn-primary text-xs disabled:opacity-50"
                  >
                    {isTagSaving
                      ? '处理中…'
                      : tagEditorMode === 'single'
                        ? '保存标签'
                        : `应用到 ${selectedAccountIds.length} 个账号`}
                  </button>
                </>
              )}
            </div>
          </div>
        </div>
      )}

      {/* MODAL: local account session backup and encrypted device transfer */}
      {isSessionTransferOpen && (
        <div data-testid="account-session-transfer-modal" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4 animate-fade-in">
	          <div role="dialog" aria-modal="true" aria-label="账号状态备份与迁移" className="bg-brand-surface border border-brand-border rounded-xl w-full max-w-2xl overflow-hidden shadow-2xl max-h-[90vh] flex flex-col">
            <div className="p-4 border-b border-brand-border flex items-center justify-between">
              <div>
                <h4 className="text-sm font-bold text-gray-900">账号状态备份与迁移</h4>
                <p className="mt-0.5 text-[10px] text-gray-500">本地保存登录状态；迁移包使用你设置的口令加密，不含平台密码。</p>
              </div>
              <button type="button" onClick={() => setIsSessionTransferOpen(false)} className="text-gray-455 hover:text-gray-900 transition" aria-label="关闭账号状态备份与迁移">
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="overflow-y-auto p-5 space-y-5">
              <div className="grid grid-cols-1 md:grid-cols-[1fr_auto] gap-3 items-end">
                <label className="space-y-1">
                  <span className="text-[10px] font-bold text-gray-550 uppercase">账号</span>
                  <select
                    value={sessionAccountID ?? ''}
                    onChange={(event) => setSessionAccountID(event.target.value ? Number(event.target.value) : null)}
                    className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-800 focus:outline-none focus:border-brand-accent"
                  >
                    <option value="">选择一个账号</option>
                    {accounts.map((account) => <option key={account.id} value={account.id}>{account.name} · {account.platformCode}</option>)}
                  </select>
                </label>
                <button type="button" disabled={isSessionWorking} onClick={handleCreateAllSessionBackups} className="ui-btn text-xs disabled:opacity-50">
                  {isSessionWorking ? '处理中…' : '备份全部已登录账号'}
                </button>
              </div>

              <section className="border border-brand-border bg-brand-bg/35 p-4 space-y-3">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <h5 className="text-xs font-bold text-gray-800">本机登录态备份</h5>
                    <p className="mt-1 text-[10px] leading-5 text-gray-500">独立 Chromium Profile 会持续保存登录态；额外加密快照按天保留 {sessionBackups?.retentionDays ?? 35} 天。浏览器必须正在运行，且不会打开或跳转页面。</p>
                  </div>
				  <div className="flex gap-2">
					<button type="button" disabled={!sessionBackups?.backups[0] || isSessionWorking} onClick={handleRestoreLatestSessionBackup} className="ui-btn text-xs disabled:opacity-50">
					  恢复最近备份
					</button>
					<button type="button" disabled={!sessionAccount?.browserRunning || isSessionWorking} onClick={handleCreateSessionBackup} className="ui-btn ui-btn-primary text-xs disabled:opacity-50">
					  立即备份
					</button>
				  </div>
                </div>
                {!sessionAccount ? (
                  <p className="text-[10px] text-gray-500">先选择账号即可查看其本机备份状态。</p>
                ) : sessionBackups === null ? (
                  <p className="text-[10px] text-gray-500">正在读取本机备份记录…</p>
                ) : sessionBackups.backups.length === 0 ? (
                  <p className="text-[10px] text-brand-warning">尚无加密快照。先启动并确认登录后点击“立即备份”。</p>
                ) : (
                  <p className="text-[10px] text-emerald-600">最近备份：{formatObservedAt(sessionBackups.backups[0]?.created_at)} · 到期：{formatObservedAt(sessionBackups.backups[0]?.expires_at)}</p>
                )}
              </section>

              <section className="border border-brand-border bg-brand-bg/35 p-4 space-y-3">
                <div>
                  <h5 className="text-xs font-bold text-gray-800">导出到另一台设备</h5>
                  <p className="mt-1 text-[10px] leading-5 text-gray-500">导出的是加密会话迁移包（.anxsession），不导出密码、Profile 路径或代理。部分平台会因设备/IP 变化要求再次验证。</p>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                  <input type="password" autoComplete="new-password" value={sessionExportPassphrase} onChange={(event) => setSessionExportPassphrase(event.target.value)} placeholder="设置迁移口令（至少 12 位）" className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-surface text-xs text-gray-800 focus:outline-none focus:border-brand-accent" />
                  <input type="password" autoComplete="new-password" value={sessionExportConfirm} onChange={(event) => setSessionExportConfirm(event.target.value)} placeholder="再次输入迁移口令" className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-surface text-xs text-gray-800 focus:outline-none focus:border-brand-accent" />
                </div>
                <button type="button" disabled={!sessionAccount?.browserRunning || isSessionWorking} onClick={handleExportSession} className="ui-btn text-xs disabled:opacity-50">
                  导出加密迁移包
                </button>
              </section>

              <section className="border border-brand-border bg-brand-bg/35 p-4 space-y-3">
                <div>
                  <h5 className="text-xs font-bold text-gray-800">从另一台设备导入</h5>
                  <p className="mt-1 text-[10px] leading-5 text-gray-500">导入会创建新的隔离账号 Profile 并写入会话。不会伪造“已登录”；新浏览器会保持打开，等你在平台侧确认。</p>
                </div>
                <input type="file" accept=".anxsession,application/octet-stream" onChange={(event) => setSessionImportFile(event.target.files?.[0] ?? null)} className="block w-full text-xs text-gray-600 file:mr-3 file:border-0 file:bg-brand-surface file:px-3 file:py-2 file:text-xs file:font-semibold file:text-gray-700 hover:file:bg-brand-bg" />
                <input type="password" autoComplete="current-password" value={sessionImportPassphrase} onChange={(event) => setSessionImportPassphrase(event.target.value)} placeholder="输入该迁移包的口令" className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-surface text-xs text-gray-800 focus:outline-none focus:border-brand-accent" />
                <button type="button" disabled={!sessionImportFile || isSessionWorking} onClick={handleImportSession} className="ui-btn text-xs disabled:opacity-50">
                  导入并打开验证浏览器
                </button>
              </section>
            </div>
          </div>
        </div>
      )}

      {/* MODAL: durable login observation history */}
      {historyAccount && (
        <div data-testid="login-history-modal" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4 animate-fade-in">
	          <div role="dialog" aria-modal="true" aria-label="账号监测历史" className="bg-brand-surface border border-brand-border rounded-xl w-full max-w-2xl overflow-hidden shadow-2xl">
            <div className="p-4 border-b border-brand-border flex items-center justify-between">
              <div>
                <h4 className="text-sm font-bold text-gray-900">登录状态记录</h4>
                <p className="mt-0.5 text-[10px] text-gray-500">
                  {historyAccount.name} · 只记录状态证据，不保存密码或 Cookie 内容
                </p>
              </div>
              <button
                type="button"
                onClick={() => setHistoryAccount(null)}
                className="text-gray-455 hover:text-gray-900 transition"
                aria-label="关闭登录状态记录"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="max-h-[65vh] overflow-y-auto p-4">
              {isHistoryLoading ? (
                <div className="py-12 text-center text-xs text-gray-500">正在读取持久记录...</div>
              ) : historyEvents.length === 0 && browserHistoryEvents.length === 0 ? (
                <div className="py-12 text-center text-xs text-gray-500">尚无监测记录，点击“检测登录态”后会保存在这里。</div>
              ) : (
                <div className="space-y-5">
                  <section>
                    <h5 className="mb-2 text-[11px] font-bold text-gray-700">浏览器运行记录</h5>
                    {browserHistoryEvents.length === 0 ? (
                      <p className="rounded-lg border border-dashed border-brand-border p-3 text-[10px] text-gray-500">暂无浏览器生命周期记录。</p>
                    ) : (
                      <div className="space-y-2">
                        {browserHistoryEvents.map((event) => (
                          <div key={`browser-${event.id}`} className="rounded-lg border border-brand-border bg-brand-bg/50 px-3 py-2.5">
                            <div className="flex flex-wrap items-center justify-between gap-2">
                              <div className="flex items-center gap-2">
                                <span className={`h-2 w-2 rounded-full ${browserEventTone(event.eventType)}`} />
                                <span className="text-xs font-semibold text-gray-800">{browserEventLabels[event.eventType] || event.eventType}</span>
                                <span className="font-mono text-[9.5px] text-gray-500">{event.eventType}</span>
                              </div>
                              <time className="text-[10px] text-gray-500">{formatObservedAt(event.createdAt)}</time>
                            </div>
                            {(event.pid || event.cdpPort) ? (
                              <p className="mt-1.5 text-[9.5px] text-gray-500">
                                {event.pid ? `进程 ${event.pid}` : ''}{event.pid && event.cdpPort ? ' · ' : ''}{event.cdpPort ? `CDP ${event.cdpPort}` : ''}
                              </p>
                            ) : null}
                            {event.reasonCode ? (
                              <p className="mt-1 text-[9.5px] text-gray-500">
                                {browserReasonLabels[event.reasonCode] || event.reasonCode}
                                {event.exitCode !== undefined ? ` · 退出码 ${event.exitCode}` : ''}
                                {event.termSignal !== undefined ? ` · 信号 ${event.termSignal}` : ''}
                              </p>
                            ) : null}
                          </div>
                        ))}
                      </div>
                    )}
                  </section>
                  <section>
                    <h5 className="mb-2 text-[11px] font-bold text-gray-700">平台登录证据</h5>
                    {historyEvents.length === 0 ? (
                      <p className="rounded-lg border border-dashed border-brand-border p-3 text-[10px] text-gray-500">暂无平台登录证据。</p>
                    ) : (
                    <div className="space-y-2">
                  {historyEvents.map((event) => (
                    <div key={event.id} className="rounded-lg border border-brand-border bg-brand-bg/50 px-3 py-2.5">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <span className={`w-2 h-2 rounded-full ${event.authState === 'logged_in' ? 'bg-emerald-500' : event.authState === 'logged_out' ? 'bg-red-500' : 'bg-brand-warning/20'}`} />
                          <span className="text-xs font-semibold text-gray-800">
                            {event.authState === 'logged_in' ? '已登录' : event.authState === 'logged_out' ? '已掉线' : '待验证'}
                          </span>
						  <span className="text-[10px] text-gray-500">
							{loginReasonLabels[event.reasonCode] || event.reasonCode}
						  </span>
						  {event.evidenceCode && event.evidenceCode !== event.reasonCode ? (
							<span className="text-[10px] text-brand-warning">
							  {loginReasonLabels[event.evidenceCode] || event.evidenceCode}
							</span>
						  ) : null}
						  {event.avatarErrorCode ? (
							<span className="text-[10px] text-brand-warning">
							  {loginReasonLabels[event.avatarErrorCode] || '本轮头像更新失败'}
							</span>
						  ) : null}
                        </div>
                        <div className="text-right text-[10px] text-gray-500">
                          <div>首次发现：{formatObservedAt(event.createdAt)}</div>
                          <div>最后确认：{formatObservedAt(event.observedAt || event.createdAt)}</div>
                        </div>
                      </div>
                      <div className="mt-2 grid grid-cols-1 sm:grid-cols-3 gap-1.5 text-[9.5px] text-gray-500">
                        <span>登录模式：{loginModeLabels[event.loginMode] || event.loginMode}</span>
                        <span>浏览器：{event.browserState}</span>
                        <span className="truncate font-mono" title={event.traceId}>trace {event.traceId.slice(0, 12)}</span>
                        <span className="truncate font-mono" title={`${event.reasonCode}${event.evidenceCode ? ` / ${event.evidenceCode}` : ''}`}>
                          证据码：{event.reasonCode}{event.evidenceCode && event.evidenceCode !== event.reasonCode ? ` / ${event.evidenceCode}` : ''}
                        </span>
                        <span title={event.avatarSource}>头像来源：{event.avatarSource || '未记录'}</span>
                      </div>
                    </div>
                  ))}
                    </div>
                    )}
                  </section>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* MODAL: Add Account */}
      {isAddOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4 animate-fade-in">
	          <div role="dialog" aria-modal="true" aria-label="添加自媒体创作者账号" className="bg-brand-surface border border-brand-border rounded-xl w-full max-w-md overflow-hidden shadow-2xl">
            <div className="p-4 border-b border-brand-border flex items-center justify-between">
                  <h4 className="text-sm font-bold text-gray-900">添加自媒体创作者账号</h4>
              <button onClick={() => setIsAddOpen(false)} className="text-gray-455 hover:text-gray-900 transition">
                <X className="w-4 h-4" />
              </button>
            </div>
            <form onSubmit={handleAddAccountSubmit} className="p-5 space-y-4">
              <div className="space-y-1.5">
                <label className="text-[10px] font-bold text-gray-550 uppercase">平台</label>
                <div className="grid grid-cols-2 gap-2 max-h-52 overflow-y-auto pr-0.5">
                  {addPlatformOptions.map((plat) => {
                    const selected = addPlatform === plat.code;
                    return (
                      <button
                        key={plat.code}
                        data-testid={`add-account-platform-${plat.code}`}
                        type="button"
                        onClick={() => setAddPlatform(plat.code)}
                        className={[
                          'flex items-center gap-2.5 px-2.5 py-2 rounded-xl border text-left transition',
                          selected
                            ? 'border-brand-accent bg-brand-accent/10 shadow-sm'
                            : 'border-brand-border bg-brand-bg hover:border-gray-400',
                        ].join(' ')}
                      >
                        <PlatformIcon platform={plat.code} className="w-7 h-7" title={plat.name} />
                        <span className="text-[12px] font-semibold text-gray-800 truncate">
                          {plat.name}
                        </span>
                      </button>
                    );
                  })}
                </div>
              </div>

              <div className="space-y-1">
                <label className="text-[10px] font-bold text-gray-550 uppercase">账号别名</label>
                <input
                  type="text"
                  placeholder="如：小腾的小红书一号，留空自动生成"
                  value={addName}
                  onChange={(e) => setAddName(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none focus:border-brand-accent transition"
                />
              </div>

              <div className="space-y-1">
                <label className="text-[10px] font-bold text-gray-550 uppercase">备注 / 用途</label>
                <input
                  type="text"
                  placeholder="如：主要发布情感类口播..."
                  value={addRemark}
                  onChange={(e) => setAddRemark(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none focus:border-brand-accent transition"
                />
              </div>

              <label className="flex items-center gap-2 cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={addStartNow}
                  onChange={(e) => setAddStartNow(e.target.checked)}
                  className="rounded border-brand-border"
                />
                <span className="text-xs text-gray-700">创建后立即启动浏览器（人工登录接管）</span>
              </label>

              <p className="text-[10px] text-gray-500 leading-relaxed">
                将创建独立 <code className="font-mono">browser_profile</code> 目录与指纹种子；不保存平台密码。
              </p>

              <div className="pt-2 flex items-center justify-end gap-2.5">
                <button
                  type="button"
                  onClick={() => setIsAddOpen(false)}
                  className="px-4 py-2 rounded-lg border border-brand-border hover:bg-black/5 text-xs text-gray-750 transition"
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 rounded-lg bg-brand-accent hover:bg-brand-accent-hover text-brand-on-accent text-xs font-bold transition shadow-md shadow-brand-accent/15"
                >
                  {addStartNow ? '添加并启动' : '确定添加'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* MODAL: Fingerprint settings */}
      {isFpOpen && selectedAccountForFp && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4 overflow-y-auto animate-fade-in">
	          <div role="dialog" aria-modal="true" aria-label="防关联浏览器指纹" className="bg-brand-surface border border-brand-border rounded-xl w-full max-w-2xl overflow-hidden shadow-2xl my-8">
            <div className="p-4 border-b border-brand-border flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Fingerprint className="w-4 h-4 text-brand-muted" />
                <h4 className="text-sm font-bold text-gray-900">
                  防关联浏览器指纹：[{selectedAccountForFp.name}]
                </h4>
              </div>
              <button onClick={() => setIsFpOpen(false)} className="text-gray-455 hover:text-gray-900 transition">
                <X className="w-4 h-4" />
              </button>
            </div>

            <div className="p-6 space-y-6 max-h-[70vh] overflow-y-auto">
              {/* Row 0: Account Browser Engine Binding */}
              <div className="p-3.5 rounded-lg border border-brand-border bg-brand-bg/50 space-y-1.5">
                <label className="text-[11px] font-bold text-gray-800 flex items-center justify-between">
                  <span>绑定独立浏览器内核 (PERSISTENT BROWSER)</span>
                  <span className="text-[10px] text-gray-500 font-normal">已持久化保存至数据库</span>
                </label>
                <select
                  value={fpEngine}
                  onChange={(e) => setFpEngine(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-surface text-xs font-semibold text-gray-900 focus:outline-none focus:border-brand-accent cursor-pointer"
                >
                  <option value="chromium">🌐 Chromium（默认首选内核）</option>
                  <option value="browseros">⚡ BrowserOS（Agentic 浏览器）</option>
                  <option value="chrome">🌐 Google Chrome（系统 Chrome）</option>
                </select>
              </div>

              {/* Row 1 */}
              <div className="grid grid-cols-2 gap-4 text-gray-700">
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">Fingerprint Seed (随机特征因子)</label>
                  <input
                    type="number"
                    value={fpSeed}
                    onChange={(e) => setFpSeed(Number(e.target.value))}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none focus:border-brand-accent"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">地理分配区域</label>
                  <select
                    value={fpRegion}
                    onChange={(e) => setFpRegion(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none cursor-pointer"
                  >
                    <option value="beijing">🇨🇳 北京</option>
                    <option value="shanghai">🇨🇳 上海</option>
                    <option value="guangzhou">🇨🇳 广州</option>
                    <option value="shenzhen">🇨🇳 深圳</option>
                    <option value="chengdu">🇨🇳 成都</option>
                    <option value="hangzhou">🇨🇳 杭州</option>
                    <option value="wuhan">🇨🇳 武汉</option>
                    <option value="xian">🇨🇳 西安</option>
                    <option value="sanya">🇨🇳 三亚</option>
                    <option value="nanjing">🇨🇳 南京</option>
                    <option value="chongqing">🇨🇳 重庆</option>
                    <option value="changsha">🇨🇳 长沙</option>
                    <option value="hong_kong">🇭🇰 香港</option>
                    <option value="macau">🇲🇴 澳门</option>
                    <option value="taiwan">🇹🇼 台湾</option>
                    <option value="singapore">🇸🇬 新加坡</option>
                    <option value="us_east">🇺🇸 美东</option>
                    <option value="us_west">🇺🇸 美西</option>
                    <option value="japan">🇯🇵 日本</option>
                    <option value="korea">🇰🇷 韩国</option>
                    <option value="uk">🇬🇧 英国</option>
                  </select>
                </div>
              </div>

              {/* Row 2 */}
              <div className="grid grid-cols-3 gap-4">
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">模拟系统 (OS)</label>
                  <select
                    value={fpPlatform}
                    onChange={(e) => setFpPlatform(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none cursor-pointer"
                  >
                    <option value="macos">macOS</option>
                    <option value="windows">Windows</option>
                  </select>
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">浏览器内核</label>
                  <input
                    type="text"
                    value={fpBrand}
                    onChange={(e) => setFpBrand(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">内核版本</label>
                  <input
                    type="text"
                    value={fpBrandVer}
                    onChange={(e) => setFpBrandVer(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none"
                  />
                </div>
              </div>

              {/* Row 3 */}
              <div className="grid grid-cols-3 gap-4">
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">CPU 核心数</label>
                  <input
                    type="number"
                    value={fpCpu}
                    onChange={(e) => setFpCpu(Number(e.target.value))}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">主语言</label>
                  <input
                    type="text"
                    value={fpLang}
                    onChange={(e) => setFpLang(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">模拟时区</label>
                  <input
                    type="text"
                    value={fpTz}
                    onChange={(e) => setFpTz(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none"
                  />
                </div>
              </div>

              {/* Row 4 */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">GPU 厂商</label>
                  <input
                    type="text"
                    value={fpGpuVendor}
                    placeholder="自动"
                    onChange={(e) => setFpGpuVendor(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-bold text-gray-550 uppercase">GPU 渲染器</label>
                  <input
                    type="text"
                    value={fpGpuRenderer}
                    placeholder="自动"
                    onChange={(e) => setFpGpuRenderer(e.target.value)}
                    className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-700 focus:outline-none"
                  />
                </div>
              </div>

              {/* Accept Lang */}
              <div className="space-y-1">
                <label className="text-[10px] font-bold text-gray-400 uppercase">Accept Headers (接受语言列表)</label>
                <input
                  type="text"
                  value={fpAccept}
                  onChange={(e) => setFpAccept(e.target.value)}
                  className="w-full px-3 py-1.5 rounded-lg border border-brand-border bg-brand-bg text-xs text-gray-200 focus:outline-none"
                />
              </div>

              {/* Help Message */}
              <div className="p-3.5 rounded-lg border border-brand-border bg-brand-surface-raised text-[10.5px] text-brand-muted flex items-start gap-2.5">
                <Info className="w-4 h-4 flex-shrink-0 mt-0.5 text-brand-muted" />
                <div className="space-y-0.5">
                  <span className="font-bold">防关联防关联规则提示：</span>
                  <p>设置指纹可以为该账号生成完全隔离的软硬件指纹。多账号同时开启时能极佳防关联，建议为每个账号设置不同的 Seed 和 区域。</p>
                </div>
              </div>
            </div>

            {/* Modal Actions */}
            <div className="p-4 border-t border-brand-border bg-black/10 flex items-center justify-between">
              <button
                type="button"
                onClick={handleSmartFpSubmit}
                className="flex items-center gap-1.5 px-3 py-2 rounded-lg border border-brand-accent/40 bg-brand-accent/5 hover:bg-brand-accent/15 text-brand-muted text-xs font-bold transition"
              >
                <Sparkles className="w-3.5 h-3.5" />
                智能特征分配
              </button>

              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setIsFpOpen(false)}
                  className="px-4 py-2 rounded-lg border border-brand-border hover:bg-black/5 text-xs text-gray-700 transition"
                >
                  取消
                </button>
                <button
                  type="button"
                  onClick={handleSaveFpSubmit}
                  className="px-4 py-2 rounded-lg bg-brand-accent hover:bg-brand-accent-hover text-brand-on-accent text-xs font-bold transition"
                >
                  保存指纹
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
