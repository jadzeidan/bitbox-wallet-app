// SPDX-License-Identifier: Apache-2.0

import type { TFunction } from 'i18next';
import type { TVaultFailure } from '@/api/vault';

// Returns the message to show for a failed vault action.
export const getVaultErrorMessage = (t: TFunction, failure: TVaultFailure): string => {
  switch (failure.errorCode) {
  case 'accountAlreadyExists':
    return t('vault.error.accountAlreadyExists');
  case 'accountLimitReached':
    return t('vault.error.accountLimitReached');
  case 'firmwareUpgradeRequired':
    return t('vault.error.firmwareUpgradeRequired');
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
