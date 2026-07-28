# Cold-Start Prediction: Multi-Season Historical Data (vaastav)

## Problem

At the start of a new FPL season (GW 1-3), there is zero or very little current-season
data in the `player_history` table. The original algorithm returns empty or degenerate
results because:

- `GetPlayerWeightedRecentStats(playerID, 5)` → 0 games for all players
- Per-90 metrics are `0/0` → NaN
- No transfer suggestions can be generated

## Solution: vaastav Historical Data Integration

The [vaastav/Fantasy-Premier-League](https://github.com/vaastav/Fantasy-Premier-League)
repo provides GW-by-GW CSV data (`merged_gw.csv`) for every season from 2016-17 through
2024-25. Each row contains: player name, team, position, minutes, goals, assists, xG, xA,
xGI, xGC, clean sheets, total points, opponent, home/away, price, and gameweek number.

### Architecture

```
vaastav GitHub raw CSV
        │
        ▼
  VaastavFetcher (services/core/internal/fetcher/vaastav.go)
        │  Downloads merged_gw.csv for up to 3 prior seasons
        │  Parses CSV → []SeasonHistoryEntry
        ▼
  Repository.StoreSeasonHistoryBatch()
        │  Batch INSERT OR IGNORE into `season_history` table
        ▼
  Repository.BuildCrossSeasonMap()
        │  Matches current player names to historical names
        │  Stores in `cross_season_map` table
        ▼
  Repository.GetPlayerMultiSeasonBlendedStats()
        │  Blends current-season + prior-season stats with decay weights
        ▼
  Analyzer.Suggest() — detects cold-start → uses blended stats
```

## New Database Tables

### `season_history`
Stores per-player per-GW stats from prior seasons. Key columns:
- `season` (e.g., "2023-24")
- `player_name` (full name, used for cross-season matching)
- `position`, `team_name`
- `expected_goals`, `expected_assists`, `expected_goal_involvements`, `expected_goals_conceded`
- `minutes`, `total_points`, `goals_scored`, `assists`, `clean_sheets`, `bonus`, `bps`
- `was_home`, `opponent_team`

### `cross_season_map`
Maps current player IDs to historical player names per season:
- `current_player_id` → `prior_player_name` for a given `season`
- `confidence` (1.0 = exact name match, 0.9 = web_name match)

## Blending Algorithm

### Multi-Season Decay
```
season_weight = exp(-0.5 * seasons_ago)
```
| Season | Weight |
|--------|--------|
| N-1 (last season) | 0.61 |
| N-2 (2 seasons ago) | 0.37 |
| N-3 (3 seasons ago) | 0.22 |

### Cold-Start Detection
```go
isColdStart = (distinct_gw_count in player_history < 3)
```

### Blending Ratio
```
current_ratio = min(current_games / 3, 1.0)  // 0 games → 0%, 1 game → 33%, 3+ → 100%
prior_ratio   = 1.0 - current_ratio          // 0 games → 100%, 1 game → 67%, 3+ → 0%
```

### Formula
```
blended_XG = current_XG + prior_XG * prior_ratio
blended_XA = current_XA + prior_XA * prior_ratio
blended_CS = current_CS + prior_CS * prior_ratio
...
```

As the season progresses (GW4+), the blending shifts entirely to current-season data.

## New/Modified Files

| File | Change |
|------|--------|
| `sql/schema.sql` | Added `season_history` and `cross_season_map` tables with indexes |
| `services/core/internal/fetcher/vaastav.go` | **New** — Downloads & parses merged_gw.csv from GitHub |
| `services/core/internal/models/models.go` | Added `SeasonHistoryEntry` struct |
| `services/core/internal/database/repository.go` | Added `StoreSeasonHistoryBatch()`, `BuildCrossSeasonMap()`, `HasSeasonData()` |
| `services/bot/internal/database/database.go` | Added `GetPlayerMultiSeasonBlendedStats()`, `IsColdStart()`, `HasSeasonData()`, `getCrossSeasonNames()`, `getPriorSeasonStats()` |
| `services/bot/internal/analyzer/analyzer.go` | Updated `Suggest()` to detect cold-start and use blended stats |

## Usage

### 1. Fetch vaastav data (one-time, at season start)
```bash
# In the core container, the VaastavFetcher can be invoked via a CLI flag:
# Add to main.go or run as a separate command:
```

### 2. Auto-detection
The bot's `Suggest()` automatically checks `IsColdStart(3)` and uses blended stats
when fewer than 3 gameweeks of current-season data exist. No user action needed.

## Example: GW1 Cold-Start

**Current season:** 0 games in `player_history`

**Player: Mohamed Salah**
- Prior season (2023-24): 32 games, 18 goals, 10 assists, xG=0.65/90, xA=0.35/90
- Prior season (2022-23): 38 games, 19 goals, 12 assists (weight 0.37)
- Prior season (2021-22): 35 games, 23 goals, 13 assists (weight 0.22)

Blended stats (0 current games → 100% prior):
```
xG/90  ≈ 0.65*0.61 + 0.55*0.37 + 0.72*0.22 = 0.40 + 0.20 + 0.16 = 0.76
xA/90  ≈ 0.35*0.61 + 0.34*0.37 + 0.42*0.22 = 0.21 + 0.13 + 0.09 = 0.43
```

**Result:** Salah gets a reasonable predicted score even with zero current-season data.

## Limitations

1. **Player transfers between teams:** Cross-season mapping uses name matching. If a
   player changes teams between seasons, the historical stats still reflect their
   previous team's performance. Team-level Poisson ratings (Phase 2) adjust for
   fixture difficulty.
2. **Promoted/Relegated teams:** Players from promoted teams have no prior-season PL
   data (the vaastav dataset only covers PL). These players fall back to using the
   Bayesian prior alone.
3. **New signings:** Players new to the PL have no historical data. The Bayesian
   prior (Phase 5) handles this via position-average regularization.
4. **Position changes:** If a player was classified as MID last season but is FWD
   this season, the positional weighting may be slightly misaligned. The system
   uses the historical position for prior stats and the current position for scoring.
