# AIrsenal Algorithm Strategy — Analysis & Application to fpl-scouting

This document analyzes AIrsenal's algorithmic approach, explains why each component works
mathematically, and provides a concrete roadmap for applying the same logic to this Go project.

---

## 1. Summary of AIrsenal's Algorithmic Stack

AIrsenal is a **two-stage hierarchical Bayesian model** paired with **combinatorial
multi-week optimization**. It moves from raw match data all the way to an executable
transfer plan spanning multiple gameweeks.

```
 Raw match results ──→ [1] Team model (Poisson)  ──→  P(team scores N goals)
 Player goals/assists ──→ [2] Player model (Dirichlet) ──→  P(player involved | goal)

 [1] + [2] ──→ Expected points per player per fixture (multinomial expectation)
 Expected points ──→ [3] Multi-week strategy tree + GA squad builder  ──→  Optimal transfers
```

---

## 2. Component Breakdown — What It Does & Why It Works

### 2.1 Team-Level Model: Extended Dixon-Coles (Poisson Regression)

**What it does:** For every future fixture, it outputs `P(home_team scores N goals)` and
`P(away_team scores N goals)` for N = 0..10.

Files: `airsenal/framework/bpl_interface.py` (321 lines)
Uses the `bpl-next` library which implements:

```
log(μ_home) = α + attack_home + defence_away + γ * home_advantage
log(μ_away) = α + attack_away + defence_home

P(goals = k) = Poisson(k | μ) = (μ^k * e^(-μ)) / k!
```

The model includes a Dixon-Coles low-scoring correction term `τ(ρ)` and exponential time
decay: `weight = exp(-0.9 * years_ago)`.

**Why it works:**
- This is the gold-standard model for football scorelines, published in peer-reviewed
  statistics literature (Dixon & Coles, 1997).
- It captures both offensive and defensive strength as latent variables, naturally
  adjusting for strength of schedule. A goal against Man City "means" less than a goal
  against a relegation side.
- The Poisson assumption is well-validated: football goals per match empirically follow
  a Poisson (or negative binomial) distribution.
- Time decay ensures the model adapts to changing form — recent matches carry more weight.
- FIFA team ratings as covariates solve the "promoted teams" cold-start problem
  (new teams have no prior match data in the league).

**Current fpl-scouting gap:** No team-level model exists. xGC/90 is used from the
player_history table, but this is a raw stat without strength-of-schedule adjustment.
A defender facing Man City and a defender facing a promoted side have the same xGC — the
model doesn't differentiate.

---

### 2.2 Player-Level Model: Bayesian Dirichlet-Multinomial

**What it does:** For each player, it estimates `P(score | team scores)` and
`P(assist | team scores)` using a Bayesian conjugate model.

Files: `airsenal/framework/player_model.py` (341 lines)

```
Prior:  Dirichlet(α_G, α_A, α_N) where α comes from empirical Bayes
        across all players in that position
        Prior strength: n_goals_prior = 35 (equivalent to 35 goals of prior belief)

Data:   count(goals), count(assists), count(neither)
        → scaled by actual minutes / team minutes  (minutes-scaling)
        → weighted by exp(-0.2 * years_ago)       (time decay)

Posterior: Dirichlet(α_G + scaled_goals, α_A + scaled_assists, α_N + scaled_neither)

Output:  prob_score  = posterior mean of goal component
         prob_assist = posterior mean of assist component
```

**Why it works:**
- **Conjugate prior:** Because Dirichlet is the conjugate prior for the Multinomial, the
  posterior is also Dirichlet with simply `prior + counts`. This makes computation trivial
  — no MCMC needed (though a Numpyro variant exists for full posterior sampling).
- **Empirical Bayes:** Using the position-wide average as prior means a new player who's
  scored once in 3 games doesn't get an absurdly high `prob_score`. The prior shrinks
  estimates toward the group mean — this is formal regularization.
- **Minutes-scaling:** A 15-minute cameo shouldn't count the same as a 90-minute start.
  Scaling goals by `player_minutes / team_minutes` accounts for time on pitch.
- **Separate time decay (`epsilon=0.2`):** Players change faster than teams (form,
  age, role changes), so a steeper decay than the team model is appropriate.

**Current fpl-scouting gap:** xG/xA/xGI are summed directly from the last 5 games and
normalized per-90. There is no Bayesian regularization, no minutes-scaling within games,
and no time decay — all 5 games are weighted equally, and a player with 1 lucky goal in
20 minutes gets the same xG/90 credit as a player who consistently generates chances.

---

### 2.3 Expected Points: Multinomial Expectation (the Core Innovation)

**What it does:** Given `P(team scores N goals)` from the team model and
`P(player involved | goal)` from the player model, it computes **true expected FPL points**
by enumerating all possible partitions of goals into (scored, assisted, neither).

Files: `airsenal/framework/prediction_utils.py:282-330` (`get_attacking_points`)

```
E[attacking_points] =
  Σ_{n=0..MAX_GOALS} P(team scores n)  ×   [ team-level probability ]
    Σ_{(g,a,n-g-a) partition of n}
      multinom_pmf(g,a,n-g-a | n, probs)  ×  [ player involvement probability ]
      (pts_per_goal * g + pts_per_assist * a)   [ FPL points earned ]
```

Where `probs = (player_prob_score * mins/90, player_prob_assist * mins/90, 1 - rest)`.

**Why it works — this is the key insight:**
- Most FPL models use a linear approximation: `xG * points_per_goal + xA * points_per_assist`.
  This is **wrong in expectation** because of the multinomial nature of the problem.
- If a team is expected to score 1.5 goals, you can't just multiply the player's 20%
  goal involvement by 1.5 and get 0.3 expected goals. The correct calculation integrates
  over the **full distribution** of team goals: there's some probability of 0 goals,
  some of 1 goal, some of 2, etc. The player's involvement probability interacts with
  each possible team goal count differently.
- The multinomial correctly handles that a player can both score AND assist in the same
  match (partition enumeration covers this).
- Position-specific goal points (GK=10, DEF=6, MID=5, FWD=4) are baked into the
  expectation — a goal for a defender is worth 6pts, not the generic 4 or 5 that many
  simple models use.

**Complete expected points formula:**

```
E[points] = E[appearance] + E[attacking] + E[defending] + E[bonus] + E[saves] + E[cards]
```

The expectation is then averaged over `recent_minutes` (last 3 matches of playing time)
to account for the fact that a player might not play the full 90 next week.

The defending component (`prediction_utils.py:333-355`):
```
E[defending] = P(clean sheet) * cs_points[position]
               - Σ_{n} (n//2) * (mins/90) * P(concede n)
```
CS requires ≥60 minutes, and conceding penalty only applies to DEF and GK.

**Current fpl-scouting gap:** The xScore formula is a pure linear weighting of per-90
metrics. It uses `xG/90 * weight + xA/90 * weight` which is the linear approximation
that AIrsenal avoids. This doesn't account for the actual FPL points earned (a DEF goal
is worth 6 points, not some abstract "xScore" unit), and doesn't account for team-level
scoring distributions.

---

### 2.4 Multi-Week Discounting: Why Look Ahead?

**What it does:** When evaluating transfers, points in future gameweeks are discounted
exponentially:

```
discount_factor = (14/15)^(gameweek_ahead)
discounted_score = Σ_{gw} discount_factor * squad_points(gw)
```

Files: `airsenal/framework/optimization_utils.py:642-665`

Example: A transfer that brings in a player with good fixtures in GW+1, GW+2, GW+3 will
have a higher discounted total than one that only helps in GW+1. The discount prevents
the model from being too "far-sighted" — next week matters most.

**Why it works:**
- Information is scarce for distant gameweeks (injuries, rotations, form changes).
- FPL points are not equally valuable across weeks — performing well in a high-variance
  week (double gameweeks, etc.) matters more.
- The exponential decay at `14/15 ≈ 0.933` means GW+5 is worth ~70% of GW+1, which is
  about right for the rate at which prediction accuracy degrades.

**Current fpl-scouting gap:** Transfer analysis is purely single-gameweek. No lookahead.
A player with 3 consecutive easy fixtures is treated identically to one with 3 tough
fixtures — only current stats matter.

---

### 2.5 Strategy Tree + Chip Optimization

**What it does:** Recursively evaluates transfer decisions across N future gameweeks.

```
Root node (GW 1)
  ├── 0 transfers GW1 → node(GW2, free_transfers+1)
  │     ├── 0 transfers GW2 → ...
  │     ├── 1 transfer GW2  → ...
  │     └── 2 transfers GW2 → ...
  ├── 1 transfer GW1  → node(GW2, free_transfers)
  │     ├── 0 transfers ...
  │     └── ...
  ├── 2 transfers GW1 → node(GW2, free_transfers-1)  → points_hit = -4
  ├── Wildcard GW1    → full squad rebuild (GA), 0 hit
  └── Free Hit GW1    → unlimited, 0 hit, squad reverts after
```

Each leaf node gets a total discounted score. The path with the highest score wins.

**Transfer execution methods by count:**
| Scenario | Method |
|----------|--------|
| 0 transfers | Keep squad as-is |
| 1 transfer | Exhaustive: try removing each of 15, add best replacement |
| 2 transfers | Brute-force all pairs: try removing every pair, add best replacements |
| 3+ transfers | Random search (100 iterations) with triangular PDF bias toward top players |
| Wildcard/Free Hit | Genetic algorithm (DEAP): population=100, generations=100, tournament sel=3 |

**Why it works:**
- The branching factor is small enough (3 choices × ~5 GWs = 3^5 = 243 leaves) that
  tree search is feasible. With multiprocessing, each branch is evaluated independently.
- Points hits are accounted for with proper discounting, so the model naturally avoids
  taking unnecessary hits.
- Chip optimization means the model doesn't just optimize transfers — it optimizes
  **when** to use each chip, which is often the difference between a good and great
  FPL season.

**Current fpl-scouting gap:** No multi-week optimization. No chip strategy. No transfer
cost modeling. Free transfers aren't tracked.

---

### 2.6 Formation & Lineup Optimization

**What it does:** Given a 15-player squad, the `Squad.optimize_subs()` method tries all
7 valid formations and picks the combination that maximizes expected points for the
starting 11.

Valid formations: (3,4,3), (3,5,2), (4,3,3), (4,4,2), (4,5,1), (5,3,2), (5,4,1), (5,2,3)

For each formation:
1. Pick top-N per position by predicted points
2. Pick best GK, bench the other
3. Assign captain (highest predicted points) and vice-captain (2nd highest)
4. Order subs by predicted points (for auto-substitution logic)

**Why it works:**
- This is brute-force over a small search space (7 formations × position permutations)
  but guarantees the optimal lineup for any given squad.
- Captaincy is the most important decision in FPL (doubles points), so always assigning
  the highest expected-points player as captain is optimal under expected value.

**Current fpl-scouting gap:** No formation or lineup optimization. The system recommends
which player to buy/sell but doesn't tell you who to bench or captain.

---

### 2.7 Genetic Algorithm for Squad Building (Wildcard)

**What it does:** When rebuilding the entire 15-player squad (wildcard or free hit),
AIrsenal uses DEAP (Distributed Evolutionary Algorithms in Python).

Files: `airsenal/framework/optimization_squad.py` (471 lines)

```
Population: 100 individuals
Generations: 100
Selection: Tournament (size=3)
Crossover: Two-point (prob=0.7)
Mutation: Uniform integer (prob=0.3)

Each individual: a 15-length integer vector [p1_id, p2_id, ..., p15_id]
Fitness: total discounted expected points of the squad after lineup optimization
```

**Why it works:**
- The search space for a 15-player squad is enormous (~500 choose 15 with constraints).
  Exhaustive search is impossible. GA is a standard metaheuristic for this kind of
  combinatorial optimization with constraints.
- Tournament selection maintains diversity better than roulette wheel.
- The GA respects budget, position limits, and team limits via penalty in the fitness
  function.

**Current fpl-scouting gap:** No squad-wide optimization. The current approach
iterates sell/buy pairs independently within each position.

---

## 3. Gap Analysis: fpl-scouting vs AIrsenal

| Capability | AIrsenal | fpl-scouting (current) | Priority |
|-----------|----------|----------------------|----------|
| Team-level goal model (Poisson/Dixon-Coles) | ✓ | ✗ | HIGH |
| Player-level Bayesian model (Dirichlet) | ✓ | ✗ | MEDIUM |
| Multinomial expected points (not linear approx) | ✓ | ✗ | HIGH |
| Multi-week discounting | ✓ | ✗ | HIGH |
| Strategy tree search + chip optimization | ✓ | ✗ | MEDIUM |
| Formation/lineup optimization | ✓ | ✗ | LOW |
| Genetic algorithm squad builder | ✓ | ✗ | LOW |
| Time-decay weighting | ✓ (exp decay) | ✗ | HIGH |
| Minutes-scaling (sub appearances) | ✓ | ✗ | MEDIUM |
| Free transfer tracking + points hits | ✓ | ✗ | MEDIUM |
| Proper sell-price rules | ✓ | Partial (uses selling_price col) | MEDIUM |
| Empirical Bayes prior (regularization) | ✓ | ✗ | MEDIUM |
| Fixture difficulty adjusted scoring | ✓ (via team model) | ✗ | HIGH |

---

## 4. Incremental Implementation Plan for fpl-scouting

These are ordered by impact-to-effort ratio. Each phase builds on the previous one.

### Phase 1: Time-Decay + Minutes-Scaling (Minimal Changes)

**Goal:** Make the existing xG/xA/xCS aggregation statistically sound.

**Files to modify:** `services/bot/internal/analyzer/scorer.go`, `database/database.go`

**Changes:**
1. Add time-decay weighting to `GetPlayerRecentStats`:
   - Current: `SUM(xG), SUM(xA), ...` — all games weighted equally
   - New: `SUM(weight * xG), SUM(weight * xA), ...` where `weight = exp(-0.2 * games_ago)`
   - A game 1 week ago gets weight 0.82; a game 5 weeks ago gets weight 0.37
   - This requires adding `event` (gameweek number) to the query

2. Track `games_ago` relative to the most recent gameweek in the dataset:
   ```go
   // In SQL, compute per-row weight:
   // SELECT SUM(expected_goals * POW(0.82, max_event - event)) AS weighted_xg ...
   ```

3. Minutes-scaling for the `GetPlayerRecentStats` query already sums minutes, but could
   add a secondary normalized metric: `xG_scaled = xG * (team_goals / total_team_goals_in_match)`
   if team-level data is available.

**Expected impact:** High. The single biggest flaw in the current approach is treating
all 5 games equally. A player's form last week matters much more than their form 5 weeks
ago.

---

### Phase 2: Team-Level Poisson Model (Replaces Raw xGC)

**Goal:** Give each player a fixture-strength-adjusted defensive metric instead of raw xGC.

**Files to add:** `services/bot/internal/analyzer/team_model.go` (new)

**Approach:**
Instead of fitting a full Dixon-Coles model (complex, requires external library),
implement a **simplified Poisson rating model**:

```go
type TeamRating struct {
    AttackStrength  float64  // goals scored relative to average
    DefenceStrength float64  // goals conceded relative to average
}

// For a home team against away team:
//   expected_goals_home = league_avg_home_goals * attack_home * defence_away
//   expected_goals_away = league_avg_away_goals * attack_away * defence_home
```

This can be fitted using:
- Sum of `goals_scored` and `goals_conceded` from `player_history`, grouped by team and gameweek
- OR: Load from the fixtures table's `team_h_difficulty` / `team_a_difficulty` as a simpler proxy
- Use exponential time decay on matches

**Expected output:** For each player, compute `xGC_expected` = the xGC you'd expect
against the **average** opponent, then compute `defensive_overperformance = 1 / (1 + xGC_actual/90) - 1 / (1 + xGC_expected/90)`.
This adjusts the defensive score component for strength of schedule.

**Expected impact:** High. Currently all defenders operate with the same formula
regardless of fixture difficulty.

---

### Phase 3: Multinomial Expected Points (True FPL Points Expectation)

**Goal:** Replace the linear `xScore` with actual expected FPL points.

**Files to add:** `services/bot/internal/analyzer/expected_points.go` (new)

**Approach:**
Since fpl-scouting already has `expected_goals` and `expected_assists` per player (FPL
API provides these), we can approximate the multinomial model:

```go
// For a given player with xG, xA, minutes:
func ExpectedAttackingPoints(position int, xG90, xA90, mins float64) float64 {
    // Scale by minutes
    prScore := xG90  // FPL's xG IS already the probability of scoring, per-90
    prAssist := xA90
    // But xG doesn't perfectly map to P(score | team scores) — we need to adjust

    // Approximate: use the player's xG/xA directly as multinomial probabilities
    // and use average team xG as the scoring rate
    teamGoalProb := PoissonDistribution(avgTeamXG) // from Phase 2
    return computeAttackingExp(prScore, prAssist, teamGoalProb, position)
}
```

Alternatively, for each position, compute:
```go
// Simplified FPL points expectation:
E_points = appearance_pts + (xG/90 * goal_points[position]) + (xA/90 * assist_points)
           + cs_prob * cs_points[position] - concede_penalty + bonus_avg

// Goal points are position-specific:
//   GK=10, DEF=6, MID=5, FWD=4
// CS points:
//   GK=4, DEF=4, MID=1, FWD=0
```

**Expected impact:** Very high. This is the single most important conceptual improvement
— aligning the optimization target with actual FPL scoring rules.

---

### Phase 4: Multi-Week Scoring With Fixture Lookahead

**Goal:** Score transfers based on the combined 3-5 gameweek horizon, not just next week.

**Files to modify:** `services/bot/internal/analyzer/analyzer.go`, new extension file.

**Approach:**
```go
func MultiWeekExpectedPoints(player PlayerScore, nextGW int, numGWs int) float64 {
    discount := 0.933  // (14/15)
    for gw := nextGW; gw < nextGW+numGWs; gw++ {
        fixture := getFixture(player.TeamID, gw)
        difficulty := fixture.TeamDifficulty(player.TeamID)
        gwPoints := adjustedExpectedPoints(player, difficulty)
        multiWeekPoints += gwPoints * math.Pow(discount, float64(gw-nextGW))
    }
    return multiWeekPoints
}
```

This requires fixture data (already in the `fixtures` table). The simplest version:
score each player based on the fixture difficulty multiplier.

**Expected impact:** High. Players with good fixture runs get properly boosted.

---

### Phase 5: Bayesian Player Model (Regularization)

**Goal:** Prevent overfitting on 5 games of data by using a position-wide prior.

**Files to add:** `services/bot/internal/analyzer/player_model.go` (new)

**Approach:**
```go
type DirichletPrior struct {
    AlphaGoal   float64
    AlphaAssist float64
    AlphaNeither float64
}

func ComputeEmpiricalBayesPrior(players []PlayerScore, position int) DirichletPrior {
    // For all players in this position:
    //   total_goals_scored, total_assists
    //   total_involvements = total_goals + total_assists
    //   total_neither = total_team_goals - total_involvements
    // prior_strength = 35  // equivalent to 35 goals of prior weight
    // alpha_G = prior_strength * (total_goals / total_team_goals)
    // alpha_A = prior_strength * (total_assists / total_team_goals)
    // alpha_N = prior_strength * (1 - alpha_G - alpha_A)
}

func PosteriorProbabilities(player PlayerScore, prior DirichletPrior) (float64, float64) {
    // scaled_goals = player.goals_scored * (team_minutes / player_minutes)
    // scaled_assists = player.assists * (team_minutes / player_minutes)
    // scaled_neither = team_goals - scaled_goals - scaled_assists
    // posterior G = prior.G + scaled_goals
    // posterior A = prior.A + scaled_assists
    // posterior N = prior.N + scaled_neither
    // total = posterior.G + posterior.A + posterior.N
    // prob_score = posterior.G / total
    // prob_assist = posterior.A / total
}
```

**Expected impact:** Medium. This makes the model more robust against small-sample noise
but requires team-goal data at the match level (not currently stored).

---

### Phase 6: Strategy Tree & Chip Optimization (Long-Term)

**Goal:** Full multi-week transfer strategy optimization with chip planning.

**Files to add:** `services/bot/internal/analyzer/strategy.go` (new)

**Approach:**
```go
type StrategyNode struct {
    Gameweek      int
    TransfersUsed int
    FreeTransfers int
    Squad         []int  // player IDs
    ChipPlayed    string // "", "WC", "FH", "BB", "TC"
    Points        float64
    Children      []*StrategyNode
}

func BuildStrategyTree(currentSquad []int, startGW int, numGWs int) *StrategyNode {
    // Recursively build tree:
    // For each GW, branch on: 0, 1, 2 transfers (if free transfers allow)
    // Also branch on: wildcard, free hit, bench boost, triple captain
    // Each node: compute optimal squad for that GW, then recurse
    // Prune: don't explore negative-points-hit paths beyond threshold
}
```

**Expected impact:** High (but high effort). This is what separates a good recommendation
system from a great one. However, it's a complete architectural addition.

---

## 5. Quick Wins: Changes That Require <50 Lines of Code

These can be done immediately in a single session:

### 5.1 Use actual FPL scoring constants
Replace the abstract `xScore` weights with real FPL points:
```go
// Current (scorer.go):
p.XScore = p.XGPer90*4.0 + p.XAPer90*2.0 + p.XGIPer90*2.0 + p.PPG*1.0  // FWD

// Better:
p.XScore = p.XGPer90*4.0 + p.XAPer90*3.0 + p.XGIPer90*2.0 + p.PPG*1.0
//        ^goal pts(FWD)=4   ^assist=3 for all  ^G+A=6 for FWD
```

### 5.2 Add exponential time decay to GetPlayerRecentStats
```sql
-- Current:
SELECT SUM(expected_goals) as xg_sum, ...
FROM player_history
WHERE player_id = ? AND minutes > 0
ORDER BY event DESC LIMIT 5

-- Better:
SELECT SUM(expected_goals * POW(0.82, (SELECT MAX(event) FROM player_history) - event)) as xg_sum, ...
FROM player_history
WHERE player_id = ? AND minutes > 0
ORDER BY event DESC LIMIT 5
```

### 5.3 Add fixture difficulty modifier
```go
func FixtureAdjustedXScore(p *PlayerScore, nextGW int) float64 {
    fixture := repo.GetNextFixture(p.TeamID, nextGW)
    multiplier := 1.0 - (fixture.Difficulty-3.0)*0.1  // difficulty 5 → 0.8x, difficulty 1 → 1.2x
    return p.XScore * multiplier
}
```

### 5.4 Track free transfers and model points hits
```go
type TransferPlan struct {
    TransfersOut []int
    TransfersIn  []int
    FreeTransfers int
    PointsHit     int  // max(0, 4 * (len(transfers) - freeTransfers))
}

func (tp TransferPlan) NetGain(beforeScore, afterScore float64) float64 {
    return (afterScore - beforeScore) - float64(tp.PointsHit)
}
```

---

## 6. Key Differences in Philosophy

| Aspect | AIrsenal (Python) | fpl-scouting (Go) |
|--------|------------------|-------------------|
| Core approach | Bayesian generative model + optimization | Heuristic linear scoring |
| Output | Expected FPL points (cardinal value) | Unitless xScore (ordinal ranking) |
| Data usage | All historical data (weighted) | Last 5 games (equal weight) |
| Recommendation | Multi-week strategy (when + who) | Single transfer (who) |
| Chip strategy | Optimized timing | Not addressed |
| Budget | Full sale price rules | Simple bank check |
| Regularization | Empirical Bayes prior | None |
| Training | Statistical model fitting per position | No training (fixed weights) |
| Computational cost | Heavy (optimization tree + GA) | Light (single-pass scoring) |

---

## 7. Conclusion

AIrsenal's strategy works because it treats FPL as a **proper statistical estimation
problem** rather than a simple heuristic ranking task:

1. **Model the generating process** (Poisson team goals → Dirichlet player involvement)
   rather than just correlating stats with points.
2. **Compute true expectations** via the multinomial distribution rather than using
   linear approximations.
3. **Optimize over a multi-week horizon** with proper discounting rather than myopically
   optimizing one gameweek at a time.
4. **Regularize with empirical Bayes** to avoid overfitting on small samples.
5. **Enforce all real FPL constraints** (budget, formation, team limits, transfer costs,
   chip rules) in the optimization, not as an afterthought.

The incremental plan in Section 4 shows how to apply these principles to fpl-scouting
without requiring a full rewrite. Start with Phase 1-2 (time decay + team model) for
immediate impact, then add multinomial expected points (Phase 3) for the biggest
conceptual leap.
