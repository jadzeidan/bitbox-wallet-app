// SPDX-License-Identifier: Apache-2.0

import { useTranslation } from 'react-i18next';
import { NavLink } from 'react-router-dom';
import style from './portfolio-tabs.module.css';

export const PortfolioTabs = () => {
  const { t } = useTranslation();

  return (
    <div className={style.container}>
      <NavLink
        end
        className={({ isActive }) => isActive ? style.active : ''}
        to="/account-summary"
      >
        {t('accountSummary.tabs.portfolio')}
      </NavLink>
      <NavLink
        className={({ isActive }) => isActive ? style.active : ''}
        to="/account-summary/markets"
      >
        {t('accountSummary.tabs.markets')}
      </NavLink>
    </div>
  );
};
