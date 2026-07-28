package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"fpl-core/internal/database"
	"fpl-core/internal/fetcher"
	"fpl-core/internal/models"

	"github.com/robfig/cron/v3"
)

// Config holds application configuration
type Config struct {
	DBPath        string
	SchemaPath    string
	FetchSchedule string
	UserID        string
	FPLTeamID     string
}

var (
	runOnce   bool
	runFetch  bool
	seedPrior bool
)

func main() {
	flag.BoolVar(&runOnce, "once", false, "Run once and exit")
	flag.BoolVar(&runFetch, "fetch", false, "Run fetch (with -once)")
	flag.BoolVar(&seedPrior, "seed-prior", false, "Seed vaastav historical data then exit")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("FPL Core Service starting (fetch-only)...")

	// Load configuration
	cfg := loadConfig()

	// Initialize database
	db, err := database.New(database.Config{
		Path:       cfg.DBPath,
		SchemaPath: cfg.SchemaPath,
	})
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	log.Println("Database initialized successfully")

	// Initialize repository
	repo := database.NewRepository(db)

	// Initialize fetcher
	httpClient := fetcher.NewClient(fetcher.Config{
		MaxRetries: 3,
		RetryDelay: 2 * time.Second,
		Timeout:    30 * time.Second,
	})
	fetcherSvc := fetcher.NewService(httpClient, repo)

	// Seed prior data from vaastav
	if seedPrior {
		log.Println("Seeding vaastav historical data...")
		vf := fetcher.NewVaastavFetcher(fetcher.GetEstimatedLatestSeason())
		entries, err := vf.FetchAllSeasons()
		if err != nil {
			log.Printf("Warning: vaastav fetch failed: %v", err)
		} else if len(entries) > 0 {
			log.Printf("Got %d entries, storing...", len(entries))
			if err := repo.StoreSeasonHistoryBatch(convertEntries(entries)); err != nil {
				log.Printf("Warning: storing vaastav data failed: %v", err)
			} else {
				for _, season := range fetcher.GetEstimatedLatestSeason() {
					if err := repo.BuildCrossSeasonMap(season); err != nil {
						log.Printf("Warning: cross-season map for %s: %v", season, err)
					}
				}
				repo.SetLastFetch()
				log.Println("Vaastav seed completed")
			}
		}
		log.Println("Vaastav seed done, exiting")
		return
	}

	// Run once mode
	if runOnce {
		log.Println("Running fetch task...")
		if err := fetcherSvc.FetchAllWithTeam(cfg.UserID, cfg.FPLTeamID); err != nil {
			log.Fatalf("Fetch task failed: %v", err)
		}
		log.Println("Fetch completed successfully")
		return
	}

	// Cron mode — fetch only
	c := cron.New()

	_, err = c.AddFunc(cfg.FetchSchedule, func() {
		log.Println("Starting scheduled fetch task...")
		if err := fetcherSvc.FetchAllWithTeam(cfg.UserID, cfg.FPLTeamID); err != nil {
			log.Printf("Scheduled fetch task failed: %v", err)
		} else {
			log.Println("Scheduled fetch task completed")
		}
	})
	if err != nil {
		log.Fatalf("Failed to schedule fetch task: %v", err)
	}

	c.Start()
	log.Printf("Cron scheduler started - fetch: %s", cfg.FetchSchedule)

	// Run initial fetch on startup
	go func() {
		time.Sleep(2 * time.Second)
		log.Println("Running initial fetch...")
		if err := fetcherSvc.FetchAllWithTeam(cfg.UserID, cfg.FPLTeamID); err != nil {
			log.Printf("Initial fetch failed: %v", err)
		}
	}()

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down gracefully...")
	c.Stop()
}

// loadConfig loads configuration from environment variables
func loadConfig() Config {
	return Config{
		DBPath:           getEnv("DB_PATH", "/data/fpl.db"),
		SchemaPath:       getEnv("SCHEMA_PATH", "/app/schema.sql"),
		FetchSchedule:    getEnv("FETCH_SCHEDULE", "0 */1 * * *"),
		UserID:           getEnv("USER_ID", "default"),
		FPLTeamID:     getEnv("FPL_TEAM_ID", ""),
	}
}

// getEnvInt gets an integer environment variable or returns a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

// convertEntries converts vaastav fetcher entries to model entries
func convertEntries(entries []fetcher.SeasonHistoryEntry) []models.SeasonHistoryEntry {
	result := make([]models.SeasonHistoryEntry, len(entries))
	for i, e := range entries {
		result[i] = models.SeasonHistoryEntry{
			Season:                   e.Season,
			PlayerName:               e.PlayerName,
			Position:                 e.Position,
			TeamName:                 e.TeamName,
			Event:                    e.Event,
			Minutes:                  e.Minutes,
			TotalPoints:              e.TotalPoints,
			GoalsScored:              e.GoalsScored,
			Assists:                  e.Assists,
			CleanSheets:              e.CleanSheets,
			GoalsConceded:            e.GoalsConceded,
			ExpectedGoals:            e.ExpectedGoals,
			ExpectedAssists:          e.ExpectedAssists,
			ExpectedGoalInvolvements: e.ExpectedGoalInvolvements,
			ExpectedGoalsConceded:    e.ExpectedGoalsConceded,
			Bonus:                    e.Bonus,
			BPS:                      e.BPS,
			Influence:                e.Influence,
			Creativity:               e.Creativity,
			Threat:                   e.Threat,
			ICTIndex:                 e.ICTIndex,
			WasHome:                  e.WasHome,
			OpponentTeam:             e.OpponentTeam,
			KickoffTime:              e.KickoffTime,
			Value:                    e.Value,
			Selected:                 e.Selected,
		}
	}
	return result
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
