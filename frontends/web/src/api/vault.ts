// SPDX-License-Identifier: Apache-2.0

import type { AccountCode, TAmountWithConversions } from './account';
import { subscribeEndpoint, TSubscriptionCallback, TUnsubscribe } from './subscribe';
import { apiGet, apiPost } from '@/utils/request';

export type TVaultKey = {
  rootFingerprint: string;
  keypath: string;
  // Encoded like the BitBox02 displays it when registering the vault.
  xpub: string;
  // Empty if the keystore has never been connected.
  keystoreName: string;
  connected: boolean;
};

export type TVaultInfo = {
  accountCode: AccountCode;
  threshold: number;
  keys: TVaultKey[];
  // True if the vault is registered on the connected keystore.
  registered: boolean;
};

export type TVaultErrorCode =
  | 'accountAlreadyExists'
  | 'accountLimitReached'
  | 'firmwareUpgradeRequired'
  | 'multisigAlreadySigned'
  | 'multisigNoPendingTx'
  | 'multisigNotRegistered'
  | 'multisigSpendUnsupported'
  | 'multisigTxPending'
  | 'unsupportedFeature'
  | 'userAbort'
  | 'vaultDescriptorInvalid'
  | 'vaultKeyMismatch'
  | 'vaultKeystoreAlreadyAdded'
  | 'vaultKeystoreNotCosigner'
  | 'vaultNameInvalid'
  | 'vaultSetupInProgress'
  | 'vaultSetupNotFound'
  | 'vaultSetupWrongStep'
  | 'vaultUnsupported'
  | 'wrongKeystore';

export type TVaultFailure = {
  success: false;
  errorCode?: TVaultErrorCode;
  errorMessage?: string;
};

export type TVaultInfoResponse = {
  success: true;
  vault: TVaultInfo;
} | TVaultFailure;

export type TVaultResponse = {
  success: true;
} | TVaultFailure;

export type TVaultAccountResponse = {
  success: true;
  accountCode: AccountCode;
} | TVaultFailure;

export const getVaultSupported = (): Promise<boolean> => {
  return apiGet('vault/supported');
};

export const getVaultInfo = (descriptor: string): Promise<TVaultInfoResponse> => {
  return apiPost('vault/info', { descriptor });
};

export const addVault = (descriptor: string, name: string): Promise<TVaultAccountResponse> => {
  return apiPost('vault/add', { descriptor, name });
};

export const registerVault = (accountCode: AccountCode): Promise<TVaultResponse> => {
  return apiPost('vault/register', { accountCode });
};

export const exportVault = (accountCode: AccountCode): Promise<TVaultResponse> => {
  return apiPost('vault/export', { accountCode });
};

export type TVaultSetupKey = TVaultKey & {
  // True if the vault is registered on the keystore holding the key.
  registered: boolean;
};

export type TVaultSetupStep = 'collectKeys' | 'register' | 'backup';

export type TVaultSetup = {
  name: string;
  step: TVaultSetupStep;
  threshold: number;
  numKeys: number;
  keys: TVaultSetupKey[];
  // True if the connected keystore can contribute the next key.
  canAddKey: boolean;
  // True if the vault can be registered on the connected keystore now.
  canRegister: boolean;
  // Set in the backup step.
  accountCode?: AccountCode;
  descriptor?: string;
};

// Resolves to null if no vault is being created.
export const getVaultSetup = (): Promise<TVaultSetup | null> => {
  return apiGet('vault/setup');
};

export const subscribeVaultSetup = (
  cb: TSubscriptionCallback<TVaultSetup | null>,
): TUnsubscribe => {
  return subscribeEndpoint('vault/setup', cb);
};

export const startVaultSetup = (name: string): Promise<TVaultResponse> => {
  return apiPost('vault/setup/start', { name });
};

export const addVaultSetupKey = (): Promise<TVaultResponse> => {
  return apiPost('vault/setup/add-key');
};

export const registerVaultSetup = (): Promise<TVaultResponse> => {
  return apiPost('vault/setup/register');
};

export const finishVaultSetup = (): Promise<TVaultAccountResponse> => {
  return apiPost('vault/setup/finish');
};

export const discardVaultSetup = (): Promise<TVaultResponse> => {
  return apiPost('vault/setup/discard');
};

export type TVaultTxKey = {
  rootFingerprint: string;
  // Empty if the keystore has never been connected.
  keystoreName: string;
  connected: boolean;
  // True if the keystore holding the key has signed the pending transaction.
  signed: boolean;
};

export type TVaultTxRecipient = {
  address: string;
  amount: TAmountWithConversions;
};

// The transaction of a vault that is waiting for its second signature.
export type TVaultPendingTx = {
  recipients: TVaultTxRecipient[];
  amount: TAmountWithConversions;
  fee: TAmountWithConversions;
  total: TAmountWithConversions;
  note: string;
  // The number of signatures needed to send the transaction.
  threshold: number;
  keys: TVaultTxKey[];
  numSignatures: number;
  // True if the connected keystore holds a key of the vault and has not signed yet.
  canSign: boolean;
  // True if the transaction has enough signatures and only needs to be sent.
  canBroadcast: boolean;
};

// Resolves to null if the vault has no pending transaction.
export const getVaultPendingTx = (accountCode: AccountCode): Promise<TVaultPendingTx | null> => {
  return apiGet(`vault/pending-tx/${accountCode}`);
};

// Signs the transaction last proposed for the vault with the first BitBox02. It is pending
// afterwards.
export const createVaultTx = (accountCode: AccountCode, note: string): Promise<TVaultResponse> => {
  return apiPost('vault/pending-tx/create', { accountCode, note });
};

export type TVaultSignTxResponse = {
  success: true;
  // Set once the transaction has enough signatures and was sent.
  txId?: string;
} | TVaultFailure;

export const signVaultTx = (accountCode: AccountCode): Promise<TVaultSignTxResponse> => {
  return apiPost('vault/pending-tx/sign', { accountCode });
};

export const discardVaultTx = (accountCode: AccountCode): Promise<TVaultResponse> => {
  return apiPost('vault/pending-tx/discard', { accountCode });
};
