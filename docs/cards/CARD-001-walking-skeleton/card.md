---
id: CARD-001
type: task
layer: infra
title: "Walking skeleton: config, healthz/metrics, host routing, Dockerfile, CI"
status: design
phase: design
right_sized: true
depends_on: []
branch: task/001-walking-skeleton-design
worktree: /Users/stevebennett/Code/nyx-drop-worktrees/CARD-001
design_pr_url: ""
pr_url: ""
adrs: []
reworks: 0
started: 2026-07-09
delivered: ""
created: 2026-07-09
---

## Why
An operator can run the binary (locally and as a container) and probe it. Establishes
the repo scaffold, config parsing/validation, clock injection, slog setup, metrics
plumbing, host-normalizing root handler, request-log middleware, Dockerfile, and CI —
the plumbing every later card builds on. Mirrors slice S1 in
`docs/implementation/06-vertical-slices.md`.

## Acceptance criteria
- [ ] `go run` then `GET /healthz` → 200 with any `Host` header (spec "Static serving — `/healthz` bypasses Host routing"; DB ping stubbed OK until CARD-002)
- [ ] Missing or invalid env (unparseable `TTL`/sizes, bad `SCHEME`, malformed `BASE_DOMAIN`) → non-zero exit naming the variable (spec "Configuration")
- [ ] `GET /metrics` serves the Prometheus registry host-independently; one JSON `slog` line per request with host, path, status, duration, bytes (spec "Observability")
- [ ] Unknown host → branded 404 page; `docker build` succeeds (distroless, non-root); CI workflow (vet/fmt/test/build) green (spec "Architecture overview", "Deployment")

## Notes
Scope detail: `internal/config` (all variables), `internal/clock` (injected `Clock`,
never `time.Now()` in handlers), `internal/metrics` (registry + HTTP histogram only),
host normalization (lowercase, strip port). See 06-vertical-slices.md §S1 and
01-architecture.md.
