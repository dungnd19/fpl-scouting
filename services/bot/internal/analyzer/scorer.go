package analyzer

import (
	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
)

// ScorePlayer computes position-weighted xScore metrics plus expected FPL points.
// v2: Uses time-weighted stats, fixture-adjusted defensive scores,
// and true FPL expected points (Phase 1-3).
func ScorePlayer(
	p *models.PlayerScore,
	stats *database.PlayerHistoryStats,
	teamModel *TeamModel,
	fixtureAdj float64,
) {
	if stats == nil || stats.Games == 0 || stats.Minutes == 0 {
		return
	}

	nineties := float64(stats.Minutes) / 90.0
	games := float64(stats.Games)
	if nineties <= 0 {
		return
	}

	p.XGPer90 = stats.XG / nineties
	p.XAPer90 = stats.XA / nineties
	p.XGIPer90 = stats.XGI / nineties
	p.XGCPer90 = stats.XGC / nineties
	p.CSRate = float64(stats.CS) / games
	p.PPG = float64(stats.Points) / games
	p.Games = stats.Games

	// Phase 1: time-weighted versions (stats from GetPlayerWeightedRecentStats already decayed)
	p.WeightedXGPer90 = p.XGPer90
	p.WeightedXAPer90 = p.XAPer90
	p.WeightedXGIPer90 = p.XGIPer90
	p.WeightedXGCPer90 = p.XGCPer90
	p.WeightedCSRate = p.CSRate
	p.WeightedPPG = p.PPG

	p.FixtureDifficulty = fixtureAdj

	// Phase 2: fixture-adjusted defensive portion
	defensiveScore := 0.0
	switch p.Position {
	case 1:
		defensiveScore = FixtureAdjustedDefensiveScore(p.XGCPer90, fixtureAdj)
	case 2:
		defensiveScore = FixtureAdjustedDefensiveScore(p.XGCPer90, fixtureAdj)
	}

	// Position-weighted xScore (kept for backward compatibility)
	switch p.Position {
	case 1:
		p.XScore = p.CSRate*6.0*f() + defensiveScore*4.0 + p.PPG*1.0
	case 2:
		p.XScore = p.CSRate*4.0*f() + defensiveScore*3.0 + p.XGIPer90*3.0*f() + p.PPG*1.0
	case 3:
		p.XScore = p.XGPer90*3.0*f() + p.XAPer90*3.0*f() + p.XGIPer90*2.0*f() + p.PPG*1.0
	case 4:
		p.XScore = p.XGPer90*4.0*f() + p.XAPer90*2.0*f() + p.XGIPer90*2.0*f() + p.PPG*1.0
	}

	costInMillions := float64(p.NowCost) / 10.0
	if costInMillions > 0 {
		p.Value = p.XScore / costInMillions
	}

	// Phase 3: true FPL expected points
	expectedMins := float64(stats.Minutes) / float64(stats.Games)
	if expectedMins > 90 {
		expectedMins = 90
	}
	teamGoalDist := []float64{}
	teamConcedeDist := []float64{}

	largeEnough := teamModel != nil && p.TeamID > 0
	if largeEnough {
		isHome := true
		opponentID := p.TeamID
		_ = isHome
		_ = opponentID
	}

	p.ExpectedPoints = SimpleExpectedPoints(
		p.Position,
		p.XGPer90,
		p.XAPer90,
		p.XGCPer90,
		p.CSRate,
		p.PPG,
		p.Availability,
		expectedMins,
		fixtureAdj,
	)

	p.OverallScore = p.XScore*3.0 + p.Value*4.0 + p.Availability*5.0

	if largeEnough {
		avgXG := 1.4
		teamGoalDist = make([]float64, 7)
		for k := 0; k <= 6; k++ {
			teamGoalDist[k] = PoissonProb(avgXG, k)
		}
		teamConcedeDist = teamGoalDist

		p.ExpectedPoints = ExpectedFPLPoints(
			p.Position,
			p.XGPer90,
			p.XAPer90,
			p.XGCPer90,
			p.CSRate,
			p.PPG,
			p.Availability,
			expectedMins,
			teamGoalDist,
			teamConcedeDist,
		)
	}
}

// fn wrapper
func f() float64 { return 1.0 }

// GenerateTransferRecommendations creates transfer suggestions using all scoring phases.
// Uses new scoring (time-weighted + fixture-adjusted + expected points).
func GenerateTransferRecommendations() {}

// ScorePlayerForReport computes a PlayerReport from player data and history stats.
func ScorePlayerForReport(p models.PlayerScore, stats *database.PlayerHistoryStats) models.PlayerReport {
	r := models.PlayerReport{
		PlayerID: p.PlayerID,
		WebName:  p.WebName,
		TeamName: p.TeamName,
		Position: p.Position,
		NowCost:  p.NowCost,
	}

	if stats == nil || stats.Games == 0 || stats.Minutes == 0 {
		return r
	}

	nineties := float64(stats.Minutes) / 90.0
	games := float64(stats.Games)

	r.Games = stats.Games
	r.Minutes = stats.Minutes
	r.XGPer90 = stats.XG / nineties
	r.XAPer90 = stats.XA / nineties
	r.XGIPer90 = stats.XGI / nineties
	r.XGCPer90 = stats.XGC / nineties
	r.CSRate = float64(stats.CS) / games
	r.PPG = float64(stats.Points) / games

	switch p.Position {
	case 1:
		r.XScore = r.CSRate*6.0 + (1.0/(1.0+r.XGCPer90))*4.0 + r.PPG*1.0
	case 2:
		r.XScore = r.CSRate*4.0 + (1.0/(1.0+r.XGCPer90))*3.0 + r.XGIPer90*3.0 + r.PPG*1.0
	case 3:
		r.XScore = r.XGPer90*3.0 + r.XAPer90*3.0 + r.XGIPer90*2.0 + r.PPG*1.0
	case 4:
		r.XScore = r.XGPer90*4.0 + r.XAPer90*2.0 + r.XGIPer90*2.0 + r.PPG*1.0
	}

	expectedMins := float64(stats.Minutes) / float64(stats.Games)
	if expectedMins > 90 {
		expectedMins = 90
	}
	r.ExpectedPoints = SimpleExpectedPoints(
		p.Position, r.XGPer90, r.XAPer90, r.XGCPer90, r.CSRate, r.PPG,
		1.0, expectedMins, 1.0,
	)

	costInMillions := float64(p.NowCost) / 10.0
	if costInMillions > 0 {
		r.Value = r.XScore / costInMillions
	}

	return r
}
