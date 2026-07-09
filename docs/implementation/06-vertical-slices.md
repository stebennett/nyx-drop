# Suggested Vertical Slices

A recommended decomposition into thin, independently shippable slices. Each slice ends
with the system **running and demonstrable**, all tests green. Build them in order —
each lists what it depends on. The plan writer may reshape these, but must keep slices
vertical (each touches whatever layers it needs to deliver observable behavior) and must
not batch all tests or all deployment work into a final slice.

Per-slice conventions (apply to every slice, don't repeat in plans):
- TDD: write the failing test from the acceptance criteria first.
- Update README as behavior appears.
- Structured logging and metrics for whatever the slice adds (once S1 lands the plumbing).

---

### S1 — Walking skeleton
**Story:** an operator can run the binary (locally and as a container) and probe it.
**Scope:** repo scaffold (`go.mod`, layout from 01-architecture), `internal/config`
(full parsing + validation, all variables), `internal/clock`, `slog` setup,
`internal/metrics` plumbing (registry + HTTP histogram only), root handler with
`/healthz` (DB ping stubbed to OK until S2) + `/metrics` + host normalization + 404
page, request-log middleware, Dockerfile, CI workflow (vet/fmt/test/build).
**Accept:** `go run` + curl `/healthz` → 200 on any Host; bad config → non-zero exit
naming the variable; `docker build` succeeds; CI green.

### S2 — Store & migrations (thin horizontal exception)
**Story:** none user-visible; foundation for everything after. Kept separate because it
is the one piece everything else consumes.
**Scope:** `internal/store` complete per 02-database.md (schema, migrations, all
methods incl. List), `/healthz` now really pings the DB and checks data-dir writability.
**Accept:** store test list in 02-database.md passes; `/healthz` 503s when `DATA_DIR`
is unwritable.

### S3 — Create a site via API and visit it *(the product exists after this slice)*
**Story:** a CI job POSTs a zip with the token and gets back a URL that serves the site.
**Depends:** S1, S2.
**Scope:** `internal/slug` (word lists + generator), `internal/extract` zip path with
ALL extraction rules (traversal, dotfiles, sole-dir strip, empty/malformed rejection,
size/count limits), `internal/lock`, `POST /api/sites` with token middleware, site-host
static serving (lookup incl. expiry check, index.html, nosniff, 405 on non-GET/HEAD),
site URL builder.
**Accept:** upload zip via curl → 201 with URL; curl with `Host: <slug>.test.local`
serves content; expired row (fake clock) → 404 page; the full 400/401/413 error table
for creation; slug format/DNS validity.

### S4 — Multi-file upload
**Story:** a user can upload individual files (paths preserved) instead of a zip.
**Depends:** S3.
**Scope:** `extract.FromMultipart`, `file` vs `files` mode selection (400 on both/neither).
**Accept:** files-mode curl → site serves nested paths; shared extraction rules hold
(dotfile filter, duplicate-path 400, sole-dir strip when all paths share one root dir).

### S5 — TTL expiry: the reaper
**Story:** sites disappear on schedule and survive restarts correctly.
**Depends:** S3.
**Scope:** `internal/reaper` (tick + startup run + orphan-dir sweep + tmp sweep),
lifecycle metrics (`sites_reaped_total`, `reap_errors_total`, gauges), lifecycle logs.
**Accept:** with fake clock advanced past TTL, tick removes row + files; permanent rows
untouched (flag manually in test store); startup reaps sites that expired "while down";
orphan dirs removed; errors on one site don't stop others.

### S6 — Update in place (`PUT`)
**Story:** CI re-deploys a preview to the same URL; the timer resets.
**Depends:** S3 (S5 for the expired-404 test).
**Scope:** `PUT /api/sites/{id}`, atomic dir swap under per-site lock, `store.Touch`,
`sites_updated_total`.
**Accept:** PUT replaces content at same URL; `expires_at`/`updated_at` reset;
permanence preserved; unknown id and expired-unreaped id → 404; parallel PUTs to one id
serialize (race test with `-race`).

### S7 — Upload web page (credential-free)
**Story:** a person drags a zip or folder onto a page and gets their URL — no token,
no sign-in.
**Depends:** S3, S4.
**Scope:** `GET /api/upload-grant` + `auth.GrantStore` (single-use, 15-min expiry) +
`requireUploadAuth` on `POST /api/sites` (token → `source=api`, grant → `source=web`;
grants rejected on `PUT`); `web/upload.html`, `upload.js` per 04-frontend.md
(grant-then-XHR upload, progress, folder walking, success/error states, one automatic
retry on 401), embedded static serving, `web` embed wiring, branded `notfound.html`
replacing any placeholder 404.
**Accept:** automated: grant issue → create succeeds with `source=web`; expired,
reused, and tampered grants → 401; grant on `PUT` → 401; token path still works with
`source=api`; pages served at `/` and `/static/*`. Manual: drag zip → progress → URL
shown and works with no credential prompt anywhere; folder drag preserves paths.

### S8 — Admin sign-in (GitHub OAuth)
**Story:** the admin (and only the admin) can sign in; everyone else is refused.
**Depends:** S1 (not S3 — can be built in parallel with S3–S7).
**Scope:** `internal/auth` (oauth flow with injectable endpoint URLs, state cookie,
session sign/verify), `/auth/*` handlers, `requireSession` middleware, Origin-check
middleware, `unauthorized.html`, stub-GitHub test server helper.
**Accept:** full flow against stub GitHub: login → callback → session cookie
(`__Host-session`, HttpOnly, Secure, SameSite=Lax, no Domain) → `/admin` 200; wrong
user → 403 page, no cookie; case-differing username succeeds; bad/expired state → 403;
logout clears; `/admin` without session → 302; `/api/admin/*` without session → 401 JSON.

### S9 — Admin list UI
**Story:** the admin sees all sites with paging, sorting, and search.
**Depends:** S8 + S3 (rows to list).
**Scope:** `GET /api/admin/sites` (validation, pagination, sort incl. permanent-last,
LIKE-escaped search, `expired` flag), `web/admin.html` + `admin.js` table/search/pager
per 04-frontend.md.
**Accept:** API contract per 03-api-reference.md incl. 400s on bad params; UI renders,
sorts, searches, pages (manual); expired rows badged.

### S10 — Admin actions: permanent, delete, download
**Story:** the admin curates: keeps sites forever, removes them, exports them.
**Depends:** S9.
**Scope:** permanent set/unset (unset ⇒ `now + TTL`), rescue semantics (reaper
re-check under lock — extend S5's reaper), delete endpoint, snapshot-then-stream
download, UI actions with confirm, `sites_deleted_total`.
**Accept:** toggle round-trips and is idempotent; unset yields fresh expiry; rescue race
test (expired row + make-permanent before tick → survives; after reap → 404); delete →
site 404s immediately, 204 then 404 on repeat; download zip contains exact files,
mid-download reap/update cannot truncate (lock test); CSRF: cross-origin Origin → 403.

### S11 — Helm chart & production hardening
**Story:** an operator installs the whole thing with one `helm install`.
**Depends:** all above (but chart skeleton can start anytime after S1).
**Scope:** chart per 05-deployment.md (incl. fsGroup, Recreate, wildcard ingress,
existingSecret), golden-template tests, chart lint in CI, NOTES.txt, README install
docs, `prometheus.io` annotations.
**Accept:** `helm lint` + golden tests green in CI; `helm template` output audited
against 05-deployment.md checklist; (if a cluster is available) kind/k3d smoke:
install → create site → visit via port-forward with Host header.

---

## Slice-to-spec traceability

Every spec requirement lands in: upload/API create (S3/S4), TTL teardown (S5), update
(S6), upload page (S7), admin auth (S8), admin list (S9), permanent/download/delete +
rescue (S10), deployability (S1/S11), observability (S1 + threaded through S3–S10),
security hardening (S3 nosniff/token, S8 cookies/CSRF/state). If a plan drops a slice,
something in the spec is unimplemented.
