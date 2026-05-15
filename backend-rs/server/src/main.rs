// SPDX-License-Identifier: Apache-2.0

mod app;
mod native;
mod proxy;
mod ws;

use crate::app::{build_router, AppState};
use anyhow::{bail, Context, Result};
use axum::serve;
use bbapp_backend_rs_compat::endpoints::EndpointModeRegistry;
use bbapp_backend_rs_go_sidecar::{spawn_sidecar, SidecarConfig, SidecarHandle};
use clap::Parser;
use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::time::Duration;
use tokio::net::TcpListener;
use tracing::{info, warn};
use tracing_subscriber::{fmt, EnvFilter};

#[derive(Parser, Debug)]
#[command(name = "servewallet-rs")]
struct Cli {
    #[arg(long, default_value_t = false)]
    mainnet: bool,
    #[arg(long, default_value_t = false)]
    regtest: bool,
    #[arg(long, default_value_t = true)]
    devservers: bool,
    #[arg(long = "gapLimitReceive", default_value_t = 0)]
    gap_limit_receive: u16,
    #[arg(long = "gapLimitChange", default_value_t = 0)]
    gap_limit_change: u16,
    #[arg(long, default_value_t = false)]
    simulator: bool,
    #[arg(long = "simulatorPort", default_value_t = 15423)]
    simulator_port: u16,
    #[arg(long = "aoppUrl", default_value = "")]
    aopp_url: String,
    #[arg(long, default_value_t = 8082)]
    port: u16,

    // Internal migration knobs
    #[arg(long, default_value_t = 18082)]
    sidecar_port: u16,
    #[arg(long, default_value_t = false)]
    no_sidecar: bool,
    #[arg(long, default_value = "")]
    native_endpoints: String,
}

#[tokio::main]
async fn main() -> Result<()> {
    init_logging();
    let cli = Cli::parse();

    if cli.mainnet && cli.regtest {
        bail!("Cannot use --mainnet and --regtest together.");
    }

    let repo_root = discover_repo_root()?;
    let version = std::fs::read_to_string(repo_root.join("APP_VERSION"))
        .context("failed to read APP_VERSION")?
        .trim()
        .to_owned();

    let native_registry = if !cli.native_endpoints.trim().is_empty() {
        EndpointModeRegistry::from_csv(&cli.native_endpoints)
    } else if let Ok(from_env) = std::env::var("BBAPP_RS_NATIVE_ENDPOINTS") {
        EndpointModeRegistry::from_csv(&from_env)
    } else {
        EndpointModeRegistry::new()
    };

    let mut sidecar_handle: Option<SidecarHandle> = None;
    if !cli.no_sidecar {
        let mut sidecar_config =
            SidecarConfig::default_with_repo_root(repo_root.clone(), cli.sidecar_port);
        sidecar_config.mainnet = cli.mainnet;
        sidecar_config.regtest = cli.regtest;
        sidecar_config.devservers = cli.devservers;
        sidecar_config.gap_limit_receive = cli.gap_limit_receive;
        sidecar_config.gap_limit_change = cli.gap_limit_change;
        sidecar_config.simulator = cli.simulator;
        sidecar_config.simulator_port = cli.simulator_port;
        sidecar_config.aopp_url = if cli.aopp_url.is_empty() {
            None
        } else {
            Some(cli.aopp_url.clone())
        };

        let handle = spawn_sidecar(&sidecar_config)?;
        handle.wait_ready()?;
        sidecar_handle = Some(handle);
    } else {
        warn!("Go sidecar disabled; proxy endpoints will return 503.");
    }

    let proxy_timeout = std::env::var("BBAPP_RS_PROXY_TIMEOUT_MS")
        .ok()
        .and_then(|value| value.parse::<u64>().ok())
        .unwrap_or(30_000);

    let state = AppState::new(
        cli.sidecar_port,
        !cli.no_sidecar,
        native_registry,
        native::NativeContext {
            version,
            testing: !cli.mainnet,
            devservers: cli.devservers,
            online: true,
            using_mobile_data: false,
        },
        Duration::from_millis(proxy_timeout),
    )?;

    for endpoint in state.registry.native_endpoints() {
        info!(endpoint, "native endpoint enabled");
    }

    let app = build_router(state);
    let addr = SocketAddr::from(([0, 0, 0, 0], cli.port));
    let listener = TcpListener::bind(addr)
        .await
        .with_context(|| format!("failed to bind {addr}"))?;

    info!("servewallet-rs listening on http://localhost:{}", cli.port);
    info!("sidecar listening on http://localhost:{}", cli.sidecar_port);

    let result = serve(listener, app).await;

    if let Some(mut handle) = sidecar_handle {
        handle.shutdown();
    }

    result.context("server error")
}

fn init_logging() {
    let env_filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    let _ = fmt().with_env_filter(env_filter).try_init();
}

fn discover_repo_root() -> Result<PathBuf> {
    let cwd = std::env::current_dir().context("failed to determine current directory")?;
    if has_repo_markers(&cwd) {
        return Ok(cwd);
    }
    if let Some(parent) = cwd.parent() {
        let parent = parent.to_path_buf();
        if has_repo_markers(&parent) {
            return Ok(parent);
        }
    }
    bail!(
        "could not detect repository root from {}; expected APP_VERSION and cmd/servewallet/main.go",
        cwd.display()
    );
}

fn has_repo_markers(path: &Path) -> bool {
    path.join("APP_VERSION").is_file() && path.join("cmd/servewallet/main.go").is_file()
}
