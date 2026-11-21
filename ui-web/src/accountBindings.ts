type AccountBinding = {
  id: number;
  identityId: number;
  platformCode: string;
  accountIdentifier: string;
  loginState: string;
  enabled: boolean;
};

export function duplicateAccountBindingIDs(accounts: AccountBinding[]): Set<number> {
  const canonicalByBinding = new Map<string, AccountBinding>();
  const duplicates = new Set<number>();
  const candidates = accounts
    .filter((account) => account.enabled && account.loginState === 'logged_in' && account.accountIdentifier.trim())
    .sort((left, right) => left.identityId - right.identityId || left.id - right.id);

  for (const account of candidates) {
    const key = `${account.platformCode.trim().toLowerCase()}\n${account.accountIdentifier.trim().toLowerCase()}`;
    const canonical = canonicalByBinding.get(key);
    if (canonical && canonical.identityId !== account.identityId) duplicates.add(account.id);
    else canonicalByBinding.set(key, account);
  }
  return duplicates;
}
