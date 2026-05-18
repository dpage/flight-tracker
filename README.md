# flight-tracker

Single-binary Go + React app that tracks your friends' flights on a live world map.
Built for the small ritual of "who's already in the air to PostgreSQL Conference Europe?"

- **Backend**: Go 1.26, `net/http`, `pgx/v5`, GitHub OAuth, Server-Sent Events.
- **Frontend**: Vite + React 18 + TypeScript + MUI + Zustand + MapLibre GL.
- **Data source**: FlightAware AeroAPI (live) or an in-process stub that
  interpolates positions along a great-circle (no API key needed for dev).
- **Deploy**: one statically-linked binary with the SPA embedded via `//go:embed`,
  fronted by nginx + Let's Encrypt.

## Quickstart (local development)

Prerequisites: Go ≥ 1.26, Node ≥ 20, a running PostgreSQL with a database for the app.

```bash
# 1. Create a Postgres database.
createdb flight_tracker

# 2. Configure environment.
cp .env.example .env
# Edit .env — fill in DATABASE_URL, GITHUB_CLIENT_ID/SECRET, SESSION_KEY (32+ chars).
# Leave AEROAPI_KEY empty to use the stub backend.

# 3. Register a GitHub OAuth app at https://github.com/settings/developers
#    Homepage URL:           http://localhost:8080
#    Authorization callback: http://localhost:8080/auth/github/callback

# 4. Build everything and run.
make build
make run
# Browse to http://localhost:8080
```

For frontend hot-reload during development, use `make dev` — that starts the Go
server on :8080 and Vite on :5173 with a proxy for `/api`, `/auth`, `/healthz`.

The first GitHub user to sign in is automatically marked as a superuser. They
can then invite others by GitHub login from the **Manage users** dialog in the
top bar.

## Configuration

All configuration is via environment variables (see `.env.example`).

| Variable                 | Required | Default                                       | Notes |
|--------------------------|----------|-----------------------------------------------|-------|
| `LISTEN_ADDR`            |          | `:8080`                                       | |
| `PUBLIC_URL`             |          | `http://localhost:8080`                       | Used for the OAuth callback URL. |
| `DATABASE_URL`           | yes      |                                               | Standard libpq URL. |
| `GITHUB_CLIENT_ID`       | yes      |                                               | From the GitHub OAuth app. |
| `GITHUB_CLIENT_SECRET`   | yes      |                                               | From the GitHub OAuth app. |
| `SESSION_KEY`            | yes      |                                               | ≥ 32 random chars. `openssl rand -base64 48`. |
| `AEROAPI_KEY`            |          |                                               | FlightAware AeroAPI key. Empty ⇒ stub backend. |
| `AEROAPI_BASE_URL`       |          | `https://aeroapi.flightaware.com/aeroapi`     | |

Database migrations are applied automatically on every startup from the
embedded `migrations/` directory.

## Stub vs. live flight data

`flight-tracker` ships with two AeroAPI backends:

- **Stub** (default when `AEROAPI_KEY` is empty): looks up origin/destination
  airport coordinates from a small embedded IATA table, then interpolates the
  plane's position along a great-circle from `scheduled_out` to `scheduled_in`.
  Sufficient for demos and local development.
- **Live**: calls FlightAware AeroAPI v4. Set `AEROAPI_KEY` and the poller will
  fetch real status, ETAs, and positions every 60 seconds for any flight in
  its active window (scheduled departure −30 min through arrival +30 min).

## Architecture

```
cmd/server/          Entrypoint: config, DB pool, migrations, HTTP server, poller goroutine.
internal/
├── config/          Env-var parsing and validation.
├── db/              pgx pool + embedded-SQL migrator.
├── store/           Typed pgx queries for users, flights, passengers, positions.
├── api/             Shared JSON DTOs (used by both handlers and poller).
├── auth/            GitHub OAuth flow, HMAC-signed session cookies, middleware.
├── aeroapi/         Flight-data client. Live (FlightAware) and Stub implementations.
├── poller/          Background goroutine: refresh active flights, persist, broadcast.
├── sse/             Server-Sent Events broadcast hub.
└── handlers/        JSON API endpoints and SPA fallback handler.

migrations/          0001_init.up.sql, .down.sql, etc. Embedded into the Go binary.
web/                 Vite + React SPA. After `npm run build`, web/dist is embedded too.
deploy/              Example systemd unit and nginx config for the Hetzner host.
```

## Deployment (Hetzner / single VM)

The Go binary embeds the SPA and runs the poller in the same process, so
deployment is a single file plus a systemd unit.

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

Then drop `deploy/nginx.conf.example` into `/etc/nginx/sites-available/`,
adjust the hostname, symlink into `sites-enabled`, and reload nginx. The SSE
endpoint needs `proxy_buffering off` — that block is already in the example.

## Project status

Pre-release. The schema, API surface, and stub backend are in place; live
AeroAPI integration is implemented but unverified against a paid account.
Tests are intentionally minimal at v0; the bones are in place to add Vitest
and a Go integration test suite next.

## Licence

PostgreSQL License — see [LICENSE](LICENSE).

## Author

Dave Page
