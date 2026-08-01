# Info

Environment facts, deploy process, test/server details. This is state, not
narrative — keep entries current and delete what's no longer true rather
than appending history (that's what `plan.md`/`requirement.md` are for).

## Local dev environment

- Go toolchains present on this machine:
  - `/usr/bin/go` — apt package, go1.18.1 (Ubuntu jammy repo cap).
  - `~/.local/go` — go1.26.5, installed manually (no sudo available for
    `/usr/local/go`). `~/.bashrc` puts this first on `PATH` via `$GOROOT`.
  - VSCode Go extension pinned to `~/.local/go` via `.vscode/settings.json`
    (`go.goroot` / `go.alternateTools`), independent of shell PATH.
- `go.work` at repo root unions `services/core` + `services/bot` so gopls
  resolves both modules in one window. Gitignored — local-only, not
  committed (each dev/agent regenerates with `go work init ./services/core
  ./services/bot`).
- `.vscode/launch.json` has two debug configs (fpl-core fetch-once, fpl-bot)
  that override `DB_PATH`/`SCHEMA_PATH` to local repo paths — the code's
  defaults (`/data/fpl.db`, `/app/schema.sql`) are container paths and don't
  exist on the host.
- `fpl-local.db` at repo root — a local copy of the SQLite DB, pulled via
  `make db-copy`. Not authoritative; the real DB only lives in the
  `fpl-data` Docker volume.
- CGO must be enabled (`go-sqlite3` dependency); it's on by default on this
  machine.

## Deploy process

- `make deploy` = `git pull && docker compose up -d --build`, run **on the
  server itself** (SSH in first — there is no push-button remote deploy
  from a dev machine).
- No fetch step needed after deploy: `fpl-core` re-fetches from the FPL API
  ~2s after every process start (`services/core/main.go`), so a fresh
  container picks up current data automatically.
- No CI/CD — deploys are manual.

## Server

- **Unconfirmed.** A Makefile commit (`1908282`, 2026-07-29) added SSH
  remote-* targets pointing at `root@103.200.22.207:/root/fpl-scouting`,
  but it was reverted same day (`2bf8713`) with no reason given in the
  commit message. Don't assume this host/path is still correct — confirm
  with the user before relying on it for anything destructive.
- No separate test/staging server is known; assume the user tests against
  whatever server they deploy to.

## Secrets / config

- `.env` (gitignored) holds `TELEGRAM_BOT_TOKEN`, `FPL_SESSION_COOKIE`,
  `FPL_TEAM_ID`. `FPL_SESSION_COOKIE` expires periodically and must be
  re-pulled from a logged-in browser session (see `.env.example`).
