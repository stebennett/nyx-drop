# Implementation Notes — Overview

These documents accompany the design spec at
[`docs/superpowers/specs/2026-07-09-nyxhub-drop-design.md`](../superpowers/specs/2026-07-09-nyxhub-drop-design.md).
The spec says **what** the system does; these notes say **how** to build it. Where they
disagree, **the spec wins** — flag the discrepancy rather than silently choosing.

## Reading order

| Doc | Contents | Read when |
|---|---|---|
| [01-architecture.md](01-architecture.md) | Package layout, components, interfaces, concurrency, sessions, testing infrastructure | Before planning anything |
| [02-database.md](02-database.md) | SQLite schema DDL, migrations, every query, driver settings | Before touching the store |
| [03-api-reference.md](03-api-reference.md) | Every endpoint: exact requests, responses, headers, errors | Before writing any handler |
| [04-frontend.md](04-frontend.md) | Upload page and admin UI: files, DOM behavior, JS patterns, design system | Before writing any HTML/JS |
| [ui-mockups/](ui-mockups/) | High-fidelity static HTML mockups of every page and state (start at `index.html`) | Alongside 04-frontend.md |
| [05-deployment.md](05-deployment.md) | Dockerfile, Helm chart, CI, local development | Before packaging |
| [06-vertical-slices.md](06-vertical-slices.md) | Suggested build order as thin vertical slices with acceptance criteria | When writing the implementation plan |

## Technology decisions (fixed — do not revisit)

| Decision | Value | Rationale |
|---|---|---|
| Language | Go, latest stable (≥ 1.23) | Single static binary, stdlib HTTP |
| Go module name | `nyxhub-drop` | Not published; short local module path |
| Router | stdlib `net/http.ServeMux` (Go 1.22+ method/wildcard patterns, e.g. `"POST /api/sites"`, `"PUT /api/sites/{id}"`) | Zero dependencies; sufficient |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no cgo, works in distroless/scratch |
| Metrics | `github.com/prometheus/client_golang` | The standard |
| OAuth | `golang.org/x/oauth2` with overridable endpoint URLs | Well-trodden; endpoint override enables test stubbing |
| Frontend | Hand-written HTML/CSS/JS in `web/`, embedded via `embed.FS`. No framework, no bundler, no npm. | One binary; trivial assets |
| Zip | stdlib `archive/zip` | Sufficient |
| Logging | stdlib `log/slog`, JSON handler | Spec requirement |

**Dependency policy:** the four external modules above and their transitive deps only.
Do not add a router, ORM, config library, or UI framework. If something feels missing,
write it — every custom component here is under 100 lines.

## System invariants — never violate these

1. **A site is never servable in a half-written state.** All content changes happen in
   `/data/tmp` and land via `os.Rename` on the same filesystem.
2. **Expired means gone on public surfaces.** Serving and `PUT` treat
   `now >= expires_at && !permanent` as 404, even before the reaper runs. Only the admin
   surface can still see (and rescue) such a site.
3. **All same-site mutations hold the per-site lock**: `PUT`, admin delete, download
   snapshot, reap. The serve path is lock-free.
4. **The DB row is authority.** Creation inserts the row first (reserving the slug),
   then renames files into place; deletion removes the row first, then files. The
   startup sweep removes both crash artifacts: directories without rows and rows
   without directories.
5. **All timestamps are UTC**, stored as RFC 3339 strings, compared with `time.Time`
   after parsing — never string comparison for "is expired", always via the injected clock.
6. **Never trust upload paths.** Every entry path is validated (see extraction rules in
   the spec) before any filesystem write.
7. **The admin session cookie is `__Host-session`** and no cookie the app sets ever has a
   `Domain` attribute.
8. **`/healthz` and `/metrics` are matched before host routing.**
9. **Handlers never call `time.Now()` or `rand` directly** — always the injected `Clock`
   and `crypto/rand`-backed slug generator, so tests can control both.

## Glossary

- **slug / site id** — `<adjective>-<animal>-<suffix>`, e.g. `trusty-tahr-x7k2mq`. The
  terms are interchangeable; the DB column is `id`.
- **apex** — the configured `BASE_DOMAIN` itself (serves the app).
- **site host** — exactly one DNS label followed by `.` + `BASE_DOMAIN`.
- **reaper** — the background goroutine that deletes expired, non-permanent sites.
- **rescue** — marking an expired-but-not-yet-reaped site permanent so the reaper skips it.
- **snapshot** — the temp zip built under `/data/tmp` for an admin download.

## Definition of done (whole project)

- `go test ./...` passes; `go vet ./...` clean; `gofmt` clean.
- Integration test suite covers every row of the spec's Testing section.
- `helm lint` passes; `helm template` golden tests pass.
- `docker build` produces an image that starts with only env vars and a volume, and
  serves the full upload → visit → expire → admin lifecycle.
- A `README.md` documents: what it is, config reference, API examples (curl), Helm
  install command, local dev instructions.
