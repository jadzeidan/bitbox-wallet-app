// SPDX-License-Identifier: Apache-2.0

use crate::app::AppState;
use anyhow::Result;
use axum::extract::ws::{Message as AxumMessage, WebSocket, WebSocketUpgrade};
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::IntoResponse;
use futures_util::{SinkExt, StreamExt};
use std::sync::Arc;
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::Message as TungsteniteMessage;

pub async fn events_ws_handler(
    State(state): State<Arc<AppState>>,
    ws: WebSocketUpgrade,
) -> impl IntoResponse {
    if !state.sidecar_enabled {
        return (StatusCode::SERVICE_UNAVAILABLE, "go sidecar disabled").into_response();
    }

    ws.on_upgrade(move |socket| proxy_ws(socket, state))
}

async fn proxy_ws(client_socket: WebSocket, state: Arc<AppState>) {
    if let Err(err) = proxy_ws_inner(client_socket, state).await {
        tracing::error!(error = ?err, "WebSocket proxy failed");
    }
}

async fn proxy_ws_inner(client_socket: WebSocket, state: Arc<AppState>) -> Result<()> {
    let target = format!("ws://127.0.0.1:{}/api/events", state.sidecar_port);
    let (upstream_socket, _) = connect_async(&target).await?;

    let (mut client_tx, mut client_rx) = client_socket.split();
    let (mut upstream_tx, mut upstream_rx) = upstream_socket.split();

    let c2u = tokio::spawn(async move {
        while let Some(next) = client_rx.next().await {
            let msg = match next {
                Ok(msg) => msg,
                Err(err) => return Err(anyhow::anyhow!(err)),
            };
            let upstream_msg = axum_to_tungstenite(msg);
            upstream_tx
                .send(upstream_msg)
                .await
                .map_err(anyhow::Error::from)?;
        }
        Ok::<(), anyhow::Error>(())
    });

    let u2c = tokio::spawn(async move {
        while let Some(next) = upstream_rx.next().await {
            let msg = next?;
            if let Some(client_msg) = tungstenite_to_axum(msg) {
                client_tx
                    .send(client_msg)
                    .await
                    .map_err(anyhow::Error::from)?;
            }
        }
        Ok::<(), anyhow::Error>(())
    });

    tokio::select! {
        result = c2u => {
            result??;
        }
        result = u2c => {
            result??;
        }
    }

    Ok(())
}

fn axum_to_tungstenite(message: AxumMessage) -> TungsteniteMessage {
    match message {
        AxumMessage::Text(text) => TungsteniteMessage::Text(text.to_string()),
        AxumMessage::Binary(bytes) => TungsteniteMessage::Binary(bytes.to_vec()),
        AxumMessage::Ping(bytes) => TungsteniteMessage::Ping(bytes.to_vec()),
        AxumMessage::Pong(bytes) => TungsteniteMessage::Pong(bytes.to_vec()),
        AxumMessage::Close(_) => TungsteniteMessage::Close(None),
    }
}

fn tungstenite_to_axum(message: TungsteniteMessage) -> Option<AxumMessage> {
    match message {
        TungsteniteMessage::Text(text) => Some(AxumMessage::Text(text.to_string())),
        TungsteniteMessage::Binary(bytes) => Some(AxumMessage::Binary(bytes)),
        TungsteniteMessage::Ping(bytes) => Some(AxumMessage::Ping(bytes)),
        TungsteniteMessage::Pong(bytes) => Some(AxumMessage::Pong(bytes)),
        TungsteniteMessage::Close(_) => Some(AxumMessage::Close(None)),
        TungsteniteMessage::Frame(_) => None,
    }
}
