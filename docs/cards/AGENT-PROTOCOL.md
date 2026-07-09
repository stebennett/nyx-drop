# Phase Agent Protocol

Every `card-*` phase agent MUST follow this protocol. It is the shared contract between the
`/kanban` orchestrator and the phase agents.

## On dispatch you receive
- `card_id` (e.g. CARD-001), `card_dir` (e.g. docs/cards/CARD-001-slug), `worktree` (absolute path
  to this card's git worktree), the full text of `card.md`, and the prior phase docs **your phase
  needs** (the orchestrator sends only those — don't expect all of them).
- Exception: the **slice** phase runs before any worktree exists, so `card-slicer` receives no
  `worktree`; it instead receives the card's current **dependents** (ids that `depends_on` it).
- A **rework** dispatch (implement phase only) additionally carries the blocking findings from the
  tester, the reviewer, or a failing CI run (job + log excerpt); fix exactly those, and push the
  branch when the dispatch notes the PR is already open.
- A **PR-comment** dispatch (implement phase: implementation-PR comments; design phase:
  design-PR comments) carries 👍-triaged PR comments (id, path, line, body); address exactly those
  and never touch the comment threads — the orchestrator replies and the human resolves.
- A **pr-review** dispatch (`pr-expert-reviewer`, one per lens after an implementation PR opens)
  carries a `lens` and the `pr_url` instead of prior phase docs.
- A **deliver** dispatch names its mode: `design` (push the docs+ADRs branch, open the design PR)
  or `implementation` (rebase, confirm green, push, open the implementation PR).

## Always, before doing anything
1. Read `docs/cards/KNOWLEDGE.md` in full.
2. Read `card.md` and the phase docs you were given.
3. Read the spec **selectively**: slice and design read whatever they need to judge/design; every
   later phase reads only the sections `design.md` cites under `## Spec references`. Never re-read
   the whole spec when the design already names its sources.

## Boundaries (sole-writer invariant)
- You MUST NOT write or edit `BOARD.md`, `KNOWLEDGE.md`, or any `card.md`. The orchestrator owns them.
- You MUST NOT write your phase doc to disk. You RETURN its full markdown as `phase_doc` (below);
  the orchestrator persists it **on the card's current branch** — slice/design docs and ADRs ride
  the **design PR**; implement/test/review docs ride the **implementation PR**. The design PR
  merges before the implementation branch is cut, so merged designs and ADRs are on `main` for
  every later card to build on.
- You MAY create and edit **code** files — but only inside `worktree` (use absolute paths under it).
  The slice and design phases produce no code; a design branch is docs-only.
- **GitHub is off-limits to phase agents**, with two exceptions: the deliver phase pushes the branch
  and opens the PR; the pr-review phase posts its lens's findings as **one `COMMENT` review** with
  `[lens]`-prefixed inline comments. No agent ever approves, requests changes, replies to, resolves,
  or reacts to PR threads — triage (👍) and resolution belong to the human.

## Doctrine (expertise every agent carries)
Distilled from expert review of this codebase and domain — treat these as standing knowledge:
- **The spec outranks your training.** Its stated acceptance criteria are binding. When plausible
  domain knowledge from memory disagrees with the project spec (`spec_path` in `config.md`), the
  spec wins — check it, don't recall it.
- **Numeric precision is a common landmine.** Binary floats and language-default rounding introduce
  representation error (a value meant to be exactly x.5 may be stored as x.4999…), so where the spec
  defines exact rounding or exact-decimal arithmetic, use the project's designated decimal/rounding
  primitive — never a language default (e.g. Python's banker's `round()`) or binary `float` — and
  never compare such values with float tolerance.
- **Parallel derived values, never blended.** When the spec defines two related but distinct
  computed quantities for different purposes (e.g. a raw measure that drives one calculation and
  an adjusted/weighted variant that drives another), using one where the other belongs is a
  blocking defect. Name which one you mean, every time.
- **As-of semantics.** Per-record figures use the values in effect on that record's date, from the
  snapshot stored on the record — not today's values, not the current reference data. Replay is
  chronological; ties (same-date records) need a deterministic order (date, then id).
- **Determinism everywhere.** Fixed clock, fixed seed data, ordered queries, no network in tests.
  A flaky test is a failing test — never re-run it to green.
- **Evidence over claims.** Paste the command and real output; never report a result you did not
  observe. Smallest change that satisfies the acceptance criteria; reuse before writing new (YAGNI).

## Your structured return (your final message — nothing else)
Emit exactly one fenced ```result block, valid YAML:

```result
status: complete            # complete | blocked | needs-input
phase: <slice|design|implement|test|review|deliver|pr-review>
card: CARD-NNN
gate: none                  # none | design | slice  (which gate this phase triggers; never "deliver")
summary:
  - "2–4 bullets a human reads at the gate or in the board"
open_questions:             # required when status is needs-input; else []
  - "Blocking question for the driver"
blockers:                   # required when status is blocked; else []
  - "What is broken and the evidence (command + output excerpt)"
knowledge:                  # may be empty — but if your phase hit a trap or set a convention, record it; an empty KNOWLEDGE.md after many cards is a process failure
  - scope: repo             # repo | personal
    section: Conventions    # Conventions | Gotchas | Glossary  (repo scope only; significant decisions are ADRs, below)
    entry: "Fact to record, prefixed mentally with [CARD-NNN]"
proposed_adrs:              # may be empty — significant architecture/technology decisions only
  - title: "Short decision title"
    context: "The forces at play"
    decision: "What we decided"
    consequences: "What becomes easier/harder"
    supersedes: []          # optional ADR ids this decision replaces, e.g. [ADR-0003]
proposed_cards:             # SLICE PHASE ONLY, with gate: slice — the child cards to create; else omit/[]
  - title: "Short imperative title"
    type: feature           # feature | task | defect
    layer: domain           # one of the project's configured layers (see config.md `layers`)
    why: "One line of user-facing intent"
    acceptance_criteria:
      - "Observable, testable criterion"
    depends_on: []          # sibling child titles and/or existing CARD ids
dependents_rewire:          # SLICE PHASE ONLY — for each existing card that depends_on the parent
  - card: CARD-NNN
    new_depends_on: []      # what it should depend on after the split replaces the parent
phase_doc: |
  <full markdown body of this phase's doc — the orchestrator writes it to card_dir/<phase>.md>
```

- `status: needs-input` → orchestrator surfaces `open_questions` to the driver and re-dispatches you with answers.
- `status: blocked` from the **tester or reviewer** with actionable findings → orchestrator auto-re-dispatches the implementer in rework mode (budget: 2 loops), then parks the card.
- `status: blocked` from any other phase → orchestrator parks the card with `blockers` shown on the board.
- `status: complete` + `gate: slice` (slice phase only) → orchestrator applies the gate policy: auto-apply the split, or surface `proposed_cards` + `dependents_rewire` to the driver.
- `status: complete` + `gate: design` → orchestrator applies the gate policy: auto-approve, or stop for the driver (domain-layer cards by default).
- The deliver gate is triggered by a card reaching `deliver` status, not by any agent `gate` value. No agent should ever emit `gate: deliver`.

## Architecture Decision Records (ADRs)

Significant **architecture or technology decisions** (framework/library choices, data-model
invariants, cross-cutting patterns, expensive-to-reverse trade-offs) are recorded as ADRs in
`docs/adrs/` (Cognitect format: Title · Context · Decision · Status · Consequences), not in
`KNOWLEDGE.md`. Small conventions → `KNOWLEDGE.md ## Conventions`; traps → `## Gotchas`.
- Any phase agent MAY return `proposed_adrs` when it makes or surfaces such a decision. You only
  *propose*: the orchestrator persists each ADR (numbering, file, index, `adrs:` card link) via
  the `adr` skill. Design-phase ADRs are written once the design gate passes (auto or approved).
- Read `docs/adrs/README.md`'s index before proposing — to reverse an earlier decision, propose a
  new ADR with `supersedes: [ADR-NNNN]`; never propose a duplicate of a standing one.
