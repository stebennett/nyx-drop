# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

NyxHub Drop — a self-hosted Cloudflare Drop clone for Kubernetes. Upload static assets
(zip or files) → temporary site on a random animal-slug subdomain
(`trusty-tahr-x7k2mq.sites.nyxhub.net`) that expires after a global TTL unless an admin
marks it permanent. Single Go binary, SQLite + PVC storage, wildcard-ingress host
routing, GitHub OAuth admin, token/grant-gated API.

**Current state: design phase — no application code exists yet.** The repo contains a
finished specification, implementation notes, and UI mockups, ready for plan-writing
and implementation.

## Document map and authority order

1. `docs/superpowers/specs/2026-07-09-nyxhub-drop-design.md` — **the spec**. Defines
   all behavior. Where anything (including the docs below) disagrees with it, the spec
   wins; flag the discrepancy rather than silently choosing.
2. `docs/implementation/00-overview.md` — how-to-build index: fixed technology
   decisions, dependency allowlist, nine system invariants, glossary, definition of done.
3. `docs/implementation/01-architecture.md` … `06-vertical-slices.md` — architecture
   and Go interfaces, database schema/queries, full API contract, frontend behavior +
   design system, deployment (Docker/Helm/CI/local dev), suggested build order.
4. `docs/implementation/ui-mockups/index.html` — high-fidelity static mockups of every
   page and state (open in a browser). `mockup.css` there is the basis for the real
   `web/static/app.css`.

## Non-negotiables (details in 00-overview.md)

- Go + stdlib `net/http.ServeMux`; only four external modules (`modernc.org/sqlite`,
  `prometheus/client_golang`, `golang.org/x/oauth2`, plus transitives). No web
  framework, no ORM, no npm/bundler.
- The nine system invariants in `00-overview.md` hold at all times: atomic site
  creation via same-filesystem rename, expired-means-gone on public surfaces, per-site
  locking for mutations, DB-row-is-authority, UTC everywhere, injected `Clock` (never
  `time.Now()` in handlers), `__Host-` cookies with no `Domain` attribute,
  `/healthz`+`/metrics` matched before host routing, never trust upload paths.
- Frontend is embedded via `embed.FS`, self-contained, zero external requests.
- The "dusk" design system in `04-frontend.md` (palette, type roles, lifetime-moon
  signature component) is a fixed decision, not a suggestion.

## Working conventions

- Build in the vertical-slice order of `06-vertical-slices.md`; each slice lands
  demonstrable with tests green (the acceptance criteria are the test list).
- Design changes go into the spec first, then ripple to the implementation notes and
  mockups in the same commit — the three doc layers are kept mutually consistent and
  cross-linked.
- Local dev uses `BASE_DOMAIN=localtest.me` with `SCHEME=http` so wildcard subdomains
  resolve to 127.0.0.1 without /etc/hosts changes (see `05-deployment.md`).

## Commands

No build exists yet. Once code lands (slice S1 onward), the standard set is:
`go test ./...`, `go test -run TestName ./internal/<pkg>`, `go vet ./...`,
`gofmt -l .`, `docker build .`, `helm lint deploy/helm/nyxhub-drop`.
