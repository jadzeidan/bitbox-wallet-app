// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { AccountCode } from '@/api/account';
import { discardVaultTx, signVaultTx, TVaultPendingTx } from '@/api/vault';
import { alertUser } from '@/components/alert/Alert';
import { AmountWithUnit } from '@/components/amount/amount-with-unit';
import { Badge } from '@/components/badge/badge';
import { confirmation } from '@/components/confirm/Confirm';
import { Button } from '@/components/forms';
import { Message } from '@/components/message/message';
import { getVaultErrorMessage, withVaultRegistration } from '@/routes/account/add/vault/utils';
import style from './vault-pending-tx.module.css';

type TProps = {
  accountCode: AccountCode;
  pendingTx: TVaultPendingTx;
  // Called after the pending transaction was signed, sent or discarded.
  onChange: () => void;
};

// Shows the transaction of a vault that is waiting for its second signature, and lets the user
// sign it with the connected BitBox02, send it, or discard it.
export const VaultPendingTx = ({ accountCode, pendingTx, onChange }: TProps) => {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);

  const sign = async () => {
    setBusy(true);
    const response = await withVaultRegistration(accountCode, () => signVaultTx(accountCode));
    setBusy(false);
    if (!response.success) {
      if (response.errorCode !== 'userAbort') {
        alertUser(getVaultErrorMessage(t, response));
      }
    } else if (response.txId) {
      alertUser(t('vault.tx.sent'));
    }
    onChange();
  };

  const discard = () => {
    confirmation(t('vault.tx.discardConfirm'), async confirmed => {
      if (!confirmed) {
        return;
      }
      const response = await discardVaultTx(accountCode);
      if (!response.success) {
        alertUser(getVaultErrorMessage(t, response));
      }
      onChange();
    });
  };

  const unsignedNames = pendingTx.keys
    .filter(key => !key.signed && !key.connected)
    .map(key => key.keystoreName)
    .filter(name => name !== '');

  return (
    <section className={style.container} data-testid="vault-pending-tx">
      <div className={style.header}>
        <h3 className={style.title}>{t('vault.tx.title')}</h3>
        <Badge type={pendingTx.canBroadcast ? 'success' : 'warning'}>
          {t('vault.tx.signatures', {
            signed: pendingTx.numSignatures,
            threshold: pendingTx.threshold,
          })}
        </Badge>
      </div>

      <dl className={style.details}>
        {pendingTx.recipients.map(recipient => (
          <div key={recipient.address} className={style.row}>
            <dt>{t('vault.tx.recipient')}</dt>
            <dd>
              <span className={style.address}>{recipient.address}</span>
              <AmountWithUnit amount={recipient.amount} alwaysShowAmounts />
            </dd>
          </div>
        ))}
        <div className={style.row}>
          <dt>{t('vault.tx.fee')}</dt>
          <dd><AmountWithUnit amount={pendingTx.fee} alwaysShowAmounts /></dd>
        </div>
        <div className={style.row}>
          <dt>{t('vault.tx.total')}</dt>
          <dd><AmountWithUnit amount={pendingTx.total} alwaysShowAmounts /></dd>
        </div>
        {pendingTx.note !== '' && (
          <div className={style.row}>
            <dt>{t('vault.tx.note')}</dt>
            <dd>{pendingTx.note}</dd>
          </div>
        )}
      </dl>

      <ul className={style.keys}>
        {pendingTx.keys.map((key, index) => (
          <li key={key.rootFingerprint} className={style.key}>
            <span>
              {t('vault.keys.key', { number: index + 1 })}
              <span className={style.keyName}>{key.keystoreName || key.rootFingerprint}</span>
            </span>
            <span className={style.badges}>
              {key.connected && (
                <Badge type="info">{t('vault.keys.connected')}</Badge>
              )}
              {key.signed && (
                <Badge type="success">{t('vault.tx.signed')}</Badge>
              )}
            </span>
          </li>
        ))}
      </ul>

      {busy && (
        <Message type="info">{t('vault.tx.signing')}</Message>
      )}
      {!busy && !pendingTx.canSign && !pendingTx.canBroadcast && (
        <Message type="info">
          {unsignedNames.length > 0
            ? t('vault.tx.connectOther', { names: unsignedNames.join(', ') })
            : t('vault.tx.connectOtherUnnamed')}
        </Message>
      )}

      <div className={style.buttons}>
        {pendingTx.canBroadcast && (
          <Button primary disabled={busy} onClick={sign}>
            {t('vault.tx.broadcast')}
          </Button>
        )}
        {pendingTx.canSign && (
          <Button primary disabled={busy} onClick={sign}>
            {t('vault.tx.sign')}
          </Button>
        )}
        <Button transparent disabled={busy} onClick={discard}>
          {t('vault.tx.discard')}
        </Button>
      </div>
    </section>
  );
};
