# Praxis

Praxis is a small, French, phone-first habit tracker. SQLite is its source of truth.

## Local MVP

The local version includes:

- password login with a signed 30-day session;
- the default habits, seeded only into an empty database;
- daily checkboxes and an optional `1, 2, 3, 5, 8, 13` rating;
- generic daily measurements managed on the `Mesures` page: name, unit, precision (integer, 0.5, 0.25, 0.1 or 0.01), min/max bounds, optional target range, help text, drag ordering, and archival;
- seeded `Sommeil` (0–24 h, quarter-hour precision, target 7–9 h) and `Cafés` (0–20, target 3 or less) measurements;
- past-day editing through a monthly calendar;
- rolling 30-day charts, recorded-day counts, and averages for every active measurement;
- optional per-device Web Push reminders at 20:00 America/Toronto for blank measurements that opted into reminders (new measurements default to off);
- habit creation, ordering, criteria, activation, and archival;
- CSV export: `/export.csv` keeps its historical columns, and `/measurements.csv` lists every recorded value as `date,mesure,nom,unite,valeur`.

A measurement with recorded values can only be archived. Archiving hides it from new entries and trends, keeps its values visible on the days that have them, and keeps them in the export. Bounds or precision changes that would exclude recorded values are rejected. The measurement key is derived from the first name and never changes, so CSV and Grafana queries stay stable after a rename.

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

Praxis refuses to listen outside localhost unless both password and session key are explicitly set. Web Push stays disabled unless both VAPID keys are present. On iPhone and iPad, install Praxis on the Home Screen before enabling notifications.

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

`v0.3.0` adds the `measurements`, `measurement_values`, and `schema_markers` tables. On first start it seeds `Sommeil` and `Cafés` once and copies the legacy `days.sleep_minutes` and `days.coffee_count` values into them. The legacy columns stay and mirror the `sommeil` and `cafes` values on every save, so a `v0.2.x` binary can be used as a rollback without data loss for those two measurements. Values written by `v0.2.x` during a rollback are copied back on the next `v0.3.0` start when the date has no generic value yet.

The production homelab repository owns Grafana reporting, R2 backups, Kubernetes manifests, Authentik ForwardAuth, and `praxis.lamiphil.dev` TLS.
