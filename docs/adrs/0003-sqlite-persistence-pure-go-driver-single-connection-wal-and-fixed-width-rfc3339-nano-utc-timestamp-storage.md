---
id: ADR-0003
title: "SQLite persistence: pure-Go driver, single-connection WAL, and fixed-width RFC3339-nano UTC timestamp storage"
status: Accepted
date: 2026-07-10
card: CARD-002
supersedes: []
superseded_by: ""
---

# ADR-0003: SQLite persistence: pure-Go driver, single-connection WAL, and fixed-width RFC3339-nano UTC timestamp storage

## Context

CARD-002 introduces the only datastore. The dependency allowlist mandates a CGO-free
driver so the binary runs in distroless/scratch. The process is single-replica on an
RWO volume (spec: no horizontal scaling), so writes are single-writer, but the
lock-free serve path (CARD-003) reads concurrently with writes and the reaper. The
store must never call `time.Now()` (invariant 9) and all timestamps are UTC
(invariant 5), stored so that both exact round-trip and lexicographic `ORDER BY` /
`expires_at` comparisons hold. `modernc.org/sqlite` sets pragmas per-connection via
DSN query params and does not connect until first use.

## Decision

We will use `modernc.org/sqlite` (driver name `sqlite`). We open one DB at
`<DATA_DIR>/drop.db` with DSN pragmas `journal_mode(WAL)`, `busy_timeout(5000)`,
`foreign_keys(ON)`, `synchronous(NORMAL)`, and `db.SetMaxOpenConns(1)`; `Open()`
`PingContext`s to force the connection and apply pragmas at startup.
`MaxOpenConns(1)` removes `SQLITE_BUSY` entirely for the single-writer process
(`busy_timeout` stays as belt-and-suspenders); WAL lets the future serve path read
without blocking writers; `foreign_keys(ON)` is future-proofing; `synchronous(NORMAL)`
is the safe WAL default.

The store is Clock-free: business times enter as explicit parameters. Timestamps are
stored as TEXT in fixed-width RFC 3339 with 9-digit fractional seconds, normalized to
UTC (format `"2006-01-02T15:04:05.000000000Z07:00"`), and parsed back with
`RFC3339Nano` — chosen over plain `RFC3339` (lossy) and `RFC3339Nano`-formatting
(variable width breaks ordering).

## Status

Accepted

## Consequences

Easier: no cgo/toolchain, cheap real-SQLite tests in `t.TempDir()`; a whole class of
lock errors is designed out; timestamps sort and round-trip correctly.

Harder: `MaxOpenConns(1)` serializes all DB access — acceptable for the stated
low-traffic single replica, and `02-database.md` warns not to raise it without
measuring; horizontal scaling is out of scope (spec) and would require revisiting
WAL/single-writer; `:memory:` is unusable for WAL so tests must use real files.
