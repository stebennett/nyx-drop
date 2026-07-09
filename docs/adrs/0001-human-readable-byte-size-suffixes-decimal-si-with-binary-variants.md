---
id: ADR-0001
title: "Human-readable byte-size suffixes: decimal SI with binary variants"
status: Accepted
date: 2026-07-09
card: CARD-001
supersedes: []
superseded_by: ""
---

# ADR-0001: Human-readable byte-size suffixes: decimal SI with binary variants

## Context

`MAX_UPLOAD_SIZE`/`MAX_SITE_SIZE` are given as strings like `100MB`/`500MB` in the
spec's Configuration table and Helm values. Go has no stdlib human-size parser, and
`MB` is ambiguous (1e6 vs 2^20). Every size limit and later size-enforcement check
depends on this being fixed and consistent, and changing it silently shifts real
upload limits.

## Decision

A custom `config.ParseSize(string) (int64, error)` (~40 lines): case-insensitive
suffixes, decimal `KB/MB/GB = 1000^n` and binary `KiB/MiB/GiB = 1024^n`, bare number
= bytes (`K/M/G` accepted as aliases of `KB/MB/GB`). So `100MB` = 100,000,000 bytes.
Rejects empty, negative, unknown-suffix, and int64-overflow inputs with an error
naming the offending value. No third-party size library (dependency policy).

## Status

Accepted

## Consequences

Least-surprise SI semantics; operators can pick binary units explicitly when they
want them. All size config flows through one parser, so the meaning can't drift
between variables. Reversing later would change effective limits, hence the record.
