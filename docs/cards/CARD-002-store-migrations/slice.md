# CARD-002 — Slice

## Verdict
Right-sized. No split.

## Rationale

CARD-002 is the project's own deliberate **thin-horizontal exception**, called out by name
in `docs/implementation/06-vertical-slices.md` §S2 ("Store & migrations — thin horizontal
exception… Kept separate because it is the one piece everything else consumes"). It maps
1:1 onto that slice, and `docs/cards/MILESTONES.md` groups it with CARD-001 as the whole of
M1 ("An operator can run and probe the binary, backed by a real store"). `docs/cards/KNOWLEDGE.md`
(CARD-001 entry) instructs treating `06-vertical-slices.md` as strong evidence for right-sizing
verdicts on foundation cards — that evidence is unambiguous here.

Applying the slice test (can this split into 2+ independently shippable, testable,
functionality-delivering pieces) against `docs/implementation/01-architecture.md`'s `Store`
interface and `docs/implementation/02-database.md`'s schema/query/test spec:

- **The `Store` interface is one cohesive unit**: `Create`, `Get`, `List`, `SetPermanent`,
  `Touch`, `Delete`, `Expired`, `Counts`, `Ping` — nine methods over a single `Site` type and
  a single `sites` table (one migration file, `0001_init.sql`). None of these methods is
  reachable from outside `internal/store` until CARD-003 wires `POST /api/sites` and site
  serving to them; only `Ping` (via `/healthz`) has any observable effect in this card's own
  milestone.
- **Tested split A** (migrations + `Ping` + real `/healthz` vs. full CRUD + `List`): the second
  child would ship with zero observable behaviour — nothing calls `Create`/`Get`/`Touch`/
  `Delete`/`List` before CARD-003 lands. That is exactly the "setup/scaffolding child with no
  observable behaviour" the slicing heuristics forbid.
- **Tested split B** (CRUD methods vs. `List` alone, by data-variation/read-before-write
  heuristics): both halves are equally unreachable pre-CARD-003, so no observable behaviour is
  gained by sequencing them — only cross-card churn.
- **Tested split C** (migrations vs. store methods, i.e. by layer): explicitly disallowed —
  "never split by layer." Migrations create the schema; store methods are its only consumer;
  they share one invariant (schema version in sync with the query set) that must land
  atomically, matching the "don't split when pieces share one invariant" guidance.
- The one genuinely observable increment in this card — `/healthz` 200 vs 503 — is already a
  single narrow acceptance criterion (spec "Deployment — Helm chart probes"), not a separable
  slice on its own; it is the sliver of user-visible behaviour that licenses bundling the rest
  of the horizontal foundation into this one exception card in the first place. It is also
  already flagged as needing a pinned test: CARD-001's review recorded that the `health` 503
  branch is currently unexercised, because S1's stub never fails.

Calibration checks: 4 acceptance criteria; one finite, already-enumerated test list
(`02-database.md` "Store tests must cover"); one schema; one package (`internal/store`); one
milestone (M1, paired with CARD-001 per `MILESTONES.md`). The title has no "and" doing real
design work — "migrations" and "store" name two facets of one atomic foundation, not two
independent features.

## Sources consulted
- `docs/superpowers/specs/2026-07-09-nyx-drop-design.md` — "Storage & data model" (schema),
  "Schema migrations" (versioning algorithm).
- `docs/implementation/00-overview.md` — dependency allowlist, invariants (DB-row-is-authority,
  UTC everywhere).
- `docs/implementation/02-database.md` — full schema, query set, `List` params, store test list.
- `docs/implementation/01-architecture.md` — `Store` interface, package layout.
- `docs/implementation/06-vertical-slices.md` §S2 — explicit "thin horizontal exception" framing.
- `docs/cards/MILESTONES.md` — M1 groups CARD-001 + CARD-002.
- `docs/cards/KNOWLEDGE.md` — CARD-001 entries (slice-to-card 1:1 mapping guidance; `Deps.Health`
  seam; `server.Deps` struct seam).
- `docs/cards/CARD-002-store-migrations/card.md`.

No split proposed; no dependents rewiring needed. CARD-003 (`depends_on: [CARD-001, CARD-002]`)
keeps its existing dependency unchanged.
