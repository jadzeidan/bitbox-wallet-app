// SPDX-License-Identifier: Apache-2.0

import '../../../../../__mocks__/i18n';
import { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from '@/utils/test-helpers';
import * as keystoresApi from '@/api/keystores';
import * as vaultApi from '@/api/vault';
import { VaultCreate } from './vault-create';

vi.mock('@/api/keystores');
vi.mock('@/api/vault');
vi.mock('@/components/confirm/Confirm', () => ({ confirmation: vi.fn() }));
vi.mock('@/components/backbutton/backbutton', () => ({
  DesktopBackButton: ({ children }: { children: ReactNode }) => <button>{children}</button>,
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

const key = (
  index: number,
  overrides: Partial<vaultApi.TVaultSetupKey> = {},
): vaultApi.TVaultSetupKey => ({
  rootFingerprint: `0000000${index}`,
  keypath: 'm/48\'/0\'/0\'/2\'',
  xpub: `Zpub${index}`,
  keystoreName: `BitBox ${index}`,
  connected: false,
  registered: false,
  ...overrides,
});

const makeSetup = (overrides: Partial<vaultApi.TVaultSetup>): vaultApi.TVaultSetup => ({
  name: 'My vault',
  step: 'collectKeys',
  threshold: 2,
  numKeys: 3,
  keys: [key(1)],
  canAddKey: false,
  canRegister: false,
  ...overrides,
});

// Renders the wizard with the given setup and returns a function that pushes a new setup, like
// the backend does when the setup or the connected device changes. A BitBox02 is connected unless
// keystores is empty.
const renderWizard = async (
  setup: vaultApi.TVaultSetup | null,
  keystores: keystoresApi.TKeystores = [{ type: 'hardware' }],
) => {
  let pushSetup: (setup: vaultApi.TVaultSetup | null) => void = () => {};
  vi.mocked(keystoresApi.getKeystores).mockResolvedValue(keystores);
  vi.mocked(keystoresApi.subscribeKeystores).mockReturnValue(() => {});
  vi.mocked(vaultApi.getVaultSetup).mockResolvedValue(setup);
  vi.mocked(vaultApi.subscribeVaultSetup).mockImplementation(cb => {
    pushSetup = cb;
    return () => {};
  });
  render(
    <MemoryRouter initialEntries={['/add-account/vault/create']}>
      <VaultCreate />
    </MemoryRouter>
  );
  await waitFor(() => expect(screen.queryByRole('heading', { name: 'loading' })).toBeNull());
  return (next: vaultApi.TVaultSetup | null) => act(() => pushSetup(next));
};

describe('vault create wizard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // Without a setup in progress the wizard asks for the vault name and starts the setup with it.
  // A rejected name is reported to the user.
  it('starts a setup with the entered name', async () => {
    vi.mocked(vaultApi.startVaultSetup).mockResolvedValue({
      success: false,
      errorCode: 'vaultNameInvalid',
    });
    await renderWizard(null);

    const startButton = screen.getByRole('button', { name: 'vault.create.name.button' });
    expect(startButton).toBeDisabled();
    fireEvent.input(screen.getByLabelText('vault.create.name.label'), { target: { value: 'My vault' } });
    await waitFor(() => expect(startButton).toBeEnabled());
    expect(screen.queryByText('vault.create.name.connect')).toBeNull();
    fireEvent.click(startButton);

    await waitFor(() => expect(vaultApi.startVaultSetup).toHaveBeenCalledWith('My vault'));
    expect(await screen.findByText('vault.error.vaultNameInvalid')).toBeInTheDocument();
  });

  // The first key comes from the connected BitBox02, so a setup can't be started without one. The
  // user is told to connect a device.
  it('asks to connect a device before starting', async () => {
    await renderWizard(null, []);

    fireEvent.input(screen.getByLabelText('vault.create.name.label'), { target: { value: 'My vault' } });
    expect(await screen.findByText('vault.create.name.connect')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'vault.create.name.button' })).toBeDisabled();
  });

  // Saving the draft leaves the wizard without discarding the setup, which the backend keeps. It
  // is offered in every step in which devices are swapped, also with no device connected.
  it('leaves the wizard and keeps the setup when saving the draft', async () => {
    const pushSetup = await renderWizard(makeSetup({ keys: [key(1)] }), []);
    expect(screen.getByText('vault.create.collectKeys.connectAny')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'vault.create.saveDraft' })).toBeInTheDocument();

    await pushSetup(makeSetup({ step: 'register', keys: [key(1), key(2), key(3)] }));
    fireEvent.click(screen.getByRole('button', { name: 'vault.create.saveDraft' }));

    await waitFor(() => expect(window.location.hash).toBe('#/'));
    expect(vaultApi.discardVaultSetup).not.toHaveBeenCalled();
  });

  // While keys are collected, a device that has not contributed a key can be added. A device that
  // already did is asked to be swapped for the next one.
  it('adds the key of a new device and asks to swap a device that was already added', async () => {
    vi.mocked(vaultApi.addVaultSetupKey).mockResolvedValue({ success: true });
    const pushSetup = await renderWizard(makeSetup({
      keys: [key(1, { connected: true })],
    }));
    expect(screen.getByText('vault.create.collectKeys.connectNext')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'vault.create.collectKeys.addKey' })).toBeNull();

    // The next device was connected.
    await pushSetup(makeSetup({ canAddKey: true }));
    fireEvent.click(screen.getByRole('button', { name: 'vault.create.collectKeys.addKey' }));
    await waitFor(() => expect(vaultApi.addVaultSetupKey).toHaveBeenCalledTimes(1));

    // No device is connected.
    await pushSetup(makeSetup({ keys: [key(1), key(2)] }));
    expect(screen.getByText('vault.create.collectKeys.connectAny')).toBeInTheDocument();
  });

  // In the register step the keys are listed for comparison with the device. The vault is
  // registered on the connected device, and the user is told what to do on the device meanwhile.
  it('registers the vault on the connected device', async () => {
    let finishRegistration: (response: vaultApi.TVaultResponse) => void = () => {};
    vi.mocked(vaultApi.registerVaultSetup).mockReturnValue(new Promise(resolve => {
      finishRegistration = resolve;
    }));
    const keys = [key(1), key(2), key(3, { connected: true })];
    const pushSetup = await renderWizard(makeSetup({ step: 'register', keys, canRegister: true }));

    expect(screen.getByTestId('vault-key-xpub-0')).toHaveTextContent('Zpub1');
    expect(screen.getAllByText('vault.keys.notRegistered')).toHaveLength(3);
    const registerButton = screen.getByRole('button', { name: 'vault.create.register.button' });
    fireEvent.click(registerButton);
    expect(await screen.findByText('vault.create.register.confirming')).toBeInTheDocument();
    expect(registerButton).toBeDisabled();

    await act(async () => finishRegistration({ success: false, errorCode: 'userAbort' }));
    expect(await screen.findByText('vault.error.userAbort')).toBeInTheDocument();
    expect(screen.queryByText('vault.create.register.confirming')).toBeNull();

    // Once registered, the user is asked to connect a device that still has to register.
    await pushSetup(makeSetup({
      step: 'register',
      keys: [key(1), key(2), key(3, { connected: true, registered: true })],
    }));
    expect(screen.queryByRole('button', { name: 'vault.create.register.button' })).toBeNull();
    expect(screen.getByText('vault.create.register.connect')).toBeInTheDocument();
    expect(screen.getAllByText('vault.keys.registered')).toHaveLength(1);
  });

  // The setup can only be finished after the user confirmed having stored the vault file.
  // Finishing leads to the new account.
  it('requires acknowledging the backup before finishing', async () => {
    vi.mocked(vaultApi.finishVaultSetup).mockResolvedValue({ success: true, accountCode: 'vault-code' });
    vi.mocked(vaultApi.exportVault).mockResolvedValue({ success: false, errorCode: 'userAbort' });
    await renderWizard(makeSetup({
      step: 'backup',
      keys: [key(1, { registered: true }), key(2, { registered: true }), key(3, { registered: true })],
      accountCode: 'vault-code',
      descriptor: 'wsh(sortedmulti(2,...))#checksum',
    }));

    expect(screen.getByDisplayValue('wsh(sortedmulti(2,...))#checksum')).toBeInTheDocument();
    const finishButton = screen.getByRole('button', { name: 'vault.create.backup.button' });
    expect(finishButton).toBeDisabled();

    // Declining to pick a file when saving is not reported as an error.
    fireEvent.click(screen.getByRole('button', { name: 'vault.create.backup.save' }));
    await waitFor(() => expect(vaultApi.exportVault).toHaveBeenCalledWith('vault-code'));
    expect(screen.queryByText('vault.error.userAbort')).toBeNull();

    fireEvent.click(screen.getByLabelText('vault.create.backup.acknowledge'));
    expect(finishButton).toBeEnabled();
    fireEvent.click(finishButton);

    fireEvent.click(await screen.findByRole('button', { name: 'vault.success.button' }));
    await waitFor(() => expect(window.location.hash).toBe('#/account/vault-code'));
  });
});
