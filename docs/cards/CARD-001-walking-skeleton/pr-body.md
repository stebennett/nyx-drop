## CARD-001 — design: Walking skeleton: config, healthz/metrics, host routing, Dockerfile, CI   [task · infra]

### Why
Stand up the project's walking skeleton so an operator can run the binary — locally and as a distroless container — and probe it. This card creates the repository from empty: the Go module, the `cmd/` + `internal/` + `web/` layout that every later card extends, and the plumbing they all consume — configuration parsing/validation, the injected `Clock`, JSON `slog` setup, the Prometheus registry + HTTP histogram, the host-normalizing root handler (`/healthz`, `/metrics`, branded not-found page), request-logging and metrics middleware, the Dockerfile, and the CI workflow. No product feature ships; the value is a running, observable, packaged process and the package boundaries every subsequent slice builds on. This is slice **S1** in `06-vertical-slices.md`.

**This is the first code in the repo**, so the scaffolding decisions here (module path, package boundaries, wiring seams) are load-bearing for CARD-002…011 — the cheapest moment to correct a package boundary is now.

### Design summary
- **Pure core, imperative shell.** All decidable logic lives in pure functions over plain data (`config.Load` over an injected `getenv`, `ParseSize`, `normalizeHost`, `siteLabel`, `routeClass`, `renderNotFound`); `net/http`, `embed`, `os`, Prometheus and signal handling stay at the edges (`internal/server`, `web`, `cmd/drop`). Config is parsed via `Load(getenv func(string) string)` rather than reading `os.Getenv` internally, so tests inject a map instead of mutating process env (race-prone under `-race`).
- **Extension seams for later cards, chosen deliberately.** `server.New(server.Deps{…}) (http.Handler, error)` takes a struct, not a positional arg list, so CARD-002/003/007/008 add wiring (store, locks, auth) by adding fields without touching call sites. Health readiness is an injected `func(context.Context) error` — an always-nil stub in S1, replaced by CARD-002's real DB-ping + data-dir-writability check.
- **Ops endpoints bypass the middleware** (ADR-0002). `/healthz` and `/metrics` are matched at the top of the handler, above `requestLog`+`instrument`, so kubelet's ~10s probes and Prometheus scrapes neither flood the access log nor dominate the request histogram. Host-routed traffic only: `requestLog` → `instrument`, matching `01-architecture.md`'s middleware order.
- **Route-class taxonomy fixed now** (ADR-0002): apex host → `app`; every other host (valid site label, multi-label, or unknown) → `site`; `admin` reserved for CARD-008/009. The Prometheus registry is created in `main` and passed explicitly — no promauto/global registry, so every test builds its own.
- **Size suffixes get a written decision** (ADR-0001): `MB` is ambiguous (1e6 vs 2^20) and silently shifts real upload limits. `config.ParseSize` fixes decimal SI (`KB/MB/GB = 1000^n`) with explicit binary variants (`KiB/MiB/GiB`), in ~40 lines rather than a third-party library the dependency policy forbids.
- **The branded 404 is templated, not static.** `web/notfound.html` is rendered once at startup with `html/template`, injecting `cfg.ExternalOrigin()` — the mockup's hardcoded `sites.nyxhub.net` link would be wrong for any other self-hosted domain.

### Acceptance criteria (sharpened)
- **`/healthz` answers on any Host, matched before host routing.** `GET /healthz` → 200 body `ok` for `Host` ∈ {`sites.nyxhub.net`, `anything.example`, `10.0.0.5:8080`, empty}; S1 readiness is an always-succeeding stub. — spec *Static serving*; `00-overview.md` invariant 8. Test: `TestHealthz_AnyHost_200`.
- **Bad configuration fails fast, naming the variable, non-zero exit.** `config.Load` errors mention the offending variable for missing `BASE_DOMAIN`; `BASE_DOMAIN` with scheme/port/path; `SCHEME=ftp`; `TTL=banana`/`TTL=0`; `MAX_UPLOAD_SIZE=10PB`; `MAX_FILE_COUNT=-1`; `PORT=70000`; `LOG_LEVEL=trace`; and each missing required var (`UPLOAD_TOKEN`, `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `ADMIN_GITHUB_USER`, `SESSION_SECRET`). `main` prints to stderr and exits 1. — spec *Configuration*. Test: `TestLoad_InvalidAndMissing` (table-driven).
- **`/metrics` serves the registry host-independently; exactly one JSON log line per host-routed request.** Exposition format on any Host, exposing `go_*`, `process_*`, `http_request_duration_seconds`. Each host-routed request emits one JSON `slog` line with `host, path, method, status, dur_ms, bytes`; `/healthz` and `/metrics` emit none. — spec *Observability*; invariant 8. Tests: `TestMetrics_AnyHost_Exposition`, `TestRequestLog_OneLinePerRequest`, `TestOpsEndpoints_NotLogged`.
- **Unknown host → branded 404; image builds distroless/non-root; CI green.** Unknown, site-label, and multi-label hosts all → 404 with the branded self-contained HTML (`text/html`, body contains "faded into the night"). `docker build .` succeeds; final image `gcr.io/distroless/static:nonroot`, uid 65532. CI runs `go vet`, `gofmt -l` (fails if non-empty), `go test -race ./...`, `docker build`. — spec *Architecture overview*, *Deployment*. Tests: `TestUnknownHost_Branded404`, `TestMultiLabelHost_404`.

### ADRs in this PR
- ADR-0001 — Human-readable byte-size suffixes: decimal SI with binary variants
- ADR-0002 — HTTP observability contract: ops-endpoint bypass and route-class taxonomy

This PR also adds `docs/adrs/template.md` and the index table in `docs/adrs/README.md`, which the scaffold left empty.

### Open questions / decisions deferred
None — the designer raised no open questions and reported no spec discrepancies.

One thing the design records rather than decides, worth a reviewer's eye: **invariant 9 ("handlers use the injected `Clock`, never `time.Now()`") governs *business* time, not latency.** The `requestLog`/`instrument` middleware deliberately measures request duration with the monotonic wall clock, because a frozen fake `Clock` would report `dur_ms=0`. This is captured as a KNOWLEDGE Gotcha so a future reviewer doesn't "fix" it into a bug.

Full design: `docs/cards/CARD-001-walking-skeleton/design.md` (in this diff). Merging this PR approves the
design and unblocks implementation — the implementation branch is cut from main after this merges,
and the code arrives as a second PR.

🤖 Design delivered via /kanban
