// SPDX-License-Identifier: Apache-2.0

import '../../../../__mocks__/i18n';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import * as accountApi from '@/api/account';
import { statusChanged, syncdone } from '@/api/accountsync';
import { AppStateContext } from '@/contexts/app-state-context';
import { useLightning } from '@/hooks/lightning';
import { AccountsSummary, Balances } from './accountssummary';

vi.mock('@/api/account');
vi.mock('@/api/accountsync', () => ({ statusChanged: vi.fn(), syncdone: vi.fn() }));
vi.mock('@/api/lightning', () => ({ subscribeLightningBalance: vi.fn(() => vi.fn()) }));
vi.mock('@/hooks/lightning', () => ({
  useLightning: vi.fn(),
}));
vi.mock('@/components/layout', () => {
  const Wrapper = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { GuideWrapper: Wrapper, GuidedContent: Wrapper, Header: Wrapper, Main: Wrapper };
});
vi.mock('@/components/guide/guide', () => ({ Guide: () => null }));
vi.mock('@/components/hideamountsbutton/hideamountsbutton', () => ({ HideAmountsButton: () => null }));
vi.mock('@/components/banners/backup', () => ({ BackupReminder: () => null }));
vi.mock('@/components/banners/offline-error', () => ({
  OfflineError: ({ error }: { error: string | null }) => <div>{error}</div>,
}));
vi.mock('./chart', () => ({ Chart: () => null }));
vi.mock('./total-balance-for-all-keystores', () => ({ TotalBalanceForAllKeystores: () => null }));
vi.mock('./keystorebalance', () => ({
  KeystoreBalance: ({ accounts, balances, keystore, title, keystoreBalance }: {
    accounts: accountApi.TAccount[];
    balances?: Balances;
    keystore?: accountApi.TKeystore;
    title?: string;
    keystoreBalance?: accountApi.TKeystoreBalance;
  }) => (
    <div data-testid={`section-${title ?? keystore?.name ?? ''}`} data-total={keystoreBalance?.total}>
      {accounts.map(account => (
        <output key={account.code} data-testid={`account-${account.code}`}>
          {balances?.[account.code]?.available.amount} BTC
        </output>
      ))}
    </div>
  ),
}));

const deferred = <T, >() => {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>(nextResolve => {
    resolve = nextResolve;
  });
  return { promise, resolve };
};

const account: accountApi.TAccount = {
  active: true,
  blockExplorerTxPrefix: '',
  code: 'account',
  coinCode: 'btc',
  coinName: 'Bitcoin',
  coinUnit: 'BTC',
  isToken: false,
  keystore: {
    connected: true,
    lastConnected: '',
    name: 'BitBox02',
    rootFingerprint: 'f23ab988',
    watchonly: false,
  },
  name: 'Bitcoin',
};

const status: accountApi.TStatus = {
  disabled: false,
  synced: true,
  fatalError: false,
  offlineError: null,
};

const balance = (amount: string): accountApi.TBalance => ({
  hasAvailable: true,
  available: { amount, unit: 'BTC', estimated: false },
  hasIncoming: false,
  incoming: { amount: '0', unit: 'BTC', estimated: false },
});

const summaryWithAccounts = (accounts: accountApi.TAccount[]) => (
  <AppStateContext.Provider value={{
    accounts,
    activeAccounts: accounts,
    deviceIDs: [],
    devices: {},
    hasAccounts: accounts.length > 0,
    hasBottomNavigation: false,
    hasDevices: false,
    hasLightningAccount: false,
    lightningAccount: null,
  }}>
    <AccountsSummary />
  </AppStateContext.Provider>
);

describe('AccountsSummary', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(useLightning).mockReturnValue({
      lightningAccount: null,
      lightningSDKStatus: 'inactive',
      isLightningReady: false,
    });
    vi.mocked(statusChanged).mockImplementation(() => vi.fn());
    vi.mocked(syncdone).mockImplementation(() => vi.fn());
    vi.mocked(accountApi.getChartData).mockResolvedValue({ success: false });
    vi.mocked(accountApi.getAccountsBalanceSummary).mockResolvedValue({ success: false });
    vi.mocked(accountApi.getStatus).mockResolvedValue(status);
    vi.mocked(accountApi.getBalance).mockResolvedValue({ success: true, balance: balance('2') });
    vi.mocked(accountApi.init).mockResolvedValue({ success: true });
  });

  it.each([true, false])('ignores a removed account status with synced=%s', async synced => {
    const pending = deferred<accountApi.TStatus>();
    vi.mocked(accountApi.getStatus).mockReturnValueOnce(pending.promise);
    const { rerender } = render(summaryWithAccounts([account]));

    rerender(summaryWithAccounts([{ ...account, code: 'next-account' }]));
    await screen.findByText('2 BTC');
    await act(async () => pending.resolve({ ...status, synced, offlineError: 'stale error' }));

    expect(screen.queryByText('stale error')).not.toBeInTheDocument();
    expect(accountApi.init).not.toHaveBeenCalled();
    expect(accountApi.getBalance).not.toHaveBeenCalledWith(account.code);
  });

  it.each(['initial', 'status', 'sync'])('ignores a pending balance from the %s load after accounts change', async source => {
    const pending = deferred<{ success: true; balance: accountApi.TBalance }>();
    if (source === 'initial') {
      vi.mocked(accountApi.getBalance).mockReturnValueOnce(pending.promise);
    }
    const { rerender } = render(summaryWithAccounts([account]));
    if (source !== 'initial') {
      await screen.findByText('2 BTC');
      vi.mocked(accountApi.getBalance).mockReturnValueOnce(pending.promise);
      act(() => {
        if (source === 'status') {
          vi.mocked(statusChanged).mock.calls[0]![1](status);
        } else {
          vi.mocked(syncdone).mock.calls[0]![1]();
        }
      });
    }
    await waitFor(() => expect(accountApi.getBalance).toHaveBeenCalledTimes(source === 'initial' ? 1 : 2));

    vi.mocked(accountApi.getBalance).mockResolvedValue({ success: true, balance: balance('3') });
    rerender(summaryWithAccounts([{ ...account, name: 'Renamed account' }]));
    await screen.findByText('3 BTC');
    await act(async () => pending.resolve({ success: true, balance: balance('1') }));

    expect(screen.getByText('3 BTC')).toBeInTheDocument();
    expect(screen.queryByText('1 BTC')).not.toBeInTheDocument();
  });

  it.each(['status', 'sync'])('keeps a pending %s balance when the Lightning lookup completes', async source => {
    vi.mocked(useLightning).mockReturnValue({
      lightningAccount: undefined,
      lightningSDKStatus: 'inactive',
      isLightningReady: false,
    });
    const accounts = [account];
    const { rerender } = render(summaryWithAccounts(accounts));
    await screen.findByText('2 BTC');

    const pending = deferred<{ success: true; balance: accountApi.TBalance }>();
    vi.mocked(accountApi.getBalance).mockReturnValueOnce(pending.promise);
    act(() => {
      if (source === 'status') {
        vi.mocked(statusChanged).mock.calls[0]![1](status);
      } else {
        vi.mocked(syncdone).mock.calls[0]![1]();
      }
    });
    await waitFor(() => expect(accountApi.getBalance).toHaveBeenCalledTimes(2));

    vi.mocked(useLightning).mockReturnValue({
      lightningAccount: null,
      lightningSDKStatus: 'inactive',
      isLightningReady: false,
    });
    rerender(summaryWithAccounts(accounts));
    await act(async () => pending.resolve({ success: true, balance: balance('3') }));

    expect(screen.getByText('3 BTC')).toBeInTheDocument();
    expect(accountApi.getBalance).toHaveBeenCalledTimes(2);
  });

  it('ignores callbacks from obsolete subscriptions and keeps current subscriptions working', async () => {
    const { rerender } = render(summaryWithAccounts([account]));
    await screen.findByText('2 BTC');
    const onStatus = vi.mocked(statusChanged).mock.calls[0]![1];
    const onSync = vi.mocked(syncdone).mock.calls[0]![1];

    vi.mocked(accountApi.getBalance).mockResolvedValue({ success: true, balance: balance('3') });
    rerender(summaryWithAccounts([{ ...account, code: 'next-account' }]));
    await screen.findByText('3 BTC');
    vi.mocked(accountApi.getStatus).mockClear();

    act(() => {
      onStatus(status);
      onSync();
    });
    expect(accountApi.getStatus).not.toHaveBeenCalled();
    expect(vi.mocked(statusChanged).mock.results[0]!.value).toHaveBeenCalledOnce();
    expect(vi.mocked(syncdone).mock.results[0]!.value).toHaveBeenCalledOnce();

    vi.mocked(accountApi.getBalance).mockResolvedValue({ success: true, balance: balance('4') });
    act(() => vi.mocked(syncdone).mock.calls[1]![1]());
    expect(await screen.findByText('4 BTC')).toBeInTheDocument();
    expect(accountApi.getStatus).toHaveBeenCalledWith('next-account');
  });

  // Vault accounts are listed in their own section with their own total, separate from the
  // accounts of the keystore they are presented under.
  it('lists vault accounts in their own section', async () => {
    const vaultAccount: accountApi.TAccount = { ...account, code: 'vault', name: 'My vault', isVault: true };
    vi.mocked(accountApi.getAccountsBalanceSummary).mockResolvedValue({
      success: true,
      accountsBalanceSummary: {
        keystoresBalance: { [account.keystore.rootFingerprint]: { fiatUnit: 'USD', total: '10.00' } },
        vaultsBalance: { fiatUnit: 'USD', total: '20.00' },
        coinsTotalBalance: [],
      },
    });
    render(summaryWithAccounts([account, vaultAccount]));

    const keystoreSection = await screen.findByTestId('section-BitBox02');
    const vaultsSection = screen.getByTestId('section-vault.sectionTitle');
    expect(within(keystoreSection).getByTestId('account-account')).toBeInTheDocument();
    expect(within(keystoreSection).queryByTestId('account-vault')).toBeNull();
    expect(within(vaultsSection).getByTestId('account-vault')).toBeInTheDocument();
    expect(within(vaultsSection).queryByTestId('account-account')).toBeNull();
    await waitFor(() => expect(vaultsSection).toHaveAttribute('data-total', '20.00'));
    expect(keystoreSection).toHaveAttribute('data-total', '10.00');
  });

  // Without vault accounts there is no vaults section.
  it('shows no vaults section without vault accounts', async () => {
    render(summaryWithAccounts([account]));
    await screen.findByTestId('section-BitBox02');
    expect(screen.queryByTestId('section-vault.sectionTitle')).toBeNull();
  });
});
