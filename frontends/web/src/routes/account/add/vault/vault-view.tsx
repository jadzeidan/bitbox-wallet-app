// SPDX-License-Identifier: Apache-2.0

import { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { GuidedContent, GuideWrapper, Header, Main } from '@/components/layout';
import { View, ViewButtons, ViewContent, ViewHeader } from '@/components/view/view';
import { SubTitle } from '@/components/title';
import { useMediaQuery } from '@/hooks/mediaquery';
import styles from './vault.module.css';

type TProps = {
  title: string;
  children: ReactNode;
  buttons?: ReactNode;
  withIcon?: 'success';
};

// Common layout of the screens for adding a vault.
export const VaultView = ({
  title,
  children,
  buttons,
  withIcon,
}: TProps) => {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 768px)');
  return (
    <GuideWrapper>
      <GuidedContent>
        <Main>
          <Header
            variant="navigation"
            mobileBackButton
            onBack={() => window.history.back()}
            title={t('manageAccounts.title')}
          />
          <View
            fitContent
            verticallyCentered={!isMobile}
            width="var(--content-width-small)">
            <ViewHeader title={<p>{t('vault.title')}</p>}>
              <SubTitle className={styles.title}>
                {title}
              </SubTitle>
            </ViewHeader>
            <ViewContent
              minHeight="50px"
              textAlign="left"
              withIcon={withIcon}>
              <div className={styles.content}>
                {children}
              </div>
            </ViewContent>
            <ViewButtons>
              {buttons}
            </ViewButtons>
          </View>
        </Main>
      </GuidedContent>
    </GuideWrapper>
  );
};
