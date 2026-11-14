## Features

* frpc can verify server certificates using system CAs, optionally combined with CAs from `trustedCaFile`.
* `frpc reload` now refreshes certificate files for unchanged `https2http` and `https2https` proxies without interrupting existing connections. Failed refreshes keep the previous certificate and report the error in proxy status and logs while reload still succeeds.
* Added `metricsServer` to both frps and frpc, a separate listener for health checks, metrics and optional profiling that does not expose the admin API. It is disabled unless `metricsServer.port` is set, defaults to `127.0.0.1`, and has its own credentials and TLS. Setting the port serves `/metrics` on both: frps exposes runtime and business metrics, so `enablePrometheus` is not needed there, and frpc exposes Go runtime metrics only, without linking an extra metrics library.

  The `/healthz`, `/metrics` and pprof endpoints on `webServer`, `webServer.pprofEnable` and `enablePrometheus` still work but are deprecated: configure `metricsServer` instead. They will continue to work until v0.80.0 is released, and may be removed in v0.80.0 or later.

## Improvements

* frpc supports optional HTTP log reporting via `log.report`, with a bounded queue, per-second rate limiting and token authentication.
* Removed the bundled frps dashboard and frpc admin UI. `webServer` now serves only the admin API, and the `assetsDir` option is gone.
* Removed legacy frpc/frps compatibility: wire protocol v1, legacy INI configuration, the protocol and TLS first-byte compatibility switches, legacy `--dashboard_*` flags, the global `start` allowlist, optional visitor `runID`, and the v2-to-v1 UDP packet codec fallback. The control protocol is always v2.
* Configuration is JSON-only. TOML and YAML parsing, environment-variable templates, and configuration CLI flags were removed. `frpc` and `frps` keep only `-c/--config` and `--version`; the parameter-based `frpc` proxy subcommands were removed.

## Fixes

* Fixed proxy and visitor configuration changes being lost when reloading frpc during reconnection.
* Updated yamux to fix a 100% CPU busy loop after a session shuts down and a session close that could hang when the underlying connection (for example WebSocket) blocks on a write, which could also delay reconnection.
* frps now closes the multiplexed session of a replaced or failed control connection immediately, releasing its streams and work connections instead of keeping them until keepalive times out.
* Fixed a data race when frpc updates log prefixes while other goroutines are logging, which could crash the process.
* Fixed HTTPS and tcpmux route listeners leaving a pending connection open when the listener is removed during connection handoff; the connection is now closed instead of leaving the client to wait for a read timeout.
