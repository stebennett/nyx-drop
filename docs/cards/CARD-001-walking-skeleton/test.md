# CARD-001 — test.md

**Phase result: pass.** All gates green. The `card-tester` returned `blocked` on a
coverage reading the orchestrator overrode at the driver's direction — see
§"Coverage gate: tester finding overridden" and `feedback.md`.

## Suite
**Command:** `go test -race ./...`
**Result:** PASS — 95 passed, 0 failed, across 6 packages (`internal/clock`,
`internal/config`, `internal/metrics`, `internal/server`, `cmd/drop`, `web`).

## Lint & build gates
| Gate | Command | Result |
|---|---|---|
| Vet | `go vet ./...` | PASS — clean, no output |
| Format | `gofmt -l .` | PASS — empty |
| Race tests | `go test -race ./...` | PASS — 95/95, 6 packages |
| Image | `docker build .` | PASS — two-stage, `gcr.io/distroless/static:nonroot`, `USER 65532:65532` |

## Coverage
**Command:** `go test -coverprofile=/tmp/cover.out ./...` → `go tool cover -func=/tmp/cover.out`

Whole-module total is **80.0%**, which includes `cmd/drop/main.go` at 0% — explicitly
excluded from the core-coverage figure by the design's Test strategy (blocking
`ListenAndServe` + signal wiring; covered indirectly by the `server`/`config` tests).

### Core logic layer — target ≥ 90% (aggregate)
| File | Covered / statements | % |
|---|---|---|
| `internal/clock/clock.go` | 11 / 11 | 100.0% |
| `internal/config/config.go` | 78 / 87 | 89.7% |
| `internal/config/size.go` | 21 / 23 | 91.3% |
| `internal/server/middleware.go` | 23 / 24 | 95.8% |
| `internal/server/pages.go` | 5 / 7 | 71.4% |
| `internal/server/routing.go` | 34 / 35 | 97.1% |
| **Core logic layer aggregate** | **172 / 187** | **92.0%** ✅ |

Per-function highlights: `normalizeHost`, `stripPort`, `siteLabel`, `routeClass`,
`newResponseRecorder`, `WriteHeader`, `Write`, `Addr`, `ExternalOrigin`, and all of
`internal/clock` at 100.0%. `Load` 92.1%, `ParseSize` 91.3%, `isDigits` 83.3%,
`parseLogLevel` 50.0%, `renderNotFound` 71.4%.

### Coverage gate: tester finding overridden
The `card-tester` returned `blocked`, reading the design's "≥ 90% on the core logic
layer" as a **per-function floor** and failing `renderNotFound` at 71.4%. The
orchestrator put this to the driver, who directed: accept the gate, and open a
follow-up card to remove the ambiguity. Rationale on record:

1. **The design states a layer target, not a per-function one.** As an aggregate the
   layer is at 92.0%, clearing 90%.
2. **The tester's own reading was inconsistent.** It passed `parseLogLevel` (50.0%) and
   `isDigits` (83.3%) without comment — both below 90% — flagging only `renderNotFound`
   because the design names it explicitly. A true per-function rule would fail those too.
3. **The two uncovered statements are unreachable.** `renderNotFound`'s only uncovered
   lines are its two error returns: `template.ParseFS(web.Files, …)` over a compile-time
   `embed.FS` (whose parse success the passing happy-path test already proves), and
   `tmpl.Execute` of a struct with one string field. Neither can fail at runtime.
   Covering them would require changing the merged design's fixed signature
   `renderNotFound(cfg) ([]byte, error)` to take an injectable `fs.FS`, or faking a
   failure — worse code in exchange for a metric.

No rework credit was spent (`reworks` remains 0). Follow-up: **CARD-012**, which amends
this ambiguous wording and lifts the rule into `PROTOCOL-ADDENDUM.md` so future
designers and testers inherit "aggregate across the layer, not per-function".

## Property / fuzz tests
| Target | Command | Result |
|---|---|---|
| `FuzzParseSize` | `go test ./internal/config/ -run=XXX -fuzz=FuzzParseSize -fuzztime=30s` | PASS — no new corpus entries |
| `FuzzNormalizeHost` | `go test ./internal/server/ -run=XXX -fuzz=FuzzNormalizeHost -fuzztime=30s` | PASS — no new corpus entries |

Both invariants hold beyond the seed corpus:
- `FuzzParseSize` — never panics on arbitrary input.
- `FuzzNormalizeHost` — idempotent (`normalizeHost(normalizeHost(x)) == normalizeHost(x)`)
  and never panics. Corpus is 4 files, including the `"[:0]"` and `"[0:0]"` regression
  cases that the implement phase's bug fix closed.

## Acceptance criteria — test backing
Every acceptance criterion in `card.md` has at least one test asserting it:

| Acceptance criterion | Backing test(s) |
|---|---|
| `/healthz` returns 200 under any `Host` | `TestHealthz_AnyHost_200` (4 hosts incl. empty and `10.0.0.5:8080`) |
| `/metrics` serves the registry host-independently | `TestMetrics_AnyHost_Exposition` (asserts `go_goroutines`, `process_*`, `http_request_duration_seconds`) |
| Ops endpoints matched before host routing, bypass middleware | `TestOpsEndpoints_NotLogged` (no log output for `/healthz`, `/metrics`) |
| Bad config fails fast, naming the variable, non-zero exit | `TestLoad_InvalidAndMissing` (17 rows: `BASE_DOMAIN` missing/scheme/port/path/single-label, `SCHEME`, `TTL` unparseable/zero, `MAX_UPLOAD_SIZE` suffix, `MAX_FILE_COUNT` negative, `PORT` range, `LOG_LEVEL`) |
| Exactly one JSON slog line per request with host, path, status, duration, bytes | `TestRequestLog_OneLinePerRequest` |
| Unknown host serves the branded 404 | `TestUnknownHost_Branded404`, `TestMultiLabelHost_404`, `TestSiteHost_404UntilStore` (assert "faded into the night", `text/html`, `X-Content-Type-Options: nosniff`) |
| Apex serves the plain placeholder, not the branded page | `TestApex_404Placeholder` |
| Image is distroless / non-root | `docker build .` + `docker inspect` → `User=65532:65532` |

No acceptance criterion lacks test backing.
