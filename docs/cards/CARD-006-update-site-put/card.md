---
id: CARD-006
type: feature
layer: domain
title: Update site in place (PUT, timer reset)
status: backlog
phase: backlog
right_sized: ""
depends_on: [CARD-003]
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
Stable URLs for CI use: create a preview site once, then `PUT` to the same id on
every push. Adds `PUT /api/sites/{id}`, the atomic directory swap under the per-site
lock, `store.Touch`, and `sites_updated_total`. Mirrors slice S6.

## Acceptance criteria
- [ ] `PUT /api/sites/{id}` with `UPLOAD_TOKEN` (zip or files mode) replaces the site's contents entirely at the same URL; `expires_at = now + TTL` and `updated_at = now` on every successful PUT; a permanent site stays permanent (spec "Updating a site")
- [ ] The swap is atomic (extract+validate in tmp, rename old aside, rename new in, delete old) under the per-site lock; parallel PUTs to one id serialize last-write-wins — race test passes under `-race` (spec "Per-site concurrency")
- [ ] Unknown id → 404; expired-but-not-yet-reaped id → 404 (expired means gone) (spec "Updating a site")
- [ ] All creation-time extraction rules and safety limits apply; response is 200 with the creation JSON shape (spec "Updating a site")

## Notes
Grant-on-PUT rejection is CARD-007's criterion (grants don't exist yet). The
expired-unreaped 404 test needs only the injected clock, not the reaper.
