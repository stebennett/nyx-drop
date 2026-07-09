---
id: CARD-002
type: task
layer: db
title: SQLite store & migrations
status: backlog
phase: backlog
right_sized: ""
depends_on: [CARD-001]
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
No user-visible behavior; the one foundation everything after consumes. Complete
`internal/store` per `docs/implementation/02-database.md` — schema, embedded
migrations, and all store methods including List — plus a `/healthz` that really
checks the DB. Mirrors slice S2 (the deliberate thin-horizontal exception).

## Acceptance criteria
- [ ] Embedded SQL migrations run at startup, versioned via `PRAGMA user_version`, each applied in a transaction; the app refuses to start if the on-disk version is newer than the binary knows (spec "Schema migrations")
- [ ] `sites` table matches the spec's columns exactly; store methods incl. List with pagination/sort/search and `LIKE`-wildcard escaping pass the 02-database.md test list (spec "Storage & data model", "Admin — Listing")
- [ ] `/healthz` returns 200 only when the SQLite DB answers a ping and `DATA_DIR` is writable; 503 otherwise (spec "Deployment — Helm chart probes")
- [ ] SQLite opened with WAL mode and busy timeout via the pure-Go driver `modernc.org/sqlite` (spec "Error handling"; 00-overview dependency allowlist)

## Notes
See 02-database.md for schema, queries, and the store test list. UTC everywhere;
DB-row-is-authority invariant starts here.
