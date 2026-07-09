---
id: CARD-007
type: feature
layer: api
title: Credential-free web upload page (upload grants)
status: backlog
phase: backlog
right_sized: ""
depends_on: [CARD-003, CARD-004]
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
A person drags a zip or a folder onto a page and gets their URL — no token, no
sign-in. Adds `GET /api/upload-grant` + `auth.GrantStore`, `requireUploadAuth`
credential dispatch, the upload page frontend, embedded static serving, and the
branded `notfound.html`. Mirrors slice S7.

## Acceptance criteria
- [ ] `GET /api/upload-grant` issues a `SESSION_SECRET`-signed grant (`{use: "upload", exp, jti}`), valid 15 minutes and single-use (consumed `jti`s tracked in memory until expiry); `POST /api/sites` with a grant → 201 with `source=web`, with the token → `source=api` (spec "Upload grants")
- [ ] Expired, reused, and tampered grants → 401; a grant on `PUT /api/sites/{id}` → 401 (grants are create-only) (spec "Upload grants")
- [ ] Upload page at `/`: drag-and-drop zip or folder (recursive walk, relative paths as slash-containing filenames), fetches a fresh grant before each upload, XHR upload with progress bar, resulting URL shown with copy button, one automatic grant-refresh retry on 401 (spec "Upload page")
- [ ] Frontend embedded via `embed.FS`, self-contained (zero external requests), served at `/` and `/static/*`; branded `notfound.html` replaces any placeholder 404 (spec "Admin UI" embedding; 00-overview invariants; 04-frontend.md)

## Notes
Follow the "dusk" design system and upload-page behavior in 04-frontend.md and
`docs/implementation/ui-mockups/`. Grants are abuse friction, not authentication
(spec "Honest framing") — no rate limiting in v1.
