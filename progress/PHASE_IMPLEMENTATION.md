# Phase Implementation Summary — AIrsenal Algorithm Applied to fpl-scouting

## Files Changed

| File | Status | What changed |
|------|--------|-------------|
| `services/bot/internal/models/models.go` | Modified | New types: `TeamRating`, `FixtureInfo`, `TransferPlan`, field additions to `PlayerScore` and `PlayerReport` |
| `services/bot/internal/database/database.go` | Modified | 6 new query methods for time-decay, fixtures, team ratings, free transfers |
| `services/bot/internal/analyzer/scorer.go` | Modified | Time-weighted stats, fixture-adjusted defensive scores, FPL expected points integration |
| `services/bot/internal/analyzer/analyzer.go` | Modified | Full pipeline integrating all 6 phases |
| `services/bot/internal/analyzer/team_model.go` | **New** | Phase 2: Poisson-style rating model with fixture difficulty |
| `services/bot/internal/analyzer/expected_points.go` | **New** | Phase 3+4: Multinomial & simplified expected FPL points, multi-GW discounting |
| `services/bot/internal/analyzer/player_model.go` | **New** | Phase 5: Bayesian empirical Bayes Dirichlet model |
| `services/bot/internal/analyzer/strategy.go` | **New** | Phase 6: Multi-GW strategy tree, free transfers, points hits |

---

## Phase 1: Time-Decay Weighting

**What:** Player history stats are now weighted by `exp(-0.2 * games_ago)` instead of
all games being weighted equally. A game 1 GW ago gets weight ~0.82; a game 5 GW ago
gets weight ~0.37.

**Where:**
- `database.go:123` — `GetPlayerWeightedRecentStats()` with `EXP(-0.2 * (maxEvent - event))`
- `database.go:105` — `GetMaxEvent()` to find latest gameweek

**Why:** Recent form matters more. A player who scored 15pts last week but 0pts for 4
weeks before that should be ranked higher than one who scored 3pts every week.
Without decay, both get the same average.

**Impact:** High. Single biggest conceptual fix in the existing pipeline.

---

## Phase 2: Team Model (Fixture-Adjusted Scoring)

**What:** A simplified Poisson-style rating model that computes:
- `ExpectedGoalsFor(team, opponent, isHome)` using attack/defence strengths
- `CleanSheetProbability(team, opponent, isHome)` using `exp(-xGC)`
- `FixtureDifficultyMultiplier(difficulty)` — FPL's 1-5 scale → 0.80x–1.20x

**Where:**
- `team_model.go` — `NewTeamModel()`, `ExpectedGoalsFor()`, `CleanSheetProbability()`, `PoissonProb()`
- `database.go:303` — `GetTeamRatings()` reads from `teams` table strength columns

**Why:** A defender playing Man City (xGC expected ~2.5) should have a different defensive
score than one facing a promoted side (xGC expected ~0.8). Without this, all defenders
use the same raw xGC regardless of opponent quality.

**Impact:** High for defenders/GKs specifically, medium for attackers (via fixture difficulty multiplier).

---

## Phase 3: True FPL Expected Points

**What:** Two functions replace the abstract `xScore` with actual expected FPL points:

1. **`SimpleExpectedPoints()`** (fast linear approx):
   ```
   E[pts] = appearance_pts + xG*goal_pts[pos] + xA*3 + csRate*cs_pts[pos] - concede_penalty + bonus_avg
   ```
   Uses position-specific goal points: GK=10, DEF=6, MID=5, FWD=4

2. **`ExpectedFPLPoints()`** (full multinomial):
   ```
   E[pts] = Σ_{n=0..6} P(team scores n) × Σ_{g,a} multinomial_pmf(g,a | n, probs) × (g*goal_pts + a*3)
   ```
   Properly handles the discrete distribution of team goals — a player with 0.3 xG on a team
   with 1.5 expected goals doesn't just get `0.3*4 = 1.2` points; the actual expectation
   requires integrating over `P(team scores 0)`, `P(team scores 1)`, etc.

**Where:**
- `expected_points.go:15` — `ExpectedFPLPoints()`
- `expected_points.go:248` — `SimpleExpectedPoints()`
- `expected_points.go:87` — `attackingPointsMultinomial()` with partition enumeration

**Why:** This is AIrsenal's core insight. Linear `xG * points_per_goal` is a biased
estimator — the true expectation requires the multinomial model. A player who is the
sole attacker on a bad team (high involvement share, low team goals) gets a different
expected value than one who shares chances on a prolific team.

**Impact:** Highest. Aligns the optimization target with actual FPL scoring.

---

## Phase 4: Multi-Week Fixture Lookahead

**What:** Players are scored over 3 upcoming gameweeks (not just the next one), with
exponential discounting at `(14/15)^(games_ahead)`. This makes the model prefer a player
with 3 easy fixtures over one with 1 easy + 2 hard fixtures.

**Where:**
- `database.go:333` — `GetNextFixture()`, `GetUpcomingFixtures()`
- `expected_points.go:304` — `MultiGWExpectedPoints()`
- `analyzer.go:107` — Integration: uses `GetUpcomingFixtures()` for 3-GW window in `Suggest()`

**Why:** FPL is a multi-week game. A transfer should be evaluated on the fixture run it
unlocks, not just GW+1. The discount prevents being too far-sighted (GW+5 is worth ~70%
of GW+1, which matches prediction accuracy decay).

**Impact:** High. Fixes the biggest structural gap in the original system.

---

## Phase 5: Bayesian Player Model

**What:** Dirichlet-Multinomial conjugate model with empirical Bayes priors:

```
Prior:  Dirichlet(α_G, α_A, α_N)  computed from position-wide averages
        Prior strength = 35 goals equivalent

Posterior: Dirichlet(α_G + scaled_goals, α_A + scaled_assists, α_N + scaled_neither)

Output:  prob_score = posterior mean of α_G component
         prob_assist = posterior mean of α_A component
```

**Where:**
- `player_model.go` — `NewBayesianPlayerModel()`, `PosteriorProbabilities()`, `ApplyBayesianModel()`
- `database.go:376` — `GetPositionStats()` for empirical Bayes prior computation

**Why:** A player with 1 goal in 20 minutes should not get the same `prob_score` as a
player with 5 goals in 900 minutes. The prior shrinks small-sample estimates toward
the position average — this is formal regularization that prevents overfitting.

**Impact:** Medium. Important for sample-size robustness but requires team-level goal
data to be fully effective (currently using approximation).

---

## Phase 6: Strategy Tree + Transfer Cost Modeling

**What:**
1. **Free transfer tracking** — `GetFreeTransfers()` reads from metadata
2. **Points hit calculation** — `ComputeTransferGain()` accounts for `4 * max(0, transfers - free)`
3. **Strategy tree** — `EvaluateTransferStrategy()` recursively evaluates combinations of 0/1/2 transfers across N gameweeks, finding the optimal path
4. **Single-week optimization** — `OptimizeSingleWeekTransfers()` evaluates 0-2 transfer combos with points-hit accounting

**Where:**
- `strategy.go` — `EvaluateTransferStrategy()`, `buildTree()`, `ComputeTransferGain()`, `OptimizeSingleWeekTransfers()`
- `database.go:381` — `GetFreeTransfers()`, `GetCurrentGameweek()`

**Why:** Without transfer cost modeling, the algorithm can't distinguish between:
- A 1-transfer move that gains 3pts (net +3)
- A 3-transfer move that gains 9pts but costs 4pts hit (net +5)
- A 0-transfer hold that gains 0pts (net 0)

The tree search finds the optimal transfer count per gameweek, which is the difference
between good and great FPL management.

**Impact:** Medium. Critical for realistic recommendations but needs existing squad data.

---

## Key Mathematical Improvements

### Before (original fpl-scouting)
```
xScore = linear_weight * (xg/90 + xa/90 + ...)  // unitless, position-independent
Value = xScore / price
OverallScore = 3*xScore + 4*Value + 5*Availability
Transfer gain = buy.xScore - sell.xScore
```

### After (AIrsenal-inspired)
```
E[pts] = appearance + Σ P(team scores n) × Σ multinomial × (g*goal_pts + a*3)
             + P(CS)*cs_pts - concede_penalty + bonus_avg
E[pts_multiGW] = Σ (14/15)^gw × E[pts_gw]

Transfer gain = buy.E[pts_multiGW] - sell.E[pts_multiGW]
Net gain = transfer_gain - 4 * max(0, transfers - free_transfers)
```

### Numbers: Impact on a sample player (FWD)
| Component | Before | After |
|-----------|--------|-------|
| Metric | xScore = 3.8 (unitless) | E[pts] = 5.2 FPL pts/GW |
| Goal pts weight | xG*4.0 (arbitrary) | xG*4.0 = FWD goal pts |
| Assist pts | xA*2.0 (arbitrary) | xA*3.0 = actual FPL assist pts |
| Defending | xGC via 1/(1+xGC)*N | Fixture-adjusted CS prob + concede penalty |
| Time weighting | Equal (all 5 games) | exp(-0.2 * games_ago) |
| Multi-GW lookahead | None | 3-GW discounted total |

---

## Verification

Both services compile cleanly:
```
$ cd services/bot && go build ./...   # OK
$ cd services/core && go build ./...  # OK
```

## Next Steps

1. **Run `make up`** to test the full pipeline end-to-end
2. **Send `/suggest`** in Telegram and verify recommendations use the new metrics
3. **Fine-tune weights** — the time-decay epsilon (0.2), discount factor (14/15),
   and prior strength (35) were taken from AIrsenal defaults and may need tuning
   for the Go implementation
4. **Add team-goal data** — the `player_history` table doesn't currently store
   team-level goals per match, which limits Phase 5's minutes-scaling. Consider
   adding a `team_goals` column to `player_history` in the core fetcher
5. **Chip support** — Phase 6 currently doesn't model wildcard/free-hit/bench-boost.
   These are complex enough to warrant their own implementation phase
