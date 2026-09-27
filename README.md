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

## Development

```sh
go test ./...
```
