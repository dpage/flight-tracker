# flight-tracker

[![CI](https://github.com/dpage/flight-tracker/actions/workflows/ci.yml/badge.svg)](https://github.com/dpage/flight-tracker/actions/workflows/ci.yml)

Single-binary Go + React app that tracks your friends' flights on a live world map.
Built for the small ritual of "who's already in the air to PostgreSQL Conference Europe?"

- **Backend**: Go 1.26, `net/http`, `pgx/v5`, GitHub OAuth, Server-Sent Events.
- **Frontend**: Vite + React 18 + TypeScript + MUI + Zustand + MapLibre GL.
- **Data sources**:
  - [OpenSky Network](https://opensky-network.org/) for live ADS-B positions (free for non-commercial use; rate-limited).
  - [AeroDataBox](https://rapidapi.com/aedbx-aedbx/api/aerodatabox/) on RapidAPI for schedule + airport + airframe lookups (cheap pay-per-call).
  - An in-memory **stub** that interpolates positions along a great-circle when nothing is configured — useful for demos with no external dependencies.
  - A **dead-reckoner** wraps whichever tracker is in use, extrapolating from the last real fix toward the destination when ADS-B coverage drops out (oceanic gaps, etc.). Estimated positions are flagged so the UI renders them with reduced opacity and a dashed outline.
- **Deploy**: one statically-linked binary with the SPA embedded via `//go:embed`,
  fronted by nginx + Let's Encrypt.

## Quickstart (local development)

Prerequisites: Go ≥ 1.26, Node ≥ 20, a running PostgreSQL with a database for the app.

```bash
# 1. Create a Postgres database.
createdb flight_tracker

# 2. Configure environment.
cp .env.example .env
# Edit .env — fill in DATABASE_URL, GITHUB_CLIENT_ID/SECRET, SESSION_KEY.
# Leave OPENSKY_USERNAME / AERODATABOX_RAPIDAPI_KEY blank for the stub backends.

# 3. Register a GitHub OAuth app at https://github.com/settings/developers
#    Homepage URL:           http://localhost:8080
#    Authorization callback: http://localhost:8080/auth/github/callback

# 4. Build everything and run.
make build
make run
# Browse to http://localhost:8080
```

`make dev` runs the Go server on `:8080` and the Vite dev server on `:5173` with a proxy for `/api`, `/auth`, `/healthz`, for frontend hot-reload.

The first GitHub user to sign in is automatically marked a superuser. They can then invite others by GitHub login from the **Manage users** dialog in the top bar.

## Configuration

All configuration is via environment variables (see `.env.example`).

| Variable                   | Required | Default                       | Notes                                                                                  |
|----------------------------|----------|-------------------------------|----------------------------------------------------------------------------------------|
| `LISTEN_ADDR`              |          | `:8080`                       |                                                                                        |
| `PUBLIC_URL`               |          | `http://localhost:8080`       | Used for the OAuth callback URL.                                                       |
| `DATABASE_URL`             | yes      |                               | Standard libpq URL.                                                                    |
| `GITHUB_CLIENT_ID`         | yes¹     |                               | From the GitHub OAuth app.                                                             |
| `GITHUB_CLIENT_SECRET`     | yes¹     |                               | From the GitHub OAuth app.                                                             |
| `SESSION_KEY`              | yes      |                               | ≥ 32 random chars. `openssl rand -base64 48`.                                          |
| `POLL_INTERVAL`            |          | `60s`                         | How often the poller refreshes active flights. Non-Enroute flights are throttled to 5×. |
| `OPENSKY_USERNAME`         |          |                               | OpenSky account for HTTP Basic Auth. Unlocks higher rate limits than anonymous.        |
| `OPENSKY_PASSWORD`         |          |                               |                                                                                        |
| `OPENSKY_ENABLED`          |          | `0`                           | Set to `1` to use OpenSky anonymously (heavily rate-limited).                          |
| `AERODATABOX_RAPIDAPI_KEY` |          |                               | When set, the Add Flight dialog drops to its minimal "ident + date" form.              |
| `DEV_AUTH_BYPASS`          |          | `0`                           | Local-only: `1` enables `/auth/dev-login?login=…` to skip OAuth. Refuses non-localhost.|

¹ Not required when `DEV_AUTH_BYPASS=1`.

Database migrations are applied automatically on every startup from the embedded `migrations/` directory.

## Tracker and resolver modes

The tracker decides where the poller gets a position for each flight; the resolver fills in the rest of a flight's metadata at creation time.

- **Tracker — stub (default)**: synthesises a position from the schedule and the embedded IATA table. No external calls. Good enough for "the plane should be roughly here" demos.
- **Tracker — OpenSky**: real ADS-B fixes keyed on the airframe's `icao24` (lowercase Mode-S hex). Requires the user (or the resolver) to record an `icao24` against the flight. Falls back to the dead-reckoner whenever OpenSky has no fresh fix.
- **Resolver — none**: the Add Flight dialog shows the full manual form (every field).
- **Resolver — AeroDataBox**: one RapidAPI call per `POST /api/flights/resolve {ident, date}` returns the full schedule, both airports with coordinates, and the `icao24`. The Add Flight dialog becomes "ident + departure date" and everything else is filled in for you.

Mix and match as you like — e.g. AeroDataBox to autofill + stub for positions during development, then OpenSky once you want real tracking.

## Architecture

```
cmd/server/          Entrypoint: config, DB pool, migrations, HTTP server, poller goroutine.
internal/
├── config/          Env-var parsing and validation.
├── db/              pgx pool + embedded-SQL migrator.
├── store/           Typed pgx queries for users, flights, passengers, positions.
├── api/             Shared JSON DTOs (used by both handlers and poller).
├── auth/            GitHub OAuth flow, HMAC-signed session cookies, middleware.
├── airports/        Embedded IATA → (lat, lon) table.
├── geo/             Great-circle helpers (slerp, bearing, haversine).
├── providers/       External flight-data integrations: Tracker (Stub, OpenSky)
│                    + Resolver (AeroDataBox) + DeadReckoner wrapper.
├── poller/          Background goroutine: refresh active flights, persist, broadcast.
├── sse/             Server-Sent Events broadcast hub.
└── handlers/        JSON API endpoints and SPA fallback handler.

migrations/          0001_init.up.sql, .down.sql, etc. Embedded into the Go binary.
web/                 Vite + React SPA. After `npm run build`, web/dist is embedded too.
deploy/              Example systemd unit and nginx config for the Hetzner host.
```

## Deployment (Hetzner / single VM)

The Go binary embeds the SPA and runs the poller in the same process, so deployment is a single file plus a systemd unit.

```bash
# On the dev machine:
GOOS=linux GOARCH=amd64 make build
scp bin/flight-tracker  user@host:/opt/flight-tracker/flight-tracker
scp deploy/flight-tracker.service user@host:/etc/systemd/system/flight-tracker.service
# Create /etc/flight-tracker.env with the env vars from .env.example.

# On the host:
systemctl daemon-reload
systemctl enable --now flight-tracker
```

Then drop `deploy/nginx.conf.example` into `/etc/nginx/sites-available/`, adjust the hostname, symlink into `sites-enabled`, and reload nginx. The SSE endpoint needs `proxy_buffering off` — that block is already in the example.

## Project status

Pre-release. Tracker and resolver paths are working end-to-end with OpenSky and AeroDataBox. Tests are intentionally minimal at v0; the bones are in place to add Vitest and a Go integration test suite next.

## Licence

PostgreSQL License — see [LICENSE](LICENSE).

## Author

Dave Page
