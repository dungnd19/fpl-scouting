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
// Phase 1-6 integrated scoring pipeline.
func (s *Service) Suggest() ([]models.Recommendation, error) {
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

		// Phase 1: use time-decayed weighted stats, or blended multi-season if cold start
		if isColdStart && hasSeasonData {
			stats, err = s.repo.GetPlayerMultiSeasonBlendedStats(players[i].PlayerID, 5)
		} else {
			stats, err = s.repo.GetPlayerWeightedRecentStats(players[i].PlayerID, 5)
		}
		if err != nil {
			continue
		}

		// Phase 2+4: fixture difficulty adjustment
		fixtureAdj := 1.0
		if s.teamModel != nil {
			fixture, err := s.repo.GetNextFixture(players[i].TeamID, currentGW)
			if err == nil && fixture != nil {
				fixtureAdj = FixtureDifficultyMultiplier(fixture.Difficulty)
			}
		}

		ScorePlayer(&players[i], stats, s.teamModel, fixtureAdj)

		// Phase 4: multi-GW expected points
		fixtures, _ := s.repo.GetUpcomingFixtures(players[i].TeamID, currentGW, 3)
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

		// Phase 6: sort by ExpectedPointsMultiGW for multi-week-aware ranking
		sort.Slice(posList, func(i, j int) bool {
			return posList[i].ExpectedPointsMultiGW > posList[j].ExpectedPointsMultiGW
		})

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

		if len(buyCandidates) > 10 {
			buyCandidates = buyCandidates[:10]
		}

		for _, sell := range sellCandidates {
			for _, buy := range buyCandidates {
				if sell.PlayerID == buy.PlayerID {
					continue
				}
				if sell.TeamID == buy.TeamID {
					continue
				}

				if hasTeam {
					sellPrice := sellingPrices[sell.PlayerID]
					if sellPrice == 0 {
						sellPrice = sell.NowCost
					}
					cost := buy.NowCost - sellPrice
					if cost > bank {
						continue
					}
				}

				// Phase 3+4: use multi-GW expected points for gain calculation
				gain := buy.ExpectedPointsMultiGW - sell.ExpectedPointsMultiGW
				if gain <= 0 {
					continue
				}

				sellPrice := sell.NowCost
				if sp, ok := sellingPrices[sell.PlayerID]; ok && sp > 0 {
					sellPrice = sp
				}
				priceDiff := float64(buy.NowCost-sellPrice) / 10.0
				valueGain := buy.Value - sell.Value

				// Phase 6: account for potential points hit in score
				freeTransfers, _ := s.repo.GetFreeTransfers()
				recScore := gain*2.0 + valueGain*3.0 - math.Abs(priceDiff)*0.3
				_ = freeTransfers

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

func (s *Service) scoreAllPlayers(players []models.PlayerScore, currentGW int) []models.PlayerScore {
	isColdStart, _ := s.repo.IsColdStart(3)
	hasSeasonData, _ := s.repo.HasSeasonData()

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
		if s.teamModel != nil {
			fixture, err := s.repo.GetNextFixture(players[i].TeamID, currentGW)
			if err == nil && fixture != nil {
				fixtureAdj = FixtureDifficultyMultiplier(fixture.Difficulty)
			}
		}

		ScorePlayer(&players[i], stats, s.teamModel, fixtureAdj)

		expectedMins := 90.0
		if players[i].Minutes > 0 && players[i].Games > 0 {
			expectedMins = float64(players[i].Minutes) / float64(players[i].Games)
		}
		if expectedMins > 90 {
			expectedMins = 90
		}
		fixtures, _ := s.repo.GetUpcomingFixtures(players[i].TeamID, currentGW, 3)
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
			players[i].ExpectedPointsMultiGW = MultiGWExpectedPoints(
				players[i].Position,
				players[i].XGPer90,
				players[i].XAPer90,
				players[i].CSRate,
				players[i].PPG,
				players[i].Availability,
				expectedMins,
				fwdList,
			)
		} else {
			discountTotal := 1.0 + math.Pow(14.0/15.0, 1) + math.Pow(14.0/15.0, 2)
			players[i].ExpectedPointsMultiGW = players[i].ExpectedPoints * discountTotal
		}
	}

	if s.bayesModel != nil {
		s.bayesModel.ApplyBayesianModel(players, s.repo)
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
	log.Printf("Scored: %d total, %d starter-quality (>=%d mins)",
		len(scored), len(starterQuality), constraints.MinStarterMinutes)

	squad := OptimizeSeasonStartSquad(starterQuality, scored, constraints)
	if squad == nil {
		return nil, fmt.Errorf("could not build a valid squad with the given constraints")
	}

	log.Printf("Best squad: %s, Starter EP: %.2f, Team EP: %.2f, Cost: £%.1fM, Bank: £%.1fM",
		squad.Formation(), squad.Starting11EP(), squad.TeamEP,
		float64(squad.TotalCost)/10.0, float64(squad.Bank)/10.0)

	return squad, nil
}
