# Database Schema & Access

SQLite via `modernc.org/sqlite` (driver name `"sqlite"`), one database file at
`<DATA_DIR>/drop.db`. The store package owns **all** SQL — no SQL strings anywhere else.

## Connection setup

```go
db, err := sql.Open("sqlite", filepath.Join(dataDir, "drop.db")+
    "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)")
db.SetMaxOpenConns(1) // single writer process; avoids SQLITE_BUSY entirely
```

`MaxOpenConns(1)` is deliberate: one process, low traffic, and it removes a whole class
of lock errors. Do not raise it without measuring.

## Migrations (`internal/store/migrate.go`)

- Files embedded from `internal/store/migrations/` named `0001_init.sql`,
  `0002_<name>.sql`, … applied in filename order.
- Version tracking: `PRAGMA user_version`. Algorithm:
  1. Read `user_version` → v.
  2. If v > highest known migration → return error (binary older than DB); app must
     refuse to start.
  3. For each migration n > v: run inside a transaction, then `PRAGMA user_version = n`.
- Timestamps are stored as **RFC 3339 UTC strings** (`TEXT`). RFC 3339 sorts
  lexicographically, so SQL `ORDER BY`/comparisons work; parse to `time.Time` on scan.

### `0001_init.sql`

```sql
CREATE TABLE sites (
    id         TEXT PRIMARY KEY,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    permanent  INTEGER NOT NULL DEFAULT 0 CHECK (permanent IN (0, 1)),
    size_bytes INTEGER NOT NULL,
    file_count INTEGER NOT NULL,
    source     TEXT    NOT NULL CHECK (source IN ('api', 'web'))
);

CREATE INDEX idx_sites_reap ON sites (permanent, expires_at);
```

## Queries (the complete set)

| Method | SQL |
|---|---|
| `Create` | `INSERT INTO sites (id, created_at, updated_at, expires_at, permanent, size_bytes, file_count, source) VALUES (?,?,?,?,0,?,?,?)` — map `sqlite.Error` code `SQLITE_CONSTRAINT_PRIMARYKEY` to `ErrDuplicate` |
| `Get` | `SELECT id, created_at, updated_at, expires_at, permanent, size_bytes, file_count, source FROM sites WHERE id = ?` → `ErrNotFound` on `sql.ErrNoRows` |
| `Touch` | `UPDATE sites SET updated_at = ?, expires_at = ?, size_bytes = ?, file_count = ? WHERE id = ?` → `ErrNotFound` if 0 rows affected |
| `SetPermanent(true)` | `UPDATE sites SET permanent = 1 WHERE id = ?` |
| `SetPermanent(false)` | `UPDATE sites SET permanent = 0, expires_at = ? WHERE id = ?` (new expiry = now + TTL, computed by caller) |
| `Delete` | `DELETE FROM sites WHERE id = ?` → `ErrNotFound` if 0 rows |
| `Expired` | `SELECT id FROM sites WHERE permanent = 0 AND expires_at <= ?` (param: now as RFC 3339) |
| `Counts` | `SELECT COUNT(*), COALESCE(SUM(permanent), 0) FROM sites` |
| `Ping` | `db.PingContext(ctx)` |

## `List` — pagination, sorting, search

```go
type ListParams struct {
    Page    int    // ≥ 1
    PerPage int    // 1..200
    Sort    string // "name" | "created" | "expires"
    Order   string // "asc" | "desc"
    Query   string // raw substring; empty = no filter
}
```

**Validation happens in the handler** (400 on bad values); the store receives only
sanitized params but still whitelists (defense in depth).

Sort key → SQL, via a fixed map (never interpolate user input):

| `sort` | ORDER BY expression |
|---|---|
| `name` | `id {order}` |
| `created` | `created_at {order}, id ASC` |
| `expires` | `permanent ASC, expires_at {order}, id ASC` |

Permanent sites must sort **last** under `expires` regardless of order direction. The
leading `permanent ASC` achieves this: it is independent of `{order}` and always sorts
`0` (temporary) before `1` (permanent). The trailing `id ASC` makes ordering
deterministic for equal keys. Test both directions.

Search: `WHERE id LIKE '%' || ? || '%' ESCAPE '\'` with `%`, `_`, and `\` in the user
input escaped as `\%`, `\_`, `\\`. SQLite `LIKE` is case-insensitive for ASCII by
default, which matches the spec (slugs are ASCII).

Two queries per list call: `SELECT COUNT(*) …` with the same WHERE, then the page query
with `LIMIT ? OFFSET ?` (`offset = (page-1)*per_page`). Return `(items, total)`.

## Store tests must cover

- Round-trip of all fields including UTC preservation (store a known time, read it back,
  `require.True(t, got.Equal(want))`).
- `ErrDuplicate` on double insert; `ErrNotFound` on get/touch/delete of missing id.
- `Expired` boundary: site with `expires_at == now` **is** returned (spec: `now >= expires_at`).
- Permanent sites never returned by `Expired` regardless of date.
- List: every sort key × both orders (deterministic fixtures), permanent-last under
  `expires`, pagination math (`total` unaffected by page), search with literal `%`/`_`
  in the query, empty result pages.
- Migration runner: fresh DB reaches latest version; DB at future version → error;
  re-running is a no-op.
