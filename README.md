# Praxis

Praxis is a small, French, phone-first habit tracker. SQLite is its source of truth.

## Local MVP

The local version includes:

- password login with a signed 30-day session;
- the default habits, seeded only into an empty database;
- daily checkboxes and an optional `1, 2, 3, 5, 8, 13` rating;
- nullable sleep duration (quarter-hour precision) and caffeinated-coffee count trackers;
- fixed feedback goals of 7–9 hours of sleep and no more than 3 coffees;
- past-day editing through a monthly calendar;
- rolling 30-day charts, recorded-day counts, and averages;
- optional per-device Web Push reminders at 20:00 America/Toronto when a tracker is blank;
- habit creation, ordering, criteria, activation, and archival;
- CSV export, including sleep and coffee values.

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
| `PRAXIS_TRUSTED_HEADER` | empty | Authenticates requests carrying this trusted reverse-proxy header |
| `PRAXIS_VAPID_PUBLIC_KEY` | empty | Enables Web Push with the matching public VAPID key |
| `PRAXIS_VAPID_PRIVATE_KEY` | empty | Private VAPID key; keep it secret |
| `PRAXIS_VAPID_SUBSCRIBER` | `mailto:admin@lamiphil.dev` | VAPID contact identifier |

Praxis refuses to listen outside localhost unless both password and session key are explicitly set. Web Push stays disabled unless both VAPID keys are present.

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

## Data compatibility

Schema upgrades only add nullable columns and new tables. Existing dates remain blank until backfilled. The `v0.1.3` binary can still open an upgraded database and ignores the tracker and Web Push data.

The production homelab repository owns Grafana reporting, R2 backups, Kubernetes manifests, Authentik ForwardAuth, and `praxis.lamiphil.dev` TLS.
