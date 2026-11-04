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
