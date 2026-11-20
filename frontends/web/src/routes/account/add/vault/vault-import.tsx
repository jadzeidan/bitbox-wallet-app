// SPDX-License-Identifier: Apache-2.0

import { ChangeEvent, FormEvent, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'wouter';
import type { AccountCode } from '@/api/account';
import { addVault, getVaultInfo, TVaultInfo } from '@/api/vault';
import { Button, Input } from '@/components/forms';
import { DesktopBackButton } from '@/components/backbutton/backbutton';
import { Message } from '@/components/message/message';
import { getVaultErrorMessage } from './utils';
import { VaultKeys } from './vault-keys';
import { VaultView } from './vault-view';
import styles from './vault.module.css';

const VAULT_NAME_MAX_LENGTH = 30;

// Adds a vault that already exists from its vault file. The user reviews the keys, and the vault
// is registered on the connected BitBox02 before the account is added.
export const VaultImport = () => {
  const { t } = useTranslation();
  const [, navigate] = useLocation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [descriptor, setDescriptor] = useState('');
  const [vault, setVault] = useState<TVaultInfo>();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string>();
  const [addedAccountCode, setAddedAccountCode] = useState<AccountCode>();

  const loadFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    // Allow selecting the same file again.
    event.target.value = '';
    if (!file) {
      return;
    }
    setDescriptor((await file.text()).trim());
    setErrorMessage(undefined);
  };

  const review = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setErrorMessage(undefined);
    const response = await getVaultInfo(descriptor.trim());
    setBusy(false);
    if (!response.success) {
      setErrorMessage(getVaultErrorMessage(t, response));
      return;
    }
    setVault(response.vault);
  };

  const add = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setErrorMessage(undefined);
    const response = await addVault(descriptor.trim(), name);
    setBusy(false);
    if (!response.success) {
      setErrorMessage(getVaultErrorMessage(t, response));
      return;
    }
    setAddedAccountCode(response.accountCode);
  };

  const error = (
    <Message type="warning" hidden={!errorMessage}>
      {errorMessage}
    </Message>
  );

  if (addedAccountCode) {
    return (
      <VaultView
        title={t('vault.success.title')}
        withIcon="success"
        buttons={(
          <Button primary onClick={() => navigate(`/account/${addedAccountCode}`)}>
            {t('vault.success.button')}
          </Button>
        )}>
        <p className={styles.description}>{t('vault.success.imported')}</p>
      </VaultView>
    );
  }

  if (vault) {
    return (
      <VaultView
        title={t('vault.import.review.title')}
        buttons={(
          <>
            <Button
              primary
              disabled={busy || name.trim() === ''}
              onClick={add}>
              {t('vault.import.review.button')}
            </Button>
            <DesktopBackButton
              onClick={() => {
                setVault(undefined);
                setErrorMessage(undefined);
              }}>
              {t('button.back')}
            </DesktopBackButton>
          </>
        )}>
        {error}
        <p className={styles.description}>
          {t('vault.keys.threshold', { threshold: vault.threshold, numKeys: vault.keys.length })}
        </p>
        <VaultKeys keys={vault.keys} numKeys={vault.keys.length} showXpubs />
        <form onSubmit={add}>
          <Input
            autoFocus
            id="vaultName"
            label={t('vault.create.name.label')}
            maxLength={VAULT_NAME_MAX_LENGTH}
            onInput={event => setName(event.currentTarget.value)}
            value={name} />
        </form>
        {busy ? (
          <Message type="info">{t('vault.create.register.confirming')}</Message>
        ) : !vault.registered && (
          <p className={styles.description}>{t('vault.import.review.registerNote')}</p>
        )}
      </VaultView>
    );
  }

  return (
    <VaultView
      title={t('vault.import.title')}
      buttons={(
        <>
          <Button
            primary
            disabled={busy || descriptor.trim() === ''}
            onClick={review}>
            {t('vault.import.continue')}
          </Button>
          <DesktopBackButton onClick={() => window.history.back()}>
            {t('button.back')}
          </DesktopBackButton>
        </>
      )}>
      {error}
      <p className={styles.description}>{t('vault.import.description')}</p>
      <form onSubmit={review}>
        <textarea
          aria-label={t('vault.import.descriptor.label')}
          className={styles.descriptor}
          onChange={event => setDescriptor(event.target.value)}
          placeholder="wsh(sortedmulti(2,…"
          spellCheck={false}
          value={descriptor} />
      </form>
      <div className={styles.secondaryActions}>
        <input
          ref={fileInputRef}
          accept=".txt,text/plain"
          className={styles.fileInput}
          data-testid="vault-file-input"
          onChange={loadFile}
          type="file" />
        <Button secondary onClick={() => fileInputRef.current?.click()}>
          {t('vault.import.descriptor.load')}
        </Button>
      </div>
    </VaultView>
  );
};
