// SPDX-License-Identifier: Apache-2.0

import { useContext } from 'react';
import type { TPortfolioPercentageType } from '@/contexts/AppContext';
import { Badge } from '@/components/badge/badge';
import { AppContext } from '@/contexts/AppContext';
import { localizePercentage } from '@/utils/localize';
import { ArrowDownRed, ArrowUpGreen } from '@/components/icon';
import styles from './percentage-diff.module.css';
import { LocalizationContext } from '@/contexts/localization-context';

type TProps = {
  badgeVisible?: boolean;
  hasDifference: boolean;
  difference?: number;
  onClick?: () => void;
  switchedLabel?: string;
  switchedType?: TPortfolioPercentageType;
  title?: string;
};

type TBadgeIconProps = {
  className?: string;
};

const PerformanceBadgeIcon = ({ className }: TBadgeIconProps) => (
  <svg aria-hidden="true" className={className || ''} fill="none" viewBox="0 0 20 15" xmlns="http://www.w3.org/2000/svg">
    <path d="M19.1959 0.00172379L14.5271 0.564224C14.3724 0.582974 14.3068 0.772817 14.417 0.882973L15.8068 2.27282L10.5099 7.56969L8.124 5.1861C7.97634 5.03844 7.73962 5.04079 7.59431 5.1861L0.0544689 12.7283C0.0195746 12.7635 0 12.8111 0 12.8607C0 12.9103 0.0195746 12.9579 0.0544689 12.9931L1.10916 14.0525C1.18181 14.1252 1.30134 14.1252 1.374 14.0525L7.85916 7.56969L10.2428 9.95329C10.3904 10.0986 10.6271 10.0986 10.7724 9.95329L17.1334 3.59704L18.5232 4.98688C18.5481 5.01166 18.5795 5.02896 18.6137 5.0368C18.648 5.04464 18.6837 5.04272 18.717 5.03124C18.7502 5.01976 18.7795 4.9992 18.8016 4.97188C18.8237 4.94457 18.8377 4.9116 18.842 4.87672L19.4045 0.207973C19.4209 0.0884423 19.3178 -0.0146825 19.1959 0.00172379Z" fill="currentColor" />
  </svg>
);

const ValueBadgeIcon = ({ className }: TBadgeIconProps) => (
  <svg aria-hidden="true" className={className || ''} fill="none" viewBox="0 0 14 14" xmlns="http://www.w3.org/2000/svg">
    <path d="M0.3677 14H2.94161C3.14384 14 3.30931 13.8788 3.30931 13.7308V2.96154C3.30931 2.81346 3.14384 2.69231 2.94161 2.69231H0.3677C0.165464 2.69231 -1.66893e-06 2.81346 -1.66893e-06 2.96154V13.7308C-1.66893e-06 13.8788 0.165464 14 0.3677 14ZM5.67702 14H8.25093C8.45317 14 8.61863 13.8788 8.61863 13.7308V5.58654C8.61863 5.43846 8.45317 5.31731 8.25093 5.31731H5.67702C5.47479 5.31731 5.30932 5.43846 5.30932 5.58654V13.7308C5.30932 13.8788 5.47479 14 5.67702 14ZM10.8677 14H13.4416C13.6439 14 13.8093 13.8788 13.8093 13.7308V0.269231C13.8093 0.121154 13.6439 0 13.4416 0H10.8677C10.6655 0 10.5 0.121154 10.5 0.269231V13.7308C10.5 13.8788 10.6655 14 10.8677 14Z" fill="currentColor" />
  </svg>
);

export const PercentageDiff = ({
  badgeVisible = false,
  difference,
  hasDifference,
  onClick,
  switchedLabel,
  switchedType,
  title,
}: TProps) => {
  const { hideAmounts, nativeLocale } = useContext(AppContext);
  const { decimal, group } = useContext(LocalizationContext);
  const positive = difference && difference > 0;
  const style = difference && positive ? 'up' : 'down';
  const className = hasDifference ? (styles[style] || '') : '';
  const badgeClassName = [
    styles.badge,
    badgeVisible ? styles.badgeVisible : '',
  ].filter(Boolean).join(' ');
  const valueBadgeIconClassName = [
    styles.badgeIcon,
    styles.valueBadgeIcon,
  ].filter(Boolean).join(' ');
  const badgeIcon = switchedType === 'moneyWeightedReturn'
    ? <PerformanceBadgeIcon className={styles.badgeIcon} />
    : <ValueBadgeIcon className={valueBadgeIconClassName} />;
  const formattedDifference = difference && localizePercentage(difference, nativeLocale, { decimal, group });
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
        <button
          className={`${styles.button || ''} ${className}`}
          data-testid="portfolio-percentage-toggle"
          onClick={onClick}
          title={title}
          type="button">
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
            {badgeIcon}
          </span>
        </Badge>
      ) : null}
    </span>
  );
};
