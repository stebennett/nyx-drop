# nyx-drop

A self-hosted [Cloudflare Drop](https://drop.cloudflare.com) clone for Kubernetes.
Upload static assets (a zip or a set of files) and get back a temporary site on a
random animal-slug subdomain (`trusty-tahr-x7k2mq.sites.nyxhub.net`) that expires
after a global TTL unless an admin marks it permanent.

Single Go binary, SQLite + PVC storage, wildcard-ingress host routing, GitHub OAuth
admin, token/grant-gated upload API. See `docs/superpowers/specs/` for the full
design and `docs/implementation/` for build notes.

**Current state:** Token-gated ZIP uploads and static serving (CARD-003 / slice S3).
`POST /api/sites` creates a site, and its returned URL is public until expiry.
GitHub OAuth sign-in is available for the configured admin. Multi-file uploads,
browser upload grants, PUT, admin site management, and on-disk cleanup of expired
sites are not implemented yet. `/healthz` checks SQLite and data-directory
writability.

## Configuration

All configuration is via environment variables, validated fail-fast at startup
(a missing or invalid value prints an error naming the variable to stderr and
exits 1).

| Variable | Required | Default | Notes |
|---|---|---|---|
| `BASE_DOMAIN` | yes | — | Multi-label DNS hostname, e.g. `sites.nyxhub.net`. No scheme, port, or path. |
| `SCHEME` | no | `https` | `http` or `https`. |
| `PUBLIC_PORT` | no | unset | Browser-facing port, `1..65535`. When unset, HTTP uses `PORT` for direct local access and HTTPS assumes `443` for ingress. The standard port for the scheme is omitted from the URL. |
| `TTL` | no | `24h` | Site lifetime, `time.ParseDuration` syntax, must be `> 0`. |
| `UPLOAD_TOKEN` | yes | — | Bearer token gating the upload API. |
| `GITHUB_CLIENT_ID` | yes | — | GitHub OAuth app client ID (admin login). |
| `GITHUB_CLIENT_SECRET` | yes | — | GitHub OAuth app client secret. |
| `ADMIN_GITHUB_USER` | yes | — | GitHub username allowed to sign in as admin (case-insensitive). |
| `SESSION_SECRET` | yes | — | Secret used to sign admin session cookies. |
| `DATA_DIR` | no | `/data` | Must already exist and be writable by the process. Holds SQLite plus app-owned `tmp/` and `sites/` on the same filesystem. |
| `MAX_UPLOAD_SIZE` | no | `100MB` | Per-upload size cap. Accepts decimal (`KB`/`MB`/`GB`), binary (`KiB`/`MiB`/`GiB`), bare single-letter decimal aliases (`K`/`M`/`G`), or bare-byte sizes. |
| `MAX_SITE_SIZE` | no | `500MB` | Per-site total size cap, same size syntax as above. |
| `MAX_FILE_COUNT` | no | `10000` | Max files per site, must be `> 0`. |
| `PORT` | no | `8080` | Listen port, `1..65535`. |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, or `error` (case-insensitive). |

## Local development

`*.localtest.me` resolves to `127.0.0.1` publicly, so running with
`BASE_DOMAIN=localtest.me SCHEME=http` gets you working wildcard subdomains with
zero `/etc/hosts` changes: `http://localtest.me:8080` is the app, and
`http://<slug>.localtest.me:8080` serves a site (host normalization strips the
`:8080`).

The branded 404's deploy link uses the configured app origin. For a direct
local run on another port, set `PORT` (e.g. `PORT=8181`). If a proxy or container
mapping exposes a different port (e.g. `-p 9080:8080`), set `PUBLIC_PORT=9080`
and leave `PORT=8080`. `PUBLIC_PORT` does not change where the app listens.

```sh
export BASE_DOMAIN=localtest.me SCHEME=http PORT=8080 \
       UPLOAD_TOKEN=dev-token SESSION_SECRET=dev-secret-please-change \
       GITHUB_CLIENT_ID=... GITHUB_CLIENT_SECRET=... ADMIN_GITHUB_USER=you \
       DATA_DIR=./tmp/data TTL=1h
mkdir -p "$DATA_DIR"
go run ./cmd/drop
```

Then, in another terminal:

```sh
curl http://localtest.me:8080/healthz   # -> 200 ok
curl http://localtest.me:8080/metrics   # -> Prometheus exposition format
```

With `zip` and `curl` installed, publish a test site (use the same local test
token as the server):

```sh
mkdir -p ./tmp/demo
printf '<h1>Hello from Nyx Drop</h1>\n' > ./tmp/demo/index.html
zip -j ./tmp/demo.zip ./tmp/demo/index.html
curl -i --noproxy '*' --resolve localtest.me:8080:127.0.0.1 \
  -H 'Authorization: Bearer dev-token' -F 'file=@./tmp/demo.zip' \
  http://localtest.me:8080/api/sites
```

Expect `201` with `id`, `url`, and `expires_at` in UTC. Open `url` to see the
uploaded page. Without wildcard DNS, use `curl --noproxy '*' --resolve
<returned-host>:8080:127.0.0.1 <returned-url>` with the returned host and URL.

## Admin sign-in

Register a GitHub OAuth app with callback `<public origin>/auth/callback`,
including any non-default public port. Leave wildcard callback matching off.
Set `GITHUB_CLIENT_ID`,
`GITHUB_CLIENT_SECRET`, `ADMIN_GITHUB_USER` (the single permitted login), and
`SESSION_SECRET` (a strong random secret). Use HTTPS in production. For local
HTTP only, use a separate OAuth app with callback
`http://localtest.me:8080/auth/callback` (adjust for `PORT` or `PUBLIC_PORT`).

Visit `/admin` to sign in. The page currently offers sign-out; the site list
and management actions are separate cards. Sign-out clears the browser's
7-day session cookie; it does not revoke a copy. Rotating `SESSION_SECRET`
invalidates existing sessions and sign-ins in progress.

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
  --tmpfs /data:rw,uid=65532,gid=65532,mode=0700 \
  -e BASE_DOMAIN=localtest.me -e SCHEME=http \
  -e UPLOAD_TOKEN=dev-token -e SESSION_SECRET=dev-secret \
  -e GITHUB_CLIENT_ID=... -e GITHUB_CLIENT_SECRET=... -e ADMIN_GITHUB_USER=you \
  nyx-drop
```

The image is a two-stage build ending in `gcr.io/distroless/static:nonroot`,
running as uid `65532`. The example uses ephemeral storage; for a persistent
database, mount a writable volume at `/data` instead. Frontend assets are
embedded in the binary via `embed.FS`; nothing else is copied into the image.

## Repository layout

```
cmd/drop/          process entrypoint (config -> logger -> clock -> server -> http.Server)
internal/auth/     GitHub OAuth and signed admin credentials
internal/clock/    injected Clock (Real, Fake) — never time.Now() in handlers
internal/config/   env parsing/validation (Load), human-readable size parsing (ParseSize)
internal/extract/  ZIP validation and private staging
internal/lock/     per-site mutation coordination
internal/metrics/  Prometheus registry wiring
internal/server/   HTTP routing, middleware, page rendering
internal/slug/     random DNS-safe site IDs
internal/store/    SQLite site metadata, migrations, readiness probe
web/                embedded frontend assets (embed.FS)
```

See `docs/implementation/06-vertical-slices.md` for the remaining build order.
