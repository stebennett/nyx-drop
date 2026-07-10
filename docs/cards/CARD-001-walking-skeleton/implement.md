# CARD-001 — implement.md

## What changed
- Fixed `normalizeHost` (internal/server/routing.go) to be genuinely idempotent and
  total: extracted port-stripping into `stripPort`, a single-pass helper that is
  provably idempotent on its own output, and routed bracket-extracted content
  through the same helper instead of returning it raw. This closes the
  reproducible bug the previous implementer's `FuzzNormalizeHost` run surfaced
  (`normalizeHost("[:0]") = ":0"`, `normalizeHost(":0") = ""`).
- `internal/metrics/metrics.go`: `New(reg prometheus.Registerer) *Metrics` registers
  the Go collector, the process collector, and `http_request_duration_seconds{class}`
  on a caller-supplied registry (no global/promauto registry). Pulled in
  `github.com/prometheus/client_golang` via `go mod tidy`.
- `internal/server/pages.go`: `renderNotFound(cfg) ([]byte, error)` parses
  `web/notfound.html` with `html/template` and injects `cfg.ExternalOrigin()` as
  `{{.AppURL}}`.
- `internal/server/middleware.go`: `responseRecorder` (status/byte capture, default
  200, `Unwrap()` for future `Flusher`/`Hijacker`), `requestLog(log)` (one JSON line
  per request: host/path/method/status/dur_ms/bytes, using `time.Now`/`time.Since`
  — not the injected Clock, per the latency-vs-business-time gotcha already in
  KNOWLEDGE.md), `instrument(m, cfg)` (observes `HTTPDuration` by `routeClass`).
- `internal/server/server.go`: `Deps` struct, `New(Deps) (http.Handler, error)`
  (renders the 404 page once), `top` (matches `/healthz`/`/metrics` before host
  routing and middleware), `rootHost` (apex → empty `apexMux`; site-host or unknown
  → branded 404), `health` (200 "ok" / 503 from the injected `HealthFunc`),
  `serveNotFound`. `newTestServer(t, logWriter)` helper for the httptest
  integration tests.
- `cmd/drop/main.go`: `run(getenv func(string) string) error` wires
  config→logger→clock→registry/metrics→server→`http.Server`
  (`ReadHeaderTimeout: 10s`), serves until SIGINT/SIGTERM, then
  `Shutdown(ctx)` with a 10s grace period. `main` calls `run`, prints errors to
  stderr, exits 1. Verified locally: `go run ./cmd/drop` + `curl /healthz` → 200
  "ok"; `kill -TERM` → logs `"shutting down"` and exits cleanly.
- `Dockerfile`: two-stage build exactly per `05-deployment.md` (`golang:1.24`
  builder, `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`,
  `gcr.io/distroless/static:nonroot`, `USER 65532:65532`, `EXPOSE 8080`). Verified
  `docker build .`, `docker run` + `curl /healthz` → 200, `docker inspect` confirms
  `User=65532:65532`.
- `.github/workflows/ci.yml`: `test` job (setup-go 1.24, `go vet`, `gofmt -l`
  emptiness check, `go test -race ./...`) and `docker` job (`docker build`, no
  push; commented registry-push step). Validated with `actionlint` (clean) and a
  Python YAML parse (clean).
- `README.md`: what-it-is, config-variable table (matches `internal/config`'s
  validation exactly), local-dev block (verified live against
  `*.localtest.me`), docker build/run instructions, repo layout.
- `.gitignore`: anchored the built-binary entry to `/drop` — the previous bare
  `drop` pattern from task 1's scaffold was also matching `cmd/drop/`, hiding that
  package from git entirely.

## Deviations from design
- None in scope or interfaces. The `normalizeHost` implementation differs
  structurally from what the dying agent had written (factored through a shared
  `stripPort` helper) but the exported signature and documented behavior
  (`normalizeHost(hostHeader string) string`, lowercase + strip `:port`, total,
  idempotent) are unchanged and match the design's Interfaces section exactly.
- Task 10's server.go/server_test.go were written together rather than strict
  red-then-green-per-file (server.go first, then the integration tests, rather
  than the reverse); correctness was verified by running the full test suite
  afterward and manually tracing that each assertion depends on the real
  implementation (e.g. the apex-vs-site-host-vs-unknown branching), not a
  tautology. All other tasks (6-9, 11-14) followed strict red→green.

## Commits
(Resumed work; tasks 1-5 were already committed as `9201697`, `deef7c0`,
`4d48411`, `f5c806a`, `8aa4ae8` before this dispatch.)
- `f186d34` feat(server): add host-routing helpers (normalizeHost, siteLabel, routeClass) — task 6, includes the idempotency fix
- `124ba10` feat(metrics): add Prometheus registry wiring with HTTP duration histogram — task 7
- `d22ec66` feat(server): render the branded not-found page with the app URL injected — task 8
- `e37aa81` feat(server): add requestLog and instrument middleware — task 9
- `08f4321` feat(server): wire New, top-level routing, health, and 404 rendering — task 10
- `e396ac2` feat(cmd/drop): wire main entrypoint with graceful shutdown (+ .gitignore fix) — task 11
- `4cee368` feat(docker): add two-stage Dockerfile (distroless, non-root) — task 12
- `3f91a9a` ci: add GitHub Actions workflow (vet/fmt/test/build) — task 13
- `19700da` docs: add README (what-it-is, config reference, local dev, docker build) — task 14

## Rework
This dispatch was a resume-from-death, not a rework dispatch, but task 6 itself
was effectively a red→green cycle against a pre-existing failing test
(`FuzzNormalizeHost/b625175f565a8a8e`, seeded corpus case `"[:0]"`):
- **Finding:** `normalizeHost` violated its documented idempotency invariant on
  bracket-derived content that looks like `"host:port"`.
- **Fix:** factored port-stripping into `stripPort`, applied uniformly to both the
  bare-host and bracket-content cases, so bracket-extracted content is always
  passed back through the same idempotent rule before being returned.
- **Verification:** seed corpus green, then two live fuzzing runs (30s finding a
  second counterexample `"[0:0]"` and auto-saving it to the corpus; 45s and 60s
  follow-up runs finding nothing further).

## Gates (verified independently by the orchestrator)
- `go vet ./...` — clean
- `gofmt -l .` — empty
- `go test -race ./...` — 95 passed, 0 failed, 6 packages
- `docker build .` — succeeds
- `go test -fuzz=FuzzNormalizeHost -fuzztime=25s` — no new counterexamples;
  corpus now 4 files (3 original seeds + the `"[0:0]"` regression case)
