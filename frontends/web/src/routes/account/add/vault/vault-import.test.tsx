// SPDX-License-Identifier: Apache-2.0

import '../../../../../__mocks__/i18n';
import { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from '@/utils/test-helpers';
import * as vaultApi from '@/api/vault';
import { VaultImport } from './vault-import';

vi.mock('@/api/vault');
vi.mock('@/components/backbutton/backbutton', () => ({
  DesktopBackButton: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button onClick={onClick}>{children}</button>
  ),
}));
vi.mock('./vault-view', () => ({
  VaultView: ({ title, children, buttons }: { title: string; children: ReactNode; buttons?: ReactNode }) => (
    <div>
      <h1>{title}</h1>
      {children}
      {buttons}
    </div>
  ),
}));

const vault: vaultApi.TVaultInfo = {
  accountCode: 'vault-code',
  threshold: 2,
  registered: false,
  keys: [1, 2, 3].map(index => ({
    rootFingerprint: `0000000${index}`,
    keypath: 'm/48\'/0\'/0\'/2\'',
    xpub: `Zpub${index}`,
    keystoreName: index === 1 ? 'BitBox 1' : '',
    connected: index === 1,
  })),
};

const renderImport = () => render(
  <MemoryRouter initialEntries={['/add-account/vault/import']}>
    <VaultImport />
  </MemoryRouter>
);

describe('vault import', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // An invalid vault file is reported and the user stays on the input step.
  it('reports an invalid vault file', async () => {
    vi.mocked(vaultApi.getVaultInfo).mockResolvedValue({
      success: false,
      errorCode: 'vaultDescriptorInvalid',
    });
    renderImport();

    const continueButton = screen.getByRole('button', { name: 'vault.import.continue' });
    expect(continueButton).toBeDisabled();
    fireEvent.change(screen.getByLabelText('vault.import.descriptor.label'), {
      target: { value: '  not a vault  ' },
    });
    fireEvent.click(continueButton);

    await waitFor(() => expect(vaultApi.getVaultInfo).toHaveBeenCalledWith('not a vault'));
    expect(await screen.findByText('vault.error.vaultDescriptorInvalid')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'vault.import.title' })).toBeInTheDocument();
  });

  // A valid vault file is shown for review with its keys. After naming it, the vault is added
  // and the user is led to the new account.
  it('adds a reviewed vault', async () => {
    vi.mocked(vaultApi.getVaultInfo).mockResolvedValue({ success: true, vault });
    vi.mocked(vaultApi.addVault).mockResolvedValue({ success: true, accountCode: 'vault-code' });
    renderImport();

    fireEvent.change(screen.getByLabelText('vault.import.descriptor.label'), {
      target: { value: 'wsh(sortedmulti(2,...))#checksum' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'vault.import.continue' }));

    expect(await screen.findByRole('heading', { name: 'vault.import.review.title' })).toBeInTheDocument();
    expect(screen.getByTestId('vault-key-xpub-2')).toHaveTextContent('Zpub3');
    expect(screen.getByText('vault.keys.connected')).toBeInTheDocument();
    expect(screen.getByText('vault.import.review.registerNote')).toBeInTheDocument();

    const addButton = screen.getByRole('button', { name: 'vault.import.review.button' });
    expect(addButton).toBeDisabled();
    fireEvent.input(screen.getByLabelText('vault.create.name.label'), { target: { value: 'My vault' } });
    fireEvent.click(addButton);

    await waitFor(() => expect(vaultApi.addVault).toHaveBeenCalledWith(
      'wsh(sortedmulti(2,...))#checksum',
      'My vault',
    ));
    fireEvent.click(await screen.findByRole('button', { name: 'vault.success.button' }));
    await waitFor(() => expect(window.location.hash).toBe('#/account/vault-code'));
  });
});
