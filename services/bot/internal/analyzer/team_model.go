package analyzer

import (
	"fpl-bot/internal/models"
)

// TeamModel holds team strength ratings and provides fixture-adjusted calculations.
// Uses a simplified Poisson-like model: expected goals = league_avg * attack * opponent_defence.
type TeamModel struct {
	ratings      map[int]models.TeamRating
	leagueAvgGF  float64
	leagueAvgGA  float64
}

// NewTeamModel creates a team model from repository ratings and computes league averages.
func NewTeamModel(ratings map[int]models.TeamRating) *TeamModel {
	tm := &TeamModel{ratings: ratings}
	tm.computeLeagueAverages()
	return tm
}

func (tm *TeamModel) computeLeagueAverages() {
	var sumAtt, sumDef float64
	n := float64(len(tm.ratings))
	if n == 0 {
		tm.leagueAvgGF = 1.4
		tm.leagueAvgGA = 1.4
		return
	}
	for _, r := range tm.ratings {
		sumAtt += r.AttackStrength
		sumDef += r.DefenceStrength
	}
	tm.leagueAvgGF = sumAtt / n
	tm.leagueAvgGA = sumDef / n
}

// ExpectedGoalsFor computes expected goals for a team against a given opponent, home/away.
// homeBonus is 1.15 for home, 1.0 for away.
func (tm *TeamModel) ExpectedGoalsFor(teamID int, opponentID int, isHome bool) float64 {
	att, ok := tm.ratings[teamID]
	if !ok {
		return tm.leagueAvgGF
	}
	def, ok := tm.ratings[opponentID]
	if !ok {
		return att.AttackStrength
	}

	homeBonus := 1.0
	if isHome {
		homeBonus = att.HomeAdvantage
	}

	expected := tm.leagueAvgGF * att.AttackStrength * def.DefenceStrength * homeBonus
	if expected < 0.1 {
		expected = 0.1
	}
	return expected
}

// ExpectedGoalsAgainst computes expected goals conceded against a given opponent.
func (tm *TeamModel) ExpectedGoalsAgainst(teamID int, opponentID int, isHome bool) float64 {
	def, ok := tm.ratings[teamID]
	if !ok {
		return tm.leagueAvgGA
	}
	att, ok := tm.ratings[opponentID]
	if !ok {
		return att.AttackStrength
	}

	awayBonus := 1.0
	if !isHome {
		awayBonus = tm.ratings[opponentID].HomeAdvantage
	}

	expected := tm.leagueAvgGA * att.AttackStrength * def.DefenceStrength * awayBonus
	if expected < 0.1 {
		expected = 0.1
	}
	return expected
}

// CleanSheetProbability estimates clean sheet probability using Poisson P(concede=0).
// P(CS) = exp(-expected_goals_conceded)
func (tm *TeamModel) CleanSheetProbability(teamID int, opponentID int, isHome bool) float64 {
	xGC := tm.ExpectedGoalsAgainst(teamID, opponentID, isHome)
	return expApprox(-xGC)
}

// PoissonProb returns P(goals = k) given lambda using the Poisson PMF.
// For k >= 7, use normal approximation to avoid overflow.
func PoissonProb(lambda float64, k int) float64 {
	if k < 0 {
		return 0
	}
	if lambda <= 0 {
		if k == 0 {
			return 1
		}
		return 0
	}
	if k > 6 {
		return 0
	}
	result := 1.0
	for i := 1; i <= k; i++ {
		result *= lambda / float64(i)
	}
	result *= expApprox(-lambda)
	if result < 0 {
		return 0
	}
	return result
}

// TeamScoringDistribution returns P(team scores k goals) for k = 0..6
func (tm *TeamModel) TeamScoringDistribution(teamID int, opponentID int, isHome bool) []float64 {
	lambda := tm.ExpectedGoalsFor(teamID, opponentID, isHome)
	dist := make([]float64, 7)
	for k := 0; k <= 6; k++ {
		dist[k] = PoissonProb(lambda, k)
	}
	return dist
}

// FixtureDifficultyMultiplier converts FPL difficulty (1-5) to a scoring multiplier.
// difficulty 1 (easy) → 1.20x, 3 → 1.0x, 5 (hard) → 0.80x.
func FixtureDifficultyMultiplier(difficulty int) float64 {
	if difficulty <= 0 {
		return 1.0
	}
	return 1.0 - (float64(difficulty)-3.0)*0.1
}

// FixtureAdjustedDefensiveScore adjusts the defensive xGC component using fixture difficulty.
// For defenders/GKs, playing a hard fixture reduces the clean sheet bonus expectation.
func FixtureAdjustedDefensiveScore(xGCPer90 float64, difficulty int) float64 {
	if difficulty <= 0 {
		difficulty = 3
	}
	adjXGC := xGCPer90 * (1.0 + (float64(difficulty)-3.0)*0.15)
	if adjXGC < 0 {
		adjXGC = 0
	}
	return 1.0 / (1.0 + adjXGC)
}

// expApprox is a simple series expansion for exp(x) for small-ish |x|.
// Uses: exp(x) ≈ 1 + x + x²/2! + x³/3! + ...
func expApprox(x float64) float64 {
	if x > 20 {
		return 485165195.4 // large but bounded
	}
	if x < -20 {
		return 0
	}
	result := 1.0
	term := 1.0
	for i := 1; i <= 30; i++ {
		term *= x / float64(i)
		result += term
		if term < 1e-15 && term > -1e-15 {
			break
		}
	}
	if result < 0 {
		return 0
	}
	return result
}
