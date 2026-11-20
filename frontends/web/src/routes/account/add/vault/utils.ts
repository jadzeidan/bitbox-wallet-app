// SPDX-License-Identifier: Apache-2.0

import type { TFunction } from 'i18next';
import type { AccountCode } from '@/api/account';
import { registerVault, TVaultFailure } from '@/api/vault';

// Returns the message to show for a failed vault action.
export const getVaultErrorMessage = (t: TFunction, failure: TVaultFailure): string => {
  switch (failure.errorCode) {
  case 'accountAlreadyExists':
    return t('vault.error.accountAlreadyExists');
  case 'accountLimitReached':
    return t('vault.error.accountLimitReached');
  case 'firmwareUpgradeRequired':
    return t('vault.error.firmwareUpgradeRequired');
  case 'multisigAlreadySigned':
    return t('vault.error.multisigAlreadySigned');
  case 'multisigNotRegistered':
    return t('vault.error.multisigNotRegistered');
  case 'multisigTxPending':
    return t('vault.error.multisigTxPending');
  case 'unsupportedFeature':
    return t('vault.error.unsupportedFeature');
  case 'userAbort':
    return t('vault.error.userAbort');
  case 'vaultDescriptorInvalid':
    return t('vault.error.vaultDescriptorInvalid');
  case 'vaultKeyMismatch':
    return t('vault.error.vaultKeyMismatch');
  case 'vaultKeystoreAlreadyAdded':
    return t('vault.error.vaultKeystoreAlreadyAdded');
  case 'vaultKeystoreNotCosigner':
    return t('vault.error.vaultKeystoreNotCosigner');
  case 'vaultNameInvalid':
    return t('vault.error.vaultNameInvalid');
  case 'vaultSetupInProgress':
    return t('vault.error.vaultSetupInProgress');
  case 'vaultUnsupported':
    return t('vault.error.vaultUnsupported');
  default:
    return t('unknownError', { errorMessage: failure.errorMessage || failure.errorCode || '' });
  }
};

// Runs an action that needs the vault to be registered on the connected BitBox02. If the device
// does not know the vault yet, the vault is registered first, which the user confirms on the
// device, and the action is run again.
export const withVaultRegistration = async <T extends { success: true }>(
  accountCode: AccountCode,
  action: () => Promise<T | TVaultFailure>,
): Promise<T | TVaultFailure> => {
  const result = await action();
  if (result.success || result.errorCode !== 'multisigNotRegistered') {
    return result;
  }
  const registered = await registerVault(accountCode);
  if (!registered.success) {
    return registered;
  }
  return action();
};
