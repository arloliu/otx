---
trigger: always_on
---

# 100 - Project Overview & Prime Directives

## Identity
- **Project:** Fuda (Go Configuration Library)
- **Module:** `github.com/arloliu/otx`
- **Language:** Go >=1.25.0
- **Linting:** `golangci-lint` (via `make lint`)

## Prime Directives
1.  **Plan First:** Create/Update `implementation_plan.md` before writing code. Wait for approval on architectural changes.
2.  **Small Diffs:** Break work into small, verifiable chunks. Do not rewrite files unnecessarily.
3.  **Dependencies:** Check `go.mod`. Prefer stdlib. Ask before adding new deps.
