// SPDX-License-Identifier: Apache-2.0

import { useCallback, useState } from 'react';
import type { AccountCode } from '@/api/account';
import { getVaultPendingTx, TVaultPendingTx } from '@/api/vault';
import { useLoad } from '@/hooks/api';
import { useKeystores } from '@/hooks/backend';

type TUseVaultPendingTx = {
  // undefined while loading, null if the vault has no pending transaction.
  pendingTx: TVaultPendingTx | null | undefined;
  reload: () => void;
};

// Loads the pending transaction of a vault account. It is loaded again when another BitBox02 is
// connected, as this changes who can sign, and on reload().
export const useVaultPendingTx = (accountCode: AccountCode, isVault: boolean): TUseVaultPendingTx => {
  const keystores = useKeystores();
  const [revision, setRevision] = useState(0);
  const pendingTx = useLoad(
    isVault ? () => getVaultPendingTx(accountCode) : null,
    [accountCode, isVault, keystores, revision],
  );
  const reload = useCallback(() => setRevision(current => current + 1), []);
  return { pendingTx: isVault ? pendingTx : null, reload };
};
