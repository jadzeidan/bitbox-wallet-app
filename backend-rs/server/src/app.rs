// SPDX-License-Identifier: Apache-2.0

use crate::{native, proxy, ws};
use anyhow::Result;
use axum::extract::{OriginalUri, State};
use axum::http::{HeaderMap, Method, StatusCode};
use axum::response::IntoResponse;
use axum::routing::{any, get};
use axum::{body::Body, Router};
use bbapp_backend_rs_compat::endpoints::{EndpointMode, EndpointModeRegistry};
use reqwest::Client;
use std::sync::Arc;
use std::time::Duration;

#[derive(Clone)]
pub struct AppState {
    pub sidecar_port: u16,
    pub sidecar_enabled: bool,
    pub proxy_client: Client,
    pub registry: EndpointModeRegistry,
    pub native: native::NativeContext,
}

impl AppState {
    pub fn new(
        sidecar_port: u16,
        sidecar_enabled: bool,
        registry: EndpointModeRegistry,
        native: native::NativeContext,
        proxy_timeout: Duration,
    ) -> Result<Self> {
        let proxy_client = Client::builder().timeout(proxy_timeout).build()?;
        Ok(Self {
            sidecar_port,
            sidecar_enabled,
            proxy_client,
            registry,
            native,
        })
    }
}

pub fn build_router(state: AppState) -> Router {
    Router::new()
        .route("/api/events", get(ws::events_ws_handler))
        .route("/api/*path", any(api_handler))
        .route("/api", any(api_handler))
        .route("/healthz", get(health_handler))
        .with_state(Arc::new(state))
}

async fn health_handler() -> StatusCode {
    StatusCode::OK
}

async fn api_handler(
    State(state): State<Arc<AppState>>,
    original_uri: OriginalUri,
    method: Method,
    headers: HeaderMap,
    body: Body,
) -> axum::response::Response {
    let endpoint_mode = state.registry.mode_for_path(original_uri.path());

    if endpoint_mode == EndpointMode::Native {
        if let Some(response) =
            native::try_handle_native(&state.native, &method, original_uri.path())
        {
            return response;
        }
    }

    if !state.sidecar_enabled {
        return (StatusCode::SERVICE_UNAVAILABLE, "go sidecar disabled").into_response();
    }

    match proxy::proxy_http_request(&state, method, headers, original_uri.0, body).await {
        Ok(response) => response,
        Err(err) => {
            tracing::error!(error = ?err, "HTTP proxy request failed");
            (StatusCode::BAD_GATEWAY, "upstream sidecar unavailable").into_response()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
    use axum::extract::Path;
    use axum::http::StatusCode;
    use axum::response::Response;
    use futures_util::{SinkExt, StreamExt};
    use std::net::SocketAddr;
    use tokio::net::TcpListener;
    use tokio_tungstenite::connect_async;

    #[tokio::test]
    async fn proxy_contract_parity_for_common_endpoints() {
        let (sidecar_addr, _sidecar_handle) = spawn_sidecar_server(mock_sidecar_router()).await;
        let app_state = AppState::new(
            sidecar_addr.port(),
            true,
            EndpointModeRegistry::new(),
            native_context(),
            Duration::from_secs(5),
        )
        .expect("state creation");
        let (proxy_addr, _proxy_handle) = spawn_proxy_server(build_router(app_state)).await;

        let client = reqwest::Client::new();
        let cases = [
            ("/api/version", StatusCode::OK, "\"sidecar-version\""),
            ("/api/testing", StatusCode::OK, "false"),
            ("/api/dev-servers", StatusCode::OK, "true"),
            (
                "/api/accounts",
                StatusCode::OK,
                r#"[{"code":"account-1","name":"Primary"}]"#,
            ),
            ("/api/keystores", StatusCode::OK, r#"[{"type":"bitbox02"}]"#),
            (
                "/api/config",
                StatusCode::OK,
                r#"{"backend":{"mainFiat":"USD"},"frontend":{}}"#,
            ),
            (
                "/api/update",
                StatusCode::OK,
                r#"{"version":"9.9.9","sha256":"abc"}"#,
            ),
        ];

        for (path, expected_status, expected_body) in cases {
            let url = format!("http://127.0.0.1:{}{path}", proxy_addr.port());
            let response = client.get(url).send().await.expect("request");
            assert_eq!(response.status().as_u16(), expected_status.as_u16());
            let body = response.text().await.expect("response body");
            assert_eq!(body, expected_body);
        }

        let not_found_url = format!("http://127.0.0.1:{}/api/unknown-route", proxy_addr.port());
        let not_found = client.get(not_found_url).send().await.expect("404 request");
        assert_eq!(not_found.status().as_u16(), StatusCode::NOT_FOUND.as_u16());
        assert_eq!(
            not_found.text().await.expect("404 body"),
            r#"{"error":"unknown endpoint"}"#
        );
    }

    #[tokio::test]
    async fn websocket_events_are_relayed() {
        let (sidecar_addr, _sidecar_handle) = spawn_sidecar_server(mock_sidecar_router()).await;
        let app_state = AppState::new(
            sidecar_addr.port(),
            true,
            EndpointModeRegistry::new(),
            native_context(),
            Duration::from_secs(5),
        )
        .expect("state creation");
        let (proxy_addr, _proxy_handle) = spawn_proxy_server(build_router(app_state)).await;

        let ws_url = format!("ws://127.0.0.1:{}/api/events", proxy_addr.port());
        let (mut socket, _response) = connect_async(ws_url).await.expect("connect ws");
        socket
            .send(tokio_tungstenite::tungstenite::Message::Text(
                "Authorization: Basic auth-token".into(),
            ))
            .await
            .expect("send auth");

        let message = socket.next().await.expect("frame").expect("message");
        let text = match message {
            tokio_tungstenite::tungstenite::Message::Text(text) => text.to_string(),
            other => panic!("expected text message, got {other:?}"),
        };
        assert_eq!(
            text,
            r#"{"subject":"accounts","action":"replace","object":[{"code":"account-1"}]}"#
        );
    }

    #[tokio::test]
    async fn first_native_slice_matches_sidecar_contract_values() {
        let (sidecar_addr, _sidecar_handle) = spawn_sidecar_server(mock_sidecar_router()).await;
        let app_state = AppState::new(
            sidecar_addr.port(),
            true,
            EndpointModeRegistry::from_csv("version,testing,dev-servers,online,using-mobile-data"),
            native_context_matching_sidecar(),
            Duration::from_secs(5),
        )
        .expect("state creation");
        let (proxy_addr, _proxy_handle) = spawn_proxy_server(build_router(app_state)).await;

        let client = reqwest::Client::new();
        let native_cases = [
            ("/api/version", "\"sidecar-version\""),
            ("/api/testing", "false"),
            ("/api/dev-servers", "true"),
            ("/api/online", "true"),
            ("/api/using-mobile-data", "false"),
        ];
        for (path, expected) in native_cases {
            let url = format!("http://127.0.0.1:{}{path}", proxy_addr.port());
            let response = client.get(url).send().await.expect("request");
            assert_eq!(response.status().as_u16(), StatusCode::OK.as_u16());
            assert_eq!(response.text().await.expect("response body"), expected);
        }
    }

    fn native_context() -> native::NativeContext {
        native::NativeContext {
            version: "native-version".to_owned(),
            testing: true,
            devservers: true,
            online: true,
            using_mobile_data: false,
        }
    }

    fn native_context_matching_sidecar() -> native::NativeContext {
        native::NativeContext {
            version: "sidecar-version".to_owned(),
            testing: false,
            devservers: true,
            online: true,
            using_mobile_data: false,
        }
    }

    async fn spawn_sidecar_server(app: Router) -> (SocketAddr, tokio::task::JoinHandle<()>) {
        spawn_server(app).await
    }

    async fn spawn_proxy_server(app: Router) -> (SocketAddr, tokio::task::JoinHandle<()>) {
        spawn_server(app).await
    }

    async fn spawn_server(app: Router) -> (SocketAddr, tokio::task::JoinHandle<()>) {
        let listener = TcpListener::bind("127.0.0.1:0")
            .await
            .expect("bind listener");
        let addr = listener.local_addr().expect("local addr");
        let handle = tokio::spawn(async move {
            let result = axum::serve(listener, app).await;
            if let Err(err) = result {
                panic!("server error: {err}");
            }
        });
        (addr, handle)
    }

    fn mock_sidecar_router() -> Router {
        Router::new()
            .route("/api/events", get(mock_events_handler))
            .route("/api/*path", any(mock_api_handler))
            .route("/api", any(mock_root_api_handler))
    }

    async fn mock_root_api_handler() -> Response {
        (
            StatusCode::OK,
            [("content-type", "application/json; charset=utf-8")],
            r#"{"ok":true}"#,
        )
            .into_response()
    }

    async fn mock_api_handler(Path(path): Path<String>) -> Response {
        let (status, payload) = match path.as_str() {
            "version" => (StatusCode::OK, "\"sidecar-version\""),
            "testing" => (StatusCode::OK, "false"),
            "dev-servers" => (StatusCode::OK, "true"),
            "accounts" => (StatusCode::OK, r#"[{"code":"account-1","name":"Primary"}]"#),
            "keystores" => (StatusCode::OK, r#"[{"type":"bitbox02"}]"#),
            "config" => (
                StatusCode::OK,
                r#"{"backend":{"mainFiat":"USD"},"frontend":{}}"#,
            ),
            "update" => (StatusCode::OK, r#"{"version":"9.9.9","sha256":"abc"}"#),
            _ => (StatusCode::NOT_FOUND, r#"{"error":"unknown endpoint"}"#),
        };

        (
            status,
            [("content-type", "application/json; charset=utf-8")],
            payload,
        )
            .into_response()
    }

    async fn mock_events_handler(ws: WebSocketUpgrade) -> impl IntoResponse {
        ws.on_upgrade(mock_events_socket)
    }

    async fn mock_events_socket(mut socket: WebSocket) {
        // Consume optional auth handshake forwarded by the proxy.
        if let Some(Ok(Message::Text(_))) = socket.next().await {}

        let payload =
            r#"{"subject":"accounts","action":"replace","object":[{"code":"account-1"}]}"#;
        let _ = socket.send(Message::Text(payload.into())).await;
    }
}
