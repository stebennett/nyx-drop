# nyx-drop

A self-hosted [Cloudflare Drop](https://drop.cloudflare.com) clone for Kubernetes.
Upload static assets (a zip or a set of files) and get back a temporary site on a
random animal-slug subdomain (`trusty-tahr-x7k2mq.sites.nyxhub.net`) that expires
after a global TTL unless an admin marks it permanent.

Single Go binary, SQLite + PVC storage, wildcard-ingress host routing, GitHub OAuth
admin, token/grant-gated upload API. See `docs/superpowers/specs/` for the full
design and `docs/implementation/` for build notes.

**Current state:** walking skeleton (CARD-001 / slice S1). The binary runs, answers
`/healthz` and `/metrics`, and serves the branded not-found page for any host that
isn't the apex — no site creation or serving yet.

## Configuration

All configuration is via environment variables, validated fail-fast at startup
(a missing or invalid value prints an error naming the variable to stderr and
exits 1).

| Variable | Required | Default | Notes |
|---|---|---|---|
| `BASE_DOMAIN` | yes | — | Multi-label DNS hostname, e.g. `sites.nyxhub.net`. No scheme, port, or path. |
| `SCHEME` | no | `https` | `http` or `https`. |
| `TTL` | no | `24h` | Site lifetime, `time.ParseDuration` syntax, must be `> 0`. |
| `UPLOAD_TOKEN` | yes | — | Bearer token gating the upload API. |
| `GITHUB_CLIENT_ID` | yes | — | GitHub OAuth app client ID (admin login). |
| `GITHUB_CLIENT_SECRET` | yes | — | GitHub OAuth app client secret. |
| `ADMIN_GITHUB_USER` | yes | — | GitHub username allowed to sign in as admin (case-insensitive). |
| `SESSION_SECRET` | yes | — | Secret used to sign admin session cookies. |
| `DATA_DIR` | no | `/data` | Root directory for the SQLite DB and site files. |
| `MAX_UPLOAD_SIZE` | no | `100MB` | Per-upload size cap. Accepts decimal (`KB`/`MB`/`GB`), binary (`KiB`/`MiB`/`GiB`), bare single-letter decimal aliases (`K`/`M`/`G`), or bare-byte sizes. |
| `MAX_SITE_SIZE` | no | `500MB` | Per-site total size cap, same size syntax as above. |
| `MAX_FILE_COUNT` | no | `10000` | Max files per site, must be `> 0`. |
| `PORT` | no | `8080` | Listen port, `1..65535`. |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, or `error` (case-insensitive). |

## Local development

`*.localtest.me` resolves to `127.0.0.1` publicly, so running with
`BASE_DOMAIN=localtest.me SCHEME=http` gets you working wildcard subdomains with
zero `/etc/hosts` changes: `http://localtest.me:8080` is the app, and
`http://<slug>.localtest.me:8080` would serve a site (host normalization strips the
`:8080`).

```sh
export BASE_DOMAIN=localtest.me SCHEME=http PORT=8080 \
       UPLOAD_TOKEN=dev-token SESSION_SECRET=dev-secret-please-change \
       GITHUB_CLIENT_ID=... GITHUB_CLIENT_SECRET=... ADMIN_GITHUB_USER=you \
       DATA_DIR=./tmp/data TTL=1h
go run ./cmd/drop
```

Then, in another terminal:

```sh
curl http://localtest.me:8080/healthz   # -> 200 ok
curl http://localtest.me:8080/metrics   # -> Prometheus exposition format
```

For OAuth locally, create a separate GitHub OAuth app with callback
`http://localtest.me:8080/auth/callback`.

## Testing

```sh
go test ./...                        # full suite
go test -race ./...                  # CI's gate
go test -run TestName ./internal/pkg # a single test
go vet ./...
gofmt -l .                           # must be empty
```

## Building the container image

```sh
docker build -t nyx-drop .
docker run --rm -p 8080:8080 \
  -e BASE_DOMAIN=localtest.me -e SCHEME=http \
  -e UPLOAD_TOKEN=dev-token -e SESSION_SECRET=dev-secret \
  -e GITHUB_CLIENT_ID=... -e GITHUB_CLIENT_SECRET=... -e ADMIN_GITHUB_USER=you \
  nyx-drop
```

The image is a two-stage build ending in `gcr.io/distroless/static:nonroot`,
running as uid `65532`. Frontend assets are embedded in the binary via
`embed.FS`; nothing else is copied into the image.

## Repository layout

```
cmd/drop/          process entrypoint (config -> logger -> clock -> server -> http.Server)
internal/clock/    injected Clock (Real, Fake) — never time.Now() in handlers
internal/config/   env parsing/validation (Load), human-readable size parsing (ParseSize)
internal/metrics/  Prometheus registry wiring
internal/server/   HTTP routing, middleware, page rendering
web/                embedded frontend assets (embed.FS)
```

This grows as later cards add the SQLite store, site creation/serving, the upload
page, and admin OAuth — see `docs/implementation/06-vertical-slices.md` for the
build order.
