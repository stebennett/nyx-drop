# CARD-012 — Clarify the coverage target as a layer aggregate, not a per-function floor

## Intent
Remove a documentation ambiguity that cost CARD-001 a phase cycle and will otherwise
recur on every future card. The phrase "≥ 90% coverage on the core logic layer",
written both in CARD-001's `design.md` Test strategy and in `config.md`'s
`coverage_target`, was read by the `card-tester` as a **per-function floor**: it
blocked `renderNotFound` at 71.4% even though the named layer aggregates to 92.0% and
clears the target. The reading was self-inconsistent (`parseLogLevel` at 50.0% and
`isDigits` at 83.3% passed unflagged only because the design does not name them), and
the sole way to satisfy a per-function floor would have been to deviate from the merged
design's fixed signature `renderNotFound(cfg) ([]byte, error)` — injecting a breakable
`fs.FS` into a compile-time `embed.FS` parse purely to move a metric.

This card is **documentation only**. Its deliverable is prose edits to three files: it
fixes the wording where it was written (CARD-001's `design.md`), lifts the rule into
binding project doctrine (`PROTOCOL-ADDENDUM.md`) so no future card depends on any one
card's prose, and tightens the `config.md` value that agents cite. It designs no code,
no package, no interfaces — there are none to design.

## Acceptance criteria
Each criterion is verified by reading the resulting file for specific text (there is no
code to test). "Enforced by" cites the source of record for the requirement, since this
is a process/doctrine card and touches none of the product spec.

1. **CARD-001's `design.md` §"Test strategy" states the target as an aggregate across
   the named core-logic layer, in a form a future agent cannot read as a per-function
   floor.** Verified: the Coverage bullet contains the words "aggregate" and "not a
   per-function floor" (or equivalent explicit negation), and no longer presents the
   enumerated units in a form readable as per-function minimums.
   — Enforced by: card AC1; `CARD-001-walking-skeleton/feedback.md` (2026-07-10 test
   gate).
2. **`PROTOCOL-ADDENDUM.md` carries the rule as project doctrine**, so every future
   `card-designer` writes the Test strategy correctly and every future `card-tester`
   applies it correctly, independent of any card's prose. Verified: a new `##` section
   exists whose text binds both agent roles.
   — Enforced by: card AC2; `AGENT-PROTOCOL.md` §"Always, before doing anything" (agents
   read the addendum on every dispatch).
3. **The doctrine names the reproducible measurement command**
   (`go test -coverprofile=… ./...` → `go tool cover -func=…`, summed over the layer's
   statements). Verified: both commands appear verbatim in the new section, with the
   aggregation described as covered-over-total statements across the layer.
   — Enforced by: card AC3.
4. **The doctrine states that unreachable error paths are not to be covered by
   contriving injectable failure points that deviate from a merged design's fixed
   signatures**, naming the `template.ParseFS`-over-`embed.FS` case as the example.
   Verified: the new section contains that rule and example.
   — Enforced by: card AC4; the `renderNotFound` finding in `feedback.md`.
5. **`config.md`'s `coverage_target` value reads unambiguously as an aggregate.**
   Verified: the value string contains "aggregate" (or "layer ratio") and negates the
   per-function reading, and points at the addendum.
   — Enforced by: card AC5.
6. **No production code changes; docs only.** Verified: `git diff --name-only` on the
   implementation branch touches only `docs/cards/CARD-001-walking-skeleton/design.md`,
   `docs/cards/PROTOCOL-ADDENDUM.md`, and `docs/cards/config.md`; `go test ./...`,
   `go vet ./...`, and `gofmt -l .` produce the same result as on `main`.
   — Enforced by: card AC6 and "No production code changes. Docs only."

## In scope
- Rewriting the **Coverage** bullet of `docs/cards/CARD-001-walking-skeleton/design.md`
  §"Test strategy" (lines 355–360 today) — and only that bullet.
- Adding one `##` doctrine section to `docs/cards/PROTOCOL-ADDENDUM.md`, at the end of
  the file, matching the voice/structure of the existing "Board state reaches `main`
  through a PR" section (bold statement, then role-scoped subsections).
- Tightening the `coverage_target` front-matter value in `docs/cards/config.md`.
- Cross-referencing: the design.md bullet and the config.md value each point at the new
  addendum section, so the addendum is the single authoritative statement.

## Out of scope (YAGNI)
- **Any code change.** No `.go` file, `Dockerfile`, workflow, `go.mod`, or embedded
  asset is touched. This card ships zero code (card AC6).
- **Editing `docs/cards/KNOWLEDGE.md`.** Its existing `[CARD-001]` coverage-target
  gotcha becomes redundant once the doctrine lands and would ideally shrink
  to a pointer at the addendum — but `KNOWLEDGE.md` is orchestrator-owned under the
  sole-writer invariant, so a phase agent must not edit it. Recommended follow-up for
  the orchestrator, not a task of this card. Captured as a `knowledge` return.
- **Changing the numeric target (90%) or the definition of the core-logic layer.** This
  card clarifies the *reading* of an existing target; it does not re-scope it.
- **Promoting the lesson to the plugin's `AGENT-PROTOCOL.md`.** That file is
  plugin-owned and read live, not editable from this repo; universal lessons are
  `/retro`'s path (see Approach, alternative C).
- **Re-measuring CARD-001's coverage.** The 92.0% figure is already recorded and the
  gate is passed; this card does not re-open it.

## Dependencies & assumptions
- **Depends on:** CARD-001 (`depends_on: [CARD-001]`). It edits CARD-001's `design.md`,
  which reaches `main` via CARD-001's implementation PR; the recent commit
  `f40cc3b` shows CARD-001's design PR already merged, so the file exists on `main` at
  the path edited. If CARD-001's implementation PR has not yet merged when this card's
  implementation branch is cut, the deliver phase rebases onto `origin/main` per the
  addendum and the edit applies against the merged `design.md`.
- **Assumption:** `docs/cards/config.md` is safe to hand-edit — the file's own preamble
  states "`/kanban` never rewrites it".
- **Assumption:** `PROTOCOL-ADDENDUM.md` is the correct home for binding, project-scoped
  doctrine that every phase agent reads after the plugin protocol
  (`AGENT-PROTOCOL.md` §"Always, before doing anything", step layered by the addendum).
- No spec-section, schema, or dependency-allowlist impact.

## Approach
Fix the ambiguity in all three places it can be read, with one authoritative statement
(the addendum) that the other two reference, so the rule cannot drift apart again:

1. **`config.md` `coverage_target`** — the citable value: make it read as an aggregate
   and point at the addendum.
2. **`PROTOCOL-ADDENDUM.md`** — the authoritative doctrine: the aggregate rule, the
   reproducible measurement command, and the unreachable-error-path rule, split for
   the two agent roles that consume it (`card-designer`, `card-tester`).
3. **CARD-001 `design.md` Test strategy** — the specific text that misfired: rewrite the
   Coverage bullet to state the aggregate explicitly, fix the self-inconsistency by
   folding the config helpers into the enumerated layer, name the command, and record
   why `renderNotFound`'s uncovered lines are not debt — pointing at the addendum for
   the general rule.

**Alternatives considered**
- *A — Fix only CARD-001's `design.md` wording; leave doctrine alone.* Rejected: every
  future `design.md` restates a Test strategy from scratch and every new `card-tester`
  re-interprets it; the ambiguity recurs card by card. Card AC2 requires the durable
  doctrine. The deciding trade-off: a one-file fix is cheaper now but re-incurs the
  phase-cycle cost indefinitely.
- *B — Record the rule only in `KNOWLEDGE.md` (where it already sits informally).*
  Rejected: `KNOWLEDGE.md` is captured notes an agent reads for context, not the binding
  contract it must obey, and it is orchestrator-owned. Doctrine that changes agent
  behaviour belongs in `PROTOCOL-ADDENDUM.md`, which every agent reads immediately after
  the plugin protocol. The existing KNOWLEDGE entry stays as a cross-reference until the
  orchestrator trims it (Out of scope).
- *C — Promote the rule to the plugin's `AGENT-PROTOCOL.md` as a universal lesson.*
  Rejected here: the plugin file is read live and not editable from this repo, and the
  project-specific `coverage_target` string is what the rule anchors to, so project
  doctrine is the right home. The lesson is broadly reusable, though — worth flagging to
  `/retro` as a candidate plugin PR; that is `/retro`'s remit, not this card's.
- *D — Define the layer as whole packages and read the `-func` `total:` line, dropping
  the enumerated function list.* Partially adopted: the addendum offers the
  package-scoped `total:` line as a convenient shortcut, but the design keeps the
  enumerated-and-summed framing because that matches the existing 92.0% reference
  computation (172/187 statements over the named units) and the design's deliberate
  exclusion of `server.go`'s HTTP wiring from the "core logic" figure. Naming the layer
  explicitly is what defeats the per-function misreading.

## Interfaces
None — this card defines no code interface. The only "interface" it touches is the
doctrine text that `card-designer` and `card-tester` consume on dispatch. The exact
load-bearing prose is specified in the Implementation task list below, because the
wording *is* the deliverable and must be executable without further decisions.

## Data flow
No data, schema, migration, or runtime flow. The relevant topology is documentary — how
the coverage-target fact is stated once and referenced twice after this card:

```
config.md  coverage_target ──cites──▶  PROTOCOL-ADDENDUM.md  (authoritative doctrine)
                                            ▲
CARD-001 design.md §Test strategy ──cites───┘
(and each future design.md §Test strategy, by the doctrine's "For card-designer" rule)
```

`KNOWLEDGE.md`'s existing CARD-001 gotcha continues to hold the same fact until the
orchestrator (sole writer) trims it to a pointer — out of this card's scope.

## Implementation task list
Docs-only. There is no red→green cycle; each task is an edit whose verification is
"read the file back and confirm the specified text is present, and that `git diff`
touches nothing else". Make the edits in this order (authoritative doctrine first, then
the two references), then run the doc-consistency checks in the Test strategy.

### Task 1 — Add the doctrine section to `docs/cards/PROTOCOL-ADDENDUM.md`
File: `docs/cards/PROTOCOL-ADDENDUM.md` (modify). Append the following `##` section at
the **end** of the file (after the "Other guard hooks in this repo" subsection). Match
the existing house style — bold lead statement, role-scoped `###` subsections:

```markdown
## The coverage target is a layer aggregate, not a per-function floor

**`config.md`'s `coverage_target` names one ratio measured across the whole named
core-logic layer — covered statements over total statements, summed over the layer.
It is not a floor that each individual function must clear on its own.** A function
sitting below the number does not fail the gate as long as the layer aggregate meets
it.

This refines how every `design.md` `## Test strategy` is written and how every
`card-tester` reads it, so the ambiguity that cost CARD-001 a phase cycle
(`renderNotFound` at 71.4% blocked while its layer stood at 92.0% — see
`CARD-001-walking-skeleton/feedback.md`) cannot recur.

### For `card-designer`

State the coverage target in `## Test strategy` as a layer aggregate, explicitly:
enumerate the packages/functions that make up the core-logic layer, and say in words
"aggregate across the layer, not a per-function floor". Never present the enumerated
list in a form a reader can mistake for a set of per-function minimums.

### For `card-tester`

Measure the figure; never eyeball it per function. From the card's worktree:

    go test -coverprofile=cover.out ./...
    go tool cover -func=cover.out

The layer figure is the sum of covered statements over total statements across the
layer's functions — the per-block statement counts recorded in `cover.out`, not an
average of the per-function percentages. `go tool cover -func=cover.out` gives the
per-function breakdown so you can see which functions contribute; when the profile is
scoped to the layer's packages, its `total:` line is that aggregate directly. Compare
the one ratio to the target. Do not fail a function that sits below the target while
the aggregate clears it.

### Unreachable error paths are not coverage debt

Some statements cannot be exercised without contriving a failure the design's types
make impossible — e.g. the error return of `template.ParseFS` over a compile-time
`embed.FS`, or `tmpl.Execute` of a fixed one-field struct. Do not inject a breakable
seam (an `fs.FS` parameter, a deliberately failing writer) that deviates from a merged
design's fixed signature purely to cover such a line and move the metric. Leave it
uncovered; the layer aggregate already accounts for it, and changing a shipped
interface to satisfy a coverage number is the wrong trade. A genuinely reachable
uncovered branch is different — that is real coverage debt; treat the two apart.
```

Verify: the section is present with all three `###` subsections; both the
`go test -coverprofile` and `go tool cover -func` commands appear verbatim (AC3); the
`template.ParseFS`/`embed.FS` example and the "do not inject a breakable seam" rule are
present (AC4).

### Task 2 — Tighten the `coverage_target` value in `docs/cards/config.md`
File: `docs/cards/config.md` (modify). Replace the front-matter line (line 20):

- **From:** `coverage_target: "90% on the core logic layer"`
- **To:** `coverage_target: "90% aggregate across the core-logic layer (a layer ratio, not a per-function floor) — see PROTOCOL-ADDENDUM.md"`

Leave the descriptive bullet at the bottom of the file
("**coverage_target** — the test-coverage expectation agents cite.") unchanged; the
value string now carries the disambiguation. Verify: the value contains "aggregate",
negates the per-function reading, and references the addendum (AC5).

### Task 3 — Rewrite the Coverage bullet in CARD-001's design.md Test strategy
File: `docs/cards/CARD-001-walking-skeleton/design.md` (modify). Replace the first
bullet of §"Test strategy" (the `**Coverage:**` bullet, lines 355–360 today) in full
with:

```markdown
- **Coverage:** the `coverage_target` ("90% on the core logic layer") is an
  **aggregate across the core-logic layer — one covered-over-total-statements ratio,
  not a per-function floor.** The layer is the union of: `internal/config` in full
  (`Load`, `ParseSize`, the accessors, and helpers such as `parseLogLevel`/`isDigits`),
  `internal/clock` in full, and in `internal/server` the pure units
  `normalizeHost`/`siteLabel`/`routeClass`, `renderNotFound`, and the
  `responseRecorder`. Measure it — do not eyeball it per function: run
  `go test -coverprofile=cover.out ./...` then `go tool cover -func=cover.out`, and sum
  covered vs. total statements over the layer's functions (the per-block counts in
  `cover.out`); the ratio must be ≥ 90%. A single function below the number does not
  fail the gate while the aggregate clears it. In particular, `renderNotFound`'s two
  uncovered statements are unreachable error returns (`template.ParseFS` over a
  compile-time `embed.FS`, `tmpl.Execute` of a one-field struct); do **not** add an
  injectable `fs.FS` parameter to force them — that would deviate from this design's
  fixed signature `renderNotFound(cfg) ([]byte, error)` purely to move a metric. The
  general rule is project doctrine in `PROTOCOL-ADDENDUM.md` ("The coverage target is a
  layer aggregate, not a per-function floor"). `cmd/drop/main.go` (blocking
  `ListenAndServe`, signal wiring) is outside the layer and excluded from the figure;
  its behaviour is covered indirectly by `server`/`config` tests.
```

This edit (a) states the aggregate explicitly with an explicit negation (AC1); (b)
fixes the original self-inconsistency by folding `parseLogLevel`/`isDigits` into the
enumerated layer, so they are aggregated, never separately floored; (c) names the
command (AC3); (d) records the unreachable-error rationale and points at the doctrine
(AC4). Change nothing else in the file. Verify: the bullet contains "aggregate" and
"not a per-function floor", and `git diff` for this file shows only this bullet changed.

### Task 4 — Confirm the diff and green gates
Run the checks in the Test strategy below. No commit-per-task TDD cycle applies; commit
the three edits together (one docs commit) once Task 4 passes.

## Test strategy
This card ships **no code**, so there is no `go test` coverage figure to report for it —
and, applying this card's own doctrine to itself: a coverage target does not apply to a
docs-only change; do not manufacture one. Verification is by inspecting the resulting
text and by confirming the code build is untouched.

- **Text presence (AC1–AC5), per file:**
  - `docs/cards/PROTOCOL-ADDENDUM.md` contains a `##` section titled
    "The coverage target is a layer aggregate, not a per-function floor" with the three
    `###` subsections; `grep -F 'go test -coverprofile'` and
    `grep -F 'go tool cover -func'` both match inside it (AC3); `grep -F 'template.ParseFS'`
    and `grep -Fi 'embed.FS'` match (AC4).
  - `docs/cards/config.md`'s `coverage_target` value contains `aggregate` and
    `not a per-function floor` and `PROTOCOL-ADDENDUM.md` (AC5).
  - `docs/cards/CARD-001-walking-skeleton/design.md` §"Test strategy" first bullet
    contains `aggregate` and `not a per-function floor` and no longer reads as a
    per-function enumeration (AC1).
- **No code moved (AC6):** `git diff --name-only origin/main...HEAD` lists exactly the
  three docs files and nothing else. Because no `.go`, `go.mod`, `Dockerfile`, or
  workflow file changed, the code gates must produce the same result as on `main`:
  `go test ./...`, `go vet ./...`, and `gofmt -l .` (expected: unchanged / empty). Run
  them to demonstrate they still pass; they are evidence the change is inert to the
  build, not a coverage claim.
- **Markdown well-formedness:** the appended addendum section and the rewritten bullet
  render as valid Markdown (fenced blocks balanced, headings correct). If the repo runs
  a docs-CI/link/lint check, it must stay green; there are no cross-file links to broken
  anchors introduced (the references are plain-text file names, matching the existing
  addendum's citation style).
- **Recursion note for the tester of this card:** you are testing the card that defines
  how coverage targets are read. Do not apply a coverage floor to it — it has no code.
  The pass condition is the text-presence checks above plus unchanged code gates.

## Spec references
This is a process/doctrine card; it touches none of the product spec
(`docs/superpowers/specs/2026-07-09-nyx-drop-design.md`). Its sources of record are the
board's own doctrine and the CARD-001 record:
- `docs/cards/CARD-001-walking-skeleton/design.md` §"Test strategy" (lines 355–360) —
  the text amended by Task 3.
- `docs/cards/CARD-001-walking-skeleton/feedback.md` (2026-07-10 test gate) — the record
  of the false block, the 92.0% (172/187) layer aggregate vs 71.4% per-function reading,
  and the driver's "Accept, but tighten the design first" decision that opened this card.
- `docs/cards/PROTOCOL-ADDENDUM.md` — the file the doctrine section is appended to
  (Task 1); its "Board state reaches `main` through a PR" section is the voice/structure
  template.
- `docs/cards/config.md` (the `coverage_target` key; and "`/kanban` never
  rewrites it") — edited by Task 2.
- `docs/cards/KNOWLEDGE.md` (Gotchas, the `[CARD-001]` coverage-target entry) —
  the informal statement the new doctrine supersedes; noted for orchestrator trim, not
  edited here.
- `AGENT-PROTOCOL.md` §"Always, before doing anything" and §"Boundaries (sole-writer
  invariant)" — why the addendum is the binding home for the rule and why `KNOWLEDGE.md`
  is out of scope.
