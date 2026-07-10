# CARD-001 — pr-review.md

Expert review panel seeded on implementation PR
[#3](https://github.com/stebennett/nyx-drop/pull/3) on 2026-07-10, after CI went green
(both `test` and `docker` jobs passed on the workflow's first-ever run).

Six lenses dispatched in parallel. The `python` and `typescript` lenses do not apply — the
diff is 18 `.go`, 6 `.md`, `Dockerfile`, `ci.yml`, `go.mod`/`go.sum`, one embedded `.html`,
and 4 fuzz-corpus files.

| Lens | Model | Findings | Posted |
|---|---|---|---|
| design | opus | 0 | nothing |
| functionality | opus | 0 | nothing |
| security | opus | 3 | review `4670330861` |
| tests | sonnet | 1 | review `4670343576` |
| simplicity | sonnet | 2 | review `4670334233` (+ duplicate `4670334597`, see below) |
| readability | sonnet | 2 | review `4670327233` |

**8 distinct findings, none blocking.** No lens approved, requested changes, or resolved a
thread — by design, that stays with the human reviewer.

---

## [security] — 3 findings

- **`cmd/drop/main.go:67` — no `IdleTimeout` (Question).** The server sets only
  `ReadHeaderTimeout: 10s`. `IdleTimeout` falls back to `ReadTimeout` (not
  `ReadHeaderTimeout`), and both are zero, so **idle keep-alive connections are never
  reaped** — a connection-hold resource-exhaustion vector. Mitigated if a fronting ingress
  reaps idle client connections, but the pod is directly reachable in-cluster.
  *Orchestrator verified: confirmed, only `ReadHeaderTimeout` is set. The design specified
  exactly that field, so adding `IdleTimeout` is a hardening addition, not a contradiction
  of the merged design.*
- **`.github/workflows/ci.yml:8` — no top-level `permissions:` block.** Both jobs inherit the
  repo-default `GITHUB_TOKEN` scope, potentially read-write. Suggested
  `permissions: { contents: read }`. *Orchestrator verified: confirmed absent. Trigger is
  `pull_request`, not `pull_request_target`, and no secrets are referenced — so this is
  defense-in-depth, not an active hole.*
- **`.github/workflows/ci.yml:12` — actions pinned to mutable major tags (Nit).**
  `actions/checkout@v4`, `actions/setup-go@v5` rather than commit SHAs; supply-chain
  tamper-evidence.

Probed and found clean: **host confusion** (`evilsites.nyxhub.net`, `xsites.nyxhub.net`,
`sites.nyxhub.net.evil.com`, leading/trailing dots, uppercase, bracketed IPv6 — all fall
through to a branded 404; only `<label>.<base>` yields a slug); **XSS in the templated 404**
(`{{.AppURL}}` is `cfg.ExternalOrigin()`, not attacker-controlled, rendered through
`html/template`'s contextual escaping; the page is self-contained with inline CSS/SVG and
zero external requests); **secret leakage** (`UPLOAD_TOKEN`, `GITHUB_CLIENT_SECRET`,
`SESSION_SECRET` fail with `… is required` and never echo their values; only non-secret vars
quote their value in the error); **metric-label cardinality** (`routeClass` returns a fixed
`{app, site}` set, so an attacker-controlled `Host` cannot explode histogram series);
**log injection** (slog's JSONHandler escapes `r.Host`/path — no CRLF forging); and the
**Dockerfile** (distroless static nonroot, uid 65532, `CGO_ENABLED=0`, no secrets baked,
`.dockerignore` excludes `.git`/`docs`).

## [tests] — 1 finding

- **`internal/config/size_test.go:27` — the "overflow" table row never reaches the overflow
  guard.** Its 22-digit input `9223372036854775807999MB` is rejected by `strconv.ParseInt`'s
  own range check first, so `size.go:62`'s `if mult != 0 && n > math.MaxInt64/mult` guard has
  **zero coverage**. `FuzzParseSize` only asserts no-panic, so it would not catch a broken
  guard either. Posted with a `suggestion` block adding two boundary rows (the guard
  boundary, plus the untested `numPart == ""` branch at `size.go:49-51`).
  *Orchestrator verified empirically: `ParseSize("9223372036854775807999MB")` errors with
  `strconv.ParseInt: value out of range`, while `ParseSize("9223372036854776kb")` — which fits
  int64 alone but overflows after ×1000 — correctly errors with `overflow`. The guard works;
  nothing exercises it.*

Verified non-tautological, which was this lens's highest-value assignment: the task-10
integration tests in `server_test.go` were written **after** `server.go` (a self-reported TDD
deviation). `TestHealthz_AnyHost_200`, `TestOpsEndpoints_NotLogged`, `TestApex_404Placeholder`,
`TestUnknownHost_Branded404`, `TestMultiLabelHost_404`, and `TestSiteHost_404UntilStore` were
each hand-traced against `top()`/`rootHost()`; every one fails under a wrong routing order or
host branch. Also confirmed: all 17 rows of `TestLoad_InvalidAndMissing` assert
`strings.Contains(err.Error(), wantVar)` rather than merely `err != nil`; every metrics and
middleware test builds its own `prometheus.NewRegistry()` with no shared global state; and the
4 checked-in `FuzzNormalizeHost` corpus files (`[0:0]`, `[:0]`, `[0:]:`, `[:]:`) are genuine
idempotency fixed-points, traced byte-by-byte — not decoration.

## [simplicity] — 2 findings

- **`internal/config/config.go:70` — a 4-line get/check-empty/error/assign block repeats four
  times** for `UPLOAD_TOKEN`, `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `SESSION_SECRET`.
  Suggested a `requireString(getenv, name) (string, error)` helper. (`BASE_DOMAIN` and
  `ADMIN_GITHUB_USER` trim/lowercase first, so they don't fit the same shape.)
- **`internal/config/config.go:101` — `if dataDir == ""` is unreachable (Nit).**
  `stringOr(getenv("DATA_DIR"), "/data")` can never return `""` given a non-empty default.
  Delete it, or move the non-empty check onto the raw env value.
  *Orchestrator verified: confirmed dead code.*

**Notable: this lens refuted its own leading hypothesis before publishing.** It suspected
`normalizeHost`/`stripPort`/`isDigits` were hand-rolled complexity that `net.SplitHostPort`
could replace, wrote a throwaway program to test the rewrite, found that `SplitHostPort`
**breaks the idempotency invariant** on bracketed input (`"[0:0]"` → `"0:0"` → `"0"`), and
dropped the finding. The hand-rolled bracket handling is necessary complexity, not
gold-plating. It also declined to flag `rootHost`'s two `serveNotFound` branches, correctly
identifying them as `design.md`'s explicit CARD-003 extension seam.

## [readability] — 2 findings

- **`internal/config/size.go:29` — `ParseSize`'s doc comment omits the bare `K`/`M`/`G`
  decimal aliases** that `sizeSuffixes` defines and `TestParseSize_Table` exercises.
  `README.md:33`'s `MAX_UPLOAD_SIZE` row has the same gap.
  *Orchestrator verified: `sizeSuffixes` ends with `{"g", 1_000_000_000}`; neither the doc
  comment nor the README mentions the bare single-letter form. An accepted input form is
  undocumented in both places.*
- **`cmd/drop/main.go:35` — `run()`'s doc comment cites internal task numbers `(tasks 4/10)`
  (Nit)** from the design's task list, which will read as opaque or stale to a future reader
  without `design.md` open.

Checked clean: the `normalizeHost`/`stripPort` idempotency comment is pitched at the right
altitude — it explains *why* bracket content is routed back through `stripPort` rather than
restating the code, which matters because that invariant being implicit is what let the
original bug through. The remaining 12 README config rows were cross-checked line-by-line
against `config.go`'s validation and all match.

## [design] — 0 findings

Verified the import graph rather than assuming it: `internal/clock` and `internal/config`
import stdlib only; `internal/metrics` does **not** import `config` (the server passes `cfg`
into `instrument`, keeping them decoupled); `internal/server` is imported only by the
`cmd/drop` composition root. No reverse or sideways layer imports. Every extension seam the
next cards need holds without a signature break: `Deps` is a struct (CARD-002 adds fields),
`Health` is already an injected `func(context.Context) error`, `siteLabel` already returns the
slug CARD-003 needs, `apexMux` is an in-package `*http.ServeMux` for CARD-007/008/009, and
`routeClass(cfg, r)` already takes the full `*http.Request` so CARD-008 can classify
`/admin/*` by path. Explicitly declined to flag the unused `Deps.Clock` and the empty
`apexMux` as speculative — both have real second consumers on the milestone plan.

## [functionality] — 0 findings

Hand-traced all four acceptance criteria to their code paths with real values, and swept the
routing edge cases: `""`, `"Slug.Example.COM:8080"`, `"[::1]:8080"`, `"[0:0]"`, `"[:0]"`,
`"a:b:c"`, `"fe80::1"`, `"[a]:5"`, unterminated `"[abc"`, `"[a]b"`, `"example.com:"`. All
total, all idempotent. Independently derived why the fix holds: `stripPort` refuses to strip
when the candidate is empty or still contains a colon, so its output is colon-free and
re-application is a no-op. Also verified `ParseSize`'s longest-suffix-first ordering (so `mb`
isn't shadowed by `m`, nor `kib` by `k`), that overflow is guarded *before* the multiply, and
that `main.go`'s graceful shutdown drains via a buffered `serveErr` channel with **no
goroutine leak**.

---

## Panel infrastructure defect (not a code finding)

The `[simplicity]` review was posted **twice** — `4670334233` and `4670334597` — with identical
inline comments on `config.go:70` and `config.go:101`. The `tests` lens reported that its first
`gh` review POST silently submitted a *different concurrent lens's* payload; it detected this by
checksumming its request body and independently re-fetching the created review, then retried and
posted its own `[tests]` review (`4670343576`).

Nothing was lost — all six lenses' findings are accounted for — but the PR carries 2 duplicate
inline comments. Consequence for the address loop: a duplicated comment would otherwise be
addressed twice and replied to twice.

Captured in `KNOWLEDGE.md` under Gotchas. Future `pr-expert-reviewer` dispatches should verify
the created review by id after posting rather than trusting the POST response body.
