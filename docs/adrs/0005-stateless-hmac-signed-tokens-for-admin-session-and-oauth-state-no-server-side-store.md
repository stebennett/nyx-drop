---
id: ADR-0005
title: "Stateless HMAC-signed tokens for admin session and OAuth state (no server-side store)"
status: Accepted
date: 2026-07-10
card: CARD-008
supersedes: []
superseded_by: ""
---

# ADR-0005: Stateless HMAC-signed tokens for admin session and OAuth state (no server-side store)

## Context

S8 needs an admin session and a login-CSRF `state`. CARD-002's SQLite store is a
sibling card, not a dependency of CARD-008 (`depends_on: [CARD-001]` only), so a
DB-backed session/state store is unavailable and would couple two parallel cards. The
spec (`01-architecture.md` "Sessions") already prescribes a stateless signed token.
The system is single-admin, single-replica on an RWO volume, and must survive pod
restarts without losing the ability to authenticate.

## Decision

We will make both the session and the OAuth `state` self-contained compact tokens
signed with `HMAC-SHA256(payload, SESSION_SECRET)`, encoded
`base64url(payloadJSON) + "." + base64url(mac)`, verified with constant-time
`hmac.Equal`.

Session payload `{"sub":<github-login-lowercased>,"exp":<unix>}`, 7-day expiry. State
payload `{"state":<128-bit+ random hex nonce>,"exp":<unix>}`, 10-minute expiry; the
raw nonce is the GitHub `state=` URL param and the signed envelope is the cookie,
compared constant-time on callback. Expiry is checked against the injected Clock
(business time). There is no server-side session/state store. The same `auth.Signer`
primitive is reused by CARD-007's upload grants.

## Status

Accepted

## Consequences

Easier: zero persistence, no DB coupling, sessions survive restarts, one signing
primitive shared with grants, trivially testable as pure functions.

Harder: no server-side revocation before the 7-day expiry — the only revocation lever
is rotating `SESSION_SECRET` (invalidates all tokens at once), an accepted trade-off
for a single-admin service. A leaked `SESSION_SECRET` forges sessions and grants
alike; it is already a required, secret-managed config value.
