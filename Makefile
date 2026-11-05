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
