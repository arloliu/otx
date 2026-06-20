# otx Agent Configuration

Authoritative entrypoint for coding agents in this repository. Claude Code imports this
file from `CLAUDE.md`; other agents read `AGENTS.md` directly.

otx (`github.com/arloliu/otx`, "OpenTelemetry eXtensions") is a config-driven wrapper over
the official OpenTelemetry SDK for Go services: tracing, logging, metrics, W3C context
propagation, span ergonomics, and HTTP/gRPC/NATS instrumentation. Design:
`docs/design/2026-06-12-zapwire-integration-design.md`. Detailed rules live under
`.agents/rules/`.

## Detailed Rules

Read `.agents/rules/AGENTS.md` first — it maps task triggers to rule files. Always follow
`.agents/rules/000-agent-contract.md`.

## Dependency policy (load-bearing)

- Single Go module (`github.com/arloliu/otx`). otx is heavy by design: OTel SDK +
  exporters, grpc, protobuf, contrib instrumentation, NATS, and `zapwire/otlp`. There is no
  stdlib-only constraint.
- **Dependency direction is permanent: otx MAY depend on zapwire; zapwire must NEVER depend
  on otx** (design doc, boundary analysis).
- Check `go.mod` and ask before adding any new dependency.

## Validation gate (before every commit)

1. `go fix ./<changed-pkg>/...` (touched packages only).
2. `make lint` — fix all issues. Never invoke golangci-lint directly or edit `.golangci.yaml`.
3. `make test` must pass. It runs `go test -race ./...` over the root module **and** the nested
   integration module (`nats/internal/integration`, embedded NATS server), so `-race` and the
   integration suite are always exercised. `make lint-integration` lints the nested module
   (`make lint` does not descend into it).
Never add `Co-Authored-By` or any attribution trailer to commits.
