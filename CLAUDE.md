# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

FPL Scouting is a Fantasy Premier League transfer recommendation system built with Go, running as 2 Docker services communicating via a shared SQLite database file. Optimized for minimal memory (<200MB total).

## Commands

All operations are via Make targets:

```bash
make build            # Build all Docker images
make up               # Start services (detached)
make down             # Stop services
make dev-build        # Build without cache
make dev-up           # Start in foreground (attached)
make logs             # Tail all logs
make logs-core        # Tail fpl-core logs
make logs-bot         # Tail fpl-bot logs
make status           # Show containers + memory usage
make fetch            # Manually trigger FPL data fetch
make shell-core       # Shell into core container
make shell-bot        # Shell into bot container
make install          # Create .env from .env.example
```

Database inspection:
```bash
make db-recs          # View recommendations
make db-players       # View players
make db-status        # View metadata/status
make db-history       # View player history
make db-copy          # Copy SQLite DB locally
make db-sql           # Direct SQL query tool
```

There is no `go test` suite; validation is done manually via `make fetch` and `make db-*` commands.

## Architecture

**Two Go services, one SQLite database:**

- **fpl-core** (`services/core/`) — Fetch-only. Cron-scheduled every 1h, fetches FPL API data (players, history, fixtures, user's team) and writes to SQLite. Supports `-once -fetch` CLI flag for one-shot execution.
- **fpl-bot** (`services/bot/`) — Long-running. Handles Telegram commands, runs on-demand analysis via `/suggest`, generates position reports via `/report`, and executes user-confirmed transfers via FPL private API.
- **SQLite** — Shared via Docker volume at `/data/fpl.db`. Schema in `sql/schema.sql` (9 tables). Uses WAL mode, 2MB cache. Both services read/write with 5s busy timeout for contention.

Data flow: `FPL API → fpl-core (fetch hourly) → SQLite → User sends /suggest → fpl-bot (analyze on-demand) → Telegram → User confirms → fpl-bot (trade) → FPL API`

## Code Organization

Each service follows the same layout under `services/{core,bot}/`:
```
main.go                    # Entry point
internal/
  fetcher/ or telegram/    # External API integration
  analyzer/ or trader/     # Business logic
  database/                # SQLite repository (repository pattern)
  models/                  # Data structures
```

The bot's `analyzer/` package handles on-demand scoring and recommendations. The core's `analyzer/` package is legacy (no longer scheduled).

Services are separate Go modules (`go.mod` per service). Dependencies: `go-sqlite3` (CGO, both services), `robfig/cron` (core), `telegram-bot-api` (bot).

## Key Technical Details

- **Go version**: 1.18+ with CGO enabled (required for go-sqlite3)
- **Docker builds**: Multi-stage (Go builder + Alpine runtime), static binary with `-ldflags="-w -s"`, final images ~15MB
- **Memory limits**: fpl-core 50MB, fpl-bot 60MB (enforced in docker-compose.yaml)
- **Player positions**: `element_type` — 1=GK, 2=DEF, 3=MID, 4=FWD
- **Prices**: Stored as integers in 0.1m units (e.g., 85 = £8.5m)
- **Recommendation statuses**: pending → sent → confirmed → executed
- **Player statuses**: a=available, d=doubtful, i=injured, s=suspended

## Scoring Algorithm (xG/xA/xCS based)

Analysis runs on-demand when user sends `/suggest` in Telegram. Uses expected stats from last 5 games of player_history.

**Per-90 metrics computed:**
- xG/90, xA/90, xGI/90, xGC/90 (from player_history)
- CS rate (clean sheets per game)
- PPG (points per game)

**Position-weighted xScore:**
```
GK:  CSRate×6 + (1/(1+xGC/90))×4 + PPG×1
DEF: CSRate×4 + (1/(1+xGC/90))×3 + xGI/90×3 + PPG×1
MID: xG/90×3 + xA/90×3 + xGI/90×2 + PPG×1
FWD: xG/90×4 + xA/90×2 + xGI/90×2 + PPG×1
```

**Value and Overall (underdog-biased):**
```
Value = xScore / PriceInMillions
OverallScore = xScore×3.0 + Value×4.0 + Availability×5.0
```
Value weighted at 4.0 (higher than raw xScore at 3.0) to surface underpriced performers.

**Transfer recommendation:** Sell from user's squad (worst 3 per position) → Buy from outside (top 10 per position). Only valid transfers (different teams, positive gain). Top 5 kept.

## Telegram Commands

- `/suggest` — On-demand analysis: scores all players using xG/xA/xCS, generates transfer recommendations for user's current squad. Shows confirm/reject buttons.
- `/report` — Top 5 per position report for last 5 GW, last 10 GW, and full season.
- `/recommendations` — View pending recommendations from previous analyses.
- `/status` — System status (player count, last fetch time, pending recs).

## Configuration

Environment variables in `.env` (see `.env.example`):
- `TELEGRAM_BOT_TOKEN` — Required
- `FPL_SESSION_COOKIE`, `FPL_TEAM_ID` — Required for squad data and auto-trading
- `FETCH_SCHEDULE` — Cron format, default every 1h
- `CHECK_INTERVAL_MINUTES` — Bot polling interval for pending recs, default 60
