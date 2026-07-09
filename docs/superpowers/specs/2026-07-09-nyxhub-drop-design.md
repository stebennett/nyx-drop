# NyxHub Drop — Design Specification

**Date:** 2026-07-09
**Status:** Approved

## Purpose

A self-hosted clone of Cloudflare Drop, deployable into a Kubernetes cluster. Users upload a set of static assets (HTML, CSS, JS, images, …) or a zip file, and the service publishes them as a temporary site on a random subdomain of a configured base domain (e.g. base domain `sites.nyxhub.net` → site at `trusty-tahr-x7k2mq.sites.nyxhub.net`). Sites expire after a configurable TTL unless an admin marks them permanent.

## Requirements summary

- Upload assets or a zip → temporary site on a random subdomain of the configured base domain.
- Sites live for a single, globally configured TTL, then are torn down automatically.
- Admin interface lists all running sites.
- Admin can mark a site **permanent** (survives TTL and pod/node restarts), and unset permanence at any time (TTL then applies again).
- Admin can download a site's contents as a zip.
- Admin can delete a site immediately.
- API to create sites programmatically by POST; response includes the site URL.
- Existing sites can be updated in place (same URL); an update resets the TTL timer.
- Uploads and the API are token-gated. Admin access is via GitHub OAuth restricted to a single configured GitHub user.

## Architecture overview

A single Go binary running as one Kubernetes Deployment (1 replica), backed by one PersistentVolumeClaim. One HTTP server handles everything, routing by `Host` header:

| Host | Behavior |
|---|---|
| `<BASE_DOMAIN>` (e.g. `sites.nyxhub.net`) | App: upload page, admin UI, API, OAuth callback |
| `<slug>.<BASE_DOMAIN>` | Static file serving for that site |
| Unknown slug / expired site / other host | Branded "site not found" 404 page |

A wildcard Ingress (`*.<BASE_DOMAIN>` plus the apex host) routes all traffic to the Service. TLS is the cluster's responsibility: the Helm chart references an existing wildcard TLS secret or carries cert-manager annotations via values; the app itself serves plain HTTP inside the cluster.

## Storage & data model

All state lives on the PVC under `DATA_DIR` (default `/data`):

```
/data/drop.db                  # SQLite metadata (pure-Go driver, no cgo)
/data/sites/<site-id>/...      # extracted site files
/data/tmp/                     # staging area for in-progress uploads
```

SQLite `sites` table:

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | The slug |
| `created_at` | TIMESTAMP | |
| `updated_at` | TIMESTAMP | Equal to `created_at` initially; set on every `PUT` |
| `expires_at` | TIMESTAMP | `created_at + TTL` at creation |
| `permanent` | BOOLEAN | Default false |
| `size_bytes` | INTEGER | Total extracted size |
| `file_count` | INTEGER | |
| `source` | TEXT | `api` or `web` |

**Slugs** are readable, Ubuntu-release-style names in the spirit of [bschiffthaler/mkname](https://github.com/bschiffthaler/mkname): a random **adjective + animal pair** followed by a random uniqueness suffix. Format:

```
<adjective>-<animal>-<suffix>     e.g.  trusty-tahr-x7k2mq
```

- Adjective and animal are chosen independently from embedded word lists (lowercase ASCII, DNS-safe). Alliteration is not required — when it happens it's a happy accident.
- The suffix is 6 characters of lowercase alphanumeric (`a-z0-9`), generated from a cryptographically secure source, and guarantees uniqueness — the word pair is for readability only.
- The whole slug must remain a valid DNS label (≤ 63 chars; word lists are curated to keep well under this).
- On collision (unique constraint violation on insert), regenerate the suffix and retry, bounded at 5 attempts before failing the request with `500` (with ~2 billion suffixes, hitting this bound indicates a bug, not bad luck).

**Time semantics:** all timestamps are stored and compared in UTC and serialized as RFC 3339 in API responses. A site is expired when `now >= expires_at`.

**Permanence semantics:** `permanent = true` means the reaper ignores `expires_at`. Unsetting permanence sets `expires_at = now + TTL`, so the site gets a full TTL from the moment of unsetting rather than vanishing instantly.

**Atomic creation:** uploads are extracted into a temp directory under `/data/tmp` (same filesystem), validated, then renamed into `/data/sites/<id>` and the DB row inserted. A site is never servable in a half-written state. Stale temp directories are removed on startup. If the volume fills mid-extraction (`ENOSPC`), the temp directory is cleaned up and the upload fails with a structured `500`; no partial state remains. There is no global storage quota — the operator sizes the PVC appropriately.

**Schema migrations:** embedded SQL migrations run at startup, versioned via SQLite's `PRAGMA user_version`. Each migration is applied in a transaction; the app refuses to start if the on-disk version is newer than the binary knows.

## Site creation

### API conventions

- All API errors are JSON: `{"error": "<human-readable message>"}` with the appropriate status code.
- Unsupported methods on known routes return `405`.
- Site hosts (`<slug>.<BASE_DOMAIN>`) serve `GET`/`HEAD` only; other methods return `405`.
- Timestamps in responses are RFC 3339 UTC.

### API

`POST /api/sites`
`Authorization: Bearer <UPLOAD_TOKEN>`
Body: `multipart/form-data`, either:

- a single field `file` containing a `.zip` archive, or
- multiple `files` fields with relative paths preserved (`filename` may contain `/`-separated paths).

Success — `201 Created`:

```json
{
  "id": "trusty-tahr-x7k2mq",
  "url": "https://trusty-tahr-x7k2mq.sites.nyxhub.net",
  "expires_at": "2026-07-10T12:00:00Z"
}
```

The URL scheme comes from the `SCHEME` config value (default `https`).

### Updating a site

`PUT /api/sites/{id}`
`Authorization: Bearer <UPLOAD_TOKEN>`
Body: same `multipart/form-data` format as creation (zip or multiple files).

Replaces the site's contents entirely while keeping its URL, and **resets the timer**: `expires_at = now + TTL`. A permanent site can be updated too — its contents are replaced and it stays permanent. All extraction rules and safety limits apply as for creation.

The swap is atomic per directory rename: the new content is extracted and validated in `/data/tmp`, the old site directory is renamed aside, the new one renamed into place, and the old content deleted. In-flight requests may briefly 404 between the two renames; this is accepted. Response is `200` with the same JSON shape as creation. Errors: `404` unknown id, plus the same `401`/`400`/`413` as creation.

This enables stable URLs for CI use ("redeploy the preview for PR #42"): create once, then `PUT` to the returned `id` on every push.

`PUT` to an **expired-but-not-yet-reaped** site returns `404` — expired means gone on all token-authenticated and public surfaces, regardless of whether the reaper has physically caught up.

**Per-site concurrency:** mutating and snapshotting operations on the same site id (`PUT`, admin delete, download snapshot, reap) are serialized by a per-site lock. Concurrent `PUT`s are last-write-wins in arrival order; a download snapshot never observes a half-swapped site. Operations on different sites do not block each other.

Errors: `401` bad/missing token, `413` size limits exceeded, `400` invalid archive / no files / path traversal detected.

### Upload page

Served at `<SCHEME>://<BASE_DOMAIN>/`. Drag-and-drop of a zip file or a folder of files. Prompts for the upload token (remembered in `localStorage`), calls `POST /api/sites`, displays the resulting site URL with a copy button.

Uploads show a **progress indicator** (percentage/bar driven by upload progress events) — with a 100MB body limit, silent uploads are not acceptable.

Folder handling: dropped folders are walked recursively via the `DataTransferItem.webkitGetAsEntry()` API (and `<input webkitdirectory>` as fallback), and each file's relative path (`webkitRelativePath` or the walked entry path) is set as the slash-containing `filename` on its `FormData` part — matching the multipart format the API expects.

### Extraction rules

- **Sole-top-level-directory stripping:** if, after extraction, the site root contains exactly one entry and it is a directory, its contents are promoted to the root (handles zips like `mysite/index.html` so the homepage serves at `/`). Applied once, not recursively; applies to both zip and multi-file uploads.
- **Dotfiles are filtered out:** any file or directory whose name begins with `.` (`.git/`, `.env`, `.DS_Store`, …) is silently dropped during extraction and never stored or served.
- A missing root `index.html` is accepted — the site simply 404s at `/` while other paths serve normally.
- **Empty results are rejected:** an upload that yields zero files after dotfile filtering (including an empty zip, or a zip containing only dotfiles) fails with `400`. Empty directories are not preserved — only files count.
- **Malformed paths are rejected (`400`):** duplicate paths within one upload, entry names containing backslashes (zip entries must use `/`), control characters, or a bare `.`/`..` component. Non-UTF-8 entry names are rejected.

### Upload safety

- `MAX_UPLOAD_SIZE` — request body limit.
- `MAX_SITE_SIZE` — total **uncompressed** size limit (zip-bomb guard, enforced during extraction).
- `MAX_FILE_COUNT` — per-site file count limit.
- Zip entries are sanitized; any entry resolving outside the site root (path traversal, absolute paths, `..`) rejects the whole upload with `400`.
- Symlinks in archives are skipped.

Every site receives the single global TTL; there is no per-site TTL override.

## Admin

### Authentication

GitHub OAuth (authorization code flow). `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` from config; callback URL `<SCHEME>://<BASE_DOMAIN>/auth/callback`. The authorization request carries a random `state` parameter, stored in a short-lived cookie (10-minute expiry, `__Host-` prefixed, cleared on callback) and verified on callback (login-CSRF protection). After the OAuth exchange, the authenticated GitHub login must equal `ADMIN_GITHUB_USER` **compared case-insensitively** (GitHub logins are case-insensitive); any other user receives `403`.

On success a signed session cookie is issued: name `__Host-session` (HMAC with `SESSION_SECRET`, `HttpOnly`, `Secure`, `Path=/`, no `Domain` attribute, `SameSite=Lax`, 7-day expiry). The `__Host-` prefix makes the cookie host-only and unsettable from subdomains — uploaded sites run arbitrary JS on `*.<BASE_DOMAIN>` and must not be able to shadow or fixate the admin session via `Domain=.<BASE_DOMAIN>` cookies (cookie tossing).

**CSRF protection:** all state-changing admin endpoints (`POST`/`DELETE`) additionally verify that the request's `Origin` (or `Referer`) header, when present, matches `<SCHEME>://<BASE_DOMAIN>`, on top of `SameSite=Lax`. All admin endpoints require a valid session.

**Sign-in/out flow:**

- `GET /admin` (or any admin page) without a valid session → `302` to `/auth/login`.
- `GET /auth/login` → generates `state`, redirects to GitHub's authorize URL.
- `GET /auth/callback` → verifies `state`, exchanges the code, checks the username, sets `__Host-session`, redirects to `/admin`.
- `POST /auth/logout` (session + Origin check) → clears the session cookie, redirects to `/`. The admin UI shows a logout button.
- A failed username check renders a "not authorized" page with no session issued.

### Admin UI

Served at `<SCHEME>://<BASE_DOMAIN>/admin` (embedded static frontend compiled into the binary via `embed.FS`; plain HTML/JS, no build-time framework). Table of all sites: slug (linked to the live site), created, last updated, expires (or "permanent"), size, file count, source. Per-site actions: toggle permanent, download zip, delete (with confirm).

### Admin API

| Endpoint | Method | Behavior |
|---|---|---|
| `/api/admin/sites` | GET | Paginated site list (see below) |
| `/api/admin/sites/{id}/permanent` | POST | Set permanent |
| `/api/admin/sites/{id}/permanent` | DELETE | Unset permanent; `expires_at = now + TTL` |
| `/api/admin/sites/{id}/download` | GET | Download a zip of the site's files (snapshot; see below) |
| `/api/admin/sites/{id}` | DELETE | Delete site immediately (files + row) |

**Listing:** `GET /api/admin/sites?page=1&per_page=50&sort=name|created|expires&order=asc|desc&q=<substring>`. Defaults: `page=1`, `per_page=50`, `sort=created`, `order=desc`. `q` filters by case-insensitive substring match on the slug (matched literally — SQL `LIKE` wildcards in the input are escaped). Permanent sites sort last under `sort=expires` (they have no effective expiry). Response includes `total` so the UI can render pager controls; the admin table exposes all three sortable columns and a **search box** wired to `q`.

**Query validation:** unknown `sort`/`order` values and non-positive `page`/`per_page` return `400`; `per_page` is capped at `200`.

**Expired-but-not-yet-reaped sites** (expiry passed, reaper hasn't ticked) still appear in the admin list, flagged as expired. Admin actions on them remain valid until the row is gone — in particular, **making one permanent rescues it** from the pending reap (the reaper re-checks the flag under the per-site lock). If the reaper wins the race, the action returns `404`.

**Auth failures:** admin *pages* redirect to `/auth/login` (302); admin *API* endpoints return `401` JSON, and the UI responds by redirecting to sign-in.

**Download:** to avoid a race with the reaper deleting files mid-stream, the handler first snapshots the site into a zip file under `/data/tmp`, then streams that file (`Content-Disposition: attachment; filename=<id>.zip`) and deletes it afterwards. If the site is reaped between snapshot start and completion the request fails cleanly with `404`; the client never receives a silently truncated archive.

## Lifecycle — the reaper

A background goroutine runs at startup and then every minute:

1. Query sites where `permanent = false AND expires_at <= now`.
2. For each: take the per-site lock, re-check the `permanent` flag (an admin may have rescued the site since the query), then delete the DB row and remove the site directory.

Deleting the row first guarantees an expired site stops being served immediately; a startup sweep removes any orphaned directories (no matching row), covering a crash between the two steps. Because metadata (including the permanent flag) lives in SQLite on the PVC, permanence survives pod and node restarts. Sites that expired while the pod was down are reaped by the startup run.

**TTL is not retroactive:** each site's `expires_at` is fixed at creation (or at permanence-unset). Redeploying with a changed `TTL` affects only future sites and future unsets; existing sites keep their stored expiry. This is intended behavior.

## Static serving

**Host normalization:** the `Host` header is lowercased and any port stripped before matching (DNS is case-insensitive; dev setups use ports). Exactly one label below `BASE_DOMAIN` is a site host — `a.b.<BASE_DOMAIN>` gets the 404 page.

For requests to `<slug>.<BASE_DOMAIN>`:

1. Look up the slug in SQLite. Missing, or expired-but-not-yet-reaped → 404 page.
2. Serve files rooted at `/data/sites/<slug>` with path-traversal-safe resolution.
3. Directory paths serve their `index.html`; missing files → plain 404.
4. Standard content types and ETag/conditional-request handling (including `HEAD` and `Range`) via Go's `http.ServeContent`/`http.ServeFile`.
5. All site responses carry `X-Content-Type-Options: nosniff` — the content is untrusted user uploads.

No SPA fallback rewriting in v1.

**`/healthz` bypasses Host routing:** kubelet probes hit the pod IP with an arbitrary Host header, so `GET /healthz` is matched before host-based dispatch and answered on any host. Likewise `GET /metrics` (see Observability).

## Configuration (environment variables)

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BASE_DOMAIN` | yes | — | e.g. `sites.nyxhub.net` |
| `SCHEME` | no | `https` | Scheme for generated URLs, OAuth callback, Origin checks (`http` for local dev) |
| `TTL` | no | `24h` | Site lifetime (Go duration) |
| `UPLOAD_TOKEN` | yes | — | Bearer token for uploads/API |
| `GITHUB_CLIENT_ID` | yes | — | OAuth app |
| `GITHUB_CLIENT_SECRET` | yes | — | OAuth app |
| `ADMIN_GITHUB_USER` | yes | — | Sole permitted admin login |
| `SESSION_SECRET` | yes | — | Session cookie HMAC key |
| `DATA_DIR` | no | `/data` | PVC mount path |
| `MAX_UPLOAD_SIZE` | no | `100MB` | Request body limit |
| `MAX_SITE_SIZE` | no | `500MB` | Uncompressed size limit |
| `MAX_FILE_COUNT` | no | `10000` | Files per site |
| `PORT` | no | `8080` | Listen port |
| `LOG_LEVEL` | no | `info` | slog level: `debug`, `info`, `warn`, `error` |

The app fails fast at startup if any required variable is missing **or any value is invalid** (unparseable `TTL` or size values, `SCHEME` not `http`/`https`, `BASE_DOMAIN` containing a scheme, port, or path), logging exactly which variable is wrong.

## Deployment

- **Image:** multi-stage Dockerfile; final stage distroless (`gcr.io/distroless/static`), single static binary with embedded frontend assets. Runs as non-root.
- **Upgrade downtime is accepted:** single replica on an RWO volume with `Recreate` strategy means image upgrades take all sites (including permanent ones) briefly offline. This is an explicit trade-off for the zero-dependency storage model.
- **Helm chart** with:
  - Deployment — 1 replica, `strategy: Recreate` (RWO volume), liveness/readiness probes on `GET /healthz`, resource requests/limits via values. `/healthz` returns `200` when the SQLite database answers a ping and the data directory is writable; `503` otherwise.
  - Service (ClusterIP).
  - PVC — size and storageClass via values.
  - Ingress — hosts `<BASE_DOMAIN>` and `*.<BASE_DOMAIN>`, ingress class, TLS secret name, and extra annotations all via values.
  - Secrets — `UPLOAD_TOKEN`, `GITHUB_CLIENT_SECRET`, `SESSION_SECRET` via an `existingSecret` reference or inline values.

## Observability

- **Structured logging:** JSON logs via Go's `log/slog` — one line per request (host, path, status, duration, bytes) plus lifecycle events (site created, reaped, made permanent, deleted, admin login) with the site id as a field. Log level configurable via `LOG_LEVEL` (default `info`).
- **Metrics:** Prometheus endpoint at `GET /metrics` (host-independent, like `/healthz`). Exposes standard Go/process metrics plus: `sites_active` and `sites_permanent` gauges, `sites_created_total`, `sites_updated_total`, `sites_reaped_total`, `sites_deleted_total`, `reap_errors_total` counters, `upload_bytes_total`, and HTTP request count/duration histograms labeled by route class (app, site, admin). The chart adds standard `prometheus.io/scrape` annotations (toggleable via values).

## Error handling

- Upload validation failures return structured JSON errors with appropriate status codes (`400`, `401`, `413`).
- Extraction failures clean up their temp directory; nothing partial reaches `/data/sites`.
- Reaper errors on one site are logged and don't block other deletions.
- SQLite is opened with WAL mode and busy timeout; all writes go through a single connection pool in the one process.

## Testing

- **Unit:** slug generation (DNS-label validity, suffix charset, bounded collision retry), zip extraction (happy path, traversal attempts, absolute paths, symlinks, backslash/control-char/non-UTF-8 entry names, duplicate paths, empty-after-filtering rejection, size/count limits, sole-top-level-dir stripping, dotfile filtering), store operations incl. pagination/sorting/search (including `LIKE`-wildcard escaping), reaper logic with an injected fake clock (expiry boundary is `now >= expires_at`), migration runner, config validation (missing and invalid values).
- **Integration:** `httptest`-based tests exercising the full flow — upload (zip and multi-file) → serve → expire → 404; update via `PUT` (contents replaced, timer reset, permanence preserved, unknown id → 404, expired-but-unreaped id → 404); concurrency (parallel `PUT`s to one site, download-during-update, permanent-rescue of an expired-unreaped site); host routing (apex vs slug vs unknown vs multi-label, uppercase Host, Host with port, `/healthz` and `/metrics` on any host); method handling (`405` on wrong methods, site hosts GET/HEAD-only); auth failures (missing/wrong upload token, no session → 302 for pages vs 401 JSON for API, wrong GitHub user, case-differing GitHub user succeeding); sign-in redirect and logout flows; CSRF (cross-origin `Origin` header rejected); admin list pagination/sort/search and query validation (`400` on bad params); download-vs-reap race behavior.
- **OAuth:** GitHub endpoints stubbed with a local test server (configurable OAuth base URL internally to allow this).
- **Chart:** `helm lint` plus `helm template` golden-file tests (default values and a fully-customized values fixture) run in CI.

## Out of scope (v1)

- Per-site TTL overrides.
- Horizontal scaling / multi-replica (single replica on RWO volume).
- SPA fallback routing.
- Custom domains per site.
- Upload rate limiting (token-gated audience is trusted).
- Site labels/descriptions and uploader attribution — sites are anonymous; the shared upload token means `source` (`api`/`web`) is the only provenance recorded.
- Uploader-initiated delete — only the admin can delete a site (or update via `PUT`, which is the uploader's remedy for mistakes).
- Expiry notifications (email/webhook warnings before teardown).
