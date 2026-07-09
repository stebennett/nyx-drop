---
id: CARD-000
type: task            # feature | task | defect
layer: <one of config.layers>    # primary architectural layer; read from config.md
title: Short imperative title
status: backlog       # backlog | slice | design | implement | test | review | deliver | done | blocked | split
phase: backlog        # mirrors status; the phase the card is currently in
right_sized: ""       # `true` once confirmed an indivisible vertical slice (by /refine at intake, the slice phase, a split carve-out, or a keep-as-one override); guards re-slicing. `true` at intake skips the slice phase entirely.
depends_on: []        # list of card ids that must be `done` before this starts, e.g. [CARD-001]
branch: ""            # current branch: `<type>/NNN-slug-design` from the slice→design transition, then `<type>/NNN-slug` once the design PR merges
worktree: ""          # absolute path of the current branch's worktree
design_pr_url: ""     # design PR (slice.md + design.md + ADRs); set when it opens, kept after merge for traceability
pr_url: ""            # implementation PR (code + implement/test/review docs); set when it opens, kept after merge
adrs: []              # ADR ids this card produced, e.g. [ADR-0007]; appended by /kanban via the adr skill (the append reserves the number; the file merges via the card's PR)
reworks: 0            # automatic test/review→implement loops consumed (budget 2); flow-metric input for /retro
started: ""           # ISO date the card left backlog (set by /kanban)
delivered: ""         # ISO date the card's PR merged (set by /kanban reconcile)
created: 2026-06-29   # ISO date
---

## Why
One paragraph: the user-facing intent and why this card exists.

## Acceptance criteria
- [ ] Observable, testable criterion one
- [ ] Observable, testable criterion two

## Notes
Free-form context. Phase docs (slice.md, design.md, …) live beside this file and hold the detail. Split lineage is recorded here as prose (e.g. "Split out of CARD-NNN" on a child, "Split into CARD-XXX, CARD-YYY" on a `split` parent).
