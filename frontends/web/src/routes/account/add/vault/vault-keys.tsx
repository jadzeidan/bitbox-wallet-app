// SPDX-License-Identifier: Apache-2.0

import { useTranslation } from 'react-i18next';
import type { TVaultKey } from '@/api/vault';
import { Badge } from '@/components/badge/badge';
import styles from './vault.module.css';

type TKey = TVaultKey & {
  registered?: boolean;
};

type TProps = {
  keys: TKey[];
  // The number of keys of the vault. Keys that are not collected yet are shown as empty slots.
  numKeys: number;
  showXpubs?: boolean;
  showRegistered?: boolean;
};

export const VaultKeys = ({
  keys,
  numKeys,
  showXpubs = false,
  showRegistered = false,
}: TProps) => {
  const { t } = useTranslation();
  const slots = Array.from({ length: numKeys }, (_, index) => keys[index]);
  return (
    <ol className={styles.keys}>
      {slots.map((key, index) => (
        <li
          key={key ? key.rootFingerprint : `empty-${index}`}
          className={`${styles.key || ''} ${key ? '' : styles.keyEmpty || ''}`}>
          <div className={styles.keyHeader}>
            <span className={styles.keyTitle}>
              {t('vault.keys.key', { number: index + 1 })}
              {key && (
                <span className={styles.keyName}>
                  {key.keystoreName || key.rootFingerprint}
                </span>
              )}
            </span>
            {key ? (
              <span className={styles.keyBadges}>
                {key.connected && (
                  <Badge type="info">{t('vault.keys.connected')}</Badge>
                )}
                {showRegistered && (
                  <Badge type={key.registered ? 'success' : 'warning'}>
                    {key.registered ? t('vault.keys.registered') : t('vault.keys.notRegistered')}
                  </Badge>
                )}
              </span>
            ) : (
              <span className={styles.keyHint}>{t('vault.keys.empty')}</span>
            )}
          </div>
          {key && showXpubs && (
            <p className={styles.keyXpub} data-testid={`vault-key-xpub-${index}`}>
              {key.xpub}
            </p>
          )}
        </li>
      ))}
    </ol>
  );
};
