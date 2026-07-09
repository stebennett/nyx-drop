# Knowledge

Cross-card knowledge captured by `/kanban` from phase agents. Entries are prefixed
`[CARD-NNN]`. Decisions live in ADRs (`adr_dir`), not here.

## Conventions
- [CARD-001] Card scopes for this project are designed to map 1:1 onto the slices in `docs/implementation/06-vertical-slices.md`; card-slicer should treat that doc as strong evidence for the right-sizing verdict on infra/foundation cards, since the project's own architects already vertical-sliced the build order there.

- [CARD-001] Config is parsed by `config.Load(getenv func(string) string) (*Config, error)`, not by reading `os.Getenv` inside the package. `main` injects `os.Getenv`; tests inject a `map`-backed getenv. Keeps config parsing a pure, table-testable function with no process-env mutation.
- [CARD-001] Frontend assets are embedded through a single `web` package (`web/embed.go` with `//go:embed`), imported as `nyx-drop/web`. Go embed cannot reach parent directories, so the directive must live in the `web/` tree, not in `internal/server`. Later cards add files to the embed set here.
- [CARD-001] `server.New(server.Deps{...}) (http.Handler, error)` takes a `Deps` struct, not a positional arg list, so later cards add wiring (store, locks, auth) by adding fields without breaking the signature. Health readiness is an injected `func(context.Context) error` (S1 stub returns nil; CARD-002 supplies the real DB-ping + data-dir-writability check).

## Gotchas
- [CARD-001] Invariant 9 (handlers use the injected Clock, never `time.Now()`) governs BUSINESS time (expiry). Request-latency measurement in the logging/metrics middleware must use `time.Now()`/`time.Since` (the monotonic wall clock) — a frozen fake Clock would report `dur_ms=0` and is semantically wrong for latency. Do not route latency timing through `clock.Clock`.

## Glossary
