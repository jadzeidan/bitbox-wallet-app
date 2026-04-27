// SPDX-License-Identifier: Apache-2.0

import { useTranslation } from 'react-i18next';
import { Main, Header, GuideWrapper, GuidedContent } from '@/components/layout';
import { View, ViewContent } from '@/components/view/view';
import { Button } from '@/components/forms';
import { ContentWrapper } from '@/components/contentwrapper/contentwrapper';
import { GlobalBanners } from '@/components/banners';
import { MobileHeader } from './components/mobile-header';
import { WithSettingsTabs } from './components/tabs';
import { TPagePropsWithSettingsTabs } from './types';
import { useToast } from '@/components/toast/toast';
import { TMessageTypes } from '@/utils/types';
import style from './toast-demo.module.css';

export const ToastDemo = ({ devices, hasAccounts }: TPagePropsWithSettingsTabs) => {
  const { t } = useTranslation();
  const { showToast } = useToast();

  const show = (type: TMessageTypes, duration?: number) => {
    showToast({
      duration,
      message: t(`toastDemo.examples.${type}`),
      type,
    });
  };

  return (
    <GuideWrapper>
      <GuidedContent>
        <Main>
          <ContentWrapper>
            <GlobalBanners devices={devices} />
          </ContentWrapper>
          <Header
            hideSidebarToggler
            title={
              <>
                <h2 className="hide-on-small">{t('sidebar.settings')}</h2>
                <MobileHeader withGuide title={t('toastDemo.title')} />
              </>
            } />
          <View fullscreen={false}>
            <ViewContent>
              <WithSettingsTabs devices={devices} hasAccounts={hasAccounts} hideMobileMenu>
                <p className={style.description}>{t('toastDemo.description')}</p>
                <div className={style.buttons}>
                  <Button secondary onClick={() => show('info')}>
                    {t('toastDemo.buttons.info')}
                  </Button>
                  <Button secondary onClick={() => show('success')}>
                    {t('toastDemo.buttons.success')}
                  </Button>
                  <Button secondary onClick={() => show('warning')}>
                    {t('toastDemo.buttons.warning')}
                  </Button>
                  <Button secondary onClick={() => show('error')}>
                    {t('toastDemo.buttons.error')}
                  </Button>
                  <Button primary onClick={() => show('info', 12000)}>
                    {t('toastDemo.buttons.long')}
                  </Button>
                </div>
              </WithSettingsTabs>
            </ViewContent>
          </View>
        </Main>
      </GuidedContent>
    </GuideWrapper>
  );
};
