import type { Account, AccountAsset } from './api';

const requiredMetaAssetTypes: Record<string, string> = {
  meta: '__meta_publish_target__',
  facebook: 'facebook_page',
  instagram: 'instagram_account',
};
const metaPlatformCodes = ['facebook', 'instagram'] as const;

export type MetaPublishPlatformCode = typeof metaPlatformCodes[number];

export type MetaPublishTargetState = {
  platformCode: MetaPublishPlatformCode;
  account?: Account;
  assets: AccountAsset[];
  missingReason: '' | '缺少公开 ID' | '未配置发布目标';
};

export type MetaPublishGroup = {
  key: string;
  name: string;
  accounts: Account[];
  targets: Record<MetaPublishPlatformCode, MetaPublishTargetState>;
};

function isMetaAccount(account: Account) {
  return account.platformCode === 'meta' || account.platformCode === 'facebook' || account.platformCode === 'instagram';
}

function hasUsableAccountName(account: Account | undefined) {
  const name = account?.name.trim() ?? '';
  return Boolean(name) && !/^(facebook|instagram)-\d+$/i.test(name);
}

function targetState(
  platformCode: MetaPublishPlatformCode,
  accounts: Account[],
  assets: AccountAsset[],
): MetaPublishTargetState {
  const accountIDs = new Set(accounts.map((account) => account.id));
  const requiredType = requiredMetaAssetTypes[platformCode];
  const candidates = assets.filter((asset) => (
    accountIDs.has(asset.account_id)
    && asset.enabled
    && asset.platform_code === platformCode
    && asset.asset_type === requiredType
  ));
  const configured = candidates.filter((asset) => Boolean(asset.external_id?.trim()));
  const account = configured.length
    ? accounts.find((item) => item.id === configured[0].account_id)
    : accounts.find((item) => item.platformCode === platformCode)
      ?? accounts.find((item) => item.platformCode === 'meta');
  return {
    platformCode,
    account,
    assets: configured,
    missingReason: configured.length ? '' : candidates.length ? '缺少公开 ID' : '未配置发布目标',
  };
}

// Meta is one product surface but remains two independently owned publish
// targets. Only an explicit account group (or one "meta" login container)
// allows the UI to combine sessions; nicknames are never used to guess pairs.
export function buildMetaPublishGroups(accounts: Account[], assets: AccountAsset[]): MetaPublishGroup[] {
  const grouped = new Map<string, Account[]>();
  for (const account of accounts) {
    if (!account.enabled || !isMetaAccount(account)) continue;
    const group = account.accountGroup.trim();
    const key = group ? `group:${group}` : `account:${account.id}`;
    const members = grouped.get(key) ?? [];
    members.push(account);
    grouped.set(key, members);
  }
  return [...grouped.entries()].map(([key, members]) => {
    const facebook = members.find((account) => account.platformCode === 'facebook');
    const instagram = members.find((account) => account.platformCode === 'instagram');
    const meta = members.find((account) => account.platformCode === 'meta');
    const primary = (hasUsableAccountName(facebook) ? facebook : undefined)
      ?? (hasUsableAccountName(instagram) ? instagram : undefined)
      ?? meta
      ?? facebook
      ?? instagram
      ?? members[0];
    return {
      key,
      name: primary?.name || 'Meta',
      accounts: members,
      targets: {
        facebook: targetState('facebook', members, assets),
        instagram: targetState('instagram', members, assets),
      },
    };
  });
}

// Meta's browser_account is a login container, not a selectable publish target.
export function isPublishableAccountAsset(asset: AccountAsset): boolean {
  const required = requiredMetaAssetTypes[asset.platform_code];
  return asset.enabled && (!required || asset.asset_type === required);
}

// Normal platforms use their browser session asset. Meta requires an explicit,
// externally identified Page / Instagram target so the worker cannot fill the
// currently active but wrong Business Suite identity.
export function isControlledPrepareAccountAsset(asset: AccountAsset): boolean {
  const required = requiredMetaAssetTypes[asset.platform_code];
  if (required) {
    return asset.enabled
      && asset.asset_type === required
      && Boolean(asset.external_id?.trim());
  }
  return asset.enabled && asset.asset_type === 'browser_account';
}
