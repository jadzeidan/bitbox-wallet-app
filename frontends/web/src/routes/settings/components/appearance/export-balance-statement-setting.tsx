// SPDX-License-Identifier: Apache-2.0

import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import * as accountApi from '@/api/account';
import { exportBalanceStatement } from '@/api/backend';
import { alertUser } from '@/components/alert/Alert';
import { Dialog, DialogButtons, DialogScrollContent } from '@/components/dialog/dialog';
import { Button, Checkbox, Input, Label } from '@/components/forms';
import { SettingsItem } from '@/routes/settings/components/settingsItem/settingsItem';
import styles from './export-balance-statement-setting.module.css';

type TProps = {
  accounts: accountApi.TAccount[];
};

const formatDateInput = (date: Date): string => {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
};

const getDefaultSnapshotDate = (): string => {
  return `${new Date().getFullYear() - 1}-12-31`;
};

export const ExportBalanceStatementSetting = ({ accounts }: TProps) => {
  const { t } = useTranslation();
  const activeAccounts = useMemo(() => accounts.filter(account => account.active), [accounts]);
  const [open, setOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [snapshotDate, setSnapshotDate] = useState(getDefaultSnapshotDate());
  const [selectedAccountCodes, setSelectedAccountCodes] = useState<accountApi.AccountCode[]>([]);

  const openDialog = () => {
    setSelectedAccountCodes(activeAccounts.map(({ code }) => code));
    setSnapshotDate(getDefaultSnapshotDate());
    setOpen(true);
  };

  const onToggleAccount = (accountCode: accountApi.AccountCode, selected: boolean) => {
    if (selected) {
      setSelectedAccountCodes(prev => prev.includes(accountCode) ? prev : [...prev, accountCode]);
      return;
    }
    setSelectedAccountCodes(prev => prev.filter(code => code !== accountCode));
  };

  const onGenerate = async () => {
    try {
      setSubmitting(true);
      const result = await exportBalanceStatement(selectedAccountCodes, snapshotDate);
      if (result.success) {
        alertUser(t('settings.balanceStatement.export.success'));
        setOpen(false);
      } else if (!result.aborted && result.message) {
        alertUser(result.message);
      }
    } catch (error) {
      console.error('Failed to export balance statement', error);
      alertUser(t('genericError'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <SettingsItem
        disabled={activeAccounts.length === 0}
        settingName={t('settings.balanceStatement.export.title')}
        secondaryText={t('settings.balanceStatement.export.description')}
        onClick={openDialog}
      />
      <Dialog
        medium
        open={open}
        onClose={() => setOpen(false)}
        title={t('settings.balanceStatement.export.dialog.title')}>
        <DialogScrollContent>
          <p className={styles.description}>
            {t('settings.balanceStatement.export.dialog.description')}
          </p>
          <Label className="m-bottom-half">
            {t('settings.balanceStatement.export.dialog.accountsLabel')}
          </Label>
          <div className={styles.accountsList}>
            {activeAccounts.length === 0 ? (
              <p>{t('settings.balanceStatement.export.dialog.noAccounts')}</p>
            ) : activeAccounts.map((account) => (
              <div className={styles.accountItem} key={account.code}>
                <Checkbox
                  checked={selectedAccountCodes.includes(account.code)}
                  id={`statement-account-${account.code}`}
                  label={`${account.name} (${account.coinUnit})`}
                  onChange={(event) => onToggleAccount(account.code, event.target.checked)}
                />
              </div>
            ))}
          </div>
          <Input
            className={styles.dateInput}
            id="statement-snapshot-date"
            label={t('settings.balanceStatement.export.dialog.dateLabel')}
            max={formatDateInput(new Date())}
            onChange={(event) => setSnapshotDate(event.target.value)}
            type="date"
            value={snapshotDate}
          />
        </DialogScrollContent>
        <DialogButtons>
          <Button
            disabled={selectedAccountCodes.length === 0 || snapshotDate.length === 0 || submitting}
            onClick={onGenerate}
            primary>
            {t('settings.balanceStatement.export.dialog.generate')}
          </Button>
          <Button onClick={() => setOpen(false)} secondary>
            {t('dialog.cancel')}
          </Button>
        </DialogButtons>
      </Dialog>
    </>
  );
};
