// SPDX-License-Identifier: Apache-2.0

use axum::http::{Method, StatusCode};
use axum::response::{IntoResponse, Response};
use bbapp_backend_rs_compat::endpoints::normalize_endpoint;

#[derive(Clone)]
pub struct NativeContext {
    pub version: String,
    pub testing: bool,
    pub devservers: bool,
    pub online: bool,
    pub using_mobile_data: bool,
}

pub fn try_handle_native(ctx: &NativeContext, method: &Method, path: &str) -> Option<Response> {
    if method != Method::GET {
        return None;
    }

    let endpoint = normalize_endpoint(path);
    match endpoint.as_str() {
        "version" => Some(json_value(&ctx.version)),
        "testing" => Some(json_value(&ctx.testing)),
        "dev-servers" => Some(json_value(&ctx.devservers)),
        "online" => Some(json_value(&ctx.online)),
        "using-mobile-data" => Some(json_value(&ctx.using_mobile_data)),
        _ => None,
    }
}

fn json_value<T: serde::Serialize>(value: &T) -> Response {
    match serde_json::to_vec(value) {
        Ok(body) => (
            StatusCode::OK,
            [("content-type", "application/json; charset=utf-8")],
            body,
        )
            .into_response(),
        Err(_) => (StatusCode::INTERNAL_SERVER_ERROR, "serialization error").into_response(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn version_endpoint_returns_json_string() {
        let ctx = NativeContext {
            version: "9.99.0".to_owned(),
            testing: true,
            devservers: true,
            online: true,
            using_mobile_data: false,
        };
        let response = try_handle_native(&ctx, &Method::GET, "/api/version")
            .expect("expected native response");
        assert_eq!(response.status(), StatusCode::OK);
    }

    #[test]
    fn online_endpoint_is_native() {
        let ctx = NativeContext {
            version: "9.99.0".to_owned(),
            testing: true,
            devservers: true,
            online: true,
            using_mobile_data: false,
        };
        let response =
            try_handle_native(&ctx, &Method::GET, "/api/online").expect("expected native response");
        assert_eq!(response.status(), StatusCode::OK);
    }
}
