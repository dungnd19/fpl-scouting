# Scoring Overhaul — July 2026

## Problem Summary

The player scoring and transfer recommendation logic had several issues:

1. **xScore formulas didn't match FPL point values** — MID weighted xG (=5 FPL pts) and xA (=3 FPL pts) equally. FWD assist weight too low. DEF bundled xGI instead of separating xG (6pts) and xA (3pts). GK had no attacking contribution.

2. **Value metric was meaningless** — `xScore / price` produced a number with no real-world FPL interpretation.

3. **OverallScore used xScore but rankings used ExpectedPoints** — misaligned, causing inconsistent ordering.

4. **FixtureAdjustedDefensiveScore had inverted direction** — passed a multiplier (e.g. 0.80 for hard fixture) instead of raw difficulty (1-5), making hard fixtures increase defensive score instead of decrease it.

5. **Dead code** — `f()` function always returned 1.0, `GenerateTransferRecommendations()` was empty.

6. **Sell candidate selection was naive** — sold cheapest bench players who couldn't fund any upgrade.

7. **Buy candidate selection only considered EP** — ignored value (points per £M), could recommend overpriced players.

8. **No DEFCON scoring** — FPL's new defensive contributions system (2026/27) wasn't modeled.

---

## Changes

### Part A: DEFCON Data Pipeline

| Layer | File | Change |
|-------|------|--------|
| Schema | `sql/schema.sql` | Added `clearances_blocks_interceptions INTEGER`, `tackles INTEGER`, `recoveries INTEGER` to `player_history` |
| Core models | `services/core/internal/models/models.go` | Added JSON-tagged fields to `HistoryEntry` |
| Core repo | `services/core/internal/database/repository.go` | Updated `StorePlayerHistory` INSERT + Exec with 3 new columns |
| Core DB | `services/core/internal/database/database.go` | Added `runMigrations()` with `ALTER TABLE ADD COLUMN` for backwards compat |
| Bot models | `services/bot/internal/database/database.go` | Added `CBIT`, `Tackles`, `Recoveries` to `PlayerHistoryStats` |
| Bot repo | `services/bot/internal/database/database.go` | Updated all 4 SELECT queries, all Scan calls, weighted accumulation loop |
| Bot models | `services/bot/internal/models/models.go` | Added `CBITPer90`, `CBIRTPer90` to `PlayerScore` |

### Part B: xScore Formula Rewrite

| Position | Before | After |
|----------|--------|-------|
| **GK** | `CSRate*6 + defScore*4 + PPG*1` | `CSRate*6 + defScore*4 + PPG*1` |
| **DEF** | `CSRate*4 + defScore*3 + xGI*3 + PPG*1` | `CSRate*5 + defScore*3 + xG*3 + xA*2 + E[defcon]*2 + PPG*1` |
| **MID** | `xG*3 + xA*3 + xGI*2 + PPG*1` | `xG*5 + xA*3 + CSRate*1 + E[defcon]*1 + PPG*1` |
| **FWD** | `xG*4 + xA*2 + xGI*2 + PPG*1` | `xG*4 + xA*3 + PPG*1` |

DEFCON expected points model:
```
E[defcon] = min(1.0, per90Rate / threshold) * 2.0
  DEF: threshold=10 (CBIT), weight=2.0
  MID: threshold=12 (CBIRT), weight=1.0
  GK/FWD: not eligible
```

### Part C: Value & OverallScore

- **Value**: `xScore / costInMillions` → `ExpectedPoints / costInMillions` (points per £1M)
- **OverallScore**: `xScore*3 + Value*4 + Availability*5` → `ExpectedPoints*3 + Value*4 + Availability*5`

### Part D: Bug Fixes

- **`FixtureAdjustedDefensiveScore`**: parameter changed from `float64` multiplier to `int` difficulty (1-5). Formula: `adjXGC = xGC * (1 + (difficulty-3)*0.15)`, then `1/(1+adjXGC)`. Hard fixtures (5) increase adjXGC → lower score. Easy fixtures (1) decrease adjXGC → higher score.
- **Dead code removal**: Removed `f()` function (always returned 1.0), removed empty `GenerateTransferRecommendations()`.

### Part E: Selection Logic Improvements

**Sell candidates** — `canAffordAnyBuy()` filter:
- After selecting worst 3 players by EP, filters out sell candidates that can't afford ANY buy candidate with positive EP gain (sellPrice + bank < min(buyPrice in position)).
- Prevents recommending selling a 4.0M bench player who can't fund an upgrade.

**Buy candidates** — `filterBuyCandidates()`:
- Takes top 15 by EP, then keeps only those with Value (pts/£M) ≥ position median.
- Falls back to top 15 unfiltered if < 3 pass the value filter.
- Caps at 10 results.

### Part F: Functions With Changed Signatures

| Function | Old | New |
|----------|-----|-----|
| `ScorePlayer` | `(p, stats, teamModel, fixtureAdj)` | `+ fixtureDifficulty int` |
| `ExpectedFPLPoints` | no defcon | `+ defconScore float64` |
| `SimpleExpectedPoints` | no defcon | `+ defconScore float64` |
| `MultiGWExpectedPoints` | no defcon | `+ defconScore float64` |
| `FixtureAdjustedDefensiveScore` | `(float64, float64)` | `(float64, int)` |

All call sites in `analyzer.go`, `main.go`, and `cmd/analyze/main.go` updated.

---

## Files Changed (8 total)

```
sql/schema.sql
services/core/internal/models/models.go
services/core/internal/database/repository.go
services/core/internal/database/database.go
services/bot/internal/database/database.go
services/bot/internal/models/models.go
services/bot/internal/analyzer/scorer.go          (rewritten)
services/bot/internal/analyzer/expected_points.go
services/bot/internal/analyzer/team_model.go
services/bot/internal/analyzer/analyzer.go
services/bot/main.go
services/bot/cmd/analyze/main.go
```
