package analyzer

import (
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
)

// Service handles on-demand player analysis and recommendations.
// v2: integrated with all 6 phases:
//
//	Phase 1: time-decay weighted stats
//	Phase 2: fixture-strength-adjusted team model
//	Phase 3: true FPL expected points (multinomial + simplified)
//	Phase 4: multi-week fixture lookahead with discounting
//	Phase 5: Bayesian empirical Bayes regularization
//	Phase 6: strategy optimization with free transfers + points hits
type Service struct {
	repo       *database.Repository
	userID     string
	teamModel  *TeamModel
	bayesModel *BayesianPlayerModel
}

// NewService creates a new analyzer service.
func NewService(repo *database.Repository, userID string) *Service {
	svc := &Service{repo: repo, userID: userID}

	// Phase 2: build team model from stored strength data
	ratings, err := repo.GetTeamRatings()
	if err != nil {
		log.Printf("Warning: failed to load team ratings: %v", err)
		svc.teamModel = nil
	} else if len(ratings) > 0 {
		svc.teamModel = NewTeamModel(ratings)
	}

	// Phase 5: build Bayesian model
	bayesModel, err := NewBayesianPlayerModel(repo)
	if err != nil {
		log.Printf("Warning: failed to fit Bayesian model: %v, using defaults", err)
		svc.bayesModel = NewBayesianPlayerModelDefault()
	} else {
		svc.bayesModel = bayesModel
	}

	return svc
}

// Suggest runs analysis and returns transfer recommendations for the user's current squad.
// numGameweeks controls the fixture lookahead window (1-3).
// Phase 1-6 integrated scoring pipeline.
func (s *Service) Suggest(numGameweeks int) ([]models.Recommendation, error) {
	s.repo.CleanOldRecommendations(7)

	myTeamIDs, err := s.repo.GetMyTeamPlayerIDs(s.userID)
	if err != nil {
		log.Printf("Warning: could not load squad: %v", err)
		myTeamIDs = nil
	}
	hasTeam := len(myTeamIDs) > 0

	sellingPrices := make(map[int]int)
	bank := 0
	if hasTeam {
		if sp, err := s.repo.GetMyTeamSellingPrices(s.userID); err == nil {
			sellingPrices = sp
		}
		if b, err := s.repo.GetBank(); err == nil {
			bank = b
		}
		log.Printf("Bank: £%.1fm", float64(bank)/10.0)
	}

	players, err := s.repo.GetActivePlayersWithExpected(90)
	if err != nil {
		return nil, fmt.Errorf("failed to load players: %w", err)
	}

	if hasTeam {
		log.Printf("Analyzing %d players against squad of %d...", len(players), len(myTeamIDs))
	} else {
		log.Printf("Analyzing %d players (no squad data)...", len(players))
	}

	// Phase 4: determine current gameweek for fixture lookahead
	currentGW, _ := s.repo.GetCurrentGameweek()
	if currentGW == 0 {
		currentGW = 1
	}

	// Score every player using time-weighted stats + fixture adjustment
	// Detect cold-start: use multi-season blended stats if < 3 games in current season
	isColdStart, _ := s.repo.IsColdStart(3)
	hasSeasonData, _ := s.repo.HasSeasonData()
	if isColdStart && hasSeasonData {
		log.Printf("Cold start detected (< 3 games) — blending prior season data (vaastav)")
	}

	for i := range players {
		var stats *database.PlayerHistoryStats
		var err error

		if isColdStart && hasSeasonData {
			stats, err = s.repo.GetPlayerMultiSeasonBlendedStats(players[i].PlayerID, 5)
		} else {
			stats, err = s.repo.GetPlayerWeightedRecentStats(players[i].PlayerID, 5)
		}
		if err != nil {
			continue
		}

		fixtureAdj := 1.0
		fixtureDifficulty := 3
		if s.teamModel != nil {
			fixture, err := s.repo.GetNextFixture(players[i].TeamID, currentGW)
			if err == nil && fixture != nil {
				fixtureAdj = FixtureDifficultyMultiplier(fixture.Difficulty)
				fixtureDifficulty = fixture.Difficulty
			}
		}

		ScorePlayer(&players[i], stats, s.teamModel, fixtureAdj, fixtureDifficulty)

		nineties := float64(stats.Minutes) / 90.0
		defconScore := 0.0
		if players[i].Position == 2 {
			cbitPer90 := float64(stats.CBIT) / nineties
			expectedMins := float64(players[i].Minutes) / float64(players[i].Games)
			if expectedMins > 90 {
				expectedMins = 90
			}
			minsFrac := expectedMins / 90.0
			if minsFrac > 1.0 {
				minsFrac = 1.0
			}
			defconScore = expectedDefconPoints(cbitPer90, minsFrac, 10)
		} else if players[i].Position == 3 {
			cbirtPer90 := float64(stats.CBIT+stats.Tackles+stats.Recoveries) / nineties
			expectedMins := float64(players[i].Minutes) / float64(players[i].Games)
			if expectedMins > 90 {
				expectedMins = 90
			}
			minsFrac := expectedMins / 90.0
			if minsFrac > 1.0 {
				minsFrac = 1.0
			}
			defconScore = expectedDefconPoints(cbirtPer90, minsFrac, 12)
		}

		fixtures, _ := s.repo.GetUpcomingFixtures(players[i].TeamID, currentGW, numGameweeks)
		if len(fixtures) > 0 {
			var fwdList []FixtureWithDifficulty
			for j, f := range fixtures {
				fwdList = append(fwdList, FixtureWithDifficulty{
					Gameweek:   f.Gameweek,
					IsHome:     f.IsHome,
					Difficulty: f.Difficulty,
					GamesAhead: j,
				})
			}
			expectedMins := 90.0
			if players[i].Minutes > 0 && players[i].Games > 0 {
				expectedMins = float64(players[i].Minutes) / float64(players[i].Games)
			}
			if expectedMins > 90 {
				expectedMins = 90
			}
			players[i].ExpectedPointsMultiGW = MultiGWExpectedPoints(
				players[i].Position,
				players[i].XGPer90,
				players[i].XAPer90,
				players[i].CSRate,
				players[i].PPG,
				players[i].Availability,
				expectedMins,
				fwdList,
				defconScore,
			)
		} else {
			players[i].ExpectedPointsMultiGW = players[i].ExpectedPoints
		}
	}

	// Phase 5: apply Bayesian model
	if s.bayesModel != nil {
		s.bayesModel.ApplyBayesianModel(players, s.repo)
	}

	// Group players by position
	byPos := make(map[int][]models.PlayerScore)
	for _, p := range players {
		byPos[p.Position] = append(byPos[p.Position], p)
	}

	var recs []models.Recommendation

	for _, posList := range byPos {
		if len(posList) < 2 {
			continue
		}

		sort.Slice(posList, func(i, j int) bool {
			return posList[i].ExpectedPointsMultiGW > posList[j].ExpectedPointsMultiGW
		})

		positionMedianVal := 0.0
		if len(posList) > 0 {
			vals := make([]float64, 0, len(posList))
			for _, p := range posList {
				if p.Value > 0 {
					vals = append(vals, p.Value)
				}
			}
			sort.Float64s(vals)
			if len(vals) > 0 {
				positionMedianVal = vals[len(vals)/2]
			}
		}

		var sellCandidates, buyCandidates []models.PlayerScore

		if hasTeam {
			for _, p := range posList {
				if myTeamIDs[p.PlayerID] {
					sellCandidates = append(sellCandidates, p)
				} else if p.Availability > 0 {
					buyCandidates = append(buyCandidates, p)
				}
			}
			sort.Slice(sellCandidates, func(i, j int) bool {
				return sellCandidates[i].ExpectedPointsMultiGW < sellCandidates[j].ExpectedPointsMultiGW
			})
			if len(sellCandidates) > 3 {
				sellCandidates = sellCandidates[:3]
			}
		} else {
			bottomN := max(1, len(posList)/5)
			sellCandidates = posList[len(posList)-bottomN:]
			for _, p := range posList {
				if p.Availability > 0 {
					buyCandidates = append(buyCandidates, p)
				}
			}
		}

		buyCandidates = filterBuyCandidates(buyCandidates, positionMedianVal, 10)

		for _, sell := range sellCandidates {
			sellPrice := sell.NowCost
			if sp, ok := sellingPrices[sell.PlayerID]; ok && sp > 0 {
				sellPrice = sp
			}
			if hasTeam && !canAffordAnyBuy(sellPrice, bank, buyCandidates) {
				continue
			}

			for _, buy := range buyCandidates {
				if sell.PlayerID == buy.PlayerID {
					continue
				}
				if sell.TeamID == buy.TeamID {
					continue
				}

				if hasTeam {
					cost := buy.NowCost - sellPrice
					if cost > bank {
						continue
					}
				}

				gain := buy.ExpectedPointsMultiGW - sell.ExpectedPointsMultiGW
				if gain <= 0 {
					continue
				}

				priceDiff := float64(buy.NowCost-sellPrice) / 10.0
				valueGain := buy.Value - sell.Value

				recScore := gain*2.0 + valueGain*3.0 - math.Abs(priceDiff)*0.3

				reason := fmt.Sprintf(
					"%s (%s, EP %.2f/mGW, sell £%.1fm) → %s (%s, EP %.2f/mGW, £%.1fm). "+
						"xG: %.2f→%.2f, xA: %.2f→%.2f, net cost: £%.1fm",
					sell.WebName, sell.TeamName, sell.ExpectedPointsMultiGW, float64(sellPrice)/10.0,
					buy.WebName, buy.TeamName, buy.ExpectedPointsMultiGW, float64(buy.NowCost)/10.0,
					sell.XGPer90, buy.XGPer90,
					sell.XAPer90, buy.XAPer90,
					priceDiff,
				)

				recs = append(recs, models.Recommendation{
					SellPlayerID:   sell.PlayerID,
					BuyPlayerID:    buy.PlayerID,
					SellPlayerName: sell.WebName,
					BuyPlayerName:  buy.WebName,
					ExpectedGain:   gain,
					PriceDiff:      priceDiff,
					Reason:         reason,
					Score:          recScore,
				})
			}
		}
	}

	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Score > recs[j].Score
	})
	if len(recs) > 5 {
		recs = recs[:5]
	}

	if len(recs) > 0 {
		if err := s.repo.StoreRecommendations(s.userID, recs); err != nil {
			log.Printf("Warning: failed to store recommendations: %v", err)
		}
		s.repo.SetMetadata("last_analyze", time.Now().Format(time.RFC3339))
	}

	stored, err := s.repo.GetPendingRecommendations(5)
	if err != nil {
		return recs, nil
	}

	return stored, nil
}

// PositionReport holds the top players for one position
type PositionReport struct {
	Position int
	Players  []models.PlayerReport
}

// Report generates top-5-per-position reports for the given game window.
// Now includes ExpectedPoints in the report.
func (s *Service) Report(window int) ([]PositionReport, error) {
	players, err := s.repo.GetActivePlayersWithExpected(90)
	if err != nil {
		return nil, fmt.Errorf("failed to load players: %w", err)
	}

	var reports []models.PlayerReport
	for _, p := range players {
		var stats *database.PlayerHistoryStats
		var err error
		if window > 0 {
			stats, err = s.repo.GetPlayerRecentStats(p.PlayerID, window)
		} else {
			stats, err = s.repo.GetPlayerSeasonStats(p.PlayerID)
		}
		if err != nil || stats == nil || stats.Games == 0 {
			continue
		}
		r := ScorePlayerForReport(p, stats)
		reports = append(reports, r)
	}

	byPos := make(map[int][]models.PlayerReport)
	for _, r := range reports {
		byPos[r.Position] = append(byPos[r.Position], r)
	}

	var result []PositionReport
	for pos := 1; pos <= 4; pos++ {
		list := byPos[pos]
		sort.Slice(list, func(i, j int) bool {
			if list[i].ExpectedPoints != list[j].ExpectedPoints {
				return list[i].ExpectedPoints > list[j].ExpectedPoints
			}
			return list[i].XScore > list[j].XScore
		})
		if len(list) > 5 {
			list = list[:5]
		}
		result = append(result, PositionReport{Position: pos, Players: list})
	}

	return result, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func filterBuyCandidates(candidates []models.PlayerScore, medianVal float64, cap int) []models.PlayerScore {
	if len(candidates) == 0 {
		return nil
	}
	takeN := 15
	if takeN > len(candidates) {
		takeN = len(candidates)
	}
	topEP := candidates[:takeN]

	if medianVal <= 0 {
		if len(topEP) > cap {
			return topEP[:cap]
		}
		return topEP
	}

	var filtered []models.PlayerScore
	for _, p := range topEP {
		if p.Value >= medianVal {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) < 3 {
		filtered = topEP
	}
	if len(filtered) > cap {
		filtered = filtered[:cap]
	}
	return filtered
}

func canAffordAnyBuy(sellPrice int, bank int, buyCandidates []models.PlayerScore) bool {
	for _, buy := range buyCandidates {
		if buy.NowCost-sellPrice <= bank {
			return true
		}
	}
	return false
}

func (s *Service) scoreAllPlayers(players []models.PlayerScore, currentGW int) []models.PlayerScore {
	isColdStart, _ := s.repo.IsColdStart(3)
	hasSeasonData, _ := s.repo.HasSeasonData()

	regPriorMins := 1500.0

	// Pre-load stats for all players
	type scoredPlayer struct {
		index      int
		stats      *database.PlayerHistoryStats
		fixtureAdj float64
	}
	var scoredList []scoredPlayer

	for i := range players {
		var stats *database.PlayerHistoryStats
		var err error

		if isColdStart && hasSeasonData {
			stats, err = s.repo.GetPlayerMultiSeasonBlendedStats(players[i].PlayerID, 5)
		} else {
			stats, err = s.repo.GetPlayerWeightedRecentStats(players[i].PlayerID, 5)
		}
		if err != nil || stats == nil || stats.Games == 0 {
			continue
		}

		fixtureAdj := 1.0
		fixtureDifficulty := 3
		if s.teamModel != nil {
			fixture, err := s.repo.GetNextFixture(players[i].TeamID, currentGW)
			if err == nil && fixture != nil {
				fixtureAdj = FixtureDifficultyMultiplier(fixture.Difficulty)
				fixtureDifficulty = fixture.Difficulty
			}
		}

		ScorePlayer(&players[i], stats, s.teamModel, fixtureAdj, fixtureDifficulty)
		scoredList = append(scoredList, scoredPlayer{index: i, stats: stats, fixtureAdj: fixtureAdj})
	}

	// Compute position averages for regression
	type posAvg struct {
		xg90Sum, xa90Sum, csSum, ppgSum float64
		totalMins                       int
		count                           int
	}
	posAvgs := map[int]*posAvg{}
	for _, sp := range scoredList {
		p := &players[sp.index]
		st := sp.stats
		pos := p.Position
		if posAvgs[pos] == nil {
			posAvgs[pos] = &posAvg{}
		}
		pa := posAvgs[pos]
		pa.xg90Sum += p.XGPer90
		pa.xa90Sum += p.XAPer90
		pa.csSum += p.CSRate
		pa.ppgSum += p.PPG
		pa.totalMins += st.Minutes
		pa.count++
	}

	// Apply minutes-based regression
	for _, sp := range scoredList {
		p := &players[sp.index]
		st := sp.stats
		pa := posAvgs[p.Position]
		if pa == nil || pa.count == 0 {
			continue
		}

		avgXG90 := pa.xg90Sum / float64(pa.count)
		avgXA90 := pa.xa90Sum / float64(pa.count)
		avgCSRate := pa.csSum / float64(pa.count)
		avgPPG := pa.ppgSum / float64(pa.count)

		weight := float64(st.Minutes) / (float64(st.Minutes) + regPriorMins)

		p.XGPer90 = avgXG90 + (p.XGPer90-avgXG90)*weight
		p.XAPer90 = avgXA90 + (p.XAPer90-avgXA90)*weight
		p.CSRate = avgCSRate + (p.CSRate-avgCSRate)*weight
		p.PPG = avgPPG + (p.PPG-avgPPG)*weight

		// Recompute ExpectedPoints with regressed stats
		expectedMins := 90.0
		if p.Minutes > 0 && p.Games > 0 {
			expectedMins = float64(p.Minutes) / float64(p.Games)
		}
		if expectedMins > 90 {
			expectedMins = 90
		}
		p.ExpectedPoints = SimpleExpectedPoints(
			p.Position, p.XGPer90, p.XAPer90, p.XGCPer90,
			p.CSRate, p.PPG, p.Availability, expectedMins, sp.fixtureAdj, 0,
		)

		fixtures, _ := s.repo.GetUpcomingFixtures(p.TeamID, currentGW, 3)
		if len(fixtures) > 0 {
			var fwdList []FixtureWithDifficulty
			for j, f := range fixtures {
				fwdList = append(fwdList, FixtureWithDifficulty{
					Gameweek:   f.Gameweek,
					IsHome:     f.IsHome,
					Difficulty: f.Difficulty,
					GamesAhead: j,
				})
			}
			p.ExpectedPointsMultiGW = MultiGWExpectedPoints(
				p.Position, p.XGPer90, p.XAPer90, p.CSRate, p.PPG,
				p.Availability, expectedMins, fwdList, 0,
			)
		} else {
			discountTotal := 1.0 + math.Pow(14.0/15.0, 1) + math.Pow(14.0/15.0, 2)
			p.ExpectedPointsMultiGW = p.ExpectedPoints * discountTotal
		}
	}

	// Blend actual total points from last season as a proven-delivery signal.
	// Players with high total points (nailed starters who delivered) get a
	// modest boost over those with only promising per-90 stats.
	maxTP := map[int]int{}
	for _, sp := range scoredList {
		p := &players[sp.index]
		if p.TotalPoints > maxTP[p.Position] {
			maxTP[p.Position] = p.TotalPoints
		}
	}
	for _, sp := range scoredList {
		p := &players[sp.index]
		if maxTP[p.Position] > 0 {
			normTP := float64(p.TotalPoints) / float64(maxTP[p.Position])
			p.ExpectedPointsMultiGW *= (0.6 + 0.4*normTP)
		}
	}

	return players
}

func (s *Service) SuggestSeasonStartSquad() (*OptimizedSquad, error) {
	players, err := s.repo.GetActivePlayersWithExpected(0)
	if err != nil {
		return nil, fmt.Errorf("failed to load players: %w", err)
	}

	currentGW, _ := s.repo.GetCurrentGameweek()
	if currentGW == 0 {
		currentGW = 1
	}

	log.Printf("Scoring %d players for season-start squad...", len(players))
	scored := s.scoreAllPlayers(players, currentGW)

	constraints := SeasonStartSquadConstraints()

	starterQuality := FilterPlayersByMinutes(scored, constraints.MinStarterMinutes)

	scoredWithEPN := 0
	for _, p := range starterQuality {
		if p.ExpectedPointsMultiGW > 0 || p.ExpectedPoints > 0 {
			scoredWithEPN++
		}
	}
	log.Printf("Scored: %d total, %d starter-quality (>=%d mins), %d with EP>0",
		len(scored), len(starterQuality), constraints.MinStarterMinutes, scoredWithEPN)

	if scoredWithEPN < 20 {
		return nil, fmt.Errorf(
			"only %d players have expected points data (need >=20). "+
				"Run 'docker compose exec fpl-core /app/fpl-core -once -seed-prior' to populate historical season data, "+
				"then try again", scoredWithEPN)
	}

	squad := OptimizeSeasonStartSquad(starterQuality, scored, constraints)
	if squad == nil {
		return nil, fmt.Errorf("could not build a valid squad with the given constraints")
	}

	log.Printf("Best squad: %s, Starter EP: %.2f, Team EP: %.2f, Cost: £%.1fM, Bank: £%.1fM",
		squad.Formation(), squad.Starting11EP(), squad.TeamEP,
		float64(squad.TotalCost)/10.0, float64(squad.Bank)/10.0)

	return squad, nil
}

// BuildInitialSquad builds an initial 15-player squad for season start or mid-season reset.
// Strategy differs by context:
//   - New season (cold start): scores based on last 20 games across seasons,
//     with prior seasons phased out over the first 20 current gameweeks.
//   - Mid-season: scores based on last 2 complete seasons as stable baseline
//     blended 50/50 with current-season weighted stats.
func (s *Service) BuildInitialSquad() (*OptimizedSquad, error) {
	players, err := s.repo.GetActivePlayersWithExpected(0)
	if err != nil {
		return nil, fmt.Errorf("failed to load players: %w", err)
	}

	currentGW, _ := s.repo.GetCurrentGameweek()
	if currentGW == 0 {
		currentGW = 1
	}

	isColdStart, _ := s.repo.IsColdStart(3)
	hasSeasonData, _ := s.repo.HasSeasonData()
	isNewSeason := isColdStart && hasSeasonData

	log.Printf("BuildInitialSquad: newSeason=%v, %d players, currentGW=%d", isNewSeason, len(players), currentGW)
	scored, scoredCount := s.scoreForInitSquad(players, currentGW, isNewSeason)

	constraints := DefaultSquadConstraints()

	starterQuality := FilterPlayersByMinutes(scored, 1500)

	scoredWithEPN := 0
	for _, p := range starterQuality {
		if p.ExpectedPointsMultiGW > 0 || p.ExpectedPoints > 0 {
			scoredWithEPN++
		}
	}
	log.Printf("Scored: %d total, %d starter-quality (>=1500 mins), %d with EP>0 (scoredCount=%d)",
		len(scored), len(starterQuality), scoredWithEPN, scoredCount)

	if scoredWithEPN < 20 {
		return nil, fmt.Errorf(
			"only %d players have expected points data (need >=20). "+
				"Run 'docker compose exec fpl-core /app/fpl-core -once -seed-prior' to populate historical season data, "+
				"then try again", scoredWithEPN)
	}

	squad := OptimizeSeasonStartSquad(starterQuality, scored, constraints)
	if squad == nil {
		return nil, fmt.Errorf("could not build a valid squad with the given constraints")
	}

	log.Printf("Best squad: %s, Starter EP: %.2f, Team EP: %.2f, Cost: £%.1fM, Bank: £%.1fM",
		squad.Formation(), squad.Starting11EP(), squad.TeamEP,
		float64(squad.TotalCost)/10.0, float64(squad.Bank)/10.0)

	return squad, nil
}

func (s *Service) scoreForInitSquad(players []models.PlayerScore, currentGW int, isNewSeason bool) ([]models.PlayerScore, int) {
	regPriorMins := 1500.0

	type scoredPlayer struct {
		index      int
		stats      *database.PlayerHistoryStats
		fixtureAdj float64
	}
	var scoredList []scoredPlayer

	for i := range players {
		stats, err := s.repo.GetPlayerInitSquadStats(players[i].PlayerID, isNewSeason)
		if err != nil || stats == nil || stats.Games == 0 {
			continue
		}

		fixtureAdj := 1.0
		fixtureDifficulty := 3
		if s.teamModel != nil {
			fixture, err := s.repo.GetNextFixture(players[i].TeamID, currentGW)
			if err == nil && fixture != nil {
				fixtureAdj = FixtureDifficultyMultiplier(fixture.Difficulty)
				fixtureDifficulty = fixture.Difficulty
			}
		}

		ScorePlayer(&players[i], stats, s.teamModel, fixtureAdj, fixtureDifficulty)
		scoredList = append(scoredList, scoredPlayer{index: i, stats: stats, fixtureAdj: fixtureAdj})
	}

	type posAvg struct {
		xg90Sum, xa90Sum, csSum, ppgSum float64
		totalMins                       int
		count                           int
	}
	posAvgs := map[int]*posAvg{}
	for _, sp := range scoredList {
		p := &players[sp.index]
		st := sp.stats
		pos := p.Position
		if posAvgs[pos] == nil {
			posAvgs[pos] = &posAvg{}
		}
		pa := posAvgs[pos]
		pa.xg90Sum += p.XGPer90
		pa.xa90Sum += p.XAPer90
		pa.csSum += p.CSRate
		pa.ppgSum += p.PPG
		pa.totalMins += st.Minutes
		pa.count++
	}

	for _, sp := range scoredList {
		p := &players[sp.index]
		st := sp.stats
		pa := posAvgs[p.Position]
		if pa == nil || pa.count == 0 {
			continue
		}

		avgXG90 := pa.xg90Sum / float64(pa.count)
		avgXA90 := pa.xa90Sum / float64(pa.count)
		avgCSRate := pa.csSum / float64(pa.count)
		avgPPG := pa.ppgSum / float64(pa.count)

		weight := float64(st.Minutes) / (float64(st.Minutes) + regPriorMins)

		p.XGPer90 = avgXG90 + (p.XGPer90-avgXG90)*weight
		p.XAPer90 = avgXA90 + (p.XAPer90-avgXA90)*weight
		p.CSRate = avgCSRate + (p.CSRate-avgCSRate)*weight
		p.PPG = avgPPG + (p.PPG-avgPPG)*weight

		expectedMins := 90.0
		if p.Minutes > 0 && p.Games > 0 {
			expectedMins = float64(p.Minutes) / float64(p.Games)
		}
		if expectedMins > 90 {
			expectedMins = 90
		}
		p.ExpectedPoints = SimpleExpectedPoints(
			p.Position, p.XGPer90, p.XAPer90, p.XGCPer90,
			p.CSRate, p.PPG, p.Availability, expectedMins, sp.fixtureAdj, 0,
		)

		fixtures, _ := s.repo.GetUpcomingFixtures(p.TeamID, currentGW, 3)
		if len(fixtures) > 0 {
			var fwdList []FixtureWithDifficulty
			for j, f := range fixtures {
				fwdList = append(fwdList, FixtureWithDifficulty{
					Gameweek:   f.Gameweek,
					IsHome:     f.IsHome,
					Difficulty: f.Difficulty,
					GamesAhead: j,
				})
			}
			p.ExpectedPointsMultiGW = MultiGWExpectedPoints(
				p.Position, p.XGPer90, p.XAPer90, p.CSRate, p.PPG,
				p.Availability, expectedMins, fwdList, 0,
			)
		} else {
			discountTotal := 1.0 + math.Pow(14.0/15.0, 1) + math.Pow(14.0/15.0, 2)
			p.ExpectedPointsMultiGW = p.ExpectedPoints * discountTotal
		}
	}

	maxTP := map[int]int{}
	for _, sp := range scoredList {
		p := &players[sp.index]
		if p.TotalPoints > maxTP[p.Position] {
			maxTP[p.Position] = p.TotalPoints
		}
	}
	for _, sp := range scoredList {
		p := &players[sp.index]
		if maxTP[p.Position] > 0 {
			normTP := float64(p.TotalPoints) / float64(maxTP[p.Position])
			p.ExpectedPointsMultiGW *= (0.6 + 0.4*normTP)
		}
	}

	log.Printf("InitSquad: %d/%d players scored with stats (isNewSeason=%v)", len(scoredList), len(players), isNewSeason)
	return players, len(scoredList)
}
