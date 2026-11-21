import { AccountAvatar } from './AccountAvatar';
import { BrowserIdentityMark } from './BrowserIdentityMark';
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import useSWR from 'swr';
import {
  ArrowDown,
  ArrowUp,
  BookmarkPlus,
  Check,
  CircleUserRound,
  Clock3,
  Download,
  ExternalLink,
  Gauge,
  LogIn,
  Monitor,
  Plus,
  RefreshCw,
  Settings2,
  ShieldCheck,
  SquarePen,
  X,
} from 'lucide-react';
import { api, type Account, type AccountAsset, type EgoIdentity, type EgoLoginQR } from '../api';
import { duplicateAccountBindingIDs } from '../accountBindings';
import { PlatformIcon } from './PlatformIcon';
import { useUIStore } from '../store';
import { platformCodesForMode } from '../workspaceMode';

export type BrowserWorkspaceSection = 'accounts' | 'fleet' | 'overview';

type BrowserWorkspaceViewProps = {
  initialSection?: BrowserWorkspaceSection;
};

type QRDialogState = {
  account: Account;
  identity: EgoIdentity;
  result: EgoLoginQR;
};

const platformOrder = [
  'douyin',
  'wechat_video',
  'kuaishou',
  'xhs',
  'tiktok',
  'youtube',
  'meta',
  'wechat_mp',
  'csdn',
	'acfun',
	'baijiahao',
	'bilibili',
	'jianshu',
	'juejin',
	'omtencent',
	'pdd',
	'sina',
	'toutiao',
	'weishi',
	'x',
	'zhihu',
	'douyin_shop',
	'taobao',
  'tiktok_shop',
  'shopee',
  'shopify',
  'amazon_seller',
  'qianchuan',
  'jd_shop',
  'pinduoduo_shop',
  'xhs_shop',
  'kuaishou_shop',
  'wechat_shop',
  'alibaba_1688',
  'aliexpress',
  'xianyu',
] as const;

const platformNames: Record<string, string> = {
  douyin: '抖音',
  wechat_video: '视频号',
  kuaishou: '快手',
  xhs: '小红书',
  tiktok: 'TikTok',
  youtube: 'YouTube',
  meta: 'Meta',
  facebook: 'Facebook',
  instagram: 'Instagram',
  wechat_mp: '微信公众号',
  csdn: 'CSDN',
	acfun: 'AcFun',
	baijiahao: '百家号',
	bilibili: '哔哩哔哩',
	jianshu: '简书',
	juejin: '掘金',
	omtencent: '腾讯内容开放平台',
	pdd: '拼多多',
	sina: '新浪微博',
	toutiao: '今日头条',
	weishi: '微视',
	x: 'X',
	zhihu: '知乎',
	douyin_shop: '抖音电商',
	taobao: '淘宝',
  tiktok_shop: 'TikTok Shop',
  shopee: 'Shopee',
  shopify: 'Shopify',
  amazon_seller: 'Amazon Seller',
  qianchuan: '巨量千川',
  jd_shop: '京东店铺',
  pinduoduo_shop: '拼多多店铺',
  xhs_shop: '小红书店铺',
  kuaishou_shop: '快手小店',
  wechat_shop: '微信小店',
  alibaba_1688: '1688',
  aliexpress: 'AliExpress',
  xianyu: '闲鱼',
};

export type PlatformCategoryKey = 'multimedia' | 'article' | 'ecommerce';

export const PLATFORM_CATEGORIES: Record<PlatformCategoryKey, { label: string; icon: string; codes: string[] }> = {
  multimedia: {
    label: '多媒体平台',
    icon: '🎬',
    codes: ['xhs', 'douyin', 'wechat_video', 'kuaishou', 'tiktok', 'youtube', 'meta', 'bilibili', 'acfun', 'weishi', 'sina', 'x'],
  },
  article: {
    label: '文章自媒体',
    icon: '📝',
    codes: ['wechat_mp', 'toutiao', 'baijiahao', 'omtencent', 'zhihu', 'jianshu', 'juejin', 'csdn'],
  },
  ecommerce: {
    label: '电商·闲鱼',
    icon: '🛍️',
    codes: [
      'xianyu', 'taobao', 'douyin_shop', 'pdd', 'pinduoduo_shop', 'alibaba_1688', 'aliexpress',
      'jd_shop', 'xhs_shop', 'kuaishou_shop', 'wechat_shop', 'tiktok_shop', 'shopee', 'shopify',
      'amazon_seller', 'qianchuan',
    ],
  },
};

export function getPlatformCategory(code: string): PlatformCategoryKey | 'other' {
  if (PLATFORM_CATEGORIES.multimedia.codes.includes(code)) return 'multimedia';
  if (PLATFORM_CATEGORIES.article.codes.includes(code)) return 'article';
  if (PLATFORM_CATEGORIES.ecommerce.codes.includes(code)) return 'ecommerce';
  return 'other';
}

export function getCategoryBadge(code: string) {
  const cat = getPlatformCategory(code);
  if (cat === 'multimedia') return { label: '多媒体', className: 'bg-brand-surface-raised text-brand-muted border-brand-border' };
  if (cat === 'article') return { label: '文章', className: 'bg-brand-surface-raised text-brand-muted border-brand-border' };
  if (cat === 'ecommerce') return { label: '电商', className: 'bg-brand-warning/10 text-brand-warning border-brand-warning/30' };
  return null;
}

const emptyIdentities: EgoIdentity[] = [];
const emptyAccounts: Account[] = [];
const emptyAccountAssets: AccountAsset[] = [];

const loginAttentionReasons = new Set([
  'CHECKPOINT_REQUIRED',
  'LOGIN_REQUIRED_PAGE',
  'SESSION_EXPIRED_PAGE',
]);

function needsLoginAttention(account: Account) {
  return account.loginState !== 'logged_in' || loginAttentionReasons.has(account.loginReasonCode ?? '');
}

/**
 * 「待确认」不是「没登录」，而是「没有可据以判定的页面证据」。
 *
 * 电商后台（淘宝、京东、拼多多、Shopee、Shopify…）暂时没有人工审核过的
 * 登录特征，代码里刻意不允许用「看起来像个人中心」的通用 DOM 判成已登录
 * ——否则会把「打开了一个长得像后台的页面」误记成登录成功。所以这里如实
 * 说明，并指向唯一可靠的判定方式：在浏览器里打开这个平台看一眼。
 */
function loginPendingHint(account: Account): string {
  switch (account.loginEvidenceCode) {
    case 'CHECKPOINT_DOM':
      return '当前停在验证页。请在浏览器里完成验证后再点核验。';
    case 'EVIDENCE_MISSING':
      return '该平台还没有审核过的登录特征，无法自动判断。请在浏览器里打开它确认一下。';
    default:
      return '还没有足够证据判断登录状态。';
  }
}

/** 证据码对应的短标签，放在状态旁边，避免只看到一个「待确认」。 */
function loginEvidenceLabel(code: string | undefined | null): string {
  switch (code) {
    case 'CHECKPOINT_DOM':
      return '需验证';
    case 'EVIDENCE_MISSING':
      return '无特征';
    default:
      return '';
  }
}

function AccountDialog({ label, onClose, children }: { label: string; onClose: () => void; children: ReactNode }) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.open) dialog.showModal();
  }, []);

  return (
    <dialog
      ref={dialogRef}
      aria-label={label}
      className="m-auto max-h-[86vh] w-[calc(100%-2rem)] max-w-xl overflow-y-auto rounded-2xl border border-brand-border bg-brand-surface p-0 shadow-2xl backdrop:bg-black/35"
      onClose={onClose}
    >
      {children}
    </dialog>
  );
}

export function BrowserWorkspaceView({ initialSection: _initialSection = 'accounts' }: BrowserWorkspaceViewProps) {
  const toast = useUIStore((state) => state.toast);
  const productMode = useUIStore((state) => state.productMode);
  const { data: identityData, error: identitiesError, mutate: mutateIdentities } = useSWR('api/ego/identities', api.getEgoIdentities);
  const { data: accountData, error: accountsError, mutate: mutateAccounts } = useSWR('api/accounts', api.getAccounts, { refreshInterval: 1_000 });
  const { data: accountAssetData, mutate: mutateAccountAssets } = useSWR('api/account-assets', api.getAccountAssets, { refreshInterval: 1_000 });
  // 登录会话列表接口在当前构建里没有注册（`legacy_chromium` 之外的构建中
  // 它被有意断言为 404），此前这里每 2 秒轮询一次、永远拿 404。批量登录的
  // 进行中状态已经由 `/api/ego/identities/{id}/login-batches` 与本地
  // `activeBatches` 维护，不再需要这条轮询。
  const { data: sessionMonitor, mutate: mutateSessionMonitor } = useSWR('api/ego/session-monitor', api.getEgoSessionMonitor, { refreshInterval: 30_000 });
  const { data: legacyAccounts, mutate: mutateLegacyAccounts } = useSWR('api/ego/legacy-accounts', api.getEgoLegacyAccounts);
  const { data: egoProfilesData } = useSWR('api/ego/profiles', api.getEgoProfiles);
  const identities = identityData ?? emptyIdentities;
  const accounts = accountData ?? emptyAccounts;
  const accountDataReady = identityData !== undefined && accountData !== undefined;
  const accountDataError = Boolean(identitiesError || accountsError);
  const configurationUnavailable = !accountDataReady || accountDataError;
  const visiblePlatformCodes = useMemo(() => platformCodesForMode(platformOrder, productMode), [productMode]);
  const modeAccounts = useMemo(() => accounts.filter((account) => visiblePlatformCodes.includes(account.platformCode)), [accounts, visiblePlatformCodes]);
  const duplicateAccountIDs = useMemo(() => duplicateAccountBindingIDs(modeAccounts), [modeAccounts]);
  const accountAssets = accountAssetData ?? emptyAccountAssets;
  const { data: doctor, error: doctorError } = useSWR('api/ego/doctor', api.getEgoDoctor);
  const [identityName, setIdentityName] = useState('');
  const [identityID, setIdentityID] = useState(0);
  const [selectedPlatforms, setSelectedPlatforms] = useState<string[]>([]);
  const [expectedPlatformCodes, setExpectedPlatformCodes] = useState<string[]>([]);
  const [activeBatches, setActiveBatches] = useState<Record<number, string[]>>({});
  const [browserOpenOffsets, setBrowserOpenOffsets] = useState<Record<number, number>>({});
  const [editingIdentityID, setEditingIdentityID] = useState(0);
  const [editingIdentityName, setEditingIdentityName] = useState('');
  const [busy, setBusy] = useState('');
  const [capacity, setCapacity] = useState<{ measured: number; current: number } | null>(null);
  const [monitorInterval, setMonitorInterval] = useState(60);
  const [monitorEnabled, setMonitorEnabled] = useState(false);
  const [panel, setPanel] = useState<'add' | 'platforms' | 'settings' | null>(null);
  const [loginQR, setLoginQR] = useState<QRDialogState | null>(null);
  const [loginQRRefreshing, setLoginQRRefreshing] = useState(false);
  const [loginQRAutoRefreshError, setLoginQRAutoRefreshError] = useState('');
  const [legacyModalOpen, setLegacyModalOpen] = useState(false);
  const [selectedLegacyPlatforms, setSelectedLegacyPlatforms] = useState<string[]>([]);
  const [targetLegacyIdentityID, setTargetLegacyIdentityID] = useState<number>(0);

  const loginQRRequestInFlight = useRef(false);
  const announcedLoginAccounts = useRef(new Set<number>());

  const grouped = useMemo(() => identities.map((identity) => ({
    identity,
    accounts: modeAccounts.filter((account) => account.identityId === identity.id && account.enabled),
  })), [modeAccounts, identities]);
  // 按 account_id 分组附属资产（仅保留非 browser_account 的展示型资产，供卡片渲染头像与显式平台名）。
  const accountAssetsByAccount = useMemo(() => {
    const groupedAssets = new Map<number, AccountAsset[]>();
    for (const asset of accountAssets) {
      if (!asset.enabled || asset.asset_type === 'browser_account') continue;
      const assets = groupedAssets.get(asset.account_id) ?? [];
      assets.push(asset);
      groupedAssets.set(asset.account_id, assets);
    }
    return groupedAssets;
  }, [accountAssets]);
  const activeIdentity = identities.find((identity) => identity.id === identityID);
  // activeBatches 只来自本页发起的批量登录（打开批次时写入、账号登录成功后
  // 移除）。切换产品模式后，超出当前可见平台的编码应当被丢弃，否则会在
  // 侧栏留下一个永远等不到结果的批次。
  useEffect(() => {
    setActiveBatches((current) => {
      let changed = false;
      const next: Record<number, string[]> = {};
      for (const [rawIdentityID, platformCodes] of Object.entries(current)) {
        const nextCodes = platformCodes.filter((code) => visiblePlatformCodes.includes(code));
        if (nextCodes.length === 0) {
          changed = true;
          continue;
        }
        if (nextCodes.length !== platformCodes.length) changed = true;
        next[Number(rawIdentityID)] = nextCodes;
      }
      return changed ? next : current;
    });
  }, [visiblePlatformCodes]);

  useEffect(() => {
    setPanel(null);
  }, [productMode]);

  useEffect(() => {
    if (sessionMonitor) {
      setMonitorInterval(sessionMonitor.interval_minutes);
      setMonitorEnabled(sessionMonitor.enabled);
    }
  }, [sessionMonitor]);

  async function refresh() {
    await Promise.all([mutateIdentities(), mutateAccounts(), mutateAccountAssets(), mutateSessionMonitor()]);
  }

  async function run(label: string, action: () => Promise<unknown>, success = 'EgoLite 配置已更新') {
    setBusy(label);
    try {
      await action();
      await refresh();
      toast.show(success, 'success');
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '操作失败', 'error');
    } finally {
      setBusy('');
    }
  }

  async function renameIdentity() {
    const name = editingIdentityName.trim();
    if (editingIdentityID <= 0 || !name) return;
    await run(`rename-identity-${editingIdentityID}`, async () => {
      await api.updateEgoIdentity(editingIdentityID, { name });
      setEditingIdentityID(0);
      setEditingIdentityName('');
    }, '浏览器名称已更新');
  }

  async function moveIdentity(identity: EgoIdentity, direction: -1 | 1) {
    const index = identities.findIndex((item) => item.id === identity.id);
    const target = identities[index + direction];
    if (!target) return;
    await run(`sort-identity-${identity.id}`, async () => {
      await Promise.all([
        api.updateEgoIdentity(identity.id, { sort: target.sort }),
        api.updateEgoIdentity(target.id, { sort: identity.sort }),
      ]);
    }, '浏览器顺序已更新');
  }

  function openNewAccount() {
    if (configurationUnavailable || busy) return;
    setIdentityID(0);
    setIdentityName('');
    setSelectedPlatforms([]);
    setExpectedPlatformCodes([]);
    setPanel('add');
  }

  function openPlatformManager(targetIdentityID: number) {
    if (configurationUnavailable || busy) return;
    setIdentityID(targetIdentityID);
    setExpectedPlatformCodes(accounts
      .filter((account) => account.identityId === targetIdentityID && account.enabled)
      .map((account) => account.platformCode));
    setSelectedPlatforms(modeAccounts
      .filter((account) => account.identityId === targetIdentityID && account.enabled)
      .sort((left, right) => platformOrder.indexOf(left.platformCode as typeof platformOrder[number]) - platformOrder.indexOf(right.platformCode as typeof platformOrder[number]))
      .map((account) => account.platformCode));
    setPanel('platforms');
  }

  async function saveAccountConfiguration() {
    if (configurationUnavailable || busy) return;
    // Preserve the complete opening snapshot, including platforms outside this workspace/catalog.
    // The backend rejects a stale snapshot instead of silently disabling newer bindings.
    const preservedPlatformCodes = expectedPlatformCodes.filter((code) => !visiblePlatformCodes.includes(code));
    const ordered = [...platformOrder.filter((code) => selectedPlatforms.includes(code)), ...preservedPlatformCodes];
    if (panel === 'add' && ordered.length === 0) {
      toast.show('至少选择一个平台', 'error');
      return;
    }
    setBusy('account-configuration');
    try {
      let targetIdentityID = identityID;
      if (panel === 'add') {
        const created = await api.createEgoIdentity(identityName.trim());
        targetIdentityID = created.id;
        setIdentityID(created.id);
        setIdentityName('');
        setPanel('platforms');
      }
      if (targetIdentityID <= 0) throw new Error('请选择浏览器');
      await api.configureEgoIdentityPlatforms(targetIdentityID, ordered, expectedPlatformCodes);
      await refresh();
      setPanel(null);
      toast.show(panel === 'add' ? '账号已添加，下一步可打开登录页' : '平台配置已更新', 'success');
    } catch (error) {
      if (error instanceof Error && error.message.includes('EGO_PLATFORM_CONFIGURATION_CONFLICT')) {
        setPanel(null);
        await Promise.allSettled([mutateIdentities(), mutateAccounts()]);
        toast.show('账号配置已在其他页面更新，本次未覆盖。请重新打开配置后再保存。', 'error');
        return;
      }
      toast.show(error instanceof Error ? error.message : panel === 'add' ? '添加账号失败' : '平台配置失败', 'error');
    } finally {
      setBusy('');
    }
  }

  // @ts-expect-error unused
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  async function openLoginBatch(identity: EgoIdentity, identityAccounts: Account[]) {
    const remaining = identityAccounts
      .filter((account) => account.enabled && (needsLoginAttention(account) || duplicateAccountIDs.has(account.id)))
      .sort((left, right) => platformOrder.indexOf(left.platformCode as typeof platformOrder[number]) - platformOrder.indexOf(right.platformCode as typeof platformOrder[number]))
      .slice(0, 3)
      .map((account) => account.platformCode);
    if (remaining.length === 0) {
      toast.show('当前浏览器的平台均已登录', 'success');
      return;
    }
    await run(`login-${identity.id}`, async () => {
      const batch = await api.openEgoLoginBatch(identity.id, remaining);
      setActiveBatches((current) => ({ ...current, [identity.id]: batch.platform_codes }));
    }, `已打开 ${remaining.length} 个创作者后台，请在 EgoLite 完成登录`);
  }

  async function switchDuplicateAccount(identity: EgoIdentity, account: Account) {
    await run(`switch-account-${account.id}`, () => api.loginEgoSpace(account.id), `${identity.name} 已打开，请切换到独立账号后再同步`);
  }


  async function openSinglePlatform(identity: EgoIdentity, account: Account) {
    setBusy(`open-single-${account.id}`);
    try {
      await api.openEgoBrowser(identity.id, 0, [account.platformCode]);
      await refresh();
      toast.show(`已在 ${identity.name} 中打开 ${platformNames[account.platformCode] ?? account.name} 创作者主页`, 'success');
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '打开平台主页失败', 'error');
    } finally {
      setBusy('');
    }
  }

  async function openLoggedInPlatforms(identity: EgoIdentity, identityAccounts: Account[]) {
    if (identityAccounts.every((account) => !account.enabled)) {
      toast.show('此浏览器还没有配置平台', 'info');
      return;
    }
    setBusy(`open-browser-${identity.id}`);
    try {
      const result = await api.openEgoBrowser(identity.id, browserOpenOffsets[identity.id] ?? 0, identityAccounts.map((account) => account.platformCode));
      setBrowserOpenOffsets((current) => {
        const next = { ...current };
        if (result.next_offset > 0) next[identity.id] = result.next_offset;
        else delete next[identity.id];
        return next;
      });
      await refresh();
      toast.show(
        result.remaining > 0
          ? `已打开 ${result.platform_codes.length} 个平台，剩余 ${result.remaining} 个`
          : `已打开 ${result.total} 个平台`,
        'success',
      );
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '浏览器打开失败', 'error');
    } finally {
      setBusy('');
    }
  }

  async function openPlatformLogin(identity: EgoIdentity, account: Account) {
    if (loginQRRequestInFlight.current) return;
    loginQRRequestInFlight.current = true;
    setLoginQRAutoRefreshError('');
    setBusy(`login-qr-${account.id}`);
    try {
      const result = await api.openEgoLoginQR(account.id, false, true);
      if (result.state === 'logged_in') {
        await api.verifyEgoLoginBatch(identity.id, [account.platformCode]);
        await refresh();
        toast.show(`${platformNames[account.platformCode] ?? account.name} 已登录，账号资料已同步`, 'success');
        return;
      }
      try {
        await api.focusEgoLite();
      } catch {
        toast.show('登录页已打开，请切换到 EgoLite 完成登录', 'info');
      }
      setLoginQR({ identity, account, result });
      if (result.state === 'manual_takeover') {
        toast.show(`${platformNames[account.platformCode] ?? account.name} 需要在 EgoLite 完成登录`, 'info');
      }
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '无法打开平台登录', 'error');
    } finally {
      setBusy('');
      loginQRRequestInFlight.current = false;
    }
  }

  async function refreshLoginQR(focusEgoLite = false) {
    if (!loginQR || loginQRRefreshing || loginQRRequestInFlight.current) return;
    loginQRRequestInFlight.current = true;
    setLoginQRRefreshing(true);
    setLoginQRAutoRefreshError('');
    try {
      if (focusEgoLite) {
        try {
          await api.focusEgoLite();
        } catch {
          // Keep QR sync available even if macOS cannot foreground EgoLite.
        }
      }
      const result = await api.openEgoLoginQR(loginQR.account.id, true, false);
      if (result.state === 'logged_in') {
        await api.verifyEgoLoginBatch(loginQR.identity.id, [loginQR.account.platformCode]);
        await refresh();
        setLoginQR(null);
        toast.show(`${platformNames[loginQR.account.platformCode] ?? loginQR.account.name} 已登录，账号资料已同步`, 'success');
        return;
      }
      setLoginQR((current) => current ? { ...current, result } : current);
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '二维码刷新失败', 'error');
    } finally {
      setLoginQRRefreshing(false);
      loginQRRequestInFlight.current = false;
    }
  }

  async function completeLoginQR() {
    if (!loginQR || busy || loginQRRequestInFlight.current) return;
    const current = loginQR;
    loginQRRequestInFlight.current = true;
    setBusy(`verify-login-qr-${current.account.id}`);
    try {
      const result = await api.verifyEgoLoginBatch(current.identity.id, [current.account.platformCode]);
      await refresh();
      const verified = result.accounts.find((account) => account.id === current.account.id);
      if (verified?.loginState !== 'logged_in') {
        toast.show('还未检测到登录完成，请确认平台页面已进入创作者后台', 'info');
        return;
      }
      announcedLoginAccounts.current.add(current.account.id);
      setActiveBatches((batches) => {
        const remaining = (batches[current.identity.id] ?? []).filter((code) => code !== current.account.platformCode);
        const next = { ...batches };
        if (remaining.length > 0) next[current.identity.id] = remaining;
        else delete next[current.identity.id];
        return next;
      });
      setLoginQR(null);
      toast.show(`${platformNames[current.account.platformCode] ?? current.account.name} 已登录，账号名称、ID 和头像已同步`, 'success');
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '登录状态同步失败', 'error');
    } finally {
      setBusy('');
      loginQRRequestInFlight.current = false;
    }
  }

  function closeLoginQR() {
    const accountID = loginQR?.account.id;
    setLoginQR(null);
    setLoginQRAutoRefreshError('');
    if (accountID) {
      void api.releaseEgoLoginQR(accountID).catch((error) => {
        toast.show(error instanceof Error ? error.message : '浏览器控制权释放失败', 'error');
      });
    }
  }

  useEffect(() => {
    if (loginQR?.result.state !== 'qr_ready' && loginQR?.result.state !== 'manual_takeover') return;
    let cancelled = false;
    let refreshing = false;
    let autoRefreshFailures = 0;
    let nextAutoRefreshAt = 0;
    const timer = window.setInterval(() => {
      if (refreshing || loginQRRequestInFlight.current || autoRefreshFailures >= 3 || Date.now() < nextAutoRefreshAt) return;
      const expiresAt = Date.parse(loginQR.result.expires_at || '');
      const refreshExpired = Number.isFinite(expiresAt) && Date.now() >= expiresAt - 3_000;
      if (!refreshExpired) return;
      refreshing = true;
      loginQRRequestInFlight.current = true;
      setLoginQRRefreshing(true);
      void api.openEgoLoginQR(loginQR.account.id, true, false)
        .then(async (result) => {
          if (cancelled) return;
          autoRefreshFailures = 0;
          nextAutoRefreshAt = 0;
          setLoginQRAutoRefreshError('');
          if (result.state === 'logged_in') {
            await api.verifyEgoLoginBatch(loginQR.identity.id, [loginQR.account.platformCode]);
            await Promise.all([mutateAccounts(), mutateAccountAssets()]);
            announcedLoginAccounts.current.add(loginQR.account.id);
            setLoginQR(null);
            toast.show(`${platformNames[loginQR.account.platformCode] ?? loginQR.account.name} 已登录，账号资料已同步`, 'success');
            return;
          }
          setLoginQR((current) => current ? { ...current, result } : current);
        })
        .catch(() => {
          if (cancelled) return;
          nextAutoRefreshAt = Date.now() + Math.min(60_000, 10_000 * 2 ** autoRefreshFailures);
          autoRefreshFailures += 1;
          if (autoRefreshFailures >= 3) {
            setLoginQRAutoRefreshError('自动刷新暂时失败，请点击“刷新二维码”重试');
          }
        })
        .finally(() => {
          refreshing = false;
          loginQRRequestInFlight.current = false;
          setLoginQRRefreshing(false);
        });
    }, 2_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [loginQR?.account.id, loginQR?.account.name, loginQR?.account.platformCode, loginQR?.identity.id, loginQR?.result.state, loginQR?.result.expires_at, loginQR?.result.image_data_url, mutateAccountAssets, mutateAccounts, toast]);

  const liveLoginQRState = loginQR ? modeAccounts.find((account) => account.id === loginQR.account.id)?.loginState : undefined;
  useEffect(() => {
    if (!loginQR || liveLoginQRState !== 'logged_in') return;
    announcedLoginAccounts.current.add(loginQR.account.id);
    setLoginQR(null);
    toast.show(`${platformNames[loginQR.account.platformCode] ?? loginQR.account.name} 已登录，账号资料已同步`, 'success');
  }, [liveLoginQRState, loginQR?.account.id, loginQR?.account.name, loginQR?.account.platformCode, toast]);

  useEffect(() => {
    const activeLoginBatches = Object.entries(activeBatches)
      .map(([rawIdentityID, platformCodes]) => ({ identityID: Number(rawIdentityID), platformCodes }))
      .filter(({ identityID, platformCodes }) => identityID > 0 && platformCodes.length > 0);
    if (activeLoginBatches.length === 0) return;

    for (const { identityID, platformCodes } of activeLoginBatches) {
      const remaining: string[] = [];
      let synced = 0;
      for (const platformCode of platformCodes) {
        const account = modeAccounts.find((item) => item.identityId === identityID && item.platformCode === platformCode);
        if (account?.loginState !== 'logged_in') {
          if (account) announcedLoginAccounts.current.delete(account.id);
          remaining.push(platformCode);
          continue;
        }
        if (!announcedLoginAccounts.current.has(account.id)) {
          announcedLoginAccounts.current.add(account.id);
          synced += 1;
          toast.show(`${platformNames[platformCode] ?? platformCode} 已登录，账号资料已同步`, 'success');
        }
      }
      setActiveBatches((current) => {
        const currentCodes = current[identityID];
        if (!currentCodes) return current;
        const nextCodes = currentCodes.filter((code) => remaining.includes(code));
        if (nextCodes.length === currentCodes.length) return current;
        const next = { ...current };
        if (nextCodes.length > 0) next[identityID] = nextCodes;
        else delete next[identityID];
        return next;
      });
      if (synced > 1) {
        toast.show(`本批已同步 ${synced} 个平台`, 'success');
      }
    }
  }, [activeBatches, modeAccounts, toast]);

  async function verifyLoginBatch(identity: EgoIdentity, identityAccounts: Account[]) {
    const codes = platformCodesForMode(activeBatches[identity.id] ?? identityAccounts
      .filter((account) => account.enabled && needsLoginAttention(account))
      .slice(0, 3)
      .map((account) => account.platformCode), productMode);
    if (codes.length === 0) return;
    await run(`verify-${identity.id}`, async () => {
      await api.verifyEgoLoginBatch(identity.id, codes);
      setActiveBatches((current) => {
        const next = { ...current };
        delete next[identity.id];
        return next;
      });
    }, '本批登录状态与账号资料已同步');
  }

  async function calibrate() {
    await run('calibrate', async () => {
      const result = await api.calibrateEgoConcurrency();
      setCapacity({ measured: result.measured_capacity, current: result.default_concurrency });
    }, '本机并发校准完成');
  }

  async function updateSessionMonitor(enabled: boolean) {
    setMonitorEnabled(enabled);
    setBusy('session-monitor-settings');
    try {
      const updated = await api.updateEgoSessionMonitor({ enabled, interval_minutes: monitorInterval });
      await mutateSessionMonitor(updated, { revalidate: false });
      toast.show(enabled ? '登录状态监控已开启' : '登录状态监控已关闭', 'success');
    } catch (error) {
      setMonitorEnabled(!enabled);
      toast.show(error instanceof Error ? error.message : '登录状态监控设置失败', 'error');
    } finally {
      setBusy('');
    }
  }


  async function runSyncVerify(identity: EgoIdentity) {
    setBusy(`sync-verify-${identity.id}`);
    try {
      await api.verifyEgoIdentity(identity.id);
      await refresh();
      toast.show(`已完成 ${identity.name} 平台登录状态核验与同步`, 'success');
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '核验失败', 'error');
    } finally {
      setBusy('');
    }
  }

  async function runSessionMonitor() {
    await run('session-monitor-run', api.runEgoSessionMonitor, '登录状态检查已完成');
  }

  function openLegacyImportModal() {
    const items = legacyAccounts?.items ?? [];
    setSelectedLegacyPlatforms(items.map((i) => i.platform_code));
    setTargetLegacyIdentityID(identities[0]?.id ?? 0);
    setLegacyModalOpen(true);
  }

  async function executeLegacyImport() {
    if (selectedLegacyPlatforms.length === 0) {
      toast.show('请至少勾选一个要导入的账号', 'info');
      return;
    }
    setBusy('legacy-accounts-import');
    try {
      const payload: { identity_id?: number; platform_codes?: string[] } = {
        platform_codes: selectedLegacyPlatforms,
      };
      if (targetLegacyIdentityID > 0) {
        payload.identity_id = targetLegacyIdentityID;
      }
      const result = await api.importEgoLegacyAccounts(payload);
      await Promise.all([refresh(), mutateLegacyAccounts()]);
      setLegacyModalOpen(false);
      toast.show(`已导入 ${result.imported} 个账号资料到指定浏览器`, 'success');
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '旧账号资料导入失败', 'error');
    } finally {
      setBusy('');
    }
  }

  return (
    <section className="w-full space-y-4 px-1 sm:px-2" data-testid="ego-workspace">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border pb-3">
        <div>
          <h3 className="text-lg font-bold text-brand-fg">{productMode === 'shop' ? '店铺管理' : '账号管理'}</h3>
          <p className="mt-1 text-xs text-gray-500">
            {accountDataReady ? (
              <>
                {`${identities.length} 个浏览器 · `}
                {(() => {
                  // 用全部账号统计，而不是当前模式可见的账号：店铺模式关闭时
                  // 电商后台会被过滤掉，只统计可见的会让人以为「只有这么几个账号」。
                  const enabled = accounts.filter((item) => item.enabled);
                  const loggedIn = enabled.filter((item) => item.loginState === 'logged_in').length;
                  const pending = enabled.filter((item) => item.loginState === 'unknown').length;
                  const out = enabled.filter((item) => item.loginState === 'logged_out').length;
                  return (
                    <span data-testid="ego-login-summary">
                      <span className="text-brand-success">{loggedIn} 已登录</span>
                      {out > 0 ? <span className="text-gray-500"> · {out} 已登出</span> : null}
                      {pending > 0 ? (
                        <span className="text-brand-warning" title="这些平台还没有审核过的登录特征，无法自动判断；在浏览器里打开确认即可。">
                          {' '}· {pending} 待确认
                        </span>
                      ) : null}
                    </span>
                  );
                })()}
              </>
            ) : (
              '正在读取账号记录'
            )}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className={`hidden items-center gap-1.5 text-[11px] sm:flex ${doctor?.available ? 'text-brand-success' : 'text-gray-500'}`}>
            <span className={`h-1.5 w-1.5 rounded-full ${doctor?.available ? 'bg-brand-success' : doctorError ? 'bg-brand-danger' : 'bg-brand-border'}`} />
            {doctor?.available ? 'EgoLite 已连接' : doctorError ? 'EgoLite 不可用' : '正在连接'}
          </span>
          <button className="ui-btn h-9 px-3" aria-label="账号配置" disabled={configurationUnavailable || Boolean(busy)} onClick={() => setPanel('settings')}>
            <ShieldCheck className="h-4 w-4" />配置
          </button>
          <button className="ui-btn ui-btn-primary h-9 px-3" aria-label="添加账号" disabled={configurationUnavailable || Boolean(busy)} onClick={openNewAccount}>
            <Plus className="h-4 w-4" />添加账号
          </button>
        </div>
      </header>

      {accountDataError ? (
        <div role="alert" className="ui-card flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs text-brand-warning" data-testid="ego-accounts-error">
          <span>{accountDataReady ? '同步暂时失败，保留上次读取的账号；恢复后再编辑配置。' : '账号记录暂时无法读取，请重试；这不代表账号已删除或掉线。'}</span>
          <button className="ui-btn h-8" onClick={() => void Promise.allSettled([mutateIdentities(), mutateAccounts()])}><RefreshCw className="h-3.5 w-3.5" />重试</button>
        </div>
      ) : null}
      <div className="space-y-3">
        {!accountDataReady ? (
          !accountDataError ? <div role="status" className="ui-card px-6 py-12 text-center text-sm text-gray-500">正在读取账号记录…</div> : null
        ) : grouped.length === 0 ? (
          <div className="ui-card flex min-h-52 flex-col items-center justify-center px-6 text-center">
            <CircleUserRound className="h-7 w-7 text-gray-300" />
            <p className="mt-3 text-sm font-semibold text-gray-900">还没有账号</p>
            <p className="mt-1 text-xs text-gray-500">添加一个浏览器并选择需要登录的平台。</p>
            <button className="ui-btn ui-btn-primary mt-4" onClick={openNewAccount}><Plus className="h-4 w-4" />添加账号</button>
          </div>
        ) : grouped.map(({ identity, accounts: identityAccounts }, identityIndex) => {
          const loggedInCount = identityAccounts.filter((account) => account.loginState === 'logged_in').length;
          const attentionCount = identityAccounts.filter((account) => account.enabled && (needsLoginAttention(account) || duplicateAccountIDs.has(account.id))).length;
          return (
            <article key={identity.id} className="rounded-xl border border-brand-border bg-brand-surface transition-colors duration-200 overflow-hidden" data-testid={`ego-identity-${identity.id}`}>
              {/* 极简浏览器头部条 */}
              <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-3 border-b border-brand-border">
                <div className="flex items-center gap-3">
                  <BrowserIdentityMark identity={identity} index={identityIndex} className="h-10 w-10" />
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-bold text-gray-900">{identity.name}</span>
                      {identity.profile_id && (
                        <span className="rounded-md bg-white border border-brand-border px-2 py-0.5 text-[10px] font-semibold text-gray-600 shadow-2xs">
                          {identity.profile_name || identity.profile_id}
                        </span>
                      )}
                    </div>
                    <p className="text-[11px] text-gray-500 font-medium">
                      {loggedInCount}/{identityAccounts.length} 已登录
                      {attentionCount > 0 ? <span className="text-brand-warning font-semibold"> · {attentionCount} 个待处理</span> : null}
                    </p>
                  </div>
                </div>

                {/* 扁平克制的操作按钮组 */}
                <div className="flex items-center gap-1.5">
                  <button
                    className="inline-flex items-center gap-1.5 rounded-lg border border-brand-border bg-brand-surface-raised px-3 py-1.5 text-xs font-semibold text-brand-fg transition hover:border-[color-mix(in_srgb,var(--brand-fg)_28%,var(--brand-border))] disabled:opacity-40"
                    aria-label={`打开 ${identity.name}`}
                    disabled={Boolean(busy) || identityAccounts.length === 0 || !doctor?.available}
                    onClick={() => void openLoggedInPlatforms(identity, identityAccounts)}
                  >
                    <Monitor className="h-3.5 w-3.5 text-gray-500" />
                    {busy === `open-browser-${identity.id}` ? '启动中…' : '打开浏览器'}
                  </button>

                  <button
                    className="inline-flex items-center gap-1 rounded-lg border border-brand-border bg-transparent px-2.5 py-1.5 text-xs font-medium text-brand-muted transition hover:bg-brand-surface-raised hover:text-brand-fg disabled:opacity-40"
                    title="核验登录状态并自动同步"
                    disabled={Boolean(busy) || identityAccounts.length === 0}
                    onClick={() => void runSyncVerify(identity)}
                  >
                    <RefreshCw className={`h-3.5 w-3.5 ${busy === `sync-verify-${identity.id}` ? 'animate-spin text-brand-fg' : 'text-gray-400'}`} />
                    <span>{busy === `sync-verify-${identity.id}` ? '检测中' : '同步'}</span>
                  </button>

                  <button
                    className="inline-flex items-center gap-1 rounded-lg border border-brand-border bg-transparent px-2.5 py-1.5 text-xs font-medium text-brand-muted transition hover:bg-brand-surface-raised hover:text-brand-fg disabled:opacity-40"
                    onClick={() => openPlatformManager(identity.id)}
                    aria-label={`配置 ${identity.name} 平台`}
                    title="配置此浏览器的平台"
                    disabled={configurationUnavailable}
                  >
                    <Settings2 className="h-3.5 w-3.5 text-gray-400" />
                    <span>配置</span>
                  </button>

                  {activeBatches[identity.id]?.length ? (
                    <button
                      className="inline-flex items-center gap-1 rounded-lg bg-brand-accent px-3 py-1.5 text-xs font-bold text-brand-on-accent shadow-sm transition hover:bg-brand-accent-hover"
                      disabled={Boolean(busy)}
                      onClick={() => void verifyLoginBatch(identity, identityAccounts)}
                    >
                      <Check className="h-3.5 w-3.5" />完成登录
                    </button>
                  ) : null}
                </div>
              </div>

              {/* 极简平台网格（Grid of Tiles） */}
              {identityAccounts.length === 0 ? (
                <div className="flex items-center justify-center p-8 text-xs text-gray-400">
                  <button
                    className="inline-flex items-center gap-1.5 rounded-lg border border-dashed border-brand-border px-4 py-2 font-medium text-brand-muted transition hover:border-[color-mix(in_srgb,var(--brand-fg)_32%,transparent)] hover:bg-brand-surface-raised"
                    onClick={() => openPlatformManager(identity.id)}
                  >
                    <Plus className="h-3.5 w-3.5" />
                    为此浏览器添加平台
                  </button>
                </div>
              ) : (
                <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6 gap-3 p-4">
                  {identityAccounts.map((account: Account) => {
                    const duplicateBinding = duplicateAccountIDs.has(account.id);
                    const primaryIdentity = account.platformCode === 'tiktok'
                      ? (account.accountIdentifier || account.name)
                      : account.name;
                    const secondaryIdentity = account.platformCode === 'tiktok' ? '' : account.accountIdentifier;
                    // secondary identity placeholder (e.g. 抖音号 / xhs id)；TikTok 不展示简介
                    void secondaryIdentity;
                    const isLoggedIn = account.loginState === 'logged_in' && !needsLoginAttention(account);
                    const rawName = (account.loginState === 'logged_in' || account.accountIdentifier ? primaryIdentity : '登录后自动识别').trim();
                    const displayName = !rawName || rawName.toLowerCase() === 'undefined' ? `${platformNames[account.platformCode] ?? account.platformCode} 账号` : rawName;
                    const identityAssets = accountAssetsByAccount.get(account.id) ?? [];

                    return (
                      <div
                        key={account.id}
                        data-testid={`ego-platform-account-${account.id}`}
                        className="group relative flex items-center justify-between gap-3 rounded-xl border border-brand-border bg-brand-surface p-3 transition-colors duration-150 hover:border-[color-mix(in_srgb,var(--brand-fg)_26%,var(--brand-border))]"
                        onContextMenu={(e) => {
                          e.preventDefault();
                          void openSinglePlatform(identity, account);
                        }}
                      >
                        {/* 左侧：始终清晰呈现平台标识与账号身份 */}
                        <div className="relative shrink-0">
                          {account.platformCode === 'meta' ? (
                            <div className="relative flex h-10 w-10 items-center justify-center rounded-xl bg-brand-surface-raised border border-brand-border shadow-2xs" data-testid={`ego-account-platform-${account.id}`}>
                              <PlatformIcon platform={account.platformCode} className="h-3 w-3" />
                              <span
                                className={`absolute -bottom-0.5 -right-0.5 h-3 w-3 rounded-full border-2 border-brand-surface ${
                                  isLoggedIn
                                    ? 'bg-brand-success'
                                    : duplicateBinding
                                      ? 'bg-brand-warning/20'
                                      : 'bg-brand-border'
                                }`}
                                title={isLoggedIn ? '已连接登录' : duplicateBinding ? '重复绑定' : '未登录'}
                              />
                            </div>
                          ) : account.avatar ? (
                            <div className="relative h-10 w-10">
                              <AccountAvatar
                                testId={`ego-account-avatar-${account.id}`}
                                src={account.avatar}
                                platform={account.platformCode}
                                size="lg"
                                alt={`${account.name} 头像`}
                                className="h-10 w-10 rounded-xl object-cover ring-1 ring-brand-border"
                              />
                              {/* 平台 Logo 徽标始终保持在右下角 */}
                              <span
                                className="absolute -bottom-1 -right-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-white shadow-xs border border-brand-border"
                                title={platformNames[account.platformCode] ?? account.platformCode}
                              >
                                <PlatformIcon platform={account.platformCode} className="h-2.5 w-2.5" />
                              </span>
                            </div>
                          ) : (
                            <div className="relative flex h-10 w-10 items-center justify-center rounded-xl bg-brand-surface-raised border border-brand-border shadow-2xs">
                              <PlatformIcon platform={account.platformCode} className="h-6 w-6" />
                              {/* 右下角状态微点 */}
                              <span
                                className={`absolute -bottom-0.5 -right-0.5 h-3 w-3 rounded-full border-2 border-brand-surface ${
                                  isLoggedIn
                                    ? 'bg-brand-success'
                                    : duplicateBinding
                                      ? 'bg-brand-warning/20'
                                      : 'bg-brand-border'
                                }`}
                                title={isLoggedIn ? '已连接登录' : duplicateBinding ? '重复绑定' : '未登录'}
                              />
                            </div>
                          )}
                        </div>

                        {/* 中间：平台名称与账号 */}
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-1.5">
                            <span className="truncate text-xs font-bold text-gray-900">
                              {platformNames[account.platformCode] ?? account.platformCode}
                            </span>
                            {duplicateBinding ? (
                              <span className="rounded bg-brand-warning/15 px-1 text-[9px] font-bold text-brand-warning">重复绑定</span>
                            ) : null}
                            {secondaryIdentity ? (
                              <span className="truncate text-[10px] font-medium text-gray-400">{secondaryIdentity}</span>
                            ) : null}
                          </div>
                          <p className="truncate text-[11px] font-medium text-gray-500">
                            {displayName}
                            {account.loginState === 'unknown' ? (
                              <span
                                className="ml-1 inline-flex items-center gap-1 text-brand-warning"
                                title={loginPendingHint(account)}
                              >
                                待确认
                                {account.loginEvidenceCode ? (
                                  <span className="text-[9px] font-normal text-gray-400">
                                    {loginEvidenceLabel(account.loginEvidenceCode)}
                                  </span>
                                ) : null}
                              </span>
                            ) : null}
                          </p>
                          {/* Meta 等平台附带的多账号资产（头像 + 显式平台名）独立展示，避免与主账号混淆 */}
                          {account.platformCode === 'meta' && identityAssets.length > 0 && (
                            <div
                              data-testid={`meta-browser-assets-${account.id}`}
                              className="mt-1.5 flex flex-wrap gap-1.5"
                            >
                              {identityAssets.map((asset) => (
                                <span
                                  key={asset.id}
                                  className="inline-flex items-center gap-1 rounded-md border border-brand-border bg-brand-surface px-1.5 py-0.5"
                                >
                                  {asset.avatar ? (
                                    <img src={asset.avatar} alt="" className="h-4 w-4 rounded object-cover" />
                                  ) : null}
                                  <PlatformIcon platform={asset.platform_code} className="h-3 w-3" />
                                  <span className="text-[10px] font-medium text-gray-600">
                                    {platformNames[asset.platform_code] ?? asset.platform_code}
                                  </span>
                                </span>
                              ))}
                            </div>
                          )}
                        </div>

                        {/* 右侧：悬浮或精简动作 */}
                        <div className="flex items-center gap-1 shrink-0">
                          <button
                            type="button"
                            className="inline-flex h-7 w-7 items-center justify-center rounded-lg text-gray-400 opacity-60 transition group-hover:opacity-100 hover:bg-gray-100 hover:text-brand-muted"
                            title="在浏览器中打开主页"
                            onClick={(e) => {
                              e.stopPropagation();
                              void openSinglePlatform(identity, account);
                            }}
                          >
                            <ExternalLink className="h-3.5 w-3.5" />
                          </button>

                          {!isLoggedIn && (
                            <button
                              type="button"
                              className="inline-flex h-6 items-center rounded-md bg-brand-accent px-2 text-[10px] font-bold text-brand-on-accent transition hover:bg-brand-accent-hover"
                              onClick={() => void (duplicateBinding ? switchDuplicateAccount(identity, account) : openPlatformLogin(identity, account))}
                            >
                              {loginQR?.account.id === account.id ? '登录中' : duplicateBinding ? '重绑' : '登录'}
                            </button>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </article>
          );
        })}
      </div>

      {loginQR ? (
        <AccountDialog
          label={`${platformNames[loginQR.account.platformCode] ?? loginQR.account.name}扫码登录`}
          onClose={closeLoginQR}
        >
          <div className="flex items-center justify-between border-b border-brand-border px-5 py-4">
            <div className="flex items-center gap-3">
              <span className="flex h-10 w-10 items-center justify-center rounded-xl border border-brand-border bg-gray-50">
                <PlatformIcon platform={loginQR.account.platformCode} className="h-6 w-6" />
              </span>
              <div>
                <h4 className="text-sm font-bold text-brand-fg">{platformNames[loginQR.account.platformCode] ?? loginQR.account.name}登录</h4>
                <p className="mt-0.5 text-[11px] text-gray-500">{loginQR.identity.name}</p>
              </div>
            </div>
            <button autoFocus className="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-800" aria-label="关闭" onClick={closeLoginQR}>
              <X className="h-4 w-4" />
            </button>
          </div>
          {loginQR.result.state === 'qr_ready' && loginQR.result.image_data_url ? (
            <div className="flex flex-col items-center px-6 py-7 text-center">
              <div className="rounded-2xl border border-gray-200 bg-white p-3 shadow-sm">
                <img
                  src={loginQR.result.image_data_url}
                  alt={`${platformNames[loginQR.account.platformCode] ?? loginQR.account.name}登录二维码`}
                  className="h-64 w-64 object-contain"
                />
              </div>
              <p className="mt-5 text-sm font-semibold text-gray-900">请使用对应平台 App 扫码</p>
              <p className="mt-1 text-xs text-gray-500">每 45 秒自动同步新二维码 · 扫码后自动识别账号名称、ID 和头像</p>
              {loginQRAutoRefreshError ? (
                <p role="alert" className="mt-2 text-xs font-medium text-red-600">{loginQRAutoRefreshError}</p>
              ) : null}
              <button type="button" className="ui-btn mt-5" disabled={loginQRRefreshing} onClick={() => void refreshLoginQR()}>
                <RefreshCw className={`h-3.5 w-3.5 ${loginQRRefreshing ? 'animate-spin' : ''}`} />
                {loginQRRefreshing ? '检测中' : '刷新二维码'}
              </button>
              <button
                type="button"
                className="ui-btn ui-btn-primary mt-3"
                disabled={Boolean(busy) || loginQRRefreshing}
                onClick={() => void completeLoginQR()}
              >
                <Check className="h-3.5 w-3.5" />
                {busy === `verify-login-qr-${loginQR.account.id}` ? '同步中…' : '完成登录并同步'}
              </button>
            </div>
          ) : (
            <div className="px-6 py-8 text-center">
              <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-brand-warning/10 text-brand-warning">
                <LogIn className="h-5 w-5" />
              </div>
              <p className="mt-4 text-sm font-semibold text-gray-900">此平台需要在 EgoLite 完成登录</p>
              <p className="mt-1 text-xs leading-5 text-gray-500">在 EgoLite 完成扫码、验证码、密码或授权；可把当前页二维码同步回这里。</p>
              {loginQR.result.reason_code === 'QR_UNAVAILABLE' ? (
                <p className="mt-2 text-xs font-medium text-brand-warning">未识别到二维码，请在 EgoLite 切换到{platformNames[loginQR.account.platformCode] ?? loginQR.account.name}扫码页面后再同步。</p>
              ) : null}
              <div className="mt-5 flex flex-wrap items-center justify-center gap-2">
                <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={closeLoginQR}>稍后处理</button>
                <button
                  type="button"
                  className="ui-btn"
                  disabled={Boolean(busy) || loginQRRefreshing}
                  onClick={() => void refreshLoginQR(true)}
                >
                  <Monitor className="h-3.5 w-3.5" />
                  {loginQRRefreshing ? '同步中…' : '在 EgoLite 显示并同步二维码'}
                </button>
                <button
                  type="button"
                  className="ui-btn ui-btn-primary"
                  disabled={Boolean(busy) || loginQRRefreshing}
                  onClick={() => void completeLoginQR()}
                >
                  <Check className="h-3.5 w-3.5" />
                  {busy === `verify-login-qr-${loginQR.account.id}` ? '同步中…' : '完成登录并同步'}
                </button>
              </div>
            </div>
          )}
        </AccountDialog>
      ) : null}

      {panel ? (
        <AccountDialog
          label={panel === 'add' ? '添加账号' : panel === 'platforms' ? `${activeIdentity?.name ?? '浏览器'} ${productMode === 'shop' ? '店铺' : '平台'}管理` : '账号配置'}
          onClose={() => setPanel(null)}
        >
            <div className="sticky top-0 z-10 flex items-center justify-between border-b border-brand-border bg-white px-5 py-4">
              <div>
                <h4 className="text-sm font-bold text-brand-fg">{panel === 'add' ? '添加账号' : panel === 'platforms' ? (productMode === 'shop' ? '店铺管理' : '平台管理') : '账号配置'}</h4>
                <p className="mt-0.5 text-[11px] text-gray-500">
                  {panel === 'add'
                    ? '新建浏览器并选择需要登录的平台'
                    : panel === 'platforms' ? `${activeIdentity?.name ?? '浏览器'} · 添加或移除${productMode === 'shop' ? '店铺' : '平台'}` : '低频设置集中在这里'}
                </p>
              </div>
              <button autoFocus className="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-800" aria-label="关闭" onClick={() => setPanel(null)}><X className="h-4 w-4" /></button>
            </div>

            {panel === 'add' || panel === 'platforms' ? (
              <div className="space-y-5 p-5">
                {panel === 'add' ? (
                  <div className="rounded-xl border border-brand-border bg-gray-50 p-3">
                    <label className="block text-xs font-semibold text-gray-700">
                      浏览器名称 <span className="font-normal text-gray-400">（可选）</span>
                      <input className="ui-input mt-2 w-full bg-white" value={identityName} onChange={(event) => setIdentityName(event.target.value)} placeholder={`留空将自动命名为 浏览器${identities.length + 1}`} maxLength={80} />
                    </label>
                  </div>
                ) : (
                  <div className="flex items-center gap-3 rounded-xl border border-brand-border bg-brand-surface-raised p-3">
                    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-brand-accent text-brand-on-accent">
                      <Monitor className="h-4 w-4" />
                    </span>
                    <div className="min-w-0">
                      <p className="truncate text-xs font-bold text-brand-fg">{activeIdentity?.name}</p>
                      <p className="mt-0.5 text-[10px] text-brand-muted">{selectedPlatforms.length} 个{productMode === 'shop' ? '店铺' : '平台'}已配置</p>
                    </div>
                  </div>
                )}
                <div className="space-y-4">
                  <div className="flex items-center justify-between">
                    <p className="text-xs font-bold text-gray-900">选择{productMode === 'shop' ? '店铺' : '平台'}（按分类组织）</p>
                    <span className="text-[10px] text-gray-400">已选 {selectedPlatforms.length} 个</span>
                  </div>
                  {(['multimedia', 'article', 'ecommerce'] as const).map((catKey) => {
                    const cat = PLATFORM_CATEGORIES[catKey];
                    const catCodes = cat.codes.filter((c) => visiblePlatformCodes.includes(c));
                    if (catCodes.length === 0) return null;
                    const selectedInCat = catCodes.filter((c) => selectedPlatforms.includes(c));
                    const allSelected = selectedInCat.length === catCodes.length;
                    return (
                      <div key={catKey} className="rounded-xl border border-brand-border bg-brand-surface-raised p-3">
                        <div className="mb-2 flex items-center justify-between border-b border-brand-border/60 pb-1.5">
                          <span className="flex items-center gap-1.5 text-xs font-bold text-gray-800">
                            <span>{cat.icon}</span>
                            <span>{cat.label}</span>
                            <span className="rounded-full bg-black/5 px-1.5 py-0.2 text-[10px] font-normal text-gray-500">
                              {selectedInCat.length}/{catCodes.length}
                            </span>
                          </span>
                          <button
                            type="button"
                            className="text-[10px] font-semibold text-brand-muted hover:text-brand-muted"
                            onClick={() => {
                              if (allSelected) {
                                setSelectedPlatforms((cur) => cur.filter((c) => !catCodes.includes(c)));
                              } else {
                                setSelectedPlatforms((cur) => Array.from(new Set([...cur, ...catCodes])));
                              }
                            }}
                          >
                            {allSelected ? '取消全选' : '全选此类'}
                          </button>
                        </div>
                        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                          {catCodes.map((code) => (
                            <label
                              key={code}
                              className={`flex cursor-pointer items-center gap-2 rounded-lg border px-2 py-1.5 text-[11px] transition ${
                                selectedPlatforms.includes(code)
                                  ? 'border-brand-accent bg-brand-accent/10 font-semibold text-brand-fg'
                                  : 'border-brand-border bg-white text-gray-700 hover:bg-gray-100'
                              }`}
                            >
                              <input
                                type="checkbox"
                                aria-label={`选择${platformNames[code] ?? code}`}
                                checked={selectedPlatforms.includes(code)}
                                onChange={(event) =>
                                  setSelectedPlatforms((current) =>
                                    event.target.checked
                                      ? [...current, code]
                                      : current.filter((item) => item !== code)
                                  )
                                }
                              />
                              <PlatformIcon platform={code} className="h-4 w-4 shrink-0" />
                              <span className="truncate">{platformNames[code] ?? code}</span>
                            </label>
                          ))}
                        </div>
                      </div>
                    );
                  })}
                </div>
                <div className="flex justify-end gap-2 border-t border-brand-border pt-4">
                  <button className="ui-btn" onClick={() => setPanel(null)}>取消</button>
                  <button className="ui-btn ui-btn-primary" disabled={configurationUnavailable || Boolean(busy) || (panel === 'add' && selectedPlatforms.length === 0)} onClick={() => void saveAccountConfiguration()}>
                    <Check className="h-4 w-4" />{panel === 'add' ? '保存账号' : '保存平台'}
                  </button>
                </div>
              </div>
            ) : (
              <div className="divide-y divide-brand-border">
                <section className="p-5">
                  <p className="text-xs font-bold text-gray-900">浏览器名称与顺序</p>
                  <div className="mt-3 space-y-2">
                    {identities.map((identity, index) => (
                      <div key={identity.id} className="flex items-center gap-2 rounded-lg border border-brand-border px-3 py-2">
                        {editingIdentityID === identity.id ? (
                          <>
                            <input className="ui-input h-8 min-w-0 flex-1" aria-label="浏览器名称" value={editingIdentityName} maxLength={80} autoFocus onChange={(event) => setEditingIdentityName(event.target.value)} />
                            <button className="ui-btn h-8 w-8 p-0" aria-label="保存浏览器名称" disabled={!editingIdentityName.trim() || Boolean(busy)} onClick={() => void renameIdentity()}><Check className="h-3.5 w-3.5" /></button>
                            <button className="ui-btn h-8 w-8 p-0" aria-label="取消修改名称" onClick={() => setEditingIdentityID(0)}><X className="h-3.5 w-3.5" /></button>
                          </>
                        ) : (
                          <>
                            <div className="min-w-0 flex-1">
                              <span className="truncate text-xs font-semibold text-gray-800">{identity.name}</span>
                              <div className="mt-1 flex items-center gap-1.5 text-[10px]">
                                <span className="text-gray-400 shrink-0">EgoLite:</span>
                                <select
                                  aria-label={`为 ${identity.name} 绑定 EgoLite Profile`}
                                  value={identity.profile_id || ''}
                                  onChange={async (e) => {
                                    const pid = e.target.value;
                                    const prof = (egoProfilesData?.profiles ?? []).find((p) => p.id === pid);
                                    try {
                                      await api.updateEgoIdentityProfile(identity.id, pid, prof?.name || pid);
                                      await refresh();
                                      toast.show(`${identity.name} 已绑定 ${prof?.name || pid || '未绑定'}`, 'success');
                                    } catch (err) {
                                      toast.show(err instanceof Error ? err.message : '绑定失败', 'error');
                                    }
                                  }}
                                  className="h-6 rounded border border-brand-border bg-white px-1.5 text-[10px] text-gray-700 focus:outline-none"
                                >
                                  <option value="">未绑定</option>
                                  {(egoProfilesData?.profiles ?? []).map((p) => (
                                    <option key={p.id} value={p.id}>
                                      {p.id} ({p.name || '未命名'}){p.isDefault ? ' [默认]' : ''}
                                    </option>
                                  ))}
                                </select>
                              </div>
                            </div>
                            <button className="rounded-md p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-700" aria-label={`编辑 ${identity.name} 名称`} onClick={() => {
                              setEditingIdentityID(identity.id);
                              setEditingIdentityName(identity.name);
                            }}><SquarePen className="h-3.5 w-3.5" /></button>
                            <button className="rounded-md p-1.5 text-gray-400 hover:bg-gray-100 disabled:opacity-30" aria-label={`上移 ${identity.name}`} disabled={index === 0 || Boolean(busy)} onClick={() => void moveIdentity(identity, -1)}><ArrowUp className="h-3.5 w-3.5" /></button>
                            <button className="rounded-md p-1.5 text-gray-400 hover:bg-gray-100 disabled:opacity-30" aria-label={`下移 ${identity.name}`} disabled={index === identities.length - 1 || Boolean(busy)} onClick={() => void moveIdentity(identity, 1)}><ArrowDown className="h-3.5 w-3.5" /></button>
                          </>
                        )}
                      </div>
                    ))}
                  </div>
                </section>

                {legacyAccounts?.available && legacyAccounts.count > 0 ? (
                  <section className="p-5">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <p className="text-xs font-bold text-gray-900">旧账号资料</p>
                        <p className="mt-1 text-[11px] leading-5 text-gray-500">发现 {legacyAccounts.count} 个旧 Chromium 账号记录。只导入名称和公开账号 ID，登录态不会迁移。</p>
                      </div>
                      <button className="ui-btn shrink-0 ui-btn-primary" aria-label="按需勾选导入旧账号资料" disabled={Boolean(busy)} onClick={() => openLegacyImportModal()}><Download className="h-4 w-4" />按需导入</button>
                    </div>
                  </section>
                ) : null}

                <section className="p-5">
                  <div className="flex items-center justify-between gap-4">
                    <div>
                      <p className="text-xs font-bold text-gray-900">EgoLite 书签栏</p>
                      <p className="mt-1 text-[11px] text-gray-500">平台选择后自动同步：媒体账号与已选商城平台分别写入浏览器书签栏。</p>
                    </div>
                    <button className="ui-btn shrink-0" disabled={Boolean(busy)} onClick={() => void run('bookmarks', api.importEgoBookmarks, '浏览器 1 媒体与商城书签已同步')}><BookmarkPlus className="h-4 w-4" />同步</button>
                  </div>
                </section>

                <section className="p-5">
                  <div className="flex items-center justify-between gap-4">
                    <div>
                      <p className="flex items-center gap-2 text-xs font-bold text-gray-900"><Clock3 className="h-4 w-4 text-brand-fg" />登录状态监控</p>
                      <p className="mt-1 text-[11px] text-gray-500">串行检查，不创建新 Space；unknown 保留原状态。</p>
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-label="开启登录状态监控"
                      aria-checked={monitorEnabled}
                      disabled={!sessionMonitor || Boolean(busy)}
                      className={`relative h-6 w-11 shrink-0 rounded-full transition-colors disabled:opacity-40 ${monitorEnabled ? 'bg-brand-accent' : 'bg-brand-border'}`}
                      onClick={() => void updateSessionMonitor(!monitorEnabled)}
                    >
                      <span className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform ${monitorEnabled ? 'translate-x-5' : 'translate-x-0'}`} />
                    </button>
                  </div>
                  <div className="mt-3 flex gap-2">
                    <input type="number" className="ui-input min-w-0 flex-1" aria-label="登录状态检查间隔" min={1} max={1440} value={monitorInterval} disabled={!monitorEnabled || Boolean(busy)} onChange={(event) => setMonitorInterval(Math.max(1, Math.min(1440, Number(event.target.value) || 1)))} />
                    <button className="ui-btn" aria-label="保存检查间隔" disabled={!monitorEnabled || Boolean(busy)} onClick={() => void updateSessionMonitor(true)}>保存间隔</button>
                    <button className="ui-btn" disabled={Boolean(busy) || sessionMonitor?.state === 'running' || !doctor?.available} onClick={() => void runSessionMonitor()}>
                      <RefreshCw className={`h-4 w-4 ${busy === 'session-monitor-run' || sessionMonitor?.state === 'running' ? 'animate-spin' : ''}`} />检查
                    </button>
                  </div>
                </section>

                <section className="p-5">
                  <div className="flex items-center justify-between gap-4">
                    <div>
                      <p className="flex items-center gap-2 text-xs font-bold text-gray-900"><Gauge className="h-4 w-4 text-brand-warning" />并发校准</p>
                      <p className="mt-1 text-[11px] text-gray-500">当前 {capacity?.current ?? 3}，最多 3 个活跃 Space。</p>
                    </div>
                    <button className="ui-btn shrink-0" disabled={Boolean(busy) || !doctor?.available} onClick={() => void calibrate()}><RefreshCw className={`h-4 w-4 ${busy === 'calibrate' ? 'animate-spin' : ''}`} />运行</button>
                  </div>
                </section>
              </div>
            )}
        </AccountDialog>
      ) : null}

      {legacyModalOpen ? (
        <AccountDialog
          label="按需导入旧账号资料"
          onClose={() => setLegacyModalOpen(false)}
        >
          <div className="flex items-center justify-between border-b border-brand-border px-5 py-4">
            <div>
              <h4 className="text-sm font-bold text-brand-fg">选择要导入的旧账号</h4>
              <p className="mt-0.5 text-[11px] text-gray-500">
                可多选或单选，直接汇入选定的浏览器中
              </p>
            </div>
            <button
              autoFocus
              className="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-800"
              aria-label="关闭"
              onClick={() => setLegacyModalOpen(false)}
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          <div className="space-y-4 p-5">
            <div className="rounded-xl border border-brand-border bg-gray-50 p-3.5">
              <label className="block text-xs font-semibold text-gray-700">
                导入目标浏览器框
                <select
                  aria-label="选择导入目标浏览器"
                  value={targetLegacyIdentityID}
                  onChange={(e) => setTargetLegacyIdentityID(Number(e.target.value))}
                  className="ui-input mt-2 w-full bg-white text-xs font-medium"
                >
                  {identities.map((ident) => (
                    <option key={ident.id} value={ident.id}>
                      {ident.name} {ident.profile_id ? `(${ident.profile_name || ident.profile_id})` : ''}
                    </option>
                  ))}
                  <option value={0}>+ 自动创建新浏览器框</option>
                </select>
              </label>
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between px-1">
                <span className="text-xs font-semibold text-gray-700">
                  可导入账号列表 ({selectedLegacyPlatforms.length}/{(legacyAccounts?.items ?? []).length})
                </span>
                <button
                  type="button"
                  className="text-xs font-semibold text-brand-muted hover:text-brand-muted"
                  onClick={() => {
                    const allCodes = (legacyAccounts?.items ?? []).map((i) => i.platform_code);
                    if (selectedLegacyPlatforms.length === allCodes.length) {
                      setSelectedLegacyPlatforms([]);
                    } else {
                      setSelectedLegacyPlatforms(allCodes);
                    }
                  }}
                >
                  {selectedLegacyPlatforms.length === (legacyAccounts?.items ?? []).length ? '取消全选' : '全选'}
                </button>
              </div>

              <div className="max-h-64 space-y-1.5 overflow-y-auto rounded-xl border border-brand-border p-2">
                {(legacyAccounts?.items ?? []).map((item) => {
                  const checked = selectedLegacyPlatforms.includes(item.platform_code);
                  return (
                    <label
                      key={item.platform_code}
                      className={`flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 text-xs transition ${
                        checked
                          ? 'border-brand-accent bg-brand-accent/10 text-brand-fg font-medium'
                          : 'border-brand-border bg-white text-gray-700 hover:bg-gray-50'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) => {
                          const code = item.platform_code;
                          setSelectedLegacyPlatforms((cur) =>
                            e.target.checked ? [...cur, code] : cur.filter((c) => c !== code)
                          );
                        }}
                      />
                      <PlatformIcon platform={item.platform_code} className="h-5 w-5 shrink-0" />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="font-bold text-gray-900">{platformNames[item.platform_code] ?? item.platform_code}</span>
                          <span className="truncate text-gray-600">{item.name || '未命名'}</span>
                        </div>
                        {item.account_identifier ? (
                          <p className="ui-lining-nums truncate text-[10px] text-gray-400">{item.account_identifier}</p>
                        ) : null}
                      </div>
                    </label>
                  );
                })}
              </div>
            </div>

            <div className="flex justify-end gap-2 border-t border-brand-border pt-4">
              <button className="ui-btn" onClick={() => setLegacyModalOpen(false)}>取消</button>
              <button
                className="ui-btn ui-btn-primary"
                disabled={Boolean(busy) || selectedLegacyPlatforms.length === 0}
                onClick={() => void executeLegacyImport()}
              >
                <Check className="h-4 w-4" />确认导入 ({selectedLegacyPlatforms.length})
              </button>
            </div>
          </div>
        </AccountDialog>
      ) : null}

    </section>
  );
}
