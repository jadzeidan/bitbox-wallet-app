// SPDX-License-Identifier: Apache-2.0

use std::collections::HashSet;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EndpointMode {
    Native,
    Proxy,
}

#[derive(Clone, Debug, Default)]
pub struct EndpointModeRegistry {
    native_endpoints: HashSet<String>,
}

impl EndpointModeRegistry {
    pub fn new() -> Self {
        Self {
            native_endpoints: HashSet::new(),
        }
    }

    pub fn with_native_endpoints<I, S>(native: I) -> Self
    where
        I: IntoIterator<Item = S>,
        S: AsRef<str>,
    {
        let native_endpoints = native
            .into_iter()
            .map(|value| normalize_endpoint(value.as_ref()))
            .filter(|value| !value.is_empty())
            .collect();
        Self { native_endpoints }
    }

    pub fn from_csv(csv: &str) -> Self {
        Self::with_native_endpoints(csv.split(',').map(str::trim))
    }

    pub fn mode_for_path(&self, path: &str) -> EndpointMode {
        let endpoint = normalize_endpoint(path);
        if self.native_endpoints.contains(&endpoint) {
            EndpointMode::Native
        } else {
            EndpointMode::Proxy
        }
    }

    pub fn native_endpoints(&self) -> impl Iterator<Item = &str> {
        self.native_endpoints.iter().map(String::as_str)
    }
}

pub fn normalize_endpoint(path: &str) -> String {
    let trimmed = path.trim();
    let no_prefix = trimmed
        .strip_prefix("/api/")
        .or_else(|| trimmed.strip_prefix("api/"))
        .or_else(|| trimmed.strip_prefix("/api"))
        .unwrap_or(trimmed);
    no_prefix.trim_start_matches('/').to_string()
}

pub const KNOWN_FRONTEND_ENDPOINTS: &[&str] = &[
    "account-add",
    "accounts",
    "accounts/balance-summary",
    "accounts/eth-account-code",
    "accounts/reinitialize",
    "aopp",
    "aopp/approve",
    "aopp/cancel",
    "aopp/choose-account",
    "authenticate",
    "bitsurance/lookup",
    "bitsurance/url",
    "bluetooth/connect",
    "bluetooth/state",
    "cancel-connect-keystore",
    "certs/download",
    "chart-data",
    "coins/btc/set-unit",
    "coins/convert-from-fiat",
    "coins/convert-to-plain-fiat",
    "config",
    "config/default",
    "connect-keystore",
    "detect-dark-theme",
    "dev-servers",
    "devices/registered",
    "electrum/check",
    "events",
    "export-log",
    "force-auth",
    "keystore/{rootFingerprint}/features",
    "keystores",
    "market/pocket/verify-address",
    "market/region-codes",
    "native-locale",
    "notes/export",
    "notes/import",
    "notify-user",
    "on-auth-setting-changed",
    "online",
    "open",
    "rename-account",
    "set-account-active",
    "set-account-receive-script-type",
    "set-dark-theme",
    "set-token-active",
    "set-watchonly",
    "socksproxy/check",
    "supported-coins",
    "swap/accounts",
    "swap/quote",
    "swap/sign",
    "swap/status",
    "test/deregister",
    "test/register",
    "testing",
    "update",
    "using-mobile-data",
    "version",
    // Dynamic account/device/event subjects used by frontend subscriptions or direct calls.
    "account/{code}/...",
    "devices/bitbox02/{id}/...",
    "devices/bitbox02-bootloader/{id}/...",
];

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalizes_api_paths() {
        assert_eq!(normalize_endpoint("/api/version"), "version");
        assert_eq!(normalize_endpoint("api/version"), "version");
        assert_eq!(normalize_endpoint("/api"), "");
    }

    #[test]
    fn default_mode_is_proxy() {
        let registry = EndpointModeRegistry::new();
        assert_eq!(registry.mode_for_path("/api/version"), EndpointMode::Proxy);
    }

    #[test]
    fn csv_native_endpoints_are_honored() {
        let registry = EndpointModeRegistry::from_csv("version, testing");
        assert_eq!(registry.mode_for_path("/api/version"), EndpointMode::Native);
        assert_eq!(registry.mode_for_path("/api/testing"), EndpointMode::Native);
        assert_eq!(registry.mode_for_path("/api/update"), EndpointMode::Proxy);
    }
}
