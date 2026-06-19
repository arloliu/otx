# 500 - Validation and Workflow

Apply before validation, commits, or PR work.

## Validation gates

- Run `make lint` after any Go change and fix every issue (see
  [600-go-after-write.md](600-go-after-write.md) for the `go fix` → lint loop).
  Never invoke golangci-lint directly or edit `.golangci.yaml`.
- Run `make test` (`go test ./...`) before calling implementation work done. For
  concurrency-sensitive changes (tracker, NATS, provider shutdown) also run
  `go test -race ./...` — the default target does not enable `-race`.
- Run `go vet ./...` when not already covered by lint.
- Update godoc and the affected `docs/` guides when exported API changes
  ([400-docs.md](400-docs.md)).

## Code review checklist

- [ ] Correctness — config precedence (YAML vs env), endpoint resolution,
      sampler construction, propagation, and provider `Shutdown` ordering.
- [ ] Traces, logs, and metrics share one resource identity (same
      `BuildResource`).
- [ ] Test coverage for new code ([300-testing.md](300-testing.md)).
- [ ] Docs updated for exported API changes ([400-docs.md](400-docs.md)).
- [ ] Dependency direction respected — otx may depend on zapwire, never the
      reverse ([100-project-map.md](100-project-map.md), root `AGENTS.md`).

## Git conventions

See [550-git-conventions.md](550-git-conventions.md) for branches, commit
format, body wrapping, the plan/review jargon prohibition, and PR conventions.

## Make targets

```bash
make test                # go test ./... (unit tests; no -race by default)
make lint                # golangci-lint via the linter.go.mod toolchain
make clean-linter-cache  # clear golangci-lint's on-disk cache
make update-linter       # install/update the pinned golangci-lint tool
make update-pkg-cache    # warm the Go proxy package cache
```

There is no `make fmt/vet/bench/coverage/ci`. Run those Go commands directly:
`go test -race ./...`, `go vet ./...`, `gofmt -l .`.
