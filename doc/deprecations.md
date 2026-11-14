# Deprecations

This document tracks deprecated features and APIs that are still shipped but scheduled for removal. Maintainers should review this list before each release to decide whether any items are due for removal.

## Active

### Observability endpoints on `webServer`

- **Deprecated since:** v0.72.0 (when `metricsServer` was added).
- **Removal target:** v0.80.0 or later. v0.71.x is the last line without `metricsServer` and is supported until v0.80.0 is released, so v0.79.x is the last release that must keep these endpoints.
- **Replacement:** `metricsServer`. Point health probes at its `/healthz`, scrapes at its `/metrics` (frps exposes business metrics there without `enablePrometheus`), and use `metricsServer.pprofEnable` for profiling.
- **Deprecated items:** `/healthz` on `webServer` in both frps and frpc; `/metrics` on the frps `webServer` and the top-level `enablePrometheus` that gates it; the pprof handlers on `webServer` and `webServer.pprofEnable` in both frps and frpc.
- **Code references:**
  - `server/api_router.go` — `/healthz`, and `/metrics` when `enablePrometheus` is set.
  - `client/api_router.go` — `/healthz`.
  - `pkg/util/http/server.go` — pprof handlers registered when `webServer.pprofEnable` is set.
  - `server/service.go` — startup deprecation warnings and `metricsCollectors`.
  - `client/service.go` — startup deprecation warnings.
  - `pkg/config/v1/server.go` — `EnablePrometheus`; `pkg/config/v1/common.go` — `WebServerConfig.PprofEnable`.
- **Notes:** Removal drops the startup warnings and the deprecation notes in `README.md` and the two `conf/*_full_example.toml` files together. `webServer` then serves only the admin API.

## Removed

_None yet._
