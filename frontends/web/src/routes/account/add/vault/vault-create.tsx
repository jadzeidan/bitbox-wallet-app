// SPDX-License-Identifier: Apache-2.0

import { FormEvent, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'wouter';
import type { AccountCode } from '@/api/account';
import {
  addVaultSetupKey,
  discardVaultSetup,
  exportVault,
  finishVaultSetup,
  getVaultSetup,
  registerVaultSetup,
  startVaultSetup,
  subscribeVaultSetup,
  TVaultFailure,
  TVaultSetup,
} from '@/api/vault';
import { useSync } from '@/hooks/api';
import { Button, Checkbox, Input } from '@/components/forms';
import { DesktopBackButton } from '@/components/backbutton/backbutton';
import { confirmation } from '@/components/confirm/Confirm';
import { CopyableInput } from '@/components/copy/Copy';
import { Message } from '@/components/message/message';
import { Skeleton } from '@/components/skeleton/skeleton';
import { getVaultErrorMessage } from './utils';
import { VaultKeys } from './vault-keys';
import { VaultView } from './vault-view';
import styles from './vault.module.css';

const VAULT_NAME_MAX_LENGTH = 30;

type TVaultAction = () => Promise<{ success: true } | TVaultFailure>;

type TStepProps = {
  setup: TVaultSetup;
  busy: boolean;
  // Runs a vault action and shows its error if it fails. With ignoreUserAbort, the user declining
  // the action is not shown as an error.
  run: (action: TVaultAction, ignoreUserAbort?: boolean) => Promise<void>;
};

const CollectKeys = ({ setup, busy, run }: TStepProps) => {
  const { t } = useTranslation();
  const connectedKeyAdded = setup.keys.some(key => key.connected);
  return (
    <>
      <p className={styles.description}>
        {t('vault.keys.threshold', { threshold: setup.threshold, numKeys: setup.numKeys })}
      </p>
      <VaultKeys keys={setup.keys} numKeys={setup.numKeys} />
      {setup.canAddKey ? (
        <Button
          primary
          disabled={busy}
          onClick={() => run(addVaultSetupKey)}>
          {t('vault.create.collectKeys.addKey', { number: setup.keys.length + 1 })}
        </Button>
      ) : (
        <Message type="info">
          {connectedKeyAdded
            ? t('vault.create.collectKeys.connectNext')
            : t('vault.create.collectKeys.connectAny')}
        </Message>
      )}
    </>
  );
};

const Register = ({ setup, busy, run }: TStepProps) => {
  const { t } = useTranslation();
  const nextKey = setup.keys.find(key => !key.registered);
  return (
    <>
      <p className={styles.description}>
        {t('vault.create.register.description')}
      </p>
      <VaultKeys keys={setup.keys} numKeys={setup.numKeys} showRegistered showXpubs />
      {setup.canRegister ? (
        <>
          {busy && (
            <Message type="info">
              {t('vault.create.register.confirming')}
            </Message>
          )}
          <Button
            primary
            disabled={busy}
            onClick={() => run(registerVaultSetup)}>
            {t('vault.create.register.button')}
          </Button>
        </>
      ) : (
        <Message type="info">
          {nextKey?.keystoreName
            ? t('vault.create.register.connect', { name: nextKey.keystoreName })
            : t('vault.create.register.connectUnnamed')}
        </Message>
      )}
    </>
  );
};

type TBackupProps = TStepProps & {
  acknowledged: boolean;
  onAcknowledge: (acknowledged: boolean) => void;
};

const Backup = ({ setup, busy, run, acknowledged, onAcknowledge }: TBackupProps) => {
  const { t } = useTranslation();
  const accountCode = setup.accountCode;
  return (
    <>
      <p className={styles.description}>
        {t('vault.create.backup.description')}
      </p>
      <CopyableInput flexibleHeight value={setup.descriptor || ''} />
      {accountCode && (
        <Button
          secondary
          disabled={busy}
          onClick={() => run(() => exportVault(accountCode), true)}>
          {t('vault.create.backup.save')}
        </Button>
      )}
      <Checkbox
        id="vault-backup-acknowledge"
        checked={acknowledged}
        onChange={event => onAcknowledge(event.target.checked)}
        label={t('vault.create.backup.acknowledge')} />
    </>
  );
};

// Guides the user through creating a vault: naming it, collecting one key from each of three
// BitBox02s, registering the vault on each of them and backing up the vault file. The progress is
// kept by the backend, as the user swaps devices between the steps.
export const VaultCreate = () => {
  const { t } = useTranslation();
  const [, navigate] = useLocation();
  const setup = useSync(getVaultSetup, subscribeVaultSetup);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string>();
  const [acknowledged, setAcknowledged] = useState(false);
  const [addedAccountCode, setAddedAccountCode] = useState<AccountCode>();

  const run = async (action: TVaultAction, ignoreUserAbort = false) => {
    setBusy(true);
    setErrorMessage(undefined);
    const response = await action();
    setBusy(false);
    if (!response.success && !(ignoreUserAbort && response.errorCode === 'userAbort')) {
      setErrorMessage(getVaultErrorMessage(t, response));
    }
  };

  const start = (event: FormEvent) => {
    event.preventDefault();
    run(() => startVaultSetup(name));
  };

  const finish = async () => {
    setBusy(true);
    setErrorMessage(undefined);
    const response = await finishVaultSetup();
    setBusy(false);
    if (!response.success) {
      setErrorMessage(getVaultErrorMessage(t, response));
      return;
    }
    setAddedAccountCode(response.accountCode);
  };

  const discard = () => {
    confirmation(t('vault.create.discard.confirm'), async confirmed => {
      if (!confirmed) {
        return;
      }
      await discardVaultSetup();
      navigate('/add-account');
    });
  };

  const backButton = (
    <DesktopBackButton onClick={() => window.history.back()}>
      {t('button.back')}
    </DesktopBackButton>
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
        <p className={styles.description}>{t('vault.success.created')}</p>
      </VaultView>
    );
  }

  if (setup === undefined) {
    return (
      <VaultView title={t('loading')} buttons={backButton}>
        <Skeleton fontSize="4rem" />
      </VaultView>
    );
  }

  const error = (
    <Message type="warning" hidden={!errorMessage}>
      {errorMessage}
    </Message>
  );

  if (setup === null) {
    return (
      <VaultView
        title={t('vault.create.name.title')}
        buttons={(
          <>
            <Button
              primary
              disabled={busy || name.trim() === ''}
              onClick={start}>
              {t('vault.create.name.button')}
            </Button>
            {backButton}
          </>
        )}>
        {error}
        <form onSubmit={start}>
          <Input
            autoFocus
            id="vaultName"
            label={t('vault.create.name.label')}
            maxLength={VAULT_NAME_MAX_LENGTH}
            onInput={event => setName(event.currentTarget.value)}
            value={name} />
        </form>
        <p className={styles.description}>{t('vault.create.name.hint')}</p>
      </VaultView>
    );
  }

  const discardButton = (
    <Button transparent disabled={busy} onClick={discard}>
      {t('vault.create.discard.button')}
    </Button>
  );

  switch (setup.step) {
  case 'collectKeys':
    return (
      <VaultView
        title={t('vault.create.collectKeys.title', { name: setup.name })}
        buttons={<>{discardButton}{backButton}</>}>
        {error}
        <CollectKeys setup={setup} busy={busy} run={run} />
      </VaultView>
    );
  case 'register':
    return (
      <VaultView
        title={t('vault.create.register.title')}
        buttons={<>{discardButton}{backButton}</>}>
        {error}
        <Register setup={setup} busy={busy} run={run} />
      </VaultView>
    );
  case 'backup':
    return (
      <VaultView
        title={t('vault.create.backup.title')}
        buttons={(
          <Button
            primary
            disabled={busy || !acknowledged}
            onClick={finish}>
            {t('vault.create.backup.button')}
          </Button>
        )}>
        {error}
        <Backup
          setup={setup}
          busy={busy}
          run={run}
          acknowledged={acknowledged}
          onAcknowledge={setAcknowledged} />
      </VaultView>
    );
  }
};
