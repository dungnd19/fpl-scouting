package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"

	"fpl-bot/internal/analyzer"
	"fpl-bot/internal/checker"
	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
	"fpl-bot/internal/telegram"
	"fpl-bot/internal/trader"
)

type Config struct {
	DBPath               string
	TelegramBotToken     string
	CheckIntervalMinutes int
	UserID               string
	FPLSessionCookie     string
	FPLTeamID            string
	BotDebug             bool
}

var suggestCLI bool
var benchReserve int

func main() {
	flag.BoolVar(&suggestCLI, "suggest-cli", false, "Run suggest analysis and print to stdout")
	flag.IntVar(&benchReserve, "bench-reserve", 120, "Bench budget reserve in 0.1m (120 = £12.0m)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg := loadConfig()

	db, err := database.New(database.Config{Path: cfg.DBPath})
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)

	if suggestCLI {
		runSuggestCLI(repo, cfg.UserID)
		return
	}

	log.Println("FPL Telegram Bot starting...")
	log.Println("Database connected successfully")

	traderSvc := trader.NewService(repo, cfg.FPLSessionCookie, cfg.FPLTeamID, cfg.UserID)
	analyzerSvc := analyzer.NewService(repo, cfg.UserID)

	bot, err := telegram.NewBot(cfg.TelegramBotToken, repo, traderSvc, analyzerSvc, cfg.UserID, cfg.BotDebug)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	checkerSvc := checker.NewService(repo, bot, cfg.CheckIntervalMinutes)
	go checkerSvc.Start()

	if err := bot.Start(); err != nil {
		log.Fatalf("Bot error: %v", err)
	}
}

func runSuggestCLI(repo *database.Repository, userID string) {
	coldStart, _ := repo.IsColdStart(3)
	hasSeason, _ := repo.HasSeasonData()

	fmt.Println("══ FPL Scouting — Starting Team Analysis (2026/27) ══")
	fmt.Printf("Cold start: %v | Prior season data: %v\n", coldStart, hasSeason)

	// Score all players
	scored := scoreAllPlayers(repo)

	// Build optimized squad via bench sweep
	squad := analyzer.OptimizeSquadBest(scored, analyzer.DefaultSquadConstraints())
	if squad == nil {
		fmt.Println("\nNo valid squad found — insufficient qualified players.")
		showValuePicks(scored)
		return
	}

	fmt.Println("\n══════════ RECOMMENDED STARTING SQUAD ══════════")
	fmt.Printf("Bench reserve: £%.1fm | Formation: %s | Budget: £%.1fm / £100.0m\n",
		float64(squad.BenchReserve)/10.0, squad.Formation(), float64(squad.TotalCost)/10.0)
	fmt.Println()

	printSquadSection("GOALKEEPERS", squad.Goalkeepers)
	printSquadSection("DEFENDERS", squad.Defenders)
	printSquadSection("MIDFIELDERS", squad.Midfielders)
	printSquadSection("FORWARDS", squad.Forwards)

	fmt.Printf("\n── Starting XI EP: %.2f pts/GW | Bank: £%.1fm ──\n",
		squad.Starting11EP(), float64(squad.Bank)/10.0)

	fmt.Println("\n═══ TOP VALUE PICKS (per position) ═══")
	showValuePicks(scored)
}

func scoreAllPlayers(repo *database.Repository) []models.PlayerScore {
	players, _ := repo.GetActivePlayersWithExpected(0)
	currentGW, _ := repo.GetCurrentGameweek()
	if currentGW == 0 {
		currentGW = 1
	}
	isCold, _ := repo.IsColdStart(3)
	hasSeason, _ := repo.HasSeasonData()

	var scored []models.PlayerScore
	for _, p := range players {
		if p.Availability <= 0 {
			continue
		}

		var stats *database.PlayerHistoryStats
		if isCold && hasSeason {
			stats, _ = repo.GetPlayerMultiSeasonBlendedStats(p.PlayerID, 10)
		} else {
			stats, _ = repo.GetPlayerWeightedRecentStats(p.PlayerID, 5)
		}
		if stats == nil || stats.Games == 0 {
			continue
		}

		analyzer.ScorePlayer(&p, stats, nil, 1.0)

		fixtures, _ := repo.GetUpcomingFixtures(p.TeamID, currentGW, 3)
		var fwdList []analyzer.FixtureWithDifficulty
		for j, f := range fixtures {
			fwdList = append(fwdList, analyzer.FixtureWithDifficulty{
				Gameweek: f.Gameweek, IsHome: f.IsHome,
				Difficulty: f.Difficulty, GamesAhead: j,
			})
		}
		expectedMins := 90.0
		if p.Minutes > 0 && stats.Games > 0 {
			expectedMins = float64(stats.Minutes) / float64(stats.Games)
		}
		if expectedMins > 90 {
			expectedMins = 90
		}
		p.ExpectedPoints = analyzer.SimpleExpectedPoints(
			p.Position, p.XGPer90, p.XAPer90, p.XGCPer90, p.CSRate, p.PPG,
			p.Availability, expectedMins, 1.0,
		)
		if len(fwdList) > 0 {
			p.ExpectedPointsMultiGW = analyzer.MultiGWExpectedPoints(
				p.Position, p.XGPer90, p.XAPer90, p.CSRate, p.PPG,
				p.Availability, expectedMins, fwdList,
			)
		} else {
			p.ExpectedPointsMultiGW = p.ExpectedPoints
		}
		scored = append(scored, p)
	}
	return scored
}

func printSquadSection(title string, slots []analyzer.SquadSlot) {
	fmt.Printf("── %s ──────────────────────────────────\n", title)
	for i, s := range slots {
		marker := "  "
		if !s.IsStarter {
			marker = "B "
		}
		costM := float64(s.Player.NowCost) / 10.0
		epDisplay := s.Player.ExpectedPointsMultiGW
		if epDisplay <= 0 {
			epDisplay = s.Player.ExpectedPoints
		}
		fmt.Printf("  %s%d. %-20s %-6s EP: %5.2f | £%4.1fm | xG90: %.2f xA90: %.2f\n",
			marker, i+1, s.Player.WebName, s.Player.TeamName,
			epDisplay, costM, s.Player.XGPer90, s.Player.XAPer90)
	}
	fmt.Println()
}

func showValuePicks(scored []models.PlayerScore) {
	for pos := 1; pos <= 4; pos++ {
		posName := models.PositionNames[pos]
		var posPlayers []models.PlayerScore
		for _, p := range scored {
			if p.Position == pos {
				posPlayers = append(posPlayers, p)
			}
		}
		sort.Slice(posPlayers, func(i, j int) bool {
			a := posPlayers[i]
			b := posPlayers[j]
			valA := a.ExpectedPointsMultiGW / (float64(a.NowCost) / 10.0)
			valB := b.ExpectedPointsMultiGW / (float64(b.NowCost) / 10.0)
			if valA == valB {
				return a.ExpectedPointsMultiGW > b.ExpectedPointsMultiGW
			}
			return valA > valB
		})
		fmt.Printf("\n  %s (max 5):\n", posName)
		for i, p := range posPlayers {
			if i >= 5 {
				break
			}
			costM := float64(p.NowCost) / 10.0
			ep := p.ExpectedPointsMultiGW
			if ep <= 0 {
				ep = p.ExpectedPoints
			}
			value := ep / costM
			fmt.Printf("    %d. %-20s %-6s EP: %.2f | £%.1fm | V: %.2f\n",
				i+1, p.WebName, p.TeamName, ep, costM, value)
		}
	}
}

func loadConfig() Config {
	return Config{
		DBPath:               getEnv("DB_PATH", "/data/fpl.db"),
		TelegramBotToken:     mustGetEnv("TELEGRAM_BOT_TOKEN"),
		CheckIntervalMinutes: getEnvInt("CHECK_INTERVAL_MINUTES", 60),
		UserID:               getEnv("DEFAULT_USER_ID", "default"),
		FPLSessionCookie:     getEnv("FPL_SESSION_COOKIE", ""),
		FPLTeamID:            getEnv("FPL_TEAM_ID", ""),
		BotDebug:             getEnv("BOT_DEBUG", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func mustGetEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("Environment variable %s is required", key)
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}
