# API Reference

Complete request/response contract. `$BASE` = `SCHEME://BASE_DOMAIN` (examples use
`https://sites.nyxhub.net`). All errors: `{"error": "<message>"}` with the status shown.
All timestamps: RFC 3339 UTC.

## Host routing summary

| Host | Surface |
|---|---|
| `sites.nyxhub.net` | Everything below |
| `<slug>.sites.nyxhub.net` | Static site serving (GET/HEAD only; others → 405) |
| any other | HTML 404 page |

`/healthz` and `/metrics` answer on **any** host, before routing.

---

## Public API

### Upload grant — `GET /api/upload-grant`

Unauthenticated. Mints a short-lived credential for browser uploads (see the spec's
"Upload grants" section: 15-minute expiry, single-use, signed with `SESSION_SECRET`).

**200:**
```json
{ "grant": "eyJ1c2UiOiJ1cGxvYWQi...·MEQCIF...", "expires_at": "2026-07-09T12:15:00Z" }
```

### Create — `POST /api/sites`

Auth: `Authorization: Bearer <credential>` where the credential is either the
`UPLOAD_TOKEN` (recorded as `source: "api"`) or a valid unused upload grant
(`source: "web"`). Resolution order: constant-time compare against `UPLOAD_TOKEN`
first; otherwise validate as a grant (signature → expiry → not-yet-consumed, then mark
consumed); otherwise 401.

```
curl -X POST https://sites.nyxhub.net/api/sites \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@dist.zip"                          # zip mode
# or files mode:
curl -X POST https://sites.nyxhub.net/api/sites \
  -H "Authorization: Bearer $TOKEN" \
  -F "files=@index.html;filename=index.html" \
  -F "files=@app.css;filename=assets/app.css"
```

Body rules: field `file` (exactly one, a zip) **or** field(s) `files` (one or more,
relative paths in `filename`). Both present, neither present, or extra unknown file
fields → 400.

**201:**
```json
{
  "id": "trusty-tahr-x7k2mq",
  "url": "https://trusty-tahr-x7k2mq.sites.nyxhub.net",
  "expires_at": "2026-07-10T12:00:00Z"
}
```

| Status | When | Example message |
|---|---|---|
| 400 | not multipart; both/neither of `file`/`files`; invalid zip; malformed path (traversal, `..`, backslash, control char, non-UTF-8, duplicate); empty after filtering | `"upload contains no files"` |
| 401 | missing/wrong token; invalid, expired, or already-used grant | `"invalid or expired upload credential"` |
| 413 | body > `MAX_UPLOAD_SIZE`; uncompressed > `MAX_SITE_SIZE`; > `MAX_FILE_COUNT` files | `"site exceeds maximum uncompressed size"` |
| 500 | slug retries exhausted; ENOSPC; other internal | `"internal error"` |

### Update — `PUT /api/sites/{id}`

Auth: `UPLOAD_TOKEN` **only** — an upload grant on `PUT` is 401 (grants must not allow
overwriting existing sites). Same body rules and error set as create, plus:

| Status | When |
|---|---|
| 404 | unknown id, **or** site expired but not yet reaped |

**200:** same shape as create (fresh `expires_at = now + TTL`). `updated_at` and
`expires_at` are reset on every successful PUT even if the site is permanent.

---

## Auth endpoints (no token)

| Route | Behavior |
|---|---|
| `GET /auth/login` | Set `__Host-oauth-state` cookie (signed, 10 min); 302 to GitHub authorize URL with `client_id`, `redirect_uri=$BASE/auth/callback`, `state` |
| `GET /auth/callback?code&state` | Verify state cookie ↔ param (mismatch/expired → 403 HTML); exchange code; `GET /user` for login; login ≠ `ADMIN_GITHUB_USER` (case-insensitive) → 403 "not authorized" HTML page, no session; else set `__Host-session`, 302 → `/admin` |
| `POST /auth/logout` | Requires session + Origin check; clear cookie (`Max-Age=0`); 302 → `/` |

## Admin API (cookie `__Host-session`)

Auth failure on `/api/admin/*` → **401 JSON**. On `/admin` (page) → **302 `/auth/login`**.
State-changing methods (POST/DELETE) also enforce the Origin check → 403 on mismatch.

### List — `GET /api/admin/sites`

Query params (invalid values → 400 `"invalid query parameter: <name>"`):

| Param | Default | Constraints |
|---|---|---|
| `page` | 1 | integer ≥ 1 |
| `per_page` | 50 | 1–200 |
| `sort` | `created` | `name` \| `created` \| `expires` |
| `order` | `desc` | `asc` \| `desc` |
| `q` | — | literal substring match on slug (case-insensitive) |

**200:**
```json
{
  "sites": [
    {
      "id": "trusty-tahr-x7k2mq",
      "url": "https://trusty-tahr-x7k2mq.sites.nyxhub.net",
      "created_at": "2026-07-09T09:30:00Z",
      "updated_at": "2026-07-09T11:02:13Z",
      "expires_at": "2026-07-10T11:02:13Z",
      "permanent": false,
      "expired": false,
      "size_bytes": 1048576,
      "file_count": 12,
      "source": "api"
    }
  ],
  "page": 1,
  "per_page": 50,
  "total": 123
}
```

`expired` is computed at response time (`now >= expires_at && !permanent`) — it flags
expired-but-not-yet-reaped rows for the UI.

### Permanence

| Route | Success | Notes |
|---|---|---|
| `POST /api/admin/sites/{id}/permanent` | 200 `{"id":…, "permanent":true}` | Works on expired-unreaped rows (rescue) |
| `DELETE /api/admin/sites/{id}/permanent` | 200 `{"id":…, "permanent":false, "expires_at":"<now+TTL>"}` | |

Both: 404 if the row is gone (reaper won). Setting an already-set flag is idempotent 200.

### Download — `GET /api/admin/sites/{id}/download`

200 with `Content-Type: application/zip`,
`Content-Disposition: attachment; filename="<id>.zip"`. Zip contains the site's files
with site-root-relative paths. 404 if gone.

### Delete — `DELETE /api/admin/sites/{id}`

204 on success (row + files removed). 404 if already gone.

---

## Site serving — `GET|HEAD <slug>.BASE_DOMAIN/<path>`

| Condition | Response |
|---|---|
| slug unknown, or expired-and-not-permanent | Branded HTML 404 page (status 404) |
| path is a directory containing `index.html` | that file, 200 |
| path missing | plain 404 |
| otherwise | file, 200, correct `Content-Type`, ETag/conditional/Range support |
| any non-GET/HEAD method | 405 |

Every site response carries `X-Content-Type-Options: nosniff`.

## Infrastructure endpoints

| Route | Response |
|---|---|
| `GET /healthz` | 200 `ok` when DB pings and data dir is writable; 503 otherwise |
| `GET /metrics` | Prometheus text format |
