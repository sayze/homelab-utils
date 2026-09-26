# CLAUDE.md

Go library of reusable components (servers, loggers, etc.) for homelab services. Module: `github.com/sayze/homelab-utils`, Go 1.24.

## Commands

```sh
go build ./...
go test ./...
go vet ./...
golangci-lint run ./...   # v2.13.2, config in .golangci.yml; CI runs this and go test
```

## Conventions

- One component per top-level directory (e.g. `server/`, `logger/`), each with a package doc comment.
- Keep external dependencies minimal; prefer the standard library.
- Tests live next to the code in `*_test.go`.
- When adding a package, add a row to the Packages table in `README.md`.
- Keep docs short.
