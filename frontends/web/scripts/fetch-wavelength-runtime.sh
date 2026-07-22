#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Downloads the wavelength wasm runtime assets (wavewalletdk.wasm and its
# supporting files) that the @lightninglabs/wavelength-web SDK loads at
# runtime. The assets are version-locked to the installed SDK release
# (RUNTIME_MANIFEST_VERSION in @lightninglabs/wavelength-core) and are
# unpacked flat into public/wavewalletdk/, from where they are served at
# `wavewalletdk/` relative to the app.
#
# Idempotent: a .version marker is written next to the assets and the
# download is skipped when it matches the installed SDK. When the download
# fails (e.g. offline) the script warns instead of failing so builds without
# network access still work as long as the assets are already present.

set -eu

WEB_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The manifest version lives in wavelength-core; wavelength-web's
# runtime-manifest.js only lists the asset file names.
VERSION_FILE="${WEB_ROOT}/node_modules/@lightninglabs/wavelength-core/dist/version.js"
TARGET_DIR="${WEB_ROOT}/public/wavewalletdk"
MARKER_FILE="${TARGET_DIR}/.version"

if [ ! -f "${VERSION_FILE}" ]; then
    echo "fetch-wavelength-runtime: ${VERSION_FILE} not found - run npm install first." >&2
    exit 1
fi

# version.js declares: export const RUNTIME_MANIFEST_VERSION = 'v0.1.0';
VERSION="$(sed -n "s/^export const RUNTIME_MANIFEST_VERSION = '\(.*\)';$/\1/p" "${VERSION_FILE}")"
if [ -z "${VERSION}" ]; then
    echo "fetch-wavelength-runtime: could not read RUNTIME_MANIFEST_VERSION from ${VERSION_FILE}." >&2
    exit 1
fi

if [ -f "${MARKER_FILE}" ] && [ "$(cat "${MARKER_FILE}")" = "${VERSION}" ]; then
    echo "fetch-wavelength-runtime: assets for ${VERSION} already present."
    exit 0
fi

# Release assets on GitHub are mutable, so pin the tarball hash per version;
# bumping the SDK requires deliberately updating this pin.
case "${VERSION}" in
    v0.1.0) EXPECTED_SHA256="1fc57d2fdbeae25de2b60e5ad1fe9322b9da8f7270fc2b450dc621dc8428c398" ;;
    *)
        echo "fetch-wavelength-runtime: no pinned sha256 for ${VERSION}; update this script." >&2
        exit 1
        ;;
esac

URL="https://github.com/lightninglabs/wavelength/releases/download/${VERSION}/Wavewalletdk.wasm.tar.gz"
TMP_TARBALL="$(mktemp -t wavewalletdk.wasm.XXXXXX)"
trap 'rm -f "${TMP_TARBALL}"' EXIT

echo "fetch-wavelength-runtime: downloading ${URL}"
if ! curl --fail --silent --show-error --location --output "${TMP_TARBALL}" "${URL}"; then
    if [ -f "${MARKER_FILE}" ]; then
        echo "fetch-wavelength-runtime: WARNING: download failed (offline?); keeping existing assets ($(cat "${MARKER_FILE}"))." >&2
    else
        echo "fetch-wavelength-runtime: WARNING: download failed (offline?); lightning will not work without the runtime assets in ${TARGET_DIR}." >&2
    fi
    exit 0
fi

# Unlike a failed download, a checksum mismatch is a hard error: a tampered
# release must never reach the app bundle.
if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_SHA256="$(sha256sum "${TMP_TARBALL}" | cut -d' ' -f1)"
else
    ACTUAL_SHA256="$(shasum -a 256 "${TMP_TARBALL}" | cut -d' ' -f1)"
fi
if [ "${ACTUAL_SHA256}" != "${EXPECTED_SHA256}" ]; then
    echo "fetch-wavelength-runtime: sha256 mismatch for ${URL} (got ${ACTUAL_SHA256}); refusing to unpack." >&2
    exit 1
fi

# Start from a clean directory so assets dropped by an SDK upgrade do not
# linger in the app bundle. Runs only after a successful, verified download.
rm -rf "${TARGET_DIR}"
mkdir -p "${TARGET_DIR}"
# Unpack flat: the SDK resolves all assets directly against runtimeBaseUrl.
tar -xzf "${TMP_TARBALL}" -C "${TARGET_DIR}"
find "${TARGET_DIR}" -mindepth 2 -type f -exec mv {} "${TARGET_DIR}/" \;
find "${TARGET_DIR}" -mindepth 1 -type d -empty -delete
# The runtime prefers wavewalletdk.wasm.gz whenever DecompressionStream exists
# (all shipped webview engines have it), so drop the redundant ~128MB raw wasm
# instead of bundling it into every installer.
rm -f "${TARGET_DIR}/wavewalletdk.wasm"

# Two patches for running without cross-origin isolation (Qt/iOS/Android and
# non-isolated dev), where the sqlite worker can only persist via the
# opfs-sahpool VFS. Remove once fixed upstream.
#
# 1. The daemon requests the 'opfs' VFS, which needs cross-origin isolation;
#    the worker then silently falls back to :memory:, and the daemon's
#    migration backup (VACUUM INTO a file path) fails with SQLITE_CANTOPEN.
#    Normalize an 'opfs' request to 'auto' so the candidate chain picks the
#    persistent opfs-sahpool VFS instead.
# 2. The default sahpool capacity of 6 files is too small for the daemon's
#    databases plus its migration backup. The SDK's worker discards the pool
#    handle, so patch the install call to size the pool and to grow pools
#    created with the old capacity.
awk '{
    if (index($0, "case \"opfs-auto\":") > 0) {
        print
        print "    case \"opfs\":"
    } else if (index($0, "installOpfsSAHPoolVfs({ name: \"opfs-sahpool\" }).catch") > 0) {
        print "    await sqlite3.installOpfsSAHPoolVfs({ name: \"opfs-sahpool\", initialCapacity: 32 }).then(async (poolUtil) => { const target = 32; const capacity = poolUtil.getCapacity(); if (capacity < target) { await poolUtil.addCapacity(target - capacity); } }).catch((error) => {"
    } else {
        print
    }
}' "${TARGET_DIR}/sqlite-worker.js" > "${TARGET_DIR}/sqlite-worker.js.patched"
if ! grep -q "initialCapacity: 32" "${TARGET_DIR}/sqlite-worker.js.patched" \
    || [ "$(grep -c '^    case "opfs":$' "${TARGET_DIR}/sqlite-worker.js.patched")" != "1" ]; then
    echo "fetch-wavelength-runtime: sqlite-worker.js patch did not apply; the SDK assets changed - update this script." >&2
    exit 1
fi
mv "${TARGET_DIR}/sqlite-worker.js.patched" "${TARGET_DIR}/sqlite-worker.js"

printf '%s' "${VERSION}" > "${MARKER_FILE}"
echo "fetch-wavelength-runtime: assets for ${VERSION} unpacked into ${TARGET_DIR}."
