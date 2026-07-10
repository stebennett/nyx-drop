---
id: CARD-012
type: task
layer: infra
title: "Clarify the coverage target as a layer aggregate, not a per-function floor"
status: design
phase: design
right_sized: true
depends_on: [CARD-001]
branch: task/012-coverage-target-wording-design
worktree: /Users/stevebennett/Code/nyx-drop-worktrees/CARD-012
design_pr_url: https://github.com/stebennett/nyx-drop/pull/12
pr_url: ""
adrs: []
reworks: 0
started: 2026-07-10
delivered: ""
created: 2026-07-10
---

## Why
CARD-001's test phase was blocked on a false finding. The design's Test strategy reads
"≥ 90% coverage on the core logic layer — `internal/config` (Load, ParseSize, accessors),
`internal/clock`, `internal/server` routing helpers, `renderNotFound`, and the middleware
recorder". The `card-tester` read the enumerated list as a **per-function floor** and
failed `renderNotFound` at 71.4%, even though the layer aggregates to 92.0% and clears the
target comfortably.

The reading was also self-inconsistent: `parseLogLevel` (50.0%) and `isDigits` (83.3%)
passed unflagged purely because the design does not name them. And the two statements
dragging `renderNotFound` down are unreachable error returns — `template.ParseFS` over a
compile-time `embed.FS`, and `tmpl.Execute` of a one-field struct — so satisfying a
per-function floor would mean deviating from the merged design's fixed signature
`renderNotFound(cfg) ([]byte, error)` to inject a breakable `fs.FS`, purely to move a
metric.

The wording is ambiguous, it cost a phase cycle once, and every future card inherits it —
each new `design.md` restates a Test strategy, and each new `card-tester` interprets it.
Fix the wording where it was written, and lift the rule into repo doctrine so it can't
recur. See `docs/cards/CARD-001-walking-skeleton/feedback.md` for the full record.

## Acceptance criteria
- [ ] `docs/cards/CARD-001-walking-skeleton/design.md` §"Test strategy" states the target
      unambiguously as an **aggregate across the named core-logic layer**, not a
      per-function floor, and says so in a form a future agent cannot misread.
- [ ] `docs/cards/PROTOCOL-ADDENDUM.md` carries the rule as project doctrine, so every
      future `card-designer` writes it correctly and every future `card-tester` applies it
      correctly, without depending on any one card's prose.
- [ ] The doctrine entry names the measurement command
      (`go test -coverprofile=… ./...` → `go tool cover -func=…`, summed over the layer's
      statements) so the figure is reproducible rather than eyeballed per function.
- [ ] The doctrine entry states that unreachable error paths (e.g. a `template.ParseFS`
      over a compile-time `embed.FS`) are not to be covered by contriving injectable
      failure points that deviate from a merged design's fixed signatures.
- [ ] `docs/cards/config.md`'s `coverage_target` value is consistent with the clarified
      wording (it currently reads `"90% on the core logic layer"` — confirm that phrasing
      still reads as an aggregate, or tighten it).
- [ ] No production code changes. Docs only.

## Notes
Opened by `/kanban` on 2026-07-10 at the driver's direction, resolving the CARD-001 test
gate ("Accept, but tighten the design first"). `depends_on: [CARD-001]` because it edits
CARD-001's `design.md`, which lands on `main` via CARD-001's implementation PR — and
because the lesson it encodes comes from that card.

Not in any milestone: `MILESTONES.md` is owned by `/refine`, and `/kanban` never writes it.
Run `/refine` to place this card in a milestone (or leave it unplaced — the scheduler
treats an unplaced card as lowest priority and will pick it up once its dependency is done).
