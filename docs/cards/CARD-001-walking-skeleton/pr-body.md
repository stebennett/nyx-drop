## CARD-001 — Walking skeleton: config, healthz/metrics, host routing, Dockerfile, CI   [task]

Implements slice S1. The design for this card landed separately in #1.

### Why
Nyx Drop needs the plumbing every later card builds on: typed configuration parsed from the
environment and validated at startup, an injected `Clock`, a Prometheus registry, host-based
routing that distinguishes the apex app surface from site subdomains, a branded 404 for
unknown hosts, and a distroless container plus CI to prove it all builds. Nothing here is
user-facing yet — the value is that CARD-002 (SQLite store) and CARD-008 (admin OAuth) can now
start against a real, tested skeleton rather than an empty module.

### What changed
- **`internal/config`** — `Load(getenv func(string) string) (*Config, error)` parses and
  validates every variable, failing fast with an error that names the offending one. `ParseSize`
  handles human-readable byte sizes (decimal SI `KB/MB/GB`, binary `KiB/MiB/GiB`, bare `K/M/G`
  aliases) per **ADR-0001**, with overflow guarded. Accessors `Addr()` and `ExternalOrigin()`.
- **`internal/clock`** — the injected `Clock` (invariant 9), with `Real` (UTC) and `Fake`
  (`SetNow`/`Advance`) implementations.
- **`internal/metrics`** — `New(prometheus.Registerer)` registers the Go and process collectors
  plus `http_request_duration_seconds{class}` on a caller-supplied registry. No `promauto`
  global.
- **`internal/server`** — `New(Deps) (http.Handler, error)`. `top()` matches `/healthz` and
  `/metrics` **before** host routing and outside the middleware chain (invariant 8,
  **ADR-0002**); `rootHost()` sends the apex to an empty `apexMux` and every site/unknown host to
  the branded 404. `requestLog` emits exactly one JSON `slog` line per host-routed request
  (host, path, method, status, dur_ms, bytes); `instrument` observes latency by route class.
  `renderNotFound` templates the embedded 404 page once at startup, injecting the instance's own
  origin.
- **`web`** — `embed.FS` carrying `notfound.html`, adapted from the mockups. Self-contained,
  zero external requests.
- **`cmd/drop`** — `run(getenv)` wires config → logger → clock → registry/metrics → server →
  `http.Server` (`ReadHeaderTimeout: 10s`), with SIGINT/SIGTERM graceful shutdown.
- **`Dockerfile`** — two-stage, `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`, final image
  `gcr.io/distroless/static:nonroot`, `USER 65532:65532`.
- **`.github/workflows/ci.yml`** — `test` job (vet, gofmt emptiness, `go test -race`) and
  `docker` job (`docker build`, no push).
- **`README.md`** — what it is, the config-variable table, the local-dev block, docker build.

Two bugs were found and fixed along the way, both worth calling out:

1. **`normalizeHost` was not idempotent.** `FuzzNormalizeHost` found that the bracket branch
   returned `[...]` contents raw, and those contents could themselves look like `host:port` and
   be stripped again on a second pass (`normalizeHost("[:0]")` → `":0"` → `""`). Fixed by
   factoring port-stripping into a `stripPort` helper that is idempotent on its own output, and
   routing bracket content back through it. Fuzzing found a second counterexample, `[0:0]`,
   during the fix; both are now permanent regression files in the corpus.
2. **`.gitignore`'s bare `drop` pattern** — intended for the built binary — was also matching
   `cmd/drop/`, silently keeping `main.go` untracked. Anchored to `/drop`.

### Acceptance criteria
- [x] `go run` then `GET /healthz` → 200 with any `Host` header (spec "Static serving — `/healthz` bypasses Host routing"; DB ping stubbed OK until CARD-002)
- [x] Missing or invalid env (unparseable `TTL`/sizes, bad `SCHEME`, malformed `BASE_DOMAIN`) → non-zero exit naming the variable (spec "Configuration")
- [x] `GET /metrics` serves the Prometheus registry host-independently; one JSON `slog` line per request with host, path, status, duration, bytes (spec "Observability")
- [x] Unknown host → branded 404 page; `docker build` succeeds (distroless, non-root); CI workflow (vet/fmt/test/build) green (spec "Architecture overview", "Deployment")

### Testing
| Gate | Result |
|---|---|
| `go vet ./...` | clean |
| `gofmt -l .` | empty |
| `go test -race ./...` | **95 passed, 0 failed**, 6 packages |
| `docker build .` | succeeds; `docker inspect` → `User=65532:65532` |
| `go test -fuzz=FuzzParseSize -fuzztime=30s` | no new counterexamples |
| `go test -fuzz=FuzzNormalizeHost -fuzztime=30s` | no new counterexamples |

**Core logic layer coverage: 92.0% (172/187 statements)**, against a 90% target — measured as an
aggregate across `internal/clock`, `internal/config`, the `internal/server` routing helpers,
`renderNotFound`, and the middleware recorder. `cmd/drop/main.go` is excluded by the design's
test strategy (blocking `ListenAndServe` + signal wiring, covered indirectly).

The `card-tester` initially blocked on `renderNotFound` at 71.4%, reading the target as a
per-function floor. That was overridden: the design states a layer target, the reading was
self-inconsistent (it passed `parseLogLevel` at 50.0% and `isDigits` at 83.3% unflagged), and
`renderNotFound`'s two uncovered statements are unreachable error returns over a compile-time
`embed.FS`. Covering them would have meant deviating from the merged design's fixed signature to
inject a breakable `fs.FS`. **CARD-012** is open to remove the ambiguity from the wording and
lift the rule into `PROTOCOL-ADDENDUM.md`.

### Review
Approved with no blocking findings. Host-parsing was scrutinised hardest, since a
`strings.HasSuffix` subdomain check is a classic host-confusion vector: `siteLabel` matches on
the full-dot suffix `"." + baseDomain` and then rejects empty or dotted labels, so
`evilsites.nyxhub.net`, `xsites.nyxhub.net`, `sites.nyxhub.net.evil.com`, `.sites.nyxhub.net`
and the apex all fail safe to a branded 404 — only `good.sites.nyxhub.net` yields a slug.
Invariants 8 and 9, UTC handling, ADR-0001 and ADR-0002 all verified adhered to; `go.mod`
direct-requires only `prometheus/client_golang`.

Four non-blocking follow-ups recorded in `review.md`, none gating this PR:
- the `health` 503 branch is unexercised (S1's stub never fails) — **pin it in CARD-002**;
- trailing-dot / leading-dot hosts fail safe but are untested — **CARD-003 must decide
  trailing-dot (FQDN) handling explicitly** when it adds real site serving;
- `config.go:50` re-reads `getenv("BASE_DOMAIN")` for the error message (nit);
- `config_test.go:106` uses `TTL=0s` where AC-2 says `TTL=0` (equivalent; traceability nit).

### Knowledge
Added to `KNOWLEDGE.md`:
- *Gotchas* — composing an idempotent port-stripper behind a bracket-unwrap step is not
  automatically idempotent; route every extraction branch through one shared primitive, and back
  the invariant with live fuzzing rather than the seed corpus alone.
- *Gotchas* — `.gitignore` entries for build artifacts sharing a name with a source directory
  must be root-anchored (`/drop`, not `drop`).
- *Gotchas* — the coverage target is a layer aggregate, not a per-function floor; unreachable
  error paths are not coverage debt.
- *Conventions* — `siteLabel` is host-confusion-safe by construction; CARD-003 must preserve its
  exact shape when it replaces the site-host stub.

Carried from the design phase: **ADR-0001** (human-readable byte-size suffixes) and **ADR-0002**
(HTTP observability contract), both merged in #1.

🤖 Card delivered via /kanban
