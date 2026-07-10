# CARD-008 — Slice

## Verdict
Right-sized. No split proposed.

## Rationale

CARD-008 maps 1:1 onto **S8 — Admin sign-in (GitHub OAuth)** in
`docs/implementation/06-vertical-slices.md`, and its four acceptance criteria are a
direct restatement of the spec's `### Authentication` section and adjacent CSRF
subsection (`docs/superpowers/specs/2026-07-09-nyx-drop-design.md`), cross-checked
against the `/auth/*` endpoint table in `docs/implementation/03-api-reference.md`.
`docs/cards/KNOWLEDGE.md` (`[CARD-001]`) records that this project's cards were
deliberately sliced to match that document's slice list, which is strong evidence for
right-sizing an infra/foundation-shaped card like this one.

The split this card most invites — separating the OAuth sign-in dance from session
*verification* (the `requireSession` / Origin-check middleware) — was weighed explicitly
and rejected:

- **Shared invariant, atomic by necessity.** Both halves are built around the same
  `__Host-session` cookie contract: HMAC-`SESSION_SECRET` signing on issue, the same
  signature verified on every subsequent request, identical `HttpOnly`/`Secure`/
  `Path=/`/no-`Domain`/`SameSite=Lax` shape, the same `SCHEME=http` local-dev
  downgrade to a bare `session` cookie. A "sign-in only" slice would mint a cookie
  nobody ever checks (no observable protected behaviour — the stub `/admin` route
  cannot legitimately return `200` without a verifier). A "verify only" slice has
  nothing to verify, since nothing upstream of it issues a valid cookie. This is the
  "pieces share one invariant that must land atomically" case from the slicing
  heuristics, not two independently shippable vertical slices.
- **The rejection path is the point, not an edge case.** The card's own `## Why`
  states the deliverable is admin-only enforcement ("the admin — and only the
  configured admin — can sign in; everyone else is refused"), not merely "login
  works." Deferring the wrong-user/bad-state rejection behaviour to a later card would
  ship an interim state where sign-in exists without its access-control guarantee — a
  security-incomplete fragment, not a smaller vertical slice.
- **Logout is not worth carving out either.** `POST /auth/logout` reuses the same
  session + Origin-check middleware this card already builds, and is meaningless to
  demonstrate without a working sign-in to terminate.

## Sizing check
- 4 acceptance criteria (below the >5 signal for oversized).
- Single spec section (`Authentication`, plus its directly adjacent CSRF
  subsection — not two unrelated spec areas).
- Title carries no compound "and" doing real work.
- No dependency on CARD-009's admin UI: the spec's own S8 acceptance list only
  requires a stub `/admin` route returning `200` for an authenticated session
  and `302`/`401` for unauthenticated page/API requests respectively — the full
  admin table UI is out of scope here and is CARD-009's job.

## Seams inherited from CARD-001 (merged)
- `server.Deps` is a struct — auth wiring is added as fields, with no call-site churn.
- `apexMux` is an empty `*http.ServeMux`; this card adds the `/auth/*` and stub `/admin`
  patterns to it. The apex currently serves Go's plain 404 placeholder.
- `routeClass(cfg, r)` already takes the full `*http.Request`, so the `admin` route class
  **reserved by ADR-0002** can be introduced here by path without a signature change. The
  design phase should decide whether CARD-008 or CARD-009 introduces it.
- Invariant (`CLAUDE.md`): `__Host-` cookies carry **no `Domain` attribute**. This constrains
  the session design directly and is a large part of why sign-in and verification must ship
  together.

Conclusion: CARD-008 is indivisible at this grain — any further split would separate
pieces that share one cookie/session invariant and cannot be independently
demonstrated or independently secure. Proceeds to design as-is.

No split proposed; no dependents rewiring needed. CARD-009
(`depends_on: [CARD-003, CARD-008]`) keeps its existing dependency unchanged.
