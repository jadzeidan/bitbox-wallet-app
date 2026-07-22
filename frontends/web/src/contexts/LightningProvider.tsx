// SPDX-License-Identifier: Apache-2.0

import { ReactNode, useEffect, useRef } from 'react';
import { LightningContext } from './LightningContext';
import { getLightningAccount, getLightningReady, subscribeLightningAccount, subscribeLightningReady } from '../api/lightning';
import { bootLightning, stopLightning } from '../api/wavelength/engine';
import { useSync } from '../hooks/api';

type TProps = {
  children: ReactNode;
};

export const LightningProvider = ({ children }: TProps) => {
  const lightningAccount = useSync(getLightningAccount, subscribeLightningAccount);
  const isLightningReady = useSync(getLightningReady, subscribeLightningReady);
  const hasBooted = useRef(false);

  // Drive the wavelength engine from the backend's account state: boot the
  // wallet while an account exists, stop and wipe it when the account goes
  // away (deactivation).
  useEffect(() => {
    if (lightningAccount) {
      hasBooted.current = true;
      bootLightning().catch(error => console.error('lightning: boot failed', error));
    } else if (lightningAccount === null && hasBooted.current) {
      hasBooted.current = false;
      stopLightning({ wipe: true }).catch(error => console.error('lightning: stop failed', error));
    }
  }, [lightningAccount]);

  return (
    <LightningContext.Provider
      value={{
        isLightningReady,
        lightningAccount
      }}>
      {children}
    </LightningContext.Provider>
  );
};
