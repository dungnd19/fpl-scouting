package analyzer

import (
	"fpl-bot/internal/models"
)

// TeamModel holds team strength ratings and provides fixture-adjusted calculations.
// Uses a simplified Poisson-like model: expected goals = league_avg * attack * opponent_defence.
type TeamModel struct {
	ratings     map[int]models.TeamRating
	leagueAvgGF float64
	leagueAvgGA float64
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
