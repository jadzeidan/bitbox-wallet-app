// SPDX-License-Identifier: Apache-2.0

import '../../../../__mocks__/i18n';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { TAmountWithConversions } from '@/api/account';
import * as vaultApi from '@/api/vault';
import { alertUser } from '@/components/alert/Alert';
import { confirmation } from '@/components/confirm/Confirm';
import { VaultPendingTx } from './vault-pending-tx';

vi.mock('@/api/vault');
vi.mock('@/components/alert/Alert', () => ({ alertUser: vi.fn() }));
vi.mock('@/components/confirm/Confirm', () => ({ confirmation: vi.fn() }));
vi.mock('@/components/amount/amount-with-unit', () => ({
  AmountWithUnit: ({ amount }: { amount: TAmountWithConversions }) => (
    <span>{amount.amount} {amount.unit}</span>
  ),
}));

const amount = (value: string): TAmountWithConversions => ({
  amount: value,
  unit: 'TBTC',
  estimated: false,
});

const makePendingTx = (overrides: Partial<vaultApi.TVaultPendingTx> = {}): vaultApi.TVaultPendingTx => ({
  recipients: [{ address: 'tb1qrecipient', amount: amount('0.001') }],
  amount: amount('0.001'),
  fee: amount('0.00000500'),
  total: amount('0.00100500'),
  note: 'rent',
  threshold: 2,
  numSignatures: 1,
  canSign: true,
  canBroadcast: false,
  keys: [
    { rootFingerprint: '00000001', keystoreName: 'BitBox A', connected: false, signed: true },
    { rootFingerprint: '00000002', keystoreName: 'BitBox B', connected: true, signed: false },
    { rootFingerprint: '00000003', keystoreName: 'BitBox C', connected: false, signed: false },
  ],
  ...overrides,
});

describe('vault pending transaction', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // The pending transaction shows what is being sent and who has signed. The connected BitBox02
  // that has not signed yet adds the second signature, which sends the transaction.
  it('signs with the connected device and reports that the transaction was sent', async () => {
    vi.mocked(vaultApi.signVaultTx).mockResolvedValue({ success: true, txId: 'txid' });
    const onChange = vi.fn();
    render(<VaultPendingTx accountCode="vault" pendingTx={makePendingTx()} onChange={onChange} />);

    expect(screen.getByText('tb1qrecipient')).toBeInTheDocument();
    expect(screen.getByText('0.00100500 TBTC')).toBeInTheDocument();
    expect(screen.getByText('rent')).toBeInTheDocument();
    expect(screen.getAllByText('vault.tx.signed')).toHaveLength(1);
    expect(screen.queryByRole('button', { name: 'vault.tx.broadcast' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'vault.tx.sign' }));
    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    expect(vaultApi.signVaultTx).toHaveBeenCalledWith('vault');
    expect(alertUser).toHaveBeenCalledWith('vault.tx.sent');
    expect(vaultApi.registerVault).not.toHaveBeenCalled();
  });

  // A BitBox02 that does not know the vault yet registers it first, then signs.
  it('registers the vault on the device before signing if needed', async () => {
    vi.mocked(vaultApi.signVaultTx)
      .mockResolvedValueOnce({ success: false, errorCode: 'multisigNotRegistered' })
      .mockResolvedValueOnce({ success: true });
    vi.mocked(vaultApi.registerVault).mockResolvedValue({ success: true });
    const onChange = vi.fn();
    render(<VaultPendingTx accountCode="vault" pendingTx={makePendingTx()} onChange={onChange} />);

    fireEvent.click(screen.getByRole('button', { name: 'vault.tx.sign' }));
    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    expect(vaultApi.registerVault).toHaveBeenCalledWith('vault');
    expect(vaultApi.signVaultTx).toHaveBeenCalledTimes(2);
    // Another signature is still needed, so nothing was sent.
    expect(alertUser).not.toHaveBeenCalled();
  });

  // The device that already signed can't sign again: the user is asked to connect another one.
  // Rejecting on the device is not reported as an error.
  it('asks for another device when the connected one can not sign', async () => {
    render(
      <VaultPendingTx
        accountCode="vault"
        pendingTx={makePendingTx({
          canSign: false,
          keys: [
            { rootFingerprint: '00000001', keystoreName: 'BitBox A', connected: true, signed: true },
            { rootFingerprint: '00000002', keystoreName: 'BitBox B', connected: false, signed: false },
            { rootFingerprint: '00000003', keystoreName: '', connected: false, signed: false },
          ],
        })}
        onChange={vi.fn()} />
    );
    expect(screen.queryByRole('button', { name: 'vault.tx.sign' })).toBeNull();
    expect(screen.getByText('vault.tx.connectOther')).toBeInTheDocument();
  });

  // A fully signed transaction that could not be sent only needs to be sent again.
  it('sends a fully signed transaction', async () => {
    vi.mocked(vaultApi.signVaultTx).mockResolvedValue({ success: false, errorCode: 'userAbort' });
    const onChange = vi.fn();
    render(
      <VaultPendingTx
        accountCode="vault"
        pendingTx={makePendingTx({ canSign: false, canBroadcast: true, numSignatures: 2 })}
        onChange={onChange} />
    );
    expect(screen.queryByRole('button', { name: 'vault.tx.sign' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'vault.tx.broadcast' }));
    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    expect(alertUser).not.toHaveBeenCalled();
  });

  // Discarding asks for confirmation first.
  it('discards the transaction after confirmation', async () => {
    vi.mocked(vaultApi.discardVaultTx).mockResolvedValue({ success: true });
    const onChange = vi.fn();
    render(<VaultPendingTx accountCode="vault" pendingTx={makePendingTx()} onChange={onChange} />);

    fireEvent.click(screen.getByRole('button', { name: 'vault.tx.discard' }));
    expect(vaultApi.discardVaultTx).not.toHaveBeenCalled();
    const confirm = vi.mocked(confirmation).mock.calls[0]![1];
    confirm(false);
    expect(vaultApi.discardVaultTx).not.toHaveBeenCalled();
    confirm(true);
    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    expect(vaultApi.discardVaultTx).toHaveBeenCalledWith('vault');
  });
});
