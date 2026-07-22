import { createReadStream, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import checker from 'vite-plugin-checker';
import eslint from 'vite-plugin-eslint';
import tsconfigPaths from 'vite-tsconfig-paths';
import { configDefaults } from 'vitest/config';

// Cross-origin isolation headers let the wavelength WASM runtime run in a
// worker with OPFS persistence (crossOriginIsolated === true). They are
// opt-in (BITBOX_COI=1) because COEP blocks the cross-origin vendor iframes
// (buy/sell widgets). Without them the runtime falls back to main-thread
// mode, matching the packaged apps.
const crossOriginIsolationHeaders = {
  'Cross-Origin-Opener-Policy': 'same-origin',
  'Cross-Origin-Embedder-Policy': 'require-corp',
  'Cross-Origin-Resource-Policy': 'same-origin',
};

// Vite's static file server treats *.gz as a pre-compressed asset and serves
// it with Content-Encoding: gzip, making the browser transparently decompress
// it. The wavelength runtime expects the raw gzip bytes of its wasm daemon
// (it decompresses itself via DecompressionStream), so serve that file as an
// opaque binary instead — like the packaged apps' scheme handlers do.
const wavelengthRuntimeGzPlugin = () => {
  const webRoot = dirname(fileURLToPath(import.meta.url));
  const gzPath = '/wavewalletdk/wavewalletdk.wasm.gz';
  const serve = (req, res, next) => {
    if (req.url?.split('?')[0] !== gzPath) {
      return next();
    }
    const file = join(webRoot, 'public', gzPath);
    try {
      res.writeHead(200, {
        'Content-Type': 'application/gzip',
        'Content-Length': statSync(file).size,
      });
      createReadStream(file).pipe(res);
    } catch {
      res.statusCode = 404;
      res.end();
    }
  };
  return {
    name: 'wavelength-runtime-gz',
    // No return value: a value returned from configureServer is treated as a
    // post hook by vite.
    configureServer(server) {
      server.middlewares.use(serve);
    },
    configurePreviewServer(server) {
      server.middlewares.use(serve);
    },
  };
};

export default defineConfig((env) => {
  const envVars = loadEnv(env.mode, process.cwd(), '')
  const host = envVars.BITBOX_DEV_BIND_HOST || '127.0.0.1'
  const port = envVars.VITE_PORT
  return {
    // Relative base path so the js/css files are referenced with `./index-...js` instead of
    // `/index-...js`. This makes it easier to find these files in iOS.
    base: './',
    build: {
      modulePreload: false,
      outDir: 'build',
      target: ['chrome122'],
    },
    plugins: [
      react(),
      checker({
        typescript: true,
      }),
      tsconfigPaths(),
      env.mode !== 'test' && eslint(),
      wavelengthRuntimeGzPlugin(),
    ],
    test: {
      css: false,
      environment: 'jsdom',
      globals: true,
      pool: 'forks',
      setupFiles: './vite.setup-tests.mjs',
      exclude: [
        ...configDefaults.exclude,
        'tests/**'
        ],
    },
    // The wavelength worker is an ES module, so the worker bundle must not be
    // built as an iife (the default when code-splitting is enabled).
    worker: {
      format: 'es',
    },
    server: {
      host,
      port: typeof port !== 'undefined' ? port : 8080,
      strictPort: true,
      ...(envVars.BITBOX_COI === '1' ? { headers: crossOriginIsolationHeaders } : {}),
    },
    preview: {
      ...(envVars.BITBOX_COI === '1' ? { headers: crossOriginIsolationHeaders } : {}),
    }
  };
});
