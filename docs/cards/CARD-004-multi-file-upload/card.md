---
id: CARD-004
type: feature
layer: domain
title: Multi-file upload with paths preserved
status: backlog
phase: backlog
right_sized: true
depends_on: [CARD-003]
branch: ""
worktree: ""
design_pr_url: ""
pr_url: ""
adrs: []
reworks: 0
started: ""
delivered: ""
created: 2026-07-09
---

## Why
A user (and later the web page's folder-drop) can upload individual files with
relative paths preserved instead of a zip. Adds `extract.FromMultipart` and the
`file`-vs-`files` mode selection. Mirrors slice S4.

## Acceptance criteria
- [ ] `POST /api/sites` with multiple `files` fields whose `filename`s contain `/`-separated relative paths → site serves the nested paths (spec "API")
- [ ] A request with both `file` and `files`, or neither, → 400 (spec "API"; 06-vertical-slices §S4)
- [ ] Shared extraction rules hold for multi-file mode: dotfile filtering, duplicate-path 400, malformed-path rejection, size/count limits, and sole-top-level-directory stripping when all paths share one root dir (spec "Extraction rules")

## Notes
Marked right_sized at intake: a single small change (one extraction entry point plus
mode selection) reusing CARD-003's rule engine.
