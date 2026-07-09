---
spec_path: docs/superpowers/specs/2026-07-09-nyx-drop-design.md
gh_command: gh
board_dir: docs/cards
adr_dir: docs/adrs
wip_limit: 3
gates:
  slice: auto
  design: pr
  deliver: auto
layers:
  - infra
  - domain
  - db
  - api
  - web
gate_layer: domain
coverage_target: "90% on the core logic layer"
---

# kanban-flow configuration

The single source of project-specific tunables. `/kanban-init` creates this file;
the skills read it; **`/kanban` never rewrites it**, so it is safe to hand-edit.

- **spec_path** — the requirements document `/refine` and `card-slicer` read.
- **gh_command** — the GitHub CLI, or a wrapper script that supplies a bot/service
  identity for automation. Every `gh`/API call in the skills and agents goes
  through this. Default `gh`.
- **board_dir** / **adr_dir** — where the board (cards, templates, this file) and
  ADRs live. These are the **conventional locations the skills assume**
  (`docs/cards`, `docs/adrs`) and match `/kanban-init`'s scaffold. The skills and
  agents currently hardcode these paths in most places, so relocating them today
  also requires editing every path reference in the skills/agents — leave them at
  the defaults unless you're prepared to do that. Full parameterization (so these
  keys alone control the location) is a future enhancement.
- **wip_limit** — max cards in flight at once.
- **gates** — per-gate policy. `slice`: `auto` | `manual`. `design`: `pr` (the
  design PR is the review) | `domain` (interactive stop for `gate_layer` cards
  only) | `manual` (stop every card). `deliver`: `auto` | `manual`.
- **layers** — the project's architectural layers, **in order**. The scheduler
  uses this order as the tie-break rank when picking the next ready card. Tag each
  card's `layer` with one of these values.
- **gate_layer** — the layer that triggers the `design: domain` interactive stop
  (its rules are the riskiest to get wrong). Usually the pure-logic core.
- **coverage_target** — the test-coverage expectation agents cite.
