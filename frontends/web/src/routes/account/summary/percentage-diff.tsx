// SPDX-License-Identifier: Apache-2.0

import { useContext } from 'react';
import type { TPortfolioPercentageType } from '@/contexts/AppContext';
import { Badge } from '@/components/badge/badge';
import { AppContext } from '@/contexts/AppContext';
import { localizePercentage } from '@/utils/localize';
import { ArrowDownRed, ArrowUpGreen } from '@/components/icon';
import performanceIcon from './assets/performance-icon.svg';
import valueIcon from './assets/value-icon.svg';
import styles from './percentage-diff.module.css';

type TPercentageDiff = {
  badgeVisible?: boolean;
  hasDifference: boolean;
  difference?: number;
  onClick?: () => void;
  switchedLabel?: string;
  switchedType?: TPortfolioPercentageType;
  title?: string;
};

export const PercentageDiff = ({
  badgeVisible = false,
  difference,
  hasDifference,
  onClick,
  switchedLabel,
  switchedType,
  title,
}: TPercentageDiff) => {
  const { hideAmounts, nativeLocale } = useContext(AppContext);
  const positive = difference && difference > 0;
  const style = difference && positive ? 'up' : 'down';
  const className = hasDifference ? (styles[style] || '') : '';
  const badgeClassName = [
    styles.badge,
    badgeVisible ? styles.badgeVisible : '',
  ].filter(Boolean).join(' ');
  const badgeIcon = switchedType === 'moneyWeightedReturn' ? performanceIcon : valueIcon;
  const formattedDifference = difference && localizePercentage(difference, nativeLocale);
  const content = hasDifference ? (
    <>
      <span className={styles.arrow}>
        {positive ? (
          <ArrowUpGreen />
        ) : (
          <ArrowDownRed />
        )}
      </span>
      <span className={styles.diffValue}>
        {hideAmounts ? '***' : formattedDifference}
        <span className={styles.diffUnit}>%</span>
      </span>
    </>
  ) : null;

  return (
    <span className={styles.container}>
      {onClick ? (
        <button className={`${styles.button || ''} ${className}`} onClick={onClick} title={title} type="button">
          {content}
        </button>
      ) : (
        <span className={className} title={title}>
          {content}
        </span>
      )}
      {switchedLabel ? (
        <Badge className={badgeClassName} type="info">
          <span className={styles.badgeContent}>
            <span>{switchedLabel}</span>
            <img alt="" aria-hidden="true" className={styles.badgeIcon} src={badgeIcon} />
          </span>
        </Badge>
      ) : null}
    </span>
  );
};
