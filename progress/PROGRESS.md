# Progress: Season-Start Squad Optimizer

**Date:** 2026-07-28 | **Current DB:** 563 players, 26/27 prices | **State:** Squad output verified

## What Was Built

A season-start FPL squad optimizer maximizing 3-GW expected points with no transfers, using vaastav multi-season historical stats blended with cold-start priors.

### Scoring Pipeline (6 phases)
1. **Time-decay stats** — Weighted per-90 metrics from player_history (`exp(-0.2 × age)`) in Go (SQLite has no `EXP()`)
2. **Poisson team model** — Team attack/defense strength from xG/xGA
3. **Multinomial EP** — Expected points per player combining xG/xA/xCS/PPG weighted by position
4. **Multi-GW discount** — `1 + (14/15)^1 + (14/15)^2` for 3-GW horizon
5. **Bayesian Dirichlet** — Cold-start blending: vaastav 24/25 + 25/26 (57K rows) as prior
6. **Transfer strategy** — Sell worst within your squad, buy best outside

### Season-Start Constraints
- Starters ≥ 1500 mins (last season), bench ≥ 700 mins
- Bench budget sweep from 20M → 12M floor
- 7 valid FPL formations (3-4-3, 3-5-2, 4-3-3, 4-4-2, 4-5-1, 5-3-2, 5-4-1)
- Max 3 players per team
- Dual-pool greedy: starters from ≥1500-min pool (EP-max), bench from ≥700-min pool (EP-first)

### Key Files Changed
| File | Change |
|------|--------|
| `services/bot/internal/analyzer/squad_optimizer.go` | Season-start constraints, dual-pool greedy, 3-GW discount |
| `services/bot/internal/analyzer/analyzer.go` | `SuggestSeasonStartSquad()`, `scoreAllPlayers()` |
| `services/bot/internal/database/database.go` | Rewrote `GetPlayerWeightedRecentStats` — Go-side `math.Exp` decay, per-90 uses weighted minutes |
| `services/bot/internal/analyzer/scorer.go` | `nineties <= 0` guard for division-by-zero |
| `services/bot/cmd/analyze/main.go` | `-mode startsquad`, `-min-starter-mins`, `-min-bench-mins` flags |
| `services/core/cmd/seed/main.go` | Bootstrap + vaastav seeder (24/25 + 25/26 → 57K rows) |
| `services/core/internal/fetcher/vaastav.go` | Historical data fetcher from vaastav/github |

### Current Output
```
Formation: 3-5-2 | Spend: £80.8M | Bank: £19.2M | Starter EP: 140.03
```
Valid output, but large bank leftover — greedy EP-max favors cheap value players. May need bench budget floor tuning or premium bias.

### Data Sources
- **26/27 prices** — FPL bootstrap-static (public, already launched)
- **Player history** — FPL element-summary (empty for 26/27, cold-start uses vaastav)
- **Cold-start prior** — vaastav 2024-25 + 2025-26 seasons (57,362 entries)

### Environment
- Go 1.18, SQLite3 (WAL mode, 2MB cache), Docker 2-service
- Server: Nemo (1.9GB RAM, 38GB disk, 103.200.22.207)

## Next Steps
1. Tune bench budget floor or add premium bias to reduce bank leftover
2. Wire `/startsquad` into Telegram bot command handler
3. Rebuild Docker images and deploy to Nemo
