# CARD-001 — Walking skeleton: config, healthz/metrics, host routing, Dockerfile, CI

## Intent
Stand up the project's walking skeleton so an operator can run the binary — locally
and as a distroless container — and probe it. This card creates the repository from
empty: the Go module, the `cmd/` + `internal/` + `web/` layout that every later card
extends, and the plumbing they all consume — configuration parsing/validation, the
injected `Clock`, JSON `slog` setup, the Prometheus registry + HTTP histogram, the
host-normalizing root handler (`/healthz`, `/metrics`, branded not-found page),
request-logging and metrics middleware, the Dockerfile, and the CI workflow. No
product feature ships; the value is a running, observable, packaged process and the
package boundaries every subsequent slice builds on. This is slice **S1** in
`06-vertical-slices.md`.

Because this is the first code in the repo, the scaffolding decisions here (module
path, package boundaries, wiring seams) are load-bearing for CARD-002…011. They are
taken to match `01-architecture.md` exactly and to leave clean extension points.

## Acceptance criteria
Each criterion is observable and maps to a named test.

1. **`/healthz` answers on any Host, matched before host routing.**
   `GET /healthz` → `200` with body `ok`, for `Host` ∈ {`sites.nyxhub.net`,
   `anything.example`, `10.0.0.5:8080`, empty}. In S1 the readiness check is a stub
   that always succeeds (real DB ping arrives in CARD-002).
   — spec *Static serving* ("`/healthz` bypasses Host routing"); `00-overview.md`
   invariant 8. Test: `TestHealthz_AnyHost_200`.
2. **Bad configuration fails fast, naming the variable, with a non-zero exit.**
   `config.Load` returns an error whose message contains the offending variable name
   for: `BASE_DOMAIN` missing; `BASE_DOMAIN=https://x` (contains scheme) /
   `BASE_DOMAIN=x:8080` (port) / `BASE_DOMAIN=x/y` (path); `SCHEME=ftp`; `TTL=banana`
   or `TTL=0`; `MAX_UPLOAD_SIZE=10PB` (unknown suffix); `MAX_FILE_COUNT=-1`;
   `PORT=70000`; `LOG_LEVEL=trace`; and each remaining required var missing
   (`UPLOAD_TOKEN`, `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `ADMIN_GITHUB_USER`,
   `SESSION_SECRET`). `main` prints the error to stderr and exits `1`.
   — spec *Configuration (environment variables)*. Test: `TestLoad_InvalidAndMissing`
   (table-driven, one row per case).
3. **`/metrics` serves the registry host-independently; exactly one JSON log line
   per host-routed request.** `GET /metrics` → `200` in Prometheus exposition format
   on any Host, exposing `go_*`, `process_*`, and `http_request_duration_seconds`.
   Every request that passes through host routing emits one JSON `slog` line with
   keys `host`, `path`, `method`, `status`, `dur_ms`, `bytes`; `/healthz` and
   `/metrics` emit none.
   — spec *Observability*; `00-overview.md` invariant 8. Tests:
   `TestMetrics_AnyHost_Exposition`, `TestRequestLog_OneLinePerRequest`,
   `TestOpsEndpoints_NotLogged`.
4. **Unknown host → branded 404; image builds distroless/non-root; CI green.**
   A request whose Host is not the apex (an unknown host, a valid single-label site
   host with no backing store yet, or a multi-label host `a.b.<BASE_DOMAIN>`) → `404`
   with the branded self-contained not-found HTML (body contains "faded into the
   night", `Content-Type: text/html`). `docker build .` succeeds and the final image
   is `gcr.io/distroless/static:nonroot` running as uid 65532. The CI workflow runs
   `go vet`, `gofmt -l` (fails if non-empty), `go test -race ./...`, and `docker
   build`, all green.
   — spec *Architecture overview* (Host table), *Deployment*. Tests:
   `TestUnknownHost_Branded404`, `TestMultiLabelHost_404`; CI + Docker verified in the
   deliver phase.

## In scope
- `go.mod` (module `nyx-drop`, `go 1.24`), `go.sum`, `.gitignore` touch-up, `.dockerignore`.
- `internal/config` — full parse + validate of **all** environment variables in the
  spec's Configuration table, plus `ParseSize` and config accessors (`Addr`,
  `ExternalOrigin`).
- `internal/clock` — `Clock` interface, `Real`, `Fake` (created now for later cards;
  S1 handlers do not consume it, but `main` injects `clock.Real` into `Deps`).
- `internal/metrics` — `metrics.New(reg)` registering Go + process collectors and the
  `http_request_duration_seconds{class}` histogram. HTTP histogram only (lifecycle
  counters/gauges arrive with the cards that emit them).
- `internal/server` — `New(Deps) (http.Handler, error)`; top-level `/healthz` +
  `/metrics` bypass; host normalization; apex vs site-host vs unknown routing;
  branded 404 render; `requestLog` + `instrument` middleware.
- `web/embed.go` + `web/notfound.html` (adapted from `ui-mockups/notfound.html`,
  app-URL templated).
- `cmd/drop/main.go` — wiring: config → logger → clock → registry/metrics → server →
  `http.Server` with `ReadHeaderTimeout` and SIGINT/SIGTERM graceful shutdown.
- `Dockerfile` (from `05-deployment.md`), `.github/workflows/ci.yml`, `README.md`
  (what-it-is, config reference, local-dev, docker build).

## Out of scope (YAGNI — deferred to the named card)
- Any SQLite store, migrations, real `/healthz` DB ping / data-dir writability check
  → CARD-002 (S2). S1's readiness is a `func(context.Context) error` stub.
- Slug generation, extraction, locks, `POST/PUT /api/sites`, real site static
  serving → CARD-003/004/006.
- Reaper, lifecycle counters/gauges (`sites_active`, `sites_created_total`, …),
  lifecycle log events → CARD-005/010.
- Upload grants, upload page, `/static/*` app assets, apex mux routes → CARD-007.
- OAuth, sessions, cookies, `admin` route class, Origin/CSRF middleware → CARD-008.
- Helm chart and `helm lint`/golden tests in CI → CARD-011 (S11). S1 CI is
  vet/fmt/test/build only.
- `SiteURL(id)` builder → CARD-003 (not needed until sites are served).

## Dependencies & assumptions
- **Depends on:** nothing (`depends_on: []`); first code in the repo.
- **Dependents:** CARD-002 (adds store + real health check via `Deps.Health`),
  CARD-003 (replaces the site-host branch with real serving), CARD-007 (populates the
  apex mux + embed set), CARD-008 (adds the `admin` route class + session/CSRF
  middleware). The seams below exist specifically for them.
- Dependency allowlist (`00-overview.md`): S1 imports only
  `github.com/prometheus/client_golang` from the external set; `modernc.org/sqlite`
  and `golang.org/x/oauth2` enter with their cards. No router/config/UI library.
- `net/http.ServeMux` (Go 1.22+ patterns) and `log/slog` are stdlib. Go toolchain
  `1.24` (matches the Dockerfile builder and CI).
- Assume `BASE_DOMAIN` is a multi-label DNS name (e.g. `sites.nyxhub.net`,
  `localtest.me`); single-label bare hosts (`localhost`) are rejected — dev uses
  `localtest.me` per `05-deployment.md`.

## Approach
Keep all decidable logic in pure functions over plain data, tested without HTTP or
process env; confine `net/http`, `embed`, `os`, Prometheus, and signal handling to
the edges (`server`, `web`, `cmd/drop`). The root handler is a thin router that
delegates to method-seam functions later cards replace.

**Alternatives considered**
- *Read `os.Getenv` directly in `config`.* Rejected: forces env mutation in tests
  (global, race-prone under `-race`). Chosen: `Load(getenv func(string) string)` —
  pure, table-driven, `main` passes `os.Getenv`.
- *A third-party human-size / config library (e.g. `go-humanize`, `envconfig`).*
  Rejected: violates the four-module dependency policy (`00-overview.md`); the parser
  is ~40 lines. Chosen: in-repo `ParseSize` (see ADR).
- *Serve the mockup `notfound.html` as a static byte slice with its hardcoded
  `sites.nyxhub.net` link.* Rejected: wrong for a self-hosted clone at another
  domain. Chosen: render once at startup with `html/template`, injecting
  `cfg.ExternalOrigin()` as the app URL; cache the bytes on the server.
- *Fold `/healthz`+`/metrics` into the logging+metrics middleware (log every
  request literally).* Rejected: kubelet probes every ~10s flood the access log and
  the histogram (whose spec label set is app/site/admin — ops endpoints don't fit).
  Chosen: match ops endpoints above the middleware (see ADR); this also matches
  `01-architecture.md` "Order for apex routes: requestLog → metrics".
- *Positional `server.New(cfg, clk, log, m, reg, health)`.* Rejected: every later
  card would rewrite call sites. Chosen: a `Deps` struct + injected `Health` /
  (later) apex-mux and site-handler seams.

## Interfaces

### `internal/clock`
```go
type Clock interface { Now() time.Time } // always UTC
type Real struct{}
func (Real) Now() time.Time              // time.Now().UTC()
type Fake struct { /* mu sync.Mutex; t time.Time (stored UTC) */ }
func NewFake(t time.Time) *Fake
func (f *Fake) Now() time.Time
func (f *Fake) SetNow(t time.Time)
func (f *Fake) Advance(d time.Duration)
```

### `internal/config`
```go
type Config struct {
    BaseDomain         string        // lowercased, validated DNS name
    Scheme             string        // "http" | "https"
    TTL                time.Duration // > 0
    UploadToken        string
    GitHubClientID     string
    GitHubClientSecret string
    AdminGitHubUser    string        // lowercased
    SessionSecret      string
    DataDir            string        // default "/data"
    MaxUploadSize      int64         // bytes
    MaxSiteSize        int64         // bytes
    MaxFileCount       int           // > 0
    Port               int           // 1..65535
    LogLevel           slog.Level
}

func Load(getenv func(string) string) (*Config, error) // fail-fast; error names the variable
func (c *Config) Addr() string           // ":" + Port
func (c *Config) ExternalOrigin() string // Scheme + "://" + BaseDomain

func ParseSize(s string) (int64, error)  // "100MB"->1e8; decimal KB/MB/GB, binary KiB/MiB/GiB, bare bytes
```
Validation table (defaults from spec *Configuration*):

| Var | Req | Default | Rule on failure → error mentions var |
|---|---|---|---|
| `BASE_DOMAIN` | yes | — | non-empty; no `://`, `/`, `:`, whitespace; matches multi-label DNS hostname `^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$` (lowercased first) |
| `SCHEME` | no | `https` | ∈ {`http`,`https`} |
| `TTL` | no | `24h` | `time.ParseDuration` and `> 0` |
| `UPLOAD_TOKEN` | yes | — | non-empty |
| `GITHUB_CLIENT_ID` | yes | — | non-empty |
| `GITHUB_CLIENT_SECRET` | yes | — | non-empty |
| `ADMIN_GITHUB_USER` | yes | — | non-empty (stored lowercased) |
| `SESSION_SECRET` | yes | — | non-empty |
| `DATA_DIR` | no | `/data` | non-empty |
| `MAX_UPLOAD_SIZE` | no | `100MB` | `ParseSize` and `> 0` |
| `MAX_SITE_SIZE` | no | `500MB` | `ParseSize` and `> 0` |
| `MAX_FILE_COUNT` | no | `10000` | int `> 0` |
| `PORT` | no | `8080` | int in `1..65535` |
| `LOG_LEVEL` | no | `info` | ∈ {`debug`,`info`,`warn`,`error`} (case-insensitive) → `slog.Level` |

### `internal/metrics`
```go
type Metrics struct {
    HTTPDuration *prometheus.HistogramVec // name "http_request_duration_seconds", label "class"
    // lifecycle counters/gauges added by later cards
}
// Registers collectors.NewGoCollector() and collectors.NewProcessCollector(...) plus
// HTTPDuration on reg. No global/promauto registry.
func New(reg prometheus.Registerer) *Metrics
```

### `internal/server`
```go
type HealthFunc func(context.Context) error

type Deps struct {
    Config   *config.Config
    Clock    clock.Clock
    Logger   *slog.Logger
    Metrics  *metrics.Metrics
    Registry *prometheus.Registry // for the /metrics handler
    Health   HealthFunc           // S1: func(ctx) error { return nil }
}

func New(d Deps) (http.Handler, error) // renders the 404 page once; wires routing + middleware

// unexported helpers (routing.go), unit-tested directly:
func normalizeHost(hostHeader string) string        // lowercase, strip :port
func siteLabel(host, baseDomain string) (slug string, ok bool) // exactly one label below base
func routeClass(cfg *config.Config, r *http.Request) string     // "app" (apex) else "site"
```

**Handler shape** (matches `01-architecture.md` "HTTP routing"):
```
top(w, r):
  switch r.URL.Path {
    case "/healthz": health(w, r); return          // any Host, bypasses middleware
    case "/metrics": promhttp handler; return       // any Host, bypasses middleware
  }
  routed.ServeHTTP(w, r)                             // requestLog(instrument(rootHost))

rootHost(w, r):
  host := normalizeHost(r.Host)
  if host == cfg.BaseDomain      → apexMux           // S1: empty http.ServeMux → 404
  else if _, ok := siteLabel(host, cfg.BaseDomain); ok → serveNotFound  // S1: no store yet
  else                            → serveNotFound     // unknown / multi-label host

health(w, r): if Health(ctx)==nil → 200 "ok" else → 503   // S1 stub always ok
serveNotFound(w, r): 404, Content-Type text/html, X-Content-Type-Options nosniff, notFoundBytes
```
`apexMux` is an empty `*http.ServeMux` in S1 (returns Go's plain `404 page not found`
for the app surface); CARD-007/008/009 add patterns to it. `serveNotFound` is the
site-host branch stub CARD-003 replaces with real serving.

### Middleware (`internal/server/middleware.go`)
Order (outermost first), per `01-architecture.md`: `requestLog` → `instrument`.
- `responseRecorder` — wraps `http.ResponseWriter`, captures status (default 200) and
  byte count; unwrapping for future `Flusher`/`Hijacker` via
  `http.NewResponseController`.
- `requestLog(log)(next)` — records `start := time.Now()`; serves; logs one JSON line
  `msg="request"` with `host, path, method, status, dur_ms, bytes`. Uses the monotonic
  wall clock, **not** `Deps.Clock` (latency ≠ business time; see Gotchas/KNOWLEDGE).
- `instrument(m, cfg)(next)` — times the call, observes
  `m.HTTPDuration.WithLabelValues(routeClass(cfg, r)).Observe(seconds)`.

### `web`
```go
package web
//go:embed notfound.html
var Files embed.FS
```

## Data flow
No persistence in S1. Startup wiring (`cmd/drop/main.go`):
```
os.Getenv ─▶ config.Load ─▶ Config
                             │
      slog.NewJSONHandler(os.Stdout, Level=Config.LogLevel) ─▶ Logger
      clock.Real{} ─▶ Clock
      prometheus.NewRegistry() ─▶ reg ─▶ metrics.New(reg) ─▶ Metrics
      Health := func(ctx) error { return nil }   // CARD-002 replaces
                             ▼
    server.New(Deps{Config, Clock, Logger, Metrics, Registry: reg, Health})
                             ▼
    http.Server{Addr: Config.Addr(), Handler, ReadHeaderTimeout: 10s}
      + SIGINT/SIGTERM ─▶ srv.Shutdown(ctx)
```
Per-request: `top` → (ops bypass) or `requestLog`→`instrument`→`rootHost`→branch.
Config errors are printed to stderr before the logger exists (logger level comes from
config); `main` returns the error and exits non-zero.

No schema/migration impact (no DB this card).

## Implementation task list
All paths under the worktree. Each task is one red→green→commit cycle; write the named
failing test first, run it red, implement, run green, `gofmt`/`go vet`, commit.

1. **Scaffold.** Create `go.mod` (`module nyx-drop`, `go 1.24`), add
   `github.com/prometheus/client_golang` (via the first import that needs it; run
   `go mod tidy` after task 7), `.dockerignore` (`.git`, `docs`, `*.md` except
   README? keep `README.md`), and extend `.gitignore` (`/tmp/`, `drop`). Verify `go
   build ./...` on the empty module. No test.
2. **`internal/clock`.** Test `clock_test.go`: `TestReal_NowIsUTC` (location == UTC),
   `TestFake_AdvanceAndSet` (Advance moves by d; SetNow stored UTC; Now idempotent
   between calls). Implement `clock.go`.
3. **`internal/config` — size parser.** Test in `size_test.go`:
   `TestParseSize_Table` (`"100MB"`→1e8, `"500MB"`→5e8, `"1KiB"`→1024, `"1MiB"`→2^20,
   `"1024"`→1024, case-insensitive `"10mb"`; errors on `""`, `"10PB"`, `"-5MB"`,
   `"abc"`, overflow) and `FuzzParseSize` (never panics). Implement `size.go`.
4. **`internal/config` — Load.** Test `config_test.go`: `TestLoad_AllDefaults` (only
   required vars set → defaults applied, values typed correctly),
   `TestLoad_InvalidAndMissing` (table, one row per AC-2 case; assert
   `strings.Contains(err.Error(), varName)`), `TestConfig_Accessors`
   (`Addr()==":8080"`, `ExternalOrigin()=="https://sites.nyxhub.net"`). Implement
   `config.go`.
5. **`web` embed + 404 page.** Create `web/notfound.html` from
   `ui-mockups/notfound.html`, replacing the hardcoded link target with `{{.AppURL}}`
   (`html/template`). Create `web/embed.go`. No standalone test (covered in task 8).
6. **`internal/server` — routing helpers.** Test `routing_test.go`:
   `TestNormalizeHost` (`"Slug.Example.COM:8080"`→`"slug.example.com"`, no-port
   passthrough, empty), `FuzzNormalizeHost` (idempotent, no panic),
   `TestSiteLabel` (`"trusty-tahr.sites.nyxhub.net"`→(`"trusty-tahr"`,true);
   apex→(_,false); `"a.b.sites.nyxhub.net"`→(_,false); unrelated host→(_,false)),
   `TestRouteClass` (apex→`"app"`, site host→`"site"`, unknown→`"site"`). Implement
   `routing.go`.
7. **`internal/metrics`.** Test `metrics_test.go`: `TestNew_RegistersCollectors`
   (gather → `go_goroutines` and a `process_*` present) and
   `TestHTTPDuration_Observes` (observe one sample with class `"app"`;
   `testutil.CollectAndCount` == 1). Implement `metrics.go`. Run `go mod tidy`.
8. **`internal/server` — pages render.** Test in `server_test.go` (or `pages_test.go`):
   `TestRenderNotFound_InjectsAppURL` (rendered bytes contain
   `cfg.ExternalOrigin()` and "faded into the night"). Implement `pages.go`
   (`renderNotFound(cfg) ([]byte, error)` using `web.Files` + `html/template`).
9. **`internal/server` — middleware.** Test `middleware_test.go`:
   `TestResponseRecorder_CapturesStatusAndBytes`,
   `TestRequestLog_OneLinePerRequest` (capture a `slog` JSON handler into a buffer;
   assert exactly one line with keys host/path/method/status/dur_ms/bytes),
   `TestInstrument_ObservesByClass`. Implement `middleware.go`.
10. **`internal/server` — New + routing (integration).** Test in `server_test.go`
    using `httptest`, a `clock.Fake`, `BASE_DOMAIN=test.local`, `req.Host` set
    directly: `TestHealthz_AnyHost_200`, `TestMetrics_AnyHost_Exposition`,
    `TestOpsEndpoints_NotLogged`, `TestUnknownHost_Branded404`,
    `TestMultiLabelHost_404`, `TestSiteHost_404UntilStore` (valid label, no store →
    branded 404), `TestApex_404Placeholder`. Provide a `newTestServer(t)` helper
    returning `(http.Handler, *clock.Fake)`. Implement `server.go` (`New`, `top`,
    `rootHost`, `health`, `serveNotFound`).
11. **`cmd/drop/main.go`.** Extract `run(getenv func(string) string) error` (build
    config→logger→clock→registry→metrics→server→`http.Server`; SIGINT/SIGTERM
    graceful shutdown; `ReadHeaderTimeout: 10*time.Second`). `main` calls `run`,
    prints error to stderr, `os.Exit(1)`. Light smoke via `run` is optional; core
    logic is already covered by tasks 4/10. Verify `go run ./cmd/drop` locally with
    dev env then `curl -s localhost:8080/healthz`.
12. **Dockerfile + `.dockerignore`.** Use the two-stage build from `05-deployment.md`
    (`golang:1.24` builder, `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`,
    `gcr.io/distroless/static:nonroot`, `USER 65532:65532`, `EXPOSE 8080`). Verify
    `docker build .` and `docker run` → `curl /healthz`.
13. **CI `.github/workflows/ci.yml`.** Jobs on push/PR: `test` (setup-go 1.24; `go vet
    ./...`; `test -z "$(gofmt -l .)"`; `go test -race ./...`) and `docker` (`docker
    build .`, no push; registry-push step present but commented, registry undecided).
14. **`README.md`.** What-it-is, the config-variable table, local-dev block from
    `05-deployment.md` (`BASE_DOMAIN=localtest.me SCHEME=http …`), and `docker build`.
    Grow in later cards.

## Test strategy
- **Coverage:** ≥ 90% on the core logic layer — `internal/config` (Load, ParseSize,
  accessors), `internal/clock`, `internal/server` routing helpers
  (normalizeHost/siteLabel/routeClass), `renderNotFound`, and the middleware
  recorder. `cmd/drop/main.go` (blocking `ListenAndServe`, signal wiring) is excluded
  from the core-coverage figure; its behavior is covered indirectly by
  `server`/`config` tests.
- **Table tests** drive config validation (one row per missing/invalid variable,
  asserting the message names the variable) and `ParseSize`.
- **Property / fuzz (Go native):** `FuzzParseSize` (no panic on arbitrary input);
  `FuzzNormalizeHost` (idempotent: `normalizeHost(normalizeHost(x)) ==
  normalizeHost(x)`, no panic). These are the invariants worth guarding here
  (idempotency, total-function-on-all-input) — the doctrine's property-test targets.
- **Integration (`httptest`):** exercise the exported `http.Handler` only (never
  internal calls) with `req.Host` set directly and `BASE_DOMAIN=test.local`
  (`01-architecture.md` "Host trick"): healthz on many hosts, metrics exposition,
  ops-endpoints-not-logged (buffer-backed slog handler), branded/multi-label 404,
  apex placeholder.
- **Determinism:** fake clock where a clock is threaded; no network; latency assertions
  check *presence and type* of `dur_ms`, not a wall-clock value. No flaky retries.
- **Metrics:** `prometheus/client_golang/prometheus/testutil` against a fresh
  `prometheus.NewRegistry()` per test — no global registry.
- **Gates:** `go vet ./...` clean; `gofmt -l .` empty; `go test -race ./...` green;
  `docker build .` succeeds. These are the S1 CI job.

## Spec references
- spec `docs/superpowers/specs/2026-07-09-nyx-drop-design.md`:
  *Architecture overview* (Host→behavior table, §24–34);
  *Static serving* (host normalization, `/healthz` bypass, §214–228);
  *Configuration (environment variables)* (variable table + fail-fast rule, §230–249);
  *Deployment* (distroless/non-root image, §251–260);
  *Observability* (JSON `slog` one-line-per-request fields, `/metrics` host-independent,
  route-class histogram, §262–265).
- `docs/implementation/00-overview.md`: fixed technology decisions (module name
  `nyx-drop`, `ServeMux`, `slog` JSON), dependency policy (four external modules),
  invariants 5 (UTC), 8 (`/healthz`+`/metrics` before host routing), 9 (injected
  `Clock`), definition of done.
- `docs/implementation/01-architecture.md`: repository layout, `Clock` interface,
  HTTP routing + `apexMux`, middleware order (requestLog → metrics), logging/metrics
  conventions (no promauto global registry), testing infrastructure (fake clock, Host
  trick, exported-handler tests).
- `docs/implementation/05-deployment.md`: Dockerfile, CI job list, local-dev env.
- `docs/implementation/06-vertical-slices.md` §S1 (scope + acceptance).
- `docs/implementation/ui-mockups/notfound.html`: source for `web/notfound.html`.
