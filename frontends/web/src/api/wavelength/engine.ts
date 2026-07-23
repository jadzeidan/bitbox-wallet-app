// SPDX-License-Identifier: Apache-2.0

// Orchestrates the wavelength wallet engine (WASM) that runs the lightning
// wallet inside the frontend. The backend only provisions the credentials
// (BitBox-derived mnemonic and wallet password) and consumes the wallet state
// snapshots pushed to it, from which it serves balance and payment list.

import {
  createWalletEngine,
  createWebClient,
  createWebWalletEngine,
  defaultConfig,
  type Entry,
  type WalletEngine,
  type WalletSnapshot,
} from '@lightninglabs/wavelength-web';
import { getLightningCredentials, postLightningState, type TLightningCredentials, type TWalletPayment, type TWalletState } from '@/api/lightning';
import { TSdkError } from '@/api/lightning-errors';

// Where the wavelength runtime assets (wasm daemon and supporting files) are
// served, relative to the app; see scripts/fetch-wavelength-runtime.sh. The
// SDK requires an absolute base URL, so resolve it against the page URL,
// which also works for the custom schemes of the packaged apps.
const runtimeBaseUrl = (): string => new URL('wavewalletdk/', document.baseURI).href;

// Debounce interval for pushing wallet state snapshots to the backend, and the
// longer delay used to retry a failed push.
const statePushDelayMs = 300;
const statePushRetryDelayMs = 2000;

// The engine only updates its snapshot on wallet events, so fields that change
// server-side only (serverConnected, blockHeight) go stale once the wallet is
// running; refresh periodically to keep them current.
const refreshIntervalMs = 30000;

let engine: WalletEngine | null = null;
let unsubscribeEngine: (() => void) | null = null;
let bootPromise: Promise<void> | null = null;
let refreshTimer: ReturnType<typeof setInterval> | null = null;
let pushTimer: ReturnType<typeof setTimeout> | null = null;
// Serializes state pushes so at most one POST is in flight and pushes cannot
// land out of order on the backend.
let pushChain: Promise<void> = Promise.resolve();
let lastPushedState: string | null = null;
// Serializes boot/stop transitions: a stop queued during a boot runs after it,
// so a boot can never resurrect an engine that a stop is tearing down, and a
// wipe can never run concurrently with a boot writing to OPFS.
let transitionChain: Promise<void> = Promise.resolve();

const serializeTransition = <T>(fn: () => Promise<T>): Promise<T> => {
  const run = transitionChain.then(fn);
  transitionChain = run.then(() => undefined, () => undefined);
  return run;
};

const createEngine = (): WalletEngine => {
  // The worker transport requires cross-origin isolation. Without it, run the
  // runtime on the main thread; SQLite still persists via the opfs-sahpool
  // VFS, and should that ever be unavailable, the wallet is simply restored
  // from the backend's credentials on the next boot.
  if (self.crossOriginIsolated) {
    return createWebWalletEngine({ runtimeBaseUrl: runtimeBaseUrl(), autoStart: false });
  }
  return createWalletEngine({
    client: createWebClient({ runtimeThread: 'main', runtimeBaseUrl: runtimeBaseUrl() }),
    autoStart: false,
  });
};

const toUnixSeconds = (date: string): number => {
  const ms = Date.parse(date);
  return Number.isFinite(ms) ? Math.floor(ms / 1000) : 0;
};

const toWalletPayment = (entry: Entry): TWalletPayment => {
  return {
    id: entry.id,
    kind: entry.kind,
    status: entry.status,
    amountSat: entry.amountSat,
    feeSat: entry.feeSat,
    createdAt: toUnixSeconds(entry.createdAt),
    note: entry.note || undefined,
    invoice: entry.request?.lightningInvoice || undefined,
    paymentHash: entry.progress?.paymentHash || undefined,
    txid: entry.progress?.txid || undefined,
    failureReason: entry.failureReason || undefined,
  };
};

const toWalletState = (snapshot: WalletSnapshot): TWalletState => {
  return {
    ready: snapshot.phase === 'ready' || snapshot.phase === 'restoring',
    serverConnected: snapshot.info?.serverConnected ?? false,
    blockHeight: snapshot.info?.blockHeight ?? 0,
    balanceSat: snapshot.balance?.confirmedSat ?? 0,
    pendingInSat: snapshot.balance?.pendingInSat ?? 0,
    pendingOutSat: snapshot.balance?.pendingOutSat ?? 0,
    payments: snapshot.activity.map(toWalletPayment),
  };
};

const pushState = async (): Promise<void> => {
  if (!engine) {
    return;
  }
  const state = toWalletState(engine.getSnapshot());
  const serialized = JSON.stringify(state);
  if (serialized === lastPushedState) {
    return;
  }
  try {
    await postLightningState(state);
    lastPushedState = serialized;
  } catch (error) {
    console.error('lightning: pushing the wallet state failed', error);
    // Re-schedule so the backend eventually converges on the latest state
    // even when this was the last snapshot change (e.g. a payment settling).
    schedulePushState(statePushRetryDelayMs);
  }
};

const schedulePushState = (delayMs: number = statePushDelayMs): void => {
  if (pushTimer !== null) {
    clearTimeout(pushTimer);
  }
  pushTimer = setTimeout(() => {
    pushTimer = null;
    pushChain = pushChain.then(pushState);
  }, delayMs);
};

// Removes all locally persisted wallet data. Called on deactivation, after the
// backend has deleted the account credentials.
const wipeLocalData = async (): Promise<void> => {
  // The wallet database (worker transport) lives in OPFS. Collect the entry
  // names first: deleting while iterating can skip entries.
  try {
    if (navigator.storage?.getDirectory) {
      const root: any = await navigator.storage.getDirectory();
      const names: string[] = [];
      for await (const name of root.keys()) {
        names.push(name);
      }
      for (const name of names) {
        await root.removeEntry(name, { recursive: true });
      }
    }
  } catch (error) {
    console.error('lightning: wiping OPFS failed', error);
  }
  // The main-thread transport keeps the encrypted seed in localStorage.
  try {
    Object.keys(localStorage)
      .filter(key => {
        const lowerKey = key.toLowerCase();
        return lowerKey.startsWith('wavelength') || lowerKey.startsWith('wavewalletdk');
      })
      .forEach(key => localStorage.removeItem(key));
  } catch (error) {
    console.error('lightning: wiping localStorage failed', error);
  }
};

// Stops, disposes and forgets the engine. Safe to call when no engine runs.
const teardownEngine = async (): Promise<void> => {
  if (refreshTimer !== null) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
  if (pushTimer !== null) {
    clearTimeout(pushTimer);
    pushTimer = null;
  }
  lastPushedState = null;
  if (!engine) {
    return;
  }
  const stoppedEngine = engine;
  engine = null;
  unsubscribeEngine?.();
  unsubscribeEngine = null;
  try {
    await stoppedEngine.stop();
  } catch (error) {
    console.error('lightning: stopping the engine failed', error);
  }
  stoppedEngine.dispose();
};

const startEngine = async (credentials: TLightningCredentials): Promise<WalletEngine> => {
  if (!engine) {
    engine = createEngine();
    unsubscribeEngine = engine.subscribe(() => schedulePushState());
  }
  const runningEngine = engine;
  let phase = runningEngine.getSnapshot().phase;
  if (phase === 'error') {
    // Recover from a previously failed start: the daemon may still be running
    // and rejects a second start, so stop it first.
    try {
      await runningEngine.stop();
    } catch {
      // Tolerated; the start below reports the actual problem.
    }
    phase = runningEngine.getSnapshot().phase;
  }
  if (phase === 'loading' || phase === 'runtimeReady' || phase === 'stopped') {
    // The dataDir namespaces the persisted wallet per account, so leftover
    // data of a different account (e.g. another BitBox) can never collide.
    await runningEngine.start(defaultConfig(credentials.network, { dataDir: credentials.code }));
  }
  return runningEngine;
};

const createWalletFromCredentials = async (runningEngine: WalletEngine, credentials: TLightningCredentials): Promise<void> => {
  // Import the deterministic wallet derived from the BitBox; recoverState
  // scans the server for existing funds and history.
  await runningEngine.createWallet({
    password: credentials.password,
    mnemonic: credentials.mnemonic.split(' '),
    recoverState: true,
  });
};

const boot = async (): Promise<void> => {
  const credentials = await getLightningCredentials();
  let runningEngine = await startEngine(credentials);
  switch (runningEngine.getSnapshot().phase) {
  case 'needsWallet':
    await createWalletFromCredentials(runningEngine, credentials);
    break;
  case 'locked':
    try {
      await runningEngine.unlockWallet({ password: credentials.password });
    } catch (error) {
      // A locked wallet these credentials cannot unlock is stale local data.
      // The backend credentials are authoritative: wipe and rebuild.
      console.error('lightning: unlocking failed, rebuilding the wallet', error);
      await teardownEngine();
      await wipeLocalData();
      runningEngine = await startEngine(credentials);
      await createWalletFromCredentials(runningEngine, credentials);
    }
    break;
  default:
    break;
  }
  if (refreshTimer === null) {
    refreshTimer = setInterval(() => {
      const phase = engine?.getSnapshot().phase;
      if (phase !== 'ready' && phase !== 'restoring' && phase !== 'syncing') {
        return;
      }
      engine?.refresh().catch(error => {
        console.error('lightning: refreshing the wallet state failed', error);
      });
    }, refreshIntervalMs);
  }
  schedulePushState();
};

/**
 * Boots the lightning wallet engine; called when the backend reports a
 * lightning account. Safe to call repeatedly: a running or in-flight boot is
 * reused, a failed boot is retried on the next call.
 */
export const bootLightning = (): Promise<void> => {
  if (!bootPromise) {
    bootPromise = serializeTransition(async () => {
      try {
        await boot();
      } catch (error) {
        bootPromise = null;
        // The retry must start from a clean slate: a second start() on an
        // already-running daemon is rejected by the runtime.
        await teardownEngine();
        throw error;
      }
    });
  }
  return bootPromise;
};

/**
 * Stops and disposes the lightning wallet engine; called when the lightning
 * account goes away. With `wipe`, all locally persisted wallet data is removed
 * as well (deactivation). Queued after an in-flight boot, which therefore
 * cannot resurrect the engine afterwards.
 */
export const stopLightning = ({ wipe }: { wipe: boolean }): Promise<void> => {
  bootPromise = null;
  return serializeTransition(async () => {
    await teardownEngine();
    if (wipe) {
      await wipeLocalData();
    }
  });
};

/**
 * Returns the running wallet engine; throws when the lightning wallet has not
 * been booted (no lightning account, or boot still in progress).
 */
export const requireEngine = (): WalletEngine => {
  if (!engine) {
    throw new TSdkError('The lightning wallet is not running');
  }
  return engine;
};

// Waits (bounded) until the wallet reports a usable phase. When the wait times
// out, the call proceeds anyway so the daemon's own error surfaces instead of
// a generic one.
const usableWalletTimeoutMs = 60000;
const waitForUsableWallet = (runningEngine: WalletEngine): Promise<void> => {
  const isUsable = (): boolean => {
    const phase = runningEngine.getSnapshot().phase;
    return phase === 'ready' || phase === 'restoring' || phase === 'error';
  };
  if (isUsable()) {
    return Promise.resolve();
  }
  return new Promise<void>(resolve => {
    const finish = (): void => {
      clearTimeout(timeout);
      unsubscribe();
      resolve();
    };
    const timeout = setTimeout(finish, usableWalletTimeoutMs);
    const unsubscribe = runningEngine.subscribe(() => {
      if (isUsable()) {
        finish();
      }
    });
  });
};

/**
 * Returns the wallet engine for performing wallet actions, waiting for an
 * in-flight boot and for the wallet to become usable. When the engine is down
 * (e.g. a previous boot failed), a new boot is attempted, so user actions
 * retry instead of failing outright. Throws when the wallet cannot be booted
 * (e.g. no lightning account).
 */
export const awaitEngine = async (): Promise<WalletEngine> => {
  if (!engine || bootPromise) {
    await bootLightning().catch(() => {
      // requireEngine below reports the failure; boot errors are logged by
      // the boot itself.
    });
  }
  const runningEngine = requireEngine();
  await waitForUsableWallet(runningEngine);
  return runningEngine;
};

// Dev-only console helpers for manual testing in webdev, e.g.
// `await wavelengthDebug.deposit()` to get a signet deposit (boarding)
// address that can be funded from a signet faucet, or
// `wavelengthDebug.snapshot()` to inspect the engine state.
if (import.meta.env.DEV) {
  (self as any).wavelengthDebug = {
    deposit: () => requireEngine().deposit(),
    snapshot: () => requireEngine().getSnapshot(),
    // Full engine access for manual debugging, e.g.
    // `await wavelengthDebug.engine().list({ view: 'onchain' })`.
    engine: () => requireEngine(),
  };
}
