// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'wouter';
import { getVaultSetup, subscribeVaultSetup } from '@/api/vault';
import { useSync } from '@/hooks/api';
import { Button } from '@/components/forms';
import { Dialog, DialogButtons } from '@/components/dialog/dialog';

const VAULT_ROUTE = '/add-account/vault';

// Offers to continue creating a vault when a BitBox02 that can take the next step is connected.
// Creating a vault requires swapping devices, which leaves the setup screen.
export const VaultSetupPrompt = () => {
  const { t } = useTranslation();
  const [location, navigate] = useLocation();
  const setup = useSync(getVaultSetup, subscribeVaultSetup);
  const [dismissed, setDismissed] = useState(false);

  const actionable = !!setup && (setup.canAddKey || setup.canRegister);

  // Prompt again for the next device that can take a step.
  useEffect(() => {
    if (!actionable) {
      setDismissed(false);
    }
  }, [actionable]);

  if (!setup || !actionable || dismissed || location.startsWith(VAULT_ROUTE)) {
    return null;
  }

  return (
    <Dialog title={t('vault.prompt.title')} medium open onClose={() => setDismissed(true)}>
      <p>
        {setup.canAddKey
          ? t('vault.prompt.addKey', { name: setup.name, number: setup.keys.length + 1 })
          : t('vault.prompt.register', { name: setup.name })}
      </p>
      <DialogButtons>
        <Button
          primary
          onClick={() => {
            setDismissed(true);
            navigate(`${VAULT_ROUTE}/create`);
          }}>
          {t('vault.prompt.continue')}
        </Button>
        <Button secondary onClick={() => setDismissed(true)}>
          {t('vault.prompt.later')}
        </Button>
      </DialogButtons>
    </Dialog>
  );
};
