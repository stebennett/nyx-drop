# Milestones

Ordered delivery milestones, authored by `/refine`. Document order = delivery order.
`/kanban` reads this file and never writes it.

## M1 — Walking skeleton
**Goal:** An operator can run and probe the binary, backed by a real store.
**Exit criteria:** `docker build` and CI green; `/healthz` reflects a real DB ping and data-dir writability.
**Cards:** CARD-001, CARD-002

## M2 — Sites via API
**Goal:** CI can create, update, and rely on expiring sites end-to-end with the upload token.
**Exit criteria:** zip POST → served URL; PUT redeploys the same URL with timer reset; expired sites 404 and get reaped (fake-clock integration tests green).
**Cards:** CARD-003, CARD-004, CARD-005, CARD-006

## M3 — Web uploads
**Goal:** A person drags a zip or folder onto the page and gets a URL — zero credentials.
**Exit criteria:** manual drag → progress → working URL; expired/reused/tampered/PUT grant cases all 401.
**Cards:** CARD-007

## M4 — Admin console
**Goal:** The sole configured GitHub admin signs in and curates all sites.
**Exit criteria:** OAuth sign-in against stub GitHub; list with paging/sort/search; permanent toggle incl. rescue race, delete, and snapshot download all pass incl. CSRF checks.
**Cards:** CARD-008, CARD-009, CARD-010

## M5 — One-command install
**Goal:** `helm install` deploys the entire system.
**Exit criteria:** `helm lint` + golden template tests in CI; template output audited against the 05-deployment.md checklist.
**Cards:** CARD-011
