---
id: CARD-011
type: task
layer: infra
title: Helm chart & production hardening
status: backlog
phase: backlog
right_sized: ""
depends_on: [CARD-006, CARD-007, CARD-010]
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
An operator installs the whole thing with one `helm install`. Chart per
05-deployment.md, golden-template tests, chart lint in CI, NOTES.txt, and README
install docs. Mirrors slice S11. Depends on the last card of each preceding
milestone (transitively, on everything).

## Acceptance criteria
- [ ] Chart matches 05-deployment.md: Deployment with 1 replica + `strategy: Recreate`, liveness/readiness probes on `/healthz`, fsGroup, ClusterIP Service, PVC (size/storageClass via values), Ingress for `<BASE_DOMAIN>` + `*.<BASE_DOMAIN>` with class/TLS-secret/annotations via values, secrets via `existingSecret` or inline values (spec "Deployment")
- [ ] `helm lint` plus `helm template` golden-file tests (default values and a fully-customized values fixture) run green in CI (spec "Testing — Chart")
- [ ] `prometheus.io/scrape` annotations present and toggleable via values; NOTES.txt and README install documentation shipped (spec "Observability"; 06-vertical-slices §S11)

## Notes
If a cluster is available, a kind/k3d smoke test (install → create site → visit via
port-forward with Host header) is the stretch acceptance; not required for done.
