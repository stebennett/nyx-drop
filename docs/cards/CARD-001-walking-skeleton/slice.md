# Slice: CARD-001 — Walking skeleton

## Verdict
Right-sized. CARD-001 is an indivisible vertical slice.

## Rationale
CARD-001's scope is the project's walking skeleton: repo scaffold, `internal/config`
(full parsing/validation), `internal/clock`, `slog` setup, `internal/metrics`
plumbing, the host-normalizing root handler (`/healthz`, `/metrics`, unknown-host
404 page), request-log middleware, Dockerfile, and CI. This corresponds exactly to
slice **S1** in `docs/implementation/06-vertical-slices.md`, which the project's own
architecture doc already vertical-sliced and explicitly kept as one slice — contrasting
it with S2 (store & migrations), which that same document calls out by name as a
deliberate "thin horizontal exception" because it is the one piece everything else
consumes. S1 carries no such caveat: it is meant to be built and shipped whole.

Applying the split test (`Can this card be split into 2+ slices that are each
independently shippable and testable and each deliver a piece of functionality?`):
the only candidate cut points are:
1. **Config parsing/validation** in isolation — no HTTP surface, so "shippable" would
   mean only a binary that can exit non-zero on bad env; there is nothing yet for an
   operator to *run and probe*, which is the card's own stated Why.
2. **HTTP endpoints (healthz/metrics/host-routing/middleware)** in isolation — cannot
   exist without the config that supplies `BASE_DOMAIN`, `PORT`, log level, etc., so
   it cannot be built or demoed first.
3. **Dockerfile + CI** in isolation — packaging/tooling for a binary that doesn't
   build cleanly without both of the above; has no observable behaviour of its own
   beyond "the thing from cut 1+2, containerized."

Each of these is a **horizontal** cut (config layer / http layer / packaging layer)
of a single feature ("an operator can run the binary and probe it"), not a vertical
slice that changes user-observable behaviour on its own. The slicing heuristics
explicitly forbid this pattern ("never split by layer," "never create a
setup/scaffolding child with no observable behaviour"). The pieces also share one
invariant that must land atomically: `main.go` wires config → clock → slog →
metrics → server as a single unit, and splitting would force a redesign of that
wiring's interface once the "later" pieces arrive — another explicit
don't-split signal.

Sequencing-wise, splitting would also gain nothing: CARD-002 (SQLite store) and
CARD-008 (GitHub OAuth) both need the *complete* skeleton (config, server, routing,
middleware) to build on, per `docs/implementation/01-architecture.md`'s
`server.New(...)` and middleware-order description — so any split's later child
would still be the one they'd actually depend on, and the earlier child(ren) would
add process overhead without changing either dependent's `depends_on`.

The card's four acceptance-criteria bullets (though each bundles 1-3 concrete
checks, ~7 checks total) all trace to the same spec sections — "Static serving",
"Configuration", "Observability", "Architecture overview", "Deployment" — describing
one coherent capability rather than spanning unrelated spec areas. Per the
calibration guidance, this is genuinely borderline on the acceptance-criteria-count
signal, but the "walking skeleton" is by definition the smallest possible unit that
touches every architectural layer to prove they connect end-to-end; further
splitting would defeat that purpose. When borderline, right-sized is preferred, and
here the project's own build-order document already independently reached the same
conclusion.

## Spec references
- `docs/superpowers/specs/2026-07-09-nyx-drop-design.md` — "Static serving" (host
  normalization, `/healthz`/`/metrics` bypass), "Configuration (environment
  variables)" (fail-fast validation table), "Observability" (structured logging,
  Prometheus endpoint), "Deployment" (Dockerfile/distroless/non-root).
- `docs/implementation/06-vertical-slices.md` §S1.
- `docs/implementation/01-architecture.md` (repository layout, root handler
  routing, middleware order, logging/metrics conventions).
