# Protocol Addendum (project-specific)

Project-specific doctrine that layers **on top of** the plugin's `AGENT-PROTOCOL.md`.
Every phase agent reads the plugin protocol first, then this file. Rules here refine
or add to the shared contract for **this repository only** — they never override the
structured-return format or the sole-writer invariant.

`/retro` appends project-specific process lessons here, each prefixed
`[retro-YYYY-MM-DD]`. Universal lessons belong in the plugin instead — `/retro`
flags those as a plugin PR rather than writing them here.

## Board state reaches `main` through a PR

**`main` is protected. Nothing is ever pushed to it directly.**

A `branch-guard.sh` hook rejects `git push origin main` outright
(`BLOCKED: Attempted push to protected branch`). This repository therefore delivers
board state — `BOARD.md`, `KNOWLEDGE.md`, every `card.md`, `docs/adrs/`, and post-PR
artifacts like `pr-review.md` — on a branch, through its own pull request.

This **refines** `/kanban`'s Section 5 step 5, which says to commit state changes to
`main` and push. The commit still happens; the push does not. It also refines Section 7,
which says merely to *report* a branch-protection refusal — here the refusal is expected,
and the orchestrator is expected to route around it rather than report and stop.

### For the orchestrator (`/kanban`)

Commit board state to local `main` during the pump exactly as the plugin doctrine
describes. Do **not** retry the rejected push. Then, at the end of the pump — or whenever
board commits have accumulated — deliver them:

1. `git branch chore/kanban-board-state-<scope> main` — capture the commits.
2. `git push -u origin chore/kanban-board-state-<scope>`.
3. `git reset --keep origin/main` — rewind local `main` so it cannot diverge. **Only after
   step 2 has succeeded**, so the commits already exist on the remote. `--hard` is blocked
   by `destructive-guard.sh` in any case; `--keep` is equivalent on a clean tree and
   refuses to clobber uncommitted work.
4. `gh pr create --base main`, with a body summarising the card transitions, any new cards,
   and any new `KNOWLEDGE.md` entries.

Before pushing, check the board branch for file overlap with any open card PR
(`git diff --name-only origin/main..<branch>`). Board PRs touch only `docs/cards/**` and
`docs/adrs/**`, so they normally merge cleanly against a card PR in either order — but
verify rather than assume.

A board-state PR carries no code and no test surface, so it may show **no CI checks at
all**. Per `/kanban` Section 6a that is reviewable, not blocked: the gate requires that no
check is *failing*, not that checks exist.

### Rebase card branches onto `origin/main`, never local `main`

Because board commits sit on local `main` until their PR merges, a card branch rebased onto
local `main` will silently absorb them into its diff. The deliver phase MUST
`git fetch origin main` and rebase onto **`origin/main`**.

### For phase agents

- **Never push `main`, and never attempt to work around the hook** — not by force, not by
  switching the remote, not by rewriting the ref. If something appears to require it, return
  `blocked` and say so.
- The **deliver** phase pushes the *card branch* only (`task/NNN-slug`,
  `<type>/NNN-slug-design`). That is permitted and is how its PR opens. It never pushes
  `main`, and never merges its own PR.
- Board state remains off-limits to write, as the plugin protocol already states. Propose
  `knowledge` and `proposed_adrs` in your structured return; the orchestrator persists them
  and ships them in a board PR.

### Other guard hooks in this repo

- `destructive-guard.sh` blocks `rm` under `docs/` and any `git reset --hard`. To discard a
  stray file, move it to the session scratchpad rather than deleting it.
- It also blocks compound `git checkout` commands containing `--force`, even when the
  `--force` belongs to a later `git worktree remove`. Split such chains into separate calls.
