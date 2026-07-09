---
id: CARD-009
type: feature
layer: api
title: "Admin site list: API + UI (paging, sort, search)"
status: backlog
phase: backlog
right_sized: ""
depends_on: [CARD-003, CARD-008]
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
The admin sees all running sites with paging, sorting, and search. Adds
`GET /api/admin/sites` (validation, pagination, sort, escaped search, expired flag)
and the `admin.html`/`admin.js` table per 04-frontend.md. Mirrors slice S9.

## Acceptance criteria
- [ ] `GET /api/admin/sites?page&per_page&sort=name|created|expires&order&q` per the 03-api-reference.md contract, defaults `page=1, per_page=50, sort=created, order=desc`, response includes `total`; permanent sites sort last under `sort=expires`; `q` is a case-insensitive substring match with SQL `LIKE` wildcards escaped (spec "Listing")
- [ ] Unknown `sort`/`order`, non-positive `page`/`per_page` → 400; `per_page` capped at 200 (spec "Query validation")
- [ ] Expired-but-not-yet-reaped sites appear in the list flagged as expired (spec "Expired-but-not-yet-reaped")
- [ ] Admin table UI: slug linked to live site, created/updated/expires-or-permanent/size/file-count/source columns, all three sortable columns, search box wired to `q`, pager driven by `total`; expired rows badged (spec "Admin UI"; 04-frontend.md)

## Notes
Store-level List (with escaping) landed in CARD-002; this card wires the HTTP
contract and UI on top.
