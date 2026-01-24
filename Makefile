LINT_TIMEOUT	:= 1m
LINTER_GOMOD	:= -modfile=linter.go.mod

.PHONY: test
test:
	@go test ./...

.PHONY: lint clean-linter-cache update-linter
lint:
	@printf "Run linter...\n"
	@go tool $(LINTER_GOMOD) golangci-lint run --timeout ${LINT_TIMEOUT}

clean-linter-cache:
	@go tool $(LINTER_GOMOD) golangci-lint cache clean


update-linter:
	@printf "Install/update linter tool...\n"
	@go get -tool $(LINTER_GOMOD) github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.0
	@go mod verify $(LINTER_GOMOD)