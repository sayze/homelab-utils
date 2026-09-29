# homelab-utils

Reusable Go components for homelab services: HTTP servers, loggers and other shared building blocks.

## Installation

```sh
go get github.com/sayze/homelab-utils
```

Requires Go 1.24 or later.

## Packages

| Package | Description |
| ------- | ----------- |
| [`logger`](logger) | Process-wide JSON logger built on `log/slog`. Call `logger.Init("my-service")` once in `main`, then `logger.Info/Warn/Error`. |
| [`router`](router) | chi router with request IDs, JSON request logging, panic recovery and `GET /health`. Call `router.New()` (or `router.New(router.WithLogger(l))`), add routes, use it as the `http.Server` handler. |
| [`server`](server) | HTTP server with graceful shutdown, `OnStart`/`OnStop` hooks and JSON lifecycle logs. Call `server.New(handler, opts...).RunWithSignals()` to serve until SIGINT/SIGTERM, or `.Run(ctx)` to serve until `ctx` is cancelled (optionally with `server.WithLogger(l)`). |

## Development

```sh
go test ./...
```
