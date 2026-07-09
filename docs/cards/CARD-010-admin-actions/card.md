---
id: CARD-010
type: feature
layer: domain
title: "Admin actions: permanent, delete, download"
status: backlog
phase: backlog
right_sized: ""
depends_on: [CARD-005, CARD-009]
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
The admin curates: keeps sites forever, removes them, exports them. Adds permanent
set/unset with rescue semantics (extending CARD-005's reaper re-check), immediate
delete, snapshot-then-stream download, the UI actions with confirm, and
`sites_deleted_total`. Mirrors slice S10.

## Acceptance criteria
- [ ] `POST`/`DELETE /api/admin/sites/{id}/permanent` round-trips and is idempotent; unset sets `expires_at = now + TTL` (a full fresh TTL, never instant expiry) (spec "Permanence semantics", "Admin API")
- [ ] Rescue race: making an expired-but-unreaped site permanent before the reaper tick saves it (reaper re-checks under the per-site lock); if the reaper already won, the action → 404 (spec "Expired-but-not-yet-reaped", "Lifecycle")
- [ ] `DELETE /api/admin/sites/{id}` removes files and row; the site host 404s immediately; repeat delete → 404 (spec "Admin API")
- [ ] `GET /api/admin/sites/{id}/download` snapshots the site to a zip under `/data/tmp` then streams it (`Content-Disposition: attachment`), deleting it after; a mid-download reap/update can never truncate the archive (per-site lock), reaped-before-snapshot → clean 404; cross-origin `Origin` on state-changing actions → 403 (spec "Download", "CSRF protection")

## Notes
UI: per-row permanent toggle (lifetime-moon component per 04-frontend.md), download,
delete-with-confirm.
