# CARD-008 — Admin sign-in via GitHub OAuth — Design

## Intent

Let the single configured admin — and no one else — sign in to nyx-drop through
GitHub's OAuth authorization-code flow, and enforce that session on every admin
surface. Concretely: `GET /auth/login` starts the flow, `GET /auth/callback`
completes it and issues a signed session cookie for `ADMIN_GITHUB_USER` (refusing
every other GitHub user), `requireSession` gates admin pages (302→login) and admin
API (401 JSON), a CSRF Origin check guards state-changing admin/auth requests, and
`POST /auth/logout` ends the session. The deliverable is admin-only *enforcement*,
not merely "login works" — the wrong-user and bad-state refusal paths are
first-class, not edge cases.

This card introduces the `internal/auth` package (the project's first use of
`golang.org/x/oauth2`), threads two fields into the existing `server.Deps` struct,
and adds routes to the empty `apexMux` — all seams CARD-001 left open, so there is
no call-site churn. It ships a stub `/admin` (200 when authenticated); the real
admin table UI and admin API handlers are CARD-009.

## Acceptance criteria

Sharpened from `card.md`, each traced to its enforcing spec text.

1. **Happy-path sign-in against a stub GitHub.** `GET /auth/login` sets a 10-minute
   `__Host-oauth-state` cookie (HttpOnly, Secure, Path=/, no Domain, SameSite=Lax)
   and 302-redirects to GitHub's authorize URL carrying `client_id`,
   `redirect_uri=<ExternalOrigin>/auth/callback`, and a random `state`. `GET
   /auth/callback?code&state` verifies the state cookie against the `state` param,
   exchanges the code, fetches the login, and — login matching `ADMIN_GITHUB_USER`
   — sets `__Host-session` (HMAC `SESSION_SECRET`, HttpOnly, Secure, Path=/, no
   Domain, SameSite=Lax, 7-day), clears the state cookie, and 302-redirects to
   `/admin`; a subsequent `GET /admin` with that session → 200.
   *(spec §Authentication "Sign-in/out flow"; 03-api-reference "Auth endpoints".)*
2. **Exactly one admin; case-insensitive.** A GitHub login equal to
   `ADMIN_GITHUB_USER` under case-insensitive comparison signs in successfully (the
   session `sub` stores the lowercased login). Any other login → 403 "not
   authorized" HTML page, **no session cookie set**. A bad, expired, missing, or
   forged/replayed `state` → 403, no session.
   *(spec §Authentication "compared case-insensitively", "any other user receives
   403", "verified on callback (login-CSRF protection)".)*
3. **Unauthenticated access is refused per surface.** With no valid session
   (absent, tampered, or expired cookie): admin **pages** (`/admin`) → 302 to
   `/auth/login`; admin **API** (`/api/admin/*`) → 401 JSON `{"error":"..."}`.
   *(spec §Admin "Auth failures"; 03-api-reference "Admin API".)*
4. **Logout and CSRF.** `POST /auth/logout` with a valid session and a passing
   Origin check clears the session cookie (`Max-Age=0`) and 302-redirects to `/`.
   State-changing admin/auth requests (POST/DELETE) whose `Origin` — or, absent
   Origin, `Referer` — is present and does not equal `<SCHEME>://<BASE_DOMAIN>` are
   rejected with 403; when neither header is present the request proceeds (spec
   scopes the check to "when present").
   *(spec §Authentication "CSRF protection", "Sign-in/out flow".)*
5. **Local-dev cookie downgrade.** When `SCHEME=http`, both cookies downgrade to
   bare names (`session`, `oauth-state`) and omit `Secure`; all other attributes
   unchanged.
   *(spec §Authentication "When SCHEME=http … the cookie downgrades".)*
6. **`__Host-` attributes are asserted directly.** A test parses the issued
   `Set-Cookie` and asserts the exact attribute set (name, HttpOnly, Secure, Path=/,
   **no Domain**, SameSite=Lax, ~7-day / ~10-min max-age) for both scheme modes.
   *(CLAUDE.md/00-overview `__Host-` invariant; spec §Authentication.)*

## In scope

- New package `internal/auth`: `Signer` (HMAC token sign/verify), session &
  state claim issue/verify, `NewNonce`, `OriginAllowed`, `AuthorizedUser`, and an
  injectable `OAuth` exchange adapter (`AuthCodeURL`, `ExchangeLogin`).
- `internal/server`: `cookies.go` (`cookieName`, `cookieFor`), `auth_middleware.go`
  (`requireSession(page|api)`, `checkOrigin`), `auth_handlers.go`
  (`/auth/login`, `/auth/callback`, `/auth/logout`, stub `/admin`, guarded
  `/api/admin/` subtree placeholder), and `renderUnauthorized`.
- `web/unauthorized.html` (self-contained, embedded) and its embed wiring.
- `server.Deps` gains `Session *auth.Signer` and `OAuth *auth.OAuth`;
  `apexMux` gains the routes; `cmd/drop/main.go` constructs and injects them.
- `routeClass` extended to return `admin` for apex `/admin` and `/api/admin/`
  paths (the class reserved by ADR-0002).
- The stub-GitHub test server + auth-aware test-handler helper.
- Adding `golang.org/x/oauth2` to `go.mod` (allowlisted for this card).

## Out of scope (YAGNI — with owning card)

- **Real admin table UI and admin API handlers** (`GET /api/admin/sites`,
  permanent/delete/download) — **CARD-009 / CARD-010**. This card ships only a stub
  `/admin` 200 page and a guarded `/api/admin/` placeholder proving the 401 contract.
- **Upload grants / `GrantStore` / `requireUploadAuth`** — **CARD-007 (S7)**. They
  reuse this card's `auth.Signer` primitive but are wired there.
- **Server-side session revocation / token rotation tooling** — not in v1 (see
  proposed ADR-0005; revocation is via `SESSION_SECRET` rotation).
- **The dusk `web/static/app.css` and branded chrome** — introduced by CARD-007;
  `unauthorized.html` is deliberately self-contained (see Approach) to avoid a
  cross-card ordering dependency.
- **Multiple admins / org-team membership** — spec fixes exactly one
  `ADMIN_GITHUB_USER`.

## Dependencies & assumptions

- **Depends on CARD-001 only** (merged, on `main`). Confirmed against `card.md`
  `depends_on: [CARD-001]`. **CARD-002's SQLite store is NOT a dependency** — hence
  the stateless (store-free) session/state design (proposed ADR-0005).
- Reuses CARD-001 seams: `server.Deps` struct (add fields), empty `apexMux` (add
  routes), `routeClass(cfg, *http.Request)` (extend by path), injected
  `clock.Clock`, `config.Config` fields `GitHubClientID/Secret`, `AdminGitHubUser`
  (already lowercased), `SessionSecret`, `Scheme`, `ExternalOrigin()`.
- Assumes `SESSION_SECRET`, `GITHUB_CLIENT_ID/SECRET`, `ADMIN_GITHUB_USER` are
  present and validated (config already fails fast if missing).
- **No new environment variables.** The OAuth base URLs are *internal* constructor
  parameters (defaulted to GitHub in `cmd/drop`, overridden by the test stub), per
  spec §Testing "configurable OAuth base URL internally" — the spec's Configuration
  table intentionally lists none.
- **No schema/migration impact.** This card needs no persistence (ADR-0005). It
  coexists with CARD-002 landing independently because it never touches the DB;
  order of merge between the two is irrelevant.

## Approach

### Alternatives considered

- **DB-backed session/state store (rejected).** Would couple CARD-008 to CARD-002's
  schema and force a merge ordering between two parallel cards, and buys little for a
  single-admin service. Rejected in favour of stateless HMAC tokens (ADR-0005). The
  one thing it would add — server-side revocation — is provided adequately by
  `SESSION_SECRET` rotation given a single admin.
- **JWT library (rejected).** A full JWT/JOSE dependency is outside the four-module
  allowlist and overkill: a fixed `HMAC-SHA256(base64url(json)).base64url(mac)`
  envelope with `hmac.Equal` is ~40 lines, stdlib-only, and is exactly the scheme
  01-architecture already specifies (and that CARD-007's grants reuse).
- **Self-verifying signed `state` with no separate nonce comparison (rejected).**
  Signing `{exp}` alone is deterministic (not unique per login) and conflates the
  CSRF binding with expiry. Chosen instead: a random nonce as the URL `state`, the
  signed envelope `{state,exp}` as the cookie, compared constant-time on callback —
  matching 03-api-reference "verify state cookie ↔ param" and giving both an
  unforgeable per-login binding and an authoritative server-checked expiry.
- **Splitting sign-in from session verification into two cards (rejected at slice).**
  They share one cookie invariant and neither half is independently demonstrable or
  independently secure (see `slice.md`).

### Session cookie construction

Stateless, per ADR-0005. On successful callback the server issues
`token = base64url(payloadJSON) + "." + base64url(HMAC-SHA256(payloadJSON,
SESSION_SECRET))` with `payload = {"sub": <login-lowercased>, "exp":
<Clock.Now()+168h unix>}`. Verification (every guarded request): split on `.`,
recompute the MAC over the decoded payload bytes, `hmac.Equal` (constant time), then
reject if `exp <= Clock.Now()`. Expiry is **business time** → `Deps.Clock`, never
`time.Now()` (invariant 9). The cookie name/attributes are produced by the single
`cookieFor` helper (ADR-0006): `__Host-session` with HttpOnly, Secure, Path=/, no
Domain, SameSite=Lax, `MaxAge=604800`; downgraded to `session` without Secure when
`SCHEME=http`.

### OAuth `state` mechanism

On `/auth/login`: `nonce := NewNonce()` (≥128-bit crypto/rand hex);
`cookie := cookieFor(cfg, "oauth-state", Signer.IssueState(nonce, now+10m), 600)`;
redirect to `OAuth.AuthCodeURL(nonce)`. The **raw nonce** is the GitHub `state=`
param; the **signed envelope** `{state:nonce, exp}` is the cookie value. On
`/auth/callback`: read the state cookie, `VerifyState` (MAC + exp via Clock), then
constant-time compare the recovered nonce with `r.URL.Query().Get("state")`. Any
failure → 403. The state cookie is **cleared unconditionally** at the top of the
callback (`Max-Age=0`), making state single-use: a replayed callback finds no cookie
and 403s. `SameSite=Lax` is mandatory so the cookie rides GitHub's cross-site
top-level redirect back to `/auth/callback` (Strict would drop it and break login).

### CSRF check and middleware ordering

`checkOrigin` enforces, on unsafe methods only (POST/PUT/PATCH/DELETE — it self-skips
GET/HEAD/OPTIONS so it can wrap a mixed-method subtree), that `OriginAllowed(Origin,
Referer, cfg.ExternalOrigin())` holds, else 403. Ordering follows 01-architecture
("authenticate → validate input"): `requireSession` runs first, then `checkOrigin`,
then the handler. So `/auth/logout` and the `/api/admin/` state-changing subtree are
wrapped `requireSession(api) → checkOrigin → handler`. This is layered on top of
`SameSite=Lax`, per spec. `OriginAllowed` is a pure function: Origin (already
scheme://host) compared directly; absent Origin, the Referer's parsed scheme://host
compared; absent both, allow.

### The `admin` route class (ADR-0002)

CARD-008 introduces it. `routeClass` returns `admin` when the host is the apex **and**
the path is `/admin`, under `/admin/`, or under `/api/admin/`; the public
unauthenticated `/auth/*` endpoints and the rest of the apex stay `app`; non-apex
stays `site`. This lands the reserved class the moment an authenticated admin surface
first exists; CARD-009 extends the same path predicate for its new admin API routes
without re-deciding the taxonomy — exactly the extension ADR-0002 anticipated
("admin split … without re-deciding"). No signature change: `routeClass` already
takes the full `*http.Request`.

### The "not authorized" page

`web/unauthorized.html` is rendered with `html/template` (auto-escaping — the
attempted GitHub `login` is user-controlled and must not enable HTML injection),
injecting the refused login and a `/auth/login` link. It is kept **self-contained**
(inline dusk-palette styles, mirroring CARD-001's `notfound.html`) so it renders
correctly regardless of whether CARD-007's `/static/app.css` has merged — CARD-008
does not depend on CARD-007.

## Interfaces

### `internal/auth` (pure core, plus one I/O adapter)

```go
package auth

// Signer signs and verifies compact HMAC-SHA256 tokens:
//   base64url(payloadJSON) "." base64url(HMAC-SHA256(payloadJSON, key)).
type Signer struct { key []byte }
func NewSigner(secret string) *Signer
func (s *Signer) sign(payload []byte) string
func (s *Signer) verify(token string) (payload []byte, err error) // ErrBadToken

type SessionClaims struct { Sub string `json:"sub"`; Exp int64 `json:"exp"` }
type StateClaims   struct { State string `json:"state"`; Exp int64 `json:"exp"` }

func (s *Signer) IssueSession(sub string, exp time.Time) string
func (s *Signer) VerifySession(token string, now time.Time) (SessionClaims, error) // ErrBadToken | ErrExpired
func (s *Signer) IssueState(nonce string, exp time.Time) string
func (s *Signer) VerifyState(token string, now time.Time) (StateClaims, error)     // ErrBadToken | ErrExpired

var ( ErrBadToken = errors.New("auth: bad token"); ErrExpired = errors.New("auth: token expired") )

// NewNonce returns a hex-encoded 256-bit crypto/rand value.
func NewNonce() (string, error)

// AuthorizedUser reports whether githubLogin is the configured admin
// (case-insensitive; adminUser is already lowercased by config).
func AuthorizedUser(githubLogin, adminUser string) bool

// OriginAllowed enforces the CSRF Origin/Referer rule (see Approach).
func OriginAllowed(origin, referer, expected string) bool

// OAuth is the GitHub adapter; base URLs are injectable for the stub server.
type OAuthConfig struct {
    ClientID, ClientSecret string
    RedirectURL string
    AuthBaseURL string        // default "https://github.com" (authorize + token)
    APIBaseURL  string        // default "https://api.github.com" (/user)
    HTTPClient  *http.Client  // nil => http.DefaultClient
}
type OAuth struct { cfg oauth2.Config; userURL string; hc *http.Client }
func NewOAuth(c OAuthConfig) *OAuth
func (o *OAuth) AuthCodeURL(state string) string
// ExchangeLogin exchanges code and returns the GitHub login. Never logs the
// code, token, or secret. Wraps failures as ErrExchange / ErrUserFetch.
func (o *OAuth) ExchangeLogin(ctx context.Context, code string) (login string, err error)
```

### `internal/server` additions

```go
// Deps gains (struct field additions — no call-site churn):
//   Session *auth.Signer
//   OAuth   *auth.OAuth

// cookies.go
func cookieName(cfg *config.Config, base string) string          // "session"->"__Host-session" (https) | "session" (http)
func cookieFor(cfg *config.Config, base, value string, maxAge int) *http.Cookie

// auth_middleware.go
type sessionMode int
const ( sessionPage sessionMode = iota; sessionAPI )
func (s *srv) requireSession(mode sessionMode, next http.Handler) http.Handler // ctx carries the login
func (s *srv) checkOrigin(next http.Handler) http.Handler                       // unsafe methods only
func loginFromContext(ctx context.Context) (string, bool)

// auth_handlers.go
func (s *srv) authLogin(w http.ResponseWriter, r *http.Request)     // GET /auth/login
func (s *srv) authCallback(w http.ResponseWriter, r *http.Request)  // GET /auth/callback
func (s *srv) authLogout(w http.ResponseWriter, r *http.Request)    // POST /auth/logout
func (s *srv) adminStub(w http.ResponseWriter, r *http.Request)     // GET /admin (200 stub; CARD-009 replaces)

// pages.go
func renderUnauthorized(cfg *config.Config, login string) ([]byte, error) // html/template, escaped
```

`New` registers on `apexMux`: `GET /auth/login`, `GET /auth/callback`,
`POST /auth/logout` (→ requireSession(api) → checkOrigin), `GET /admin`
(→ requireSession(page)), and `/api/admin/` subtree (→ requireSession(api) →
checkOrigin) as a placeholder that 404s JSON when authenticated (CARD-009 mounts real
handlers). The unauthorized page bytes are **not** pre-rendered (they embed the
per-request login); the template is parsed once at `New`.

### `cmd/drop/main.go`

```go
signer := auth.NewSigner(cfg.SessionSecret)
gh := auth.NewOAuth(auth.OAuthConfig{
    ClientID:     cfg.GitHubClientID,
    ClientSecret: cfg.GitHubClientSecret,
    RedirectURL:  cfg.ExternalOrigin() + "/auth/callback",
    // AuthBaseURL/APIBaseURL left blank => GitHub defaults; HTTPClient nil.
})
// added to the existing server.New(server.Deps{...}) literal:
//   Session: signer,
//   OAuth:   gh,
```

## Data flow

### OAuth round trip (against stub GitHub in tests)

1. Browser → `GET /auth/login`. Server: `nonce=NewNonce()`; `Set-Cookie`
   `__Host-oauth-state = IssueState(nonce, now+10m)` (Lax, 10-min); `302` to
   `OAuth.AuthCodeURL(nonce)` (authorize URL + client_id + redirect_uri + state=nonce).
2. GitHub authenticates the user, redirects browser →
   `GET /auth/callback?code=<c>&state=<nonce>` (top-level cross-site GET; Lax state
   cookie is sent).
3. Server: clear state cookie (`Max-Age=0`); read+`VerifyState` the cookie (MAC+exp
   via Clock); constant-time compare recovered nonce == `state` param — mismatch/
   missing/expired → `403`. Then `login, _ := OAuth.ExchangeLogin(ctx, code)` (POST
   token, GET `/user`).
4. `AuthorizedUser(login, cfg.AdminGitHubUser)` false → render `unauthorized.html`
   (`403`), **no session**. True → `Set-Cookie` `__Host-session =
   IssueSession(strings.ToLower(login), now+168h)` (Lax, 7-day); `slog.Info("admin
   login", user=login)` (no token); `302` → `/admin`.

### Per-request session verification

`requireSession(mode)`: read cookie `cookieName(cfg,"session")`. Absent/`VerifySession`
error/expired (Clock) → unauthorized: `mode==page` → `302` `/auth/login`;
`mode==api` → `401` `{"error":"unauthorized"}`. Success → put `claims.Sub` in request
context, call `next`. `checkOrigin` (when wired) then applies on unsafe methods.

## Schema / migration impact

**None.** This card introduces no tables, columns, or migrations and reads/writes no
database. Sessions and OAuth state are stateless signed cookies (proposed ADR-0005).
It therefore coexists with CARD-002 (the SQLite store) landing independently, in
either merge order, with zero shared surface.

## Test strategy

The test list **is** the acceptance criteria. No test touches the real GitHub or the
network: a stub `httptest.Server` implements `/login/oauth/access_token` and `/user`,
injected via `OAuthConfig.AuthBaseURL/APIBaseURL/HTTPClient`. All time-dependent
assertions use `clock.Fake`.

**Coverage.** Core-logic layer = package `internal/auth` (Signer, session/state
claim verification, `AuthorizedUser`, `OriginAllowed`, `NewNonce`), measured as a
**layer aggregate ≥90% of statements** (per the `[CARD-001]` KNOWLEDGE rule — not a
per-function floor; unreachable error branches are not coverage debt). The
`internal/server` handlers/middleware are covered by httptest integration tests, not
held to the per-function 90%. Gates: `go test ./...`, `go test -race ./...`,
`go vet ./...`, `gofmt -l .` clean.

Unit (`internal/auth`):
- `Signer` round-trips a payload; a token with a flipped byte in payload or MAC →
  `ErrBadToken`; a token missing the `.` separator → `ErrBadToken`.
- `VerifySession`/`VerifyState`: valid within window; `exp == now` and `exp < now`
  → `ErrExpired`; wrong-secret Signer → `ErrBadToken`.
- Property test (Go `testing/quick` or table+ranged inputs): for random sub/exp,
  `VerifySession(IssueSession(sub, exp), now)` returns `sub` iff `now < exp`; any
  single-byte mutation of the token → error (integrity invariant). Constrain
  generated `sub` to valid UTF-8 logins.
- `AuthorizedUser`: `"Octocat"` vs admin `"octocat"` → true; `"octocat "`,
  `"octocatx"`, `""` → false.
- `OriginAllowed`: matching Origin → true; mismatching Origin → false; no Origin +
  matching Referer (`https://base/path?q`) → true; no Origin + cross-origin Referer
  → false; neither header → true; malformed Referer → false.
- `NewNonce`: two calls differ; length/charset is hex; ≥256 bits.
- `OAuth.ExchangeLogin` against the stub: returns the stubbed login; stub token
  endpoint 500 → `ErrExchange`; stub `/user` 500 → `ErrUserFetch`.
- `OAuth.AuthCodeURL` contains `client_id`, `redirect_uri`, and the given `state`.

Integration (`internal/server`, httptest handler + stub GitHub + fake clock):
- **Happy path (AC1):** login→callback→session→`/admin` 200, as in Data flow; assert
  the redirect chain and that the state cookie is cleared on callback.
- **Case-insensitive success (AC2):** stub returns `"Octocat"`, `ADMIN_GITHUB_USER=
  octocat` → session issued with `sub="octocat"`.
- **Wrong user (AC2):** stub returns `"mallory"` → `403`, body is the unauthorized
  page containing the escaped login, **no `Set-Cookie` session**.
- **State failures (AC2):** callback with (a) no state cookie, (b) cookie present but
  `state` param altered, (c) state cookie expired (advance fake clock >10m),
  (d) replayed callback (second use after cookie cleared) → each `403`, no session.
- **Unauthenticated (AC3):** `GET /admin` with no/expired/tampered session → `302`
  `/auth/login`; `GET /api/admin/anything` with same → `401` JSON.
- **Logout (AC4):** `POST /auth/logout` with valid session + matching Origin → clears
  cookie (`Max-Age=0`), `302` `/`.
- **CSRF (AC4):** `POST /auth/logout` with valid session and (a) `Origin:
  https://evil.example` → `403`; (b) no Origin, cross-origin `Referer` → `403`;
  (c) no Origin/Referer → allowed; (d) matching Origin → allowed. Same matrix
  against the guarded `/api/admin/` state-changing placeholder.
- **Cookie downgrade (AC5):** build the handler with `Scheme:"http"`; assert issued
  cookies are named `session`/`oauth-state` with **no** Secure; `Scheme:"https"` →
  `__Host-session`/`__Host-oauth-state` **with** Secure.
- **`__Host-` attributes (AC6):** parse the callback's session `Set-Cookie`; assert
  HttpOnly, Secure (https mode), Path=/, `Domain==""`, `SameSite=Lax`, MaxAge≈604800;
  and the login state cookie ≈600.
- **Route class:** `routeClass` returns `admin` for apex `/admin` and
  `/api/admin/sites`, `app` for apex `/auth/login` and `/`, `site` for a site host
  (table test).
- **No secret leaks:** drive a callback with a log buffer; assert the request log
  line and any auth logs contain neither the `code`, the session token, nor the
  `access_token` (only `path=/auth/callback` and `user=<login>`).

## Implementation task list

Each step is one red→green→commit cycle. Absolute paths under
`/Users/stevebennett/Code/nyx-drop-worktrees/CARD-008`.

0. **Add dependency.** `go get golang.org/x/oauth2`; commit the `go.mod`/`go.sum`
   change (no code yet). *(Verifies the allowlisted module resolves.)*
1. **Signer** — create `internal/auth/token.go` + `token_test.go`. Test
   `TestSigner_RoundTrip` and `TestSigner_TamperedToken_ErrBadToken` (flip a payload
   byte, a MAC byte, drop the separator) → red → implement `NewSigner/sign/verify` →
   green → commit.
2. **Session & state claims** — extend `token.go`/`token_test.go`.
   `TestVerifySession_WithinWindow`, `TestVerifySession_AtAndPastExp_ErrExpired`,
   `TestVerifyState_*`, plus the property test `TestSession_RoundTrip_Property`.
3. **Nonce** — `internal/auth/nonce.go` + test `TestNewNonce_UniqueHex256`.
4. **Predicates** — `internal/auth/authz.go` + test:
   `TestAuthorizedUser_CaseInsensitive`, `TestOriginAllowed_Matrix`.
5. **OAuth adapter** — `internal/auth/oauth.go` + `oauth_test.go` with an in-test stub
   `httptest.Server`. `TestOAuth_AuthCodeURL_ContainsStateAndClient`,
   `TestOAuth_ExchangeLogin_ReturnsLogin`,
   `TestOAuth_ExchangeLogin_TokenError/UserError`.
6. **Cookie helpers** — `internal/server/cookies.go` + `cookies_test.go`.
   `TestCookieFor_HostPrefix_HTTPS` (name, Secure, Path=/, no Domain, Lax),
   `TestCookieFor_Downgrade_HTTP` (bare name, no Secure), `TestCookieName_*`.
7. **`requireSession`** — `internal/server/auth_middleware.go` +
   `auth_middleware_test.go`. Tests: page-mode absent/tampered/expired → 302;
   api-mode → 401 JSON; valid → next sees login in context.
8. **`checkOrigin`** — extend `auth_middleware.go`/test. Matrix from Test strategy;
   assert GET is not guarded.
9. **Unauthorized page** — `web/unauthorized.html` (self-contained) + update
   `web/embed.go` embed set; `renderUnauthorized` in `internal/server/pages.go` +
   `pages_test.go` `TestRenderUnauthorized_EscapesLogin`.
10. **`/auth/login`** — `internal/server/auth_handlers.go` + register in `New`
    (needs the Deps fields from step 16 available; introduce the `Deps.Session`/
    `Deps.OAuth` fields here as part of the first handler that needs them). Test
    `TestAuthLogin_SetsStateCookie_RedirectsToAuthorize`.
11. **`/auth/callback` happy path** — add handler + the stub-GitHub test helper
    (`internal/server/servertest_test.go`: builds the handler with fake clock,
    known secrets, and a stub GitHub; returns `(handler, *clock.Fake, stubURL)`).
    Test `TestAuthCallback_HappyPath_SetsSession_Redirects` incl. state-cookie clear.
12. **Callback refusals** — tests `TestAuthCallback_WrongUser_403_NoSession`,
    `TestAuthCallback_CaseInsensitiveSuccess`, `TestAuthCallback_BadState_403`
    (no cookie / altered param / expired / replayed) → implement the state-verify
    and username-refusal branches.
13. **Stub `/admin` + `/api/admin/` guard** — `adminStub` (200) behind
    `requireSession(page)`; guarded `/api/admin/` placeholder behind
    `requireSession(api)`+`checkOrigin`. Register both in `New`. Tests
    `TestAdmin_NoSession_302`, `TestAdmin_WithSession_200`,
    `TestAPIAdmin_NoSession_401JSON`.
14. **`/auth/logout`** — handler behind `requireSession(api)`+`checkOrigin`. Tests
    `TestLogout_ClearsCookie_Redirects`, `TestLogout_CrossOrigin_403`.
15. **Route class** — extend `routeClass` in `internal/server/routing.go` +
    `routing_test.go` `TestRouteClass_AdminPaths`.
16. **Wire `cmd/drop`** — construct `auth.NewSigner` + `auth.NewOAuth`, add to the
    `server.Deps` literal; extend `cmd/drop/main_test.go` if it asserts wiring.
17. **Cookie-attribute + no-leak assertions** — dedicated tests
    `TestSessionCookie_HostAttributes`, `TestAuth_NoSecretInLogs` (log buffer).
18. **Full-flow integration** — `TestAdminSignIn_EndToEnd` exercising login→callback
    →`/admin` 200 through the exported handler only. Final `go test -race ./...`,
    `go vet`, `gofmt -l .` green → commit.

## Spec references

- `docs/superpowers/specs/2026-07-09-nyx-drop-design.md` §Authentication (incl. CSRF
  protection & Sign-in/out flow), lines 163–177; Configuration table (SCHEME,
  GITHUB_CLIENT_ID/SECRET, ADMIN_GITHUB_USER, SESSION_SECRET), lines 230–249;
  §Admin "Auth failures", line 199; §Testing (auth failures & "OAuth: stubbed"),
  lines 277–278.
- `docs/implementation/03-api-reference.md` "Auth endpoints" and "Admin API"
  (`__Host-oauth-state`, `__Host-session`, 401-JSON vs 302), lines 84–95.
- `docs/implementation/01-architecture.md` package layout (lines 16–28), Middleware
  (`requireSession`, `checkOrigin`), lines 147–162; Sessions (token format,
  dev-mode downgrade, `cookieFor`), lines 216–240; Testing infrastructure (stub
  GitHub helper), lines 260–274.
- `docs/implementation/04-frontend.md` `unauthorized.html` (lines 5, 20) and
  `docs/implementation/ui-mockups/unauthorized.html`.
- `docs/implementation/00-overview.md` — dependency allowlist, `__Host-` cookie
  invariant, injected-Clock invariant (9).
- `docs/adrs/0002-...md` — reserves the `admin` route class for CARD-008/009
  (introduced here).

## Discrepancies flagged (none blocking)

- The card's AC names the state cookie only as "a 10-min `__Host-` state cookie";
  03-api-reference and 01-architecture name it `__Host-oauth-state` and specify
  HMAC signing. No conflict — this design adopts the named, signed form (the more
  specific implementation notes refine, and do not contradict, the spec).
- The card AC mentions only the **session** cookie downgrading under `SCHEME=http`;
  01-architecture (line 238) downgrades the **state** cookie name too
  (`oauth-state`). This design downgrades both, consistent with the implementation
  notes and with the spec's stated rationale (browsers refuse `__Host-`/`Secure`
  on non-localhost HTTP for *any* cookie).
