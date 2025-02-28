/**
 * Copyright 2018 Shift Devices AG
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import i18n from '../../i18n/i18n';
import { useLoad } from '../../hooks/api';
import { useDarkmode } from '../../hooks/darkmode';
import { getTesting } from '../../api/backend';
import { Entry } from '../../components/guide/entry';
import { Guide, store as panelStore } from '../../components/guide/guide';
import { AppLogo, AppLogoInverted, SwissMadeOpenSource, SwissMadeOpenSourceDark } from '../../components/icon/logo';
import { Footer, Header } from '../../components/layout';
import { setSidebarStatus } from '../../components/sidebar/sidebar';
import { debug } from '../../utils/env';
import { SkipForTesting } from './components/skipfortesting';
import style from './bitbox01/bitbox01.module.css';

export const Waiting = () => {
  const { t } = useTranslation();
  const testing = useLoad(debug ? getTesting : () => Promise.resolve(false));
  const darkmode = useDarkmode();

  useEffect(() => {
    const { sidebarStatus } = panelStore.state;
    if (['forceCollapsed', 'forceHidden'].includes(sidebarStatus)) {
      setSidebarStatus('');
    }
  }, []);

  return (
    <div className="contentWithGuide">
      <div className="container">
        <Header title={<h2>{t('welcome.title')}</h2>} />
        <div className="content padded narrow isVerticallyCentered">
          <div>
            {darkmode ? (<AppLogoInverted />) : (<AppLogo />)}
            <div className="box large">
              <h3 className={style.waitingText}>{t('welcome.insertDevice')}</h3>
              <p className={style.waitingDescription}>{t('welcome.insertBitBox02')}</p>
            </div>
            {
              testing && (
                <div className={style.testingContainer}>
                  <SkipForTesting show />
                </div>
              )
            }
          </div>
        </div>
        <Footer>
          {darkmode ? (<SwissMadeOpenSourceDark />) : (<SwissMadeOpenSource />)}
        </Footer>
      </div>
      <Guide>
        <Entry entry={t('guide.waiting.welcome')} shown={true} />
        <Entry entry={{
          link: {
            text: t('guide.waiting.getDevice.link.text'),
            url: 'https://shiftcrypto.shop/',
          },
          text: t('guide.waiting.getDevice.text'),
          title: t('guide.waiting.getDevice.title'),
        }} />
        <Entry entry={{
          link: {
            text: t('guide.waiting.lostDevice.link.text'),
            url: (i18n.language === 'de')
              ? 'https://shiftcrypto.support/help/de-de/5-backup/8-wie-kann-ich-ein-bitbox02-wallet-in-ein-drittanbieter-wallet-importieren'
              : 'https://shiftcrypto.support/help/en-us/5-backup/8-how-do-i-restore-my-wallet-if-my-bitbox02-is-lost',
          },
          text: t('guide.waiting.lostDevice.text'),
          title: t('guide.waiting.lostDevice.title'),
        }} />
        <Entry entry={t('guide.waiting.internet')} />
        <Entry entry={t('guide.waiting.deviceNotRecognized')} />
        <Entry entry={t('guide.waiting.useWithoutDevice')} />
      </Guide>
    </div>
  );
};
