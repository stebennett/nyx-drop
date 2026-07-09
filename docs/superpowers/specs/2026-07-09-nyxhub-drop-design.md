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
| `expires_at` | TIMESTAMP | `created_at + TTL` at creation |
| `permanent` | BOOLEAN | Default false |
| `size_bytes` | INTEGER | Total extracted size |
| `file_count` | INTEGER | |
| `source` | TEXT | `api` or `web` |

**Slugs** are readable, Ubuntu-release-style names in the spirit of [bschiffthaler/mkname](https://github.com/bschiffthaler/mkname): a random **alliterative adjective + animal pair** (both words share the same first letter, like Ubuntu's "Trusty Tahr" or "Jaunty Jackalope") followed by a random uniqueness suffix. Format:

```
<adjective>-<animal>-<suffix>     e.g.  trusty-tahr-x7k2mq
```

- Adjective and animal come from embedded word lists (lowercase ASCII, DNS-safe); the animal is chosen first, then an adjective sharing its first letter.
- The suffix is 6 characters of lowercase alphanumeric (`a-z0-9`), generated from a cryptographically secure source, and guarantees uniqueness — the word pair is for readability only.
- The whole slug must remain a valid DNS label (≤ 63 chars; word lists are curated to keep well under this).
- On collision (unique constraint violation on insert), regenerate the suffix and retry.

**Permanence semantics:** `permanent = true` means the reaper ignores `expires_at`. Unsetting permanence sets `expires_at = now + TTL`, so the site gets a full TTL from the moment of unsetting rather than vanishing instantly.

**Atomic creation:** uploads are extracted into a temp directory under `/data/tmp` (same filesystem), validated, then renamed into `/data/sites/<id>` and the DB row inserted. A site is never servable in a half-written state. Stale temp directories are removed on startup.

## Site creation

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

Errors: `401` bad/missing token, `413` size limits exceeded, `400` invalid archive / no files / path traversal detected.

### Upload page

Served at `https://<BASE_DOMAIN>/`. Drag-and-drop of a zip file or a folder of files. Prompts for the upload token (remembered in `localStorage`), calls `POST /api/sites`, displays the resulting site URL with a copy button.

### Upload safety

- `MAX_UPLOAD_SIZE` — request body limit.
- `MAX_SITE_SIZE` — total **uncompressed** size limit (zip-bomb guard, enforced during extraction).
- `MAX_FILE_COUNT` — per-site file count limit.
- Zip entries are sanitized; any entry resolving outside the site root (path traversal, absolute paths, `..`) rejects the whole upload with `400`.
- Symlinks in archives are skipped.

Every site receives the single global TTL; there is no per-site TTL override.

## Admin

### Authentication

GitHub OAuth (authorization code flow). `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` from config; callback URL `https://<BASE_DOMAIN>/auth/callback`. After the OAuth exchange, the authenticated GitHub login must equal `ADMIN_GITHUB_USER`; any other user receives `403`. On success a signed session cookie (HMAC with `SESSION_SECRET`, `HttpOnly`, `Secure`, 7-day expiry) is issued. All admin endpoints require a valid session.

### Admin UI

Served at `https://<BASE_DOMAIN>/admin` (embedded static frontend compiled into the binary via `embed.FS`; plain HTML/JS, no build-time framework). Table of all sites: slug (linked to the live site), created, expires (or "permanent"), size, file count, source. Per-site actions: toggle permanent, download zip, delete (with confirm).

### Admin API

| Endpoint | Method | Behavior |
|---|---|---|
| `/api/admin/sites` | GET | List all sites with metadata |
| `/api/admin/sites/{id}/permanent` | POST | Set permanent |
| `/api/admin/sites/{id}/permanent` | DELETE | Unset permanent; `expires_at = now + TTL` |
| `/api/admin/sites/{id}/download` | GET | Stream a zip of the site's files |
| `/api/admin/sites/{id}` | DELETE | Delete site immediately (files + row) |

## Lifecycle — the reaper

A background goroutine runs at startup and then every minute:

1. Query sites where `permanent = false AND expires_at < now`.
2. For each: delete the DB row, then remove the site directory.

Deleting the row first guarantees an expired site stops being served immediately; a startup sweep removes any orphaned directories (no matching row), covering a crash between the two steps. Because metadata (including the permanent flag) lives in SQLite on the PVC, permanence survives pod and node restarts. Sites that expired while the pod was down are reaped by the startup run.

## Static serving

For requests to `<slug>.<BASE_DOMAIN>`:

1. Look up the slug in SQLite. Missing, or expired-but-not-yet-reaped → 404 page.
2. Serve files rooted at `/data/sites/<slug>` with path-traversal-safe resolution.
3. Directory paths serve their `index.html`; missing files → plain 404.
4. Standard content types and ETag/conditional-request handling via Go's `http.ServeContent`/`http.ServeFile`.

No SPA fallback rewriting in v1.

## Configuration (environment variables)

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BASE_DOMAIN` | yes | — | e.g. `sites.nyxhub.net` |
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

The app fails fast at startup if any required variable is missing.

## Deployment

- **Image:** multi-stage Dockerfile; final stage distroless (`gcr.io/distroless/static`), single static binary with embedded frontend assets. Runs as non-root.
- **Helm chart** with:
  - Deployment — 1 replica, `strategy: Recreate` (RWO volume), liveness/readiness probes on `GET /healthz`, resource requests/limits via values.
  - Service (ClusterIP).
  - PVC — size and storageClass via values.
  - Ingress — hosts `<BASE_DOMAIN>` and `*.<BASE_DOMAIN>`, ingress class, TLS secret name, and extra annotations all via values.
  - Secrets — `UPLOAD_TOKEN`, `GITHUB_CLIENT_SECRET`, `SESSION_SECRET` via an `existingSecret` reference or inline values.

## Error handling

- Upload validation failures return structured JSON errors with appropriate status codes (`400`, `401`, `413`).
- Extraction failures clean up their temp directory; nothing partial reaches `/data/sites`.
- Reaper errors on one site are logged and don't block other deletions.
- SQLite is opened with WAL mode and busy timeout; all writes go through a single connection pool in the one process.

## Testing

- **Unit:** slug generation (alliteration invariant, DNS-label validity, suffix charset, collision retry), zip extraction (happy path, traversal attempts, absolute paths, symlinks, size/count limits), store operations, reaper logic with an injected fake clock.
- **Integration:** `httptest`-based tests exercising the full flow — upload (zip and multi-file) → serve → expire → 404; host routing (apex vs slug vs unknown); auth failures (missing/wrong upload token, no session, wrong GitHub user).
- **OAuth:** GitHub endpoints stubbed with a local test server (configurable OAuth base URL internally to allow this).

## Out of scope (v1)

- Per-site TTL overrides.
- Horizontal scaling / multi-replica (single replica on RWO volume).
- SPA fallback routing.
- Custom domains per site.
- Upload rate limiting (token-gated audience is trusted).
