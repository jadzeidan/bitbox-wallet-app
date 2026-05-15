// SPDX-License-Identifier: Apache-2.0

use crate::app::AppState;
use anyhow::{Context, Result};
use axum::body::{to_bytes, Body};
use axum::http::{header, HeaderMap, HeaderName, Method, Uri};
use axum::response::Response;

pub async fn proxy_http_request(
    state: &AppState,
    method: Method,
    headers: HeaderMap,
    uri: Uri,
    body: Body,
) -> Result<Response> {
    let mut target = format!("http://127.0.0.1:{}{}", state.sidecar_port, uri.path());
    if let Some(query) = uri.query() {
        target.push('?');
        target.push_str(query);
    }

    let body_bytes = to_bytes(body, usize::MAX)
        .await
        .context("failed to read request body")?;

    let mut request = state.proxy_client.request(method, target);

    for (name, value) in headers.iter() {
        if should_skip_request_header(name) {
            continue;
        }
        request = request.header(name, value);
    }

    let upstream = request
        .body(body_bytes)
        .send()
        .await
        .context("sidecar request failed")?;

    let status = upstream.status();
    let mut response = Response::builder().status(status);

    for (name, value) in upstream.headers() {
        if should_skip_response_header(name) {
            continue;
        }
        response = response.header(name, value);
    }

    let payload = upstream
        .bytes()
        .await
        .context("failed to read upstream response body")?;

    response
        .body(Body::from(payload))
        .context("failed to build response")
}

fn should_skip_request_header(name: &HeaderName) -> bool {
    matches!(
        *name,
        header::HOST
            | header::CONNECTION
            | header::TRANSFER_ENCODING
            | header::CONTENT_LENGTH
            | header::TE
            | header::TRAILER
            | header::UPGRADE
            | header::PROXY_AUTHENTICATE
            | header::PROXY_AUTHORIZATION
    )
}

fn should_skip_response_header(name: &HeaderName) -> bool {
    matches!(
        *name,
        header::CONNECTION
            | header::TRANSFER_ENCODING
            | header::CONTENT_LENGTH
            | header::TE
            | header::TRAILER
            | header::UPGRADE
            | header::PROXY_AUTHENTICATE
            | header::PROXY_AUTHORIZATION
    )
}
