---
id: CARD-008
type: feature
layer: api
title: Admin sign-in via GitHub OAuth
status: design
phase: design
right_sized: true
depends_on: [CARD-001]
branch: feature/008-admin-oauth-design
worktree: /Users/stevebennett/Code/nyx-drop-worktrees/CARD-008
design_pr_url: https://github.com/stebennett/nyx-drop/pull/11
pr_url: ""
adrs: [ADR-0005, ADR-0006]
reworks: 0
started: 2026-07-10
delivered: ""
created: 2026-07-09
---

## Why
The admin — and only the configured admin — can sign in; everyone else is refused.
Adds `internal/auth` (OAuth flow with injectable endpoint URLs for the stub GitHub
test server, state cookie, session sign/verify), `/auth/*` handlers,
`requireSession` and Origin-check middleware, and `unauthorized.html`. Mirrors
slice S8; can proceed in parallel with CARD-003–007.

## Acceptance criteria
- [ ] Full flow against stubbed GitHub: `/auth/login` sets a 10-min `__Host-` state cookie and redirects to the authorize URL; `/auth/callback` verifies state, exchanges the code, checks the login, sets `__Host-session` (HMAC `SESSION_SECRET`, HttpOnly, Secure, `Path=/`, no Domain, SameSite=Lax, 7-day), redirects to `/admin` → 200 (spec "Authentication", "Sign-in/out flow")
- [ ] Login differing from `ADMIN_GITHUB_USER` only by case succeeds; any other user → 403 "not authorized" page with no session; bad/expired/missing state → 403 (spec "Authentication")
- [ ] Without a session: admin pages → 302 to `/auth/login`; `/api/admin/*` → 401 JSON (spec "Auth failures")
- [ ] `POST /auth/logout` (session + Origin check) clears the cookie and redirects to `/`; state-changing admin requests with a cross-origin `Origin`/`Referer` are rejected; `SCHEME=http` downgrades the cookie to name `session` without `Secure` for local dev (spec "CSRF protection", "Authentication")

## Notes
`__Host-` prefix defends against cookie tossing from `*.<BASE_DOMAIN>` sites running
arbitrary uploaded JS — a core security invariant (00-overview.md).
