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
