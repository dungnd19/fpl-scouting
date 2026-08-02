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

### Task 1 — Real migration framework (goose) in fpl-core — DONE

**Test plan (write before/alongside implementation) — new
`services/core/internal/database/migrations_test.go`, stdlib `testing`,
table-driven, temp SQLite file per case:**
- [x] Fresh DB → all 11 tables created, `goose_db_version` at 2, no errors.
- [x] Pre-existing DB (old schema + old 3 `ALTER TABLE`s, no goose table)
      → new `runMigrations` doesn't error, version table seeded at 2.
- [x] Idempotency: `runMigrations` called twice in a row → second call
      no-ops, no error.
- [x] Pre-existing DB predating even the DEFCON columns → columns added
      exactly once, no error.
- [x] Manual: `docker compose build && docker compose up -d fpl-core`,
      no migration errors in logs on a fresh volume (isolated `docker run`
      against a throwaway volume) **and** against this dev machine's real
      local `fpl-data` volume — a genuine pre-goose DB from 2026-07-28,
      confirmed bootstrapped to `goose_db_version` 2 with DEFCON columns
      and all 564 players intact, zero data loss. `make db-status` itself
      is broken independent of this change (`db-query.sh` shells out to a
      `sqlite3` CLI binary that was never installed in the runtime Alpine
      stage — pre-existing bug, not in this task's scope). Real production
      server volume still unverified — no SSH access from this environment.

**Plan:**
- [x] Bump `services/core/Dockerfile` + `services/bot/Dockerfile` builder
      Go version and both `go.mod` `go` directives off 1.18 (goose needs
      newer than 1.18); verify `docker compose build` still succeeds.
      Bumped to `golang:1.26-alpine` / `go 1.26` (matches this machine's
      `~/.local/go`). Also had to bump `mattn/go-sqlite3` v1.14.18 →
      v1.14.49 — the old version fails to compile against Alpine 3.24's
      newer musl headers (`off64_t`/`osPread64` errors), unrelated to
      goose but surfaced by the same base-image bump.
- [x] Add `github.com/pressly/goose/v3` (v3.27.3) to `services/core/go.mod`
      only.
- [x] Create `sql/migrations/00001_initial_schema.sql` (current
      `sql/schema.sql` verbatim, goose `Up`/`Down` annotations). Lives at
      `services/core/internal/database/migrations/00001_initial_schema.sql`
      instead of repo-root `sql/` — Go's `//go:embed` can't cross a module
      boundary (`services/core` is its own module), so the migrations have
      to be co-located with the package that embeds them. Deleted the now-
      unused repo-root `sql/schema.sql` and updated its two doc references
      (`README.md`, `CLAUDE.md`).
- [x] Create `sql/migrations/00002_player_history_defcon_columns.sql`
      (the 3 `ALTER TABLE` statements, now goose-tracked). Same path note
      as above.
- [x] `go:embed` migrations into `services/core/internal/database`; drop
      the `sql/schema.sql` bind-mount + `SCHEMA_PATH` env var from
      `docker-compose.yaml`.
- [x] Replace `executeSchema()`/`runMigrations()` in `database.go` with
      goose-based `runMigrations(db)` (new `migrations.go`).
- [x] Add pre-existing-DB bootstrap detection (seed `goose_db_version` to
      2 before calling `Up`, so 00002's `ALTER TABLE` never re-runs on a
      DB that already has those columns from the old ad hoc path). Seeds
      version 1 unconditionally and version 2 only if the DEFCON columns
      already exist on `player_history` — otherwise 00002 is left to run
      for real and add them (needed to pass the pre-DEFCON test case).

### Task 2 — `/suggest` optional GW-horizon argument — DONE

**Test plan (write first) — new
`services/bot/internal/telegram/suggest_args_test.go`, table-driven:**
- [x] `parseSuggestHorizon` on `"/suggest"`, `"/suggest 1"`, `"2"`, `"3"`
      → valid, defaults to 3 when absent.
- [x] `"/suggest 4"` (out of range) and `"/suggest abc"` (not a number)
      → rejected with a clear error, not silently clamped.
- [x] `"/suggest  2  "` (whitespace) parses correctly.
- [ ] Manual: `/suggest`, `/suggest 1`, `/suggest 2` against the real bot
      — fixture count / EP figures visibly differ by horizon. Not run —
      no live bot/Telegram access from this environment.

**Plan:**
- [x] `analyzer.Service.Suggest()` → `Suggest(numGameweeks int)`; replaced
      the hardcoded `3` in the `GetUpcomingFixtures` call (analyzer.go:155).
      Also updated the standalone `cmd/analyze` debug CLI to pass `3`.
- [x] Extract `parseSuggestHorizon(text string) (int, error)` in the
      telegram package; wired into `handleSuggest`, default 3, replies with
      a friendly error on invalid input instead of clamping silently.

### Task 3 — `/init-squad`: unit tests first, then rename from `/startsquad` — DONE

**Deviation:** requirement text said `/init-squad`, but Telegram's
`setMyCommands` only accepts `^[a-z0-9_]{1,32}$` — hyphens are rejected
and would break registration of the *entire* command menu (one batch
call for all commands), not just this one. Shipped as `/init_squad`
(underscore) instead.

**Test plan (this task's main deliverable — repo's first `_test.go`,
stdlib `testing` only, table-driven) — new
`services/bot/internal/analyzer/squad_optimizer_test.go`, written and
green against *current* behavior before any rename:**
- [x] Valid squad at default budget with ample pool of every position →
      non-nil, cost ≤ 1000, exactly 2/5/5/3, formation in `validFormations`.
- [x] Bench-reserve sweep (`OptimizeSquadBest`) still returns non-nil.
      Note: `benchReserve` is currently dead weight in `tryFormation` —
      it's only recorded on the result, never used to change what the
      budget can afford — so the sweep can't actually "relax" anything
      today; test asserts the sweep matches a direct single-reserve call
      instead of a since-disproven relaxation behavior.
- [x] No valid squad possible at any level (e.g. zero forwards in pool)
      → `nil`, no panic.
- [x] Max-3-per-team constraint never violated even when top players
      cluster on one team.
- [x] Chosen formation always one of the 7 `validFormations`.
- [x] Empty/insufficient player pool → `nil`, no panic, no index-out-of-
      range.
- [x] `OptimizeSeasonStartSquad`: starters below `MinStarterMinutes`
      excluded even with high score.
- [x] `OptimizeSeasonStartSquad`: insufficient starter candidates per
      `SeasonStartSquadConstraints` → `nil` (matches existing guard at
      squad_optimizer.go:487-491).
- [x] `go test ./services/bot/internal/analyzer/...` passes, `go vet` clean.

**Plan:**
- [x] Write and green the test suite above against current `/startsquad`
      behavior (before touching the rename).
- [x] Rename `/startsquad` → `/init_squad` in `telegram/bot.go` (command
      menu, dispatch case, handler doc comment) and `telegram/messages.go`
      help text; updated `README.md`'s two references. Kept the £1.0m
      bench-sweep step size unchanged (explicit user decision). Left the
      unrelated `-mode startsquad` flag on the standalone
      `services/bot/cmd/analyze` debug CLI as-is (different surface, not
      the Telegram command).
- [x] Re-run the test suite post-rename — passed unchanged (confirms the
      rename was cosmetic, no behavior change).

**Cross-task verification:** `go build ./...` and `go test ./...` in both
`services/core` and `services/bot` must pass before moving to the next
task. After each task, tick its checklist here and flip the status line
in `docs/requirement.md` once all tasks for a requirement are done.
