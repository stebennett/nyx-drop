# CARD-002 — SQLite store & migrations — Design

## Intent

Build `internal/store`: the single persistence layer every later card consumes.
It owns **all** SQL — the `sites` table, embedded schema migrations versioned by
`PRAGMA user_version`, and the nine `Store` methods over one `Site` type — and it
supplies the *real* readiness check that replaces CARD-001's always-nil
`Deps.Health` stub (`/healthz` = DB ping **and** `DATA_DIR` writable). No
user-visible feature ships here; this is the deliberate thin-horizontal foundation
(slice S2), the point at which the **DB-row-is-authority** and **UTC-everywhere**
invariants begin.

## Acceptance criteria

Sharpened from `card.md`; each cites the spec section it enforces.

1. **Migrations run, are versioned, and are safe.** On `store.Open`, embedded SQL
   migrations apply in filename order; version is tracked in `PRAGMA user_version`;
   each migration runs in a transaction (DDL + version bump commit or roll back
   together); if the on-disk `user_version` exceeds the highest embedded migration,
   `Open` returns `ErrSchemaTooNew` and the process refuses to start. Re-running
   `Open` on an up-to-date DB applies nothing. *(spec "Schema migrations")*
2. **Schema matches the spec exactly.** The `sites` table has columns
   `id, created_at, updated_at, expires_at, permanent, size_bytes, file_count,
   source` with the checks in `02-database.md`, plus `idx_sites_reap`. *(spec
   "Storage & data model")*
3. **Every `Store` method behaves per `02-database.md`.** `Create` (`ErrDuplicate`
   on id collision), `Get`/`Touch`/`Delete` (`ErrNotFound` on missing id),
   `SetPermanent(true|false)`, `Expired` (boundary `expires_at == now` **is**
   returned; permanent sites never returned), `Counts`, `Ping`, and `List` with
   pagination/sort/search including `LIKE`-wildcard escaping and permanent-last
   ordering under `sort=expires`. *(spec "Storage & data model", "Admin — Listing")*
4. **Timestamps round-trip in UTC.** A time stored and read back satisfies
   `got.Equal(want)`, and stored strings sort lexicographically in chronological
   order. *(spec "Time semantics"; invariant 5)*
5. **`/healthz` is a real readiness check.** Returns `200` only when the SQLite DB
   answers a ping **and** `DATA_DIR` accepts a write; `503` otherwise. The `503`
   branch is exercised by a test (closing CARD-001's unexercised-branch finding).
   *(spec "Deployment"; "`/healthz` bypasses Host routing")*
6. **SQLite is opened correctly.** Pure-Go `modernc.org/sqlite`, WAL journal mode,
   busy timeout, `MaxOpenConns(1)`. *(spec "Error handling"; `00-overview` allowlist)*

## In scope

- `internal/store` package: `store.go` (types, errors, `Open`, all methods,
  `Readiness`), `migrate.go` (embedded runner), `migrations/0001_init.sql`.
- Adding `modernc.org/sqlite` to `go.mod`/`go.sum` (this card introduces it).
- Replacing the `Deps.Health` stub in `cmd/drop/main.go` with the real
  `store.Open` + `store.Readiness` wiring, including `defer st.Close()` and
  `Open`-error → `run` returns an error.
- Pinning the `/healthz` `200`-vs-`503` contract in `internal/server` tests.

## Out of scope (YAGNI — with the owning card)

- Wiring the store into request handlers / adding a `Store` field to `server.Deps`
  — **CARD-003** (site create/serve) is the first consumer; it declares the
  consumer-side `Store` interface then. This card threads the store only into the
  health closure and `cmd/drop`.
- Slug generation, extraction, locking, the reaper loop, the startup crash-artifact
  sweep — **CARD-003/CARD-004** and the reaper card. `Expired`/`Delete`/`Counts`
  exist here but are unreached until then.
- Down/rollback migrations, a second migration file — future schema-change cards.
- Prometheus `sites_active`/`sites_permanent` gauge *refresh* wiring — the reaper
  card; `Counts` merely exists and is tested here.
- Handler-side `List` query **validation** (400 on bad params) — the store
  whitelists defensively but the 400 contract is **CARD-008/009** (admin API).
- `WriteTimeout` revisit (KNOWLEDGE `[CARD-001]`) — CARD-003/004.

## Dependencies & assumptions

- **Depends on CARD-001** (merged on `main`): `server.Deps` struct with the
  injected `Health HealthFunc` seam; `config.Config.DataDir`; `clock.Clock`;
  `cmd/drop`'s `run(getenv)` wiring. All read from real code.
- **Assumption:** the store never needs the clock — all business times enter as
  explicit method parameters (caller passes `clock.Now()`), so invariant 9 holds
  structurally and the store stays a pure, real-SQLite-testable unit.
- **Assumption:** single-writer process on an RWO volume (spec: no multi-replica),
  which licenses `MaxOpenConns(1)`.
- **Driver quirk relied on:** `modernc.org/sqlite` accepts `_pragma=` DSN query
  params and executes multi-statement `Exec`; the implementation must confirm the
  latter (task 3) and split statements in the runner if it does not.

## Approach

A single concrete `*store.DB` wrapping a `*sql.DB`, constructed by `store.Open`,
which opens the file with the DSN pragmas, sets `MaxOpenConns(1)`, pings, and runs
migrations before returning. Methods are thin, parameterized SQL — no ORM, no SQL
outside this package. Times cross the boundary as `time.Time` and are encoded to a
fixed-width UTC string internally.

**Migration mechanism (see proposed ADR).** Embedded `migrations/*.sql`, applied in
filename order, versioned by `PRAGMA user_version`, each file in its own
transaction, refuse-start when the DB is newer. Chosen over: (a) a migration
library — barred by the allowlist; (b) `CREATE TABLE IF NOT EXISTS` with no version
ledger — cannot detect a newer DB or sequence future migrations, and drifts silently
from the query set; (c) an app-managed `schema_migrations` table — reinvents
`user_version`, which SQLite gives us free and transactionally.

**SQLite connection (see proposed ADR).** `modernc.org/sqlite`, one file,
DSN `?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)`,
`SetMaxOpenConns(1)`. Rejected: raising `MaxOpenConns` (reintroduces `SQLITE_BUSY`
for no benefit at this traffic); `cgo` `mattn/go-sqlite3` (breaks distroless/scratch,
off-allowlist); default rollback-journal mode (no reader/writer concurrency for the
future lock-free serve path).

**Timestamp encoding (see proposed ADR + flagged discrepancy).** Fixed-width
RFC3339 with 9 fractional digits, UTC. `02-database.md` says "RFC 3339 UTC strings"
and relies on lexicographic sort; taken literally as `time.RFC3339` it loses
sub-second precision (the round-trip `got.Equal(want)` test fails on a
nanosecond-precision clock value), and `time.RFC3339Nano` trims trailing zeros to
variable width, which breaks the very lexicographic ordering the doc depends on
(`"…05.1Z"` sorts before `"…05Z"` though it is chronologically later). The
fixed-width nano form satisfies both and is still valid RFC 3339 (the **spec** only
says "RFC 3339"/"UTC"), so this refines `02-database.md`, not the spec — flagged
under Spec references.

**Store interface placement.** Per `01-architecture.md` ("define interfaces where
consumed") and the CARD-001 `server.Deps` precedent, this card ships the concrete
`*store.DB` and the value/error types; the `Store` *interface* is declared at its
consumption site by CARD-003. `Readiness` takes `*store.DB` concretely and is tested
with a real closed DB — no interface or mock needed (matches the "do not mock the
store" testing rule).

## Interfaces

All in `package store` (`internal/store`).

```go
// Site is the single site record, used everywhere. Mirrors 01-architecture.md.
type Site struct {
    ID        string
    CreatedAt time.Time
    UpdatedAt time.Time
    ExpiresAt time.Time
    Permanent bool
    SizeBytes int64
    FileCount int
    Source    string // "api" | "web"
}

// ListParams — sanitized by the caller (handler validates & 400s); the store
// whitelists Sort/Order defensively.
type ListParams struct {
    Page    int    // >= 1
    PerPage int    // 1..200
    Sort    string // "name" | "created" | "expires"
    Order   string // "asc" | "desc"
    Query   string // raw substring; empty = no filter
}

// Sentinel errors crossing the package boundary (errors.Is at call sites).
var (
    ErrNotFound     = errors.New("store: site not found")
    ErrDuplicate    = errors.New("store: duplicate site id")
    ErrSchemaTooNew = errors.New("store: database schema is newer than this binary")
)

// DB is the concrete SQLite-backed store. Methods are safe for concurrent use.
type DB struct { db *sql.DB }

// Open opens <dataDir>/drop.db with the WAL/busy_timeout/foreign_keys DSN and
// MaxOpenConns(1), pings, and runs embedded migrations. Returns ErrSchemaTooNew
// (wrapped) if the on-disk user_version is ahead of this binary.
func Open(dataDir string) (*DB, error)
func (d *DB) Close() error

func (d *DB) Create(ctx context.Context, s Site) error
func (d *DB) Get(ctx context.Context, id string) (Site, error)
func (d *DB) List(ctx context.Context, p ListParams) (items []Site, total int, err error)
func (d *DB) SetPermanent(ctx context.Context, id string, v bool, newExpiry time.Time) error
func (d *DB) Touch(ctx context.Context, id string, updatedAt, expiresAt time.Time, size int64, files int) error
func (d *DB) Delete(ctx context.Context, id string) error
func (d *DB) Expired(ctx context.Context, now time.Time) ([]string, error)
func (d *DB) Counts(ctx context.Context) (active, permanent int, err error)
func (d *DB) Ping(ctx context.Context) error

// Readiness returns a func(context.Context) error compatible with
// server.HealthFunc: nil iff the DB answers a ping AND dataDir accepts a write.
func Readiness(d *DB, dataDir string) func(context.Context) error

// migrate.go (unexported)
//go:embed migrations/*.sql
// var migrationsFS embed.FS
// func migrate(db *sql.DB) error   // called by Open
```

The full nine-method set is the documented **contract** `*store.DB` fulfils (the
`Store` interface in `01-architecture.md`); the interface type itself is declared by
the first consumer (CARD-003).

**Threading into `server.Deps` and `cmd/drop`.** `server.Deps` gains **no** field
this card — `Deps.Health` already exists. In `cmd/drop/main.go` `run`:

```go
st, err := store.Open(cfg.DataDir)
if err != nil { return fmt.Errorf("store: %w", err) }
defer st.Close()
// ...
handler, err := server.New(server.Deps{
    // ...unchanged CARD-001 fields...
    Health: store.Readiness(st, cfg.DataDir),
})
```

## Data flow

```
main.run
  └─ store.Open(cfg.DataDir)
       ├─ sql.Open("sqlite", <dataDir>/drop.db?_pragma=…)   // no connection yet
       ├─ db.SetMaxOpenConns(1)
       ├─ db.PingContext(ctx)                                // force conn + pragmas
       └─ migrate(db): user_version → apply n>v in tx each → set user_version=n
  └─ store.Readiness(st, cfg.DataDir) ─┐
                                        └─ Deps.Health ── /healthz handler (200/503)

request path (later cards): handler → clock.Now() → store method (times in) → SQL
```

Time encoding lives entirely inside the store: on write, `t.UTC().Format(tsLayout)`
where `tsLayout = "2006-01-02T15:04:05.000000000Z07:00"`; on read,
`time.Parse(time.RFC3339Nano, s)`. `Expired`/`List` comparisons and `ORDER BY`
operate on these fixed-width strings, so lexical order = chronological order.

**Schema/migration impact.** New DB file and `sites` table + `idx_sites_reap` via
`0001_init.sql`; `user_version` goes `0 → 1`. No change to any existing table (none
exist). No data migration.

## Implementation task list

Each step is one red→green→commit cycle. Paths absolute under the worktree
`/Users/stevebennett/Code/nyx-drop-worktrees/CARD-002`. Gate commands after every
task: `go test ./...`, `go vet ./...`, `gofmt -l .`.

1. **Pin the `/healthz` 503/200 contract (no store dep — do first).**
   Modify `internal/server/server_test.go`: give the test helper an injectable
   `Health` (e.g. add `newTestServerWithHealth(t, logWriter, health)` and have
   `newTestServer` delegate with a nil-returning func). Add `TestHealthz_Ready_200`
   (health returns nil → 200, body `"ok"`) and `TestHealthz_NotReady_503` (health
   returns `errors.New("db down")` → 503, empty body). Run → red (503 test asserts a
   branch never before exercised) → confirm green → commit.
2. **Introduce the driver.** `go get modernc.org/sqlite`; add
   `internal/store/store_driver_test.go` with `TestDriverOpensInTempDir`: `sql.Open`
   a `drop.db` in `t.TempDir()`, `PingContext`, assert nil. Run → red (dep absent) →
   `go mod tidy` → green → commit. (Confirms the pure-Go driver links.)
3. **Migration runner + `0001_init.sql`.** Create
   `internal/store/migrations/0001_init.sql` (schema from `02-database.md`),
   `internal/store/migrate.go` with the embed + `migrate(db)`. Test
   `internal/store/migrate_test.go` `TestMigrate_FreshReachesV1`: open raw db, run
   `migrate`, assert `PRAGMA user_version == 1` and `sites` table exists (query
   `PRAGMA table_info(sites)` column set). Confirm multi-statement `Exec` works here;
   if not, split statements in the runner. Red → implement → green → commit.
4. **Migration idempotency + refuse-newer.** Add
   `TestMigrate_RerunIsNoop` (run twice; version stays 1; no error) and
   `TestMigrate_FutureVersionRefused` (`PRAGMA user_version = 999`; `migrate` returns
   `ErrSchemaTooNew` via `errors.Is`). Red → implement the `v > highest` guard →
   green → commit.
5. **`Open`/`Close` + DSN pragmas.** Create `internal/store/store.go` with `DB`,
   `Open`, `Close`. Test `TestOpen_AppliesPragmas`: `Open` a temp dir, then query
   `PRAGMA journal_mode` == `"wal"`, `PRAGMA foreign_keys` == `1`,
   `PRAGMA busy_timeout` == `5000`; assert `user_version == 1`. `TestOpen_BadDir`:
   non-existent parent → error. Red → implement → green → commit.
6. **`Create`/`Get` round-trip + errors + timestamp encoding.** Define `Site`,
   `tsLayout`, encode/decode helpers, `ErrNotFound`, `ErrDuplicate`.
   `TestCreateGet_RoundTrip`: create with a known nanosecond UTC time, `Get`, assert
   all fields incl. `got.CreatedAt.Equal(want)` and `.Location() == time.UTC`.
   `TestCreate_DuplicateID` (`errors.Is(err, ErrDuplicate)`).
   `TestGet_Missing` (`ErrNotFound`). Red → implement (map
   `SQLITE_CONSTRAINT_PRIMARYKEY` via `errors.As`+`*sqlite.Error`) → green → commit.
7. **`Touch`.** `TestTouch_UpdatesFields` (re-`Get`, assert updated_at/expires_at/
   size/file_count changed; created_at unchanged), `TestTouch_Missing`
   (`ErrNotFound`, 0 rows). Red → green → commit.
8. **`SetPermanent`.** `TestSetPermanent_True` (permanent flips true; expires_at
   unchanged), `TestSetPermanent_False_ResetsExpiry` (permanent false; expires_at ==
   passed `newExpiry`). Red → green → commit.
9. **`Delete`.** `TestDelete_RemovesRow` (`Get` after → `ErrNotFound`),
   `TestDelete_Missing` (`ErrNotFound`). Red → green → commit.
10. **`Expired`.** `TestExpired_BoundaryInclusive` (`expires_at == now` returned —
    spec `now >= expires_at`), `TestExpired_ExcludesPermanent` (permanent never
    returned even far past expiry), `TestExpired_ExcludesFuture`. Red → green →
    commit.
11. **`Counts`.** `TestCounts` (mixed permanent/temporary fixture → `active` ==
    total rows, `permanent` == permanent count; empty DB → `0,0`). Red → green →
    commit.
12. **`List` — sort/order/pagination.** `TestList_SortKeysBothOrders` (table over
    `name|created|expires` × `asc|desc` with deterministic fixtures; `id ASC`
    tiebreak),
    `TestList_ExpiresPermanentLast` (permanent rows last under `expires` in **both**
    orders), `TestList_Pagination` (`total` constant across pages; correct slice per
    page; out-of-range page → empty items, same `total`). Red → implement fixed
    sort-key map + count/page queries → green → commit.
13. **`List` — search + `LIKE` escaping.** `TestList_SearchLiteralWildcards`
    (fixtures with ids containing literal `%`/`_`; a query of `%` matches only the id
    containing a literal `%`, not everything), `TestList_SearchCaseInsensitive`.
    Add `FuzzLikeEscape` asserting the escaped-`LIKE` result set equals
    `strings.Contains(lower(id), lower(q))` over random ASCII ids/queries. Red →
    implement escape (`\`→`\\`, `%`→`\%`, `_`→`\_`; `ESCAPE '\'`) → green → run
    `go test -run Fuzz -fuzz=FuzzLikeEscape -fuzztime=30s` → commit.
14. **Timestamp invariants (property).** `internal/store/timestamp_test.go`:
    `TestTimestamp_RoundTrip` (`quick.Check`: random UTC time → encode → decode →
    `Equal`), `TestTimestamp_OrderMatchesChronology` (for `a.Before(b)`,
    `encode(a) < encode(b)` as strings). Red → green (may already pass from task 6;
    then this is a guard) → commit.
15. **`Readiness`.** `TestReadiness_OK` (open DB + writable temp dir → nil),
    `TestReadiness_PingFails` (open then `Close`, call → non-nil),
    `TestReadiness_DirNotWritable` (non-existent dir → non-nil, via `os.CreateTemp`
    failure). Red → implement (ping then `os.CreateTemp(dataDir, ".readyz-*")` /
    close / remove) → green → commit.
16. **Wire `cmd/drop`.** Replace the stub `health` in `internal/cmd`/`main.go`'s
    `run` with `store.Open(cfg.DataDir)` + `defer st.Close()` +
    `Health: store.Readiness(st, cfg.DataDir)`; map `Open` error to `run`'s return.
    Add `TestRun_StoreOpenError` (getenv with a `DATA_DIR` pointing at an unwritable/
    non-existent parent → `run` returns a store error) to `cmd/drop/main_test.go`.
    Red → green → commit.

## Test strategy

- **The test list above *is* the acceptance criteria.** It covers migration
  idempotency + version tracking + refuse-newer (tasks 3–4), every `Store` method
  (6–13), UTC round-trip through SQLite (6, 14), the `Expired` boundary and
  permanent-exclusion / expired-means-gone semantics (10), `List`
  pagination/sort/search + `LIKE` escaping (12–13), and the `/healthz` `200`-vs-`503`
  contract with both a real and a failing health func (1, 15).
- **Real SQLite, no mocks** (per `01-architecture.md` testing infra): every store
  test opens a DB in `t.TempDir()`; never `:memory:` (WAL needs a real file).
  Deterministic fixtures with a fixed base time; no network; no reliance on wall
  clock.
- **Property/fuzz where invariants earn it:** `FuzzLikeEscape` (task 13) proves user
  input can never inject a `LIKE` wildcard — the escaped set always equals literal
  ASCII substring containment; `TestTimestamp_RoundTrip`/`OrderMatchesChronology`
  (task 14) prove the encode/decode round-trips exactly and preserves ordering (the
  two invariants the storage format exists to guarantee). Run the fuzz target ≥30s
  (KNOWLEDGE `[CARD-001]`: trust a fuzz invariant only after live fuzzing, not the
  seed corpus alone).
- **Coverage:** target **90% on the core logic layer, measured as an aggregate over
  `internal/store`'s statements** (`go test -coverprofile … ./internal/store/...`,
  sum covered/total across the package) — **not a per-function floor** (KNOWLEDGE
  `[CARD-001]`: the per-function misreading cost a phase cycle). Unreachable/defensive
  error paths (e.g. a scan error on a healthy driver) are not coverage debt.
- **Gates:** `go test ./...` green, `go vet ./...` clean, `gofmt -l .` empty after
  every task.

## Spec references

- `docs/superpowers/specs/2026-07-09-nyx-drop-design.md`:
  "Storage & data model" (schema, slug/id, **Time semantics** `now >= expires_at`,
  **Permanence semantics**, **Schema migrations**), "Admin — Listing"
  (pagination/sort/search, `q` literal match, permanent-last under `expires`),
  "Error handling" (WAL + busy timeout, single connection pool), "Deployment"
  (`/healthz` = DB ping **and** data dir writable, `503` otherwise),
  "`/healthz` bypasses Host routing".
- `docs/implementation/02-database.md` — **primary source**: connection setup DSN,
  migration algorithm, `0001_init.sql`, the query table, `List` sort-map + `LIKE`
  escaping, and the store test list.
- `docs/implementation/01-architecture.md` — `Store` interface, `Site` type, package
  layout, "define interfaces where consumed", testing infrastructure
  ("real SQLite in `t.TempDir()`; do not mock the store").
- `docs/implementation/00-overview.md` — dependency allowlist, system invariants
  (4 DB-row-is-authority, 5 UTC, 8 ops-endpoint bypass, 9 injected Clock).
- `docs/cards/KNOWLEDGE.md` — `[CARD-001]` entries: `Deps.Health` seam, `server.Deps`
  struct seam, coverage-as-layer-aggregate, fuzz-before-trust, `WriteTimeout` defer.
- `docs/adrs/0002-…` — ops-endpoint bypass (the `/healthz` handler this card gives
  teeth).
- **Flagged refinement:** `02-database.md`'s "RFC 3339 UTC strings" is under-specified
  for simultaneous exact round-trip **and** lexicographic ordering; this design stores
  fixed-width 9-digit-nanosecond RFC 3339, which satisfies both and remains within the
  **spec's** "RFC 3339"/"UTC" requirement. No spec conflict; `02-database.md` guidance
  is refined, not overridden.
