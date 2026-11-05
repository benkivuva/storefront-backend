# storefront-backend

Go OLTP backend for the Retail Platform storefront (Postgres, Redis Streams,
transactional outbox).


## Layout rule

Dependencies point inward: `adapter -> service -> domain`.

- `internal/domain` imports the **standard library only**.
- `internal/service` never imports `internal/adapter`.

Both rules are enforced twice: by `depguard` in `.golangci.yml`, and by
`test/arch` (runs under `go test ./...`, no linter required).

## Commands

    make test   # go test -race -cover ./...
    make vet
    make lint   # golangci-lint
    make fmt    # gofmt -w

Requires Go 1.26+ (see `go.mod`).
