package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"

	"fpl-bot/internal/analyzer"
	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
)

func main() {
	dbPath := flag.String("db", "/tmp/fpl_check.db", "Path to fpl.db")
	userID := flag.String("user", "default", "User ID")
	mode := flag.String("mode", "suggest", "Mode: suggest, startsquad, initsquad")
	minStarterMins := flag.Int("min-starter-mins", 1500, "Min minutes for starting 11")
	minBenchMins := flag.Int("min-bench-mins", 700, "Min minutes for bench")
	flag.Parse()

	db, err := database.New(database.Config{Path: *dbPath})
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)

	// Check data availability
	coldStart, _ := repo.IsColdStart(3)
	hasSeason, _ := repo.HasSeasonData()
	log.Printf("Cold start: %v, has season data: %v", coldStart, hasSeason)

	svc := analyzer.NewService(repo, *userID)

	if *mode == "startsquad" {
		printSeasonStartSquad(svc, *minStarterMins, *minBenchMins)
		return
	}

	if *mode == "initsquad" {
		printInitSquad(svc)
		return
	}

	runSuggest(svc, repo)
}

func runSuggest(svc *analyzer.Service, repo *database.Repository) {
	recs, err := svc.Suggest()
	if err != nil {
		log.Fatalf("Analysis failed: %v", err)
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     FPL Scouting — Transfer Suggestions                     ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")

	if len(recs) == 0 {
		fmt.Println("║  No recommendations found. Check data availability.         ║")
		fmt.Println("╚══════════════════════════════════════════════════════════════╝")
		return
	}

	for i, rec := range recs {
		fmt.Printf("║ #%d — Score: %.2f                                      ║\n", i+1, rec.Score)
		fmt.Printf("║   Sell: %s → Buy: %s                     ║\n",
			padRight(rec.SellPlayerName, 18), padRight(rec.BuyPlayerName, 18))
		fmt.Printf("║   Gain: %.2f pts/gw | Price diff: £%.1fm                       ║\n",
			rec.ExpectedGain, rec.PriceDiff)
		fmt.Printf("║   %s║\n", wrapText(rec.Reason, 60))
		fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	}

	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	printTopPlayers(repo, svc)
}

func printSeasonStartSquad(svc *analyzer.Service, minStarterMins, minBenchMins int) {
	squad, err := svc.SuggestSeasonStartSquad()
	if err != nil {
		log.Fatalf("Season start squad failed: %v", err)
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     FPL Scouting — Season Start Squad                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Formation: %-12s Starter EP: %8.2f                    ║\n",
		squad.Formation(), squad.Starting11EP())
	fmt.Printf("║  Team EP: %10.2f  Cost: £%6.1fM  Bank: £%6.1fM          ║\n",
		squad.TeamEPValue(), float64(squad.TotalCost)/10.0, float64(squad.Bank)/10.0)
	fmt.Printf("║  Min starter mins: %d  Min bench mins: %d                 ║\n",
		minStarterMins, minBenchMins)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	printSquad("GOALKEEPERS", squad.Goalkeepers)
	printSquad("DEFENDERS", squad.Defenders)
	printSquad("MIDFIELDERS", squad.Midfielders)
	printSquad("FORWARDS", squad.Forwards)
}

func printInitSquad(svc *analyzer.Service) {
	squad, err := svc.BuildInitialSquad()
	if err != nil {
		log.Fatalf("Init squad failed: %v", err)
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     FPL Scouting — Initial Squad (Build)                    ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Formation: %-12s Starter EP: %8.2f                    ║\n",
		squad.Formation(), squad.Starting11EP())
	fmt.Printf("║  Team EP: %10.2f  Cost: £%6.1fM  Bank: £%6.1fM          ║\n",
		squad.TeamEPValue(), float64(squad.TotalCost)/10.0, float64(squad.Bank)/10.0)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	printSquad("GOALKEEPERS", squad.Goalkeepers)
	printSquad("DEFENDERS", squad.Defenders)
	printSquad("MIDFIELDERS", squad.Midfielders)
	printSquad("FORWARDS", squad.Forwards)
}

func printSquad(label string, slots []analyzer.SquadSlot) {
	starters := make([]analyzer.SquadSlot, 0)
	bench := make([]analyzer.SquadSlot, 0)
	for _, s := range slots {
		if s.IsStarter {
			starters = append(starters, s)
		} else {
			bench = append(bench, s)
		}
	}

	fmt.Printf("\n── %s ─────────────────────────────────────────────\n", label)
	if len(starters) > 0 {
		fmt.Println("  STARTERS:")
		for i, s := range starters {
			p := s.Player
			fmt.Printf("  %d. %-18s (%s)  £%5.1fM  EP3GW: %6.2f  Mins: %5d  xG90: %.3f  xA90: %.3f\n",
				i+1, p.WebName, padRight(p.TeamName, 4), float64(p.NowCost)/10.0,
				p.ExpectedPointsMultiGW, p.Minutes, p.XGPer90, p.XAPer90)
		}
	}
	if len(bench) > 0 {
		fmt.Println("  BENCH:")
		for i, s := range bench {
			p := s.Player
			fmt.Printf("  %d. %-18s (%s)  £%5.1fM  EP3GW: %6.2f  Mins: %5d\n",
				i+1, p.WebName, padRight(p.TeamName, 4), float64(p.NowCost)/10.0,
				p.ExpectedPointsMultiGW, p.Minutes)
		}
	}
}

func printTopPlayers(repo *database.Repository, svc *analyzer.Service) {
	fmt.Println("\n══════════ Top Players by Position (Expected Points) ══════════")
	for pos := 1; pos <= 4; pos++ {
		posName := models.PositionNames[pos]
		fmt.Printf("\n── %s ─────────────────────────────────────────────\n", posName)
		topPlayers := getTopPlayersForPosition(repo, svc, pos)
		for i, p := range topPlayers {
			if i >= 5 {
				break
			}
			costMil := float64(p.NowCost) / 10.0
			fmt.Printf("  %d. %s (%s) — EP: %.2f | £%.1fm | xG90: %.2f xA90: %.2f\n",
				i+1, padRight(p.WebName, 18), padRight(p.TeamName, 12),
				p.ExpectedPoints, costMil, p.XGPer90, p.XAPer90)
		}
	}
	fmt.Println()
}

func getTopPlayersForPosition(repo *database.Repository, svc *analyzer.Service, position int) []models.PlayerScore {
	players, _ := repo.GetActivePlayersWithExpected(0)
	currentGW, _ := repo.GetCurrentGameweek()
	if currentGW == 0 {
		currentGW = 1
	}

	var filtered []models.PlayerScore
	for _, p := range players {
		if p.Position != position {
			continue
		}
		if p.Availability <= 0 {
			continue
		}

		stats, _ := repo.GetPlayerWeightedRecentStats(p.PlayerID, 5)
		if stats == nil || stats.Games == 0 {
			stats, _ = repo.GetPlayerMultiSeasonBlendedStats(p.PlayerID, 5)
		}
		if stats == nil || stats.Games == 0 {
			continue
		}

		analyzer.ScorePlayer(&p, stats, nil, 1.0, 3)

		fixtures, _ := repo.GetUpcomingFixtures(p.TeamID, currentGW, 3)
		var fwdList []analyzer.FixtureWithDifficulty
		for j, f := range fixtures {
			fwdList = append(fwdList, analyzer.FixtureWithDifficulty{
				Gameweek: f.Gameweek, IsHome: f.IsHome,
				Difficulty: f.Difficulty, GamesAhead: j,
			})
		}
		expectedMins := 90.0
		if p.Minutes > 0 && p.Games > 0 {
			expectedMins = float64(p.Minutes) / float64(p.Games)
		}
		if expectedMins > 90 {
			expectedMins = 90
		}
		p.ExpectedPoints = analyzer.SimpleExpectedPoints(
			p.Position, p.XGPer90, p.XAPer90, p.XGCPer90, p.CSRate, p.PPG,
			p.Availability, expectedMins, 1.0, 0,
		)
		if len(fwdList) > 0 {
			p.ExpectedPointsMultiGW = analyzer.MultiGWExpectedPoints(
				p.Position, p.XGPer90, p.XAPer90, p.CSRate, p.PPG,
				p.Availability, expectedMins, fwdList, 0,
			)
		}

		filtered = append(filtered, p)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].ExpectedPointsMultiGW != filtered[j].ExpectedPointsMultiGW {
			return filtered[i].ExpectedPointsMultiGW > filtered[j].ExpectedPointsMultiGW
		}
		return filtered[i].ExpectedPoints > filtered[j].ExpectedPoints
	})

	return filtered
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func wrapText(s string, width int) string {
	if len(s) <= width {
		return s
	}
	return s[:width-3] + "..."
}
