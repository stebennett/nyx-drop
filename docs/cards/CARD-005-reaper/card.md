---
id: CARD-005
type: feature
layer: domain
title: "Reaper: TTL expiry & startup sweep"
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
Sites must actually disappear on schedule and the system must self-heal across
restarts. `internal/reaper`: startup run + minutely tick, orphan/tmp sweeps,
lifecycle metrics and logs. Mirrors slice S5.

## Acceptance criteria
- [ ] With a fake clock advanced past TTL, a tick deletes the DB row first, then the site directory, for every `permanent = false AND expires_at <= now` site; permanent rows are untouched (spec "Lifecycle — the reaper")
- [ ] Each deletion takes the per-site lock and re-checks the `permanent` flag before deleting (the admin-rescue hook) (spec "Lifecycle", "Expired-but-not-yet-reaped")
- [ ] Startup sweep reaps sites that expired while the pod was down and removes stale tmp dirs, site dirs with no DB row, and rows with no site dir (spec "Atomic creation", "Lifecycle")
- [ ] An error reaping one site is logged and doesn't block the others; `sites_reaped_total`, `reap_errors_total`, and the `sites_active`/`sites_permanent` gauges update; lifecycle log events carry the site id (spec "Error handling", "Observability")

## Notes
Expiry boundary is `now >= expires_at`. TTL is not retroactive — stored expiries are
never recomputed on redeploy (spec "Lifecycle").
