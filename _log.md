# Praxis - Log

## 2026-11-25 - Generic measurements

**2026-11-25**

- Replaced the hard-coded sleep and coffee trackers with generic daily measurements, managed on a new top-level `Mesures` page.
- Each measurement has a stable key, name, unit, precision, bounds, optional target range, help text, order, opt-in reminder, and archival. Measurements with values can only be archived; definition changes that exclude recorded values are rejected.
- Migrated sleep and coffee into seeded `Sommeil` and `Cafés` measurements, kept the legacy columns as a rollback mirror, and rehearsed upgrade, v0.2.1 rollback, and re-upgrade on a copy of production data.
- `Journée`, `Tendances`, reminders, `/export.csv`, and the new long-form `/measurements.csv` now read the generic tables. The homelab Grafana dashboard gets a measurement selector.

## 2026-11-25 - Settings for reminders

**2026-11-25**

- Added the top-level `Paramètres` page to the desktop and mobile navigation.
- Moved the per-device Web Push reminder controls from `Tendances` to `Paramètres`; the reminder remains fixed at 20:00 America/Toronto.
- Kept the root-scoped service worker, VAPID flow, and enabled, disabled, unsupported, denied, loading, and error states unchanged.
- Added route and navigation coverage to ensure `Tendances` has no reminder UI and `Paramètres` has the working reminder controls.

## 2026-11-25 - Sleep and coffee tracking

**2026-11-25 | 08:33**

Released sleep and coffee tracking with trends and Web Push reminders (v0.2.0, v0.2.1).

- Added nullable sleep and coffee fields through an additive SQLite migration, saved atomically with habits.
- Added the Tendances page with 30-day charts, goals, and per-device push opt-in.
- Added an evening Web Push reminder when a measurement is missing, with same-day catch-up.
- Fixed mobile push in v0.2.1: root-scoped service worker and installable web-app manifest.

### Next step

- Decide whether to catch up a missed reminder after an overnight outage.
