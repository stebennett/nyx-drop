---
id: ADR-0002
title: "HTTP observability contract: ops-endpoint bypass and route-class taxonomy"
status: Accepted
date: 2026-07-09
card: CARD-001
supersedes: []
superseded_by: ""
---

# ADR-0002: HTTP observability contract: ops-endpoint bypass and route-class taxonomy

## Context

The spec matches `/healthz` and `/metrics` before host routing (invariant 8) and
labels the request histogram by route class `app, site, admin`. Kubelet probes hit
`/healthz` every ~10s and Prometheus scrapes `/metrics`; folding those into the
access log and the request histogram would flood logs and dominate the metric. The
route-class label is cross-cutting — every later card's handlers are classified by
it.

## Decision

`/healthz` and `/metrics` are matched at the top of the handler, OUTSIDE the
`requestLog`+`instrument` middleware, so they are neither logged nor observed in the
request histogram. All host-routed requests (apex/site/unknown) are wrapped by
`requestLog` (JSON slog line: host, path, method, status, dur_ms, bytes) then
`instrument` (histogram `http_request_duration_seconds{class}`). Route class: apex
host → `app`; any other host (valid site label, multi-label, or unknown) → `site`;
`admin` is reserved and assigned by CARD-008/009 when apex admin routes appear. The
registry (`prometheus.NewRegistry()`) is created in main and passed to both
`metrics.New` and the `/metrics` handler — no promauto/global registry, so tests
build their own.

## Status

Accepted

## Consequences

Access logs and the histogram reflect product traffic only; probe/scrape noise is
excluded. Later cards classify new routes by extending `routeClass` (e.g. admin
split) without re-deciding the taxonomy. Trade-off: `/healthz`/`/metrics` latency is
not in the request histogram — acceptable, since Go/process collectors and external
probes cover ops surfaces.
