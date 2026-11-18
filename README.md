# Praxis

Praxis is a small, French, phone-first habit tracker. SQLite is its source of truth.

## Local MVP

The local version includes:

- password login with a signed 30-day session;
- the 14 agreed habits, seeded only into an empty database;
- daily checkboxes and an optional `1, 2, 3, 5, 8, 13` rating;
- past-day editing through a monthly calendar;
- habit creation, ordering, criteria, activation, and archival;
- CSV export.

Run it from this directory:

```fish
set -lx GOPATH $HOME/go
set -lx PRAXIS_PASSWORD 'choose-a-password'
go run .
```

Open <http://127.0.0.1:8080>. If `PRAXIS_PASSWORD` is unset, the localhost-only test password is `praxis`.

Run the checks:

```fish
set -lx GOPATH $HOME/go
go test ./...
```

## Local configuration

| Variable | Default | Purpose |
|---|---|---|
| `PRAXIS_ADDR` | `127.0.0.1:8080` | Listen address |
| `PRAXIS_DB` | `data/praxis.db` | SQLite database path |
| `PRAXIS_SEED_FILE` | `seed.json` | First-run habits |
| `PRAXIS_PASSWORD` | `praxis` on localhost | Login password |
| `PRAXIS_SESSION_KEY` | local test key | Signs session cookies |
| `PRAXIS_SECURE_COOKIE` | `false` | Set to `true` behind HTTPS |

Praxis refuses to listen outside localhost unless both password and session key are explicitly set.

## Container image

The image runs as a non-root user (UID 65532), listens on `0.0.0.0:8080`, and stores SQLite in `/data/praxis.db`. Because it listens outside localhost, `PRAXIS_PASSWORD` and `PRAXIS_SESSION_KEY` are required.

```fish
docker build -t praxis:dev .
docker run --rm -p 8080:8080 -v praxis-data:/data \
    -e PRAXIS_PASSWORD=choose-a-password -e PRAXIS_SESSION_KEY=(openssl rand -hex 32) praxis:dev
```

Pushing a `vX.Y.Z` tag publishes `ghcr.io/lamiphil/praxis:X.Y.Z` and `sha-<commit>` for `linux/amd64` and `linux/arm64`. No `latest` tag is published. Pushes to `main` and pull requests only test and build the image.

```fish
git tag v0.1.0
git push origin v0.1.0
```

## Not in this local MVP

Grafana reporting, Prometheus metrics, backups, Kubernetes manifests, and `praxis.lamiphil.dev` TLS are intentionally deferred until the daily workflow is validated.
