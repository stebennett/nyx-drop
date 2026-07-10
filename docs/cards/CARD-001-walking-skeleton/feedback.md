**Driver chose:** *Seed the panel.*
> Dispatch 6 pr-expert-reviewer agents in parallel (design, functionality, security on opus;
> simplicity, tests, readability on sonnet). Each posts one COMMENT review with [lens]-prefixed
> inline comments — none approve or request changes. You then review, and anything you 👍 gets
> addressed. This is the first code in the repo, so the package boundaries and wiring seams here
> are load-bearing for CARD-002…011 — the cheapest moment to catch a bad seam is now.
**Context:** the diff is 35 files (18 `.go`, 6 `.md`, Dockerfile, CI workflow, go.mod/go.sum,
embedded HTML, 4 fuzz-corpus files). No `.py` or `.ts`, so the python/typescript lenses do not
apply. CI: both jobs green on the workflow's first-ever run. PR state MERGEABLE / CLEAN.
**Driver chose:** *Delete the 2 duplicate comments.*
> Delete the inline comments belonging to review 4670334597 via the API, leaving review
> 4670334233's identical pair intact. The stray review's summary body will remain visible
> (GitHub offers no way to remove it), but the address loop will then see each finding exactly
> once. Irreversible, and it edits a public PR — hence asking.
**Cause:** the `tests` lens's first `gh` review POST silently submitted a different concurrent
lens's payload. It detected the swap by checksumming its request body and re-fetching the
created review by id, then retried and posted its own `[tests]` review (`4670343576`). No
findings were lost. Mitigation recorded in `KNOWLEDGE.md` under Gotchas.
