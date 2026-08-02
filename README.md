# FPL Scouting

A Fantasy Premier League transfer and squad-building assistant. Two Go
services share a SQLite database and talk to you over Telegram.

## Architecture

```
FPL API → fpl-core (fetch, hourly) → SQLite → /suggest → fpl-bot (analyze on-demand) → Telegram → confirm → fpl-bot (trade) → FPL API
```

| Service | Type | Purpose |
|---|---|---|
| **fpl-core** (`services/core`) | Cron, fetch-only | Pulls FPL API data (players, history, fixtures, your team) every hour and writes it to SQLite. |
| **fpl-bot** (`services/bot`) | Long-running | Handles Telegram commands, runs on-demand analysis, builds squads, executes user-confirmed transfers via the FPL private API. |

Both services are separate Go modules, built as ~15MB Alpine images, and
capped at 50MB/60MB memory respectively. There's no separate analytics
database — everything lives in one SQLite file (schema managed by
goose migrations in `services/core/internal/database/migrations/`, 11
tables), mounted as a shared Docker volume.

## What it does

- **Scores every player** using expected goals/assists/clean-sheets (xG/xA/xCS)
  from recent match history, weighted by position.
- **`/suggest`** — compares your current squad against the rest of the
  league and proposes sell/buy transfers ranked by expected point gain,
  respecting your bank and each player's live selling price.
- **`/startsquad`** — builds an optimal 15-player squad from scratch (season
  start or a full rebuild), sweeping formations and bench-budget splits for
  the best starting XI.
- **`/report`** — top 5 players per position over the last 5 GWs, last 10
  GWs, and the full season.
- **`/recommendations`** / **`/status`** — review pending suggestions and
  check system health (player count, last fetch/analysis time).
- **Confirm/reject via Telegram buttons** — accepted transfers are executed
  against the live FPL API using your session cookie.

See `CLAUDE.md` for the exact scoring formulas and code layout.

## Setup

```bash
make install       # creates .env from .env.example — fill in your values
make build          # build both Docker images
make up              # start services (detached)
```

Required in `.env`:
- `TELEGRAM_BOT_TOKEN` — your bot's token.
- `FPL_SESSION_COOKIE`, `FPL_TEAM_ID` — needed for squad data and trading.
  Get the cookie from your browser: DevTools → Application → Cookies →
  `pl_profile`, while logged into the FPL site.

## Common commands

```bash
make deploy          # git pull, rebuild, recreate containers
make logs            # tail all logs
make status           # container status + memory usage
make fetch             # manually trigger an FPL data fetch
make shell-core         # shell into the core container
make shell-bot            # shell into the bot container
make db-copy                # copy the SQLite DB out for local inspection
```

Run `make help` for the full list.

## Deploying to a server

SSH into the server, then from the repo directory:

```bash
make deploy
```

This pulls the latest commit, rebuilds only what changed, and recreates
containers. fpl-core re-fetches from the FPL API a few seconds after every
startup, so the database is current again without a separate step.

## Telegram commands

- `/suggest` — analyze your squad, propose transfers.
- `/startsquad` — build a fresh 15-player squad.
- `/report` — top performers per position.
- `/myteam` — show your current squad.
- `/recommendations` — view pending suggestions.
- `/status` — system health.
- `/fetch` — trigger a data fetch on demand.
