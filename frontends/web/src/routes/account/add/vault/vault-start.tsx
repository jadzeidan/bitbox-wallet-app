// SPDX-License-Identifier: Apache-2.0

import { useTranslation } from 'react-i18next';
import { useLocation } from 'wouter';
import { getVaultSetup, subscribeVaultSetup } from '@/api/vault';
import { useSync } from '@/hooks/api';
import { ActionableItem } from '@/components/actionable-item/actionable-item';
import { DesktopBackButton } from '@/components/backbutton/backbutton';
import { Skeleton } from '@/components/skeleton/skeleton';
import { VaultView } from './vault-view';
import styles from './vault.module.css';

type TOptionProps = {
  title: string;
  description: string;
  onClick: () => void;
};

const Option = ({ title, description, onClick }: TOptionProps) => (
  <ActionableItem className={styles.option} onClick={onClick}>
    <span className={styles.optionText}>
      <span className={styles.optionTitle}>{title}</span>
      <span className={styles.optionDescription}>{description}</span>
    </span>
  </ActionableItem>
);

// Lets the user choose between creating a vault and adding an existing one. A vault that is
// being created is continued instead of starting another one.
export const VaultStart = () => {
  const { t } = useTranslation();
  const [, navigate] = useLocation();
  const setup = useSync(getVaultSetup, subscribeVaultSetup);

  return (
    <VaultView
      title={t('vault.start.title')}
      buttons={(
        <DesktopBackButton onClick={() => window.history.back()}>
          {t('button.back')}
        </DesktopBackButton>
      )}>
      {setup === undefined ? (
        <Skeleton fontSize="4rem" />
      ) : (
        <div className={styles.options}>
          {setup === null ? (
            <Option
              title={t('vault.start.create.title')}
              description={t('vault.start.create.description')}
              onClick={() => navigate('/add-account/vault/create')} />
          ) : (
            <Option
              title={t('vault.start.resume.title', { name: setup.name })}
              description={t('vault.start.resume.description')}
              onClick={() => navigate('/add-account/vault/create')} />
          )}
          <Option
            title={t('vault.start.import.title')}
            description={t('vault.start.import.description')}
            onClick={() => navigate('/add-account/vault/import')} />
        </div>
      )}
    </VaultView>
  );
};
