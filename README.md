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

## Development

```sh
go test ./...
```
