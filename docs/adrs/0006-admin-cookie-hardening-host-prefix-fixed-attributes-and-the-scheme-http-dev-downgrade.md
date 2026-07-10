---
id: ADR-0006
title: "Admin cookie hardening: __Host- prefix, fixed attributes, and the SCHEME=http dev downgrade"
status: Accepted
date: 2026-07-10
card: CARD-008
supersedes: []
superseded_by: ""
---

# ADR-0006: Admin cookie hardening: __Host- prefix, fixed attributes, and the SCHEME=http dev downgrade

## Context

Uploaded sites run attacker-controlled JS on `*.<BASE_DOMAIN>` and must not be able to
set, shadow, or fixate the admin session via a `Domain=.<BASE_DOMAIN>` cookie (cookie
tossing) — a core invariant (`00-overview.md` / `CLAUDE.md`). The `__Host-` prefix
enforces host-only cookies but mandates `Secure` + `Path=/` + no `Domain`, and browsers
refuse `Secure`/`__Host-` cookies on non-localhost HTTP origins, which would silently
break the documented local-dev setup (`SCHEME=http` on `localtest.me`).

## Decision

We will give admin cookies the `__Host-` prefix (`__Host-session`,
`__Host-oauth-state`) with `Secure`, `Path=/`, no `Domain`, `HttpOnly`,
`SameSite=Lax`. When `cfg.Scheme == "http"` (local dev only) the cookies downgrade to
bare names (`session`, `oauth-state`) and omit `Secure`; all other attributes are
unchanged.

The prefix/name/`Secure` choice is driven solely by `cfg.Scheme`, not the live
connection, and is centralized in one `cookieFor(cfg, base, value, maxAge)` helper
(plus `cookieName(cfg, base)` for reads) so the two cookies cannot drift. `SameSite`
is `Lax` (not `Strict`) so the state cookie survives GitHub's cross-site top-level
callback redirect.

## Status

Accepted

## Consequences

Easier: the production admin session is unforgeable and un-tossable from subdomains;
local dev over plain HTTP works without a TLS terminator; the single helper makes the
rule auditable and testable.

Harder: dev cookies lack `__Host-`/`Secure` protection — acceptable because dev runs
on a single trusted host with no untrusted subdomains, and the downgrade is gated
strictly on `SCHEME=http`. A misconfiguration serving real traffic with `SCHEME=http`
would weaken the session; config validation already restricts `SCHEME` to
`http`/`https` and operators are expected to run `https` in production.
