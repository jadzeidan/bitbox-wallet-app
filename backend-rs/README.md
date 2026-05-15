# backend-rs

Rust backend migration workspace for BitBoxApp.

Phase 1 goal:
- `servewallet-rs` owns port `8082`.
- Existing Go backend runs as a sidecar on an internal port.
- Rust proxies `/api/*` and `/api/events` by default.
- Endpoint mode registry allows flipping individual routes from `proxy` to `native`.

## Run

From repo root:

```sh
make servewallet-rs
```

Useful internal knobs:
- `BBAPP_RS_NATIVE_ENDPOINTS`: comma-separated endpoint list (e.g. `version,testing`).
- `BBAPP_RS_PROXY_TIMEOUT_MS`: proxy timeout for HTTP forwarding (default `30000`).

Dev/CI default preset:
- `make servewallet-rs` and CI default to:
  `version,testing,dev-servers,online,using-mobile-data`
- Override by exporting `BBAPP_RS_NATIVE_ENDPOINTS`.

Initial native endpoints currently implemented:
- `version`
- `testing`
- `dev-servers`
- `online`
- `using-mobile-data`

## Crates

- `server`: HTTP/WS surface + endpoint routing.
- `compat`: API contract helpers and endpoint mode registry.
- `go_sidecar`: starts and supervises `cmd/servewallet` as migration sidecar.
- `core/*`: placeholders for progressively ported native domains.
