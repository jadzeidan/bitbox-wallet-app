// SPDX-License-Identifier: Apache-2.0

use anyhow::{bail, Context, Result};
use std::fs;
use std::net::{IpAddr, Ipv4Addr, SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

const DEFAULT_WAIT_TIMEOUT: Duration = Duration::from_secs(30);
const CONNECT_RETRY_INTERVAL: Duration = Duration::from_millis(150);

#[derive(Clone, Debug)]
pub struct SidecarConfig {
    pub repo_root: PathBuf,
    pub internal_port: u16,
    pub mainnet: bool,
    pub regtest: bool,
    pub devservers: bool,
    pub gap_limit_receive: u16,
    pub gap_limit_change: u16,
    pub simulator: bool,
    pub simulator_port: u16,
    pub aopp_url: Option<String>,
}

impl SidecarConfig {
    pub fn default_with_repo_root(repo_root: PathBuf, internal_port: u16) -> Self {
        Self {
            repo_root,
            internal_port,
            mainnet: false,
            regtest: false,
            devservers: true,
            gap_limit_receive: 0,
            gap_limit_change: 0,
            simulator: false,
            simulator_port: 15423,
            aopp_url: None,
        }
    }
}

#[derive(Debug)]
pub struct SidecarHandle {
    child: Child,
    pub internal_port: u16,
}

impl SidecarHandle {
    pub fn wait_ready(&self) -> Result<()> {
        wait_for_tcp(self.internal_port, DEFAULT_WAIT_TIMEOUT)
    }

    pub fn shutdown(&mut self) {
        if self.child.try_wait().ok().flatten().is_none() {
            let _ = self.child.kill();
            let _ = self.child.wait();
        }
    }
}

impl Drop for SidecarHandle {
    fn drop(&mut self) {
        self.shutdown();
    }
}

pub fn spawn_sidecar(config: &SidecarConfig) -> Result<SidecarHandle> {
    if config.mainnet && config.regtest {
        bail!("mainnet and regtest cannot both be enabled");
    }

    let app_version = read_app_version(&config.repo_root)?;
    let ldflags = format!(
        "-X github.com/BitBoxSwiss/bitbox-wallet-app/backend/versioninfo.versionString={app_version}"
    );

    let mut cmd = Command::new("go");
    cmd.arg("run")
        .arg("-mod=vendor")
        .arg("-ldflags")
        .arg(ldflags)
        .arg("./cmd/servewallet")
        .arg(format!("-port={}", config.internal_port))
        .current_dir(&config.repo_root)
        .stdin(Stdio::null())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit());

    if config.mainnet {
        cmd.arg("-mainnet");
    }
    if config.regtest {
        cmd.arg("-regtest");
    }
    if !config.devservers {
        cmd.arg("-devservers=false");
    }
    if config.gap_limit_receive > 0 {
        cmd.arg(format!("-gapLimitReceive={}", config.gap_limit_receive));
    }
    if config.gap_limit_change > 0 {
        cmd.arg(format!("-gapLimitChange={}", config.gap_limit_change));
    }
    if config.simulator {
        cmd.arg("-simulator=true");
        cmd.arg(format!("-simulatorPort={}", config.simulator_port));
    }
    if let Some(url) = &config.aopp_url {
        if !url.is_empty() {
            cmd.arg(format!("-aoppUrl={url}"));
        }
    }

    let child = cmd
        .spawn()
        .with_context(|| "failed to spawn Go sidecar (go run ./cmd/servewallet)")?;

    Ok(SidecarHandle {
        child,
        internal_port: config.internal_port,
    })
}

fn read_app_version(repo_root: &Path) -> Result<String> {
    let file = repo_root.join("APP_VERSION");
    let value = fs::read_to_string(&file)
        .with_context(|| format!("failed to read app version from {}", file.display()))?;
    Ok(value.trim().to_owned())
}

fn wait_for_tcp(port: u16, timeout: Duration) -> Result<()> {
    let start = Instant::now();
    let target = SocketAddr::new(IpAddr::V4(Ipv4Addr::LOCALHOST), port);
    loop {
        match TcpStream::connect_timeout(&target, Duration::from_millis(300)) {
            Ok(stream) => {
                drop(stream);
                return Ok(());
            }
            Err(err) => {
                if start.elapsed() >= timeout {
                    return Err(err).with_context(|| {
                        format!("timed out waiting for sidecar on 127.0.0.1:{port}")
                    });
                }
                std::thread::sleep(CONNECT_RETRY_INTERVAL);
            }
        }
    }
}
