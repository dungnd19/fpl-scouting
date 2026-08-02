# Requirements

Log of requirements as given by the user. Newest entry on top. When a prompt
changes or adds a requirement, append a new dated entry — don't edit past
entries except to mark them superseded.

---

## 2026-08-02 — Deploy workflow

- Server deploy = pull latest code from GitHub, rebuild Docker images,
  restart containers.
- DB must auto-refresh with latest FPL data at service start (no manual
  fetch step required after deploy).

Status: done — `make deploy` (`git pull && docker compose up -d --build`);
`fpl-core` already re-fetches ~2s after every startup, so no extra step was
needed for the DB-refresh requirement.

## 2026-08-02 — Docs tracking process

- Add `docs/requirement.md`, `docs/plan.md`, `docs/info.md`.
- Update the relevant file(s) whenever a step finishes — analysis or code,
  not just code.
- Ruled into `CLAUDE.md` so it's enforced across sessions.

Status: done.

## 2026-08-02 — Fix deploy pipeline
User ssh from server, then fetch the latest code from github, rebuild image and run the docker service.

Make sure the telegram work , all env key in server should pass by param only (which using 'pass' tool to store) and not printed out in code.
Status: new

**Decision (2026-08-02):** secrets live in `pass` as one entry per
variable (not a single blob) — `fpl-scouting/telegram-bot-token`,
`fpl-scouting/fpl-session-cookie`, `fpl-scouting/fpl-team-id`. Keep the
existing manual-SSH-then-`make deploy` flow (don't resurrect the reverted
SSH-from-dev-machine `remote-*` targets). See `docs/plan.md` Task 4.

## 2026-08-02 — Fix service workflow and function
The DB data should be saved and in migration solution, so when at start of serivce, the DB is load the initial data and fetch the lastest data at start of service.

Then when the data is up-to-date, we can using these endpoint to get the data
- `/suggest` — On-demand analysis: scores all players using xG/xA/xCS, generates transfer recommendations for user's current squad (for optimiztion score in next 3 gameweek or have a selection for 1-3 gameweek). Shows confirm/reject buttons with weeb hook to transfer those trade.
- `/report` — Top 5 per position report for last 5 GW, last 10 GW, and full season.
- `/recommendations` — View pending recommendations from previous analyses.
- `/init-squad` - View and calculate intial squad at start of the season base on current player and price with maximine the scoring value of next 3 gameweek. The squad must comply to setting and if no squad can be created with default bench bugget, keep open bench budget by 0.1 till find the maxinum point of the starting 11 squad
- `/status` — System status (player count, last fetch time, pending recs).
Status: done — Tasks 1-3 below all landed. `/init-squad` shipped as
`/init_squad` (see progress note).

**Decisions (2026-08-02):**
- DB migrations: adopt a real migration framework (goose), not just
  hardening the existing ad hoc `ALTER TABLE` hack.
- `/init-squad`: rename from the already-implemented `/startsquad`. Keep
  its £1.0m bench-sweep step size unchanged — instead write a proper unit
  test suite for the squad optimizer first (repo's first `_test.go` file).
- `/suggest`: add an optional GW-horizon argument (`/suggest [1|2|3]`),
  default 3, threading into the existing multi-GW lookahead.
See `docs/plan.md` Tasks 1-3.

**Progress (2026-08-02):** All 3 tasks done — see `docs/plan.md` for
details. Task 3 shipped as `/init_squad` (underscore, not hyphen) because
Telegram's `setMyCommands` rejects hyphens in command names.