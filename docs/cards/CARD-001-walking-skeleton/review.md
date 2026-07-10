# CARD-001 — review.md

Reviewed `git diff main...HEAD` (33 files, +2007). Read-only review; no code changed.
**Verdict: approve — advance to deliver.** No blocking findings.

## Acceptance criteria coverage

| # | Criterion (design.md / card.md) | Proving test(s) | Verdict |
|---|---|---|---|
| 1 | `/healthz` → 200 "ok" on any Host, matched before host routing | `TestHealthz_AnyHost_200` (hosts: `sites.nyxhub.net`, `anything.example`, `10.0.0.5:8080`, empty) — depends on `top()` matching `/healthz` above `rootHost`; a host-first order would 404 the empty/IP hosts | Covered |
| 2 | Bad config fails fast, error names the variable, non-zero exit | `TestLoad_InvalidAndMissing` (17 rows: BASE_DOMAIN missing/scheme/port/path/single-label, SCHEME, TTL unparseable/zero, MAX_UPLOAD_SIZE suffix, MAX_FILE_COUNT negative, PORT range, LOG_LEVEL, + all 5 remaining required-missing) asserts `strings.Contains(err, VAR)`; `main`→`run` returns err, `os.Exit(1)` | Covered |
| 3 | `/metrics` host-independent Prometheus exposition; exactly one JSON slog line per host-routed request with host/path/method/status/dur_ms/bytes; ops endpoints emit none | `TestMetrics_AnyHost_Exposition` (foreign host, asserts `go_goroutines`/`process_`/`http_request_duration_seconds`), `TestRequestLog_OneLinePerRequest` (all six keys + values), `TestOpsEndpoints_NotLogged` (buffer stays empty — fails if ops were inside middleware) | Covered |
| 4 | Unknown host → branded self-contained 404 (`text/html`, nosniff, "faded into the night"); apex → plain placeholder; image distroless/non-root; CI green | `TestUnknownHost_Branded404`, `TestMultiLabelHost_404`, `TestSiteHost_404UntilStore`, `TestApex_404Placeholder` (asserts NOT branded); Dockerfile `gcr.io/distroless/static:nonroot` + `USER 65532:65532`; `.github/workflows/ci.yml` runs vet/gofmt/`test -race`/build | Covered |

Supporting unit coverage confirmed non-tautological: `TestNormalizeHost` + `FuzzNormalizeHost`
(idempotency/totality), `TestSiteLabel` (incl. suffix-not-subdomain row), `TestRouteClass`,
`TestParseSize_Table` + `FuzzParseSize`, `TestFake_AdvanceAndSet` / `TestReal_NowIsUTC`,
`TestResponseRecorder_*`, `TestInstrument_ObservesByClass`, `TestNew_RegistersCollectors`.

The task-10 integration tests were written after `server.go` (a TDD deviation the implementer
self-reported). They were checked specifically for tautology and are sound: each assertion
fails if the routing order or host branching is wrong, rather than restating the
implementation.

## Blocking findings

None.

Deep-dive on the flagged risk areas, all clear:

- **Host confusion (`siteLabel`)** — matches on the full-dot suffix `"." + baseDomain`, then
  rejects an empty label and any label still containing a dot. Traced `evilsites.nyxhub.net`,
  `xsites.nyxhub.net`, `sites.nyxhub.net.evil.com`, `.sites.nyxhub.net`, `..sites.nyxhub.net`
  and the apex itself — every one returns `ok=false` and serves a branded 404. Only
  `good.sites.nyxhub.net` yields a slug. Not foolable. (Independently re-verified by the
  orchestrator with a throwaway table test before approval.)
- **`normalizeHost`/`stripPort` idempotency & totality** — the prior `[:0]`/`[0:0]` bug is
  fixed: bracket content is routed back through the same `stripPort` primitive, whose result
  is colon-free and therefore stable on re-application. `FuzzNormalizeHost` guards it;
  hand-verified for `[::1]:8080`, `[a:b]`, `[0:0]`, `a:b:c`.
- **`ParseSize` / ADR-0001** — decimal `KB/MB/GB = 1000^n`, binary `KiB/MiB/GiB = 1024^n`,
  bare `K/M/G` as decimal aliases; longest-suffix-first ordering prevents `mb`/`m` shadowing;
  overflow guarded before the multiply; `10PB` and `-5MB` rejected. `100MB→1e8`, `500MB→5e8`,
  not swapped at the config assignment.
- **Invariant 9 / UTC** — `time.Now()` appears only in `middleware.go` (latency timing) and
  `clock.go` (the `Real` implementation). No business-time misuse in handlers. `clock.Real.Now()`
  and `Fake` both normalise to UTC.
- **ADR-0002** — `/healthz` and `/metrics` are matched in `top()` outside the
  `requestLog`→`instrument` chain; middleware order matches `01-architecture.md`; route class
  is apex→`app`, else→`site`; the registry is injected, with no `promauto` global.
- **Dependency allowlist** — `go.mod` direct-requires only `prometheus/client_golang`; every
  other entry is one of its transitives. No router, config, or UI library; no ORM.
- **Security** — Dockerfile is `CGO_ENABLED=0`, two-stage, distroless static nonroot, uid
  65532, no secrets baked; `.dockerignore` drops `.git`/`docs`/`*.md` while keeping `README.md`;
  `nosniff` on the 404; no file-serving or path-traversal surface yet.

## Non-blocking findings

1. **`internal/server/server.go:101-108` — the `health` 503 branch is unexercised.** The S1
   `Health` stub always returns nil, so no test injects a failing `HealthFunc`. Not a CARD-001
   acceptance criterion (real readiness is CARD-002) and the branch is trivially correct, but a
   one-line test passing `func(context.Context) error { return errBoom }` and asserting 503
   would cheaply pin the contract CARD-002 depends on. **Follow-up for CARD-002.**
2. **`internal/server/routing.go:75-85` — trailing-dot (FQDN) and leading-dot hosts fail safe
   but are untested.** `sites.nyxhub.net.` is not treated as apex, and `.sites.nyxhub.net`
   yields an empty label; both correctly 404, but `normalizeHost` does not strip a trailing dot,
   so this is implicit behaviour. Safe for S1. **CARD-003 must decide trailing-dot handling
   explicitly** when it adds real site serving. Captured in `KNOWLEDGE.md`.
3. **`internal/config/config.go:50` — `getenv("BASE_DOMAIN")` is called a second time to build
   the error message.** Harmless for a pure map or `os.Getenv` lookup; a captured
   `raw := getenv(...)` would be marginally cleaner. Nit.
4. **`internal/config/config_test.go:106` — AC-2 names `TTL=0`; the test uses `TTL=0s`.**
   Semantically equivalent (both parse to a zero duration, caught by the `<= 0` guard); noted
   only for literal traceability. Nit.

The tester's `renderNotFound` per-function coverage finding is treated as settled per
`feedback.md` / `test.md` (layer aggregate 92.0% clears the 90% target; the two uncovered lines
are unreachable error returns over a compile-time `embed.FS`) and is deliberately not re-raised.
CARD-012 tracks the wording fix.

## Verdict

**Approve — advance to deliver.** A high-quality first slice: pure decidable logic isolated from
the edges, tests that assert behaviour rather than mirror implementation, all four acceptance
criteria genuinely covered, both system invariants and both ADRs honoured, and host-parsing
hardened against confusion and backed by live fuzzing. Four non-blocking follow-ups recorded
above; none gate the PR.
