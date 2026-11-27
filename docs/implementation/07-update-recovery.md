# REVIEW-02 — Crash-safe PUT rollback and restart recovery

**Status:** design for [CARD-006](../cards/CARD-006-update-site-put/card.md),
with a startup ordering contract for [CARD-005](../cards/CARD-005-reaper/card.md).
No runtime behavior is implemented by this document.

## Decision and invariants

Keep the stable URL, `/data/sites/<id>` layout and two same-filesystem renames.
Use a durable SQLite update intent and a private backup outside the ordinary tmp
sweep. A rename makes one pathname change atomic; it does not make the two renames
and the metadata update one transaction. Deleting the backup before SQLite commits
can lose the last good version, and a blind startup sweep can turn an interrupted
update into deletion of a valid site. An immutable generation layout with a DB
pointer would avoid the swap gap but would change serving, downloads, creation and
reaping; it is not needed for this design.

- The SQLite commit is the decision point. Before it, recover the old tree and
  leave the entire old row unchanged. After it, retain the new tree and the new
  metadata; never roll back because cleanup or response delivery failed.
- No request may serve staged bytes or pair a committed row with the wrong tree.
  A brief public 404 during the swap is acceptable. Admin snapshots serialize
  with updates, reaping, delete and permanence changes under the same site lock.
- A successful PUT resets `updated_at`, `expires_at`, `size_bytes` and `file_count`
  together at the same URL, preserving `id`, `created_at`, `source` and `permanent`.
  The clock still applies to the selected revision; recovery never recomputes TTL.

## Persistent state and ownership

- `L`: live `/data/sites/<id>`; `N` and `B`: `new` and `old` under
  `/data/updates/<id>/<op-id>/`. The application generates a unique operation ID;
  it does not use an upload name. `updates` is private and is never served or
  traversed by the generic tmp/site sweeps. `drop.db`, its WAL, and all three
  directory locations must be on the same persistent filesystem.
- Add nullable `sites.content_generation` (existing and newly created sites start
  untagged). On PUT, use the operation ID as the new generation. Write a regular,
  app-owned `.nyx-generation` at the staged tree root after extraction; exclude
  it from file counts, byte totals, static serving and admin downloads. It cannot
  come from an upload. An untagged tree is valid only when the expected generation
  is null and the marker is absent, not malformed or unreadable.
- Add an `update_intents` table with one row per site (site ID as PK, restrictive
  FK to `sites`), unique operation ID, expected previous generation (nullable),
  and phase `prepared` or `committed`. The operation ID is also the expected new
  generation. The canonical site row holds the authoritative metadata; no duplicate
  old/new TTL or timestamp snapshots are used to infer a commit. Persist the
  metadata and generation update and the `prepared` → `committed` transition in
  **one** SQLite transaction, with guards on site ID, expected generation (including
  null), and intent identity/phase. Each guarded change must affect exactly one row.
- Validate canonical single-label site IDs and fixed-format operation IDs before
  deriving paths. Never accept absolute paths or paths from the DB or upload as
  rename/removal targets. Resolve beneath trusted, app-owned directory handles;
  refuse symlinks, unexpected types or mount crossings at roots, components and
  markers. Extraction accepts only regular files and directories; serving opens
  beneath a pinned tree root without following symlinks. Cleanup must not traverse
  symlinks or mounts. Do not guess at a directory's identity or overwrite a
  destination during recovery. Unexpected entries or marker mismatches at needed
  paths are recovery errors, not permission to delete data.

## Runtime protocol

1. Authenticate with `UPLOAD_TOKEN`, enforce normal request/extraction limits,
   and extract to ordinary tmp without holding the site lock. Take the site lock,
   settle any prior intent before touching that site, and re-read the site row and
   clock. Missing or expired non-permanent sites return 404 and discard staging.
2. Validate `L` against the current generation. Move the validated staging tree
   to `N`, record its generation marker, and make both the existing old tree and
   new tree durable (including the previously untagged case). Persist the
   `prepared` intent before either live rename. If preparation cannot be proved
   durable, do not rename `L`; preserve any potentially referenced staging.
3. Fence new public opens for this site without taking its site mutex on the
   serve path; while fenced return the normal public 404. Move `L` → `B`, then
   `N` → `L`; neither destination may exist. Sync affected directories after **each** rename
   before proceeding. Keep `B`; no cleanup or metadata writes occur between the
   renames. Static serving takes a stamp from the clear fence before reading the
   committed generation/expiry, verifies a pinned `L` root against that generation,
   opens the file relative to it, and accepts the result only if the stamp is still
   current and clear. Advance the stamp on each fence transition; do not recycle
   a value while readers can still hold it. A boolean alone misses a complete swap
   during lookup. Never resolve through the live pathname a second time. An
   already-open, accepted file may finish; a raced or mismatched lookup returns 404.
4. Take one injected UTC clock sample for the updated timestamps. Commit the
   guarded metadata/generation and intent change together. Once the durable
   decision and canonical tree are known, clear the serving fence. Return 200
   with the committed expiry; count/log the update only for a known commit.
   Remove the backup and retire the intent using the cleanup rules below.
   Cleanup can be attempted before responding, but failure is not a PUT failure
   after a known durable commit.

Before the commit is attempted, or after a transaction is conclusively rolled
back, reconcile using the `prepared` rule (including after a failed rename or
filesystem sync). Return the existing structured 500 for infrastructure errors,
including ENOSPC; do not reset metadata on rollback. Validation errors keep their
existing status codes. After intent creation, rollback/cleanup use a bounded
internal context, not a cancelled request context. If that work fails, preserve
all recovery evidence and leave the affected site fenced until recovery. A commit
error, timeout or lost connection is **not** proof of rollback; neither is a
matching timestamp proof of commit. On an ambiguous commit (including intent
creation), stop same-site mutations, keep the site fenced, retain the intent and
all trees, and require startup reconciliation after the writing process is stopped.
Return generic 500 if a response is still possible, with the outcome unknown to
the client. A lost response after commit likewise does not undo the update. A
subsequent PUT is a new replacement, not an exactly-once retry.

## Restart reconciliation

Run with exclusive process ownership of the volume: open/migrate SQLite and
complete its WAL recovery, establish the durability barrier below, then reconcile
**all** update intents before any generic orphan/tmp sweep, startup expiry pass,
periodic reaper, or HTTP listener/readiness. During reconciliation the `sites`
row is the metadata authority and the intent phase determines the direction.
A `prepared` intent requires the old generation in SQLite; `committed` requires
the operation ID. No row, incorrect generation, duplicate/conflicting intent,
transient I/O error or an unrecognized tree layout is not a generic orphan:
retain evidence, log site ID, operation ID, phase and failed step (no credentials
or uploaded contents), and fail startup rather than guess or take traffic.

| Durable intent | Checked tree layout at entry | Action and restart behavior |
|---|---|---|
| None | Before intent or after retirement; `N` may exist, `B` absent | Remove abandoned private staging or an empty work directory only. Never modify `L` on the strength of untracked staging. An unreferenced `B` is unexpected: preserve and fail closed. |
| `prepared` | `L=old`, `B` absent; `N` present or absent | Old live is already correct (no swap or rollback finished). Sync the settled layout; discard `N` if present. |
| `prepared` | `L` absent, `B=old`, `N=new` | Interrupted after saving old or moving new back during rollback. Rename `B` → `L`, sync both parents, then discard `N`. |
| `prepared` | `L=new`, `B=old`, `N` absent | Interrupted after install but before commit. Rename `L` → `N` and sync both parents, then `B` → `L` and sync both parents. Discard `N` only after old live is durable. |
| `committed` | `L=new`, `N` absent; `B` present or absent | Keep the new live tree and committed metadata; resume backup removal. The new tree must already be durable before the commit, so a missing or wrong live tree is a recovery error, not a reason to fall back to `B`. |

When `L` is already the durable winner and the other copy is only cleanup debris,
that debris may be partially removed (including its marker) by an interrupted
cleanup. Do not require its marker in that case; still require a safe, real
operation-owned directory and safe traversal. Sync the settled live pathname and
both rename parents even if recovery found no rename to perform: a prior process
could have stopped after a rename but before syncing its parents. After the
winner is durable, remove only the losing tree, sync the affected directories
(including the operation directory), then durably delete the intent. Remove the
empty operation directory afterwards; a leftover empty directory is harmless.
On retry, use the same table; do not write the outcome based on cleanup progress.
At runtime a cleanup failure with a known winner leaves that revision servable;
same-site mutations must retry cleanup under the lock before proceeding. If
intent retirement is ambiguous, leave the work directory and defer further
mutations to startup; a missing intent on a live read is not a durability proof.
The periodic reaper must not bypass this guard and must continue with other sites
on error. Once recovery succeeds, the normal expiry pass uses the recovered
row, including the permanent flag and whichever TTL won. Generic orphan handling
for create/delete applies only after update recovery succeeds.

If a live site is fenced on an ambiguous update, restart with the same volume
and one recovery-capable process. If startup fails, keep the app out of service
and preserve the DB, WAL and all three trees for inspection; deleting the backup,
clearing an intent, or running a generic sweep is not a recovery procedure.

## Durability and deployment boundary

This is a single-writer design for process/pod restarts and node/power loss on a
Linux persistent volume that provides crash-consistent same-filesystem rename,
working file and directory `fsync`, and SQLite's WAL/locking guarantees. Use a
non-overlapping rollout (`Recreate` for the chart); one replica and RWO alone do
not guarantee that two writers never overlap. Before opening the DB or sweeping,
take an exclusive nonblocking volume lock on a stable lock file outside all
sweeps, held until the process stops. Fail closed on unsupported storage
or failed barriers. Volume loss, corruption, external edits and storage that
lies about sync are outside the guarantee; do not advertise this as a backup.

Sync the contents of each required tree and directories bottom-up, including the
marker and its parent; sync parents for directory creation and both source and
destination parents after each cross-directory rename. A rename without syncing
its parents is not the durability barrier (see [Linux fsync(2)][fsync]). Require
SQLite WAL with `synchronous=FULL` on writer connections, not WAL/NORMAL, which
can lose a recent decision after power loss (see [SQLite synchronous][sqlite]).
On startup, before trusting a recovered phase or removing either copy, verify
WAL/FULL and require a successful [FULL checkpoint][checkpoint] with no busy or
uncheckpointed frames, then sync the DB parent directory. This also covers a previous process crash after a
commit/sync error left readable but not necessarily durable WAL frames. On the
live path, a successful FULL commit and synced DB parent directory precede both
200 and deletion of the backup. If the barrier fails, handle it as uncertain; do
not infer safe cleanup from a readable row. Do not take a ready listener live
with unresolved recovery state. The performance cost is extra syncs and space for
both trees during the update; do not weaken ordering to save those writes.

[fsync]: https://man7.org/linux/man-pages/man2/fsync.2.html
[sqlite]: https://sqlite.org/pragma.html#pragma_synchronous
[checkpoint]: https://sqlite.org/pragma.html#pragma_wal_checkpoint

## Implementation and acceptance evidence

- **CARD-005 / S5:** expose a startup recovery-before-sweep seam; prove ordering
  before readiness and expiry and protect the update namespace. Keep existing
  create/delete orphan behavior. **CARD-006 / S6:** add the migration and guarded
  store operations, coordinator, serving gate and generation binding, then wire
  recovery at that seam. Do not ship PUT without both runtime rollback and restart
  recovery. Update the deployment contract in CARD-011 when adding the chart.
- Use real SQLite and a temp volume, an injected clock, and deterministic failure
  hooks at writes, renames, syncs and commit. Reopen the same DB/directories in a
  fresh process (kill without deferred cleanup) before/after prepare, each rename
  and parent sync, the decision commit, backup removal and intent retirement;
  repeat during rollback and during startup itself. Include a partial backup or
  staging removal, ENOSPC, and commit-return errors representing both durable
  commit and no commit. Simulate failed/blocked startup checkpoints and verify
  neither sweep nor HTTP startup runs. Process-kill tests do **not** prove power
  loss durability: exercise the barriers on the supported PVC/filesystem (including
  abrupt node loss) before claiming that guarantee there.
- At every cut assert the selected file bytes and all row fields, including exact
  pre-update metadata on rollback, TTL/size/count and unchanged permanence on
  commit, with the same ID/URL. Assert cleanup is idempotent on a second restart,
  no valid backup is swept, permanent and expiry-boundary behavior matches the
  selected row, and malformed IDs, symlinks, missing expected trees and mismatched
  phases fail closed without destructive follow-up. Under `go test -race ./...`,
  cover serialized concurrent PUTs, delete/reap/permanence/download races, and a
  public GET paused at each swap gate: only a consistent revision or 404 is allowed.
  Retain ZIP and files-mode, token/auth, size/validation, unknown/expired 404,
  structured 500 and 200 response-shape checks.
