# Architecture & Software Design

## Repository layout

```
nyxhub-drop/
├── cmd/drop/main.go            # wiring only: config → deps → server → reaper → ListenAndServe
├── internal/
│   ├── config/config.go        # env parsing + validation (fail fast)
│   ├── store/                  # SQLite access; owns all SQL
│   │   ├── store.go
│   │   ├── migrate.go
│   │   └── migrations/*.sql    # embedded via embed.FS
│   ├── slug/slug.go            # name generation + embedded word lists (adjectives.txt, animals.txt)
│   ├── extract/extract.go      # multipart/zip → validated site directory in tmp
│   ├── lock/lock.go            # per-site keyed mutex
│   ├── clock/clock.go          # Clock interface + real/fake implementations
│   ├── auth/                   # GitHub OAuth + session cookies
│   │   ├── oauth.go
│   │   └── session.go
│   ├── server/                 # HTTP layer; handlers own no business rules beyond orchestration
│   │   ├── server.go           # root handler: healthz/metrics → host routing → apex/site muxes
│   │   ├── sites_api.go        # POST /api/sites, PUT /api/sites/{id}
│   │   ├── admin_api.go        # /api/admin/*
│   │   ├── auth_handlers.go    # /auth/login, /auth/callback, /auth/logout
│   │   ├── siteserve.go        # static serving for site hosts
│   │   ├── pages.go            # upload page, admin page, 404 page (from web/)
│   │   └── middleware.go       # request logging, metrics, auth guards, CSRF origin check
│   ├── reaper/reaper.go
│   └── metrics/metrics.go      # all prometheus collectors, registered once
├── web/                        # embedded frontend (see 04-frontend.md)
├── deploy/helm/nyxhub-drop/    # chart (see 05-deployment.md)
├── Dockerfile
└── README.md
```

Rule of thumb: **`internal/server` orchestrates; everything else does the work.** A
handler should read as: authenticate → validate input → take lock if mutating → call
store/extract → write response. Business rules (expiry semantics, extraction rules,
permanence) live in the owning package, tested there without HTTP.

## Component responsibilities and interfaces

Define these interfaces where they are *consumed* (Go convention), shown here centrally
for clarity. Constructors take dependencies explicitly — no globals, no `init()` magic.

```go
// clock — everything time-related goes through this.
type Clock interface {
    Now() time.Time // always UTC
}
// clock.Real{} wraps time.Now().UTC(). clock.Fake has SetNow/Advance for tests.

// store.Site is the single site record type used everywhere.
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

// store.Store — all methods are safe for concurrent use.
type Store interface {
    Create(ctx context.Context, s Site) error                  // ErrDuplicate on id collision
    Get(ctx context.Context, id string) (Site, error)          // ErrNotFound
    List(ctx context.Context, p ListParams) ([]Site, int, error) // items, total
    SetPermanent(ctx context.Context, id string, v bool, newExpiry time.Time) error
    Touch(ctx context.Context, id string, updatedAt, expiresAt time.Time, size int64, files int) error // after PUT
    Delete(ctx context.Context, id string) error
    Expired(ctx context.Context, now time.Time) ([]string, error)
    Counts(ctx context.Context) (active, permanent int, err error) // for gauges
    Ping(ctx context.Context) error
}

// lock.Sites — keyed mutex. Lock returns an unlock func.
type Sites interface {
    Lock(id string) (unlock func())
}

// extract.Result of a successful extraction into a temp dir.
type Result struct {
    Dir       string // path under DATA_DIR/tmp, ready to be renamed into place
    SizeBytes int64
    FileCount int
}
// extract.FromZip(r io.ReaderAt, size int64, limits Limits) (Result, error)
// extract.FromMultipart(form *multipart.Form, limits Limits) (Result, error)
// Both enforce ALL extraction rules from the spec (traversal, dotfiles, sole-dir
// stripping, empty rejection, malformed paths, size/count limits). Errors are typed
// (extract.ErrTooLarge, extract.ErrBadPath, extract.ErrEmpty, ...) so handlers can map
// them to 400 vs 413 without string matching.

// slug.Generator
type Generator interface {
    New() string // adjective-animal-suffix; caller retries on store.ErrDuplicate
}
```

### Keyed per-site lock (`internal/lock`)

~30 lines: a `sync.Mutex`-guarded `map[string]*entry` where entry holds a `sync.Mutex`
and a refcount; the entry is removed when the refcount drops to zero so the map doesn't
grow forever. Do not use a global lock — operations on different sites must not block
each other. Unit-test with two goroutines and a fake critical section.

## HTTP routing

`server.New(...)` returns an `http.Handler` structured as:

```
rootHandler(r):
  path == "/healthz"           → health handler        (any Host)
  path == "/metrics"           → promhttp handler      (any Host)
  host := normalize(r.Host)    // lowercase, strip :port
  host == cfg.BaseDomain       → apexMux
  slug, ok := siteLabel(host)  // exactly one label before BASE_DOMAIN
  ok                           → siteHandler(slug)
  otherwise                    → notFoundPage (404)
```

`apexMux` is a `http.ServeMux` with method-qualified patterns:

```
GET  /{$}                    upload page
GET  /admin                  admin page (session-guarded, 302 → /auth/login)
GET  /auth/login             oauth start
GET  /auth/callback          oauth callback
POST /auth/logout            logout (session + origin check)
POST /api/sites              create (token)
PUT  /api/sites/{id}         update (token)
GET  /api/admin/sites        list (session)
POST /api/admin/sites/{id}/permanent    (session + origin)
DELETE /api/admin/sites/{id}/permanent  (session + origin)
GET  /api/admin/sites/{id}/download     (session)
DELETE /api/admin/sites/{id}            (session + origin)
GET  /static/...             embedded assets for the app pages
```

Anything else on the apex → app 404 (JSON for `/api/*` paths, HTML otherwise).
ServeMux returns 405 with correct `Allow` headers for known paths automatically.

### Middleware (plain `func(http.Handler) http.Handler`)

Order for apex routes: `requestLog` → `metrics` → route-specific guards.

- `requireToken`: constant-time compare (`crypto/subtle.ConstantTimeCompare`) of the
  Bearer token against `cfg.UploadToken`; 401 JSON on failure.
- `requireSession`: validates `__Host-session`; API routes → 401 JSON, page routes →
  302 `/auth/login`.
- `checkOrigin`: on state-changing admin/auth routes, if `Origin` (else `Referer`)
  is present it must equal `cfg.ExternalOrigin()` (`SCHEME://BASE_DOMAIN`); mismatch → 403.
- `maxBytes`: wraps body with `http.MaxBytesReader(w, r.Body, cfg.MaxUploadSize)`;
  a `*http.MaxBytesError` from parsing maps to 413.

## Key flows

### Create (`POST /api/sites`)

1. `requireToken`, `maxBytes`.
2. Parse multipart (`r.MultipartForm`); decide zip vs files mode (field `file` = zip,
   fields `files` = individual files; both present or neither → 400).
3. `extract.FromZip/FromMultipart` → `Result` in `/data/tmp/<random>`.
4. Loop ≤ 5: `id := slug.New()`; `store.Create(...)`; on `ErrDuplicate` continue.
   (Insert the row **before** the rename — the PK constraint is the collision detector,
   and a concurrent identical slug can't race the directory. On rename failure, delete
   the row; a crash in between is healed by the startup sweep.)
5. `os.Rename(result.Dir, sitesDir/id)`.
6. Respond 201 with id, URL (`cfg.SiteURL(id)`), `expires_at`.
7. On any error after extraction: `os.RemoveAll` the temp dir.

### Update (`PUT /api/sites/{id}`)

1. `requireToken`, `maxBytes`; extract to temp dir (same as create, before the lock —
   extraction is slow and touches nothing shared).
2. `unlock := locks.Lock(id)`; defer unlock.
3. `store.Get(id)` → not found, or expired-and-not-permanent → 404 (remove temp dir).
4. Swap: `os.Rename(siteDir, tmp/<id>.old)`, `os.Rename(newDir, siteDir)`,
   `os.RemoveAll(old)`.
5. `store.Touch(id, now, now+TTL, size, files)`.
6. Respond 200 (same shape as create).

### Admin download

1. `requireSession`; `unlock := locks.Lock(id)`; `store.Get(id)` → 404 if gone.
2. Build zip at `/data/tmp/dl-<id>-<random>.zip` while holding the lock.
3. Unlock, then stream the file with `Content-Disposition: attachment;
   filename="<id>.zip"`; `defer os.Remove(zipPath)`.
   (Snapshot under lock, stream outside it — a slow client must not block a PUT.)

### Reaper tick

```
ids := store.Expired(ctx, clock.Now())
for id := range ids:
    unlock := locks.Lock(id)
    s, err := store.Get(id)
    if err == nil && !s.Permanent && !clock.Now().Before(s.ExpiresAt):
        store.Delete(id); os.RemoveAll(siteDir(id)); metrics.Reaped.Inc()
    unlock()
```

Errors: log with site id, `metrics.ReapErrors.Inc()`, continue. Runs at startup and
every minute (`time.Ticker`; interval injectable for tests). Startup also: remove
`/data/tmp/*`, remove any `/data/sites/<dir>` with no DB row, and delete any DB row
with no site directory (both directions of crash artifact).

## Sessions (`internal/auth/session.go`)

Stateless signed token, no server-side session store.

```
payload  = {"sub": "<github-login-lowercased>", "exp": <unix>}
token    = base64url(payloadJSON) + "." + base64url(HMAC-SHA256(payloadJSON, SESSION_SECRET))
```

Validate: split, recompute HMAC, `hmac.Equal`, then check `exp` against the clock.
Logout = `Set-Cookie` with `Max-Age=0`. The OAuth `state` cookie (`__Host-oauth-state`)
uses the same signing scheme with a 10-minute `exp`.

**Dev-mode cookie downgrade:** `__Host-` and `Secure` cookies are refused by browsers
on non-localhost HTTP origins. When `SCHEME=http`, cookie names drop the `__Host-`
prefix (`session`, `oauth-state`) and omit `Secure`; all other attributes unchanged.
Centralize this in one `cookieFor(cfg, name, value, maxAge)` helper so the rule can't
drift between the two cookies.

## Error handling conventions

- JSON errors: `writeErr(w, status, msg)` → `{"error": msg}` + correct status.
- Typed sentinel errors cross package boundaries (`store.ErrNotFound`,
  `store.ErrDuplicate`, `extract.Err*`); handlers map them to statuses with `errors.Is`.
- Never leak internal paths or SQL in error messages; log details, return generic text.
- Unexpected errors → 500 `{"error": "internal error"}` + `slog.Error` with details.

## Logging & metrics conventions

- One `slog.Logger` created in main, JSON handler, level from config; passed down.
- Request log line (in middleware): `msg="request" host= path= method= status= dur_ms= bytes=`.
- Lifecycle events: `msg="site created|updated|reaped|deleted|made permanent|permanence unset|admin login"` with `site=` and/or `user=` fields.
- All Prometheus collectors are defined in `internal/metrics`, created by `metrics.New(reg)`;
  no `promauto` global registry — tests build their own registry.
- Gauges `sites_active`/`sites_permanent` refresh from `store.Counts()` on each reaper
  tick and after each create/delete/permanent change (cheap query).

## Testing infrastructure

- **Fake clock** (`clock.Fake`): mutex-guarded `time.Time` with `Advance(d)`.
- **Test store**: real SQLite in `t.TempDir()` — the pure-Go driver makes this cheap;
  do not mock the store in integration tests.
- **Test server helper** (`internal/server/servertest_test.go`): constructs the full
  handler with fake clock, temp data dir, known tokens/secrets, and a **stub GitHub**
  (`httptest.Server` implementing `/login/oauth/authorize`, `/login/oauth/access_token`,
  `/user`) whose base URLs are injected via the oauth config. Returns
  `(handler, *clock.Fake, dataDir)`.
- **Multipart builders**: helpers to build zip-mode and files-mode bodies from a
  `map[string]string` of path→content.
- **Host trick**: requests against the handler set `req.Host = "slug.test.local"`
  directly — no DNS needed. Use `BASE_DOMAIN=test.local` in tests.
- Integration tests drive the exported handler only (no internal calls) so they stay
  valid across refactors.
