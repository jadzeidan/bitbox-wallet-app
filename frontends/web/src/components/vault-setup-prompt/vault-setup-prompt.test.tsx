// SPDX-License-Identifier: Apache-2.0

import '../../../__mocks__/i18n';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from '@/utils/test-helpers';
import * as vaultApi from '@/api/vault';
import { VaultSetupPrompt } from './vault-setup-prompt';

vi.mock('@/api/vault');

const setup: vaultApi.TVaultSetup = {
  name: 'My vault',
  step: 'collectKeys',
  threshold: 2,
  numKeys: 3,
  keys: [],
  canAddKey: true,
  canRegister: false,
};

const renderPrompt = (route: string, current: vaultApi.TVaultSetup | null) => {
  vi.mocked(vaultApi.getVaultSetup).mockResolvedValue(current);
  vi.mocked(vaultApi.subscribeVaultSetup).mockReturnValue(() => {});
  return render(
    <MemoryRouter initialEntries={[route]}>
      <VaultSetupPrompt />
    </MemoryRouter>
  );
};

describe('vault setup prompt', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // After swapping devices the user is elsewhere in the app. A connected device that can take the
  // next step offers to continue, which leads back to the wizard.
  it('offers to continue when the connected device can take the next step', async () => {
    renderPrompt('/account-summary', setup);
    expect(await screen.findByText('vault.prompt.addKey')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'vault.prompt.continue' }));
    await waitFor(() => expect(window.location.hash).toBe('#/add-account/vault/create'));
  });

  // The prompt does not interrupt when there is nothing to do with the connected device, or when
  // the user is already in the wizard.
  it('stays hidden without a next step or inside the wizard', async () => {
    const { unmount } = renderPrompt('/account-summary', { ...setup, canAddKey: false });
    await waitFor(() => expect(vaultApi.getVaultSetup).toHaveBeenCalled());
    expect(screen.queryByText('vault.prompt.addKey')).toBeNull();
    unmount();

    renderPrompt('/add-account/vault/create', { ...setup, canAddKey: false, canRegister: true });
    await waitFor(() => expect(vaultApi.getVaultSetup).toHaveBeenCalledTimes(2));
    expect(screen.queryByText('vault.prompt.register')).toBeNull();
  });

  // Declining the prompt leaves the user where they are.
  it('can be dismissed', async () => {
    renderPrompt('/account-summary', { ...setup, canAddKey: false, canRegister: true });
    fireEvent.click(await screen.findByRole('button', { name: 'vault.prompt.later' }));
    await waitFor(() => expect(screen.queryByText('vault.prompt.register')).toBeNull());
    expect(window.location.hash).toBe('#/account-summary');
  });
});
