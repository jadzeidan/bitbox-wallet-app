// SPDX-License-Identifier: Apache-2.0

import { ChangeEvent, Dispatch } from 'react';
import { useTranslation } from 'react-i18next';
import { Toggle } from '@/components/toggle/toggle';
import { SettingsItem } from '@/routes/settings/components/settingsItem/settingsItem';
import { TBackendConfig, TConfig } from '@/routes/settings/advanced-settings';
import { setConfig } from '@/utils/config';
import { onAuthSettingChanged, TAuthEventObject, subscribeAuth, forceAuth } from '@/api/backend';
import { runningInAndroid, runningInIOS, runningInQtWebEngine } from '@/utils/env';

type TProps = {
  backendConfig?: TBackendConfig;
  onChangeConfig: Dispatch<TConfig>;
};

export const EnableAuthSetting = ({ backendConfig, onChangeConfig }: TProps) => {
  const { t } = useTranslation();

  const handleToggleAuth = async (e: ChangeEvent<HTMLInputElement>) => {
    const targetEnabled = e.target.checked;
    // Before updating the config we need the user to authenticate.
    // The forceAuth is needed to force the backend to execute the
    // authentication even if the auth config is disabled.
    let handled = false;
    const unsubscribe = subscribeAuth(async (data: TAuthEventObject) => {
      if (handled || data.typ !== 'auth-result') {
        return;
      }
      switch (data.result) {
      case 'authres-ok':
        handled = true;
        unsubscribe();
        await updateConfig(targetEnabled);
        break;
      case 'authres-cancel':
      case 'authres-err':
      case 'authres-missing':
        // If auth was not successful, leave everything as is.
        handled = true;
        unsubscribe();
        break;
      }
    });
    await forceAuth();
  };

  const updateConfig = async (auth: boolean) => {
    const config = await setConfig({
      backend: { authentication: auth },
    }) as TConfig;
    onChangeConfig(config);
    // Do not block the UI update if this hook runs slowly.
    void onAuthSettingChanged();
  };

  const isQtMacOS = runningInQtWebEngine() && navigator.userAgent.toLowerCase().includes('mac');
  if (!runningInAndroid() && !runningInIOS() && !isQtMacOS) {
    return null;
  }

  return (
    <SettingsItem
      settingName={t('newSettings.advancedSettings.authentication.title')}
      secondaryText={t('newSettings.advancedSettings.authentication.description')}
      extraComponent={
        backendConfig !== undefined ? (
          <Toggle
            checked={backendConfig?.authentication || false}
            onChange={handleToggleAuth}
          />
        ) : null
      }
    />
  );
};
