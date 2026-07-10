# CARD-001 — driver feedback

## 2026-07-10 · test · coverage gate: tester blocked on `renderNotFound` 71.4% per-function, while the core layer aggregates to 92.0% (target 90%). Accept, rework, or accept-and-clarify?

**Driver chose:** *Accept, but tighten the design first.*

> Advance to review, and additionally open a follow-up card to amend the design's Test
> strategy wording to say 'aggregate across the layer, not per-function', so this can't
> recur. Slower, but leaves no ambiguity on main.

**Orchestrator context presented at the gate:**
- The design's Test strategy reads "≥ 90% on the core logic layer — `internal/config`
  (Load, ParseSize, accessors), `internal/clock`, `internal/server` routing helpers,
  `renderNotFound`, and the middleware recorder". Measured as a layer aggregate this is
  **92.0%** (172/187 statements) — target met.
- The tester applied a per-function floor instead, and did so inconsistently: it passed
  `parseLogLevel` (50.0%) and `isDigits` (83.3%) unflagged, because the design does not
  name them individually. Under a true per-function rule those would also fail.
- `renderNotFound`'s 2 uncovered statements are both unreachable error returns:
  `template.ParseFS` over a compile-time `embed.FS` (parse success already proven by the
  passing happy-path test) and `tmpl.Execute` of a single-string-field struct. Covering
  them requires either changing the merged design's fixed signature
  `renderNotFound(cfg) ([]byte, error)` to accept an injectable `fs.FS`, or contriving a
  fake failure.

**Resolution:** coverage gate recorded as **pass** at 92.0% layer aggregate. No rework
credit spent (`reworks` stays 0). Follow-up card CARD-012 opened to remove the ambiguity.

## 2026-07-10 · deliver/pr-review · PR #3 CI green: seed the six-lens expert review panel, skip it, or run a reduced panel?

**Driver chose:** *Seed the panel.*

> Dispatch 6 pr-expert-reviewer agents in parallel (design, functionality, security on opus;
> simplicity, tests, readability on sonnet). Each posts one COMMENT review with [lens]-prefixed
> inline comments — none approve or request changes. You then review, and anything you 👍 gets
> addressed. This is the first code in the repo, so the package boundaries and wiring seams here
> are load-bearing for CARD-002…011 — the cheapest moment to catch a bad seam is now.

**Context:** the diff is 35 files (18 `.go`, 6 `.md`, Dockerfile, CI workflow, go.mod/go.sum,
embedded HTML, 4 fuzz-corpus files). No `.py` or `.ts`, so the python/typescript lenses do not
apply. CI: both jobs green on the workflow's first-ever run. PR state MERGEABLE / CLEAN.

## 2026-07-10 · pr-review · A concurrency bug duplicated the [simplicity] review on PR #3. Delete the duplicate inline comments, leave them, or re-post the panel cleanly?

**Driver chose:** *Delete the 2 duplicate comments.*

> Delete the inline comments belonging to review 4670334597 via the API, leaving review
> 4670334233's identical pair intact. The stray review's summary body will remain visible
> (GitHub offers no way to remove it), but the address loop will then see each finding exactly
> once. Irreversible, and it edits a public PR — hence asking.

**Cause:** the `tests` lens's first `gh` review POST silently submitted a different concurrent
lens's payload. It detected the swap by checksumming its request body and re-fetching the
created review by id, then retried and posted its own `[tests]` review (`4670343576`). No
findings were lost. Mitigation recorded in `KNOWLEDGE.md` under Gotchas.

**Executed:** deleted inline comments `3557949267` (`config.go:70`) and `3557949276`
(`config.go:101`). Verified beforehand that review `4670334233` carried surviving comments at
both lines, and that no replies were threaded under the deleted pair. PR now carries 8 inline
comments, one per distinct finding.

## 2026-07-10 · pr-review · PR #3 review `4670759127` (CHANGES_REQUESTED) — the driver's own comments, verbatim

Review body was empty; the signal is the submitted review itself. Three inline comments, all
authored by `stebennett`:

> **`cmd/drop/main.go:35`** (`3558322371`)
> Remove references to task list in comments.

> **`internal/config/config.go:101`** (`3558359575`)
> Remove the check.

> **`internal/config/size.go:29`** (`3558367428`)
> add k, m, g as supported in the comments.

The driver additionally 👍'd four panel comments, which authorises them for the address loop:
`3557945904` (`[security]` CI `permissions:`), `3557945929` (`[security]` `IdleTimeout`),
`3557948844` (`[simplicity]` `requireString`), `3557957696` (`[tests]` overflow-guard coverage).
They did **not** 👍 the remaining two panel comments (`[security]` action tag-pinning nit,
`[readability]` — its two were superseded by their own comments), so those were left unactioned.

**Signal reading:** a submitted review authorises its own inline comments and body as one atomic
unit; the 👍s authorise those specific panel comments. Both applied. All seven items were fixed,
each replied to once with `[kanban] Addressed in <commit-url>`. No thread was resolved, nothing
approved — that belongs to the human.

**One item was only partly actioned, and the reply says so:** `3557945929` asked about the
server's timeouts. `IdleTimeout` was added; **`WriteTimeout` was deliberately left unset**,
because CARD-003/004 will serve site content and accept uploads bounded by `MAX_SITE_SIZE` /
`MAX_UPLOAD_SIZE`, and a write deadline chosen before those limits are wired in risks truncating
legitimate large transfers. Flagged for revisit rather than guessed at.

Human-directed fixes, so no rework credit was consumed (`reworks` stays 0).
