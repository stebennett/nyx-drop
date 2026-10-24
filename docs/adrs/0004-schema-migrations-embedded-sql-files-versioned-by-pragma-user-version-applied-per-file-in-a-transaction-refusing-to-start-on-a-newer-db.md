---
id: ADR-0004
title: "Schema migrations: embedded SQL files versioned by PRAGMA user_version, applied per-file in a transaction, refusing to start on a newer DB"
status: Accepted
date: 2026-07-10
card: CARD-002
supersedes: []
superseded_by: ""
---

# ADR-0004: Schema migrations: embedded SQL files versioned by PRAGMA user_version, applied per-file in a transaction, refusing to start on a newer DB

## Context

The spec ("Schema migrations") requires embedded SQL migrations that run at startup,
are versioned via `PRAGMA user_version`, each applied in a transaction, with the app
refusing to start if the on-disk version is newer than the binary knows. No ORM or
migration library is permitted by the dependency allowlist. Schema version and the
store's query set must stay in lockstep and land atomically (the slice verdict's
shared invariant).

## Decision

We will ship SQL files under `internal/store/migrations/` (`0001_init.sql`, …)
embedded via `embed.FS` and applied in filename order. The runner reads
`PRAGMA user_version` → `v`; if `v` exceeds the highest embedded migration number it
returns a sentinel `ErrSchemaTooNew` and `Open()` fails (fail-fast startup). For each
file numbered `n > v` it opens a transaction, executes the file body, sets
`PRAGMA user_version = n` (literal int, not a bound param), and commits — so a failed
migration rolls back both DDL and the version bump. Re-running against an up-to-date
DB applies nothing (idempotent).

## Status

Accepted

## Consequences

Easier: zero migration dependencies; deterministic, transactional, idempotent startup
migration; a stale binary against a future DB is caught loudly instead of corrupting
data.

Harder: migrations are forward-only (no down migrations — out of scope for v1); each
future schema change is a new numbered file plus a matching query-set change in the
same card; multi-statement migration files rely on `modernc` executing all statements
in one `Exec`, which the implementation must confirm (else split statements in the
runner).
