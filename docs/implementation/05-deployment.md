# Deployment: Docker, Helm, CI, Local Dev

## Dockerfile

```dockerfile
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /drop ./cmd/drop

FROM gcr.io/distroless/static:nonroot
COPY --from=build /drop /drop
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/drop"]
```

Notes: `CGO_ENABLED=0` works because the SQLite driver is pure Go. Frontend assets are
`embed.FS`, so nothing else is copied. `distroless/static:nonroot` runs as uid 65532.

## Helm chart — `deploy/helm/nyx-drop/`

```
Chart.yaml
values.yaml
templates/
├── _helpers.tpl
├── deployment.yaml
├── service.yaml
├── pvc.yaml
├── ingress.yaml
├── secret.yaml        # only rendered when existingSecret is not set
└── NOTES.txt
```

### `values.yaml` schema (complete)

```yaml
image:
  repository: ghcr.io/OWNER/nyx-drop   # no default registry assumption; must be set
  tag: ""                                  # defaults to .Chart.AppVersion
  pullPolicy: IfNotPresent

config:
  baseDomain: ""          # REQUIRED, e.g. sites.nyxhub.net
  scheme: https
  ttl: 24h
  adminGithubUser: ""     # REQUIRED
  githubClientId: ""      # REQUIRED
  maxUploadSize: 100MB
  maxSiteSize: 500MB
  maxFileCount: 10000
  logLevel: info

secrets:
  existingSecret: ""      # name of a Secret with keys below; if set, inline values ignored
  uploadToken: ""         # inline alternatives (dev only)
  githubClientSecret: ""
  sessionSecret: ""

persistence:
  size: 10Gi
  storageClass: ""        # "" = cluster default
  existingClaim: ""       # use a pre-created PVC instead

ingress:
  className: nginx
  annotations: {}
  tls:
    enabled: true
    secretName: ""        # REQUIRED when tls.enabled: existing wildcard cert secret

resources:
  requests: { cpu: 50m, memory: 64Mi }
  limits: { memory: 256Mi }

metrics:
  scrapeAnnotations: true

podAnnotations: {}
nodeSelector: {}
tolerations: []
affinity: {}
```

### Template requirements

- **deployment.yaml**: `replicas: 1` (not configurable — RWO volume; add a comment
  saying why), `strategy: {type: Recreate}`, env from config values + `secretKeyRef`s
  (name = `existingSecret` or the chart-managed secret). Probes: liveness + readiness
  both `GET /healthz` port 8080 (readiness `initialDelaySeconds: 2`, liveness
  `periodSeconds: 10`). `securityContext`: `runAsNonRoot: true`, `runAsUser: 65532`,
  **`fsGroup: 65532`** — without fsGroup the PVC mounts root-owned and the app can't
  write; this is the most common deploy failure, do not omit. Also
  `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true` (all writes go to
  the volume), `capabilities: {drop: [ALL]}`. Volume mounted at `/data`. When
  `metrics.scrapeAnnotations`, add `prometheus.io/scrape: "true"`,
  `prometheus.io/port: "8080"`, `prometheus.io/path: "/metrics"` to pod annotations.
- **ingress.yaml**: two rules — host `{{ .Values.config.baseDomain }}` and host
  `*.{{ .Values.config.baseDomain }}` — both routing `/` (Prefix) to the service.
  One `tls` block covering both hosts with `secretName` when enabled.
- **secret.yaml**: rendered only if `existingSecret` is empty; `stringData` with keys
  `uploadToken`, `githubClientSecret`, `sessionSecret`. Helpers must fail the render
  (`required`) if a needed value is missing in both places.
- **NOTES.txt**: print the app URL, admin URL, and a reminder to create the DNS
  wildcard record and the GitHub OAuth app (callback `SCHEME://BASE_DOMAIN/auth/callback`).

### Chart tests

- `helm lint deploy/helm/nyx-drop`
- Golden-file tests: `helm template` with (a) minimal required values, (b) a
  fully-customized fixture; compare against checked-in expected output
  (`deploy/helm/testdata/`). A tiny Go test or shell script in CI is enough.

## CI (GitHub Actions, `.github/workflows/ci.yml`)

1. `go vet ./...`, `gofmt -l` (fail if nonempty), `go test ./...` (with `-race`).
2. Helm lint + template golden tests.
3. `docker build` (no push on PRs; push to registry on tags/main — leave the registry
   push step present but commented/documented, since the registry isn't decided).

## Local development

```sh
export BASE_DOMAIN=localtest.me SCHEME=http PORT=8080 \
       UPLOAD_TOKEN=dev-token SESSION_SECRET=dev-secret-please-change \
       GITHUB_CLIENT_ID=... GITHUB_CLIENT_SECRET=... ADMIN_GITHUB_USER=you \
       DATA_DIR=./tmp/data TTL=1h
go run ./cmd/drop
```

`*.localtest.me` resolves to 127.0.0.1 publicly, so `http://localtest.me:8080` is the
app and `http://<slug>.localtest.me:8080` serves sites with zero /etc/hosts fiddling.
(Host normalization strips the `:8080`.) For OAuth locally, create a separate GitHub
OAuth app with callback `http://localtest.me:8080/auth/callback`.

Document all of this in the README.
