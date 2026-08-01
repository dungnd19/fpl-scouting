package analyzer

import (
	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
)

func ScorePlayer(
	p *models.PlayerScore,
	stats *database.PlayerHistoryStats,
	teamModel *TeamModel,
	fixtureAdj float64,
	fixtureDifficulty int,
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

	p.WeightedXGPer90 = p.XGPer90
	p.WeightedXAPer90 = p.XAPer90
	p.WeightedXGIPer90 = p.XGIPer90
	p.WeightedXGCPer90 = p.XGCPer90
	p.WeightedCSRate = p.CSRate
	p.WeightedPPG = p.PPG

	p.FixtureDifficulty = fixtureAdj

	expectedMins := float64(stats.Minutes) / float64(stats.Games)
	if expectedMins > 90 {
		expectedMins = 90
	}

	minsFraction := expectedMins / 90.0
	if minsFraction > 1.0 {
		minsFraction = 1.0
	}

	defensiveScore := 0.0
	if p.Position == 1 || p.Position == 2 {
		defensiveScore = FixtureAdjustedDefensiveScore(p.XGCPer90, fixtureDifficulty)
	}

	defconScore := 0.0
	if p.Position == 2 {
		cbitPer90 := float64(stats.CBIT) / nineties
		p.CBITPer90 = cbitPer90
		defconScore = expectedDefconPoints(cbitPer90, minsFraction, 10)
	} else if p.Position == 3 {
		cbirtPer90 := float64(stats.CBIT+stats.Tackles+stats.Recoveries) / nineties
		p.CBIRTPer90 = cbirtPer90
		defconScore = expectedDefconPoints(cbirtPer90, minsFraction, 12)
	}

	switch p.Position {
	case 1: // GK
		p.XScore = p.CSRate*6.0 + defensiveScore*4.0 + p.PPG*1.0
	case 2: // DEF
		p.XScore = p.CSRate*5.0 + defensiveScore*3.0 + p.XGPer90*3.0 + p.XAPer90*2.0 + defconScore*2.0 + p.PPG*1.0
	case 3: // MID
		p.XScore = p.XGPer90*5.0 + p.XAPer90*3.0 + p.CSRate*1.0 + defconScore*1.0 + p.PPG*1.0
	case 4: // FWD
		p.XScore = p.XGPer90*4.0 + p.XAPer90*3.0 + p.PPG*1.0
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
		defconScore,
	)

	costInMillions := float64(p.NowCost) / 10.0
	if costInMillions > 0 && p.ExpectedPoints > 0 {
		p.Value = p.ExpectedPoints / costInMillions
	}

	p.OverallScore = p.ExpectedPoints*3.0 + p.Value*4.0 + p.Availability*5.0
}

func expectedDefconPoints(per90Rate float64, minsFraction float64, threshold int) float64 {
	if per90Rate <= 0 || minsFraction <= 0 || threshold <= 0 {
		return 0
	}
	expectedActions := per90Rate * minsFraction
	prob := expectedActions / float64(threshold)
	if prob > 1.0 {
		prob = 1.0
	}
	return prob * 2.0
}

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

	defensiveScore := 0.0
	if p.Position == 1 || p.Position == 2 {
		defensiveScore = 1.0 / (1.0 + r.XGCPer90)
	}

	defconScore := 0.0
	if p.Position == 2 {
		cbitPer90 := float64(stats.CBIT) / nineties
		defconScore = expectedDefconPoints(cbitPer90, 1.0, 10)
	} else if p.Position == 3 {
		cbirtPer90 := float64(stats.CBIT+stats.Tackles+stats.Recoveries) / nineties
		defconScore = expectedDefconPoints(cbirtPer90, 1.0, 12)
	}

	switch p.Position {
	case 1:
		r.XScore = r.CSRate*6.0 + defensiveScore*4.0 + r.PPG*1.0
	case 2:
		r.XScore = r.CSRate*5.0 + defensiveScore*3.0 + r.XGPer90*3.0 + r.XAPer90*2.0 + defconScore*2.0 + r.PPG*1.0
	case 3:
		r.XScore = r.XGPer90*5.0 + r.XAPer90*3.0 + r.CSRate*1.0 + defconScore*1.0 + r.PPG*1.0
	case 4:
		r.XScore = r.XGPer90*4.0 + r.XAPer90*3.0 + r.PPG*1.0
	}

	expectedMins := float64(stats.Minutes) / float64(stats.Games)
	if expectedMins > 90 {
		expectedMins = 90
	}
	r.ExpectedPoints = SimpleExpectedPoints(
		p.Position, r.XGPer90, r.XAPer90, r.XGCPer90, r.CSRate, r.PPG,
		1.0, expectedMins, 1.0, defconScore,
	)

	costInMillions := float64(p.NowCost) / 10.0
	if costInMillions > 0 && r.ExpectedPoints > 0 {
		r.Value = r.ExpectedPoints / costInMillions
	}

	return r
}
