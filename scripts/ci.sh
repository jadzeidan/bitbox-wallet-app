#!/bin/bash
# SPDX-License-Identifier: Apache-2.0

set -e
set -x

# Set go-langs data race detector options
export GORACE="halt_on_error=1"
export GOTOOLCHAIN="local"
export BBAPP_RS_NATIVE_ENDPOINTS="${BBAPP_RS_NATIVE_ENDPOINTS:-version,testing,dev-servers,online,using-mobile-data}"

APP_VERSION="$(cat APP_VERSION)"
GO_LDFLAGS="-X github.com/BitBoxSwiss/bitbox-wallet-app/backend/versioninfo.versionString=${APP_VERSION}"

# This script has to be called from the project root directory.
go build -trimpath -mod=vendor -ldflags "${GO_LDFLAGS}" ./...
go test -race -mod=vendor -ldflags "${GO_LDFLAGS}" ./... -count=1 -v
golangci-lint --version
golangci-lint config verify
golangci-lint run

cargo --version
cargo fmt --manifest-path backend-rs/Cargo.toml --all -- --check
cargo clippy --manifest-path backend-rs/Cargo.toml --workspace --all-targets -- -D warnings
cargo test --manifest-path backend-rs/Cargo.toml --workspace
make servewallet-rs-smoke

npm --prefix=frontends/web install # needed to install dev dependencies.
make weblint
npm --prefix=frontends/web test -- --no-color --no-watch

./scripts/check-i18n-placeholders.py
