// SPDX-License-Identifier: Apache-2.0

import { ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AccountCode, TBitcoinMultisig } from '@/api/account';
import { exportVault } from '@/api/vault';
import { alertUser } from '@/components/alert/Alert';
import { CopyableInput } from '@/components/copy/Copy';
import { Button } from '@/components/forms';
import { getVaultErrorMessage } from '@/routes/account/add/vault/utils';
import { VaultKeys } from '@/routes/account/add/vault/vault-keys';
import style from './info.module.css';

type TProps = {
  code: AccountCode;
  multisig: TBitcoinMultisig;
  children: ReactNode;
};

// Shows the keys of a vault account and its vault file, which is needed to recover the funds.
export const VaultDetails = ({ code, multisig, children }: TProps) => {
  const { t } = useTranslation();
  const [saving, setSaving] = useState(false);

  const save = async () => {
    setSaving(true);
    const response = await exportVault(code);
    setSaving(false);
    // Declining to pick a file is not an error.
    if (!response.success && response.errorCode !== 'userAbort') {
      alertUser(getVaultErrorMessage(t, response));
    }
  };

  const keys = multisig.keys.map(key => ({
    rootFingerprint: key.rootFingerprint,
    keypath: key.keypath,
    xpub: key.xpub,
    keystoreName: '',
    connected: false,
  }));

  return (
    <div className={style.vaultDetails}>
      <p>
        {t('vault.keys.threshold', { threshold: multisig.threshold, numKeys: keys.length })}
      </p>
      <VaultKeys keys={keys} numKeys={keys.length} showXpubs />
      <p>{t('vault.details.vaultFile')}</p>
      <CopyableInput flexibleHeight value={multisig.descriptor} />
      <div className="buttons">
        <Button primary disabled={saving} onClick={save}>
          {t('vault.create.backup.save')}
        </Button>
        {children}
      </div>
    </div>
  );
};
