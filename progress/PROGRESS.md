# Progress: Season-Start Squad Optimizer

**Date:** 2026-07-28 | **Current DB:** 563 players, 26/27 prices | **State:** Squad output verified, £99.5M spent

## What Was Built

A season-start FPL squad optimizer maximizing 3-GW expected points with no transfers, using vaastav multi-season historical stats blended with cold-start priors.

### Scoring Pipeline

1. **Time-decay stats** — Weighted per-90 metrics via `exp(-0.2 * age)` computed in Go (SQLite lacks `EXP()`)
2. **Minutes regression** — Per-90 xG/xA/CS/PPG shrunk toward position average: `regressed = avg + (observed - avg) * mins/(mins + 1500)`
3. **Total points blend** — 60% model EP + 40% normalized actual points from last season, rewarding proven scorers
4. **Poisson team model** — Team attack/defense strength from xG/xGA (loaded if available)
5. **Multinomial EP** — Expected points combining xG/xA/xCS/PPG weighted by position
6. **Multi-GW discount** — `1 + (14/15)^1 + (14/15)^2` for 3-GW horizon (no fixtures table, else fixture-adjusted)
7. **Bayesian Dirichlet** — Cold-start blending: vaastav 2024-25 (weight 0.61) + 2025-26 (weight 1.0), older seasons ignored
8. **Transfer strategy** — Sell worst within your squad, buy best outside

### Season-Start Constraints

- Starters >= 1500 mins (last season), bench >= 700 mins
- Bench budget sweep 150 -> 80 (step 5)
- 7 valid FPL formations
- Max 3 players per team
- Dual-pool: starters from >=1500-min pool (EP-sorted), bench from >=700-min pool (EP/cost value-sorted)
- `applyFormationSeasonStart` keeps starter/bench labels separate (prevents bench inflation leaking into starting 11)

### Key Files Changed

| File | Change |
|------|--------|
| `services/bot/internal/analyzer/analyzer.go` | `scoreAllPlayers()`: minutes regression, total points blend, two-pass EP recompute |
| `services/bot/internal/analyzer/squad_optimizer.go` | `applyFormationSeasonStart`, bench value-sort, lowered sweep, `MaxBenchPlayerCost` removed |
| `services/bot/internal/database/database.go` | Go-side `math.Exp` decay in `GetPlayerWeightedRecentStats` |
| `services/bot/internal/analyzer/scorer.go` | `nineties <= 0` guard |
| `services/bot/cmd/analyze/main.go` | `-mode startsquad` CLI flag |
| `services/core/cmd/seed/main.go` | Bootstrap + vaastav seeder (24/25 + 25/26 -> 57K rows) |
| `services/core/internal/fetcher/vaastav.go` | Historical data fetcher |

### Current Output

```
Formation: 3-4-3 | Cost: £99.5M | Bank: £0.5M | Starter EP: 92.31

GK:  Leno (FUL, £4.5M) / Dubravka (TOT, £4.0M)
DEF: O'Reilly (MCI, £6.5M), Gu�hi (MCI, £6.0M), Dalot (MUN, £5.0M)
     bench: Smith (BOU, £4.5M), Rodon (LEE, £4.5M)
MID: B.Fernandes (MUN, £12.0M), Bruno G. (NEW, £7.0M),
     Lewis-Potter (BRE, £5.5M), Aaronson (LEE, £5.5M)
     bench: Sangar� (NFO, £5.0M)
FWD: Haaland (MCI, £15.5M), Welbeck (BHA, £6.0M), Watkins (AVL, £8.0M)
```

### Data Sources

- **26/27 prices** — FPL bootstrap-static (public, already launched, GW1 Aug 21 2026)
- **Player history** — FPL element-summary (empty for 26/27, cold-start uses vaastav)
- **Cold-start prior** — vaastav 2024-25 + 2025-26 seasons (57,362 entries)
- **Cross-season blending** — `exp(-0.5 * seasonIdx)`: 25/26 weight 1.0, 24/25 weight 0.61

### Bugs Fixed

| Bug | Fix |
|-----|-----|
| Bench players re-labeled as starters by `applyFormation` | New `applyFormationSeasonStart` sorts starters/bench separately, keeps labels |
| Per-90 stats inflated for low-minute players | Minutes regression: `weight = mins/(mins+1500)`, pulls toward position average |
| Greedy EP undervalues proven scorers | 40% total points blend: `EP *= 0.6 + 0.4*normalizedTP` |
| Bench overpays for EP instead of value | Bench pool sorted by EP/cost instead of pure EP |
| SQLite `no such function: EXP` | Time-decay computed in Go, not SQL |

### Environment

- Go 1.18, SQLite3 (WAL mode, 2MB cache), Docker 2-service
- Server: Nemo (1.9GB RAM, 38GB disk, 103.200.22.207)

## Next Steps

1. Wire `/startsquad` into Telegram bot command handler
2. Rebuild Docker images and deploy to Nemo
3. Add fixtures table for fixture-adjusted multi-GW EP (currently 3-GW raw discount only)
