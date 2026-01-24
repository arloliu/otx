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

.PHONY: update-pkg-cache
update-pkg-cache:
	@printf "Updating Go package cache...\n"
	@GOPROXY=$(shell go env GOPROXY | cut -d',' -f1) && \
	MODULE=$(shell go mod edit -json | grep -o '"ModulePath": "[^"]*"' | cut -d'"' -f4) && \
	VERSION=$(shell git describe --tags --abbrev=0 2>/dev/null || echo "latest") && \
	echo "Updating $$MODULE@$$VERSION on $$GOPROXY" && \
	curl -s "$$GOPROXY/$$MODULE/@v/$$VERSION.info" > /dev/null && \
	echo "Package cache updated successfully."