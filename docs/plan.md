# Plan

Implementation plan and test plan for in-flight work. One section per
requirement (matches `requirement.md` entries by date). Update the relevant
section's checklist as steps complete — don't leave finished items unchecked.

---

## 2026-08-02 — Deploy workflow

**Plan:**
- [x] Confirm `fpl-core` already fetches on startup (`services/core/main.go`
      — initial fetch fires 2s after boot on every restart). No new fetch
      logic needed.
- [x] Add `make deploy` target: `git pull` then `docker compose up -d
      --build` (rebuilds only what changed, recreates containers whose
      image/config changed).
- [x] Remove dead `make analyze` target (called a `-analyze` flag no longer
      present on `fpl-core` after the legacy-analyzer removal).
- [x] Document deploy steps in `README.md`.
- [x] Fix `setup.sh`'s dangling reference to a nonexistent `DEPLOYMENT.md`.

**Test plan:**
- [x] `docker compose config -q` — validates compose file parses with the
      new target's assumptions.
- [ ] Run `make deploy` on the actual server once reachable — not yet
      verified live (no SSH access from this environment).

## 2026-08-02 — Docs tracking process

**Plan:**
- [x] Create `docs/requirement.md`, `docs/plan.md`, `docs/info.md`.
- [x] Add a rule to `CLAUDE.md` requiring these files be kept current.

**Test plan:** n/a (documentation only).

## 2026-08-02 — Fix deploy pipeline

Full design in `.claude/plans/read-2-new-requirement-hazy-sunrise.md`
(Task 4). Pure ops/Makefile change, zero Go code, independent of the
service-workflow tasks below.

**Test plan (manual, server-side — no SSH access from dev environment):**
- [ ] `pass show fpl-scouting/telegram-bot-token` (and `fpl-session-cookie`,
      `fpl-team-id`) resolve to non-empty values after the operator's
      one-time `pass insert` step.
- [ ] `make deploy` on the server exits 0; its transcript contains no
      secret *values* (only var names).
- [ ] `docker compose exec fpl-bot env | grep TELEGRAM_BOT_TOKEN` shows the
      token was injected into the running container.
- [ ] Deploy target's Telegram-verification step reports success, and a
      live `/status` message to the bot gets a reply.
- [ ] One forced-failure run (bad token) confirms verification fails
      loudly instead of false-passing.

**Plan:**
- [ ] Document `pass` entry naming convention in `docs/info.md`.
- [ ] Update `make deploy` to export the 3 secrets from `pass show` into
      the shell env only for the `docker compose up -d --build` call —
      never written to a file, never echoed.
- [ ] Add post-deploy Telegram verification (grep `fpl-bot` logs for the
      existing "Authorized on account" line; fail loudly if absent).
- [ ] Update `README.md` deploy section + `.env.example` to note server
      secrets come from `pass`, not `.env` (local dev `.env` unchanged).
- [ ] Update `docs/info.md` deploy process / secrets sections.

## 2026-08-02 — Fix service workflow and function

Full design in `.claude/plans/read-2-new-requirement-hazy-sunrise.md`
(Tasks 1-3). Task 1 (migrations) must land first — Tasks 2 and 3 both read
via `database.Repository` methods that depend on the migrated schema.

### Task 1 — Real migration framework (goose) in fpl-core

**Test plan (write before/alongside implementation) — new
`services/core/internal/database/migrations_test.go`, stdlib `testing`,
table-driven, temp SQLite file per case:**
- [ ] Fresh DB → all 11 tables created, `goose_db_version` at 2, no errors.
- [ ] Pre-existing DB (old schema + old 3 `ALTER TABLE`s, no goose table)
      → new `runMigrations` doesn't error, version table seeded at 2.
- [ ] Idempotency: `runMigrations` called twice in a row → second call
      no-ops, no error.
- [ ] Pre-existing DB predating even the DEFCON columns → columns added
      exactly once, no error.
- [ ] Manual: `docker compose build && docker compose up -d fpl-core`,
      `make db-status` still reports correctly, no migration errors in
      logs on a fresh volume (and the real server's volume, if reachable).

**Plan:**
- [ ] Bump `services/core/Dockerfile` + `services/bot/Dockerfile` builder
      Go version and both `go.mod` `go` directives off 1.18 (goose needs
      newer than 1.18); verify `docker compose build` still succeeds.
- [ ] Add `github.com/pressly/goose/v3` to `services/core/go.mod` only.
- [ ] Create `sql/migrations/00001_initial_schema.sql` (current
      `sql/schema.sql` verbatim, goose `Up`/`Down` annotations).
- [ ] Create `sql/migrations/00002_player_history_defcon_columns.sql`
      (the 3 `ALTER TABLE` statements, now goose-tracked).
- [ ] `go:embed` migrations into `services/core/internal/database`; drop
      the `sql/schema.sql` bind-mount + `SCHEMA_PATH` env var from
      `docker-compose.yaml`.
- [ ] Replace `executeSchema()`/`runMigrations()` in `database.go` with
      goose-based `runMigrations(db)`.
- [ ] Add pre-existing-DB bootstrap detection (seed `goose_db_version` to
      2 before calling `Up`, so 00002's `ALTER TABLE` never re-runs on a
      DB that already has those columns from the old ad hoc path).

### Task 2 — `/suggest` optional GW-horizon argument

**Test plan (write first) — new
`services/bot/internal/telegram/suggest_args_test.go`, table-driven:**
- [ ] `parseSuggestHorizon` on `"/suggest"`, `"/suggest 1"`, `"2"`, `"3"`
      → valid, defaults to 3 when absent.
- [ ] `"/suggest 4"` (out of range) and `"/suggest abc"` (not a number)
      → rejected with a clear error, not silently clamped.
- [ ] `"/suggest  2  "` (whitespace) parses correctly.
- [ ] Manual: `/suggest`, `/suggest 1`, `/suggest 2` against the real bot
      — fixture count / EP figures visibly differ by horizon.

**Plan:**
- [ ] `analyzer.Service.Suggest()` → `Suggest(numGameweeks int)`; replace
      the hardcoded `3` in the `GetUpcomingFixtures` call (analyzer.go:155).
- [ ] Extract `parseSuggestHorizon(text string) (int, error)` in the
      telegram package; wire into `handleSuggest`, default 3, reply with
      a friendly error on invalid input instead of clamping silently.

### Task 3 — `/init-squad`: unit tests first, then rename from `/startsquad`

**Test plan (this task's main deliverable — repo's first `_test.go`,
stdlib `testing` only, table-driven) — new
`services/bot/internal/analyzer/squad_optimizer_test.go`, written and
green against *current* behavior before any rename:**
- [ ] Valid squad at default budget with ample pool of every position →
      non-nil, cost ≤ 1000, exactly 2/5/5/3, formation in `validFormations`.
- [ ] Budget must be relaxed via the bench-reserve sweep before a valid
      starting 11 emerges → `OptimizeSquadBest` still returns non-nil.
- [ ] No valid squad possible at any level (e.g. zero forwards in pool)
      → `nil`, no panic.
- [ ] Max-3-per-team constraint never violated even when top players
      cluster on one team.
- [ ] Chosen formation always one of the 7 `validFormations`.
- [ ] Empty/insufficient player pool → `nil`, no panic, no index-out-of-
      range.
- [ ] `OptimizeSeasonStartSquad`: starters below `MinStarterMinutes`
      excluded even with high score.
- [ ] `OptimizeSeasonStartSquad`: insufficient starter candidates per
      `SeasonStartSquadConstraints` → `nil` (matches existing guard at
      squad_optimizer.go:487-491).
- [ ] `go test ./services/bot/internal/analyzer/...` passes, `go vet` clean.

**Plan:**
- [ ] Write and green the test suite above against current `/startsquad`
      behavior (before touching the rename).
- [ ] Rename `/startsquad` → `/init-squad` in `telegram/bot.go` (command
      menu, dispatch case, handler doc comment) and `telegram/messages.go`
      help text; update `README.md`'s two references. Keep the £1.0m
      bench-sweep step size unchanged (explicit user decision).
- [ ] Re-run the test suite post-rename — must still pass unchanged
      (confirms the rename was cosmetic, no behavior change).

**Cross-task verification:** `go build ./...` and `go test ./...` in both
`services/core` and `services/bot` must pass before moving to the next
task. After each task, tick its checklist here and flip the status line
in `docs/requirement.md` once all tasks for a requirement are done.
