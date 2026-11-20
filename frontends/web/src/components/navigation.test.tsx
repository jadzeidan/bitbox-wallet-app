// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { useLocation } from 'wouter';
import { MemoryRouter } from '@/utils/test-helpers';
import { AppStateContext } from '@/contexts/app-state-context';
import { AppNavigation } from './navigation';

const Location = () => {
  const [location] = useLocation();
  return <div data-testid="location">{location}</div>;
};

// Renders the navigation rules at the given route with no account loaded and no device connected,
// as is the case while BitBox02s are swapped.
const renderWithoutAccounts = (route: string) => render(
  <MemoryRouter initialEntries={[route]}>
    <AppStateContext.Provider
      value={{
        accounts: [],
        activeAccounts: [],
        deviceIDs: [],
        devices: {},
        hasAccounts: false,
        hasBottomNavigation: false,
        hasDevices: false,
        lightningAccount: undefined,
        hasLightningAccount: false,
      }}
    >
      <AppNavigation />
      <Location />
    </AppStateContext.Provider>
  </MemoryRouter>
);

describe('components/navigation', () => {
  // Creating a vault requires disconnecting one BitBox02 to connect the next. The wizard stays
  // open in between, although no account is loaded then.
  it('stays in the vault wizard when no account is loaded', async () => {
    renderWithoutAccounts('/add-account/vault/create');
    await act(async () => {});
    expect(screen.getByTestId('location')).toHaveTextContent('/add-account/vault/create');
  });

  // The other screens for adding an account need a connected device and are left when no
  // account is loaded.
  it.each([
    '/add-account',
    '/add-account/vault',
    '/add-account/vault/import',
  ])('leaves %s when no account is loaded', async (route) => {
    renderWithoutAccounts(route);
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/'));
  });
});
