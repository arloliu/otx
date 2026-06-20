LINT_TIMEOUT	:= 1m
LINTER_GOMOD	:= -modfile=linter.go.mod
INTEGRATION_DIR	:= nats/internal/integration

# test runs the full suite under the race detector across both the root module
# and the nested integration module (embedded NATS server). The integration
# module is a separate Go module so the nats-server dependency never enters the
# root go.mod; the root `go test ./...` does not descend into it, so it is run
# explicitly here.
.PHONY: test
test:
	@printf "Run tests with -race (root + integration)...\n"
	@go test -race ./...
	@go test -C $(INTEGRATION_DIR) -race ./...

# test-integration runs only the nested integration module (embedded NATS
# server) under the race detector, for focused iteration on those tests.
.PHONY: test-integration
test-integration:
	@printf "Run integration tests (embedded NATS)...\n"
	@go test -C $(INTEGRATION_DIR) -race ./...

.PHONY: lint clean-linter-cache update-linter lint-integration
lint:
	@printf "Run linter...\n"
	@go tool $(LINTER_GOMOD) golangci-lint run --timeout ${LINT_TIMEOUT}

# lint-integration lints the nested integration module using the same pinned
# linter tool (root linter.go.mod); `make lint` does not descend into it.
lint-integration:
	@printf "Run linter (integration module)...\n"
	@cd $(INTEGRATION_DIR) && go tool -modfile=../../../linter.go.mod golangci-lint run --timeout ${LINT_TIMEOUT}

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
	MODULE=$(shell go list -m) && \
	VERSION=$(shell git describe --tags --abbrev=0 2>/dev/null || echo "latest") && \
	echo "Updating $$MODULE@$$VERSION on $$GOPROXY" && \
	curl -fsS "$$GOPROXY/$$MODULE/@v/$$VERSION.info" > /dev/null && \
	echo "Package cache updated successfully."