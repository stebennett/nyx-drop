---
id: CARD-003
type: feature
layer: domain
title: Create site via API (zip) and serve it
status: backlog
phase: backlog
right_sized: ""
depends_on: [CARD-001, CARD-002]
branch: ""
worktree: ""
design_pr_url: ""
pr_url: ""
adrs: []
reworks: 0
started: ""
delivered: ""
created: 2026-07-09
---

## Why
The product exists after this card: a CI job POSTs a zip with the upload token and
gets back a URL that serves the site. Covers `internal/slug`, `internal/extract`
(zip path, all rules), `internal/lock`, `POST /api/sites` with token middleware,
and site-host static serving. Mirrors slice S3.

## Acceptance criteria
- [ ] `POST /api/sites` with `Authorization: Bearer <UPLOAD_TOKEN>` and a zip → 201 `{id, url, expires_at}` (RFC 3339 UTC); slug is `<adjective>-<animal>-<6-char a-z0-9 suffix>`, a valid DNS label, with collision retry bounded at 5 then 500 (spec "API", "Slugs")
- [ ] `GET`/`HEAD` on `<slug>.<BASE_DOMAIN>` serves the extracted files (index.html for directories, `X-Content-Type-Options: nosniff`, ETag/Range via `http.ServeContent`); expired-but-unreaped site → 404 page under an injected fake clock; other methods → 405 (spec "Static serving", "API conventions")
- [ ] Full creation error table: path traversal, absolute paths, backslashes, control chars, non-UTF-8 names, duplicate paths, empty-after-dotfile-filtering → 400; bad/missing token → 401; `MAX_UPLOAD_SIZE`/`MAX_SITE_SIZE`/`MAX_FILE_COUNT` exceeded → 413/400 as specced; symlinks skipped; sole-top-level-directory stripped once (spec "Extraction rules", "Upload safety")
- [ ] Atomic creation: extract to `/data/tmp` (same filesystem), validate, insert row, rename into `/data/sites/<id>`; `ENOSPC` mid-extraction cleans temp and returns structured 500 (spec "Atomic creation")

## Notes
Lowest substantive layer is domain (slug/extract/lock) even though it exposes an API
endpoint and static serving. Word lists embedded, lowercase ASCII, DNS-safe.
