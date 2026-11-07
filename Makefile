.RECIPEPREFIX = >
.PHONY: test vet lint tidy fmt

test:
> go test -race -cover ./...

vet:
> go vet ./...

lint:
> golangci-lint run ./...

tidy:
> go mod tidy

fmt:
> gofmt -l -w .

up:
> docker compose up -d --wait

down:
> docker compose down

logs:
> docker compose logs -f --tail=100

MIGRATION_DATABASE_URL ?= postgres://storefront:storefront@localhost:5432/storefront?sslmode=disable

migrate-up:
> MIGRATION_DATABASE_URL=$(MIGRATION_DATABASE_URL) go run ./cmd/migrate up

migrate-down:
> MIGRATION_DATABASE_URL=$(MIGRATION_DATABASE_URL) go run ./cmd/migrate down

migrate-version:
> MIGRATION_DATABASE_URL=$(MIGRATION_DATABASE_URL) go run ./cmd/migrate version

test-int:
> go test -race -v ./test/integration/...

.PHONY: genkey
genkey:
> @if [ -f $(HOME)/.storefront-dev-key ]; then \
> echo "$(HOME)/.storefront-dev-key exists — not overwriting."; \
> echo "To regenerate: go run ./cmd/genkey -force -f $(HOME)/.storefront-dev-key"; \
> else \
> go run ./cmd/genkey -f $(HOME)/.storefront-dev-key; \
> fi
> @echo ""
> @echo "To use: source $(HOME)/.storefront-dev-key"
> @echo "Or add to .env: $$(cat $(HOME)/.storefront-dev-key)"
